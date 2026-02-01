package handler

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

const (
	// RedactedPlaceholder is what users see in place of sensitive values.
	// The format includes an ID for restoration: <redacted:pattern_name:index>
	// But displayed to users as just <redacted>
	RedactedPlaceholder = "<redacted>"
)

// SensitivePattern defines a pattern for detecting sensitive values in HAProxy config.
type SensitivePattern struct {
	Name        string         // Human-readable name for the pattern
	Pattern     *regexp.Regexp // Compiled regex pattern
	GroupIndex  int            // Which capture group contains the sensitive value (1-indexed)
	Description string         // What this pattern detects
}

// sensitivePatterns contains all patterns for detecting sensitive values in HAProxy configs.
// These are based on patterns from gitleaks, trufflehog, and HAProxy-specific knowledge.
// Reference: https://github.com/gitleaks/gitleaks
var sensitivePatterns = []SensitivePattern{
	// HAProxy-specific patterns
	{
		Name:        "stats_auth",
		Pattern:     regexp.MustCompile(`(?m)^(\s*stats\s+auth\s+\S+:)(.+)$`),
		GroupIndex:  2,
		Description: "HAProxy stats authentication password",
	},
	{
		Name:        "user_password",
		Pattern:     regexp.MustCompile(`(?mi)^(\s*user\s+\S+\s+(?:password|insecure-password)\s+)(\S+)(.*)$`),
		GroupIndex:  2,
		Description: "HAProxy user password directive",
	},
	{
		Name:        "userlist_password",
		Pattern:     regexp.MustCompile(`(?mi)^(\s*password\s+)(\S+)(.*)$`),
		GroupIndex:  2,
		Description: "HAProxy userlist password",
	},

	// SSL/TLS related - these contain paths but the password param is sensitive
	{
		Name:        "ssl_crt_key_password",
		Pattern:     regexp.MustCompile(`(?mi)(\s+(?:ssl-)?(?:crt|key)-(?:ignore-err|list)\s+.*?|\s+(?:crt|ca-file|crt-list|crl-file)\s+\S+\s+)(key-password\s+)(\S+)`),
		GroupIndex:  3,
		Description: "SSL certificate key password",
	},

	// Generic password patterns in HAProxy config
	{
		Name:        "http_auth_header",
		Pattern:     regexp.MustCompile(`(?mi)(http-request\s+(?:set-header|add-header)\s+(?:Authorization|X-Api-Key|X-Auth-Token)\s+)([^\s].*)$`),
		GroupIndex:  2,
		Description: "HTTP Authorization or API key header value",
	},
	{
		Name:        "basic_auth_value",
		Pattern:     regexp.MustCompile(`(?mi)((?:Basic|Bearer)\s+)([A-Za-z0-9+/=]{20,})(\s|$)`),
		GroupIndex:  2,
		Description: "Basic or Bearer auth token",
	},

	// Environment variable patterns that might contain secrets
	{
		Name:        "setenv_secret",
		Pattern:     regexp.MustCompile(`(?mi)^(\s*setenv\s+(?:\S*(?:PASSWORD|SECRET|TOKEN|KEY|API_KEY|AUTH|CREDENTIAL)\S*)\s+)(\S+)(.*)$`),
		GroupIndex:  2,
		Description: "Environment variable containing secret",
	},

	// Lua script passwords/tokens
	{
		Name:        "lua_load_secret",
		Pattern:     regexp.MustCompile(`(?mi)(lua-load\s+\S+\s+.*?(?:password|secret|token|key|api_key|auth)[=:]\s*)([^\s]+)`),
		GroupIndex:  2,
		Description: "Lua script password parameter",
	},

	// Generic patterns - high entropy strings that look like secrets
	// AWS Access Key ID - add surrounding context capture
	{
		Name:        "aws_access_key",
		Pattern:     regexp.MustCompile(`(?m)(\s|^|#)((?:A3T[A-Z0-9]|AKIA|ASIA|ABIA|ACCA)[A-Z2-7]{16})(\s|$)`),
		GroupIndex:  2,
		Description: "AWS Access Key ID",
	},

	// Generic API key patterns
	{
		Name:        "generic_api_key",
		Pattern:     regexp.MustCompile(`(?mi)(\s*#?\s*(?:api[_-]?key|apikey|api[_-]?secret)\s*[=:]\s*)([^\s#]+)`),
		GroupIndex:  2,
		Description: "Generic API key in comment or config",
	},

	// Private key detection (PEM format - just detect presence, redact the key content)
	{
		Name:        "private_key_inline",
		Pattern:     regexp.MustCompile(`(?ms)(-----BEGIN\s+(?:RSA\s+|EC\s+|DSA\s+|OPENSSH\s+|ENCRYPTED\s+)?PRIVATE\s+KEY-----)(.*?)(-----END\s+(?:RSA\s+|EC\s+|DSA\s+|OPENSSH\s+|ENCRYPTED\s+)?PRIVATE\s+KEY-----)`),
		GroupIndex:  2,
		Description: "Inline private key content",
	},

	// GitHub Personal Access Token - add surrounding context
	{
		Name:        "github_pat",
		Pattern:     regexp.MustCompile(`(?m)(\s|^|#|:)(ghp_[0-9a-zA-Z]{36})(\s|$)`),
		GroupIndex:  2,
		Description: "GitHub Personal Access Token",
	},

	// GitLab Personal Access Token - add surrounding context
	{
		Name:        "gitlab_pat",
		Pattern:     regexp.MustCompile(`(?m)(\s|^|#|:)(glpat-[\w-]{20})(\s|$)`),
		GroupIndex:  2,
		Description: "GitLab Personal Access Token",
	},

	// JWT Token (long enough to be meaningful) - add surrounding context
	{
		Name:        "jwt_token",
		Pattern:     regexp.MustCompile(`(?m)(\s|^|#|:)(ey[a-zA-Z0-9]{17,}\.ey[a-zA-Z0-9/\_-]{17,}\.[a-zA-Z0-9/\_-]{10,}[=]*)(\s|$)`),
		GroupIndex:  2,
		Description: "JSON Web Token (JWT)",
	},

	// Slack tokens - add surrounding context
	{
		Name:        "slack_token",
		Pattern:     regexp.MustCompile(`(?m)(\s|^|#|:)(xox[baprs]-[0-9]{10,13}-[0-9]{10,13}[a-zA-Z0-9-]*)(\s|$)`),
		GroupIndex:  2,
		Description: "Slack API token",
	},

	// Generic password in key=value format (common in comments or config)
	{
		Name:        "generic_password_assignment",
		Pattern:     regexp.MustCompile(`(?mi)(\s*#?\s*(?:password|passwd|pwd|secret|token|credential|auth_token|access_token|refresh_token)\s*[=:]\s*)([^\s#]+)`),
		GroupIndex:  2,
		Description: "Generic password/secret assignment",
	},

	// Database connection strings with passwords
	{
		Name:        "db_connection_password",
		Pattern:     regexp.MustCompile(`(?mi)((?:mysql|postgres|postgresql|mongodb|redis|amqp)://[^:]+:)([^@]+)(@)`),
		GroupIndex:  2,
		Description: "Database connection string password",
	},

	// LDAP bind password
	{
		Name:        "ldap_password",
		Pattern:     regexp.MustCompile(`(?mi)(bindpw\s+)(\S+)`),
		GroupIndex:  2,
		Description: "LDAP bind password",
	},

	// HTTP Basic Auth in URL
	{
		Name:        "http_basic_auth_url",
		Pattern:     regexp.MustCompile(`(?mi)(https?://[^:]+:)([^@]+)(@[^\s]+)`),
		GroupIndex:  2,
		Description: "HTTP Basic Auth password in URL",
	},
}

