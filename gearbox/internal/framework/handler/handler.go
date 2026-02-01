package handler

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/collector"
	"github.com/sarg3nt/gearbox/internal/framework/dashboard"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/events"
	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
	"github.com/sarg3nt/gearbox/internal/framework/services/email"
	"github.com/sarg3nt/gearbox/internal/framework/services/geoip"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	authManager      *auth.Manager
	webAuthnMgr      *auth.WebAuthnManager
	collectors       map[string]*collector.Manager // serverID -> collector
	servers          []models.ServerConfig
	logger           *slog.Logger
	db               *database.DB
	emailService     *email.Service
	encryptor        *crypto.Encryptor
	registry         *collector.Registry
	eventHub         *events.Hub
	wsManager        *collector.WebSocketManager
	geoipClient      *geoip.Client
	dashboardStorage *dashboard.Storage

	// Config redactor for hiding sensitive values (e.g., stats auth passwords)
	configRedactor *ConfigRedactor

	// Traffic delta tracking state
	// HAProxy provides cumulative counters, we need to calculate deltas
	trafficDeltaMu      sync.RWMutex
	prevSourceData      map[string]*trafficSourceSnapshot  // key: "serverID:ip:backend"
	prevBackendData     map[string]*trafficBackendSnapshot // key: "serverID:backend"
}

// trafficSourceSnapshot stores previous values for delta calculation
type trafficSourceSnapshot struct {
	Requests int64
	BytesIn  int64
	BytesOut int64
	LastSeen time.Time
}

// trafficBackendSnapshot stores previous values for delta calculation
type trafficBackendSnapshot struct {
	TotalRequests int64
	BytesIn       int64
	BytesOut      int64
	Response2xx   int64
	Response3xx   int64
	Response4xx   int64
	Response5xx   int64
	LastSeen      time.Time
}

// NewHandler creates a new handler instance.
func NewHandler(
	authManager *auth.Manager,
	collectors map[string]*collector.Manager,
	servers []models.ServerConfig,
	logger *slog.Logger,
	db *database.DB,
	emailService *email.Service,
	encryptor *crypto.Encryptor,
	registry *collector.Registry,
) *Handler {
	return &Handler{
		authManager:     authManager,
		collectors:      collectors,
		servers:         servers,
		logger:          logger,
		db:              db,
		emailService:    emailService,
		encryptor:       encryptor,
		registry:        registry,
		geoipClient:     geoip.NewClient(),
		configRedactor:  NewConfigRedactor(),
		prevSourceData:  make(map[string]*trafficSourceSnapshot),
		prevBackendData: make(map[string]*trafficBackendSnapshot),
	}
}

// SetRegistry sets the collector registry for dynamic management.
func (h *Handler) SetRegistry(registry *collector.Registry) {
	h.registry = registry
}

// SetEncryptor sets the encryptor for handling secrets.
func (h *Handler) SetEncryptor(encryptor *crypto.Encryptor) {
	h.encryptor = encryptor
}

// SetWebAuthnManager sets the WebAuthn manager for passkey support.
func (h *Handler) SetWebAuthnManager(mgr *auth.WebAuthnManager) {
	h.webAuthnMgr = mgr
}

// SetEventHub sets the event hub for real-time updates.
func (h *Handler) SetEventHub(hub *events.Hub) {
	h.eventHub = hub
}

// SetWebSocketManager sets the WebSocket manager for Agent connections.
func (h *Handler) SetWebSocketManager(mgr *collector.WebSocketManager) {
	h.wsManager = mgr
}

// SetDashboardStorage sets the dashboard storage for deploying plugin dashboards.
func (h *Handler) SetDashboardStorage(storage *dashboard.Storage) {
	h.dashboardStorage = storage
}

// getWebAuthnManager returns the WebAuthn manager.
func (h *Handler) getWebAuthnManager() *auth.WebAuthnManager {
	return h.webAuthnMgr
}

