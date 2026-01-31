package services

import (
	"github.com/sarg3nt/gearbox/internal/framework/events"
	"github.com/sarg3nt/gearbox/internal/framework/plugin"
)

// EventsAdapter wraps the events.Hub to implement plugin.EventPublisher.
type EventsAdapter struct {
	hub *events.Hub
}

// NewEventsAdapter creates a new EventsAdapter wrapping an existing events.Hub.
func NewEventsAdapter(hub *events.Hub) *EventsAdapter {
	return &EventsAdapter{hub: hub}
}

// Publish broadcasts an event to all subscribers.
func (e *EventsAdapter) Publish(event plugin.Event) {
	// Convert plugin.Event to events.Event
	hubEvent := events.Event{
		Type:     events.EventType(event.Type),
		ServerID: event.ServerID,
		Data:     event.Data,
	}
	e.hub.Publish(hubEvent)
}

// Subscribe registers a handler for events of a specific type.
// Returns a function to unsubscribe.
func (e *EventsAdapter) Subscribe(eventType string, handler func(plugin.Event)) func() {
	// Create a unique subscriber ID
	sub := e.hub.Subscribe(eventType, "")

	// Start a goroutine to forward events
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case hubEvent, ok := <-sub.Events:
				if !ok {
					return
				}
				// Only forward events of the requested type
				if string(hubEvent.Type) == eventType {
					handler(plugin.Event{
						Type:     string(hubEvent.Type),
						ServerID: hubEvent.ServerID,
						Data:     hubEvent.Data,
					})
				}
			}
		}
	}()

	// Return unsubscribe function
	return func() {
		close(done)
		e.hub.Unsubscribe(sub)
	}
}

// Ensure EventsAdapter implements plugin.EventPublisher
var _ plugin.EventPublisher = (*EventsAdapter)(nil)
