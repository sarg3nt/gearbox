package haproxy

import (
	"strings"
	"testing"

	"github.com/sarg3nt/gearbox-agent/internal/framework/services/compose"
)

func TestNewGenerator(t *testing.T) {
	backends := []compose.BackendConfig{
		{BackendName: "test_backend", Hostname: "test.example.com"},
	}

	gen := NewGenerator(backends)
	if gen == nil {
		t.Fatal("NewGenerator() returned nil")
	}

	if len(gen.backends) != 1 {
		t.Errorf("NewGenerator() backends count = %d, want 1", len(gen.backends))
	}
}

func TestNewGenerator_EmptyBackends(t *testing.T) {
	gen := NewGenerator(nil)
	if gen == nil {
		t.Fatal("NewGenerator() returned nil for nil backends")
	}

	if gen.backends != nil {
		t.Errorf("NewGenerator() backends should be nil, got %v", gen.backends)
	}
}

func TestHostnameToACLName(t *testing.T) {
	tests := []struct {
		hostname string
		want     string
	}{
		{"example.com", "example_com"},
		{"sub.example.com", "sub_example_com"},
		{"my-app.example.com", "my_app_example_com"},
		{"test", "test"},
		{"a.b.c.d", "a_b_c_d"},
		{"my-long-hostname.sub-domain.example.com", "my_long_hostname_sub_domain_example_com"},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			got := hostnameToACLName(tt.hostname)
			if got != tt.want {
				t.Errorf("hostnameToACLName(%q) = %q, want %q", tt.hostname, got, tt.want)
			}
		})
	}
}

func TestGenerator_GetIPRestrictedBackends(t *testing.T) {
	backends := []compose.BackendConfig{
		{BackendName: "restricted1", ACLIP: "10.0.0.0/8"},
		{BackendName: "open1", ACLIP: ""},
		{BackendName: "restricted2", ACLIP: "192.168.1.0/24"},
		{BackendName: "open2", ACLIP: ""},
	}

	gen := NewGenerator(backends)
	restricted := gen.getIPRestrictedBackends()

	if len(restricted) != 2 {
		t.Errorf("getIPRestrictedBackends() count = %d, want 2", len(restricted))
	}

	// Verify the correct backends are returned
	names := make(map[string]bool)
	for _, b := range restricted {
		names[b.BackendName] = true
	}

	if !names["restricted1"] || !names["restricted2"] {
		t.Errorf("getIPRestrictedBackends() missing expected backends")
	}
}

func TestGenerator_GetPublicBackends(t *testing.T) {
	backends := []compose.BackendConfig{
		{BackendName: "public1", Public: true},
		{BackendName: "internal1", Public: false},
		{BackendName: "public2", Public: true},
		{BackendName: "internal2", Public: false},
	}

	gen := NewGenerator(backends)
	public := gen.getPublicBackends()

	if len(public) != 2 {
		t.Errorf("getPublicBackends() count = %d, want 2", len(public))
	}

	// Verify the correct backends are returned
	names := make(map[string]bool)
	for _, b := range public {
		names[b.BackendName] = true
	}

	if !names["public1"] || !names["public2"] {
		t.Errorf("getPublicBackends() missing expected backends")
	}
}

func TestGenerator_GenerateRoutingConfig_Basic(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName: "app_backend",
			Hostname:    "app.example.com",
			RateLimit:   "100",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateRoutingConfig()

	// Check for required elements
	requiredElements := []string{
		"# Auto-generated routing rules",
		"acl is_internal src 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16",
		"acl is_app_example_com hdr(host) -i app.example.com",
		"http-request deny deny_status 429 if is_app_example_com { sc_http_req_rate(0) gt 100 }",
		"use_backend app_backend if is_app_example_com",
	}

	for _, elem := range requiredElements {
		if !strings.Contains(config, elem) {
			t.Errorf("GenerateRoutingConfig() missing: %q", elem)
		}
	}
}

func TestGenerator_GenerateRoutingConfig_WithPublicBackends(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName: "public_app",
			Hostname:    "public.example.com",
			Public:      true,
			RateLimit:   "100",
		},
		{
			BackendName: "internal_app",
			Hostname:    "internal.example.com",
			Public:      false,
			RateLimit:   "50",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateRoutingConfig()

	// Should have public service ACL
	if !strings.Contains(config, "acl is_public_service hdr(host) -i public.example.com") {
		t.Error("GenerateRoutingConfig() missing public service ACL")
	}

	// Should block external access to non-public services
	if !strings.Contains(config, "http-request deny deny_status 403 if !is_internal !is_public_service") {
		t.Error("GenerateRoutingConfig() missing external access block rule")
	}
}

