// Package accesslog provides format-aware access-log parsers used by the
// per-source metrics gears (nginx, Apache, Caddy, HAProxy) and by the
// agent's /api/v1/access-log/{source}/recent endpoint. Each Profile is
// a Parser that translates one log-line shape into the common Record
// struct so downstream consumers (dashboard Error Insights, metrics
// rollups) don't need to know which proxy / web server produced the
// line.
//
// Profiles are intentionally tolerant — fields that don't appear in a
// given format stay zero-valued on the Record rather than producing a
// parse error. The decision rule for "did this line parse?" is a valid
// HTTP status code (100–599); anything missing that is treated as
// noise (e.g. SSL handshake errors, connection-level diagnostics).
// That mirrors the original dashboard-side `parseHAProxyLogLine`
// behaviour the HAProxy profile is ported from.
package accesslog

import (
	"strings"
	"time"
)

// Record is the common shape every Profile produces. Fields that
// the source format doesn't carry are left zero-valued; consumers
// distinguish "missing" via the zero value (empty string, 0 status,
// zero time.Time).
type Record struct {
	// Profile names the parser that produced this record. Stable
	// identifier ("haproxy", "nginx", "apache", "caddy") matching the
	// metric-source gear name where applicable.
	Profile string `json:"profile"`

	// Timestamp is the access time as the source format reported it.
	// Parsed into Go's time.Time when the format gives us enough to
	// disambiguate; otherwise the Raw timestamp string is preserved
	// in TimestampRaw so callers can render or re-parse as needed.
	Timestamp    time.Time `json:"timestamp,omitempty"`
	TimestampRaw string    `json:"timestamp_raw,omitempty"`

	// Network details. SourceIP is the remote client; for HAProxy
	// this is the connecting IP (port stripped). IPv6 hosts come
	// through unbracketed.
	SourceIP string `json:"source_ip,omitempty"`

	// Request details.
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	Host   string `json:"host,omitempty"`

	// Response details. StatusCode is always populated when parse
	// succeeded — it's the gating field.
	StatusCode int   `json:"status_code"`
	BytesSent  int64 `json:"bytes_sent,omitempty"`

	// Latency in milliseconds when the format includes it (HAProxy's
	// Tt total time; Caddy's "duration"; nginx upstream_response_time
	// when configured). Zero means "format didn't expose it".
	DurationMs float64 `json:"duration_ms,omitempty"`

	// HAProxy-specific topology — backend + server names. Other
	// profiles leave these empty.
	Backend string `json:"backend,omitempty"`
	Server  string `json:"server,omitempty"`

	// User-Agent and Referer from the "combined" log formats.
	UserAgent string `json:"user_agent,omitempty"`
	Referer   string `json:"referer,omitempty"`

	// Raw is a length-capped copy of the original line for the
	// dashboard's "see the actual line" affordance. Capped at
	// RawMaxLen below to keep payload sizes sane on full streams.
	Raw string `json:"raw"`
}

// RawMaxLen caps how much of the original line each Record carries
// through to JSON. HAProxy log lines on busy hosts can hit 2+ KB; we
// want enough for a human to recognise the request, not so much that
// shipping 5000 records to the dashboard becomes a bandwidth problem.
const RawMaxLen = 1024

// trimRaw returns raw capped to RawMaxLen with a trailing ellipsis
// when it was truncated. Single point of truth so each Profile's Raw
// field has consistent shape downstream.
func trimRaw(raw string) string {
	if len(raw) > RawMaxLen {
		return raw[:RawMaxLen] + "…"
	}
	return raw
}

// Parser is the interface every Profile implements. Implementations
// must:
//   - Return nil for any line that doesn't look like an HTTP access
//     log (no status code, wrong shape) — this is how callers filter
//     noise.
//   - Populate Profile with their own identifier.
//   - Be safe to call concurrently — Profiles hold no mutable state
//     after construction.
type Parser interface {
	Profile() string
	Parse(line string) *Record
}

// validStatusCode is the gating check used by every Profile. Anything
// outside 100–599 isn't an HTTP response and the line is ignored.
func validStatusCode(s int) bool {
	return s >= 100 && s <= 599
}

// Profile names — kept as constants so the gears that consume them
// can refer to a single source of truth rather than re-typing the
// string ("haproxy", "nginx", …) at every call site.
const (
	ProfileHAProxy        = "haproxy"
	ProfileNginxCombined  = "nginx-combined"
	ProfileApacheCommon   = "apache-common"
	ProfileApacheCombined = "apache-combined"
	ProfileCaddyJSON      = "caddy-json"
)

// AllProfiles returns every Parser the package supports, in a stable
// order. Used by the agent's source-to-profile registry and by tests
// that need to enumerate the supported set.
func AllProfiles() []Parser {
	return []Parser{
		HAProxyProfile{},
		NginxCombinedProfile{},
		ApacheCommonProfile{},
		ApacheCombinedProfile{},
		CaddyJSONProfile{},
	}
}

// ProfileByName returns the Parser whose Profile() equals name, or
// nil. Used by the access-log endpoint to dispatch on the
// {source}-style URL parameter without a switch in every caller.
func ProfileByName(name string) Parser {
	for _, p := range AllProfiles() {
		if strings.EqualFold(p.Profile(), name) {
			return p
		}
	}
	return nil
}
