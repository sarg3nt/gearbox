package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

// Manager handles plugin lifecycle and coordination.
type Manager struct {
	mu            sync.RWMutex
	deps          Dependencies
	logger        *slog.Logger
	initialized   map[string]bool
	started       map[string]bool
	eventHandlers map[string][]func(events.Event) // eventType -> handlers
	collectors    []*runningCollector      // Active periodic collectors
	stopChan      chan struct{}            // Signal to stop all background goroutines
}

// runningCollector tracks an active periodic collector.
type runningCollector struct {
	name     string
	interval time.Duration
	collect  func(ctx context.Context) (any, error)
	onData   func(data any) error
	stopChan chan struct{}
}

// NewManager creates a new plugin manager.
func NewManager(deps Dependencies, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
		deps:          deps,
		logger:        logger,
		initialized:   make(map[string]bool),
		started:       make(map[string]bool),
		eventHandlers: make(map[string][]func(events.Event)),
		collectors:    make([]*runningCollector, 0),
		stopChan:      make(chan struct{}),
	}
}

// InitializeAll initializes all registered plugins.
// Returns an error if any plugin fails to initialize.
func (m *Manager) InitializeAll(ctx context.Context) error {
	plugins := All()
	m.logger.Info("initializing plugins", "count", len(plugins))

	for _, p := range plugins {
		info := p.Info()

		// Create a plugin-specific logger
		pluginDeps := m.deps
		pluginDeps.Logger = m.logger.With("plugin", info.Name)

		m.logger.Debug("initializing plugin", "plugin", info.Name, "version", info.Version)

		if err := p.Initialize(ctx, pluginDeps); err != nil {
			return fmt.Errorf("failed to initialize plugin %q: %w", info.Name, err)
		}

		m.mu.Lock()
		m.initialized[info.Name] = true
		m.mu.Unlock()

		m.logger.Debug("plugin initialized", "plugin", info.Name)
	}

	// Set up event handlers for plugins that implement EventHandlerPlugin
	m.setupEventHandlers()

	m.logger.Info("all plugins initialized", "count", len(plugins))
	return nil
}

// setupEventHandlers registers event handlers for EventHandlerPlugin implementations.
func (m *Manager) setupEventHandlers() {
	for _, p := range GetEventHandlerPlugins() {
		info := p.Info()
		for _, eventType := range p.SubscribedEvents() {
			handler := p // Capture for closure
			m.mu.Lock()
			m.eventHandlers[eventType] = append(m.eventHandlers[eventType], func(event events.Event) {
				handler.HandleEvent(string(event.Type), event.Data)
			})
			m.mu.Unlock()
			m.logger.Debug("registered event handler", "plugin", info.Name, "event", eventType)
		}
	}
}

// StartAll starts all initialized plugins.
func (m *Manager) StartAll(ctx context.Context) error {
	plugins := All()
	m.logger.Info("starting plugins", "count", len(plugins))

	for _, p := range plugins {
		info := p.Info()

		m.mu.RLock()
		initialized := m.initialized[info.Name]
		m.mu.RUnlock()

		if !initialized {
			m.logger.Warn("skipping uninitialized plugin", "plugin", info.Name)
			continue
		}

		m.logger.Debug("starting plugin", "plugin", info.Name)

		if err := p.Start(ctx); err != nil {
			return fmt.Errorf("failed to start plugin %q: %w", info.Name, err)
		}

		m.mu.Lock()
		m.started[info.Name] = true
		m.mu.Unlock()

		m.logger.Debug("plugin started", "plugin", info.Name)
	}

	// Start periodic collectors
	m.startCollectors(ctx)

	// Start streamers
	m.startStreamers(ctx)

	m.logger.Info("all plugins started", "count", len(plugins))
	return nil
}

// startCollectors starts all periodic collectors from CollectorPlugin implementations.
func (m *Manager) startCollectors(ctx context.Context) {
	for _, cp := range GetCollectorPlugins() {
		info := cp.Info()
		for _, c := range cp.Collectors() {
			if c.Interval <= 0 {
				// On-demand only collector
				continue
			}

			rc := &runningCollector{
				name:     fmt.Sprintf("%s/%s", info.Name, c.Name),
				interval: c.Interval,
				collect:  c.Collect,
				onData:   c.OnData,
				stopChan: make(chan struct{}),
			}

			m.mu.Lock()
			m.collectors = append(m.collectors, rc)
			m.mu.Unlock()

			go m.runCollector(ctx, rc)
			m.logger.Debug("started collector", "collector", rc.name, "interval", c.Interval)
		}
	}
}

