package haproxy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// RoutingMarkerStart marks the beginning of auto-generated routing rules.
	RoutingMarkerStart = "  # BEGIN AUTO-GENERATED ROUTING RULES - DO NOT EDIT MANUALLY"
	// RoutingMarkerEnd marks the end of auto-generated routing rules.
	RoutingMarkerEnd = "  # END AUTO-GENERATED ROUTING RULES"
	// BackendMarkerStart marks the beginning of auto-generated backends.
	BackendMarkerStart = "# BEGIN AUTO-GENERATED BACKENDS - DO NOT EDIT MANUALLY"
	// BackendMarkerEnd marks the end of auto-generated backends.
	BackendMarkerEnd = "# END AUTO-GENERATED BACKENDS"
)

// Manager manages HAProxy configuration updates.
type Manager struct {
	configFile string
	dryRun     bool
	logger     *slog.Logger
}

// NewManager creates a new HAProxy manager.
func NewManager(configFile string, dryRun bool, logger *slog.Logger) *Manager {
	return &Manager{
		configFile: configFile,
		dryRun:     dryRun,
		logger:     logger,
	}
}

// ValidateConfig validates the HAProxy configuration file.
func (m *Manager) ValidateConfig() error {
	cmd := exec.Command("haproxy", "-c", "-f", m.configFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validation failed: %s", string(output))
	}
	m.logger.Info("HAProxy configuration is valid")
	return nil
}

// Reload reloads the HAProxy service.
func (m *Manager) Reload() error {
	if m.dryRun {
		m.logger.Info("DRY RUN: Would reload HAProxy")
		return nil
	}

	cmd := exec.Command("systemctl", "reload", "haproxy")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reload failed: %s", string(output))
	}
	m.logger.Info("HAProxy reloaded successfully")
	return nil
}

// UpdateConfig updates the HAProxy configuration with new routing and backend sections.
// Returns true if the configuration was changed, false if no changes were needed.
func (m *Manager) UpdateConfig(routingConfig, backendConfig string) (bool, error) {
	// Read and validate current config exists
	currentConfig, err := m.readCurrentConfig()
	if err != nil {
		return false, err
	}

	// Check if config actually changed
	newHash := hashContent(routingConfig + backendConfig)
	oldHash := m.extractExistingHash(currentConfig)
	if oldHash == newHash {
		m.logger.Info("Configuration unchanged, skipping reload")
		return false, nil
	}

	m.logger.Info("Configuration changed, updating...")

	// Generate new config content
	newConfig, err := m.generateNewConfig(currentConfig, routingConfig, backendConfig)
	if err != nil {
		return false, err
	}

	// Apply the new configuration
	return m.applyConfig(newConfig)
}

