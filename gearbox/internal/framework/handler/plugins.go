package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/dashboard"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
	"gopkg.in/yaml.v3"
)

// PluginsPage serves the plugins settings page.
func (h *Handler) PluginsPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Check if user has permission to manage plugins
	if !h.authManager.HasPermission(r, models.ComponentPlugins, models.PermissionManage) {
		http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
		return
	}

	// Get enabled servers from database (dynamic, includes newly created servers)
	servers := h.getEnabledServers()

	// Get server ID from query param, default to first server
	boxID := r.URL.Query().Get("server")
	if boxID == "" && len(servers) > 0 {
		boxID = servers[0].ID
	}

	if boxID == "" {
		http.Error(w, "No servers configured", http.StatusBadRequest)
		return
	}

	// Ensure default plugins exist for this server
	if err := h.db.EnsureServerPlugins(boxID); err != nil {
		h.logger.Error("Failed to ensure server plugins", "error", err)
	}

	plugins, err := h.db.GetPlugins(boxID)
	if err != nil {
		h.logger.Error("Failed to get plugins", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get success/error messages from query params
	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")

	component := pages.PluginsPage(user, servers, boxID, plugins, successMsg, errorMsg)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render plugins template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// getComponentForPlugin maps plugin names to their corresponding permission component.
func getComponentForPlugin(pluginName string) models.Component {
	switch pluginName {
	case database.PluginCertificates:
		return models.ComponentCertificates
	case database.PluginMetrics:
		return models.ComponentMetrics
	case database.PluginLogs:
		return models.ComponentLogs
	case database.PluginServices:
		return models.ComponentServices
	case database.PluginAlerts:
		return models.ComponentAlerts
	case database.PluginOSUpdates:
		return models.ComponentOSUpdates
	default:
		return models.ComponentPlugins
	}
}

// PluginDetailPage serves the detail/config page for a specific plugin.
func (h *Handler) PluginDetailPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get plugin name first to determine which permission to check
	pluginName := chi.URLParam(r, "name")

	// Check if user has permission to configure this specific component
	permComponent := getComponentForPlugin(pluginName)
	if !h.authManager.HasPermission(r, permComponent, models.PermissionConfigure) {
		http.Error(w, "Forbidden: insufficient permissions to configure "+string(permComponent), http.StatusForbidden)
		return
	}

	// Get enabled servers from database (dynamic, includes newly created servers)
	servers := h.getEnabledServers()

	// Get server ID from query param
	boxID := r.URL.Query().Get("server")
	if boxID == "" && len(servers) > 0 {
		boxID = servers[0].ID
	}

	if boxID == "" {
		http.Error(w, "No servers configured", http.StatusBadRequest)
		return
	}
	plugin, err := h.db.GetPlugin(boxID, pluginName)
	if err != nil {
		h.logger.Error("failed to get plugin", "plugin", pluginName, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if plugin == nil {
		http.NotFound(w, r)
		return
	}

	// Get available services from agent for services plugin
	var availableServices []string
	if pluginName == database.PluginServices {
		availableServices = h.getInstalledServices(boxID)
	}

	// Get available log sources from agent for logs plugin
	var availableLogSources []pages.LogSourceInfo
	if pluginName == database.PluginLogs {
		availableLogSources = h.getAvailableLogSources(boxID)
	}

	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")

	// Get server name for display
	var serverName string
	if serverConfig, exists := h.getServerConfig(boxID); exists {
		serverName = serverConfig.Name
	}

	component := pages.PluginDetailPage(user, boxID, serverName, plugin, availableServices, availableLogSources, successMsg, errorMsg)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render plugin detail template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// PluginTogglePost handles toggling an plugin on/off.
func (h *Handler) PluginTogglePost(w http.ResponseWriter, r *http.Request) {
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

	// Check if user has permission to manage plugins
	if !h.authManager.HasPermission(r, models.ComponentPlugins, models.PermissionManage) {
		if isAJAXRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Forbidden: insufficient permissions"}) //#nosec G104
			return
		}
		http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
		return
	}

	// Get enabled servers from database (dynamic, includes newly created servers)
	servers := h.getEnabledServers()

	// Get server ID from query param
	boxID := r.URL.Query().Get("server")
	if boxID == "" && len(servers) > 0 {
		boxID = servers[0].ID
	}

	pluginName := chi.URLParam(r, "name")
	redirectBase := "/settings/plugins?server=" + url.QueryEscape(boxID)

	// Get current plugin
	plugin, err := h.db.GetPlugin(boxID, pluginName)
	if err != nil {
		h.logger.Error("failed to get plugin", "plugin", pluginName, "error", err)
		if isAJAXRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to get plugin"}) //#nosec G104
			return
		}
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape("Failed to get plugin"), http.StatusSeeOther)
		return
	}

	if plugin == nil {
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
	newEnabled := !plugin.Enabled
	if err := h.db.SetPluginEnabled(boxID, pluginName, newEnabled, &user.ID); err != nil {
		h.logger.Error("failed to toggle plugin", "plugin", pluginName, "error", err)
		if isAJAXRequest(r) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "Failed to toggle plugin"}) //#nosec G104
			return
		}
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape("Failed to toggle plugin"), http.StatusSeeOther)
		return
	}

	// If plugin was enabled, deploy its predefined dashboards
	if newEnabled && h.dashboardStorage != nil {
		if err := h.deployPluginDashboards(pluginName); err != nil {
			h.logger.Warn("failed to deploy plugin dashboards", "plugin", pluginName, "error", err)
			// Don't fail the whole operation, just log the warning
		}
	}

	// Log the action
	action := "enabled"
	if !newEnabled {
		action = "disabled"
	}
	h.logAudit(r, user.ID, "plugin_toggled", plugin.DisplayName+" "+action)

	// Return JSON for AJAX requests
	if isAJAXRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //#nosec G104
			"success": true,
			"message": plugin.DisplayName + " has been " + action,
			"enabled": newEnabled,
		})
		return
	}

	http.Redirect(w, r, redirectBase+"&success="+url.QueryEscape(plugin.DisplayName+" has been "+action), http.StatusSeeOther)
}

