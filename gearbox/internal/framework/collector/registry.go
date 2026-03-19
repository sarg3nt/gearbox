package collector

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// Registry manages a dynamic collection of collectors that can be added/removed at runtime.
type Registry struct {
	collectors map[string]*Manager // serverID -> Manager
	mu         sync.RWMutex
	logger     *slog.Logger
	db         *database.DB

	// Intervals for collectors
	statsInterval    time.Duration
	metadataInterval time.Duration
	metricsInterval  time.Duration
	historyInterval  time.Duration

	// TTLs for cache
	statsTTL    time.Duration
	metadataTTL time.Duration
	metricsTTL  time.Duration
	logsTTL     time.Duration
}

// RegistryConfig holds configuration for the registry.
type RegistryConfig struct {
	StatsInterval    time.Duration
	MetadataInterval time.Duration
	MetricsInterval  time.Duration
	HistoryInterval  time.Duration
	StatsTTL         time.Duration
	MetadataTTL      time.Duration
	MetricsTTL       time.Duration
	LogsTTL          time.Duration
}

// NewRegistry creates a new collector registry.
func NewRegistry(logger *slog.Logger, db *database.DB, config RegistryConfig) *Registry {
	return &Registry{
		collectors:       make(map[string]*Manager),
		logger:           logger,
		db:               db,
		statsInterval:    config.StatsInterval,
		metadataInterval: config.MetadataInterval,
		metricsInterval:  config.MetricsInterval,
		historyInterval:  config.HistoryInterval,
		statsTTL:         config.StatsTTL,
		metadataTTL:      config.MetadataTTL,
		metricsTTL:       config.MetricsTTL,
		logsTTL:          config.LogsTTL,
	}
}

// AddCollector adds and starts a new collector for a server.
func (r *Registry) AddCollector(serverConfig models.BoxConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if collector already exists
	if _, exists := r.collectors[serverConfig.ID]; exists {
		return fmt.Errorf("collector for server %s already exists", serverConfig.ID)
	}

	// Validate Agent API configuration
	if !serverConfig.UsesAgentAPI() {
		return fmt.Errorf("server %s has no valid Agent API configuration", serverConfig.ID)
	}

	// Create Agent client
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey, serverConfig.SkipTLSVerify)

	// Create collector manager with Agent client
	manager := NewManager(
		serverConfig.ID,
		agentClient,
		r.statsTTL,
		r.metadataTTL,
		r.metricsTTL,
		r.logsTTL,
		r.logger,
		r.db,
	)

	// Start background collection
	manager.Start(r.statsInterval, r.metadataInterval, r.metricsInterval, r.historyInterval)

	// Store in registry
	r.collectors[serverConfig.ID] = manager

	r.logger.Info("collector added for server",
		"server_name", serverConfig.Name,
		"server_id", serverConfig.ID)
	return nil
}

// RemoveCollector stops and removes a collector for a server.
func (r *Registry) RemoveCollector(serverID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	manager, exists := r.collectors[serverID]
	if !exists {
		return fmt.Errorf("collector for server %s does not exist", serverID)
	}

	// Stop the manager
	manager.Stop()

	// Remove from registry
	delete(r.collectors, serverID)

	r.logger.Info("collector removed for server", "server_id", serverID)
	return nil
}

// ReloadCollector restarts a collector with updated configuration.
func (r *Registry) ReloadCollector(serverConfig models.BoxConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Stop existing collector if it exists
	if manager, exists := r.collectors[serverConfig.ID]; exists {
		manager.Stop()
		delete(r.collectors, serverConfig.ID)
	}

	// Validate Agent API configuration
	if !serverConfig.UsesAgentAPI() {
		return fmt.Errorf("server %s has no valid Agent API configuration", serverConfig.ID)
	}

	// Create Agent client
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey, serverConfig.SkipTLSVerify)

	// Create collector manager with Agent client
	manager := NewManager(
		serverConfig.ID,
		agentClient,
		r.statsTTL,
		r.metadataTTL,
		r.metricsTTL,
		r.logsTTL,
		r.logger,
		r.db,
	)

	// Start background collection
	manager.Start(r.statsInterval, r.metadataInterval, r.metricsInterval, r.historyInterval)

	// Store in registry
	r.collectors[serverConfig.ID] = manager

	r.logger.Info("collector reloaded for server",
		"server_name", serverConfig.Name,
		"server_id", serverConfig.ID)
	return nil
}

// GetCollector retrieves a collector by server ID.
func (r *Registry) GetCollector(serverID string) (*Manager, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	manager, exists := r.collectors[serverID]
	return manager, exists
}

// GetAllCollectors returns a copy of the collectors map.
func (r *Registry) GetAllCollectors() map[string]*Manager {
	r.mu.RLock()
	defer r.mu.RUnlock()

	collectors := make(map[string]*Manager, len(r.collectors))
	for k, v := range r.collectors {
		collectors[k] = v
	}
	return collectors
}

// StopAll stops all collectors.
func (r *Registry) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for serverID, manager := range r.collectors {
		r.logger.Info("stopping collector for server", "server_id", serverID)
		manager.Stop()
	}

	r.collectors = make(map[string]*Manager)
}

// Count returns the number of active collectors.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.collectors)
}
