// Package crm: leads, pipeline, custom fields, journey, dashboard, CSV import/export.
// OWNER: crm module. Exported functions below are contracts used by other modules – keep signatures.
package crm

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
)

// CreateIn: core lead fields (v1 CRM::CORE) plus custom fields and extra data.
type CreateIn struct {
	Fields map[string]string // name, contact_name, email, phone, alt_phone, website, address, city, category, service, deal_value, message, tags, follow_up_at
	Custom map[string]string // custom field key → value
	Extra  map[string]any    // stored under custom._extra
	Source string            // maps | manual | website | facebook | google | webhook | import | booking
	Detail string            // source detail, e.g. the form name
	UserID int64             // assigned owner (0 = none)
}

// Create adds a lead or merges it into a recent duplicate (same email / phone within 30 days).
// Writes the journey and emits lead_created. Returns the lead id.
func Create(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, in CreateIn) (int64, error) {
	return 0, errors.New("crm: not implemented")
}

// SetStatus changes the CRM status (journey + status_changed).
func SetStatus(ctx context.Context, a *app.App, tx pgx.Tx, orgID, leadID int64, status, note string) error {
	return errors.New("crm: not implemented")
}

// AddTag adds a tag (journey + tag_added).
func AddTag(ctx context.Context, a *app.App, tx pgx.Tx, orgID, leadID int64, tag string) error {
	return errors.New("crm: not implemented")
}

// Promote moves a scraped business into Leads (is_lead) – e.g. when it replies or is won.
func Promote(ctx context.Context, a *app.App, tx pgx.Tx, orgID, leadID int64, reason string) error {
	return errors.New("crm: not implemented")
}

func Register(a *app.App) {}
