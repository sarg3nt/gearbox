package middleware

import (
	"context"
	"log"
	"net/http"
)

type contextKey string

const (
	// UseLocalAssetsKey is the context key for the UseLocalAssets config value
	UseLocalAssetsKey contextKey = "useLocalAssets"
)

// InjectAssetConfig injects the asset loading configuration into the request context.
// This allows templates to conditionally load CDN vs local assets.
func InjectAssetConfig(useLocalAssets bool) func(http.Handler) http.Handler {
	log.Printf("🔧 InjectAssetConfig middleware initialized with useLocalAssets=%v", useLocalAssets)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), UseLocalAssetsKey, useLocalAssets)
			log.Printf("📝 Request to %s - injecting useLocalAssets=%v into context", r.URL.Path, useLocalAssets)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UseLocalAssets retrieves the UseLocalAssets value from the request context.
func UseLocalAssets(ctx context.Context) bool {
	val, ok := ctx.Value(UseLocalAssetsKey).(bool)
	result := ok && val
	log.Printf("🔍 UseLocalAssets called - found in context: %v, value: %v, returning: %v", ok, val, result)
	return result
}
