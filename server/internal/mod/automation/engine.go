package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/mod/messaging"
	"github.com/narendersinghh/setu/server/internal/mod/prospecting"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Job types and timings of the engine.
const (
	jobStep = "auto_step" // ref = automation_runs.id: execute the steps of a run until it waits or ends
	jobDue  = "auto_due"  // per workspace: queue auto_step for every due run
	jobScan = "auto_scan" // per workspace: time-based triggers (no_reply, follow_up_due)

	lease     = 15 * time.Minute // a run being executed is "leased": next_at = now()+lease (crash recovery)
	stepGuard = 15               // max steps executed in one job before re-queueing (v1 guard)
)

// Cond is one "Only if" condition: field, operator, value.
type Cond struct {
	F  string `json:"f"`
	Op string `json:"op"`
	V  string `json:"v"`
}

// UnmarshalJSON accepts numbers/booleans as values (imported v1 data).
func (c *Cond) UnmarshalJSON(b []byte) error {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	c.F, c.Op, c.V = str(m, "f"), str(m, "op"), str(m, "v")
	return nil
}

// Automation is one row of the automations table.
type Automation struct {
	ID          int64            `db:"id" json:"id"`
	Name        string           `db:"name" json:"name"`
	Trig        string           `db:"trig" json:"trig"`
	TrigValue   string           `db:"trig_value" json:"trig_value"`
	Conditions  []Cond           `db:"conditions" json:"conditions"`
	Steps       []map[string]any `db:"steps" json:"steps"`
	StopOnReply bool             `db:"stop_on_reply" json:"stop_on_reply"`
	Active      bool             `db:"active" json:"active"`
	Runs        int              `db:"runs" json:"runs"`
	CreatedAt   time.Time        `db:"created_at" json:"created_at"`
}

const autoCols = `id,name,trig,trig_value,conditions,steps,stop_on_reply,active,runs,created_at`

// LogEntry is one line of automation_runs.log.
type LogEntry struct {
	At   time.Time `json:"at"`
	Step string    `json:"step"` // step number (1-based) or "end"
	Msg  string    `json:"msg"`
	St   string    `json:"st"` // ok | skip | wait | err | end
}

// ---------------------------------------------------------------- registration of the engine

func registerEngine(a *app.App) {
	for _, ev := range eventTriggers {
		ev := ev
		a.On(ev, func(ctx context.Context, tx pgx.Tx, orgID, leadID int64, data map[string]any) error {
			if ev == "replied" {
				if err := stopOnReply(ctx, tx, leadID); err != nil {
					return err
				}
			}
			return Fire(ctx, a, tx, orgID, ev, leadID, data)
		})
	}
	a.Jobs.Handle(jobStep, 5*time.Minute, func(ctx context.Context, j *jobs.Job) error { return runJob(ctx, a, j.OrgID, j.RefID) })
	a.Jobs.Handle(jobDue, 2*time.Minute, func(ctx context.Context, j *jobs.Job) error { return dueJob(ctx, a, j.OrgID) })
	a.Jobs.Handle(jobScan, 5*time.Minute, func(ctx context.Context, j *jobs.Job) error { return scanJob(ctx, a, j.OrgID) })
	a.Jobs.Every("automation_due", time.Minute, func(ctx context.Context) error { return sweepDue(ctx, a) })
	a.Jobs.Every("automation_scan", 5*time.Minute, func(ctx context.Context) error { return sweepScan(ctx, a) })
}

// ---------------------------------------------------------------- triggers

