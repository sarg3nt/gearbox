//go:build windows

package pty

import (
	"context"
	"errors"
)

// ErrSignalUnsupported is returned by Session.Signal when the named
// signal has no equivalent on this platform.
var ErrSignalUnsupported = errors.New("pty: unsupported signal name")

// ErrNotImplemented is returned by SpawnUnix on Windows. ConPTY support
// lands in Phase 3 (#89). Until then the Windows build compiles but
// reports host_console=false in capabilities.
var ErrNotImplemented = errors.New("pty: windows ConPTY backend not implemented (Phase 3 of #89)")

// SpawnUnix on Windows is a stub. Kept under the same exported name so
// the handler can compile cross-platform; Phase 3 introduces a
// SpawnWindows alongside this one or replaces it entirely.
func SpawnUnix(_ context.Context, _ []string, _ string, _, _ uint16) (Session, error) {
	return nil, ErrNotImplemented
}
