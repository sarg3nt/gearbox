//go:build !dev

// Production siblings to dev_bypass_on.go — no-ops, so no dev-login codepath
// exists in release binaries. The webcore-side bypass is likewise compiled
// out without `-tags dev`.

package auth

import (
	"log/slog"

	"github.com/sarg3nt/gearbox/internal/framework/database"
)

func SeedDevUserIfEnabled(_ *database.DB, _ *slog.Logger) error { return nil }

func LogDevBypassStartupBanner(_ *slog.Logger) {}
