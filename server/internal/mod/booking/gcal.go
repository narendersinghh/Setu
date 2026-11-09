package booking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/netx"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Google Calendar sync (v1 GCal.php). Tokens are stored as secret settings: gcal_refresh_token and
// gcal_access_token ({"t":access token,"exp":unix}). Each workspace uses its own OAuth client.
// Network calls never run inside a tenant transaction – settings and usage are saved in short ones.

const (
	googleAuth  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleToken = "https://oauth2.googleapis.com/token"
	calAPI      = "https://www.googleapis.com/calendar/v3/"
)

type gcal struct {
	a   *app.App
	org int64
	s   *settings.Store
}

func newGCal(a *app.App, org int64, s *settings.Store) *gcal { return &gcal{a: a, org: org, s: s} }

func gcalReady(s *settings.Store) bool {
	return s.Get("gcal_client_id", "") != "" && s.Get("gcal_refresh_token", "") != ""
}

func (g *gcal) ready() bool { return gcalReady(g.s) }

func redirectURI(a *app.App) string { return a.Cfg.AppURL() + "/webhooks/gcal/callback" }

func (g *gcal) calendar() string { return g.s.Get("gcal_calendar", "primary") }

// save stores settings in a short transaction of the workspace.
func (g *gcal) save(ctx context.Context, kv map[string]any) error {
	return g.a.DB.Org(context.WithoutCancel(ctx), g.org, func(tx pgx.Tx) error { return g.s.Many(ctx, tx, kv) })
}

func (g *gcal) usage(ctx context.Context, op string, failed bool) {
	g.a.DB.Org(context.WithoutCancel(ctx), g.org, func(tx pgx.Tx) error {
		app.TrackUsage(ctx, tx, "gcal", op, 1, 0, 0, failed)
		return nil
	})
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (g *gcal) tokenCall(ctx context.Context, form url.Values) (*tokenResp, string, error) {
	r, err := netx.Do(ctx, netx.Req{Method: http.MethodPost, URL: googleToken, Body: strings.NewReader(form.Encode()),
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}, Timeout: 20 * time.Second})
	if err != nil {
		return nil, "", err
	}
	var t tokenResp
	_ = r.JSON(&t)
	return &t, string(r.Body), nil
}

func accessJSON(t *tokenResp) string {
	b, _ := json.Marshal(map[string]any{"t": t.AccessToken, "exp": time.Now().Unix() + t.ExpiresIn - 60})
	return string(b)
}

