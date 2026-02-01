package pages

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// backendBelongsToFrontend checks if a backend is associated with a frontend via metadata.
func backendBelongsToFrontend(backendName, frontendName string, metadata *models.Metadata) bool {
	if metadata == nil {
		return false
	}
	fm := metadata.GetMetadataForFrontend(frontendName)
	if fm == nil {
		return false
	}
	for _, bn := range fm.Backends {
		if bn == backendName {
			return true
		}
	}
	return false
}

// hasOrphanedBackends checks if there are any backends without frontend metadata.
func hasOrphanedBackends(stats *models.HAProxyStats, metadata *models.Metadata) bool {
	for _, backend := range stats.Backends {
		// Skip system backends
		if isSystemBackend(backend.Name) {
			continue
		}
		if isOrphanedBackend(backend.Name, metadata) {
			return true
		}
	}
	return false
}

// isOrphanedBackend checks if a backend has no frontend association in metadata.
func isOrphanedBackend(backendName string, metadata *models.Metadata) bool {
	if metadata == nil {
		return true
	}
	// Check if this backend appears in any frontend's backend list
	for _, fm := range metadata.Frontends {
		for _, bn := range fm.Backends {
			if bn == backendName {
				return false
			}
		}
	}
	return true
}

// isSystemBackend checks if a backend is a HAProxy system backend
func isSystemBackend(backendName string) bool {
	systemBackends := []string{"stats", "no_backend"}
	for _, sysBackend := range systemBackends {
		if backendName == sysBackend {
			return true
		}
	}
	return false
}

// isSystemFrontend checks if a frontend is a HAProxy system frontend
func isSystemFrontend(frontendName string) bool {
	systemFrontends := []string{"stats"}
	for _, sysFrontend := range systemFrontends {
		if frontendName == sysFrontend {
			return true
		}
	}
	return false
}

// BackendGroupInfo holds parsed backend name parts
type BackendGroupInfo struct {
	Group       string // First part before underscore (e.g., "hardware")
	Item        string // Middle part (e.g., "thor" or "mjolnir-ipmi")
	Kind        string // Last part (usually "backend")
	DisplayName string // What to show on the card
	FullName    string // Original full name
}

// ParseBackendName parses a backend name into its components
// Format: group_item_kind (e.g., "hardware_thor_backend", "audiobookshelf_audiobookshelf_backend")
func ParseBackendName(name string) BackendGroupInfo {
	parts := strings.Split(name, "_")

	info := BackendGroupInfo{
		FullName: name,
	}

	if len(parts) >= 3 {
		info.Group = parts[0]
		info.Kind = parts[len(parts)-1]
		// Item is everything in between
		info.Item = strings.Join(parts[1:len(parts)-1], "_")

		// If group and item are the same (like audiobookshelf_audiobookshelf),
		// just use the item name as display
		if info.Group == info.Item {
			info.DisplayName = info.Item
		} else {
			info.DisplayName = info.Item
		}
	} else if len(parts) == 2 {
		info.Group = parts[0]
		info.Item = parts[1]
		info.DisplayName = parts[1]
	} else {
		// Single word backend name
		info.Group = ""
		info.Item = name
		info.DisplayName = name
	}

	return info
}

// GroupedBackends represents backends organized by group
type GroupedBackends struct {
	GroupName string
	Backends  []models.Backend
}

// GroupBackends organizes backends by their group prefix
func GroupBackends(backends []models.Backend) []GroupedBackends {
	// Map group name to backends
	groupMap := make(map[string][]models.Backend)
	groupOrder := []string{}

	for _, backend := range backends {
		info := ParseBackendName(backend.Name)
		groupName := info.Group
		if groupName == "" {
			groupName = "__ungrouped__"
		}

		if _, exists := groupMap[groupName]; !exists {
			groupOrder = append(groupOrder, groupName)
		}
		groupMap[groupName] = append(groupMap[groupName], backend)
	}

	// Sort groups alphabetically, but keep ungrouped at end
	sort.Slice(groupOrder, func(i, j int) bool {
		if groupOrder[i] == "__ungrouped__" {
			return false
		}
		if groupOrder[j] == "__ungrouped__" {
			return true
		}
		return groupOrder[i] < groupOrder[j]
	})

	// Build result
	var result []GroupedBackends
	for _, groupName := range groupOrder {
		backends := groupMap[groupName]

		displayGroupName := groupName
		if groupName == "__ungrouped__" {
			displayGroupName = "Other"
		}

		// Sort backends within group
		sort.Slice(backends, func(i, j int) bool {
			return backends[i].Name < backends[j].Name
		})

		result = append(result, GroupedBackends{
			GroupName: displayGroupName,
			Backends:  backends,
		})
	}

	return result
}

