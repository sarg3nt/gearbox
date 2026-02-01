package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// APILogsHandler returns logs as JSON.
// Supported log names: haproxy, system, fail2ban
func (h *Handler) APILogsHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view logs
	if !h.authManager.HasPermission(r, models.ComponentLogs, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view logs", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	logName := chi.URLParam(r, "logName")

	if boxID == "" || logName == "" {
		http.Error(w, "Server ID and log name required", http.StatusBadRequest)
		return
	}

	collector, exists := h.getCollector(boxID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Fetch logs via Agent API
	content, err := collector.GetLog(logName, 1000)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("fetch logs", err))
		return
	}

	response := map[string]interface{}{
		"log_name": logName,
		"logs":     content,
		"lines":    len(content),
	}

	h.writeJSON(w, response)
}

// APILogSourcesHandler returns the enabled log sources for a server.
// If no explicit settings exist, it fetches available sources from the agent.
func (h *Handler) APILogSourcesHandler(w http.ResponseWriter, r *http.Request) {
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get enabled log sources from database
	sources, err := h.db.GetEnabledLogSourcesByServerID(boxID)
	if err != nil {
		h.logger.Error("Failed to get log sources", "error", err)
		http.Error(w, "Failed to get log sources", http.StatusInternalServerError)
		return
	}

	// If no explicit settings, return default sources (haproxy, system)
	if len(sources) == 0 {
		h.writeJSON(w, map[string]interface{}{
			"server_id": boxID,
			"sources": []map[string]string{
				{"name": "haproxy", "display_name": "HAProxy"},
				{"name": "system", "display_name": "System"},
			},
			"has_settings": false,
		})
		return
	}

	// Convert to response format
	sourcesResp := make([]map[string]string, 0, len(sources))
	for _, s := range sources {
		sourcesResp = append(sourcesResp, map[string]string{
			"name":         s.LogName,
			"display_name": s.DisplayName,
		})
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id":    boxID,
		"sources":      sourcesResp,
		"has_settings": true,
	})
}
