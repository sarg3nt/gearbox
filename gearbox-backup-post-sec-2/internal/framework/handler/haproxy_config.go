package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/services/crypto"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// HAProxyServersPage lists all HAProxy server configurations.
func (h *Handler) HAProxyServersPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can access
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	servers, err := h.db.GetServers()
	if err != nil {
		h.logger.Error("Failed to get HAProxy servers", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	component := pages.HAProxyServersPage(user, servers)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render HAProxy servers page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HAProxyServerNewPage shows the form for creating a new server.
func (h *Handler) HAProxyServerNewPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can access
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	component := pages.HAProxyServerNewPage(user, "")
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render new server page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HAProxyServerCreatePost handles creating a new HAProxy server.
func (h *Handler) HAProxyServerCreatePost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can create
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderServerFormWithError(w, r, user, nil, false, "Failed to parse form")
		return
	}

	// Get encryptor
	encryptor, err := h.getEncryptor()
	if err != nil {
		h.renderServerFormWithError(w, r, user, nil, false, "Encryption initialization failed")
		return
	}

	// Parse form into server struct
	server := &database.ServerDB{
		ServerID:  strings.TrimSpace(r.FormValue("server_id")),
		Name:      strings.TrimSpace(r.FormValue("name")),
		Location:  strings.TrimSpace(r.FormValue("location")),
		Notes:     strings.TrimSpace(r.FormValue("notes")),
		AgentURL:  strings.TrimSpace(r.FormValue("agent_url")),
		Enabled:   r.FormValue("enabled") == "on",
		CreatedBy: &user.ID,
	}

	// Validate required fields
	if server.ServerID == "" || server.Name == "" || server.AgentURL == "" {
		h.renderServerFormWithError(w, r, user, server, false, "Required fields missing")
		return
	}

	// Encrypt API key
	apiKey := strings.TrimSpace(r.FormValue("api_key"))
	if apiKey == "" {
		h.renderServerFormWithError(w, r, user, server, false, "API key is required")
		return
	}
	server.APIKeyEncrypted, err = encryptor.EncryptString(apiKey)
	if err != nil {
		h.renderServerFormWithError(w, r, user, server, false, "Failed to encrypt API key")
		return
	}

	// Create server in database
	if err := h.db.CreateServer(server); err != nil {
		h.logger.Error("Failed to create HAProxy server", "error", err)
		h.renderServerFormWithError(w, r, user, server, false, fmt.Sprintf("Failed to create server: %v", err))
		return
	}

	// Log audit
	h.logAudit(r, user.ID, "haproxy_server_create", fmt.Sprintf("Created HAProxy server: %s (%s)", server.Name, server.ServerID))

	// Start collector if enabled
	if server.Enabled && h.registry != nil {
		// Decrypt API key for collector
		apiKeyDecrypted, _ := encryptor.DecryptString(server.APIKeyEncrypted)

		serverConfig := server.ToServerConfig(apiKeyDecrypted)
		if err := h.registry.AddCollector(serverConfig); err != nil {
			h.logger.Error("failed to start collector for server", "server_id", server.ServerID, "error", err)
		}
	}

	// Redirect to servers list page
	http.Redirect(w, r, "/settings/servers", http.StatusSeeOther)
}

