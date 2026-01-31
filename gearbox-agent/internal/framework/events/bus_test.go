package events

import (
	"testing"
	"time"
)

func TestNewBus(t *testing.T) {
	bus := NewBus()
	if bus == nil {
		t.Fatal("NewBus() returned nil")
	}
	if bus.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount() = %d, want 0", bus.SubscriberCount())
	}
}

func TestSubscribeUnsubscribe(t *testing.T) {
	bus := NewBus()

	sub := bus.Subscribe()
	if sub == nil {
		t.Fatal("Subscribe() returned nil")
	}
	if bus.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1", bus.SubscriberCount())
	}

	bus.Unsubscribe(sub)
	if bus.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount() after unsubscribe = %d, want 0", bus.SubscriberCount())
	}
}

func TestPublish(t *testing.T) {
	bus := NewBus()
	sub := bus.Subscribe()

	event := NewEvent(EventSyncStarted, map[string]interface{}{"test": "value"})
	bus.Publish(event)

	select {
	case received := <-sub:
		if received.Type != EventSyncStarted {
			t.Errorf("Event type = %s, want %s", received.Type, EventSyncStarted)
		}
		if received.Data["test"] != "value" {
			t.Errorf("Event data = %v, want test:value", received.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for event")
	}

	bus.Unsubscribe(sub)
}

func TestPublishMultipleSubscribers(t *testing.T) {
	bus := NewBus()
	sub1 := bus.Subscribe()
	sub2 := bus.Subscribe()

	event := NewEvent(EventConfigChanged, nil)
	bus.Publish(event)

	// Both subscribers should receive the event
	for i, sub := range []Subscriber{sub1, sub2} {
		select {
		case received := <-sub:
			if received.Type != EventConfigChanged {
				t.Errorf("Subscriber %d: Event type = %s, want %s", i, received.Type, EventConfigChanged)
			}
		case <-time.After(time.Second):
			t.Fatalf("Subscriber %d: Timed out waiting for event", i)
		}
	}

	bus.Unsubscribe(sub1)
	bus.Unsubscribe(sub2)
}

func TestClose(t *testing.T) {
	bus := NewBus()
	sub := bus.Subscribe()

	bus.Close()

	// Subscriber channel should be closed
	select {
	case _, ok := <-sub:
		if ok {
			t.Error("Expected subscriber channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for channel close")
	}

	// Subscribe after close should return nil
	newSub := bus.Subscribe()
	if newSub != nil {
		t.Error("Subscribe() after Close() should return nil")
	}
}

func TestEventJSON(t *testing.T) {
	event := NewEvent(EventSyncCompleted, map[string]interface{}{
		"backend_count": 5,
	})

	data, err := event.JSON()
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}

	if len(data) == 0 {
		t.Error("JSON() returned empty data")
	}
}
