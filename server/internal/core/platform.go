package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

// Platform (SaaS owner) console: organisations, plans & limits, modules per organisation, branding,
// platform mailer and updates. See docs/V2_PLATFORM.md.

func registerPlatform(a *app.App) {
	a.Get("/public/brand", func(c *app.Ctx) (any, error) {
		b := Brand(c, c.A)
		b["signup"] = c.A.Cfg.SignupOpen
		return b, nil
	}, app.NoAuth())
	a.Get("/public/plans", publicPlans, app.NoAuth())

	p := app.PlatformOnly()
	a.Get("/platform/stats", platformStats, p)
	a.Get("/platform/orgs", platformOrgs, p)
	a.Get("/platform/orgs/{id}", platformOrg, p)
	a.Put("/platform/orgs/{id}", platformOrgUpdate, p)
	a.Post("/platform/orgs/{id}/enter", platformEnter, p)
	a.Post("/platform/console", platformConsole, app.AnyOrg())
	a.Get("/platform/plans", plansList, p)
	a.Post("/platform/plans", planSave, p)
	a.Put("/platform/plans/{id}", planSave, p)
	a.Delete("/platform/plans/{id}", planDelete, p)
	a.Get("/platform/brand", func(c *app.Ctx) (any, error) { return Brand(c, c.A), nil }, p)
	a.Put("/platform/brand", brandSave, p)
	a.Post("/platform/files", platformUpload, p)
	a.Get("/platform/mail", mailGet, p)
	a.Put("/platform/mail", mailSave, p)
	a.Post("/platform/mail/test", mailTest, p)
	a.Get("/platform/admins", adminsList, p)
	a.Post("/platform/admins", adminAdd, p)
	a.Delete("/platform/admins/{id}", adminRemove, p)
	registerUpdater(a)
}

// ---------------------------------------------------------------- platform settings (instance-wide)

var pset struct {
	sync.Mutex
	m  map[string]string
	at time.Time
}

// PSettings returns all platform settings (secrets decrypted). Cached for 30 s.
func PSettings(ctx context.Context, a *app.App) map[string]string {
	pset.Lock()
	defer pset.Unlock()
	if pset.m != nil && time.Since(pset.at) < 30*time.Second {
		return pset.m
	}
	m := map[string]string{}
	rows, err := a.DB.Pool.Query(ctx, `SELECT k, v FROM platform_settings`)
	if err == nil {
		for rows.Next() {
			var k, v string
			if rows.Scan(&k, &v) == nil {
				v, _ = a.Box.Decrypt(0, v)
				m[k] = v
			}
		}
		rows.Close()
	}
	pset.m, pset.at = m, time.Now()
	return m
}

// PGet returns one platform setting or the default.
func PGet(ctx context.Context, a *app.App, k, def string) string {
	if v := PSettings(ctx, a)[k]; v != "" {
		return v
	}
	return def
}

// PSet saves platform settings (secret keys encrypted; the masked placeholder means unchanged).
func PSet(ctx context.Context, a *app.App, kv map[string]string) error {
	for k, v := range kv {
		if settings.IsSecret(k) && v == settings.Masked {
			continue
		}
		stored := v
		if settings.IsSecret(k) {
			stored = a.Box.Encrypt(0, v)
		}
		if _, err := a.DB.Pool.Exec(ctx, `INSERT INTO platform_settings (k,v) VALUES ($1,$2) ON CONFLICT (k) DO UPDATE SET v=EXCLUDED.v`, k, stored); err != nil {
			return err
		}
	}
	pset.Lock()
	pset.m = nil
	pset.Unlock()
	return nil
}

