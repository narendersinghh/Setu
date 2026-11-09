package capture

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/secure"
)

type pgxTx = pgx.Tx

var tokenOK = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

func cors(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With")
	h.Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
}

func text(w http.ResponseWriter, code int, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	io.WriteString(w, s)
}

// request carries what the pipeline needs from the HTTP request (empty for the settings test).
type request struct {
	sig      string // X-Hub-Signature-256
	referer  string
	redirect string
	test     bool // "Send test lead" from Settings: no signature
}

// hook is the public capture endpoint: /hook/{token} (form/JSON, Meta verification) and /hook/{token}.js.
func hook(c *app.Ctx) (any, error) {
	c.Raw()
	w, r := c.W, c.R
	cors(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return nil, nil
	}
	tok := c.Param("token")
	js := strings.HasSuffix(tok, ".js")
	tok = strings.TrimSuffix(tok, ".js")
	if !tokenOK.MatchString(tok) {
		text(w, 404, "Unknown or disabled source")
		return nil, nil
	}
	ref, err := c.UseToken("source", tok)
	if err != nil {
		text(w, 404, "Unknown or disabled source")
		return nil, nil
	}
	a, org := c.A, c.Org.ID
	var src source
	err = a.DB.Org(c, org, func(tx pgx.Tx) error {
		src, err = db.One[source](c, tx, `SELECT `+srcCols+` FROM sources WHERE id=$1 AND active`, ref)
		return err
	})
	if err != nil {
		text(w, 404, "Unknown or disabled source")
		return nil, nil
	}
	if js {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		io.WriteString(w, embedJS(hookURL(a, tok)))
		return nil, nil
	}

	// Meta webhook verification (GET ?hub.mode=subscribe&hub.verify_token=…&hub.challenge=…)
	q := r.URL.Query()
	if r.Method == http.MethodGet && src.Type == "facebook" && (q.Has("hub.mode") || q.Has("hub_mode")) {
		vt, ch := first(q, "hub.verify_token", "hub_verify_token"), first(q, "hub.challenge", "hub_challenge")
		if src.Config.VerifyToken != "" && secure.Equal(vt, src.Config.VerifyToken) {
			text(w, 200, ch)
		} else {
			text(w, 403, "Invalid verify token")
		}
		return nil, nil
	}

	ajax := wantsJSON(r, src.Type)
	if !a.Throttle(c, "hook:"+tok+":"+c.IP(), 10, time.Minute) {
		if ajax {
			app.WriteJSON(w, 429, map[string]any{"ok": false, "error": "Too many submissions – please try again in a few minutes."})
		} else {
			page(w, 429, "Please wait a moment", "Too many submissions – please try again in a few minutes.", "")
		}
		return nil, nil
	}

	var raw []byte
	var in omap
	if r.Method == http.MethodGet {
		in = decodeQuery(r.URL.RawQuery).without("k")
		if len(in) == 0 {
			text(w, 200, "Lead capture endpoint is active.")
			return nil, nil
		}
	} else {
		raw, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		in = parseBody(r.Header.Get("Content-Type"), raw)
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt == "multipart/form-data" {
			raw = nil // stored as JSON instead
		}
	}
	rq := request{sig: r.Header.Get("X-Hub-Signature-256"), referer: r.Referer(), redirect: in.str("redirect")}
	res := process(c, a, org, &src, in, raw, rq)
	switch {
	case res.status != 0: // rejected (bad signature / key)
		text(w, res.status, res.msg)
		return nil, nil
	case res.err != nil:
		// Never fail the visitor's form: the real reason is shown in Settings → Lead capture.
		if ajax {
			app.WriteJSON(w, 200, map[string]any{"ok": false, "error": "The enquiry could not be saved."})
			return nil, nil
		}
	case ajax:
		app.WriteJSON(w, 200, map[string]any{"ok": true, "leads": res.leads})
		return nil, nil
	}
	thanks(w, r, &src, rq)
	return nil, nil
}

func first(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			return v
		}
	}
	return ""
}

// wantsJSON: server-to-server calls (Elementor/WordPress, Zapier, Meta, Google) get JSON; a browser
// form post gets the thank-you page.
func wantsJSON(r *http.Request, typ string) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "json") ||
		strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "json") ||
		r.Header.Get("X-Requested-With") != "" || typ != "website" || !strings.Contains(r.UserAgent(), "Mozilla")
}

