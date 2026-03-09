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
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/api"
	"github.com/sarg3nt/gearbox-agent/internal/framework/config"
	"github.com/sarg3nt/gearbox-agent/internal/framework/crypto"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/middleware"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/sync"

	// Import plugins - blank identifier triggers init() registration
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/certs"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/containers"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/haproxy"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/logs"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/metrics"
	_ "github.com/sarg3nt/gearbox-agent/internal/gears/security"
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

	// Handle API key commands
	if *showAPIKey {
		key, err := crypto.ReadAPIKey(cfg.APIKeyPath)
		if err != nil {
			logger.Error("Failed to read API key", "error", err)
			os.Exit(1)
		}
		fmt.Println(key)
		os.Exit(0)
	}

	if *rotateAPIKey {
		key, err := crypto.GenerateAPIKey()
		if err != nil {
			logger.Error("Failed to generate API key", "error", err)
			os.Exit(1)
		}
		if err := os.WriteFile(cfg.APIKeyPath, []byte(key), crypto.APIKeyFilePerms); err != nil {
			logger.Error("Failed to write API key", "error", err)
			os.Exit(1)
		}
		fmt.Println("New API key generated:")
		fmt.Println(key)
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

	// Normal startup
	logger.Info("Starting gearbox-agent",
		"version", Version,
		"commit", CommitSHA,
		"built", BuildDate,
	)

	// Load or create API key
	apiKey, isNewKey, err := crypto.LoadOrCreateAPIKey(cfg.APIKeyPath)
	if err != nil {
		logger.Error("Failed to initialize API key", "error", err)
		os.Exit(1)
	}
	if isNewKey {
		logger.Warn("NEW API KEY GENERATED - Save this key, it will not be shown again!")
		// Print to stdout (not logger) so it doesn't appear in system logs
		fmt.Printf("API Key: %s\n", apiKey)
		logger.Info("API key saved", "path", cfg.APIKeyPath)
	} else {
		logger.Info("API key loaded from file")
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
		// Use self-signed certs (generate if needed)
		hosts := []string{"localhost"}
		if hostname, err := os.Hostname(); err == nil {
			hosts = append(hosts, hostname)
		}

		var isNewCert bool
		tlsCfg, isNewCert, err = crypto.LoadOrCreateTLSCert(cfg.TLSCert, cfg.TLSKey, hosts)
		if err != nil {
			logger.Error("Failed to initialize TLS", "error", err)
			os.Exit(1)
		}
		if isNewCert {
			logger.Info("TLS: Generated new self-signed certificate (valid for 1 year)")
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

	// Create and start API server
	serverCfg := api.ServerConfig{
		ListenAddr: cfg.ListenAddr,
		APIKey:     apiKey,
		CertFile:   tlsCfg.CertPath,
		KeyFile:    tlsCfg.KeyPath,
		Version:    Version,
		Logger:     logger,
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
	}

	// Create plugin manager
	gearManager := gear.NewManager(gearDeps, logger)

	// Initialize all plugins
	ctx := context.Background()
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
	// Apply rate limiting and API key auth middleware
	rateLimiter := middleware.DefaultRateLimiter(logger)
	pluginRouter := server.Router().Group(func(r chi.Router) {
		r.Use(middleware.RateLimitMiddleware(rateLimiter))
		r.Use(middleware.APIKeyAuth(server.APIKey(), logger))
	})
	gearManager.RegisterRoutes(pluginRouter)

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
