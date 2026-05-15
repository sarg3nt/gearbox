package docker

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

// fakeFileInfo is a minimal os.FileInfo stand-in for the stat indirection.
// mode lets each test pin the file type — ModeSocket for the happy path,
// 0 / ModeDir to exercise the "exists but not a socket" branch.
type fakeFileInfo struct {
	name string
	mode os.FileMode
}

func (f fakeFileInfo) Name() string         { return f.name }
func (fakeFileInfo) Size() int64            { return 0 }
func (f fakeFileInfo) Mode() os.FileMode    { return f.mode }
func (fakeFileInfo) ModTime() (t time.Time) { return }
func (f fakeFileInfo) IsDir() bool          { return f.mode.IsDir() }
func (fakeFileInfo) Sys() any               { return nil }

// newTestGear returns a gear with stubbed-out OS dependencies so probe
// results don't depend on whether docker is installed on the runner.
func newTestGear() *Gear {
	return &Gear{
		lookPath:        func(string) (string, error) { return "/usr/bin/docker", nil },
		runVersionCmd:   func(context.Context) ([]byte, error) { return []byte("Docker version 24.0.7, build afdd53b\n"), nil },
		stat:            func(string) (os.FileInfo, error) { return fakeFileInfo{name: "docker.sock", mode: os.ModeSocket}, nil },
		systemctlActive: func(context.Context, string) bool { return true },
	}
}

func TestProbeReturnsNotInstalledWhenBinaryMissing(t *testing.T) {
	// No docker binary on PATH → not_installed. The fix is "install
	// docker", not "fix access", so this status is distinct from
	// Inaccessible.
	g := newTestGear()
	g.lookPath = func(string) (string, error) { return "", errors.New("not found") }

	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusNotInstalled {
		t.Errorf("status = %v, want NotInstalled", res.Status)
	}
}

func TestProbeReturnsInaccessibleWhenSocketMissing(t *testing.T) {
	// Docker installed but dockerd not running, or wrong socket path
	// in container mode. Operator fix is "start dockerd / fix bind
	// mount", not "install docker" — Inaccessible signals that.
	g := newTestGear()
	g.stat = func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist }

	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Fatalf("status = %v, want Inaccessible", res.Status)
	}
	if res.Reason == "" {
		t.Error("inaccessible result must carry an operator-readable reason")
	}
}

func TestProbeReturnsInaccessibleWhenPathExistsButIsNotASocket(t *testing.T) {
	// Bind-mount or operator typo points the socket path at a regular
	// file (or a directory). The path exists, but it isn't actually a
	// Unix socket the metrics gear could dial. Catching this at probe
	// time keeps the manifest honest — without the ModeSocket check we
	// would declare Available and the failure would surface later in
	// confusing form.
	g := newTestGear()
	g.stat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{name: "docker.sock", mode: 0}, nil // regular file
	}

	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Fatalf("status = %v, want Inaccessible (path exists but is not a socket)", res.Status)
	}
	if !strings.Contains(res.Reason, "not a socket") {
		t.Errorf("reason should name the non-socket mismatch, got %q", res.Reason)
	}
}

func TestProbeReturnsInaccessibleOnStatError(t *testing.T) {
	// Distinct branch from "does not exist" — e.g. EACCES on the
	// socket parent dir. We should still surface the error verbatim
	// so the operator can act on it.
	g := newTestGear()
	g.stat = func(string) (os.FileInfo, error) { return nil, fs.ErrPermission }

	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusInaccessible {
		t.Errorf("status = %v, want Inaccessible", res.Status)
	}
}

func TestProbeAvailableExtractsVersionAndPath(t *testing.T) {
	g := newTestGear()
	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
	if res.Capabilities["version"] != "24.0.7" {
		t.Errorf("version = %q, want 24.0.7", res.Capabilities["version"])
	}
	if res.Capabilities["binary_path"] != "/usr/bin/docker" {
		t.Errorf("binary_path = %q, want /usr/bin/docker", res.Capabilities["binary_path"])
	}
	if res.Capabilities["socket_path"] != defaultSocketPath {
		t.Errorf("socket_path = %q, want default %q", res.Capabilities["socket_path"], defaultSocketPath)
	}
}

func TestProbeHonorsDockerSocketOverride(t *testing.T) {
	// Rootless docker, custom socket paths — operator sets
	// DOCKER_SOCKET. The probe must use that value and flag the
	// override_source so dashboards can show "this came from an
	// env var, not auto-detection".
	g := newTestGear()
	deps := gear.Dependencies{DockerSocket: "/home/dave/.docker/run/docker.sock"}

	res := g.Probe(context.Background(), deps)
	if res.Status != gear.ProbeStatusAvailable {
		t.Fatalf("status = %v, want Available", res.Status)
	}
	if res.Capabilities["socket_path"] != "/home/dave/.docker/run/docker.sock" {
		t.Errorf("socket_path = %q, want override path", res.Capabilities["socket_path"])
	}
	if res.Capabilities["override_source"] != "env" {
		t.Errorf("override_source = %q, want env", res.Capabilities["override_source"])
	}
}

func TestProbeReportsServiceActive(t *testing.T) {
	g := newTestGear()
	g.systemctlActive = func(context.Context, string) bool { return false }

	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		// Service-not-active doesn't gate Available — socket presence
		// is the real signal. The fact lands in capabilities only.
		t.Fatalf("status = %v, want Available even with service inactive", res.Status)
	}
	if res.Capabilities["service_active"] != "false" {
		t.Errorf("service_active = %q, want false", res.Capabilities["service_active"])
	}
}

func TestProbeTolerantOfVersionParseFailure(t *testing.T) {
	// If `docker --version` returns garbage (or errors), we still
	// know docker is installed enough to be available — just omit the
	// version key rather than failing the probe.
	g := newTestGear()
	g.runVersionCmd = func(context.Context) ([]byte, error) { return []byte("garbage output\n"), nil }

	res := g.Probe(context.Background(), gear.Dependencies{})
	if res.Status != gear.ProbeStatusAvailable {
		t.Errorf("status = %v, want Available", res.Status)
	}
	if _, ok := res.Capabilities["version"]; ok {
		t.Error("version should be omitted when parse fails (no fake value)")
	}
}
