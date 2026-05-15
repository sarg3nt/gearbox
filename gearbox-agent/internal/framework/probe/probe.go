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
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

// HTTPGet performs a bounded GET against rawURL. Returns the status
// code and body (decoded as UTF-8 string, capped at maxBody bytes) on
// any HTTP response — including 4xx/5xx, since detectors care about
// the distinction between 403/404 (server present, surface missing or
// permissioned) and a connection error (server absent).
//
// Loopback TLS verification is intentionally disabled — a probe of
// `https://127.0.0.1/...` that fronts a self-signed cert is a normal
// shape (e.g. nginx with `ssl_certificate snakeoil.pem`). For non-
// loopback hosts the helper leaves verification on; the loopback
// check parses the URL with net/url and inspects the host with
// net.IP.IsLoopback so a userinfo-spoofed URL like
// `https://localhost@evil.com/...` can't trick us into skipping verify
// against `evil.com`.
//
// maxBody must be > 0; passing a non-positive value returns an error
// instead of silently truncating to nothing (which would hide
// sentinel mismatches and look like a 200 with empty body).
func HTTPGet(ctx context.Context, rawURL string, maxBody int64) (HTTPResult, error) {
	if maxBody <= 0 {
		return HTTPResult{}, fmt.Errorf("probe.HTTPGet: maxBody must be positive, got %d", maxBody)
	}
	client := &http.Client{
		Timeout: DefaultTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: isLoopback(rawURL)}, // #nosec G402
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
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

// isLoopback reports whether rawURL points at a loopback host —
// `localhost`, anything in 127.0.0.0/8, or `[::1]`. Parses the URL
// with net/url so userinfo (`https://localhost@evil.com`) can't fool
// the check: url.Hostname() strips userinfo, port, and IPv6 brackets,
// leaving just the actual target host. Returns false on parse error
// so the safe default is "TLS verification stays on" for anything we
// don't recognise as loopback.
func isLoopback(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
