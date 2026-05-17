// Package updates provides OS update management as a gear.
package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

func init() {
	gear.Register(&Gear{})
}

// Plugin provides OS update and package management functionality.
type Gear struct {
	gear.BaseGear
	collector *UpdatesCollector
	aptRunner *AptRunner
	eventBus  *events.Bus
}

// Info returns plugin metadata.
func (p *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "updates",
		DisplayName: "OS Updates",
		Description: "Manages OS updates, packages, snapshots, and unattended-upgrades",
		Version:     "1.0.0",
		Category:    "system",
		Core:        false,
	}
}

// Initialize sets up the gear.
func (p *Gear) Initialize(ctx context.Context, deps gear.Dependencies) error {
	if err := p.BaseGear.Initialize(ctx, deps); err != nil {
		return err
	}

	p.collector = NewUpdatesCollector()
	p.eventBus = deps.EventBus
	if p.eventBus != nil {
		p.aptRunner = NewAptRunner(p.eventBus)
	}
	return nil
}

// Start is a no-op for this gear.
func (p *Gear) Start(ctx context.Context) error {
	return nil
}

// Probe reports whether a supported package manager is present. The gear
// manages installs, upgrades, snapshots, and reboot orchestration, all of
// which require a real package manager on this host. Today only apt is
// wired into the runner, but we accept any of the common Linux managers
// here so the table reflects what's installed rather than what we've
// implemented support for; mismatch is the right kind of warning.
func (p *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
	// Ordered by frequency on the hosts we care about. First hit wins.
	for _, mgr := range []string{"apt-get", "apt", "dnf", "yum", "zypper", "apk"} {
		if path, err := exec.LookPath(mgr); err == nil {
			return gear.ProbeAvailable("package manager found", map[string]string{
				"package_manager": mgr,
				"path":            path,
			})
		}
	}
	return gear.ProbeNotInstalled("no supported package manager found on PATH (apt-get/apt/dnf/yum/zypper/apk)")
}

// Stop cleans up resources.
func (p *Gear) Stop(ctx context.Context) error {
	return nil
}

// RegisterRoutes registers HTTP API routes.
func (p *Gear) RegisterRoutes(r chi.Router) {
	// Update status and actions
	r.Get("/api/v1/system/updates", p.handleStatus)
	r.Get("/api/v1/system/updates/list", p.handleListUpgradable)
	r.Post("/api/v1/system/updates/check", p.handleTriggerCheck)
	r.Post("/api/v1/system/updates/install", p.handleInstallUpdates)
	r.Get("/api/v1/system/updates/history", p.handleHistory)
	r.Get("/api/v1/system/updates/reboot-required", p.handleRebootRequired)
	r.Post("/api/v1/system/updates/reboot", p.handleScheduleReboot)
	r.Delete("/api/v1/system/updates/reboot", p.handleCancelReboot)

	// Snapshots
	r.Get("/api/v1/system/updates/snapshots", p.handleListSnapshots)
	r.Post("/api/v1/system/updates/snapshots", p.handleCreateSnapshot)
	r.Post("/api/v1/system/updates/snapshots/restore", p.handleRestoreSnapshot)
	r.Get("/api/v1/system/updates/snapshots/{id}/preview", p.handlePreviewSnapshot)
	r.Delete("/api/v1/system/updates/snapshots/{id}", p.handleDeleteSnapshot)

	// Package management
	r.Get("/api/v1/system/packages/installed", p.handleListInstalledPackages)
	r.Get("/api/v1/system/packages/search", p.handleSearchPackages)
	r.Post("/api/v1/system/packages/install", p.handleInstallPackage)
	r.Post("/api/v1/system/packages/remove", p.handleRemovePackage)
	r.Post("/api/v1/system/packages/hold", p.handleHoldPackage)
	r.Post("/api/v1/system/packages/unhold", p.handleUnholdPackage)

	// Pipx
	r.Get("/api/v1/system/pipx", p.handlePipxStatus)
	r.Post("/api/v1/system/pipx/install", p.handlePipxInstall)
	r.Post("/api/v1/system/pipx/uninstall", p.handlePipxUninstall)
	r.Post("/api/v1/system/pipx/upgrade", p.handlePipxUpgrade)
	r.Post("/api/v1/system/pipx/upgrade-all", p.handlePipxUpgradeAll)

	// Pip
	r.Get("/api/v1/system/pip", p.handlePipStatus)
	r.Post("/api/v1/system/pip/install", p.handlePipInstall)
	r.Post("/api/v1/system/pip/uninstall", p.handlePipUninstall)
	r.Post("/api/v1/system/pip/upgrade", p.handlePipUpgrade)
	r.Post("/api/v1/system/pip/upgrade-all", p.handlePipUpgradeAll)

	// Combined Python tools status
	r.Get("/api/v1/system/python-tools", p.handlePythonToolsStatus)
	r.Get("/api/v1/system/python-tools/versions", p.handlePythonToolsVersions)

	// Unattended-upgrades
	r.Get("/api/v1/system/updates/unattended", p.handleUnattendedConfig)
	r.Post("/api/v1/system/updates/unattended", p.handleConfigureUnattended)

	// Operations
	r.Get("/api/v1/system/updates/operations", p.handleListOperations)
	r.Get("/api/v1/system/updates/operation/{id}", p.handleGetOperation)
	r.Delete("/api/v1/system/updates/operation/{id}", p.handleCancelOperation)

	// Update logs
	r.Get("/api/v1/system/updates/logs", p.handleListUpdateLogs)
	r.Get("/api/v1/system/updates/logs/{id}", p.handleGetUpdateLog)
}

