// Package app wires HTTP routing, tenancy, sessions, ACL and module hooks.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/config"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/jobs"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

type App struct {
	Cfg  *config.Config
	DB   *db.DB
	Box  *secure.Box
	Jobs *jobs.Queue
	// Mux is the root router: /api/* (JSON, signed in), public pages, webhooks, SPA.
	Mux chi.Router
	api chi.Router

	orgs sync.Map // host → *orgEntry
	// BrandName is the white-label name of this installation (set by core; default "Setu").
	BrandName func(ctx context.Context) string

	// SystemMail sends a platform email (password reset, notices) with the platform mailer; set by messaging.
	SystemMail func(ctx context.Context, to, subject, html string) error

	hookMu sync.RWMutex
	hooks  map[string][]HookFn
	seeds  []SeedFn
}

// HookFn reacts to a domain event (e.g. "lead_created") inside the caller's tenant transaction.
type HookFn func(ctx context.Context, tx pgx.Tx, orgID, leadID int64, data map[string]any) error

// SeedFn fills a new organisation with defaults (prompts, recipes, settings…).
type SeedFn func(ctx context.Context, tx pgx.Tx, orgID int64, s *settings.Store) error

func New(cfg *config.Config, d *db.DB) *App {
	a := &App{Cfg: cfg, DB: d, Box: secure.NewBox(cfg.MasterKey), hooks: map[string][]HookFn{}, BrandName: func(context.Context) string { return "Setu" }}
	Default = a
	a.Jobs = jobs.New(d, cfg.OrgJobCap)
	r := chi.NewRouter()
	r.Use(recoverer, securityHeaders)
	a.Mux = r
	r.Route("/api", func(api chi.Router) { a.api = api })
	return a
}

// ---------------------------------------------------------------- route registration

type opts struct {
	perm    string
	module  string
	public  bool
	console bool // platform-admin only; works without an organisation
	anyOrg  bool // signed in, organisation optional (me, account, logout)
}

type Opt func(*opts)

// Perm requires a permission (see Can). Module requires an enabled module ("prospecting").
func Perm(p string) Opt   { return func(o *opts) { o.perm = p } }
func Module(m string) Opt { return func(o *opts) { o.module = m } }

// PlatformOnly: platform admins (SaaS owner) only; no organisation needed.
func PlatformOnly() Opt { return func(o *opts) { o.console = true } }

// AnyOrg: signed-in route that also works in the platform console (no organisation).
func AnyOrg() Opt { return func(o *opts) { o.anyOrg = true } }

// NoAuth: the route runs for the workspace of the host without a signed-in user.
func NoAuth() Opt { return func(o *opts) { o.public = true } }

func (a *App) Get(p string, h Handler, o ...Opt)    { a.handle(http.MethodGet, p, h, o) }
func (a *App) Post(p string, h Handler, o ...Opt)   { a.handle(http.MethodPost, p, h, o) }
func (a *App) Put(p string, h Handler, o ...Opt)    { a.handle(http.MethodPut, p, h, o) }
func (a *App) Patch(p string, h Handler, o ...Opt)  { a.handle(http.MethodPatch, p, h, o) }
func (a *App) Delete(p string, h Handler, o ...Opt) { a.handle(http.MethodDelete, p, h, o) }

func (a *App) handle(method, path string, h Handler, os []Opt) {
	var o opts
	for _, f := range os {
		f(&o)
	}
	a.api.Method(method, path, a.wrap(h, o))
}

// Public registers a route outside /api (public pages, webhooks, file downloads). The workspace is
// taken from the host when it has one; handlers may also resolve it from a token (c.UseToken).
func (a *App) Public(method, path string, h Handler) {
	a.Mux.Method(method, path, a.wrap(h, opts{public: true}))
}

// ---------------------------------------------------------------- hooks & seeds

func (a *App) On(event string, fn HookFn) {
	a.hookMu.Lock()
	a.hooks[event] = append(a.hooks[event], fn)
	a.hookMu.Unlock()
}

// Emit runs every hook of an event; hook errors are logged, never fail the caller.
func (a *App) Emit(ctx context.Context, tx pgx.Tx, orgID int64, event string, leadID int64, data map[string]any) {
	a.hookMu.RLock()
	hs := a.hooks[event]
	a.hookMu.RUnlock()
	for _, h := range hs {
		if _, err := tx.Exec(ctx, "SAVEPOINT hook"); err != nil {
			return
		}
		if err := h(ctx, tx, orgID, leadID, data); err != nil {
			slog.Error("hook", "event", event, "org", orgID, "lead", leadID, "err", err)
			tx.Exec(ctx, "ROLLBACK TO SAVEPOINT hook")
			continue
		}
		tx.Exec(ctx, "RELEASE SAVEPOINT hook")
	}
}

func (a *App) Seed(fn SeedFn) { a.seeds = append(a.seeds, fn) }

