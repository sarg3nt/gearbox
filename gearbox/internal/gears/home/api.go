package home

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/services"
)

// db returns the typed database handle, downcasting through the AuthAdapter.
// Returns nil if the adapter is not the expected concrete type — handlers
// should treat that as an internal error.
func (h *Handlers) db() *database.DB {
	a, ok := h.deps.Auth.(*services.AuthAdapter)
	if !ok {
		return nil
	}
	return a.GetDB()
}

// requirePerm gates a handler on a Home gear permission. Writes the appropriate
// HTTP error and returns false if the user is not allowed.
func (h *Handlers) requirePerm(w http.ResponseWriter, r *http.Request, action string) bool {
	if h.deps.Auth == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	if !h.deps.Auth.HasPermission(r, "home", action) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return false
	}
	return true
}

// writeJSON writes v as JSON with the given status. Errors are logged but
// not exposed to the client.
func (h *Handlers) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.deps.Logger.Error("failed to encode JSON response", "error", err)
	}
}

// boardCreateRequest is the body shape for POST /home/api/boards.
type boardCreateRequest struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

// boardUpdateRequest is the body shape for PATCH /home/api/boards/{id}.
type boardUpdateRequest struct {
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

// tileCreateRequest is the body shape for POST /home/api/boards/{id}/tiles.
type tileCreateRequest struct {
	Type      string          `json:"type"`
	X         int             `json:"x"`
	Y         int             `json:"y"`
	W         int             `json:"w"`
	H         int             `json:"h"`
	Config    json.RawMessage `json:"config"`
	SortOrder int             `json:"sort_order"`
}

// tileUpdateRequest is the body shape for PATCH /home/api/tiles/{id}.
// Either layout or config (or both) may be present; nil pointers mean "leave alone".
type tileUpdateRequest struct {
	X         *int             `json:"x,omitempty"`
	Y         *int             `json:"y,omitempty"`
	W         *int             `json:"w,omitempty"`
	H         *int             `json:"h,omitempty"`
	SortOrder *int             `json:"sort_order,omitempty"`
	Config    *json.RawMessage `json:"config,omitempty"`
}

// landingPathRequest sets the calling user's default_landing_path.
type landingPathRequest struct {
	// Path is the destination URL. Empty string clears the override.
	Path string `json:"path"`
}

// ListBoards returns all boards on the dashboard.
func (h *Handlers) ListBoards(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "view") {
		return
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	boards, err := db.ListHomeBoards()
	if err != nil {
		h.deps.Logger.Error("ListHomeBoards failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if boards == nil {
		boards = []database.HomeBoard{}
	}
	h.writeJSON(w, http.StatusOK, boards)
}

// CreateBoard inserts a new board.
func (h *Handlers) CreateBoard(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "edit") {
		return
	}
	var req boardCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	req.Name = strings.TrimSpace(req.Name)
	if req.Slug == "" || req.Name == "" {
		http.Error(w, "slug and name are required", http.StatusBadRequest)
		return
	}
	if !validSlug(req.Slug) {
		http.Error(w, "slug must be lowercase letters, digits, or '-'", http.StatusBadRequest)
		return
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	board, err := db.CreateHomeBoard(req.Slug, req.Name, req.SortOrder)
	if err != nil {
		h.deps.Logger.Error("CreateHomeBoard failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, http.StatusCreated, board)
}

// UpdateBoard updates a board's name and sort order.
func (h *Handlers) UpdateBoard(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "edit") {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid board id", http.StatusBadRequest)
		return
	}
	var req boardUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if err := db.UpdateHomeBoard(id, strings.TrimSpace(req.Name), req.SortOrder); err != nil {
		h.deps.Logger.Error("UpdateHomeBoard failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteBoard removes a board (and cascades to its tiles). Refuses to delete
// the last board to keep the dashboard renderable.
func (h *Handlers) DeleteBoard(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "edit") {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid board id", http.StatusBadRequest)
		return
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if err := db.DeleteHomeBoard(id); err != nil {
		// "cannot delete the only remaining home board" is a user-facing constraint.
		if strings.Contains(err.Error(), "only remaining") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		h.deps.Logger.Error("DeleteHomeBoard failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListTiles returns every tile on a board.
func (h *Handlers) ListTiles(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "view") {
		return
	}
	boardID, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid board id", http.StatusBadRequest)
		return
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	tiles, err := db.ListHomeTiles(boardID)
	if err != nil {
		h.deps.Logger.Error("ListHomeTiles failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if tiles == nil {
		tiles = []database.HomeTile{}
	}
	// Annotate each tile with whether a secret is configured, so the
	// browser can show a "key set" indicator without ever seeing the value.
	type tileResp struct {
		database.HomeTile
		HasSecret bool `json:"has_secret"`
	}
	resp := make([]tileResp, 0, len(tiles))
	for _, t := range tiles {
		has, _ := db.HasHomeTileSecret(t.ID)
		resp = append(resp, tileResp{HomeTile: t, HasSecret: has})
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// CreateTile inserts a new tile on a board.
func (h *Handlers) CreateTile(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "edit") {
		return
	}
	boardID, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid board id", http.StatusBadRequest)
		return
	}
	var req tileCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if !validTileType(req.Type) {
		http.Error(w, "invalid tile type", http.StatusBadRequest)
		return
	}
	if req.W <= 0 {
		req.W = 2
	}
	if req.H <= 0 {
		req.H = 1
	}
	tile := &database.HomeTile{
		BoardID:   boardID,
		Type:      database.HomeTileType(req.Type),
		X:         req.X,
		Y:         req.Y,
		W:         req.W,
		H:         req.H,
		Config:    req.Config,
		SortOrder: req.SortOrder,
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	id, err := db.CreateHomeTile(tile)
	if err != nil {
		h.deps.Logger.Error("CreateHomeTile failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	tile.ID = id
	h.writeJSON(w, http.StatusCreated, tile)
}

// UpdateTile patches a tile. Layout fields and/or config can be supplied;
// missing fields are left untouched.
func (h *Handlers) UpdateTile(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "edit") {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid tile id", http.StatusBadRequest)
		return
	}
	var req tileUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Apply layout if any of x/y/w/h/sort_order were provided.
	if req.X != nil || req.Y != nil || req.W != nil || req.H != nil || req.SortOrder != nil {
		existing, err := db.GetHomeTile(id)
		if err != nil {
			h.deps.Logger.Error("GetHomeTile failed", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		if existing == nil {
			http.Error(w, "tile not found", http.StatusNotFound)
			return
		}
		x := existing.X
		if req.X != nil {
			x = *req.X
		}
		y := existing.Y
		if req.Y != nil {
			y = *req.Y
		}
		wv := existing.W
		if req.W != nil && *req.W > 0 {
			wv = *req.W
		}
		hv := existing.H
		if req.H != nil && *req.H > 0 {
			hv = *req.H
		}
		so := existing.SortOrder
		if req.SortOrder != nil {
			so = *req.SortOrder
		}
		if err := db.UpdateHomeTileLayout(id, x, y, wv, hv, so); err != nil {
			h.deps.Logger.Error("UpdateHomeTileLayout failed", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	}

	// Apply config update independently so partial updates of either
	// half don't require sending the other half.
	if req.Config != nil {
		if err := db.UpdateHomeTileConfig(id, *req.Config); err != nil {
			h.deps.Logger.Error("UpdateHomeTileConfig failed", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteTile removes a tile (and any associated secret via FK cascade).
func (h *Handlers) DeleteTile(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "edit") {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid tile id", http.StatusBadRequest)
		return
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if err := db.DeleteHomeTile(id); err != nil {
		h.deps.Logger.Error("DeleteHomeTile failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// secretRequest is the body shape for PUT /home/api/tiles/{id}/secret.
type secretRequest struct {
	// Secret is the API key / token / password (depending on the app's auth mode).
	Secret string `json:"secret"`
	// BasicUsername is set for providers that need both username + password
	// (e.g. qBittorrent). Stored alongside the secret as "<user>\n<secret>".
	BasicUsername string `json:"basic_username,omitempty"`
}

// SetTileSecret encrypts and stores a tile's secret. To clear, send an empty
// secret. The browser never sees the encrypted value back; subsequent reads
// only ever return has_secret: true.
func (h *Handlers) SetTileSecret(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "edit") {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid tile id", http.StatusBadRequest)
		return
	}
	var req secretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	if req.Secret == "" {
		if err := db.SetHomeTileSecret(id, nil); err != nil {
			h.deps.Logger.Error("clear tile secret failed", "error", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	enc := h.encryptor()
	if enc == nil {
		http.Error(w, "secrets encryptor not configured", http.StatusServiceUnavailable)
		return
	}

	plain := req.Secret
	if req.BasicUsername != "" {
		// Encode "<username>\n<secret>" so a single blob covers both halves.
		plain = req.BasicUsername + "\n" + req.Secret
	}
	encrypted, err := enc.EncryptString(plain)
	if err != nil {
		h.deps.Logger.Error("encrypt tile secret failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if err := db.SetHomeTileSecret(id, encrypted); err != nil {
		h.deps.Logger.Error("store tile secret failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TileWidget returns the most recent widget data snapshot for a tile.
// Used by the browser on page load to populate widget tiles before the
// first SSE event arrives.
func (h *Handlers) TileWidget(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "view") {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid tile id", http.StatusBadRequest)
		return
	}
	if h.widgetRunner == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{"tile_id": id, "fields": map[string]string{}})
		return
	}
	if evt, ok := h.widgetRunner.snapshot(id); ok {
		h.writeJSON(w, http.StatusOK, evt)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"tile_id": id, "fields": map[string]string{}})
}

// CatalogList returns the predefined apps catalog.
func (h *Handlers) CatalogList(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "view") {
		return
	}
	entries, err := Catalog()
	if err != nil {
		h.deps.Logger.Error("Catalog load failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, http.StatusOK, entries)
}

// Probe fingerprints a URL against the catalog and returns the matched
// app entry (or null when nothing matched). The browser calls this after
// the user pastes a URL in the add-tile modal.
func (h *Handlers) Probe(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "view") {
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("url"))
	if target == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	prober := newFingerprintProber()
	slug, ok := prober.Detect(r.Context(), target)
	if !ok {
		h.writeJSON(w, http.StatusOK, map[string]any{"matched": false})
		return
	}
	entry, _ := CatalogBySlug(slug)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"matched": true,
		"app":     entry,
	})
}

// TileStatus returns the most recent status snapshot for a tile, or unknown
// when the worker has not yet probed it. The browser hits this on page load
// to populate status dots before the SSE stream catches up.
func (h *Handlers) TileStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "view") {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.Error(w, "invalid tile id", http.StatusBadRequest)
		return
	}
	if h.worker == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{
			"tile_id": id,
			"status":  StatusUnknown,
		})
		return
	}
	if evt, ok := h.worker.snapshot(id); ok {
		h.writeJSON(w, http.StatusOK, evt)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"tile_id": id,
		"status":  StatusUnknown,
	})
}

// SetLandingPath sets the calling user's per-user default_landing_path.
// Empty string clears it. The browser POSTs this from a "Make Home my default
// page" toggle on the Home settings panel.
func (h *Handlers) SetLandingPath(w http.ResponseWriter, r *http.Request) {
	if h.deps.Auth == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	user := h.deps.Auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req landingPathRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	// Defensive: only accept relative paths starting with "/".
	if req.Path != "" && !strings.HasPrefix(req.Path, "/") {
		http.Error(w, "path must start with /", http.StatusBadRequest)
		return
	}
	db := h.db()
	if db == nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	if err := db.SetUserDefaultLandingPath(user.ID, req.Path); err != nil {
		h.deps.Logger.Error("SetUserDefaultLandingPath failed", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathID extracts a numeric URL parameter.
func pathID(r *http.Request, key string) (int64, error) {
	raw := chi.URLParam(r, key)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

// validSlug enforces a kebab-case slug for board URLs.
func validSlug(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

// validTileType returns true for a known HomeTileType string.
func validTileType(t string) bool {
	switch database.HomeTileType(t) {
	case database.TileTypeApp, database.TileTypeBookmark, database.TileTypeCustomAPI,
		database.TileTypeIframe, database.TileTypeClock, database.TileTypeWeather, database.TileTypeSearch:
		return true
	}
	return false
}
