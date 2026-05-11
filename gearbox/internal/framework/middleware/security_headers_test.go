package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 2026-05 audit P1-4: CSP_EXTRA_SOURCES entries must be validated as
// well-formed CSP directive lines, with no characters that would let an
// attacker close the directive and inject a new one.
func TestSanitizeCSPExtraSource(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		// Accepted: well-formed CSP directive lines.
		{"script-src-self", "script-src 'self'", "script-src 'self'", true},
		{"connect-src-multi", "connect-src 'self' wss://example.com", "connect-src 'self' wss://example.com", true},
		{"img-src-data", "img-src 'self' data:", "img-src 'self' data:", true},
		{"font-src-cdn", "font-src https://fonts.example.com", "font-src https://fonts.example.com", true},
		{"trimmed-whitespace", "  script-src 'self'  ", "script-src 'self'", true},

		// Rejected: empty / structural injection / CRLF.
		{"empty", "", "", false},
		{"whitespace-only", "   ", "", false},
		{"directive-injection", "script-src 'self'; default-src *", "", false},
		{"crlf-injection", "script-src 'self'\r\nX-Header: pwn", "", false},
		{"lf-only", "script-src 'self'\ndefault-src *", "", false},

		// Rejected: doesn't look like a directive line at all.
		{"single-token", "evil", "", false},
		{"leading-quote", "'unsafe-inline'", "", false},
		{"directive-no-source", "script-src", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := sanitizeCSPExtraSource(tt.in)
			if ok != tt.ok {
				t.Errorf("sanitizeCSPExtraSource(%q) ok=%v, want %v", tt.in, ok, tt.ok)
				return
			}
			if ok && got != tt.want {
				t.Errorf("sanitizeCSPExtraSource(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// CSP_REPORT_URI must parse as an absolute http(s) URL; relative paths,
// non-http schemes, and CRLF injection must all be rejected.
func TestSanitizeCSPReportURI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"https", "https://csp-report.example.com/r", true},
		{"http", "http://csp-report.example.com/r", true},
		{"trimmed", "  https://csp-report.example.com/r  ", true},

		{"empty", "", false},
		{"relative-path", "/csp-report", false},
		{"javascript-scheme", "javascript:alert(1)", false},
		{"data-scheme", "data:text/html,evil", false},
		{"no-scheme-host-only", "csp-report.example.com", false},
		{"crlf-injection", "https://example.com\r\nX-Header: pwn", false},
		{"whitespace-in-middle", "https://example.com /path", false},
		{"directive-injection", "https://example.com; default-src *", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := sanitizeCSPReportURI(tt.in)
			if ok != tt.ok {
				t.Errorf("sanitizeCSPReportURI(%q) ok=%v, want %v", tt.in, ok, tt.ok)
			}
		})
	}
}

// End-to-end: a malicious CSP_EXTRA_SOURCES env var must not be reflected
// into the response header.
func TestSecurityHeaders_RejectsCSPInjection(t *testing.T) {
	t.Setenv("CSP_EXTRA_SOURCES", "script-src 'self'; default-src *")
	t.Setenv("CSP_REPORT_URI", "javascript:alert(1)")

	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("CSP header missing")
	}
	if strings.Contains(csp, "default-src *") {
		t.Errorf("CSP contains attacker-injected 'default-src *': %q", csp)
	}
	if strings.Contains(csp, "javascript:") {
		t.Errorf("CSP contains attacker-injected 'javascript:' report URI: %q", csp)
	}
}

// Sanity: a well-formed extra source DOES appear in the header.
func TestSecurityHeaders_AcceptsValidCSPExtra(t *testing.T) {
	t.Setenv("CSP_EXTRA_SOURCES", "connect-src 'self' wss://events.example.com")
	t.Setenv("CSP_REPORT_URI", "https://csp.example.com/report")

	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "wss://events.example.com") {
		t.Errorf("CSP missing valid extra source: %q", csp)
	}
	if !strings.Contains(csp, "report-uri https://csp.example.com/report") {
		t.Errorf("CSP missing valid report-uri: %q", csp)
	}
}
