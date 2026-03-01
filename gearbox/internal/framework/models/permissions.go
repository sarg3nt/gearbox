package models

import (
	"time"
)

// Permission represents a specific permission in the system.
type Permission string

// Component represents a component/resource type in the system.
type Component string

const (
	// Components (resources that can have permissions)
	ComponentDashboard        Component = "dashboard"
	ComponentDisabledEntities Component = "disabled_entities" // Manage disabled backends/services
	ComponentCertificates     Component = "certificates"
	ComponentLogs             Component = "logs"
	ComponentServices         Component = "services"
	ComponentMetrics          Component = "metrics"
	ComponentGears          Component = "gears"
	ComponentSettings         Component = "settings"
	ComponentUsers            Component = "users"
	ComponentHAProxyConfig    Component = "haproxy_config"   // HAProxy configuration editing
	ComponentFirewallConfig   Component = "firewall_config"  // Firewall/nftables configuration editing
	ComponentSecurity         Component = "security"         // Security dashboard, IP blocking, fail2ban
	ComponentAlerts           Component = "alerts"           // Alert management and configuration
	ComponentOSUpdates        Component = "os_updates"       // OS updates and package management
)

const (
	// Permission types
	PermissionView          Permission = "view"           // View component
	PermissionConfigure     Permission = "configure"      // Configure component settings (implies view)
	PermissionManage        Permission = "manage"         // Enable/disable entities, reorder (implies view)
	PermissionAction        Permission = "action"         // Perform actions (refresh certs, etc.) (implies view)
	PermissionDownload      Permission = "download"       // Download files (certificates, etc.) (implies view)
	PermissionApproveUsers  Permission = "approve_users"  // Approve new user accounts
	PermissionManageBoxes Permission = "manage_boxes" // Add/edit/delete monitored boxes
)

// PermissionGrant represents a permission granted to a user for a specific component.
type PermissionGrant struct {
	ID          int64      `json:"id"`
	UserID      string     `json:"user_id"` // UUID
	Component   Component  `json:"component"`
	Permission  Permission `json:"permission"`
	GrantedBy   string     `json:"granted_by"` // UUID
	GrantedAt   time.Time  `json:"granted_at"`
	BoxID    *string    `json:"box_id,omitempty"` // If permission is box-specific
}

// UserPermissions holds all permissions for a user.
type UserPermissions struct {
	UserID      string // UUID
	IsAdmin     bool   // Admins have all permissions
	Permissions map[Component][]Permission
}

// HasPermission checks if the user has a specific permission for a component.
func (up *UserPermissions) HasPermission(component Component, permission Permission) bool {
	// Admins have all permissions
	if up.IsAdmin {
		return true
	}

	perms, exists := up.Permissions[component]
	if !exists {
		return false
	}

	// Check for the specific permission
	for _, p := range perms {
		if p == permission {
			return true
		}

		// Configure permission implies view
		if permission == PermissionView && p == PermissionConfigure {
			return true
		}

		// Action permission implies view
		if permission == PermissionView && p == PermissionAction {
			return true
		}

		// Manage permission implies view
		if permission == PermissionView && p == PermissionManage {
			return true
		}
	}

	return false
}

// CanView checks if user can view a component.
func (up *UserPermissions) CanView(component Component) bool {
	return up.HasPermission(component, PermissionView)
}

// CanConfigure checks if user can configure a component.
func (up *UserPermissions) CanConfigure(component Component) bool {
	return up.HasPermission(component, PermissionConfigure)
}

// CanManage checks if user can manage (enable/disable/reorder) a component.
func (up *UserPermissions) CanManage(component Component) bool {
	return up.HasPermission(component, PermissionManage)
}

// CanPerformActions checks if user can perform actions on a component.
func (up *UserPermissions) CanPerformActions(component Component) bool {
	return up.HasPermission(component, PermissionAction)
}

// CanApproveUsers checks if user can approve new user accounts.
func (up *UserPermissions) CanApproveUsers() bool {
	return up.IsAdmin || up.HasPermission(ComponentUsers, PermissionApproveUsers)
}

