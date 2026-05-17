package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 2026-05 audit P2-6: HSTS and Permissions-Policy must be emitted on every
// response so a forced-downgrade MITM can't sustain plain HTTP and so the
// browser knows the dashboard never uses camera/mic/geolocation/etc.
func TestSecurityHeaders_HSTS(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	hsts := rr.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Fatal("Strict-Transport-Security header missing")
	}
	for _, want := range []string{"max-age=", "includeSubDomains", "preload"} {
		if !strings.Contains(hsts, want) {
			t.Errorf("Strict-Transport-Security missing %q: %q", want, hsts)
		}
	}
}

func TestSecurityHeaders_PermissionsPolicy(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	pp := rr.Header().Get("Permissions-Policy")
	if pp == "" {
		t.Fatal("Permissions-Policy header missing")
	}
	// All of these features should be disabled (=()) — none are used by the
	// dashboard, so a future XSS shouldn't be able to silently re-enable
	// them without an explicit header change here.
	for _, feature := range []string{"geolocation=()", "microphone=()", "camera=()", "usb=()", "payment=()"} {
		if !strings.Contains(pp, feature) {
			t.Errorf("Permissions-Policy missing %q: %q", feature, pp)
		}
	}
}
