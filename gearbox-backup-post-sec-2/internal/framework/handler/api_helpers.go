package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/events"
)

// writeJSON writes a JSON response, logging any encoding errors.
// This is a helper function used by all API handlers.
func (h *Handler) writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", "error", err)
	}
}

// writeSSEEvent writes a Server-Sent Event to the response writer.
// This is used for real-time event streaming to clients.
func (h *Handler) writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event events.Event) {
	data, err := events.MarshalEvent(event)
	if err != nil {
		h.logger.Error("Failed to marshal SSE event", "error", err)
		return
	}

	fmt.Fprintf(w, "event: %s\n", event.Type)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// apiError writes a structured error response using the errors package.
// This provides consistent error handling across all API endpoints.
func (h *Handler) apiError(w http.ResponseWriter, err error) {
	errors.WriteHTTPError(w, h.logger, err)
}

// apiBadRequest returns a 400 Bad Request error with a user-friendly message.
func (h *Handler) apiBadRequest(w http.ResponseWriter, message string) {
	h.apiError(w, errors.BadRequest(message, nil))
}

// apiNotFound returns a 404 Not Found error.
func (h *Handler) apiNotFound(w http.ResponseWriter, resource string) {
	h.apiError(w, errors.NotFound(resource, nil))
}

// apiInternal returns a 500 Internal Server Error with sanitized message.
func (h *Handler) apiInternal(w http.ResponseWriter, operation string, err error) {
	h.apiError(w, errors.Internal(operation, err))
}

// apiUnauthorized returns a 401 Unauthorized error.
func (h *Handler) apiUnauthorized(w http.ResponseWriter, message string) {
	h.apiError(w, errors.Unauthorized(message, nil))
}

// apiForbidden returns a 403 Forbidden error.
func (h *Handler) apiForbidden(w http.ResponseWriter, message string) {
	h.apiError(w, errors.Forbidden(message, nil))
}
