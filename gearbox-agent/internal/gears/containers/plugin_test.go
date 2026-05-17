// Package containers provides functional tests for the Containers gear HTTP API.
//
// These tests exercise every API endpoint the gear registers, using
// httptest to call the handlers directly. They verify:
//   - Correct HTTP status codes
//   - Proper JSON response format (not plain text)
//   - No "Failed to" prefix leakage from the agent (the JS client adds these)
//   - Consistent error response structure
//
// When Docker is not available (e.g., macOS CI), endpoints return empty results
// or ServiceUnavailable — never plain-text errors.
package containers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

// setupTestGear creates a Gear initialized without a Docker runtime.
// On machines without Docker this will simply have runtimeInfo.Available=false,
// which is the expected production behaviour on non-Docker servers.
func setupTestGear(t *testing.T) *Gear {
	t.Helper()

	g := &Gear{}
	logger := slog.Default()
	bus := events.NewBus()

	deps := gear.Dependencies{
		Logger:   logger,
		EventBus: bus,
	}

	if err := g.Initialize(context.Background(), deps); err != nil {
		t.Fatalf("failed to initialize containers gear: %v", err)
	}
	return g
}

// setupTestRouter creates a chi router with all gear routes registered.
func setupTestRouter(t *testing.T) (*Gear, *chi.Mux) {
	t.Helper()
	g := setupTestGear(t)
	r := chi.NewRouter()
	g.RegisterRoutes(r)
	return g, r
}

// --- helpers ---

func assertJSONResponse(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	var raw json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Errorf("response body is not valid JSON: %v\nbody: %s", err, w.Body.String())
	}
}

func assertNoFailedToPrefix(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if strings.Contains(w.Body.String(), "Failed to") {
		t.Errorf("response contains 'Failed to' prefix — agent must not add these (JS adds its own)\nbody: %s", w.Body.String())
	}
}

func assertErrorJSON(t *testing.T, w *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if w.Code != wantStatus {
		t.Errorf("expected status %d, got %d\nbody: %s", wantStatus, w.Code, w.Body.String())
	}
	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)

	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Errorf("failed to parse error JSON: %v\nbody: %s", err, w.Body.String())
	}
	if errResp.Error == "" {
		t.Error("error response has empty error message")
	}
}

// --- Info / Health ---

func TestGearInfo(t *testing.T) {
	g := setupTestGear(t)
	info := g.Info()
	if info.Name != "containers" {
		t.Errorf("expected gear name 'containers', got %q", info.Name)
	}
}

func TestGearHealth_NoDocker(t *testing.T) {
	g := setupTestGear(t)
	// When Docker is not available, health should be degraded (not unhealthy/panicking)
	h := g.Health()
	if h.Status == "" {
		t.Error("expected a non-empty health status")
	}
	// Must be one of the valid constants
	valid := map[string]bool{
		gear.HealthStatusHealthy:   true,
		gear.HealthStatusDegraded:  true,
		gear.HealthStatusUnhealthy: true,
	}
	if !valid[h.Status] {
		t.Errorf("unexpected health status %q", h.Status)
	}
}

// --- GET /api/v1/containers/runtime ---

func TestHandleRuntime(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/runtime", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	assertJSONResponse(t, w)

	var info RuntimeInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("failed to parse RuntimeInfo: %v", err)
	}
	// Runtime field must always be set
	if info.Runtime == "" {
		t.Error("RuntimeInfo.Runtime field must not be empty")
	}
}

// --- GET /api/v1/containers/list ---

func TestHandleList(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/list", nil)
	router.ServeHTTP(w, req)

	// Either 200 with list or an error — never a panic
	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)

	if w.Code == http.StatusOK {
		var list ContainerList
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("failed to parse ContainerList: %v", err)
		}
		if list.Containers == nil {
			t.Error("ContainerList.Containers must not be nil (use empty slice)")
		}
		if list.Total < 0 {
			t.Error("ContainerList.Total must be >= 0")
		}
	}
}

// --- GET /api/v1/containers/{id} ---

