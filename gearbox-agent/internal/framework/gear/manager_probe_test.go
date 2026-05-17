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

// withTestRegistry swaps the global registry's plugin map for an isolated
// one populated with the given gears, then restores it on test cleanup.
// Required because ProbeAll/Manager talks to the package-level registry
// and we need each test to control the registered set.
func withTestRegistry(t *testing.T, plugins ...Gear) {
	t.Helper()
	registry.mu.Lock()
	original := registry.plugins
	registry.plugins = make(map[string]Gear)
	for _, p := range plugins {
		registry.plugins[p.Info().Name] = p
	}
	registry.mu.Unlock()
	t.Cleanup(func() {
		registry.mu.Lock()
		registry.plugins = original
		registry.mu.Unlock()
	})
}

// mockProbeGear is a Gear that records lifecycle calls and returns a
// caller-supplied probe verdict. The lifecycle counters let tests assert
// that skipped gears are genuinely skipped (Initialize=0, Start=0,
// RegisterRoutes=0).
type mockProbeGear struct {
	BaseGear
	info               Info
	probeResult        ProbeResult
	initializeCount    int
	startCount         int
	registerRoutesHits int
}

func (m *mockProbeGear) Info() Info { return m.info }

func (m *mockProbeGear) Initialize(ctx context.Context, deps Dependencies) error {
	m.initializeCount++
	return nil
}

func (m *mockProbeGear) Start(ctx context.Context) error {
	m.startCount++
	return nil
}

func (m *mockProbeGear) Stop(ctx context.Context) error { return nil }
func (m *mockProbeGear) Health() HealthStatus           { return NewHealthyStatus("test") }
func (m *mockProbeGear) RegisterRoutes(r chi.Router) {
	m.registerRoutesHits++
	r.Get("/api/v1/"+m.info.Name, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
func (m *mockProbeGear) EventTypes() []EventType { return nil }
func (m *mockProbeGear) Probe(ctx context.Context, deps Dependencies) ProbeResult {
	return m.probeResult
}

// noProbeGear has no Probe method — used to verify the manager defaults
// to Available for gears that haven't been migrated to ProbeableGear.
type noProbeGear struct {
	BaseGear
	info            Info
	initializeCount int
}

func (n *noProbeGear) Info() Info { return n.info }
func (n *noProbeGear) Initialize(ctx context.Context, deps Dependencies) error {
	n.initializeCount++
	return nil
}
func (n *noProbeGear) Start(ctx context.Context) error  { return nil }
func (n *noProbeGear) Stop(ctx context.Context) error   { return nil }
func (n *noProbeGear) Health() HealthStatus             { return NewHealthyStatus("test") }
func (n *noProbeGear) RegisterRoutes(r chi.Router)      {}
func (n *noProbeGear) EventTypes() []EventType          { return nil }

// newTestManager returns a Manager that writes its probe table into the
// returned buffer instead of stderr, so tests can assert on the output.
func newTestManager(t *testing.T) (*Manager, *bytes.Buffer) {
	t.Helper()
	m := NewManager(Dependencies{}, nil)
	buf := &bytes.Buffer{}
	m.tableWriter = buf
	return m, buf
}

func TestProbeAllRecordsResultsForEveryGear(t *testing.T) {
	a := &mockProbeGear{info: Info{Name: "alpha"}, probeResult: ProbeAvailable("ok", nil)}
	b := &mockProbeGear{info: Info{Name: "bravo"}, probeResult: ProbeNotInstalled("missing")}
	c := &mockProbeGear{info: Info{Name: "charlie"}, probeResult: ProbeInaccessible("EACCES")}
	withTestRegistry(t, a, b, c)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())

	results := m.ProbeResults()
	if got := results["alpha"].Status; got != ProbeStatusAvailable {
		t.Errorf("alpha status = %v, want Available", got)
	}
	if got := results["bravo"].Status; got != ProbeStatusNotInstalled {
		t.Errorf("bravo status = %v, want NotInstalled", got)
	}
	if got := results["charlie"].Status; got != ProbeStatusInaccessible {
		t.Errorf("charlie status = %v, want Inaccessible", got)
	}
}

func TestGearWithoutProbeDefaultsToAvailable(t *testing.T) {
	g := &noProbeGear{info: Info{Name: "vanilla"}}
	withTestRegistry(t, g)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())

	results := m.ProbeResults()
	if got := results["vanilla"].Status; got != ProbeStatusAvailable {
		t.Errorf("non-probeable gear status = %v, want Available (the safe default)", got)
	}
}

