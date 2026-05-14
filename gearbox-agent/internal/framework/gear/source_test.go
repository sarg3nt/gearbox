package gear

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// newTestManagerWithDeps mirrors newTestManager but lets the test seed
// Dependencies — needed to exercise the operator-override paths on
// ResolvePrimarySources (SourceOverrides).
func newTestManagerWithDeps(t *testing.T, deps Dependencies) (*Manager, *bytes.Buffer) {
	t.Helper()
	m := NewManager(deps, nil)
	buf := &bytes.Buffer{}
	m.tableWriter = buf
	return m, buf
}

// mockSourceGear is a gear that declares one or more MetricCategory
// values, suitable for exercising ResolvePrimarySources. Embeds
// mockProbeGear so it inherits the lifecycle counters and Probe
// behaviour the existing tests already rely on.
type mockSourceGear struct {
	mockProbeGear
	categories []MetricCategory
}

func (m *mockSourceGear) MetricCategories() []MetricCategory {
	return m.categories
}

func newSourceGear(name string, status ProbeStatus, cats ...MetricCategory) *mockSourceGear {
	pr := ProbeResult{Status: status, Reason: ""}
	g := &mockSourceGear{
		mockProbeGear: mockProbeGear{
			info:        Info{Name: name},
			probeResult: pr,
		},
		categories: cats,
	}
	return g
}

func TestResolvePrimarySources_AutoDetectPicksFirstAvailableFromPreference(t *testing.T) {
	// HAProxy first in preferenceOrder, nginx second; both Available.
	// Expect HAProxy to win and nginx to be listed as an alternative.
	hap := newSourceGear("haproxy", ProbeStatusAvailable, CategoryHTTPRequests)
	ngx := newSourceGear("nginx", ProbeStatusAvailable, CategoryHTTPRequests)
	withTestRegistry(t, hap, ngx)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())

	picks := m.ResolvePrimarySources()
	sel, ok := picks[CategoryHTTPRequests]
	if !ok {
		t.Fatal("CategoryHTTPRequests should have a primary")
	}
	if sel.Source != "haproxy" {
		t.Errorf("source = %q, want haproxy (first in preferenceOrder)", sel.Source)
	}
	if !strings.Contains(sel.Reason, "auto-detected") {
		t.Errorf("reason should mention auto-detect, got %q", sel.Reason)
	}
	if len(sel.Alternatives) != 1 || sel.Alternatives[0] != "nginx" {
		t.Errorf("alternatives = %v, want [nginx]", sel.Alternatives)
	}
}

func TestResolvePrimarySources_OverrideWinsOverPreferenceOrder(t *testing.T) {
	hap := newSourceGear("haproxy", ProbeStatusAvailable, CategoryHTTPRequests)
	ngx := newSourceGear("nginx", ProbeStatusAvailable, CategoryHTTPRequests)
	withTestRegistry(t, hap, ngx)

	deps := Dependencies{
		SourceOverrides: map[MetricCategory]string{
			CategoryHTTPRequests: "nginx",
		},
	}
	m, _ := newTestManagerWithDeps(t, deps)
	m.ProbeAll(context.Background())

	sel := m.ResolvePrimarySources()[CategoryHTTPRequests]
	if sel.Source != "nginx" {
		t.Errorf("override should pick nginx, got %q", sel.Source)
	}
	if !strings.Contains(sel.Reason, "operator override") {
		t.Errorf("reason should mention override, got %q", sel.Reason)
	}
	if !strings.Contains(sel.Reason, "GEARBOX_AGENT_HTTP_SOURCE") {
		t.Errorf("reason should name the env var, got %q", sel.Reason)
	}
	// Alternatives should list what would have been picked otherwise,
	// minus the chosen source. HAProxy comes first in preferenceOrder.
	if len(sel.Alternatives) != 1 || sel.Alternatives[0] != "haproxy" {
		t.Errorf("alternatives = %v, want [haproxy]", sel.Alternatives)
	}
}

func TestResolvePrimarySources_OverrideFallsBackWhenTargetNotAvailable(t *testing.T) {
	// Override names nginx, but nginx didn't probe Available — fall
	// back to auto-detect (HAProxy wins) and log a warning rather than
	// dropping HTTP metrics entirely.
	hap := newSourceGear("haproxy", ProbeStatusAvailable, CategoryHTTPRequests)
	ngx := newSourceGear("nginx", ProbeStatusNotInstalled, CategoryHTTPRequests)
	withTestRegistry(t, hap, ngx)

	deps := Dependencies{
		SourceOverrides: map[MetricCategory]string{
			CategoryHTTPRequests: "nginx",
		},
	}
	m, _ := newTestManagerWithDeps(t, deps)
	m.ProbeAll(context.Background())

	sel := m.ResolvePrimarySources()[CategoryHTTPRequests]
	if sel.Source != "haproxy" {
		t.Errorf("override target unavailable → should fall back to haproxy, got %q", sel.Source)
	}
	if !strings.Contains(sel.Reason, "auto-detected") {
		t.Errorf("fallback should be marked auto-detected, got %q", sel.Reason)
	}
}

