// Package certs provides certificate discovery, monitoring, and renewal functionality.
// It supports both certbot and acme.sh certificate managers, auto-detects the installed
// manager, and provides certificate expiration tracking and renewal operations.
package certs

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Default paths for certificate discovery.
const (
	DefaultAcmeshHome   = "/root/.acme.sh"
	DefaultCertbotLive  = "/etc/letsencrypt/live"
	DefaultHAProxyCerts = "/etc/haproxy/certs"
	AcmeshScript        = "acme.sh"
	AcmeshLogFile       = "acme.sh.log"
	CertbotBinary       = "certbot"
	CertbotDeployHook   = "/etc/letsencrypt/renewal-hooks/deploy/haproxy.sh"
)

// allowedSymlinkDirs defines directories where symlink targets are allowed.
// This prevents path traversal attacks via symlinks pointing to arbitrary locations.
var allowedSymlinkDirs = []string{
	"/usr/local/bin",
	"/usr/bin",
	"/bin",
	"/opt",
	"/root/.local/bin",
	"/home",
}

// isSymlinkTargetSafe validates that a resolved symlink target is within allowed directories.
// This prevents path traversal attacks where a symlink could point to arbitrary files.
func isSymlinkTargetSafe(target string) bool {
	// Get absolute path
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	// Clean the path to remove .. and other traversal attempts
	cleanTarget := filepath.Clean(absTarget)

	// Check if the target is within any allowed directory
	for _, allowedDir := range allowedSymlinkDirs {
		// Use filepath.HasPrefix equivalent check
		allowedPrefix := filepath.Clean(allowedDir) + string(filepath.Separator)
		if strings.HasPrefix(cleanTarget+string(filepath.Separator), allowedPrefix) {
			return true
		}
		// Also allow exact match with allowed directory
		if cleanTarget == filepath.Clean(allowedDir) {
			return true
		}
	}

	return false
}

// Collector handles certificate discovery and metrics collection.
type Collector struct {
	logger          *slog.Logger
	acmeshHome      string
	certbotLive     string
	certbotPath     string // Detected certbot binary path
	haproxyCerts    string
	certManager     string // "certbot", "acme.sh", or "none"
	configuredTimer string // User-configured timer name (optional)
}

// NewCollector creates a new certificate collector.
// certbotTimer is optional - if empty, auto-detection will try common timer names.
func NewCollector(logger *slog.Logger, certbotTimer string) *Collector {
	if logger == nil {
		logger = slog.Default()
	}

	c := &Collector{
		logger:          logger,
		haproxyCerts:    DefaultHAProxyCerts,
		certbotLive:     DefaultCertbotLive,
		certManager:     CertManagerNone,
		configuredTimer: certbotTimer,
	}

	// Detect certificate manager - prefer certbot over acme.sh
	if c.detectCertbot() {
		c.certManager = CertManagerCertbot
		c.logger.Info("Certificate manager detected", "manager", "certbot", "path", c.certbotPath)
	} else if acmeshHome := detectAcmeshHome(); acmeshHome != "" {
		c.acmeshHome = acmeshHome
		c.certManager = CertManagerAcmeSH
		c.logger.Info("Certificate manager detected", "manager", "acme.sh", "home", acmeshHome)
	} else {
		c.logger.Info("No certificate manager detected")
	}

	return c
}

// Common certbot installation paths (pipx, pip, system package)
var certbotPaths = []string{
	"/usr/local/bin/certbot",        // symlink or pip install (most common)
	"/root/.local/bin/certbot",      // pipx install as root
	"/usr/bin/certbot",              // system package (apt/dnf)
	"/snap/bin/certbot",             // snap install
	"/opt/certbot/bin/certbot",      // certbot-auto or venv install
	"/home/certbot/.local/bin/certbot", // pipx install as certbot user
}

