package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/secure"
)

const SessionCookie = "setu_sid"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil && v != http.ErrAbortHandler {
				slog.Error("panic", "err", v, "path", r.URL.Path, "stack", string(debug.Stack()))
				WriteJSON(w, 500, map[string]any{"error": "Something went wrong. Please try again."})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func WriteJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

// wrap turns a Handler into an http.Handler with tenancy, session, CSRF, ACL and a committed transaction.
func (a *App) wrap(h Handler, o opts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := &Ctx{Context: r.Context(), A: a, W: w, R: r}
		res, err := a.run(c, h, o)
		if c.tx != nil {
			if err == nil {
				if cerr := c.tx.Commit(context.Background()); cerr != nil {
					err = cerr
				}
			} else {
				c.tx.Rollback(context.Background())
			}
		}
		if err == nil {
			for _, f := range c.afterFn {
				f()
			}
		}
		if c.raw {
			return
		}
		if err != nil {
			var he *HTTPError
			if errors.As(err, &he) {
				WriteJSON(w, he.Code, he)
				return
			}
			slog.Error("request", "path", r.URL.Path, "org", orgID(c), "err", err)
			WriteJSON(w, 500, map[string]any{"error": "Something went wrong. Please try again."})
			return
		}
		if res == nil {
			res = map[string]any{"ok": true}
		}
		code := c.status
		if code == 0 {
			code = 200
		}
		WriteJSON(w, code, res)
	})
}

func orgID(c *Ctx) int64 {
	if c.Org == nil {
		return 0
	}
	return c.Org.ID
}

func (a *App) run(c *Ctx, h Handler, o opts) (res any, err error) {
	defer func() {
		if v := recover(); v != nil {
			if he, ok := v.(*HTTPError); ok {
				err = he
				return
			}
			if e, ok := v.(error); ok {
				slog.Error("panic", "err", e, "stack", string(debug.Stack()))
				err = e
				return
			}
			panic(v)
		}
	}()
	// Optional: a workspace on its own (custom) domain
	if org, e := a.orgByDomain(c, c.R.Host); e == nil {
		c.Org = org
	}
	if !o.public {
		if err := a.loadSession(c); err != nil {
			return nil, err
		}
		if o.console && !c.User.PlatformAdmin {
			return nil, Forbidden()
		}
		if c.Org == nil && !o.console && !o.anyOrg {
			return nil, Err(http.StatusConflict, "Open a workspace first (Platform console → Organisations).")
		}
		if c.R.Method != http.MethodGet && c.R.Method != http.MethodHead && !secure.Equal(c.R.Header.Get("X-CSRF"), c.CSRF) {
			return nil, Err(419, "Session expired. Refresh the page.")
		}
		if o.perm != "" && !c.Can(o.perm) {
			return nil, Forbidden()
		}
		if o.module != "" && !c.Mod(o.module) {
			return nil, Err(http.StatusForbidden, "Your plan does not include the "+o.module+" module. Contact us to upgrade your plan.")
		}
	}
	return h(c)
}

// loadSession authenticates the request for the current workspace.
func (a *App) loadSession(c *Ctx) error {
	ck, err := c.R.Cookie(SessionCookie)
	if err != nil || ck.Value == "" {
		return Err(http.StatusUnauthorized, "Please log in.")
	}
	sid := secure.SHA256(ck.Value)
	type row struct {
		User
		OrgID   *int64    `db:"org_id"`
		Member  bool      `db:"member"`
		CSRF    string    `db:"csrf"`
		Expires time.Time `db:"expires_at"`
	}
	// One address for everyone (like Zoho): the workspace comes from the session. Platform admins may
	// hold a console session (no workspace) or open any workspace for support without being a member.
	s, err := db.One[row](c, a.DB.Pool, `SELECT u.id,u.name,u.email,u.phone,u.platform_admin,
  COALESCE(m.role, CASE WHEN u.platform_admin THEN 'super' ELSE '' END) role, m.user_id IS NOT NULL member,
  s.org_id,s.csrf,s.expires_at
FROM sessions s JOIN users u ON u.id=s.user_id
LEFT JOIN memberships m ON m.user_id=s.user_id AND m.org_id=s.org_id AND m.active
WHERE s.id=$1 AND s.expires_at>now() AND u.active AND (m.user_id IS NOT NULL OR u.platform_admin)`, sid)
	if err != nil || (s.OrgID == nil && !s.PlatformAdmin) || (c.Org != nil && (s.OrgID == nil || c.Org.ID != *s.OrgID)) {
		return Err(http.StatusUnauthorized, "Please log in.")
	}
	if c.Org == nil && s.OrgID != nil {
		org, err := a.orgByID(c, *s.OrgID)
		if err != nil {
			return Err(http.StatusForbidden, "This workspace is suspended. Contact support.")
		}
		c.Org = org
	}
	// Sliding expiry: extend when less than half of the lifetime is left
	if time.Until(s.Expires) < SessionTTL/2 {
		a.DB.Pool.Exec(c, `UPDATE sessions SET expires_at=$2 WHERE id=$1`, sid, time.Now().Add(SessionTTL))
	}
	u := s.User
	c.User = &u
	c.CSRF = s.CSRF
	c.Support = c.Org != nil && !s.Member
	return nil
}

