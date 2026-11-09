package automation

// Engine test against a real PostgreSQL database. Runs only when SETU_TEST_DATABASE_URL (app role)
// and SETU_TEST_MIGRATE_URL (owner role) are set, e.g.
//   SETU_TEST_DATABASE_URL=postgres://setu_app:app@localhost:5432/setu_automation?sslmode=disable
//   SETU_TEST_MIGRATE_URL=postgres://setu_owner:owner@localhost:5432/setu_automation?sslmode=disable

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/config"
	"github.com/narendersinghh/setu/server/internal/db"
)

func testApp(t *testing.T) (*app.App, int64) {
	url, mig := os.Getenv("SETU_TEST_DATABASE_URL"), os.Getenv("SETU_TEST_MIGRATE_URL")
	if url == "" || mig == "" {
		t.Skip("SETU_TEST_DATABASE_URL / SETU_TEST_MIGRATE_URL not set")
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, mig, url); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	cfg := &config.Config{AppHost: "localhost", Scheme: "http", MasterKey: make([]byte, 32), OrgJobCap: 3}
	a := app.New(cfg, d)
	Register(a)
	var org int64
	slug := fmt.Sprintf("autotest-%d", time.Now().UnixNano())
	if err := d.Pool.QueryRow(ctx, `INSERT INTO orgs (slug,name) VALUES ($1,'Auto test') RETURNING id`, slug).Scan(&org); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Pool.Exec(context.Background(), `DELETE FROM orgs WHERE id=$1`, org) })
	return a, org
}

func orgDo(t *testing.T, a *app.App, org int64, fn func(tx pgx.Tx) error) {
	t.Helper()
	if err := a.DB.Org(context.Background(), org, fn); err != nil {
		t.Fatal(err)
	}
}

type runState struct {
	Status string     `db:"status"`
	Step   int        `db:"step"`
	NextAt *time.Time `db:"next_at"`
	Log    []LogEntry `db:"log"`
}

func state(t *testing.T, a *app.App, org, rid int64) runState {
	t.Helper()
	var r runState
	orgDo(t, a, org, func(tx pgx.Tx) error {
		var err error
		r, err = db.One[runState](context.Background(), tx, `SELECT status,step,next_at,log FROM automation_runs WHERE id=$1`, rid)
		return err
	})
	return r
}

func TestEngine(t *testing.T) {
	a, org := testApp(t)
	ctx := context.Background()
	steps, _ := json.Marshal([]map[string]any{
		{"t": "followup", "n": 2, "u": "days"},
		{"t": "webhook", "url": "not a url"},
		{"t": "wait", "n": 1, "u": "minutes"},
		{"t": "notify", "subject": "x", "text": "y"},
		{"t": "tag", "v": "hello"}, // crm.AddTag (fails while crm is a stub → run failed)
	})
	var autoID, other, lid, rid int64
	orgDo(t, a, org, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO automations (name,trig,trig_value,conditions,steps,active) VALUES ('Welcome','lead_created','manual','[{"f":"city","op":"is","v":"Pune"}]',$1,true) RETURNING id`, steps).Scan(&autoID); err != nil {
			return err
		}
		// Source filter excludes this one; inactive one must not run either
		tx.QueryRow(ctx, `INSERT INTO automations (name,trig,trig_value,steps,active) VALUES ('FB only','lead_created','facebook',$1,true) RETURNING id`, steps).Scan(&other)
		tx.Exec(ctx, `INSERT INTO automations (name,trig,steps,active) VALUES ('Off','lead_created',$1,false)`, steps)
		if err := tx.QueryRow(ctx, `INSERT INTO leads (place_id,name,city,source,is_lead,phone) VALUES ('m1','Acme','pune','manual',true,'9999999999') RETURNING id`).Scan(&lid); err != nil {
			return err
		}
		a.Emit(ctx, tx, org, "lead_created", lid, map[string]any{"source": "manual"})
		return nil
	})
	orgDo(t, a, org, func(tx pgx.Tx) error {
		n, _ := db.Val[int](ctx, tx, `SELECT count(*) FROM automation_runs WHERE lead_id=$1`, lid)
		if n != 1 {
			t.Fatalf("runs = %d, want 1", n)
		}
		rid, _ = db.Val[int64](ctx, tx, `SELECT id FROM automation_runs WHERE lead_id=$1 AND automation_id=$2`, lid, autoID)
		j, _ := db.Val[int](ctx, tx, `SELECT count(*) FROM jobs WHERE type='auto_step' AND ref_id=$1 AND status='pending'`, rid)
		if rid == 0 || j != 1 {
			t.Fatalf("run %d, pending jobs %d", rid, j)
		}
		// lead_created runs only once per lead
		a.Emit(ctx, tx, org, "lead_created", lid, nil)
		n, _ = db.Val[int](ctx, tx, `SELECT count(*) FROM automation_runs WHERE lead_id=$1`, lid)
		if n != 1 {
			t.Fatalf("second emit started a run: %d", n)
		}
		return nil
	})

	// Steps 1-3 run, then the run waits
	if err := runJob(ctx, a, org, rid); err != nil {
		t.Fatal(err)
	}
	r := state(t, a, org, rid)
	if r.Status != "waiting" || r.Step != 3 || r.NextAt == nil || len(r.Log) != 3 {
		t.Fatalf("after wait: %+v", r)
	}
	t.Logf("log: %+v", r.Log)
	// Not due yet: a second job does nothing
	runJob(ctx, a, org, rid)
	if r2 := state(t, a, org, rid); r2.Step != 3 {
		t.Fatalf("ran before due: %+v", r2)
	}

	// Make it due → sweep (cross-workspace) → per-workspace job → step job
	orgDo(t, a, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE automation_runs SET next_at=now()-interval '1 second' WHERE id=$1`, rid)
		return err
	})
	if err := sweepDue(ctx, a); err != nil {
		t.Fatal(err)
	}
	n, _ := db.Val[int](ctx, a.DB.Pool, `SELECT count(*) FROM jobs WHERE org_id=$1 AND type='auto_due' AND status='pending'`, org)
	if n != 1 {
		t.Fatalf("auto_due jobs = %d", n)
	}
	sweepDue(ctx, a) // no duplicate
	if n, _ := db.Val[int](ctx, a.DB.Pool, `SELECT count(*) FROM jobs WHERE org_id=$1 AND type='auto_due' AND status='pending'`, org); n != 1 {
		t.Fatalf("duplicate auto_due jobs = %d", n)
	}
	// the original step job is done in a real worker; mark it so dueJob queues a fresh one
	a.DB.Pool.Exec(ctx, `UPDATE jobs SET status='done' WHERE org_id=$1`, org)
	if err := dueJob(ctx, a, org); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Val[int](ctx, a.DB.Pool, `SELECT count(*) FROM jobs WHERE org_id=$1 AND type='auto_step' AND ref_id=$2 AND status='pending'`, org, rid); n != 1 {
		t.Fatalf("auto_step after dueJob = %d", n)
	}
	if err := runJob(ctx, a, org, rid); err != nil {
		t.Fatal(err)
	}
	r = state(t, a, org, rid)
	t.Logf("final: %s log: %+v", r.Status, r.Log)
	// notify skips (no company email); tag fails while crm is a stub, otherwise the run completes
	if r.Status != "failed" && r.Status != "done" {
		t.Fatalf("final status %s", r.Status)
	}
	orgDo(t, a, org, func(tx pgx.Tx) error {
		ev, _ := db.Val[int](ctx, tx, `SELECT count(*) FROM events WHERE lead_id=$1 AND channel='auto'`, lid)
		if ev < 5 {
			t.Fatalf("journey events = %d", ev)
		}
		f, _ := db.Val[bool](ctx, tx, `SELECT follow_up_at > now()+interval '47 hours' FROM leads WHERE id=$1`, lid)
		if !f {
			t.Fatal("follow-up not set")
		}
		return nil
	})
}

