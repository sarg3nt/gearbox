package dashboard

import (
	"gopkg.in/yaml.v3"
)

// Exporter handles exporting dashboards to YAML.
type Exporter struct{}

// NewExporter creates a new Exporter.
func NewExporter() *Exporter {
	return &Exporter{}
}

// Export converts a Dashboard to YAML bytes.
func (e *Exporter) Export(dashboard *Dashboard) ([]byte, error) {
	return yaml.Marshal(dashboard)
}

// ExportToString converts a Dashboard to a YAML string.
func (e *Exporter) ExportToString(dashboard *Dashboard) (string, error) {
	data, err := e.Export(dashboard)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
