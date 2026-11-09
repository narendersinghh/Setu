package prospecting

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/mod/shared"
)

const perPage = 50

// ---------------------------------------------------------------- campaigns (Map Scraper)

type campaignOut struct {
	ID        int64     `db:"id" json:"id"`
	Keyword   string    `db:"keyword" json:"keyword"`
	Location  string    `db:"location" json:"location"`
	LimitN    int       `db:"limit_n" json:"limit_n"`
	Source    string    `db:"source" json:"source"`
	Status    string    `db:"status" json:"status"`
	Found     int       `db:"found" json:"found"`
	Skipped   int       `db:"skipped" json:"skipped"`
	Error     string    `db:"error" json:"error"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	Leads     int       `db:"leads" json:"leads"`
	Web       int       `db:"web" json:"web"`
	RR        int       `db:"rr" json:"rr"`
	CT        int       `db:"ct" json:"ct"`
	RP        int       `db:"rp" json:"rp"`
	IL        int       `db:"il" json:"il"`
}

const contactedSQL = `(l.email_status IN ('sent','opened') OR l.wa_status IN ('sent','delivered','read','manual'))`

func (m *mod) campaignList(c *app.Ctx) (any, error) {
	rows, err := db.All[campaignOut](c, c.Tx(), `SELECT c.id,c.keyword,c.location,c.limit_n,c.source,c.status,c.found,c.skipped,c.error,c.created_at,
  x.leads,x.web,x.rr,x.ct,x.rp,x.il
FROM (SELECT * FROM campaigns ORDER BY id DESC LIMIT 200) c
LEFT JOIN LATERAL (SELECT count(*)::int leads, count(*) FILTER (WHERE l.website<>'')::int web,
  count(*) FILTER (WHERE l.stage IN ('report_ready','contacted','opened','replied','won','lost'))::int rr,
  count(*) FILTER (WHERE `+contactedSQL+`)::int ct, count(*) FILTER (WHERE l.replied_at IS NOT NULL)::int rp,
  count(*) FILTER (WHERE l.is_lead)::int il
  FROM leads l WHERE l.campaign_id=c.id) x ON true
ORDER BY c.id DESC`)
	if err != nil {
		return nil, err
	}
	src := c.S().Get("maps_source", "places")
	keyOk := c.S().Get("google_api_key", "") != ""
	if src == "serpapi" {
		keyOk = c.S().Get("serpapi_key", "") != ""
	}
	return map[string]any{"campaigns": rows, "source": src, "key_ok": keyOk}, nil
}

func (m *mod) campaignCreate(c *app.Ctx) (any, error) {
	var in struct {
		Keyword  string `json:"keyword"`
		Location string `json:"location"`
		Limit    int    `json:"limit"`
		Source   string `json:"source"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	kw, loc := cut(strings.TrimSpace(in.Keyword), 180), cut(strings.TrimSpace(in.Location), 180)
	if kw == "" {
		return nil, app.Bad("Please enter a keyword.")
	}
	n := in.Limit
	if n == 0 {
		n = 20
	}
	n = max(1, min(500, n))
	src := in.Source
	if src != "places" && src != "serpapi" {
		src = c.S().Get("maps_source", "places")
	}
	type notice struct {
		T   string `json:"t"`
		Msg string `json:"msg"`
	}
	var notes []notice
	if src == "places" && n > 60 {
		notes = append(notes, notice{"warn", "Google Places API returns at most 60 results per search – use SerpAPI for more. Fetching up to 60."})
		n = 60
	}
	if c.S().Get("maps_skip_existing", "1") == "1" {
		var at *time.Time
		if err := c.Tx().QueryRow(c, `SELECT max(created_at) FROM campaigns WHERE keyword=$1 AND location=$2`, kw, loc).Scan(&at); err != nil {
			return nil, err
		}
		if at != nil {
			notes = append(notes, notice{"info", "You searched this before (" + agoText(*at) + "). Businesses you already have are skipped – only new ones are added, and they are checked with a free request first."})
		}
	}
	id, err := db.Insert(c, c.Tx(), "campaigns", map[string]any{"keyword": kw, "location": loc, "limit_n": n, "source": src})
	if err != nil {
		return nil, err
	}
	if _, err := jobs.Push(c, c.Tx(), c.Org.ID, "search", id, nil, jobs.Opt{MaxAttempts: 1}); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "notices": notes}, nil
}

