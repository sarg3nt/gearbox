package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
)

func TestCapabilities_Phase1aEnvelope(t *testing.T) {
	// Pin Phase 1a's envelope so a refactor that accidentally claims
	// "host_console: true" before the PTY lands is caught here. As
	// later phases extend capabilities, these expectations evolve
	// deliberately rather than drifting.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/capabilities", nil)
	rr := httptest.NewRecorder()

	Capabilities(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp CapabilitiesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Enabled {
		t.Error("enabled = false; if this handler runs, the surface is enabled")
	}
	if resp.Mode != ModeEcho {
		t.Errorf("mode = %q, want %q (Phase 1a is echo-only)", resp.Mode, ModeEcho)
	}
	if resp.HostConsole {
		t.Error("host_console = true; Phase 1a has no PTY yet so host access must be false")
	}
	if resp.Phase != "1a" {
		t.Errorf("phase = %q, want \"1a\"", resp.Phase)
	}
	if resp.OS != runtime.GOOS {
		t.Errorf("os = %q, want %q", resp.OS, runtime.GOOS)
	}
	if runtime.GOOS == "windows" {
		// Windows has no UID; we report -1 so dashboards can detect.
		if resp.DefaultUID != -1 {
			t.Errorf("default_uid = %d on windows, want -1", resp.DefaultUID)
		}
	} else if resp.DefaultUID != os.Geteuid() {
		t.Errorf("default_uid = %d, want %d (process euid)", resp.DefaultUID, os.Geteuid())
	}
}
