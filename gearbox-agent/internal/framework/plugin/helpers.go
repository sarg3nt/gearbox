package plugin

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// BasePlugin provides a default implementation of the Plugin interface.
// Plugins can embed this struct to get default implementations of optional methods.
type BasePlugin struct {
	info    Info
	deps    Dependencies
	logger  *slog.Logger
	healthy atomic.Bool
}

// NewBasePlugin creates a new base plugin with the given info.
func NewBasePlugin(info Info) *BasePlugin {
	bp := &BasePlugin{
		info: info,
	}
	bp.healthy.Store(true)
	return bp
}

// Info returns the plugin's metadata.
func (p *BasePlugin) Info() Info {
	return p.info
}

// Initialize stores the dependencies. Override to add custom initialization.
func (p *BasePlugin) Initialize(ctx context.Context, deps Dependencies) error {
	p.deps = deps
	p.logger = deps.Logger
	return nil
}

// Start is a no-op by default. Override to start background goroutines.
func (p *BasePlugin) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op by default. Override to clean up resources.
func (p *BasePlugin) Stop(ctx context.Context) error {
	return nil
}

// Health returns the current health status.
func (p *BasePlugin) Health() HealthStatus {
	if p.healthy.Load() {
		return NewHealthyStatus("operational")
	}
	return NewUnhealthyStatus("not healthy")
}

// RegisterRoutes is a no-op by default. Override to register HTTP routes.
func (p *BasePlugin) RegisterRoutes(r chi.Router) {
	// No routes by default
}

// EventTypes returns an empty list by default. Override to declare events.
func (p *BasePlugin) EventTypes() []EventType {
	return nil
}

// SetHealthy sets the plugin's health status.
func (p *BasePlugin) SetHealthy(healthy bool) {
	p.healthy.Store(healthy)
}

// Logger returns the plugin's logger.
func (p *BasePlugin) Logger() *slog.Logger {
	return p.logger
}

// Deps returns the plugin's dependencies.
func (p *BasePlugin) Deps() Dependencies {
	return p.deps
}

// PublishEvent publishes an event to the event bus.
func (p *BasePlugin) PublishEvent(eventType string, data map[string]any) {
	if p.deps.EventBus != nil {
		p.deps.EventBus.Publish(events.NewEvent(events.EventType(eventType), data))
	}
}

// GetConfigString retrieves a string config value.
func (p *BasePlugin) GetConfigString(key string, defaultValue string) string {
	if p.deps.Config == nil {
		return defaultValue
	}
	if v, ok := p.deps.Config[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultValue
}

// GetConfigInt retrieves an int config value.
func (p *BasePlugin) GetConfigInt(key string, defaultValue int) int {
	if p.deps.Config == nil {
		return defaultValue
	}
	if v, ok := p.deps.Config[key]; ok {
		switch i := v.(type) {
		case int:
			return i
		case int64:
			return int(i)
		case float64:
			return int(i)
		}
	}
	return defaultValue
}

// GetConfigBool retrieves a bool config value.
func (p *BasePlugin) GetConfigBool(key string, defaultValue bool) bool {
	if p.deps.Config == nil {
		return defaultValue
	}
	if v, ok := p.deps.Config[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultValue
}