// EventTypes returns the events this plugin publishes.
func (p *Gear) EventTypes() []gear.EventType {
	return []gear.EventType{
		{
			Name:        "apt.output",
			Description: "Published when apt produces output during streaming operations",
			Payload:     "operation_id, line, stream (stdout/stderr)",
		},
		{
			Name:        "apt.completed",
			Description: "Published when an apt operation completes",
			Payload:     "operation_id, status, exit_code, error",
		},
	}
}

// Response types

// UpdateStatusResponse represents the current update status.
type UpdateStatusResponse struct {
	Available        bool   `json:"available"`
	TotalUpdates     int    `json:"total_updates"`
	SecurityUpdates  int    `json:"security_updates"`
	RebootRequired   bool   `json:"reboot_required"`
	LastCheck        string `json:"last_check"`
	UnattendedActive bool   `json:"unattended_active"`
}

// PackageListResponse represents a list of packages.
type PackageListResponse struct {
	Packages []Package `json:"packages"`
	Count    int              `json:"count"`
}

// InstallUpdatesRequest represents a request to install updates.
type InstallUpdatesRequest struct {
	Packages       []string `json:"packages"`
	SecurityOnly   bool     `json:"security_only"`
	CreateSnapshot bool     `json:"create_snapshot"`
}

// InstallUpdatesResponse represents the result of installing updates.
type InstallUpdatesResponse struct {
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	Packages   []string `json:"packages"`
	SnapshotID string   `json:"snapshot_id,omitempty"`
}

// StreamingInstallResponse represents the response for streaming install.
type StreamingInstallResponse struct {
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
	Message     string `json:"message"`
}

// UpdateHistoryResponse represents package update history.
type UpdateHistoryResponse struct {
	History []UpdateHistoryEntry `json:"history"`
	Count   int                         `json:"count"`
}

// RebootRequiredResponse represents reboot status.
type RebootRequiredResponse struct {
	Required bool     `json:"required"`
	Packages []string `json:"packages"`
}

// ScheduleRebootRequest represents a reboot scheduling request.
type ScheduleRebootRequest struct {
	When string `json:"when"`
}

// ScheduleRebootResponse represents reboot scheduling result.
type ScheduleRebootResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// SnapshotsResponse represents available snapshots.
type SnapshotsResponse struct {
	Snapshots []AptSnapshot `json:"snapshots"`
	Count     int                  `json:"count"`
}