// runCollector runs a periodic collector until stopped.
func (m *Manager) runCollector(ctx context.Context, rc *runningCollector) {
	ticker := time.NewTicker(rc.interval)
	defer ticker.Stop()

	// Run immediately once
	m.executeCollector(ctx, rc)

	for {
		select {
		case <-ticker.C:
			m.executeCollector(ctx, rc)
		case <-rc.stopChan:
			return
		case <-m.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

// executeCollector runs a single collection cycle.
func (m *Manager) executeCollector(ctx context.Context, rc *runningCollector) {
	data, err := rc.collect(ctx)
	if err != nil {
		m.logger.Error("collector failed", "collector", rc.name, "error", err)
		return
	}

	if rc.onData != nil {
		if err := rc.onData(data); err != nil {
			m.logger.Error("collector onData failed", "collector", rc.name, "error", err)
		}
	}
}

// startStreamers starts all streamers from StreamerPlugin implementations.
func (m *Manager) startStreamers(ctx context.Context) {
	for _, sp := range GetStreamerPlugins() {
		info := sp.Info()
		for _, s := range sp.Streamers() {
			if err := s.Start(ctx); err != nil {
				m.logger.Error("failed to start streamer", "plugin", info.Name, "streamer", s.Name, "error", err)
				continue
			}
			m.logger.Debug("started streamer", "plugin", info.Name, "streamer", s.Name)
		}
	}
}

// StopAll stops all running plugins in reverse order.
func (m *Manager) StopAll(ctx context.Context) error {
	// Signal all background goroutines to stop
	close(m.stopChan)

	// Stop all collectors
	m.mu.RLock()
	collectors := m.collectors
	m.mu.RUnlock()

	for _, rc := range collectors {
		close(rc.stopChan)
		m.logger.Debug("stopped collector", "collector", rc.name)
	}

	// Stop all streamers
	for _, sp := range GetStreamerPlugins() {
		for _, s := range sp.Streamers() {
			if err := s.Stop(); err != nil {
				m.logger.Error("failed to stop streamer", "streamer", s.Name, "error", err)
			}
		}
	}

	// Stop plugins in reverse order
	plugins := All()
	for i, j := 0, len(plugins)-1; i < j; i, j = i+1, j-1 {
		plugins[i], plugins[j] = plugins[j], plugins[i]
	}

	m.logger.Info("stopping plugins", "count", len(plugins))

	var errors []error
	for _, p := range plugins {
		info := p.Info()

		m.mu.RLock()
		started := m.started[info.Name]
		m.mu.RUnlock()

		if !started {
			continue
		}

		m.logger.Debug("stopping plugin", "plugin", info.Name)

		if err := p.Stop(ctx); err != nil {
			m.logger.Error("failed to stop plugin", "plugin", info.Name, "error", err)
			errors = append(errors, fmt.Errorf("plugin %q: %w", info.Name, err))
		} else {
			m.mu.Lock()
			m.started[info.Name] = false
			m.mu.Unlock()
			m.logger.Debug("plugin stopped", "plugin", info.Name)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to stop %d plugins", len(errors))
	}

	m.logger.Info("all plugins stopped")
	return nil
}

// RegisterRoutes registers HTTP routes for all plugins.
func (m *Manager) RegisterRoutes(r chi.Router) {
	plugins := All()
	m.logger.Info("registering plugin routes", "count", len(plugins))

	for _, p := range plugins {
		info := p.Info()
		m.logger.Debug("registering routes", "plugin", info.Name)
		p.RegisterRoutes(r)
	}
}

// GetHealth returns health status for all plugins.
func (m *Manager) GetHealth() map[string]HealthStatus {
	plugins := All()
	health := make(map[string]HealthStatus)

	for _, p := range plugins {
		info := p.Info()

		m.mu.RLock()
		started := m.started[info.Name]
		m.mu.RUnlock()

		if !started {
			health[info.Name] = HealthStatus{
				Status:    HealthStatusUnhealthy,
				Message:   "plugin not started",
				LastCheck: time.Now(),
			}
			continue
		}

		health[info.Name] = p.Health()
	}

	return health
}

// PublishEvent publishes an event to all subscribed handlers.
func (m *Manager) PublishEvent(event events.Event) {
	m.mu.RLock()
	handlers := m.eventHandlers[string(event.Type)]
	m.mu.RUnlock()

	for _, handler := range handlers {
		go handler(event)
	}

	// Also publish to the event bus if available
	if m.deps.EventBus != nil {
		m.deps.EventBus.Publish(event)
	}
}

// GetMonitoredServices returns all services that should be monitored.
// This aggregates services from all ServicePlugin implementations.
func (m *Manager) GetMonitoredServices() []string {
	return GetAllMonitoredServices()
}

// GetEventTypes returns all event types from all plugins.
func (m *Manager) GetEventTypes() []EventType {
	return GetAllEventTypes()
}

// IsPluginStarted returns whether a plugin has been started.
func (m *Manager) IsPluginStarted(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started[name]
}

// IsPluginInitialized returns whether a plugin has been initialized.
func (m *Manager) IsPluginInitialized(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initialized[name]
}
