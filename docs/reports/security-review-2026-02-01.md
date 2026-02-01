# Gearbox Security Review Report

**Date:** 2026-02-01
**Scope:** Full codebase (`gearbox/` and `gearbox-agent/`)
**Method:** Static analysis against AI Code Security Anti-Patterns (breadth version)

---

## Executive Summary

The Gearbox codebase demonstrates **strong security practices** across both the dashboard and agent applications. No critical production vulnerabilities were found. The codebase uses proper parameterized queries, cryptographically secure random generation, bcrypt password hashing, and comprehensive authentication middleware. A small number of medium and low severity issues were identified, primarily around XSS in JavaScript client-side code and a SQL injection edge case in the backup function.

**Overall Security Posture: STRONG**

---

## Findings Summary

| # | Finding | Severity | CWE | Location |
|---|---------|----------|-----|----------|
| 1 | Unescaped user data in innerHTML (traffic filter label) | High | CWE-79 | `static/js/traffic/traffic-visualization.js:417,451` |
| 2 | Backend data rendered without escaping (backup restore) | High | CWE-79 | `static/js/haproxy_config/editor.js:1400-1410` |
| 3 | SQL injection in VACUUM INTO (backup path) | Medium | CWE-89 | `gearbox/internal/framework/database/backup.go:38` |
| 4 | No rate limiting on dashboard login endpoint | Medium | CWE-307 | `gearbox/cmd/server/main.go` (login routes) |
| 5 | TLS verification can be disabled via env var | Medium | CWE-295 | `gearbox/internal/framework/agent/client.go:70-78` |
| 6 | Cookie Secure flag not set by default | Low | CWE-614 | `gearbox/internal/framework/auth/auth.go:46` |
| 7 | CSP allows 'unsafe-inline' for scripts/styles | Low | CWE-79 | `gearbox/internal/framework/middleware/security_headers.go:60-76` |
| 8 | Swagger docs publicly accessible on agent | Low | CWE-200 | `gearbox-agent/internal/api/server.go:83-85` |
| 9 | Temp file created without explicit permissions | Low | CWE-377 | `gearbox-agent/internal/plugins/security/plugin.go:869` |
| 10 | Error messages may expose internal details | Low | CWE-209 | Multiple locations |

---

## Detailed Findings

### 1. HIGH - XSS: Unescaped Filter Label in Traffic Visualization

**CWE-79 (Cross-Site Scripting)**

**File:** `gearbox/static/js/traffic/traffic-visualization.js`
**Lines:** 417, 451

**Description:** The `activeFilter.label` value is interpolated directly into an innerHTML assignment without escaping. If the label contains user-controlled data (IP addresses, backend names), it could execute arbitrary JavaScript.

**Vulnerable code pattern:**

```javascript
tbody.innerHTML = `<tr><td>No traffic matching filter: ${activeFilter?.label || ''}</td></tr>`;
```

**Recommendation:** Use `escapeHtml()` (already defined elsewhere in the codebase) before interpolation:

```javascript
tbody.innerHTML = `<tr><td>No traffic matching filter: ${escapeHtml(activeFilter?.label || '')}</td></tr>`;
```

---

### 2. HIGH - XSS: Backend Data in Backup Restore UI

**CWE-79 (Cross-Site Scripting)**

**File:** `gearbox/static/js/haproxy_config/editor.js`
**Lines:** 1400-1410

**Description:** Backend API response data (`b.reason`, `b.id`) is rendered via innerHTML without escaping. If the database contains malicious data, it could execute scripts. The `b.id` is also used in an inline `onclick` handler, which is vulnerable to attribute escape attacks.

**Recommendation:** Escape all dynamic values with `escapeHtml()` and use event listeners instead of inline onclick handlers.

---

### 3. MEDIUM - SQL Injection in Database Backup

**CWE-89 (SQL Injection)**

**File:** `gearbox/internal/framework/database/backup.go`
**Line:** 38

**Description:** The backup path is constructed and interpolated directly into a SQL statement via `fmt.Sprintf`:

```go
_, err := d.db.Exec(fmt.Sprintf("VACUUM INTO '%s'", backupPath))
```

SQLite's `VACUUM INTO` does not support parameterized queries, but the backup path should be validated to contain only safe filesystem characters before use.

**Recommendation:** Add explicit path validation (alphanumeric, slashes, hyphens, underscores, dots only) before constructing the query. Reject any path containing single quotes.

---

### 4. MEDIUM - No Rate Limiting on Dashboard Login

**CWE-307 (Improper Restriction of Excessive Authentication Attempts)**

**Description:** The dashboard login endpoint has account lockout (5 failed attempts, 15-minute lockout) but no per-IP rate limiting. Distributed brute force attacks across multiple accounts could bypass the per-account lockout.

**Current mitigation:** Account lockout after 5 failures.

**Recommendation:** Add per-IP rate limiting to the login endpoint, similar to the token bucket algorithm already implemented in the agent (`gearbox-agent/internal/framework/middleware/ratelimit.go`).

---

### 5. MEDIUM - TLS Verification Bypass via Environment Variable

**CWE-295 (Improper Certificate Validation)**

**File:** `gearbox/internal/framework/agent/client.go`
**Lines:** 70-78

**Description:** Setting `GEARBOX_INSECURE_TLS=true` disables TLS certificate verification. While this is intentional for development, it could be exploited if an attacker gains control of environment variables.

