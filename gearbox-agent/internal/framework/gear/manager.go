package gear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sort"
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
// table writer (defaults to os.Stderr → systemd journal), the primary
// metric source per category is resolved (logging override warnings for
// any operator-supplied overrides that don't resolve), and a structured
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
	m.logPrimarySources()
}

// logPrimarySources resolves and logs the primary metric source per
// category. Runs at startup (from ProbeAll) so override warnings appear
// in the journal alongside the probe table, and operators can confirm at
// a glance which source the dashboard will treat as authoritative.
// Categories with no available producer are silently skipped — they're
// just absent from this host, no log noise warranted.
//
// Override-validation warnings are emitted here (once per agent start)
// rather than from inside ResolvePrimarySources, so the API endpoint
// that calls Resolve on every request doesn't replay the same warning
// on every dashboard poll.
func (m *Manager) logPrimarySources() {
	m.ValidatePrimarySourceOverrides()
	picks := m.ResolvePrimarySources()
	if len(picks) == 0 {
		return
	}
	for _, cat := range AllMetricCategories() {
		sel, ok := picks[cat]
		if !ok {
			continue
		}
		m.logger.Info("primary metric source resolved",
			"category", cat,
			"source", sel.Source,
			"reason", sel.Reason,
			"alternatives", sel.Alternatives,
		)
	}
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

	// PrimarySources keys each MetricCategory to the gear that's been
	// chosen as the authoritative producer on this host. Categories
	// with no available producer are omitted entirely (rather than
	// surfacing as an empty SourceSelection) so the dashboard can
	// distinguish "no category" from "category exists but unselected".
	PrimarySources map[MetricCategory]SourceSelection `json:"primary_sources,omitempty"`
}

// preferenceOrder ranks gears for each metric category when multiple
// produce the same data and the operator hasn't set an override. First
// match wins. The order encodes opinions about what's most likely to be
// the user's "real" source on a homelab-flavoured deploy:
//
//   - HAProxy comes first because in this project HAProxy is the L7
//     entry point and its stats see the union of every backend's
//     traffic; other web servers tend to be backends behind it. A box
//     with both an HAProxy frontend AND a backend nginx benefits from
//     the proxy view first.
//   - nginx > Apache > Caddy > Traefik is rough order-of-prevalence
//     across the homelab fleet. Operators on a Traefik-primary host
//     will simply set GEARBOX_AGENT_HTTP_SOURCE=traefik; this list
//     just makes the no-config case do something reasonable rather
//     than picking arbitrarily.
//
// Entries that aren't yet implemented as gears are harmless — the
// resolver only considers gears that probed Available.
var preferenceOrder = map[MetricCategory][]string{
	CategoryHTTPRequests: {"haproxy", "nginx", "apache", "caddy", "traefik"},
}

// categoryEnvVar names the env var that controls each metric category's
// primary-source override. Kept as a map (rather than a switch) so
// adding a new category requires only an entry here — and the
// exhaustiveness test in source_test.go fails until that entry exists,
// preventing the "(unknown category)" placeholder from ever appearing
// in operator-facing warnings or SourceSelection.Reason strings.
var categoryEnvVar = map[MetricCategory]string{
	CategoryHTTPRequests: "GEARBOX_AGENT_HTTP_SOURCE",
}

// ResolvePrimarySources picks the primary metric source for each
// defined MetricCategory based on the probe table, the built-in
// preference order, and any operator overrides in deps.SourceOverrides.
// The returned map omits any category for which no available producer
// exists — callers should treat missing keys as "no primary".
//
// Resolution rules per category:
//  1. If deps.SourceOverrides[category] names a gear that probed
//     Available AND is a registered producer (implements
//     MetricSourceGear and declares the category), use it. The
//     SourceSelection.Reason names the env var so operators can
//     verify the override took effect.
//  2. Otherwise walk preferenceOrder[category] and pick the first
//     gear that probed Available + is a registered producer.
//  3. If neither path finds anything, the category is omitted.
//
// Overrides that name an unknown gear, a not-Available gear, or a gear
// that doesn't produce data for this category fall through to auto-
// detection — operators templating env files across heterogeneous
// fleets shouldn't lose metrics because one host doesn't have the
// override's target installed. Warnings about mis-targeted overrides
// are emitted **once at startup** by ValidatePrimarySourceOverrides,
// not here, so the API endpoint that calls Resolve on every request
// doesn't turn dashboard polling into log spam.
//
// The function is side-effect-free apart from reading the probe table,
// safe to call concurrently from the capabilities API handler.
func (m *Manager) ResolvePrimarySources() map[MetricCategory]SourceSelection {
	picks, _ := m.resolvePrimarySources()
	return picks
}

// ValidatePrimarySourceOverrides re-runs the resolver and logs a
// warning for each operator override that doesn't apply on this host
// — unknown gear, not-Available gear, or gear that doesn't produce
// the category in question. Called once from ProbeAll so operators
// see actionable warnings in journalctl at startup without the
// /api/v1/system/capabilities endpoint replaying them on every poll.
//
// Returns the number of override entries that failed validation,
// purely so tests can assert without parsing log output. Production
// callers can ignore the return value.
func (m *Manager) ValidatePrimarySourceOverrides() int {
	_, invalid := m.resolvePrimarySources()
	for _, inv := range invalid {
		m.logger.Warn(inv.message,
			"category", inv.category,
			"override", inv.override,
			"env_var", categoryEnvVar[inv.category])
	}
	return len(invalid)
}

