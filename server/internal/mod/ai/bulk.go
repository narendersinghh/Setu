package ai

// Bulk engine (v1 Parallel.php + Batch.php). Other modules hand over AI requests of a bulk run with
// SubmitBulk; each answer is stored in ai_batch and handed back through a job of the caller's type.
//
//   - "parallel" (any provider): one "aipar" job per workspace keeps up to ai_parallel (default 5) requests
//     streaming at the same time, saves every answer the moment it is finished, retries rate limits and
//     overloads with a wait (the slot is freed meanwhile) and is resumable: requests left "running" by a
//     stopped worker go back into the queue.
//   - "batch" (Claude only, 50% cheaper): one "aibatch" job per workspace collects the requests of a run for
//     a few seconds, sends them as one Message Batch, checks it every 20 seconds and hands out the answers.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Job types owned by the ai engine.
const (
	JobParallel = "aipar"
	JobBatch    = "aibatch"
)

type jobsJob = jobs.Job

// reqJSON is a Request as stored in ai_batch.params.
type reqJSON struct {
	System      string  `json:"system"`
	Messages    []Msg   `json:"messages"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature,omitempty"`
	Model       string  `json:"model,omitempty"`
	WebSearch   bool    `json:"web_search,omitempty"`
	JSON        bool    `json:"json,omitempty"`
	Purpose     string  `json:"purpose,omitempty"`
	Label       string  `json:"label,omitempty"`
	Searches    int     `json:"searches,omitempty"`
}

func (r reqJSON) label() string {
	if r.Label != "" {
		return r.Label
	}
	return "Writing the report"
}

func (r reqJSON) op() string {
	if r.Purpose != "" {
		return r.Purpose
	}
	return "report"
}

// working is the lead's progress message while the AI writes ("AI is writing the report").
func (r reqJSON) working() string {
	l := r.label()
	return "AI is " + strings.ToLower(l[:1]) + l[1:]
}

func (r reqJSON) opts() callOpts {
	mx := r.MaxTokens
	if mx <= 0 {
		mx = 16000
	}
	return callOpts{System: r.System, Messages: r.Messages, Max: mx, Temperature: r.Temperature, JSON: r.JSON, WebSearch: r.WebSearch, Searches: r.Searches, NoRetry: true}
}

// bulkPayload is ai_batch.payload: the caller's callback and its payload, plus answer details.
type bulkPayload struct {
	CB      string          `json:"cb"`
	P       json.RawMessage `json:"p"`
	Model   string          `json:"model,omitempty"`
	Cost    float64         `json:"cost,omitempty"`
	Sources []Source        `json:"sources,omitempty"`
}

