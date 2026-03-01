package plugin

import (
	"database/sql"
	"log/slog"
	"net/http"
)

// Dependencies contains services provided by the framework to plugins.
// All plugins receive the same Dependencies instance during initialization.
type Dependencies struct {
	// DB is the database connection.
	DB *sql.DB

	// Logger is the structured logger for the plugin.
	// Each plugin should create a child logger with its name.
	Logger *slog.Logger

	// EventHub allows plugins to publish and subscribe to events.
	EventHub EventPublisher

	// Auth provides authentication and authorization services.
	Auth AuthChecker

	// Agent provides communication with the HAProxy agent.
	Agent AgentClient

	// Servers provides access to server configurations.
	Servers ServerRegistry

	// HTTPClient is a pre-configured HTTP client for external requests.
	HTTPClient *http.Client

	// Config contains the plugin's configuration from the database.
	// This is loaded from the integrations/plugins table.
	Config map[string]any
}

// EventPublisher allows plugins to publish events to the event hub.
type EventPublisher interface {
	// Publish broadcasts an event to all subscribers.
	Publish(event Event)

	// Subscribe registers a handler for events of a specific type.
	// Returns a function to unsubscribe.
	Subscribe(eventType string, handler func(Event)) func()
}

// Event represents an event in the system.
type Event struct {
	// Type identifies the event (e.g., "stats.updated", "alert.triggered").
	Type string

	// ServerID is the optional server this event relates to.
	ServerID string

	// Data contains event-specific data.
	Data map[string]any
}

// AuthChecker provides authentication and authorization checks.
type AuthChecker interface {
	// IsAuthenticated checks if the request has a valid session.
	IsAuthenticated(r *http.Request) bool

	// GetUser retrieves the authenticated user from the request.
	// Returns nil if not authenticated.
	GetUser(r *http.Request) *User

	// HasPermission checks if the current user has a specific permission.
	HasPermission(r *http.Request, component, action string) bool

	// GetCSRFToken retrieves the CSRF token from the session.
	GetCSRFToken(r *http.Request) (string, error)

	// ValidateCSRFToken validates the CSRF token in the request.
	ValidateCSRFToken(r *http.Request) error
}

// FullAuthChecker extends AuthChecker with methods that return full model types.
// Use this interface when plugins need access to complete user/permission information.
type FullAuthChecker interface {
	AuthChecker

	// GetPluginUserPermissions retrieves the user's full permission set.
	// Returns a UserPermissions object with all component-action permissions.
	GetPluginUserPermissions(r *http.Request) (*UserPermissions, error)

	// GetFullUser retrieves the complete user model from the request context.
	// Returns nil if not authenticated. This provides access to all user fields
	// that templates may need.
	GetFullUser(r *http.Request) *FullUser
}

// FullUser represents a complete user model for templates.
// This mirrors models.User for use in plugins.
type FullUser struct {
	ID                 string // UUID
	Email              string
	FirstName          string
	LastName           string
	Role               string
	MustChangePassword bool
}

// IsAdmin returns true if the user has admin role.
func (u *FullUser) IsAdmin() bool {
	return u.Role == "admin"
}

// DisplayName returns the user's display name.
func (u *FullUser) DisplayName() string {
	if u.FirstName != "" && u.LastName != "" {
		return u.FirstName + " " + u.LastName
	}
	if u.FirstName != "" {
		return u.FirstName
	}
	return u.Email
}

// UserPermissions represents a user's permission set.
type UserPermissions struct {
	Permissions map[string]map[string]bool // component -> action -> allowed
}

// HasPermission checks if a specific permission is granted.
func (p *UserPermissions) HasPermission(component, action string) bool {
	if p == nil || p.Permissions == nil {
		return false
	}
	if actions, ok := p.Permissions[component]; ok {
		return actions[action]
	}
	return false
}

// User represents a user for plugins.
// This is a simplified view of the user model.
type User struct {
	ID        string // UUID
	Email     string
	FirstName string
	LastName  string
	IsAdmin   bool
}

// DisplayName returns the user's display name.
func (u *User) DisplayName() string {
	if u.FirstName != "" && u.LastName != "" {
		return u.FirstName + " " + u.LastName
	}
	if u.FirstName != "" {
		return u.FirstName
	}
	return u.Email
}

// AgentClient provides communication with the HAProxy agent.
type AgentClient interface {
	// IsConnected returns true if the agent is connected.
	IsConnected() bool

	// Get performs a GET request to the agent API.
	Get(path string, result any) error

	// Post performs a POST request to the agent API.
	Post(path string, body, result any) error

	// Delete performs a DELETE request to the agent API.
	Delete(path string, result any) error
}

// PluginStore provides storage operations for plugins.
// Plugins can use this to persist their configuration and state.
type PluginStore interface {
	// GetConfig retrieves the plugin's configuration.
	GetConfig(pluginName string) (map[string]any, error)

	// SaveConfig saves the plugin's configuration.
	SaveConfig(pluginName string, config map[string]any) error

	// IsEnabled checks if the plugin is enabled for the given server.
	IsEnabled(pluginName, serverID string) bool

	// SetEnabled enables or disables the plugin for a server.
	SetEnabled(pluginName, serverID string, enabled bool) error

	// GetOrder returns the display order for the plugin.
	GetOrder(pluginName, serverID string) int

	// SetOrder sets the display order for the plugin.
	SetOrder(pluginName, serverID string, order int) error
}

// ServerConfig represents a server configuration for plugins.
// This is a simplified view that plugins can use without direct database access.
type ServerConfig struct {
	ID       string
	Name     string
	AgentURL string
}

// ServerRegistry provides access to server configurations.
// Plugins use this instead of direct database access for server information.
type ServerRegistry interface {
	// GetEnabledBoxes returns all boxes that are currently enabled.
	// Returns simplified ServerConfig for plugin use.
	GetEnabledBoxes() []ServerConfig

	// GetServer returns a specific server by ID.
	GetServer(id string) (*ServerConfig, bool)

	// IsPluginEnabled checks if a specific integration/plugin is enabled for a server.
	IsPluginEnabled(serverID, integration string) bool
}

// FullServerRegistry extends ServerRegistry with methods that return full model types.
// Use this interface when plugins need access to complete server configuration.
type FullServerRegistry interface {
	ServerRegistry

	// GetFullServers returns servers with full model information.
	// This is useful for templates that need complete server details.
	GetFullServers() []FullServerConfig
}

// FullServerConfig represents a complete server configuration.
// This mirrors models.BoxConfig for use in plugins and templates.
type FullServerConfig struct {
	ID       string
	Name     string
	AgentURL string
	APIKey   string
	Enabled  bool
}
