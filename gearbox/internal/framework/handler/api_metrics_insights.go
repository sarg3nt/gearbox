// Package handler — api_metrics_insights.go
//
// HTTP handlers backing the Metrics gear's "insights" surface: KPI band,
// Error Insights panel, per-backend drill-down drawer, and the related-log
// snippet pulled from the agent. These endpoints exist to turn the
// historical metrics page from "pretty graphs you can't act on" into
// something a user can use to answer "what is broken, and where?".
package handler

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// metricsRangeToTimes parses the ?range= query param ("5m", "30m", "1h",
// "6h", "24h", "3d", "7d") and returns (since, until, label). until is
// always "now". Unknown values fall back to 24h to match the page selector.
func metricsRangeToTimes(rangeStr string) (time.Time, time.Time, string) {
	now := time.Now()
	switch rangeStr {
	case "5m":
		return now.Add(-5 * time.Minute), now, "5m"
	case "30m":
		return now.Add(-30 * time.Minute), now, "30m"
	case "1h":
		return now.Add(-1 * time.Hour), now, "1h"
	case "6h":
		return now.Add(-6 * time.Hour), now, "6h"
	case "24h":
		return now.Add(-24 * time.Hour), now, "24h"
	case "3d":
		return now.Add(-72 * time.Hour), now, "3d"
	case "7d":
		return now.Add(-7 * 24 * time.Hour), now, "7d"
	}
	return now.Add(-24 * time.Hour), now, "24h"
}

// kpiCard is one square in the KPI band at the top of the metrics page.
type kpiCard struct {
	Key         string    `json:"key"`           // stable identifier (e.g. "requests")
	Label       string    `json:"label"`         // human-readable
	Value       float64   `json:"value"`         // current value
	Unit        string    `json:"unit"`          // "ms", "%", "" etc.
	Decimals    int       `json:"decimals"`      // display precision
	PrevValue   float64   `json:"prev_value"`    // same metric, previous window of equal length
	DeltaPct    float64   `json:"delta_pct"`     // (value - prev) / prev * 100
	Status      string    `json:"status"`        // "good" | "warn" | "bad"
	Sparkline   []float64 `json:"sparkline"`     // 30 points spanning the current window
	IsCount     bool      `json:"is_count"`      // hint to UI: render as integer count
	Description string    `json:"description"`   // tooltip body
	Source      string    `json:"source"`        // metric source: "haproxy", "host", … (drives the UI badge)
	SourceLabel string    `json:"source_label"`  // human-readable form ("HAProxy", "Host") for the badge
}

// APIMetricsSummaryHandler returns the KPI band data for the top of the
// metrics page. It runs on top of the existing stats_history table — no
// new collection required.
//
// Each KPI has a sparkline (downsampled to ~30 points) plus a delta vs.
// the same-length window immediately preceding the current one.
func (h *Handler) APIMetricsSummaryHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	since, until, label := metricsRangeToTimes(r.URL.Query().Get("range"))
	prevSince := since.Add(-until.Sub(since))

	// Capability-aware gating: only emit HAProxy KPI cards when the agent
	// surfaces an available haproxy gear. Hosts without HAProxy still get
	// host-level cards below. Missing/unknown capabilities fail open
	// (haproxyAvailable = true) so a flaky agent doesn't strip the band
	// down to nothing.
	haproxyAvailable := true
	if caps, ok := h.getBoxCapabilities(boxID); ok && caps != nil {
		if entry, present := caps.Entry(sourceHAProxy); present {
			haproxyAvailable = entry.IsAvailable()
		}
	}

	cards := []kpiCard{}

	if haproxyAvailable {
		haproxyCards, err := h.haproxyKPICards(boxID, since, until, prevSince)
		if err != nil {
			h.logger.Error("metrics summary: haproxy KPIs", "error", err)
			http.Error(w, "Failed to load stats history", http.StatusInternalServerError)
			return
		}
		cards = append(cards, haproxyCards...)
	}

	hostCards, err := h.hostKPICards(boxID, since, until, prevSince)
	if err != nil {
		h.logger.Error("metrics summary: host KPIs", "error", err)
		// Host KPIs are best-effort: an empty system_metrics_history table
		// shouldn't blow up the band when HAProxy data is fine. Log and
		// return whatever HAProxy gave us.
	} else {
		cards = append(cards, hostCards...)
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id":         boxID,
		"range":             label,
		"window_start":      since,
		"window_end":        until,
		"prev_window_start": prevSince,
		"haproxy_available": haproxyAvailable,
		"cards":             cards,
	})
}

