//go:build windows

package pty

import (
	"context"
	"errors"
)

// HostExecMode names the host-exec strategy. On Windows we have no
// container-to-host bridge story yet — Phase 3+ may revisit.
type HostExecMode string

const (
	HostExecDirect    HostExecMode = "direct"
	HostExecNsenter   HostExecMode = "nsenter"
	HostExecSSHBridge HostExecMode = "ssh_bridge"
	HostExecNone      HostExecMode = "none"
)

// HostExecDetect on Windows reports direct — host installs only, no
// container support today.
func HostExecDetect() HostExecMode {
	return HostExecDirect
}

// SpawnNsenter is a Windows stub.
func SpawnNsenter(_ context.Context, _ []string, _ string, _, _ uint16) (Session, error) {
	return nil, errors.New("nsenter: not supported on windows")
}
