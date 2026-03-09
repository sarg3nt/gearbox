package updates

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// aptPackageManager implements PackageManager for Debian/Ubuntu (apt/dpkg).
type aptPackageManager struct {
	collector *UpdatesCollector
}

// NewAptPackageManager returns a PackageManager backed by apt/apt-get/dpkg.
func NewAptPackageManager() PackageManager {
	return &aptPackageManager{collector: NewUpdatesCollector()}
}

func (a *aptPackageManager) Name() string    { return "apt" }
func (a *aptPackageManager) Available() bool { _, err := exec.LookPath("apt-get"); return err == nil }

func (a *aptPackageManager) EnvVars() []string {
	return []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=a"}
}

func (a *aptPackageManager) BuildUpdateCheckCommand() []string {
	return []string{"apt-get", "update"}
}

func (a *aptPackageManager) BuildUpgradeCommand() []string {
	return []string{"apt-get", "upgrade", "-y"}
}

func (a *aptPackageManager) BuildInstallCommand(packages []string, securityOnly bool) []string {
	if len(packages) > 0 {
		return append([]string{"apt-get", "install", "-y"}, packages...)
	}
	if securityOnly {
		return []string{"unattended-upgrade", "--minimal-upgrade-steps"}
	}
	return []string{"apt-get", "upgrade", "-y"}
}

func (a *aptPackageManager) IsAutoUpdateActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "unattended-upgrades").Run() == nil
}

func (a *aptPackageManager) CheckUpdates() (*UpdateInfo, error) {
	info := &UpdateInfo{Available: true, LastCheck: time.Now()}

	output, err := a.collector.runCommand("apt", "list", "--upgradable")
	if err != nil {
		info.TotalUpdates = 0
	} else {
		packages := a.parseAptListOutput(string(output))
		info.TotalUpdates = len(packages)
		for _, pkg := range packages {
			if pkg.IsSecurityUpdate {
				info.SecurityUpdates++
			}
		}
	}

	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		info.RebootRequired = true
	}
	info.UnattendedActive = a.IsAutoUpdateActive()
	return info, nil
}

func (a *aptPackageManager) ListUpgradable() ([]Package, error) {
	output, err := a.collector.runCommand("apt", "list", "--upgradable")
	if err != nil {
		return nil, fmt.Errorf("apt list --upgradable failed: %w", err)
	}
	packages := a.parseAptListOutput(string(output))
	a.fetchPackageSizes(packages)
	return packages, nil
}

func (a *aptPackageManager) TriggerUpdateCheck() error {
	_, err := a.collector.runCommandWithOutput("apt-get", "update")
	if err != nil {
		return fmt.Errorf("apt update failed: %w", err)
	}
	return nil
}

func (a *aptPackageManager) InstallUpdates(securityOnly bool, packages []string) ([]string, error) {
	if len(packages) > 0 {
		_, err := a.collector.runCommandWithOutput("apt-get", append([]string{"install", "-y"}, packages...)...)
		if err != nil {
			return nil, fmt.Errorf("apt upgrade failed: %w", err)
		}
		return packages, nil
	}
	if securityOnly {
		output, err := a.collector.runCommand("unattended-upgrade", "--dry-run", "-d")
		if err != nil {
			return nil, fmt.Errorf("failed to check security updates: %w", err)
		}
		secPkgs := a.parseUnattendedUpgradeOutput(string(output))
		if len(secPkgs) == 0 {
			return nil, nil
		}
		if _, err := a.collector.runCommand("unattended-upgrade"); err != nil {
			return nil, fmt.Errorf("failed to install security updates: %w", err)
		}
		return secPkgs, nil
	}
	_, err := a.collector.runCommandWithOutput("apt-get", "upgrade", "-y")
	if err != nil {
		return nil, fmt.Errorf("apt upgrade failed: %w", err)
	}
	return nil, nil
}

func (a *aptPackageManager) InstallPackage(name string) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	_, err := a.collector.runCommandWithOutput("apt-get", "install", "-y", name)
	if err != nil {
		return fmt.Errorf("failed to install package %s: %w", name, err)
	}
	return nil
}

