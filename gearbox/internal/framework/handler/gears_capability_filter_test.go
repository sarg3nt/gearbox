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
	"github.com/sarg3nt/gearbox/internal/framework/database"
	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// newCapabilitiesHandler returns an http.Handler that serves the supplied
// CapabilitiesResponse on /api/v1/system/capabilities. Used by the tests
// below to drive the filter through a real httptest.Server so the
// production CapabilitiesCache fetch path (not just a stubbed cache) is
// exercised.
func newCapabilitiesHandler(t *testing.T, resp agent.CapabilitiesResponse) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/capabilities" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode capabilities: %v", err)
		}
	})
}

// newCapabilitiesTestHandler stitches together the minimum Handler state
// the filter needs: a logger, a static `servers` slice (so getServerConfig
// resolves without DB), and a fresh CapabilitiesCache.
func newCapabilitiesTestHandler(t *testing.T, boxID, agentURL string) *Handler {
	t.Helper()
	return &Handler{
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		capabilities: agent.NewCapabilitiesCache(5*time.Minute, 2*time.Second),
		servers: []models.BoxConfig{
			{ID: boxID, AgentURL: agentURL, APIKey: "test-key"},
		},
	}
}

func gearsByName(in []database.Gear) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, g := range in {
		out[g.Name] = true
	}
	return out
}

// TestFilterGearsByAgentCapabilities_HidesUnavailable mirrors the
// production Mjolnir agent's probe table: access-log/host/metrics
// available, every other gear unavailable. The filter must keep only
// dashboard gears whose agent counterpart is available, drop gears
// whose counterpart is reported unavailable, and leave dashboard-only
// gears (alerts, home, bx) untouched. Guards both the Gears settings
// page and the sidebar middleware path (issue #112).
func TestFilterGearsByAgentCapabilities_HidesUnavailable(t *testing.T) {
	srv := httptest.NewServer(newCapabilitiesHandler(t, agent.CapabilitiesResponse{
		Gears: map[string]agent.CapabilityEntry{
			"access-log":   {Status: "available"},
			"host":         {Status: "available"},
			"metrics":      {Status: "available"},
			"apache":       {Status: "not_installed"},
			"caddy":        {Status: "not_installed"},
			"certificates": {Status: "not_installed"},
			"docker":       {Status: "not_installed"},
			"haproxy":      {Status: "inaccessible"},
			"logs":         {Status: "inaccessible"},
			"nginx":        {Status: "not_installed"},
			"security":     {Status: "not_installed"},
			"traefik":      {Status: "not_installed"},
			"traffic":      {Status: "inaccessible"},
			"updates":      {Status: "not_installed"},
		},
	}))
	t.Cleanup(srv.Close)

	h := newCapabilitiesTestHandler(t, "mjolnir", srv.URL)
	dashboardGears := []database.Gear{
		{Name: database.GearHAProxy},
		{Name: database.GearLogs},
		{Name: database.GearMetrics},
		{Name: database.GearServices},
		{Name: database.GearCertificates},
		{Name: database.GearTraffic},
		{Name: database.GearOSUpdates},
		{Name: database.GearAlerts},
	}

	got := gearsByName(h.filterGearsByAgentCapabilities("mjolnir", dashboardGears))

	want := map[string]bool{
		// metrics is available → keep
		database.GearMetrics: true,
		// services maps to "metrics" (which is available) → keep
		database.GearServices: true,
		// alerts has no agent counterpart → keep (always-on)
		database.GearAlerts: true,
	}
	// haproxy / logs / traffic mapped to inaccessible agent gears → drop.
	// certificates / updates mapped to not_installed → drop.
	for name := range gearsByName(dashboardGears) {
		if want[name] {
			if !got[name] {
				t.Errorf("filter dropped %q but it should remain", name)
			}
			continue
		}
		if got[name] {
			t.Errorf("filter kept %q but it should be hidden (agent gear unavailable)", name)
		}
	}
}

// TestFilterGearsByAgentCapabilities_FailsOpenOnAgentDown verifies that
// when capabilities can't be fetched, the filter returns the full list
// unchanged. Same contract as the Gears settings page — a flaky agent
// must not collapse the sidebar / settings page (issue #112).
func TestFilterGearsByAgentCapabilities_FailsOpenOnAgentDown(t *testing.T) {
	// AgentURL points at a closed port so the fetch errors. APIKey isn't
	// empty so the dashboard considers the box agent-API-backed.
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	closed.Close()

	h := newCapabilitiesTestHandler(t, "downbox", closed.URL)
	gears := []database.Gear{
		{Name: database.GearHAProxy},
		{Name: database.GearLogs},
		{Name: database.GearMetrics},
	}
	got := h.filterGearsByAgentCapabilities("downbox", gears)
	if len(got) != len(gears) {
		t.Errorf("fail-open broken: filter returned %d gears, expected all %d", len(got), len(gears))
	}
}

// TestFilterGearsByAgentCapabilities_FailsOpenOnUnknownGear verifies
// that an agent that doesn't surface a particular gear name at all
// (older agent that pre-dates the gear) leaves the dashboard gear in
// the result — distinguishing "not reported" from "reported as
// unavailable" is the same fix #116 added on the Logs source picker.
func TestFilterGearsByAgentCapabilities_FailsOpenOnUnknownGear(t *testing.T) {
	srv := httptest.NewServer(newCapabilitiesHandler(t, agent.CapabilitiesResponse{
		// Older agent: surfaces only haproxy; doesn't know about logs/metrics yet.
		Gears: map[string]agent.CapabilityEntry{
			"haproxy": {Status: "available"},
		},
	}))
	t.Cleanup(srv.Close)

	h := newCapabilitiesTestHandler(t, "oldbox", srv.URL)
	gears := []database.Gear{
		{Name: database.GearHAProxy},
		{Name: database.GearLogs},
		{Name: database.GearMetrics},
	}
	got := gearsByName(h.filterGearsByAgentCapabilities("oldbox", gears))
	// All three should be present: haproxy because available, logs and
	// metrics because the agent didn't report them (forward-compat).
	for _, name := range []string{database.GearHAProxy, database.GearLogs, database.GearMetrics} {
		if !got[name] {
			t.Errorf("forward-compat fail-open broken: %q dropped", name)
		}
	}
}
