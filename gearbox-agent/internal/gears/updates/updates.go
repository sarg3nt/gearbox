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
}

// InstalledPackage represents a manually installed package.
type InstalledPackage struct {
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	Architecture string    `json:"architecture"`
	Description  string    `json:"description,omitempty"`
	InstalledAt  time.Time `json:"installed_at,omitempty"` // May not be available
	AutoInstall  bool      `json:"auto_install"`           // Automatically installed as dependency
	Installed    bool      `json:"installed,omitempty"`    // True if currently installed (for search results)
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
}

// NewUpdatesCollector creates a new updates collector.
func NewUpdatesCollector() *UpdatesCollector {
	return &UpdatesCollector{
		commandTimeout: 120 * time.Second, // 2 minute timeout for apt operations
	}
}

// runCommand runs a command with context and timeout.
// Returns the raw combined stdout/stderr output and any error.
// Callers that discard the output on error should use runCommandWithOutput instead.
// For apt/apt-get commands, DEBIAN_FRONTEND=noninteractive is set automatically.
func (c *UpdatesCollector) runCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)

	// Set noninteractive frontend for apt commands to prevent prompts and
	// ensure consistent behavior when running under systemd.
	if name == "apt" || name == "apt-get" || name == "apt-cache" {
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
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
		// Extract the last meaningful line(s) from the output for a concise error
		errDetail := extractErrorLines(output)
		if errDetail != "" {
			return output, fmt.Errorf("%s: %w", errDetail, err)
		}
		return output, err
	}
	return output, nil
}

// extractErrorLines pulls the most useful error lines from command output.
// For apt, these are lines starting with "E:" or "W:". If none found,
// returns the last non-empty line of output (trimmed to a reasonable length).
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

	// No apt-specific error lines; use the last non-empty line
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
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
// command's output, just like runCommandWithOutput does for regular commands.
func (c *UpdatesCollector) runPipxCommandWithOutput(args ...string) ([]byte, error) {
	output, err := c.runPipxCommand(args...)
	if err != nil {
		errDetail := extractErrorLines(output)
		if errDetail != "" {
			return output, fmt.Errorf("%s: %w", errDetail, err)
		}
		return output, err
	}
	return output, nil
}

// CheckUpdates retrieves the current update status.
func (c *UpdatesCollector) CheckUpdates() (*UpdateInfo, error) {
	info := &UpdateInfo{
		Available: true,
		LastCheck: time.Now(),
	}

	// Check if apt is available
	if _, err := exec.LookPath("apt"); err != nil {
		info.Available = false
		return info, nil
	}

	// Get list of upgradable packages
	output, err := c.runCommand("apt", "list", "--upgradable")
	if err != nil {
		// Not a fatal error - might just be no updates
		info.TotalUpdates = 0
	} else {
		packages := c.parseAptListOutput(string(output))
		info.TotalUpdates = len(packages)

		// Count security updates
		for _, pkg := range packages {
			if pkg.IsSecurityUpdate {
				info.SecurityUpdates++
			}
		}
	}

	// Check if reboot is required
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		info.RebootRequired = true
	}

	// Check if unattended-upgrades is active
	info.UnattendedActive = c.isUnattendedUpgradesActive()

	return info, nil
}

// ListUpgradable returns the list of packages that can be upgraded.
func (c *UpdatesCollector) ListUpgradable() ([]Package, error) {
	output, err := c.runCommand("apt", "list", "--upgradable")
	if err != nil {
		return nil, fmt.Errorf("failed to list upgradable packages: %w", err)
	}

	packages := c.parseAptListOutput(string(output))

	// Fetch sizes for all packages in one batch using apt-cache show
	c.fetchPackageSizes(packages)

	return packages, nil
}

