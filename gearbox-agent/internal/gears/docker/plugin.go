// Package docker detects a Docker installation on the host and reports
// it in the capability manifest. Phase 3 only — no metrics collection
// yet; that's Phase 4+ shipped as a separate issue.
//
// The detection logic is ported from the old internal/framework/discovery
// package (deleted in this same PR). Reporting facts here:
//   - whether the docker binary is on PATH
//   - parsed version
//   - whether the socket is reachable (default /var/run/docker.sock or
//     DOCKER_SOCKET override)
//   - whether the systemd service is active (best-effort)
package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

func init() {
	gear.Register(New())
}

// defaultSocketPath is where dockerd listens on a typical Linux install.
// Operators with non-default sockets (rootless, custom paths) override
// via the DOCKER_SOCKET env var.
const defaultSocketPath = "/var/run/docker.sock"

// versionRegex parses `docker --version` output:
//
//	Docker version 24.0.7, build afdd53b
//
// Tolerates patch suffixes and CE/EE annotations by matching everything
// up to the comma.
var versionRegex = regexp.MustCompile(`Docker version ([^,]+)`)

// Gear is the docker installation detector.
type Gear struct {
	gear.BaseGear

	// Probe-time indirection so unit tests don't need docker on the
	// runner. Each field's default in New() calls the real OS surface.
	lookPath        func(string) (string, error)
	runVersionCmd   func(ctx context.Context) ([]byte, error)
	stat            func(string) (os.FileInfo, error)
	systemctlActive func(ctx context.Context, unit string) bool
}

// New constructs a docker gear with real OS-backed defaults.
func New() *Gear {
	return &Gear{
		lookPath: exec.LookPath,
		runVersionCmd: func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "docker", "--version").CombinedOutput()
		},
		stat:            os.Stat,
		systemctlActive: defaultSystemctlActive,
	}
}

// Info returns the gear's metadata.
func (g *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "docker",
		DisplayName: "Docker",
		Description: "Detects a Docker installation and reports its version + socket reachability.",
		Version:     "1.0.0",
		Category:    "system",
	}
}

// Probe reports whether docker is installed and reachable. Precedence:
//  1. docker binary not on PATH → not_installed.
//  2. binary present, socket missing → inaccessible (likely dockerd not
//     running, or the operator's bind mount is wrong in container mode).
//  3. binary + socket present → available. Service-active status from
//     systemctl is included as a fact but doesn't gate availability —
//     the socket existing is what matters for the agent's ability to
//     read metrics in Phase 4+.
//
// `DOCKER_SOCKET` env var overrides the default `/var/run/docker.sock`
// for rootless / custom setups. The probe trusts the configured path
// directly; the only check is os.Stat (no Unix socket dial — that's a
// runtime concern for the metrics gear, not detection).
func (g *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
	binary, err := g.lookPath("docker")
	if err != nil {
		return gear.ProbeNotInstalled("no docker binary on PATH")
	}

	caps := map[string]string{
		"binary_path": binary,
	}

	if out, err := g.runVersionCmd(ctx); err == nil {
		if m := versionRegex.FindStringSubmatch(string(out)); len(m) > 1 {
			caps["version"] = strings.TrimSpace(m[1])
		}
	}

	socketPath := deps.DockerSocket
	if socketPath == "" {
		socketPath = defaultSocketPath
	} else {
		caps["override_source"] = "env"
	}
	caps["socket_path"] = socketPath

	if _, err := g.stat(socketPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return gear.ProbeInaccessible(fmt.Sprintf(
				"docker binary at %s but socket %s does not exist (dockerd not running, or socket path wrong)",
				binary, socketPath,
			))
		}
		// Permission denied or other stat error — still inaccessible
		// but name the actual error so operators can act.
		return gear.ProbeInaccessible(fmt.Sprintf(
			"docker binary at %s but cannot stat socket %s: %v",
			binary, socketPath, err,
		))
	}

	if g.systemctlActive(ctx, "docker") {
		caps["service_active"] = "true"
	} else {
		caps["service_active"] = "false"
	}

	return gear.ProbeAvailable("docker socket reachable", caps)
}

// RegisterRoutes is a no-op — Phase 3 docker detection produces no API
// surface. The metrics gear lands in Phase 4+ (separate issue).
func (g *Gear) RegisterRoutes(_ chi.Router) {}

// defaultSystemctlActive shells out to systemctl. Returns false on any
// error (including "systemctl not installed" on non-systemd hosts) so
// the field reads "false" in the manifest without leaking error noise.
func defaultSystemctlActive(ctx context.Context, unit string) bool {
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", unit)
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out)) == "active"
}

// Ensure the gear implements the required interfaces.
var (
	_ gear.Gear          = (*Gear)(nil)
	_ gear.ProbeableGear = (*Gear)(nil)
)
