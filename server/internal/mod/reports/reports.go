// Package reports: AI growth reports – web report, branded PDF, charts, illustrations, templates.
// OWNER: reports module. Exported functions below are contracts used by other modules – keep signatures.
package reports

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/core"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/ai"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
)

type EnqueueOpts struct {
	PromptID int64 // 0 = active report prompt
	Contact  bool  // send to the business when ready (outreach)
	Bulk     bool  // part of a bulk run (parallel / batch engine)
}

func Register(a *app.App) {
	pro := app.Module("prospecting")
	// Reports of a lead
	a.Post("/leads/{id}/report", startReport, app.Perm("prospecting"), pro)
	a.Get("/leads/{id}/reports", leadReports, app.Perm("leads"), pro)
	// Audit Reports page
	a.Get("/reports", listReports, app.Perm("prospecting"), pro)
	a.Get("/reports/preview", preview, app.Perm("templates"), pro)
	a.Get("/reports/{id}/pdf", downloadPDF, app.Perm("leads"), pro)
	a.Delete("/reports/{id}", deleteReport, app.Perm("prospecting"), pro)
	// Templates & report design: prompts of every type, illustrations, fonts
	a.Get("/prompt-types", promptTypes, app.Perm("templates"), pro)
	a.Get("/prompts", listPrompts, app.Perm("templates"), pro)
	a.Get("/prompts/{id}", getPrompt, app.Perm("templates"), pro)
	a.Post("/prompts", savePrompt, app.Perm("templates"), pro)
	a.Put("/prompts/{id}", savePrompt, app.Perm("templates"), pro)
	a.Post("/prompts/{id}/use", usePrompt, app.Perm("templates"), pro)
	a.Post("/prompts/{id}/duplicate", dupPrompt, app.Perm("templates"), pro)
	a.Post("/prompts/reset", resetPrompt, app.Perm("templates"), pro)
	a.Delete("/prompts/{id}", deletePrompt, app.Perm("templates"), pro)
	a.Get("/assets", listAssets, app.Perm("templates"), pro)
	a.Post("/assets", uploadAssets, app.Perm("templates"), pro)
	a.Delete("/assets/{id}", deleteAsset, app.Perm("templates"), pro)
	a.Get("/report-fonts", listFonts, app.Perm("settings"))
	a.Post("/report-fonts", uploadFont, app.Perm("settings"))
	a.Delete("/report-fonts/{name}", deleteFont, app.Perm("settings"))
	// Public web report, PDF download and fonts
	a.Public(http.MethodGet, "/r/fonts/{name}", publicFont)
	a.Public(http.MethodGet, "/r/{token}", publicReport)
	// Background job & defaults of a new workspace
	a.Jobs.Handle(JobReport, 15*time.Minute, handleJob(a))
	a.Seed(seed)
}

// ---------------------------------------------------------------- lead reports

