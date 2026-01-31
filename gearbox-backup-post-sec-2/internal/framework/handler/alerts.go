package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// AlertsPage serves the alerts management page.
func (h *Handler) AlertsPage(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view alerts
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view alerts", http.StatusForbidden)
		return
	}

	user, _ := auth.GetUserFromContext(r.Context())

	servers := h.getEnabledServers()
	if len(servers) == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Get user permissions for alert actions
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		h.logger.Error("Failed to get user permissions", "error", err)
		perms = &models.UserPermissions{}
	}

	// Render alerts template with current enabled servers and permissions
	component := pages.AlertsPage(user, servers, perms)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render alerts template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIAlertsHandler returns alerts for a server.
func (h *Handler) APIAlertsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view alerts", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	status := r.URL.Query().Get("status")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	var alerts []models.Alert
	var err error

	switch status {
	case "active":
		alerts, err = h.db.GetActiveAlerts(serverID)
	case "acknowledged":
		alerts, err = h.db.GetAlertsByStatusPublic(serverID, "acknowledged")
	case "resolved":
		alerts, err = h.db.GetAlertsByStatusPublic(serverID, "resolved")
	default:
		alerts, err = h.db.GetRecentAlerts(serverID, limit)
	}

	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get alerts", err))
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"alerts": alerts,
		"count":  len(alerts),
	})
}

// APIAlertSummaryHandler returns alert summary statistics.
func (h *Handler) APIAlertSummaryHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view alerts", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	summary, err := h.db.GetAlertSummary(serverID)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get alert summary", err))
		return
	}

	h.writeJSON(w, summary)
}

// APIAlertRulesHandler returns alert rules for a server.
func (h *Handler) APIAlertRulesHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view alerts", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	rules, err := h.db.GetAlertRules(serverID)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get alert rules", err))
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"rules": rules,
		"count": len(rules),
	})
}

// APICreateAlertRuleHandler creates a new alert rule.
func (h *Handler) APICreateAlertRuleHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionConfigure) {
		http.Error(w, "Forbidden: insufficient permissions to configure alerts", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	var rule models.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	rule.ServerID = serverID
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	id, err := h.db.CreateAlertRule(&rule)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("create alert rule", err))
		return
	}
	rule.ID = id

	h.writeJSON(w, map[string]interface{}{
		"success": true,
		"rule":    rule,
	})
}

// APIUpdateAlertRuleHandler updates an existing alert rule.
func (h *Handler) APIUpdateAlertRuleHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionConfigure) {
		http.Error(w, "Forbidden: insufficient permissions to configure alerts", http.StatusForbidden)
		return
	}

	ruleIDStr := chi.URLParam(r, "ruleID")
	if ruleIDStr == "" {
		http.Error(w, "Rule ID required", http.StatusBadRequest)
		return
	}

	ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid rule ID", http.StatusBadRequest)
		return
	}

	var rule models.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	rule.ID = ruleID
	rule.UpdatedAt = time.Now()

	if err := h.db.UpdateAlertRule(&rule); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("update alert rule", err))
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"success": true,
		"rule":    rule,
	})
}

// APIDeleteAlertRuleHandler deletes an alert rule.
func (h *Handler) APIDeleteAlertRuleHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionConfigure) {
		http.Error(w, "Forbidden: insufficient permissions to configure alerts", http.StatusForbidden)
		return
	}

	ruleIDStr := chi.URLParam(r, "ruleID")
	if ruleIDStr == "" {
		http.Error(w, "Rule ID required", http.StatusBadRequest)
		return
	}

	ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid rule ID", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteAlertRule(ruleID); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("delete alert rule", err))
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"success": true,
	})
}

// APIAcknowledgeAlertHandler acknowledges an alert.
func (h *Handler) APIAcknowledgeAlertHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionAction) {
		http.Error(w, "Forbidden: insufficient permissions to acknowledge alerts", http.StatusForbidden)
		return
	}

	alertIDStr := chi.URLParam(r, "alertID")
	if alertIDStr == "" {
		http.Error(w, "Alert ID required", http.StatusBadRequest)
		return
	}

	alertID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid alert ID", http.StatusBadRequest)
		return
	}

	user, _ := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	if err := h.db.AcknowledgeAlert(alertID, user.ID); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("acknowledge alert", err))
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"success": true,
	})
}

