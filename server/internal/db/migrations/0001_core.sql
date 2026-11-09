-- Setu v2 core schema. Global tables have no RLS; tenant tables are isolated by org_id with FORCE'd RLS.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;

-- Current organisation of the transaction (set by the app with set_config('app.org_id', …, true))
CREATE OR REPLACE FUNCTION app_org() RETURNS bigint LANGUAGE sql STABLE AS
$$ SELECT NULLIF(current_setting('app.org_id', true), '')::bigint $$;

----------------------------------------------------------------- global
CREATE TABLE orgs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  slug citext NOT NULL UNIQUE,
  name text NOT NULL,
  plan text NOT NULL DEFAULT 'free',
  status text NOT NULL DEFAULT 'active',          -- active | suspended | deleted
  shard int NOT NULL DEFAULT 0,                   -- database pod (future sharding)
  custom_domain citext UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  email citext NOT NULL UNIQUE,
  name text NOT NULL,
  password_hash text NOT NULL,
  phone text NOT NULL DEFAULT '',
  totp_secret text NOT NULL DEFAULT '',
  platform_admin boolean NOT NULL DEFAULT false,
  active boolean NOT NULL DEFAULT true,
  last_login timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
  org_id bigint NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role text NOT NULL DEFAULT 'sales',             -- super | admin | sales
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id)
);
CREATE INDEX memberships_user ON memberships (user_id);

