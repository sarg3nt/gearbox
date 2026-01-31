package dashboard

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Importer handles importing dashboards from YAML.
type Importer struct{}

// NewImporter creates a new Importer.
func NewImporter() *Importer {
	return &Importer{}
}

// Import reads a dashboard from a YAML reader.
func (i *Importer) Import(r io.Reader) (*Dashboard, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read dashboard file: %w", err)
	}

	return i.ImportFromBytes(data)
}

// ImportFromBytes parses dashboard YAML from bytes.
func (i *Importer) ImportFromBytes(data []byte) (*Dashboard, error) {
	var dashboard Dashboard
	if err := yaml.Unmarshal(data, &dashboard); err != nil {
		return nil, fmt.Errorf("failed to parse dashboard YAML: %w", err)
	}

	// Validate the imported dashboard
	validator := NewValidator()
	if err := validator.Validate(&dashboard); err != nil {
		return nil, fmt.Errorf("dashboard validation failed: %w", err)
	}

	return &dashboard, nil
}

// ImportFromString parses dashboard YAML from a string.
func (i *Importer) ImportFromString(yamlStr string) (*Dashboard, error) {
	return i.ImportFromBytes([]byte(yamlStr))
}