// exchange trades the OAuth code for tokens and remembers the connected account.
func (g *gcal) exchange(ctx context.Context, code string) error {
	t, body, err := g.tokenCall(ctx, url.Values{"code": {code}, "client_id": {g.s.Get("gcal_client_id", "")}, "client_secret": {g.s.Get("gcal_client_secret", "")},
		"redirect_uri": {redirectURI(g.a)}, "grant_type": {"authorization_code"}})
	if err != nil {
		return err
	}
	if t.RefreshToken == "" {
		d := t.ErrorDesc
		if d == "" {
			d = clip(body, 300)
		}
		return errors.New("Google did not return a refresh token: " + d)
	}
	if err := g.save(ctx, map[string]any{"gcal_refresh_token": t.RefreshToken, "gcal_access_token": accessJSON(t), "gcal_busy": ""}); err != nil {
		return err
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := g.api(ctx, http.MethodGet, "users/me/calendarList/primary", nil, &me); err != nil {
		slog.Warn("gcal account", "org", g.org, "err", err)
	}
	return g.save(ctx, map[string]any{"gcal_account": me.ID})
}

// token returns a valid access token, refreshing it when expired.
func (g *gcal) token(ctx context.Context) (string, error) {
	var c struct {
		T   string `json:"t"`
		Exp int64  `json:"exp"`
	}
	if g.s.JSON("gcal_access_token", &c) && c.T != "" && c.Exp > time.Now().Unix() {
		return c.T, nil
	}
	t, _, err := g.tokenCall(ctx, url.Values{"client_id": {g.s.Get("gcal_client_id", "")}, "client_secret": {g.s.Get("gcal_client_secret", "")},
		"refresh_token": {g.s.Get("gcal_refresh_token", "")}, "grant_type": {"refresh_token"}})
	if err != nil || t.AccessToken == "" {
		return "", errors.New("Google Calendar token refresh failed – reconnect in Settings → Appointments.")
	}
	if err := g.save(ctx, map[string]any{"gcal_access_token": accessJSON(t)}); err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

// api calls the Calendar API and decodes the JSON answer into out (when not nil).
func (g *gcal) api(ctx context.Context, method, path string, body any, out any) error {
	tok, err := g.token(ctx)
	if err != nil {
		return err
	}
	r, err := netx.Do(ctx, netx.Req{Method: method, URL: calAPI + path, JSON: body, Headers: map[string]string{"Authorization": "Bearer " + tok, "Accept": "application/json"}, Timeout: 20 * time.Second})
	failed := err != nil || r.Code >= 300
	g.usage(ctx, strings.ToLower(method), failed)
	if err != nil {
		return fmt.Errorf("Google Calendar: %w", err)
	}
	if r.Code >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = r.JSON(&e)
		if e.Error.Message == "" {
			e.Error.Message = "HTTP " + strconv.Itoa(r.Code)
		}
		return errors.New("Google Calendar: " + e.Error.Message)
	}
	if out != nil && len(r.Body) > 0 {
		return r.JSON(out)
	}
	return nil
}

type busyCache struct {
	At   int64      `json:"at"`
	From int64      `json:"from"`
	To   int64      `json:"to"`
	Cal  string     `json:"cal"`
	B    [][2]int64 `json:"b"`
}

// busy returns busy periods of the calendar – cached 5 minutes to save API calls. Errors are logged
// and ignored (the booking page still works with Setu's own appointments).
func (g *gcal) busy(ctx context.Context, from, to time.Time) []span {
	if !g.ready() {
		return nil
	}
	conv := func(b [][2]int64) []span {
		out := make([]span, 0, len(b))
		for _, x := range b {
			out = append(out, span{time.Unix(x[0], 0), time.Unix(x[1], 0)})
		}
		return out
	}
	var c busyCache
	if g.s.JSON("gcal_busy", &c) && c.At > time.Now().Unix()-300 && c.From <= from.Unix() && c.To >= to.Unix() && c.Cal == g.calendar() {
		return conv(c.B)
	}
	var r struct {
		Calendars map[string]struct {
			Busy []struct {
				Start time.Time `json:"start"`
				End   time.Time `json:"end"`
			} `json:"busy"`
		} `json:"calendars"`
	}
	if err := g.api(ctx, http.MethodPost, "freeBusy", map[string]any{"timeMin": from.Format(time.RFC3339), "timeMax": to.Format(time.RFC3339), "items": []map[string]string{{"id": g.calendar()}}}, &r); err != nil {
		slog.Warn("gcal busy", "org", g.org, "err", err)
		return nil
	}
	c = busyCache{At: time.Now().Unix(), From: from.Unix(), To: to.Unix(), Cal: g.calendar(), B: [][2]int64{}}
	for _, x := range r.Calendars[g.calendar()].Busy {
		c.B = append(c.B, [2]int64{x.Start.Unix(), x.End.Unix()})
	}
	b, _ := json.Marshal(c)
	if err := g.save(ctx, map[string]any{"gcal_busy": string(b)}); err != nil {
		slog.Warn("gcal busy cache", "org", g.org, "err", err)
	}
	return conv(c.B)
}

type apptLead struct {
	ID          int64     `db:"id"`
	LeadID      int64     `db:"lead_id"`
	Title       string    `db:"title"`
	StartAt     time.Time `db:"start_at"`
	EndAt       time.Time `db:"end_at"`
	Status      string    `db:"status"`
	Location    string    `db:"location"`
	Notes       string    `db:"notes"`
	GcalID      string    `db:"gcal_id"`
	MeetLink    string    `db:"meet_link"`
	Name        string    `db:"name"`
	ContactName string    `db:"contact_name"`
	Email       string    `db:"email"`
	Phone       string    `db:"phone"`
}

const apptLeadSQL = `SELECT a.id,a.lead_id,a.title,a.start_at,a.end_at,a.status,a.location,a.notes,a.gcal_id,a.meet_link,
COALESCE(l.name,'') name,COALESCE(l.contact_name,'') contact_name,COALESCE(l.email,'') email,COALESCE(l.phone,'') phone
FROM appointments a LEFT JOIN leads l ON l.id=a.lead_id WHERE a.id=$1`

func (x *apptLead) who() string {
	if x.ContactName != "" {
		return x.ContactName
	}
	return x.Name
}

func (g *gcal) event(x *apptLead) map[string]any {
	desc := strings.TrimSpace(x.who() + "\nPhone: " + x.Phone + "\nEmail: " + x.Email + "\n\n" + x.Notes)
	if x.LeadID > 0 {
		desc += "\n\n" + g.a.Cfg.AppURL() + "/leads/" + strconv.FormatInt(x.LeadID, 10)
	}
	return map[string]any{"summary": x.Title, "description": desc, "location": x.Location,
		"start": map[string]string{"dateTime": x.StartAt.Format(time.RFC3339)}, "end": map[string]string{"dateTime": x.EndAt.Format(time.RFC3339)}}
}

// push creates the calendar event (with a Google Meet link when enabled) and stores its id.
func (g *gcal) push(ctx context.Context, apptID int64) {
	if !g.ready() {
		return
	}
	var x apptLead
	if err := g.a.DB.Org(ctx, g.org, func(tx pgx.Tx) (err error) { x, err = db.One[apptLead](ctx, tx, apptLeadSQL, apptID); return }); err != nil {
		slog.Warn("gcal push: load", "org", g.org, "appt", apptID, "err", err)
		return
	}
	ev := g.event(&x)
	invite := g.s.Get("gcal_invite", "1") == "1"
	if x.Email != "" && invite {
		ev["attendees"] = []map[string]string{{"email": x.Email, "displayName": x.who()}}
	}
	if g.s.Get("gcal_meet", "1") == "1" {
		ev["conferenceData"] = map[string]any{"createRequest": map[string]any{"requestId": secure.Token(8), "conferenceSolutionKey": map[string]string{"type": "hangoutsMeet"}}}
	}
	send := "none"
	if invite {
		send = "all"
	}
	var r struct {
		ID          string `json:"id"`
		HangoutLink string `json:"hangoutLink"`
	}
	err := g.api(ctx, http.MethodPost, "calendars/"+url.PathEscape(g.calendar())+"/events?conferenceDataVersion=1&sendUpdates="+send, ev, &r)
	g.a.DB.Org(context.WithoutCancel(ctx), g.org, func(tx pgx.Tx) error {
		if err != nil {
			slog.Warn("gcal push", "org", g.org, "appt", apptID, "err", err)
			if x.LeadID > 0 {
				app.Event(ctx, tx, x.LeadID, "error", "Google Calendar sync failed: "+err.Error(), "cal", nil)
			}
			return nil
		}
		if _, e := tx.Exec(ctx, `UPDATE appointments SET gcal_id=$2, meet_link=$3, location=CASE WHEN $3<>'' THEN $3 ELSE location END WHERE id=$1`, apptID, r.ID, r.HangoutLink); e != nil {
			return e
		}
		g.s.Set(ctx, tx, "gcal_busy", "")
		if x.LeadID > 0 {
			m := "Added to Google Calendar"
			if r.HangoutLink != "" {
				m += " with Meet link"
			}
			app.Event(ctx, tx, x.LeadID, "note", m, "cal", nil)
		}
		return nil
	})
}

// update moves / renames the calendar event after an edit.
func (g *gcal) update(ctx context.Context, apptID int64) {
	if !g.ready() {
		return
	}
	var x apptLead
	if err := g.a.DB.Org(ctx, g.org, func(tx pgx.Tx) (err error) { x, err = db.One[apptLead](ctx, tx, apptLeadSQL, apptID); return }); err != nil || x.GcalID == "" {
		return
	}
	ev := g.event(&x)
	if err := g.api(ctx, http.MethodPatch, "calendars/"+url.PathEscape(g.calendar())+"/events/"+url.PathEscape(x.GcalID)+"?sendUpdates=all", ev, nil); err != nil {
		slog.Warn("gcal update", "org", g.org, "appt", apptID, "err", err)
		return
	}
	g.save(ctx, map[string]any{"gcal_busy": ""})
}

// cancel removes the calendar event (guests are notified).
func (g *gcal) cancel(ctx context.Context, gcalID string) {
	if gcalID == "" || !g.ready() {
		return
	}
	if err := g.api(ctx, http.MethodDelete, "calendars/"+url.PathEscape(g.calendar())+"/events/"+url.PathEscape(gcalID)+"?sendUpdates=all", nil, nil); err != nil {
		slog.Warn("gcal cancel", "org", g.org, "err", err)
		return
	}
	g.save(ctx, map[string]any{"gcal_busy": ""})
}

// ---------------------------------------------------------------- OAuth endpoints

func gcalStatus(c *app.Ctx) (any, error) {
	s := c.S()
	return map[string]any{"ready": gcalReady(s), "account": s.Get("gcal_account", ""), "has_client": s.Get("gcal_client_id", "") != "" && s.Get("gcal_client_secret", "") != "",
		"redirect_uri": redirectURI(c.A)}, nil
}

// signedState carries the workspace (and user) through Google's redirect; it cannot be forged.
func signedState(a *app.App, org, user int64) string {
	p := fmt.Sprintf("%d.%d.%d", org, user, time.Now().Unix())
	return p + "." + a.Box.Sign("gcal:"+p)
}

func checkState(a *app.App, st string) (int64, error) {
	p := strings.Split(st, ".")
	if len(p) != 4 || !secure.Equal(p[3], a.Box.Sign("gcal:"+strings.Join(p[:3], "."))) {
		return 0, errors.New("Security check failed – please try connecting again.")
	}
	ts, _ := strconv.ParseInt(p[2], 10, 64)
	if time.Since(time.Unix(ts, 0)) > 30*time.Minute {
		return 0, errors.New("The connection request expired – please try connecting again.")
	}
	return strconv.ParseInt(p[0], 10, 64)
}

func gcalConnect(c *app.Ctx) (any, error) {
	s := c.S()
	if s.Get("gcal_client_id", "") == "" || s.Get("gcal_client_secret", "") == "" {
		return nil, app.Bad("Save your Google OAuth Client ID and Secret first.")
	}
	q := url.Values{"client_id": {s.Get("gcal_client_id", "")}, "redirect_uri": {redirectURI(c.A)}, "response_type": {"code"},
		"scope": {"https://www.googleapis.com/auth/calendar"}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {signedState(c.A, c.Org.ID, c.User.ID)}}
	return map[string]any{"url": googleAuth + "?" + q.Encode()}, nil
}

func gcalDisconnect(c *app.Ctx) (any, error) {
	for _, k := range []string{"gcal_refresh_token", "gcal_access_token", "gcal_account", "gcal_busy"} {
		if err := c.SetS(k, ""); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "message": "Google Calendar disconnected."}, nil
}

func gcalFlush(c *app.Ctx) (any, error) { return nil, c.SetS("gcal_busy", "") }

// gcalCallback: Google redirects here with ?code&state (or ?error). Back to Settings → Appointments.
func gcalCallback(c *app.Ctx) (any, error) {
	c.Raw()
	back := c.A.Cfg.AppURL() + "/settings/appointments"
	fail := func(msg string) (any, error) {
		http.Redirect(c.W, c.R, back+"?gcal_error="+url.QueryEscape(msg), http.StatusFound)
		return nil, nil
	}
	if e := c.Query("error"); e != "" {
		return fail("Google: " + e)
	}
	org, err := checkState(c.A, c.Query("state"))
	if err != nil {
		return fail(err.Error())
	}
	if !c.A.Throttle(c, "gcal:"+strconv.FormatInt(org, 10), 20, time.Hour) {
		return fail("Too many attempts – please try again later.")
	}
	if err := c.UseOrg(org); err != nil {
		return fail("Workspace not found.")
	}
	var s *settings.Store
	if err := c.A.OrgTx(c, org, func(tx pgx.Tx, st *settings.Store) error { s = st; return nil }); err != nil {
		return fail("Something went wrong. Please try again.")
	}
	g := newGCal(c.A, org, s)
	if err := g.exchange(c, c.Query("code")); err != nil {
		return fail(err.Error())
	}
	http.Redirect(c.W, c.R, back+"?gcal=connected", http.StatusFound)
	return nil, nil
}
