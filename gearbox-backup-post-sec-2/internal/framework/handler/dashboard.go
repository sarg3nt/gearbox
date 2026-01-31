package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/dashboard"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// DashboardHandler handles dashboard-related HTTP requests.
type DashboardHandler struct {
	storage            *dashboard.Storage
	renderer           *dashboard.Renderer
	widgetRegistry     *widget.Registry
	dataSourceRegistry *widget.DataSourceRegistry
	logger             *slog.Logger
	serverID           string
	userID             string
}

// NewDashboardHandler creates a new dashboard handler.
func NewDashboardHandler(
	storage *dashboard.Storage,
	widgetRegistry *widget.Registry,
	dataSourceRegistry *widget.DataSourceRegistry,
	logger *slog.Logger,
	serverID string,
	userID string,
) *DashboardHandler {
	if logger == nil {
		logger = slog.Default()
	}

	renderer := dashboard.NewRenderer(widgetRegistry, dataSourceRegistry, logger)

	return &DashboardHandler{
		storage:            storage,
		renderer:           renderer,
		widgetRegistry:     widgetRegistry,
		dataSourceRegistry: dataSourceRegistry,
		logger:             logger,
		serverID:           serverID,
		userID:             userID,
	}
}

// RegisterRoutes registers dashboard routes.
func (h *DashboardHandler) RegisterRoutes(r chi.Router) {
	// Settings route
	r.Get("/settings/dashboards", h.DashboardManagerPage)

	// Dashboard routes
	r.Get("/dashboards", h.ListDashboards)
	r.Get("/dashboards/create", h.CreateDashboardPage)
	r.Get("/dashboards/{slug}", h.ViewDashboard)
	r.Get("/dashboards/{slug}/edit", h.EditDashboardPage)

	// API routes
	r.Post("/api/dashboards", h.CreateDashboard)
	r.Delete("/api/dashboards/{slug}", h.DeleteDashboard)
	r.Put("/api/dashboards/{slug}", h.UpdateDashboard)
	r.Post("/api/dashboards/order", h.SaveDashboardOrder)
	r.Post("/api/dashboards/{slug}/toggle", h.TogglePluginDashboard)
}

// ListDashboards handles GET /dashboards
func (h *DashboardHandler) ListDashboards(w http.ResponseWriter, r *http.Request) {
	dashboards, err := h.storage.List()
	if err != nil {
		h.logger.Error("failed to list dashboards", "error", err)
		http.Error(w, "Failed to load dashboards", http.StatusInternalServerError)
		return
	}

	// Get user from context
	user, _ := auth.GetUserFromContext(r.Context())

	// Render dashboard list page
	component := pages.DashboardListPage(dashboards, user, r.URL.Path)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render dashboard list", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
}

// CreateDashboardPage handles GET /dashboards/create
func (h *DashboardHandler) CreateDashboardPage(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, _ := auth.GetUserFromContext(r.Context())

	component := pages.DashboardCreatePage(user, r.URL.Path)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render create dashboard page", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
}

// ViewDashboard handles GET /dashboards/{slug}
func (h *DashboardHandler) ViewDashboard(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	// Load dashboard
	dash, err := h.storage.Load(slug)
	if err != nil {
		h.logger.Error("failed to load dashboard", "slug", slug, "error", err)
		http.Error(w, "Dashboard not found", http.StatusNotFound)
		return
	}

	// Render dashboard
	content, err := h.renderer.Render(r.Context(), dash, h.serverID, h.userID)
	if err != nil {
		h.logger.Error("failed to render dashboard", "slug", slug, "error", err)
		http.Error(w, "Failed to render dashboard", http.StatusInternalServerError)
		return
	}

	// Get user from context
	user, _ := auth.GetUserFromContext(r.Context())

	// Wrap in page layout
	component := pages.DashboardPage(dash, content, user, r.URL.Path)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render dashboard page", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
}

