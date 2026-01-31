package discovery

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
)

// HAProxyDetector detects HAProxy installation and status.
type HAProxyDetector struct{}

// NewHAProxyDetector creates a new HAProxy detector.
func NewHAProxyDetector() *HAProxyDetector {
	return &HAProxyDetector{}
}

// Name returns the capability name.
func (d *HAProxyDetector) Name() string {
	return "haproxy"
}

// Detect checks if HAProxy is installed and running.
func (d *HAProxyDetector) Detect(ctx context.Context) (Capability, error) {
	cap := Capability{
		Name:        "haproxy",
		DisplayName: "HAProxy",
		Detected:    false,
	}

	// Check if haproxy binary exists
	versionCmd := exec.CommandContext(ctx, "haproxy", "-v")
	output, err := versionCmd.CombinedOutput()
	if err != nil {
		cap.Details = "HAProxy binary not found"
		return cap, nil
	}

	// Parse version from output
	// Example: "HAProxy version 2.8.5-1ubuntu3 2024/04/01"
	versionRegex := regexp.MustCompile(`HAProxy version ([^\s]+)`)
	matches := versionRegex.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		cap.Version = matches[1]
	}

	// Check if HAProxy service is running
	statusCmd := exec.CommandContext(ctx, "systemctl", "is-active", "haproxy")
	statusOutput, _ := statusCmd.Output()
	isRunning := strings.TrimSpace(string(statusOutput)) == "active"

	cap.Detected = true
	if isRunning {
		cap.Details = "HAProxy is installed and running"
	} else {
		cap.Details = "HAProxy is installed but not running"
	}

	return cap, nil
}
