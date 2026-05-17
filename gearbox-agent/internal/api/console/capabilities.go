package console

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
)

// Mode names what the agent will actually exec when a session opens.
// Stable string contract for the dashboard to switch on.
const (
	// ModeEcho — the handler echoes data frames back to the client
	// without spawning a shell. Phase 1a default, Windows fallback,
	// and what tests run with when no Spawner is wired.
	ModeEcho = "echo"

	// ModeHostPTY — direct PTY on the host the agent runs on.
	// Phase 1b default on Linux/macOS host installs.
	ModeHostPTY = "host_pty"

	// ModeNsenter — Phase 1d. Container agent crossing into the host's
	// namespaces via nsenter (requires pid:host + privileged).
	ModeNsenter = "nsenter"

	// ModeSSHBridge — Phase 2. Container agent connecting to the host's
	// sshd over a private channel using an agent-managed key.
	ModeSSHBridge = "ssh_bridge"
)

// CapabilitiesResponse describes what the console endpoint can actually
// do on this agent. The dashboard reads this before exposing any
// affordance; if Enabled is false, or HostConsole is false in a context
// where the operator expected host access, the dashboard hides the
// console button and surfaces the reason in box settings.
type CapabilitiesResponse struct {
	// Enabled is always true today — the agent unconditionally
	// mounts the console surface. Kept in the envelope for
	// symmetry with future "registered but degraded" states
	// (e.g. a platform that probes negative for PTY support).
	Enabled bool `json:"enabled" example:"true"`

	// Mode is the exec strategy the agent will use when a session
	// opens. See Mode* constants.
	Mode string `json:"mode" example:"host_pty"`

	// HostConsole is true when a session lands on the host the
	// operator thinks of as "this box" — direct PTY on a host
	// install, nsenter or SSH bridge from a container install.
	// False in echo mode and in any container deployment that
	// lacks both bridges.
	HostConsole bool `json:"host_console" example:"true"`

	// DefaultUID is the UID a session will run as if the dashboard
	// doesn't override it. Equals the agent process's effective UID
	// (geteuid). On almost every box in this fleet the agent runs
	// as root, so this is typically 0 — surfaced so operators
	// reading the box-settings UI see, unambiguously, that the
	// default shell is a root shell. The agent never escalates
	// above this value; it can drop below it via the run-as setting.
	//
	// -1 on Windows where the UID concept doesn't apply (Phase 3).
	DefaultUID int `json:"default_uid" example:"0"`

	// OS is the runtime.GOOS the agent is built for. Lets the
	// dashboard pick a sensible default shell ("/bin/bash -l" on
	// linux, "pwsh" on windows).
	OS string `json:"os" example:"linux"`

	// Shell is the argv the agent will exec when a session opens.
	// Surfaced so the dashboard can show "Console: /bin/bash -l"
	// next to the run-as field rather than leaving operators
	// guessing what they're about to get.
	Shell []string `json:"shell,omitempty" example:"[\"/bin/bash\", \"-l\"]"`
}

// HandleCapabilities reports what this agent's console surface can do.
//
//	@Summary		Console surface capabilities
//	@Description	Reports whether the remote-console surface is enabled, which execution mode it will use, the UID a session will run as by default, and the runtime OS. Dashboard reads this before exposing the console affordance.
//	@Tags			Console
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	CapabilitiesResponse	"Console capabilities"
//	@Failure		401	{string}	string					"Unauthorized"
//	@Router			/api/v1/console/capabilities [get]
func (h *Handler) HandleCapabilities(w http.ResponseWriter, r *http.Request) {
	mode := h.Mode
	if mode == "" {
		mode = ModeEcho
	}
	resp := CapabilitiesResponse{
		Enabled:     true,
		Mode:        mode,
		HostConsole: hostConsoleForMode(mode),
		DefaultUID:  effectiveUID(),
		OS:          runtime.GOOS,
		Shell:       h.Shell,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// hostConsoleForMode reports whether a session in the given mode lands
// on the host the operator thinks of as "this box." Echo mode is the
// only one that doesn't.
func hostConsoleForMode(mode string) bool {
	switch mode {
	case ModeHostPTY, ModeNsenter, ModeSSHBridge:
		return true
	}
	return false
}

// effectiveUID returns os.Geteuid on POSIX, -1 on Windows. Keeps the
// capabilities response honest about what "default UID" means across
// platforms without dragging in a syscall package.
func effectiveUID() int {
	uid := os.Geteuid()
	if uid < 0 {
		return -1
	}
	return uid
}