func (a *aptPackageManager) RemovePackage(name string, purge bool) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	action := "remove"
	if purge {
		action = "purge"
	}
	_, err := a.collector.runCommandWithOutput("apt-get", action, "-y", name)
	if err != nil {
		return fmt.Errorf("failed to remove package %s: %w", name, err)
	}
	return nil
}

func (a *aptPackageManager) ListInstalledPackages() ([]InstalledPackage, error) {
	output, err := a.collector.runCommand("dpkg-query", "-W",
		"-f", "${Package}\t${Version}\t${Architecture}\t${db:Status-Abbrev}\t${binary:Summary}\n")
	if err != nil {
		return nil, fmt.Errorf("dpkg-query failed: %w", err)
	}

	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 5)
		if len(parts) < 4 {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(parts[3]), "ii") {
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

func (a *aptPackageManager) SearchPackages(query string, limit int) ([]InstalledPackage, error) {
	if limit <= 0 {
		limit = 50
	}
	output, err := a.collector.runCommand("apt-cache", "search", query)
	if err != nil {
		return nil, fmt.Errorf("apt-cache search failed: %w", err)
	}

	installedSet := make(map[string]bool)
	if out, err := a.collector.runCommand("dpkg-query", "-W", "-f", "${Package}\t${db:Status-Abbrev}\n"); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(out)))
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
		parts := strings.SplitN(scanner.Text(), " - ", 2)
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

func (a *aptPackageManager) GetUpdateHistory(limit int) ([]UpdateHistoryEntry, error) {
	return parseAptHistory(limit)
}

// parseAptHistory reads dpkg.log and apt/history.log to build update history.
func parseAptHistory(limit int) ([]UpdateHistoryEntry, error) {
	var history []UpdateHistoryEntry

	// Primary source: dpkg.log
	for _, logFile := range []string{"/var/log/dpkg.log.1", "/var/log/dpkg.log"} {
		history = append(history, parseDpkgLog(logFile)...)
	}

	// Fallback/supplement: apt history.log
	aptHistory := parseAptHistoryLog("/var/log/apt/history.log")
	if len(history) == 0 {
		history = aptHistory
	}

	sort.Slice(history, func(i, j int) bool {
		return history[i].Timestamp.After(history[j].Timestamp)
	})

	// Deduplicate
	if len(history) > 1 {
		deduped := []UpdateHistoryEntry{history[0]}
		for i := 1; i < len(history); i++ {
			prev := deduped[len(deduped)-1]
			curr := history[i]
			if curr.Package != prev.Package || curr.Action != prev.Action || !curr.Timestamp.Equal(prev.Timestamp) {
				deduped = append(deduped, curr)
			}
		}
		history = deduped
	}

	if limit > 0 && len(history) > limit {
		history = history[:limit]
	}
	return history, nil
}

