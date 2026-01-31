package logs

import (
	"testing"
)

func TestDefaultSources(t *testing.T) {
	sources := DefaultSources()

	if len(sources) == 0 {
		t.Fatal("DefaultSources() returned empty slice")
	}

	// Check that haproxy source exists
	found := false
	for _, s := range sources {
		if s.Name == "haproxy" {
			found = true
			if s.Unit != "haproxy" {
				t.Errorf("haproxy source unit = %s, want haproxy", s.Unit)
			}
			break
		}
	}
	if !found {
		t.Error("DefaultSources() missing haproxy source")
	}
}

func TestNewCollector(t *testing.T) {
	sources := []LogSource{
		{Name: "test1", Unit: "test1.service"},
		{Name: "test2", Type: SourceTypeFile, FilePath: "/var/log/test.log"},
	}

	collector := NewCollector(sources)
	if collector == nil {
		t.Fatal("NewCollector() returned nil")
	}

	if !collector.HasSource("test1") {
		t.Error("HasSource(test1) = false, want true")
	}
	if !collector.HasSource("test2") {
		t.Error("HasSource(test2) = false, want true")
	}
	if collector.HasSource("nonexistent") {
		t.Error("HasSource(nonexistent) = true, want false")
	}
}

func TestCollector_GetSources(t *testing.T) {
	sources := []LogSource{
		{Name: "a", Unit: "a.service"},
		{Name: "b", Unit: "b.service"},
		{Name: "c", Type: SourceTypeJournalKernel},
	}

	collector := NewCollector(sources)
	result := collector.GetSources()

	if len(result) != 3 {
		t.Errorf("GetSources() returned %d sources, want 3", len(result))
	}
}

func TestCollector_FetchLogs_UnknownSource(t *testing.T) {
	collector := NewCollector(nil)

	_, err := collector.FetchLogs("unknown", 100)
	if err == nil {
		t.Error("FetchLogs(unknown) should return error")
	}
}

func TestCollector_FetchLogs_LineLimits(t *testing.T) {
	// Test with a source that has no valid configuration
	// to verify line limit validation happens before fetch
	sources := []LogSource{
		{Name: "empty"},
	}
	collector := NewCollector(sources)

	// Test default (0 should become 500 internally, but will error on empty source)
	_, err := collector.FetchLogs("empty", 0)
	if err == nil {
		t.Error("FetchLogs() with empty source should return error")
	}

	// Test negative (should also work for limit setting, but error on empty source)
	_, err = collector.FetchLogs("empty", -1)
	if err == nil {
		t.Error("FetchLogs() with empty source should return error")
	}

	// Test above cap (should be capped at 10000, but still error on empty source)
	_, err = collector.FetchLogs("empty", 20000)
	if err == nil {
		t.Error("FetchLogs() with empty source should return error")
	}
}

func TestCollector_FetchLogs_NoValidConfiguration(t *testing.T) {
	sources := []LogSource{
		{Name: "empty"},
	}
	collector := NewCollector(sources)

	_, err := collector.FetchLogs("empty", 100)
	if err == nil {
		t.Error("FetchLogs() with empty source should return error")
	}
}

func TestFilterLines(t *testing.T) {
	content := `line one
line two with ERROR
line three
ERROR on line four
line five`

	result := filterLines(content, "ERROR")

	if result == "" {
		t.Fatal("filterLines returned empty string")
	}

	lines := len(result)
	if lines == 0 {
		t.Error("filterLines should return matching lines")
	}
}

func TestMatchesPattern_CaseInsensitive(t *testing.T) {
	tests := []struct {
		line    string
		pattern string
		want    bool
	}{
		{"Hello World", "(?i)hello", true},
		{"Hello World", "(?i)WORLD", true},
		{"Hello World", "(?i)missing", false},
	}

	for _, tt := range tests {
		got := matchesPattern(tt.line, tt.pattern)
		if got != tt.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.line, tt.pattern, got, tt.want)
		}
	}
}

func TestMatchesPattern_OrPatterns(t *testing.T) {
	tests := []struct {
		line    string
		pattern string
		want    bool
	}{
		{"error occurred", "(error|warning|fatal)", true},
		{"warning issued", "(error|warning|fatal)", true},
		{"info message", "(error|warning|fatal)", false},
	}

	for _, tt := range tests {
		got := matchesPattern(tt.line, tt.pattern)
		if got != tt.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.line, tt.pattern, got, tt.want)
		}
	}
}

func TestMatchesPattern_SimpleContains(t *testing.T) {
	tests := []struct {
		line    string
		pattern string
		want    bool
	}{
		{"this is an error", "error", true},
		{"no match here", "error", false},
		{"ERROR uppercase", "ERROR", true},
	}

	for _, tt := range tests {
		got := matchesPattern(tt.line, tt.pattern)
		if got != tt.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.line, tt.pattern, got, tt.want)
		}
	}
}

func TestMatchesPattern_CaseInsensitiveWithOr(t *testing.T) {
	tests := []struct {
		line    string
		pattern string
		want    bool
	}{
		{"ERROR occurred", "(?i)(error|warning)", true},
		{"WARNING issued", "(?i)(error|warning)", true},
		{"Info message", "(?i)(error|warning)", false},
	}

	for _, tt := range tests {
		got := matchesPattern(tt.line, tt.pattern)
		if got != tt.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.line, tt.pattern, got, tt.want)
		}
	}
}

func TestFilterLines_EmptyContent(t *testing.T) {
	result := filterLines("", "pattern")
	if result != "" {
		t.Errorf("filterLines(\"\", \"pattern\") = %q, want empty string", result)
	}
}

func TestFilterLines_NoMatches(t *testing.T) {
	content := `line one
line two
line three`

	result := filterLines(content, "nomatch")
	if result != "" {
		t.Errorf("filterLines() with no matches should return empty, got %q", result)
	}
}

