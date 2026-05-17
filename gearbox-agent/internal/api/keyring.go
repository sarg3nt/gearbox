package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/sarg3nt/gearbox-agent/internal/framework/crypto"
)

// KeyRingHandler exposes the agent's keyring metadata and rotation
// endpoints over the HTTP API. The mutation endpoints (install/use/
// remove) drive the controller-orchestrated three-phase rotation from
// issue #72; the metadata endpoint is read-only.
type KeyRingHandler struct {
	keyring *crypto.KeyRingPointer
	path    string
	logger  *slog.Logger

	// mu serializes mutation handlers — keyring writes are tmpfile+
	// rename atomic on disk, but two concurrent installs racing would
	// still let the second one's read-modify-write step lose the
	// first one's changes.
	mu sync.Mutex
}

// NewKeyRingHandler wires a handler around the agent's keyring pointer
// and the disk path the keyring is persisted to.
func NewKeyRingHandler(keyring *crypto.KeyRingPointer, path string, logger *slog.Logger) *KeyRingHandler {
	return &KeyRingHandler{keyring: keyring, path: path, logger: logger}
}

// RegisterRoutes mounts the keyring endpoints on r. The caller is
// responsible for putting r behind the API-key auth middleware.
func (h *KeyRingHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/system/keyring", h.handleGet)
	r.Post("/api/v1/system/keyring/install", h.handleInstall)
	r.Post("/api/v1/system/keyring/use", h.handleUse)
	r.Delete("/api/v1/system/keyring/{kid}", h.handleRemove)
}

// keyRingResponse is what GET returns. Never includes secret bytes.
type keyRingResponse struct {
	Version int                      `json:"version"`
	Entries []crypto.KeyRingMetadata `json:"entries"`
}

// handleGet returns keyring metadata — kids, roles, creation times,
// fingerprints. Secrets are never exposed.
//
//	@Summary		Get agent keyring metadata
//	@Description	Returns the currently-installed API keys' metadata. Secrets are NEVER exposed. The fingerprint is the first 8 hex chars of sha256(secret); useful to verify the dashboard has the same secret without round-tripping the secret itself.
//	@Tags			system
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	keyRingResponse
//	@Failure		401	{string}	string	"Unauthorized"
//	@Router			/api/v1/system/keyring [get]
func (h *KeyRingHandler) handleGet(w http.ResponseWriter, _ *http.Request) {
	if h.keyring == nil {
		h.logger.Error("keyring pointer field nil when serving /system/keyring")
		http.Error(w, "keyring unavailable", http.StatusInternalServerError)
		return
	}
	kr := h.keyring.Load()
	if kr == nil {
		// Should be unreachable — main.go calls NewKeyRingPointer(kr)
		// with a non-nil value before mounting the handler. Defensive
		// 500 + log so a future wiring bug fails loud rather than
		// panicking the agent on every keyring request.
		h.logger.Error("keyring pointer empty when serving /system/keyring")
		http.Error(w, "keyring unavailable", http.StatusInternalServerError)
		return
	}
	resp := keyRingResponse{
		Version: kr.Version,
		Entries: kr.Snapshot(),
	}
	writeJSON(w, http.StatusOK, resp)
}

// installRequest is the install endpoint's body. Secret is base64url-
// encoded raw bytes (the same encoding used in the on-wire token).
type installRequest struct {
	KID      string `json:"kid"`
	SecretB64 string `json:"secret_b64"`
	Role     string `json:"role"` // optional; defaults to "secondary"
}