func agoText(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	}
	return strconv.Itoa(int(d.Hours()/24)) + "d ago"
}

type campCounts struct {
	T  int `db:"t" json:"t"`
	NW int `db:"nw" json:"nw"`
	NN int `db:"nn" json:"nn"`
	RR int `db:"rr" json:"rr"`
	CT int `db:"ct" json:"ct"`
	RP int `db:"rp" json:"rp"`
	FL int `db:"fl" json:"fl"`
	WK int `db:"wk" json:"wk"`
}

func (m *mod) campaignGet(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	cp, err := db.One[campaignOut](c, c.Tx(), `SELECT id,keyword,location,limit_n,source,status,found,skipped,error,created_at FROM campaigns WHERE id=$1`, id)
	if err != nil {
		return nil, app.NotFound()
	}
	cnt, err := db.One[campCounts](c, c.Tx(), `SELECT count(*)::int t, count(*) FILTER (WHERE l.website='')::int nw, count(*) FILTER (WHERE l.stage='new')::int nn,
  count(*) FILTER (WHERE l.stage='report_ready')::int rr, count(*) FILTER (WHERE `+contactedSQL+`)::int ct,
  count(*) FILTER (WHERE l.replied_at IS NOT NULL)::int rp, count(*) FILTER (WHERE l.stage='failed')::int fl,
  count(*) FILTER (WHERE l.stage IN ('queued','auditing','reporting','contacting'))::int wk
FROM leads l WHERE l.campaign_id=$1`, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"campaign": cp, "counts": cnt}, nil
}