// RedactedValue stores information about a redacted value for restoration.
type RedactedValue struct {
	PatternName string // Which pattern matched
	Original    string // Original sensitive value
	Prefix      string // Text before the sensitive value
	Suffix      string // Text after the sensitive value (if any)
	FullMatch   string // The full original match for replacement
}

// ConfigRedactor handles redaction and restoration of sensitive values in configs.
// It stores original values server-side to restore them when saving.
type ConfigRedactor struct {
	mu sync.RWMutex
	// redactedValues maps boxID -> map[redactionKey -> RedactedValue]
	// redactionKey is a unique identifier for each redacted value (e.g., "stats_auth_0")
	redactedValues map[string]map[string]RedactedValue
}

// NewConfigRedactor creates a new ConfigRedactor instance.
func NewConfigRedactor() *ConfigRedactor {
	return &ConfigRedactor{
		redactedValues: make(map[string]map[string]RedactedValue),
	}
}

// redactedMarker creates a unique marker for a redacted value that can be found during restoration.
func redactedMarker(patternName string, index int) string {
	return fmt.Sprintf("<redacted:%s:%d>", patternName, index)
}

// RedactConfig redacts sensitive values from HAProxy config content.
// It stores the original values server-side for later restoration.
// Returns the redacted content.
func (r *ConfigRedactor) RedactConfig(boxID, content string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Initialize/clear map for this server
	r.redactedValues[boxID] = make(map[string]RedactedValue)

	result := content

	// Apply each pattern
	for _, sp := range sensitivePatterns {
		counter := 0
		patternKey := sp.Name

		result = sp.Pattern.ReplaceAllStringFunc(result, func(match string) string {
			submatches := sp.Pattern.FindStringSubmatch(match)
			if len(submatches) <= sp.GroupIndex {
				return match
			}

			// Get the sensitive value
			sensitiveValue := submatches[sp.GroupIndex]

			// Skip if already redacted or empty
			// Check both the sensitive value and the full match for existing markers
			if strings.Contains(sensitiveValue, "<redacted") ||
				strings.Contains(match, "<redacted:") ||
				strings.TrimSpace(sensitiveValue) == "" {
				return match
			}

			// Skip very short values that are likely not secrets (unless it's a known pattern)
			if len(sensitiveValue) < 4 && sp.Name != "stats_auth" && sp.Name != "user_password" {
				return match
			}

			// Build prefix and suffix
			var prefixBuilder, suffixBuilder strings.Builder
			for i := 1; i < sp.GroupIndex; i++ {
				prefixBuilder.WriteString(submatches[i])
			}
			for i := sp.GroupIndex + 1; i < len(submatches); i++ {
				suffixBuilder.WriteString(submatches[i])
			}
			prefix := prefixBuilder.String()
			suffix := suffixBuilder.String()

			// Generate unique key for this redaction
			key := redactionKey(patternKey, counter)
			marker := redactedMarker(patternKey, counter)
			counter++

			// Store the original value with context
			r.redactedValues[boxID][key] = RedactedValue{
				PatternName: sp.Name,
				Original:    sensitiveValue,
				Prefix:      prefix,
				Suffix:      suffix,
				FullMatch:   match,
			}

			// Return redacted version with unique marker
			return prefix + marker + suffix
		})
	}

	// Convert unique markers to user-friendly <redacted> for display
	// Keep a mapping so we can restore them
	result = convertMarkersToDisplay(result)

	return result
}

