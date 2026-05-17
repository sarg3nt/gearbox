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

// Resource returns the named structured resource published by the gear,
// if both the gear and the resource key are present. Issue #112 Phase 2.
//
// The agent publishes resources as `map[string]any` so each gear can
// pick its own shape; the dashboard reads each gear's known keys
// explicitly. Typical use is the Logs page reading
// caps.Resource("access-log", "log_sources") to populate its source
// dropdown without inferring sources from gear-availability flags.
//
// The third return distinguishes "the gear isn't surfaced" (false)
// from "the gear is surfaced but didn't publish this resource" (also
// false). Callers that need to fail-open on the older-agent case
// should pair this with caps.Has(gearName).
func (b *BoxCapabilities) Resource(gearName, resourceKey string) (any, bool) {
	entry, ok := b.Entry(gearName)
	if !ok {
		return nil, false
	}
	v, ok := entry.Resources[resourceKey]
	return v, ok
}

// CapabilitiesCache memoises agent capability fetches per (box, agent
// URL) pair. Dashboard pages call into this on every render that needs
// to decide what to surface; without the cache, that's one synchronous
// round-trip per render. TTL is short so a recently-restarted agent's
// new probe table shows up without operator intervention; reconnect
// events should invalidate explicitly via Invalidate() for an immediate
// refresh.
//
// The cache key includes the agent URL (not just the box ID) so that an
// operator editing a box's Agent URL — or re-pointing the same box ID
// at a different host — gets fresh capabilities on the next render
// rather than stale data from the prior agent until the 5-minute TTL
// expires. The API key is intentionally not part of the key (rotations
// don't change which gears the host runs; if the new key is wrong the
// fetch errors and the negative-cache entry expires normally).
//
// Negative results (fetch errors, agent dead) are also cached for the
// TTL window so a flaky or unreachable agent doesn't drag down every
// page render with a fresh failed call.
type CapabilitiesCache struct {
	mu           sync.RWMutex
	entries      map[cacheKey]*cachedCapabilities
	ttl          time.Duration
	fetchTimeout time.Duration
}

// cacheKey identifies one cached entry by (boxID, agentURL). Splitting
// into a struct rather than concatenating strings sidesteps any chance
// of separator collision in operator-supplied URLs.
type cacheKey struct {
	boxID    string
	agentURL string
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
		entries:      make(map[cacheKey]*cachedCapabilities),
		ttl:          ttl,
		fetchTimeout: fetchTimeout,
	}
}

// Get returns the cached capabilities for (boxID, agentURL). If the
// entry is missing or older than the TTL, the cache fetches fresh
// capabilities from agentURL using the supplied API key. On fetch error,
// returns (nil, err); the error is also cached for the TTL window.
// Callers should treat (nil, err) as "unknown" and fail open — locking
// users out of pages because the agent is briefly unreachable is worse
// than briefly showing gears that may not be available.
//
// Changing agentURL for the same boxID produces a different cache entry,
// so config edits take effect on the next call rather than waiting out
// the TTL. The previous entry expires naturally; no explicit cleanup
// needed for the rare case of an operator-driven URL change.
func (c *CapabilitiesCache) Get(boxID, agentURL, apiKey string) (*BoxCapabilities, error) {
	k := cacheKey{boxID: boxID, agentURL: agentURL}
	if entry, ok := c.lookup(k); ok {
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
	c.entries[k] = &cachedCapabilities{caps: caps, err: err, fetchedAt: now}
	c.mu.Unlock()

	return caps, err
}

// lookup returns a non-stale entry under the read lock, or (nil, false)
// if missing/expired. Split out so Get's refresh path can drop the read
// lock before doing network I/O.
func (c *CapabilitiesCache) lookup(k cacheKey) (*cachedCapabilities, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[k]
	if !ok {
		return nil, false
	}
	if time.Since(entry.fetchedAt) >= c.ttl {
		return nil, false
	}
	return entry, true
}

// Invalidate drops every cached entry for boxID, regardless of the agent
// URL it was fetched against. Call this when the box reconnects or when
// the box's config changes (URL or API key edited), so the next Get
// refetches against the current settings rather than at the next TTL
// boundary.
func (c *CapabilitiesCache) Invalidate(boxID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if k.boxID == boxID {
			delete(c.entries, k)
		}
	}
}

// InvalidateAll drops every cached entry. Useful when global config that
// affects probing changes (rare) and in tests.
func (c *CapabilitiesCache) InvalidateAll() {
	c.mu.Lock()
	c.entries = make(map[cacheKey]*cachedCapabilities)
	c.mu.Unlock()
}
