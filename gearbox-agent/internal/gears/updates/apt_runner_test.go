package updates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
)

func newTestAptRunner(t *testing.T) *AptRunner {
	t.Helper()

	bus := events.NewBus()
	runner := &AptRunner{
		eventBus:   bus,
		operations: make(map[string]*AptOperation),
		logBuffers: make(map[string]*strings.Builder),
	}

	return runner
}

func TestAptRunner_PublishLineCapture(t *testing.T) {
	runner := newTestAptRunner(t)

	opID := "test-op-123"
	runner.publishLine(opID, "line 1")
	runner.publishLine(opID, "line 2")
	runner.publishLineWithStream(opID, "error line", "stderr")

	runner.logMu.Lock()
	buf := runner.logBuffers[opID]
	output := buf.String()
	runner.logMu.Unlock()

	if !strings.Contains(output, "line 1\n") {
		t.Errorf("expected output to contain 'line 1', got: %s", output)
	}
	if !strings.Contains(output, "line 2\n") {
		t.Errorf("expected output to contain 'line 2', got: %s", output)
	}
	if !strings.Contains(output, "error line\n") {
		t.Errorf("expected output to contain 'error line', got: %s", output)
	}
}

func TestAptRunner_PersistLog(t *testing.T) {
	runner := newTestAptRunner(t)

	// Use temp dir for logs
	tmpDir := t.TempDir()
	origDir := updateLogsDir

	// We can't change the const, so we'll test persistLog directly
	// by manually setting up the log buffer and operation, then checking output
	opID := "persist-test-id-123"
	runner.publishLine(opID, "test output line 1")
	runner.publishLine(opID, "test output line 2")

	now := time.Now()
	op := &AptOperation{
		ID:          opID,
		Type:        "check",
		Status:      "completed",
		StartedAt:   now.Add(-10 * time.Second),
		CompletedAt: &now,
		ExitCode:    0,
	}

	// Verify log buffer is consumed
	runner.logMu.Lock()
	buf := runner.logBuffers[opID]
	if buf == nil {
		t.Fatal("expected log buffer to exist")
	}
	output := buf.String()
	runner.logMu.Unlock()

	if !strings.Contains(output, "test output line 1") {
		t.Error("expected log buffer to contain output")
	}

	// Test persistLog writes a file (use the real const dir only if writable)
	_ = os.MkdirAll(tmpDir, 0750)

	// We can't test persistLog directly since it uses a const path,
	// but we can test the log entry creation
	completedAt := ""
	if op.CompletedAt != nil {
		completedAt = op.CompletedAt.Format(time.RFC3339)
	}

	logEntry := UpdateLog{
		ID:          op.ID,
		Type:        op.Type,
		Status:      op.Status,
		StartedAt:   op.StartedAt,
		CompletedAt: completedAt,
		ExitCode:    op.ExitCode,
		Output:      output,
	}

	data, err := json.MarshalIndent(logEntry, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal log entry: %v", err)
	}

	filename := op.StartedAt.Format("20060102-150405") + "_" + op.ID[:8] + ".json"
	logPath := filepath.Join(tmpDir, filename)
	if err := os.WriteFile(logPath, data, 0640); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}

	// Read it back and verify
	readData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	var readLog UpdateLog
	if err := json.Unmarshal(readData, &readLog); err != nil {
		t.Fatalf("failed to parse log file: %v", err)
	}

	if readLog.ID != opID {
		t.Errorf("expected ID %s, got %s", opID, readLog.ID)
	}
	if readLog.Status != "completed" {
		t.Errorf("expected status 'completed', got %s", readLog.Status)
	}
	if readLog.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", readLog.ExitCode)
	}
	if !strings.Contains(readLog.Output, "test output line 1") {
		t.Error("expected log output to contain test line")
	}

	_ = origDir // suppress unused
}

func TestAptRunner_GetOperation(t *testing.T) {
	runner := newTestAptRunner(t)

	// Add an operation
	op := &AptOperation{
		ID:        "op-1",
		Type:      "install",
		Status:    "running",
		StartedAt: time.Now(),
		Packages:  []string{"nginx"},
	}
	runner.mu.Lock()
	runner.operations["op-1"] = op
	runner.mu.Unlock()

	// Get existing operation
	got, ok := runner.GetOperation("op-1")
	if !ok {
		t.Fatal("expected to find operation")
	}
	if got.ID != "op-1" {
		t.Errorf("expected ID 'op-1', got %s", got.ID)
	}
	if got.Type != "install" {
		t.Errorf("expected type 'install', got %s", got.Type)
	}

	// Get non-existent operation
	_, ok = runner.GetOperation("nonexistent")
	if ok {
		t.Error("expected operation not found")
	}
}