// markerPattern matches our internal markers like <redacted:stats_auth:0>
var markerPattern = regexp.MustCompile(`<redacted:([^:>]+):(\d+)>`)

// convertMarkersToDisplay converts internal markers to display format while preserving uniqueness
// by keeping the pattern info in a way that's invisible/minimal to users
func convertMarkersToDisplay(content string) string {
	// We keep the markers as-is since they're unique and allow restoration
	// The UI should display them nicely
	return content
}

// RestoreConfig restores redacted values in config content before saving.
// If a value is still "<redacted:...>", it restores the original.
// If the user changed it to something else, that new value is used.
// Returns the restored content and an error if any markers couldn't be restored.
func (r *ConfigRedactor) RestoreConfig(boxID, content string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	serverValues := r.redactedValues[boxID]

	result := content
	var unrestoredMarkers []string

	// Find all markers and restore them
	result = markerPattern.ReplaceAllStringFunc(result, func(marker string) string {
		submatches := markerPattern.FindStringSubmatch(marker)
		if len(submatches) < 3 {
			return marker
		}

		patternName := submatches[1]
		index := submatches[2]
		key := fmt.Sprintf("%s_%s", patternName, index)

		if serverValues != nil {
			if stored, ok := serverValues[key]; ok {
				return stored.Original
			}
		}

		// No stored value found, track it
		unrestoredMarkers = append(unrestoredMarkers, patternName)
		return marker
	})

	if len(unrestoredMarkers) > 0 {
		return result, fmt.Errorf("cannot restore %d redacted value(s) - session may have expired, please reload the config page", len(unrestoredMarkers))
	}

	return result, nil
}

// HasUnrestorableMarkers checks if the content contains markers that cannot be restored.
// This is useful for checking before attempting validation/save.
func (r *ConfigRedactor) HasUnrestorableMarkers(boxID, content string) (bool, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	serverValues := r.redactedValues[boxID]
	count := 0

	markerPattern.ReplaceAllStringFunc(content, func(marker string) string {
		submatches := markerPattern.FindStringSubmatch(marker)
		if len(submatches) < 3 {
			return marker
		}

		patternName := submatches[1]
		index := submatches[2]
		key := fmt.Sprintf("%s_%s", patternName, index)

		if serverValues == nil {
			count++
		} else if _, ok := serverValues[key]; !ok {
			count++
		}
		return marker
	})

	return count > 0, count
}

// ClearServerCache clears the stored redacted values for a server.
// Call this after a successful save to free memory.
func (r *ConfigRedactor) ClearServerCache(boxID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.redactedValues, boxID)
}

// GetRedactedCount returns the number of redacted values for a server.
// Useful for debugging and UI feedback.
func (r *ConfigRedactor) GetRedactedCount(boxID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if serverValues, exists := r.redactedValues[boxID]; exists {
		return len(serverValues)
	}
	return 0
}

// GetRedactedPatterns returns a list of pattern names that had matches for a server.
// Useful for debugging and audit logging.
func (r *ConfigRedactor) GetRedactedPatterns(boxID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	patterns := make(map[string]bool)
	if serverValues, exists := r.redactedValues[boxID]; exists {
		for _, rv := range serverValues {
			patterns[rv.PatternName] = true
		}
	}

	result := make([]string, 0, len(patterns))
	for p := range patterns {
		result = append(result, p)
	}
	return result
}

// redactionKey generates a unique key for a redacted value.
func redactionKey(patternName string, index int) string {
	return fmt.Sprintf("%s_%d", patternName, index)
}
