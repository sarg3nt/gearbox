// Package system provides system-level collectors for OS updates and package management.
package updates

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrCommandTimeout indicates a command execution timeout.
var ErrCommandTimeout = errors.New("command execution timeout")

// UpdateInfo represents the current update status of the system.
type UpdateInfo struct {
	Available        bool      `json:"available"`         // Whether update checking is available
	TotalUpdates     int       `json:"total_updates"`     // Total available updates
	SecurityUpdates  int       `json:"security_updates"`  // Security updates specifically
	RebootRequired   bool      `json:"reboot_required"`   // Whether a reboot is needed
	LastCheck        time.Time `json:"last_check"`        // When updates were last checked
	UnattendedActive bool      `json:"unattended_active"` // Whether unattended-upgrades is active
}

// Package represents an available package update.
type Package struct {
	Name             string `json:"name"`
	CurrentVersion   string `json:"current_version"`
	AvailableVersion string `json:"available_version"`
	Architecture     string `json:"architecture"`
	IsSecurityUpdate bool   `json:"is_security_update"`
	Priority         string `json:"priority"` // low, medium, high, critical
	Repository       string `json:"repository"`
	Size             int64  `json:"size_bytes"`    // Download size in bytes
	ChangelogURL     string `json:"changelog_url"` // URL to package changelog (Launchpad)
	PackageURL       string `json:"package_url,omitempty"` // Link to package info page
}

// InstalledPackage represents a manually installed package.
type InstalledPackage struct {
	Name             string    `json:"name"`
	Version          string    `json:"version"`
	Architecture     string    `json:"architecture"`
	Description      string    `json:"description,omitempty"`
	InstalledAt      time.Time `json:"installed_at,omitempty"` // May not be available
	AutoInstall      bool      `json:"auto_install"`           // Automatically installed as dependency
	Installed        bool      `json:"installed,omitempty"`    // True if currently installed (for search results)
	UpdateAvailable  bool      `json:"update_available,omitempty"`
	AvailableVersion string    `json:"available_version,omitempty"`
	IsSecurityUpdate bool      `json:"is_security_update,omitempty"`
	IsHeld           bool      `json:"is_held,omitempty"` // Package is held (pinned) by dpkg/apt-mark
	PackageURL       string    `json:"package_url,omitempty"` // Link to package info page
}


// UpdateHistoryEntry represents a past update action.
type UpdateHistoryEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	Action      string    `json:"action"` // "install", "upgrade", "remove"
	Package     string    `json:"package"`
	FromVersion string    `json:"from_version,omitempty"`
	ToVersion   string    `json:"to_version,omitempty"`
	Status      string    `json:"status"` // "success", "failed"
}

// PipxPackage represents a pipx-installed Python package.
type PipxPackage struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	LatestVersion string   `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available,omitempty"`
	PythonPath    string   `json:"python_path,omitempty"`
	Apps          []string `json:"apps,omitempty"` // Exposed applications
}

// AptSnapshot represents an APT snapshot for rollback.
type AptSnapshot struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Reason    string    `json:"reason,omitempty"`
}

// UpdatesCollector collects OS update information.
type UpdatesCollector struct {
	commandTimeout time.Duration
	pm             PackageManager // detected package manager; nil = use DetectPackageManager()
}

// NewUpdatesCollector creates a new updates collector.
func NewUpdatesCollector() *UpdatesCollector {
	return &UpdatesCollector{
		commandTimeout: 120 * time.Second,
	}
}

// PM returns the detected PackageManager, initializing it on first call.
func (c *UpdatesCollector) PM() PackageManager {
	if c.pm == nil {
		c.pm = DetectPackageManager()
	}
	return c.pm
}

// runCommand runs a command with context and timeout.
// Returns the raw combined stdout/stderr output and any error.
// Callers that discard the output on error should use runCommandWithOutput instead.
// Environment variables required by the active package manager are applied automatically.
func (c *UpdatesCollector) runCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)

	// Apply PM-specific environment variables (e.g. DEBIAN_FRONTEND=noninteractive for apt).
	if envVars := c.PM().EnvVars(); len(envVars) > 0 {
		cmd.Env = append(os.Environ(), envVars...)
	}

	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return output, ErrCommandTimeout
	}

	return output, err
}

