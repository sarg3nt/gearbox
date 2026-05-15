package apache

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
	"github.com/sarg3nt/gearbox-agent/internal/framework/probe"
)

type fakeFileInfo struct{ name string }

func (f fakeFileInfo) Name() string         { return f.name }
func (fakeFileInfo) Size() int64            { return 0 }
func (fakeFileInfo) Mode() os.FileMode      { return 0 }
func (fakeFileInfo) ModTime() (t time.Time) { return }
func (fakeFileInfo) IsDir() bool            { return false }
func (fakeFileInfo) Sys() any               { return nil }

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

// lookPathFor returns a lookPath that resolves the listed binaries to
// /usr/sbin/<name> and reports "not found" for everything else.
func lookPathFor(binaries ...string) func(string) (string, error) {
	set := make(map[string]struct{}, len(binaries))
	for _, b := range binaries {
		set[b] = struct{}{}
	}
	return func(b string) (string, error) {
		if _, ok := set[b]; ok {
			return "/usr/sbin/" + b, nil
		}
		return "", errors.New("not found")
	}
}

// newTestGear sets up a debian-flavour Apache install (apache2 binary,
// 2.4.58, mod_status loaded, server-status?auto reachable).
func newTestGear() *Gear {
	return &Gear{
		lookPath: lookPathFor("apache2"),
		runV: func(_ context.Context, _ string) ([]byte, error) {
			return []byte(`Server version: Apache/2.4.58 (Ubuntu)
Server built:   2024-04-30T09:43:42
Server's Module Magic Number: 20120211:127
Server loaded:  APR 1.7.2, APR-UTIL 1.6.3, PCRE 8.39 2016-06-14
Compiled using: APR 1.7.2, APR-UTIL 1.6.3, PCRE 8.39 2016-06-14
Architecture:   64-bit
Server MPM:     event
  threaded:     yes (fixed thread count)
    forked:     yes (variable process count)
Server compiled with....
 -D SERVER_CONFIG_FILE="/etc/apache2/apache2.conf"`), nil
		},
		runM: func(_ context.Context, _ string) ([]byte, error) {
			return []byte("Loaded Modules:\n core_module (static)\n status_module (shared)\n"), nil
		},
		stat: statExisting("/etc/apache2/apache2.conf"),
		httpGet: func(context.Context, string) (probe.HTTPResult, error) {
			return probe.HTTPResult{
				StatusCode: http.StatusOK,
				Body:       "Total Accesses: 12\nTotal kBytes: 4\nUptime: 100\n",
			}, nil
		},
	}
}

func TestProbeReturnsNotInstalledWhenNeitherBinaryFound(t *testing.T) {
	g := newTestGear()
	g.lookPath = lookPathFor() // neither apache2 nor httpd
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusNotInstalled {
		t.Errorf("status = %v, want NotInstalled", res.Status)
	}
}

func TestProbePrefersApache2OverHttpd(t *testing.T) {
	// Both binaries present (unusual but possible on a misconfigured
	// host). apache2 wins per binaryNames ordering.
	g := newTestGear()
	g.lookPath = lookPathFor("apache2", "httpd")
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
	if res.Capabilities["binary"] != "apache2" {
		t.Errorf("binary = %q, want apache2 (preferred when both exist)", res.Capabilities["binary"])
	}
}

func TestProbeFallsBackToHttpd(t *testing.T) {
	// RHEL-style install: only httpd available.
	g := newTestGear()
	g.lookPath = lookPathFor("httpd")
	g.stat = statExisting("/etc/httpd/conf/httpd.conf")
	g.runV = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`Server version: Apache/2.4.62 (CentOS)
 -D SERVER_CONFIG_FILE="/etc/httpd/conf/httpd.conf"`), nil
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
	if res.Capabilities["binary"] != "httpd" {
		t.Errorf("binary = %q, want httpd", res.Capabilities["binary"])
	}
	if res.Capabilities["config_path"] != "/etc/httpd/conf/httpd.conf" {
		t.Errorf("config_path = %q, want RHEL path", res.Capabilities["config_path"])
	}
}