// SubmitBulk queues one AI request of a bulk run for a lead (in the caller's transaction). When the answer
// is ready – or the request finally failed – a job of type callbackJobType is queued for the lead with the
// caller's payload plus "ai_batch": <ai_batch id> (and "ai_error": "<why>" when it failed). The callback
// reads the answer with BulkResult and marks it used with BulkDone. Earlier waiting requests of the same
// lead are cancelled. The engine is chosen by the workspace setting ai_batch (see BulkMode).
func SubmitBulk(ctx context.Context, a *app.App, tx pgx.Tx, orgID, leadID int64, r Request, callbackJobType string, payload any) error {
	if callbackJobType == "" {
		return errors.New("ai: callback job type required")
	}
	s, err := settings.Load(ctx, tx, a.Box, orgID)
	if err != nil {
		return err
	}
	mode := BulkMode(s)
	if mode == "" {
		mode = "parallel"
	}
	if r.Provider != "" && r.Provider != ActiveProvider(s) {
		return errors.New("ai: bulk requests use the workspace provider")
	}
	p, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	pl, _ := json.Marshal(bulkPayload{CB: callbackJobType, P: p})
	params, _ := json.Marshal(reqJSON{System: r.System, Messages: r.Messages, MaxTokens: r.MaxTokens, Temperature: r.Temperature, Model: r.Model,
		WebSearch: r.WebSearch, JSON: r.JSON, Purpose: r.Purpose, Label: r.Label, Searches: r.Searches})
	if _, err := tx.Exec(ctx, `UPDATE ai_batch SET status='cancelled' WHERE lead_id=$1 AND status IN ('queued','sent','running')`, leadID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ai_batch (lead_id,mode,payload,params) VALUES ($1,$2,$3,$4)`, leadID, mode, pl, params); err != nil {
		return err
	}
	if mode == "batch" {
		// collect the whole bulk run for a few seconds, then send it as one batch
		return ensureJob(ctx, tx, orgID, JobBatch, time.Now().Add(15*time.Second), false)
	}
	return ensureJob(ctx, tx, orgID, JobParallel, time.Now().Add(3*time.Second), true)
}

// ensureJob makes sure a runner job of the workspace is waiting. pull: move a later pending run forward.
func ensureJob(ctx context.Context, q db.Q, orgID int64, typ string, at time.Time, pull bool) error {
	if pull {
		n, err := db.Exec(ctx, q, `UPDATE jobs SET run_after=LEAST(run_after,$3) WHERE org_id=$1 AND type=$2 AND status='pending'`, orgID, typ, at)
		if err != nil || n > 0 {
			return err
		}
	} else {
		have, err := db.Val[bool](ctx, q, `SELECT EXISTS(SELECT 1 FROM jobs WHERE org_id=$1 AND type=$2 AND status IN ('pending','running'))`, orgID, typ)
		if err != nil || have {
			return err
		}
	}
	// (a runner that is running right now may already be finishing, so a follow-up run is always queued)
	_, err := jobs.Push(ctx, q, orgID, typ, 0, nil, jobs.Opt{At: at, MaxAttempts: 5})
	return err
}

// BulkResult returns the stored answer of a bulk request (ai_batch id). It returns the reason when the
// request failed (the caller then does the work directly) and shared.ErrAborted when it was cancelled.
func BulkResult(ctx context.Context, q db.Q, batchRowID int64) (*Response, error) {
	type row struct {
		LeadID    int64  `db:"lead_id"`
		Mode      string `db:"mode"`
		Status    string `db:"status"`
		Result    string `db:"result"`
		TokensIn  int    `db:"tokens_in"`
		TokensOut int    `db:"tokens_out"`
		Error     string `db:"error"`
		Payload   []byte `db:"payload"`
	}
	r, err := db.One[row](ctx, q, `SELECT lead_id,mode,status,COALESCE(result,'') result,tokens_in,tokens_out,error,COALESCE(payload,'{}'::jsonb) payload FROM ai_batch WHERE id=$1`, batchRowID)
	if errors.Is(err, db.ErrNoRows) {
		return nil, errors.New("The AI answer for this request was not found – start it again.")
	}
	if err != nil {
		return nil, err
	}
	switch r.Status {
	case "done":
	case "cancelled":
		return nil, shared.ErrAborted
	case "error":
		return nil, errors.New(r.Error)
	case "saved":
		return nil, errors.New("The AI answer for this request was already used – start it again.")
	default:
		return nil, errors.New("The AI answer for this request is not ready yet.")
	}
	var p bulkPayload
	json.Unmarshal(r.Payload, &p)
	return &Response{Text: r.Result, Model: p.Model, TokensIn: r.TokensIn, TokensOut: r.TokensOut, Cost: p.Cost, Sources: p.Sources}, nil
}

// BulkDone marks a bulk answer as used and frees its stored text.
func BulkDone(ctx context.Context, q db.Q, batchRowID int64) error {
	_, err := q.Exec(ctx, `UPDATE ai_batch SET status='saved', params=NULL, result=NULL WHERE id=$1 AND status='done'`, batchRowID)
	return err
}

// callback queues the caller's job with its payload + ai_batch id (+ ai_error).
func callback(ctx context.Context, tx pgx.Tx, orgID, leadID, id int64, payload []byte, why string) error {
	var p bulkPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.CB == "" {
		return fmt.Errorf("ai_batch %d: no callback", id)
	}
	m := map[string]any{}
	if len(p.P) > 0 && json.Unmarshal(p.P, &m) != nil {
		m = map[string]any{"payload": json.RawMessage(p.P)}
	}
	m["ai_batch"] = id
	if why != "" {
		m["ai_error"] = why
	}
	_, err := jobs.Push(ctx, tx, orgID, p.CB, leadID, m)
	return err
}

// ================================================================ parallel engine

type parItem struct {
	ID       int64
	LeadID   int64
	Attempts int
	Req      reqJSON
	Payload  []byte
}

// parStore is everything the parallel runner writes (a database in production, memory in tests).
type parStore interface {
	claim(ctx context.Context, slots int) (*parItem, error)
	started(ctx context.Context, it *parItem, model string) int64
	progress(ctx context.Context, it *parItem, step int64, words int, el time.Duration)
	cancelled(ctx context.Context, ids []int64) map[int64]bool
	stopped(ctx context.Context, it *parItem, step int64, ms int)
	done(ctx context.Context, it *parItem, step int64, model string, r *result, ms int)
	retry(ctx context.Context, it *parItem, step int64, model string, r *result, wait time.Duration, why string, ms int)
	fail(ctx context.Context, it *parItem, step int64, model string, r *result, why string, ms int)
	heartbeat(ctx context.Context)
}

type caller func(ctx context.Context, c conn, o callOpts) (*result, error)

type parCfg struct {
	Slots   int
	Budget  time.Duration // stop taking new requests after this
	Beat    time.Duration // progress / abort check interval
	Timeout time.Duration // one request
	RetryIn func(attempt int, code int) time.Duration
}

func defaultRetryIn(attempt, code int) time.Duration {
	if code == 429 {
		return time.Duration(60*attempt) * time.Second
	}
	return time.Duration(20*attempt) * time.Second
}

// runParallel keeps up to cfg.Slots requests running until the queue is empty (or the budget for taking
// new ones is used up). Returns how many requests finished.
func runParallel(ctx context.Context, st parStore, c conn, cfg parCfg, do caller) int {
	type slot struct {
		it      *parItem
		model   string
		step    int64
		cancel  context.CancelFunc
		chars   atomic.Int64
		t0      time.Time
		stopped bool
	}
	type fin struct {
		s   *slot
		r   *result
		err error
	}
	if cfg.RetryIn == nil {
		cfg.RetryIn = defaultRetryIn
	}
	finished := make(chan fin)
	active := map[int64]*slot{}
	t0 := time.Now()
	n := 0
	start := func() bool {
		if time.Since(t0) > cfg.Budget || ctx.Err() != nil {
			return false
		}
		it, err := st.claim(ctx, cfg.Slots)
		if err != nil || it == nil {
			if err != nil {
				slog.Warn("aipar claim", "err", err)
			}
			return false
		}
		ci := c
		if it.Req.Model != "" {
			ci.Model = it.Req.Model
		}
		s := &slot{it: it, model: ci.Model, t0: time.Now()}
		s.step = st.started(ctx, it, ci.Model)
		rctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		s.cancel = cancel
		active[it.ID] = s
		o := it.Req.opts()
		o.OnDelta = func(t string) { s.chars.Add(int64(utf8.RuneCountInString(t))) }
		go func() {
			r, err := do(rctx, ci, o)
			finished <- fin{s, r, err}
		}()
		return true
	}
	fill := func() {
		for len(active) < cfg.Slots && start() {
		}
	}
	fill()
	tk := time.NewTicker(cfg.Beat)
	defer tk.Stop()
	for len(active) > 0 {
		select {
		case f := <-finished:
			s := f.s
			delete(active, s.it.ID)
			s.cancel()
			if s.stopped {
				continue
			}
			n++
			r := f.r
			if r == nil {
				r = &result{}
			}
			ms := int(time.Since(s.t0).Milliseconds())
			err := f.err
			if err == nil && strings.TrimSpace(r.Text) == "" {
				err = &apiError{Code: 200, Msg: "The AI returned an empty answer"}
			}
			if err == nil {
				st.done(ctx, s.it, s.step, s.model, r, ms)
			} else {
				why := err.Error()
				var ae *apiError
				code := 0
				if errors.As(err, &ae) {
					code = ae.Code
				}
				if s.it.Attempts < 3 && (retryable(err) || reBusy.MatchString(why)) && code != 200 {
					st.retry(ctx, s.it, s.step, s.model, r, cfg.RetryIn(s.it.Attempts, code), why, ms)
				} else {
					st.fail(ctx, s.it, s.step, s.model, r, why, ms)
				}
			}
			fill()
		case <-tk.C:
			ids := make([]int64, 0, len(active))
			for id, s := range active {
				if !s.stopped {
					ids = append(ids, id)
				}
			}
			canc := st.cancelled(ctx, ids)
			for _, id := range ids {
				s := active[id]
				if canc[id] {
					s.stopped = true
					s.cancel()
					st.stopped(ctx, s.it, s.step, int(time.Since(s.t0).Milliseconds()))
					continue
				}
				st.progress(ctx, s.it, s.step, int(s.chars.Load()/6), time.Since(s.t0))
			}
			st.heartbeat(ctx)
			fill()
		}
	}
	return n
}

// dbStore is the production parStore of one workspace.
type dbStore struct {
	a   *app.App
	org int64
	job int64
	s   *settings.Store
	pv  string
	tag string // claim marker of this runner
}

func (d *dbStore) tx(ctx context.Context, fn func(tx pgx.Tx) error) {
	bg := context.WithoutCancel(ctx)
	if err := d.a.DB.Org(bg, d.org, fn); err != nil {
		slog.Warn("aipar", "org", d.org, "err", err)
	}
}

func (d *dbStore) claim(ctx context.Context, slots int) (*parItem, error) {
	var it *parItem
	err := d.a.DB.Org(ctx, d.org, func(tx pgx.Tx) error {
		var params []byte
		var x parItem
		err := tx.QueryRow(ctx, `UPDATE ai_batch SET status='running', claim=$1, started_at=now(), attempts=attempts+1
WHERE id=(SELECT id FROM ai_batch WHERE mode='parallel' AND status='queued' AND (next_at IS NULL OR next_at<=now())
  AND (SELECT count(*) FROM ai_batch r WHERE r.mode='parallel' AND r.status='running') < $2
  ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)
RETURNING id, lead_id, attempts, COALESCE(params,'{}'::jsonb), COALESCE(payload,'{}'::jsonb)`, d.tag, slots).Scan(&x.ID, &x.LeadID, &x.Attempts, &params, &x.Payload)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		json.Unmarshal(params, &x.Req)
		it = &x
		return nil
	})
	return it, err
}

func (d *dbStore) started(ctx context.Context, it *parItem, model string) int64 {
	var sid int64
	name := it.Req.label() + " (parallel engine"
	if it.Attempts > 1 {
		name += ", try " + strconv.Itoa(it.Attempts)
	}
	name += ")"
	d.tx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO job_steps (lead_id,kind,step,status,model,detail) VALUES ($1,'ai',$2,'run',$3,'Sent to the AI – waiting for the first words') RETURNING id`,
			it.LeadID, cut(name, 190), cut(model, 90)).Scan(&sid); err != nil {
			return err
		}
		tx.Exec(ctx, `UPDATE ai_batch SET step_id=$2 WHERE id=$1`, it.ID, sid)
		_, err := tx.Exec(ctx, `UPDATE leads SET progress=72, progress_msg=$2 WHERE id=$1`, it.LeadID, cut(it.Req.working(), 180))
		return err
	})
	return sid
}

