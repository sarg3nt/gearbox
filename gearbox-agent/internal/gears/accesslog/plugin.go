// Package accesslog hosts the agent endpoint that reads recent
// access-log records for a given source (haproxy / nginx / apache /
// caddy) and returns them as structured Records.
//
// The endpoint is the Phase-5 deliverable from issue #91: until now,
// the dashboard's Metrics page parsed HAProxy log lines client-side
// from the agent's generic logs gear. Moving the parser agent-side
// behind a typed endpoint means the dashboard treats every source the
// same way (call this endpoint, render the records) and the agent owns
// the per-format quirks.
//
// Detection only — the gear doesn't tail or buffer logs in the
// background. Each request reads the last N lines of the relevant
// access log on demand, parses them, applies the filters, and returns.
// Tailing + ring buffering is a future enhancement; on-demand reading
// is good enough for the dashboard's "show me recent 5xx" panel and
// avoids holding open file handles in the agent process.
package accesslog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/accesslog"
)

func init() {
	gear.Register(New())
}

// defaultLogPaths maps each supported source identifier to the
// well-known access-log location for that software on Linux. The
// operator overrides per-source via the *_ACCESS_LOG env vars when
// their distro / config differs.
var defaultLogPaths = map[string]string{
	"haproxy": "/var/log/haproxy.log",
	"nginx":   "/var/log/nginx/access.log",
	// Distros split — try the Debian path first, fall back to the
	// RHEL one inside the resolver. We only list one default here;
	// resolveLogPath does the multi-path search.
	"apache": "/var/log/apache2/access.log",
	"caddy":  "/var/log/caddy/access.log",
}

// apacheFallbackPath is the RHEL-style location tried when the
// Debian default isn't readable. Keeping it in a named const rather
// than encoding the fallback list in defaultLogPaths makes the
// "did we try multiple paths?" logic explicit at the call site.
const apacheFallbackPath = "/var/log/httpd/access_log"

// sourceProfile maps a source identifier to the primary parser
// profile the endpoint tries first. Apache uniquely also has a
// fallback profile (CLF without Referer / User-Agent) tried when
// the primary returns nil — see sourceFallbackProfile and
// parseWithFallback.
var sourceProfile = map[string]string{
	"haproxy": accesslog.ProfileHAProxy,
	"nginx":   accesslog.ProfileNginxCombined,
	"apache":  accesslog.ProfileApacheCombined,
	"caddy":   accesslog.ProfileCaddyJSON,
}

// sourceFallbackProfile names the second profile tried when the
// primary returns nil for a line. Today only Apache has one: many
// RHEL-style installs ship CLF (no Referer / User-Agent) by
// default, so we try ApacheCombined first (covers the Debian
// default + custom combined-format ops) and fall back to
// ApacheCommon. Lines that match neither stay rejected as noise.
var sourceFallbackProfile = map[string]string{
	"apache": accesslog.ProfileApacheCommon,
}

// maxLimit caps the per-request `limit` parameter so a buggy
// dashboard call can't make the agent shell `tail -n 100000` against
// a 10 GB log file. 10 000 records matches the existing logs gear's
// hard ceiling.
const maxLimit = 10000

// defaultLimit is the limit applied when the caller doesn't supply
// one. Tracks what the dashboard's "recent 5xx" drawer wants out of
// the box.
const defaultLimit = 500

// defaultLines is how many raw log lines to read off the tail when
// the caller hasn't asked for a specific count. Records pass through
// the status_min filter, so the effective return is usually much
// less; over-read by 4x by default to ensure 500 matches are findable
// on a low-error host.
const defaultLines = 2000

// maxLines caps how many raw log lines the endpoint reads — same
// hard ceiling as the existing logs gear.
const maxLines = 10000

// Gear is the access-log read-back endpoint. It owns no probe-time
// state — every dependency comes through the gear.Dependencies hand-
// off — but is structured the same way as the other gears for
// uniformity.
type Gear struct {
	gear.BaseGear

	// Probe-time indirection — tests swap these to control file
	// presence and tail output without needing real log files.
	stat func(string) (os.FileInfo, error)
	tail func(ctx context.Context, path string, lines int) ([]string, error)

	// Configured paths captured at Initialize. Empty means "fall
	// back to defaultLogPaths".
	paths map[string]string
}

// New returns a gear with real OS-backed defaults.
func New() *Gear {
	return &Gear{
		stat: os.Stat,
		tail: defaultTail,
	}
}

// Info returns gear metadata.
func (g *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "access-log",
		DisplayName: "Access Log",
		Description: "Reads recent access-log records for haproxy/nginx/apache/caddy and returns them as parsed Records.",
		Version:     "1.0.0",
		Category:    "monitoring",
		Core:        true,
	}
}