// GetBackendDisplayName returns the display name for a backend
func GetBackendDisplayName(name string) string {
	info := ParseBackendName(name)
	return info.DisplayName
}

// GetContainersForBackend returns the containers for a backend from metadata
func GetContainersForBackend(backendName string, metadata *models.Metadata) []models.Container {
	if metadata == nil {
		return []models.Container{}
	}

	backendMeta := metadata.GetMetadataForBackend(backendName)
	if backendMeta == nil {
		return []models.Container{}
	}

	return backendMeta.Containers
}

// frontendHasBackends checks if a frontend has any associated backends
func frontendHasBackends(frontendName string, backends []models.Backend, metadata *models.Metadata) bool {
	for _, backend := range backends {
		if backendBelongsToFrontend(backend.Name, frontendName, metadata) {
			return true
		}
	}
	return false
}

// SortedFrontends sorts frontends with those having backends first
type FrontendWithBackendStatus struct {
	Frontend          models.Frontend
	HasBackends       bool
	IsHTTPRedirect    bool   // True if this frontend only redirects HTTP to HTTPS
	RedirectsToFront  string // Name of the frontend it redirects to (e.g., "https_front")
}

// getFrontendDisplayTitle returns the display title for a frontend.
// For HTTP redirect frontends, it shows "http_front → https_front"
func getFrontendDisplayTitle(fs FrontendWithBackendStatus) string {
	if fs.IsHTTPRedirect && fs.RedirectsToFront != "" {
		return fmt.Sprintf("%s → %s", fs.Frontend.Name, fs.RedirectsToFront)
	}
	return fs.Frontend.Name
}

// isHTTPRedirectFrontend detects if a frontend only serves to redirect HTTP traffic to HTTPS.
// This is detected by:
// 1. Frontend name contains "http" but not "https"
// 2. Frontend has no backends
// 3. There exists a corresponding HTTPS frontend
func isHTTPRedirectFrontend(frontendName string, backends []models.Backend, frontends []models.Frontend, metadata *models.Metadata) (bool, string) {
	nameLower := strings.ToLower(frontendName)

	// Must contain "http" but not "https" in the name
	if !strings.Contains(nameLower, "http") || strings.Contains(nameLower, "https") {
		return false, ""
	}

	// Must have no backends
	if frontendHasBackends(frontendName, backends, metadata) {
		return false, ""
	}

	// Look for a corresponding HTTPS frontend
	// Try common naming patterns: http_front -> https_front, http-frontend -> https-frontend
	possibleHTTPSNames := []string{
		strings.Replace(frontendName, "http_", "https_", 1),
		strings.Replace(frontendName, "http-", "https-", 1),
		strings.Replace(frontendName, "HTTP_", "HTTPS_", 1),
		strings.Replace(frontendName, "HTTP-", "HTTPS-", 1),
	}

	for _, httpsName := range possibleHTTPSNames {
		for _, fe := range frontends {
			if fe.Name == httpsName {
				return true, httpsName
			}
		}
	}

	// If no matching HTTPS frontend found but it still looks like an HTTP redirect frontend
	// (no backends, has "http" in name without "https"), still mark it as redirect
	return true, ""
}

// GetSortedFrontends returns frontends sorted with backend-having ones first
func GetSortedFrontends(frontends []models.Frontend, backends []models.Backend, metadata *models.Metadata) []FrontendWithBackendStatus {
	var result []FrontendWithBackendStatus

	for _, frontend := range frontends {
		// Skip system frontends
		if isSystemFrontend(frontend.Name) {
			continue
		}
		hasBackends := frontendHasBackends(frontend.Name, backends, metadata)
		isRedirect, redirectsTo := isHTTPRedirectFrontend(frontend.Name, backends, frontends, metadata)
		result = append(result, FrontendWithBackendStatus{
			Frontend:         frontend,
			HasBackends:      hasBackends,
			IsHTTPRedirect:   isRedirect,
			RedirectsToFront: redirectsTo,
		})
	}

	// Sort: frontends with backends first, then alphabetically within each group
	sort.Slice(result, func(i, j int) bool {
		if result[i].HasBackends != result[j].HasBackends {
			return result[i].HasBackends // true (has backends) comes first
		}
		return result[i].Frontend.Name < result[j].Frontend.Name
	})

	return result
}

