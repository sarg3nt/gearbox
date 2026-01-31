package widget

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

var (
	// ErrDataSourceNotFound is returned when a data source is not registered.
	ErrDataSourceNotFound = errors.New("data source not registered")

	// ErrDataSourceAlreadyRegistered is returned when attempting to register a duplicate data source.
	ErrDataSourceAlreadyRegistered = errors.New("data source already registered")
)

// DataSource provides data for widgets.
type DataSource interface {
	// Name returns the data source identifier (e.g., "haproxy.backends", "metrics.cpu")
	Name() string

	// Fetch fetches data for the widget
	Fetch(ctx context.Context, params map[string]any) (any, error)

	// Subscribe subscribes to real-time updates (optional)
	Subscribe(ctx context.Context, handler func(data any)) error

	// Schema returns the data source schema for validation and documentation
	Schema() DataSourceSchema
}

// DataSourceSchema describes the structure of data returned by a data source.
type DataSourceSchema struct {
	// Type is the JSON type of the data (object, array, string, number, boolean)
	Type string

	// Description explains what data this source provides
	Description string

	// Items defines the schema for array items (if Type is "array")
	Items map[string]any

	// Properties defines object properties (if Type is "object")
	Properties map[string]any
}

// DataSourceRegistry manages registered data sources.
type DataSourceRegistry struct {
	mu      sync.RWMutex
	sources map[string]DataSource
	logger  *slog.Logger
}

// NewDataSourceRegistry creates a new data source registry.
func NewDataSourceRegistry(logger *slog.Logger) *DataSourceRegistry {
	if logger == nil {
		logger = slog.Default()
	}

	return &DataSourceRegistry{
		sources: make(map[string]DataSource),
		logger:  logger,
	}
}

// Register registers a new data source.
func (r *DataSourceRegistry) Register(source DataSource) error {
	if source.Name() == "" {
		return fmt.Errorf("data source name cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sources[source.Name()]; exists {
		return fmt.Errorf("%w: %s", ErrDataSourceAlreadyRegistered, source.Name())
	}

	r.sources[source.Name()] = source
	r.logger.Info("registered data source", "name", source.Name())

	return nil
}

// Get retrieves a data source by name.
func (r *DataSourceRegistry) Get(name string) (DataSource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	source, exists := r.sources[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrDataSourceNotFound, name)
	}

	return source, nil
}

// Fetch fetches data from a data source.
func (r *DataSourceRegistry) Fetch(ctx context.Context, name string, params map[string]any) (any, error) {
	source, err := r.Get(name)
	if err != nil {
		return nil, err
	}

	return source.Fetch(ctx, params)
}

// Subscribe subscribes to real-time updates from a data source.
func (r *DataSourceRegistry) Subscribe(ctx context.Context, name string, handler func(data any)) error {
	source, err := r.Get(name)
	if err != nil {
		return err
	}

	return source.Subscribe(ctx, handler)
}

// List returns all registered data sources.
func (r *DataSourceRegistry) List() []DataSource {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sources := make([]DataSource, 0, len(r.sources))
	for _, source := range r.sources {
		sources = append(sources, source)
	}

	return sources
}

// Exists checks if a data source is registered.
func (r *DataSourceRegistry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.sources[name]
	return exists
}

// globalDataSourceRegistry is the global data source registry instance.
var globalDataSourceRegistry *DataSourceRegistry
var dataSourceRegistryOnce sync.Once

// GlobalDataSourceRegistry returns the global data source registry.
func GlobalDataSourceRegistry() *DataSourceRegistry {
	dataSourceRegistryOnce.Do(func() {
		globalDataSourceRegistry = NewDataSourceRegistry(slog.Default())
	})
	return globalDataSourceRegistry
}

// RegisterDataSource registers a data source with the global registry.
func RegisterDataSource(source DataSource) error {
	return GlobalDataSourceRegistry().Register(source)
}
