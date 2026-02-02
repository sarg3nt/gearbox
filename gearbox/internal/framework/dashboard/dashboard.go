// Package dashboard provides the core types and interfaces for the dashboard system.
package dashboard

import (
	"encoding/json"
	"time"
)

// Dashboard represents a complete dashboard configuration.
type Dashboard struct {
	// Version of the dashboard specification
	Version string `yaml:"version"`

	// Name is the display name of the dashboard
	Name string `yaml:"name"`

	// Description explains what the dashboard shows
	Description string `yaml:"description"`

	// CreatedBy indicates if this is a user or plugin dashboard
	// Values: "user" or "plugin:<plugin-name>"
	CreatedBy string `yaml:"created_by"`

	// PluginName is the name of the plugin that provided this dashboard (null for user dashboards)
	PluginName *string `yaml:"plugin_name"`

	// Editable indicates if users can modify this dashboard
	// Plugin-provided dashboards are typically not editable
	Editable bool `yaml:"editable"`

	// Layout defines the dashboard's grid system
	Layout Layout `yaml:"layout"`

	// Widgets contains all widgets in the dashboard
	Widgets []Widget `yaml:"widgets"`

	// Metadata for internal use (not in YAML)
	Slug      string    `yaml:"-"`
	FilePath  string    `yaml:"-"`
	CreatedAt time.Time `yaml:"-"`
	UpdatedAt time.Time `yaml:"-"`
}

// Layout defines the dashboard's grid system.
type Layout struct {
	// Columns is the number of columns in the grid (1-12)
	Columns int `yaml:"columns"`

	// Gap is the spacing between widgets (Tailwind spacing units)
	Gap int `yaml:"gap"`
}

// Widget represents a single widget instance in a dashboard.
type Widget struct {
	// ID is a unique identifier for this widget instance
	ID string `yaml:"id" json:"id"`

	// Type identifies the widget type (e.g., "status-card", "line-chart")
	Type string `yaml:"type" json:"type"`

	// Position defines where the widget is placed in the grid
	Position WidgetPosition `yaml:"position" json:"position"`

	// Config contains widget-specific configuration
	Config map[string]interface{} `yaml:"config" json:"config"`
}

// WidgetPosition defines a widget's position in the grid.
type WidgetPosition struct {
	// Row is the row number (1-based)
	Row int `yaml:"row" json:"row"`

	// Column is the starting column (1-based)
	Column int `yaml:"column" json:"column"`

	// Width is the number of columns the widget spans
	Width int `yaml:"width" json:"width"`

	// Height is the widget height in pixels (or "auto")
	// Store as interface{} to support both int and string
	Height interface{} `yaml:"height" json:"height"`
}

// MarshalJSON customizes JSON marshaling for WidgetPosition to handle the Height field properly.
func (wp WidgetPosition) MarshalJSON() ([]byte, error) {
	type Alias WidgetPosition
	height := wp.Height

	// If height is nil, use "auto" as default
	if height == nil {
		height = "auto"
	}

	return json.Marshal(&struct {
		*Alias
		Height interface{} `json:"height"`
	}{
		Alias:  (*Alias)(&wp),
		Height: height,
	})
}

// DashboardMetadata provides summary information about a dashboard.
type DashboardMetadata struct {
	Name        string
	Slug        string
	Description string
	CreatedBy   string
	PluginName  *string
	Editable    bool
	Enabled     bool
	FilePath    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DashboardManagerData contains data for the dashboard manager page
type DashboardManagerData struct {
	UserDashboards   []DashboardMetadata
	PluginDashboards []PluginDashboardInfo
}

// PluginDashboardInfo contains information about a plugin-provided dashboard
type PluginDashboardInfo struct {
	Name        string
	Description string
	PluginName  string
	Slug        string
	Enabled     bool
	Icon        string
}

// ValidationError represents a validation error in a dashboard.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return "no errors"
	}
	return e[0].Error()
}

// HasErrors returns true if there are validation errors.
func (e ValidationErrors) HasErrors() bool {
	return len(e) > 0
}
