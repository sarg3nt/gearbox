package updates

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// yumPackageManager implements PackageManager for CentOS 7 / RHEL 7 (yum).
type yumPackageManager struct {
	collector *UpdatesCollector
}

// NewYumPackageManager returns a PackageManager backed by yum.
func NewYumPackageManager() PackageManager {
	return &yumPackageManager{collector: NewUpdatesCollector()}
}

func (y *yumPackageManager) Name() string    { return "yum" }
func (y *yumPackageManager) Available() bool { _, err := exec.LookPath("yum"); return err == nil }
func (y *yumPackageManager) EnvVars() []string { return nil }

func (y *yumPackageManager) BuildUpdateCheckCommand() []string {
	return []string{"yum", "makecache", "-q"}
}

func (y *yumPackageManager) BuildUpgradeCommand() []string {
	return []string{"yum", "upgrade", "-y"}
}

func (y *yumPackageManager) BuildInstallCommand(packages []string, securityOnly bool) []string {
	if len(packages) > 0 {
		return append([]string{"yum", "install", "-y"}, packages...)
	}
	if securityOnly {
		return []string{"yum", "upgrade", "--security", "-y"}
	}
	return []string{"yum", "upgrade", "-y"}
}

func (y *yumPackageManager) IsAutoUpdateActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "yum-cron").Run() == nil
}

func (y *yumPackageManager) CheckUpdates() (*UpdateInfo, error) {
	info := &UpdateInfo{Available: true, LastCheck: time.Now()}

	output, err := y.collector.runCommand("yum", "check-update", "-q")
	_ = err // exit 100 = updates available

	packages := y.parseCheckUpdateOutput(string(output))
	info.TotalUpdates = len(packages)
	for _, p := range packages {
		if p.IsSecurityUpdate {
			info.SecurityUpdates++
		}
	}
	info.UnattendedActive = y.IsAutoUpdateActive()
	return info, nil
}

func (y *yumPackageManager) ListUpgradable() ([]Package, error) {
	output, err := y.collector.runCommand("yum", "check-update", "-q")
	_ = err
	return y.parseCheckUpdateOutput(string(output)), nil
}

func (y *yumPackageManager) TriggerUpdateCheck() error {
	_, err := y.collector.runCommandWithOutput("yum", "makecache")
	if err != nil {
		return fmt.Errorf("yum makecache failed: %w", err)
	}
	return nil
}

func (y *yumPackageManager) InstallUpdates(securityOnly bool, packages []string) ([]string, error) {
	var args []string
	if len(packages) > 0 {
		args = append([]string{"yum", "install", "-y"}, packages...)
	} else if securityOnly {
		args = []string{"yum", "upgrade", "--security", "-y"}
	} else {
		args = []string{"yum", "upgrade", "-y"}
	}
	_, err := y.collector.runCommandWithOutput(args[0], args[1:]...)
	if err != nil {
		return nil, fmt.Errorf("yum upgrade failed: %w", err)
	}
	return packages, nil
}

func (y *yumPackageManager) InstallPackage(name string) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	_, err := y.collector.runCommandWithOutput("yum", "install", "-y", name)
	if err != nil {
		return fmt.Errorf("failed to install %s: %w", name, err)
	}
	return nil
}

func (y *yumPackageManager) RemovePackage(name string, _ bool) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	_, err := y.collector.runCommandWithOutput("yum", "remove", "-y", name)
	if err != nil {
		return fmt.Errorf("failed to remove %s: %w", name, err)
	}
	return nil
}

func (y *yumPackageManager) ListInstalledPackages() ([]InstalledPackage, error) {
	output, err := y.collector.runCommand("rpm", "-qa", "--queryformat", "%{NAME}\t%{VERSION}-%{RELEASE}\t%{ARCH}\t%{SUMMARY}\n")
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

func (y *yumPackageManager) SearchPackages(query string, limit int) ([]InstalledPackage, error) {
	if limit <= 0 {
		limit = 50
	}
	output, err := y.collector.runCommand("yum", "search", "-q", query)
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("yum search failed: %w", err)
	}

	installedSet := y.installedSet()
	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() && len(packages) < limit {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, " : ") {
			continue
		}
		parts := strings.SplitN(line, " : ", 2)
		namePart := strings.TrimSpace(parts[0])
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

func (y *yumPackageManager) installedSet() map[string]bool {
	out, err := y.collector.runCommand("rpm", "-qa", "--queryformat", "%{NAME}\n")
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

func (y *yumPackageManager) GetUpdateHistory(limit int) ([]UpdateHistoryEntry, error) {
	return parseDnfHistory(y.collector, limit)
}

func (y *yumPackageManager) GetAutoUpdateConfig() (map[string]any, error) {
	return map[string]any{"enabled": y.IsAutoUpdateActive()}, nil
}

func (y *yumPackageManager) ConfigureAutoUpdate(enabled, _ bool) error {
	svc := "yum-cron"
	if enabled {
		if _, err := exec.LookPath("yum-cron"); err != nil {
			return fmt.Errorf("yum-cron is not installed; install it with: yum install yum-cron")
		}
		_, err := y.collector.runCommandWithOutput("systemctl", "enable", "--now", svc)
		return err
	}
	_, err := y.collector.runCommandWithOutput("systemctl", "disable", "--now", svc)
	return err
}

func (y *yumPackageManager) GetRebootRequired() (bool, []string, error) {
	if exec.Command("needs-restarting", "-r").Run() != nil {
		return true, nil, nil
	}
	return false, nil, nil
}

func (y *yumPackageManager) CreateSnapshot(reason string) (*AptSnapshot, error) {
	return createRPMSnapshot(y.collector, reason)
}

func (y *yumPackageManager) RestoreSnapshot(_ string) error {
	return fmt.Errorf("snapshot restore is not yet supported for yum; use 'yum history undo' manually")
}

func (y *yumPackageManager) parseCheckUpdateOutput(output string) []Package {
	var packages []Package
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Loaded") || strings.HasPrefix(line, "Obsoleting") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
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

func (y *yumPackageManager) HoldPackage(_ string) error            { return errNotSupported("yum") }
func (y *yumPackageManager) UnholdPackage(_ string) error          { return errNotSupported("yum") }
func (y *yumPackageManager) ListHeldPackages() ([]string, error)   { return nil, errNotSupported("yum") }
