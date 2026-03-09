package updates

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// dnfPackageManager implements PackageManager for Fedora / RHEL 8+ / AlmaLinux / Rocky.
type dnfPackageManager struct {
	collector *UpdatesCollector
}

// NewDnfPackageManager returns a PackageManager backed by dnf.
func NewDnfPackageManager() PackageManager {
	return &dnfPackageManager{collector: NewUpdatesCollector()}
}

func (d *dnfPackageManager) Name() string    { return "dnf" }
func (d *dnfPackageManager) Available() bool { _, err := exec.LookPath("dnf"); return err == nil }
func (d *dnfPackageManager) EnvVars() []string { return nil }

func (d *dnfPackageManager) BuildUpdateCheckCommand() []string {
	return []string{"dnf", "check-update", "-q"}
}

func (d *dnfPackageManager) BuildUpgradeCommand() []string {
	return []string{"dnf", "upgrade", "-y"}
}

func (d *dnfPackageManager) BuildInstallCommand(packages []string, securityOnly bool) []string {
	if len(packages) > 0 {
		return append([]string{"dnf", "install", "-y"}, packages...)
	}
	if securityOnly {
		return []string{"dnf", "upgrade", "--security", "-y"}
	}
	return []string{"dnf", "upgrade", "-y"}
}

func (d *dnfPackageManager) IsAutoUpdateActive() bool {
	// dnf-automatic service
	return exec.Command("systemctl", "is-active", "--quiet", "dnf-automatic.timer").Run() == nil ||
		exec.Command("systemctl", "is-active", "--quiet", "dnf-automatic-install.timer").Run() == nil
}

func (d *dnfPackageManager) CheckUpdates() (*UpdateInfo, error) {
	info := &UpdateInfo{Available: true, LastCheck: time.Now()}

	// dnf check-update exits 100 when updates are available, 0 when none, other on error
	output, err := d.collector.runCommand("dnf", "check-update", "-q", "--refresh")
	_ = err // exit code 100 is expected

	packages := d.parseCheckUpdateOutput(string(output))
	info.TotalUpdates = len(packages)
	for _, p := range packages {
		if p.IsSecurityUpdate {
			info.SecurityUpdates++
		}
	}

	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		info.RebootRequired = true
	}
	// Also check needs-restarting
	if exec.Command("needs-restarting", "-r").Run() != nil {
		info.RebootRequired = true
	}

	info.UnattendedActive = d.IsAutoUpdateActive()
	return info, nil
}

func (d *dnfPackageManager) ListUpgradable() ([]Package, error) {
	output, err := d.collector.runCommand("dnf", "check-update", "-q")
	_ = err // exit 100 = updates available
	return d.parseCheckUpdateOutput(string(output)), nil
}

func (d *dnfPackageManager) TriggerUpdateCheck() error {
	// dnf makecache refreshes the cache
	_, err := d.collector.runCommandWithOutput("dnf", "makecache")
	if err != nil {
		return fmt.Errorf("dnf makecache failed: %w", err)
	}
	return nil
}

func (d *dnfPackageManager) InstallUpdates(securityOnly bool, packages []string) ([]string, error) {
	var args []string
	if len(packages) > 0 {
		args = append([]string{"dnf", "install", "-y"}, packages...)
	} else if securityOnly {
		args = []string{"dnf", "upgrade", "--security", "-y"}
	} else {
		args = []string{"dnf", "upgrade", "-y"}
	}
	_, err := d.collector.runCommandWithOutput(args[0], args[1:]...)
	if err != nil {
		return nil, fmt.Errorf("dnf upgrade failed: %w", err)
	}
	return packages, nil
}

func (d *dnfPackageManager) InstallPackage(name string) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	_, err := d.collector.runCommandWithOutput("dnf", "install", "-y", name)
	if err != nil {
		return fmt.Errorf("failed to install %s: %w", name, err)
	}
	return nil
}

