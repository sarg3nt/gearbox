package dashboard

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Storage handles dashboard persistence to/from YAML files.
type Storage struct {
	dataDir   string
	validator *Validator
	logger    *slog.Logger
}

// NewStorage creates a new dashboard storage instance.
func NewStorage(dataDir string, logger *slog.Logger) (*Storage, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Ensure the dashboards directory exists
	dashboardDir := filepath.Join(dataDir, "dashboards")
	if err := os.MkdirAll(dashboardDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dashboards directory: %w", err)
	}

	return &Storage{
		dataDir:   dataDir,
		validator: NewValidator(),
		logger:    logger,
	}, nil
}

// Load loads a dashboard from a YAML file by slug.
func (s *Storage) Load(slug string) (*Dashboard, error) {
	filename := GenerateFilename(slug)
	filePath := filepath.Join(s.dataDir, "dashboards", filename)

	return s.LoadFromPath(filePath)
}

// LoadFromPath loads a dashboard from a specific file path.
func (s *Storage) LoadFromPath(filePath string) (*Dashboard, error) {
	// Read the YAML file
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("dashboard not found: %s", filePath)
		}
		return nil, fmt.Errorf("failed to read dashboard file: %w", err)
	}

	// Parse YAML
	var dashboard Dashboard
	if err := yaml.Unmarshal(data, &dashboard); err != nil {
		return nil, fmt.Errorf("failed to parse dashboard YAML: %w", err)
	}

	// Validate dashboard
	if validationErrors := s.validator.Validate(&dashboard); validationErrors.HasErrors() {
		return nil, fmt.Errorf("dashboard validation failed: %w", validationErrors)
	}

	// Validate widget IDs
	if validationErrors := s.validator.ValidateWidgetIDs(dashboard.Widgets); validationErrors.HasErrors() {
		return nil, fmt.Errorf("dashboard validation failed: %w", validationErrors)
	}

	// Get file info for metadata
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		dashboard.CreatedAt = fileInfo.ModTime()
		dashboard.UpdatedAt = fileInfo.ModTime()
	}

	// Set metadata
	dashboard.FilePath = filePath
	dashboard.Slug = filepath.Base(filePath[:len(filePath)-5]) // Remove .yaml extension

	return &dashboard, nil
}

// Save saves a dashboard to a YAML file.
func (s *Storage) Save(dashboard *Dashboard) error {
	// Validate dashboard
	if validationErrors := s.validator.Validate(dashboard); validationErrors.HasErrors() {
		return fmt.Errorf("dashboard validation failed: %w", validationErrors)
	}

	// Validate widget IDs
	if validationErrors := s.validator.ValidateWidgetIDs(dashboard.Widgets); validationErrors.HasErrors() {
		return fmt.Errorf("dashboard validation failed: %w", validationErrors)
	}

	// Generate slug if not set
	if dashboard.Slug == "" {
		dashboard.Slug = GenerateSlug(dashboard.Name)
	}

	// Generate file path
	filename := GenerateFilename(dashboard.Slug)
	filePath := filepath.Join(s.dataDir, "dashboards", filename)

	// Marshal to YAML
	data, err := yaml.Marshal(dashboard)
	if err != nil {
		return fmt.Errorf("failed to marshal dashboard to YAML: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write dashboard file: %w", err)
	}

	// Update metadata
	dashboard.FilePath = filePath
	dashboard.UpdatedAt = time.Now()

	s.logger.Info("dashboard saved", "slug", dashboard.Slug, "path", filePath)

	return nil
}

// Delete deletes a dashboard by slug.
func (s *Storage) Delete(slug string) error {
	filename := GenerateFilename(slug)
	filePath := filepath.Join(s.dataDir, "dashboards", filename)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("dashboard not found: %s", slug)
	}

	// Delete the file
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete dashboard file: %w", err)
	}

	s.logger.Info("dashboard deleted", "slug", slug, "path", filePath)

	return nil
}

// List returns metadata for all dashboards.
func (s *Storage) List() ([]DashboardMetadata, error) {
	dashboardDir := filepath.Join(s.dataDir, "dashboards")

	// Read directory
	entries, err := os.ReadDir(dashboardDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []DashboardMetadata{}, nil
		}
		return nil, fmt.Errorf("failed to read dashboards directory: %w", err)
	}

	var dashboards []DashboardMetadata

	for _, entry := range entries {
		// Skip non-YAML files
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		// Load dashboard
		filePath := filepath.Join(dashboardDir, entry.Name())
		dashboard, err := s.LoadFromPath(filePath)
		if err != nil {
			s.logger.Warn("failed to load dashboard", "file", entry.Name(), "error", err)
			continue
		}

		// Add to list
		dashboards = append(dashboards, DashboardMetadata{
			Name:        dashboard.Name,
			Slug:        dashboard.Slug,
			Description: dashboard.Description,
			CreatedBy:   dashboard.CreatedBy,
			PluginName:  dashboard.PluginName,
			Editable:    dashboard.Editable,
			FilePath:    dashboard.FilePath,
			CreatedAt:   dashboard.CreatedAt,
			UpdatedAt:   dashboard.UpdatedAt,
		})
	}

	return dashboards, nil
}

// Exists checks if a dashboard with the given slug exists.
func (s *Storage) Exists(slug string) bool {
	filename := GenerateFilename(slug)
	filePath := filepath.Join(s.dataDir, "dashboards", filename)

	_, err := os.Stat(filePath)
	return err == nil
}

// CreateDefaultDashboard creates the default "Dashboard" homepage if it doesn't exist.
func (s *Storage) CreateDefaultDashboard() error {
	// Check if default dashboard already exists
	if s.Exists("dashboard") {
		s.logger.Info("default dashboard already exists")
		return nil
	}

	// Create default dashboard
	dashboard := &Dashboard{
		Version:     "1.0",
		Name:        "Dashboard",
		Description: "Main monitoring overview",
		CreatedBy:   "user",
		PluginName:  nil,
		Editable:    true,
		Layout: Layout{
			Columns: 12,
			Gap:     4,
		},
		Widgets: []Widget{
			{
				ID:   "welcome-1",
				Type: "alert-banner",
				Position: WidgetPosition{
					Row:    1,
					Column: 1,
					Width:  12,
					Height: "auto",
				},
				Config: map[string]interface{}{
					"severity":   "info",
					"message":    "Welcome to Gearbox! This is your default dashboard. You can customize it by adding widgets.",
					"icon":       "info",
					"dismissible": true,
				},
			},
		},
		Slug: "dashboard",
	}

	// Save dashboard
	if err := s.Save(dashboard); err != nil {
		return fmt.Errorf("failed to create default dashboard: %w", err)
	}

	s.logger.Info("created default dashboard")

	return nil
}
