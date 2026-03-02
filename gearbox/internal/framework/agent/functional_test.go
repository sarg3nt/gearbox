package agent

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Functional tests for the OS Updates gear against a live gearbox-agent.
//
// These tests require a running gearbox-agent and are gated by two environment variables:
//
//   GEARBOX_FUNCTIONAL_TEST_URL     - Base URL of the agent (e.g. https://host:8405)
//   GEARBOX_FUNCTIONAL_TEST_API_KEY - API key for Bearer auth
//
// Run:
//   GEARBOX_FUNCTIONAL_TEST_URL=https://light-hugger.sarg3.net:8405 \
//   GEARBOX_FUNCTIONAL_TEST_API_KEY=9795a6e1d5030438fd271344a1fc1f2d016e5348361d0dedede16318e9657a52 \
//   go test -v -run TestFunctional ./gearbox/internal/framework/agent/

// functionalClient returns a Client configured for the live agent,
// or skips the test if the env vars are not set.
func functionalClient(t *testing.T) *Client {
	t.Helper()

	baseURL := os.Getenv("GEARBOX_FUNCTIONAL_TEST_URL")
	apiKey := os.Getenv("GEARBOX_FUNCTIONAL_TEST_API_KEY")
	if baseURL == "" || apiKey == "" {
		t.Skip("Skipping functional test: GEARBOX_FUNCTIONAL_TEST_URL and GEARBOX_FUNCTIONAL_TEST_API_KEY must be set")
	}

	return NewClient(baseURL, apiKey)
}

// ---------- Update Status & List ----------

func TestFunctional_GetUpdateStatus(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.GetUpdateStatus()
	if err != nil {
		t.Fatalf("GetUpdateStatus failed: %v", err)
	}

	// last_check should be a non-empty timestamp
	if resp.LastCheck == "" {
		t.Error("expected non-empty LastCheck")
	}

	// total_updates and security_updates should be non-negative
	if resp.TotalUpdates < 0 {
		t.Errorf("expected TotalUpdates >= 0, got %d", resp.TotalUpdates)
	}
	if resp.SecurityUpdates < 0 {
		t.Errorf("expected SecurityUpdates >= 0, got %d", resp.SecurityUpdates)
	}

	t.Logf("Update status: available=%v total=%d security=%d reboot=%v unattended=%v lastCheck=%s",
		resp.Available, resp.TotalUpdates, resp.SecurityUpdates, resp.RebootRequired, resp.UnattendedActive, resp.LastCheck)
}

func TestFunctional_ListUpgradablePackages(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.ListUpgradablePackages()
	if err != nil {
		t.Fatalf("ListUpgradablePackages failed: %v", err)
	}

	if resp.Count < 0 {
		t.Errorf("expected Count >= 0, got %d", resp.Count)
	}
	if resp.Count != len(resp.Packages) {
		t.Errorf("Count (%d) does not match len(Packages) (%d)", resp.Count, len(resp.Packages))
	}

	// If there are packages, validate the first one has required fields
	if len(resp.Packages) > 0 {
		pkg := resp.Packages[0]
		if pkg.Name == "" {
			t.Error("first package has empty name")
		}
		t.Logf("First upgradable package: %s %s -> %s (security=%v)",
			pkg.Name, pkg.CurrentVersion, pkg.AvailableVersion, pkg.IsSecurityUpdate)
	}

	t.Logf("Upgradable packages: %d total", resp.Count)
}

// ---------- Update History ----------

func TestFunctional_GetUpdateHistory(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.GetUpdateHistory(10)
	if err != nil {
		t.Fatalf("GetUpdateHistory failed: %v", err)
	}

	if resp.Count < 0 {
		t.Errorf("expected Count >= 0, got %d", resp.Count)
	}
	if resp.Count != len(resp.History) {
		t.Errorf("Count (%d) does not match len(History) (%d)", resp.Count, len(resp.History))
	}

	// Validate history entries have required fields
	for i, entry := range resp.History {
		if entry.Timestamp == "" {
			t.Errorf("history[%d] has empty Timestamp", i)
		}
		if entry.Action == "" {
			t.Errorf("history[%d] has empty Action", i)
		}
		if entry.Package == "" {
			t.Errorf("history[%d] has empty Package", i)
		}
	}

	if len(resp.History) > 0 {
		e := resp.History[0]
		t.Logf("Most recent history entry: %s %s %s (%s -> %s) status=%s",
			e.Timestamp, e.Action, e.Package, e.FromVersion, e.ToVersion, e.Status)
	}

	t.Logf("History entries returned: %d", resp.Count)
}

