package nginx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

// Stats is the normalised view of one stub_status scrape. We keep
// the connection counters split out because the dashboard renders
// reading/writing/waiting as a stacked chart and needs them
// distinguishable. Rates (req/sec etc.) are derived on the
// dashboard side from successive samples — the agent's job is to
// emit honest counter values, not to invent rates between scrapes.
type Stats struct {
	// Active is the count of currently active client connections,
	// including waiting connections.
	Active int `json:"active"`

	// Reading: connections where nginx is reading the request
	// header.
	Reading int `json:"reading"`

	// Writing: connections where nginx is writing the response
	// back to the client.
	Writing int `json:"writing"`

	// Waiting: idle keepalive connections waiting for the next
	// request. On busy sites this is the bulk of `Active`.
	Waiting int `json:"waiting"`

	// Total connection counters since nginx start. Monotonic; the
	// dashboard subtracts successive samples to derive rates.
	Accepts  int64 `json:"accepts"`
	Handled  int64 `json:"handled"`
	Requests int64 `json:"requests"`

	// CollectedAt is when the agent scraped stub_status, in RFC3339.
	// Lets the dashboard compute "data age" without trusting wall-
	// clock skew between the two hosts (it just compares to the
	// previous sample's CollectedAt).
	CollectedAt string `json:"collected_at"`
}

// stubStatusBody is what nginx writes to the stub_status URL:
//
//	Active connections: 291
//	server accepts handled requests
//	 16630948 16630948 31070465
//	Reading: 6 Writing: 179 Waiting: 106
//
// Each field has a stable regex; we parse defensively because some
// older nginx builds emit slight whitespace variations.
var (
	reActive   = regexp.MustCompile(`Active connections:\s+(\d+)`)
	reTotals   = regexp.MustCompile(`(?m)^\s*(\d+)\s+(\d+)\s+(\d+)\s*$`)
	reReadWait = regexp.MustCompile(`Reading:\s+(\d+)\s+Writing:\s+(\d+)\s+Waiting:\s+(\d+)`)
)

// ParseStubStatus extracts a Stats struct from raw stub_status text.
// Exported so the collector can also be exercised by unit tests
// without needing a live nginx — the function is pure, no state.
func ParseStubStatus(body string) (Stats, error) {
	stats := Stats{CollectedAt: time.Now().UTC().Format(time.RFC3339)}
	if m := reActive.FindStringSubmatch(body); len(m) > 1 {
		stats.Active, _ = strconv.Atoi(m[1])
	} else {
		return Stats{}, errors.New("stub_status body missing 'Active connections' line")
	}
	if m := reTotals.FindStringSubmatch(body); len(m) > 3 {
		stats.Accepts, _ = strconv.ParseInt(m[1], 10, 64)
		stats.Handled, _ = strconv.ParseInt(m[2], 10, 64)
		stats.Requests, _ = strconv.ParseInt(m[3], 10, 64)
	} else {
		return Stats{}, errors.New("stub_status body missing 'accepts handled requests' counter row")
	}
	if m := reReadWait.FindStringSubmatch(body); len(m) > 3 {
		stats.Reading, _ = strconv.Atoi(m[1])
		stats.Writing, _ = strconv.Atoi(m[2])
		stats.Waiting, _ = strconv.Atoi(m[3])
	}
	return stats, nil
}

// Initialize stores deps + last-stats slot so the collector + API
// handler can both reach probe-time indirections (httpGet) and the
// status URL the detector picked.
func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := g.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}
	g.statusURL = pickStatusURL(deps)
	g.eventBus = deps.EventBus
	g.statsInterval = deps.StatsInterval
	return nil
}

// pickStatusURL returns the URL the collector should scrape: the
// operator override when set, otherwise the default stub_status URL.
// We don't re-read the probe result here because the resolution
// already happened during Probe(); collectors trust the probed
// surface and don't second-guess it.
func pickStatusURL(deps gear.Dependencies) string {
	if deps.NginxStatusURL != "" {
		return deps.NginxStatusURL
	}
	return defaultStatusURL
}

