package prospecting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// leadJobTypes are the background jobs that work on one lead (ref_id = lead id).
var leadJobTypes = []string{"enrich", "audit", "report", "contact"}

var busyStages = []string{"queued", "auditing", "reporting", "contacting"}

// tracker records API usage in its own short transaction (never held across the HTTP call).
func (m *mod) tracker(ctx context.Context, org int64) usageFn {
	return func(service, op string, failed bool) {
		cost := 0.0
		if !failed {
			cost = skuPrice[service]
		}
		if err := m.a.DB.Org(context.WithoutCancel(ctx), org, func(tx pgx.Tx) error {
			app.TrackUsage(ctx, tx, service, op, 1, 0, cost, failed)
			return nil
		}); err != nil {
			slog.Warn("usage", "err", err)
		}
	}
}

// ---------------------------------------------------------------- search (one result page per job)

type campaignRow struct {
	ID       int64  `db:"id"`
	Keyword  string `db:"keyword"`
	Location string `db:"location"`
	LimitN   int    `db:"limit_n"`
	Source   string `db:"source"`
	Status   string `db:"status"`
	Found    int    `db:"found"`
	Skipped  int    `db:"skipped"`
}

type searchPayload struct {
	Next string `json:"next,omitempty"`
}

func (m *mod) jobSearch(ctx context.Context, j *jobs.Job) error {
	var p searchPayload
	_ = j.Decode(&p)
	var c campaignRow
	var s *settings.Store
	err := m.a.OrgTx(ctx, j.OrgID, func(tx pgx.Tx, st *settings.Store) error {
		var err error
		c, err = db.One[campaignRow](ctx, tx, `SELECT id,keyword,location,limit_n,source,status,found,skipped FROM campaigns WHERE id=$1`, j.RefID)
		s = st
		return err
	})
	if errors.Is(err, db.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if c.Status == "stopped" {
		return nil
	}
	if err := m.searchPage(ctx, j.OrgID, c, s, p.Next); err != nil {
		m.a.DB.Org(context.WithoutCancel(ctx), j.OrgID, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `UPDATE campaigns SET status='error', error=$2 WHERE id=$1`, c.ID, cut(err.Error(), 2000))
			return e
		})
		return jobs.Permanent(err)
	}
	return nil
}

