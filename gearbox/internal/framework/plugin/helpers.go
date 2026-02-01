// Package plugin provides the plugin system for the HAProxy monitoring application.
// This package defines interfaces for plugins to interact with the framework,
// provides base implementations for common plugin patterns, and helper functions
// to reduce boilerplate code in plugin handlers.
package plugin

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
)

// BasePlugin provides a default implementation of the Plugin interface.
// Embed this struct in your plugin to get sensible defaults for optional methods.
type BasePlugin struct {
	info   Info
	deps   Dependencies
	logger *slog.Logger
}

// NewBasePlugin creates a new BasePlugin with the given info.
func NewBasePlugin(info Info) *BasePlugin {
	return &BasePlugin{
		info: info,
	}
}

// Info returns the plugin metadata.
func (b *BasePlugin) Info() Info {
	return b.info
}

// Initialize stores the dependencies for later use.
// Override this method if you need custom initialization logic.
func (b *BasePlugin) Initialize(ctx context.Context, deps Dependencies) error {
	b.deps = deps
	b.logger = deps.Logger
	return nil
}

// Start is a no-op by default.
// Override this method if your plugin needs to start background tasks.
func (b *BasePlugin) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op by default.
// Override this method if your plugin needs to clean up resources.
func (b *BasePlugin) Stop(ctx context.Context) error {
	return nil
}

// Health returns a healthy status by default.
// Override this method if your plugin needs custom health checks.
func (b *BasePlugin) Health() HealthStatus {
	return HealthStatus{
		Status:    HealthStatusHealthy,
		Message:   "ok",
		LastCheck: time.Now(),
	}
}

// RegisterRoutes is a no-op by default.
// Override this method to register your plugin's HTTP routes.
func (b *BasePlugin) RegisterRoutes(r chi.Router) {
	// No routes by default
}

// SidebarItem returns nil by default (no sidebar entry).
// Override this method to add a sidebar entry for your plugin.
func (b *BasePlugin) SidebarItem() *SidebarConfig {
	return nil
}

// SettingsPage returns nil by default (no settings page).
// Override this method to provide a settings page for your plugin.
func (b *BasePlugin) SettingsPage(config map[string]any) templ.Component {
	return nil
}

// Permissions returns an empty slice by default.
// Override this method to define permissions for your plugin.
func (b *BasePlugin) Permissions() []PermissionDef {
	return nil
}

// Migrations returns an empty slice by default.
// Override this method to define database migrations for your plugin.
func (b *BasePlugin) Migrations() []Migration {
	return nil
}

// GetDeps returns the plugin's dependencies.
// Use this from your plugin implementation to access framework services.
func (b *BasePlugin) GetDeps() Dependencies {
	return b.deps
}

// GetLogger returns the plugin's logger.
func (b *BasePlugin) GetLogger() *slog.Logger {
	if b.logger == nil {
		return slog.Default()
	}
	return b.logger
}

// GetDB returns the database connection.
// Returns the *sql.DB instance for direct database access.
func (b *BasePlugin) GetDB() *sql.DB {
	return b.deps.DB
}

// GetEventHub returns the event publisher.
func (b *BasePlugin) GetEventHub() EventPublisher {
	return b.deps.EventHub
}

// GetAuth returns the auth checker.
func (b *BasePlugin) GetAuth() AuthChecker {
	return b.deps.Auth
}

// GetAgent returns the agent client.
func (b *BasePlugin) GetAgent() AgentClient {
	return b.deps.Agent
}

// GetConfig returns the plugin's configuration.
func (b *BasePlugin) GetConfig() map[string]any {
	return b.deps.Config
}

// GetConfigString returns a string config value or the default.
func (b *BasePlugin) GetConfigString(key, defaultValue string) string {
	if b.deps.Config == nil {
		return defaultValue
	}
	if v, ok := b.deps.Config[key].(string); ok {
		return v
	}
	return defaultValue
}

