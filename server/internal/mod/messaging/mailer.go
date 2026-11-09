package messaging

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// EmailAPI is an email service used with just an API key (sent over its HTTPS API).
type EmailAPI struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Paste string `json:"paste"` // what to paste
	URL   string `json:"url"`   // where to get it
	Extra string `json:"extra"` // '' | secret | domain
}

// Same list and wording as v1 Mailer::apis().
var EmailAPIs = []EmailAPI{
	{"brevo", "Brevo (Sendinblue)", "API key (starts with xkeysib-)", "https://app.brevo.com/settings/keys/api", ""},
	{"sendgrid", "SendGrid", "API key (starts with SG.)", "https://app.sendgrid.com/settings/api_keys", ""},
	{"mailjet", "Mailjet", "API key", "https://app.mailjet.com/account/apikeys", "secret"},
	{"postmark", "Postmark", "Server API token", "https://account.postmarkapp.com/servers", ""},
	{"resend", "Resend", "API key (starts with re_)", "https://resend.com/api-keys", ""},
	{"zeptomail", "Zoho ZeptoMail", "Send Mail token", "https://www.zoho.com/zeptomail/", ""},
	{"mailgun", "Mailgun", "API key", "https://app.mailgun.com/settings/api_security", "domain"},
	{"smtp2go", "SMTP2GO", "API key (starts with api-)", "https://app.smtp2go.com/settings/api_keys/", ""},
	{"elastic", "Elastic Email", "API key", "https://app.elasticemail.com/marketing/settings/new/manage-api", ""},
}

func emailAPI(k string) *EmailAPI {
	for i := range EmailAPIs {
		if EmailAPIs[i].Key == k {
			return &EmailAPIs[i]
		}
	}
	return nil
}

// SMTPPreset is a mailbox provider: only the email address and its (app) password are asked.
type SMTPPreset struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   string `json:"port"`
	Secure string `json:"secure"` // ssl | tls | none
	Hint   string `json:"hint"`   // password hint
}

// Same list as v1 Mailer::providers().
var SMTPPresets = []SMTPPreset{
	{"custom", "Other / custom SMTP server", "", "", "", "Password"},
	{"gmail", "Gmail / Google Workspace", "smtp.gmail.com", "587", "tls", "App password (Google Account → Security → App passwords)"},
	{"zoho", "Zoho Mail", "smtp.zoho.in", "465", "ssl", "Mailbox or app-specific password"},
	{"outlook", "Microsoft 365 / Outlook", "smtp.office365.com", "587", "tls", "Password or app password"},
	{"hostinger", "Hostinger email", "smtp.hostinger.com", "465", "ssl", "Mailbox password"},
	{"cpanel", "cPanel / my hosting mailbox", "", "465", "ssl", "Mailbox password"},
	{"ses", "Amazon SES (SMTP)", "email-smtp.ap-south-1.amazonaws.com", "587", "tls", "SMTP password from SES → SMTP settings"},
}

func smtpPreset(k string) *SMTPPreset {
	for i := range SMTPPresets {
		if SMTPPresets[i].Key == k {
			return &SMTPPresets[i]
		}
	}
	return nil
}

// ownMode: the organisation's own set-up – "api" (email service) or "smtp" (mailbox).
func ownMode(s *settings.Store) string {
	if s.Get("mail_transport", "") == "smtp" {
		return "smtp"
	}
	return "api"
}

// mailMode: the organisation's own SMTP / email service when it is set up, otherwise "platform" – the
// platform mailer (sendmail like PHP mail, or the platform's SMTP mailbox) used by default.
func mailMode(s *settings.Store) string {
	if t := s.Get("mail_transport", ""); (t == "smtp" || t == "api") && ownMailReady(s) {
		return ownMode(s)
	}
	return "platform"
}

func mailLabel(s *settings.Store) string {
	if mailMode(s) == "platform" {
		return "the default mailer"
	}
	if mailMode(s) == "smtp" {
		return "SMTP"
	}
	if a := emailAPI(s.Get("mail_api", "")); a != nil {
		return a.Name + " API"
	}
	return "API"
}

func smtpUser(s *settings.Store) string { return s.Get("smtp_user", s.Get("from_email", "")) }