// accessLogSourceDisplayName names the access-log source as the
// dashboard's Logs page renders it in the source picker dropdown.
// Centralized here so the agent stays the single source of truth for
// what each source is called — the dashboard's old hardcoded
// "haproxy" → "HAProxy" mapping (api_logs.go) goes away once
// dashboards consume the Resources field added in issue #112.
var accessLogSourceDisplayName = map[string]string{
	"haproxy": "HAProxy",
	"nginx":   "nginx",
	"apache":  "Apache",
	"caddy":   "Caddy",
}

// Probe always reports Available — the gear's only job is to read
// files on demand, which is universally possible. Capabilities map
// records which sources have a readable log path; the dashboard
// uses this to gate the "Error Insights" panel per source.
//
// Resources["log_sources"] is the structured form the dashboard's
// Logs page reads to populate its source picker (issue #112 Phase 2).
// Each entry is {"name", "display_name", "path"} for one discovered
// web-server access log. Older dashboards that don't know about
// Resources continue to read the flat Capabilities map; both views
// are kept in sync. When no readable log file is found at all, the
// Resources field is left unset so the JSON envelope omits it and
// callers fall back to the flat Capabilities map.
//
// Side-effect-free: Probe builds the operator-override map as a
// local rather than mutating g.paths, so the function honors the
// ProbeableGear contract that probing must be free of state writes.
// Initialize re-runs the dep-to-paths copy below to populate g.paths
// for the handler path that needs it.
func (g *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
	overrides := pathsFromDeps(deps)

	caps := map[string]string{}
	logSources := []map[string]string{}
	for _, src := range []string{"haproxy", "nginx", "apache", "caddy"} {
		path := g.resolveLogPathWith(src, overrides)
		if path == "" {
			continue
		}
		caps[src+"_log"] = path
		logSources = append(logSources, map[string]string{
			"name":         src,
			"display_name": accessLogSourceDisplayName[src],
			"path":         path,
		})
	}
	// Only attach Resources when we actually have a payload — keeps
	// the JSON envelope's `resources` field truly omitempty.
	if len(logSources) > 0 {
		return gear.ProbeAvailableWithResources(
			"access-log endpoint registered",
			caps,
			map[string]any{"log_sources": logSources},
		)
	}
	return gear.ProbeAvailable("access-log endpoint registered", caps)
}

// pathsFromDeps extracts the operator-overrides map from
// gear.Dependencies. Shared by Probe (local-only, side-effect-free)
// and Initialize (mutates g.paths so handlers can read it later)
// so the deps→map mapping has one source of truth.
func pathsFromDeps(deps gear.Dependencies) map[string]string {
	return map[string]string{
		"haproxy": deps.HAProxyAccessLog,
		"nginx":   deps.NginxAccessLog,
		"apache":  deps.ApacheAccessLog,
		"caddy":   deps.CaddyAccessLog,
	}
}

// Initialize captures the path overrides for later use by the
// handler. Probe is side-effect-free (#112 review) so this is the
// single place g.paths gets written; the handler reads g.paths via
// resolveLogPath during request handling.
func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := g.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}
	g.paths = pathsFromDeps(deps)
	return nil
}

// RegisterRoutes registers the single recent-records endpoint.
func (g *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/access-log/{source}/recent", g.handleRecent)
}

// Response is the JSON envelope the endpoint returns. Available is
// false when the source has no readable log file on this host; the
// dashboard surfaces "logs unavailable" in that case rather than
// rendering an empty panel that looks like "no errors found".
type Response struct {
	Source     string             `json:"source"`
	Profile    string             `json:"profile"`
	Path       string             `json:"path,omitempty"`
	Available  bool               `json:"available"`
	Reason     string             `json:"reason,omitempty"`
	MatchCount int                `json:"match_count"`
	Records    []accesslog.Record `json:"records"`
}

