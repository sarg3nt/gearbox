package apache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

// Stats is the normalised view of one mod_status?auto scrape. Field
// names mirror what Apache prints in its key-value format so a
// reader can map them back to the upstream surface without guessing.
//
// All counters are monotonic since httpd start; the dashboard
// derives rates by diffing successive samples. Reading per-request
// "best" / "worst" timing would require parsing the Scoreboard
// character-by-character, which we intentionally don't do here —
// that's a follow-up enhancement when the dashboard actually needs
// it.
type Stats struct {
	// TotalAccesses is the total number of requests served since
	// httpd start.
	TotalAccesses int64 `json:"total_accesses"`

	// TotalKBytes is the total response body bytes (in KB) served
	// since httpd start.
	TotalKBytes int64 `json:"total_kbytes"`

	// UptimeSeconds is how long the parent httpd has been running.
	UptimeSeconds int64 `json:"uptime_seconds"`

	// ReqPerSec is Apache's own short-window average. We keep it
	// even though the dashboard prefers diff-based rates, because
	// it's the first value visible before two samples have
	// accumulated.
	ReqPerSec float64 `json:"req_per_sec"`

	// BytesPerSec / BytesPerReq from mod_status. Same caveat as
	// ReqPerSec — Apache's averages.
	BytesPerSec float64 `json:"bytes_per_sec"`
	BytesPerReq float64 `json:"bytes_per_req"`

	// Worker pool. BusyWorkers + IdleWorkers ≈ ServerLimit on a
	// healthy host; deviations signal config drift.
	BusyWorkers int `json:"busy_workers"`
	IdleWorkers int `json:"idle_workers"`

	// CPULoad is Apache's view of recent CPU consumption — useful
	// alongside the host CPU% gauge for "is httpd the culprit?".
	CPULoad float64 `json:"cpu_load"`

	// CollectedAt is when the agent scraped, in RFC3339.
	CollectedAt string `json:"collected_at"`
}

// ParseModStatus extracts a Stats struct from mod_status?auto's
// key-value body. Exported for unit testing without a live Apache.
// Unknown keys are silently skipped — Apache versions add new lines
// over time and the parser must tolerate that.
func ParseModStatus(body string) (Stats, error) {
	if strings.TrimSpace(body) == "" {
		return Stats{}, errors.New("empty mod_status body")
	}
	stats := Stats{CollectedAt: time.Now().UTC().Format(time.RFC3339)}
	sawAnyField := false

	for _, line := range strings.Split(body, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		sawAnyField = true
		switch key {
		case "Total Accesses":
			stats.TotalAccesses, _ = strconv.ParseInt(value, 10, 64)
		case "Total kBytes":
			stats.TotalKBytes, _ = strconv.ParseInt(value, 10, 64)
		case "Uptime":
			stats.UptimeSeconds, _ = strconv.ParseInt(value, 10, 64)
		case "ReqPerSec":
			stats.ReqPerSec, _ = strconv.ParseFloat(value, 64)
		case "BytesPerSec":
			stats.BytesPerSec, _ = strconv.ParseFloat(value, 64)
		case "BytesPerReq":
			stats.BytesPerReq, _ = strconv.ParseFloat(value, 64)
		case "BusyWorkers":
			stats.BusyWorkers, _ = strconv.Atoi(value)
		case "IdleWorkers":
			stats.IdleWorkers, _ = strconv.Atoi(value)
		case "CPULoad":
			stats.CPULoad, _ = strconv.ParseFloat(value, 64)
		}
	}
	if !sawAnyField {
		// Body had no parseable key:value lines — usually means
		// mod_status returned HTML (?auto wasn't honoured) or some
		// other surface answered instead of mod_status.
		return Stats{}, errors.New("mod_status body had no recognisable key:value pairs (is ?auto enabled?)")
	}
	return stats, nil
}

// Initialize captures collector-time state (status URL, event bus,
// scrape interval) once the gear has been probed Available.
func (g *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := g.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}
	g.statusURL = pickStatusURL(deps)
	g.eventBus = deps.EventBus
	g.statsInterval = deps.StatsInterval
	return nil
}

// pickStatusURL: operator override beats the default.
func pickStatusURL(deps gear.Dependencies) string {
	if deps.ApacheStatusURL != "" {
		return deps.ApacheStatusURL
	}
	return defaultStatusURL
}

// Collectors registers the periodic mod_status scrape.
func (g *Gear) Collectors() []gear.Collector {
	if g.statusURL == "" {
		return nil
	}
	return []gear.Collector{
		{
			Name:     "apache-mod-status",
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
			Name:        "apache.stats.updated",
			Description: "Published when Apache mod_status is scraped",
			Payload:     "apache.Stats — total accesses, worker pool, uptime, etc.",
		},
	}
}

// scrape fetches mod_status?auto, parses it, caches the latest snapshot.
func (g *Gear) scrape(ctx context.Context) (any, error) {
	res, err := g.httpGet(ctx, g.statusURL)
	if err != nil {
		return nil, fmt.Errorf("apache mod_status fetch: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apache mod_status returned HTTP %d", res.StatusCode)
	}
	stats, err := ParseModStatus(res.Body)
	if err != nil {
		return nil, fmt.Errorf("apache mod_status parse: %w", err)
	}
	g.cacheStats(stats)
	return stats, nil
}

// publish broadcasts the new stats to the event bus.
func (g *Gear) publish(data any) error {
	if g.eventBus == nil {
		return nil
	}
	stats, ok := data.(Stats)
	if !ok {
		return nil
	}
	g.eventBus.Publish(events.Event{
		Type:      events.EventType("apache.stats.updated"),
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

// RegisterRoutes registers the apache HTTP endpoints.
func (g *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/apache/stats", g.handleStats)
}

// handleStats returns the most recent mod_status snapshot. The
// `force` query parameter triggers a synchronous scrape.
func (g *Gear) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("force") == "true" {
		stats, err := g.scrape(r.Context())
		if err != nil {
			http.Error(w, "apache scrape failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, stats)
		return
	}
	stats, ok := g.readCachedStats()
	if !ok {
		http.Error(w, "apache stats not yet collected — agent just started, or mod_status is not reachable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, stats)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

var _ gear.CollectorGear = (*Gear)(nil)
