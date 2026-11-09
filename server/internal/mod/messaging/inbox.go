package messaging

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/settings"
)

func registerInbox(a *app.App) {
	// Every 2 minutes: one inbox check per workspace with IMAP switched on
	a.Jobs.Every("imap_poll", 2*time.Minute, func(ctx context.Context) error {
		orgs, err := db.All[struct {
			OrgID int64 `db:"org_id"`
		}](ctx, a.DB.Pool, `SELECT s.org_id FROM settings s JOIN orgs o ON o.id=s.org_id AND o.status='active'
WHERE s.k='imap_on' AND s.v='1'
  AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.org_id=s.org_id AND j.type='imap_sync' AND j.status IN ('pending','running'))`)
		if err != nil {
			return err
		}
		for _, o := range orgs {
			if _, err := jobs.Push(ctx, a.DB.Pool, o.OrgID, "imap_sync", 0, nil, jobs.Opt{MaxAttempts: 1}); err != nil {
				return err
			}
		}
		return nil
	})
	a.Jobs.Handle("imap_sync", 3*time.Minute, func(ctx context.Context, j *jobs.Job) error {
		if _, err := syncInbox(ctx, a, j.OrgID); err != nil {
			return jobs.Permanent(err)
		}
		return nil
	})
}

// syncInbox pulls new emails from the inbox; replies from leads land in their conversation.
// Returns how many were stored.
func syncInbox(ctx context.Context, a *app.App, orgID int64) (int, error) {
	var s *settings.Store
	if err := a.OrgTx(ctx, orgID, func(_ pgx.Tx, st *settings.Store) error { s = st; return nil }); err != nil {
		return 0, err
	}
	if !imapOn(s) {
		return 0, nil
	}
	msgs, last, uv, ferr := fetchNew(ctx, s, 40)
	n := 0
	err := a.OrgTx(context.WithoutCancel(ctx), orgID, func(tx pgx.Tx, s *settings.Store) error {
		s.Set(ctx, tx, "imap_last", strconv.FormatInt(time.Now().Unix(), 10))
		if ferr != nil {
			return s.Set(ctx, tx, "imap_error", cut(ferr.Error(), 300))
		}
		s.Set(ctx, tx, "imap_error", "")
		for _, m := range msgs {
			var lid int64
			savepoint(ctx, tx, "inmail", func() (err error) { lid, err = storeInbound(ctx, a, tx, orgID, s, m); return })
			if lid > 0 {
				n++
			}
		}
		if len(msgs) > 0 || s.Get("imap_uidv", "") != uv {
			s.Set(ctx, tx, "imap_uid", strconv.FormatUint(uint64(last), 10))
			s.Set(ctx, tx, "imap_uidv", uv)
		}
		return nil
	})
	if ferr != nil {
		return 0, ferr
	}
	return n, err
}

