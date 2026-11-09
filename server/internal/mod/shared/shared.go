// Package shared: lead model and helpers used by several modules (template variables, phone numbers,
// statuses, progress & abort, process log, public links). No module-specific logic lives here.
package shared

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Lead mirrors the leads table (nullable columns are pointers).
type Lead struct {
	ID           int64          `db:"id" json:"id"`
	CampaignID   int64          `db:"campaign_id" json:"campaign_id"`
	Position     int            `db:"position" json:"position"`
	PlaceID      string         `db:"place_id" json:"place_id"`
	Name         string         `db:"name" json:"name"`
	Category     string         `db:"category" json:"category"`
	Address      string         `db:"address" json:"address"`
	City         string         `db:"city" json:"city"`
	Phone        string         `db:"phone" json:"phone"`
	PhoneSource  string         `db:"phone_source" json:"phone_source"`
	Email        string         `db:"email" json:"email"`
	Website      string         `db:"website" json:"website"`
	Rating       *float64       `db:"rating" json:"rating"`
	Reviews      *int           `db:"reviews" json:"reviews"`
	MapsURL      string         `db:"maps_url" json:"maps_url"`
	Lat          *float64       `db:"lat" json:"lat"`
	Lng          *float64       `db:"lng" json:"lng"`
	Socials      any            `db:"socials" json:"socials"`
	WebScore     *int16         `db:"web_score" json:"web_score"`
	MapScore     *int16         `db:"map_score" json:"map_score"`
	Stage        string         `db:"stage" json:"stage"`
	EmailStatus  string         `db:"email_status" json:"email_status"`
	WaStatus     string         `db:"wa_status" json:"wa_status"`
	RepliedAt    *time.Time     `db:"replied_at" json:"replied_at"`
	RepliedVia   string         `db:"replied_via" json:"replied_via"`
	Notes        string         `db:"notes" json:"notes"`
	LastActivity *time.Time     `db:"last_activity" json:"last_activity"`
	IsLead       bool           `db:"is_lead" json:"is_lead"`
	Source       string         `db:"source" json:"source"`
	SourceDetail string         `db:"source_detail" json:"source_detail"`
	ContactName  string         `db:"contact_name" json:"contact_name"`
	AltPhone     string         `db:"alt_phone" json:"alt_phone"`
	Service      string         `db:"service" json:"service"`
	DealValue    *float64       `db:"deal_value" json:"deal_value"`
	Status       string         `db:"status" json:"status"`
	Tags         string         `db:"tags" json:"tags"`
	FollowUpAt   *time.Time     `db:"follow_up_at" json:"follow_up_at"`
	AssignedTo   *int64         `db:"assigned_to" json:"assigned_to"`
	Custom       map[string]any `db:"custom" json:"custom"`
	Message      string         `db:"message" json:"message"`
	Progress     int16          `db:"progress" json:"progress"`
	ProgressMsg  string         `db:"progress_msg" json:"progress_msg"`
	AbortFlag    bool           `db:"abort_flag" json:"abort_flag"`
	LastError    string         `db:"last_error" json:"last_error"`
	LeadAt       *time.Time     `db:"lead_at" json:"lead_at"`
	Token        string         `db:"token" json:"token"`
	CreatedAt    time.Time      `db:"created_at" json:"created_at"`
	Keyword      string         `db:"keyword" json:"keyword"` // from campaigns (when joined)
}

// LeadCols is the column list for Lead (leads aliased as l), with the campaign keyword.
const LeadCols = `l.id,l.campaign_id,l.position,l.place_id,l.name,l.category,l.address,l.city,l.phone,l.phone_source,l.email,l.website,l.rating::float8 rating,l.reviews,l.maps_url,l.lat::float8 lat,l.lng::float8 lng,l.socials,l.web_score,l.map_score,l.stage,l.email_status,l.wa_status,l.replied_at,l.replied_via,l.notes,l.last_activity,l.is_lead,l.source,l.source_detail,l.contact_name,l.alt_phone,l.service,l.deal_value::float8 deal_value,l.status,l.tags,l.follow_up_at,l.assigned_to,l.custom,l.message,l.progress,l.progress_msg,l.abort_flag,l.last_error,l.lead_at,l.token,l.created_at,COALESCE(c.keyword,'') keyword`