// searchPage fetches one page. Businesses already in your data (any earlier search or Leads) are skipped.
// Google Places: a free IDs-only request first, so pages with nothing new cost nothing and only new businesses are paid for.
func (m *mod) searchPage(ctx context.Context, org int64, c campaignRow, s *settings.Store, next string) error {
	q := strings.TrimSpace(c.Keyword)
	if c.Location != "" {
		q = strings.TrimSpace(q + " in " + c.Location)
	}
	have, skip := c.Found, c.Skipped
	need := c.LimitN - have
	if need <= 0 {
		return m.a.DB.Org(ctx, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE campaigns SET status='ready' WHERE id=$1`, c.ID)
			return err
		})
	}
	dedupe := s.Get("maps_skip_existing", "1") == "1"
	// place_id → true when already in this search, false when in another search / Leads
	known := func(ids []string) (map[string]bool, error) {
		out := map[string]bool{}
		if len(ids) == 0 {
			return out, nil
		}
		err := m.a.DB.Org(ctx, org, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT place_id, bool_or(campaign_id=$1) FROM leads WHERE place_id = ANY($2) GROUP BY place_id`, c.ID, ids)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id string
				var mine bool
				if err := rows.Scan(&id, &mine); err != nil {
					return err
				}
				if mine || dedupe {
					out[id] = mine
				}
			}
			return rows.Err()
		})
		return out, err
	}
	mc := &mapsClient{s: s, track: m.tracker(ctx, org)}
	var items []Place
	var nextTok string
	more := false
	if c.Source == "places" {
		ids, nx, err := mc.ids(ctx, q, next)
		if err != nil {
			return err
		}
		kn, err := known(ids)
		if err != nil {
			return err
		}
		var fresh []string
		for _, id := range ids {
			mine, ok := kn[id]
			if ok && !mine {
				skip++
			}
			if !ok {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) > need {
			fresh = fresh[:need]
		}
		if len(fresh) > 0 {
			// Cheapest way to fetch only the new ones: 1 Text Search page, or 1 Place Details request per business
			pt, pd := skuPrice[mc.sku(false)], skuPrice[mc.sku(true)]
			by := map[string]Place{}
			if float64(len(fresh))*pd >= pt {
				full, _, err := mc.places(ctx, q, next)
				if err != nil {
					slog.Info("places page fetch, using details instead", "err", err)
				}
				for _, it := range full {
					by[it.PlaceID] = it
				}
			}
			for _, id := range fresh {
				if it, ok := by[id]; ok {
					items = append(items, it)
					continue
				}
				it, err := mc.details(ctx, id)
				if err != nil {
					return err
				}
				items = append(items, it)
			}
		}
		nextTok, more = nx, len(ids) > 0
	} else {
		res, nx, err := mc.serp(ctx, q, next)
		if err != nil {
			return err
		}
		ids := make([]string, len(res))
		for i, it := range res {
			ids[i] = it.PlaceID
		}
		kn, err := known(ids)
		if err != nil {
			return err
		}
		for _, it := range res {
			if mine, ok := kn[it.PlaceID]; ok {
				if !mine {
					skip++
				}
				continue
			}
			items = append(items, it)
		}
		nextTok, more = nx, len(res) > 0
	}

	// Insert the new businesses, their journey entry and contact-scan jobs in one statement
	type row struct {
		Position int      `json:"position"`
		PlaceID  string   `json:"place_id"`
		Name     string   `json:"name"`
		Category string   `json:"category"`
		Address  string   `json:"address"`
		City     string   `json:"city"`
		Phone    string   `json:"phone"`
		Website  string   `json:"website"`
		Rating   *float64 `json:"rating"`
		Reviews  int      `json:"reviews"`
		MapsURL  string   `json:"maps_url"`
		Lat      *float64 `json:"lat"`
		Lng      *float64 `json:"lng"`
		Raw      PlaceRaw `json:"raw"`
	}
	var rows []row
	seen := map[string]bool{}
	for _, it := range items {
		if have >= c.LimitN {
			break
		}
		if seen[it.PlaceID] || it.PlaceID == "" {
			continue
		}
		seen[it.PlaceID] = true
		have++
		city := it.City
		if city == "" {
			city = c.Location
		}
		rows = append(rows, row{have, it.PlaceID, cut(it.Name, 250), cut(it.Category, 180), cut(it.Address, 490), cut(city, 110), cut(it.Phone, 40), cut(it.Website, 490), it.Rating, it.Reviews, cut(it.MapsURL, 490), it.Lat, it.Lng, it.Raw})
	}
	return m.a.DB.Org(ctx, org, func(tx pgx.Tx) error {
		if r := app.Remaining(ctx, tx, org, "leads"); r >= 0 && len(rows) > r { // plan limit on leads
			rows = rows[:r]
		}
		if len(rows) > 0 {
			js, _ := json.Marshal(rows)
			var n int
			err := tx.QueryRow(ctx, `WITH src AS (
  SELECT * FROM jsonb_to_recordset($2::jsonb) AS x(position int, place_id text, name text, category text, address text, city text, phone text, website text, rating numeric, reviews int, maps_url text, lat numeric, lng numeric, raw jsonb)
), ins AS (
  INSERT INTO leads (campaign_id,position,place_id,name,category,address,city,phone,phone_source,website,rating,reviews,maps_url,lat,lng,raw,source,source_detail,last_activity)
  SELECT $1,position,place_id,name,category,address,city,phone,CASE WHEN phone<>'' THEN 'maps' ELSE '' END,website,round(rating,1),reviews,maps_url,lat,lng,raw,'maps',$3,now() FROM src
  ON CONFLICT (org_id,campaign_id,place_id) DO NOTHING
  RETURNING id, position, website
), ev AS (
  INSERT INTO events (lead_id,type,channel,message) SELECT id,'found','maps','Found on Google Maps at position #'||position||' for "'||$4||'"' FROM ins
), jb AS (
  INSERT INTO jobs (org_id,type,ref_id,max_attempts) SELECT app_org(),'enrich',id,2 FROM ins WHERE website<>''
)
SELECT count(*) FROM ins`, c.ID, string(js), cut(q, 180), q).Scan(&n)
			if err != nil {
				return err
			}
			have -= len(rows) - n
		}
		status := "ready"
		if have < c.LimitN && nextTok != "" && more {
			status = "searching"
			at := time.Now()
			if c.Source == "places" {
				at = at.Add(2 * time.Second) // the next-page token needs a moment to become valid
			}
			if _, err := jobs.Push(ctx, tx, org, "search", c.ID, searchPayload{Next: nextTok}, jobs.Opt{At: at, MaxAttempts: 1}); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE campaigns SET found=$2, skipped=$3, status=CASE WHEN status='stopped' THEN status ELSE $4 END WHERE id=$1`, c.ID, have, skip, status)
		return err
	})
}

// ---------------------------------------------------------------- enrich (contact scan of the website)

func (m *mod) jobEnrich(ctx context.Context, j *jobs.Job) error {
	return m.enrich(ctx, j.OrgID, j.RefID)
}

func (m *mod) enrich(ctx context.Context, org, lead int64) error {
	type lr struct {
		Website string `db:"website"`
		Phone   string `db:"phone"`
		Email   string `db:"email"`
	}
	var l lr
	err := m.a.DB.Org(ctx, org, func(tx pgx.Tx) error {
		var err error
		l, err = db.One[lr](ctx, tx, `SELECT website,phone,email FROM leads WHERE id=$1`, lead)
		return err
	})
	if errors.Is(err, db.ErrNoRows) || l.Website == "" {
		return nil
	}
	if err != nil {
		return err
	}
	c := scanContacts(ctx, l.Website)
	soc := map[string]string{}
	for k, v := range c.Socials {
		soc[k] = v
	}
	if c.WhatsApp != "" {
		soc["whatsapp"] = c.WhatsApp
	}
	sj, _ := json.Marshal(soc)
	var found []string
	phone, email := "", ""
	if l.Phone == "" && len(c.Phones) > 0 {
		phone = cut(c.Phones[0], 40)
		found = append(found, "phone "+phone)
	}
	if l.Email == "" && len(c.Emails) > 0 {
		email = cut(c.Emails[0], 190)
		found = append(found, "email "+email)
	}
	return m.a.DB.Org(ctx, org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE leads SET socials=$2::jsonb,
  phone=CASE WHEN $3<>'' AND phone='' THEN $3 ELSE phone END,
  phone_source=CASE WHEN $3<>'' AND phone='' THEN 'website' ELSE phone_source END,
  email=CASE WHEN $4<>'' AND email='' THEN $4 ELSE email END WHERE id=$1`, lead, string(sj), phone, email); err != nil {
			return err
		}
		msg := "Website scanned – no extra contact details found"
		if len(found) > 0 {
			msg = "Found on website: " + strings.Join(found, ", ")
		}
		return app.Event(ctx, tx, lead, "enriched", msg, "web", nil)
	})
}