// Collectors implements gear.CollectorGear. One periodic collector
// per gear: scrape stub_status, normalise into Stats, cache, publish.
// Interval matches the agent's standard stats interval (HAProxy uses
// the same). Returning an empty list when there's no status URL
// keeps the gear quiet on hosts where probe came back Inaccessible
// — the manager already skipped Initialize in that case, but the
// belt-and-suspenders check costs nothing.
func (g *Gear) Collectors() []gear.Collector {
	if g.statusURL == "" {
		return nil
	}
	return []gear.Collector{
		{
			Name:     "nginx-stub-status",
			Interval: g.statsInterval,
			Collect:  g.scrape,
			OnData:   g.publish,
		},
	}
}

// EventTypes documents what this gear publishes for the WebSocket
// API consumers. Mirrors HAProxy's pattern.
func (g *Gear) EventTypes() []gear.EventType {
	return []gear.EventType{
		{
			Name:        "nginx.stats.updated",
			Description: "Published when nginx stub_status is scraped",
			Payload:     "nginx.Stats — active/reading/writing/waiting + accepts/handled/requests counters",
		},
	}
}

// scrape runs one stub_status fetch. Returns an error on transport
// failure; returns a parse error if the body doesn't look like
// stub_status (e.g. someone enabled the surface on a non-stub URL).
func (g *Gear) scrape(ctx context.Context) (any, error) {
	res, err := g.httpGet(ctx, g.statusURL)
	if err != nil {
		return nil, fmt.Errorf("nginx stub_status fetch: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nginx stub_status returned HTTP %d", res.StatusCode)
	}
	stats, err := ParseStubStatus(res.Body)
	if err != nil {
		return nil, fmt.Errorf("nginx stub_status parse: %w", err)
	}
	g.cacheStats(stats)
	return stats, nil
}

// publish broadcasts the new stats to the agent event bus so
// WebSocket consumers can subscribe to live nginx data the same
// way they do for HAProxy. Returns nil even on type mismatch —
// publishing is best-effort and shouldn't fail collection.
func (g *Gear) publish(data any) error {
	if g.eventBus == nil {
		return nil
	}
	stats, ok := data.(Stats)
	if !ok {
		return nil
	}
	g.eventBus.Publish(events.Event{
		Type:      events.EventType("nginx.stats.updated"),
		Timestamp: time.Now(),
		Data: map[string]any{
			"collected_at": stats.CollectedAt,
			"stats":        stats,
		},
	})
	return nil
}

// cacheStats stores the most recent stub_status sample so the
// synchronous /api/v1/nginx/stats handler can return data without
// triggering a fresh scrape on every request — keeping the dashboard
// snappy even when nginx is on a slow box.
func (g *Gear) cacheStats(s Stats) {
	g.statsMu.Lock()
	g.lastStats = &s
	g.statsMu.Unlock()
}

// readCachedStats returns the most recent sample if one exists.
// The bool conveys "has anything been collected yet?" so the handler
// can return a 503 with a clear reason rather than an empty Stats
// (which would look like "nginx has zero connections", a different
// bug).
func (g *Gear) readCachedStats() (Stats, bool) {
	g.statsMu.RLock()
	defer g.statsMu.RUnlock()
	if g.lastStats == nil {
		return Stats{}, false
	}
	return *g.lastStats, true
}

// RegisterRoutes registers the nginx HTTP endpoints. Detection-only
// installations (Probe returned Available without any stats yet)
// still get the routes because the dashboard uses route presence as
// "this source is supported"; the handler distinguishes "no data
// yet" with a 503.
func (g *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/nginx/stats", g.handleStats)
}

// handleStats returns the most recent stub_status snapshot. The
// `force` query parameter triggers a synchronous scrape (useful for
// debugging and for the first dashboard load before the collector's
// first tick).
func (g *Gear) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("force") == "true" {
		stats, err := g.scrape(r.Context())
		if err != nil {
			http.Error(w, "nginx scrape failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, stats)
		return
	}
	stats, ok := g.readCachedStats()
	if !ok {
		http.Error(w, "nginx stats not yet collected — agent just started, or stub_status is not reachable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, stats)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Ensure the gear implements CollectorGear at compile time so a
// future refactor that removes one of the methods fails the build
// rather than silently disabling collection.
var _ gear.CollectorGear = (*Gear)(nil)
