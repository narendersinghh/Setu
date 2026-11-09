package messaging

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

func waProvider(s *settings.Store) string {
	switch p := s.Get("wa_provider", "manual"); p {
	case "cloud", "generic":
		return p
	}
	return "manual"
}

func phoneNorm(s *settings.Store, p string) string {
	return shared.PhoneNorm(p, s.Get("default_country_code", "91"))
}

// WALink is the click-to-chat link of manual mode.
func WALink(to, text string) string {
	return "https://wa.me/" + to + "?text=" + strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}

// sendWA sends text (+ files) to a phone. In manual mode it returns the wa.me link instead.
func sendWA(ctx context.Context, a *app.App, orgID int64, s *settings.Store, leadID int64, phone, text string, files []Attachment) (int64, string, error) {
	vars := map[string]string{"business_name": "Test", "sender_name": s.Get("sender_name", s.Get("company_name", ""))}
	if leadID > 0 {
		err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
			l, err := shared.GetLead(ctx, tx, leadID)
			if err != nil {
				return err
			}
			if phone == "" {
				phone = l.Phone
			}
			vars = shared.LeadVars(ctx, a, tx, s, l, nil)
			return nil
		})
		if err != nil {
			if errors.Is(err, db.ErrNoRows) {
				return 0, "", errors.New("Lead not found.")
			}
			return 0, "", err
		}
	}
	to := phoneNorm(s, phone)
	if len(to) < 8 {
		return 0, "", errors.New("Invalid WhatsApp number")
	}
	p := waProvider(s)
	if p == "manual" {
		link := WALink(to, text)
		if leadID > 0 {
			err := a.DB.Org(context.WithoutCancel(ctx), orgID, func(tx pgx.Tx) error {
				if _, err := tx.Exec(ctx, `UPDATE leads SET wa_status='queued' WHERE id=$1 AND wa_status IN ('','failed')`, leadID); err != nil {
					return err
				}
				return app.Event(ctx, tx, leadID, "wa_manual", "WhatsApp message ready – click \"Send on WhatsApp\" on the lead to send it manually: "+ellipsis(text, 120), "whatsapp", nil)
			})
			return 0, link, err
		}
		return 0, link, nil
	}
	var (
		ext, svc, op string
		n            int64
		err          error
	)
	if p == "cloud" {
		svc = "whatsapp"
		ext, op, n, err = waCloud(ctx, s, to, text, files, vars)
	} else {
		svc, op = "whatsapp_api", "message"
		ext, n, err = waGeneric(ctx, s, to, text, files)
	}
	var id int64
	derr := a.DB.Org(context.WithoutCancel(ctx), orgID, func(tx pgx.Tx) error {
		app.TrackUsage(ctx, tx, svc, op, max(n, 1), 0, 0, err != nil)
		if p == "cloud" {
			registerWA(ctx, tx, orgID, s)
		}
		if leadID == 0 {
			return nil
		}
		if err != nil {
			tx.Exec(ctx, `UPDATE leads SET wa_status='failed' WHERE id=$1 AND wa_status NOT IN ('delivered','read')`, leadID)
			return app.Event(ctx, tx, leadID, "error", "WhatsApp failed: "+err.Error(), "whatsapp", nil)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO messages (lead_id,channel,direction,status,ext_id,body,addr) VALUES ($1,'whatsapp','out','sent',$2,$3,$4) RETURNING id`,
			leadID, cut(ext, 190), text, to).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE leads SET wa_status='sent' WHERE id=$1`, leadID); err != nil {
			return err
		}
		msg := "WhatsApp message sent to " + to
		if len(files) > 0 {
			msg = "WhatsApp message + PDF sent to " + to
		}
		return app.Event(ctx, tx, leadID, "sent", msg, "whatsapp", map[string]any{"message_id": id})
	})
	if err != nil {
		return 0, "", err
	}
	return id, "", derr
}