func startReport(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	var in struct {
		PW       int64 `json:"pw"`
		PN       int64 `json:"pn"`
		PromptID int64 `json:"prompt_id"`
		Contact  bool  `json:"contact"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	tx := c.Tx()
	if n, _ := db.Val[int64](c, tx, `SELECT count(*) FROM leads WHERE id=$1`, id); n == 0 {
		return nil, app.NotFound()
	}
	if !ai.Configured(c.S()) {
		return nil, app.Bad("Connect an AI provider first (Settings → AI Connection).")
	}
	if _, err := tx.Exec(c, `DELETE FROM jobs WHERE type=$1 AND ref_id=$2 AND status='pending'`, JobReport, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(c, `UPDATE leads SET abort_flag=false, progress=58, progress_msg='Queued for AI report', stage='queued', last_error='' WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err := pushReport(c, tx, c.Org.ID, id, payload{PW: in.PW, PN: in.PN, PromptID: in.PromptID, Contact: in.Contact}); err != nil {
		return nil, err
	}
	if err := app.Event(c, tx, id, "stage", "Report regeneration queued", "", map[string]any{"stage": "queued"}); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Report regeneration queued."}, nil
}

type reportItem struct {
	ID         int64     `db:"id" json:"id"`
	LeadID     int64     `db:"lead_id" json:"lead_id"`
	Token      string    `db:"token" json:"token"`
	Views      int       `db:"views" json:"views"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	HasPDF     bool      `db:"has_pdf" json:"has_pdf"`
	PromptName *string   `db:"pname" json:"prompt_name"`
	Name       string    `db:"name" json:"name"`
	Category   string    `db:"category" json:"category"`
	Website    string    `db:"website" json:"website"`
	WebScore   *int16    `db:"web_score" json:"web_score"`
	MapScore   *int16    `db:"map_score" json:"map_score"`
	Stage      string    `db:"stage" json:"stage"`
	URL        string    `json:"url"`
	PDFURL     string    `json:"pdf_url"`
}

func (r *reportItem) links(c *app.Ctx) {
	r.URL = c.BaseURL() + "/r/" + r.Token
	r.PDFURL = c.BaseURL() + "/r/" + r.Token + ".pdf"
}

const itemCols = `r.id,r.lead_id,r.token,r.views,r.created_at,r.pdf<>'' has_pdf,p.name pname,l.name,l.category,l.website,l.web_score,l.map_score,l.stage`

func leadReports(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	l, err := shared.GetLead(c, c.Tx(), id)
	if err != nil {
		return nil, app.NotFound()
	}
	if !c.LeadOK(l.IsLead, l.AssignedTo) {
		return nil, app.Forbidden()
	}
	rows, err := db.All[reportItem](c, c.Tx(), `SELECT `+itemCols+` FROM reports r JOIN leads l ON l.id=r.lead_id LEFT JOIN prompts p ON p.id=r.prompt_id WHERE r.lead_id=$1 ORDER BY r.id DESC`, id)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].links(c)
	}
	return map[string]any{"reports": rows, "mode": reportMode(c.S())}, nil
}

// ---------------------------------------------------------------- Audit Reports page

func listReports(c *app.Ctx) (any, error) {
	q := c.Query("q")
	w, args := "", []any{}
	if q != "" {
		w = ` WHERE l.name ILIKE $1`
		args = append(args, "%"+q+"%")
	}
	rows, err := db.All[reportItem](c, c.Tx(), `SELECT `+itemCols+` FROM reports r JOIN leads l ON l.id=r.lead_id LEFT JOIN prompts p ON p.id=r.prompt_id`+w+` ORDER BY r.id DESC LIMIT 300`, args...)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].links(c)
	}
	k, err := db.Map(c, c.Tx(), `SELECT count(*) total, count(*) FILTER (WHERE views>0) opened, COALESCE(sum(views),0) views, count(*) FILTER (WHERE created_at>=now()-interval '7 days') week FROM reports`)
	if err != nil {
		return nil, err
	}
	return map[string]any{"rows": rows, "kpi": k, "mode": reportMode(c.S())}, nil
}

func deleteReport(c *app.Ctx) (any, error) {
	r, err := db.One[reportRow](c, c.Tx(), `SELECT `+reportCols+` FROM reports WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	if f := pdfPath(c.A, c.Org.ID, r.PDF); f != "" {
		os.Remove(f)
	}
	if _, err := c.Tx().Exec(c, `DELETE FROM reports WHERE id=$1`, r.ID); err != nil {
		return nil, err
	}
	c.Tx().Exec(c, `DELETE FROM public_tokens WHERE token=$1 AND kind='report'`, r.Token)
	if err := app.Event(c, c.Tx(), r.LeadID, "note", "Report deleted", "", nil); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Report deleted."}, nil
}

// downloadPDF: PDF for the team (also when businesses only get the web link).
func downloadPDF(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	lid, err := db.Val[int64](c, c.Tx(), `SELECT lead_id FROM reports WHERE id=$1`, id)
	if err != nil || lid == 0 {
		return nil, app.NotFound()
	}
	l, err := shared.GetLead(c, c.Tx(), lid)
	if err != nil || !c.LeadOK(l.IsLead, l.AssignedTo) {
		return nil, app.Forbidden()
	}
	f, name, err := ensurePDF(c, c.A, c.Org.ID, id)
	if err != nil {
		return nil, app.Bad("The PDF could not be created: " + err.Error())
	}
	return servePDF(c, f, name, true)
}

func servePDF(c *app.Ctx, f, name string, attach bool) (any, error) {
	c.Raw()
	disp := "inline"
	if attach {
		disp = "attachment"
	}
	h := c.W.Header()
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", disp+`; filename="`+name+`"`)
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Robots-Tag", "noindex")
	http.ServeFile(c.W, c.R, f)
	return nil, nil
}

// ---------------------------------------------------------------- design preview (no AI cost)

func sampleLead() *shared.Lead {
	r, rv, ws, ms := 4.6, 120, int16(62), int16(71)
	return &shared.Lead{Name: "Sample Business", Category: "Dental clinic", City: "Pune", Address: "Baner, Pune", Website: "https://example.com",
		Rating: &r, Reviews: &rv, Position: 3, WebScore: &ws, MapScore: &ms, Keyword: "dentist"}
}

func sampleData() J {
	var d map[string]any
	if err := jsonUnmarshal(sampleJSON, &d); err != nil {
		return J{}
	}
	return J(d)
}

// preview re-renders the latest report (or sample content) with the current design.
func preview(c *app.Ctx) (any, error) {
	pid, _ := strconv.ParseInt(c.Query("prompt_id"), 10, 64)
	web := c.Query("format") == "html"
	var fu func(string) string
	if web {
		fu = func(f string) string { return "/r/fonts/" + f }
	}
	var in *RenderIn
	r, err := db.One[reportRow](c, c.Tx(), `SELECT `+reportCols+` FROM reports ORDER BY id DESC LIMIT 1`)
	if err == nil {
		if in, err = renderInput(c, c.A, c.Tx(), c.S(), c.Org.ID, &r, fu); err != nil {
			in = nil
		}
	}
	if in == nil {
		if in, err = designInput(c, c.A, c.Tx(), c.S(), c.Org.ID, 0, fu); err != nil {
			return nil, err
		}
		in.Lead, in.Keyword, in.Audit, in.HasWeb = sampleLead(), "dentist", J{}, true
	}
	// The illustration set of the chosen prompt
	if in2, err := designInput(c, c.A, c.Tx(), c.S(), c.Org.ID, pid, fu); err == nil {
		in.Art = in2.Art
	}
	if in.Data.S("summary") == "" {
		in.Data = sampleData()
	}
	if web {
		c.Raw()
		c.W.Header().Set("Content-Type", "text/html; charset=utf-8")
		c.W.Write([]byte(Render(in)))
		return nil, nil
	}
	b, err := renderPDF(c, c.A, in)
	if err != nil {
		return nil, app.Bad("The PDF engine is not reachable (" + err.Error() + "). Open the web preview instead.")
	}
	c.Raw()
	c.W.Header().Set("Content-Type", "application/pdf")
	c.W.Header().Set("Content-Disposition", `inline; filename="design-preview.pdf"`)
	c.W.Write(b)
	return nil, nil
}

// ---------------------------------------------------------------- fonts (Settings → Branding)

func listFonts(c *app.Ctx) (any, error) {
	fonts := []string{"Inter", "DejaVu Sans", "Helvetica", "Times-Roman", "Courier"}
	seen := map[string]bool{}
	for _, f := range fonts {
		seen[f] = true
	}
	custom := customFonts(c.A, c.Org.ID)
	for _, f := range custom {
		if !seen[f] {
			fonts = append(fonts, f)
		}
	}
	if custom == nil {
		custom = []string{}
	}
	return map[string]any{"fonts": fonts, "custom": custom}, nil
}

func uploadFont(c *app.Ctx) (any, error) {
	if err := c.R.ParseMultipartForm(12 << 20); err != nil {
		return nil, app.Bad("Upload failed.")
	}
	f, h, err := c.R.FormFile("file")
	if err != nil {
		return nil, app.Bad("Choose a font file.")
	}
	defer f.Close()
	if strings.ToLower(filepath.Ext(h.Filename)) != ".ttf" {
		return nil, app.Bad("Upload a TrueType font (.ttf).")
	}
	n := strings.Trim(regexp.MustCompile(`[^A-Za-z0-9_-]+`).ReplaceAllString(strings.TrimSuffix(h.Filename, filepath.Ext(h.Filename)), "-"), "-")
	if n == "" || stdFonts[n] {
		return nil, app.Bad("Please rename the font file.")
	}
	b := make([]byte, 0, h.Size)
	buf := make([]byte, 32<<10)
	for {
		k, e := f.Read(buf)
		b = append(b, buf[:k]...)
		if len(b) > 8<<20 {
			return nil, app.Bad("Font file is too large.")
		}
		if e != nil {
			break
		}
	}
	// TrueType / OpenType signatures
	if len(b) < 4 || !(string(b[:4]) == "\x00\x01\x00\x00" || string(b[:4]) == "true" || string(b[:4]) == "OTTO") {
		return nil, app.Bad("This is not a valid TrueType font.")
	}
	dir := fontDir(c.A, c.Org.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, n+".ttf"), b, 0o640); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "name": n, "message": fmt.Sprintf("Font \"%s\" uploaded.", n)}, nil
}

