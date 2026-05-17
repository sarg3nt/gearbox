# Remote Console — Threat Model

Companion to [docs/console-setup.md](../console-setup.md). This
document is the reasoning audit for why the console feature is shaped
the way it is. If you're touching anything in
`gearbox-agent/internal/api/console/` or
`gearbox/internal/framework/handler/api_console.go`, read this first.

## Table of Contents

- [Threat model summary](#threat-model-summary)
- [In scope](#in-scope)
- [Out of scope](#out-of-scope)
- [Attack surface](#attack-surface)
- [Mitigations by attack vector](#mitigations-by-attack-vector)
- [Residual risks](#residual-risks)
- [Deployment posture summary](#deployment-posture-summary)

## Threat model summary

The console exposes a path from an authenticated dashboard user to an
interactive shell on a monitored box, gated by a per-user permission.
The threat model treats:

- **The dashboard user as authenticated and authorized** at the
  permission boundary, but otherwise untrusted (input fuzzing,
  protocol misuse, replay attempts).
- **The network between browser and dashboard** as TLS-protected but
  potentially observed.
- **The network between dashboard and agent** as TLS-protected and
  pinnable but reachable from a hostile vantage in some deployments.
- **The agent itself** as fully trusted — it already has root on the
  box for non-console reasons (logs, systemd, certs). Console doesn't
  widen this blast radius.

## In scope

| Threat                                                        | Coverage |
|---------------------------------------------------------------|----------|
| Stolen dashboard session cookie → unauthorized session        | ✅       |
| Stolen agent API key → unauthorized session                   | ✅       |
| Stolen console WS token → replay                              | ✅       |
| MITM between dashboard and agent                              | ✅       |
| Cross-Site WebSocket Hijacking from another origin            | ✅       |
| Browser XSS injecting into terminal output                    | ✅       |
| Privilege escalation via the console handler itself           | ✅       |
| Audit-log evasion ("I was never here")                        | ✅       |
| Session leakage between users / boxes (token cross-use)       | ✅       |
| Container escape via the agent's nsenter/SSH bridges          | ✅       |
| Filesystem traversal via box names in recordings              | ✅       |

## Out of scope

| Threat                                                                                  | Why                                                                                       |
|-----------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------|
| Compromised agent → shell on the box                                                    | The agent already runs as root for non-console reasons; console doesn't widen this.       |
| Operator with `box_console:connect` running destructive commands intentionally          | A shell is, by design, the maximum-impact action; permission boundary is the only control. |
| Long-term confidentiality of the shell session content from someone with file access    | Recording (if on) is stored mode `0600` on disk; FDE / KMS is the operator's job.         |
| Side-channel timing on the WebSocket frame stream                                       | Out of scope for an interactive-UX feature; treat as observable.                          |
| Browser-side credential exfiltration via malicious browser extension                    | Generic browser-trust problem, no console-specific mitigation possible.                   |

## Attack surface

The new endpoints and how they're gated:

| Endpoint                                       | Auth                                                              |
|------------------------------------------------|-------------------------------------------------------------------|
| `POST /api/v1/console/token` (agent)           | Bearer API key                                                    |
| `GET  /api/v1/console/ws` (agent)              | Single-use 60s console token (separate namespace from events)     |
| `GET  /api/v1/console/capabilities` (agent)    | Bearer API key                                                    |
| `GET  /api/console/{boxID}/ws` (dashboard)     | Session cookie + `box_console:connect`                            |
| `GET  /api/console/{boxID}/capabilities` (dashboard) | Session cookie + `box_console:view`                         |

## Mitigations by attack vector

### Stolen dashboard session cookie

- Cookie is `HttpOnly`, `Secure`, `SameSite=Strict` per existing
  dashboard policy.
- A stolen cookie still requires `box_console:connect` to be granted
  on the victim's account.
- Audit log records every session-open by `remote_addr`, so abuse is
  detectable post-hoc.

### Stolen agent API key

- Without the WS token (which requires the API key to issue), no
  console session can open. Possessing both the API key AND knowing
  which box has console enabled raises the cost of a stolen key.
- Tokens are single-use and 60s — even a stolen token cannot be
  replayed.
- Operators are encouraged to rotate API keys (`gearbox-agent
  --rotate-api-key`) periodically.

### Stolen console WS token

- 60-second TTL.
- Single-use: validated by deleting from the map before checking
  expiry, so a replay race is impossible.
- Token namespace is separate from the events WS token — a token
  minted for events cannot be replayed against `/console/ws`.
- Wire format makes the token visible only in the query string; the
  dashboard proxy never logs it.

### MITM between dashboard and agent

- TLS 1.2+ mandatory on the agent (TLS 1.3 floor for newer agents);
  optional CA-cert pinning via `AGENT_CA_CERT_PATH`.
- SSH bridge mode requires an explicit host key — `FixedHostKey`
  callback, no `InsecureIgnoreHostKey()` codepath exists.

### Cross-Site WebSocket Hijacking

- Agent's WS upgrader uses the same canonical-origin check as the
  events WS (see [websocket.go](../../gearbox-agent/internal/api/websocket.go))
  — `AGENT_ALLOWED_ORIGINS` allowlist, default same-origin only.
- The console WS endpoint additionally requires a single-use token
  obtained via authenticated API call — CSWSH alone (no token)
  cannot succeed.
- Dashboard's proxy upgrader is permissive because cookie auth has
  already verified the user on the upstream HTTP request.

### Browser XSS in terminal output

- xterm.js parses VT sequences itself; output is never injected as
  HTML.
- The drawer's status pills (mode, UID, box name) are set via
  `textContent`, never `innerHTML`.
- The Bx tile's console-icon column uses explicit DOM construction
  (no `innerHTML`) for the same reason.
- Strict CSP is in effect on the dashboard.

### Privilege escalation via the handler itself

- The agent **never** elevates above its own UID — no `sudo`, no
  `setuid`-up codepath exists.
- run-as override (when set) uses `syscall.Credential{Uid: …}` to
  drop privilege before `exec`. It cannot raise.
- nsenter and ssh-bridge modes refuse run-as overrides — composing
  privilege changes with namespace crossing or remote login would
  hide the effective UID.
- The session-start audit event records the **effective** UID at
  spawn time (not the configured value), so post-hoc review shows
  the actual outcome.

### Audit-log evasion

- Audit events fire from the handler outer loop, **after** session
  cleanup but **before** the WS goroutine returns. A handler crash
  inside the loop is still followed by the deferred conn.Close,
  which preserves the EventBus emission as a deferred call. (If a
  process abort happens — `SIGKILL` to the agent — no event fires,
  but no shell was active either.)
- The audit event includes session ID, byte counts in both
  directions, exit code (when known), and the close reason as a
  short tag (`client_close`, `idle_timeout`, `exit`, `protocol_violation`).

### Session leakage between users / boxes

- The dashboard proxy resolves boxID → agent at the start of the
  request and binds the WS proxy to that single agent. There's no
  shared state between concurrent sessions that could leak.
- The agent's session ID is freshly generated per upgrade; no caller
  controls it.

### Container escape via nsenter / SSH bridge

- nsenter mode is opt-in via `HAPROXY_AGENT_HOST_EXEC=nsenter` and
  requires the operator to have *already* granted `pid:host +
  privileged` on the container — i.e. the escape capability is
  granted explicitly at deploy time, not by the console code.
- SSH bridge mode uses a dedicated keypair (not the operator's
  personal key); the host's authorized_keys entry has the
  `gearbox-agent` comment for easy audit.
- Both modes refuse `run-as` overrides, so the only way to drop to
  a less-privileged user is via the SSH login user
  (`HAPROXY_AGENT_CONSOLE_SSH_USER`) or, for nsenter, by the
  operator running the agent itself as non-root.

### Filesystem traversal via box names in recordings

- `sanitizeForFilename` replaces non-portable chars with `_` and
  strips leading dots. Even though box names are operator-supplied
  (so this is defense-in-depth), the cost of defending is zero.

## Residual risks

1. **A user with `box_console:connect` is effectively a root operator
   on the boxes they can reach.** This is by design — a shell is the
   maximum-impact thing. Treat the permission like sudo.
2. **Session recordings (when enabled) capture credentials typed at
   the prompt.** No automated redaction. Operators who enable
   recording for compliance should also enable encryption-at-rest on
   the data dir and restrict who can read the recordings directory.
3. **The dashboard's WS proxy uses `InsecureSkipVerify: true` for the
   upstream TLS dial.** The HTTP agent client validates certs at the
   HTTP layer; the WebSocket dial relies on the operator-controlled
   trust path (LAN, mTLS, etc.) and the agent's own API-key + token
   gate. A follow-up is to wire the WS dialer to honor
   `AGENT_CA_CERT_PATH` the same way the HTTP client does.
4. **Idle timeout is currently fixed at 15 minutes.** Operators who
   want a longer/shorter cap have to patch the Handler field; no env
   knob today. Follow-up.

## Deployment posture summary

| Posture                          | Risk                                                    | Mitigation                                  |
|----------------------------------|---------------------------------------------------------|---------------------------------------------|
| Agent on host (Mode A)           | Console = remote root on that box                       | Permission boundary; audit log              |
| Agent in container w/ nsenter    | Container has `pid:host + privileged`                   | Audit access to deploy that container       |
| Agent in container w/ SSH bridge | Agent has SSH access to the host                        | Dedicated key, host-key pinned, perms 0600  |
| Recording on                     | Disk holds full session transcripts                     | FDE / KMS; restrict recordings dir         |
| Recording off                    | No post-hoc replay of "what did Joe type"               | Operator chooses                            |