// storeInbound saves one received email in the matching lead's conversation; returns the lead id
// (0 = not from a lead, ignored).
func storeInbound(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, s *settings.Store, m inMail) (int64, error) {
	mine := strings.ToLower(mailFrom(s))
	if m.From == "" || m.From == mine || (m.Auto && m.InReplyTo == "") {
		return 0, nil
	}
	if m.MsgID != "" {
		if n, _ := db.Val[int64](ctx, tx, `SELECT count(*) FROM messages WHERE channel='email' AND direction='in' AND msg_id=$1`, m.MsgID); n > 0 {
			return 0, nil
		}
	}
	// Which lead: the conversation it answers, else the sender's address
	var lid int64
	if ids := strings.Fields(m.InReplyTo + " " + m.References); len(ids) > 0 {
		lid, _ = db.Val[int64](ctx, tx, `SELECT lead_id FROM messages WHERE channel='email' AND msg_id = ANY($1) ORDER BY id DESC LIMIT 1`, ids)
	}
	if lid == 0 {
		lid, _ = db.Val[int64](ctx, tx, `SELECT id FROM leads WHERE lower(email)=$1 ORDER BY is_lead DESC, last_activity DESC NULLS LAST LIMIT 1`, m.From)
	}
	if lid == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO messages (lead_id,channel,direction,status,msg_id,in_reply_to,addr,subject,body,is_read,created_at)
VALUES ($1,'email','in','received',$2,$3,$4,$5,$6,false,$7)`, lid, cut(m.MsgID, 250), cut(m.InReplyTo, 250), cut(m.From, 190), cut(m.Subject, 250), m.Text, m.Date); err != nil {
		return 0, err
	}
	var was *time.Time
	tx.QueryRow(ctx, `SELECT replied_at FROM leads WHERE id=$1`, lid).Scan(&was)
	if err := MarkReplied(ctx, a, tx, orgID, lid, "email", m.Text); err != nil {
		return 0, err
	}
	if was != nil {
		if err := app.Event(ctx, tx, lid, "replied", "New email: "+ellipsis(m.Subject, 120), "email", nil); err != nil {
			return 0, err
		}
	}
	return lid, nil
}

// ---------------------------------------------------------------- Emails page API

type conv struct {
	ID          int64     `db:"id" json:"id"`
	Name        string    `db:"name" json:"name"`
	ContactName string    `db:"contact_name" json:"contact_name"`
	Email       string    `db:"email" json:"email"`
	Mid         int64     `db:"mid" json:"-"`
	At          time.Time `db:"at" json:"at"`
	Un          int64     `db:"un" json:"un"`
	Ins         int64     `db:"ins" json:"ins"`
	N           int64     `db:"n" json:"n"`
	LastDir     string    `db:"-" json:"last_dir"`
	LastSubject string    `db:"-" json:"last_subject"`
	LastBody    string    `db:"-" json:"last_body"`
}

// visibility: scraped businesses only with prospecting access; sales people see own + unassigned.
func visibility(c *app.Ctx, s *settings.Store) string {
	v := ""
	if !(c.Can("prospecting") && c.Mod("prospecting")) {
		v = " AND l.is_lead"
	}
	return v + c.Scope("l")
}

func likeArg(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

var spaces = regexp.MustCompile(`\s+`)

// GET /api/emails?f=&q= – conversations per lead.
func emailsList(c *app.Ctx) (any, error) {
	s := c.S()
	tx := c.Tx()
	vis := visibility(c, s)
	f, q := c.Query("f"), c.Query("q")
	w := "m.channel='email'" + vis
	var args []any
	if q != "" {
		args = append(args, likeArg(q))
		w += ` AND (l.name ILIKE $1 OR l.contact_name ILIKE $1 OR l.email ILIKE $1 OR m.subject ILIKE $1)`
	}
	hav := map[string]string{"unread": "HAVING count(*) FILTER (WHERE m.direction='in' AND NOT m.is_read)>0", "replied": "HAVING count(*) FILTER (WHERE m.direction='in')>0", "waiting": "HAVING count(*) FILTER (WHERE m.direction='in')=0"}[f]
	rows, err := db.All[conv](c, tx, `SELECT l.id,l.name,l.contact_name,l.email,MAX(m.id) mid,MAX(m.created_at) at,
count(*) FILTER (WHERE m.direction='in' AND NOT m.is_read) un, count(*) FILTER (WHERE m.direction='in') ins, count(*) n
FROM messages m JOIN leads l ON l.id=m.lead_id WHERE `+w+` GROUP BY l.id `+hav+` ORDER BY at DESC LIMIT 150`, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		ids := make([]int64, len(rows))
		for i, r := range rows {
			ids[i] = r.Mid
		}
		type last struct {
			ID        int64  `db:"id"`
			Direction string `db:"direction"`
			Subject   string `db:"subject"`
			Body      string `db:"body"`
		}
		ls, err := db.All[last](c, tx, `SELECT id,direction,subject,left(body,300) body FROM messages WHERE id = ANY($1)`, ids)
		if err != nil {
			return nil, err
		}
		by := map[int64]last{}
		for _, l := range ls {
			by[l.ID] = l
		}
		for i := range rows {
			l := by[rows[i].Mid]
			rows[i].LastDir, rows[i].LastSubject, rows[i].LastBody = l.Direction, ellipsis(l.Subject, 60), ellipsis(spaces.ReplaceAllString(l.Body, " "), 80)
		}
	}
	var t, un int64
	tx.QueryRow(c, `SELECT count(DISTINCT m.lead_id), count(DISTINCT m.lead_id) FILTER (WHERE m.direction='in' AND NOT m.is_read)
FROM messages m JOIN leads l ON l.id=m.lead_id WHERE m.channel='email'`+vis).Scan(&t, &un)
	return map[string]any{"conversations": rows, "total": t, "unread": un, "ready": MailReady(s), "imap": imapOn(s),
		"imap_last": s.Int("imap_last", 0), "imap_error": s.Get("imap_error", ""), "can_settings": c.Can("settings")}, nil
}

type threadMsg struct {
	ID        int64     `db:"id" json:"id"`
	Direction string    `db:"direction" json:"direction"`
	Status    string    `db:"status" json:"status"`
	Subject   string    `db:"subject" json:"subject"`
	Body      string    `db:"body" json:"body"`
	Addr      string    `db:"addr" json:"addr"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// GET /api/emails/{id} – the conversation with one lead (marks replies as read).
func emailsThread(c *app.Ctx) (any, error) {
	s := c.S()
	tx := c.Tx()
	id := c.ParamID("id")
	type leadRow struct {
		ID          int64  `db:"id" json:"id"`
		Name        string `db:"name" json:"name"`
		ContactName string `db:"contact_name" json:"contact_name"`
		Email       string `db:"email" json:"email"`
		EmailStatus string `db:"email_status" json:"email_status"`
		IsLead      bool   `db:"is_lead" json:"is_lead"`
	}
	l, err := db.One[leadRow](c, tx, `SELECT l.id,l.name,l.contact_name,l.email,l.email_status,l.is_lead FROM leads l WHERE l.id=$1`+visibility(c, s), id)
	if err != nil {
		return nil, app.NotFound()
	}
	th, err := db.All[threadMsg](c, tx, `SELECT id,direction,status,subject,body,addr,created_at FROM messages WHERE lead_id=$1 AND channel='email' ORDER BY created_at,id`, id)
	if err != nil {
		return nil, err
	}
	tx.Exec(c, `UPDATE messages SET is_read=true WHERE lead_id=$1 AND channel='email' AND direction='in' AND NOT is_read`, id)
	var rep *threadMsg
	for i := range th {
		if th[i].Direction == "in" || rep == nil {
			rep = &th[i]
		}
	}
	sub, repID := "", int64(0)
	if rep != nil {
		sub = "Re: " + regexp.MustCompile(`(?i)^((re|fwd?):\s*)+`).ReplaceAllString(rep.Subject, "")
		repID = rep.ID
	}
	hasRep := false
	if reportMode(s) != "link" {
		n, _ := db.Val[int64](c, tx, `SELECT count(*) FROM reports WHERE lead_id=$1`, id)
		hasRep = n > 0
	}
	return map[string]any{"lead": l, "thread": th, "reply_id": repID, "subject": sub, "has_report": hasRep, "ready": MailReady(s), "imap": imapOn(s)}, nil
}

// GET /api/emails/leads?q= – leads with an email address for "New email".
func emailsLeads(c *app.Ctx) (any, error) {
	s := c.S()
	q := c.Query("q")
	w, args := "", []any{}
	if q != "" {
		args = append(args, likeArg(q))
		w = ` AND (l.name ILIKE $1 OR l.contact_name ILIKE $1 OR l.email ILIKE $1)`
	}
	return db.Maps(c, c.Tx(), `SELECT l.id,l.name,l.contact_name,l.email FROM leads l WHERE l.email<>''`+visibility(c, s)+w+` ORDER BY COALESCE(l.last_activity,l.created_at) DESC LIMIT 400`, args...)
}

// POST /api/emails/sync – check the inbox now (at most every 45 seconds).
func emailsSync(c *app.Ctx) (any, error) {
	var s *settings.Store
	if err := c.A.OrgTx(c, c.Org.ID, func(_ pgx.Tx, st *settings.Store) error { s = st; return nil }); err != nil {
		return nil, err
	}
	if !imapOn(s) {
		return map[string]any{"n": 0}, nil
	}
	if time.Now().Unix()-int64(s.Int("imap_last", 0)) <= 45 {
		return map[string]any{"n": 0}, nil
	}
	n, err := syncInbox(c, c.A, c.Org.ID)
	if err != nil {
		return map[string]any{"n": 0, "error": err.Error()}, nil
	}
	return map[string]any{"n": n}, nil
}
