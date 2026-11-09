package crm

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
)

func registerLeads(a *app.App) {
	p := app.Perm("leads")
	a.Get("/leads", leadList, p)
	a.Post("/leads", leadCreate, p)
	a.Get("/leads/export", leadExport, app.Perm("settings"))
	a.Post("/leads/import", leadImport, app.Perm("settings"))
	a.Post("/leads/bulk", leadBulk, p)
	a.Get("/leads/{id}", leadGet, p)
	a.Put("/leads/{id}", leadUpdate, p)
	a.Delete("/leads/{id}", leadDelete, app.Perm("settings"))
	a.Get("/leads/{id}/journey", leadJourney, p)
	a.Post("/leads/{id}/note", leadNote, p)
	a.Post("/leads/{id}/status", leadStatus, p)
	a.Post("/leads/{id}/tags", leadTag, p)
	a.Post("/leads/{id}/won", leadWon, p)
	a.Post("/leads/{id}/lost", leadLost, p)
	a.Post("/leads/{id}/replied", leadReplied, p)
	a.Post("/leads/{id}/promote", leadPromote, app.Perm("prospecting"), app.Module("prospecting"))
	a.Get("/meta/crm", meta, p)
	a.Get("/events", activity, app.Perm("dashboard"))
}

// ---------------------------------------------------------------- query builder

type qb struct {
	w    []string
	args []any
}

func (q *qb) arg(v any) string { q.args = append(q.args, v); return "$" + strconv.Itoa(len(q.args)) }
func (q *qb) and(s string)     { q.w = append(q.w, s) }
func (q *qb) where() string    { return strings.Join(q.w, " AND ") }

func scopeSQL(c *app.Ctx, alias string) string {
	return strings.TrimPrefix(c.Scope(alias), " AND ")
}