// haproxyKPICards returns the HAProxy-sourced KPI cards (requests/min,
// response time, error rate, 5xx count, active sessions, backend health).
// Computed from the dashboard's stats_history table — the source-of-truth
// is HAProxy's own admin socket / stats URL via the agent.
func (h *Handler) haproxyKPICards(boxID string, since, until, prevSince time.Time) ([]kpiCard, error) {
	curr, err := h.db.GetStatsHistory(boxID, since, 5000)
	if err != nil {
		return nil, err
	}
	prevRaw, err := h.db.GetStatsHistory(boxID, prevSince, 5000)
	if err != nil {
		return nil, err
	}
	// GetStatsHistory takes a lower bound only, so prevRaw spans both the
	// previous AND current windows. Trim to entries strictly before `since`
	// so the prev-window aggregates (max counter, avg response time, …)
	// reflect the prior window in isolation — otherwise every delta vs.
	// prior is ~0% because we'd be comparing each window to its own union
	// with the current window.
	prev := prevRaw[:0]
	for _, s := range prevRaw {
		if s.CollectedAt.Before(since) {
			prev = append(prev, s)
		}
	}

	cards := []kpiCard{}

	currReqMax := maxStatField(curr, func(s database.StatsSnapshot) float64 { return float64(s.TotalRequests) })
	prevReqMax := maxStatField(prev, func(s database.StatsSnapshot) float64 { return float64(s.TotalRequests) })
	currReqRate := perMinute(currReqMax, since, until)
	prevReqRate := perMinute(prevReqMax, prevSince, since)
	cards = append(cards, kpiCard{
		Key:         "requests",
		Label:       "Requests / min",
		Value:       currReqRate,
		Unit:        "/min",
		Decimals:    1,
		PrevValue:   prevReqRate,
		DeltaPct:    pctDelta(currReqRate, prevReqRate),
		Status:      "good",
		Sparkline:   statSparkline(curr, func(s database.StatsSnapshot) float64 { return float64(s.TotalRequests) }, 30),
		Description: "Total HTTP requests served per minute, averaged across the window. Reported by HAProxy stats.",
		Source:      sourceHAProxy,
		SourceLabel: sourceLabelHAProxy,
	})

	currRT := avgPositiveStat(curr, func(s database.StatsSnapshot) float64 { return float64(s.AvgResponseTime) })
	prevRT := avgPositiveStat(prev, func(s database.StatsSnapshot) float64 { return float64(s.AvgResponseTime) })
	rtStatus := "good"
	if currRT > 250 {
		rtStatus = "warn"
	}
	if currRT > 1000 {
		rtStatus = "bad"
	}
	cards = append(cards, kpiCard{
		Key:         "response_time",
		Label:       "Avg Response",
		Value:       currRT,
		Unit:        "ms",
		Decimals:    0,
		PrevValue:   prevRT,
		DeltaPct:    pctDelta(currRT, prevRT),
		Status:      rtStatus,
		Sparkline:   statSparkline(curr, func(s database.StatsSnapshot) float64 { return float64(s.AvgResponseTime) }, 30),
		Description: "Mean backend response time across all backends. Reported by HAProxy stats.",
		Source:      sourceHAProxy,
		SourceLabel: sourceLabelHAProxy,
	})

	curr5xx, curr4xx, currTotalResp := h.errorTotals(boxID, since, until)
	prev5xx, prev4xx, prevTotalResp := h.errorTotals(boxID, prevSince, since)
	currErrPct := pctOf(curr4xx+curr5xx, currTotalResp)
	prevErrPct := pctOf(prev4xx+prev5xx, prevTotalResp)
	errStatus := "good"
	if currErrPct >= 1 {
		errStatus = "warn"
	}
	if currErrPct >= 5 {
		errStatus = "bad"
	}
	cards = append(cards, kpiCard{
		Key:         "error_rate",
		Label:       "Error Rate",
		Value:       currErrPct,
		Unit:        "%",
		Decimals:    2,
		PrevValue:   prevErrPct,
		DeltaPct:    pctDelta(currErrPct, prevErrPct),
		Status:      errStatus,
		Sparkline:   h.errorRateSparkline(boxID, since, until, 30),
		Description: "Percentage of requests with 4xx or 5xx status. Click the Error Insights panel below to see which backend / source is responsible.",
		Source:      sourceHAProxy,
		SourceLabel: sourceLabelHAProxy,
	})

	cards = append(cards, kpiCard{
		Key:         "errors_5xx",
		Label:       "5xx Errors",
		Value:       float64(curr5xx),
		Unit:        "",
		Decimals:    0,
		IsCount:     true,
		PrevValue:   float64(prev5xx),
		DeltaPct:    pctDelta(float64(curr5xx), float64(prev5xx)),
		Status:      statusFromCount(curr5xx, 5, 50),
		Sparkline:   h.errorCountSparkline(boxID, since, until, 30),
		Description: "Total HTTP 5xx server errors in this window. Reported by HAProxy stats.",
		Source:      sourceHAProxy,
		SourceLabel: sourceLabelHAProxy,
	})

	currSess := lastStat(curr, func(s database.StatsSnapshot) float64 { return float64(s.TotalSessions) })
	prevSess := avgPositiveStat(prev, func(s database.StatsSnapshot) float64 { return float64(s.TotalSessions) })
	cards = append(cards, kpiCard{
		Key:         "sessions",
		Label:       "Active Sessions",
		Value:       currSess,
		Unit:        "",
		Decimals:    0,
		IsCount:     true,
		PrevValue:   prevSess,
		DeltaPct:    pctDelta(currSess, prevSess),
		Status:      "good",
		Sparkline:   statSparkline(curr, func(s database.StatsSnapshot) float64 { return float64(s.TotalSessions) }, 30),
		Description: "Current concurrent sessions across all backends. Reported by HAProxy stats.",
		Source:      sourceHAProxy,
		SourceLabel: sourceLabelHAProxy,
	})

	currHealthy := lastStat(curr, func(s database.StatsSnapshot) float64 { return float64(s.HealthyServers) })
	currTotal := lastStat(curr, func(s database.StatsSnapshot) float64 { return float64(s.TotalServers) })
	healthStatus := "good"
	if currTotal > 0 {
		ratio := currHealthy / currTotal
		if ratio < 1 {
			healthStatus = "warn"
		}
		if ratio < 0.6 {
			healthStatus = "bad"
		}
	}
	cards = append(cards, kpiCard{
		Key:         "health",
		Label:       "Healthy Backends",
		Value:       currHealthy,
		PrevValue:   currTotal,
		Unit:        "",
		Decimals:    0,
		IsCount:     true,
		Status:      healthStatus,
		Sparkline:   statSparkline(curr, func(s database.StatsSnapshot) float64 { return float64(s.HealthyServers) }, 30),
		Description: "Live healthy backend servers out of total configured. Reported by HAProxy stats.",
		Source:      sourceHAProxy,
		SourceLabel: sourceLabelHAProxy,
	})

	return cards, nil
}

