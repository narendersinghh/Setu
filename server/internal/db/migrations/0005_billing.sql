-- Plan purchases through payment gateways (Razorpay Payment Links, Stripe Checkout)
CREATE TABLE payments (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id bigint NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  plan_id bigint REFERENCES plans(id) ON DELETE SET NULL,
  user_id bigint REFERENCES users(id) ON DELETE SET NULL,
  cycle text NOT NULL DEFAULT 'month',          -- month | year
  gateway text NOT NULL,                        -- razorpay | stripe
  amount numeric(12,2) NOT NULL,
  currency text NOT NULL DEFAULT 'INR',
  status text NOT NULL DEFAULT 'pending',       -- pending | paid | failed
  ext_id text NOT NULL DEFAULT '',              -- payment link id / checkout session id
  ext_payment text NOT NULL DEFAULT '',         -- gateway payment id
  created_at timestamptz NOT NULL DEFAULT now(),
  paid_at timestamptz
);
CREATE INDEX payments_org ON payments (org_id, id DESC);
CREATE INDEX payments_ext ON payments (gateway, ext_id);
