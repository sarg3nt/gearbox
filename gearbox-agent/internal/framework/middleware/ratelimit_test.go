package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, 20, false, nil)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	if rl.rate != 10 {
		t.Errorf("rate = %d, want 10", rl.rate)
	}
	if rl.burst != 20 {
		t.Errorf("burst = %d, want 20", rl.burst)
	}
}

func TestRateLimiter_Allow_NewClient(t *testing.T) {
	rl := NewRateLimiter(10, 5, false, nil)

	// New client should be allowed
	if !rl.Allow("192.168.1.1") {
		t.Error("New client should be allowed")
	}
}

func TestRateLimiter_Allow_BurstLimit(t *testing.T) {
	rl := NewRateLimiter(10, 5, false, nil)
	ip := "192.168.1.2"

	// Should allow up to burst limit
	for i := 0; i < 5; i++ {
		if !rl.Allow(ip) {
			t.Errorf("Request %d should be allowed (within burst)", i+1)
		}
	}

	// 6th request should be denied (burst exhausted)
	if rl.Allow(ip) {
		t.Error("Request after burst should be denied")
	}
}

func TestRateLimiter_Allow_TokenRefill(t *testing.T) {
	rl := NewRateLimiter(100, 2, false, nil) // 100/sec = refill quickly
	ip := "192.168.1.3"

	// Exhaust burst
	rl.Allow(ip)
	rl.Allow(ip)

	// Should be denied
	if rl.Allow(ip) {
		t.Error("Should be denied after burst exhausted")
	}

	// Wait for token refill (100/sec = 1 token every 10ms)
	time.Sleep(15 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow(ip) {
		t.Error("Should be allowed after token refill")
	}
}

func TestRateLimiter_Allow_MultipleClients(t *testing.T) {
	rl := NewRateLimiter(10, 2, false, nil)

	// Each client has independent limits
	if !rl.Allow("client1") {
		t.Error("client1 should be allowed")
	}
	if !rl.Allow("client2") {
		t.Error("client2 should be allowed")
	}

	// Exhaust client1's burst
	rl.Allow("client1")
	if rl.Allow("client1") {
		t.Error("client1 should be denied (burst exhausted)")
	}

	// client2 should still have tokens
	if !rl.Allow("client2") {
		t.Error("client2 should still be allowed")
	}
}

func TestRateLimitMiddleware_Allowed(t *testing.T) {
	rl := NewRateLimiter(100, 10, false, nil)
	middleware := RateLimitMiddleware(rl)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.10:12345"
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRateLimitMiddleware_Denied(t *testing.T) {
	rl := NewRateLimiter(100, 2, false, nil)
	middleware := RateLimitMiddleware(rl)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust the limit
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.20:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Next request should be denied
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.20:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}

	// Check Retry-After header
	if rr.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header should be set")
	}
}

func TestRateLimitMiddleware_XForwardedFor(t *testing.T) {
	// trustProxy=true to test X-Forwarded-For handling
	rl := NewRateLimiter(100, 2, true, nil)
	middleware := RateLimitMiddleware(rl)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Use X-Forwarded-For to identify client
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.1:12345" // Proxy IP
		req.Header.Set("X-Forwarded-For", "192.168.1.30")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Next request from same forwarded IP should be denied
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.168.1.30")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}
}

func TestDefaultRateLimiter(t *testing.T) {
	rl := DefaultRateLimiter(nil)
	if rl == nil {
		t.Fatal("DefaultRateLimiter returned nil")
	}
	if rl.rate != 50 {
		t.Errorf("default rate = %d, want 50", rl.rate)
	}
	if rl.burst != 100 {
		t.Errorf("default burst = %d, want 100", rl.burst)
	}
}

// 2026-05 audit P1-6: the per-IP map must not grow unbounded.
func TestRateLimiter_BoundedMap(t *testing.T) {
	rl := NewRateLimiter(50, 100, false, nil)
	defer rl.Close()
	// Shrink the cap to keep the test fast.
	rl.maxClients = 100

	// Fill the map with 100 distinct IPs. All should be allowed (each gets
	// its own bucket with full burst).
	for i := 0; i < 100; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		if !rl.Allow(ip) {
			t.Fatalf("Allow(%q) = false at i=%d; want true (map not yet at cap)", ip, i)
		}
	}
	if len(rl.clients) != 100 {
		t.Fatalf("map size = %d after filling; want 100", len(rl.clients))
	}

	// The 101st distinct IP must be denied because the map is at cap and
	// no entries are stale (all just created).
	overflow := "10.1.0.1"
	if rl.Allow(overflow) {
		t.Errorf("Allow(%q) = true past cap; want false (map full, no stale entries)", overflow)
	}
	if len(rl.clients) != 100 {
		t.Errorf("map size = %d after overflow attempt; want 100 (no new entry)", len(rl.clients))
	}
}

func TestRateLimiter_EvictsStaleWhenAtCap(t *testing.T) {
	rl := NewRateLimiter(50, 100, false, nil)
	defer rl.Close()
	rl.maxClients = 10

	// Fill with 10 IPs, but rewind their lastUpdate so they look stale.
	for i := 0; i < 10; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		rl.Allow(ip)
		rl.clients[ip].lastUpdate = time.Now().Add(-5 * time.Minute)
	}
	if len(rl.clients) != 10 {
		t.Fatalf("map size = %d; want 10", len(rl.clients))
	}

	// A new IP should be admitted: the premature-cleanup path will evict
	// the 10 stale entries before allocating.
	if !rl.Allow("10.1.0.1") {
		t.Errorf("Allow(10.1.0.1) = false; want true (cap reached but stale entries evictable)")
	}
}
