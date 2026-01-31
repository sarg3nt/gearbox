package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// SyncTrigger is an interface for triggering sync operations.
type SyncTrigger interface {
	TriggerSync()
}

// WebhookHandler handles GitHub webhook requests.
type WebhookHandler struct {
	secret      string
	syncTrigger SyncTrigger
	logger      *slog.Logger
	webhookURL  string
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(secret string, syncTrigger SyncTrigger, webhookURL string, logger *slog.Logger) *WebhookHandler {
	return &WebhookHandler{
		secret:      secret,
		syncTrigger: syncTrigger,
		logger:      logger,
		webhookURL:  webhookURL,
	}
}

// WebhookErrorResponse represents a structured error response.
type WebhookErrorResponse struct {
	Status  string `json:"status" example:"error"`
	Error   string `json:"error" example:"invalid_signature"`
	Message string `json:"message,omitempty" example:"Signature verification failed"`
}

// WebhookSuccessResponse represents a successful webhook response.
type WebhookSuccessResponse struct {
	Status  string `json:"status" example:"accepted"`
	Message string `json:"message,omitempty" example:"Sync triggered"`
}

// webhookError writes a structured JSON error response.
func (h *WebhookHandler) webhookError(w http.ResponseWriter, statusCode int, errMsg string, details string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(WebhookErrorResponse{
		Status:  "error",
		Error:   errMsg,
		Message: details,
	})
}

// GitHubPushPayload represents the relevant parts of a GitHub push webhook payload.
type GitHubPushPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Pusher struct {
		Name string `json:"name"`
	} `json:"pusher"`
	HeadCommit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"head_commit"`
}

// HandleWebhook handles POST /api/v1/webhook/github
//
//	@Summary		GitHub webhook
//	@Description	Receives GitHub push events and triggers a sync. Authenticated via HMAC-SHA256 signature in X-Hub-Signature-256 header.
//	@Tags			Webhook
//	@Accept			json
//	@Produce		json
//	@Param			X-Hub-Signature-256	header		string					true	"GitHub HMAC signature"
//	@Param			X-GitHub-Event		header		string					true	"GitHub event type"
//	@Param			body				body		GitHubPushPayload		true	"GitHub push payload"
//	@Success		200					{object}	WebhookSuccessResponse	"Webhook processed successfully"
//	@Failure		400					{object}	WebhookErrorResponse	"Bad request"
//	@Failure		401					{object}	WebhookErrorResponse	"Invalid or missing signature"
//	@Router			/api/v1/webhook/github [post]
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// Read the body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("WEBHOOK: Failed to read body", "error", err)
		h.webhookError(w, http.StatusBadRequest, "bad_request", "Failed to read request body")
		return
	}

	// Verify the signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		h.logger.Warn("WEBHOOK: Missing X-Hub-Signature-256 header", "remote_addr", r.RemoteAddr)
		h.webhookError(w, http.StatusUnauthorized, "missing_signature", "X-Hub-Signature-256 header required")
		return
	}

	if !h.verifySignature(body, signature) {
		h.logger.Warn("WEBHOOK: Invalid signature", "remote_addr", r.RemoteAddr)
		h.webhookError(w, http.StatusUnauthorized, "invalid_signature", "Signature verification failed")
		return
	}

	// Check the event type
	event := r.Header.Get("X-GitHub-Event")
	if event == "" {
		h.logger.Warn("WEBHOOK: Missing X-GitHub-Event header", "remote_addr", r.RemoteAddr)
		h.webhookError(w, http.StatusBadRequest, "missing_event", "X-GitHub-Event header required")
		return
	}

	// Handle ping event (sent when webhook is first configured)
	if event == "ping" {
		h.logger.Info("WEBHOOK: Received ping event", "remote_addr", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "pong"})
		return
	}

	// We only care about push events
	if event != "push" {
		h.logger.Info("WEBHOOK: Ignoring event", "event", event, "remote_addr", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ignored", "reason": "not a push event"})
		return
	}

	// Parse the payload
	var payload GitHubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logger.Error("WEBHOOK: Failed to parse payload", "error", err)
		h.webhookError(w, http.StatusBadRequest, "invalid_payload", "Failed to parse JSON payload")
		return
	}

	commitID := payload.HeadCommit.ID
	if len(commitID) > 7 {
		commitID = commitID[:7]
	}

	h.logger.Info("WEBHOOK: Received push",
		"repo", payload.Repository.FullName,
		"ref", payload.Ref,
		"pusher", payload.Pusher.Name,
		"commit", commitID,
	)

	// Trigger a sync
	if h.syncTrigger != nil {
		h.syncTrigger.TriggerSync()
		h.logger.Info("WEBHOOK: Triggered sync for push event")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Sync triggered",
	})
}

// verifySignature verifies the GitHub webhook signature.
func (h *WebhookHandler) verifySignature(body []byte, signature string) bool {
	// GitHub sends signature as "sha256=<hex>"
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	expectedSig := signature[7:] // Remove "sha256=" prefix

	// Calculate HMAC
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	actualSig := hex.EncodeToString(mac.Sum(nil))

	// Use constant-time comparison to prevent timing attacks
	return hmac.Equal([]byte(expectedSig), []byte(actualSig))
}

// WebhookInfoResponse contains webhook configuration info.
type WebhookInfoResponse struct {
	Enabled    bool   `json:"enabled" example:"true"`
	WebhookURL string `json:"webhook_url,omitempty" example:"https://proxy.example.com:8405/api/v1/webhook/github"`
	Message    string `json:"message,omitempty" example:"Configure this URL in GitHub repository settings"`
}

// HandleWebhookInfo handles GET /api/v1/webhook/info
//
//	@Summary		Webhook info
//	@Description	Returns webhook configuration information including the URL to configure in GitHub.
//	@Tags			Webhook
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	WebhookInfoResponse	"Webhook configuration info"
//	@Failure		401	{string}	string				"Unauthorized"
//	@Router			/api/v1/webhook/info [get]
func (h *WebhookHandler) HandleWebhookInfo(w http.ResponseWriter, r *http.Request) {
	resp := WebhookInfoResponse{
		Enabled:    true,
		WebhookURL: h.webhookURL,
		Message:    "Configure this URL in GitHub repository settings with content type 'application/json' and the webhook secret",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
