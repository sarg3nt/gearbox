package nginx

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/probe"
)

// fakeFileInfo is a minimal os.FileInfo for the stat indirection.
type fakeFileInfo struct{ name string }

func (f fakeFileInfo) Name() string         { return f.name }
func (fakeFileInfo) Size() int64            { return 0 }
func (fakeFileInfo) Mode() os.FileMode      { return 0 }
func (fakeFileInfo) ModTime() (t time.Time) { return }
func (fakeFileInfo) IsDir() bool            { return false }
func (fakeFileInfo) Sys() any               { return nil }

// statExisting reports the listed paths as existing; everything else
// returns os.ErrNotExist. Lets each test pin which well-known config
// path the resolver should "find".
func statExisting(paths ...string) func(string) (os.FileInfo, error) {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[p] = struct{}{}
	}
	return func(p string) (os.FileInfo, error) {
		if _, ok := set[p]; ok {
			return fakeFileInfo{name: p}, nil
		}
		return nil, os.ErrNotExist
	}
}

// newTestGear returns a gear with stubbed-out OS dependencies. Each
// test overrides the fields it cares about; the defaults represent
// "nginx 1.27.0 installed, vanilla config, stub_status reachable" so
// the happy-path tests stay short.
func newTestGear() *Gear {
	return &Gear{
		lookPath: func(string) (string, error) { return "/usr/sbin/nginx", nil },
		runShortV: func(context.Context) ([]byte, error) {
			return []byte("nginx version: nginx/1.27.0 (Ubuntu)\n"), nil
		},
		runLongV: func(context.Context) ([]byte, error) {
			return []byte("nginx version: nginx/1.27.0\nconfigure arguments: --conf-path=/etc/nginx/nginx.conf --with-http_api_module"), nil
		},
		stat: statExisting("/etc/nginx/nginx.conf"),
		httpGet: func(context.Context, string) (probe.HTTPResult, error) {
			return probe.HTTPResult{
				StatusCode: http.StatusOK,
				Body:       "Active connections: 1\nserver accepts handled requests\n 1 1 1\nReading: 0 Writing: 1 Waiting: 0\n",
			}, nil
		},
	}
}

func TestProbeReturnsNotInstalledWhenBinaryMissing(t *testing.T) {
	// Distinct status — operator action is "install nginx", not
	// "fix config".
	g := newTestGear()
	g.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusNotInstalled {
		t.Errorf("status = %v, want NotInstalled", res.Status)
	}
}

func TestProbeAvailableOnStubStatusHit(t *testing.T) {
	g := newTestGear()
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
	if res.Capabilities["version"] != "1.27.0" {
		t.Errorf("version = %q, want 1.27.0", res.Capabilities["version"])
	}
	if res.Capabilities["binary_path"] != "/usr/sbin/nginx" {
		t.Errorf("binary_path = %q, want /usr/sbin/nginx", res.Capabilities["binary_path"])
	}
	if res.Capabilities["status_url"] != defaultStatusURL {
		t.Errorf("status_url = %q, want default", res.Capabilities["status_url"])
	}
	if res.Capabilities["status_source"] != "stub_status" {
		t.Errorf("status_source = %q, want stub_status", res.Capabilities["status_source"])
	}
	if res.Capabilities["api_module"] != "true" {
		t.Errorf("api_module = %q, want true (Plus / 1.19+ build)", res.Capabilities["api_module"])
	}
	if res.Capabilities["config_path"] != "/etc/nginx/nginx.conf" {
		t.Errorf("config_path = %q, want /etc/nginx/nginx.conf", res.Capabilities["config_path"])
	}
}

