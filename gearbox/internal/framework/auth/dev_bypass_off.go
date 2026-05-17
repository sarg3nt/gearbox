//go:build !dev

// Production sibling to dev_bypass_on.go. Compiled in for every build
// that does NOT specify `-tags dev`. All entry points are no-ops, so the
// dev auto-login bypass is not present in the resulting binary at all —
// no codepath, no env-var check, no loopback check, nothing to exploit.

package auth

import (
	"log/slog"
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

func tryDevBypass(_ *Manager, _ *http.Request) (*models.User, bool) {
	return nil, false
}

func SeedDevUserIfEnabled(_ *database.DB, _ *slog.Logger) error { return nil }

func LogDevBypassStartupBanner(_ *slog.Logger) {}
