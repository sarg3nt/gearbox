package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/dashboard"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// DashboardManagerPage handles GET /settings/dashboards
func (h *DashboardHandler) DashboardManagerPage(w http.ResponseWriter, r *http.Request) {
	// Get all dashboards
	allDashboards, err := h.storage.List()
	if err != nil {
		h.logger.Error("failed to list dashboards", "error", err)
		http.Error(w, "Failed to load dashboards", http.StatusInternalServerError)
		return
	}

	// Separate user and plugin dashboards
	var userDashboards []dashboard.DashboardMetadata
	var pluginDashboards []dashboard.PluginDashboardInfo

	for _, dash := range allDashboards {
		if dash.PluginName != nil {
			// Plugin dashboard
			pluginDashboards = append(pluginDashboards, dashboard.PluginDashboardInfo{
				Name:        dash.Name,
				Description: dash.Description,
				PluginName:  *dash.PluginName,
				Slug:        dash.Slug,
				Enabled:     true, // TODO: Check actual enabled state
				Icon:        "",   // TODO: Get from plugin
			})
		} else {
			// User dashboard
			userDashboards = append(userDashboards, dash)
		}
	}

	// Get user from context
	user, _ := auth.GetUserFromContext(r.Context())

	// Render manager page
	data := dashboard.DashboardManagerData{
		UserDashboards:   userDashboards,
		PluginDashboards: pluginDashboards,
	}

	component := pages.DashboardManagerPage(data, user, r.URL.Path)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render dashboard manager", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
}

// SaveDashboardOrder handles POST /api/dashboards/order
func (h *DashboardHandler) SaveDashboardOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Order []string `json:"order"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// TODO: Save dashboard order to database or config file
	h.logger.Info("dashboard order saved", "order", req.Order)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{ //#nosec G104
		"status": "success",
	})
}

// TogglePluginDashboard handles POST /api/dashboards/{slug}/toggle
func (h *DashboardHandler) TogglePluginDashboard(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// TODO: Save enabled state to database
	h.logger.Info("plugin dashboard toggled", "slug", slug, "enabled", req.Enabled)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{ //#nosec G104
		"status": "success",
	})
}
