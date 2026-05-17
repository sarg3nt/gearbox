// Package api provides the HTTP/HTTPS API server and core endpoints for the HAProxy agent.
// It handles authentication, rate limiting, WebSocket connections, and webhook integration.
// Plugin-specific routes are registered via the plugin system's RegisterRoutes method.
package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"

	_ "github.com/sarg3nt/gearbox-agent/docs" // Swagger docs
	"github.com/sarg3nt/gearbox-agent/internal/api/console"
	"github.com/sarg3nt/gearbox-agent/internal/framework/crypto"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	frameworkmiddleware "github.com/sarg3nt/gearbox-agent/internal/framework/middleware"
)

// Server represents the HTTP API server.
type Server struct {
	httpServer *http.Server
	router     chi.Router
	logger     *slog.Logger
	keyring    *crypto.KeyRingPointer
	certFile   string
	keyFile    string
}

// ServerConfig holds configuration for the API server.
type ServerConfig struct {
	ListenAddr       string
	KeyRing          *crypto.KeyRingPointer
	CertFile         string
	KeyFile          string
	Version          string
	Logger           *slog.Logger
	MetadataProvider MetadataProvider

	// Webhook settings (optional)
	WebhookEnabled bool
	WebhookSecret  string
	WebhookURL     string
	SyncTrigger    SyncTrigger

	// SwaggerEnabled controls whether the Swagger UI / OpenAPI spec are
	// served at /swagger and /swagger/*. Useful in development; in
	// production it lets unauthenticated callers enumerate the agent's
	// endpoints + schemas. Default false (off in production). See
	// 2026-05 security audit P3-2.
	SwaggerEnabled bool

	// WebSocket settings (optional)
	EventBus *events.Bus

	// HAProxy settings (optional)
	HAProxyStatsSocket   string
	HAProxyStatsURL      string
	HAProxyStatsUser     string
	HAProxyStatsPassword string
	HAProxyConfigPath    string

	// Certificate settings (optional)
	CertbotTimer string // Custom certbot timer name for renewal detection
}

