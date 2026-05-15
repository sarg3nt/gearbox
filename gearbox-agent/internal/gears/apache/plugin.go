// Package apache detects an Apache HTTP Server installation on the host
// and reports it in the capability manifest. Phase 3 only — no metrics
// collection yet; that lands in Phase 7 (separate issue, #95 followup)
// and will read the mod_status surface this gear already verified.
//
// The detector declares CategoryHTTPRequests so the agent's
// primary-source resolver (see [gear.ResolvePrimarySources]) considers
// Apache as a candidate when more than one HTTP source is detected on
// the host. Distros split on the binary name — apache2 on Debian/
// Ubuntu, httpd on RHEL/Fedora — so the detector tries both and
// prefers whichever it finds first.
package apache

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

// binaryNames lists the executables to try in order. apache2 comes
// first because Debian-family hosts are the more common deploy target
// in this project's homelab; the order doesn't actually affect
// correctness (a host can't have two simultaneously running Apaches
// under different binary names without manual configuration).
var binaryNames = []string{"apache2", "httpd"}

// wellKnownConfigPaths is searched when neither APACHE_CONFIG_FILE nor
// `httpd -V` reveals the path. Same ordering as binaryNames: apache2
// layout first, then RHEL-style httpd.
var wellKnownConfigPaths = []string{
	"/etc/apache2/apache2.conf",
	"/etc/httpd/conf/httpd.conf",
}

// defaultStatusURL is the canonical mod_status URL with the `?auto`
// query that returns the machine-readable format the Phase 4 metrics
// gear will consume.
const defaultStatusURL = "http://127.0.0.1/server-status?auto"

// modStatusSentinel is the first line of every server-status?auto
// response — a stable marker that guards against catch-all vhosts
// returning 200 with unrelated content.
const modStatusSentinel = "Total Accesses:"

// versionRegex parses `apache2 -v` / `httpd -v` first line:
//
//	Server version: Apache/2.4.58 (Ubuntu)
var versionRegex = regexp.MustCompile(`Apache/(\d+\.\d+\.\d+)`)

// serverConfigFileRegex pulls SERVER_CONFIG_FILE out of `httpd -V`.
// Source of truth when the config moved off the well-known paths.
var serverConfigFileRegex = regexp.MustCompile(`SERVER_CONFIG_FILE="([^"]+)"`)

// statusModuleSentinel is what `httpd -M` prints for the mod_status
// module when it's loaded. Without it, the status endpoint can't
// respond no matter how the config is set up.
const statusModuleSentinel = "status_module"

// Gear is the Apache detector.
type Gear struct {
	gear.BaseGear

	// Probe-time indirection — tests swap these to control the
	// outcome without needing Apache installed on the runner.
	lookPath func(string) (string, error)
	runV     func(ctx context.Context, binary string) ([]byte, error) // <bin> -v / -V
	runM     func(ctx context.Context, binary string) ([]byte, error) // <bin> -M
	stat     func(string) (os.FileInfo, error)
	httpGet  func(ctx context.Context, url string) (probe.HTTPResult, error)
}