// hostKPICards returns the host-level KPI cards (memory, disk, load,
// network). Computed from the dashboard's system_metrics_history table.
// Host metrics are produced by the agent on every box, regardless of
// whether HAProxy or any other proxy is installed — so these cards always
// render when data is available. CPU%, uptime, and failed-systemd
// counters aren't persisted in system_metrics_history today; they'll
// follow when the collector starts saving them.
func (h *Handler) hostKPICards(boxID string, since, until, prevSince time.Time) ([]kpiCard, error) {
	curr, err := h.db.GetSystemMetricsHistory(boxID, since, 5000)
	if err != nil {
		return nil, err
	}
	prevRaw, err := h.db.GetSystemMetricsHistory(boxID, prevSince, 5000)
	if err != nil {
		return nil, err
	}
	prev := prevRaw[:0]
	for _, s := range prevRaw {
		if s.CollectedAt.Before(since) {
			prev = append(prev, s)
		}
	}

	cards := []kpiCard{}

	// Memory %
	currMem := avgPositiveSysField(curr, func(s database.SystemMetricsSnapshot) float64 { return s.MemoryUsagePercent })
	prevMem := avgPositiveSysField(prev, func(s database.SystemMetricsSnapshot) float64 { return s.MemoryUsagePercent })
	memStatus := "good"
	if currMem >= 80 {
		memStatus = "warn"
	}
	if currMem >= 95 {
		memStatus = "bad"
	}
	cards = append(cards, kpiCard{
		Key:         "memory",
		Label:       "Memory",
		Value:       currMem,
		Unit:        "%",
		Decimals:    1,
		PrevValue:   prevMem,
		DeltaPct:    pctDelta(currMem, prevMem),
		Status:      memStatus,
		Sparkline:   sysSparkline(curr, func(s database.SystemMetricsSnapshot) float64 { return s.MemoryUsagePercent }, 30),
		Description: "Average memory usage in this window. Reported by the agent's host collector.",
		Source:      sourceHost,
		SourceLabel: sourceLabelHost,
	})

	// Disk %
	currDisk := avgPositiveSysField(curr, func(s database.SystemMetricsSnapshot) float64 { return s.DiskUsagePercent })
	prevDisk := avgPositiveSysField(prev, func(s database.SystemMetricsSnapshot) float64 { return s.DiskUsagePercent })
	diskStatus := "good"
	if currDisk >= 80 {
		diskStatus = "warn"
	}
	if currDisk >= 95 {
		diskStatus = "bad"
	}
	cards = append(cards, kpiCard{
		Key:         "disk",
		Label:       "Disk",
		Value:       currDisk,
		Unit:        "%",
		Decimals:    1,
		PrevValue:   prevDisk,
		DeltaPct:    pctDelta(currDisk, prevDisk),
		Status:      diskStatus,
		Sparkline:   sysSparkline(curr, func(s database.SystemMetricsSnapshot) float64 { return s.DiskUsagePercent }, 30),
		Description: "Average disk usage on the root filesystem. Reported by the agent's host collector.",
		Source:      sourceHost,
		SourceLabel: sourceLabelHost,
	})

	// Load average (1 min)
	currLoad := avgPositiveSysField(curr, func(s database.SystemMetricsSnapshot) float64 { return s.LoadAverage1 })
	prevLoad := avgPositiveSysField(prev, func(s database.SystemMetricsSnapshot) float64 { return s.LoadAverage1 })
	cards = append(cards, kpiCard{
		Key:         "load_1m",
		Label:       "Load (1m)",
		Value:       currLoad,
		Unit:        "",
		Decimals:    2,
		PrevValue:   prevLoad,
		DeltaPct:    pctDelta(currLoad, prevLoad),
		Status:      "good",
		Sparkline:   sysSparkline(curr, func(s database.SystemMetricsSnapshot) float64 { return s.LoadAverage1 }, 30),
		Description: "1-minute load average across the window. Reported by the agent's host collector.",
		Source:      sourceHost,
		SourceLabel: sourceLabelHost,
	})

	return cards, nil
}

