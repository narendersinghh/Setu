// Package capture: lead capture from website forms, the embed script, Meta (Facebook/Instagram) Lead
// Ads, Google Ads lead forms and generic webhooks (Zapier, Make, n8n, Pabbly…). Ports v1 Capture.php,
// hook.php and the "Lead capture" settings tab.
package capture

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/secure"
)

// Type of a lead source: label, description, icon, colour (v1 Capture::types).
type Type struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Desc  string `json:"desc"`
	Ic    string `json:"ic"`
	C     string `json:"c"`
	Hint  string `json:"hint"`
}

var types = []Type{
	{"website", "Website form", "Paste a ready-made form or connect your existing form (WordPress, Elementor, Contact Form 7, WPForms, custom HTML) to this URL.", "globe", "blue", "Contact page"},
	{"facebook", "Meta / Facebook Lead Ads", "Instant-form leads from Facebook & Instagram ads arrive in seconds via the Meta webhook.", "target", "violet", "FB – Pune"},
	{"google", "Google Ads lead forms", "Lead form assets in Google Ads send each lead to this webhook.", "zap", "green", "Search campaign"},
	{"webhook", "Webhook / Zapier / Make / n8n", "Send JSON or form data from any tool (Zapier, Make, n8n, Pabbly, IndiaMART, JustDial exports…).", "plug", "teal", "Zapier"},
}

func typeOf(k string) (Type, bool) {
	for _, t := range types {
		if t.Key == k {
			return t, true
		}
	}
	return Type{}, false
}

// Config is sources.config. page_token and app_secret are encrypted with the workspace key.
type Config struct {
	VerifyToken string            `json:"verify_token,omitempty"`
	PageToken   string            `json:"page_token,omitempty"`
	AppSecret   string            `json:"app_secret,omitempty"`
	GoogleKey   string            `json:"google_key,omitempty"`
	ThanksURL   string            `json:"thanks_url,omitempty"`
	ThanksTitle string            `json:"thanks_title,omitempty"`
	ThanksText  string            `json:"thanks_text,omitempty"`
	Map         map[string]string `json:"map,omitempty"`
	Keys        [][3]string       `json:"keys,omitempty"` // learned fields in order: [key, label, sample]
	LastError   *LastError        `json:"last_error,omitempty"`
}

type LastError struct {
	At  time.Time `json:"at"`
	Msg string    `json:"msg"`
}

type source struct {
	ID          int64      `db:"id"`
	Type        string     `db:"type"`
	Name        string     `db:"name"`
	Token       string     `db:"token"`
	Config      Config     `db:"config"`
	Active      bool       `db:"active"`
	Received    int        `db:"received"`
	LastAt      *time.Time `db:"last_at"`
	LastPayload string     `db:"last_payload"`
	CreatedAt   time.Time  `db:"created_at"`
}

const srcCols = `id,type,name,token,config,active,received,last_at,last_payload,created_at`

func Register(a *app.App) {
	a.Get("/capture", list, app.Perm("settings"))
	a.Post("/capture/sources", add, app.Perm("settings"))
	a.Put("/capture/sources/{id}", save, app.Perm("settings"))
	a.Delete("/capture/sources/{id}", del, app.Perm("settings"))
	a.Post("/capture/sources/{id}/test", test, app.Perm("settings"))
	for _, m := range []string{"GET", "POST", "OPTIONS"} {
		a.Public(m, "/hook/{token}", hook)
	}
}

// hookURL is the public endpoint of a source.
func hookURL(a *app.App, token string) string { return a.Cfg.AppURL() + "/hook/" + token }

// ---------------------------------------------------------------- settings API

type mapOpt struct {
	V string `json:"v"`
	L string `json:"l"`
}

var coreOpts = []mapOpt{{"contact_name", "Contact person"}, {"name", "Business / company"}, {"phone", "Phone"}, {"alt_phone", "Alternate phone"}, {"email", "Email"}, {"website", "Website"}, {"city", "City"}, {"address", "Address"}, {"category", "Category"}, {"service", "Service"}, {"deal_value", "Deal value"}, {"tags", "Tags"}, {"message", "Enquiry message"}}

