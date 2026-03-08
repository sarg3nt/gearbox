package updates

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// apkPackageManager implements PackageManager for Alpine Linux.
type apkPackageManager struct {
	collector *UpdatesCollector
}

// NewApkPackageManager returns a PackageManager backed by apk.
func NewApkPackageManager() PackageManager {
	return &apkPackageManager{collector: NewUpdatesCollector()}
}

func (a *apkPackageManager) Name() string    { return "apk" }
func (a *apkPackageManager) Available() bool { _, err := exec.LookPath("apk"); return err == nil }
func (a *apkPackageManager) EnvVars() []string { return nil }

func (a *apkPackageManager) BuildUpdateCheckCommand() []string {
	return []string{"apk", "update"}
}

func (a *apkPackageManager) BuildUpgradeCommand() []string {
	return []string{"apk", "upgrade"}
}

func (a *apkPackageManager) BuildInstallCommand(packages []string, _ bool) []string {
	if len(packages) > 0 {
		return append([]string{"apk", "add"}, packages...)
	}
	return []string{"apk", "upgrade"}
}

func (a *apkPackageManager) IsAutoUpdateActive() bool {
	// Alpine typically uses cron for auto-updates; check for apk-upgrade cron job
	data, err := os.ReadFile("/etc/crontabs/root")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "apk upgrade") || strings.Contains(string(data), "apk -U upgrade")
}

func (a *apkPackageManager) CheckUpdates() (*UpdateInfo, error) {
	info := &UpdateInfo{Available: true, LastCheck: time.Now()}

	_, _ = a.collector.runCommand("apk", "update", "-q")
	output, err := a.collector.runCommand("apk", "list", "--upgradable")
	if err != nil && len(output) == 0 {
		return info, nil
	}

	packages := a.parseUpgradableOutput(string(output))
	info.TotalUpdates = len(packages)
	info.UnattendedActive = a.IsAutoUpdateActive()
	return info, nil
}

func (a *apkPackageManager) ListUpgradable() ([]Package, error) {
	output, err := a.collector.runCommand("apk", "list", "--upgradable")
	if err != nil {
		return nil, fmt.Errorf("apk list --upgradable failed: %w", err)
	}
	return a.parseUpgradableOutput(string(output)), nil
}

func (a *apkPackageManager) TriggerUpdateCheck() error {
	_, err := a.collector.runCommandWithOutput("apk", "update")
	if err != nil {
		return fmt.Errorf("apk update failed: %w", err)
	}
	return nil
}

func (a *apkPackageManager) InstallUpdates(_ bool, packages []string) ([]string, error) {
	var args []string
	if len(packages) > 0 {
		args = append([]string{"apk", "add"}, packages...)
	} else {
		args = []string{"apk", "upgrade"}
	}
	_, err := a.collector.runCommandWithOutput(args[0], args[1:]...)
	if err != nil {
		return nil, fmt.Errorf("apk upgrade failed: %w", err)
	}
	return packages, nil
}

func (a *apkPackageManager) InstallPackage(name string) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	_, err := a.collector.runCommandWithOutput("apk", "add", name)
	if err != nil {
		return fmt.Errorf("failed to install %s: %w", name, err)
	}
	return nil
}

func (a *apkPackageManager) RemovePackage(name string, purge bool) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	args := []string{"apk", "del", name}
	if purge {
		args = []string{"apk", "del", "--purge", name}
	}
	_, err := a.collector.runCommandWithOutput(args[0], args[1:]...)
	if err != nil {
		return fmt.Errorf("failed to remove %s: %w", name, err)
	}
	return nil
}

func (a *apkPackageManager) ListInstalledPackages() ([]InstalledPackage, error) {
	output, err := a.collector.runCommand("apk", "info", "-v")
	if err != nil {
		return nil, fmt.Errorf("apk info -v failed: %w", err)
	}
	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: "name-version" (e.g. "musl-1.2.3-r0")
		// We need to find the last hyphen that precedes a digit for version split
		name, version := splitApkNameVersion(line)
		packages = append(packages, InstalledPackage{
			Name:      name,
			Version:   version,
			Installed: true,
		})
	}
	return packages, nil
}

func (a *apkPackageManager) SearchPackages(query string, limit int) ([]InstalledPackage, error) {
	if limit <= 0 {
		limit = 50
	}
	output, err := a.collector.runCommand("apk", "search", "-q", query)
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("apk search failed: %w", err)
	}

	installedSet := a.installedSet()
	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() && len(packages) < limit {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		name, version := splitApkNameVersion(line)
		packages = append(packages, InstalledPackage{
			Name:      name,
			Version:   version,
			Installed: installedSet[name],
		})
	}
	return packages, nil
}