// ---------------------------------------------------------------- lead jobs: trace, progress, retry & abort

type run struct {
	m    *mod
	org  int64
	lead int64
	t    *shared.Trace
	last string
}

// progress updates the lead's progress bar, logs a process step and stops when the user aborted.
func (r *run) progress(ctx context.Context, pct int, msg string) error {
	if msg != r.last && !strings.HasPrefix(msg, "AI is ") {
		r.t.Step(ctx, msg, "step")
		r.last = msg
	}
	return r.m.a.DB.Org(ctx, r.org, func(tx pgx.Tx) error { return shared.Progress(ctx, tx, r.lead, pct, msg) })
}

func (r *run) tx(ctx context.Context, fn func(tx pgx.Tx, s *settings.Store) error) error {
	return r.m.a.OrgTx(ctx, r.org, fn)
}

// leadJob wraps audit/contact jobs: process log, abort → cancelled, one retry, then the lead is marked failed.
func (m *mod) leadJob(title string, fn func(ctx context.Context, j *jobs.Job, r *run) error) jobs.Handler {
	return func(ctx context.Context, j *jobs.Job) error {
		t := &shared.Trace{A: m.a, Org: j.OrgID, Lead: j.RefID, Job: j.ID}
		h := title
		if j.Attempts > 1 {
			h += fmt.Sprintf(" (attempt %d)", j.Attempts)
		}
		t.Step(ctx, h, "start")
		r := &run{m: m, org: j.OrgID, lead: j.RefID, t: t}
		err := fn(ctx, j, r)
		var de jobs.DeferError
		bg := context.WithoutCancel(ctx)
		switch {
		case err == nil:
			t.Close(bg, "ok", "")
			return nil
		case errors.Is(err, shared.ErrAborted) || errors.Is(err, jobs.ErrCancelled):
			t.Step(bg, "Stopped by user", "step")
			t.Close(bg, "err", "The process was aborted.")
			return jobs.ErrCancelled
		case errors.As(err, &de):
			t.Close(bg, "ok", fmt.Sprintf("Waiting – trying again in %ds", int(de.After.Seconds())))
			return err
		}
		msg := err.Error()
		retry := j.Attempts < j.MaxAttempts
		t.Close(bg, "err", msg)
		if retry {
			t.Step(bg, "Error – will retry in 1 minute: "+strim(msg, 140), "step")
		} else {
			t.Step(bg, "Failed: "+strim(msg, 140), "step")
		}
		t.Close(bg, "err", msg)
		m.a.DB.Org(bg, j.OrgID, func(tx pgx.Tx) error {
			if retry {
				_, e := tx.Exec(bg, `UPDATE leads SET progress_msg=$2 WHERE id=$1`, j.RefID, cut("Retrying after error: "+cut(msg, 140), 180))
				return e
			}
			if _, e := tx.Exec(bg, `UPDATE leads SET stage='failed', last_error=$2, progress_msg='Failed' WHERE id=$1`, j.RefID, cut(msg, 2000)); e != nil {
				return e
			}
			return app.Event(bg, tx, j.RefID, "error", strings.ToUpper(j.Type[:1])+j.Type[1:]+" failed: "+msg, "", nil)
		})
		if retry {
			return err
		}
		return jobs.Permanent(err)
	}
}

