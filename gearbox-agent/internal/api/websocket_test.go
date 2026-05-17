package api

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

func TestWSHandler_HandleWSInfo(t *testing.T) {
	eventBus := events.NewBus()
	defer eventBus.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWSHandler(eventBus, logger)

	req := httptest.NewRequest("GET", "/api/v1/events/info", nil)
	rr := httptest.NewRecorder()

	handler.HandleWSInfo(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("HandleWSInfo() status = %v, want %v", rr.Code, http.StatusOK)
	}

	var resp WSInfoResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !resp.Enabled {
		t.Error("HandleWSInfo() enabled = false, want true")
	}

	if resp.Endpoint != "/api/v1/events" {
		t.Errorf("HandleWSInfo() endpoint = %v, want /api/v1/events", resp.Endpoint)
	}

	if len(resp.EventTypes) == 0 {
		t.Error("HandleWSInfo() returned no event types")
	}

	// Check that expected event types are present
	expectedTypes := []string{
		string(events.EventSyncStarted),
		string(events.EventSyncCompleted),
		string(events.EventSyncFailed),
		string(events.EventConfigChanged),
	}
	for _, expected := range expectedTypes {
		found := false
		for _, et := range resp.EventTypes {
			if et == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("HandleWSInfo() missing event type %s", expected)
		}
	}
}

func TestWSHandler_HandleEvents(t *testing.T) {
	eventBus := events.NewBus()
	defer eventBus.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWSHandler(eventBus, logger)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(handler.HandleEvents))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect WebSocket client
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Wait for subscription to register on the server side
	time.Sleep(50 * time.Millisecond)

	// Publish an event
	testEvent := events.NewEvent(events.EventSyncStarted, map[string]interface{}{
		"test_key": "test_value",
	})
	eventBus.Publish(testEvent)

	// Read the event from WebSocket
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read WebSocket message: %v", err)
	}

	// Parse the event
	var receivedEvent events.Event
	if err := json.Unmarshal(message, &receivedEvent); err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	if receivedEvent.Type != events.EventSyncStarted {
		t.Errorf("Event type = %s, want %s", receivedEvent.Type, events.EventSyncStarted)
	}

	if receivedEvent.Data["test_key"] != "test_value" {
		t.Errorf("Event data = %v, want test_key:test_value", receivedEvent.Data)
	}
}

func TestWSHandler_MultipleEvents(t *testing.T) {
	eventBus := events.NewBus()
	defer eventBus.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWSHandler(eventBus, logger)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleEvents))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Wait for subscription to register on the server side
	time.Sleep(50 * time.Millisecond)

	// Publish multiple events
	eventTypes := []events.EventType{
		events.EventSyncStarted,
		events.EventSyncCompleted,
		events.EventConfigChanged,
	}

	for _, et := range eventTypes {
		eventBus.Publish(events.NewEvent(et, nil))
	}

	// Read all events
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for i, expectedType := range eventTypes {
		_, message, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("Failed to read event %d: %v", i, err)
		}

		var event events.Event
		if err := json.Unmarshal(message, &event); err != nil {
			t.Fatalf("Failed to unmarshal event %d: %v", i, err)
		}

		if event.Type != expectedType {
			t.Errorf("Event %d type = %s, want %s", i, event.Type, expectedType)
		}
	}
}

func TestWSHandler_SubscriberCount(t *testing.T) {
	eventBus := events.NewBus()
	defer eventBus.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWSHandler(eventBus, logger)

	server := httptest.NewServer(http.HandlerFunc(handler.HandleEvents))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Initial count should be 0
	if eventBus.SubscriberCount() != 0 {
		t.Errorf("Initial SubscriberCount = %d, want 0", eventBus.SubscriberCount())
	}

	// Connect a client
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}

	// Give time for connection to register
	time.Sleep(50 * time.Millisecond)

	if eventBus.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount after connect = %d, want 1", eventBus.SubscriberCount())
	}

	// Disconnect
	conn.Close()

	// Give time for disconnection to process
	time.Sleep(50 * time.Millisecond)

	if eventBus.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount after disconnect = %d, want 0", eventBus.SubscriberCount())
	}
}

