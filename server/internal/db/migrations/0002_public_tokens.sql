-- Every public token (lead booking/tracking links, capture endpoints, web reports, call hooks, message
-- tracking) is registered in public_tokens automatically, so public URLs resolve to their organisation.
CREATE OR REPLACE FUNCTION register_public_token() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_TABLE_NAME = 'leads' AND (NEW.token IS NULL OR NEW.token = '') THEN
    NEW.token := encode(gen_random_bytes(16), 'hex');
  END IF;
  IF NEW.token IS NOT NULL AND NEW.token <> '' AND (TG_OP = 'INSERT' OR NEW.token IS DISTINCT FROM OLD.token) THEN
    INSERT INTO public_tokens (token, org_id, kind, ref_id) VALUES (NEW.token, NEW.org_id, TG_ARGV[0], NEW.id)
    ON CONFLICT (token) DO NOTHING;
  END IF;
  RETURN NEW;
END $$;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TRIGGER leads_token BEFORE INSERT OR UPDATE OF token ON leads FOR EACH ROW EXECUTE FUNCTION register_public_token('lead');
CREATE TRIGGER sources_token AFTER INSERT OR UPDATE OF token ON sources FOR EACH ROW EXECUTE FUNCTION register_public_token('source');
CREATE TRIGGER reports_token AFTER INSERT OR UPDATE OF token ON reports FOR EACH ROW EXECUTE FUNCTION register_public_token('report');
CREATE TRIGGER calls_token AFTER INSERT OR UPDATE OF token ON calls FOR EACH ROW EXECUTE FUNCTION register_public_token('call');
CREATE TRIGGER messages_token AFTER INSERT OR UPDATE OF token ON messages FOR EACH ROW EXECUTE FUNCTION register_public_token('msg');