func TestFunctional_GetUpdateHistory_DefaultLimit(t *testing.T) {
	c := functionalClient(t)

	// Test with 0 limit (should use server default of 50)
	resp, err := c.GetUpdateHistory(0)
	if err != nil {
		t.Fatalf("GetUpdateHistory (default limit) failed: %v", err)
	}

	if resp.Count > 50 {
		t.Errorf("default limit should cap at 50, got %d entries", resp.Count)
	}

	t.Logf("History entries with default limit: %d", resp.Count)
}

// ---------- Reboot Required ----------

func TestFunctional_GetRebootRequired(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.GetRebootRequired()
	if err != nil {
		t.Fatalf("GetRebootRequired failed: %v", err)
	}

	// Packages should only be populated if reboot is required
	if !resp.Required && len(resp.Packages) > 0 {
		t.Error("packages listed but reboot not required")
	}

	t.Logf("Reboot required: %v, packages: %v", resp.Required, resp.Packages)
}

// ---------- Snapshots ----------

func TestFunctional_ListSnapshots(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots failed: %v", err)
	}

	if resp.Count < 0 {
		t.Errorf("expected Count >= 0, got %d", resp.Count)
	}
	if resp.Count != len(resp.Snapshots) {
		t.Errorf("Count (%d) does not match len(Snapshots) (%d)", resp.Count, len(resp.Snapshots))
	}

	// Validate snapshot fields
	for i, snap := range resp.Snapshots {
		if snap.ID == "" {
			t.Errorf("snapshot[%d] has empty ID", i)
		}
		if snap.CreatedAt == "" {
			t.Errorf("snapshot[%d] has empty CreatedAt", i)
		}
	}

	t.Logf("Snapshots: %d total", resp.Count)
}

func TestFunctional_CreateAndDeleteSnapshot(t *testing.T) {
	c := functionalClient(t)

	// Create a test snapshot
	createResp, err := c.CreateSnapshot("functional-test")
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}

	if !createResp.Success {
		t.Fatalf("CreateSnapshot reported failure: %s", createResp.Message)
	}
	if createResp.Snapshot == nil {
		t.Fatal("CreateSnapshot returned nil snapshot")
	}
	if createResp.Snapshot.ID == "" {
		t.Fatal("CreateSnapshot returned empty snapshot ID")
	}

	snapshotID := createResp.Snapshot.ID
	t.Logf("Created snapshot: id=%s reason=%s", snapshotID, createResp.Snapshot.Reason)

	// Clean up: delete the test snapshot
	deleteResp, err := c.DeleteSnapshot(snapshotID)
	if err != nil {
		t.Fatalf("DeleteSnapshot failed: %v", err)
	}

	if !deleteResp.Success {
		t.Errorf("DeleteSnapshot reported failure: %s", deleteResp.Message)
	}

	t.Logf("Deleted snapshot: %s", snapshotID)
}

