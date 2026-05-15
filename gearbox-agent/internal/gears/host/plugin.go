// Package host reports static facts about the machine the agent runs on
// (hostname, kernel, CPU count, whether systemd is around). It does not
// collect metrics or expose HTTP routes — its only job is to land a
// `host` entry in the capability manifest so every box's response is
// semantically complete, even on hosts where no proxy / web server is
// installed.
//
// The dashboard's "no HAProxy" mode (issue #91 / PR #93) already infers
// this implicitly; surfacing it explicitly means the dashboard doesn't
// have to special-case "manifest has zero entries" any more.
package host

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

func init() {
	gear.Register(New())
}

// Gear is the static "facts about this machine" detector.
type Gear struct {
	gear.BaseGear

	// Probe-time indirection. Tests swap these to make probe results
	// reproducible across CI runners that have different hostnames /
	// kernel versions. The defaults call the real OS surfaces.
	hostname        func() (string, error)
	readFile        func(string) ([]byte, error)
	cpuCount        func() int
	systemdDetector func() bool
}

// New constructs a host gear with real OS-backed defaults.
func New() *Gear {
	return &Gear{
		hostname:        os.Hostname,
		readFile:        os.ReadFile,
		cpuCount:        runtime.NumCPU,
		systemdDetector: defaultSystemdPresent,
	}
}

// Info returns the gear's metadata.
func (g *Gear) Info() gear.Info {
	return gear.Info{
		Name:        "host",
		DisplayName: "Host",
		Description: "Reports static facts about the machine the agent runs on (hostname, kernel, CPU count, systemd presence).",
		Version:     "1.0.0",
		Category:    "system",
		Core:        true,
	}
}

// Probe always reports Available — every host has a host. The interesting
// bits land in the Capabilities map: hostname, kernel version, CPU count,
// and whether systemd is the init system. Cheap; everything probed here
// is a syscall or a /proc read.
func (g *Gear) Probe(ctx context.Context, _ gear.Dependencies) gear.ProbeResult {
	caps := map[string]string{
		"cpu_count": fmt.Sprintf("%d", g.cpuCount()),
	}

	if name, err := g.hostname(); err == nil && name != "" {
		caps["hostname"] = name
	}

	if kernel := readKernelVersion(g.readFile); kernel != "" {
		caps["kernel"] = kernel
	}

	if g.systemdDetector() {
		caps["systemd_present"] = "true"
	} else {
		caps["systemd_present"] = "false"
	}

	return gear.ProbeAvailable("host facts collected", caps)
}

// RegisterRoutes is a no-op — the host gear's data flows entirely through
// the capability manifest. No HTTP endpoint of its own.
func (g *Gear) RegisterRoutes(_ chi.Router) {}

// readKernelVersion parses the first line of /proc/version for the
// kernel release string. Returns "" on read failure (e.g. running on
// macOS for tests) — the manifest entry then simply omits the field.
func readKernelVersion(readFile func(string) ([]byte, error)) string {
	data, err := readFile("/proc/version")
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if scanner.Scan() {
		// /proc/version starts with "Linux version <kernel> (<builder>)..."
		// Pull out the third token if it looks like a version string.
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 && fields[0] == "Linux" && fields[1] == "version" {
			return fields[2]
		}
	}
	return ""
}

// defaultSystemdPresent reports whether systemd is the init system. We
// look for /run/systemd/system, which systemd creates when it boots and
// removes on shutdown — more reliable than checking for a binary, since
// distros that ship systemd may have it installed but not running.
func defaultSystemdPresent() bool {
	info, err := os.Stat("/run/systemd/system")
	return err == nil && info.IsDir()
}

// Ensure the gear implements the required interfaces.
var (
	_ gear.Gear          = (*Gear)(nil)
	_ gear.ProbeableGear = (*Gear)(nil)
)