// detectCertbot checks if certbot is installed and functional.
// If found, stores the path in c.certbotPath for later use.
// Uses multiple detection strategies: PATH lookup, common paths, and find command.
func (c *Collector) detectCertbot() bool {
	// Strategy 1: Check PATH first (handles symlinks properly)
	if resolved, err := exec.LookPath("certbot"); err == nil {
		c.certbotPath = resolved
		c.logger.Debug("found certbot via PATH", "path", resolved)
		return true
	}

	// Strategy 2: Check common locations
	for _, path := range certbotPaths {
		// Use Lstat first to check if the path exists (works for symlinks)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}

		// If it's a symlink, try to resolve it
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				c.logger.Debug("certbot symlink unresolvable", "path", path, "error", err)
				continue
			}

			// SECURITY: Validate symlink target to prevent path traversal
			if !isSymlinkTargetSafe(resolved) {
				c.logger.Warn("certbot symlink target outside allowed directories",
					"symlink", path,
					"target", resolved)
				continue
			}

			// Check if resolved path exists and is executable
			if info, err := os.Stat(resolved); err == nil && info.Mode()&0111 != 0 {
				c.certbotPath = path // Use the symlink path for consistency
				c.logger.Debug("found certbot via symlink", "symlink", path, "target", resolved)
				return true
			}
		} else if info.Mode()&0111 != 0 {
			// Regular file that is executable
			c.certbotPath = path
			c.logger.Debug("found certbot", "path", path)
			return true
		}
	}

	// Strategy 3: Use find command as last resort to search common bin directories
	searchDirs := []string{"/usr/local/bin", "/usr/bin", "/root/.local/bin", "/opt", "/home"}
	for _, dir := range searchDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		cmd := exec.Command("find", dir, "-name", "certbot", "-type", "f", "-perm", "/111", "2>/dev/null")
		output, _ := cmd.Output()
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Verify the found file is accessible and executable
			if info, err := os.Stat(line); err == nil && info.Mode()&0111 != 0 {
				c.certbotPath = line
				c.logger.Debug("found certbot via find", "path", line)
				return true
			}
		}
	}

	c.logger.Debug("certbot not found")
	return false
}

// detectAcmeshHome finds the acme.sh installation directory.
func detectAcmeshHome() string {
	// Check LE_CONFIG_HOME environment variable first
	if envHome := os.Getenv("LE_CONFIG_HOME"); envHome != "" {
		scriptPath := filepath.Join(envHome, AcmeshScript)
		if _, err := os.Stat(scriptPath); err == nil {
			return envHome
		}
	}

	// Check common locations - order matters, most common first
	locations := []string{
		"/root/.acme.sh",
		"/home/acme/.acme.sh",
	}

	// Add $HOME/.acme.sh if HOME is set and different from above
	if home := os.Getenv("HOME"); home != "" {
		homePath := filepath.Join(home, ".acme.sh")
		// Avoid duplicates
		isDuplicate := false
		for _, loc := range locations {
			if loc == homePath {
				isDuplicate = true
				break
			}
		}
		if !isDuplicate {
			locations = append(locations, homePath)
		}
	}

	for _, loc := range locations {
		scriptPath := filepath.Join(loc, AcmeshScript)
		if _, err := os.Stat(scriptPath); err == nil {
			return loc
		}
	}

	// Try to find acme.sh in PATH and derive home from it
	if acmePath, err := exec.LookPath("acme.sh"); err == nil {
		// acme.sh is typically at ~/.acme.sh/acme.sh
		acmeDir := filepath.Dir(acmePath)
		if filepath.Base(acmeDir) == ".acme.sh" {
			return acmeDir
		}
	}

	return ""
}

