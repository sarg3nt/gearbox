package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// OSUpdatesPage handles GET /os-updates to display the OS updates dashboard.
func (h *Handler) OSUpdatesPage(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Check integration is enabled
	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	enabled, _ := h.db.IsGearEnabled(boxID, database.GearOSUpdates)
	if !enabled {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Check permissions
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Get server from database
	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.logger.Error("failed to get server", "server_id", boxID, "error", err)
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Get integration config
	config, err := h.db.GetOSUpdatesConfig(boxID)
	if err != nil {
		h.logger.Error("Failed to get OS updates config", "error", err)
		config = &database.OSUpdatesConfig{
			CheckFrequencyMinutes: 60,
			ShowPipx:              true,
		}
	}

	// Get agent client
	client, err := h.getAgentClient(server)
	if err != nil {
		h.logger.Error("failed to get agent client", "server_id", boxID, "error", err)
		http.Error(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	// Fetch update status from agent
	updateStatus, err := client.GetUpdateStatus()
	if err != nil {
		h.logger.Error("Failed to get update status from agent", "error", err)
		// Show page with error state
		updateStatus = &agent.UpdateStatusResponse{
			Available: false,
		}
	}

	// Fetch package list if there are updates
	var packages []agent.Package
	if updateStatus.TotalUpdates > 0 {
		pkgResp, err := client.ListUpgradablePackages()
		if err == nil && pkgResp != nil {
			packages = pkgResp.Packages
		}
	}

	// Check reboot status
	rebootStatus, _ := client.GetRebootRequired()

	// Get unattended-upgrades config
	unattendedConfig, _ := client.GetUnattendedConfig()

	// Get pipx packages if enabled
	var pipxStatus *agent.PipxStatusResponse
	if config.ShowPipx {
		pipxStatus, _ = client.GetPipxStatus()
	}

	// Get snapshots
	snapshots, _ := client.ListSnapshots()

	// Get update history
	history, _ := client.GetUpdateHistory(20)

	// Prepare view model
	data := pages.OSUpdatesPageData{
		User:             user,
		BoxID:            boxID,
		Servers:          h.getEnabledServers(),
		Config:           config,
		UpdateStatus:     updateStatus,
		Packages:         packages,
		RebootStatus:     rebootStatus,
		UnattendedConfig: unattendedConfig,
		PipxStatus:       pipxStatus,
		Snapshots:        snapshots,
		History:          history,
		CanConfigure:     h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionConfigure),
		CanAction:        h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction),
	}

	component := pages.OSUpdatesPage(data)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render OS updates page", "error", err)
	}
}

// APIUpdateStatusHandler handles GET /api/os-updates/status.
func (h *Handler) APIUpdateStatusHandler(w http.ResponseWriter, r *http.Request) {
	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	status, err := client.GetUpdateStatus()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get update status", err))
		return
	}

	h.jsonResponseOK(w, status)
}

// APIListPackagesHandler handles GET /api/os-updates/packages.
func (h *Handler) APIListPackagesHandler(w http.ResponseWriter, r *http.Request) {
	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	packages, err := client.ListUpgradablePackages()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("list packages", err))
		return
	}

	h.jsonResponseOK(w, packages)
}

// APITriggerUpdateCheckHandler handles POST /api/os-updates/check.
func (h *Handler) APITriggerUpdateCheckHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	status, err := client.TriggerUpdateCheck()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("trigger update check", err))
		return
	}

	h.logger.Info("AUDIT: triggered apt update check", "user_id", user.ID, "action", "os_updates_check")
	h.jsonResponseOK(w, status)
}

