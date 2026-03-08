package updates

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// distroInfo holds parsed /etc/os-release fields.
type distroInfo struct {
	ID         string // e.g. "ubuntu", "fedora", "arch"
	IDLike     string // e.g. "debian", "rhel fedora"
	Name       string // e.g. "Ubuntu 22.04 LTS"
	VersionID  string // e.g. "22.04"
}

var (
	detectedPM   PackageManager
	detectedOnce sync.Once
)

// DetectPackageManager returns the PackageManager implementation for this system.
// The result is cached after the first call.
func DetectPackageManager() PackageManager {
	detectedOnce.Do(func() {
		detectedPM = detectPM()
		slog.Info("package manager detected", "name", detectedPM.Name())
	})
	return detectedPM
}

// detectPM performs the actual detection logic.
func detectPM() PackageManager {
	info := readOSRelease()

	// Check by ID and ID_LIKE fields first (most reliable)
	ids := strings.Fields(strings.ToLower(info.ID + " " + info.IDLike))
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu", "mint", "linuxmint", "pop", "elementary", "kali", "raspbian":
			if pm := tryApt(); pm != nil {
				return pm
			}
		case "fedora":
			if pm := tryDnf(); pm != nil {
				return pm
			}
		case "rhel", "centos", "almalinux", "rocky", "ol":
			// RHEL 8+ uses dnf; RHEL 7 / CentOS 7 uses yum
			if pm := tryDnf(); pm != nil {
				return pm
			}
			if pm := tryYum(); pm != nil {
				return pm
			}
		case "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles", "sled":
			if pm := tryZypper(); pm != nil {
				return pm
			}
		case "arch", "manjaro", "endeavouros", "garuda":
			if pm := tryPacman(); pm != nil {
				return pm
			}
		case "alpine":
			if pm := tryApk(); pm != nil {
				return pm
			}
		}
	}

	// Fallback: probe by binary presence in PATH order of popularity
	if pm := tryApt(); pm != nil {
		return pm
	}
	if pm := tryDnf(); pm != nil {
		return pm
	}
	if pm := tryYum(); pm != nil {
		return pm
	}
	if pm := tryZypper(); pm != nil {
		return pm
	}
	if pm := tryPacman(); pm != nil {
		return pm
	}
	if pm := tryApk(); pm != nil {
		return pm
	}

	// No known package manager found — return a no-op implementation so the
	// rest of the gear degrades gracefully instead of panicking.
	slog.Warn("no supported package manager found", "os_id", info.ID, "os_id_like", info.IDLike)
	return &unsupportedPM{name: info.Name}
}

func tryApt() PackageManager {
	if _, err := exec.LookPath("apt-get"); err == nil {
		return NewAptPackageManager()
	}
	return nil
}

func tryDnf() PackageManager {
	if _, err := exec.LookPath("dnf"); err == nil {
		return NewDnfPackageManager()
	}
	return nil
}

func tryYum() PackageManager {
	if _, err := exec.LookPath("yum"); err == nil {
		return NewYumPackageManager()
	}
	return nil
}

func tryZypper() PackageManager {
	if _, err := exec.LookPath("zypper"); err == nil {
		return NewZypperPackageManager()
	}
	return nil
}

func tryPacman() PackageManager {
	if _, err := exec.LookPath("pacman"); err == nil {
		return NewPacmanPackageManager()
	}
	return nil
}

func tryApk() PackageManager {
	if _, err := exec.LookPath("apk"); err == nil {
		return NewApkPackageManager()
	}
	return nil
}

// readOSRelease parses /etc/os-release (with /usr/lib/os-release as fallback).
func readOSRelease() distroInfo {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if info, ok := parseOSReleaseFile(path); ok {
			return info
		}
	}
	return distroInfo{}
}

func parseOSReleaseFile(path string) (distroInfo, bool) {
	f, err := os.Open(path) //#nosec G304
	if err != nil {
		return distroInfo{}, false
	}
	defer f.Close()

	info := distroInfo{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)

		switch key {
		case "ID":
			info.ID = strings.ToLower(val)
		case "ID_LIKE":
			info.IDLike = strings.ToLower(val)
		case "NAME":
			info.Name = val
		case "VERSION_ID":
			info.VersionID = val
		}
	}
	return info, true
}

