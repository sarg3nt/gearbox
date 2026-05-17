package console

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sarg3nt/gearbox-agent/internal/api/console/pty"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// Handler owns the runtime state for the /api/v1/console/* surface —
// the token manager, the audit event bus, and the PTY spawner.
// Construct one per Server.
type Handler struct {
	Tokens *TokenManager
	Logger *slog.Logger

	// Audit is where session-start / session-end events go. In
	// production this is an *events.Bus; tests substitute a capture.
	// The interface is intentionally narrow — Handler doesn't
	// subscribe, it only publishes.
	Audit auditPublisher

	// Shell is the command run inside the PTY. Defaults to
	// /bin/bash -l on linux/darwin; the agent reads
	// HAPROXY_AGENT_CONSOLE_SHELL to override for non-bash hosts
	// (alpine, BSD, etc.). The split into argv slots avoids ever
	// running through a parent shell — no opportunity for word
	// splitting / glob expansion of operator-supplied strings.
	Shell []string

	// RunAsUID is an optional numeric UID the spawned shell drops
	// to before exec. Empty (default) means inherit the agent's UID
	// — which on a root agent yields a root shell, by design.
	// See [#89] for the privilege model.
	RunAsUID string

	// Spawner is the function used to attach a PTY to the WS. nil
	// means "echo mode" — frames are bounced back to the client
	// without a real shell. This is the Phase 1a path; production
	// installs set Spawner to pty.SpawnUnix (or its container/SSH
	// equivalents from later phases). Tests inject directly to
	// avoid spawning real processes.
	Spawner pty.Spawner

	// Mode is the string value reported in the capabilities envelope
	// and in audit events. Defaults to ModeEcho when Spawner is nil,
	// ModeHostPTY when Spawner is set. Container/SSH wiring in later
	// phases overrides this at construction.
	Mode string

	// IdleTimeout bounds how long a session may sit silent before the
	// server hangs it up. Defaults to 15 minutes — shells left open in
	// a forgotten browser tab are the most common source of long-lived
	// sessions, and a 15-minute timeout is short enough to bound
	// exposure without disrupting interactive work.
	IdleTimeout time.Duration

	// MaxFrameBytes caps the size of a single inbound frame (after JSON
	// decode + base64 decode of Data). The default 64 KiB matches a
	// generous paste from a terminal; legitimate interactive use is
	// many orders of magnitude smaller.
	MaxFrameBytes int

	// ReadBufBytes sizes the buffer used to pump PTY stdout to the WS.
	// 4 KiB is a sensible default for character-at-a-time interactive
	// traffic — a single screen redraw fits in two or three frames.
	ReadBufBytes int

	// RecordSessions, when true, writes an NDJSON transcript of every
	// session under DataDir/console-sessions/<box>-<ts>-<sid>.ndjson
	// (mode 0600, parent dir 0700). Off by default — recording shells
	// is sensitive and the user shouldn't be surprised by it.
	// Operators opt in via HAPROXY_AGENT_CONSOLE_RECORD=true.
	RecordSessions bool

	// DataDir is where session recordings live. Sourced from
	// HAPROXY_AGENT_DATA_DIR via main wiring; passed in explicitly
	// so this package stays independent of the config package.
	DataDir string
}

