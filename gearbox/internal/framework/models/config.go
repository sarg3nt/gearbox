package models

import (
	"time"
)

// ConfigSection represents a parsed section of the HAProxy config.
type ConfigSection struct {
	Type      string `json:"type"`       // "global", "defaults", "frontend", "backend", "listen"
	Name      string `json:"name"`       // Section name (empty for global/defaults)
	StartLine int    `json:"start_line"` // 1-indexed
	EndLine   int    `json:"end_line"`   // 1-indexed, inclusive
	IsAutoGen bool   `json:"is_auto_gen"`
}

// MarkerRange represents an auto-generated section range.
type MarkerRange struct {
	Type      string `json:"type"`       // "routing" or "backend"
	StartLine int    `json:"start_line"` // 1-indexed
	EndLine   int    `json:"end_line"`   // 1-indexed, inclusive
}

// ConfigReadResponse is returned when reading the configuration from the agent.
type ConfigReadResponse struct {
	Content      string          `json:"content"`
	Sections     []ConfigSection `json:"sections"`
	MarkerRanges []MarkerRange   `json:"marker_ranges"`
	SHA256       string          `json:"sha256"`
	LastModified time.Time       `json:"last_modified"`
	FilePath     string          `json:"file_path"`
}

// ConfigUpdateRequest is the request body for updating configuration.
type ConfigUpdateRequest struct {
	Content      string `json:"content"`
	ExpectedSHA  string `json:"expected_sha"`   // For optimistic locking
	BackupReason string `json:"backup_reason"`  // Reason for the change (for audit)
	DryRun       bool   `json:"dry_run"`        // If true, validate only, don't apply
}

// ConfigUpdateResponse is returned after updating configuration.
type ConfigUpdateResponse struct {
	Success          bool     `json:"success"`
	ValidationOutput string   `json:"validation_output"`
	BackupPath       string   `json:"backup_path,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	Message          string   `json:"message"`
	NewSHA256        string   `json:"new_sha256,omitempty"`
}

// ConfigBackup represents a configuration backup file.
type ConfigBackup struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"created_at"`
	Reason    string    `json:"reason"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
}

// BackupsListResponse lists available backups.
type BackupsListResponse struct {
	Backups []ConfigBackup `json:"backups"`
}

// RestoreRequest is the request body for restoring from backup.
type RestoreRequest struct {
	BackupID string `json:"backup_id"`
	DryRun   bool   `json:"dry_run"`
}

// RestoreResponse is returned after restoring from backup.
type RestoreResponse struct {
	Success          bool   `json:"success"`
	Message          string `json:"message"`
	ValidationOutput string `json:"validation_output,omitempty"`
	NewSHA256        string `json:"new_sha256,omitempty"`
}
