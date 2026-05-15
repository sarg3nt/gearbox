package traefik

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/probe"
)

const sampleTraefikBody = `# HELP traefik_router_requests_total Total
# TYPE traefik_router_requests_total counter
traefik_router_requests_total{code="200",method="GET",router="r1"} 100
traefik_router_requests_total{code="201",method="POST",router="r1"} 4
traefik_router_requests_total{code="404",method="GET",router="r1"} 7
traefik_router_requests_total{code="500",method="POST",router="r2"} 3
traefik_router_requests_total{code="503",method="GET",router="r2"} 2
traefik_entrypoint_requests_total{entrypoint="web",code="200"} 90
traefik_entrypoint_requests_total{entrypoint="websecure",code="200"} 20
`

func TestParsePrometheusOutputBucketsByStatusClass(t *testing.T) {
	got := ParsePrometheusOutput(sampleTraefikBody)
	got.CollectedAt = ""
	want := Stats{
		RequestsTotal: 116, // 100 + 4 + 7 + 3 + 2
		Response2xx:   104, // 100 + 4
		Response4xx:   7,
		Response5xx:   5, // 3 + 2
		EntryPoints:   []string{"web", "websecure"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Stats = %+v, want %+v", got, want)
	}
}

func TestParsePrometheusOutputIgnoresCounterWithoutCodeLabel(t *testing.T) {
	// A traefik_router_requests_total without `code` shouldn't
	// land in a status-class bucket (would silently inflate the
	// wrong class). It still counts toward RequestsTotal because
	// SumByName sums everything by name.
	body := `traefik_router_requests_total{method="GET"} 99
`
	got := ParsePrometheusOutput(body)
	if got.RequestsTotal != 99 {
		t.Errorf("RequestsTotal = %d, want 99", got.RequestsTotal)
	}
	if got.Response1xx+got.Response2xx+got.Response3xx+got.Response4xx+got.Response5xx != 0 {
		t.Errorf("status-class buckets should stay zero without code label; got %+v", got)
	}
}

func gearWithHTTPStub(get func(ctx context.Context, url string) (probe.HTTPResult, error)) *Gear {
	g := New()
	g.httpGet = get
	g.metricsURL = defaultMetricsURLs[0]
	return g
}

func TestCollectorScrapeCachesAndReturnsStats(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: sampleTraefikBody}, nil
	})
	data, err := g.scrape(context.Background())
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	stats, ok := data.(Stats)
	if !ok || stats.Response5xx != 5 {
		t.Errorf("scrape data = %+v, want Response5xx=5", data)
	}
}

func TestCollectorScrapeRejectsBodyWithoutSentinel(t *testing.T) {
	// A non-Traefik service answering on the configured URL would
	// return Prometheus output without the traefik_ sentinel. The
	// collector should error rather than caching a zero-valued
	// Stats that misleads the dashboard.
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{
			StatusCode: http.StatusOK,
			Body:       "other_service_metric{a=\"b\"} 1\n",
		}, nil
	})
	if _, err := g.scrape(context.Background()); err == nil {
		t.Error("expected scrape to error when sentinel is absent")
	}
}

func TestCollectorScrapeReturnsErrorOnNon200(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusServiceUnavailable}, nil
	})
	if _, err := g.scrape(context.Background()); err == nil {
		t.Error("expected scrape to error on 503")
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traefik/stats", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

func TestHandleStatsReturnsCachedJSON(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: sampleTraefikBody}, nil
	})
	if _, err := g.scrape(context.Background()); err != nil {
		t.Fatalf("scrape: %v", err)
	}
	r := chi.NewRouter()
	g.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traefik/stats", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"response_5xx":5`) {
		t.Errorf("body missing response_5xx=5, got %s", rr.Body.String())
	}
}

func TestPickMetricsURLHonorsOverride(t *testing.T) {
	if got := pickMetricsURL(gear.Dependencies{TraefikMetricsURL: "http://1.2.3.4/_metrics"}); got != "http://1.2.3.4/_metrics" {
		t.Errorf("override = %q", got)
	}
	if got := pickMetricsURL(gear.Dependencies{}); got != defaultMetricsURLs[0] {
		t.Errorf("default = %q, want %q", got, defaultMetricsURLs[0])
	}
}