func (m *mod) campaignStop(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	if _, err := c.Tx().Exec(c, `UPDATE campaigns SET status='stopped' WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if _, err := c.Tx().Exec(c, `DELETE FROM jobs WHERE org_id=app_org() AND type='search' AND ref_id=$1 AND status='pending'`, id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Search stopped."}, nil
}

func (m *mod) campaignMore(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	var in struct {
		N int `json:"n"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	n := max(1, min(500, in.N))
	var src string
	var lim int
	if err := c.Tx().QueryRow(c, `UPDATE campaigns SET status='searching', limit_n=limit_n+$2, error='' WHERE id=$1 RETURNING source, limit_n`, id, n).Scan(&src, &lim); err != nil {
		return nil, app.NotFound()
	}
	msg := "Fetching more listings…"
	if src == "places" && lim > 60 {
		c.Tx().Exec(c, `UPDATE campaigns SET limit_n=60 WHERE id=$1`, id)
		msg = "Google Places API returns at most 60 results per search – use SerpAPI for more. Fetching up to 60."
	}
	if _, err := c.Tx().Exec(c, `DELETE FROM jobs WHERE org_id=app_org() AND type='search' AND ref_id=$1 AND status='pending'`, id); err != nil {
		return nil, err
	}
	if _, err := jobs.Push(c, c.Tx(), c.Org.ID, "search", id, nil, jobs.Opt{MaxAttempts: 1}); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": msg}, nil
}

func (m *mod) campaignDelete(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	tx := c.Tx()
	var ids []int64
	if err := tx.QueryRow(c, `SELECT COALESCE(array_agg(id), '{}') FROM leads WHERE campaign_id=$1 AND NOT is_lead`, id).Scan(&ids); err != nil {
		return nil, err
	}
	_, pdfs, err := deleteLeads(c, tx, ids)
	if err != nil {
		return nil, err
	}
	// Businesses already moved to Leads are kept (detached from the search)
	if _, err := tx.Exec(c, `UPDATE leads SET campaign_id=0 WHERE campaign_id=$1 AND NOT EXISTS (SELECT 1 FROM leads x WHERE x.campaign_id=0 AND x.place_id=leads.place_id)`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(c, `DELETE FROM jobs WHERE org_id=app_org() AND type='search' AND ref_id=$1`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(c, `DELETE FROM campaigns WHERE id=$1`, id); err != nil {
		return nil, err
	}
	c.After(func() { m.removeFiles(pdfs) })
	return map[string]any{"ok": true, "message": "Search deleted. Businesses already moved to Leads were kept."}, nil
}

// ---------------------------------------------------------------- lists (campaign table & Scraped Data)

// listQ is the filter of the campaign table (Campaign + F) or the Scraped Data list (Q, Stage, CID, Web, IL).
type listQ struct {
	Campaign int64  `json:"campaign_id"`
	F        string `json:"f"`
	Q        string `json:"q"`
	Stage    string `json:"stage"`
	CID      int64  `json:"cid"`
	Web      string `json:"web"`
	IL       string `json:"il"`
}

var campaignTabs = map[string]string{
	"noweb": "l.website=''", "web": "l.website<>''", "new": "l.stage='new'", "ready": "l.stage='report_ready'",
	"contacted": contactedSQL, "replied": "l.replied_at IS NOT NULL", "failed": "l.stage='failed'",
	"working": "l.stage IN ('queued','auditing','reporting','contacting')",
}

func readListQ(c *app.Ctx) listQ {
	cid, _ := strconv.ParseInt(c.Query("cid"), 10, 64)
	return listQ{F: c.Query("f"), Q: c.Query("q"), Stage: c.Query("stage"), CID: cid, Web: c.Query("web"), IL: c.Query("il")}
}

func likeEsc(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (f listQ) where() (string, []any) {
	var w []string
	var args []any
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	if f.Campaign > 0 {
		w = append(w, "l.campaign_id="+arg(f.Campaign))
		if x, ok := campaignTabs[f.F]; ok {
			w = append(w, x)
		}
	} else {
		w = append(w, "l.source='maps'")
		if f.Q != "" {
			p := arg("%" + likeEsc(f.Q) + "%")
			w = append(w, fmt.Sprintf("(l.name ILIKE %[1]s OR l.email ILIKE %[1]s OR l.phone ILIKE %[1]s OR l.category ILIKE %[1]s OR l.address ILIKE %[1]s)", p))
		}
		if _, ok := shared.Stages[f.Stage]; ok {
			w = append(w, "l.stage="+arg(f.Stage))
		}
		if f.Stage == "working" {
			w = append(w, "l.stage IN ('queued','auditing','reporting','contacting')")
		}
		if f.Stage == "replied_any" {
			w = append(w, "l.replied_at IS NOT NULL")
		}
		if f.CID > 0 {
			w = append(w, "l.campaign_id="+arg(f.CID))
		}
		switch f.Web {
		case "1":
			w = append(w, "l.website<>''")
		case "0":
			w = append(w, "l.website=''")
		}
		switch f.IL {
		case "1":
			w = append(w, "l.is_lead")
		case "0":
			w = append(w, "NOT l.is_lead")
		}
	}
	return strings.Join(w, " AND "), args
}

func (f listQ) order() string {
	if f.Campaign > 0 {
		return "l.position, l.id"
	}
	return "COALESCE(l.last_activity,l.created_at) DESC, l.id DESC"
}

type rowOut struct {
	ID          int64      `db:"id" json:"id"`
	Position    int        `db:"position" json:"position"`
	Name        string     `db:"name" json:"name"`
	Category    string     `db:"category" json:"category"`
	City        string     `db:"city" json:"city"`
	Phone       string     `db:"phone" json:"phone"`
	PhoneSource string     `db:"phone_source" json:"phone_source"`
	Email       string     `db:"email" json:"email"`
	Website     string     `db:"website" json:"website"`
	Rating      *float64   `db:"rating" json:"rating"`
	Reviews     *int       `db:"reviews" json:"reviews"`
	WebScore    *int16     `db:"web_score" json:"web_score"`
	MapScore    *int16     `db:"map_score" json:"map_score"`
	Stage       string     `db:"stage" json:"stage"`
	Progress    int16      `db:"progress" json:"progress"`
	ProgressMsg string     `db:"progress_msg" json:"progress_msg"`
	LastError   string     `db:"last_error" json:"last_error"`
	EmailStatus string     `db:"email_status" json:"email_status"`
	WaStatus    string     `db:"wa_status" json:"wa_status"`
	RepliedAt   *time.Time `db:"replied_at" json:"replied_at"`
	RepliedVia  string     `db:"replied_via" json:"replied_via"`
	IsLead      bool       `db:"is_lead" json:"is_lead"`
	CampaignID  int64      `db:"campaign_id" json:"campaign_id"`
}

const rowCols = `l.id,l.position,l.name,l.category,l.city,l.phone,l.phone_source,l.email,l.website,l.rating::float8 rating,l.reviews,l.web_score,l.map_score,l.stage,l.progress,l.progress_msg,l.last_error,l.email_status,l.wa_status,l.replied_at,l.replied_via,l.is_lead,l.campaign_id`

func (m *mod) list(c *app.Ctx, f listQ) (any, error) {
	w, args := f.where()
	var total int
	if err := c.Tx().QueryRow(c, `SELECT count(*) FROM leads l WHERE `+w, args...).Scan(&total); err != nil {
		return nil, err
	}
	pages := max(1, (total+perPage-1)/perPage)
	pg := max(1, min(pages, c.QueryInt("p", 1)))
	rows, err := db.All[rowOut](c, c.Tx(), `SELECT `+rowCols+` FROM leads l WHERE `+w+` ORDER BY `+f.order()+fmt.Sprintf(` LIMIT %d OFFSET %d`, perPage, (pg-1)*perPage), args...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"rows": rows, "total": total, "page": pg, "pages": pages, "per": perPage}, nil
}

func (m *mod) campaignLeads(c *app.Ctx) (any, error) {
	f := readListQ(c)
	f.Campaign = c.ParamID("id")
	return m.list(c, f)
}

func (m *mod) prospectList(c *app.Ctx) (any, error) { return m.list(c, readListQ(c)) }

func (m *mod) prospectSummary(c *app.Ctx) (any, error) {
	type k struct {
		T  int `db:"t" json:"t"`
		WK int `db:"wk" json:"wk"`
		RR int `db:"rr" json:"rr"`
		RP int `db:"rp" json:"rp"`
		IL int `db:"il" json:"il"`
	}
	kp, err := db.One[k](c, c.Tx(), `SELECT count(*)::int t, count(*) FILTER (WHERE stage IN ('queued','auditing','reporting','contacting'))::int wk,
  count(*) FILTER (WHERE stage='report_ready')::int rr, count(*) FILTER (WHERE replied_at IS NOT NULL)::int rp, count(*) FILTER (WHERE is_lead)::int il
FROM leads WHERE source='maps'`)
	if err != nil {
		return nil, err
	}
	type camp struct {
		ID       int64  `db:"id" json:"id"`
		Keyword  string `db:"keyword" json:"keyword"`
		Location string `db:"location" json:"location"`
	}
	cs, err := db.All[camp](c, c.Tx(), `SELECT id,keyword,location FROM campaigns ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	st := make([][3]string, 0, len(stageOrder))
	for _, s := range stageOrder {
		x := shared.Stages[s]
		st = append(st, [3]string{s, x[0], x[1]})
	}
	return map[string]any{"kpi": kp, "campaigns": cs, "stages": st}, nil
}

var stageOrder = []string{"new", "queued", "auditing", "reporting", "report_ready", "contacting", "contacted", "opened", "replied", "won", "lost", "failed", "skipped"}

// ---------------------------------------------------------------- CSV export (streamed)

func (m *mod) campaignExport(c *app.Ctx) (any, error) {
	f := readListQ(c)
	f.Campaign = c.ParamID("id")
	return nil, m.export(c, f)
}

func (m *mod) prospectExport(c *app.Ctx) (any, error) {
	f := readListQ(c)
	return nil, m.export(c, f)
}

// csvCell protects against formula injection (cells starting with = + - @ run as formulas in Excel/Sheets).
func csvCell(v string) string {
	if v != "" && strings.ContainsAny(v[:1], "=+-@\t\r") {
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return "'" + v
		}
	}
	return v
}

func (m *mod) export(c *app.Ctx, f listQ) error {
	type field struct {
		Key   string `db:"fkey"`
		Label string `db:"label"`
	}
	fs, err := db.All[field](c, c.Tx(), `SELECT fkey,label FROM fields ORDER BY sort,id`)
	if err != nil {
		return err
	}
	w, args := f.where()
	order := "l.id DESC"
	if f.Campaign > 0 {
		order = "l.position, l.id"
	}
	rows, err := c.Tx().Query(c, `SELECT l.name,l.contact_name,l.category,l.address,l.city,l.phone,l.alt_phone,l.email,l.website,l.source,l.source_detail,l.status,l.service,
  COALESCE(l.deal_value::text,''),l.tags,l.follow_up_at,COALESCE(l.rating::text,''),COALESCE(l.reviews::text,''),l.maps_url,COALESCE(l.web_score::text,''),COALESCE(l.map_score::text,''),
  l.stage,l.email_status,l.wa_status,l.replied_at,l.created_at,l.custom FROM leads l WHERE `+w+` ORDER BY `+order, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	c.Raw()
	h := c.W.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="setu-`+time.Now().In(c.TZ()).Format("20060102-150405")+`.csv"`)
	h.Set("Cache-Control", "no-store")
	c.W.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(c.W)
	head := []string{"Name", "Contact person", "Category", "Address", "City", "Phone", "Alt phone", "Email", "Website", "Source", "Source detail", "Status", "Service", "Deal value", "Tags", "Follow-up", "Rating", "Reviews", "Maps URL", "Website score", "Maps score", "Outreach stage", "Email status", "WhatsApp status", "Replied at", "Created"}
	for _, x := range fs {
		head = append(head, x.Label)
	}
	cw.Write(head)
	tz := c.TZ()
	ts := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.In(tz).Format("2006-01-02 15:04:05")
	}
	n := 0
	for rows.Next() {
		var v [15]string
		var dv, rating, reviews, maps, ws, ms, stage, es, wa string
		var follow, replied *time.Time
		var created time.Time
		var custom map[string]any
		if err := rows.Scan(&v[0], &v[1], &v[2], &v[3], &v[4], &v[5], &v[6], &v[7], &v[8], &v[9], &v[10], &v[11], &v[12], &dv, &v[13], &follow, &rating, &reviews, &maps, &ws, &ms, &stage, &es, &wa, &replied, &created, &custom); err != nil {
			return err
		}
		st := v[11]
		v[11] = shared.StatusLabel(st)
		sl := stage
		if x, ok := shared.Stages[stage]; ok {
			sl = x[0]
		}
		rec := []string{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9], v[10], v[11], v[12], dv, v[13], ts(follow), rating, reviews, maps, ws, ms, sl, es, wa, ts(replied), ts(&created)}
		for _, x := range fs {
			switch cv := custom[x.Key].(type) {
			case nil:
				rec = append(rec, "")
			case []any:
				p := make([]string, len(cv))
				for i, e := range cv {
					p[i] = fmt.Sprint(e)
				}
				rec = append(rec, strings.Join(p, ", "))
			default:
				rec = append(rec, fmt.Sprint(cv))
			}
		}
		for i := range rec {
			rec[i] = csvCell(rec[i])
		}
		cw.Write(rec)
		if n++; n%500 == 0 {
			cw.Flush()
		}
	}
	cw.Flush()
	return rows.Err()
}

// ---------------------------------------------------------------- bulk actions & pipeline

type bulkIn struct {
	Action string   `json:"action"`
	IDs    []int64  `json:"ids"`
	All    bool     `json:"all"`    // every record matching Filter (not only the selected page)
	Filter *listQ   `json:"filter"` // with All
	Steps  []string `json:"steps"`
	PW     int64    `json:"pw"`
	PN     int64    `json:"pn"`
	Auto   bool     `json:"auto"`
}

// targets resolves the ids of a bulk request (selected ids, or every record matching the filter).
func (m *mod) targets(c *app.Ctx, in bulkIn) ([]int64, error) {
	if !in.All || in.Filter == nil {
		if len(in.IDs) > 100000 {
			return nil, app.Bad("Too many records selected.")
		}
		return in.IDs, nil
	}
	w, args := in.Filter.where()
	var ids []int64
	err := c.Tx().QueryRow(c, `SELECT COALESCE(array_agg(id), '{}') FROM (SELECT l.id FROM leads l WHERE `+w+` ORDER BY `+in.Filter.order()+` LIMIT 100000) x`, args...).Scan(&ids)
	return ids, err
}

func (m *mod) prospectBulk(c *app.Ctx) (any, error) {
	var in bulkIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	ids, err := m.targets(c, in)
	if err != nil {
		return nil, err
	}
	msg, err := m.bulk(c, in.Action, ids, in)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": msg}, nil
}

// POST /api/prospects/process {ids, steps, pw, pn, auto}
func (m *mod) prospectProcess(c *app.Ctx) (any, error) {
	var in bulkIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	ids, err := m.targets(c, in)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, app.Bad("Select at least one lead.")
	}
	for _, s := range in.Steps {
		if s != "audit" && s != "report" && s != "contact" {
			return nil, app.Bad("Unknown step: " + s)
		}
	}
	n, err := process(c, m.a, c.Tx(), c.Org.ID, c.S(), ids, ProcessOpts{Steps: in.Steps, PW: in.PW, PN: in.PN, Auto: in.Auto})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "queued": n, "message": strconv.Itoa(n) + " lead(s) queued for audit & report."}, nil
}