func parseBody(ct string, raw []byte) omap {
	mt, params, _ := mime.ParseMediaType(ct)
	if mt == "multipart/form-data" && params["boundary"] != "" {
		return decodeMultipart(raw, params["boundary"])
	}
	if t := strings.TrimSpace(string(raw)); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		if o, ok := decodeJSON(raw); ok {
			return o
		}
	}
	return decodeQuery(strings.TrimSpace(string(raw)))
}

// ---------------------------------------------------------------- pipeline

type result struct {
	leads  int
	status int // non-zero: reject the request with this HTTP status
	msg    string
	err    error
}

// fetched is one Meta lead read from the Graph API.
type fetched struct {
	fields omap
	detail string
	extra  map[string]any
}

// process turns one incoming payload into leads. It uses short transactions of its own (never one held
// open across the Graph API call) and records failures on the source.
func process(ctx context.Context, a *app.App, orgID int64, src *source, in omap, raw []byte, rq request) (res result) {
	cfg := src.Config
	defer func() {
		if v := recover(); v != nil {
			slog.Error("capture panic", "org", orgID, "source", src.ID, "err", v, "stack", string(debug.Stack()))
			res = result{err: fmt.Errorf("%v", v)}
		}
		if res.err != nil {
			slog.Warn("capture", "org", orgID, "source", src.ID, "err", res.err)
			recordError(ctx, a, orgID, src.ID, res.err.Error())
		}
	}()

	// checks before anything is stored
	switch src.Type {
	case "facebook":
		if cfg.AppSecret != "" && !rq.test {
			sec, _ := a.Box.Decrypt(orgID, cfg.AppSecret)
			m := hmac.New(sha256.New, []byte(sec))
			m.Write(raw)
			if sec != "" && !secure.Equal("sha256="+hex.EncodeToString(m.Sum(nil)), rq.sig) {
				return result{status: 401, msg: "Invalid signature"}
			}
		}
	case "google":
		if cfg.GoogleKey != "" && !secure.Equal(in.str("google_key"), cfg.GoogleKey) {
			return result{status: 401, msg: "Invalid key"}
		}
	}

	// Meta: read each new lead from the Graph API (outside any transaction)
	var fb []fetched
	var fbErrs []string
	entries := fbEntries(in)
	if src.Type == "facebook" && len(entries) > 0 {
		tok, _ := a.Box.Decrypt(orgID, cfg.PageToken)
		for _, e := range entries {
			if tok == "" {
				fbErrs = append(fbErrs, "Meta lead "+e.leadgen+" received but no Page access token saved")
				continue
			}
			f, err := graphLead(ctx, tok, e)
			if err != nil {
				fbErrs = append(fbErrs, err.Error())
				continue
			}
			fb = append(fb, f)
		}
	}

	// 1) remember what arrived (also when saving the lead fails later)
	payload := string(raw)
	if payload == "" {
		b, _ := json.Marshal(in)
		payload = string(b)
	}
	if len(fb) > 0 {
		b, _ := json.Marshal(fb[len(fb)-1].fields)
		payload = string(b)
	}
	learnFrom := in
	switch {
	case src.Type == "google":
		learnFrom = nil
	case src.Type == "facebook" && len(fb) > 0:
		learnFrom = fb[len(fb)-1].fields
	case src.Type == "facebook" && len(entries) > 0:
		learnFrom = nil
	}
	if learnFrom != nil {
		if _, hp := learnFrom.get("_hp"); hp && learnFrom.str("_hp") != "" {
			learnFrom = nil
		}
	}
	if err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		cols := map[string]any{"last_payload": cut(payload, 20000), "last_at": time.Now()}
		if learnFrom != nil {
			keys := learnKeys(cfg.Keys, learnFrom)
			if !sameKeys(keys, cfg.Keys) {
				// update only the keys inside config (settings may be saved at the same time)
				kb, _ := json.Marshal(keys)
				if _, err := tx.Exec(ctx, `UPDATE sources SET config = jsonb_set(config, '{keys}', $2::jsonb) WHERE id=$1`, src.ID, string(kb)); err != nil {
					return err
				}
				src.Config.Keys = keys
			}
		}
		return db.Update(ctx, tx, "sources", src.ID, cols)
	}); err != nil {
		return result{err: err}
	}
	if len(fbErrs) > 0 && len(fb) == 0 {
		return result{err: errors.New(strings.Join(fbErrs, " · "))}
	}

	// 2) create the leads
	n := 0
	err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		fs, err := crm.Fields(ctx, tx)
		if err != nil {
			return err
		}
		fk := crm.FieldKeys(fs)
		create := func(ci crm.CreateIn) error {
			ci.Source = src.Type
			if _, err := crm.Create(ctx, a, tx, orgID, ci); err != nil {
				return err
			}
			n++
			return nil
		}
		switch src.Type {
		case "facebook":
			for _, f := range fb {
				core, custom, rest := normalize(f.fields, cfg.Map, fk)
				if len(core) == 0 && len(custom) == 0 {
					continue
				}
				addRest(core, rest)
				if err := create(crm.CreateIn{Fields: core, Custom: custom, Detail: f.detail, Extra: f.extra}); err != nil {
					return err
				}
			}
			// already-flattened payloads (Zapier/Make "Facebook Lead Ads" apps)
			if len(entries) == 0 && len(in) > 0 {
				core, custom, rest := normalize(in, cfg.Map, fk)
				if len(core) > 0 {
					addRest(core, rest)
					return create(crm.CreateIn{Fields: core, Custom: custom, Detail: src.Name})
				}
			}
			return nil
		case "google":
			core, detail, extra := google(in)
			if len(core) == 0 {
				return nil
			}
			return create(crm.CreateIn{Fields: core, Detail: detail, Extra: extra})
		}
		if in.str("_hp") != "" { // honeypot: spam
			return nil
		}
		in = in.without("_hp")
		core, custom, rest := normalize(in, cfg.Map, fk)
		if len(core) == 0 && len(custom) == 0 {
			return nil
		}
		addRest(core, rest)
		_, _, meta := crm.Flatten(plain(in).(map[string]any))
		pageURL := meta["page_url"]
		if pageURL == "" {
			pageURL = rq.referer
		}
		detail := src.Name
		if u, err := url.Parse(pageURL); err == nil && u.Host != "" {
			detail += " · " + u.Host + u.Path
		}
		return create(crm.CreateIn{Fields: core, Custom: custom, Detail: cut(detail, 250)})
	})
	if err != nil {
		return result{err: err}
	}
	if n > 0 {
		if err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE sources SET received=received+$2 WHERE id=$1`, src.ID, n)
			return err
		}); err != nil {
			slog.Warn("capture received", "err", err)
		}
	}
	if len(fbErrs) > 0 { // some Meta leads failed: show it in Settings, the others were saved
		recordError(ctx, a, orgID, src.ID, strings.Join(fbErrs, " · "))
	}
	return result{leads: n}
}

// normalize maps fields with crm (mapping + automatic detection incl. custom field keys); rest keeps
// the order the form sent the fields in.
func normalize(in omap, mapping map[string]string, fieldKeys []string) (core, custom map[string]string, rest [][2]string) {
	core, custom, r, _ := crm.NormalizeFields(plain(in).(map[string]any), mapping, fieldKeys)
	if core == nil {
		core = map[string]string{}
	}
	seen := map[string]bool{}
	for _, k := range flatten(in).keys {
		if v, ok := r[k]; ok && !seen[k] {
			rest = append(rest, [2]string{k, v})
			seen[k] = true
		}
	}
	var others []string
	for k := range r {
		if !seen[k] {
			others = append(others, k)
		}
	}
	sort.Strings(others)
	for _, k := range others {
		rest = append(rest, [2]string{k, r[k]})
	}
	return core, custom, rest
}

// addRest appends unmatched fields to the enquiry message ("Which service: SEO").
func addRest(core map[string]string, rest [][2]string) {
	if len(rest) == 0 {
		return
	}
	lines := make([]string, 0, len(rest))
	for _, x := range rest {
		lines = append(lines, ucfirst(strings.ReplaceAll(x[0], "_", " "))+": "+x[1])
	}
	core["message"] = strings.TrimSpace(core["message"] + "\n" + strings.Join(lines, "\n"))
}

func ucfirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// learnKeys remembers the field names a source sends (with a sample value) so they can be mapped in
// Settings. Keeps the order of arrival and the last 40 keys.
func learnKeys(keys [][3]string, in omap) [][3]string {
	f := flatten(in)
	out := append([][3]string{}, keys...)
	for _, nk := range f.keys {
		s := cut(strings.TrimSpace(f.vals[nk]), 60)
		lab := cut(f.labels[nk], 60)
		if lab == "" {
			lab = nk
		}
		found := false
		for i := range out {
			if out[i][0] == nk {
				found = true
				out[i][1], out[i][2] = lab, s
			}
		}
		if !found {
			out = append(out, [3]string{nk, lab, s})
		}
	}
	if len(out) > 40 {
		out = out[len(out)-40:]
	}
	return out
}

func sameKeys(a, b [][3]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func recordError(ctx context.Context, a *app.App, orgID, srcID int64, msg string) {
	e, _ := json.Marshal(LastError{At: time.Now(), Msg: cut(msg, 400)})
	if err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE sources SET config = jsonb_set(config, '{last_error}', $2::jsonb) WHERE id=$1`, srcID, string(e))
		return err
	}); err != nil {
		slog.Warn("capture last_error", "err", err)
	}
}

