package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/events"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// WSConnection represents a WebSocket connection to an HAProxy Agent.
type WSConnection struct {
	serverID   string
	serverName string
	client     *agent.WSClient
	cancel     context.CancelFunc
}

// WebSocketManager manages WebSocket connections to HAProxy Agents.
type WebSocketManager struct {
	connections map[string]*WSConnection // serverID -> connection
	mu          sync.RWMutex
	hub         *events.Hub
	registry    *Registry
	logger      *slog.Logger
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewWebSocketManager creates a new WebSocket manager.
func NewWebSocketManager(hub *events.Hub, registry *Registry, logger *slog.Logger) *WebSocketManager {
	if logger == nil {
		logger = slog.Default()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WebSocketManager{
		connections: make(map[string]*WSConnection),
		hub:         hub,
		registry:    registry,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Connect establishes a WebSocket connection to an HAProxy Agent.
func (m *WebSocketManager) Connect(serverConfig models.BoxConfig) error {
	if !serverConfig.UsesAgentAPI() {
		m.logger.Debug("server does not use Agent API, skipping WebSocket",
			"server_id", serverConfig.ID)
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already connected
	if _, exists := m.connections[serverConfig.ID]; exists {
		m.logger.Debug("WebSocket already connected", "server_id", serverConfig.ID)
		return nil
	}

	// Create agent client
	agentClient := agent.NewClient(serverConfig.AgentURL, serverConfig.APIKey)

	// Create event handler that processes events and triggers actions
	handler := m.createEventHandler(serverConfig.ID, serverConfig.Name)

	// Create WebSocket client
	wsClient := agent.NewWSClient(agentClient, handler, m.logger)

	// Set up connection callback to notify hub of connection status changes
	wsClient.SetConnectionCallback(func(connected bool) {
		if connected {
			m.hub.PublishServerConnected(serverConfig.ID, serverConfig.Name)
		} else {
			m.hub.PublishServerDisconnected(serverConfig.ID, serverConfig.Name)
		}
	})

	// Create connection context
	ctx, cancel := context.WithCancel(m.ctx)

	// Store connection
	m.connections[serverConfig.ID] = &WSConnection{
		serverID:   serverConfig.ID,
		serverName: serverConfig.Name,
		client:     wsClient,
		cancel:     cancel,
	}

	// Start connection in background
	go func() {
		m.logger.Info("starting WebSocket connection", "server_id", serverConfig.ID)

		if err := wsClient.Connect(ctx); err != nil {
			if ctx.Err() == nil {
				m.logger.Error("WebSocket connection failed",
					"server_id", serverConfig.ID,
					"error", err)
			}
		}
	}()

	return nil
}

// createEventHandler creates an event handler for a specific server.
func (m *WebSocketManager) createEventHandler(serverID, serverName string) agent.EventHandler {
	return func(event agent.WSEvent) {
		m.logger.Debug("received WebSocket event",
			"server_id", serverID,
			"type", event.Type)

		switch event.Type {
		case "config.changed":
			// Trigger metadata refresh
			m.triggerMetadataRefresh(serverID)

			// Forward event to hub
			commitSHA, _ := event.Data["commit_sha"].(string)
			backendCount, _ := event.Data["backend_count"].(float64)
			m.hub.PublishConfigChanged(serverID, commitSHA, int(backendCount))

		case "sync.started":
			m.hub.Publish(events.Event{
				Type:     events.EventTypeSyncStarted,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "sync.completed":
			// Trigger metadata refresh on sync completion
			m.triggerMetadataRefresh(serverID)

			m.hub.Publish(events.Event{
				Type:     events.EventTypeSyncCompleted,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "sync.failed":
			m.hub.Publish(events.Event{
				Type:     events.EventTypeSyncFailed,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "webhook.received":
			m.hub.Publish(events.Event{
				Type:     events.EventTypeWebhookReceived,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "stats.updated":
			m.logger.Debug("received stats.updated event", "server_id", serverID)

			// Update cache with stats from the WebSocket event (if available)
			m.updateStatsFromEvent(serverID, event.Data)

			// Forward stats update to SSE clients
			m.hub.Publish(events.Event{
				Type:     events.EventTypeStatsUpdated,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "metrics.updated":
			// Update cache with metrics from the WebSocket event (if available)
			m.updateMetricsFromEvent(serverID, event.Data)

			// Forward metrics update to SSE clients
			m.hub.Publish(events.Event{
				Type:     events.EventTypeMetricsUpdated,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "logs.updated":
			// Forward log update to SSE clients
			m.hub.Publish(events.Event{
				Type:     events.EventTypeLogsUpdated,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "certificates.updated":
			// Update cache with certificates from the WebSocket event (if available)
			m.updateCertificatesFromEvent(serverID, event.Data)

			// Forward certificates update to SSE clients
			m.hub.Publish(events.Event{
				Type:     events.EventTypeCertificatesUpdated,
				ServerID: serverID,
				Data:     event.Data,
			})

		// Apt operation events - forward directly to SSE clients for terminal streaming
		case "apt.started":
			m.hub.Publish(events.Event{
				Type:     events.EventTypeAptStarted,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "apt.output":
			m.hub.Publish(events.Event{
				Type:     events.EventTypeAptOutput,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "apt.completed":
			m.hub.Publish(events.Event{
				Type:     events.EventTypeAptCompleted,
				ServerID: serverID,
				Data:     event.Data,
			})

		case "apt.failed":
			m.hub.Publish(events.Event{
				Type:     events.EventTypeAptFailed,
				ServerID: serverID,
				Data:     event.Data,
			})
		}
	}
}

// triggerMetadataRefresh triggers an immediate metadata refresh for a server.
func (m *WebSocketManager) triggerMetadataRefresh(serverID string) {
	manager, exists := m.registry.GetCollector(serverID)
	if !exists {
		m.logger.Warn("cannot refresh metadata: collector not found",
			"server_id", serverID)
		return
	}

	// Force refresh in background
	go func() {
		if err := manager.ForceRefresh(); err != nil {
			m.logger.Error("failed to refresh data after config change",
				"server_id", serverID,
				"error", err)
		} else {
			m.logger.Info("refreshed data after config change",
				"server_id", serverID)
			m.hub.PublishMetadataRefreshed(serverID)
		}
	}()
}

// triggerStatsRefresh triggers an immediate stats refresh for a server.
func (m *WebSocketManager) triggerStatsRefresh(serverID string) {
	manager, exists := m.registry.GetCollector(serverID)
	if !exists {
		m.logger.Debug("cannot refresh stats: collector not found",
			"server_id", serverID)
		return
	}

	// Refresh stats synchronously so cache is updated before SSE event
	if err := manager.RefreshStats(); err != nil {
		m.logger.Debug("failed to refresh stats",
			"server_id", serverID,
			"error", err)
	}
}

// triggerMetricsRefresh triggers an immediate metrics refresh for a server.
func (m *WebSocketManager) triggerMetricsRefresh(serverID string) {
	manager, exists := m.registry.GetCollector(serverID)
	if !exists {
		m.logger.Debug("cannot refresh metrics: collector not found",
			"server_id", serverID)
		return
	}

	// Refresh metrics synchronously so cache is updated before SSE event
	if err := manager.RefreshMetrics(); err != nil {
		m.logger.Debug("failed to refresh metrics",
			"server_id", serverID,
			"error", err)
	}
}

// updateStatsFromEvent updates the cache with stats from a WebSocket event.
// This avoids a redundant API call to the agent since it already sent the stats.
func (m *WebSocketManager) updateStatsFromEvent(serverID string, eventData map[string]interface{}) {
	manager, exists := m.registry.GetCollector(serverID)
	if !exists {
		m.logger.Warn("cannot update stats: collector not found",
			"server_id", serverID)
		return
	}

	// Extract stats from event data
	statsRaw, ok := eventData["stats"]
	if !ok {
		m.logger.Warn("stats not found in event data, falling back to API call")
		// Fallback to fetching from API
		if err := manager.RefreshStats(); err != nil {
			m.logger.Warn("failed to refresh stats",
				"server_id", serverID,
				"error", err)
		}
		return
	}

	// Convert the stats to HAProxyStats
	// The stats are already in the correct format from the agent
	stats, err := convertEventDataToStats(statsRaw)
	if err != nil {
		m.logger.Error("failed to convert event stats, falling back to API call",
			"server_id", serverID,
			"error", err)
		// Fallback to fetching from API
		if err := manager.RefreshStats(); err != nil {
			m.logger.Warn("failed to refresh stats",
				"server_id", serverID,
				"error", err)
		}
		return
	}

	// Update cache directly
	manager.GetCache().SetStats(stats)
	m.logger.Debug("updated stats cache from WebSocket",
		"server_id", serverID,
		"backend_count", len(stats.Backends),
		"frontend_count", len(stats.Frontends))
}

// Disconnect closes a WebSocket connection.
func (m *WebSocketManager) Disconnect(serverID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, exists := m.connections[serverID]
	if !exists {
		return
	}

	m.logger.Info("stopping WebSocket connection", "server_id", serverID)

	// Cancel context and stop client
	conn.cancel()
	conn.client.Stop()

	delete(m.connections, serverID)
}

// Reconnect closes and reopens a WebSocket connection.
func (m *WebSocketManager) Reconnect(serverConfig models.BoxConfig) error {
	m.Disconnect(serverConfig.ID)
	return m.Connect(serverConfig)
}

// StopAll closes all WebSocket connections.
func (m *WebSocketManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("stopping all WebSocket connections")

	// Cancel root context
	m.cancel()

	// Stop all clients
	for serverID, conn := range m.connections {
		m.logger.Debug("stopping WebSocket", "server_id", serverID)
		conn.client.Stop()
	}

	m.connections = make(map[string]*WSConnection)
}

// IsConnected returns whether a server has an active WebSocket connection.
func (m *WebSocketManager) IsConnected(serverID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.connections[serverID]
	if !exists {
		return false
	}

	return conn.client.IsConnected()
}

// ConnectionCount returns the number of active connections.
func (m *WebSocketManager) ConnectionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.connections)
}

// GetConnectionStatus returns connection status for all servers.
func (m *WebSocketManager) GetConnectionStatus() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]bool, len(m.connections))
	for serverID, conn := range m.connections {
		status[serverID] = conn.client.IsConnected()
	}
	return status
}

// updateMetricsFromEvent updates the cache with metrics from a WebSocket event.
// This avoids a redundant API call to the agent since it already sent the metrics.
func (m *WebSocketManager) updateMetricsFromEvent(serverID string, eventData map[string]interface{}) {
	manager, exists := m.registry.GetCollector(serverID)
	if !exists {
		m.logger.Debug("cannot update metrics: collector not found",
			"server_id", serverID)
		return
	}

	// Extract metrics from event data
	metricsRaw, ok := eventData["metrics"]
	if !ok {
		m.logger.Debug("metrics not found in event data, falling back to API call")
		// Fallback to fetching from API
		if err := manager.RefreshMetrics(); err != nil {
			m.logger.Debug("failed to refresh metrics",
				"server_id", serverID,
				"error", err)
		}
		return
	}

	// Convert the metrics to SystemMetrics
	metrics, err := convertEventDataToMetrics(metricsRaw, eventData)
	if err != nil {
		m.logger.Warn("failed to convert event metrics, falling back to API call",
			"server_id", serverID,
			"error", err)
		// Fallback to fetching from API
		if err := manager.RefreshMetrics(); err != nil {
			m.logger.Debug("failed to refresh metrics",
				"server_id", serverID,
				"error", err)
		}
		return
	}

	// Update cache directly
	manager.GetCache().SetSystemMetrics(metrics)
	m.logger.Debug("updated metrics from WebSocket event", "server_id", serverID)
}

// convertEventDataToStats converts the raw stats from a WebSocket event to HAProxyStats.
// The stats are sent as a nested map structure from the agent's JSON serialization.
func convertEventDataToStats(statsRaw interface{}) (*models.HAProxyStats, error) {
	// The stats come as map[string]interface{} from JSON unmarshaling
	// We need to marshal it back to JSON and unmarshal into HAProxyStats
	statsBytes, err := json.Marshal(statsRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal stats: %w", err)
	}

	var stats models.HAProxyStats
	if err := json.Unmarshal(statsBytes, &stats); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stats: %w", err)
	}

	return &stats, nil
}

// convertEventDataToMetrics converts the raw metrics from a WebSocket event to SystemMetrics.
// The agent sends service_statuses separately in the event data, not inside the metrics object.
func convertEventDataToMetrics(metricsRaw interface{}, eventData map[string]interface{}) (*models.SystemMetrics, error) {
	metricsBytes, err := json.Marshal(metricsRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metrics: %w", err)
	}

	var metrics models.SystemMetrics
	if err := json.Unmarshal(metricsBytes, &metrics); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
	}

	// Extract service_statuses from event data (if present)
	if serviceStatusesRaw, ok := eventData["service_statuses"]; ok && serviceStatusesRaw != nil {
		// Convert []ServiceStatus to map[string]ServiceStatus
		var serviceStatusList []models.ServiceStatus
		serviceStatusBytes, err := json.Marshal(serviceStatusesRaw)
		if err == nil {
			if err := json.Unmarshal(serviceStatusBytes, &serviceStatusList); err == nil {
				metrics.ServiceStatuses = make(map[string]models.ServiceStatus)
				for _, status := range serviceStatusList {
					metrics.ServiceStatuses[status.Name] = status
				}
			}
		}
	}

	return &metrics, nil
}

// updateCertificatesFromEvent updates the cache with certificates from a WebSocket event.
// This avoids a redundant API call to the agent since it already sent the certificates.
func (m *WebSocketManager) updateCertificatesFromEvent(serverID string, eventData map[string]interface{}) {
	manager, exists := m.registry.GetCollector(serverID)
	if !exists {
		m.logger.Debug("cannot update certificates: collector not found",
			"server_id", serverID)
		return
	}

	// The agent sends the entire CertificateMetrics in the event data
	// Convert the event data to CertificateMetrics
	certificates, err := convertEventDataToCertificates(eventData)
	if err != nil {
		m.logger.Warn("failed to convert event certificates",
			"server_id", serverID,
			"error", err)
		return
	}

	// Update cache directly
	manager.GetCache().SetCertificates(certificates)
	m.logger.Debug("updated certificates from WebSocket event",
		"server_id", serverID,
		"certificate_count", len(certificates.Certificates))
}

// convertEventDataToCertificates converts the raw event data to CertificateMetrics.
func convertEventDataToCertificates(eventData map[string]interface{}) (*models.CertificateMetrics, error) {
	// The entire event data represents the CertificateMetrics
	dataBytes, err := json.Marshal(eventData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal certificate data: %w", err)
	}

	var certificates models.CertificateMetrics
	if err := json.Unmarshal(dataBytes, &certificates); err != nil {
		return nil, fmt.Errorf("failed to unmarshal certificate data: %w", err)
	}

	return &certificates, nil
}