// NewHandler constructs a Handler with sensible defaults. Pass the
// production event bus as audit — tests construct the Handler directly
// with an injected capture instead of calling this.
//
// Mode selection cascade (first match wins):
//
//  1. Agent in a container with HAPROXY_AGENT_HOST_EXEC=nsenter AND
//     nsenter is usable → Spawner = SpawnNsenter, Mode = nsenter.
//     This is the TrueNAS/docker-host path; requires pid:host +
//     privileged on the container.
//  2. Agent in a container with HAPROXY_AGENT_HOST_EXEC=ssh-bridge
//     AND the four SSH env vars validate → Spawner = SSHBridgeSpawner,
//     Mode = ssh_bridge. The TrueNAS-friendly fallback.
//  3. Agent on host (or container without a configured bridge) →
//     Spawner = SpawnUnix (POSIX) or SpawnUnix's ConPTY-backed Windows
//     equivalent (same exported name across builds), Mode = host_pty.
//
// Notes:
//   - No "platform unsupported → echo" case exists today; every
//     platform Go builds for has a PTY backend. If a future platform
//     genuinely lacks one, hostSpawnerAvailable() returns false and
//     we degrade to echo.
//
// Operators set the shell + run-as via HAPROXY_AGENT_CONSOLE_SHELL
// and HAPROXY_AGENT_CONSOLE_RUN_AS regardless of mode.
func NewHandler(bus *events.Bus, logger *slog.Logger) *Handler {
	h := &Handler{
		Tokens:        NewTokenManager(),
		Logger:        logger,
		IdleTimeout:   15 * time.Minute,
		MaxFrameBytes: 64 * 1024,
		ReadBufBytes:  4 * 1024,
		Shell:         defaultShell(),
	}
	if bus != nil {
		h.Audit = bus
	}
	switch pickHostExecMode() {
	case modeUnavailable:
		// No PTY backend on this platform. Drops to echo mode so
		// the dashboard's UI still works for protocol prototyping.
		h.Mode = ModeEcho
	case modeNsenter:
		h.Spawner = pty.SpawnNsenter
		h.Mode = ModeNsenter
		if logger != nil {
			logger.Info("console: nsenter host-exec selected (container → host via PID 1 namespaces)")
		}
	case modeSSHBridge:
		cfg, err := pty.LoadSSHBridgeConfigFromEnv()
		if err != nil {
			if logger != nil {
				logger.Error("console: ssh_bridge requested but config is invalid; degrading to echo mode", "error", err)
			}
			h.Mode = ModeEcho
		} else {
			h.Spawner = pty.SSHBridgeSpawner(cfg)
			h.Mode = ModeSSHBridge
			if logger != nil {
				logger.Info("console: ssh_bridge host-exec selected", "host", cfg.Host, "user", cfg.User)
			}
		}
	default:
		h.Spawner = pty.SpawnUnix
		h.Mode = ModeHostPTY
	}
	if v := os.Getenv("HAPROXY_AGENT_CONSOLE_SHELL"); v != "" {
		h.Shell = strings.Fields(v)
	}
	if v := os.Getenv("HAPROXY_AGENT_CONSOLE_RUN_AS"); v != "" {
		h.RunAsUID = v
	}
	// HAPROXY_AGENT_CONSOLE_IDLE_TIMEOUT lets operators override the
	// 15-minute default. Format is a Go duration string ("30m", "2h",
	// "168h" to effectively disable for a week). Invalid values fall
	// back to the default with a warning so a typo doesn't silently
	// leave sessions vulnerable. We don't accept "0" — a zero deadline
	// would cause every read to fail immediately; if you need
	// no-effective-timeout, set a very large duration.
	if v := os.Getenv("HAPROXY_AGENT_CONSOLE_IDLE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			if logger != nil {
				logger.Warn("console: invalid HAPROXY_AGENT_CONSOLE_IDLE_TIMEOUT, falling back to default",
					"value", v, "default", h.IdleTimeout, "error", err)
			}
		case d <= 0:
			if logger != nil {
				logger.Warn("console: HAPROXY_AGENT_CONSOLE_IDLE_TIMEOUT must be positive, falling back to default",
					"value", v, "default", h.IdleTimeout)
			}
		default:
			h.IdleTimeout = d
			if logger != nil {
				logger.Info("console: idle timeout overridden", "value", d)
			}
		}
	}
	if os.Getenv("HAPROXY_AGENT_CONSOLE_RECORD") == "true" {
		h.RecordSessions = true
		h.DataDir = os.Getenv("HAPROXY_AGENT_DATA_DIR")
		if h.DataDir == "" {
			h.DataDir = "/var/lib/gearbox-agent"
		}
		if logger != nil {
			logger.Warn("Console session recording ENABLED — transcripts written to "+h.DataDir+"/console-sessions",
				"format", "ndjson", "perms", "0600")
		}
	}
	return h
}