// Source identifiers + display labels. Defined as constants so handlers
// and templates stay aligned and a typo doesn't silently drop a card from
// the filtered view.
const (
	sourceHAProxy      = "haproxy"
	sourceLabelHAProxy = "HAProxy"
	sourceHost         = "host"
	sourceLabelHost    = "Host"
)

// APIMetricsErrorBreakdownHandler powers the "Error Insights" panel: a
// three-column view of top-N backends, source IPs, and countries
// responsible for 4xx/5xx in the window.
func (h *Handler) APIMetricsErrorBreakdownHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	since, until, label := metricsRangeToTimes(r.URL.Query().Get("range"))

	limit := 10
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	backends, err := h.db.GetTopErrorBackends(boxID, since, until, limit)
	if err != nil {
		h.logger.Error("error breakdown: backends", "error", err)
		http.Error(w, "Failed to load error breakdown", http.StatusInternalServerError)
		return
	}
	sources, err := h.db.GetTopErrorSources(boxID, since, until, limit)
	if err != nil {
		h.logger.Error("error breakdown: sources", "error", err)
		http.Error(w, "Failed to load error breakdown", http.StatusInternalServerError)
		return
	}
	countries, err := h.db.GetTopErrorCountries(boxID, since, until, limit)
	if err != nil {
		h.logger.Error("error breakdown: countries", "error", err)
		http.Error(w, "Failed to load error breakdown", http.StatusInternalServerError)
		return
	}

	// Enrich source rows with GeoIP if available — handy for sources whose
	// `traffic_sources` row hasn't been written yet (very-recent IPs).
	if h.geoipClient != nil {
		ips := make([]string, 0, len(sources))
		for _, s := range sources {
			if s.SourceIP != "" && (s.Country == "" || s.Country == "Unknown") {
				ips = append(ips, s.SourceIP)
			}
		}
		if len(ips) > 0 {
			geo := h.geoipClient.LookupBatch(ips)
			for i := range sources {
				if loc, ok := geo[sources[i].SourceIP]; ok && loc != nil {
					if sources[i].Country == "" || sources[i].Country == "Unknown" {
						sources[i].Country = loc.Country
					}
					if sources[i].CountryCode == "" || sources[i].CountryCode == "XX" {
						sources[i].CountryCode = loc.CountryCode
					}
				}
			}
		}
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id": boxID,
		"range":     label,
		"backends":  backends,
		"sources":   sources,
		"countries": countries,
	})
}

