// Package events provides an internal event bus for pub/sub messaging.
package events

import (
	"encoding/json"
	"sync"
	"time"
)

// EventType represents the type of event.
type EventType string

const (
	// EventSyncStarted is emitted when a sync cycle begins.
	EventSyncStarted EventType = "sync.started"
	// EventSyncCompleted is emitted when a sync cycle completes successfully.
	EventSyncCompleted EventType = "sync.completed"
	// EventSyncFailed is emitted when a sync cycle fails.
	EventSyncFailed EventType = "sync.failed"
	// EventConfigChanged is emitted when HAProxy config is updated.
	EventConfigChanged EventType = "config.changed"
	// EventWebhookReceived is emitted when a webhook is received.
	EventWebhookReceived EventType = "webhook.received"
	// EventStatsUpdated is emitted periodically with HAProxy stats.
	EventStatsUpdated EventType = "stats.updated"
	// EventMetricsUpdated is emitted periodically with system metrics.
	EventMetricsUpdated EventType = "metrics.updated"
	// EventLogsUpdated is emitted when new log lines are available.
	EventLogsUpdated EventType = "logs.updated"
	// EventCertificatesUpdated is emitted periodically with certificate metrics.
	EventCertificatesUpdated EventType = "certificates.updated"

	// EventAptStarted is emitted when a package manager operation begins.
	// Kept as "apt.started" for backwards compatibility with existing dashboard clients.
	EventAptStarted EventType = "apt.started"
	// EventAptOutput is emitted for each line of package manager stdout/stderr.
	EventAptOutput EventType = "apt.output"
	// EventAptCompleted is emitted when a package manager operation finishes successfully.
	EventAptCompleted EventType = "apt.completed"
	// EventAptFailed is emitted when a package manager operation fails.
	EventAptFailed EventType = "apt.failed"

	// EventConsoleSessionStart is emitted when a remote-console session
	// opens (after token validation + WebSocket upgrade). Payload includes
	// the session ID, effective UID, remote address, mode, and start time.
	// This is the load-bearing audit record — every shell session must
	// emit one. See [#89].
	EventConsoleSessionStart EventType = "console.session.start"
	// EventConsoleSessionEnd is emitted when a remote-console session
	// closes for any reason (client disconnect, exit, error, idle
	// timeout). Payload includes the matching session ID, byte counts in
	// both directions, exit reason, and duration.
	EventConsoleSessionEnd EventType = "console.session.end"
	// EventConsoleConfigChange is emitted when console settings are
	// modified at the agent (currently env-driven; reserved for future
	// API-driven config). Operators rely on this to detect surprise
	// console-enabling.
	EventConsoleConfigChange EventType = "console.config.change"
)

// Event represents an event in the system.
type Event struct {
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// NewEvent creates a new event with the given type and optional data.
func NewEvent(eventType EventType, data map[string]any) Event {
	return Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
}

// JSON returns the event as a JSON byte slice.
func (e Event) JSON() ([]byte, error) {
	return json.Marshal(e)
}

// Subscriber is a channel that receives events.
type Subscriber chan Event

// Bus is a simple pub/sub event bus.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[Subscriber]struct{}
	closed      bool
}

// NewBus creates a new event bus.
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[Subscriber]struct{}),
	}
}

// Subscribe creates a new subscriber and returns its channel.
// The subscriber should call Unsubscribe when done to prevent leaks.
func (b *Bus) Subscribe() Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	// Buffered channel to prevent blocking on slow subscribers
	sub := make(Subscriber, 100)
	b.subscribers[sub] = struct{}{}
	return sub
}

// Unsubscribe removes a subscriber from the bus.
func (b *Bus) Unsubscribe(sub Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[sub]; ok {
		delete(b.subscribers, sub)
		close(sub)
	}
}

// Publish sends an event to all subscribers.
// Non-blocking: if a subscriber's buffer is full, the event is dropped for that subscriber.
func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for sub := range b.subscribers {
		select {
		case sub <- event:
		default:
			// Subscriber buffer full, skip
		}
	}
}

// PublishAsync publishes an event asynchronously.
func (b *Bus) PublishAsync(event Event) {
	go b.Publish(event)
}

// SubscriberCount returns the number of active subscribers.
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Close closes the event bus and all subscriber channels.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true
	for sub := range b.subscribers {
		close(sub)
		delete(b.subscribers, sub)
	}
}
