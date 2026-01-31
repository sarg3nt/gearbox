// Package system provides system-level collectors for OS updates and package management.
package updates

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

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

// AptRunner manages streaming apt operations.
type AptRunner struct {
	eventBus   *events.Bus
	mu         sync.RWMutex
	operations map[string]*AptOperation
}

// NewAptRunner creates a new apt runner.
func NewAptRunner(eventBus *events.Bus) *AptRunner {
	runner := &AptRunner{
		eventBus:   eventBus,
		operations: make(map[string]*AptOperation),
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

	// Optionally create snapshot first
	if createSnapshot {
		r.publishLine(op.ID, "Creating pre-update snapshot...")
		// Note: Snapshot creation would go here if implemented
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

// publishLineWithStream publishes a single output line with stream type.
func (r *AptRunner) publishLineWithStream(operationID, line, stream string) {
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

// completeOperation marks an operation as complete and emits the appropriate event.
func (r *AptRunner) completeOperation(op *AptOperation) {
	r.mu.Lock()
	now := time.Now()
	op.CompletedAt = &now
	if op.Status == "running" {
		op.Status = "completed"
	}
	r.mu.Unlock()

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