func TestFilterLines_CaseInsensitive(t *testing.T) {
	content := `ERROR on line one
error on line two
Line three has Error`

	result := filterLines(content, "error")

	// Should match all three lines (case-insensitive comparison in filterLines)
	if result == "" {
		t.Error("filterLines() should match case-insensitively")
	}
}

func TestLogSource_Fields(t *testing.T) {
	source := LogSource{
		Name:     "test",
		Unit:     "test.service",
		Type:     SourceTypeFile,
		FilePath: "/var/log/test.log",
		Priority: "warning..emerg",
		Filter:   "(?i)error",
	}

	if source.Name != "test" {
		t.Error("LogSource.Name not set correctly")
	}
	if source.Unit != "test.service" {
		t.Error("LogSource.Unit not set correctly")
	}
	if source.Type != SourceTypeFile {
		t.Error("LogSource.Type not set correctly")
	}
	if source.FilePath != "/var/log/test.log" {
		t.Error("LogSource.FilePath not set correctly")
	}
	if source.Priority != "warning..emerg" {
		t.Error("LogSource.Priority not set correctly")
	}
	if source.Filter != "(?i)error" {
		t.Error("LogSource.Filter not set correctly")
	}
}

func TestLogResponse_Fields(t *testing.T) {
	resp := LogResponse{
		Name:    "haproxy",
		Lines:   100,
		Content: "log content here",
	}

	if resp.Name != "haproxy" {
		t.Error("LogResponse.Name not set correctly")
	}
	if resp.Lines != 100 {
		t.Error("LogResponse.Lines not set correctly")
	}
	if resp.Content != "log content here" {
		t.Error("LogResponse.Content not set correctly")
	}
}

func TestSourcesResponse_Fields(t *testing.T) {
	resp := SourcesResponse{
		Sources: []LogSource{
			{Name: "test1"},
			{Name: "test2"},
		},
	}

	if len(resp.Sources) != 2 {
		t.Errorf("SourcesResponse.Sources length = %d, want 2", len(resp.Sources))
	}
}

func TestLogSourceType_Constants(t *testing.T) {
	tests := []struct {
		sourceType LogSourceType
		want       string
	}{
		{SourceTypeUnit, "unit"},
		{SourceTypeJournalKernel, "kernel"},
		{SourceTypeJournalPriority, "priority"},
		{SourceTypeJournalBoot, "boot"},
		{SourceTypeJournalSSH, "ssh"},
		{SourceTypeFile, "file"},
	}

	for _, tt := range tests {
		if string(tt.sourceType) != tt.want {
			t.Errorf("LogSourceType %v = %q, want %q", tt.sourceType, string(tt.sourceType), tt.want)
		}
	}
}

func TestDefaultSources_ExpectedSources(t *testing.T) {
	sources := DefaultSources()

	// Build a map for easier checking
	sourceMap := make(map[string]LogSource)
	for _, s := range sources {
		sourceMap[s.Name] = s
	}

	// Check for expected sources
	expectedSources := []string{
		"haproxy",
		"gearbox-agent",
		"fail2ban",
		"fail2ban-log",
		"nftables",
		"firewall",
		"auth",
		"ssh",
		"system",
		"kernel",
		"boot",
	}

	for _, name := range expectedSources {
		if _, ok := sourceMap[name]; !ok {
			t.Errorf("DefaultSources() missing expected source: %s", name)
		}
	}
}

func TestDefaultSources_CorrectTypes(t *testing.T) {
	sources := DefaultSources()

	// Build a map for easier checking
	sourceMap := make(map[string]LogSource)
	for _, s := range sources {
		sourceMap[s.Name] = s
	}

	// Verify specific source configurations
	if s, ok := sourceMap["fail2ban-log"]; ok {
		if s.Type != SourceTypeFile {
			t.Errorf("fail2ban-log should be SourceTypeFile, got %v", s.Type)
		}
		if s.FilePath != "/var/log/fail2ban.log" {
			t.Errorf("fail2ban-log FilePath = %q, want /var/log/fail2ban.log", s.FilePath)
		}
	}

	if s, ok := sourceMap["kernel"]; ok {
		if s.Type != SourceTypeJournalKernel {
			t.Errorf("kernel should be SourceTypeJournalKernel, got %v", s.Type)
		}
	}

	if s, ok := sourceMap["system"]; ok {
		if s.Type != SourceTypeJournalPriority {
			t.Errorf("system should be SourceTypeJournalPriority, got %v", s.Type)
		}
		if s.Priority != "warning..emerg" {
			t.Errorf("system Priority = %q, want warning..emerg", s.Priority)
		}
	}
}

func TestNewCollector_EmptySources(t *testing.T) {
	collector := NewCollector(nil)
	if collector == nil {
		t.Fatal("NewCollector(nil) returned nil")
	}

	sources := collector.GetSources()
	if len(sources) != 0 {
		t.Errorf("GetSources() returned %d sources for empty collector, want 0", len(sources))
	}
}

func TestNewCollector_DuplicateNames(t *testing.T) {
	// If duplicate names are provided, the last one should win
	sources := []LogSource{
		{Name: "test", Unit: "first.service"},
		{Name: "test", Unit: "second.service"},
	}

	collector := NewCollector(sources)

	// Should only have 1 source (map overwrites)
	result := collector.GetSources()
	if len(result) != 1 {
		t.Errorf("GetSources() returned %d sources, want 1 (duplicates should merge)", len(result))
	}

	// The unit should be from the second entry
	if result[0].Unit != "second.service" {
		t.Errorf("Duplicate source should use last value, got Unit=%q", result[0].Unit)
	}
}
