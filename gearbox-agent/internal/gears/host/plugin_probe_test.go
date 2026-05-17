package host

import (
	"context"
	"errors"
	"testing"

	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

// newTestGear returns a gear with stubbed-out OS dependencies so probe
// results are reproducible across CI runners. Each test customises the
// fields it cares about; the rest stay at safe inert defaults.
func newTestGear() *Gear {
	return &Gear{
		hostname:        func() (string, error) { return "test-host", nil },
		readFile:        func(string) ([]byte, error) { return []byte("Linux version 6.1.0 (root@builder)\n"), nil },
		cpuCount:        func() int { return 4 },
		systemdDetector: func() bool { return true },
	}
}

func TestProbeAlwaysAvailable(t *testing.T) {
	// Host is always-present by definition; probe must report
	// Available so the manifest carries an entry on every box, even
	// when no proxy / web server is installed.
	g := newTestGear()
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
}

func TestProbePopulatesExpectedCapabilities(t *testing.T) {
	g := newTestGear()
	res := g.Probe(context.Background(), gear.Dependencies{})

	want := map[string]string{
		"hostname":        "test-host",
		"kernel":          "6.1.0",
		"cpu_count":       "4",
		"systemd_present": "true",
	}
	for k, v := range want {
		if got := res.Capabilities[k]; got != v {
			t.Errorf("capabilities[%q] = %q, want %q", k, got, v)
		}
	}
}

func TestProbeOmitsHostnameOnError(t *testing.T) {
	// If os.Hostname fails (rare but possible in odd container
	// configs), omit the field rather than emit an empty string —
	// downstream callers distinguish "not detected" via key absence.
	g := newTestGear()
	g.hostname = func() (string, error) { return "", errors.New("nope") }
	res := g.Probe(context.Background(), gear.Dependencies{})
	if _, ok := res.Capabilities["hostname"]; ok {
		t.Error("hostname should be omitted when os.Hostname fails")
	}
}

func TestProbeOmitsKernelOnReadError(t *testing.T) {
	// macOS test runners have no /proc/version. The detector must
	// degrade gracefully — Available with no kernel key, not an error.
	g := newTestGear()
	g.readFile = func(string) ([]byte, error) { return nil, errors.New("not linux") }
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Errorf("status = %v, want Available on non-Linux", res.Status)
	}
	if _, ok := res.Capabilities["kernel"]; ok {
		t.Error("kernel should be omitted when /proc/version unreadable")
	}
}

func TestProbeReportsSystemdAbsence(t *testing.T) {
	// Containers and BSD-style hosts won't have /run/systemd/system.
	// The capability map must always carry an explicit true/false so
	// the dashboard can render the fact without distinguishing
	// "missing" from "false".
	g := newTestGear()
	g.systemdDetector = func() bool { return false }
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Capabilities["systemd_present"] != "false" {
		t.Errorf("systemd_present = %q, want %q", res.Capabilities["systemd_present"], "false")
	}
}

func TestReadKernelVersionTrustsThirdField(t *testing.T) {
	// Defence against `cat /proc/version` output drift: only parse if
	// the line starts with the expected "Linux version" prefix.
	cases := []struct {
		name, body, want string
	}{
		{"standard", "Linux version 6.1.0 (root@builder) ...", "6.1.0"},
		{"unexpected prefix", "FreeBSD 14.0-RELEASE", ""},
		{"empty", "", ""},
		{"single field", "Linux", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readKernelVersion(func(string) ([]byte, error) { return []byte(tc.body), nil })
			if got != tc.want {
				t.Errorf("readKernelVersion(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}
