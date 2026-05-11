package middleware

import (
	"testing"
	"time"
)

// 2026-05 audit P1-7. The tracker should:
// 1. Not block an IP until it exceeds the threshold within the window.
// 2. Block for at least baseDelay once the threshold is exceeded.
// 3. Grow the block window exponentially with continued failures.
// 4. Reset on successful auth.
// 5. Forget failures that fall outside the sliding window.
// 6. Bound its own memory.

func TestBackoffTracker_BelowThreshold(t *testing.T) {
	bt := NewBackoffTracker(3, 5*time.Minute, 10*time.Second, 30*time.Minute, nil)
	defer bt.Close()

	for i := 0; i < 3; i++ {
		bt.RecordFailure("10.0.0.1")
		if bt.IsBlocked("10.0.0.1") {
			t.Fatalf("IsBlocked=true at failure %d; want false (still at threshold)", i+1)
		}
	}
}

func TestBackoffTracker_BlocksPastThreshold(t *testing.T) {
	bt := NewBackoffTracker(3, 5*time.Minute, 10*time.Second, 30*time.Minute, nil)
	defer bt.Close()

	for i := 0; i < 4; i++ {
		bt.RecordFailure("10.0.0.1")
	}
	if !bt.IsBlocked("10.0.0.1") {
		t.Errorf("IsBlocked=false after 4 failures; want true")
	}
}

func TestBackoffTracker_ExponentialGrowth(t *testing.T) {
	bt := NewBackoffTracker(3, 5*time.Minute, 10*time.Second, 30*time.Minute, nil)
	defer bt.Close()

	// 4 failures → baseDelay (10s, 2^0=1).
	for i := 0; i < 4; i++ {
		bt.RecordFailure("10.0.0.1")
	}
	rec := bt.failures["10.0.0.1"]
	delay := time.Until(rec.blockedUntil)
	if delay < 9*time.Second || delay > 11*time.Second {
		t.Errorf("4th failure: delay=%v, want ~10s", delay)
	}

	// 5th failure: baseDelay << 1 = 20s.
	bt.RecordFailure("10.0.0.1")
	delay = time.Until(bt.failures["10.0.0.1"].blockedUntil)
	if delay < 19*time.Second || delay > 21*time.Second {
		t.Errorf("5th failure: delay=%v, want ~20s", delay)
	}

	// 6th failure: 40s.
	bt.RecordFailure("10.0.0.1")
	delay = time.Until(bt.failures["10.0.0.1"].blockedUntil)
	if delay < 39*time.Second || delay > 41*time.Second {
		t.Errorf("6th failure: delay=%v, want ~40s", delay)
	}
}

func TestBackoffTracker_CapAtMaxDelay(t *testing.T) {
	// baseDelay 10s, maxDelay 30s. After threshold + many failures the delay
	// must not exceed 30s.
	bt := NewBackoffTracker(3, 5*time.Minute, 10*time.Second, 30*time.Second, nil)
	defer bt.Close()

	for i := 0; i < 20; i++ {
		bt.RecordFailure("10.0.0.1")
	}
	delay := time.Until(bt.failures["10.0.0.1"].blockedUntil)
	if delay > 31*time.Second {
		t.Errorf("delay=%v exceeds maxDelay 30s", delay)
	}
}

func TestBackoffTracker_ResetClearsBlock(t *testing.T) {
	bt := NewBackoffTracker(3, 5*time.Minute, 10*time.Second, 30*time.Minute, nil)
	defer bt.Close()

	for i := 0; i < 4; i++ {
		bt.RecordFailure("10.0.0.1")
	}
	if !bt.IsBlocked("10.0.0.1") {
		t.Fatal("setup: IP should be blocked")
	}
	bt.Reset("10.0.0.1")
	if bt.IsBlocked("10.0.0.1") {
		t.Errorf("IsBlocked=true after Reset; want false")
	}
}

func TestBackoffTracker_BoundedMap(t *testing.T) {
	bt := NewBackoffTracker(3, 5*time.Minute, 10*time.Second, 30*time.Minute, nil)
	defer bt.Close()
	bt.maxEntries = 100

	// Fill the map with 100 distinct IPs (one failure each — not enough to
	// block any of them, but the records exist).
	for i := 0; i < 100; i++ {
		ip := makeIP(i)
		bt.RecordFailure(ip)
	}
	if len(bt.failures) != 100 {
		t.Fatalf("map size = %d; want 100", len(bt.failures))
	}

	// The 101st distinct IP must NOT cause map growth. The implementation
	// drops the record rather than expanding (rate-limiting in front is
	// the other backstop).
	bt.RecordFailure("10.99.99.99")
	if len(bt.failures) > 100 {
		t.Errorf("map size = %d after overflow; want <=100", len(bt.failures))
	}
}

func makeIP(i int) string {
	return "10.0." + itoaPad(i/256) + "." + itoaPad(i%256)
}

func itoaPad(n int) string {
	if n < 10 {
		return string(rune('0'+n%10)) // for the small numbers we use in tests
	}
	// We never actually hit this branch with 100 entries (only 0..99), but
	// keep it correct in case the test is extended later.
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