// overrideValidationFailure captures one mis-targeted override so
// ValidatePrimarySourceOverrides can log it from a single place
// (consistent slog key-set) and tests can count failures.
type overrideValidationFailure struct {
	category MetricCategory
	override string
	message  string
}

// resolvePrimarySources does the actual work shared between Resolve
// (silent) and Validate (logs warnings). Returns the picks plus any
// override failures the caller may want to surface. Unexported so the
// validation-vs-resolution split stays an implementation detail.
func (m *Manager) resolvePrimarySources() (map[MetricCategory]SourceSelection, []overrideValidationFailure) {
	probed := m.ProbeResults()

	// Build category-to-producers from the registered gears that
	// implement MetricSourceGear. Producers are filtered to only those
	// that probed Available — a not-installed nginx isn't a candidate.
	producers := make(map[MetricCategory][]string)
	registeredFor := make(map[MetricCategory]map[string]struct{})
	for _, p := range All() {
		name := p.Info().Name
		src, ok := p.(MetricSourceGear)
		if !ok {
			continue
		}
		for _, cat := range src.MetricCategories() {
			if registeredFor[cat] == nil {
				registeredFor[cat] = make(map[string]struct{})
			}
			registeredFor[cat][name] = struct{}{}
			if r, ok := probed[name]; ok && r.IsAvailable() {
				producers[cat] = append(producers[cat], name)
			}
		}
	}

	out := make(map[MetricCategory]SourceSelection)
	var failures []overrideValidationFailure
	for _, cat := range AllMetricCategories() {
		available := producers[cat]
		if len(available) == 0 {
			// No producer — omit the category entirely. The dashboard
			// reads missing key as "no primary for this category".
			continue
		}

		// Order alternatives by the built-in preference so the
		// dashboard's "switch source" UI presents a stable list.
		ordered := orderByPreference(available, preferenceOrder[cat])

		// Operator override: must be available AND a registered
		// producer. Capture validation failures for the caller to log,
		// but fall through to auto-detect either way.
		override := strings.ToLower(strings.TrimSpace(overrideForCategory(m.deps, cat)))
		if override != "" {
			if _, isProducer := registeredFor[cat][override]; !isProducer {
				failures = append(failures, overrideValidationFailure{
					category: cat,
					override: override,
					message:  "source override names a gear that doesn't produce this metric category; falling back to auto-detect",
				})
			} else if !slices.Contains(ordered, override) {
				failures = append(failures, overrideValidationFailure{
					category: cat,
					override: override,
					message:  "source override names an unavailable gear; falling back to auto-detect",
				})
			} else {
				// Defensive Clone+DeleteFunc — the Alternatives slice
				// returned to callers must not share backing storage
				// with `ordered`, which a future change to this
				// function could mutate.
				alts := slices.Clone(ordered)
				alts = slices.DeleteFunc(alts, func(s string) bool { return s == override })
				out[cat] = SourceSelection{
					Category:     cat,
					Source:       override,
					Reason:       "operator override via " + categoryEnvVar[cat],
					Alternatives: alts,
				}
				continue
			}
		}

		// Auto-detect from preference order. Clone the alternatives
		// slice for the same defensive reason as the override branch.
		out[cat] = SourceSelection{
			Category:     cat,
			Source:       ordered[0],
			Reason:       "auto-detected from preference order",
			Alternatives: slices.Clone(ordered[1:]),
		}
	}

	return out, failures
}

// overrideForCategory fetches the operator's override for a category
// from Dependencies. Standalone (not a method) so tests that build
// Dependencies directly stay readable.
func overrideForCategory(d Dependencies, cat MetricCategory) string {
	if d.SourceOverrides == nil {
		return ""
	}
	return d.SourceOverrides[cat]
}

// orderByPreference returns the subset of `available` ordered by where
// each entry appears in `preferred`. Entries in `available` that
// aren't in `preferred` get appended at the end (sorted alphabetically
// so the result is deterministic across runs). Lets newer producers
// that haven't been ranked yet still surface as alternatives without
// editing preferenceOrder.
func orderByPreference(available, preferred []string) []string {
	have := make(map[string]struct{}, len(available))
	for _, n := range available {
		have[n] = struct{}{}
	}

	out := make([]string, 0, len(available))
	used := make(map[string]struct{}, len(available))
	for _, n := range preferred {
		if _, ok := have[n]; ok {
			out = append(out, n)
			used[n] = struct{}{}
		}
	}
	// Tack on any available producer not in the preference list,
	// alphabetised for determinism.
	var extras []string
	for _, n := range available {
		if _, ok := used[n]; !ok {
			extras = append(extras, n)
		}
	}
	sort.Strings(extras)
	out = append(out, extras...)
	return out
}

// RegisterSystemRoutes registers cross-cutting agent endpoints that aren't
// tied to a single gear. Today that's just the capability table; future
// system-wide endpoints belong here too. Mount under the same auth group as
// the plugin routes.
func (m *Manager) RegisterSystemRoutes(r chi.Router) {
	r.Get("/api/v1/system/capabilities", m.handleCapabilities)
}

// handleCapabilities renders the probe table — every registered gear, its
// verdict, reason, and any detected capability key-values — plus the
// resolved primary metric source per category. The dashboard uses this
// to hide gears that can't run on a given box (issue #71 item 2) and to
// pick which source's data to render for each category (issue #91).
func (m *Manager) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	plugins := All()
	results := m.ProbeResults()
	out := CapabilitiesResponse{
		Gears:          make(map[string]CapabilityEntry, len(plugins)),
		PrimarySources: m.ResolvePrimarySources(),
	}
	for _, p := range plugins {
		name := p.Info().Name
		out.Gears[name] = CapabilityEntry(results[name])
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
