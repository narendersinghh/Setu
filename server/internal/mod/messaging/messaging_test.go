package messaging

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"

	gomail "github.com/emersion/go-message/mail"
)

func TestBuildMIME(t *testing.T) {
	m := &outMail{From: "hello@agency.in", FromName: "Agency Ünïcode", To: "lead@example.com", ReplyTo: "reply@agency.in", Bcc: "copy@agency.in",
		Subject: "Your audit – नमस्ते", HTML: "<p>Hi <b>there</b></p>", Text: "Hi there", MsgID: "<setu.abc@agency.in>",
		Headers: map[string]string{"In-Reply-To": "<x@y>", "References": "<w@y> <x@y>"},
		Files:   []Attachment{{Name: "Report.pdf", Mime: "application/pdf", Data: []byte("%PDF-1.4 test")}}}
	raw, err := buildMIME(m, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("copy@agency.in")) {
		t.Fatal("Bcc must not appear in the headers")
	}
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := r.Header.Subject(); s != m.Subject {
		t.Fatalf("subject %q", s)
	}
	if f, _ := r.Header.AddressList("From"); len(f) != 1 || f[0].Address != m.From || f[0].Name != m.FromName {
		t.Fatalf("from %+v", f)
	}
	for k, v := range map[string]string{"Message-Id": m.MsgID, "In-Reply-To": "<x@y>", "References": "<w@y> <x@y>"} {
		if got := r.Header.Get(k); got != v {
			t.Fatalf("%s = %q", k, got)
		}
	}
	var text, html, att string
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p.Body)
		switch h := p.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := h.ContentType()
			if ct == "text/plain" {
				text = string(b)
			} else {
				html = string(b)
			}
		case *gomail.AttachmentHeader:
			fn, _ := h.Filename()
			att = fn + ":" + string(b)
		}
	}
	if text != "Hi there" || html != m.HTML || att != "Report.pdf:%PDF-1.4 test" {
		t.Fatalf("parts text=%q html=%q att=%q", text, html, att)
	}
	// and our own parser reads it back
	p := parseMail(raw)
	if p.From != "hello@agency.in" || p.Subject != m.Subject || p.MsgID != m.MsgID || p.InReplyTo != "<x@y>" || p.Text != "Hi there" {
		t.Fatalf("parsed %+v", p)
	}
}

func TestParseReplyStripsQuote(t *testing.T) {
	raw := "From: \"Ravi\" <Ravi@Shop.in>\r\nTo: hello@agency.in\r\nSubject: =?UTF-8?B?UmU6IEF1ZGl0?=\r\nMessage-ID: <r1@shop.in>\r\nIn-Reply-To: <setu.abc@agency.in>\r\n" +
		"Content-Type: multipart/alternative; boundary=b1\r\n\r\n--b1\r\nContent-Type: text/html; charset=iso-8859-1\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n<p>Yes, call me caf=E9</p><blockquote>old</blockquote>\r\n--b1--\r\n"
	p := parseMail([]byte(raw))
	if p.From != "ravi@shop.in" || p.FromName != "Ravi" || p.Subject != "Re: Audit" || p.Text != "Yes, call me café" {
		t.Fatalf("parsed %+v", p)
	}
	if got := stripQuoted("Thanks!\n\nOn Mon, 1 Jan 2026 at 10:00, Agency wrote:\n> hello"); got != "Thanks!" {
		t.Fatalf("strip %q", got)
	}
}

func TestSignature(t *testing.T) {
	body := []byte(`{"entry":[]}`)
	m := hmac.New(sha256.New, []byte("s3cret"))
	m.Write(body)
	good := "sha256=" + hex.EncodeToString(m.Sum(nil))
	if !validSignature("s3cret", body, good) {
		t.Fatal("valid signature rejected")
	}
	if validSignature("other", body, good) || validSignature("s3cret", []byte(`{"entry":[1]}`), good) || validSignature("s3cret", body, "") {
		t.Fatal("bad signature accepted")
	}
}

func TestCallPayload(t *testing.T) {
	d, err := decodeOrdered(strings.NewReader(`{"type":"end-of-call-report","call":{"status":"ended","metadata":{"hook":"x"}},"message":{"summary":"Interested","durationSeconds":75,"transcript":"yes tomorrow"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := find(d, "call_status", "status"); s != "ended" {
		t.Fatalf("status %q", s)
	}
	if s, _ := find(d, "duration", "durationSeconds"); s != "75" {
		t.Fatalf("duration %q", s)
	}
	if !interestedRe.MatchString("yes we can do a call tomorrow") {
		t.Fatal("interest")
	}
}

func TestWALink(t *testing.T) {
	if got := WALink("919800000000", "Hi & bye"); got != "https://wa.me/919800000000?text=Hi%20%26%20bye" {
		t.Fatal(got)
	}
	if jsonEsc("a\"b\n<c>") != `a\"b\n<c>` {
		t.Fatal(jsonEsc("a\"b\n<c>"))
	}
}