// isAJAXRequest checks if the request is an XMLHttpRequest (AJAX)
func isAJAXRequest(r *http.Request) bool {
	return r.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

// PluginUpdatePost handles updating plugin configuration.
func (h *Handler) PluginUpdatePost(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	pluginName := chi.URLParam(r, "name")

	// Check if user has permission to configure this specific component
	permComponent := getComponentForPlugin(pluginName)
	if !h.authManager.HasPermission(r, permComponent, models.PermissionConfigure) {
		http.Error(w, "Forbidden: insufficient permissions to configure "+string(permComponent), http.StatusForbidden)
		return
	}

	// Get enabled servers from database (dynamic, includes newly created servers)
	servers := h.getEnabledServers()

	// Get server ID from query param
	boxID := r.URL.Query().Get("server")
	if boxID == "" && len(servers) > 0 {
		boxID = servers[0].ID
	}
	redirectBase := "/settings/plugins/" + pluginName + "?server=" + url.QueryEscape(boxID)

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape("Invalid form data"), http.StatusSeeOther)
		return
	}

	// Get current plugin
	plugin, err := h.db.GetPlugin(boxID, pluginName)
	if err != nil || plugin == nil {
		http.Redirect(w, r, "/settings/plugins?server="+url.QueryEscape(boxID)+"&error="+url.QueryEscape("Integration not found"), http.StatusSeeOther)
		return
	}

	// Handle config update based on plugin type
	var newConfig json.RawMessage
	switch pluginName {
	case database.PluginServices:
		newConfig, err = h.parseServicesConfig(r)
	case database.PluginLogs:
		// Logs have a special handler that saves to log_sources table
		err = h.saveLogSourcesConfig(boxID, r)
		if err != nil {
			http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		h.logAudit(r, user.ID, "plugin_updated", plugin.DisplayName+" configuration updated")
		http.Redirect(w, r, redirectBase+"&success="+url.QueryEscape("Configuration saved successfully"), http.StatusSeeOther)
		return
	case database.PluginCertificates:
		newConfig, err = h.parseCertificatesConfig(r)
	case database.PluginMetrics:
		newConfig, err = h.parseMetricsConfig(r)
	case database.PluginTraffic:
		newConfig, err = h.parseTrafficConfig(r)
	case database.PluginAlerts:
		newConfig, err = h.parseAlertsConfig(r)
	case database.PluginOSUpdates:
		newConfig, err = h.parseOSUpdatesConfig(r)
	default:
		// No special config, just save empty
		newConfig = json.RawMessage(`{}`)
	}

	if err != nil {
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Update the plugin
	if err := h.db.SetPluginConfig(boxID, pluginName, newConfig, &user.ID); err != nil {
		h.logger.Error("failed to update plugin", "plugin", pluginName, "error", err)
		http.Redirect(w, r, redirectBase+"&error="+url.QueryEscape("Failed to save configuration"), http.StatusSeeOther)
		return
	}

	h.logAudit(r, user.ID, "plugin_updated", plugin.DisplayName+" configuration updated")
	http.Redirect(w, r, redirectBase+"&success="+url.QueryEscape("Configuration saved successfully"), http.StatusSeeOther)
}

// parseServicesConfig parses the services plugin form.
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

// parseCertificatesConfig parses the certificates plugin form (includes certbot settings).
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

// parseMetricsConfig parses the metrics plugin form.
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

// parseTrafficConfig parses the traffic plugin form.
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

// parseAlertsConfig parses the alerts plugin form.
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
		ShowRulesInAlerts:   false, // Rules are now in plugin config
	}

	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// parseOSUpdatesConfig parses the OS updates plugin form.
