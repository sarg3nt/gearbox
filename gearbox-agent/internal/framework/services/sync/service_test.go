package sync

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/services/haproxy"
)

func TestTruncateSHA(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc1234567890", "abc1234"},
		{"abcdefg", "abcdefg"},
		{"abcdef", "abcdef"},
		{"abc", "abc"},
		{"", ""},
		{"1234567", "1234567"},
		{"12345678", "1234567"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateSHA(tt.input)
			if got != tt.want {
				t.Errorf("truncateSHA(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestService_TriggerSync(t *testing.T) {
	// Create a minimal service with just the trigger channel
	svc := &Service{
		triggerChan: make(chan struct{}, 1),
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// First trigger should succeed
	svc.TriggerSync()

	// Verify something was sent to the channel
	select {
	case <-svc.triggerChan:
		// Good - trigger was received
	default:
		t.Error("TriggerSync() did not send to channel")
	}
}

func TestService_TriggerSync_NonBlocking(t *testing.T) {
	// Create a minimal service with buffered channel
	svc := &Service{
		triggerChan: make(chan struct{}, 1),
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// First trigger fills the buffer
	svc.TriggerSync()

	// Second trigger should not block (just logs and skips)
	done := make(chan bool, 1)
	go func() {
		svc.TriggerSync()
		done <- true
	}()

	// If it blocks, test will timeout
	select {
	case <-done:
		// Good - did not block
	case <-time.After(100 * time.Millisecond):
		t.Error("TriggerSync() blocked on full channel")
	}
}

func TestService_GetTriggerChan(t *testing.T) {
	svc := &Service{
		triggerChan: make(chan struct{}, 1),
	}

	ch := svc.GetTriggerChan()
	if ch == nil {
		t.Error("GetTriggerChan() returned nil")
	}

	// Verify it's the same channel
	svc.triggerChan <- struct{}{}
	select {
	case <-ch:
		// Good - received from the returned channel
	default:
		t.Error("GetTriggerChan() did not return the correct channel")
	}
}

func TestService_GetMetadata_NilInitially(t *testing.T) {
	svc := &Service{}

	meta := svc.GetMetadata()
	if meta != nil {
		t.Errorf("GetMetadata() = %v, want nil initially", meta)
	}
}

func TestService_GetMetadata_ThreadSafe(t *testing.T) {
	svc := &Service{}

	// Set metadata
	testMeta := &haproxy.Metadata{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	svc.mu.Lock()
	svc.currentMetadata = testMeta
	svc.mu.Unlock()

	// Read concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_ = svc.GetMetadata()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestService_GetLastError_NilInitially(t *testing.T) {
	svc := &Service{}

	err := svc.GetLastError()
	if err != nil {
		t.Errorf("GetLastError() = %v, want nil initially", err)
	}
}

func TestService_SetEventBus(t *testing.T) {
	svc := &Service{}

	if svc.eventBus != nil {
		t.Error("eventBus should be nil initially")
	}

	bus := events.NewBus()
	defer bus.Close()

	svc.SetEventBus(bus)

	if svc.eventBus == nil {
		t.Error("SetEventBus() did not set the event bus")
	}
}

func TestService_PublishEvent_NilBus(t *testing.T) {
	svc := &Service{}

	// Should not panic when eventBus is nil
	svc.publishEvent(events.EventSyncStarted, map[string]interface{}{
		"test": "value",
	})
}

func TestService_PublishEvent_WithBus(t *testing.T) {
	svc := &Service{}
	bus := events.NewBus()
	defer bus.Close()

	svc.SetEventBus(bus)

	// Subscribe to events
	sub := bus.Subscribe()
	defer bus.Unsubscribe(sub)

	// Publish event
	svc.publishEvent(events.EventSyncStarted, map[string]interface{}{
		"test": "value",
	})

	// Verify event was received
	select {
	case event := <-sub:
		if event.Type != events.EventSyncStarted {
			t.Errorf("received event type = %v, want %v", event.Type, events.EventSyncStarted)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("did not receive published event")
	}
}

func TestSyncResult_Fields(t *testing.T) {
	result := SyncResult{
		Updated:       true,
		BackendCount:  5,
		CommitSHA:     "abc1234",
		ConfigChanged: true,
		Error:         nil,
	}

	if !result.Updated {
		t.Error("SyncResult.Updated should be true")
	}
	if result.BackendCount != 5 {
		t.Errorf("SyncResult.BackendCount = %d, want 5", result.BackendCount)
	}
	if result.CommitSHA != "abc1234" {
		t.Errorf("SyncResult.CommitSHA = %q, want %q", result.CommitSHA, "abc1234")
	}
	if !result.ConfigChanged {
		t.Error("SyncResult.ConfigChanged should be true")
	}
	if result.Error != nil {
		t.Errorf("SyncResult.Error = %v, want nil", result.Error)
	}
}

func TestConfig_Fields(t *testing.T) {
	cfg := Config{
		GitRepoURL:        "https://github.com/test/repo.git",
		GitPAT:            "test_pat",
		GitBranch:         "main",
		AppsFolder:        "apps",
		RepoCacheDir:      "/tmp/cache",
		HAProxyConfigFile: "/etc/haproxy/haproxy.cfg",
		MetadataFile:      "/tmp/metadata.json",
		StateFile:         "/tmp/state.json",
		DryRun:            true,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Verify all fields are set correctly
	if cfg.GitRepoURL != "https://github.com/test/repo.git" {
		t.Error("Config.GitRepoURL not set correctly")
	}
	if cfg.GitBranch != "main" {
		t.Error("Config.GitBranch not set correctly")
	}
	if cfg.AppsFolder != "apps" {
		t.Error("Config.AppsFolder not set correctly")
	}
	if !cfg.DryRun {
		t.Error("Config.DryRun should be true")
	}
}

func TestNewService_CreatesComponents(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	cfg := Config{
		GitRepoURL:        "https://github.com/test/repo.git",
		GitPAT:            "",
		GitBranch:         "main",
		AppsFolder:        "apps",
		RepoCacheDir:      tmpDir,
		HAProxyConfigFile: "/etc/haproxy/haproxy.cfg",
		MetadataFile:      filepath.Join(tmpDir, "metadata.json"),
		StateFile:         stateFile,
		DryRun:            true,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if svc.github == nil {
		t.Error("NewService() did not create github client")
	}
	if svc.state == nil {
		t.Error("NewService() did not create state manager")
	}
	if svc.haproxyMgr == nil {
		t.Error("NewService() did not create haproxy manager")
	}
	if svc.triggerChan == nil {
		t.Error("NewService() did not create trigger channel")
	}
	if svc.appsFolder != "apps" {
		t.Errorf("NewService() appsFolder = %q, want %q", svc.appsFolder, "apps")
	}
	if !svc.dryRun {
		t.Error("NewService() dryRun should be true")
	}
}

func TestService_LoadMetadataFromFile(t *testing.T) {
	svc := &Service{}

	// Currently a no-op that returns nil
	err := svc.LoadMetadataFromFile()
	if err != nil {
		t.Errorf("LoadMetadataFromFile() error = %v, want nil", err)
	}
}