func (a *apkPackageManager) installedSet() map[string]bool {
	out, err := a.collector.runCommand("apk", "info", "-q")
	if err != nil {
		return nil
	}
	set := make(map[string]bool)
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		set[strings.TrimSpace(sc.Text())] = true
	}
	return set
}

func (a *apkPackageManager) GetUpdateHistory(_ int) ([]UpdateHistoryEntry, error) {
	// Alpine logs to /var/log/apk.log if configured
	data, err := os.ReadFile("/var/log/apk.log")
	if err != nil {
		return nil, nil
	}
	var history []UpdateHistoryEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: "2024-01-15 10:30:45 Installed: pkg-1.2.3-r0"
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			continue
		}
		t, err := time.Parse("2006-01-02 15:04:05", parts[0]+" "+parts[1])
		if err != nil {
			continue
		}
		actionStr := strings.ToLower(strings.TrimRight(parts[2], ":"))
		var action string
		switch actionStr {
		case "installed", "upgrading":
			action = "install"
		case "deleted", "removing":
			action = "remove"
		default:
			continue
		}
		name, _ := splitApkNameVersion(parts[3])
		history = append(history, UpdateHistoryEntry{
			Timestamp: t,
			Action:    action,
			Package:   name,
			Status:    "success",
		})
	}
	return history, nil
}

func (a *apkPackageManager) GetAutoUpdateConfig() (map[string]any, error) {
	return map[string]any{"enabled": a.IsAutoUpdateActive()}, nil
}

func (a *apkPackageManager) ConfigureAutoUpdate(enabled, _ bool) error {
	// Add/remove a cron entry for apk upgrade
	return fmt.Errorf("automatic update configuration for Alpine is not yet automated; edit /etc/crontabs/root manually")
}

func (a *apkPackageManager) GetRebootRequired() (bool, []string, error) {
	// Alpine typically doesn't track reboot-required explicitly
	return false, nil, nil
}

func (a *apkPackageManager) CreateSnapshot(reason string) (*AptSnapshot, error) {
	timestamp := time.Now()
	snapshotID := timestamp.Format("20060102-150405")
	snapshotDir := "/var/lib/gearbox-agent/snapshots"

	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	output, err := a.collector.runCommand("apk", "info", "-v")
	if err != nil {
		return nil, fmt.Errorf("apk info -v failed: %w", err)
	}

	if err := os.WriteFile(fmt.Sprintf("%s/%s.versions", snapshotDir, snapshotID), output, 0644); err != nil {
		return nil, fmt.Errorf("failed to save snapshot: %w", err)
	}
	if err := os.WriteFile(fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID), []byte("# apk snapshot\n"), 0644); err != nil {
		return nil, fmt.Errorf("failed to save placeholder: %w", err)
	}

	meta := fmt.Sprintf("timestamp=%s\nreason=%s\n", timestamp.Format(time.RFC3339), reason)
	if err := os.WriteFile(fmt.Sprintf("%s/%s.meta", snapshotDir, snapshotID), []byte(meta), 0644); err != nil {
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}

	return &AptSnapshot{ID: snapshotID, CreatedAt: timestamp, Reason: reason}, nil
}

func (a *apkPackageManager) RestoreSnapshot(_ string) error {
	return fmt.Errorf("snapshot restore is not yet supported for apk")
}

func (a *apkPackageManager) parseUpgradableOutput(output string) []Package {
	var packages []Package
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: "name-version-r0 {repo} (deps) [upgradable from: old-version]"
		// or simply "name-version" with upgradable marker
		name, version := splitApkNameVersion(line)
		if name == "" {
			continue
		}
		packages = append(packages, Package{
			Name:             name,
			AvailableVersion: version,
			Priority:         "medium",
		})
	}
	return packages
}

// splitApkNameVersion splits an apk package string like "musl-1.2.3-r0" into
// name ("musl") and version ("1.2.3-r0"). It finds the last dash before a digit.
func splitApkNameVersion(s string) (name, version string) {
	for i := len(s) - 1; i > 0; i-- {
		if s[i] == '-' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

func (a *apkPackageManager) HoldPackage(_ string) error            { return errNotSupported("apk") }
func (a *apkPackageManager) UnholdPackage(_ string) error          { return errNotSupported("apk") }
func (a *apkPackageManager) ListHeldPackages() ([]string, error)   { return nil, errNotSupported("apk") }
