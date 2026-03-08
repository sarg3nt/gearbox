// Package updates provides OS update and package management functionality.
package updates

// PackageManager defines the interface for interacting with a system package manager.
// Implementations exist for apt (Debian/Ubuntu), dnf (Fedora/RHEL 8+), yum (CentOS/RHEL 7),
// zypper (openSUSE/SLES), pacman (Arch Linux), and apk (Alpine Linux).
type PackageManager interface {
	// Name returns the human-readable name of this package manager (e.g. "apt", "dnf").
	Name() string

	// Available returns true if this package manager is present on the system.
	Available() bool

	// CheckUpdates returns a summary of available updates.
	CheckUpdates() (*UpdateInfo, error)

	// ListUpgradable returns the list of packages that can be upgraded.
	ListUpgradable() ([]Package, error)

	// TriggerUpdateCheck refreshes the local package cache (e.g. apt-get update, dnf check-update).
	TriggerUpdateCheck() error

	// InstallUpdates installs available updates.
	// If securityOnly is true, only security updates are applied.
	// If packages is non-empty, only those named packages are upgraded/installed.
	InstallUpdates(securityOnly bool, packages []string) ([]string, error)

	// InstallPackage installs a new package by name.
	InstallPackage(name string) error

	// RemovePackage removes an installed package.
	// If purge is true, configuration files are also removed.
	RemovePackage(name string, purge bool) error

	// ListInstalledPackages returns all currently installed packages.
	ListInstalledPackages() ([]InstalledPackage, error)

	// SearchPackages searches the available package index for packages matching query.
	SearchPackages(query string, limit int) ([]InstalledPackage, error)

	// GetUpdateHistory returns recent package operation history.
	GetUpdateHistory(limit int) ([]UpdateHistoryEntry, error)

	// IsAutoUpdateActive returns true if automatic updates are enabled.
	IsAutoUpdateActive() bool

	// GetAutoUpdateConfig returns the current auto-update configuration.
	GetAutoUpdateConfig() (map[string]any, error)

	// ConfigureAutoUpdate enables or disables automatic updates.
	// autoReboot controls whether the system may reboot automatically after updates.
	ConfigureAutoUpdate(enabled, autoReboot bool) error

	// GetRebootRequired returns true if a reboot is required, along with the
	// list of packages (if available) that triggered the requirement.
	GetRebootRequired() (required bool, packages []string, err error)

	// CreateSnapshot captures the current package state for later rollback.
	// reason is a short description (e.g. "auto: before upgrading 5 packages").
	CreateSnapshot(reason string) (*AptSnapshot, error)

	// RestoreSnapshot restores the package state from a previous snapshot.
	// This is a streaming-unfriendly synchronous operation; use AptRunner for streaming.
	RestoreSnapshot(snapshotID string) error

	// BuildInstallCommand returns the exec args to install the given packages.
	// Used by the streaming runner.
	BuildInstallCommand(packages []string, securityOnly bool) []string

	// BuildUpgradeCommand returns the exec args to upgrade all packages.
	BuildUpgradeCommand() []string

	// BuildUpdateCheckCommand returns the exec args to refresh the package cache.
	BuildUpdateCheckCommand() []string

	// EnvVars returns environment variables that should be set when running
	// package manager commands (e.g. DEBIAN_FRONTEND=noninteractive for apt).
	EnvVars() []string

	// HoldPackage marks a package as held so it will not be upgraded.
	HoldPackage(name string) error

	// UnholdPackage removes the hold from a package, allowing it to be upgraded again.
	UnholdPackage(name string) error

	// ListHeldPackages returns the names of all currently held packages.
	ListHeldPackages() ([]string, error)

}
