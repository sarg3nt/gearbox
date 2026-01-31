// Package events provides real-time event distribution for the monitoring application.
package events

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// EventType represents the type of event.
type EventType string

const (
	// EventTypeConfigChanged indicates HAProxy configuration has changed.
	EventTypeConfigChanged EventType = "config.changed"

	// EventTypeSyncStarted indicates a git sync has started.
	EventTypeSyncStarted EventType = "sync.started"

	// EventTypeSyncCompleted indicates a git sync has completed.
	EventTypeSyncCompleted EventType = "sync.completed"

	// EventTypeSyncFailed indicates a git sync has failed.
	EventTypeSyncFailed EventType = "sync.failed"

	// EventTypeWebhookReceived indicates a webhook was received.
	EventTypeWebhookReceived EventType = "webhook.received"

	// EventTypeServerConnected indicates WebSocket connected to a server.
	EventTypeServerConnected EventType = "server.connected"

	// EventTypeServerDisconnected indicates WebSocket disconnected from a server.
	EventTypeServerDisconnected EventType = "server.disconnected"

	// EventTypeMetadataRefreshed indicates metadata was refreshed.
	EventTypeMetadataRefreshed EventType = "metadata.refreshed"

	// EventTypeStatsUpdated indicates HAProxy stats have been updated.
	EventTypeStatsUpdated EventType = "stats.updated"

	// EventTypeMetricsUpdated indicates system metrics have been updated.
	EventTypeMetricsUpdated EventType = "metrics.updated"

	// EventTypeLogsUpdated indicates new log lines are available.
	EventTypeLogsUpdated EventType = "logs.updated"

	// EventTypeCertificatesUpdated indicates certificate metrics have been updated.
	EventTypeCertificatesUpdated EventType = "certificates.updated"

	// EventTypeAlertTriggered indicates a new alert has been triggered.
	EventTypeAlertTriggered EventType = "alert.triggered"

	// EventTypeAlertAcknowledged indicates an alert has been acknowledged.
	EventTypeAlertAcknowledged EventType = "alert.acknowledged"

	// EventTypeAlertResolved indicates an alert has been resolved.
	EventTypeAlertResolved EventType = "alert.resolved"

	// EventTypeAlertSilenced indicates an alert has been silenced.
	EventTypeAlertSilenced EventType = "alert.silenced"

	// EventTypeAlertCountUpdated indicates the active alert count has changed.
	EventTypeAlertCountUpdated EventType = "alert.count_updated"

	// EventTypeAptStarted indicates an apt operation has started.
	EventTypeAptStarted EventType = "apt.started"

	// EventTypeAptOutput indicates apt command output line.
	EventTypeAptOutput EventType = "apt.output"

	// EventTypeAptCompleted indicates an apt operation has completed.
	EventTypeAptCompleted EventType = "apt.completed"

	// EventTypeAptFailed indicates an apt operation has failed.
	EventTypeAptFailed EventType = "apt.failed"
)

