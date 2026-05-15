// Package caddy detects a Caddy installation on the host and reports
// it in the capability manifest. Phase 3 only — no metrics collection
// yet; that lands in Phase 7+ (separate issue, #95 followup) and will
// scrape the Prometheus surface this gear already verified.
//
// Caddy is the most ergonomic of the four web servers to add metrics
// for: it ships Prometheus output by default at :2019/metrics, no
// modules to enable, no second config file. The whole detector
// reduces to "is the binary on PATH?" + "does the admin endpoint
// answer with a recognisable metric line?".
//
// The detector declares CategoryHTTPRequests so the agent's
// primary-source resolver (see [gear.ResolvePrimarySources]) considers
// Caddy as a candidate when more than one HTTP source is detected on
// the host.
package caddy

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/probe"
)

func init() {
	gear.Register(New())
}

// defaultAdminURL is where Caddy's admin endpoint listens out of the
// box. Operators with a non-default `admin` directive in their
// Caddyfile override via CADDY_ADMIN_URL.
const defaultAdminURL = "http://127.0.0.1:2019/metrics"

// prometheusSentinel is a metric name Caddy emits whenever the
// `metrics` admin endpoint is mounted. Matching on it (rather than HTTP
// 200 alone) protects against the admin API being mounted on a
// different path that happens to return 200.
const prometheusSentinel = "caddy_http_requests_total"

// versionRegex parses `caddy version` output:
//
//	v2.8.4 h1:q3pe0hpTPqkaN53lp5x3lndR0SBgwh3w29bBeoP5J7s=
//
// Tolerant of the build hash suffix; we only care about the semver.
var versionRegex = regexp.MustCompile(`v(\d+\.\d+\.\d+)`)

// Gear is the Caddy detector.
type Gear struct {
	gear.BaseGear

	// Probe-time indirection — tests swap these to control the
	// outcome without needing Caddy installed on the runner.
	lookPath   func(string) (string, error)
	runVersion func(ctx context.Context) ([]byte, error)
	httpGet    func(ctx context.Context, url string) (probe.HTTPResult, error)
}

// New constructs a Caddy gear with real OS-backed defaults.
func New() *Gear {
	return &Gear{
		lookPath: exec.LookPath,
		runVersion: func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "caddy", "version").CombinedOutput()
		},
		httpGet: func(ctx context.Context, url string) (probe.HTTPResult, error) {
			return probe.HTTPGet(ctx, url, 8192)
		},
	}
}

// Info returns the gear's metadata.
func (g *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "caddy",
		DisplayName: "Caddy",
		Description: "Detects a Caddy installation and verifies its admin/metrics endpoint so the Metrics gear can scrape it in Phase 7+.",
		Version:     "1.0.0",
		Category:    "monitoring",
	}
}

// MetricCategories declares Caddy as an HTTP-requests producer.
func (g *Gear) MetricCategories() []gear.MetricCategory {
	return []gear.MetricCategory{gear.CategoryHTTPRequests}
}

// Probe walks the precedence model:
//  1. CADDY_ADMIN_URL set → trust operator, return Available.
//  2. caddy binary not on PATH → NotInstalled.
//  3. Binary present → probe default admin URL.
//     - 200 with prometheus sentinel → Available.
//     - 200 without sentinel → Inaccessible (admin API may be on a
//     different mount point — operator should set CADDY_ADMIN_URL).
//     - 404 → Inaccessible (admin API disabled in Caddyfile).
//     - connection refused / timeout → Inaccessible.
//
// Unlike nginx/Apache there's no `is the service running` heuristic
// — Caddy under foreground / supervisor / systemd all look the same
// to us. Reachability of the admin API is the only reliable signal,
// which is why this detector is the simplest of the four.
func (g *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
	// Branch 1: operator override.
	if deps.CaddyAdminURL != "" {
		caps := map[string]string{
			"admin_url":       deps.CaddyAdminURL,
			"status_source":   "prometheus",
			"override_source": "env",
		}
		g.recordBinaryFacts(ctx, caps)
		return gear.ProbeAvailable("admin URL configured via CADDY_ADMIN_URL", caps)
	}

	binary, err := g.lookPath("caddy")
	if err != nil {
		return gear.ProbeNotInstalled("no caddy binary on PATH")
	}

	caps := map[string]string{
		"binary_path":   binary,
		"admin_url":     defaultAdminURL,
		"status_source": "prometheus",
	}
	g.recordBinaryFacts(ctx, caps)

	res, err := g.httpGet(ctx, defaultAdminURL)
	if err != nil {
		return gear.ProbeInaccessible(fmt.Sprintf(
			"caddy binary at %s but admin endpoint probe to %s failed: %v — admin endpoint may be disabled in Caddyfile",
			binary, defaultAdminURL, err,
		))
	}
	switch res.StatusCode {
	case http.StatusOK:
		if strings.Contains(res.Body, prometheusSentinel) {
			return gear.ProbeAvailable(
				fmt.Sprintf("admin metrics endpoint reachable at %s", defaultAdminURL),
				caps,
			)
		}
		return gear.ProbeInaccessible(fmt.Sprintf(
			"caddy binary at %s but %s returned 200 without the caddy_http_requests_total metric; admin API may be on a different mount point — set CADDY_ADMIN_URL",
			binary, defaultAdminURL,
		))
	case http.StatusNotFound:
		return gear.ProbeInaccessible(fmt.Sprintf(
			"caddy binary at %s but admin endpoint at %s returns 404; admin may be disabled (admin off in Caddyfile) or mounted at a non-default path",
			binary, defaultAdminURL,
		))
	default:
		return gear.ProbeInaccessible(fmt.Sprintf(
			"caddy binary at %s but admin endpoint at %s returned HTTP %d",
			binary, defaultAdminURL, res.StatusCode,
		))
	}
}

// recordBinaryFacts populates the version fact in caps when
// `caddy version` is available. Best-effort.
func (g *Gear) recordBinaryFacts(ctx context.Context, caps map[string]string) {
	if out, err := g.runVersion(ctx); err == nil {
		if m := versionRegex.FindStringSubmatch(string(out)); len(m) > 1 {
			caps["version"] = m[1]
		}
	}
}

// RegisterRoutes is a no-op — Phase 3 Caddy detection produces no API
// surface. The metrics gear that scrapes Caddy's Prometheus output
// lands in Phase 7+.
func (g *Gear) RegisterRoutes(_ chi.Router) {}

// Ensure the gear implements the required interfaces.
var (
	_ gear.Gear             = (*Gear)(nil)
	_ gear.ProbeableGear    = (*Gear)(nil)
	_ gear.MetricSourceGear = (*Gear)(nil)
)
