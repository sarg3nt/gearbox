// Package logs provides log collection and streaming as a gear.
package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// validLogSourceName matches only alphanumeric, hyphen, and underscore characters.
var validLogSourceName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Plugin provides log collection and streaming functionality.
type Gear struct {
	gear.BaseGear
	collector *Collector
	streamer  *Streamer
	eventBus  *events.Bus
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "logs",
		DisplayName: "Log Streaming",
		Description: "Collects and streams logs from systemd units, files, and kernel",
		Version:     "1.0.0",
		Category:    "monitoring",
		Core:        true,
	}
}

// Initialize sets up the gear.
func (p *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := p.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}

	// Create collector with default sources
	sources := DefaultSources()
	p.collector = NewCollector(sources)

	// Create streamer for real-time log streaming
	p.eventBus = deps.EventBus
	p.streamer = NewStreamer(sources, deps.EventBus, deps.Logger)

	return nil
}

// Start starts the log streamer.
func (p *Gear) Start(ctx context.Context) error {
	// Start streaming all log sources
	p.streamer.StartAll()
	p.Logger().Info("log streamer started", "sources", len(DefaultSources()))
	return nil
}

// Probe reports whether at least one log streaming tool is present. The
// gear shells out to journalctl for systemd-unit sources and to tail for
// file sources; with neither available, none of the 16 default sources
// can produce output and loading the gear is pure noise. Individual
// source viability is still checked at stream-start time (gh issue #60
// tracks moving that detection into per-source probes).
func (p *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
	hasJournalctl := false
	hasTail := false
	if _, err := exec.LookPath("journalctl"); err == nil {
		hasJournalctl = true
	}
	if _, err := exec.LookPath("tail"); err == nil {
		hasTail = true
	}
	if !hasJournalctl && !hasTail {
		return gear.ProbeNotInstalled("neither journalctl nor tail found on PATH; cannot stream any log source")
	}
	caps := map[string]string{}
	if hasJournalctl {
		caps["journalctl"] = "present"
	}
	if hasTail {
		caps["tail"] = "present"
	}
	return gear.ProbeAvailable("log streaming tools present", caps)
}

// Stop stops the log streamer.
func (p *Gear) Stop(ctx context.Context) error {
	p.streamer.Stop()
	return nil
}

// RegisterRoutes registers HTTP API routes.
func (p *Gear) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/logs", p.handleSources)
	r.Get("/api/v1/logs/{name}", p.handleFetch)
}

// Streamers returns the real-time streamers for this gear.
func (p *Gear) Streamers() []gear.Streamer {
	// The streamer is started/stopped via Start/Stop methods
	// Return empty since we manage it directly
	return nil
}

// EventTypes returns the events this plugin publishes.
func (p *Gear) EventTypes() []gear.EventType {
	return []gear.EventType{
		{
			Name:        string(events.EventLogsUpdated),
			Description: "Published when new log entries are received",
			Payload:     "LogLine with source, line content, and timestamp",
		},
	}
}

// HTTP Handlers

// handleSources handles GET /api/v1/logs.
func (p *Gear) handleSources(w http.ResponseWriter, r *http.Request) {
	sources := p.collector.GetSources()
	resp := SourcesResponse{
		Sources: sources,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleFetch handles GET /api/v1/logs/{name}.
func (p *Gear) handleFetch(w http.ResponseWriter, r *http.Request) {
	// Extract the log source name from the path
	name := chi.URLParam(r, "name")
	if name == "" {
		http.Error(w, "Log source name required", http.StatusBadRequest)
		return
	}

	// Validate log source name to prevent path traversal attacks
	if !validLogSourceName.MatchString(name) {
		http.Error(w, "Invalid log source name", http.StatusBadRequest)
		return
	}

	// Check if the source exists
	if !p.collector.HasSource(name) {
		http.Error(w, "Unknown log source: "+name, http.StatusNotFound)
		return
	}

	// Parse lines parameter (optional, defaults to 500, max 10000)
	lines := 500
	if linesParam := r.URL.Query().Get("lines"); linesParam != "" {
		if parsed, err := strconv.Atoi(linesParam); err == nil && parsed > 0 {
			lines = parsed
			if lines > 10000 {
				lines = 10000 // Cap at 10k lines to prevent abuse
			}
		}
	}

	// Fetch the logs
	content, err := p.collector.FetchLogs(name, lines)
	if err != nil {
		http.Error(w, "Failed to fetch logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := LogResponse{
		Name:    name,
		Lines:   lines,
		Content: content,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GetStreamer returns the log streamer for use by other components.
func (p *Gear) GetStreamer() *Streamer {
	return p.streamer
}

// GetCollector returns the log collector for use by other components.
func (p *Gear) GetCollector() *Collector {
	return p.collector
}

// Ensure plugin implements required interfaces.
var (
	_ gear.Gear         = (*Gear)(nil)
	_ gear.StreamerGear = (*Gear)(nil)
)