// ---------------------------------------------------------------- Google Ads lead forms

var googleCols = map[string]string{"FULL_NAME": "contact_name", "FIRST_NAME": "contact_name", "EMAIL": "email", "WORK_EMAIL": "email", "PHONE_NUMBER": "phone", "WORK_PHONE": "phone", "COMPANY_NAME": "name", "CITY": "city", "STREET_ADDRESS": "address"}

func google(in omap) (map[string]string, string, map[string]any) {
	d := map[string]string{}
	var rest []string
	cols, _ := in.get("user_column_data")
	var list []any
	switch x := cols.(type) {
	case []any:
		list = x
	case omap:
		for _, e := range x {
			list = append(list, e.V)
		}
	}
	for _, it := range list {
		c, ok := it.(omap)
		if !ok {
			continue
		}
		id, val := c.str("column_id"), c.str("string_value")
		if id == "LAST_NAME" {
			d["contact_name"] = strings.TrimSpace(d["contact_name"] + " " + val)
			continue
		}
		if f, ok := googleCols[id]; ok && d[f] == "" {
			d[f] = val
			continue
		}
		name := c.str("column_name")
		if name == "" {
			name = id
		}
		rest = append(rest, name+": "+val)
	}
	if len(d) == 0 {
		return nil, "", nil
	}
	if len(rest) > 0 {
		d["message"] = strings.Join(rest, "\n")
	}
	detail := ""
	if t := in.str("is_test"); t != "" && t != "0" && t != "false" {
		detail = "TEST · "
	}
	detail += "Campaign " + in.str("campaign_id")
	if in.str("gcl_id") != "" {
		detail += " · gclid"
	}
	return d, detail, map[string]any{"lead_id": in.str("lead_id"), "form_id": in.str("form_id"), "gcl_id": in.str("gcl_id")}
}