// HAProxyServerEditPage shows the form for editing a server.
func (h *Handler) HAProxyServerEditPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can edit
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetServerByID(id)
	if err != nil {
		h.logger.Error("Failed to get HAProxy server", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	component := pages.HAProxyServerEditPage(user, server, "")
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render edit server page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HAProxyServerUpdatePost handles updating a server.
func (h *Handler) HAProxyServerUpdatePost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can update
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Get existing server
	server, err := h.db.GetServerByID(id)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Get encryptor
	encryptor, err := h.getEncryptor()
	if err != nil {
		h.renderServerFormWithError(w, r, user, server, true, "Encryption initialization failed")
		return
	}

	// Update fields
	server.Name = strings.TrimSpace(r.FormValue("name"))
	server.Location = strings.TrimSpace(r.FormValue("location"))
	server.Notes = strings.TrimSpace(r.FormValue("notes"))
	server.AgentURL = strings.TrimSpace(r.FormValue("agent_url"))
	server.Enabled = r.FormValue("enabled") == "on"

	// Update API key if provided
	apiKey := strings.TrimSpace(r.FormValue("api_key"))
	if apiKey != "" {
		server.APIKeyEncrypted, err = encryptor.EncryptString(apiKey)
		if err != nil {
			h.renderServerFormWithError(w, r, user, server, true, "Failed to encrypt API key")
			return
		}
	}

	// Update in database
	if err := h.db.UpdateServer(server); err != nil {
		h.logger.Error("Failed to update HAProxy server", "error", err)
		h.renderServerFormWithError(w, r, user, server, true, fmt.Sprintf("Failed to update server: %v", err))
		return
	}

	// Log audit
	h.logAudit(r, user.ID, "haproxy_server_update", fmt.Sprintf("Updated HAProxy server: %s (%s)", server.Name, server.ServerID))

	// Reload collector if enabled
	if h.registry != nil {
		// Decrypt API key for collector
		apiKeyDecrypted, _ := encryptor.DecryptString(server.APIKeyEncrypted)

		serverConfig := server.ToServerConfig(apiKeyDecrypted)

		if server.Enabled {
			if err := h.registry.ReloadCollector(serverConfig); err != nil {
				h.logger.Error("failed to reload collector for server", "server_id", server.ServerID, "error", err)
			}
		} else {
			if err := h.registry.RemoveCollector(server.ServerID); err != nil {
				h.logger.Error("failed to stop collector for server", "server_id", server.ServerID, "error", err)
			}
		}
	}

	// Redirect to list
	http.Redirect(w, r, "/settings/servers", http.StatusSeeOther)
}

// HAProxyServerDeletePost handles deleting a server.
func (h *Handler) HAProxyServerDeletePost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can delete
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	// Get server for logging
	server, err := h.db.GetServerByID(id)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Stop collector if running
	if h.registry != nil {
		if err := h.registry.RemoveCollector(server.ServerID); err != nil {
			h.logger.Error("failed to stop collector for server", "server_id", server.ServerID, "error", err)
		}
	}

	// Delete from database
	if err := h.db.DeleteServer(id); err != nil {
		h.logger.Error("Failed to delete HAProxy server", "error", err)
		http.Error(w, "Failed to delete server", http.StatusInternalServerError)
		return
	}

	// Log audit
	h.logAudit(r, user.ID, "haproxy_server_delete", fmt.Sprintf("Deleted HAProxy server: %s (%s)", server.Name, server.ServerID))

	// Redirect to list
	http.Redirect(w, r, "/settings/servers", http.StatusSeeOther)
}

// HAProxyServerTogglePost handles enabling/disabling a server.
func (h *Handler) HAProxyServerTogglePost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can toggle
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	// Get current server
	server, err := h.db.GetServerByID(id)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Toggle enabled status
	newEnabled := !server.Enabled
	if err := h.db.SetServerEnabled(id, newEnabled); err != nil {
		h.logger.Error("Failed to toggle HAProxy server", "error", err)
		http.Error(w, "Failed to toggle server", http.StatusInternalServerError)
		return
	}

	// Log audit
	action := "disabled"
	if newEnabled {
		action = "enabled"
	}
	h.logAudit(r, user.ID, "haproxy_server_toggle", fmt.Sprintf("%s HAProxy server: %s (%s)", action, server.Name, server.ServerID))

	// Start or stop collector
	if h.registry != nil {
		if newEnabled {
			// Get encryptor and decrypt API key
			encryptor, err := h.getEncryptor()
			if err == nil {
				apiKeyDecrypted, _ := encryptor.DecryptString(server.APIKeyEncrypted)

				serverConfig := server.ToServerConfig(apiKeyDecrypted)
				if err := h.registry.AddCollector(serverConfig); err != nil {
					h.logger.Error("failed to start collector for server", "server_id", server.ServerID, "error", err)
				}
			}
		} else {
			if err := h.registry.RemoveCollector(server.ServerID); err != nil {
				h.logger.Error("failed to stop collector for server", "server_id", server.ServerID, "error", err)
			}
		}
	}

	// Redirect to list
	http.Redirect(w, r, "/settings/servers", http.StatusSeeOther)
}

