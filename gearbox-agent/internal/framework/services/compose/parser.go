// Package compose parses Docker Compose files for HAProxy configuration.
package compose

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// LabelPrefix is the prefix for HAProxy-related labels.
	LabelPrefix = "haproxy."
	// HardwareServicesFile is the name of the hardware services file.
	HardwareServicesFile = "hardware-services.yml"
)

// Validation patterns for user-provided values.
var (
	// validHostname matches valid DNS hostnames (RFC 1123).
	validHostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	// validServerAddr matches host:port format where host is hostname or IP.
	validServerAddr = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.\-]*:\d{1,5}$`)
	// validRateLimit matches numeric rate limits (positive integers only).
	validRateLimit = regexp.MustCompile(`^\d{1,6}$`)
	// validDuration matches HAProxy-style duration strings (e.g., "5s", "100ms", "1m").
	validDuration = regexp.MustCompile(`^\d{1,6}(ms|s|m|h)?$`)
	// validInteger matches positive integers for check fall/rise.
	validInteger = regexp.MustCompile(`^\d{1,3}$`)
	// validMode matches valid HAProxy modes.
	validMode = regexp.MustCompile(`^(http|tcp)$`)
	// validBalance matches valid HAProxy balance algorithms.
	validBalance = regexp.MustCompile(`^(roundrobin|leastconn|source|uri|hdr|first|random)$`)
	// validSSLVerify matches valid SSL verify options.
	validSSLVerify = regexp.MustCompile(`^(none|required)$`)
	// validBackendName matches HAProxy backend names. HAProxy itself accepts a
	// broader character set, but we restrict to DNS-label-style to prevent
	// newline / directive injection from a Docker Compose label into the
	// generated haproxy.cfg (see 2026-05 security audit, P0-1).
	validBackendName = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.\-]{0,62}$`)
	// validACLPath matches a URL path component for an HAProxy path-based
	// ACL: leading `/`, then a safe URL-path character set. Used for the
	// `haproxy.acl.path` label, which is currently parsed into BackendConfig
	// but not yet wired into the generator. Validating now closes the
	// latent injection class — if a future PR wires this into a generated
	// `acl req_path path_beg %s` directive, no flag/newline can slip
	// through. See 2026-05 security audit, P2-10.
	validACLPath = regexp.MustCompile(`^/[a-zA-Z0-9/_.\-~%]*$`)
	// validACLHeader matches an HTTP header name (RFC 7230 token chars).
	// Same forward-looking rationale as validACLPath: parsed but not yet
	// generated, validated now.
	validACLHeader = regexp.MustCompile(`^[a-zA-Z0-9!#$%&'*+\-.^_` + "`" + `|~]+$`)
)

// validateACLIPList validates a comma-separated list of IPs/CIDRs against
// net.ParseCIDR / net.ParseIP. Returns the original string if all entries parse,
// or an empty string + false on the first invalid entry. Prevents newline or
// HAProxy directive injection via the haproxy.acl.ip label (2026-05 audit P0-2).
//
// The user-supplied form for this label is a comma-separated mix of single IPs
// (10.0.0.1) and CIDRs (10.0.0.0/24). Anything that doesn't parse as one of
// those is rejected — including embedded newlines, whitespace beyond simple
// trimming, or HAProxy keywords.
func validateACLIPList(raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	for _, part := range strings.Split(raw, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		if strings.ContainsAny(entry, " \t\n\r") {
			return "", false
		}
		if _, _, err := net.ParseCIDR(entry); err == nil {
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			continue
		}
		return "", false
	}
	return raw, true
}

// Parser parses Docker Compose files for HAProxy backend configuration.
type Parser struct {
	appsFolder string
	logger     *slog.Logger
}

// NewParser creates a new Docker Compose parser.
func NewParser(appsFolder string, logger *slog.Logger) *Parser {
	return &Parser{
		appsFolder: appsFolder,
		logger:     logger,
	}
}

// BackendConfig holds the extracted HAProxy backend configuration.
type BackendConfig struct {
	BackendName      string      `json:"backend_name"`
	AppName          string      `json:"app_name"`
	Hostname         string      `json:"hostname"`
	Server           string      `json:"server"`
	Containers       []Container `json:"containers"`
	Mode             string      `json:"mode"`
	Balance          string      `json:"balance"`
	Check            bool        `json:"check"`
	CheckInterval    string      `json:"check_interval"`
	CheckFall        string      `json:"check_fall"`
	CheckRise        string      `json:"check_rise"`
	SSLRedirect      bool        `json:"ssl_redirect"`
	ACLPath          string      `json:"acl_path,omitempty"`
	ACLHeader        string      `json:"acl_header,omitempty"`
	Public           bool        `json:"public"`
	RateLimit        string      `json:"rate_limit"`
	ACLIP            string      `json:"acl_ip,omitempty"`
	BackendSSL       bool        `json:"backend_ssl"`
	BackendSSLVerify string      `json:"backend_ssl_verify"`
}

