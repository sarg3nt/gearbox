package plugin

import (
	"fmt"
	"sort"
	"sync"
)

// registry is the global plugin registry.
// Plugins register themselves via init() functions.
var registry = &Registry{
	plugins: make(map[string]Plugin),
}

// Registry maintains a thread-safe collection of registered plugins.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

// Register adds a plugin to the global registry.
// This is typically called from a plugin's init() function.
// Panics if a plugin with the same name is already registered.
func Register(p Plugin) {
	registry.Register(p)
}

// Register adds a plugin to the registry.
func (r *Registry) Register(p Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()

	info := p.Info()
	if info.Name == "" {
		panic("plugin name cannot be empty")
	}

	if _, exists := r.plugins[info.Name]; exists {
		panic(fmt.Sprintf("plugin %q is already registered", info.Name))
	}

	r.plugins[info.Name] = p
}

// Get retrieves a plugin by name from the global registry.
func Get(name string) (Plugin, bool) {
	return registry.Get(name)
}

// Get retrieves a plugin by name.
func (r *Registry) Get(name string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.plugins[name]
	return p, exists
}

// All returns all registered plugins from the global registry.
func All() []Plugin {
	return registry.All()
}

// All returns all registered plugins sorted by name.
func (r *Registry) All() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugins := make([]Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		plugins = append(plugins, p)
	}

	// Sort by name for consistent ordering
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Info().Name < plugins[j].Info().Name
	})

	return plugins
}

// AllByCategory returns plugins grouped by category from the global registry.
func AllByCategory() map[string][]Plugin {
	return registry.AllByCategory()
}

// AllByCategory returns plugins grouped by category.
func (r *Registry) AllByCategory() map[string][]Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	categories := make(map[string][]Plugin)
	for _, p := range r.plugins {
		info := p.Info()
		category := info.Category
		if category == "" {
			category = "other"
		}
		categories[category] = append(categories[category], p)
	}

	// Sort plugins within each category
	for category := range categories {
		sort.Slice(categories[category], func(i, j int) bool {
			return categories[category][i].Info().Name < categories[category][j].Info().Name
		})
	}

	return categories
}

// Names returns the names of all registered plugins from the global registry.
func Names() []string {
	return registry.Names()
}

// Names returns the names of all registered plugins.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Count returns the number of registered plugins from the global registry.
func Count() int {
	return registry.Count()
}

// Count returns the number of registered plugins.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}

// GetCollectorPlugins returns all plugins that implement CollectorPlugin.
func GetCollectorPlugins() []CollectorPlugin {
	return registry.GetCollectorPlugins()
}

// GetCollectorPlugins returns all plugins that implement CollectorPlugin.
func (r *Registry) GetCollectorPlugins() []CollectorPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var collectors []CollectorPlugin
	for _, p := range r.plugins {
		if cp, ok := p.(CollectorPlugin); ok {
			collectors = append(collectors, cp)
		}
	}
	return collectors
}

// GetStreamerPlugins returns all plugins that implement StreamerPlugin.
func GetStreamerPlugins() []StreamerPlugin {
	return registry.GetStreamerPlugins()
}

// GetStreamerPlugins returns all plugins that implement StreamerPlugin.
func (r *Registry) GetStreamerPlugins() []StreamerPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var streamers []StreamerPlugin
	for _, p := range r.plugins {
		if sp, ok := p.(StreamerPlugin); ok {
			streamers = append(streamers, sp)
		}
	}
	return streamers
}

// GetServicePlugins returns all plugins that implement ServicePlugin.
func GetServicePlugins() []ServicePlugin {
	return registry.GetServicePlugins()
}

// GetServicePlugins returns all plugins that implement ServicePlugin.
func (r *Registry) GetServicePlugins() []ServicePlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var servicePlugins []ServicePlugin
	for _, p := range r.plugins {
		if sp, ok := p.(ServicePlugin); ok {
			servicePlugins = append(servicePlugins, sp)
		}
	}
	return servicePlugins
}

// GetWebSocketPlugins returns all plugins that implement WebSocketPlugin.
func GetWebSocketPlugins() []WebSocketPlugin {
	return registry.GetWebSocketPlugins()
}

// GetWebSocketPlugins returns all plugins that implement WebSocketPlugin.
func (r *Registry) GetWebSocketPlugins() []WebSocketPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var wsPlugins []WebSocketPlugin
	for _, p := range r.plugins {
		if wp, ok := p.(WebSocketPlugin); ok {
			wsPlugins = append(wsPlugins, wp)
		}
	}
	return wsPlugins
}

// GetEventHandlerPlugins returns all plugins that implement EventHandlerPlugin.
func GetEventHandlerPlugins() []EventHandlerPlugin {
	return registry.GetEventHandlerPlugins()
}

// GetEventHandlerPlugins returns all plugins that implement EventHandlerPlugin.
func (r *Registry) GetEventHandlerPlugins() []EventHandlerPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var handlers []EventHandlerPlugin
	for _, p := range r.plugins {
		if eh, ok := p.(EventHandlerPlugin); ok {
			handlers = append(handlers, eh)
		}
	}
	return handlers
}

// GetCorePlugins returns plugins that are marked as core (cannot be disabled).
func GetCorePlugins() []Plugin {
	return registry.GetCorePlugins()
}

// GetCorePlugins returns plugins that are marked as core.
func (r *Registry) GetCorePlugins() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var core []Plugin
	for _, p := range r.plugins {
		if p.Info().Core {
			core = append(core, p)
		}
	}
	return core
}

// MustGet retrieves a plugin by name or panics if not found.
func MustGet(name string) Plugin {
	p, ok := Get(name)
	if !ok {
		panic(fmt.Sprintf("plugin %q not found", name))
	}
	return p
}

// Exists checks if a plugin with the given name is registered.
func Exists(name string) bool {
	_, ok := Get(name)
	return ok
}

// GetAllMonitoredServices returns all services monitored by ServicePlugin implementations.
// This aggregates services from all plugins into a unique list.
func GetAllMonitoredServices() []string {
	return registry.GetAllMonitoredServices()
}

// GetAllMonitoredServices returns all services monitored by ServicePlugin implementations.
func (r *Registry) GetAllMonitoredServices() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var services []string

	for _, p := range r.plugins {
		if sp, ok := p.(ServicePlugin); ok {
			for _, svc := range sp.MonitoredServices() {
				if !seen[svc] {
					seen[svc] = true
					services = append(services, svc)
				}
			}
		}
	}

	sort.Strings(services)
	return services
}

// GetAllEventTypes returns all event types from all plugins.
func GetAllEventTypes() []EventType {
	return registry.GetAllEventTypes()
}

// GetAllEventTypes returns all event types from all plugins.
func (r *Registry) GetAllEventTypes() []EventType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var events []EventType
	for _, p := range r.plugins {
		events = append(events, p.EventTypes()...)
	}
	return events
}
