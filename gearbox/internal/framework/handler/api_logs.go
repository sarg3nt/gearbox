package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// APILogsHandler returns logs as JSON.
// Supported log names: haproxy, system, fail2ban
func (h *Handler) APILogsHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view logs
	if !h.authManager.HasPermission(r, models.ComponentLogs, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view logs", http.StatusForbidden)
		return
	}

	boxID := chi.URLParam(r, "boxID")
	logName := chi.URLParam(r, "logName")

	if boxID == "" || logName == "" {
		http.Error(w, "Server ID and log name required", http.StatusBadRequest)
		return
	}

	collector, exists := h.getCollector(boxID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Fetch logs via Agent API
	content, err := collector.GetLog(logName, 1000)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("fetch logs", err))
		return
	}

	response := map[string]interface{}{
		"log_name": logName,
		"logs":     content,
		"lines":    len(content),
	}

	h.writeJSON(w, response)
}

// APILogSourcesHandler returns the enabled log sources for a server.
//
// Source resolution precedence (issue #112):
//  1. Explicit per-box settings in the database — operator opted in.
//  2. Defaults derived from the agent's probe table — only offer sources
//     the agent can actually serve. `haproxy` requires the agent's
//     `access-log` or `logs` gear; `system` requires `logs`.
//  3. Fail-open: if the agent is unreachable, fall back to the legacy
//     `[haproxy, system]` pair so an existing deployment still works
//     through a transient outage.
//
// Historically this method returned the legacy pair unconditionally
// when no settings existed, which caused the Logs page on a box without
// a `logs` / `access-log` gear (e.g. the mjolnir agent in a distroless
// container) to immediately ask for haproxy logs the agent couldn't
// serve, producing the "Failed to..." JSON parse error the page used
// to render.
func (h *Handler) APILogSourcesHandler(w http.ResponseWriter, r *http.Request) {
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get enabled log sources from database
	sources, err := h.db.GetEnabledLogSourcesByServerID(boxID)
	if err != nil {
		h.logger.Error("Failed to get log sources", "error", err)
		http.Error(w, "Failed to get log sources", http.StatusInternalServerError)
		return
	}

	// If no explicit settings, derive defaults from the agent's probe
	// table so a Logs page on a box without those gears doesn't immediately
	// ask for sources the agent can't serve.
	if len(sources) == 0 {
		h.writeJSON(w, map[string]interface{}{
			"server_id":    boxID,
			"sources":      h.defaultLogSourcesForBox(boxID),
			"has_settings": false,
		})
		return
	}

	// Convert to response format
	sourcesResp := make([]map[string]string, 0, len(sources))
	for _, s := range sources {
		sourcesResp = append(sourcesResp, map[string]string{
			"name":         s.LogName,
			"display_name": s.DisplayName,
		})
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id":    boxID,
		"sources":      sourcesResp,
		"has_settings": true,
	})
}

// defaultLogSourcesForBox returns the default Logs-page source picker
// entries for a box, derived from the agent's probe table. Used when
// the operator hasn't saved explicit log-source settings.
//
// Heuristic:
//   - `haproxy` is offered when EITHER access-log OR logs gear is
//     available. access-log streams the HAProxy access log directly;
//     logs streams it via journalctl/file when journalctl is present.
//   - `system` is offered only when the `logs` gear is available, since
//     system journal access requires journalctl/tail.
//
// Fail-open posture (matches filterGearsByAgentCapabilities):
//   - Capabilities unreachable (agent down, no API key) → legacy pair.
//   - Agent didn't surface BOTH gears (older agent that pre-dates
//     access-log/logs probing) → legacy pair. We distinguish "the
//     agent didn't tell us about this gear" (caps.Has == false) from
//     "the agent said this gear is unavailable" (caps.Has == true,
//     IsAvailable == false) so a forward-compatibility gap doesn't
//     silently strip the source list.
//   - Agent surfaced both gears and reported both unavailable → empty
//     list. This is the only case the JS renders an empty dropdown
//     and skips the immediate fetch — better than 5xx storming.
func (h *Handler) defaultLogSourcesForBox(boxID string) []map[string]string {
	legacy := []map[string]string{
		{"name": "haproxy", "display_name": "HAProxy"},
		{"name": "system", "display_name": "System"},
	}
	caps, ok := h.getBoxCapabilities(boxID)
	if !ok {
		return legacy
	}

	knowsAccessLog := caps.Has("access-log")
	knowsLogs := caps.Has("logs")
	if !knowsAccessLog && !knowsLogs {
		// Agent doesn't surface either gear name yet — likely an older
		// agent that pre-dates this probe. Fail open.
		return legacy
	}

	hasAccessLog := knowsAccessLog && caps.IsAvailable("access-log")
	hasLogs := knowsLogs && caps.IsAvailable("logs")

	out := make([]map[string]string, 0, 2)
	if hasAccessLog || hasLogs {
		out = append(out, map[string]string{"name": "haproxy", "display_name": "HAProxy"})
	}
	if hasLogs {
		out = append(out, map[string]string{"name": "system", "display_name": "System"})
	}
	// Result may be empty here — agent explicitly reported both gears
	// unavailable. JS renders an empty dropdown rather than 5xx-storming
	// fetches for sources the agent can't serve.
	return out
}
