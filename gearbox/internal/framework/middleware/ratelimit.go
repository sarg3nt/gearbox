package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter per IP.
type RateLimiter struct {
	mu          sync.Mutex
	clients     map[string]*clientBucket
	rate        int           // Requests per second
	burst       int           // Maximum burst size
	cleanup     time.Duration // How often to clean up old entries
	logger      *slog.Logger
	stopCleanup chan struct{}
}

type clientBucket struct {
	tokens     float64
	lastUpdate time.Time
}

// NewRateLimiter creates a new rate limiter.
// rate: requests per second allowed
// burst: maximum burst size (allows short spikes)
func NewRateLimiter(rate, burst int, logger *slog.Logger) *RateLimiter {
	rl := &RateLimiter{
		clients:     make(map[string]*clientBucket),
		rate:        rate,
		burst:       burst,
		cleanup:     5 * time.Minute,
		logger:      logger,
		stopCleanup: make(chan struct{}),
	}

	go rl.cleanupLoop()

	return rl
}

// Close stops the rate limiter's cleanup goroutine.
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
		rl.clients[ip] = &clientBucket{
			tokens:     float64(rl.burst - 1),
			lastUpdate: now,
		}
		return true
	}

	elapsed := now.Sub(bucket.lastUpdate).Seconds()
	bucket.tokens += elapsed * float64(rl.rate)
	if bucket.tokens > float64(rl.burst) {
		bucket.tokens = float64(rl.burst)
	}
	bucket.lastUpdate = now

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

// RateLimitMiddleware creates HTTP middleware that rate limits requests.
// Returns 429 Too Many Requests when limit is exceeded.
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)

			if !limiter.Allow(ip) {
				if limiter.logger != nil {
					limiter.logger.Warn("rate limit exceeded",
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
// Uses X-Forwarded-For if present (chi's RealIP middleware should have already
// set RemoteAddr, but we also check the header for proxied setups).
func getClientIP(r *http.Request) string {
	// chi's RealIP middleware sets RemoteAddr from X-Forwarded-For/X-Real-IP,
	// so RemoteAddr is typically the correct client IP already.
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		if !strings.Contains(ip[idx:], "]") {
			ip = ip[:idx]
		}
	}
	return ip
}
