package prospecting

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/mod/messaging"
	"github.com/narendersinghh/setu/server/internal/mod/reports"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Cross-module calls (replaceable in tests).
var (
	enqueueReport = func(ctx context.Context, tx pgx.Tx, org, lead, prompt int64, contact, bulk bool) error {
		return reports.Enqueue(ctx, tx, org, lead, reports.EnqueueOpts{PromptID: prompt, Contact: contact, Bulk: bulk})
	}
	reportPDF    = reports.PDF
	sendEmail    = messaging.SendEmail
	sendWhatsApp = messaging.SendWhatsApp
)

func reportMode(s *settings.Store) string {
	switch m := s.Get("report_delivery", "both"); m {
	case "link", "pdf", "both":
		return m
	}
	return "both"
}

// ---------------------------------------------------------------- scheduling (set-based)

// scheduleContact queues the "contact" job for leads, spaced by send_gap after the last queued send.
// onlyWithReport skips leads without a report. now=true sends straight away. Returns how many were queued.
func scheduleContact(ctx context.Context, tx pgx.Tx, s *settings.Store, ids []int64, onlyWithReport, now bool, payload map[string]any) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	gap := max(0, s.Int("send_gap", 90))
	if now {
		gap = 0
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM jobs WHERE org_id=app_org() AND type='contact' AND status='pending' AND ref_id = ANY($1)`, ids); err != nil {
		return 0, err
	}
	tz := s.Get("timezone", "Asia/Kolkata")
	if app.LoadTZ(tz).String() != tz {
		tz = "Asia/Kolkata"
	}
	var n int
	err := tx.QueryRow(ctx, `WITH base AS (
  SELECT CASE WHEN $5 THEN now() ELSE GREATEST(now(), COALESCE(max(run_after) + make_interval(secs => $2::float8), now())) END t
  FROM jobs WHERE org_id=app_org() AND type='contact' AND status IN ('pending','running')
), sel AS (
  SELECT l.id, row_number() OVER (ORDER BY array_position($1::bigint[], l.id)) - 1 AS n FROM leads l
  WHERE l.id = ANY($1) AND (NOT $4 OR EXISTS (SELECT 1 FROM reports r WHERE r.lead_id=l.id))
), jb AS (
  INSERT INTO jobs (org_id,type,ref_id,payload,run_after,max_attempts)
  SELECT app_org(),'contact',sel.id,$3::jsonb,base.t + make_interval(secs => $2::float8 * sel.n),2 FROM sel, base
  RETURNING ref_id, run_after
), ev AS (
  INSERT INTO events (lead_id,type,message) SELECT ref_id,'scheduled','Outreach scheduled for '||to_char(run_after AT TIME ZONE $6,'DD Mon, HH12:MI AM') FROM jb
), la AS (
  UPDATE leads SET last_activity=now() WHERE id IN (SELECT ref_id FROM jb)
)
SELECT count(*) FROM jb`, ids, gap, payload, onlyWithReport, now, tz).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------- contact: send the report to the business

type contactPayload struct {
	Force    bool     `json:"force,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

var slugRe = regexp.MustCompile(`(?i)[^a-z0-9]+`)

func (m *mod) jobContact(ctx context.Context, j *jobs.Job, r *run) error {
	var p contactPayload
	_ = j.Decode(&p)
	if len(p.Channels) == 0 {
		p.Channels = []string{"email", "whatsapp"}
	}
	var l *shared.Lead
	var s *settings.Store
	var defer_ time.Duration
	var skipped bool
	err := r.tx(ctx, func(tx pgx.Tx, st *settings.Store) error {
		var err error
		s = st
		if l, err = shared.GetLead(ctx, tx, r.lead); err != nil {
			return err
		}
		if kw, _ := db.Val[string](ctx, tx, `SELECT COALESCE(NULLIF(c.keyword,''),l.category) FROM leads l LEFT JOIN campaigns c ON c.id=l.campaign_id WHERE l.id=$1`, l.ID); kw != "" {
			l.Keyword = kw
		}
		if l.AbortFlag {
			return shared.ErrAborted
		}
		// Daily sending limit (businesses contacted today, workspace time zone)
		if limit := s.Int("daily_limit", 60); limit > 0 && !p.Force {
			loc := app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))
			now := time.Now().In(loc)
			day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			n, err := db.Val[int](ctx, tx, `SELECT count(DISTINCT lead_id) FROM messages WHERE direction='out' AND created_at >= $1`, day)
			if err != nil {
				return err
			}
			if n >= limit {
				tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 10, 0, 0, 0, loc)
				defer_ = time.Until(tomorrow)
				return app.Event(ctx, tx, l.ID, "scheduled", "Daily sending limit reached – moved to tomorrow", "", nil)
			}
		}
		if s.Get("skip_contacted", "1") == "1" && !p.Force && l.Source == "maps" {
			cid, err := db.Val[*int64](ctx, tx, `SELECT campaign_id FROM leads WHERE place_id=$1 AND id<>$2 AND (email_status IN ('sent','opened') OR wa_status IN ('sent','delivered','read','manual')) LIMIT 1`, l.PlaceID, l.ID)
			if err != nil {
				return err
			}
			if cid != nil {
				skipped = true
				return setStage(ctx, tx, l.ID, "skipped", "Skipped – already contacted in campaign #"+itoa(*cid))
			}
		}
		// Seconds between two sends: reserve the workspace's send slot
		if gap := s.Int("send_gap", 90); gap > 0 && !p.Force {
			var ok bool
			if err := tx.QueryRow(ctx, `WITH up AS (
  INSERT INTO outreach_state (last_send_at) VALUES (now())
  ON CONFLICT (org_id) DO UPDATE SET last_send_at=now() WHERE outreach_state.last_send_at <= now() - make_interval(secs => $1::float8)
  RETURNING 1) SELECT EXISTS (SELECT 1 FROM up)`, gap).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				w, _ := db.Val[float64](ctx, tx, `SELECT EXTRACT(EPOCH FROM last_send_at + make_interval(secs => $1::float8) - now())::float8 FROM outreach_state`, gap)
				defer_ = time.Duration(max(5, w)) * time.Second
				return nil
			}
		}
		return setStage(ctx, tx, l.ID, "contacting", "")
	})
	if err != nil {
		return err
	}
	if defer_ > 0 {
		return jobs.Defer(defer_)
	}
	if skipped {
		return nil
	}
	if err := r.progress(ctx, 95, "Sending email & WhatsApp"); err != nil {
		return err
	}
	files, err := m.files(ctx, s, r.org, l)
	if err != nil {
		return err
	}
	var vars map[string]string
	var emailSub, emailBody, waBody string
	nw := ""
	if l.Website == "" {
		nw = "_noweb"
	}
	if err := r.tx(ctx, func(tx pgx.Tx, _ *settings.Store) error {
		vars = shared.LeadVars(ctx, m.a, tx, s, l, nil)
		emailSub, emailBody = message(ctx, tx, "email"+nw)
		_, waBody = message(ctx, tx, "whatsapp"+nw)
		return nil
	}); err != nil {
		return err
	}
	mode := reportMode(s)
	withLink := func(b string) string {
		if mode != "pdf" && !strings.Contains(b, "{{report_link}}") {
			b += "\n\nYour report: {{report_link}}"
		}
		return b
	}
	has := func(c string) bool {
		for _, x := range p.Channels {
			if x == c {
				return true
			}
		}
		return false
	}
	ok := 0
	var errs []string
	bg := context.WithoutCancel(ctx)
	note := func(fn func(tx pgx.Tx) error) {
		if e := m.a.DB.Org(bg, r.org, fn); e != nil {
			errs = append(errs, e.Error())
		}
	}
	if has("email") && s.Get("send_email", "1") == "1" && l.Email != "" && l.EmailStatus != "sent" {
		sub := shared.Tpl(emailSub, vars)
		text := shared.Tpl(withLink(emailBody), vars)
		_, e := sendEmail(ctx, m.a, r.org, s, messaging.Email{LeadID: l.ID, To: l.Email, Subject: sub, Body: text, Attachments: files, Track: true})
		if e != nil {
			errs = append(errs, "Email: "+e.Error())
			note(func(tx pgx.Tx) error {
				tx.Exec(bg, `UPDATE leads SET email_status='failed' WHERE id=$1`, l.ID)
				return app.Event(bg, tx, l.ID, "error", "Email failed: "+e.Error(), "email", nil)
			})
		} else {
			ok++
			note(func(tx pgx.Tx) error {
				_, err := tx.Exec(bg, `UPDATE leads SET email_status='sent' WHERE id=$1`, l.ID)
				return err
			})
		}
	}
	if has("whatsapp") && s.Get("send_whatsapp", "1") == "1" && l.Phone != "" && l.WaStatus != "sent" && l.WaStatus != "delivered" && l.WaStatus != "read" {
		text := shared.Tpl(withLink(waBody), vars)
		var att *messaging.Attachment
		if len(files) > 0 {
			att = &files[0]
		}
		_, link, e := sendWhatsApp(ctx, m.a, r.org, s, messaging.WhatsApp{LeadID: l.ID, Phone: l.Phone, Text: text, Attachment: att})
		// further PDFs (proposal) go as extra messages
		if e == nil && link == "" && len(files) > 1 {
			for i := 1; i < len(files); i++ {
				f := files[i]
				if _, _, e2 := sendWhatsApp(ctx, m.a, r.org, s, messaging.WhatsApp{LeadID: l.ID, Phone: l.Phone, Text: f.Name, Attachment: &f}); e2 != nil {
					errs = append(errs, "WhatsApp: "+e2.Error())
				}
			}
		}
		switch {
		case e != nil:
			errs = append(errs, "WhatsApp: "+e.Error())
			note(func(tx pgx.Tx) error {
				tx.Exec(bg, `UPDATE leads SET wa_status='failed' WHERE id=$1`, l.ID)
				return app.Event(bg, tx, l.ID, "error", "WhatsApp failed: "+e.Error(), "whatsapp", nil)
			})
		case link != "":
			note(func(tx pgx.Tx) error {
				tx.Exec(bg, `UPDATE leads SET wa_status='queued' WHERE id=$1`, l.ID)
				return app.Event(bg, tx, l.ID, "wa_manual", `WhatsApp message ready – click "Send on WhatsApp" on the lead to send it manually`, "whatsapp", map[string]string{"link": link})
			})
		default:
			ok++
			note(func(tx pgx.Tx) error {
				_, err := tx.Exec(bg, `UPDATE leads SET wa_status='sent' WHERE id=$1`, l.ID)
				return err
			})
		}
	}
	var final error
	err = r.tx(bg, func(tx pgx.Tx, _ *settings.Store) error {
		pm := "Done"
		if ok > 0 {
			pm = "Sent"
		}
		var es, ws string
		if err := tx.QueryRow(bg, `UPDATE leads SET progress=100, progress_msg=$2 WHERE id=$1 RETURNING email_status, wa_status`, l.ID, pm).Scan(&es, &ws); err != nil {
			return err
		}
		switch {
		case ok > 0 || es == "sent" || es == "opened" || ws == "sent" || ws == "delivered" || ws == "read" || ws == "manual":
			return setStage(bg, tx, l.ID, "contacted", "")
		case ws == "queued":
			return setStage(bg, tx, l.ID, "report_ready", "")
		case len(errs) > 0:
			final = errors.New(strings.Join(errs, " | "))
			return nil
		default:
			return setStage(bg, tx, l.ID, "report_ready", "No email or WhatsApp number available to contact")
		}
	})
	if err != nil {
		return err
	}
	return final
}

