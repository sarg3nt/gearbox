package logs

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterLogsWidgets registers all logs widgets with the widget registry.
func RegisterLogsWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		logViewerDefinition(),
	}

	for _, w := range widgets {
		if err := registry.Register(w); err != nil {
			return err
		}
	}

	return nil
}

// Helper to get string from config
func getStringFromConfig(config map[string]any, key string, defaultVal string) string {
	if val, ok := config[key].(string); ok && val != "" {
		return val
	}
	return defaultVal
}

// Helper to get bool from config
func getBoolFromConfig(config map[string]any, key string, defaultVal bool) bool {
	if val, ok := config[key].(bool); ok {
		return val
	}
	return defaultVal
}

// Helper to get int from config
func getIntFromConfig(config map[string]any, key string, defaultVal int) int {
	if val, ok := config[key].(int); ok {
		return val
	}
	if val, ok := config[key].(float64); ok {
		return int(val)
	}
	return defaultVal
}

// logViewerDefinition creates the Log Viewer widget.
// Full-featured log viewer with streaming, colorization, and all controls.
func logViewerDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "log-viewer",
		Name:        "Log Viewer",
		Description: "Full-featured log viewer with real-time streaming, ANSI color support, filtering, and export capabilities",
		Category:    "system-monitoring",
		PluginName:  "logs",
		Icon:        "document-text",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"box_id": {
					Type:        "string",
					Description: "Server ID to monitor (optional, uses current server if not specified)",
				},
				"default_lines": {
					Type:        "number",
					Description: "Default number of lines to load",
					Default:     100,
				},
				"default_source": {
					Type:        "string",
					Description: "Default log source to display",
					Default:     "haproxy",
				},
				"show_controls": {
					Type:        "boolean",
					Description: "Show controls in widget (if false, use top bar controls)",
					Default:     false,
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			boxID := getStringFromConfig(config, "box_id", "")
			defaultLines := getIntFromConfig(config, "default_lines", 100)
			defaultSource := getStringFromConfig(config, "default_source", "haproxy")
			showControls := getBoolFromConfig(config, "show_controls", false)
			return LogViewerWidget(boxID, defaultLines, defaultSource, showControls), nil
		},
		TopBarControlsProvider: func(ctx *widget.WidgetRenderContext, config map[string]any) []widget.TopBarControl {
			// Only provide top bar controls if in-widget controls are disabled
			showControls := getBoolFromConfig(config, "show_controls", false)
			if showControls {
				return []widget.TopBarControl{} // Use in-widget controls instead
			}
			return GetLogViewerTopBarControls(ctx, config)
		},
	}
}

// GetLogViewerTopBarControls returns the top bar controls for the log viewer widget.
func GetLogViewerTopBarControls(ctx *widget.WidgetRenderContext, config map[string]any) []widget.TopBarControl {
	return []widget.TopBarControl{
		{
			ID:       "logs-source",
			Priority: 10,
			Group:    "filters",
			Component: templ.Raw(`
				<select id="log-source-select" class="px-3 py-1.5 bg-slate-700 text-white border border-slate-600 rounded-md text-sm">
					<option value="haproxy">HAProxy</option>
					<option value="gearbox-agent">Gearbox Agent</option>
					<option value="fail2ban">Fail2ban</option>
					<option value="nftables">NFTables</option>
					<option value="syslog">System Log</option>
					<option value="kern">Kernel Log</option>
				</select>
			`),
		},
		{
			ID:       "logs-lines",
			Priority: 20,
			Group:    "filters",
			Component: templ.Raw(`
				<select id="log-lines-select" class="px-3 py-1.5 bg-slate-700 text-white border border-slate-600 rounded-md text-sm">
					<option value="50">50 lines</option>
					<option value="100" selected>100 lines</option>
					<option value="500">500 lines</option>
					<option value="1000">1000 lines</option>
				</select>
			`),
		},
		{
			ID:       "logs-search",
			Priority: 30,
			Group:    "filters",
			Component: templ.Raw(`
				<input
					type="text"
					id="log-search"
					placeholder="Search logs..."
					class="px-3 py-1.5 bg-slate-700 text-white border border-slate-600 rounded-md text-sm w-64"
				/>
			`),
		},
		{
			ID:       "logs-follow",
			Priority: 40,
			Group:    "actions",
			Component: templ.Raw(`
				<button
					id="follow-logs-btn"
					class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-md text-sm font-medium transition-colors"
				>
					<svg class="w-4 h-4 inline-block mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" />
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
					</svg>
					Follow
				</button>
			`),
		},
		{
			ID:       "logs-refresh",
			Priority: 50,
			Group:    "actions",
			Component: templ.Raw(`
				<button
					id="refresh-logs-btn"
					class="px-3 py-1.5 bg-slate-600 hover:bg-slate-700 text-white rounded-md text-sm font-medium transition-colors"
				>
					<svg class="w-4 h-4 inline-block mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
					</svg>
					Refresh
				</button>
			`),
		},
		{
			ID:       "logs-export",
			Priority: 60,
			Group:    "actions",
			Component: templ.Raw(`
				<button
					id="export-logs-btn"
					class="px-3 py-1.5 bg-slate-600 hover:bg-slate-700 text-white rounded-md text-sm font-medium transition-colors"
				>
					<svg class="w-4 h-4 inline-block mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />
					</svg>
					Export
				</button>
			`),
		},
	}
}
