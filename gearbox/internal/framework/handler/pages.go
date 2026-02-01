package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// Note: The following page handlers have been migrated to plugins and removed:
// - OverviewPage -> plugins/dashboard
// - StatusGridPage -> plugins/dashboard
// - LogsPage -> plugins/logs
// - HistoryPage -> plugins/metrics
// - ServicesPage -> plugins/services
// - CertificatesPage -> plugins/certificates
// - TrafficPage -> plugins/traffic
// - AlertsPage -> plugins/alerts

// FrontendDetailPage serves frontend detail page.
func (h *Handler) FrontendDetailPage(w http.ResponseWriter, r *http.Request) {
	frontendName := chi.URLParam(r, "name")
	boxID := chi.URLParam(r, "boxID")
	user, _ := auth.GetUserFromContext(r.Context())

	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get server configuration
	serverConfig, exists := h.getServerConfig(boxID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Get collector for this server
	collector, exists := h.getCollector(boxID)
	if !exists {
		http.Error(w, "Server collector not found", http.StatusNotFound)
		return
	}

	// Get stats and metadata from the specific server
	stats, _, _ := collector.GetCache().GetStats()
	metadata, _, _ := collector.GetCache().GetMetadata()

	// Render frontend detail template
	component := pages.FrontendDetail(user, frontendName, stats, metadata, boxID, serverConfig.Name)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render frontend detail template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// BackendDetailPage serves backend detail page.
func (h *Handler) BackendDetailPage(w http.ResponseWriter, r *http.Request) {
	backendName := chi.URLParam(r, "name")
	boxID := chi.URLParam(r, "boxID")
	user, _ := auth.GetUserFromContext(r.Context())

	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get server configuration
	serverConfig, exists := h.getServerConfig(boxID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Get collector for this server
	collector, exists := h.getCollector(boxID)
	if !exists {
		http.Error(w, "Server collector not found", http.StatusNotFound)
		return
	}

	// Get stats and metadata from the specific server
	stats, _, _ := collector.GetCache().GetStats()
	metadata, _, _ := collector.GetCache().GetMetadata()

	// Get disabled entity status for this backend
	var disabledEntity *database.DisabledEntity
	entity, err := h.db.GetDisabledEntity(boxID, database.EntityTypeBackend, backendName)
	if err != nil {
		h.logger.Error("Failed to get disabled entity status", "error", err)
	} else {
		disabledEntity = entity
	}

	// Render backend detail template
	component := pages.BackendDetail(user, backendName, stats, metadata, boxID, serverConfig.Name, disabledEntity)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render backend detail template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// StatsPartialHandler serves the stats partial for HTMX requests.
func (h *Handler) StatsPartialHandler(w http.ResponseWriter, r *http.Request) {
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

	stats, _, _ := collector.GetStats()
	if stats == nil {
		http.Error(w, "No stats available", http.StatusServiceUnavailable)
		return
	}

	// Get metadata for frontend/backend relationships
	metadata, _, _ := collector.GetCache().GetMetadata()

	// Get disabled entities for this server
	disabledEntities, err := h.db.GetDisabledEntities(boxID)
	if err != nil {
		h.logger.Error("Failed to get disabled entities", "error", err)
		// Continue with empty disabled list rather than failing
		disabledEntities = nil
	}

	// Check if user can manage disabled entities (for showing/hiding toggle)
	canManageDisabled := h.authManager.HasPermission(r, models.ComponentDisabledEntities, models.PermissionManage)

	// Render stats partial template
	component := pages.StatsPartial(stats, boxID, metadata, disabledEntities, canManageDisabled)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render stats partial template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// StatusSummaryPartialHandler serves just the status summary doughnuts for HTMX requests.
func (h *Handler) StatusSummaryPartialHandler(w http.ResponseWriter, r *http.Request) {
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

	stats, _, _ := collector.GetStats()
	if stats == nil {
		http.Error(w, "No stats available", http.StatusServiceUnavailable)
		return
	}

	metadata, _, _ := collector.GetCache().GetMetadata()

	// Get disabled entities for this server
	disabledEntities, err := h.db.GetDisabledEntities(boxID)
	if err != nil {
		h.logger.Error("Failed to get disabled entities", "error", err)
		disabledEntities = nil
	}

	// Render just the status summary section
	component := pages.StatusSummarySection(stats, metadata, disabledEntities)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render status summary partial template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// BackendGridPartialHandler serves just the backend grid (without status summary) for HTMX requests.
func (h *Handler) BackendGridPartialHandler(w http.ResponseWriter, r *http.Request) {
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

	stats, _, _ := collector.GetStats()
	if stats == nil {
		http.Error(w, "No stats available", http.StatusServiceUnavailable)
		return
	}

	// Get metadata for frontend/backend relationships
	metadata, _, _ := collector.GetCache().GetMetadata()

	// Get disabled entities for this server
	disabledEntities, err := h.db.GetDisabledEntities(boxID)
	if err != nil {
		h.logger.Error("Failed to get disabled entities", "error", err)
		disabledEntities = nil
	}

	// Check if user can manage disabled entities
	canManageDisabled := h.authManager.HasPermission(r, models.ComponentDisabledEntities, models.PermissionManage)

	// Render backend grid partial template (without status summary)
	component := pages.BackendGridPartial(stats, boxID, metadata, disabledEntities, canManageDisabled)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render backend grid partial template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// MetricsPartialHandler serves the metrics partial for HTMX requests.
func (h *Handler) MetricsPartialHandler(w http.ResponseWriter, r *http.Request) {
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

	metrics, _, _ := collector.GetSystemMetrics()
	if metrics == nil {
		http.Error(w, "No metrics available", http.StatusServiceUnavailable)
		return
	}

	// Render metrics partial template
	component := pages.MetricsPartial(metrics)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render metrics partial template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// AdminDisabledEntitiesPage serves the admin page for managing disabled entities.
func (h *Handler) AdminDisabledEntitiesPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Check if user has permission to manage disabled entities
	if !h.authManager.HasPermission(r, models.ComponentDisabledEntities, models.PermissionManage) {
		http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
		return
	}

	// Get enabled servers from database (dynamic, includes newly created servers)
	servers := h.getEnabledServers()

	// Get all disabled entities across all servers
	var allEntities []database.DisabledEntity
	for _, serverConfig := range servers {
		entities, err := h.db.GetDisabledEntities(serverConfig.ID)
		if err != nil {
			h.logger.Error("failed to get disabled entities for server", "server_id", serverConfig.ID, "error", err)
			continue
		}
		allEntities = append(allEntities, entities...)
	}

	// Build a map of user IDs to names for displaying who disabled each entity
	userNames := make(map[string]string)
	for _, entity := range allEntities {
		if entity.DisabledBy != nil {
			if _, exists := userNames[*entity.DisabledBy]; !exists {
				if u, err := h.db.GetUserByID(*entity.DisabledBy); err == nil && u != nil {
					userNames[*entity.DisabledBy] = u.FirstName + " " + u.LastName
				} else {
					userNames[*entity.DisabledBy] = "Unknown User"
				}
			}
		}
	}

	// Render admin disabled entities template
	component := pages.DisabledEntitiesPage(user, allEntities, servers, userNames)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render admin disabled entities template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