func TestGenerator_GenerateRoutingConfig_NoPublicBackends(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName: "internal_only",
			Hostname:    "internal.example.com",
			Public:      false,
			RateLimit:   "100",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateRoutingConfig()

	// Should block all external access
	if !strings.Contains(config, "http-request deny deny_status 403 if !is_internal") {
		t.Error("GenerateRoutingConfig() missing external access block for internal-only services")
	}
}

func TestGenerator_GenerateRoutingConfig_IPRestrictions(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName: "restricted_app",
			Hostname:    "restricted.example.com",
			ACLIP:       "10.0.0.0/24, 192.168.1.0/24",
			RateLimit:   "100",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateRoutingConfig()

	// Check IP allowlist ACLs
	if !strings.Contains(config, "acl ip_allowed_restricted_example_com src 10.0.0.0/24") {
		t.Error("GenerateRoutingConfig() missing first IP allowlist ACL")
	}
	if !strings.Contains(config, "acl ip_allowed_restricted_example_com src 192.168.1.0/24") {
		t.Error("GenerateRoutingConfig() missing second IP allowlist ACL")
	}

	// Check IP restriction enforcement
	if !strings.Contains(config, "http-request deny deny_status 403 if is_restricted_example_com !ip_allowed_restricted_example_com") {
		t.Error("GenerateRoutingConfig() missing IP restriction enforcement")
	}
}

func TestGenerator_GenerateRoutingConfig_Empty(t *testing.T) {
	gen := NewGenerator(nil)
	config := gen.GenerateRoutingConfig()

	// Should still have basic structure
	if !strings.Contains(config, "# Auto-generated routing rules") {
		t.Error("GenerateRoutingConfig() missing header for empty backends")
	}

	// Should have internal detection
	if !strings.Contains(config, "acl is_internal") {
		t.Error("GenerateRoutingConfig() missing internal detection for empty backends")
	}
}

func TestGenerator_GenerateBackendConfig_Basic(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName:   "app_backend",
			Hostname:      "app.example.com",
			Server:        "app-server:8080",
			Mode:          "http",
			Balance:       "roundrobin",
			Check:         true,
			CheckInterval: "5s",
			CheckFall:     "3",
			CheckRise:     "2",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateBackendConfig()

	// Check for required elements
	requiredElements := []string{
		"# Auto-generated backends",
		"backend app_backend",
		"mode http",
		"balance roundrobin",
		"server app_backend_srv app-server:8080 check inter 5s fall 3 rise 2",
	}

	for _, elem := range requiredElements {
		if !strings.Contains(config, elem) {
			t.Errorf("GenerateBackendConfig() missing: %q", elem)
		}
	}
}

func TestGenerator_GenerateBackendConfig_WithSSL(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName:      "ssl_backend",
			Hostname:         "secure.example.com",
			Server:           "secure-server:443",
			Mode:             "http",
			Balance:          "roundrobin",
			Check:            true,
			CheckInterval:    "5s",
			CheckFall:        "3",
			CheckRise:        "2",
			BackendSSL:       true,
			BackendSSLVerify: "none",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateBackendConfig()

	// Check SSL options are present
	if !strings.Contains(config, "ssl") {
		t.Error("GenerateBackendConfig() missing ssl option")
	}
	if !strings.Contains(config, "verify none") {
		t.Error("GenerateBackendConfig() missing verify option")
	}
	if !strings.Contains(config, "sni str(secure.example.com)") {
		t.Error("GenerateBackendConfig() missing SNI option")
	}
}

func TestGenerator_GenerateBackendConfig_NoCheck(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName: "no_check_backend",
			Hostname:    "nocheck.example.com",
			Server:      "server:8080",
			Mode:        "http",
			Balance:     "roundrobin",
			Check:       false,
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateBackendConfig()

	// Server line should NOT have check options
	if strings.Contains(config, "check inter") {
		t.Error("GenerateBackendConfig() should not have check options when Check=false")
	}

	// Server line should be simple
	if !strings.Contains(config, "server no_check_backend_srv server:8080") {
		t.Error("GenerateBackendConfig() missing simple server line")
	}
}

func TestGenerator_GenerateBackendConfig_TCPMode(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName:   "tcp_backend",
			Hostname:      "tcp.example.com",
			Server:        "tcp-server:3306",
			Mode:          "tcp",
			Balance:       "leastconn",
			Check:         true,
			CheckInterval: "10s",
			CheckFall:     "2",
			CheckRise:     "3",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateBackendConfig()

	if !strings.Contains(config, "mode tcp") {
		t.Error("GenerateBackendConfig() missing tcp mode")
	}
	if !strings.Contains(config, "balance leastconn") {
		t.Error("GenerateBackendConfig() missing leastconn balance")
	}
}

