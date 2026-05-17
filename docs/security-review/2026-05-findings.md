# Gearbox Security Review — 2026-05

**Audit date:** 2026-05-10. **P0 fixes shipped:** PR #39 (commit `965b535`). Retrospective.

Top-down audit of `sarg3nt/gearbox` (dashboard + agent) covering five subsystems via parallel deep-dives:

1. Dashboard authentication, sessions, CSRF
2. Agent API-key + WebSocket auth
3. HAProxy config-generation pipeline (highest blast radius)
4. SQL injection, XSS, security headers, open redirects
5. Agent subprocess execution, command construction, path traversal

| Severity | Count |
|----------|-------|
| **P0 — Critical, exploitable** | 4 |
| **P1 — High, latent or hard-to-exploit RCE** | 8 |
| **P2 — Medium, hardening + doc accuracy** | 10 |
| **P3 — Low / informational** | 8 |
| **What looks good** | (substantial) |

**Reproducibility:** every finding cites `file:line` from the working tree at the time of audit. Bumping commits will move lines; SHAs are not pinned here.

**Threat-model note** (shared assumption for the whole report):

- `gearbox-agent` runs as **root** with `NoNewPrivileges=false` per its systemd unit. There is no seccomp/AppArmor confinement. Compromise of the agent = full root on the box.
- `gearbox` dashboard has SQLite + bcrypt + sessions; compromise of dashboard credentials yields control of every connected agent.
- `homelab` deployments push their Docker Compose label changes to GitHub, and the agent scrapes from there. Trust boundary on the HAProxy config-generation pipeline is therefore "**anyone with push access to a configured source repo**".

---

## P0 — Critical — ✅ all four FIXED in PR #39

### P0-1. HAProxy config injection via `haproxy.backend.name` label — ✅ FIXED

**Files:** `gearbox-agent/internal/framework/services/compose/parser.go:244`, `gearbox-agent/internal/framework/services/haproxy/generator.go:105,120,140`

**Symptom.** The `haproxy.backend.name` Docker Compose label is consumed without any validation and inserted into HAProxy directives via `fmt.Sprintf`. An attacker with push access to the source repo can put a newline (or any HAProxy keyword) in the label and inject arbitrary directives into the generated `haproxy.cfg`.

**Exploit, end-to-end.**

```yaml
# In a docker-compose.yml in the scraped repo:
labels:
  haproxy.backend.name: "myapp\n  lua-load /tmp/evil.lua"
```

Generator output:

```text
backend myapp
  lua-load /tmp/evil.lua
  server myapp_srv 10.40.0.99:8080
```

HAProxy executes Lua at startup with HAProxy's privileges (typically root). With Lua not compiled in, `errorfile <code> <path>` reads arbitrary host files at startup, `default_backend` reroutes traffic, etc. The reload at the end of the pipeline runs as the agent (root).

**Why P0.** Real, working exploit. The attacker only needs commit access to a repo the agent is configured to scrape — which for the homelab pattern is just "push to the `homelab` repo," which is whoever can authenticate to GitHub as the maintainer (or a compromised CI token).

**Fix.** Validate `BackendName` at parser time, before it's stored in `BackendConfig`. DNS-label-style regex is appropriate:

```go
var validBackendName = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,62}$`)
```

Reject the whole backend on mismatch (don't try to "sanitize" — that's where injection bypasses live).

### P0-2. HAProxy config injection via `haproxy.acl.ip` label — ✅ FIXED

**File:** `gearbox-agent/internal/framework/services/compose/parser.go:275`, `generator.go:49-54`

**Symptom.** Same class as P0-1. `acl.ip` is parsed with no validation and inserted into ACL directives. The generator splits on commas, but doesn't validate each range. An attacker injects:

```yaml
labels:
  haproxy.acl.ip: "10.0.0.0/8\n  acl admin src 0.0.0.0/0"
```

The output adds an additional `acl admin src 0.0.0.0/0` that bypasses the intended IP allowlist on whatever backend it's attached to. Or `# comment`-style injection comments out the deny rule.

**Why P0.** Same exploitability as P0-1, lower blast radius (config bypass vs. arbitrary code), but still a real attack.

**Fix.** Validate each comma-separated value with `net.ParseCIDR` (or `net.ParseIP` for single IPs); reject the field on any failure.

### P0-3. SECURITY.md claims "encryption at rest with AES-256-GCM" that does not exist — ✅ FIXED (docs)

