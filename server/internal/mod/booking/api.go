package booking

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
)

// Appt is an appointment row for the browser. Local fields are in the workspace time zone.
type Appt struct {
	ID          int64     `db:"id" json:"id"`
	LeadID      int64     `db:"lead_id" json:"lead_id"`
	UserID      *int64    `db:"user_id" json:"user_id"`
	Title       string    `db:"title" json:"title"`
	StartAt     time.Time `db:"start_at" json:"start_at"`
	EndAt       time.Time `db:"end_at" json:"end_at"`
	Status      string    `db:"status" json:"status"`
	Location    string    `db:"location" json:"location"`
	Notes       string    `db:"notes" json:"notes"`
	BookedBy    string    `db:"booked_by" json:"booked_by"`
	Reminded    bool      `db:"reminded" json:"reminded"`
	MeetLink    string    `db:"meet_link" json:"meet_link"`
	InGCal      bool      `db:"in_gcal" json:"in_gcal"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	Name        string    `db:"name" json:"name"`
	ContactName string    `db:"contact_name" json:"contact_name"`
	Phone       string    `db:"phone" json:"phone"`
	Email       string    `db:"email" json:"email"`
	UserName    string    `db:"user_name" json:"user_name"`
	// workspace-local presentation
	Day   string `json:"day"`   // 2006-01-02
	Time  string `json:"time"`  // 03:04 PM
	Local string `json:"local"` // 2006-01-02T15:04 (datetime-local input)
	Mins  int    `json:"mins"`
}

const apptCols = `a.id,a.lead_id,a.user_id,a.title,a.start_at,a.end_at,a.status,a.location,a.notes,a.booked_by,a.reminded,a.meet_link,a.gcal_id<>'' in_gcal,a.created_at,
COALESCE(l.name,'') name,COALESCE(l.contact_name,'') contact_name,COALESCE(l.phone,'') phone,COALESCE(l.email,'') email,COALESCE(u.name,'') user_name
FROM appointments a LEFT JOIN leads l ON l.id=a.lead_id LEFT JOIN users u ON u.id=a.user_id`

// scope: sales people see appointments they own, of their leads and of unassigned leads (v1).
func scope(c *app.Ctx) string {
	if c.Role() != "sales" {
		return ""
	}
	id := c.User.ID
	return fmt.Sprintf(" AND (a.user_id=%d OR l.assigned_to=%d OR l.assigned_to IS NULL)", id, id)
}

func localize(rows []Appt, loc *time.Location) {
	for i := range rows {
		t := rows[i].StartAt.In(loc)
		rows[i].Day = t.Format("2006-01-02")
		rows[i].Time = t.Format("03:04 PM")
		rows[i].Local = t.Format("2006-01-02T15:04")
		rows[i].Mins = int(rows[i].EndAt.Sub(rows[i].StartAt).Round(time.Minute).Minutes())
	}
}

func loadAppt(c *app.Ctx, id int64) (*Appt, error) {
	rows, err := db.All[Appt](c, c.Tx(), `SELECT `+apptCols+` WHERE a.id=$1`+scope(c), id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, app.NotFound()
	}
	localize(rows, c.TZ())
	return &rows[0], nil
}

// GET /api/appointments?from&to&status&q&user&lead&page&per – list and calendar range.
func apptList(c *app.Ctx) (any, error) {
	loc := c.TZ()
	where, args := " WHERE 1=1"+scope(c), []any{}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	if d, err := time.ParseInLocation("2006-01-02", c.Query("from"), loc); err == nil {
		where += " AND a.start_at>=" + arg(d)
	}
	if d, err := time.ParseInLocation("2006-01-02", c.Query("to"), loc); err == nil {
		where += " AND a.start_at<" + arg(d.AddDate(0, 0, 1))
	}
	switch st := c.Query("status"); st {
	case "scheduled", "completed", "no_show", "cancelled":
		where += " AND a.status=" + arg(st)
	case "upcoming":
		where += " AND a.status='scheduled' AND a.start_at>=now()"
	case "past":
		where += " AND a.start_at<now()"
	}
	if q := c.Query("q"); q != "" {
		p := "%" + strings.ToLower(q) + "%"
		x := arg(p)
		where += " AND (lower(a.title) LIKE " + x + " OR lower(COALESCE(l.name,'')) LIKE " + x + " OR lower(COALESCE(l.contact_name,'')) LIKE " + x + " OR lower(COALESCE(l.email,'')) LIKE " + x + " OR COALESCE(l.phone,'') LIKE " + x + ")"
	}
	if u := c.QueryInt("user", 0); u > 0 {
		where += " AND a.user_id=" + arg(int64(u))
	}
	if b := c.Query("by"); b == "lead" || b == "admin" {
		where += " AND a.booked_by=" + arg(b)
	}
	if l := c.QueryInt("lead", 0); l > 0 {
		where += " AND a.lead_id=" + arg(int64(l))
	}
	per := min(max(c.QueryInt("per", 50), 1), 1000)
	page := max(c.QueryInt("page", 1), 1)
	total, err := db.Val[int64](c, c.Tx(), `SELECT count(*) FROM appointments a LEFT JOIN leads l ON l.id=a.lead_id`+where, args...)
	if err != nil {
		return nil, err
	}
	order := " ORDER BY a.start_at"
	if c.Query("sort") == "desc" {
		order = " ORDER BY a.start_at DESC"
	}
	rows, err := db.All[Appt](c, c.Tx(), `SELECT `+apptCols+where+order+fmt.Sprintf(" LIMIT %d OFFSET %d", per, (page-1)*per), args...)
	if err != nil {
		return nil, err
	}
	localize(rows, loc)
	return map[string]any{"rows": rows, "total": total, "page": page, "pages": (total + int64(per) - 1) / int64(per)}, nil
}

func apptGet(c *app.Ctx) (any, error) { return loadAppt(c, c.ParamID("id")) }

// GET /api/appointments/summary – KPIs, next appointments, booking link and setup (v1 page header).
func apptSummary(c *app.Ctx) (any, error) {
	loc, s := c.TZ(), c.S()
	sc := scope(c)
	var k struct {
		Up, Today, Done, NoShow, Self int64
	}
	today := todayIn(loc)
	if err := c.Tx().QueryRow(c, `SELECT count(*) FILTER (WHERE a.status='scheduled' AND a.start_at>=now()),
count(*) FILTER (WHERE a.status='scheduled' AND a.start_at>=$1 AND a.start_at<$2),
count(*) FILTER (WHERE a.status='completed'), count(*) FILTER (WHERE a.status='no_show'), count(*) FILTER (WHERE a.booked_by='lead')
FROM appointments a LEFT JOIN leads l ON l.id=a.lead_id WHERE 1=1`+sc, today, today.AddDate(0, 0, 1)).Scan(&k.Up, &k.Today, &k.Done, &k.NoShow, &k.Self); err != nil {
		return nil, err
	}
	up, err := db.All[Appt](c, c.Tx(), `SELECT `+apptCols+` WHERE a.status='scheduled' AND a.start_at>=now()-interval '1 hour'`+sc+` ORDER BY a.start_at LIMIT 12`)
	if err != nil {
		return nil, err
	}
	localize(up, loc)
	type grp struct {
		Label string `json:"label"`
		Rows  []Appt `json:"rows"`
	}
	groups := []grp{}
	tStr, tmStr := today.Format("2006-01-02"), today.AddDate(0, 0, 1).Format("2006-01-02")
	for _, x := range up {
		lb := x.StartAt.In(loc).Format("Monday, 02 Jan")
		if x.Day == tStr {
			lb = "Today"
		} else if x.Day == tmStr {
			lb = "Tomorrow"
		}
		if len(groups) == 0 || groups[len(groups)-1].Label != lb {
			groups = append(groups, grp{Label: lb})
		}
		groups[len(groups)-1].Rows = append(groups[len(groups)-1].Rows, x)
	}
	key, err := ensureKey(c, c.Tx(), c.Org.ID, s)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kpi":      map[string]int64{"today": k.Today, "upcoming": k.Up, "completed": k.Done, "no_show": k.NoShow, "self_booked": k.Self},
		"upcoming": groups, "link": c.A.Cfg.AppURL() + "/book/g/" + key,
		"setup": map[string]any{"start": s.Get("appt_start", "10:00"), "end": s.Get("appt_end", "18:00"), "slot": slotLen(s), "remind": s.Get("appt_remind", "") == "1",
			"notify": s.Get("appt_notify", "") == "1", "gcal": gcalReady(s), "title": apptTitle(s, "Meeting"), "location": s.Get("appt_location", "")},
		"today": tStr, "now": time.Now().In(loc).Format("2006-01-02T15:04"), "tz": loc.String(),
	}, nil
}

// GET /api/appointments/slots?days=14 – free slots as the booking page shows them.
func apptSlots(c *app.Ctx) (any, error) {
	days := min(max(c.QueryInt("days", 14), 1), 60)
	ds, s, err := loadSlots(c, c.A, c.Org.ID, days)
	if err != nil {
		return nil, err
	}
	loc := app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))
	type slot struct {
		T     int64  `json:"t"`
		Label string `json:"label"`
		Local string `json:"local"`
	}
	type day struct {
		Date  string `json:"date"`
		Label string `json:"label"`
		Slots []slot `json:"slots"`
	}
	out := []day{}
	for _, d := range ds {
		x := day{Date: d.Date, Label: d.Times[0].In(loc).Format("Mon, 02 Jan")}
		for _, t := range d.Times {
			x.Slots = append(x.Slots, slot{T: t.Unix(), Label: t.In(loc).Format("03:04 PM"), Local: t.In(loc).Format("2006-01-02T15:04")})
		}
		out = append(out, x)
	}
	return map[string]any{"days": out, "slot": slotLen(s), "gcal": gcalReady(s)}, nil
}

// GET /api/appointments/leads?q – leads for the "Book a meeting" picker (own + unassigned for sales).
func apptLeads(c *app.Ctx) (any, error) {
	where, args := "", []any{}
	if q := c.Query("q"); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		where = " AND (lower(l.name) LIKE $1 OR lower(l.contact_name) LIKE $1 OR lower(l.email) LIKE $1 OR l.phone LIKE $1)"
	}
	return db.Maps(c, c.Tx(), `SELECT l.id, l.name, l.contact_name, l.phone, l.email FROM leads l WHERE l.is_lead`+c.Scope("l")+where+` ORDER BY COALESCE(l.lead_at,l.created_at) DESC LIMIT 400`, args...)
}

// GET /api/leads/{id}/appointments
func leadAppointments(c *app.Ctx) (any, error) {
	l, err := shared.GetLead(c, c.Tx(), c.ParamID("id"))
	if errors.Is(err, db.ErrNoRows) {
		return nil, app.NotFound()
	}
	if err != nil {
		return nil, err
	}
	if !c.LeadOK(l.IsLead, l.AssignedTo) {
		return nil, app.Forbidden()
	}
	rows, err := db.All[Appt](c, c.Tx(), `SELECT `+apptCols+` WHERE a.lead_id=$1 ORDER BY a.start_at DESC`, l.ID)
	if err != nil {
		return nil, err
	}
	localize(rows, c.TZ())
	return rows, nil
}

type apptIn struct {
	LeadID   int64   `json:"lead_id"`
	Name     string  `json:"name"`
	Phone    string  `json:"phone"`
	Email    string  `json:"email"`
	Company  string  `json:"company"`
	At       string  `json:"at"` // 2006-01-02T15:04 in the workspace time zone (or RFC 3339)
	Slot     int64   `json:"slot"`
	Minutes  int     `json:"minutes"`
	Title    *string `json:"title"`
	Notes    *string `json:"notes"`
	Location *string `json:"location"`
	UserID   *int64  `json:"user_id"`
	Status   string  `json:"status"`
}

func parseAt(v string, slot int64, loc *time.Location) (time.Time, bool) {
	if slot > 0 {
		return time.Unix(slot, 0), true
	}
	v = strings.TrimSpace(v)
	for _, f := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(f, v, loc); err == nil {
			return t, true
		}
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// owner: who the appointment belongs to. Admins may pick a team member; sales people book for themselves.
func owner(c *app.Ctx, in *int64) (*int64, error) {
	me := c.User.ID
	if in == nil || *in == 0 || c.Role() == "sales" {
		return &me, nil
	}
	ok, err := db.Val[bool](c, c.A.DB.Pool, `SELECT EXISTS(SELECT 1 FROM memberships WHERE org_id=$1 AND user_id=$2 AND active)`, c.Org.ID, *in)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, app.Bad("Choose a member of your team.")
	}
	return in, nil
}

// POST /api/appointments – book a meeting for a lead or a new person (v1 appointments.php "add").
func apptCreate(c *app.Ctx) (any, error) {
	var in apptIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	s := c.S()
	t, ok := parseAt(in.At, in.Slot, c.TZ())
	if !ok {
		return nil, app.Bad("Pick a date and time.")
	}
	uid, err := owner(c, in.UserID)
	if err != nil {
		return nil, err
	}
	tx := c.Tx()
	lid := in.LeadID
	if lid > 0 {
		l, err := shared.GetLead(c, tx, lid)
		if err != nil || !c.LeadOK(l.IsLead, l.AssignedTo) {
			return nil, app.Bad("Choose one of your leads.")
		}
	} else {
		n := clip(in.Name, 190)
		if n == "" {
			return nil, app.Bad("Choose a lead or enter a name.")
		}
		biz := clip(in.Company, 250)
		if biz == "" {
			biz = n
		}
		var assign int64
		if c.Role() == "sales" {
			assign = c.User.ID
		}
		lid, err = createLead(c, c.A, tx, c.Org.ID, crm.CreateIn{Fields: map[string]string{"contact_name": n, "name": biz, "phone": clip(in.Phone, 60), "email": clip(in.Email, 190)},
			Source: "manual", Detail: "Appointment", UserID: assign})
		if err != nil {
			return nil, err
		}
	}
	id, err := book(c, c.A, tx, s, c.Org.ID, bookIn{LeadID: lid, Start: t, Minutes: in.Minutes, By: "admin", Notes: str(in.Notes), Title: str(in.Title), Location: str(in.Location), UserID: uid})
	if err != nil {
		return nil, err
	}
	org := c.Org.ID
	c.After(func() { afterBook(context.Background(), c.A, org, id) })
	msg := "Appointment booked."
	if gcalReady(s) {
		msg = "Appointment booked and added to Google Calendar."
	}
	return map[string]any{"ok": true, "id": id, "lead_id": lid, "message": msg}, nil
}

// PUT /api/appointments/{id} – edit time, title, notes, location, owner or status.
func apptUpdate(c *app.Ctx) (any, error) {
	var in apptIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	cur, err := loadAppt(c, c.ParamID("id"))
	if err != nil {
		return nil, err
	}
	tx := c.Tx()
	loc := c.TZ()
	set := map[string]any{}
	moved := false
	if in.At != "" || in.Slot > 0 || in.Minutes > 0 {
		start := cur.StartAt
		if in.At != "" || in.Slot > 0 {
			t, ok := parseAt(in.At, in.Slot, loc)
			if !ok {
				return nil, app.Bad("Pick a date and time.")
			}
			start = t
		}
		mins := cur.Mins
		if in.Minutes > 0 {
			mins = min(in.Minutes, 24*60)
		}
		end := start.Add(time.Duration(mins) * time.Minute)
		if !start.Equal(cur.StartAt) || !end.Equal(cur.EndAt) {
			if _, err := tx.Exec(c, `SELECT pg_advisory_xact_lock(7301, ($1::bigint % 2147483647)::int)`, c.Org.ID); err != nil {
				return nil, err
			}
			taken, err := db.Val[int64](c, tx, `SELECT id FROM appointments WHERE status='scheduled' AND id<>$3 AND start_at<$1 AND end_at>$2 LIMIT 1`, end, start, cur.ID)
			if err != nil {
				return nil, err
			}
			if taken > 0 {
				return nil, app.Bad("Another appointment is booked at that time.")
			}
			set["start_at"], set["end_at"], set["reminded"] = start, end, false
			moved = true
		}
	}
	if in.Title != nil {
		if t := clip(*in.Title, 250); t != "" {
			set["title"] = t
		}
	}
	if in.Notes != nil {
		set["notes"] = clip(*in.Notes, 5000)
	}
	if in.Location != nil {
		set["location"] = clip(*in.Location, 500)
	}
	if in.UserID != nil && c.Role() != "sales" {
		uid, err := owner(c, in.UserID)
		if err != nil {
			return nil, err
		}
		set["user_id"] = uid
	}
	if err := db.Update(c, tx, "appointments", cur.ID, set); err != nil {
		return nil, err
	}
	if moved && cur.LeadID > 0 {
		if err := app.Event(c, tx, cur.LeadID, "note", "Appointment moved to "+set["start_at"].(time.Time).In(loc).Format("Mon, 02 Jan 2006 · 03:04 PM"), "cal", map[string]any{"appointment_id": cur.ID}); err != nil {
			return nil, err
		}
	}
	gid := ""
	if in.Status != "" && in.Status != cur.Status {
		if gid, err = changeStatus(c, tx, cur.ID, in.Status); err != nil {
			return nil, err
		}
	}
	org, id, a := c.Org.ID, cur.ID, c.A
	sync := cur.InGCal && (moved || in.Title != nil || in.Location != nil || in.Notes != nil) && gid == ""
	c.After(func() {
		ctx := context.Background()
		if !sync && gid == "" {
			return
		}
		g := orgGCal(ctx, a, org)
		if g == nil {
			return
		}
		if gid != "" {
			g.cancel(ctx, gid)
		} else {
			g.update(ctx, id)
		}
	})
	return loadAppt(c, cur.ID)
}

// POST /api/appointments/{id}/status {status} (also /cancel and /complete).
func apptStatus(fixed string) app.Handler {
	return func(c *app.Ctx) (any, error) {
		st := fixed
		if st == "" {
			var in struct {
				Status string `json:"status"`
			}
			if err := c.Bind(&in); err != nil {
				return nil, err
			}
			st = in.Status
		}
		cur, err := loadAppt(c, c.ParamID("id"))
		if err != nil {
			return nil, err
		}
		gid, err := changeStatus(c, c.Tx(), cur.ID, st)
		if err != nil {
			return nil, err
		}
		if gid != "" {
			org, a := c.Org.ID, c.A
			c.After(func() {
				ctx := context.Background()
				if g := orgGCal(ctx, a, org); g != nil {
					g.cancel(ctx, gid)
				}
			})
		}
		msg := map[string]string{"completed": "Appointment marked as held.", "no_show": "Marked as no-show.", "cancelled": "Appointment cancelled.", "scheduled": "Appointment scheduled again."}[st]
		return map[string]any{"ok": true, "message": msg}, nil
	}
}