// pickHostExecMode collapses the platform + host-exec detection into a
// single tag used by NewHandler's switch. Pure function for testability
// (the pty subpackage's detector is platform-specific and hard to mock).
type internalMode int

const (
	modeHostDirect internalMode = iota
	modeUnavailable
	modeNsenter
	modeSSHBridge
)

func pickHostExecMode() internalMode {
	if !hostSpawnerAvailable() {
		return modeUnavailable
	}
	switch pty.HostExecDetect() {
	case pty.HostExecNsenter:
		return modeNsenter
	case pty.HostExecSSHBridge:
		return modeSSHBridge
	default:
		return modeHostDirect
	}
}

// hostSpawnerAvailable reports whether pty.SpawnUnix on this platform
// actually returns a usable session. Phase 1b shipped Linux + macOS
// (unix build tag); Phase 3 added a ConPTY-backed Windows
// implementation (also exported as SpawnUnix for handler symmetry).
// So today the answer is "yes" on every platform Go builds for —
// kept as a function so a future platform that genuinely has no PTY
// support (plan9?) can opt out.
func hostSpawnerAvailable() bool {
	return true
}

// defaultShell returns the platform's typical interactive login shell.
// The empty-array case (Windows) is never used because the spawner is
// nil there.
func defaultShell() []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"powershell.exe", "-NoLogo"}
	default:
		return []string{"/bin/bash", "-l"}
	}
}

// Close releases the token manager's cleanup goroutine.
func (h *Handler) Close() {
	if h.Tokens != nil {
		h.Tokens.Close()
	}
}

// WebSocket protocol timing — borrowed verbatim from api/websocket.go so
// the events and console channels behave identically to operators
// watching connection lifecycles. If these ever diverge it should be
// because the console channel needs *tighter* deadlines, not looser.
const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

// upgrader is the console-specific WebSocket upgrader. CheckOrigin
// matches the api package's logic — same canonicalization, same
// AGENT_ALLOWED_ORIGINS allowlist — but lives here so the console
// package can be developed and tested without an import cycle on the
// outer api package.
//
// Buffers are sized for character-at-a-time interactive traffic, not
// throughput. A 4 KiB read buffer comfortably holds the largest
// reasonable single keystroke burst (e.g. a pasted command line); a
// 32 KiB write buffer holds a screen-clearing redraw without
// fragmenting. Larger sizes only help bulk transfers, which a console
// is not.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin:     checkOrigin,
}

// checkOrigin mirrors the events-channel CheckOrigin: same-origin by
// default, override via AGENT_ALLOWED_ORIGINS comma list with "*" for
// wildcard (development only). Lives here instead of importing from the
// api package so the console subpackage stays free of upward imports.
//
// See api/websocket.go for the canonicalization rationale (2026-05
// security audit P1-5). This is a deliberate copy — diverging the two
// origin-check policies would be a latent footgun. If a third
// WebSocket channel appears, extract this to a shared helper.
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser client (curl, websocat, Go) — Origin only
		// matters for browser-driven CSWSH.
		return true
	}
	canon, ok := canonicalOrigin(origin)
	if !ok {
		return false
	}
	allowed := os.Getenv("AGENT_ALLOWED_ORIGINS")
	if allowed != "" {
		for _, entry := range strings.Split(allowed, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			if entry == "*" {
				return true
			}
			if c, ok := canonicalOrigin(entry); ok && c == canon {
				return true
			}
		}
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "http" || proto == "https" {
		scheme = proto
	}
	return canon == canonicalHost(scheme, r.Host)
}

func canonicalOrigin(s string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u == nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port == "" {
		return scheme + "://" + host, true
	}
	return scheme + "://" + host + ":" + port, true
}

func canonicalHost(scheme, hostPort string) string {
	scheme = strings.ToLower(scheme)
	host, port, err := net.SplitHostPort(strings.ToLower(hostPort))
	if err != nil {
		host = strings.Trim(strings.ToLower(hostPort), "[]")
		return scheme + "://" + host
	}
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}

