// Package gear provides the core gear system for the HAProxy monitoring application.
// It follows the compile-time gear pattern (similar to Caddy) where plugins register
// themselves via init() functions and implement a common Gear interface.
package gear

import (
	"context"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
)

// Gear is the main interface that all gears must implement.
// Gears are self-contained modules that provide specific functionality
// to the monitoring application.
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

	// RegisterRoutes registers HTTP routes for this gear.
	// The router is mounted at the gear's base path (e.g., /logs).
	RegisterRoutes(r chi.Router)

	// SidebarItem returns configuration for this gear's sidebar entry.
	// Return nil if the gear should not appear in the sidebar.
	SidebarItem() *SidebarConfig

	// SettingsPage returns a templ component for the gear's settings page.
	// Return nil if the gear has no configurable settings.
	// The config parameter contains the gear's current configuration.
	SettingsPage(config map[string]any) templ.Component

	// Permissions returns the permissions this gear requires.
	// These are registered with the permission system on startup.
	Permissions() []PermissionDef

	// Migrations returns database migrations for this gear.
	// Migrations are run in order during application startup.
	Migrations() []Migration
}

// Info contains metadata about a gear.
type Info struct {
	// Name is the internal identifier (e.g., "logs", "metrics").
	// Must be unique across all plugins.
	Name string

	// DisplayName is shown in the UI (e.g., "System Logs").
	DisplayName string

	// Description provides a detailed description of the gear.
	Description string

	// Version is the semantic version (e.g., "1.0.0").
	Version string

	// Icon is the icon identifier used in the sidebar.
	Icon string

	// Category groups related plugins (e.g., "monitoring", "security", "system").
	Category string

	// Author is optional author information.
	Author string

	// Website is an optional documentation URL.
	Website string

	// Core indicates this is a core gear that cannot be disabled.
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

// SidebarConfig defines how a gear appears in the navigation sidebar.
type SidebarConfig struct {
	// Path is the URL path (e.g., "/logs").
	Path string

	// Icon returns the SVG icon component for the sidebar.
	Icon templ.Component

	// DefaultOrder is the default sort order (lower numbers appear first).
	DefaultOrder int

	// BadgeProvider returns a badge count (e.g., unread alerts).
	// Return 0 to hide the badge.
	BadgeProvider func() int

	// ShowAlways shows this item even when the gear is disabled.
	// Used for core plugins like Dashboard.
	ShowAlways bool

	// RequiresPermission specifies the permission needed to see this item.
	// If empty, visible to all authenticated users.
	RequiresPermission string
}

// PermissionDef defines a permission that a gear uses.
type PermissionDef struct {
	// Component is the permission component name (e.g., "logs").
	Component string

	// Actions are the available actions (e.g., "view", "configure", "manage").
	Actions []string

	// Description is a human-readable description of the permission.
	Description string
}

// Migration defines a database migration for a gear.
type Migration struct {
	// Version is a sequential version number.
	// Migrations are run in version order.
	Version int

	// Description describes what this migration does.
	Description string

	// Up is the SQL to apply the migration.
	Up string

	// Down is the SQL to revert the migration.
	Down string
}

// CollectorGear is implemented by plugins that collect data periodically.
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
	Interval time.Duration

	// Collect runs the data collection.
	// Returns the collected data and any error.
	Collect func(ctx context.Context) (any, error)

	// OnData is called with collected data.
	// Gears can use this to store data or publish events.
	OnData func(data any) error
}

// WebSocketGear is implemented by plugins that handle WebSocket connections.
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

	// SubscribedEvents returns the event types this gear handles.
	SubscribedEvents() []string

	// HandleEvent is called when a subscribed event occurs.
	HandleEvent(eventType string, payload any)
}

// SearchableGear is implemented by gears that support global search.
type SearchableGear interface {
	Gear

	// Search returns results matching the query.
	Search(ctx context.Context, query string) ([]SearchResult, error)
}

// SearchResult represents a search result from a gear.
type SearchResult struct {
	// Title is the result title.
	Title string

	// Description provides context about the result.
	Description string

	// URL is the link to the result.
	URL string

	// Relevance is a score from 0 to 1.
	Relevance float64

	// Plugin is the name of the gear that provided this result.
	Gear string
}