// fetchPackageSizes fetches download sizes for packages using apt-cache show.
func (c *UpdatesCollector) fetchPackageSizes(packages []Package) {
	if len(packages) == 0 {
		return
	}

	// Build list of package names with versions
	var packageSpecs []string
	for _, pkg := range packages {
		packageSpecs = append(packageSpecs, pkg.Name+"="+pkg.AvailableVersion)
	}

	// Run apt-cache show for all packages at once
	args := append([]string{"show"}, packageSpecs...)
	output, err := c.runCommand("apt-cache", args...)
	if err != nil {
		// Non-fatal, sizes will remain 0
		return
	}

	// Parse the output to extract sizes
	// Format: "Size: 12345" (in bytes)
	sizeMap := make(map[string]int64)
	var currentPackage string

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Package: ") {
			currentPackage = strings.TrimPrefix(line, "Package: ")
		} else if strings.HasPrefix(line, "Size: ") && currentPackage != "" {
			sizeStr := strings.TrimPrefix(line, "Size: ")
			if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
				sizeMap[currentPackage] = size
			}
		}
	}

	// Update package sizes
	for i := range packages {
		if size, ok := sizeMap[packages[i].Name]; ok {
			packages[i].Size = size
		}
	}
}

// parseAptListOutput parses output from 'apt list --upgradable'.
// Format: package/repo version arch [upgradable from: old_version]
func (c *UpdatesCollector) parseAptListOutput(output string) []Package {
	var packages []Package

	// Pattern: name/repo version arch [upgradable from: old_version]
	re := regexp.MustCompile(`^([^/]+)/([^\s]+)\s+([^\s]+)\s+([^\s]+)\s+\[upgradable from:\s+([^\]]+)\]`)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "Listing...") {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if len(matches) >= 6 {
			pkg := Package{
				Name:             matches[1],
				Repository:       matches[2],
				AvailableVersion: matches[3],
				Architecture:     matches[4],
				CurrentVersion:   matches[5],
				ChangelogURL:     generateChangelogURL(matches[1]),
			}

			// Check if it's a security update
			if strings.Contains(matches[2], "security") {
				pkg.IsSecurityUpdate = true
				pkg.Priority = "high"
			} else {
				pkg.Priority = "medium"
			}

			packages = append(packages, pkg)
		}
	}

	return packages
}

// TriggerUpdateCheck triggers a fresh 'apt-get update' to refresh package lists.
func (c *UpdatesCollector) TriggerUpdateCheck() error {
	_, err := c.runCommandWithOutput("apt-get", "update")
	if err != nil {
		return fmt.Errorf("apt update failed: %w", err)
	}
	return nil
}

// InstallUpdates installs available updates.
// If securityOnly is true, only security updates are installed.
// If packages is non-empty, only those specific packages are upgraded.
// Returns the list of packages that were updated.
func (c *UpdatesCollector) InstallUpdates(securityOnly bool, packages []string) ([]string, error) {
	var args []string

	if len(packages) > 0 {
		// Install specific packages
		args = append([]string{"install", "-y"}, packages...)
	} else if securityOnly {
		// Use unattended-upgrade for security-only updates
		output, err := c.runCommand("unattended-upgrade", "--dry-run", "-d")
		if err != nil {
			return nil, fmt.Errorf("failed to check security updates: %w", err)
		}

		// Parse which packages would be upgraded
		securityPackages := c.parseUnattendedUpgradeOutput(string(output))
		if len(securityPackages) == 0 {
			return nil, nil // No security updates
		}

		// Actually install them
		_, err = c.runCommand("unattended-upgrade")
		if err != nil {
			return nil, fmt.Errorf("failed to install security updates: %w", err)
		}

		return securityPackages, nil
	} else {
		// Full upgrade
		args = []string{"upgrade", "-y"}
	}

	_, err := c.runCommandWithOutput("apt-get", args...)
	if err != nil {
		return nil, fmt.Errorf("apt upgrade failed: %w", err)
	}

	// Return list of upgraded packages (we'd need to compare before/after for accuracy)
	return packages, nil
}

