package updates

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// zypperPackageManager implements PackageManager for openSUSE / SLES.
type zypperPackageManager struct {
	collector *UpdatesCollector
}

// NewZypperPackageManager returns a PackageManager backed by zypper.
func NewZypperPackageManager() PackageManager {
	return &zypperPackageManager{collector: NewUpdatesCollector()}
}

func (z *zypperPackageManager) Name() string    { return "zypper" }
func (z *zypperPackageManager) Available() bool { _, err := exec.LookPath("zypper"); return err == nil }
func (z *zypperPackageManager) EnvVars() []string { return nil }

func (z *zypperPackageManager) BuildUpdateCheckCommand() []string {
	return []string{"zypper", "refresh"}
}

func (z *zypperPackageManager) BuildUpgradeCommand() []string {
	return []string{"zypper", "--non-interactive", "update"}
}

func (z *zypperPackageManager) BuildInstallCommand(packages []string, securityOnly bool) []string {
	if len(packages) > 0 {
		return append([]string{"zypper", "--non-interactive", "install"}, packages...)
	}
	if securityOnly {
		return []string{"zypper", "--non-interactive", "patch", "--category", "security"}
	}
	return []string{"zypper", "--non-interactive", "update"}
}

func (z *zypperPackageManager) IsAutoUpdateActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "zypper-timer.timer").Run() == nil ||
		exec.Command("systemctl", "is-active", "--quiet", "yast2-online-update-configuration.timer").Run() == nil
}

func (z *zypperPackageManager) CheckUpdates() (*UpdateInfo, error) {
	info := &UpdateInfo{Available: true, LastCheck: time.Now()}

	output, err := z.collector.runCommand("zypper", "--non-interactive", "list-updates")
	if err != nil && len(output) == 0 {
		return info, nil
	}

	packages := z.parseListUpdatesOutput(string(output))
	info.TotalUpdates = len(packages)
	for _, p := range packages {
		if p.IsSecurityUpdate {
			info.SecurityUpdates++
		}
	}
	info.UnattendedActive = z.IsAutoUpdateActive()
	return info, nil
}

func (z *zypperPackageManager) ListUpgradable() ([]Package, error) {
	output, err := z.collector.runCommand("zypper", "--non-interactive", "list-updates")
	if err != nil {
		return nil, fmt.Errorf("zypper list-updates failed: %w", err)
	}
	return z.parseListUpdatesOutput(string(output)), nil
}

func (z *zypperPackageManager) TriggerUpdateCheck() error {
	_, err := z.collector.runCommandWithOutput("zypper", "refresh")
	if err != nil {
		return fmt.Errorf("zypper refresh failed: %w", err)
	}
	return nil
}

func (z *zypperPackageManager) InstallUpdates(securityOnly bool, packages []string) ([]string, error) {
	var args []string
	if len(packages) > 0 {
		args = append([]string{"zypper", "--non-interactive", "install"}, packages...)
	} else if securityOnly {
		args = []string{"zypper", "--non-interactive", "patch", "--category", "security"}
	} else {
		args = []string{"zypper", "--non-interactive", "update"}
	}
	_, err := z.collector.runCommandWithOutput(args[0], args[1:]...)
	if err != nil {
		return nil, fmt.Errorf("zypper update failed: %w", err)
	}
	return packages, nil
}

func (z *zypperPackageManager) InstallPackage(name string) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	_, err := z.collector.runCommandWithOutput("zypper", "--non-interactive", "install", name)
	if err != nil {
		return fmt.Errorf("failed to install %s: %w", name, err)
	}
	return nil
}

func (z *zypperPackageManager) RemovePackage(name string, _ bool) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	_, err := z.collector.runCommandWithOutput("zypper", "--non-interactive", "remove", name)
	if err != nil {
		return fmt.Errorf("failed to remove %s: %w", name, err)
	}
	return nil
}

