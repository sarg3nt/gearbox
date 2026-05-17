package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// newResolveTestHandler builds the minimum Handler state the resolver
// needs for the URL-query-param paths: a static `servers` slice (so the
// cookie path can validate against enabled servers if exercised). Tests
// that exercise the cookie or first-server fallback would also need a
// DB; those branches aren't covered here.
func newResolveTestHandler(servers ...models.BoxConfig) *Handler {
	return &Handler{servers: servers}
}

// TestResolveBoxIDFromRequest_ServerParam covers the historical
// `?server=<id>` path — gear-settings links and the OS-Updates page
// still emit this form.
func TestResolveBoxIDFromRequest_ServerParam(t *testing.T) {
	h := newResolveTestHandler()
	r := httptest.NewRequest("GET", "/anything?server=mjolnir", nil)
	if got := h.resolveBoxIDFromRequest(r); got != "mjolnir" {
		t.Errorf("resolveBoxIDFromRequest with ?server=mjolnir = %q, want %q", got, "mjolnir")
	}
}

// TestResolveBoxIDFromRequest_BoxIDParam covers the synonym alias added
// in issue #112 Phase 4. `?box_id=` is the form the header-pill
// middleware writes; pre-Phase-4 the handler-level resolver ignored it
// because it only knew `?server=`. Recognizing both keeps the resolver
// consistent across the handler / middleware / gear-plugin layers.
func TestResolveBoxIDFromRequest_BoxIDParam(t *testing.T) {
	h := newResolveTestHandler()
	r := httptest.NewRequest("GET", "/anything?box_id=mjolnir", nil)
	if got := h.resolveBoxIDFromRequest(r); got != "mjolnir" {
		t.Errorf("resolveBoxIDFromRequest with ?box_id=mjolnir = %q, want %q", got, "mjolnir")
	}
}

// TestResolveBoxIDFromRequest_ServerWinsOverBoxID is the precedence
// guard: when a request happens to carry both (rare — typically only
// when an operator hand-edits a URL), `?server=` wins because it's the
// older, more-explicit convention used by gear-settings deep links.
func TestResolveBoxIDFromRequest_ServerWinsOverBoxID(t *testing.T) {
	h := newResolveTestHandler()
	r := httptest.NewRequest("GET", "/anything?server=alpha&box_id=beta", nil)
	if got := h.resolveBoxIDFromRequest(r); got != "alpha" {
		t.Errorf("resolveBoxIDFromRequest with both params = %q, want %q", got, "alpha")
	}
}

// TestResolveActiveBox_ReturnsServerConfig verifies that the new
// resolveActiveBox returns the full BoxConfig (not just an ID) by
// matching against the handler's static `servers` slice.
func TestResolveActiveBox_ReturnsServerConfig(t *testing.T) {
	h := newResolveTestHandler(
		models.BoxConfig{ID: "mjolnir", Name: "Mjolnir", AgentURL: "https://10.0.0.1:8405"},
	)
	r := httptest.NewRequest("GET", "/anything?box_id=mjolnir", nil)
	box, ok := h.resolveActiveBox(r)
	if !ok {
		t.Fatalf("resolveActiveBox = (_, false), want a BoxConfig")
	}
	if box.ID != "mjolnir" || box.AgentURL != "https://10.0.0.1:8405" {
		t.Errorf("resolveActiveBox returned %+v, want {ID:mjolnir, AgentURL:https://10.0.0.1:8405}", box)
	}
}

// The "resolved ID doesn't match any configured server" branch isn't
// covered here because exercising it would require a DB stub —
// getServerConfig falls back to getEnabledServers (a DB read) when the
// static list misses, and the DB layer panics on nil. The static-list
// happy path above guards the helper's primary contract (an ID that
// matches a configured server returns the right BoxConfig); the
// dynamic-server path is exercised by existing handler integration
// tests that run against the full Handler fixture.
