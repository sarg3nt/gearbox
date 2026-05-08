package home

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

// sseHub fans out home-gear events to connected browsers. Each browser opens
// one HTTP request that hangs forever, receiving lines like:
//
//	event: tile.status
//	data: {"tile_id":42,"status":"up","latency_ms":83,"http_status":200,"checked_at":"2026-..."}
//
// We keep this gear-local rather than reusing the global /api/events bus so
// the worker can pause when there are no Home subscribers without affecting
// other gears' streams.
type sseHub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
	count   atomic.Int64
}

// newSSEHub builds an empty hub.
func newSSEHub() *sseHub {
	return &sseHub{clients: make(map[chan []byte]struct{})}
}

// subscribe registers a new client channel. The returned unsubscribe function
// must be called when the request handler returns.
func (h *sseHub) subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	h.count.Add(1)
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.clients[ch]; ok {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
		h.count.Add(-1)
	}
}

// publish sends an event to all subscribers. Non-blocking — slow consumers
// drop messages rather than back-pressuring the worker.
func (h *sseHub) publish(event string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	frame := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, body))
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- frame:
		default:
			// Drop on slow consumer — they'll get the next update.
		}
	}
}

// activeClients returns the number of connected clients. The worker uses
// this to pause polling when nobody is watching.
func (h *sseHub) activeClients() int {
	return int(h.count.Load())
}

// EventsStream serves SSE to a single client. Streams until the client
// disconnects or the request context is cancelled.
func (h *Handlers) EventsStream(w http.ResponseWriter, r *http.Request) {
	if !h.requirePerm(w, r, "view") {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering when proxied

	ch, unsubscribe := h.hub.subscribe()
	defer unsubscribe()

	// Initial comment line so the browser registers the stream as open.
	_, _ = w.Write([]byte(": ok\n\n"))
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case frame, alive := <-ch:
			if !alive {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