// HAProxyServerTestConnectionPost tests the connection to a server using the Agent API.
func (h *Handler) HAProxyServerTestConnectionPost(w http.ResponseWriter, r *http.Request) {
	// Only admins can test
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to parse form",
		})
		return
	}

	// Get form values
	serverIDStr := r.FormValue("server_id")
	agentURL := strings.TrimSpace(r.FormValue("agent_url"))
	apiKey := strings.TrimSpace(r.FormValue("api_key"))

	// Validate required fields
	if agentURL == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Agent URL is required",
		})
		return
	}

	// If editing an existing server and API key is empty, load from database
	if serverIDStr != "" && apiKey == "" {
		h.logger.Debug("test connection for existing server ID", "server_id", serverIDStr)
		existingServer, err := h.db.GetServerByServerID(serverIDStr)
		if err != nil {
			h.logger.Error("ERROR: Failed to get existing server", "error", err)
		} else if existingServer == nil {
			h.logger.Info("WARNING: Existing server not found in database")
		} else {
			h.logger.Debug("found existing server", "name", existingServer.Name)
			encryptor, err := h.getEncryptor()
			if err != nil {
				h.logger.Error("ERROR: Failed to get encryptor", "error", err)
			} else if len(existingServer.APIKeyEncrypted) > 0 {
				decrypted, err := encryptor.Decrypt(existingServer.APIKeyEncrypted)
				if err != nil {
					h.logger.Error("ERROR: Failed to decrypt API key", "error", err)
				} else {
					apiKey = string(decrypted)
					h.logger.Info("Successfully loaded existing API key")
				}
			}
		}
	}

	// Validate API key is available
	if apiKey == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "API key is required",
		})
		return
	}

	h.logger.Debug("test connection", "agent_url", agentURL)

	// Create Agent client and test connection
	client := agent.NewClient(agentURL, apiKey)

	// Step 1: Test health endpoint (no auth required)
	_, err := client.Health()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Health check failed: %v", err),
		})
		return
	}

	// Step 2: Test authenticated endpoint
	_, err = client.GetInfo()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if agent.IsUnauthorized(err) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": "Authentication failed: Invalid API key",
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"message": fmt.Sprintf("API request failed: %v", err),
			})
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Connection test successful! Agent API is reachable and authenticated.",
	})
}

// Helper methods

