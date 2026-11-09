-- Automation engine (module automation, migrations 0120–0129).
-- Runs that wait (status 'waiting') or are due again (status 'running') are found by next_at.
CREATE INDEX IF NOT EXISTS automation_runs_next ON automation_runs (next_at) WHERE status IN ('running', 'waiting');
CREATE INDEX IF NOT EXISTS automation_runs_auto ON automation_runs (org_id, automation_id, id DESC);
CREATE INDEX IF NOT EXISTS automation_runs_status ON automation_runs (org_id, status, created_at);
-- Workspaces with time-based triggers (no_reply, follow_up_due) are found by the periodic scan.
CREATE INDEX IF NOT EXISTS automations_timed ON automations (trig, org_id) WHERE active AND trig IN ('no_reply', 'follow_up_due');

-- The periodic sweeps run without a workspace (app_org() IS NULL) and only need to know WHICH
-- workspaces have due work; they then queue one job per workspace that runs inside its own tenant
-- transaction. Read-only, and only outside a tenant transaction (inside one the tenant policy applies).
CREATE POLICY sweep ON automation_runs FOR SELECT USING (app_org() IS NULL);
CREATE POLICY sweep ON automations FOR SELECT USING (app_org() IS NULL);
