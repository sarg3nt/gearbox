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
	"github.com/sarg3nt/gearbox/internal/framework/database"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// HAProxyConfigPage shows the HAProxy configuration editor page.
func (h *Handler) HAProxyConfigPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentHAProxyConfig, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	// Get server from database
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Check if user can edit (for UI purposes)
	canEdit := h.authManager.HasPermission(r, models.ComponentHAProxyConfig, models.PermissionConfigure)

	// Get config from agent
	client, err := h.getAgentClient(server)
	if err != nil {
		h.logger.Error("Failed to create agent client", "error", err)
		component := pages.HAProxyConfigPageWithError(user, server, "Failed to connect to agent: "+err.Error(), canEdit)
		component.Render(r.Context(), w)
		return
	}

	configResp, err := client.GetHAProxyConfig()
	if err != nil {
		h.logger.Error("Failed to get HAProxy config", "error", err)
		component := pages.HAProxyConfigPageWithError(user, server, "Failed to load configuration: "+err.Error(), canEdit)
		component.Render(r.Context(), w)
		return
	}

	// Redact sensitive values (e.g., stats auth password) server-side
	// The original values are stored in the redactor for restoration on save
	if configResp != nil && h.configRedactor != nil {
		configResp.Content = h.configRedactor.RedactConfig(serverID, configResp.Content)
	}

	// Get git config if any
	gitConfig, _ := h.db.GetServerGitConfig(server.ID, database.ConfigTypeHAProxy)

	// Get recent changes
	changes, _ := h.db.GetConfigChanges(server.ID, database.ConfigTypeHAProxy, 10)

	component := pages.HAProxyConfigPage(user, server, configResp, gitConfig, changes, canEdit)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render HAProxy config page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIHAProxyConfigGet returns the current HAProxy configuration as JSON.
func (h *Handler) APIHAProxyConfigGet(w http.ResponseWriter, r *http.Request) {
	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentHAProxyConfig, models.PermissionView) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	configResp, err := client.GetHAProxyConfig()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get config", err))
		return
	}

	// Redact sensitive values (e.g., stats auth password) server-side
	if configResp != nil && h.configRedactor != nil {
		configResp.Content = h.configRedactor.RedactConfig(serverID, configResp.Content)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configResp)
}

// APIHAProxyConfigSave saves the HAProxy configuration.
func (h *Handler) APIHAProxyConfigSave(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentHAProxyConfig, models.PermissionConfigure) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	// Parse request body
	var req agent.HAProxyConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.BadRequest("Invalid request body", err))
		return
	}

	// Restore any redacted values (e.g., stats auth password) before saving
	// If user kept "<redacted>", the original password is restored
	// If user changed it to a new value, that value is used
	if h.configRedactor != nil {
		restored, err := h.configRedactor.RestoreConfig(serverID, req.Content)
		if err != nil {
			h.jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Content = restored
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	// Get current config SHA for change tracking
	currentConfig, _ := client.GetHAProxyConfig()
	previousSHA := ""
	if currentConfig != nil {
		previousSHA = currentConfig.SHA256
	}

	// Send update to agent
	updateResp, err := client.UpdateHAProxyConfig(&req)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("update config", err))
		return
	}

	// Record the change if not a dry run and successful
	if !req.DryRun && updateResp.Success {
		change := &database.ConfigChange{
			HAProxyServerID: server.ID,
			ConfigType:      database.ConfigTypeHAProxy,
			ChangeType:      database.ChangeTypeManual,
			PreviousSHA256:  previousSHA,
			NewSHA256:       updateResp.NewSHA256,
			ChangedBy:       &user.ID,
			ChangeReason:    req.BackupReason,
			BackupPath:      updateResp.BackupPath,
		}
		if err := h.db.RecordConfigChange(change); err != nil {
			h.logger.Error("Failed to record config change", "error", err)
		}

		// Log audit
		h.logAudit(r, user.ID, "haproxy_config_update", fmt.Sprintf("Updated HAProxy config on server %s", server.Name))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updateResp)
}