// unsupportedPM is returned when no known package manager is found.
// All methods return appropriate "not supported" errors or empty results
// so the rest of the gear doesn't panic.
type unsupportedPM struct {
	name string
}

func (u *unsupportedPM) Name() string        { return "unsupported" }
func (u *unsupportedPM) Available() bool     { return false }
func (u *unsupportedPM) IsAutoUpdateActive() bool { return false }

func (u *unsupportedPM) CheckUpdates() (*UpdateInfo, error) {
	return &UpdateInfo{Available: false}, nil
}

func (u *unsupportedPM) ListUpgradable() ([]Package, error) {
	return nil, errNotSupported(u.name)
}

func (u *unsupportedPM) TriggerUpdateCheck() error {
	return errNotSupported(u.name)
}

func (u *unsupportedPM) InstallUpdates(_ bool, _ []string) ([]string, error) {
	return nil, errNotSupported(u.name)
}

func (u *unsupportedPM) InstallPackage(_ string) error {
	return errNotSupported(u.name)
}

func (u *unsupportedPM) RemovePackage(_ string, _ bool) error {
	return errNotSupported(u.name)
}

func (u *unsupportedPM) ListInstalledPackages() ([]InstalledPackage, error) {
	return nil, errNotSupported(u.name)
}

func (u *unsupportedPM) SearchPackages(_ string, _ int) ([]InstalledPackage, error) {
	return nil, errNotSupported(u.name)
}

func (u *unsupportedPM) GetUpdateHistory(_ int) ([]UpdateHistoryEntry, error) {
	return nil, errNotSupported(u.name)
}

func (u *unsupportedPM) GetAutoUpdateConfig() (map[string]any, error) {
	return nil, errNotSupported(u.name)
}

func (u *unsupportedPM) ConfigureAutoUpdate(_ bool, _ bool) error {
	return errNotSupported(u.name)
}

func (u *unsupportedPM) GetRebootRequired() (bool, []string, error) {
	return false, nil, nil
}

func (u *unsupportedPM) CreateSnapshot(_ string) (*AptSnapshot, error) {
	return nil, errNotSupported(u.name)
}

func (u *unsupportedPM) RestoreSnapshot(_ string) error {
	return errNotSupported(u.name)
}

func (u *unsupportedPM) BuildInstallCommand(_ []string, _ bool) []string { return nil }
func (u *unsupportedPM) BuildUpgradeCommand() []string                   { return nil }
func (u *unsupportedPM) BuildUpdateCheckCommand() []string               { return nil }
func (u *unsupportedPM) EnvVars() []string                               { return nil }

func (u *unsupportedPM) HoldPackage(_ string) error                              { return errNotSupported(u.name) }
func (u *unsupportedPM) UnholdPackage(_ string) error                            { return errNotSupported(u.name) }
func (u *unsupportedPM) ListHeldPackages() ([]string, error)                     { return nil, nil }

// PackageURL returns a best-effort URL to the package info page for the given
// package manager and package name. Returns empty string if no URL is known.
func PackageURL(pmName, pkgName string) string {
	switch pmName {
	case "apt":
		info := readOSRelease()
		id := strings.ToLower(info.ID)
		switch {
		case id == "ubuntu" || strings.Contains(strings.ToLower(info.IDLike), "ubuntu"):
			return "https://packages.ubuntu.com/search?keywords=" + pkgName
		default: // debian and derivatives
			return "https://packages.debian.org/" + pkgName
		}
	case "dnf", "yum":
		info := readOSRelease()
		id := strings.ToLower(info.ID)
		if id == "fedora" {
			return "https://packages.fedoraproject.org/pkgs/" + pkgName
		}
		// AlmaLinux / Rocky / RHEL — no universal public URL
		return ""
	case "zypper":
		return "https://software.opensuse.org/package/" + pkgName
	case "pacman":
		return "https://archlinux.org/packages/?q=" + pkgName
	case "apk":
		return "https://pkgs.alpinelinux.org/packages?name=" + pkgName
	}
	return ""
}

func errNotSupported(osName string) error {
	if osName == "" {
		osName = "this system"
	}
	return fmt.Errorf("package management not supported on %s", osName)
}
