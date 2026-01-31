package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/database"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// BackupPage serves the database backup/restore management page.
func (h *Handler) BackupPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.authManager.GetUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Only admins can manage backups
	if !user.IsAdmin() {
		http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
		return
	}

	// Get backup directory from environment or use default
	backupDir := os.Getenv("BACKUP_DIR")
	if backupDir == "" {
		backupDir = "/data/backups"
	}

	// List existing backups
	backups, err := database.ListBackups(backupDir)
	if err != nil {
		h.logger.Error("Failed to list backups", "error", err)
		backups = []database.BackupInfo{} // Return empty list on error
	}

	// Get success/error messages from query params
	successMsg := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error")

	component := pages.BackupPage(user, backups, backupDir, successMsg, errorMsg)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render backup template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APICreateBackup creates a new database backup.
func (h *Handler) APICreateBackup(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	user, err := h.authManager.GetUser(r)
	if err != nil || !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Get backup directory from environment or use default
	backupDir := os.Getenv("BACKUP_DIR")
	if backupDir == "" {
		backupDir = "/data/backups"
	}

	// Create backup
	backupInfo, err := h.db.CreateBackup(backupDir)
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("create backup", err))
		return
	}

	h.logAudit(r, user.ID, "backup_created", fmt.Sprintf("Created database backup: %s", backupInfo.Filename))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"backup":  backupInfo,
		"message": "Backup created successfully",
	})
}

// APIRestoreBackup restores the database from a backup file.
func (h *Handler) APIRestoreBackup(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	user, err := h.authManager.GetUser(r)
	if err != nil || !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Parse request body
	var req struct {
		BackupPath string `json:"backup_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid request body",
		})
		return
	}

	// Validate backup path (security check - must be in backup directory)
	backupDir := os.Getenv("BACKUP_DIR")
	if backupDir == "" {
		backupDir = "/data/backups"
	}

	// Convert to absolute paths for comparison
	absBackupDir, err := filepath.Abs(backupDir)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Failed to resolve backup directory",
		})
		return
	}

	absBackupPath, err := filepath.Abs(req.BackupPath)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid backup path",
		})
		return
	}

	// Ensure the backup file is within the backup directory
	if filepath.Dir(absBackupPath) != absBackupDir {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Backup file must be in the backup directory",
		})
		return
	}

	// Restore from backup
	if err := h.db.RestoreFromBackup(req.BackupPath); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("restore backup", err))
		return
	}

	h.logAudit(r, user.ID, "backup_restored", fmt.Sprintf("Restored database from backup: %s", filepath.Base(req.BackupPath)))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Database restored successfully. The application will restart automatically.",
	})
}

// APIDeleteBackup deletes a backup file.
func (h *Handler) APIDeleteBackup(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	user, err := h.authManager.GetUser(r)
	if err != nil || !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Get backup path from URL parameter
	backupPath := chi.URLParam(r, "path")
	if backupPath == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Backup path required",
		})
		return
	}

	// Validate backup path (security check)
	backupDir := os.Getenv("BACKUP_DIR")
	if backupDir == "" {
		backupDir = "/data/backups"
	}

	absBackupDir, err := filepath.Abs(backupDir)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Failed to resolve backup directory",
		})
		return
	}

	absBackupPath, err := filepath.Abs(backupPath)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Invalid backup path",
		})
		return
	}

	if filepath.Dir(absBackupPath) != absBackupDir {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Backup file must be in the backup directory",
		})
		return
	}

	// Delete backup
	if err := database.DeleteBackup(backupPath); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("delete backup", err))
		return
	}

	h.logAudit(r, user.ID, "backup_deleted", fmt.Sprintf("Deleted database backup: %s", filepath.Base(backupPath)))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Backup deleted successfully",
	})
}

// APIDownloadBackup allows downloading a backup file.
func (h *Handler) APIDownloadBackup(w http.ResponseWriter, r *http.Request) {
	// Check if user is admin
	user, err := h.authManager.GetUser(r)
	if err != nil || !user.IsAdmin() {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Get backup path from URL parameter
	backupPath := chi.URLParam(r, "path")
	if backupPath == "" {
		http.Error(w, "Backup path required", http.StatusBadRequest)
		return
	}

	// Validate backup path (security check)
	backupDir := os.Getenv("BACKUP_DIR")
	if backupDir == "" {
		backupDir = "/data/backups"
	}

	absBackupDir, err := filepath.Abs(backupDir)
	if err != nil {
		http.Error(w, "Failed to resolve backup directory", http.StatusInternalServerError)
		return
	}

	absBackupPath, err := filepath.Abs(backupPath)
	if err != nil {
		http.Error(w, "Invalid backup path", http.StatusBadRequest)
		return
	}

	if filepath.Dir(absBackupPath) != absBackupDir {
		http.Error(w, "Backup file must be in the backup directory", http.StatusBadRequest)
		return
	}

	// Check if file exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		http.Error(w, "Backup file not found", http.StatusNotFound)
		return
	}

	h.logAudit(r, user.ID, "backup_downloaded", fmt.Sprintf("Downloaded database backup: %s", filepath.Base(backupPath)))

	// Serve the file
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(backupPath)))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, backupPath)
}
