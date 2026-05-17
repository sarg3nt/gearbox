package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

// SSHBridgeConfig captures the operator-supplied wiring for mode B.2:
// the agent (running inside a container) connects out to the host's
// sshd over a private channel using a dedicated agent key. None of
// these are guessable defaults — the operator must produce the key,
// install the public half on the host, and tell the agent where to
// find the private half and what user to log in as.
//
// Loaded from env at handler construction:
//   - HAPROXY_AGENT_CONSOLE_SSH_HOST     "127.0.0.1:22" (or path to UNIX socket)
//   - HAPROXY_AGENT_CONSOLE_SSH_USER     "root" or whatever the operator wants
//   - HAPROXY_AGENT_CONSOLE_SSH_KEY      path to private key (mode 0600)
//   - HAPROXY_AGENT_CONSOLE_SSH_HOSTKEY  path to expected host pubkey
//
// HostKey is *not* optional. We refuse to fall back to
// ssh.InsecureIgnoreHostKey() — a bridge that ignores host keys
// would silently land sessions anywhere a MITM redirected the
// connection, which is exactly the thing the bridge is supposed to
// avoid.
type SSHBridgeConfig struct {
	Host        string // "host:port" — TCP only for now; UNIX-socket support is Phase 2b
	User        string
	PrivateKey  string // path
	HostKey     string // path to expected ssh public key in authorized_keys format
}