// NewServer creates a new API server.
func NewServer(cfg ServerConfig) *Server {
	// Create handlers
	handlers := NewHandlers(cfg.MetadataProvider)

	// Create chi router
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(frameworkmiddleware.RequestLogger(cfg.Logger))
	r.Use(frameworkmiddleware.PanicRecovery(cfg.Logger))
	r.Use(frameworkmiddleware.MaxBodySize(frameworkmiddleware.MaxRequestBodySize))

	// Create rate limiter (50 req/sec with burst of 100)
	rateLimiter := frameworkmiddleware.DefaultRateLimiter(cfg.Logger)

	// Per-IP auth-failure backoff layered on top of rate limiting — defends
	// against distributed API-key brute force where each IP individually
	// stays below the 50 req/sec rate-limit threshold. See 2026-05 audit P1-7.
	authBackoff := frameworkmiddleware.DefaultBackoffTracker(cfg.Logger)

	// Health endpoint (no auth required, no rate limit)
	r.Get("/health", handlers.Health)

	// Swagger UI (no auth required) — only served when explicitly enabled.
	// In production, leaving this on lets unauthenticated callers enumerate
	// every endpoint + schema, which is a fingerprinting aid for attackers
	// (2026-05 security audit P3-2). Set HAPROXY_AGENT_SWAGGER_ENABLED=true
	// at deploy time when debugging an API contract; default off.
	if cfg.SwaggerEnabled {
		r.Get("/swagger", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
		r.Get("/swagger/*", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
		cfg.Logger.Warn("Swagger UI enabled at /swagger; unauth-readable. Disable for production deploys.")
	}

	// Webhook endpoint (uses GitHub signature verification, not API key)
	var webhookHandler *WebhookHandler
	if cfg.WebhookEnabled && cfg.WebhookSecret != "" {
		webhookHandler = NewWebhookHandler(cfg.WebhookSecret, cfg.SyncTrigger, cfg.WebhookURL, cfg.Logger)
		r.With(frameworkmiddleware.RateLimitMiddleware(rateLimiter)).Post("/api/v1/webhook/github", webhookHandler.HandleWebhook)
	}

	// WebSocket handler (optional, uses token auth)
	var wsHandler *WSHandler
	var wsTokenMgr *WSTokenManager
	if cfg.EventBus != nil {
		wsHandler = NewWSHandler(cfg.EventBus, cfg.Logger)
		wsTokenMgr = NewWSTokenManager()
		r.With(frameworkmiddleware.RateLimitMiddleware(rateLimiter)).Get("/api/v1/events", func(w http.ResponseWriter, r *http.Request) {
			// Validate short-lived token (required)
			token := r.URL.Query().Get("token")
			if token == "" || wsTokenMgr == nil || !wsTokenMgr.ValidateToken(token) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			wsHandler.HandleEvents(w, r)
		})
	}

	// Remote console handler. Always mounted — the dashboard's
	// per-box console_enabled toggle is the sole gate on whether a
	// session can actually open. The agent's surface is still
	// API-key gated (token endpoint) and single-use-token gated
	// (WS endpoint), and the dashboard refuses to proxy to a box
	// that hasn't opted in. See [#89] and the per-box toggle in
	// the dashboard's box settings page.
	consoleHandler := console.NewHandler(cfg.EventBus, cfg.Logger)
	// The token-exchange + capabilities endpoints sit behind the
	// shared API-key + rate-limit + auth-backoff stack; the WS
	// endpoint trusts the single-use token alone (consistent
	// with /api/v1/events).
	r.With(frameworkmiddleware.RateLimitMiddleware(rateLimiter)).Get(
		"/api/v1/console/ws", consoleHandler.HandleWS)

	// Protected API routes (require API key auth)
	r.Group(func(r chi.Router) {
		r.Use(frameworkmiddleware.RateLimitMiddleware(rateLimiter))
		r.Use(frameworkmiddleware.APIKeyAuth(cfg.KeyRing, cfg.Logger, authBackoff))

		// Core endpoints (not handled by plugins)
		r.Get("/api/v1/metadata", handlers.Metadata)
		r.Get("/api/v1/sync/status", handlers.SyncStatus)

		// Webhook info (if enabled)
		if webhookHandler != nil {
			r.Get("/api/v1/webhook/info", webhookHandler.HandleWebhookInfo)
		}

		// WebSocket token exchange (if enabled)
		if wsTokenMgr != nil {
			r.Post("/api/v1/events/token", wsTokenMgr.HandleWSTokenExchange)
			if wsHandler != nil {
				r.Get("/api/v1/events/info", wsHandler.HandleWSInfo)
			}
		}

		// Console token exchange + capabilities. These two sit inside
		// the API-key + auth-backoff group; the WS endpoint itself uses
		// the single-use token and is mounted above.
		r.Post("/api/v1/console/token", consoleHandler.Tokens.HandleTokenExchange)
		r.Get("/api/v1/console/capabilities", consoleHandler.HandleCapabilities)
	})

	return &Server{
		httpServer: &http.Server{
			Addr:         cfg.ListenAddr,
			Handler:      r,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
			// Explicit TLS 1.3 floor. Go's default minimum is TLS 1.2 (still
			// negotiable by misconfigured or older clients); both ends of the
			// agent <-> dashboard channel are controlled by us, so we have
			// no reason to leave TLS 1.2 reachable. Pinning to 1.3 shrinks
			// the negotiation attack surface (no downgrade, fewer cipher
			// suites, AEAD-only, forward secrecy guaranteed). See 2026-05
			// security audit P2-7.
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
			},
		},
		router:   r,
		logger:   cfg.Logger,
		keyring:  cfg.KeyRing,
		certFile: cfg.CertFile,
		keyFile:  cfg.KeyFile,
	}
}

// Router returns the chi router for plugin route registration.
func (s *Server) Router() chi.Router {
	return s.router
}

// KeyRing returns the server's keyring pointer for middleware
// configuration on plugin routes registered after server construction.
func (s *Server) KeyRing() *crypto.KeyRingPointer {
	return s.keyring
}

// Logger returns the server's logger.
func (s *Server) Logger() *slog.Logger {
	return s.logger
}

// Start starts the HTTPS server.
func (s *Server) Start() error {
	s.logger.Info("Starting HTTPS server", "addr", s.httpServer.Addr)
	err := s.httpServer.ListenAndServeTLS(s.certFile, s.keyFile)
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down server...")
	return s.httpServer.Shutdown(ctx)
}