// runCommandWithOutput runs a command and, on failure, wraps the error with the
// command's combined stdout/stderr output. This ensures error messages contain
// the actual diagnostic output (e.g. "E: The repository does not have a Release
// file") instead of just "exit status 100".
func (c *UpdatesCollector) runCommandWithOutput(name string, args ...string) ([]byte, error) {
	output, err := c.runCommand(name, args...)
	if err != nil {
		// Extract the last meaningful line(s) from the output for a concise error.
		// When we have useful output, return that alone — the bare "exit status N"
		// suffix is not useful to end users and can cause test assertions to fail.
		errDetail := extractErrorLines(output)
		if errDetail != "" {
			return output, errors.New(errDetail)
		}
		return output, err
	}
	return output, nil
}

// extractErrorLines pulls the most useful error lines from command output.
// For apt, these are lines starting with "E:" or "W:". If none found,
// returns the last non-empty line of output (trimmed to a reasonable length).
// Lines that begin with "Failed to" are excluded — those are tool-level
// prefixes (e.g. from systemctl) that the JS client re-wraps itself.
func extractErrorLines(output []byte) string {
	if len(output) == 0 {
		return ""
	}

	text := strings.TrimSpace(string(output))
	lines := strings.Split(text, "\n")

	// Collect apt error/warning lines (E: ..., W: ...)
	var errLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "E: ") || strings.HasPrefix(trimmed, "Err:") {
			errLines = append(errLines, trimmed)
		}
	}

	if len(errLines) > 0 {
		// Return up to 3 error lines joined
		if len(errLines) > 3 {
			errLines = errLines[:3]
		}
		return strings.Join(errLines, "; ")
	}

	// No apt-specific error lines; use the last non-empty line that doesn't
	// begin with "Failed to" (systemctl/systemd uses that prefix internally).
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" && !strings.HasPrefix(trimmed, "Failed to") {
			// Truncate very long lines
			if len(trimmed) > 200 {
				trimmed = trimmed[:200] + "..."
			}
			return trimmed
		}
	}

	return ""
}

// runPipxCommand runs a pipx command, ensuring HOME is set.
// When running under systemd, HOME may not be set, but pipx needs it
// to locate its venvs (e.g., /root/.local/share/pipx/venvs).
func (c *UpdatesCollector) runPipxCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pipx", args...)
	// Ensure HOME is set — systemd services don't always provide it
	if os.Getenv("HOME") == "" {
		cmd.Env = append(os.Environ(), "HOME=/root")
	}
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return output, ErrCommandTimeout
	}

	return output, err
}

// runPipxCommandWithOutput runs a pipx command and wraps failures with the
// command's output. Pipx's failure messages don't follow apt's "E:" convention,
// so we use a pipx-tuned extractor that surfaces multiple trailing lines rather
// than just the final "<long-path>/python -m pip install --upgrade pkg -q' failed"
// summary line — that line on its own is missing the actual pip diagnostic.
//
// On failure the full output is also logged via slog at Warn level so operators
// can dig into the agent log for full pip stdout/stderr; the returned error is
// shorter and dashboard-friendly.
func (c *UpdatesCollector) runPipxCommandWithOutput(args ...string) ([]byte, error) {
	output, err := c.runPipxCommand(args...)
	if err != nil {
		slog.Warn("pipx command failed",
			"args", args,
			"err", err,
			"output", strings.TrimSpace(string(output)),
		)
		errDetail := extractPipxErrorDetail(output)
		if errDetail != "" {
			return output, errors.New(errDetail)
		}
		return output, err
	}
	return output, nil
}

// extractPipxErrorDetail collects the most useful diagnostic lines from pipx
// output for surfacing in a wrapped error. Unlike apt, pipx's failure summary
// is typically the final line ("'<python> -m pip install --upgrade pkg -q' failed")
// while the cause (network error, dependency conflict, missing module, etc.)
// is on lines above it. We therefore return up to the last few non-empty lines
// joined with "; ", capped at a reasonable display length.
//
// "Failed to" lines are still skipped — systemctl/systemd uses that prefix
// internally and it tends to be noise for our callers.
func extractPipxErrorDetail(output []byte) string {
	if len(output) == 0 {
		return ""
	}

	text := strings.TrimSpace(string(output))
	if text == "" {
		return ""
	}

	const maxLines = 5
	const maxTotalLen = 500

	rawLines := strings.Split(text, "\n")
	var picked []string
	for i := len(rawLines) - 1; i >= 0 && len(picked) < maxLines; i-- {
		trimmed := strings.TrimSpace(rawLines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "Failed to") {
			continue
		}
		picked = append([]string{trimmed}, picked...)
	}

	if len(picked) == 0 {
		return ""
	}

	joined := strings.Join(picked, "; ")
	if len(joined) > maxTotalLen {
		joined = joined[:maxTotalLen] + "..."
	}
	return joined
}

