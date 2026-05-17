package api

import (
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sarg3nt/gearbox-agent/internal/framework/crypto"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Console routes are always mounted. The agent-side env-var gate
// (HAPROXY_AGENT_CONSOLE_ENABLED) was removed in the post-#127
// cleanup — the dashboard's per-box console_enabled toggle is the
// sole opt-in. This test pins the contract that the three routes
// exist and stay behind their respective auth gates (API key on
// token + capabilities; single-use token on the WS endpoint, which
// returns 401 when called with no token).
//
// A regression that re-introduces a conditional mount here would
// silently break any deployment that flipped the per-box toggle on
// but didn't also set a now-defunct env var.
func TestNewServer_ConsoleRoutesAlwaysMounted(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	// Build a one-entry keyring whose legacy bare-hex token the test
	// then sends in the Authorization header. The keyring replaced the
	// single-string APIKey field on ServerConfig (issue #72).
	testSecret := strings.Repeat("ab", 32) // 64 hex chars / 32 bytes
	secretBytes, _ := hex.DecodeString(testSecret)
	kr := &crypto.KeyRing{
		Version: 1,
		Entries: []crypto.KeyRingEntry{{
			KID: "legacy", Secret: secretBytes, SecretHex: testSecret, Role: "primary",
		}},
	}

	srv := NewServer(ServerConfig{
		ListenAddr: "127.0.0.1:0",
		KeyRing:    crypto.NewKeyRingPointer(kr),
		Logger:     newSilentLogger(),
		EventBus:   bus,
	})

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Capabilities — auth-gated, no token required, deterministic
	// response.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/console/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+testSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("capabilities request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("capabilities status = %d, want 200", resp.StatusCode)
	}

	// Same path without auth → 401 (proves the route is behind the
	// API-key middleware, not unauth-readable).
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

	// WS endpoint requires a single-use console token; no token in
	// the query string ⇒ 401. (We don't drive the upgrade here;
	// that lives in console/handler_test.go.)
	req4, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/console/ws", nil)
	resp4, err := http.DefaultClient.Do(req4)
	if err != nil {
		t.Fatalf("ws (no token) request: %v", err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusUnauthorized {
		t.Errorf("ws (no token) status = %d, want 401", resp4.StatusCode)
	}
}