func list(c *app.Ctx) (any, error) {
	tx := c.Tx()
	srcs, err := db.All[source](c, tx, `SELECT `+srcCols+` FROM sources ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	fs, err := crm.Fields(c, tx)
	if err != nil {
		return nil, err
	}
	fk := crm.FieldKeys(fs)
	opts := append([]mapOpt{}, coreOpts...)
	for _, f := range fs {
		opts = append(opts, mapOpt{"cf:" + f.Key, f.Label + " (custom)"})
	}
	opts = append(opts, mapOpt{"ignore", "Ignore – do not save"})
	optLabel := map[string]string{}
	for _, o := range opts {
		optLabel[o.V] = o.L
	}
	out := make([]map[string]any, 0, len(srcs))
	for _, s := range srcs {
		cfg := s.Config
		keys := cfg.Keys
		// not learned yet (e.g. created before learning): read the last payload
		if len(keys) == 0 && s.Type != "google" && s.LastPayload != "" {
			if in, ok := decodeJSON([]byte(s.LastPayload)); ok {
				if _, fb := in.get("entry"); !fb {
					keys = learnKeys(nil, in)
				}
			}
		}
		kl := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			au := crm.Auto(k[0], fk)
			if au == "" {
				au = crm.Auto(crm.Key(k[1]), fk)
			}
			al := "Enquiry notes"
			if au != "" {
				al = optLabel[au]
				if al == "" {
					al = au
				}
			}
			kl = append(kl, map[string]any{"key": k[0], "label": k[1], "sample": k[2], "auto": au, "auto_label": al})
		}
		var lastErr *LastError
		if cfg.LastError != nil && (s.LastAt == nil || !cfg.LastError.At.Before(*s.LastAt)) {
			lastErr = cfg.LastError
		}
		lp := s.LastPayload
		if utf8.RuneCountInString(lp) > 3000 {
			lp = string([]rune(lp)[:3000]) + "…"
		}
		u := hookURL(c.A, s.Token)
		m := cfg.Map
		if m == nil {
			m = map[string]string{}
		}
		thanksTitle := cfg.ThanksTitle
		if thanksTitle == "" {
			thanksTitle = "Thank you!"
		}
		out = append(out, map[string]any{
			"id": s.ID, "type": s.Type, "name": s.Name, "active": s.Active, "received": s.Received, "last_at": s.LastAt,
			"created_at": s.CreatedAt, "url": u, "last_payload": lp, "last_error": lastErr, "keys": kl, "map": m,
			"cfg": map[string]any{
				"verify_token": cfg.VerifyToken, "google_key": cfg.GoogleKey, "thanks_url": cfg.ThanksURL,
				"thanks_title": thanksTitle, "thanks_text": cfg.ThanksText,
				"page_token_set": cfg.PageToken != "", "app_secret_set": cfg.AppSecret != "",
			},
			"html":   formSnippet(u, c.S().Get("brand_accent", "#B8860B")),
			"script": scriptSnippet(u),
		})
	}
	return map[string]any{"types": types, "sources": out, "options": opts}, nil
}

func add(c *app.Ctx) (any, error) {
	var in struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	t, ok := typeOf(in.Type)
	if !ok {
		t, _ = typeOf("webhook")
	}
	cfg := Config{}
	switch t.Key {
	case "facebook":
		cfg.VerifyToken = secure.Token(8)
	case "google":
		cfg.GoogleKey = secure.Token(10)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = t.Label
	}
	if err := c.A.CheckLimit(c, c.Tx(), c.Org.ID, "sources", 1); err != nil {
		return nil, err
	}
	id, err := db.Insert(c, c.Tx(), "sources", map[string]any{"type": t.Key, "name": cut(name, 110), "token": secure.Token(16), "config": cfg})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id, "message": "Lead source created – copy its URL below."}, nil
}

func load(c *app.Ctx) (*source, error) {
	s, err := db.One[source](c, c.Tx(), `SELECT `+srcCols+` FROM sources WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	return &s, nil
}

func save(c *app.Ctx) (any, error) {
	var in struct {
		Name   *string           `json:"name"`
		Active *bool             `json:"active"`
		Cfg    map[string]string `json:"cfg"`
		Map    map[string]string `json:"map"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	s, err := load(c)
	if err != nil {
		return nil, err
	}
	cfg := s.Config
	for k, v := range in.Cfg {
		v = strings.TrimSpace(v)
		switch k {
		case "page_token", "app_secret": // blank = keep the saved value
			if v == "" {
				continue
			}
			enc := c.A.Box.Encrypt(c.Org.ID, v)
			if k == "page_token" {
				cfg.PageToken = enc
			} else {
				cfg.AppSecret = enc
			}
		case "thanks_url":
			cfg.ThanksURL = cut(v, 500)
		case "thanks_title":
			cfg.ThanksTitle = cut(v, 200)
		case "thanks_text":
			cfg.ThanksText = cut(v, 1000)
		}
	}
	if in.Map != nil {
		m := map[string]string{}
		for k, v := range in.Map {
			if k != "" && v != "" {
				m[cut(k, 120)] = cut(v, 120)
			}
		}
		cfg.Map = m
	}
	cfg.LastError = nil
	name, active := s.Name, s.Active
	if in.Name != nil && strings.TrimSpace(*in.Name) != "" {
		name = cut(strings.TrimSpace(*in.Name), 110)
	}
	if in.Active != nil {
		active = *in.Active
	}
	if err := db.Update(c, c.Tx(), "sources", s.ID, map[string]any{"name": name, "active": active, "config": cfg}); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Saved."}, nil
}

func del(c *app.Ctx) (any, error) {
	s, err := load(c)
	if err != nil {
		return nil, err
	}
	tx := c.Tx()
	if _, err := tx.Exec(c, `DELETE FROM sources WHERE id=$1`, s.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(c, `DELETE FROM public_tokens WHERE token=$1 AND kind='source'`, s.Token); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Lead source deleted."}, nil
}

// test sends a sample enquiry through the same pipeline as a real one (v1 "Send test lead").
func test(c *app.Ctx) (any, error) {
	var s source
	var err error
	// own short transactions (like a real webhook), so a failure is recorded on the source
	err = c.A.DB.Org(c, c.Org.ID, func(tx pgxTx) error {
		s, err = db.One[source](c, tx, `SELECT `+srcCols+` FROM sources WHERE id=$1`, c.ParamID("id"))
		return err
	})
	if err != nil {
		return nil, app.NotFound()
	}
	var sample string
	if s.Type == "google" {
		sample = `{"google_key":` + jsonStr(s.Config.GoogleKey) + `,"is_test":true,"campaign_id":"123","user_column_data":[{"column_id":"FULL_NAME","column_name":"Full Name","string_value":"Test Lead (Google)"},{"column_id":"PHONE_NUMBER","column_name":"Phone","string_value":"+919800000000"},{"column_id":"EMAIL","column_name":"Email","string_value":"test.google@example.com"}]}`
	} else {
		sample = `{"name":"Test Lead","phone":"+919800000001","email":"test@example.com","company":"Test Company","message":"This is a test enquiry from Setu settings."}`
	}
	in, _ := decodeJSON([]byte(sample))
	res := process(c, c.A, c.Org.ID, &s, in, []byte(sample), request{test: true})
	if res.err != nil {
		return nil, app.Bad("Test failed – " + res.err.Error())
	}
	if res.status != 0 {
		return nil, app.Bad("Test failed – " + res.msg)
	}
	if res.leads == 0 {
		return nil, app.Bad("Test failed – no lead was created.")
	}
	src := s.Type
	return map[string]any{"ok": true, "message": "Test lead received – open Leads to see it.", "leads_url": "/leads?src=" + src}, nil
}

func jsonStr(s string) string { b, _ := json.Marshal(s); return string(b) }

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) > n {
		return string([]rune(s)[:n])
	}
	return s
}

// ---------------------------------------------------------------- snippets

// formSnippet is a ready-to-paste HTML form for a website source.
func formSnippet(u, accent string) string {
	in := `padding:10px;border:1px solid #ccc;border-radius:6px`
	return `<form action="` + u + `" method="post" style="max-width:420px;font-family:Arial,sans-serif;display:grid;gap:10px">
  <input name="name" placeholder="Your name" required style="` + in + `">
  <input name="phone" placeholder="Phone / WhatsApp" required style="` + in + `">
  <input name="email" type="email" placeholder="Email" style="` + in + `">
  <input name="company" placeholder="Business name" style="` + in + `">
  <textarea name="message" placeholder="How can we help?" style="` + in + `"></textarea>
  <input name="_hp" style="display:none" tabindex="-1" autocomplete="off">
  <input type="hidden" name="redirect" value="">
  <button style="padding:12px;background:` + safeColor(accent) + `;color:#fff;border:0;border-radius:6px;font-weight:bold">Send enquiry</button>
</form>`
}

// scriptSnippet copies every form submission on a page to the source (keeps the form's normal behaviour).
func scriptSnippet(u string) string {
	return "<!-- Setu: send any existing form on this page to your CRM (keeps the form's normal behaviour) -->\n<script src=\"" + u + ".js\" async></script>"
}

func safeColor(c string) string {
	for _, r := range c {
		if !(r == '#' || r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return "#B8860B"
		}
	}
	if c == "" {
		return "#B8860B"
	}
	return c
}
