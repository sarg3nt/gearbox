package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// PanicRecovery creates middleware that recovers from panics and returns 500.
func PanicRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic with stack trace
					logger.Error("PANIC recovered",
						"error", err,
						"stack", string(debug.Stack()),
					)

					// Return 500 to client
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
