package booking

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

type jobsJob = jobs.Job

const jobNotify = "booking_notify"

var ErrTaken = errors.New("That time was just taken – please pick another slot.")

// ---------------------------------------------------------------- settings helpers

func slotLen(s *settings.Store) int { return max(10, s.Int("appt_slot", 30)) }

func apptTitle(s *settings.Store, def string) string { return s.Get("appt_title", def) }

// hm parses "HH:MM".
func hm(v, def string) (int, int) {
	p := strings.SplitN(strings.TrimSpace(v), ":", 2)
	if len(p) != 2 {
		p = strings.SplitN(def, ":", 2)
	}
	h, _ := strconv.Atoi(strings.TrimSpace(p[0]))
	m, _ := strconv.Atoi(strings.TrimSpace(p[1]))
	return min(max(h, 0), 24), min(max(m, 0), 59)
}

// workDays: v1 stores ISO days (1=Mon … 7=Sun); the v2 settings form uses 0 for Sunday – both work.
func workDays(s *settings.Store) map[time.Weekday]bool {
	out := map[time.Weekday]bool{}
	for _, x := range strings.Split(s.Get("appt_days", "1,2,3,4,5,6"), ",") {
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil || n < 0 || n > 7 {
			continue
		}
		out[time.Weekday(n%7)] = true
	}
	return out
}

// ---------------------------------------------------------------- slots

type span struct{ A, B time.Time }

type Day struct {
	Date  string      `json:"date"` // YYYY-MM-DD in the workspace time zone
	Times []time.Time `json:"times"`
}

// computeSlots: free slots from today for `days` days within working days/hours, at least one hour
// from now, not overlapping busy periods (v1 Booking::slots).
func computeSlots(now time.Time, loc *time.Location, s *settings.Store, busy []span, days int) []Day {
	wd := workDays(s)
	ln := time.Duration(slotLen(s)) * time.Minute
	sh, sm := hm(s.Get("appt_start", "10:00"), "10:00")
	eh, em := hm(s.Get("appt_end", "18:00"), "18:00")
	n := now.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	earliest := now.Add(time.Hour)
	var out []Day
	for d := 0; d <= days; d++ {
		day := today.AddDate(0, 0, d)
		if !wd[day.Weekday()] {
			continue
		}
		end := time.Date(day.Year(), day.Month(), day.Day(), eh, em, 0, 0, loc)
		var ts []time.Time
		for t := time.Date(day.Year(), day.Month(), day.Day(), sh, sm, 0, 0, loc); !t.Add(ln).After(end); t = t.Add(ln) {
			if t.Before(earliest) {
				continue
			}
			free := true
			for _, b := range busy {
				if t.Before(b.B) && t.Add(ln).After(b.A) {
					free = false
					break
				}
			}
			if free {
				ts = append(ts, t)
			}
		}
		if len(ts) > 0 {
			out = append(out, Day{Date: day.Format("2006-01-02"), Times: ts})
		}
	}
	return out
}

func todayIn(loc *time.Location) time.Time {
	n := time.Now().In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
}

