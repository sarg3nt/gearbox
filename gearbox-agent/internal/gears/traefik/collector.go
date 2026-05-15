package traefik

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/promtext"
)

// Stats is the normalised view of one Traefik Prometheus scrape.
// Traefik labels its router/service counters by HTTP status code,
// which lets us emit a real per-class breakdown — unlike Caddy,
// whose default surface lacks per-code granularity.
//
// All counters are monotonic since Traefik start; the dashboard
// derives rates by diffing successive samples.
type Stats struct {
	// RequestsTotal sums traefik_router_requests_total across all
	// labels — the all-traffic counter.
	RequestsTotal int64 `json:"requests_total"`

	// Response counts split by HTTP status-code first digit. The
	// dashboard uses these for the "5xx %" KPI and the per-class
	// stacked chart.
	Response1xx int64 `json:"response_1xx"`
	Response2xx int64 `json:"response_2xx"`
	Response3xx int64 `json:"response_3xx"`
	Response4xx int64 `json:"response_4xx"`
	Response5xx int64 `json:"response_5xx"`

	// EntryPoints lists the entrypoints Traefik exposed metrics
	// for. Stable order (alphabetised) so the dashboard renders
	// the same list across polls.
	EntryPoints []string `json:"entrypoints"`

	// CollectedAt is when the agent scraped, in RFC3339.
	CollectedAt string `json:"collected_at"`
}

// ParsePrometheusOutput extracts a Stats from Traefik's
// `:8082/metrics` Prometheus output. Exported for unit testing.
func ParsePrometheusOutput(body string) Stats {
	samples := promtext.Parse(body)
	stats := Stats{CollectedAt: time.Now().UTC().Format(time.RFC3339)}

	stats.RequestsTotal = int64(promtext.SumByName(samples, "traefik_router_requests_total"))

	// Per-status-class breakdown: walk every router_requests_total
	// sample once, bucket by first digit of the `code` label.
	entryPointSet := make(map[string]struct{})
	for _, s := range samples {
		switch s.Name {
		case "traefik_router_requests_total":
			bucketByStatusClass(&stats, s.Labels["code"], s.Value)
		case "traefik_entrypoint_requests_total":
			if ep := s.Labels["entrypoint"]; ep != "" {
				entryPointSet[ep] = struct{}{}
			}
		}
	}

	stats.EntryPoints = sortedKeys(entryPointSet)
	return stats
}

// bucketByStatusClass adds value to the right Response{1..5}xx
// counter on stats. Unknown / empty codes are ignored so a metric
// without the `code` label doesn't quietly land in the wrong bucket.
func bucketByStatusClass(stats *Stats, code string, value float64) {
	if len(code) == 0 {
		return
	}
	v := int64(value)
	switch code[0] {
	case '1':
		stats.Response1xx += v
	case '2':
		stats.Response2xx += v
	case '3':
		stats.Response3xx += v
	case '4':
		stats.Response4xx += v
	case '5':
		stats.Response5xx += v
	}
}

// sortedKeys returns the map's keys in alphabetical order — a
// helper for stable JSON output.
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Tiny enough that a manual sort beats pulling in `sort`'s
	// extra closure overhead — usually 2-4 entrypoints.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// Initialize captures collector-time state.
func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := g.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}
	g.metricsURL = pickMetricsURL(deps)
	g.eventBus = deps.EventBus
	g.statsInterval = deps.StatsInterval
	return nil
}

// pickMetricsURL: operator override > first default URL. Unlike the
// probe phase (which tries both default URLs in order), the
// collector commits to one — the operator's override or the first
// default. Switching URLs at collection time would invalidate the
// counters mid-stream from the dashboard's perspective.
func pickMetricsURL(deps gear.Dependencies) string {
	if deps.TraefikMetricsURL != "" {
		return deps.TraefikMetricsURL
	}
	return defaultMetricsURLs[0]
}

// Collectors registers the periodic Prometheus scrape.
func (g *Gear) Collectors() []gear.Collector {
	if g.metricsURL == "" {
		return nil
	}
	return []gear.Collector{
		{
			Name:     "traefik-prometheus",
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
			Name:        "traefik.stats.updated",
			Description: "Published when Traefik Prometheus metrics are scraped",
			Payload:     "traefik.Stats — request counters split by status class + entrypoints",
		},
	}
}

func (g *Gear) scrape(ctx context.Context) (any, error) {
	res, err := g.httpGet(ctx, g.metricsURL)
	if err != nil {
		return nil, fmt.Errorf("traefik /metrics fetch: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("traefik /metrics returned HTTP %d", res.StatusCode)
	}
	// Defence against the operator pointing us at a non-Traefik
	// Prometheus exporter (the override branch's risk): a body
	// without the traefik_ sentinel means we'd produce all zeros,
	// which the dashboard would misread as "Traefik is up but
	// idle". Surface as an error so the issue is visible.
	if !strings.Contains(res.Body, prometheusSentinel) {
		return nil, fmt.Errorf("traefik /metrics body lacked the %q sentinel; a different service may be answering at %s", prometheusSentinel, g.metricsURL)
	}
	stats := ParsePrometheusOutput(res.Body)
	g.cacheStats(stats)
	return stats, nil
}

func (g *Gear) publish(data any) error {
	if g.eventBus == nil {
		return nil
	}
	stats, ok := data.(Stats)
	if !ok {
		return nil
	}
	g.eventBus.Publish(events.Event{
		Type:      events.EventType("traefik.stats.updated"),
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

// RegisterRoutes registers the traefik HTTP endpoint.
func (g *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/traefik/stats", g.handleStats)
}

func (g *Gear) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("force") == "true" {
		stats, err := g.scrape(r.Context())
		if err != nil {
			http.Error(w, "traefik scrape failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, stats)
		return
	}
	stats, ok := g.readCachedStats()
	if !ok {
		http.Error(w, "traefik stats not yet collected — agent just started, or metrics endpoint is not reachable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, stats)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

var _ gear.CollectorGear = (*Gear)(nil)
