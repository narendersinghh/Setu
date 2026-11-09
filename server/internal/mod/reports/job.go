package reports

// Job "report" in three short phases (each its own job so a slow AI call never blocks the rest):
// research (skipped when off or cached) → AI writes the report → PDF is rendered. Port of v1
// Queue::report / Queue::ready and Report::research / write / finish / verifySources.

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/mod/ai"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

const JobReport = "report"

// payload of a "report" job. "auto" is the v1 name of "contact" (prospecting may send either).
type payload struct {
	Phase    string          `json:"phase,omitempty"`
	PromptID int64           `json:"prompt_id,omitempty"`
	PW       int64           `json:"pw,omitempty"` // prompt for businesses with a website
	PN       int64           `json:"pn,omitempty"` // prompt for businesses without a website
	Contact  bool            `json:"contact,omitempty"`
	Auto     bool            `json:"auto,omitempty"`
	Bulk     bool            `json:"bulk,omitempty"`
	Brief    json.RawMessage `json:"brief,omitempty"`
	RID      int64           `json:"rid,omitempty"`
	// set by the ai bulk engine on the "save" callback
	AIBatch int64  `json:"ai_batch,omitempty"`
	AIError string `json:"ai_error,omitempty"`
}

func (p *payload) contact() bool { return p.Contact || p.Auto }

func (p payload) next(phase string) payload {
	p.Phase = phase
	return p
}

// pushReport queues a phase of the report job (each phase is retried once, like v1).
func pushReport(ctx context.Context, q db.Q, orgID, leadID int64, p payload) error {
	_, err := jobs.Push(ctx, q, orgID, JobReport, leadID, p, jobs.Opt{MaxAttempts: 2})
	return err
}