// Fire starts every active automation of a trigger for a lead (inside the caller's transaction).
func Fire(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, trig string, leadID int64, data map[string]any) error {
	list, err := db.All[Automation](ctx, tx, `SELECT `+autoCols+` FROM automations WHERE active AND trig=$1 ORDER BY id`, trig)
	if err != nil || len(list) == 0 {
		return err
	}
	l, err := shared.GetLead(ctx, tx, leadID)
	if errors.Is(err, db.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	for i := range list {
		au := &list[i]
		if tv := strings.ToLower(strings.TrimSpace(au.TrigValue)); tv != "" {
			var got string
			switch trig {
			case "lead_created":
				got = l.Source
			case "status_changed":
				got = str(data, "status")
				if got == "" {
					got = l.Status
				}
			case "tag_added":
				got = str(data, "tag")
			default:
				got = tv
			}
			if strings.ToLower(strings.TrimSpace(got)) != tv {
				continue
			}
		}
		if _, err := start(ctx, tx, orgID, au, l, false); err != nil {
			return err
		}
	}
	return nil
}

// Start runs an automation for a lead (force = start even if it ran before, e.g. started by hand).
// Returns the run id, or 0 when the lead does not match the conditions.
func Start(ctx context.Context, tx pgx.Tx, orgID, automationID, leadID int64, force bool) (int64, error) {
	au, err := db.One[Automation](ctx, tx, `SELECT `+autoCols+` FROM automations WHERE id=$1`, automationID)
	if err != nil {
		return 0, err
	}
	l, err := shared.GetLead(ctx, tx, leadID)
	if err != nil {
		return 0, err
	}
	return start(ctx, tx, orgID, &au, l, force)
}

func start(ctx context.Context, tx pgx.Tx, orgID int64, au *Automation, l *shared.Lead, force bool) (int64, error) {
	if !match(au.Conditions, l) {
		return 0, nil
	}
	if !force {
		// Not twice at the same time; once-per-lead triggers only ever once (v1).
		dup, err := db.Val[bool](ctx, tx, `SELECT EXISTS(SELECT 1 FROM automation_runs WHERE automation_id=$1 AND lead_id=$2
AND (status IN ('running','waiting') OR $3 IN ('lead_created','report_ready','no_reply')))`, au.ID, l.ID, au.Trig)
		if err != nil || dup {
			return 0, err
		}
	}
	var rid int64
	if err := tx.QueryRow(ctx, `INSERT INTO automation_runs (automation_id,lead_id,status,next_at) VALUES ($1,$2,'running',now()) RETURNING id`, au.ID, l.ID).Scan(&rid); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE automations SET runs=runs+1 WHERE id=$1`, au.ID); err != nil {
		return 0, err
	}
	if err := app.Event(ctx, tx, l.ID, "auto", "Automation started: "+au.Name, "auto", map[string]any{"automation_id": au.ID, "run_id": rid}); err != nil {
		return 0, err
	}
	_, err := jobs.Push(ctx, tx, orgID, jobStep, rid, nil)
	return rid, err
}

// match checks the "Only if" conditions against a lead (v1 Automation::match).
func match(conds []Cond, l *shared.Lead) bool {
	for _, c := range conds {
		v := strings.ToLower(strings.TrimSpace(c.V))
		val := strings.ToLower(strings.TrimSpace(leadField(l, c.F)))
		var ok bool
		switch c.Op {
		case "", "is":
			ok = val == v
		case "not":
			ok = val != v
		case "contains":
			ok = v != "" && strings.Contains(val, v)
		case "empty":
			ok = val == ""
		case "not_empty":
			ok = val != ""
		default:
			ok = true
		}
		if !ok {
			return false
		}
	}
	return true
}

func leadField(l *shared.Lead, f string) string {
	if k, ok := strings.CutPrefix(f, "custom."); ok {
		return anyStr(l.Custom[k])
	}
	switch f {
	case "source":
		return l.Source
	case "status":
		return l.Status
	case "city":
		return l.City
	case "service":
		return l.Service
	case "tags":
		return l.Tags
	case "email":
		return l.Email
	case "phone":
		return l.Phone
	case "website":
		return l.Website
	case "category":
		return l.Category
	case "name":
		return l.Name
	case "contact_name":
		return l.ContactName
	case "stage":
		return l.Stage
	case "address":
		return l.Address
	case "keyword":
		return l.Keyword
	}
	return ""
}

// stopOnReply ends the running flows of a lead whose automation stops on reply (v1 onReply).
func stopOnReply(ctx context.Context, tx pgx.Tx, leadID int64) error {
	ids, err := db.All[struct {
		ID int64 `db:"id"`
	}](ctx, tx, `SELECT r.id FROM automation_runs r JOIN automations a ON a.id=r.automation_id
WHERE r.lead_id=$1 AND a.stop_on_reply AND r.status IN ('running','waiting')`, leadID)
	if err != nil {
		return err
	}
	for _, r := range ids {
		if err := endRun(ctx, tx, r.ID, "stopped", "Stopped – lead replied"); err != nil {
			return err
		}
	}
	return nil
}

// endRun finishes a run with a status and a last log line, and cancels its queued jobs.
func endRun(ctx context.Context, q db.Q, rid int64, status, msg string) error {
	if _, err := q.Exec(ctx, `UPDATE jobs SET status='cancelled', finished_at=now(), error=$2 WHERE type=$3 AND ref_id=$1 AND status='pending'`, rid, msg, jobStep); err != nil {
		return err
	}
	return appendLog(ctx, q, rid, LogEntry{Step: "end", Msg: msg, St: "end"}, map[string]any{"status": status, "next_at": nil})
}

// appendLog adds a log line and updates the run.
func appendLog(ctx context.Context, q db.Q, rid int64, e LogEntry, set map[string]any) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	b, _ := json.Marshal([]LogEntry{e})
	sql, args := `UPDATE automation_runs SET log=log||$2::jsonb, updated_at=now()`, []any{rid, string(b)}
	for _, k := range []string{"status", "step", "next_at"} {
		if v, ok := set[k]; ok {
			args = append(args, v)
			sql += fmt.Sprintf(", %s=$%d", k, len(args))
		}
	}
	_, err := q.Exec(ctx, sql+` WHERE id=$1`, args...)
	return err
}

// CancelRun stops a run by hand (lead page).
func CancelRun(ctx context.Context, tx pgx.Tx, rid int64) error {
	return endRun(ctx, tx, rid, "cancelled", "Cancelled manually")
}

// ---------------------------------------------------------------- execution (background jobs)

type runRow struct {
	ID          int64            `db:"id"`
	LeadID      int64            `db:"lead_id"`
	Step        int              `db:"step"`
	Status      string           `db:"status"`
	CreatedAt   time.Time        `db:"created_at"`
	Name        string           `db:"name"`
	Steps       []map[string]any `db:"steps"`
	Active      bool             `db:"active"`
	StopOnReply bool             `db:"stop_on_reply"`
}

// runJob claims a due run (lease) and executes its steps until it waits or ends.
func runJob(ctx context.Context, a *app.App, orgID, rid int64) error {
	var claimed bool
	err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		n, err := db.Exec(ctx, tx, `UPDATE automation_runs SET status='running', next_at=now()+$2::interval, updated_at=now()
WHERE id=$1 AND status IN ('running','waiting') AND (next_at IS NULL OR next_at<=now())`, rid, fmt.Sprintf("%d seconds", int(lease.Seconds())))
		claimed = n > 0
		return err
	})
	if err != nil || !claimed {
		return err
	}
	for i := 0; i < stepGuard; i++ {
		stop, err := advance(ctx, a, orgID, rid)
		if err != nil {
			// Infrastructure error (database): release the lease so the job retry can continue.
			a.DB.Org(context.WithoutCancel(ctx), orgID, func(tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `UPDATE automation_runs SET next_at=now() WHERE id=$1 AND status='running'`, rid)
				return e
			})
			return err
		}
		if stop {
			return nil
		}
	}
	// Long flow: continue in a new job so other work gets a turn.
	return a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE automation_runs SET next_at=now() WHERE id=$1 AND status='running'`, rid); err != nil {
			return err
		}
		_, err := jobs.Push(ctx, tx, orgID, jobStep, rid, nil)
		return err
	})
}

