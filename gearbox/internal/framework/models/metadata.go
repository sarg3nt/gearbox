package models

import "time"

// Metadata represents the HAProxy configuration metadata exported by haproxy-autoconfig.
type Metadata struct {
	Version     string             `json:"version"`
	GeneratedAt time.Time          `json:"generated_at"`
	Frontends   []FrontendMetadata `json:"frontends"`
	Backends    []BackendMetadata  `json:"backends"`
}

// FrontendMetadata represents metadata for a frontend with its backend mappings.
type FrontendMetadata struct {
	Name     string   `json:"name"`
	Backends []string `json:"backends"`
}

// Container represents a single container in a docker-compose stack.
type Container struct {
	Name        string   `json:"name"`
	IsBackend   bool     `json:"is_backend"`
	NetworkMode string   `json:"network_mode"`
	DependsOn   []string `json:"depends_on"`
}

// BackendMetadata represents metadata for a single backend.
type BackendMetadata struct {
	Name       string            `json:"name"`
	AppName    string            `json:"app_name"`
	Hostname   string            `json:"hostname"`
	Servers    []string          `json:"servers"`
	Public     bool              `json:"public"`
	RateLimit  int               `json:"rate_limit,omitempty"`
	Frontend   string            `json:"frontend"`
	Containers []Container       `json:"containers"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// GetMetadataForBackend finds metadata for a specific backend by name.
func (m *Metadata) GetMetadataForBackend(backendName string) *BackendMetadata {
	for i := range m.Backends {
		if m.Backends[i].Name == backendName {
			return &m.Backends[i]
		}
	}
	return nil
}

// GetMetadataForFrontend finds metadata for a specific frontend by name.
func (m *Metadata) GetMetadataForFrontend(frontendName string) *FrontendMetadata {
	for i := range m.Frontends {
		if m.Frontends[i].Name == frontendName {
			return &m.Frontends[i]
		}
	}
	return nil
}

// GetBackendsForFrontend returns all backend names that route through a given frontend.
func (m *Metadata) GetBackendsForFrontend(frontendName string) []string {
	if fm := m.GetMetadataForFrontend(frontendName); fm != nil {
		return fm.Backends
	}
	return nil
}

// GetFrontendForBackend returns the frontend name that a backend routes through.
func (m *Metadata) GetFrontendForBackend(backendName string) string {
	if bm := m.GetMetadataForBackend(backendName); bm != nil {
		return bm.Frontend
	}
	return ""
}

// GetServerAddress returns the first server address for a backend.
func (bm *BackendMetadata) GetServerAddress() string {
	if len(bm.Servers) > 0 {
		return bm.Servers[0]
	}
	return ""
}
