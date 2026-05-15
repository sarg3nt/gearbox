package apache

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

const sampleModStatusBody = `Total Accesses: 1234
Total kBytes: 5678
CPULoad: .5
Uptime: 1000
ReqPerSec: 1.234
BytesPerSec: 5678.0
BytesPerReq: 100.5
BusyWorkers: 5
IdleWorkers: 95
Scoreboard: ____W___K___
`

func TestParseModStatus(t *testing.T) {
	got, err := ParseModStatus(sampleModStatusBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got.CollectedAt = ""
	want := Stats{
		TotalAccesses: 1234,
		TotalKBytes:   5678,
		UptimeSeconds: 1000,
		ReqPerSec:     1.234,
		BytesPerSec:   5678.0,
		BytesPerReq:   100.5,
		BusyWorkers:   5,
		IdleWorkers:   95,
		CPULoad:       0.5,
	}
	if got != want {
		t.Errorf("Stats = %+v, want %+v", got, want)
	}
}

func TestParseModStatusRejectsEmptyOrHTML(t *testing.T) {
	for _, raw := range []string{
		"",
		"   \n  \n ",
		// HTML-shaped — no key:value pairs the parser recognises.
		"<html><body><h1>Apache Status</h1></body></html>",
	} {
		if _, err := ParseModStatus(raw); err == nil {
			t.Errorf("expected error for %q", raw)
		}
	}
}

func TestParseModStatusToleratesUnknownKeys(t *testing.T) {
	// Newer Apache builds add lines like ConnsTotal / Load1 — they
	// must not break the parser, just get ignored.
	body := "Total Accesses: 5\nTotal kBytes: 10\nLoad1: 0.7\nConnsTotal: 42\n"
	got, err := ParseModStatus(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalAccesses != 5 || got.TotalKBytes != 10 {
		t.Errorf("Stats = %+v, want Total Accesses=5 / kBytes=10", got)
	}
}

func gearWithHTTPStub(get func(ctx context.Context, url string) (probe.HTTPResult, error)) *Gear {
	g := New()
	g.httpGet = get
	g.statusURL = defaultStatusURL
	return g
}

func TestCollectorScrapeCachesAndReturnsStats(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: sampleModStatusBody}, nil
	})
	data, err := g.scrape(context.Background())
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	stats, ok := data.(Stats)
	if !ok || stats.TotalAccesses != 1234 {
		t.Errorf("scrape data = %+v, want Stats with TotalAccesses=1234", data)
	}
	cached, hit := g.readCachedStats()
	if !hit || cached.TotalAccesses != 1234 {
		t.Errorf("cache after scrape = %+v hit=%v", cached, hit)
	}
}

func TestCollectorScrapeReturnsErrorOnNon200(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusForbidden}, nil
	})
	if _, err := g.scrape(context.Background()); err == nil {
		t.Error("expected scrape to error on 403")
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apache/stats", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 before first scrape", rr.Code)
	}
}

func TestHandleStatsReturnsCachedJSON(t *testing.T) {
	g := gearWithHTTPStub(func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: sampleModStatusBody}, nil
	})
	if _, err := g.scrape(context.Background()); err != nil {
		t.Fatalf("scrape: %v", err)
	}
	r := chi.NewRouter()
	g.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apache/stats", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"total_accesses":1234`) {
		t.Errorf("body missing total_accesses=1234, got %s", rr.Body.String())
	}
}

func TestPickStatusURLHonorsOverride(t *testing.T) {
	if got := pickStatusURL(gear.Dependencies{ApacheStatusURL: "http://1.2.3.4/_status"}); got != "http://1.2.3.4/_status" {
		t.Errorf("override = %q, want override URL", got)
	}
	if got := pickStatusURL(gear.Dependencies{}); got != defaultStatusURL {
		t.Errorf("default = %q, want %q", got, defaultStatusURL)
	}
}
