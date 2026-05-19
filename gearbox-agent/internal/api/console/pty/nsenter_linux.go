//go:build linux

package pty

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// HostExecMode names the strategy the agent uses to cross from a
// container into the host. Reported via the capabilities envelope so
// the dashboard can branch on it.
type HostExecMode string

const (
	HostExecDirect    HostExecMode = "direct"     // not in a container — Mode A
	HostExecNsenter   HostExecMode = "nsenter"    // Mode B.1
	HostExecSSHBridge HostExecMode = "ssh_bridge" // Mode B.2 (placeholder, real impl Phase 2)
	HostExecNone      HostExecMode = "none"       // container, no bridge available
)

// HostExecDetect returns the best available host-exec strategy for
// this agent process. Detection is conservative — if we can't *prove*
// nsenter will work, we don't claim it does. The dashboard would
// rather show "console unavailable on this box" than hand a user a
// shell that lands somewhere unexpected.
//
// Detection rules:
//   - If we're not in a container (no /.dockerenv, no /proc/1/cgroup
//     hint of containerd/docker), the agent is on the host → HostExecDirect.
//   - Else, if HAPROXY_AGENT_HOST_EXEC=nsenter is set AND /proc/1/ns/mnt
//     differs from our own mount namespace AND `nsenter` is on PATH AND
//     the host's bash binary is reachable through /host or directly,
//     → HostExecNsenter.
//   - Else, if HAPROXY_AGENT_HOST_EXEC=ssh-bridge is set, → HostExecSSHBridge
//     (real wiring lands in Phase 2; this returns the mode for the
//     capabilities envelope today).
//   - Else, HostExecNone — capabilities will report host_console=false.
//
// All filesystem probes use the agent's own UID; we don't try to
// detect "could we nsenter if we had more privileges" because the
// honest answer there is "ask the operator to grant them and re-probe."
func HostExecDetect() HostExecMode {
	if !runningInContainer() {
		return HostExecDirect
	}
	switch os.Getenv("HAPROXY_AGENT_HOST_EXEC") {
	case "nsenter":
		if nsenterUsable() {
			return HostExecNsenter
		}
	case "ssh-bridge", "ssh_bridge":
		return HostExecSSHBridge
	}
	return HostExecNone
}

// runningInContainer is a best-effort container detection. The signals
// we trust most: /.dockerenv (Docker), /run/.containerenv (Podman), and
// the absence of a host-style /proc layout. We don't trust /proc/1/cgroup
// strings alone — they're brittle across runtimes.
func runningInContainer() bool {
	for _, p := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	// Heuristic: a container's PID 1 is the entrypoint binary, not
	// systemd/init. If /proc/1/comm contains our own command, we're
	// almost certainly the container's PID 1 — i.e. in a container.
	if data, err := os.ReadFile("/proc/1/comm"); err == nil {
		comm := string(data)
		for _, marker := range []string{"gearbox-agent", "haproxy-agent"} {
			if len(comm) >= len(marker) && comm[:len(marker)] == marker {
				return true
			}
		}
	}
	return false
}

// nsenterUsable reports whether nsenter into PID 1's namespaces would
// work right now. Requires:
//   - the nsenter binary reachable from the container (probed against
//     resolveHostNsenter; distroless containers don't ship util-linux,
//     so we expect to find it on the host via /proc/1/root)
//   - /proc/1/ns/mnt readable (proves we have access to the host's mount
//     ns reference)
//   - that ns differs from our own (proves there's actually a host to
//     cross into)
func nsenterUsable() bool {
	hostMnt, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil {
		return false
	}
	selfMnt, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil {
		return false
	}
	if hostMnt == selfMnt {
		// Same namespace — nsenter would be a no-op and we'd
		// land in the agent container's shell (which doesn't
		// exist because distroless).
		return false
	}
	if resolveHostNsenter() == "" {
		return false
	}
	return true
}

// nsenterCandidates is the search list for an nsenter binary the
// agent can exec. Container-local paths come first because they're
// guaranteed to have a working ELF interpreter in the agent's mount
// namespace; the official agent image bundles a statically-linked
// busybox at /usr/bin/nsenter for exactly this reason.
//
// The /proc/1/root candidates remain as a last-ditch fallback for
// non-distroless agent flavors that happen to share enough libc
// layout with the host to make exec succeed — but in practice, with
// the official distroless image, the kernel resolves the host's
// nsenter binary fine for stat() yet fails the subsequent execve()
// because the binary's PT_INTERP (e.g. /lib64/ld-linux-x86-64.so.2)
// isn't visible in the container's mount namespace.
var nsenterCandidates = []string{
	// Container-local (bundled by Dockerfile, or operator-installed):
	"/usr/bin/nsenter",
	"/bin/nsenter",
	"/usr/sbin/nsenter",
	"/sbin/nsenter",
	// Host fallback via /proc/1/root — only loads when libc paths
	// happen to line up. Kept for completeness; never relied on.
	"/proc/1/root/usr/bin/nsenter",
	"/proc/1/root/bin/nsenter",
}

// resolveHostNsenter returns the first reachable nsenter binary path,
// or "" if none exist. Not cached — cheap stat()s, and operator
// changes (image upgrade, host package install) could affect the
// answer between calls.
func resolveHostNsenter() string {
	for _, p := range nsenterCandidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// SpawnNsenter wraps SpawnUnix in an nsenter invocation. The argv
// becomes: nsenter --target 1 --mount --uts --ipc --net --pid --
// <user shell>. The host shell is whatever the operator configured
// (or /bin/bash by default), resolved at the host's mount namespace.
//
// Requires the container to be run with pid:host + (privileged OR
// CAP_SYS_ADMIN + CAP_SYS_PTRACE). The agent doesn't verify those
// capabilities directly — if nsenter fails at exec, the resulting
// audit event captures the error and the dashboard shows it.
func SpawnNsenter(ctx context.Context, command []string, runAs string, cols, rows uint16) (Session, error) {
	if len(command) == 0 {
		return nil, errors.New("nsenter: empty command")
	}
	// runAs through nsenter is a request to su to that UID *inside*
	// the host namespace, which we don't currently implement. If
	// someone passes one, fail loud rather than silently giving a
	// root shell.
	if runAs != "" {
		return nil, fmt.Errorf("nsenter: run-as UID override not supported in this mode (got %q)", runAs)
	}
	nsenterBin := resolveHostNsenter()
	if nsenterBin == "" {
		return nil, errors.New("nsenter: binary not found (looked under /proc/1/root and /usr/bin)")
	}
	argv := append([]string{
		nsenterBin,
		"--target", "1",
		"--mount", "--uts", "--ipc", "--net", "--pid",
		"--",
	}, command...)
	return SpawnUnix(ctx, argv, "", cols, rows)
}
