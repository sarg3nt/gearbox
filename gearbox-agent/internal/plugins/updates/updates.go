// Package system provides system-level collectors for OS updates and package management.
package updates

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
	InstalledAt  time.Time `json:"installed_at,omitempty"` // May not be available
	AutoInstall  bool      `json:"auto_install"`           // Automatically installed as dependency
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
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	PythonPath string   `json:"python_path,omitempty"`
	Apps       []string `json:"apps,omitempty"` // Exposed applications
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
func (c *UpdatesCollector) runCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return output, ErrCommandTimeout
	}

	return output, err
}

// runPipxCommand runs a pipx command with the correct environment for root's pipx.
// pipx stores packages in user-specific directories, so we need to ensure
// HOME is set to /root when the agent runs as root.
func (c *UpdatesCollector) runPipxCommand(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pipx", args...)
	// Set HOME to /root to access root's pipx packages
	cmd.Env = append(os.Environ(), "HOME=/root")
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return output, ErrCommandTimeout
	}

	return output, err
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

// TriggerUpdateCheck triggers a fresh 'apt update' to refresh package lists.
func (c *UpdatesCollector) TriggerUpdateCheck() error {
	_, err := c.runCommand("apt", "update")
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

	_, err := c.runCommand("apt", args...)
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
			for _, pkg := range strings.Fields(line) {
				packages = append(packages, pkg)
			}
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
		_, err := c.runCommand("systemctl", "disable", "--now", "unattended-upgrades")
		return err
	}

	// Enable unattended-upgrades
	_, err := c.runCommand("systemctl", "enable", "--now", "unattended-upgrades")
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

	_, err := c.runCommand("shutdown", args...)
	if err != nil {
		return fmt.Errorf("failed to schedule reboot: %w", err)
	}

	return nil
}

// CancelReboot cancels a scheduled reboot.
func (c *UpdatesCollector) CancelReboot() error {
	_, err := c.runCommand("shutdown", "-c")
	return err
}

// --- APT Snapshot Management ---

// CreateSnapshot creates an APT snapshot for rollback capability.
func (c *UpdatesCollector) CreateSnapshot(reason string) (*AptSnapshot, error) {
	// Ubuntu 24.04+ has apt-snapshot (snapper integration)
	// For now, we'll create a manual snapshot using dpkg selections

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
	cmd := exec.Command("dpkg", "--set-selections")
	cmd.Stdin = strings.NewReader(string(selectionsData))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to set package selections: %w", err)
	}

	// Apply the selections
	_, err = c.runCommand("apt-get", "dselect-upgrade", "-y")
	if err != nil {
		return fmt.Errorf("failed to restore packages: %w", err)
	}

	return nil
}

// DeleteSnapshot removes a snapshot.
func (c *UpdatesCollector) DeleteSnapshot(snapshotID string) error {
	snapshotDir := "/var/lib/gearbox-agent/snapshots"

	// Remove both files
	os.Remove(fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID))
	os.Remove(fmt.Sprintf("%s/%s.meta", snapshotDir, snapshotID))

	return nil
}

// --- Update History ---

