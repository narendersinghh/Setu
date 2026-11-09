// Package prospecting: Google Maps search, contact scan, website & Maps audits, outreach.
// OWNER: prospecting module. Exported functions below are contracts used by other modules – keep signatures.
package prospecting

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// EnqueueProcess starts the chosen steps ("audit", "report", "contact") for scraped businesses.
func EnqueueProcess(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, ids []int64, steps []string) (int, error) {
	s, err := settings.Load(ctx, tx, a.Box, orgID)
	if err != nil {
		return 0, err
	}
	return process(ctx, a, tx, orgID, s, ids, ProcessOpts{Steps: steps})
}

type mod struct{ a *app.App }

// Defaults of the Prospecting settings (v1 Seed).
var defaults = map[string]string{
	"maps_source": "places", "maps_skip_existing": "1", "maps_atmosphere": "0", "maps_language": "en", "maps_region": "in",
	"default_country_code": "91", "pagespeed": "0", "deep_crawl": "1",
	"yearly_fee": "72000", "website_fee": "25000",
	"services": "Local SEO & Google Business Profile optimisation\nGoogle Maps ranking (top 3 local pack)\nOn-page SEO & technical fixes\nReview generation system\nWebsite design & speed optimisation\nMonthly performance reports",
	"auto_contact": "1", "send_email": "1", "send_whatsapp": "1", "send_gap": "90", "daily_limit": "60", "skip_contacted": "1", "promote_on_reply": "0",
}

func Register(a *app.App) {
	m := &mod{a: a}
	o := []app.Opt{app.Perm("prospecting"), app.Module("prospecting")}

	// Map Scraper: searches (campaigns)
	a.Get("/campaigns", m.campaignList, o...)
	a.Post("/campaigns", m.campaignCreate, o...)
	a.Get("/campaigns/{id}", m.campaignGet, o...)
	a.Get("/campaigns/{id}/leads", m.campaignLeads, o...)
	a.Get("/campaigns/{id}/export", m.campaignExport, o...)
	a.Post("/campaigns/{id}/stop", m.campaignStop, o...)
	a.Post("/campaigns/{id}/more", m.campaignMore, o...)
	a.Delete("/campaigns/{id}", m.campaignDelete, o...)

	// Scraped Data
	a.Get("/prospects", m.prospectList, o...)
	a.Get("/prospects/summary", m.prospectSummary, o...)
	a.Get("/prospects/export", m.prospectExport, o...)
	a.Post("/prospects/bulk", m.prospectBulk, o...)
	a.Post("/prospects/process", m.prospectProcess, o...)
	a.Post("/prospects/abort", m.prospectAbort, o...)
	a.Get("/prospects/{id}/audit", m.prospectAudit, o...)
	a.Post("/prospects/{id}/send", m.prospectSend, o...)
	a.Post("/prospects/{id}/enrich", m.prospectEnrich, o...)
	a.Get("/prospecting/report-prompts", m.reportPrompts, o...)
	a.Post("/prospecting/test-maps", m.testMaps, o...)

	a.Jobs.Handle("search", 3*time.Minute, m.jobSearch)
	a.Jobs.Handle("enrich", 2*time.Minute, m.jobEnrich)
	a.Jobs.Handle("audit", 8*time.Minute, m.leadJob("Audit started", m.jobAudit))
	a.Jobs.Handle("contact", 5*time.Minute, m.leadJob("Sending the report", m.jobContact))

	// Move scraped businesses to Leads when they reply (Settings → Outreach sending)
	a.On("replied", func(ctx context.Context, tx pgx.Tx, orgID, leadID int64, data map[string]any) error {
		if _, err := tx.Exec(ctx, `UPDATE leads SET stage='replied' WHERE id=$1 AND source='maps' AND stage NOT IN ('replied','won','lost')`, leadID); err != nil {
			return err
		}
		s, err := settings.Load(ctx, tx, a.Box, orgID)
		if err != nil || s.Get("promote_on_reply", "0") != "1" {
			return err
		}
		var isLead bool
		if err := tx.QueryRow(ctx, `SELECT is_lead FROM leads WHERE id=$1`, leadID).Scan(&isLead); err != nil || isLead {
			return nil
		}
		return crm.Promote(ctx, a, tx, orgID, leadID, "replied")
	})

	a.Seed(func(ctx context.Context, tx pgx.Tx, orgID int64, s *settings.Store) error {
		for k, v := range defaults {
			if s.Get(k, "") == "" {
				if err := s.Set(ctx, tx, k, v); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
