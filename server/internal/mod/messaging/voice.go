package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

type VoiceField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"` // text | secret | textarea
}

type VoiceProvider struct {
	Key    string       `json:"key"`
	Name   string       `json:"name"`
	Desc   string       `json:"desc"`
	Fields []VoiceField `json:"fields"`
}

func vf(k, l string) VoiceField {
	t := "text"
	if strings.HasSuffix(k, "_key") {
		t = "secret"
	} else if k == "voice_headers" || k == "voice_body" {
		t = "textarea"
	}
	return VoiceField{k, l, t}
}

// Same providers, fields and wording as v1 Voice::providers().
var VoiceProviders = []VoiceProvider{
	{"sarvam", "Sarvam AI", "Indian-language voice agents (Hindi, Marathi, Tamil, Telugu, English…). Build the agent in Sarvam, then paste its IDs here.",
		[]VoiceField{vf("sarvam_api_key", "Workspace API key"), vf("sarvam_org", "Org ID"), vf("sarvam_workspace", "Workspace ID"), vf("sarvam_app", "Agent (app) ID"), vf("sarvam_version", "Agent version (number)"), vf("sarvam_connection", "Connection ID"), vf("sarvam_number", "Agent phone number")}},
	{"bland", "Bland AI", "Give the AI a task in plain words – no agent setup needed.",
		[]VoiceField{vf("bland_api_key", "API key"), vf("bland_from", "From number (optional)"), vf("bland_voice", "Voice (e.g. maya, josh – optional)")}},
	{"vapi", "Vapi", "Uses an assistant you built in Vapi.",
		[]VoiceField{vf("vapi_api_key", "Private API key"), vf("vapi_assistant", "Assistant ID"), vf("vapi_phone", "Phone number ID")}},
	{"retell", "Retell AI", "Uses a Retell agent and your Retell phone number.",
		[]VoiceField{vf("retell_api_key", "API key"), vf("retell_agent", "Agent ID"), vf("retell_from", "From number (+91…)")}},
	{"custom", "Other provider (HTTP)", "Any voice API: we POST JSON to your URL. Placeholders: {{phone}} {{task}} {{first_message}} {{webhook}} {{business_name}} {{contact_name}}",
		[]VoiceField{vf("voice_url", "Endpoint URL"), vf("voice_headers", "Headers (JSON)"), vf("voice_body", "Body template (JSON)")}},
}

var VoiceLanguages = []string{"English", "Hindi", "Marathi", "Gujarati", "Tamil", "Telugu", "Kannada", "Malayalam", "Bengali", "Punjabi", "Odia"}

func voiceProvider(k string) *VoiceProvider {
	for i := range VoiceProviders {
		if VoiceProviders[i].Key == k {
			return &VoiceProviders[i]
		}
	}
	return nil
}

func VoiceReady(s *settings.Store) bool { return voiceProvider(s.Get("voice_provider", "")) != nil }

func startCall(ctx context.Context, a *app.App, orgID int64, s *settings.Store, c Call) (int64, error) {
	p := s.Get("voice_provider", "")
	var (
		cid  int64
		tk   = secure.Token(16)
		l    *shared.Lead
		vars map[string]string
	)
	err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		if err := a.CheckLimit(ctx, tx, orgID, "calls_month", 1); err != nil {
			return err
		}
		var err error
		if l, err = shared.GetLead(ctx, tx, c.LeadID); err != nil || l.Phone == "" {
			return errors.New("No phone number")
		}
		if !VoiceReady(s) {
			return errors.New("No AI voice provider connected (Settings → Voice calls)")
		}
		vars = shared.LeadVars(ctx, a, tx, s, l, nil)
		return tx.QueryRow(ctx, `INSERT INTO calls (lead_id,provider,token,status) VALUES ($1,$2,$3,'queued') RETURNING id`, c.LeadID, p, tk).Scan(&cid)
	})
	if err != nil {
		return 0, err
	}
	task := c.Task
	if task == "" {
		task = s.Get("voice_task", "")
	}
	first := c.FirstMessage
	if first == "" {
		first = s.Get("voice_first_message", "")
	}
	lang := c.Language
	if lang == "" {
		lang = s.Get("voice_language", "")
	}
	task, first = shared.Tpl(task, vars), shared.Tpl(first, vars)
	to := "+" + phoneNorm(s, l.Phone)
	hook := a.Cfg.AppURL() + "/webhooks/call/" + tk
	ext, derr := dialVoice(ctx, s, p, to, task, first, lang, hook, vars, l.ID)
	err = a.DB.Org(context.WithoutCancel(ctx), orgID, func(tx pgx.Tx) error {
		app.TrackUsage(ctx, tx, "voice_"+p, "call", 1, 0, 0, derr != nil)
		if derr != nil {
			tx.Exec(ctx, `UPDATE calls SET status='failed', summary=$2, updated_at=now() WHERE id=$1`, cid, cut(derr.Error(), 4000))
			return app.Event(ctx, tx, l.ID, "error", "AI call failed: "+derr.Error(), "call", nil)
		}
		if _, err := tx.Exec(ctx, `UPDATE calls SET ext_id=$2, status='calling', updated_at=now() WHERE id=$1`, cid, cut(ext, 190)); err != nil {
			return err
		}
		return app.Event(ctx, tx, l.ID, "call", "AI voice call started to "+to+" via "+voiceProvider(p).Name, "call", map[string]any{"call_id": cid})
	})
	if derr != nil {
		return cid, derr
	}
	return cid, err
}

