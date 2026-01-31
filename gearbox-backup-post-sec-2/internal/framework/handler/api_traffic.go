package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// APITrafficAnalysisHandler returns traffic analysis data as JSON.
func (h *Handler) APITrafficAnalysisHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view traffic
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view traffic data", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	serverConfig, exists := h.getServerConfig(serverID)
	if !exists || !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	// Parse query params
	timeRange := r.URL.Query().Get("range")
	if timeRange == "" {
		timeRange = "24h"
	}

	// Get traffic data from agent
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	trafficData, err := agentClient.GetTraffic(1000, 25)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get traffic data", err))
		return
	}

	// Enrich traffic sources with GeoIP data (non-blocking, with fallback)
	h.enrichTrafficSourcesWithGeoIP(trafficData)

	// Also get traffic summary
	summary, err := agentClient.GetTrafficSummary()
	if err != nil {
		h.logger.Error("Failed to get traffic summary from agent", "error", err)
	}

	// Persist live traffic data to database for historical viewing
	// This runs in background to not slow down the API response
	go h.persistTrafficData(serverID, trafficData)

	// Try to get historical data from database
	var historicalSources []models.TrafficSourceStats
	var historicalBackends []models.TrafficBackendStats
	var historicalCountries []models.TrafficCountryStats
	var hourlyBreakdown []models.HourlyTrafficStats

	// Calculate start time based on range
	var since time.Time
	switch timeRange {
	case "1h":
		since = time.Now().Add(-1 * time.Hour)
	case "6h":
		since = time.Now().Add(-6 * time.Hour)
	case "24h":
		since = time.Now().Add(-24 * time.Hour)
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		since = time.Now().Add(-30 * 24 * time.Hour)
	default:
		since = time.Now().Add(-24 * time.Hour)
	}

	filter := &models.TrafficFilter{
		ServerID:  serverID,
		StartTime: since,
		EndTime:   time.Now(),
		Limit:     25,
	}

	historicalSources, err = h.db.GetTrafficSources(filter)
	if err != nil {
		h.logger.Error("Failed to get historical sources", "error", err)
	} else {
		h.logger.Debug("got historical sources from database", "count", len(historicalSources))
	}
	historicalBackends, err = h.db.GetTrafficByBackend(filter)
	if err != nil {
		h.logger.Error("Failed to get historical backends", "error", err)
	}
	historicalCountries, err = h.db.GetTrafficByCountry(filter)
	if err != nil {
		h.logger.Error("Failed to get historical countries", "error", err)
	}
	hourlyBreakdown, err = h.db.GetHourlyTraffic(filter)
	if err != nil {
		h.logger.Error("Failed to get hourly breakdown", "error", err)
	}

	// Get metadata for backend hostnames
	var backendHostnames map[string]string
	metadataResp, err := agentClient.GetMetadata()
	if err == nil && metadataResp != nil {
		backendHostnames = make(map[string]string)
		for _, backend := range metadataResp.Metadata.Backends {
			if backend.Hostname != "" {
				backendHostnames[backend.Name] = backend.Hostname
			}
		}
	}

	// Get traffic integration config for bandwidth settings
	trafficConfig, err := h.db.GetTrafficConfig(serverID)
	if err != nil {
		h.logger.Error("Failed to get traffic config", "error", err)
		// Use defaults
		trafficConfig = &database.TrafficConfig{
			OutboundBandwidth:     1000,
			OutboundBandwidthUnit: database.BandwidthUnitMbps,
		}
	}

	// Build response
	response := map[string]interface{}{
		"server_id":         serverID,
		"collected_at":      time.Now(),
		"time_range":        timeRange,
		"live_data":         trafficData,
		"summary":           summary,
		"backend_hostnames": backendHostnames,
		"bandwidth_config": map[string]interface{}{
			"outbound_bps": trafficConfig.GetOutboundBandwidthBps(),
			"inbound_bps":  trafficConfig.GetInboundBandwidthBps(),
			"internal_bps": trafficConfig.GetInternalBandwidthBps(),
		},
		"historical": map[string]interface{}{
			"top_sources":      historicalSources,
			"top_backends":     historicalBackends,
			"top_countries":    historicalCountries,
			"hourly_breakdown": hourlyBreakdown,
		},
	}

	h.writeJSON(w, response)
}

