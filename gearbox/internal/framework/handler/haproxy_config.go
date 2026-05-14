package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

// HAProxyBoxesPage lists all HAProxy server configurations.
func (h *Handler) HAProxyBoxesPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can access
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	servers, err := h.db.GetBoxes()
	if err != nil {
		h.logger.Error("Failed to get HAProxy servers", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	component := pages.HAProxyBoxesPage(user, servers)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render HAProxy servers page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HAProxyBoxNewPage shows the form for creating a new server.
func (h *Handler) HAProxyBoxNewPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can access
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Fetch existing servers to determine if we should show sidebar
	servers, err := h.db.GetBoxes()
	if err != nil {
		h.logger.Error("Failed to fetch servers", "error", err)
		servers = []*database.BoxDB{} // Default to empty to show no-sidebar layout
	}

	component := pages.HAProxyBoxNewPage(user, servers, "")
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render new server page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HAProxyBoxCreatePost handles creating a new HAProxy server.
func (h *Handler) HAProxyBoxCreatePost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can create
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
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

	// Validate field lengths
	if len(r.FormValue("name")) > 100 || len(r.FormValue("box_id")) > 50 || len(r.FormValue("location")) > 200 || len(r.FormValue("notes")) > 1000 || len(r.FormValue("agent_url")) > 2048 || len(r.FormValue("api_key")) > 256 {
		h.renderServerFormWithError(w, r, user, nil, false, "One or more fields exceed maximum length")
		return
	}

	// Parse form into server struct
	server := &database.BoxDB{
		BoxID:     strings.TrimSpace(r.FormValue("box_id")),
		Name:      strings.TrimSpace(r.FormValue("name")),
		Location:  strings.TrimSpace(r.FormValue("location")),
		Notes:     strings.TrimSpace(r.FormValue("notes")),
		AgentURL:  strings.TrimSpace(r.FormValue("agent_url")),
		Enabled:   r.FormValue("enabled") == "on",
		CreatedBy: &user.ID,
	}

	// Validate required fields
	if server.BoxID == "" || server.Name == "" || server.AgentURL == "" {
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
	if err := h.db.CreateBox(server); err != nil {
		h.logger.Error("Failed to create HAProxy server", "error", err)
		h.renderServerFormWithError(w, r, user, server, false, fmt.Sprintf("Failed to create server: %v", err))
		return
	}

	// Log audit
	h.logAudit(r, user.ID, "haproxy_box_create", fmt.Sprintf("Created HAProxy box: %s (%s)", server.Name, server.BoxID))

	// Start collector if enabled
	if server.Enabled && h.registry != nil {
		// Decrypt API key for collector
		apiKeyDecrypted, _ := encryptor.DecryptString(server.APIKeyEncrypted)

		serverConfig := server.ToBoxConfig(apiKeyDecrypted)
		if err := h.registry.AddCollector(serverConfig); err != nil {
			h.logger.Error("failed to start collector for server", "server_id", server.BoxID, "error", err)
		}
	}

	// Redirect to plugins page so user can enable plugins for this new server
	http.Redirect(w, r, "/settings/gears?server="+url.QueryEscape(server.BoxID), http.StatusSeeOther)
}

// HAProxyBoxEditPage shows the form for editing a server.
func (h *Handler) HAProxyBoxEditPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can edit
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByID(id)
	if err != nil {
		h.logger.Error("Failed to get HAProxy server", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	component := pages.HAProxyBoxEditPage(user, server, "")
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render edit server page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HAProxyBoxUpdatePost handles updating a server.
func (h *Handler) HAProxyBoxUpdatePost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can update
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
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
	server, err := h.db.GetBoxByID(id)
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

	// Validate field lengths
	if len(r.FormValue("name")) > 100 || len(r.FormValue("location")) > 200 || len(r.FormValue("notes")) > 1000 || len(r.FormValue("agent_url")) > 2048 || len(r.FormValue("api_key")) > 256 {
		h.renderServerFormWithError(w, r, user, server, true, "One or more fields exceed maximum length")
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
	if err := h.db.UpdateBox(server); err != nil {
		h.logger.Error("Failed to update HAProxy server", "error", err)
		h.renderServerFormWithError(w, r, user, server, true, fmt.Sprintf("Failed to update server: %v", err))
		return
	}

	// The Agent URL or API key may have changed; drop any cached
	// capabilities so the next render fetches against the new endpoint
	// rather than serving stale data from the previous agent until the
	// TTL expires.
	h.invalidateBoxCapabilities(server.BoxID)

	// Log audit
	h.logAudit(r, user.ID, "haproxy_box_update", fmt.Sprintf("Updated HAProxy box: %s (%s)", server.Name, server.BoxID))

	// Reload collector if enabled
	if h.registry != nil {
		// Decrypt API key for collector
		apiKeyDecrypted, _ := encryptor.DecryptString(server.APIKeyEncrypted)

		serverConfig := server.ToBoxConfig(apiKeyDecrypted)

		if server.Enabled {
			if err := h.registry.ReloadCollector(serverConfig); err != nil {
				h.logger.Error("failed to reload collector for server", "server_id", server.BoxID, "error", err)
			}
		} else {
			if err := h.registry.RemoveCollector(server.BoxID); err != nil {
				h.logger.Error("failed to stop collector for server", "server_id", server.BoxID, "error", err)
			}
		}
	}

	// Redirect to list
	http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
}

// HAProxyBoxDeletePost handles deleting a server.
func (h *Handler) HAProxyBoxDeletePost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can delete
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
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
	server, err := h.db.GetBoxByID(id)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Stop collector if running
	if h.registry != nil {
		if err := h.registry.RemoveCollector(server.BoxID); err != nil {
			h.logger.Error("failed to stop collector for server", "server_id", server.BoxID, "error", err)
		}
	}

	// Delete from database
	if err := h.db.DeleteBox(id); err != nil {
		h.logger.Error("Failed to delete HAProxy server", "error", err)
		http.Error(w, "Failed to delete server", http.StatusInternalServerError)
		return
	}

	// Drop cached capabilities — even if the same box ID is recreated
	// later, it's likely a different host and we shouldn't serve the
	// previous probe table.
	h.invalidateBoxCapabilities(server.BoxID)

	// Log audit
	h.logAudit(r, user.ID, "haproxy_box_delete", fmt.Sprintf("Deleted HAProxy box: %s (%s)", server.Name, server.BoxID))

	// Redirect to list
	http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
}

// HAProxyBoxTogglePost handles enabling/disabling a server.
func (h *Handler) HAProxyBoxTogglePost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can toggle
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
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
	server, err := h.db.GetBoxByID(id)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Toggle enabled status
	newEnabled := !server.Enabled
	if err := h.db.SetBoxEnabled(id, newEnabled); err != nil {
		h.logger.Error("Failed to toggle HAProxy server", "error", err)
		http.Error(w, "Failed to toggle server", http.StatusInternalServerError)
		return
	}

	// Log audit
	action := "disabled"
	if newEnabled {
		action = "enabled"
	}
	h.logAudit(r, user.ID, "haproxy_box_toggle", fmt.Sprintf("%s HAProxy box: %s (%s)", action, server.Name, server.BoxID))

	// Start or stop collector
	if h.registry != nil {
		if newEnabled {
			// Get encryptor and decrypt API key
			encryptor, err := h.getEncryptor()
			if err == nil {
				apiKeyDecrypted, _ := encryptor.DecryptString(server.APIKeyEncrypted)

				serverConfig := server.ToBoxConfig(apiKeyDecrypted)
				if err := h.registry.AddCollector(serverConfig); err != nil {
					h.logger.Error("failed to start collector for server", "server_id", server.BoxID, "error", err)
				}
			}
		} else {
			if err := h.registry.RemoveCollector(server.BoxID); err != nil {
				h.logger.Error("failed to stop collector for server", "server_id", server.BoxID, "error", err)
			}
		}
	}

	// Redirect to list
	http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
}

// HAProxyBoxTestConnectionPost tests the connection to a server using the Agent API.
func (h *Handler) HAProxyBoxTestConnectionPost(w http.ResponseWriter, r *http.Request) {
	// Only admins can test
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
			"success": false,
			"message": "Failed to parse form",
		})
		return
	}

	// Get form values
	boxIDStr := r.FormValue("box_id")
	agentURL := strings.TrimSpace(r.FormValue("agent_url"))
	apiKey := strings.TrimSpace(r.FormValue("api_key"))

	// Validate required fields
	if agentURL == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
			"success": false,
			"message": "Agent URL is required",
		})
		return
	}

	// If editing an existing server and API key is empty, load from database
	if boxIDStr != "" && apiKey == "" {
		h.logger.Debug("test connection for existing server ID", "server_id", boxIDStr)
		existingServer, err := h.db.GetBoxByBoxID(boxIDStr)
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
		json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
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
		json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
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
			json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
				"success": false,
				"message": "Authentication failed: Invalid API key",
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
				"success": false,
				"message": fmt.Sprintf("API request failed: %v", err),
			})
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
		"success": true,
		"message": "Connection test successful! Agent API is reachable and authenticated.",
	})
}

