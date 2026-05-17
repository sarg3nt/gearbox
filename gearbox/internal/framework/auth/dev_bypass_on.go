//go:build dev

// Dev-only loopback auto-login bypass. Compiled in only when the binary
// is built with `-tags dev`. The tag is set by the `dev:` target in
// gearbox/Makefile via `air --build.cmd "$(DEV_BUILD_CMD)"`; the
// production build paths (`make build`, `make deploy-build`) deliberately
// omit it, so this file and its symbols are not in release binaries at
// all. See issue #83.

package auth

import (
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

const (
	// devBypassEnvVar gates whether the bypass is allowed to fire even
	// when the binary was built with `-tags dev`. Set to "1" to enable.
	devBypassEnvVar = "GEARBOX_DEV_AUTO_LOGIN"

	// devBypassEmail is the email/username of the seeded dev account that
	// the bypass auto-authenticates as. The account must exist (and be
	// active) in the users table; the bypass never creates sessions or
	// auto-promotes a non-existent user.
	devBypassEmail = "dev"
)

var devBypassBannerOnce sync.Once

// tryDevBypass returns the seeded `dev` user when ALL of these hold:
//
//  1. The binary was built with `-tags dev` (gearbox/Makefile's `dev:`
//     target adds the tag via `air --build.cmd "$(DEV_BUILD_CMD)"`; this
//     file is compiled in. The production sibling dev_bypass_off.go is
//     compiled in for tag-less builds and provides a no-op stub.).
//  2. GEARBOX_DEV_AUTO_LOGIN=1 is set in the process environment.
//  3. r.RemoteAddr is a loopback address (127.0.0.0/8 or ::1).
//
// In production builds the bypass never enters the binary at all — the
// sibling dev_bypass_off.go provides a hard-coded `nil, false` stub.
func tryDevBypass(m *Manager, r *http.Request) (*models.User, bool) {
	if os.Getenv(devBypassEnvVar) != "1" {
		return nil, false
	}
	if !requestIsLoopback(r) {
		return nil, false
	}
	user, err := m.db.GetUserByEmail(devBypassEmail)
	if err != nil || user == nil {
		m.logger.Warn("dev auto-login: dev user missing from database; bypass inactive",
			"user", devBypassEmail, "error", err)
		return nil, false
	}
	if user.Status != models.UserStatusActive {
		m.logger.Warn("dev auto-login: dev user is not active; bypass inactive",
			"status", user.Status)
		return nil, false
	}
	return user, true
}

// requestIsLoopback reports whether r.RemoteAddr resolves to a loopback IP.
// chi.middleware.RealIP rewrites RemoteAddr from X-Forwarded-For / X-Real-IP,
// so a proxy between the browser and gearbox would surface the proxy's IP
// here, not the browser's — that's the intended behavior: requests routed
// through any proxy aren't loopback and the bypass declines.
func requestIsLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// SeedDevUserIfEnabled creates the `dev` user used by the loopback bypass
// when GEARBOX_DEV_AUTO_LOGIN=1. The user is given an unusable bcrypt hash
// (the package-level dummyPasswordHash) so the form-login path can never
// authenticate as it — only the loopback bypass can.
//
// Production builds replace this with a no-op (dev_bypass_off.go).
func SeedDevUserIfEnabled(db *database.DB, logger *slog.Logger) error {
	if os.Getenv(devBypassEnvVar) != "1" {
		return nil
	}
	created, err := db.EnsureDevUserExists(devBypassEmail, dummyPasswordHash)
	if err != nil {
		return err
	}
	if created {
		logger.Info("dev auto-login: seeded `dev` user for loopback bypass",
			"email", devBypassEmail)
	}
	return nil
}

// LogDevBypassStartupBanner emits a loud warning at startup whenever the
// dev auto-login bypass is potentially active in the running process.
// Designed to be impossible to miss in logs so an operator never confuses
// a dev binary for a production one.
//
// Production builds replace this with a no-op (dev_bypass_off.go) — the
// production build targets in gearbox/Makefile (`build`, `deploy-build`)
// omit `-tags dev`, so this banner cannot fire on a release artifact.
func LogDevBypassStartupBanner(logger *slog.Logger) {
	if os.Getenv(devBypassEnvVar) != "1" {
		logger.Info("dev auto-login: bypass compiled in (`-tags dev`) but disabled — set GEARBOX_DEV_AUTO_LOGIN=1 to enable")
		return
	}
	devBypassBannerOnce.Do(func() {
		logger.Warn("==========================================================")
		logger.Warn("dev auto-login ACTIVE — loopback requests log in as `dev`")
		logger.Warn("DO NOT USE IN PRODUCTION. Rebuild without `-tags dev` to remove entirely.")
		logger.Warn("==========================================================")
	})
}
