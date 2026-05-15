package traefik

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/probe"
)

// metricsBody is a minimal Prometheus scrape output that includes the
// sentinel metric — used by the happy-path test cases.
const metricsBody = "# HELP traefik_router_requests_total Total number of requests.\n# TYPE traefik_router_requests_total counter\ntraefik_router_requests_total{router=\"router-1\"} 42\n"

// recordingHTTPGet returns an httpGet stub plus a slice that records
// every URL it was called with, ordered. Lets tests assert that the
// detector tried the URLs in the expected order without parsing logs.
func recordingHTTPGet(responses map[string]probe.HTTPResult) (func(context.Context, string) (probe.HTTPResult, error), *[]string) {
	calls := &[]string{}
	return func(_ context.Context, url string) (probe.HTTPResult, error) {
		*calls = append(*calls, url)
		if r, ok := responses[url]; ok {
			return r, nil
		}
		return probe.HTTPResult{}, errors.New("connection refused")
	}, calls
}

func newTestGear() *Gear {
	g, _ := recordingHTTPGet(map[string]probe.HTTPResult{
		"http://127.0.0.1:8082/metrics": {StatusCode: http.StatusOK, Body: metricsBody},
	})
	return &Gear{
		lookPath: func(string) (string, error) { return "/usr/local/bin/traefik", nil },
		runVersion: func(context.Context) ([]byte, error) {
			return []byte("Version:      v3.1.2\nCodename:     beaufort\nGo version:   go1.22.0\n"), nil
		},
		httpGet: g,
	}
}

func TestProbeReturnsNotInstalledWhenBinaryMissing(t *testing.T) {
	g := newTestGear()
	g.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusNotInstalled {
		t.Errorf("status = %v, want NotInstalled", res.Status)
	}
}

func TestProbeAvailableOnFirstMetricsURL(t *testing.T) {
	g := newTestGear()
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
	if res.Capabilities["metrics_url"] != "http://127.0.0.1:8082/metrics" {
		t.Errorf("metrics_url = %q, want :8082 (first in fallback order)", res.Capabilities["metrics_url"])
	}
	if res.Capabilities["version"] != "3.1.2" {
		t.Errorf("version = %q, want 3.1.2", res.Capabilities["version"])
	}
}

func TestProbeFallsBackToSecondMetricsURL(t *testing.T) {
	// :8082 connection refused, :8080/metrics answers. Common when
	// the operator wires Prometheus onto the API entrypoint.
	httpGet, calls := recordingHTTPGet(map[string]probe.HTTPResult{
		"http://127.0.0.1:8080/metrics": {StatusCode: http.StatusOK, Body: metricsBody},
	})
	g := newTestGear()
	g.httpGet = httpGet

	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
	if res.Capabilities["metrics_url"] != "http://127.0.0.1:8080/metrics" {
		t.Errorf("metrics_url = %q, want :8080 (fallback)", res.Capabilities["metrics_url"])
	}
	// Must have tried both URLs in order — dashboard API probe will
	// also appear in the call list, so check the metrics ones came
	// in the right relative order.
	idx8082, idx8080 := -1, -1
	for i, u := range *calls {
		switch u {
		case "http://127.0.0.1:8082/metrics":
			idx8082 = i
		case "http://127.0.0.1:8080/metrics":
			idx8080 = i
		}
	}
	if idx8082 < 0 || idx8080 < 0 || idx8082 >= idx8080 {
		t.Errorf("expected :8082 to be probed before :8080; calls = %v", *calls)
	}
}

func TestProbeInaccessibleWhenAllDefaultsFail(t *testing.T) {
	// All default URLs return connection-refused. Must be Inaccessible,
	// not NotInstalled, since the binary IS there — the fix is config
	// (enable [metrics.prometheus]), not installation.
	httpGet, _ := recordingHTTPGet(map[string]probe.HTTPResult{})
	g := newTestGear()
	g.httpGet = httpGet

	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Fatalf("status = %v, want Inaccessible", res.Status)
	}
	for _, want := range []string{":8082", ":8080", "TRAEFIK_METRICS_URL"} {
		if !strings.Contains(res.Reason, want) {
			t.Errorf("reason should mention %q, got %q", want, res.Reason)
		}
	}
}

func TestProbeMetricsURLOverrideShortCircuits(t *testing.T) {
	g := newTestGear()
	g.httpGet = func(_ context.Context, url string) (probe.HTTPResult, error) {
		if url == "http://traefik.internal:9000/_metrics" {
			t.Fatal("httpGet should not be called against the override URL")
		}
		// Dashboard API still probed for capabilities — return refused
		// so the test stays focused on the override branch.
		return probe.HTTPResult{}, errors.New("refused")
	}
	deps := gear.Dependencies{TraefikMetricsURL: "http://traefik.internal:9000/_metrics"}
	res := g.Probe(context.Background(), deps)
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available (override)", res.Status)
	}
	if res.Capabilities["metrics_url"] != "http://traefik.internal:9000/_metrics" {
		t.Errorf("metrics_url = %q, want override", res.Capabilities["metrics_url"])
	}
	if res.Capabilities["override_source"] != "env" {
		t.Errorf("override_source = %q, want env", res.Capabilities["override_source"])
	}
}

func TestProbeRecordsDashboardAPIWhenReachable(t *testing.T) {
	g := newTestGear()
	g.httpGet = func(_ context.Context, url string) (probe.HTTPResult, error) {
		switch url {
		case "http://127.0.0.1:8082/metrics":
			return probe.HTTPResult{StatusCode: http.StatusOK, Body: metricsBody}, nil
		case "http://127.0.0.1:8080/api/rawdata":
			return probe.HTTPResult{StatusCode: http.StatusOK, Body: `{"routers":{}}`}, nil
		}
		return probe.HTTPResult{}, errors.New("refused")
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Capabilities["dashboard_api"] != "http://127.0.0.1:8080/api/rawdata" {
		t.Errorf("dashboard_api = %q, want default URL", res.Capabilities["dashboard_api"])
	}
}

func TestProbeOmitsDashboardAPIWhenUnreachable(t *testing.T) {
	// Dashboard disabled is the common security-conscious config.
	// Must be absent from caps (key omitted), not present-with-empty.
	g := newTestGear()
	res := g.Probe(context.Background(), gear.Dependencies{})
	if _, ok := res.Capabilities["dashboard_api"]; ok {
		t.Error("dashboard_api should be omitted when unreachable, not set to empty string")
	}
}

func TestMetricCategoriesDeclaresHTTPRequests(t *testing.T) {
	g := New()
	cats := g.MetricCategories()
	if len(cats) != 1 || cats[0] != gear.CategoryHTTPRequests {
		t.Errorf("MetricCategories() = %v, want [CategoryHTTPRequests]", cats)
	}
}