// ---------------------------------------------------------------- Meta Lead Ads

type fbEntry struct{ leadgen, formID string }

func fbEntries(in omap) []fbEntry {
	var out []fbEntry
	ev, _ := in.get("entry")
	for _, en := range asList(ev) {
		eo, _ := en.(omap)
		chs, _ := eo.get("changes")
		for _, ch := range asList(chs) {
			co, _ := ch.(omap)
			v, _ := co.get("value")
			vo, _ := v.(omap)
			if lg := vo.str("leadgen_id"); lg != "" {
				out = append(out, fbEntry{lg, vo.str("form_id")})
			}
		}
	}
	return out
}

func asList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case omap:
		l := make([]any, len(x))
		for i, e := range x {
			l[i] = e.V
		}
		return l
	}
	return nil
}

// graphLead reads one lead (its form answers) from the Meta Graph API.
func graphLead(ctx context.Context, token string, e fbEntry) (fetched, error) {
	u := "https://graph.facebook.com/v21.0/" + url.PathEscape(e.leadgen) + "?" + url.Values{"access_token": {token}, "fields": {"field_data,created_time,ad_name,campaign_name,form_id,platform"}}.Encode()
	r, err := netx.Do(ctx, netx.Req{URL: u, Timeout: 20 * time.Second})
	if err != nil {
		return fetched{}, fmt.Errorf("Meta lead fetch failed: %v", err)
	}
	if r.Code != 200 {
		return fetched{}, fmt.Errorf("Meta lead fetch failed: %s", cut(strings.Join(strings.Fields(string(r.Body)), " "), 300))
	}
	var d struct {
		FieldData []struct {
			Name   string   `json:"name"`
			Values []string `json:"values"`
		} `json:"field_data"`
		AdName       string `json:"ad_name"`
		CampaignName string `json:"campaign_name"`
		Platform     string `json:"platform"`
	}
	if err := json.Unmarshal(r.Body, &d); err != nil {
		return fetched{}, fmt.Errorf("Meta lead fetch failed: %v", err)
	}
	fd := omap{}
	for _, f := range d.FieldData {
		fd.set(f.Name, strings.Join(f.Values, ", "))
	}
	detail := strings.Trim(d.CampaignName+" · "+d.AdName, " ·")
	return fetched{fields: fd, detail: detail, extra: map[string]any{"leadgen_id": e.leadgen, "form_id": e.formID, "platform": d.Platform}}, nil
}