**Files:** `gearbox-agent/internal/framework/crypto/keys.go` (all), `SECURITY.md:174`

**Symptom.** The agent's API keys, the webhook signing secret, and (presumably) any future secret material are stored as **plaintext hex strings** in files with mode 0600. `grep -ri "aes\|GCM\|cipher" gearbox-agent/` returns zero hits. The "encrypted at rest" claim in `SECURITY.md` is documentation that doesn't match reality.

**Why P0.** This is a misrepresentation issue, not a runtime exploit. But the SECURITY.md file is the primary thing a security reviewer or compliance audit reads, and it's lying. That's a category of finding that erodes trust in everything else the doc says.

**Fix.** Pick one:

1. Implement actual envelope encryption (key from `argon2id(passphrase, salt)` or an external KMS, then AES-256-GCM the secret material at rest). Substantial work, real value.
2. Remove the claim from `SECURITY.md` and replace with "Secret files are protected by filesystem permissions (mode 0600, owner-only). Encryption at rest is not currently implemented; if the filesystem is compromised, secrets are exposed." Cheap, accurate, defensible.

For a single-operator deployment, (2) is honest. (1) only buys real protection if the decryption key lives somewhere the attacker can't reach (KMS, TPM, prompt at startup) — co-locating the key with the ciphertext is theater.

### P0-4. Open redirect via post-login `returnURL` query parameter — ✅ FIXED

**File:** `gearbox/internal/framework/handler/login.go:129-130`

```go
redirectTarget := h.resolvePostLoginPath(user.ID)
if returnURL != "" && returnURL != "/login" && returnURL != "/logout" {
    redirectTarget = returnURL
}
http.Redirect(w, r, redirectTarget, http.StatusSeeOther)
```

**Symptom.** Post-login redirect target accepts an arbitrary `returnURL` query param. Validation only checks for two literal strings. An attacker phishes a victim to `https://gearbox.example.com/login?return=https://attacker.example.com/credentials.html`, the victim logs in, and gets seamlessly redirected to the attacker's harvest page.

**Why P0.** Standard open-redirect class. Not by itself critical, but pairs with credential-phishing chains to make them look legitimate.

**Fix.** Allowlist relative paths only:

```go
if !strings.HasPrefix(returnURL, "/") || strings.HasPrefix(returnURL, "//") {
    returnURL = ""  // fall through to default
}
```

The `//` check rejects protocol-relative URLs (`//evil.com/path`).

---

## P1 — High

### P1-1. Service-name flag injection in systemd control endpoints

**Files:** `gearbox-agent/internal/gears/metrics/plugin.go:276-281`, `collector.go:236`

**Symptom.** The whitelist allows `[a-zA-Z0-9._@-]+`. That permits a "service name" of `--all` or `--no-block`, which `systemctl start --all` would interpret as a flag instead of a unit. Discrete `exec.Command` args block shell injection, but **don't** block flag injection.

**Fix.** Either validate against `ListAvailableServices()` membership, OR insert `--` between subcommand and unit name: `exec.Command("systemctl", "start", "--", svc)`.

### P1-2. Certbot/acme.sh domain argument unvalidated

**Files:** `gearbox-agent/internal/gears/certs/collector.go:732,833`

**Symptom.** Domain flows from HTTP API into `exec.Command(certbotBin, "renew", "--cert-name", domain, "--force-renewal")`. No FQDN validation. `domain="--help"` runs `certbot renew --cert-name --help --force-renewal`, which certbot may interpret as flag injection. More concerning: `domain="../../../tmp/evil"` may confuse certbot's cert lookup.

**Fix.** Validate with strict FQDN regex before exec:

```go
var validFQDN = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
```

### P1-3. fail2ban jail-name injection

**File:** `gearbox-agent/internal/gears/security/fail2ban.go:148`

**Symptom.** Same class as P1-1/P1-2. `fail2ban-client status <jail>` accepts the jail name from… where exactly? The audit subagent flagged this as critical but the input flow chain wasn't fully traced. If jail names ALWAYS come from `fail2ban-client status` (which lists configured jails), the risk is theoretical. If they come from API input, it's real flag injection.

**Fix.** Trace the input source. If from API: validate `^[a-zA-Z0-9_-]+$`. If from fail2ban itself: document the trust boundary.

