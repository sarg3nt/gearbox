// Package handler — per-source metrics endpoints for the dashboard's
// Metrics gear multi-source pivot (issue #91 phases 4 / 5 / 7).
//
// Two endpoints live here:
//
//	GET /api/{boxID}/metrics/source/{source}/summary
//	    → latest snapshot + a small time-series for the per-source
//	      chart cards (request rate, status-class breakdown if
//	      emitted, active connections for nginx/Apache, etc.).
//
//	GET /api/{boxID}/metrics/source/{source}/log-errors
//	    → recent parsed access-log records via the agent's
//	      /api/v1/access-log/{source}/recent endpoint. This is the
//	      Phase-5 refactor: the existing /metrics/log-errors handler
//	      hard-coded "haproxy"; the new per-source endpoint pushes
//	      parsing agent-side and lets the Error Insights panel ask
//	      for any supported source.
//
// The HAProxy-only /metrics/log-errors endpoint stays as-is for now
// to avoid breaking older dashboard clients mid-rollout; the new
// front-end calls into /source/{source}/log-errors instead.
package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// supportedNonHAProxySources is the dispatch set for /source/{source}/*.
// HAProxy is intentionally excluded — its data flows through the
// existing /metrics/summary + /metrics/log-errors endpoints which
// read from stats_history / GetLogs, not from source_stats.
//
// Kept in sync with collector.SupportedSources() (the agent-side
// scraper polls the same set). A new source landing requires both
// lists to grow in lockstep; the per-source handler test covers
// each entry to keep them honest.
var supportedNonHAProxySources = map[string]struct{}{
	sourceNginx:   {},
	sourceApache:  {},
	sourceCaddy:   {},
	sourceTraefik: {},
}

// sourceSummaryResponse is the JSON the per-source summary endpoint
// emits. Latest is nil-able so the dashboard can distinguish "the
// source isn't probed Available yet" (Latest == nil, Available ==
// false) from "the source is Available but has zero traffic"
// (Latest non-nil, RequestsTotal == 0).
type sourceSummaryResponse struct {
	ServerID    string                         `json:"server_id"`
	Source      string                         `json:"source"`
	SourceLabel string                         `json:"source_label"`
	Available   bool                           `json:"available"`
	Reason      string                         `json:"reason,omitempty"`
	Range       string                         `json:"range"`
	WindowStart time.Time                      `json:"window_start"`
	WindowEnd   time.Time                      `json:"window_end"`
	Latest      *database.SourceStatsSnapshot  `json:"latest,omitempty"`
	History     []database.SourceStatsSnapshot `json:"history"`
}

// APIMetricsSourceSummaryHandler returns the per-source summary for
// one box. The dashboard's per-source chart card calls this to
// render its KPIs + time-series — one endpoint per render, response
// is small enough that compression / sparse-time-series tricks
// aren't worth it yet.
func (h *Handler) APIMetricsSourceSummaryHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}
	source := chi.URLParam(r, "source")
	if _, ok := supportedNonHAProxySources[source]; !ok {
		http.Error(w, "Unsupported source — valid: nginx, apache, caddy, traefik", http.StatusNotFound)
		return
	}

	since, until, label := metricsRangeToTimes(r.URL.Query().Get("range"))
	resp := sourceSummaryResponse{
		ServerID:    boxID,
		Source:      source,
		SourceLabel: sourceLabel(source),
		Range:       label,
		WindowStart: since,
		WindowEnd:   until,
		History:     []database.SourceStatsSnapshot{},
	}

	// Available is gated by the agent's capability report — a source
	// that hasn't probed Available shouldn't surface as "available
	// with zero data" because that's misleading; render the empty
	// state with a reason instead so the dashboard can show a
	// "install nginx / enable stub_status / ..." hint.
	if caps, ok := h.getBoxCapabilities(boxID); ok && caps != nil {
		if entry, present := caps.Entry(source); present {
			resp.Available = entry.IsAvailable()
			if !resp.Available {
				resp.Reason = entry.Reason
			}
		}
	}

	// Even when the capability says not-Available we still try to
	// surface the most recent snapshot — sometimes the capability
	// table is stale (just-installed nginx, agent restart pending)
	// and the dashboard would rather show data than nothing.
	if latest, err := h.db.GetLatestSourceStats(boxID, source); err == nil {
		resp.Latest = latest
	} else if !errors.Is(err, database.ErrNoSourceStats) {
		h.logger.Error("source summary: latest fetch",
			"server_id", boxID, "source", source, "error", err)
	}

	history, err := h.db.GetSourceStatsRange(boxID, source, since, until, 2000)
	if err != nil {
		h.logger.Error("source summary: range fetch",
			"server_id", boxID, "source", source, "error", err)
	} else {
		resp.History = history
	}

	h.writeJSON(w, resp)
}

