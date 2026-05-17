// Package main is the entry point for gearbox-agent.
//
// Gearbox Agent API
//
//	@title						Gearbox Agent API
//	@version					1.0
//	@description				REST API for monitoring and managing HAProxy instances. Provides endpoints for system metrics, HAProxy stats, log streaming, security monitoring, and real-time WebSocket events.
//	@termsOfService				https://github.com/sarg3nt/gearbox-agent
//	@contact.name				Gearbox Agent Support
//	@contact.url				https://github.com/sarg3nt/gearbox-agent/issues
//	@license.name				MIT
//	@license.url				https://opensource.org/licenses/MIT
//	@host						localhost:8405
//	@BasePath					/
//	@schemes					https
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				API key authentication. Use format: Bearer YOUR_API_KEY
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/crypto/ssh"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/api"
	"github.com/sarg3nt/gearbox-agent/internal/framework/config"
	"github.com/sarg3nt/gearbox-agent/internal/framework/crypto"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/middleware"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/sync"

	// Import plugins - blank identifier triggers init() registration
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/accesslog"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/apache"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/caddy"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/certs"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/docker"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/haproxy"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/host"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/logs"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/metrics"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/nginx"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/security"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/traefik"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/traffic"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/updates"
)

var (
	// Version is set via ldflags during build
	Version = "dev"
	// CommitSHA is set via ldflags during build
	CommitSHA = "unknown"
	// BuildDate is set via ldflags during build
	BuildDate = "unknown"
)