// APIHAProxyConfigValidate validates the HAProxy configuration without saving.
func (h *Handler) APIHAProxyConfigValidate(w http.ResponseWriter, r *http.Request) {
	// Check permission (view is enough for validation)
	if !h.authManager.HasPermission(r, models.ComponentHAProxyConfig, models.PermissionView) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	// Parse request body
	var req agent.HAProxyConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.BadRequest("Invalid request body", err))
		return
	}

	// Restore any redacted values before validation
	if h.configRedactor != nil {
		restored, err := h.configRedactor.RestoreConfig(serverID, req.Content)
		if err != nil {
			h.jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Content = restored
	}

	// Force dry run
	req.DryRun = true

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	updateResp, err := client.UpdateHAProxyConfig(&req)
	if err != nil {
		// Check if config contains unrestored markers (indicates a bug)
		if strings.Contains(req.Content, "<redacted:") {
			h.jsonError(w, "Config contains unrestored redaction markers - this is a bug, please report it", http.StatusInternalServerError)
			return
		}
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("validate config", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updateResp)
}

// APIHAProxyConfigBackups returns the list of configuration backups.
func (h *Handler) APIHAProxyConfigBackups(w http.ResponseWriter, r *http.Request) {
	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentHAProxyConfig, models.PermissionView) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	backupsResp, err := client.GetHAProxyConfigBackups()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get backups", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(backupsResp)
}

// APIHAProxyConfigRestore restores configuration from a backup.
func (h *Handler) APIHAProxyConfigRestore(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentHAProxyConfig, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	// Parse request body
	var req agent.HAProxyRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.BadRequest("Invalid request body", err))
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	// Get current config SHA for change tracking
	currentConfig, _ := client.GetHAProxyConfig()
	previousSHA := ""
	if currentConfig != nil {
		previousSHA = currentConfig.SHA256
	}

	restoreResp, err := client.RestoreHAProxyConfig(&req)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("restore config", err))
		return
	}

	// Record the change if not a dry run and successful
	if !req.DryRun && restoreResp.Success {
		change := &database.ConfigChange{
			HAProxyServerID: server.ID,
			ConfigType:      database.ConfigTypeHAProxy,
			ChangeType:      database.ChangeTypeRestore,
			PreviousSHA256:  previousSHA,
			NewSHA256:       restoreResp.NewSHA256,
			ChangedBy:       &user.ID,
			ChangeReason:    fmt.Sprintf("Restored from backup: %s", req.BackupID),
		}
		if err := h.db.RecordConfigChange(change); err != nil {
			h.logger.Error("Failed to record config change", "error", err)
		}

		// Log audit
		h.logAudit(r, user.ID, "haproxy_config_restore", fmt.Sprintf("Restored HAProxy config on server %s from backup %s", server.Name, req.BackupID))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(restoreResp)
}

// APIHAProxyConfigHistory returns the configuration change history.
func (h *Handler) APIHAProxyConfigHistory(w http.ResponseWriter, r *http.Request) {
	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentHAProxyConfig, models.PermissionView) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	changes, err := h.db.GetConfigChanges(server.ID, database.ConfigTypeHAProxy, 50)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get history", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"changes": changes,
	})
}