func TestReplyAndScan(t *testing.T) {
	a, org := testApp(t)
	ctx := context.Background()
	var noReply, lid, rid int64
	orgDo(t, a, org, func(tx pgx.Tx) error {
		tx.QueryRow(ctx, `INSERT INTO automations (name,trig,trig_value,steps,active) VALUES ('Nudge','no_reply','2','[{"t":"wait","n":1,"u":"days"},{"t":"wait","n":1,"u":"days"}]',true) RETURNING id`).Scan(&noReply)
		tx.QueryRow(ctx, `INSERT INTO leads (place_id,name,is_lead) VALUES ('m2','Silent',true) RETURNING id`).Scan(&lid)
		_, err := tx.Exec(ctx, `INSERT INTO messages (lead_id,channel,direction,created_at) VALUES ($1,'email','out',now()-interval '3 days')`, lid)
		return err
	})
	if err := sweepScan(ctx, a); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Val[int](ctx, a.DB.Pool, `SELECT count(*) FROM jobs WHERE org_id=$1 AND type='auto_scan'`, org); n != 1 {
		t.Fatalf("auto_scan jobs = %d", n)
	}
	if err := scanJob(ctx, a, org); err != nil {
		t.Fatal(err)
	}
	orgDo(t, a, org, func(tx pgx.Tx) error {
		rid, _ = db.Val[int64](ctx, tx, `SELECT id FROM automation_runs WHERE lead_id=$1 AND automation_id=$2`, lid, noReply)
		return nil
	})
	if rid == 0 {
		t.Fatal("no_reply did not start")
	}
	scanJob(ctx, a, org) // only once
	runJob(ctx, a, org, rid)
	if s := state(t, a, org, rid); s.Status != "waiting" {
		t.Fatalf("status %s", s.Status)
	}
	// Reply → stopped
	orgDo(t, a, org, func(tx pgx.Tx) error {
		tx.Exec(ctx, `UPDATE leads SET replied_at=now() WHERE id=$1`, lid)
		a.Emit(ctx, tx, org, "replied", lid, map[string]any{"via": "email"})
		return nil
	})
	s := state(t, a, org, rid)
	if s.Status != "stopped" || !strings.Contains(s.Log[len(s.Log)-1].Msg, "replied") {
		t.Fatalf("after reply: %+v", s)
	}
	orgDo(t, a, org, func(tx pgx.Tx) error {
		n, _ := db.Val[int](ctx, tx, `SELECT count(*) FROM automation_runs WHERE automation_id=$1`, noReply)
		if n != 1 {
			t.Fatalf("no_reply runs = %d", n)
		}
		return nil
	})
}
