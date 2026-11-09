# Setu v2 – conventions for module work

Read `docs/REWRITE_PLAN.md` first. v1 (PHP) stays in the repo root as the **functional reference**:
every v1 feature must exist in v2 with the same behaviour, labels and options.

## Ground rules
* Edit only your own module directories: `server/internal/mod/<module>/` and `web/src/modules/<module>/`,
  plus migrations with your reserved number range. Do **not** edit core/app/db/shared/ui files – if
  you need a change there, write it in your final report.
* Do **not** run `go get`, `go mod tidy` or `npm install` (go.mod / package.json are shared). Available:
  stdlib, pgx, chi, x/crypto, x/net/html, goquery, emersion/go-imap v1, emersion/go-message;
  web: React 18, react-router 6, TanStack Query 5.
* Check your work compiles: `cd server && go build ./... && go vet ./internal/mod/<module>/...` and
  `cd web && npx tsc -b`. (Other modules may be mid-change; only your files must be clean.)
* No secrets in logs. User-facing text in plain English, same wording as v1 where it exists.
* Code style: normal gofmt Go and readable TSX (the minified style of v1 is not required).

## Server (Go)
* `func Register(a *app.App)` in your package registers everything:
  * `a.Get/Post/Put/Patch/Delete(path, handler, app.Perm("leads"), app.Module("prospecting"))` → `/api/...`
    (signed in, CSRF checked, RLS transaction). Permissions: dashboard, leads, appointments, account,
    prospecting, automations, templates, settings, team, modules, super_users.
  * `a.Public(method, path, handler)` → public pages & webhooks outside `/api` (no session). Resolve the
    workspace with `c.UseToken(kind, token)` (kinds: lead, source, report, call, msg – registered
    automatically by DB triggers when rows are inserted) or `c.UseOrg(id)`.
  * `a.Jobs.Handle(type, timeout, fn)` background jobs; `a.Jobs.Every(name, interval, fn)` periodic tasks.
  * `a.On(event, fn)` domain hooks; `a.Seed(fn)` defaults for a new workspace.
* Handler: `func(c *app.Ctx) (any, error)`; return a value → JSON; `app.Bad("msg")`, `app.NotFound()`,
  `app.Forbidden()` for errors. `c.Tx()` is the tenant transaction (Row-Level Security: queries never
  need `org_id`; inserts get it by default). `c.S()` settings, `c.SetS(k,v)`, `c.Bind(&in)`, `c.ParamID("id")`,
  `c.Query("q")`, `c.Scope("l")` (sales people see own + unassigned leads), `c.LeadOK(isLead, assignedTo)`,
  `c.Can(p)`, `c.Mod(m)`, `c.TZ()`, `c.BaseURL()`, `c.After(fn)` (after commit), `c.Raw()` (handler wrote the response).
* Outside a request (jobs, periodic): `a.OrgTx(ctx, orgID, func(tx pgx.Tx, s *settings.Store) error {...})`.
  Never hold a transaction open across a slow external call (AI, HTTP) – use short transactions.
* Helpers: `db.All/One/Val/Maps/Map/Exec/Insert/Update` (`internal/db`), `app.Event(ctx,q,leadID,type,msg,channel,meta)`
  (journey), `app.TrackUsage(ctx,q,service,op,in,out,cost,failed)`, `a.Throttle(...)`, `netx.Do(ctx, netx.Req{..., Public:true})`
  (**always `Public:true` for URLs from users/websites** – SSRF guard), `pdf.Render(...)` (Gotenberg at
  `a.Cfg.GotenbergURL`), `core.SaveFile(...)` for uploads, `jobs.Push(ctx, tx, orgID, type, ref, payload)`.
* `internal/mod/shared`: `Lead` + `GetLead`, `LeadCols`, `LeadVars` (template variables), `Tpl`, `PhoneNorm`,
  `PhoneTail`, `CRMStatuses`, `Stages`, `LeadSources`, `Progress` (+ abort → `ErrAborted`), `Trace` (process log),
  `BookingLink`, `GeneralBookingLink`, `ReportLink`, `Money`, `FormatNum`.
* Timestamps are `timestamptz`; show them in the workspace time zone (`c.TZ()`).
* Settings are per workspace (`settings` table). Generic `GET/PUT /api/settings` already exists (secrets are
  masked as `••••••••`, sending the mask back means unchanged). File upload: `POST /api/files` (multipart
  `file`, optional `setting` key) → `{url}`.

## Domain events (`a.Emit(ctx, tx, orgID, event, leadID, data)`)
`lead_created {source}`, `status_changed {status}`, `tag_added {tag}`, `replied {via}`, `report_ready`,
`appointment_booked {appointment_id}`, `call_completed {call_id}`, `lead_moved` (scraped → leads).
Emitters: crm (created/status/tag), capture (lead_created via crm.Create), messaging (replied, call_completed),
booking (appointment_booked), reports (report_ready). Listener: automation (+ others as needed).
Time-based triggers (`no_reply`, `follow_up_due`) are periodic tasks in automation.

