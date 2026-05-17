package console

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
)

// echoHandler returns a Handler with no Spawner — capabilities should
// reflect echo mode regardless of platform.
func echoHandler() *Handler {
	return &Handler{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:   ModeEcho,
	}
}

func TestCapabilities_EchoMode(t *testing.T) {
	h := echoHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/capabilities", nil)
	rr := httptest.NewRecorder()
	h.HandleCapabilities(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp CapabilitiesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Enabled {
		t.Error("enabled = false; handler running ⇒ enabled")
	}
	if resp.Mode != ModeEcho {
		t.Errorf("mode = %q, want %q", resp.Mode, ModeEcho)
	}
	if resp.HostConsole {
		t.Error("host_console = true in echo mode; want false")
	}
	if resp.OS != runtime.GOOS {
		t.Errorf("os = %q, want %q", resp.OS, runtime.GOOS)
	}
}

func TestCapabilities_HostPTYMode(t *testing.T) {
	// When a Spawner is set and Mode is HostPTY, the dashboard sees
	// host_console=true and learns the actual shell it will get.
	h := &Handler{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:   ModeHostPTY,
		Shell:  []string{"/bin/bash", "-l"},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/capabilities", nil)
	rr := httptest.NewRecorder()
	h.HandleCapabilities(rr, req)

	var resp CapabilitiesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Mode != ModeHostPTY {
		t.Errorf("mode = %q, want %q", resp.Mode, ModeHostPTY)
	}
	if !resp.HostConsole {
		t.Error("host_console = false in host_pty mode; want true")
	}
	if len(resp.Shell) != 2 || resp.Shell[0] != "/bin/bash" {
		t.Errorf("shell = %v, want [/bin/bash -l]", resp.Shell)
	}
	if runtime.GOOS == "windows" {
		if resp.DefaultUID != -1 {
			t.Errorf("default_uid = %d on windows, want -1", resp.DefaultUID)
		}
	} else if resp.DefaultUID != os.Geteuid() {
		t.Errorf("default_uid = %d, want %d", resp.DefaultUID, os.Geteuid())
	}
}

func TestCapabilities_NsenterAndSSHBridgeAreHostConsole(t *testing.T) {
	// These modes don't ship in Phase 1b but the host_console
	// classifier must already report them correctly so the dashboard
	// can wire its conditional UI ahead of time.
	for _, mode := range []string{ModeNsenter, ModeSSHBridge} {
		if !hostConsoleForMode(mode) {
			t.Errorf("hostConsoleForMode(%q) = false, want true", mode)
		}
	}
	if hostConsoleForMode(ModeEcho) {
		t.Error("hostConsoleForMode(echo) = true, want false")
	}
}
