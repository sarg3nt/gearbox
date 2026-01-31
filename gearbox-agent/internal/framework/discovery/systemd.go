package discovery

import (
	"context"
	"os/exec"
	"strings"
)

// SystemdDetector detects systemd availability.
type SystemdDetector struct{}

// NewSystemdDetector creates a new systemd detector.
func NewSystemdDetector() *SystemdDetector {
	return &SystemdDetector{}
}

// Name returns the capability name.
func (d *SystemdDetector) Name() string {
	return "systemd"
}

// Detect checks if systemd is available.
func (d *SystemdDetector) Detect(ctx context.Context) (Capability, error) {
	cap := Capability{
		Name:        "systemd",
		DisplayName: "Systemd",
		Detected:    false,
	}

	// Check if systemctl is available
	versionCmd := exec.CommandContext(ctx, "systemctl", "--version")
	output, err := versionCmd.CombinedOutput()
	if err != nil {
		cap.Details = "Systemd not available"
		return cap, nil
	}

	// Parse version from first line
	// Example: "systemd 255 (255.4-1ubuntu8.4)"
	lines := strings.Split(string(output), "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		parts := strings.Fields(firstLine)
		if len(parts) >= 2 {
			cap.Version = parts[1]
		}
	}

	cap.Detected = true
	cap.Details = "Systemd is available for service management"

	return cap, nil
}