// readCurrentConfig reads and returns the current configuration.
func (m *Manager) readCurrentConfig() (string, error) {
	if _, err := os.Stat(m.configFile); os.IsNotExist(err) {
		return "", fmt.Errorf("config file not found: %s", m.configFile)
	}

	data, err := os.ReadFile(m.configFile)
	if err != nil {
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	return string(data), nil
}

// generateNewConfig creates the new configuration by replacing marked sections.
func (m *Manager) generateNewConfig(currentConfig, routingConfig, backendConfig string) (string, error) {
	// Replace routing section
	newConfig, err := m.replaceSection(
		currentConfig,
		RoutingMarkerStart,
		RoutingMarkerEnd,
		routingConfig,
	)
	if err != nil {
		return "", fmt.Errorf("failed to update routing section: %w", err)
	}

	// Replace backend section
	newConfig, err = m.replaceSection(
		newConfig,
		BackendMarkerStart,
		BackendMarkerEnd,
		backendConfig,
	)
	if err != nil {
		return "", fmt.Errorf("failed to update backend section: %w", err)
	}

	return newConfig, nil
}

// applyConfig applies a new configuration with backup and rollback support.
func (m *Manager) applyConfig(newConfig string) (bool, error) {
	backupFile := m.configFile + ".backup"
	tempFile := m.configFile + ".tmp"

	// Create backup
	if err := copyFile(m.configFile, backupFile); err != nil {
		return false, fmt.Errorf("failed to create backup: %w", err)
	}
	m.logger.Info("Backed up config", "path", backupFile)

	// Write to temp file first with restricted permissions
	if err := os.WriteFile(tempFile, []byte(newConfig), 0640); err != nil {
		return false, fmt.Errorf("failed to write temp config: %w", err)
	}

	// Ensure temp file is cleaned up on error
	defer func() {
		if _, err := os.Stat(tempFile); err == nil {
			os.Remove(tempFile)
		}
	}()

	// Validate temp config
	if err := m.validateTempConfig(tempFile); err != nil {
		return false, err
	}
	m.logger.Info("New configuration is valid")

	// Move temp file to actual config
	if err := os.Rename(tempFile, m.configFile); err != nil {
		return false, fmt.Errorf("failed to move temp config: %w", err)
	}
	m.logger.Info("Updated configuration file", "path", m.configFile)

	// Reload HAProxy
	if err := m.Reload(); err != nil {
		m.logger.Warn("Failed to reload HAProxy, restoring backup")
		if restoreErr := copyFile(backupFile, m.configFile); restoreErr != nil {
			m.logger.Error("Failed to restore backup", "error", restoreErr)
		}
		return false, err
	}

	// Remove backup on success
	if err := os.Remove(backupFile); err != nil {
		m.logger.Warn("Failed to remove backup file", "error", err)
	}

	return true, nil
}

// validateTempConfig validates a temporary configuration file.
func (m *Manager) validateTempConfig(tempFile string) error {
	cmd := exec.Command("haproxy", "-c", "-f", tempFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("new configuration is invalid: %s", string(output))
	}
	return nil
}

// HasMarkers checks if the config file has the required markers.
func (m *Manager) HasMarkers() (bool, error) {
	content, err := os.ReadFile(m.configFile)
	if err != nil {
		return false, err
	}

	hasRouting := strings.Contains(string(content), RoutingMarkerStart) &&
		strings.Contains(string(content), RoutingMarkerEnd)
	hasBackend := strings.Contains(string(content), BackendMarkerStart) &&
		strings.Contains(string(content), BackendMarkerEnd)

	return hasRouting && hasBackend, nil
}

// replaceSection replaces content between markers.
func (m *Manager) replaceSection(config, startMarker, endMarker, newContent string) (string, error) {
	startIdx := strings.Index(config, startMarker)
	endIdx := strings.Index(config, endMarker)

	if startIdx == -1 || endIdx == -1 {
		return "", fmt.Errorf("markers not found: %s / %s", startMarker, endMarker)
	}

	// Ensure markers are in correct order
	if startIdx >= endIdx {
		return "", fmt.Errorf("markers are in wrong order or overlapping")
	}

	// Build replacement with markers and content
	replacement := fmt.Sprintf("%s\n  # This section is managed by gearbox-agent\n%s\n  %s",
		startMarker, newContent, endMarker)

	// Replace the section
	newConfig := config[:startIdx] + replacement + config[endIdx+len(endMarker):]

	return newConfig, nil
}

// extractExistingHash extracts and hashes the existing auto-generated content.
func (m *Manager) extractExistingHash(config string) string {
	var combined string

	// Extract routing section
	routingStart := strings.Index(config, RoutingMarkerStart)
	routingEnd := strings.Index(config, RoutingMarkerEnd)
	if routingStart != -1 && routingEnd != -1 {
		combined += config[routingStart : routingEnd+len(RoutingMarkerEnd)]
	}

	// Extract backend section
	backendStart := strings.Index(config, BackendMarkerStart)
	backendEnd := strings.Index(config, BackendMarkerEnd)
	if backendStart != -1 && backendEnd != -1 {
		combined += config[backendStart : backendEnd+len(BackendMarkerEnd)]
	}

	if combined == "" {
		return ""
	}

	return hashContent(combined)
}

// hashContent returns a SHA256 hash of the content.
func hashContent(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// Preserve original file permissions
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}

// GetConfigDir returns the directory of the config file.
func (m *Manager) GetConfigDir() string {
	return filepath.Dir(m.configFile)
}
