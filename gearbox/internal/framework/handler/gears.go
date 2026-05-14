package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// GearsPage serves the gears settings page.
func (h *Handler) GearsPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Check if user has permission to manage gears
	if !h.authManager.HasPermission(r, models.ComponentGears, models.PermissionManage) {
		http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
		return
	}

	// Get enabled servers from database (dynamic, includes newly created servers)
	servers := h.getEnabledServers()

	// Resolve which box to show config for: explicit ?server= wins, then
	// the persistent active-box cookie (the header pill), then first server.
	boxID := h.resolveBoxIDFromRequest(r)

	// boxID may be empty on a fresh install — the template renders an
	// "Add a Box" CTA in place of the per-box gear list. System-scoped
	// gears still render regardless.

	var plugins []database.Gear
	if boxID != "" {
		// Ensure default gears exist for this box
		if err := h.db.EnsureServerGears(boxID); err != nil {
			h.logger.Error("Failed to ensure server gears", "error", err)
		}

		plugins, err = h.db.GetGears(boxID)
		if err != nil {
			h.logger.Error("Failed to get gears", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		// Hide gears the agent's probe phase says aren't available on this
		// box (issue #71 item 2). Fails open if the agent is unreachable.
		plugins = h.filterGearsByAgentCapabilities(boxID, plugins)
	}

	// Load system-scoped (box-agnostic) gears so they render alongside the
	// per-box ones. They're keyed by ServerID = SystemServerID, so the
	// template / JS will route their toggle posts to that sentinel rather
	// than the currently selected box.
	systemGears, err := h.db.GetGears(database.SystemServerID)
	if err != nil {
		h.logger.Warn("failed to load system gears for settings page", "error", err)
		// non-fatal: just render box gears
	}

	// Get success/error messages from query params
	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")

	component := pages.GearsPage(user, servers, boxID, systemGears, plugins, successMsg, errorMsg)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render gears template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// dashboardGearToAgentGear maps a dashboard gear name to the agent gear
// whose probe verdict gates its visibility. Dashboard gears not in this map
// have no agent counterpart and are always shown (services & alerts are
// always-on dashboard concepts; certbot piggy-backs on certificates;
// system gears like home don't probe any host capability).
var dashboardGearToAgentGear = map[string]string{
	database.GearHAProxy:      "haproxy",
	database.GearLogs:         "logs",
	database.GearCertificates: "certificates",
	database.GearMetrics:      "metrics",
	database.GearTraffic:      "traffic",
	database.GearOSUpdates:    "updates",
}

// capabilitiesFetchTimeout caps the synchronous Gears-page → agent call
// so a dead agent doesn't stall page rendering for the full 30s default.
// 3s is enough for a healthy LAN agent, short enough that operators don't
// notice when an agent is gone.
const capabilitiesFetchTimeout = 3 * time.Second

// filterGearsByAgentCapabilities removes gears the agent has reported as
// not-installed / inaccessible / disabled. Fail-open on any error: when the
// agent is unreachable or running a version without the capabilities
// endpoint, we surface the full gear list so users aren't locked out of
// configuring boxes mid-upgrade. Non-mapped gears are always retained.
//
// Uses Handler.capabilities so repeated renders within the cache TTL share
// one upstream fetch instead of one round-trip per render.
func (h *Handler) filterGearsByAgentCapabilities(boxID string, gears []database.Gear) []database.Gear {
	caps, ok := h.getBoxCapabilities(boxID)
	if !ok {
		// Either not agent-backed, or fetch failed. Fail-open.
		return gears
	}

	out := make([]database.Gear, 0, len(gears))
	for _, g := range gears {
		agentName, gated := dashboardGearToAgentGear[g.Name]
		if !gated {
			out = append(out, g)
			continue
		}
		entry, present := caps.Entry(agentName)
		if !present {
			// Agent didn't report this gear at all — keep it (older agent
			// or gear not yet probe-aware).
			out = append(out, g)
			continue
		}
		if entry.IsAvailable() {
			out = append(out, g)
		}
	}
	return out
}

// getComponentForGear maps gear names to their corresponding permission component.
func getComponentForGear(gearName string) models.Component {
	switch gearName {
	case database.GearCertificates:
		return models.ComponentCertificates
	case database.GearMetrics:
		return models.ComponentMetrics
	case database.GearLogs:
		return models.ComponentLogs
	case database.GearServices:
		return models.ComponentServices
	case database.GearAlerts:
		return models.ComponentAlerts
	case database.GearOSUpdates:
		return models.ComponentOSUpdates
	default:
		return models.ComponentGears
	}
}

// GearDetailPage serves the detail/config page for a specific gear.
func (h *Handler) GearDetailPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get gear name first to determine which permission to check
	gearName := chi.URLParam(r, "name")

	// Check if user has permission to configure this specific component
	permComponent := getComponentForGear(gearName)
	if !h.authManager.HasPermission(r, permComponent, models.PermissionConfigure) {
		http.Error(w, "Forbidden: insufficient permissions to configure "+string(permComponent), http.StatusForbidden)
		return
	}

	// Resolve which box to load config for. Same precedence as GearsPage so
	// the configure link from the gears list lands on the right box.
	boxID := h.resolveBoxIDFromRequest(r)

	if boxID == "" {
		http.Error(w, "No servers configured", http.StatusBadRequest)
		return
	}

	// HAProxy has its own dedicated settings page
	if gearName == database.GearHAProxy {
		h.renderHAProxySettingsPage(w, r, user, boxID)
		return
	}

	gearItem, err := h.db.GetGear(boxID, gearName)
	if err != nil {
		h.logger.Error("failed to get gear", "gear", gearName, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if gearItem == nil {
		http.NotFound(w, r)
		return
	}

	// Get available services from agent for services gear
	var availableServices []string
	if gearName == database.GearServices {
		availableServices = h.getInstalledServices(boxID)
	}

	// Get available log sources from agent for logs gear
	var availableLogSources []pages.LogSourceInfo
	if gearName == database.GearLogs {
		availableLogSources = h.getAvailableLogSources(boxID)
	}

	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")

	// Get server name for display
	var serverName string
	if serverConfig, exists := h.getServerConfig(boxID); exists {
		serverName = serverConfig.Name
	}

	component := pages.GearDetailPage(user, boxID, serverName, gearItem, availableServices, availableLogSources, successMsg, errorMsg)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render gear detail template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// GearTogglePost handles toggling a gear on/off.
func (h *Handler) GearTogglePost(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		if isAJAXRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Unauthorized"}) //#nosec G104
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Check if user has permission to manage gears
	if !h.authManager.HasPermission(r, models.ComponentGears, models.PermissionManage) {
		if isAJAXRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Forbidden: insufficient permissions"}) //#nosec G104
			return
		}
		http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
		return
	}

	// Resolve which box to toggle for. Honors the active-box cookie when the
	// AJAX call omits ?server= (some legacy callers do).
	boxID := h.resolveBoxIDFromRequest(r)

	gearName := chi.URLParam(r, "name")

	// System-scoped gears are keyed by SystemServerID, not by any box.
	// Override boxID for the lookup so toggling Home (or any future system
	// gear) hits the right row even if the client posted ?server=<box-uuid>.
	// Keep the original boxID for the post-toggle redirect so the page comes
	// back focused on whichever box the user was viewing.
	gearServerID := boxID
	if database.IsSystemGear(gearName) {
		gearServerID = database.SystemServerID
	}

	redirectBase := "/settings/gears?server=" + url.QueryEscape(boxID)

	// Get current gear
	gearItem, err := h.db.GetGear(gearServerID, gearName)
	if err != nil {
		h.logger.Error("failed to get gear", "gear", gearName, "error", err)
		if isAJAXRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to get gear"}) //#nosec G104
			return
		}
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape("Failed to get gear"), http.StatusSeeOther)
		return
	}

	if gearItem == nil {
		if isAJAXRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Integration not found"}) //#nosec G104
			return
		}
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape("Integration not found"), http.StatusSeeOther)
		return
	}

	// Toggle the enabled state
	newEnabled := !gearItem.Enabled
	if err := h.db.SetGearEnabled(gearServerID, gearName, newEnabled, &user.ID); err != nil {
		h.logger.Error("failed to toggle gear", "gear", gearName, "error", err)
		if isAJAXRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to toggle gear"}) //#nosec G104
			return
		}
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape("Failed to toggle gear"), http.StatusSeeOther)
		return
	}

	// Log the action
	action := "enabled"
	if !newEnabled {
		action = "disabled"
	}
	h.logAudit(r, user.ID, "gear_toggled", gearItem.DisplayName+" "+action)

	// Return JSON for AJAX requests
	if isAJAXRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
			"success": true,
			"message": gearItem.DisplayName + " has been " + action,
			"enabled": newEnabled,
		})
		return
	}

	http.Redirect(w, r, redirectBase+"&success="+url.QueryEscape(gearItem.DisplayName+" has been "+action), http.StatusSeeOther)
}

