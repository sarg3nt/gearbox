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

// ProbeStatus is the agent's verdict on whether a gear can run on this host.
// Operators reading the startup table and the upcoming
// /api/v1/system/capabilities endpoint distinguish the four states because
// the appropriate fix differs — "install the thing", "fix the access", or
// "change the config" — and conflating them costs debugging time.
type ProbeStatus string

const (
	// ProbeStatusAvailable means the gear's prerequisites are present and
	// reachable. The gear will go through Initialize/Start normally.
	ProbeStatusAvailable ProbeStatus = "available"

	// ProbeStatusNotInstalled means the thing the gear manages isn't on
	// this host at all. Expected on hosts that don't run this software.
	ProbeStatusNotInstalled ProbeStatus = "not_installed"

	// ProbeStatusInaccessible means the prerequisites exist but the agent
	// can't reach them — bind mount missing, permission denied, socket
	// unreadable. The fix is access, not installation.
	ProbeStatusInaccessible ProbeStatus = "inaccessible"

	// ProbeStatusDisabled means the gear was turned off by configuration.
	// Reserved for a future GEARBOX_AGENT_DISABLE_GEARS-style mechanism;
	// no gear returns this yet.
	ProbeStatusDisabled ProbeStatus = "disabled"
)

// ProbeResult is what a ProbeableGear returns from Probe(). Status drives
// the manager's load decision; Reason is a human-readable sentence shown
// in the startup table and the capabilities API.
type ProbeResult struct {
	// Status is the verdict. Only Available causes the gear to load.
	Status ProbeStatus

	// Reason is a free-text sentence written for an operator: name the
	// surface that was probed and what was wrong. Mandatory unless the
	// status is Available (in which case it may describe what was found).
	Reason string

	// Capabilities is optional detected facts the gear wants to surface
	// (e.g. "haproxy_version": "2.8.5", "stats_socket": "/run/haproxy/admin.sock").
	// Populated mainly when Status == Available. Flat key/value map —
	// use Resources for structured lists.
	Capabilities map[string]string

	// Resources is optional structured data the gear wants to advertise
	// to the dashboard, beyond what fits in the flat Capabilities map.
	// The shape is gear-specific; the dashboard reads each gear's
	// known keys explicitly. Typical use:
	//
	//   - access-log: "log_sources" → []map[string]string of
	//     {"name", "display_name", "path"} per discovered web server
	//     log file. The dashboard's Logs page populates its source
	//     dropdown from this instead of inferring sources from gear
	//     availability flags.
	//
	// Stable across the wire: serialized to JSON as part of the
	// /api/v1/system/capabilities response. Gears that omit it stay
	// fully backward-compatible — the dashboard treats a missing
	// Resources as "fall back to the older capability-flag heuristic".
	//
	// Issue #112 Phase 2 extension: dashboard consumers in
	// gearbox/internal/framework/agent.CapabilityEntry mirror this
	// field as map[string]json.RawMessage for typed-per-gear decoding.
	Resources map[string]any
}

// IsAvailable reports whether the gear should be loaded.
func (r ProbeResult) IsAvailable() bool {
	return r.Status == ProbeStatusAvailable
}

// ProbeAvailable returns an available ProbeResult with an optional reason
// (typically the detected version or path) and capabilities map.
func ProbeAvailable(reason string, capabilities map[string]string) ProbeResult {
	return ProbeResult{Status: ProbeStatusAvailable, Reason: reason, Capabilities: capabilities}
}

// ProbeAvailableWithResources is ProbeAvailable plus the structured
// Resources field. Gears that need to publish typed resource lists
// (log sources, service catalogs, metric sources, …) to the dashboard
// use this constructor; gears that only need the flat Capabilities map
// continue to use ProbeAvailable. Issue #112 Phase 2 extension.
func ProbeAvailableWithResources(reason string, capabilities map[string]string, resources map[string]any) ProbeResult {
	return ProbeResult{
		Status:       ProbeStatusAvailable,
		Reason:       reason,
		Capabilities: capabilities,
		Resources:    resources,
	}
}

// ProbeNotInstalled returns a result for the case where the software the
// gear manages isn't on this host.
func ProbeNotInstalled(reason string) ProbeResult {
	return ProbeResult{Status: ProbeStatusNotInstalled, Reason: reason}
}

// ProbeInaccessible returns a result for the case where prereqs exist but
// the agent can't reach them. Use when the fix is access, not installation.
func ProbeInaccessible(reason string) ProbeResult {
	return ProbeResult{Status: ProbeStatusInaccessible, Reason: reason}
}

// ProbeDisabled returns a result for gears that have been turned off by
// configuration.
func ProbeDisabled(reason string) ProbeResult {
	return ProbeResult{Status: ProbeStatusDisabled, Reason: reason}
}

// ProbeableGear is implemented by gears that can self-report whether they
// have what they need to run on the current host. Gears that do not
// implement this interface are treated as always-available.
//
// Probe runs before Initialize. A non-Available result causes the manager
// to skip Initialize, Start, and route registration for that gear — it
// does not exist for this run.
type ProbeableGear interface {
	Gear

	// Probe inspects the host for the gear's prerequisites and returns the
	// verdict. It must be side-effect-free: do not connect to anything,
	// do not mutate state, do not log loudly. The manager logs a single
	// summary line per probe.
	Probe(ctx context.Context, deps Dependencies) ProbeResult
}