// Container represents a container in a Docker Compose service.
type Container struct {
	Name        string   `json:"name"`
	IsBackend   bool     `json:"is_backend"`
	NetworkMode string   `json:"network_mode"`
	DependsOn   []string `json:"depends_on"`
}

// ComposeFile represents the structure of a docker-compose.yml file.
type ComposeFile struct {
	Services map[string]Service `yaml:"services"`
}

// Service represents a service in a docker-compose.yml file.
type Service struct {
	Labels      any            `yaml:"labels"`
	NetworkMode string                 `yaml:"network_mode"`
	DependsOn   any            `yaml:"depends_on"`
	Other       map[string]any `yaml:",inline"`
}

// FindComposeFiles finds all docker-compose.yml files in the apps folder.
func (p *Parser) FindComposeFiles() ([]string, error) {
	var files []string

	if _, err := os.Stat(p.appsFolder); os.IsNotExist(err) {
		p.logger.Info("Apps folder not found", "path", p.appsFolder)
		return files, nil
	}

	entries, err := os.ReadDir(p.appsFolder)
	if err != nil {
		return nil, fmt.Errorf("failed to read apps folder: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		composePath := filepath.Join(p.appsFolder, entry.Name(), "docker-compose.yml")
		if _, err := os.Stat(composePath); err == nil {
			files = append(files, composePath)
		}
	}

	p.logger.Info("Found docker-compose.yml files", "count", len(files))
	return files, nil
}

// FindHardwareServicesFile finds the hardware-services.yml file if it exists.
func (p *Parser) FindHardwareServicesFile() string {
	path := filepath.Join(p.appsFolder, HardwareServicesFile)
	if _, err := os.Stat(path); err == nil {
		p.logger.Info("Found hardware services file", "path", path)
		return path
	}
	return ""
}

// ParseFile parses a docker-compose or hardware-services file.
func (p *Parser) ParseFile(filePath string) ([]BackendConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if compose.Services == nil {
		p.logger.Info("No services found", "file", filePath)
		return nil, nil
	}

	var backends []BackendConfig

	// Determine app name
	appName := filepath.Base(filepath.Dir(filePath))
	if filepath.Base(filePath) == HardwareServicesFile {
		appName = "hardware"
	}

	for serviceName, service := range compose.Services {
		labels := p.parseLabels(service.Labels)

		// Check if HAProxy is enabled for this service
		if labels[LabelPrefix+"enable"] != "true" {
			continue
		}

		backend := p.extractBackendConfig(labels, appName, serviceName, compose.Services)
		if backend != nil {
			backends = append(backends, *backend)
			p.logger.Info("Found HAProxy backend",
				"backend", backend.BackendName,
				"file", filepath.Base(filePath),
			)
		}
	}

	return backends, nil
}

// parseLabels converts various label formats to a map.
func (p *Parser) parseLabels(labels any) map[string]string {
	result := make(map[string]string)

	switch v := labels.(type) {
	case map[string]any:
		for key, val := range v {
			if s, ok := val.(string); ok {
				result[key] = s
			}
		}
	case []any:
		// List format: ["key=value", "key2=value2"]
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts := strings.SplitN(s, "=", 2)
				if len(parts) == 2 {
					result[parts[0]] = parts[1]
				}
			}
		}
	}

	return result
}

