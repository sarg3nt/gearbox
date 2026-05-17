package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// consoleUpgrader handles browser → dashboard WebSocket upgrades for
// console sessions. CheckOrigin is permissive because the request is
// already gated by a session cookie + permission check upstream; if
// you got here, you're already authenticated as the user who's
// connecting. (Cross-Site WebSocket Hijacking only matters for
// unauthenticated endpoints or those relying solely on the cookie for
// access — we don't.)
var consoleUpgrader = websocket.Upgrader{
	ReadBufferSize:  4 * 1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// APIConsoleCapabilities proxies the agent's /api/v1/console/capabilities
// response to the dashboard caller. Returns 404 with a small JSON
// envelope when the agent has console disabled, so the dashboard's UI
// can branch on that rather than a generic error.
//
// Permission: box_console:view.
func (h *Handler) APIConsoleCapabilities(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentBoxConsole, models.PermissionView) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Box ID is required", http.StatusBadRequest)
		return
	}
	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		http.Error(w, "Box not found", http.StatusNotFound)
		return
	}
	// Per-box opt-in (#89 Phase 2c). The dashboard's per-box
	// toggle is the sole gate on this proxy path. Returning a
	// "disabled" envelope so the dashboard JS branches identically
	// for "box opted out" and "agent unreachable."
	if !server.ConsoleEnabled {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": false,
			"reason":  "console not enabled for this box (toggle on the box settings page)",
		})
		return
	}
	client, err := h.getAgentClient(server)
	if err != nil {
		http.Error(w, "Failed to connect to agent", http.StatusBadGateway)
		return
	}
	caps, err := client.GetConsoleCapabilities()
	if err != nil {
		// 404 from the agent → operator hasn't opted in. Surface
		// that explicitly to the dashboard rather than a generic
		// 502 so the UI can show "console disabled on this box."
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": false,
			"reason":  "console not enabled on this agent",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(caps)
}

// APIConsoleWS proxies a browser WebSocket to the agent's
// /api/v1/console/ws. The dashboard handles the agent-side token
// exchange so the browser never sees the agent's API key — it just
// rides the user's existing session cookie.
//
// Flow:
//  1. Validate cookie + box_console:connect permission (middleware +
//     this handler).
//  2. Resolve box → agent client.
//  3. POST /api/v1/console/token against the agent for a fresh
//     single-use token.
//  4. Dial wss://agent/api/v1/console/ws?token=… as the upstream
//     side of the proxy.
//  5. Upgrade the client-side WS.
//  6. Pump messages in both directions until either side closes.
//
// Audit recording lives on the agent (it sees the actual session
// open/close); the dashboard logs a short "session proxied" line so
// support can correlate by box+user when needed.
func (h *Handler) APIConsoleWS(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentBoxConsole, models.PermissionConnect) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Box ID is required", http.StatusBadRequest)
		return
	}
	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		http.Error(w, "Box not found", http.StatusNotFound)
		return
	}
	// Per-box opt-in (#89 Phase 2c). Refuse before even reaching the
	// agent so a misconfigured box doesn't leak the agent's token
	// endpoint to the audit log on every refused click.
	if !server.ConsoleEnabled {
		http.Error(w, "Console disabled for this box", http.StatusForbidden)
		return
	}
	client, err := h.getAgentClient(server)
	if err != nil {
		h.logger.Error("console proxy: agent client", "box", boxID, "error", err)
		http.Error(w, "Failed to connect to agent", http.StatusBadGateway)
		return
	}

	tokenResp, err := client.GetConsoleToken()
	if err != nil {
		h.logger.Error("console proxy: token exchange", "box", boxID, "error", err)
		http.Error(w, "Failed to authorize console session", http.StatusBadGateway)
		return
	}

	// Build wss:// upstream URL. The agent client's BaseURL is
	// https://... — flipping the scheme to wss:// is the standard
	// way to address a WebSocket on the same TLS listener.
	wsBase, err := url.Parse(client.BaseURL())
	if err != nil {
		http.Error(w, "Invalid agent URL", http.StatusInternalServerError)
		return
	}
	scheme := "wss"
	if wsBase.Scheme == "http" {
		scheme = "ws"
	}
	upstreamURL := scheme + "://" + wsBase.Host + "/api/v1/console/ws?token=" + url.QueryEscape(tokenResp.Token)

	// Dial the agent. Share the same TLS trust policy as the HTTP
	// client (AGENT_CA_CERT_PATH for pinning, GEARBOX_INSECURE_TLS
	// for explicit opt-out, system pool otherwise) so a deployment
	// that hardens REST against MITM gets the WebSocket dial
	// hardened too — no quiet "REST is pinned, WS isn't" gap.
	// See #89 follow-up.
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  agent.BuildTLSConfig(),
	}
	upstream, upstreamResp, err := dialer.Dial(upstreamURL, nil)
	if err != nil {
		code := http.StatusBadGateway
		if upstreamResp != nil {
			code = upstreamResp.StatusCode
		}
		h.logger.Error("console proxy: upstream dial", "box", boxID, "status", code, "error", err)
		http.Error(w, "Failed to open console session: "+err.Error(), code)
		return
	}
	defer func() { _ = upstream.Close() }()

	// Now upgrade the browser side. If this fails after the
	// upstream dial succeeded, close the upstream cleanly to free
	// the agent's session slot.
	downstream, err := consoleUpgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = upstream.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "dashboard upgrade failed"))
		h.logger.Warn("console proxy: downstream upgrade failed", "box", boxID, "error", err)
		return
	}
	defer func() { _ = downstream.Close() }()

	h.logger.Info("console proxy: session opened", "box", boxID)
	startedAt := time.Now()

	// Bidirectional message pump. We use the channel-of-error
	// pattern: whichever side errors first closes both. No need to
	// inspect message contents — the wire format is the agent's
	// JSON frame shape, opaque to the proxy.
	pumpErr := make(chan error, 2)
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = upstream.Close()
			_ = downstream.Close()
		})
	}

	// Browser → agent
	go func() {
		for {
			mt, data, err := downstream.ReadMessage()
			if err != nil {
				pumpErr <- err
				closeBoth()
				return
			}
			if err := upstream.WriteMessage(mt, data); err != nil {
				pumpErr <- err
				closeBoth()
				return
			}
		}
	}()

	// Agent → browser
	go func() {
		for {
			mt, data, err := upstream.ReadMessage()
			if err != nil {
				pumpErr <- err
				closeBoth()
				return
			}
			if err := downstream.WriteMessage(mt, data); err != nil {
				pumpErr <- err
				closeBoth()
				return
			}
		}
	}()

	first := <-pumpErr
	closeBoth()
	// Drain the second error so the goroutine can exit cleanly.
	select {
	case <-pumpErr:
	case <-time.After(time.Second):
	}

	reason := "client_close"
	if !isExpectedCloseErr(first) {
		reason = "upstream_error"
	}
	h.logger.Info("console proxy: session closed",
		"box", boxID,
		"reason", reason,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
}

// isExpectedCloseErr distinguishes "client/agent hung up cleanly"
// from "something went wrong." Used only for the proxy's
// info-level log line so support can spot anomalies; the audit
// record on the agent side has the authoritative reason.
func isExpectedCloseErr(err error) bool {
	if err == nil {
		return true
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return true
	}
	var ce *websocket.CloseError
	if errors.As(err, &ce) {
		return true
	}
	if strings.Contains(err.Error(), "use of closed network connection") {
		return true
	}
	return false
}