// GetBackendsForFrontend returns backends that belong to a frontend
func GetBackendsForFrontend(frontendName string, backends []models.Backend, metadata *models.Metadata) []models.Backend {
	var result []models.Backend
	for _, backend := range backends {
		if backendBelongsToFrontend(backend.Name, frontendName, metadata) {
			result = append(result, backend)
		}
	}
	return result
}

// hasSharedNetwork checks if any container in the list shares a network with another container
func hasSharedNetwork(containers []models.Container) bool {
	for _, container := range containers {
		if container.NetworkMode != "" &&
			container.NetworkMode != "bridge" &&
			container.NetworkMode != "host" &&
			strings.HasPrefix(container.NetworkMode, "service:") {
			return true
		}
	}
	return false
}

// GetServerAddress returns the server address (IP:port) for a backend
func GetServerAddress(backendName string, metadata *models.Metadata) string {
	if metadata == nil {
		return ""
	}
	backendMeta := metadata.GetMetadataForBackend(backendName)
	if backendMeta == nil {
		return ""
	}
	return backendMeta.GetServerAddress()
}

// GetServiceURL returns the public URL for a backend service (e.g., https://hostname)
func GetServiceURL(backendName string, metadata *models.Metadata) string {
	if metadata == nil {
		return ""
	}
	backendMeta := metadata.GetMetadataForBackend(backendName)
	if backendMeta == nil || backendMeta.Hostname == "" {
		return ""
	}
	return "https://" + backendMeta.Hostname
}


// StatusSummary holds summary statistics for status doughnuts
type StatusSummary struct {
	HealthyFrontends int
	TotalFrontends   int
	HealthyBackends  int
	TotalBackends    int
	HealthyServices  int
	TotalServices    int
}

// CalculateStatusSummary calculates health statistics for doughnut charts
func CalculateStatusSummary(stats *models.HAProxyStats, metadata *models.Metadata) StatusSummary {
	summary := StatusSummary{}

	// Count frontends (exclude system frontends)
	for _, frontend := range stats.Frontends {
		if isSystemFrontend(frontend.Name) {
			continue
		}
		summary.TotalFrontends++
		if frontend.Status == "OPEN" {
			summary.HealthyFrontends++
		}
	}

	// Count backends (only those with frontend associations, exclude system backends)
	for _, backend := range stats.Backends {
		if isSystemBackend(backend.Name) {
			continue
		}
		if !isOrphanedBackend(backend.Name, metadata) {
			summary.TotalBackends++
			// Consider healthy if at least 90% health
			if backend.HealthPercent >= 90 {
				summary.HealthyBackends++
			}
		}
	}

	// Count services (unique servers across all backends, exclude system backends)
	// This counts actual servers, not backend groups
	for _, backend := range stats.Backends {
		if isSystemBackend(backend.Name) {
			continue
		}
		if !isOrphanedBackend(backend.Name, metadata) {
			summary.TotalServices += int(backend.TotalServers)
			summary.HealthyServices += int(backend.ActiveServers)
		}
	}

	return summary
}

// GetHealthyBackendCount counts healthy backends in a group
func GetHealthyBackendCount(backends []models.Backend) int {
	count := 0
	for _, backend := range backends {
		if backend.HealthPercent >= 90 {
			count++
		}
	}
	return count
}

// GetBackendHealthBadgeText returns badge text showing healthy/total ratio
func GetBackendHealthBadgeText(backends []models.Backend) string {
	healthy := GetHealthyBackendCount(backends)
	total := len(backends)
	return fmt.Sprintf("%d/%d", healthy, total)
}

// GetBackendHealthBadgeColor returns badge color based on health status
func GetBackendHealthBadgeColor(backends []models.Backend) string {
	healthy := GetHealthyBackendCount(backends)
	total := len(backends)

	if healthy == total {
		return "bg-green-600"
	}
	return "bg-red-600"
}

// GetContainerBackendsForFrontend returns non-hardware backends for a frontend
func GetContainerBackendsForFrontend(frontendName string, backends []models.Backend, metadata *models.Metadata) []models.Backend {
	var result []models.Backend
	for _, backend := range backends {
		if backendBelongsToFrontend(backend.Name, frontendName, metadata) {
			info := ParseBackendName(backend.Name)
			if info.Group != "hardware" {
				result = append(result, backend)
			}
		}
	}
	return result
}

// FormatNumberWithCommas formats a number with comma separators for thousands
func FormatNumberWithCommas(value float64, decimals int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "0"
	}

	// Format with specified decimals
	formatted := fmt.Sprintf("%.*f", decimals, value)

	// Split into integer and decimal parts
	parts := strings.Split(formatted, ".")
	intPart := parts[0]
	decPart := ""
	if len(parts) > 1 {
		decPart = parts[1]
	}

	// Add commas to integer part
	var result strings.Builder
	for i, digit := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(digit)
	}

	// Append decimal part if exists
	if decPart != "" {
		result.WriteRune('.')
		result.WriteString(decPart)
	}

	return result.String()
}

