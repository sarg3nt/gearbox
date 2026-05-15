package collector

import (
	"errors"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
)

func TestNormaliseSourceStatsNginx(t *testing.T) {
	// JSON-decoded floats: encoding/json gives every numeric value
	// as float64 regardless of the wire shape, so we have to
	// reproduce that here for the test to match production.
	raw := agent.SourceStats{
		"active":       float64(291),
		"reading":      float64(6),
		"writing":      float64(179),
		"waiting":      float64(106),
		"accepts":      float64(16630948),
		"handled":      float64(16630948),
		"requests":     float64(31070465),
		"collected_at": "2026-05-15T04:00:00Z",
	}
	got := normaliseSourceStats("server-1", "nginx", raw)
	if got == nil {
		t.Fatal("expected snapshot")
	}
	if got.RequestsTotal != 31070465 {
		t.Errorf("RequestsTotal = %d, want 31070465 (nginx 'requests' counter)", got.RequestsTotal)
	}
	if got.ActiveConnections != 291 {
		t.Errorf("ActiveConnections = %d, want 291 (nginx 'active')", got.ActiveConnections)
	}
	// reading/writing/waiting/accepts/handled must land in Extra
	// for the dashboard's stacked-connection chart.
	for _, k := range []string{"reading", "writing", "waiting", "accepts", "handled"} {
		if _, ok := got.Extra[k]; !ok {
			t.Errorf("Extra[%q] missing", k)
		}
	}
	if got.CollectedAt.UTC().Format(time.RFC3339) != "2026-05-15T04:00:00Z" {
		t.Errorf("CollectedAt = %v, want round-trip of agent timestamp", got.CollectedAt)
	}
}

func TestNormaliseSourceStatsApacheUsesBusyWorkersAsActive(t *testing.T) {
	// Apache doesn't have a direct "active connections" — busy
	// workers is the closest analogue for the dashboard's parity
	// chart against nginx. Idle workers + CPU load + uptime move
	// to Extra so the per-source card can render them too.
	raw := agent.SourceStats{
		"total_accesses": float64(1234),
		"busy_workers":   float64(5),
		"idle_workers":   float64(95),
		"cpu_load":       float64(0.5),
		"uptime_seconds": float64(1000),
		"collected_at":   "2026-05-15T04:00:00Z",
	}
	got := normaliseSourceStats("s", "apache", raw)
	if got.RequestsTotal != 1234 {
		t.Errorf("RequestsTotal = %d, want 1234", got.RequestsTotal)
	}
	if got.ActiveConnections != 5 {
		t.Errorf("ActiveConnections = %d, want 5 (busy_workers)", got.ActiveConnections)
	}
	if got.Extra["idle_workers"].(float64) != 95 {
		t.Errorf("Extra[idle_workers] = %v, want 95", got.Extra["idle_workers"])
	}
}

func TestNormaliseSourceStatsCaddy(t *testing.T) {
	raw := agent.SourceStats{
		"requests_total":       float64(59),
		"request_errors_total": float64(3),
		"collected_at":         "2026-05-15T04:00:00Z",
	}
	got := normaliseSourceStats("s", "caddy", raw)
	if got.RequestsTotal != 59 {
		t.Errorf("RequestsTotal = %d, want 59", got.RequestsTotal)
	}
	// Caddy's error counter lands in Extra (no status-class
	// breakdown by default).
	if got.Extra["request_errors_total"].(float64) != 3 {
		t.Errorf("Extra[request_errors_total] = %v, want 3", got.Extra["request_errors_total"])
	}
}

func TestNormaliseSourceStatsTraefikSplitsStatusClass(t *testing.T) {
	raw := agent.SourceStats{
		"requests_total": float64(116),
		"response_2xx":   float64(104),
		"response_4xx":   float64(7),
		"response_5xx":   float64(5),
		"entrypoints":    []any{"web", "websecure"},
		"collected_at":   "2026-05-15T04:00:00Z",
	}
	got := normaliseSourceStats("s", "traefik", raw)
	if got.RequestsTotal != 116 || got.Response2xx != 104 || got.Response5xx != 5 {
		t.Errorf("rollups wrong: %+v", got)
	}
	// Entrypoints list must survive the normalisation so the
	// dashboard can render the entrypoint badges.
	if eps, ok := got.Extra["entrypoints"]; !ok || eps == nil {
		t.Errorf("Extra[entrypoints] missing")
	}
}

func TestNormaliseSourceStatsCollectedAtFallsBackToNow(t *testing.T) {
	// Agent payloads without collected_at (or with garbage) get
	// time.Now stamped so the row still lands rather than getting
	// dropped silently.
	raw := agent.SourceStats{"requests_total": float64(1)}
	before := time.Now().UTC().Add(-time.Second)
	got := normaliseSourceStats("s", "caddy", raw)
	if got.CollectedAt.Before(before) {
		t.Errorf("CollectedAt = %v, expected fallback ~now", got.CollectedAt)
	}
}

func TestNormaliseSourceStatsTolerantOfMissingFields(t *testing.T) {
	// Empty payload (e.g. agent returned a body that decoded
	// cleanly but had no useful fields) — must produce a
	// zero-valued snapshot, not nil. Persisting the zeros is the
	// honest thing to do; the dashboard will render "no activity".
	got := normaliseSourceStats("s", "nginx", agent.SourceStats{})
	if got == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if got.RequestsTotal != 0 || got.ActiveConnections != 0 {
		t.Errorf("expected zero counters for empty payload, got %+v", got)
	}
}

func TestIsExpectedSourceMiss(t *testing.T) {
	// The agent client returns *agent.APIError for any non-2xx
	// response — we match on the typed field, not the message text.
	// 404 and 503 are the "source not present / not ready" shapes
	// we swallow; everything else surfaces as a warning.
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"503 (not yet collected)", &agent.APIError{StatusCode: 503, Message: "stats not yet collected"}, true},
		{"404 (endpoint missing on older agent)", &agent.APIError{StatusCode: 404, Message: "not found"}, true},
		{"500 (real failure)", &agent.APIError{StatusCode: 500, Message: "boom"}, false},
		{"plain error (transport)", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isExpectedSourceMiss(tc.err); got != tc.want {
				t.Errorf("isExpectedSourceMiss(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
