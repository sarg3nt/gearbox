package handler

import (
	"testing"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
)

func boxCaps(entries map[string]agent.CapabilityEntry) *agent.BoxCapabilities {
	return &agent.BoxCapabilities{
		Response: &agent.CapabilitiesResponse{Gears: entries},
	}
}

// TestLogSourcesFromResources_PreferredShape: when the access-log gear
// publishes `log_sources` in the Resources map, the dropdown is built
// directly from it. Issue #112 Phase 2 — the dashboard stops inferring
// sources from gear-availability flags.
func TestLogSourcesFromResources_PreferredShape(t *testing.T) {
	caps := boxCaps(map[string]agent.CapabilityEntry{
		"access-log": {
			Status: "available",
			Resources: map[string]any{
				// Use the JSON-decoded shape to exercise the production path.
				"log_sources": []any{
					map[string]any{"name": "haproxy", "display_name": "HAProxy", "path": "/var/log/haproxy.log"},
					map[string]any{"name": "nginx", "display_name": "nginx", "path": "/var/log/nginx/access.log"},
				},
			},
		},
	})

	got, ok := logSourcesFromResources(caps)
	if !ok {
		t.Fatalf("logSourcesFromResources returned false; expected to consume the resource")
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[0]["name"] != "haproxy" || got[0]["display_name"] != "HAProxy" {
		t.Errorf("first entry = %+v, want haproxy/HAProxy", got[0])
	}
	if _, hasPath := got[0]["path"]; hasPath {
		t.Errorf("path field leaked through to dropdown shape: %+v", got[0])
	}
}

// TestLogSourcesFromResources_GoTypedShape exercises the tolerated
// alternate input shape — when tests or in-process callers construct
// the resource as a Go-typed []map[string]string, the helper accepts
// it without forcing a JSON round-trip.
func TestLogSourcesFromResources_GoTypedShape(t *testing.T) {
	caps := boxCaps(map[string]agent.CapabilityEntry{
		"access-log": {
			Status: "available",
			Resources: map[string]any{
				"log_sources": []map[string]string{
					{"name": "caddy", "display_name": "Caddy", "path": "/var/log/caddy/access.log"},
				},
			},
		},
	})
	got, ok := logSourcesFromResources(caps)
	if !ok || len(got) != 1 || got[0]["name"] != "caddy" {
		t.Fatalf("logSourcesFromResources for Go-typed shape = (%+v, %v), want one caddy entry", got, ok)
	}
}

// TestLogSourcesFromResources_AccessLogGearMissing: an older agent
// that doesn't surface the access-log gear at all yields (nil, false)
// so the caller falls through to the capability heuristic instead of
// returning an empty dropdown.
func TestLogSourcesFromResources_AccessLogGearMissing(t *testing.T) {
	caps := boxCaps(map[string]agent.CapabilityEntry{
		"host":    {Status: "available"},
		"metrics": {Status: "available"},
	})
	if got, ok := logSourcesFromResources(caps); ok {
		t.Errorf("logSourcesFromResources for missing access-log = (%+v, true), want (nil, false)", got)
	}
}

// TestLogSourcesFromResources_ResourceKeyMissing: the access-log gear
// is surfaced but pre-Phase-2 — Resources map is nil or doesn't carry
// log_sources. Caller falls through to the heuristic.
func TestLogSourcesFromResources_ResourceKeyMissing(t *testing.T) {
	caps := boxCaps(map[string]agent.CapabilityEntry{
		"access-log": {
			Status: "available",
			Capabilities: map[string]string{
				"nginx_log": "/var/log/nginx/access.log",
			},
		},
	})
	if got, ok := logSourcesFromResources(caps); ok {
		t.Errorf("logSourcesFromResources for missing log_sources key = (%+v, true), want (nil, false)", got)
	}
}

// TestLogSourcesFromResources_RejectsBadShape: a malformed payload
// (string where an array was expected, or array of strings instead of
// objects) yields (nil, false). Defensive — guards against future agent
// changes producing the wrong shape.
func TestLogSourcesFromResources_RejectsBadShape(t *testing.T) {
	cases := []struct {
		name string
		val  any
	}{
		{"string instead of array", "haproxy,nginx"},
		{"array of strings", []any{"haproxy", "nginx"}},
		{"array of objects missing display_name", []any{
			map[string]any{"name": "haproxy"},
		}},
		{"array of objects missing name", []any{
			map[string]any{"display_name": "HAProxy"},
		}},
		{"empty array", []any{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			caps := boxCaps(map[string]agent.CapabilityEntry{
				"access-log": {
					Status:    "available",
					Resources: map[string]any{"log_sources": c.val},
				},
			})
			if got, ok := logSourcesFromResources(caps); ok {
				t.Errorf("malformed %q accepted = (%+v, true), want (nil, false)", c.name, got)
			}
		})
	}
}
