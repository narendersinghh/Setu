package booking

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/messaging"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// notifyJob: "email me when someone books" and the confirmation to the lead (v1 Booking::notify).
// Sending problems are logged, never retried (no duplicate emails).
func notifyJob(ctx context.Context, a *app.App, j *jobsJob) error {
	var p struct {
		ApptID int64 `json:"appt_id"`
	}
	if err := j.Decode(&p); err != nil || p.ApptID == 0 {
		return nil
	}
	var s *settings.Store
	var x apptLead
	err := a.OrgTx(ctx, j.OrgID, func(tx pgx.Tx, st *settings.Store) (err error) {
		s = st
		x, err = db.One[apptLead](ctx, tx, apptLeadSQL, p.ApptID)
		return err
	})
	if err != nil || x.LeadID == 0 || x.Status != "scheduled" {
		return nil
	}
	loc := app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))
	when := fmtWhen(x.StartAt, loc)
	short := x.StartAt.In(loc)
	if s.Get("appt_notify", "") == "1" {
		if to := s.Get("company_email", ""); to != "" {
			t := "New appointment booked\n\n" + x.Name
			if x.ContactName != "" {
				t += " (" + x.ContactName + ")"
			}
			t += "\nPhone: " + x.Phone + "\nEmail: " + x.Email + "\nWhen: " + when + "\n\n" + a.Cfg.AppURL() + "/leads/" + strconv.FormatInt(x.LeadID, 10)
			// Internal notification: not part of the lead's conversation (LeadID 0)
			if _, err := messaging.SendEmail(ctx, a, j.OrgID, s, messaging.Email{To: to, Subject: "📅 New booking: " + x.Name + " – " + short.Format("02 Jan 03:04 PM"), Body: t}); err != nil {
				slog.Warn("booking mail: notify", "org", j.OrgID, "appt", x.ID, "err", err)
			}
		}
	}
	if x.Email != "" {
		loc2 := x.Location
		if loc2 == "" {
			loc2 = s.Get("appt_location", "")
		}
		t := "Hi " + x.who() + ",\n\nYour " + s.Get("appt_title", "meeting") + " with " + s.Get("company_name", "") + " is confirmed for " + when + "."
		if loc2 != "" {
			t += "\nWhere: " + loc2
		}
		t += "\n\nSee you then!\n" + s.Get("sender_name", s.Get("company_name", ""))
		if _, err := messaging.SendEmail(ctx, a, j.OrgID, s, messaging.Email{LeadID: x.LeadID, To: x.Email, Subject: "Confirmed: " + s.Get("appt_title", "Meeting") + " on " + short.Format("02 Jan, 03:04 PM"), Body: t}); err != nil {
			slog.Warn("booking mail: confirmation", "org", j.OrgID, "appt", x.ID, "err", err)
		}
	}
	return nil
}

// remindAll (every minute): finds workspaces with appointments starting within ~1 hour through the
// global appt_due index, then sends reminders per workspace (v1 Booking::remind).
func remindAll(ctx context.Context, a *app.App) error {
	a.DB.Pool.Exec(ctx, `DELETE FROM appt_due WHERE start_at < now() - interval '1 day'`)
	rows, err := a.DB.Pool.Query(ctx, `SELECT DISTINCT d.org_id FROM appt_due d JOIN orgs o ON o.id=d.org_id AND o.status='active'
WHERE d.start_at BETWEEN now() AND now() + interval '70 minutes' LIMIT 5000`)
	if err != nil {
		return err
	}
	orgs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	for _, org := range orgs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := remindOrg(ctx, a, org); err != nil {
			slog.Warn("booking reminders", "org", org, "err", err)
		}
	}
	return nil
}

type due struct {
	ID          int64     `db:"id"`
	LeadID      int64     `db:"lead_id"`
	StartAt     time.Time `db:"start_at"`
	Location    string    `db:"location"`
	Name        string    `db:"name"`
	ContactName string    `db:"contact_name"`
	Phone       string    `db:"phone"`
	Email       string    `db:"email"`
}

func remindOrg(ctx context.Context, a *app.App, org int64) error {
	var s *settings.Store
	var list []due
	err := a.OrgTx(ctx, org, func(tx pgx.Tx, st *settings.Store) (err error) {
		s = st
		if s.Get("appt_remind", "") != "1" {
			return nil
		}
		// Marked first (as v1) so a reminder is never sent twice
		list, err = db.All[due](ctx, tx, `UPDATE appointments a SET reminded=true FROM leads l
WHERE l.id=a.lead_id AND a.status='scheduled' AND NOT a.reminded AND a.start_at BETWEEN now() AND now() + interval '70 minutes'
RETURNING a.id, a.lead_id, a.start_at, a.location, l.name, l.contact_name, l.phone, l.email`)
		return err
	})
	if err != nil || len(list) == 0 {
		return err
	}
	loc := app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))
	wa := s.Get("wa_provider", "manual") != "manual"
	for _, x := range list {
		t := "Reminder: your " + s.Get("appt_title", "meeting") + " with " + s.Get("company_name", "") + " starts at " + x.StartAt.In(loc).Format("03:04 PM") + "."
		if x.Location != "" {
			t += "\n" + x.Location
		}
		var err error
		sent := false
		switch {
		case x.Phone != "" && wa:
			_, _, err = messaging.SendWhatsApp(ctx, a, org, s, messaging.WhatsApp{LeadID: x.LeadID, Phone: x.Phone, Text: t})
			sent = err == nil
		case x.Email != "":
			_, err = messaging.SendEmail(ctx, a, org, s, messaging.Email{LeadID: x.LeadID, To: x.Email, Subject: "Reminder: " + s.Get("appt_title", "Meeting") + " today", Body: t})
			sent = err == nil
		}
		if err != nil {
			slog.Warn("booking reminder", "org", org, "appt", x.ID, "err", err)
		}
		if sent {
			a.DB.Org(ctx, org, func(tx pgx.Tx) error {
				return app.Event(ctx, tx, x.LeadID, "sent", "Appointment reminder sent", "cal", map[string]any{"appointment_id": x.ID})
			})
		}
	}
	return nil
}