func TestIsLoadedDefaultsToTrueBeforeProbe(t *testing.T) {
	// A Manager that has never run ProbeAll must treat every gear as
	// loaded — otherwise callers that haven't migrated to the probe flow
	// would silently lose all their gears.
	m, _ := newTestManager(t)
	if !m.isLoaded("anything") {
		t.Error("isLoaded should default to true before ProbeAll runs")
	}
}

func TestInitializeAllSkipsNonAvailableGears(t *testing.T) {
	yes := &mockProbeGear{info: Info{Name: "yes"}, probeResult: ProbeAvailable("ok", nil)}
	no := &mockProbeGear{info: Info{Name: "no"}, probeResult: ProbeNotInstalled("nope")}
	withTestRegistry(t, yes, no)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())
	if err := m.InitializeAll(context.Background()); err != nil {
		t.Fatalf("InitializeAll: %v", err)
	}

	if yes.initializeCount != 1 {
		t.Errorf("expected Available gear to be Initialized once, got %d calls", yes.initializeCount)
	}
	if no.initializeCount != 0 {
		t.Errorf("expected non-Available gear to NOT be Initialized, got %d calls", no.initializeCount)
	}
}

func TestStartAllSkipsNonAvailableGears(t *testing.T) {
	yes := &mockProbeGear{info: Info{Name: "yes"}, probeResult: ProbeAvailable("ok", nil)}
	no := &mockProbeGear{info: Info{Name: "no"}, probeResult: ProbeInaccessible("permission")}
	withTestRegistry(t, yes, no)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())
	_ = m.InitializeAll(context.Background())
	if err := m.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if yes.startCount != 1 {
		t.Errorf("expected Available gear to be Started once, got %d calls", yes.startCount)
	}
	if no.startCount != 0 {
		t.Errorf("expected non-Available gear to NOT be Started, got %d calls", no.startCount)
	}
}

func TestRegisterRoutesSkipsNonAvailableGears(t *testing.T) {
	yes := &mockProbeGear{info: Info{Name: "yes"}, probeResult: ProbeAvailable("ok", nil)}
	no := &mockProbeGear{info: Info{Name: "no"}, probeResult: ProbeDisabled("config")}
	withTestRegistry(t, yes, no)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())
	m.RegisterRoutes(chi.NewRouter())

	if yes.registerRoutesHits != 1 {
		t.Errorf("expected Available gear to have RegisterRoutes called once, got %d", yes.registerRoutesHits)
	}
	if no.registerRoutesHits != 0 {
		t.Errorf("expected non-Available gear to NOT have RegisterRoutes called, got %d", no.registerRoutesHits)
	}
}

func TestProbeTableHasHeaderAndAlignedRows(t *testing.T) {
	a := &mockProbeGear{info: Info{Name: "alpha"}, probeResult: ProbeAvailable("ok", nil)}
	b := &mockProbeGear{info: Info{Name: "bravo"}, probeResult: ProbeNotInstalled("missing thing")}
	c := &mockProbeGear{info: Info{Name: "very-long-name"}, probeResult: ProbeAvailable("ok", nil)}
	withTestRegistry(t, a, b, c)

	m, buf := newTestManager(t)
	m.ProbeAll(context.Background())

	out := buf.String()

	for _, want := range []string{
		"Gear probe summary:",
		"GEAR",
		"STATUS",
		"REASON",
		"alpha",
		"bravo",
		"very-long-name",
		"enabled",
		"disabled",
		"missing thing",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("probe table missing %q\nfull output:\n%s", want, out)
		}
	}

	// Reasons are shown only for disabled rows. The enabled gears here
	// have reason "ok" but the table column for them should be blank to
	// keep the line uncluttered.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "alpha") && strings.Contains(line, "ok") {
			t.Errorf("enabled gear should not show its reason in the table, got line: %q", line)
		}
	}
}

