// Package handler — per-user, per-box layout endpoints for the
// Metrics gear's draggable page (issue #103).
//
// Two endpoints live here:
//
//	GET   /api/{boxID}/metrics/layout
//	    → returns the user's saved layout, or 204 No Content when
//	      no row exists (caller renders the template default).
//
//	PATCH /api/{boxID}/metrics/layout
//	    → upserts the layout from a JSON body. Body is GridStack's
//	      `save()` output verbatim, validated only as well-formed
//	      JSON + a tile-shape sanity check.
//
// A DELETE variant powers "reset to default" — drops the saved row
// so the next GET 204s and the page falls back to its template
// default.
//
// All three require `metrics:view` permission (the same scope the
// metrics page itself uses); we don't have a separate "edit layout"
// scope because the persisted layout is per-user, so a user can
// only ever edit their own view.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// maxLayoutBytes caps the request body size so a runaway client
// can't fill the database with megabyte-sized JSON. Real GridStack
// payloads for the metrics page are ~11 tiles × ~50 bytes/tile +
// envelope = ~1 KB; 16 KB is generous and matches what other
// dashboard PATCH endpoints accept.
const maxLayoutBytes = 16 * 1024

// layoutTile is the minimum shape we require each entry in the
// posted layout array to carry. GridStack's `save()` includes
// these four fields for every node; the `id` is the stable DOM id
// of the tile (e.g. "card-cpu"). We don't enforce the id's value
// against the known set of cards — the dashboard renders cards by
// id and ignores anything it doesn't recognise, so an unknown id
// in the saved layout is a no-op at render time rather than a
// failure mode worth rejecting here.
type layoutTile struct {
	ID string `json:"id"`
	X  int    `json:"x"`
	Y  int    `json:"y"`
	W  int    `json:"w"`
	H  int    `json:"h"`
}

// APIMetricsLayoutGetHandler returns the user's saved metrics
// layout for one box. Returns 204 No Content when nothing's saved
// — that's the signal for the front-end to use the template's
// default tile positions rather than the empty grid.
func (h *Handler) APIMetricsLayoutGetHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	user, err := h.authManager.GetUser(r)
	if err != nil || user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	layout, err := h.db.GetMetricsLayout(user.ID, boxID)
	if err != nil {
		if errors.Is(err, database.ErrNoMetricsLayout) {
			// Empty response — front-end falls back to defaults.
			// 204 is the right code: success, no body to return.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.logger.Error("metrics layout get", "user_id", user.ID, "server_id", boxID, "error", err)
		http.Error(w, "Failed to load layout", http.StatusInternalServerError)
		return
	}

	// Pass the JSON blob through verbatim — we never re-parse it
	// on the way out (it round-trips back into GridStack.load()
	// on the client).
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(layout.Layout)
}

// APIMetricsLayoutPatchHandler upserts the layout JSON the
// dashboard sent. The body must decode as a JSON array of tile
// objects (see layoutTile); anything else is rejected as 400
// rather than persisted, so the GET path can trust the stored
// blob is shape-valid.
func (h *Handler) APIMetricsLayoutPatchHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	user, err := h.authManager.GetUser(r)
	if err != nil || user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxLayoutBytes+1))
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	if len(body) > maxLayoutBytes {
		http.Error(w, "Layout body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Shape-validate: array of tile objects with at minimum the
	// four GridStack fields. We don't reject extra keys — the
	// agnostic-blob design wants newer GridStack versions' extra
	// fields to round-trip without a Go change.
	var tiles []layoutTile
	if err := json.Unmarshal(body, &tiles); err != nil {
		http.Error(w, "Invalid layout JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(tiles) == 0 {
		http.Error(w, "Layout must contain at least one tile", http.StatusBadRequest)
		return
	}

	if err := h.db.SaveMetricsLayout(user.ID, boxID, body); err != nil {
		h.logger.Error("metrics layout save", "user_id", user.ID, "server_id", boxID, "error", err)
		http.Error(w, "Failed to save layout", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// APIMetricsLayoutDeleteHandler drops the user's saved layout for
// one box. The next GET 204s and the page renders defaults — the
// "reset to default" button in the edit-mode toolbar hits this.
func (h *Handler) APIMetricsLayoutDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	user, err := h.authManager.GetUser(r)
	if err != nil || user == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteMetricsLayout(user.ID, boxID); err != nil {
		h.logger.Error("metrics layout delete", "user_id", user.ID, "server_id", boxID, "error", err)
		http.Error(w, "Failed to delete layout", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