// CreateSnapshotRequest represents a snapshot creation request.
type CreateSnapshotRequest struct {
	Reason string `json:"reason"`
}

// CreateSnapshotResponse represents snapshot creation result.
type CreateSnapshotResponse struct {
	Success  bool                `json:"success"`
	Message  string              `json:"message"`
	Snapshot *AptSnapshot `json:"snapshot,omitempty"`
}

// RestoreSnapshotRequest represents a snapshot restore request.
type RestoreSnapshotRequest struct {
	SnapshotID string `json:"snapshot_id"`
}

// RestoreSnapshotResponse represents snapshot restore result.
type RestoreSnapshotResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// PackageSearchResponse represents package search results.
type PackageSearchResponse struct {
	Packages []InstalledPackage `json:"packages"`
	Count    int                       `json:"count"`
}

// InstallPackageRequest represents a package installation request.
type InstallPackageRequest struct {
	Name string `json:"name"`
}

// InstallPackageResponse represents package installation result.
type InstallPackageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Package string `json:"package"`
}

// RemovePackageRequest represents a package removal request.
type RemovePackageRequest struct {
	Name  string `json:"name"`
	Purge bool   `json:"purge"`
}

// PipxStatusResponse represents pipx availability status.
type PipxStatusResponse struct {
	Available bool                 `json:"available"`
	Packages  []PipxPackage `json:"packages,omitempty"`
	Count     int                  `json:"count"`
}

// PipxPackageRequest represents a pipx package operation request.
type PipxPackageRequest struct {
	Name string `json:"name"`
}

// PipxPackageResponse represents a pipx operation result.
type PipxPackageResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Package string `json:"package"`
}

// PipStatusResponse represents pip availability status.
type PipStatusResponse struct {
	Available bool          `json:"available"`
	Packages  []PipxPackage `json:"packages,omitempty"`
	Count     int           `json:"count"`
}

// PythonToolsStatusResponse represents combined pip + pipx status.
type PythonToolsStatusResponse struct {
	Pip  PipStatusResponse  `json:"pip"`
	Pipx PipxStatusResponse `json:"pipx"`
}

// UnattendedConfigResponse represents unattended-upgrades configuration.
type UnattendedConfigResponse struct {
	Enabled     bool `json:"enabled"`
	AutoUpdate  bool `json:"auto_update"`
	AutoUpgrade bool `json:"auto_upgrade"`
	AutoReboot  bool `json:"auto_reboot"`
	MailOnError bool `json:"mail_on_error"`
}

// ConfigureUnattendedRequest represents unattended-upgrades configuration request.
type ConfigureUnattendedRequest struct {
	Enabled    bool `json:"enabled"`
	AutoReboot bool `json:"auto_reboot"`
}

// OperationsListResponse represents a list of apt operations.
type OperationsListResponse struct {
	Operations []OperationStatusResponse `json:"operations"`
	Count      int                       `json:"count"`
}

