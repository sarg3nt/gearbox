// Package haproxy generates and manages HAProxy configuration.
package haproxy

import (
	"fmt"
	"strings"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/services/compose"
)

// Generator generates HAProxy configuration from backend definitions.
type Generator struct {
	backends []compose.BackendConfig
}

// NewGenerator creates a new HAProxy config generator.
func NewGenerator(backends []compose.BackendConfig) *Generator {
	return &Generator{backends: backends}
}

// GenerateRoutingConfig generates ACLs and use_backend directives for the frontend.
func (g *Generator) GenerateRoutingConfig() string {
	var lines []string

	lines = append(lines, "# Auto-generated routing rules")
	lines = append(lines, fmt.Sprintf("# Generated: %s", time.Now().Format("2006-01-02 15:04:05")))
	lines = append(lines, "")

	// Detect internal traffic (RFC1918 private networks)
	lines = append(lines, "  # Detect external vs internal traffic (RFC1918 private networks)")
	lines = append(lines, "  acl is_internal src 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16")
	lines = append(lines, "")

	// Generate hostname ACLs
	lines = append(lines, "  # Hostname matching")
	for _, backend := range g.backends {
		aclName := hostnameToACLName(backend.Hostname)
		lines = append(lines, fmt.Sprintf("  acl is_%s hdr(host) -i %s", aclName, backend.Hostname))
	}
	lines = append(lines, "")

	// Generate IP allowlist ACLs (if specified)
	ipRestrictedBackends := g.getIPRestrictedBackends()
	if len(ipRestrictedBackends) > 0 {
		lines = append(lines, "  # IP allowlists for restricted services")
		for _, backend := range ipRestrictedBackends {
			aclName := hostnameToACLName(backend.Hostname)
			ipRanges := strings.Split(backend.ACLIP, ",")
			for _, ipRange := range ipRanges {
				ipRange = strings.TrimSpace(ipRange)
				if ipRange != "" {
					lines = append(lines, fmt.Sprintf("  acl ip_allowed_%s src %s", aclName, ipRange))
				}
			}
		}
		lines = append(lines, "")
	}

	// Generate public service ACL
	publicBackends := g.getPublicBackends()
	if len(publicBackends) > 0 {
		lines = append(lines, "  # Public services (accessible externally)")
		var publicHosts []string
		for _, b := range publicBackends {
			publicHosts = append(publicHosts, b.Hostname)
		}
		lines = append(lines, fmt.Sprintf("  acl is_public_service hdr(host) -i %s", strings.Join(publicHosts, " ")))
		lines = append(lines, "")
	}

	// Security rules: Block external access to non-public services
	if len(g.backends) > 0 && len(publicBackends) > 0 {
		lines = append(lines, "  # Block external access to internal-only services")
		lines = append(lines, "  http-request deny deny_status 403 if !is_internal !is_public_service")
		lines = append(lines, "")
	} else if len(g.backends) > 0 && len(publicBackends) == 0 {
		lines = append(lines, "  # Block all external access (no public services defined)")
		lines = append(lines, "  http-request deny deny_status 403 if !is_internal")
		lines = append(lines, "")
	}

	// IP allowlist enforcement
	if len(ipRestrictedBackends) > 0 {
		lines = append(lines, "  # Enforce IP allowlists")
		for _, backend := range ipRestrictedBackends {
			aclName := hostnameToACLName(backend.Hostname)
			lines = append(lines, fmt.Sprintf("  http-request deny deny_status 403 if is_%s !ip_allowed_%s", aclName, aclName))
		}
		lines = append(lines, "")
	}

	// Rate limiting per backend
	lines = append(lines, "  # Rate limiting (per backend)")
	for _, backend := range g.backends {
		aclName := hostnameToACLName(backend.Hostname)
		lines = append(lines, fmt.Sprintf("  http-request deny deny_status 429 if is_%s { sc_http_req_rate(0) gt %s }", aclName, backend.RateLimit))
	}
	lines = append(lines, "")

	// Generate use_backend directives
	lines = append(lines, "  # Backend routing")
	for _, backend := range g.backends {
		aclName := hostnameToACLName(backend.Hostname)
		lines = append(lines, fmt.Sprintf("  use_backend %s if is_%s", backend.BackendName, aclName))
	}

	return strings.Join(lines, "\n")
}

// GenerateBackendConfig generates backend definitions.
func (g *Generator) GenerateBackendConfig() string {
	var lines []string

	lines = append(lines, "# Auto-generated backends")
	lines = append(lines, fmt.Sprintf("# Generated: %s", time.Now().Format("2006-01-02 15:04:05")))
	lines = append(lines, "")

	for _, backend := range g.backends {
		lines = append(lines, fmt.Sprintf("backend %s", backend.BackendName))
		lines = append(lines, fmt.Sprintf("  mode %s", backend.Mode))
		lines = append(lines, fmt.Sprintf("  balance %s", backend.Balance))
		if backend.HTTPKeepAlive && backend.Mode == "http" {
			lines = append(lines, "  option http-keep-alive")
		}

		// Build server options
		var serverOpts []string
		if backend.Check {
			serverOpts = append(serverOpts, "check")
			serverOpts = append(serverOpts, fmt.Sprintf("inter %s", backend.CheckInterval))
			serverOpts = append(serverOpts, fmt.Sprintf("fall %s", backend.CheckFall))
			serverOpts = append(serverOpts, fmt.Sprintf("rise %s", backend.CheckRise))
		}

		// SSL backend options
		if backend.BackendSSL {
			serverOpts = append(serverOpts, "ssl")
			serverOpts = append(serverOpts, fmt.Sprintf("verify %s", backend.BackendSSLVerify))
			serverOpts = append(serverOpts, fmt.Sprintf("sni str(%s)", backend.Hostname))
		}

		serverLine := fmt.Sprintf("  server %s_srv %s", backend.BackendName, backend.Server)
		if len(serverOpts) > 0 {
			serverLine += " " + strings.Join(serverOpts, " ")
		}
		lines = append(lines, serverLine)
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

// getIPRestrictedBackends returns backends that have IP restrictions.
func (g *Generator) getIPRestrictedBackends() []compose.BackendConfig {
	var restricted []compose.BackendConfig
	for _, b := range g.backends {
		if b.ACLIP != "" {
			restricted = append(restricted, b)
		}
	}
	return restricted
}

// getPublicBackends returns backends that are marked as public.
func (g *Generator) getPublicBackends() []compose.BackendConfig {
	var public []compose.BackendConfig
	for _, b := range g.backends {
		if b.Public {
			public = append(public, b)
		}
	}
	return public
}

// hostnameToACLName converts a hostname to a valid HAProxy ACL name.
func hostnameToACLName(hostname string) string {
	name := strings.ReplaceAll(hostname, ".", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name
}