// APITrafficSourcesHandler returns traffic sources (top IPs) as JSON.
func (h *Handler) APITrafficSourcesHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view traffic
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view traffic data", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Parse query params
	timeRange := r.URL.Query().Get("range")
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	// Calculate start time based on range
	var since time.Time
	switch timeRange {
	case "1h":
		since = time.Now().Add(-1 * time.Hour)
	case "6h":
		since = time.Now().Add(-6 * time.Hour)
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		since = time.Now().Add(-30 * 24 * time.Hour)
	default:
		since = time.Now().Add(-24 * time.Hour)
	}

	filter := &models.TrafficFilter{
		ServerID:  serverID,
		StartTime: since,
		EndTime:   time.Now(),
		Limit:     limit,
		SortBy:    r.URL.Query().Get("sort_by"),
		SortOrder: r.URL.Query().Get("sort_order"),
	}

	sources, err := h.db.GetTrafficSources(filter)
	if err != nil {
		h.logger.Error("Failed to get traffic sources", "error", err)
		http.Error(w, "Failed to get traffic sources", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"server_id": serverID,
		"sources":   sources,
		"count":     len(sources),
	}

	h.writeJSON(w, response)
}

// APITrafficNetworkHandler returns traffic network visualization data.
func (h *Handler) APITrafficNetworkHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view traffic
	if !h.authManager.HasPermission(r, models.ComponentMetrics, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view traffic data", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	serverConfig, exists := h.getServerConfig(serverID)
	if !exists || !serverConfig.UsesAgentAPI() {
		http.Error(w, "Agent API not configured", http.StatusServiceUnavailable)
		return
	}

	// Get traffic data from agent
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	trafficData, err := agentClient.GetTraffic(100, 15)
	if err != nil {
		h.logger.Error("Failed to get traffic data for network view", "error", err)
		http.Error(w, "Failed to get traffic data", http.StatusInternalServerError)
		return
	}

	// Enrich traffic sources with GeoIP data
	h.enrichTrafficSourcesWithGeoIP(trafficData)

	// Build network nodes and edges
	nodes, edges := buildNetworkVisualizationFromLiveData(trafficData)

	response := map[string]interface{}{
		"server_id":    serverID,
		"collected_at": time.Now(),
		"nodes":        nodes,
		"edges":        edges,
	}

	h.writeJSON(w, response)
}

