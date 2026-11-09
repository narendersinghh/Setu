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