// APIResolveAlertHandler resolves an alert.
func (h *Handler) APIResolveAlertHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionAction) {
		http.Error(w, "Forbidden: insufficient permissions to resolve alerts", http.StatusForbidden)
		return
	}

	alertIDStr := chi.URLParam(r, "alertID")
	if alertIDStr == "" {
		http.Error(w, "Alert ID required", http.StatusBadRequest)
		return
	}

	alertID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid alert ID", http.StatusBadRequest)
		return
	}

	if err := h.db.ResolveAlert(alertID); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("resolve alert", err))
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"success": true,
	})
}

// APISilenceAlertHandler silences an alert.
func (h *Handler) APISilenceAlertHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionAction) {
		http.Error(w, "Forbidden: insufficient permissions to silence alerts", http.StatusForbidden)
		return
	}

	alertIDStr := chi.URLParam(r, "alertID")
	if alertIDStr == "" {
		http.Error(w, "Alert ID required", http.StatusBadRequest)
		return
	}

	alertID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid alert ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Duration int `json:"duration"` // Duration in minutes
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Duration <= 0 {
		req.Duration = 60 // Default to 1 hour
	}

	until := time.Now().Add(time.Duration(req.Duration) * time.Minute)
	if err := h.db.SilenceAlert(alertID, until); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("silence alert", err))
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"success":        true,
		"silenced_until": until,
	})
}

// APIGlobalAlertCountHandler returns the global active alert count across all servers.
func (h *Handler) APIGlobalAlertCountHandler(w http.ResponseWriter, r *http.Request) {
	// Allow any authenticated user to see alert counts
	totalActive, criticalCount, err := h.db.GetGlobalActiveAlertCount()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get alert count", err))
		return
	}

	h.writeJSON(w, map[string]any{
		"total_active":   totalActive,
		"critical_count": criticalCount,
	})
}

// APIAcknowledgeAlertWithNoteHandler acknowledges an alert with an optional note.
func (h *Handler) APIAcknowledgeAlertWithNoteHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionAction) {
		http.Error(w, "Forbidden: insufficient permissions to acknowledge alerts", http.StatusForbidden)
		return
	}

	alertIDStr := chi.URLParam(r, "alertID")
	if alertIDStr == "" {
		http.Error(w, "Alert ID required", http.StatusBadRequest)
		return
	}

	alertID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid alert ID", http.StatusBadRequest)
		return
	}

	user, _ := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	var req struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If no body, that's fine - note is optional
		req.Note = ""
	}

	if err := h.db.AcknowledgeAlertWithNote(alertID, user.ID, req.Note); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("acknowledge alert", err))
		return
	}

	// Publish event if event hub is available
	if h.eventHub != nil {
		alert, _ := h.db.GetAlert(alertID)
		if alert != nil {
			h.eventHub.PublishAlertAcknowledged(alert.ServerID, alertID, alert.Title)
			// Update global count
			total, critical, _ := h.db.GetGlobalActiveAlertCount()
			h.eventHub.PublishAlertCountUpdated(total, critical)
		}
	}

	h.writeJSON(w, map[string]any{
		"success": true,
	})
}

// APIResolveAlertWithNoteHandler resolves an alert with an optional note.
func (h *Handler) APIResolveAlertWithNoteHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionAction) {
		http.Error(w, "Forbidden: insufficient permissions to resolve alerts", http.StatusForbidden)
		return
	}

	alertIDStr := chi.URLParam(r, "alertID")
	if alertIDStr == "" {
		http.Error(w, "Alert ID required", http.StatusBadRequest)
		return
	}

	alertID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid alert ID", http.StatusBadRequest)
		return
	}

	user, _ := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	var req struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If no body, that's fine - note is optional
		req.Note = ""
	}

	// Get alert before resolving for event publishing
	var serverID, title string
	alert, _ := h.db.GetAlert(alertID)
	if alert != nil {
		serverID = alert.ServerID
		title = alert.Title
	}

	if err := h.db.ResolveAlertWithNote(alertID, user.ID, req.Note); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("resolve alert", err))
		return
	}

	// Publish event if event hub is available
	if h.eventHub != nil && serverID != "" {
		h.eventHub.PublishAlertResolved(serverID, alertID, title)
		// Update global count
		total, critical, _ := h.db.GetGlobalActiveAlertCount()
		h.eventHub.PublishAlertCountUpdated(total, critical)
	}

	h.writeJSON(w, map[string]any{
		"success": true,
	})
}

