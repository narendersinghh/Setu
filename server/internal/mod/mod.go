// Package mod registers every feature module. Each module lives in its own sub-package and
// exposes Register(*app.App).
package mod

import (
	"github.com/narendersinghh/setu/server/internal/app"
	"github.com/narendersinghh/setu/server/internal/mod/ai"
	"github.com/narendersinghh/setu/server/internal/mod/automation"
	"github.com/narendersinghh/setu/server/internal/mod/booking"
	"github.com/narendersinghh/setu/server/internal/mod/capture"
	"github.com/narendersinghh/setu/server/internal/mod/crm"
	"github.com/narendersinghh/setu/server/internal/mod/messaging"
	"github.com/narendersinghh/setu/server/internal/mod/prospecting"
	"github.com/narendersinghh/setu/server/internal/mod/reports"
	"github.com/narendersinghh/setu/server/internal/mod/usage"
)

func Register(a *app.App) {
	crm.Register(a)
	capture.Register(a)
	automation.Register(a)
	booking.Register(a)
	messaging.Register(a)
	ai.Register(a)
	prospecting.Register(a)
	reports.Register(a)
	usage.Register(a)
}