// setStage changes the outreach stage; with a message it is also written to the journey.
func setStage(ctx context.Context, q db.Q, lead int64, stage, msg string) error {
	if _, err := q.Exec(ctx, `UPDATE leads SET stage=$2 WHERE id=$1`, lead, stage); err != nil {
		return err
	}
	if msg != "" {
		return app.Event(ctx, q, lead, "stage", msg, "", map[string]string{"stage": stage})
	}
	return nil
}

// ---------------------------------------------------------------- audit (website + Google Maps) → report

type auditPayload struct {
	Steps []string `json:"steps,omitempty"`
	PW    int64    `json:"pw,omitempty"`
	PN    int64    `json:"pn,omitempty"`
	Auto  bool     `json:"auto,omitempty"`
	Bulk  bool     `json:"bulk,omitempty"`
}

func (p auditPayload) has(step string) bool {
	for _, s := range p.Steps {
		if s == step {
			return true
		}
	}
	return false
}

type auditLeadRow struct {
	ID          int64           `db:"id"`
	CampaignID  int64           `db:"campaign_id"`
	Position    int             `db:"position"`
	Name        string          `db:"name"`
	Category    string          `db:"category"`
	City        string          `db:"city"`
	Phone       string          `db:"phone"`
	PhoneSource string          `db:"phone_source"`
	Email       string          `db:"email"`
	Website     string          `db:"website"`
	Rating      *float64        `db:"rating"`
	Reviews     *int            `db:"reviews"`
	Raw         json.RawMessage `db:"raw"`
	Audit       json.RawMessage `db:"audit"`
	Keyword     string          `db:"keyword"`
}