func parseDpkgLog(logFile string) []UpdateHistoryEntry {
	var history []UpdateHistoryEntry

	file, err := os.Open(logFile) //#nosec G304
	if err != nil {
		return history
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 4 {
			continue
		}
		t, err := time.Parse("2006-01-02 15:04:05", parts[0]+" "+parts[1])
		if err != nil {
			continue
		}
		action, status := parts[2], parts[3]
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
			entry.Action = "upgrade"
			if len(parts) >= 5 {
				entry.Package = strings.Split(parts[4], ":")[0]
			}
			if len(parts) >= 6 {
				entry.ToVersion = parts[5]
			}
		case action == "status" && status == "installed" && len(parts) >= 6:
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

func parseAptHistoryLog(logFile string) []UpdateHistoryEntry {
	var history []UpdateHistoryEntry

	file, err := os.Open(logFile) //#nosec G304
	if err != nil {
		return history
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	var ts time.Time
	var curStatus string
	inEntry := false

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Start-Date:"):
			dateStr := strings.TrimSpace(strings.TrimPrefix(line, "Start-Date:"))
			if t, err := time.Parse("2006-01-02  15:04:05", dateStr); err == nil {
				ts = t
				curStatus = "success"
				inEntry = true
			}
		case inEntry && (strings.HasPrefix(line, "Upgrade:") || strings.HasPrefix(line, "Install:") ||
			strings.HasPrefix(line, "Remove:") || strings.HasPrefix(line, "Purge:")):
			var action, pkgLine string
			switch {
			case strings.HasPrefix(line, "Upgrade:"):
				action, pkgLine = "upgrade", strings.TrimPrefix(line, "Upgrade: ")
			case strings.HasPrefix(line, "Install:"):
				action, pkgLine = "install", strings.TrimPrefix(line, "Install: ")
			case strings.HasPrefix(line, "Remove:"):
				action, pkgLine = "remove", strings.TrimPrefix(line, "Remove: ")
			case strings.HasPrefix(line, "Purge:"):
				action, pkgLine = "remove", strings.TrimPrefix(line, "Purge: ")
			}
			for _, e := range strings.Split(pkgLine, "),") {
				e = strings.TrimSpace(e)
				if e == "" {
					continue
				}
				he := UpdateHistoryEntry{Timestamp: ts, Action: action, Status: curStatus}
				if idx := strings.Index(e, " ("); idx > 0 {
					he.Package = strings.Split(strings.TrimSpace(e[:idx]), ":")[0]
					verPart := strings.TrimRight(e[idx+2:], ")")
					vers := strings.Split(verPart, ", ")
					if len(vers) == 2 {
						he.FromVersion = strings.TrimSpace(vers[0])
						he.ToVersion = strings.TrimSpace(vers[1])
					} else if len(vers) == 1 {
						he.ToVersion = strings.TrimSpace(vers[0])
					}
				} else {
					he.Package = strings.Split(strings.TrimSpace(e), ":")[0]
				}
				if he.Package != "" {
					history = append(history, he)
				}
			}
		case strings.HasPrefix(line, "Error:") && inEntry:
			curStatus = "failed"
		case strings.HasPrefix(line, "End-Date:") && inEntry:
			inEntry = false
		}
	}
	return history
}

func (a *aptPackageManager) GetAutoUpdateConfig() (map[string]any, error) {
	config := make(map[string]any)
	config["enabled"] = a.IsAutoUpdateActive()

	if data, err := os.ReadFile("/etc/apt/apt.conf.d/20auto-upgrades"); err == nil {
		content := string(data)
		config["auto_update"] = strings.Contains(content, `APT::Periodic::Update-Package-Lists "1"`)
		config["auto_upgrade"] = strings.Contains(content, `APT::Periodic::Unattended-Upgrade "1"`)
	}
	if data, err := os.ReadFile("/etc/apt/apt.conf.d/50unattended-upgrades"); err == nil {
		content := string(data)
		config["auto_reboot"] = strings.Contains(content, `Unattended-Upgrade::Automatic-Reboot "true"`)
		config["mail_on_error"] = strings.Contains(content, `Unattended-Upgrade::Mail`)
	}
	return config, nil
}

func (a *aptPackageManager) ConfigureAutoUpdate(enabled, autoReboot bool) error {
	if !enabled {
		_, err := a.collector.runCommandWithOutput("systemctl", "disable", "--now", "unattended-upgrades")
		return err
	}
	_, err := a.collector.runCommandWithOutput("systemctl", "enable", "--now", "unattended-upgrades")
	if err != nil {
		return fmt.Errorf("failed to enable unattended-upgrades: %w", err)
	}
	autoUpdateContent := "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\nAPT::Periodic::AutocleanInterval \"7\";\n"
	return os.WriteFile("/etc/apt/apt.conf.d/20auto-upgrades", []byte(autoUpdateContent), 0644) //#nosec G306
}

func (a *aptPackageManager) GetRebootRequired() (bool, []string, error) {
	if _, err := os.Stat("/var/run/reboot-required"); err != nil {
		if os.IsNotExist(err) {
			return false, nil, nil
		}
		return false, nil, err
	}
	// Read packages that triggered the reboot requirement
	data, err := os.ReadFile("/var/run/reboot-required.pkgs")
	if err != nil {
		return true, nil, nil
	}
	var pkgs []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			pkgs = append(pkgs, line)
		}
	}
	return true, pkgs, nil
}

