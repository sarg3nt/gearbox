// Package plugin provides the core plugin system for the HAProxy agent.
// It follows the compile-time plugin pattern (similar to Caddy) where plugins register
// themselves via init() functions and implement a common Plugin interface.
package plugin

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Plugin is the main interface that all agent plugins must implement.
// Plugins are self-contained modules that provide specific functionality
// to the HAProxy agent.
type Plugin interface {
	// Info returns metadata about the plugin.
	Info() Info

	// Initialize is called once when the application starts.
	// Plugins should set up their internal state and validate configuration.
	Initialize(ctx context.Context, deps Dependencies) error

	// Start is called after all plugins are initialized.
	// Plugins should start any background goroutines here.
	Start(ctx context.Context) error

	// Stop is called during application shutdown.
	// Plugins should clean up resources and stop background goroutines.
	Stop(ctx context.Context) error

	// Health returns the current health status of the plugin.
	Health() HealthStatus

	// RegisterRoutes registers HTTP API routes for this plugin.
	// The router is mounted at the plugin's base path (e.g., /api/v1/logs).
	RegisterRoutes(r chi.Router)

	// EventTypes returns the event types this plugin publishes.
	// Used for documentation and WebSocket filtering.
	EventTypes() []EventType
}

// Info contains metadata about a plugin.
type Info struct {
	// Name is the internal identifier (e.g., "metrics", "haproxy-stats").
	// Must be unique across all plugins.
	Name string

	// DisplayName is shown in logs and API responses (e.g., "System Metrics").
	DisplayName string

	// Description provides a detailed description of the plugin.
	Description string

	// Version is the semantic version (e.g., "1.0.0").
	Version string

	// Category groups related plugins (e.g., "monitoring", "security", "system").
	Category string

	// Core indicates this is a core plugin that cannot be disabled.
	Core bool
}

// HealthStatus represents the health state of a plugin.
type HealthStatus struct {
	// Status is one of: "healthy", "degraded", "unhealthy"
	Status string

	// Message provides additional context about the health status.
	Message string

	// LastCheck is when the health was last checked.
	LastCheck time.Time
}

// Health status constants.
const (
	HealthStatusHealthy   = "healthy"
	HealthStatusDegraded  = "degraded"
	HealthStatusUnhealthy = "unhealthy"
)

// EventType describes an event that a plugin can publish.
type EventType struct {
	// Name is the event type identifier (e.g., "stats.updated", "metrics.updated").
	Name string

	// Description describes when this event is published.
	Description string

	// Payload describes the structure of the event data.
	Payload string
}

// CollectorPlugin is implemented by plugins that collect data periodically.
type CollectorPlugin interface {
	Plugin

	// Collectors returns the data collectors provided by this plugin.
	Collectors() []Collector
}

// Collector defines a periodic data collector.
type Collector struct {
	// Name identifies this collector.
	Name string

	// Interval is how often to run the collector.
	// If zero, the collector runs on-demand only.
	Interval time.Duration

	// Collect runs the data collection.
	// Returns the collected data and any error.
	Collect func(ctx context.Context) (any, error)

	// OnData is called with collected data.
	// Plugins can use this to publish events or store data.
	OnData func(data any) error
}

// StreamerPlugin is implemented by plugins that stream real-time data.
type StreamerPlugin interface {
	Plugin

	// Streamers returns the real-time streamers provided by this plugin.
	Streamers() []Streamer
}

// Streamer defines a real-time data streamer.
type Streamer struct {
	// Name identifies this streamer.
	Name string

	// Start begins streaming data.
	Start func(ctx context.Context) error

	// Stop stops the streamer.
	Stop func() error
}

// ServicePlugin is implemented by plugins that provide service status monitoring.
type ServicePlugin interface {
	Plugin

	// MonitoredServices returns the systemd services this plugin monitors.
	// These will be included in periodic health checks.
	MonitoredServices() []string
}

// WebSocketPlugin is implemented by plugins that handle WebSocket connections.
type WebSocketPlugin interface {
	Plugin

	// WebSocketHandlers returns WebSocket handlers provided by this plugin.
	WebSocketHandlers() []WebSocketHandler
}

// WebSocketHandler defines a WebSocket endpoint.
type WebSocketHandler struct {
	// Path is the WebSocket endpoint path.
	Path string

	// Handler processes WebSocket connections.
	Handler http.HandlerFunc
}

// EventHandlerPlugin is implemented by plugins that react to events.
type EventHandlerPlugin interface {
	Plugin

	// SubscribedEvents returns the event types this plugin handles.
	SubscribedEvents() []string

	// HandleEvent is called when a subscribed event occurs.
	HandleEvent(eventType string, payload any)
}

// NewHealthyStatus creates a healthy status.
func NewHealthyStatus(message string) HealthStatus {
	return HealthStatus{
		Status:    HealthStatusHealthy,
		Message:   message,
		LastCheck: time.Now(),
	}
}

// NewDegradedStatus creates a degraded status.
func NewDegradedStatus(message string) HealthStatus {
	return HealthStatus{
		Status:    HealthStatusDegraded,
		Message:   message,
		LastCheck: time.Now(),
	}
}

// NewUnhealthyStatus creates an unhealthy status.
func NewUnhealthyStatus(message string) HealthStatus {
	return HealthStatus{
		Status:    HealthStatusUnhealthy,
		Message:   message,
		LastCheck: time.Now(),
	}
}