// CheckUpdates retrieves the current update status.
func (c *UpdatesCollector) CheckUpdates() (*UpdateInfo, error) {
	return c.PM().CheckUpdates()
}

// ListUpgradable returns the list of packages that can be upgraded.
func (c *UpdatesCollector) ListUpgradable() ([]Package, error) {
	return c.PM().ListUpgradable()
}

// TriggerUpdateCheck triggers a fresh package cache refresh.
func (c *UpdatesCollector) TriggerUpdateCheck() error {
	return c.PM().TriggerUpdateCheck()
}

// InstallUpdates installs available updates.
// If securityOnly is true, only security updates are installed.
// If packages is non-empty, only those specific packages are upgraded.
// Returns the list of packages that were updated.
func (c *UpdatesCollector) InstallUpdates(securityOnly bool, packages []string) ([]string, error) {
	return c.PM().InstallUpdates(securityOnly, packages)
}

// GetUnattendedUpgradesConfig retrieves the current auto-update configuration.
// Kept for backwards compatibility; delegates to PM.GetAutoUpdateConfig().
func (c *UpdatesCollector) GetUnattendedUpgradesConfig() (map[string]any, error) {
	return c.PM().GetAutoUpdateConfig()
}

// ConfigureUnattendedUpgrades configures automatic updates.
// Kept for backwards compatibility; delegates to PM.ConfigureAutoUpdate().
func (c *UpdatesCollector) ConfigureUnattendedUpgrades(enabled, autoReboot bool) error {
	return c.PM().ConfigureAutoUpdate(enabled, autoReboot)
}

// GetRebootRequiredPackages returns the packages that triggered a reboot requirement.
func (c *UpdatesCollector) GetRebootRequiredPackages() ([]string, error) {
	_, pkgs, err := c.PM().GetRebootRequired()
	return pkgs, err
}

// ScheduleReboot schedules a system reboot at the specified time.
// Use empty string for immediate reboot, or formats like "02:00" for 2 AM, "+5" for 5 minutes.
func (c *UpdatesCollector) ScheduleReboot(when string) error {
	var args []string
	if when == "" {
		args = []string{"-r", "now"}
	} else if strings.HasPrefix(when, "+") {
		// Relative time in minutes
		args = []string{"-r", when}
	} else {
		// Absolute time
		args = []string{"-r", when}
	}

	_, err := c.runCommandWithOutput("shutdown", args...)
	if err != nil {
		return fmt.Errorf("failed to schedule reboot: %w", err)
	}

	return nil
}

// CancelReboot cancels a scheduled reboot.
func (c *UpdatesCollector) CancelReboot() error {
	_, err := c.runCommandWithOutput("shutdown", "-c")
	return err
}

// --- Snapshot Management ---

// validSnapshotID matches only safe snapshot IDs (timestamp + hex suffix, no path separators).
var validSnapshotID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validateSnapshotID returns an error if id is empty or contains path-unsafe characters.
func validateSnapshotID(id string) error {
	if id == "" || !validSnapshotID.MatchString(id) {
		return fmt.Errorf("invalid snapshot ID")
	}
	return nil
}

// CreateSnapshot creates a package snapshot for rollback capability.
// The snapshot format depends on the active package manager.
func (c *UpdatesCollector) CreateSnapshot(reason string) (*AptSnapshot, error) {
	return c.PM().CreateSnapshot(reason)
}

// ListSnapshots returns available APT snapshots.
func (c *UpdatesCollector) ListSnapshots() ([]AptSnapshot, error) {
	snapshotDir := "/var/lib/gearbox-agent/snapshots"

	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read snapshot directory: %w", err)
	}

	var snapshots []AptSnapshot
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".meta") {
			continue
		}

		// Validate the base snapshot ID embedded in the filename to prevent
		// path traversal if the on-disk directory is ever tampered with.
		snapshotID := strings.TrimSuffix(entry.Name(), ".meta")
		if err := validateSnapshotID(snapshotID); err != nil {
			continue
		}

		metaFile := fmt.Sprintf("%s/%s", snapshotDir, entry.Name())
		data, err := os.ReadFile(metaFile)
		if err != nil {
			continue
		}

		snapshot := AptSnapshot{
			ID: snapshotID,
		}

		// Parse metadata
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			switch parts[0] {
			case "timestamp":
				if t, err := time.Parse(time.RFC3339, parts[1]); err == nil {
					snapshot.CreatedAt = t
				}
			case "reason":
				snapshot.Reason = parts[1]
			}
		}

		snapshots = append(snapshots, snapshot)
	}

	// Sort newest first
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt.After(snapshots[j].CreatedAt)
	})

	return snapshots, nil
}

