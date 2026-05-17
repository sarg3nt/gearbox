package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
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
// Resolution chain (issue #112 Phase 2):
//
//  1. **Structured resources**: when the access-log gear publishes
//     `log_sources` in its agent-side Resources map (one entry per
//     discovered web-server access log, with name + display_name +
//     path), the dashboard surfaces that list verbatim. The agent is
//     the single source of truth for which sources exist and what
//     they're called.
//  2. **Capability heuristic** (legacy fallback for pre-Phase-2
//     agents): if the agent doesn't publish a `log_sources` resource,
//     derive defaults from gear availability flags:
//       - `haproxy` is offered when EITHER access-log OR logs gear is
//         available
//       - `system` is offered only when the `logs` gear is available
//  3. **Fail-open** to the legacy `[haproxy, system]` pair when
//     capabilities are unreachable (agent down, no API key) OR the
//     agent doesn't surface either gear at all (older agent). Per
//     the #116 fix we distinguish caps.Has() == false ("the agent
//     doesn't know about this gear yet") from
//     caps.IsAvailable() == false ("the agent says it's unavailable")
//     so a forward-compat gap doesn't silently strip the dropdown.
//  4. **Empty list**: only when the agent explicitly reports both
//     gears unavailable. The JS renders an empty dropdown instead
//     of 5xx-storming fetches.
func (h *Handler) defaultLogSourcesForBox(boxID string) []map[string]string {
	legacy := []map[string]string{
		{"name": "haproxy", "display_name": "HAProxy"},
		{"name": "system", "display_name": "System"},
	}
	caps, ok := h.getBoxCapabilities(boxID)
	if !ok {
		return legacy
	}

	// Phase 2 preferred path: agent advertises its log sources directly.
	if sources, ok := logSourcesFromResources(caps); ok {
		return sources
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

// logSourcesFromResources extracts the access-log gear's `log_sources`
// resource (issue #112 Phase 2) and reshapes it into the dropdown
// envelope the Logs page expects: `[{name, display_name}, …]`. The
// `path` the agent publishes is intentionally dropped at this layer
// because the dashboard doesn't expose log paths to the browser.
//
// Returns (nil, false) when:
//   - the access-log gear isn't surfaced by the agent;
//   - the `log_sources` key is missing from its Resources map;
//   - the resource isn't shaped as the expected JSON array of objects.
//
// Returning false in those cases lets defaultLogSourcesForBox fall
// through to the older capability heuristic instead of producing an
// empty dropdown.
//
// JSON-decoding note: encoding/json decodes a JSON array as []any
// where each member object is map[string]any. We also tolerate the
// Go-typed []map[string]string shape so tests can build the
// CapabilitiesResponse with concrete types without coercing through
// json.Marshal/Unmarshal.
func logSourcesFromResources(caps *agent.BoxCapabilities) ([]map[string]string, bool) {
	raw, ok := caps.Resource("access-log", "log_sources")
	if !ok {
		return nil, false
	}

	var entries []map[string]string
	switch v := raw.(type) {
	case []any:
		entries = make([]map[string]string, 0, len(v))
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := obj["name"].(string)
			display, _ := obj["display_name"].(string)
			if name == "" || display == "" {
				continue
			}
			entries = append(entries, map[string]string{
				"name":         name,
				"display_name": display,
			})
		}
	case []map[string]string:
		entries = make([]map[string]string, 0, len(v))
		for _, item := range v {
			if item["name"] == "" || item["display_name"] == "" {
				continue
			}
			entries = append(entries, map[string]string{
				"name":         item["name"],
				"display_name": item["display_name"],
			})
		}
	default:
		return nil, false
	}

	if len(entries) == 0 {
		return nil, false
	}
	return entries, true
}
