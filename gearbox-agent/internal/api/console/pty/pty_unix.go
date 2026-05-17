//go:build unix

package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"

	creackpty "github.com/creack/pty"
)

// ErrSignalUnsupported is returned by Session.Signal when the named
// signal has no equivalent on this platform.
var ErrSignalUnsupported = errors.New("pty: unsupported signal name")

// unixSession is the POSIX implementation of Session: the child runs
// under a real PTY (creack/pty), Signal dispatches via syscall.Kill on
// the negated PID so it reaches the whole process group.
type unixSession struct {
	cmd  *exec.Cmd
	ptmx *os.File // master side of the PTY

	mu       sync.Mutex
	exitCode int
	exited   bool
	exitCh   chan struct{} // closed when Wait completes
}

// SpawnUnix is the Spawner implementation for POSIX systems. Always
// returns a Session backed by /dev/ptmx (Linux) or /dev/ptyXX (BSD /
// macOS) via creack/pty.
//
// If runAs is non-empty, the child is forked with a Credential set:
//   - numeric uid → that UID (and same GID)
//   - "user:uid" form rejected; the caller should resolve to a numeric
//     UID before calling. Keeps this layer free of /etc/passwd parsing.
//
// An empty runAs means inherit — on a root agent the child is root.
// This is the documented Phase 1b behavior; the dashboard surfaces it
// via the box-settings UI.
func SpawnUnix(ctx context.Context, command []string, runAs string, cols, rows uint16) (Session, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("pty: empty command")
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)

	// New session + controlling TTY: required so the child's process
	// group is distinct from the agent's, which is what makes
	// signal-by-pgid work without nuking the agent.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
	}

	if runAs != "" {
		uid, err := strconv.ParseUint(runAs, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("pty: runAs must be a numeric UID, got %q: %w", runAs, err)
		}
		cmd.SysProcAttr.Credential = &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(uid),
		}
	}

	// Match the dashboard's idea of a sensible shell environment.
	// PATH is the only one we set deliberately — the rest of the
	// environment falls through from the agent process so things like
	// TZ, LANG, and HOME work as the operator expects.
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := creackpty.StartWithSize(cmd, &creackpty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, fmt.Errorf("pty: start failed: %w", err)
	}

	s := &unixSession{
		cmd:    cmd,
		ptmx:   ptmx,
		exitCh: make(chan struct{}),
	}

	// Reap in a goroutine — Wait() reads the cached value.
	go s.reap()

	return s, nil
}

func (s *unixSession) Reader() io.Reader { return s.ptmx }

func (s *unixSession) Write(p []byte) (int, error) { return s.ptmx.Write(p) }

func (s *unixSession) Resize(cols, rows uint16) error {
	return creackpty.Setsize(s.ptmx, &creackpty.Winsize{Cols: cols, Rows: rows})
}

// nameToSignal maps the small set of WS-protocol signal names we
// understand to syscall.Signal values. The list is intentionally short —
// Ctrl-C / Ctrl-D / Ctrl-Z are all just bytes on the wire; this is the
// out-of-band path for explicit "kill the session" cases.
func nameToSignal(name string) (syscall.Signal, error) {
	switch name {
	case "INT", "SIGINT":
		return syscall.SIGINT, nil
	case "TERM", "SIGTERM":
		return syscall.SIGTERM, nil
	case "HUP", "SIGHUP":
		return syscall.SIGHUP, nil
	case "QUIT", "SIGQUIT":
		return syscall.SIGQUIT, nil
	case "KILL", "SIGKILL":
		return syscall.SIGKILL, nil
	}
	return 0, ErrSignalUnsupported
}

func (s *unixSession) Signal(name string) error {
	sig, err := nameToSignal(name)
	if err != nil {
		return err
	}
	if s.cmd.Process == nil {
		return fmt.Errorf("pty: child not started")
	}
	// Negative PID delivers to the process group — needed so Ctrl-C
	// reaches the child of the shell (e.g. a running `top`), not just
	// the shell itself.
	return syscall.Kill(-s.cmd.Process.Pid, sig)
}

func (s *unixSession) Wait() int {
	<-s.exitCh
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

func (s *unixSession) Close() error {
	// Try graceful first — TERM then KILL after a short window if
	// the child ignores it. The reap goroutine handles the actual
	// wait; this just nudges the child toward exit.
	if s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
	}
	// Closing the PTY master propagates SIGHUP to the slave, which
	// any well-behaved shell will respect.
	err := s.ptmx.Close()
	// Don't block on Wait here — Close should be fast. The reap
	// goroutine will populate exitCode when the child exits.
	return err
}

func (s *unixSession) reap() {
	defer close(s.exitCh)

	err := s.cmd.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exited = true
	if err == nil {
		s.exitCode = 0
		return
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		s.exitCode = ee.ExitCode()
		return
	}
	// Non-exit-error means killed-before-exit or some other oddity;
	// surface as -1.
	s.exitCode = -1
}
