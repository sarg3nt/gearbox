package handler

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// APICertificatesHandler returns certificate information for a server.
func (h *Handler) APICertificatesHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view certificates
	if !h.authManager.HasPermission(r, models.ComponentCertificates, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view certificates", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	collector, exists := h.getCollector(boxID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Try to get from cache first
	certs, _, _ := collector.GetCache().GetCertificates()
	if certs != nil {
		h.writeJSON(w, certs)
		return
	}

	// Fallback: fetch directly from agent
	serverConfig, exists := h.getServerConfig(boxID)
	if !exists || !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	certsResp, err := agentClient.GetCertificates()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get certificates", err))
		return
	}

	h.writeJSON(w, certsResp)
}

// APICertificateRefreshHandler triggers a certificate renewal.
func (h *Handler) APICertificateRefreshHandler(w http.ResponseWriter, r *http.Request) {
	// Check permission - requires action permission to refresh/rotate certificates
	if !h.authManager.HasPermission(r, models.ComponentCertificates, models.PermissionAction) {
		http.Error(w, "Forbidden: insufficient permissions to refresh certificates", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	domain := chi.URLParam(r, "domain")

	if boxID == "" || domain == "" {
		http.Error(w, "Server ID and domain required", http.StatusBadRequest)
		return
	}

	serverConfig, exists := h.getServerConfig(boxID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	if !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	result, err := agentClient.RefreshCertificate(domain)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("refresh certificate", err))
		return
	}

	h.writeJSON(w, result)
}

// APICertificateDownloadHandler proxies certificate download from the agent.
func (h *Handler) APICertificateDownloadHandler(w http.ResponseWriter, r *http.Request) {
	// Check permission - requires download permission to download certificate files
	if !h.authManager.HasPermission(r, models.ComponentCertificates, models.PermissionDownload) {
		http.Error(w, "Forbidden: insufficient permissions to download certificates", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	domain := chi.URLParam(r, "domain")

	if boxID == "" || domain == "" {
		http.Error(w, "Server ID and domain required", http.StatusBadRequest)
		return
	}

	serverConfig, exists := h.getServerConfig(boxID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	if !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	certData, filename, err := agentClient.DownloadCertificate(domain)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("download certificate", err))
		return
	}

	// Set headers for file download
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(certData)))
	w.Write(certData)
}
