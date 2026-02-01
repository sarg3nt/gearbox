// Package logs provides log collection and streaming as a plugin.
package logs

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// validLogSourceName matches only alphanumeric, hyphen, and underscore characters.
var validLogSourceName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Plugin provides log collection and streaming functionality.
type Plugin struct {
	plugin.BasePlugin
	collector *Collector
	streamer  *Streamer
	eventBus  *events.Bus
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "logs",
		DisplayName: "Log Streaming",
		Description: "Collects and streams logs from systemd units, files, and kernel",
		Version:     "1.0.0",
		Category:    "monitoring",
		Core:        true,
	}
}

// Initialize sets up the plugin.
func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
	if err := p.BasePlugin.Initialize(ctx, deps); err != nil {
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
func (p *Plugin) Start(ctx context.Context) error {
	// Start streaming all log sources
	p.streamer.StartAll()
	p.Logger().Info("log streamer started", "sources", len(DefaultSources()))
	return nil
}

// Stop stops the log streamer.
func (p *Plugin) Stop(ctx context.Context) error {
	p.streamer.Stop()
	return nil
}

// RegisterRoutes registers HTTP API routes.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/logs", p.handleSources)
	r.Get("/api/v1/logs/{name}", p.handleFetch)
}

// Streamers returns the real-time streamers for this plugin.
func (p *Plugin) Streamers() []plugin.Streamer {
	// The streamer is started/stopped via Start/Stop methods
	// Return empty since we manage it directly
	return nil
}

// EventTypes returns the events this plugin publishes.
func (p *Plugin) EventTypes() []plugin.EventType {
	return []plugin.EventType{
		{
			Name:        string(events.EventLogsUpdated),
			Description: "Published when new log entries are received",
			Payload:     "LogLine with source, line content, and timestamp",
		},
	}
}

// HTTP Handlers

// handleSources handles GET /api/v1/logs.
func (p *Plugin) handleSources(w http.ResponseWriter, r *http.Request) {
	sources := p.collector.GetSources()
	resp := SourcesResponse{
		Sources: sources,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleFetch handles GET /api/v1/logs/{name}.
func (p *Plugin) handleFetch(w http.ResponseWriter, r *http.Request) {
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
func (p *Plugin) GetStreamer() *Streamer {
	return p.streamer
}

// GetCollector returns the log collector for use by other components.
func (p *Plugin) GetCollector() *Collector {
	return p.collector
}

// Ensure plugin implements required interfaces.
var (
	_ plugin.Plugin         = (*Plugin)(nil)
	_ plugin.StreamerPlugin = (*Plugin)(nil)
)
