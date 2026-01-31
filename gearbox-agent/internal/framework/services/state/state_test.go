package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if mgr == nil {
		t.Fatal("NewManager() returned nil")
	}

	if mgr.filePath != statePath {
		t.Errorf("filePath = %q, want %q", mgr.filePath, statePath)
	}
}

func TestNewManager_LoadsExistingState(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create existing state file
	existingState := State{
		LastCommitSHA: "abc1234567890",
		LastSyncTime:  time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		LastError:     "previous error",
	}
	data, _ := json.MarshalIndent(existingState, "", "  ")
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatalf("Failed to write test state: %v", err)
	}

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if mgr.GetLastCommitSHA() != existingState.LastCommitSHA {
		t.Errorf("LastCommitSHA = %q, want %q", mgr.GetLastCommitSHA(), existingState.LastCommitSHA)
	}
	if !mgr.GetLastSyncTime().Equal(existingState.LastSyncTime) {
		t.Errorf("LastSyncTime = %v, want %v", mgr.GetLastSyncTime(), existingState.LastSyncTime)
	}
	if mgr.GetLastError() != existingState.LastError {
		t.Errorf("LastError = %q, want %q", mgr.GetLastError(), existingState.LastError)
	}
}

func TestNewManager_NonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "nonexistent", "state.json")

	// Should not error for nonexistent file - just start with empty state
	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if mgr.GetLastCommitSHA() != "" {
		t.Errorf("LastCommitSHA should be empty for new state")
	}
}

func TestSetLastCommitSHA(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	testSHA := "def456789abcdef"
	beforeSet := time.Now()
	if err := mgr.SetLastCommitSHA(testSHA); err != nil {
		t.Fatalf("SetLastCommitSHA() error = %v", err)
	}
	afterSet := time.Now()

	// Check in-memory state
	if mgr.GetLastCommitSHA() != testSHA {
		t.Errorf("GetLastCommitSHA() = %q, want %q", mgr.GetLastCommitSHA(), testSHA)
	}

	// Check that sync time was updated
	syncTime := mgr.GetLastSyncTime()
	if syncTime.Before(beforeSet) || syncTime.After(afterSet) {
		t.Error("LastSyncTime should be set to current time")
	}

	// Check that error was cleared
	if mgr.GetLastError() != "" {
		t.Error("SetLastCommitSHA should clear LastError")
	}

	// Verify file was written
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read state file: %v", err)
	}

	var savedState State
	if err := json.Unmarshal(data, &savedState); err != nil {
		t.Fatalf("Failed to parse state file: %v", err)
	}

	if savedState.LastCommitSHA != testSHA {
		t.Errorf("Saved LastCommitSHA = %q, want %q", savedState.LastCommitSHA, testSHA)
	}
}

func TestSetError(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	testErr := errors.New("test error message")
	if err := mgr.SetError(testErr); err != nil {
		t.Fatalf("SetError() error = %v", err)
	}

	if mgr.GetLastError() != testErr.Error() {
		t.Errorf("GetLastError() = %q, want %q", mgr.GetLastError(), testErr.Error())
	}

	// Verify file was written
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read state file: %v", err)
	}

	var savedState State
	if err := json.Unmarshal(data, &savedState); err != nil {
		t.Fatalf("Failed to parse state file: %v", err)
	}

	if savedState.LastError != testErr.Error() {
		t.Errorf("Saved LastError = %q, want %q", savedState.LastError, testErr.Error())
	}
}

func TestSetError_ClearError(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Set an error
	if err := mgr.SetError(errors.New("some error")); err != nil {
		t.Fatalf("SetError() error = %v", err)
	}

	// Clear error by passing nil
	if err := mgr.SetError(nil); err != nil {
		t.Fatalf("SetError(nil) error = %v", err)
	}

	if mgr.GetLastError() != "" {
		t.Errorf("GetLastError() = %q, want empty string", mgr.GetLastError())
	}
}

func TestSave(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Modify state directly (normally done through setters)
	mgr.state.LastCommitSHA = "test123"
	mgr.state.LastSyncTime = time.Now()

	if err := mgr.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file was written
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("State file not created: %v", err)
	}
}

func TestSave_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "subdir", "deep", "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if err := mgr.SetLastCommitSHA("test"); err != nil {
		t.Fatalf("SetLastCommitSHA() error = %v", err)
	}

	// Verify file and directories were created
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("State file not created: %v", err)
	}
}

func TestFilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if err := mgr.SetLastCommitSHA("test"); err != nil {
		t.Fatalf("SetLastCommitSHA() error = %v", err)
	}

	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("Failed to stat state file: %v", err)
	}

	// Should be 0640 as per our fix
	expectedPerm := os.FileMode(0640)
	if info.Mode().Perm() != expectedPerm {
		t.Errorf("State file permissions = %o, want %o", info.Mode().Perm(), expectedPerm)
	}
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Run concurrent reads and writes
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			mgr.SetLastCommitSHA("sha" + string(rune('0'+i%10)))
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = mgr.GetLastCommitSHA()
			_ = mgr.GetLastSyncTime()
			_ = mgr.GetLastError()
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// If we get here without deadlock or panic, the test passes
}

func TestGetLastSyncTime_ZeroValue(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	syncTime := mgr.GetLastSyncTime()
	if !syncTime.IsZero() {
		t.Errorf("GetLastSyncTime() = %v, want zero time", syncTime)
	}
}

func TestStateJSONFormat(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if err := mgr.SetLastCommitSHA("abc123"); err != nil {
		t.Fatalf("SetLastCommitSHA() error = %v", err)
	}

	// Read the raw file content
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("Failed to read state file: %v", err)
	}

	// Verify it's properly indented JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("State file is not valid JSON: %v", err)
	}

	// Check that expected fields exist
	if _, ok := parsed["last_commit_sha"]; !ok {
		t.Error("JSON missing last_commit_sha field")
	}
	if _, ok := parsed["last_sync_time"]; !ok {
		t.Error("JSON missing last_sync_time field")
	}
}

func TestNewManager_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Write invalid JSON
	if err := os.WriteFile(statePath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write invalid JSON: %v", err)
	}

	_, err := NewManager(statePath)
	if err == nil {
		t.Error("NewManager() should error on invalid JSON")
	}
}