// ServerGitSettingsPage shows the git settings page for a server.
func (h *Handler) ServerGitSettingsPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// The route uses {id} which is the database ID
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

	// Get git configs for both haproxy and firewall
	haproxyGitConfig, _ := h.db.GetServerGitConfig(server.ID, database.ConfigTypeHAProxy)
	firewallGitConfig, _ := h.db.GetServerGitConfig(server.ID, database.ConfigTypeFirewall)

	component := pages.ServerGitSettingsPage(user, server, haproxyGitConfig, firewallGitConfig, "", "")
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render git settings page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ServerGitSettingsSave saves the git settings for a server.
func (h *Handler) ServerGitSettingsSave(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentSettings, models.PermissionManageServers) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// The route uses {id} which is the database ID
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

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Get encryptor for PAT encryption
	encryptor, err := h.getEncryptor()
	if err != nil {
		h.logger.Error("Failed to get encryptor", "error", err)
		http.Error(w, "Encryption error", http.StatusInternalServerError)
		return
	}

	// Save HAProxy git config
	haproxyConfig := &database.ServerGitConfig{
		HAProxyServerID:     server.ID,
		ConfigType:          database.ConfigTypeHAProxy,
		GitRepoURL:          r.FormValue("haproxy_git_repo"),
		GitBranch:           r.FormValue("haproxy_git_branch"),
		GitFilePath:         r.FormValue("haproxy_git_path"),
		AutoApplyEnabled:    r.FormValue("haproxy_auto_apply") == "on",
		SyncIntervalMinutes: 60, // Default, could make configurable
	}

	// Handle PAT
	if pat := r.FormValue("haproxy_git_pat"); pat != "" {
		haproxyConfig.GitPATEncrypted, _ = encryptor.EncryptString(pat)
	} else {
		// Keep existing PAT if not provided
		existing, _ := h.db.GetServerGitConfig(server.ID, database.ConfigTypeHAProxy)
		if existing != nil {
			haproxyConfig.GitPATEncrypted = existing.GitPATEncrypted
		}
	}

	if haproxyConfig.GitRepoURL != "" {
		if err := h.db.SaveServerGitConfig(haproxyConfig); err != nil {
			h.logger.Error("Failed to save HAProxy git config", "error", err)
		}
	} else {
		// Clear config if repo URL is empty
		h.db.DeleteServerGitConfig(server.ID, database.ConfigTypeHAProxy)
	}

	// Save Firewall git config
	firewallConfig := &database.ServerGitConfig{
		HAProxyServerID:     server.ID,
		ConfigType:          database.ConfigTypeFirewall,
		GitRepoURL:          r.FormValue("firewall_git_repo"),
		GitBranch:           r.FormValue("firewall_git_branch"),
		GitFilePath:         r.FormValue("firewall_git_path"),
		AutoApplyEnabled:    r.FormValue("firewall_auto_apply") == "on",
		SyncIntervalMinutes: 60,
	}

	// Handle PAT
	if pat := r.FormValue("firewall_git_pat"); pat != "" {
		firewallConfig.GitPATEncrypted, _ = encryptor.EncryptString(pat)
	} else {
		existing, _ := h.db.GetServerGitConfig(server.ID, database.ConfigTypeFirewall)
		if existing != nil {
			firewallConfig.GitPATEncrypted = existing.GitPATEncrypted
		}
	}

	if firewallConfig.GitRepoURL != "" {
		if err := h.db.SaveServerGitConfig(firewallConfig); err != nil {
			h.logger.Error("Failed to save firewall git config", "error", err)
		}
	} else {
		h.db.DeleteServerGitConfig(server.ID, database.ConfigTypeFirewall)
	}

	// Log audit
	h.logAudit(r, user.ID, "git_settings_update", fmt.Sprintf("Updated git settings for server %s", server.Name))

	// Redirect back to settings
	http.Redirect(w, r, "/settings/servers", http.StatusSeeOther)
}

// FirewallConfigPage shows the firewall configuration editor page.
func (h *Handler) FirewallConfigPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentFirewallConfig, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID is required", http.StatusBadRequest)
		return
	}

	// Get server from database
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Check if user can edit (for UI purposes)
	canEdit := h.authManager.HasPermission(r, models.ComponentFirewallConfig, models.PermissionConfigure)

	// Get config from agent
	client, err := h.getAgentClient(server)
	if err != nil {
		h.logger.Error("Failed to create agent client", "error", err)
		component := pages.FirewallConfigPageWithError(user, server, "Failed to connect to agent: "+err.Error(), canEdit)
		component.Render(r.Context(), w)
		return
	}

	configResp, err := client.GetFirewallConfig()
	if err != nil {
		h.logger.Error("Failed to get firewall config", "error", err)
		component := pages.FirewallConfigPageWithError(user, server, "Failed to load configuration: "+err.Error(), canEdit)
		component.Render(r.Context(), w)
		return
	}

	// Get git config if any
	gitConfig, _ := h.db.GetServerGitConfig(server.ID, database.ConfigTypeFirewall)

	// Get recent changes
	changes, _ := h.db.GetConfigChanges(server.ID, database.ConfigTypeFirewall, 10)

	component := pages.FirewallConfigPage(user, server, configResp, gitConfig, changes, canEdit)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render firewall config page", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APIFirewallConfigGet returns the current firewall configuration as JSON.
func (h *Handler) APIFirewallConfigGet(w http.ResponseWriter, r *http.Request) {
	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentFirewallConfig, models.PermissionView) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	configResp, err := client.GetFirewallConfig()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get config", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configResp)
}