// getCollector retrieves a collector for a specific server from the registry.
// This ensures we always get the current state (respecting enable/disable).
func (h *Handler) getCollector(serverID string) (*collector.Manager, bool) {
	if h.registry != nil {
		return h.registry.GetCollector(serverID)
	}
	// Fallback to static map if registry not available
	collector, exists := h.collectors[serverID]
	return collector, exists
}

// getServerConfig retrieves server config by ID.
// First checks the static list, then falls back to database lookup for newly created servers.
func (h *Handler) getServerConfig(serverID string) (*models.ServerConfig, bool) {
	// Check static list first (for backwards compatibility)
	for i := range h.servers {
		if h.servers[i].ID == serverID {
			return &h.servers[i], true
		}
	}

	// Fall back to database lookup for dynamically created servers
	servers := h.getEnabledServers()
	for i := range servers {
		if servers[i].ID == serverID {
			return &servers[i], true
		}
	}
	return nil, false
}

// getEnabledServers returns the list of currently enabled servers from the database.
// This ensures pages always reflect the current enable/disable state.
func (h *Handler) getEnabledServers() []models.ServerConfig {
	dbServers, err := h.db.GetEnabledServers()
	if err != nil {
		h.logger.Error("failed to get enabled servers", "error", err)
		return h.servers // Fallback to static list on error
	}

	encryptor, err := h.getEncryptor()
	if err != nil {
		h.logger.Error("failed to get encryptor", "error", err)
		return h.servers // Fallback to static list on error
	}

	var servers []models.ServerConfig
	for _, dbServer := range dbServers {
		apiKey, _ := encryptor.DecryptString(dbServer.APIKeyEncrypted)
		serverConfig := dbServer.ToServerConfig(apiKey)
		if serverConfig.UsesAgentAPI() {
			servers = append(servers, serverConfig)
		}
	}
	return servers
}

// getDefaultServerID returns the ID of the first enabled server, or empty string if none.
func (h *Handler) getDefaultServerID() string {
	servers := h.getEnabledServers()
	if len(servers) > 0 {
		return servers[0].ID
	}
	return ""
}

// InjectIntegrationStatus is middleware that adds integration status and user permissions to the request context.
// This enables server-side conditional rendering of navigation items based on integration status and permissions.
func (h *Handler) InjectIntegrationStatus(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Inject user permissions into context for sidebar visibility
		perms, err := h.authManager.GetUserPermissions(r)
		if err == nil && perms != nil {
			ctx = auth.SetUserPermissions(ctx, perms)
		}

		serverID := h.getDefaultServerID()
		if serverID != "" {
			// Get integrations with their enabled status and sort order
			integrations, err := h.db.GetPlugins(serverID)
			if err != nil {
				h.logger.Error("failed to get integrations for sidebar", "error", err)
				// Continue without status - sidebar will show all items (fail open)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Build status map for backward compatibility
			status := make(map[string]bool)
			for _, i := range integrations {
				status[i.Name] = i.Enabled
			}

			// Build ordered list for sidebar
			orderedIntegrations := make([]auth.SidebarIntegration, len(integrations))
			for idx, i := range integrations {
				orderedIntegrations[idx] = auth.SidebarIntegration{
					Name:      i.Name,
					Enabled:   i.Enabled,
					SortOrder: i.SortOrder,
				}
			}

			ctx = auth.SetPluginStatus(ctx, status)
			ctx = auth.SetIntegrationOrder(ctx, orderedIntegrations)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// No server configured - explicitly disable all plugins in navigation
		// This provides a clean initial setup experience without plugin clutter
		status := map[string]bool{
			"metrics":      false,
			"logs":         false,
			"services":     false,
			"certificates": false,
			"traffic":      false,
			"alerts":       false,
			"os_updates":   false,
		}
		ctx = auth.SetPluginStatus(ctx, status)
		ctx = auth.SetIntegrationOrder(ctx, []auth.SidebarIntegration{})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
