package dashboard

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/a-h/templ"
	"github.com/sarg3nt/gearbox/internal/framework/widget"
)

// Renderer handles rendering dashboards with their widgets.
type Renderer struct {
	widgetRegistry     *widget.Registry
	dataSourceRegistry *widget.DataSourceRegistry
	logger             *slog.Logger
}

// NewRenderer creates a new dashboard renderer.
func NewRenderer(widgetRegistry *widget.Registry, dataSourceRegistry *widget.DataSourceRegistry, logger *slog.Logger) *Renderer {
	if logger == nil {
		logger = slog.Default()
	}

	return &Renderer{
		widgetRegistry:     widgetRegistry,
		dataSourceRegistry: dataSourceRegistry,
		logger:             logger,
	}
}

// Render renders a dashboard with all its widgets.
func (r *Renderer) Render(ctx context.Context, dashboard *Dashboard, serverID string, userID string) (templ.Component, error) {
	// Render each widget
	widgetComponents := make([]RenderedWidget, 0, len(dashboard.Widgets))

	for _, widgetConfig := range dashboard.Widgets {
		// Create widget instance
		widgetInstance, err := r.widgetRegistry.CreateInstance(widgetConfig.Type, widgetConfig.ID, widgetConfig.Config)
		if err != nil {
			r.logger.Warn("failed to create widget instance",
				"widget_id", widgetConfig.ID,
				"widget_type", widgetConfig.Type,
				"error", err)
			// Add error widget instead
			widgetComponents = append(widgetComponents, RenderedWidget{
				ID:        widgetConfig.ID,
				Position:  widgetConfig.Position,
				Component: r.renderErrorWidget(widgetConfig.ID, widgetConfig.Type, err),
			})
			continue
		}

		// Render widget
		component, err := r.widgetRegistry.RenderWidget(ctx, widgetInstance, r.dataSourceRegistry, serverID, userID)
		if err != nil {
			r.logger.Warn("failed to render widget",
				"widget_id", widgetConfig.ID,
				"widget_type", widgetConfig.Type,
				"error", err)
			// Add error widget instead
			widgetComponents = append(widgetComponents, RenderedWidget{
				ID:        widgetConfig.ID,
				Position:  widgetConfig.Position,
				Component: r.renderErrorWidget(widgetConfig.ID, widgetConfig.Type, err),
			})
			continue
		}

		widgetComponents = append(widgetComponents, RenderedWidget{
			ID:        widgetConfig.ID,
			Position:  widgetConfig.Position,
			Component: component,
		})
	}

	// Render dashboard with grid layout
	return r.renderDashboardGrid(dashboard, widgetComponents), nil
}

// RenderWithTopBarControls renders a dashboard and returns both the component and top bar controls.
func (r *Renderer) RenderWithTopBarControls(ctx context.Context, dashboard *Dashboard, serverID string, userID string) (templ.Component, *widget.TopBarControlRegistry, error) {
	// Create top bar control registry
	topBarRegistry := widget.NewTopBarControlRegistry()

	// Render context for widgets
	renderCtx := &widget.WidgetRenderContext{
		Context:     ctx,
		ServerID:    serverID,
		UserID:      userID,
		DataSources: r.dataSourceRegistry,
	}

	// Render each widget and collect top bar controls
	widgetComponents := make([]RenderedWidget, 0, len(dashboard.Widgets))

	for _, widgetConfig := range dashboard.Widgets {
		// Create widget instance
		widgetInstance, err := r.widgetRegistry.CreateInstance(widgetConfig.Type, widgetConfig.ID, widgetConfig.Config)
		if err != nil {
			r.logger.Warn("failed to create widget instance",
				"widget_id", widgetConfig.ID,
				"widget_type", widgetConfig.Type,
				"error", err)
			// Add error widget instead
			widgetComponents = append(widgetComponents, RenderedWidget{
				ID:        widgetConfig.ID,
				Position:  widgetConfig.Position,
				Component: r.renderErrorWidget(widgetConfig.ID, widgetConfig.Type, err),
			})
			continue
		}

		// Collect top bar controls from this widget
		topBarControls := widgetInstance.GetTopBarControls(renderCtx)
		for _, control := range topBarControls {
			topBarRegistry.Register(control)
		}

		// Render widget
		component, err := r.widgetRegistry.RenderWidget(ctx, widgetInstance, r.dataSourceRegistry, serverID, userID)
		if err != nil {
			r.logger.Warn("failed to render widget",
				"widget_id", widgetConfig.ID,
				"widget_type", widgetConfig.Type,
				"error", err)
			// Add error widget instead
			widgetComponents = append(widgetComponents, RenderedWidget{
				ID:        widgetConfig.ID,
				Position:  widgetConfig.Position,
				Component: r.renderErrorWidget(widgetConfig.ID, widgetConfig.Type, err),
			})
			continue
		}

		widgetComponents = append(widgetComponents, RenderedWidget{
			ID:        widgetConfig.ID,
			Position:  widgetConfig.Position,
			Component: component,
		})
	}

	// Render dashboard with grid layout
	dashboardComponent := r.renderDashboardGrid(dashboard, widgetComponents)

	return dashboardComponent, topBarRegistry, nil
}

// RenderedWidget represents a rendered widget with its position.
type RenderedWidget struct {
	ID        string
	Position  WidgetPosition
	Component templ.Component
}

// renderDashboardGrid renders the dashboard grid layout.
func (r *Renderer) renderDashboardGrid(dashboard *Dashboard, widgets []RenderedWidget) templ.Component {
	return DashboardGrid(dashboard.Layout, widgets)
}

// renderErrorWidget renders an error widget when a widget fails to load.
func (r *Renderer) renderErrorWidget(widgetID string, widgetType string, err error) templ.Component {
	return ErrorWidget(widgetID, widgetType, err.Error())
}

// RenderContext provides context for rendering.
type RenderContext struct {
	Context  context.Context
	ServerID string
	UserID   string
}

// GetWidgetData fetches data for a widget from its data source.
func (r *Renderer) GetWidgetData(ctx context.Context, widgetConfig Widget) (any, error) {
	// Check if widget has a data source configured
	dataSource, ok := widgetConfig.Config["data_source"].(string)
	if !ok || dataSource == "" {
		return nil, nil // No data source configured
	}

	// Fetch data from data source
	data, err := r.dataSourceRegistry.Fetch(ctx, dataSource, widgetConfig.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data from source %s: %w", dataSource, err)
	}

	return data, nil
}
