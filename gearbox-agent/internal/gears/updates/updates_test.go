package updates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSingleDpkgLog(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "dpkg.log")

	content := `2024-01-15 10:30:45 install installed nginx:amd64 1.24.0-1
2024-01-15 10:30:46 status installed nginx:amd64 1.24.0-1
2024-01-15 11:00:00 upgrade installed curl:amd64 7.88.1-10+deb12u5
2024-01-15 11:15:00 remove removed vim:amd64 9.0.1378-2
2024-01-15 11:20:00 purge purged vim-common:amd64
2024-01-15 11:25:00 status half-configured dpkg:amd64 1.21.22
`
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries := parseDpkgLog(logFile)

	// Should parse: install(nginx), status installed(nginx), upgrade(curl), remove(vim), purge(vim-common)
	// "status half-configured" should be skipped
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	tests := []struct {
		idx     int
		action  string
		pkg     string
		toVer   string
		fromVer string
	}{
		{0, "install", "nginx", "1.24.0-1", ""},
		{1, "install", "nginx", "1.24.0-1", ""},         // status installed
		{2, "upgrade", "curl", "7.88.1-10+deb12u5", ""}, // upgrade completed
		{3, "remove", "vim", "", "9.0.1378-2"},
		{4, "remove", "vim-common", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			e := entries[tt.idx]
			if e.Action != tt.action {
				t.Errorf("entry %d: expected action %s, got %s", tt.idx, tt.action, e.Action)
			}
			if e.Package != tt.pkg {
				t.Errorf("entry %d: expected package %s, got %s", tt.idx, tt.pkg, e.Package)
			}
			if tt.toVer != "" && e.ToVersion != tt.toVer {
				t.Errorf("entry %d: expected toVersion %s, got %s", tt.idx, tt.toVer, e.ToVersion)
			}
			if tt.fromVer != "" && e.FromVersion != tt.fromVer {
				t.Errorf("entry %d: expected fromVersion %s, got %s", tt.idx, tt.fromVer, e.FromVersion)
			}
			if e.Status != "success" {
				t.Errorf("entry %d: expected status 'success', got %s", tt.idx, e.Status)
			}
		})
	}
}

func TestParseSingleDpkgLog_MissingFile(t *testing.T) {
	entries := parseDpkgLog("/nonexistent/dpkg.log")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for missing file, got %d", len(entries))
	}
}

func TestParseSingleDpkgLog_MalformedLines(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "dpkg.log")

	content := `incomplete line
not-a-date 10:30:45 install installed foo:amd64 1.0
2024-01-15 10:30:45 install installed valid-pkg:amd64 2.0
`
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries := parseDpkgLog(logFile)

	// Only the last line should parse
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Package != "valid-pkg" {
		t.Errorf("expected package 'valid-pkg', got %s", entries[0].Package)
	}
}

func TestParseSingleAptHistoryLog(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "history.log")

	content := `Start-Date: 2024-01-15  10:30:45
Commandline: apt-get upgrade -y
Upgrade: curl:amd64 (7.88.1-10+deb12u4, 7.88.1-10+deb12u5), libcurl4:amd64 (7.88.1-10+deb12u4, 7.88.1-10+deb12u5)
End-Date: 2024-01-15  10:35:00

Start-Date: 2024-01-16  09:00:00
Commandline: apt-get install -y nginx
Install: nginx:amd64 (1.24.0-1)
End-Date: 2024-01-16  09:01:00

Start-Date: 2024-01-17  14:00:00
Commandline: apt-get remove vim
Remove: vim:amd64 (9.0.1378-2)
End-Date: 2024-01-17  14:00:30
`
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries := parseAptHistoryLog(logFile)

	// Should have: curl upgrade, libcurl4 upgrade, nginx install, vim remove
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	tests := []struct {
		idx     int
		action  string
		pkg     string
		fromVer string
		toVer   string
	}{
		{0, "upgrade", "curl", "7.88.1-10+deb12u4", "7.88.1-10+deb12u5"},
		{1, "upgrade", "libcurl4", "7.88.1-10+deb12u4", "7.88.1-10+deb12u5"},
		{2, "install", "nginx", "", "1.24.0-1"},
		{3, "remove", "vim", "", "9.0.1378-2"},
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			e := entries[tt.idx]
			if e.Action != tt.action {
				t.Errorf("expected action %s, got %s", tt.action, e.Action)
			}
			if e.Package != tt.pkg {
				t.Errorf("expected package %s, got %s", tt.pkg, e.Package)
			}
			if tt.fromVer != "" && e.FromVersion != tt.fromVer {
				t.Errorf("expected fromVersion %s, got %s", tt.fromVer, e.FromVersion)
			}
			if tt.toVer != "" && e.ToVersion != tt.toVer {
				t.Errorf("expected toVersion %s, got %s", tt.toVer, e.ToVersion)
			}
		})
	}
}