// Enqueue starts writing a report for a lead (job "report"). A pending report job of the lead is replaced.
func Enqueue(ctx context.Context, tx pgx.Tx, orgID, leadID int64, o EnqueueOpts) error {
	if err := app.Limit(ctx, tx, orgID, "reports_month", 1); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM jobs WHERE org_id=$1 AND type=$2 AND ref_id=$3 AND status='pending'`, orgID, JobReport, leadID); err != nil {
		return err
	}
	return pushReport(ctx, tx, orgID, leadID, payload{PromptID: o.PromptID, Contact: o.Contact, Bulk: o.Bulk})
}

func phaseTitle(p string, attempt int16) string {
	a := ""
	if attempt > 1 {
		a = fmt.Sprintf(" (attempt %d)", attempt)
	}
	switch p {
	case "write":
		return "Report · step 2 of 3: AI writes the report" + a
	case "save":
		return "Report · step 2 of 3: saving the AI answer" + a
	case "pdf":
		return "Report · step 3 of 3: PDF design" + a
	}
	return "Report · step 1 of 3: market research" + a
}

// runner carries one job execution.
type runner struct {
	a     *app.App
	ctx   context.Context
	job   *jobs.Job
	org   int64
	lead  int64
	tr    *shared.Trace
	s     *settings.Store
	abort bool
}

// prog updates the lead's progress (and the process log) and reports an abort by the user.
func (r *runner) prog(pct int, msg string) error {
	if !strings.HasPrefix(msg, "AI is ") && msg != r.tr.Label {
		r.tr.Step(r.ctx, msg, "step")
	}
	err := r.a.DB.Org(r.ctx, r.org, func(tx pgx.Tx) error {
		err := shared.Progress(r.ctx, tx, r.lead, pct, msg)
		if errors.Is(err, shared.ErrAborted) {
			r.abort = true
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	if r.abort {
		return shared.ErrAborted
	}
	return nil
}

func (r *runner) tx(fn func(tx pgx.Tx) error) error { return r.a.DB.Org(r.ctx, r.org, fn) }

func (r *runner) settings() error {
	return r.tx(func(tx pgx.Tx) error {
		s, err := settings.Load(r.ctx, tx, r.a.Box, r.org)
		r.s = s
		return err
	})
}

func handleJob(a *app.App) jobs.Handler {
	return func(ctx context.Context, j *jobs.Job) error {
		var p payload
		j.Decode(&p)
		tr := &shared.Trace{A: a, Org: j.OrgID, Lead: j.RefID, Job: j.ID}
		r := &runner{a: a, ctx: ctx, job: j, org: j.OrgID, lead: j.RefID, tr: tr}
		tr.Step(ctx, phaseTitle(p.Phase, j.Attempts), "start")
		err := r.settings()
		if err == nil {
			err = r.run(&p)
		}
		var de jobs.DeferError
		switch {
		case err == nil:
			tr.Close(ctx, "ok", "")
			return nil
		case errors.As(err, &de):
			tr.Close(ctx, "ok", fmt.Sprintf("Waiting for shared market research – trying again in %ds", int(de.After.Seconds())))
			return err
		case errors.Is(err, shared.ErrAborted) || errors.Is(err, jobs.ErrCancelled):
			tr.Step(ctx, "Stopped by user", "step")
			tr.Close(ctx, "err", "The process was aborted.")
			return jobs.ErrCancelled
		}
		msg := err.Error()
		var pe jobs.PermanentError
		final := errors.As(err, &pe) || j.Attempts >= j.MaxAttempts
		tr.Close(ctx, "err", msg)
		lbl := "Error – will retry in 1 minute"
		if final {
			lbl = "Failed"
		}
		tr.Step(ctx, lbl+": "+trunc(msg, 140), "step")
		tr.Close(ctx, "err", msg)
		bg := context.WithoutCancel(ctx)
		a.DB.Org(bg, j.OrgID, func(tx pgx.Tx) error {
			if final {
				tx.Exec(bg, `UPDATE leads SET stage='failed', last_error=$2, progress_msg='Failed' WHERE id=$1`, j.RefID, trunc(msg, 1000))
				return app.Event(bg, tx, j.RefID, "error", "Report failed: "+msg, "", nil)
			}
			_, err := tx.Exec(bg, `UPDATE leads SET progress_msg=$2 WHERE id=$1`, j.RefID, trunc("Retrying after error: "+msg, 180))
			return err
		})
		return err
	}
}

func (r *runner) run(p *payload) error {
	switch p.Phase {
	case "", "research":
		if err := r.tx(func(tx pgx.Tx) error {
			_, err := tx.Exec(r.ctx, `UPDATE leads SET stage='reporting' WHERE id=$1`, r.lead)
			return err
		}); err != nil {
			return err
		}
		if err := r.prog(60, "Preparing audit data for the AI"); err != nil {
			return err
		}
		brief, err := r.research()
		if err != nil {
			return err
		}
		if err := r.prog(68, "Queued: AI is about to write the report"); err != nil {
			return err
		}
		n := p.next("write")
		if brief != nil {
			n.Brief, _ = json.Marshal(brief)
		}
		return r.tx(func(tx pgx.Tx) error { return pushReport(r.ctx, tx, r.org, r.lead, n) })
	case "write", "save":
		var brief J
		if len(p.Brief) > 0 {
			json.Unmarshal(p.Brief, &brief)
		}
		var rid int64
		var err error
		if p.Phase == "save" {
			rid, err = r.save(p, brief)
		} else if mode := ai.BulkMode(r.s); p.Bulk && mode != "" {
			// Bulk run: the AI engine writes several reports at once (parallel) or as one Claude batch;
			// the answer comes back as phase "save".
			return r.submitBulk(p, brief, mode)
		} else {
			rid, err = r.write(p, brief)
		}
		if err != nil {
			return err
		}
		n := p.next("pdf")
		n.Brief, n.RID, n.AIBatch, n.AIError = nil, rid, 0, ""
		if reportMode(r.s) != "link" {
			if err := r.prog(88, "Queued: designing the PDF"); err != nil {
				return err
			}
			return r.tx(func(tx pgx.Tx) error { return pushReport(r.ctx, tx, r.org, r.lead, n) })
		}
		return r.ready(&n)
	case "pdf":
		if err := r.prog(92, "Rendering PDF pages: charts, illustrations & layout"); err != nil {
			return err
		}
		if _, _, err := ensurePDF(r.ctx, r.a, r.org, p.RID); err != nil && !errors.Is(err, db.ErrNoRows) {
			return err
		}
		return r.ready(p)
	}
	return jobs.Permanent(fmt.Errorf("unknown report phase %q", p.Phase))
}

// ready: report finished – lead stage, journey, automations and (optionally) outreach.
func (r *runner) ready(p *payload) error {
	return r.tx(func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.ctx, `UPDATE leads SET progress=100, progress_msg='Report ready', stage='report_ready' WHERE id=$1`, r.lead); err != nil {
			return err
		}
		if err := app.Event(r.ctx, tx, r.lead, "stage", "Report generated", "", map[string]any{"stage": "report_ready"}); err != nil {
			return err
		}
		r.a.Emit(r.ctx, tx, r.org, "report_ready", r.lead, map[string]any{"report_id": p.RID})
		if p.contact() {
			return scheduleContact(r.ctx, tx, r.s, r.org, r.lead)
		}
		return nil
	})
}

// scheduleContact queues sending the report to the business (job "contact", owned by prospecting),
// spaced by the "Seconds between two sends" setting (v1 Outreach::schedule).
func scheduleContact(ctx context.Context, tx pgx.Tx, s *settings.Store, orgID, leadID int64) error {
	gap := time.Duration(max(0, s.Int("send_gap", 90))) * time.Second
	at := time.Now()
	var last *time.Time
	tx.QueryRow(ctx, `SELECT max(run_after) FROM jobs WHERE org_id=$1 AND type='contact' AND status IN ('pending','running')`, orgID).Scan(&last)
	if last != nil && last.Add(gap).After(at) {
		at = last.Add(gap)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM jobs WHERE org_id=$1 AND type='contact' AND ref_id=$2 AND status='pending'`, orgID, leadID); err != nil {
		return err
	}
	if _, err := jobs.Push(ctx, tx, orgID, "contact", leadID, map[string]any{}, jobs.Opt{At: at, MaxAttempts: 2}); err != nil {
		return err
	}
	tz := app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))
	return app.Event(ctx, tx, leadID, "scheduled", "Outreach scheduled for "+at.In(tz).Format("02 Jan, 03:04 PM"), "", nil)
}

