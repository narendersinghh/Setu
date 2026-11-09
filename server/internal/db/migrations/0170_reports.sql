-- reports module: indexes for the Audit Reports list, KPIs and per-prompt illustrations.
CREATE INDEX IF NOT EXISTS reports_created ON reports (org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS assets_prompt ON assets (org_id, prompt_id, placement);
CREATE INDEX IF NOT EXISTS research_cache_created ON research_cache (created_at);