### P1-4. `CSP_EXTRA_SOURCES` / `CSP_REPORT_URI` env vars splice unsanitized into CSP header

**File:** `gearbox/internal/framework/middleware/security_headers.go:88-103`

**Symptom.** These env vars are concatenated into the CSP header without validation. An attacker who compromises the deployment environment (container escape, CI compromise) can splice `'unsafe-eval' *;` into the CSP, neutralizing it for every user from that point on. The trust boundary is wrong: env vars set by the deployment SHOULD be trusted, but if compromised the CSP becomes attacker-controlled.

**Fix.** Validate each `CSP_EXTRA_SOURCES` entry against a strict source-expression regex; reject anything containing `;` (which closes a directive). Validate `CSP_REPORT_URI` is a parseable `http(s)://` URL.

### P1-5. WebSocket origin validation case-sensitive and port-naive

**File:** `gearbox-agent/internal/framework/server/websocket.go:60-66`

**Symptom.** Origin comparison is byte-equal after stripping `https?://`. `Origin: https://Example.Com` does NOT match an allowlist entry of `example.com`. `Origin: https://example.com:8405` does not match `example.com`. Either masks legitimate connections OR creates allowlist bypasses depending on how the allowlist is generated.

**Fix.** Parse both with `net/url.Parse`, lowercase hosts, normalize ports, compare scheme + host (+ port if relevant) separately.

### P1-6. Rate limiter unbounded map growth (memory-DoS surface)

**File:** `gearbox-agent/internal/framework/middleware/ratelimit.go:65-87`

