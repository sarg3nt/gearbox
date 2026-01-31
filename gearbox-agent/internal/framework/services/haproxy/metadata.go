package haproxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/services/compose"
)

// Metadata represents the exported metadata for the monitoring application.
type Metadata struct {
	Version     string             `json:"version"`
	GeneratedAt string             `json:"generated_at"`
	Frontends   []FrontendMetadata `json:"frontends"`
	Backends    []BackendMetadata  `json:"backends"`
}

// FrontendMetadata represents metadata for a frontend.
type FrontendMetadata struct {
	Name     string   `json:"name"`
	Backends []string `json:"backends"`
}

// BackendMetadata represents metadata for a backend.
type BackendMetadata struct {
	Name       string              `json:"name"`
	AppName    string              `json:"app_name"`
	Hostname   string              `json:"hostname"`
	Public     bool                `json:"public"`
	RateLimit  int                 `json:"rate_limit"`
	ACLs       []ACLMetadata       `json:"acls"`
	Servers    []string            `json:"servers"`
	Containers []ContainerMetadata `json:"containers"`
	Frontend   string              `json:"frontend"`
	SSLEnabled bool                `json:"ssl_enabled,omitempty"`
	SSLVerify  string              `json:"ssl_verify,omitempty"`
}

// ACLMetadata represents an ACL in the metadata.
type ACLMetadata struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// ContainerMetadata represents a container in the metadata.
type ContainerMetadata struct {
	Name        string   `json:"name"`
	IsBackend   bool     `json:"is_backend"`
	NetworkMode string   `json:"network_mode,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
}

// ExportMetadata generates metadata from backend configurations.
func ExportMetadata(backends []compose.BackendConfig) *Metadata {
	// Build frontend-to-backend mapping
	frontendBackends := map[string][]string{
		"https_front":    {},
		"plex_tcp_front": {"plex_tcp_backend"}, // Manual TCP frontend for Plex
	}

	var backendsMeta []BackendMetadata

	for _, backend := range backends {
		rateLimit, _ := strconv.Atoi(backend.RateLimit)

		meta := BackendMetadata{
			Name:      backend.BackendName,
			AppName:   backend.AppName,
			Hostname:  backend.Hostname,
			Public:    backend.Public,
			RateLimit: rateLimit,
			ACLs:      []ACLMetadata{},
			Servers:   []string{backend.Server},
			Frontend:  "https_front",
		}

		// Convert containers
		for _, c := range backend.Containers {
			meta.Containers = append(meta.Containers, ContainerMetadata{
				Name:        c.Name,
				IsBackend:   c.IsBackend,
				NetworkMode: c.NetworkMode,
				DependsOn:   c.DependsOn,
			})
		}

		// Add IP ACLs
		if backend.ACLIP != "" {
			meta.ACLs = append(meta.ACLs, ACLMetadata{
				Type:  "ip",
				Value: backend.ACLIP,
			})
		}

		// Add SSL info
		if backend.BackendSSL {
			meta.SSLEnabled = true
			meta.SSLVerify = backend.BackendSSLVerify
		}

		backendsMeta = append(backendsMeta, meta)

		// Track in frontend mapping
		frontendBackends["https_front"] = append(frontendBackends["https_front"], backend.BackendName)
	}

	// Build frontends list
	var frontendsMeta []FrontendMetadata
	for name, backends := range frontendBackends {
		sort.Strings(backends)
		frontendsMeta = append(frontendsMeta, FrontendMetadata{
			Name:     name,
			Backends: backends,
		})
	}

	return &Metadata{
		Version:     "1.1",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Frontends:   frontendsMeta,
		Backends:    backendsMeta,
	}
}

// WriteMetadataFile writes metadata to a JSON file.
func WriteMetadataFile(metadata *Metadata, filePath string) error {
	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
