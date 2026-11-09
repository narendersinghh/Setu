-- CRM: indexes for the leads list, board, filters, search and dashboard on large workspaces.

-- Default list order (newest lead first) and the pipeline board (per status)
CREATE INDEX IF NOT EXISTS leads_crm_recent ON leads (org_id, (COALESCE(lead_at, created_at)) DESC, id DESC) WHERE is_lead;
CREATE INDEX IF NOT EXISTS leads_crm_board ON leads (org_id, status, (COALESCE(lead_at, created_at)) DESC, id DESC) WHERE is_lead;
-- Sorting by last activity and by deal value
CREATE INDEX IF NOT EXISTS leads_crm_activity ON leads (org_id, (COALESCE(last_activity, created_at)) DESC, id DESC) WHERE is_lead;
CREATE INDEX IF NOT EXISTS leads_crm_deal ON leads (org_id, deal_value DESC NULLS LAST, id DESC) WHERE is_lead;
-- Duplicate detection on create (crm.Create). Plain columns, because under Row-Level Security an index
-- on an expression such as lower(email) cannot be used (lower() is not leakproof); "=" on a column can.
-- (Search and tag filters use ILIKE, which is not leakproof either, so they filter the workspace's rows
-- found through the org_id indexes; trigram indexes would not be used and are not added.)
ALTER TABLE leads ADD COLUMN IF NOT EXISTS email_lc text GENERATED ALWAYS AS (lower(email)) STORED;
ALTER TABLE leads ADD COLUMN IF NOT EXISTS phone_tail text GENERATED ALWAYS AS (right(regexp_replace(phone, '\D', '', 'g'), 10)) STORED;
CREATE INDEX IF NOT EXISTS leads_crm_email ON leads (org_id, email_lc);
CREATE INDEX IF NOT EXISTS leads_crm_phone ON leads (org_id, phone_tail);
-- Recent activity (dashboard / activity page) in id order
CREATE INDEX IF NOT EXISTS events_crm_recent ON events (org_id, id DESC);
CREATE INDEX IF NOT EXISTS events_crm_type ON events (org_id, type, id DESC);
-- Custom fields in display order
CREATE INDEX IF NOT EXISTS fields_crm_sort ON fields (org_id, sort, id);