func (d *dbStore) progress(ctx context.Context, it *parItem, step int64, words int, el time.Duration) {
	detail := "Waiting for the first words"
	if words > 0 {
		detail = shared.FormatNum(float64(words)) + " words written so far"
	}
	d.tx(ctx, func(tx pgx.Tx) error {
		tx.Exec(ctx, `UPDATE leads SET progress=$2, progress_msg=$3 WHERE id=$1`, it.LeadID, min(84, 72+words/90),
			cut(it.Req.working()+" · "+shared.FormatNum(float64(words))+" words · "+elapsed(el), 180))
		_, err := tx.Exec(ctx, `UPDATE job_steps SET detail=$2 WHERE id=$1`, step, detail+" · "+elapsed(el))
		return err
	})
}

func (d *dbStore) cancelled(ctx context.Context, ids []int64) map[int64]bool {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out
	}
	d.tx(ctx, func(tx pgx.Tx) error {
		rows, err := db.All[struct {
			ID int64 `db:"id"`
		}](ctx, tx, `SELECT id FROM ai_batch WHERE id = ANY($1) AND status='cancelled'`, ids)
		for _, r := range rows {
			out[r.ID] = true
		}
		return err
	})
	return out
}

func (d *dbStore) stopped(ctx context.Context, it *parItem, step int64, ms int) {
	d.tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE job_steps SET status='err', detail='Stopped by user', ms=$2 WHERE id=$1`, step, ms)
		return err
	})
}

func (d *dbStore) usage(ctx context.Context, tx pgx.Tx, it *parItem, model string, r *result, failed bool) float64 {
	cost := Cost(d.s, model, r.In, r.Out, 1)
	app.TrackUsage(ctx, tx, d.pv, it.Req.op(), int64(r.In), int64(r.Out), cost, failed)
	return cost
}

func (d *dbStore) done(ctx context.Context, it *parItem, step int64, model string, r *result, ms int) {
	d.tx(ctx, func(tx pgx.Tx) error {
		cost := d.usage(ctx, tx, it, model, r, false)
		extra, _ := json.Marshal(map[string]any{"model": model, "cost": round5(cost), "sources": r.Sources})
		n, err := db.Exec(ctx, tx, `UPDATE ai_batch SET status='done', result=$2, tokens_in=$3, tokens_out=$4, payload=COALESCE(payload,'{}'::jsonb)||$5::jsonb WHERE id=$1 AND status='running'`,
			it.ID, r.Text, r.In, r.Out, extra)
		if err != nil {
			return err
		}
		tx.Exec(ctx, `UPDATE job_steps SET status='ok', ms=$2, tokens_in=$3, tokens_out=$4, cost=$5, detail=$6 WHERE id=$1`, step, ms, r.In, r.Out, round5(cost),
			fmt.Sprintf("Written in %ds · %s characters", (ms+500)/1000, shared.FormatNum(float64(utf8.RuneCountInString(r.Text)))))
		if n == 0 { // stopped meanwhile
			return nil
		}
		tx.Exec(ctx, `UPDATE leads SET progress=85, progress_msg=$2 WHERE id=$1`, it.LeadID, doneMsg(it.Req))
		return callback(ctx, tx, d.org, it.LeadID, it.ID, it.Payload, "")
	})
}

func doneMsg(r reqJSON) string {
	if r.op() == "report" {
		return "Report written – saving"
	}
	return "AI answer ready – saving"
}

func (d *dbStore) retry(ctx context.Context, it *parItem, step int64, model string, r *result, wait time.Duration, why string, ms int) {
	secs := int(wait.Seconds())
	d.tx(ctx, func(tx pgx.Tx) error {
		d.usage(ctx, tx, it, model, r, true)
		tx.Exec(ctx, `UPDATE ai_batch SET status='queued', claim=NULL, next_at=now()+$2::interval WHERE id=$1 AND status='running'`, it.ID, fmt.Sprintf("%d seconds", secs))
		tx.Exec(ctx, `UPDATE job_steps SET status='err', ms=$2, detail=$3 WHERE id=$1`, step, ms, cut(why, 400)+fmt.Sprintf(" – trying again in %ds", secs))
		_, err := tx.Exec(ctx, `UPDATE leads SET progress_msg=$2 WHERE id=$1`, it.LeadID, fmt.Sprintf("AI busy – trying again in %ds", secs))
		return err
	})
}

// fail: not possible in the engine – the caller writes this one on its own (its step reports the exact error).
func (d *dbStore) fail(ctx context.Context, it *parItem, step int64, model string, r *result, why string, ms int) {
	d.tx(ctx, func(tx pgx.Tx) error {
		if r != nil {
			d.usage(ctx, tx, it, model, r, true)
		}
		if step > 0 {
			tx.Exec(ctx, `UPDATE job_steps SET status='err', ms=$2, detail=$3 WHERE id=$1`, step, ms, cut(why, 400))
		}
		n, err := db.Exec(ctx, tx, `UPDATE ai_batch SET status='error', error=$2 WHERE id=$1 AND status IN ('running','queued')`, it.ID, cut(why, 500))
		if err != nil || n == 0 {
			return err
		}
		return callback(ctx, tx, d.org, it.LeadID, it.ID, it.Payload, cut(why, 500))
	})
}

func (d *dbStore) heartbeat(ctx context.Context) {
	if d.job == 0 {
		return
	}
	d.tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE jobs SET locked_at=now() WHERE id=$1 AND status='running'`, d.job)
		return err
	})
}

