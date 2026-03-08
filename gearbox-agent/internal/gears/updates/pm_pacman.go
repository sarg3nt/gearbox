package updates

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// pacmanPackageManager implements PackageManager for Arch Linux / Manjaro.
type pacmanPackageManager struct {
	collector *UpdatesCollector
}

// NewPacmanPackageManager returns a PackageManager backed by pacman.
func NewPacmanPackageManager() PackageManager {
	return &pacmanPackageManager{collector: NewUpdatesCollector()}
}

func (p *pacmanPackageManager) Name() string    { return "pacman" }
func (p *pacmanPackageManager) Available() bool { _, err := exec.LookPath("pacman"); return err == nil }
func (p *pacmanPackageManager) EnvVars() []string { return nil }

func (p *pacmanPackageManager) BuildUpdateCheckCommand() []string {
	return []string{"pacman", "-Sy"}
}

func (p *pacmanPackageManager) BuildUpgradeCommand() []string {
	return []string{"pacman", "--noconfirm", "-Su"}
}

func (p *pacmanPackageManager) BuildInstallCommand(packages []string, _ bool) []string {
	if len(packages) > 0 {
		return append([]string{"pacman", "--noconfirm", "-S"}, packages...)
	}
	return []string{"pacman", "--noconfirm", "-Su"}
}

func (p *pacmanPackageManager) IsAutoUpdateActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "pacman-upgrades.timer").Run() == nil
}

func (p *pacmanPackageManager) CheckUpdates() (*UpdateInfo, error) {
	info := &UpdateInfo{Available: true, LastCheck: time.Now()}

	// Refresh DB first (non-invasive)
	_, _ = p.collector.runCommand("pacman", "-Sy")

	output, err := p.collector.runCommand("pacman", "-Qu")
	if err != nil && len(output) == 0 {
		// No updates
		return info, nil
	}

	packages := p.parseUpgradableOutput(string(output))
	info.TotalUpdates = len(packages)
	// pacman doesn't natively distinguish security updates
	info.UnattendedActive = p.IsAutoUpdateActive()
	return info, nil
}

func (p *pacmanPackageManager) ListUpgradable() ([]Package, error) {
	output, err := p.collector.runCommand("pacman", "-Qu")
	if err != nil && len(output) == 0 {
		return nil, nil
	}
	return p.parseUpgradableOutput(string(output)), nil
}

func (p *pacmanPackageManager) TriggerUpdateCheck() error {
	_, err := p.collector.runCommandWithOutput("pacman", "-Sy")
	if err != nil {
		return fmt.Errorf("pacman -Sy failed: %w", err)
	}
	return nil
}

func (p *pacmanPackageManager) InstallUpdates(_ bool, packages []string) ([]string, error) {
	var args []string
	if len(packages) > 0 {
		args = append([]string{"pacman", "--noconfirm", "-S"}, packages...)
	} else {
		args = []string{"pacman", "--noconfirm", "-Su"}
	}
	_, err := p.collector.runCommandWithOutput(args[0], args[1:]...)
	if err != nil {
		return nil, fmt.Errorf("pacman upgrade failed: %w", err)
	}
	return packages, nil
}

func (p *pacmanPackageManager) InstallPackage(name string) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	_, err := p.collector.runCommandWithOutput("pacman", "--noconfirm", "-S", name)
	if err != nil {
		return fmt.Errorf("failed to install %s: %w", name, err)
	}
	return nil
}

func (p *pacmanPackageManager) RemovePackage(name string, purge bool) error {
	if !isValidPackageName(name) {
		return fmt.Errorf("invalid package name: %s", name)
	}
	args := []string{"--noconfirm", "-R"}
	if purge {
		args = []string{"--noconfirm", "-Rns"} // remove with config files and orphans
	}
	_, err := p.collector.runCommandWithOutput("pacman", append(args, name)...)
	if err != nil {
		return fmt.Errorf("failed to remove %s: %w", name, err)
	}
	return nil
}

func (p *pacmanPackageManager) ListInstalledPackages() ([]InstalledPackage, error) {
	output, err := p.collector.runCommand("pacman", "-Q")
	if err != nil {
		return nil, fmt.Errorf("pacman -Q failed: %w", err)
	}
	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		packages = append(packages, InstalledPackage{
			Name:      parts[0],
			Version:   parts[1],
			Installed: true,
		})
	}
	return packages, nil
}

func (p *pacmanPackageManager) SearchPackages(query string, limit int) ([]InstalledPackage, error) {
	if limit <= 0 {
		limit = 50
	}
	output, err := p.collector.runCommand("pacman", "-Ss", query)
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("pacman -Ss failed: %w", err)
	}

	installedSet := p.installedSet()
	var packages []InstalledPackage
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	var lastName string
	for scanner.Scan() && len(packages) < limit {
		line := scanner.Text()
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			// Description line for the previous package
			if lastName != "" && len(packages) > 0 {
				packages[len(packages)-1].Description = strings.TrimSpace(line)
			}
			lastName = ""
			continue
		}
		// Package line: "repo/name version [Installed]"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		nameRepo := parts[0]
		nameOnly := nameRepo
		if slashIdx := strings.Index(nameRepo, "/"); slashIdx >= 0 {
			nameOnly = nameRepo[slashIdx+1:]
		}
		lastName = nameOnly
		packages = append(packages, InstalledPackage{
			Name:      nameOnly,
			Version:   parts[1],
			Installed: installedSet[nameOnly],
		})
	}
	return packages, nil
}

