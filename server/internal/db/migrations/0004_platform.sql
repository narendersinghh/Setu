-- Platform (SaaS owner) features: pricing plans & limits, per-organisation modules/limits overrides,
-- white-label settings, platform-admin console sessions (no organisation).

CREATE TABLE plans (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  slug citext NOT NULL UNIQUE,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  price_month numeric(12,2) NOT NULL DEFAULT 0,
  price_year numeric(12,2) NOT NULL DEFAULT 0,
  currency text NOT NULL DEFAULT 'INR',
  trial_days int NOT NULL DEFAULT 0,
  features jsonb NOT NULL DEFAULT '[]',     -- bullet points shown on the pricing cards
  limits jsonb NOT NULL DEFAULT '{}',       -- {"users":3,"leads":1000,...}; missing/0 = unlimited
  modules jsonb NOT NULL DEFAULT '["crm"]',
  public boolean NOT NULL DEFAULT true,     -- shown on sign up
  is_default boolean NOT NULL DEFAULT false,
  highlight boolean NOT NULL DEFAULT false,
  sort int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX plans_one_default ON plans (is_default) WHERE is_default;

INSERT INTO plans (slug,name,description,price_month,price_year,trial_days,features,limits,modules,is_default,highlight,sort) VALUES
 ('free','Starter','For freelancers getting started',0,0,0,
  '["2 team members","1,000 leads","Lead capture from forms & ads","Appointments & booking page"]',
  '{"users":2,"leads":1000,"automations":2,"sources":2,"searches_month":0,"reports_month":0,"emails_month":500,"calls_month":0}',
  '["crm"]',true,false,1),
 ('growth','Growth','For growing agencies',1999,19990,14,
  '["5 team members","20,000 leads","Automations & AI calls","Google Maps prospecting","AI audit reports"]',
  '{"users":5,"leads":20000,"automations":20,"sources":10,"searches_month":50,"reports_month":200,"emails_month":10000,"calls_month":300}',
  '["crm","prospecting"]',false,true,2),
 ('pro','Pro','Unlimited scale for large teams',4999,49990,14,
  '["Unlimited team members","Unlimited leads","Everything in Growth","Priority support"]',
  '{}','["crm","prospecting"]',false,false,3);

ALTER TABLE orgs
  ADD COLUMN plan_id bigint REFERENCES plans(id),
  ADD COLUMN modules jsonb,                 -- override of the plan's modules (null = plan)
  ADD COLUMN limits jsonb,                  -- override of the plan's limits per key (null = plan)
  ADD COLUMN trial_ends_at timestamptz,
  ADD COLUMN plan_expires_at timestamptz,
  ADD COLUMN notes text NOT NULL DEFAULT '';
UPDATE orgs SET plan_id = (SELECT id FROM plans WHERE is_default) WHERE plan_id IS NULL;

-- Platform admins sign in to the console without an organisation
ALTER TABLE sessions ALTER COLUMN org_id DROP NOT NULL;

-- Instance-wide settings (branding, platform mailer, updater)
CREATE TABLE platform_settings (
  k text PRIMARY KEY,
  v text NOT NULL DEFAULT ''
);

-- Password reset links (one-time, hashed, 1 hour)
CREATE TABLE password_resets (
  token_hash text PRIMARY KEY,
  user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at timestamptz NOT NULL
);