func TestCapabilitiesEndpointReportsEveryGearWithVerdict(t *testing.T) {
	// Dashboard relies on every probed gear appearing in the response so it
	// can decide whether to surface or hide each gear's settings card.
	// Available gears must be flagged available; non-available gears must
	// carry their reason so the operator can act on it (issue #71 item 2).
	a := &mockProbeGear{
		info:        Info{Name: "alpha"},
		probeResult: ProbeAvailable("ok", map[string]string{"version": "1.0"}),
	}
	b := &mockProbeGear{
		info:        Info{Name: "bravo"},
		probeResult: ProbeNotInstalled("not on PATH"),
	}
	withTestRegistry(t, a, b)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())

	r := chi.NewRouter()
	m.RegisterSystemRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type = %q, want application/json", got)
	}

	var resp CapabilitiesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	alpha, ok := resp.Gears["alpha"]
	if !ok {
		t.Fatalf("alpha missing from response")
	}
	if alpha.Status != ProbeStatusAvailable {
		t.Errorf("alpha status = %v, want available", alpha.Status)
	}
	if alpha.Capabilities["version"] != "1.0" {
		t.Errorf("alpha capabilities = %v, want version=1.0", alpha.Capabilities)
	}

	bravo, ok := resp.Gears["bravo"]
	if !ok {
		t.Fatalf("bravo missing from response")
	}
	if bravo.Status != ProbeStatusNotInstalled {
		t.Errorf("bravo status = %v, want not_installed", bravo.Status)
	}
	if bravo.Reason == "" {
		t.Errorf("bravo reason is empty; operator-facing message lost")
	}
}

// TestCapabilitiesEndpointSurfacesResources guards the Resources field
// added in issue #112 Phase 2: gears that publish structured resources
// (log sources, services, metric sources, …) must round-trip them
// through the /api/v1/system/capabilities JSON envelope so the
// dashboard's capability-driven UI can read them directly instead of
// reverse-engineering them from gear-availability flags.
func TestCapabilitiesEndpointSurfacesResources(t *testing.T) {
	g := &mockProbeGear{
		info: Info{Name: "alpha"},
		probeResult: ProbeAvailableWithResources(
			"ok",
			map[string]string{"version": "1.0"},
			map[string]any{
				"log_sources": []map[string]string{
					{"name": "nginx", "display_name": "nginx", "path": "/var/log/nginx/access.log"},
				},
			},
		),
	}
	withTestRegistry(t, g)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())

	r := chi.NewRouter()
	m.RegisterSystemRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/capabilities", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	// Two-stage decode: first peel the outer envelope into
	// json.RawMessage per gear so we can verify the `resources` key
	// is present in the raw bytes (catches name-tag regressions like
	// renaming the field to "Resources" or "extra"). Then decode
	// the resources object into a typed shape and check the actual
	// payload contents. Decoding through RawMessage rather than
	// straight into map[string]any avoids relying on Go's `any`
	// decoding quirks for the wire-format guard.
	var envelope struct {
		Gears map[string]struct {
			Status    string          `json:"status"`
			Resources json.RawMessage `json:"resources"`
		} `json:"gears"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	alpha, ok := envelope.Gears["alpha"]
	if !ok {
		t.Fatalf("alpha missing from response")
	}
	if len(alpha.Resources) == 0 {
		t.Fatalf("alpha.resources missing from envelope; got body %s", rr.Body.String())
	}

	var resources struct {
		LogSources []struct {
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
			Path        string `json:"path"`
		} `json:"log_sources"`
	}
	if err := json.Unmarshal(alpha.Resources, &resources); err != nil {
		t.Fatalf("decode alpha.resources: %v", err)
	}
	if len(resources.LogSources) != 1 {
		t.Fatalf("log_sources = %v, want one entry", resources.LogSources)
	}
	got := resources.LogSources[0]
	if got.Name != "nginx" {
		t.Errorf("name = %q, want nginx", got.Name)
	}
	if got.DisplayName != "nginx" {
		t.Errorf("display_name = %q, want nginx", got.DisplayName)
	}
	if got.Path != "/var/log/nginx/access.log" {
		t.Errorf("path = %q, want /var/log/nginx/access.log", got.Path)
	}
}

func TestProbeResultsIsCopy(t *testing.T) {
	// Returning a copy prevents downstream callers (e.g. the capabilities
	// API handler) from accidentally mutating manager state under load.
	g := &mockProbeGear{info: Info{Name: "g"}, probeResult: ProbeAvailable("ok", map[string]string{"k": "v"})}
	withTestRegistry(t, g)

	m, _ := newTestManager(t)
	m.ProbeAll(context.Background())

	snap := m.ProbeResults()
	snap["g"] = ProbeResult{Status: ProbeStatusInaccessible, Reason: "mutated"}

	if m.ProbeResults()["g"].Status != ProbeStatusAvailable {
		t.Error("ProbeResults must return a copy; caller mutation leaked back into Manager state")
	}
}
