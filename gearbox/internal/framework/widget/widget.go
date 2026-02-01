// Package widget provides the widget system for dashboards.
package widget

import (
	"context"

	"github.com/a-h/templ"
)

// WidgetDefinition defines a widget type that can be used in dashboards.
type WidgetDefinition struct {
	// Type is the unique identifier for this widget type (e.g., "status-card")
	Type string

	// Name is the human-readable name of the widget
	Name string

	// Description explains what the widget does
	Description string

	// Category groups widgets (data-visualization, data-display, status-metrics, etc.)
	Category string

	// PluginName is the name of the plugin that provides this widget (e.g., "haproxy", "metrics")
	// Used to filter widgets based on enabled plugins for a server
	PluginName string

	// Icon is the icon name for the widget
	Icon string

	// ConfigSchema defines the JSON schema for widget configuration
	ConfigSchema ConfigSchema

	// Renderer is the function that renders the widget
	Renderer WidgetRenderer

	// TopBarControlsProvider is an optional function that returns top bar controls for this widget
	// If nil, the widget does not provide top bar controls
	TopBarControlsProvider func(ctx *WidgetRenderContext, config map[string]any) []TopBarControl

	// JavaScript is optional JavaScript code for the widget
	JavaScript string
}

// WidgetRenderer is a function that renders a widget.
type WidgetRenderer func(ctx context.Context, config map[string]any, data any) (templ.Component, error)

// ConfigSchema defines the JSON schema for a widget's configuration.
type ConfigSchema struct {
	// Properties defines the configuration properties
	Properties map[string]Property

	// Required lists required property names
	Required []string
}

// Property defines a configuration property.
type Property struct {
	// Type is the JSON type (string, number, boolean, object, array)
	Type string

	// Description explains the property
	Description string

	// Default is the default value
	Default any

	// Enum lists allowed values (for type: string)
	Enum []string

	// Items defines array item schema (for type: array)
	Items *Property

	// Properties defines nested object properties (for type: object)
	Properties map[string]Property
}

// WidgetInstance represents a widget instance with its configuration.
type WidgetInstance struct {
	// ID is the unique identifier for this widget instance
	ID string

	// Type identifies the widget type
	Type string

	// Config contains widget-specific configuration
	Config map[string]any

	// Definition is the widget definition (resolved from registry)
	Definition *WidgetDefinition
}

// WidgetRenderContext provides context for rendering a widget.
type WidgetRenderContext struct {
	// Context is the request context
	Context context.Context

	// ServerID is the ID of the server being monitored (if applicable)
	ServerID string

	// UserID is the ID of the current user
	UserID string

	// DataSources provides access to data sources
	DataSources *DataSourceRegistry
}

// Render renders the widget instance.
func (w *WidgetInstance) Render(ctx *WidgetRenderContext) (templ.Component, error) {
	if w.Definition == nil {
		return nil, ErrWidgetNotFound
	}

	// Fetch data from data source if configured
	var data any
	if dataSource, ok := w.Config["data_source"].(string); ok && dataSource != "" {
		var err error
		data, err = ctx.DataSources.Fetch(ctx.Context, dataSource, w.Config)
		if err != nil {
			return nil, err
		}
	}

	// Render the widget
	return w.Definition.Renderer(ctx.Context, w.Config, data)
}

// GetTopBarControls returns the top bar controls for this widget instance.
// Returns an empty slice if the widget does not provide top bar controls.
func (w *WidgetInstance) GetTopBarControls(ctx *WidgetRenderContext) []TopBarControl {
	if w.Definition == nil || w.Definition.TopBarControlsProvider == nil {
		return []TopBarControl{}
	}

	return w.Definition.TopBarControlsProvider(ctx, w.Config)
}