// APIMetricsBackendDetailsHandler powers the drill-down drawer for one
// backend. Returns the time-series for that backend (so the drawer can
// draw mini-charts) plus the top source IPs hitting it.
func (h *Handler) APIMetricsBackendDetailsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	boxID := chi.URLParam(r, "boxID")
	backendName := chi.URLParam(r, "backendName")
	if boxID == "" || backendName == "" {
		http.Error(w, "Server ID and backend name required", http.StatusBadRequest)
		return
	}

	since, until, label := metricsRangeToTimes(r.URL.Query().Get("range"))

	timeseries, err := h.db.GetBackendTimeSeries(boxID, backendName, since, until, 2000)
	if err != nil {
		h.logger.Error("backend details: timeseries", "error", err)
		http.Error(w, "Failed to load backend timeseries", http.StatusInternalServerError)
		return
	}

	topSources, err := h.db.GetTopSourcesForBackend(boxID, backendName, since, until, 10)
	if err != nil {
		h.logger.Error("backend details: sources", "error", err)
		http.Error(w, "Failed to load top sources", http.StatusInternalServerError)
		return
	}

	// Also pull per-backend snapshot history (from backend_history table) so
	// we can show backend-server health over time.
	bhSince := since
	bh, err := h.db.GetBackendHistory(boxID, backendName, bhSince, 2000)
	if err != nil {
		h.logger.Warn("backend details: backend_history fallback", "error", err)
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id":      boxID,
		"backend_name":   backendName,
		"range":          label,
		"window_start":   since,
		"window_end":     until,
		"timeseries":     timeseries,
		"top_sources":    topSources,
		"backend_history": bh,
	})
}