// EditDashboardPage handles GET /dashboards/{slug}/edit
func (h *DashboardHandler) EditDashboardPage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	// Load dashboard
	dash, err := h.storage.Load(slug)
	if err != nil {
		h.logger.Error("failed to load dashboard", "slug", slug, "error", err)
		http.Error(w, "Dashboard not found", http.StatusNotFound)
		return
	}

	// Check if editable
	if !dash.Editable {
		http.Error(w, "This dashboard cannot be edited", http.StatusForbidden)
		return
	}

	// Render dashboard content (same as view mode)
	content, err := h.renderer.Render(r.Context(), dash, h.serverID, h.userID)
	if err != nil {
		h.logger.Error("failed to render dashboard", "slug", slug, "error", err)
		http.Error(w, "Failed to render dashboard", http.StatusInternalServerError)
		return
	}

	// Get available widgets
	widgets := h.widgetRegistry.List()

	// Get user from context
	user, _ := auth.GetUserFromContext(r.Context())

	// Render editor with live content
	component := pages.DashboardEditorPage(dash, content, widgets, user, r.URL.Path)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render dashboard editor", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}
}

// CreateDashboard handles POST /api/dashboards
func (h *DashboardHandler) CreateDashboard(w http.ResponseWriter, r *http.Request) {
	// Parse form data
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	description := r.FormValue("description")

	if name == "" {
		http.Error(w, "Dashboard name is required", http.StatusBadRequest)
		return
	}

	// Create dashboard
	dash := &dashboard.Dashboard{
		Version:     "1.0",
		Name:        name,
		Description: description,
		CreatedBy:   "user",
		PluginName:  nil,
		Editable:    true,
		Layout: dashboard.Layout{
			Columns: 12,
			Gap:     4,
		},
		Widgets: []dashboard.Widget{},
	}

	// Save dashboard
	if err := h.storage.Save(dash); err != nil {
		h.logger.Error("failed to save dashboard", "error", err)
		http.Error(w, "Failed to create dashboard", http.StatusInternalServerError)
		return
	}

	// Redirect to dashboard list
	http.Redirect(w, r, "/dashboards", http.StatusSeeOther)
}

// DeleteDashboard handles DELETE /api/dashboards/{slug}
func (h *DashboardHandler) DeleteDashboard(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	// Load dashboard first to check if it's editable
	dash, err := h.storage.Load(slug)
	if err != nil {
		h.logger.Error("failed to load dashboard", "slug", slug, "error", err)
		http.Error(w, "Dashboard not found", http.StatusNotFound)
		return
	}

	// Check if editable
	if !dash.Editable {
		http.Error(w, "This dashboard cannot be deleted", http.StatusForbidden)
		return
	}

	// Delete dashboard
	if err := h.storage.Delete(slug); err != nil {
		h.logger.Error("failed to delete dashboard", "slug", slug, "error", err)
		http.Error(w, "Failed to delete dashboard", http.StatusInternalServerError)
		return
	}

	// Return success
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
	})
}

// UpdateDashboard handles PUT /api/dashboards/{slug}
func (h *DashboardHandler) UpdateDashboard(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	// Load dashboard
	dash, err := h.storage.Load(slug)
	if err != nil {
		h.logger.Error("failed to load dashboard", "slug", slug, "error", err)
		http.Error(w, "Dashboard not found", http.StatusNotFound)
		return
	}

	// Check if editable
	if !dash.Editable {
		http.Error(w, "This dashboard cannot be edited", http.StatusForbidden)
		return
	}

	// Parse JSON body
	var updateData struct {
		Name        string               `json:"name"`
		Description string               `json:"description"`
		Widgets     []dashboard.Widget   `json:"widgets"`
		Layout      *dashboard.Layout    `json:"layout"`
	}

	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		http.Error(w, "Invalid JSON data", http.StatusBadRequest)
		return
	}

	// Update dashboard
	if updateData.Name != "" {
		dash.Name = updateData.Name
	}
	if updateData.Description != "" {
		dash.Description = updateData.Description
	}
	if updateData.Widgets != nil {
		dash.Widgets = updateData.Widgets
	}
	if updateData.Layout != nil {
		dash.Layout = *updateData.Layout
	}

	// Save dashboard
	if err := h.storage.Save(dash); err != nil {
		h.logger.Error("failed to update dashboard", "slug", slug, "error", err)
		http.Error(w, "Failed to update dashboard", http.StatusInternalServerError)
		return
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"slug":   dash.Slug,
	})
}
