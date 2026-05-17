package nginx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/probe"
)

const sampleStubStatusBody = `Active connections: 291
server accepts handled requests
 16630948 16630948 31070465
Reading: 6 Writing: 179 Waiting: 106
`

func TestParseStubStatus(t *testing.T) {
	got, err := ParseStubStatus(sampleStubStatusBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Stats{
		Active:   291,
		Reading:  6,
		Writing:  179,
		Waiting:  106,
		Accepts:  16630948,
		Handled:  16630948,
		Requests: 31070465,
	}
	// CollectedAt is non-deterministic; drop before comparing.
	got.CollectedAt = ""
	if got != want {
		t.Errorf("Stats = %+v, want %+v", got, want)
	}
}

func TestParseStubStatusRejectsNonStubBody(t *testing.T) {
	cases := []string{
		"",
		"random garbage",
		"Active connections: nope",
		// Missing the 'accepts handled requests' counter row.
		"Active connections: 5\nReading: 1 Writing: 2 Waiting: 3\n",
	}
	for _, raw := range cases {
		if _, err := ParseStubStatus(raw); err == nil {
			t.Errorf("expected parse error for %q", raw)
		}
	}
}

// gearWithHTTPStub returns a nginx gear set up to mock the HTTP
// probe so collector tests don't need a live nginx.
func gearWithHTTPStub(get func(ctx context.Context, url string) (probe.HTTPResult, error)) *Gear {
	g := New()
	g.httpGet = get
	g.statusURL = defaultStatusURL
	return g
}

func TestCollectorScrapeCachesAndReturnsStats(t *testing.T) {
	calls := 0
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		calls++
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: sampleStubStatusBody}, nil
	})

	data, err := g.scrape(context.Background())
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	if calls != 1 {
		t.Errorf("scrape should call httpGet once, got %d", calls)
	}
	stats, ok := data.(Stats)
	if !ok || stats.Active != 291 {
		t.Errorf("scrape returned %+v, want Stats{Active: 291, …}", data)
	}
	// Cache must hold the same stats so the synchronous handler can
	// return without a fresh scrape.
	cached, hit := g.readCachedStats()
	if !hit || cached.Active != 291 {
		t.Errorf("cache after scrape = %+v hit=%v, want Active=291", cached, hit)
	}
}

func TestCollectorScrapeReturnsErrorOnNon200(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusForbidden}, nil
	})
	if _, err := g.scrape(context.Background()); err == nil {
		t.Error("expected scrape to error on 403 response")
	}
}

func TestCollectorScrapeReturnsErrorOnTransportFailure(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{}, errors.New("connection refused")
	})
	if _, err := g.scrape(context.Background()); err == nil {
		t.Error("expected scrape to surface transport errors")
	}
}

func TestHandleStatsReturns503BeforeFirstScrape(t *testing.T) {
	// The dashboard distinguishes "no data yet" from "zero active
	// connections" by HTTP status code; the handler must return 503
	// not an empty 200 when nothing has been cached yet.
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{}, errors.New("unused")
	})
	r := chi.NewRouter()
	g.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nginx/stats", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

func TestHandleStatsReturnsCachedJSON(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: sampleStubStatusBody}, nil
	})
	// Prime the cache.
	if _, err := g.scrape(context.Background()); err != nil {
		t.Fatalf("scrape: %v", err)
	}

	r := chi.NewRouter()
	g.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nginx/stats", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type = %q", got)
	}
	if !strings.Contains(rr.Body.String(), `"active":291`) {
		t.Errorf("body should contain JSON-encoded Active=291, got %s", rr.Body.String())
	}
}

func TestHandleStatsForceTriggersFreshScrape(t *testing.T) {
	calls := 0
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		calls++
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: sampleStubStatusBody}, nil
	})

	r := chi.NewRouter()
	g.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nginx/stats?force=true", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if calls != 1 {
		t.Errorf("force=true should call httpGet once, got %d", calls)
	}
}

func TestPickStatusURLHonorsOverride(t *testing.T) {
	if got := pickStatusURL(gear.Dependencies{NginxStatusURL: "http://1.2.3.4/_status"}); got != "http://1.2.3.4/_status" {
		t.Errorf("pickStatusURL with override = %q, want override", got)
	}
	if got := pickStatusURL(gear.Dependencies{}); got != defaultStatusURL {
		t.Errorf("pickStatusURL without override = %q, want default", got)
	}
}