func deleteFont(c *app.Ctx) (any, error) {
	n := fontName(c.Param("name"))
	if n == "" {
		return nil, app.NotFound()
	}
	os.Remove(filepath.Join(fontDir(c.A, c.Org.ID), n+".ttf"))
	return nil, nil
}

// ---------------------------------------------------------------- uploads (illustrations)

var svgBad = regexp.MustCompile(`(?i)<script|\bon\w+\s*=|javascript:|<foreignObject|<iframe|<embed|<object|<!ENTITY|<use[^>]+href=["']?[^#"'\s]`)

func uploadAssets(c *app.Ctx) (any, error) {
	if err := c.R.ParseMultipartForm(40 << 20); err != nil {
		return nil, app.Bad("Upload failed.")
	}
	files := c.R.MultipartForm.File["art"]
	if len(files) == 0 {
		return nil, app.Bad("Choose one or more images.")
	}
	place := c.R.FormValue("placement")
	if !placeOK(place) {
		place = "any"
	}
	var pid any
	if id, _ := strconv.ParseInt(c.R.FormValue("prompt_id"), 10, 64); id > 0 {
		if n, _ := db.Val[int64](c, c.Tx(), `SELECT count(*) FROM prompts WHERE id=$1`, id); n > 0 {
			pid = id
		}
	}
	n := 0
	for _, fh := range files {
		ext := strings.ToLower(filepath.Ext(fh.Filename))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".webp", ".svg":
		default:
			continue
		}
		f, err := fh.Open()
		if err != nil {
			continue
		}
		data := make([]byte, 0, fh.Size)
		buf := make([]byte, 32<<10)
		for {
			k, e := f.Read(buf)
			data = append(data, buf[:k]...)
			if e != nil || len(data) > 10<<20 {
				break
			}
		}
		f.Close()
		if ext == ".svg" && svgBad.Match(data) {
			continue
		}
		p, err := core.SaveFile(c, c.A, c.Org.ID, fh.Filename, bytes.NewReader(data), 10<<20)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(c.R.FormValue("name"))
		if name == "" {
			name = strings.TrimSuffix(fh.Filename, filepath.Ext(fh.Filename))
		}
		if _, err := db.Insert(c, c.Tx(), "assets", map[string]any{"name": trunc(name, 150), "file": p, "placement": place, "prompt_id": pid}); err != nil {
			return nil, err
		}
		n++
	}
	return map[string]any{"ok": true, "count": n, "message": fmt.Sprintf("%d illustration(s) uploaded.", n)}, nil
}

