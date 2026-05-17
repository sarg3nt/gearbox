# Gear Probes — capability-driven gear loading

**Version:** 0.1.0 | **Last updated:** 2026-05-13

The gearbox-agent runs on hosts with wildly different software stacks: an HAProxy gateway, a TrueNAS storage box, a Docker host, a workstation. The agent ships every gear it knows about, but a given host can only usefully run a subset of them. The **probe phase** is how the agent figures out, at startup, which gears apply on *this* box.

This document explains:

- What the probe phase does and what it does not do
- The four-state status enum
- Per-gear probe contracts
- The startup summary table
- How to add `Probe()` to a new gear

For background and the broader capability-driven loading design, see issue [#60](https://github.com/sarg3nt/gearbox/issues/60).

## Lifecycle

The agent's gear lifecycle now has four phases, in this order:

```text
Probe → Initialize → Start → (later) Stop
```

1. **Probe** — each gear is asked `Probe(ctx, deps) ProbeResult`. The result is recorded in the manager and rendered to the journal as a summary table.
2. **Initialize** — only gears whose probe returned `available` go through `Initialize`. Their `Collector`, `Streamer`, runner, and event-handler objects are constructed here.
3. **Start** — the same loaded gears have `Start(ctx)` called; background goroutines (log streamers, periodic tickers, etc.) launch.
4. **Stop** — on shutdown, only gears that were started get `Stop(ctx)`.

Gears that probe non-`available` are skipped at every subsequent stage:

- No `Initialize` (no state objects allocated)
- No `Start` (no goroutines, no exec'd subprocesses, no periodic collection)
- No `RegisterRoutes` (their HTTP endpoints simply do not exist on this host)
- No collector/streamer registration
- No event-handler registration

### What is *not* saved

Be honest about the boundaries:

- **Binary size is unchanged.** Every gear is blank-imported in [`cmd/gearbox-agent/main.go`](../cmd/gearbox-agent/main.go), so all gear code is linked into the binary regardless of probe verdict. Go does not support module unloading.
- **Gear `init()` functions still run.** That is how each gear registers itself with `gear.Register(...)`. The empty `&Gear{}` instance is held in the global registry forever.
- **Probe itself runs on every gear.** That is the point — we need the verdict.

### What is saved

The dominant wins are **CPU/IO and runtime RAM**, not code size:

| Skipped step                          | Cost avoided                                                                            |
|---------------------------------------|-----------------------------------------------------------------------------------------|
| Construct collectors / streamers      | Heap allocations for per-source `tail`/`journalctl` pipelines, parser state, etc.       |
| Start background goroutines           | Goroutine stacks + scheduler load; with log streaming this is the largest single win    |
| Spawn `tail` / `journalctl` children  | Subprocess RSS + parent-side read pumps; was the source of the original log-spam loops  |
| Periodic collection tickers           | Continuous CPU every N seconds, often per-source                                        |
| Chi route entries + event subscribers | Small but real — keeps the router and event bus pointed at gears that actually answer   |

For a TrueNAS host that genuinely has nothing this agent cares about (no HAProxy, no certbot, no fail2ban, no apt), the probe phase reduces the runtime from "five permanently-degraded gears noisily failing every two seconds" to "everything skipped, agent sits idle." That is the regression the probe phase exists to fix.

## Status enum

Each gear reports one of four statuses via `ProbeResult.Status`. The statuses are not interchangeable: each implies a different operator fix, and conflating them is what historically cost debugging time.

| Status          | Meaning                                                       | Operator action                                                          |
|-----------------|---------------------------------------------------------------|--------------------------------------------------------------------------|
| `available`     | Prereqs present and reachable. Gear loads normally.           | None.                                                                    |
| `not_installed` | The thing the gear manages isn't on this host at all.         | Expected on hosts that don't run this software.                          |
| `inaccessible`  | Prereqs exist, but the agent can't reach them.                | Fix the access — add bind mount, adjust permissions, set correct path.   |
| `disabled`      | Forced off by configuration.                                  | Change the config to re-enable. (Reserved; no gear returns this today.)  |

The distinction between `not_installed` and `inaccessible` matters most in container mode. An agent running in a Docker container on an HAProxy host that wasn't given the right bind mounts should report `inaccessible` for the haproxy gear — *not* `not_installed`, because that would send the operator to install HAProxy on a box that already has it.

`ProbeResult.Reason` is a human-readable sentence that names what surface was probed and what was wrong. Examples:

- `"stats socket configured at /run/haproxy/admin.sock but does not exist (HAProxy not running, or bind mount missing in container mode)"`
- `"neither fail2ban-client nor nft found on PATH"`
- `"no haproxy binary on PATH; no stats socket or URL configured"`

Bad reasons (do not ship these):

- `"prereqs not met"`
- `"haproxy unavailable"`
- `"see logs"`

## Per-gear probe contracts

Each gear's `Probe()` determines `available` from a clearly-named host surface. The table below is the current contract; expect this to evolve as #60 lands its container-mode probing work.

| Gear           | `available` when                                                              | Capabilities reported                            |
|----------------|-------------------------------------------------------------------------------|--------------------------------------------------|
| `certificates` | `certbot` on PATH or in common install paths, **or** `acme.sh` home present   | `manager`, `path` / `home`                       |
| `haproxy`      | Stats URL set, **or** stats socket file exists, **or** `haproxy` on PATH      | `stats_url` / `stats_socket`                     |
| `logs`         | `journalctl` **or** `tail` on PATH                                            | `journalctl`, `tail`                             |
| `metrics`      | `/proc/stat` readable                                                         | —                                                |
| `security`     | `fail2ban-client` **or** `nft` on PATH                                        | `fail2ban`, `nftables`                           |
| `traffic`      | Same as `haproxy` (stick tables share the stats socket)                       | `stats_url` / `stats_socket`                     |
| `updates`      | One of `apt-get` / `apt` / `dnf` / `yum` / `zypper` / `apk` on PATH           | `package_manager`, `path`                        |

The `haproxy` and `traffic` probes deliberately distinguish three states:

- **`available`** — stats URL configured, or stats socket file actually exists.
- **`inaccessible`** — socket path is configured but missing on disk; or haproxy binary is present but neither stats source is configured. The fix differs from "install HAProxy."
- **`not_installed`** — no haproxy binary, no socket, no URL. The host genuinely doesn't run HAProxy.

## Startup summary table

After all probes run, the manager writes a single human-readable table to stderr (which lands in the systemd journal on systemd hosts, and in `docker logs` for container deployments):

```text
Gear probe summary:

  GEAR          STATUS    REASON
  certificates  enabled
  haproxy       enabled
  logs          enabled
  metrics       enabled
  security      disabled  neither fail2ban-client nor nft found on PATH
  traffic       enabled
  updates       enabled
```

Conventions:

- Status column shows `enabled` (probe returned `available`) or `disabled` (any other status). The distinct `not_installed` / `inaccessible` / `disabled` reasons live in the reason column for `disabled` rows.
- Reason column is **blank** for enabled rows — clutter-suppressed, since the detected version/path is already in the structured slog stream via `gear probe complete`.
- Columns are auto-aligned to the widest cell; the table works for one gear or twenty.

In addition to the table, the probe phase emits structured slog lines that operators or log shippers can parse:

```text
INFO probing host for gear capabilities registered_gears=7
INFO gear probe complete registered=7 available=6 unavailable=1
```

The dashboard (gearbox web UI) consumes this information via the upcoming `/api/v1/system/capabilities` endpoint — tracked in [#60](https://github.com/sarg3nt/gearbox/issues/60) §6.

## Adding `Probe()` to a new gear

`ProbeableGear` is a **sub-interface** of `Gear`. Implementing it is optional: gears that don't are treated as always-available, which preserves the pre-probe-phase behaviour. Concretely:

```go
package mygear

import (
    "context"
    "os/exec"

    "github.com/sarg3nt/gearbox-agent/internal/framework/gear"
)

func (g *Gear) Probe(ctx context.Context, deps gear.Dependencies) gear.ProbeResult {
    // Detect prereqs side-effect-free. No connections, no state mutation,
    // no loud logging — the manager logs a single summary line per gear.
    path, err := exec.LookPath("the-thing-i-need")
    if err != nil {
        return gear.ProbeNotInstalled(
            "the-thing-i-need binary not found on PATH",
        )
    }
    return gear.ProbeAvailable(
        "the-thing-i-need found",
        map[string]string{"path": path},
    )
}
```

Rules for a good `Probe()`:

1. **Be fast.** Probe runs synchronously before Initialize. Don't open network connections, don't shell out to slow commands (`find /` etc.), don't read multi-megabyte config files.
2. **Be side-effect-free.** No state mutation on the gear receiver. No log spam. The manager logs once per gear; if your probe is chatty, raise it to `Debug` or remove it.
3. **Use the helpers.** `ProbeAvailable`, `ProbeNotInstalled`, `ProbeInaccessible`, `ProbeDisabled` exist so reviewers can see the verdict at a glance.
4. **Reason like a sentence.** "What did I probe, what did I expect, what did I get?" An operator should be able to act on the reason without reading source.
5. **Distinguish missing-thing from missing-access.** If the configured path doesn't exist *as a file*, that may be `not_installed` (host doesn't run it) or `inaccessible` (bind mount missing). Check the parent directory: if the *parent* doesn't exist, mount is missing → `inaccessible`. If parent exists but the file doesn't, host genuinely lacks it → `not_installed`.

## Testing

Manager-level tests live in [`internal/framework/gear/manager_probe_test.go`](../internal/framework/gear/manager_probe_test.go). They use a `withTestRegistry` helper that swaps the global registry's plugin map for an isolated one, so each test controls exactly which gears are registered.

A typical unit test for per-gear `Probe()` should:

- Stub out filesystem / `exec.LookPath` lookups (use `t.TempDir()` and prepend it to `$PATH` via `t.Setenv("PATH", ...)`).
- Assert both the `Status` and the substring of `Reason` an operator would search for.

## Related

- [#60 — capability-driven gear loading + containerizable agent](https://github.com/sarg3nt/gearbox/issues/60) — the broader design; §6 covers the future `/api/v1/system/capabilities` endpoint and the container-mode probing rules.
- [docs/docker.md](docker.md) — current container deployment, including the bind mounts each probe expects to see in container mode.
- [Top-level gear architecture (docs/gears.md)](../../docs/gears.md) — gear system overview shared by gearbox and gearbox-agent.