// stepCtx is everything a step needs (loaded in a short transaction).
type stepCtx struct {
	a    *app.App
	org  int64
	s    *settings.Store
	lead *shared.Lead
	vars map[string]string
	step map[string]any
	name string
	n    int // 1-based step number
	tz   *time.Location
}

type result struct {
	msg  string
	st   string // ok | skip | wait | err
	wait time.Time
	meta map[string]any
}

// advance executes one step of a run. stop = the run waits or ended.
func advance(ctx context.Context, a *app.App, orgID, rid int64) (stop bool, err error) {
	var sc *stepCtx
	err = a.OrgTx(ctx, orgID, func(tx pgx.Tx, s *settings.Store) error {
		r, err := db.One[runRow](ctx, tx, `SELECT r.id,r.lead_id,r.step,r.status,r.created_at,a.name,a.steps,a.active,a.stop_on_reply
FROM automation_runs r JOIN automations a ON a.id=r.automation_id WHERE r.id=$1`, rid)
		if errors.Is(err, db.ErrNoRows) {
			stop = true // automation deleted
			return nil
		}
		if err != nil {
			return err
		}
		if r.Status != "running" {
			stop = true
			return nil
		}
		if !r.Active {
			stop = true
			return endRun(ctx, tx, rid, "cancelled", "Automation turned off")
		}
		l, err := shared.GetLead(ctx, tx, r.LeadID)
		if errors.Is(err, db.ErrNoRows) {
			stop = true
			return endRun(ctx, tx, rid, "cancelled", "Lead deleted")
		}
		if err != nil {
			return err
		}
		if r.StopOnReply && l.RepliedAt != nil && !l.RepliedAt.Before(r.CreatedAt) {
			stop = true
			return endRun(ctx, tx, rid, "stopped", "Stopped – lead replied")
		}
		if r.Step >= len(r.Steps) {
			stop = true
			if err := endRun(ctx, tx, rid, "done", "Completed"); err != nil {
				return err
			}
			return app.Event(ctx, tx, l.ID, "auto", "Automation finished: "+r.Name, "auto", map[string]any{"run_id": rid})
		}
		sc = &stepCtx{a: a, org: orgID, s: s, lead: l, step: r.Steps[r.Step], name: r.Name, n: r.Step + 1,
			vars: shared.LeadVars(ctx, a, tx, s, l, nil), tz: app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))}
		return nil
	})
	if err != nil || sc == nil {
		return stop, err
	}

	// The step itself runs outside any transaction (external calls can be slow).
	res, serr := execStep(ctx, sc)

	err = a.DB.Org(context.WithoutCancel(ctx), orgID, func(tx pgx.Tx) error {
		label := actionLabel(str(sc.step, "t"))
		if serr != nil {
			msg := errText(serr)
			stop = true
			if err := appendLog(ctx, tx, rid, LogEntry{Step: strconv.Itoa(sc.n), Msg: label + " failed: " + msg, St: "err"}, map[string]any{"step": sc.n}); err != nil {
				return err
			}
			if err := endRun(ctx, tx, rid, "failed", "Error: "+msg); err != nil {
				return err
			}
			return app.Event(ctx, tx, sc.lead.ID, "error", `Automation "`+sc.name+`" failed: `+msg, "auto", map[string]any{"run_id": rid, "step": sc.n})
		}
		set := map[string]any{"step": sc.n, "status": "running"}
		if !res.wait.IsZero() {
			set["status"], set["next_at"] = "waiting", res.wait
			stop = true
		}
		if err := appendLog(ctx, tx, rid, LogEntry{Step: strconv.Itoa(sc.n), Msg: res.msg, St: res.st}, set); err != nil {
			return err
		}
		meta := map[string]any{"run_id": rid, "step": sc.n, "type": str(sc.step, "t")}
		for k, v := range res.meta {
			meta[k] = v
		}
		return app.Event(ctx, tx, sc.lead.ID, "auto", sc.name+" – step "+strconv.Itoa(sc.n)+": "+res.msg, "auto", meta)
	})
	return stop, err
}

