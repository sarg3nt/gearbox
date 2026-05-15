package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/promtext"
)

// Stats is the normalised view of one Prometheus scrape. Caddy
// emits a rich Prometheus surface; we surface the rollups the
// Metrics gear cares about and leave the histogram detail for a
// future enhancement when the dashboard renders it.
//
// All counters are monotonic since Caddy start; the dashboard
// derives rates by diffing successive samples. The status-class
// totals are derived from caddy_http_request_errors_total + the
// inverse for 2xx (Caddy doesn't emit a per-code counter by default
// — `request_errors_total` is the supported error signal; future
// caddy versions or operator-extended metrics may add per-code
// granularity).
type Stats struct {
	// RequestsTotal sums caddy_http_requests_total across all
	// server / handler labels.
	RequestsTotal int64 `json:"requests_total"`

	// RequestErrorsTotal sums caddy_http_request_errors_total —
	// requests that failed before producing a response (5xx-class
	// from Caddy's perspective). Distinct from response_5xx in
	// other proxies; documented as such to avoid the dashboard
	// double-counting.
	RequestErrorsTotal int64 `json:"request_errors_total"`

	// CollectedAt is when the agent scraped, in RFC3339.
	CollectedAt string `json:"collected_at"`
}

// ParsePrometheusOutput extracts a Stats struct from raw Caddy
// `:2019/metrics` Prometheus output. Exported for unit testing.
//
// We deliberately do NOT report a separate "admin reachable"
// boolean here: the scrape itself goes against the admin endpoint
// (default `:2019/metrics`), so a Stats record landing in the
// dashboard's cache already proves admin is reachable. Surfacing it
// as a field would either always be true (redundant) or depend on
// request-driven metrics like `caddy_admin_http_requests_total`,
// which a freshly-started Caddy with admin enabled but no admin
// traffic yet would not emit — confusing the dashboard with a false
// "admin disconnected" reading. The 503 response from the handler
// before the first scrape covers the "not reachable" state.
func ParsePrometheusOutput(body string) Stats {
	samples := promtext.Parse(body)
	stats := Stats{CollectedAt: time.Now().UTC().Format(time.RFC3339)}

	stats.RequestsTotal = int64(promtext.SumByName(samples, "caddy_http_requests_total"))
	stats.RequestErrorsTotal = int64(promtext.SumByName(samples, "caddy_http_request_errors_total"))

	return stats
}

// Initialize captures collector-time state.
func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := g.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}
	g.adminURL = pickAdminURL(deps)
	g.eventBus = deps.EventBus
	g.statsInterval = deps.StatsInterval
	return nil
}

// pickAdminURL: operator override beats the default.
func pickAdminURL(deps gear.Dependencies) string {
	if deps.CaddyAdminURL != "" {
		return deps.CaddyAdminURL
	}
	return defaultAdminURL
}

// Collectors registers the periodic Prometheus scrape.
func (g *Gear) Collectors() []gear.Collector {
	if g.adminURL == "" {
		return nil
	}
	return []gear.Collector{
		{
			Name:     "caddy-prometheus",
			Interval: g.statsInterval,
			Collect:  g.scrape,
			OnData:   g.publish,
		},
	}
}

// EventTypes documents what this gear publishes.
func (g *Gear) EventTypes() []gear.EventType {
	return []gear.EventType{
		{
			Name:        "caddy.stats.updated",
			Description: "Published when Caddy Prometheus metrics are scraped",
			Payload:     "caddy.Stats — requests + error counters, admin status",
		},
	}
}

// scrape fetches the Prometheus surface and normalises the result.
// Unlike nginx/Apache, ParsePrometheusOutput cannot fail — a
// Prometheus surface that returns 200 with garbage simply yields
// zero counters, so the dashboard sees "Caddy is up but emitted
// nothing" rather than an error.
func (g *Gear) scrape(ctx context.Context) (any, error) {
	res, err := g.httpGet(ctx, g.adminURL)
	if err != nil {
		return nil, fmt.Errorf("caddy /metrics fetch: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("caddy /metrics returned HTTP %d", res.StatusCode)
	}
	stats := ParsePrometheusOutput(res.Body)
	g.cacheStats(stats)
	return stats, nil
}

// publish broadcasts stats updates to the event bus.
func (g *Gear) publish(data any) error {
	if g.eventBus == nil {
		return nil
	}
	stats, ok := data.(Stats)
	if !ok {
		return nil
	}
	g.eventBus.Publish(events.Event{
		Type:      events.EventType("caddy.stats.updated"),
		Timestamp: time.Now(),
		Data: map[string]any{
			"collected_at": stats.CollectedAt,
			"stats":        stats,
		},
	})
	return nil
}

func (g *Gear) cacheStats(s Stats) {
	g.statsMu.Lock()
	g.lastStats = &s
	g.statsMu.Unlock()
}

func (g *Gear) readCachedStats() (Stats, bool) {
	g.statsMu.RLock()
	defer g.statsMu.RUnlock()
	if g.lastStats == nil {
		return Stats{}, false
	}
	return *g.lastStats, true
}

// RegisterRoutes registers the caddy HTTP endpoint.
func (g *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/caddy/stats", g.handleStats)
}

func (g *Gear) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("force") == "true" {
		stats, err := g.scrape(r.Context())
		if err != nil {
			http.Error(w, "caddy scrape failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, stats)
		return
	}
	stats, ok := g.readCachedStats()
	if !ok {
		http.Error(w, "caddy stats not yet collected — agent just started, or admin endpoint is not reachable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, stats)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

var _ gear.CollectorGear = (*Gear)(nil)