func (h *Handler) parseOSUpdatesConfig(r *http.Request) (json.RawMessage, error) {
	alertOnAvailable := r.FormValue("alert_on_available_value") == "true"
	createSnapshotBefore := r.FormValue("create_snapshot_before_value") == "true"
	showPipx := r.FormValue("show_pipx_value") == "true"

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
		ShowPipx:               showPipx,
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

// APIPluginsHandler returns all plugins as JSON.
func (h *Handler) APIPluginsHandler(w http.ResponseWriter, r *http.Request) {
	// Get enabled servers from database (dynamic, includes newly created servers)
	servers := h.getEnabledServers()

	boxID := r.URL.Query().Get("server")
	if boxID == "" && len(servers) > 0 {
		boxID = servers[0].ID
	}

	if boxID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No server specified"}) //#nosec G104
		return
	}

	plugins, err := h.db.GetPlugins(boxID)
	if err != nil {
		h.logger.Error("Failed to get plugins", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get plugins"}) //#nosec G104
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(plugins) //#nosec G104
}

// APIPluginStatusHandler returns enabled status for plugins.
func (h *Handler) APIPluginStatusHandler(w http.ResponseWriter, r *http.Request) {
	// Get enabled servers from database (dynamic, includes newly created servers)
	servers := h.getEnabledServers()

	boxID := r.URL.Query().Get("server")
	if boxID == "" && len(servers) > 0 {
		boxID = servers[0].ID
	}

	if boxID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "No server specified"}) //#nosec G104
		return
	}

	status, err := h.db.GetEnabledPlugins(boxID)
	if err != nil {
		h.logger.Error("Failed to get plugin status", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to get plugin status"}) //#nosec G104
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

// APIUpdatePluginSortOrder updates the sort order for plugins.
func (h *Handler) APIUpdatePluginSortOrder(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"}) //#nosec G104
		return
	}

	// Check if user has permission to manage plugins
	if !h.authManager.HasPermission(r, models.ComponentPlugins, models.PermissionManage) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Forbidden"}) //#nosec G104
		return
	}

	// Get enabled servers from database (dynamic, includes newly created servers)
	servers := h.getEnabledServers()

	boxID := r.URL.Query().Get("server")
	if boxID == "" && len(servers) > 0 {
		boxID = servers[0].ID
	}

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

	if err := h.db.UpdatePluginSortOrders(boxID, orders, &user.ID); err != nil {
		h.logger.Error("Failed to update plugin sort orders", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update sort order"}) //#nosec G104
		return
	}

	h.logAudit(r, user.ID, "plugin_reordered", "Updated plugin display order")

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

	// Check if user has permission to manage plugins (metrics is part of plugins)
	if !h.authManager.HasPermission(r, models.ComponentPlugins, models.PermissionManage) {
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

// deployPluginDashboards deploys predefined dashboards for a plugin.
func (h *Handler) deployPluginDashboards(pluginName string) error {
	// Get the plugin from the registry
	p, ok := plugin.Get(pluginName)
	if !ok {
		return fmt.Errorf("plugin %s not found in registry", pluginName)
	}

	// Check if plugin implements DashboardPlugin interface
	dashPlugin, ok := p.(plugin.DashboardPlugin)
	if !ok {
		// Plugin doesn't provide dashboards, that's OK
		return nil
	}

	// Get predefined dashboards
	dashboards := dashPlugin.PredefinedDashboards()
	if len(dashboards) == 0 {
		return nil
	}

	// Deploy each dashboard
	for _, dashDef := range dashboards {
		// Parse YAML to dashboard struct
		var dash dashboard.Dashboard
		if err := yaml.Unmarshal([]byte(dashDef.YAML), &dash); err != nil {
			h.logger.Error("failed to parse plugin dashboard YAML", "plugin", pluginName, "dashboard", dashDef.Name, "error", err)
			continue
		}

		// Set plugin metadata
		dash.PluginName = &pluginName
		dash.CreatedBy = "plugin:" + pluginName
		dash.Editable = false

		// Check if dashboard already exists
		if h.dashboardStorage.Exists(dashDef.Slug) {
			h.logger.Info("plugin dashboard already exists, skipping", "plugin", pluginName, "slug", dashDef.Slug)
			continue
		}

		// Save the dashboard
		if err := h.dashboardStorage.Save(&dash); err != nil {
			h.logger.Error("failed to save plugin dashboard", "plugin", pluginName, "dashboard", dashDef.Name, "error", err)
			continue
		}

		h.logger.Info("deployed plugin dashboard", "plugin", pluginName, "dashboard", dashDef.Name, "slug", dashDef.Slug)
	}

	return nil
}
