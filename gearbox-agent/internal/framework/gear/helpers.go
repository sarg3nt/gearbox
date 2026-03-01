package gear

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// BaseGear provides a default implementation of the Plugin interface.
// Plugins can embed this struct to get default implementations of optional methods.
type BaseGear struct {
	info    Info
	deps    Dependencies
	logger  *slog.Logger
	healthy atomic.Bool
}

// NewBaseGear creates a new base plugin with the given info.
func NewBaseGear(info Info) *BaseGear {
	bp := &BaseGear{
		info: info,
	}
	bp.healthy.Store(true)
	return bp
}

// Info returns the plugin's metadata.
func (p *BaseGear) Info() Info {
	return p.info
}

// Initialize stores the dependencies. Override to add custom initialization.
func (p *BaseGear) Initialize(ctx context.Context, deps Dependencies) error {
	p.deps = deps
	p.logger = deps.Logger
	return nil
}

// Start is a no-op by default. Override to start background goroutines.
func (p *BaseGear) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op by default. Override to clean up resources.
func (p *BaseGear) Stop(ctx context.Context) error {
	return nil
}

// Health returns the current health status.
func (p *BaseGear) Health() HealthStatus {
	if p.healthy.Load() {
		return NewHealthyStatus("operational")
	}
	return NewUnhealthyStatus("not healthy")
}

// RegisterRoutes is a no-op by default. Override to register HTTP routes.
func (p *BaseGear) RegisterRoutes(r chi.Router) {
	// No routes by default
}

// EventTypes returns an empty list by default. Override to declare events.
func (p *BaseGear) EventTypes() []EventType {
	return nil
}

// SetHealthy sets the plugin's health status.
func (p *BaseGear) SetHealthy(healthy bool) {
	p.healthy.Store(healthy)
}

// Logger returns the plugin's logger.
func (p *BaseGear) Logger() *slog.Logger {
	return p.logger
}

// Deps returns the plugin's dependencies.
func (p *BaseGear) Deps() Dependencies {
	return p.deps
}

// PublishEvent publishes an event to the event bus.
func (p *BaseGear) PublishEvent(eventType string, data map[string]any) {
	if p.deps.EventBus != nil {
		p.deps.EventBus.Publish(events.NewEvent(events.EventType(eventType), data))
	}
}

// GetConfigString retrieves a string config value.
func (p *BaseGear) GetConfigString(key string, defaultValue string) string {
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
func (p *BaseGear) GetConfigInt(key string, defaultValue int) int {
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
func (p *BaseGear) GetConfigBool(key string, defaultValue bool) bool {
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