// RunSeeds fills a new organisation.
func (a *App) RunSeeds(ctx context.Context, tx pgx.Tx, orgID int64, s *settings.Store) error {
	for _, f := range a.seeds {
		if err := f(ctx, tx, orgID, s); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- shared helpers for modules & jobs

// OrgTx runs fn in a tenant transaction with the organisation's settings loaded.
func (a *App) OrgTx(ctx context.Context, orgID int64, fn func(tx pgx.Tx, s *settings.Store) error) error {
	return a.DB.Org(ctx, orgID, func(tx pgx.Tx) error {
		s, err := settings.Load(ctx, tx, a.Box, orgID)
		if err != nil {
			return err
		}
		return fn(tx, s)
	})
}

// Event writes a journey entry for a lead and bumps its last activity.
func Event(ctx context.Context, q db.Q, leadID int64, typ, msg, channel string, meta any) error {
	var m []byte
	if meta != nil {
		m, _ = json.Marshal(meta)
	}
	if len([]rune(msg)) > 1000 {
		msg = string([]rune(msg)[:1000])
	}
	if _, err := q.Exec(ctx, `INSERT INTO events (lead_id,type,channel,message,meta) VALUES ($1,$2,$3,$4,$5)`, leadID, typ, channel, msg, m); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE leads SET last_activity=now() WHERE id=$1`, leadID)
	return err
}

// TrackUsage adds one API call to today's usage of the organisation of the transaction.
func TrackUsage(ctx context.Context, q db.Q, service, op string, unitsIn, unitsOut int64, cost float64, failed bool) {
	e := 0
	if failed {
		e = 1
	}
	if _, err := q.Exec(ctx, `INSERT INTO api_usage (day,service,op,calls,units_in,units_out,cost,errors) VALUES (current_date,$1,$2,1,$3,$4,$5,$6)
ON CONFLICT (org_id,day,service,op) DO UPDATE SET calls=api_usage.calls+1, units_in=api_usage.units_in+EXCLUDED.units_in, units_out=api_usage.units_out+EXCLUDED.units_out, cost=api_usage.cost+EXCLUDED.cost, errors=api_usage.errors+EXCLUDED.errors`,
		service, op, unitsIn, unitsOut, cost, e); err != nil {
		slog.Warn("usage", "err", err)
	}
}

// Throttle is a per-key rate limit: false when more than max hits happened within window.
func (a *App) Throttle(ctx context.Context, key string, max int, window time.Duration) bool {
	if len(key) > 120 {
		key = key[:120]
	}
	iv := fmt.Sprintf("%d seconds", int(window.Seconds()))
	var n int
	err := a.DB.Pool.QueryRow(ctx, `INSERT INTO throttle (k,n,last) VALUES ($1,1,now())
ON CONFLICT (k) DO UPDATE SET n = CASE WHEN throttle.last < now()-$2::interval THEN 1 ELSE throttle.n+1 END, last=now() RETURNING n`, key, iv).Scan(&n)
	return err != nil || n <= max
}

// PublicToken creates a public link token that resolves to (org, kind, ref).
func PublicToken(ctx context.Context, q db.Q, orgID int64, kind string, ref int64, ttl time.Duration) (string, error) {
	t := secure.Token(20)
	var exp *time.Time
	if ttl > 0 {
		e := time.Now().Add(ttl)
		exp = &e
	}
	_, err := q.Exec(ctx, `INSERT INTO public_tokens (token,org_id,kind,ref_id,expires_at) VALUES ($1,$2,$3,$4,$5)`, t, orgID, kind, ref, exp)
	return t, err
}

// RegisterToken maps an existing token (e.g. sources.token) to the organisation.
func RegisterToken(ctx context.Context, q db.Q, orgID int64, kind string, ref int64, token string) error {
	_, err := q.Exec(ctx, `INSERT INTO public_tokens (token,org_id,kind,ref_id) VALUES ($1,$2,$3,$4) ON CONFLICT (token) DO NOTHING`, token, orgID, kind, ref)
	return err
}

// ResolveToken returns (org, ref) of a public token of a given kind.
func (a *App) ResolveToken(ctx context.Context, kind, token string) (int64, int64, error) {
	var org, ref int64
	err := a.DB.Pool.QueryRow(ctx, `SELECT org_id, ref_id FROM public_tokens WHERE token=$1 AND kind=$2 AND (expires_at IS NULL OR expires_at>now())`, token, kind).Scan(&org, &ref)
	return org, ref, err
}

// SlugOK validates a workspace sub-domain.
func SlugOK(s string) bool {
	if len(s) < 3 || len(s) > 40 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return !reserved[s]
}

var reserved = map[string]bool{"www": true, "app": true, "api": true, "admin": true, "mail": true, "smtp": true, "imap": true, "ftp": true, "static": true, "cdn": true, "assets": true, "status": true, "help": true, "docs": true, "blog": true, "billing": true, "login": true, "signup": true, "setu": true, "dashboard": true, "support": true, "dev": true, "staging": true, "test": true}
