package dashboard

import (
	"fmt"
	"log/slog"
	"path/filepath"
)

// Resetter handles resetting plugin dashboards to their defaults.
type Resetter struct {
	storage *Storage
}

// NewResetter creates a new Resetter.
func NewResetter() *Resetter {
	storage, _ := NewStorage(filepath.Join("data", "dashboards"), slog.Default())
	return &Resetter{
		storage: storage,
	}
}

// Reset resets a dashboard to its plugin-provided default.
// This is only valid for plugin-provided dashboards.
func (r *Resetter) Reset(slug string, pluginName string) error {
	if pluginName == "" {
		return fmt.Errorf("cannot reset: not a plugin dashboard")
	}

	// Load the current dashboard to verify it exists and is a plugin dashboard
	current, err := r.storage.Load(slug)
	if err != nil {
		return fmt.Errorf("failed to load dashboard: %w", err)
	}

	if current.Editable {
		return fmt.Errorf("cannot reset user-created dashboard")
	}

	if current.PluginName == nil || *current.PluginName != pluginName {
		return fmt.Errorf("plugin name mismatch")
	}

	// For now, we'll just note that the reset would require reloading from the plugin.
	// In a full implementation, we'd:
	// 1. Look up the plugin by name
	// 2. Call plugin.PredefinedDashboards() to get the default
	// 3. Find the matching dashboard by slug
	// 4. Save it back to disk

	// TODO: Implement plugin lookup and dashboard reloading
	return fmt.Errorf("reset functionality requires plugin registry integration")
}

// ResetAll resets all plugin dashboards to their defaults.
func (r *Resetter) ResetAll() error {
	dashboards, err := r.storage.List()
	if err != nil {
		return fmt.Errorf("failed to list dashboards: %w", err)
	}

	var errors []error
	for _, meta := range dashboards {
		dash, err := r.storage.Load(meta.Slug)
		if err != nil {
			continue
		}
		if !dash.Editable && dash.PluginName != nil && *dash.PluginName != "" {
			if err := r.Reset(meta.Slug, *dash.PluginName); err != nil {
				errors = append(errors, fmt.Errorf("failed to reset %s: %w", meta.Slug, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("reset errors: %v", errors)
	}

	return nil
}
