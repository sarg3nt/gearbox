package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/database"
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
	boxID := h.resolveBoxIDFromRequest(r)

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
			ShowPythonTools:       true,
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

	// Get Python tools status (pip + pipx) if enabled
	var pythonToolsStatus *agent.PythonToolsStatusResponse
	if config.ShowPythonTools {
		pythonToolsStatus, _ = client.GetPythonToolsStatus()
	}

	// Snapshots and update history are now lazy-loaded by the page JS on
	// first expand of their collapsible sections (see SECTIONS in
	// os-updates-page.js). Skipping the server-side fetch here saves two
	// agent round-trips on every page load.

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
		PythonToolsStatus: pythonToolsStatus,
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
	boxID := h.resolveBoxIDFromRequest(r)

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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponseOK(w, status)
}

// APIListPackagesHandler handles GET /api/os-updates/packages.
func (h *Handler) APIListPackagesHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
		h.logger.Error("Failed to trigger update check", "error", err)
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
			h.jsonError(w, err.Error(), http.StatusInternalServerError)
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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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
	boxID := h.resolveBoxIDFromRequest(r)

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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		When string `json:"when"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.When) > 20 {
		h.jsonError(w, "when must be 20 characters or less", http.StatusBadRequest)
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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.logger.Info("AUDIT: cancelled scheduled reboot", "user_id", user.ID, "action", "os_updates_reboot_cancel")
	h.jsonResponseOK(w, result)
}

// APIListSnapshotsHandler handles GET /api/os-updates/snapshots.
func (h *Handler) APIListSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
	if len(req.Reason) > 255 {
		h.jsonError(w, "reason must be 255 characters or less", http.StatusBadRequest)
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

	result, err := client.CreateSnapshot(req.Reason)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
	if len(req.SnapshotID) > 100 {
		h.jsonError(w, "snapshot_id must be 100 characters or less", http.StatusBadRequest)
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

	// Always use streaming mode for restore operations
	result, err := client.RestoreSnapshotStreaming(req.SnapshotID)
	if err != nil {
		h.logger.Error("Failed to start snapshot restore", "error", err, "snapshot_id", req.SnapshotID)
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.logger.Info("AUDIT: started streaming snapshot restore", "user_id", user.ID, "action", "os_updates_snapshot_restore", "snapshot_id", req.SnapshotID, "operation_id", result.OperationID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(result) //#nosec G104
}

// APIDeleteSnapshotHandler handles DELETE /api/os-updates/snapshots/{id}.
func (h *Handler) APIDeleteSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := h.resolveBoxIDFromRequest(r)

	snapshotID := chi.URLParam(r, "id")
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
		h.logger.Error("Failed to delete snapshot", "error", err, "snapshot_id", snapshotID)
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.logger.Info("AUDIT: deleted package snapshot", "user_id", user.ID, "action", "os_updates_snapshot_delete", "snapshot_id", snapshotID)
	h.jsonResponseOK(w, result)
}

// APIPreviewSnapshotHandler handles GET /api/os-updates/snapshots/{id}/preview.
func (h *Handler) APIPreviewSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

	snapshotID := chi.URLParam(r, "id")
	if snapshotID == "" {
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

	preview, err := client.PreviewSnapshot(snapshotID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponseOK(w, preview)
}

// APIListInstalledPackagesHandler handles GET /api/os-updates/packages/installed.
func (h *Handler) APIListInstalledPackagesHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

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

	packages, err := client.GetInstalledPackages()
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponseOK(w, packages)
}

// APISearchPackagesHandler handles GET /api/os-updates/packages/search.
func (h *Handler) APISearchPackagesHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
	if len(req.Name) > 200 {
		h.jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
	if len(req.Name) > 200 {
		h.jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	action := "Removed"
	if req.Purge {
		action = "Purged"
	}
	h.logger.Info("AUDIT: package operation", "user_id", user.ID, "action", "os_updates_package_remove", "operation", action, "package", req.Name)
	h.jsonResponseOK(w, result)
}

// APIHoldPackageHandler handles POST /api/os-updates/packages/hold.
func (h *Handler) APIHoldPackageHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}
	boxID := h.resolveBoxIDFromRequest(r)
	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}
	var req agent.HoldPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		h.jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}
	result, err := client.HoldPackage(req.Name)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	user, _ := auth.GetUserFromContext(r.Context())
	h.logger.Info("AUDIT: package hold", "user_id", user.ID, "action", "os_updates_package_hold", "package", req.Name)
	h.jsonResponseOK(w, result)
}

// APIUnholdPackageHandler handles POST /api/os-updates/packages/unhold.
func (h *Handler) APIUnholdPackageHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}
	boxID := h.resolveBoxIDFromRequest(r)
	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		h.jsonError(w, "Server not found", http.StatusNotFound)
		return
	}
	var req agent.HoldPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		h.jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	client, err := h.getAgentClient(server)
	if err != nil {
		h.jsonError(w, "Failed to connect to agent", http.StatusServiceUnavailable)
		return
	}
	result, err := client.UnholdPackage(req.Name)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	user, _ := auth.GetUserFromContext(r.Context())
	h.logger.Info("AUDIT: package unhold", "user_id", user.ID, "action", "os_updates_package_unhold", "package", req.Name)
	h.jsonResponseOK(w, result)
}


// APIPipxStatusHandler handles GET /api/os-updates/pipx.
func (h *Handler) APIPipxStatusHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
	if len(req.Name) > 200 {
		h.jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
	if len(req.Name) > 200 {
		h.jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 200 {
		h.jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
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
	} else {
		result, err = client.UpgradePipxPackage(req.Name)
	}

	if err != nil {
		h.logger.Error("Failed to upgrade pipx package(s)", "error", err, "name", req.Name)
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Name == "" {
		h.logger.Info("AUDIT: upgraded all pipx packages", "user_id", user.ID, "action", "os_updates_pipx_upgrade")
	} else {
		h.logger.Info("AUDIT: upgraded pipx package", "user_id", user.ID, "action", "os_updates_pipx_upgrade", "package", req.Name)
	}

	h.jsonResponseOK(w, result)
}

// APIPipStatusHandler handles GET /api/os-updates/pip.
func (h *Handler) APIPipStatusHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

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

	status, err := client.GetPipStatus()
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponseOK(w, status)
}

// APIPipInstallHandler handles POST /api/os-updates/pip/install.
func (h *Handler) APIPipInstallHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := h.resolveBoxIDFromRequest(r)

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
	if len(req.Name) > 200 {
		h.jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
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

	result, err := client.InstallPipPackage(req.Name)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.logger.Info("AUDIT: installed pip package", "user_id", user.ID, "action", "os_updates_pip_install", "package", req.Name)
	h.jsonResponseOK(w, result)
}

// APIPipUninstallHandler handles POST /api/os-updates/pip/uninstall.
func (h *Handler) APIPipUninstallHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := h.resolveBoxIDFromRequest(r)

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
	if len(req.Name) > 200 {
		h.jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
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

	result, err := client.UninstallPipPackage(req.Name)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.logger.Info("AUDIT: uninstalled pip package", "user_id", user.ID, "action", "os_updates_pip_uninstall", "package", req.Name)
	h.jsonResponseOK(w, result)
}

// APIPipUpgradeHandler handles POST /api/os-updates/pip/upgrade.
func (h *Handler) APIPipUpgradeHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentOSUpdates, models.PermissionAction) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := h.resolveBoxIDFromRequest(r)

	user, _ := auth.GetUserFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 200 {
		h.jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
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
		result, err = client.UpgradeAllPipPackages()
	} else {
		result, err = client.UpgradePipPackage(req.Name)
	}

	if err != nil {
		h.logger.Error("Failed to upgrade pip package(s)", "error", err, "name", req.Name)
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Name == "" {
		h.logger.Info("AUDIT: upgraded all pip packages", "user_id", user.ID, "action", "os_updates_pip_upgrade")
	} else {
		h.logger.Info("AUDIT: upgraded pip package", "user_id", user.ID, "action", "os_updates_pip_upgrade", "package", req.Name)
	}

	h.jsonResponseOK(w, result)
}

// APIPythonToolsVersionsHandler handles GET /api/os-updates/python-tools/versions.
// This is the slow endpoint that fetches latest PyPI version info for all packages.
func (h *Handler) APIPythonToolsVersionsHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

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

	status, err := client.GetPythonToolsVersions()
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponseOK(w, status)
}

// APIPyPILookupHandler handles GET /api/os-updates/pypi-lookup?name=<pkg>.
// Proxies the PyPI JSON API so the browser avoids CORS issues.
func (h *Handler) APIPyPILookupHandler(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		h.jsonError(w, "name parameter required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://pypi.org/pypi/"+name+"/json", nil)
	if err != nil {
		h.jsonError(w, "Failed to build request", http.StatusInternalServerError)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.jsonError(w, "PyPI unreachable", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		h.jsonError(w, "Package not found", http.StatusNotFound)
		return
	}
	if resp.StatusCode != http.StatusOK {
		h.jsonError(w, "PyPI error", http.StatusBadGateway)
		return
	}

	var pypiResp struct {
		Info struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			Summary     string `json:"summary"`
			ProjectURL  string `json:"project_url"`
		} `json:"info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pypiResp); err != nil {
		h.jsonError(w, "Failed to parse PyPI response", http.StatusInternalServerError)
		return
	}

	h.jsonResponseOK(w, map[string]string{
		"name":        pypiResp.Info.Name,
		"version":     pypiResp.Info.Version,
		"summary":     pypiResp.Info.Summary,
		"project_url": "https://pypi.org/project/" + pypiResp.Info.Name + "/",
	})
}