func (p *pacmanPackageManager) installedSet() map[string]bool {
	out, err := p.collector.runCommand("pacman", "-Qq")
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

func (p *pacmanPackageManager) GetUpdateHistory(limit int) ([]UpdateHistoryEntry, error) {
	return parsePacmanHistory(limit), nil
}

func (p *pacmanPackageManager) GetAutoUpdateConfig() (map[string]any, error) {
	return map[string]any{"enabled": p.IsAutoUpdateActive()}, nil
}

func (p *pacmanPackageManager) ConfigureAutoUpdate(enabled, _ bool) error {
	return fmt.Errorf("automatic updates are not natively supported by pacman; consider installing a helper like paru or an AUR package for scheduled updates")
}

func (p *pacmanPackageManager) GetRebootRequired() (bool, []string, error) {
	// Arch doesn't have a standard reboot-required mechanism;
	// check if kernel was updated since last boot by comparing uname -r with installed
	return false, nil, nil
}

func (p *pacmanPackageManager) CreateSnapshot(reason string) (*AptSnapshot, error) {
	timestamp := time.Now()
	snapshotID := timestamp.Format("20060102-150405")
	snapshotDir := "/var/lib/gearbox-agent/snapshots"

	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	output, err := p.collector.runCommand("pacman", "-Q")
	if err != nil {
		return nil, fmt.Errorf("pacman -Q failed: %w", err)
	}

	if err := os.WriteFile(fmt.Sprintf("%s/%s.versions", snapshotDir, snapshotID), output, 0644); err != nil {
		return nil, fmt.Errorf("failed to save snapshot: %w", err)
	}
	if err := os.WriteFile(fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID), []byte("# pacman snapshot\n"), 0644); err != nil {
		return nil, fmt.Errorf("failed to save snapshot placeholder: %w", err)
	}

	meta := fmt.Sprintf("timestamp=%s\nreason=%s\n", timestamp.Format(time.RFC3339), reason)
	if err := os.WriteFile(fmt.Sprintf("%s/%s.meta", snapshotDir, snapshotID), []byte(meta), 0644); err != nil {
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}

	return &AptSnapshot{ID: snapshotID, CreatedAt: timestamp, Reason: reason}, nil
}

func (p *pacmanPackageManager) RestoreSnapshot(_ string) error {
	return fmt.Errorf("snapshot restore is not yet supported for pacman; use downgrade or pacman manually")
}

func (p *pacmanPackageManager) parseUpgradableOutput(output string) []Package {
	var packages []Package
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		// Format: "name current_version -> new_version"
		parts := strings.Fields(scanner.Text())
		if len(parts) < 4 {
			continue
		}
		packages = append(packages, Package{
			Name:             parts[0],
			CurrentVersion:   parts[1],
			AvailableVersion: parts[3],
			Priority:         "medium",
		})
	}
	return packages
}

func parsePacmanHistory(limit int) []UpdateHistoryEntry {
	// Pacman log: /var/log/pacman.log
	data, err := os.ReadFile("/var/log/pacman.log")
	if err != nil {
		return nil
	}
	var history []UpdateHistoryEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		// Format: "[2024-01-15T10:30:45+0000] [ALPM] installed/upgraded/removed pkg (version)"
		if !strings.Contains(line, "[ALPM]") {
			continue
		}
		var action string
		switch {
		case strings.Contains(line, " installed "):
			action = "install"
		case strings.Contains(line, " upgraded "):
			action = "upgrade"
		case strings.Contains(line, " removed "):
			action = "remove"
		default:
			continue
		}

		// Parse timestamp: "[2024-01-15T10:30:45+0000]"
		start := strings.Index(line, "[")
		end := strings.Index(line, "]")
		if start < 0 || end <= start {
			continue
		}
		t, err := time.Parse("2006-01-02T15:04:05-0700", line[start+1:end])
		if err != nil {
			continue
		}

		// Parse package name from after "[ALPM] action "
		rest := line[end+1:]
		alpmIdx := strings.Index(rest, "[ALPM] ")
		if alpmIdx < 0 {
			continue
		}
		rest = rest[alpmIdx+7:]
		words := strings.Fields(rest)
		if len(words) < 2 {
			continue
		}
		pkg := words[1]

		entry := UpdateHistoryEntry{
			Timestamp: t,
			Action:    action,
			Package:   pkg,
			Status:    "success",
		}
		history = append(history, entry)
		if limit > 0 && len(history) >= limit {
			break
		}
	}
	return history
}

func (p *pacmanPackageManager) HoldPackage(_ string) error            { return errNotSupported("pacman") }
func (p *pacmanPackageManager) UnholdPackage(_ string) error          { return errNotSupported("pacman") }
func (p *pacmanPackageManager) ListHeldPackages() ([]string, error)   { return nil, errNotSupported("pacman") }