func voicePost(ctx context.Context, name, u string, body any, h map[string]string, okFn func(code int, d map[string]any) bool, public bool) (map[string]any, error) {
	req := netx.Req{Method: "POST", URL: u, Headers: h, Timeout: 30 * time.Second, Public: public}
	switch b := body.(type) {
	case string:
		req.Body = strings.NewReader(b)
	default:
		req.JSON = b
	}
	r, err := netx.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}
	var d map[string]any
	json.Unmarshal(r.Body, &d)
	if !okFn(r.Code, d) {
		return nil, fmt.Errorf("%s HTTP %d: %s", name, r.Code, cut(string(r.Body), 300))
	}
	return d, nil
}

func ok200(code int, _ map[string]any) bool { return code == 200 }
func ok2xx(code int, _ map[string]any) bool { return code >= 200 && code < 300 }

func dialVoice(ctx context.Context, s *settings.Store, p, to, task, first, lang, hook string, vars map[string]string, leadID int64) (string, error) {
	S := func(k string) string { return s.Get(k, "") }
	switch p {
	case "sarvam":
		ver, _ := strconv.Atoi(S("sarvam_version"))
		if ver == 0 {
			ver = 1
		}
		ac := obj{"app_id": S("sarvam_app"), "app_version": ver,
			"connection_config": obj{"connection_id": S("sarvam_connection"), "agent_phone_number": S("sarvam_number")},
			"agent_variables":   obj{"business_name": vars["business_name"], "contact_name": vars["contact_name"], "task": task, "agency_name": vars["agency_name"]}}
		if first != "" {
			ov := obj{"initial_bot_message": first}
			if lang != "" {
				ov["initial_language_name"] = lang
			}
			ac["app_overrides"] = ov
		}
		body := obj{"app_config": ac, "user_config": obj{"user_phone_number": to}, "webhook_config": obj{"url": hook, "metadata": obj{"lead_id": leadID}}}
		d, err := voicePost(ctx, "Sarvam", "https://apps.sarvam.ai/api/outbounds/v1/orgs/"+url.PathEscape(S("sarvam_org"))+"/workspaces/"+url.PathEscape(S("sarvam_workspace"))+"/outbounds",
			body, map[string]string{"X-API-Key": S("sarvam_api_key")}, ok200, false)
		if err != nil {
			return "", err
		}
		return str(d["attempt_id"]), nil
	case "bland":
		body := obj{"phone_number": to, "task": task, "first_sentence": first, "webhook": hook, "record": true, "metadata": obj{"lead_id": leadID}}
		if v := S("bland_from"); v != "" {
			body["from"] = v
		}
		if v := S("bland_voice"); v != "" {
			body["voice"] = v
		}
		d, err := voicePost(ctx, "Bland", "https://api.bland.ai/v1/calls", body, map[string]string{"authorization": S("bland_api_key")},
			func(code int, d map[string]any) bool { return code == 200 && str(d["status"]) != "error" }, false)
		if err != nil {
			return "", err
		}
		return str(d["call_id"]), nil
	case "vapi":
		body := obj{"assistantId": S("vapi_assistant"), "phoneNumberId": S("vapi_phone"), "customer": obj{"number": to, "name": cut(vars["contact_name"], 40)},
			"assistantOverrides": obj{"firstMessage": first, "variableValues": obj{"business_name": vars["business_name"], "task": task}, "server": obj{"url": hook}}}
		d, err := voicePost(ctx, "Vapi", "https://api.vapi.ai/call", body, map[string]string{"Authorization": "Bearer " + S("vapi_api_key")}, ok2xx, false)
		if err != nil {
			return "", err
		}
		return str(d["id"]), nil
	case "retell":
		body := obj{"from_number": S("retell_from"), "to_number": to, "override_agent_id": S("retell_agent"),
			"retell_llm_dynamic_variables": obj{"business_name": vars["business_name"], "contact_name": vars["contact_name"], "task": task},
			"metadata":                     obj{"lead_id": leadID, "hook": hook}}
		d, err := voicePost(ctx, "Retell", "https://api.retellai.com/v2/create-phone-call", body, map[string]string{"Authorization": "Bearer " + S("retell_api_key")}, ok2xx, false)
		if err != nil {
			return "", err
		}
		return str(d["call_id"]), nil
	case "custom":
		u := S("voice_url")
		if u == "" {
			return "", errors.New("Enter the voice API endpoint URL in Settings → Voice calls.")
		}
		h, err := headersJSON(S("voice_headers"))
		if err != nil {
			return "", err
		}
		h2 := map[string]string{"Content-Type": "application/json"}
		for k, v := range h {
			h2[k] = v
		}
		ev := map[string]string{}
		for k, v := range vars {
			ev[k] = jsonEsc(v)
		}
		for k, v := range map[string]string{"phone": to, "task": task, "first_message": first, "webhook": hook, "language": lang} {
			ev[k] = jsonEsc(v)
		}
		d, err := voicePost(ctx, "Voice API", u, shared.Tpl(S("voice_body"), ev), h2, ok2xx, true)
		if err != nil {
			return "", err
		}
		for _, k := range []string{"id", "call_id", "attempt_id"} {
			if v := str(d[k]); v != "" {
				return v, nil
			}
		}
		return "", nil
	}
	return "", errors.New("Unknown voice provider")
}

