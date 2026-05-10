// Package widget provides per-app data fetchers for Tier-1 service tiles.
//
// Each Provider implements one self-hosted app's API: given a base URL,
// a decrypted secret, and the user's chosen field set, it returns the
// values to render. Providers run server-side only — secrets never leave
// the Gearbox process.
package widget

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"sync"
	"time"
)

// Result is the typed payload a Provider returns. Values are always
// stringified for direct rendering on the dashboard.
type Result struct {
	// Fields maps the catalog field key (e.g. "wanted") to its rendered value.
	Fields map[string]string
	// FetchedAt is when the data was collected.
	FetchedAt time.Time
	// Err is any non-fatal collection error (the provider may still have partial fields).
	Err error
}

// Request is what's handed to a Provider.
type Request struct {
	BaseURL        string            // tile URL
	Secret         string            // decrypted API key, basic-auth password, or bearer token
	BasicUsername  string            // for providers that need basic auth (qBittorrent)
	SelectedFields []string          // optional — empty means "default fields"
	Timeout        time.Duration     // probe timeout
	Logger         logger            // optional structured logger; pass NoopLogger when not needed
	HTTPClient     *http.Client      // optional override; default has 5s timeout + lax TLS
	Extra          map[string]string // app-specific knobs
}

// logger is a tiny interface so we don't pull slog into the widget package.
type logger interface {
	Warn(msg string, args ...any)
	Debug(msg string, args ...any)
}

// noopLogger is the zero-value default.
type noopLogger struct{}

func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Debug(string, ...any) {}

// NoopLogger returns a logger that drops every message.
func NoopLogger() logger { return noopLogger{} }

// Provider is the interface every Tier-1 app implements.
type Provider interface {
	// Slug matches the catalog entry (e.g. "sonarr").
	Slug() string
	// Fetch hits the upstream and returns the rendered field map.
	Fetch(ctx context.Context, req Request) (Result, error)
}

// registry holds all registered providers, indexed by catalog slug.
var (
	registryMu sync.RWMutex
	registry   = map[string]Provider{}
)

// Register adds a Provider to the registry. Called from each provider's init().
func Register(p Provider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p.Slug()] = p
}

// Get returns the Provider for an app slug, or nil if no provider has been registered.
func Get(slug string) Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[slug]
}

// HasProvider reports whether a Tier-1 widget exists for an app slug.
func HasProvider(slug string) bool {
	return Get(slug) != nil
}

// DefaultClient returns an HTTP client suitable for self-hosted endpoints
// — short timeout, TLS verification skipped to tolerate self-signed certs.
func DefaultClient(timeout time.Duration) *http.Client {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
}

// ErrNotConfigured is returned when a tile lacks the secret a provider needs.
var ErrNotConfigured = errors.New("widget: missing secret")
