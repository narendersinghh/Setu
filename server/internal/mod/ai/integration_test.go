package ai

// Integration test against PostgreSQL (skipped unless SETU_AI_TEST_DB / SETU_AI_TEST_APPDB are set):
//   SETU_AI_TEST_DB=postgres://setu_owner:owner@localhost:5432/setu_ai SETU_AI_TEST_APPDB=postgres://setu_app:app@localhost:5432/setu_ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/config"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/settings"
)

func TestBulkEngineWithPostgres(t *testing.T) {
	owner, appURL := os.Getenv("SETU_AI_TEST_DB"), os.Getenv("SETU_AI_TEST_APPDB")
	if owner == "" || appURL == "" {
		t.Skip("set SETU_AI_TEST_DB and SETU_AI_TEST_APPDB")
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, owner, appURL); err != nil {
		t.Fatal(err)
	}
	os.Setenv("DATABASE_URL", appURL)
	if os.Getenv("SETU_MASTER_KEY") == "" {
		os.Setenv("SETU_MASTER_KEY", strings.Repeat("ab", 32))
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ctx, appURL)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	a := app.New(cfg, d)
	var org int64
	slug := "aitest" + time.Now().Format("150405.000")
	slug = strings.ReplaceAll(slug, ".", "")
	if err := d.Pool.QueryRow(ctx, `INSERT INTO orgs (slug,name) VALUES ($1,'AI test') RETURNING id`, slug).Scan(&org); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := capture(r)
		msgs := c.body["messages"].([]any)
		p := msgs[len(msgs)-1].(map[string]any)["content"].(string)
		openAIStream(w, `{"choices":[{"delta":{"content":"report for `+p+`"}}]}`, `{"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":500}}`)
	}))
	defer srv.Close()

	var leads []int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(a.OrgTx(ctx, org, func(tx pgx.Tx, s *settings.Store) error {
		must(s.Many(ctx, tx, map[string]any{"ai_provider": "openai", "ai_key_openai": "sk-test", "ai_model_openai": "gpt-4.1", "ai_parallel": "2"}))
		for i := 0; i < 3; i++ {
			var id int64
			must(tx.QueryRow(ctx, `INSERT INTO leads (place_id,name,stage) VALUES ($1,$2,'reporting') RETURNING id`, "p"+string(rune('a'+i)), "Biz "+string(rune('A'+i))).Scan(&id))
			leads = append(leads, id)
			must(SubmitBulk(ctx, a, tx, org, id, Request{System: "sys", Messages: []Msg{{Role: "user", Content: "lead " + string(rune('A'+i))}}, MaxTokens: 100, Purpose: "report"}, "report", map[string]any{"phase": "save", "x": i}))
		}
		if !Configured(s) || BulkMode(s) != "parallel" || Slots(s) != 2 {
			t.Fatalf("settings: configured %v mode %q slots %d", Configured(s), BulkMode(s), Slots(s))
		}
		return nil
	}))
	// one runner job queued for the workspace
	n, _ := db.Val[int](ctx, d.Pool, `SELECT count(*) FROM jobs WHERE org_id=$1 AND type='aipar' AND status='pending'`, org)
	if n != 1 {
		t.Fatalf("aipar jobs %d", n)
	}
	// the sweep sees the waiting work from outside any workspace (worker_scan policy) but adds no duplicate job
	must(sweep(ctx, a))
	n, _ = db.Val[int](ctx, d.Pool, `SELECT count(*) FROM jobs WHERE org_id=$1 AND type='aipar'`, org)
	if n != 1 {
		t.Fatalf("sweep duplicated the runner: %d", n)
	}

	var s *settings.Store
	must(a.OrgTx(ctx, org, func(tx pgx.Tx, st *settings.Store) error { s = st; return nil }))
	st := &dbStore{a: a, org: org, s: s, pv: "openai", tag: "test"}
	done := runParallel(ctx, st, conn{Provider: "openai", Key: "sk-test", Model: "gpt-4.1", Base: srv.URL}, parCfg{Slots: 2, Budget: time.Minute, Beat: 50 * time.Millisecond, Timeout: 10 * time.Second}, call)
	if done != 3 {
		t.Fatalf("finished %d", done)
	}
	must(a.DB.Org(ctx, org, func(tx pgx.Tx) error {
		type cb struct {
			RefID   int64  `db:"ref_id"`
			Payload []byte `db:"payload"`
		}
		cbs, err := db.All[cb](ctx, tx, `SELECT ref_id,payload FROM jobs WHERE type='report' ORDER BY id`)
		must(err)
		if len(cbs) != 3 {
			t.Fatalf("callbacks %d", len(cbs))
		}
		for _, c := range cbs {
			var p struct {
				Phase   string `json:"phase"`
				X       *int   `json:"x"`
				AIBatch int64  `json:"ai_batch"`
			}
			json.Unmarshal(c.Payload, &p)
			if p.Phase != "save" || p.X == nil || p.AIBatch == 0 {
				t.Fatalf("callback payload %s", c.Payload)
			}
			res, err := BulkResult(ctx, tx, p.AIBatch)
			must(err)
			if !strings.HasPrefix(res.Text, "report for lead ") || res.Model != "gpt-4.1" || res.TokensIn != 1000 || res.Cost <= 0 {
				t.Fatalf("result %+v", res)
			}
			must(BulkDone(ctx, tx, p.AIBatch))
			if _, err := BulkResult(ctx, tx, p.AIBatch); err == nil {
				t.Fatal("used answer returned again")
			}
		}
		steps, tot, err := ProcessView(ctx, tx, leads[0], 150)
		must(err)
		if len(steps) != 1 || steps[0].Kind != "ai" || steps[0].Status != "ok" || tot.AI != 1 || tot.In != 1000 || tot.Cost <= 0 {
			t.Fatalf("process view %+v %+v", steps, tot)
		}
		u, _ := db.Val[int](ctx, tx, `SELECT calls FROM api_usage WHERE service='openai' AND op='report'`)
		if u != 3 {
			t.Fatalf("usage calls %d", u)
		}
		pr, _ := db.Val[int16](ctx, tx, `SELECT progress FROM leads WHERE id=$1`, leads[1])
		if pr != 85 {
			t.Fatalf("progress %d", pr)
		}
		return nil
	}))

	// Abort: waiting requests and lead jobs are cancelled, the lead goes back to a stable stage
	must(a.OrgTx(ctx, org, func(tx pgx.Tx, s *settings.Store) error {
		must(SubmitBulk(ctx, a, tx, org, leads[2], Request{Messages: []Msg{{Role: "user", Content: "again"}}}, "report", map[string]any{}))
		n, err := Abort(ctx, tx, []int64{leads[2]})
		must(err)
		var stage string
		var flag bool
		must(tx.QueryRow(ctx, `SELECT stage,abort_flag FROM leads WHERE id=$1`, leads[2]).Scan(&stage, &flag))
		st, _ := db.Val[string](ctx, tx, `SELECT status FROM ai_batch WHERE lead_id=$1 ORDER BY id DESC LIMIT 1`, leads[2])
		left, _ := db.Val[int](ctx, tx, `SELECT count(*) FROM jobs WHERE ref_id=$1 AND type='report' AND status='pending'`, leads[2])
		if n != 1 || stage != "new" || !flag || st != "cancelled" || left != 0 {
			t.Fatalf("abort n=%d stage=%s flag=%v batch=%s jobs=%d", n, stage, flag, st, left)
		}
		id, _ := db.Val[int64](ctx, tx, `SELECT id FROM ai_batch WHERE lead_id=$1 ORDER BY id DESC LIMIT 1`, leads[2])
		if _, err := BulkResult(ctx, tx, id); err != shared.ErrAborted {
			t.Fatalf("cancelled result: %v", err)
		}
		return nil
	}))

	// Message Batches: send, poll, hand out answers
	bsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/messages/batches":
			c := capture(r)
			var lines []string
			for _, q := range c.body["requests"].([]any) {
				lines = append(lines, `{"custom_id":"`+q.(map[string]any)["custom_id"].(string)+`","result":{"type":"succeeded","message":{"content":[{"type":"text","text":"batch answer"}],"usage":{"input_tokens":10,"output_tokens":5}}}}`)
			}
			os.WriteFile(os.TempDir()+"/ai-batch-results.jsonl", []byte(strings.Join(lines, "\n")), 0o600)
			w.Write([]byte(`{"id":"msgbatch_t"}`))
		case r.URL.Path == "/v1/messages/batches/msgbatch_t":
			w.Write([]byte(`{"processing_status":"ended","results_url":"http://` + r.Host + `/results"}`))
		case r.URL.Path == "/results":
			b, _ := os.ReadFile(os.TempDir() + "/ai-batch-results.jsonl")
			w.Write(b)
		}
	}))
	defer bsrv.Close()
	var items []batchItem
	must(a.OrgTx(ctx, org, func(tx pgx.Tx, s *settings.Store) error {
		must(s.Many(ctx, tx, map[string]any{"ai_provider": "anthropic", "ai_key_anthropic": "k", "ai_model_anthropic": "claude-haiku-5-5", "ai_batch": "batch"}))
		for _, l := range leads[:2] {
			must(SubmitBulk(ctx, a, tx, org, l, Request{Messages: []Msg{{Role: "user", Content: "b"}}}, "report", map[string]any{"phase": "save"}))
		}
		var err error
		items, err = db.All[batchItem](ctx, tx, `SELECT id,lead_id,params,payload,created_at FROM ai_batch WHERE mode='batch' AND status='queued'`)
		return err
	}))
	if len(items) != 2 {
		t.Fatalf("batch items %d", len(items))
	}
	br := &batchRun{a: a, org: org, s: s, c: conn{Provider: "anthropic", Key: "k", Model: "claude-haiku-5-5", Base: bsrv.URL}}
	br.send(ctx, items)
	br.poll(ctx)
	must(a.DB.Org(ctx, org, func(tx pgx.Tx) error {
		n, _ := db.Val[int](ctx, tx, `SELECT count(*) FROM ai_batch WHERE mode='batch' AND status='done' AND result='batch answer'`)
		cb, _ := db.Val[int](ctx, tx, `SELECT count(*) FROM jobs WHERE type='report' AND payload ? 'ai_batch' AND status='pending'`)
		u, _ := db.Val[int](ctx, tx, `SELECT calls FROM api_usage WHERE service='anthropic' AND op='report_batch'`)
		if n != 2 || cb < 2 || u != 2 {
			t.Fatalf("batch done %d callbacks %d usage %d", n, cb, u)
		}
		return nil
	}))
	// StopAll cancels everything that is still waiting
	must(a.DB.Org(ctx, org, func(tx pgx.Tx) error {
		_, err := StopAll(ctx, tx)
		return err
	}))
	d.Pool.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, org)
}

