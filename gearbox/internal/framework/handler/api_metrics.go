package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/gears/metrics"
)

// APIMetricsCPUHandler returns an HTML fragment with CPU usage for the dashboard widget.
func (h *Handler) APIMetricsCPUHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
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

	m, _, _ := collector.GetSystemMetrics()
	if m == nil {
		http.Error(w, "No metrics available", http.StatusServiceUnavailable)
		return
	}

	component := metrics.CPUPartial(m.CPUUsagePercent)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render CPU partial", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIMetricsMemoryHandler returns an HTML fragment with memory usage for the dashboard widget.
func (h *Handler) APIMetricsMemoryHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
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

	m, _, _ := collector.GetSystemMetrics()
	if m == nil {
		http.Error(w, "No metrics available", http.StatusServiceUnavailable)
		return
	}

	component := metrics.MemoryPartial(m.MemoryUsedBytes, m.MemoryTotalBytes, m.MemoryUsagePercent)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render memory partial", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIMetricsDiskHandler returns an HTML fragment with disk usage for the dashboard widget.
func (h *Handler) APIMetricsDiskHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
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

	m, _, _ := collector.GetSystemMetrics()
	if m == nil {
		http.Error(w, "No metrics available", http.StatusServiceUnavailable)
		return
	}

	component := metrics.DiskPartial(m.DiskUsedBytes, m.DiskTotalBytes, m.DiskUsagePercent)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render disk partial", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIMetricsNetworkHandler returns an HTML fragment with network traffic for the dashboard widget.
func (h *Handler) APIMetricsNetworkHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
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

	m, _, _ := collector.GetSystemMetrics()
	if m == nil {
		http.Error(w, "No metrics available", http.StatusServiceUnavailable)
		return
	}

	component := metrics.NetworkPartial(m.NetworkRxMbps, m.NetworkTxMbps)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render network partial", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIMetricsLoadHandler returns an HTML fragment with load average for the dashboard widget.
func (h *Handler) APIMetricsLoadHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
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

	m, _, _ := collector.GetSystemMetrics()
	if m == nil {
		http.Error(w, "No metrics available", http.StatusServiceUnavailable)
		return
	}

	component := metrics.LoadPartial(m.LoadAverage1, m.LoadAverage5, m.LoadAverage15)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render load partial", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIMetricsUptimeHandler returns an HTML fragment with system uptime for the dashboard widget.
func (h *Handler) APIMetricsUptimeHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
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

	m, _, _ := collector.GetSystemMetrics()
	if m == nil {
		http.Error(w, "No metrics available", http.StatusServiceUnavailable)
		return
	}

	component := metrics.UptimePartial(m.Uptime)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render uptime partial", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
