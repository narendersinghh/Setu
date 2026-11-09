// Package messaging: email (API services / SMTP), IMAP inbox, WhatsApp, AI voice calls, tracking.
// OWNER: messaging module. The exported API below is a contract used by other modules – keep it.
package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/core"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

type Attachment struct {
	Name string
	Mime string
	Data []byte
	URL  string // public URL of the file (WhatsApp providers that fetch by URL)
}

type Email struct {
	LeadID      int64
	To          string // default: the lead's email
	Subject     string
	Body        string // plain text or simple HTML written by the user / template
	Attachments []Attachment
	Track       bool // add open tracking pixel
	InReplyTo   string
	By          string // optional: name of the user who sent it (journey "Email sent by …")
}

// SendEmail sends an email, stores it in messages (direction out) and writes the lead journey.
// Returns the messages.id. With LeadID 0 the email is only sent (team notifications) and 0 is returned.
func SendEmail(ctx context.Context, a *app.App, orgID int64, s *settings.Store, e Email) (int64, error) {
	to := strings.TrimSpace(e.To)
	var refs string
	if e.LeadID > 0 {
		err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
			l, err := shared.GetLead(ctx, tx, e.LeadID)
			if err != nil {
				return err
			}
			if to == "" {
				to = l.Email
			}
			if e.InReplyTo != "" {
				refs, _ = db.Val[string](ctx, tx, `SELECT in_reply_to FROM messages WHERE channel='email' AND msg_id=$1 ORDER BY id DESC LIMIT 1`, e.InReplyTo)
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, db.ErrNoRows) {
				return 0, errors.New("Lead not found.")
			}
			return 0, err
		}
	}
	if _, err := mail.ParseAddress(to); err != nil || !strings.Contains(to, "@") {
		return 0, errors.New("This lead has no valid email address.")
	}
	if e.LeadID > 0 {
		if err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error { return a.CheckLimit(ctx, tx, orgID, "emails_month", 1) }); err != nil {
			return 0, err
		}
	}
	subject := strings.TrimSpace(e.Subject)
	tk := secure.Token(12)
	dom := helloName(mailFrom(s))
	mid := "<setu." + tk + "@" + dom + ">"
	pixel := ""
	if e.Track && e.LeadID > 0 {
		pixel = a.Cfg.AppURL() + "/t/" + tk + ".gif"
	}
	m := newOutMail(s, to, subject, emailHTML(a, s, e.Body, pixel), plainText(e.Body))
	m.MsgID = mid
	if e.InReplyTo != "" {
		m.Headers["In-Reply-To"] = e.InReplyTo
		m.Headers["References"] = strings.TrimSpace(refs + " " + e.InReplyTo)
	}
	for _, f := range e.Attachments {
		f = fillAttachment(ctx, a, orgID, f)
		if len(f.Data) > 0 {
			m.Files = append(m.Files, f)
		}
	}
	sent, op, sendErr := deliver(ctx, s, m)
	if op == "" {
		op = "send"
	}
	var id int64
	err := a.DB.Org(context.WithoutCancel(ctx), orgID, func(tx pgx.Tx) error {
		app.TrackUsage(ctx, tx, "email", op, 1, 0, 0, sendErr != nil)
		if e.LeadID == 0 {
			return nil
		}
		if sendErr != nil {
			tx.Exec(ctx, `UPDATE leads SET email_status='failed' WHERE id=$1 AND email_status NOT IN ('sent','opened')`, e.LeadID)
			return app.Event(ctx, tx, e.LeadID, "error", "Email failed: "+sendErr.Error(), "email", nil)
		}
		// SMTP uses our Message-ID; services return their own when they share it
		msgID := mid
		if mailMode(s) == "api" {
			msgID = ""
			if regexp.MustCompile(`^<[^>]+@[^>]+>$`).MatchString(sent) {
				msgID = sent
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO messages (lead_id,channel,direction,status,ext_id,msg_id,in_reply_to,addr,subject,body,token)
VALUES ($1,'email','out','sent',$2,$3,$4,$5,$6,$7,$8) RETURNING id`, e.LeadID, cut(sent, 190), cut(msgID, 250), cut(e.InReplyTo, 250),
			cut(strings.ToLower(to), 190), cut(subject, 250), plainText(e.Body), tk).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE leads SET email_status='sent' WHERE id=$1 AND email_status NOT IN ('sent','opened')`, e.LeadID); err != nil {
			return err
		}
		msg := "Email sent to " + to + ": " + subject
		if e.By != "" {
			msg = "Email sent by " + e.By + ": " + subject
		}
		if n := len(m.Files); n > 0 {
			msg += fmt.Sprintf(" (with %d attachment(s))", n)
		}
		return app.Event(ctx, tx, e.LeadID, "sent", msg, "email", map[string]any{"message_id": id})
	})
	if sendErr != nil {
		return 0, sendErr
	}
	return id, err
}

