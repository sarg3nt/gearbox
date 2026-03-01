package security

import (
	"testing"
)

func TestNewFirewallCollector(t *testing.T) {
	collector := NewFirewallCollector()
	if collector == nil {
		t.Fatal("NewFirewallCollector returned nil")
	}
}

func TestFirewallCollector_IsInstalled(t *testing.T) {
	collector := NewFirewallCollector()
	// Just test that it doesn't panic - result depends on system
	_ = collector.IsInstalled()
}

func TestFirewallCollector_Collect_NotInstalled(t *testing.T) {
	collector := NewFirewallCollector()
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
			t.Error("Expected ErrServiceNotInstalled when nftables not installed")
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

func TestParseBlockLine(t *testing.T) {
	collector := NewFirewallCollector()

	tests := []struct {
		line        string
		expectSrcIP string
		expectDstIP string
		expectProto string
		expectDPort int
	}{
		{
			line:        "Jan 17 10:30:45 server kernel: [nft_drop] IN=eth0 OUT= SRC=192.168.1.100 DST=10.0.0.1 PROTO=TCP SPT=54321 DPT=22",
			expectSrcIP: "192.168.1.100",
			expectDstIP: "10.0.0.1",
			expectProto: "TCP",
			expectDPort: 22,
		},
		{
			line:        "IN=eno2 OUT= SRC=10.0.0.50 DST=10.0.0.1 PROTO=UDP DPT=53",
			expectSrcIP: "10.0.0.50",
			expectDstIP: "10.0.0.1",
			expectProto: "UDP",
			expectDPort: 53,
		},
	}

	for _, tt := range tests {
		event := collector.parseBlockLine(tt.line)

		if event.SrcIP != tt.expectSrcIP {
			t.Errorf("SrcIP = %s, want %s", event.SrcIP, tt.expectSrcIP)
		}
		if event.DstIP != tt.expectDstIP {
			t.Errorf("DstIP = %s, want %s", event.DstIP, tt.expectDstIP)
		}
		if event.Protocol != tt.expectProto {
			t.Errorf("Protocol = %s, want %s", event.Protocol, tt.expectProto)
		}
		if event.DstPort != tt.expectDPort {
			t.Errorf("DstPort = %d, want %d", event.DstPort, tt.expectDPort)
		}
	}
}

func TestBlockEventFields(t *testing.T) {
	event := BlockEvent{
		Chain:     "input",
		Action:    "DROP",
		Protocol:  "TCP",
		SrcIP:     "192.168.1.100",
		DstIP:     "10.0.0.1",
		SrcPort:   54321,
		DstPort:   443,
		Interface: "eth0",
	}

	if event.Chain != "input" {
		t.Errorf("Chain = %s, want input", event.Chain)
	}
	if event.Action != "DROP" {
		t.Errorf("Action = %s, want DROP", event.Action)
	}
	if event.DstPort != 443 {
		t.Errorf("DstPort = %d, want 443", event.DstPort)
	}
}

func TestTableStatsFields(t *testing.T) {
	stats := TableStats{
		Family: "inet",
		Name:   "filter",
		Chains: []ChainStats{
			{
				Name:   "input",
				Type:   "filter",
				Hook:   "input",
				Policy: "drop",
			},
		},
	}

	if stats.Family != "inet" {
		t.Errorf("Family = %s, want inet", stats.Family)
	}
	if len(stats.Chains) != 1 {
		t.Errorf("Chains len = %d, want 1", len(stats.Chains))
	}
	if stats.Chains[0].Policy != "drop" {
		t.Errorf("Chains[0].Policy = %s, want drop", stats.Chains[0].Policy)
	}
}
