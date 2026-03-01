package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/gears/metrics"
)

// APIChartsSessionsRequestsHandler returns an HTML fragment with Sessions & Requests chart.
func (h *Handler) APIChartsSessionsRequestsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Parse query parameters
	hours := parseIntQueryParam(r, "hours", 24)
	limit := parseIntQueryParam(r, "limit", 500)

	component := metrics.SessionsRequestsChartPartial(boxID, hours, limit)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render sessions/requests chart", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIChartsServerHealthHandler returns an HTML fragment with Server Health chart.
func (h *Handler) APIChartsServerHealthHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	hours := parseIntQueryParam(r, "hours", 24)
	limit := parseIntQueryParam(r, "limit", 500)

	component := metrics.ServerHealthChartPartial(boxID, hours, limit)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render server health chart", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIChartsCPULoadHandler returns an HTML fragment with CPU Load chart.
func (h *Handler) APIChartsCPULoadHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	hours := parseIntQueryParam(r, "hours", 24)
	limit := parseIntQueryParam(r, "limit", 500)

	component := metrics.CPULoadChartPartial(boxID, hours, limit)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render CPU load chart", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIChartsMemoryUsageHandler returns an HTML fragment with Memory Usage chart.
func (h *Handler) APIChartsMemoryUsageHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	hours := parseIntQueryParam(r, "hours", 24)
	limit := parseIntQueryParam(r, "limit", 500)

	component := metrics.MemoryUsageChartPartial(boxID, hours, limit)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render memory usage chart", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIChartsNetworkThroughputHandler returns an HTML fragment with Network Throughput chart.
func (h *Handler) APIChartsNetworkThroughputHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	hours := parseIntQueryParam(r, "hours", 24)
	limit := parseIntQueryParam(r, "limit", 500)

	component := metrics.NetworkThroughputChartPartial(boxID, hours, limit)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render network throughput chart", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIChartsResponseTimesHandler returns an HTML fragment with Response Times chart.
func (h *Handler) APIChartsResponseTimesHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	hours := parseIntQueryParam(r, "hours", 24)
	limit := parseIntQueryParam(r, "limit", 500)

	component := metrics.ResponseTimesChartPartial(boxID, hours, limit)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render response times chart", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIChartsErrorRatesHandler returns an HTML fragment with Error Rates chart.
func (h *Handler) APIChartsErrorRatesHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	hours := parseIntQueryParam(r, "hours", 24)
	limit := parseIntQueryParam(r, "limit", 500)

	component := metrics.ErrorRatesChartPartial(boxID, hours, limit)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render error rates chart", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// Helper function to parse integer query parameters with defaults and upper bounds.
func parseIntQueryParam(r *http.Request, param string, defaultValue int) int {
	const maxHours = 168  // 7 days
	const maxLimit = 5000 // max data points

	if valueStr := r.URL.Query().Get(param); valueStr != "" {
		if value, err := strconv.Atoi(valueStr); err == nil && value > 0 {
			// Clamp to upper bounds
			switch param {
			case "hours":
				if value > maxHours {
					value = maxHours
				}
			case "limit":
				if value > maxLimit {
					value = maxLimit
				}
			}
			return value
		}
	}
	return defaultValue
}