// Collect gathers all certificate metrics.
func (c *Collector) Collect() (*CertificateMetrics, error) {
	metrics := &CertificateMetrics{
		CollectedAt: time.Now().UTC(),
		CertManager: c.certManager,
	}

	// Check certificate manager installation and get details
	c.checkCertManagerInstallation(metrics)

	// Collect certificates based on which manager is installed
	var certManagerCerts []Certificate
	var err error

	switch c.certManager {
	case CertManagerCertbot:
		certManagerCerts, err = c.collectCertbotCertificates()
		if err != nil {
			c.logger.Warn("failed to collect certbot certificates", "error", err)
		}
	case CertManagerAcmeSH:
		certManagerCerts, err = c.collectAcmeshCertificates()
		if err != nil {
			c.logger.Warn("failed to collect acme.sh certificates", "error", err)
		}
	}
	metrics.Certificates = append(metrics.Certificates, certManagerCerts...)

	// Collect certificates from HAProxy certs directory
	haproxyCerts, err := c.collectHAProxyCertificates()
	if err != nil {
		c.logger.Warn("failed to collect HAProxy certificates", "error", err)
	}

	// Merge HAProxy certs, avoiding duplicates (by fingerprint)
	existingFingerprints := make(map[string]bool)
	for _, cert := range metrics.Certificates {
		existingFingerprints[cert.Fingerprint] = true
	}
	for _, cert := range haproxyCerts {
		if !existingFingerprints[cert.Fingerprint] {
			metrics.Certificates = append(metrics.Certificates, cert)
			existingFingerprints[cert.Fingerprint] = true
		}
	}

	c.logger.Debug("collected certificates",
		"total", len(metrics.Certificates),
		"cert_manager", c.certManager)

	return metrics, nil
}

// checkCertManagerInstallation checks which cert manager is installed and gets version info.
func (c *Collector) checkCertManagerInstallation(metrics *CertificateMetrics) {
	switch c.certManager {
	case CertManagerCertbot:
		c.checkCertbotInstallation(metrics)
	case CertManagerAcmeSH:
		c.checkAcmeshInstallation(metrics)
	default:
		metrics.CertManager = CertManagerNone
		metrics.AutoRenewalEnabled = false
	}
}

// checkCertbotInstallation checks certbot installation and gets version info.
func (c *Collector) checkCertbotInstallation(metrics *CertificateMetrics) {
	metrics.CertManager = CertManagerCertbot
	metrics.CertManagerHome = c.certbotLive

	// Use detected certbot path, or fall back to "certbot"
	certbotBin := c.certbotPath
	if certbotBin == "" {
		certbotBin = CertbotBinary
	}

	// Get version
	cmd := exec.Command(certbotBin, "--version")
	output, err := cmd.Output()
	if err == nil {
		version := strings.TrimSpace(string(output))
		// certbot outputs: "certbot 2.x.x"
		metrics.CertManagerVersion = version
	}

	// Detect auto-renewal using smart detection
	enabled, method, service := c.detectCertbotRenewal()
	metrics.AutoRenewalEnabled = enabled
	metrics.RenewalMethod = method
	metrics.RenewalService = service

	// Set legacy fields for backwards compatibility
	metrics.AcmeshInstalled = false
	metrics.CronEnabled = metrics.AutoRenewalEnabled
}

// detectCertbotRenewal detects the certbot renewal method and service.
// Returns (enabled, method, serviceName).
func (c *Collector) detectCertbotRenewal() (bool, string, string) {
	// Build list of timers to try - configured timer first, then common names
	timers := []string{}
	if c.configuredTimer != "" {
		timers = append(timers, c.configuredTimer)
	}
	// Common certbot timer names
	timers = append(timers, "certbot.timer", "certbot-renew.timer", "letsencrypt.timer")

	// Try systemd timers
	for _, timer := range timers {
		cmd := exec.Command("systemctl", "is-active", timer)
		output, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(output)) == "active" {
			c.logger.Debug("detected certbot renewal via systemd timer", "timer", timer)
			return true, RenewalMethodSystemd, timer
		}
	}

	// Check root user's crontab for certbot
	cmd := exec.Command("crontab", "-l")
	output, err := cmd.Output()
	if err == nil && strings.Contains(string(output), "certbot") {
		c.logger.Debug("detected certbot renewal via crontab")
		return true, RenewalMethodCron, "crontab"
	}

	// Check /etc/cron.d for certbot
	entries, err := os.ReadDir("/etc/cron.d")
	if err == nil {
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry.Name()), "certbot") {
				cronPath := "/etc/cron.d/" + entry.Name()
				c.logger.Debug("detected certbot renewal via cron.d", "file", cronPath)
				return true, RenewalMethodCron, cronPath
			}
		}
	}

	// Check /etc/cron.daily, /etc/cron.weekly for certbot scripts
	cronDirs := []string{"/etc/cron.daily", "/etc/cron.weekly"}
	for _, dir := range cronDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry.Name()), "certbot") {
				cronPath := dir + "/" + entry.Name()
				c.logger.Debug("detected certbot renewal via cron script", "file", cronPath)
				return true, RenewalMethodCron, cronPath
			}
		}
	}

	c.logger.Debug("no certbot renewal method detected")
	return false, RenewalMethodNone, ""
}