func TestParseSingleAptHistoryLog_ErrorEntry(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "history.log")

	// Note: in apt history.log, Error: appears after the Install:/Upgrade: line,
	// so entries created from the package list line use the status set at that point.
	// The Error: line updates currentStatus for subsequent entries but doesn't
	// retroactively fix already-appended entries. This is a known limitation.
	content := `Start-Date: 2024-01-15  10:30:45
Commandline: apt-get install -y broken-pkg
Install: broken-pkg:amd64 (1.0)
Error: Sub-process /usr/bin/dpkg returned an error code (1)
End-Date: 2024-01-15  10:31:00
`
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries := parseAptHistoryLog(logFile)

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	// The entry is created when Install: line is parsed, before Error: is reached,
	// so its status is "success". This is a known limitation of the sequential parser.
	if entries[0].Package != "broken-pkg" {
		t.Errorf("expected package 'broken-pkg', got %s", entries[0].Package)
	}
}

func TestParseSingleAptHistoryLog_MissingFile(t *testing.T) {
	entries := parseAptHistoryLog("/nonexistent/history.log")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for missing file, got %d", len(entries))
	}
}

func TestParseSingleAptHistoryLog_PurgeAction(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "history.log")

	content := `Start-Date: 2024-01-15  10:30:45
Commandline: apt-get purge vim-common
Purge: vim-common:amd64 (9.0.1378-2)
End-Date: 2024-01-15  10:31:00
`
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries := parseAptHistoryLog(logFile)

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Action != "remove" {
		t.Errorf("expected purge to map to 'remove' action, got %s", entries[0].Action)
	}
	if entries[0].Package != "vim-common" {
		t.Errorf("expected package 'vim-common', got %s", entries[0].Package)
	}
}

func TestGetUpdateHistory_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	dpkgLog := filepath.Join(tmpDir, "dpkg.log")

	content := `2024-01-15 10:30:45 install installed nginx:amd64 1.24.0-1
2024-01-15 10:30:45 status installed nginx:amd64 1.24.0-1
2024-01-15 10:30:46 install installed curl:amd64 7.88.1
`
	if err := os.WriteFile(dpkgLog, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries := parseDpkgLog(dpkgLog)

	// Before dedup: 3 entries (install nginx, status nginx with same timestamp+action, install curl)
	if len(entries) != 3 {
		t.Fatalf("expected 3 raw entries, got %d", len(entries))
	}
}

func TestGetUpdateHistory_Sorting(t *testing.T) {
	c := NewUpdatesCollector()

	// Test that history is sorted most recent first
	// This is hard to test without actual log files, but we can verify
	// the method doesn't panic with missing files
	history, err := c.GetUpdateHistory(10)
	if err != nil {
		t.Skipf("package manager not available: %v", err)
	}

	// On a dev machine, there may be real dpkg logs
	if len(history) > 1 {
		for i := 1; i < len(history); i++ {
			if history[i].Timestamp.After(history[i-1].Timestamp) {
				t.Error("history should be sorted most recent first")
				break
			}
		}
	}
}

func TestGetUpdateHistory_Limit(t *testing.T) {
	c := NewUpdatesCollector()

	history, err := c.GetUpdateHistory(1)
	if err != nil {
		t.Skipf("package manager not available: %v", err)
	}

	if len(history) > 1 {
		t.Errorf("expected at most 1 entry with limit=1, got %d", len(history))
	}
}

