package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Off-by-default is the load-bearing security property of the console
// surface: an agent that hasn't explicitly opted in MUST NOT expose any
// /api/v1/console/* route. A regression here is silently giving every
// box in the fleet a shell-by-token, so this test pins the contract
// from the server-config level (not just the handler level). See [#89].
func TestNewServer_ConsoleDisabled_RoutesReturn404(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	srv := NewServer(ServerConfig{
		ListenAddr:     "127.0.0.1:0",
		APIKey:         "test-key",
		Logger:         newSilentLogger(),
		EventBus:       bus,
		ConsoleEnabled: false, // the property under test
	})

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/console/token"},
		{http.MethodGet, "/api/v1/console/capabilities"},
		{http.MethodGet, "/api/v1/console/ws"},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(tc.method, ts.URL+tc.path, nil)
		req.Header.Set("Authorization", "Bearer test-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 (route should not exist when ConsoleEnabled=false)",
				tc.method, tc.path, resp.StatusCode)
		}
	}
}

// Mirror of the above: when the operator has opted in, the routes
// exist. We don't exercise the full WS upgrade here (that's covered in
// console/handler_test.go) — just that the routes are mounted and
// auth-gated correctly.
func TestNewServer_ConsoleEnabled_RoutesExist(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	srv := NewServer(ServerConfig{
		ListenAddr:     "127.0.0.1:0",
		APIKey:         "test-key",
		Logger:         newSilentLogger(),
		EventBus:       bus,
		ConsoleEnabled: true,
	})

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Capabilities is the easiest reach — auth-gated, no token
	// required, deterministic response shape.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/console/capabilities", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("capabilities request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("capabilities status = %d, want 200", resp.StatusCode)
	}

	// Without auth, the same path must be unauthorized (proves the
	// route is behind the API-key middleware, not unauth-readable).
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/console/capabilities", nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("capabilities (no auth) request: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("capabilities (no auth) status = %d, want 401", resp2.StatusCode)
	}

	// Token endpoint must also be auth-gated.
	req3, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/console/token", nil)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("token (no auth) request: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Errorf("token (no auth) status = %d, want 401", resp3.StatusCode)
	}
}
