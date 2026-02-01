package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/events"
)

// APIBoxesHandler returns list of configured servers.
func (h *Handler) APIBoxesHandler(w http.ResponseWriter, r *http.Request) {
	enabledServers := h.getEnabledServers()
	servers := make([]map[string]interface{}, 0, len(enabledServers))

	for _, server := range enabledServers {
		servers = append(servers, map[string]interface{}{
			"id":   server.ID,
			"name": server.Name,
		})
	}

	h.writeJSON(w, map[string]interface{}{
		"servers": servers,
	})
}

// APIKeepaliveHandler extends the session when the user is active.
func (h *Handler) APIKeepaliveHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.authManager.ExtendSession(w, r); err != nil {
		http.Error(w, "Session extension failed", http.StatusUnauthorized)
		return
	}

	// Get the new expiration time
	expiresAt, err := h.authManager.GetSessionExpirationTime(r)
	if err != nil {
		http.Error(w, "Failed to get session expiration", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"status":    "ok",
		"expiresAt": expiresAt.Format(time.RFC3339),
	})
}

// APISessionInfoHandler returns information about the current session.
func (h *Handler) APISessionInfoHandler(w http.ResponseWriter, r *http.Request) {
	expiresAt, err := h.authManager.GetSessionExpirationTime(r)
	if err != nil {
		http.Error(w, "Failed to get session info", http.StatusUnauthorized)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"expiresAt": expiresAt.Format(time.RFC3339),
	})
}

// APIIncidentsHandler returns recent incidents.
func (h *Handler) APIIncidentsHandler(w http.ResponseWriter, r *http.Request) {
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Parse active_only parameter
	activeOnly := r.URL.Query().Get("active_only") == "true"

	// Parse limit parameter (default: 50)
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	var incidents interface{}
	var err error

	if activeOnly {
		incidents, err = h.db.GetActiveIncidents(boxID)
	} else {
		incidents, err = h.db.GetRecentIncidents(boxID, limit)
	}

	if err != nil {
		h.logger.Error("Failed to get incidents", "error", err)
		http.Error(w, "Failed to get incidents", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id":   boxID,
		"active_only": activeOnly,
		"incidents":   incidents,
	})
}

// APIDatabaseStatsHandler returns database statistics.
func (h *Handler) APIDatabaseStatsHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := h.db.GetDatabaseStats()
	if err != nil {
		h.logger.Error("Failed to get database stats", "error", err)
		http.Error(w, "Failed to get database stats", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"stats": stats,
	})
}

// APIEventsHandler provides Server-Sent Events for real-time updates.
// This endpoint streams events to the browser using SSE protocol.
func (h *Handler) APIEventsHandler(w http.ResponseWriter, r *http.Request) {
	if h.eventHub == nil {
		http.Error(w, "Events not available", http.StatusServiceUnavailable)
		return
	}

	// Optional server filter
	boxID := r.URL.Query().Get("server")

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create subscriber
	subscriberID := uuid.New().String()
	sub := h.eventHub.Subscribe(subscriberID, boxID)
	defer h.eventHub.Unsubscribe(sub)

	// Flush initial response
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial connection event
	h.writeSSEEvent(w, flusher, events.Event{
		Type:      "connected",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"subscriber_id": subscriberID,
		},
	})

	// Send keepalive comment every 5 seconds to prevent connection timeout
	// More frequent keepalives ensure proxies and browsers don't close the connection
	keepalive := time.NewTicker(5 * time.Second)
	defer keepalive.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			h.logger.Debug("SSE client disconnected", "subscriber_id", subscriberID)
			return

		case event, ok := <-sub.Events:
			if !ok {
				// Channel closed
				return
			}
			h.writeSSEEvent(w, flusher, event)

		case <-keepalive.C:
			// Send keepalive comment
			_, _ = fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// APIWebSocketStatusHandler returns WebSocket connection status for all servers.
func (h *Handler) APIWebSocketStatusHandler(w http.ResponseWriter, r *http.Request) {
	if h.wsManager == nil {
		h.writeJSON(w, map[string]interface{}{
			"enabled": false,
		})
		return
	}

	status := h.wsManager.GetConnectionStatus()
	h.writeJSON(w, map[string]interface{}{
		"enabled":     true,
		"connections": status,
		"count":       h.wsManager.ConnectionCount(),
	})
}

// APIDisableEntityHandler disables a backend, frontend, or service.
func (h *Handler) APIDisableEntityHandler(w http.ResponseWriter, r *http.Request) {
	// Check permission to manage disabled entities
	if !h.authManager.HasPermission(r, "disabled_entities", "manage") {
		http.Error(w, "Forbidden: insufficient permissions to disable entities", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		EntityType string `json:"entity_type"`
		EntityName string `json:"entity_name"`
		Notes      string `json:"notes"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.EntityType == "" || req.EntityName == "" {
		http.Error(w, "Entity type and name required", http.StatusBadRequest)
		return
	}

	// Get user ID from session context
	var userID *string
	if user, ok := auth.GetUserFromContext(r.Context()); ok && user != nil {
		userID = &user.ID
	}

	// Disable the entity
	if err := h.db.DisableEntity(boxID, database.EntityType(req.EntityType), req.EntityName, userID, req.Notes); err != nil {
		h.logger.Error("Failed to disable entity", "error", err)
		http.Error(w, "Failed to disable entity", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"status":      "ok",
		"server_id":   boxID,
		"entity_type": req.EntityType,
		"entity_name": req.EntityName,
	})
}

// APIEnableEntityHandler re-enables a disabled backend, frontend, or service.
func (h *Handler) APIEnableEntityHandler(w http.ResponseWriter, r *http.Request) {
	// Check permission to manage disabled entities
	if !h.authManager.HasPermission(r, "disabled_entities", "manage") {
		http.Error(w, "Forbidden: insufficient permissions to enable entities", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		EntityType string `json:"entity_type"`
		EntityName string `json:"entity_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.EntityType == "" || req.EntityName == "" {
		http.Error(w, "Entity type and name required", http.StatusBadRequest)
		return
	}

	// Enable the entity
	if err := h.db.EnableEntity(boxID, database.EntityType(req.EntityType), req.EntityName); err != nil {
		h.logger.Error("Failed to enable entity", "error", err)
		http.Error(w, "Failed to enable entity", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"status":      "ok",
		"server_id":   boxID,
		"entity_type": req.EntityType,
		"entity_name": req.EntityName,
	})
}

// APIDisabledEntitiesHandler returns all disabled entities for a server.
func (h *Handler) APIDisabledEntitiesHandler(w http.ResponseWriter, r *http.Request) {
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	entities, err := h.db.GetDisabledEntities(boxID)
	if err != nil {
		h.logger.Error("Failed to get disabled entities", "error", err)
		http.Error(w, "Failed to get disabled entities", http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id": boxID,
		"entities":  entities,
		"count":     len(entities),
	})
}
