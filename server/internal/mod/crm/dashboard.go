package crm

import (
	"strconv"
	"time"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
)

func registerDashboard(a *app.App) {
	a.Get("/nav", nav)
	a.Get("/dashboard", dashboard, app.Perm("dashboard"))
}

// apptScope: sales people see their own appointments and those of their / unassigned leads.
func apptScope(c *app.Ctx) string {
	if c.Role() != "sales" {
		return ""
	}
	id := strconv.FormatInt(c.User.ID, 10)
	return " AND (a.user_id=" + id + " OR l.assigned_to=" + id + " OR l.assigned_to IS NULL)"
}

// nav: sidebar counters {leads_new, scraped, mail_unread, appt_today}.
func nav(c *app.Ctx) (any, error) {
	pro := c.Mod("prospecting") && c.Can("prospecting")
	sc := c.Scope("l")
	tx := c.Tx()
	day := dayStart(c)
	scraped := "0"
	if pro {
		scraped = `(SELECT count(*) FROM leads WHERE source='maps' AND NOT is_lead)`
	}
	mailLead := ""
	if !pro {
		mailLead = " AND l.is_lead"
	}
	return db.One[navCounts](c, tx, `SELECT
 (SELECT count(*) FROM leads l WHERE l.is_lead AND l.status='new'`+sc+`) leads_new,
 `+scraped+` scraped,
 (SELECT count(DISTINCT m.lead_id) FROM messages m JOIN leads l ON l.id=m.lead_id WHERE m.channel='email' AND m.direction='in' AND NOT m.is_read`+mailLead+sc+`) mail_unread,
 (SELECT count(*) FROM appointments a LEFT JOIN leads l ON l.id=a.lead_id WHERE a.status='scheduled' AND a.start_at>=$1 AND a.start_at<$2`+apptScope(c)+`) appt_today`,
		day, day.AddDate(0, 0, 1))
}

type navCounts struct {
	LeadsNew   int64 `db:"leads_new" json:"leads_new"`
	Scraped    int64 `db:"scraped" json:"scraped"`
	MailUnread int64 `db:"mail_unread" json:"mail_unread"`
	ApptToday  int64 `db:"appt_today" json:"appt_today"`
}

