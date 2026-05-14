package agent

import (
	"sync"
	"time"
)

// BoxCapabilities is a dashboard-side view of one box's probe table.
// Carries the CapabilitiesResponse from the agent plus when it was last
// observed. Handlers consume this to gate UI on what the agent reports
// the box can do — e.g. hide HAProxy charts when the haproxy gear isn't
// available on this host.
type BoxCapabilities struct {
	Response  *CapabilitiesResponse
	FetchedAt time.Time
}

// Has reports whether the agent surfaced the given gear at all, regardless
// of probe verdict. Use when "the agent is recent enough to know about
// this gear" is the question — older agents that pre-date a gear simply
// won't have it in their probe table.
func (b *BoxCapabilities) Has(gearName string) bool {
	if b == nil || b.Response == nil {
		return false
	}
	_, ok := b.Response.Gears[gearName]
	return ok
}

// IsAvailable reports whether the gear probed Available on the agent.
// Returns false when capabilities are nil/unknown; callers wanting
// fail-open behaviour should check Has() separately.
func (b *BoxCapabilities) IsAvailable(gearName string) bool {
	if b == nil || b.Response == nil {
		return false
	}
	e, ok := b.Response.Gears[gearName]
	return ok && e.IsAvailable()
}

// Entry returns the agent's probe entry for the gear if it's present in
// the response. The second return is false when the gear isn't surfaced
// or capabilities are nil.
func (b *BoxCapabilities) Entry(gearName string) (CapabilityEntry, bool) {
	if b == nil || b.Response == nil {
		return CapabilityEntry{}, false
	}
	e, ok := b.Response.Gears[gearName]
	return e, ok
}

// CapabilitiesCache memoises agent capability fetches per box. Dashboard
// pages call into this on every render that needs to decide what to
// surface; without the cache, that's one synchronous round-trip per
// render. TTL is short so a recently-restarted agent's new probe table
// shows up without operator intervention; reconnect events should
// invalidate explicitly via Invalidate() for an immediate refresh.
//
// Negative results (fetch errors, agent dead) are also cached for the
// TTL window so a flaky or unreachable agent doesn't drag down every
// page render with a fresh failed call.
type CapabilitiesCache struct {
	mu           sync.RWMutex
	entries      map[string]*cachedCapabilities
	ttl          time.Duration
	fetchTimeout time.Duration
}

type cachedCapabilities struct {
	caps      *BoxCapabilities // nil when the last fetch errored
	err       error            // captured on failure for callers that want it
	fetchedAt time.Time
}

// NewCapabilitiesCache creates a cache with the given TTL and per-fetch
// timeout. The timeout must be short — capability fetches sit on the
// critical path of page renders, so a dead agent must not stall the UI.
// 3s matches the value the Gears settings page used pre-cache.
func NewCapabilitiesCache(ttl, fetchTimeout time.Duration) *CapabilitiesCache {
	return &CapabilitiesCache{
		entries:      make(map[string]*cachedCapabilities),
		ttl:          ttl,
		fetchTimeout: fetchTimeout,
	}
}

// Get returns the cached capabilities for boxID. If the entry is missing
// or older than the TTL, the cache fetches fresh capabilities from
// agentURL using the supplied API key. On fetch error, returns (nil, err);
// the error is also cached for the TTL window. Callers should treat
// (nil, err) as "unknown" and fail open — locking users out of pages
// because the agent is briefly unreachable is worse than briefly showing
// gears that may not be available.
func (c *CapabilitiesCache) Get(boxID, agentURL, apiKey string) (*BoxCapabilities, error) {
	if entry, ok := c.lookup(boxID); ok {
		return entry.caps, entry.err
	}

	client := NewClientWithTimeout(agentURL, apiKey, c.fetchTimeout)
	resp, err := client.GetCapabilities()

	now := time.Now()
	var caps *BoxCapabilities
	if err == nil {
		caps = &BoxCapabilities{Response: resp, FetchedAt: now}
	}

	c.mu.Lock()
	c.entries[boxID] = &cachedCapabilities{caps: caps, err: err, fetchedAt: now}
	c.mu.Unlock()

	return caps, err
}

// lookup returns a non-stale entry under the read lock, or (nil, false)
// if missing/expired. Split out so Get's refresh path can drop the read
// lock before doing network I/O.
func (c *CapabilitiesCache) lookup(boxID string) (*cachedCapabilities, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[boxID]
	if !ok {
		return nil, false
	}
	if time.Since(entry.fetchedAt) >= c.ttl {
		return nil, false
	}
	return entry, true
}

// Invalidate drops the cached entry for boxID, forcing a fresh fetch on
// the next Get. Call this when the box reconnects so a restarted agent's
// new probe table is picked up immediately rather than at the next TTL
// boundary.
func (c *CapabilitiesCache) Invalidate(boxID string) {
	c.mu.Lock()
	delete(c.entries, boxID)
	c.mu.Unlock()
}

// InvalidateAll drops every cached entry. Useful when global config that
// affects probing changes (rare) and in tests.
func (c *CapabilitiesCache) InvalidateAll() {
	c.mu.Lock()
	c.entries = make(map[string]*cachedCapabilities)
	c.mu.Unlock()
}
