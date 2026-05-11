package middleware

import (
	"log/slog"
	"sync"
	"time"
)

// 2026-05 security audit, P1-7.
//
// BackoffTracker imposes exponential backoff on a per-IP basis when API-key
// auth fails. The agent's existing rate limiter (50 req/sec/IP) makes a
// brute-force on a 256-bit hex API key mathematically infeasible from one
// IP, but offers no defense against distributed attempts across many IPs
// or against an attacker who pairs auth probing with other endpoints to
// stay under the global limit.
//
// On each failed auth, the per-IP failure count is incremented. Once the
// count exceeds `threshold` within `window`, that IP is blocked for an
// exponentially growing duration (baseDelay × 2^(extra-failures)),
// capped at maxDelay. A successful auth resets the IP's record.
//
// The tracker bounds its own map (same defense as the rate limiter,
// P1-6) so a botnet spraying unique IPs can't grow memory unbounded.
type BackoffTracker struct {
	mu          sync.Mutex
	failures    map[string]*failureRecord
	threshold   int           // Allowed failures within window before blocking starts
	window      time.Duration // Sliding window for counting failures
	baseDelay   time.Duration // Initial block duration after threshold reached
	maxDelay    time.Duration // Cap on exponential growth
	maxEntries  int           // Bounded map
	logger      *slog.Logger
	stopCleanup chan struct{}
}

type failureRecord struct {
	count           int
	firstFailure   time.Time
	lastFailure    time.Time
	blockedUntil   time.Time
}

// NewBackoffTracker creates a BackoffTracker. Reasonable defaults:
//
//	threshold = 3 failures
//	window    = 5 minutes
//	baseDelay = 10 seconds
//	maxDelay  = 30 minutes
func NewBackoffTracker(threshold int, window, baseDelay, maxDelay time.Duration, logger *slog.Logger) *BackoffTracker {
	bt := &BackoffTracker{
		failures:    make(map[string]*failureRecord),
		threshold:   threshold,
		window:      window,
		baseDelay:   baseDelay,
		maxDelay:    maxDelay,
		maxEntries:  10000, // mirrors maxRateLimitClients in ratelimit.go
		logger:      logger,
		stopCleanup: make(chan struct{}),
	}
	go bt.cleanupLoop()
	return bt
}

// DefaultBackoffTracker returns a tracker with the reasonable defaults above.
func DefaultBackoffTracker(logger *slog.Logger) *BackoffTracker {
	return NewBackoffTracker(3, 5*time.Minute, 10*time.Second, 30*time.Minute, logger)
}

// Close stops the cleanup goroutine.
func (b *BackoffTracker) Close() {
	close(b.stopCleanup)
}

// IsBlocked reports whether the IP is currently in a backoff window.
// Call this BEFORE attempting authentication.
func (b *BackoffTracker) IsBlocked(ip string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	rec, ok := b.failures[ip]
	if !ok {
		return false
	}
	return time.Now().Before(rec.blockedUntil)
}

// RecordFailure increments the IP's failure counter and, if past the
// threshold, computes a new blockedUntil with exponential backoff.
func (b *BackoffTracker) RecordFailure(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	rec, ok := b.failures[ip]
	if !ok {
		// New entry. Check bounded-map cap; evict stale before allocating.
		if b.maxEntries > 0 && len(b.failures) >= b.maxEntries {
			b.evictStaleLocked(now)
			if len(b.failures) >= b.maxEntries {
				// Map full of fresh failures. We silently drop the record
				// for THIS IP; rate-limiting (in front of the auth check)
				// is the other backstop.
				return
			}
		}
		b.failures[ip] = &failureRecord{
			count:        1,
			firstFailure: now,
			lastFailure:  now,
		}
		return
	}

	// If the previous failure was outside the sliding window, reset the
	// count rather than continuing exponential growth.
	if now.Sub(rec.firstFailure) > b.window {
		rec.count = 0
		rec.firstFailure = now
		rec.blockedUntil = time.Time{}
	}
	rec.count++
	rec.lastFailure = now

	if rec.count > b.threshold {
		extra := rec.count - b.threshold
		// baseDelay * 2^(extra-1); cap at maxDelay.
		delay := b.baseDelay << uint(extra-1)
		if delay <= 0 || delay > b.maxDelay {
			delay = b.maxDelay
		}
		rec.blockedUntil = now.Add(delay)
		if b.logger != nil {
			b.logger.Warn("Auth backoff: IP blocked",
				"ip", ip,
				"failures", rec.count,
				"blocked_for", delay.String(),
			)
		}
	}
}

// Reset clears the IP's record. Call on successful authentication.
func (b *BackoffTracker) Reset(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.failures, ip)
}

// evictStaleLocked drops failure records that are older than the window AND
// not currently blocked. Caller MUST hold b.mu.
func (b *BackoffTracker) evictStaleLocked(now time.Time) {
	for ip, rec := range b.failures {
		if now.Sub(rec.lastFailure) > b.window && now.After(rec.blockedUntil) {
			delete(b.failures, ip)
		}
	}
}

// cleanupLoop periodically drops expired entries.
func (b *BackoffTracker) cleanupLoop() {
	ticker := time.NewTicker(b.window) // run as often as the window itself
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.mu.Lock()
			b.evictStaleLocked(time.Now())
			b.mu.Unlock()
		case <-b.stopCleanup:
			return
		}
	}
}