func (d *dnfPackageManager) RemovePackage(name string, _ bool) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	_, err := d.collector.runCommandWithOutput("dnf", "remove", "-y", name)
	if err != nil {
		return fmt.Errorf("failed to remove %s: %w", name, err)
	}
	return nil
}

func (d *dnfPackageManager) ListInstalledPackages() ([]InstalledPackage, error) {
	output, err := d.collector.runCommand("rpm", "-qa", "--queryformat", "%{NAME}\t%{VERSION}-%{RELEASE}\t%{ARCH}\t%{SUMMARY}\n")
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

func (d *dnfPackageManager) SearchPackages(query string, limit int) ([]InstalledPackage, error) {
	if limit <= 0 {
		limit = 50
	}
	output, err := d.collector.runCommand("dnf", "search", "-q", query)
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("dnf search failed: %w", err)
	}

	installedSet := d.installedSet()
	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() && len(packages) < limit {
		line := strings.TrimSpace(scanner.Text())
		// Format: "name.arch : description"
		if !strings.Contains(line, " : ") {
			continue
		}
		parts := strings.SplitN(line, " : ", 2)
		namePart := strings.TrimSpace(parts[0])
		// strip arch suffix (e.g. ".x86_64")
		if idx := strings.LastIndex(namePart, "."); idx > 0 {
			namePart = namePart[:idx]
		}
		pkg := InstalledPackage{
			Name:      namePart,
			Installed: installedSet[namePart],
		}
		if len(parts) == 2 {
			pkg.Description = strings.TrimSpace(parts[1])
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

func (d *dnfPackageManager) installedSet() map[string]bool {
	out, err := d.collector.runCommand("rpm", "-qa", "--queryformat", "%{NAME}\n")
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

func (d *dnfPackageManager) GetUpdateHistory(limit int) ([]UpdateHistoryEntry, error) {
	return parseDnfHistory(d.collector, limit)
}

func (d *dnfPackageManager) GetAutoUpdateConfig() (map[string]any, error) {
	cfg := map[string]any{
		"enabled": d.IsAutoUpdateActive(),
	}
	// Read /etc/dnf/automatic.conf if present
	if data, err := os.ReadFile("/etc/dnf/automatic.conf"); err == nil {
		content := string(data)
		cfg["apply_updates"] = strings.Contains(content, "apply_updates = yes")
		cfg["auto_reboot"] = strings.Contains(content, "reboot = when-needed") ||
			strings.Contains(content, "reboot = when-changed")
	}
	return cfg, nil
}

func (d *dnfPackageManager) ConfigureAutoUpdate(enabled, _ bool) error {
	svc := "dnf-automatic-install.timer"
	if enabled {
		if _, err := exec.LookPath("dnf-automatic"); err != nil {
			return fmt.Errorf("dnf-automatic is not installed; install it with: dnf install dnf-automatic")
		}
		_, err := d.collector.runCommandWithOutput("systemctl", "enable", "--now", svc)
		return err
	}
	_, err := d.collector.runCommandWithOutput("systemctl", "disable", "--now", svc)
	return err
}

func (d *dnfPackageManager) GetRebootRequired() (bool, []string, error) {
	if exec.Command("needs-restarting", "-r").Run() != nil {
		return true, nil, nil
	}
	return false, nil, nil
}

func (d *dnfPackageManager) CreateSnapshot(reason string) (*AptSnapshot, error) {
	return createRPMSnapshot(d.collector, reason)
}

func (d *dnfPackageManager) RestoreSnapshot(_ string) error {
	return fmt.Errorf("snapshot restore is not yet supported for dnf; use 'dnf history undo' manually")
}

// parseCheckUpdateOutput parses 'dnf check-update' output.
// Format: "package-name.arch  new-version  repo"
func (d *dnfPackageManager) parseCheckUpdateOutput(output string) []Package {
	var packages []Package
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Last metadata") || strings.HasPrefix(line, "Obsoleting") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// name.arch
		nameArch := fields[0]
		dotIdx := strings.LastIndex(nameArch, ".")
		name := nameArch
		arch := ""
		if dotIdx > 0 {
			name = nameArch[:dotIdx]
			arch = nameArch[dotIdx+1:]
		}
		pkg := Package{
			Name:             name,
			AvailableVersion: fields[1],
			Architecture:     arch,
			Repository:       fields[2],
			Priority:         "medium",
		}
		if strings.Contains(strings.ToLower(fields[2]), "security") {
			pkg.IsSecurityUpdate = true
			pkg.Priority = "high"
		}
		packages = append(packages, pkg)
	}
	return packages
}

// parseDnfHistory reads 'dnf history list' output.
func parseDnfHistory(c *UpdatesCollector, limit int) ([]UpdateHistoryEntry, error) {
	output, err := c.runCommand("dnf", "history", "list", "--reverse")
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("dnf history list failed: %w", err)
	}
	var history []UpdateHistoryEntry
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		// Skip header lines
		if strings.HasPrefix(line, "ID") || strings.HasPrefix(line, "--") || strings.TrimSpace(line) == "" {
			continue
		}
		// Format: "  ID | Command | Date/Time | Action | Altered"
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 4 {
			continue
		}
		dateStr := strings.TrimSpace(parts[2])
		t, err := time.Parse("2006-01-02 15:04", dateStr)
		if err != nil {
			continue
		}
		actionStr := strings.ToLower(strings.TrimSpace(parts[3]))
		var action string
		switch {
		case strings.Contains(actionStr, "install"):
			action = "install"
		case strings.Contains(actionStr, "upgrade"):
			action = "upgrade"
		case strings.Contains(actionStr, "erase"), strings.Contains(actionStr, "remove"):
			action = "remove"
		default:
			continue
		}
		entry := UpdateHistoryEntry{
			Timestamp: t,
			Action:    action,
			Package:   strings.TrimSpace(parts[1]),
			Status:    "success",
		}
		history = append(history, entry)
	}
	if limit > 0 && len(history) > limit {
		history = history[:limit]
	}
	return history, nil
}

// createRPMSnapshot captures the current RPM package list for rollback reference.
func createRPMSnapshot(c *UpdatesCollector, reason string) (*AptSnapshot, error) {
	timestamp := time.Now()
	snapshotID := timestamp.Format("20060102-150405")
	snapshotDir := "/var/lib/gearbox-agent/snapshots"

	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	output, err := c.runCommand("rpm", "-qa", "--queryformat", "%{NAME}\t%{VERSION}-%{RELEASE}\n")
	if err != nil {
		return nil, fmt.Errorf("rpm -qa failed: %w", err)
	}

	snapshotFile := fmt.Sprintf("%s/%s.versions", snapshotDir, snapshotID)
	if err := os.WriteFile(snapshotFile, output, 0644); err != nil {
		return nil, fmt.Errorf("failed to save snapshot: %w", err)
	}

	// Write a selections-compatible placeholder so ListSnapshots works
	if err := os.WriteFile(fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID), []byte("# rpm snapshot\n"), 0644); err != nil {
		return nil, fmt.Errorf("failed to save snapshot placeholder: %w", err)
	}

	metaContent := fmt.Sprintf("timestamp=%s\nreason=%s\n", timestamp.Format(time.RFC3339), reason)
	if err := os.WriteFile(fmt.Sprintf("%s/%s.meta", snapshotDir, snapshotID), []byte(metaContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to save snapshot metadata: %w", err)
	}

	return &AptSnapshot{ID: snapshotID, CreatedAt: timestamp, Reason: reason}, nil
}

func (d *dnfPackageManager) HoldPackage(_ string) error            { return errNotSupported("dnf") }
func (d *dnfPackageManager) UnholdPackage(_ string) error          { return errNotSupported("dnf") }
func (d *dnfPackageManager) ListHeldPackages() ([]string, error)   { return nil, errNotSupported("dnf") }
