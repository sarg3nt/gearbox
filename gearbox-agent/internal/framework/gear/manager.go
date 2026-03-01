package gear

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

// InitializeAll initializes all registered gears.
// Returns an error if any plugin fails to initialize.
func (m *Manager) InitializeAll(ctx context.Context) error {
	plugins := All()
	m.logger.Info("initializing plugins", "count", len(plugins))

	for _, p := range plugins {
		info := p.Info()

		// Create a plugin-specific logger
		gearDeps := m.deps
		gearDeps.Logger = m.logger.With("gear", info.Name)

		m.logger.Debug("initializing gear", "gear", info.Name, "version", info.Version)

		if err := p.Initialize(ctx, gearDeps); err != nil {
			return fmt.Errorf("failed to initialize gear %q: %w", info.Name, err)
		}

		m.mu.Lock()
		m.initialized[info.Name] = true
		m.mu.Unlock()

		m.logger.Debug("gear initialized", "gear", info.Name)
	}

	// Set up event handlers for plugins that implement EventHandlerGear
	m.setupEventHandlers()

	m.logger.Info("all plugins initialized", "count", len(plugins))
	return nil
}

// setupEventHandlers registers event handlers for EventHandlerGear implementations.
func (m *Manager) setupEventHandlers() {
	for _, p := range GetEventHandlerGears() {
		info := p.Info()
		for _, eventType := range p.SubscribedEvents() {
			handler := p // Capture for closure
			m.mu.Lock()
			m.eventHandlers[eventType] = append(m.eventHandlers[eventType], func(event events.Event) {
				handler.HandleEvent(string(event.Type), event.Data)
			})
			m.mu.Unlock()
			m.logger.Debug("registered event handler", "gear", info.Name, "event", eventType)
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
			m.logger.Warn("skipping uninitialized gear", "gear", info.Name)
			continue
		}

		m.logger.Debug("starting gear", "gear", info.Name)

		if err := p.Start(ctx); err != nil {
			return fmt.Errorf("failed to start gear %q: %w", info.Name, err)
		}

		m.mu.Lock()
		m.started[info.Name] = true
		m.mu.Unlock()

		m.logger.Debug("gear started", "gear", info.Name)
	}

	// Start periodic collectors
	m.startCollectors(ctx)

	// Start streamers
	m.startStreamers(ctx)

	m.logger.Info("all plugins started", "count", len(plugins))
	return nil
}

// startCollectors starts all periodic collectors from CollectorGear implementations.
func (m *Manager) startCollectors(ctx context.Context) {
	for _, cp := range GetCollectorGears() {
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

// startStreamers starts all streamers from StreamerGear implementations.
func (m *Manager) startStreamers(ctx context.Context) {
	for _, sp := range GetStreamerGears() {
		info := sp.Info()
		for _, s := range sp.Streamers() {
			if err := s.Start(ctx); err != nil {
				m.logger.Error("failed to start streamer", "gear", info.Name, "streamer", s.Name, "error", err)
				continue
			}
			m.logger.Debug("started streamer", "gear", info.Name, "streamer", s.Name)
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
	for _, sp := range GetStreamerGears() {
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

		m.logger.Debug("stopping gear", "gear", info.Name)

		if err := p.Stop(ctx); err != nil {
			m.logger.Error("failed to stop gear", "gear", info.Name, "error", err)
			errors = append(errors, fmt.Errorf("gear %q: %w", info.Name, err))
		} else {
			m.mu.Lock()
			m.started[info.Name] = false
			m.mu.Unlock()
			m.logger.Debug("gear stopped", "gear", info.Name)
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
	m.logger.Info("registering gear routes", "count", len(plugins))

	for _, p := range plugins {
		info := p.Info()
		m.logger.Debug("registering routes", "gear", info.Name)
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
				Message:   "gear not started",
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
// This aggregates services from all ServiceGear implementations.
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
