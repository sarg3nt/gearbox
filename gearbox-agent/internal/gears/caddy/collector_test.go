package caddy

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

const samplePrometheusBody = `# HELP caddy_http_requests_total Counter of HTTP requests
# TYPE caddy_http_requests_total counter
caddy_http_requests_total{server="srv0",handler="reverse_proxy"} 42
caddy_http_requests_total{server="srv0",handler="file_server"} 17
# HELP caddy_http_request_errors_total Error counter
# TYPE caddy_http_request_errors_total counter
caddy_http_request_errors_total{server="srv0"} 3
# admin endpoint counter — its mere existence signals admin is up
caddy_admin_http_requests_total{path="/load"} 5
`

func TestParsePrometheusOutputSumsCounters(t *testing.T) {
	got := ParsePrometheusOutput(samplePrometheusBody)
	got.CollectedAt = ""
	want := Stats{
		RequestsTotal:      59, // 42 + 17
		RequestErrorsTotal: 3,
		AdminRunning:       true,
	}
	if got != want {
		t.Errorf("Stats = %+v, want %+v", got, want)
	}
}

func TestParsePrometheusOutputHandlesMissingAdminMetric(t *testing.T) {
	// Admin endpoint disabled (admin off in Caddyfile) — the
	// `caddy_admin_*` metric won't appear; AdminRunning must be
	// false even though the rest of the scrape is fine.
	body := `caddy_http_requests_total{server="srv0"} 7
`
	got := ParsePrometheusOutput(body)
	if got.AdminRunning {
		t.Error("AdminRunning should be false when admin metric is absent")
	}
	if got.RequestsTotal != 7 {
		t.Errorf("RequestsTotal = %d, want 7", got.RequestsTotal)
	}
}

func gearWithHTTPStub(get func(ctx context.Context, url string) (probe.HTTPResult, error)) *Gear {
	g := New()
	g.httpGet = get
	g.adminURL = defaultAdminURL
	return g
}

func TestCollectorScrapeCachesAndReturnsStats(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: samplePrometheusBody}, nil
	})
	data, err := g.scrape(context.Background())
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	stats, ok := data.(Stats)
	if !ok || stats.RequestsTotal != 59 {
		t.Errorf("scrape data = %+v, want RequestsTotal=59", data)
	}
	cached, hit := g.readCachedStats()
	if !hit || cached.RequestsTotal != 59 {
		t.Errorf("cache after scrape = %+v hit=%v", cached, hit)
	}
}

func TestCollectorScrapeReturnsErrorOnNon200(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusNotFound}, nil
	})
	if _, err := g.scrape(context.Background()); err == nil {
		t.Error("expected scrape to error on 404")
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
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{}, errors.New("unused")
	})
	r := chi.NewRouter()
	g.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/caddy/stats", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

func TestHandleStatsReturnsCachedJSON(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: samplePrometheusBody}, nil
	})
	if _, err := g.scrape(context.Background()); err != nil {
		t.Fatalf("scrape: %v", err)
	}
	r := chi.NewRouter()
	g.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/caddy/stats", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"requests_total":59`) {
		t.Errorf("body missing requests_total=59, got %s", rr.Body.String())
	}
}

func TestPickAdminURLHonorsOverride(t *testing.T) {
	if got := pickAdminURL(gear.Dependencies{CaddyAdminURL: "http://1.2.3.4/_metrics"}); got != "http://1.2.3.4/_metrics" {
		t.Errorf("override = %q", got)
	}
	if got := pickAdminURL(gear.Dependencies{}); got != defaultAdminURL {
		t.Errorf("default = %q", got)
	}
}
