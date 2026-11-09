// Package crm: leads, pipeline, custom fields, journey, dashboard, CSV import/export.
// OWNER: crm module. Exported functions below are contracts used by other modules – keep signatures.
package crm

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/secure"
)

// CreateIn: core lead fields (v1 CRM::CORE) plus custom fields and extra data.
type CreateIn struct {
	Fields map[string]string // name, contact_name, email, phone, alt_phone, website, address, city, category, service, deal_value, message, tags, follow_up_at
	Custom map[string]string // custom field key → value
	Extra  map[string]any    // stored under custom._extra
	Source string            // maps | manual | website | facebook | google | webhook | import | booking
	Detail string            // source detail, e.g. the form name
	UserID int64             // assigned owner (0 = none)
}

// Field is a custom lead field (Settings → CRM → Custom fields).
type Field struct {
	ID       int64  `db:"id" json:"id"`
	Key      string `db:"fkey" json:"fkey"`
	Label    string `db:"label" json:"label"`
	Type     string `db:"type" json:"type"`
	Options  string `db:"options" json:"options"`
	Required bool   `db:"required" json:"required"`
	Sort     int    `db:"sort" json:"sort"`
}

// Fields returns the workspace's custom fields in display order.
func Fields(ctx context.Context, q db.Q) ([]Field, error) {
	return db.All[Field](ctx, q, `SELECT id,fkey,label,type,options,required,sort FROM fields ORDER BY sort,id`)
}

// FieldKeys returns the keys of the custom fields (for NormalizeFields).
func FieldKeys(fs []Field) []string {
	k := make([]string, len(fs))
	for i, f := range fs {
		k[i] = f.Key
	}
	return k
}

var nonDigitDot = regexp.MustCompile(`[^\d.]`)

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func sourceLabel(s string) string { return shared.SourceLabel(s) }

// orgTZ reads the workspace time zone inside a tenant transaction.
func orgTZ(ctx context.Context, q db.Q) *time.Location {
	tz, _ := db.Val[string](ctx, q, `SELECT v FROM settings WHERE k='timezone'`)
	return app.LoadTZ(tz)
}