// APIUnattendedConfigHandler handles GET /api/os-updates/unattended.
func (h *Handler) APIUnattendedConfigHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
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

	boxID := h.resolveBoxIDFromRequest(r)

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
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	status := "disabled"
	if req.Enabled {
		status = "enabled"
	}
	h.logger.Info("AUDIT: configured unattended-upgrades", "user_id", user.ID, "action", "os_updates_unattended_config", "status", status)
	h.jsonResponseOK(w, config)
}

// APIGetOperationHandler handles GET /api/os-updates/operation/{id}.
// Proxies to the agent's operation status endpoint for polling fallback.
func (h *Handler) APIGetOperationHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

	operationID := chi.URLParam(r, "id")
	if operationID == "" {
		h.jsonError(w, "operation_id is required", http.StatusBadRequest)
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

	status, err := client.GetOperationStatus(operationID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponseOK(w, status)
}

// APIListUpdateLogsHandler handles GET /api/os-updates/logs.
func (h *Handler) APIListUpdateLogsHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

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

	logs, err := client.ListUpdateLogs(limit)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponseOK(w, logs)
}

// APIGetUpdateLogHandler handles GET /api/os-updates/logs/{id}.
func (h *Handler) APIGetUpdateLogHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

	logID := chi.URLParam(r, "id")
	if logID == "" {
		h.jsonError(w, "log id is required", http.StatusBadRequest)
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

	logEntry, err := client.GetUpdateLog(logID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.jsonResponseOK(w, logEntry)
}

// Helper for JSON responses
func (h *Handler) jsonResponseOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data) //#nosec G104
}
