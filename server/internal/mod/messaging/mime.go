package messaging

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
)

// buildMIME renders an email: multipart/mixed { multipart/alternative { text, html }, attachments… }.
// Bcc is never written as a header (it is only a RCPT TO recipient).
func buildMIME(m *outMail, now time.Time) ([]byte, error) {
	var h gomail.Header
	h.SetDate(now)
	h.SetAddressList("From", []*gomail.Address{{Name: m.FromName, Address: m.From}})
	h.SetAddressList("To", []*gomail.Address{{Address: m.To}})
	if m.ReplyTo != "" {
		h.SetAddressList("Reply-To", []*gomail.Address{{Address: m.ReplyTo}})
	}
	h.SetSubject(m.Subject)
	if m.MsgID != "" {
		h.Set("Message-Id", m.MsgID)
	}
	for k, v := range m.Headers {
		h.Set(k, oneLine(v))
	}
	h.Set("MIME-Version", "1.0")
	var buf bytes.Buffer
	mw, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	iw, err := mw.CreateInline()
	if err != nil {
		return nil, err
	}
	for _, p := range [][2]string{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		var ph gomail.InlineHeader
		ph.SetContentType(p[0], map[string]string{"charset": "utf-8"})
		w, err := iw.CreatePart(ph)
		if err != nil {
			return nil, err
		}
		io.WriteString(w, p[1])
		w.Close()
	}
	iw.Close()
	for _, f := range m.Files {
		if len(f.Data) == 0 {
			continue
		}
		var ah gomail.AttachmentHeader
		t := f.Mime
		if t == "" {
			t = "application/octet-stream"
		}
		ah.SetContentType(t, nil)
		ah.SetFilename(f.Name)
		w, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		w.Write(f.Data)
		w.Close()
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// multipartBody builds a multipart/form-data body (Mailgun, WhatsApp media upload).
func multipartBody(fields [][2]string, files []Attachment, fileField string) (*bytes.Buffer, string, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, f := range fields {
		if err := w.WriteField(f[0], f[1]); err != nil {
			return nil, "", err
		}
	}
	n := 0
	for _, f := range files {
		if len(f.Data) == 0 {
			continue
		}
		name := fileField
		if n > 0 && fileField == "attachment" {
			name = fileField + "[" + itoa(n) + "]"
		}
		n++
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="`+name+`"; filename="`+strings.ReplaceAll(f.Name, `"`, "")+`"`)
		t := f.Mime
		if t == "" {
			t = "application/octet-stream"
		}
		h.Set("Content-Type", t)
		p, err := w.CreatePart(h)
		if err != nil {
			return nil, "", err
		}
		p.Write(f.Data)
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &b, w.FormDataContentType(), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
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
