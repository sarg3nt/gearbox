package gear

import (
	"fmt"
	"sort"
	"sync"
)

// registry is the global gear registry.
// Plugins register themselves via init() functions.
var registry = &Registry{
	plugins: make(map[string]Gear),
}

// Registry maintains a thread-safe collection of registered gears.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Gear
}

// Register adds a gear to the global registry.
// This is typically called from a gear's init() function.
// Panics if a gear with the same name is already registered.
func Register(p Gear) {
	registry.Register(p)
}

// Register adds a gear to the registry.
func (r *Registry) Register(p Gear) {
	r.mu.Lock()
	defer r.mu.Unlock()

	info := p.Info()
	if info.Name == "" {
		panic("gear name cannot be empty")
	}

	if _, exists := r.plugins[info.Name]; exists {
		panic(fmt.Sprintf("gear %q is already registered", info.Name))
	}

	r.plugins[info.Name] = p
}

// Get retrieves a gear by name from the global registry.
func Get(name string) (Gear, bool) {
	return registry.Get(name)
}

// Get retrieves a gear by name.
func (r *Registry) Get(name string) (Gear, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.plugins[name]
	return p, exists
}

// All returns all registered gears from the global registry.
func All() []Gear {
	return registry.All()
}

// All returns all registered gears sorted by name.
func (r *Registry) All() []Gear {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]Gear, 0, len(r.plugins))
	for _, p := range r.plugins {
		items = append(items, p)
	}

	// Sort by name for consistent ordering
	sort.Slice(items, func(i, j int) bool {
		return items[i].Info().Name < items[j].Info().Name
	})

	return items
}

// AllByCategory returns plugins grouped by category from the global registry.
func AllByCategory() map[string][]Gear {
	return registry.AllByCategory()
}

// AllByCategory returns plugins grouped by category.
func (r *Registry) AllByCategory() map[string][]Gear {
	r.mu.RLock()
	defer r.mu.RUnlock()

	categories := make(map[string][]Gear)
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

// Names returns the names of all registered gears from the global registry.
func Names() []string {
	return registry.Names()
}

// Names returns the names of all registered gears.
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

// Count returns the number of registered gears from the global registry.
func Count() int {
	return registry.Count()
}

// Count returns the number of registered gears.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.plugins)
}

// GetCollectorGears returns all plugins that implement CollectorGear.
func GetCollectorGears() []CollectorGear {
	return registry.GetCollectorGears()
}

// GetCollectorGears returns all plugins that implement CollectorGear.
func (r *Registry) GetCollectorGears() []CollectorGear {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var collectors []CollectorGear
	for _, p := range r.plugins {
		if cp, ok := p.(CollectorGear); ok {
			collectors = append(collectors, cp)
		}
	}
	return collectors
}

// GetWebSocketGears returns all plugins that implement WebSocketGear.
func GetWebSocketGears() []WebSocketGear {
	return registry.GetWebSocketGears()
}

// GetWebSocketGears returns all plugins that implement WebSocketGear.
func (r *Registry) GetWebSocketGears() []WebSocketGear {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var wsGears []WebSocketGear
	for _, p := range r.plugins {
		if wp, ok := p.(WebSocketGear); ok {
			wsGears = append(wsGears, wp)
		}
	}
	return wsGears
}

// GetEventHandlerGears returns all plugins that implement EventHandlerGear.
func GetEventHandlerGears() []EventHandlerGear {
	return registry.GetEventHandlerGears()
}

// GetEventHandlerGears returns all plugins that implement EventHandlerGear.
func (r *Registry) GetEventHandlerGears() []EventHandlerGear {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var handlers []EventHandlerGear
	for _, p := range r.plugins {
		if eh, ok := p.(EventHandlerGear); ok {
			handlers = append(handlers, eh)
		}
	}
	return handlers
}

// GetSearchableGears returns all plugins that implement SearchableGear.
func GetSearchableGears() []SearchableGear {
	return registry.GetSearchableGears()
}

// GetSearchableGears returns all plugins that implement SearchableGear.
func (r *Registry) GetSearchableGears() []SearchableGear {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var searchable []SearchableGear
	for _, p := range r.plugins {
		if sp, ok := p.(SearchableGear); ok {
			searchable = append(searchable, sp)
		}
	}
	return searchable
}

// GetCoreGears returns plugins that are marked as core (cannot be disabled).
func GetCoreGears() []Gear {
	return registry.GetCoreGears()
}

// GetCoreGears returns plugins that are marked as core.
func (r *Registry) GetCoreGears() []Gear {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var core []Gear
	for _, p := range r.plugins {
		if p.Info().Core {
			core = append(core, p)
		}
	}
	return core
}

// MustGet retrieves a gear by name or panics if not found.
func MustGet(name string) Gear {
	p, ok := Get(name)
	if !ok {
		panic(fmt.Sprintf("gear %q not found", name))
	}
	return p
}

// Exists checks if a plugin with the given name is registered.
func Exists(name string) bool {
	_, ok := Get(name)
	return ok
}