// HandleWS handles the WebSocket upgrade at GET /api/v1/console/ws.
// Token is required via the ?token= query parameter; only single-use
// console tokens issued via POST /api/v1/console/token are accepted.
//
// When Spawner is set (the production path on Linux/macOS), the
// handler attaches a real PTY to the WS: stdin/stdout flow through
// FrameTypeData, resize requests reach the kernel, signals reach the
// process group, and audit events record the child's exit code. When
// Spawner is nil (Phase 1a fallback, Windows pre-Phase-3), data
// frames are echoed back to the client.
//
//	@Summary		Console WebSocket
//	@Description	Upgrades to a WebSocket carrying a JSON-framed console session. Pass a valid token from POST /api/v1/console/token in the ?token= query parameter. When the agent has a PTY backend, a real shell is attached; otherwise data frames are echoed.
//	@Tags			Console
//	@Produce		json
//	@Param			token	query	string	true	"Single-use console token from /api/v1/console/token"
//	@Success		101		"Switching Protocols"
//	@Failure		401		{string}	string	"Unauthorized"
//	@Router			/api/v1/console/ws [get]
func (h *Handler) HandleWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" || h.Tokens == nil || !h.Tokens.Validate(token) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if h.Logger != nil {
			h.Logger.Warn("console: WS upgrade failed", "remote_addr", r.RemoteAddr, "error", err)
		}
		return
	}
	defer func() { _ = conn.Close() }()

	sessionID := newSessionID()
	startedAt := time.Now()
	uid := os.Geteuid()
	mode := h.Mode
	if mode == "" {
		mode = ModeEcho
	}

	emitSessionStart(h.Audit, sessionID, r.RemoteAddr, mode, uid, startedAt)
	if h.Logger != nil {
		h.Logger.Info("console: session opened",
			"session_id", sessionID,
			"remote_addr", r.RemoteAddr,
			"mode", mode,
			"effective_uid", uid,
			"run_as", h.RunAsUID,
		)
	}

	// Optional NDJSON transcript. Open lazily so a recorder failure
	// (disk full, perms wrong on the data dir) doesn't block the
	// session — we log the open error and keep going. The session
	// audit record on the bus is always written regardless.
	var recorder *Recorder
	if h.RecordSessions && h.DataDir != "" {
		// Box-name isn't known to the agent (the dashboard is the
		// thing that knows boxes); use the remote IP as the
		// per-file disambiguator. Operators correlate file → box
		// via the matching audit event on the dashboard side.
		rec, err := OpenRecorder(h.DataDir, r.RemoteAddr, sessionID)
		if err != nil {
			if h.Logger != nil {
				h.Logger.Warn("console: recorder open failed; session continues unrecorded", "error", err)
			}
		} else {
			recorder = rec
			recorder.LogOpen(sessionID, mode, uid)
		}
	}

	var bytesIn, bytesOut atomic.Int64
	var reason string
	var exitCode int
	if h.Spawner != nil {
		reason, exitCode = h.ptyLoop(r.Context(), conn, &bytesIn, &bytesOut, recorder)
	} else {
		reason = h.echoLoop(conn, &bytesIn, &bytesOut, recorder)
	}

	if recorder != nil {
		_ = recorder.Close(reason, exitCode)
	}

	emitSessionEnd(h.Audit, sessionID, reason, bytesIn.Load(), bytesOut.Load(), time.Since(startedAt), exitCode)
	if h.Logger != nil {
		h.Logger.Info("console: session closed",
			"session_id", sessionID,
			"reason", reason,
			"exit_code", exitCode,
			"bytes_in", bytesIn.Load(),
			"bytes_out", bytesOut.Load(),
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	}
}