func mailFrom(s *settings.Store) string {
	if mailMode(s) == "platform" {
		return platformCfg().FromEmail
	}
	if ownMode(s) == "smtp" {
		return s.Get("from_email", s.Get("smtp_user", ""))
	}
	return s.Get("from_email", "")
}

func fromName(s *settings.Store) string { return s.Get("from_name", s.Get("company_name", "")) }

// smtpConfig fills server settings for known mailboxes (cPanel: mail.<your domain>), like v1 Mailer::preset.
func smtpConfig(s *settings.Store) (host, port, sec string) {
	host, port, sec = s.Get("smtp_host", ""), s.Get("smtp_port", "465"), s.Get("smtp_secure", "ssl")
	k := s.Get("smtp_provider", "custom")
	p := smtpPreset(k)
	if p == nil || k == "custom" || ((k == "ses" || k == "cpanel") && host != "") {
		return
	}
	if k == "cpanel" {
		f := mailFrom(s)
		host = "mail." + f[strings.LastIndexByte(f, '@')+1:]
	} else {
		host = p.Host
	}
	return host, p.Port, p.Secure
}

// MailReady reports whether email can be sent. The platform mailer is always there as the default.
func MailReady(s *settings.Store) bool { return true }

// ownMailReady reports whether the organisation set up its own email (v1 Mailer::ready).
func ownMailReady(s *settings.Store) bool {
	if ownMode(s) == "smtp" {
		h, _, _ := smtpConfig(s)
		return h != "" && smtpUser(s) != "" && mailFrom(s) != ""
	}
	a := emailAPI(s.Get("mail_api", ""))
	return a != nil && s.Get("mail_api_key", "") != "" && mailFrom(s) != "" &&
		(a.Extra != "secret" || s.Get("mail_api_secret", "") != "") && (a.Extra != "domain" || s.Get("mail_api_domain", "") != "")
}

// outMail is one email ready to go out.
type outMail struct {
	From, FromName string
	To             string
	ReplyTo, Bcc   string
	Subject        string
	HTML, Text     string
	MsgID          string            // our Message-ID (SMTP)
	Headers        map[string]string // In-Reply-To / References
	Files          []Attachment
}

func newOutMail(s *settings.Store, to, subject, htmlBody, text string) *outMail {
	rt := s.Get("reply_to", "")
	if rt == "" && mailMode(s) == "platform" {
		rt = s.Get("from_email", s.Get("company_email", "")) // replies reach the organisation, not the platform
	}
	return &outMail{From: mailFrom(s), FromName: fromName(s), To: to, ReplyTo: rt, Bcc: s.Get("bcc_email", ""),
		Subject: subject, HTML: htmlBody, Text: text, Headers: map[string]string{}}
}

// deliver sends through the configured transport. Returns the provider's id (or our Message-ID for SMTP)
// and the usage op name.
func deliver(ctx context.Context, s *settings.Store, m *outMail) (string, string, error) {
	if mailMode(s) == "platform" {
		return m.MsgID, "platform", sendPlatform(ctx, m)
	}
	if mailMode(s) == "smtp" {
		return m.MsgID, "smtp", sendSMTP(ctx, s, m)
	}
	op := "api_" + s.Get("mail_api", "")
	id, err := sendAPI(ctx, s, m, m.Headers)
	// A service that refuses the threading headers still gets the email out
	if err != nil && len(m.Headers) > 0 && regexp.MustCompile(`(?i)header|reply|reference`).MatchString(err.Error()) {
		id, err = sendAPI(ctx, s, m, nil)
	}
	return id, op, err
}

// ---------------------------------------------------------------- email services (HTTPS APIs)

type obj = map[string]any

func withName(name, addr string) string {
	if name == "" {
		return addr
	}
	return name + " <" + addr + ">"
}

type b64file struct{ name, typ, b64 string }

