package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// LeadJobs are the background job types that work on one lead (ref_id = lead id).
var LeadJobs = []string{"enrich", "audit", "report", "contact"}

// BusyStages: a lead in one of these stages has work running or queued.
var BusyStages = []string{"queued", "auditing", "reporting", "contacting"}

// JobTitle is the heading of a job in "View process" (v1 Queue::title). Modules that run lead jobs start
// their process log with trace.Step(ctx, ai.JobTitle(j.Type, j.Payload, int(j.Attempts)), "start").
func JobTitle(typ string, payload []byte, attempts int) string {
	a := ""
	if attempts > 1 {
		a = fmt.Sprintf(" (attempt %d)", attempts)
	}
	switch typ {
	case "audit":
		return "Audit started" + a
	case "contact":
		return "Sending the report" + a
	case "enrich":
		return "Looking for contact details on the website" + a
	case "report":
		var p struct {
			Phase string `json:"phase"`
		}
		json.Unmarshal(payload, &p)
		t, ok := map[string]string{"research": "Report · step 1 of 3: market research", "write": "Report · step 2 of 3: AI writes the report",
			"save": "Report · step 2 of 3: saving the AI answer", "pdf": "Report · step 3 of 3: PDF design"}[p.Phase]
		if !ok {
			t = "Report · step 1 of 3: market research"
		}
		return t + a
	}
	return strings.ToUpper(typ[:min(1, len(typ))]) + typ[min(1, len(typ)):] + a
}

// Abort stops everything for these leads (v1 Queue::abort): waiting AI requests and lead jobs are cancelled,
// a running job stops at its next step (abort_flag), and busy leads go back to their last stable stage.
// Returns how many processes were stopped. The modules that start new work reset abort_flag.
func Abort(ctx context.Context, tx pgx.Tx, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_batch SET status='cancelled' WHERE status IN ('queued','sent','running') AND lead_id = ANY($1)`, ids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET status='cancelled', error='Stopped by user', finished_at=now() WHERE status IN ('pending','running') AND type = ANY($2) AND ref_id = ANY($1)`, ids, LeadJobs); err != nil {
		return 0, err
	}
	type row struct {
		ID      int64 `db:"id"`
		Reports bool  `db:"reports"`
	}
	rows, err := db.All[row](ctx, tx, `SELECT l.id, EXISTS(SELECT 1 FROM reports r WHERE r.lead_id=l.id) reports FROM leads l WHERE l.id = ANY($1) AND l.stage = ANY($2)`, ids, BusyStages)
	if err != nil {
		return 0, err
	}
	for _, l := range rows {
		back := "new"
		if l.Reports {
			back = "report_ready"
		}
		if _, err := tx.Exec(ctx, `UPDATE leads SET abort_flag=true, stage=$2, progress=0, progress_msg='Stopped' WHERE id=$1`, l.ID, back); err != nil {
			return 0, err
		}
		if err := app.Event(ctx, tx, l.ID, "error", "Process stopped manually", "", nil); err != nil {
			return 0, err
		}
	}
	return len(rows), nil
}

