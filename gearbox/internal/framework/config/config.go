package config

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// secretEnvVars is a list of environment variable names that contain secrets
var secretEnvVars = map[string]bool{
	"ADMIN_PASSWORD":         true,
	"SESSION_SECRET":         true,
	"HAPROXY_SERVERS":        true, // Contains API keys and tokens
	"WEBAUTHN_RP_ID":         false,
	"WEBAUTHN_RP_DISPLAY":    false,
	"WEBAUTHN_RP_ORIGINS":    false,
	"AGENT_CA_CERT_PATH":     false,
	"GEARBOX_INSECURE_TLS":   false,
	"CSP_REPORT_URI":         false,
	"CSP_EXTRA_SOURCES":      false,
	"AGENT_ALLOWED_ORIGINS":  false,
}

// Load parses environment variables and returns the application configuration.
func Load() (*models.AppConfig, error) {
	cfg := &models.AppConfig{
		SessionTimeoutMinutes:   60,
		DashboardRefreshSeconds: 30,
		ConfigRefreshSeconds:    60,
		HTTPPort:                "3000",
		LogLevel:                "info",
		DatabasePath:            "/data/gearbox.db",
		DatabaseRetentionHours:  168, // 7 days
		HistoryIntervalSeconds:  60,  // Save history every minute
	}

	// Parse HAPROXY_SERVERS (DEPRECATED - servers now managed via database)
	// This is kept only for backward compatibility during migration
	serversJSON := os.Getenv("HAPROXY_SERVERS")
	if serversJSON != "" {
		log.Println("WARNING: HAPROXY_SERVERS env var is deprecated. Servers will be migrated to database on first run.")
		log.Println("         After migration, you can remove HAPROXY_SERVERS from your environment.")

		if err := json.Unmarshal([]byte(serversJSON), &cfg.Servers); err != nil {
			return nil, fmt.Errorf("failed to parse HAPROXY_SERVERS: %w", err)
		}
	} else {
		// No ENV var - servers will be loaded from database
		cfg.Servers = []models.ServerConfig{}
	}

	// Validate each server and resolve env var references
	for i := range cfg.Servers {
		if err := resolveServerEnvVars(&cfg.Servers[i]); err != nil {
			return nil, fmt.Errorf("failed to resolve env vars for server %s: %w", cfg.Servers[i].ID, err)
		}
		if err := cfg.Servers[i].Validate(); err != nil {
			return nil, err
		}
	}

	// Parse ADMIN_PASSWORD (optional - will be generated if not set)
	cfg.AdminPassword = os.Getenv("ADMIN_PASSWORD")
	// Note: If not set, a random password will be generated and logged on startup

	// Parse SESSION_SECRET (required)
	cfg.SessionSecret = os.Getenv("SESSION_SECRET")
	if cfg.SessionSecret == "" {
		return nil, fmt.Errorf("SESSION_SECRET environment variable is required")
	}
	if len(cfg.SessionSecret) < 32 {
		return nil, fmt.Errorf("SESSION_SECRET must be at least 32 characters long")
	}

	// Parse BASE_URL (optional, for email links)
	cfg.BaseURL = os.Getenv("BASE_URL")
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:3000"
	}

	// Optional settings
	if val := os.Getenv("SESSION_TIMEOUT_MINUTES"); val != "" {
		timeout, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("invalid SESSION_TIMEOUT_MINUTES: %w", err)
		}
		cfg.SessionTimeoutMinutes = timeout
	}

	if val := os.Getenv("DASHBOARD_REFRESH_SECONDS"); val != "" {
		refresh, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("invalid DASHBOARD_REFRESH_SECONDS: %w", err)
		}
		cfg.DashboardRefreshSeconds = refresh
	}

	if val := os.Getenv("CONFIG_REFRESH_SECONDS"); val != "" {
		refresh, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("invalid CONFIG_REFRESH_SECONDS: %w", err)
		}
		cfg.ConfigRefreshSeconds = refresh
	}

	if val := os.Getenv("HTTP_PORT"); val != "" {
		cfg.HTTPPort = val
	}

	if val := os.Getenv("LOG_LEVEL"); val != "" {
		cfg.LogLevel = val
	}

	if val := os.Getenv("TLS_CERT_PATH"); val != "" {
		cfg.TLSCertPath = val
	}

	if val := os.Getenv("TLS_KEY_PATH"); val != "" {
		cfg.TLSKeyPath = val
	}

	// Database settings
	if val := os.Getenv("DATABASE_PATH"); val != "" {
		cfg.DatabasePath = val
	}

	if val := os.Getenv("DATABASE_RETENTION_HOURS"); val != "" {
		retention, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("invalid DATABASE_RETENTION_HOURS: %w", err)
		}
		cfg.DatabaseRetentionHours = retention
	}

	if val := os.Getenv("HISTORY_INTERVAL_SECONDS"); val != "" {
		interval, err := strconv.Atoi(val)
		if err != nil {
			return nil, fmt.Errorf("invalid HISTORY_INTERVAL_SECONDS: %w", err)
		}
		cfg.HistoryIntervalSeconds = interval
	}

	// Configure WebAuthn from BASE_URL (for passkey support)
	if err := configureWebAuthn(cfg); err != nil {
		return nil, err
	}

	// Asset loading configuration (for CSP-compliant local development)
	if val := os.Getenv("USE_LOCAL_ASSETS"); val == "true" {
		cfg.UseLocalAssets = true
		log.Println("✓ USE_LOCAL_ASSETS enabled - using local assets from /static/js/vendor")
	} else {
		log.Println("ℹ USE_LOCAL_ASSETS not set - using CDN assets (production mode)")
	}

	return cfg, nil
}