// parseUnattendedUpgradeOutput extracts package names from unattended-upgrade dry-run output.
func (c *UpdatesCollector) parseUnattendedUpgradeOutput(output string) []string {
	var packages []string

	// Look for lines like "Packages that will be upgraded:"
	inList := false
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Packages that will be upgraded:") ||
			strings.Contains(line, "Packages that are upgraded:") {
			inList = true
			continue
		}
		if inList {
			// Package names are listed on subsequent lines
			if strings.TrimSpace(line) == "" {
				break
			}
			packages = append(packages, strings.Fields(line)...)
		}
	}

	return packages
}

// isUnattendedUpgradesActive checks if unattended-upgrades is running.
func (c *UpdatesCollector) isUnattendedUpgradesActive() bool {
	err := exec.Command("systemctl", "is-active", "--quiet", "unattended-upgrades").Run()
	return err == nil
}

// GetUnattendedUpgradesConfig retrieves the current unattended-upgrades configuration.
func (c *UpdatesCollector) GetUnattendedUpgradesConfig() (map[string]any, error) {
	config := make(map[string]any)

	// Check if enabled
	config["enabled"] = c.isUnattendedUpgradesActive()

	// Read auto-update config
	autoUpdateConfig := "/etc/apt/apt.conf.d/20auto-upgrades"
	if data, err := os.ReadFile(autoUpdateConfig); err == nil {
		content := string(data)
		config["auto_update"] = strings.Contains(content, `APT::Periodic::Update-Package-Lists "1"`)
		config["auto_upgrade"] = strings.Contains(content, `APT::Periodic::Unattended-Upgrade "1"`)
	}

	// Read unattended-upgrades config
	unattendedConfig := "/etc/apt/apt.conf.d/50unattended-upgrades"
	if data, err := os.ReadFile(unattendedConfig); err == nil {
		content := string(data)
		config["auto_reboot"] = strings.Contains(content, `Unattended-Upgrade::Automatic-Reboot "true"`)
		config["mail_on_error"] = strings.Contains(content, `Unattended-Upgrade::Mail`)
	}

	return config, nil
}

// ConfigureUnattendedUpgrades configures automatic security updates.
func (c *UpdatesCollector) ConfigureUnattendedUpgrades(enabled, autoReboot bool) error {
	if !enabled {
		// Disable unattended-upgrades
		_, err := c.runCommandWithOutput("systemctl", "disable", "--now", "unattended-upgrades")
		return err
	}

	// Enable unattended-upgrades
	_, err := c.runCommandWithOutput("systemctl", "enable", "--now", "unattended-upgrades")
	if err != nil {
		return fmt.Errorf("failed to enable unattended-upgrades: %w", err)
	}

	// Configure auto-upgrades
	autoUpdateContent := `APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
`
	err = os.WriteFile("/etc/apt/apt.conf.d/20auto-upgrades", []byte(autoUpdateContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write auto-upgrades config: %w", err)
	}

	return nil
}

// GetRebootRequiredPackages returns the packages that triggered a reboot requirement.
func (c *UpdatesCollector) GetRebootRequiredPackages() ([]string, error) {
	data, err := os.ReadFile("/var/run/reboot-required.pkgs")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read reboot-required.pkgs: %w", err)
	}

	var packages []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			packages = append(packages, line)
		}
	}

	return packages, nil
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

// --- APT Snapshot Management ---