// New constructs an Apache gear with real OS-backed defaults.
func New() *Gear {
	return &Gear{
		lookPath: exec.LookPath,
		runV: func(ctx context.Context, binary string) ([]byte, error) {
			return exec.CommandContext(ctx, binary, "-V").CombinedOutput()
		},
		runM: func(ctx context.Context, binary string) ([]byte, error) {
			return exec.CommandContext(ctx, binary, "-M").CombinedOutput()
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
		Name:        "apache",
		DisplayName: "Apache HTTP Server",
		Description: "Detects an Apache installation and verifies its mod_status surface so the Metrics gear can consume it in Phase 7+.",
		Version:     "1.0.0",
		Category:    "monitoring",
	}
}

// MetricCategories declares that Apache is an HTTP-requests producer.
func (g *Gear) MetricCategories() []gear.MetricCategory {
	return []gear.MetricCategory{gear.CategoryHTTPRequests}
}

// Probe walks the precedence model from issue #95:
//  1. APACHE_STATUS_URL set → trust operator. Return Available with
//     the configured URL surfaced; no synchronous HTTP probe.
//  2. Neither apache2 nor httpd on PATH → NotInstalled.
//  3. Binary present → run `-V` (version + SERVER_CONFIG_FILE),
//     `-M` (loaded modules; flag status_module). Probe the default
//     status URL.
//     - 200 with mod_status sentinel body → Available.
//     - 403 → Inaccessible (`Require local` / Allow directive missing).
//     - 404 → Inaccessible (mod_status not loaded, or no Location block).
//     - connection refused / timeout → Inaccessible (no listener).
func (g *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
	// Branch 1: operator override.
	if deps.ApacheStatusURL != "" {
		caps := map[string]string{
			"status_url":      deps.ApacheStatusURL,
			"status_source":   "mod_status",
			"override_source": "env",
		}
		g.recordBinaryFacts(ctx, deps, caps)
		return gear.ProbeAvailable("status URL configured via APACHE_STATUS_URL", caps)
	}

	binary, binaryPath := g.findBinary()
	if binary == "" {
		return gear.ProbeNotInstalled("no apache2 or httpd binary on PATH")
	}

	caps := map[string]string{
		"binary":        binary,
		"binary_path":   binaryPath,
		"status_source": "mod_status",
	}
	g.recordBinaryFactsForBinary(ctx, deps, binary, caps)

	// Branch 2/3/4: probe the default mod_status URL.
	caps["status_url"] = defaultStatusURL
	res, err := g.httpGet(ctx, defaultStatusURL)
	if err != nil {
		return gear.ProbeInaccessible(fmt.Sprintf(
			"%s at %s but mod_status probe to %s failed: %v",
			binary, binaryPath, defaultStatusURL, err,
		))
	}
	switch res.StatusCode {
	case http.StatusOK:
		if strings.HasPrefix(strings.TrimSpace(res.Body), modStatusSentinel) {
			return gear.ProbeAvailable(
				fmt.Sprintf("mod_status reachable at %s", defaultStatusURL),
				caps,
			)
		}
		return gear.ProbeInaccessible(fmt.Sprintf(
			"%s at %s but %s returned 200 without the mod_status sentinel; a catch-all vhost is intercepting the probe",
			binary, binaryPath, defaultStatusURL,
		))
	case http.StatusForbidden:
		return gear.ProbeInaccessible(fmt.Sprintf(
			"%s at %s but mod_status at %s returns 403; add 'Require local' (or 'Require ip 127.0.0.1') to the server-status Location block",
			binary, binaryPath, defaultStatusURL,
		))
	case http.StatusNotFound:
		return gear.ProbeInaccessible(fmt.Sprintf(
			"%s at %s but mod_status not enabled; load mod_status and add a '<Location /server-status>' block with 'SetHandler server-status'",
			binary, binaryPath,
		))
	default:
		return gear.ProbeInaccessible(fmt.Sprintf(
			"%s at %s but mod_status at %s returned HTTP %d",
			binary, binaryPath, defaultStatusURL, res.StatusCode,
		))
	}
}

// findBinary returns the first matching binary name + its absolute
// path, or ("", "") when neither is on PATH. Order is apache2 first,
// then httpd (see binaryNames comment).
func (g *Gear) findBinary() (name, path string) {
	for _, n := range binaryNames {
		if p, err := g.lookPath(n); err == nil {
			return n, p
		}
	}
	return "", ""
}

// recordBinaryFacts is the override-branch convenience that picks the
// first available binary (if any) before delegating. Lets the override
// branch still populate version / config_path / status_module so the
// manifest stays informative even when the probe was bypassed.
func (g *Gear) recordBinaryFacts(ctx context.Context, deps gear.Dependencies, caps map[string]string) {
	binary, binaryPath := g.findBinary()
	if binary == "" {
		return
	}
	caps["binary"] = binary
	caps["binary_path"] = binaryPath
	g.recordBinaryFactsForBinary(ctx, deps, binary, caps)
}

// recordBinaryFactsForBinary populates the version / config_path /
// status_module facts from the chosen binary. Best-effort — a missing
// fact is omitted rather than failing the probe.
func (g *Gear) recordBinaryFactsForBinary(ctx context.Context, deps gear.Dependencies, binary string, caps map[string]string) {
	var vOut string
	if out, err := g.runV(ctx, binary); err == nil {
		vOut = string(out)
		if m := versionRegex.FindStringSubmatch(vOut); len(m) > 1 {
			caps["version"] = m[1]
		}
	}

	if out, err := g.runM(ctx, binary); err == nil {
		if strings.Contains(string(out), statusModuleSentinel) {
			caps["status_module"] = "true"
		}
	}

	if path := resolveConfigPath(deps.ApacheConfigFile, vOut, g.stat); path != "" {
		caps["config_path"] = path
	}
}

// resolveConfigPath: override → `httpd -V` SERVER_CONFIG_FILE →
// well-known paths.
func resolveConfigPath(override, vOut string, stat func(string) (os.FileInfo, error)) string {
	if override != "" {
		return override
	}
	if m := serverConfigFileRegex.FindStringSubmatch(vOut); len(m) > 1 {
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

// RegisterRoutes is a no-op — Phase 3 Apache detection produces no API
// surface. The metrics gear that reads mod_status lands in Phase 7+.
func (g *Gear) RegisterRoutes(_ chi.Router) {}

// Ensure the gear implements the required interfaces.
var (
	_ gear.Gear             = (*Gear)(nil)
	_ gear.ProbeableGear    = (*Gear)(nil)
	_ gear.MetricSourceGear = (*Gear)(nil)
)
