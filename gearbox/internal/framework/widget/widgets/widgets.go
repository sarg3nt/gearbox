// Package widgets provides the core widget implementations for the dashboard system.
package widgets

import (
	"context"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// RegisterCoreWidgets registers all core widgets with the widget registry.
func RegisterCoreWidgets(registry *widget.Registry) error {
	widgets := []*widget.WidgetDefinition{
		placementBoxDefinition(),
		statusCardDefinition(),
		metricCardDefinition(),
		collapsibleContainerDefinition(),
		// Note: Control widgets like refresh-control are intentionally not exposed
		// to dashboard designers. Widget authors embed controls within their widgets
		// as needed (e.g., filters in table headers, refresh in page headers).
	}

	for _, w := range widgets {
		if err := registry.Register(w); err != nil {
			return err
		}
	}

	return nil
}

func placementBoxDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "placement-box",
		Name:        "Placement Box",
		Description: "Container widget with configurable styling and layout",
		Category:    "layout",
		PluginName:  "core",
		Icon:        "box",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"width": {
					Type:        "string",
					Description: "Width of the container",
					Default:     "full",
					Enum:        []string{"small", "medium", "full"},
				},
				"border": {
					Type:        "boolean",
					Description: "Show border",
					Default:     true,
				},
				"shadow": {
					Type:        "boolean",
					Description: "Show shadow",
					Default:     true,
				},
				"padding": {
					Type:        "string",
					Description: "Padding size",
					Default:     "normal",
					Enum:        []string{"none", "small", "normal", "large"},
				},
				"background": {
					Type:        "string",
					Description: "Background color",
					Default:     "white",
					Enum:        []string{"white", "gray", "transparent"},
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			// PlacementBox wraps other widgets, so it needs special handling
			// For now, return a simple div
			return PlacementBox(
				getStringConfig(config, "width", "full"),
				getBoolConfig(config, "border", true),
				getBoolConfig(config, "shadow", true),
				getStringConfig(config, "padding", "normal"),
				getStringConfig(config, "background", "white"),
			), nil
		},
	}
}

func statusCardDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "status-card",
		Name:        "Status Card",
		Description: "Displays status information with health indicator and metrics",
		Category:    "status-metrics",
		PluginName:  "core",
		Icon:        "activity",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"data_source": {
					Type:        "string",
					Description: "Data source for the card",
				},
				"title": {
					Type:        "string",
					Description: "Card title template",
				},
				"subtitle": {
					Type:        "string",
					Description: "Card subtitle template",
				},
				"status": {
					Type:        "object",
					Description: "Status field configuration",
					Properties: map[string]widget.Property{
						"field": {
							Type:        "string",
							Description: "Data field for status",
						},
						"show_indicator": {
							Type:        "boolean",
							Description: "Show status indicator",
							Default:     true,
						},
					},
				},
				"metrics": {
					Type:        "array",
					Description: "Metrics to display",
					Items: &widget.Property{
						Type: "object",
						Properties: map[string]widget.Property{
							"label": {
								Type:        "string",
								Description: "Metric label",
							},
							"field": {
								Type:        "string",
								Description: "Data field for metric value",
							},
							"format": {
								Type:        "string",
								Description: "Value format",
								Enum:        []string{"number", "percentage", "bytes", "duration"},
							},
							"suffix": {
								Type:        "string",
								Description: "Value suffix",
							},
						},
					},
				},
				"actions": {
					Type:        "array",
					Description: "Action buttons",
					Items: &widget.Property{
						Type: "object",
						Properties: map[string]widget.Property{
							"label": {
								Type:        "string",
								Description: "Button label",
							},
							"link": {
								Type:        "string",
								Description: "Link URL (supports templates)",
							},
						},
					},
				},
			},
			Required: []string{},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			return StatusCardWidget(config, data), nil
		},
	}
}

func metricCardDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "metric-card",
		Name:        "Metric Card",
		Description: "Displays a single metric with value, trend, and threshold coloring",
		Category:    "status-metrics",
		PluginName:  "core",
		Icon:        "trending-up",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"title": {
					Type:        "string",
					Description: "Metric title",
				},
				"value_source": {
					Type:        "string",
					Description: "Data field for metric value",
				},
				"format": {
					Type:        "string",
					Description: "Value format",
					Default:     "number",
					Enum:        []string{"number", "percentage", "bytes", "duration"},
				},
				"trend": {
					Type:        "object",
					Description: "Trend configuration",
					Properties: map[string]widget.Property{
						"enabled": {
							Type:        "boolean",
							Description: "Show trend indicator",
							Default:     false,
						},
						"field": {
							Type:        "string",
							Description: "Data field for trend value",
						},
					},
				},
				"threshold": {
					Type:        "object",
					Description: "Threshold configuration",
					Properties: map[string]widget.Property{
						"warning": {
							Type:        "number",
							Description: "Warning threshold",
						},
						"critical": {
							Type:        "number",
							Description: "Critical threshold",
						},
					},
				},
				"icon": {
					Type:        "string",
					Description: "Icon name",
				},
				"color": {
					Type:        "string",
					Description: "Card color",
					Default:     "blue",
					Enum:        []string{"blue", "green", "yellow", "red", "gray"},
				},
			},
			Required: []string{"title"},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			return MetricCardWidget(config, data), nil
		},
	}
}

func collapsibleContainerDefinition() *widget.WidgetDefinition {
	return &widget.WidgetDefinition{
		Type:        "collapsible-container",
		Name:        "Collapsible Container",
		Description: "Container that can be expanded or collapsed",
		Category:    "layout",
		PluginName:  "core",
		Icon:        "chevrons-up-down",
		ConfigSchema: widget.ConfigSchema{
			Properties: map[string]widget.Property{
				"title": {
					Type:        "string",
					Description: "Container title",
				},
				"subtitle": {
					Type:        "string",
					Description: "Container subtitle",
				},
				"default_collapsed": {
					Type:        "boolean",
					Description: "Start collapsed",
					Default:     false,
				},
				"persist_state": {
					Type:        "boolean",
					Description: "Remember collapse state",
					Default:     true,
				},
				"badge": {
					Type:        "object",
					Description: "Badge configuration",
					Properties: map[string]widget.Property{
						"text": {
							Type:        "string",
							Description: "Badge text",
						},
						"color": {
							Type:        "string",
							Description: "Badge color",
							Default:     "blue",
							Enum:        []string{"blue", "green", "yellow", "red", "gray"},
						},
					},
				},
			},
			Required: []string{"title"},
		},
		Renderer: func(ctx context.Context, config map[string]any, data any) (templ.Component, error) {
			// CollapsibleContainer needs special handling for nested widgets
			id := getStringConfig(config, "id", "collapsible-1")
			return CollapsibleContainerWidget(id, config, []templ.Component{}), nil
		},
	}
}

// refreshControlDefinition removed - control widgets are not exposed to designers.
// Widget authors embed controls within their widgets as needed.