// buildNetworkVisualizationFromLiveData creates visualization data from live traffic.
func buildNetworkVisualizationFromLiveData(traffic *agent.TrafficResponse) ([]models.NetworkNode, []models.NetworkEdge) {
	var nodes []models.NetworkNode
	var edges []models.NetworkEdge

	// Find max values for sizing
	var maxRequests int64
	for _, s := range traffic.TopByRequests {
		if s.Requests > maxRequests {
			maxRequests = s.Requests
		}
	}
	for _, b := range traffic.BackendTraffic {
		if b.TotalRequests > maxRequests {
			maxRequests = b.TotalRequests
		}
	}
	if maxRequests == 0 {
		maxRequests = 1 // Prevent division by zero
	}

	// Add HAProxy central node
	frontendNode := models.NetworkNode{
		ID:       "haproxy",
		Type:     "frontend",
		Label:    "HAProxy",
		SubLabel: "Load Balancer",
		Size:     40,
		Color:    "#3b82f6",
		Status:   "healthy",
		X:        400,
		Y:        300,
	}
	nodes = append(nodes, frontendNode)

	// Add source IP nodes (left side)
	for i, source := range traffic.TopByRequests {
		if i >= 10 {
			break
		}

		size := 15.0 + 25.0*(float64(source.Requests)/float64(maxRequests))

		node := models.NetworkNode{
			ID:        fmt.Sprintf("source_%d", i),
			Type:      "source",
			Label:     source.IPAddress,
			SubLabel:  fmt.Sprintf("%d req", source.Requests),
			IPAddress: source.IPAddress,
			Size:      size,
			Color:     "#8b5cf6", // purple
			Status:    "healthy",
			Requests:  source.Requests,
			BytesIn:   source.BytesIn,
			BytesOut:  source.BytesOut,
			X:         100,
			Y:         float64(80 + i*55),
		}
		nodes = append(nodes, node)

		// Edge from source to HAProxy
		weight := 1.0 + 4.0*(float64(source.Requests)/float64(maxRequests))
		edge := models.NetworkEdge{
			ID:       fmt.Sprintf("edge_source_%d_haproxy", i),
			Source:   fmt.Sprintf("source_%d", i),
			Target:   "haproxy",
			Weight:   weight,
			Requests: source.Requests,
			BytesIn:  source.BytesIn,
			BytesOut: source.BytesOut,
			Animated: source.Requests > maxRequests/3,
			Color:    "#22c55e",
		}
		edges = append(edges, edge)
	}

	// Add backend nodes (right side)
	for i, backend := range traffic.BackendTraffic {
		if i >= 10 {
			break
		}

		size := 20.0 + 30.0*(float64(backend.TotalRequests)/float64(maxRequests))

		status := "healthy"
		color := "#22c55e"
		if backend.ErrorRate > 5 {
			status = "warning"
			color = "#f59e0b"
		}
		if backend.ErrorRate > 20 {
			status = "error"
			color = "#ef4444"
		}

		node := models.NetworkNode{
			ID:         fmt.Sprintf("backend_%s", backend.Name),
			Type:       "backend",
			Label:      backend.Name,
			SubLabel:   fmt.Sprintf("%d req", backend.TotalRequests),
			Size:       size,
			Color:      color,
			Status:     status,
			Requests:   backend.TotalRequests,
			BytesIn:    backend.BytesIn,
			BytesOut:   backend.BytesOut,
			ErrorRate:  backend.ErrorRate,
			AvgLatency: backend.AvgResponseTime,
			X:          700,
			Y:          float64(80 + i*55),
		}
		nodes = append(nodes, node)

		// Edge from HAProxy to backend
		weight := 1.0 + 4.0*(float64(backend.TotalRequests)/float64(maxRequests))
		edgeColor := "#22c55e"
		if backend.ErrorRate > 5 {
			edgeColor = "#f59e0b"
		}
		if backend.ErrorRate > 20 {
			edgeColor = "#ef4444"
		}

		edge := models.NetworkEdge{
			ID:         fmt.Sprintf("edge_haproxy_backend_%s", backend.Name),
			Source:     "haproxy",
			Target:     fmt.Sprintf("backend_%s", backend.Name),
			Weight:     weight,
			Requests:   backend.TotalRequests,
			BytesIn:    backend.BytesIn,
			BytesOut:   backend.BytesOut,
			AvgLatency: backend.AvgResponseTime,
			ErrorRate:  backend.ErrorRate,
			Animated:   backend.TotalRequests > maxRequests/3,
			Color:      edgeColor,
		}
		edges = append(edges, edge)
	}

	return nodes, edges
}

// enrichTrafficSourcesWithGeoIP adds GeoIP data to traffic sources.
// This is done on the monitor backend to avoid blocking the agent.
// On any lookup failure, "Location not found" is used as the fallback.
func (h *Handler) enrichTrafficSourcesWithGeoIP(trafficData *agent.TrafficResponse) {
	if trafficData == nil || h.geoipClient == nil {
		return
	}

	// Collect all unique IPs from all source lists
	ipSet := make(map[string]bool)
	for _, s := range trafficData.Sources {
		if s.IPAddress != "" {
			ipSet[s.IPAddress] = true
		}
	}
	for _, s := range trafficData.TopByRequests {
		if s.IPAddress != "" {
			ipSet[s.IPAddress] = true
		}
	}
	for _, s := range trafficData.TopByBytes {
		if s.IPAddress != "" {
			ipSet[s.IPAddress] = true
		}
	}
	for _, s := range trafficData.TopByConnections {
		if s.IPAddress != "" {
			ipSet[s.IPAddress] = true
		}
	}

	// Convert to slice for batch lookup
	ips := make([]string, 0, len(ipSet))
	for ip := range ipSet {
		ips = append(ips, ip)
	}

	// Do batch lookup (handles caching and concurrency internally)
	geoData := h.geoipClient.LookupBatch(ips)

	// Enrich Sources
	for i := range trafficData.Sources {
		if loc, ok := geoData[trafficData.Sources[i].IPAddress]; ok && loc != nil {
			trafficData.Sources[i].Country = loc.Country
			trafficData.Sources[i].CountryCode = loc.CountryCode
		}
	}

	// Enrich TopByRequests
	for i := range trafficData.TopByRequests {
		if loc, ok := geoData[trafficData.TopByRequests[i].IPAddress]; ok && loc != nil {
			trafficData.TopByRequests[i].Country = loc.Country
			trafficData.TopByRequests[i].CountryCode = loc.CountryCode
		}
	}

	// Enrich TopByBytes
	for i := range trafficData.TopByBytes {
		if loc, ok := geoData[trafficData.TopByBytes[i].IPAddress]; ok && loc != nil {
			trafficData.TopByBytes[i].Country = loc.Country
			trafficData.TopByBytes[i].CountryCode = loc.CountryCode
		}
	}

	// Enrich TopByConnections
	for i := range trafficData.TopByConnections {
		if loc, ok := geoData[trafficData.TopByConnections[i].IPAddress]; ok && loc != nil {
			trafficData.TopByConnections[i].Country = loc.Country
			trafficData.TopByConnections[i].CountryCode = loc.CountryCode
		}
	}
}

