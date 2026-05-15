// Package nginx detects an nginx installation on the host and reports
// it in the capability manifest. Phase 3 only — no metrics collection
// yet; that lands in Phase 4 (separate issue, #95 followup) and will
// read the `stub_status` endpoint surface this gear already verified.
//
// The detector declares CategoryHTTPRequests so the agent's
// primary-source resolver (see [gear.ResolvePrimarySources]) considers
// nginx as a candidate when both HAProxy and nginx are installed on the
// same host. Operators force the choice with GEARBOX_AGENT_HTTP_SOURCE.
package nginx

import (
	"context"
	"fmt"
	"net/http"
	"os"
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

// Well-known nginx.conf locations searched in order when neither
// NGINX_CONFIG_FILE nor `nginx -V` reveals the path. Order matters:
// /etc/nginx is the modern Linux default; the others handle older
// installs and BSD-style layouts. Harmless to keep all four.
var wellKnownConfigPaths = []string{
	"/etc/nginx/nginx.conf",
	"/usr/local/nginx/conf/nginx.conf",
	"/usr/local/etc/nginx/nginx.conf",
}

// defaultStatusURL is the loopback URL we expect a vanilla stub_status
// to live at. Operators with different `location` paths or hosts
// override via NGINX_STATUS_URL.
const defaultStatusURL = "http://127.0.0.1/nginx_status"

// stubStatusSentinel is the first line of every stub_status response.
// Matching on it (rather than HTTP 200 alone) protects against a
// catch-all virtual host returning 200 with a different body.
const stubStatusSentinel = "Active connections:"

// versionRegex parses `nginx -v` stderr output:
//
//	nginx version: nginx/1.27.0 (Ubuntu)
//
// Tolerant of build-info suffixes in parentheses.
var versionRegex = regexp.MustCompile(`nginx/(\d+\.\d+\.\d+)`)

// confPathRegex pulls --conf-path=PATH out of `nginx -V 2>&1`. Source
// of truth for the config location when the agent doesn't find it in
// a well-known place — useful for source-built nginx with non-standard
// prefixes.
var confPathRegex = regexp.MustCompile(`--conf-path=([^\s]+)`)

// apiModuleSentinel is what `nginx -V` prints in --with-http_api_module
// builds (open-source 1.19+, Plus). Phase 4 may prefer the JSON API
// over stub_status when available; Phase 3 just records the fact.
const apiModuleSentinel = "--with-http_api_module"

// Gear is the nginx detector.
type Gear struct {
	gear.BaseGear

	// Probe-time indirection — tests swap these to control the
	// outcome without needing nginx installed on the runner.
	lookPath  func(string) (string, error)
	runShortV func(ctx context.Context) ([]byte, error) // nginx -v
	runLongV  func(ctx context.Context) ([]byte, error) // nginx -V
	stat      func(string) (os.FileInfo, error)
	httpGet   func(ctx context.Context, url string) (probe.HTTPResult, error)
}

// New constructs an nginx gear with real OS-backed defaults.
func New() *Gear {
	return &Gear{
		lookPath: exec.LookPath,
		runShortV: func(ctx context.Context) ([]byte, error) {
			// nginx writes -v / -V to stderr; CombinedOutput captures both.
			return exec.CommandContext(ctx, "nginx", "-v").CombinedOutput()
		},
		runLongV: func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "nginx", "-V").CombinedOutput()
		},
		stat: os.Stat,
		httpGet: func(ctx context.Context, url string) (probe.HTTPResult, error) {
			return probe.HTTPGet(ctx, url, 4096)
		},
	}
}

// Info returns the gear's metadata.
func (g *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "nginx",
		DisplayName: "nginx",
		Description: "Detects an nginx installation and verifies its stub_status surface so the Metrics gear can consume it in Phase 4+.",
		Version:     "1.0.0",
		Category:    "monitoring",
	}
}

// MetricCategories declares that nginx is an HTTP-requests producer so
// the manager's primary-source resolver treats it as a candidate for
// CategoryHTTPRequests when more than one HTTP source is detected on
// the host. Without this, the manifest would still carry an `nginx`
// entry but the dashboard would never pick nginx's numbers over
// HAProxy's even with the operator override set.
func (g *Gear) MetricCategories() []gear.MetricCategory {
	return []gear.MetricCategory{gear.CategoryHTTPRequests}
}