// RestoreSnapshot restores packages to a previous snapshot state.
// This is a potentially dangerous operation; use AptRunner.StartRestore for streaming.
func (c *UpdatesCollector) RestoreSnapshot(snapshotID string) error {
	return c.PM().RestoreSnapshot(snapshotID)
}

// computeDowngrades compares a snapshot's .versions file against currently
// installed packages and returns a list of "package=version" strings for
// packages that need to be downgraded.
func computeDowngrades(versionsFile string) []string {
	data, err := os.ReadFile(versionsFile) //#nosec G304
	if err != nil {
		return nil
	}

	// Build map of snapshot versions: package -> version
	snapshotVersions := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			snapshotVersions[parts[0]] = parts[1]
		}
	}

	// Get current installed versions
	cmd := exec.Command("dpkg-query", "-W", "-f", "${Package}\t${Version}\n")
	currentOutput, err := cmd.Output()
	if err != nil {
		return nil
	}

	currentVersions := make(map[string]string)
	scanner = bufio.NewScanner(strings.NewReader(string(currentOutput)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			currentVersions[parts[0]] = parts[1]
		}
	}

	// Find packages that need downgrading
	var downgrades []string
	for pkg, snapshotVer := range snapshotVersions {
		currentVer, exists := currentVersions[pkg]
		if !exists {
			continue // package not currently installed, dselect-upgrade handles this
		}
		if currentVer != snapshotVer {
			downgrades = append(downgrades, pkg+"="+snapshotVer)
		}
	}

	return downgrades
}

// DeleteSnapshot removes a snapshot.
func (c *UpdatesCollector) DeleteSnapshot(snapshotID string) error {
	if err := validateSnapshotID(snapshotID); err != nil {
		return err
	}
	snapshotDir := "/var/lib/gearbox-agent/snapshots"

	selectionsPath := fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID)
	metaPath := fmt.Sprintf("%s/%s.meta", snapshotDir, snapshotID)
	versionsPath := fmt.Sprintf("%s/%s.versions", snapshotDir, snapshotID)

	// Check that the snapshot exists before attempting removal
	_, selErr := os.Stat(selectionsPath)
	_, metaErr := os.Stat(metaPath)
	if os.IsNotExist(selErr) && os.IsNotExist(metaErr) {
		return fmt.Errorf("snapshot not found: %s", snapshotID)
	}

	// Remove all snapshot files, ignoring errors for files that don't exist
	os.Remove(selectionsPath)
	os.Remove(metaPath)
	os.Remove(versionsPath)

	return nil
}

// PackageChange represents a version change for a single package.
type PackageChange struct {
	Package        string `json:"package"`
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
}

// SnapshotPreview represents the changes that would be applied by restoring a snapshot.
type SnapshotPreview struct {
	SnapshotID   string          `json:"snapshot_id"`
	HasVersions  bool            `json:"has_versions"`
	Downgrades   []PackageChange `json:"downgrades"`
	Installs     []string        `json:"installs"`
	Removals     []string        `json:"removals"`
	TotalChanges int             `json:"total_changes"`
}