// CreateSnapshot creates an APT snapshot for rollback capability.
func (c *UpdatesCollector) CreateSnapshot(reason string) (*AptSnapshot, error) {
	timestamp := time.Now()
	snapshotID := timestamp.Format("20060102-150405")
	snapshotDir := "/var/lib/gearbox-agent/snapshots"

	// Ensure snapshot directory exists
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	// Save current package selections
	output, err := c.runCommand("dpkg", "--get-selections")
	if err != nil {
		return nil, fmt.Errorf("failed to get package selections: %w", err)
	}

	snapshotFile := fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID)
	if err := os.WriteFile(snapshotFile, output, 0644); err != nil {
		return nil, fmt.Errorf("failed to save snapshot: %w", err)
	}

	// Save installed package versions for version-aware restore.
	// Format: "package\tversion\n" for each installed package.
	versionsOutput, err := c.runCommand("dpkg-query", "-W", "-f", "${Package}\t${Version}\n")
	if err == nil {
		versionsFile := fmt.Sprintf("%s/%s.versions", snapshotDir, snapshotID)
		_ = os.WriteFile(versionsFile, versionsOutput, 0644)
	}

	// Save metadata
	metaContent := fmt.Sprintf("timestamp=%s\nreason=%s\n", timestamp.Format(time.RFC3339), reason)
	metaFile := fmt.Sprintf("%s/%s.meta", snapshotDir, snapshotID)
	if err := os.WriteFile(metaFile, []byte(metaContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to save snapshot metadata: %w", err)
	}

	return &AptSnapshot{
		ID:        snapshotID,
		CreatedAt: timestamp,
		Reason:    reason,
	}, nil
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

		metaFile := fmt.Sprintf("%s/%s", snapshotDir, entry.Name())
		data, err := os.ReadFile(metaFile)
		if err != nil {
			continue
		}

		snapshot := AptSnapshot{
			ID: strings.TrimSuffix(entry.Name(), ".meta"),
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
// This is a potentially dangerous operation.
func (c *UpdatesCollector) RestoreSnapshot(snapshotID string) error {
	snapshotDir := "/var/lib/gearbox-agent/snapshots"
	snapshotFile := fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID)

	// Verify snapshot exists
	if _, err := os.Stat(snapshotFile); err != nil {
		return fmt.Errorf("snapshot not found: %s", snapshotID)
	}

	// Set package selections
	selectionsData, err := os.ReadFile(snapshotFile)
	if err != nil {
		return fmt.Errorf("failed to read snapshot: %w", err)
	}

	// Write selections to dpkg
	ctx, cancel := context.WithTimeout(context.Background(), c.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dpkg", "--set-selections")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmd.Stdin = strings.NewReader(string(selectionsData))
	if output, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("dpkg --set-selections timed out")
		}
		errDetail := extractErrorLines(output)
		if errDetail != "" {
			return fmt.Errorf("failed to set package selections: %s: %w", errDetail, err)
		}
		return fmt.Errorf("failed to set package selections: %w", err)
	}

	// Apply the selections — this can take a long time
	_, err = c.runCommandWithOutput("apt-get", "dselect-upgrade", "-y")
	if err != nil {
		return fmt.Errorf("failed to restore packages: %w", err)
	}

	// Version-aware downgrade: if we have a .versions file, downgrade packages
	// that are currently at a newer version than what the snapshot recorded.
	versionsFile := fmt.Sprintf("%s/%s.versions", snapshotDir, snapshotID)
	if downgrades := computeDowngrades(versionsFile); len(downgrades) > 0 {
		args := append([]string{"install", "-y", "--allow-downgrades"}, downgrades...)
		_, _ = c.runCommandWithOutput("apt-get", args...)
	}

	// Refresh package cache so the available updates list is accurate
	_, _ = c.runCommandWithOutput("apt-get", "update")

	return nil
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

// GetUpdateHistory returns recent package update history from dpkg and apt logs.
// Reads both /var/log/dpkg.log and /var/log/apt/history.log for comprehensive history
// regardless of whether updates were performed via Gearbox or directly via SSH.
func (c *UpdatesCollector) GetUpdateHistory(limit int) ([]UpdateHistoryEntry, error) {
	var history []UpdateHistoryEntry

	// Primary source: dpkg.log — logs every individual package operation
	dpkgHistory := c.parseDpkgLogs()
	history = append(history, dpkgHistory...)

	// Fallback/supplement: apt history.log — provides higher-level context
	aptHistory := c.parseAptHistoryLogs()

	// Merge apt history entries that aren't already captured by dpkg
	// dpkg is more granular so prefer it, but apt history captures the "why"
	if len(history) == 0 {
		history = aptHistory
	}

	// Sort by timestamp, most recent first
	sort.Slice(history, func(i, j int) bool {
		return history[i].Timestamp.After(history[j].Timestamp)
	})

	// Deduplicate entries with same timestamp+package+action
	if len(history) > 1 {
		deduped := []UpdateHistoryEntry{history[0]}
		for i := 1; i < len(history); i++ {
			prev := deduped[len(deduped)-1]
			curr := history[i]
			if curr.Package != prev.Package || curr.Action != prev.Action ||
				!curr.Timestamp.Equal(prev.Timestamp) {
				deduped = append(deduped, curr)
			}
		}
		history = deduped
	}

	// Apply limit
	if limit > 0 && len(history) > limit {
		history = history[:limit]
	}

	return history, nil
}

// parseDpkgLogs parses /var/log/dpkg.log and rotated versions.
// dpkg.log format: "2024-01-15 10:30:45 status installed package-name:amd64 1.2.3-1"
func (c *UpdatesCollector) parseDpkgLogs() []UpdateHistoryEntry {
	var history []UpdateHistoryEntry

	// Read rotated logs first (older), then current log (newest)
	logFiles := []string{
		"/var/log/dpkg.log.1",
		"/var/log/dpkg.log",
	}

	for _, logFile := range logFiles {
		entries := c.parseSingleDpkgLog(logFile)
		history = append(history, entries...)
	}

	return history
}

func (c *UpdatesCollector) parseSingleDpkgLog(logFile string) []UpdateHistoryEntry {
	var history []UpdateHistoryEntry

	file, err := os.Open(logFile)
	if err != nil {
		return history
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Increase buffer size for long lines
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// dpkg.log format: "2024-01-15 10:30:45 <action> <status> <package>:<arch> <version>"
		// We care about lines with specific status transitions
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}

		// Parse timestamp (first two fields)
		dateStr := parts[0] + " " + parts[1]
		t, err := time.Parse("2006-01-02 15:04:05", dateStr)
		if err != nil {
			continue
		}

		action := parts[2]
		status := parts[3]

		// Filter to meaningful status transitions
		var entry UpdateHistoryEntry
		entry.Timestamp = t
		entry.Status = "success"

		switch {
		case action == "install" && status == "installed":
			entry.Action = "install"
			if len(parts) >= 5 {
				entry.Package = strings.Split(parts[4], ":")[0]
			}
			if len(parts) >= 6 {
				entry.ToVersion = parts[5]
			}
		case action == "upgrade" && status == "installed":
			// Upgrade completion: "upgrade <pkg>:<arch> <old-version> <new-version>"
			entry.Action = "upgrade"
			if len(parts) >= 5 {
				entry.Package = strings.Split(parts[4], ":")[0]
			}
			if len(parts) >= 6 {
				entry.ToVersion = parts[5]
			}
		case action == "status" && status == "installed" && len(parts) >= 6:
			// "status installed <pkg>:<arch> <version>" — final state after install/upgrade
			entry.Action = "install"
			entry.Package = strings.Split(parts[4], ":")[0]
			entry.ToVersion = parts[5]
		case action == "remove" && status == "removed":
			entry.Action = "remove"
			if len(parts) >= 5 {
				entry.Package = strings.Split(parts[4], ":")[0]
			}
			if len(parts) >= 6 {
				entry.FromVersion = parts[5]
			}
		case action == "purge" && status == "purged":
			entry.Action = "remove"
			if len(parts) >= 5 {
				entry.Package = strings.Split(parts[4], ":")[0]
			}
		default:
			continue
		}

		if entry.Package != "" {
			history = append(history, entry)
		}
	}

	return history
}

