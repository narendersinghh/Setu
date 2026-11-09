// Package prospecting: Google Maps search, contact scan, website & Maps audits, outreach.
// OWNER: prospecting module. Exported functions below are contracts used by other modules – keep signatures.
package prospecting

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
)

// EnqueueProcess starts the chosen steps ("audit", "report", "contact") for scraped businesses.
func EnqueueProcess(ctx context.Context, a *app.App, tx pgx.Tx, orgID int64, ids []int64, steps []string) (int, error) {
	return 0, errors.New("prospecting: not implemented")
}

func Register(a *app.App) {}
