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
	"github.com/sarg3nt/gearbox/internal/gears/services"
)

// APIServicesConfigHandler returns the services integration config for a server.
func (h *Handler) APIServicesConfigHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view services
	if !h.authManager.HasPermission(r, models.ComponentServices, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view services", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Ensure default integrations exist for this server
	if err := h.db.EnsureServerGears(boxID); err != nil {
		h.logger.Error("Failed to ensure server integrations", "error", err)
	}

	// Get services config for this server
	config, err := h.db.GetServicesConfig(boxID)
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

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get server config
	serverConfig, exists := h.getServerConfig(boxID)
	if !exists || !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	// Parse services from query param
	servicesParam := r.URL.Query().Get("services")
	var svcNames []string
	if servicesParam != "" {
		for _, s := range strings.Split(servicesParam, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				svcNames = append(svcNames, s)
			}
		}
	}

	// Fetch from agent
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	servicesResp, err := agentClient.GetServices(svcNames)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get services", err))
		return
	}

	// If this is an HTMX request (from dashboard widgets), return HTML fragment
	if r.Header.Get("HX-Request") == "true" {
		component := services.ServicesListPartial(servicesResp.Services)
		if err := component.Render(r.Context(), w); err != nil {
			h.logger.Error("Failed to render services list partial", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
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

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get server config
	serverConfig, exists := h.getServerConfig(boxID)
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

// fetchServicesFromAgent is a helper that fetches services from the agent for a given server.
func (h *Handler) fetchServicesFromAgent(boxID string) (*agent.ServicesResponse, error) {
	serverConfig, exists := h.getServerConfig(boxID)
	if !exists || !serverConfig.UsesAgentAPI() {
		return nil, apperrors.NotFound("server", nil)
	}

	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	return agentClient.GetServices(nil)
}

// APIServicesOverviewHandler returns an HTML fragment with services overview summary.
// Used by the services-overview-card widget via HTMX.
func (h *Handler) APIServicesOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentServices, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	servicesResp, err := h.fetchServicesFromAgent(boxID)
	if err != nil {
		h.logger.Error("Failed to fetch services for overview", "box_id", boxID, "error", err)
		http.Error(w, "Failed to fetch services", http.StatusInternalServerError)
		return
	}

	component := services.ServicesOverviewPartial(servicesResp.Services)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render services overview partial", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIServicesFailedHandler returns an HTML fragment with failed services.
// Used by the failed-services-alert widget via HTMX.
func (h *Handler) APIServicesFailedHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentServices, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	servicesResp, err := h.fetchServicesFromAgent(boxID)
	if err != nil {
		h.logger.Error("Failed to fetch services for failed check", "box_id", boxID, "error", err)
		http.Error(w, "Failed to fetch services", http.StatusInternalServerError)
		return
	}

	hideWhenEmpty := r.URL.Query().Get("hide_when_empty") == "true"
	failedServices := services.FilterFailed(servicesResp.Services)

	component := services.FailedServicesPartial(failedServices, hideWhenEmpty)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render failed services partial", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
