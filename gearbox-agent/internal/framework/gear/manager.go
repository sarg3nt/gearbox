package gear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
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
	probed        map[string]ProbeResult // verdict from ProbeAll
	eventHandlers map[string][]func(events.Event) // eventType -> handlers
	collectors    []*runningCollector      // Active periodic collectors
	stopChan      chan struct{}            // Signal to stop all background goroutines

	// tableWriter is where the probe summary table is rendered. Defaults
	// to os.Stderr (which goes to the systemd journal in production).
	// Tests override this to capture output.
	tableWriter io.Writer
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
		probed:        make(map[string]ProbeResult),
		eventHandlers: make(map[string][]func(events.Event)),
		collectors:    make([]*runningCollector, 0),
		stopChan:      make(chan struct{}),
		tableWriter:   os.Stderr,
	}
}

// ProbeAll runs each gear's Probe (if implemented) and records the verdict.
// Gears that don't implement ProbeableGear are treated as always-available.
// After all gears are probed, a summary table is written to the configured
// table writer (defaults to os.Stderr → systemd journal) and a structured
// completion line is logged via slog.
//
// Must be called before InitializeAll. The verdict drives whether each
// gear participates in Initialize, Start, route registration, collectors,
// and streamers.
func (m *Manager) ProbeAll(ctx context.Context) {
	plugins := All()
	m.logger.Info("probing host for gear capabilities", "registered_gears", len(plugins))

	for _, p := range plugins {
		info := p.Info()
		result := probeOrDefault(ctx, p, m.deps)
		m.mu.Lock()
		m.probed[info.Name] = result
		m.mu.Unlock()
	}

	m.logProbeTable()
}

// probeOrDefault calls a gear's Probe if it implements ProbeableGear, or
// returns an Available result for gears that don't. Defaulting to
// Available preserves the pre-probe-phase behavior for gears that haven't
// been migrated yet.
func probeOrDefault(ctx context.Context, p Gear, deps Dependencies) ProbeResult {
	if probable, ok := p.(ProbeableGear); ok {
		return probable.Probe(ctx, deps)
	}
	return ProbeAvailable("no probe implementation; defaulting to available", nil)
}

// isLoaded reports whether the gear should participate in the lifecycle.
// If ProbeAll has not been called, every gear is considered loaded so that
// existing callers (including tests) keep working.
func (m *Manager) isLoaded(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.probed[name]
	if !ok {
		return true
	}
	return r.IsAvailable()
}

// ProbeResults returns a snapshot of every gear's probe verdict. Useful
// for the upcoming /api/v1/system/capabilities endpoint and for tests.
func (m *Manager) ProbeResults() map[string]ProbeResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]ProbeResult, len(m.probed))
	for k, v := range m.probed {
		out[k] = v
	}
	return out
}

// logProbeTable renders the visual probe summary to the table writer and
// logs a structured completion line via slog. The visual table goes to
// stderr (the systemd journal in production) because slog text handlers
// quote multi-line msg strings; rendering raw to stderr keeps the table
// readable in journalctl output without losing structured metadata in the
// slog stream.
func (m *Manager) logProbeTable() {
	plugins := All()

	type row struct {
		name   string
		status string
		reason string
	}

	rows := make([]row, 0, len(plugins))
	var available, unavailable int
	for _, p := range plugins {
		info := p.Info()
		r := m.probed[info.Name]
		label := "enabled"
		if !r.IsAvailable() {
			label = "disabled"
			unavailable++
		} else {
			available++
		}
		reason := ""
		if label == "disabled" {
			reason = r.Reason
		}
		rows = append(rows, row{name: info.Name, status: label, reason: reason})
	}

	nameWidth := len("GEAR")
	statusWidth := len("STATUS")
	for _, r := range rows {
		if l := len(r.name); l > nameWidth {
			nameWidth = l
		}
		if l := len(r.status); l > statusWidth {
			statusWidth = l
		}
	}
	// 2-space padding between columns for legibility.
	nameWidth += 2
	statusWidth += 2

	var b strings.Builder
	b.WriteString("\nGear probe summary:\n\n")
	fmt.Fprintf(&b, "  %-*s%-*s%s\n", nameWidth, "GEAR", statusWidth, "STATUS", "REASON")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s%-*s%s\n", nameWidth, r.name, statusWidth, r.status, r.reason)
	}
	b.WriteString("\n")

	_, _ = io.WriteString(m.tableWriter, b.String())

	m.logger.Info("gear probe complete",
		"registered", len(plugins),
		"available", available,
		"unavailable", unavailable,
	)
}