// checkAcmeshInstallation checks if acme.sh is installed and gets version info.
func (c *Collector) checkAcmeshInstallation(metrics *CertificateMetrics) {
	scriptPath := filepath.Join(c.acmeshHome, AcmeshScript)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		metrics.CertManager = CertManagerNone
		metrics.AcmeshInstalled = false
		metrics.RenewalMethod = RenewalMethodNone
		return
	}

	metrics.CertManager = CertManagerAcmeSH
	metrics.CertManagerHome = c.acmeshHome

	// Legacy fields
	metrics.AcmeshInstalled = true
	metrics.AcmeshHome = c.acmeshHome

	// Get version
	cmd := exec.Command(scriptPath, "--version")
	output, err := cmd.Output()
	if err == nil {
		version := strings.TrimSpace(string(output))
		// acme.sh outputs version like "https://github.com/acmesh-official/acme.sh\nv3.0.7"
		lines := strings.Split(version, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "v") {
				metrics.CertManagerVersion = line
				metrics.AcmeshVersion = line
				break
			}
		}
	}

	// Check if cron is enabled (look for acme.sh in crontab)
	cmd = exec.Command("crontab", "-l")
	output, err = cmd.Output()
	if err == nil {
		cronFound := strings.Contains(string(output), "acme.sh")
		metrics.CronEnabled = cronFound
		metrics.AutoRenewalEnabled = cronFound
		if cronFound {
			metrics.RenewalMethod = RenewalMethodCron
			metrics.RenewalService = "crontab"
		} else {
			metrics.RenewalMethod = RenewalMethodNone
		}
	}
}

// collectCertbotCertificates collects certificates from certbot's live directory.
func (c *Collector) collectCertbotCertificates() ([]Certificate, error) {
	var certs []Certificate

	if _, err := os.Stat(c.certbotLive); os.IsNotExist(err) {
		return certs, nil
	}

	// List directories in certbot live (each domain has its own directory)
	entries, err := os.ReadDir(c.certbotLive)
	if err != nil {
		return nil, fmt.Errorf("failed to read certbot live directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip README
		name := entry.Name()
		if name == "README" {
			continue
		}

		// Look for certificate file in domain directory
		domainDir := filepath.Join(c.certbotLive, name)

		// Try fullchain.pem first (preferred), then cert.pem
		certFile := filepath.Join(domainDir, "fullchain.pem")
		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			certFile = filepath.Join(domainDir, "cert.pem")
			if _, err := os.Stat(certFile); os.IsNotExist(err) {
				continue
			}
		}

		cert, err := c.parseCertificateFile(certFile, SourceCertbot)
		if err != nil {
			c.logger.Warn("failed to parse certificate",
				"file", certFile,
				"error", err)
			continue
		}

		cert.Domain = name
		certs = append(certs, cert)
	}

	return certs, nil
}

// collectAcmeshCertificates collects certificates from acme.sh directories.
func (c *Collector) collectAcmeshCertificates() ([]Certificate, error) {
	var certs []Certificate

	if c.acmeshHome == "" {
		return certs, nil
	}

	// Check if acme.sh home exists
	if _, err := os.Stat(c.acmeshHome); os.IsNotExist(err) {
		return certs, nil
	}

	// List directories in acme.sh home (each domain has its own directory)
	entries, err := os.ReadDir(c.acmeshHome)
	if err != nil {
		return nil, fmt.Errorf("failed to read acme.sh home: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip hidden directories and account directory
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "ca" || name == "deploy" {
			continue
		}

		// Look for certificate file in domain directory
		domainDir := filepath.Join(c.acmeshHome, name)
		certFile := filepath.Join(domainDir, name+".cer")

		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			// Try fullchain.cer
			certFile = filepath.Join(domainDir, "fullchain.cer")
			if _, err := os.Stat(certFile); os.IsNotExist(err) {
				continue
			}
		}

		cert, err := c.parseCertificateFile(certFile, SourceAcmeSH)
		if err != nil {
			c.logger.Warn("failed to parse certificate",
				"file", certFile,
				"error", err)
			continue
		}

		cert.Domain = name
		certs = append(certs, cert)
	}

	return certs, nil
}

