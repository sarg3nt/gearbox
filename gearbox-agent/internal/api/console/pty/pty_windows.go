//go:build windows

package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/UserExistsError/conpty"
)

// ErrSignalUnsupported is returned by Session.Signal when the named
// signal has no equivalent on this platform.
var ErrSignalUnsupported = errors.New("pty: signal not supported on windows")

// SpawnUnix on Windows backs the Spawner interface with a ConPTY-based
// session. The function name is kept for cross-platform compatibility —
// the handler picks the spawner based on runtime.GOOS, and giving the
// Windows implementation the same name avoids handler-side branching.
// (A future renaming pass could call it SpawnPlatform on both sides.)
//
// runAs is currently ignored on Windows; the child runs under the
// agent's account. Phase 3 follow-up: STARTUPINFOEX with a
// PROC_THREAD_ATTRIBUTE_HANDLE_LIST + LogonUser for under-the-agent
// privilege drop.
//
// NOTE: this code path has been compiled but not run-tested in CI as
// of the Phase 3 commit. The fleet this ships for is Linux/macOS;
// Windows support is the "doesn't break the build, ready for the
// first operator to try it" tier, not "battle-tested." A real
// Windows install should expect to find at least one rough edge.
func SpawnUnix(ctx context.Context, command []string, _ string, cols, rows uint16) (Session, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("pty: empty command")
	}
	// conpty.Start takes a single command-line string. Join argv
	// with spaces — that's the standard Windows convention, and
	// operators who need quoting should set HAPROXY_AGENT_CONSOLE_SHELL
	// to something like `cmd /c "C:\\path with spaces\\app.exe" --flag`.
	cmd := strings.Join(command, " ")
	cpty, err := conpty.Start(
		cmd,
		conpty.ConPtyDimensions(int(cols), int(rows)),
		conpty.ConPtyEnv(nil), // inherit
	)
	if err != nil {
		return nil, fmt.Errorf("pty: ConPTY start failed: %w", err)
	}

	s := &windowsSession{
		cpty:   cpty,
		exitCh: make(chan struct{}),
	}
	go s.reap(ctx)
	return s, nil
}

// windowsSession wraps a *conpty.ConPty as a pty.Session. ConPTY
// exposes a single io.ReadWriteCloser combining stdin and stdout/stderr,
// which is exactly the contract we want.
type windowsSession struct {
	cpty *conpty.ConPty

	mu       sync.Mutex
	exited   bool
	exitCode int
	exitCh   chan struct{}
}

func (s *windowsSession) Reader() io.Reader              { return s.cpty }
func (s *windowsSession) Write(p []byte) (int, error)    { return s.cpty.Write(p) }
func (s *windowsSession) Resize(cols, rows uint16) error { return s.cpty.Resize(int(cols), int(rows)) }

// Signal on Windows is best-effort. SIGINT maps to writing Ctrl-C
// (0x03) to the input stream — ConPTY translates that to a console
// control event. SIGTERM/KILL fall through to Close, which terminates
// the child via ConPTY's lifecycle. Anything else returns
// ErrSignalUnsupported so the dashboard sees a clear "not for this
// platform" rather than silent failure.
func (s *windowsSession) Signal(name string) error {
	switch name {
	case "INT", "SIGINT":
		_, err := s.cpty.Write([]byte{0x03})
		return err
	case "TERM", "SIGTERM", "KILL", "SIGKILL":
		return s.cpty.Close()
	}
	return ErrSignalUnsupported
}

func (s *windowsSession) Wait() int {
	<-s.exitCh
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

func (s *windowsSession) Close() error { return s.cpty.Close() }

func (s *windowsSession) reap(ctx context.Context) {
	defer close(s.exitCh)
	// conpty.ConPty.Wait blocks until the child exits, returning the
	// exit code. The context isn't directly honored by the upstream
	// API; if cancellation comes in while we're still waiting, we
	// close the ConPty to force exit.
	doneCh := make(chan uint32, 1)
	go func() {
		code, _ := s.cpty.Wait(ctx)
		doneCh <- code
	}()
	var code uint32
	select {
	case code = <-doneCh:
	case <-ctx.Done():
		_ = s.cpty.Close()
		code = <-doneCh
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exited = true
	s.exitCode = int(code)
}