// Probe walks the precedence model from issue #95:
//  1. NGINX_STATUS_URL set → trust operator, return Available with the
//     configured URL surfaced (no synchronous HTTP probe — the operator
//     pointed us at a specific instance).
//  2. nginx binary not on PATH → NotInstalled.
//  3. Binary present → run `nginx -v` (version) + `nginx -V` (build
//     options, conf-path, API module). Probe default status URL.
//     - 200 with stub_status sentinel body → Available.
//     - 403 → Inaccessible (stub_status exists but rejects request).
//     - 404 → Inaccessible (stub_status not configured).
//     - connection refused / timeout → Inaccessible (no listener).
//
// The function is cheap: a single LookPath, two `nginx -V` runs (each
// well under 100ms), and one HTTP GET with a 1s timeout. No retries.
func (g *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
	// Branch 1: operator override. Skip the synchronous probe; trust
	// the operator. We still try to record version + binary if we
	// can find them, but a failure here doesn't downgrade the verdict.
	if deps.NginxStatusURL != "" {
		caps := map[string]string{
			"status_url":      deps.NginxStatusURL,
			"status_source":   "stub_status",
			"override_source": "env",
		}
		g.recordBinaryFacts(ctx, deps, caps)
		return gear.ProbeAvailable("status URL configured via NGINX_STATUS_URL", caps)
	}

	binary, err := g.lookPath("nginx")
	if err != nil {
		return gear.ProbeNotInstalled("no nginx binary on PATH")
	}

	caps := map[string]string{
		"binary_path":   binary,
		"status_source": "stub_status",
	}
	g.recordBinaryFacts(ctx, deps, caps)

	// Branch 2/3/4: probe the default stub_status URL.
	caps["status_url"] = defaultStatusURL
	res, err := g.httpGet(ctx, defaultStatusURL)
	if err != nil {
		// Connection refused, DNS error, timeout — nginx binary is
		// here but nothing's listening for us.
		return gear.ProbeInaccessible(fmt.Sprintf(
			"nginx binary at %s but stub_status probe to %s failed: %v",
			binary, defaultStatusURL, err,
		))
	}
	switch res.StatusCode {
	case http.StatusOK:
		if strings.Contains(res.Body, stubStatusSentinel) {
			return gear.ProbeAvailable(
				fmt.Sprintf("stub_status reachable at %s", defaultStatusURL),
				caps,
			)
		}
		// 200 from something that wasn't stub_status — a catch-all
		// virtual host swallowing our probe. Treat as inaccessible
		// so the operator routes /nginx_status to a real
		// `stub_status` location.
		return gear.ProbeInaccessible(fmt.Sprintf(
			"nginx binary at %s but %s returned 200 without the stub_status sentinel; a catch-all vhost is intercepting the probe",
			binary, defaultStatusURL,
		))
	case http.StatusForbidden:
		return gear.ProbeInaccessible(fmt.Sprintf(
			"nginx binary at %s but stub_status at %s returns 403; add 'allow 127.0.0.1; deny all;' to the stub_status location",
			binary, defaultStatusURL,
		))
	case http.StatusNotFound:
		return gear.ProbeInaccessible(fmt.Sprintf(
			"nginx binary at %s but stub_status not configured; add 'location /nginx_status { stub_status; allow 127.0.0.1; deny all; }' to a server block",
			binary,
		))
	default:
		return gear.ProbeInaccessible(fmt.Sprintf(
			"nginx binary at %s but stub_status at %s returned HTTP %d",
			binary, defaultStatusURL, res.StatusCode,
		))
	}
}

// recordBinaryFacts populates version / config_path / api_module facts
// in caps based on `nginx -v`, `nginx -V`, the NGINX_CONFIG_FILE
// override, and well-known config paths. Best-effort — a missing fact
// is omitted rather than failing the probe, since the version-skew
// detection and config-driven log-path discovery in Phase 4+ are
// nice-to-haves, not gating conditions.
func (g *Gear) recordBinaryFacts(ctx context.Context, deps gear.Dependencies, caps map[string]string) {
	if out, err := g.runShortV(ctx); err == nil {
		if m := versionRegex.FindStringSubmatch(string(out)); len(m) > 1 {
			caps["version"] = m[1]
		}
	}

	var buildInfo string
	if out, err := g.runLongV(ctx); err == nil {
		buildInfo = string(out)
		if strings.Contains(buildInfo, apiModuleSentinel) {
			caps["api_module"] = "true"
		}
	}

	if path := resolveConfigPath(deps.NginxConfigFile, buildInfo, g.stat); path != "" {
		caps["config_path"] = path
	}
}

// resolveConfigPath finds nginx.conf in priority order: explicit
// override → `nginx -V`'s --conf-path → well-known paths. Returns ""
// if none exist — caller treats "" as "field omitted" by leaving it
// out of the capabilities map.
func resolveConfigPath(override, buildInfo string, stat func(string) (os.FileInfo, error)) string {
	if override != "" {
		// Trust operator; don't even stat. Phase 4+'s config parser
		// is the right place to surface "configured but missing"
		// errors with full context.
		return override
	}
	if m := confPathRegex.FindStringSubmatch(buildInfo); len(m) > 1 {
		if _, err := stat(m[1]); err == nil {
			return m[1]
		}
	}
	for _, p := range wellKnownConfigPaths {
		if _, err := stat(p); err == nil {
			return p
		}
	}
	return ""
}

// RegisterRoutes is a no-op — Phase 3 nginx detection produces no API
// surface. The metrics gear that reads stub_status lands in Phase 4+.
func (g *Gear) RegisterRoutes(_ chi.Router) {}

// Ensure the gear implements the required interfaces.
var (
	_ gear.Gear             = (*Gear)(nil)
	_ gear.ProbeableGear    = (*Gear)(nil)
	_ gear.MetricSourceGear = (*Gear)(nil)
)