// InitializeAll initializes all registered gears that the probe phase
// flagged as Available. Gears that probed negative are skipped silently —
// they were already accounted for in the probe summary table.
// Returns an error if any loaded plugin fails to initialize.
func (m *Manager) InitializeAll(ctx context.Context) error {
	plugins := All()
	var loadedCount int
	for _, p := range plugins {
		if m.isLoaded(p.Info().Name) {
			loadedCount++
		}
	}
	m.logger.Info("initializing plugins", "loaded", loadedCount, "registered", len(plugins))

	for _, p := range plugins {
		info := p.Info()

		if !m.isLoaded(info.Name) {
			continue
		}

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

	m.logger.Info("all plugins initialized", "count", loadedCount)
	return nil
}

// setupEventHandlers registers event handlers for EventHandlerGear implementations.
func (m *Manager) setupEventHandlers() {
	for _, p := range GetEventHandlerGears() {
		info := p.Info()
		if !m.isLoaded(info.Name) {
			continue
		}
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

// StartAll starts every initialized plugin. Gears skipped by the probe
// phase have initialized=false and are passed over silently.
func (m *Manager) StartAll(ctx context.Context) error {
	plugins := All()
	var startedCount int

	for _, p := range plugins {
		info := p.Info()

		m.mu.RLock()
		initialized := m.initialized[info.Name]
		m.mu.RUnlock()

		if !initialized {
			// Either skipped by probe (logged in the probe table) or a
			// load-failure that already returned from InitializeAll. Either
			// way, nothing to start.
			continue
		}

		m.logger.Debug("starting gear", "gear", info.Name)

		if err := p.Start(ctx); err != nil {
			return fmt.Errorf("failed to start gear %q: %w", info.Name, err)
		}

		m.mu.Lock()
		m.started[info.Name] = true
		m.mu.Unlock()
		startedCount++

		m.logger.Debug("gear started", "gear", info.Name)
	}

	m.logger.Info("starting plugins", "count", startedCount)

	// Start periodic collectors
	m.startCollectors(ctx)

	// Start streamers
	m.startStreamers(ctx)

	m.logger.Info("all plugins started", "count", startedCount)
	return nil
}

// startCollectors starts all periodic collectors from CollectorGear implementations.
func (m *Manager) startCollectors(ctx context.Context) {
	for _, cp := range GetCollectorGears() {
		info := cp.Info()
		if !m.isLoaded(info.Name) {
			continue
		}
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
		if !m.isLoaded(info.Name) {
			continue
		}
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

// RegisterRoutes registers HTTP routes for plugins that the probe phase
// flagged as loaded. Routes for skipped gears are intentionally absent so
// the API doesn't lie about what the agent can actually do on this host.
func (m *Manager) RegisterRoutes(r chi.Router) {
	plugins := All()
	var loaded int
	for _, p := range plugins {
		info := p.Info()
		if !m.isLoaded(info.Name) {
			continue
		}
		loaded++
		m.logger.Debug("registering routes", "gear", info.Name)
		p.RegisterRoutes(r)
	}
	m.logger.Info("registered gear routes", "count", loaded)
}

// CapabilityEntry is the per-gear shape returned by the capabilities API.
// Stable JSON keys — the dashboard parses this to decide which gears to
// surface in the Gears settings page.
type CapabilityEntry struct {
	Status       ProbeStatus       `json:"status"`
	Reason       string            `json:"reason,omitempty"`
	Capabilities map[string]string `json:"capabilities,omitempty"`
}

// CapabilitiesResponse is the envelope for GET /api/v1/system/capabilities.
type CapabilitiesResponse struct {
	Gears map[string]CapabilityEntry `json:"gears"`
}

// RegisterSystemRoutes registers cross-cutting agent endpoints that aren't
// tied to a single gear. Today that's just the capability table; future
// system-wide endpoints belong here too. Mount under the same auth group as
// the plugin routes.
func (m *Manager) RegisterSystemRoutes(r chi.Router) {
	r.Get("/api/v1/system/capabilities", m.handleCapabilities)
}

// handleCapabilities renders the probe table — every registered gear, its
// verdict, reason, and any detected capability key-values. The dashboard
// uses this to hide gears that can't run on a given box (issue #71 item 2).
func (m *Manager) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	plugins := All()
	results := m.ProbeResults()
	out := CapabilitiesResponse{Gears: make(map[string]CapabilityEntry, len(plugins))}
	for _, p := range plugins {
		name := p.Info().Name
		pr := results[name]
		out.Gears[name] = CapabilityEntry{
			Status:       pr.Status,
			Reason:       pr.Reason,
			Capabilities: pr.Capabilities,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		m.logger.Error("encode capabilities response failed", "error", err)
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