**Current mitigation:** Warning logged when enabled; `#nosec G402` annotation present.

**Recommendation:** Document the security implications clearly and consider restricting this to explicitly non-production environments.

---

### 6. LOW - Cookie Secure Flag Default

**CWE-614 (Sensitive Cookie in HTTPS Session Without 'Secure' Attribute)**

**File:** `gearbox/internal/framework/auth/auth.go`
**Line:** 46

**Description:** The `Secure` cookie flag defaults to `false` and is only enabled when TLS is configured. If the application is deployed behind a TLS-terminating proxy without being configured for TLS, session cookies could be sent over HTTP.

**Current mitigation:** Warning logged when running without TLS.

---

### 7. LOW - CSP Allows unsafe-inline

**CWE-79 (Cross-Site Scripting)**

**File:** `gearbox/internal/framework/middleware/security_headers.go`
**Lines:** 60-76

**Description:** The Content-Security-Policy allows `'unsafe-inline'` for both scripts and styles, which reduces XSS protection. This is documented as necessary for Tailwind CSS and inline event handlers.

**Recommendation:** Long-term, consider migrating to CSP nonces or hashes for inline scripts.

---

### 8-10. LOW - Minor Issues

- **Swagger docs public** on the agent could aid reconnaissance (`gearbox-agent/internal/api/server.go:83-85`)
- **Temp file permissions** not explicitly set in security plugin (`gearbox-agent/internal/plugins/security/plugin.go:869`)
- **Error messages** in some handlers use `%v` formatting which could expose internal paths

---

## Security Strengths

The codebase demonstrates many excellent security practices:

| Category | Implementation | Rating |
|----------|---------------|--------|
| **Secrets Management** | No hardcoded production secrets; env vars required; auto-generation with crypto/rand | Excellent |
| **Password Security** | bcrypt cost 12; entropy validation (50+ bits); common password blacklist | Excellent |
| **Session Management** | 128-bit crypto/rand tokens; server-side validation; DB-backed; proper cookie flags | Excellent |
| **CSRF Protection** | 256-bit tokens; validated on all state-changing requests | Excellent |
| **SQL Injection** | Parameterized queries throughout; whitelist validation for dynamic columns | Excellent |
| **Command Injection** | exec.Command with separate args (no shell); input validation on IPs, packages, branches | Strong |
| **Cryptography** | crypto/rand everywhere; TLS 1.2+ enforced; AES-256-GCM for API key encryption | Excellent |
| **Authentication** | Bearer token with constant-time comparison; WebSocket token auth (60s TTL); passkey support | Excellent |
| **Rate Limiting** | Token bucket algorithm on agent (50 req/s, burst 100); per-IP tracking | Strong |
| **Input Validation** | SQL injection detection; email RFC 5322 validation; branch name whitelist regex | Strong |
| **Security Headers** | CSP, X-Frame-Options DENY, X-Content-Type-Options, HSTS, Referrer-Policy | Strong |
| **WebSocket Security** | Origin validation; short-lived single-use tokens; configurable allowed origins | Excellent |
| **Audit Logging** | Login/logout/password changes logged with IP and user-agent | Strong |
| **Config Redaction** | Automatic redaction of passwords, tokens, and secrets in HAProxy configs | Excellent |
| **File Permissions** | API key files stored with 0600; proper backup directory validation | Strong |

---

## Recommendations by Priority

### Immediate

1. Escape `activeFilter.label` in `traffic-visualization.js` (lines 417, 451)
2. Escape backup data in `haproxy_config/editor.js` (lines 1400-1410)

### High Priority

3. Add per-IP rate limiting to dashboard login endpoint
4. Add path validation before `VACUUM INTO` in `backup.go`

### Medium Priority

5. Add input validation for log source fields (Unit, FilePath, Priority) in `streamer.go`
6. Add domain name validation before certbot/acme.sh commands in `collector.go`
7. Consider DOMPurify for complex HTML rendering in JavaScript

### Low Priority

8. Document `GEARBOX_INSECURE_TLS` security implications
9. Protect Swagger docs in production agent deployments
10. Set explicit permissions (0600) on temp files
11. Review error messages for information disclosure
12. Long-term: migrate from CSP `unsafe-inline` to nonces

---

## Domains Reviewed

| Security Domain | Issues Found | Notes |
|-----------------|-------------|-------|
| 1. Secrets and Credentials | 0 | No hardcoded secrets; proper encryption at rest |
| 2. Injection (SQL/Cmd/LDAP) | 1 Medium | VACUUM INTO path; command execution is safe |
| 3. Cross-Site Scripting | 2 High, 1 Low | innerHTML without escaping in 2 JS files |
| 4. Authentication and Sessions | 1 Medium, 1 Low | Missing login rate limiting; cookie Secure flag |
| 5. Cryptographic Failures | 1 Medium | TLS skip verify option (documented/intentional) |
| 6. Input Validation | 0 | Comprehensive validation framework |
| 7. Configuration and Deployment | 1 Low, 1 Low | Swagger exposure; error message detail |
| 8. Dependency and Supply Chain | Not Scanned | Recommend separate `go mod audit` |
| 9. API Security | 0 | Proper auth middleware on all protected routes |
| 10. File Handling | 1 Low | Temp file permissions |