func main() {
	// Parse command line flags
	showAPIKey := flag.Bool("show-api-key", false, "Display the current API key and exit")
	rotateAPIKey := flag.Bool("rotate-api-key", false, "Generate a new API key and exit")
	showWebhookSecret := flag.Bool("show-webhook-secret", false, "Display the current webhook secret and exit")
	generateWebhookSecret := flag.Bool("generate-webhook-secret", false, "Generate webhook secret (if not exists) and display it")
	showVersion := flag.Bool("version", false, "Show version and exit")
	syncOnce := flag.Bool("sync-once", false, "Run one sync cycle and exit")
	generateConsoleKey := flag.Bool("generate-console-key", false, "Generate an ed25519 SSH key pair under HAPROXY_AGENT_DATA_DIR for the console ssh_bridge mode and print the public key for authorized_keys")
	flag.Parse()

	if *showVersion {
		fmt.Printf("gearbox-agent %s (commit: %s, built: %s)\n", Version, CommitSHA, BuildDate)
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid config: %v\n", err)
		os.Exit(1)
	}

	// Setup structured logger with configured level
	logLevel := config.ParseLogLevel(cfg.LogLevel)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Hard-fail early if GEARBOX_AGENT_ENCRYPTION_KEY is set but malformed,
	// regardless of which subcommand we're about to run — one-shot CLI
	// commands like --show-api-key need a usable key too.
	if _, err := crypto.EncryptionConfigured(); err != nil {
		fmt.Fprintf(os.Stderr, "Encryption key configuration error: %v\n", err)
		os.Exit(1)
	}

	// Handle API key commands.
	//
	// Both flags read/write the agent's keyring (issue #72), falling back
	// to the legacy api-key file when no keyring is present yet. The
	// printed key is the keyring's primary entry, in the
	// `gbx_<kid>_<base64url>` wire format the dashboard accepts.
	if *showAPIKey {
		kr, _, err := crypto.LoadOrCreateKeyRing(cfg.KeyRingPath, cfg.APIKeyPath)
		if err != nil {
			logger.Error("Failed to read keyring", "error", err)
			os.Exit(1)
		}
		primary := kr.Primary()
		if primary == nil {
			fmt.Fprintln(os.Stderr, "no primary key found in keyring")
			os.Exit(1)
		}
		fmt.Println(crypto.FormatToken(primary.KID, primary.Secret))
		os.Exit(0)
	}

	if *rotateAPIKey {
		// Generates a fresh primary key and writes it to the keyring,
		// replacing whatever was primary before. The agent reads the
		// keyring on startup so the new key takes effect after restart.
		// (Phase 2 will add a hot-reload endpoint that avoids restart.)
		kr, _, err := crypto.LoadOrCreateKeyRing(cfg.KeyRingPath, cfg.APIKeyPath)
		if err != nil {
			logger.Error("Failed to load keyring", "error", err)
			os.Exit(1)
		}
		// Replace all entries with a single fresh primary. CLI rotate is
		// the operator escape hatch — it's intentionally not the same as
		// the controller-driven three-phase rotation; rather, it wipes
		// the slate so a fresh dashboard pairing can take over.
		kid, kerr := crypto.NewKID()
		if kerr != nil {
			logger.Error("Failed to generate key id", "error", kerr)
			os.Exit(1)
		}
		secret, serr := crypto.NewSecret()
		if serr != nil {
			logger.Error("Failed to generate secret", "error", serr)
			os.Exit(1)
		}
		fresh := crypto.KeyRingEntry{
			KID:       kid,
			Secret:    secret,
			Role:      "primary",
			CreatedAt: time.Now().UTC(),
		}
		kr.Entries = []crypto.KeyRingEntry{fresh}
		if err := crypto.SaveKeyRing(cfg.KeyRingPath, kr); err != nil {
			logger.Error("Failed to write keyring", "error", err)
			os.Exit(1)
		}
		fmt.Println("New API key generated:")
		fmt.Println(crypto.FormatToken(fresh.KID, fresh.Secret))
		fmt.Println("\nRestart the service for the new key to take effect.")
		os.Exit(0)
	}

	if *showWebhookSecret {
		secret, err := crypto.ReadWebhookSecret(cfg.WebhookSecretPath)
		if err != nil {
			if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
				fmt.Println("Webhook secret not found.")
				fmt.Println("")
				fmt.Println("The webhook secret is only generated when webhooks are enabled.")
				fmt.Println("To generate it now, run:")
				fmt.Println("  gearbox-agent --generate-webhook-secret")
				fmt.Println("")
				fmt.Println("Or enable webhooks and restart the service:")
				fmt.Println("  1. Add HAPROXY_WEBHOOK_ENABLED=true to /etc/default/gearbox-agent")
				fmt.Println("  2. Restart the service: sudo systemctl restart gearbox-agent")
				os.Exit(1)
			}
			logger.Error("Failed to read webhook secret", "error", err)
			os.Exit(1)
		}
		fmt.Println(secret)
		os.Exit(0)
	}

	if *generateWebhookSecret {
		secret, isNew, err := crypto.LoadOrCreateWebhookSecret(cfg.WebhookSecretPath)
		if err != nil {
			logger.Error("Failed to generate webhook secret", "error", err)
			os.Exit(1)
		}
		if isNew {
			fmt.Println("Webhook secret generated and saved to:")
			fmt.Printf("  %s\n", cfg.WebhookSecretPath)
		} else {
			fmt.Println("Webhook secret already exists at:")
			fmt.Printf("  %s\n", cfg.WebhookSecretPath)
		}
		fmt.Println("")
		fmt.Println("Secret:")
		fmt.Println(secret)
		fmt.Println("")
		fmt.Println("Use this secret when configuring the webhook in GitHub.")
		os.Exit(0)
	}

	if *generateConsoleKey {
		// Generate an ed25519 keypair under DataDir/console-ssh/agent.
		// Print the public key so the operator can paste it into the
		// host's authorized_keys. Refuses to overwrite an existing
		// key — rotation is a deliberate "delete the old one then
		// re-run" step, not an implicit clobber.
		keyDir := cfg.DataDir + "/console-ssh"
		privPath := keyDir + "/agent"
		pubPath := privPath + ".pub"
		if _, err := os.Stat(privPath); err == nil {
			fmt.Fprintf(os.Stderr, "Console SSH key already exists at %s — delete it first if you want to rotate.\n", privPath)
			os.Exit(1)
		}
		if err := os.MkdirAll(keyDir, 0o700); err != nil {
			logger.Error("Failed to create console-ssh dir", "error", err)
			os.Exit(1)
		}
		pub, priv, err := generateConsoleEd25519()
		if err != nil {
			logger.Error("Failed to generate console SSH key", "error", err)
			os.Exit(1)
		}
		if err := os.WriteFile(privPath, priv, 0o600); err != nil {
			logger.Error("Failed to write console SSH private key", "error", err)
			os.Exit(1)
		}
		if err := os.WriteFile(pubPath, pub, 0o644); err != nil {
			logger.Error("Failed to write console SSH public key", "error", err)
			os.Exit(1)
		}
		fmt.Printf("Console SSH key pair generated:\n  private: %s (mode 0600)\n  public:  %s\n\n", privPath, pubPath)
		fmt.Println("Install the public key on the host's authorized_keys (typically /root/.ssh/authorized_keys),")
		fmt.Println("then set the following env vars on the agent:")
		fmt.Println("")
		fmt.Println("  HAPROXY_AGENT_HOST_EXEC=ssh-bridge")
		fmt.Println("  HAPROXY_AGENT_CONSOLE_SSH_HOST=127.0.0.1:22")
		fmt.Println("  HAPROXY_AGENT_CONSOLE_SSH_USER=root  # or whatever user owns the authorized_keys")
		fmt.Printf("  HAPROXY_AGENT_CONSOLE_SSH_KEY=%s\n", privPath)
		fmt.Println("  HAPROXY_AGENT_CONSOLE_SSH_HOSTKEY=/path/to/expected/host.pub  # ssh-keyscan -t ed25519 <host>")
		fmt.Println("")
		fmt.Println("Public key (paste into authorized_keys):")
		fmt.Println(string(pub))
		os.Exit(0)
	}

	// Normal startup
	logger.Info("Starting gearbox-agent",
		"version", Version,
		"commit", CommitSHA,
		"built", BuildDate,
	)

	// Warn once at startup if secret-file encryption is not configured.
	// Placed after one-shot flag handlers so it doesn't pollute their
	// stdout output (e.g. --show-api-key piped to a clipboard tool).
	if ok, _ := crypto.EncryptionConfigured(); !ok {
		logger.Warn("Secret files are stored in plaintext. " +
			"Set GEARBOX_AGENT_ENCRYPTION_KEY (64 hex chars, see 'openssl rand -hex 32') " +
			"to enable AES-256-GCM encryption-at-rest.")
	}

	// Load or create the keyring. Issue #72 replaced the single-key model
	// with an N-entry keyring to support zero-downtime rotation. Existing
	// installs that still have an api-key file (no keyring.json yet) are
	// migrated transparently: the on-disk hex key becomes the keyring's
	// primary entry with kid="legacy", and the file is left in place as
	// a read-only fallback for one release cycle.
	keyring, isNewKey, err := crypto.LoadOrCreateKeyRing(cfg.KeyRingPath, cfg.APIKeyPath)
	if err != nil {
		logger.Error("Failed to initialize keyring", "error", err)
		os.Exit(1)
	}
	keyringPtr := crypto.NewKeyRingPointer(keyring)
	if isNewKey {
		primary := keyring.Primary()
		if primary != nil {
			logger.Warn("NEW API KEY GENERATED - Save this key, it will not be shown again!")
			// Print to stdout (not logger) so it doesn't appear in system logs
			fmt.Printf("API Key: %s\n", crypto.FormatToken(primary.KID, primary.Secret))
			logger.Info("Keyring saved", "path", cfg.KeyRingPath, "kid", primary.KID)
		}
	} else {
		primary := keyring.Primary()
		kid := "<none>"
		if primary != nil {
			kid = primary.KID
		}
		logger.Info("Keyring loaded", "path", cfg.KeyRingPath, "entries", len(keyring.Entries), "primary_kid", kid)
	}

	// Load or create TLS certificates
	var tlsCfg *crypto.TLSConfig
	if cfg.TLSCustom {
		// User provided custom cert paths (e.g., Let's Encrypt) - load only, don't generate
		tlsCfg, err = crypto.LoadTLSCert(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			logger.Error("Failed to load custom TLS certificate", "error", err)
			os.Exit(1)
		}
		logger.Info("TLS: Using custom certificate", "path", tlsCfg.CertPath)
	} else {
		// Use self-signed certs (generate if needed). The generator
		// always covers loopback (localhost / 127.0.0.1 / ::1); anything
		// else clients will dial — a static container IP, an FQDN, a
		// LAN hostname — must come from HAPROXY_AGENT_TLS_HOSTS.
		//
		// We deliberately do NOT add os.Hostname() here: in a container
		// it's a random short ID that changes on every recreation,
		// which would force a cert regen each restart for no value.
		// Operators who want a specific hostname pin it explicitly.
		var isNewCert bool
		tlsCfg, isNewCert, err = crypto.LoadOrCreateTLSCert(cfg.TLSCert, cfg.TLSKey, cfg.TLSHosts)
		if err != nil {
			logger.Error("Failed to initialize TLS", "error", err)
			os.Exit(1)
		}
		if isNewCert {
			logger.Info("TLS: Generated new self-signed certificate (valid for 1 year)",
				"extra_sans", cfg.TLSHosts)
		} else {
			logger.Info("TLS: Using existing self-signed certificate", "path", tlsCfg.CertPath)
		}
	}

	// Initialize sync service if configured
	var syncService *sync.Service
	if cfg.SyncEnabled && cfg.GitRepoURL != "" {
		logger.Info("Sync enabled",
			"repo", cfg.GitRepoURL,
			"branch", cfg.GitBranch,
			"poll_interval_sec", cfg.PollInterval,
		)

		syncService, err = sync.NewService(sync.Config{
			GitRepoURL:        cfg.GitRepoURL,
			GitPAT:            cfg.GitPAT,
			GitBranch:         cfg.GitBranch,
			AppsFolder:        cfg.AppsFolder,
			RepoCacheDir:      cfg.RepoCacheDir,
			HAProxyConfigFile: cfg.HAProxyConfigFile,
			MetadataFile:      cfg.MetadataFile,
			StateFile:         cfg.StateFile,
			DryRun:            cfg.DryRun,
			Logger:            logger,
		})
		if err != nil {
			logger.Error("Failed to create sync service", "error", err)
			os.Exit(1)
		}
		logger.Info("Sync service initialized")
	} else if cfg.SyncEnabled {
		logger.Info("Sync disabled: HAPROXY_GIT_REPO not configured")
	} else {
		logger.Info("Sync disabled by configuration")
	}

	// Handle sync-once flag
	if *syncOnce {
		if syncService == nil {
			logger.Error("Cannot run sync: sync service not configured (set HAPROXY_GIT_REPO)")
			os.Exit(1)
		}
		logger.Info("Running single sync cycle...")
		result := syncService.RunOnce(true)
		if result.Error != nil {
			logger.Error("Sync failed", "error", result.Error)
			os.Exit(1)
		}
		logger.Info("Sync complete",
			"backends", result.BackendCount,
			"config_changed", result.ConfigChanged,
		)
		os.Exit(0)
	}

	// Load webhook secret if webhooks are enabled
	var webhookSecret string
	if cfg.WebhookEnabled && syncService != nil {
		var isNewSecret bool
		webhookSecret, isNewSecret, err = crypto.LoadOrCreateWebhookSecret(cfg.WebhookSecretPath)
		if err != nil {
			logger.Error("Failed to initialize webhook secret", "error", err)
			os.Exit(1)
		}
		if isNewSecret {
			logger.Warn("NEW WEBHOOK SECRET GENERATED - Configure this in GitHub repository webhook settings!")
			// Print to stdout (not logger) so it doesn't appear in system logs
			fmt.Printf("Webhook Secret: %s\n", webhookSecret)
			logger.Info("Webhook secret saved", "path", cfg.WebhookSecretPath)
		} else {
			logger.Info("Webhook: Secret loaded from file")
		}
		logger.Info("Webhook: Enabled - listening for GitHub push events at POST /api/v1/webhook/github")
	} else if cfg.WebhookEnabled && syncService == nil {
		logger.Info("Webhook: Disabled (sync service not configured)")
	} else {
		logger.Info("Webhook: Disabled (set HAPROXY_WEBHOOK_ENABLED=true to enable)")
	}

	// Build webhook URL for display
	webhookURL := ""
	if cfg.WebhookEnabled {
		// Extract port from listen address (handles :8080 or 0.0.0.0:8080 formats)
		port := cfg.ListenAddr
		if idx := strings.LastIndex(cfg.ListenAddr, ":"); idx >= 0 {
			port = cfg.ListenAddr[idx+1:]
		}
		webhookURL = fmt.Sprintf("https://<your-server>:%s/api/v1/webhook/github", port)
	}

	// Create event bus for real-time updates
	eventBus := events.NewBus()
	defer eventBus.Close()

	// Wire event bus to sync service if configured
	if syncService != nil {
		syncService.SetEventBus(eventBus)
	}
	logger.Info("WebSocket: Enabled - real-time events at GET /api/v1/events")

	// [#89] Console endpoints are always mounted; access is gated by
	// the API key on the token endpoint and by the dashboard's
	// per-box console_enabled toggle for the WS path. One startup
	// line so operators see the surface exists in `journalctl`.
	logger.Info("Console: endpoints mounted at /api/v1/console/* (per-box opt-in is dashboard-side; sessions inherit agent UID)")

	// Create and start API server
	serverCfg := api.ServerConfig{
		ListenAddr:     cfg.ListenAddr,
		KeyRing:        keyringPtr,
		CertFile:       tlsCfg.CertPath,
		KeyFile:        tlsCfg.KeyPath,
		Version:        Version,
		Logger:         logger,
		SwaggerEnabled: cfg.SwaggerEnabled, // P3-2: off by default; opt in via GEARBOX_AGENT_SWAGGER_ENABLED=true
	}
	// Only set MetadataProvider if sync service is configured
	// (Go interfaces holding nil pointers are not themselves nil)
	if syncService != nil {
		serverCfg.MetadataProvider = syncService
		serverCfg.SyncTrigger = syncService
	}
	// Configure webhook if enabled
	if cfg.WebhookEnabled && webhookSecret != "" {
		serverCfg.WebhookEnabled = true
		serverCfg.WebhookSecret = webhookSecret
		serverCfg.WebhookURL = webhookURL
	}
	// Configure event bus for WebSocket support
	serverCfg.EventBus = eventBus

	// Configure HAProxy stats proxy
	serverCfg.HAProxyStatsSocket = cfg.HAProxyStatsSocket
	serverCfg.HAProxyStatsURL = cfg.HAProxyStatsURL
	serverCfg.HAProxyStatsUser = cfg.HAProxyStatsUser
	serverCfg.HAProxyStatsPassword = cfg.HAProxyStatsPassword
	serverCfg.HAProxyConfigPath = cfg.HAProxyConfigFile
	if cfg.HAProxyStatsSocket != "" || cfg.HAProxyStatsURL != "" {
		logger.Info("HAProxy: Stats proxy enabled")
	}

	// Configure certificate renewal detection
	serverCfg.CertbotTimer = cfg.CertbotTimer

	server := api.NewServer(serverCfg)

	// Initialize plugin system
	logger.Info("Initializing gear system", "registered_gears", gear.Count())

	// Create plugin dependencies
	gearDeps := gear.Dependencies{
		Logger:               logger,
		EventBus:             eventBus,
		HTTPClient:           http.DefaultClient,
		Config:               make(map[string]any),
		StatsInterval:        time.Duration(cfg.StatsInterval) * time.Second,
		HAProxyStatsSocket:   cfg.HAProxyStatsSocket,
		HAProxyStatsURL:      cfg.HAProxyStatsURL,
		HAProxyStatsUser:     cfg.HAProxyStatsUser,
		HAProxyStatsPassword: cfg.HAProxyStatsPassword,
		HAProxyConfigPath:    cfg.HAProxyConfigFile,
		CertbotTimer:         cfg.CertbotTimer,
		SourceOverrides:      buildSourceOverrides(cfg),
		NginxStatusURL:       cfg.NginxStatusURL,
		NginxConfigFile:      cfg.NginxConfigFile,
		ApacheStatusURL:      cfg.ApacheStatusURL,
		ApacheConfigFile:     cfg.ApacheConfigFile,
		CaddyAdminURL:        cfg.CaddyAdminURL,
		TraefikMetricsURL:    cfg.TraefikMetricsURL,
		DockerSocket:         cfg.DockerSocket,
		HAProxyAccessLog:     cfg.HAProxyAccessLog,
		NginxAccessLog:       cfg.NginxAccessLog,
		ApacheAccessLog:      cfg.ApacheAccessLog,
		CaddyAccessLog:       cfg.CaddyAccessLog,
	}

	// Create plugin manager
	gearManager := gear.NewManager(gearDeps, logger)

	// Probe the host: each gear self-reports whether its prerequisites are
	// present. Gears that probe negative are skipped for the rest of the
	// lifecycle (no Initialize, no Start, no routes). A summary table is
	// written to stderr (→ systemd journal) so operators can see at a
	// glance which gears are running on this box and why others aren't.
	ctx := context.Background()
	gearManager.ProbeAll(ctx)

	// Initialize the gears that probed Available.
	if err := gearManager.InitializeAll(ctx); err != nil {
		logger.Error("Failed to initialize plugins", "error", err)
		os.Exit(1)
	}

	// Start all plugins
	if err := gearManager.StartAll(ctx); err != nil {
		logger.Error("Failed to start plugins", "error", err)
		os.Exit(1)
	}

	// Register plugin routes with the server
	// Apply rate limiting and API key auth middleware (with per-IP auth-failure
	// backoff layered on top — see 2026-05 audit P1-7).
	rateLimiter := middleware.DefaultRateLimiter(logger)
	authBackoff := middleware.DefaultBackoffTracker(logger)
	keyRingHandler := api.NewKeyRingHandler(server.KeyRing(), logger)
	pluginRouter := server.Router().Group(func(r chi.Router) {
		r.Use(middleware.RateLimitMiddleware(rateLimiter))
		r.Use(middleware.APIKeyAuth(server.KeyRing(), logger, authBackoff))
	})
	gearManager.RegisterRoutes(pluginRouter)
	gearManager.RegisterSystemRoutes(pluginRouter)
	keyRingHandler.RegisterRoutes(pluginRouter)

	logger.Info("Plugin system initialized",
		"plugins", gear.Names(),
		"services", gear.GetAllMonitoredServices(),
	)

	// Channel to signal server startup failure
	serverErr := make(chan error, 1)

	// Start server in goroutine
	go func() {
		if err := server.Start(); err != nil {
			serverErr <- err
		}
	}()

	// Channel to signal sync loop completion
	syncDone := make(chan struct{})
	// Channel to signal sync loop should stop
	stopSync := make(chan struct{})
	if syncService != nil {
		// Determine if we should poll
		// Poll if: webhooks disabled, OR webhooks enabled with poll backup
		shouldPoll := !cfg.WebhookEnabled || cfg.WebhookPollBackup

		if shouldPoll {
			logger.Info("Polling: Enabled", "interval_sec", cfg.PollInterval)
		} else {
			logger.Info("Polling: Disabled (webhook-only mode)")
		}

		go func() {
			defer close(syncDone) // Signal completion when goroutine exits

			// Run initial sync
			logger.Info("Running initial sync...")
			result := syncService.RunOnce(true)
			if result.Error != nil {
				logger.Error("Initial sync failed", "error", result.Error)
			} else {
				logger.Info("Initial sync complete", "backends", result.BackendCount)
			}

			triggerChan := syncService.GetTriggerChan()

			if shouldPoll {
				// Polling mode: use ticker + webhook triggers
				ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
				defer ticker.Stop()

				for {
					select {
					case <-ticker.C:
						result := syncService.RunOnce(false)
						if result.Error != nil {
							logger.Error("Sync error", "error", result.Error)
						} else if result.Updated {
							logger.Info("Sync updated",
								"backends", result.BackendCount,
								"config_changed", result.ConfigChanged,
							)
						}
					case <-triggerChan:
						// Webhook triggered sync
						logger.Info("Webhook triggered sync...")
						result := syncService.RunOnce(false)
						if result.Error != nil {
							logger.Error("Webhook sync error", "error", result.Error)
						} else if result.Updated {
							logger.Info("Webhook sync updated",
								"backends", result.BackendCount,
								"config_changed", result.ConfigChanged,
							)
						} else {
							logger.Info("Webhook sync: no changes detected")
						}
					case <-stopSync:
						return
					}
				}
			} else {
				// Webhook-only mode: no polling, just listen for webhook triggers
				for {
					select {
					case <-triggerChan:
						logger.Info("Webhook triggered sync...")
						result := syncService.RunOnce(false)
						if result.Error != nil {
							logger.Error("Webhook sync error", "error", result.Error)
						} else if result.Updated {
							logger.Info("Webhook sync updated",
								"backends", result.BackendCount,
								"config_changed", result.ConfigChanged,
							)
						} else {
							logger.Info("Webhook sync: no changes detected")
						}
					case <-stopSync:
						return
					}
				}
			}
		}()
	}

	// Wait for interrupt signal or server error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		logger.Info("Shutdown signal received")
	case err := <-serverErr:
		logger.Error("Server error", "error", err)
	}

	// Stop plugins
	logger.Info("Stopping plugins...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := gearManager.StopAll(shutdownCtx); err != nil {
		logger.Warn("Error stopping plugins", "error", err)
	}
	shutdownCancel()

	// Stop sync loop and wait for it to finish
	close(stopSync)
	if syncService != nil {
		// Wait for sync goroutine to finish (with timeout)
		select {
		case <-syncDone:
			logger.Info("Sync loop stopped")
		case <-time.After(5 * time.Second):
			logger.Warn("Sync loop stop timed out")
		}
	}

	// Graceful shutdown of HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", "error", err)
	}

	logger.Info("Server stopped")
}