// POST /api/prospects/abort {ids}
func (m *mod) prospectAbort(c *app.Ctx) (any, error) {
	var in bulkIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	ids, err := m.targets(c, in)
	if err != nil {
		return nil, err
	}
	n, err := abort(c, c.Tx(), ids)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "stopped": n, "message": strconv.Itoa(n) + " process(es) stopped."}, nil
}

func (m *mod) prospectAudit(c *app.Ctx) (any, error) {
	var a json.RawMessage
	var ws, ms *int16
	if err := c.Tx().QueryRow(c, `SELECT audit, web_score, map_score FROM leads WHERE id=$1`, c.ParamID("id")).Scan(&a, &ws, &ms); err != nil {
		return nil, app.NotFound()
	}
	if len(a) == 0 {
		a = json.RawMessage("null")
	}
	return map[string]any{"audit": a, "web_score": ws, "map_score": ms}, nil
}

// POST /api/prospects/{id}/send {force, channels} – send the report now (skips gap, daily limit and duplicate checks when force).
func (m *mod) prospectSend(c *app.Ctx) (any, error) {
	var in struct {
		Force    bool     `json:"force"`
		Channels []string `json:"channels"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	id := c.ParamID("id")
	if _, err := c.Tx().Exec(c, `UPDATE leads SET abort_flag=false WHERE id=$1`, id); err != nil {
		return nil, err
	}
	p := map[string]any{"force": in.Force}
	if len(in.Channels) > 0 {
		p["channels"] = in.Channels
	}
	n, err := scheduleContact(c, c.Tx(), c.S(), []int64{id}, true, true, p)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, app.Bad("No report generated yet")
	}
	return map[string]any{"ok": true, "message": "Sending now…"}, nil
}

func (m *mod) prospectEnrich(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	var site string
	if err := c.Tx().QueryRow(c, `SELECT website FROM leads WHERE id=$1`, id).Scan(&site); err != nil {
		return nil, app.NotFound()
	}
	if site == "" {
		return nil, app.Bad("This business has no website.")
	}
	if _, err := jobs.Push(c, c.Tx(), c.Org.ID, "enrich", id, nil, jobs.Opt{MaxAttempts: 2}); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "message": "Scanning the website for phone & email…"}, nil
}

func (m *mod) reportPrompts(c *app.Ctx) (any, error) {
	type pr struct {
		ID       int64  `db:"id" json:"id"`
		Name     string `db:"name" json:"name"`
		IsActive bool   `db:"is_active" json:"is_active"`
	}
	pw, err := db.All[pr](c, c.Tx(), `SELECT id,name,is_active FROM prompts WHERE type='report_website' ORDER BY is_active DESC, name`)
	if err != nil {
		return nil, err
	}
	pn, err := db.All[pr](c, c.Tx(), `SELECT id,name,is_active FROM prompts WHERE type='report_noweb' ORDER BY is_active DESC, name`)
	if err != nil {
		return nil, err
	}
	return map[string]any{"website": pw, "noweb": pn, "auto_contact": c.S().Get("auto_contact", "1") == "1"}, nil
}

func (m *mod) testMaps(c *app.Ctx) (any, error) {
	var in struct {
		City string `json:"city"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	city := strings.TrimSpace(in.City)
	if city == "" {
		city = "Pune"
	}
	s := c.S()
	org := c.Org.ID
	// Run outside the request transaction: usage is recorded in short transactions of its own.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c), 70*time.Second)
	defer cancel()
	mc := &mapsClient{s: s, track: m.tracker(ctx, org)}
	return map[string]any{"lines": mc.diagnose(ctx, cut(city, 80))}, nil
}
