package prospecting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/config"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Integration test against a migrated database: SETU_TEST_DB=postgres://setu_app:…/setu_prospecting go test -run Flow
func TestFlowDB(t *testing.T) {
	url := os.Getenv("SETU_TEST_DB")
	if url == "" {
		t.Skip("set SETU_TEST_DB")
	}
	ctx := context.Background()
	d, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	a := app.New(&config.Config{MasterKey: make([]byte, 32), StorageDir: t.TempDir(), Scheme: "http", AppHost: "localhost"}, d)
	m := &mod{a: a}
	var org int64
	if err := d.Pool.QueryRow(ctx, `INSERT INTO orgs (slug,name) VALUES ($1,'Flow test') RETURNING id`, fmt.Sprintf("flow-%d", os.Getpid())).Scan(&org); err != nil {
		t.Fatal(err)
	}
	defer d.Pool.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, org)
	defer d.Pool.Exec(ctx, `DELETE FROM jobs WHERE org_id=$1`, org)

	// Mock Google Places: page 1 = p1…p20 (next tok2), page 2 = p21…p30
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mask := r.Header.Get("X-Goog-FieldMask")
		place := func(i int) map[string]any {
			p := map[string]any{"id": fmt.Sprintf("p%d", i), "displayName": map[string]any{"text": fmt.Sprintf("Dental %d", i)}, "formattedAddress": "Baner, Pune",
				"addressComponents": []any{map[string]any{"longText": "Pune", "types": []string{"locality"}}}, "rating": 4.36, "userRatingCount": i * 3,
				"primaryTypeDisplayName": map[string]any{"text": "Dentist"}, "types": []string{"dentist", "health"}, "location": map[string]any{"latitude": 18.56, "longitude": 73.78}}
			if i%2 == 0 {
				p["websiteUri"] = fmt.Sprintf("https://dental%d.example", i)
				p["internationalPhoneNumber"] = "+91 98765 000" + fmt.Sprint(10+i)
			}
			return p
		}
		if strings.HasPrefix(r.URL.Path, "/places/") {
			calls["details"]++
			var i int
			fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/places/p"), "%d", &i)
			json.NewEncoder(w).Encode(place(i))
			return
		}
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		from, to, next := 1, 20, "tok2"
		if b["pageToken"] == "tok2" {
			from, to, next = 21, 30, ""
		}
		var ps []any
		for i := from; i <= to; i++ {
			if mask == "places.id,nextPageToken" {
				ps = append(ps, map[string]any{"id": fmt.Sprintf("p%d", i)})
			} else {
				ps = append(ps, place(i))
			}
		}
		if mask == "places.id,nextPageToken" {
			calls["ids"]++
		} else {
			calls["full"]++
		}
		json.NewEncoder(w).Encode(map[string]any{"places": ps, "nextPageToken": next})
	}))
	defer srv.Close()
	ou, ob := placesSearchURL, placesBase
	placesSearchURL, placesBase = srv.URL+"/search", srv.URL+"/places/"
	defer func() { placesSearchURL, placesBase = ou, ob }()

	var s *settings.Store
	var cid, other int64
	must(t, a.OrgTx(ctx, org, func(tx pgx.Tx, st *settings.Store) error {
		s = st
		st.Set(ctx, tx, "google_api_key", "k")
		if err := tx.QueryRow(ctx, `INSERT INTO campaigns (keyword,location,limit_n) VALUES ('dentist','Baner',25) RETURNING id`).Scan(&cid); err != nil {
			return err
		}
		// p3 already exists from an earlier search → skipped
		return tx.QueryRow(ctx, `INSERT INTO leads (campaign_id,place_id,name) VALUES (999,'p3','Old') RETURNING id`).Scan(&other)
	}))
	camp := func() campaignRow {
		var c campaignRow
		a.OrgTx(ctx, org, func(tx pgx.Tx, _ *settings.Store) error {
			var err error
			c, err = db.One[campaignRow](ctx, tx, `SELECT id,keyword,location,limit_n,source,status,found,skipped FROM campaigns WHERE id=$1`, cid)
			return err
		})
		return c
	}
	must(t, m.searchPage(ctx, org, camp(), s, ""))
	c := camp()
	if c.Found != 19 || c.Skipped != 1 || c.Status != "searching" {
		t.Fatalf("page 1: %+v", c)
	}
	must(t, m.searchPage(ctx, org, c, s, "tok2"))
	c = camp()
	if c.Found != 25 || c.Status != "ready" {
		t.Fatalf("page 2: %+v", c)
	}
	if calls["ids"] != 2 || calls["full"] != 2 || calls["details"] != 0 {
		t.Fatalf("calls %v", calls)
	}
	type cnt struct {
		Leads, Events, Enrich, Search, Usage int
		Rating                              float64
	}
	var k cnt
	a.OrgTx(ctx, org, func(tx pgx.Tx, _ *settings.Store) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM leads WHERE campaign_id=$1),(SELECT count(*) FROM events WHERE type='found'),
 (SELECT count(*) FROM jobs WHERE type='enrich'),(SELECT count(*) FROM jobs WHERE type='search'),(SELECT COALESCE(sum(calls),0) FROM api_usage),
 (SELECT rating::float8 FROM leads WHERE place_id='p1')`, cid).Scan(&k.Leads, &k.Events, &k.Enrich, &k.Search, &k.Usage, &k.Rating)
	})
	if k.Leads != 25 || k.Events != 25 || k.Enrich != 13 || k.Search != 1 || k.Usage != 4 || k.Rating != 4.4 {
		t.Fatalf("counts %+v", k)
	}
	// Single new business on a page → 1 Place Details request instead of a whole page
	must(t, a.OrgTx(ctx, org, func(tx pgx.Tx, _ *settings.Store) error {
		_, err := tx.Exec(ctx, `UPDATE campaigns SET limit_n=26, status='searching' WHERE id=$1`, cid)
		return err
	}))
	must(t, m.searchPage(ctx, org, camp(), s, "tok2"))
	if c = camp(); c.Found != 26 || calls["details"] != 1 || calls["full"] != 2 {
		t.Fatalf("details path: %+v %v", c, calls)
	}

	// Pipeline: initiate (set-based), abort, schedule contact, delete
	var ids []int64
	a.OrgTx(ctx, org, func(tx pgx.Tx, _ *settings.Store) error {
		return tx.QueryRow(ctx, `SELECT array_agg(id ORDER BY position) FROM leads WHERE campaign_id=$1`, cid).Scan(&ids)
	})
	must(t, a.OrgTx(ctx, org, func(tx pgx.Tx, st *settings.Store) error {
		n, err := process(ctx, a, tx, org, st, ids, ProcessOpts{Steps: []string{"audit", "report"}, Auto: true})
		if err != nil || n != 26 {
			return fmt.Errorf("process %d %v", n, err)
		}
		var jobsN, queued int
		tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM jobs WHERE type='audit' AND status='pending' AND payload->>'auto'='true' AND (payload->>'bulk')::bool),(SELECT count(*) FROM leads WHERE stage='queued')`).Scan(&jobsN, &queued)
		if jobsN != 26 || queued != 26 {
			return fmt.Errorf("audit jobs %d queued %d", jobsN, queued)
		}
		// busy leads are not queued twice
		if n, _ := process(ctx, a, tx, org, st, ids, ProcessOpts{}); n != 0 {
			return fmt.Errorf("re-queued %d", n)
		}
		n, err = abort(ctx, tx, ids[:10])
		if err != nil || n != 10 {
			return fmt.Errorf("abort %d %v", n, err)
		}
		var cancelled, stopped int
		tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM jobs WHERE type='audit' AND status='cancelled'),(SELECT count(*) FROM events WHERE message='Process stopped manually')`).Scan(&cancelled, &stopped)
		if cancelled != 10 || stopped != 10 {
			return fmt.Errorf("cancelled %d stopped %d", cancelled, stopped)
		}
		// contact: only leads with a report; spaced by send_gap
		if _, err := tx.Exec(ctx, `INSERT INTO reports (lead_id,token) SELECT id, 'tok'||id FROM leads WHERE id = ANY($1)`, ids[:3]); err != nil {
			return err
		}
		n, err = scheduleContact(ctx, tx, st, ids[:5], true, false, nil)
		if err != nil || n != 3 {
			return fmt.Errorf("contact %d %v", n, err)
		}
		var span float64
		tx.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM max(run_after)-min(run_after)) FROM jobs WHERE type='contact'`).Scan(&span)
		if span != 180 {
			return fmt.Errorf("gap span %v", span)
		}
		n, _, err = deleteLeads(ctx, tx, ids[:2])
		if err != nil || n != 2 {
			return fmt.Errorf("delete %d %v", n, err)
		}
		return nil
	}))
	// List filters & where builder
	f := listQ{Q: "dental 1", Web: "1"}
	w, args := f.where()
	var n int
	a.OrgTx(ctx, org, func(tx pgx.Tx, _ *settings.Store) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM leads l WHERE `+w, args...).Scan(&n)
	})
	if n != 5 { // Dental 10,12,14,16,18 (+ website)
		t.Fatalf("filter count %d (%s)", n, w)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
