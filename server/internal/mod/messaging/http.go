package messaging

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/settings"
)

func registerHTTP(a *app.App) {
	leads := app.Perm("leads")
	a.Post("/leads/{id}/email", leadEmail, leads)
	a.Post("/leads/{id}/whatsapp", leadWhatsApp, leads)
	a.Post("/leads/{id}/whatsapp/done", leadWhatsAppDone, leads)
	a.Post("/leads/{id}/call", leadCall, leads)
	a.Post("/leads/{id}/replied", leadReplied, leads)
	a.Get("/leads/{id}/messages", leadMessages, leads)
	a.Get("/leads/{id}/calls", leadCalls, leads)

	a.Get("/emails", emailsList, leads)
	a.Get("/emails/leads", emailsLeads, leads)
	a.Post("/emails/sync", emailsSync, leads)
	a.Get("/emails/{id}", emailsThread, leads)

	a.Get("/meta/messaging", metaMessaging)

	set := app.Perm("settings")
	a.Post("/messaging/test/email", testEmail, set)
	a.Post("/messaging/test/imap", testIMAP, set)
	a.Post("/messaging/test/whatsapp", testWhatsApp, set)
	a.Post("/messaging/test/voice", testVoice, set)
	a.Post("/messaging/whatsapp/register", waRegister, set)
}

func registerWebhooks(a *app.App) {
	a.Public(http.MethodGet, "/webhooks/whatsapp", waWebhook)
	a.Public(http.MethodPost, "/webhooks/whatsapp", waWebhook)
	a.Public(http.MethodPost, "/webhooks/call/{token}", callWebhook)
	a.Public(http.MethodPost, "/webhooks/call", callWebhook)
	a.Public(http.MethodGet, "/t/{token}.gif", trackOpen)
}

// ---------------------------------------------------------------- helpers

// prep loads the settings and a lead the user may open in a short transaction (the handler then
// talks to outside services without holding a transaction open).
func prep(c *app.Ctx, fn func(tx pgx.Tx, s *settings.Store, l *shared.Lead) error) (*settings.Store, *shared.Lead, error) {
	var (
		st *settings.Store
		ld *shared.Lead
	)
	err := c.A.OrgTx(c, c.Org.ID, func(tx pgx.Tx, s *settings.Store) error {
		st = s
		l, err := shared.GetLead(c, tx, c.ParamID("id"))
		if err != nil {
			return app.NotFound()
		}
		if !c.LeadOK(l.IsLead, l.AssignedTo) {
			return app.Forbidden()
		}
		ld = l
		if fn != nil {
			return fn(tx, s, l)
		}
		return nil
	})
	return st, ld, err
}

// asBad turns a sending error into a 400 with its message (the user can act on it).
func asBad(err error) error {
	var he *app.HTTPError
	if err == nil || errors.As(err, &he) {
		return err
	}
	return app.Bad(err.Error())
}

func prompt(c *app.Ctx, tx pgx.Tx, typ string) (string, string) {
	type p struct {
		Subject string `db:"subject"`
		Content string `db:"content"`
	}
	r, err := db.One[p](c, tx, `SELECT subject,content FROM prompts WHERE type=$1 ORDER BY is_active DESC, id LIMIT 1`, typ)
	if err != nil {
		return "", ""
	}
	return r.Subject, r.Content
}

// ---------------------------------------------------------------- lead actions

