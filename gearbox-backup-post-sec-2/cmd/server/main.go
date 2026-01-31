package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	gearbox "github.com/sarg3nt/gearbox"
	"github.com/sarg3nt/gearbox/internal/framework/collector"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/config"
	"github.com/sarg3nt/gearbox/internal/framework/events"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/services/alerts"
	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
	"github.com/sarg3nt/gearbox/internal/framework/services/email"
	"github.com/sarg3nt/gearbox/internal/framework/handler"
	gbmiddleware "github.com/sarg3nt/gearbox/internal/framework/middleware"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/dashboard"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
	"github.com/sarg3nt/gearbox/internal/framework/widget/widgets"

	// Import plugins - blank identifier triggers init() registration
	_ "github.com/sarg3nt/gearbox/internal/plugins/alerts"
	_ "github.com/sarg3nt/gearbox/internal/plugins/certificates"
	dashboardPlugin "github.com/sarg3nt/gearbox/internal/plugins/dashboard"
	_ "github.com/sarg3nt/gearbox/internal/plugins/logs"
	_ "github.com/sarg3nt/gearbox/internal/plugins/metrics"
	_ "github.com/sarg3nt/gearbox/internal/plugins/services"
	_ "github.com/sarg3nt/gearbox/internal/plugins/traffic"
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
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Setup structured logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	logger.Info("starting Gearbox",
		"version", Version,
		"commit", CommitSHA,
		"build_date", BuildDate)
	logger.Info("configuration loaded",
		"log_level", cfg.LogLevel,
		"server_count", len(cfg.Servers))

	// Log debug configuration if in debug mode
	config.LogConfigDebug(cfg, logger)

	// Initialize database first (needed for auth manager)
	db, err := database.New(cfg.DatabasePath, logger)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	logger.Info("database initialized",
		"path", cfg.DatabasePath,
		"retention_hours", cfg.DatabaseRetentionHours)

	// Ensure admin user exists
	var adminPassword string
	var adminPasswordHash string
	mustChangePassword := true

	if cfg.AdminPassword != "" {
		// Use provided password
		adminPassword = cfg.AdminPassword
		adminPasswordHash, err = auth.HashPassword(adminPassword)
		if err != nil {
			log.Fatalf("Failed to hash admin password: %v", err)
		}
	} else {
		// Generate a random password
		adminPassword, err = auth.GenerateRandomPassword()
		if err != nil {
			log.Fatalf("Failed to generate admin password: %v", err)
		}
		adminPasswordHash, err = auth.HashPassword(adminPassword)
		if err != nil {
			log.Fatalf("Failed to hash admin password: %v", err)
		}
	}

	_, isNewAdmin, err := db.EnsureAdminExists(adminPasswordHash, mustChangePassword)
	if err != nil {
		log.Fatalf("Failed to ensure admin user exists: %v", err)
	}
	if isNewAdmin {
		// SECURITY FIX: Write password to secure file instead of logging
		// Prevent password exposure in logs, terminal history, process lists, etc.
		credentialsFile := "data/admin-credentials.txt"
		credentialsContent := fmt.Sprintf("ADMIN USER CREATED\n\nEmail: admin\nPassword: %s\n\nPlease change this password after first login!\nDelete this file after logging in.\n", adminPassword)

		if err := os.WriteFile(credentialsFile, []byte(credentialsContent), 0600); err != nil {
			logger.Error("failed to write admin credentials to file",
				"error", err,
				"file", credentialsFile)
			logger.Warn("ADMIN CREDENTIALS NOT SAVED - check application startup logs for password")
		} else {
			logger.Info("========================================")
			logger.Info("ADMIN USER CREATED")
			logger.Info("Email: admin")
			logger.Info("Credentials saved to:", "file", credentialsFile)
			logger.Info("Please change password after first login and delete the credentials file!")
			logger.Info("========================================")
		}
	}

	// Calculate session timeout
	sessionTimeout := time.Duration(cfg.SessionTimeoutMinutes) * time.Minute

	// Initialize authentication manager with database
	authManager, err := auth.NewManager(db, cfg.SessionSecret, sessionTimeout, logger)
	if err != nil {
		log.Fatalf("Failed to create authentication manager: %v", err)
	}
	logger.Info("authentication manager initialized")

	// Enable secure cookies if TLS is configured
	if cfg.TLSCertPath != "" && cfg.TLSKeyPath != "" {
		authManager.SetSecure(true)
	}

	// Initialize email service
	emailService := email.NewService(db, logger, cfg.BaseURL)
	logger.Info("email service initialized")

	// Initialize encryptor for secrets
	encryptor, err := crypto.NewEncryptor(cfg.SessionSecret)
	if err != nil {
		log.Fatalf("Failed to create encryptor: %v", err)
	}
	logger.Info("encryption service initialized")

	// Start database cleanup routine
	historyInterval := time.Duration(cfg.HistoryIntervalSeconds) * time.Second
	retentionDuration := time.Duration(cfg.DatabaseRetentionHours) * time.Hour
	go func() {
		// Run cleanup every hour to apply per-server retention policies
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			// Global cleanup (fallback)
			if err := db.CleanupOldData(retentionDuration); err != nil {
				logger.Error("failed to cleanup old data", "error", err)
			} else {
				logger.Info("database cleanup completed")
			}

			// Per-server cleanup based on metrics integration config
			dbServers, err := db.GetEnabledServers()
			if err != nil {
				logger.Error("failed to get servers for metrics cleanup", "error", err)
				continue
			}

			for _, dbServer := range dbServers {
				metricsConfig, err := db.GetMetricsConfig(dbServer.ServerID)
				if err != nil {
					logger.Error("failed to get metrics config",
						"server_id", dbServer.ServerID,
						"error", err)
					continue
				}

				// Skip if historical storage is disabled
				if !metricsConfig.StoreHistory {
					continue
				}

				switch metricsConfig.RetentionType {
				case database.MetricsRetentionByDays:
					retention := time.Duration(metricsConfig.RetentionDays) * 24 * time.Hour
					deleted, err := db.CleanupMetricsByAge(dbServer.ServerID, retention)
					if err != nil {
						logger.Error("failed to cleanup metrics by age",
							"server_id", dbServer.ServerID,
							"error", err)
					} else if deleted > 0 {
						logger.Info("cleaned up old metrics records",
							"deleted_count", deleted,
							"server_id", dbServer.ServerID)
					}
				case database.MetricsRetentionBySize:
					deleted, err := db.CleanupMetricsBySize(dbServer.ServerID, metricsConfig.RetentionSizeMB)
					if err != nil {
						logger.Error("failed to cleanup metrics by size",
							"server_id", dbServer.ServerID,
							"error", err)
					} else if deleted > 0 {
						logger.Info("cleaned up metrics records by size",
							"deleted_count", deleted,
							"server_id", dbServer.ServerID)
					}
				}
			}
		}
	}()

	// Calculate refresh intervals
	statsRefreshInterval := time.Duration(cfg.DashboardRefreshSeconds) * time.Second
	metadataRefreshInterval := time.Duration(cfg.ConfigRefreshSeconds) * time.Second
	metricsRefreshInterval := time.Duration(cfg.DashboardRefreshSeconds) * time.Second

	// TTLs for caching (2x refresh intervals)
	statsTTL := statsRefreshInterval * 2
	metadataTTL := metadataRefreshInterval * 2
	metricsTTL := metricsRefreshInterval * 2
	logsTTL := 60 * time.Second

	// Load servers from database
	dbServers, err := db.GetEnabledServers()
	if err != nil {
		log.Fatalf("Failed to load servers from database: %v", err)
	}

	// Convert database servers to models.ServerConfig with decrypted credentials
	var servers []models.ServerConfig
	for _, dbServer := range dbServers {
		// Decrypt Agent API key
		apiKey, err := encryptor.DecryptString(dbServer.APIKeyEncrypted)
		if err != nil {
			logger.Error("failed to decrypt API key (skipping server)",
				"server_id", dbServer.ServerID,
				"error", err)
			continue
		}

		serverConfig := dbServer.ToServerConfig(apiKey)

		// Validate the server has a valid Agent API configuration
		if !serverConfig.UsesAgentAPI() {
			logger.Warn("server has no valid Agent API configuration, skipping",
				"server_id", dbServer.ServerID)
			continue
		}

		servers = append(servers, serverConfig)
	}

	logger.Info("loaded enabled servers from database",
		"server_count", len(servers))

	// If no servers configured, warn but continue (admin can configure via UI)
	if len(servers) == 0 {
		logger.Warn("no servers configured, admin users can add servers via /settings/servers")
	}

	// Initialize collector registry
	registryConfig := collector.RegistryConfig{
		StatsInterval:    statsRefreshInterval,
		MetadataInterval: metadataRefreshInterval,
		MetricsInterval:  metricsRefreshInterval,
		HistoryInterval:  historyInterval,
		StatsTTL:         statsTTL,
		MetadataTTL:      metadataTTL,
		MetricsTTL:       metricsTTL,
		LogsTTL:          logsTTL,
	}
	registry := collector.NewRegistry(logger, db, registryConfig)

	// Add collectors for each server
	for _, server := range servers {
		if err := registry.AddCollector(server); err != nil {
			logger.Warn("failed to add collector for server",
				"server_id", server.ID,
				"error", err)
		}
	}

	logger.Info("collector registry initialized",
		"active_collectors", registry.Count())

	// Get collectors map for handler (for compatibility)
	collectors := registry.GetAllCollectors()

	// Initialize event hub for real-time updates
	eventHub := events.NewHub(logger)
	eventHub.Start()
	logger.Info("event hub initialized for real-time updates")

	// Initialize alert evaluator
	alertEvaluator := alerts.NewEvaluator(db, registry, eventHub, logger)
	alertEvaluator.Start(30 * time.Second) // Evaluate alerts every 30 seconds
	logger.Info("alert evaluator initialized")

	// Initialize WebSocket manager for Agent connections
	wsManager := collector.NewWebSocketManager(eventHub, registry, logger)
	logger.Info("WebSocket manager initialized")

	// Start WebSocket connections for Agent-based servers
	agentServerCount := 0
	for _, server := range servers {
		if server.UsesAgentAPI() {
			if err := wsManager.Connect(server); err != nil {
				logger.Warn("failed to start WebSocket for server",
					"server_id", server.ID,
					"error", err)
			} else {
				agentServerCount++
			}
		}
	}
	if agentServerCount > 0 {
		logger.Info("started WebSocket connections for Agent-based servers",
			"server_count", agentServerCount)
	}

	// Initialize HTTP handler
	h := handler.NewHandler(authManager, collectors, servers, logger, db, emailService, encryptor, registry)
	h.SetEventHub(eventHub)
	h.SetWebSocketManager(wsManager)
	logger.Info("HTTP handlers initialized")

	// Initialize dashboard handler
	dashboardStorage, err := dashboard.NewStorage("data", logger)
	if err != nil {
		log.Fatalf("Failed to initialize dashboard storage: %v", err)
	}
	h.SetDashboardStorage(dashboardStorage)

	// Continue with dashboard storage setup
	err = dashboardStorage.CreateDefaultDashboard()
	if err != nil {
		log.Fatalf("Failed to initialize dashboard storage: %v", err)
	}
	widgetRegistry := widget.NewRegistry(logger)
	dataSourceRegistry := widget.NewDataSourceRegistry(logger)

	// Register core widgets
	widgets.RegisterCoreWidgets(widgetRegistry)

	// Register HAProxy-specific widgets
	if err := dashboardPlugin.RegisterHAProxyWidgets(widgetRegistry); err != nil {
		log.Fatalf("Failed to register HAProxy widgets: %v", err)
	}

	dashboardHandler := handler.NewDashboardHandler(
		dashboardStorage,
		widgetRegistry,
		dataSourceRegistry,
		logger,
		"", // serverID - can be set per request
		"", // userID - can be set per request
	)
	logger.Info("dashboard handler initialized")

	// Initialize plugin system
	logger.Info("initializing plugin system",
		"plugin_count", len(plugin.All()))

	// Create plugin dependencies
	authAdapter := services.NewAuthAdapter(authManager)
	eventsAdapter := services.NewEventsAdapter(eventHub)
	serverAdapter := services.NewServerAdapter(db, encryptor, servers, logger)

	pluginDeps := plugin.Dependencies{
		DB:             db.GetDB(), // Get the underlying *sql.DB
		Logger:         logger,
		EventHub:       eventsAdapter,
		Auth:           authAdapter,
		Servers:        serverAdapter,
		HTTPClient:     http.DefaultClient,
		WidgetRegistry: widgetRegistry,
		Config:         make(map[string]any),
	}

	// Create plugin manager (no store for now - will add database-backed store later)
	pluginManager := plugin.NewManager(pluginDeps, nil, logger)

	// Initialize all plugins
	if err := pluginManager.InitializeAll(context.Background()); err != nil {
		log.Fatalf("Failed to initialize plugins: %v", err)
	}

	// Start all plugins
	if err := pluginManager.StartAll(context.Background()); err != nil {
		log.Fatalf("Failed to start plugins: %v", err)
	}
	logger.Info("plugin system initialized")

	// Initialize WebAuthn for passkey support
	if cfg.WebAuthnRPID != "" && cfg.WebAuthnRPID != "localhost" {
		webAuthnCfg := &auth.WebAuthnConfig{
			RPDisplayName: cfg.WebAuthnRPDisplayName,
			RPID:          cfg.WebAuthnRPID,
			RPOrigins:     cfg.WebAuthnRPOrigins,
		}
		webAuthnMgr, err := auth.NewWebAuthnManager(webAuthnCfg)
		if err != nil {
			logger.Warn("failed to initialize WebAuthn (passkeys disabled)",
				"error", err)
		} else {
			h.SetWebAuthnManager(webAuthnMgr)
			logger.Info("WebAuthn initialized",
				"rp_id", cfg.WebAuthnRPID,
				"origins", cfg.WebAuthnRPOrigins)
		}
	} else {
		logger.Info("WebAuthn disabled (requires BASE_URL with non-localhost domain)")
	}

	// Setup router
	r := chi.NewRouter()

	// Global middleware (applies to all routes)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// SECURITY: Add security headers (CSP, X-Frame-Options, etc.)
	r.Use(gbmiddleware.SecurityHeaders)
	// Note: Timeout middleware is applied per-route group below
	// SSE endpoints need to bypass the timeout middleware

	// Static files (favicon, etc.)
	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400") // Cache for 1 day
		data, err := gearbox.StaticFiles.ReadFile("static/favicon.svg")
		if err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		w.Write(data)
	})
	r.Get("/favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		data, err := gearbox.StaticFiles.ReadFile("static/favicon.svg")
		if err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		w.Write(data)
	})

	// Static file server for CSS, JavaScript, and other assets
	staticFS, err := fs.Sub(gearbox.StaticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(staticFS))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Public routes
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	r.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# Prometheus metrics will be added here\n"))
	})

	// Public routes - authentication
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.LoginPost)

	// Public routes - account request
	r.Get("/request-account", h.RequestAccountPage)
	r.Post("/request-account", h.RequestAccountPost)
	r.Get("/request-account/success", h.RequestAccountSuccessPage)

	// Public routes - password reset
	r.Get("/forgot-password", h.ForgotPasswordPage)
	r.Post("/forgot-password", h.ForgotPasswordPost)
	r.Get("/reset-password", h.ResetPasswordPage)
	r.Post("/reset-password", h.ResetPasswordPost)

	// Public routes - passkey authentication
	r.Get("/api/passkey/login/begin", h.PasskeyLoginBegin)
	r.Post("/api/passkey/login/finish", h.PasskeyLoginFinish)

	// SSE endpoint - needs auth but NO timeout (long-lived connections)
	r.Group(func(r chi.Router) {
		r.Use(authManager.RequireAuth)
		// No timeout middleware for SSE - connections stay open indefinitely
		r.Get("/api/events", h.APIEventsHandler)
	})

	// Protected routes (require authentication + timeout)
	r.Group(func(r chi.Router) {
		r.Use(authManager.RequireAuth)
		r.Use(h.InjectIntegrationStatus) // Add integration status to context for sidebar rendering
		r.Use(middleware.Timeout(60 * time.Second))

		// Logout
		r.Post("/logout", h.Logout)

		// Settings routes
		r.Route("/settings", func(r chi.Router) {
			// Settings menu page (accessible by all authenticated users)
			r.Get("/", h.SettingsPage)

			// User profile and password management
			r.Get("/profile", h.ProfilePage)
			r.Post("/profile", h.ProfileUpdatePost)
			r.Get("/change-password", h.ChangePasswordPage)
			r.Post("/change-password", h.ChangePasswordPost)

			// Passkey management
			r.Get("/api/passkey/register/begin", h.PasskeyRegisterBegin)
			r.Post("/api/passkey/register/finish", h.PasskeyRegisterFinish)
			r.Post("/profile/passkey/delete", h.PasskeyDelete)

			// User management routes (require admin or approve_users permission)
			r.Get("/users", h.AdminUsersPage)
			r.Post("/users/approve", h.AdminApproveUserPost)
			r.Post("/users/deny", h.AdminDenyUserPost)

			// Permissions management routes (require admin or approve_users permission)
			r.Get("/permissions", h.AdminPermissionsPage)
			r.Get("/permissions/{userID}", h.AdminUserPermissionsPage)

			// User management (admin only - checked in handlers)
			r.Get("/users/{userID}", h.AdminUserDetailPage)
			r.Post("/users/{userID}", h.AdminUpdateUserPost)
			r.Post("/users/{userID}/reset-password", h.AdminForcePasswordResetPost)
			r.Post("/users/{userID}/delete", h.AdminDeleteUserPost)
			r.Post("/users/toggle", h.AdminToggleUserPost)
			r.Post("/users/role", h.AdminChangeUserRolePost)

			// SMTP settings (admin only - checked in handlers)
			r.Get("/smtp", h.AdminSMTPSettingsPage)
			r.Post("/smtp", h.AdminSMTPSettingsPost)
			r.Post("/smtp/test", h.AdminSMTPTestPost)

			// Server management (requires PermissionManageServers - checked in handlers)
			r.Get("/servers", h.HAProxyServersPage)
			r.Get("/servers/new", h.HAProxyServerNewPage)
			r.Post("/servers/new", h.HAProxyServerCreatePost)
			r.Get("/servers/{id}/edit", h.HAProxyServerEditPage)
			r.Post("/servers/{id}/edit", h.HAProxyServerUpdatePost)
			r.Post("/servers/{id}/delete", h.HAProxyServerDeletePost)
			r.Post("/servers/{id}/toggle", h.HAProxyServerTogglePost)
			r.Post("/servers/test", h.HAProxyServerTestConnectionPost)
			r.Get("/servers/{id}/logs", h.HAProxyServerLogSettingsPage)
			r.Post("/servers/{id}/logs", h.HAProxyServerLogSettingsPost)
			r.Get("/servers/{id}/git", h.ServerGitSettingsPage)
			r.Post("/servers/{id}/git", h.ServerGitSettingsSave)

			// Disabled entities management (requires ComponentBackends/PermissionManage - checked in handler)
			r.Get("/admin/disabled-entities", h.AdminDisabledEntitiesPage)

			// Plugins management (requires ComponentPlugins/PermissionManage - checked in handlers)
			r.Get("/plugins", h.PluginsPage)
			r.Get("/plugins/{name}", h.PluginDetailPage)
			r.Post("/plugins/{name}", h.PluginUpdatePost)
			r.Post("/plugins/{name}/toggle", h.PluginTogglePost)
			r.Get("/plugins/alerts/rules", h.AlertRulesPage)

			// Database Backup management (admin only - checked in handlers)
			r.Get("/backup", h.BackupPage)
		})

		// Dashboard routes
		dashboardHandler.RegisterRoutes(r)

		// Plugin-registered routes
		// Plugins handle: / (dashboard), /status-grid (dashboard), /logs (logs),
		// /services (services), /history (metrics), /certificates (certificates),
		// /traffic (traffic), /alerts (alerts)
		pluginManager.RegisterRoutes(r)

		// Page routes (non-plugin)
		r.Get("/os-updates", h.OSUpdatesPage)
		r.Get("/server/{serverID}/frontend/{name}", h.FrontendDetailPage)
		r.Get("/server/{serverID}/backend/{name}", h.BackendDetailPage)

		// Config editor pages
		r.Get("/config/haproxy/{serverID}", h.HAProxyConfigPage)
		r.Get("/config/firewall/{serverID}", h.FirewallConfigPage)

		// HTMX partial routes (return HTML fragments)
		r.Get("/htmx/{serverID}/status-summary", h.StatusSummaryPartialHandler)
		r.Get("/htmx/{serverID}/backend-grid", h.BackendGridPartialHandler)
		r.Get("/htmx/{serverID}/stats", h.StatsPartialHandler)
		r.Get("/htmx/{serverID}/metrics", h.MetricsPartialHandler)

		// API routes (return JSON) - excluding SSE which is handled above
		r.Route("/api", func(r chi.Router) {
			r.Post("/keepalive", h.APIKeepaliveHandler)
			r.Get("/session-info", h.APISessionInfoHandler)
			r.Get("/servers", h.APIServersHandler)
			r.Get("/database/stats", h.APIDatabaseStatsHandler)
			r.Get("/user", h.APICurrentUser)
			r.Get("/user/permissions", h.APIGetCurrentUserPermissions)
			r.Get("/user/pending-count", h.APIPendingUsersCount)
			r.Get("/websocket/status", h.APIWebSocketStatusHandler) // WebSocket connection status
			r.Get("/{serverID}/stats", h.APIStatsHandler)
			r.Get("/{serverID}/metadata", h.APIMetadataHandler)
			r.Get("/{serverID}/metrics", h.APISystemMetricsHandler)
			r.Get("/{serverID}/logs/{logName}", h.APILogsHandler)
			r.Get("/{serverID}/log-sources", h.APILogSourcesHandler) // Get enabled log sources
			// History API endpoints
			r.Get("/{serverID}/history/stats", h.APIStatsHistoryHandler)
			r.Get("/{serverID}/history/metrics", h.APISystemMetricsHistoryHandler)
			r.Get("/{serverID}/history/backend/{backendName}", h.APIBackendHistoryHandler)
			r.Get("/{serverID}/incidents", h.APIIncidentsHandler)

			// Disabled entities management
			r.Get("/{serverID}/disabled-entities", h.APIDisabledEntitiesHandler)
			r.Post("/{serverID}/disable-entity", h.APIDisableEntityHandler)
			r.Post("/{serverID}/enable-entity", h.APIEnableEntityHandler)

			// Certificate management
			r.Get("/{serverID}/certificates", h.APICertificatesHandler)
			r.Post("/{serverID}/certificates/{domain}/refresh", h.APICertificateRefreshHandler)
			r.Get("/{serverID}/certificates/{domain}/download", h.APICertificateDownloadHandler)

			// Services management
			r.Get("/{serverID}/services-config", h.APIServicesConfigHandler)
			r.Get("/{serverID}/services", h.APIServicesHandler)
			r.Post("/{serverID}/service-control", h.APIServiceControlHandler)

			// Traffic analysis API
			r.Get("/{serverID}/traffic", h.APITrafficAnalysisHandler)
			r.Get("/{serverID}/traffic/sources", h.APITrafficSourcesHandler)
			r.Get("/{serverID}/traffic/network", h.APITrafficNetworkHandler)

			// Integrations API
			r.Get("/plugins", h.APIPluginsHandler)
			r.Get("/plugins/status", h.APIPluginStatusHandler)
			r.Post("/plugins/sort-order", h.APIUpdatePluginSortOrder)

			// Database Backup API (admin only - checked in handlers)
			r.Post("/backup/create", h.APICreateBackup)
			r.Post("/backup/restore", h.APIRestoreBackup)
			r.Delete("/backup/delete/{path}", h.APIDeleteBackup)
			r.Get("/backup/download/{path}", h.APIDownloadBackup)

			// Metrics storage management
			r.Get("/{serverID}/metrics/storage-stats", h.APIMetricsStorageStatsHandler)
			r.Post("/{serverID}/metrics/clear", h.APIClearMetricsDataHandler)

			// HAProxy config management
			r.Get("/{serverID}/haproxy/config", h.APIHAProxyConfigGet)
			r.Post("/{serverID}/haproxy/config", h.APIHAProxyConfigSave)
			r.Post("/{serverID}/haproxy/config/validate", h.APIHAProxyConfigValidate)
			r.Get("/{serverID}/haproxy/config/backups", h.APIHAProxyConfigBackups)
			r.Post("/{serverID}/haproxy/config/restore", h.APIHAProxyConfigRestore)
			r.Get("/{serverID}/haproxy/config/history", h.APIHAProxyConfigHistory)

			// Firewall config management
			r.Get("/{serverID}/firewall/config", h.APIFirewallConfigGet)
			r.Post("/{serverID}/firewall/config", h.APIFirewallConfigSave)
			r.Post("/{serverID}/firewall/config/validate", h.APIFirewallConfigValidate)
			r.Get("/{serverID}/firewall/config/backups", h.APIFirewallConfigBackups)
			r.Post("/{serverID}/firewall/config/restore", h.APIFirewallConfigRestore)

			// User permissions API
			r.Route("/users", func(r chi.Router) {
				r.Get("/{userID}/permissions", h.APIGetUserPermissions)
				r.Post("/{userID}/permissions", h.APIUpdateUserPermissions)
				r.Post("/{userID}/permissions/template", h.APIApplyPermissionTemplate)
			})

			// Alerts API
			r.Get("/alerts/count", h.APIGlobalAlertCountHandler) // Global active alert count
			r.Get("/{serverID}/alerts", h.APIAlertsHandler)
			r.Get("/{serverID}/alerts/summary", h.APIAlertSummaryHandler)
			r.Get("/{serverID}/alerts/rules", h.APIAlertRulesHandler)
			r.Post("/{serverID}/alerts/rules", h.APICreateAlertRuleHandler)
			r.Get("/{serverID}/alerts/retention", h.APIAlertRetentionConfigHandler)
			r.Post("/{serverID}/alerts/retention", h.APIUpdateAlertRetentionConfigHandler)
			r.Put("/alerts/rules/{ruleID}", h.APIUpdateAlertRuleHandler)
			r.Delete("/alerts/rules/{ruleID}", h.APIDeleteAlertRuleHandler)
			r.Post("/alerts/{alertID}/acknowledge", h.APIAcknowledgeAlertWithNoteHandler)
			r.Post("/alerts/{alertID}/resolve", h.APIResolveAlertWithNoteHandler)
			r.Post("/alerts/{alertID}/silence", h.APISilenceAlertHandler)
			r.Get("/alerts/{alertID}/notes", h.APIAlertNotesHandler)
			r.Post("/alerts/{alertID}/notes", h.APIAddAlertNoteHandler)

			// OS Updates API
			r.Get("/os-updates/status", h.APIUpdateStatusHandler)
			r.Get("/os-updates/packages", h.APIListPackagesHandler)
			r.Post("/os-updates/check", h.APITriggerUpdateCheckHandler)
			r.Post("/os-updates/install", h.APIInstallUpdatesHandler)
			r.Get("/os-updates/history", h.APIUpdateHistoryHandler)
			r.Post("/os-updates/reboot", h.APIScheduleRebootHandler)
			r.Delete("/os-updates/reboot", h.APICancelRebootHandler)
			r.Get("/os-updates/snapshots", h.APIListSnapshotsHandler)
			r.Post("/os-updates/snapshots", h.APICreateSnapshotHandler)
			r.Post("/os-updates/snapshots/restore", h.APIRestoreSnapshotHandler)
			r.Delete("/os-updates/snapshots/{id}", h.APIDeleteSnapshotHandler)
			r.Get("/os-updates/packages/search", h.APISearchPackagesHandler)
			r.Post("/os-updates/packages/install", h.APIInstallPackageHandler)
			r.Post("/os-updates/packages/remove", h.APIRemovePackageHandler)
			r.Get("/os-updates/pipx", h.APIPipxStatusHandler)
			r.Post("/os-updates/pipx/install", h.APIPipxInstallHandler)
			r.Post("/os-updates/pipx/uninstall", h.APIPipxUninstallHandler)
			r.Post("/os-updates/pipx/upgrade", h.APIPipxUpgradeHandler)
			r.Get("/os-updates/unattended", h.APIUnattendedConfigHandler)
			r.Post("/os-updates/unattended", h.APIConfigureUnattendedHandler)
		})
	})

	// Setup HTTP server
	// Note: WriteTimeout is set to 0 (disabled) to support SSE (Server-Sent Events)
	// which require long-lived connections. The SSE handler manages its own keepalives.
	addr := ":" + cfg.HTTPPort
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Disabled for SSE support
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("server listening", "address", addr)
		if cfg.TLSCertPath != "" && cfg.TLSKeyPath != "" {
			logger.Info("TLS enabled",
				"cert", cfg.TLSCertPath,
				"key", cfg.TLSKeyPath)
			if err := srv.ListenAndServeTLS(cfg.TLSCertPath, cfg.TLSKeyPath); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Failed to start HTTPS server: %v", err)
			}
		} else {
			logger.Warn("running without TLS (HTTP only)")
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Failed to start HTTP server: %v", err)
			}
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")

	// Stop alert evaluator first
	logger.Info("stopping alert evaluator...")
	alertEvaluator.Stop()

	// Stop WebSocket connections
	logger.Info("stopping WebSocket connections...")
	wsManager.StopAll()

	// Stop all collectors gracefully via registry
	logger.Info("stopping all collectors...")
	registry.StopAll()

	// Stop event hub
	logger.Info("stopping event hub...")
	eventHub.Stop()

	// Stop plugins
	logger.Info("stopping plugins...")
	if err := pluginManager.StopAll(context.Background()); err != nil {
		logger.Warn("error stopping plugins", "error", err)
	}

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}

	logger.Info("server stopped")
}