// persistTrafficData saves live traffic data to the database for historical viewing.
// This runs in background so it doesn't slow down API responses.
// HAProxy provides cumulative counters, so we calculate deltas to get per-period values.
func (h *Handler) persistTrafficData(serverID string, trafficData *agent.TrafficResponse) {
	if trafficData == nil {
		return
	}

	now := time.Now()
	bucketTime := now.Truncate(time.Minute) // Bucket by minute for aggregation

	h.trafficDeltaMu.Lock()
	defer h.trafficDeltaMu.Unlock()

	// Build a map of source IP -> backend from ConnectionFlows for better relationships
	sourceBackendMap := make(map[string]string)
	for _, flow := range trafficData.ConnectionFlows {
		if flow.SourceIP != "" && flow.Backend != "" {
			sourceBackendMap[flow.SourceIP] = flow.Backend
		}
	}

	// Convert agent traffic sources to flows and save
	// Use TopByRequests as primary source (Sources field is often empty)
	// Calculate deltas from previous values (HAProxy gives cumulative counters)
	var flows []models.TrafficFlow

	// Process TopByRequests - these have the most reliable aggregate data per IP
	for _, source := range trafficData.TopByRequests {
		// Skip empty or invalid entries
		if source.IPAddress == "" || source.IPAddress == "unix" || source.IPAddress == "localhost" {
			continue
		}
		if strings.HasPrefix(source.IPAddress, "/") || strings.HasPrefix(source.IPAddress, "127.") {
			continue
		}

		// Get backend from source data or connection flow mapping
		backendName := source.Backend
		if backendName == "" {
			backendName = sourceBackendMap[source.IPAddress]
		}
		if backendName == "" {
			backendName = "_unknown"
		}

		// Calculate delta from previous snapshot
		key := serverID + ":" + source.IPAddress + ":" + backendName
		currentRequests := source.Requests + source.HTTPRequestRate
		currentBytesIn := source.BytesIn
		currentBytesOut := source.BytesOut

		var deltaRequests, deltaBytesIn, deltaBytesOut int64
		if prev, exists := h.prevSourceData[key]; exists {
			// Calculate delta (handle counter resets)
			if currentRequests >= prev.Requests {
				deltaRequests = currentRequests - prev.Requests
			} else {
				deltaRequests = currentRequests // Counter was reset
			}
			if currentBytesIn >= prev.BytesIn {
				deltaBytesIn = currentBytesIn - prev.BytesIn
			} else {
				deltaBytesIn = currentBytesIn
			}
			if currentBytesOut >= prev.BytesOut {
				deltaBytesOut = currentBytesOut - prev.BytesOut
			} else {
				deltaBytesOut = currentBytesOut
			}
		}
		// If no previous data, delta is 0 (don't record initial cumulative spike)

		// Store current values for next delta calculation
		h.prevSourceData[key] = &trafficSourceSnapshot{
			Requests: currentRequests,
			BytesIn:  currentBytesIn,
			BytesOut: currentBytesOut,
			LastSeen: now,
		}

		// Only save if there's actual activity
		if deltaRequests > 0 || deltaBytesIn > 0 || deltaBytesOut > 0 {
			flow := models.TrafficFlow{
				ServerID:        serverID,
				SourceIP:        source.IPAddress,
				Country:         source.Country,
				CountryCode:     source.CountryCode,
				BackendName:     backendName,
				FrontendName:    source.Frontend,
				Timestamp:       bucketTime,
				RequestCount:    deltaRequests,
				BytesIn:         deltaBytesIn,
				BytesOut:        deltaBytesOut,
				AvgResponseTime: 0, // Not available from stick tables
			}

			// Set defaults for country if not available
			if flow.Country == "" {
				flow.Country = "Unknown"
			}
			if flow.CountryCode == "" {
				flow.CountryCode = "XX"
			}

			flows = append(flows, flow)
		}
	}

	// Save source flows to database
	if len(flows) > 0 {
		if err := h.db.SaveTrafficFlows(flows); err != nil {
			h.logger.Error("Failed to persist traffic flows", "error", err)
		}
	}

	// Save backend traffic statistics as aggregate records with delta calculation
	for _, bt := range trafficData.BackendTraffic {
		key := serverID + ":" + bt.Name

		// Calculate deltas from previous values
		var deltaRequests, deltaBytesIn, deltaBytesOut int64
		var delta2xx, delta3xx, delta4xx, delta5xx int64

		if prev, exists := h.prevBackendData[key]; exists {
			// Calculate deltas (handle counter resets)
			if bt.TotalRequests >= prev.TotalRequests {
				deltaRequests = bt.TotalRequests - prev.TotalRequests
			} else {
				deltaRequests = bt.TotalRequests
			}
			if bt.BytesIn >= prev.BytesIn {
				deltaBytesIn = bt.BytesIn - prev.BytesIn
			} else {
				deltaBytesIn = bt.BytesIn
			}
			if bt.BytesOut >= prev.BytesOut {
				deltaBytesOut = bt.BytesOut - prev.BytesOut
			} else {
				deltaBytesOut = bt.BytesOut
			}
			if bt.Response2xx >= prev.Response2xx {
				delta2xx = bt.Response2xx - prev.Response2xx
			} else {
				delta2xx = bt.Response2xx
			}
			if bt.Response3xx >= prev.Response3xx {
				delta3xx = bt.Response3xx - prev.Response3xx
			} else {
				delta3xx = bt.Response3xx
			}
			if bt.Response4xx >= prev.Response4xx {
				delta4xx = bt.Response4xx - prev.Response4xx
			} else {
				delta4xx = bt.Response4xx
			}
			if bt.Response5xx >= prev.Response5xx {
				delta5xx = bt.Response5xx - prev.Response5xx
			} else {
				delta5xx = bt.Response5xx
			}
		}
		// If no previous data, delta is 0 (don't record initial cumulative spike)

		// Store current values for next delta calculation
		h.prevBackendData[key] = &trafficBackendSnapshot{
			TotalRequests: bt.TotalRequests,
			BytesIn:       bt.BytesIn,
			BytesOut:      bt.BytesOut,
			Response2xx:   bt.Response2xx,
			Response3xx:   bt.Response3xx,
			Response4xx:   bt.Response4xx,
			Response5xx:   bt.Response5xx,
			LastSeen:      now,
		}

		// Only save if there's actual activity
		if deltaRequests > 0 || deltaBytesIn > 0 || deltaBytesOut > 0 {
			flow := models.TrafficFlow{
				ServerID:        serverID,
				SourceIP:        "_aggregate", // Special marker for aggregate data
				Country:         "Aggregate",
				CountryCode:     "AG",
				BackendName:     bt.Name,
				FrontendName:    "",
				Timestamp:       bucketTime,
				RequestCount:    deltaRequests,
				BytesIn:         deltaBytesIn,
				BytesOut:        deltaBytesOut,
				Response2xx:     delta2xx,
				Response3xx:     delta3xx,
				Response4xx:     delta4xx,
				Response5xx:     delta5xx,
				AvgResponseTime: bt.AvgResponseTime,
			}
			if err := h.db.SaveTrafficFlow(&flow); err != nil {
				h.logger.Warn("failed to persist backend traffic", "backend", bt.Name, "error", err)
			}
		}
	}

	// Cleanup stale source snapshots (not seen in 10 minutes)
	// This prevents memory leaks from ephemeral sources
	staleThreshold := now.Add(-10 * time.Minute)
	for key, snap := range h.prevSourceData {
		if snap.LastSeen.Before(staleThreshold) {
			delete(h.prevSourceData, key)
		}
	}
}