type waError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
	ID string `json:"id"`
}

// waCloud: WhatsApp Cloud API. PDFs are uploaded so the file itself is delivered; with an approved
// template the first PDF is its DOCUMENT header, otherwise the text is the caption of the first PDF.
func waCloud(ctx context.Context, s *settings.Store, to, text string, files []Attachment, vars map[string]string) (string, string, int64, error) {
	id, tk := s.Get("wa_phone_id", ""), s.Get("wa_access_token", "")
	if id == "" || tk == "" {
		return "", "message", 0, errors.New("WhatsApp Cloud API not configured")
	}
	base := "https://graph.facebook.com/" + url.PathEscape(s.Get("wa_api_version", "v21.0")) + "/" + url.PathEscape(id)
	h := map[string]string{"Authorization": "Bearer " + tk}
	docs := make([]obj, 0, len(files))
	for _, f := range files {
		if len(f.Data) > 0 {
			mid, err := waUpload(ctx, base, tk, f)
			if err != nil {
				return "", "message", 0, err
			}
			docs = append(docs, obj{"id": mid, "filename": f.Name})
		} else if f.URL != "" {
			docs = append(docs, obj{"link": f.URL, "filename": f.Name})
		}
	}
	post := func(body obj) (string, error) {
		r, err := netx.Do(ctx, netx.Req{Method: "POST", URL: base + "/messages", JSON: body, Headers: h, Timeout: 40e9})
		if err != nil {
			return "", fmt.Errorf("WhatsApp: %v", err)
		}
		var d waError
		json.Unmarshal(r.Body, &d)
		if r.Code != 200 {
			m := d.Error.Message
			if m == "" {
				m = cut(string(r.Body), 300)
			}
			return "", errors.New("WhatsApp: " + m)
		}
		if len(d.Messages) > 0 {
			return d.Messages[0].ID, nil
		}
		return "", nil
	}
	var first, op string
	var err error
	if tpl := s.Get("wa_template", ""); tpl != "" {
		op = "template"
		var params []obj
		for _, x := range strings.Split(s.Get("wa_template_params", "{{business_name}}"), "|") {
			if x == "" {
				continue
			}
			params = append(params, obj{"type": "text", "text": shared.Tpl(strings.TrimSpace(x), vars)})
		}
		var comp []obj
		if len(docs) > 0 {
			comp = append(comp, obj{"type": "header", "parameters": []obj{{"type": "document", "document": docs[0]}}})
			docs = docs[1:]
		}
		if len(params) > 0 {
			comp = append(comp, obj{"type": "body", "parameters": params})
		}
		t := obj{"name": tpl, "language": obj{"code": s.Get("wa_template_lang", "en")}}
		if comp != nil {
			t["components"] = comp
		}
		first, err = post(obj{"messaging_product": "whatsapp", "to": to, "type": "template", "template": t})
	} else {
		op = "message"
		if len(docs) > 0 {
			d := obj{}
			for k, v := range docs[0] {
				d[k] = v
			}
			d["caption"] = cut(text, 1024)
			docs = docs[1:]
			first, err = post(obj{"messaging_product": "whatsapp", "to": to, "type": "document", "document": d})
		} else {
			first, err = post(obj{"messaging_product": "whatsapp", "to": to, "type": "text", "text": obj{"body": text, "preview_url": true}})
		}
	}
	if err != nil {
		return "", op, 1, err
	}
	for _, d := range docs {
		post(obj{"messaging_product": "whatsapp", "to": to, "type": "document", "document": d})
	}
	return first, op, int64(1 + len(docs)), nil
}