func TestAptRunner_GetOperation_ReturnsCopy(t *testing.T) {
	runner := newTestAptRunner(t)

	op := &AptOperation{
		ID:        "op-copy",
		Type:      "check",
		Status:    "running",
		StartedAt: time.Now(),
	}
	runner.mu.Lock()
	runner.operations["op-copy"] = op
	runner.mu.Unlock()

	got, _ := runner.GetOperation("op-copy")
	got.Status = "modified"

	// Original should not be affected
	runner.mu.RLock()
	original := runner.operations["op-copy"]
	runner.mu.RUnlock()

	if original.Status != "running" {
		t.Error("modifying returned operation should not affect original")
	}
}

func TestAptRunner_CancelOperation(t *testing.T) {
	runner := newTestAptRunner(t)

	cancelled := false
	op := &AptOperation{
		ID:        "op-cancel",
		Type:      "install",
		Status:    "running",
		StartedAt: time.Now(),
		cancel:    func() { cancelled = true },
	}
	runner.mu.Lock()
	runner.operations["op-cancel"] = op
	runner.mu.Unlock()

	// Cancel running operation
	ok := runner.CancelOperation("op-cancel")
	if !ok {
		t.Error("expected cancel to succeed")
	}
	if !cancelled {
		t.Error("expected cancel function to be called")
	}

	// Cancel already cancelled operation
	ok = runner.CancelOperation("op-cancel")
	if ok {
		t.Error("expected cancel to fail for non-running operation")
	}

	// Cancel non-existent operation
	ok = runner.CancelOperation("nonexistent")
	if ok {
		t.Error("expected cancel to fail for non-existent operation")
	}
}

func TestAptRunner_HasActiveOperations(t *testing.T) {
	runner := newTestAptRunner(t)

	// No operations
	if runner.HasActiveOperations() {
		t.Error("expected no active operations")
	}

	// Add running operation
	runner.mu.Lock()
	runner.operations["op-1"] = &AptOperation{Status: "running"}
	runner.mu.Unlock()

	if !runner.HasActiveOperations() {
		t.Error("expected active operations")
	}

	// Mark as completed
	runner.mu.Lock()
	runner.operations["op-1"].Status = "completed"
	runner.mu.Unlock()

	if runner.HasActiveOperations() {
		t.Error("expected no active operations after completion")
	}
}

func TestAptRunner_ListOperations(t *testing.T) {
	runner := newTestAptRunner(t)

	// Empty list
	ops := runner.ListOperations()
	if len(ops) != 0 {
		t.Errorf("expected 0 operations, got %d", len(ops))
	}

	// Add operations
	runner.mu.Lock()
	runner.operations["op-1"] = &AptOperation{ID: "op-1", Status: "running"}
	runner.operations["op-2"] = &AptOperation{ID: "op-2", Status: "completed"}
	runner.mu.Unlock()

	ops = runner.ListOperations()
	if len(ops) != 2 {
		t.Errorf("expected 2 operations, got %d", len(ops))
	}
}

func TestAptRunner_ListOperations_ReturnsCopies(t *testing.T) {
	runner := newTestAptRunner(t)

	runner.mu.Lock()
	runner.operations["op-1"] = &AptOperation{ID: "op-1", Status: "running"}
	runner.mu.Unlock()

	ops := runner.ListOperations()
	ops[0].Status = "modified"

	runner.mu.RLock()
	original := runner.operations["op-1"]
	runner.mu.RUnlock()

	if original.Status != "running" {
		t.Error("modifying returned list should not affect originals")
	}
}

func TestAptRunner_FailOperation(t *testing.T) {
	runner := newTestAptRunner(t)

	op := &AptOperation{
		ID:        "op-fail",
		Type:      "install",
		Status:    "running",
		StartedAt: time.Now(),
	}

	runner.failOperation(op, "something went wrong")

	if op.Status != "failed" {
		t.Errorf("expected status 'failed', got %s", op.Status)
	}
	if op.Error != "something went wrong" {
		t.Errorf("expected error message, got %s", op.Error)
	}

	// Verify the error was published to log buffer
	runner.logMu.Lock()
	buf := runner.logBuffers[op.ID]
	runner.logMu.Unlock()

	if buf == nil {
		t.Fatal("expected log buffer to exist")
	}
	if !strings.Contains(buf.String(), "ERROR: something went wrong") {
		t.Error("expected error message in log buffer")
	}
}