// ParseTime reads timestamps from forms and APIs: RFC 3339, or a local date/time in loc.
func ParseTime(s string, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	for _, f := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02", "02/01/2006 15:04", "02/01/2006", "02-01-2006"} {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Create adds a lead or merges it into a recent duplicate (same email / phone within 30 days).
// Writes the journey and emits lead_created. Returns the lead id.
func Create(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, in CreateIn) (int64, error) {
	d := map[string]string{}
	for k, v := range in.Fields {
		if coreSet[k] {
			d[k] = strings.TrimSpace(v)
		}
	}
	name := d["name"]
	if name == "" {
		name = d["contact_name"]
	}
	if name == "" {
		name = "Unnamed lead"
	}
	d["name"] = cut(name, 250)
	cols := map[string]any{}
	for k, v := range d {
		switch k {
		case "deal_value", "follow_up_at":
		default:
			cols[k] = v
		}
	}
	if v, ok := d["deal_value"]; ok {
		f, _ := strconv.ParseFloat(nonDigitDot.ReplaceAllString(v, ""), 64)
		if f > 0 && f < 1e10 {
			cols["deal_value"] = f
		}
	}
	if v := d["follow_up_at"]; v != "" {
		if t, ok := ParseTime(v, orgTZ(ctx, tx)); ok {
			cols["follow_up_at"] = t
		}
	}
	if cols["tags"] != nil {
		cols["tags"] = cut(cols["tags"].(string), 490)
	}
	custom := map[string]any{}
	for k, v := range in.Custom {
		custom[k] = cut(strings.TrimSpace(v), 2000)
	}
	if len(in.Extra) > 0 {
		custom["_extra"] = in.Extra
	}
	src := in.Source
	if src == "" {
		src = "manual"
	}
	email := strings.ToLower(d["email"])
	tail := shared.PhoneTail(d["phone"])
	var dup dupRow
	found := false
	if email != "" || tail != "" {
		var w []string
		var args []any
		if email != "" {
			args = append(args, email)
			w = append(w, "email_lc=$"+strconv.Itoa(len(args)))
		}
		if tail != "" {
			args = append(args, tail)
			w = append(w, "phone_tail=$"+strconv.Itoa(len(args)))
		}
		r, err := db.One[dupRow](ctx, tx, `SELECT id,custom,message,assigned_to FROM leads WHERE is_lead AND created_at>now()-interval '30 days' AND (`+strings.Join(w, " OR ")+`) ORDER BY id DESC LIMIT 1`, args...)
		if err == nil {
			dup, found = r, true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	detail := cut(strings.TrimSpace(in.Detail), 180)
	if found {
		u := map[string]any{}
		for k, v := range cols {
			if k == "name" {
				continue
			}
			if s, ok := v.(string); ok && s == "" {
				continue
			}
			u[k] = v
		}
		c := dup.Custom
		if c == nil {
			c = map[string]any{}
		}
		for k, v := range custom {
			c[k] = v
		}
		cj, _ := json.Marshal(c)
		u["custom"] = cj
		if m := d["message"]; m != "" {
			if dup.Message != "" {
				m = dup.Message + "\n---\n" + m
			}
			u["message"] = strings.TrimSpace(m)
		}
		if in.UserID > 0 && dup.Assigned == nil {
			u["assigned_to"] = in.UserID
		}
		if err := db.Update(ctx, tx, "leads", dup.ID, u); err != nil {
			return 0, err
		}
		msg := "New enquiry received again via " + sourceLabel(src)
		if detail != "" {
			msg += " (" + detail + ")"
		}
		return dup.ID, app.Event(ctx, tx, dup.ID, "enriched", msg, src, nil)
	}
	cj, _ := json.Marshal(custom)
	now := time.Now()
	cols["campaign_id"] = 0
	cols["place_id"] = "crm-" + secure.Token(8)
	cols["is_lead"] = true
	cols["source"] = src
	cols["source_detail"] = detail
	cols["status"] = "new"
	cols["stage"] = "new"
	cols["custom"] = cj
	cols["lead_at"] = now
	cols["last_activity"] = now
	if d["phone"] != "" {
		cols["phone_source"] = "form"
	}
	if in.UserID > 0 {
		cols["assigned_to"] = in.UserID
	}
	id, err := db.Insert(ctx, tx, "leads", cols)
	if err != nil {
		return 0, err
	}
	msg := "New lead from " + sourceLabel(src)
	if detail != "" {
		msg += " · " + detail
	}
	if err := app.Event(ctx, tx, id, "found", msg, src, nil); err != nil {
		return 0, err
	}
	a.Emit(ctx, tx, orgID, "lead_created", id, map[string]any{"source": src})
	return id, nil
}

type dupRow struct {
	ID       int64          `db:"id"`
	Custom   map[string]any `db:"custom"`
	Message  string         `db:"message"`
	Assigned *int64         `db:"assigned_to"`
}

// ValidStatus reports a known CRM status key.
func ValidStatus(s string) bool {
	for _, x := range shared.CRMStatuses {
		if x[0] == s {
			return true
		}
	}
	return false
}

// SetStatus changes the CRM status (journey + status_changed).
func SetStatus(ctx context.Context, a *app.App, tx pgx.Tx, orgID, leadID int64, status, note string) error {
	if !ValidStatus(status) {
		return app.Bad("Unknown status.")
	}
	var cur, stage string
	if err := tx.QueryRow(ctx, `SELECT status,stage FROM leads WHERE id=$1 FOR UPDATE`, leadID).Scan(&cur, &stage); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.NotFound()
		}
		return err
	}
	if cur == status {
		return nil
	}
	newStage := stage
	if status == "won" {
		newStage = "won"
	}
	if status == "lost" && stage != "won" {
		newStage = "lost"
	}
	if _, err := tx.Exec(ctx, `UPDATE leads SET status=$2, is_lead=true, stage=$3, lead_at=COALESCE(lead_at, now()) WHERE id=$1`, leadID, status, newStage); err != nil {
		return err
	}
	msg := "Status: " + shared.StatusLabel(cur) + " → " + shared.StatusLabel(status)
	if note = strings.TrimSpace(note); note != "" {
		msg += " · " + note
	}
	if err := app.Event(ctx, tx, leadID, "stage", msg, "", nil); err != nil {
		return err
	}
	a.Emit(ctx, tx, orgID, "status_changed", leadID, map[string]any{"status": status})
	return nil
}

// SplitTags splits a comma separated tag list.
func SplitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// AddTag adds a tag (journey + tag_added).
func AddTag(ctx context.Context, a *app.App, tx pgx.Tx, orgID, leadID int64, tag string) error {
	tag = strings.TrimSpace(strings.ReplaceAll(tag, ",", ""))
	if tag == "" {
		return nil
	}
	var tags string
	if err := tx.QueryRow(ctx, `SELECT tags FROM leads WHERE id=$1 FOR UPDATE`, leadID).Scan(&tags); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.NotFound()
		}
		return err
	}
	t := SplitTags(tags)
	for _, x := range t {
		if x == tag {
			return nil
		}
	}
	t = append(t, tag)
	if _, err := tx.Exec(ctx, `UPDATE leads SET tags=$2 WHERE id=$1`, leadID, cut(strings.Join(t, ", "), 490)); err != nil {
		return err
	}
	if err := app.Event(ctx, tx, leadID, "note", "Tag added: "+tag, "", nil); err != nil {
		return err
	}
	a.Emit(ctx, tx, orgID, "tag_added", leadID, map[string]any{"tag": tag})
	return nil
}