// haproxyLogLine is a parsed structured form of a recent HAProxy access log
// line. We don't try to be exhaustive — the goal is "give the user enough
// to find what's failing without reading the raw log".
type haproxyLogLine struct {
	Timestamp  string `json:"timestamp"`
	SourceIP   string `json:"source_ip"`
	Frontend   string `json:"frontend"`
	Backend    string `json:"backend"`
	Server     string `json:"server"`
	StatusCode int    `json:"status_code"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Raw        string `json:"raw"`
}

// HAProxy HTTP log format:
//   <date> <host> haproxy[pid]: <client_ip>:<port> [<accept_date>] <frontend>~ <backend>/<server> <Tq>/<Tw>/<Tc>/<Tr>/<Tt> <status> <bytes_read> ... "<method> <path> HTTP/1.x"
//
// We parse defensively — fields can vary by HAProxy version and logformat.
var (
	reHAProxyStatus = regexp.MustCompile(`\s(\d{3})\s+\d+\s`)
	reHAProxyReq    = regexp.MustCompile(`"([A-Z]+)\s+([^\s"]+)`)
	reHAProxyBkSvr  = regexp.MustCompile(`\s([A-Za-z0-9_.\-]+)/([A-Za-z0-9_.\-]+)\s+\d+\/\-?\d+\/\-?\d+\/\-?\d+\/\-?\d+\s`)
	reHAProxyClient = regexp.MustCompile(`(?:^|\s)(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}|[0-9a-fA-F:]+):\d+\s+\[`)
	reHAProxyDate   = regexp.MustCompile(`\[([^\]]+)\]`)
)

// parseHAProxyLogLine pulls the interesting fields out of one access-log
// line. Returns nil if the line doesn't look like an HTTP access log.
func parseHAProxyLogLine(raw string) *haproxyLogLine {
	statusMatch := reHAProxyStatus.FindStringSubmatch(raw)
	if len(statusMatch) < 2 {
		return nil
	}
	status, _ := strconv.Atoi(statusMatch[1])
	if status < 100 || status > 599 {
		return nil
	}

	line := &haproxyLogLine{StatusCode: status, Raw: raw}

	if m := reHAProxyDate.FindStringSubmatch(raw); len(m) > 1 {
		line.Timestamp = m[1]
	}
	if m := reHAProxyClient.FindStringSubmatch(raw); len(m) > 1 {
		line.SourceIP = m[1]
	}
	if m := reHAProxyBkSvr.FindStringSubmatch(raw); len(m) > 2 {
		line.Backend = m[1]
		line.Server = m[2]
	}
	if m := reHAProxyReq.FindStringSubmatch(raw); len(m) > 2 {
		line.Method = m[1]
		line.Path = m[2]
	}

	// Trim raw line to a reasonable length — full HAProxy lines can be 2+ KB
	// and we don't want to ship that to the browser.
	if len(line.Raw) > 1024 {
		line.Raw = line.Raw[:1024] + "…"
	}

	return line
}

// APIMetricsLogErrorsHandler fetches recent HAProxy log lines from the
// agent, parses status codes, and returns the lines >= 400. The dashboard
// surfaces these on the metrics page so the user can see exactly which
// URLs / source IPs are responsible for the error rate.
//
// Query params:
//   - status_min: minimum status code to return (default 500)
//   - lines:      number of recent log lines to fetch from the agent (default 2000, max 10000)
//   - backend:    optional filter — only return lines whose backend matches
func (h *Handler) APIMetricsLogErrorsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view metrics", http.StatusForbidden)
		return
	}
	// Lack of `logs:view` is the only "you're allowed on the metrics page,
	// but this one sub-panel needs more permission" case we have. Return a
	// soft envelope rather than 403 so the drawer can render the documented
	// "logs unavailable" hint instead of the generic "fetch failed" error.
	if !h.authManager.HasPermission(r, "logs", "view") {
		h.writeJSON(w, map[string]interface{}{
			"server_id":  chi.URLParam(r, "boxID"),
			"available":  false,
			"reason":     "Logs permission required to correlate access-log lines with metrics.",
			"matchCount": 0,
			"matches":    []haproxyLogLine{},
		})
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	serverConfig, exists := h.getServerConfig(boxID)
	if !exists || !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	statusMin := 500
	if s, err := strconv.Atoi(r.URL.Query().Get("status_min")); err == nil && s >= 100 && s <= 599 {
		statusMin = s
	}
	lines := 2000
	if l, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && l > 0 && l <= 10000 {
		lines = l
	}
	backendFilter := strings.TrimSpace(r.URL.Query().Get("backend"))

	client := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	resp, err := client.GetLogs("haproxy", lines)
	if err != nil {
		// Don't 500 — return an empty result with a hint. The metrics page
		// shouldn't break just because logs are unavailable on this box.
		h.writeJSON(w, map[string]interface{}{
			"server_id":  boxID,
			"available":  false,
			"reason":     err.Error(),
			"matches":    []haproxyLogLine{},
			"matchCount": 0,
		})
		return
	}

	matches := make([]haproxyLogLine, 0, 64)
	for _, raw := range strings.Split(resp.Content, "\n") {
		raw = strings.TrimRight(raw, "\r")
		if raw == "" {
			continue
		}
		parsed := parseHAProxyLogLine(raw)
		if parsed == nil || parsed.StatusCode < statusMin {
			continue
		}
		if backendFilter != "" && !strings.EqualFold(parsed.Backend, backendFilter) {
			continue
		}
		matches = append(matches, *parsed)
	}

	// Most-recent first (HAProxy logs are appended in chronological order).
	// We display newest-on-top in the UI.
	for i, j := 0, len(matches)-1; i < j; i, j = i+1, j-1 {
		matches[i], matches[j] = matches[j], matches[i]
	}

	// Cap the response — 500 lines of error context is plenty for a UI list.
	if len(matches) > 500 {
		matches = matches[:500]
	}

	h.writeJSON(w, map[string]interface{}{
		"server_id":  boxID,
		"available":  true,
		"status_min": statusMin,
		"backend":    backendFilter,
		"matchCount": len(matches),
		"matches":    matches,
	})
}

// Helpers (KPI math, sparkline downsampling, error totals) live in
// api_metrics_insights_helpers.go.