// collectHAProxyCertificates collects certificates from HAProxy certs directory.
func (c *Collector) collectHAProxyCertificates() ([]Certificate, error) {
	var certs []Certificate

	if _, err := os.Stat(c.haproxyCerts); os.IsNotExist(err) {
		return certs, nil
	}

	// Walk the HAProxy certs directory
	err := filepath.Walk(c.haproxyCerts, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		if info.IsDir() {
			return nil
		}

		// Only process .pem files
		if !strings.HasSuffix(path, ".pem") && !strings.HasSuffix(path, ".crt") {
			return nil
		}

		cert, err := c.parseCertificateFile(path, SourceManual)
		if err != nil {
			c.logger.Debug("failed to parse certificate",
				"file", path,
				"error", err)
			return nil
		}

		// Determine source based on issuer and cert manager
		if IsLetsEncrypt(cert.Issuer) {
			// It's from Let's Encrypt - determine which tool
			switch c.certManager {
			case CertManagerCertbot:
				cert.Source = SourceCertbot
			case CertManagerAcmeSH:
				cert.Source = SourceAcmeSH
			default:
				// Default to certbot for Let's Encrypt certs if manager unknown
				cert.Source = SourceCertbot
			}
		}

		// Extract domain from filename if not set
		if cert.Domain == "" {
			base := filepath.Base(path)
			cert.Domain = strings.TrimSuffix(strings.TrimSuffix(base, ".pem"), ".crt")
		}

		certs = append(certs, cert)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk HAProxy certs directory: %w", err)
	}

	return certs, nil
}

// parseCertificateFile reads and parses a certificate file.
func (c *Collector) parseCertificateFile(path, source string) (Certificate, error) {
	cert := Certificate{
		FilePath: path,
		Source:   source,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cert, fmt.Errorf("failed to read certificate file: %w", err)
	}

	// Parse PEM block
	block, _ := pem.Decode(data)
	if block == nil {
		return cert, fmt.Errorf("failed to decode PEM block")
	}

	// Parse X.509 certificate
	x509Cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return cert, fmt.Errorf("failed to parse X.509 certificate: %w", err)
	}

	// Extract certificate information
	cert.CommonName = x509Cert.Subject.CommonName
	cert.Issuer = x509Cert.Issuer.CommonName
	if cert.Issuer == "" && len(x509Cert.Issuer.Organization) > 0 {
		cert.Issuer = x509Cert.Issuer.Organization[0]
	}
	cert.NotBefore = x509Cert.NotBefore
	cert.NotAfter = x509Cert.NotAfter
	cert.SANs = x509Cert.DNSNames
	cert.SerialNumber = x509Cert.SerialNumber.String()

	// Calculate SHA256 fingerprint
	fingerprint := sha256.Sum256(x509Cert.Raw)
	cert.Fingerprint = hex.EncodeToString(fingerprint[:])

	// Calculate days until expiry
	now := time.Now()
	duration := x509Cert.NotAfter.Sub(now)
	cert.DaysUntilExpiry = int(duration.Hours() / 24)
	cert.IsExpired = now.After(x509Cert.NotAfter)

	// Set domain from CommonName if not already set
	if cert.Domain == "" {
		cert.Domain = cert.CommonName
	}

	// Detect self-signed certificates
	if x509Cert.Issuer.CommonName == x509Cert.Subject.CommonName {
		cert.Source = SourceSelfSigned
	}

	return cert, nil
}