// extractBackendConfig extracts HAProxy backend configuration from labels.
func (p *Parser) extractBackendConfig(labels map[string]string, appName, serviceName string, allServices map[string]Service) *BackendConfig {
	hostname := labels[LabelPrefix+"hostname"]
	server := labels[LabelPrefix+"backend.server"]

	if hostname == "" || server == "" {
		p.logger.Warn("Missing required labels",
			"app", appName,
			"service", serviceName,
		)
		return nil
	}

	// Validate hostname format to prevent injection attacks
	if !validHostname.MatchString(hostname) {
		p.logger.Warn("Invalid hostname format",
			"app", appName,
			"service", serviceName,
			"hostname", hostname,
		)
		return nil
	}

	// Validate server address format (host:port)
	if !validServerAddr.MatchString(server) {
		p.logger.Warn("Invalid server address format",
			"app", appName,
			"service", serviceName,
			"server", server,
		)
		return nil
	}

	backendName := labels[LabelPrefix+"backend.name"]
	if backendName == "" {
		backendName = fmt.Sprintf("%s_%s_backend", sanitizeName(appName), sanitizeName(serviceName))
	}

	// Reject any user-supplied backend name that would let an attacker inject
	// HAProxy directives (newlines, spaces, etc.) into the generated config.
	// The default form built from sanitizeName always passes this regex; the
	// check exists to gate values that come from the haproxy.backend.name
	// label. See 2026-05 security audit, P0-1.
	if !validBackendName.MatchString(backendName) {
		p.logger.Warn("Invalid backend name; rejecting backend",
			"app", appName,
			"service", serviceName,
			"backend_name", backendName,
		)
		return nil
	}

	// haproxy.acl.ip flows into the `acl ip_allowed_* src <list>` directive in
	// the generated config. An unvalidated value can inject additional ACL
	// rules via embedded newlines (2026-05 audit P0-2). Validate every comma-
	// separated entry as an IP or CIDR before accepting; reject the whole
	// backend on a malformed entry rather than silently dropping the ACL.
	aclIP, aclIPOK := validateACLIPList(labels[LabelPrefix+"acl.ip"])
	if !aclIPOK {
		p.logger.Warn("Invalid haproxy.acl.ip value; rejecting backend",
			"app", appName,
			"service", serviceName,
		)
		return nil
	}

	// 2026-05 audit P2-10: haproxy.acl.path and haproxy.acl.header are
	// documented labels (see ubuntu-ha-proxy-install/docs/) that gearbox-
	// agent parses today but does NOT yet emit into the generated config.
	// Validate them here so the latent injection class is closed BEFORE
	// any future PR wires them into a directive like
	// `acl req_path path_beg <path>` or `acl req_hdr_cnt(<header>) gt 0`.
	// Empty is accepted (the common case); non-empty must match the
	// allow-list pattern, otherwise the backend is rejected.
	aclPath := labels[LabelPrefix+"acl.path"]
	if aclPath != "" && !validACLPath.MatchString(aclPath) {
		p.logger.Warn("Invalid haproxy.acl.path value; rejecting backend",
			"app", appName,
			"service", serviceName,
		)
		return nil
	}
	aclHeader := labels[LabelPrefix+"acl.header"]
	if aclHeader != "" && !validACLHeader.MatchString(aclHeader) {
		p.logger.Warn("Invalid haproxy.acl.header value; rejecting backend",
			"app", appName,
			"service", serviceName,
		)
		return nil
	}

	// Get and validate configurable values with safe defaults
	mode := getValidatedOrDefault(labels, LabelPrefix+"backend.mode", "http", validMode)
	balance := getValidatedOrDefault(labels, LabelPrefix+"backend.balance", "roundrobin", validBalance)
	checkInterval := getValidatedOrDefault(labels, LabelPrefix+"backend.check.interval", "5s", validDuration)
	checkFall := getValidatedOrDefault(labels, LabelPrefix+"backend.check.fall", "3", validInteger)
	checkRise := getValidatedOrDefault(labels, LabelPrefix+"backend.check.rise", "2", validInteger)
	rateLimit := getValidatedOrDefault(labels, LabelPrefix+"rate_limit", "1000", validRateLimit)
	sslVerify := getValidatedOrDefault(labels, LabelPrefix+"backend.ssl.verify", "none", validSSLVerify)

	config := &BackendConfig{
		BackendName:      backendName,
		AppName:          appName,
		Hostname:         hostname,
		Server:           server,
		Containers:       p.extractContainers(appName, serviceName, allServices),
		Mode:             mode,
		Balance:          balance,
		Check:            labels[LabelPrefix+"backend.check"] != "false",
		CheckInterval:    checkInterval,
		CheckFall:        checkFall,
		CheckRise:        checkRise,
		SSLRedirect:      labels[LabelPrefix+"ssl.redirect"] != "false",
		ACLPath:          aclPath,
		ACLHeader:        aclHeader,
		Public:           labels[LabelPrefix+"public"] == "true",
		RateLimit:        rateLimit,
		ACLIP:            aclIP,
		BackendSSL:       labels[LabelPrefix+"backend.ssl"] == "true",
		BackendSSLVerify: sslVerify,
	}

	return config
}

// extractContainers extracts all containers from the compose file.
func (p *Parser) extractContainers(_, backendServiceName string, allServices map[string]Service) []Container {
	var containers []Container

	for name, service := range allServices {
		container := Container{
			Name:        name,
			IsBackend:   name == backendServiceName,
			NetworkMode: service.NetworkMode,
			DependsOn:   p.parseDependsOn(service.DependsOn),
		}
		containers = append(containers, container)
	}

	return containers
}

// parseDependsOn parses the depends_on field which can be a list or map.
func (p *Parser) parseDependsOn(dependsOn any) []string {
	var result []string

	switch v := dependsOn.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
	case map[string]any:
		for key := range v {
			result = append(result, key)
		}
	}

	return result
}

// sanitizeName sanitizes a name for use in HAProxy backend names.
func sanitizeName(name string) string {
	// Replace invalid characters with underscore
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
	sanitized := re.ReplaceAllString(name, "_")
	// Replace multiple consecutive underscores
	re = regexp.MustCompile(`_+`)
	sanitized = re.ReplaceAllString(sanitized, "_")
	// Remove leading/trailing underscores
	return strings.Trim(sanitized, "_")
}

// getOrDefault returns the value for a key or a default if not present.
func getOrDefault(m map[string]string, key, defaultValue string) string {
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return defaultValue
}

// getValidatedOrDefault returns the value for a key if it matches the pattern,
// otherwise returns the default. This prevents injection of malicious values.
func getValidatedOrDefault(m map[string]string, key, defaultValue string, pattern *regexp.Regexp) string {
	if v, ok := m[key]; ok && v != "" {
		if pattern.MatchString(v) {
			return v
		}
		// Invalid value, fall through to default
	}
	return defaultValue
}
