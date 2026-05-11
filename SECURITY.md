# Security Policy

## Supported Versions

We actively support the following versions of Gearbox with security updates:

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take the security of Gearbox seriously. If you discover a security vulnerability, please follow these steps:

### 1. **DO NOT** Create a Public Issue

Please do not report security vulnerabilities through public GitHub issues, discussions, or pull requests.

### 2. Report Privately

Please report security vulnerabilities via one of these methods:

- **GitHub Security Advisories** (Preferred): Use the "Security" tab and click "Report a vulnerability"
- **Email**: Send details to security@example.com (replace with actual email)

### 3. Include the Following Information

To help us understand and address the issue quickly, please include:

- **Description**: A clear description of the vulnerability
- **Impact**: What the vulnerability allows an attacker to do
- **Reproduction Steps**: Detailed steps to reproduce the issue
- **Affected Versions**: Which versions are affected
- **Proof of Concept**: If possible, include a PoC or code snippet
- **Suggested Fix**: If you have ideas on how to fix it (optional)

### 4. What to Expect

- **Initial Response**: We will acknowledge your report within **48 hours**
- **Status Updates**: We will provide updates on our progress every **7 days**
- **Resolution Timeline**: We aim to release a fix within **90 days** for high-severity issues
- **Disclosure**: We follow coordinated disclosure and will work with you on timing

### 5. Responsible Disclosure

We kindly ask that you:

- Allow us reasonable time to fix the vulnerability before public disclosure
- Make a good faith effort to avoid privacy violations and service disruption
- Do not exploit the vulnerability beyond what is necessary to demonstrate it

### 6. Recognition

We maintain a Security Hall of Fame to recognize researchers who help improve Gearbox's security. If you would like to be acknowledged, please let us know when you report the issue.

## Security Best Practices

### For Users

When deploying Gearbox, follow these security best practices:

1. **Use TLS/HTTPS**: Always use TLS certificates for production deployments
   - Set `TLS_CERT_PATH` and `TLS_KEY_PATH` environment variables
   - Use `AGENT_CA_CERT_PATH` for certificate pinning to agent connections

2. **Strong Secrets**: Use cryptographically secure random values
   ```bash
   # Generate a strong SESSION_SECRET
   openssl rand -hex 32
   ```

3. **Change Default Passwords**: Change the auto-generated admin password immediately
   - Admin credentials are saved to `data/admin-credentials.txt`
   - Delete this file after changing your password

4. **Enable CSP**: Content-Security-Policy headers are enabled by default
   - Configure `CSP_REPORT_URI` to receive violation reports
   - Use `CSP_EXTRA_SOURCES` if you need to allow additional sources

5. **WebSocket Security**: Configure allowed origins for WebSocket connections
   ```bash
   # Agent side - restrict WebSocket origins
   export AGENT_ALLOWED_ORIGINS="https://your-dashboard.example.com"
   ```

6. **Update Regularly**: Keep Gearbox and its dependencies up to date
   ```bash
   # Check for updates
   git pull origin main
   make build
   ```

7. **Network Isolation**: Deploy behind a firewall or reverse proxy
   - Use authentication on reverse proxy for additional security layer
   - Restrict agent API access to dashboard IP addresses only

8. **Secure File Permissions**: Ensure proper file permissions
   ```bash
   # Admin credentials file (if it exists)
   chmod 600 data/admin-credentials.txt

   # Database
   chmod 600 data/gearbox.db

   # TLS certificates
   chmod 600 /path/to/cert.pem /path/to/key.pem
   ```

### For Developers

When contributing to Gearbox:

1. **Security Review**: All code changes undergo security review
2. **Pre-commit Hooks**: Use gitleaks to prevent secret commits
   ```bash
   # Install pre-commit hook
   make install-hooks
   ```

3. **Static Analysis**: Run gosec before submitting PRs
   ```bash
   cd gearbox && gosec ./...
   cd gearbox-agent && gosec ./...
   ```

4. **Dependency Audits**: Check for vulnerable dependencies
   ```bash
   # Go dependencies
   go list -json -m all | nancy sleuth

   # NPM dependencies
   cd gearbox && npm audit
   ```

5. **Follow Secure Coding Guidelines**:
   - Never log sensitive data (passwords, tokens, API keys)
   - Use parameterized queries for SQL (prevent injection)
   - Validate and sanitize all user input
   - Use `textContent` instead of `innerHTML` (prevent XSS)
   - Validate file paths to prevent traversal attacks
   - Use constant-time comparison for secrets

## Security Features

Gearbox includes the following security features:

### Authentication & Authorization

- **Bcrypt Password Hashing**: Industry-standard password hashing (cost 12; OWASP 2026 baseline)
- **Session Management**: Secure, HTTP-only session cookies
- **Password Requirements**: Minimum 50 bits entropy, blocks common passwords
- **Multi-factor Support**: WebAuthn/Passkey support for strong authentication
- **Role-Based Access Control**: Granular permissions per component

### Network Security

- **TLS 1.2+ Only**: Enforces modern TLS versions
- **Certificate Pinning**: Optional CA certificate pinning for agent connections
- **CORS Protection**: WebSocket origin validation
- **Content Security Policy**: Restrictive CSP headers to prevent XSS
- **Security Headers**: X-Frame-Options, X-Content-Type-Options, Referrer-Policy

### Input Validation

- **Email Validation**: RFC 5322 compliant via `net/mail`
- **Branch Name Validation**: Prevents command injection in git operations
- **Symlink Validation**: Prevents path traversal via symlinks
- **SQL Injection Protection**: Parameterized queries throughout

### Data Protection

- **Secret Redaction**: Automatic redaction in debug logs
- **Filesystem-Protected Secrets**: API keys, webhook secrets, and admin
  credentials are stored as files owned by the agent user with mode `0600`
  (owner read/write only). Encryption-at-rest is **not** currently
  implemented; if the filesystem is compromised by an attacker with the
  agent's UID or root, secrets are exposed. Deployments that need
  encryption-at-rest should run the agent on a filesystem with FDE
  (LUKS, FileVault, etc.) or use an external KMS-backed secret store.
- **Secure Password Storage**: Admin credentials written to file with 0600 permissions
- **Session Secrets**: 256-bit minimum session secret requirement

### Monitoring & Auditing

- **Security Scanning**: Automated gosec, trivy, npm audit in CI/CD
- **Dependency Review**: Automated dependency vulnerability checks
- **Secret Scanning**: Gitleaks integration to prevent credential leaks
- **Audit Logging**: Security events logged for monitoring

## Security Updates

Security updates are released as soon as possible after a vulnerability is confirmed and fixed:

- **Critical**: Released within 24-48 hours
- **High**: Released within 7 days
- **Medium**: Released within 30 days
- **Low**: Released with next regular release

Subscribe to the repository to receive notifications of security updates.

## Security Audit History

| Date       | Auditor               | Scope                                       | Findings                        | Status         |
|------------|-----------------------|---------------------------------------------|---------------------------------|----------------|
| 2026-01-31 | Internal Scan         | Full codebase                               | 10 total                        | ✅ Resolved     |
| 2026-05-10 | Internal Deep Audit   | Auth, agent API, HAProxy pipeline, SQL/XSS, agent subprocess | 4 P0, 8 P1, 10 P2, 8 P3 | 🔄 In progress |

## Contact

For security-related questions or concerns, please contact:

- **Security Team**: security@example.com (replace with actual email)
- **GitHub Security**: Use the "Security" tab to report vulnerabilities

## License

This security policy is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
