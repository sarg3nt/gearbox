//go:build unix && !linux

package pty

import (
	"context"
	"errors"
)

// HostExecMode names the host-exec strategy. On non-Linux POSIX
// (macOS, BSD), only HostExecDirect is meaningful — there's no
// nsenter equivalent in production use, and the agent typically runs
// directly on the host.
type HostExecMode string

const (
	HostExecDirect    HostExecMode = "direct"
	HostExecNsenter   HostExecMode = "nsenter"
	HostExecSSHBridge HostExecMode = "ssh_bridge"
	HostExecNone      HostExecMode = "none"
)

// HostExecDetect on non-Linux always reports direct — we trust the
// agent is on the host rather than guessing about container-like
// environments (macOS containers run under a Linux VM and the agent
// would be inside that VM, where the linux build kicks in).
func HostExecDetect() HostExecMode {
	return HostExecDirect
}

// SpawnNsenter is not available on non-Linux POSIX. Returns a clear
// error so the caller can fall through to direct host PTY.
func SpawnNsenter(_ context.Context, _ []string, _ string, _, _ uint16) (Session, error) {
	return nil, errors.New("nsenter: not supported on this platform")
}