// PreviewRestore computes what changes restoring a snapshot would make without applying them.
func (c *UpdatesCollector) PreviewRestore(snapshotID string) (*SnapshotPreview, error) {
	if err := validateSnapshotID(snapshotID); err != nil {
		return nil, err
	}
	snapshotDir := "/var/lib/gearbox-agent/snapshots"
	selectionsFile := fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID)
	versionsFile := fmt.Sprintf("%s/%s.versions", snapshotDir, snapshotID)

	// Verify snapshot exists
	if _, err := os.Stat(selectionsFile); err != nil {
		return nil, fmt.Errorf("snapshot not found: %s", snapshotID)
	}

	preview := &SnapshotPreview{
		SnapshotID: snapshotID,
	}

	// Compare selections to find installs/removals
	snapshotSelections, err := os.ReadFile(selectionsFile) //#nosec G304
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot selections: %w", err)
	}

	// Parse snapshot selections: "package\tinstall" or "package\tdeinstall"
	snapshotState := make(map[string]string) // package -> "install"/"deinstall"
	scanner := bufio.NewScanner(strings.NewReader(string(snapshotSelections)))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) == 2 {
			snapshotState[parts[0]] = parts[1]
		}
	}

	// Get current selections
	currentOutput, err := c.runCommand("dpkg", "--get-selections")
	if err != nil {
		return nil, fmt.Errorf("failed to get current selections: %w", err)
	}

	currentState := make(map[string]string)
	scanner = bufio.NewScanner(strings.NewReader(string(currentOutput)))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) == 2 {
			currentState[parts[0]] = parts[1]
		}
	}

	// Find packages that would be installed (in snapshot but not currently, or marked deinstall currently)
	for pkg, snapState := range snapshotState {
		curState, exists := currentState[pkg]
		if snapState == "install" && (!exists || curState == "deinstall" || curState == "purge") {
			preview.Installs = append(preview.Installs, pkg)
		}
	}

	// Find packages that would be removed (currently installed but snapshot has deinstall/purge)
	for pkg, curState := range currentState {
		if curState != "install" {
			continue
		}
		snapState, exists := snapshotState[pkg]
		if exists && (snapState == "deinstall" || snapState == "purge") {
			preview.Removals = append(preview.Removals, pkg)
		}
	}

	// Check for version-aware downgrades
	if _, err := os.Stat(versionsFile); err == nil {
		preview.HasVersions = true

		// Read snapshot versions
		versionsData, err := os.ReadFile(versionsFile) //#nosec G304
		if err == nil {
			snapshotVersions := make(map[string]string)
			scanner = bufio.NewScanner(strings.NewReader(string(versionsData)))
			for scanner.Scan() {
				parts := strings.SplitN(scanner.Text(), "\t", 2)
				if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
					snapshotVersions[parts[0]] = parts[1]
				}
			}

			// Get current versions
			currentVersionsOutput, err := c.runCommand("dpkg-query", "-W", "-f", "${Package}\t${Version}\n")
			if err == nil {
				currentVersions := make(map[string]string)
				scanner = bufio.NewScanner(strings.NewReader(string(currentVersionsOutput)))
				for scanner.Scan() {
					parts := strings.SplitN(scanner.Text(), "\t", 2)
					if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
						currentVersions[parts[0]] = parts[1]
					}
				}

				for pkg, snapVer := range snapshotVersions {
					curVer, exists := currentVersions[pkg]
					if exists && curVer != snapVer {
						preview.Downgrades = append(preview.Downgrades, PackageChange{
							Package:        pkg,
							CurrentVersion: curVer,
							TargetVersion:  snapVer,
						})
					}
				}
			}
		}
	}

	sort.Strings(preview.Installs)
	sort.Strings(preview.Removals)
	sort.Slice(preview.Downgrades, func(i, j int) bool {
		return preview.Downgrades[i].Package < preview.Downgrades[j].Package
	})

	preview.TotalChanges = len(preview.Downgrades) + len(preview.Installs) + len(preview.Removals)
	return preview, nil
}

// --- Update History ---

// GetUpdateHistory returns recent package update history from the package manager logs.
func (c *UpdatesCollector) GetUpdateHistory(limit int) ([]UpdateHistoryEntry, error) {
	return c.PM().GetUpdateHistory(limit)
}


// --- Package Search ---

// ListInstalledPackages returns all installed packages.
func (c *UpdatesCollector) ListInstalledPackages() ([]InstalledPackage, error) {
	return c.PM().ListInstalledPackages()
}

// SearchPackages searches for packages matching a query.
func (c *UpdatesCollector) SearchPackages(query string, limit int) ([]InstalledPackage, error) {
	return c.PM().SearchPackages(query, limit)
}

// InstallPackage installs a new package.
func (c *UpdatesCollector) InstallPackage(name string) error {
	return c.PM().InstallPackage(name)
}

// RemovePackage removes a package.
func (c *UpdatesCollector) RemovePackage(name string, purge bool) error {
	return c.PM().RemovePackage(name, purge)
}

// --- Pipx Package Management ---

// IsPipxAvailable checks if pipx is installed.
func (c *UpdatesCollector) IsPipxAvailable() bool {
	_, err := exec.LookPath("pipx")
	return err == nil
}

// getLatestPyPIVersion returns the latest version of a package from the PyPI JSON API.
// Returns empty string if the check fails (non-fatal).
func (c *UpdatesCollector) getLatestPyPIVersion(pkgName string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://pypi.org/pypi/"+pkgName+"/json", nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()
	var result struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return ""
	}
	return result.Info.Version
}

