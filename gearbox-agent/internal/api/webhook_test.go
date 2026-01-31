package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockSyncTrigger implements SyncTrigger for testing.
type mockSyncTrigger struct {
	triggered bool
}

func (m *mockSyncTrigger) TriggerSync() {
	m.triggered = true
}

func TestWebhookHandler_VerifySignature(t *testing.T) {
	secret := "test-secret-key"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWebhookHandler(secret, nil, "https://example.com/webhook", logger)

	tests := []struct {
		name      string
		body      []byte
		signature string
		want      bool
	}{
		{
			name:      "valid signature",
			body:      []byte(`{"test": "data"}`),
			signature: generateSignature([]byte(`{"test": "data"}`), secret),
			want:      true,
		},
		{
			name:      "invalid signature",
			body:      []byte(`{"test": "data"}`),
			signature: "sha256=invalidsignature",
			want:      false,
		},
		{
			name:      "missing sha256 prefix",
			body:      []byte(`{"test": "data"}`),
			signature: "invalidsignature",
			want:      false,
		},
		{
			name:      "wrong body",
			body:      []byte(`{"test": "different"}`),
			signature: generateSignature([]byte(`{"test": "data"}`), secret),
			want:      false,
		},
		{
			name:      "empty signature",
			body:      []byte(`{"test": "data"}`),
			signature: "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.verifySignature(tt.body, tt.signature)
			if got != tt.want {
				t.Errorf("verifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebhookHandler_HandleWebhook_PushEvent(t *testing.T) {
	secret := "test-secret"
	trigger := &mockSyncTrigger{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWebhookHandler(secret, trigger, "https://example.com/webhook", logger)

	payload := GitHubPushPayload{
		Ref: "refs/heads/main",
		Repository: struct {
			FullName string `json:"full_name"`
		}{FullName: "user/repo"},
		Pusher: struct {
			Name string `json:"name"`
		}{Name: "testuser"},
		HeadCommit: struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}{ID: "abc1234567890", Message: "Test commit"},
	}

	body, _ := json.Marshal(payload)
	signature := generateSignature(body, secret)

	req := httptest.NewRequest("POST", "/api/v1/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "push")

	rr := httptest.NewRecorder()
	handler.HandleWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("HandleWebhook() status = %v, want %v", rr.Code, http.StatusOK)
	}

	if !trigger.triggered {
		t.Error("HandleWebhook() did not trigger sync")
	}

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "accepted" {
		t.Errorf("HandleWebhook() response status = %v, want 'accepted'", resp["status"])
	}
}

func TestWebhookHandler_HandleWebhook_PingEvent(t *testing.T) {
	secret := "test-secret"
	trigger := &mockSyncTrigger{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWebhookHandler(secret, trigger, "https://example.com/webhook", logger)

	body := []byte(`{"zen": "Keep it logically awesome."}`)
	signature := generateSignature(body, secret)

	req := httptest.NewRequest("POST", "/api/v1/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "ping")

	rr := httptest.NewRecorder()
	handler.HandleWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("HandleWebhook() status = %v, want %v", rr.Code, http.StatusOK)
	}

	if trigger.triggered {
		t.Error("HandleWebhook() should not trigger sync for ping event")
	}

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "pong" {
		t.Errorf("HandleWebhook() response status = %v, want 'pong'", resp["status"])
	}
}

func TestWebhookHandler_HandleWebhook_MissingSignature(t *testing.T) {
	secret := "test-secret"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWebhookHandler(secret, nil, "https://example.com/webhook", logger)

	body := []byte(`{"test": "data"}`)

	req := httptest.NewRequest("POST", "/api/v1/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	// No signature header

	rr := httptest.NewRecorder()
	handler.HandleWebhook(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("HandleWebhook() status = %v, want %v", rr.Code, http.StatusUnauthorized)
	}
}

func TestWebhookHandler_HandleWebhook_InvalidSignature(t *testing.T) {
	secret := "test-secret"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWebhookHandler(secret, nil, "https://example.com/webhook", logger)

	body := []byte(`{"test": "data"}`)

	req := httptest.NewRequest("POST", "/api/v1/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=wrong")
	req.Header.Set("X-GitHub-Event", "push")

	rr := httptest.NewRecorder()
	handler.HandleWebhook(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("HandleWebhook() status = %v, want %v", rr.Code, http.StatusUnauthorized)
	}
}

func TestWebhookHandler_HandleWebhook_IgnoredEvent(t *testing.T) {
	secret := "test-secret"
	trigger := &mockSyncTrigger{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWebhookHandler(secret, trigger, "https://example.com/webhook", logger)

	body := []byte(`{"action": "opened"}`)
	signature := generateSignature(body, secret)

	req := httptest.NewRequest("POST", "/api/v1/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "issues") // Not a push event

	rr := httptest.NewRecorder()
	handler.HandleWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("HandleWebhook() status = %v, want %v", rr.Code, http.StatusOK)
	}

	if trigger.triggered {
		t.Error("HandleWebhook() should not trigger sync for non-push events")
	}

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "ignored" {
		t.Errorf("HandleWebhook() response status = %v, want 'ignored'", resp["status"])
	}
}

func TestWebhookHandler_HandleWebhookInfo(t *testing.T) {
	secret := "test-secret"
	webhookURL := "https://example.com:8405/api/v1/webhook/github"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewWebhookHandler(secret, nil, webhookURL, logger)

	req := httptest.NewRequest("GET", "/api/v1/webhook/info", nil)
	rr := httptest.NewRecorder()

	handler.HandleWebhookInfo(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("HandleWebhookInfo() status = %v, want %v", rr.Code, http.StatusOK)
	}

	var resp WebhookInfoResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if !resp.Enabled {
		t.Error("HandleWebhookInfo() enabled = false, want true")
	}

	if resp.WebhookURL != webhookURL {
		t.Errorf("HandleWebhookInfo() URL = %v, want %v", resp.WebhookURL, webhookURL)
	}
}

// generateSignature creates a GitHub-style HMAC-SHA256 signature.
func generateSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
