package config

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ListenAddr != "0.0.0.0:8405" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "0.0.0.0:8405")
	}
	if cfg.DataDir != "/var/lib/gearbox-agent" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "/var/lib/gearbox-agent")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.GitBranch != "main" {
		t.Errorf("GitBranch = %q, want %q", cfg.GitBranch, "main")
	}
	if cfg.AppsFolder != "apps" {
		t.Errorf("AppsFolder = %q, want %q", cfg.AppsFolder, "apps")
	}
	if cfg.PollInterval != 60 {
		t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, 60)
	}
	if cfg.HAProxyConfigFile != "/etc/haproxy/haproxy.cfg" {
		t.Errorf("HAProxyConfigFile = %q, want %q", cfg.HAProxyConfigFile, "/etc/haproxy/haproxy.cfg")
	}
	if !cfg.SyncEnabled {
		t.Error("SyncEnabled should be true by default")
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Clear all env vars that could affect the test
	envVars := []string{
		"HAPROXY_AGENT_LISTEN",
		"HAPROXY_AGENT_DATA_DIR",
		"HAPROXY_AGENT_TLS_CERT",
		"HAPROXY_AGENT_TLS_KEY",
		"HAPROXY_AGENT_API_KEY_PATH",
		"HAPROXY_AGENT_LOG_LEVEL",
		"HAPROXY_STATS_SOCKET",
		"HAPROXY_STATS_URL",
		"HAPROXY_GIT_REPO",
		"HAPROXY_GIT_PAT",
		"HAPROXY_GIT_BRANCH",
		"HAPROXY_APPS_FOLDER",
		"HAPROXY_POLL_INTERVAL",
		"HAPROXY_CONFIG_FILE",
		"HAPROXY_SYNC_ENABLED",
		"HAPROXY_DRY_RUN",
		"HAPROXY_WEBHOOK_ENABLED",
	}

	// Save and clear env vars
	savedEnv := make(map[string]string)
	for _, key := range envVars {
		savedEnv[key] = os.Getenv(key)
		os.Unsetenv(key)
	}
	defer func() {
		for key, val := range savedEnv {
			if val != "" {
				os.Setenv(key, val)
			}
		}
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check defaults are applied
	if cfg.ListenAddr != "0.0.0.0:8405" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "0.0.0.0:8405")
	}
	if cfg.HAProxyStatsSocket != "/run/haproxy/admin.sock" {
		t.Errorf("HAProxyStatsSocket = %q, want %q", cfg.HAProxyStatsSocket, "/run/haproxy/admin.sock")
	}
}

func TestLoad_EnvironmentVariables(t *testing.T) {
	// Save current env
	savedEnv := map[string]string{
		"HAPROXY_AGENT_LISTEN":    os.Getenv("HAPROXY_AGENT_LISTEN"),
		"HAPROXY_AGENT_DATA_DIR":  os.Getenv("HAPROXY_AGENT_DATA_DIR"),
		"HAPROXY_GIT_REPO":        os.Getenv("HAPROXY_GIT_REPO"),
		"HAPROXY_GIT_BRANCH":      os.Getenv("HAPROXY_GIT_BRANCH"),
		"HAPROXY_POLL_INTERVAL":   os.Getenv("HAPROXY_POLL_INTERVAL"),
		"HAPROXY_WEBHOOK_ENABLED": os.Getenv("HAPROXY_WEBHOOK_ENABLED"),
		"HAPROXY_DRY_RUN":         os.Getenv("HAPROXY_DRY_RUN"),
	}
	defer func() {
		for key, val := range savedEnv {
			if val != "" {
				os.Setenv(key, val)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	// Set test values
	os.Setenv("HAPROXY_AGENT_LISTEN", "127.0.0.1:9000")
	os.Setenv("HAPROXY_AGENT_DATA_DIR", "/tmp/test-agent")
	os.Setenv("HAPROXY_GIT_REPO", "https://github.com/test/repo")
	os.Setenv("HAPROXY_GIT_BRANCH", "develop")
	os.Setenv("HAPROXY_POLL_INTERVAL", "120")
	os.Setenv("HAPROXY_WEBHOOK_ENABLED", "true")
	os.Setenv("HAPROXY_DRY_RUN", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ListenAddr != "127.0.0.1:9000" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "127.0.0.1:9000")
	}
	if cfg.DataDir != "/tmp/test-agent" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "/tmp/test-agent")
	}
	if cfg.GitRepoURL != "https://github.com/test/repo" {
		t.Errorf("GitRepoURL = %q, want %q", cfg.GitRepoURL, "https://github.com/test/repo")
	}
	if cfg.GitBranch != "develop" {
		t.Errorf("GitBranch = %q, want %q", cfg.GitBranch, "develop")
	}
	if cfg.PollInterval != 120 {
		t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, 120)
	}
	if !cfg.WebhookEnabled {
		t.Error("WebhookEnabled should be true")
	}
	if !cfg.DryRun {
		t.Error("DryRun should be true")
	}
}