// ptyLoop is the production path. It spawns a shell via h.Spawner,
// then runs three goroutines: WS reader (client → PTY), PTY reader
// (PTY → client), and ping ticker. The first one to error wins and
// drives the close reason.
//
// Returns (reason, exitCode). exitCode is the child's exit status
// when reason == "exit"; -1 otherwise.
func (h *Handler) ptyLoop(parentCtx context.Context, conn *websocket.Conn, bytesIn, bytesOut *atomic.Int64, rec *Recorder) (string, int) {
	conn.SetReadLimit(int64(h.MaxFrameBytes) * 2)
	_ = conn.SetReadDeadline(time.Now().Add(h.IdleTimeout))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(h.IdleTimeout))
		return nil
	})

	// Wire context cancellation to PTY child termination — when the
	// outer request context cancels (server shutdown, client drop),
	// the SpawnContext-backed exec.Cmd kills the child for us.
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	sess, err := h.Spawner(ctx, h.Shell, h.RunAsUID, 80, 24)
	if err != nil {
		h.writeErr(conn, ErrCodeInternal, "failed to start shell")
		if h.Logger != nil {
			h.Logger.Error("console: spawner failed", "error", err)
		}
		return "spawn_error", -1
	}
	defer func() { _ = sess.Close() }()

	// reasonCh is buffered to 1 so the first writer wins and the
	// others drop their reason silently. Each goroutine that can
	// terminate the session writes here and then returns.
	reasonCh := make(chan string, 1)

	// PTY → WS pump. When the child exits (PTY EOF) or the master
	// FD goes away, we close the WS connection — that wakes up the
	// WS reader's blocking ReadMessage so the outer loop can collect
	// the reason and emit the session-end audit event.
	go func() {
		defer func() { _ = conn.Close() }()
		buf := make([]byte, h.ReadBufBytes)
		for {
			n, err := sess.Reader().Read(buf)
			if n > 0 {
				out := Frame{
					Type: FrameTypeData,
					Data: base64.StdEncoding.EncodeToString(buf[:n]),
				}
				if werr := h.writeFrame(conn, out); werr != nil {
					select {
					case reasonCh <- "write_error":
					default:
					}
					return
				}
				bytesOut.Add(int64(n))
				if rec != nil {
					rec.LogOut(buf[:n])
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					select {
					case reasonCh <- "exit":
					default:
					}
				} else {
					select {
					case reasonCh <- "pty_read_error":
					default:
					}
				}
				return
			}
		}
	}()

	// Ping ticker (keep-alive)
	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-pingDone:
				return
			}
		}
	}()

	// WS → PTY pump (this goroutine, since we need to block until
	// some terminal condition).
	wsReason := h.pumpWStoPTY(conn, sess, bytesIn, rec)
	// The PTY-side goroutine writes its reason to reasonCh *before*
	// it closes the WS conn. So if the WS pump just returned because
	// of a conn close initiated by the PTY pump, reasonCh already
	// has the real reason. Prefer it over the WS pump's
	// generic "client_close". A short timeout absorbs the rare
	// scheduling race where this goroutine wakes up before the
	// reasonCh write commits.
	var reason string
	select {
	case reason = <-reasonCh:
	case <-time.After(50 * time.Millisecond):
		reason = wsReason
	}

	// Kill the child and wait for the reaper to settle so the exit
	// code is populated.
	cancel()
	exitCode := sess.Wait()
	return reason, exitCode
}

// pumpWStoPTY reads frames from the WS and dispatches them to the
// PTY. Returns the close reason, or "" if the WS pump terminated
// without owning the close (PTY-side goroutine got there first).
func (h *Handler) pumpWStoPTY(conn *websocket.Conn, sess pty.Session, bytesIn *atomic.Int64, rec *Recorder) string {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if isIdleTimeout(err) {
				return "idle_timeout"
			}
			return "client_close"
		}
		var f Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			h.writeErr(conn, ErrCodeProtocolViolation, "invalid frame")
			return "protocol_violation"
		}
		switch f.Type {
		case FrameTypeData:
			payload, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				h.writeErr(conn, ErrCodeProtocolViolation, "invalid base64 in data frame")
				return "protocol_violation"
			}
			if len(payload) > h.MaxFrameBytes {
				h.writeErr(conn, ErrCodeProtocolViolation, "data frame exceeds max size")
				return "protocol_violation"
			}
			if _, werr := sess.Write(payload); werr != nil {
				return "pty_write_error"
			}
			bytesIn.Add(int64(len(payload)))
			if rec != nil {
				rec.LogIn(payload)
			}
		case FrameTypeResize:
			if f.Cols > 0 && f.Rows > 0 && f.Cols < 1<<16 && f.Rows < 1<<16 {
				_ = sess.Resize(uint16(f.Cols), uint16(f.Rows))
				if rec != nil {
					rec.LogResize(f.Cols, f.Rows)
				}
			}
		case FrameTypeSignal:
			if f.Signal != "" {
				_ = sess.Signal(f.Signal)
			}
		case FrameTypePing:
			if err := h.writeFrame(conn, Frame{Type: FrameTypePong}); err != nil {
				return "write_error"
			}
		default:
			h.writeErr(conn, ErrCodeProtocolViolation, "unknown frame type")
			return "protocol_violation"
		}
	}
}

