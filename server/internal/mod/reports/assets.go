package reports

// Files used by the report: logos, uploaded illustrations, built-in and uploaded fonts, stored PDFs.

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/pdf"
	"github.com/narendersinghh/setu/server/internal/settings"
)

//go:embed fonts/*.ttf
var fontFS embed.FS

// builtinFont returns an embedded font file (Inter / Inter Tight).
func builtinFont(name string) ([]byte, bool) {
	for _, f := range builtinFonts {
		if f.File == name {
			b, err := fontFS.ReadFile("fonts/" + name)
			return b, err == nil
		}
	}
	return nil, false
}

// fontAssets: the built-in fonts as Gotenberg assets (referenced by file name from the HTML).
func fontAssets() []pdf.Asset {
	var out []pdf.Asset
	for _, f := range builtinFonts {
		if b, ok := builtinFont(f.File); ok {
			out = append(out, pdf.Asset{Name: f.File, Data: b})
		}
	}
	return out
}

var fontNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// fontName sanitises a font family / file name (v1 preg_replace('/[^a-z0-9_-]/i','',$n)).
func fontName(n string) string { return fontNameRe.ReplaceAllString(strings.TrimSpace(n), "") }

func fontDir(a *app.App, orgID int64) string {
	return filepath.Join(a.Cfg.StorageDir, "fonts", strconv.FormatInt(orgID, 10))
}

// customFonts lists the uploaded brand fonts of a workspace.
func customFonts(a *app.App, orgID int64) []string {
	ents, _ := os.ReadDir(fontDir(a, orgID))
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".ttf") {
			out = append(out, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		}
	}
	return out
}

// customFontURIs: the uploaded fonts used by the heading / body settings, as data URIs.
func customFontURIs(a *app.App, orgID int64, s Getter) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"font_heading", "font_body"} {
		n := fontName(s.Get(k, ""))
		if n == "" || stdFonts[n] {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(fontDir(a, orgID), n+".ttf")); err == nil {
			out[n] = "data:font/ttf;base64," + base64.StdEncoding.EncodeToString(b)
		}
	}
	return out
}

var filePathRe = regexp.MustCompile(`^/files/(\d+)/([a-f0-9]{32}\.[a-z]{3,4})$`)

// localFile maps a public upload path (/files/{org}/{name}) to the file on disk, only for the workspace.
func localFile(a *app.App, orgID int64, p string) string {
	m := filePathRe.FindStringSubmatch(strings.TrimSpace(p))
	if m == nil || m[1] != strconv.FormatInt(orgID, 10) {
		return ""
	}
	return filepath.Join(a.Cfg.StorageDir, "files", m[1], m[2])
}

var mimes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif", ".svg": "image/svg+xml"}