func masked(m map[string]string, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		v := m[k]
		if settings.IsSecret(k) && v != "" {
			v = settings.Masked
		}
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------- branding (white label)

var brandDefaults = map[string]string{
	"brand_name": "Setu", "brand_tagline": "Bridge every lead to conversion", "brand_font_heading": "Plus Jakarta Sans", "brand_font_body": "Inter",
	"brand_primary": "#C21F3A", "brand_accent": "#FFD60A", "brand_success": "#6BA539", "brand_warning": "#F59E0B", "brand_danger": "#E5484D", "brand_info": "#3B82F6",
	"brand_radius": "16", "brand_theme": "system",
}

// Brand is the instance's white-label design (public).
func Brand(ctx context.Context, a *app.App) map[string]any {
	m := PSettings(ctx, a)
	g := func(k string) string {
		if v := m[k]; v != "" {
			return v
		}
		return brandDefaults[k]
	}
	return map[string]any{
		"name": g("brand_name"), "tagline": g("brand_tagline"), "logo": g("brand_logo"), "logo_dark": g("brand_logo_dark"), "favicon": g("brand_favicon"),
		"font_heading": g("brand_font_heading"), "font_body": g("brand_font_body"), "radius": g("brand_radius"), "theme_default": g("brand_theme"),
		"colors":        map[string]string{"primary": g("brand_primary"), "accent": g("brand_accent"), "success": g("brand_success"), "warning": g("brand_warning"), "danger": g("brand_danger"), "info": g("brand_info")},
		"support_email": m["brand_support_email"], "website": m["brand_website"],
	}
}

func brandSave(c *app.Ctx) (any, error) {
	var in map[string]string
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, v := range in {
		if !strings.HasPrefix(k, "brand_") || len(v) > 500 {
			continue
		}
		v = strings.TrimSpace(v)
		if strings.HasSuffix(k, "_primary") || strings.HasSuffix(k, "_accent") || strings.HasSuffix(k, "_success") || strings.HasSuffix(k, "_warning") || strings.HasSuffix(k, "_danger") || strings.HasSuffix(k, "_info") {
			if v != "" && !hexColor.MatchString(v) {
				return nil, app.Bad("Colours must be hex values like #C21F3A.")
			}
		}
		if strings.HasPrefix(k, "brand_font_") && !fontName.MatchString(v) {
			return nil, app.Bad("Font names may contain letters, numbers and spaces only.")
		}
		out[k] = v
	}
	if err := PSet(c, c.A, out); err != nil {
		return nil, err
	}
	return Brand(c, c.A), nil
}

func platformUpload(c *app.Ctx) (any, error) {
	if err := c.R.ParseMultipartForm(6 << 20); err != nil {
		return nil, app.Bad("Upload failed.")
	}
	f, h, err := c.R.FormFile("file")
	if err != nil {
		return nil, app.Bad("Choose a file.")
	}
	defer f.Close()
	if strings.HasSuffix(strings.ToLower(h.Filename), ".svg") {
		return nil, app.Bad("Upload a PNG, JPG or WEBP image.")
	}
	path, err := SaveFile(c, c.A, 0, h.Filename, f, 5<<20)
	if err != nil {
		return nil, err
	}
	if k := c.R.FormValue("setting"); strings.HasPrefix(k, "brand_") {
		if err := PSet(c, c.A, map[string]string{k: path}); err != nil {
			return nil, err
		}
	}
	return map[string]any{"ok": true, "url": path}, nil
}

// ---------------------------------------------------------------- plans

type Plan struct {
	ID          int64          `db:"id" json:"id"`
	Slug        string         `db:"slug" json:"slug"`
	Name        string         `db:"name" json:"name"`
	Description string         `db:"description" json:"description"`
	PriceMonth  float64        `db:"price_month" json:"price_month"`
	PriceYear   float64        `db:"price_year" json:"price_year"`
	Currency    string         `db:"currency" json:"currency"`
	TrialDays   int            `db:"trial_days" json:"trial_days"`
	Features    []string       `db:"features" json:"features"`
	Limits      map[string]int `db:"limits" json:"limits"`
	Modules     []string       `db:"modules" json:"modules"`
	Public      bool           `db:"public" json:"public"`
	IsDefault   bool           `db:"is_default" json:"is_default"`
	Highlight   bool           `db:"highlight" json:"highlight"`
	Sort        int            `db:"sort" json:"sort"`
	Orgs        int            `db:"orgs" json:"orgs"`
}

const planCols = `p.id,p.slug,p.name,p.description,p.price_month::float8 price_month,p.price_year::float8 price_year,p.currency,p.trial_days,p.features,p.limits,p.modules,p.public,p.is_default,p.highlight,p.sort`

func publicPlans(c *app.Ctx) (any, error) {
	return db.All[Plan](c, c.A.DB.Pool, `SELECT `+planCols+` FROM plans p WHERE p.public ORDER BY p.sort, p.price_month`)
}

func plansList(c *app.Ctx) (any, error) {
	ps, err := db.All[Plan](c, c.A.DB.Pool, `SELECT `+planCols+`,(SELECT count(*) FROM orgs o WHERE o.plan_id=p.id AND o.status<>'deleted')::int orgs FROM plans p ORDER BY p.sort, p.price_month`)
	return map[string]any{"plans": ps, "limit_keys": app.LimitKeys, "modules": [][2]string{{"crm", "CRM"}, {"prospecting", "Prospecting"}}}, err
}

func planSave(c *app.Ctx) (any, error) {
	var in Plan
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	in.Name = clean(in.Name, 80)
	if in.Name == "" {
		return nil, app.Bad("Enter a plan name.")
	}
	if in.Slug = slugify(in.Slug); in.Slug == "org" || in.Slug == "" {
		in.Slug = slugify(in.Name)
	}
	if in.Currency == "" {
		in.Currency = "INR"
	}
	mods := []string{"crm"}
	for _, m := range in.Modules {
		if m == "prospecting" {
			mods = append(mods, m)
		}
	}
	lim := map[string]int{}
	for _, k := range app.LimitKeys {
		if v := in.Limits[k[0]]; v > 0 {
			lim[k[0]] = v
		}
	}
	feats := []string{}
	for _, f := range in.Features {
		if f = clean(f, 120); f != "" {
			feats = append(feats, f)
		}
	}
	fj, _ := json.Marshal(feats)
	lj, _ := json.Marshal(lim)
	mj, _ := json.Marshal(mods)
	id := c.ParamID("id")
	err := pgx.BeginFunc(c, c.A.DB.Pool, func(tx pgx.Tx) error {
		if in.IsDefault {
			if _, err := tx.Exec(c, `UPDATE plans SET is_default=false WHERE is_default AND id<>$1`, id); err != nil {
				return err
			}
		}
		args := []any{in.Slug, in.Name, in.Description, in.PriceMonth, in.PriceYear, in.Currency, max(0, in.TrialDays), fj, lj, mj, in.Public, in.IsDefault, in.Highlight, in.Sort}
		if id > 0 {
			_, err := tx.Exec(c, `UPDATE plans SET slug=$1,name=$2,description=$3,price_month=$4,price_year=$5,currency=$6,trial_days=$7,features=$8,limits=$9,modules=$10,public=$11,is_default=$12,highlight=$13,sort=$14 WHERE id=$15`, append(args, id)...)
			return err
		}
		return tx.QueryRow(c, `INSERT INTO plans (slug,name,description,price_month,price_year,currency,trial_days,features,limits,modules,public,is_default,highlight,sort) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, args...).Scan(&id)
	})
	if db.IsUnique(err) {
		return nil, app.Bad("Another plan already uses this short name.")
	}
	if err != nil {
		return nil, err
	}
	c.A.ForgetAllOrgs()
	return map[string]any{"ok": true, "id": id}, nil
}

func planDelete(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	n, _ := db.Val[int](c, c.A.DB.Pool, `SELECT count(*) FROM orgs WHERE plan_id=$1 AND status<>'deleted'`, id)
	if n > 0 {
		return nil, app.Bad(fmt.Sprintf("%d organisation(s) use this plan. Move them to another plan first.", n))
	}
	if d, _ := db.Val[bool](c, c.A.DB.Pool, `SELECT is_default FROM plans WHERE id=$1`, id); d {
		return nil, app.Bad("Make another plan the default first.")
	}
	_, err := c.A.DB.Pool.Exec(c, `DELETE FROM plans WHERE id=$1`, id)
	return nil, err
}

// ---------------------------------------------------------------- organisations

func platformStats(c *app.Ctx) (any, error) {
	st, err := db.Map(c, c.A.DB.Pool, `SELECT
 (SELECT count(*) FROM orgs WHERE status<>'deleted')::int orgs,
 (SELECT count(*) FROM orgs WHERE status='active')::int active,
 (SELECT count(*) FROM orgs WHERE status='suspended')::int suspended,
 (SELECT count(*) FROM orgs WHERE created_at >= date_trunc('month', now()))::int new_month,
 (SELECT count(*) FROM orgs WHERE trial_ends_at > now())::int trials,
 (SELECT count(*) FROM users WHERE NOT platform_admin)::int users,
 (SELECT count(DISTINCT user_id) FROM sessions WHERE created_at > now()-interval '1 day')::int active_users,
 (SELECT count(*) FROM jobs WHERE status='pending')::int jobs_pending,
 (SELECT count(*) FROM jobs WHERE status='failed' AND finished_at > now()-interval '1 day')::int jobs_failed`)
	if err != nil {
		return nil, err
	}
	byPlan, _ := db.Maps(c, c.A.DB.Pool, `SELECT COALESCE(p.name,'No plan') plan, count(*)::int n, COALESCE(sum(p.price_month),0)::float8 mrr
FROM orgs o LEFT JOIN plans p ON p.id=o.plan_id WHERE o.status='active' GROUP BY 1 ORDER BY 2 DESC`)
	signups, _ := db.Maps(c, c.A.DB.Pool, `SELECT to_char(g.d,'YYYY-MM-DD') AS day, (SELECT count(*) FROM orgs o WHERE o.created_at::date=g.d::date)::int AS n
FROM generate_series(current_date-29, current_date, interval '1 day') AS g(d) ORDER BY g.d`)
	return map[string]any{"stats": st, "by_plan": byPlan, "signups": signups}, nil
}

func platformOrgs(c *app.Ctx) (any, error) {
	q := "%" + c.Query("q") + "%"
	return db.Maps(c, c.A.DB.Pool, `SELECT o.id,o.slug,o.name,o.status,o.created_at,o.trial_ends_at,o.plan_expires_at,o.plan_id,COALESCE(p.name,'') plan,
 (SELECT count(*) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.org_id=o.id AND NOT u.platform_admin)::int users,
 (SELECT u.email FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.org_id=o.id AND m.role='super' AND NOT u.platform_admin ORDER BY m.created_at LIMIT 1) owner,
 (SELECT max(s.created_at) FROM sessions s WHERE s.org_id=o.id) last_seen
FROM orgs o LEFT JOIN plans p ON p.id=o.plan_id
WHERE o.status<>'deleted' AND ($1='%%' OR o.name ILIKE $1 OR o.slug ILIKE $1 OR EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.org_id=o.id AND u.email ILIKE $1))
  AND ($2='' OR o.status=$2) AND ($3=0 OR o.plan_id=$3)
ORDER BY o.id DESC LIMIT 500`, q, c.Query("status"), c.QueryInt("plan", 0))
}

func platformOrg(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	o, err := db.Map(c, c.A.DB.Pool, `SELECT o.id,o.slug,o.name,o.status,o.created_at,o.trial_ends_at,o.plan_expires_at,o.plan_id,o.modules,o.limits,o.notes,o.custom_domain,
 COALESCE(p.name,'') plan, COALESCE(p.modules,'["crm"]') plan_modules, COALESCE(p.limits,'{}') plan_limits
FROM orgs o LEFT JOIN plans p ON p.id=o.plan_id WHERE o.id=$1 AND o.status<>'deleted'`, id)
	if err != nil || o == nil {
		return nil, app.NotFound()
	}
	members, _ := db.Maps(c, c.A.DB.Pool, `SELECT u.id,u.name,u.email,m.role,m.active,u.last_login FROM memberships m JOIN users u ON u.id=m.user_id
WHERE m.org_id=$1 AND NOT u.platform_admin ORDER BY m.created_at`, id)
	usage := map[string]int{}
	c.A.DB.Org(c, id, func(tx pgx.Tx) error {
		for _, k := range app.LimitKeys {
			usage[k[0]] = app.Usage(c, tx, k[0])
		}
		return nil
	})
	return map[string]any{"org": o, "members": members, "usage": usage, "limit_keys": app.LimitKeys}, nil
}

func platformOrgUpdate(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	var in struct {
		Name          string          `json:"name"`
		Status        string          `json:"status"`
		PlanID        int64           `json:"plan_id"`
		Modules       *[]string       `json:"modules"` // null = follow the plan
		Limits        *map[string]int `json:"limits"`  // null = follow the plan
		TrialEndsAt   *time.Time      `json:"trial_ends_at"`
		PlanExpiresAt *time.Time      `json:"plan_expires_at"`
		Notes         string          `json:"notes"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if in.Status != "active" && in.Status != "suspended" && in.Status != "deleted" {
		return nil, app.Bad("Unknown status")
	}
	var mods, lims any
	if in.Modules != nil {
		m := []string{"crm"}
		for _, x := range *in.Modules {
			if x == "prospecting" {
				m = append(m, x)
			}
		}
		b, _ := json.Marshal(m)
		mods = string(b)
	}
	if in.Limits != nil {
		l := map[string]int{}
		for _, k := range app.LimitKeys {
			if v, ok := (*in.Limits)[k[0]]; ok && v >= 0 {
				l[k[0]] = v
			}
		}
		b, _ := json.Marshal(l)
		lims = string(b)
	}
	var plan any
	if in.PlanID > 0 {
		plan = in.PlanID
	}
	n, err := db.Exec(c, c.A.DB.Pool, `UPDATE orgs SET name=COALESCE(NULLIF($2,''),name), status=$3, plan_id=COALESCE($4,plan_id), modules=$5::jsonb, limits=$6::jsonb,
 trial_ends_at=$7, plan_expires_at=$8, notes=$9 WHERE id=$1`, id, clean(in.Name, 120), in.Status, plan, mods, lims, in.TrialEndsAt, in.PlanExpiresAt, clean(in.Notes, 4000))
	if err != nil || n == 0 {
		return nil, app.NotFound()
	}
	if in.Status != "active" {
		c.A.DB.Pool.Exec(c, `DELETE FROM sessions WHERE org_id=$1`, id)
	}
	c.A.ForgetAllOrgs()
	return nil, nil
}

// platformEnter opens a workspace for support. The platform admin is not a member and stays hidden.
func platformEnter(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	if _, err := c.A.OrgByID(c, id); err != nil {
		return nil, app.Bad("This organisation is not active.")
	}
	c.A.EndSession(c)
	if err := c.A.StartSession(c, c.User.ID, id); err != nil {
		return nil, err
	}
	slog.Info("platform support access", "admin", c.User.Email, "org", id)
	return map[string]any{"ok": true, "csrf": c.CSRF}, nil
}

// platformConsole returns a platform admin from a workspace to the console.
func platformConsole(c *app.Ctx) (any, error) {
	if !c.User.PlatformAdmin {
		return nil, app.Forbidden()
	}
	c.A.EndSession(c)
	if err := c.A.StartSession(c, c.User.ID, 0); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "csrf": c.CSRF}, nil
}

// ---------------------------------------------------------------- platform admins

func adminsList(c *app.Ctx) (any, error) {
	return db.Maps(c, c.A.DB.Pool, `SELECT id,name,email,last_login FROM users WHERE platform_admin ORDER BY id`)
}

func adminAdd(c *app.Ctx) (any, error) {
	var in struct{ Name, Email, Password string }
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	in.Email = strings.ToLower(clean(in.Email, 190))
	if !validEmail(in.Email) || clean(in.Name, 120) == "" {
		return nil, app.Bad("Enter a name and a valid email.")
	}
	n, _ := db.Val[int](c, c.A.DB.Pool, `SELECT count(*) FROM users u JOIN memberships m ON m.user_id=u.id WHERE u.email=$1`, in.Email)
	if n > 0 {
		return nil, app.Bad("This email belongs to an organisation's user. Use a separate email for platform admins.")
	}
	if len(in.Password) < 10 {
		return nil, app.Bad("Use a password of at least 10 characters.")
	}
	_, err := c.A.DB.Pool.Exec(c, `INSERT INTO users (email,name,password_hash,platform_admin) VALUES ($1,$2,$3,true)
ON CONFLICT (email) DO UPDATE SET platform_admin=true`, in.Email, clean(in.Name, 120), secure.HashPassword(in.Password))
	return nil, err
}

func adminRemove(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	if id == c.User.ID {
		return nil, app.Bad("You cannot remove yourself.")
	}
	_, err := c.A.DB.Pool.Exec(c, `UPDATE users SET platform_admin=false, active=false WHERE id=$1 AND platform_admin`, id)
	c.A.DB.Pool.Exec(c, `DELETE FROM sessions WHERE user_id=$1`, id)
	return nil, err
}

// EnsureAdmin creates (or promotes) the bootstrap platform admin from SETU_ADMIN_EMAIL / SETU_ADMIN_PASSWORD.
func EnsureAdmin(ctx context.Context, a *app.App) error {
	e, pw := a.Cfg.AdminEmail, a.Cfg.AdminPass
	if e == "" {
		return nil
	}
	var id int64
	err := a.DB.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, e).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if len(pw) < 10 {
			return fmt.Errorf("SETU_ADMIN_PASSWORD must be at least 10 characters")
		}
		_, err = a.DB.Pool.Exec(ctx, `INSERT INTO users (email,name,password_hash,platform_admin) VALUES ($1,'Platform admin',$2,true)`, e, secure.HashPassword(pw))
		return err
	}
	if err != nil {
		return err
	}
	_, err = a.DB.Pool.Exec(ctx, `UPDATE users SET platform_admin=true, active=true WHERE id=$1`, id)
	return err
}

