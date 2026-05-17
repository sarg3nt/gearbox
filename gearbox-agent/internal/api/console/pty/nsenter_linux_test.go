//go:build linux

package pty

import (
	"context"
	"strings"
	"testing"
)

// TestHostExecDetect_DefaultsToDirect verifies that on a developer
// machine (not in a container, no env override) the detector picks
// HostExecDirect. The CI matrix runs both bare-metal Linux and
// Linux-in-Docker, so this is the discrimination test.
func TestHostExecDetect_DefaultsToDirect(t *testing.T) {
	t.Setenv("HAPROXY_AGENT_HOST_EXEC", "")
	if runningInContainer() {
		t.Skip("running in a container; this test asserts the host-mode default")
	}
	if got := HostExecDetect(); got != HostExecDirect {
		t.Errorf("HostExecDetect() = %q, want %q", got, HostExecDirect)
	}
}

// TestHostExecDetect_SSHBridgeRequiresOptIn — even in a container,
// SSH bridge mode never auto-selects. The env var is the only path.
func TestHostExecDetect_SSHBridgeRequiresOptIn(t *testing.T) {
	if !runningInContainer() {
		t.Skip("only meaningful inside a container")
	}
	t.Setenv("HAPROXY_AGENT_HOST_EXEC", "")
	if got := HostExecDetect(); got == HostExecSSHBridge {
		t.Errorf("HostExecDetect() = ssh_bridge without env opt-in; got %q", got)
	}
	t.Setenv("HAPROXY_AGENT_HOST_EXEC", "ssh-bridge")
	if got := HostExecDetect(); got != HostExecSSHBridge {
		t.Errorf("HostExecDetect() = %q with env opt-in, want ssh_bridge", got)
	}
}

// TestSpawnNsenter_RejectsRunAs — nsenter doesn't compose with a
// run-as drop. We surface that as a clear error rather than silently
// giving a root shell.
func TestSpawnNsenter_RejectsRunAs(t *testing.T) {
	_, err := SpawnNsenter(context.Background(), []string{"/bin/true"}, "1000", 80, 24)
	if err == nil {
		t.Fatal("SpawnNsenter with run-as = nil err; want explicit refusal")
	}
	if !strings.Contains(err.Error(), "run-as") {
		t.Errorf("error = %q; want one mentioning run-as", err.Error())
	}
}

// TestSpawnNsenter_EmptyCommand — defensive: empty argv is a caller
// bug, fail fast.
func TestSpawnNsenter_EmptyCommand(t *testing.T) {
	_, err := SpawnNsenter(context.Background(), nil, "", 80, 24)
	if err == nil {
		t.Fatal("SpawnNsenter with empty command = nil err; want error")
	}
}
