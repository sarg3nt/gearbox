// Package plugin provides the core plugin system for the HAProxy monitoring application.
// It follows the compile-time plugin pattern (similar to Caddy) where plugins register
// themselves via init() functions and implement a common Plugin interface.
package plugin

import (
	"context"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
)

// Plugin is the main interface that all plugins must implement.
// Plugins are self-contained modules that provide specific functionality
// to the monitoring application.
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

	// RegisterRoutes registers HTTP routes for this plugin.
	// The router is mounted at the plugin's base path (e.g., /logs).
	RegisterRoutes(r chi.Router)

	// SidebarItem returns configuration for this plugin's sidebar entry.
	// Return nil if the plugin should not appear in the sidebar.
	SidebarItem() *SidebarConfig

	// SettingsPage returns a templ component for the plugin's settings page.
	// Return nil if the plugin has no configurable settings.
	// The config parameter contains the plugin's current configuration.
	SettingsPage(config map[string]any) templ.Component

	// Permissions returns the permissions this plugin requires.
	// These are registered with the permission system on startup.
	Permissions() []PermissionDef

	// Migrations returns database migrations for this plugin.
	// Migrations are run in order during application startup.
	Migrations() []Migration
}

// Info contains metadata about a plugin.
type Info struct {
	// Name is the internal identifier (e.g., "logs", "metrics").
	// Must be unique across all plugins.
	Name string

	// DisplayName is shown in the UI (e.g., "System Logs").
	DisplayName string

	// Description provides a detailed description of the plugin.
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

// SidebarConfig defines how a plugin appears in the navigation sidebar.
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

	// ShowAlways shows this item even when the plugin is disabled.
	// Used for core plugins like Dashboard.
	ShowAlways bool

	// RequiresPermission specifies the permission needed to see this item.
	// If empty, visible to all authenticated users.
	RequiresPermission string
}

// PermissionDef defines a permission that a plugin uses.
type PermissionDef struct {
	// Component is the permission component name (e.g., "logs").
	Component string

	// Actions are the available actions (e.g., "view", "configure", "manage").
	Actions []string

	// Description is a human-readable description of the permission.
	Description string
}

// Migration defines a database migration for a plugin.
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
	Interval time.Duration

	// Collect runs the data collection.
	// Returns the collected data and any error.
	Collect func(ctx context.Context) (any, error)

	// OnData is called with collected data.
	// Plugins can use this to store data or publish events.
	OnData func(data any) error
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

// SearchablePlugin is implemented by plugins that support global search.
type SearchablePlugin interface {
	Plugin

	// Search returns results matching the query.
	Search(ctx context.Context, query string) ([]SearchResult, error)
}

// SearchResult represents a search result from a plugin.
type SearchResult struct {
	// Title is the result title.
	Title string

	// Description provides context about the result.
	Description string

	// URL is the link to the result.
	URL string

	// Relevance is a score from 0 to 1.
	Relevance float64

	// Plugin is the name of the plugin that provided this result.
	Plugin string
}

