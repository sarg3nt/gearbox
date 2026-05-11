package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// canonicalOrigin parses an Origin-header value (or an entry from
// AGENT_ALLOWED_ORIGINS) and returns a comparable canonical form:
// "<scheme>://<lowercased-host>[:<port>]". Default ports (80 for http,
// 443 for https) are stripped so the caller can write "https://example.com"
// in the allowlist regardless of whether browsers send the explicit :443.
// Returns false if the input doesn't parse as a scheme://host URL.
//
// Without this, the previous byte-equal comparison treated
// "https://Example.Com" and "https://example.com" as different origins,
// or "https://example.com:443" and "https://example.com" as different.
// See 2026-05 security audit, P1-5.
func canonicalOrigin(s string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u == nil {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	// Strip default ports so the allowlist needn't second-guess what a
	// browser will or won't send.
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port == "" {
		return scheme + "://" + host, true
	}
	return scheme + "://" + host + ":" + port, true
}

// canonicalHost normalizes an http.Request.Host value (host[:port] with no
// scheme) for comparison against canonicalOrigin output. The scheme is
// derived from r.TLS at the call site, since the Host header itself carries
// no scheme. Same default-port stripping rules as canonicalOrigin.
func canonicalHost(scheme, hostPort string) string {
	scheme = strings.ToLower(scheme)
	host := strings.ToLower(hostPort)
	if i := strings.Index(host, ":"); i >= 0 {
		port := host[i+1:]
		host = host[:i]
		if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
			return scheme + "://" + host
		}
		return scheme + "://" + host + ":" + port
	}
	return scheme + "://" + host
}

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Validate WebSocket origin to prevent Cross-Site WebSocket Hijacking.
	// AGENT_ALLOWED_ORIGINS is a comma-separated list of allowed origins.
	// If unset, the default is same-origin only.
	//
	// Origins and hosts are compared in canonical form (lowercased host,
	// default ports stripped) so an allowlist entry of "https://example.com"
	// matches an Origin header of "https://Example.Com:443". See P1-5 in
	// the 2026-05 security audit.
	CheckOrigin: checkOrigin,
}

// checkOrigin is the gorilla/websocket Upgrader.CheckOrigin function for the
// agent. Factored out so it can be unit tested without standing up a real
// WebSocket connection.
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")

	// No Origin header (curl, Go clients, etc.) — accept, since CSWSH only
	// applies in a browser context where the header is mandatory.
	if origin == "" {
		return true
	}

	canonOrigin, ok := canonicalOrigin(origin)
	if !ok {
		return false
	}

	allowedOriginsEnv := os.Getenv("AGENT_ALLOWED_ORIGINS")
	if allowedOriginsEnv != "" {
		for _, allowed := range strings.Split(allowedOriginsEnv, ",") {
			allowed = strings.TrimSpace(allowed)
			if allowed == "" {
				continue
			}
			if allowed == "*" {
				// Wildcard — allow all origins (not recommended for production)
				return true
			}
			if canon, ok := canonicalOrigin(allowed); ok && canon == canonOrigin {
				return true
			}
		}
		return false
	}

	// Default: same-origin only. Derive scheme from TLS state since the Host
	// header carries none.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Allow X-Forwarded-Proto when set by a trusted reverse proxy. We don't
	// honor X-Forwarded-Host because the canonical-host comparison treats
	// r.Host as authoritative for the agent's own listener.
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		if proto == "http" || proto == "https" {
			scheme = proto
		}
	}
	return canonOrigin == canonicalHost(scheme, r.Host)
}

// WSHandler handles WebSocket connections for real-time events.
type WSHandler struct {
	eventBus *events.Bus
	logger   *slog.Logger
}

// NewWSHandler creates a new WebSocket handler.
func NewWSHandler(eventBus *events.Bus, logger *slog.Logger) *WSHandler {
	return &WSHandler{
		eventBus: eventBus,
		logger:   logger,
	}
}

// HandleEvents handles WebSocket connections for the /api/v1/events endpoint.
//
//	@Summary		WebSocket events stream
//	@Description	Upgrades to WebSocket connection for real-time event streaming. First obtain a token via POST /api/v1/events/token, then connect with ?token=<token>. Events: sync.started, sync.completed, sync.failed, config.changed, webhook.received.
//	@Tags			WebSocket
//	@Produce		json
//	@Param			token	query	string	true	"Short-lived WebSocket token from /api/v1/events/token"
//	@Success		101		"Switching Protocols - WebSocket connection established"
//	@Failure		401		{string}	string	"Unauthorized"
//	@Router			/api/v1/events [get]
func (h *WSHandler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("WebSocket upgrade failed", "error", err)
		return
	}
	defer func() { _ = conn.Close() }()

	// Subscribe to events
	sub := h.eventBus.Subscribe()
	if sub == nil {
		h.logger.Warn("WebSocket: Event bus closed, rejecting connection")
		return
	}
	defer h.eventBus.Unsubscribe(sub)

	h.logger.Info("WebSocket: Client connected", "remote_addr", r.RemoteAddr)

	// Start read pump (handles pongs and client disconnect)
	done := make(chan struct{})
	go h.readPump(conn, done)

	// Start write pump (sends events to client)
	h.writePump(conn, sub, done)

	h.logger.Info("WebSocket: Client disconnected", "remote_addr", r.RemoteAddr)
}

// readPump reads messages from the WebSocket connection.
// It handles pong messages and detects client disconnection.
func (h *WSHandler) readPump(conn *websocket.Conn, done chan struct{}) {
	defer close(done)

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Error("WebSocket read error", "error", err)
			}
			return
		}
		// We don't expect any messages from clients, but we need to read to detect disconnection
	}
}

// writePump sends events to the WebSocket connection.
func (h *WSHandler) writePump(conn *websocket.Conn, sub events.Subscriber, done chan struct{}) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-sub:
			if !ok {
				// Subscriber channel closed
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			conn.SetWriteDeadline(time.Now().Add(writeWait))

			data, err := event.JSON()
			if err != nil {
				h.logger.Error("WebSocket: Failed to marshal event", "error", err)
				continue
			}

			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				h.logger.Error("WebSocket write error", "error", err)
				return
			}

		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-done:
			return
		}
	}
}

// WSInfoResponse contains information about the WebSocket endpoint.
type WSInfoResponse struct {
	Enabled     bool     `json:"enabled" example:"true"`
	Endpoint    string   `json:"endpoint" example:"/api/v1/events"`
	Subscribers int      `json:"subscribers" example:"2"`
	EventTypes  []string `json:"event_types" example:"sync.started,sync.completed,sync.failed,config.changed,webhook.received"`
}

// HandleWSInfo returns information about the WebSocket endpoint.
//
//	@Summary		WebSocket info
//	@Description	Returns information about the WebSocket endpoint including available event types and current subscriber count.
//	@Tags			WebSocket
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	WSInfoResponse	"WebSocket endpoint info"
//	@Failure		401	{string}	string			"Unauthorized"
//	@Router			/api/v1/events/info [get]
func (h *WSHandler) HandleWSInfo(w http.ResponseWriter, r *http.Request) {
	info := WSInfoResponse{
		Enabled:     true,
		Endpoint:    "/api/v1/events",
		Subscribers: h.eventBus.SubscriberCount(),
		EventTypes: []string{
			string(events.EventSyncStarted),
			string(events.EventSyncCompleted),
			string(events.EventSyncFailed),
			string(events.EventConfigChanged),
			string(events.EventWebhookReceived),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}
