-- Messaging: inbox, WhatsApp, AI calls.

-- Emails page: conversations per lead (channel first, newest first)
CREATE INDEX IF NOT EXISTS messages_channel ON messages (org_id, channel, lead_id, created_at DESC);

-- Call webhooks without a token (Retell) and provider ids
CREATE INDEX IF NOT EXISTS calls_ext ON calls (org_id, ext_id);

-- When the call ended (first final webhook) – call_completed fires only once per call
ALTER TABLE calls ADD COLUMN IF NOT EXISTS ended_at timestamptz;

-- The IMAP poller runs without a workspace: it may read only the "imap_on" switch of every workspace
-- (no other setting, no secret). Inside a tenant transaction this policy never applies.
DROP POLICY IF EXISTS imap_poll ON settings;
CREATE POLICY imap_poll ON settings FOR SELECT USING (app_org() IS NULL AND k = 'imap_on');
