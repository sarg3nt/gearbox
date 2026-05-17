package gear

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// Dependencies contains services provided by the framework to plugins.
// All plugins receive the same Dependencies instance during initialization.
type Dependencies struct {
	// Logger is the structured logger for the gear.
	// Each plugin receives a logger with its name already set.
	Logger *slog.Logger

	// EventBus allows plugins to publish events for WebSocket streaming.
	// Gears can use this to publish events.Event structs.
	EventBus *events.Bus

	// HTTPClient is a pre-configured HTTP client for external requests.
	HTTPClient *http.Client

	// Config contains the plugin's configuration from environment variables.
	Config map[string]any

	// StatsInterval is the configured interval for stats collection.
	StatsInterval time.Duration

	// HAProxyStatsSocket is the path to the HAProxy stats socket.
	HAProxyStatsSocket string

	// HAProxyStatsURL is the URL for HAProxy stats HTTP endpoint.
	HAProxyStatsURL string

	// HAProxyStatsUser is the username for HAProxy stats authentication.
	HAProxyStatsUser string

	// HAProxyStatsPassword is the password for HAProxy stats authentication.
	HAProxyStatsPassword string

	// HAProxyConfigPath is the path to the HAProxy configuration file.
	HAProxyConfigPath string

	// CertbotTimer is the name of the certbot systemd timer.
	CertbotTimer string

	// SourceOverrides maps metric categories to an operator-chosen
	// primary gear name, bypassing auto-detection for that category.
	// Empty map (or missing key) means auto-detect. Sourced from
	// per-category env vars at startup (GEARBOX_AGENT_HTTP_SOURCE for
	// CategoryHTTPRequests, etc.). Names are pre-lowercased to match
	// Info().Name lookups in the manager.
	SourceOverrides map[MetricCategory]string

	// Per-source detection overrides. Each one bypasses the default
	// well-known-paths / loopback-URL probe for one gear and trusts the
	// operator's value directly. Empty means "auto-detect" — the
	// corresponding gear walks its defaults. Populated from per-source
	// env vars at startup. See [docs/source-detection.md] / issue #95.
	NginxStatusURL    string // NGINX_STATUS_URL
	NginxConfigFile   string // NGINX_CONFIG_FILE
	ApacheStatusURL   string // APACHE_STATUS_URL
	ApacheConfigFile  string // APACHE_CONFIG_FILE
	CaddyAdminURL     string // CADDY_ADMIN_URL
	TraefikMetricsURL string // TRAEFIK_METRICS_URL
	DockerSocket      string // DOCKER_SOCKET

	// Per-source access-log paths. Empty means "fall back to the
	// gear's well-known default"; an explicit value bypasses the
	// fallback (and a non-existent path then surfaces as
	// "log file not readable" through the access-log endpoint).
	HAProxyAccessLog string // HAPROXY_ACCESS_LOG
	NginxAccessLog   string // NGINX_ACCESS_LOG
	ApacheAccessLog  string // APACHE_ACCESS_LOG
	CaddyAccessLog   string // CADDY_ACCESS_LOG
}

// Common event types used across plugins.
const (
	// EventStatsUpdated is published when HAProxy stats are collected.
	EventStatsUpdated = "stats.updated"

	// EventMetricsUpdated is published when system metrics are collected.
	EventMetricsUpdated = "metrics.updated"

	// EventLogsUpdated is published when new log entries are received.
	EventLogsUpdated = "logs.updated"

	// EventCertificatesUpdated is published when certificate status changes.
	EventCertificatesUpdated = "certificates.updated"

	// EventSecurityUpdated is published when security status changes.
	EventSecurityUpdated = "security.updated"

	// EventUpdatesAvailable is published when system updates are available.
	EventUpdatesAvailable = "updates.available"

	// EventConfigChanged is published when HAProxy config changes.
	EventConfigChanged = "config.changed"

	// EventSyncUpdated is published when Git sync completes.
	EventSyncUpdated = "sync.updated"
)