func TestProbeStatusURLOverrideShortCircuits(t *testing.T) {
	// NGINX_STATUS_URL bypasses the synchronous HTTP probe — we
	// shouldn't even call httpGet when an override is set. Wire an
	// httpGet that fails the test if invoked.
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		t.Fatal("httpGet should not be called when NGINX_STATUS_URL is set")
		return probe.HTTPResult{}, nil
	}
	deps := gear.Dependencies{NginxStatusURL: "http://nginx.internal:8080/_status"}
	res := g.Probe(context.Background(), deps)
	if res.Status != gear.ProbeStatusAvailable {
		t.Errorf("status = %v, want Available (operator override)", res.Status)
	}
	if res.Capabilities["status_url"] != "http://nginx.internal:8080/_status" {
		t.Errorf("status_url = %q, want override value", res.Capabilities["status_url"])
	}
	if res.Capabilities["override_source"] != "env" {
		t.Errorf("override_source = %q, want env", res.Capabilities["override_source"])
	}
}

func TestProbeInaccessibleOn403(t *testing.T) {
	// 403 means stub_status is configured but rejects loopback —
	// operator forgot `allow 127.0.0.1`. Reason must point them at
	// the fix so they don't have to dig through docs.
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusForbidden}, nil
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Fatalf("status = %v, want Inaccessible", res.Status)
	}
	if !contains(res.Reason, "403") || !contains(res.Reason, "allow 127.0.0.1") {
		t.Errorf("reason should name 403 + the fix snippet, got %q", res.Reason)
	}
}

func TestProbeInaccessibleOn404(t *testing.T) {
	// 404 means stub_status isn't configured at all — different fix.
	// Reason must name the location-block snippet.
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusNotFound}, nil
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Fatalf("status = %v, want Inaccessible", res.Status)
	}
	if !contains(res.Reason, "stub_status") || !contains(res.Reason, "location") {
		t.Errorf("reason should name stub_status + location block fix, got %q", res.Reason)
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
}

func TestProbeInaccessibleOn200WithoutSentinel(t *testing.T) {
	// A catch-all virtual host returning 200 for everything would
	// fool a status-code-only check. Sentinel matching catches it.
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: "<html>welcome to my homepage</html>"}, nil
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Errorf("status = %v, want Inaccessible (catch-all vhost detection)", res.Status)
	}
}

func TestMetricCategoriesDeclaresHTTPRequests(t *testing.T) {
	// Required for the primary-source resolver to treat nginx as a
	// candidate when both HAProxy and nginx are detected.
	g := New()
	cats := g.MetricCategories()
	if len(cats) != 1 || cats[0] != gear.CategoryHTTPRequests {
		t.Errorf("MetricCategories() = %v, want [CategoryHTTPRequests]", cats)
	}
}

func TestResolveConfigPath(t *testing.T) {
	cases := []struct {
		name      string
		override  string
		buildInfo string
		stat      func(string) (os.FileInfo, error)
		want      string
	}{
		{
			name:     "override wins",
			override: "/custom/nginx.conf",
			stat:     statExisting(), // override path doesn't need to exist — we trust operator
			want:     "/custom/nginx.conf",
		},
		{
			name:      "buildinfo --conf-path beats well-known",
			buildInfo: "configure arguments: --conf-path=/opt/nginx/conf/nginx.conf --with-http_ssl_module",
			stat:      statExisting("/opt/nginx/conf/nginx.conf", "/etc/nginx/nginx.conf"),
			want:      "/opt/nginx/conf/nginx.conf",
		},
		{
			name: "well-known path used when buildinfo path missing",
			// --conf-path points at something that doesn't exist
			// (e.g. installed from package but config moved). Fall
			// through to well-known list.
			buildInfo: "--conf-path=/var/empty/missing.conf",
			stat:      statExisting("/etc/nginx/nginx.conf"),
			want:      "/etc/nginx/nginx.conf",
		},
		{
			name: "nothing found",
			stat: statExisting(),
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveConfigPath(tc.override, tc.buildInfo, tc.stat)
			if got != tc.want {
				t.Errorf("resolveConfigPath = %q, want %q", got, tc.want)
			}
		})
	}
}

// contains is a strings.Contains alias to keep the test bodies short.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