// fileURI reads an image as a data URI ("" when missing).
func fileURI(f string) string {
	if f == "" {
		return ""
	}
	mt, ok := mimes[strings.ToLower(filepath.Ext(f))]
	if !ok {
		return ""
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return ""
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// logoPicker: two logo versions – company_logo for dark backgrounds (light logo), company_logo_light
// for light backgrounds (dark logo). When only the other version exists it is put on a contrasting chip.
func logoPicker(a *app.App, orgID int64, s Getter) func(bg string) (string, string) {
	cache := map[string]string{}
	load := func(p string) string {
		if v, ok := cache[p]; ok {
			return v
		}
		v := fileURI(localFile(a, orgID, p))
		cache[p] = v
		return v
	}
	d, l := s.Get("company_logo", ""), s.Get("company_logo_light", "")
	return func(bg string) (string, string) {
		dark := isDark(bg)
		switch {
		case dark && load(d) != "":
			return load(d), ""
		case !dark && load(l) != "":
			return load(l), ""
		case dark && load(l) != "":
			return load(l), "#FFFFFF"
		case !dark && load(d) != "":
			return load(d), "#0B0B0B"
		}
		return "", ""
	}
}

type assetRow struct {
	ID        int64     `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	File      string    `db:"file" json:"file"`
	Placement string    `db:"placement" json:"placement"`
	PromptID  *int64    `db:"prompt_id" json:"prompt_id"`
	PName     *string   `db:"pname" json:"prompt_name"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// artPicker: uploaded doodle for a section (prompt-specific first, then for all prompts, then
// placement "any"), otherwise the built-in illustration in the brand colours.
func artPicker(a *app.App, orgID int64, rows []assetRow, pid int64, P, A string) func(key string) string {
	pick := func(key string) []assetRow {
		var mine, all []assetRow
		for _, r := range rows {
			if r.Placement != key {
				continue
			}
			if r.PromptID != nil && *r.PromptID == pid && pid != 0 {
				mine = append(mine, r)
			} else if r.PromptID == nil {
				all = append(all, r)
			}
		}
		if len(mine) > 0 {
			return mine
		}
		return all
	}
	return func(key string) string {
		c := pick(key)
		if len(c) == 0 && key != "cover" {
			for _, r := range rows {
				if r.Placement == "any" && (r.PromptID == nil || (pid != 0 && *r.PromptID == pid)) {
					c = append(c, r)
				}
			}
		}
		if len(c) > 0 {
			if u := fileURI(localFile(a, orgID, c[rand.IntN(len(c))].File)); u != "" {
				return u
			}
		}
		return illusURI(key, s2(P, "#0B0B0B"), s2(A, "#B8860B"))
	}
}

func s2(v, def string) string { return safeColor(v, def) }

// ---------------------------------------------------------------- stored reports

type reportRow struct {
	ID        int64     `db:"id" json:"id"`
	LeadID    int64     `db:"lead_id" json:"lead_id"`
	PromptID  *int64    `db:"prompt_id" json:"prompt_id"`
	Token     string    `db:"token" json:"token"`
	HTML      string    `db:"html" json:"-"`
	PDF       string    `db:"pdf" json:"-"`
	Views     int       `db:"views" json:"views"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

const reportCols = `id,lead_id,prompt_id,token,html,pdf,views,created_at`

// keywordOf: the search phrase analysed (campaign keyword, else category).
func keywordOf(l *shared.Lead) string {
	if l.Keyword != "" {
		return l.Keyword
	}
	return l.Category
}

func leadAudit(l *shared.Lead, raw []byte) (J, bool) {
	var a map[string]any
	if len(raw) > 0 {
		json.Unmarshal(raw, &a)
	}
	au := J(a)
	if au == nil {
		au = J{}
	}
	down := au.M("website").M("meta")["down"]
	return au, l.Website != "" && empty(down)
}

// competitors: up to 5 other businesses of the same Maps search.
func competitors(ctx context.Context, q db.Q, l *shared.Lead) []Competitor {
	if l.CampaignID == 0 {
		return nil
	}
	c, _ := db.All[Competitor](ctx, q, `SELECT name,rating::float8 rating,reviews,position,website FROM leads WHERE campaign_id=$1 AND id<>$2 ORDER BY position LIMIT 5`, l.CampaignID, l.ID)
	return c
}

// renderInput loads everything to draw a stored report (inside a tenant transaction).
// fontURL: nil → built-in font files are referenced by name (PDF assets).
func renderInput(ctx context.Context, a *app.App, q db.Q, s *settings.Store, orgID int64, r *reportRow, fontURL func(string) string) (*RenderIn, error) {
	l, err := shared.GetLead(ctx, q, r.LeadID)
	if err != nil {
		return nil, err
	}
	raw, _ := db.Val[[]byte](ctx, q, `SELECT audit FROM leads WHERE id=$1`, r.LeadID)
	au, hasWeb := leadAudit(l, raw)
	var d map[string]any
	json.Unmarshal([]byte(r.HTML), &d)
	pid := int64(0)
	if r.PromptID != nil {
		pid = *r.PromptID
	}
	in, err := designInput(ctx, a, q, s, orgID, pid, fontURL)
	if err != nil {
		return nil, err
	}
	in.Lead, in.Keyword, in.Audit, in.Data, in.HasWeb, in.Comp = l, keywordOf(l), au, J(d), hasWeb, competitors(ctx, q, l)
	return in, nil
}

// designInput: the workspace design (brand, logos, illustrations, fonts) without report content.
func designInput(ctx context.Context, a *app.App, q db.Q, s *settings.Store, orgID, pid int64, fontURL func(string) string) (*RenderIn, error) {
	arts, err := db.All[assetRow](ctx, q, `SELECT id,name,file,placement,prompt_id,created_at FROM assets`)
	if err != nil {
		return nil, err
	}
	tz := app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))
	return &RenderIn{
		S: s, Now: time.Now().In(tz), FontURL: fontURL,
		Art:         artPicker(a, orgID, arts, pid, s.Get("brand_primary", "#0B0B0B"), s.Get("brand_accent", "#B8860B")),
		Logo:        logoPicker(a, orgID, s),
		CustomFonts: customFontURIs(a, orgID, s),
	}, nil
}

var fileSafe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// pdfName: download name of a report PDF.
func pdfName(lead string) string {
	n := strings.Trim(fileSafe.ReplaceAllString(lead, "-"), "-")
	if n == "" {
		n = "report"
	}
	return "Audit-Report-" + n + ".pdf"
}

// savePDF stores a rendered PDF under storage/reports/{org}/{yyyymm}/ and returns its relative path.
func savePDF(a *app.App, orgID int64, token, name string, b []byte) (string, error) {
	n := []rune(name)
	n = n[:min(40, len(n))]
	base := strings.Trim(fileSafe.ReplaceAllString(string(n), "-"), "-")
	rel := filepath.Join("reports", strconv.FormatInt(orgID, 10), time.Now().Format("200601"), base+"-"+token+".pdf")
	full := filepath.Join(a.Cfg.StorageDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, b, 0o640); err != nil {
		return "", err
	}
	return rel, nil
}

// pdfPath: the file of a stored PDF ("" when none / missing).
func pdfPath(a *app.App, orgID int64, rel string) string {
	if rel == "" || strings.Contains(rel, "..") {
		return ""
	}
	if !strings.HasPrefix(filepath.ToSlash(rel), "reports/"+strconv.FormatInt(orgID, 10)+"/") {
		return ""
	}
	f := filepath.Join(a.Cfg.StorageDir, rel)
	if st, err := os.Stat(f); err != nil || st.IsDir() {
		return ""
	}
	return f
}

// renderPDF draws a report and converts it with Gotenberg.
func renderPDF(ctx context.Context, a *app.App, in *RenderIn) ([]byte, error) {
	if a.Cfg.GotenbergURL == "" {
		return nil, errors.New("the PDF engine (Gotenberg) is not configured")
	}
	return pdf.Render(ctx, a.Cfg.GotenbergURL, Render(in), fontAssets(), pdf.Options{PaperWidth: 11.69, PaperHeight: 8.27})
}