// CanManageBoxes checks if user can manage monitored box configurations.
func (up *UserPermissions) CanManageBoxes() bool {
	return up.IsAdmin || up.HasPermission(ComponentSettings, PermissionManageBoxes)
}

// GetComponentPermissions returns all permissions for a specific component.
func (up *UserPermissions) GetComponentPermissions(component Component) []Permission {
	if up.IsAdmin {
		return []Permission{
			PermissionView,
			PermissionConfigure,
			PermissionManage,
			PermissionAction,
		}
	}

	perms, exists := up.Permissions[component]
	if !exists {
		return []Permission{}
	}

	return perms
}

// PermissionTemplate represents a pre-defined set of permissions.
type PermissionTemplate struct {
	Name        string
	Description string
	Permissions map[Component][]Permission
}

// GetPermissionTemplates returns common permission templates.
func GetPermissionTemplates() []PermissionTemplate {
	return []PermissionTemplate{
		{
			Name:        "Read Only",
			Description: "Can view all pages but cannot make any changes",
			Permissions: map[Component][]Permission{
				ComponentCertificates: {PermissionView},
				ComponentLogs:         {PermissionView},
				ComponentServices:     {PermissionView},
				ComponentMetrics:      {PermissionView},
			},
		},
		{
			Name:        "Operator",
			Description: "Can perform actions and manage monitoring, but cannot change settings",
			Permissions: map[Component][]Permission{
				ComponentDisabledEntities: {PermissionManage},
				ComponentCertificates:     {PermissionView, PermissionDownload, PermissionAction},
				ComponentLogs:             {PermissionView},
				ComponentServices:         {PermissionView},
				ComponentMetrics:          {PermissionView},
			},
		},
		{
			Name:        "Power User",
			Description: "Can configure most components except user and box management",
			Permissions: map[Component][]Permission{
				ComponentDisabledEntities: {PermissionManage},
				ComponentCertificates:     {PermissionView, PermissionConfigure, PermissionDownload, PermissionAction},
				ComponentLogs:             {PermissionView, PermissionConfigure},
				ComponentServices:         {PermissionView, PermissionConfigure},
				ComponentMetrics:          {PermissionView, PermissionConfigure},
				ComponentGears:          {PermissionManage},
			},
		},
		{
			Name:        "User Manager",
			Description: "Can approve users and assign permissions",
			Permissions: map[Component][]Permission{
				ComponentUsers: {PermissionApproveUsers},
			},
		},
	}
}

// AllComponents returns a list of all components that have configurable permissions.
// Components without configurable permissions (Dashboard) are not included.
func AllComponents() []Component {
	return []Component{
		ComponentDisabledEntities,
		ComponentCertificates,
		ComponentLogs,
		ComponentServices,
		ComponentMetrics,
		ComponentGears,
		ComponentSettings,
		ComponentUsers,
		ComponentHAProxyConfig,
		ComponentFirewallConfig,
		ComponentSecurity,
		ComponentAlerts,
		ComponentOSUpdates,
	}
}

// GetComponentDisplayName returns a human-readable name for a component.
func GetComponentDisplayName(c Component) string {
	names := map[Component]string{
		ComponentDashboard:        "Dashboard",
		ComponentDisabledEntities: "Disabled Entities",
		ComponentCertificates:     "Certificates",
		ComponentLogs:             "Logs",
		ComponentServices:         "Services",
		ComponentMetrics:          "Metrics",
		ComponentGears:          "Gears",
		ComponentSettings:         "Settings",
		ComponentUsers:            "User Management",
		ComponentHAProxyConfig:    "HAProxy Configuration",
		ComponentFirewallConfig:   "Firewall Configuration",
		ComponentSecurity:         "Security",
		ComponentAlerts:           "Alerts",
		ComponentOSUpdates:        "OS Updates",
	}

	if name, exists := names[c]; exists {
		return name
	}
	return string(c)
}