// ---------------------------------------------------------------- call webhook

// onode is a JSON object that keeps its key order (the v1 parser searched keys in document order).
type onode struct {
	keys []string
	vals map[string]any
}

func decodeOrdered(r io.Reader) (any, error) {
	d := json.NewDecoder(r)
	d.UseNumber()
	return decodeValue(d)
}

func decodeValue(d *json.Decoder) (any, error) {
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch v := t.(type) {
	case json.Delim:
		if v == '{' {
			o := &onode{vals: map[string]any{}}
			for d.More() {
				kt, err := d.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				x, err := decodeValue(d)
				if err != nil {
					return nil, err
				}
				if _, dup := o.vals[k]; !dup {
					o.keys = append(o.keys, k)
				}
				o.vals[k] = x
			}
			_, err := d.Token()
			return o, err
		}
		var arr []any
		for d.More() {
			x, err := decodeValue(d)
			if err != nil {
				return nil, err
			}
			arr = append(arr, x)
		}
		_, err := d.Token()
		return arr, err
	}
	return t, nil
}

func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, x != ""
	case json.Number:
		return x.String(), true
	case bool:
		if x {
			return "1", true
		}
		return "", true
	}
	return "", false
}

// find returns the first matching key anywhere in a nested payload (v1 Voice::find).
func find(v any, keys ...string) (string, bool) {
	switch n := v.(type) {
	case *onode:
		for _, k := range keys {
			if s, ok := scalar(n.vals[k]); ok {
				return s, true
			}
		}
		for _, k := range n.keys {
			if s, ok := find(n.vals[k], keys...); ok {
				return s, true
			}
		}
	case []any:
		for _, x := range n {
			if s, ok := find(x, keys...); ok {
				return s, true
			}
		}
	}
	return "", false
}

var (
	callTokenRe  = regexp.MustCompile(`/webhooks/call/([a-f0-9]+)|[?&]t=([a-f0-9]+)`)
	interestedRe = regexp.MustCompile(`(?i)\b(yes|sure|ok|okay|haan|ha|ji)\b.*\b(meeting|call|appointment|tomorrow|monday|tuesday|wednesday|thursday|friday|saturday)\b`)
	interimTypes = map[string]bool{"status-update": true, "speech-update": true, "transcript": true, "conversation-update": true, "call_started": true, "call.started": true}
)

type callRow struct {
	ID       int64  `db:"id"`
	LeadID   int64  `db:"lead_id"`
	Provider string `db:"provider"`
	Duration *int   `db:"duration"`
}

