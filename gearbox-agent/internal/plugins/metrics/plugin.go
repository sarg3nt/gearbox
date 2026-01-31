// Package metrics provides system metrics collection as a plugin.
package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin provides system metrics collection functionality.
type Plugin struct {
	plugin.BasePlugin
	collector *Collector
	eventBus  *events.Bus
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "metrics",
		DisplayName: "System Metrics",
		Description: "Collects system metrics including CPU, memory, disk, and network usage",
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

	p.collector = NewCollector()
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

// RegisterRoutes registers HTTP API routes.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/metrics", p.handleMetrics)
	r.Get("/api/v1/services", p.handleServices)
	r.Get("/api/v1/services/available", p.handleAvailableServices)
	r.Post("/api/v1/services/control", p.handleServiceControl)
}

// Collectors returns the periodic collectors for this plugin.
func (p *Plugin) Collectors() []plugin.Collector {
	return []plugin.Collector{
		{
			Name:     "system-metrics",
			Interval: p.Deps().StatsInterval,
			Collect: func(ctx context.Context) (any, error) {
				return p.collectMetrics()
			},
			OnData: func(data any) error {
				return p.publishMetrics(data)
			},
		},
	}
}

// MonitoredServices returns the services this plugin monitors.
func (p *Plugin) MonitoredServices() []string {
	return []string{
		"haproxy",
		"gearbox-agent",
		"nftables",
		"fail2ban",
	}
}

// EventTypes returns the events this plugin publishes.
func (p *Plugin) EventTypes() []plugin.EventType {
	return []plugin.EventType{
		{
			Name:        plugin.EventMetricsUpdated,
			Description: "Published when system metrics are collected",
			Payload:     "SystemMetrics object with CPU, memory, disk, and network stats",
		},
	}
}

// collectMetrics gathers system metrics and service statuses.
func (p *Plugin) collectMetrics() (any, error) {
	sysMetrics, err := p.collector.Collect()
	if err != nil {
		return nil, err
	}

	// Get all monitored services from plugins
	services := plugin.GetAllMonitoredServices()
	serviceStatuses := p.collector.CollectServiceStatuses(services)

	return &MetricsData{
		Metrics:         sysMetrics,
		ServiceStatuses: serviceStatuses,
	}, nil
}

// publishMetrics publishes metrics to the event bus.
func (p *Plugin) publishMetrics(data any) error {
	metricsData, ok := data.(*MetricsData)
	if !ok {
		return nil
	}

	eventData := map[string]any{
		"collected_at":     time.Now().UTC().Format(time.RFC3339),
		"metrics":          metricsData.Metrics,
		"service_statuses": metricsData.ServiceStatuses,
	}

	p.eventBus.Publish(events.Event{
		Type:      events.EventMetricsUpdated,
		Timestamp: time.Now(),
		Data:      eventData,
	})

	p.Logger().Debug("published metrics update",
		"load_avg", metricsData.Metrics.LoadAverage1,
		"memory_percent", metricsData.Metrics.MemoryUsagePercent)

	return nil
}

// MetricsData holds collected metrics and service statuses.
type MetricsData struct {
	Metrics         *SystemMetrics  `json:"metrics"`
	ServiceStatuses []ServiceStatus `json:"service_statuses"`
}

// HTTP Handlers

// MetricsResponse wraps system metrics with metadata.
type MetricsResponse struct {
	Metrics  *SystemMetrics  `json:"metrics"`
	Services []ServiceStatus `json:"services"`
}

// handleMetrics handles GET /api/v1/metrics.
func (p *Plugin) handleMetrics(w http.ResponseWriter, r *http.Request) {
	m, err := p.collector.Collect()
	if err != nil {
		http.Error(w, "Failed to collect metrics: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all monitored services from plugins
	services := plugin.GetAllMonitoredServices()
	serviceStatuses := p.collector.CollectServiceStatuses(services)

	resp := MetricsResponse{
		Metrics:  m,
		Services: serviceStatuses,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ServicesResponse contains service status information.
type ServicesResponse struct {
	Services []ServiceStatus `json:"services"`
}

// handleServices handles GET /api/v1/services.
func (p *Plugin) handleServices(w http.ResponseWriter, r *http.Request) {
	// Get default services from all plugins
	serviceList := plugin.GetAllMonitoredServices()

	// Check for custom services in query parameter
	if svcParam := r.URL.Query().Get("services"); svcParam != "" {
		serviceList = parseServiceList(svcParam)
	}

	services := p.collector.CollectServiceStatuses(serviceList)

	resp := ServicesResponse{
		Services: services,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// parseServiceList parses a comma-separated list of service names.
func parseServiceList(svcParam string) []string {
	parts := make([]string, 0)
	for _, s := range strings.Split(svcParam, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}
	return parts
}

// AvailableServicesResponse contains the list of available services on the system.
type AvailableServicesResponse struct {
	Services []string `json:"services"`
}

// handleAvailableServices handles GET /api/v1/services/available.
func (p *Plugin) handleAvailableServices(w http.ResponseWriter, r *http.Request) {
	services, err := p.collector.ListAvailableServices()
	if err != nil {
		http.Error(w, "Failed to list services: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := AvailableServicesResponse{
		Services: services,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ServiceControlRequest represents a request to control a systemd service.
type ServiceControlRequest struct {
	Service string `json:"service"`
	Action  string `json:"action"` // start, stop, restart
}

// ServiceControlResponse represents the response from service control.
type ServiceControlResponse struct {
	Service string `json:"service"`
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Output  string `json:"output,omitempty"`
}

// handleServiceControl handles POST /api/v1/services/control.
func (p *Plugin) handleServiceControl(w http.ResponseWriter, r *http.Request) {
	var req ServiceControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate action
	if req.Action != "start" && req.Action != "stop" && req.Action != "restart" {
		http.Error(w, "Invalid action. Must be start, stop, or restart", http.StatusBadRequest)
		return
	}

	// Validate service name (basic sanity check - prevent command injection)
	if req.Service == "" || len(req.Service) > 256 {
		http.Error(w, "Invalid service name", http.StatusBadRequest)
		return
	}
	// Only allow alphanumeric, dash, underscore, at-sign, and dot in service names
	for _, c := range req.Service {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '@' || c == '.') {
			http.Error(w, "Invalid character in service name", http.StatusBadRequest)
			return
		}
	}

	// Execute systemctl command
	output, err := p.collector.ControlService(req.Service, req.Action)

	resp := ServiceControlResponse{
		Service: req.Service,
		Action:  req.Action,
		Output:  output,
	}

	if err != nil {
		resp.Success = false
		resp.Message = err.Error()
	} else {
		resp.Success = true
		resp.Message = "Service " + req.Action + " successful"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Ensure plugin implements required interfaces.
var (
	_ plugin.Plugin          = (*Plugin)(nil)
	_ plugin.CollectorPlugin = (*Plugin)(nil)
	_ plugin.ServicePlugin   = (*Plugin)(nil)
)
