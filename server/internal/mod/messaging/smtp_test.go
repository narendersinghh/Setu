package messaging

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
)

// fakeSMTP accepts one message (AUTH LOGIN only, like Office 365) and returns the transcript.
func fakeSMTP(t *testing.T) (string, chan string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		var log strings.Builder
		w := func(s string) { c.Write([]byte(s + "\r\n")) }
		w("220 fake")
		data := false
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				break
			}
			log.WriteString(l)
			if data {
				if l == ".\r\n" {
					data = false
					w("250 queued")
				}
				continue
			}
			switch u := strings.ToUpper(l); {
			case strings.HasPrefix(u, "EHLO"):
				w("250-fake\r\n250 AUTH LOGIN")
			case strings.HasPrefix(u, "AUTH LOGIN"):
				w("334 VXNlcm5hbWU6")
			case strings.HasPrefix(u, "MAIL"), strings.HasPrefix(u, "RCPT"):
				w("250 ok")
			case strings.HasPrefix(u, "DATA"):
				data = true
				w("354 go")
			case strings.HasPrefix(u, "QUIT"):
				w("221 bye")
				out <- log.String()
				return
			default: // AUTH LOGIN answers
				if strings.Contains(log.String(), "AUTH LOGIN") && !strings.Contains(log.String(), "MAIL FROM") {
					if strings.Count(log.String(), "\n") == 3 {
						w("334 UGFzc3dvcmQ6")
					} else {
						w("235 ok")
					}
				} else {
					w("500 ?")
				}
			}
		}
		out <- log.String()
	}()
	return ln.Addr().String(), out
}

func TestSMTPSend(t *testing.T) {
	addr, out := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	m := &outMail{From: "me@agency.in", To: "lead@example.com", Bcc: "copy@agency.in", Subject: "Hi", Text: "Hello", HTML: "<p>Hello</p>", MsgID: "<a@b>", Headers: map[string]string{}}
	// the SSRF guard refuses a local server
	if err := smtpSend(context.Background(), host, port, "none", "me@agency.in", "pw", m); err == nil || !strings.Contains(err.Error(), "not a public") {
		t.Fatalf("expected the private address to be refused, got %v", err)
	}
	allowPrivate = true
	defer func() { allowPrivate = false }()
	addr, out = fakeSMTP(t)
	host, port, _ = net.SplitHostPort(addr)
	if err := smtpSend(context.Background(), host, port, "none", "me@agency.in", "pw", m); err != nil {
		t.Fatal(err)
	}
	log := <-out
	for _, want := range []string{"MAIL FROM:<me@agency.in>", "RCPT TO:<lead@example.com>", "RCPT TO:<copy@agency.in>", "Subject: Hi", "Message-Id: <a@b>"} {
		if !strings.Contains(log, want) {
			t.Fatalf("missing %q in\n%s", want, log)
		}
	}
	if strings.Contains(log, "Bcc:") {
		t.Fatal("Bcc header leaked")
	}
}