// parseAptHistoryLogs parses /var/log/apt/history.log and rotated versions.
func (c *UpdatesCollector) parseAptHistoryLogs() []UpdateHistoryEntry {
	var history []UpdateHistoryEntry

	logFiles := []string{
		"/var/log/apt/history.log.1.gz",
		"/var/log/apt/history.log",
	}

	for _, logFile := range logFiles {
		if strings.HasSuffix(logFile, ".gz") {
			// Skip gzipped logs for now — only read plain text
			continue
		}
		entries := c.parseSingleAptHistoryLog(logFile)
		history = append(history, entries...)
	}

	return history
}

func (c *UpdatesCollector) parseSingleAptHistoryLog(logFile string) []UpdateHistoryEntry {
	var history []UpdateHistoryEntry

	file, err := os.Open(logFile)
	if err != nil {
		return history
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	var currentTimestamp time.Time
	var currentAction string
	var currentStatus string
	inEntry := false

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "Start-Date:") {
			dateStr := strings.TrimSpace(strings.TrimPrefix(line, "Start-Date:"))
			if t, err := time.Parse("2006-01-02  15:04:05", dateStr); err == nil {
				currentTimestamp = t
				currentStatus = "success"
				currentAction = ""
				inEntry = true
			}
		} else if strings.HasPrefix(line, "Commandline:") && inEntry {
			cmdLine := strings.TrimPrefix(line, "Commandline: ")
			if strings.Contains(cmdLine, "upgrade") || strings.Contains(cmdLine, "dist-upgrade") {
				currentAction = "upgrade"
			} else if strings.Contains(cmdLine, "install") {
				currentAction = "install"
			} else if strings.Contains(cmdLine, "remove") || strings.Contains(cmdLine, "purge") {
				currentAction = "remove"
			}
		} else if inEntry && (strings.HasPrefix(line, "Upgrade:") || strings.HasPrefix(line, "Install:") || strings.HasPrefix(line, "Remove:") || strings.HasPrefix(line, "Purge:")) {
			// Parse individual packages from the line
			// Format: "pkg1:arch (old-ver, new-ver), pkg2:arch (old-ver, new-ver), ..."
			var action string
			var pkgLine string
			if strings.HasPrefix(line, "Upgrade:") {
				action = "upgrade"
				pkgLine = strings.TrimPrefix(line, "Upgrade: ")
			} else if strings.HasPrefix(line, "Install:") {
				action = "install"
				pkgLine = strings.TrimPrefix(line, "Install: ")
			} else if strings.HasPrefix(line, "Remove:") {
				action = "remove"
				pkgLine = strings.TrimPrefix(line, "Remove: ")
			} else if strings.HasPrefix(line, "Purge:") {
				action = "remove"
				pkgLine = strings.TrimPrefix(line, "Purge: ")
			}

			if action == "" {
				continue
			}
			if currentAction == "" {
				currentAction = action
			}

			// Parse comma-separated package entries
			pkgEntries := strings.Split(pkgLine, "),")
			for _, entry := range pkgEntries {
				entry = strings.TrimSpace(entry)
				if entry == "" {
					continue
				}

				he := UpdateHistoryEntry{
					Timestamp: currentTimestamp,
					Action:    action,
					Status:    currentStatus,
				}

				// Parse "pkgname:arch (old-ver, new-ver)" or "pkgname:arch (ver)"
				if parenIdx := strings.Index(entry, " ("); parenIdx > 0 {
					pkgPart := entry[:parenIdx]
					he.Package = strings.Split(strings.TrimSpace(pkgPart), ":")[0]

					verPart := strings.TrimRight(entry[parenIdx+2:], ")")
					versions := strings.Split(verPart, ", ")
					if len(versions) == 2 {
						he.FromVersion = strings.TrimSpace(versions[0])
						he.ToVersion = strings.TrimSpace(versions[1])
					} else if len(versions) == 1 {
						he.ToVersion = strings.TrimSpace(versions[0])
					}
				} else {
					he.Package = strings.Split(strings.TrimSpace(entry), ":")[0]
				}

				if he.Package != "" {
					history = append(history, he)
				}
			}
		} else if strings.HasPrefix(line, "Error:") && inEntry {
			currentStatus = "failed"
		} else if strings.HasPrefix(line, "End-Date:") && inEntry {
			inEntry = false
		}
	}

	return history
}

