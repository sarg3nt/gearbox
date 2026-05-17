// Package console implements the remote-console surface — a token-gated
// WebSocket endpoint that lets the dashboard open an interactive shell on
// the box the agent runs on. See [#89] for the full design.
//
// Phase 1a (this file): token exchange + WebSocket echo. No PTY yet; that
// lands in Phase 1b ([#119] or successor).
package console

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// tokenLength is the byte length of console session tokens. 32 bytes →
// 64 hex chars, matches the wstoken pattern used for the events channel.
const tokenLength = 32

// tokenExpiry is how long an unredeemed token is valid. Short by design —
// the dashboard exchanges the API key for a token and immediately opens
// the WebSocket, so 60s is comfortable; anything longer just widens the
// replay window.
const tokenExpiry = 60 * time.Second

// token holds a single in-flight console token.
type token struct {
	value     string
	expiresAt time.Time
}

// TokenManager mints and consumes short-lived single-use tokens for
// console WebSocket upgrades.
//
// Parallel to api.WSTokenManager (events channel). Kept in a separate
// type — and a separate map — so the two namespaces can't leak into each
// other: a token minted for /events cannot be replayed against /console,
// and vice versa. If a third such channel ever appears, extract the
// common code to a shared wstoken package; two callers don't justify the
// refactor yet.
type TokenManager struct {
	mu          sync.RWMutex
	tokens      map[string]token
	stopCleanup chan struct{}
}

// NewTokenManager constructs a TokenManager and starts its background
// cleanup goroutine. Call Close to stop it.
func NewTokenManager() *TokenManager {
	mgr := &TokenManager{
		tokens:      make(map[string]token),
		stopCleanup: make(chan struct{}),
	}
	go mgr.cleanupLoop()
	return mgr
}

// Close stops the cleanup goroutine. Safe to call once.
func (m *TokenManager) Close() {
	close(m.stopCleanup)
}

// Create mints a fresh token with full TTL and returns its hex value.
func (m *TokenManager) Create() (string, error) {
	b := make([]byte, tokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	v := hex.EncodeToString(b)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[v] = token{value: v, expiresAt: time.Now().Add(tokenExpiry)}
	return v, nil
}

// Validate consumes the token if it exists and is unexpired. Returns
// true exactly once per minted token. Expired tokens are still removed
// (so a slow attacker can't keep them alive past their TTL by failing to
// redeem them in time) but never validate.
func (m *TokenManager) Validate(v string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[v]
	if !ok {
		return false
	}
	// Delete first to make replay impossible even if a goroutine races
	// the post-delete check below.
	delete(m.tokens, v)
	return !time.Now().After(t.expiresAt)
}

// cleanupLoop sweeps expired tokens every 30s. Cheap — only runs when
// the manager is alive, exits on Close.
func (m *TokenManager) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			for v, t := range m.tokens {
				if now.After(t.expiresAt) {
					delete(m.tokens, v)
				}
			}
			m.mu.Unlock()
		case <-m.stopCleanup:
			return
		}
	}
}

// TokenResponse is the body returned by POST /api/v1/console/token.
type TokenResponse struct {
	Token     string `json:"token" example:"a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456"`
	ExpiresIn int    `json:"expires_in" example:"60"`
}

// HandleTokenExchange handles POST /api/v1/console/token. Caller must be
// behind APIKeyAuth — this handler trusts that auth has already happened.
//
//	@Summary		Exchange API key for console WebSocket token
//	@Description	Exchanges a valid API key (Bearer auth) for a 60-second single-use token. Use the returned token in the `?token=` query parameter when opening the WebSocket at /api/v1/console/ws.
//	@Tags			Console
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	TokenResponse	"Console WebSocket token"
//	@Failure		401	{string}	string			"Unauthorized"
//	@Failure		405	{string}	string			"Method not allowed"
//	@Failure		500	{string}	string			"Failed to generate token"
//	@Router			/api/v1/console/token [post]
func (m *TokenManager) HandleTokenExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	t, err := m.Create()
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TokenResponse{
		Token:     t,
		ExpiresIn: int(tokenExpiry.Seconds()),
	})
}
