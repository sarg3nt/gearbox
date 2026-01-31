package widget

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/a-h/templ"
)

var (
	// ErrWidgetNotFound is returned when a widget type is not registered.
	ErrWidgetNotFound = errors.New("widget type not registered")

	// ErrWidgetAlreadyRegistered is returned when attempting to register a duplicate widget type.
	ErrWidgetAlreadyRegistered = errors.New("widget type already registered")
)

// Registry manages registered widget types.
type Registry struct {
	mu      sync.RWMutex
	widgets map[string]*WidgetDefinition
	logger  *slog.Logger
}

// NewRegistry creates a new widget registry.
func NewRegistry(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}

	return &Registry{
		widgets: make(map[string]*WidgetDefinition),
		logger:  logger,
	}
}

// Register registers a new widget type.
func (r *Registry) Register(widget *WidgetDefinition) error {
	if widget.Type == "" {
		return fmt.Errorf("widget type cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.widgets[widget.Type]; exists {
		return fmt.Errorf("%w: %s", ErrWidgetAlreadyRegistered, widget.Type)
	}

	r.widgets[widget.Type] = widget
	r.logger.Info("registered widget", "type", widget.Type, "name", widget.Name)

	return nil
}

// Get retrieves a widget definition by type.
func (r *Registry) Get(widgetType string) (*WidgetDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	widget, exists := r.widgets[widgetType]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrWidgetNotFound, widgetType)
	}

	return widget, nil
}

// List returns all registered widget types.
func (r *Registry) List() []*WidgetDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	widgets := make([]*WidgetDefinition, 0, len(r.widgets))
	for _, widget := range r.widgets {
		widgets = append(widgets, widget)
	}

	return widgets
}

// ListByCategory returns all widgets in a specific category.
func (r *Registry) ListByCategory(category string) []*WidgetDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var widgets []*WidgetDefinition
	for _, widget := range r.widgets {
		if widget.Category == category {
			widgets = append(widgets, widget)
		}
	}

	return widgets
}

// Exists checks if a widget type is registered.
func (r *Registry) Exists(widgetType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.widgets[widgetType]
	return exists
}

// CreateInstance creates a widget instance from a type and configuration.
func (r *Registry) CreateInstance(widgetType string, id string, config map[string]any) (*WidgetInstance, error) {
	definition, err := r.Get(widgetType)
	if err != nil {
		return nil, err
	}

	return &WidgetInstance{
		ID:         id,
		Type:       widgetType,
		Config:     config,
		Definition: definition,
	}, nil
}

// RenderWidget renders a widget instance.
func (r *Registry) RenderWidget(ctx context.Context, instance *WidgetInstance, dataSources *DataSourceRegistry, serverID string, userID string) (templ.Component, error) {
	renderCtx := &WidgetRenderContext{
		Context:     ctx,
		ServerID:    serverID,
		UserID:      userID,
		DataSources: dataSources,
	}

	return instance.Render(renderCtx)
}

// globalRegistry is the global widget registry instance.
var globalRegistry *Registry
var registryOnce sync.Once

// GlobalRegistry returns the global widget registry.
func GlobalRegistry() *Registry {
	registryOnce.Do(func() {
		globalRegistry = NewRegistry(slog.Default())
	})
	return globalRegistry
}

// Register registers a widget with the global registry.
func Register(widget *WidgetDefinition) error {
	return GlobalRegistry().Register(widget)
}