// ListPipxPackages returns installed pipx packages (fast, no version check).
func (c *UpdatesCollector) ListPipxPackages() ([]PipxPackage, error) {
	if !c.IsPipxAvailable() {
		return nil, fmt.Errorf("pipx is not installed")
	}

	output, err := c.runPipxCommand("list", "--json")
	if err != nil {
		slog.Warn("pipx list --json failed, trying plain list",
			"error", err,
			"output", strings.TrimSpace(string(output)))
		// Fallback to plain list if JSON fails
		output, err = c.runPipxCommand("list")
		if err != nil {
			return nil, fmt.Errorf("failed to list pipx packages (output: %s): %w", strings.TrimSpace(string(output)), err)
		}
		return c.parsePipxListPlain(string(output)), nil
	}

	return c.parsePipxListJSON(string(output))
}

// ListPipxPackagesWithVersions returns installed pipx packages with latest PyPI version info (slow).
func (c *UpdatesCollector) ListPipxPackagesWithVersions() ([]PipxPackage, error) {
	packages, err := c.ListPipxPackages()
	if err != nil {
		return nil, err
	}
	for i := range packages {
		latest := c.getLatestPyPIVersion(packages[i].Name)
		if latest != "" {
			packages[i].LatestVersion = latest
			packages[i].UpdateAvailable = latest != packages[i].Version
		}
	}
	return packages, nil
}

// parsePipxListPlain parses plain text pipx list output.
// Example format:
//
//	venvs are in /root/.local/share/pipx/venvs
//	apps are exposed on your $PATH at /root/.local/bin
//	manual pages are exposed at /root/.local/share/man
//	   package certbot 5.2.2, installed using Python 3.13.7
//	    - certbot
func (c *UpdatesCollector) parsePipxListPlain(output string) []PipxPackage {
	var packages []PipxPackage
	var currentPackage *PipxPackage

	// Pattern for package line: "   package <name> <version>, installed using Python x.x.x"
	packageRe := regexp.MustCompile(`^\s+package\s+(\S+)\s+(\S+),`)
	// Pattern for app line: "    - <app_name>"
	appRe := regexp.MustCompile(`^\s+-\s+(\S+)`)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		// Check for package line
		if matches := packageRe.FindStringSubmatch(line); len(matches) >= 3 {
			// Save previous package if exists
			if currentPackage != nil {
				packages = append(packages, *currentPackage)
			}
			currentPackage = &PipxPackage{
				Name:    matches[1],
				Version: matches[2],
			}
			continue
		}

		// Check for app line
		if matches := appRe.FindStringSubmatch(line); len(matches) >= 2 && currentPackage != nil {
			currentPackage.Apps = append(currentPackage.Apps, matches[1])
		}
	}

	// Don't forget the last package
	if currentPackage != nil {
		packages = append(packages, *currentPackage)
	}

	return packages
}

// parsePipxListJSON parses JSON pipx list output (pipx >= 1.0).
// The JSON format has a top-level "venvs" object keyed by package name,
// each containing metadata.main_package with package info and app_paths.
func (c *UpdatesCollector) parsePipxListJSON(output string) ([]PipxPackage, error) {
	var result struct {
		Venvs map[string]struct {
			Metadata struct {
				MainPackage struct {
					Package    string `json:"package"`
					PackageURL string `json:"package_or_url"`
					Version    string `json:"package_version"`
					AppPaths   []struct {
						Name string `json:"__Path__"`
					} `json:"app_paths"`
					Apps []string `json:"apps"`
				} `json:"main_package"`
			} `json:"metadata"`
		} `json:"venvs"`
	}

	if err := json.Unmarshal([]byte(output), &result); err != nil {
		// Fall back to plain text parsing if JSON structure is unexpected
		return c.parsePipxListPlain(output), nil
	}

	var packages []PipxPackage
	for name, venv := range result.Venvs {
		pkg := PipxPackage{
			Name:    name,
			Version: venv.Metadata.MainPackage.Version,
		}

		// Collect app names from the apps field
		if len(venv.Metadata.MainPackage.Apps) > 0 {
			pkg.Apps = venv.Metadata.MainPackage.Apps
		}

		packages = append(packages, pkg)
	}

	return packages, nil
}

// SearchPyPI searches PyPI for packages (basic implementation).
func (c *UpdatesCollector) SearchPyPI(query string, _ int) ([]PipxPackage, error) {
	// Use pip index versions for basic search (limited but works without external API)
	// For full search, we'd need to query pypi.org/pypi API
	output, err := c.runCommand("pip", "index", "versions", query)
	if err != nil {
		// pip index may not be available, return empty result
		return nil, nil
	}

	var packages []PipxPackage

	// Parse output: "package_name (version1, version2, ...)"
	re := regexp.MustCompile(`^(\S+)\s+\(([^)]+)\)`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) >= 3 {
		versions := strings.Split(matches[2], ", ")
		if len(versions) > 0 {
			packages = append(packages, PipxPackage{
				Name:    matches[1],
				Version: versions[0], // Latest version
			})
		}
	}

	return packages, nil
}

