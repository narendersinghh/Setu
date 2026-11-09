-- Prospecting: indexes for campaign tables, the Scraped Data list and outreach limits, plus the
-- per-workspace send slot used to keep "Seconds between two sends" across all workers.

CREATE INDEX IF NOT EXISTS leads_campaign_pos ON leads (org_id, campaign_id, position);
CREATE INDEX IF NOT EXISTS leads_scraped_recent ON leads (org_id, (COALESCE(last_activity, created_at)) DESC, id DESC) WHERE source = 'maps';
CREATE INDEX IF NOT EXISTS messages_out_day ON messages (org_id, created_at) WHERE direction = 'out';
CREATE INDEX IF NOT EXISTS campaigns_kw ON campaigns (org_id, keyword, location);

CREATE TABLE IF NOT EXISTS outreach_state (
  org_id bigint PRIMARY KEY DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  last_send_at timestamptz NOT NULL DEFAULT '-infinity'
);
ALTER TABLE outreach_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE outreach_state FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON outreach_state USING (org_id = app_org()) WITH CHECK (org_id = app_org());