// 2026-05 audit P1-5: WebSocket origin validation must be case-insensitive
// on the host portion (DNS standard) and aware of default-port equivalence
// (an Origin of "https://example.com:443" must match an allowlist entry of
// "https://example.com"). The pre-2026-05 implementation byte-compared
// strings after a naive trim, so case mismatches and port differences
// produced unexpected accept/reject decisions.
func TestCanonicalOrigin(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://example.com", "https://example.com", true},
		{"https://Example.Com", "https://example.com", true},
		{"https://example.com:443", "https://example.com", true},
		{"http://example.com:80", "http://example.com", true},
		{"http://example.com:8080", "http://example.com:8080", true},
		{"https://example.com:8443", "https://example.com:8443", true},
		{"  https://example.com  ", "https://example.com", true},

		// Rejected: anything that isn't a complete scheme://host URL.
		{"", "", false},
		{"example.com", "", false},
		{"https://", "", false},
		{"//example.com", "", false},
	}
	for _, tt := range tests {
		got, ok := canonicalOrigin(tt.in)
		if ok != tt.ok {
			t.Errorf("canonicalOrigin(%q) ok=%v, want %v", tt.in, ok, tt.ok)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("canonicalOrigin(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCheckOrigin_SameOriginDefault(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		host       string
		tls        bool
		fwdProto   string
		wantAccept bool
	}{
		{"no-origin-allowed", "", "agent.example.com:8405", true, "", true},
		{"matching-https", "https://agent.example.com:8405", "agent.example.com:8405", true, "", true},
		{"matching-https-case-insensitive", "https://Agent.Example.Com:8405", "agent.example.com:8405", true, "", true},
		{"matching-https-default-port", "https://agent.example.com:443", "agent.example.com", true, "", true},
		{"matching-https-no-port", "https://agent.example.com", "agent.example.com:443", true, "", true},
		{"matching-http", "http://agent.example.com:8080", "agent.example.com:8080", false, "", true},

		{"mismatched-host", "https://attacker.example.com", "agent.example.com:8405", true, "", false},
		{"mismatched-port", "https://agent.example.com:8406", "agent.example.com:8405", true, "", false},
		{"scheme-mismatch", "http://agent.example.com:8405", "agent.example.com:8405", true, "", false},
		{"malformed-origin", "not-a-url", "agent.example.com:8405", true, "", false},
		{"protocol-relative", "//agent.example.com", "agent.example.com:8405", true, "", false},

		{"xfwd-proto-https", "https://agent.example.com", "agent.example.com", false, "https", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// httptest.NewRequest auto-populates r.TLS when given an https://
			// URL, so build with http:// then optionally stub r.TLS to mirror
			// what an actual TLS-terminated server.go listener would set.
			r := httptest.NewRequest("GET", "http://agent.example.com/events", nil)
			r.TLS = nil
			r.Host = tt.host
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.fwdProto != "" {
				r.Header.Set("X-Forwarded-Proto", tt.fwdProto)
			}
			if tt.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if got := checkOrigin(r); got != tt.wantAccept {
				t.Errorf("checkOrigin(origin=%q host=%q tls=%v xfp=%q) = %v, want %v",
					tt.origin, tt.host, tt.tls, tt.fwdProto, got, tt.wantAccept)
			}
		})
	}
}

func TestCheckOrigin_AllowedOriginsEnv(t *testing.T) {
	tests := []struct {
		name       string
		envValue   string
		origin     string
		wantAccept bool
	}{
		{"exact-match", "https://dashboard.example.com", "https://dashboard.example.com", true},
		{"case-insensitive-match", "https://dashboard.example.com", "https://Dashboard.Example.Com", true},
		{"default-port-equivalence", "https://dashboard.example.com", "https://dashboard.example.com:443", true},
		{"port-mismatch-rejected", "https://dashboard.example.com", "https://dashboard.example.com:8443", false},
		{"second-in-list", "https://a.example.com, https://b.example.com", "https://b.example.com", true},
		{"not-in-list-rejected", "https://a.example.com", "https://attacker.example.com", false},
		{"wildcard-allows-all", "*", "https://attacker.example.com", true},
		{"empty-entries-skipped", ", ,https://dashboard.example.com,,", "https://dashboard.example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENT_ALLOWED_ORIGINS", tt.envValue)
			r := httptest.NewRequest("GET", "http://agent.example.com/events", nil)
			r.TLS = &tls.ConnectionState{}
			r.Header.Set("Origin", tt.origin)
			if got := checkOrigin(r); got != tt.wantAccept {
				t.Errorf("checkOrigin(env=%q origin=%q) = %v, want %v",
					tt.envValue, tt.origin, got, tt.wantAccept)
			}
		})
	}
}
