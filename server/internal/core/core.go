// Package core: workspace signup, login/sessions, account, team & roles, modules, platform admin.
package core

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/db"
	"github.com/narendersinghh/setu/server/internal/secure"
	"github.com/narendersinghh/setu/server/internal/settings"
)

func Register(a *app.App) {
	a.Post("/signup", signup, app.NoAuth())
	a.Post("/login", login, app.NoAuth())
	a.Post("/logout", logout, app.AnyOrg())
	a.Post("/switch", switchOrg, app.AnyOrg())
	a.Get("/me", me, app.AnyOrg())
	a.Post("/password/forgot", forgot, app.NoAuth())
	a.Post("/password/reset", resetPassword, app.NoAuth())
	a.Get("/workspace", workspace, app.NoAuth())
	a.Put("/account", account, app.AnyOrg())
	a.Get("/team", teamList, app.Perm("team"))
	a.Post("/team", teamSave, app.Perm("team"))
	a.Put("/team/{id}", teamSave, app.Perm("team"))
	a.Delete("/team/{id}", teamDelete, app.Perm("team"))
	registerSettings(a)
	a.Get("/domain-check", domainCheck, app.NoAuth())
	a.Put("/workspace/domain", setDomain, app.Perm("super_users"))
	a.Public(http.MethodGet, "/healthz", func(c *app.Ctx) (any, error) {
		if err := c.A.DB.Pool.Ping(c); err != nil {
			return nil, app.Err(http.StatusServiceUnavailable, "database unavailable")
		}
		return map[string]any{"ok": true}, nil
	})
	registerPlatform(a)
	a.Seed(func(ctx context.Context, tx pgx.Tx, orgID int64, s *settings.Store) error {
		return s.Many(ctx, tx, map[string]any{"timezone": "Asia/Kolkata", "currency": "INR", "currency_symbol": "₹", "default_country_code": "91"})
	})
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func validEmail(e string) bool {
	_, err := mail.ParseAddress(e)
	return err == nil && emailRe.MatchString(e)
}

func clean(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// ---------------------------------------------------------------- signup

type signupIn struct {
	Company  string `json:"company"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Phone    string `json:"phone"`
	PlanID   int64  `json:"plan_id"`
}

func signup(c *app.Ctx) (any, error) {
	if !c.A.Cfg.SignupOpen {
		return nil, app.Err(http.StatusForbidden, "Sign-ups are closed.")
	}
	if !c.A.Throttle(c, "signup:"+c.IP(), 5, time.Hour) {
		return nil, app.Err(http.StatusTooManyRequests, "Too many sign-ups from your network. Try again later.")
	}
	var in signupIn
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	in.Company, in.Name, in.Phone = clean(in.Company, 120), clean(in.Name, 120), clean(in.Phone, 40)
	in.Email = strings.ToLower(clean(in.Email, 190))
	switch {
	case in.Company == "" || in.Name == "":
		return nil, app.Bad("Enter your company and your name.")
	case !validEmail(in.Email):
		return nil, app.Bad("Enter a valid email.")
	case len(in.Password) < 8:
		return nil, app.Bad("Password must be at least 8 characters.")
	}
	var orgID, userID int64
	err := pgx.BeginFunc(c, c.A.DB.Pool, func(tx pgx.Tx) error {
		// Internal short name (used in exports/support); everyone signs in at the same address
		// Plan: a public plan chosen on sign up, else the default plan; trial days start now
		var planID *int64
		var trial int
		if err := tx.QueryRow(c, `SELECT id, trial_days FROM plans WHERE (id=$1 AND public) OR ($1=0 AND is_default) ORDER BY is_default LIMIT 1`, in.PlanID).Scan(&planID, &trial); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var trialEnd *time.Time
		if trial > 0 {
			t := time.Now().AddDate(0, 0, trial)
			trialEnd = &t
		}
		if err := tx.QueryRow(c, `INSERT INTO orgs (slug,name,plan_id,trial_ends_at) VALUES ($1,$2,$3,$4) RETURNING id`, slugify(in.Company)+"-"+secure.Token(3), in.Company, planID, trialEnd).Scan(&orgID); err != nil {
			return err
		}
		var hash string
		var platform bool
		err := tx.QueryRow(c, `SELECT id, password_hash, platform_admin FROM users WHERE email=$1`, in.Email).Scan(&userID, &hash, &platform)
		if err == nil && platform {
			return app.Bad("This email is reserved. Use another email for your workspace.")
		}
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(c, `INSERT INTO users (email,name,password_hash,phone) VALUES ($1,$2,$3,$4) RETURNING id`,
				in.Email, in.Name, secure.HashPassword(in.Password), in.Phone).Scan(&userID); err != nil {
				return err
			}
		case err != nil:
			return err
		case !secure.CheckPassword(hash, in.Password):
			// Existing Setu login: the password must match so nobody can attach an account they do not own
			return app.Bad("This email already has a " + c.A.BrandName(c) + " login. Enter its password to create another workspace.")
		}
		if _, err := tx.Exec(c, `INSERT INTO memberships (org_id,user_id,role) VALUES ($1,$2,'super')`, orgID, userID); err != nil {
			return err
		}
		if err := db.SetOrg(c, tx, orgID); err != nil {
			return err
		}
		s, err := settings.Load(c, tx, c.A.Box, orgID)
		if err != nil {
			return err
		}
		if err := s.Many(c, tx, map[string]any{"company_name": in.Company, "company_email": in.Email, "company_phone": in.Phone, "sender_name": in.Company}); err != nil {
			return err
		}
		return c.A.RunSeeds(c, tx, orgID, s)
	})
	if err != nil {
		return nil, err
	}
	if err := c.A.StartSession(c, userID, orgID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "csrf": c.CSRF}, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 30 {
		s = strings.Trim(s[:30], "-")
	}
	if s == "" {
		s = "org"
	}
	return s
}

// ---------------------------------------------------------------- login

type creds struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userRow struct {
	ID       int64  `db:"id"`
	Hash     string `db:"password_hash"`
	Platform bool   `db:"platform_admin"`
	Active   bool   `db:"active"`
}

func checkCreds(c *app.Ctx, in creds) (*userRow, error) {
	key := "login:" + c.IP()
	if !c.A.Throttle(c, key+":chk", 10, 15*time.Minute) {
		return nil, app.Err(http.StatusTooManyRequests, "Too many failed attempts. Please wait 15 minutes and try again.")
	}
	u, err := db.One[userRow](c, c.A.DB.Pool, `SELECT id,password_hash,active,platform_admin FROM users WHERE email=$1`, strings.ToLower(strings.TrimSpace(in.Email)))
	if err != nil || !secure.CheckPassword(u.Hash, in.Password) {
		return nil, app.Err(http.StatusUnauthorized, "Invalid email or password.")
	}
	c.A.DB.Pool.Exec(c, `DELETE FROM throttle WHERE k=$1`, key+":chk")
	if secure.NeedsRehash(u.Hash) {
		c.A.DB.Pool.Exec(c, `UPDATE users SET password_hash=$2 WHERE id=$1`, u.ID, secure.HashPassword(in.Password))
	}
	if !u.Active {
		return nil, app.Err(http.StatusForbidden, "Your account has been deactivated.")
	}
	return &u, nil
}

type workspaceRow struct {
	ID   int64  `db:"id" json:"id"`
	Name string `db:"name" json:"name"`
	Role string `db:"role" json:"role"`
}

func workspacesOf(c *app.Ctx, uid int64) ([]workspaceRow, error) {
	return db.All[workspaceRow](c, c.A.DB.Pool, `SELECT o.id,o.name,m.role FROM memberships m JOIN orgs o ON o.id=m.org_id
WHERE m.user_id=$1 AND m.active AND o.status='active' ORDER BY o.name`, uid)
}

// login: one address for everyone. A person in several workspaces picks one (org_id).
func login(c *app.Ctx) (any, error) {
	var in struct {
		creds
		OrgID int64 `json:"org_id"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	u, err := checkCreds(c, in.creds)
	if err != nil {
		return nil, err
	}
	if u.Platform && c.Org == nil {
		if err := c.A.StartSession(c, u.ID, 0); err != nil {
			return nil, err
		}
		c.A.DB.Pool.Exec(c, `UPDATE users SET last_login=now() WHERE id=$1`, u.ID)
		return map[string]any{"ok": true, "csrf": c.CSRF, "console": true}, nil
	}
	ws, err := workspacesOf(c, u.ID)
	if err != nil {
		return nil, err
	}
	if c.Org != nil { // custom domain: only that workspace
		in.OrgID = c.Org.ID
	}
	var pick int64
	for _, w := range ws {
		if w.ID == in.OrgID || (in.OrgID == 0 && len(ws) == 1) {
			pick = w.ID
		}
	}
	if pick == 0 {
		if len(ws) == 0 || in.OrgID != 0 {
			return nil, app.Err(http.StatusForbidden, "Your account has no active workspace. Ask your admin to switch it on.")
		}
		return map[string]any{"ok": false, "choose": ws}, nil
	}
	if err := c.A.StartSession(c, u.ID, pick); err != nil {
		return nil, err
	}
	c.A.DB.Pool.Exec(c, `UPDATE users SET last_login=now() WHERE id=$1`, u.ID)
	return map[string]any{"ok": true, "csrf": c.CSRF}, nil
}

// switchOrg moves the signed-in person to another of their workspaces (new session).
func switchOrg(c *app.Ctx) (any, error) {
	var in struct {
		OrgID int64 `json:"org_id"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	ws, err := workspacesOf(c, c.User.ID)
	if err != nil {
		return nil, err
	}
	for _, w := range ws {
		if w.ID == in.OrgID {
			c.A.EndSession(c)
			if err := c.A.StartSession(c, c.User.ID, w.ID); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true, "csrf": c.CSRF}, nil
		}
	}
	return nil, app.Forbidden()
}

func logout(c *app.Ctx) (any, error) { c.A.EndSession(c); return nil, nil }

// workspace: public branding of the workspace for the login page.
func workspace(c *app.Ctx) (any, error) {
	if c.Org == nil {
		return map[string]any{"custom": false, "signup": c.A.Cfg.SignupOpen}, nil
	}
	return map[string]any{"custom": true, "signup": false, "name": c.Org.Name, "logo": c.S().Get("logo_light", "")}, nil
}

func me(c *app.Ctx) (any, error) {
	brand := Brand(c, c.A)
	if c.Org == nil { // platform console session
		return map[string]any{"user": c.User, "org": nil, "workspaces": []any{}, "csrf": c.CSRF, "perms": []string{}, "modules": map[string]bool{},
			"roles": app.Roles, "settings": map[string]string{}, "brand": brand, "console": true}, nil
	}
	perms := []string{}
	for _, p := range []string{"dashboard", "leads", "appointments", "account", "prospecting", "automations", "templates", "settings", "team", "super_users"} {
		if c.Can(p) {
			perms = append(perms, p)
		}
	}
	s := c.S()
	ws, err := workspacesOf(c, c.User.ID)
	if err != nil {
		return nil, err
	}
	// Plan usage for keys that have a limit
	limits := map[string]any{}
	for _, k := range app.LimitKeys {
		if lim := c.Org.Limits[k[0]]; lim > 0 {
			limits[k[0]] = map[string]any{"limit": lim, "used": app.Usage(c, c.Tx(), k[0]), "label": k[1]}
		}
	}
	var trialEnd, expires *time.Time
	c.A.DB.Pool.QueryRow(c, `SELECT trial_ends_at, plan_expires_at FROM orgs WHERE id=$1`, c.Org.ID).Scan(&trialEnd, &expires)
	org := map[string]any{"id": c.Org.ID, "slug": c.Org.Slug, "name": c.Org.Name, "status": c.Org.Status, "plan": map[string]any{"id": c.Org.PlanID, "name": c.Org.Plan},
		"plan_status": c.Org.PlanStatus, "trial_ends_at": trialEnd, "plan_expires_at": expires}
	return map[string]any{
		"user": c.User, "org": org, "workspaces": ws, "csrf": c.CSRF, "perms": perms, "support": c.Support,
		"modules":  map[string]bool{"crm": true, "prospecting": c.Mod("prospecting")},
		"limits":   limits,
		"brand":    brand,
		"billing":  len(Gateways(c, c.A)) > 0,
		"roles":    app.Roles,
		"settings": map[string]string{"company_name": s.Get("company_name", c.Org.Name), "currency": s.Get("currency", "INR"), "currency_symbol": s.Get("currency_symbol", "₹"), "timezone": s.Get("timezone", "Asia/Kolkata"), "default_country_code": s.Get("default_country_code", "91"), "logo_dark": s.Get("company_logo", ""), "logo_light": s.Get("company_logo_light", "")},
	}, nil
}

// ---------------------------------------------------------------- account

func account(c *app.Ctx) (any, error) {
	var in struct {
		Name, Email, Phone, Old, New, New2 string
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	var hash string
	if err := c.A.DB.Pool.QueryRow(c, `SELECT password_hash FROM users WHERE id=$1`, c.User.ID).Scan(&hash); err != nil {
		return nil, err
	}
	if !secure.CheckPassword(hash, in.Old) {
		return nil, app.Bad("Your current password is not correct.")
	}
	in.Name, in.Phone, in.Email = clean(in.Name, 120), clean(in.Phone, 40), strings.ToLower(clean(in.Email, 190))
	if in.Name == "" || !validEmail(in.Email) {
		return nil, app.Bad("Enter your name and a valid email.")
	}
	if in.New != "" {
		if len(in.New) < 8 {
			return nil, app.Bad("New password must be at least 8 characters.")
		}
		if in.New != in.New2 {
			return nil, app.Bad("The two new passwords do not match.")
		}
		hash = secure.HashPassword(in.New)
		// Sign out every other session of this user
		if ck, err := c.R.Cookie(app.SessionCookie); err == nil {
			c.A.DB.Pool.Exec(c, `DELETE FROM sessions WHERE user_id=$1 AND id<>$2`, c.User.ID, secure.SHA256(ck.Value))
		}
	}
	_, err := c.A.DB.Pool.Exec(c, `UPDATE users SET name=$2,email=$3,phone=$4,password_hash=$5 WHERE id=$1`, c.User.ID, in.Name, in.Email, in.Phone, hash)
	if db.IsUnique(err) {
		return nil, app.Bad("Another user already has this email.")
	}
	return nil, err
}

// ---------------------------------------------------------------- team

type member struct {
	ID        int64      `db:"id" json:"id"`
	Name      string     `db:"name" json:"name"`
	Email     string     `db:"email" json:"email"`
	Phone     string     `db:"phone" json:"phone"`
	Role      string     `db:"role" json:"role"`
	Active    bool       `db:"active" json:"active"`
	LastLogin *time.Time `db:"last_login" json:"last_login"`
	Shared    bool       `db:"shared" json:"shared"` // also member of other workspaces (profile is read-only)
	OpenN     int        `db:"open_n" json:"open_n"`
	WonN      int        `db:"won_n" json:"won_n"`
}

func teamList(c *app.Ctx) (any, error) {
	ms, err := db.All[member](c, c.Tx(), `SELECT u.id,u.name,u.email,u.phone,m.role,m.active,u.last_login,
  EXISTS(SELECT 1 FROM memberships x WHERE x.user_id=u.id AND x.org_id<>m.org_id) shared,
  (SELECT count(*) FROM leads l WHERE l.assigned_to=u.id AND l.is_lead AND l.status NOT IN ('won','lost')) open_n,
  (SELECT count(*) FROM leads l WHERE l.assigned_to=u.id AND l.status='won') won_n
FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.org_id=$1 AND NOT u.platform_admin
ORDER BY m.active DESC, array_position(ARRAY['super','admin','sales'], m.role), u.name`, c.Org.ID)
	return map[string]any{"members": ms, "me": c.User.ID}, err
}

func supersLeft(c *app.Ctx, except int64) int {
	n, _ := db.Val[int](c, c.Tx(), `SELECT count(*) FROM memberships WHERE org_id=$1 AND role='super' AND active AND user_id<>$2`, c.Org.ID, except)
	return n
}

func teamSave(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	var in struct {
		Name, Email, Phone, Role, Password string
		Active, Release                    bool
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	if id == c.User.ID {
		return nil, app.Bad("Edit your own profile under My account.")
	}
	allowed := map[string]bool{"admin": true, "sales": true, "super": c.IsSuper()}
	if !allowed[in.Role] {
		in.Role = "sales"
	}
	in.Name, in.Phone, in.Email = clean(in.Name, 120), clean(in.Phone, 40), strings.ToLower(clean(in.Email, 190))
	tx := c.Tx()
	if id > 0 {
		var role string
		var shared bool
		if err := tx.QueryRow(c, `SELECT m.role, EXISTS(SELECT 1 FROM memberships x WHERE x.user_id=m.user_id AND x.org_id<>m.org_id) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.org_id=$1 AND m.user_id=$2 AND NOT u.platform_admin`, c.Org.ID, id).Scan(&role, &shared); err != nil {
			return nil, app.NotFound()
		}
		if role == "super" && !c.IsSuper() {
			return nil, app.Err(http.StatusForbidden, "Only a super admin can change another super admin.")
		}
		if role == "super" && (in.Role != "super" || !in.Active) && supersLeft(c, id) == 0 {
			return nil, app.Bad("Keep at least one active super admin.")
		}
		if _, err := tx.Exec(c, `UPDATE memberships SET role=$3, active=$4 WHERE org_id=$1 AND user_id=$2`, c.Org.ID, id, in.Role, in.Active); err != nil {
			return nil, err
		}
		// Profile and password belong to the person; another workspace's admin must not change them
		if !shared {
			if in.Name == "" || !validEmail(in.Email) {
				return nil, app.Bad("Enter a name and a valid email.")
			}
			q, args := `UPDATE users SET name=$2,email=$3,phone=$4`, []any{id, in.Name, in.Email, in.Phone}
			if in.Password != "" {
				if len(in.Password) < 8 {
					return nil, app.Bad("Password must be at least 8 characters.")
				}
				q += `,password_hash=$5`
				args = append(args, secure.HashPassword(in.Password))
			}
			if _, err := tx.Exec(c, q+` WHERE id=$1`, args...); err != nil {
				if db.IsUnique(err) {
					return nil, app.Bad("Another user already has this email.")
				}
				return nil, err
			}
		}
		if !in.Active {
			tx.Exec(c, `DELETE FROM sessions WHERE user_id=$1 AND org_id=$2`, id, c.Org.ID)
			if in.Release {
				tx.Exec(c, `UPDATE leads SET assigned_to=NULL WHERE assigned_to=$1 AND status NOT IN ('won','lost')`, id)
			}
		}
		return map[string]any{"ok": true, "id": id}, nil
	}
	if in.Name == "" || !validEmail(in.Email) {
		return nil, app.Bad("Enter a name and a valid email.")
	}
	if err := c.A.CheckLimit(c, tx, c.Org.ID, "users", 1); err != nil {
		return nil, err
	}
	var uid int64
	var platform bool
	err := tx.QueryRow(c, `SELECT id, platform_admin FROM users WHERE email=$1`, in.Email).Scan(&uid, &platform)
	if err == nil && platform {
		return nil, app.Bad("This email cannot be added. Use another email.")
	}
	existing := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		if len(in.Password) < 8 {
			return nil, app.Bad("Set a password for the new user (8+ characters).")
		}
		if err := tx.QueryRow(c, `INSERT INTO users (email,name,phone,password_hash) VALUES ($1,$2,$3,$4) RETURNING id`, in.Email, in.Name, in.Phone, secure.HashPassword(in.Password)).Scan(&uid); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(c, `INSERT INTO memberships (org_id,user_id,role,active) VALUES ($1,$2,$3,true)`, c.Org.ID, uid, in.Role); err != nil {
		if db.IsUnique(err) {
			return nil, app.Bad("This person is already in your team.")
		}
		return nil, err
	}
	msg := in.Name + " added. Share the sign-in link and password with them."
	if existing {
		msg = in.Email + " already has a " + c.A.BrandName(c) + " login – they can sign in with their existing password."
	}
	return map[string]any{"ok": true, "id": uid, "message": msg}, nil
}

func teamDelete(c *app.Ctx) (any, error) {
	id := c.ParamID("id")
	if id == c.User.ID {
		return nil, app.Bad("You cannot remove yourself.")
	}
	tx := c.Tx()
	var role string
	if err := tx.QueryRow(c, `SELECT m.role FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.org_id=$1 AND m.user_id=$2 AND NOT u.platform_admin`, c.Org.ID, id).Scan(&role); err != nil {
		return nil, app.NotFound()
	}
	if role == "super" && (!c.IsSuper() || supersLeft(c, id) == 0) {
		return nil, app.Bad("You cannot remove the last super admin.")
	}
	tx.Exec(c, `UPDATE leads SET assigned_to=NULL WHERE assigned_to=$1`, id)
	tx.Exec(c, `DELETE FROM sessions WHERE user_id=$1 AND org_id=$2`, id, c.Org.ID)
	_, err := tx.Exec(c, `DELETE FROM memberships WHERE org_id=$1 AND user_id=$2`, c.Org.ID, id)
	// Remove the login entirely when the person belongs to no other workspace
	tx.Exec(c, `DELETE FROM users u WHERE u.id=$1 AND NOT EXISTS (SELECT 1 FROM memberships m WHERE m.user_id=u.id)`, id)
	return nil, err
}

// ---------------------------------------------------------------- custom domains (optional)

// domainCheck is asked by Caddy before issuing a certificate on demand.
func domainCheck(c *app.Ctx) (any, error) {
	d := strings.ToLower(strings.TrimSpace(c.Query("domain")))
	n, _ := db.Val[int](c, c.A.DB.Pool, `SELECT count(*) FROM orgs WHERE custom_domain=$1 AND status='active'`, d)
	if n == 0 {
		return nil, app.NotFound()
	}
	return nil, nil
}

var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)

// setDomain lets a super admin open Setu on the workspace's own domain (CNAME to the app host).
func setDomain(c *app.Ctx) (any, error) {
	var in struct {
		Domain string `json:"domain"`
	}
	if err := c.Bind(&in); err != nil {
		return nil, err
	}
	d := strings.ToLower(strings.TrimSpace(in.Domain))
	var v any
	if d != "" {
		if !domainRe.MatchString(d) || d == c.A.Cfg.AppHost {
			return nil, app.Bad("Enter a domain like crm.yourcompany.com")
		}
		v = d
	}
	if _, err := c.A.DB.Pool.Exec(c, `UPDATE orgs SET custom_domain=$2 WHERE id=$1`, c.Org.ID, v); err != nil {
		if db.IsUnique(err) {
			return nil, app.Bad("This domain is already used by another workspace.")
		}
		return nil, err
	}
	return map[string]any{"ok": true, "cname": c.A.Cfg.AppHost}, nil
}