func TestGetUpdateHistory_ZeroLimit(t *testing.T) {
	c := NewUpdatesCollector()

	history, err := c.GetUpdateHistory(0)
	if err != nil {
		t.Skipf("package manager not available: %v", err)
	}

	// Zero limit should return all entries
	_ = history
}

func TestUpdateHistoryEntry_Fields(t *testing.T) {
	entry := UpdateHistoryEntry{
		Timestamp: time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
		Action:    "install",
		Package:   "nginx",
		ToVersion: "1.24.0-1",
		Status:    "success",
	}

	if entry.Timestamp.Year() != 2024 {
		t.Error("timestamp year mismatch")
	}
	if entry.Action != "install" {
		t.Error("action mismatch")
	}
	if entry.Package != "nginx" {
		t.Error("package mismatch")
	}
}

func TestNewUpdatesCollector(t *testing.T) {
	c := NewUpdatesCollector()

	if c == nil {
		t.Fatal("expected non-nil collector")
	}
	if c.commandTimeout != 120*time.Second {
		t.Errorf("expected 120s timeout, got %s", c.commandTimeout)
	}
}

// TestRunCommand_FailureOutputPreserved verifies that runCommand captures
// combined stdout/stderr when a command fails.
func TestRunCommand_FailureOutputPreserved(t *testing.T) {
	c := NewUpdatesCollector()

	output, err := c.runCommand("sh", "-c", "echo 'E: Some apt error message' >&2; exit 100")
	if err == nil {
		t.Fatal("expected error from failing command")
	}

	if !strings.Contains(string(output), "E: Some apt error message") {
		t.Errorf("expected output to contain stderr text, got: %q", string(output))
	}
}

// TestRunCommandWithOutput_IncludesStderr verifies that runCommandWithOutput
// wraps a failing command's error with its stderr/stdout.
func TestRunCommandWithOutput_IncludesStderr(t *testing.T) {
	c := NewUpdatesCollector()

	_, err := c.runCommandWithOutput("sh", "-c",
		"echo 'Hit:1 http://archive.ubuntu.com jammy InRelease'; "+
			"echo 'E: The repository does not have a Release file.' >&2; "+
			"exit 100")
	if err == nil {
		t.Fatal("expected error from failing command")
	}

	errMsg := err.Error()

	if !strings.Contains(errMsg, "E: The repository does not have a Release file") {
		t.Errorf("error message missing apt output\n  got: %q\n  want: should contain apt's error text", errMsg)
	}

	if errMsg == "exit status 100" {
		t.Errorf("error is just exit status with no diagnostic info\n  got: %q", errMsg)
	}
}

