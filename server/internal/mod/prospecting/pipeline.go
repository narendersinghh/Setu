package prospecting

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// ProcessOpts: what Initiate Process runs for the selected businesses.
type ProcessOpts struct {
	Steps []string // audit, report, contact (default audit + report)
	PW    int64    // report prompt for businesses with a website (0 = active)
	PN    int64    // report prompt for businesses without a website (0 = active)
	Auto  bool     // send by email & WhatsApp when the report is ready
}

func hasStep(steps []string, s string) bool {
	for _, x := range steps {
		if x == s {
			return true
		}
	}
	return false
}

// process starts audit → report → (optional) contact for the given leads (set-based).
func process(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, s *settings.Store, ids []int64, o ProcessOpts) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if len(o.Steps) == 0 {
		o.Steps = []string{"audit", "report"}
	}
	if hasStep(o.Steps, "contact") {
		o.Auto = true
	}
	if o.Auto && !hasStep(o.Steps, "contact") {
		o.Steps = append(o.Steps, "contact")
	}
	// Several reports at once → their AI writing goes out as a bulk run (parallel engine / batch)
	bulk := len(ids) >= max(2, s.Int("batch_min", 4))
	switch {
	case hasStep(o.Steps, "audit"):
		msg := "Process initiated"
		if o.Auto {
			msg += " (auto-contact on)"
		}
		payload := auditPayload{Steps: o.Steps, PW: o.PW, PN: o.PN, Auto: o.Auto, Bulk: bulk}
		var n int
		err := tx.QueryRow(ctx, `WITH sel AS (
  SELECT id FROM leads WHERE id = ANY($1) AND stage <> ALL($2)
), del AS (
  DELETE FROM jobs WHERE org_id=app_org() AND type IN ('audit','report','contact') AND status='pending' AND ref_id IN (SELECT id FROM sel)
), upd AS (
  UPDATE leads SET abort_flag=false, progress=1, progress_msg='Waiting in queue', last_error='', stage='queued', last_activity=now()
  WHERE id IN (SELECT id FROM sel) RETURNING id
), ev AS (
  INSERT INTO events (lead_id,type,message,meta) SELECT id,'stage',$3,'{"stage":"queued"}'::jsonb FROM upd
), jb AS (
  INSERT INTO jobs (org_id,type,ref_id,payload,max_attempts) SELECT app_org(),'audit',id,$4::jsonb,2 FROM upd
)
SELECT count(*) FROM upd`, ids, busyStages, msg, payload).Scan(&n)
		return n, err
	case hasStep(o.Steps, "report"):
		type lr struct {
			ID      int64  `db:"id"`
			Website string `db:"website"`
		}
		rows, err := db.All[lr](ctx, tx, `UPDATE leads SET abort_flag=false, last_error='' WHERE id = ANY($1) AND stage <> ALL($2) RETURNING id, website`, ids, busyStages)
		if err != nil {
			return 0, err
		}
		for _, l := range rows {
			p := o.PN
			if l.Website != "" {
				p = o.PW
			}
			if err := enqueueReport(ctx, tx, orgID, l.ID, p, o.Auto, bulk); err != nil {
				return 0, err
			}
		}
		return len(rows), nil
	case hasStep(o.Steps, "contact"):
		return scheduleContact(ctx, tx, s, ids, true, false, nil)
	}
	return 0, nil
}

// abort stops everything for these leads: pending jobs are cancelled, a running job stops at its next step.
func abort(ctx context.Context, tx pgx.Tx, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_batch SET status='cancelled' WHERE status IN ('queued','sent') AND lead_id = ANY($1)`, ids); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE jobs SET status='cancelled', error='Stopped by user', finished_at=now() WHERE org_id=app_org() AND status IN ('pending','running') AND type = ANY($2) AND ref_id = ANY($1)`, ids, leadJobTypes); err != nil {
		return 0, err
	}
	var n int
	err := tx.QueryRow(ctx, `WITH upd AS (
  UPDATE leads l SET abort_flag=true, progress=0, progress_msg='Stopped', last_activity=now(),
    stage=CASE WHEN EXISTS (SELECT 1 FROM reports r WHERE r.lead_id=l.id) THEN 'report_ready' ELSE 'new' END
  WHERE l.id = ANY($1) AND l.stage = ANY($2) RETURNING l.id
), ev AS (
  INSERT INTO events (lead_id,type,message) SELECT id,'error','Process stopped manually' FROM upd
)
SELECT count(*) FROM upd`, ids, busyStages).Scan(&n)
	return n, err
}