// Event represents an event to be broadcast to clients.
type Event struct {
	Type      EventType              `json:"type"`
	ServerID  string                 `json:"server_id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// Subscriber represents a client subscribed to events.
type Subscriber struct {
	ID       string
	ServerID string // Optional: filter events for specific server
	Events   chan Event
	done     chan struct{}
}

// Hub manages event distribution to subscribers.
type Hub struct {
	subscribers map[string]*Subscriber
	mu          sync.RWMutex
	logger      *slog.Logger
	broadcast   chan Event
	register    chan *Subscriber
	unregister  chan *Subscriber
	done        chan struct{}
}

// NewHub creates a new event hub.
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}

	h := &Hub{
		subscribers: make(map[string]*Subscriber),
		logger:      logger,
		broadcast:   make(chan Event, 100),
		register:    make(chan *Subscriber),
		unregister:  make(chan *Subscriber),
		done:        make(chan struct{}),
	}

	return h
}

// Start begins processing events.
func (h *Hub) Start() {
	go h.run()
}

// Stop gracefully shuts down the hub.
func (h *Hub) Stop() {
	close(h.done)
}

// run is the main event loop.
func (h *Hub) run() {
	for {
		select {
		case <-h.done:
			// Close all subscriber channels
			h.mu.Lock()
			for _, sub := range h.subscribers {
				close(sub.Events)
			}
			h.subscribers = make(map[string]*Subscriber)
			h.mu.Unlock()
			return

		case sub := <-h.register:
			h.mu.Lock()
			h.subscribers[sub.ID] = sub
			h.mu.Unlock()
			h.logger.Debug("subscriber registered", "id", sub.ID, "server_filter", sub.ServerID)

		case sub := <-h.unregister:
			h.mu.Lock()
			if _, exists := h.subscribers[sub.ID]; exists {
				close(sub.Events)
				delete(h.subscribers, sub.ID)
				h.logger.Debug("subscriber unregistered", "id", sub.ID)
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.mu.RLock()
			for _, sub := range h.subscribers {
				// Filter by server if subscriber has a filter
				if sub.ServerID != "" && event.ServerID != "" && sub.ServerID != event.ServerID {
					continue
				}

				// Non-blocking send
				select {
				case sub.Events <- event:
				default:
					// Channel full, skip this event for this subscriber
					h.logger.Warn("subscriber event channel full, dropping event",
						"subscriber_id", sub.ID,
						"event_type", event.Type)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Subscribe creates a new subscriber.
func (h *Hub) Subscribe(id string, serverID string) *Subscriber {
	sub := &Subscriber{
		ID:       id,
		ServerID: serverID,
		Events:   make(chan Event, 50),
		done:     make(chan struct{}),
	}

	h.register <- sub
	return sub
}

// Unsubscribe removes a subscriber.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	select {
	case h.unregister <- sub:
	case <-h.done:
	}
}

// Publish broadcasts an event to all subscribers.
func (h *Hub) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	select {
	case h.broadcast <- event:
	case <-h.done:
	default:
		// Broadcast channel full
		h.logger.Warn("broadcast channel full, dropping event", "event_type", event.Type)
	}
}

// PublishConfigChanged publishes a config changed event.
func (h *Hub) PublishConfigChanged(serverID string, commitSHA string, backendCount int) {
	h.Publish(Event{
		Type:      EventTypeConfigChanged,
		ServerID:  serverID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"commit_sha":    commitSHA,
			"backend_count": backendCount,
		},
	})
}

// PublishServerConnected publishes a server connected event.
func (h *Hub) PublishServerConnected(serverID string, serverName string) {
	h.Publish(Event{
		Type:      EventTypeServerConnected,
		ServerID:  serverID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"server_name": serverName,
		},
	})
}

// PublishServerDisconnected publishes a server disconnected event.
func (h *Hub) PublishServerDisconnected(serverID string, serverName string) {
	h.Publish(Event{
		Type:      EventTypeServerDisconnected,
		ServerID:  serverID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"server_name": serverName,
		},
	})
}

// PublishMetadataRefreshed publishes a metadata refreshed event.
func (h *Hub) PublishMetadataRefreshed(serverID string) {
	h.Publish(Event{
		Type:      EventTypeMetadataRefreshed,
		ServerID:  serverID,
		Timestamp: time.Now(),
	})
}

// SubscriberCount returns the number of active subscribers.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// PublishAlertTriggered publishes an alert triggered event.
func (h *Hub) PublishAlertTriggered(serverID string, alertID int64, title, message, severity string) {
	h.Publish(Event{
		Type:      EventTypeAlertTriggered,
		ServerID:  serverID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"alert_id": alertID,
			"title":    title,
			"message":  message,
			"severity": severity,
		},
	})
}

// PublishAlertAcknowledged publishes an alert acknowledged event.
func (h *Hub) PublishAlertAcknowledged(serverID string, alertID int64, title string) {
	h.Publish(Event{
		Type:      EventTypeAlertAcknowledged,
		ServerID:  serverID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"alert_id": alertID,
			"title":    title,
		},
	})
}

// PublishAlertResolved publishes an alert resolved event.
func (h *Hub) PublishAlertResolved(serverID string, alertID int64, title string) {
	h.Publish(Event{
		Type:      EventTypeAlertResolved,
		ServerID:  serverID,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"alert_id": alertID,
			"title":    title,
		},
	})
}

// PublishAlertCountUpdated publishes an alert count updated event.
func (h *Hub) PublishAlertCountUpdated(activeCount int, criticalCount int) {
	h.Publish(Event{
		Type:      EventTypeAlertCountUpdated,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"active_count":   activeCount,
			"critical_count": criticalCount,
		},
	})
}

// MarshalEvent converts an event to JSON for SSE.
func MarshalEvent(event Event) ([]byte, error) {
	return json.Marshal(event)
}