CREATE TABLE sessions (
  id text PRIMARY KEY,                            -- sha256 of the cookie token
  user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  org_id bigint NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  csrf text NOT NULL,
  ip text NOT NULL DEFAULT '',
  ua text NOT NULL DEFAULT '',
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_exp ON sessions (expires_at);

CREATE TABLE throttle (
  k text PRIMARY KEY,
  n int NOT NULL DEFAULT 0,
  last timestamptz NOT NULL DEFAULT now()
);

-- Public links → organisation (capture endpoints, reports, booking pages, call hooks, tracking)
CREATE TABLE public_tokens (
  token text PRIMARY KEY,
  org_id bigint NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  kind text NOT NULL,
  ref_id bigint NOT NULL DEFAULT 0,
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX public_tokens_ref ON public_tokens (org_id, kind, ref_id);

-- Background jobs for every organisation (workers claim with SKIP LOCKED)
CREATE TABLE jobs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  type text NOT NULL,
  ref_id bigint NOT NULL DEFAULT 0,
  payload jsonb NOT NULL DEFAULT '{}',
  status text NOT NULL DEFAULT 'pending',         -- pending | running | done | failed | cancelled
  priority smallint NOT NULL DEFAULT 0,
  attempts smallint NOT NULL DEFAULT 0,
  max_attempts smallint NOT NULL DEFAULT 3,
  error text NOT NULL DEFAULT '',
  run_after timestamptz NOT NULL DEFAULT now(),
  locked_at timestamptz,
  locked_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX jobs_due ON jobs (run_after, priority) WHERE status = 'pending';
CREATE INDEX jobs_running ON jobs (org_id) WHERE status = 'running';
CREATE INDEX jobs_ref ON jobs (org_id, type, ref_id, status);

-- Market research shared by all organisations (per business type & city)
CREATE TABLE research_cache (
  k text PRIMARY KEY,
  brief text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

----------------------------------------------------------------- tenant
CREATE TABLE settings (
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  k text NOT NULL,
  v text NOT NULL DEFAULT '',
  PRIMARY KEY (org_id, k)
);

CREATE TABLE campaigns (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  keyword text NOT NULL,
  location text NOT NULL DEFAULT '',
  limit_n int NOT NULL DEFAULT 20,
  source text NOT NULL DEFAULT 'places',
  status text NOT NULL DEFAULT 'searching',
  found int NOT NULL DEFAULT 0,
  skipped int NOT NULL DEFAULT 0,
  error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX campaigns_org ON campaigns (org_id, created_at DESC);

CREATE TABLE leads (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  campaign_id bigint NOT NULL DEFAULT 0,
  position int NOT NULL DEFAULT 0,
  place_id text NOT NULL,
  name text NOT NULL,
  category text NOT NULL DEFAULT '',
  address text NOT NULL DEFAULT '',
  city text NOT NULL DEFAULT '',
  phone text NOT NULL DEFAULT '',
  phone_source text NOT NULL DEFAULT '',
  email text NOT NULL DEFAULT '',
  website text NOT NULL DEFAULT '',
  rating numeric(2,1),
  reviews int,
  maps_url text NOT NULL DEFAULT '',
  lat numeric(10,7),
  lng numeric(10,7),
  socials jsonb,
  raw jsonb,
  web_score smallint,
  map_score smallint,
  audit jsonb,
  stage text NOT NULL DEFAULT 'new',
  email_status text NOT NULL DEFAULT '',
  wa_status text NOT NULL DEFAULT '',
  replied_at timestamptz,
  replied_via text NOT NULL DEFAULT '',
  notes text NOT NULL DEFAULT '',
  last_activity timestamptz,
  is_lead boolean NOT NULL DEFAULT false,
  source text NOT NULL DEFAULT 'maps',
  source_detail text NOT NULL DEFAULT '',
  contact_name text NOT NULL DEFAULT '',
  alt_phone text NOT NULL DEFAULT '',
  service text NOT NULL DEFAULT '',
  deal_value numeric(12,2),
  status text NOT NULL DEFAULT 'new',
  tags text NOT NULL DEFAULT '',
  follow_up_at timestamptz,
  assigned_to bigint,
  custom jsonb NOT NULL DEFAULT '{}',
  message text NOT NULL DEFAULT '',
  progress smallint NOT NULL DEFAULT 0,
  progress_msg text NOT NULL DEFAULT '',
  abort_flag boolean NOT NULL DEFAULT false,
  last_error text NOT NULL DEFAULT '',
  lead_at timestamptz,
  token text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, campaign_id, place_id)
);
CREATE INDEX leads_status ON leads (org_id, is_lead, status);
CREATE INDEX leads_source ON leads (org_id, source, is_lead);
CREATE INDEX leads_stage ON leads (org_id, stage, last_activity);
CREATE INDEX leads_follow ON leads (org_id, follow_up_at) WHERE follow_up_at IS NOT NULL;
CREATE INDEX leads_owner ON leads (org_id, assigned_to);
CREATE INDEX leads_place ON leads (org_id, place_id);
CREATE INDEX leads_email ON leads (org_id, lower(email));
CREATE INDEX leads_phone ON leads (org_id, right(regexp_replace(phone, '\D', '', 'g'), 10));
CREATE INDEX leads_search ON leads USING gin ((name || ' ' || contact_name || ' ' || email || ' ' || phone) gin_trgm_ops);

CREATE TABLE reports (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  lead_id bigint NOT NULL,
  prompt_id bigint,
  token text NOT NULL UNIQUE,
  html text NOT NULL DEFAULT '',
  pdf text NOT NULL DEFAULT '',
  views int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX reports_lead ON reports (org_id, lead_id);

CREATE TABLE prompts (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  type text NOT NULL,
  name text NOT NULL,
  subject text NOT NULL DEFAULT '',
  content text NOT NULL DEFAULT '',
  is_active boolean NOT NULL DEFAULT false,
  is_default boolean NOT NULL DEFAULT false,
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX prompts_type ON prompts (org_id, type, is_active);

CREATE TABLE events (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  lead_id bigint NOT NULL,
  type text NOT NULL,
  channel text NOT NULL DEFAULT '',
  message text NOT NULL DEFAULT '',
  meta jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_lead ON events (org_id, lead_id, id DESC);
CREATE INDEX events_time ON events (org_id, created_at DESC);

CREATE TABLE messages (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  lead_id bigint NOT NULL,
  channel text NOT NULL,
  direction text NOT NULL DEFAULT 'out',
  status text NOT NULL DEFAULT 'sent',
  ext_id text NOT NULL DEFAULT '',
  subject text NOT NULL DEFAULT '',
  body text NOT NULL DEFAULT '',
  token text NOT NULL DEFAULT '',
  msg_id text NOT NULL DEFAULT '',
  in_reply_to text NOT NULL DEFAULT '',
  addr text NOT NULL DEFAULT '',
  is_read boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX messages_lead ON messages (org_id, lead_id, direction, created_at);
CREATE INDEX messages_ext ON messages (org_id, ext_id);
CREATE INDEX messages_msgid ON messages (org_id, msg_id);
CREATE INDEX messages_unread ON messages (org_id, channel, direction) WHERE NOT is_read;

CREATE TABLE fields (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  fkey text NOT NULL,
  label text NOT NULL,
  type text NOT NULL DEFAULT 'text',
  options text NOT NULL DEFAULT '',
  required boolean NOT NULL DEFAULT false,
  sort int NOT NULL DEFAULT 0,
  UNIQUE (org_id, fkey)
);

CREATE TABLE sources (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  type text NOT NULL,
  name text NOT NULL,
  token text NOT NULL UNIQUE,
  config jsonb NOT NULL DEFAULT '{}',
  active boolean NOT NULL DEFAULT true,
  received int NOT NULL DEFAULT 0,
  last_at timestamptz,
  last_payload text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sources_org ON sources (org_id);

CREATE TABLE automations (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  name text NOT NULL,
  trig text NOT NULL,
  trig_value text NOT NULL DEFAULT '',
  conditions jsonb NOT NULL DEFAULT '[]',
  steps jsonb NOT NULL DEFAULT '[]',
  stop_on_reply boolean NOT NULL DEFAULT true,
  active boolean NOT NULL DEFAULT false,
  runs int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX automations_trig ON automations (org_id, trig) WHERE active;

CREATE TABLE automation_runs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  automation_id bigint NOT NULL,
  lead_id bigint NOT NULL,
  step int NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'running',
  next_at timestamptz,
  log jsonb NOT NULL DEFAULT '[]',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX automation_runs_lead ON automation_runs (org_id, lead_id, automation_id);
CREATE INDEX automation_runs_due ON automation_runs (next_at) WHERE status = 'running';

CREATE TABLE appointments (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  lead_id bigint NOT NULL DEFAULT 0,
  user_id bigint,
  title text NOT NULL,
  start_at timestamptz NOT NULL,
  end_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'scheduled',
  location text NOT NULL DEFAULT '',
  notes text NOT NULL DEFAULT '',
  booked_by text NOT NULL DEFAULT 'admin',
  reminded boolean NOT NULL DEFAULT false,
  gcal_id text NOT NULL DEFAULT '',
  meet_link text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX appointments_start ON appointments (org_id, status, start_at);
CREATE INDEX appointments_lead ON appointments (org_id, lead_id);
CREATE INDEX appointments_remind ON appointments (start_at) WHERE status = 'scheduled' AND NOT reminded;

CREATE TABLE calls (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  lead_id bigint NOT NULL,
  provider text NOT NULL,
  ext_id text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'queued',
  duration int,
  summary text NOT NULL DEFAULT '',
  transcript text NOT NULL DEFAULT '',
  recording text NOT NULL DEFAULT '',
  token text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX calls_lead ON calls (org_id, lead_id);

CREATE TABLE assets (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  name text NOT NULL,
  file text NOT NULL,
  placement text NOT NULL DEFAULT 'any',
  prompt_id bigint,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX assets_org ON assets (org_id);

CREATE TABLE api_usage (
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  day date NOT NULL,
  service text NOT NULL,
  op text NOT NULL DEFAULT '',
  calls int NOT NULL DEFAULT 0,
  units_in bigint NOT NULL DEFAULT 0,
  units_out bigint NOT NULL DEFAULT 0,
  cost numeric(14,6) NOT NULL DEFAULT 0,
  errors int NOT NULL DEFAULT 0,
  PRIMARY KEY (org_id, day, service, op)
);

CREATE TABLE job_steps (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  lead_id bigint NOT NULL,
  job_id bigint NOT NULL DEFAULT 0,
  kind text NOT NULL DEFAULT 'step',
  step text NOT NULL,
  status text NOT NULL DEFAULT 'run',
  detail text NOT NULL DEFAULT '',
  model text NOT NULL DEFAULT '',
  tokens_in int NOT NULL DEFAULT 0,
  tokens_out int NOT NULL DEFAULT 0,
  cost numeric(10,5) NOT NULL DEFAULT 0,
  ms int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX job_steps_lead ON job_steps (org_id, lead_id, id);

CREATE TABLE ai_batch (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE,
  batch_id text,
  lead_id bigint NOT NULL,
  mode text NOT NULL DEFAULT 'batch',
  payload jsonb,
  params jsonb,
  result text,
  tokens_in int NOT NULL DEFAULT 0,
  tokens_out int NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'queued',
  error text NOT NULL DEFAULT '',
  attempts smallint NOT NULL DEFAULT 0,
  next_at timestamptz,
  started_at timestamptz,
  step_id bigint NOT NULL DEFAULT 0,
  claim text,
  created_at timestamptz NOT NULL DEFAULT now(),
  sent_at timestamptz
);
CREATE INDEX ai_batch_mq ON ai_batch (mode, status);
CREATE INDEX ai_batch_lead ON ai_batch (org_id, lead_id);
CREATE INDEX ai_batch_b ON ai_batch (batch_id);

----------------------------------------------------------------- row-level security
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['settings','campaigns','leads','reports','prompts','events','messages','fields','sources',
    'automations','automation_runs','appointments','calls','assets','api_usage','job_steps','ai_batch'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant ON %I USING (org_id = app_org()) WITH CHECK (org_id = app_org())', t);
  END LOOP;
END $$;

-- jobs & public tokens: workers/public lookups run without an organisation and see all rows;
-- inside a tenant transaction only that organisation's rows are visible.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['jobs','public_tokens'] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    EXECUTE format('CREATE POLICY tenant ON %I USING (app_org() IS NULL OR org_id = app_org()) WITH CHECK (app_org() IS NULL OR org_id = app_org())', t);
  END LOOP;
END $$;

-- Periodic tasks (claimed atomically so only one server runs each tick)
CREATE TABLE schedules (
  name text PRIMARY KEY,
  next_at timestamptz NOT NULL DEFAULT now(),
  last_ok timestamptz,
  last_error text NOT NULL DEFAULT ''
);
