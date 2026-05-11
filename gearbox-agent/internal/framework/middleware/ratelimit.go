package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxRateLimitClients caps the number of distinct client IPs the rate
// limiter will track in memory. A distributed attacker hitting the agent
// from a botnet (or any large pool of unique source IPs) would otherwise
// be able to grow the map unbounded, since the cleanup goroutine only
// evicts entries idle for 10 minutes. With the cap in place, attempts to
// add a new bucket past the cap fall through to a synchronous "premature
// cleanup" of stale entries; if that doesn't free space, the request is
// denied as if rate-limited. See 2026-05 security audit, P1-6.
const maxRateLimitClients = 10000

// staleClientThreshold is how old a bucket's lastUpdate must be before the
// premature-cleanup path is willing to evict it. Lower than the 10-minute
// cleanupLoop threshold to give the at-capacity path more room to work,
// but high enough that a brief burst from many clients doesn't cause real
// requests to be denied.
const staleClientThreshold = 2 * time.Minute

// RateLimiter implements a simple token bucket rate limiter per IP.
type RateLimiter struct {
	mu          sync.Mutex
	clients     map[string]*clientBucket
	rate        int           // Requests per second
	burst       int           // Maximum burst size
	cleanup     time.Duration // How often to clean up old entries
	maxClients  int           // Bounded map size; 0 = unbounded (tests only)
	logger      *slog.Logger
	trustProxy  bool          // Whether to trust X-Forwarded-For headers
	stopCleanup chan struct{} // Signal to stop cleanup goroutine
}

type clientBucket struct {
	tokens     float64
	lastUpdate time.Time
}

// NewRateLimiter creates a new rate limiter.
// rate: requests per second allowed
// burst: maximum burst size (allows short spikes)
// trustProxy: if true, uses X-Forwarded-For header for client IP
func NewRateLimiter(rate, burst int, trustProxy bool, logger *slog.Logger) *RateLimiter {
	rl := &RateLimiter{
		clients:     make(map[string]*clientBucket),
		rate:        rate,
		burst:       burst,
		cleanup:     5 * time.Minute,
		maxClients:  maxRateLimitClients,
		logger:      logger,
		trustProxy:  trustProxy,
		stopCleanup: make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Close stops the rate limiter's cleanup goroutine.
// Should be called when the rate limiter is no longer needed.
func (rl *RateLimiter) Close() {
	close(rl.stopCleanup)
}

// Allow checks if a request from the given IP should be allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	bucket, exists := rl.clients[ip]
	if !exists {
		// New client. Check the bounded-map cap before allocating a bucket
		// to defend against a distributed attacker pumping unique IPs into
		// the map faster than the 5-minute cleanup loop evicts them
		// (2026-05 audit, P1-6).
		if rl.maxClients > 0 && len(rl.clients) >= rl.maxClients {
			rl.evictStaleLocked(now)
			if len(rl.clients) >= rl.maxClients {
				// Still full after premature cleanup. Deny rather than
				// allocate. The denied IP gets back in next time someone
				// else's bucket goes stale.
				if rl.logger != nil {
					rl.logger.Warn("Rate limiter map at capacity; denying new client",
						"ip", ip,
						"map_size", len(rl.clients),
					)
				}
				return false
			}
		}
		// Start with full burst capacity (-1 for this request).
		rl.clients[ip] = &clientBucket{
			tokens:     float64(rl.burst - 1),
			lastUpdate: now,
		}
		return true
	}

	// Calculate tokens to add based on time elapsed
	elapsed := now.Sub(bucket.lastUpdate).Seconds()
	bucket.tokens += elapsed * float64(rl.rate)
	if bucket.tokens > float64(rl.burst) {
		bucket.tokens = float64(rl.burst)
	}
	bucket.lastUpdate = now

	// Check if we have tokens available
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}

	return false
}

// cleanupLoop periodically removes old client entries.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, bucket := range rl.clients {
				// Remove clients that haven't been seen in 10 minutes
				if now.Sub(bucket.lastUpdate) > 10*time.Minute {
					delete(rl.clients, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCleanup:
			return
		}
	}
}

// evictStaleLocked drops bucket entries older than staleClientThreshold.
// Caller MUST hold rl.mu. Used by Allow() when the map hits capacity, to
// make room before allocating a new bucket. The 2-minute threshold is
// shorter than the cleanupLoop's 10-minute threshold so the at-capacity
// path can free space without waiting for the periodic sweep.
func (rl *RateLimiter) evictStaleLocked(now time.Time) {
	for ip, bucket := range rl.clients {
		if now.Sub(bucket.lastUpdate) > staleClientThreshold {
			delete(rl.clients, ip)
		}
	}
}

// RateLimitMiddleware creates HTTP middleware that rate limits requests.
// Returns 429 Too Many Requests when limit is exceeded.
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := limiter.getClientIP(r)

			if !limiter.Allow(ip) {
				if limiter.logger != nil {
					limiter.logger.Warn("Rate limit exceeded",
						"ip", ip,
						"method", r.Method,
						"path", r.URL.Path,
					)
				}
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP from the request.
// Only trusts X-Forwarded-For if trustProxy is enabled.
func (rl *RateLimiter) getClientIP(r *http.Request) string {
	// Only trust proxy headers if explicitly configured
	if rl.trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			// Take only the first IP in the chain (original client)
			if idx := strings.Index(forwarded, ","); idx > 0 {
				return strings.TrimSpace(forwarded[:idx])
			}
			return strings.TrimSpace(forwarded)
		}
	}

	// Use RemoteAddr directly (strip port if present)
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		// Check if this is an IPv6 address in brackets
		if !strings.Contains(ip[idx:], "]") {
			ip = ip[:idx]
		}
	}
	return ip
}

// DefaultRateLimiter returns a rate limiter with sensible defaults.
// 50 requests/second with burst of 100 should handle normal monitoring
// while preventing abuse.
// trustProxy defaults to false for security - set to true only if behind a trusted proxy.
func DefaultRateLimiter(logger *slog.Logger) *RateLimiter {
	return NewRateLimiter(50, 100, false, logger)
}

// DefaultRateLimiterWithProxy returns a rate limiter that trusts proxy headers.
// Use this when the agent is behind a trusted reverse proxy like HAProxy.
func DefaultRateLimiterWithProxy(logger *slog.Logger) *RateLimiter {
	return NewRateLimiter(50, 100, true, logger)
}
