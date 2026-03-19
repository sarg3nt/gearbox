package models

// BoxConfig represents the configuration for a single monitored box (server or workstation).
type BoxConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Agent API connection
	AgentURL       string `json:"agent_url,omitempty"`        // e.g., "https://light-hugger.sarg3.net:8405"
	APIKey         string `json:"api_key,omitempty"`          // Bearer token for Agent API
	SkipTLSVerify  bool   `json:"skip_tls_verify,omitempty"`  // Skip TLS certificate verification (for self-signed certs)
}

// UsesAgentAPI returns true if this box is configured to use the Agent API.
func (b *BoxConfig) UsesAgentAPI() bool {
	return b.AgentURL != "" && b.APIKey != ""
}

// Validate checks if the box configuration is valid.
func (b *BoxConfig) Validate() error {
	if b.ID == "" {
		return ErrInvalidConfig("box id is required")
	}
	if b.Name == "" {
		return ErrInvalidConfig("box name is required")
	}

	// Box must have Agent API configured
	if !b.UsesAgentAPI() {
		return ErrInvalidConfig("box must have agent_url and api_key configured for box " + b.ID)
	}

	return nil
}

// AppConfig represents the overall application configuration.
type AppConfig struct {
	Boxes                   []BoxConfig
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