func TestProbeAvailableHappyPath(t *testing.T) {
	g := newTestGear()
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
	want := map[string]string{
		"binary":        "apache2",
		"binary_path":   "/usr/sbin/apache2",
		"version":       "2.4.58",
		"status_url":    defaultStatusURL,
		"status_source": "mod_status",
		"status_module": "true",
		"config_path":   "/etc/apache2/apache2.conf",
	}
	for k, v := range want {
		if res.Capabilities[k] != v {
			t.Errorf("capabilities[%q] = %q, want %q", k, res.Capabilities[k], v)
		}
	}
}

func TestProbeStatusURLOverrideShortCircuits(t *testing.T) {
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		t.Fatal("httpGet should not be called when APACHE_STATUS_URL is set")
		return probe.HTTPResult{}, nil
	}
	deps := gear.Dependencies{ApacheStatusURL: "http://internal.example/_apache_status"}
	res := g.Probe(context.Background(), deps)
	if res.Status != gear.ProbeStatusAvailable {
		t.Errorf("status = %v, want Available", res.Status)
	}
	if res.Capabilities["status_url"] != "http://internal.example/_apache_status" {
		t.Errorf("status_url = %q, want override", res.Capabilities["status_url"])
	}
	if res.Capabilities["override_source"] != "env" {
		t.Errorf("override_source = %q, want env", res.Capabilities["override_source"])
	}
}

func TestProbeInaccessibleOn403(t *testing.T) {
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusForbidden}, nil
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Fatalf("status = %v, want Inaccessible", res.Status)
	}
	if !strings.Contains(res.Reason, "Require") {
		t.Errorf("reason should name the Require directive, got %q", res.Reason)
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
	if !strings.Contains(res.Reason, "mod_status") {
		t.Errorf("reason should name mod_status, got %q", res.Reason)
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
	g := newTestGear()
	g.httpGet = func(context.Context, string) (probe.HTTPResult, error) {
		return probe.HTTPResult{StatusCode: http.StatusOK, Body: "<html>welcome</html>"}, nil
	}
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Errorf("status = %v, want Inaccessible (catch-all vhost detection)", res.Status)
	}
}

func TestMetricCategoriesDeclaresHTTPRequests(t *testing.T) {
	g := New()
	cats := g.MetricCategories()
	if len(cats) != 1 || cats[0] != gear.CategoryHTTPRequests {
		t.Errorf("MetricCategories() = %v, want [CategoryHTTPRequests]", cats)
	}
}

func TestResolveConfigPath(t *testing.T) {
	cases := []struct {
		name     string
		override string
		vOut     string
		stat     func(string) (os.FileInfo, error)
		want     string
	}{
		{
			name:     "override wins",
			override: "/custom/httpd.conf",
			stat:     statExisting(),
			want:     "/custom/httpd.conf",
		},
		{
			name: "SERVER_CONFIG_FILE beats well-known",
			vOut: ` -D SERVER_CONFIG_FILE="/opt/apache/conf/httpd.conf"`,
			stat: statExisting("/opt/apache/conf/httpd.conf", "/etc/apache2/apache2.conf"),
			want: "/opt/apache/conf/httpd.conf",
		},
		{
			name: "well-known when SERVER_CONFIG_FILE points at missing path",
			vOut: ` -D SERVER_CONFIG_FILE="/var/empty/missing.conf"`,
			stat: statExisting("/etc/apache2/apache2.conf"),
			want: "/etc/apache2/apache2.conf",
		},
		{
			name: "RHEL path used when only it exists",
			stat: statExisting("/etc/httpd/conf/httpd.conf"),
			want: "/etc/httpd/conf/httpd.conf",
		},
		{
			name: "nothing found",
			stat: statExisting(),
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveConfigPath(tc.override, tc.vOut, tc.stat)
			if got != tc.want {
				t.Errorf("resolveConfigPath = %q, want %q", got, tc.want)
			}
		})
	}
}