// --- Package Search ---

// ListInstalledPackages returns all installed packages using dpkg-query.
func (c *UpdatesCollector) ListInstalledPackages() ([]InstalledPackage, error) {
	// Format: name\tversion\tarchitecture\tauto\tdescription
	output, err := c.runCommand("dpkg-query", "-W",
		"-f", "${Package}\t${Version}\t${Architecture}\t${db:Status-Abbrev}\t${binary:Summary}\n")
	if err != nil {
		return nil, fmt.Errorf("dpkg-query failed: %w", err)
	}

	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 4 {
			continue
		}
		// Status abbrev is "ii" for installed, skip others
		status := strings.TrimSpace(parts[3])
		if !strings.HasPrefix(status, "ii") {
			continue
		}
		pkg := InstalledPackage{
			Name:         parts[0],
			Version:      parts[1],
			Architecture: parts[2],
			Installed:    true,
		}
		if len(parts) == 5 {
			pkg.Description = strings.TrimSpace(parts[4])
		}
		packages = append(packages, pkg)
	}

	return packages, nil
}

// SearchPackages searches for packages matching a query using apt-cache search.
// Results include whether each package is currently installed.
func (c *UpdatesCollector) SearchPackages(query string, limit int) ([]InstalledPackage, error) {
	if limit <= 0 {
		limit = 50
	}

	// Use apt-cache search for fuzzy matching — returns name + description
	output, err := c.runCommand("apt-cache", "search", query)
	if err != nil {
		return nil, fmt.Errorf("apt-cache search failed: %w", err)
	}

	// Build installed set for marking results
	installedSet := make(map[string]bool)
	if installedOutput, err := c.runCommand("dpkg-query", "-W", "-f", "${Package}\t${db:Status-Abbrev}\n"); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(installedOutput)))
		for sc.Scan() {
			parts := strings.SplitN(sc.Text(), "\t", 2)
			if len(parts) == 2 && strings.HasPrefix(strings.TrimSpace(parts[1]), "ii") {
				installedSet[parts[0]] = true
			}
		}
	}

	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	for scanner.Scan() && len(packages) < limit {
		line := scanner.Text()
		// Format: package_name - description
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) < 1 {
			continue
		}
		pkg := InstalledPackage{
			Name:      strings.TrimSpace(parts[0]),
			Installed: installedSet[strings.TrimSpace(parts[0])],
		}
		if len(parts) == 2 {
			pkg.Description = strings.TrimSpace(parts[1])
		}
		packages = append(packages, pkg)
	}

	return packages, nil
}

// InstallPackage installs a new package.
func (c *UpdatesCollector) InstallPackage(name string) error {
	// Validate package name
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}

	_, err := c.runCommandWithOutput("apt-get", "install", "-y", name)
	if err != nil {
		return fmt.Errorf("failed to install package %s: %w", name, err)
	}

	return nil
}

// RemovePackage removes a package.
func (c *UpdatesCollector) RemovePackage(name string, purge bool) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}

	action := "remove"
	if purge {
		action = "purge"
	}

	_, err := c.runCommandWithOutput("apt-get", action, "-y", name)
	if err != nil {
		return fmt.Errorf("failed to remove package %s: %w", name, err)
	}

	return nil
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
			return output, fmt.Errorf("%s: %w", errDetail, err)
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
