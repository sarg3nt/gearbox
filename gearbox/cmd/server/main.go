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
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/services"
	"github.com/sarg3nt/gearbox/internal/framework/services/alerts"
	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
	"github.com/sarg3nt/gearbox/internal/framework/services/email"
	"github.com/sarg3nt/gearbox/internal/framework/handler"
	gbmiddleware "github.com/sarg3nt/gearbox/internal/framework/middleware"
	"github.com/sarg3nt/gearbox/internal/framework/models"

	// Import gears - blank identifier triggers init() registration
	_ "github.com/sarg3nt/gearbox/internal/gears/alerts"
	_ "github.com/sarg3nt/gearbox/internal/gears/certificates"
	_ "github.com/sarg3nt/gearbox/internal/gears/haproxy"
	_ "github.com/sarg3nt/gearbox/internal/gears/home"
	_ "github.com/sarg3nt/gearbox/internal/gears/logs"
	_ "github.com/sarg3nt/gearbox/internal/gears/metrics"
	_ "github.com/sarg3nt/gearbox/internal/gears/services"
	_ "github.com/sarg3nt/gearbox/internal/gears/traffic"
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
		"server_count", len(cfg.Boxes))

	// Log debug configuration if in debug mode
	config.LogConfigDebug(cfg, logger)

	// Initialize database first (needed for auth manager)
	db, err := database.New(cfg.DatabasePath, logger)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer func() { _ = db.Close() }()
	logger.Info("database initialized",
		"path", cfg.DatabasePath,
		"retention_hours", cfg.DatabaseRetentionHours)

	// Seed default rows for system-wide gears (e.g. the Home dashboard).
	// Box-scoped gears are seeded lazily on first access; system gears
	// have no box to attach to, so we seed them once here.
	if err := db.EnsureSystemGears(); err != nil {
		logger.Error("failed to seed system gears", "error", err)
	}

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
		// Only write credentials file if password was auto-generated
		// If admin set ADMIN_PASSWORD in .env, they already know it
		if cfg.AdminPassword == "" {
			// SECURITY: Write password to secure file with restrictive permissions
			// File will be auto-deleted after first password change
			credentialsFile := "data/admin-credentials.txt" //#nosec G101 -- Not a credential; this is a file path constant
			credentialsContent := fmt.Sprintf("ADMIN USER CREATED\n\nEmail: admin\nPassword: %s\n\nIMPORTANT: You will be forced to change this password on first login.\nThis file will be automatically deleted after you change your password.\n", adminPassword)

			if err := os.WriteFile(credentialsFile, []byte(credentialsContent), 0600); err != nil {
				logger.Error("failed to write admin credentials to file",
					"error", err,
					"file", credentialsFile)
				logger.Warn("ADMIN CREDENTIALS NOT SAVED - server startup failed")
			} else {
				logger.Info("========================================")
				logger.Info("NEW ADMIN USER CREATED")
				logger.Info("Login email: admin")
				logger.Info("Credentials file:", "path", credentialsFile)
				logger.Info("You must change your password on first login.")
				logger.Info("The credentials file will be auto-deleted after password change.")
				logger.Info("========================================")
			}
		} else {
			// Admin password was set via environment variable
			logger.Info("========================================")
			logger.Info("NEW ADMIN USER CREATED")
			logger.Info("Login email: admin")
			logger.Info("Password: (set via ADMIN_PASSWORD environment variable)")
			logger.Info("You must change your password on first login.")
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

	// Secure cookies are enabled by default; disable only when TLS is not configured
	if cfg.TLSCertPath == "" || cfg.TLSKeyPath == "" {
		authManager.SetSecure(false)
		logger.Warn("TLS not configured — session cookies will be sent over HTTP (insecure)")
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
		// Run cleanup every hour to apply per-box retention policies
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			// Global cleanup (fallback)
			if err := db.CleanupOldData(retentionDuration); err != nil {
				logger.Error("failed to cleanup old data", "error", err)
			} else {
				logger.Info("database cleanup completed")
			}

			// Per-box cleanup based on metrics integration config
			dbServers, err := db.GetEnabledBoxes()
			if err != nil {
				logger.Error("failed to get servers for metrics cleanup", "error", err)
				continue
			}

			for _, dbServer := range dbServers {
				metricsConfig, err := db.GetMetricsConfig(dbServer.BoxID)
				if err != nil {
					logger.Error("failed to get metrics config",
						"server_id", dbServer.BoxID,
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
					deleted, err := db.CleanupMetricsByAge(dbServer.BoxID, retention)
					if err != nil {
						logger.Error("failed to cleanup metrics by age",
							"server_id", dbServer.BoxID,
							"error", err)
					} else if deleted > 0 {
						logger.Info("cleaned up old metrics records",
							"deleted_count", deleted,
							"server_id", dbServer.BoxID)
					}
				case database.MetricsRetentionBySize:
					deleted, err := db.CleanupMetricsBySize(dbServer.BoxID, metricsConfig.RetentionSizeMB)
					if err != nil {
						logger.Error("failed to cleanup metrics by size",
							"server_id", dbServer.BoxID,
							"error", err)
					} else if deleted > 0 {
						logger.Info("cleaned up metrics records by size",
							"deleted_count", deleted,
							"server_id", dbServer.BoxID)
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

	// Load boxes from database
	dbServers, err := db.GetEnabledBoxes()
	if err != nil {
		log.Fatalf("Failed to load servers from database: %v", err)
	}

	// Convert database boxes to models.BoxConfig with decrypted credentials
	var servers []models.BoxConfig
	for _, dbServer := range dbServers {
		// Decrypt Agent API key
		apiKey, err := encryptor.DecryptString(dbServer.APIKeyEncrypted)
		if err != nil {
			logger.Error("failed to decrypt API key (skipping server)",
				"server_id", dbServer.BoxID,
				"error", err)
			continue
		}

		serverConfig := dbServer.ToBoxConfig(apiKey)

		// Validate the box has a valid Agent API configuration
		if !serverConfig.UsesAgentAPI() {
			logger.Warn("server has no valid Agent API configuration, skipping",
				"server_id", dbServer.BoxID)
			continue
		}

		servers = append(servers, serverConfig)
	}

	logger.Info("loaded enabled servers from database",
		"server_count", len(servers))

	// If no boxes configured, warn but continue (admin can configure via UI)
	if len(servers) == 0 {
		logger.Warn("no boxes configured, admin users can add servers via /settings/boxes")
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

	// Add collectors for each box
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

	// Start WebSocket connections for Agent-based boxes
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
		logger.Info("started WebSocket connections for Agent-based boxes",
			"server_count", agentServerCount)
	}

	// Initialize HTTP handler
	h := handler.NewHandler(authManager, collectors, servers, logger, db, emailService, encryptor, registry)
	h.SetEventHub(eventHub)
	h.SetWebSocketManager(wsManager)
	logger.Info("HTTP handlers initialized")

	// Initialize gear system
	logger.Info("initializing gear system",
		"gear_count", len(gear.All()))

	// Create gear dependencies
	authAdapter := services.NewAuthAdapter(authManager)
	eventsAdapter := services.NewEventsAdapter(eventHub)
	serverAdapter := services.NewServerAdapter(db, encryptor, servers, logger)

	gearDeps := gear.Dependencies{
		DB:             db.GetDB(), // Get the underlying *sql.DB
		Logger:         logger,
		EventHub:       eventsAdapter,
		Auth:           authAdapter,
		Servers:        serverAdapter,
		HTTPClient:     http.DefaultClient,
		Config:         make(map[string]any),
	}

	// Create gear manager (no store for now - will add database-backed store later)
	gearManager := gear.NewManager(gearDeps, nil, logger)

	// Initialize all gears
	if err := gearManager.InitializeAll(context.Background()); err != nil {
		log.Fatalf("Failed to initialize gears: %v", err)
	}

	// Start all gears
	if err := gearManager.StartAll(context.Background()); err != nil {
		log.Fatalf("Failed to start gears: %v", err)
	}
	logger.Info("gear system initialized")

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
	// Inject asset configuration for templates (CDN vs local assets)
	r.Use(gbmiddleware.InjectAssetConfig(cfg.UseLocalAssets))
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
		_, _ = w.Write(data)
	})
	r.Get("/favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		data, err := gearbox.StaticFiles.ReadFile("static/favicon.svg")
		if err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		_, _ = w.Write(data)
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

	// Rate limiter for authentication endpoints (5 req/sec, burst of 10 per IP)
	// Protects against brute force and credential stuffing attacks
	authRateLimiter := gbmiddleware.NewRateLimiter(5, 10, logger)
	defer authRateLimiter.Close()

	// Public routes - authentication (rate limited)
	r.Group(func(r chi.Router) {
		r.Use(gbmiddleware.RateLimitMiddleware(authRateLimiter))
		r.Get("/login", h.LoginPage)
		r.Post("/login", h.LoginPost)

		// Account request
		r.Get("/request-account", h.RequestAccountPage)
		r.Post("/request-account", h.RequestAccountPost)
		r.Get("/request-account/success", h.RequestAccountSuccessPage)

		// Password reset
		r.Get("/forgot-password", h.ForgotPasswordPage)
		r.Post("/forgot-password", h.ForgotPasswordPost)
		r.Get("/reset-password", h.ResetPasswordPage)
		r.Post("/reset-password", h.ResetPasswordPost)

		// Passkey authentication
		r.Get("/api/passkey/login/begin", h.PasskeyLoginBegin)
		r.Post("/api/passkey/login/finish", h.PasskeyLoginFinish)
	})

	// SSE endpoint - needs auth but NO timeout (long-lived connections)
	r.Group(func(r chi.Router) {
		r.Use(authManager.RequireAuth)
		// No timeout middleware for SSE - connections stay open indefinitely
		r.Get("/api/events", h.APIEventsHandler)
	})

	// Protected routes (require authentication + timeout)
	r.Group(func(r chi.Router) {
		r.Use(authManager.RequireAuth)
		r.Use(authManager.RequirePasswordChange) // Enforce password change before any other action
		r.Use(h.InjectIntegrationStatus) // Add integration status to context for sidebar rendering
		r.Use(middleware.Timeout(60 * time.Second))

		// Logout
		r.Post("/logout", h.Logout)

		// Root URL — redirect to the user's default-landing-path
		// (per-user → system → fallback). See feature/dashboard-gear F1.
		r.Get("/", h.RootRedirect)

		// Settings routes
		r.Route("/settings", func(r chi.Router) {
			// Settings menu page (accessible by all authenticated users)
			r.Get("/", h.SettingsPage)

			// Account setup for first-time login (force password change)
			r.Get("/complete-account-setup", h.CompleteAccountSetupPage)
			r.Post("/complete-account-setup", h.CompleteAccountSetupPost)

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

			// Box management (requires PermissionManageBoxes - checked in handlers)
			r.Get("/boxes", h.HAProxyBoxesPage)
			r.Get("/boxes/new", h.HAProxyBoxNewPage)
			r.Post("/boxes/new", h.HAProxyBoxCreatePost)
			r.Get("/boxes/{id}/edit", h.HAProxyBoxEditPage)
			r.Post("/boxes/{id}/edit", h.HAProxyBoxUpdatePost)
			r.Post("/boxes/{id}/delete", h.HAProxyBoxDeletePost)
			r.Post("/boxes/{id}/toggle", h.HAProxyBoxTogglePost)
			r.Post("/boxes/test", h.HAProxyBoxTestConnectionPost)
			r.Get("/boxes/{id}/logs", h.HAProxyBoxLogSettingsPage)
			r.Post("/boxes/{id}/logs", h.HAProxyBoxLogSettingsPost)
			r.Get("/boxes/{id}/git", h.BoxGitSettingsPage)
			r.Post("/boxes/{id}/git", h.BoxGitSettingsSave)

			// Disabled entities management (requires ComponentBackends/PermissionManage - checked in handler)
			r.Get("/admin/disabled-entities", h.AdminDisabledEntitiesPage)

			// Gears management (requires ComponentGears/PermissionManage - checked in handlers)
			r.Get("/gears", h.GearsPage)
			r.Post("/gears/haproxy/git", h.HAProxyGitSettingsSave)
			r.Get("/gears/{name}", h.GearDetailPage)
			r.Post("/gears/{name}", h.GearUpdatePost)
			r.Post("/gears/{name}/toggle", h.GearTogglePost)
			r.Get("/gears/alerts/rules", h.AlertRulesPage)

			// Database Backup management (admin only - checked in handlers)
			r.Get("/backup", h.BackupPage)
		})

		// Gear-registered routes
		// Gears handle: / (haproxy overview), /status-grid (haproxy), /logs (logs),
		// /services (services), /history (metrics), /certificates (certificates),
		// /traffic (traffic), /alerts (alerts)
		gearManager.RegisterRoutes(r)

		// Page routes (non-gear)
		r.Get("/os-updates", h.OSUpdatesPage)
		r.Get("/box/{boxID}/frontend/{name}", h.FrontendDetailPage)
		r.Get("/box/{boxID}/backend/{name}", h.BackendDetailPage)

		// Config editor pages
		r.Get("/config/haproxy/{boxID}", h.HAProxyConfigPage)
		r.Get("/config/firewall/{boxID}", h.FirewallConfigPage)

		// HTMX partial routes (return HTML fragments)
		r.Get("/htmx/sidebar-nav", h.SidebarNavPartialHandler)
		r.Get("/htmx/{boxID}/status-summary", h.StatusSummaryPartialHandler)
		r.Get("/htmx/{boxID}/backend-grid", h.BackendGridPartialHandler)
		r.Get("/htmx/{boxID}/stats", h.StatsPartialHandler)
		r.Get("/htmx/{boxID}/metrics", h.MetricsPartialHandler)

		// API routes (return JSON) - excluding SSE which is handled above
		r.Route("/api", func(r chi.Router) {
			r.Post("/keepalive", h.APIKeepaliveHandler)
			r.Get("/session-info", h.APISessionInfoHandler)
			r.Get("/servers", h.APIBoxesHandler)
			r.Get("/database/stats", h.APIDatabaseStatsHandler)
			r.Get("/user", h.APICurrentUser)
			r.Get("/user/permissions", h.APIGetCurrentUserPermissions)
			r.Get("/user/pending-count", h.APIPendingUsersCount)
			r.Get("/websocket/status", h.APIWebSocketStatusHandler) // WebSocket connection status
			r.Get("/{boxID}/stats", h.APIStatsHandler)
			r.Get("/{boxID}/metadata", h.APIMetadataHandler)
			r.Get("/{boxID}/metrics", h.APISystemMetricsHandler)
			r.Get("/{boxID}/metrics/cpu", h.APIMetricsCPUHandler)         // HTML partial for CPU widget
			r.Get("/{boxID}/metrics/memory", h.APIMetricsMemoryHandler)   // HTML partial for memory widget
			r.Get("/{boxID}/metrics/disk", h.APIMetricsDiskHandler)       // HTML partial for disk widget
			r.Get("/{boxID}/metrics/network", h.APIMetricsNetworkHandler) // HTML partial for network widget
			r.Get("/{boxID}/metrics/load", h.APIMetricsLoadHandler)       // HTML partial for load widget
			r.Get("/{boxID}/metrics/uptime", h.APIMetricsUptimeHandler)   // HTML partial for uptime widget
			// Chart widget HTML partials (with Chart.js)
			r.Get("/{boxID}/charts/sessions-requests", h.APIChartsSessionsRequestsHandler)
			r.Get("/{boxID}/charts/server-health", h.APIChartsServerHealthHandler)
			r.Get("/{boxID}/charts/cpu-load", h.APIChartsCPULoadHandler)
			r.Get("/{boxID}/charts/memory-usage", h.APIChartsMemoryUsageHandler)
			r.Get("/{boxID}/charts/network-throughput", h.APIChartsNetworkThroughputHandler)
			r.Get("/{boxID}/charts/response-times", h.APIChartsResponseTimesHandler)
			r.Get("/{boxID}/charts/error-rates", h.APIChartsErrorRatesHandler)
			r.Get("/{boxID}/logs/{logName}", h.APILogsHandler)
			r.Get("/{boxID}/log-sources", h.APILogSourcesHandler) // Get enabled log sources
			// History API endpoints
			r.Get("/{boxID}/history/stats", h.APIStatsHistoryHandler)
			r.Get("/{boxID}/history/metrics", h.APISystemMetricsHistoryHandler)
			r.Get("/{boxID}/history/backend/{backendName}", h.APIBackendHistoryHandler)
			r.Get("/{boxID}/incidents", h.APIIncidentsHandler)

			// Disabled entities management
			r.Get("/{boxID}/disabled-entities", h.APIDisabledEntitiesHandler)
			r.Post("/{boxID}/disable-entity", h.APIDisableEntityHandler)
			r.Post("/{boxID}/enable-entity", h.APIEnableEntityHandler)

			// Certificate management
			r.Get("/{boxID}/certificates", h.APICertificatesHandler)
			r.Post("/{boxID}/certificates/{domain}/refresh", h.APICertificateRefreshHandler)
			r.Get("/{boxID}/certificates/{domain}/download", h.APICertificateDownloadHandler)

			// Services management
			r.Get("/{boxID}/services-config", h.APIServicesConfigHandler)
			r.Get("/{boxID}/services/overview", h.APIServicesOverviewHandler) // HTML partial for overview widget
			r.Get("/{boxID}/services/failed", h.APIServicesFailedHandler)     // HTML partial for failed services widget
			r.Get("/{boxID}/services", h.APIServicesHandler)                  // JSON for services page JS
			r.Post("/{boxID}/service-control", h.APIServiceControlHandler)

			// Traffic analysis API
			r.Get("/{boxID}/traffic", h.APITrafficAnalysisHandler)
			r.Get("/{boxID}/traffic/sources", h.APITrafficSourcesHandler)
			r.Get("/{boxID}/traffic/network", h.APITrafficNetworkHandler)

			// Integrations API
			r.Get("/gears", h.APIGearsHandler)
			r.Get("/gears/status", h.APIGearStatusHandler)
			r.Post("/gears/sort-order", h.APIUpdateGearSortOrder)

			// Database Backup API (admin only - checked in handlers)
			r.Post("/backup/create", h.APICreateBackup)
			r.Post("/backup/restore", h.APIRestoreBackup)
			r.Delete("/backup/delete/{path}", h.APIDeleteBackup)
			r.Get("/backup/download/{path}", h.APIDownloadBackup)

			// Metrics storage management
			r.Get("/{boxID}/metrics/storage-stats", h.APIMetricsStorageStatsHandler)
			r.Post("/{boxID}/metrics/clear", h.APIClearMetricsDataHandler)

			// HAProxy config management
			r.Get("/{boxID}/haproxy/config", h.APIHAProxyConfigGet)
			r.Post("/{boxID}/haproxy/config", h.APIHAProxyConfigSave)
			r.Post("/{boxID}/haproxy/config/validate", h.APIHAProxyConfigValidate)
			r.Get("/{boxID}/haproxy/config/backups", h.APIHAProxyConfigBackups)
			r.Post("/{boxID}/haproxy/config/restore", h.APIHAProxyConfigRestore)
			r.Get("/{boxID}/haproxy/config/history", h.APIHAProxyConfigHistory)

			// Firewall config management
			r.Get("/{boxID}/firewall/config", h.APIFirewallConfigGet)
			r.Post("/{boxID}/firewall/config", h.APIFirewallConfigSave)
			r.Post("/{boxID}/firewall/config/validate", h.APIFirewallConfigValidate)
			r.Get("/{boxID}/firewall/config/backups", h.APIFirewallConfigBackups)
			r.Post("/{boxID}/firewall/config/restore", h.APIFirewallConfigRestore)

			// User permissions API
			r.Route("/users", func(r chi.Router) {
				r.Get("/{userID}/permissions", h.APIGetUserPermissions)
				r.Post("/{userID}/permissions", h.APIUpdateUserPermissions)
				r.Post("/{userID}/permissions/template", h.APIApplyPermissionTemplate)
			})

			// Alerts API
			r.Get("/alerts/count", h.APIGlobalAlertCountHandler) // Global active alert count
			r.Get("/{boxID}/alerts", h.APIAlertsHandler)
			r.Get("/{boxID}/alerts/summary", h.APIAlertSummaryHandler)
			r.Get("/{boxID}/alerts/rules", h.APIAlertRulesHandler)
			r.Post("/{boxID}/alerts/rules", h.APICreateAlertRuleHandler)
			r.Get("/{boxID}/alerts/retention", h.APIAlertRetentionConfigHandler)
			r.Post("/{boxID}/alerts/retention", h.APIUpdateAlertRetentionConfigHandler)
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
			r.Get("/os-updates/snapshots/{id}/preview", h.APIPreviewSnapshotHandler)
			r.Delete("/os-updates/snapshots/{id}", h.APIDeleteSnapshotHandler)
			r.Get("/os-updates/packages/installed", h.APIListInstalledPackagesHandler)
			r.Get("/os-updates/packages/search", h.APISearchPackagesHandler)
			r.Post("/os-updates/packages/install", h.APIInstallPackageHandler)
			r.Post("/os-updates/packages/remove", h.APIRemovePackageHandler)
			r.Post("/os-updates/packages/hold", h.APIHoldPackageHandler)
			r.Post("/os-updates/packages/unhold", h.APIUnholdPackageHandler)
			r.Get("/os-updates/pipx", h.APIPipxStatusHandler)
			r.Post("/os-updates/pipx/install", h.APIPipxInstallHandler)
			r.Post("/os-updates/pipx/uninstall", h.APIPipxUninstallHandler)
			r.Post("/os-updates/pipx/upgrade", h.APIPipxUpgradeHandler)
			r.Get("/os-updates/pip", h.APIPipStatusHandler)
			r.Post("/os-updates/pip/install", h.APIPipInstallHandler)
			r.Post("/os-updates/pip/uninstall", h.APIPipUninstallHandler)
			r.Post("/os-updates/pip/upgrade", h.APIPipUpgradeHandler)
			r.Get("/os-updates/python-tools/versions", h.APIPythonToolsVersionsHandler)
			r.Get("/os-updates/pypi-lookup", h.APIPyPILookupHandler)
			r.Get("/os-updates/unattended", h.APIUnattendedConfigHandler)
			r.Post("/os-updates/unattended", h.APIConfigureUnattendedHandler)
			r.Get("/os-updates/operation/{id}", h.APIGetOperationHandler)
			r.Get("/os-updates/logs", h.APIListUpdateLogsHandler)
			r.Get("/os-updates/logs/{id}", h.APIGetUpdateLogHandler)
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
	if err := gearManager.StopAll(context.Background()); err != nil {
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
