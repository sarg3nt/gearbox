package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

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
	// SECURITY FIX: Validate WebSocket origin to prevent Cross-Site WebSocket Hijacking (CSWSH)
	// Environment variable AGENT_ALLOWED_ORIGINS can specify allowed origins (comma-separated)
	// If not set, defaults to same-origin only
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")

		// If no Origin header, allow (same-origin requests from curl/Go clients)
		if origin == "" {
			return true
		}

		// Check environment variable for allowed origins
		allowedOriginsEnv := os.Getenv("AGENT_ALLOWED_ORIGINS")
		if allowedOriginsEnv != "" {
			allowedOrigins := strings.Split(allowedOriginsEnv, ",")
			for _, allowed := range allowedOrigins {
				allowed = strings.TrimSpace(allowed)
				if allowed == "*" {
					// Wildcard - allow all origins (not recommended for production)
					return true
				}
				if origin == allowed {
					return true
				}
			}
			// Origin not in allowed list
			return false
		}

		// Default: same-origin only
		// Compare origin with the request host
		originHost := strings.TrimPrefix(origin, "https://")
		originHost = strings.TrimPrefix(originHost, "http://")
		originHost = strings.Split(originHost, "/")[0] // Remove path if present

		requestHost := r.Host

		return originHost == requestHost
	},
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
	defer conn.Close()

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