// APIInstallUpdatesHandler handles POST /api/os-updates/install.
// Use ?stream=true for streaming output via WebSocket events.
func (h *Handler) APIInstallUpdatesHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	streaming := r.URL.Query().Get("stream") == "true"
	user, _ := auth.GetUserFromContext(r.Context())

	var req agent.InstallUpdatesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get config to check if snapshot should be created
	config, _ := h.db.GetOSUpdatesConfig(boxID)
	if config != nil && config.CreateSnapshotBefore {
		req.CreateSnapshot = true
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	if streaming {
		// Streaming mode - return operation ID for WebSocket tracking
		result, err := client.InstallUpdatesStreaming(&req)
		if err != nil {
			apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("start update installation", err))
			return
		}

		auditMsg := "Started streaming update installation"
		if req.SecurityOnly {
			auditMsg = "Started streaming security updates installation"
		}
		if len(req.Packages) > 0 {
			auditMsg = "Started streaming installation of " + strconv.Itoa(len(req.Packages)) + " packages"
		}
		h.logger.Info("AUDIT", "user_id", user.ID, "action", "os_updates_install_streaming", "message", auditMsg, "operation_id", result.OperationID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(result) //#nosec G104
		return
	}

	// Synchronous mode (backwards compatible)
	result, err := client.InstallUpdates(&req)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("install updates", err))
		return
	}

	auditMsg := "Installed updates"
	if req.SecurityOnly {
		auditMsg = "Installed security updates"
	}
	if len(req.Packages) > 0 {
		auditMsg = "Installed specific packages: " + strconv.Itoa(len(req.Packages))
	}
	h.logger.Info("AUDIT", "user_id", user.ID, "action", "os_updates_install", "message", auditMsg)

	h.jsonResponseOK(w, result)
}

// APIUpdateHistoryHandler handles GET /api/os-updates/history.
func (h *Handler) APIUpdateHistoryHandler(w http.ResponseWriter, r *http.Request) {
	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	history, err := client.GetUpdateHistory(limit)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get update history", err))
		return
	}

	h.jsonResponseOK(w, history)
}

// APIScheduleRebootHandler handles POST /api/os-updates/reboot.
func (h *Handler) APIScheduleRebootHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		When string `json:"when"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	result, err := client.ScheduleReboot(req.When)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("schedule reboot", err))
		return
	}

	var auditMsg string
	if req.When == "" {
		auditMsg = "Initiated immediate system reboot"
	} else {
		auditMsg = "Scheduled system reboot for " + req.When
	}
	h.logger.Info("AUDIT", "user_id", user.ID, "action", "os_updates_reboot", "message", auditMsg)

	h.jsonResponseOK(w, result)
}

// APICancelRebootHandler handles DELETE /api/os-updates/reboot.
func (h *Handler) APICancelRebootHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	result, err := client.CancelReboot()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("cancel reboot", err))
		return
	}

	h.logger.Info("AUDIT: cancelled scheduled reboot", "user_id", user.ID, "action", "os_updates_reboot_cancel")
	h.jsonResponseOK(w, result)
}

// APIListSnapshotsHandler handles GET /api/os-updates/snapshots.
func (h *Handler) APIListSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	snapshots, err := client.ListSnapshots()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("list snapshots", err))
		return
	}

	h.jsonResponseOK(w, snapshots)
}

// APICreateSnapshotHandler handles POST /api/os-updates/snapshots.
func (h *Handler) APICreateSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Reason = "manual"
	}
	if req.Reason == "" {
		req.Reason = "manual"
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	result, err := client.CreateSnapshot(req.Reason)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("create snapshot", err))
		return
	}

	h.logger.Info("AUDIT: created package snapshot", "user_id", user.ID, "action", "os_updates_snapshot_create", "reason", req.Reason)
	h.jsonResponseOK(w, result)
}

// APIRestoreSnapshotHandler handles POST /api/os-updates/snapshots/restore.
func (h *Handler) APIRestoreSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.SnapshotID == "" {
		h.jsonError(w, "snapshot_id is required", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	result, err := client.RestoreSnapshot(req.SnapshotID)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("restore snapshot", err))
		return
	}

	h.logger.Info("AUDIT: restored package snapshot", "user_id", user.ID, "action", "os_updates_snapshot_restore", "snapshot_id", req.SnapshotID)
	h.jsonResponseOK(w, result)
}

// APIDeleteSnapshotHandler handles DELETE /api/os-updates/snapshots/{id}.
func (h *Handler) APIDeleteSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	snapshotID := r.PathValue("id")
	if snapshotID == "" {
		h.jsonError(w, "snapshot_id is required", http.StatusBadRequest)
		return
	}

	user, _ := auth.GetUserFromContext(r.Context())

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	result, err := client.DeleteSnapshot(snapshotID)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("delete snapshot", err))
		return
	}

	h.logger.Info("AUDIT: deleted package snapshot", "user_id", user.ID, "action", "os_updates_snapshot_delete", "snapshot_id", snapshotID)
	h.jsonResponseOK(w, result)
}

// APISearchPackagesHandler handles GET /api/os-updates/packages/search.
func (h *Handler) APISearchPackagesHandler(w http.ResponseWriter, r *http.Request) {
	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		h.jsonError(w, "query parameter 'q' is required", http.StatusBadRequest)
		return
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	packages, err := client.SearchPackages(query, limit)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("search packages", err))
		return
	}

	h.jsonResponseOK(w, packages)
}

// APIInstallPackageHandler handles POST /api/os-updates/packages/install.
func (h *Handler) APIInstallPackageHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		h.jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	result, err := client.InstallPackage(req.Name)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("install package", err))
		return
	}

	h.logger.Info("AUDIT: installed package", "user_id", user.ID, "action", "os_updates_package_install", "package", req.Name)
	h.jsonResponseOK(w, result)
}

// APIRemovePackageHandler handles POST /api/os-updates/packages/remove.
func (h *Handler) APIRemovePackageHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Name  string `json:"name"`
		Purge bool   `json:"purge"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		h.jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	result, err := client.RemovePackage(req.Name, req.Purge)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("remove package", err))
		return
	}

	action := "Removed"
	if req.Purge {
		action = "Purged"
	}
	h.logger.Info("AUDIT: package operation", "user_id", user.ID, "action", "os_updates_package_remove", "operation", action, "package", req.Name)
	h.jsonResponseOK(w, result)
}

