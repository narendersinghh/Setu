// Package messaging: email (API services / SMTP), IMAP inbox, WhatsApp, AI voice calls, tracking.
// OWNER: messaging module. The exported API below is a contract used by other modules – keep it.
package messaging

import (
	"context"
	"errors"

	"github.com/narendersinghh/setu/server/internal/app"
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
}

// SendEmail sends an email, stores it in messages (direction out) and writes the lead journey.
// Returns the messages.id.
func SendEmail(ctx context.Context, a *app.App, orgID int64, s *settings.Store, e Email) (int64, error) {
	return 0, errors.New("messaging: not implemented")
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
	return 0, "", errors.New("messaging: not implemented")
}

type Call struct {
	LeadID       int64
	Task         string
	FirstMessage string
	Language     string
}

// StartCall places an AI voice call with the configured provider. Returns calls.id.
func StartCall(ctx context.Context, a *app.App, orgID int64, s *settings.Store, c Call) (int64, error) {
	return 0, errors.New("messaging: not implemented")
}

func Register(a *app.App) {}
