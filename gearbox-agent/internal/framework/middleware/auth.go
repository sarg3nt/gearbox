// Package middleware provides HTTP middleware for the gearbox-agent framework.
package middleware

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
)

// APIKeyAuth creates middleware that validates API key authentication.
func APIKeyAuth(validKey string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract API key from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				logger.Warn("AUTH DENIED: Missing Authorization header", "remote_addr", r.RemoteAddr)
				http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
				return
			}

			// Expect "Bearer <token>" format
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				logger.Warn("AUTH DENIED: Invalid Authorization format", "remote_addr", r.RemoteAddr)
				http.Error(w, "Unauthorized: invalid Authorization format (expected 'Bearer <token>')", http.StatusUnauthorized)
				return
			}

			token := parts[1]
			// Use constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(token), []byte(validKey)) != 1 {
				logger.Warn("AUTH DENIED: Invalid API key", "remote_addr", r.RemoteAddr)
				http.Error(w, "Unauthorized: invalid API key", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