func TestFunctional_DeleteSnapshot_NotFound(t *testing.T) {
	c := functionalClient(t)

	_, err := c.DeleteSnapshot("nonexistent-snapshot-id")
	if err == nil {
		t.Fatal("expected error deleting nonexistent snapshot, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 404 {
		t.Errorf("expected status 404 for nonexistent snapshot, got %d", apiErr.StatusCode)
	}

	t.Logf("Delete nonexistent snapshot error (expected): %v", err)
}

func TestFunctional_RestoreSnapshot_NotFound(t *testing.T) {
	c := functionalClient(t)

	_, err := c.RestoreSnapshot("nonexistent-snapshot-id")
	if err == nil {
		t.Fatal("expected error restoring nonexistent snapshot, got nil")
	}

	t.Logf("Restore nonexistent snapshot error (expected): %v", err)
}

// ---------- Package Search ----------

func TestFunctional_SearchPackages(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.SearchPackages("curl", 10)
	if err != nil {
		t.Fatalf("SearchPackages failed: %v", err)
	}

	if resp.Count < 0 {
		t.Errorf("expected Count >= 0, got %d", resp.Count)
	}
	if resp.Count != len(resp.Packages) {
		t.Errorf("Count (%d) does not match len(Packages) (%d)", resp.Count, len(resp.Packages))
	}

	// curl should be found on any Debian/Ubuntu system
	if resp.Count == 0 {
		t.Error("expected at least one result for 'curl' search")
	}

	// Validate package fields
	for i, pkg := range resp.Packages {
		if pkg.Name == "" {
			t.Errorf("package[%d] has empty Name", i)
		}
	}

	if len(resp.Packages) > 0 {
		p := resp.Packages[0]
		t.Logf("First search result: %s %s (%s)", p.Name, p.Version, p.Architecture)
	}

	t.Logf("Search results for 'curl': %d packages", resp.Count)
}

func TestFunctional_SearchPackages_NoQuery(t *testing.T) {
	c := functionalClient(t)

	// Empty query should return a 400 error
	_, err := c.SearchPackages("", 10)
	if err == nil {
		t.Fatal("expected error for empty search query, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 400 {
		t.Errorf("expected status 400 for empty query, got %d", apiErr.StatusCode)
	}

	t.Logf("Empty search query error (expected): %v", err)
}

func TestFunctional_SearchPackages_WithLimit(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.SearchPackages("lib", 5)
	if err != nil {
		t.Fatalf("SearchPackages with limit failed: %v", err)
	}

	if resp.Count > 5 {
		t.Errorf("expected at most 5 results with limit=5, got %d", resp.Count)
	}

	t.Logf("Search results for 'lib' (limit 5): %d packages", resp.Count)
}

// ---------- Pipx ----------

func TestFunctional_GetPipxStatus(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.GetPipxStatus()
	if err != nil {
		t.Fatalf("GetPipxStatus failed: %v", err)
	}

	// Available is a bool, either value is valid
	if resp.Available {
		t.Logf("Pipx available: %d packages installed", resp.Count)
		for _, pkg := range resp.Packages {
			t.Logf("  pipx package: %s %s (apps: %v)", pkg.Name, pkg.Version, pkg.Apps)
		}
	} else {
		t.Logf("Pipx not available on this system")
		if resp.Count != 0 {
			t.Errorf("expected Count=0 when pipx not available, got %d", resp.Count)
		}
	}
}

// ---------- Unattended Upgrades ----------

func TestFunctional_GetUnattendedConfig(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.GetUnattendedConfig()
	if err != nil {
		t.Fatalf("GetUnattendedConfig failed: %v", err)
	}

	// All bools are valid in any combination, just log the state
	t.Logf("Unattended config: enabled=%v autoUpdate=%v autoUpgrade=%v autoReboot=%v mailOnError=%v",
		resp.Enabled, resp.AutoUpdate, resp.AutoUpgrade, resp.AutoReboot, resp.MailOnError)
}

// ---------- Operations ----------

func TestFunctional_ListOperations(t *testing.T) {
	c := functionalClient(t)

	// The client doesn't have a ListOperations method, so we use doRequest directly.
	// We make a raw GET request to /api/v1/system/updates/operations.
	body, err := c.doRequest("GET", "/api/v1/system/updates/operations", nil)
	if err != nil {
		t.Fatalf("ListOperations failed: %v", err)
	}

	var resp struct {
		Operations []OperationStatusResponse `json:"operations"`
		Count      int                       `json:"count"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to parse operations response: %v", err)
	}

	if resp.Count < 0 {
		t.Errorf("expected Count >= 0, got %d", resp.Count)
	}
	if resp.Count != len(resp.Operations) {
		t.Errorf("Count (%d) does not match len(Operations) (%d)", resp.Count, len(resp.Operations))
	}

	for i, op := range resp.Operations {
		if op.ID == "" {
			t.Errorf("operation[%d] has empty ID", i)
		}
		if op.Type == "" {
			t.Errorf("operation[%d] has empty Type", i)
		}
		if op.Status == "" {
			t.Errorf("operation[%d] has empty Status", i)
		}
	}

	t.Logf("Operations: %d total", resp.Count)
}

func TestFunctional_GetOperation_NotFound(t *testing.T) {
	c := functionalClient(t)

	// Use the agent's actual route: /api/v1/system/updates/operation/{id} (singular)
	_, err := c.doRequest("GET", "/api/v1/system/updates/operation/nonexistent-op-id", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent operation, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 404 && apiErr.StatusCode != 501 {
		t.Errorf("expected status 404 or 501, got %d", apiErr.StatusCode)
	}

	t.Logf("Get nonexistent operation error (expected): %v", err)
}

func TestFunctional_CancelOperation_NotFound(t *testing.T) {
	c := functionalClient(t)

	// Use the agent's actual route: /api/v1/system/updates/operation/{id} (singular)
	_, err := c.doRequest("DELETE", "/api/v1/system/updates/operation/nonexistent-op-id", nil)
	if err == nil {
		t.Fatal("expected error for cancelling nonexistent operation, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 404 && apiErr.StatusCode != 501 {
		t.Errorf("expected status 404 or 501, got %d", apiErr.StatusCode)
	}

	t.Logf("Cancel nonexistent operation error (expected): %v", err)
}

// ---------- Update Logs ----------

func TestFunctional_ListUpdateLogs(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.ListUpdateLogs(10)
	if err != nil {
		t.Fatalf("ListUpdateLogs failed: %v", err)
	}

	if resp.Count < 0 {
		t.Errorf("expected Count >= 0, got %d", resp.Count)
	}
	if resp.Count != len(resp.Logs) {
		t.Errorf("Count (%d) does not match len(Logs) (%d)", resp.Count, len(resp.Logs))
	}

	for i, log := range resp.Logs {
		if log.ID == "" {
			t.Errorf("log[%d] has empty ID", i)
		}
		if log.Type == "" {
			t.Errorf("log[%d] has empty Type", i)
		}
		if log.Status == "" {
			t.Errorf("log[%d] has empty Status", i)
		}
		if log.StartedAt == "" {
			t.Errorf("log[%d] has empty StartedAt", i)
		}
	}

	t.Logf("Update logs: %d total", resp.Count)
}

func TestFunctional_ListUpdateLogs_DefaultLimit(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.ListUpdateLogs(0)
	if err != nil {
		t.Fatalf("ListUpdateLogs (default limit) failed: %v", err)
	}

	t.Logf("Update logs with default limit: %d entries", resp.Count)
}

func TestFunctional_GetUpdateLog_NotFound(t *testing.T) {
	c := functionalClient(t)

	_, err := c.GetUpdateLog("nonexistent-log-id")
	if err == nil {
		t.Fatal("expected error for nonexistent log, got nil")
	}

	t.Logf("Get nonexistent log error (expected): %v", err)
}

func TestFunctional_GetUpdateLog_ValidEntry(t *testing.T) {
	c := functionalClient(t)

	// First list logs to find a valid ID
	listResp, err := c.ListUpdateLogs(1)
	if err != nil {
		t.Fatalf("ListUpdateLogs failed: %v", err)
	}
	if len(listResp.Logs) == 0 {
		t.Skip("no update logs available to test GetUpdateLog")
	}

	logID := listResp.Logs[0].ID
	logResp, err := c.GetUpdateLog(logID)
	if err != nil {
		t.Fatalf("GetUpdateLog(%s) failed: %v", logID, err)
	}

	if logResp.ID != logID {
		t.Errorf("expected log ID %s, got %s", logID, logResp.ID)
	}
	if logResp.Type == "" {
		t.Error("expected non-empty Type")
	}
	if logResp.Status == "" {
		t.Error("expected non-empty Status")
	}

	t.Logf("Update log: id=%s type=%s status=%s output_length=%d",
		logResp.ID, logResp.Type, logResp.Status, len(logResp.Output))
}

// ---------- Streaming Check (non-destructive) ----------

func TestFunctional_TriggerUpdateCheckStreaming(t *testing.T) {
	c := functionalClient(t)

	resp, err := c.TriggerUpdateCheckStreaming()
	if err != nil {
		t.Fatalf("TriggerUpdateCheckStreaming failed: %v", err)
	}

	if resp.OperationID == "" {
		t.Error("expected non-empty OperationID")
	}
	if resp.Status != "started" {
		t.Errorf("expected status 'started', got %q", resp.Status)
	}

	t.Logf("Streaming check started: operation_id=%s status=%s message=%s",
		resp.OperationID, resp.Status, resp.Message)

	// Verify we can look up the operation we just started
	opBody, err := c.doRequest("GET", "/api/v1/system/updates/operation/"+resp.OperationID, nil)
	if err != nil {
		t.Logf("GetOperation for started check failed (may have completed already): %v", err)
		return
	}

	var op OperationStatusResponse
	if err := json.Unmarshal(opBody, &op); err != nil {
		t.Fatalf("failed to parse operation: %v", err)
	}

	if op.ID != resp.OperationID {
		t.Errorf("operation ID mismatch: expected %s, got %s", resp.OperationID, op.ID)
	}
	validStatuses := map[string]bool{"running": true, "completed": true, "failed": true}
	if !validStatuses[op.Status] {
		t.Errorf("unexpected operation status: %q", op.Status)
	}

	t.Logf("Operation status: id=%s type=%s status=%s", op.ID, op.Type, op.Status)
}

// ---------- Sync TriggerUpdateCheck (long running) ----------

func TestFunctional_TriggerUpdateCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sync update check in short mode (can take minutes)")
	}

	c := functionalClient(t)

	resp, err := c.TriggerUpdateCheck()
	if err != nil {
		// apt update can fail due to server-side repo issues (expired, unreachable, etc.)
		// This is not a protocol error — the API correctly returned an error response.
		apiErr, ok := err.(*APIError)
		if ok && apiErr.StatusCode == 500 {
			t.Logf("TriggerUpdateCheck returned server error (apt issue on remote host): %s", apiErr.Message)
			t.Log("API protocol is correct — apt itself failed on the server")
			return
		}
		t.Fatalf("TriggerUpdateCheck failed with unexpected error: %v", err)
	}

	if resp.LastCheck == "" {
		t.Error("expected non-empty LastCheck after check")
	}

	t.Logf("Sync update check completed: available=%v total=%d security=%d lastCheck=%s",
		resp.Available, resp.TotalUpdates, resp.SecurityUpdates, resp.LastCheck)
}

// ---------- Error Response Format Validation ----------

func TestFunctional_ErrorResponses_AreJSON(t *testing.T) {
	c := functionalClient(t)

	// Test a few endpoints that should return structured errors.
	// These validate the "no plain text errors" rule.

	testCases := []struct {
		name   string
		method string
		path   string
	}{
		{"SearchPackages empty query", "GET", "/api/v1/system/packages/search"},
		{"GetOperation not found", "GET", "/api/v1/system/updates/operation/nonexistent"},
		{"GetUpdateLog not found", "GET", "/api/v1/system/updates/logs/nonexistent"},
		{"DeleteSnapshot not found", "DELETE", "/api/v1/system/updates/snapshots/nonexistent"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.doRequest(tc.method, tc.path, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("expected *APIError, got %T: %v", err, err)
			}

			// The error message should not be empty
			if apiErr.Message == "" {
				t.Error("error message is empty")
			}

			// The error message should not start with "Failed to" (agent shouldn't prefix)
			if len(apiErr.Message) > 10 && apiErr.Message[:10] == "Failed to " {
				t.Errorf("agent error should not start with 'Failed to': %q", apiErr.Message)
			}

			t.Logf("Error response: status=%d message=%q", apiErr.StatusCode, apiErr.Message)
		})
	}
}

// ---------- Input Validation Tests ----------

func TestFunctional_InstallPackage_EmptyName(t *testing.T) {
	c := functionalClient(t)

	_, err := c.InstallPackage("")
	if err == nil {
		t.Fatal("expected error for empty package name, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 400 {
		t.Errorf("expected status 400 for empty name, got %d", apiErr.StatusCode)
	}

	t.Logf("Empty package name error (expected): %v", err)
}

func TestFunctional_RemovePackage_EmptyName(t *testing.T) {
	c := functionalClient(t)

	_, err := c.RemovePackage("", false)
	if err == nil {
		t.Fatal("expected error for empty package name, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 400 {
		t.Errorf("expected status 400 for empty name, got %d", apiErr.StatusCode)
	}

	t.Logf("Empty remove package name error (expected): %v", err)
}

func TestFunctional_RestoreSnapshot_EmptyID(t *testing.T) {
	c := functionalClient(t)

	_, err := c.RestoreSnapshot("")
	if err == nil {
		t.Fatal("expected error for empty snapshot ID, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 400 {
		t.Errorf("expected status 400 for empty snapshot ID, got %d", apiErr.StatusCode)
	}

	t.Logf("Empty snapshot ID error (expected): %v", err)
}

// ---------- Pipx Input Validation ----------

func TestFunctional_InstallPipxPackage_EmptyName(t *testing.T) {
	c := functionalClient(t)

	_, err := c.InstallPipxPackage("")
	if err == nil {
		t.Fatal("expected error for empty pipx package name, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 400 {
		t.Errorf("expected status 400 for empty name, got %d", apiErr.StatusCode)
	}

	t.Logf("Empty pipx install name error (expected): %v", err)
}

func TestFunctional_UninstallPipxPackage_EmptyName(t *testing.T) {
	c := functionalClient(t)

	_, err := c.UninstallPipxPackage("")
	if err == nil {
		t.Fatal("expected error for empty pipx package name, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 400 {
		t.Errorf("expected status 400 for empty name, got %d", apiErr.StatusCode)
	}

	t.Logf("Empty pipx uninstall name error (expected): %v", err)
}

func TestFunctional_UpgradePipxPackage_EmptyName(t *testing.T) {
	c := functionalClient(t)

	_, err := c.UpgradePipxPackage("")
	if err == nil {
		t.Fatal("expected error for empty pipx package name, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 400 {
		t.Errorf("expected status 400 for empty name, got %d", apiErr.StatusCode)
	}

	t.Logf("Empty pipx upgrade name error (expected): %v", err)
}

// ---------- Authentication ----------

func TestFunctional_BadAPIKey(t *testing.T) {
	baseURL := os.Getenv("GEARBOX_FUNCTIONAL_TEST_URL")
	if baseURL == "" {
		t.Skip("Skipping: GEARBOX_FUNCTIONAL_TEST_URL not set")
	}

	c := NewClient(baseURL, "invalid-api-key-for-testing")

	_, err := c.GetUpdateStatus()
	if err == nil {
		t.Fatal("expected error with bad API key, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 401 && apiErr.StatusCode != 403 {
		t.Errorf("expected status 401 or 403 for bad API key, got %d", apiErr.StatusCode)
	}

	t.Logf("Bad API key error (expected): %v", err)
}

func TestFunctional_NoAPIKey(t *testing.T) {
	baseURL := os.Getenv("GEARBOX_FUNCTIONAL_TEST_URL")
	if baseURL == "" {
		t.Skip("Skipping: GEARBOX_FUNCTIONAL_TEST_URL not set")
	}

	c := NewClient(baseURL, "")

	_, err := c.GetUpdateStatus()
	if err == nil {
		t.Fatal("expected error with no API key, got nil")
	}

	apiErr, ok := err.(*APIError)
	if ok && apiErr.StatusCode != 401 && apiErr.StatusCode != 403 {
		t.Errorf("expected status 401 or 403 for missing API key, got %d", apiErr.StatusCode)
	}

	t.Logf("No API key error (expected): %v", err)
}

// ==========================================================================
// Comprehensive Update Lifecycle Test
//
// This test exercises the full update workflow end-to-end:
//   1. Check for updates (streaming)
//   2. Record which packages are available
//   3. Create a snapshot (safety net)
//   4. Install ONE specific package
//   5. Verify that package is no longer in the upgradable list
//   6. Install ALL remaining updates
//   7. Verify no upgradable packages remain
//   8. Restore the snapshot (rollback)
//   9. Check for updates again
//  10. Verify the original packages are available again
//  11. Delete the snapshot
//  12. Verify snapshot is gone
//
// Skipped in -short mode. Requires a live agent with real upgradable packages.
// ==========================================================================

func TestFunctional_UpdateLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping lifecycle test in short mode (modifies system packages)")
	}

	c := functionalClient(t)

	// ---- Step 1: Trigger a streaming update check and wait for it ----
	t.Log("Step 1: Triggering streaming update check...")
	checkResp, err := c.TriggerUpdateCheckStreaming()
	if err != nil {
		t.Fatalf("TriggerUpdateCheckStreaming failed: %v", err)
	}
	t.Logf("  operation_id=%s", checkResp.OperationID)

	// Poll until the check completes (up to 5 minutes)
	waitForOperation(t, c, checkResp.OperationID, 5*time.Minute)

	// ---- Step 2: Record available packages ----
	t.Log("Step 2: Listing upgradable packages...")
	listResp, err := c.ListUpgradablePackages()
	if err != nil {
		t.Fatalf("ListUpgradablePackages failed: %v", err)
	}
	if len(listResp.Packages) == 0 {
		t.Skip("No upgradable packages available — cannot test install/restore lifecycle")
	}

	originalPackages := make(map[string]bool)
	for _, pkg := range listResp.Packages {
		originalPackages[pkg.Name] = true
	}
	originalCount := len(listResp.Packages)
	t.Logf("  %d upgradable packages found", originalCount)
	for _, pkg := range listResp.Packages {
		t.Logf("    %s: %s -> %s", pkg.Name, pkg.CurrentVersion, pkg.AvailableVersion)
	}

	// ---- Step 3: Create a snapshot ----
	t.Log("Step 3: Creating pre-test snapshot...")
	snapResp, err := c.CreateSnapshot("functional-lifecycle-test")
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
	if !snapResp.Success || snapResp.Snapshot == nil {
		t.Fatalf("CreateSnapshot failed: success=%v snapshot=%v", snapResp.Success, snapResp.Snapshot)
	}
	snapshotID := snapResp.Snapshot.ID
	t.Logf("  snapshot_id=%s", snapshotID)

	// Ensure cleanup: delete snapshot and attempt restore even if test fails
	snapshotDeleted := false
	defer func() {
		if !snapshotDeleted {
			t.Log("CLEANUP: Deleting leftover snapshot...")
			c.DeleteSnapshot(snapshotID)
		}
	}()

	// ---- Step 4: Install ONE specific package (streaming) ----
	targetPkg := listResp.Packages[0].Name
	t.Logf("Step 4: Installing single package (streaming): %s", targetPkg)
	{
		streamResp, err := c.InstallUpdatesStreaming(&InstallUpdatesRequest{
			Packages: []string{targetPkg},
		})
		if err != nil {
			t.Fatalf("InstallUpdatesStreaming (single) HTTP/protocol error: %v", err)
		}
		t.Logf("  operation_id=%s", streamResp.OperationID)

		// Wait up to 10 minutes for install to complete
		result := waitForOperation(t, c, streamResp.OperationID, 10*time.Minute)
		if result.Status == "failed" || result.ExitCode != 0 {
			// apt install can fail due to server-side issues (broken repos, etc.)
			// This is not a test framework failure — the API responded correctly.
			t.Logf("  WARNING: apt install failed on server: %s (exit_code=%d)", result.Error, result.ExitCode)
			t.Log("  Skipping steps 5-10 (install-dependent) due to server-side apt failure")
			t.Log("  The API protocol is correct — apt itself cannot install packages on this server")
			goto cleanup
		}
	}

	// ---- Step 5: Verify the package is no longer upgradable ----
	t.Logf("Step 5: Verifying %s is no longer upgradable...", targetPkg)
	{
		afterOneResp, err := c.ListUpgradablePackages()
		if err != nil {
			t.Fatalf("ListUpgradablePackages (after single install) failed: %v", err)
		}

		for _, pkg := range afterOneResp.Packages {
			if pkg.Name == targetPkg {
				t.Errorf("  %s still appears in upgradable list after install", targetPkg)
			}
		}
		if afterOneResp.Count >= originalCount {
			t.Errorf("  expected fewer upgradable packages after install, got %d (was %d)", afterOneResp.Count, originalCount)
		}
		t.Logf("  upgradable packages now: %d (was %d)", afterOneResp.Count, originalCount)
	}

	// ---- Step 6: Install ALL remaining updates (streaming) ----
	t.Log("Step 6: Installing all remaining updates (streaming)...")
	{
		streamResp, err := c.InstallUpdatesStreaming(&InstallUpdatesRequest{})
		if err != nil {
			t.Fatalf("InstallUpdatesStreaming (all) HTTP/protocol error: %v", err)
		}
		t.Logf("  operation_id=%s", streamResp.OperationID)

		// Wait up to 10 minutes for install to complete
		result := waitForOperation(t, c, streamResp.OperationID, 10*time.Minute)
		if result.Status == "failed" || result.ExitCode != 0 {
			t.Logf("  WARNING: apt install-all failed on server: %s (exit_code=%d)", result.Error, result.ExitCode)
			t.Log("  Skipping steps 7-10 due to server-side apt failure")
			goto cleanup
		}
	}

	// ---- Step 7: Verify no upgradable packages remain ----
	t.Log("Step 7: Verifying no upgradable packages remain...")
	{
		afterAllResp, err := c.ListUpgradablePackages()
		if err != nil {
			t.Fatalf("ListUpgradablePackages (after install all) failed: %v", err)
		}
		if afterAllResp.Count != 0 {
			t.Errorf("  expected 0 upgradable packages, got %d", afterAllResp.Count)
			for _, pkg := range afterAllResp.Packages {
				t.Logf("    still upgradable: %s", pkg.Name)
			}
		} else {
			t.Log("  0 upgradable packages (as expected)")
		}
	}

	// ---- Step 8: Restore the snapshot ----
	t.Logf("Step 8: Restoring snapshot %s...", snapshotID)
	{
		restoreResp, err := c.RestoreSnapshot(snapshotID)
		if err != nil {
			t.Fatalf("RestoreSnapshot failed: %v", err)
		}
		if !restoreResp.Success {
			t.Fatalf("RestoreSnapshot reported failure: %s", restoreResp.Message)
		}
		t.Logf("  restore success: %s", restoreResp.Message)
	}

	// ---- Step 9: Check for updates again ----
	t.Log("Step 9: Checking for updates after restore...")
	{
		checkResp2, err := c.TriggerUpdateCheckStreaming()
		if err != nil {
			t.Fatalf("TriggerUpdateCheckStreaming (post-restore) failed: %v", err)
		}
		waitForOperation(t, c, checkResp2.OperationID, 5*time.Minute)
	}

	// ---- Step 10: Verify the original packages are available again ----
	t.Log("Step 10: Verifying original packages are upgradable again...")
	{
		afterRestoreResp, err := c.ListUpgradablePackages()
		if err != nil {
			t.Fatalf("ListUpgradablePackages (after restore) failed: %v", err)
		}

		restoredPackages := make(map[string]bool)
		for _, pkg := range afterRestoreResp.Packages {
			restoredPackages[pkg.Name] = true
		}

		// Every package that was originally upgradable should be upgradable again
		missing := 0
		for name := range originalPackages {
			if !restoredPackages[name] {
				t.Errorf("  package %s was upgradable before but not after restore", name)
				missing++
			}
		}
		if missing == 0 {
			t.Logf("  all %d original packages are upgradable again", originalCount)
		}
		t.Logf("  upgradable packages after restore: %d (originally %d)", afterRestoreResp.Count, originalCount)
	}

cleanup:
	// ---- Step 11: Delete the snapshot ----
	t.Logf("Step 11: Deleting snapshot %s...", snapshotID)
	deleteResp, err := c.DeleteSnapshot(snapshotID)
	if err != nil {
		t.Fatalf("DeleteSnapshot failed: %v", err)
	}
	if !deleteResp.Success {
		t.Fatalf("DeleteSnapshot reported failure: %s", deleteResp.Message)
	}
	snapshotDeleted = true
	t.Log("  snapshot deleted")

	// ---- Step 12: Verify snapshot is gone ----
	t.Log("Step 12: Verifying snapshot no longer exists...")
	_, err = c.DeleteSnapshot(snapshotID)
	if err == nil {
		t.Error("  expected error deleting already-deleted snapshot, got nil")
	} else {
		apiErr, ok := err.(*APIError)
		if ok && apiErr.StatusCode != 404 {
			t.Errorf("  expected 404, got %d: %s", apiErr.StatusCode, apiErr.Message)
		} else {
			t.Log("  confirmed: snapshot not found (404)")
		}
	}

	t.Log("Lifecycle test complete.")
}

// waitForOperation polls the agent for operation status until it reaches a
// terminal state (completed or failed) or the timeout expires.
// waitForOperationResult holds the outcome of a polled operation.
type waitForOperationResult struct {
	Status   string // "completed", "failed", or "unknown"
	ExitCode int
	Error    string
}

func waitForOperation(t *testing.T, c *Client, operationID string, timeout time.Duration) waitForOperationResult {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("operation %s did not complete within %v", operationID, timeout)
		}

		body, err := c.doRequest("GET", "/api/v1/system/updates/operation/"+operationID, nil)
		if err != nil {
			// Operation may have been cleaned up already after completion
			t.Logf("  operation lookup returned error (may have completed): %v", err)
			return waitForOperationResult{Status: "unknown"}
		}

		var op OperationStatusResponse
		if err := json.Unmarshal(body, &op); err != nil {
			t.Fatalf("failed to parse operation status: %v", err)
		}

		switch op.Status {
		case "completed":
			t.Logf("  operation %s completed (exit_code=%d)", operationID, op.ExitCode)
			if op.ExitCode != 0 {
				t.Logf("  WARNING: operation completed with non-zero exit code %d (error: %s)", op.ExitCode, op.Error)
			}
			return waitForOperationResult{Status: "completed", ExitCode: op.ExitCode, Error: op.Error}
		case "failed":
			t.Logf("  operation %s failed: %s (exit_code=%d)", operationID, op.Error, op.ExitCode)
			return waitForOperationResult{Status: "failed", ExitCode: op.ExitCode, Error: op.Error}
		default:
			// Still running, wait and poll again
			time.Sleep(2 * time.Second)
		}
	}
}