// handleInstall adds a new entry to the keyring. Idempotent: re-
// installing the same (kid, secret) pair is a no-op and returns 200.
// Installing a different secret under an existing kid returns 409.
//
//	@Summary		Install a new keyring entry
//	@Description	Adds a new accepted API key to the agent's keyring. New entries default to role=secondary; the controller calls /use to flip primary after confirming installation.
//	@Tags			system
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		installRequest	true	"new key"
//	@Success		200		{object}	keyRingResponse
//	@Failure		400		{string}	string	"Bad Request"
//	@Failure		401		{string}	string	"Unauthorized"
//	@Failure		409		{string}	string	"Conflict"
//	@Failure		507		{string}	string	"Keyring full"
//	@Router			/api/v1/system/keyring/install [post]
func (h *KeyRingHandler) handleInstall(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.KID = strings.TrimSpace(req.KID)
	if req.KID == "" || req.SecretB64 == "" {
		writeError(w, http.StatusBadRequest, "kid and secret_b64 are required")
		return
	}
	secret, err := base64.RawURLEncoding.DecodeString(req.SecretB64)
	if err != nil || len(secret) != crypto.SecretLength {
		writeError(w, http.StatusBadRequest, "secret_b64 must be base64url-encoded 32 random bytes")
		return
	}
	role := req.Role
	if role == "" {
		role = "secondary"
	}
	if role != "primary" && role != "secondary" {
		writeError(w, http.StatusBadRequest, "role must be 'primary' or 'secondary'")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	current := h.keyring.Load()
	next := current.Clone()

	// Idempotency: same (kid, secret) → 200 with current state. Same
	// kid + different secret → 409. The controller treats both as
	// "already done" but the 409 surfaces a state divergence to logs.
	if existing := findEntry(current, req.KID); existing != nil {
		if constantTimeEq(existing.Secret, secret) {
			writeJSON(w, http.StatusOK, keyRingResponse{
				Version: current.Version,
				Entries: current.Snapshot(),
			})
			return
		}
		writeError(w, http.StatusConflict, "kid already exists with a different secret")
		return
	}

	entry := crypto.KeyRingEntry{
		KID:    req.KID,
		Secret: secret,
		Role:   role,
	}
	if err := next.Add(entry); err != nil {
		if errors.Is(err, crypto.ErrKeyRingFull) {
			writeError(w, http.StatusInsufficientStorage, "keyring at maximum capacity")
			return
		}
		writeError(w, http.StatusBadRequest, "add entry: "+err.Error())
		return
	}

	if err := crypto.SaveKeyRing(h.path, next); err != nil {
		h.logger.Error("save keyring", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist keyring")
		return
	}
	h.keyring.Store(next)
	h.logger.Info("keyring: installed entry", "kid", req.KID, "role", role)

	writeJSON(w, http.StatusOK, keyRingResponse{
		Version: next.Version,
		Entries: next.Snapshot(),
	})
}

// useRequest is the use endpoint's body.
type useRequest struct {
	KID string `json:"kid"`
}

// handleUse flips the named entry to role=primary and demotes the
// existing primary to secondary. Both stay accepted; the agent doesn't
// distinguish primary vs secondary for inbound auth purposes, but the
// /keyring metadata response and the kid echoed in `X-Gearbox-Kid` let
// the controller drive the overlap window correctly.
//
//	@Summary		Promote a keyring entry to primary
//	@Description	Flips the named keyring entry's role to "primary" and demotes the prior primary to "secondary". Both keys remain valid for inbound auth.
//	@Tags			system
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		useRequest	true	"target kid"
//	@Success		200		{object}	keyRingResponse
//	@Failure		400		{string}	string	"Bad Request"
//	@Failure		401		{string}	string	"Unauthorized"
//	@Failure		404		{string}	string	"Unknown kid"
//	@Router			/api/v1/system/keyring/use [post]
func (h *KeyRingHandler) handleUse(w http.ResponseWriter, r *http.Request) {
	var req useRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.KID = strings.TrimSpace(req.KID)
	if req.KID == "" {
		writeError(w, http.StatusBadRequest, "kid is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	next := h.keyring.Load().Clone()
	if err := next.SetPrimary(req.KID); err != nil {
		if errors.Is(err, crypto.ErrUnknownKID) {
			writeError(w, http.StatusNotFound, "unknown kid")
			return
		}
		writeError(w, http.StatusInternalServerError, "set primary: "+err.Error())
		return
	}

	if err := crypto.SaveKeyRing(h.path, next); err != nil {
		h.logger.Error("save keyring", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist keyring")
		return
	}
	h.keyring.Store(next)
	h.logger.Info("keyring: promoted to primary", "kid", req.KID)

	writeJSON(w, http.StatusOK, keyRingResponse{
		Version: next.Version,
		Entries: next.Snapshot(),
	})
}

// handleRemove deletes the named entry. Refuses to remove the only
// remaining entry — an operator error otherwise bricks the box.
//
//	@Summary		Remove a keyring entry
//	@Description	Deletes the named keyring entry. Refuses to remove the only remaining entry; the agent always retains at least one accepted key.
//	@Tags			system
//	@Security		BearerAuth
//	@Param			kid	path	string	true	"key id"
//	@Success		200	{object}	keyRingResponse
//	@Failure		401	{string}	string	"Unauthorized"
//	@Failure		404	{string}	string	"Unknown kid"
//	@Failure		409	{string}	string	"Cannot remove last key"
//	@Router			/api/v1/system/keyring/{kid} [delete]
func (h *KeyRingHandler) handleRemove(w http.ResponseWriter, r *http.Request) {
	kid := strings.TrimSpace(chi.URLParam(r, "kid"))
	if kid == "" {
		writeError(w, http.StatusBadRequest, "kid is required")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	next := h.keyring.Load().Clone()
	if err := next.Remove(kid); err != nil {
		switch {
		case errors.Is(err, crypto.ErrUnknownKID):
			writeError(w, http.StatusNotFound, "unknown kid")
		case errors.Is(err, crypto.ErrCannotRemoveLast):
			writeError(w, http.StatusConflict, "cannot remove the only remaining key")
		default:
			writeError(w, http.StatusInternalServerError, "remove: "+err.Error())
		}
		return
	}

	if err := crypto.SaveKeyRing(h.path, next); err != nil {
		h.logger.Error("save keyring", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist keyring")
		return
	}
	h.keyring.Store(next)
	h.logger.Info("keyring: removed entry", "kid", kid)

	writeJSON(w, http.StatusOK, keyRingResponse{
		Version: next.Version,
		Entries: next.Snapshot(),
	})
}

// findEntry returns the entry with the given kid, or nil. Reads only
// — does not lock; caller already holds the handler mutex.
func findEntry(kr *crypto.KeyRing, kid string) *crypto.KeyRingEntry {
	for i := range kr.Entries {
		if kr.Entries[i].KID == kid {
			return &kr.Entries[i]
		}
	}
	return nil
}

// constantTimeEq compares two byte slices in constant time. Used by
// the install-idempotency check so reinstalling under an existing kid
// doesn't leak timing about the existing secret.
func constantTimeEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