// callWebhook: the provider posts the result when the call ends (/webhooks/call/{token}). Without a
// token in the URL (Retell posts to one agent-level URL) it is taken from metadata.hook.
func callWebhook(c *app.Ctx) (any, error) {
	raw, _ := io.ReadAll(io.LimitReader(c.R.Body, 4<<20))
	d, _ := decodeOrdered(bytes.NewReader(raw))
	tk := strings.ToLower(c.Param("token"))
	if tk == "" {
		if o, ok := d.(*onode); ok {
			for _, path := range [][]string{{"call", "metadata", "hook"}, {"metadata", "hook"}} {
				var v any = o
				for _, k := range path {
					if n, ok := v.(*onode); ok {
						v = n.vals[k]
					} else {
						v = nil
					}
				}
				if s, ok := v.(string); ok {
					if m := callTokenRe.FindStringSubmatch(s); m != nil {
						tk = m[1] + m[2]
						break
					}
				}
			}
		}
	}
	if _, err := c.UseToken("call", tk); err != nil {
		return nil, app.Err(404, "Unknown call")
	}
	if d == nil {
		return map[string]any{"ok": true}, nil
	}
	tx := c.Tx()
	call, err := db.One[callRow](c, tx, `SELECT id,lead_id,provider,duration FROM calls WHERE token=$1`, tk)
	if err != nil {
		return nil, app.Err(404, "Unknown call")
	}
	if err := applyCallResult(c, c.A, tx, c.Org.ID, call, d); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func applyCallResult(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, call callRow, d any) error {
	status, ok := find(d, "call_status", "status", "disconnection_reason", "endedReason", "end_reason", "outcome")
	if !ok {
		status = "completed"
	}
	status = cut(strings.ToLower(status), 20)
	typ, _ := find(d, "type", "event")
	_, hasText := find(d, "transcript", "concatenated_transcript", "summary")
	if interimTypes[strings.ToLower(typ)] && !hasText {
		return nil
	}
	var dur *int
	if s, ok := find(d, "duration", "durationSeconds", "duration_seconds", "call_length", "call_duration"); ok {
		f, _ := strconv.ParseFloat(s, 64)
		if o, ok := d.(*onode); ok {
			if cl, ok := scalar(o.vals["call_length"]); ok { // Bland reports minutes
				m, _ := strconv.ParseFloat(cl, 64)
				f = math.Round(m * 60)
			}
		}
		n := int(f)
		dur = &n
	}
	tr, _ := find(d, "concatenated_transcript", "transcript", "transcript_text")
	sum, _ := find(d, "summary", "call_summary", "analysis_summary")
	rec, _ := find(d, "recording_url", "recordingUrl", "recording", "stereoRecordingUrl")
	if _, err := tx.Exec(ctx, `UPDATE calls SET status=$2, duration=COALESCE($3,duration), transcript=CASE WHEN $4='' THEN transcript ELSE $4 END,
summary=CASE WHEN $5='' THEN summary ELSE $5 END, recording=CASE WHEN $6='' THEN recording ELSE $6 END, updated_at=now() WHERE id=$1`,
		call.ID, status, dur, cut(tr, 60000), cut(sum, 4000), cut(rec, 490)); err != nil {
		return err
	}
	if dur != nil && *dur > 0 && call.Duration == nil {
		app.TrackUsage(ctx, tx, "voice_"+call.Provider, "minutes", 0, int64(*dur), 0, false)
	}
	msg := "AI call " + status
	if dur != nil && *dur > 0 {
		msg += fmt.Sprintf(" · %02d:%02d", *dur/60, *dur%60)
	}
	if sum != "" {
		msg += ": " + ellipsis(sum, 400)
	}
	if err := app.Event(ctx, tx, call.LeadID, "call", msg, "call", map[string]any{"call_id": call.ID}); err != nil {
		return err
	}
	if tr != "" && interestedRe.MatchString(tr+" "+sum) {
		savepoint(ctx, tx, "calltag", func() error { return crm.AddTag(ctx, a, tx, orgID, call.LeadID, "call-interested") })
	}
	if n, _ := db.Exec(ctx, tx, `UPDATE calls SET ended_at=now() WHERE id=$1 AND ended_at IS NULL`, call.ID); n > 0 {
		a.Emit(ctx, tx, orgID, "call_completed", call.LeadID, map[string]any{"call_id": call.ID})
	}
	return nil
}