// dbBusy: booked appointments (scheduled / completed) in the window.
func dbBusy(ctx context.Context, q db.Q, from, to time.Time) ([]span, error) {
	rows, err := q.Query(ctx, `SELECT start_at, end_at FROM appointments WHERE status IN ('scheduled','completed') AND start_at < $2 AND end_at > $1`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []span
	for rows.Next() {
		var b span
		if err := rows.Scan(&b.A, &b.B); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// loadSlots computes free slots for a workspace without holding a transaction open during the
// Google Calendar free/busy request. Returns the settings it used.
func loadSlots(ctx context.Context, a *app.App, orgID int64, days int) ([]Day, *settings.Store, error) {
	var s *settings.Store
	var busy []span
	var loc *time.Location
	var from, to time.Time
	err := a.OrgTx(ctx, orgID, func(tx pgx.Tx, st *settings.Store) error {
		s = st
		loc = app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))
		from = todayIn(loc)
		to = from.AddDate(0, 0, days+1)
		var err error
		busy, err = dbBusy(ctx, tx, from, to)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	busy = append(busy, newGCal(a, orgID, s).busy(ctx, from, to)...)
	return computeSlots(time.Now(), loc, s, busy, days), s, nil
}

func slotOK(days []Day, t time.Time) bool {
	for _, d := range days {
		for _, x := range d.Times {
			if x.Equal(t) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------- booking

type bookIn struct {
	LeadID   int64
	Start    time.Time
	Minutes  int // 0 = slot length
	By       string
	Notes    string
	Title    string
	Location string
	UserID   *int64
}

// book stores an appointment inside the caller's tenant transaction (v1 Booking::book): checks the
// time is free, writes the journey, moves the lead to "Meeting booked" and emits appointment_booked.
// Google Calendar and the confirmation emails run after commit (afterBook).
func book(ctx context.Context, a *app.App, tx pgx.Tx, s *settings.Store, orgID int64, in bookIn) (int64, error) {
	ln := in.Minutes
	if ln <= 0 {
		ln = slotLen(s)
	}
	end := in.Start.Add(time.Duration(ln) * time.Minute)
	// One booking at a time per workspace, so two people cannot take the same slot
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7301, ($1::bigint % 2147483647)::int)`, orgID); err != nil {
		return 0, err
	}
	taken, err := db.Val[int64](ctx, tx, `SELECT id FROM appointments WHERE status='scheduled' AND start_at<$1 AND end_at>$2 LIMIT 1`, end, in.Start)
	if err != nil {
		return 0, err
	}
	if taken > 0 {
		return 0, app.Bad(ErrTaken.Error())
	}
	l, err := shared.GetLead(ctx, tx, in.LeadID)
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return 0, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = apptTitle(s, "Meeting")
		if l != nil {
			title += " – " + l.Name
		}
	}
	loc := in.Location
	if loc == "" {
		loc = s.Get("appt_location", "")
	}
	by := in.By
	if by == "" {
		by = "admin"
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO appointments (lead_id,user_id,title,start_at,end_at,location,notes,booked_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		in.LeadID, in.UserID, clip(title, 250), in.Start, end, clip(loc, 500), clip(in.Notes, 5000), by).Scan(&id)
	if err != nil {
		return 0, err
	}
	if l == nil {
		return id, nil
	}
	tz := app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))
	msg := "Appointment booked for " + in.Start.In(tz).Format("Mon, 02 Jan 2006 · 03:04 PM")
	if by == "lead" {
		msg += " (by the lead)"
	}
	if err := app.Event(ctx, tx, l.ID, "booked", msg, "cal", map[string]any{"appointment_id": id}); err != nil {
		return 0, err
	}
	if l.Status == "new" || l.Status == "contacted" || l.Status == "qualified" {
		if err := setStatus(ctx, a, tx, orgID, l.ID, "meeting"); err != nil {
			return 0, err
		}
	}
	a.Emit(ctx, tx, orgID, "appointment_booked", l.ID, map[string]any{"appointment_id": id})
	return id, nil
}

// setStatus moves the lead through crm.SetStatus; if that fails the status is set directly so the
// booking is never lost.
func setStatus(ctx context.Context, a *app.App, tx pgx.Tx, orgID, leadID int64, st string) error {
	if _, err := tx.Exec(ctx, "SAVEPOINT bk_status"); err != nil {
		return err
	}
	if err := crm.SetStatus(ctx, a, tx, orgID, leadID, st, ""); err == nil {
		_, err = tx.Exec(ctx, "RELEASE SAVEPOINT bk_status")
		return err
	} else {
		slog.Warn("booking: crm.SetStatus", "org", orgID, "lead", leadID, "err", err)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT bk_status"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE leads SET status=$2 WHERE id=$1`, leadID, st); err != nil {
		return err
	}
	return app.Event(ctx, tx, leadID, "status", "Status changed to "+shared.StatusLabel(st), "crm", nil)
}

// createLead adds the person who booked as a lead through crm.Create (create or merge duplicate).
// When the CRM is unavailable the lead is inserted directly so the booking still goes through.
func createLead(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, in crm.CreateIn) (int64, error) {
	if _, err := tx.Exec(ctx, "SAVEPOINT bk_lead"); err != nil {
		return 0, err
	}
	id, err := crm.Create(ctx, a, tx, orgID, in)
	if err == nil && id > 0 {
		_, err = tx.Exec(ctx, "RELEASE SAVEPOINT bk_lead")
		return id, err
	}
	slog.Warn("booking: crm.Create", "org", orgID, "err", err)
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT bk_lead"); err != nil {
		return 0, err
	}
	f := in.Fields
	name := strings.TrimSpace(f["name"])
	if name == "" {
		name = strings.TrimSpace(f["contact_name"])
	}
	if name == "" {
		name = "Unnamed lead"
	}
	var uid *int64
	if in.UserID > 0 {
		uid = &in.UserID
	}
	ps := ""
	if f["phone"] != "" {
		ps = "form"
	}
	err = tx.QueryRow(ctx, `INSERT INTO leads (campaign_id,place_id,name,contact_name,phone,email,message,is_lead,source,source_detail,status,stage,lead_at,last_activity,phone_source,assigned_to)
VALUES (0,$1,$2,$3,$4,$5,$6,true,$7,$8,'new','new',now(),now(),$9,$10) RETURNING id`,
		"crm-"+secure.Token(8), clip(name, 250), clip(f["contact_name"], 190), clip(f["phone"], 60), clip(strings.ToLower(f["email"]), 190), f["message"],
		in.Source, clip(in.Detail, 180), ps, uid).Scan(&id)
	if err != nil {
		return 0, err
	}
	msg := "New lead from " + shared.SourceLabel(in.Source)
	if in.Detail != "" {
		msg += " · " + in.Detail
	}
	if err := app.Event(ctx, tx, id, "found", msg, in.Source, nil); err != nil {
		return 0, err
	}
	a.Emit(ctx, tx, orgID, "lead_created", id, map[string]any{"source": in.Source})
	return id, nil
}

// afterBook runs after the booking committed: Google Calendar event (with Meet link) first, so the
// confirmation email can include the link, then the notification/confirmation emails as a job.
func afterBook(ctx context.Context, a *app.App, orgID, apptID int64) {
	ctx = context.WithoutCancel(ctx)
	if g := orgGCal(ctx, a, orgID); g != nil {
		g.push(ctx, apptID)
	}
	if err := a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		_, err := jobs.Push(ctx, tx, orgID, jobNotify, apptID, map[string]any{"appt_id": apptID}, jobs.Opt{MaxAttempts: 1})
		return err
	}); err != nil {
		slog.Error("booking: notify job", "org", orgID, "appt", apptID, "err", err)
	}
}

// orgGCal loads the workspace settings in a short transaction (nil on error).
func orgGCal(ctx context.Context, a *app.App, orgID int64) *gcal {
	var s *settings.Store
	if err := a.OrgTx(ctx, orgID, func(_ pgx.Tx, st *settings.Store) error { s = st; return nil }); err != nil {
		slog.Error("booking: settings", "org", orgID, "err", err)
		return nil
	}
	return newGCal(a, orgID, s)
}

// ---------------------------------------------------------------- status

var statuses = map[string]bool{"scheduled": true, "completed": true, "no_show": true, "cancelled": true}

// changeStatus updates an appointment (v1 Booking::status). Returns the Google event id to remove
// when it was cancelled.
func changeStatus(ctx context.Context, tx pgx.Tx, id int64, st string) (string, error) {
	if !statuses[st] {
		return "", app.Bad("Unknown status.")
	}
	var lead int64
	var gid string
	if err := tx.QueryRow(ctx, `UPDATE appointments SET status=$2 WHERE id=$1 RETURNING lead_id, gcal_id`, id, st).Scan(&lead, &gid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", app.NotFound()
		}
		return "", err
	}
	if lead > 0 {
		if err := app.Event(ctx, tx, lead, "note", "Appointment marked "+strings.ReplaceAll(st, "_", " "), "cal", map[string]any{"appointment_id": id}); err != nil {
			return "", err
		}
	}
	if st == "cancelled" {
		return gid, nil
	}
	return "", nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func fmtWhen(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Monday, 02 Jan 2006 at 03:04 PM")
}
