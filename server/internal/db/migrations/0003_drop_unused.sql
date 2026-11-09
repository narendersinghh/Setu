-- Expression indexes that Row-Level Security prevents the planner from using (non-leakproof functions);
-- the crm module uses indexed generated columns (email_lc, phone_tail) instead.
DROP INDEX IF EXISTS leads_email;
DROP INDEX IF EXISTS leads_phone;
DROP INDEX IF EXISTS leads_search;
