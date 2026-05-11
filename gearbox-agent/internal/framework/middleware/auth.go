// Package middleware provides HTTP middleware for the gearbox-agent framework.
package middleware

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
)

// APIKeyAuth creates middleware that validates API key authentication.
//
// When backoff is non-nil, per-IP auth failures trigger exponential
// backoff (2026-05 audit P1-7) — a defense layered on top of the
// global rate limiter for distributed brute-force attempts. Pass nil
// to disable; callers concerned about brute-force should pass a real
// tracker constructed via DefaultBackoffTracker.
func APIKeyAuth(validKey string, logger *slog.Logger, backoff *BackoffTracker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIPFromRemoteAddr(r.RemoteAddr)

			if backoff != nil && backoff.IsBlocked(ip) {
				logger.Warn("AUTH DENIED: IP in backoff window", "remote_addr", r.RemoteAddr)
				w.Header().Set("Retry-After", "60")
				http.Error(w, "Too Many Authentication Failures", http.StatusTooManyRequests)
				return
			}

			// Extract API key from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				logger.Warn("AUTH DENIED: Missing Authorization header", "remote_addr", r.RemoteAddr)
				if backoff != nil {
					backoff.RecordFailure(ip)
				}
				http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
				return
			}

			// Expect "Bearer <token>" format
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				logger.Warn("AUTH DENIED: Invalid Authorization format", "remote_addr", r.RemoteAddr)
				if backoff != nil {
					backoff.RecordFailure(ip)
				}
				http.Error(w, "Unauthorized: invalid Authorization format (expected 'Bearer <token>')", http.StatusUnauthorized)
				return
			}

			token := parts[1]
			// Use constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(token), []byte(validKey)) != 1 {
				logger.Warn("AUTH DENIED: Invalid API key", "remote_addr", r.RemoteAddr)
				if backoff != nil {
					backoff.RecordFailure(ip)
				}
				http.Error(w, "Unauthorized: invalid API key", http.StatusUnauthorized)
				return
			}

			if backoff != nil {
				backoff.Reset(ip)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIPFromRemoteAddr strips the :port from a net/http RemoteAddr value.
// Handles IPv6 bracketed form ([::1]:1234) and the common IPv4 form.
func clientIPFromRemoteAddr(addr string) string {
	if addr == "" {
		return ""
	}
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		// IPv6 in brackets: keep the host portion as-is.
		if !strings.Contains(addr[idx:], "]") {
			return addr[:idx]
		}
	}
	return addr
}
