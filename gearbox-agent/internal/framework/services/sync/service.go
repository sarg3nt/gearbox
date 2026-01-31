// Package sync orchestrates the HAProxy auto-configuration sync process.
package sync

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/services/compose"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/github"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/haproxy"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/state"
)

// Service orchestrates the sync process.
type Service struct {
	github       *github.Client
	state        *state.Manager
	haproxyMgr   *haproxy.Manager
	appsFolder   string
	metadataFile string
	logger       *slog.Logger
	dryRun       bool

	// Current metadata for API access
	mu              sync.RWMutex
	currentMetadata *haproxy.Metadata
	lastSyncError   error

	// Channel for triggering immediate sync (e.g., from webhook)
	triggerChan chan struct{}

	// Event bus for publishing sync events (optional)
	eventBus *events.Bus
}

// Config holds sync service configuration.
type Config struct {
	// GitHub settings
	GitRepoURL   string
	GitPAT       string
	GitBranch    string
	AppsFolder   string
	RepoCacheDir string

	// HAProxy settings
	HAProxyConfigFile string
	MetadataFile      string
	StateFile         string

	// Runtime
	DryRun bool
	Logger *slog.Logger
}

// NewService creates a new sync service.
func NewService(cfg Config) (*Service, error) {
	// Create GitHub client
	ghClient, err := github.NewClient(github.Config{
		RepoURL:  cfg.GitRepoURL,
		PAT:      cfg.GitPAT,
		Branch:   cfg.GitBranch,
		CacheDir: cfg.RepoCacheDir,
		Logger:   cfg.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Create state manager
	stateMgr, err := state.NewManager(cfg.StateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create state manager: %w", err)
	}

	// Create HAProxy manager
	haproxyMgr := haproxy.NewManager(cfg.HAProxyConfigFile, cfg.DryRun, cfg.Logger)

	return &Service{
		github:       ghClient,
		state:        stateMgr,
		haproxyMgr:   haproxyMgr,
		appsFolder:   cfg.AppsFolder,
		metadataFile: cfg.MetadataFile,
		logger:       cfg.Logger,
		dryRun:       cfg.DryRun,
		triggerChan:  make(chan struct{}, 1), // Buffered to avoid blocking webhook handler
	}, nil
}

// SyncResult contains the result of a sync operation.
type SyncResult struct {
	Updated       bool
	BackendCount  int
	CommitSHA     string
	Error         error
	ConfigChanged bool
}

// RunOnce performs a single sync cycle.
func (s *Service) RunOnce(forceUpdate bool) SyncResult {
	result := SyncResult{}

	s.logger.Info("Starting synchronization cycle")
	s.publishEvent(events.EventSyncStarted, map[string]any{
		"force_update": forceUpdate,
	})

	// Check for new commits and determine if update needed
	latestSHA, needsUpdate, err := s.checkForUpdates(forceUpdate)
	if err != nil {
		result.Error = err
		s.setError(err)
		return result
	}
	result.CommitSHA = latestSHA

	if !needsUpdate {
		s.logger.Info("No new commits", "current_sha", truncateSHA(latestSHA))
		return result
	}

	// Sync repository
	if _, err := s.github.Sync(); err != nil {
		result.Error = fmt.Errorf("failed to sync repository: %w", err)
		s.setError(result.Error)
		return result
	}

	// Parse all backends from compose files
	allBackends, err := s.parseAllBackends()
	if err != nil {
		result.Error = err
		s.setError(err)
		return result
	}
	result.BackendCount = len(allBackends)
	s.logger.Info("Found HAProxy backends", "count", result.BackendCount)

	// Generate and apply configuration
	configChanged, err := s.applyConfiguration(allBackends)
	if err != nil {
		result.Error = err
		s.setError(err)
		return result
	}
	result.ConfigChanged = configChanged

	// Finalize sync: export metadata and update state
	s.finalizeSync(allBackends, latestSHA)

	result.Updated = true
	s.logger.Info("Synchronization cycle completed successfully")
	s.publishSyncCompletedEvents(result)

	return result
}

// checkForUpdates checks if a sync is needed based on commit SHA and markers.
func (s *Service) checkForUpdates(forceUpdate bool) (string, bool, error) {
	latestSHA, err := s.github.GetLatestCommitSHA()
	if err != nil {
		return "", false, fmt.Errorf("failed to fetch latest commit: %w", err)
	}

	lastSHA := s.state.GetLastCommitSHA()

	// Check if config has required markers (force update if not)
	hasMarkers, err := s.haproxyMgr.HasMarkers()
	if err != nil {
		s.logger.Warn("Could not check for markers", "error", err)
	} else if !hasMarkers {
		s.logger.Info("Config file missing required markers, forcing update")
		forceUpdate = true
	}

	needsUpdate := latestSHA != lastSHA || forceUpdate

	if needsUpdate {
		if forceUpdate {
			s.logger.Info("Forcing configuration update")
		} else {
			s.logger.Info("New commit detected", "sha", truncateSHA(latestSHA))
		}
	}

	return latestSHA, needsUpdate, nil
}

// parseAllBackends parses all docker-compose files and returns backend configs.
func (s *Service) parseAllBackends() ([]compose.BackendConfig, error) {
	appsPath := filepath.Join(s.github.GetCacheDir(), s.appsFolder)
	parser := compose.NewParser(appsPath, s.logger)

	composeFiles, err := parser.FindComposeFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to find compose files: %w", err)
	}

	var allBackends []compose.BackendConfig

	for _, file := range composeFiles {
		backends, err := parser.ParseFile(file)
		if err != nil {
			s.logger.Warn("Failed to parse compose file", "file", file, "error", err)
			continue
		}
		allBackends = append(allBackends, backends...)
	}

	// Parse hardware services file if it exists
	if hwFile := parser.FindHardwareServicesFile(); hwFile != "" {
		backends, err := parser.ParseFile(hwFile)
		if err != nil {
			s.logger.Warn("Failed to parse hardware services", "error", err)
		} else {
			allBackends = append(allBackends, backends...)
		}
	}

	return allBackends, nil
}

// applyConfiguration generates and applies the HAProxy configuration.
func (s *Service) applyConfiguration(backends []compose.BackendConfig) (bool, error) {
	generator := haproxy.NewGenerator(backends)
	routingConfig := generator.GenerateRoutingConfig()
	backendConfig := generator.GenerateBackendConfig()

	configChanged, err := s.haproxyMgr.UpdateConfig(routingConfig, backendConfig)
	if err != nil {
		return false, fmt.Errorf("failed to update HAProxy config: %w", err)
	}

	return configChanged, nil
}

// finalizeSync exports metadata and updates state after a successful sync.
func (s *Service) finalizeSync(backends []compose.BackendConfig, commitSHA string) {
	// Export metadata
	metadata := haproxy.ExportMetadata(backends)
	if err := haproxy.WriteMetadataFile(metadata, s.metadataFile); err != nil {
		s.logger.Warn("Failed to write metadata file", "error", err)
	} else {
		s.logger.Info("Exported metadata", "path", s.metadataFile)
	}

	// Update current metadata for API access
	s.mu.Lock()
	s.currentMetadata = metadata
	s.lastSyncError = nil
	s.mu.Unlock()

	// Update state
	if err := s.state.SetLastCommitSHA(commitSHA); err != nil {
		s.logger.Warn("Failed to save state", "error", err)
	}
}

// publishSyncCompletedEvents publishes events for a completed sync.
func (s *Service) publishSyncCompletedEvents(result SyncResult) {
	s.publishEvent(events.EventSyncCompleted, map[string]any{
		"commit_sha":     result.CommitSHA,
		"backend_count":  result.BackendCount,
		"config_changed": result.ConfigChanged,
	})

	if result.ConfigChanged {
		s.publishEvent(events.EventConfigChanged, map[string]any{
			"commit_sha":    result.CommitSHA,
			"backend_count": result.BackendCount,
		})
	}
}

// truncateSHA safely truncates a SHA to 7 characters for display.
func truncateSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// GetMetadata returns the current metadata (thread-safe).
func (s *Service) GetMetadata() *haproxy.Metadata {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentMetadata
}

// GetLastError returns the last sync error (thread-safe).
func (s *Service) GetLastError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSyncError
}