func errText(err error) string {
	m := err.Error()
	if len([]rune(m)) > 300 {
		m = string([]rune(m)[:300]) + "…"
	}
	return m
}

func actionLabel(t string) string {
	for _, x := range Actions {
		if x.Key == t {
			return x.Label
		}
	}
	return "Step"
}

func ok(msg string) result   { return result{msg: msg, st: "ok"} }
func skip(msg string) result { return result{msg: msg, st: "skip"} }

var urlRe = regexp.MustCompile(`(?i)^https?://`)

// execStep runs one step (v1 Automation::run). An error fails the run.
func execStep(ctx context.Context, sc *stepCtx) (result, error) {
	a, s, l, st, vars := sc.a, sc.s, sc.lead, sc.step, sc.vars
	t := str(st, "t")
	switch t {
	case "wait":
		n := max(1, num(st, "n", 1))
		u := str(st, "u")
		mult := map[string]time.Duration{"minutes": time.Minute, "hours": time.Hour, "days": 24 * time.Hour}
		d, okU := mult[u]
		if !okU {
			u, d = "hours", time.Hour
		}
		return result{msg: "Waiting " + unitText(n, u), st: "wait", wait: time.Now().Add(time.Duration(n) * d)}, nil

	case "whatsapp", "booking":
		ch := str(st, "ch")
		if ch == "" {
			ch = "whatsapp"
		}
		byEmail := t == "booking" && ch == "email"
		if l.Phone == "" && !byEmail {
			return skip("Skipped WhatsApp – no phone number"), nil
		}
		text := shared.Tpl(str(st, "text"), vars)
		if t == "booking" && vars["booking_link"] != "" && !strings.Contains(text, vars["booking_link"]) {
			text = strings.TrimRight(text, "\n ") + "\n\n" + vars["booking_link"]
		}
		if byEmail {
			if l.Email == "" {
				return skip("Skipped booking email – no email address or email not set up"), nil
			}
			sub := str(st, "subject")
			if sub == "" {
				sub = "Book a time with {{agency_name}}"
			}
			if _, err := messaging.SendEmail(ctx, a, sc.org, s, messaging.Email{LeadID: l.ID, To: l.Email, Subject: shared.Tpl(sub, vars), Body: text, Track: true}); err != nil {
				return result{}, err
			}
			return ok("Booking link emailed"), nil
		}
		w := messaging.WhatsApp{LeadID: l.ID, Phone: l.Phone, Text: text}
		if truthy(st, "attach") {
			w.Attachment = reportPDF(ctx, sc)
		}
		_, link, err := messaging.SendWhatsApp(ctx, a, sc.org, s, w)
		if err != nil {
			return result{}, err
		}
		if link != "" {
			r := ok("WhatsApp prepared (manual mode)")
			r.meta = map[string]any{"link": link}
			return r, nil
		}
		if t == "booking" {
			return ok("Booking link sent by WhatsApp"), nil
		}
		return ok("WhatsApp sent"), nil

	case "email":
		if l.Email == "" {
			return skip("Skipped email – no email address"), nil
		}
		e := messaging.Email{LeadID: l.ID, To: l.Email, Subject: shared.Tpl(str(st, "subject"), vars), Body: shared.Tpl(str(st, "body"), vars), Track: true}
		if truthy(st, "attach") {
			if att := reportPDF(ctx, sc); att != nil {
				e.Attachments = []messaging.Attachment{*att}
			}
		}
		if _, err := messaging.SendEmail(ctx, a, sc.org, s, e); err != nil {
			return result{}, err
		}
		return ok("Email sent: " + e.Subject), nil

	case "call":
		if l.Phone == "" {
			return skip("Skipped call – no phone number"), nil
		}
		id, err := messaging.StartCall(ctx, a, sc.org, s, messaging.Call{LeadID: l.ID, Task: shared.Tpl(str(st, "task"), vars), FirstMessage: shared.Tpl(str(st, "first"), vars)})
		if err != nil {
			return result{}, err
		}
		return ok(fmt.Sprintf("AI call started (#%d)", id)), nil

	case "status":
		v := str(st, "v")
		if v == "" {
			v = "new"
		}
		if err := a.DB.Org(ctx, sc.org, func(tx pgx.Tx) error {
			return crm.SetStatus(ctx, a, tx, sc.org, l.ID, v, "by automation")
		}); err != nil {
			return result{}, err
		}
		return ok("Status → " + shared.StatusLabel(v)), nil

	case "tag":
		v := strings.TrimSpace(str(st, "v"))
		if v == "" {
			return skip("Skipped tag – no tag name"), nil
		}
		if err := a.DB.Org(ctx, sc.org, func(tx pgx.Tx) error { return crm.AddTag(ctx, a, tx, sc.org, l.ID, v) }); err != nil {
			return result{}, err
		}
		return ok("Tag " + v), nil

	case "assign":
		uid := int64(num(st, "v", 0))
		name := ""
		if uid > 0 {
			name, _ = db.Val[string](ctx, a.DB.Pool, `SELECT u.name FROM users u JOIN memberships m ON m.user_id=u.id WHERE u.id=$1 AND m.org_id=$2 AND m.active`, uid, sc.org)
		}
		var owner any
		if name != "" {
			owner = uid
		} else {
			name = "nobody"
		}
		if err := a.DB.Org(ctx, sc.org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE leads SET assigned_to=$2 WHERE id=$1`, l.ID, owner); err != nil {
				return err
			}
			return app.Event(ctx, tx, l.ID, "note", "Assigned to "+name, "", nil)
		}); err != nil {
			return result{}, err
		}
		return ok("Assigned to " + name), nil

	case "followup":
		n := max(1, num(st, "n", 1))
		d := 24 * time.Hour
		if str(st, "u") == "hours" {
			d = time.Hour
		}
		at := time.Now().Add(time.Duration(n) * d)
		when := at.In(sc.tz).Format("02 Jan, 03:04 PM")
		if err := a.DB.Org(ctx, sc.org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `UPDATE leads SET follow_up_at=$2 WHERE id=$1`, l.ID, at); err != nil {
				return err
			}
			return app.Event(ctx, tx, l.ID, "note", "Follow-up set for "+when, "", nil)
		}); err != nil {
			return result{}, err
		}
		return ok("Follow-up " + when), nil

	case "notify":
		to := strings.TrimSpace(str(st, "to"))
		if to == "" {
			to = s.Get("company_email", "")
		}
		if to == "" {
			return skip("Skipped team email – email not set up"), nil
		}
		text := str(st, "text")
		if text == "" {
			text = "New lead: {{business_name}} – {{phone}} {{email}}"
		}
		sub := str(st, "subject")
		if sub == "" {
			sub = "Lead update – {{business_name}}"
		}
		body := shared.Tpl(text, vars) + "\n\n" + a.Cfg.AppURL() + "/leads/" + strconv.FormatInt(l.ID, 10)
		// A team notification is not a message to the lead: LeadID 0 keeps it out of the lead's conversation.
		if _, err := messaging.SendEmail(ctx, a, sc.org, s, messaging.Email{To: to, Subject: a.BrandName(ctx) + ": " + shared.Tpl(sub, vars), Body: body}); err != nil {
			return skip("Skipped team email – " + errText(err)), nil
		}
		return ok("Team notified (" + to + ")"), nil

	case "report":
		steps := []string{"audit", "report"}
		if truthy(st, "contact") {
			steps = append(steps, "contact")
		}
		if err := a.DB.Org(ctx, sc.org, func(tx pgx.Tx) error {
			_, err := prospecting.EnqueueProcess(ctx, a, tx, sc.org, []int64{l.ID}, steps)
			return err
		}); err != nil {
			return result{}, err
		}
		return ok("Audit & report started"), nil

	case "webhook":
		u := strings.TrimSpace(str(st, "url"))
		if !urlRe.MatchString(u) {
			return skip("Skipped webhook – invalid URL"), nil
		}
		data := map[string]any{"id": l.ID, "name": l.Name, "contact_name": l.ContactName, "email": l.Email, "phone": l.Phone, "city": l.City,
			"website": l.Website, "source": l.Source, "status": l.Status, "tags": l.Tags, "service": l.Service, "deal_value": l.DealValue,
			"custom": l.Custom, "link": a.Cfg.AppURL() + "/leads/" + strconv.FormatInt(l.ID, 10)}
		r, err := netx.Do(ctx, netx.Req{Method: "POST", URL: u, JSON: data, Timeout: 15 * time.Second, Max: 1 << 20, Public: true})
		if err != nil {
			return result{msg: "Webhook HTTP 0 – " + errText(err), st: "err"}, nil
		}
		res := result{msg: "Webhook HTTP " + strconv.Itoa(r.Code), st: "ok"}
		if !r.OK() {
			res.st = "err"
		}
		return res, nil
	}
	return skip("Unknown step " + t), nil
}

// reportPDF is the latest report of the lead as a PDF attachment (nil when there is none).
func reportPDF(ctx context.Context, sc *stepCtx) *messaging.Attachment {
	var tok string
	sc.a.DB.Org(ctx, sc.org, func(tx pgx.Tx) error {
		tok, _ = db.Val[string](ctx, tx, `SELECT token FROM reports WHERE lead_id=$1 ORDER BY id DESC LIMIT 1`, sc.lead.ID)
		return nil
	})
	if tok == "" {
		return nil
	}
	name := strings.Trim(regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(sc.lead.Name, "-"), "-")
	return &messaging.Attachment{Name: "Audit-Report-" + name + ".pdf", Mime: "application/pdf", URL: sc.a.Cfg.AppURL() + "/r/" + tok + ".pdf"}
}

func unitText(n int, u string) string {
	if n == 1 {
		u = strings.TrimSuffix(u, "s")
	}
	return strconv.Itoa(n) + " " + u
}

// ---------------------------------------------------------------- periodic tasks

// sweepDue finds workspaces with due runs (one indexed query) and queues one auto_due job each.
func sweepDue(ctx context.Context, a *app.App) error {
	_, err := a.DB.Pool.Exec(ctx, `INSERT INTO jobs (org_id,type,ref_id,payload,max_attempts)
SELECT d.org_id, $1, 0, '{}', 1 FROM (SELECT DISTINCT org_id FROM automation_runs
  WHERE status IN ('running','waiting') AND next_at<=now() LIMIT 10000) d
WHERE NOT EXISTS (SELECT 1 FROM jobs j WHERE j.org_id=d.org_id AND j.type=$1 AND j.ref_id=0 AND j.status IN ('pending','running'))`, jobDue)
	return err
}

// dueJob queues auto_step for every due run of a workspace that has no step job yet.
func dueJob(ctx context.Context, a *app.App, orgID int64) error {
	return a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO jobs (org_id,type,ref_id,payload)
SELECT r.org_id, $1, r.id, '{}' FROM automation_runs r
WHERE r.status IN ('running','waiting') AND r.next_at<=now()
  AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.org_id=r.org_id AND j.type=$1 AND j.ref_id=r.id AND j.status IN ('pending','running'))
ORDER BY r.next_at LIMIT 1000`, jobStep)
		return err
	})
}