func waUpload(ctx context.Context, base, tk string, f Attachment) (string, error) {
	if f.Mime == "" {
		f.Mime = "application/pdf"
	}
	b, ct, err := multipartBody([][2]string{{"messaging_product", "whatsapp"}, {"type", f.Mime}}, []Attachment{f}, "file")
	if err != nil {
		return "", err
	}
	r, err := netx.Do(ctx, netx.Req{Method: "POST", URL: base + "/media", Body: b, Headers: map[string]string{"Authorization": "Bearer " + tk, "Content-Type": ct}, Timeout: 60e9})
	if err != nil {
		return "", fmt.Errorf("WhatsApp media upload failed: %v", err)
	}
	var d waError
	json.Unmarshal(r.Body, &d)
	if r.Code != 200 || d.ID == "" {
		m := d.Error.Message
		if m == "" {
			m = cut(string(r.Body), 200)
		}
		return "", errors.New("WhatsApp media upload failed: " + m)
	}
	return d.ID, nil
}

// jsonEsc escapes a value for use inside a JSON string of a body template.
func jsonEsc(s string) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.Encode(s)
	out := strings.TrimSpace(b.String())
	return out[1 : len(out)-1]
}

func headersJSON(raw string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, errors.New("Custom headers are not valid JSON.")
	}
	for k, v := range m {
		out[k] = str(v)
	}
	return out, nil
}

// waGeneric: any provider HTTP API (Interakt, WATI, AiSensy…), one request per file.
func waGeneric(ctx context.Context, s *settings.Store, to, text string, files []Attachment) (string, int64, error) {
	u := s.Get("wa_api_url", "")
	if u == "" {
		return "", 0, errors.New("WhatsApp API URL not configured")
	}
	hdr, err := headersJSON(s.Get("wa_api_headers", ""))
	if err != nil {
		return "", 0, err
	}
	tplb := s.Get("wa_api_body", "")
	token := s.Get("wa_api_token", "")
	method := strings.ToUpper(s.Get("wa_api_method", "POST"))
	if method != "GET" {
		method = "POST"
	}
	list := files
	if len(list) == 0 {
		list = []Attachment{{}}
	}
	var ids []string
	for i, f := range list {
		msg := ""
		if i == 0 {
			msg = text
		}
		b64 := ""
		if len(f.Data) > 0 && strings.Contains(tplb, "{{file_base64}}") {
			b64 = base64.StdEncoding.EncodeToString(f.Data)
		}
		body := shared.Tpl(tplb, map[string]string{"phone": to, "message": jsonEsc(msg), "file_url": jsonEsc(f.URL), "file_name": jsonEsc(f.Name), "file_base64": b64, "token": token})
		ru := shared.Tpl(u, map[string]string{"phone": to, "token": url.QueryEscape(token)})
		req := netx.Req{Method: method, URL: ru, Headers: hdr, Timeout: 40e9, Public: true}
		if method == "POST" {
			req.Body = strings.NewReader(body)
			if _, ok := hdr["Content-Type"]; !ok {
				req.Headers = map[string]string{"Content-Type": "application/json"}
				for k, v := range hdr {
					req.Headers[k] = v
				}
			}
		}
		r, err := netx.Do(ctx, req)
		if err != nil {
			return "", int64(len(ids)), fmt.Errorf("WhatsApp API: %v", err)
		}
		if !r.OK() {
			return "", int64(len(ids)), fmt.Errorf("WhatsApp API HTTP %d: %s", r.Code, cut(string(r.Body), 200))
		}
		ids = append(ids, cut(string(r.Body), 60))
	}
	return "api:" + secure.SHA256(strings.Join(ids, "|"))[:32], int64(len(ids)), nil
}

// ---------------------------------------------------------------- webhook lookup & signature

func verifyToken(vt string) string { return "wav_" + secure.SHA256(vt)[:40] }

