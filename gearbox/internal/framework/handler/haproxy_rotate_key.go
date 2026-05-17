package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/sarg3nt/gearbox/internal/framework/services/agent_keyring"
)

// HAProxyBoxRotateKeyPost rotates the API key for a single box via the
// install -> use -> mark-retired three-phase dance. The old key stays
// accepted on the agent until the overlap window elapses; an explicit
// cleanup step (manual or scheduled) removes it after that.
//
// Wired at POST /settings/boxes/{id}/rotate-key. Body is empty; the
// only input is the box id from the path.
//
// Response: 200 with {"success":true, "new_kid":..., "old_kid":...,
// "retire_after": "..."} or 4xx/5xx with {"success":false, "message":
// ...}.
func (h *Handler) HAProxyBoxRotateKeyPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeRotateError(w, http.StatusBadRequest, "invalid box id")
		return
	}

	encryptor, err := h.getEncryptor()
	if err != nil {
		h.logger.Error("rotate-key: getEncryptor", "error", err)
		writeRotateError(w, http.StatusInternalServerError, "encryption unavailable")
		return
	}

	rotator := agent_keyring.New(h.db, encryptor, h.logger)

	result, err := rotator.RotateBox(id, agent_keyring.DefaultOverlapWindow)
	if err != nil {
		h.logger.Warn("rotate-key: failed", "box_id", id, "error", err)
		writeRotateError(w, http.StatusBadGateway, "rotation failed: "+err.Error())
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":      true,
		"new_kid":      result.NewKID,
		"old_kid":      result.OldKID,
		"retire_after": result.RetireAfter,
	})
}

// HAProxyBoxesRotateKeyAllPost rotates every enabled box sequentially
// with a small stagger between rotations (avoids hammering the
// agents). Reports per-box outcomes; the operator sees which boxes
// succeeded and which failed and can act on each.
//
// Wired at POST /settings/boxes/rotate-key-all. Body is empty.
func (h *Handler) HAProxyBoxesRotateKeyAllPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	encryptor, err := h.getEncryptor()
	if err != nil {
		h.logger.Error("rotate-all: getEncryptor", "error", err)
		writeRotateError(w, http.StatusInternalServerError, "encryption unavailable")
		return
	}

	boxes, err := h.db.GetEnabledBoxes()
	if err != nil {
		h.logger.Error("rotate-all: list boxes", "error", err)
		writeRotateError(w, http.StatusInternalServerError, "failed to list boxes")
		return
	}

	rotator := agent_keyring.New(h.db, encryptor, h.logger)

	type boxResult struct {
		BoxID   int64  `json:"box_id"`
		Name    string `json:"name"`
		Success bool   `json:"success"`
		NewKID  string `json:"new_kid,omitempty"`
		OldKID  string `json:"old_kid,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	results := make([]boxResult, 0, len(boxes))
	successCount := 0
	for _, box := range boxes {
		out := boxResult{BoxID: box.ID, Name: box.Name}
		rr, rerr := rotator.RotateBox(box.ID, agent_keyring.DefaultOverlapWindow)
		if rerr != nil {
			out.Success = false
			out.Error = rerr.Error()
			h.logger.Warn("rotate-all: box failed", "box_id", box.ID, "name", box.Name, "error", rerr)
		} else {
			out.Success = true
			out.NewKID = rr.NewKID
			out.OldKID = rr.OldKID
			successCount++
		}
		results = append(results, out)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  successCount == len(boxes),
		"rotated":  successCount,
		"total":    len(boxes),
		"results":  results,
	})
}

func writeRotateError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"message": msg,
	})
}