type WhatsApp struct {
	LeadID     int64
	Phone      string // default: the lead's phone
	Text       string
	Attachment *Attachment
}

// SendWhatsApp sends a WhatsApp message (Cloud API / generic provider) or, in manual mode,
// returns a wa.me link for the user to tap. Returns (messages.id, manualLink, error).
func SendWhatsApp(ctx context.Context, a *app.App, orgID int64, s *settings.Store, w WhatsApp) (int64, string, error) {
	var files []Attachment
	if w.Attachment != nil {
		files = []Attachment{fillAttachment(ctx, a, orgID, *w.Attachment)}
	}
	return sendWA(ctx, a, orgID, s, w.LeadID, w.Phone, w.Text, files)
}

type Call struct {
	LeadID       int64
	Task         string
	FirstMessage string
	Language     string
}

// StartCall places an AI voice call with the configured provider. Returns calls.id.
func StartCall(ctx context.Context, a *app.App, orgID int64, s *settings.Store, c Call) (int64, error) {
	return startCall(ctx, a, orgID, s, c)
}

// MarkReplied records the first reply of a lead (stage, replied_at/via, journey), moves scraped
// businesses to Leads when promote_on_reply=1 and emits "replied". Later replies change nothing.
func MarkReplied(ctx context.Context, a *app.App, tx pgx.Tx, orgID, leadID int64, via, msg string) error {
	var replied *time.Time
	if err := tx.QueryRow(ctx, `SELECT replied_at FROM leads WHERE id=$1`, leadID).Scan(&replied); err != nil {
		if errors.Is(err, db.ErrNoRows) {
			return nil
		}
		return err
	}
	if replied != nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE leads SET replied_at=now(), replied_via=$2, stage='replied' WHERE id=$1`, leadID, via); err != nil {
		return err
	}
	text := "Replied via " + via
	if msg != "" {
		text += ": " + cut(msg, 300)
	}
	if err := app.Event(ctx, tx, leadID, "replied", text, via, nil); err != nil {
		return err
	}
	if p, _ := db.Val[string](ctx, tx, `SELECT v FROM settings WHERE k='promote_on_reply'`); p == "1" {
		savepoint(ctx, tx, "promote", func() error { return crm.Promote(ctx, a, tx, orgID, leadID, "contacted") })
	}
	a.Emit(ctx, tx, orgID, "replied", leadID, map[string]any{"via": via})
	return nil
}

// savepoint runs fn so that a failure (e.g. another module's SQL error) does not abort the caller's transaction.
func savepoint(ctx context.Context, tx pgx.Tx, name string, fn func() error) error {
	if _, err := tx.Exec(ctx, "SAVEPOINT "+name); err != nil {
		return err
	}
	if err := fn(); err != nil {
		slog.Warn("messaging", "step", name, "err", err)
		tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+name)
		return err
	}
	_, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+name)
	return err
}

func Register(a *app.App) {
	platformCfg = func() core.MailConfig { return core.PlatformMail(context.Background(), a) }
	a.SystemMail = func(ctx context.Context, to, subject, html string) error {
		cfg := platformCfg()
		m := &outMail{From: cfg.FromEmail, FromName: cfg.FromName, To: to, Subject: subject, HTML: html, Text: plainText(html), Headers: map[string]string{},
			MsgID: "<setu." + secure.Token(12) + "@" + helloName(cfg.FromEmail) + ">"}
		return sendPlatform(ctx, m)
	}
	registerHTTP(a)
	registerInbox(a)
	registerWebhooks(a)
}
