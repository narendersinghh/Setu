# Setu v2 – Multi-tenant SaaS rewrite plan

Goal: turn Setu (single-install PHP + MySQL) into one multi-tenant SaaS where every organisation
signs up on the same URL and gets `https://{workspace}.setu.in`, with the same features as v1.0.8,
able to carry lakhs of organisations on low-cost VPS hardware with no paid database.

## 1. Stack

| Layer | Choice | Reason |
|---|---|---|
| API + workers | **Go 1.24** (chi router, pgx v5) – one static binary: `setu serve`, `setu worker`, `setu migrate` | ~50–150 MB RAM, high concurrency, tiny patch surface |
| Database | **PostgreSQL 17** (16+ works) | Row-Level Security for tenant isolation, JSONB custom fields, partitioning, Citus to shard by `org_id` later |
| Job queue | PostgreSQL table + `FOR UPDATE SKIP LOCKED`, per-org fairness cap | No extra server; job is created in the same transaction as the data |
| Cache / rate limits | PostgreSQL (`throttle` table) now, Valkey optional later | Fewer moving parts at launch |
| Front end | **React 18 + TypeScript + Vite**, built to static files and embedded in the Go binary | No Node process in production |
| Public pages (booking, web report, thank-you, tracking) | Go `html/template`, server rendered | Fast, SEO-friendly, no JS needed |
| PDF | **Gotenberg** (Chromium) sidecar | Pixel-perfect HTML → PDF, capped memory |
| Web server / TLS | **Caddy** – wildcard cert for `*.setu.in` via Cloudflare DNS, on-demand TLS for custom domains | Zero-maintenance HTTPS |
| Edge | Cloudflare free plan | DNS, DDoS, CDN |
| Deploy | Docker Compose on Ubuntu 24.04 | One command deploy / rollback |
| Backups | pgBackRest (PITR) to a second VPS | Point-in-time restore |
| Monitoring | Beszel / Netdata + Uptime Kuma | Low RAM |

## 2. Repository layout

```
server/                         Go module
  cmd/setu/main.go              serve | worker | migrate | all
  internal/config               env config
  internal/db                   pgx pool, tenant transactions (RLS), migrator, SQL migrations
  internal/secure               argon2id, AES-GCM per-tenant keys, tokens
  internal/app                  App, Ctx, router, middleware (tenant, auth, CSRF, headers, ACL)
  internal/settings             per-org settings with encrypted secrets
  internal/jobs                 Postgres job queue + periodic scheduler
  internal/netx                 outbound HTTP with SSRF guard + usage hooks
  internal/core                 signup, login, sessions, me, platform admin
  internal/mod/<module>         feature modules (see §5)
web/                            React SPA (Vite)
deploy/                         docker-compose.yml, Caddyfile, backup scripts
docs/                           this plan, conventions, operations
(legacy PHP stays at repo root until cutover, untouched)
```

## 3. Multi-tenancy model

* One shared schema. Every tenant table has `org_id BIGINT NOT NULL DEFAULT current org`.
* **Row-Level Security** on every tenant table (`FORCE`d). The app connects as `setu_app` (no BYPASSRLS);
  every tenant query runs inside a transaction that sets `app.org_id`. A forgotten `WHERE org_id=` can
  never leak data – Postgres returns nothing.
* Global tables (no RLS): `orgs`, `users`, `memberships`, `sessions`, `throttle`, `public_tokens`,
  `jobs`, `research_cache`.
* `public_tokens(token → org_id, kind, ref_id)` resolves public links (lead-capture endpoints,
  web reports, booking pages, call webhooks, tracking pixels) to the right organisation.
* Every index starts with `org_id`. `orgs.shard` is stored from day one so organisations can later be
  moved to other database pods / a Citus cluster without app changes.
* Users are global (one login, many workspaces); `memberships(org_id,user_id,role)` holds the role.

## 4. Security

argon2id passwords · server-side sessions (hashed token, HttpOnly, Secure, SameSite=Lax, per-subdomain) ·
CSRF token header on every mutating request · login & public-form throttling · per-tenant AES-256-GCM
keys (HKDF from master key) for API keys/passwords · SSRF-safe outbound HTTP (blocks private,
loopback, link-local and metadata IPs at dial time, every redirect) · strict security headers + CSP ·
RLS · audit trail in `events` · least-privilege DB roles · CSV formula-injection guard.

## 5. Module map (v1 PHP → v2 Go)

| v2 module | Replaces (v1) | Features |
|---|---|---|
| core | install.php, login, account, team, user, modules, bootstrap, helpers, Settings, Migrator, Updater | signup, workspaces, login/logout, roles (super/admin/sales), team, modules on/off, account, platform admin |
| crm | CRM.php, leads, lead, lead_new, fields, dashboard, activity | leads list/board, statuses, owners, follow-ups, tags, deal value, custom fields, CSV import/export, journey, notes, dashboard |
| capture | Capture.php, hook.php, sources | website forms, embed script, Meta & Google Ads webhooks, generic webhook, field mapping, duplicate merge |
| automation | Automation.php, automations, automation, recipes | triggers, conditions, steps, recipes, runs, journey logging |
| booking | Booking.php, GCal.php, book.php, gcal.php, appointments | calendar, public booking page, reminders, Google Calendar sync + Meet |
| messaging | Mailer.php, Outreach.php, WhatsApp.php, Voice.php, emails, webhook.php, callhook.php, track.php | email (API services/SMTP), IMAP inbox, WhatsApp Cloud API, AI voice calls, open tracking |
| prospecting | Maps.php, Scraper.php, Auditor.php, campaigns, scraper, prospects | Google Places/SerpAPI search, contact scan, 45-check website audit, Maps audit |
| reports | AI.php, Parallel.php, Batch.php, Report.php, Charts.php, Illus.php, Trace.php, report.php, reports, templates, prompts, ai | AI providers, parallel engine, Claude batches, research cache, branded web report + PDF, process log |
| usage | Usage.php, usage | API usage & costs per org, price table, plan quotas |

Dropped by design: install.php (signup instead), one-click GitHub updater (central deploys),
web-kicked cron (real workers), per-install crontab editing.

## 6. Phases

1. **Foundation** – schema + RLS, migrator, auth, signup, tenancy, settings, queue, netx, SPA shell, deploy kit.
2. **CRM core** – leads, fields, journey, dashboard, team, modules, settings UI.
3. **Capture + automations + booking.**
4. **Messaging** – email, inbox/IMAP, WhatsApp, voice.
5. **Prospecting + AI reports + usage.**
6. **Hardening** – load test (k6), backups drill, monitoring, security review.
7. **Cutover** – `setu import-v1` imports each existing v1 MySQL install as one organisation; old PHP removed.

## 7. Scaling path

* ≤5k orgs: 1 VPS (4 vCPU / 8 GB) running Caddy, setu (serve+worker), Postgres, Gotenberg.
* ≤50k orgs: dedicated DB VPS + streaming replica, 2+ stateless app VPS, PgBouncer.
* Lakhs: Citus distributed by `org_id` (or multiple pods via `orgs.shard`); `events`/`messages` partitioned by month.
