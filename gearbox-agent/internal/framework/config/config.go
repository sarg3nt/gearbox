// Package config handles configuration loading for the gearbox-agent.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the gearbox-agent.
type Config struct {
	// API server settings
	ListenAddr string
	TLSCert    string
	TLSKey     string
	TLSCustom  bool // True if user provided custom TLS cert paths

	// API key settings
	APIKeyPath string

	// Data directory for state, certs, etc.
	DataDir string

	// Logging
	LogLevel string

	// HAProxy stats proxy settings (optional)
	HAProxyStatsSocket   string // Unix socket path (preferred)
	HAProxyStatsURL      string // HTTP stats URL (fallback)
	HAProxyStatsUser     string
	HAProxyStatsPassword string

	// GitHub sync settings
	GitRepoURL   string
	GitPAT       string
	GitBranch    string
	AppsFolder   string
	PollInterval int // seconds

	// HAProxy settings
	HAProxyConfigFile string
	MetadataFile      string
	StateFile         string
	RepoCacheDir      string

	// Sync mode
	SyncEnabled bool
	DryRun      bool

	// Webhook settings
	WebhookEnabled    bool
	WebhookSecretPath string
	WebhookPollBackup bool // If true, keep polling as backup when webhooks enabled

	// Stats collection interval (for WebSocket push)
	StatsInterval int // seconds (default 5)

	// Certificate renewal detection
	CertbotTimer string // Custom certbot timer name (default: auto-detect)
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		ListenAddr:        "0.0.0.0:8405",
		DataDir:           "/var/lib/gearbox-agent",
		LogLevel:          "info",
		GitBranch:         "main",
		AppsFolder:        "apps",
		PollInterval:      60,
		HAProxyConfigFile: "/etc/haproxy/haproxy.cfg",
		SyncEnabled:       true,
		StatsInterval:     2, // 2 seconds default for near real-time updates (configurable 1-60s)
	}
}

// Load loads configuration from environment variables with defaults.
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Server settings
	if v := os.Getenv("HAPROXY_AGENT_LISTEN"); v != "" {
		cfg.ListenAddr = v
	}

	if v := os.Getenv("HAPROXY_AGENT_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}

	// TLS paths - check if user explicitly provided custom paths
	if certPath := os.Getenv("HAPROXY_AGENT_TLS_CERT"); certPath != "" {
		cfg.TLSCert = certPath
		cfg.TLSCustom = true
	} else {
		cfg.TLSCert = cfg.DataDir + "/tls/server.crt"
	}
	if keyPath := os.Getenv("HAPROXY_AGENT_TLS_KEY"); keyPath != "" {
		cfg.TLSKey = keyPath
		cfg.TLSCustom = true
	} else {
		cfg.TLSKey = cfg.DataDir + "/tls/server.key"
	}
	cfg.APIKeyPath = getEnvOrDefault("HAPROXY_AGENT_API_KEY_PATH", cfg.DataDir+"/api-key")

	// Logging
	if v := os.Getenv("HAPROXY_AGENT_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	// HAProxy stats proxy (optional)
	cfg.HAProxyStatsSocket = getEnvOrDefault("HAPROXY_STATS_SOCKET", "/run/haproxy/admin.sock")
	cfg.HAProxyStatsURL = os.Getenv("HAPROXY_STATS_URL")
	cfg.HAProxyStatsUser = os.Getenv("HAPROXY_STATS_USER")
	cfg.HAProxyStatsPassword = os.Getenv("HAPROXY_STATS_PASSWORD")

	// GitHub sync settings
	cfg.GitRepoURL = os.Getenv("HAPROXY_GIT_REPO")
	cfg.GitPAT = os.Getenv("HAPROXY_GIT_PAT")
	if v := os.Getenv("HAPROXY_GIT_BRANCH"); v != "" {
		cfg.GitBranch = v
	}
	if v := os.Getenv("HAPROXY_APPS_FOLDER"); v != "" {
		cfg.AppsFolder = v
	}
	cfg.PollInterval = getEnvDurationSecondsOrDefault("HAPROXY_POLL_INTERVAL", cfg.PollInterval)

	// HAProxy settings
	if v := os.Getenv("HAPROXY_CONFIG_FILE"); v != "" {
		cfg.HAProxyConfigFile = v
	}

	// Paths default to data directory
	cfg.MetadataFile = getEnvOrDefault("HAPROXY_METADATA_FILE", cfg.DataDir+"/metadata.json")
	cfg.StateFile = getEnvOrDefault("HAPROXY_STATE_FILE", cfg.DataDir+"/state.json")
	cfg.RepoCacheDir = getEnvOrDefault("HAPROXY_REPO_CACHE", cfg.DataDir+"/repo")

	// Sync mode
	cfg.SyncEnabled = os.Getenv("HAPROXY_SYNC_ENABLED") != "false"
	cfg.DryRun = os.Getenv("HAPROXY_DRY_RUN") == "true"

	// Webhook settings (disabled by default, enable with HAPROXY_WEBHOOK_ENABLED=true)
	cfg.WebhookEnabled = os.Getenv("HAPROXY_WEBHOOK_ENABLED") == "true"
	cfg.WebhookSecretPath = getEnvOrDefault("HAPROXY_WEBHOOK_SECRET_PATH", cfg.DataDir+"/webhook-secret")
	cfg.WebhookPollBackup = os.Getenv("HAPROXY_WEBHOOK_POLL_BACKUP") == "true"

	// Stats collection interval (1-60 seconds, default 5)
	cfg.StatsInterval = getEnvDurationSecondsOrDefault("HAPROXY_STATS_INTERVAL", cfg.StatsInterval)

	// Certificate renewal detection (optional override)
	cfg.CertbotTimer = os.Getenv("HAPROXY_CERTBOT_TIMER")

	return cfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("listen address is required")
	}

	if c.DataDir == "" {
		return fmt.Errorf("data directory is required")
	}

	if c.PollInterval < 10 {
		return fmt.Errorf("poll interval must be at least 10 seconds")
	}

	if c.PollInterval > 3600 {
		return fmt.Errorf("poll interval must be at most 3600 seconds (1 hour)")
	}

	if c.StatsInterval < 1 {
		return fmt.Errorf("stats interval must be at least 1 second")
	}

	if c.StatsInterval > 60 {
		return fmt.Errorf("stats interval must be at most 60 seconds")
	}

	// Validate TLS settings if custom certs are specified
	if c.TLSCustom {
		if c.TLSCert == "" || c.TLSKey == "" {
			return fmt.Errorf("both TLS certificate and key paths are required when using custom TLS")
		}
	}

	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// getEnvDurationSecondsOrDefault parses env var as duration (e.g., "5m", "300s", "60")
// and returns the value in seconds. Plain integers are treated as seconds for backward compatibility.
func getEnvDurationSecondsOrDefault(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}

	// Try parsing as plain integer first (backward compatibility - seconds)
	if i, err := strconv.Atoi(v); err == nil {
		return i
	}

	// Try parsing as Go duration string (e.g., "5m", "300s", "1h")
	if d, err := time.ParseDuration(v); err == nil {
		return int(d.Seconds())
	}

	// Invalid format, return default
	return defaultValue
}

// ParseLogLevel converts a string log level to slog.Level.
// Valid values: debug, info, warn, error (case-insensitive).
// Returns slog.LevelInfo if the level is invalid.
func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ValidLogLevels returns the list of valid log level strings.
func ValidLogLevels() []string {
	return []string{"debug", "info", "warn", "error"}
}
