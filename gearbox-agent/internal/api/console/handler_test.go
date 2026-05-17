package console

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// captureBus is a test double for events.Bus used by audit.go. The real
// bus's Publish has a non-blocking semantic; the capture keeps an
// ordered list so tests can assert on session-start / session-end pairs.
type captureBus struct {
	mu     sync.Mutex
	events []events.Event
}

func (c *captureBus) Publish(e events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *captureBus) snapshot() []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]events.Event, len(c.events))
	copy(out, c.events)
	return out
}

func newTestHandler() *Handler {
	return &Handler{
		Tokens:        NewTokenManager(),
		Audit:         nil, // auth/echo tests don't care; audit-specific tests inject explicitly
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		IdleTimeout:   2 * time.Second,
		MaxFrameBytes: 64 * 1024,
	}
}

func TestHandleWS_RejectsMissingToken(t *testing.T) {
	// No token query param → 401, no upgrade.
	h := newTestHandler()
	defer h.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/ws", nil)
	rr := httptest.NewRecorder()
	h.HandleWS(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestHandleWS_RejectsUnknownToken(t *testing.T) {
	h := newTestHandler()
	defer h.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/ws?token=garbage", nil)
	rr := httptest.NewRecorder()
	h.HandleWS(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestHandleWS_RejectsReplayedToken(t *testing.T) {
	// A token validated by an earlier call must not work a second
	// time, even on a different connection.
	h := newTestHandler()
	defer h.Close()

	tok, err := h.Tokens.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// First validation consumes the token.
	if !h.Tokens.Validate(tok) {
		t.Fatal("first Validate = false")
	}
	// Second attempt against the handler must 401.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/console/ws?token="+tok, nil)
	rr := httptest.NewRecorder()
	h.HandleWS(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestHandleWS_EchoRoundTripAndAudit(t *testing.T) {
	// Full happy path: stand up an httptest server, mint a token,
	// open a WS, send a data frame, expect the same payload echoed
	// back. Then close and confirm both audit events fired with
	// matching session_id and a non-zero byte count.
	bus := &captureBus{}
	h := &Handler{
		Tokens:        NewTokenManager(),
		Audit:         bus,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		IdleTimeout:   2 * time.Second,
		MaxFrameBytes: 64 * 1024,
	}
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	tok, err := h.Tokens.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	payload := []byte("hello world\n")
	out, _ := json.Marshal(Frame{Type: FrameTypeData, Data: base64.StdEncoding.EncodeToString(payload)})
	if err := conn.WriteMessage(websocket.TextMessage, out); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var got Frame
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Type != FrameTypeData {
		t.Errorf("echoed type = %q, want data", got.Type)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != string(payload) {
		t.Errorf("echo payload = %q, want %q", decoded, payload)
	}

	_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = conn.Close()

	// The session-end audit fires after the server-side loop returns.
	// Poll briefly rather than racing.
	deadline := time.Now().Add(2 * time.Second)
	var snapshot []events.Event
	for time.Now().Before(deadline) {
		snapshot = bus.snapshot()
		if len(snapshot) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(snapshot) < 2 {
		t.Fatalf("expected 2 audit events, got %d: %+v", len(snapshot), snapshot)
	}
	if snapshot[0].Type != events.EventConsoleSessionStart {
		t.Errorf("event[0].Type = %q, want %q", snapshot[0].Type, events.EventConsoleSessionStart)
	}
	if snapshot[1].Type != events.EventConsoleSessionEnd {
		t.Errorf("event[1].Type = %q, want %q", snapshot[1].Type, events.EventConsoleSessionEnd)
	}
	startID, _ := snapshot[0].Data["session_id"].(string)
	endID, _ := snapshot[1].Data["session_id"].(string)
	if startID == "" || startID != endID {
		t.Errorf("session_id mismatch: start=%q end=%q", startID, endID)
	}
	if bin, _ := snapshot[1].Data["bytes_in"].(int64); bin != int64(len(payload)) {
		t.Errorf("bytes_in = %v, want %d", snapshot[1].Data["bytes_in"], len(payload))
	}
	if bout, _ := snapshot[1].Data["bytes_out"].(int64); bout != int64(len(payload)) {
		t.Errorf("bytes_out = %v, want %d", snapshot[1].Data["bytes_out"], len(payload))
	}
}

func TestHandleWS_PingFrameGetsPong(t *testing.T) {
	// Ping/pong is the application-layer keep-alive (separate from
	// the WS protocol ping). Useful for the dashboard to confirm the
	// session is still wired up to the agent end-to-end.
	h := newTestHandler()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	tok, _ := h.Tokens.Create()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	out, _ := json.Marshal(Frame{Type: FrameTypePing})
	if err := conn.WriteMessage(websocket.TextMessage, out); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var got Frame
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Type != FrameTypePong {
		t.Errorf("type = %q, want pong", got.Type)
	}
}

func TestHandleWS_MalformedFrameClosesConnectionWithError(t *testing.T) {
	// Garbage in → ErrCodeProtocolViolation frame, then close. A
	// strict parser is the cheap defense against half-deployed
	// clients sending the wrong wire shape.
	h := newTestHandler()
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()

	tok, _ := h.Tokens.Create()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Not valid JSON.
	if err := conn.WriteMessage(websocket.TextMessage, []byte("{not-json")); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var got Frame
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Type != FrameTypeErr {
		t.Errorf("type = %q, want err", got.Type)
	}
	if got.Reason != ErrCodeProtocolViolation {
		t.Errorf("reason = %q, want %q", got.Reason, ErrCodeProtocolViolation)
	}
}