// GetLead loads one lead (RLS-scoped by the transaction).
func GetLead(ctx context.Context, q db.Q, id int64) (*Lead, error) {
	l, err := db.One[Lead](ctx, q, `SELECT `+LeadCols+` FROM leads l LEFT JOIN campaigns c ON c.id=l.campaign_id WHERE l.id=$1`, id)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// ---------------------------------------------------------------- labels (same as v1)

var Stages = map[string][2]string{"new": {"New", "gray"}, "queued": {"Queued", "blue"}, "auditing": {"Auditing", "blue"}, "reporting": {"Writing report", "purple"}, "report_ready": {"Report ready", "gold"}, "contacting": {"Sending", "blue"}, "contacted": {"Contacted", "teal"}, "opened": {"Opened", "teal"}, "replied": {"Replied", "green"}, "won": {"Won", "green"}, "lost": {"Not interested", "red"}, "failed": {"Failed", "red"}, "skipped": {"Skipped", "gray"}}

var CRMStatuses = [][3]string{{"new", "New", "blue"}, {"contacted", "Contacted", "teal"}, {"qualified", "Qualified", "purple"}, {"meeting", "Meeting booked", "orange"}, {"proposal", "Proposal sent", "gold"}, {"won", "Won", "green"}, {"lost", "Lost", "red"}}

func StatusLabel(k string) string {
	for _, s := range CRMStatuses {
		if s[0] == k {
			return s[1]
		}
	}
	return k
}

var LeadSources = map[string][3]string{"maps": {"Map scraper", "orange", "map"}, "manual": {"Manual", "gray", "edit"}, "website": {"Website form", "blue", "globe"}, "facebook": {"Meta / Facebook ads", "purple", "target"}, "google": {"Google Ads", "green", "zap"}, "webhook": {"Webhook / API", "teal", "plug"}, "import": {"CSV import", "gray", "upload"}, "booking": {"Booking page", "orange", "cal"}}

func SourceLabel(k string) string {
	if s, ok := LeadSources[k]; ok {
		return s[0]
	}
	return k
}

// ---------------------------------------------------------------- templates & formatting

var tplRe = regexp.MustCompile(`(?i)\{\{\s*([a-z0-9_]+)\s*\}\}`)

// Tpl fills {{var}} placeholders; unknown placeholders stay as they are.
func Tpl(s string, vars map[string]string) string {
	return tplRe.ReplaceAllStringFunc(s, func(m string) string {
		k := tplRe.FindStringSubmatch(m)[1]
		if v, ok := vars[k]; ok {
			return v
		}
		return m
	})
}

// PhoneNorm returns digits with country code (v1 phone_norm).
func PhoneNorm(p, defaultCC string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	plus := p[0] == '+'
	d := onlyDigits(p)
	if d == "" {
		return ""
	}
	if plus {
		return d
	}
	if strings.HasPrefix(d, "00") {
		return d[2:]
	}
	d = strings.TrimLeft(d, "0")
	if len(d) <= 10 {
		return onlyDigits(defaultCC) + d
	}
	return d
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PhoneTail is the last 10 digits (duplicate detection).
func PhoneTail(p string) string {
	d := onlyDigits(p)
	if len(d) > 10 {
		return d[len(d)-10:]
	}
	return d
}

func Money(s *settings.Store, n float64) string {
	return s.Get("currency_symbol", "₹") + FormatNum(n)
}

// FormatNum formats with Indian digit grouping (12,34,567).
func FormatNum(n float64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	str := strconv.FormatInt(int64(n+0.5), 10)
	if len(str) > 3 {
		head, tail := str[:len(str)-3], str[len(str)-3:]
		var parts []string
		for len(head) > 2 {
			parts = append([]string{head[len(head)-2:]}, parts...)
			head = head[:len(head)-2]
		}
		if head != "" {
			parts = append([]string{head}, parts...)
		}
		str = strings.Join(parts, ",") + "," + tail
	}
	if neg {
		return "-" + str
	}
	return str
}

// ---------------------------------------------------------------- public links

// BookingLink: personal booking page of a lead, or the general booking page.
func BookingLink(a *app.App, l *Lead) string {
	if l != nil && l.Token != "" {
		return a.Cfg.AppURL() + "/book/" + l.Token
	}
	return ""
}

// GeneralBookingLink: the workspace booking page (token kept in setting booking_key).
func GeneralBookingLink(a *app.App, s *settings.Store) string {
	if k := s.Get("booking_key", ""); k != "" {
		return a.Cfg.AppURL() + "/book/g/" + k
	}
	return ""
}

// ReportLink: latest web report of a lead ("" when none).
func ReportLink(ctx context.Context, a *app.App, q db.Q, leadID int64) string {
	t, _ := db.Val[string](ctx, q, `SELECT token FROM reports WHERE lead_id=$1 ORDER BY id DESC LIMIT 1`, leadID)
	if t == "" {
		return ""
	}
	return a.Cfg.AppURL() + "/r/" + t
}

// LeadVars: template variables for messages, scripts and prompts (v1 lead_vars).
func LeadVars(ctx context.Context, a *app.App, q db.Q, s *settings.Store, l *Lead, extra map[string]string) map[string]string {
	cn := l.ContactName
	if cn == "" {
		cn = "Sir/Madam"
	}
	first := strings.Fields(cn)[0]
	f := func(p *float64) string {
		if p == nil {
			return ""
		}
		return strconv.FormatFloat(*p, 'f', -1, 64)
	}
	rv := ""
	if l.Reviews != nil {
		rv = strconv.Itoa(*l.Reviews)
	}
	city := l.City
	v := map[string]string{
		"contact_name": cn, "first_name": first, "booking_link": BookingLink(a, l), "service": l.Service, "status": StatusLabel(l.Status),
		"business_name": l.Name, "category": l.Category, "address": l.Address, "city": city, "phone": l.Phone, "email": l.Email, "website": l.Website,
		"rating": f(l.Rating), "reviews": rv, "keyword": l.Keyword,
		"agency_name": s.Get("company_name", ""), "agency_phone": s.Get("company_phone", ""), "agency_email": s.Get("company_email", ""),
		"agency_website": s.Get("company_website", ""), "agency_address": s.Get("company_address", ""),
		"sender_name": s.Get("sender_name", s.Get("company_name", "")), "report_link": ReportLink(ctx, a, q, l.ID),
		"currency": s.Get("currency", "INR"), "currency_symbol": s.Get("currency_symbol", "₹"),
		"yearly_fee": FormatNum(s.Float("yearly_fee", 60000)), "website_fee": FormatNum(s.Float("website_fee", 25000)),
	}
	for k, x := range extra {
		v[k] = x
	}
	return v
}

// ---------------------------------------------------------------- lead job progress, abort & process log

// ErrAborted: the user pressed Abort for this lead.
var ErrAborted = fmt.Errorf("stopped by user")

// Progress updates the lead's progress bar and returns ErrAborted when the user stopped it.
func Progress(ctx context.Context, q db.Q, leadID int64, pct int, msg string) error {
	pct = max(0, min(100, pct))
	if len([]rune(msg)) > 180 {
		msg = string([]rune(msg)[:180])
	}
	var abort bool
	if err := q.QueryRow(ctx, `UPDATE leads SET progress=$2, progress_msg=$3 WHERE id=$1 RETURNING abort_flag`, leadID, pct, msg).Scan(&abort); err != nil {
		return err
	}
	if abort {
		return ErrAborted
	}
	return nil
}

// Aborted reports whether the user stopped the lead's work.
func Aborted(ctx context.Context, q db.Q, leadID int64) bool {
	b, _ := db.Val[bool](ctx, q, `SELECT abort_flag FROM leads WHERE id=$1`, leadID)
	return b
}

// Trace writes the "View process" log of a lead's background work (job_steps).
type Trace struct {
	A     *app.App
	Org   int64
	Lead  int64
	Job   int64
	cur   int64
	t0    time.Time
	Label string
}

func (t *Trace) exec(ctx context.Context, fn func(q db.Q) error) {
	if t == nil || t.Lead == 0 {
		return
	}
	t.A.DB.Org(context.WithoutCancel(ctx), t.Org, func(tx pgx.Tx) error { return fn(tx) })
}

// Step closes the open step (ok) and starts a new one. kind: step | ai | start.
func (t *Trace) Step(ctx context.Context, msg, kind string) {
	if t == nil || t.Lead == 0 {
		return
	}
	t.Close(ctx, "ok", "")
	t.t0 = time.Now()
	t.Label = msg
	if kind == "" {
		kind = "step"
	}
	if len([]rune(msg)) > 190 {
		msg = string([]rune(msg)[:190])
	}
	t.exec(ctx, func(q db.Q) error {
		return q.QueryRow(ctx, `INSERT INTO job_steps (lead_id,job_id,kind,step) VALUES ($1,$2,$3,$4) RETURNING id`, t.Lead, t.Job, kind, msg).Scan(&t.cur)
	})
}

// Close finishes the open step with a status (ok | err | warn) and optional detail.
func (t *Trace) Close(ctx context.Context, status, detail string) {
	if t == nil || t.cur == 0 {
		return
	}
	id, ms := t.cur, time.Since(t.t0).Milliseconds()
	t.cur = 0
	if len([]rune(detail)) > 2000 {
		detail = string([]rune(detail)[:2000])
	}
	t.exec(ctx, func(q db.Q) error {
		_, err := q.Exec(ctx, `UPDATE job_steps SET status=$2, ms=$3, detail=CASE WHEN $4='' THEN detail ELSE $4 END WHERE id=$1`, id, status, ms, detail)
		return err
	})
}

// AI records model, tokens and cost on the open step (call after an AI request).
func (t *Trace) AI(ctx context.Context, model string, in, out int, cost float64) {
	if t == nil || t.cur == 0 {
		return
	}
	id := t.cur
	t.exec(ctx, func(q db.Q) error {
		_, err := q.Exec(ctx, `UPDATE job_steps SET model=$2,tokens_in=$3,tokens_out=$4,cost=$5 WHERE id=$1`, id, model, in, out, cost)
		return err
	})
}

// Note sets the detail of the open step.
func (t *Trace) Note(ctx context.Context, detail string) {
	if t == nil || t.cur == 0 {
		return
	}
	id := t.cur
	t.exec(ctx, func(q db.Q) error {
		_, err := q.Exec(ctx, `UPDATE job_steps SET detail=$2 WHERE id=$1`, id, detail)
		return err
	})
}
