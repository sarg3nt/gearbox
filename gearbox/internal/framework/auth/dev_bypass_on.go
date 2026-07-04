//go:build dev

// Dev-only support for the loopback auto-login bypass. The bypass itself is
// implemented in webcore/core/auth (compiled in only with `-tags dev`, enabled
// only when GEARBOX_DEV_AUTO_LOGIN=1 and the request is loopback); this file
// keeps the gearbox-side pieces: seeding the `dev` account and the startup
// banner. Production builds compile the no-op siblings in dev_bypass_off.go.

package auth

import (
	"log/slog"
	"os"
	"sync"

	"github.com/sarg3nt/gearbox/internal/framework/database"
)

var devBypassBannerOnce sync.Once

// SeedDevUserIfEnabled creates the `dev` user used by the loopback bypass
// when GEARBOX_DEV_AUTO_LOGIN=1. The account gets an unusable random-password
// bcrypt hash so the form-login path can never authenticate as it — only the
// loopback bypass can.
func SeedDevUserIfEnabled(db *database.DB, logger *slog.Logger) error {
	if os.Getenv(devBypassEnvVar) != "1" {
		return nil
	}
	// An unusable-but-valid hash: random 24-char password, immediately
	// discarded. CheckPassword against it can only succeed by guessing the
	// discarded random value.
	pw, err := GenerateRandomPassword()
	if err != nil {
		return err
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	created, err := db.EnsureDevUserExists(devBypassEmail, hash)
	if err != nil {
		return err
	}
	if created {
		logger.Info("dev auto-login: seeded `dev` user for loopback bypass", "email", devBypassEmail)
	}
	return nil
}

// LogDevBypassStartupBanner emits a loud warning at startup whenever the dev
// auto-login bypass is potentially active in the running process.
func LogDevBypassStartupBanner(logger *slog.Logger) {
	if os.Getenv(devBypassEnvVar) != "1" {
		logger.Info("dev auto-login: bypass compiled in (`-tags dev`) but disabled — set " + devBypassEnvVar + "=1 to enable")
		return
	}
	devBypassBannerOnce.Do(func() {
		logger.Warn("==========================================================")
		logger.Warn("dev auto-login ACTIVE — loopback requests log in as `dev`")
		logger.Warn("DO NOT USE IN PRODUCTION. Rebuild without `-tags dev` to remove entirely.")
		logger.Warn("==========================================================")
	})
}
