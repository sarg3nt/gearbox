package dashboard

import (
	"regexp"
	"strings"
)

// GenerateSlug converts a dashboard name to a URL-safe slug.
//
// Rules:
// 1. Convert to lowercase
// 2. Replace spaces with hyphens
// 3. Remove special characters (keep letters, numbers, hyphens)
// 4. Collapse multiple hyphens to single hyphen
// 5. Trim leading/trailing hyphens
//
// Examples:
//   - "My Dashboard" → "my-dashboard"
//   - "Production Overview" → "production-overview"
//   - "Database Monitoring!" → "database-monitoring"
func GenerateSlug(name string) string {
	// Convert to lowercase
	slug := strings.ToLower(name)

	// Replace spaces with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")

	// Remove special characters (keep letters, numbers, hyphens)
	reg := regexp.MustCompile(`[^a-z0-9-]+`)
	slug = reg.ReplaceAllString(slug, "")

	// Collapse multiple hyphens to single hyphen
	reg = regexp.MustCompile(`-+`)
	slug = reg.ReplaceAllString(slug, "-")

	// Trim leading/trailing hyphens
	slug = strings.Trim(slug, "-")

	return slug
}

// GeneratePluginDashboardSlug creates a slug for a plugin-provided dashboard.
//
// Format: "plugin-{plugin-name}-{dashboard-slug}"
//
// Examples:
//   - ("haproxy", "Overview") → "plugin-haproxy-overview"
//   - ("security", "Dashboard") → "plugin-security-dashboard"
func GeneratePluginDashboardSlug(pluginName, dashboardName string) string {
	dashboardSlug := GenerateSlug(dashboardName)
	return "plugin-" + pluginName + "-" + dashboardSlug
}

// GenerateFilename creates a YAML filename from a slug.
//
// Examples:
//   - "my-dashboard" → "my-dashboard.yaml"
//   - "plugin-haproxy-overview" → "plugin-haproxy-overview.yaml"
func GenerateFilename(slug string) string {
	return slug + ".yaml"
}