// ---------------------------------------------------------------- password reset

func forgot(c *app.Ctx) (any, error) {
	var in struct{ Email string }
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if !c.A.Throttle(c, "forgot:"+c.IP(), 5, 15*time.Minute) {
		return nil, app.Err(http.StatusTooManyRequests, "Too many requests. Try again in a few minutes.")
	}
	var uid int64
	var name string
	if err := c.A.DB.Pool.QueryRow(c, `SELECT id,name FROM users WHERE email=$1 AND active`, strings.ToLower(strings.TrimSpace(in.Email))).Scan(&uid, &name); err == nil && c.A.SystemMail != nil {
		tok := secure.Token(24)
		c.A.DB.Pool.Exec(c, `DELETE FROM password_resets WHERE user_id=$1 OR expires_at<now()`, uid)
		if _, err := c.A.DB.Pool.Exec(c, `INSERT INTO password_resets (token_hash,user_id,expires_at) VALUES ($1,$2,now()+interval '1 hour')`, secure.SHA256(tok), uid); err == nil {
			b := Brand(c, c.A)
			link := c.A.Cfg.AppURL() + "/reset?token=" + tok
			html := fmt.Sprintf(`<p>Hi %s,</p><p>Someone asked to reset your %s password. Click the button below within one hour to choose a new password.</p>
<p><a href="%s" style="display:inline-block;background:%s;color:#fff;padding:12px 22px;border-radius:10px;text-decoration:none;font-weight:600">Reset password</a></p>
<p style="color:#666;font-size:13px">If you did not ask for this, ignore this email – your password stays the same.</p>`, htmlEsc(name), htmlEsc(b["name"].(string)), link, b["colors"].(map[string]string)["primary"])
			to := strings.ToLower(strings.TrimSpace(in.Email))
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				if err := c.A.SystemMail(ctx, to, "Reset your password", html); err != nil {
					slog.Warn("password reset mail", "err", err)
				}
			}()
		}
	}
	// Same answer whether or not the email exists (no account enumeration)
	return map[string]any{"ok": true}, nil
}