func TestLoad_PollIntervalDuration(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     int
	}{
		{"plain integer", "300", 300},
		{"seconds suffix", "300s", 300},
		{"minutes suffix", "5m", 300},
		{"hour suffix", "1h", 3600},
		{"invalid format uses default", "invalid", 60},
		{"empty uses default", "", 60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved := os.Getenv("HAPROXY_POLL_INTERVAL")
			defer func() {
				if saved != "" {
					os.Setenv("HAPROXY_POLL_INTERVAL", saved)
				} else {
					os.Unsetenv("HAPROXY_POLL_INTERVAL")
				}
			}()

			if tt.envValue != "" {
				os.Setenv("HAPROXY_POLL_INTERVAL", tt.envValue)
			} else {
				os.Unsetenv("HAPROXY_POLL_INTERVAL")
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.PollInterval != tt.want {
				t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, tt.want)
			}
		})
	}
}

func TestLoad_CustomTLS(t *testing.T) {
	saved := map[string]string{
		"HAPROXY_AGENT_TLS_CERT": os.Getenv("HAPROXY_AGENT_TLS_CERT"),
		"HAPROXY_AGENT_TLS_KEY":  os.Getenv("HAPROXY_AGENT_TLS_KEY"),
	}
	defer func() {
		for key, val := range saved {
			if val != "" {
				os.Setenv(key, val)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	os.Setenv("HAPROXY_AGENT_TLS_CERT", "/etc/letsencrypt/live/example.com/fullchain.pem")
	os.Setenv("HAPROXY_AGENT_TLS_KEY", "/etc/letsencrypt/live/example.com/privkey.pem")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.TLSCustom {
		t.Error("TLSCustom should be true when custom paths are provided")
	}
	if cfg.TLSCert != "/etc/letsencrypt/live/example.com/fullchain.pem" {
		t.Errorf("TLSCert = %q, want custom path", cfg.TLSCert)
	}
	if cfg.TLSKey != "/etc/letsencrypt/live/example.com/privkey.pem" {
		t.Errorf("TLSKey = %q, want custom path", cfg.TLSKey)
	}
}

func TestLoad_TLSHosts(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     []string
	}{
		{"unset", "", nil},
		{"whitespace only", "   ", nil},
		{"single host", "mjolnir", []string{"mjolnir"}},
		{"comma separated", "mjolnir,10.0.0.1,agent.local", []string{"mjolnir", "10.0.0.1", "agent.local"}},
		{"trims surrounding whitespace", " mjolnir , 10.0.0.1 ", []string{"mjolnir", "10.0.0.1"}},
		{"drops empty entries", "mjolnir,,10.0.0.1,", []string{"mjolnir", "10.0.0.1"}},
	}

	saved := os.Getenv("HAPROXY_AGENT_TLS_HOSTS")
	defer func() {
		if saved != "" {
			os.Setenv("HAPROXY_AGENT_TLS_HOSTS", saved)
		} else {
			os.Unsetenv("HAPROXY_AGENT_TLS_HOSTS")
		}
	}()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				os.Unsetenv("HAPROXY_AGENT_TLS_HOSTS")
			} else {
				os.Setenv("HAPROXY_AGENT_TLS_HOSTS", tt.envValue)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if len(cfg.TLSHosts) != len(tt.want) {
				t.Fatalf("TLSHosts = %v (len %d), want %v (len %d)",
					cfg.TLSHosts, len(cfg.TLSHosts), tt.want, len(tt.want))
			}
			for i, v := range tt.want {
				if cfg.TLSHosts[i] != v {
					t.Errorf("TLSHosts[%d] = %q, want %q", i, cfg.TLSHosts[i], v)
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "empty listen address",
			modify:  func(c *Config) { c.ListenAddr = "" },
			wantErr: true,
			errMsg:  "listen address is required",
		},
		{
			name:    "empty data directory",
			modify:  func(c *Config) { c.DataDir = "" },
			wantErr: true,
			errMsg:  "data directory is required",
		},
		{
			name:    "poll interval too low",
			modify:  func(c *Config) { c.PollInterval = 5 },
			wantErr: true,
			errMsg:  "poll interval must be at least 10 seconds",
		},
		{
			name:    "poll interval too high",
			modify:  func(c *Config) { c.PollInterval = 7200 },
			wantErr: true,
			errMsg:  "poll interval must be at most 3600 seconds",
		},
		{
			name:    "custom TLS missing cert",
			modify:  func(c *Config) { c.TLSCustom = true; c.TLSCert = "" },
			wantErr: true,
			errMsg:  "both TLS certificate and key paths are required",
		},
		{
			name:    "custom TLS missing key",
			modify:  func(c *Config) { c.TLSCustom = true; c.TLSKey = "" },
			wantErr: true,
			errMsg:  "both TLS certificate and key paths are required",
		},
		{
			name: "valid custom TLS",
			modify: func(c *Config) {
				c.TLSCustom = true
				c.TLSCert = "/path/to/cert.pem"
				c.TLSKey = "/path/to/key.pem"
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error, got nil")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"debug", "DEBUG"},
		{"DEBUG", "DEBUG"},
		{"info", "INFO"},
		{"INFO", "INFO"},
		{"warn", "WARN"},
		{"WARN", "WARN"},
		{"warning", "WARN"},
		{"error", "ERROR"},
		{"ERROR", "ERROR"},
		{"invalid", "INFO"},
		{"", "INFO"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLogLevel(tt.input)
			if got.String() != tt.want {
				t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.input, got.String(), tt.want)
			}
		})
	}
}

func TestValidLogLevels(t *testing.T) {
	levels := ValidLogLevels()
	expected := []string{"debug", "info", "warn", "error"}

	if len(levels) != len(expected) {
		t.Errorf("ValidLogLevels() returned %d levels, want %d", len(levels), len(expected))
	}

	for i, level := range levels {
		if level != expected[i] {
			t.Errorf("ValidLogLevels()[%d] = %q, want %q", i, level, expected[i])
		}
	}
}

func TestGetEnvDurationSecondsOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		envValue string
		setEnv   bool
		defVal   int
		want     int
	}{
		{"env not set", "TEST_DUR_1", "", false, 100, 100},
		{"empty string", "TEST_DUR_2", "", true, 100, 100},
		{"plain integer", "TEST_DUR_3", "300", true, 100, 300},
		{"seconds suffix", "TEST_DUR_4", "45s", true, 100, 45},
		{"minutes suffix", "TEST_DUR_5", "2m", true, 100, 120},
		{"hours suffix", "TEST_DUR_6", "1h", true, 100, 3600},
		{"invalid duration", "TEST_DUR_7", "notaduration", true, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			got := getEnvDurationSecondsOrDefault(tt.key, tt.defVal)
			if got != tt.want {
				t.Errorf("getEnvDurationSecondsOrDefault() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNormaliseSourceOverride(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"haproxy", "haproxy"},
		{"HAProxy", "haproxy"},
		{" HAPROXY ", "haproxy"},
		{"  Nginx  ", "nginx"},
	}
	for _, tc := range cases {
		if got := normaliseSourceOverride(tc.in); got != tc.want {
			t.Errorf("normaliseSourceOverride(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoad_HTTPSourceOverride(t *testing.T) {
	// t.Setenv handles save/restore automatically and prevents
	// parallel-test interference; the old os.Setenv + t.Cleanup pattern
	// would leak the env var if the test panicked before Cleanup ran.
	t.Setenv("GEARBOX_AGENT_HTTP_SOURCE", " Nginx ")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPSource != "nginx" {
		t.Errorf("HTTPSource = %q, want %q (trimmed + lowercased)", cfg.HTTPSource, "nginx")
	}
}

func TestLoad_HTTPSourceDefaultsEmpty(t *testing.T) {
	// Unset within the test; t.Setenv with an empty string only marks
	// the env var for restoration but doesn't unset it, so explicit
	// Unsetenv is the right tool when the test specifically needs the
	// var absent.
	t.Setenv("GEARBOX_AGENT_HTTP_SOURCE", "") // record original for restore
	os.Unsetenv("GEARBOX_AGENT_HTTP_SOURCE")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPSource != "" {
		t.Errorf("HTTPSource = %q, want empty (auto-detect)", cfg.HTTPSource)
	}
}

// TestLoad_PerSourceOverrides exercises the per-source env vars that
// short-circuit each detector's default probe. The values must round-
// trip with whitespace trimmed but case preserved — URLs are
// case-sensitive in their path components and Linux filesystem paths
// are case-sensitive too, so the HTTPSource trim+lowercase treatment
// would be wrong here.
func TestLoad_PerSourceOverrides(t *testing.T) {
	cases := []struct {
		envKey, envVal string
		field          func(*Config) string
		want           string
	}{
		{"NGINX_STATUS_URL", " http://127.0.0.1/Nginx_Status ", func(c *Config) string { return c.NginxStatusURL }, "http://127.0.0.1/Nginx_Status"},
		{"NGINX_CONFIG_FILE", "/etc/nginx/Nginx.conf", func(c *Config) string { return c.NginxConfigFile }, "/etc/nginx/Nginx.conf"},
		{"APACHE_STATUS_URL", "http://127.0.0.1/server-status?auto", func(c *Config) string { return c.ApacheStatusURL }, "http://127.0.0.1/server-status?auto"},
		{"APACHE_CONFIG_FILE", "/etc/httpd/conf/httpd.conf", func(c *Config) string { return c.ApacheConfigFile }, "/etc/httpd/conf/httpd.conf"},
		{"CADDY_ADMIN_URL", "http://127.0.0.1:2019/metrics", func(c *Config) string { return c.CaddyAdminURL }, "http://127.0.0.1:2019/metrics"},
		{"TRAEFIK_METRICS_URL", "http://127.0.0.1:8082/metrics", func(c *Config) string { return c.TraefikMetricsURL }, "http://127.0.0.1:8082/metrics"},
		{"DOCKER_SOCKET", "/var/run/docker.sock", func(c *Config) string { return c.DockerSocket }, "/var/run/docker.sock"},
	}
	for _, tc := range cases {
		t.Run(tc.envKey, func(t *testing.T) {
			t.Setenv(tc.envKey, tc.envVal)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := tc.field(cfg); got != tc.want {
				t.Errorf("%s round-trip = %q, want %q", tc.envKey, got, tc.want)
			}
		})
	}
}

func TestLoad_PerSourceOverridesDefaultEmpty(t *testing.T) {
	// Defaults must be empty strings — empty signals to each gear's
	// Probe() that no override is in effect and it should walk its
	// well-known-paths default.
	for _, k := range []string{
		"NGINX_STATUS_URL", "NGINX_CONFIG_FILE",
		"APACHE_STATUS_URL", "APACHE_CONFIG_FILE",
		"CADDY_ADMIN_URL", "TRAEFIK_METRICS_URL", "DOCKER_SOCKET",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	getters := map[string]string{
		"NginxStatusURL":    cfg.NginxStatusURL,
		"NginxConfigFile":   cfg.NginxConfigFile,
		"ApacheStatusURL":   cfg.ApacheStatusURL,
		"ApacheConfigFile":  cfg.ApacheConfigFile,
		"CaddyAdminURL":     cfg.CaddyAdminURL,
		"TraefikMetricsURL": cfg.TraefikMetricsURL,
		"DockerSocket":      cfg.DockerSocket,
	}
	for name, got := range getters {
		if got != "" {
			t.Errorf("%s default = %q, want empty", name, got)
		}
	}
}