// Promote moves a scraped business into Leads (is_lead) – e.g. when it replies or is won.
// reason may be a CRM status ("won", "contacted"…) that the new lead starts with; any other text is
// written to the journey. Emits lead_created and lead_moved.
func Promote(ctx context.Context, a *app.App, tx pgx.Tx, orgID, leadID int64, reason string) error {
	var isLead bool
	var status string
	if err := tx.QueryRow(ctx, `SELECT is_lead,status FROM leads WHERE id=$1 FOR UPDATE`, leadID).Scan(&isLead, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return app.NotFound()
		}
		return err
	}
	st, note := "new", ""
	if ValidStatus(reason) {
		st = reason
	} else {
		note = strings.TrimSpace(reason)
	}
	if isLead {
		if st == "won" && status != "won" {
			return SetStatus(ctx, a, tx, orgID, leadID, "won", "")
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE leads SET is_lead=true, status=$2, lead_at=now() WHERE id=$1`, leadID, st); err != nil {
		return err
	}
	msg := "Moved to Leads (" + shared.StatusLabel(st) + ")"
	if note != "" {
		msg += " · " + note
	}
	if err := app.Event(ctx, tx, leadID, "stage", msg, "", nil); err != nil {
		return err
	}
	a.Emit(ctx, tx, orgID, "lead_created", leadID, map[string]any{"source": "maps"})
	a.Emit(ctx, tx, orgID, "lead_moved", leadID, map[string]any{"status": st})
	return nil
}

// SaveCustom stores custom field values of a lead (only known fields; v1 CRM::saveCustom).
func SaveCustom(ctx context.Context, tx pgx.Tx, leadID int64, in map[string]any) error {
	if len(in) == 0 {
		return nil
	}
	fs, err := Fields(ctx, tx)
	if err != nil {
		return err
	}
	set := map[string]string{}
	for _, f := range fs {
		if v, ok := in[f.Key]; ok {
			s, _ := scalar(v)
			if v == nil {
				s = ""
			}
			set[f.Key] = cut(strings.TrimSpace(s), 2000)
		}
	}
	if len(set) == 0 {
		return nil
	}
	b, _ := json.Marshal(set)
	_, err = tx.Exec(ctx, `UPDATE leads SET custom=COALESCE(custom,'{}'::jsonb) || $2::jsonb WHERE id=$1`, leadID, b)
	return err
}

func Register(a *app.App) {
	registerLeads(a)
	registerFields(a)
	registerDashboard(a)
}