// isAJAXRequest checks if the request is an XMLHttpRequest (AJAX)
func isAJAXRequest(r *http.Request) bool {
	return r.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

// GearUpdatePost handles updating gear configuration.
func (h *Handler) GearUpdatePost(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	gearName := chi.URLParam(r, "name")

	// Check if user has permission to configure this specific component
	permComponent := getComponentForGear(gearName)
	if !h.authManager.HasPermission(r, permComponent, models.PermissionConfigure) {
		http.Error(w, "Forbidden: insufficient permissions to configure "+string(permComponent), http.StatusForbidden)
		return
	}

	// Resolve which box to update config for. Same precedence as GearsPage.
	boxID := h.resolveBoxIDFromRequest(r)
	redirectBase := "/settings/gears/" + gearName + "?server=" + url.QueryEscape(boxID)

	// Compute wantsJSON up front so every error path below (parse failures,
	// gear lookup failures, validation errors, save failures) can respond
	// in the format the caller actually expects. AJAX auto-savers (services,
	// logs) request JSON; classic form POSTs accept HTML and get redirects.
	wantsJSON := strings.Contains(r.Header.Get("Accept"), "application/json")
	jsonError := func(status int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": msg})
	}

	if err := r.ParseForm(); err != nil {
		if wantsJSON {
			jsonError(http.StatusBadRequest, "Invalid form data")
			return
		}
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape("Invalid form data"), http.StatusSeeOther)
		return
	}

	// Get current gear
	gearItem, err := h.db.GetGear(boxID, gearName)
	if err != nil || gearItem == nil {
		if wantsJSON {
			jsonError(http.StatusNotFound, "Integration not found")
			return
		}
		http.Redirect(w, r, "/settings/gears?server="+url.QueryEscape(boxID)+"&error="+url.QueryEscape("Integration not found"), http.StatusSeeOther)
		return
	}

	// Handle config update based on gear type
	var newConfig json.RawMessage
	switch gearName {
	case database.GearServices:
		newConfig, err = h.parseServicesConfig(r)
	case database.GearLogs:
		// Logs have a special handler that saves to log_sources table.
		// The gear configure page POSTs each toggle change with
		// Accept: application/json and stays on the page, so respond
		// in kind instead of redirecting.
		err = h.saveLogSourcesConfig(boxID, r)
		if err != nil {
			if wantsJSON {
				jsonError(http.StatusInternalServerError, err.Error())
				return
			}
			http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		h.logAudit(r, user.ID, "gear_updated", gearItem.DisplayName+" configuration updated")
		if wantsJSON {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
			return
		}
		http.Redirect(w, r, redirectBase+"&success="+url.QueryEscape("Configuration saved successfully"), http.StatusSeeOther)
		return
	case database.GearCertificates:
		newConfig, err = h.parseCertificatesConfig(r)
	case database.GearMetrics:
		newConfig, err = h.parseMetricsConfig(r)
	case database.GearTraffic:
		newConfig, err = h.parseTrafficConfig(r)
	case database.GearAlerts:
		newConfig, err = h.parseAlertsConfig(r)
	case database.GearOSUpdates:
		newConfig, err = h.parseOSUpdatesConfig(r)
	default:
		// No special config, just save empty
		newConfig = json.RawMessage(`{}`)
	}

	if err != nil {
		if wantsJSON {
			jsonError(http.StatusBadRequest, err.Error())
			return
		}
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Update the gear
	if err := h.db.SetGearConfig(boxID, gearName, newConfig, &user.ID); err != nil {
		h.logger.Error("failed to update gear", "gear", gearName, "error", err)
		if wantsJSON {
			jsonError(http.StatusInternalServerError, "Failed to save configuration")
			return
		}
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape("Failed to save configuration"), http.StatusSeeOther)
		return
	}

	h.logAudit(r, user.ID, "gear_updated", gearItem.DisplayName+" configuration updated")
	if wantsJSON {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		return
	}
	http.Redirect(w, r, redirectBase+"&success="+url.QueryEscape("Configuration saved successfully"), http.StatusSeeOther)
}

// parseServicesConfig parses the services gear form.
func (h *Handler) parseServicesConfig(r *http.Request) (json.RawMessage, error) {
	services := r.Form["services"]
	showAll := r.FormValue("show_all") == "on"

	config := database.ServicesConfig{
		MonitoredServices: services,
		ShowAll:           showAll,
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// parseCertificatesConfig parses the certificates gear form (includes certbot settings).
func (h *Handler) parseCertificatesConfig(r *http.Request) (json.RawMessage, error) {
	certbotEnabled := r.FormValue("certbot_enabled_value") == "true"
	method := database.CertbotRenewalMethod(r.FormValue("renewal_method"))
	serviceName := strings.TrimSpace(r.FormValue("service_name"))
	cronPath := strings.TrimSpace(r.FormValue("cron_path"))

	// Validate method
	switch method {
	case database.CertbotMethodNone, database.CertbotMethodSystemd, database.CertbotMethodCron:
		// Valid
	default:
		method = database.CertbotMethodSystemd
	}

	// Default service name if empty and using systemd
	if serviceName == "" && method == database.CertbotMethodSystemd {
		serviceName = "certbot.timer"
	}

	config := database.CertificatesConfig{
		CertbotEnabled: certbotEnabled,
		Certbot: database.CertbotConfig{
			RenewalMethod: method,
			ServiceName:   serviceName,
			CronPath:      cronPath,
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// parseMetricsConfig parses the metrics gear form.
func (h *Handler) parseMetricsConfig(r *http.Request) (json.RawMessage, error) {
	storeHistory := r.FormValue("store_history_value") == "true"
	retentionType := database.MetricsRetentionType(r.FormValue("retention_type"))

	// Validate retention type
	switch retentionType {
	case database.MetricsRetentionByDays, database.MetricsRetentionBySize:
		// Valid
	default:
		retentionType = database.MetricsRetentionByDays
	}

	// Parse retention values
	retentionDays := 7 // default
	if days := r.FormValue("retention_days"); days != "" {
		if parsed, err := parseInt(days); err == nil && parsed > 0 {
			retentionDays = parsed
		}
	}

	retentionSizeMB := 100 // default
	if size := r.FormValue("retention_size_mb"); size != "" {
		if parsed, err := parseInt(size); err == nil && parsed > 0 {
			retentionSizeMB = parsed
		}
	}

	config := database.MetricsConfig{
		StoreHistory:    storeHistory,
		RetentionType:   retentionType,
		RetentionDays:   retentionDays,
		RetentionSizeMB: retentionSizeMB,
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// parseInt is a helper to parse integer form values.
func parseInt(s string) (int, error) {
	var v int
	err := json.Unmarshal([]byte(s), &v)
	return v, err
}

// parseTrafficConfig parses the traffic gear form.
func (h *Handler) parseTrafficConfig(r *http.Request) (json.RawMessage, error) {
	// Parse outbound bandwidth
	outboundBandwidth := 1000 // default 1000 Mbps
	if val := r.FormValue("outbound_bandwidth"); val != "" {
		if parsed, err := parseInt(val); err == nil && parsed > 0 {
			outboundBandwidth = parsed
		}
	}

	outboundUnit := database.BandwidthUnit(r.FormValue("outbound_bandwidth_unit"))
	switch outboundUnit {
	case database.BandwidthUnitMbps, database.BandwidthUnitGbps:
		// Valid
	default:
		outboundUnit = database.BandwidthUnitMbps
	}

	// Parse inbound bandwidth (optional - defaults to 0 which means use outbound)
	inboundBandwidth := 0
	if val := r.FormValue("inbound_bandwidth"); val != "" {
		if parsed, err := parseInt(val); err == nil && parsed >= 0 {
			inboundBandwidth = parsed
		}
	}

	inboundUnit := database.BandwidthUnit(r.FormValue("inbound_bandwidth_unit"))
	switch inboundUnit {
	case database.BandwidthUnitMbps, database.BandwidthUnitGbps:
		// Valid
	default:
		inboundUnit = database.BandwidthUnitMbps
	}

	// Parse internal bandwidth (optional - defaults to 0 which means use request-based visualization)
	internalBandwidth := 0
	if val := r.FormValue("internal_bandwidth"); val != "" {
		if parsed, err := parseInt(val); err == nil && parsed >= 0 {
			internalBandwidth = parsed
		}
	}

	internalUnit := database.BandwidthUnit(r.FormValue("internal_bandwidth_unit"))
	switch internalUnit {
	case database.BandwidthUnitMbps, database.BandwidthUnitGbps:
		// Valid
	default:
		internalUnit = database.BandwidthUnitGbps // Default to Gbps for internal networks
	}

	// Parse retention days
	retentionDays := 7 // default 7 days
	if val := r.FormValue("retention_days"); val != "" {
		if parsed, err := parseInt(val); err == nil && parsed > 0 {
			retentionDays = parsed
		}
	}

	config := database.TrafficConfig{
		OutboundBandwidth:     outboundBandwidth,
		OutboundBandwidthUnit: outboundUnit,
		InboundBandwidth:      inboundBandwidth,
		InboundBandwidthUnit:  inboundUnit,
		InternalBandwidth:     internalBandwidth,
		InternalBandwidthUnit: internalUnit,
		RetentionDays:         retentionDays,
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// parseAlertsConfig parses the alerts gear form.
func (h *Handler) parseAlertsConfig(r *http.Request) (json.RawMessage, error) {
	enableLiveStreaming := r.FormValue("enable_live_streaming_value") == "true"
	suppressAfterAck := r.FormValue("suppress_after_ack_value") == "true"

	// Parse suppress minutes
	suppressMinutes := 60 // default
	if minutes := r.FormValue("suppress_minutes"); minutes != "" {
		if parsed, err := parseInt(minutes); err == nil && parsed > 0 {
			suppressMinutes = parsed
		}
	}

	// Parse auto-resolve hours
	autoResolveHours := 0 // default disabled
	if hours := r.FormValue("auto_resolve_hours"); hours != "" {
		if parsed, err := parseInt(hours); err == nil && parsed >= 0 {
			autoResolveHours = parsed
		}
	}

	// Parse retention days
	retentionDays := 30 // default
	if days := r.FormValue("retention_days"); days != "" {
		if parsed, err := parseInt(days); err == nil && parsed > 0 {
			retentionDays = parsed
		}
	}

	config := database.AlertsConfig{
		RetentionDays:       retentionDays,
		AutoResolveHours:    autoResolveHours,
		SuppressAfterAck:    suppressAfterAck,
		SuppressMinutes:     suppressMinutes,
		EnableLiveStreaming: enableLiveStreaming,
		ShowRulesInAlerts:   false, // Rules are now in gear config
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// parseOSUpdatesConfig parses the OS updates gear form.
func (h *Handler) parseOSUpdatesConfig(r *http.Request) (json.RawMessage, error) {
	alertOnAvailable := r.FormValue("alert_on_available_value") == "true"
	createSnapshotBefore := r.FormValue("create_snapshot_before_value") == "true"
	showPythonTools := r.FormValue("show_pipx_value") == "true"

	// Parse check frequency
	checkFrequencyMinutes := 60 // default
	if freq := r.FormValue("check_frequency_minutes"); freq != "" {
		if parsed, err := parseInt(freq); err == nil && parsed >= 15 {
			checkFrequencyMinutes = parsed
		}
	}

	// Parse alert threshold
	alertThreshold := 0 // default: any
	if threshold := r.FormValue("alert_threshold"); threshold != "" {
		if parsed, err := parseInt(threshold); err == nil && parsed >= 0 {
			alertThreshold = parsed
		}
	}

	// Parse security alert threshold
	securityAlertThreshold := 0 // default: any
	if threshold := r.FormValue("security_alert_threshold"); threshold != "" {
		if parsed, err := parseInt(threshold); err == nil && parsed >= 0 {
			securityAlertThreshold = parsed
		}
	}

	// Parse history retention days
	historyRetentionDays := 90 // default
	if days := r.FormValue("history_retention_days"); days != "" {
		if parsed, err := parseInt(days); err == nil && parsed >= 7 {
			historyRetentionDays = parsed
		}
	}

	config := database.OSUpdatesConfig{
		CheckFrequencyMinutes:  checkFrequencyMinutes,
		AutoSecurityUpdates:    false, // Controlled via unattended-upgrades on server
		AutoReboot:             false, // Controlled via unattended-upgrades on server
		AlertOnAvailable:       alertOnAvailable,
		AlertThreshold:         alertThreshold,
		SecurityAlertThreshold: securityAlertThreshold,
		CreateSnapshotBefore:   createSnapshotBefore,
		ShowPythonTools:        showPythonTools,
		HistoryRetentionDays:   historyRetentionDays,
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// saveLogSourcesConfig saves log source selections to the database.
func (h *Handler) saveLogSourcesConfig(boxID string, r *http.Request) error {
	logSources := r.Form["log_sources"]

	// Build the list of log source settings
	var settings []database.LogSourceSettingInput
	for _, name := range logSources {
		settings = append(settings, database.LogSourceSettingInput{
			LogName:     name,
			DisplayName: pages.FormatLogSourceName(name),
		})
	}

	// Save to database
	return h.db.SetEnabledLogSourcesByServerID(boxID, settings)
}

// getAvailableLogSources fetches the list of available log sources from the agent
// and combines it with the current enabled settings from the database.
func (h *Handler) getAvailableLogSources(boxID string) []pages.LogSourceInfo {
	// Get server config for agent connection
	serverConfig, exists := h.getServerConfig(boxID)
	if !exists || !serverConfig.UsesAgentAPI() {
		h.logger.Info("agent API not configured for server", "server_id", boxID)
		return nil
	}

	// Fetch available log sources from agent
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	sourcesResp, err := agentClient.GetLogSources()
	if err != nil {
		h.logger.Error("failed to get log sources from agent for server", "server_id", boxID, "error", err)
		return nil
	}

	// Get enabled log sources from database
	enabledSources, err := h.db.GetEnabledLogSourcesByServerID(boxID)
	if err != nil {
		h.logger.Error("failed to get enabled log sources for server", "server_id", boxID, "error", err)
	}

	// Build the combined list
	return pages.BuildLogSourceInfoList(sourcesResp.Sources, enabledSources)
}

// getInstalledServices fetches the list of installed services from the agent,
// filtered to only include services from the curated suggestions list.
// Returns the intersection of available services on the target and the curated list.
func (h *Handler) getInstalledServices(boxID string) []string {
	// Get server config for agent connection
	serverConfig, exists := h.getServerConfig(boxID)
	if !exists || !serverConfig.UsesAgentAPI() {
		h.logger.Info("agent API not configured for server, falling back to curated list", "server_id", boxID)
		return h.getCuratedServicesList()
	}

	// Fetch available services from agent
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)
	availableResp, err := agentClient.GetAvailableServices()
	if err != nil {
		h.logger.Error("failed to get available services from agent for server", "server_id", boxID, "error", err)
		return h.getCuratedServicesList()
	}

	// Create a set of available services for quick lookup
	availableSet := make(map[string]bool)
	for _, svc := range availableResp.Services {
		availableSet[svc] = true
	}

	// Filter curated list to only include services that are available on the target
	curatedList := h.getCuratedServicesList()
	var filteredServices []string
	for _, svc := range curatedList {
		if availableSet[svc] {
			filteredServices = append(filteredServices, svc)
		}
	}

	// Sort alphabetically for consistent display
	sort.Strings(filteredServices)

	return filteredServices
}

// getCuratedServicesList returns a curated list of services commonly monitored with HAProxy.
func (h *Handler) getCuratedServicesList() []string {
	return []string{
		// HAProxy-related
		"haproxy",
		"gearbox-agent",

		// Security
		"fail2ban",
		"nftables",
		"firewalld",
		"ufw",

		// Certificate management
		"certbot.timer",
		"certbot-renew.timer",
		"letsencrypt.timer",

		// Web servers (sometimes co-located)
		"nginx",
		"apache2",
		"httpd",

		// Container runtimes
		"docker",
		"containerd",
		"podman",

		// Databases (backend services)
		"mysql",
		"mariadb",
		"postgresql",
		"redis",
		"memcached",
		"mongodb",

		// System services
		"ssh", // Ubuntu/Debian name; sshd is an alias that resolves to ssh.service
		"systemd-resolved",
		"cron",
		"rsyslog",
		"journald",

		// Monitoring
		"prometheus",
		"grafana-server",
		"node_exporter",

		// Message queues
		"rabbitmq-server",
		"kafka",

		// Other common services
		"consul",
		"vault",
		"traefik",
	}
}

// APIGearsHandler returns all plugins as JSON.
func (h *Handler) APIGearsHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

	if boxID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No server specified"}) //#nosec G104
		return
	}

	plugins, err := h.db.GetGears(boxID)
	if err != nil {
		h.logger.Error("Failed to get gears", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get gears"}) //#nosec G104
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(plugins) //#nosec G104
}

// APIGearStatusHandler returns enabled status for plugins.
func (h *Handler) APIGearStatusHandler(w http.ResponseWriter, r *http.Request) {
	boxID := h.resolveBoxIDFromRequest(r)

	if boxID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No server specified"}) //#nosec G104
		return
	}

	status, err := h.db.GetEnabledGears(boxID)
	if err != nil {
		h.logger.Error("Failed to get gear status", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get gear status"}) //#nosec G104
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status) //#nosec G104
}

// APIMetricsStorageStatsHandler returns storage statistics for metrics data.
func (h *Handler) APIMetricsStorageStatsHandler(w http.ResponseWriter, r *http.Request) {
	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No server specified"}) //#nosec G104
		return
	}

	stats, err := h.db.GetMetricsStorageStats(boxID)
	if err != nil {
		h.logger.Error("Failed to get metrics storage stats", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get storage stats"}) //#nosec G104
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats) //#nosec G104
}

// APIUpdateGearSortOrder updates the sort order for plugins.
func (h *Handler) APIUpdateGearSortOrder(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"}) //#nosec G104
		return
	}

	// Check if user has permission to manage gears
	if !h.authManager.HasPermission(r, models.ComponentGears, models.PermissionManage) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Forbidden"}) //#nosec G104
		return
	}

	boxID := h.resolveBoxIDFromRequest(r)

	if boxID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No server specified"}) //#nosec G104
		return
	}

	// Parse the order map from request body
	var orders map[string]int
	if err := json.NewDecoder(r.Body).Decode(&orders); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"}) //#nosec G104
		return
	}

	if err := h.db.UpdateGearSortOrders(boxID, orders, &user.ID); err != nil {
		h.logger.Error("Failed to update gear sort orders", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update sort order"}) //#nosec G104
		return
	}

	h.logAudit(r, user.ID, "gear_reordered", "Updated gear display order")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"success": "Order updated"}) //#nosec G104
}

// APIClearMetricsDataHandler clears all metrics data for a server.
func (h *Handler) APIClearMetricsDataHandler(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"}) //#nosec G104
		return
	}

	// Check if user has permission to manage gears (metrics is part of plugins)
	if !h.authManager.HasPermission(r, models.ComponentGears, models.PermissionManage) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Forbidden"}) //#nosec G104
		return
	}

	boxID := chi.URLParam(r, "boxID")
	if boxID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No server specified"}) //#nosec G104
		return
	}

	if err := h.db.ClearMetricsData(boxID); err != nil {
		h.logger.Error("Failed to clear metrics data", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to clear metrics data"}) //#nosec G104
		return
	}

	h.logAudit(r, user.ID, "metrics_cleared", "Cleared all metrics data for server "+boxID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"success": "Metrics data cleared"}) //#nosec G104
}

// renderHAProxySettingsPage renders the HAProxy-specific gear settings page.
func (h *Handler) renderHAProxySettingsPage(w http.ResponseWriter, r *http.Request, user *models.User, boxID string) {
	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")

	var serverName string
	if serverConfig, exists := h.getServerConfig(boxID); exists {
		serverName = serverConfig.Name
	}

	// Look up the BoxDB record to get the database ID for git config lookup
	server, _ := h.db.GetBoxByBoxID(boxID)

	var haproxyGit *database.BoxGitConfig
	if server != nil {
		haproxyGit, _ = h.db.GetBoxGitConfig(server.ID, database.ConfigTypeHAProxy)
	}

	component := pages.HAProxyGearSettingsPage(user, boxID, serverName, server, haproxyGit, successMsg, errorMsg)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render HAProxy settings template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// HAProxyGitSettingsSave saves the HAProxy git configuration from the gear settings page.
func (h *Handler) HAProxyGitSettingsSave(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !h.authManager.HasPermission(r, models.ComponentGears, models.PermissionConfigure) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	boxID := h.resolveBoxIDFromRequest(r)
	if boxID == "" {
		http.Error(w, "No server specified", http.StatusBadRequest)
		return
	}

	server, err := h.db.GetBoxByBoxID(boxID)
	if err != nil || server == nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	encryptor, err := h.getEncryptor()
	if err != nil {
		h.logger.Error("Failed to get encryptor", "error", err)
		http.Error(w, "Encryption error", http.StatusInternalServerError)
		return
	}

	haproxyConfig := &database.BoxGitConfig{
		HAProxyBoxID:        server.ID,
		ConfigType:          database.ConfigTypeHAProxy,
		GitRepoURL:          r.FormValue("haproxy_git_repo"),
		GitBranch:           r.FormValue("haproxy_git_branch"),
		GitFilePath:         r.FormValue("haproxy_git_path"),
		AutoApplyEnabled:    r.FormValue("haproxy_auto_apply") == "on",
		SyncIntervalMinutes: 60,
	}

	if pat := r.FormValue("haproxy_git_pat"); pat != "" {
		haproxyConfig.GitPATEncrypted, _ = encryptor.EncryptString(pat)
	} else {
		existing, _ := h.db.GetBoxGitConfig(server.ID, database.ConfigTypeHAProxy)
		if existing != nil {
			haproxyConfig.GitPATEncrypted = existing.GitPATEncrypted
		}
	}

	if haproxyConfig.GitRepoURL != "" {
		if err := h.db.SaveBoxGitConfig(haproxyConfig); err != nil {
			h.logger.Error("Failed to save HAProxy git config", "error", err)
			redirectURL := fmt.Sprintf("/settings/gears/haproxy?server=%s&error=%s", url.QueryEscape(boxID), url.QueryEscape("Failed to save git settings"))
			http.Redirect(w, r, redirectURL, http.StatusSeeOther)
			return
		}
	} else {
		_ = h.db.DeleteBoxGitConfig(server.ID, database.ConfigTypeHAProxy)
	}

	h.logAudit(r, user.ID, "haproxy_git_settings_update", fmt.Sprintf("Updated HAProxy git settings for server %s", server.Name))

	redirectURL := fmt.Sprintf("/settings/gears/haproxy?server=%s&success=%s", url.QueryEscape(boxID), url.QueryEscape("Git settings saved"))
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}
