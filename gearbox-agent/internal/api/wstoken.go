package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const (
	// wsTokenLength is the length of WebSocket tokens in bytes.
	wsTokenLength = 32
	// wsTokenExpiry is how long a WebSocket token is valid.
	wsTokenExpiry = 60 * time.Second
)

// wsToken represents a short-lived WebSocket authentication token.
type wsToken struct {
	token     string
	expiresAt time.Time
}

// WSTokenManager manages short-lived tokens for WebSocket authentication.
type WSTokenManager struct {
	mu          sync.RWMutex
	tokens      map[string]wsToken
	stopCleanup chan struct{}
}

// NewWSTokenManager creates a new WebSocket token manager.
func NewWSTokenManager() *WSTokenManager {
	mgr := &WSTokenManager{
		tokens:      make(map[string]wsToken),
		stopCleanup: make(chan struct{}),
	}
	// Start cleanup goroutine
	go mgr.cleanupLoop()
	return mgr
}

// Close stops the token manager's cleanup goroutine.
// Should be called when the token manager is no longer needed.
func (m *WSTokenManager) Close() {
	close(m.stopCleanup)
}

// CreateToken generates a new short-lived token.
func (m *WSTokenManager) CreateToken() (string, error) {
	bytes := make([]byte, wsTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.tokens[token] = wsToken{
		token:     token,
		expiresAt: time.Now().Add(wsTokenExpiry),
	}

	return token, nil
}

// ValidateToken checks if a token is valid and not expired.
// A valid token is consumed (single use).
func (m *WSTokenManager) ValidateToken(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, exists := m.tokens[token]
	if !exists {
		return false
	}

	// Remove token (single use) - delete before checking to prevent replay
	delete(m.tokens, token)

	// Check expiry after deletion to ensure token can't be reused
	if time.Now().After(t.expiresAt) {
		return false
	}

	// Token was found in map and is not expired - it's valid
	// Note: constant-time comparison was removed as the map lookup already
	// determined equality. The token parameter IS t.token since we used
	// it as the map key.
	return true
}

// cleanupLoop periodically removes expired tokens.
func (m *WSTokenManager) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			for token, t := range m.tokens {
				if now.After(t.expiresAt) {
					delete(m.tokens, token)
				}
			}
			m.mu.Unlock()
		case <-m.stopCleanup:
			return
		}
	}
}

// WSTokenResponse is the response from the token exchange endpoint.
type WSTokenResponse struct {
	Token     string `json:"token" example:"a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456"`
	ExpiresIn int    `json:"expires_in" example:"60"` // Seconds until expiry
}

// HandleWSTokenExchange handles POST /api/v1/events/token.
//
//	@Summary		Exchange API key for WebSocket token
//	@Description	Exchanges an API key for a short-lived (60 second) single-use token for WebSocket authentication. Use the returned token in the WebSocket connection query parameter.
//	@Tags			WebSocket
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	WSTokenResponse	"WebSocket token"
//	@Failure		401	{string}	string			"Unauthorized"
//	@Failure		405	{string}	string			"Method not allowed"
//	@Failure		500	{string}	string			"Failed to generate token"
//	@Router			/api/v1/events/token [post]
func (m *WSTokenManager) HandleWSTokenExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token, err := m.CreateToken()
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	resp := WSTokenResponse{
		Token:     token,
		ExpiresIn: int(wsTokenExpiry.Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
