package messaging

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/smtp"
	"strings"
	"syscall"
	"time"

	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/settings"
)

const (
	apiTimeout  = 60 * time.Second
	mailTimeout = 40 * time.Second
)

// allowPrivate lets tests talk to a local mail server; always false in the app.
var allowPrivate = false

// dialPublic connects to host:port only when the resolved address is a public one (SMTP / IMAP hosts
// come from users – never let them reach the server's own network). The check runs at connect time
// on the actual IP, so DNS rebinding cannot get around it.
func dialPublic(ctx context.Context, host, port string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 20 * time.Second}
	if !allowPrivate {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			h, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(h)
			if err != nil || !netx.IsPublic(ip) {
				return fmt.Errorf("%s is not a public mail server address", host)
			}
			return nil
		}
	}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		var oe *net.OpError
		if errors.As(err, &oe) && strings.Contains(err.Error(), "not a public") {
			return nil, fmt.Errorf("%s is not a public mail server address", host)
		}
		return nil, err
	}
	return c, nil
}

// loginAuth implements AUTH PLAIN or AUTH LOGIN (Office 365 offers only LOGIN). Unlike net/smtp's
// PlainAuth it does not refuse an unencrypted connection: the user chose "None" explicitly.
type loginAuth struct {
	user, pass string
	login      bool
	step       int
}

func (a *loginAuth) Start(si *smtp.ServerInfo) (string, []byte, error) {
	a.login = true
	for _, m := range si.Auth {
		if strings.EqualFold(m, "PLAIN") {
			a.login = false
		}
	}
	if a.login {
		return "LOGIN", nil, nil
	}
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}

func (a *loginAuth) Next(from []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	if !a.login {
		return nil, errors.New("unexpected server challenge")
	}
	a.step++
	p := strings.ToLower(string(from))
	switch {
	case strings.Contains(p, "user"):
		return []byte(a.user), nil
	case strings.Contains(p, "pass"):
		return []byte(a.pass), nil
	case a.step == 1:
		return []byte(a.user), nil
	default:
		return []byte(a.pass), nil
	}
}

// sendSMTP delivers through the user's mailbox (SSL on 465, STARTTLS on 587, or none).
func sendSMTP(ctx context.Context, s *settings.Store, m *outMail) error {
	host, port, sec := smtpConfig(s)
	return smtpSend(ctx, host, port, sec, smtpUser(s), s.Get("smtp_pass", ""), m)
}

func smtpSend(ctx context.Context, host, port, sec, user, pass string, m *outMail) error {
	if host == "" {
		return errors.New("Enter the SMTP server in Settings → Messaging → Email.")
	}
	raw, err := buildMIME(m, time.Now())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, mailTimeout)
	defer cancel()
	conn, err := dialPublic(ctx, host, port)
	if err != nil {
		return fmt.Errorf("Could not connect to the mail server (%s:%s): %v", host, port, err)
	}
	dl, _ := ctx.Deadline()
	conn.SetDeadline(dl)
	tc := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if sec == "ssl" {
		t := tls.Client(conn, tc)
		if err := t.HandshakeContext(ctx); err != nil {
			conn.Close()
			return fmt.Errorf("SSL connection to %s:%s failed: %v – try TLS / STARTTLS on port 587", host, port, err)
		}
		conn = t
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("Mail server %s:%s did not answer: %v", host, port, err)
	}
	defer c.Close()
	if err := c.Hello(helloName(m.From)); err != nil {
		return err
	}
	if sec == "tls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s does not offer STARTTLS on port %s – choose SSL (465)", host, port)
		}
		if err := c.StartTLS(tc); err != nil {
			return fmt.Errorf("STARTTLS failed: %v", err)
		}
	}
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(&loginAuth{user: user, pass: pass}); err != nil {
			return fmt.Errorf("Mailbox login failed – check the email and (app) password: %v", err)
		}
	}
	if err := c.Mail(m.From); err != nil {
		return fmt.Errorf("The mail server refused the From address %s: %v", m.From, err)
	}
	rcpt := []string{m.To}
	if m.Bcc != "" {
		rcpt = append(rcpt, m.Bcc)
	}
	for _, r := range rcpt {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("The mail server refused the recipient %s: %v", r, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("The mail server did not accept the email: %v", err)
	}
	return c.Quit()
}

func helloName(from string) string {
	if i := strings.LastIndexByte(from, '@'); i >= 0 && i < len(from)-1 {
		return from[i+1:]
	}
	return "localhost"
}
