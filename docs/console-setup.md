# Remote Console — Operator Setup

Guide for enabling the in-browser console feature on a gearbox agent.
The dashboard side ships always-on; the agent side is opt-in per box.

## Table of Contents

- [Overview](#overview)
- [When you need it (and when you don't)](#when-you-need-it-and-when-you-dont)
- [Mode A — Host install (the simple case)](#mode-a--host-install-the-simple-case)
- [Mode B.1 — Container with `pid:host` + `privileged` (nsenter)](#mode-b1--container-with-pidhost--privileged-nsenter)
- [Mode B.2 — Container with SSH bridge (TrueNAS-friendly)](#mode-b2--container-with-ssh-bridge-truenas-friendly)
- [Permissions on the dashboard side](#permissions-on-the-dashboard-side)
- [Session recording (optional)](#session-recording-optional)
- [Verifying it works](#verifying-it-works)
- [Troubleshooting](#troubleshooting)

## Overview

The console feature gives an operator a real interactive shell on a
monitored box from inside the dashboard — no SSH keys to manage on
their workstation, no jump host, no VPN. Every session goes through
the existing `gearbox-agent` TLS surface and is gated by the same
API-key authentication, with audit events on every open and close.

> [!IMPORTANT]
> The shell **inherits the agent's UID**. On a typical install the
> agent runs as root (it needs root for `/var/log`, systemd, certs,
> `apt`), so the default console session is a **root shell**. That's
> usually what an operator wants. Set `HAPROXY_AGENT_CONSOLE_RUN_AS=<uid>`
> if you want sessions to land as a less-privileged user.

## When you need it (and when you don't)

| Use the console for                                            | Don't use the console for                       |
|----------------------------------------------------------------|-------------------------------------------------|
| Quick "I need a shell on this one box, now" investigations     | Fleet-wide automation (use SSH + Ansible/etc.)  |
| Debugging an alert from inside the same browser tab            | CI / scripted operations                        |
| Pairing — show a colleague what you're typing in real time     | Long-running interactive sessions (timeout)     |

If your workflow is "I'm at my terminal anyway and have SSH keys
distributed," keep using SSH. The console is for the
"already-in-the-dashboard" path.

## Mode A — Host install (the simple case)

Agent runs directly on the box (systemd unit on Linux, launchd on
macOS). No container, no bridge — `pty.SpawnUnix` opens a real PTY
and runs `/bin/bash -l` as the agent's UID.

### Enable

Edit the agent's environment (typically `/etc/default/gearbox-agent`
or a systemd `Environment=` line):

```bash
HAPROXY_AGENT_CONSOLE_ENABLED=true
# optional overrides:
# HAPROXY_AGENT_CONSOLE_SHELL=/bin/bash -l
# HAPROXY_AGENT_CONSOLE_RUN_AS=1000   # numeric UID; default = inherit
```

Restart the agent:

```bash
sudo systemctl restart gearbox-agent
```

Confirm with `journalctl -u gearbox-agent | grep -i console` — you
should see:

```text
Console: ENABLED — token + WS at /api/v1/console/*; sessions inherit agent UID
```

## Mode B.1 — Container with `pid:host` + `privileged` (nsenter)

Agent runs in a container on a Docker host (e.g. plain Docker
Compose, not TrueNAS app). Cross into the host's namespaces via
`nsenter --target 1` for each session.

> [!WARNING]
> This grants the agent container effectively root-equivalent
> capabilities on the host. Only enable if your threat model accepts
> the agent itself being trusted at root level — which it usually
> already is, since the agent runs as root in Mode A too.

### Required container settings

```yaml
services:
  gearbox-agent:
    image: ghcr.io/sarg3nt/gearbox/gearbox-agent:VERSION
    pid: host
    privileged: true            # or cap_add: [SYS_ADMIN, SYS_PTRACE]
    volumes:
      - /:/host:ro              # so the host's bash path resolves
      - ./data:/var/lib/gearbox-agent
    environment:
      HAPROXY_AGENT_CONSOLE_ENABLED: "true"
      HAPROXY_AGENT_HOST_EXEC: "nsenter"
      # the shell path is resolved in the HOST's mount ns, not the container's
      HAPROXY_AGENT_CONSOLE_SHELL: "/bin/bash -l"
```

Bring it up and check the agent log:

```text
console: nsenter host-exec selected (container → host via PID 1 namespaces)
Console: ENABLED — token + WS at /api/v1/console/*
```

## Mode B.2 — Container with SSH bridge (TrueNAS-friendly)

For environments where `pid:host + privileged` is unacceptable or
impossible — TrueNAS SCALE apps run under a restricted PSP that
forbids both. The agent SSHs out to `127.0.0.1` (or a UNIX socket
mount) on the host using a dedicated keypair.

### One-time setup

1. **Generate the agent's keypair** from inside the agent container
   (or wherever the agent runs):

   ```bash
   gearbox-agent --generate-console-key
   ```

   Output includes the public key and a recipe for the env vars.

2. **Install the public key** on the host's `authorized_keys`. The
   key comment is `gearbox-agent` so you can `grep gearbox-agent
   ~/.ssh/authorized_keys` later to audit.

3. **Capture the host's SSH host key** so the agent can verify it:

   ```bash
   ssh-keyscan -t ed25519 127.0.0.1 > /var/lib/gearbox-agent/console-ssh/host.pub
   ```

4. **Set the env vars on the agent**:

   ```bash
   HAPROXY_AGENT_CONSOLE_ENABLED=true
   HAPROXY_AGENT_HOST_EXEC=ssh-bridge
   HAPROXY_AGENT_CONSOLE_SSH_HOST=127.0.0.1:22
   HAPROXY_AGENT_CONSOLE_SSH_USER=root
   HAPROXY_AGENT_CONSOLE_SSH_KEY=/var/lib/gearbox-agent/console-ssh/agent
   HAPROXY_AGENT_CONSOLE_SSH_HOSTKEY=/var/lib/gearbox-agent/console-ssh/host.pub
   ```

5. Restart the agent. Log should show:

   ```text
   console: ssh_bridge host-exec selected host=127.0.0.1:22 user=root
   ```

> [!CAUTION]
> The agent refuses to start the bridge if the private key has
> permissions wider than `0600`. If you see *"private key has
> too-open permissions"* in the agent log, fix with `chmod 600`.

## Permissions on the dashboard side

Console adds a new permission component, `box_console`, with three
actions:

| Permission                 | What it allows                                                   |
|----------------------------|------------------------------------------------------------------|
| `box_console:view`         | See that console is available for a box                          |
| `box_console:configure`    | Toggle per-box console + edit shell / run-as (per-box UI: Phase 2c) |
| `box_console:connect`      | Open an actual shell session — **the load-bearing one**          |

Grant via *Settings → Users → \<user\> → Permissions*. `connect`
isn't granted to any role by default — opt users in deliberately.

## Session recording (optional)

Opt-in per agent via `HAPROXY_AGENT_CONSOLE_RECORD=true`. Each
session writes a newline-delimited JSON transcript to
`<data-dir>/console-sessions/<box>-<utc>-<sid>.ndjson` (mode `0600`,
parent dir `0700`).

Replay with `jq`:

```bash
jq -r 'select(.t=="out") | .d | @base64d' \
  /var/lib/gearbox-agent/console-sessions/box-20260516T010101-abc12345.ndjson
```

No rotation is built in — wire `logrotate` or a cron sweep yourself.

## Verifying it works

1. Hit the capabilities endpoint directly:

   ```bash
   curl -sk -H "Authorization: Bearer <agent-api-key>" \
     https://<agent-host>:8405/api/v1/console/capabilities | jq
   ```

   You should see `{"enabled": true, "mode": "host_pty", ...}` (or
   `"nsenter"` / `"ssh_bridge"`).

2. Grant a user `box_console:connect`, log into the dashboard, open
   the Bx fleet view, click the `>_` icon on a tile.

3. You should land in a terminal. Try `whoami`, `hostname`, and
   verify they match what you expect.

## Troubleshooting

| Symptom                                                       | Likely cause                                             | Fix                                                        |
|---------------------------------------------------------------|----------------------------------------------------------|------------------------------------------------------------|
| `/api/v1/console/*` returns 404                               | Agent has console disabled                               | Set `HAPROXY_AGENT_CONSOLE_ENABLED=true` and restart        |
| `console icon missing on Bx tile`                             | User lacks `box_console:connect`                         | Grant via Settings → Users → Permissions                   |
| `"Failed to open console session"` in browser                 | Agent unreachable, or token exchange failed              | Check agent logs, network from dashboard host to agent     |
| `nsenter: namespaces unreachable`                             | Container missing `pid:host` or `privileged`             | Add both to compose / k8s manifest                         |
| `ssh_bridge: private key has too-open permissions`            | Key file isn't `0600`                                    | `chmod 600 <key path>`                                     |
| `nsenter mode but lands in container`                         | `/proc/1/ns/mnt` same as `/proc/self/ns/mnt`             | Container wasn't started with `pid:host`                   |
| Session disconnects after 15 min of idle                      | Default idle timeout                                     | Raise `IdleTimeout` (currently env-fixed; PR welcome)      |
| `host key does not match`                                     | Host key rotated since `ssh-keyscan`                     | Re-capture with `ssh-keyscan -t ed25519 127.0.0.1 > ...`   |

See also [security-review/console-threat-model.md](security-review/console-threat-model.md)
for the threat model.
