package parser

import (
	"encoding/json"
	"fmt"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

// ParseMetadataJSON parses the metadata JSON exported by haproxy-autoconfig.
func ParseMetadataJSON(jsonData string) (*models.Metadata, error) {
	var metadata models.Metadata

	if err := json.Unmarshal([]byte(jsonData), &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata JSON: %w", err)
	}

	return &metadata, nil
}
