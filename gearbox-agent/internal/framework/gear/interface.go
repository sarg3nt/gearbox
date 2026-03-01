// Package gear provides the core plugin system for the HAProxy agent.
// It follows the compile-time plugin pattern (similar to Caddy) where plugins register
// themselves via init() functions and implement a common Plugin interface.
package gear

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Gear is the main interface that all agent gears must implement.
// Gears are self-contained modules that provide specific functionality
// to the HAProxy agent.
type Gear interface {
	// Info returns metadata about the gear.
	Info() Info

	// Initialize is called once when the application starts.
	// Gears should set up their internal state and validate configuration.
	Initialize(ctx context.Context, deps Dependencies) error

	// Start is called after all plugins are initialized.
	// Gears should start any background goroutines here.
	Start(ctx context.Context) error

	// Stop is called during application shutdown.
	// Gears should clean up resources and stop background goroutines.
	Stop(ctx context.Context) error

	// Health returns the current health status of the gear.
	Health() HealthStatus

	// RegisterRoutes registers HTTP API routes for this gear.
	// The router is mounted at the plugin's base path (e.g., /api/v1/logs).
	RegisterRoutes(r chi.Router)

	// EventTypes returns the event types this plugin publishes.
	// Used for documentation and WebSocket filtering.
	EventTypes() []EventType
}

// Info contains metadata about a gear.
type Info struct {
	// Name is the internal identifier (e.g., "metrics", "haproxy-stats").
	// Must be unique across all plugins.
	Name string

	// DisplayName is shown in logs and API responses (e.g., "System Metrics").
	DisplayName string

	// Description provides a detailed description of the gear.
	Description string

	// Version is the semantic version (e.g., "1.0.0").
	Version string

	// Category groups related plugins (e.g., "monitoring", "security", "system").
	Category string

	// Core indicates this is a core plugin that cannot be disabled.
	Core bool
}

// HealthStatus represents the health state of a gear.
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

// CollectorGear is implemented by gears that collect data periodically.
type CollectorGear interface {
	Gear

	// Collectors returns the data collectors provided by this gear.
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
	// Gears can use this to publish events or store data.
	OnData func(data any) error
}

// StreamerGear is implemented by gears that stream real-time data.
type StreamerGear interface {
	Gear

	// Streamers returns the real-time streamers provided by this gear.
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

// ServiceGear is implemented by gears that provide service status monitoring.
type ServiceGear interface {
	Gear

	// MonitoredServices returns the systemd services this plugin monitors.
	// These will be included in periodic health checks.
	MonitoredServices() []string
}

// WebSocketGear is implemented by gears that handle WebSocket connections.
type WebSocketGear interface {
	Gear

	// WebSocketHandlers returns WebSocket handlers provided by this gear.
	WebSocketHandlers() []WebSocketHandler
}

// WebSocketHandler defines a WebSocket endpoint.
type WebSocketHandler struct {
	// Path is the WebSocket endpoint path.
	Path string

	// Handler processes WebSocket connections.
	Handler http.HandlerFunc
}

// EventHandlerGear is implemented by gears that react to events.
type EventHandlerGear interface {
	Gear

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