func TestCompleteWithPostgres(t *testing.T) {
	owner, appURL := os.Getenv("SETU_AI_TEST_DB"), os.Getenv("SETU_AI_TEST_APPDB")
	if owner == "" || appURL == "" {
		t.Skip("set SETU_AI_TEST_DB and SETU_AI_TEST_APPDB")
	}
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Migrate(ctx, owner, appURL))
	os.Setenv("DATABASE_URL", appURL)
	if os.Getenv("SETU_MASTER_KEY") == "" {
		os.Setenv("SETU_MASTER_KEY", strings.Repeat("ab", 32))
	}
	cfg, err := config.Load()
	must(err)
	d, err := db.Open(ctx, appURL)
	must(err)
	defer d.Close()
	a := app.New(cfg, d)
	allowPrivate, beatEvery = true, 40*time.Millisecond
	defer func() { allowPrivate, beatEvery = false, 4*time.Second }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := capture(r)
		msgs := c.body["messages"].([]any)
		if msgs[len(msgs)-1].(map[string]any)["content"] == "slow" {
			w.Header().Set("Content-Type", "text/event-stream")
			for i := 0; i < 300; i++ {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
				w.Write([]byte(`data: {"choices":[{"delta":{"content":"word "}}]}` + "\n\n"))
				w.(http.Flusher).Flush()
			}
			return
		}
		openAIStream(w, `{"choices":[{"delta":{"content":"Connection OK"}}]}`, `{"choices":[],"usage":{"prompt_tokens":2000000,"completion_tokens":100000}}`)
	}))
	defer srv.Close()
	var org, lead int64
	must(d.Pool.QueryRow(ctx, `INSERT INTO orgs (slug,name) VALUES ($1,'AI complete') RETURNING id`, "aic"+strings.ReplaceAll(time.Now().Format("150405.000"), ".", "")).Scan(&org))
	defer d.Pool.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, org)
	var s *settings.Store
	must(a.OrgTx(ctx, org, func(tx pgx.Tx, st *settings.Store) error {
		s = st
		must(st.Many(ctx, tx, map[string]any{"ai_provider": "custom", "ai_key_custom": "k", "ai_model_custom": "gpt-4.1", "ai_custom_url": srv.URL}))
		return tx.QueryRow(ctx, `INSERT INTO leads (place_id,name,stage) VALUES ('x','Biz','reporting') RETURNING id`).Scan(&lead)
	}))
	tr := &shared.Trace{A: a, Org: org, Lead: lead}
	res, err := Complete(ctx, a, org, s, Request{System: "s", Messages: []Msg{{Role: "user", Content: "hi"}}, Purpose: "report", Label: "Writing the report", Trace: tr})
	must(err)
	tr.Close(ctx, "ok", "")
	if res.Text != "Connection OK" || res.Provider != "custom" || res.Cost < 4.79 || res.Cost > 4.81 {
		t.Fatalf("%+v", res)
	}
	// Abort while the AI is writing: the request stops at the next heartbeat
	go func() {
		time.Sleep(200 * time.Millisecond)
		a.DB.Org(ctx, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE leads SET abort_flag=true WHERE id=$1`, lead)
			return err
		})
	}()
	t0 := time.Now()
	_, err = Complete(ctx, a, org, s, Request{Messages: []Msg{{Role: "user", Content: "slow"}}, Purpose: "report", Label: "Writing the report", Trace: tr})
	if err != shared.ErrAborted || time.Since(t0) > 2*time.Second {
		t.Fatalf("abort: %v after %s", err, time.Since(t0))
	}
	must(a.DB.Org(ctx, org, func(tx pgx.Tx) error {
		steps, tot, err := ProcessView(ctx, tx, lead, 10)
		must(err)
		if len(steps) != 2 || steps[0].Model != "gpt-4.1" || steps[0].TokensIn != 2000000 || steps[0].Status != "ok" || !strings.HasPrefix(steps[0].Detail, "Answered in") ||
			steps[1].Status != "err" || steps[1].Detail != "Stopped by user" || tot.AI != 2 {
			t.Fatalf("steps %+v totals %+v", steps, tot)
		}
		msg, _ := db.Val[string](ctx, tx, `SELECT progress_msg FROM leads WHERE id=$1`, lead)
		if !strings.Contains(msg, "Writing the report") {
			t.Fatalf("progress msg %q", msg)
		}
		u, _ := db.Val[int](ctx, tx, `SELECT calls FROM api_usage WHERE service='custom' AND op='report'`)
		if u != 2 {
			t.Fatalf("usage %d", u)
		}
		return nil
	}))
}