## Cross-module Go contracts (do not change signatures)
* `crm.Create(ctx, a, tx, orgID, CreateIn) (leadID int64, err error)` – create or merge duplicate (30 days, email/phone tail), emits `lead_created`.
* `crm.SetStatus(ctx, a, tx, orgID, leadID, status, note)`, `crm.AddTag(ctx, a, tx, orgID, leadID, tag)`, `crm.Promote(ctx, a, tx, orgID, leadID, reason)` (scraped → lead).
* `messaging.SendEmail / SendWhatsApp / StartCall` (see `internal/mod/messaging/messaging.go`).
* `ai.Complete / ai.Configured` (see `internal/mod/ai/ai.go`).
* Job types: `audit` (prospecting), `report` (reports), `contact` (prospecting: send report to the business),
  `enrich` (prospecting: find phone/email on website), `search` (prospecting: maps search), `aipar` (ai engine).
* `reports.Enqueue(ctx, tx, orgID, leadID, opts)` starts a report; `prospecting.EnqueueProcess(ctx, tx, orgID, ids, steps)`.

## HTTP contracts used across modules (implemented by the owner)
| Endpoint | Owner |
|---|---|
| `GET /api/nav` → `{leads_new, scraped, mail_unread, appt_today}` | crm |
| `GET /api/leads`, `GET/PUT/DELETE /api/leads/{id}`, `POST /api/leads`, `GET /api/leads/{id}/journey`, `POST /api/leads/bulk` | crm |
| `POST /api/leads/{id}/email {subject, body, attach_report}` / `POST /api/leads/{id}/whatsapp {message, attach_report}` → `{ok, link?}` / `POST /api/leads/{id}/call {task, first_message, language}` | messaging |
| `GET /api/leads/{id}/messages`, `GET /api/leads/{id}/calls` | messaging |
| `GET /api/leads/{id}/appointments`, `POST /api/appointments` | booking |
| `POST /api/prospects/process {ids, steps}`, `POST /api/prospects/abort {ids}` | prospecting |
| `POST /api/leads/{id}/report`, `GET /api/leads/{id}/reports` | reports |
| `GET /api/leads/{id}/process` (process log), `GET /api/activity`, `POST /api/activity/stop-all` | ai-engine |
| `GET /api/leads/{id}/automations` (runs) | automation |

## Public URLs
`/book/{leadToken}`, `/book/g/{bookingKey}`, `/r/{reportToken}` (web report), `/r/{reportToken}.pdf`,
`/hook/{sourceToken}` (lead capture), `/hook/{sourceToken}.js` (embed script), `/webhooks/whatsapp`,
`/webhooks/call/{callToken}`, `/webhooks/gcal`, `/t/{msgToken}.gif` (open tracking), `/files/...`.

## Web (React)
* Your `web/src/modules/<module>/index.tsx` default-exports `{ routes, settings }` (`WebModule` in `modules/types.ts`).
  Routes are children of the signed-in layout, paths without leading slash (e.g. `{ path: 'leads/:id', element: ... }`).
  Wrap with `<Guard perm="..." module="...">` from `@/pages/Core`.
* Settings tabs: `{ key, group: 'general'|'channels'|'prospecting'|'crm'|'system', label, ic, order, component }`.
  Simple tabs use `<SettingsForm fields={[...]}/>` from `@/components/SettingsForm` (same field model as v1).
* UI kit `@/components/ui`: `Icon, Hi, Badge, Kpi, Card, Sec, Btn, IconBtn, Alert, Spinner, Empty, Modal, Tabs, Pager,
  Switch, Field, Secret, toast, act, confirmDo, usePageTitle, useInterval, useSelection`. Use the v1 CSS class names
  (styles/app.css is the v1 stylesheet) so pages look the same as v1.
* `@/lib/api` (`api.get/post/put/del/upload`, `download`), `@/lib/session` (`useMe, useCan, useMod`), `@/lib/format`.
* Data fetching with TanStack Query (`useQuery`, `useQueryClient().invalidateQueries`).

## Migrations
`server/internal/db/migrations/NNNN_name.sql`, applied in order. Reserved numbers:
crm 0100–0109 · capture 0110–0119 · automation 0120–0129 · booking 0130–0139 · messaging 0140–0149 ·
prospecting 0150–0159 · ai-engine 0160–0169 · reports 0170–0179 · usage 0180–0189.
New tenant tables: `org_id bigint NOT NULL DEFAULT app_org() REFERENCES orgs(id) ON DELETE CASCADE`, an index
starting with `org_id`, and `ENABLE`+`FORCE ROW LEVEL SECURITY` with policy
`USING (org_id = app_org()) WITH CHECK (org_id = app_org())`.

## Local run
```
pg_ctlcluster 16 main start
. /tmp/claude-0/-home-user-Setu/12594cce-59a5-5958-a691-0339f8481c2b/scratchpad/env.sh   # DATABASE_URL etc.
cd server && go build -o /tmp/setu-<module> ./cmd/setu && SETU_ADDR=:<port> /tmp/setu-<module> all
```
Use your own port (crm 8101, capture 8102, automation 8103, booking 8104, messaging 8105, prospecting 8106,
ai-engine 8107, reports 8108) and your own database: `createdb -O setu_owner setu_<module>` as postgres
(`su postgres -c "createdb -O setu_owner setu_<module>"`), then set DATABASE_URL / MIGRATE_DATABASE_URL to it.
