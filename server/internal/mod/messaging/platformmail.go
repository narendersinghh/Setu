package messaging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/core"
)

// platformCfg returns the platform mailer configuration (set in Register).
var platformCfg = func() core.MailConfig { return core.MailConfig{Mode: "sendmail", SendmailPath: "/usr/sbin/sendmail"} }

// sendPlatform delivers with the platform mailer: local sendmail (Postfix, the equivalent of PHP mail)
// or the platform's SMTP mailbox.
func sendPlatform(ctx context.Context, m *outMail) error {
	cfg := platformCfg()
	if m.From == "" {
		m.From = cfg.FromEmail
	}
	if cfg.Mode == "smtp" {
		if cfg.Host == "" || cfg.User == "" {
			return errors.New("The default mailer is not configured – ask the platform admin (Platform → Email).")
		}
		return smtpSend(ctx, cfg.Host, cfg.Port, cfg.Secure, cfg.User, cfg.Pass, m)
	}
	raw, err := buildMIME(m, time.Now())
	if err != nil {
		return err
	}
	rcpt := []string{m.To}
	if m.Bcc != "" {
		rcpt = append(rcpt, m.Bcc)
	}
	for _, r := range rcpt {
		if strings.HasPrefix(r, "-") || strings.ContainsAny(r, " \r\n") {
			return errors.New("invalid recipient address")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// Recipients are passed explicitly (no -t) so Bcc stays hidden; -f sets the envelope sender
	cmd := exec.CommandContext(ctx, cfg.SendmailPath, append([]string{"-i", "-f", m.From, "--"}, rcpt...)...)
	cmd.Stdin = bytes.NewReader(raw)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("sendmail is not installed on the server – install Postfix or set an SMTP mailbox in Platform → Email")
		}
		return fmt.Errorf("sendmail: %v %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}
