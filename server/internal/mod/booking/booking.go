// Package booking: appointments, free slots (working days/hours, slot length, busy times), the public
// booking page (general and personal links), confirmations, 1-hour reminders and the optional Google
// Calendar sync (OAuth per workspace, free/busy blocking, events with Meet link + invites).
// Port of v1 Booking.php, GCal.php, gcal.php, book.php and pages/appointments.php.
package booking

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Defaults of a new workspace (v1 Seed.php).
var defaults = map[string]any{
	"appt_days": "1,2,3,4,5,6", "appt_start": "10:00", "appt_end": "18:00", "appt_slot": "30",
	"appt_title": "Free strategy call", "appt_location": "Google Meet (link will be shared)",
	"appt_notify": "1", "appt_remind": "1", "gcal_calendar": "primary", "gcal_meet": "1", "gcal_invite": "1",
}

func Register(a *app.App) {
	// Appointments (calendar / list) – sales people see their own
	a.Get("/appointments", apptList, app.Perm("appointments"))
	a.Get("/appointments/summary", apptSummary, app.Perm("appointments"))
	a.Get("/appointments/slots", apptSlots, app.Perm("appointments"))
	a.Get("/appointments/leads", apptLeads, app.Perm("appointments"))
	a.Post("/appointments", apptCreate, app.Perm("appointments"))
	a.Get("/appointments/{id}", apptGet, app.Perm("appointments"))
	a.Put("/appointments/{id}", apptUpdate, app.Perm("appointments"))
	a.Post("/appointments/{id}/status", apptStatus(""), app.Perm("appointments"))
	a.Post("/appointments/{id}/cancel", apptStatus("cancelled"), app.Perm("appointments"))
	a.Post("/appointments/{id}/complete", apptStatus("completed"), app.Perm("appointments"))
	a.Get("/leads/{id}/appointments", leadAppointments, app.Perm("leads"))

	// Google Calendar
	a.Get("/gcal/status", gcalStatus, app.Perm("settings"))
	a.Get("/gcal/connect", gcalConnect, app.Perm("settings"))
	a.Post("/gcal/disconnect", gcalDisconnect, app.Perm("settings"))
	a.Post("/gcal/flush", gcalFlush, app.Perm("settings"))
	a.Public(http.MethodGet, "/webhooks/gcal/callback", gcalCallback)

	// Public booking page: personal (lead token) and general (workspace booking key) links
	a.Public(http.MethodGet, "/book/{token}", bookPage)
	a.Public(http.MethodPost, "/book/{token}", bookPage)
	a.Public(http.MethodGet, "/book/g/{key}", bookPage)
	a.Public(http.MethodPost, "/book/g/{key}", bookPage)

	a.Jobs.Handle(jobNotify, 2*time.Minute, func(ctx context.Context, j *jobsJob) error { return notifyJob(ctx, a, j) })
	a.Jobs.Every("booking_reminders", time.Minute, func(ctx context.Context) error { return remindAll(ctx, a) })

	a.Seed(func(ctx context.Context, tx pgx.Tx, orgID int64, s *settings.Store) error {
		for k, v := range defaults {
			if s.Get(k, "") == "" {
				if err := s.Set(ctx, tx, k, v); err != nil {
					return err
				}
			}
		}
		_, err := ensureKey(ctx, tx, orgID, s)
		return err
	})
}

// ensureKey returns the workspace booking key (general link), creating and registering it when missing.
func ensureKey(ctx context.Context, tx pgx.Tx, orgID int64, s *settings.Store) (string, error) {
	k := s.Get("booking_key", "")
	if k == "" {
		k = secure.Token(8)
		if err := s.Set(ctx, tx, "booking_key", k); err != nil {
			return "", err
		}
	}
	return k, app.RegisterToken(ctx, tx, orgID, "booking", 0, k)
}