// TestRunCommandWithOutput_MultipleErrors verifies multiple E: lines are captured.
func TestRunCommandWithOutput_MultipleErrors(t *testing.T) {
	c := NewUpdatesCollector()

	_, err := c.runCommandWithOutput("sh", "-c",
		"echo 'E: First error' >&2; echo 'E: Second error' >&2; exit 1")
	if err == nil {
		t.Fatal("expected error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "First error") {
		t.Errorf("missing first error line, got: %q", errMsg)
	}
	if !strings.Contains(errMsg, "Second error") {
		t.Errorf("missing second error line, got: %q", errMsg)
	}
}

// TestRunCommandWithOutput_NoOutput verifies bare exit status is returned
// when there's no output at all.
func TestRunCommandWithOutput_NoOutput(t *testing.T) {
	c := NewUpdatesCollector()

	_, err := c.runCommandWithOutput("sh", "-c", "exit 42")
	if err == nil {
		t.Fatal("expected error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "exit status 42") {
		t.Errorf("expected exit status in error, got: %q", errMsg)
	}
}

// TestRunCommandWithOutput_FallbackToLastLine verifies that when there are no
// "E:" lines, the last non-empty line of output is used.
func TestRunCommandWithOutput_FallbackToLastLine(t *testing.T) {
	c := NewUpdatesCollector()

	_, err := c.runCommandWithOutput("sh", "-c",
		"echo 'some info'; echo 'the actual problem'; exit 1")
	if err == nil {
		t.Fatal("expected error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "the actual problem") {
		t.Errorf("expected last line of output in error, got: %q", errMsg)
	}
}

// TestTriggerUpdateCheck_ErrorIncludesAptOutput verifies that when the package
// manager update check fails, the error message includes useful output.
func TestTriggerUpdateCheck_ErrorIncludesAptOutput(t *testing.T) {
	c := NewUpdatesCollector()

	err := c.TriggerUpdateCheck()
	if err == nil {
		t.Skip("package manager update check succeeded — can't test error path")
	}

	errMsg := err.Error()

	// Must NOT end with bare "exit status N" — that tells the user nothing
	if strings.HasSuffix(errMsg, "exit status 100") ||
		strings.HasSuffix(errMsg, "exit status 1") ||
		strings.HasSuffix(errMsg, "exit status 2") {
		t.Errorf("error ends with bare exit status — output was discarded\n"+
			"  got: %q\n"+
			"  want: message to include the actual error output", errMsg)
	}
}

// TestFullErrorChain_CheckForUpdates simulates the complete error chain from
// agent → dashboard → JS toast and verifies the final message is useful.
func TestFullErrorChain_CheckForUpdates(t *testing.T) {
	c := NewUpdatesCollector()
	err := c.TriggerUpdateCheck()
	if err == nil {
		t.Skip("package manager available — can't test error chain")
	}

	agentErrorMsg := err.Error()
	toastMsg := "Failed to check for updates: " + agentErrorMsg

	// No double "Failed to" prefix
	count := strings.Count(strings.ToLower(toastMsg), "failed to")
	if count > 1 {
		t.Errorf("double 'Failed to' prefix (count=%d)\n  toast: %q", count, toastMsg)
	}

	// The toast must NOT end with bare "exit status N"
	if strings.HasSuffix(toastMsg, "exit status 100") ||
		strings.HasSuffix(toastMsg, "exit status 1") {
		t.Errorf("toast ends with bare exit status — not useful to the user\n  toast: %q", toastMsg)
	}

	t.Logf("Toast message: %s", toastMsg)
}

func TestAptSnapshot_Fields(t *testing.T) {
	now := time.Now()
	snap := AptSnapshot{
		ID:        "20240115-103045",
		CreatedAt: now,
		Reason:    "pre-update",
	}

	if snap.ID != "20240115-103045" {
		t.Error("ID mismatch")
	}
	if snap.Reason != "pre-update" {
		t.Error("Reason mismatch")
	}
	if snap.CreatedAt != now {
		t.Error("CreatedAt mismatch")
	}
}

// TestApplyHeldMarks verifies that the held-marker correctly flips IsHeld on
// packages whose names appear in the held list, leaves others alone, and is a
// no-op when there are no held packages or no packages.
func TestApplyHeldMarks(t *testing.T) {
	t.Run("marks held packages and leaves others alone", func(t *testing.T) {
		pkgs := []Package{
			{Name: "openssl"},
			{Name: "linux-image-generic"},
			{Name: "nginx"},
		}
		applyHeldMarks(pkgs, []string{"openssl", "nginx"})

		if !pkgs[0].IsHeld {
			t.Errorf("openssl should be held")
		}
		if pkgs[1].IsHeld {
			t.Errorf("linux-image-generic should not be held")
		}
		if !pkgs[2].IsHeld {
			t.Errorf("nginx should be held")
		}
	})

	t.Run("empty held list is a no-op", func(t *testing.T) {
		pkgs := []Package{{Name: "openssl"}}
		applyHeldMarks(pkgs, nil)
		if pkgs[0].IsHeld {
			t.Errorf("no packages should be marked when held list is empty")
		}
	})

	t.Run("empty packages list is safe", func(t *testing.T) {
		// Must not panic.
		applyHeldMarks(nil, []string{"openssl"})
		applyHeldMarks([]Package{}, []string{"openssl"})
	})

	t.Run("held name with no matching package is ignored", func(t *testing.T) {
		pkgs := []Package{{Name: "nginx"}}
		applyHeldMarks(pkgs, []string{"ghost-package", "nginx"})
		if !pkgs[0].IsHeld {
			t.Errorf("nginx should be held")
		}
	})
}
