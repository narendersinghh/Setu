// Package reports: AI growth reports – web report, branded PDF, charts, illustrations, templates.
// OWNER: reports module. Exported functions below are contracts used by other modules – keep signatures.
package reports

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/narendersinghh/setu/server/internal/app"
)

type EnqueueOpts struct {
	PromptID int64 // 0 = active report prompt
	Contact  bool  // send to the business when ready (outreach)
	Bulk     bool  // part of a bulk run (parallel / batch engine)
}

// Enqueue starts writing a report for a lead (job "report").
func Enqueue(ctx context.Context, tx pgx.Tx, orgID, leadID int64, o EnqueueOpts) error {
	return errors.New("reports: not implemented")
}

func Register(a *app.App) {}