// message: subject and text of the active message template of a type (email, email_noweb, whatsapp, whatsapp_noweb).
var message = func(ctx context.Context, q db.Q, typ string) (string, string) {
	sub, body, err := reports.ActivePrompt(ctx, q, typ)
	if err != nil {
		return "", ""
	}
	return sub, body
}

// files: the report PDF (unless delivery is "link only") and the website proposal PDF for businesses without a website.
func (m *mod) files(ctx context.Context, s *settings.Store, org int64, l *shared.Lead) ([]messaging.Attachment, error) {
	var tok string
	if err := m.a.DB.Org(ctx, org, func(tx pgx.Tx) error {
		var err error
		tok, err = db.Val[string](ctx, tx, `SELECT token FROM reports WHERE lead_id=$1 ORDER BY id DESC LIMIT 1`, l.ID)
		return err
	}); err != nil {
		return nil, err
	}
	if tok == "" {
		return nil, errors.New("No report generated yet")
	}
	base := m.a.Cfg.AppURL()
	var out []messaging.Attachment
	if reportMode(s) != "link" {
		file, name, err := reportPDF(ctx, m.a, org, l.ID)
		var data []byte
		if err == nil {
			data, _ = os.ReadFile(file)
		}
		if len(data) == 0 {
			return nil, errors.New("Report PDF could not be created - regenerate the report")
		}
		if name == "" {
			name = "Audit-Report-" + slugRe.ReplaceAllString(cut(l.Name, 40), "-") + ".pdf"
		}
		out = append(out, messaging.Attachment{Name: name, Mime: "application/pdf", Data: data, URL: base + "/r/" + tok + ".pdf"})
	}
	if pp := s.Get("proposal_pdf", ""); l.Website == "" && pp != "" {
		if data := m.readFile(pp); data != nil {
			out = append(out, messaging.Attachment{Name: "Website-Proposal-" + slugRe.ReplaceAllString(s.Get("company_name", "Agency"), "-") + ".pdf", Mime: "application/pdf", Data: data, URL: base + pp})
		}
	}
	return out, nil
}

// filePath maps a stored file ("/files/{org}/{name}" or a path inside the storage folder) to disk; "" when outside storage.
func (m *mod) filePath(p string) string {
	if p == "" {
		return ""
	}
	root, _ := filepath.Abs(m.a.Cfg.StorageDir)
	var f string
	if filepath.IsAbs(p) && !strings.HasPrefix(p, "/files/") {
		f = filepath.Clean(p)
	} else {
		f = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, "/")))
	}
	if !strings.HasPrefix(f, root+string(filepath.Separator)) {
		return ""
	}
	return f
}

// readFile reads a stored file.
func (m *mod) readFile(p string) []byte {
	f := m.filePath(p)
	if f == "" {
		return nil
	}
	b, err := os.ReadFile(f)
	if err != nil || len(b) == 0 {
		return nil
	}
	return b
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
