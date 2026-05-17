// Package middleware provides HTTP middleware for the gearbox-agent framework.
package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/sarg3nt/gearbox-agent/internal/framework/crypto"
)

// ResponseHeaderKID is the header the agent echoes back on every
// successful authenticated response — the kid that actually
// authenticated, not necessarily the one the caller thinks is primary.
// The dashboard compares this with its own expectation and surfaces
// "rotation drift" alerts when they diverge.
const ResponseHeaderKID = "X-Gearbox-Kid"

// APIKeyAuth creates middleware that validates API key authentication
// against a keyring containing one or more accepted keys.
//
// Token formats accepted on the wire:
//   - "Bearer gbx_<kid>_<base64url(secret)>"  — preferred (allows
//     versioned rotation; the agent matches the named kid only).
//   - "Bearer <64-hex-chars>"                 — legacy single-key form,
//     matched against any keyring entry's secret bytes via constant-
//     time compare.
//
// On success the matched entry's kid is echoed back in the
// X-Gearbox-Kid response header so the caller can detect drift.
//
// When backoff is non-nil, per-IP auth failures trigger exponential
// backoff (2026-05 audit P1-7).
func APIKeyAuth(keyring *crypto.KeyRingPointer, logger *slog.Logger, backoff *BackoffTracker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Fail-closed nil guard. Should be unreachable — the
			// agent's main.go always passes a non-nil keyring pointer
			// before this middleware is mounted — but a future wiring
			// bug should 401 every request instead of panicking the
			// agent on the auth path.
			if keyring == nil {
				logger.Error("AUTH DENIED: middleware constructed with nil keyring pointer", "remote_addr", r.RemoteAddr)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			ip := clientIPFromRemoteAddr(r.RemoteAddr)

			if backoff != nil && backoff.IsBlocked(ip) {
				logger.Warn("AUTH DENIED: IP in backoff window", "remote_addr", r.RemoteAddr)
				w.Header().Set("Retry-After", "60")
				http.Error(w, "Too Many Authentication Failures", http.StatusTooManyRequests)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				logger.Warn("AUTH DENIED: Missing Authorization header", "remote_addr", r.RemoteAddr)
				if backoff != nil {
					backoff.RecordFailure(ip)
				}
				http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
				return
			}

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
			kr := keyring.Load()
			if kr == nil {
				logger.Error("AUTH DENIED: keyring pointer empty (bug?)", "remote_addr", r.RemoteAddr)
				if backoff != nil {
					backoff.RecordFailure(ip)
				}
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			entry, err := kr.MatchToken(token)
			if err != nil || entry == nil {
				logger.Warn("AUTH DENIED: token did not match any keyring entry",
					"remote_addr", r.RemoteAddr, "reason", errString(err))
				if backoff != nil {
					backoff.RecordFailure(ip)
				}
				http.Error(w, "Unauthorized: invalid API key", http.StatusUnauthorized)
				return
			}

			w.Header().Set(ResponseHeaderKID, entry.KID)

			if backoff != nil {
				backoff.Reset(ip)
			}

			if entry.Role != "primary" {
				// Surfaced through the audit log so the dashboard can warn
				// when a retired-but-not-yet-removed key is still in use.
				logger.Info("AUTH OK: authenticated with secondary key",
					"kid", entry.KID, "remote_addr", r.RemoteAddr)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// clientIPFromRemoteAddr strips the :port from a net/http RemoteAddr value.
// Handles IPv6 bracketed form ([::1]:1234) and the common IPv4 form.
func clientIPFromRemoteAddr(addr string) string {
	if addr == "" {
		return ""
	}
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		if !strings.Contains(addr[idx:], "]") {
			return addr[:idx]
		}
	}
	return addr
}
