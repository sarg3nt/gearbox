package collector

import (
	"testing"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/models"
)

func TestNewCache(t *testing.T) {
	cache := NewCache(5*time.Minute, 10*time.Minute, 30*time.Second, 1*time.Minute)
	if cache == nil {
		t.Fatal("NewCache returned nil")
	}

	if cache.statsTTL != 5*time.Minute {
		t.Errorf("Cache statsTTL = %v, want %v", cache.statsTTL, 5*time.Minute)
	}
	if cache.metadataTTL != 10*time.Minute {
		t.Errorf("Cache metadataTTL = %v, want %v", cache.metadataTTL, 10*time.Minute)
	}
	if cache.metricsTTL != 30*time.Second {
		t.Errorf("Cache metricsTTL = %v, want %v", cache.metricsTTL, 30*time.Second)
	}
	if cache.logsTTL != 1*time.Minute {
		t.Errorf("Cache logsTTL = %v, want %v", cache.logsTTL, 1*time.Minute)
	}
}

func TestCache_Stats(t *testing.T) {
	cache := NewCache(1*time.Hour, 1*time.Hour, 1*time.Hour, 1*time.Hour)

	// Test getting stats when none set
	stats, _, valid := cache.GetStats()
	if stats != nil || valid {
		t.Error("GetStats should return nil and false when no stats set")
	}

	// Set stats
	testStats := &models.HAProxyStats{
		Frontends: []models.Frontend{{Name: "test-frontend"}},
	}
	cache.SetStats(testStats)

	// Get stats
	got, updatedAt, valid := cache.GetStats()
	if !valid {
		t.Error("GetStats should return valid=true for fresh stats")
	}
	if got == nil {
		t.Fatal("GetStats returned nil stats")
	}
	if len(got.Frontends) != 1 || got.Frontends[0].Name != "test-frontend" {
		t.Error("GetStats returned incorrect stats")
	}
	if updatedAt.IsZero() {
		t.Error("GetStats returned zero updatedAt time")
	}
}

func TestCache_StatsExpiration(t *testing.T) {
	cache := NewCache(100*time.Millisecond, 1*time.Hour, 1*time.Hour, 1*time.Hour)

	testStats := &models.HAProxyStats{
		Frontends: []models.Frontend{{Name: "test"}},
	}
	cache.SetStats(testStats)

	// Should be valid immediately
	_, _, valid := cache.GetStats()
	if !valid {
		t.Error("Stats should be valid immediately after set")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired but still return stale data
	got, _, valid := cache.GetStats()
	if valid {
		t.Error("Stats should be invalid after expiration")
	}
	if got == nil {
		t.Error("GetStats should return stale data even when expired")
	}
}

func TestCache_Metadata(t *testing.T) {
	cache := NewCache(1*time.Hour, 1*time.Hour, 1*time.Hour, 1*time.Hour)

	// Test getting metadata when none set
	metadata, _, valid := cache.GetMetadata()
	if metadata != nil || valid {
		t.Error("GetMetadata should return nil and false when no metadata set")
	}

	// Set metadata
	testMetadata := &models.Metadata{
		Version: "2.8.0",
	}
	cache.SetMetadata(testMetadata)

	// Get metadata
	got, updatedAt, valid := cache.GetMetadata()
	if !valid {
		t.Error("GetMetadata should return valid=true for fresh metadata")
	}
	if got == nil {
		t.Fatal("GetMetadata returned nil")
	}
	if got.Version != "2.8.0" {
		t.Errorf("GetMetadata returned wrong version: %s", got.Version)
	}
	if updatedAt.IsZero() {
		t.Error("GetMetadata returned zero updatedAt time")
	}
}

func TestCache_SystemMetrics(t *testing.T) {
	cache := NewCache(1*time.Hour, 1*time.Hour, 1*time.Hour, 1*time.Hour)

	// Test getting metrics when none set
	metrics, _, valid := cache.GetSystemMetrics()
	if metrics != nil || valid {
		t.Error("GetSystemMetrics should return nil and false when no metrics set")
	}

	// Set metrics
	testMetrics := &models.SystemMetrics{
		CPUUsagePercent:    50.5,
		MemoryUsagePercent: 60.0,
	}
	cache.SetSystemMetrics(testMetrics)

	// Get metrics
	got, updatedAt, valid := cache.GetSystemMetrics()
	if !valid {
		t.Error("GetSystemMetrics should return valid=true for fresh metrics")
	}
	if got == nil {
		t.Fatal("GetSystemMetrics returned nil")
	}
	if got.CPUUsagePercent != 50.5 || got.MemoryUsagePercent != 60.0 {
		t.Error("GetSystemMetrics returned incorrect values")
	}
	if updatedAt.IsZero() {
		t.Error("GetSystemMetrics returned zero updatedAt time")
	}
}

func TestCache_Logs(t *testing.T) {
	cache := NewCache(1*time.Hour, 1*time.Hour, 1*time.Hour, 1*time.Hour)

	// Test getting log when none set
	logData, _, valid := cache.GetLog("test.log")
	if logData != nil || valid {
		t.Error("GetLog should return nil and false when no log set")
	}

	// Set log
	cache.SetLog("test.log", "line1\nline2\nline3", 3)

	// Get log
	got, updatedAt, valid := cache.GetLog("test.log")
	if !valid {
		t.Error("GetLog should return valid=true for fresh log")
	}
	if got == nil {
		t.Fatal("GetLog returned nil")
	}
	if got.Content != "line1\nline2\nline3" {
		t.Errorf("GetLog returned wrong content: %s", got.Content)
	}
	if got.Lines != 3 {
		t.Errorf("GetLog returned wrong line count: %d", got.Lines)
	}
	if updatedAt.IsZero() {
		t.Error("GetLog returned zero updatedAt time")
	}
}

func TestCache_IsStale(t *testing.T) {
	cache := NewCache(100*time.Millisecond, 1*time.Hour, 1*time.Hour, 1*time.Hour)

	// Empty cache is not stale
	if cache.IsStale() {
		t.Error("Empty cache should not be stale")
	}

	// Add fresh stats
	cache.SetStats(&models.HAProxyStats{})
	if cache.IsStale() {
		t.Error("Cache with fresh stats should not be stale")
	}

	// Wait for stats to expire
	time.Sleep(150 * time.Millisecond)
	if !cache.IsStale() {
		t.Error("Cache with expired stats should be stale")
	}
}

func TestCache_ConcurrentAccess(t *testing.T) {
	cache := NewCache(1*time.Hour, 1*time.Hour, 1*time.Hour, 1*time.Hour)
	done := make(chan bool)
	iterations := 100

	// Concurrent stats writes
	go func() {
		for i := 0; i < iterations; i++ {
			cache.SetStats(&models.HAProxyStats{})
		}
		done <- true
	}()

	// Concurrent metadata writes
	go func() {
		for i := 0; i < iterations; i++ {
			cache.SetMetadata(&models.Metadata{})
		}
		done <- true
	}()

	// Concurrent reads
	go func() {
		for i := 0; i < iterations; i++ {
			cache.GetStats()
			cache.GetMetadata()
		}
		done <- true
	}()

	// Wait for completion
	<-done
	<-done
	<-done

	// No panics = success (testing thread-safety)
}
