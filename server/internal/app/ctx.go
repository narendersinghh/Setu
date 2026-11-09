package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/settings"
)

type Org struct {
	ID      int64          `db:"id" json:"id"`
	Slug    string         `db:"slug" json:"slug"`
	Name    string         `db:"name" json:"name"`
	Plan    string         `db:"plan" json:"plan"` // plan name
	PlanID  *int64         `db:"plan_id" json:"plan_id"`
	Status  string         `db:"status" json:"status"`
	Modules []string       `db:"modules" json:"modules"` // effective: org override, else plan
	Limits  map[string]int `db:"limits" json:"limits"`   // effective: plan limits merged with org overrides
}

// HasModule reports whether the organisation's plan (or override) includes a module.
func (o *Org) HasModule(m string) bool {
	if m == "crm" || m == "" {
		return true
	}
	for _, x := range o.Modules {
		if x == m {
			return true
		}
	}
	return false
}

type User struct {
	ID            int64  `db:"id" json:"id"`
	Name          string `db:"name" json:"name"`
	Email         string `db:"email" json:"email"`
	Phone         string `db:"phone" json:"phone"`
	Role          string `db:"role" json:"role"`
	PlatformAdmin bool   `db:"platform_admin" json:"platform_admin"`
}

// Handler returns a value that is sent as JSON (nil → {"ok":true}) or an error.
// Handlers that write the response themselves call c.Raw().
type Handler func(c *Ctx) (any, error)

// Ctx is the per-request context. Tenant queries go through c.Tx(): a lazily opened transaction
// scoped to the workspace by RLS, committed after the handler succeeds.
type Ctx struct {
	context.Context
	A    *App
	W    http.ResponseWriter
	R    *http.Request
	Org  *Org
	User *User
	CSRF string
	// Support is true when a platform admin opened this workspace (not a member, hidden from the team).
	Support bool

	tx      pgx.Tx
	set     *settings.Store
	raw     bool
	status  int
	afterFn []func()
}

// Tx returns the tenant transaction (opened on first use).
func (c *Ctx) Tx() pgx.Tx {
	if c.tx == nil {
		if c.Org == nil {
			panic(Err(http.StatusNotFound, "Workspace not found"))
		}
		tx, err := c.A.DB.Begin(c, c.Org.ID)
		if err != nil {
			panic(err)
		}
		c.tx = tx
	}
	return c.tx
}

// S returns the workspace settings (loaded once per request).
func (c *Ctx) S() *settings.Store {
	if c.set == nil {
		s, err := settings.Load(c, c.Tx(), c.A.Box, c.Org.ID)
		if err != nil {
			panic(err)
		}
		c.set = s
	}
	return c.set
}

// SetS saves a setting.
func (c *Ctx) SetS(k string, v any) error { return c.S().Set(c, c.Tx(), k, v) }

// UseOrg switches a public request to the workspace of a token (webhooks, public pages).
func (c *Ctx) UseOrg(orgID int64) error {
	if c.Org != nil && c.Org.ID == orgID {
		return nil
	}
	if c.Org != nil {
		return Err(http.StatusNotFound, "Not found")
	}
	o, err := c.A.orgByID(c, orgID)
	if err != nil {
		return Err(http.StatusNotFound, "Not found")
	}
	c.Org = o
	return nil
}

// UseToken resolves a public token, switches to its workspace and returns its ref id.
func (c *Ctx) UseToken(kind, token string) (int64, error) {
	if token == "" {
		return 0, Err(http.StatusNotFound, "Not found")
	}
	org, ref, err := c.A.ResolveToken(c, kind, token)
	if err != nil {
		return 0, Err(http.StatusNotFound, "Not found")
	}
	return ref, c.UseOrg(org)
}

// After runs fn after the transaction committed (e.g. kick off non-transactional work).
func (c *Ctx) After(fn func()) { c.afterFn = append(c.afterFn, fn) }

// Raw marks the response as written by the handler.
func (c *Ctx) Raw() { c.raw = true }

// Status sets the HTTP status of a JSON response.
func (c *Ctx) Status(code int) { c.status = code }