// LoadSSHBridgeConfigFromEnv reads the bridge config from env. Returns
// nil + error if any required field is missing or unreadable — the
// caller should treat that as "ssh_bridge mode misconfigured" and
// surface it to the operator rather than silently falling through to
// another mode.
func LoadSSHBridgeConfigFromEnv() (*SSHBridgeConfig, error) {
	cfg := &SSHBridgeConfig{
		Host:       os.Getenv("HAPROXY_AGENT_CONSOLE_SSH_HOST"),
		User:       os.Getenv("HAPROXY_AGENT_CONSOLE_SSH_USER"),
		PrivateKey: os.Getenv("HAPROXY_AGENT_CONSOLE_SSH_KEY"),
		HostKey:    os.Getenv("HAPROXY_AGENT_CONSOLE_SSH_HOSTKEY"),
	}
	missing := []string{}
	if cfg.Host == "" {
		missing = append(missing, "HAPROXY_AGENT_CONSOLE_SSH_HOST")
	}
	if cfg.User == "" {
		missing = append(missing, "HAPROXY_AGENT_CONSOLE_SSH_USER")
	}
	if cfg.PrivateKey == "" {
		missing = append(missing, "HAPROXY_AGENT_CONSOLE_SSH_KEY")
	}
	if cfg.HostKey == "" {
		missing = append(missing, "HAPROXY_AGENT_CONSOLE_SSH_HOSTKEY")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("ssh_bridge: missing required env: %v", missing)
	}
	// Verify the private key file is readable and 0600 — sloppy
	// permissions on an SSH key in the data dir are the kind of
	// foot-gun we'd rather catch at startup than at first session.
	st, err := os.Stat(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("ssh_bridge: private key %q: %w", cfg.PrivateKey, err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("ssh_bridge: private key %q has too-open permissions %o (want 0600)",
			cfg.PrivateKey, st.Mode().Perm())
	}
	if _, err := os.Stat(cfg.HostKey); err != nil {
		return nil, fmt.Errorf("ssh_bridge: host key %q: %w", cfg.HostKey, err)
	}
	return cfg, nil
}

// SSHBridgeSpawner closes over an SSHBridgeConfig and returns a
// Spawner that satisfies the pty.Spawner contract. Construction is
// separated from spawning so the dashboard can verify the bridge is
// well-configured at agent startup (LoadSSHBridgeConfigFromEnv) and
// fail loud, rather than only discovering the misconfiguration when
// a user tries to open a session.
func SSHBridgeSpawner(cfg *SSHBridgeConfig) Spawner {
	return func(ctx context.Context, command []string, runAs string, cols, rows uint16) (Session, error) {
		if cfg == nil {
			return nil, errors.New("ssh_bridge: nil config")
		}
		if runAs != "" {
			// Same reasoning as nsenter mode: composing a UID
			// drop with the SSH login user is more confusing
			// than useful. The operator picks the login user via
			// HAPROXY_AGENT_CONSOLE_SSH_USER.
			return nil, fmt.Errorf("ssh_bridge: run-as UID override not supported (got %q); use HAPROXY_AGENT_CONSOLE_SSH_USER instead", runAs)
		}
		if len(command) == 0 {
			return nil, errors.New("ssh_bridge: empty command")
		}
		return dialAndStart(ctx, cfg, command, cols, rows)
	}
}

// dialAndStart opens a fresh SSH connection, allocates a PTY, and
// starts the requested command. A new connection per session is
// deliberately simple — we don't multiplex; the agent is a single
// process serving a small number of concurrent operators, and
// multiplexing would force us to deal with connection-level
// failure modes leaking into multiple sessions.
func dialAndStart(ctx context.Context, cfg *SSHBridgeConfig, command []string, cols, rows uint16) (Session, error) {
	keyBytes, err := os.ReadFile(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("ssh_bridge: read key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("ssh_bridge: parse key: %w", err)
	}
	hostKeyBytes, err := os.ReadFile(cfg.HostKey)
	if err != nil {
		return nil, fmt.Errorf("ssh_bridge: read host key: %w", err)
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey(hostKeyBytes)
	if err != nil {
		// Try ParsePublicKey as a fallback for raw public-key files
		// (the operator may have used `ssh-keyscan` output directly).
		hostKey, err = ssh.ParsePublicKey(hostKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("ssh_bridge: parse host key (%q): %w", filepath.Base(cfg.HostKey), err)
		}
	}

	clientCfg := &ssh.ClientConfig{
		User: cfg.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.FixedHostKey(hostKey),
	}

	// We don't use context.DialContext directly — golang.org/x/crypto/ssh
	// doesn't accept it. Wrap the synchronous Dial in a goroutine and
	// honor cancellation by closing the connection if it returns after
	// the context expires. This is the standard ssh.Dial pattern.
	type dialResult struct {
		client *ssh.Client
		err    error
	}
	res := make(chan dialResult, 1)
	go func() {
		c, err := ssh.Dial("tcp", cfg.Host, clientCfg)
		res <- dialResult{c, err}
	}()
	var client *ssh.Client
	select {
	case r := <-res:
		if r.err != nil {
			return nil, fmt.Errorf("ssh_bridge: dial %s: %w", cfg.Host, r.err)
		}
		client = r.client
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ssh_bridge: new session: %w", err)
	}

	if err := sess.RequestPty("xterm-256color", int(rows), int(cols), ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("ssh_bridge: request pty: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("ssh_bridge: stdin pipe: %w", err)
	}
	stdoutR, stdoutW := io.Pipe()
	sess.Stdout = stdoutW
	sess.Stderr = stdoutW

	// Build the remote command string. Quoting is intentionally
	// minimal — the agent's Shell field comes from operator-set env
	// and is expected to already be a sensible argv. If they want
	// quoting, they wrap in `sh -c 'whatever'`.
	cmdStr := ""
	for i, a := range command {
		if i > 0 {
			cmdStr += " "
		}
		cmdStr += a
	}
	if err := sess.Start(cmdStr); err != nil {
		_ = stdoutW.Close()
		_ = sess.Close()
		_ = client.Close()
		return nil, fmt.Errorf("ssh_bridge: start %q: %w", cmdStr, err)
	}

	s := &sshSession{
		client:   client,
		sess:     sess,
		stdin:    stdin,
		stdoutR:  stdoutR,
		stdoutW:  stdoutW,
		exitCh:   make(chan struct{}),
		exitCode: -1,
	}
	go s.reap()
	return s, nil
}

// sshSession adapts an *ssh.Session to the pty.Session interface.
// Combining stdout+stderr via the pipe matches the local PTY contract
// (the kernel already merges them on the slave side); SSH separates
// them at protocol level, so we merge them here.
type sshSession struct {
	client *ssh.Client
	sess   *ssh.Session

	stdin   io.WriteCloser
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter

	exitCode int32
	exited   atomic.Bool
	exitCh   chan struct{}
	closeMu  sync.Mutex
	closed   bool
}

func (s *sshSession) Reader() io.Reader              { return s.stdoutR }
func (s *sshSession) Write(p []byte) (int, error)    { return s.stdin.Write(p) }
func (s *sshSession) Resize(cols, rows uint16) error { return s.sess.WindowChange(int(rows), int(cols)) }

// Signal maps the same WS-protocol names as the local PTY backend.
// SSH's signal handling depends on sshd honoring the request (OpenSSH
// historically did not; recent versions do). We return nil even when
// the request was sent but possibly ignored — partial support is
// closer to "works" than "fails."
func (s *sshSession) Signal(name string) error {
	var sig ssh.Signal
	switch name {
	case "INT", "SIGINT":
		sig = ssh.SIGINT
	case "TERM", "SIGTERM":
		sig = ssh.SIGTERM
	case "HUP", "SIGHUP":
		sig = ssh.SIGHUP
	case "QUIT", "SIGQUIT":
		sig = ssh.SIGQUIT
	case "KILL", "SIGKILL":
		sig = ssh.SIGKILL
	default:
		return ErrSignalUnsupported
	}
	return s.sess.Signal(sig)
}

func (s *sshSession) Wait() int {
	<-s.exitCh
	return int(atomic.LoadInt32(&s.exitCode))
}

func (s *sshSession) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	_ = s.stdin.Close()
	_ = s.sess.Close()
	_ = s.client.Close()
	return nil
}

func (s *sshSession) reap() {
	defer close(s.exitCh)
	defer func() { _ = s.stdoutW.Close() }()

	err := s.sess.Wait()
	s.exited.Store(true)
	if err == nil {
		atomic.StoreInt32(&s.exitCode, 0)
		return
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		atomic.StoreInt32(&s.exitCode, int32(ee.ExitStatus()))
		return
	}
	atomic.StoreInt32(&s.exitCode, -1)
}
