package bx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/auth"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// Handlers serves the Bx fleet view pages and APIs.
type Handlers struct {
	deps    gear.Dependencies
	monitor *statusMonitor
}

// NewHandlers builds the HTTP handlers backed by the given status monitor.
func NewHandlers(deps gear.Dependencies, monitor *statusMonitor) *Handlers {
	return &Handlers{deps: deps, monitor: monitor}
}

// IndexPage renders the fleet view. The page lists every enabled box with
// its current status dot; the table is hydrated client-side from
// /bx/api/status and updated live via /bx/api/events (SSE).
func (h *Handlers) IndexPage(w http.ResponseWriter, r *http.Request) {
	if !gear.RequirePermission(h.deps.Auth, w, r, "bx", "view") {
		return
	}
	user, fullUser := h.fullUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	boxes := auth.GetAllBoxesFromContext(r.Context())
	// Augment with what the monitor has already learned so the first paint
	// has correct dots instead of waiting on the SSE stream.
	rows := buildRows(boxes, h.monitor)

	data := IndexPageData{
		User:  fullUser,
		Rows:  rows,
		Total: len(rows),
	}
	IndexPage(data).Render(r.Context(), w) //nolint:errcheck
}

// StatusList returns the current status of every box as JSON. Used by the
// page's reconcile-on-load logic and by the header chip's switcher palette.
func (h *Handlers) StatusList(w http.ResponseWriter, r *http.Request) {
	if !gear.RequirePermission(h.deps.Auth, w, r, "bx", "view") {
		return
	}
	boxes := auth.GetAllBoxesFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"rows":  buildRows(boxes, h.monitor),
		"total": len(boxes),
	})
}

// EventsStream is the SSE endpoint that streams `box.status` updates as
// soon as the monitor detects a level transition. Browsers subscribe on
// page load and re-render the affected row in place.
func (h *Handlers) EventsStream(w http.ResponseWriter, r *http.Request) {
	if !gear.RequirePermission(h.deps.Auth, w, r, "bx", "view") {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := h.monitor.Subscribe()
	defer unsub()

	// Send an initial snapshot so a fresh subscriber doesn't sit blank
	// waiting for the next transition.
	for _, s := range h.monitor.Snapshot() {
		if !writeSSE(w, flusher, s) {
			return
		}
	}

	// Periodic heartbeat keeps proxies from idling the connection out.
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case s, ok := <-ch:
			if !ok {
				return
			}
			if !writeSSE(w, flusher, s) {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, s BoxStatus) bool {
	payload, err := json.Marshal(s)
	if err != nil {
		return true // skip malformed but stay connected
	}
	if _, err := fmt.Fprintf(w, "event: box.status\ndata: %s\n\n", payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// fullUser returns the framework-level user and a templ-friendly *models.User
// so the page header can render the avatar correctly.
func (h *Handlers) fullUser(r *http.Request) (*gear.User, *models.User) {
	u := h.deps.Auth.GetUser(r)
	if u == nil {
		return nil, nil
	}
	full := &models.User{
		ID:        u.ID,
		Email:     u.Email,
		FirstName: u.FirstName,
		LastName:  u.LastName,
	}
	if u.IsAdmin {
		full.Role = "admin"
	} else {
		full.Role = "user"
	}
	return u, full
}

// buildRows joins the configured-box roster with the monitor's snapshot.
// Boxes the monitor hasn't reached yet get StatusUnknown so the table
// renders immediately on first load instead of waiting on the SSE.
func buildRows(boxes []models.BoxConfig, monitor *statusMonitor) []BoxStatus {
	rows := make([]BoxStatus, 0, len(boxes))
	for _, b := range boxes {
		if s, ok := monitor.Get(b.ID); ok {
			rows = append(rows, s)
			continue
		}
		rows = append(rows, BoxStatus{
			BoxID:    b.ID,
			Name:     b.Name,
			AgentURL: b.AgentURL,
			Enabled:  true,
			Level:    StatusUnknown,
		})
	}
	return rows
}