// InstallPipxPackage installs a package via pipx.
func (c *UpdatesCollector) InstallPipxPackage(name string) error {
	if !c.IsPipxAvailable() {
		return fmt.Errorf("pipx is not installed")
	}

	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}

	_, err := c.runPipxCommandWithOutput("install", name)
	if err != nil {
		return fmt.Errorf("failed to install pipx package %s: %w", name, err)
	}

	return nil
}

// UninstallPipxPackage uninstalls a package via pipx.
func (c *UpdatesCollector) UninstallPipxPackage(name string) error {
	if !c.IsPipxAvailable() {
		return fmt.Errorf("pipx is not installed")
	}

	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}

	_, err := c.runPipxCommandWithOutput("uninstall", name)
	if err != nil {
		return fmt.Errorf("failed to uninstall pipx package %s: %w", name, err)
	}

	return nil
}

// UpgradePipxPackage upgrades a pipx package.
func (c *UpdatesCollector) UpgradePipxPackage(name string) error {
	if !c.IsPipxAvailable() {
		return fmt.Errorf("pipx is not installed")
	}

	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}

	_, err := c.runPipxCommandWithOutput("upgrade", name)
	if err != nil {
		return fmt.Errorf("failed to upgrade pipx package %s: %w", name, err)
	}

	return nil
}

// UpgradeAllPipxPackages upgrades all pipx packages.
func (c *UpdatesCollector) UpgradeAllPipxPackages() error {
	if !c.IsPipxAvailable() {
		return fmt.Errorf("pipx is not installed")
	}

	_, err := c.runPipxCommandWithOutput("upgrade-all")
	if err != nil {
		return fmt.Errorf("failed to upgrade all pipx packages: %w", err)
	}

	return nil
}

// --- Pip Package Management ---

// pipCommand returns the pip executable name available on this system.
// Prefers pip3 over pip.
func pipCommand() string {
	if _, err := exec.LookPath("pip3"); err == nil {
		return "pip3"
	}
	return "pip"
}

// runPipCommand runs a pip command with a timeout.
func (c *UpdatesCollector) runPipCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, pipCommand(), args...)
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return output, ErrCommandTimeout
	}

	return output, err
}

// runPipCommandWithOutput runs a pip command and wraps failures with
// the command's output, just like runCommandWithOutput does for regular commands.
func (c *UpdatesCollector) runPipCommandWithOutput(args ...string) ([]byte, error) {
	output, err := c.runPipCommand(args...)
	if err != nil {
		errDetail := extractErrorLines(output)
		if errDetail != "" {
			return output, errors.New(errDetail)
		}
		return output, err
	}
	return output, nil
}

// IsPipAvailable checks if pip is installed.
func (c *UpdatesCollector) IsPipAvailable() bool {
	_, err := exec.LookPath("pip3")
	if err != nil {
		_, err = exec.LookPath("pip")
	}
	return err == nil
}

// ListPipPackages returns user-installed pip packages (fast, no version check).
func (c *UpdatesCollector) ListPipPackages() ([]PipxPackage, error) {
	if !c.IsPipAvailable() {
		return nil, fmt.Errorf("pip is not installed")
	}

	output, err := c.runPipCommand("list", "--user", "--format=json")
	if err != nil {
		return nil, fmt.Errorf("listing pip packages: %w", err)
	}

	var pipList []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(output, &pipList); err != nil {
		return nil, fmt.Errorf("parsing pip list output: %w", err)
	}

	var packages []PipxPackage
	for _, p := range pipList {
		packages = append(packages, PipxPackage{Name: p.Name, Version: p.Version})
	}
	return packages, nil
}

// ListPipPackagesWithVersions returns user-installed pip packages with latest version info (slow).
func (c *UpdatesCollector) ListPipPackagesWithVersions() ([]PipxPackage, error) {
	packages, err := c.ListPipPackages()
	if err != nil {
		return nil, err
	}

	// Build outdated map via pip list --outdated (one network call, best-effort)
	outdatedMap := map[string]string{}
	outdatedOutput, err := c.runPipCommand("list", "--user", "--outdated", "--format=json")
	if err == nil {
		var outdatedList []struct {
			Name   string `json:"name"`
			Latest string `json:"latest_version"`
		}
		if json.Unmarshal(outdatedOutput, &outdatedList) == nil {
			for _, o := range outdatedList {
				outdatedMap[strings.ToLower(o.Name)] = o.Latest
			}
		}
	}

	for i := range packages {
		if latest, ok := outdatedMap[strings.ToLower(packages[i].Name)]; ok {
			packages[i].LatestVersion = latest
			packages[i].UpdateAvailable = true
		}
	}
	return packages, nil
}

