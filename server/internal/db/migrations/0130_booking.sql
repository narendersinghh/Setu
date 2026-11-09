-- booking: cross-workspace index of appointments that still need a 1-hour reminder.
-- The reminder task runs without a workspace (RLS hides tenant rows), so it reads this small
-- table to find which organisations have appointments starting soon, then works per organisation.
CREATE TABLE appt_due (
  appt_id bigint PRIMARY KEY,
  org_id bigint NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  start_at timestamptz NOT NULL
);
CREATE INDEX appt_due_start ON appt_due (start_at, org_id);
CREATE INDEX appt_due_org ON appt_due (org_id);

-- Same policy as jobs/public_tokens: visible to the scanner (no workspace) and to its own workspace.
ALTER TABLE appt_due ENABLE ROW LEVEL SECURITY;
ALTER TABLE appt_due FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON appt_due USING (app_org() IS NULL OR org_id = app_org()) WITH CHECK (app_org() IS NULL OR org_id = app_org());

CREATE OR REPLACE FUNCTION appt_due_sync() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    DELETE FROM appt_due WHERE appt_id = OLD.id;
    RETURN OLD;
  END IF;
  IF NEW.status = 'scheduled' AND NOT NEW.reminded AND NEW.lead_id > 0 THEN
    INSERT INTO appt_due (appt_id, org_id, start_at) VALUES (NEW.id, NEW.org_id, NEW.start_at)
    ON CONFLICT (appt_id) DO UPDATE SET start_at = EXCLUDED.start_at;
  ELSE
    DELETE FROM appt_due WHERE appt_id = NEW.id;
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER appointments_due AFTER INSERT OR DELETE OR UPDATE OF status, reminded, start_at, lead_id ON appointments
  FOR EACH ROW EXECUTE FUNCTION appt_due_sync();

-- Appointments of one owner (sales people see their own)
CREATE INDEX appointments_user ON appointments (org_id, user_id, start_at);