func likeEsc(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

// searchExpr matches the leads_crm_search index expression.
const searchExpr = `(l.name || ' ' || l.contact_name || ' ' || l.email || ' ' || l.phone || ' ' || l.city || ' ' || l.tags || ' ' || (l.custom::text))`

const followDue = `l.follow_up_at IS NOT NULL AND l.follow_up_at<=now() AND l.status NOT IN ('won','lost')`

// leadFilter builds the WHERE of the leads list (v1 leads.php filters). withStatus=false for the board.
func leadFilter(c *app.Ctx, withStatus bool) *qb {
	q := &qb{}
	q.and("l.is_lead")
	if s := scopeSQL(c, "l"); s != "" {
		q.and(s)
	}
	if s := c.Query("q"); s != "" {
		q.and(searchExpr + " ILIKE " + q.arg(likeEsc(s)))
	}
	if st := c.Query("status"); withStatus && ValidStatus(st) {
		q.and("l.status=" + q.arg(st))
	}
	if src := c.Query("src"); src != "" {
		if _, ok := shared.LeadSources[src]; ok {
			q.and("l.source=" + q.arg(src))
		}
	}
	if t := c.Query("tag"); t != "" {
		q.and("l.tags ILIKE " + q.arg(likeEsc(t)))
	}
	switch own := c.Query("own"); own {
	case "":
	case "me":
		q.and("l.assigned_to=" + q.arg(c.User.ID))
	case "none":
		q.and("l.assigned_to IS NULL")
	default:
		if n, _ := strconv.ParseInt(own, 10, 64); n > 0 && c.Can("settings") {
			q.and("l.assigned_to=" + q.arg(n))
		}
	}
	if c.Query("fu") == "due" {
		q.and(followDue)
	}
	return q
}

type leadRow struct {
	ID           int64      `db:"id" json:"id"`
	Name         string     `db:"name" json:"name"`
	ContactName  string     `db:"contact_name" json:"contact_name"`
	City         string     `db:"city" json:"city"`
	Service      string     `db:"service" json:"service"`
	Tags         string     `db:"tags" json:"tags"`
	Phone        string     `db:"phone" json:"phone"`
	Email        string     `db:"email" json:"email"`
	Source       string     `db:"source" json:"source"`
	SourceDetail string     `db:"source_detail" json:"source_detail"`
	Status       string     `db:"status" json:"status"`
	DealValue    *float64   `db:"deal_value" json:"deal_value"`
	FollowUpAt   *time.Time `db:"follow_up_at" json:"follow_up_at"`
	AssignedTo   *int64     `db:"assigned_to" json:"assigned_to"`
	Owner        *string    `db:"owner" json:"owner"`
	LastActivity *time.Time `db:"last_activity" json:"last_activity"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
}

const rowCols = `l.id,l.name,l.contact_name,l.city,l.service,l.tags,l.phone,l.email,l.source,l.source_detail,l.status,l.deal_value::float8 deal_value,l.follow_up_at,l.assigned_to,u.name owner,l.last_activity,l.created_at`

var sorts = map[string]string{
	"":         "COALESCE(l.lead_at,l.created_at) DESC, l.id DESC",
	"old":      "COALESCE(l.lead_at,l.created_at) ASC, l.id ASC",
	"activity": "COALESCE(l.last_activity,l.created_at) DESC, l.id DESC",
	"follow":   "l.follow_up_at ASC NULLS LAST, l.id DESC",
	"deal":     "l.deal_value DESC NULLS LAST, l.id DESC",
	"name":     "lower(COALESCE(NULLIF(l.contact_name,''),l.name)), l.id",
}

func dayStart(c *app.Ctx) time.Time {
	n := time.Now().In(c.TZ())
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
}

// leadStats: KPIs, status counts (for the source filter) and source counts in one scan.
func leadStats(c *app.Ctx, src string) (map[string]any, error) {
	type g struct {
		Status string  `db:"status"`
		Source string  `db:"source"`
		N      int64   `db:"n"`
		Today  int64   `db:"td"`
		Due    int64   `db:"due"`
		Un     int64   `db:"un"`
		Deal   float64 `db:"dv"`
	}
	sc := c.Scope("l")
	rows, err := db.All[g](c, c.Tx(), `SELECT l.status, l.source, count(*) n,
 count(*) FILTER (WHERE l.created_at>=$1) td,
 count(*) FILTER (WHERE `+followDue+`) due,
 count(*) FILTER (WHERE l.assigned_to IS NULL) un,
 COALESCE(sum(l.deal_value),0)::float8 dv
FROM leads l WHERE l.is_lead`+sc+` GROUP BY 1,2`, dayStart(c))
	if err != nil {
		return nil, err
	}
	var t, nw, td, wn, due, un int64
	var wv, pv float64
	st, so := map[string]int64{}, map[string]int64{}
	for _, r := range rows {
		t += r.N
		td += r.Today
		due += r.Due
		un += r.Un
		switch r.Status {
		case "won":
			wn += r.N
			wv += r.Deal
		case "lost":
		default:
			pv += r.Deal
		}
		if r.Status == "new" {
			nw += r.N
		}
		so[r.Source] += r.N
		if src == "" || src == r.Source {
			st[r.Status] += r.N
		}
	}
	return map[string]any{
		"kpi":     map[string]any{"total": t, "new": nw, "today": td, "won": wn, "won_value": wv, "pipeline": pv, "due": due, "unassigned": un},
		"status":  st,
		"sources": so,
	}, nil
}

func leadList(c *app.Ctx) (any, error) {
	tx := c.Tx()
	if c.Query("view") == "board" {
		return leadBoard(c)
	}
	q := leadFilter(c, true)
	w := q.where()
	per := c.QueryInt("per", 50)
	if per != 25 && per != 100 {
		per = 50
	}
	total, err := db.Val[int64](c, tx, `SELECT count(*) FROM leads l WHERE `+w, q.args...)
	if err != nil {
		return nil, err
	}
	pages := max(1, int((total+int64(per)-1)/int64(per)))
	pg := max(1, min(c.QueryInt("p", 1), pages))
	order, ok := sorts[c.Query("sort")]
	if !ok {
		order = sorts[""]
	}
	rows, err := db.All[leadRow](c, tx, `SELECT `+rowCols+` FROM leads l LEFT JOIN users u ON u.id=l.assigned_to WHERE `+w+` ORDER BY `+order+` LIMIT `+strconv.Itoa(per)+` OFFSET `+strconv.Itoa((pg-1)*per), q.args...)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"rows": rows, "total": total, "page": pg, "pages": pages, "per": per}
	if c.Query("stats") != "0" {
		s, err := leadStats(c, c.Query("src"))
		if err != nil {
			return nil, err
		}
		for k, v := range s {
			out[k] = v
		}
	}
	return out, nil
}

// leadBoard: pipeline board – the newest 50 leads of every status plus the count per status.
func leadBoard(c *app.Ctx) (any, error) {
	tx := c.Tx()
	q := leadFilter(c, false)
	w := q.where()
	limit := min(max(c.QueryInt("limit", 50), 10), 200)
	type cnt struct {
		Status string `db:"status"`
		N      int64  `db:"n"`
	}
	counts, err := db.All[cnt](c, tx, `SELECT l.status, count(*) n FROM leads l WHERE `+w+` GROUP BY 1`, q.args...)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(shared.CRMStatuses))
	for i, s := range shared.CRMStatuses {
		keys[i] = s[0]
	}
	sa := q.arg(keys)
	rows, err := db.All[leadRow](c, tx, `SELECT x.* FROM unnest(`+sa+`::text[]) s(st) CROSS JOIN LATERAL (
 SELECT `+rowCols+` FROM leads l LEFT JOIN users u ON u.id=l.assigned_to WHERE `+w+` AND l.status=s.st
 ORDER BY COALESCE(l.lead_at,l.created_at) DESC, l.id DESC LIMIT `+strconv.Itoa(limit)+`) x`, q.args...)
	if err != nil {
		return nil, err
	}
	cm := map[string]int64{}
	for _, x := range counts {
		cm[x.Status] = x.N
	}
	cols := map[string][]leadRow{}
	for _, k := range keys {
		cols[k] = []leadRow{}
	}
	for _, r := range rows {
		cols[r.Status] = append(cols[r.Status], r)
	}
	return map[string]any{"columns": cols, "counts": cm, "limit": limit}, nil
}

// ---------------------------------------------------------------- one lead

var errAssigned = app.Err(403, "That lead is assigned to someone else.")

// loadLead loads a lead the signed-in user may open (v1 lead_ok + prospecting access).
func loadLead(c *app.Ctx, id int64) (*shared.Lead, error) {
	l, err := shared.GetLead(c, c.Tx(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, app.Err(404, "Record not found.")
		}
		return nil, err
	}
	if !c.LeadOK(l.IsLead, l.AssignedTo) {
		return nil, errAssigned
	}
	if !l.IsLead && (!c.Can("prospecting") || !c.Mod("prospecting")) {
		return nil, app.Forbidden()
	}
	return l, nil
}

func leadGet(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	tx := c.Tx()
	fs, err := Fields(c, tx)
	if err != nil {
		return nil, err
	}
	var owner string
	if l.AssignedTo != nil {
		owner, _ = db.Val[string](c, tx, `SELECT name FROM users WHERE id=$1`, *l.AssignedTo)
	}
	counts, err := db.Map(c, tx, `SELECT
 (SELECT count(*) FROM events WHERE lead_id=$1) journey,
 (SELECT count(*) FROM messages WHERE lead_id=$1) messages,
 (SELECT count(*) FROM appointments WHERE lead_id=$1) appointments,
 (SELECT count(*) FROM calls WHERE lead_id=$1) calls,
 (SELECT count(*) FROM automation_runs WHERE lead_id=$1) automations`, l.ID)
	if err != nil {
		return nil, err
	}
	next, err := db.Map(c, tx, `SELECT id,title,start_at,meet_link FROM appointments WHERE lead_id=$1 AND status='scheduled' AND end_at>=now() ORDER BY start_at LIMIT 1`, l.ID)
	if err != nil {
		return nil, err
	}
	var report map[string]any
	if c.Can("prospecting") && c.Mod("prospecting") {
		if report, err = db.Map(c, tx, `SELECT id,token,views,created_at FROM reports WHERE lead_id=$1 ORDER BY id DESC LIMIT 1`, l.ID); err != nil {
			return nil, err
		}
		if report != nil {
			report["url"] = c.BaseURL() + "/r/" + report["token"].(string)
		}
	}
	location, _ := db.Val[string](c, tx, `SELECT location FROM campaigns WHERE id=$1`, l.CampaignID)
	if l.Custom == nil {
		l.Custom = map[string]any{}
	}
	return map[string]any{
		"lead": l, "owner": owner, "fields": fs, "counts": counts, "next_appt": next, "report": report, "location": location,
		"booking_link": shared.BookingLink(c.A, l),
		"wa_phone":     shared.PhoneNorm(l.Phone, c.S().Get("default_country_code", "91")),
		"busy":         l.Stage == "queued" || l.Stage == "auditing" || l.Stage == "reporting" || l.Stage == "contacting",
	}, nil
}

func leadJourney(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	lim := min(max(c.QueryInt("limit", 250), 1), 1000)
	return db.Maps(c, c.Tx(), `SELECT id,type,channel,message,meta,created_at FROM events WHERE lead_id=$1 ORDER BY id DESC LIMIT `+strconv.Itoa(lim), l.ID)
}

func str(v any) string {
	if v == nil {
		return ""
	}
	s, _ := scalar(v)
	return strings.TrimSpace(s)
}

func parseDeal(v any) (any, error) {
	s := str(v)
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || f >= 1e10 {
		return nil, app.Bad("Enter a valid deal value.")
	}
	return f, nil
}

func parseFollow(c *app.Ctx, v any) (any, error) {
	s := str(v)
	if s == "" {
		return nil, nil
	}
	t, ok := ParseTime(s, c.TZ())
	if !ok {
		return nil, app.Bad("Enter a valid follow-up date.")
	}
	return t, nil
}

// memberOK checks an owner belongs to the workspace.
func memberOK(c *app.Ctx, uid int64) bool {
	ok, _ := db.Val[bool](c, c.A.DB.Pool, `SELECT EXISTS(SELECT 1 FROM memberships WHERE org_id=$1 AND user_id=$2 AND active)`, c.Org.ID, uid)
	return ok
}

func ownerArg(c *app.Ctx, v any) (any, error) {
	n, _ := strconv.ParseInt(str(v), 10, 64)
	if n <= 0 {
		return nil, nil
	}
	if !memberOK(c, n) {
		return nil, app.Bad("Pick an active team member as owner.")
	}
	return n, nil
}

func leadUpdate(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	var in map[string]any
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	tx := c.Tx()
	u := map[string]any{}
	for _, k := range append(append([]string{}, Core...), "notes") {
		v, ok := in[k]
		if !ok {
			continue
		}
		switch k {
		case "deal_value":
			if u[k], err = parseDeal(v); err != nil {
				return nil, err
			}
		case "follow_up_at":
			if u[k], err = parseFollow(c, v); err != nil {
				return nil, err
			}
		case "message", "notes":
			u[k] = cut(str(v), 20000)
		case "tags":
			u[k] = cut(strings.Join(SplitTags(str(v)), ", "), 490)
		default:
			u[k] = cut(str(v), 250)
		}
	}
	if v, ok := u["name"]; ok && v == "" {
		n := str(in["contact_name"])
		if n == "" {
			n = l.ContactName
		}
		if n == "" {
			return nil, app.Bad("Enter the contact person or the business name.")
		}
		u["name"] = n
	}
	if v, ok := in["assigned_to"]; ok && c.Can("settings") {
		if u["assigned_to"], err = ownerArg(c, v); err != nil {
			return nil, err
		}
	}
	if p, ok := u["phone"]; ok && p != l.Phone {
		if p == "" {
			u["phone_source"] = ""
		} else {
			u["phone_source"] = "manual"
		}
	}
	if err := db.Update(c, tx, "leads", l.ID, u); err != nil {
		return nil, err
	}
	if cf, ok := in["custom"].(map[string]any); ok {
		if err := SaveCustom(c, tx, l.ID, cf); err != nil {
			return nil, err
		}
	}
	if t, ok := u["tags"].(string); ok {
		old := map[string]bool{}
		for _, x := range SplitTags(l.Tags) {
			old[x] = true
		}
		for _, x := range SplitTags(t) {
			if !old[x] {
				c.A.Emit(c, tx, c.Org.ID, "tag_added", l.ID, map[string]any{"tag": x})
			}
		}
	}
	if st := str(in["status"]); st != "" && st != l.Status {
		if err := SetStatus(c, c.A, tx, c.Org.ID, l.ID, st, ""); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "id": l.ID}, app.Event(c, tx, l.ID, "note", "Details updated", "", nil)
}

func leadCreate(c *app.Ctx) (any, error) {
	var in map[string]any
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	d := map[string]string{}
	for _, k := range Core {
		if v := str(in[k]); v != "" {
			d[k] = v
		}
	}
	if d["name"] == "" && d["contact_name"] == "" {
		return nil, app.Bad("Enter the contact person or the business name.")
	}
	if d["phone"] == "" && d["email"] == "" {
		return nil, app.Bad("Enter a phone number or an email so you can reach this lead.")
	}
	if _, err := parseDeal(in["deal_value"]); err != nil {
		return nil, err
	}
	if v, err := parseFollow(c, in["follow_up_at"]); err != nil {
		return nil, err
	} else if t, ok := v.(time.Time); ok {
		d["follow_up_at"] = t.Format(time.RFC3339)
	}
	tx := c.Tx()
	cf, _ := in["custom"].(map[string]any)
	fs, err := Fields(c, tx)
	if err != nil {
		return nil, err
	}
	for _, f := range fs {
		if f.Required && str(cf[f.Key]) == "" {
			return nil, app.Bad("Fill in " + f.Label + ".")
		}
	}
	var owner any = c.User.ID
	if c.Can("settings") {
		if owner, err = ownerArg(c, in["assigned_to"]); err != nil {
			return nil, err
		}
	}
	id, err := Create(c, c.A, tx, c.Org.ID, CreateIn{Fields: d, Source: "manual", Detail: c.User.Name})
	if err != nil {
		return nil, err
	}
	if err := SaveCustom(c, tx, id, cf); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(c, `UPDATE leads SET assigned_to=$2, notes=$3 WHERE id=$1`, id, owner, cut(str(in["notes"]), 20000)); err != nil {
		return nil, err
	}
	if st := str(in["status"]); st != "" && st != "new" {
		if err := SetStatus(c, c.A, tx, c.Org.ID, id, st, ""); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "id": id, "message": "Lead added."}, nil
}

func leadDelete(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	if err := deleteLeads(c, []int64{l.ID}); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Deleted.", "is_lead": l.IsLead}, nil
}

// deleteLeads removes leads and all their history (v1 Outreach::bulk delete).
func deleteLeads(c *app.Ctx, ids []int64) error {
	tx := c.Tx()
	for _, s := range []string{
		`DELETE FROM reports WHERE lead_id=ANY($1)`,
		`DELETE FROM events WHERE lead_id=ANY($1)`,
		`DELETE FROM messages WHERE lead_id=ANY($1)`,
		`DELETE FROM jobs WHERE type NOT IN ('search','auto') AND ref_id=ANY($1)`,
		`DELETE FROM appointments WHERE lead_id=ANY($1)`,
		`DELETE FROM calls WHERE lead_id=ANY($1)`,
		`DELETE FROM automation_runs WHERE lead_id=ANY($1)`,
		`DELETE FROM job_steps WHERE lead_id=ANY($1)`,
		`DELETE FROM ai_batch WHERE lead_id=ANY($1)`,
		`DELETE FROM public_tokens WHERE kind='lead' AND ref_id=ANY($1)`,
		`DELETE FROM leads WHERE id=ANY($1)`,
	} {
		if _, err := tx.Exec(c, s, ids); err != nil {
			return err
		}
	}
	return nil
}

func leadNote(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	var in struct {
		Note string `json:"note"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if in.Note = strings.TrimSpace(in.Note); in.Note == "" {
		return nil, app.Bad("Write a note first.")
	}
	return nil, app.Event(c, c.Tx(), l.ID, "note", in.Note, "", nil)
}

func leadStatus(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	var in struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Status updated."}, SetStatus(c, c.A, c.Tx(), c.Org.ID, l.ID, in.Status, in.Note)
}

func leadTag(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	var in struct {
		Tag string `json:"tag"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	return nil, AddTag(c, c.A, c.Tx(), c.Org.ID, l.ID, in.Tag)
}

// setStage moves the outreach stage and writes the journey (v1 stage()).
func setStage(c *app.Ctx, id int64, stage, msg string) error {
	if _, err := c.Tx().Exec(c, `UPDATE leads SET stage=$2 WHERE id=$1`, id, stage); err != nil {
		return err
	}
	return app.Event(c, c.Tx(), id, "stage", msg, "", map[string]string{"stage": stage})
}

func leadWon(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	if err := setStage(c, l.ID, "won", "Marked as Won"); err != nil {
		return nil, err
	}
	if err := Promote(c, c.A, c.Tx(), c.Org.ID, l.ID, "won"); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Marked as won 🎉"}, SetStatus(c, c.A, c.Tx(), c.Org.ID, l.ID, "won", "")
}

func leadLost(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	if err := setStage(c, l.ID, "lost", "Marked as Not interested"); err != nil {
		return nil, err
	}
	if l.IsLead {
		if err := SetStatus(c, c.A, c.Tx(), c.Org.ID, l.ID, "lost", ""); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "message": "Marked as not interested."}, nil
}

func leadPromote(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Moved to Leads."}, Promote(c, c.A, c.Tx(), c.Org.ID, l.ID, "new")
}

// leadReplied logs a reply by hand (v1 Outreach::markReplied); emits "replied".
func leadReplied(c *app.Ctx) (any, error) {
	l, err := loadLead(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	var in struct {
		Via string `json:"via"`
		Msg string `json:"msg"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	switch in.Via {
	case "email", "whatsapp", "call", "other":
	default:
		in.Via = "other"
	}
	if l.RepliedAt != nil {
		return map[string]any{"ok": true, "message": "A reply was already logged for this lead."}, nil
	}
	tx := c.Tx()
	if _, err := tx.Exec(c, `UPDATE leads SET replied_at=now(), replied_via=$2, stage='replied' WHERE id=$1`, l.ID, in.Via); err != nil {
		return nil, err
	}
	msg := "Replied via " + in.Via
	if m := strings.TrimSpace(in.Msg); m != "" {
		msg += ": " + cut(m, 300)
	}
	if err := app.Event(c, tx, l.ID, "replied", msg, in.Via, nil); err != nil {
		return nil, err
	}
	if c.S().Get("promote_on_reply", "0") == "1" {
		if err := Promote(c, c.A, tx, c.Org.ID, l.ID, "contacted"); err != nil {
			return nil, err
		}
	}
	c.A.Emit(c, tx, c.Org.ID, "replied", l.ID, map[string]any{"via": in.Via})
	return map[string]any{"ok": true, "message": "Reply logged."}, nil
}

// ---------------------------------------------------------------- bulk

func leadBulk(c *app.Ctx) (any, error) {
	var in struct {
		Action string  `json:"action"`
		IDs    []int64 `json:"ids"`
		Value  string  `json:"value"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if len(in.IDs) == 0 {
		return nil, app.Bad("Select at least one lead.")
	}
	if len(in.IDs) > 5000 {
		return nil, app.Bad("Select at most 5,000 leads at a time.")
	}
	tx := c.Tx()
	adm := c.Can("settings")
	ids, err := db.All[int64ID](c, tx, `SELECT l.id FROM leads l WHERE l.id=ANY($1)`+c.Scope("l")+` ORDER BY l.id`, in.IDs)
	if err != nil {
		return nil, err
	}
	list := make([]int64, len(ids))
	for i, x := range ids {
		list[i] = x.ID
	}
	n := len(list)
	switch in.Action {
	case "status":
		if !ValidStatus(in.Value) {
			return nil, app.Bad("Unknown status.")
		}
		for _, id := range list {
			if err := SetStatus(c, c.A, tx, c.Org.ID, id, in.Value, ""); err != nil {
				return nil, err
			}
		}
		return map[string]any{"ok": true, "n": n, "message": strconv.Itoa(n) + " lead(s) updated."}, nil
	case "owner":
		if !adm {
			return nil, app.Forbidden()
		}
		owner, err := ownerArg(c, in.Value)
		if err != nil {
			return nil, err
		}
		name := "nobody"
		if owner != nil {
			name, _ = db.Val[string](c, tx, `SELECT name FROM users WHERE id=$1`, owner)
		}
		if _, err := tx.Exec(c, `UPDATE leads SET assigned_to=$2, last_activity=now() WHERE id=ANY($1)`, list, owner); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(c, `INSERT INTO events (lead_id,type,message) SELECT x,'note',$2 FROM unnest($1::bigint[]) x`, list, "Owner changed to "+name); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "n": n, "message": strconv.Itoa(n) + " lead(s) reassigned."}, nil
	case "tag":
		if strings.TrimSpace(in.Value) == "" {
			return nil, app.Bad("Enter a tag.")
		}
		for _, id := range list {
			if err := AddTag(c, c.A, tx, c.Org.ID, id, in.Value); err != nil {
				return nil, err
			}
		}
		return map[string]any{"ok": true, "n": n, "message": "Tag added to " + strconv.Itoa(n) + " lead(s)."}, nil
	case "delete":
		if !adm {
			return nil, app.Forbidden()
		}
		if err := deleteLeads(c, list); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "n": n, "message": strconv.Itoa(n) + " lead(s) deleted."}, nil
	}
	return nil, app.Bad("You cannot do that.")
}

type int64ID struct {
	ID int64 `db:"id"`
}

// ---------------------------------------------------------------- meta & activity

// Users returns the active team members (owner pickers).
func users(c *app.Ctx) ([]map[string]any, error) {
	return db.Maps(c, c.A.DB.Pool, `SELECT u.id,u.name,m.role FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.org_id=$1 AND m.active AND u.active ORDER BY u.name`, c.Org.ID)
}

func meta(c *app.Ctx) (any, error) {
	var us []map[string]any
	var err error
	if c.Can("settings") {
		if us, err = users(c); err != nil {
			return nil, err
		}
	}
	fs, err := Fields(c, c.Tx())
	if err != nil {
		return nil, err
	}
	statuses := make([]map[string]string, len(shared.CRMStatuses))
	for i, s := range shared.CRMStatuses {
		statuses[i] = map[string]string{"key": s[0], "label": s[1], "color": s[2]}
	}
	src := make([]map[string]string, 0, len(shared.LeadSources))
	for _, k := range []string{"maps", "manual", "website", "facebook", "google", "webhook", "import", "booking"} {
		s := shared.LeadSources[k]
		src = append(src, map[string]string{"key": k, "label": s[0], "color": s[1], "icon": s[2]})
	}
	stages := map[string]map[string]string{}
	for k, s := range shared.Stages {
		stages[k] = map[string]string{"label": s[0], "color": s[1]}
	}
	var services []string
	for _, s := range strings.Split(c.S().Get("services", ""), "\n") {
		if s = strings.TrimSpace(s); s != "" {
			services = append(services, s)
		}
	}
	return map[string]any{
		"statuses": statuses, "sources": src, "stages": stages, "users": us, "fields": fs, "services": services,
		"cc": c.S().Get("default_country_code", "91"), "currency_symbol": c.S().Get("currency_symbol", "₹"),
		"admin": c.Can("settings"), "me": c.User.ID,
	}, nil
}

// activity: every journey entry across leads (v1 activity.php), 50 per page.
func activity(c *app.Ctx) (any, error) {
	q := &qb{}
	q.and("true")
	if !(c.Mod("prospecting") && c.Can("prospecting")) {
		q.and("l.is_lead")
	}
	if s := scopeSQL(c, "l"); s != "" {
		q.and(s)
	}
	if t := c.Query("type"); t != "" {
		q.and("e.type=" + q.arg(t))
	}
	w := q.where()
	tx := c.Tx()
	// Counting millions of events is slow; the pager stops at 10,000
	total, err := db.Val[int64](c, tx, `SELECT count(*) FROM (SELECT 1 FROM events e JOIN leads l ON l.id=e.lead_id WHERE `+w+` LIMIT 10000) x`, q.args...)
	if err != nil {
		return nil, err
	}
	per := 50
	pages := max(1, int((total+49)/50))
	pg := max(1, min(c.QueryInt("p", 1), pages))
	rows, err := db.Maps(c, tx, `SELECT e.id,e.lead_id,e.type,e.channel,e.message,e.created_at,l.name,l.contact_name FROM events e JOIN leads l ON l.id=e.lead_id WHERE `+w+` ORDER BY e.id DESC LIMIT 50 OFFSET `+strconv.Itoa((pg-1)*per), q.args...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"rows": rows, "total": total, "page": pg, "pages": pages, "capped": total >= 10000}, nil
}