func (h *Handler) renderServerFormWithError(w http.ResponseWriter, r *http.Request, user *models.User, server *database.ServerDB, isEdit bool, errorMsg string) {
	if isEdit {
		component := pages.HAProxyServerEditPage(user, server, errorMsg)
		if err := component.Render(r.Context(), w); err != nil {
			h.logger.Error("Failed to render form", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	} else {
		component := pages.HAProxyServerNewPage(user, errorMsg)
		if err := component.Render(r.Context(), w); err != nil {
			h.logger.Error("Failed to render form", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}

func (h *Handler) getEncryptor() (*crypto.Encryptor, error) {
	// Get session secret from config (stored in handler or passed separately)
	// For now, we'll need to add this to handler struct
	if h.encryptor == nil {
		return nil, fmt.Errorf("encryptor not initialized")
	}
	return h.encryptor, nil
}

func (h *Handler) logAudit(r *http.Request, userID string, action, details string) {
	ipAddress := r.RemoteAddr
	userAgent := r.UserAgent()

	auditLog := &models.AuditLog{
		UserID:    &userID,
		Action:    action,
		Details:   details,
		IPAddress: ipAddress,
		UserAgent: userAgent,
	}

	if err := h.db.CreateAuditLog(auditLog); err != nil {
		h.logger.Error("Failed to create audit log", "error", err)
	}
}

// HAProxyServerLogSettingsPage shows the log source settings for a server.
func (h *Handler) HAProxyServerLogSettingsPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can access
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetServerByID(id)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Get available log sources from the agent
	var agentSources []agent.LogSource
	if server.Enabled && server.UsesAgentAPI() {
		encryptor, err := h.getEncryptor()
		if err == nil {
			apiKey, _ := encryptor.DecryptString(server.APIKeyEncrypted)
			client := agent.NewClient(server.AgentURL, apiKey)
			sourcesResp, err := client.GetLogSources()
			if err == nil && sourcesResp != nil {
				agentSources = sourcesResp.Sources
			} else {
				h.logger.Error("Failed to get log sources from agent", "error", err)
			}
		}
	}

	// Get enabled settings from database
	enabledSources, err := h.db.GetEnabledLogSources(id)
	if err != nil {
		h.logger.Error("Failed to get enabled log sources", "error", err)
	}

	// Build the combined list
	sourceInfoList := pages.BuildLogSourceInfoList(agentSources, enabledSources)

	// Check for flash messages
	successMsg := ""
	errorMsg := ""

	component := pages.LogSettingsPage(user, server, sourceInfoList, errorMsg, successMsg)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render log settings page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HAProxyServerLogSettingsPost saves the log source settings for a server.
func (h *Handler) HAProxyServerLogSettingsPost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can access
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetServerByID(id)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Parse form
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Get selected log sources
	selectedSources := r.Form["log_sources"]

	// Get available log sources from the agent for display names
	displayNames := make(map[string]string)
	if server.Enabled && server.UsesAgentAPI() {
		encryptor, err := h.getEncryptor()
		if err == nil {
			apiKey, _ := encryptor.DecryptString(server.APIKeyEncrypted)
			client := agent.NewClient(server.AgentURL, apiKey)
			sourcesResp, err := client.GetLogSources()
			if err == nil && sourcesResp != nil {
				for _, src := range sourcesResp.Sources {
					displayNames[src.Name] = pages.FormatLogSourceName(src.Name)
				}
			}
		}
	}

	// Build settings to save
	var settings []database.LogSourceSetting
	for _, name := range selectedSources {
		displayName := displayNames[name]
		if displayName == "" {
			displayName = name
		}
		settings = append(settings, database.LogSourceSetting{
			HAProxyID:   id,
			LogName:     name,
			DisplayName: displayName,
		})
	}

	// Save to database
	if err := h.db.SetEnabledLogSources(id, settings); err != nil {
		h.logger.Error("Failed to save log sources", "error", err)
		h.renderLogSettingsWithError(w, r, user, server, "Failed to save settings: "+err.Error())
		return
	}

	// Log audit
	h.logAudit(r, user.ID, "log_settings_update", fmt.Sprintf("Updated log settings for server %s: %d sources enabled", server.Name, len(settings)))

	// Redirect back to server list
	http.Redirect(w, r, "/settings/servers", http.StatusSeeOther)
}

func (h *Handler) renderLogSettingsWithError(w http.ResponseWriter, r *http.Request, user *models.User, server *database.ServerDB, errorMsg string) {
	// Get available log sources from the agent
	var agentSources []agent.LogSource
	if server.Enabled && server.UsesAgentAPI() {
		encryptor, err := h.getEncryptor()
		if err == nil {
			apiKey, _ := encryptor.DecryptString(server.APIKeyEncrypted)
			client := agent.NewClient(server.AgentURL, apiKey)
			sourcesResp, err := client.GetLogSources()
			if err == nil && sourcesResp != nil {
				agentSources = sourcesResp.Sources
			}
		}
	}

	enabledSources, _ := h.db.GetEnabledLogSources(server.ID)
	sourceInfoList := pages.BuildLogSourceInfoList(agentSources, enabledSources)

	component := pages.LogSettingsPage(user, server, sourceInfoList, errorMsg, "")
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render log settings page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
