package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

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

	_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}