// GetPermissionDisplayName returns a human-readable name for a permission.
func GetPermissionDisplayName(p Permission) string {
	names := map[Permission]string{
		PermissionView:          "View",
		PermissionConfigure:     "Configure",
		PermissionManage:        "Manage (Enable/Disable)",
		PermissionAction:        "Perform Actions",
		PermissionDownload:      "Download",
		PermissionApproveUsers:  "Approve Users",
		PermissionManageBoxes: "Manage Boxes",
	}

	if name, exists := names[p]; exists {
		return name
	}
	return string(p)
}

// GetPermissionDescription returns a description of what a permission allows.
func GetPermissionDescription(p Permission) string {
	descriptions := map[Permission]string{
		PermissionView:          "View component data and status",
		PermissionConfigure:     "Modify component configuration and settings",
		PermissionManage:        "Enable, disable, and reorder entities within the component",
		PermissionAction:        "Perform actions such as refresh certificates, restart services, etc.",
		PermissionDownload:      "Download certificate files",
		PermissionApproveUsers:  "Approve or deny new user account requests",
		PermissionManageBoxes: "Add, edit, or remove monitored box connections",
	}

	if desc, exists := descriptions[p]; exists {
		return desc
	}
	return string(p)
}

// GetAvailablePermissionsForComponent returns valid permissions for a component.
func GetAvailablePermissionsForComponent(c Component) []Permission {
	// Default permissions available for most components
	defaultPerms := []Permission{
		PermissionView,
		PermissionConfigure,
		PermissionManage,
		PermissionAction,
	}

	// Component-specific permission sets
	switch c {
	case ComponentUsers:
		return []Permission{
			PermissionApproveUsers, // Approve new user accounts + manage permissions
		}
	case ComponentSettings:
		return []Permission{
			PermissionManageBoxes, // Add/edit/delete monitored boxes
		}
	case ComponentDashboard:
		// Dashboard is always visible to authenticated users
		return []Permission{}
	case ComponentHAProxyConfig:
		return []Permission{
			PermissionView,      // View HAProxy config
			PermissionConfigure, // Edit and save HAProxy config
			PermissionAction,    // Restore from backup, trigger git sync
		}
	case ComponentFirewallConfig:
		return []Permission{
			PermissionView,      // View firewall config
			PermissionConfigure, // Edit and save firewall config
			PermissionAction,    // Restore from backup, trigger git sync
		}
	case ComponentDisabledEntities:
		return []Permission{
			PermissionManage, // Enable/disable backend monitoring
		}
	case ComponentCertificates:
		return []Permission{
			PermissionView,      // View certificates page and data
			PermissionConfigure, // Configure certificate renewal settings
			PermissionDownload,  // Download certificate files
			PermissionAction,    // Refresh/rotate certificates
		}
	case ComponentLogs:
		return []Permission{
			PermissionView,      // View logs page and data
			PermissionConfigure, // Configure which log sources are enabled
		}
	case ComponentServices:
		return []Permission{
			PermissionView,      // View services page and data
			PermissionConfigure, // Configure which services are monitored
		}
	case ComponentMetrics:
		return []Permission{
			PermissionView,      // View metrics/history page and data
			PermissionConfigure, // Configure metrics storage settings
		}
	case ComponentGears:
		return []Permission{
			PermissionManage, // Enable/disable gears from the list
		}
	case ComponentSecurity:
		return []Permission{
			PermissionView,   // View security dashboard, fail2ban stats, blocked IPs
			PermissionAction, // Block/unblock IPs
		}
	case ComponentAlerts:
		return []Permission{
			PermissionView,      // View alerts and alert history
			PermissionConfigure, // Configure alert rules
			PermissionAction,    // Acknowledge/silence alerts
		}
	case ComponentOSUpdates:
		return []Permission{
			PermissionView,      // View update status, packages, history
			PermissionConfigure, // Configure automatic updates settings
			PermissionAction,    // Install updates, manage packages, reboot
		}
	default:
		return defaultPerms
	}
}
