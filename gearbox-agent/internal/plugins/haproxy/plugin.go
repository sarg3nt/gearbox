// Package haproxy provides HAProxy stats collection as a plugin.
package haproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/plugin"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/haproxy"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin provides HAProxy stats collection functionality.
type Plugin struct {
	plugin.BasePlugin
	statsClient *haproxy.StatsClient
	configPath  string
	eventBus    *events.Bus
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "haproxy",
		DisplayName: "HAProxy Stats",
		Description: "Collects and exposes HAProxy statistics, runtime info, and configuration validation",
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

	// Only create stats client if HAProxy is configured
	if deps.HAProxyStatsSocket != "" || deps.HAProxyStatsURL != "" {
		p.statsClient = haproxy.NewStatsClient(
			deps.HAProxyStatsSocket,
			deps.HAProxyStatsURL,
			deps.HAProxyStatsUser,
			deps.HAProxyStatsPassword,
		)
	}

	p.configPath = deps.HAProxyConfigPath
	p.eventBus = deps.EventBus
	return nil
}

// Start is a no-op - collection is handled by the plugin manager.
func (p *Plugin) Start(ctx context.Context) error {
	return nil
}

// Stop cleans up resources.
func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}

// Health returns the health status.
func (p *Plugin) Health() plugin.HealthStatus {
	if p.statsClient == nil {
		return plugin.NewDegradedStatus("HAProxy stats not configured")
	}
	return plugin.NewHealthyStatus("operational")
}

// RegisterRoutes registers HTTP API routes.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/haproxy/stats", p.handleStats)
	r.Get("/api/v1/haproxy/info", p.handleRuntimeInfo)
	r.Get("/api/v1/haproxy/tables", p.handleStickTables)
	r.Get("/api/v1/haproxy/validate", p.handleConfigValidation)
}

// Collectors returns the periodic collectors for this plugin.
func (p *Plugin) Collectors() []plugin.Collector {
	if p.statsClient == nil {
		return nil
	}

	return []plugin.Collector{
		{
			Name:     "haproxy-stats",
			Interval: p.Deps().StatsInterval,
			Collect: func(ctx context.Context) (any, error) {
				return p.collectStats()
			},
			OnData: func(data any) error {
				return p.publishStats(data)
			},
		},
	}
}

// EventTypes returns the events this plugin publishes.
func (p *Plugin) EventTypes() []plugin.EventType {
	return []plugin.EventType{
		{
			Name:        string(events.EventStatsUpdated),
			Description: "Published when HAProxy stats are collected",
			Payload:     "HAProxyStats object with frontends, backends, and runtime info",
		},
	}
}

// StatsData holds collected HAProxy stats.
type StatsData struct {
	Stats       *haproxy.HAProxyStats `json:"stats"`
	RuntimeInfo *haproxy.RuntimeInfo  `json:"runtime_info,omitempty"`
}

// collectStats gathers HAProxy statistics.
func (p *Plugin) collectStats() (any, error) {
	csv, err := p.statsClient.GetStatsCSV()
	if err != nil {
		return nil, err
	}

	stats, err := haproxy.ParseStatsCSV(csv)
	if err != nil {
		return nil, err
	}

	// Also get runtime info (optional)
	runtimeInfo, err := p.statsClient.GetRuntimeInfo()
	if err != nil {
		p.Logger().Warn("failed to get HAProxy runtime info", "error", err)
		// Continue without runtime info
	}

	return &StatsData{
		Stats:       stats,
		RuntimeInfo: runtimeInfo,
	}, nil
}

// publishStats publishes stats to the event bus.
func (p *Plugin) publishStats(data any) error {
	statsData, ok := data.(*StatsData)
	if !ok {
		return nil
	}

	eventData := map[string]any{
		"collected_at":   time.Now().UTC().Format(time.RFC3339),
		"frontend_count": len(statsData.Stats.Frontends),
		"backend_count":  len(statsData.Stats.Backends),
		"stats":          statsData.Stats,
	}

	if statsData.RuntimeInfo != nil {
		eventData["runtime_info"] = statsData.RuntimeInfo
	}

	p.eventBus.Publish(events.Event{
		Type:      events.EventStatsUpdated,
		Timestamp: time.Now(),
		Data:      eventData,
	})

	p.Logger().Debug("published stats update",
		"frontends", len(statsData.Stats.Frontends),
		"backends", len(statsData.Stats.Backends))

	return nil
}

// HTTP Handlers

// handleStats handles GET /api/v1/haproxy/stats.
func (p *Plugin) handleStats(w http.ResponseWriter, r *http.Request) {
	if p.statsClient == nil {
		http.Error(w, "HAProxy stats not configured", http.StatusServiceUnavailable)
		return
	}

	csvData, err := p.statsClient.GetStatsCSV()
	if err != nil {
		http.Error(w, "Failed to fetch stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Allow requesting raw CSV format
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Write([]byte(csvData))
		return
	}

	// Default: parse and return as JSON
	stats, err := haproxy.ParseStatsCSV(csvData)
	if err != nil {
		http.Error(w, "Failed to parse stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleRuntimeInfo handles GET /api/v1/haproxy/info.
func (p *Plugin) handleRuntimeInfo(w http.ResponseWriter, r *http.Request) {
	if p.statsClient == nil {
		http.Error(w, "HAProxy stats not configured", http.StatusServiceUnavailable)
		return
	}

	info, err := p.statsClient.GetRuntimeInfo()
	if err != nil {
		http.Error(w, "Failed to fetch runtime info: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// StickTablesResponse contains stick table information.
type StickTablesResponse struct {
	Tables []haproxy.StickTableInfo `json:"tables"`
}

// handleStickTables handles GET /api/v1/haproxy/tables.
func (p *Plugin) handleStickTables(w http.ResponseWriter, r *http.Request) {
	if p.statsClient == nil {
		http.Error(w, "HAProxy stats not configured", http.StatusServiceUnavailable)
		return
	}

	tables, err := p.statsClient.GetStickTables()
	if err != nil {
		http.Error(w, "Failed to fetch stick tables: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := StickTablesResponse{
		Tables: tables,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ConfigValidationResponse contains the result of config validation.
type ConfigValidationResponse struct {
	Valid   bool   `json:"valid"`
	Output  string `json:"output"`
	Message string `json:"message,omitempty"`
}

// handleConfigValidation handles GET /api/v1/haproxy/validate.
func (p *Plugin) handleConfigValidation(w http.ResponseWriter, r *http.Request) {
	if p.configPath == "" {
		http.Error(w, "HAProxy config path not configured", http.StatusServiceUnavailable)
		return
	}

	valid, output, err := haproxy.ValidateConfig(p.configPath)
	if err != nil {
		http.Error(w, "Failed to validate config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ConfigValidationResponse{
		Valid:  valid,
		Output: output,
	}

	if valid {
		resp.Message = "Configuration is valid"
	} else {
		resp.Message = "Configuration is invalid"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GetStatsClient returns the stats client for use by other plugins.
func (p *Plugin) GetStatsClient() *haproxy.StatsClient {
	return p.statsClient
}

// Ensure plugin implements required interfaces.
var (
	_ plugin.Plugin          = (*Plugin)(nil)
	_ plugin.CollectorPlugin = (*Plugin)(nil)
)
