package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// APICapabilitiesResponse is the JSON envelope returned by
// GET /api/{boxID}/capabilities. The dashboard's metrics gear (and
// future source-aware gears) call this on render to decide which
// cards / KPIs / panels to surface for the active box. Fields mirror
// the agent's CapabilitiesResponse with one wrapping layer so the
// envelope can grow (e.g. dashboard-side derived flags) without
// changing the agent contract.
type APICapabilitiesResponse struct {
	// Gears keys agent gear names ("haproxy", "metrics", "logs", …)
	// to their probe verdicts. Missing keys mean the agent didn't
	// surface that gear at all — older agents or gears not yet
	// probe-aware. Callers should treat missing as "unknown" rather
	// than "not available".
	Gears map[string]APICapabilityEntry `json:"gears"`

	// FetchedAt is when the dashboard last saw fresh capabilities from
	// the agent. Allows callers to reason about staleness; the
	// dashboard refreshes on agent reconnect events plus a short TTL.
	FetchedAt time.Time `json:"fetched_at"`
}

// APICapabilityEntry is one row of the probe table. Mirrors the
// agent-side CapabilityEntry without exposing the agent's internal
// ProbeStatus type alias.
type APICapabilityEntry struct {
	Status       string            `json:"status"`
	Reason       string            `json:"reason,omitempty"`
	Capabilities map[string]string `json:"capabilities,omitempty"`
}

// APIBoxCapabilitiesHandler handles GET /api/{boxID}/capabilities and
// returns the cached agent probe table for the box. Used by the
// metrics gear's frontend (and any future source-aware UI) to hide
// cards / KPIs for sources the box can't produce.
//
// Gated on ComponentMetrics + PermissionView. The manifest enumerates
// detected services on the host, which is enough information for
// fingerprinting in a multi-tenant deploy — a user who can't see
// metrics has no business knowing whether the host runs nginx vs
// Apache vs Caddy. Mirrors APIMetricsSummaryHandler's gate.
//
// Returns 503 when the box isn't agent-backed or capabilities can't
// be fetched — callers should fail open (show the full UI) on errors
// so a flaky agent doesn't lock the user out.
func (h *Handler) APIBoxCapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	caps, ok := h.getBoxCapabilities(boxID)
	if !ok || caps == nil || caps.Response == nil {
		http.Error(w, "Capabilities unavailable", http.StatusServiceUnavailable)
		return
	}

	out := APICapabilitiesResponse{
		Gears:     make(map[string]APICapabilityEntry, len(caps.Response.Gears)),
		FetchedAt: caps.FetchedAt,
	}
	for name, entry := range caps.Response.Gears {
		out.Gears[name] = APICapabilityEntry{
			Status:       entry.Status,
			Reason:       entry.Reason,
			Capabilities: entry.Capabilities,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		h.logger.Error("encode capabilities response failed", "box_id", boxID, "error", err)
	}
}