func resetPassword(c *app.Ctx) (any, error) {
	var in struct{ Token, Password string }
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if len(in.Password) < 8 {
		return nil, app.Bad("Password must be at least 8 characters.")
	}
	var uid int64
	if err := c.A.DB.Pool.QueryRow(c, `DELETE FROM password_resets WHERE token_hash=$1 AND expires_at>now() RETURNING user_id`, secure.SHA256(in.Token)).Scan(&uid); err != nil {
		return nil, app.Bad("This link has expired. Ask for a new one.")
	}
	if _, err := c.A.DB.Pool.Exec(c, `UPDATE users SET password_hash=$2 WHERE id=$1`, uid, secure.HashPassword(in.Password)); err != nil {
		return nil, err
	}
	c.A.DB.Pool.Exec(c, `DELETE FROM sessions WHERE user_id=$1`, uid)
	return map[string]any{"ok": true}, nil
}

func htmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// ---------------------------------------------------------------- platform mailer (default for every organisation)

// MailConfig is the platform mailer used when an organisation has no own email set-up.
type MailConfig struct {
	Mode                string // sendmail | smtp
	Host, Port, Secure  string
	User, Pass          string
	FromEmail, FromName string
	SendmailPath        string
}

func PlatformMail(ctx context.Context, a *app.App) MailConfig {
	m := PSettings(ctx, a)
	g := func(k, d string) string {
		if v := m[k]; v != "" {
			return v
		}
		return d
	}
	return MailConfig{Mode: g("mail_mode", "sendmail"), Host: m["mail_smtp_host"], Port: g("mail_smtp_port", "465"), Secure: g("mail_smtp_secure", "ssl"),
		User: m["mail_smtp_user"], Pass: m["mail_smtp_pass"], FromEmail: g("mail_from_email", "no-reply@"+hostOnly(a.Cfg.AppHost)),
		FromName: g("mail_from_name", Brand(ctx, a)["name"].(string)), SendmailPath: g("mail_sendmail_path", "/usr/sbin/sendmail")}
}