// POST /api/leads/{id}/email {subject, body, attach_report, to?, reply?}
func leadEmail(c *app.Ctx) (any, error) {
	var in struct {
		Subject      string `json:"subject"`
		Body         string `json:"body"`
		AttachReport bool   `json:"attach_report"`
		To           string `json:"to"`
		Reply        int64  `json:"reply"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	var (
		sub, text, inReply string
		rep                *reportRow
	)
	s, l, err := prep(c, func(tx pgx.Tx, s *settings.Store, l *shared.Lead) error {
		vars := shared.LeadVars(c, c.A, tx, s, l, nil)
		sub, text = strings.TrimSpace(shared.Tpl(in.Subject, vars)), strings.TrimSpace(shared.Tpl(in.Body, vars))
		if in.Reply > 0 {
			inReply, _ = db.Val[string](c, tx, `SELECT msg_id FROM messages WHERE id=$1 AND lead_id=$2 AND channel='email'`, in.Reply, l.ID)
		}
		if in.AttachReport {
			rep, _ = latestReport(c, tx, l.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if sub == "" || text == "" {
		return nil, app.Bad("Write a subject and a message.")
	}
	if !MailReady(s) {
		return nil, app.Bad("Email is not set up yet – Settings → Messaging → Email.")
	}
	var files []Attachment
	if rep != nil {
		if files, err = reportFiles(c, c.A, s, l, rep); err != nil {
			return nil, app.Bad(err.Error())
		}
	}
	to := strings.TrimSpace(in.To)
	id, err := SendEmail(c, c.A, c.Org.ID, s, Email{LeadID: l.ID, To: to, Subject: sub, Body: text, Attachments: files, Track: true, InReplyTo: inReply, By: c.User.Name})
	if err != nil {
		return nil, asBad(err)
	}
	if to == "" {
		to = l.Email
	}
	return map[string]any{"ok": true, "id": id, "message": "Email sent to " + to + "."}, nil
}

// POST /api/leads/{id}/whatsapp {message, attach_report} → {ok, link?}
func leadWhatsApp(c *app.Ctx) (any, error) {
	var in struct {
		Message      string `json:"message"`
		AttachReport bool   `json:"attach_report"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	var (
		text string
		rep  *reportRow
	)
	s, l, err := prep(c, func(tx pgx.Tx, s *settings.Store, l *shared.Lead) error {
		body := in.Message
		if strings.TrimSpace(body) == "" {
			t := "whatsapp"
			if l.Website == "" {
				t = "whatsapp_noweb"
			}
			_, body = prompt(c, tx, t)
		}
		text = strings.TrimSpace(shared.Tpl(body, shared.LeadVars(c, c.A, tx, s, l, nil)))
		if in.AttachReport {
			rep, _ = latestReport(c, tx, l.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if l.Phone == "" {
		return nil, app.Bad("This lead has no phone number.")
	}
	if text == "" {
		return nil, app.Bad("Write a message.")
	}
	var files []Attachment
	if rep != nil && waProvider(s) != "manual" {
		if files, err = reportFiles(c, c.A, s, l, rep); err != nil {
			return nil, app.Bad(err.Error())
		}
	}
	id, link, err := sendWA(c, c.A, c.Org.ID, s, l.ID, "", text, files)
	if err != nil {
		return nil, asBad(err)
	}
	out := map[string]any{"ok": true, "id": id}
	if link != "" {
		out["link"] = link
		if rep != nil {
			out["pdf"] = c.A.Cfg.AppURL() + "/r/" + rep.Token + ".pdf"
		}
	} else {
		out["message"] = "WhatsApp message sent."
	}
	return out, nil
}

// POST /api/leads/{id}/whatsapp/done {message?} – the user sent the message by hand (manual mode).
func leadWhatsAppDone(c *app.Ctx) (any, error) {
	var in struct {
		Message string `json:"message"`
	}
	c.Bind(&in)
	_, _, err := prep(c, func(tx pgx.Tx, s *settings.Store, l *shared.Lead) error {
		body := in.Message
		if body == "" {
			t := "whatsapp"
			if l.Website == "" {
				t = "whatsapp_noweb"
			}
			_, body = prompt(c, tx, t)
			body = shared.Tpl(body, shared.LeadVars(c, c.A, tx, s, l, nil))
		}
		if _, err := tx.Exec(c, `INSERT INTO messages (lead_id,channel,direction,status,body,addr) VALUES ($1,'whatsapp','out','manual',$2,$3)`, l.ID, body, phoneNorm(s, l.Phone)); err != nil {
			return err
		}
		if _, err := tx.Exec(c, `UPDATE leads SET wa_status='manual' WHERE id=$1`, l.ID); err != nil {
			return err
		}
		if !map[string]bool{"opened": true, "replied": true, "won": true, "lost": true}[l.Stage] {
			tx.Exec(c, `UPDATE leads SET stage='contacted' WHERE id=$1`, l.ID)
		}
		if l.IsLead && l.Status == "new" {
			savepoint(c, tx, "status", func() error { return crm.SetStatus(c, c.A, tx, c.Org.ID, l.ID, "contacted", "") })
		}
		return app.Event(c, tx, l.ID, "sent", "WhatsApp sent manually with the PDF", "whatsapp", nil)
	})
	return nil, err
}

// POST /api/leads/{id}/call {task, first_message, language}
func leadCall(c *app.Ctx) (any, error) {
	var in struct {
		Task         string `json:"task"`
		FirstMessage string `json:"first_message"`
		First        string `json:"first"`
		Language     string `json:"language"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if in.FirstMessage == "" {
		in.FirstMessage = in.First
	}
	s, l, err := prep(c, nil)
	if err != nil {
		return nil, err
	}
	id, err := StartCall(c, c.A, c.Org.ID, s, Call{LeadID: l.ID, Task: strings.TrimSpace(in.Task), FirstMessage: strings.TrimSpace(in.FirstMessage), Language: in.Language})
	if err != nil {
		return nil, asBad(err)
	}
	return map[string]any{"ok": true, "id": id, "message": "AI call started – the result will appear in the journey."}, nil
}

// POST /api/leads/{id}/replied {via, msg} – log a reply by hand.
func leadReplied(c *app.Ctx) (any, error) {
	var in struct {
		Via string `json:"via"`
		Msg string `json:"msg"`
	}
	c.Bind(&in)
	if in.Via != "email" && in.Via != "whatsapp" && in.Via != "call" {
		in.Via = "other"
	}
	_, _, err := prep(c, func(tx pgx.Tx, s *settings.Store, l *shared.Lead) error {
		return MarkReplied(c, c.A, tx, c.Org.ID, l.ID, in.Via, strings.TrimSpace(in.Msg))
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Reply logged."}, nil
}

func leadMessages(c *app.Ctx) (any, error) {
	if _, _, err := prep(c, nil); err != nil {
		return nil, err
	}
	q := `SELECT id,channel,direction,status,ext_id,subject,body,addr,is_read,created_at FROM messages WHERE lead_id=$1`
	args := []any{c.ParamID("id")}
	if ch := c.Query("channel"); ch != "" {
		q += ` AND channel=$2`
		args = append(args, ch)
	}
	return db.Maps(c, c.Tx(), q+` ORDER BY created_at DESC, id DESC LIMIT 200`, args...)
}

func leadCalls(c *app.Ctx) (any, error) {
	if _, _, err := prep(c, nil); err != nil {
		return nil, err
	}
	return db.Maps(c, c.Tx(), `SELECT id,provider,ext_id,status,duration,summary,transcript,recording,created_at,updated_at FROM calls WHERE lead_id=$1 ORDER BY id DESC LIMIT 100`, c.ParamID("id"))
}

// ---------------------------------------------------------------- meta & settings tests

func metaMessaging(c *app.Ctx) (any, error) {
	s := c.S()
	return map[string]any{
		"email_apis": EmailAPIs, "smtp_presets": SMTPPresets, "voice_providers": VoiceProviders, "voice_languages": VoiceLanguages,
		"webhooks":   map[string]string{"whatsapp": c.BaseURL() + "/webhooks/whatsapp", "call": c.BaseURL() + "/webhooks/call"},
		"mail_ready": MailReady(s), "mail_label": mailLabel(s), "imap_on": imapOn(s), "wa_provider": waProvider(s), "voice_ready": VoiceReady(s),
		"voice_provider": s.Get("voice_provider", ""), "voice_first_message": s.Get("voice_first_message", ""), "voice_task": s.Get("voice_task", ""),
	}, nil
}

func loadS(c *app.Ctx) (*settings.Store, error) {
	var st *settings.Store
	err := c.A.OrgTx(c, c.Org.ID, func(_ pgx.Tx, s *settings.Store) error { st = s; return nil })
	return st, err
}

func testEmail(c *app.Ctx) (any, error) {
	var in struct {
		To string `json:"to"`
	}
	c.Bind(&in)
	to := strings.TrimSpace(in.To)
	if to == "" {
		to = c.User.Email
	}
	if !strings.Contains(to, "@") {
		return nil, app.Bad("Enter a valid email address for the test.")
	}
	s, err := loadS(c)
	if err != nil {
		return nil, err
	}
	m := newOutMail(s, to, c.A.BrandName(c)+" test email", "<p>Your email settings work.</p>", "Your email settings work.")
	m.MsgID = "<setu.test." + time.Now().Format("20060102150405") + "@" + helloName(mailFrom(s)) + ">"
	if _, _, err := deliver(c, s, m); err != nil {
		return nil, app.Bad(err.Error())
	}
	return map[string]any{"ok": true, "message": "Test email sent to " + to + " using " + mailLabel(s) + ". Check the inbox (and spam folder)."}, nil
}

func testIMAP(c *app.Ctx) (any, error) {
	s, err := loadS(c)
	if err != nil {
		return nil, err
	}
	n, err := testInbox(c, s)
	if err != nil {
		return nil, app.Bad(err.Error())
	}
	return map[string]any{"ok": true, "message": "Inbox connected – " + itoa(n) + " message(s) in the inbox. Replies from leads now appear in the Emails tab."}, nil
}

func testWhatsApp(c *app.Ctx) (any, error) {
	var in struct {
		To string `json:"to"`
	}
	c.Bind(&in)
	if strings.TrimSpace(in.To) == "" {
		return nil, app.Bad("Enter your WhatsApp number.")
	}
	s, err := loadS(c)
	if err != nil {
		return nil, err
	}
	_, link, err := sendWA(c, c.A, c.Org.ID, s, 0, in.To, "Test message from "+c.A.BrandName(c)+" ✅", nil)
	if err != nil {
		return nil, app.Bad(err.Error())
	}
	if link != "" {
		return map[string]any{"ok": true, "link": link, "message": "Manual mode: open WhatsApp to send the test."}, nil
	}
	return map[string]any{"ok": true, "message": "WhatsApp test sent."}, nil
}

func testVoice(c *app.Ctx) (any, error) {
	var in struct {
		To string `json:"to"`
	}
	c.Bind(&in)
	to := strings.TrimSpace(in.To)
	if to == "" {
		return nil, app.Bad("Enter a phone number.")
	}
	var lid int64
	err := c.A.OrgTx(c, c.Org.ID, func(tx pgx.Tx, s *settings.Store) error {
		if !VoiceReady(s) {
			return app.Bad("No AI voice provider connected (Settings → Voice calls)")
		}
		e := savepoint(c, tx, "testlead", func() (err error) {
			lid, err = crm.Create(c, c.A, tx, c.Org.ID, crm.CreateIn{Fields: map[string]string{"contact_name": "Test call", "name": c.A.BrandName(c) + " test", "phone": to}, Source: "manual", Detail: "Voice test", UserID: c.User.ID})
			return err
		})
		if e != nil || lid == 0 { // CRM not available: add the test lead directly
			return tx.QueryRow(c, `INSERT INTO leads (place_id,name,contact_name,phone,is_lead,source,source_detail,lead_at) VALUES ($1,'Setu test','Test call',$2,true,'manual','Voice test',now()) RETURNING id`,
				"manual-"+base64.RawURLEncoding.EncodeToString([]byte(time.Now().String()))[:20], to).Scan(&lid)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s, err := loadS(c)
	if err != nil {
		return nil, err
	}
	if _, err := StartCall(c, c.A, c.Org.ID, s, Call{LeadID: lid}); err != nil {
		return nil, app.Bad(err.Error())
	}
	return map[string]any{"ok": true, "lead_id": lid, "message": "Test call started – it appears in the \"" + c.A.BrandName(c) + " test\" lead journey."}, nil
}

// POST /api/messaging/whatsapp/register – after saving the WhatsApp settings: map the phone number id
// and verify token to this workspace for the shared webhook.
func waRegister(c *app.Ctx) (any, error) {
	if err := registerWA(c, c.Tx(), c.Org.ID, c.S()); err != nil {
		return nil, err
	}
	s := c.S()
	if h := s.Get("wa_api_headers", ""); h != "" {
		if _, err := headersJSON(h); err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
	}
	return map[string]any{"ok": true, "webhook": c.BaseURL() + "/webhooks/whatsapp"}, nil
}

// ---------------------------------------------------------------- open tracking

var gif, _ = base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")

// GET /t/{token}.gif – the email was opened.
func trackOpen(c *app.Ctx) (any, error) {
	tk := strings.ToLower(c.Param("token"))
	if _, err := c.UseToken("msg", tk); err == nil {
		tx := c.Tx()
		var id, lead int64
		var st string
		if tx.QueryRow(c, `SELECT id,lead_id,status FROM messages WHERE token=$1 AND channel='email'`, tk).Scan(&id, &lead, &st) == nil && st == "sent" {
			tx.Exec(c, `UPDATE messages SET status='opened' WHERE id=$1`, id)
			tx.Exec(c, `UPDATE leads SET email_status='opened' WHERE id=$1 AND email_status='sent'`, lead)
			tx.Exec(c, `UPDATE leads SET stage='opened' WHERE id=$1 AND stage='contacted'`, lead)
			app.Event(c, tx, lead, "opened", "Email opened", "email", nil)
		}
	}
	c.Raw()
	c.W.Header().Set("Content-Type", "image/gif")
	c.W.Header().Set("Cache-Control", "no-store")
	c.W.Write(gif)
	return nil, nil
}
