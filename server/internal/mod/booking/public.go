package booking

import (
	"bytes"
	_ "embed"
	"errors"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/settings"
)

//go:embed book.html
var bookHTML string

var bookTpl = template.Must(template.New("book").Parse(bookHTML))

const bookDays = 21

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{3,6}$`)

// lum is the relative luminance of a hex colour (readable text on brand colours).
func lum(h string) float64 {
	h = strings.TrimPrefix(h, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	h = (h + "000000")[:6]
	f := func(c float64) float64 {
		if c <= .03928 {
			return c / 12.92
		}
		return math.Pow((c+.055)/1.055, 2.4)
	}
	ch := func(i int) float64 { n, _ := strconv.ParseUint(h[i:i+2], 16, 8); return f(float64(n) / 255) }
	return .2126*ch(0) + .7152*ch(2) + .0722*ch(4)
}

type tSlot struct {
	T     int64
	Label string
	Long  string
}

type tGroup struct {
	Name  string
	Slots []tSlot
}

type tDay struct {
	Date, Dow, Num, Mon string
	First               bool
	Groups              []tGroup
}

type tDone struct {
	When, MeetLink, GCalURL string
}

type pageData struct {
	Title, Company, CompanyPhone, Len, LocLabel, ForName, TZ string
	Vars                                                     template.CSS
	Logo                                                     string
	LogoStyle                                                template.CSS
	Meet, WA, Lead                                           bool
	Err                                                      string
	Days                                                     []tDay
	NDays                                                    int
	Done                                                     *tDone
	Form                                                     map[string]string
}

func part(t time.Time) string {
	switch h := t.Hour(); {
	case h < 12:
		return "Morning"
	case h < 17:
		return "Afternoon"
	}
	return "Evening"
}

// logoPick: the logo that reads on the brand colour; the other one on a contrasting chip (v1 Report::logoPick).
func logoPick(s *settings.Store, bg string) (string, string) {
	dark := lum(bg) <= .45
	d, l := s.Get("company_logo", ""), s.Get("company_logo_light", "")
	ok := func(u string) bool { return strings.HasPrefix(u, "/files/") || strings.HasPrefix(u, "https://") }
	switch {
	case dark && ok(d):
		return d, ""
	case !dark && ok(l):
		return l, ""
	case dark && ok(l):
		return l, "#FFFFFF"
	case !dark && ok(d):
		return d, "#0B0B0B"
	}
	return "", ""
}

// bookPage: GET shows free slots, POST books one. /book/{leadToken} (personal) or /book/g/{key} (general).
func bookPage(c *app.Ctx) (any, error) {
	c.Raw()
	a := c.A
	notFound := func() (any, error) {
		c.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
		c.W.WriteHeader(http.StatusNotFound)
		c.W.Write([]byte("This booking link is not valid."))
		return nil, nil
	}
	if !a.Throttle(c, "bv:"+c.IP(), 120, time.Minute) {
		http.Error(c.W, "Too many requests – please try again in a minute.", http.StatusTooManyRequests)
		return nil, nil
	}
	var leadID int64
	if k := c.Param("key"); k != "" {
		if _, err := c.UseToken("booking", k); err != nil {
			return notFound()
		}
	} else {
		id, err := c.UseToken("lead", c.Param("token"))
		if err != nil || id == 0 {
			return notFound()
		}
		leadID = id
	}
	org := c.Org.ID
	var lead *shared.Lead
	if leadID > 0 {
		if err := a.DB.Org(c, org, func(tx pgx.Tx) (err error) { lead, err = shared.GetLead(c, tx, leadID); return }); err != nil {
			if errors.Is(err, db.ErrNoRows) {
				return notFound()
			}
			return nil, err
		}
	}
	days, s, err := loadSlots(c, a, org, bookDays)
	if err != nil {
		return nil, err
	}
	// The general link must still match the workspace key (a changed key retires the old link)
	if leadID == 0 && s.Get("booking_key", "") != c.Param("key") {
		return notFound()
	}
	loc := app.LoadTZ(s.Get("timezone", "Asia/Kolkata"))
	form := map[string]string{}
	var errMsg string
	var done *tDone
	if c.R.Method == http.MethodPost {
		c.R.ParseForm()
		for _, k := range []string{"name", "phone", "email", "company", "notes"} {
			form[k] = strings.TrimSpace(c.R.PostForm.Get(k))
		}
		done, errMsg = bookSubmit(c, s, lead, days, form, loc)
		if done != nil {
			days = nil
		}
	}
	P, A := s.Get("brand_primary", "#0B0B0B"), s.Get("brand_accent", "#B8860B")
	if !hexColor.MatchString(P) {
		P = "#0B0B0B"
	}
	if !hexColor.MatchString(A) {
		A = "#4F46E5"
	}
	pick := func(c string, dark, light string) string {
		if lum(c) > .45 {
			return dark
		}
		return light
	}
	vars := "--p:" + P + ";--onp:" + pick(P, "#111827", "#FFFFFF") + ";--a:" + A + ";--ona:" + pick(A, "#111827", "#FFFFFF") + ";--ta:" + pick(A, "#111827", A)
	co := s.Get("company_name", c.Org.Name)
	d := pageData{Title: s.Get("appt_title", "Book a meeting"), Company: co, CompanyPhone: s.Get("company_phone", ""), Len: strconv.Itoa(slotLen(s)),
		Vars: template.CSS(vars), TZ: loc.String(), WA: s.Get("wa_provider", "manual") != "manual", Lead: lead != nil, Err: errMsg, Done: done, NDays: bookDays, Form: form}
	if lf, chip := logoPick(s, P); lf != "" {
		d.Logo = lf
		if chip != "" {
			d.LogoStyle = template.CSS("background:" + chip + ";padding:8px 10px;border-radius:10px")
		}
	}
	if l := s.Get("appt_location", ""); l != "" {
		if strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
			if strings.Contains(strings.ToLower(l), "meet.google") {
				d.LocLabel = "Google Meet (link in confirmation)"
			} else {
				d.LocLabel = "Online meeting (link in confirmation)"
			}
		} else {
			d.LocLabel = l
		}
	} else if gcalReady(s) && s.Get("gcal_meet", "1") == "1" {
		d.Meet = true
	}
	if lead != nil {
		d.ForName = lead.ContactName
		if d.ForName == "" {
			d.ForName = lead.Name
		}
	}
	for i, x := range days {
		dt, _ := time.ParseInLocation("2006-01-02", x.Date, loc)
		td := tDay{Date: x.Date, Dow: dt.Format("Mon"), Num: dt.Format("02"), Mon: dt.Format("Jan"), First: i == 0}
		for _, t := range x.Times {
			lt := t.In(loc)
			g := part(lt)
			if len(td.Groups) == 0 || td.Groups[len(td.Groups)-1].Name != g {
				td.Groups = append(td.Groups, tGroup{Name: g})
			}
			gr := &td.Groups[len(td.Groups)-1]
			gr.Slots = append(gr.Slots, tSlot{T: t.Unix(), Label: lt.Format("03:04 PM"), Long: lt.Format("Mon, 02 Jan · 03:04 PM")})
		}
		d.Days = append(d.Days, td)
	}
	var buf bytes.Buffer
	if err := bookTpl.Execute(&buf, d); err != nil {
		return nil, err
	}
	h := c.W.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex")
	c.W.Write(buf.Bytes())
	return nil, nil
}

// bookSubmit validates the form and books the slot. Returns the confirmation or a message for the visitor.
func bookSubmit(c *app.Ctx, s *settings.Store, lead *shared.Lead, days []Day, f map[string]string, loc *time.Location) (*tDone, string) {
	a, org := c.A, c.Org.ID
	if !a.Throttle(c, "b:"+c.IP(), 12, time.Minute) {
		return nil, "Too many booking attempts – please try again later."
	}
	n, _ := strconv.ParseInt(c.R.PostForm.Get("slot"), 10, 64)
	start := time.Unix(n, 0)
	if n == 0 || !slotOK(days, start) {
		return nil, "Please choose an available time."
	}
	if lead == nil && (f["name"] == "" || f["phone"] == "") {
		return nil, "Please enter your name and phone number."
	}
	var id int64
	err := a.OrgTx(c, org, func(tx pgx.Tx, st *settings.Store) error {
		lid := int64(0)
		if lead != nil {
			lid = lead.ID
		} else {
			biz := clip(f["company"], 250)
			if biz == "" {
				biz = clip(f["name"], 250)
			}
			var err error
			lid, err = createLead(c, a, tx, org, crm.CreateIn{Fields: map[string]string{"contact_name": clip(f["name"], 190), "name": biz, "phone": clip(f["phone"], 60),
				"email": clip(f["email"], 190), "message": clip(f["notes"], 5000)}, Source: "booking", Detail: "Booking page"})
			if err != nil {
				return err
			}
		}
		var err error
		id, err = book(c, a, tx, st, org, bookIn{LeadID: lid, Start: start, By: "lead", Notes: clip(f["notes"], 5000)})
		return err
	})
	if err != nil {
		var he *app.HTTPError
		if errors.As(err, &he) {
			return nil, he.Msg
		}
		slog.Error("booking page", "org", org, "err", err)
		return nil, "Something went wrong. Please try again."
	}
	afterBook(c, a, org, id)
	var x apptLead
	if err := a.DB.Org(c, org, func(tx pgx.Tx) (err error) { x, err = db.One[apptLead](c, tx, apptLeadSQL, id); return }); err != nil {
		return nil, "Something went wrong. Please try again."
	}
	q := url.Values{"action": {"TEMPLATE"}, "text": {x.Title}, "dates": {x.StartAt.UTC().Format("20060102T150405Z") + "/" + x.EndAt.UTC().Format("20060102T150405Z")},
		"details": {"Booked with " + s.Get("company_name", "")}, "location": {x.Location}}
	return &tDone{When: x.StartAt.In(loc).Format("Monday, 02 January 2006 · 03:04 PM"), MeetLink: x.MeetLink, GCalURL: "https://calendar.google.com/calendar/render?" + q.Encode()}, ""
}