// OperationStatusResponse represents the status of an apt operation.
type OperationStatusResponse struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Status       string   `json:"status"`
	StartedAt    string   `json:"started_at"`
	CompletedAt  *string  `json:"completed_at,omitempty"`
	ExitCode     int      `json:"exit_code"`
	Packages     []string `json:"packages,omitempty"`
	SecurityOnly bool     `json:"security_only,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// jsonError writes a JSON error response. All error responses from this gear
// MUST use this helper instead of http.Error() to ensure:
//  1. Consistent JSON format that the dashboard client can parse
//  2. No "Failed to" prefixes (the JS client adds its own user-friendly prefix)
func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// HTTP Handlers

func (p *Gear) handleStatus(w http.ResponseWriter, r *http.Request) {
	info, err := p.collector.CheckUpdates()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := UpdateStatusResponse{
		Available:        info.Available,
		TotalUpdates:     info.TotalUpdates,
		SecurityUpdates:  info.SecurityUpdates,
		RebootRequired:   info.RebootRequired,
		LastCheck:        info.LastCheck.Format("2006-01-02T15:04:05Z07:00"),
		UnattendedActive: info.UnattendedActive,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleListUpgradable(w http.ResponseWriter, r *http.Request) {
	packages, err := p.collector.ListUpgradable()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pmName := p.collector.PM().Name()
	for i := range packages {
		if packages[i].PackageURL == "" {
			packages[i].PackageURL = PackageURL(pmName, packages[i].Name)
		}
	}

	resp := PackageListResponse{
		Packages: packages,
		Count:    len(packages),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleTriggerCheck(w http.ResponseWriter, r *http.Request) {
	streaming := r.URL.Query().Get("stream") == "true"

	if streaming && p.aptRunner != nil {
		p.Logger().Info("Triggering streaming apt update check")

		operationID, err := p.aptRunner.StartCheck()
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp := StreamingInstallResponse{
			OperationID: operationID,
			Status:      "started",
			Message:     "Update check started. Watch apt.output events for progress.",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(resp)
		return
	}

	p.Logger().Info("Triggering apt update check (sync mode)")

	if err := p.collector.TriggerUpdateCheck(); err != nil {
		p.Logger().Error("apt update failed", "error", err)
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	p.handleStatus(w, r)
}

func (p *Gear) handleInstallUpdates(w http.ResponseWriter, r *http.Request) {
	var req InstallUpdatesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	streaming := r.URL.Query().Get("stream") == "true"

	if streaming && p.aptRunner != nil {
		p.Logger().Info("Starting streaming install", "security_only", req.SecurityOnly, "packages", req.Packages)

		operationID, err := p.aptRunner.StartInstall(req.SecurityOnly, req.Packages, req.CreateSnapshot)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp := StreamingInstallResponse{
			OperationID: operationID,
			Status:      "started",
			Message:     "Update installation started. Watch apt.output events for progress.",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := InstallUpdatesResponse{Success: true}

	if req.CreateSnapshot {
		snapshot, err := p.collector.CreateSnapshot("pre-update")
		if err != nil {
			p.Logger().Warn("Failed to create snapshot", "error", err)
		} else {
			resp.SnapshotID = snapshot.ID
		}
	}

	p.Logger().Info("Installing updates (sync mode)", "security_only", req.SecurityOnly, "packages", req.Packages)

	packages, err := p.collector.InstallUpdates(req.SecurityOnly, req.Packages)
	if err != nil {
		p.Logger().Error("Failed to install updates", "error", err)
		resp.Success = false
		resp.Message = err.Error()
	} else {
		resp.Packages = packages
		resp.Message = "Updates installed successfully"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	history, err := p.collector.GetUpdateHistory(limit)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := UpdateHistoryResponse{
		History: history,
		Count:   len(history),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleRebootRequired(w http.ResponseWriter, r *http.Request) {
	packages, _ := p.collector.GetRebootRequiredPackages()

	resp := RebootRequiredResponse{
		Required: len(packages) > 0,
		Packages: packages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleScheduleReboot(w http.ResponseWriter, r *http.Request) {
	var req ScheduleRebootRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.When) > 20 {
		jsonError(w, "when must be 20 characters or less", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Scheduling reboot", "when", req.When)

	if err := p.collector.ScheduleReboot(req.When); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ScheduleRebootResponse{
		Success: true,
		Message: "Reboot scheduled",
	}
	if req.When == "" {
		resp.Message = "Immediate reboot initiated"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleCancelReboot(w http.ResponseWriter, r *http.Request) {
	p.Logger().Info("Cancelling scheduled reboot")

	if err := p.collector.CancelReboot(); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ScheduleRebootResponse{
		Success: true,
		Message: "Scheduled reboot cancelled",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Snapshot handlers

func (p *Gear) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := p.collector.ListSnapshots()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := SnapshotsResponse{
		Snapshots: snapshots,
		Count:     len(snapshots),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req CreateSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Reason = "manual"
	}
	if req.Reason == "" {
		req.Reason = "manual"
	}
	if len(req.Reason) > 255 {
		jsonError(w, "reason must be 255 characters or less", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Creating package snapshot", "reason", req.Reason)

	snapshot, err := p.collector.CreateSnapshot(req.Reason)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := CreateSnapshotResponse{
		Success:  true,
		Message:  "Snapshot created",
		Snapshot: snapshot,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	var req RestoreSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.SnapshotID == "" {
		jsonError(w, "snapshot_id is required", http.StatusBadRequest)
		return
	}
	if len(req.SnapshotID) > 100 {
		jsonError(w, "snapshot_id must be 100 characters or less", http.StatusBadRequest)
		return
	}

	streaming := r.URL.Query().Get("stream") == "true"

	if streaming && p.aptRunner != nil {
		p.Logger().Warn("Starting streaming snapshot restore", "snapshot_id", req.SnapshotID)

		operationID, err := p.aptRunner.StartRestore(req.SnapshotID)
		if err != nil {
			jsonError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp := StreamingInstallResponse{
			OperationID: operationID,
			Status:      "started",
			Message:     "Snapshot restore started. Watch apt.output events for progress.",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(resp)
		return
	}

	p.Logger().Warn("Restoring package snapshot (sync mode)", "snapshot_id", req.SnapshotID)

	if err := p.collector.RestoreSnapshot(req.SnapshotID); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := RestoreSnapshotResponse{
		Success: true,
		Message: "Snapshot restored successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshotID := chi.URLParam(r, "id")
	if snapshotID == "" {
		jsonError(w, "snapshot_id is required", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Deleting package snapshot", "snapshot_id", snapshotID)

	if err := p.collector.DeleteSnapshot(snapshotID); err != nil {
		if strings.Contains(err.Error(), "snapshot not found") {
			jsonError(w, err.Error(), http.StatusNotFound)
		} else {
			jsonError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	resp := RestoreSnapshotResponse{
		Success: true,
		Message: "Snapshot deleted",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handlePreviewSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshotID := chi.URLParam(r, "id")
	if snapshotID == "" {
		jsonError(w, "snapshot_id is required", http.StatusBadRequest)
		return
	}

	preview, err := p.collector.PreviewRestore(snapshotID)
	if err != nil {
		if strings.Contains(err.Error(), "snapshot not found") {
			jsonError(w, err.Error(), http.StatusNotFound)
		} else {
			jsonError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preview)
}

// Package management handlers

func (p *Gear) handleListInstalledPackages(w http.ResponseWriter, r *http.Request) {
	packages, err := p.collector.ListInstalledPackages()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pmName := p.collector.PM().Name()

	// Cross-reference with upgradable packages to populate update fields.
	// Failures here are non-fatal — installed list is still returned.
	upgradable, _ := p.collector.ListUpgradable()
	updateMap := make(map[string]*Package, len(upgradable))
	for i := range upgradable {
		updateMap[upgradable[i].Name] = &upgradable[i]
	}

	// Cross-reference with held packages.
	heldNames, _ := p.collector.PM().ListHeldPackages()
	heldSet := make(map[string]bool, len(heldNames))
	for _, n := range heldNames {
		heldSet[n] = true
	}

	for i := range packages {
		if upd, ok := updateMap[packages[i].Name]; ok {
			packages[i].UpdateAvailable = true
			packages[i].AvailableVersion = upd.AvailableVersion
			packages[i].IsSecurityUpdate = upd.IsSecurityUpdate
		}
		if heldSet[packages[i].Name] {
			packages[i].IsHeld = true
		}
		if packages[i].PackageURL == "" {
			packages[i].PackageURL = PackageURL(pmName, packages[i].Name)
		}
	}

	resp := PackageSearchResponse{
		Packages: packages,
		Count:    len(packages),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleSearchPackages(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		jsonError(w, "query parameter is required", http.StatusBadRequest)
		return
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	packages, err := p.collector.SearchPackages(query, limit)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := PackageSearchResponse{
		Packages: packages,
		Count:    len(packages),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleInstallPackage(w http.ResponseWriter, r *http.Request) {
	var req InstallPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 2026-05 audit P2-9: surface invalid package names as a clean 400 at
	// the boundary rather than a generic 500 from the package manager.
	// isValidPackageName enforces the Debian-style name pattern AND
	// rejects leading hyphens — the latter prevents an attacker-chosen
	// "package" being passed to apt-get as a flag.
	if !isValidPackageName(req.Name) {
		jsonError(w, "invalid package name", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Installing package", "name", req.Name)

	if err := p.collector.InstallPackage(req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := InstallPackageResponse{
		Success: true,
		Message: "Package installed successfully",
		Package: req.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleRemovePackage(w http.ResponseWriter, r *http.Request) {
	var req RemovePackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// See P2-9 note on handleInstallPackage.
	if !isValidPackageName(req.Name) {
		jsonError(w, "invalid package name", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Removing package", "name", req.Name, "purge", req.Purge)

	if err := p.collector.RemovePackage(req.Name, req.Purge); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := InstallPackageResponse{
		Success: true,
		Message: "Package removed successfully",
		Package: req.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Pipx handlers

func (p *Gear) handlePipxStatus(w http.ResponseWriter, r *http.Request) {
	resp := PipxStatusResponse{
		Available: p.collector.IsPipxAvailable(),
	}

	if resp.Available {
		packages, err := p.collector.ListPipxPackages()
		if err != nil {
			p.Logger().Warn("listing pipx packages", "error", err)
		} else {
			resp.Packages = packages
			resp.Count = len(packages)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handlePipxInstall(w http.ResponseWriter, r *http.Request) {
	var req PipxPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 200 {
		jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Installing pipx package", "name", req.Name)

	if err := p.collector.InstallPipxPackage(req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := PipxPackageResponse{
		Success: true,
		Message: "Package installed successfully",
		Package: req.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handlePipxUninstall(w http.ResponseWriter, r *http.Request) {
	var req PipxPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 200 {
		jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Uninstalling pipx package", "name", req.Name)

	if err := p.collector.UninstallPipxPackage(req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := PipxPackageResponse{
		Success: true,
		Message: "Package uninstalled successfully",
		Package: req.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handlePipxUpgrade(w http.ResponseWriter, r *http.Request) {
	var req PipxPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 200 {
		jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Upgrading pipx package", "name", req.Name)

	if err := p.collector.UpgradePipxPackage(req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := PipxPackageResponse{
		Success: true,
		Message: "Package upgraded successfully",
		Package: req.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handlePipxUpgradeAll(w http.ResponseWriter, r *http.Request) {
	p.Logger().Info("Upgrading all pipx packages")

	if err := p.collector.UpgradeAllPipxPackages(); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := PipxPackageResponse{
		Success: true,
		Message: "All packages upgraded successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Pip handlers

func (p *Gear) handlePipStatus(w http.ResponseWriter, r *http.Request) {
	resp := PipStatusResponse{
		Available: p.collector.IsPipAvailable(),
	}

	if resp.Available {
		packages, err := p.collector.ListPipPackages()
		if err != nil {
			p.Logger().Warn("listing pip packages", "error", err)
		} else {
			resp.Packages = packages
			resp.Count = len(packages)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handlePipInstall(w http.ResponseWriter, r *http.Request) {
	var req PipxPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 200 {
		jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Installing pip package", "name", req.Name)

	if err := p.collector.InstallPipPackage(req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := PipxPackageResponse{
		Success: true,
		Message: "Package installed successfully",
		Package: req.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handlePipUninstall(w http.ResponseWriter, r *http.Request) {
	var req PipxPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 200 {
		jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Uninstalling pip package", "name", req.Name)

	if err := p.collector.UninstallPipPackage(req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := PipxPackageResponse{
		Success: true,
		Message: "Package uninstalled successfully",
		Package: req.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handlePipUpgrade(w http.ResponseWriter, r *http.Request) {
	var req PipxPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		jsonError(w, "package name is required", http.StatusBadRequest)
		return
	}
	if len(req.Name) > 200 {
		jsonError(w, "package name must be 200 characters or less", http.StatusBadRequest)
		return
	}

	p.Logger().Info("Upgrading pip package", "name", req.Name)

	if err := p.collector.UpgradePipPackage(req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := PipxPackageResponse{
		Success: true,
		Message: "Package upgraded successfully",
		Package: req.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handlePipUpgradeAll(w http.ResponseWriter, r *http.Request) {
	p.Logger().Info("Upgrading all pip packages")

	if err := p.collector.UpgradeAllPipPackages(); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := PipxPackageResponse{
		Success: true,
		Message: "All pip packages upgraded successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Combined Python tools handler

func (p *Gear) handlePythonToolsStatus(w http.ResponseWriter, r *http.Request) {
	resp := PythonToolsStatusResponse{}

	// Pip status
	resp.Pip.Available = p.collector.IsPipAvailable()
	if resp.Pip.Available {
		if packages, err := p.collector.ListPipPackages(); err == nil {
			resp.Pip.Packages = packages
			resp.Pip.Count = len(packages)
		} else {
			p.Logger().Warn("listing pip packages", "error", err)
		}
	}

	// Pipx status
	resp.Pipx.Available = p.collector.IsPipxAvailable()
	if resp.Pipx.Available {
		if packages, err := p.collector.ListPipxPackages(); err == nil {
			resp.Pipx.Packages = packages
			resp.Pipx.Count = len(packages)
		} else {
			p.Logger().Warn("listing pipx packages", "error", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePythonToolsVersions is the slow endpoint that fetches latest PyPI versions.
func (p *Gear) handlePythonToolsVersions(w http.ResponseWriter, r *http.Request) {
	resp := PythonToolsStatusResponse{}

	resp.Pip.Available = p.collector.IsPipAvailable()
	if resp.Pip.Available {
		if packages, err := p.collector.ListPipPackagesWithVersions(); err == nil {
			resp.Pip.Packages = packages
			resp.Pip.Count = len(packages)
		} else {
			p.Logger().Warn("listing pip packages with versions", "error", err)
		}
	}

	resp.Pipx.Available = p.collector.IsPipxAvailable()
	if resp.Pipx.Available {
		if packages, err := p.collector.ListPipxPackagesWithVersions(); err == nil {
			resp.Pipx.Packages = packages
			resp.Pipx.Count = len(packages)
		} else {
			p.Logger().Warn("listing pipx packages with versions", "error", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Unattended-upgrades handlers

func (p *Gear) handleUnattendedConfig(w http.ResponseWriter, r *http.Request) {
	config, _ := p.collector.GetUnattendedUpgradesConfig()

	resp := UnattendedConfigResponse{}
	if v, ok := config["enabled"].(bool); ok {
		resp.Enabled = v
	}
	if v, ok := config["auto_update"].(bool); ok {
		resp.AutoUpdate = v
	}
	if v, ok := config["auto_upgrade"].(bool); ok {
		resp.AutoUpgrade = v
	}
	if v, ok := config["auto_reboot"].(bool); ok {
		resp.AutoReboot = v
	}
	if v, ok := config["mail_on_error"].(bool); ok {
		resp.MailOnError = v
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleConfigureUnattended(w http.ResponseWriter, r *http.Request) {
	var req ConfigureUnattendedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	p.Logger().Info("Configuring unattended-upgrades", "enabled", req.Enabled, "auto_reboot", req.AutoReboot)

	if err := p.collector.ConfigureUnattendedUpgrades(req.Enabled, req.AutoReboot); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	p.handleUnattendedConfig(w, r)
}

// Operations handlers

func (p *Gear) handleListOperations(w http.ResponseWriter, r *http.Request) {
	if p.aptRunner == nil {
		resp := OperationsListResponse{
			Operations: []OperationStatusResponse{},
			Count:      0,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	ops := p.aptRunner.ListOperations()
	operations := make([]OperationStatusResponse, 0, len(ops))
	for _, op := range ops {
		opResp := OperationStatusResponse{
			ID:           op.ID,
			Type:         op.Type,
			Status:       op.Status,
			StartedAt:    op.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			ExitCode:     op.ExitCode,
			Packages:     op.Packages,
			SecurityOnly: op.SecurityOnly,
			Error:        op.Error,
		}
		if op.CompletedAt != nil {
			completedAt := op.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
			opResp.CompletedAt = &completedAt
		}
		operations = append(operations, opResp)
	}

	resp := OperationsListResponse{
		Operations: operations,
		Count:      len(operations),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleGetOperation(w http.ResponseWriter, r *http.Request) {
	operationID := chi.URLParam(r, "id")
	if operationID == "" {
		jsonError(w, "operation_id is required", http.StatusBadRequest)
		return
	}

	if p.aptRunner == nil {
		jsonError(w, "streaming operations not available", http.StatusNotImplemented)
		return
	}

	op, ok := p.aptRunner.GetOperation(operationID)
	if !ok {
		jsonError(w, "operation not found", http.StatusNotFound)
		return
	}

	resp := OperationStatusResponse{
		ID:           op.ID,
		Type:         op.Type,
		Status:       op.Status,
		StartedAt:    op.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
		ExitCode:     op.ExitCode,
		Packages:     op.Packages,
		SecurityOnly: op.SecurityOnly,
		Error:        op.Error,
	}

	if op.CompletedAt != nil {
		completedAt := op.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		resp.CompletedAt = &completedAt
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (p *Gear) handleCancelOperation(w http.ResponseWriter, r *http.Request) {
	operationID := chi.URLParam(r, "id")
	if operationID == "" {
		jsonError(w, "operation_id is required", http.StatusBadRequest)
		return
	}

	if p.aptRunner == nil {
		jsonError(w, "streaming operations not available", http.StatusNotImplemented)
		return
	}

	if p.aptRunner.CancelOperation(operationID) {
		p.Logger().Info("Cancelled apt operation", "operation_id", operationID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
	} else {
		jsonError(w, "operation not found or not running", http.StatusNotFound)
	}
}

func (p *Gear) handleListUpdateLogs(w http.ResponseWriter, r *http.Request) {
	if p.aptRunner == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"logs": []any{}, "count": 0})
		return
	}

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	logs, err := p.aptRunner.ListUpdateLogs(limit)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"logs": logs, "count": len(logs)})
}

func (p *Gear) handleGetUpdateLog(w http.ResponseWriter, r *http.Request) {
	logID := chi.URLParam(r, "id")
	if logID == "" {
		jsonError(w, "log id is required", http.StatusBadRequest)
		return
	}

	if p.aptRunner == nil {
		jsonError(w, "update logs not available", http.StatusNotImplemented)
		return
	}

	logEntry, err := p.aptRunner.GetUpdateLog(logID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logEntry)
}

// GetUpdatesCollector returns the updates collector for use by other components.
func (p *Gear) GetUpdatesCollector() *UpdatesCollector {
	return p.collector
}

// GetAptRunner returns the apt runner for use by other components.
func (p *Gear) GetAptRunner() *AptRunner {
	return p.aptRunner
}

// Ensure plugin implements required interfaces.
var _ gear.Gear = (*Gear)(nil)

// ── Package hold / version handlers ───────────────────────────────────────────

type holdPackageRequest struct {
	Name string `json:"name"`
}

func (p *Gear) handleHoldPackage(w http.ResponseWriter, r *http.Request) {
	var req holdPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := p.collector.PM().HoldPackage(req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "name": req.Name, "held": true})
}

func (p *Gear) handleUnholdPackage(w http.ResponseWriter, r *http.Request) {
	var req holdPackageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := p.collector.PM().UnholdPackage(req.Name); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "name": req.Name, "held": false})
}