// StopAll stops every running search, audit, report and scheduled outreach of the workspace.
func StopAll(ctx context.Context, tx pgx.Tx) (int, error) {
	ids, err := db.All[struct {
		ID int64 `db:"id"`
	}](ctx, tx, `SELECT id FROM leads WHERE stage = ANY($1)`, BusyStages)
	if err != nil {
		return 0, err
	}
	lids := make([]int64, len(ids))
	for i, x := range ids {
		lids[i] = x.ID
	}
	n, err := Abort(ctx, tx, lids)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE campaigns SET status='stopped' WHERE status='searching'`); err != nil {
		return 0, err
	}
	// AI requests of leads that are no longer busy (e.g. a single report) stop too
	if _, err := tx.Exec(ctx, `UPDATE ai_batch SET status='cancelled' WHERE status IN ('queued','sent','running')`); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `UPDATE jobs SET status='cancelled', error='Stopped by user', finished_at=now() WHERE status='pending' AND type IN ('search','contact')`)
	return n, err
}

// ---------------------------------------------------------------- process log (v1 Trace::view)

type stepRow struct {
	ID        int64   `db:"id" json:"id"`
	JobID     int64   `db:"job_id" json:"job_id"`
	Kind      string  `db:"kind" json:"kind"`
	Step      string  `db:"step" json:"step"`
	Status    string  `db:"status" json:"status"`
	Detail    string  `db:"detail" json:"detail"`
	Model     string  `db:"model" json:"model"`
	TokensIn  int     `db:"tokens_in" json:"tokens_in"`
	TokensOut int     `db:"tokens_out" json:"tokens_out"`
	Cost      float64 `db:"cost" json:"cost"`
	Ms        int64   `db:"ms" json:"ms"`
	At        int64   `db:"at" json:"at"`
}

type totals struct {
	In   int     `json:"in"`
	Out  int     `json:"out"`
	Cost float64 `json:"cost"`
	AIMs int64   `json:"ai_ms"`
	AI   int     `json:"ai"`
}

// ProcessView returns the latest steps of a lead's background work with AI totals.
func ProcessView(ctx context.Context, q db.Q, leadID int64, limit int) ([]stepRow, totals, error) {
	rows, err := db.All[stepRow](ctx, q, `SELECT id,job_id,kind,step,status,detail,model,tokens_in,tokens_out,cost::float8 cost,ms::bigint ms,extract(epoch FROM created_at)::bigint at
FROM job_steps WHERE lead_id=$1 ORDER BY id DESC LIMIT $2`, leadID, limit)
	var t totals
	if err != nil {
		return nil, t, err
	}
	slices.Reverse(rows)
	now := time.Now().Unix()
	for i := range rows {
		r := &rows[i]
		if r.Status == "run" {
			r.Ms = (now - r.At) * 1000
		}
		if r.Kind == "ai" {
			t.In += r.TokensIn
			t.Out += r.TokensOut
			t.Cost += r.Cost
			t.AIMs += r.Ms
			t.AI++
		}
	}
	t.Cost = round5(t.Cost)
	return rows, t, nil
}

func leadProcess(c *app.Ctx) (any, error) {
	type lead struct {
		ID          int64  `db:"id"`
		Name        string `db:"name"`
		Stage       string `db:"stage"`
		Progress    int16  `db:"progress"`
		ProgressMsg string `db:"progress_msg"`
		LastError   string `db:"last_error"`
		IsLead      bool   `db:"is_lead"`
		AssignedTo  *int64 `db:"assigned_to"`
	}
	l, err := db.One[lead](c, c.Tx(), `SELECT id,name,stage,progress,progress_msg,last_error,is_lead,assigned_to FROM leads WHERE id=$1`, c.ParamID("id"))
	if errors.Is(err, db.ErrNoRows) {
		return nil, app.NotFound()
	}
	if err != nil {
		return nil, err
	}
	if !c.LeadOK(l.IsLead, l.AssignedTo) || (!l.IsLead && !c.Can("prospecting")) {
		return nil, app.Forbidden()
	}
	busy := slices.Contains(BusyStages, l.Stage)
	label := l.Stage
	if s, ok := shared.Stages[l.Stage]; ok {
		label = s[0]
	}
	o := map[string]any{"id": l.ID, "name": l.Name, "stage": l.Stage, "stage_label": label, "busy": busy, "progress": l.Progress,
		"msg": l.ProgressMsg, "error": "", "can_abort": c.Can("settings")}
	if l.Stage == "failed" {
		o["error"] = l.LastError
	}
	if c.Query("lite") != "" {
		return o, nil
	}
	steps, t, err := ProcessView(c, c.Tx(), l.ID, 150)
	if err != nil {
		return nil, err
	}
	// What is queued or running for this lead right now
	type job struct {
		ID       int64     `db:"id"`
		Type     string    `db:"type"`
		Status   string    `db:"status"`
		Attempts int16     `db:"attempts"`
		Payload  []byte    `db:"payload"`
		RunAfter time.Time `db:"run_after"`
	}
	js, err := db.All[job](c, c.Tx(), `SELECT id,type,status,attempts,payload,run_after FROM jobs WHERE ref_id=$1 AND type = ANY($2) AND status IN ('pending','running') ORDER BY id`, l.ID, LeadJobs)
	if err != nil {
		return nil, err
	}
	jl := make([]map[string]any, 0, len(js))
	for _, j := range js {
		jl = append(jl, map[string]any{"id": j.ID, "type": j.Type, "status": j.Status, "title": JobTitle(j.Type, j.Payload, int(j.Attempts)), "run_after": j.RunAfter})
	}
	o["steps"], o["totals"], o["now"], o["jobs"] = steps, t, time.Now().Unix(), jl
	return o, nil
}

// ---------------------------------------------------------------- activity drawer (v1 api.php?do=activity / tick)

func activity(c *app.Ctx) (any, error) {
	tx := c.Tx()
	q := func(sql string, args ...any) []map[string]any {
		m, err := db.Maps(c, tx, sql, args...)
		if err != nil {
			panic(err)
		}
		return m
	}
	out := map[string]any{
		"active":    q(`SELECT id,name,stage,progress,progress_msg,last_error,source FROM leads WHERE stage = ANY($1) ORDER BY last_activity DESC NULLS LAST, id DESC LIMIT 60`, BusyStages),
		"failed":    q(`SELECT id,name,last_error,last_activity FROM leads WHERE stage='failed' AND last_activity>now()-interval '2 days' ORDER BY last_activity DESC LIMIT 15`),
		"search":    q(`SELECT id,keyword,location,found,limit_n FROM campaigns WHERE status='searching' ORDER BY id DESC LIMIT 10`),
		"scheduled": q(`SELECT j.ref_id id,l.name,j.run_after FROM jobs j JOIN leads l ON l.id=j.ref_id WHERE j.type='contact' AND j.status='pending' ORDER BY j.run_after LIMIT 30`),
		"runs":      q(`SELECT r.id,r.status,r.next_at,a.name,l.name lead FROM automation_runs r JOIN automations a ON a.id=r.automation_id JOIN leads l ON l.id=r.lead_id WHERE r.status IN ('running','waiting') ORDER BY r.updated_at DESC LIMIT 30`),
		"now":       time.Now().Unix(),
	}
	b, err := db.Map(c, tx, `SELECT count(*) FILTER (WHERE status='queued')::int queued, count(*) FILTER (WHERE status='running')::int running, count(*) FILTER (WHERE status='sent')::int sent,
  COALESCE(max(mode) FILTER (WHERE status IN ('queued','running','sent')),'') mode FROM ai_batch WHERE status IN ('queued','running','sent')`)
	if err != nil {
		return nil, err
	}
	b["slots"] = Slots(c.S())
	out["bulk"] = b
	t, err := queueStats(c, tx)
	if err != nil {
		return nil, err
	}
	out["queue"] = t
	return out, nil
}

func queueStats(ctx context.Context, q db.Q) (map[string]any, error) {
	m, err := db.Map(ctx, q, `SELECT count(*) FILTER (WHERE status='pending' AND run_after<=now())::int due, count(*) FILTER (WHERE status='pending' AND run_after>now())::int later,
  count(*) FILTER (WHERE status='running')::int running, ceil(extract(epoch FROM min(run_after) FILTER (WHERE status='pending' AND run_after>now()) - now()))::int next
FROM jobs WHERE status IN ('pending','running')`)
	if m == nil {
		m = map[string]any{"due": 0, "later": 0, "running": 0, "next": nil}
	}
	return m, err
}

// tick: counts for the activity button badge (v1 api.php?do=tick).
func tick(c *app.Ctx) (any, error) {
	m, err := queueStats(c, c.Tx())
	if err != nil {
		return nil, err
	}
	busy, err := db.Val[int](c, c.Tx(), `SELECT count(*)::int FROM leads WHERE stage = ANY($1)`, BusyStages)
	m["busy"] = busy
	return m, err
}

func stopAll(c *app.Ctx) (any, error) {
	n, err := StopAll(c, c.Tx())
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "n": n}, nil
}

func abortLeads(c *app.Ctx) (any, error) {
	var in struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if in.All {
		return stopAll(c)
	}
	n, err := Abort(c, c.Tx(), in.IDs)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "n": n}, nil
}

func unschedule(c *app.Ctx) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	n, err := db.Exec(c, c.Tx(), `UPDATE jobs SET status='cancelled', error='Stopped by user', finished_at=now() WHERE type='contact' AND status='pending' AND ref_id=$1`, in.ID)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		if err := app.Event(c, c.Tx(), in.ID, "note", "Scheduled outreach cancelled", "", nil); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func cancelRun(c *app.Ctx) (any, error) {
	var in struct {
		ID int64 `json:"id"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	tx := c.Tx()
	if _, err := tx.Exec(c, `UPDATE jobs SET status='cancelled', error='Cancelled manually', finished_at=now() WHERE type='auto' AND ref_id=$1 AND status='pending'`, in.ID); err != nil {
		return nil, err
	}
	_, err := tx.Exec(c, `UPDATE automation_runs SET status='cancelled', next_at=NULL, updated_at=now(),
  log = log || jsonb_build_array(jsonb_build_object('at', now(), 'msg', 'Cancelled manually')) WHERE id=$1 AND status IN ('running','waiting')`, in.ID)
	return nil, err
}