// handleRecent reads recent log lines for the named source, parses
// them, filters by status_min, and returns at most `limit` records.
// All filters apply BEFORE the limit cap, so the caller gets the
// most-recent N matching records rather than N raw records that
// happen to include some matches.
func (g *Gear) handleRecent(w http.ResponseWriter, r *http.Request) {
	source := chi.URLParam(r, "source")
	profile, ok := sourceProfile[source]
	if !ok {
		http.Error(w, "unknown source — supported: haproxy, nginx, apache, caddy", http.StatusNotFound)
		return
	}
	parser := accesslog.ProfileByName(profile)
	if parser == nil {
		// Defensive: every entry in sourceProfile maps to a known
		// parser, so this would only fire if the maps drift.
		http.Error(w, "no parser registered for source "+source, http.StatusInternalServerError)
		return
	}
	// fallback may be nil — most sources have a single profile.
	// parseWithFallback handles the nil case as "no second try".
	fallback := accesslog.ProfileByName(sourceFallbackProfile[source])

	// status_min defaults to 500 (the dashboard's primary use case
	// is 5xx insights) but lets callers pass an explicit 0 to
	// disable the filter entirely. Min clamp is 0 (not 100) so
	// "explicitly disable" works; the gating logic compares with
	// rec.StatusCode < statusMin, which is a no-op when statusMin
	// is 0.
	statusMin := parseIntDefault(r.URL.Query().Get("status_min"), 500, 0, 599, 500)
	limit := parseIntDefault(r.URL.Query().Get("limit"), defaultLimit, 1, maxLimit, defaultLimit)
	lines := parseIntDefault(r.URL.Query().Get("lines"), defaultLines, 1, maxLines, defaultLines)

	path := g.resolveLogPath(source)
	resp := Response{Source: source, Profile: profile, Path: path}
	if path == "" {
		resp.Reason = fmt.Sprintf("no readable %s access log on this host (set %s_ACCESS_LOG to override)", source, strings.ToUpper(source))
		writeJSON(w, resp)
		return
	}

	raw, err := g.tail(r.Context(), path, lines)
	if err != nil {
		resp.Reason = fmt.Sprintf("tail %s: %v", path, err)
		writeJSON(w, resp)
		return
	}

	// Walk newest-to-oldest so when we hit the limit we keep the
	// most recent matches. `tail` returns oldest-first, so iterate
	// the slice in reverse.
	matches := make([]accesslog.Record, 0, limit)
	for i := len(raw) - 1; i >= 0; i-- {
		rec := parseWithFallback(parser, fallback, raw[i])
		if rec == nil {
			continue
		}
		if rec.StatusCode < statusMin {
			continue
		}
		matches = append(matches, *rec)
		if len(matches) >= limit {
			break
		}
	}

	resp.Available = true
	resp.Records = matches
	resp.MatchCount = len(matches)
	writeJSON(w, resp)
}

// parseWithFallback tries primary first; if primary returns nil and
// a fallback parser was registered for this source, it tries the
// fallback. Returns nil only when both reject the line. The Apache
// source uses this to handle both combined (default Debian) and
// CLF (default RHEL) without the caller needing to pre-detect
// which format the operator's running.
func parseWithFallback(primary, fallback accesslog.Parser, raw string) *accesslog.Record {
	if rec := primary.Parse(raw); rec != nil {
		return rec
	}
	if fallback == nil {
		return nil
	}
	return fallback.Parse(raw)
}

// resolveLogPath returns the access-log path for src using the
// operator-override map captured in g.paths. Thin wrapper over
// resolveLogPathWith so existing handler callers keep their
// `g.resolveLogPath(src)` ergonomics; Probe (side-effect-free)
// uses resolveLogPathWith directly with a local map.
func (g *Gear) resolveLogPath(src string) string {
	return g.resolveLogPathWith(src, g.paths)
}

// resolveLogPathWith is the underlying resolver. Takes the
// operator-override map explicitly so the caller (Probe / handler /
// test) can avoid touching shared state:
//
//   - the operator override if set and readable, OR
//   - the well-known default if readable, OR
//   - "" when neither exists.
//
// Apache gets a second-chance lookup against the RHEL-style path
// because Debian and RHEL ship the log under different paths.
func (g *Gear) resolveLogPathWith(src string, overrides map[string]string) string {
	if override, ok := overrides[src]; ok && override != "" {
		// Operator explicitly pointed us at a path — trust them.
		// If the path isn't readable the endpoint surfaces "tail
		// failed" rather than silently falling back to a
		// well-known default the operator may have deliberately
		// avoided.
		return override
	}
	if def, ok := defaultLogPaths[src]; ok {
		if g.isReadable(def) {
			return def
		}
	}
	if src == "apache" && g.isReadable(apacheFallbackPath) {
		return apacheFallbackPath
	}
	return ""
}

// isReadable is a defensive `stat` wrapper: we only consider the
// file usable if it exists AND looks like a regular file. A
// directory or socket at the path means the operator misconfigured
// something; failing fast with "" surfaces that as "no readable log
// file" rather than confusing tail errors later.
func (g *Gear) isReadable(path string) bool {
	info, err := g.stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// parseIntDefault parses one query parameter, clamps to [min,max],
// and falls back to def on parse failure / empty input. Consolidated
// here so the three params handleRecent reads share the same
// well-tested clamping logic.
func parseIntDefault(raw string, def, min, max, badDef int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return badDef
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// defaultTail shells out to /usr/bin/tail to read the last `lines`
// of `path`. Same approach as the existing logs gear — handles
// rotation and partial-write edge cases for free via tail's own
// implementation, and the agent already depends on tail being on
// PATH per logs gear's probe.
func defaultTail(ctx context.Context, path string, lines int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "tail", "-n", strconv.Itoa(lines), path)
	out, err := cmd.Output()
	if err != nil {
		// Distinguish missing-file from other failures so the
		// caller's reason field is actionable.
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("log file %s does not exist", path)
		}
		return nil, err
	}
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// EventTypes returns an empty slice — this gear publishes no events;
// it's a synchronous read-back endpoint only.
func (g *Gear) EventTypes() []gear.EventType { return nil }

// Ensure the gear satisfies the required interfaces.
var (
	_ gear.Gear          = (*Gear)(nil)
	_ gear.ProbeableGear = (*Gear)(nil)
)