func deleteAsset(c *app.Ctx) (any, error) {
	as, err := db.One[assetRow](c, c.Tx(), `SELECT id,name,file,placement,prompt_id,created_at FROM assets WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	if n, _ := db.Val[int64](c, c.Tx(), `SELECT count(*) FROM assets WHERE file=$1 AND id<>$2`, as.File, as.ID); n == 0 {
		if f := localFile(c.A, c.Org.ID, as.File); f != "" {
			os.Remove(f)
		}
	}
	if _, err := c.Tx().Exec(c, `DELETE FROM assets WHERE id=$1`, as.ID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Illustration removed."}, nil
}

func listAssets(c *app.Ctx) (any, error) {
	q := `SELECT a.id,a.name,a.file,a.placement,a.prompt_id,p.name pname,a.created_at FROM assets a LEFT JOIN prompts p ON p.id=a.prompt_id`
	args := []any{}
	if pid, _ := strconv.ParseInt(c.Query("prompt_id"), 10, 64); pid > 0 {
		q += ` WHERE a.prompt_id=$1`
		args = append(args, pid)
	}
	rows, err := db.All[assetRow](c, c.Tx(), q+` ORDER BY a.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	P, A := c.S().Get("brand_primary", "#0B0B0B"), c.S().Get("brand_accent", "#B8860B")
	type bi struct {
		Key   string `json:"key"`
		Label string `json:"label"`
		URI   string `json:"uri"`
	}
	var builtin []bi
	places := []map[string]string{}
	for _, p := range Places {
		places = append(places, map[string]string{"key": p[0], "label": p[1]})
		if p[0] != "any" {
			builtin = append(builtin, bi{p[0], p[1], illusURI(p[0], P, A)})
		}
	}
	prompts, err := db.All[promptRow](c, c.Tx(), `SELECT id,type,name,'' subject,'' content,is_active,is_default,updated_at FROM prompts WHERE type IN ('report_website','report_noweb') ORDER BY type,name`)
	if err != nil {
		return nil, err
	}
	return map[string]any{"assets": rows, "builtin": builtin, "places": places, "prompts": prompts}, nil
}
