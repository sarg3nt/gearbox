package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// APIStatsHandler returns HAProxy stats as JSON.
func (h *Handler) APIStatsHandler(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	collector, exists := h.getCollector(serverID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	stats, updatedAt, fresh := collector.GetStats()
	if stats == nil {
		http.Error(w, "No stats available", http.StatusServiceUnavailable)
		return
	}

	response := map[string]interface{}{
		"stats":      stats,
		"updated_at": updatedAt,
		"fresh":      fresh,
	}

	h.writeJSON(w, response)
}

// APIMetadataHandler returns metadata as JSON.
func (h *Handler) APIMetadataHandler(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	collector, exists := h.getCollector(serverID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	metadata, updatedAt, fresh := collector.GetMetadata()
	if metadata == nil {
		http.Error(w, "No metadata available", http.StatusServiceUnavailable)
		return
	}

	response := map[string]interface{}{
		"metadata":   metadata,
		"updated_at": updatedAt,
		"fresh":      fresh,
	}

	h.writeJSON(w, response)
}

// APISystemMetricsHandler returns system metrics as JSON.
func (h *Handler) APISystemMetricsHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view metrics
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	collector, exists := h.getCollector(serverID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	metrics, updatedAt, fresh := collector.GetSystemMetrics()
	if metrics == nil {
		http.Error(w, "No metrics available", http.StatusServiceUnavailable)
		return
	}

	response := map[string]interface{}{
		"metrics":    metrics,
		"updated_at": updatedAt,
		"fresh":      fresh,
	}

	h.writeJSON(w, response)
}

// APIStatsHistoryHandler returns historical stats data.
func (h *Handler) APIStatsHistoryHandler(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Parse hours parameter (default: 24)
	hours := 24
	if hoursStr := r.URL.Query().Get("hours"); hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	// Parse limit parameter (default: 1000)
	limit := 1000
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	history, err := h.db.GetStatsHistory(serverID, since, limit)
	if err != nil {
		h.logger.Error("Failed to get stats history", "error", err)
		http.Error(w, "Failed to get history", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id": serverID,
		"hours":     hours,
		"count":     len(history),
		"data":      history,
	})
}

// APIBackendHistoryHandler returns historical data for a specific backend.
func (h *Handler) APIBackendHistoryHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view metrics
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	backendName := chi.URLParam(r, "backendName")

	if serverID == "" || backendName == "" {
		http.Error(w, "Server ID and backend name required", http.StatusBadRequest)
		return
	}

	// Parse hours parameter (default: 24)
	hours := 24
	if hoursStr := r.URL.Query().Get("hours"); hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	// Parse limit parameter (default: 1000)
	limit := 1000
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	history, err := h.db.GetBackendHistory(serverID, backendName, since, limit)
	if err != nil {
		h.logger.Error("Failed to get backend history", "error", err)
		http.Error(w, "Failed to get history", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id":    serverID,
		"backend_name": backendName,
		"hours":        hours,
		"count":        len(history),
		"data":         history,
	})
}

// APISystemMetricsHistoryHandler returns historical system metrics.
func (h *Handler) APISystemMetricsHistoryHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view metrics
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Parse hours parameter (default: 24)
	hours := 24
	if hoursStr := r.URL.Query().Get("hours"); hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	// Parse limit parameter (default: 1000)
	limit := 1000
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	history, err := h.db.GetSystemMetricsHistory(serverID, since, limit)
	if err != nil {
		h.logger.Error("Failed to get system metrics history", "error", err)
		http.Error(w, "Failed to get history", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id": serverID,
		"hours":     hours,
		"count":     len(history),
		"data":      history,
	})
}