func hostOnly(h string) string {
	if i := strings.IndexByte(h, ':'); i > 0 {
		return h[:i]
	}
	return h
}

var mailKeys = []string{"mail_mode", "mail_smtp_host", "mail_smtp_port", "mail_smtp_secure", "mail_smtp_user", "mail_smtp_pass", "mail_from_email", "mail_from_name", "mail_sendmail_path"}

func mailGet(c *app.Ctx) (any, error) {
	cfg := PlatformMail(c, c.A)
	out := masked(PSettings(c, c.A), mailKeys...)
	out["mail_mode"], out["mail_from_email"], out["mail_from_name"], out["mail_smtp_port"], out["mail_smtp_secure"], out["mail_sendmail_path"] = cfg.Mode, cfg.FromEmail, cfg.FromName, cfg.Port, cfg.Secure, cfg.SendmailPath
	return out, nil
}

func mailSave(c *app.Ctx) (any, error) {
	var in map[string]string
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, k := range mailKeys {
		if v, ok := in[k]; ok {
			out[k] = strings.TrimSpace(v)
		}
	}
	if m := out["mail_mode"]; m != "" && m != "sendmail" && m != "smtp" {
		return nil, app.Bad("Choose sendmail or SMTP.")
	}
	if p := out["mail_sendmail_path"]; p != "" && !strings.HasPrefix(p, "/") {
		return nil, app.Bad("The sendmail path must be absolute, e.g. /usr/sbin/sendmail.")
	}
	return nil, PSet(c, c.A, out)
}

func mailTest(c *app.Ctx) (any, error) {
	var in struct{ To string }
	c.Bind(&in)
	if in.To == "" {
		in.To = c.User.Email
	}
	if c.A.SystemMail == nil {
		return nil, app.Bad("The mailer is not available.")
	}
	if err := c.A.SystemMail(c, in.To, "Test email from "+Brand(c, c.A)["name"].(string), "<p>Your platform mailer works. Organisations without their own email set-up send through it.</p>"); err != nil {
		return nil, app.Bad("Sending failed: " + err.Error())
	}
	return map[string]any{"ok": true, "message": "Test email sent to " + in.To}, nil
}