// runParallelJob is the "aipar" job of one workspace.
func runParallelJob(ctx context.Context, a *app.App, j *jobs.Job) error {
	var s *settings.Store
	if err := a.OrgTx(ctx, j.OrgID, func(tx pgx.Tx, st *settings.Store) error {
		s = st
		// Requests left "running" by a worker that was stopped go back into the queue
		_, err := tx.Exec(ctx, `UPDATE ai_batch SET status='queued', claim=NULL WHERE mode='parallel' AND status='running' AND started_at<now()-interval '7 minutes'`)
		return err
	}); err != nil {
		return err
	}
	c, cerr := connFor(s, "", a.Cfg.AppURL())
	st := &dbStore{a: a, org: j.OrgID, job: j.ID, s: s, pv: c.Provider, tag: fmt.Sprintf("j%d-%d", j.ID, time.Now().UnixNano())}
	if cerr != nil {
		why := "No API key or model set in AI Connection."
		for {
			it, err := st.claim(ctx, 1000)
			if err != nil || it == nil {
				return err
			}
			st.fail(ctx, it, 0, "", nil, why, 0)
		}
	}
	runParallel(ctx, st, c, parCfg{Slots: Slots(s), Budget: 150 * time.Second, Beat: 4 * time.Second, Timeout: 300 * time.Second}, call)
	// More waiting (or waiting for a retry slot): run again
	return a.DB.Org(context.WithoutCancel(ctx), j.OrgID, func(tx pgx.Tx) error {
		var next *time.Time
		if err := tx.QueryRow(ctx, `SELECT MIN(COALESCE(next_at,now())) FROM ai_batch WHERE mode='parallel' AND status='queued'`).Scan(&next); err != nil || next == nil {
			return err
		}
		at := *next
		if at.Before(time.Now()) {
			at = time.Now()
		}
		have, _ := db.Val[bool](ctx, tx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE type=$1 AND status='pending')`, JobParallel)
		if have {
			return nil
		}
		_, err := jobs.Push(ctx, tx, j.OrgID, JobParallel, 0, nil, jobs.Opt{At: at, MaxAttempts: 5})
		return err
	})
}

// ================================================================ Claude Message Batches

type batchItem struct {
	ID      int64     `db:"id"`
	LeadID  int64     `db:"lead_id"`
	Params  []byte    `db:"params"`
	Payload []byte    `db:"payload"`
	Created time.Time `db:"created_at"`
}

// batchParams turns a stored request into Messages API params (no streaming).
func batchParams(r reqJSON, model string) map[string]any {
	o := r.opts()
	if o.JSON {
		o.System = strings.TrimSpace(o.System + "\n\nReply with one valid JSON object only – no text before or after it.")
	}
	b := anthropicBody(model, o.System, o.Max, o.Temperature)
	b["messages"] = anthropicMsgs(o.Messages)
	if o.WebSearch {
		b["tools"] = []any{webSearchTool(model, o.Searches)}
	}
	return b
}

// runBatchJob is the "aibatch" job of one workspace: send waiting requests, check sent batches, hand out answers.
func runBatchJob(ctx context.Context, a *app.App, j *jobs.Job) error {
	var s *settings.Store
	var queued []batchItem
	var recent bool
	if err := a.OrgTx(ctx, j.OrgID, func(tx pgx.Tx, st *settings.Store) error {
		s = st
		var err error
		queued, err = db.All[batchItem](ctx, tx, `SELECT id,lead_id,COALESCE(params,'{}'::jsonb) params,COALESCE(payload,'{}'::jsonb) payload,created_at FROM ai_batch WHERE mode='batch' AND status='queued' ORDER BY id LIMIT 500`)
		if err != nil {
			return err
		}
		recent, err = db.Val[bool](ctx, tx, `SELECT EXISTS(SELECT 1 FROM ai_batch WHERE mode='batch' AND status='queued' AND created_at>now()-interval '10 seconds')`)
		return err
	}); err != nil {
		return err
	}
	c, cerr := connFor(s, "anthropic", a.Cfg.AppURL())
	bs := &batchRun{a: a, org: j.OrgID, s: s, c: c}
	// 1 · send everything that is waiting as one batch (wait a little for the rest of a bulk run)
	if len(queued) > 0 && cerr != nil {
		bs.fallback(ctx, queued, "Claude Message Batches need a Claude API key and model: "+cerr.Error())
	} else if len(queued) > 0 && !recent {
		bs.send(ctx, queued)
	}
	// 2 · check sent batches
	if cerr == nil {
		bs.poll(ctx)
	}
	// 3 · more to do: check again soon
	var left bool
	a.DB.Org(ctx, j.OrgID, func(tx pgx.Tx) error {
		var err error
		left, err = db.Val[bool](ctx, tx, `SELECT EXISTS(SELECT 1 FROM ai_batch WHERE mode='batch' AND status IN ('queued','sent'))`)
		return err
	})
	if left {
		if recent {
			return jobs.Defer(10 * time.Second)
		}
		return jobs.Defer(20 * time.Second)
	}
	return nil
}

type batchRun struct {
	a   *app.App
	org int64
	s   *settings.Store
	c   conn
}

func (b *batchRun) tx(ctx context.Context, fn func(tx pgx.Tx) error) {
	bg := context.WithoutCancel(ctx)
	if err := b.a.DB.Org(bg, b.org, fn); err != nil {
		slog.Warn("aibatch", "org", b.org, "err", err)
	}
}

func step(ctx context.Context, tx pgx.Tx, leadID int64, name, status, detail, model string, in, out int, cost float64, ms int) {
	kind := "step"
	if model != "" {
		kind = "ai"
	}
	tx.Exec(ctx, `INSERT INTO job_steps (lead_id,kind,step,status,detail,model,tokens_in,tokens_out,cost,ms) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		leadID, kind, cut(name, 190), status, cut(detail, 2000), cut(model, 90), in, out, round5(cost), ms)
}

// fallback: back to the normal one-by-one path (batch failed, expired or answer unusable).
func (b *batchRun) fallback(ctx context.Context, items []batchItem, why string) {
	b.tx(ctx, func(tx pgx.Tx) error {
		for _, it := range items {
			n, err := db.Exec(ctx, tx, `UPDATE ai_batch SET status='error', error=$2 WHERE id=$1 AND status IN ('queued','sent')`, it.ID, cut(why, 500))
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			step(ctx, tx, it.LeadID, "AI batch did not work – writing this report directly instead", "err", why, "", 0, 0, 0, 0)
			if err := callback(ctx, tx, b.org, it.LeadID, it.ID, it.Payload, cut(why, 500)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b *batchRun) send(ctx context.Context, items []batchItem) {
	reqs := make([]map[string]any, 0, len(items))
	for _, it := range items {
		var r reqJSON
		json.Unmarshal(it.Params, &r)
		m := b.c.Model
		if r.Model != "" {
			m = r.Model
		}
		reqs = append(reqs, map[string]any{"custom_id": "r" + strconv.FormatInt(it.ID, 10), "params": batchParams(r, m)})
	}
	var out struct {
		ID string `json:"id"`
	}
	code, err := doJSON(ctx, b.c, "POST", b.c.Base+"/v1/messages/batches", map[string]any{"requests": reqs}, &out)
	if err != nil || out.ID == "" {
		msg := "no batch id"
		if err != nil {
			msg = err.Error()
		}
		b.fallback(ctx, items, fmt.Sprintf("Batch could not be sent (HTTP %d): %s", code, cut(msg, 200)))
		return
	}
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	n := len(items)
	b.tx(ctx, func(tx pgx.Tx) error {
		rows, err := db.All[struct {
			LeadID int64 `db:"lead_id"`
		}](ctx, tx, `UPDATE ai_batch SET status='sent', batch_id=$1, sent_at=now() WHERE id = ANY($2) AND status='queued' RETURNING lead_id`, out.ID, ids)
		if err != nil {
			return err
		}
		lids := make([]int64, 0, len(rows))
		for _, r := range rows {
			lids = append(lids, r.LeadID)
		}
		tx.Exec(ctx, `UPDATE leads SET progress=72, progress_msg=$2 WHERE id = ANY($1)`, lids, fmt.Sprintf("AI batch: %d reports are being written together · usually a few minutes", n))
		s := ""
		if n > 1 {
			s = "s"
		}
		for _, l := range lids {
			step(ctx, tx, l, fmt.Sprintf("Sent to the AI as one batch with %d report%s (50%% cheaper, no waiting on the server)", n, s), "ok", "Batch "+out.ID, "", 0, 0, 0, 0)
		}
		return nil
	})
}

type batchStatus struct {
	ProcessingStatus string `json:"processing_status"`
	ResultsURL       string `json:"results_url"`
	Counts           struct {
		Processing, Succeeded, Errored, Canceled, Expired int
	} `json:"request_counts"`
}

func (b *batchRun) poll(ctx context.Context) {
	type group struct {
		BatchID string    `db:"batch_id"`
		Sent    time.Time `db:"sent"`
		N       int       `db:"n"`
	}
	var groups []group
	b.tx(ctx, func(tx pgx.Tx) error {
		var err error
		groups, err = db.All[group](ctx, tx, `SELECT batch_id, MIN(sent_at) sent, COUNT(*)::int n FROM ai_batch WHERE mode='batch' AND status='sent' AND batch_id IS NOT NULL GROUP BY batch_id`)
		return err
	})
	for _, g := range groups {
		base := b.c.Base + "/v1/messages/batches/" + url.PathEscape(g.BatchID)
		var d batchStatus
		if _, err := doJSON(ctx, b.c, "GET", base, nil, &d); err != nil {
			continue
		}
		var items []batchItem
		b.tx(ctx, func(tx pgx.Tx) error {
			var err error
			items, err = db.All[batchItem](ctx, tx, `SELECT id,lead_id,COALESCE(params,'{}'::jsonb) params,COALESCE(payload,'{}'::jsonb) payload,created_at FROM ai_batch WHERE batch_id=$1 AND status='sent'`, g.BatchID)
			return err
		})
		age := time.Since(g.Sent)
		if d.ProcessingStatus != "ended" {
			// Taking far too long: cancel it and write these one by one instead
			if age > 45*time.Minute {
				doJSON(ctx, b.c, "POST", base+"/cancel", map[string]any{}, nil)
				b.fallback(ctx, items, "The AI batch took longer than 45 minutes")
				continue
			}
			done := d.Counts.Succeeded + d.Counts.Errored + d.Counts.Expired + d.Counts.Canceled
			lids := make([]int64, len(items))
			for i, it := range items {
				lids[i] = it.LeadID
			}
			b.tx(ctx, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE leads SET progress_msg=$2 WHERE id = ANY($1) AND stage='reporting'`, lids,
					fmt.Sprintf("AI batch: %d of %d reports written · %s", done, g.N, elapsed(age)))
				return err
			})
			continue
		}
		// 3 · ended: store each answer and hand it to its own short save step
		var raw []byte
		if d.ResultsURL == "" {
			d.ResultsURL = base + "/results"
		}
		code, err := doJSON(ctx, b.c, "GET", d.ResultsURL, nil, &raw)
		if err != nil {
			if age > 45*time.Minute {
				b.fallback(ctx, items, fmt.Sprintf("Batch results could not be downloaded (HTTP %d)", code))
			}
			continue
		}
		b.store(ctx, items, parseBatchResults(raw), int(age.Milliseconds()))
	}
}

type batchAnswer struct {
	Type    string
	Text    string
	In, Out int
	Sources []Source
	Error   string
}

// parseBatchResults reads the JSONL results file: custom_id → answer.
func parseBatchResults(raw []byte) map[string]batchAnswer {
	out := map[string]batchAnswer{}
	for _, ln := range bytes.Split(raw, []byte("\n")) {
		ln = bytes.TrimSpace(ln)
		if len(ln) == 0 {
			continue
		}
		var x struct {
			CustomID string `json:"custom_id"`
			Result   struct {
				Type    string `json:"type"`
				Message struct {
					Content []map[string]any `json:"content"`
					Usage   aUsage           `json:"usage"`
				} `json:"message"`
				Error struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				} `json:"error"`
			} `json:"result"`
		}
		if json.Unmarshal(ln, &x) != nil || x.CustomID == "" {
			continue
		}
		t, src := anthropicText(x.Result.Message.Content)
		out[x.CustomID] = batchAnswer{Type: x.Result.Type, Text: t, Sources: src, In: x.Result.Message.Usage.input(), Out: x.Result.Message.Usage.Out, Error: x.Result.Error.Error.Message}
	}
	return out
}

func (b *batchRun) store(ctx context.Context, items []batchItem, by map[string]batchAnswer, ms int) {
	for _, it := range items {
		x, ok := by["r"+strconv.FormatInt(it.ID, 10)]
		if !ok || x.Type != "succeeded" {
			t := x.Type
			if !ok {
				t = "missing"
			}
			why := "Batch result: " + t
			if x.Error != "" {
				why += " – " + x.Error
			}
			b.fallback(ctx, []batchItem{it}, why)
			continue
		}
		var r reqJSON
		json.Unmarshal(it.Params, &r)
		model := b.c.Model
		if r.Model != "" {
			model = r.Model
		}
		b.tx(ctx, func(tx pgx.Tx) error {
			cost := Cost(b.s, model, x.In, x.Out, 0.5)
			app.TrackUsage(ctx, tx, "anthropic", r.op()+"_batch", int64(x.In), int64(x.Out), cost, strings.TrimSpace(x.Text) == "")
			extra, _ := json.Marshal(map[string]any{"model": model, "cost": round5(cost), "sources": x.Sources})
			n, err := db.Exec(ctx, tx, `UPDATE ai_batch SET status='done', result=$2, tokens_in=$3, tokens_out=$4, payload=COALESCE(payload,'{}'::jsonb)||$5::jsonb WHERE id=$1 AND status='sent'`,
				it.ID, x.Text, x.In, x.Out, extra)
			if err != nil || n == 0 {
				return err
			}
			step(ctx, tx, it.LeadID, r.label()+" (AI batch)", "ok", fmt.Sprintf("Written in the batch · %s characters · half price", shared.FormatNum(float64(utf8.RuneCountInString(x.Text)))), model, x.In, x.Out, cost, ms)
			return callback(ctx, tx, b.org, it.LeadID, it.ID, it.Payload, "")
		})
	}
}

// ================================================================ periodic safety net

// sweep restarts the runner of every workspace that has waiting bulk requests but no runner job
// (e.g. a worker crashed, or a request arrived just as the runner finished).
func sweep(ctx context.Context, a *app.App) error {
	type row struct {
		OrgID int64  `db:"org_id"`
		Mode  string `db:"mode"`
	}
	rows, err := db.All[row](ctx, a.DB.Pool, `SELECT org_id, mode FROM ai_batch
WHERE (mode='parallel' AND (status='queued' OR (status='running' AND started_at<now()-interval '7 minutes')))
   OR (mode='batch' AND status IN ('queued','sent'))
GROUP BY org_id, mode LIMIT 5000`)
	if err != nil {
		return err
	}
	for _, r := range rows {
		typ := JobParallel
		if r.Mode == "batch" {
			typ = JobBatch
		}
		if err := ensureJob(ctx, a.DB.Pool, r.OrgID, typ, time.Now(), false); err != nil {
			return err
		}
	}
	return nil
}
