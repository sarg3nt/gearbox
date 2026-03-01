package security

import (
	"testing"
)

func TestNewFail2BanCollector(t *testing.T) {
	collector := NewFail2BanCollector()
	if collector == nil {
		t.Fatal("NewFail2BanCollector returned nil")
	}
}

func TestParseIntFromLine(t *testing.T) {
	tests := []struct {
		line     string
		expected int
	}{
		{"Currently banned: 5", 5},
		{"Total failed: 100", 100},
		{"Some value: 0", 0},
		{"Invalid line", 0},
		{"No colon here", 0},
	}

	for _, tt := range tests {
		result := parseIntFromLine(tt.line)
		if result != tt.expected {
			t.Errorf("parseIntFromLine(%q) = %d, want %d", tt.line, result, tt.expected)
		}
	}
}

func TestFail2BanCollector_IsInstalled(t *testing.T) {
	collector := NewFail2BanCollector()
	// Just test that it doesn't panic - result depends on system
	_ = collector.IsInstalled()
}

func TestFail2BanCollector_Collect_NotInstalled(t *testing.T) {
	collector := NewFail2BanCollector()
	stats, err := collector.Collect(false, 0)

	// Stats should always be returned
	if stats == nil {
		t.Fatal("Collect returned nil stats")
	}

	if stats.CollectedAt.IsZero() {
		t.Error("CollectedAt should be set")
	}

	// If not installed, should return ErrServiceNotInstalled
	if !collector.IsInstalled() {
		if err == nil {
			t.Error("Expected ErrServiceNotInstalled when fail2ban not installed")
		}
		if stats.Available {
			t.Error("Available should be false when not installed")
		}
	} else {
		// If installed, Available should be true
		if !stats.Available {
			t.Error("Available should be true when installed")
		}
	}
}

func TestBanEventFields(t *testing.T) {
	event := BanEvent{
		Jail:   "sshd",
		IP:     "192.168.1.100",
		Action: "ban",
	}

	if event.Jail != "sshd" {
		t.Errorf("Jail = %s, want sshd", event.Jail)
	}
	if event.IP != "192.168.1.100" {
		t.Errorf("IP = %s, want 192.168.1.100", event.IP)
	}
	if event.Action != "ban" {
		t.Errorf("Action = %s, want ban", event.Action)
	}
}

func TestJailStatsFields(t *testing.T) {
	stats := JailStats{
		Name:            "sshd",
		CurrentlyBanned: 2,
		TotalBanned:     10,
		CurrentlyFailed: 5,
		TotalFailed:     50,
		BannedIPs:       []string{"192.168.1.1", "10.0.0.1"},
	}

	if stats.Name != "sshd" {
		t.Errorf("Name = %s, want sshd", stats.Name)
	}
	if stats.CurrentlyBanned != 2 {
		t.Errorf("CurrentlyBanned = %d, want 2", stats.CurrentlyBanned)
	}
	if len(stats.BannedIPs) != 2 {
		t.Errorf("BannedIPs len = %d, want 2", len(stats.BannedIPs))
	}
}