// Helper methods

func (h *Handler) renderServerFormWithError(w http.ResponseWriter, r *http.Request, user *models.User, server *database.BoxDB, isEdit bool, errorMsg string) {
	if isEdit {
		component := pages.HAProxyBoxEditPage(user, server, errorMsg)
		if err := component.Render(r.Context(), w); err != nil {
			h.logger.Error("Failed to render form", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	} else {
		// Fetch existing servers to determine if we should show sidebar
		servers, err := h.db.GetBoxes()
		if err != nil {
			h.logger.Error("Failed to fetch servers", "error", err)
			servers = []*database.BoxDB{} // Default to empty to show no-sidebar layout
		}

		component := pages.HAProxyBoxNewPage(user, servers, errorMsg)
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

// HAProxyBoxLogSettingsPage shows the log source settings for a server.
func (h *Handler) HAProxyBoxLogSettingsPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can access
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByID(id)
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

// HAProxyBoxLogSettingsPost saves the log source settings for a server.
func (h *Handler) HAProxyBoxLogSettingsPost(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Only admins can access
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageBoxes) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid server ID", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByID(id)
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
	http.Redirect(w, r, "/settings/boxes", http.StatusSeeOther)
}

func (h *Handler) renderLogSettingsWithError(w http.ResponseWriter, r *http.Request, user *models.User, server *database.BoxDB, errorMsg string) {
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
