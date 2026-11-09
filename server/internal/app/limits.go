package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/narendersinghh/setu/server/internal/db"
)

// LimitKeys are the plan limits the platform admin can set (missing or 0 = unlimited).
var LimitKeys = [][2]string{
	{"users", "team members"}, {"leads", "leads & scraped businesses"}, {"automations", "automations"},
	{"sources", "lead capture sources"}, {"searches_month", "map searches a month"},
	{"reports_month", "AI reports a month"}, {"emails_month", "emails a month"}, {"calls_month", "AI calls a month"},
}

const month = `date_trunc('month', now())`

// usageSQL counts current usage per key inside a tenant transaction (users use the global memberships).
var usageSQL = map[string]string{
	"users":          `SELECT count(*) FROM memberships WHERE org_id=app_org() AND active`,
	"leads":          `SELECT count(*) FROM leads`,
	"automations":    `SELECT count(*) FROM automations`,
	"sources":        `SELECT count(*) FROM sources`,
	"searches_month": `SELECT count(*) FROM campaigns WHERE created_at >= ` + month,
	"reports_month":  `SELECT (SELECT count(*) FROM reports WHERE created_at >= ` + month + `) + (SELECT count(DISTINCT ref_id) FROM jobs WHERE type='report' AND status IN ('pending','running'))`,
	"emails_month":   `SELECT count(*) FROM messages WHERE channel='email' AND direction='out' AND created_at >= ` + month,
	"calls_month":    `SELECT count(*) FROM calls WHERE created_at >= ` + month,
}

// Usage returns the current usage of one limit key (q must be the organisation's tenant transaction).
func Usage(ctx context.Context, q db.Q, key string) int {
	sql, ok := usageSQL[key]
	if !ok {
		return 0
	}
	n, _ := db.Val[int64](ctx, q, sql)
	return int(n)
}

func limitLabel(key string) string {
	for _, k := range LimitKeys {
		if k[0] == key {
			return k[1]
		}
	}
	return key
}

// Default is the running App (for helpers called without one, e.g. reports.Enqueue).
var Default *App

// Limit is CheckLimit on the running App.
func Limit(ctx context.Context, q db.Q, orgID int64, key string, add int) error {
	if Default == nil {
		return nil
	}
	return Default.CheckLimit(ctx, q, orgID, key, add)
}

// Remaining returns how many more units the plan allows (-1 = unlimited).
func Remaining(ctx context.Context, q db.Q, orgID int64, key string) int {
	if Default == nil {
		return -1
	}
	o, err := Default.orgByID(ctx, orgID)
	if err != nil || o.Limits[key] <= 0 {
		return -1
	}
	return max(0, o.Limits[key]-Usage(ctx, q, key))
}

// CheckLimit refuses an action that would take the organisation over its plan limit (HTTP 402).
// q must be the organisation's tenant transaction.
func (a *App) CheckLimit(ctx context.Context, q db.Q, orgID int64, key string, add int) error {
	o, err := a.orgByID(ctx, orgID)
	if err != nil {
		return nil
	}
	lim := o.Limits[key]
	if lim <= 0 {
		return nil
	}
	if used := Usage(ctx, q, key); used+add > lim {
		plan := o.Plan
		if plan == "" {
			plan = "current"
		}
		return Err(http.StatusPaymentRequired, fmt.Sprintf("Your %s plan includes %d %s (used: %d). Contact us to upgrade your plan.", plan, lim, limitLabel(key), used))
	}
	return nil
}
