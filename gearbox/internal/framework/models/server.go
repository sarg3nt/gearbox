package models

// ServerConfig represents the configuration for a single HAProxy server to monitor.
type ServerConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Agent API connection
	AgentURL string `json:"agent_url,omitempty"` // e.g., "https://light-hugger.sarg3.net:8405"
	APIKey   string `json:"api_key,omitempty"`   // Bearer token for Agent API
}

// UsesAgentAPI returns true if this server is configured to use the Agent API.
func (s *ServerConfig) UsesAgentAPI() bool {
	return s.AgentURL != "" && s.APIKey != ""
}

// Validate checks if the server configuration is valid.
func (s *ServerConfig) Validate() error {
	if s.ID == "" {
		return ErrInvalidConfig("server id is required")
	}
	if s.Name == "" {
		return ErrInvalidConfig("server name is required")
	}

	// Server must have Agent API configured
	if !s.UsesAgentAPI() {
		return ErrInvalidConfig("server must have agent_url and api_key configured for server " + s.ID)
	}

	return nil
}

// AppConfig represents the overall application configuration.
type AppConfig struct {
	Servers                 []ServerConfig
	AdminPassword           string // Initial admin password (optional, will be generated if not set)
	SessionSecret           string
	SessionTimeoutMinutes   int
	DashboardRefreshSeconds int
	ConfigRefreshSeconds    int
	HTTPPort                string
	LogLevel                string
	TLSCertPath             string
	TLSKeyPath              string
	BaseURL                 string // Base URL for email links (e.g., https://gearbox.example.com)
	// Database settings
	DatabasePath           string
	DatabaseRetentionHours int
	HistoryIntervalSeconds int
	// WebAuthn settings for passkey support
	WebAuthnRPDisplayName string   // Display name for the relying party (e.g., "Gearbox")
	WebAuthnRPID          string   // Relying Party ID - domain name (e.g., "gearbox.example.com")
	WebAuthnRPOrigins     []string // Allowed origins (e.g., ["https://gearbox.example.com"])
	// Asset loading configuration
	UseLocalAssets bool // If true, load JS/CSS from local files instead of CDN (for CSP-compliant development)
}

// Note: User struct is now defined in user.go with enhanced fields

// ErrInvalidConfig represents a configuration validation error.
type ErrInvalidConfig string

func (e ErrInvalidConfig) Error() string {
	return "invalid configuration: " + string(e)
}