// deleteLeads removes records with their history (set-based); returns stored report PDFs to delete after commit.
func deleteLeads(ctx context.Context, tx pgx.Tx, ids []int64) (int, []string, error) {
	if len(ids) == 0 {
		return 0, nil, nil
	}
	var pdfs []string
	rows, err := tx.Query(ctx, `SELECT pdf FROM reports WHERE lead_id = ANY($1) AND pdf<>''`, ids)
	if err != nil {
		return 0, nil, err
	}
	for rows.Next() {
		var p string
		rows.Scan(&p)
		pdfs = append(pdfs, p)
	}
	rows.Close()
	for _, t := range []string{"reports", "events", "messages", "appointments", "calls", "automation_runs", "job_steps", "ai_batch"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+t+` WHERE lead_id = ANY($1)`, ids); err != nil {
			return 0, nil, fmt.Errorf("%s: %w", t, err)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM jobs WHERE org_id=app_org() AND type = ANY($2) AND ref_id = ANY($1)`, ids, leadJobTypes); err != nil {
		return 0, nil, err
	}
	n, err := db.Exec(ctx, tx, `DELETE FROM leads WHERE id = ANY($1)`, ids)
	return int(n), pdfs, err
}

func (m *mod) removeFiles(paths []string) {
	for _, p := range paths {
		if f := m.filePath(p); f != "" {
			os.Remove(f)
		}
	}
}

// bulk runs a Scraped Data bulk action and returns the message shown to the user.
func (m *mod) bulk(c *app.Ctx, action string, ids []int64, in bulkIn) (string, error) {
	if len(ids) == 0 {
		return "", app.Bad("Select at least one lead.")
	}
	ctx, tx, org := c, c.Tx(), c.Org.ID
	switch action {
	case "initiate":
		steps := []string{"audit", "report"}
		if in.Auto {
			steps = append(steps, "contact")
		}
		n, err := process(ctx, m.a, tx, org, c.S(), ids, ProcessOpts{Steps: steps, PW: in.PW, PN: in.PN, Auto: in.Auto})
		return strconv.Itoa(n) + " lead(s) queued for audit & report.", err
	case "contact":
		n, err := scheduleContact(ctx, tx, c.S(), ids, true, false, nil)
		msg := strconv.Itoa(n) + " lead(s) scheduled for outreach"
		if n < len(ids) {
			msg += " (leads without a report were skipped)"
		}
		return msg + ".", err
	case "won", "lost":
		label := "Not interested"
		if action == "won" {
			label = "Won"
		}
		if _, err := tx.Exec(ctx, `WITH upd AS (UPDATE leads SET stage=$2, last_activity=now() WHERE id = ANY($1) RETURNING id)
INSERT INTO events (lead_id,type,message,meta) SELECT id,'stage',$3,jsonb_build_object('stage',$2::text) FROM upd`, ids, action, "Marked as "+label); err != nil {
			return "", err
		}
		if action == "lost" {
			if _, err := tx.Exec(ctx, `UPDATE leads SET status='lost' WHERE id = ANY($1) AND is_lead`, ids); err != nil {
				return "", err
			}
		} else {
			type lr struct {
				ID     int64  `db:"id"`
				IsLead bool   `db:"is_lead"`
				Status string `db:"status"`
			}
			rows, err := db.All[lr](ctx, tx, `SELECT id,is_lead,status FROM leads WHERE id = ANY($1) AND NOT (is_lead AND status='won')`, ids)
			if err != nil {
				return "", err
			}
			for _, l := range rows {
				if l.IsLead {
					err = crm.SetStatus(ctx, m.a, tx, org, l.ID, "won", "")
				} else {
					err = crm.Promote(ctx, m.a, tx, org, l.ID, "won")
				}
				if err != nil {
					return "", err
				}
			}
		}
		return strconv.Itoa(len(ids)) + " record(s) updated.", nil
	case "promote":
		rows, err := db.All[struct {
			ID int64 `db:"id"`
		}](ctx, tx, `SELECT id FROM leads WHERE id = ANY($1) AND NOT is_lead`, ids)
		if err != nil {
			return "", err
		}
		for _, l := range rows {
			if err := crm.Promote(ctx, m.a, tx, org, l.ID, "manual"); err != nil {
				return "", err
			}
		}
		return strconv.Itoa(len(ids)) + " record(s) moved to Leads.", nil
	case "abort":
		n, err := abort(ctx, tx, ids)
		return strconv.Itoa(n) + " process(es) stopped.", err
	case "delete":
		n, pdfs, err := deleteLeads(ctx, tx, ids)
		if err != nil {
			return "", err
		}
		c.After(func() { m.removeFiles(pdfs) })
		return strconv.Itoa(n) + " lead(s) deleted.", nil
	}
	return "", app.Bad("Unknown action")
}
