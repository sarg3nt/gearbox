package middleware

import (
	"net/http"
	"os"
	"strings"
)

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

		// Strict-Transport-Security
		// Force HTTPS for a year, including subdomains, and signal eligibility
		// for the HSTS preload list. The header is only honored over HTTPS,
		// so emitting it unconditionally is safe — a plain-HTTP response on
		// :3000 in dev is ignored by the browser. Required for an HTTPS-only
		// deployment; harmless on the rare plain-HTTP request. 2026-05 audit P2-6.
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")

		// Permissions-Policy
		// Disable browser features the dashboard never uses. Prevents a
		// future XSS or compromised iframe-embedded asset from quietly
		// accessing camera/mic/geolocation/etc. 2026-05 audit P2-6.
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), usb=(), payment=(), interest-cohort=(), browsing-topics=()")

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

	// Add extra sources from environment variable if configured
	extraSources := os.Getenv("CSP_EXTRA_SOURCES")
	if extraSources != "" {
		sources := strings.Split(extraSources, ",")
		for _, source := range sources {
			source = strings.TrimSpace(source)
			if source != "" {
				directives = append(directives, source)
			}
		}
	}

	// Add report-uri if configured
	reportURI := os.Getenv("CSP_REPORT_URI")
	if reportURI != "" {
		directives = append(directives, "report-uri "+reportURI)
	}

	return strings.Join(directives, "; ")
}
