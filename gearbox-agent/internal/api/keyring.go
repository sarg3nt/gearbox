package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sarg3nt/gearbox-agent/internal/framework/crypto"
)

// KeyRingHandler exposes the agent's keyring metadata over the HTTP API.
// Phase 1 ships only the read endpoint; Phase 2 adds install/use/remove
// for the controller-driven three-phase rotation flow (issue #72).
type KeyRingHandler struct {
	keyring *crypto.KeyRingPointer
	logger  *slog.Logger
}

// NewKeyRingHandler wires a handler around the agent's keyring pointer.
func NewKeyRingHandler(keyring *crypto.KeyRingPointer, logger *slog.Logger) *KeyRingHandler {
	return &KeyRingHandler{keyring: keyring, logger: logger}
}

// RegisterRoutes mounts the keyring endpoints on r. The caller is
// responsible for putting r behind the API-key auth middleware.
func (h *KeyRingHandler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/system/keyring", h.handleGet)
}

// keyRingResponse is what the API returns. Never includes secret bytes.
type keyRingResponse struct {
	Version int                       `json:"version"`
	Entries []crypto.KeyRingMetadata  `json:"entries"`
}

// handleGet returns the keyring metadata — kids, roles, creation times,
// and short fingerprints — but never the actual secrets.
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
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("encode keyring response failed", "error", err)
	}
}
