package messaging

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/settings"
)

var filesPath = regexp.MustCompile(`^/files/(\d+)/([A-Za-z0-9._-]+)$`)

// readLocalFile reads an upload by its public path (/files/{org}/{name}).
func readLocalFile(a *app.App, p string) ([]byte, error) {
	m := filesPath.FindStringSubmatch(p)
	if m == nil || strings.Contains(m[2], "..") {
		return nil, errors.New("not a stored file")
	}
	return os.ReadFile(filepath.Join(a.Cfg.StorageDir, "files", m[1], m[2]))
}

// readStored reads a file path saved by another module: a /files/… path, a path relative to the
// storage directory, or an absolute path inside it.
func readStored(a *app.App, p string) ([]byte, error) {
	if p == "" {
		return nil, errors.New("no file")
	}
	if strings.HasPrefix(p, "/files/") {
		return readLocalFile(a, p)
	}
	root, _ := filepath.Abs(a.Cfg.StorageDir)
	full := p
	if !filepath.IsAbs(p) {
		full = filepath.Join(root, p)
	}
	full = filepath.Clean(full)
	if !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return nil, errors.New("file outside storage")
	}
	return os.ReadFile(full)
}

type reportRow struct {
	ID    int64  `db:"id"`
	Token string `db:"token"`
	PDF   string `db:"pdf"`
}

func latestReport(ctx context.Context, q db.Q, leadID int64) (*reportRow, error) {
	r, err := db.One[reportRow](ctx, q, `SELECT id,token,pdf FROM reports WHERE lead_id=$1 ORDER BY id DESC LIMIT 1`, leadID)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func reportMode(s *settings.Store) string {
	switch m := s.Get("report_delivery", "both"); m {
	case "link", "pdf", "both":
		return m
	}
	return "both"
}

var slugRe = regexp.MustCompile(`(?i)[^a-z0-9]+`)

// reportFiles: the latest report PDF of the lead (unless reports go as a link only) and the website
// proposal for businesses without a website (v1 Outreach::files). Call outside a transaction.
func reportFiles(ctx context.Context, a *app.App, s *settings.Store, l *shared.Lead, r *reportRow) ([]Attachment, error) {
	if r == nil {
		return nil, errors.New("No report generated yet")
	}
	slug := slugRe.ReplaceAllString(cut(l.Name, 40), "-")
	var out []Attachment
	if reportMode(s) != "link" {
		data := loadReportPDF(ctx, a, r)
		if data == nil {
			return nil, errors.New("Report PDF could not be created - regenerate the report")
		}
		out = append(out, Attachment{Name: "Audit-Report-" + slug + ".pdf", Mime: "application/pdf", Data: data, URL: a.Cfg.AppURL() + "/r/" + r.Token + ".pdf"})
	}
	if pp := s.Get("proposal_pdf", ""); l.Website == "" && pp != "" {
		if data, err := readStored(a, pp); err == nil {
			u := pp
			if strings.HasPrefix(u, "/") {
				u = a.Cfg.AppURL() + u
			}
			out = append(out, Attachment{Name: "Website-Proposal-" + slugRe.ReplaceAllString(s.Get("company_name", "Agency"), "-") + ".pdf", Mime: "application/pdf", Data: data, URL: u})
		}
	}
	return out, nil
}

// loadReportPDF reads the stored PDF; when it is not on this server it is fetched from the public
// report URL (the reports module renders it on demand).
func loadReportPDF(ctx context.Context, a *app.App, r *reportRow) []byte {
	if b, err := readStored(a, r.PDF); err == nil && bytes.HasPrefix(b, []byte("%PDF")) {
		return b
	}
	res, err := netx.Do(ctx, netx.Req{URL: a.Cfg.AppURL() + "/r/" + r.Token + ".pdf", Timeout: 90 * time.Second, Max: 30 << 20})
	if err == nil && res.OK() && bytes.HasPrefix(res.Body, []byte("%PDF")) {
		return res.Body
	}
	return nil
}

var reportURLRe = regexp.MustCompile(`^/r/([A-Za-z0-9]+)\.pdf$`)

// fillAttachment loads Data for an attachment given only by URL: /files/… uploads and /r/{token}.pdf
// reports are read from this server (relative or with the app address); other URLs are left as they are.
// A relative URL is made absolute so WhatsApp providers can fetch it.
func fillAttachment(ctx context.Context, a *app.App, orgID int64, f Attachment) Attachment {
	base := a.Cfg.AppURL()
	p := f.URL
	if strings.HasPrefix(p, base+"/") {
		p = strings.TrimPrefix(p, base)
	}
	if len(f.Data) == 0 && strings.HasPrefix(p, "/") {
		if strings.HasPrefix(p, "/files/") {
			f.Data, _ = readLocalFile(a, p)
		} else if m := reportURLRe.FindStringSubmatch(p); m != nil {
			var r *reportRow
			a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
				x, err := db.One[reportRow](ctx, tx, `SELECT id,token,pdf FROM reports WHERE token=$1`, m[1])
				if err == nil {
					r = &x
				}
				return nil
			})
			if r != nil {
				f.Data = loadReportPDF(ctx, a, r)
			}
		}
	}
	if strings.HasPrefix(f.URL, "/") {
		f.URL = base + f.URL
	}
	if f.Name == "" {
		f.Name = path.Base(p)
	}
	if f.Mime == "" && strings.HasSuffix(strings.ToLower(f.Name), ".pdf") {
		f.Mime = "application/pdf"
	}
	return f
}
