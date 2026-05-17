package models

// SettingsPage is a single entry on the /settings root grid. The Settings
// template renders one card per entry; the Cmd+K palette serializes the
// same slice into its `cmdk-pages` JSON island. Both consumers go through
// SettingsPagesFor so permission gating lives in exactly one place — if
// it drifted, the palette would either dangle 403 destinations or hide
// pages users can in fact reach (PR #76 Copilot review).
type SettingsPage struct {
	// Name is the stable kebab-case slug used as the palette item id and
	// as the icon-key switch on the Settings card renderer. Must be unique.
	Name string
	// Label is the user-facing title shown on the card and in the palette.
	Label string
	// Description is the one-line card subtitle. Empty for palette-only
	// entries (none today, kept as an explicit option).
	Description string
	// Path is the destination URL.
	Path string
}

// SettingsPagesFor returns the ordered list of settings pages the given
// user can reach. The Settings template iterates this for card layout;
// encodePagesJSON iterates the same slice to populate the Cmd+K palette.
// Adding a new settings page = appending an entry here once.
//
// perms may be nil for users whose permission record hasn't loaded — we
// treat that as "no extra permissions beyond role" so admins still see
// everything they should.
func SettingsPagesFor(user *User, perms *UserPermissions) []SettingsPage {
	isAdmin := user != nil && user.IsAdmin()
	hasPerm := func(c Component, p Permission) bool {
		return perms != nil && perms.HasPermission(c, p)
	}

	pages := make([]SettingsPage, 0, 10)
	if isAdmin || hasPerm(ComponentSettings, PermissionManageBoxes) {
		pages = append(pages, SettingsPage{
			Name:        "boxes",
			Label:       "Boxes",
			Description: "Manage monitored boxes",
			Path:        "/settings/boxes",
		})
	}
	if isAdmin || hasPerm(ComponentGears, PermissionManage) {
		pages = append(pages, SettingsPage{
			Name:        "gears",
			Label:       "Gears",
			Description: "Enable/disable features and configure gears",
			Path:        "/settings/gears",
		})
	}
	pages = append(pages, SettingsPage{
		Name:        "profile",
		Label:       "Profile",
		Description: "Update your personal information and passkeys",
		Path:        "/settings/profile",
	})
	if isAdmin || hasPerm(ComponentUsers, PermissionApproveUsers) {
		pages = append(pages, SettingsPage{
			Name:        "users",
			Label:       "User Management",
			Description: "Manage users and account requests",
			Path:        "/settings/users",
		})
	}
	if isAdmin {
		pages = append(pages, SettingsPage{
			Name:        "smtp",
			Label:       "Email Settings",
			Description: "Configure SMTP server for email notifications",
			Path:        "/settings/smtp",
		})
	}
	pages = append(pages, SettingsPage{
		Name:        "backups",
		Label:       "Database Backups",
		Description: "Create and restore database backups",
		Path:        "/settings/backup",
	})
	if isAdmin {
		pages = append(pages, SettingsPage{
			Name:        "permissions",
			Label:       "Permission Management",
			Description: "Manage granular permissions for users",
			Path:        "/settings/permissions",
		})
	}
	return pages
}
