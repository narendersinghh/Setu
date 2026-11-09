# Setu v2 – platform (SaaS owner) features

The **platform admin** is the company that runs this Setu installation (Digital Nil). Organisations
(customers) never see platform admins: they are not members of any organisation, never appear in team
lists, counts, owner pickers or journeys.

## Roles
* Platform admin (`users.platform_admin`): manages organisations, plans & limits, modules per organisation,
  branding (white label), the default mailer and updates. Signs in at the same address; lands in the
  **Platform console** (`/admin`). Can "Open workspace" for support (session for that organisation without
  membership, role = owner, hidden from the team).
* Organisation roles: `super` (shown as **Owner**), `admin`, `sales`. Owners can no longer switch modules –
  modules come from the plan or the platform admin's per-organisation override.
* Bootstrap: env `SETU_ADMIN_EMAIL` + `SETU_ADMIN_PASSWORD` create/ensure the platform admin at start-up;
  `setu platform-admin <email>` also works.

## White label (one codebase, several SaaS brands)
Each deployment (instance) has its own brand: run two instances (two databases, two domains) to sell two
CRM products. Brand settings (platform admin → Branding):
`name, tagline, logo (light bg), logo_dark (dark bg), favicon, font_heading, font_body (Google Fonts),
colors {primary, accent, success, warning, danger, info}, radius, theme_default (light|dark|system)`.
Default palette (from the checkout reference): primary `#C21F3A` (crimson), accent `#FFD60A` (yellow,
dark text), success `#6BA539` (green), warning `#F59E0B`, danger `#E5484D`, info `#3B82F6`; neutrals
light `#F3F3F1` page / `#FFFFFF` surface / `#111111` ink; dark `#0E0F12` page / `#17191E` surface.
Fonts: heading `Plus Jakarta Sans`, body `Inter`. Radius `16`.

## Public API (no session)
* `GET /api/public/brand` → `{name, tagline, logo, logo_dark, favicon, font_heading, font_body,
  colors:{primary,accent,success,warning,danger,info}, radius, theme_default, signup}`
* `GET /api/public/plans` → `[{id, name, slug, description, price_month, price_year, currency, trial_days,
  highlight, features:[..], limits:{key:n}, modules:[..]}]` (public plans only, sorted)
* `POST /api/signup` accepts `plan_id` (default plan when omitted).
* `POST /api/password/forgot {email}` → always `{ok:true}`; `POST /api/password/reset {token, password}`.

## `/api/me` additions
`user.platform_admin`, `org` may be **null** (platform console session), `org.plan {id,name}`,
`limits {key:{limit,used}}`, `brand {…same as public brand}`, `support` (true when a platform admin opened
the workspace).

## Plans & limits
Limit keys (missing/0 = unlimited): `users, leads, automations, sources, searches_month, reports_month,
emails_month, calls_month`. Modules: `crm` (always) and `prospecting`. Effective modules/limits =
plan values overridden per organisation. Exceeding a limit returns HTTP 402 with a friendly message.

## Platform API (`/api/platform/*`, platform admin only)
orgs list/detail/update (status, plan, modules override, limits override, notes), `POST orgs/{id}/enter`,
plans CRUD, brand get/put, mail get/put/test, update status/check/apply, stats.

## Email
Default for every organisation = the platform mailer (platform admin → Email): `sendmail` (local
Postfix, like PHP mail) or the platform's SMTP mailbox. When an organisation adds its own SMTP / email
service in Settings → Email, that is used instead.

## Updates
Releases are built by GitHub Actions (`.github/workflows/release.yml`): every push to `main` refreshes the
`edge` pre-release; tags `v*` create stable releases. Assets: `setu-linux-amd64`, `setu-linux-arm64` (+ `.sha256`).
The platform admin clicks **Update**: the binary downloads the asset for its OS/arch, verifies the
checksum, replaces itself and exits; systemd restarts it and migrations run. Channel: stable | edge;
private repositories need a GitHub token.

## Billing & payment gateways (plan purchasing)
Gateways (platform admin → Payments): **Razorpay** (Payment Links, hosted page) and **Stripe** (Checkout,
hosted page). Keys are encrypted platform settings. Webhooks confirm payments:
`/webhooks/billing/razorpay`, `/webhooks/billing/stripe`.

Plan status per organisation: `free` (price 0), `trial` (trial_ends_at in the future), `active`
(plan_expires_at in the future, or a paid plan assigned by the platform admin without dates), `expired`
(paid plan whose trial / paid period ended → the organisation automatically runs on the default plan's
modules & limits until it pays).

* `GET /api/billing` (owners & admins, perm `settings`) →
  `{plan:{id,name,price_month,price_year,currency}, status, trial_ends_at, expires_at, effective_plan,
    limits:[{key,label,limit,used}], plans:[public plans…], gateways:[{key,name}], payments:[{id,plan,cycle,
    amount,currency,status,gateway,created_at,paid_at}]}`
* `POST /api/billing/checkout {plan_id, cycle:"month"|"year", gateway}` → `{url}` – redirect the browser.
  Afterwards the customer returns to `/billing?paid=1` (or `?failed=1`).
* `/api/me` adds `org.plan_status` and `org.plan_expires_at` (plus `trial_ends_at`) and `billing:true` when a
  gateway is enabled.
* Platform: `GET/PUT /api/platform/payments/settings` (`razorpay_enabled, razorpay_key_id,
  razorpay_key_secret, razorpay_webhook_secret, stripe_enabled, stripe_secret_key, stripe_webhook_secret`;
  response adds `webhooks:{razorpay,stripe}` URLs to copy), `GET /api/platform/payments` (all payments with
  organisation names).