// GetUpdateHistory returns recent package update history from apt logs.
func (c *UpdatesCollector) GetUpdateHistory(limit int) ([]UpdateHistoryEntry, error) {
	var history []UpdateHistoryEntry

	// Parse /var/log/apt/history.log
	logFile := "/var/log/apt/history.log"
	file, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return history, nil
		}
		return nil, fmt.Errorf("failed to open apt history log: %w", err)
	}
	defer file.Close()

	var currentEntry *UpdateHistoryEntry
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "Start-Date:") {
			// New entry
			dateStr := strings.TrimPrefix(line, "Start-Date: ")
			if t, err := time.Parse("2006-01-02  15:04:05", dateStr); err == nil {
				currentEntry = &UpdateHistoryEntry{
					Timestamp: t,
					Status:    "success",
				}
			}
		} else if strings.HasPrefix(line, "Commandline:") && currentEntry != nil {
			cmdLine := strings.TrimPrefix(line, "Commandline: ")
			if strings.Contains(cmdLine, "upgrade") {
				currentEntry.Action = "upgrade"
			} else if strings.Contains(cmdLine, "install") {
				currentEntry.Action = "install"
			} else if strings.Contains(cmdLine, "remove") {
				currentEntry.Action = "remove"
			}
		} else if strings.HasPrefix(line, "Upgrade:") && currentEntry != nil {
			currentEntry.Action = "upgrade"
			currentEntry.Package = strings.TrimPrefix(line, "Upgrade: ")
		} else if strings.HasPrefix(line, "Install:") && currentEntry != nil {
			currentEntry.Action = "install"
			currentEntry.Package = strings.TrimPrefix(line, "Install: ")
		} else if strings.HasPrefix(line, "Remove:") && currentEntry != nil {
			currentEntry.Action = "remove"
			currentEntry.Package = strings.TrimPrefix(line, "Remove: ")
		} else if strings.HasPrefix(line, "End-Date:") && currentEntry != nil {
			history = append(history, *currentEntry)
			currentEntry = nil
		} else if strings.HasPrefix(line, "Error:") && currentEntry != nil {
			currentEntry.Status = "failed"
		}
	}

	// Reverse to get most recent first
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}

	// Apply limit
	if limit > 0 && len(history) > limit {
		history = history[:limit]
	}

	return history, nil
}

// --- Package Search ---

// SearchPackages searches for packages matching a query.
func (c *UpdatesCollector) SearchPackages(query string, limit int) ([]InstalledPackage, error) {
	if limit <= 0 {
		limit = 50
	}

	// Use apt-cache search for fuzzy matching
	output, err := c.runCommand("apt-cache", "search", query)
	if err != nil {
		return nil, fmt.Errorf("apt-cache search failed: %w", err)
	}

	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	for scanner.Scan() && len(packages) < limit {
		line := scanner.Text()
		// Format: package_name - description
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) >= 1 {
			pkg := InstalledPackage{
				Name: strings.TrimSpace(parts[0]),
			}
			packages = append(packages, pkg)
		}
	}

	return packages, nil
}

// InstallPackage installs a new package.
func (c *UpdatesCollector) InstallPackage(name string) error {
	// Validate package name
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}

	_, err := c.runCommand("apt", "install", "-y", name)
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

	_, err := c.runCommand("apt", action, "-y", name)
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

// ListPipxPackages returns installed pipx packages.
func (c *UpdatesCollector) ListPipxPackages() ([]PipxPackage, error) {
	if !c.IsPipxAvailable() {
		return nil, fmt.Errorf("pipx is not installed")
	}

	output, err := c.runPipxCommand("list", "--json")
	if err != nil {
		// Fallback to plain list if JSON fails
		output, err = c.runPipxCommand("list")
		if err != nil {
			return nil, fmt.Errorf("failed to list pipx packages: %w", err)
		}
		return c.parsePipxListPlain(string(output)), nil
	}

	return c.parsePipxListJSON(string(output))
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
func (c *UpdatesCollector) parsePipxListJSON(output string) ([]PipxPackage, error) {
	// For now, fall back to plain parsing since JSON format varies
	// TODO: Implement proper JSON parsing if needed
	return c.parsePipxListPlain(output), nil
}

// SearchPyPI searches PyPI for packages (basic implementation).
func (c *UpdatesCollector) SearchPyPI(query string, limit int) ([]PipxPackage, error) {
	if limit <= 0 {
		limit = 20
	}

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

	_, err := c.runPipxCommand("install", name)
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

	_, err := c.runPipxCommand("uninstall", name)
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

	_, err := c.runPipxCommand("upgrade", name)
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

	_, err := c.runPipxCommand("upgrade-all")
	if err != nil {
		return fmt.Errorf("failed to upgrade all pipx packages: %w", err)
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
