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

		// X-XSS-Protection (legacy, but still useful for older browsers)
		// Modern browsers rely on CSP instead
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		next.ServeHTTP(w, r)
	})
}

// buildCSP constructs the Content-Security-Policy header value.
// Uses environment variables for configuration:
// - CSP_REPORT_URI: URL to send CSP violation reports
// - CSP_EXTRA_SOURCES: Additional sources to allow (comma-separated)
func buildCSP() string {
	// Base CSP policy
	// 'self' - Allow resources from same origin
	// 'unsafe-inline' - Required for inline styles (Tailwind CSS uses inline styles)
	// 'unsafe-eval' - Avoid if possible, but some frameworks require it
	directives := []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'", // TODO: Move to nonces for better security
		"style-src 'self' 'unsafe-inline'",  // Tailwind CSS requires inline styles
		"img-src 'self' data: blob:",        // Allow data URIs and blob for images
		"font-src 'self' data:",             // Allow data URIs for fonts
		"connect-src 'self' ws: wss:",       // Allow WebSocket connections
		"frame-ancestors 'none'",            // Prevent framing (same as X-Frame-Options)
		"base-uri 'self'",                   // Restrict <base> tag URIs
		"form-action 'self'",                // Restrict form submissions to same origin
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