func (a *aptPackageManager) CreateSnapshot(reason string) (*AptSnapshot, error) {
	timestamp := time.Now()
	snapshotID := timestamp.Format("20060102-150405")
	snapshotDir := "/var/lib/gearbox-agent/snapshots"

	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	output, err := a.collector.runCommand("dpkg", "--get-selections")
	if err != nil {
		return nil, fmt.Errorf("failed to get package selections: %w", err)
	}
	if err := os.WriteFile(fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID), output, 0644); err != nil { //#nosec G306
		return nil, fmt.Errorf("failed to save snapshot: %w", err)
	}

	if versionsOutput, err := a.collector.runCommand("dpkg-query", "-W", "-f", "${Package}\t${Version}\n"); err == nil {
		_ = os.WriteFile(fmt.Sprintf("%s/%s.versions", snapshotDir, snapshotID), versionsOutput, 0644) //#nosec G306
	}

	metaContent := fmt.Sprintf("timestamp=%s\nreason=%s\n", timestamp.Format(time.RFC3339), reason)
	if err := os.WriteFile(fmt.Sprintf("%s/%s.meta", snapshotDir, snapshotID), []byte(metaContent), 0644); err != nil { //#nosec G306
		return nil, fmt.Errorf("failed to save snapshot metadata: %w", err)
	}
	return &AptSnapshot{ID: snapshotID, CreatedAt: timestamp, Reason: reason}, nil
}

func (a *aptPackageManager) RestoreSnapshot(snapshotID string) error {
	if err := validateSnapshotID(snapshotID); err != nil {
		return err
	}
	snapshotDir := "/var/lib/gearbox-agent/snapshots"
	snapshotFile := fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID)

	if _, err := os.Stat(snapshotFile); err != nil {
		return fmt.Errorf("snapshot not found: %s", snapshotID)
	}
	selectionsData, err := os.ReadFile(snapshotFile) //#nosec G304
	if err != nil {
		return fmt.Errorf("failed to read snapshot: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.collector.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dpkg", "--set-selections")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmd.Stdin = strings.NewReader(string(selectionsData))
	if output, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("dpkg --set-selections timed out")
		}
		return fmt.Errorf("failed to set package selections: %s: %w", strings.TrimSpace(string(output)), err)
	}

	_, err = a.collector.runCommandWithOutput("apt-get", "dselect-upgrade", "-y")
	return err
}

// --- internal helpers ---

func (a *aptPackageManager) parseAptListOutput(output string) []Package {
	var packages []Package
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

func (a *aptPackageManager) fetchPackageSizes(packages []Package) {
	if len(packages) == 0 {
		return
	}
	var specs []string
	for _, p := range packages {
		specs = append(specs, p.Name+"="+p.AvailableVersion)
	}
	args := append([]string{"show"}, specs...)
	output, err := a.collector.runCommand("apt-cache", args...)
	if err != nil {
		return
	}
	sizeMap := make(map[string]int64)
	var currentPkg string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Package: ") {
			currentPkg = strings.TrimPrefix(line, "Package: ")
		} else if strings.HasPrefix(line, "Size: ") && currentPkg != "" {
			if size, err := strconv.ParseInt(strings.TrimPrefix(line, "Size: "), 10, 64); err == nil {
				sizeMap[currentPkg] = size
			}
		}
	}
	for i := range packages {
		if size, ok := sizeMap[packages[i].Name]; ok {
			packages[i].Size = size
		}
	}
}

func (a *aptPackageManager) parseUnattendedUpgradeOutput(output string) []string {
	var packages []string
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
			if strings.TrimSpace(line) == "" {
				break
			}
			packages = append(packages, strings.Fields(line)...)
		}
	}
	return packages
}


// HoldPackage marks a package as held using apt-mark, preventing upgrades.
func (a *aptPackageManager) HoldPackage(name string) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "apt-mark", "hold", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("apt-mark hold %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// UnholdPackage removes the hold from a package using apt-mark.
func (a *aptPackageManager) UnholdPackage(name string) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "apt-mark", "unhold", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("apt-mark unhold %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ListHeldPackages returns the names of all currently held packages.
func (a *aptPackageManager) ListHeldPackages() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "apt-mark", "showhold").Output()
	if err != nil {
		return nil, fmt.Errorf("apt-mark showhold: %w", err)
	}
	var held []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if name := strings.TrimSpace(scanner.Text()); name != "" {
			held = append(held, name)
		}
	}
	return held, nil
}
