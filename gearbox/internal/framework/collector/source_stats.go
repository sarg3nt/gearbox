// Package collector — per-source metric scraping for the Metrics
// gear's multi-source pivot (issue #91 phases 4 / 7).
//
// The dashboard polls each of the four non-HAProxy agent endpoints
// (/api/v1/{nginx,apache,caddy,traefik}/stats) on the same cadence
// as the HAProxy stats collector. Each scrape's normalised result
// lands in the source_stats table the database layer added; the
// metrics-page handlers read it back from there.
//
// We always poll every source rather than gating on the capability
// cache: a fresh agent that hasn't completed its first probe yet,
// or a brief network hiccup against the capability endpoint, would
// drop scrapes if the collector consulted the cache. Agent endpoints
// return 503 for sources whose gear hasn't successfully scraped yet
// and 404 for unsupported sources — both cases are silently skipped
// here so the collector stays robust.
package collector

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/database"
)

// supportedSources lists the source identifiers the dashboard
// collector polls every cycle. Order doesn't matter for correctness
// (each scrape persists its own row); kept alphabetical for
// readability when iterating in the manager and the JS layer's
// matching list.
var supportedSources = []string{"apache", "caddy", "nginx", "traefik"}

// SupportedSources exports the canonical list so other packages
// (handlers, tests) don't redeclare it.
func SupportedSources() []string {
	out := make([]string, len(supportedSources))
	copy(out, supportedSources)
	return out
}

// SourceStatsCollector polls one agent's per-source /stats endpoints
// and persists the normalised results to source_stats. One instance
// per manager; the manager's persistHistory tick drives Run().
type SourceStatsCollector struct {
	serverID string
	client   *agent.Client
	db       *database.DB
	logger   *slog.Logger
}

// NewSourceStatsCollector wires a collector to one box's agent
// client and the shared database. Returns nil-safe — a nil db just
// means scrapes are no-ops, which keeps the manager wiring simple
// during tests that don't care about persistence.
func NewSourceStatsCollector(serverID string, client *agent.Client, db *database.DB, logger *slog.Logger) *SourceStatsCollector {
	if logger == nil {
		logger = slog.Default()
	}
	return &SourceStatsCollector{
		serverID: serverID,
		client:   client,
		db:       db,
		logger:   logger.With("collector", "source_stats"),
	}
}

// Run scrapes every supported source once and persists what landed.
// Called from the manager's persistHistory loop on the same cadence
// the HAProxy stats snapshot is saved. Each source is fully
// independent — one source's failure doesn't skip the next.
func (s *SourceStatsCollector) Run() {
	if s.db == nil {
		return
	}
	for _, src := range supportedSources {
		s.collectOne(src)
	}
}

// collectOne scrapes one source and persists the normalised result.
// Silently skips on the common "source not present on this host"
// shapes (agent returns 503 = no data yet, 404 = source unsupported)
// so a homelab box that runs only HAProxy doesn't fill the journal
// with one warning per source per tick.
func (s *SourceStatsCollector) collectOne(source string) {
	raw, err := s.client.GetSourceStats(source)
	if err != nil {
		if isExpectedSourceMiss(err) {
			return
		}
		s.logger.Warn("source stats scrape failed",
			"server_id", s.serverID,
			"source", source,
			"error", err)
		return
	}
	snap := normaliseSourceStats(s.serverID, source, raw)
	if snap == nil {
		// Shouldn't happen — every source has a normaliser — but
		// defensively skip rather than persist a half-shaped row.
		return
	}
	if err := s.db.SaveSourceStats(snap); err != nil {
		s.logger.Error("source stats save failed",
			"server_id", s.serverID,
			"source", source,
			"error", err)
	}
}

// isExpectedSourceMiss reports whether the agent error matches one
// of the "this source isn't installed/ready on this host" shapes we
// want to swallow silently. The agent client returns *agent.APIError
// for any non-2xx response with the HTTP status code on a typed
// field — we inspect that directly rather than substring-matching
// the Message text, which would have been brittle (and previously
// broken — the Message is just the parsed response body, not a
// "status NNN" string).
//
// 503 = source-not-installed-or-not-yet-scraped (the per-source
// /stats handler returns 503 before its first successful collection),
// 404 = pre-#100 agent that doesn't have the endpoint at all.
func isExpectedSourceMiss(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *agent.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound ||
			apiErr.StatusCode == http.StatusServiceUnavailable
	}
	return false
}