// APIMetricsSourceLogErrorsHandler is the Phase-5 deliverable: the
// dashboard now asks the agent for parsed log records rather than
// shipping raw lines to be parsed client-side. The endpoint proxies
// to the agent's /api/v1/access-log/{source}/recent and surfaces
// the structured records the agent emits.
//
// Backwards-compatibility note: the existing /metrics/log-errors
// endpoint (HAProxy only, dashboard-side parser) stays around — see
// the package doc-comment. Both endpoints will be reconciled when
// the front-end fully migrates to the per-source UI; until then,
// keeping both keeps an in-flight rollout safe.
func (h *Handler) APIMetricsSourceLogErrorsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	// As with the HAProxy variant, lacking `logs:view` is a soft
	// "panel unavailable" rather than a hard 403 — the metrics
	// page's overall view permission lets the operator on the
	// page; this sub-panel needs the more specific scope.
	if !h.authManager.HasPermission(r, "logs", "view") {
		h.writeJSON(w, map[string]any{
			"server_id":   chi.URLParam(r, "boxID"),
			"source":      chi.URLParam(r, "source"),
			"available":   false,
			"reason":      "Logs permission required to correlate access-log lines with metrics.",
			"match_count": 0,
			"records":     []agent.AccessLogRecord{},
		})
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}
	source := chi.URLParam(r, "source")
	// HAProxy IS allowed here even though we excluded it from the
	// summary handler — the agent's access-log endpoint supports
	// "haproxy" as a profile, and the dashboard's Phase-5 plan is
	// to migrate the Error Insights panel onto this endpoint for
	// EVERY source including HAProxy. The summary handler stays
	// HAProxy-excluded because HAProxy summary still flows through
	// stats_history; log parsing is the Phase-5 unification point.
	if source != sourceHAProxy {
		if _, ok := supportedNonHAProxySources[source]; !ok {
			http.Error(w, "Unsupported source — valid: haproxy, nginx, apache, caddy, traefik", http.StatusNotFound)
			return
		}
	}

	serverConfig, exists := h.getServerConfig(boxID)
	if !exists || !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	statusMin := 500
	if s, err := strconv.Atoi(r.URL.Query().Get("status_min")); err == nil && s >= 0 && s <= 599 {
		statusMin = s
	}
	limit := 500
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 10000 {
		limit = l
	}

	client := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	resp, err := client.GetAccessLogRecent(source, statusMin, limit)
	if err != nil {
		// Don't 500 — return an empty envelope with a hint. The
		// metrics page shouldn't break because logs are
		// unavailable on this box.
		h.writeJSON(w, map[string]any{
			"server_id":   boxID,
			"source":      source,
			"available":   false,
			"reason":      err.Error(),
			"match_count": 0,
			"records":     []agent.AccessLogRecord{},
		})
		return
	}

	// Pass the agent's envelope through directly — Available /
	// Reason / MatchCount / Records all map 1:1 to what the
	// dashboard expects. Tack on server_id for the front-end's
	// debugging convenience.
	h.writeJSON(w, map[string]any{
		"server_id":   boxID,
		"source":      resp.Source,
		"profile":     resp.Profile,
		"path":        resp.Path,
		"available":   resp.Available,
		"reason":      resp.Reason,
		"match_count": resp.MatchCount,
		"records":     resp.Records,
	})
}