// GetConfigInt returns an integer config value or the default.
func (b *BasePlugin) GetConfigInt(key string, defaultValue int) int {
	if b.deps.Config == nil {
		return defaultValue
	}
	switch v := b.deps.Config[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return defaultValue
}

// GetConfigBool returns a boolean config value or the default.
func (b *BasePlugin) GetConfigBool(key string, defaultValue bool) bool {
	if b.deps.Config == nil {
		return defaultValue
	}
	if v, ok := b.deps.Config[key].(bool); ok {
		return v
	}
	return defaultValue
}

// PublishEvent is a helper to publish an event.
func (b *BasePlugin) PublishEvent(eventType string, serverID string, data map[string]any) {
	if b.deps.EventHub != nil {
		b.deps.EventHub.Publish(Event{
			Type:     eventType,
			ServerID: serverID,
			Data:     data,
		})
	}
}

// HealthyStatus returns a healthy status with a custom message.
func HealthyStatus(message string) HealthStatus {
	return HealthStatus{
		Status:    HealthStatusHealthy,
		Message:   message,
		LastCheck: time.Now(),
	}
}

// DegradedStatus returns a degraded status with a message.
func DegradedStatus(message string) HealthStatus {
	return HealthStatus{
		Status:    HealthStatusDegraded,
		Message:   message,
		LastCheck: time.Now(),
	}
}

// UnhealthyStatus returns an unhealthy status with a message.
func UnhealthyStatus(message string) HealthStatus {
	return HealthStatus{
		Status:    HealthStatusUnhealthy,
		Message:   message,
		LastCheck: time.Now(),
	}
}

// -----------------------------------------------------------------------------
// HTTP Request Helpers
// -----------------------------------------------------------------------------

// RequireAuth checks authentication and returns the user, or writes an error response.
// Returns nil if the user is not authenticated (response already written).
// Use this at the start of handler functions to enforce authentication.
//
// Example:
//
//	func (h *Handlers) MyPage(w http.ResponseWriter, r *http.Request) {
//	    user := plugin.RequireAuth(h.deps.Auth, w, r)
//	    if user == nil {
//	        return // Response already sent
//	    }
//	    // ... rest of handler
//	}
func RequireAuth(auth AuthChecker, w http.ResponseWriter, r *http.Request) *User {
	if !auth.IsAuthenticated(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil
	}

	user := auth.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil
	}

	return user
}

// RequirePermission checks if the user has the required permission.
// Returns false if permission denied (response already written with 403 Forbidden).
// Use this after RequireAuth to enforce specific permissions.
//
// Example:
//
//	if !plugin.RequirePermission(h.deps.Auth, w, r, "logs", "view") {
//	    return // 403 response already sent
//	}
func RequirePermission(auth AuthChecker, w http.ResponseWriter, r *http.Request, component, action string) bool {
	if !auth.HasPermission(r, component, action) {
		http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
		return false
	}
	return true
}

// GetServersOrRedirect returns enabled servers or redirects to home if none configured.
// Returns nil if redirected (response already written).
// Use this when a page requires at least one configured server.
func GetServersOrRedirect(servers ServerRegistry, w http.ResponseWriter, r *http.Request) []ServerConfig {
	enabledServers := servers.GetEnabledBoxes()
	if len(enabledServers) == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
	}
	return enabledServers
}

// AsFullServerRegistry attempts to cast ServerRegistry to FullServerRegistry.
// Returns nil if the cast fails. Use this when you need access to
// GetFullServers() for templates that require complete server details.
func AsFullServerRegistry(servers ServerRegistry) FullServerRegistry {
	if full, ok := servers.(FullServerRegistry); ok {
		return full
	}
	return nil
}

// AsFullAuthChecker attempts to cast AuthChecker to FullAuthChecker.
// Returns nil if the cast fails. Use this when you need access to
// GetPluginUserPermissions() or GetFullUser().
func AsFullAuthChecker(auth AuthChecker) FullAuthChecker {
	if full, ok := auth.(FullAuthChecker); ok {
		return full
	}
	return nil
}

// -----------------------------------------------------------------------------
// Configuration Helpers for Plugin Settings Pages
// -----------------------------------------------------------------------------
// These functions are used by plugin settings templ components to extract
// typed values from config maps. They handle JSON unmarshaling quirks
// (e.g., numbers as float64) and provide sensible defaults.

// ConfigInt retrieves an integer value from a config map with a default.
// Handles int, int64, and float64 types (for JSON unmarshaling compatibility).
func ConfigInt(config map[string]any, key string, defaultValue int) int {
	if config == nil {
		return defaultValue
	}
	switch v := config[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return defaultValue
}

// ConfigBool retrieves a boolean value from a config map with a default.
func ConfigBool(config map[string]any, key string, defaultValue bool) bool {
	if config == nil {
		return defaultValue
	}
	if v, ok := config[key].(bool); ok {
		return v
	}
	return defaultValue
}

// ConfigString retrieves a string value from a config map with a default.
func ConfigString(config map[string]any, key string, defaultValue string) string {
	if config == nil {
		return defaultValue
	}
	if v, ok := config[key].(string); ok {
		return v
	}
	return defaultValue
}