// RefreshCertificate triggers a certificate renewal.
// Uses certbot or acme.sh depending on which is installed.
func (c *Collector) RefreshCertificate(domain string) (*RefreshResult, error) {
	switch c.certManager {
	case CertManagerCertbot:
		return c.refreshCertbotCertificate(domain)
	case CertManagerAcmeSH:
		return c.refreshAcmeshCertificate(domain)
	default:
		return &RefreshResult{
			Domain:  domain,
			Message: "no certificate manager installed",
		}, nil
	}
}

// refreshCertbotCertificate triggers renewal using certbot.
func (c *Collector) refreshCertbotCertificate(domain string) (*RefreshResult, error) {
	result := &RefreshResult{
		Domain: domain,
	}

	// Use detected certbot path, or fall back to "certbot"
	certbotBin := c.certbotPath
	if certbotBin == "" {
		certbotBin = CertbotBinary
	}

	// Check if certbot is available
	if certbotBin == CertbotBinary {
		if _, err := exec.LookPath(CertbotBinary); err != nil {
			result.Message = "certbot is not installed"
			return result, nil
		}
	}

	// Check if certificate exists
	certDir := filepath.Join(c.certbotLive, domain)
	if _, err := os.Stat(certDir); os.IsNotExist(err) {
		result.Message = fmt.Sprintf("certificate for domain %s not found in certbot", domain)
		return result, nil
	}

	var outputBuffer bytes.Buffer

	// Step 1: Renew the certificate
	c.logger.Info("renewing certificate with certbot", "domain", domain)
	cmd := exec.Command(certbotBin, "renew", "--cert-name", domain, "--force-renewal")
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	if err := cmd.Run(); err != nil {
		result.Message = fmt.Sprintf("renewal failed: %s", err)
		result.Output = outputBuffer.String()
		return result, nil
	}
	result.Renewed = true
	outputBuffer.WriteString("\n--- Renewal completed ---\n")

	// Step 2: Run deploy hook if it exists
	if _, err := os.Stat(CertbotDeployHook); err == nil {
		c.logger.Info("running deploy hook")
		cmd = exec.Command(CertbotDeployHook)
		cmd.Stdout = &outputBuffer
		cmd.Stderr = &outputBuffer

		if err := cmd.Run(); err != nil {
			// Deploy hook failed, but renewal succeeded
			outputBuffer.WriteString(fmt.Sprintf("Warning: deploy hook failed: %s\n", err))
		} else {
			result.Installed = true
			outputBuffer.WriteString("\n--- Deploy hook completed ---\n")
		}
	} else {
		// No deploy hook, manually create HAProxy PEM
		c.logger.Info("creating HAProxy certificate manually")
		srcFullchain := filepath.Join(certDir, "fullchain.pem")
		srcKey := filepath.Join(certDir, "privkey.pem")
		destPem := filepath.Join(c.haproxyCerts, domain+".pem")

		certData, err := os.ReadFile(srcFullchain)
		if err != nil {
			result.Message = fmt.Sprintf("failed to read fullchain.pem: %s", err)
			result.Output = outputBuffer.String()
			return result, nil
		}

		keyData, err := os.ReadFile(srcKey)
		if err != nil {
			result.Message = fmt.Sprintf("failed to read privkey.pem: %s", err)
			result.Output = outputBuffer.String()
			return result, nil
		}

		combinedPem := append(certData, keyData...)
		if err := os.WriteFile(destPem, combinedPem, 0600); err != nil {
			result.Message = fmt.Sprintf("failed to write combined PEM: %s", err)
			result.Output = outputBuffer.String()
			return result, nil
		}
		result.Installed = true
		outputBuffer.WriteString(fmt.Sprintf("Installed to %s\n", destPem))
	}

	// Step 3: Reload HAProxy
	c.logger.Info("reloading HAProxy")
	cmd = exec.Command("systemctl", "reload", "haproxy")
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	if err := cmd.Run(); err != nil {
		result.Message = fmt.Sprintf("HAProxy reload failed: %s", err)
		result.Output = outputBuffer.String()
		return result, nil
	}
	result.Reloaded = true
	outputBuffer.WriteString("\n--- HAProxy reloaded ---\n")

	result.Success = true
	result.Message = "Certificate renewed, installed, and HAProxy reloaded successfully"
	result.Output = outputBuffer.String()

	return result, nil
}

