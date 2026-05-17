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
	// ModeEcho — Phase 1a only. The handler echoes data frames back to
	// the client; no shell is spawned. Lets the dashboard prove the
	// auth + WS plumbing before any PTY exists.
	ModeEcho = "echo"

	// ModeHostPTY — Phase 1b. Direct PTY on the host the agent runs on.
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
//
// Fields are conservative — Phase 1a always reports {Mode: "echo",
// HostConsole: false}. As later phases add real PTY support, the same
// envelope grows to advertise the live mode.
type CapabilitiesResponse struct {
	// Enabled mirrors HAPROXY_AGENT_CONSOLE_ENABLED — true iff this
	// surface is registered at all. Always true when this handler runs
	// (registration is gated on the same flag), but exposed for
	// symmetry with future "registered but degraded" states.
	Enabled bool `json:"enabled" example:"true"`

	// Mode is the exec strategy the agent will use when a session
	// opens. See Mode* constants.
	Mode string `json:"mode" example:"echo"`

	// HostConsole is true when a session lands on the host the operator
	// thinks of as "this box" — direct PTY on a host install, nsenter
	// or SSH bridge from a container install. False in Phase 1a (echo
	// only) and in any container deployment that lacks both bridges.
	HostConsole bool `json:"host_console" example:"false"`

	// DefaultUID is the UID a session will run as if the dashboard
	// doesn't override it. Equals the agent process's effective UID
	// (geteuid). On almost every box in this fleet the agent runs as
	// root, so this is typically 0 — surfaced so operators reading the
	// box-settings UI see, unambiguously, that the default shell is a
	// root shell. The agent never escalates above this value; it can
	// drop below it via the run-as setting (Phase 1b+).
	//
	// -1 on Windows where the UID concept doesn't apply (Phase 3).
	DefaultUID int `json:"default_uid" example:"0"`

	// OS is the runtime.GOOS the agent is built for. Lets the dashboard
	// pick a sensible default shell ("/bin/bash -l" on linux, "pwsh"
	// on windows).
	OS string `json:"os" example:"linux"`

	// Phase exists for early dashboard work — it lets the UI say
	// "echo-only, PTY coming in 1b" instead of pretending the surface
	// is fully wired. Drop the field once Phase 1c lands or hardcode
	// to "production"; for now it's load-bearing for the rollout note.
	Phase string `json:"phase" example:"1a"`
}

// Capabilities reports what this agent's console surface can do.
//
//	@Summary		Console surface capabilities
//	@Description	Reports whether the remote-console surface is enabled, which execution mode it will use, the UID a session will run as by default, and the runtime OS. Dashboard reads this before exposing the console affordance.
//	@Tags			Console
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	CapabilitiesResponse	"Console capabilities"
//	@Failure		401	{string}	string					"Unauthorized"
//	@Router			/api/v1/console/capabilities [get]
func Capabilities(w http.ResponseWriter, r *http.Request) {
	resp := CapabilitiesResponse{
		Enabled:     true,
		Mode:        ModeEcho,
		HostConsole: false,
		DefaultUID:  effectiveUID(),
		OS:          runtime.GOOS,
		Phase:       "1a",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
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