// echoLoop is the test/fallback path. Behavior matches Phase 1a: data
// frames are echoed, resize/signal are no-ops, ping → pong.
//
// The loop terminates when:
//   - the client closes the WS (reason "client_close"),
//   - no message arrives within IdleTimeout (reason "idle_timeout"),
//   - a malformed frame arrives (reason "protocol_violation"),
//   - a write fails (reason "write_error").
func (h *Handler) echoLoop(conn *websocket.Conn, bytesIn, bytesOut *atomic.Int64, rec *Recorder) string {
	conn.SetReadLimit(int64(h.MaxFrameBytes) * 2)
	deadline := time.Now().Add(h.IdleTimeout)
	_ = conn.SetReadDeadline(deadline)
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(h.IdleTimeout))
		return nil
	})

	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-pingDone:
				return
			}
		}
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if isIdleTimeout(err) {
				return "idle_timeout"
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return "client_close"
			}
			return "client_close"
		}

		var f Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			h.writeErr(conn, ErrCodeProtocolViolation, "invalid frame")
			return "protocol_violation"
		}

		switch f.Type {
		case FrameTypeData:
			payload, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				h.writeErr(conn, ErrCodeProtocolViolation, "invalid base64 in data frame")
				return "protocol_violation"
			}
			if len(payload) > h.MaxFrameBytes {
				h.writeErr(conn, ErrCodeProtocolViolation, "data frame exceeds max size")
				return "protocol_violation"
			}
			bytesIn.Add(int64(len(payload)))
			if rec != nil {
				rec.LogIn(payload)
			}
			out := Frame{Type: FrameTypeData, Data: f.Data}
			if err := h.writeFrame(conn, out); err != nil {
				return "write_error"
			}
			bytesOut.Add(int64(len(payload)))
			if rec != nil {
				rec.LogOut(payload)
			}
		case FrameTypePing:
			if err := h.writeFrame(conn, Frame{Type: FrameTypePong}); err != nil {
				return "write_error"
			}
		case FrameTypeResize, FrameTypeSignal:
			// Echo mode: accept and ignore.
		default:
			h.writeErr(conn, ErrCodeProtocolViolation, "unknown frame type")
			return "protocol_violation"
		}
	}
}

// writeFrame JSON-encodes and sends a single frame with the standard
// write deadline.
func (h *Handler) writeFrame(conn *websocket.Conn, f Frame) error {
	_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
	return conn.WriteJSON(f)
}

// writeErr sends a FrameTypeErr frame; errors writing it are dropped
// because the caller is about to close the connection anyway.
func (h *Handler) writeErr(conn *websocket.Conn, code, msg string) {
	_ = h.writeFrame(conn, Frame{Type: FrameTypeErr, Reason: code, Msg: msg})
}

// isIdleTimeout reports whether an error from ReadMessage was caused by
// the read deadline. gorilla/websocket wraps os.ErrDeadlineExceeded —
// errors.Is unwraps the chain.
func isIdleTimeout(err error) bool {
	return errors.Is(err, os.ErrDeadlineExceeded)
}

// newSessionID returns a short hex ID used in audit events. 8 bytes is
// enough to disambiguate sessions in a per-host log without becoming
// noise; collisions are not security-sensitive because the audit log is
// already authenticated by who wrote it.
func newSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000")
	}
	return hex.EncodeToString(b)
}