func (z *zypperPackageManager) ListInstalledPackages() ([]InstalledPackage, error) {
	output, err := z.collector.runCommand("rpm", "-qa", "--queryformat", "%{NAME}\t%{VERSION}-%{RELEASE}\t%{ARCH}\t%{SUMMARY}\n")
	if err != nil {
		return nil, fmt.Errorf("rpm -qa failed: %w", err)
	}
	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 4)
		if len(parts) < 3 {
			continue
		}
		pkg := InstalledPackage{
			Name:         parts[0],
			Version:      parts[1],
			Architecture: parts[2],
			Installed:    true,
		}
		if len(parts) == 4 {
			pkg.Description = parts[3]
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

func (z *zypperPackageManager) SearchPackages(query string, limit int) ([]InstalledPackage, error) {
	if limit <= 0 {
		limit = 50
	}
	// zypper search outputs a table; -s shows installed status
	output, err := z.collector.runCommand("zypper", "--non-interactive", "search", "-s", query)
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("zypper search failed: %w", err)
	}

	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	inTable := false
	for scanner.Scan() && len(packages) < limit {
		line := scanner.Text()
		if strings.Contains(line, "----") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		// Format: "i | name | summary | type"
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 2 {
			continue
		}
		status := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])
		if name == "" {
			continue
		}
		pkg := InstalledPackage{
			Name:      name,
			Installed: strings.HasPrefix(status, "i"),
		}
		if len(parts) >= 3 {
			pkg.Description = strings.TrimSpace(parts[2])
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

func (z *zypperPackageManager) GetUpdateHistory(limit int) ([]UpdateHistoryEntry, error) {
	// zypper history is in /var/log/zypp/history
	return parseZypperHistory(limit), nil
}

func (z *zypperPackageManager) GetAutoUpdateConfig() (map[string]any, error) {
	return map[string]any{"enabled": z.IsAutoUpdateActive()}, nil
}

func (z *zypperPackageManager) ConfigureAutoUpdate(enabled, _ bool) error {
	if enabled {
		_, err := z.collector.runCommandWithOutput("systemctl", "enable", "--now", "zypper-timer.timer")
		return err
	}
	_, err := z.collector.runCommandWithOutput("systemctl", "disable", "--now", "zypper-timer.timer")
	return err
}

func (z *zypperPackageManager) GetRebootRequired() (bool, []string, error) {
	// zypper ps -s exits 102 when reboot is needed
	err := exec.Command("zypper", "--non-interactive", "ps", "-s").Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 102 {
				return true, nil, nil
			}
		}
	}
	return false, nil, nil
}

func (z *zypperPackageManager) CreateSnapshot(reason string) (*AptSnapshot, error) {
	return createRPMSnapshot(z.collector, reason)
}

func (z *zypperPackageManager) RestoreSnapshot(_ string) error {
	return fmt.Errorf("snapshot restore is not yet supported for zypper; use zypper manually")
}

func (z *zypperPackageManager) parseListUpdatesOutput(output string) []Package {
	var packages []Package
	inTable := false
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "----") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		// Format: "Repository | Name | Current Version | Available Version | Arch"
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 5 {
			continue
		}
		pkg := Package{
			Repository:       strings.TrimSpace(parts[0]),
			Name:             strings.TrimSpace(parts[1]),
			CurrentVersion:   strings.TrimSpace(parts[2]),
			AvailableVersion: strings.TrimSpace(parts[3]),
			Architecture:     strings.TrimSpace(parts[4]),
			Priority:         "medium",
		}
		if strings.Contains(strings.ToLower(pkg.Repository), "security") {
			pkg.IsSecurityUpdate = true
			pkg.Priority = "high"
		}
		packages = append(packages, pkg)
	}
	return packages
}

func parseZypperHistory(limit int) []UpdateHistoryEntry {
	data, err := os.ReadFile("/var/log/zypp/history")
	if err != nil {
		return nil
	}
	var history []UpdateHistoryEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		// Format: "date time | action | name | version | arch | repo"
		parts := strings.SplitN(line, "|", 6)
		if len(parts) < 3 {
			continue
		}
		dateStr := strings.TrimSpace(parts[0])
		t, err := time.Parse("2006-01-02 15:04:05", dateStr)
		if err != nil {
			continue
		}
		actionStr := strings.ToLower(strings.TrimSpace(parts[1]))
		var action string
		switch actionStr {
		case "install":
			action = "install"
		case "update":
			action = "upgrade"
		case "remove":
			action = "remove"
		default:
			continue
		}
		entry := UpdateHistoryEntry{
			Timestamp: t,
			Action:    action,
			Package:   strings.TrimSpace(parts[2]),
			Status:    "success",
		}
		if len(parts) >= 4 {
			entry.ToVersion = strings.TrimSpace(parts[3])
		}
		history = append(history, entry)
		if limit > 0 && len(history) >= limit {
			break
		}
	}
	return history
}

func (z *zypperPackageManager) HoldPackage(_ string) error            { return errNotSupported("zypper") }
func (z *zypperPackageManager) UnholdPackage(_ string) error          { return errNotSupported("zypper") }
func (z *zypperPackageManager) ListHeldPackages() ([]string, error)   { return nil, errNotSupported("zypper") }
