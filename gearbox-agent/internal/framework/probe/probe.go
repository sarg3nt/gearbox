// Package probe provides cheap, side-effect-light primitives used by
// gear Probe() implementations to test for the presence and
// reachability of locally-running services.
//
// The contract for everything in this package is identical to the
// contract on gear.ProbeableGear.Probe itself — bounded latency, no
// retries, no log noise. Each detector gets its own indirection-
// friendly entry point (e.g. nginx swaps `HTTPGet` for a stub in
// tests) so unit tests don't need the real service installed on the
// runner.
package probe

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout caps how long a probe-time HTTP request may take.
// Probe runs on the startup path; a misbehaving local server must not
// stall the agent's boot. One second is generous for a loopback call.
const DefaultTimeout = 1 * time.Second

// HTTPResult captures the result of one probe-time HTTP call. Body is
// already a string (not a Reader) because callers want to substring-
// match on a status-endpoint sentinel like "Active connections:" or
// "Total Accesses:" — both small payloads where eager Read is fine.
type HTTPResult struct {
	StatusCode int
	Body       string
}

// HTTPGet performs a bounded GET against url. Returns the status code
// and body (decoded as UTF-8 string, capped at maxBody bytes) on any
// HTTP response — including 4xx/5xx, since detectors care about the
// distinction between 403/404 (server present, surface missing or
// permissioned) and a connection error (server absent).
//
// Loopback TLS verification is intentionally disabled — a probe of
// `https://127.0.0.1/...` that fronts a self-signed cert is a normal
// shape (e.g. nginx with `ssl_certificate snakeoil.pem`). For non-
// loopback hosts, the caller should construct their own client with
// verification on; this helper is loopback-oriented by design.
//
// maxBody must be > 0; we cap at that many bytes to avoid pulling an
// entire log file or large debug page into memory.
func HTTPGet(ctx context.Context, url string, maxBody int64) (HTTPResult, error) {
	client := &http.Client{
		Timeout: DefaultTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: isLoopback(url)}, // #nosec G402
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return HTTPResult{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return HTTPResult{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return HTTPResult{
		StatusCode: resp.StatusCode,
		Body:       string(body),
	}, nil
}

// isLoopback is true when the URL points at 127.0.0.0/8 or [::1] —
// matches the cases where TLS verification is meaningless and
// commonly fails on self-signed local certs. Anything else returns
// false so the helper doesn't silently disable verification on
// public endpoints.
func isLoopback(url string) bool {
	url = strings.ToLower(url)
	return strings.Contains(url, "://127.") ||
		strings.Contains(url, "://[::1]") ||
		strings.Contains(url, "://localhost")
}