// ---------------------------------------------------------------- visitor pages

// thanks redirects to the saved thank-you page, or to a page on the site the form was posted from
// (no open redirect), or shows a simple thank-you page.
func thanks(w http.ResponseWriter, r *http.Request, src *source, rq request) {
	cfg := src.Config
	to := cfg.ThanksURL
	if rd := rq.redirect; isHTTP(rd) {
		if u, err := url.Parse(rd); err == nil {
			if ref, err := url.Parse(rq.referer); err == nil && ref.Host != "" && strings.EqualFold(ref.Host, u.Host) {
				to = rd
			}
		}
	}
	if isHTTP(to) {
		http.Redirect(w, r, to, http.StatusSeeOther)
		return
	}
	title, msg := cfg.ThanksTitle, cfg.ThanksText
	if title == "" {
		title = "Thank you!"
	}
	if msg == "" {
		msg = "We have received your enquiry and will contact you shortly."
	}
	back := ""
	if isHTTP(rq.referer) {
		back = rq.referer
	}
	page(w, 200, title, msg, back)
}

func isHTTP(u string) bool {
	l := strings.ToLower(u)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

func page(w http.ResponseWriter, code int, title, msg, back string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	b := ""
	if back != "" {
		b = `<a href="` + html.EscapeString(back) + `" style="color:#0B2545">← Back</a>`
	}
	icon := "✅"
	if code != 200 {
		icon = "⏳"
	}
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title></head><body style="font-family:Arial,sans-serif;display:flex;min-height:100vh;align-items:center;justify-content:center;background:#f5f7fb;margin:0"><div style="background:#fff;padding:40px;border-radius:14px;text-align:center;max-width:420px;box-shadow:0 10px 30px rgba(0,0,0,.08)"><div style="font-size:44px">%s</div><h2 style="margin:10px 0">%s</h2><p style="color:#555">%s</p>%s</div></body></html>`,
		html.EscapeString(title), icon, html.EscapeString(title), html.EscapeString(msg), b)
}

// embedJS copies every form submission on the page to Setu and leaves the form working as before.
// Forms that already post to Setu (the ready-made form) are skipped so nothing arrives twice.
func embedJS(u string) string {
	b, _ := json.Marshal(u)
	return `/* Setu lead capture: sends every form on this page to your CRM and keeps the form's normal behaviour */
(function(){var u=` + string(b) + `;if(window.__setuCapture)return;window.__setuCapture=1;
document.addEventListener('submit',function(e){try{var f=e.target;if(!f||f.tagName!=='FORM')return;
var a=f.getAttribute('action')||'';if(a.indexOf(u)===0)return;
var d=new FormData(f);d.append('page_url',location.href);
if(navigator.sendBeacon&&navigator.sendBeacon(u,d))return;
fetch(u,{method:'POST',body:d,mode:'no-cors',keepalive:true});}catch(x){}},true);})();
`
}