// configureWebAuthn sets up WebAuthn configuration based on BASE_URL.
func configureWebAuthn(cfg *models.AppConfig) error {
	// Parse the base URL to extract domain
	parsedURL, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid BASE_URL for WebAuthn: %w", err)
	}

	// Set display name
	cfg.WebAuthnRPDisplayName = "Gearbox"
	if val := os.Getenv("WEBAUTHN_RP_DISPLAY_NAME"); val != "" {
		cfg.WebAuthnRPDisplayName = val
	}

	// Set RP ID (domain without port)
	cfg.WebAuthnRPID = parsedURL.Hostname()
	if val := os.Getenv("WEBAUTHN_RP_ID"); val != "" {
		cfg.WebAuthnRPID = val
	}

	// Set allowed origins
	// Build origin from scheme + host (including port if non-standard)
	origin := parsedURL.Scheme + "://" + parsedURL.Host
	cfg.WebAuthnRPOrigins = []string{origin}

	// Allow additional origins via env var (comma-separated)
	if val := os.Getenv("WEBAUTHN_RP_ORIGINS"); val != "" {
		cfg.WebAuthnRPOrigins = strings.Split(val, ",")
	}

	return nil
}

// LogConfigDebug logs all configuration values when in debug mode.
// Secrets are masked with asterisks.
func LogConfigDebug(cfg *models.AppConfig, logger *slog.Logger) {
	if cfg.LogLevel != "debug" {
		return
	}

	logger.Info("========================================")
	logger.Info("DEBUG: Environment Variables")
	logger.Info("========================================")

	// List of env vars we care about
	envVars := []string{
		"HAPROXY_SERVERS",
		"ADMIN_PASSWORD",
		"SESSION_SECRET",
		"BASE_URL",
		"SESSION_TIMEOUT_MINUTES",
		"DASHBOARD_REFRESH_SECONDS",
		"CONFIG_REFRESH_SECONDS",
		"HTTP_PORT",
		"LOG_LEVEL",
		"TLS_CERT_PATH",
		"TLS_KEY_PATH",
		"DATABASE_PATH",
		"DATABASE_RETENTION_HOURS",
		"HISTORY_INTERVAL_SECONDS",
	}

	for _, name := range envVars {
		value := os.Getenv(name)
		if value == "" {
			logger.Info("env var", "name", name, "value", "<not set>")
		} else if isSecretEnvVar(name) {
			// SECURITY: Redact secrets from debug logs
			logger.Info("env var", "name", name, "value", "********", "length", len(value))
		} else {
			logger.Info("env var", "name", name, "value", value)
		}
	}

	logger.Info("========================================")
	logger.Info("DEBUG: Parsed Configuration")
	logger.Info("========================================")
	logger.Info("servers configured", "count", len(cfg.Servers))
	for _, s := range cfg.Servers {
		logger.Info("server", "id", s.ID, "name", s.Name)
	}
	logger.Info("config", "admin_password", maskSecret(cfg.AdminPassword))
	logger.Info("config", "session_secret", maskSecret(cfg.SessionSecret))
	logger.Info("config", "base_url", cfg.BaseURL)
	logger.Info("config", "session_timeout_minutes", cfg.SessionTimeoutMinutes)
	logger.Info("config", "dashboard_refresh_seconds", cfg.DashboardRefreshSeconds)
	logger.Info("config", "config_refresh_seconds", cfg.ConfigRefreshSeconds)
	logger.Info("config", "http_port", cfg.HTTPPort)
	logger.Info("config", "log_level", cfg.LogLevel)
	logger.Info("config", "database_path", cfg.DatabasePath)
	logger.Info("config", "database_retention_hours", cfg.DatabaseRetentionHours)
	logger.Info("config", "history_interval_seconds", cfg.HistoryIntervalSeconds)
	if cfg.TLSCertPath != "" {
		logger.Info("config", "tls_cert_path", cfg.TLSCertPath)
		logger.Info("config", "tls_key_path", cfg.TLSKeyPath)
	} else {
		logger.Info("config", "tls", "disabled")
	}
	logger.Info("========================================")
}

// isSecretEnvVar checks if an environment variable name contains a secret.
// Returns true for explicitly marked secrets or names matching sensitive patterns.
func isSecretEnvVar(name string) bool {
	// Check explicit list first
	isSecret, exists := secretEnvVars[name]
	if exists {
		return isSecret
	}

	// SECURITY: Enhanced pattern matching for sensitive data
	// Check for common secret patterns in environment variable names
	upperName := strings.ToUpper(name)
	sensitivePatterns := []string{
		"PASSWORD",
		"SECRET",
		"PRIVATE_KEY",
		"API_KEY",
		"APIKEY",
		"TOKEN",
		"AUTH",
		"CREDENTIAL",
		"PAT", // Personal Access Token
		"KEY",
	}

	for _, pattern := range sensitivePatterns {
		if strings.Contains(upperName, pattern) {
			return true
		}
	}

	return false
}

// maskSecret returns a masked version of a secret value.
func maskSecret(value string) string {
	if value == "" {
		return "<not set>"
	}
	return fmt.Sprintf("******** (%d chars)", len(value))
}

// resolveServerEnvVars is a legacy function kept for backward compatibility.
// Servers are now configured via the database with Agent API credentials.
func resolveServerEnvVars(_ *models.ServerConfig) error {
	return nil
}
