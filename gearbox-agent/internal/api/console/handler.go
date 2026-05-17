package console

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// Handler owns the runtime state for the /api/v1/console/* surface —
// the token manager and the audit event bus. Construct one per Server.
type Handler struct {
	Tokens *TokenManager
	Logger *slog.Logger

	// Audit is where session-start / session-end events go. In
	// production this is an *events.Bus; tests substitute a capture.
	// The interface is intentionally narrow — Handler doesn't
	// subscribe, it only publishes.
	Audit auditPublisher

	// IdleTimeout bounds how long a session may sit silent before the
	// server hangs it up. Defaults to 15 minutes — shells left open in
	// a forgotten browser tab are the most common source of long-lived
	// sessions, and a 15-minute timeout is short enough to bound
	// exposure without disrupting interactive work.
	IdleTimeout time.Duration

	// MaxFrameBytes caps the size of a single inbound frame (after JSON
	// decode + base64 decode of Data). Phase 1a is echo-only, so any
	// frame the agent has to copy back into its own buffer is bounded
	// here. The default 64 KiB matches a generous paste from a
	// terminal; legitimate interactive use is many orders of magnitude
	// smaller.
	MaxFrameBytes int
}

// NewHandler constructs a Handler with sensible defaults. Pass the
// production event bus as audit — tests construct the Handler directly
// with an injected capture instead of calling this.
func NewHandler(bus *events.Bus, logger *slog.Logger) *Handler {
	h := &Handler{
		Tokens:        NewTokenManager(),
		Logger:        logger,
		IdleTimeout:   15 * time.Minute,
		MaxFrameBytes: 64 * 1024,
	}
	// Storing bus directly as auditPublisher would wrap a nil *events.Bus
	// in a non-nil interface — emitSessionStart's nil-check wouldn't fire
	// and we'd panic. Only set Audit when there's a real bus.
	if bus != nil {
		h.Audit = bus
	}
	return h
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
	// net.SplitHostPort handles IPv6 brackets; fall back to the raw
	// value when there's no port at all.
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
// Phase 1a: every inbound FrameTypeData is echoed back verbatim. Resize,
// signal, and ping frames are accepted and acknowledged but have no
// side effects (no PTY exists yet). The handler emits
// EventConsoleSessionStart on successful upgrade and
// EventConsoleSessionEnd when the connection closes for any reason.
//
//	@Summary		Console WebSocket
//	@Description	Upgrades to a WebSocket carrying a JSON-framed console session. Pass a valid token from POST /api/v1/console/token in the ?token= query parameter. Phase 1a echoes data frames back to the client; later phases attach a PTY.
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
		// Upgrader has already written an HTTP error response; just
		// log so we have a breadcrumb.
		if h.Logger != nil {
			h.Logger.Warn("console: WS upgrade failed", "remote_addr", r.RemoteAddr, "error", err)
		}
		return
	}
	defer func() { _ = conn.Close() }()

	sessionID := newSessionID()
	startedAt := time.Now()
	uid := os.Geteuid()

	emitSessionStart(h.Audit, sessionID, r.RemoteAddr, ModeEcho, uid, startedAt)
	if h.Logger != nil {
		h.Logger.Info("console: session opened",
			"session_id", sessionID,
			"remote_addr", r.RemoteAddr,
			"mode", ModeEcho,
			"effective_uid", uid,
		)
	}

	var bytesIn, bytesOut atomic.Int64
	reason := h.echoLoop(conn, &bytesIn, &bytesOut)

	emitSessionEnd(h.Audit, sessionID, reason, bytesIn.Load(), bytesOut.Load(), time.Since(startedAt))
	if h.Logger != nil {
		h.Logger.Info("console: session closed",
			"session_id", sessionID,
			"reason", reason,
			"bytes_in", bytesIn.Load(),
			"bytes_out", bytesOut.Load(),
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	}
}

// echoLoop reads frames from the client, echoes data frames back, and
// keeps the connection alive with pings. Returns the close reason
// (a short tag suitable for the audit event).
//
// The loop terminates when:
//   - the client closes the WS (reason "client_close"),
//   - no message arrives within IdleTimeout (reason "idle_timeout"),
//   - a malformed frame arrives (reason "protocol_violation"),
//   - a write fails (reason "write_error").
func (h *Handler) echoLoop(conn *websocket.Conn, bytesIn, bytesOut *atomic.Int64) string {
	// Read deadline gets pushed forward on every message; the idle
	// timeout enforces the gap-between-messages cap. Initial deadline
	// is the idle timeout itself.
	conn.SetReadLimit(int64(h.MaxFrameBytes) * 2) // *2 to accommodate base64 + JSON envelope overhead
	deadline := time.Now().Add(h.IdleTimeout)
	_ = conn.SetReadDeadline(deadline)
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(h.IdleTimeout))
		return nil
	})

	// Ping ticker keeps the connection from looking dead to NAT /
	// load balancer middleware.
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
			// Decode just to update the audit byte counter, then
			// echo the *original* envelope back so the client gets
			// byte-for-byte what it sent.
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

			out := Frame{Type: FrameTypeData, Data: f.Data}
			if err := h.writeFrame(conn, out); err != nil {
				return "write_error"
			}
			bytesOut.Add(int64(len(payload)))

		case FrameTypePing:
			if err := h.writeFrame(conn, Frame{Type: FrameTypePong}); err != nil {
				return "write_error"
			}

		case FrameTypeResize, FrameTypeSignal:
			// Phase 1a: accept, no-op. The dashboard can start
			// sending these now and they'll start mattering in
			// Phase 1b.

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
		// Fall back to a timestamp-derived ID if the OS RNG fails —
		// extremely unlikely, and we'd rather have a less-unique ID
		// than no session record at all.
		return time.Now().UTC().Format("20060102T150405.000000")
	}
	return hex.EncodeToString(b)
}