**Symptom.** The rate limiter creates a new map entry per client IP. Cleanup runs every 5 min and only evicts entries idle for >10 min. A distributed attacker (or even a misbehaving NAT'd network) can flood the map with unique IPs faster than cleanup; memory grows unbounded.

**Fix.** Use a bounded LRU cache, OR enforce a hard cap (refuse new entries beyond N, treating overflow as rate-limited).

### P1-7. No per-endpoint brute-force protection separate from global IP limit

**Files:** `gearbox-agent/internal/framework/server/auth.go:33-34`, `server.go:113-114`

**Symptom.** Login (dashboard) has account-level lockout (5 attempts, 15 min). Agent's API-key authentication has only the global IP rate limiter (50/sec). At 50 req/sec per IP, a brute-force on a 256-bit hex API key is mathematically infeasible — but at scale, a botnet with N IPs gets N×50 attempts/sec. The defense relies entirely on key entropy.

**Fix.** Add exponential backoff per IP after auth failures: 3 failures → 1 min cooldown, 5 → 10 min, 10 → 1 hour. Stacks on top of the rate limiter, not in place of it.

### P1-8. Alpine.js directives in templates with no Alpine.js script loaded

**File:** `gearbox/internal/framework/templates/components/dialog.templ:9-11,63-66,125-128`

**Symptom.** Modal components use `x-data`, `x-show`, `@keydown.escape.window`, `@click.away` — Alpine.js directives — but no `<script src=alpine.js>` is loaded in the base layout. The modals are non-functional (silent feature break) AND if Alpine is added later, the inline event handlers would need `script-src 'unsafe-eval'` in the CSP unless the CSP-safe build is used. Either way, the current state is broken; the future state risks CSP regression.

**Fix.** Decide: rewrite the modals in HTMX + vanilla JS (consistent with the rest of the stack), or load Alpine 3 + `@alpinejs/csp` build with appropriate CSP. Don't leave it in the broken middle state.

---

## P2 — Medium

### P2-1. CSRF token comparison uses `!=` (not constant-time)

**File:** `gearbox/internal/framework/auth/auth.go:281`

The other constant-time comparison (API key in the agent at `apikey.go`) is correct. CSRF should match. Replace with `crypto/subtle.ConstantTimeCompare`.

### P2-2. SECURITY.md says bcrypt cost 10; code uses 12

**Files:** `SECURITY.md:150`, `gearbox/internal/framework/auth/password.go:31`

Doc accuracy. Code is correct (cost 12 is the 2026 OWASP baseline). Update SECURITY.md.

### P2-3. Session has sliding timeout, no hard TTL

**File:** `gearbox/internal/framework/auth/auth.go:209,294-311`

A heavily-used admin account never has to re-authenticate. Server-side token validation is good and logout works, but a hard 24-72h TTL would force periodic re-auth and reduce blast radius of a stolen session cookie on a long-running browser.

### P2-4. No per-email rate limit on login (only per-IP + account lockout)

**Files:** `gearbox/internal/framework/auth/auth.go:22-23`, `cmd/server/main.go:350`

5 attempts/15min lockout per account is solid against single-IP. Distributed credential-stuffing across many IPs against a single email can still iterate the password list. Add per-email rate limiting (e.g., 3 failed attempts/min/email) in addition to the per-IP and per-account limits.

### P2-5. Health endpoint leaks build version

**File:** `gearbox-agent/internal/framework/server/handlers.go:50-59`

`/health` is unauthenticated and returns `{"version": "1.0.0", "uptime": "..."}`. Lets attackers fingerprint the exact build to look up known CVEs. Strip everything except `{"status":"ok"}` from the unauthenticated response.

### P2-6. Missing security headers: HSTS, Permissions-Policy

**File:** `gearbox/internal/framework/middleware/security_headers.go:15-40`

Set:

- `Strict-Transport-Security: max-age=31536000; includeSubDomains; preload`
- `Permissions-Policy: geolocation=(), microphone=(), camera=(), usb=(), payment=()`

### P2-7. TLS config not explicit (relies on Go defaults)

**File:** `gearbox-agent/internal/framework/server/server.go:168`

`s.httpServer.ListenAndServeTLS(...)` with no explicit `TLSConfig`. Go defaults to TLS 1.2 minimum. Force TLS 1.3:

```go
s.httpServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13}
```

### P2-8. Webhook secret stored plaintext (same as API key)

**Files:** `gearbox-agent/internal/framework/crypto/keys.go`, `webhook.go`

Related to P0-3. The HMAC verification IS constant-time (`hmac.Equal`); the secret material is plaintext at rest.

### P2-9. Package-install name validation too weak

**File:** `gearbox-agent/internal/gears/updates/plugin.go:787-795`

Length-only validation. A package name of `--reinstall` flows to `apt-get install --reinstall`. Apply `^[a-zA-Z0-9.+-]+$` regex AND/OR use `apt-get install -- <pkg>` separator.

### P2-10. `ACLPath` / `ACLHeader` parsed and stored but unused

**File:** `gearbox-agent/internal/framework/services/compose/parser.go:271-272`

Latent injection risk. If any future PR wires these fields into the generator without revisiting validation, P0-1's class of bug re-emerges. Either delete the fields now or validate them now even though they're unused.

---

## P3 — Low / informational

- **P3-1.** `SECURITY.md:25,206` has `security@example.com` placeholder never replaced.
- **P3-2.** Swagger UI exposed without auth on `/swagger/*`. Useful in dev, info-disclosure in prod. Either auth-gate or build-flag exclude in release.
- **P3-3.** WebSocket token expiry check runs AFTER `delete()` from the map. Effectively safe (single-use enforced by deletion) but the code order is confusing. `wstoken.go:80-83`.
- **P3-4.** WebAuthn/passkey path has no backup-codes recovery flow. If the user loses their authenticator, they fall back to password — fine, but limits MFA value.
- **P3-5.** Certbot deploy hook is hardcoded `/etc/letsencrypt/renewal-hooks/deploy/haproxy.sh`. If that path is ever made writable by a non-root user, symlink attack → code exec at hook time. Verify permissions in deployment docs.
- **P3-6.** `CheckPassword` wraps `bcrypt.CompareHashAndPassword` (which IS constant-time internally). The wrapper itself uses `err == nil` which is fine, but future maintainers swapping hash algorithms could regress. Worth a comment.
- **P3-7.** Log file path traversal: today safe because paths come from a fixed `DefaultSources()` allowlist. If a future API ever accepts a `path=` parameter, the validator must use `filepath.Clean` + `strings.HasPrefix` against the base, not a naive `..` check.
- **P3-8.** nftables IP-block validation only supports single IPs (no CIDR). Less flexible than fail2ban but safer; flag if you want CIDR support later.

---

## What looks GOOD

A surprising amount, given the surface area:

**Auth / sessions (dashboard):**

- Bcrypt cost 12 (2026 OWASP baseline)
- Server-side session token validation on every request — sessions live in the DB and can be revoked instantly
- HTTP-only / Secure / SameSite=Strict cookies
- Session secret minimum 256 bits enforced at boot
- Account lockout after 5 failed attempts
- Email enumeration mitigated — same generic error for missing-user and wrong-password paths
- Entropy-based password validation (`go-password-validator`, 50-bit minimum, blocks common passwords) — not the naive "must have 1 uppercase 1 number" check
- First-run admin password generated at 158 bits entropy, written 0600, forced change on first login, file deleted after change
- Audit logging with IP + UA on every auth event
- Email validation via `net/mail.ParseAddress` (RFC 5322), not regex

**Agent API:**

- API keys 256-bit hex, `crypto/rand.Read`-sourced
- `crypto/subtle.ConstantTimeCompare` for API key match (good — CSRF should follow this pattern)
- Bearer token in `Authorization` header, NOT query param
- HMAC-SHA256 for webhook signatures with `hmac.Equal` (constant time)
- WebSocket token is short-lived (60s)
- Two-step WebSocket token exchange pattern is correct
- TLS files mode 0600 (key) and 0644 (cert), perms actually set with `os.WriteFile`
- Git branch name validated against `[a-zA-Z0-9_/-]+` allowlist before `git clone`
- Request logger doesn't include headers or body — auth values not leaked to access logs

**SQL / XSS / output:**

- Every `db.Query` / `db.Exec` uses `?` placeholders — no `fmt.Sprintf` SQL anywhere in the dashboard
- Whitelist-only dynamic table reference in `GetDatabaseStats` (safe)
- `templ.Raw` used ONLY for static SVG icon embedding, never for user data
- JSON endpoints use explicit request/response DTOs — no `json.Unmarshal` into the full User model with `PasswordHash`
- WebSocket / SSE frames are JSON, never raw HTML
- CSP middleware present, restrictive for `script-src` (`'self'` + named CDN, with `'unsafe-inline'` documented as a Tailwind tradeoff)
- CSRF middleware enforced on POST/PUT/DELETE/PATCH

**HAProxy pipeline (the GOOD parts):**

- `hostname`, `server` (`ip:port`), `mode`, `balance`, `checkInterval`, `checkFall`, `checkRise`, `rateLimit`, `backendSSLVerify` all have strict regex validation in `parser.go` before reaching the generator
- Atomic config replace: tempfile → `haproxy -c -f` syntax check → rename. If validation fails, HAProxy keeps running the old config.

**Process hygiene:**

- Pre-commit gitleaks hook documented
- `gosec`, Trivy, CodeQL, OpenSSF Scorecard, npm audit, Dependency Review all in CI
- Per-gear `AgentClient` facade interfaces — code-level dependency minimization, makes audit easier

---

## Recommended fix order

1. **P0-1, P0-2** (HAProxy injection — 2 regexes in `parser.go`, ~20 lines of change, full fix). Add unit tests with malicious labels.
2. **P0-3** (SECURITY.md AES-256-GCM claim) — pick the cheap fix (remove claim) or the real one (implement). Land same week.
3. **P0-4** (open redirect) — single-line allowlist fix in `login.go`. Add an integration test.
4. **P2-10** (`ACLPath` / `ACLHeader`) — delete the unused fields OR validate them now. 5-line fix.
5. **P1-1, P1-2, P1-3, P2-9** (argument-flag injection) — uniform `--` separator pattern across all `exec.Command` flag-bearing call sites + per-call regex validation of the flowing argument.
6. **P1-4** (CSP env splicing) — validate the two env vars.
7. **P1-5** (WS origin normalization) — `net/url.Parse` + lowercase.
8. **P2-2** (SECURITY.md bcrypt 10→12), **P2-1** (constant-time CSRF), **P2-6** (HSTS + Permissions-Policy) — three trivial fixes, batch into one PR.
9. The rest as backlog.

---

## Disclosure timeline

| Date       | Event                                                                                           |
|------------|-------------------------------------------------------------------------------------------------|
| 2026-05-10 | Audit performed; findings doc kept off the public branch.                                       |
| 2026-05-10 | Fix PR (#39) opened with the four P0 fixes + tests.                                             |
| 2026-05-10 | PR #39 merged to `main` as `965b535`.                                                           |
| 2026-05-10 | This retrospective + findings doc pushed (this PR).                                             |

The P1, P2, and P3 items above remain open and are tracked for the next maintenance window. None of them have a public exploit chain as direct as the four P0s; the flag-injection class (P1-1..P1-3) needs an attacker who can already authenticate to the agent API, which is a much higher bar than "anyone with push access to a label source."