func dashboard(c *app.Ctx) (any, error) {
	tx := c.Tx()
	s := c.S()
	pro := c.Mod("prospecting") && c.Can("prospecting")
	sc := c.Scope("l")
	day := dayStart(c)
	k, err := db.Map(c, tx, `SELECT count(*) total,
 count(*) FILTER (WHERE l.created_at>=$1) today,
 count(*) FILTER (WHERE l.created_at>=now()-interval '7 days') week,
 count(*) FILTER (WHERE l.status='new') new,
 count(*) FILTER (WHERE l.status='won') won,
 COALESCE(sum(l.deal_value) FILTER (WHERE l.status='won'),0)::float8 won_value,
 COALESCE(sum(l.deal_value) FILTER (WHERE l.status NOT IN ('won','lost')),0)::float8 pipeline,
 count(*) FILTER (WHERE `+followDue+`) due
FROM leads l WHERE l.is_lead`+sc, day)
	if err != nil {
		return nil, err
	}
	type kn struct {
		K string `db:"k" json:"k"`
		N int64  `db:"n" json:"n"`
	}
	st, err := db.All[kn](c, tx, `SELECT l.status k, count(*) n FROM leads l WHERE l.is_lead`+sc+` GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	srcs, err := db.All[kn](c, tx, `SELECT l.source k, count(*) n FROM leads l WHERE l.is_lead`+sc+` GROUP BY 1 ORDER BY 2 DESC`)
	if err != nil {
		return nil, err
	}
	as := apptScope(c)
	appts, err := db.Maps(c, tx, `SELECT a.id,a.lead_id,a.title,a.start_at,a.meet_link,COALESCE(l.name,'') name,COALESCE(l.contact_name,'') contact_name
FROM appointments a LEFT JOIN leads l ON l.id=a.lead_id WHERE a.status='scheduled' AND a.start_at>=now()-interval '1 hour'`+as+` ORDER BY a.start_at LIMIT 5`)
	if err != nil {
		return nil, err
	}
	up, err := db.Val[int64](c, tx, `SELECT count(*) FROM appointments a LEFT JOIN leads l ON l.id=a.lead_id WHERE a.status='scheduled' AND a.start_at>=now()`+as)
	if err != nil {
		return nil, err
	}
	fu, err := db.Maps(c, tx, `SELECT l.id,l.name,l.contact_name,l.follow_up_at,l.phone FROM leads l WHERE l.is_lead AND l.follow_up_at IS NOT NULL AND l.follow_up_at<=now()+interval '1 day' AND l.status NOT IN ('won','lost')`+sc+` ORDER BY l.follow_up_at LIMIT 4`)
	if err != nil {
		return nil, err
	}
	evw := ""
	if !pro {
		evw = " AND l.is_lead"
	}
	ev, err := db.Maps(c, tx, `SELECT e.id,e.lead_id,e.type,e.message,e.created_at,l.name,l.contact_name FROM events e JOIN leads l ON l.id=e.lead_id WHERE true`+evw+sc+` ORDER BY e.id DESC LIMIT 6`)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"kpi": k, "status": st, "sources": srcs, "appts": appts, "upcoming": up, "follow_ups": fu, "events": ev, "pro": pro}
	if pro {
		p, err := db.Map(c, tx, `SELECT count(*) FILTER (WHERE source='maps') t,
 count(*) FILTER (WHERE source='maps' AND map_score IS NOT NULL) au,
 count(*) FILTER (WHERE source='maps' AND stage IN ('report_ready','contacting','contacted','opened','replied','won','lost')) rr,
 count(*) FILTER (WHERE source='maps' AND (email_status IN ('sent','opened') OR wa_status IN ('sent','delivered','read','manual'))) ct,
 count(*) FILTER (WHERE source='maps' AND (email_status='opened' OR wa_status='read' OR stage='opened')) op,
 count(*) FILTER (WHERE source='maps' AND replied_at IS NOT NULL) rp,
 count(*) FILTER (WHERE source='maps' AND is_lead) il,
 count(*) FILTER (WHERE stage IN ('queued','auditing','reporting','contacting')) wk
FROM leads`)
		if err != nil {
			return nil, err
		}
		out["prospecting"] = p
	}
	if c.Can("settings") {
		set := func(k string) bool { return s.Get(k, "") != "" }
		has := func(sql string) bool { b, _ := db.Val[bool](c, tx, sql); return b }
		multi, _ := db.Val[bool](c, c.A.DB.Pool, `SELECT count(*)>1 FROM memberships WHERE org_id=$1`, c.Org.ID)
		type check struct {
			OK    bool   `json:"ok"`
			Label string `json:"label"`
			To    string `json:"to"`
		}
		checks := []check{
			{set("mail_api_key") || set("smtp_host") || s.Get("mail_transport", "") == "php", "Check email sending", "/settings/email"},
			{s.Get("wa_provider", "manual") != "manual", "Connect WhatsApp API (optional)", "/settings/whatsapp"},
			{has(`SELECT EXISTS(SELECT 1 FROM sources)`), "Capture leads from forms & ads", "/settings/capture"},
			{has(`SELECT EXISTS(SELECT 1 FROM automations WHERE active)`), "Switch on an automation", "/automations"},
			{set("gcal_refresh_token"), "Sync Google Calendar (optional)", "/settings/appointments"},
			{multi, "Invite your team", "/team"},
		}
		if pro {
			maps := set("google_api_key")
			if s.Get("maps_source", "places") == "serpapi" {
				maps = set("serpapi_key")
			}
			checks = append([]check{{set("ai_key_" + s.Get("ai_provider", "anthropic")), "Connect an AI provider", "/settings/ai"}, {maps, "Add a Google Maps API key", "/settings/data"}}, checks...)
		}
		out["checks"] = checks
	}
	out["now"] = time.Now()
	return out, nil
}