func TestGenerator_GenerateBackendConfig_Empty(t *testing.T) {
	gen := NewGenerator(nil)
	config := gen.GenerateBackendConfig()

	// Should have header but no backends
	if !strings.Contains(config, "# Auto-generated backends") {
		t.Error("GenerateBackendConfig() missing header for empty backends")
	}

	// Should not have any backend definitions
	if strings.Contains(config, "backend ") {
		t.Error("GenerateBackendConfig() should not have backend definitions when empty")
	}
}

func TestGenerator_GenerateBackendConfig_MultipleBackends(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName:   "backend1",
			Hostname:      "app1.example.com",
			Server:        "server1:8080",
			Mode:          "http",
			Balance:       "roundrobin",
			Check:         true,
			CheckInterval: "5s",
			CheckFall:     "3",
			CheckRise:     "2",
		},
		{
			BackendName:   "backend2",
			Hostname:      "app2.example.com",
			Server:        "server2:9090",
			Mode:          "http",
			Balance:       "source",
			Check:         true,
			CheckInterval: "10s",
			CheckFall:     "2",
			CheckRise:     "4",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateBackendConfig()

	// Both backends should be present
	if !strings.Contains(config, "backend backend1") {
		t.Error("GenerateBackendConfig() missing backend1")
	}
	if !strings.Contains(config, "backend backend2") {
		t.Error("GenerateBackendConfig() missing backend2")
	}

	// Each backend should have its own server line
	if !strings.Contains(config, "server backend1_srv server1:8080") {
		t.Error("GenerateBackendConfig() missing server1 definition")
	}
	if !strings.Contains(config, "server backend2_srv server2:9090") {
		t.Error("GenerateBackendConfig() missing server2 definition")
	}

	// Check different balance methods
	if strings.Count(config, "balance roundrobin") != 1 {
		t.Error("GenerateBackendConfig() should have one roundrobin balance")
	}
	if strings.Count(config, "balance source") != 1 {
		t.Error("GenerateBackendConfig() should have one source balance")
	}
}

func TestGenerator_GenerateRoutingConfig_MultiplePublicBackends(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName: "public1",
			Hostname:    "public1.example.com",
			Public:      true,
			RateLimit:   "100",
		},
		{
			BackendName: "public2",
			Hostname:    "public2.example.com",
			Public:      true,
			RateLimit:   "200",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateRoutingConfig()

	// Both hostnames should be in the public service ACL
	if !strings.Contains(config, "acl is_public_service hdr(host) -i public1.example.com public2.example.com") {
		t.Error("GenerateRoutingConfig() should combine multiple public hostnames in single ACL")
	}
}

func TestGenerator_GenerateBackendConfig_SSLWithRequiredVerify(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName:      "verified_ssl",
			Hostname:         "verified.example.com",
			Server:           "verified-server:443",
			Mode:             "http",
			Balance:          "roundrobin",
			BackendSSL:       true,
			BackendSSLVerify: "required",
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateBackendConfig()

	if !strings.Contains(config, "verify required") {
		t.Error("GenerateBackendConfig() missing 'verify required' option")
	}
}

func TestGenerator_GenerateBackendConfig_HTTPKeepAlive(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName:   "keepalive_backend",
			Hostname:      "idrac.example.com",
			Server:        "10.0.0.6:443",
			Mode:          "http",
			Balance:       "roundrobin",
			HTTPKeepAlive: true,
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateBackendConfig()

	if !strings.Contains(config, "option http-keep-alive") {
		t.Error("GenerateBackendConfig() missing 'option http-keep-alive' directive")
	}
}

func TestGenerator_GenerateBackendConfig_HTTPKeepAlive_TCPModeSkipped(t *testing.T) {
	backends := []compose.BackendConfig{
		{
			BackendName:   "keepalive_tcp_backend",
			Hostname:      "tcp.example.com",
			Server:        "10.0.0.7:3306",
			Mode:          "tcp",
			Balance:       "roundrobin",
			HTTPKeepAlive: true,
		},
	}

	gen := NewGenerator(backends)
	config := gen.GenerateBackendConfig()

	if strings.Contains(config, "option http-keep-alive") {
		t.Error("GenerateBackendConfig() emitted 'option http-keep-alive' for tcp-mode backend")
	}
}
