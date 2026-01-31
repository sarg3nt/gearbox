package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// DefaultReconnectDelay is the initial delay before reconnecting.
	DefaultReconnectDelay = 1 * time.Second

	// MaxReconnectDelay is the maximum delay between reconnection attempts.
	MaxReconnectDelay = 60 * time.Second

	// TokenRefreshBuffer is how long before expiry to refresh the token.
	TokenRefreshBuffer = 10 * time.Second

	// PingInterval is how often to send ping frames.
	PingInterval = 30 * time.Second

	// PongTimeout is how long to wait for a pong response.
	PongTimeout = 10 * time.Second
)

// EventHandler is a callback function that handles WebSocket events.
type EventHandler func(event WSEvent)

// ConnectionCallback is called when connection status changes.
type ConnectionCallback func(connected bool)

// WSClient manages a WebSocket connection to the HAProxy Agent.
type WSClient struct {
	client             *Client
	conn               *websocket.Conn
	handler            EventHandler
	connectionCallback ConnectionCallback
	logger             *slog.Logger
	mu                 sync.Mutex
	stopCh             chan struct{}
	doneCh             chan struct{}
	reconnectDelay     time.Duration
	connected          bool
}

// NewWSClient creates a new WebSocket client.
func NewWSClient(client *Client, handler EventHandler, logger *slog.Logger) *WSClient {
	if logger == nil {
		logger = slog.Default()
	}

	return &WSClient{
		client:         client,
		handler:        handler,
		logger:         logger,
		reconnectDelay: DefaultReconnectDelay,
	}
}

// SetConnectionCallback sets a callback to be called when connection status changes.
func (w *WSClient) SetConnectionCallback(cb ConnectionCallback) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.connectionCallback = cb
}

// Connect establishes a WebSocket connection and starts reading events.
// This method blocks until Stop is called or an unrecoverable error occurs.
func (w *WSClient) Connect(ctx context.Context) error {
	w.mu.Lock()
	if w.stopCh != nil {
		w.mu.Unlock()
		return fmt.Errorf("websocket client already running")
	}
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	w.mu.Unlock()

	defer close(w.doneCh)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.stopCh:
			return nil
		default:
			if err := w.connectAndRead(ctx); err != nil {
				w.logger.Error("websocket connection error", "error", err)
			}

			// Check if we should stop before reconnecting
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-w.stopCh:
				return nil
			case <-time.After(w.reconnectDelay):
				// Exponential backoff
				w.reconnectDelay *= 2
				if w.reconnectDelay > MaxReconnectDelay {
					w.reconnectDelay = MaxReconnectDelay
				}
			}
		}
	}
}

// connectAndRead establishes a connection and reads messages until disconnection.
func (w *WSClient) connectAndRead(ctx context.Context) error {
	// Get a fresh token
	tokenResp, err := w.client.GetWebSocketToken()
	if err != nil {
		return fmt.Errorf("failed to get websocket token: %w", err)
	}

	// Build WebSocket URL
	wsURL, err := w.buildWebSocketURL(tokenResp.Token)
	if err != nil {
		return fmt.Errorf("failed to build websocket URL: %w", err)
	}

	// Connect
	w.logger.Debug("connecting to websocket", "url", wsURL)

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.connected = true
	w.reconnectDelay = DefaultReconnectDelay // Reset on successful connection
	cb := w.connectionCallback
	w.mu.Unlock()

	w.logger.Info("websocket connected")

	// Notify connection callback
	if cb != nil {
		cb(true)
	}

	defer func() {
		w.mu.Lock()
		w.connected = false
		if w.conn != nil {
			w.conn.Close()
			w.conn = nil
		}
		cb := w.connectionCallback
		w.mu.Unlock()
		w.logger.Info("websocket disconnected")

		// Notify connection callback
		if cb != nil {
			cb(false)
		}
	}()

	// Set up ping/pong handlers
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(PongTimeout + PingInterval))
	})

	// Start ping goroutine
	pingDone := make(chan struct{})
	go w.pingLoop(ctx, conn, pingDone)
	defer close(pingDone)

	// Read messages
	return w.readLoop(ctx, conn)
}

// pingLoop sends periodic ping frames to keep the connection alive.
func (w *WSClient) pingLoop(ctx context.Context, conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			if w.conn == nil {
				w.mu.Unlock()
				return
			}
			err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			w.mu.Unlock()
			if err != nil {
				w.logger.Debug("failed to send ping", "error", err)
				return
			}
		}
	}
}

// readLoop reads messages from the WebSocket connection.
func (w *WSClient) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.stopCh:
			return nil
		default:
			// Set read deadline
			if err := conn.SetReadDeadline(time.Now().Add(PongTimeout + PingInterval)); err != nil {
				return fmt.Errorf("failed to set read deadline: %w", err)
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					return fmt.Errorf("websocket read error: %w", err)
				}
				return err
			}

			// Parse event
			var event WSEvent
			if err := json.Unmarshal(message, &event); err != nil {
				w.logger.Warn("failed to parse websocket event", "error", err, "message", string(message))
				continue
			}

			// Call handler
			if w.handler != nil {
				w.handler(event)
			}
		}
	}
}

// buildWebSocketURL constructs the WebSocket URL with the auth token.
func (w *WSClient) buildWebSocketURL(token string) (string, error) {
	// Parse the base URL
	baseURL := w.client.baseURL + "/api/v1/events"

	// Convert http(s) to ws(s)
	if strings.HasPrefix(baseURL, "https://") {
		baseURL = "wss://" + strings.TrimPrefix(baseURL, "https://")
	} else if strings.HasPrefix(baseURL, "http://") {
		baseURL = "ws://" + strings.TrimPrefix(baseURL, "http://")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	// Add token as query parameter
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// Stop gracefully closes the WebSocket connection.
func (w *WSClient) Stop() {
	w.mu.Lock()
	if w.stopCh == nil {
		w.mu.Unlock()
		return
	}
	close(w.stopCh)
	w.stopCh = nil
	w.mu.Unlock()

	// Wait for connection to close
	<-w.doneCh
}

// IsConnected returns whether the WebSocket is currently connected.
func (w *WSClient) IsConnected() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.connected
}

// Send sends a message over the WebSocket connection.
// Currently unused but available for future bidirectional communication.
func (w *WSClient) Send(data interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	return w.conn.WriteJSON(data)
}
