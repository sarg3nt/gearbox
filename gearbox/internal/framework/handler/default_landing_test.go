package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// fakeCapabilitiesServer serves the supplied CapabilitiesResponse on the
// agent's well-known capabilities path, so defaultLandingForActiveBox
// goes through the production cache fetch path rather than a stubbed
// cache. Returns an httptest.Server the caller is responsible for closing.
func fakeCapabilitiesServer(t *testing.T, resp agent.CapabilitiesResponse) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/capabilities" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode capabilities: %v", err)
		}
	}))
}

// newLandingTestHandler builds the minimum Handler state
// defaultLandingForActiveBox needs: a logger, a static `servers` slice
// (so getServerConfig resolves without DB), and a fresh CapabilitiesCache.
func newLandingTestHandler(t *testing.T, boxID, agentURL string) *Handler {
	t.Helper()
	return &Handler{
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		capabilities: agent.NewCapabilitiesCache(5*time.Minute, 2*time.Second),
		servers: []models.BoxConfig{
			{ID: boxID, AgentURL: agentURL, APIKey: "test-key"},
		},
	}
}

// requestWithCookie returns an *http.Request with the gearbox_active_box
// cookie set to boxID, so resolveBoxIDFromRequest picks up that box
// without needing a `?server=` URL param.
func requestWithCookie(boxID string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: activeBoxCookieName, Value: boxID})
	return r
}

// TestDefaultLandingForActiveBox_PrefersHAProxy: when the active box's
// agent advertises haproxy, the landing route is /haproxy regardless of
// what else is available. Preserves the historical landing for any
// HAProxy-fronted deployment.
func TestDefaultLandingForActiveBox_PrefersHAProxy(t *testing.T) {
	srv := fakeCapabilitiesServer(t, agent.CapabilitiesResponse{
		Gears: map[string]agent.CapabilityEntry{
			"haproxy": {Status: "available"},
			"metrics": {Status: "available"},
		},
	})
	t.Cleanup(srv.Close)

	h := newLandingTestHandler(t, "light-hugger", srv.URL)
	// resolveBoxIDFromRequest needs the box to be in the enabled list to
	// honor the cookie. Without a DB the helper falls back to
	// getDefaultServerID() which returns "" — but the cookie path requires
	// the box to be in getEnabledServers. We sidestep that by passing the
	// box through `?server=` which always wins.
	r := httptest.NewRequest(http.MethodGet, "/?server=light-hugger", nil)
	if got := h.defaultLandingForActiveBox(r); got != "/haproxy" {
		t.Errorf("haproxy-capable box landed on %q, want /haproxy", got)
	}
}

// TestDefaultLandingForActiveBox_FallsBackToMetrics: an active box with
// no haproxy gear but with metrics available lands on /metrics. This is
// the Mjolnir scenario — a TrueNAS container agent.
func TestDefaultLandingForActiveBox_FallsBackToMetrics(t *testing.T) {
	srv := fakeCapabilitiesServer(t, agent.CapabilitiesResponse{
		Gears: map[string]agent.CapabilityEntry{
			"haproxy":    {Status: "inaccessible"},
			"metrics":    {Status: "available"},
			"host":       {Status: "available"},
			"access-log": {Status: "available"},
		},
	})
	t.Cleanup(srv.Close)

	h := newLandingTestHandler(t, "mjolnir", srv.URL)
	r := httptest.NewRequest(http.MethodGet, "/?server=mjolnir", nil)
	if got := h.defaultLandingForActiveBox(r); got != "/metrics" {
		t.Errorf("metrics-only box landed on %q, want /metrics", got)
	}
}

// TestDefaultLandingForActiveBox_FallsBackToBx: an active box with
// neither haproxy nor metrics still gets a sensible landing — the Bx
// fleet view is universally available.
func TestDefaultLandingForActiveBox_FallsBackToBx(t *testing.T) {
	srv := fakeCapabilitiesServer(t, agent.CapabilitiesResponse{
		Gears: map[string]agent.CapabilityEntry{
			"haproxy": {Status: "inaccessible"},
			"metrics": {Status: "inaccessible"},
			"host":    {Status: "available"},
		},
	})
	t.Cleanup(srv.Close)

	h := newLandingTestHandler(t, "minimal", srv.URL)
	r := httptest.NewRequest(http.MethodGet, "/?server=minimal", nil)
	if got := h.defaultLandingForActiveBox(r); got != "/bx" {
		t.Errorf("minimal box landed on %q, want /bx", got)
	}
}

// TestDefaultLandingForActiveBox_FailOpenOnAgentDown: when capabilities
// can't be fetched (agent unreachable), the landing route falls back to
// /haproxy to match historical behavior. Same contract as the rest of
// the capability-driven helpers added in #112 — a flaky agent doesn't
// change the dashboard's behavior.
func TestDefaultLandingForActiveBox_FailOpenOnAgentDown(t *testing.T) {
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	closed.Close() // close immediately so the URL refuses connections

	h := newLandingTestHandler(t, "downbox", closed.URL)
	r := httptest.NewRequest(http.MethodGet, "/?server=downbox", nil)
	if got := h.defaultLandingForActiveBox(r); got != "/haproxy" {
		t.Errorf("unreachable agent landed on %q, want /haproxy (fail-open)", got)
	}
}

// The "no resolvable box" case isn't covered here — RootRedirect's
// CountEnabledBoxes check fires before defaultLandingForActiveBox is
// called, so a no-boxes install never reaches this helper in
// production. Testing it would require stubbing the database layer,
// which isn't worth the setup cost for an unreachable branch.
