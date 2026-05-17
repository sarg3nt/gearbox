package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newCapabilitiesServer is a small httptest server that returns a
// CapabilitiesResponse and tracks how many times it was called.
func newCapabilitiesServer(t *testing.T, response CapabilitiesResponse) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/api/v1/system/capabilities" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	return srv, &hits
}

func TestCapabilitiesCacheCachesWithinTTL(t *testing.T) {
	srv, hits := newCapabilitiesServer(t, CapabilitiesResponse{
		Gears: map[string]CapabilityEntry{
			"haproxy": {Status: "available"},
		},
	})
	defer srv.Close()

	cache := NewCapabilitiesCache(5*time.Minute, 2*time.Second)

	for i := 0; i < 3; i++ {
		caps, err := cache.Get("box-1", srv.URL, "test-key", false)
		if err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
		if !caps.IsAvailable("haproxy") {
			t.Fatalf("Get %d: haproxy should be available", i)
		}
	}

	if got := hits.Load(); got != 1 {
		t.Errorf("expected 1 upstream hit, got %d", got)
	}
}

func TestCapabilitiesCacheRefreshesAfterTTL(t *testing.T) {
	srv, hits := newCapabilitiesServer(t, CapabilitiesResponse{
		Gears: map[string]CapabilityEntry{"haproxy": {Status: "available"}},
	})
	defer srv.Close()

	// Sub-millisecond TTL: every Get triggers a refresh.
	cache := NewCapabilitiesCache(1*time.Nanosecond, 2*time.Second)

	for i := 0; i < 3; i++ {
		if _, err := cache.Get("box-1", srv.URL, "test-key", false); err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
	}

	if got := hits.Load(); got != 3 {
		t.Errorf("expected 3 upstream hits (each past TTL), got %d", got)
	}
}

func TestCapabilitiesCacheInvalidateForcesRefresh(t *testing.T) {
	srv, hits := newCapabilitiesServer(t, CapabilitiesResponse{
		Gears: map[string]CapabilityEntry{"haproxy": {Status: "available"}},
	})
	defer srv.Close()

	cache := NewCapabilitiesCache(5*time.Minute, 2*time.Second)

	if _, err := cache.Get("box-1", srv.URL, "test-key", false); err != nil {
		t.Fatalf("initial Get: %v", err)
	}
	cache.Invalidate("box-1")
	if _, err := cache.Get("box-1", srv.URL, "test-key", false); err != nil {
		t.Fatalf("post-invalidate Get: %v", err)
	}

	if got := hits.Load(); got != 2 {
		t.Errorf("expected 2 upstream hits (invalidate + refresh), got %d", got)
	}
}

func TestCapabilitiesCacheNegativeCaching(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "agent unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cache := NewCapabilitiesCache(5*time.Minute, 2*time.Second)

	for i := 0; i < 3; i++ {
		caps, err := cache.Get("box-1", srv.URL, "test-key", false)
		if err == nil {
			t.Fatalf("Get %d: expected error from failing agent", i)
		}
		if caps != nil {
			t.Fatalf("Get %d: expected nil caps on error, got %+v", i, caps)
		}
	}

	if got := hits.Load(); got != 1 {
		t.Errorf("expected 1 upstream hit (negative cache), got %d", got)
	}
}

func TestBoxCapabilitiesNilSafe(t *testing.T) {
	var b *BoxCapabilities
	if b.Has("haproxy") {
		t.Error("nil Has should return false")
	}
	if b.IsAvailable("haproxy") {
		t.Error("nil IsAvailable should return false")
	}
	if _, ok := b.Entry("haproxy"); ok {
		t.Error("nil Entry should return false")
	}
}

func TestBoxCapabilitiesAccessors(t *testing.T) {
	b := &BoxCapabilities{
		Response: &CapabilitiesResponse{
			Gears: map[string]CapabilityEntry{
				"haproxy": {Status: "available"},
				"metrics": {Status: "not_installed"},
			},
		},
		FetchedAt: time.Now(),
	}

	if !b.Has("haproxy") {
		t.Error("Has(haproxy) should be true")
	}
	if b.Has("nginx") {
		t.Error("Has(nginx) should be false (not in response)")
	}
	if !b.IsAvailable("haproxy") {
		t.Error("IsAvailable(haproxy) should be true")
	}
	if b.IsAvailable("metrics") {
		t.Error("IsAvailable(metrics) should be false (not_installed)")
	}
	if b.IsAvailable("nginx") {
		t.Error("IsAvailable(nginx) should be false (absent)")
	}

	entry, ok := b.Entry("metrics")
	if !ok || entry.Status != "not_installed" {
		t.Errorf("Entry(metrics) = %+v, ok=%v; want not_installed", entry, ok)
	}
}

