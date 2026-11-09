-- CRM: indexes for the leads list, board, filters, search and dashboard on large workspaces.

-- Default list order (newest lead first) and the pipeline board (per status)
CREATE INDEX IF NOT EXISTS leads_crm_recent ON leads (org_id, (COALESCE(lead_at, created_at)) DESC, id DESC) WHERE is_lead;
CREATE INDEX IF NOT EXISTS leads_crm_board ON leads (org_id, status, (COALESCE(lead_at, created_at)) DESC, id DESC) WHERE is_lead;
-- Sorting by last activity and by deal value
CREATE INDEX IF NOT EXISTS leads_crm_activity ON leads (org_id, (COALESCE(last_activity, created_at)) DESC, id DESC) WHERE is_lead;
CREATE INDEX IF NOT EXISTS leads_crm_deal ON leads (org_id, deal_value DESC NULLS LAST, id DESC) WHERE is_lead;
-- Tag filter (LIKE '%tag%')
CREATE INDEX IF NOT EXISTS leads_crm_tags ON leads USING gin (tags gin_trgm_ops) WHERE tags <> '';
-- Search across name, contact, email, phone, city, tags and custom fields (ILIKE '%q%')
CREATE INDEX IF NOT EXISTS leads_crm_search ON leads USING gin (
  (name || ' ' || contact_name || ' ' || email || ' ' || phone || ' ' || city || ' ' || tags || ' ' || (custom::text)) gin_trgm_ops);
-- Duplicate detection on create (same expressions as crm.Create)
CREATE INDEX IF NOT EXISTS leads_crm_email ON leads (org_id, lower(email)) WHERE is_lead AND email <> '';
CREATE INDEX IF NOT EXISTS leads_crm_phone ON leads (org_id, right(regexp_replace(phone, '\D', '', 'g'), 10)) WHERE is_lead AND phone <> '';
-- Recent activity (dashboard / activity page) in id order
CREATE INDEX IF NOT EXISTS events_crm_recent ON events (org_id, id DESC);
CREATE INDEX IF NOT EXISTS events_crm_type ON events (org_id, type, id DESC);
-- Custom fields in display order
CREATE INDEX IF NOT EXISTS fields_crm_sort ON fields (org_id, sort, id);
