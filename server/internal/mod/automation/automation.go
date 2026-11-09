// Package automation: rule-based automations (trigger → conditions → steps), ready-made recipes,
// runs with a log, and the background engine. See docs/V2_CONVENTIONS.md.
package automation

import (
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
)

func Register(a *app.App) {
	registerEngine(a)

	p := app.Perm("automations")
	a.Get("/automations", list, p)
	a.Post("/automations", create, p)
	a.Get("/automations/recipes", recipesList, p)
	a.Post("/automations/recipes/{key}", recipeInstall, p)
	a.Get("/automations/{id}", get, p)
	a.Put("/automations/{id}", update, p)
	a.Delete("/automations/{id}", remove, p)
	a.Post("/automations/{id}/toggle", toggle, p)
	a.Post("/automations/{id}/duplicate", duplicate, p)
	a.Get("/automations/{id}/runs", runs, p)
	a.Get("/meta/automation", meta, p)

	// Lead page (anyone who may open the lead sees its runs; starting one needs the automations permission)
	a.Get("/leads/{id}/automations", leadRuns, app.Perm("leads"))
	a.Post("/leads/{id}/automations", leadStart, p)
	a.Post("/automation-runs/{id}/cancel", cancelRun, app.Perm("leads"))
}

// ---------------------------------------------------------------- list & editor

type listRow struct {
	Automation
	Act int `db:"act" json:"running"`
	Dn  int `db:"dn" json:"completed"`
}

func list(c *app.Ctx) (any, error) {
	rows, err := db.All[listRow](c, c.Tx(), `SELECT a.id,a.name,a.trig,a.trig_value,a.conditions,a.steps,a.stop_on_reply,a.active,a.runs,a.created_at,
(SELECT count(*) FROM automation_runs r WHERE r.automation_id=a.id AND r.status IN ('running','waiting')) act,
(SELECT count(*) FROM automation_runs r WHERE r.automation_id=a.id AND r.status='done') dn
FROM automations a ORDER BY a.active DESC, a.id DESC`)
	if err != nil {
		return nil, err
	}
	week, err := db.Val[int](c, c.Tx(), `SELECT count(*) FROM automation_runs WHERE created_at>=now()-interval '7 days'`)
	if err != nil {
		return nil, err
	}
	on, run, done := 0, 0, 0
	for _, r := range rows {
		if r.Active {
			on++
		}
		run += r.Act
		done += r.Dn
	}
	return map[string]any{"rows": rows, "stats": map[string]int{"total": len(rows), "on": on, "running": run, "completed": done, "week": week}, "ready": ready(c)}, nil
}

// ready: which channels are connected (for the "connect … for fully automatic follow-ups" hint).
func ready(c *app.Ctx) map[string]bool {
	s := c.S()
	mail := s.Get("mail_api_key", "") != "" || s.Get("smtp_host", "") != ""
	return map[string]bool{"email": mail, "whatsapp": s.Get("wa_provider", "manual") != "manual", "voice": s.Get("voice_provider", "") != ""}
}

func get(c *app.Ctx) (any, error) {
	au, err := db.One[Automation](c, c.Tx(), `SELECT `+autoCols+` FROM automations WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	return au, nil
}

type saveIn struct {
	Name        string           `json:"name"`
	Trig        string           `json:"trig"`
	TrigValue   string           `json:"trig_value"`
	Conditions  []Cond           `json:"conditions"`
	Steps       []map[string]any `json:"steps"`
	StopOnReply *bool            `json:"stop_on_reply"`
	Active      bool             `json:"active"`
}

func (in *saveIn) clean() map[string]any {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Untitled automation"
	}
	if r := []rune(name); len(r) > 150 {
		name = string(r[:150])
	}
	trig := in.Trig
	if !triggerOK(trig) {
		trig = "lead_created"
	}
	tv := strings.TrimSpace(in.TrigValue)
	switch trig {
	case "lead_created", "status_changed", "tag_added", "no_reply":
	default:
		tv = ""
	}
	conds := []Cond{}
	for _, x := range in.Conditions {
		if x.F != "" {
			if x.Op == "" {
				x.Op = "is"
			}
			conds = append(conds, x)
		}
	}
	steps := []map[string]any{}
	for _, s := range in.Steps {
		if actionOK(str(s, "t")) {
			steps = append(steps, s)
		}
	}
	stop := true
	if in.StopOnReply != nil {
		stop = *in.StopOnReply
	}
	return map[string]any{"name": name, "trig": trig, "trig_value": tv, "conditions": conds, "steps": steps, "stop_on_reply": stop, "active": in.Active}
}

func saved(id int64, d map[string]any) map[string]any {
	msg := "Automation saved (off – switch it on when ready)."
	if d["active"].(bool) {
		msg = "Automation saved and switched on."
	}
	out := map[string]any{"ok": true, "id": id, "message": msg}
	if len(d["steps"].([]map[string]any)) == 0 {
		out["warn"] = "Add at least one step."
	}
	return out
}

func create(c *app.Ctx) (any, error) {
	var in saveIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	d := in.clean()
	if err := c.A.CheckLimit(c, c.Tx(), c.Org.ID, "automations", 1); err != nil {
		return nil, err
	}
	id, err := db.Insert(c, c.Tx(), "automations", d)
	if err != nil {
		return nil, err
	}
	return saved(id, d), nil
}

func update(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	var in saveIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if ok, _ := db.Val[bool](c, c.Tx(), `SELECT EXISTS(SELECT 1 FROM automations WHERE id=$1)`, id); !ok {
		return nil, app.NotFound()
	}
	d := in.clean()
	if err := db.Update(c, c.Tx(), "automations", id, d); err != nil {
		return nil, err
	}
	return saved(id, d), nil
}

func remove(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	tx := c.Tx()
	if _, err := tx.Exec(c, `UPDATE jobs SET status='cancelled', finished_at=now(), error='Automation deleted' WHERE type=$2 AND status='pending'
AND ref_id IN (SELECT id FROM automation_runs WHERE automation_id=$1)`, id, jobStep); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(c, `DELETE FROM automation_runs WHERE automation_id=$1`, id); err != nil {
		return nil, err
	}
	n, err := db.Exec(c, tx, `DELETE FROM automations WHERE id=$1`, id)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, app.NotFound()
	}
	return map[string]any{"ok": true, "message": "Automation deleted."}, nil
}

func toggle(c *app.Ctx) (any, error) {
	var in struct {
		On bool `json:"on"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	n, err := db.Exec(c, c.Tx(), `UPDATE automations SET active=$2 WHERE id=$1`, c.ParamID("id"), in.On)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, app.NotFound()
	}
	return map[string]any{"ok": true, "message": "Saved"}, nil
}