func TestCapabilitiesCacheDifferentAgentURLBypassesCache(t *testing.T) {
	srv, hits := newCapabilitiesServer(t, CapabilitiesResponse{
		Gears: map[string]CapabilityEntry{"haproxy": {Status: "available"}},
	})
	defer srv.Close()
	srv2, hits2 := newCapabilitiesServer(t, CapabilitiesResponse{
		Gears: map[string]CapabilityEntry{"haproxy": {Status: "not_installed"}},
	})
	defer srv2.Close()

	cache := NewCapabilitiesCache(5*time.Minute, 2*time.Second)

	// Fetch against srv, then against srv2 with the same boxID. The
	// second call must NOT serve cached data from srv — operator edits
	// to the Agent URL should take effect on the next render.
	caps1, err := cache.Get("box-1", srv.URL, "test-key", false)
	if err != nil || !caps1.IsAvailable("haproxy") {
		t.Fatalf("first Get: want available, got caps=%+v err=%v", caps1, err)
	}
	caps2, err := cache.Get("box-1", srv2.URL, "test-key", false)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if caps2.IsAvailable("haproxy") {
		t.Error("expected second Get to reflect srv2's not_installed verdict, got available (stale cache)")
	}

	if hits.Load() != 1 {
		t.Errorf("srv: expected 1 hit, got %d", hits.Load())
	}
	if hits2.Load() != 1 {
		t.Errorf("srv2: expected 1 hit, got %d", hits2.Load())
	}
}

func TestCapabilitiesCacheInvalidateDropsAllAgentURLsForBox(t *testing.T) {
	srv, hits := newCapabilitiesServer(t, CapabilitiesResponse{
		Gears: map[string]CapabilityEntry{"haproxy": {Status: "available"}},
	})
	defer srv.Close()
	srv2, hits2 := newCapabilitiesServer(t, CapabilitiesResponse{
		Gears: map[string]CapabilityEntry{"haproxy": {Status: "available"}},
	})
	defer srv2.Close()

	cache := NewCapabilitiesCache(5*time.Minute, 2*time.Second)
	if _, err := cache.Get("box-1", srv.URL, "k", false); err != nil {
		t.Fatalf("seed srv: %v", err)
	}
	if _, err := cache.Get("box-1", srv2.URL, "k", false); err != nil {
		t.Fatalf("seed srv2: %v", err)
	}

	cache.Invalidate("box-1")

	if _, err := cache.Get("box-1", srv.URL, "k", false); err != nil {
		t.Fatalf("post-invalidate srv: %v", err)
	}
	if _, err := cache.Get("box-1", srv2.URL, "k", false); err != nil {
		t.Fatalf("post-invalidate srv2: %v", err)
	}

	// Each backend should have been hit twice: once for the initial
	// seed, once after invalidation dropped both entries for box-1.
	if got := hits.Load(); got != 2 {
		t.Errorf("srv hits = %d, want 2", got)
	}
	if got := hits2.Load(); got != 2 {
		t.Errorf("srv2 hits = %d, want 2", got)
	}
}

func TestCapabilitiesCacheInvalidateAll(t *testing.T) {
	srv, hits := newCapabilitiesServer(t, CapabilitiesResponse{
		Gears: map[string]CapabilityEntry{"haproxy": {Status: "available"}},
	})
	defer srv.Close()

	cache := NewCapabilitiesCache(5*time.Minute, 2*time.Second)

	if _, err := cache.Get("box-1", srv.URL, "test-key", false); err != nil {
		t.Fatalf("box-1 Get: %v", err)
	}
	if _, err := cache.Get("box-2", srv.URL, "test-key", false); err != nil {
		t.Fatalf("box-2 Get: %v", err)
	}

	cache.InvalidateAll()

	if _, err := cache.Get("box-1", srv.URL, "test-key", false); err != nil {
		t.Fatalf("post-invalidate box-1: %v", err)
	}
	if _, err := cache.Get("box-2", srv.URL, "test-key", false); err != nil {
		t.Fatalf("post-invalidate box-2: %v", err)
	}

	if got := hits.Load(); got != 4 {
		t.Errorf("expected 4 hits (2 initial + 2 post-invalidate), got %d", got)
	}
}