func sendAPI(ctx context.Context, s *settings.Store, m *outMail, hd map[string]string) (string, error) {
	k := s.Get("mail_api", "")
	key := s.Get("mail_api_key", "")
	from, name, rt, bcc, to := m.From, m.FromName, m.ReplyTo, m.Bcc, m.To
	var att []b64file
	for _, f := range m.Files {
		if len(f.Data) == 0 {
			continue
		}
		t := f.Mime
		if t == "" {
			t = "application/octet-stream"
		}
		att = append(att, b64file{f.Name, t, base64.StdEncoding.EncodeToString(f.Data)})
	}
	mapAtt := func(fn func(a b64file) obj) []obj {
		out := make([]obj, 0, len(att))
		for _, a := range att {
			out = append(out, fn(a))
		}
		return out
	}
	var (
		u    string
		body obj
		h    = map[string]string{}
		req  netx.Req
	)
	switch k {
	case "brevo":
		u = "https://api.brevo.com/v3/smtp/email"
		body = obj{"sender": obj{"email": from, "name": name}, "to": []obj{{"email": to}}, "subject": m.Subject, "htmlContent": m.HTML, "textContent": m.Text}
		if rt != "" {
			body["replyTo"] = obj{"email": rt}
		}
		if len(hd) > 0 {
			body["headers"] = hd
		}
		if bcc != "" {
			body["bcc"] = []obj{{"email": bcc}}
		}
		if att != nil {
			body["attachment"] = mapAtt(func(a b64file) obj { return obj{"name": a.name, "content": a.b64} })
		}
		h["api-key"], h["Accept"] = key, "application/json"
	case "sendgrid":
		u = "https://api.sendgrid.com/v3/mail/send"
		p := obj{"to": []obj{{"email": to}}}
		if bcc != "" {
			p["bcc"] = []obj{{"email": bcc}}
		}
		body = obj{"personalizations": []obj{p}, "from": obj{"email": from, "name": name}, "subject": m.Subject,
			"content": []obj{{"type": "text/plain", "value": m.Text}, {"type": "text/html", "value": m.HTML}}}
		if rt != "" {
			body["reply_to"] = obj{"email": rt}
		}
		if len(hd) > 0 {
			body["headers"] = hd
		}
		if att != nil {
			body["attachments"] = mapAtt(func(a b64file) obj { return obj{"content": a.b64, "filename": a.name, "type": a.typ} })
		}
		h["Authorization"] = "Bearer " + key
	case "mailjet":
		u = "https://api.mailjet.com/v3.1/send"
		msg := obj{"From": obj{"Email": from, "Name": name}, "To": []obj{{"Email": to}}, "Subject": m.Subject, "TextPart": m.Text, "HTMLPart": m.HTML}
		if rt != "" {
			msg["ReplyTo"] = obj{"Email": rt}
		}
		if len(hd) > 0 {
			msg["Headers"] = hd
		}
		if bcc != "" {
			msg["Bcc"] = []obj{{"Email": bcc}}
		}
		if att != nil {
			msg["Attachments"] = mapAtt(func(a b64file) obj { return obj{"ContentType": a.typ, "Filename": a.name, "Base64Content": a.b64} })
		}
		body = obj{"Messages": []obj{msg}}
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(key+":"+s.Get("mail_api_secret", "")))
	case "postmark":
		u = "https://api.postmarkapp.com/email"
		body = obj{"From": withName(name, from), "To": to, "Subject": m.Subject, "HtmlBody": m.HTML, "TextBody": m.Text, "MessageStream": "outbound"}
		if rt != "" {
			body["ReplyTo"] = rt
		}
		if len(hd) > 0 {
			var hs []obj
			for hk, hv := range hd {
				hs = append(hs, obj{"Name": hk, "Value": hv})
			}
			body["Headers"] = hs
		}
		if bcc != "" {
			body["Bcc"] = bcc
		}
		if att != nil {
			body["Attachments"] = mapAtt(func(a b64file) obj { return obj{"Name": a.name, "Content": a.b64, "ContentType": a.typ} })
		}
		h["X-Postmark-Server-Token"], h["Accept"] = key, "application/json"
	case "resend":
		u = "https://api.resend.com/emails"
		body = obj{"from": withName(name, from), "to": []string{to}, "subject": m.Subject, "html": m.HTML, "text": m.Text}
		if rt != "" {
			body["reply_to"] = rt
		}
		if len(hd) > 0 {
			body["headers"] = hd
		}
		if bcc != "" {
			body["bcc"] = []string{bcc}
		}
		if att != nil {
			body["attachments"] = mapAtt(func(a b64file) obj { return obj{"filename": a.name, "content": a.b64} })
		}
		h["Authorization"] = "Bearer " + key
	case "zeptomail":
		tld := map[string]string{"com": "com", "eu": "eu"}[s.Get("mail_api_region", "")]
		if tld == "" {
			tld = "in"
		}
		u = "https://api.zeptomail." + tld + "/v1.1/email"
		body = obj{"from": obj{"address": from, "name": name}, "to": []obj{{"email_address": obj{"address": to}}}, "subject": m.Subject, "htmlbody": m.HTML, "textbody": m.Text}
		if rt != "" {
			body["reply_to"] = []obj{{"address": rt}}
		}
		if len(hd) > 0 {
			body["mime_headers"] = hd
		}
		if bcc != "" {
			body["bcc"] = []obj{{"email_address": obj{"address": bcc}}}
		}
		if att != nil {
			body["attachments"] = mapAtt(func(a b64file) obj { return obj{"content": a.b64, "mime_type": a.typ, "name": a.name} })
		}
		if strings.HasPrefix(strings.ToLower(key), "zoho-enczapikey") {
			h["Authorization"] = key
		} else {
			h["Authorization"] = "Zoho-enczapikey " + key
		}
	case "mailgun":
		region := ""
		if s.Get("mail_api_region", "") == "eu" {
			region = "eu."
		}
		u = "https://api." + region + "mailgun.net/v3/" + url.PathEscape(s.Get("mail_api_domain", "")) + "/messages"
		fields := [][2]string{{"from", withName(name, from)}, {"to", to}, {"subject", m.Subject}, {"html", m.HTML}, {"text", m.Text}}
		if rt != "" {
			fields = append(fields, [2]string{"h:Reply-To", rt})
		}
		if bcc != "" {
			fields = append(fields, [2]string{"bcc", bcc})
		}
		for hk, hv := range hd {
			fields = append(fields, [2]string{"h:" + hk, hv})
		}
		mb, ct, err := multipartBody(fields, m.Files, "attachment")
		if err != nil {
			return "", err
		}
		req = netx.Req{Method: "POST", URL: u, Body: mb, Headers: map[string]string{"Content-Type": ct, "Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("api:"+key))}, Timeout: apiTimeout}
	case "smtp2go":
		u = "https://api.smtp2go.com/v3/email/send"
		body = obj{"api_key": key, "sender": withName(name, from), "to": []string{to}, "subject": m.Subject, "html_body": m.HTML, "text_body": m.Text}
		if bcc != "" {
			body["bcc"] = []string{bcc}
		}
		var ch []obj
		if rt != "" {
			ch = append(ch, obj{"header": "Reply-To", "value": rt})
		}
		for hk, hv := range hd {
			ch = append(ch, obj{"header": hk, "value": hv})
		}
		if ch != nil {
			body["custom_headers"] = ch
		}
		if att != nil {
			body["attachments"] = mapAtt(func(a b64file) obj { return obj{"filename": a.name, "fileblob": a.b64, "mimetype": a.typ} })
		}
	case "elastic":
		u = "https://api.elasticemail.com/v4/emails/transactional"
		rc := obj{"To": []string{to}}
		if bcc != "" {
			rc["BCC"] = []string{bcc}
		}
		ct := obj{"From": withName(name, from), "Subject": m.Subject, "Body": []obj{{"ContentType": "HTML", "Content": m.HTML}, {"ContentType": "PlainText", "Content": m.Text}}}
		if rt != "" {
			ct["ReplyTo"] = rt
		}
		if len(hd) > 0 {
			ct["Headers"] = hd
		}
		if att != nil {
			ct["Attachments"] = mapAtt(func(a b64file) obj { return obj{"BinaryContent": a.b64, "Name": a.name, "ContentType": a.typ} })
		}
		body = obj{"Recipients": rc, "Content": ct}
		h["X-ElasticEmail-ApiKey"] = key
	default:
		return "", errors.New("Choose your email service in Settings → Messaging → Email.")
	}
	if req.URL == "" {
		req = netx.Req{Method: "POST", URL: u, JSON: body, Headers: h, Timeout: apiTimeout}
	}
	r, err := netx.Do(ctx, req)
	api := emailAPI(k)
	if err != nil {
		return "", fmt.Errorf("%s refused the email: %v", api.Name, err)
	}
	var d map[string]any
	_ = json.Unmarshal(r.Body, &d)
	ok := r.OK() && !(k == "smtp2go" && dig(d, "data", "error") != nil && dig(d, "data", "error") != "")
	if !ok {
		msg := ""
		for _, p := range [][]any{{"error", "details", 0, "message"}, {"ErrorMessage"}, {"Messages", 0, "Errors", 0, "ErrorMessage"}, {"errors", 0, "message"}, {"data", "error"}, {"message"}, {"Message"}, {"Error"}, {"error"}} {
			if v, ok := dig(d, p...).(string); ok && v != "" {
				msg = v
				break
			}
		}
		if msg == "" {
			msg = cut(string(r.Body), 200)
		}
		tail := ""
		if r.Code == 401 || r.Code == 403 {
			tail = " – check the API key and that the From email / domain is verified in your " + api.Name + " account."
		}
		return "", fmt.Errorf("%s refused the email (HTTP %d): %s%s", api.Name, r.Code, msg, tail)
	}
	var id string
	switch k {
	case "brevo":
		id = str(dig(d, "messageId"))
	case "sendgrid":
		id = "sg-" + secure.Token(8)
	case "mailjet":
		id = str(dig(d, "Messages", 0, "To", 0, "MessageID"))
	case "postmark":
		id = str(dig(d, "MessageID"))
	case "resend":
		id = str(dig(d, "id"))
	case "zeptomail":
		id = str(dig(d, "request_id"))
	case "mailgun":
		id = str(dig(d, "id"))
	case "smtp2go":
		id = str(dig(d, "data", "email_id"))
	case "elastic":
		id = str(dig(d, "MessageID"))
	}
	if id == "" {
		id = "api-" + secure.Token(6)
	}
	return id, nil
}

// ---------------------------------------------------------------- email body (v1 Mailer::html)

var (
	linkRe    = regexp.MustCompile(`(https?://[^\s<]+)`)
	looksHTML = regexp.MustCompile(`(?i)<(p|br|div|table|a|b|strong|em|i|ul|ol|li|h[1-6]|span|img)\b[^>]*>`)
)

// emailHTML wraps the message in the branded layout: text (escaped, links clickable) + signature with
// the logo for light backgrounds, plus the open-tracking pixel.
func emailHTML(a *app.App, s *settings.Store, body, pixel string) string {
	acc := s.Get("brand_accent", "#B8860B")
	var t string
	if looksHTML.MatchString(body) {
		t = body
	} else {
		t = strings.ReplaceAll(html.EscapeString(strings.ReplaceAll(body, "\r\n", "\n")), "\n", "<br>\n")
		t = linkRe.ReplaceAllString(t, `<a href="$1" style="color:`+html.EscapeString(acc)+`">$1</a>`)
	}
	logo, chip := s.Get("company_logo_light", ""), ""
	if logo == "" {
		if logo = s.Get("company_logo", ""); logo != "" {
			chip = "#0B0B0B"
		}
	}
	if strings.HasPrefix(logo, "/") {
		logo = a.Cfg.AppURL() + logo
	}
	var b strings.Builder
	b.WriteString(`<div style="font-family:Arial,Helvetica,sans-serif;font-size:14px;line-height:1.6;color:#1a1a1a;max-width:600px">`)
	b.WriteString(t)
	b.WriteString(`<br><br><table cellpadding="0" cellspacing="0" style="border-top:2px solid ` + html.EscapeString(acc) + `;padding-top:10px"><tr>`)
	if logo != "" {
		st := "padding:0 4px 0 0"
		if chip != "" {
			st = "background:" + chip + ";padding:8px;border-radius:6px"
		}
		b.WriteString(`<td style="` + st + `"><img src="` + html.EscapeString(logo) + `" height="48" alt=""></td>`)
	}
	b.WriteString(`<td style="padding-left:12px;font-size:12px;color:#555"><b style="color:#0b0b0b">` + html.EscapeString(s.Get("company_name", "")) + `</b><br>` +
		html.EscapeString(s.Get("company_phone", "")) + ` · ` + html.EscapeString(s.Get("company_website", "")) + `</td></tr></table>`)
	if pixel != "" {
		b.WriteString(`<img src="` + html.EscapeString(pixel) + `" width="1" height="1" alt="" style="display:block">`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// plainText is the text alternative of a body (HTML bodies are converted).
func plainText(body string) string {
	if looksHTML.MatchString(body) {
		return html2text(body)
	}
	return body
}

// ---------------------------------------------------------------- small helpers

func dig(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k >= len(a) {
				return nil
			}
			v = a[k]
		}
	}
	return v
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%.0f", x)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func ellipsis(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
