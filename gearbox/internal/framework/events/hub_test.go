package events

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestHub returns a Hub backed by a buffered slog handler so tests can
// inspect emitted warnings.
func newTestHub(t *testing.T) (*Hub, *bytes.Buffer, *sync.Mutex) {
	t.Helper()
	var buf bytes.Buffer
	var mu sync.Mutex
	handler := slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)
	return NewHub(logger), &buf, &mu
}

// lockedWriter serialises writes from goroutines that share the slog handler
// so the buffer doesn't get torn output under -race.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func countLines(buf *bytes.Buffer, mu *sync.Mutex, substr string) int {
	mu.Lock()
	defer mu.Unlock()
	return strings.Count(buf.String(), substr)
}

func TestSubscriberEventBufferIsLarge(t *testing.T) {
	hub, _, _ := newTestHub(t)
	hub.Start()
	defer hub.Stop()

	sub := hub.Subscribe("sub-1", "")
	if got := cap(sub.Events); got != subscriberEventBufferSize {
		t.Fatalf("subscriber event channel capacity = %d, want %d", got, subscriberEventBufferSize)
	}
}

func TestRecordDropFirstDropLogsImmediately(t *testing.T) {
	hub, buf, mu := newTestHub(t)
	t0 := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	hub.recordDropAt("sub-1", EventTypeLogsUpdated, t0)

	if got := countLines(buf, mu, "subscriber event channel full"); got != 1 {
		t.Fatalf("expected 1 warning on first drop, got %d. log=%q", got, buf.String())
	}
}

func TestRecordDropCoalescesWithinWindow(t *testing.T) {
	hub, buf, mu := newTestHub(t)
	t0 := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	// First drop logs.
	hub.recordDropAt("sub-1", EventTypeLogsUpdated, t0)
	// 99 more drops within the window — should not log again.
	for i := 1; i <= 99; i++ {
		hub.recordDropAt("sub-1", EventTypeLogsUpdated, t0.Add(time.Duration(i)*time.Millisecond))
	}

	if got := countLines(buf, mu, "subscriber event channel full"); got != 1 {
		t.Fatalf("expected 1 coalesced warning, got %d. log=%q", got, buf.String())
	}
}

func TestRecordDropEmitsAfterIntervalWithCount(t *testing.T) {
	hub, buf, mu := newTestHub(t)
	t0 := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	hub.recordDropAt("sub-1", EventTypeLogsUpdated, t0)
	for i := 1; i <= 49; i++ {
		hub.recordDropAt("sub-1", EventTypeLogsUpdated, t0.Add(time.Duration(i)*time.Millisecond))
	}
	// Cross the interval: the next drop should log again, this time with
	// dropped=50 (the 49 coalesced + this one).
	hub.recordDropAt("sub-1", EventTypeLogsUpdated, t0.Add(dropLogInterval+time.Millisecond))

	if got := countLines(buf, mu, "subscriber event channel full"); got != 2 {
		t.Fatalf("expected 2 warnings after crossing interval, got %d. log=%q", got, buf.String())
	}
	if !strings.Contains(buf.String(), "dropped=50") {
		t.Fatalf("expected coalesced count of 50 in second warning, log=%q", buf.String())
	}
}

func TestRecordDropPerSubscriberPerEventTypeIsIndependent(t *testing.T) {
	hub, buf, mu := newTestHub(t)
	t0 := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	// Three distinct (subscriber, event_type) streams in the same instant —
	// each should produce its own first-drop warning.
	hub.recordDropAt("sub-1", EventTypeLogsUpdated, t0)
	hub.recordDropAt("sub-1", EventTypeMetricsUpdated, t0)
	hub.recordDropAt("sub-2", EventTypeLogsUpdated, t0)

	if got := countLines(buf, mu, "subscriber event channel full"); got != 3 {
		t.Fatalf("expected 3 independent first-drop warnings, got %d. log=%q", got, buf.String())
	}
}

func TestCleanupDropTrackerRemovesSubscriberEntries(t *testing.T) {
	hub, _, _ := newTestHub(t)
	t0 := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)

	hub.recordDropAt("sub-1", EventTypeLogsUpdated, t0)
	hub.recordDropAt("sub-1", EventTypeMetricsUpdated, t0)
	hub.recordDropAt("sub-2", EventTypeLogsUpdated, t0)

	if got := len(hub.subscriberDrops); got != 3 {
		t.Fatalf("expected 3 tracker entries, got %d", got)
	}

	hub.cleanupDropTracker("sub-1")

	if got := len(hub.subscriberDrops); got != 1 {
		t.Fatalf("expected 1 tracker entry after cleanup, got %d", got)
	}
	if _, ok := hub.subscriberDrops[dropKey{subID: "sub-2", eventType: EventTypeLogsUpdated}]; !ok {
		t.Fatal("sub-2 entry should have survived cleanup of sub-1")
	}
}

func TestSlowSubscriberDropsAreCoalesced(t *testing.T) {
	// End-to-end through the broadcast loop: a subscriber that never drains
	// will overflow, but we should still emit only one warning per event-type
	// during the burst (no clock fast-forwarding here, so we stay inside
	// dropLogInterval).
	hub, buf, mu := newTestHub(t)
	hub.Start()
	defer hub.Stop()

	sub := hub.Subscribe("slow-sub", "")
	// Never read sub.Events.
	_ = sub

	// Publish well past the buffer size so a flood of drops occurs.
	for i := 0; i < subscriberEventBufferSize*3; i++ {
		hub.Publish(Event{Type: EventTypeLogsUpdated})
	}

	// Let the broadcast loop drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countLines(buf, mu, "subscriber event channel full") >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got := countLines(buf, mu, "subscriber event channel full")
	if got == 0 {
		t.Fatalf("expected at least one drop warning under burst, got none. log=%q", buf.String())
	}
	// Under the throttle, a single burst should produce far fewer warnings
	// than dropped events. Allow some slack but assert it's not 1-per-drop.
	if got > 5 {
		t.Fatalf("expected coalesced warning(s), got %d (no throttling?). log=%q", got, buf.String())
	}
}