// GetLastSyncTime returns the last successful sync time.
func (s *Service) GetLastSyncTime() time.Time {
	return s.state.GetLastSyncTime()
}

// setError records an error.
func (s *Service) setError(err error) {
	s.mu.Lock()
	s.lastSyncError = err
	s.mu.Unlock()
	s.state.SetError(err)
	s.logger.Error("Sync error", "error", err)

	// Publish sync failed event
	s.publishEvent(events.EventSyncFailed, map[string]any{
		"error": err.Error(),
	})
}

// LoadMetadataFromFile loads metadata from the metadata file.
func (s *Service) LoadMetadataFromFile() error {
	// This would be used on startup to load existing metadata
	// For now, we'll let it be populated on first sync
	return nil
}

// TriggerSync triggers an immediate sync cycle (non-blocking).
// Used by webhook handler to trigger sync on push events.
func (s *Service) TriggerSync() {
	select {
	case s.triggerChan <- struct{}{}:
		s.logger.Info("Sync triggered via webhook")
	default:
		s.logger.Info("Sync already pending, ignoring trigger")
	}
}

// GetTriggerChan returns the trigger channel for use in the main loop.
func (s *Service) GetTriggerChan() <-chan struct{} {
	return s.triggerChan
}

// SetEventBus sets the event bus for publishing sync events.
func (s *Service) SetEventBus(bus *events.Bus) {
	s.eventBus = bus
}

// publishEvent publishes an event if an event bus is configured.
func (s *Service) publishEvent(eventType events.EventType, data map[string]any) {
	if s.eventBus != nil {
		s.eventBus.Publish(events.NewEvent(eventType, data))
	}
}