// APIAlertNotesHandler returns notes for an alert.
func (h *Handler) APIAlertNotesHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view alerts", http.StatusForbidden)
		return
	}

	alertIDStr := chi.URLParam(r, "alertID")
	if alertIDStr == "" {
		http.Error(w, "Alert ID required", http.StatusBadRequest)
		return
	}

	alertID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid alert ID", http.StatusBadRequest)
		return
	}

	notes, err := h.db.GetAlertNotes(alertID)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get alert notes", err))
		return
	}

	h.writeJSON(w, map[string]any{
		"notes": notes,
		"count": len(notes),
	})
}

// APIAddAlertNoteHandler adds a note to an alert.
func (h *Handler) APIAddAlertNoteHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionAction) {
		http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
		return
	}

	alertIDStr := chi.URLParam(r, "alertID")
	if alertIDStr == "" {
		http.Error(w, "Alert ID required", http.StatusBadRequest)
		return
	}

	alertID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid alert ID", http.StatusBadRequest)
		return
	}

	user, _ := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	var req struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Note == "" {
		http.Error(w, "Note is required", http.StatusBadRequest)
		return
	}

	if err := h.db.AddAlertNote(alertID, user.ID, "comment", req.Note); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("add note", err))
		return
	}

	h.writeJSON(w, map[string]any{
		"success": true,
	})
}

// APIAlertRetentionConfigHandler returns the alert retention config for a server.
func (h *Handler) APIAlertRetentionConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	config, err := h.db.GetAlertRetentionConfig(serverID)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get retention config", err))
		return
	}

	h.writeJSON(w, config)
}

// APIUpdateAlertRetentionConfigHandler updates the alert retention config for a server.
func (h *Handler) APIUpdateAlertRetentionConfigHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionConfigure) {
		http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var config struct {
		RetentionDays    int `json:"retention_days"`
		AutoResolveHours int `json:"auto_resolve_hours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate
	if config.RetentionDays < 1 {
		config.RetentionDays = 30
	}
	if config.AutoResolveHours < 0 {
		config.AutoResolveHours = 0
	}

	var userID *string
	if user != nil {
		userID = &user.ID
	}

	retConfig := &database.AlertRetentionConfig{
		ServerID:         serverID,
		RetentionDays:    config.RetentionDays,
		AutoResolveHours: config.AutoResolveHours,
	}

	if err := h.db.SetAlertRetentionConfig(retConfig, userID); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("update retention config", err))
		return
	}

	h.writeJSON(w, map[string]any{
		"success": true,
		"config":  retConfig,
	})
}

// AlertRulesPage serves the alert rules configuration page.
func (h *Handler) AlertRulesPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Check if user has permission to configure alerts
	if !h.authManager.HasPermission(r, models.ComponentAlerts, models.PermissionConfigure) {
		http.Error(w, "Forbidden: insufficient permissions to configure alerts", http.StatusForbidden)
		return
	}

	// Get enabled servers from database
	servers := h.getEnabledServers()

	// Get server ID from query param, default to first server
	serverID := r.URL.Query().Get("server")
	if serverID == "" && len(servers) > 0 {
		serverID = servers[0].ID
	}

	if serverID == "" {
		http.Error(w, "No servers configured", http.StatusBadRequest)
		return
	}

	// Get server name for display
	var serverName string
	if serverConfig, exists := h.getServerConfig(serverID); exists {
		serverName = serverConfig.Name
	}

	// Get success/error messages from query params
	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")

	component := pages.AlertRulesPage(user, serverID, serverName, servers, successMsg, errorMsg)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render alert rules template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