// sweepScan queues one auto_scan job per workspace that has time-based automations switched on.
func sweepScan(ctx context.Context, a *app.App) error {
	_, err := a.DB.Pool.Exec(ctx, `INSERT INTO jobs (org_id,type,ref_id,payload,max_attempts)
SELECT d.org_id, $1, 0, '{}', 1 FROM (SELECT DISTINCT org_id FROM automations WHERE active AND trig IN ('no_reply','follow_up_due')) d
WHERE NOT EXISTS (SELECT 1 FROM jobs j WHERE j.org_id=d.org_id AND j.type=$1 AND j.status IN ('pending','running'))`, jobScan)
	return err
}

// scanJob starts time-based automations for matching leads (v1 Automation::scan).
func scanJob(ctx context.Context, a *app.App, orgID int64) error {
	return a.DB.Org(ctx, orgID, func(tx pgx.Tx) error { return Scan(ctx, tx, orgID) })
}

// Scan runs the time-based triggers of one workspace inside its transaction.
func Scan(ctx context.Context, tx pgx.Tx, orgID int64) error {
	list, err := db.All[Automation](ctx, tx, `SELECT `+autoCols+` FROM automations WHERE active AND trig IN ('no_reply','follow_up_due') ORDER BY id`)
	if err != nil {
		return err
	}
	type idRow struct {
		ID int64 `db:"id"`
	}
	for i := range list {
		au := &list[i]
		var rows []idRow
		if au.Trig == "no_reply" {
			d, _ := strconv.Atoi(strings.TrimSpace(au.TrigValue))
			rows, err = db.All[idRow](ctx, tx, `SELECT l.id FROM leads l WHERE l.replied_at IS NULL AND l.stage NOT IN ('won','lost') AND l.status NOT IN ('won','lost')
AND EXISTS (SELECT 1 FROM messages m WHERE m.lead_id=l.id AND m.direction='out' AND m.created_at < now()-make_interval(days => $2))
AND NOT EXISTS (SELECT 1 FROM automation_runs r WHERE r.lead_id=l.id AND r.automation_id=$1) LIMIT 50`, au.ID, max(1, d))
		} else {
			rows, err = db.All[idRow](ctx, tx, `SELECT l.id FROM leads l WHERE l.follow_up_at IS NOT NULL AND l.follow_up_at<=now() AND l.follow_up_at>now()-interval '2 days'
AND NOT EXISTS (SELECT 1 FROM automation_runs r WHERE r.lead_id=l.id AND r.automation_id=$1 AND r.created_at>=l.follow_up_at) LIMIT 50`, au.ID)
		}
		if err != nil {
			return err
		}
		for _, r := range rows {
			l, err := shared.GetLead(ctx, tx, r.ID)
			if err != nil {
				continue
			}
			if _, err := start(ctx, tx, orgID, au, l, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- small helpers for loose JSON

func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	return anyStr(m[k])
}

func anyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		if x {
			return "1"
		}
		return ""
	}
	return fmt.Sprint(v)
}

func num(m map[string]any, k string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(str(m, k))); err == nil {
		return n
	}
	return def
}

func truthy(m map[string]any, k string) bool {
	v := str(m, k)
	return v != "" && v != "0" && v != "false"
}
