package caddy

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/probe"
)

func newTestGear() *Gear {
	return &Gear{
		lookPath: func(string) (string, error) { return "/usr/bin/caddy", nil },
		runVersion: func(context.Context) ([]byte, error) {
			return []byte("v2.8.4 h1:q3pe0hpTPqkaN53lp5x3lndR0SBgwh3w29bBeoP5J7s=\n"), nil
		},
		httpGet: func(context.Context, string) (probe.HTTPResult, error) {
			return probe.HTTPResult{
				StatusCode: http.StatusOK,
				Body:       "# HELP caddy_http_requests_total Counter of HTTP requests\n# TYPE caddy_http_requests_total counter\ncaddy_http_requests_total 1.0\n",
			}, nil
		},
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

func TestProbeAvailableHappyPath(t *testing.T) {
	g := newTestGear()
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
	if res.Capabilities["version"] != "2.8.4" {
		t.Errorf("version = %q, want 2.8.4", res.Capabilities["version"])
	}
	if res.Capabilities["admin_url"] != defaultAdminURL {
		t.Errorf("admin_url = %q, want default", res.Capabilities["admin_url"])
	}
	if res.Capabilities["status_source"] != "prometheus" {
		t.Errorf("status_source = %q, want prometheus", res.Capabilities["status_source"])
	}
}

func TestProbeAdminURLOverrideShortCircuits(t *testing.T) {
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		t.Fatal("httpGet should not be called when CADDY_ADMIN_URL is set")
		return probe.HTTPResult{}, nil
	}
	deps := gear.Dependencies{CaddyAdminURL: "http://localhost:7777/metrics"}
	res := g.Probe(context.Background(), deps)
	if res.Status != gear.ProbeStatusAvailable {
		t.Errorf("status = %v, want Available (override)", res.Status)
	}
	if res.Capabilities["admin_url"] != "http://localhost:7777/metrics" {
		t.Errorf("admin_url = %q, want override", res.Capabilities["admin_url"])
	}
	if res.Capabilities["override_source"] != "env" {
		t.Errorf("override_source = %q, want env", res.Capabilities["override_source"])
	}
}

func TestProbeInaccessibleOnConnectionError(t *testing.T) {
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{}, errors.New("connection refused")
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Errorf("status = %v, want Inaccessible", res.Status)
	}
	if !strings.Contains(res.Reason, "admin endpoint") {
		t.Errorf("reason should name the admin endpoint, got %q", res.Reason)
	}
}

func TestProbeInaccessibleOn404(t *testing.T) {
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusNotFound}, nil
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Fatalf("status = %v, want Inaccessible", res.Status)
	}
	if !strings.Contains(res.Reason, "admin") {
		t.Errorf("reason should reference the admin endpoint, got %q", res.Reason)
	}
}

func TestProbeInaccessibleOn200WithoutSentinel(t *testing.T) {
	// Some other endpoint is mounted at :2019/metrics and answers 200.
	// Catch this so we don't claim Caddy is available when it isn't.
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: "some other metrics page"}, nil
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Errorf("status = %v, want Inaccessible", res.Status)
	}
}

func TestMetricCategoriesDeclaresHTTPRequests(t *testing.T) {
	g := New()
	cats := g.MetricCategories()
	if len(cats) != 1 || cats[0] != gear.CategoryHTTPRequests {
		t.Errorf("MetricCategories() = %v, want [CategoryHTTPRequests]", cats)
	}
}
