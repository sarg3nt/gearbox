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

	// SwaggerEnabled, when true, serves the Swagger UI + OpenAPI spec at
	// /swagger and /swagger/*. Off by default to avoid exposing the agent's
	// endpoint + schema list to unauthenticated callers in production. See
	// 2026-05 security audit P3-2.
	SwaggerEnabled bool

	// Metric-source overrides. Each one points a single metric category
	// at a specific gear, bypassing auto-detection. Empty = pure
	// auto-detection (built-in preference order). Lowercased at load
	// time; the manager validates that each named gear is actually
	// available before honouring the override (falls back to auto and
	// warns otherwise).
	//
	// Background: most hosts have one obvious producer per metric
	// category. When two coexist (e.g. HAProxy + nginx both serving
	// HTTP), the agent picks one as primary; this override lets the
	// operator force the choice for boxes where the built-in
	// preference picks wrong. See [docs/source-detection.md] /
	// issue #95.
	HTTPSource string // GEARBOX_AGENT_HTTP_SOURCE — primary for CategoryHTTPRequests

	// Per-source detection overrides. Each one short-circuits the
	// detector's default well-known-paths/loopback-URL probe and trusts
	// the operator-supplied surface instead. The agent does not probe
	// these synchronously at startup — a misconfigured value will
	// surface later when the metrics phase tries to read from it.
	//
	// Empty (the default) means "auto-detect" — the corresponding
	// gear's Probe() walks its well-known paths and default loopback
	// URL. Operators on hosts where the binary lives in a non-standard
	// place, or whose status endpoint lives on a non-default address,
	// reach for these as the escape hatch. See [docs/source-detection.md]
	// / issue #95.
	NginxStatusURL    string // NGINX_STATUS_URL    — force a specific stub_status URL
	NginxConfigFile   string // NGINX_CONFIG_FILE   — force a specific nginx.conf path
	ApacheStatusURL   string // APACHE_STATUS_URL   — force a specific mod_status URL
	ApacheConfigFile  string // APACHE_CONFIG_FILE  — force a specific httpd.conf path
	CaddyAdminURL     string // CADDY_ADMIN_URL     — force the admin/Prometheus URL
	TraefikMetricsURL string // TRAEFIK_METRICS_URL — force the Prometheus endpoint URL
	DockerSocket      string // DOCKER_SOCKET       — force a specific docker socket path

	// Access-log paths per source. The /api/v1/access-log/{source}/recent
	// endpoint reads the most recent N lines from these files and parses
	// each with the matching profile. Empty (the default) means the
	// endpoint falls back to a well-known path; if that doesn't exist
	// the endpoint reports "no readable log file" rather than failing
	// the agent. See [docs/source-detection.md] / issue #91 Phase 5.
	HAProxyAccessLog string // HAPROXY_ACCESS_LOG
	NginxAccessLog   string // NGINX_ACCESS_LOG
	ApacheAccessLog  string // APACHE_ACCESS_LOG
	CaddyAccessLog   string // CADDY_ACCESS_LOG
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

	// Swagger UI off by default; opt in for dev / API debugging.
	cfg.SwaggerEnabled = os.Getenv("HAPROXY_AGENT_SWAGGER_ENABLED") == "true"

	// Metric-source overrides. Lowercased + trimmed so 'HAProxy ' and
	// 'haproxy' both match the gear's Info().Name. Empty = auto-detect.
	cfg.HTTPSource = normaliseSourceOverride(os.Getenv("GEARBOX_AGENT_HTTP_SOURCE"))

	// Per-source detection overrides. Unprefixed env vars to match the
	// existing HAPROXY_STATS_URL style — these belong to the subject,
	// not the agent. Trimmed but not lowercased (URLs and paths are
	// case-sensitive on most filesystems).
	cfg.NginxStatusURL = strings.TrimSpace(os.Getenv("NGINX_STATUS_URL"))
	cfg.NginxConfigFile = strings.TrimSpace(os.Getenv("NGINX_CONFIG_FILE"))
	cfg.ApacheStatusURL = strings.TrimSpace(os.Getenv("APACHE_STATUS_URL"))
	cfg.ApacheConfigFile = strings.TrimSpace(os.Getenv("APACHE_CONFIG_FILE"))
	cfg.CaddyAdminURL = strings.TrimSpace(os.Getenv("CADDY_ADMIN_URL"))
	cfg.TraefikMetricsURL = strings.TrimSpace(os.Getenv("TRAEFIK_METRICS_URL"))
	cfg.DockerSocket = strings.TrimSpace(os.Getenv("DOCKER_SOCKET"))

	// Access-log path overrides — trimmed but case-preserved (paths
	// are case-sensitive on most filesystems).
	cfg.HAProxyAccessLog = strings.TrimSpace(os.Getenv("HAPROXY_ACCESS_LOG"))
	cfg.NginxAccessLog = strings.TrimSpace(os.Getenv("NGINX_ACCESS_LOG"))
	cfg.ApacheAccessLog = strings.TrimSpace(os.Getenv("APACHE_ACCESS_LOG"))
	cfg.CaddyAccessLog = strings.TrimSpace(os.Getenv("CADDY_ACCESS_LOG"))

	return cfg, nil
}

// normaliseSourceOverride trims + lowercases an operator-supplied gear
// name so case / whitespace differences ('HAProxy ' vs 'haproxy') don't
// cause silent misses against Info().Name. Returns "" for an empty or
// whitespace-only input, which downstream callers treat as "no
// override, auto-detect".
func normaliseSourceOverride(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
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
