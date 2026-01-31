// Package discovery provides service auto-discovery for the gearbox-agent.
// It detects installed services and enables appropriate monitoring plugins.
package discovery

import (
	"context"
)

// Capability represents a detected capability on the system.
type Capability struct {
	Name        string `json:"name"`        // Capability identifier (e.g., "haproxy", "docker")
	DisplayName string `json:"displayName"` // Human-readable name
	Detected    bool   `json:"detected"`    // Whether the capability was detected
	Version     string `json:"version"`     // Version if detected
	Details     string `json:"details"`     // Additional details about detection
}

// Detector is the interface that all service detectors must implement.
type Detector interface {
	// Name returns the capability name (e.g., "haproxy", "docker").
	Name() string

	// Detect checks if the service is installed and running.
	// Returns a Capability struct with detection results.
	Detect(ctx context.Context) (Capability, error)
}

// Registry holds all registered detectors.
type Registry struct {
	detectors []Detector
}

// NewRegistry creates a new detector registry.
func NewRegistry() *Registry {
	return &Registry{
		detectors: []Detector{},
	}
}

// Register adds a detector to the registry.
func (r *Registry) Register(d Detector) {
	r.detectors = append(r.detectors, d)
}

// DetectAll runs all registered detectors and returns capabilities.
func (r *Registry) DetectAll(ctx context.Context) ([]Capability, error) {
	capabilities := make([]Capability, 0, len(r.detectors))

	for _, detector := range r.detectors {
		cap, err := detector.Detect(ctx)
		if err != nil {
			// Log error but continue with other detectors
			cap = Capability{
				Name:        detector.Name(),
				DisplayName: detector.Name(),
				Detected:    false,
				Details:     "Error: " + err.Error(),
			}
		}
		capabilities = append(capabilities, cap)
	}

	return capabilities, nil
}
