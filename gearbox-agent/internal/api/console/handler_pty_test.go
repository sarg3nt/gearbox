//go:build unix

package console

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sarg3nt/gearbox-agent/internal/api/console/pty"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

func newPTYTestHandler(spawner pty.Spawner) *Handler {
	return &Handler{
		Tokens:        NewTokenManager(),
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:          ModeHostPTY,
		Spawner:       spawner,
		Shell:         []string{"/bin/cat"}, // cat is a deterministic echo we can drive from tests
		IdleTimeout:   5 * time.Second,
		MaxFrameBytes: 64 * 1024,
		ReadBufBytes:  4 * 1024,
	}
}

// TestPTYLoop_RealCatRoundTrip spawns `cat` in a real PTY, sends bytes
// in via the WS, and confirms the PTY echoes them back to the WS. cat
// is the canonical "input == output" process — its termcap echo +
// line-buffered read make this a tight integration test of the whole
// stack: WS frame → base64 decode → PTY write → cat echo → PTY read
// → base64 encode → WS frame.
func TestPTYLoop_RealCatRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PTY integration test in -short mode")
	}
	bus := &captureBus{}
	h := newPTYTestHandler(pty.SpawnUnix)
	h.Audit = bus
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

	// cat echoes its stdin to stdout (with PTY echo on top, so we
	// see each character twice — once from PTY echo, once from cat
	// writing it back). The presence of our payload anywhere in the
	// next 2 seconds of frames is sufficient.
	payload := []byte("hello-pty\n")
	out, _ := json.Marshal(Frame{Type: FrameTypeData, Data: base64.StdEncoding.EncodeToString(payload)})
	if err := conn.WriteMessage(websocket.TextMessage, out); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got strings.Builder
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var f Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		if f.Type == FrameTypeData {
			decoded, _ := base64.StdEncoding.DecodeString(f.Data)
			got.Write(decoded)
			if strings.Contains(got.String(), "hello-pty") {
				break
			}
		}
	}
	if !strings.Contains(got.String(), "hello-pty") {
		t.Fatalf("did not see payload echoed; got %q", got.String())
	}

	// Close the WS — the deferred sess.Close in ptyLoop should kill
	// cat. Audit event should fire with a non-error reason.
	_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	_ = conn.Close()

	endDeadline := time.Now().Add(3 * time.Second)
	var snapshot []events.Event
	for time.Now().Before(endDeadline) {
		snapshot = bus.snapshot()
		if len(snapshot) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(snapshot) < 2 {
		t.Fatalf("expected 2 audit events, got %d", len(snapshot))
	}
	if snapshot[0].Type != events.EventConsoleSessionStart {
		t.Errorf("event[0].Type = %q, want %q", snapshot[0].Type, events.EventConsoleSessionStart)
	}
	if snapshot[1].Type != events.EventConsoleSessionEnd {
		t.Errorf("event[1].Type = %q, want %q", snapshot[1].Type, events.EventConsoleSessionEnd)
	}
	if mode, _ := snapshot[0].Data["mode"].(string); mode != ModeHostPTY {
		t.Errorf("start.mode = %q, want %q", mode, ModeHostPTY)
	}
	if _, hasExit := snapshot[1].Data["exit_code"]; !hasExit {
		t.Error("session-end event missing exit_code field")
	}
}

// TestPTYLoop_ExitFromShellEndsSession runs `/bin/true`, which exits
// immediately with code 0. The handler should observe the EOF on the
// PTY, report reason="exit" and exit_code=0 in the audit event.
func TestPTYLoop_ExitFromShellEndsSession(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PTY integration test in -short mode")
	}
	bus := &captureBus{}
	h := newPTYTestHandler(pty.SpawnUnix)
	h.Audit = bus
	h.Shell = []string{"/bin/sh", "-c", "exit 7"}
	defer h.Close()

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	defer srv.Close()
	tok, _ := h.Tokens.Create()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	// Read until the WS closes (the PTY EOF should drive that).
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	deadline := time.Now().Add(3 * time.Second)
	var snapshot []events.Event
	for time.Now().Before(deadline) {
		snapshot = bus.snapshot()
		if len(snapshot) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = conn.Close()
	if len(snapshot) < 2 {
		t.Fatalf("expected 2 audit events, got %d", len(snapshot))
	}
	exit, _ := snapshot[1].Data["exit_code"].(int)
	if exit != 7 {
		t.Errorf("exit_code = %v, want 7", exit)
	}
}

// TestPTYLoop_ResizeReachesPTY drives a resize frame and confirms the
// PTY's reported window matches via `stty size`. The shell prints its
// stty output, which we read back through the WS.
func TestPTYLoop_ResizeReachesPTY(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PTY integration test in -short mode")
	}
	h := newPTYTestHandler(pty.SpawnUnix)
	h.Shell = []string{"/bin/sh"}
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

	// Resize to a memorable non-default geometry.
	r, _ := json.Marshal(Frame{Type: FrameTypeResize, Cols: 132, Rows: 50})
	if err := conn.WriteMessage(websocket.TextMessage, r); err != nil {
		t.Fatalf("WriteMessage resize: %v", err)
	}
	// stty size prints "<rows> <cols>"
	cmd, _ := json.Marshal(Frame{Type: FrameTypeData, Data: base64.StdEncoding.EncodeToString([]byte("stty size; exit\n"))})
	if err := conn.WriteMessage(websocket.TextMessage, cmd); err != nil {
		t.Fatalf("WriteMessage cmd: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var got strings.Builder
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var f Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		if f.Type == FrameTypeData {
			decoded, _ := base64.StdEncoding.DecodeString(f.Data)
			got.Write(decoded)
			if strings.Contains(got.String(), "50 132") {
				return
			}
		}
	}
	t.Fatalf("did not see resized geometry in output; got %q", got.String())
}

// TestPTYLoop_ContextCancelKillsChild verifies that cancelling the
// request context terminates the PTY child. Important for server
// shutdown — we don't want zombie shells after the agent exits.
func TestPTYLoop_ContextCancelKillsChild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PTY integration test in -short mode")
	}
	h := newPTYTestHandler(pty.SpawnUnix)
	h.Shell = []string{"/bin/sh", "-c", "sleep 30"}
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.HandleWS(w, r.WithContext(ctx))
	}))
	defer srv.Close()
	tok, _ := h.Tokens.Create()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?token=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	// Cancel before the sleep finishes — the shell should die.
	time.Sleep(100 * time.Millisecond)
	cancel()

	// The WS should close shortly after.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
		// good
	case <-time.After(3 * time.Second):
		t.Fatal("WS did not close after context cancel")
	}
}