// ---------------------------------------------------------------- AI connection page (v1 ai.php)

func connView(s *settings.Store) map[string]any {
	ps := make([]map[string]any, 0, len(Providers))
	for _, p := range Providers {
		ps = append(ps, map[string]any{"key": p.Key, "name": p.Name, "url": p.URL, "models": p.Models, "steps": p.Steps, "note": p.Note, "suggest": p.Suggest,
			"has_key": s.Get(SetKeyPrefix+p.Key, "") != "", "model": Model(s, p.Key)})
	}
	return map[string]any{"provider": ActiveProvider(s), "providers": ps, "research": ResearchOn(s), "custom_url": s.Get(SetCustomURL, ""),
		"anthropic_base": s.Get(SetAnthropic, ""), "research_cache_days": s.Int("research_cache_days", 30), "configured": Configured(s)}
}

func getConn(c *app.Ctx) (any, error) { return connView(c.S()), nil }

type connIn struct {
	Provider      string  `json:"provider"`
	Key           string  `json:"key"`
	Clear         bool    `json:"clear"`
	Model         *string `json:"model"`
	URL           *string `json:"url"`
	AnthropicBase *string `json:"anthropic_base"`
	Research      *bool   `json:"research"`
}

func saveConnTx(ctx context.Context, tx pgx.Tx, s *settings.Store, in connIn) error {
	if _, ok := providerByKey(in.Provider); !ok {
		return app.Bad("Choose an AI provider.")
	}
	p := in.Provider
	set := func(k string, v any) error { return s.Set(ctx, tx, k, v) }
	if k := strings.TrimSpace(in.Key); k != "" && k != settings.Masked {
		if err := set(SetKeyPrefix+p, k); err != nil {
			return err
		}
	}
	if in.Clear {
		if err := set(SetKeyPrefix+p, ""); err != nil {
			return err
		}
	}
	if in.Model != nil {
		if err := set(SetModelPrefix+p, strings.TrimSpace(*in.Model)); err != nil {
			return err
		}
	}
	for _, u := range []struct {
		v *string
		k string
	}{{in.URL, SetCustomURL}, {in.AnthropicBase, SetAnthropic}} {
		if u.v == nil || (u.k == SetCustomURL && p != "custom") {
			continue
		}
		v := strings.TrimRight(strings.TrimSpace(*u.v), "/")
		if v != "" && !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
			return app.Bad("Enter a full address starting with https://")
		}
		if err := set(u.k, v); err != nil {
			return err
		}
	}
	if in.Research != nil {
		if err := set(SetResearch, *in.Research); err != nil {
			return err
		}
	}
	return set(SetProvider, p)
}

