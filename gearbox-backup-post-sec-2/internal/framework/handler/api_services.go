package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// APIServicesConfigHandler returns the services integration config for a server.
func (h *Handler) APIServicesConfigHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view services
	if !h.authManager.HasPermission(r, models.ComponentServices, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view services", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Ensure default integrations exist for this server
	if err := h.db.EnsureServerPlugins(serverID); err != nil {
		h.logger.Error("Failed to ensure server integrations", "error", err)
	}

	// Get services config for this server
	config, err := h.db.GetServicesConfig(serverID)
	if err != nil {
		h.logger.Error("Failed to get services config", "error", err)
		config = &database.ServicesConfig{
			MonitoredServices: []string{"haproxy", "gearbox-agent", "nftables", "fail2ban"},
		}
	}

	h.writeJSON(w, config)
}

// APIServicesHandler returns service status for a server.
func (h *Handler) APIServicesHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view services
	if !h.authManager.HasPermission(r, models.ComponentServices, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view services", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get server config
	serverConfig, exists := h.getServerConfig(serverID)
	if !exists || !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	// Parse services from query param
	servicesParam := r.URL.Query().Get("services")
	var services []string
	if servicesParam != "" {
		for _, s := range strings.Split(servicesParam, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				services = append(services, s)
			}
		}
	}

	// Fetch from agent
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	servicesResp, err := agentClient.GetServices(services)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get services", err))
		return
	}

	h.writeJSON(w, servicesResp)
}

// APIServiceControlHandler handles service control requests (start/stop/restart).
// This is an admin-only endpoint.
func (h *Handler) APIServiceControlHandler(w http.ResponseWriter, r *http.Request) {
	// Check admin permission
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || !user.IsAdmin() {
		http.Error(w, "Admin access required", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get server config
	serverConfig, exists := h.getServerConfig(serverID)
	if !exists || !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	// Parse request body
	var req struct {
		Service string `json:"service"`
		Action  string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate action
	if req.Action != "start" && req.Action != "stop" && req.Action != "restart" {
		http.Error(w, "Invalid action. Must be start, stop, or restart", http.StatusBadRequest)
		return
	}

	// Validate service name (basic sanity check)
	if req.Service == "" || len(req.Service) > 256 {
		http.Error(w, "Invalid service name", http.StatusBadRequest)
		return
	}

	// Forward to agent
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	resp, err := agentClient.ServiceControl(req.Service, req.Action)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("control service", err))
		return
	}

	h.writeJSON(w, resp)
}
