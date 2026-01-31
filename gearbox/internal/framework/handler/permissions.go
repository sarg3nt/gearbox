package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// AdminPermissionsPage serves the permissions management page.
func (h *Handler) AdminPermissionsPage(w http.ResponseWriter, r *http.Request) {
	currentUser, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Only admins and users with user approval permission can manage permissions
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !currentUser.IsAdmin() && !perms.CanApproveUsers() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")
	successMessage := r.URL.Query().Get("success")

	// Get all users
	users, _ := h.authManager.GetDB().GetAllUsers()

	// Get permissions for all users
	allPerms, _ := h.authManager.GetDB().GetAllUserPermissions()

	// Get permission templates
	templates := models.GetPermissionTemplates()

	component := pages.AdminPermissions(currentUser, users, allPerms, templates, errorMessage, successMessage, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render permissions template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// AdminUserPermissionsPage serves the user-specific permissions edit page.
func (h *Handler) AdminUserPermissionsPage(w http.ResponseWriter, r *http.Request) {
	currentUser, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Only admins and users with user approval permission can manage permissions
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !currentUser.IsAdmin() && !perms.CanApproveUsers() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "userID")
	userID := userIDStr
	if userID == "" {
		http.Redirect(w, r, "/settings/permissions?error=Invalid+user+ID", http.StatusSeeOther)
		return
	}

	targetUser, err := h.authManager.GetDB().GetUserByID(userID)
	if err != nil || targetUser == nil {
		http.Redirect(w, r, "/settings/permissions?error=User+not+found", http.StatusSeeOther)
		return
	}

	// Get user's current permissions
	userPerms, _ := h.authManager.GetDB().GetUserPermissions(userID)

	csrfToken, _ := h.authManager.GetCSRFToken(r)
	errorMessage := r.URL.Query().Get("error")
	successMessage := r.URL.Query().Get("success")

	// Get all components and available permissions
	components := models.AllComponents()
	templates := models.GetPermissionTemplates()

	component := pages.AdminUserPermissions(currentUser, targetUser, userPerms, components, templates, errorMessage, successMessage, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render user permissions template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIUpdateUserPermissions handles updating a user's permissions.
func (h *Handler) APIUpdateUserPermissions(w http.ResponseWriter, r *http.Request) {
	currentUser, err := h.authManager.GetUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Only admins and users with user approval permission can manage permissions
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !currentUser.IsAdmin() && !perms.CanApproveUsers() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "userID")
	userID := userIDStr
	if userID == "" {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		Permissions map[string][]string `json:"permissions"` // component -> []permission
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Convert to proper types
	permissions := make(map[models.Component][]models.Permission)
	for compStr, permStrs := range req.Permissions {
		comp := models.Component(compStr)
		perms := make([]models.Permission, 0, len(permStrs))
		for _, permStr := range permStrs {
			perms = append(perms, models.Permission(permStr))
		}
		permissions[comp] = perms
	}

	// Update permissions
	if err := h.authManager.GetDB().SetUserPermissions(userID, permissions, currentUser.ID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to update permissions: %v", err), http.StatusInternalServerError)
		return
	}

	// Get updated user for response
	targetUser, _ := h.authManager.GetDB().GetUserByID(userID)

	h.authManager.LogAudit(r, &currentUser.ID, "permissions_updated",
		fmt.Sprintf("updated permissions for user: %s", targetUser.Email))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Permissions updated successfully",
	})
}

// APIApplyPermissionTemplate applies a permission template to a user.
func (h *Handler) APIApplyPermissionTemplate(w http.ResponseWriter, r *http.Request) {
	currentUser, err := h.authManager.GetUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Only admins and users with user approval permission can manage permissions
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !currentUser.IsAdmin() && !perms.CanApproveUsers() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	userIDStr := chi.URLParam(r, "userID")
	userID := userIDStr
	if userID == "" {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		TemplateName string `json:"template_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Apply template
	if err := h.authManager.GetDB().CopyPermissionsFromTemplate(userID, req.TemplateName, currentUser.ID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to apply template: %v", err), http.StatusInternalServerError)
		return
	}

	// Get updated user for response
	targetUser, _ := h.authManager.GetDB().GetUserByID(userID)

	h.authManager.LogAudit(r, &currentUser.ID, "permissions_template_applied",
		fmt.Sprintf("applied template '%s' to user: %s", req.TemplateName, targetUser.Email))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Template '%s' applied successfully", req.TemplateName),
	})
}

// APIGetUserPermissions returns a user's current permissions.
func (h *Handler) APIGetUserPermissions(w http.ResponseWriter, r *http.Request) {
	currentUser, err := h.authManager.GetUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userIDStr := chi.URLParam(r, "userID")
	userID := userIDStr
	if userID == "" {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Users can view their own permissions, admins can view anyone's
	if userID != currentUser.ID && !currentUser.IsAdmin() {
		// Check if user has permission to manage users
		perms, err := h.authManager.GetUserPermissions(r)
		if err != nil || !perms.CanApproveUsers() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// Get user permissions
	userPerms, err := h.authManager.GetDB().GetUserPermissions(userID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get permissions: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userPerms)
}

// APIGetCurrentUserPermissions returns the current user's permissions.
func (h *Handler) APIGetCurrentUserPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(perms)
}
