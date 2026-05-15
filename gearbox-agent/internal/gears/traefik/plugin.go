// Package traefik detects a Traefik installation on the host and reports
// it in the capability manifest. Phase 3 only — no metrics collection
// yet; that lands in Phase 7+ (separate issue, #95 followup) and will
// scrape the Prometheus surface this gear already verified.
//
// Traefik's metrics surface address is operator-configurable (typically
// `:8082/metrics` from the static config, but some deployments wire it
// onto the dashboard API entrypoint at `:8080/metrics`). The detector
// tries both before giving up, since "Traefik is installed but the
// metrics entrypoint is on the other default" is the common case for
// new installs.
//
// The detector declares CategoryHTTPRequests so the agent's
// primary-source resolver (see [gear.ResolvePrimarySources]) considers
// Traefik as a candidate when more than one HTTP source is detected on
// the host.
package traefik

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

// defaultMetricsURLs is the fallback chain searched when the operator
// hasn't set TRAEFIK_METRICS_URL. Order matters: 8082 first because
// that's the conventional `metrics.entryPoint = "metrics"` address
// from the docs; 8080 catches setups that bind Prometheus on the
// dashboard API entrypoint instead.
var defaultMetricsURLs = []string{
	"http://127.0.0.1:8082/metrics",
	"http://127.0.0.1:8080/metrics",
}

// prometheusSentinel is a Traefik-specific metric name. Picking one
// that's clearly namespaced ("traefik_") protects against a non-Traefik
// Prometheus exporter occupying one of the fallback addresses.
const prometheusSentinel = "traefik_router_requests_total"

// dashboardAPIURL is the rawdata endpoint Phase 7 will read for the
// router/service breakdown view. We probe it here only to note its
// presence in capabilities — connection refused here doesn't gate
// Availability.
const dashboardAPIURL = "http://127.0.0.1:8080/api/rawdata"

// versionRegex parses `traefik version` output:
//
//	Version:      v3.1.2
//	Codename:     beaufort
//
// The `v` prefix is optional; older builds printed plain semver.
var versionRegex = regexp.MustCompile(`Version:\s+v?(\d+\.\d+\.\d+)`)

// Gear is the Traefik detector.
type Gear struct {
	gear.BaseGear

	// Probe-time indirection — tests swap these to control the
	// outcome without needing Traefik installed on the runner.
	lookPath   func(string) (string, error)
	runVersion func(ctx context.Context) ([]byte, error)
	httpGet    func(ctx context.Context, url string) (probe.HTTPResult, error)
}

// New constructs a Traefik gear with real OS-backed defaults.
func New() *Gear {
	return &Gear{
		lookPath: exec.LookPath,
		runVersion: func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "traefik", "version").CombinedOutput()
		},
		httpGet: func(ctx context.Context, url string) (probe.HTTPResult, error) {
			return probe.HTTPGet(ctx, url, 8192)
		},
	}
}

// Info returns the gear's metadata.
func (g *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "traefik",
		DisplayName: "Traefik",
		Description: "Detects a Traefik installation and verifies its Prometheus metrics endpoint so the Metrics gear can scrape it in Phase 7+.",
		Version:     "1.0.0",
		Category:    "monitoring",
	}
}

// MetricCategories declares Traefik as an HTTP-requests producer.
func (g *Gear) MetricCategories() []gear.MetricCategory {
	return []gear.MetricCategory{gear.CategoryHTTPRequests}
}

// Probe walks the precedence model:
//  1. TRAEFIK_METRICS_URL set → trust operator, return Available.
//  2. traefik binary not on PATH → NotInstalled.
//  3. Binary present → try defaultMetricsURLs in order.
//     - First URL that returns 200 with the traefik_ sentinel wins
//     → Available.
//     - All defaults exhausted → Inaccessible, naming both URLs in
//     the reason so the operator can see what we tried.
func (g *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
	// Branch 1: operator override.
	if deps.TraefikMetricsURL != "" {
		caps := map[string]string{
			"metrics_url":     deps.TraefikMetricsURL,
			"status_source":   "prometheus",
			"override_source": "env",
		}
		g.recordBinaryFacts(ctx, caps)
		g.recordDashboardAPI(ctx, caps)
		return gear.ProbeAvailable("metrics URL configured via TRAEFIK_METRICS_URL", caps)
	}

	binary, err := g.lookPath("traefik")
	if err != nil {
		return gear.ProbeNotInstalled("no traefik binary on PATH")
	}

	caps := map[string]string{
		"binary_path":   binary,
		"status_source": "prometheus",
	}
	g.recordBinaryFacts(ctx, caps)
	g.recordDashboardAPI(ctx, caps)

	// Track the last response that wasn't a sentinel match so the
	// inaccessible reason can distinguish three operator-actionable
	// states: nothing-listening (all attempts errored), wrong-service-
	// on-this-port (200 without sentinel), and non-200 status code.
	// Without this, the message would always read "no metrics endpoint
	// reachable" even when a perfectly happy non-Traefik server was
	// answering on :8080 — which leads operators to debug the wrong
	// thing.
	var lastNonMatch string
	for _, url := range defaultMetricsURLs {
		res, err := g.httpGet(ctx, url)
		if err != nil {
			continue
		}
		if res.StatusCode == http.StatusOK && strings.Contains(res.Body, prometheusSentinel) {
			caps["metrics_url"] = url
			return gear.ProbeAvailable(
				fmt.Sprintf("metrics endpoint reachable at %s", url),
				caps,
			)
		}
		if res.StatusCode == http.StatusOK {
			lastNonMatch = fmt.Sprintf("%s returned 200 but body lacked the Traefik sentinel (likely a different service on this port)", url)
		} else {
			lastNonMatch = fmt.Sprintf("%s returned HTTP %d", url, res.StatusCode)
		}
	}

	reason := fmt.Sprintf(
		"traefik binary at %s but no metrics endpoint reachable at %s — enable the Prometheus exporter under '[metrics.prometheus]' or set TRAEFIK_METRICS_URL",
		binary, strings.Join(defaultMetricsURLs, " or "),
	)
	if lastNonMatch != "" {
		reason = fmt.Sprintf("%s (last attempt: %s)", reason, lastNonMatch)
	}
	return gear.ProbeInaccessible(reason)
}

// recordBinaryFacts populates the version fact in caps. Best-effort.
func (g *Gear) recordBinaryFacts(ctx context.Context, caps map[string]string) {
	if out, err := g.runVersion(ctx); err == nil {
		if m := versionRegex.FindStringSubmatch(string(out)); len(m) > 1 {
			caps["version"] = m[1]
		}
	}
}

// recordDashboardAPI notes the dashboard API's presence so Phase 7 can
// query the router/service breakdown without re-detecting. Failure
// here doesn't affect the verdict — many deploys disable the dashboard
// for security and still expose Prometheus, which is what we actually
// need.
func (g *Gear) recordDashboardAPI(ctx context.Context, caps map[string]string) {
	res, err := g.httpGet(ctx, dashboardAPIURL)
	if err != nil || res.StatusCode != http.StatusOK {
		return
	}
	caps["dashboard_api"] = dashboardAPIURL
}

// RegisterRoutes is a no-op — Phase 3 Traefik detection produces no API
// surface. The metrics gear lands in Phase 7+.
func (g *Gear) RegisterRoutes(_ chi.Router) {}

// Ensure the gear implements the required interfaces.
var (
	_ gear.Gear             = (*Gear)(nil)
	_ gear.ProbeableGear    = (*Gear)(nil)
	_ gear.MetricSourceGear = (*Gear)(nil)
)