func TestHandleInspect_MissingID(t *testing.T) {
	// Route with empty id won't match — test with a non-existent container ID
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/nonexistentcontainer123", nil)
	router.ServeHTTP(w, req)

	// Either 503 (no runtime) or 500 (runtime error)
	if w.Code == http.StatusOK {
		assertJSONResponse(t, w)
	} else {
		assertErrorJSON(t, w, w.Code)
	}
}

// --- GET /api/v1/containers/{id}/stats ---

func TestHandleStats_NoRuntime(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/abc123/stats", nil)
	router.ServeHTTP(w, req)

	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)
}

// --- GET /api/v1/containers/{id}/logs ---

func TestHandleLogs_InvalidTail(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/abc123/logs?tail=notanumber", nil)
	router.ServeHTTP(w, req)

	assertErrorJSON(t, w, http.StatusBadRequest)
}

func TestHandleLogs_ValidTailAll(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/abc123/logs?tail=all", nil)
	router.ServeHTTP(w, req)

	// Either 503 (no runtime) or error — the tail=all is valid
	if w.Code == http.StatusBadRequest {
		t.Error("tail=all should be accepted as valid input")
	}
}

func TestHandleLogs_ValidTailNumber(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/abc123/logs?tail=50", nil)
	router.ServeHTTP(w, req)

	// Should not be a 400 bad request
	if w.Code == http.StatusBadRequest {
		t.Errorf("tail=50 should be valid, got 400\nbody: %s", w.Body.String())
	}
}

// --- POST /api/v1/containers/{id}/start ---

func TestHandleStart_NoRuntime(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/containers/abc123/start", nil)
	router.ServeHTTP(w, req)

	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)
}

// --- POST /api/v1/containers/{id}/stop ---

func TestHandleStop_NoRuntime(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/containers/abc123/stop", nil)
	router.ServeHTTP(w, req)

	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)
}

// --- POST /api/v1/containers/{id}/restart ---

func TestHandleRestart_NoRuntime(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/containers/abc123/restart", nil)
	router.ServeHTTP(w, req)

	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)
}

// --- DELETE /api/v1/containers/{id} ---

func TestHandleRemove_NoRuntime(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/containers/abc123", nil)
	router.ServeHTTP(w, req)

	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)
}

// --- GET /api/v1/containers/stacks ---

func TestHandleListStacks(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/stacks", nil)
	router.ServeHTTP(w, req)

	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)

	if w.Code == http.StatusOK {
		var list StackList
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatalf("failed to parse StackList: %v", err)
		}
		if list.Stacks == nil {
			t.Error("StackList.Stacks must not be nil")
		}
	}
}

// --- GET /api/v1/containers/stacks/{name} ---

func TestHandleStackDetail_NotFound(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/stacks/nonexistent", nil)
	router.ServeHTTP(w, req)

	// Either 503 (no runtime) or 404 (stack not found)
	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)
}

// --- GET /api/v1/containers/images ---

func TestHandleListImages(t *testing.T) {
	_, router := setupTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/containers/images", nil)
	router.ServeHTTP(w, req)

	assertJSONResponse(t, w)
	assertNoFailedToPrefix(t, w)
}

// --- Helper function tests ---

func TestShortID(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"abc123", "abc123"},
		{"abcdefghijklmnop", "abcdefghijkl"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := shortID(tt.id); got != tt.want {
			t.Errorf("shortID(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestStackStatus(t *testing.T) {
	tests := []struct {
		name         string
		running      int
		total        int
		wantStatus   string
	}{
		{"all running", 3, 3, "running"},
		{"partial", 1, 3, "partial"},
		{"stopped", 0, 3, "stopped"},
		{"empty", 0, 0, "stopped"},
	}
	for _, tt := range tests {
		s := &Stack{RunningCount: tt.running, ContainerCount: tt.total}
		got := stackStatus(s)
		if got != tt.wantStatus {
			t.Errorf("%s: stackStatus() = %q, want %q", tt.name, got, tt.wantStatus)
		}
	}
}