// APIPipxStatusHandler handles GET /api/os-updates/pipx.
func (h *Handler) APIPipxStatusHandler(w http.ResponseWriter, r *http.Request) {
	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	status, err := client.GetPipxStatus()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get pipx status", err))
		return
	}

	h.jsonResponseOK(w, status)
}

// APIPipxInstallHandler handles POST /api/os-updates/pipx/install.
func (h *Handler) APIPipxInstallHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		h.jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	result, err := client.InstallPipxPackage(req.Name)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("install pipx package", err))
		return
	}

	h.logger.Info("AUDIT: installed pipx package", "user_id", user.ID, "action", "os_updates_pipx_install", "package", req.Name)
	h.jsonResponseOK(w, result)
}

// APIPipxUninstallHandler handles POST /api/os-updates/pipx/uninstall.
func (h *Handler) APIPipxUninstallHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		h.jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	result, err := client.UninstallPipxPackage(req.Name)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("uninstall pipx package", err))
		return
	}

	h.logger.Info("AUDIT: uninstalled pipx package", "user_id", user.ID, "action", "os_updates_pipx_uninstall", "package", req.Name)
	h.jsonResponseOK(w, result)
}

// APIPipxUpgradeHandler handles POST /api/os-updates/pipx/upgrade.
func (h *Handler) APIPipxUpgradeHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	var result *agent.PipxPackageResponse
	if req.Name == "" {
		// Upgrade all
		result, err = client.UpgradeAllPipxPackages()
		h.logger.Info("AUDIT: upgraded all pipx packages", "user_id", user.ID, "action", "os_updates_pipx_upgrade")
	} else {
		result, err = client.UpgradePipxPackage(req.Name)
		h.logger.Info("AUDIT: upgraded pipx package", "user_id", user.ID, "action", "os_updates_pipx_upgrade", "package", req.Name)
	}

	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("upgrade pipx package", err))
		return
	}

	h.jsonResponseOK(w, result)
}

// APIUnattendedConfigHandler handles GET /api/os-updates/unattended.
func (h *Handler) APIUnattendedConfigHandler(w http.ResponseWriter, r *http.Request) {
	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	config, err := client.GetUnattendedConfig()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get unattended config", err))
		return
	}

	h.jsonResponseOK(w, config)
}

// APIConfigureUnattendedHandler handles POST /api/os-updates/unattended.
func (h *Handler) APIConfigureUnattendedHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionConfigure) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := r.URL.Query().Get("server")
	if boxID == "" {
		boxID = h.getDefaultServerID()
	}

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Enabled    bool `json:"enabled"`
		AutoReboot bool `json:"auto_reboot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}

	config, err := client.ConfigureUnattended(req.Enabled, req.AutoReboot)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("configure unattended", err))
		return
	}

	status := "disabled"
	if req.Enabled {
		status = "enabled"
	}
	h.logger.Info("AUDIT: configured unattended-upgrades", "user_id", user.ID, "action", "os_updates_unattended_config", "status", status)
	h.jsonResponseOK(w, config)
}

// Helper for JSON responses
func (h *Handler) jsonResponseOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data) //#nosec G104
}