// APIFirewallConfigSave saves the firewall configuration.
func (h *Handler) APIFirewallConfigSave(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentFirewallConfig, models.PermissionConfigure) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	// Parse request body
	var req agent.FirewallConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.BadRequest("Invalid request body", err))
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	// Get current config SHA for change tracking
	currentConfig, _ := client.GetFirewallConfig()
	previousSHA := ""
	if currentConfig != nil {
		previousSHA = currentConfig.SHA256
	}

	// Send update to agent
	updateResp, err := client.UpdateFirewallConfig(&req)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("update config", err))
		return
	}

	// Record the change if not a dry run and successful
	if !req.DryRun && updateResp.Success {
		change := &database.ConfigChange{
			HAProxyServerID: server.ID,
			ConfigType:      database.ConfigTypeFirewall,
			ChangeType:      database.ChangeTypeManual,
			PreviousSHA256:  previousSHA,
			NewSHA256:       updateResp.NewSHA256,
			ChangedBy:       &user.ID,
			ChangeReason:    req.BackupReason,
			BackupPath:      updateResp.BackupPath,
		}
		if err := h.db.RecordConfigChange(change); err != nil {
			h.logger.Error("Failed to record config change", "error", err)
		}

		// Log audit
		h.logAudit(r, user.ID, "firewall_config_update", fmt.Sprintf("Updated firewall config on server %s", server.Name))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updateResp)
}

// APIFirewallConfigValidate validates the firewall configuration without saving.
func (h *Handler) APIFirewallConfigValidate(w http.ResponseWriter, r *http.Request) {
	// Check permission (view is enough for validation)
	if !h.authManager.HasPermission(r, models.ComponentFirewallConfig, models.PermissionView) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	// Parse request body
	var req agent.FirewallConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.BadRequest("Invalid request body", err))
		return
	}

	// Force dry run
	req.DryRun = true

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	updateResp, err := client.UpdateFirewallConfig(&req)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("validate config", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updateResp)
}

// APIFirewallConfigBackups returns the list of firewall configuration backups.
func (h *Handler) APIFirewallConfigBackups(w http.ResponseWriter, r *http.Request) {
	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentFirewallConfig, models.PermissionView) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	backupsResp, err := client.GetFirewallConfigBackups()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get backups", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(backupsResp)
}

// APIFirewallConfigRestore restores firewall configuration from a backup.
func (h *Handler) APIFirewallConfigRestore(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())

	// Check permission
	if !h.authManager.HasPermission(r, models.ComponentFirewallConfig, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.db.GetServerByServerID(serverID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	// Parse request body
	var req agent.FirewallRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.BadRequest("Invalid request body", err))
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.ServiceUnavailable("agent", err))
		return
	}

	// Get current config SHA for change tracking
	currentConfig, _ := client.GetFirewallConfig()
	previousSHA := ""
	if currentConfig != nil {
		previousSHA = currentConfig.SHA256
	}

	restoreResp, err := client.RestoreFirewallConfig(&req)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("restore config", err))
		return
	}

	// Record the change if not a dry run and successful
	if !req.DryRun && restoreResp.Success {
		change := &database.ConfigChange{
			HAProxyServerID: server.ID,
			ConfigType:      database.ConfigTypeFirewall,
			ChangeType:      database.ChangeTypeRestore,
			PreviousSHA256:  previousSHA,
			NewSHA256:       restoreResp.NewSHA256,
			ChangedBy:       &user.ID,
			ChangeReason:    fmt.Sprintf("Restored from backup: %s", req.BackupID),
		}
		if err := h.db.RecordConfigChange(change); err != nil {
			h.logger.Error("Failed to record config change", "error", err)
		}

		// Log audit
		h.logAudit(r, user.ID, "firewall_config_restore", fmt.Sprintf("Restored firewall config on server %s from backup %s", server.Name, req.BackupID))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(restoreResp)
}

// Helper methods

func (h *Handler) getAgentClient(server *database.ServerDB) (*agent.Client, error) {
	encryptor, err := h.getEncryptor()
	if err != nil {
		return nil, fmt.Errorf("failed to get encryptor: %w", err)
	}

	apiKey, err := encryptor.DecryptString(server.APIKeyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	return agent.NewClient(server.AgentURL, apiKey), nil
}

func (h *Handler) jsonError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"message": message,
	})
}
