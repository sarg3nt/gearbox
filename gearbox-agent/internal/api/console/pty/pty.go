// Package pty wraps platform-specific pseudo-terminal allocation behind
// a single interface so the console handler can stay OS-agnostic.
//
// The contract is small on purpose: spawn a child process with its
// stdio attached to a PTY, read/write bytes on the master side, change
// terminal size, send signals to the child's process group, and wait
// for exit. Each backend file (pty_unix.go, pty_windows.go) implements
// the OS specifics; the handler never touches `os/exec` or `syscall`
// directly.
package pty

import (
	"context"
	"io"
)

// Session is a running shell attached to a PTY. The implementation is
// platform-specific; the interface is what console/handler.go consumes.
type Session interface {
	// Reader returns the master-side reader (stdout + stderr merged,
	// which is the standard PTY contract — the kernel already
	// combines them on the slave side).
	Reader() io.Reader

	// Write sends bytes to the child's stdin via the master side.
	Write(p []byte) (int, error)

	// Resize updates the child terminal's window size. cols and rows
	// match the WS protocol field names; ws_xpixel / ws_ypixel are
	// always zero because xterm.js doesn't report them.
	Resize(cols, rows uint16) error

	// Signal sends a POSIX signal to the child's process group. On
	// Windows the implementation maps the well-known names to ConPTY
	// control sequences as best it can; unsupported signals return
	// ErrSignalUnsupported.
	Signal(name string) error

	// Wait blocks until the child exits and returns its exit code.
	// 0 = clean exit; -1 = killed by signal before producing a code.
	// Subsequent calls return the cached value.
	Wait() int

	// Close terminates the child if it's still running and releases
	// the master FD. Idempotent. Calling Close before Wait drains
	// returns -1 from Wait.
	Close() error
}

// Spawn launches a shell attached to a fresh PTY. ctx is wired to
// child cancellation — when ctx is cancelled, the child is killed and
// Wait unblocks with -1.
//
// The `runAs` field, when non-empty, asks the implementation to drop
// to a less-privileged UID before exec. An empty string means "inherit
// the parent's UID" — which on a root-running agent means the spawned
// shell is root. This is intentional; see [#89] for the privilege
// discussion. On Windows the field is ignored (Phase 3 may revisit).
type Spawner func(ctx context.Context, cmd []string, runAs string, cols, rows uint16) (Session, error)