func TestResolvePrimarySources_OverrideFallsBackWhenTargetUnknown(t *testing.T) {
	// Override names a gear that isn't registered as a producer for
	// this category at all (e.g. nginx not yet implemented, operator
	// templating an env file for a future deploy). Same fallback +
	// warning behaviour as unavailable.
	hap := newSourceGear("haproxy", ProbeStatusAvailable, CategoryHTTPRequests)
	withTestRegistry(t, hap)

	deps := Dependencies{
		SourceOverrides: map[MetricCategory]string{
			CategoryHTTPRequests: "imaginary-server",
		},
	}
	m, _ := newTestManagerWithDeps(t, deps)
	m.ProbeAll(context.Background())

	sel := m.ResolvePrimarySources()[CategoryHTTPRequests]
	if sel.Source != "haproxy" {
		t.Errorf("unknown override → should fall back to haproxy, got %q", sel.Source)
	}
}

func TestResolvePrimarySources_NoAvailableProducerOmitsCategory(t *testing.T) {
	// Only producer for HTTP is HAProxy, but it probed NotInstalled.
	// Category should be omitted from the result, not appear with an
	// empty Source — callers distinguish "no primary" via key absence.
	hap := newSourceGear("haproxy", ProbeStatusNotInstalled, CategoryHTTPRequests)
	withTestRegistry(t, hap)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())

	if _, ok := m.ResolvePrimarySources()[CategoryHTTPRequests]; ok {
		t.Error("CategoryHTTPRequests should be omitted when no producer is Available")
	}
}

func TestResolvePrimarySources_OverrideForCategoryNoProducerOmitsCategory(t *testing.T) {
	// No producer registered for this category at all. Override is
	// meaningless and should not cause a synthetic entry.
	withTestRegistry(t)

	deps := Dependencies{
		SourceOverrides: map[MetricCategory]string{
			CategoryHTTPRequests: "nginx",
		},
	}
	m, _ := newTestManagerWithDeps(t, deps)
	m.ProbeAll(context.Background())

	if _, ok := m.ResolvePrimarySources()[CategoryHTTPRequests]; ok {
		t.Error("no registered producers → category should be omitted regardless of override")
	}
}

func TestResolvePrimarySources_UnrankedProducerStillSurfacesAsAlternative(t *testing.T) {
	// A producer that isn't in preferenceOrder for this category
	// should still show up in alternatives so the dashboard's "switch
	// source" UI can offer it. Alphabetical tiebreak among unranked
	// producers.
	hap := newSourceGear("haproxy", ProbeStatusAvailable, CategoryHTTPRequests)
	weird := newSourceGear("zebra-proxy", ProbeStatusAvailable, CategoryHTTPRequests)
	withTestRegistry(t, hap, weird)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())

	sel := m.ResolvePrimarySources()[CategoryHTTPRequests]
	if sel.Source != "haproxy" {
		t.Fatalf("primary should be haproxy, got %q", sel.Source)
	}
	if len(sel.Alternatives) != 1 || sel.Alternatives[0] != "zebra-proxy" {
		t.Errorf("alternatives = %v, want [zebra-proxy]", sel.Alternatives)
	}
}

func TestCapabilitiesEndpointIncludesPrimarySources(t *testing.T) {
	// End-to-end: /api/v1/system/capabilities response includes the
	// resolved primary_sources alongside the existing gears table.
	hap := newSourceGear("haproxy", ProbeStatusAvailable, CategoryHTTPRequests)
	withTestRegistry(t, hap)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())

	r := chi.NewRouter()
	m.RegisterSystemRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp CapabilitiesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp.Gears["haproxy"]; !ok {
		t.Error("response should include haproxy in Gears")
	}
	sel, ok := resp.PrimarySources[CategoryHTTPRequests]
	if !ok {
		t.Fatal("primary_sources should include CategoryHTTPRequests")
	}
	if sel.Source != "haproxy" {
		t.Errorf("primary source = %q, want haproxy", sel.Source)
	}
}
