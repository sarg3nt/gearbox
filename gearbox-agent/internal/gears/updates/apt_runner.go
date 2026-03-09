// Package system provides system-level collectors for OS updates and package management.
package updates

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

const updateLogsDir = "/var/lib/gearbox-agent/update-logs"

// AptOperation represents an active or completed apt operation.
type AptOperation struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`   // "install", "upgrade", "check"
	Status       string     `json:"status"` // "running", "completed", "failed"
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ExitCode     int        `json:"exit_code"`
	Packages     []string   `json:"packages,omitempty"`
	SecurityOnly bool       `json:"security_only,omitempty"`
	Error        string     `json:"error,omitempty"`
	cancel       context.CancelFunc
}

// UpdateLog represents a persisted update operation log.
type UpdateLog struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  string    `json:"completed_at,omitempty"`
	Packages     []string  `json:"packages,omitempty"`
	SecurityOnly bool      `json:"security_only,omitempty"`
	ExitCode     int       `json:"exit_code"`
	Error        string    `json:"error,omitempty"`
	Output       string    `json:"output,omitempty"`
}

// AptRunner manages streaming apt operations.
type AptRunner struct {
	eventBus   *events.Bus
	mu         sync.RWMutex
	operations map[string]*AptOperation
	logMu      sync.Mutex
	logBuffers map[string]*strings.Builder
}

// NewAptRunner creates a new apt runner.
func NewAptRunner(eventBus *events.Bus) *AptRunner {
	// Ensure log directory exists
	if err := os.MkdirAll(updateLogsDir, 0750); err != nil {
		slog.Warn("failed to create update logs directory", "dir", updateLogsDir, "error", err)
	}

	runner := &AptRunner{
		eventBus:   eventBus,
		operations: make(map[string]*AptOperation),
		logBuffers: make(map[string]*strings.Builder),
	}

	// Start cleanup goroutine
	go runner.cleanupOldOperations()

	return runner
}

// cleanupOldOperations removes completed operations older than 1 hour.
func (r *AptRunner) cleanupOldOperations() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		r.mu.Lock()
		cutoff := time.Now().Add(-1 * time.Hour)
		for id, op := range r.operations {
			if op.Status != "running" && op.CompletedAt != nil && op.CompletedAt.Before(cutoff) {
				delete(r.operations, id)
			}
		}
		r.mu.Unlock()
	}
}

// StartInstall begins a streaming apt install operation.
func (r *AptRunner) StartInstall(securityOnly bool, packages []string, createSnapshot bool) (string, error) {
	operationID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())

	op := &AptOperation{
		ID:           operationID,
		Type:         "install",
		Status:       "running",
		StartedAt:    time.Now(),
		Packages:     packages,
		SecurityOnly: securityOnly,
		cancel:       cancel,
	}

	r.mu.Lock()
	r.operations[operationID] = op
	r.mu.Unlock()

	// Emit started event
	r.eventBus.Publish(events.Event{
		Type:      events.EventAptStarted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"operation_id":  operationID,
			"type":          "install",
			"packages":      packages,
			"security_only": securityOnly,
		},
	})

	// Run apt in background goroutine
	go r.runAptInstall(ctx, op, securityOnly, packages, createSnapshot)

	return operationID, nil
}

// StartCheck begins a streaming apt update check.
func (r *AptRunner) StartCheck() (string, error) {
	operationID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())

	op := &AptOperation{
		ID:        operationID,
		Type:      "check",
		Status:    "running",
		StartedAt: time.Now(),
		cancel:    cancel,
	}

	r.mu.Lock()
	r.operations[operationID] = op
	r.mu.Unlock()

	// Emit started event
	r.eventBus.Publish(events.Event{
		Type:      events.EventAptStarted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"operation_id": operationID,
			"type":         "check",
		},
	})

	// Run apt update in background
	go r.runAptCheck(ctx, op)

	return operationID, nil
}

// runAptCheck executes apt update and streams output.
func (r *AptRunner) runAptCheck(ctx context.Context, op *AptOperation) {
	defer r.completeOperation(op)

	r.publishLine(op.ID, "Running apt update...")

	cmd := exec.CommandContext(ctx, "apt-get", "update")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")

	if err := r.runCommandWithStreaming(ctx, op, cmd); err != nil {
		r.failOperation(op, fmt.Sprintf("apt update failed: %v", err))
		return
	}

	r.publishLine(op.ID, "")
	r.publishLine(op.ID, "Update check completed successfully.")
	op.Status = "completed"
}

// runAptInstall executes the apt install command and streams output.
func (r *AptRunner) runAptInstall(ctx context.Context, op *AptOperation, securityOnly bool, packages []string, createSnapshot bool) {
	defer r.completeOperation(op)

	// Optionally create snapshot before installing
	if createSnapshot {
		r.publishLine(op.ID, "Creating pre-update snapshot...")

		// Build a descriptive reason for the auto-snapshot
		var reason string
		if len(packages) > 0 {
			if len(packages) <= 3 {
				reason = fmt.Sprintf("auto: before installing %s", strings.Join(packages, ", "))
			} else {
				reason = fmt.Sprintf("auto: before installing %s and %d more", strings.Join(packages[:3], ", "), len(packages)-3)
			}
		} else if securityOnly {
			reason = "auto: before installing security updates"
		} else {
			reason = "auto: before installing all updates"
		}

		collector := NewUpdatesCollector()
		snapshot, err := collector.CreateSnapshot(reason)
		if err != nil {
			r.publishLine(op.ID, fmt.Sprintf("Warning: snapshot creation failed: %v", err))
		} else {
			r.publishLine(op.ID, fmt.Sprintf("Snapshot %s created.", snapshot.ID))
		}
		r.publishLine(op.ID, "")
	}

	// Build apt command
	var args []string
	if len(packages) > 0 {
		// Install specific packages
		args = append([]string{"install", "-y"}, packages...)
		r.publishLine(op.ID, fmt.Sprintf("Installing %d specific package(s)...", len(packages)))
	} else if securityOnly {
		// Security updates only - use unattended-upgrade
		r.publishLine(op.ID, "Installing security updates only...")
		r.runSecurityOnlyUpgrade(ctx, op)
		return
	} else {
		// Full upgrade
		args = []string{"upgrade", "-y"}
		r.publishLine(op.ID, "Installing all available updates...")
	}

	r.publishLine(op.ID, "")

	cmd := exec.CommandContext(ctx, "apt-get", args...)
	cmd.Env = append(os.Environ(),
		"DEBIAN_FRONTEND=noninteractive",
		"NEEDRESTART_MODE=a",
	)

	if err := r.runCommandWithStreaming(ctx, op, cmd); err != nil {
		r.failOperation(op, fmt.Sprintf("apt install failed: %v", err))
		return
	}

	r.publishLine(op.ID, "")
	r.publishLine(op.ID, "Installation completed successfully.")
	op.Status = "completed"
}

// runSecurityOnlyUpgrade runs unattended-upgrade for security updates.
func (r *AptRunner) runSecurityOnlyUpgrade(ctx context.Context, op *AptOperation) {
	cmd := exec.CommandContext(ctx, "unattended-upgrade", "--minimal-upgrade-steps")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")

	if err := r.runCommandWithStreaming(ctx, op, cmd); err != nil {
		r.failOperation(op, fmt.Sprintf("unattended-upgrade failed: %v", err))
		return
	}

	r.publishLine(op.ID, "")
	r.publishLine(op.ID, "Security updates installed successfully.")
	op.Status = "completed"
}

// runCommandWithStreaming executes a command and streams its output.
func (r *AptRunner) runCommandWithStreaming(ctx context.Context, op *AptOperation, cmd *exec.Cmd) error {
	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Read stdout and stderr concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		r.streamOutput(ctx, op.ID, stdout, "stdout")
	}()

	go func() {
		defer wg.Done()
		r.streamOutput(ctx, op.ID, stderr, "stderr")
	}()

	wg.Wait()

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			op.ExitCode = exitErr.ExitCode()
		}
		return err
	}

	op.ExitCode = 0
	return nil
}

// streamOutput reads from reader and publishes each line.
func (r *AptRunner) streamOutput(ctx context.Context, operationID string, reader io.Reader, stream string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			line := scanner.Text()
			r.publishLineWithStream(operationID, line, stream)
		}
	}
}

// publishLine publishes a single output line.
func (r *AptRunner) publishLine(operationID, line string) {
	r.publishLineWithStream(operationID, line, "stdout")
}

// publishLineWithStream publishes a single output line with stream type
// and captures it to the persistent log buffer.
func (r *AptRunner) publishLineWithStream(operationID, line, stream string) {
	// Capture to log buffer for persistence
	r.logMu.Lock()
	buf, ok := r.logBuffers[operationID]
	if !ok {
		buf = &strings.Builder{}
		r.logBuffers[operationID] = buf
	}
	buf.WriteString(line)
	buf.WriteString("\n")
	r.logMu.Unlock()

	r.eventBus.Publish(events.Event{
		Type:      events.EventAptOutput,
		Timestamp: time.Now(),
		Data: map[string]any{
			"operation_id": operationID,
			"line":         line,
			"stream":       stream,
			"timestamp":    time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// completeOperation marks an operation as complete, persists the log, and emits the appropriate event.
func (r *AptRunner) completeOperation(op *AptOperation) {
	r.mu.Lock()
	now := time.Now()
	op.CompletedAt = &now
	if op.Status == "running" {
		op.Status = "completed"
	}
	r.mu.Unlock()

	// Persist log to disk
	r.persistLog(op)

	// Emit completion event
	eventType := events.EventAptCompleted
	if op.Status == "failed" {
		eventType = events.EventAptFailed
	}

	r.eventBus.Publish(events.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data: map[string]any{
			"operation_id": op.ID,
			"status":       op.Status,
			"exit_code":    op.ExitCode,
			"error":        op.Error,
		},
	})
}

// failOperation marks an operation as failed.
func (r *AptRunner) failOperation(op *AptOperation, errorMsg string) {
	r.mu.Lock()
	op.Status = "failed"
	op.Error = errorMsg
	r.mu.Unlock()

	r.publishLine(op.ID, "ERROR: "+errorMsg)
}

// StartRestore begins a streaming snapshot restore operation.
func (r *AptRunner) StartRestore(snapshotID string) (string, error) {
	if err := validateSnapshotID(snapshotID); err != nil {
		return "", err
	}
	operationID := uuid.New().String()
	ctx, cancel := context.WithCancel(context.Background())

	op := &AptOperation{
		ID:        operationID,
		Type:      "restore",
		Status:    "running",
		StartedAt: time.Now(),
		cancel:    cancel,
	}

	r.mu.Lock()
	r.operations[operationID] = op
	r.mu.Unlock()

	// Emit started event
	r.eventBus.Publish(events.Event{
		Type:      events.EventAptStarted,
		Timestamp: time.Now(),
		Data: map[string]any{
			"operation_id": operationID,
			"type":         "restore",
			"snapshot_id":  snapshotID,
		},
	})

	// Run restore in background goroutine
	go r.runRestoreSnapshot(ctx, op, snapshotID)

	return operationID, nil
}

// runRestoreSnapshot executes snapshot restore and streams output.
func (r *AptRunner) runRestoreSnapshot(ctx context.Context, op *AptOperation, snapshotID string) {
	defer r.completeOperation(op)

	snapshotDir := "/var/lib/gearbox-agent/snapshots"
	snapshotFile := fmt.Sprintf("%s/%s.selections", snapshotDir, snapshotID)

	// Verify snapshot exists
	if _, err := os.Stat(snapshotFile); err != nil {
		r.failOperation(op, fmt.Sprintf("snapshot not found: %s", snapshotID))
		return
	}

	// Read snapshot selections
	selectionsData, err := os.ReadFile(snapshotFile) //#nosec G304
	if err != nil {
		r.failOperation(op, fmt.Sprintf("failed to read snapshot: %v", err))
		return
	}

	r.publishLine(op.ID, fmt.Sprintf("Restoring snapshot %s...", snapshotID))
	r.publishLine(op.ID, "")

	// Step 1: dpkg --set-selections
	r.publishLine(op.ID, "Setting package selections...")
	setCmd := exec.CommandContext(ctx, "dpkg", "--set-selections")
	setCmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	setCmd.Stdin = strings.NewReader(string(selectionsData))

	output, err := setCmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.Canceled {
			r.failOperation(op, "operation cancelled")
			return
		}
		errMsg := "dpkg --set-selections failed"
		if len(output) > 0 {
			errMsg += ": " + strings.TrimSpace(string(output))
		}
		r.failOperation(op, errMsg)
		return
	}
	r.publishLine(op.ID, "Package selections set successfully.")
	r.publishLine(op.ID, "")

	// Step 2: apt-get dselect-upgrade -y (streamed)
	r.publishLine(op.ID, "Applying package changes (apt-get dselect-upgrade)...")
	r.publishLine(op.ID, "")

	upgradeCmd := exec.CommandContext(ctx, "apt-get", "dselect-upgrade", "-y")
	upgradeCmd.Env = append(os.Environ(),
		"DEBIAN_FRONTEND=noninteractive",
		"NEEDRESTART_MODE=a",
	)

	if err := r.runCommandWithStreaming(ctx, op, upgradeCmd); err != nil {
		r.failOperation(op, fmt.Sprintf("apt-get dselect-upgrade failed: %v", err))
		return
	}

	// Step 3: version-aware downgrade if .versions file exists
	versionsFile := fmt.Sprintf("%s/%s.versions", snapshotDir, snapshotID)
	if downgrades := computeDowngrades(versionsFile); len(downgrades) > 0 {
		r.publishLine(op.ID, "")
		r.publishLine(op.ID, fmt.Sprintf("Downgrading %d package(s) to snapshot versions...", len(downgrades)))
		r.publishLine(op.ID, "")

		args := append([]string{"install", "-y", "--allow-downgrades"}, downgrades...)
		downgradeCmd := exec.CommandContext(ctx, "apt-get", args...)
		downgradeCmd.Env = append(os.Environ(),
			"DEBIAN_FRONTEND=noninteractive",
			"NEEDRESTART_MODE=a",
		)

		if err := r.runCommandWithStreaming(ctx, op, downgradeCmd); err != nil {
			// Non-fatal: log but don't fail
			r.publishLine(op.ID, fmt.Sprintf("Warning: some downgrades failed: %v", err))
		} else {
			r.publishLine(op.ID, "Package downgrades applied.")
		}
	}

	// Step 4: apt-get update to refresh package cache
	r.publishLine(op.ID, "")
	r.publishLine(op.ID, "Refreshing package cache (apt-get update)...")
	r.publishLine(op.ID, "")

	updateCmd := exec.CommandContext(ctx, "apt-get", "update")
	updateCmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")

	if err := r.runCommandWithStreaming(ctx, op, updateCmd); err != nil {
		// Non-fatal: log the error but don't fail the whole restore
		r.publishLine(op.ID, fmt.Sprintf("Warning: apt-get update failed: %v", err))
	} else {
		r.publishLine(op.ID, "Package cache refreshed.")
	}

	r.publishLine(op.ID, "")
	r.publishLine(op.ID, "Snapshot restored successfully.")
	op.Status = "completed"
}

// GetOperation returns an operation by ID.
func (r *AptRunner) GetOperation(id string) (*AptOperation, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	op, ok := r.operations[id]
	if !ok {
		return nil, false
	}
	// Return a copy to avoid race conditions
	opCopy := *op
	return &opCopy, true
}

// CancelOperation cancels a running operation.
func (r *AptRunner) CancelOperation(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	op, ok := r.operations[id]
	if !ok {
		return false
	}

	if op.Status == "running" && op.cancel != nil {
		op.cancel()
		op.Status = "cancelled"
		return true
	}
	return false
}

// HasActiveOperations returns true if there are running operations.
func (r *AptRunner) HasActiveOperations() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, op := range r.operations {
		if op.Status == "running" {
			return true
		}
	}
	return false
}

// ListOperations returns all operations.
func (r *AptRunner) ListOperations() []*AptOperation {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ops := make([]*AptOperation, 0, len(r.operations))
	for _, op := range r.operations {
		// Return a copy to avoid race conditions
		opCopy := *op
		ops = append(ops, &opCopy)
	}
	return ops
}

// persistLog writes the operation log to disk as a JSON file.
func (r *AptRunner) persistLog(op *AptOperation) {
	r.logMu.Lock()
	buf := r.logBuffers[op.ID]
	output := ""
	if buf != nil {
		output = buf.String()
	}
	delete(r.logBuffers, op.ID)
	r.logMu.Unlock()

	completedAt := ""
	if op.CompletedAt != nil {
		completedAt = op.CompletedAt.Format(time.RFC3339)
	}

	logEntry := UpdateLog{
		ID:           op.ID,
		Type:         op.Type,
		Status:       op.Status,
		StartedAt:    op.StartedAt,
		CompletedAt:  completedAt,
		Packages:     op.Packages,
		SecurityOnly: op.SecurityOnly,
		ExitCode:     op.ExitCode,
		Error:        op.Error,
		Output:       output,
	}

	data, err := json.MarshalIndent(logEntry, "", "  ")
	if err != nil {
		return
	}

	filename := fmt.Sprintf("%s_%s.json", op.StartedAt.Format("20060102-150405"), op.ID[:8])
	logPath := filepath.Join(updateLogsDir, filename)
	_ = os.WriteFile(logPath, data, 0640) //#nosec G306
}

// ListUpdateLogs returns metadata for all persisted update logs, most recent first.
func (r *AptRunner) ListUpdateLogs(limit int) ([]UpdateLog, error) {
	entries, err := os.ReadDir(updateLogsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []UpdateLog{}, nil
		}
		return nil, fmt.Errorf("failed to read update logs directory: %w", err)
	}

	var logs []UpdateLog
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		logPath := filepath.Join(updateLogsDir, entry.Name())
		data, err := os.ReadFile(logPath) //#nosec G304
		if err != nil {
			continue
		}

		var logEntry UpdateLog
		if err := json.Unmarshal(data, &logEntry); err != nil {
			continue
		}

		// Don't include the full output in the list — just metadata
		logEntry.Output = ""
		logs = append(logs, logEntry)
	}

	// Sort by started_at descending (most recent first)
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].StartedAt.After(logs[j].StartedAt)
	})

	if limit > 0 && len(logs) > limit {
		logs = logs[:limit]
	}

	return logs, nil
}

// GetUpdateLog returns a specific update log with full output.
func (r *AptRunner) GetUpdateLog(id string) (*UpdateLog, error) {
	if len(id) < 8 {
		return nil, fmt.Errorf("invalid log ID")
	}
	entries, err := os.ReadDir(updateLogsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read update logs directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		// Check if filename contains the ID prefix
		if !strings.Contains(entry.Name(), id[:8]) {
			continue
		}

		logPath := filepath.Join(updateLogsDir, entry.Name())
		data, err := os.ReadFile(logPath) //#nosec G304
		if err != nil {
			return nil, fmt.Errorf("failed to read log file: %w", err)
		}

		var logEntry UpdateLog
		if err := json.Unmarshal(data, &logEntry); err != nil {
			return nil, fmt.Errorf("failed to parse log file: %w", err)
		}

		return &logEntry, nil
	}

	return nil, fmt.Errorf("log not found: %s", id)
}