func saveConn(c *app.Ctx) (any, error) {
	var in connIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if err := saveConnTx(c, c.Tx(), c.S(), in); err != nil {
		return nil, err
	}
	v := connView(c.S())
	v["message"] = ProviderName(in.Provider) + " saved and in use."
	return v, nil
}

// testConn saves the connection (own short transaction) and sends a test request outside any transaction.
func testConn(c *app.Ctx) (any, error) {
	var in connIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	var s *settings.Store
	if err := c.A.OrgTx(c, c.Org.ID, func(tx pgx.Tx, st *settings.Store) error {
		s = st
		return saveConnTx(c, tx, st, in)
	}); err != nil {
		return nil, err
	}
	t0 := time.Now()
	r, err := Complete(c, c.A, c.Org.ID, s, Request{System: "You are a helpful assistant.", Messages: []Msg{{Role: "user", Content: "Reply with exactly: Connection OK"}},
		MaxTokens: 200, Provider: in.Provider, Purpose: "chat", Label: "Test connection", Deadline: 90 * time.Second})
	if err != nil {
		return nil, app.Bad(err.Error())
	}
	reply := strings.TrimSpace(r.Text)
	if len([]rune(reply)) > 120 {
		reply = string([]rune(reply)[:119]) + "…"
	}
	v := connView(s)
	v["test"] = map[string]any{"name": ProviderName(in.Provider), "secs": float64(time.Since(t0).Milliseconds()/100) / 10, "reply": reply, "model": r.Model}
	return v, nil
}