const SessionTTL = 7 * 24 * time.Hour

// StartSession creates a session for a user in a workspace and sets the cookie.
// StartSession signs a user in to a workspace (orgID 0 = platform console).
func (a *App) StartSession(c *Ctx, userID, orgID int64) error {
	tok := secure.Token(32)
	csrf := secure.Token(16)
	ua := c.R.UserAgent()
	if len(ua) > 250 {
		ua = ua[:250]
	}
	if _, err := a.DB.Pool.Exec(c, `INSERT INTO sessions (id,user_id,org_id,csrf,ip,ua,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		secure.SHA256(tok), userID, nullID(orgID), csrf, c.IP(), ua, time.Now().Add(SessionTTL)); err != nil {
		return err
	}
	http.SetCookie(c.W, &http.Cookie{Name: SessionCookie, Value: tok, Path: "/", HttpOnly: true, Secure: a.Cfg.Scheme == "https",
		SameSite: http.SameSiteLaxMode, MaxAge: int(SessionTTL.Seconds())})
	c.CSRF = csrf
	return nil
}

// EndSession deletes the current session.
func (a *App) EndSession(c *Ctx) {
	if ck, err := c.R.Cookie(SessionCookie); err == nil {
		a.DB.Pool.Exec(c, `DELETE FROM sessions WHERE id=$1`, secure.SHA256(ck.Value))
	}
	http.SetCookie(c.W, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

// ---- organisation lookup with a short in-memory cache (avoids a query per request)

type orgEntry struct {
	org *Org
	exp time.Time
}

func (a *App) cachedOrg(key string, load func() (*Org, error)) (*Org, error) {
	if v, ok := a.orgs.Load(key); ok {
		if e := v.(*orgEntry); time.Now().Before(e.exp) {
			if e.org == nil {
				return nil, db.ErrNoRows
			}
			return e.org, nil
		}
	}
	o, err := load()
	if err != nil && !errors.Is(err, db.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		o = nil
	}
	a.orgs.Store(key, &orgEntry{org: o, exp: time.Now().Add(30 * time.Second)})
	if o == nil {
		return nil, db.ErrNoRows
	}
	return o, nil
}

func (a *App) orgByDomain(ctx context.Context, host string) (*Org, error) {
	if i := len(host) - 1; i > 0 {
		for j := i; j >= 0; j-- {
			if host[j] == ':' {
				host = host[:j]
				break
			}
		}
	}
	if host == a.Cfg.AppHost || host == "" || host == "localhost" || host == "127.0.0.1" {
		return nil, db.ErrNoRows
	}
	return a.cachedOrg("d:"+host, func() (*Org, error) {
		o, err := db.One[Org](ctx, a.DB.Pool, orgCols+`WHERE o.custom_domain=$1 AND o.status='active'`, host)
		return &o, err
	})
}

func (a *App) orgByID(ctx context.Context, id int64) (*Org, error) {
	return a.cachedOrg("i:"+itoa(id), func() (*Org, error) {
		o, err := db.One[Org](ctx, a.DB.Pool, orgCols+`WHERE o.id=$1 AND o.status='active'`, id)
		return &o, err
	})
}

// ForgetOrg drops cached lookups after a change (rename, suspend…).
func (a *App) ForgetOrg(o *Org) {
	a.orgs.Delete("i:" + itoa(o.ID))
}

func itoa(n int64) string {
	b := [20]byte{}
	i := len(b)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

const orgCols = `SELECT o.id,o.slug,o.name,COALESCE(p.name,'') plan,o.plan_id,o.status,
  COALESCE(o.modules, p.modules, '["crm"]'::jsonb) modules,
  COALESCE(p.limits,'{}'::jsonb) || COALESCE(o.limits,'{}'::jsonb) limits
FROM orgs o LEFT JOIN plans p ON p.id=o.plan_id `

// OrgByID returns the active organisation (cached for 30 s).
func (a *App) OrgByID(ctx context.Context, id int64) (*Org, error) { return a.orgByID(ctx, id) }

// ForgetAllOrgs drops every cached organisation (after a plan change).
func (a *App) ForgetAllOrgs() {
	a.orgs.Range(func(k, _ any) bool { a.orgs.Delete(k); return true })
}
