package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Manager handles plugin lifecycle and coordination.
type Manager struct {
	mu            sync.RWMutex
	deps          Dependencies
	logger        *slog.Logger
	initialized   map[string]bool
	started       map[string]bool
	enabled       map[string]map[string]bool // serverID -> pluginName -> enabled
	store         PluginStore
	eventHandlers map[string][]func(Event) // eventType -> handlers
}

// NewManager creates a new plugin manager.
func NewManager(deps Dependencies, store PluginStore, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
		deps:          deps,
		logger:        logger,
		initialized:   make(map[string]bool),
		started:       make(map[string]bool),
		enabled:       make(map[string]map[string]bool),
		store:         store,
		eventHandlers: make(map[string][]func(Event)),
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

		// Load plugin config from store
		if m.store != nil {
			config, err := m.store.GetConfig(info.Name)
			if err != nil {
				m.logger.Warn("failed to load plugin config", "plugin", info.Name, "error", err)
			} else {
				pluginDeps.Config = config
			}
		}

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
			m.mu.Lock()
			m.eventHandlers[eventType] = append(m.eventHandlers[eventType], func(event Event) {
				p.HandleEvent(event.Type, event.Data)
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

	m.logger.Info("all plugins started", "count", len(plugins))
	return nil
}

// StopAll stops all running plugins in reverse order.
func (m *Manager) StopAll(ctx context.Context) error {
	plugins := All()

	// Reverse the order for shutdown
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

		// Get the sidebar config to determine the base path
		sidebar := p.SidebarItem()
		if sidebar == nil {
			// Plugin has no UI routes
			m.logger.Debug("plugin has no routes", "plugin", info.Name)
			continue
		}

		m.logger.Debug("registering routes", "plugin", info.Name, "path", sidebar.Path)

		// Mount the plugin's routes at its base path
		r.Route(sidebar.Path, func(sr chi.Router) {
			p.RegisterRoutes(sr)
		})
	}
}

// GetSidebarItems returns sidebar items for enabled plugins.
func (m *Manager) GetSidebarItems(serverID string) []SidebarItem {
	plugins := All()
	var items []SidebarItem

	for _, p := range plugins {
		info := p.Info()
		sidebar := p.SidebarItem()
		if sidebar == nil {
			continue
		}

		// Check if plugin is enabled for this server (or if it's always shown)
		if !sidebar.ShowAlways && !m.IsEnabled(info.Name, serverID) {
			continue
		}

		// Get badge count if provider exists
		badgeCount := 0
		if sidebar.BadgeProvider != nil {
			badgeCount = sidebar.BadgeProvider()
		}

		// Get order from store or use default
		order := sidebar.DefaultOrder
		if m.store != nil {
			order = m.store.GetOrder(info.Name, serverID)
		}

		items = append(items, SidebarItem{
			Name:               info.Name,
			DisplayName:        info.DisplayName,
			Path:               sidebar.Path,
			Icon:               sidebar.Icon,
			Order:              order,
			BadgeCount:         badgeCount,
			RequiresPermission: sidebar.RequiresPermission,
		})
	}

	// Sort by order
	sort.Slice(items, func(i, j int) bool {
		return items[i].Order < items[j].Order
	})

	return items
}

// SidebarItem represents a sidebar menu item.
type SidebarItem struct {
	Name               string
	DisplayName        string
	Path               string
	Icon               any // templ.Component
	Order              int
	BadgeCount         int
	RequiresPermission string
}

// IsEnabled checks if a plugin is enabled for a server.
func (m *Manager) IsEnabled(pluginName, serverID string) bool {
	// Check if it's a core plugin (always enabled)
	if p, ok := Get(pluginName); ok && p.Info().Core {
		return true
	}

	if m.store != nil {
		return m.store.IsEnabled(pluginName, serverID)
	}

	// If no store, check in-memory cache
	m.mu.RLock()
	defer m.mu.RUnlock()

	serverEnabled, ok := m.enabled[serverID]
	if !ok {
		return false
	}
	return serverEnabled[pluginName]
}

// SetEnabled enables or disables a plugin for a server.
func (m *Manager) SetEnabled(pluginName, serverID string, enabled bool) error {
	// Cannot disable core plugins
	if p, ok := Get(pluginName); ok && p.Info().Core && !enabled {
		return fmt.Errorf("cannot disable core plugin %q", pluginName)
	}

	if m.store != nil {
		return m.store.SetEnabled(pluginName, serverID, enabled)
	}

	// Update in-memory cache
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.enabled[serverID]; !ok {
		m.enabled[serverID] = make(map[string]bool)
	}
	m.enabled[serverID][pluginName] = enabled
	return nil
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

// RunMigrations runs database migrations for all plugins.
func (m *Manager) RunMigrations(ctx context.Context) error {
	plugins := All()
	m.logger.Info("running plugin migrations", "count", len(plugins))

	for _, p := range plugins {
		info := p.Info()
		migrations := p.Migrations()

		if len(migrations) == 0 {
			continue
		}

		m.logger.Debug("running migrations", "plugin", info.Name, "count", len(migrations))

		// Sort migrations by version
		sort.Slice(migrations, func(i, j int) bool {
			return migrations[i].Version < migrations[j].Version
		})

		for _, migration := range migrations {
			// Check if migration has been applied
			applied, err := m.isMigrationApplied(ctx, info.Name, migration.Version)
			if err != nil {
				return fmt.Errorf("failed to check migration status for %q v%d: %w",
					info.Name, migration.Version, err)
			}

			if applied {
				continue
			}

			m.logger.Debug("applying migration",
				"plugin", info.Name,
				"version", migration.Version,
				"description", migration.Description)

			if err := m.applyMigration(ctx, info.Name, migration); err != nil {
				return fmt.Errorf("failed to apply migration for %q v%d: %w",
					info.Name, migration.Version, err)
			}
		}
	}

	m.logger.Info("plugin migrations complete")
	return nil
}

// isMigrationApplied checks if a migration has been applied.
func (m *Manager) isMigrationApplied(ctx context.Context, pluginName string, version int) (bool, error) {
	if m.deps.DB == nil {
		return false, nil
	}

	var count int
	err := m.deps.DB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM plugin_migrations WHERE plugin_name = ? AND version = ?",
		pluginName, version).Scan(&count)

	if err != nil {
		// Table might not exist yet
		return false, nil
	}

	return count > 0, nil
}

// applyMigration applies a single migration.
func (m *Manager) applyMigration(ctx context.Context, pluginName string, migration Migration) error {
	if m.deps.DB == nil {
		return fmt.Errorf("no database connection")
	}

	// Start transaction
	tx, err := m.deps.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Run migration SQL
	if _, err := tx.ExecContext(ctx, migration.Up); err != nil {
		return fmt.Errorf("migration SQL failed: %w", err)
	}

	// Record migration
	_, err = tx.ExecContext(ctx,
		"INSERT INTO plugin_migrations (plugin_name, version, description, applied_at) VALUES (?, ?, ?, ?)",
		pluginName, migration.Version, migration.Description, time.Now())
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}

// PublishEvent publishes an event to all subscribed handlers.
func (m *Manager) PublishEvent(event Event) {
	m.mu.RLock()
	handlers := m.eventHandlers[event.Type]
	m.mu.RUnlock()

	for _, handler := range handlers {
		go handler(event)
	}
}

// GetPluginConfig returns the configuration for a specific plugin.
func (m *Manager) GetPluginConfig(pluginName string) (map[string]any, error) {
	if m.store == nil {
		return nil, fmt.Errorf("no plugin store configured")
	}
	return m.store.GetConfig(pluginName)
}

// SavePluginConfig saves the configuration for a specific plugin.
func (m *Manager) SavePluginConfig(pluginName string, config map[string]any) error {
	if m.store == nil {
		return fmt.Errorf("no plugin store configured")
	}
	return m.store.SaveConfig(pluginName, config)
}