// DisabledEntitiesMap converts a slice of DisabledEntity to a map for quick lookup
type DisabledEntitiesMap struct {
	Backends  map[string]bool
	Frontends map[string]bool
	Services  map[string]bool
}

// BuildDisabledEntitiesMap creates a lookup map from disabled entities slice
func BuildDisabledEntitiesMap(entities []database.DisabledEntity) DisabledEntitiesMap {
	result := DisabledEntitiesMap{
		Backends:  make(map[string]bool),
		Frontends: make(map[string]bool),
		Services:  make(map[string]bool),
	}

	for _, entity := range entities {
		switch entity.EntityType {
		case database.EntityTypeBackend:
			result.Backends[entity.EntityName] = true
		case database.EntityTypeFrontend:
			result.Frontends[entity.EntityName] = true
		case database.EntityTypeService:
			result.Services[entity.EntityName] = true
		}
	}

	return result
}

// IsBackendDisabled checks if a backend is disabled
func IsBackendDisabled(backendName string, disabledMap DisabledEntitiesMap) bool {
	return disabledMap.Backends[backendName]
}

// IsFrontendDisabled checks if a frontend is disabled
func IsFrontendDisabled(frontendName string, disabledMap DisabledEntitiesMap) bool {
	return disabledMap.Frontends[frontendName]
}

// CalculateStatusSummaryWithDisabled calculates health statistics excluding disabled entities
func CalculateStatusSummaryWithDisabled(stats *models.HAProxyStats, metadata *models.Metadata, disabledEntities []database.DisabledEntity) StatusSummary {
	summary := StatusSummary{}
	disabledMap := BuildDisabledEntitiesMap(disabledEntities)

	// Count frontends (exclude system frontends and disabled ones)
	for _, frontend := range stats.Frontends {
		if isSystemFrontend(frontend.Name) || IsFrontendDisabled(frontend.Name, disabledMap) {
			continue
		}
		summary.TotalFrontends++
		if frontend.Status == "OPEN" {
			summary.HealthyFrontends++
		}
	}

	// Count backends (only those with frontend associations, exclude system backends and disabled ones)
	for _, backend := range stats.Backends {
		if isSystemBackend(backend.Name) || IsBackendDisabled(backend.Name, disabledMap) {
			continue
		}
		if !isOrphanedBackend(backend.Name, metadata) {
			summary.TotalBackends++
			// Consider healthy if at least 90% health
			if backend.HealthPercent >= 90 {
				summary.HealthyBackends++
			}
		}
	}

	// Count services (unique servers across all backends, exclude system backends and disabled ones)
	for _, backend := range stats.Backends {
		if isSystemBackend(backend.Name) || IsBackendDisabled(backend.Name, disabledMap) {
			continue
		}
		if !isOrphanedBackend(backend.Name, metadata) {
			summary.TotalServices += int(backend.TotalServers)
			summary.HealthyServices += int(backend.ActiveServers)
		}
	}

	return summary
}

// GetDisabledEntity returns the DisabledEntity for a backend name, or nil if not disabled
func GetDisabledEntity(backendName string, disabledEntities []database.DisabledEntity) *database.DisabledEntity {
	for i := range disabledEntities {
		if disabledEntities[i].EntityType == database.EntityTypeBackend && disabledEntities[i].EntityName == backendName {
			return &disabledEntities[i]
		}
	}
	return nil
}

// FormatFrontendStats formats frontend statistics for display in header
func FormatFrontendStats(frontend models.Frontend) string {
	return fmt.Sprintf("Sessions: %d/%d    Requests: %d    In: %.2f MB    Out: %.2f MB",
		frontend.CurrentSessions,
		frontend.MaxSessions,
		frontend.RequestTotal,
		float64(frontend.BytesIn)/(1024*1024),
		float64(frontend.BytesOut)/(1024*1024),
	)
}

// IsHardwareBackend checks if a backend is a hardware service (not containerized)
func IsHardwareBackend(backendName string) bool {
	info := ParseBackendName(backendName)
	return info.Group == "hardware"
}

// formatDisabledTime formats a time.Time as a human-readable string (e.g., "2 hours ago")
func formatDisabledTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "Just now"
	} else if diff < time.Hour {
		minutes := int(diff.Minutes())
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}

	return t.Format("Jan 2, 2006 3:04 PM")
}
