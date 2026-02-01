package database

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// BackupInfo contains information about a database backup.
type BackupInfo struct {
	Filename  string    `json:"filename"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateBackup creates a backup of the database to the specified directory.
// Returns the path to the backup file and any error.
func (d *DB) CreateBackup(backupDir string) (*BackupInfo, error) {
	// Ensure backup directory exists
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Generate backup filename with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("gearbox-backup_%s.db", timestamp)
	backupPath := filepath.Join(backupDir, filename)

	// Lock the database for reading
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Execute VACUUM INTO to create a compact backup
	// This is safer than file copy and creates a clean, defragmented backup
	_, err := d.db.Exec(fmt.Sprintf("VACUUM INTO '%s'", backupPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}

	// Get backup file info
	fileInfo, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat backup file: %w", err)
	}

	info := &BackupInfo{
		Filename:  filename,
		Path:      backupPath,
		Size:      fileInfo.Size(),
		CreatedAt: time.Now(),
	}

	d.logger.Info("database backup created", "path", backupPath, "size_bytes", fileInfo.Size())
	return info, nil
}

// RestoreFromBackup restores the database from a backup file.
// WARNING: This will close the current database connection and replace the database file.
// The caller must reinitialize the database connection after this operation.
func (d *DB) RestoreFromBackup(backupPath string) error {
	// Verify backup file exists
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup file not found: %w", err)
	}

	// Get the current database file path
	var dbPath string
	row := d.db.QueryRow("PRAGMA database_list")
	var seq int
	var name string
	var file string
	if err := row.Scan(&seq, &name, &file); err != nil {
		return fmt.Errorf("failed to get database path: %w", err)
	}
	dbPath = file

	// Close the current database connection
	if err := d.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	// Create a temporary backup of the current database (safety measure)
	tempBackup := dbPath + ".before-restore"
	if err := copyFile(dbPath, tempBackup); err != nil {
		d.logger.Warn("failed to create safety backup", "error", err)
	}

	// Copy the backup file over the current database
	if err := copyFile(backupPath, dbPath); err != nil {
		// Try to restore the temporary backup
		if tempErr := copyFile(tempBackup, dbPath); tempErr != nil {
			return fmt.Errorf("failed to restore backup AND failed to recover: %w (recovery error: %v)", err, tempErr)
		}
		return fmt.Errorf("failed to restore backup (original database preserved): %w", err)
	}

	// Clean up temporary backup
	_ = os.Remove(tempBackup)

	d.logger.Info("database restored from backup", "path", backupPath)
	return nil
}

// ListBackups returns a list of all backup files in the specified directory.
func ListBackups(backupDir string) ([]BackupInfo, error) {
	// Check if directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []BackupInfo{}, nil // Return empty list if directory doesn't exist
	}

	files, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []BackupInfo
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Only include .db files
		if filepath.Ext(file.Name()) != ".db" {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		backups = append(backups, BackupInfo{
			Filename:  file.Name(),
			Path:      filepath.Join(backupDir, file.Name()),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
		})
	}

	return backups, nil
}

// DeleteBackup deletes a backup file.
func DeleteBackup(backupPath string) error {
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}
	return nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src) //#nosec G304 -- paths are internal database/backup paths, not user-controlled
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst) //#nosec G304 -- paths are internal database/backup paths, not user-controlled
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return destFile.Sync()
}