func reportMode(s Getter) string {
	switch m := s.Get("report_delivery", "both"); m {
	case "link", "pdf", "both":
		return m
	}
	return "both"
}

// ---------------------------------------------------------------- AI input

type promptRow struct {
	ID        int64     `db:"id" json:"id"`
	Type      string    `db:"type" json:"type"`
	Name      string    `db:"name" json:"name"`
	Subject   string    `db:"subject" json:"subject"`
	Content   string    `db:"content" json:"content"`
	IsActive  bool      `db:"is_active" json:"is_active"`
	IsDefault bool      `db:"is_default" json:"is_default"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

const promptCols = `id,type,name,subject,content,is_active,is_default,updated_at`

// activePrompt: the prompt in use for a type (or the first one).
func activePrompt(ctx context.Context, q db.Q, typ string) (*promptRow, error) {
	p, err := db.One[promptRow](ctx, q, `SELECT `+promptCols+` FROM prompts WHERE type=$1 ORDER BY is_active DESC, id LIMIT 1`, typ)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ActivePrompt returns (subject, content) of the prompt / message template in use for a type
// (report_website, report_noweb, email, email_noweb, whatsapp, whatsapp_noweb). Used by outreach.
func ActivePrompt(ctx context.Context, q db.Q, typ string) (subject, content string, err error) {
	p, err := activePrompt(ctx, q, typ)
	if err != nil {
		return "", "", err
	}
	return p.Subject, p.Content, nil
}

type aiInput struct {
	lead   *shared.Lead
	hasWeb bool
	prompt *promptRow
	vars   map[string]string
}

func jsonStr(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.Encode(v)
	return strings.TrimSpace(b.String())
}

// compactChecks: the audit checks in a short form for the AI.
func compactChecks(part J) string {
	out := []map[string]any{}
	for _, c := range part.Objects("checks") {
		out = append(out, map[string]any{"area": c.S("cat"), "check": c["label"], "status": c["status"], "detail": c["detail"]})
	}
	return jsonStr(out)
}

// input: everything the AI steps need about a lead (v1 Report::input).
func input(ctx context.Context, a *app.App, q db.Q, s *settings.Store, leadID int64, p *payload) (*aiInput, error) {
	l, err := shared.GetLead(ctx, q, leadID)
	if err != nil {
		return nil, jobs.Permanent(errors.New("Lead not found"))
	}
	raw, _ := db.Val[[]byte](ctx, q, `SELECT audit FROM leads WHERE id=$1`, leadID)
	au, hasWeb := leadAudit(l, raw)
	typ, want := "report_noweb", p.PN
	if hasWeb {
		typ, want = "report_website", p.PW
	}
	var pr *promptRow
	for _, id := range []int64{want, p.PromptID} {
		if id == 0 {
			continue
		}
		if x, err := db.One[promptRow](ctx, q, `SELECT `+promptCols+` FROM prompts WHERE id=$1 AND type LIKE 'report_%'`, id); err == nil && (x.Type == typ || id == want) {
			pr = &x
			break
		}
	}
	if pr == nil {
		if pr, err = activePrompt(ctx, q, typ); err != nil {
			return nil, jobs.Permanent(errors.New("No report prompt found"))
		}
	}
	comp := competitors(ctx, q, l)
	cats := catScores(au, true)
	var cs []string
	for _, k := range sortedKeys(cats) {
		cs = append(cs, fmt.Sprintf("%s %s/100", k, strconv.FormatFloat(cats[k], 'f', -1, 64)))
	}
	ex := map[string]string{"web_score": "N/A", "map_score": "N/A", "web_categories": "n/a", "position": "n/a", "website_audit": "No website",
		"map_audit": compactChecks(au.M("map")), "competitors": "none found", "services": strings.ReplaceAll(s.Get("services", ""), "\n", "; "), "keyword": keywordOf(l)}
	if l.WebScore != nil {
		ex["web_score"] = strconv.Itoa(int(*l.WebScore))
	}
	if l.MapScore != nil {
		ex["map_score"] = strconv.Itoa(int(*l.MapScore))
	}
	if len(cs) > 0 {
		ex["web_categories"] = strings.Join(cs, ", ")
	}
	if l.Position > 0 {
		ex["position"] = strconv.Itoa(l.Position)
	}
	if hasWeb {
		ex["website_audit"] = compactChecks(au.M("website"))
	}
	if len(comp) > 0 {
		ex["competitors"] = jsonStr(comp)
	}
	return &aiInput{lead: l, hasWeb: hasWeb, prompt: pr, vars: shared.LeadVars(ctx, a, q, s, l, ex)}, nil
}

func (r *runner) input(p *payload) (in *aiInput, err error) {
	err = r.tx(func(tx pgx.Tx) error {
		in, err = input(r.ctx, r.a, tx, r.s, r.lead, p)
		return err
	})
	return
}

// ---------------------------------------------------------------- step 1: market research

func researchOn(s *settings.Store) bool { return ai.ResearchOn(s) }

// research: market research brief with sources, shared by businesses of the same kind in the same city
// (global research_cache). Skipped completely when research is switched off in AI Connection.
func (r *runner) research() (J, error) {
	if !researchOn(r.s) {
		r.tr.Step(r.ctx, "Market research is switched off (AI Connection) – skipped, no AI call", "step")
		return nil, nil
	}
	in, err := r.input(&payload{})
	if err != nil {
		return nil, err
	}
	l := in.lead
	prov := ai.ActiveProvider(r.s)
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(keywordOf(l)+"|"+l.City)) + "|" + prov))
	k := hex.EncodeToString(sum[:])
	days := max(0, r.s.Int("research_cache_days", 30))
	pool := r.a.DB.Pool
	if days > 0 {
		c, _ := db.Val[string](r.ctx, pool, `SELECT brief FROM research_cache WHERE k=$1 AND brief<>'' AND created_at>now()-make_interval(days=>$2)`, k, days)
		var b map[string]any
		if c != "" && json.Unmarshal([]byte(c), &b) == nil && len(b) > 0 {
			return J(b), r.prog(66, "Using recent market research for this area (no AI cost)")
		}
		// One research call per business type & city: the first report researches, the others wait and reuse it
		n, _ := db.Exec(r.ctx, pool, `INSERT INTO research_cache (k,brief) VALUES ($1,'') ON CONFLICT (k) DO NOTHING`, k)
		if n == 0 {
			n, _ = db.Exec(r.ctx, pool, `UPDATE research_cache SET brief='', created_at=now() WHERE k=$1 AND (brief<>'' OR created_at<now()-interval '4 minutes')`, k)
		}
		if n == 0 {
			if err := r.prog(61, "Waiting for shared market research (another report of this area is researching it)"); err != nil {
				return nil, err
			}
			return nil, jobs.Defer(20 * time.Second)
		}
	}
	release := func() {
		if days > 0 {
			pool.Exec(context.WithoutCancel(r.ctx), `DELETE FROM research_cache WHERE k=$1 AND brief=''`, k)
		}
	}
	if err := r.prog(62, "AI is researching the market, customers & competitors"); err != nil {
		release()
		return nil, err
	}
	label := "Market research (model knowledge)"
	if prov == "anthropic" {
		label = "Market research (live web search)"
	}
	user := shared.Tpl("Business: {{business_name}} – {{category}} in {{city}} ({{address}}).\nWebsite: {{website}}\nMain Google search analysed: \"{{keyword}}\" (their Maps position #{{position}}, rating {{rating}} from {{reviews}} reviews).\nCompetitors on Google Maps: {{competitors}}\nCurrency: {{currency}}.", in.vars)
	res, err := ai.Complete(r.ctx, r.a, r.org, r.s, ai.Request{System: researchPrompt, Messages: []ai.Msg{{Role: "user", Content: user}}, MaxTokens: 5000,
		WebSearch: true, Searches: 5, Purpose: "research", Label: label, Deadline: 200 * time.Second, Trace: r.tr})
	var b J
	if err != nil {
		if errors.Is(err, shared.ErrAborted) || r.ctx.Err() != nil {
			release()
			return nil, shared.ErrAborted
		}
		slog.Warn("report research", "org", r.org, "lead", r.lead, "err", err)
		r.tr.Step(r.ctx, "Research failed – the report is written without it", "step")
		r.tr.Close(r.ctx, "err", err.Error())
	} else if b = parseReport(res.Text, "facts"); b == nil {
		r.tr.Note(r.ctx, "The research answer had no usable facts – continuing without it")
	}
	if b != nil && days > 0 {
		pool.Exec(r.ctx, `INSERT INTO research_cache (k,brief,created_at) VALUES ($1,$2,now()) ON CONFLICT (k) DO UPDATE SET brief=EXCLUDED.brief, created_at=now()`, k, jsonStr(b))
	} else {
		release()
	}
	return b, nil
}

// ---------------------------------------------------------------- step 2: the AI writes the report

// writeRequest: the user prompt of the report-writing request (v1 Report::writeRequest).
func writeRequest(in *aiInput, brief J, research bool) string {
	u := shared.Tpl(in.prompt.Content, in.vars)
	if len(brief) > 0 {
		u += "\n\nRESEARCH BRIEF (gathered for this report – base market statistics, trends and benchmarks on it and copy its sources into \"sources\"):\n" + jsonStr(brief)
	}
	if !research {
		u += "\n\nIMPORTANT: market research is switched OFF. Do not research or browse the web. Write the report from the audit data above and well-established general knowledge only. Include a statistic only if you are certain of it and its publisher; otherwise leave it out. \"sources\" may be an empty array."
	}
	return u
}

// writeReq: the report-writing AI request of a lead.
func (r *runner) writeReq(p *payload, brief J) (*aiInput, ai.Request, error) {
	in, err := r.input(p)
	if err != nil {
		return nil, ai.Request{}, err
	}
	research := researchOn(r.s)
	user := writeRequest(in, brief, research)
	live := research && len(brief) == 0
	label := "Writing the report"
	if live {
		label += " (with web search)"
	}
	return in, ai.Request{System: systemPrompt, Messages: []ai.Msg{{Role: "user", Content: user}}, MaxTokens: 16000, WebSearch: live, Searches: 3,
		Purpose: "report", Label: label, Deadline: 300 * time.Second, Trace: r.tr}, nil
}

// submitBulk hands the request to the ai bulk engine (v1 Batch::add / Parallel::add).
func (r *runner) submitBulk(p *payload, brief J, mode string) error {
	_, req, err := r.writeReq(p, brief)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Waiting for a free AI slot (%d reports are written at the same time)", ai.Slots(r.s))
	if mode == "batch" {
		msg = "Waiting to go to the AI together with the other reports (batch)"
	}
	if err := r.prog(70, msg); err != nil {
		return err
	}
	n := p.next("save")
	req.Trace = nil
	return r.tx(func(tx pgx.Tx) error { return ai.SubmitBulk(r.ctx, r.a, tx, r.org, r.lead, req, JobReport, n) })
}

// save stores the answer of the bulk engine. When the bulk request failed, the report is written directly.
func (r *runner) save(p *payload, brief J) (int64, error) {
	var res *ai.Response
	err := errors.New(p.AIError)
	if p.AIError == "" && p.AIBatch > 0 {
		err = r.tx(func(tx pgx.Tx) error {
			var e error
			res, e = ai.BulkResult(r.ctx, tx, p.AIBatch)
			return e
		})
	}
	if errors.Is(err, shared.ErrAborted) {
		return 0, err
	}
	if err == nil {
		in, e := r.input(p)
		if e != nil {
			return 0, e
		}
		rid, e := r.finish(res.Text, brief, in.prompt.ID)
		if e != nil {
			return 0, e
		}
		r.tx(func(tx pgx.Tx) error { return ai.BulkDone(r.ctx, tx, p.AIBatch) })
		if rid != 0 {
			return rid, nil
		}
		err = errors.New("the answer was not valid JSON")
	}
	r.tr.Step(r.ctx, "Bulk AI answer not usable ("+trunc(err.Error(), 120)+") – writing this report directly", "step")
	return r.write(p, brief)
}

func (r *runner) write(p *payload, brief J) (int64, error) {
	in, req, err := r.writeReq(p, brief)
	if err != nil {
		return 0, err
	}
	user := req.Messages[0].Content
	if err := r.prog(70, "AI is writing the report (usually 1–3 min)"); err != nil {
		return 0, err
	}
	res, err := ai.Complete(r.ctx, r.a, r.org, r.s, req)
	if err != nil {
		return 0, err
	}
	rid, err := r.finish(res.Text, brief, in.prompt.ID)
	if err != nil || rid != 0 {
		return rid, err
	}
	r.tr.Step(r.ctx, "AI answer could not be read as a report (not valid JSON) – asking again", "step")
	r.tr.Close(r.ctx, "err", trunc(strings.TrimSpace(res.Text), 300))
	if err := r.prog(78, "Tidying the AI answer"); err != nil {
		return 0, err
	}
	req.Messages[0].Content = user + "\n\nIMPORTANT: your previous answer was not valid JSON. Reply with the JSON object only."
	req.Label = "Rewriting – first answer was not valid JSON"
	if res, err = ai.Complete(r.ctx, r.a, r.org, r.s, req); err != nil {
		return 0, err
	}
	if rid, err = r.finish(res.Text, brief, in.prompt.ID); err != nil || rid != 0 {
		return rid, err
	}
	return 0, errors.New("The AI did not return a usable report. Please try again or use a different model.")
}

// finish turns the AI answer into a saved report; 0 when the answer is not usable JSON.
func (r *runner) finish(raw string, brief J, pid int64) (int64, error) {
	d := parseReport(raw, "summary")
	if d == nil {
		return 0, nil
	}
	if len(d.L("sources")) == 0 && len(brief.L("sources")) > 0 {
		d["sources"] = brief["sources"]
	}
	if err := r.prog(84, "Checking that every source link opens"); err != nil {
		return 0, err
	}
	d = verifySources(r.ctx, r.tr, d)
	if err := r.prog(86, "Report written – saving"); err != nil {
		return 0, err
	}
	var id int64
	err := r.tx(func(tx pgx.Tx) error {
		var e error
		id, e = SaveReport(r.ctx, tx, r.lead, pid, d)
		return e
	})
	return id, err
}

// SaveReport stores an AI-written report (JSON) for a lead and returns its id. The public token is
// registered automatically. (Also used by the bulk engine when it saves an answer.)
func SaveReport(ctx context.Context, tx pgx.Tx, leadID, promptID int64, d map[string]any) (int64, error) {
	var pid any
	if promptID > 0 {
		pid = promptID
	}
	return db.Insert(ctx, tx, "reports", map[string]any{"lead_id": leadID, "prompt_id": pid, "token": secure.Token(16), "html": jsonStr(d), "pdf": ""})
}

// verifySources keeps only sources whose link really opens; a source without a working link is left out.
func verifySources(ctx context.Context, tr *shared.Trace, d J) J {
	var src []any
	var urls []string
	for _, s := range d.Objects("sources") {
		if u := strings.TrimSpace(s.S("url")); schemeRe.MatchString(u) {
			src = append(src, map[string]any(s))
			urls = append(urls, u)
		}
	}
	if len(src) > 0 {
		pl := ""
		if len(src) > 1 {
			pl = "s"
		}
		tr.Step(ctx, fmt.Sprintf("Checking %d source link%s", len(src), pl), "step")
		ok := linksOK(ctx, urls)
		var keep []any
		var bad []string
		for i, s := range src {
			if ok[urls[i]] {
				keep = append(keep, s)
			} else {
				bad = append(bad, trunc(urls[i], 70))
			}
		}
		msg := fmt.Sprintf("%d working", len(keep))
		if len(bad) > 0 {
			msg += fmt.Sprintf(" · removed %d broken: %s", len(bad), strings.Join(bad, ", "))
		}
		tr.Close(ctx, "ok", msg)
		src = keep
	}
	if src == nil {
		src = []any{}
	}
	d["sources"] = src
	d["_src"] = 1
	return d
}

var soft404 = regexp.MustCompile(`(?i)<title[^>]*>[^<]{0,120}(404|not found|page (?:does not|doesn.t) exist|page unavailable|no longer available)`)

// linkCheck is replaceable in tests.
var linkCheck = func(ctx context.Context, u string) bool {
	res, err := netx.Do(ctx, netx.Req{URL: u, Public: true, Timeout: 12 * time.Second, Max: 60000,
		Headers: map[string]string{"Accept": "text/html,application/xhtml+xml,application/pdf;q=0.9,*/*;q=0.8", "Accept-Language": "en"}})
	if err != nil {
		return false
	}
	switch res.Code {
	case 401, 403, 429:
		return true
	}
	return res.Code >= 200 && res.Code < 400 && !soft404.Match(res.Body)
}

// linksOK checks links in parallel (v1 links_ok).
func linksOK(ctx context.Context, urls []string) map[string]bool {
	out := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, u := range urls {
		if _, done := out[u]; done {
			continue
		}
		out[u] = false
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ok := linkCheck(ctx, u)
			mu.Lock()
			out[u] = ok
			mu.Unlock()
		}(u)
	}
	wg.Wait()
	return out
}

// ---------------------------------------------------------------- step 3: PDF

// ensurePDF returns the PDF file of a stored report, rendering it when missing.
func ensurePDF(ctx context.Context, a *app.App, orgID, reportID int64) (file, name string, err error) {
	var r reportRow
	var in *RenderIn
	err = a.OrgTx(ctx, orgID, func(tx pgx.Tx, s *settings.Store) error {
		r, err = db.One[reportRow](ctx, tx, `SELECT `+reportCols+` FROM reports WHERE id=$1`, reportID)
		if err != nil {
			return err
		}
		if f := pdfPath(a, orgID, r.PDF); f != "" {
			file = f
		} else if in, err = renderInput(ctx, a, tx, s, orgID, &r, nil); err != nil {
			return err
		}
		ln, _ := db.Val[string](ctx, tx, `SELECT name FROM leads WHERE id=$1`, r.LeadID)
		name = pdfName(ln)
		return nil
	})
	if err != nil || file != "" {
		return
	}
	b, err := renderPDF(ctx, a, in)
	if err != nil {
		return "", "", err
	}
	rel, err := savePDF(a, orgID, r.Token, in.Lead.Name, b)
	if err != nil {
		return "", "", err
	}
	err = a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE reports SET pdf=$2 WHERE id=$1`, r.ID, rel)
		return err
	})
	return pdfPath(a, orgID, rel), name, err
}

// PDF returns the PDF file and download name of a lead's latest report, rendering it when needed
// (used by outreach to attach the report). Returns db.ErrNoRows when the lead has no report.
func PDF(ctx context.Context, a *app.App, orgID, leadID int64) (file, name string, err error) {
	var id int64
	err = a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		id, err = db.Val[int64](ctx, tx, `SELECT id FROM reports WHERE lead_id=$1 ORDER BY id DESC LIMIT 1`, leadID)
		return err
	})
	if err != nil {
		return "", "", err
	}
	if id == 0 {
		return "", "", db.ErrNoRows
	}
	return ensurePDF(ctx, a, orgID, id)
}