// registerWA maps the workspace's Cloud API phone number id and webhook verify token to it, so
// the shared webhook URL finds the right workspace.
func registerWA(ctx context.Context, q db.Q, orgID int64, s *settings.Store) error {
	if id := strings.TrimSpace(s.Get("wa_phone_id", "")); id != "" {
		q.Exec(ctx, `DELETE FROM public_tokens WHERE org_id=$1 AND kind='wa_phone' AND token<>$2`, orgID, id)
		if err := app.RegisterToken(ctx, q, orgID, "wa_phone", 0, id); err != nil {
			return err
		}
	}
	if vt := s.Get("wa_verify_token", ""); vt != "" {
		t := verifyToken(vt)
		q.Exec(ctx, `DELETE FROM public_tokens WHERE org_id=$1 AND kind='wa_verify' AND token<>$2`, orgID, t)
		return app.RegisterToken(ctx, q, orgID, "wa_verify", 0, t)
	}
	return nil
}

// validSignature checks Meta's X-Hub-Signature-256 header (sha256=HMAC of the raw body with the app secret).
func validSignature(secret string, body []byte, header string) bool {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hmac.Equal([]byte("sha256="+hex.EncodeToString(m.Sum(nil))), []byte(header))
}

var challengeRe = regexp.MustCompile(`[^\w.-]`)