// refreshAcmeshCertificate triggers renewal using acme.sh.
func (c *Collector) refreshAcmeshCertificate(domain string) (*RefreshResult, error) {
	result := &RefreshResult{
		Domain: domain,
	}

	scriptPath := filepath.Join(c.acmeshHome, AcmeshScript)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		result.Message = "acme.sh is not installed"
		return result, nil
	}

	// Check if certificate exists
	certDir := filepath.Join(c.acmeshHome, domain)
	if _, err := os.Stat(certDir); os.IsNotExist(err) {
		result.Message = fmt.Sprintf("certificate for domain %s not found in acme.sh", domain)
		return result, nil
	}

	var outputBuffer bytes.Buffer

	// Step 1: Renew the certificate
	c.logger.Info("renewing certificate with acme.sh", "domain", domain)
	cmd := exec.Command(scriptPath, "--renew", "-d", domain, "--force")
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	if err := cmd.Run(); err != nil {
		result.Message = fmt.Sprintf("renewal failed: %s", err)
		result.Output = outputBuffer.String()
		return result, nil
	}
	result.Renewed = true
	outputBuffer.WriteString("\n--- Renewal completed ---\n")

	// Step 2: Install to HAProxy certs directory
	c.logger.Info("installing certificate to HAProxy", "domain", domain)
	srcCert := filepath.Join(certDir, "fullchain.cer")
	srcKey := filepath.Join(certDir, domain+".key")
	destPem := filepath.Join(c.haproxyCerts, domain+".pem")

	// Check if source files exist
	if _, err := os.Stat(srcCert); os.IsNotExist(err) {
		srcCert = filepath.Join(certDir, domain+".cer")
	}

	// Read and combine cert + key into PEM
	certData, err := os.ReadFile(srcCert)
	if err != nil {
		result.Message = fmt.Sprintf("failed to read certificate: %s", err)
		result.Output = outputBuffer.String()
		return result, nil
	}

	keyData, err := os.ReadFile(srcKey)
	if err != nil {
		result.Message = fmt.Sprintf("failed to read key: %s", err)
		result.Output = outputBuffer.String()
		return result, nil
	}

	// HAProxy expects cert + key in a single PEM file
	combinedPem := append(certData, keyData...)
	if err := os.WriteFile(destPem, combinedPem, 0600); err != nil {
		result.Message = fmt.Sprintf("failed to write combined PEM: %s", err)
		result.Output = outputBuffer.String()
		return result, nil
	}
	result.Installed = true
	outputBuffer.WriteString(fmt.Sprintf("Installed to %s\n", destPem))
	outputBuffer.WriteString("\n--- Installation completed ---\n")

	// Step 3: Reload HAProxy
	c.logger.Info("reloading HAProxy")
	cmd = exec.Command("systemctl", "reload", "haproxy")
	cmd.Stdout = &outputBuffer
	cmd.Stderr = &outputBuffer

	if err := cmd.Run(); err != nil {
		result.Message = fmt.Sprintf("HAProxy reload failed: %s", err)
		result.Output = outputBuffer.String()
		return result, nil
	}
	result.Reloaded = true
	outputBuffer.WriteString("\n--- HAProxy reloaded ---\n")

	result.Success = true
	result.Message = "Certificate renewed, installed, and HAProxy reloaded successfully"
	result.Output = outputBuffer.String()

	return result, nil
}

// GetCertManager returns the detected certificate manager type.
func (c *Collector) GetCertManager() string {
	return c.certManager
}

// GetAcmeshHome returns the detected acme.sh home directory.
func (c *Collector) GetAcmeshHome() string {
	return c.acmeshHome
}

// SetHAProxyCertsDir sets the HAProxy certificates directory (for testing).
func (c *Collector) SetHAProxyCertsDir(dir string) {
	c.haproxyCerts = dir
}