func (m *mod) jobAudit(ctx context.Context, j *jobs.Job, r *run) error {
	var p auditPayload
	_ = j.Decode(&p)
	if len(p.Steps) == 0 {
		p.Steps = []string{"audit", "report"}
	}
	var l auditLeadRow
	var s *settings.Store
	err := r.tx(ctx, func(tx pgx.Tx, st *settings.Store) error {
		var err error
		s = st
		l, err = db.One[auditLeadRow](ctx, tx, `SELECT l.id,l.campaign_id,l.position,l.name,l.category,l.city,l.phone,l.phone_source,l.email,l.website,l.rating::float8 rating,l.reviews,l.raw,l.audit,COALESCE(NULLIF(c.keyword,''),l.category) keyword
FROM leads l LEFT JOIN campaigns c ON c.id=l.campaign_id WHERE l.id=$1`, j.RefID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE leads SET stage='auditing', last_error='' WHERE id=$1`, l.ID)
		return err
	})
	if errors.Is(err, db.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := r.progress(ctx, 5, "Starting audit"); err != nil {
		return err
	}
	if l.Website != "" && l.Email == "" {
		if err := r.progress(ctx, 8, "Looking for email & phone on the website"); err != nil {
			return err
		}
		if err := m.enrich(ctx, r.org, l.ID); err != nil {
			return err
		}
	}
	audit := map[string]json.RawMessage{}
	if len(l.Audit) > 0 {
		_ = json.Unmarshal(l.Audit, &audit)
	}
	var raw PlaceRaw
	_ = json.Unmarshal(l.Raw, &raw)
	al := AuditLead{Name: l.Name, City: l.City, Keyword: l.Keyword, Category: l.Category, Phone: l.Phone, PhoneSource: l.PhoneSource, Website: l.Website, Position: l.Position, Raw: raw}
	if l.Rating != nil {
		al.Rating = *l.Rating
	}
	if l.Reviews != nil {
		al.Reviews = *l.Reviews
	}
	var web *Result
	if l.Website != "" {
		actx := context.WithValue(ctx, pageSpeedCtxKey{}, m.tracker(ctx, r.org))
		web, err = auditWebsite(actx, s, l.Website, func(pct int, msg string) error { return r.progress(ctx, 10+pct*4/10, msg) }, al)
		if err != nil {
			return err
		}
		audit["website"], _ = json.Marshal(web)
	}
	if err := r.progress(ctx, 52, "Auditing Google Maps profile"); err != nil {
		return err
	}
	var comp []Competitor
	if l.CampaignID != 0 {
		if err := r.tx(ctx, func(tx pgx.Tx, _ *settings.Store) error {
			var err error
			comp, err = db.All[Competitor](ctx, tx, `SELECT COALESCE(reviews,0) reviews, COALESCE(rating,0)::float8 rating FROM leads WHERE campaign_id=$1 AND id<>$2 ORDER BY position LIMIT 3`, l.CampaignID, l.ID)
			return err
		}); err != nil {
			return err
		}
	}
	mp := auditMap(al, comp)
	audit["map"], _ = json.Marshal(mp)
	aj, _ := json.Marshal(audit)
	var ws *int
	msg := "No website · "
	if web != nil {
		ws = &web.Score
		msg = fmt.Sprintf("Website score %d/100 · ", web.Score)
	}
	if err := r.tx(ctx, func(tx pgx.Tx, _ *settings.Store) error {
		if _, err := tx.Exec(ctx, `UPDATE leads SET audit=$2::jsonb, web_score=$3, map_score=$4 WHERE id=$1`, l.ID, string(aj), ws, mp.Score); err != nil {
			return err
		}
		return app.Event(ctx, tx, l.ID, "audited", msg+fmt.Sprintf("Google Maps score %d/100", mp.Score), "", nil)
	}); err != nil {
		return err
	}
	if !p.has("report") {
		if err := r.progress(ctx, 100, "Audit complete"); err != nil {
			return err
		}
		return r.tx(ctx, func(tx pgx.Tx, _ *settings.Store) error {
			_, err := tx.Exec(ctx, `UPDATE leads SET stage=CASE WHEN EXISTS (SELECT 1 FROM reports r WHERE r.lead_id=leads.id) THEN 'report_ready' ELSE 'new' END WHERE id=$1`, l.ID)
			return err
		})
	}
	if err := r.progress(ctx, 58, "Audit complete – queued for AI report"); err != nil {
		return err
	}
	prompt := p.PN
	if l.Website != "" {
		prompt = p.PW
	}
	return r.tx(ctx, func(tx pgx.Tx, _ *settings.Store) error {
		return enqueueReport(ctx, tx, r.org, l.ID, prompt, p.Auto || p.has("contact"), p.Bulk)
	})
}