func duplicate(c *app.Ctx) (any, error) {
	au, err := db.One[Automation](c, c.Tx(), `SELECT `+autoCols+` FROM automations WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	name := au.Name + " (copy)"
	if r := []rune(name); len(r) > 150 {
		name = string(r[:150])
	}
	if err := c.A.CheckLimit(c, c.Tx(), c.Org.ID, "automations", 1); err != nil {
		return nil, err
	}
	id, err := db.Insert(c, c.Tx(), "automations", map[string]any{"name": name, "trig": au.Trig, "trig_value": au.TrigValue,
		"conditions": au.Conditions, "steps": au.Steps, "stop_on_reply": au.StopOnReply, "active": false})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id, "message": "Automation copied – review it and switch it on."}, nil
}

// ---------------------------------------------------------------- runs

type runOut struct {
	ID           int64      `db:"id" json:"id"`
	AutomationID int64      `db:"automation_id" json:"automation_id"`
	Automation   string     `db:"automation" json:"automation"`
	LeadID       int64      `db:"lead_id" json:"lead_id"`
	Lead         string     `db:"lead" json:"lead"`
	Step         int        `db:"step" json:"step"`
	Steps        int        `db:"steps" json:"steps"`
	Status       string     `db:"status" json:"status"`
	NextAt       *time.Time `db:"next_at" json:"next_at"`
	Log          []LogEntry `db:"log" json:"log"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}

const runSel = `SELECT r.id,r.automation_id,COALESCE(a.name,'') automation,r.lead_id,COALESCE(NULLIF(l.contact_name,''),l.name,'') lead,
r.step,COALESCE(jsonb_array_length(a.steps),0) steps,r.status,CASE WHEN r.status='waiting' THEN r.next_at END next_at,
CASE WHEN jsonb_typeof(r.log)='array' THEN r.log ELSE '[]'::jsonb END log,r.created_at,r.updated_at
FROM automation_runs r LEFT JOIN automations a ON a.id=r.automation_id LEFT JOIN leads l ON l.id=r.lead_id`

func runs(c *app.Ctx) (any, error) {
	rows, err := db.All[runOut](c, c.Tx(), runSel+` WHERE r.automation_id=$1`+c.Scope("l")+` ORDER BY r.id DESC LIMIT 30`, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"rows": rows}, nil
}

// leadAccess loads the lead's access fields and checks a sales person may open it.
func leadAccess(c *app.Ctx, leadID int64) error {
	type acc struct {
		IsLead     bool   `db:"is_lead"`
		AssignedTo *int64 `db:"assigned_to"`
	}
	l, err := db.One[acc](c, c.Tx(), `SELECT is_lead,assigned_to FROM leads WHERE id=$1`, leadID)
	if err != nil {
		return app.NotFound()
	}
	if !c.LeadOK(l.IsLead, l.AssignedTo) {
		return app.Forbidden()
	}
	return nil
}

func leadRuns(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	if err := leadAccess(c, id); err != nil {
		return nil, err
	}
	rows, err := db.All[runOut](c, c.Tx(), runSel+` WHERE r.lead_id=$1 ORDER BY r.id DESC LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"rows": rows, "automations": []any{}}
	if c.Can("automations") {
		type opt struct {
			ID     int64  `db:"id" json:"id"`
			Name   string `db:"name" json:"name"`
			Active bool   `db:"active" json:"active"`
		}
		autos, err := db.All[opt](c, c.Tx(), `SELECT id,name,active FROM automations ORDER BY name`)
		if err != nil {
			return nil, err
		}
		out["automations"] = autos
	}
	return out, nil
}

func leadStart(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	if err := leadAccess(c, id); err != nil {
		return nil, err
	}
	var in struct {
		AutomationID int64 `json:"automation_id"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	active, err := db.One[struct {
		Active bool `db:"active"`
	}](c, c.Tx(), `SELECT active FROM automations WHERE id=$1`, in.AutomationID)
	if err != nil {
		return nil, app.Bad("Choose an automation.")
	}
	if !active.Active {
		return nil, app.Bad("This automation is switched off. Switch it on first.")
	}
	rid, err := Start(c, c.Tx(), c.Org.ID, in.AutomationID, id, true)
	if err != nil {
		return nil, err
	}
	if rid == 0 {
		return nil, app.Bad("This lead does not match the automation conditions.")
	}
	return map[string]any{"ok": true, "id": rid, "message": "Automation started."}, nil
}

func cancelRun(c *app.Ctx) (any, error) {
	type rr struct {
		LeadID int64  `db:"lead_id"`
		Status string `db:"status"`
	}
	r, err := db.One[rr](c, c.Tx(), `SELECT lead_id,status FROM automation_runs WHERE id=$1`, c.ParamID("id"))
	if err != nil {
		return nil, app.NotFound()
	}
	if err := leadAccess(c, r.LeadID); err != nil {
		return nil, err
	}
	if r.Status != "running" && r.Status != "waiting" {
		return map[string]any{"ok": true, "message": "This automation has already ended."}, nil
	}
	if err := CancelRun(c, c.Tx(), c.ParamID("id")); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Automation stopped for this lead."}, nil
}

// ---------------------------------------------------------------- recipes

func recipesList(c *app.Ctx) (any, error) {
	type nm struct {
		Name string `db:"name"`
	}
	names, err := db.All[nm](c, c.Tx(), `SELECT name FROM automations`)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	for _, n := range names {
		used[n.Name] = true
	}
	type out struct {
		Recipe
		Added bool `json:"added"`
	}
	rows := make([]out, 0, len(Recipes))
	for _, r := range Recipes {
		if r.Conds == nil {
			r.Conds = []Cond{}
		}
		rows = append(rows, out{Recipe: r, Added: used[r.Name]})
	}
	return map[string]any{"rows": rows}, nil
}

func recipeInstall(c *app.Ctx) (any, error) {
	r := recipe(c.Param("key"))
	if r == nil {
		return nil, app.NotFound()
	}
	conds := r.Conds
	if conds == nil {
		conds = []Cond{}
	}
	if err := c.A.CheckLimit(c, c.Tx(), c.Org.ID, "automations", 1); err != nil {
		return nil, err
	}
	id, err := db.Insert(c, c.Tx(), "automations", map[string]any{"name": r.Name, "trig": r.Trig, "trig_value": r.TV,
		"conditions": conds, "steps": r.Steps, "stop_on_reply": true, "active": false})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "id": id, "message": "Template added – review it and switch it on."}, nil
}

// ---------------------------------------------------------------- editor metadata

func meta(c *app.Ctx) (any, error) {
	type user struct {
		ID   int64  `db:"id" json:"id"`
		Name string `db:"name" json:"name"`
	}
	users, err := db.All[user](c, c.A.DB.Pool, `SELECT u.id,u.name FROM memberships m JOIN users u ON u.id=m.user_id
WHERE m.org_id=$1 AND m.active AND u.active ORDER BY u.name`, c.Org.ID)
	if err != nil {
		return nil, err
	}
	type field struct {
		Key   string `db:"fkey" json:"key"`
		Label string `db:"label" json:"label"`
	}
	custom, err := db.All[field](c, c.Tx(), `SELECT fkey,label FROM fields ORDER BY sort,id`)
	if err != nil {
		return nil, err
	}
	conds := make([][2]string, 0, len(Conds)+len(custom))
	conds = append(conds, Conds...)
	for _, f := range custom {
		conds = append(conds, [2]string{"custom." + f.Key, f.Label})
	}
	statuses := make([][3]string, len(shared.CRMStatuses))
	copy(statuses, shared.CRMStatuses)
	sources := [][2]string{}
	for _, k := range []string{"maps", "manual", "website", "facebook", "google", "webhook", "import", "booking"} {
		sources = append(sources, [2]string{k, shared.SourceLabel(k)})
	}
	return map[string]any{
		"triggers": Triggers, "actions": Actions, "conds": conds, "ops": Ops, "statuses": statuses, "sources": sources,
		"users": users, "vars": Vars, "ready": ready(c), "prospecting": c.Mod("prospecting"),
	}, nil
}