// normaliseSourceStats maps one source's agent-side JSON shape into
// the common SourceStatsSnapshot the database stores. Each source
// emits different field names; we surface the rollups
// (requests_total, response_* where available, active connections),
// and the rest lands in Extra for the per-source chart card to
// consume.
//
// Numeric coercion uses asInt64 / asFloat — JSON numbers come
// through encoding/json as float64, and integer fields like
// `requests_total` round-trip cleanly because the agent never emits
// values approaching float64's 2^53 integer ceiling.
func normaliseSourceStats(serverID, source string, raw agent.SourceStats) *database.SourceStatsSnapshot {
	if raw == nil {
		return nil
	}
	snap := &database.SourceStatsSnapshot{
		ServerID:    serverID,
		Source:      source,
		CollectedAt: parseCollectedAt(raw["collected_at"]),
		Extra:       map[string]any{},
	}

	switch source {
	case "nginx":
		snap.RequestsTotal = asInt64(raw["requests"])
		snap.ActiveConnections = asInt64(raw["active"])
		// Per-state connection split into Extra so the dashboard
		// can render the reading/writing/waiting stacked chart.
		for _, k := range []string{"reading", "writing", "waiting", "accepts", "handled"} {
			if v, ok := raw[k]; ok {
				snap.Extra[k] = v
			}
		}

	case "apache":
		snap.RequestsTotal = asInt64(raw["total_accesses"])
		// Busy workers ≈ "active connections" for chart parity
		// with nginx; the idle counter goes to Extra alongside
		// the other Apache-specific fields.
		snap.ActiveConnections = asInt64(raw["busy_workers"])
		for _, k := range []string{
			"idle_workers", "cpu_load", "uptime_seconds",
			"req_per_sec", "bytes_per_sec", "bytes_per_req", "total_kbytes",
		} {
			if v, ok := raw[k]; ok {
				snap.Extra[k] = v
			}
		}

	case "caddy":
		snap.RequestsTotal = asInt64(raw["requests_total"])
		// Caddy doesn't emit per-status-class by default; the
		// rich aggregate of "requests that errored" lands in
		// Extra so the dashboard can render a single error
		// counter alongside requests_total.
		if v, ok := raw["request_errors_total"]; ok {
			snap.Extra["request_errors_total"] = v
		}

	case "traefik":
		snap.RequestsTotal = asInt64(raw["requests_total"])
		snap.Response2xx = asInt64(raw["response_2xx"])
		snap.Response3xx = asInt64(raw["response_3xx"])
		snap.Response4xx = asInt64(raw["response_4xx"])
		snap.Response5xx = asInt64(raw["response_5xx"])
		if v, ok := raw["response_1xx"]; ok {
			snap.Extra["response_1xx"] = v
		}
		if v, ok := raw["entrypoints"]; ok {
			snap.Extra["entrypoints"] = v
		}

	default:
		// Unknown source — store the raw payload in Extra so the
		// dashboard can at least render the bytes for diagnosis.
		// Defensive only; supportedSources gates the caller.
		snap.Extra["raw"] = raw
	}

	if len(snap.Extra) == 0 {
		snap.Extra = nil
	}
	return snap
}

// parseCollectedAt converts the agent's RFC3339 collected_at string
// into a time.Time. Falls back to time.Now (UTC) when the field is
// missing or unparseable so the row still lands; better to have a
// slightly-off timestamp than to drop the snapshot entirely.
func parseCollectedAt(v any) time.Time {
	if s, ok := v.(string); ok && s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

// asInt64 coerces a JSON-decoded number (float64) to int64 with
// truncation toward zero. Returns 0 for nil, strings, or anything
// else — keeps the normaliser tolerant of agent changes (a new
// field added as a string instead of a number doesn't crash; it
// just lands as zero until we update the mapping).
func asInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