type waHook struct {
	Entry []struct {
		Changes []struct {
			Value struct {
				Metadata struct {
					PhoneNumberID string `json:"phone_number_id"`
				} `json:"metadata"`
				Statuses []struct {
					ID     string `json:"id"`
					Status string `json:"status"`
					Errors []struct {
						Title string `json:"title"`
					} `json:"errors"`
				} `json:"statuses"`
				Messages []struct {
					ID   string `json:"id"`
					From string `json:"from"`
					Type string `json:"type"`
					Text struct {
						Body string `json:"body"`
					} `json:"text"`
					Button struct {
						Text string `json:"text"`
					} `json:"button"`
					Interactive struct {
						ButtonReply struct {
							Title string `json:"title"`
						} `json:"button_reply"`
						ListReply struct {
							Title string `json:"title"`
						} `json:"list_reply"`
					} `json:"interactive"`
				} `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// waWebhook: GET = Meta verification, POST = delivery/read receipts and incoming replies.
func waWebhook(c *app.Ctx) (any, error) {
	a := c.A
	if c.R.Method == http.MethodGet {
		vt := c.Query("hub.verify_token")
		ok := false
		if vt != "" {
			if org, _, err := a.ResolveToken(c, "wa_verify", verifyToken(vt)); err == nil {
				a.OrgTx(c, org, func(tx pgx.Tx, s *settings.Store) error {
					ok = secure.Equal(s.Get("wa_verify_token", ""), vt)
					return nil
				})
			}
		}
		c.Raw()
		c.W.Header().Set("Content-Type", "text/plain")
		if !ok {
			c.W.WriteHeader(http.StatusForbidden)
			io.WriteString(c.W, "Invalid verify token")
			return nil, nil
		}
		io.WriteString(c.W, challengeRe.ReplaceAllString(c.Query("hub.challenge"), ""))
		return nil, nil
	}
	raw, _ := io.ReadAll(io.LimitReader(c.R.Body, 2<<20))
	sig := c.R.Header.Get("X-Hub-Signature-256")
	var d waHook
	json.Unmarshal(raw, &d)
	// group the changes by workspace (one Meta app may serve several numbers)
	orgs := map[int64]bool{}
	for _, en := range d.Entry {
		for _, ch := range en.Changes {
			if pid := ch.Value.Metadata.PhoneNumberID; pid != "" {
				if org, _, err := a.ResolveToken(c, "wa_phone", pid); err == nil {
					orgs[org] = true
				}
			}
		}
	}
	denied, done := 0, 0
	for org := range orgs {
		err := a.OrgTx(c, org, func(tx pgx.Tx, s *settings.Store) error {
			if sec := s.Get("wa_app_secret", ""); sec != "" && !validSignature(sec, raw, sig) {
				denied++
				return nil
			}
			done++
			pid := s.Get("wa_phone_id", "")
			for _, en := range d.Entry {
				for _, ch := range en.Changes {
					if ch.Value.Metadata.PhoneNumberID != pid {
						continue
					}
					v := ch.Value
					for _, st := range v.Statuses {
						if err := waStatus(c, tx, st.ID, st.Status, firstTitle(st.Errors)); err != nil {
							return err
						}
					}
					for _, m := range v.Messages {
						text := m.Text.Body
						if text == "" {
							text = m.Button.Text
						}
						if text == "" {
							text = m.Interactive.ButtonReply.Title + m.Interactive.ListReply.Title
						}
						if text == "" {
							t := m.Type
							if t == "" {
								t = "message"
							}
							text = "[" + t + "]"
						}
						if err := waInbound(c, a, tx, org, s, m.ID, m.From, text); err != nil {
							return err
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	c.Raw()
	if denied > 0 && done == 0 {
		c.W.WriteHeader(http.StatusUnauthorized)
		return nil, nil
	}
	c.W.Header().Set("Content-Type", "text/plain")
	io.WriteString(c.W, "OK")
	return nil, nil
}

func firstTitle(e []struct {
	Title string `json:"title"`
}) string {
	if len(e) > 0 {
		return e[0].Title
	}
	return ""
}

var waRank = map[string]int{"sent": 1, "delivered": 2, "read": 3}

func waStatus(ctx context.Context, tx pgx.Tx, extID, st, errTitle string) error {
	if extID == "" || (st != "delivered" && st != "read" && st != "failed") {
		return nil
	}
	var id, lead int64
	if err := tx.QueryRow(ctx, `SELECT id,lead_id FROM messages WHERE ext_id=$1 AND channel='whatsapp' ORDER BY id DESC LIMIT 1`, extID).Scan(&id, &lead); err != nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET status=$2 WHERE id=$1`, id, st); err != nil {
		return err
	}
	cur, _ := db.Val[string](ctx, tx, `SELECT wa_status FROM leads WHERE id=$1`, lead)
	if st == "failed" || waRank[st] > waRank[cur] {
		tx.Exec(ctx, `UPDATE leads SET wa_status=$2 WHERE id=$1`, lead, st)
	}
	if st == "read" {
		tx.Exec(ctx, `UPDATE leads SET stage='opened' WHERE id=$1 AND stage='contacted'`, lead)
	}
	msg := "WhatsApp message " + st
	if st == "failed" {
		msg += ": " + errTitle
	}
	return app.Event(ctx, tx, lead, st, msg, "whatsapp", nil)
}

var nonDigit = regexp.MustCompile(`\D`)

func waInbound(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, s *settings.Store, extID, from, text string) error {
	from = nonDigit.ReplaceAllString(from, "")
	if len(from) < 8 {
		return nil
	}
	if extID != "" {
		if n, _ := db.Val[int64](ctx, tx, `SELECT count(*) FROM messages WHERE ext_id=$1 AND channel='whatsapp' AND direction='in'`, extID); n > 0 {
			return nil
		}
	}
	tail := from[len(from)-10:]
	type row struct {
		ID    int64  `db:"id"`
		Phone string `db:"phone"`
	}
	ls, err := db.All[row](ctx, tx, `SELECT id,phone FROM leads WHERE wa_status<>'' AND right(regexp_replace(phone,'\D','','g'),10)=$1 ORDER BY id DESC LIMIT 20`, tail)
	if err != nil {
		return err
	}
	for _, l := range ls {
		if n := phoneNorm(s, l.Phone); len(n) < 10 || n[len(n)-10:] != tail {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO messages (lead_id,channel,direction,status,ext_id,body,addr,is_read) VALUES ($1,'whatsapp','in','received',$2,$3,$4,false)`,
			l.ID, cut(extID, 190), text, from); err != nil {
			return err
		}
		if err := MarkReplied(ctx, a, tx, orgID, l.ID, "whatsapp", text); err != nil {
			return err
		}
	}
	return nil
}