// buildSourceOverrides packs the per-category override env vars from
// the agent's Config into the category-keyed map that Dependencies
// (and the manager's primary-source resolver) consume. Empty values
// are omitted so the map only contains explicit operator picks —
// callers downstream treat absence as "auto-detect".
func buildSourceOverrides(cfg *config.Config) map[gear.MetricCategory]string {
	out := make(map[gear.MetricCategory]string)
	if cfg.HTTPSource != "" {
		out[gear.CategoryHTTPRequests] = cfg.HTTPSource
	}
	return out
}

// generateConsoleEd25519 mints an ed25519 keypair encoded in the
// formats sshd expects: OpenSSH PEM for the private side, single-line
// authorized_keys format for the public side. Used only by the
// --generate-console-key one-shot flag, so we keep the implementation
// inline rather than scattering it across the framework.
func generateConsoleEd25519() (pub, priv []byte, err error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate ed25519: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal public: %w", err)
	}
	// "gearbox-agent" comment makes the key easy to identify on the
	// host (`grep gearbox-agent ~/.ssh/authorized_keys`).
	pub = append(ssh.MarshalAuthorizedKey(sshPub)[:len(ssh.MarshalAuthorizedKey(sshPub))-1], []byte(" gearbox-agent\n")...)
	pemBlock, err := ssh.MarshalPrivateKey(privateKey, "gearbox-agent console bridge")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal private: %w", err)
	}
	priv = pem.EncodeToMemory(pemBlock)
	return pub, priv, nil
}