// ---- input

// Bind decodes the JSON body (max 4 MB).
func (c *Ctx) Bind(v any) error {
	if err := json.NewDecoder(io.LimitReader(c.R.Body, 4<<20)).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return Err(http.StatusBadRequest, "Invalid request: "+err.Error())
	}
	return nil
}

func (c *Ctx) Param(k string) string { return chi.URLParam(c.R, k) }

func (c *Ctx) ParamID(k string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(c.R, k), 10, 64)
	return n
}

func (c *Ctx) Query(k string) string { return strings.TrimSpace(c.R.URL.Query().Get(k)) }

func (c *Ctx) QueryInt(k string, def int) int {
	if n, err := strconv.Atoi(c.Query(k)); err == nil {
		return n
	}
	return def
}

func (c *Ctx) IP() string {
	if v := c.R.Header.Get("CF-Connecting-IP"); v != "" {
		return v
	}
	if v := c.R.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	h := c.R.RemoteAddr
	if i := strings.LastIndexByte(h, ':'); i > 0 {
		h = h[:i]
	}
	return strings.Trim(h, "[]")
}

// ---- roles & access (same model as v1: super, admin, sales)

var Roles = map[string][3]string{
	"super": {"Owner", "purple", "Full control of your organisation: all settings, every user and every lead."},
	"admin": {"Admin / Manager", "blue", "Runs the team: all leads, prospecting, automations, templates and settings. Cannot manage owners."},
	"sales": {"Sales person", "green", "Works their own leads and unassigned leads, books appointments, logs calls and notes."},
}

func (c *Ctx) Role() string {
	if c.User == nil {
		return ""
	}
	return c.User.Role
}

func (c *Ctx) IsSuper() bool { return c.Role() == "super" }

// Can mirrors v1 permissions.
func (c *Ctx) Can(p string) bool {
	switch c.Role() {
	case "super":
		return p != "modules" // modules belong to the plan, set by the platform admin
	case "admin":
		return p != "modules" && p != "super_users"
	case "sales":
		return p == "dashboard" || p == "leads" || p == "appointments" || p == "account"
	}
	return false
}

// Mod reports whether a module is switched on (CRM is always on).
func (c *Ctx) Mod(m string) bool { return c.Org != nil && c.Org.HasModule(m) }

// Scope is the SQL filter so sales people only see their own and unassigned leads.
func (c *Ctx) Scope(alias string) string {
	if c.Role() != "sales" {
		return ""
	}
	return fmt.Sprintf(" AND (%s.assigned_to=%d OR %s.assigned_to IS NULL)", alias, c.User.ID, alias)
}

// LeadOK checks a sales person may open a lead.
func (c *Ctx) LeadOK(isLead bool, assignedTo *int64) bool {
	if c.Role() != "sales" {
		return true
	}
	return isLead && (assignedTo == nil || *assignedTo == c.User.ID)
}

// TZ is the workspace time zone (default Asia/Kolkata).
func (c *Ctx) TZ() *time.Location { return LoadTZ(c.S().Get("timezone", "Asia/Kolkata")) }

func LoadTZ(name string) *time.Location {
	if l, err := time.LoadLocation(name); err == nil {
		return l
	}
	l, _ := time.LoadLocation("Asia/Kolkata")
	if l == nil {
		return time.UTC
	}
	return l
}

// BaseURL is the public address of the app (custom domain when the workspace has one).
func (c *Ctx) BaseURL() string { return c.A.Cfg.AppURL() }

// ---- errors

type HTTPError struct {
	Code int    `json:"-"`
	Msg  string `json:"error"`
}

func (e *HTTPError) Error() string { return e.Msg }

func Err(code int, msg string) *HTTPError { return &HTTPError{Code: code, Msg: msg} }
func Bad(msg string) *HTTPError           { return Err(http.StatusBadRequest, msg) }
func NotFound() *HTTPError                { return Err(http.StatusNotFound, "Not found") }
func Forbidden() *HTTPError               { return Err(http.StatusForbidden, "You do not have access to that.") }