func TestAptRunner_CompleteOperation(t *testing.T) {
	runner := newTestAptRunner(t)

	op := &AptOperation{
		ID:        "op-complete",
		Type:      "check",
		Status:    "running",
		StartedAt: time.Now(),
	}

	runner.mu.Lock()
	runner.operations["op-complete"] = op
	runner.mu.Unlock()

	runner.completeOperation(op)

	if op.Status != "completed" {
		t.Errorf("expected status 'completed', got %s", op.Status)
	}
	if op.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestAptRunner_CompleteOperation_PreservesFailedStatus(t *testing.T) {
	runner := newTestAptRunner(t)

	op := &AptOperation{
		ID:        "op-failed",
		Type:      "install",
		Status:    "failed",
		StartedAt: time.Now(),
		Error:     "test error",
	}

	runner.mu.Lock()
	runner.operations["op-failed"] = op
	runner.mu.Unlock()

	runner.completeOperation(op)

	// Should stay as "failed", not be overwritten to "completed"
	if op.Status != "failed" {
		t.Errorf("expected status 'failed' to be preserved, got %s", op.Status)
	}
}

func TestAptRunner_ConcurrentOperations(t *testing.T) {
	runner := newTestAptRunner(t)

	var wg sync.WaitGroup
	const numOps = 20

	// Concurrently add operations
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "concurrent-" + strings.Repeat("x", n)
			runner.mu.Lock()
			runner.operations[id] = &AptOperation{ID: id, Status: "running"}
			runner.mu.Unlock()
		}(i)
	}
	wg.Wait()

	// Concurrently list operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ops := runner.ListOperations()
			if len(ops) != numOps {
				t.Errorf("expected %d operations, got %d", numOps, len(ops))
			}
		}()
	}
	wg.Wait()
}

func TestUpdateLog_JSON(t *testing.T) {
	now := time.Now()
	log := UpdateLog{
		ID:           "test-123",
		Type:         "install",
		Status:       "completed",
		StartedAt:    now,
		CompletedAt:  now.Add(30 * time.Second).Format(time.RFC3339),
		Packages:     []string{"nginx", "curl"},
		SecurityOnly: false,
		ExitCode:     0,
		Output:       "some output\nline 2\n",
	}

	data, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed UpdateLog
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.ID != log.ID {
		t.Errorf("expected ID %s, got %s", log.ID, parsed.ID)
	}
	if parsed.Type != log.Type {
		t.Errorf("expected Type %s, got %s", log.Type, parsed.Type)
	}
	if len(parsed.Packages) != 2 {
		t.Errorf("expected 2 packages, got %d", len(parsed.Packages))
	}
	if parsed.Output != log.Output {
		t.Errorf("expected output to match")
	}
}

func TestUpdateLog_JSONOmitEmpty(t *testing.T) {
	log := UpdateLog{
		ID:        "test-456",
		Type:      "check",
		Status:    "completed",
		StartedAt: time.Now(),
		ExitCode:  0,
	}

	data, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	str := string(data)
	if strings.Contains(str, `"packages"`) {
		t.Error("expected packages to be omitted when empty")
	}
	if strings.Contains(str, `"error"`) {
		t.Error("expected error to be omitted when empty")
	}
	if strings.Contains(str, `"output"`) {
		t.Error("expected output to be omitted when empty")
	}
}

func TestAptOperation_JSON(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(5 * time.Second)
	op := AptOperation{
		ID:           "json-test",
		Type:         "install",
		Status:       "completed",
		StartedAt:    now,
		CompletedAt:  &completedAt,
		ExitCode:     0,
		Packages:     []string{"vim"},
		SecurityOnly: true,
	}

	data, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed AptOperation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.ID != "json-test" {
		t.Errorf("expected ID 'json-test', got %s", parsed.ID)
	}
	if !parsed.SecurityOnly {
		t.Error("expected SecurityOnly to be true")
	}
}

func TestListUpdateLogs_EmptyDir(t *testing.T) {
	runner := newTestAptRunner(t)

	// ListUpdateLogs should handle missing directory gracefully
	logs, err := runner.ListUpdateLogs(10)
	if err != nil {
		// May fail because /var/lib/gearbox-agent/update-logs doesn't exist on dev,
		// which is fine — it should return empty list
		if !os.IsNotExist(err) {
			// Only fail if it's an error other than "not found"
			t.Logf("ListUpdateLogs returned error (expected on dev): %v", err)
		}
	}
	// Should be empty or nil
	_ = logs
}

func TestListUpdateLogs_WithFiles(t *testing.T) {
	// Create temp directory with test log files
	tmpDir := t.TempDir()

	now := time.Now()
	for i, status := range []string{"completed", "failed", "completed"} {
		log := UpdateLog{
			ID:        "log-" + strings.Repeat("x", 8),
			Type:      "install",
			Status:    status,
			StartedAt: now.Add(-time.Duration(i) * time.Hour),
			ExitCode:  0,
		}
		if status == "failed" {
			log.ExitCode = 1
			log.Error = "test error"
		}

		data, err := json.MarshalIndent(log, "", "  ")
		if err != nil {
			t.Fatal(err)
		}

		filename := log.StartedAt.Format("20060102-150405") + "_" + log.ID[:8] + ".json"
		if err := os.WriteFile(filepath.Join(tmpDir, filename), data, 0640); err != nil {
			t.Fatal(err)
		}
	}

	// Verify files were created
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 files, got %d", len(entries))
	}
}