// InstallPipPackage installs a package via pip.
func (c *UpdatesCollector) InstallPipPackage(name string) error {
	if !c.IsPipAvailable() {
		return fmt.Errorf("pip is not installed")
	}

	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}

	_, err := c.runPipCommandWithOutput("install", "--user", name)
	return err
}

// UninstallPipPackage uninstalls a package via pip.
func (c *UpdatesCollector) UninstallPipPackage(name string) error {
	if !c.IsPipAvailable() {
		return fmt.Errorf("pip is not installed")
	}

	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}

	_, err := c.runPipCommandWithOutput("uninstall", "-y", name)
	return err
}

// UpgradePipPackage upgrades a pip package.
func (c *UpdatesCollector) UpgradePipPackage(name string) error {
	if !c.IsPipAvailable() {
		return fmt.Errorf("pip is not installed")
	}

	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}

	_, err := c.runPipCommandWithOutput("install", "--user", "--upgrade", name)
	return err
}

// UpgradeAllPipPackages upgrades all user-installed pip packages.
func (c *UpdatesCollector) UpgradeAllPipPackages() error {
	if !c.IsPipAvailable() {
		return fmt.Errorf("pip is not installed")
	}

	// Get list of outdated packages
	output, err := c.runPipCommand("list", "--user", "--outdated", "--format=json")
	if err != nil {
		return fmt.Errorf("listing outdated packages: %w", err)
	}

	var outdated []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(output, &outdated); err != nil {
		return fmt.Errorf("parsing outdated list: %w", err)
	}

	for _, pkg := range outdated {
		if _, err := c.runPipCommandWithOutput("install", "--user", "--upgrade", pkg.Name); err != nil {
			return fmt.Errorf("upgrading %s: %w", pkg.Name, err)
		}
	}
	return nil
}

// --- Helpers ---

// generateChangelogURL generates the Launchpad changelog URL for an Ubuntu package.
func generateChangelogURL(packageName string) string {
	// Launchpad changelog URL format: https://launchpad.net/ubuntu/+source/<package>/+changelog
	return "https://launchpad.net/ubuntu/+source/" + packageName + "/+changelog"
}

// isValidPackageName validates a package name to prevent command injection.
func isValidPackageName(name string) bool {
	if name == "" || len(name) > 256 {
		return false
	}

	// Package names can contain: alphanumeric, +, -, ., _
	// They cannot start with - or .
	if name[0] == '-' || name[0] == '.' {
		return false
	}

	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.' || c == '_') {
			return false
		}
	}

	return true
}

// GetPackageInfo retrieves detailed information about a specific package.
func (c *UpdatesCollector) GetPackageInfo(name string) (*InstalledPackage, error) {
	if !isValidPackageName(name) {
		return nil, fmt.Errorf("invalid package name: %s", name)
	}

	output, err := c.runCommand("dpkg", "-s", name)
	if err != nil {
		return nil, fmt.Errorf("package not installed: %s", name)
	}

	pkg := &InstalledPackage{Name: name}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Version: ") {
			pkg.Version = strings.TrimPrefix(line, "Version: ")
		} else if strings.HasPrefix(line, "Architecture: ") {
			pkg.Architecture = strings.TrimPrefix(line, "Architecture: ")
		}
	}

	return pkg, nil
}

// GetDiskUsageForUpdates estimates disk space needed for pending updates.
func (c *UpdatesCollector) GetDiskUsageForUpdates() (int64, error) {
	output, err := c.runCommand("apt", "upgrade", "--dry-run")
	if err != nil {
		return 0, fmt.Errorf("failed to simulate upgrade: %w", err)
	}

	// Parse "After this operation, X MB of additional disk space will be used."
	re := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(kB|MB|GB)\s+of additional disk space`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) >= 3 {
		value, _ := strconv.ParseFloat(matches[1], 64)
		switch matches[2] {
		case "GB":
			return int64(value * 1024 * 1024 * 1024), nil
		case "MB":
			return int64(value * 1024 * 1024), nil
		case "kB":
			return int64(value * 1024), nil
		}
	}

	return 0, nil
}
