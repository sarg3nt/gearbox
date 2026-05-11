package middleware

import (
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// validCSPDirective matches a single CSP directive line in the shape the
// directives slice expects: a directive name followed by one or more source
// expressions, with no semicolons (which would close the directive and let
// the env-var splice arbitrary new directives into the CSP header).
//
// CSP directive names are letters, digits, and hyphens. Source expressions
// can be 'self' / 'none' / 'unsafe-inline' / nonce-... / sha256-... / scheme
// URLs / wildcards / data: / blob: — i.e. printable ASCII without ; or ,
// or newline. We keep the validator deliberately conservative: only
// alphanumerics, a small punctuation set, and quoted keywords.
//
// `+` and `=` are included so nonce-... and sha256/sha384/sha512-...
// expressions whose payload uses the standard base64 alphabet aren't
// silently dropped. 2026-05 audit P1-4 follow-up (Copilot review on
// PR #43).
//
// 2026-05 audit P1-4.
var validCSPDirective = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]+(\s+[a-zA-Z0-9'_:/.\-*+=]+)+$`)

// cspContainsForbidden returns true when s contains any byte that has no
// business appearing in a CSP header value. A hand-curated whitespace
// list misses codepoints like form-feed (\f) and the various unicode
// space chars, so we reject ASCII control chars (< 0x20, 0x7F) and any
// rune outside ASCII as a precaution against header smuggling via the
// CSP env vars. 2026-05 audit P1-4 follow-up (Copilot review on PR #43).
func cspContainsForbidden(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r > 0x7e {
			return true
		}
	}
	return false
}

// sanitizeCSPExtraSource accepts a single entry from the
// CSP_EXTRA_SOURCES comma-separated env var and returns the trimmed
// value plus an OK flag. Rejects values containing characters that
// would let an attacker close the current directive and inject a new
// one (`;`), inject CRLF into the header (`\r`, `\n`), or that don't
// look like a valid `directive source [source...]` line.
func sanitizeCSPExtraSource(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", false
	}
	if strings.ContainsAny(trimmed, ";") || cspContainsForbidden(trimmed) {
		return "", false
	}
	if !validCSPDirective.MatchString(trimmed) {
		return "", false
	}
	return trimmed, true
}

// sanitizeCSPReportURI validates the CSP_REPORT_URI env var. Accepts an
// http(s):// URL; rejects relative paths (would silently fall through to
// the dashboard's own origin), schemed URIs other than http(s) (no
// `javascript:` or `data:`), and anything with embedded whitespace or
// newlines (CRLF injection into the response header).
func sanitizeCSPReportURI(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", false
	}
	if strings.ContainsAny(trimmed, " ;") || cspContainsForbidden(trimmed) {
		return "", false
	}
	u, err := url.Parse(trimmed)
	if err != nil || u == nil {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if u.Host == "" {
		return "", false
	}
	return trimmed, true
}

// SecurityHeaders adds security-related HTTP headers to responses.
// Implements defense-in-depth security controls including:
// - Content-Security-Policy (CSP) to prevent XSS attacks
// - X-Content-Type-Options to prevent MIME-sniffing
// - X-Frame-Options to prevent clickjacking
// - Referrer-Policy to control referrer information
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Content-Security-Policy (CSP)
		// Prevent XSS by restricting resource loading
		csp := buildCSP()
		w.Header().Set("Content-Security-Policy", csp)

		// X-Content-Type-Options
		// Prevent browsers from MIME-sniffing responses
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// X-Frame-Options
		// Prevent clickjacking attacks
		w.Header().Set("X-Frame-Options", "DENY")

		// Referrer-Policy
		// Control how much referrer information is sent
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// X-XSS-Protection (legacy, but still useful for older browsers)
		// Modern browsers rely on CSP instead
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		next.ServeHTTP(w, r)
	})
}

// buildCSP constructs the Content-Security-Policy header value.
// Uses environment variables for configuration:
// - USE_LOCAL_ASSETS: If "true", uses strict CSP (local assets only)
//                     If "false" or unset, allows CDN sources for production
// - CSP_REPORT_URI: URL to send CSP violation reports
// - CSP_EXTRA_SOURCES: Additional sources to allow (comma-separated)
func buildCSP() string {
	// Check if we're using local assets (strict CSP mode)
	useLocalAssets := os.Getenv("USE_LOCAL_ASSETS") == "true"

	var directives []string

	if useLocalAssets {
		// Local assets mode: Strict CSP - only allow 'self'
		// Used for CSP-compliant local development
		// Requires running `make dev-assets` first to download assets
		directives = []string{
			"default-src 'self'",
			"script-src 'self' 'unsafe-inline'", // 'unsafe-inline' needed for Tailwind config and inline scripts
			"style-src 'self' 'unsafe-inline'",  // Tailwind CSS requires inline styles
			"img-src 'self' data: blob:",        // Allow data URIs and blob for images
			"font-src 'self' data:",             // Allow data URIs for fonts
			"connect-src 'self' ws: wss:",       // Allow WebSocket connections
			"frame-ancestors 'none'",            // Prevent framing (same as X-Frame-Options)
			"base-uri 'self'",                   // Restrict <base> tag URIs
			"form-action 'self'",                // Restrict form submissions to same origin
		}
	} else {
		// Production mode: Allow CDN resources
		// This is the default mode for production deployments
		// CDNs provide automatic updates and good performance via edge caching
		directives = []string{
			"default-src 'self'",
			"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://unpkg.com https://cdn.jsdelivr.net https://d3js.org",
			"style-src 'self' 'unsafe-inline' https://unpkg.com",
			// Home gear app catalog hosts SVG icons on jsDelivr (selfh.st/icons).
			"img-src 'self' data: blob: https://cdn.jsdelivr.net",
			"font-src 'self' data:",
			"connect-src 'self' ws: wss:",
			"frame-ancestors 'none'",
			"base-uri 'self'",
			"form-action 'self'",
		}
	}

	// Add extra sources from environment variable if configured.
	// Each comma-separated entry is required to be a well-formed CSP
	// directive line; entries that look like injection attempts (embedded
	// `;`, CRLF, etc.) are silently dropped so a compromised deployment
	// env can't neutralize the CSP. See 2026-05 security audit P1-4.
	extraSources := os.Getenv("CSP_EXTRA_SOURCES")
	if extraSources != "" {
		for _, source := range strings.Split(extraSources, ",") {
			if clean, ok := sanitizeCSPExtraSource(source); ok {
				directives = append(directives, clean)
			}
		}
	}

	// Add report-uri if configured. The URI is required to be an absolute
	// http(s) URL; anything else (including relative paths, `javascript:`,
	// or values with embedded whitespace) is dropped.
	if uri, ok := sanitizeCSPReportURI(os.Getenv("CSP_REPORT_URI")); ok {
		directives = append(directives, "report-uri "+uri)
	}

	return strings.Join(directives, "; ")
}
