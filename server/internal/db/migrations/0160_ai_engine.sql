-- ai-engine: workers (running without an organisation) find the workspaces with waiting bulk AI requests,
-- like they do for jobs. Inside a tenant transaction only that organisation's rows stay visible.
CREATE POLICY worker_scan ON ai_batch FOR SELECT USING (app_org() IS NULL);

CREATE INDEX IF NOT EXISTS ai_batch_wait ON ai_batch (status, mode, org_id) WHERE status IN ('queued','sent','running');
CREATE INDEX IF NOT EXISTS ai_batch_org_status ON ai_batch (org_id, mode, status, id);
CREATE INDEX IF NOT EXISTS job_steps_job ON job_steps (org_id, job_id);
