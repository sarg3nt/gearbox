package discovery

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// DockerDetector detects Docker installation and status.
type DockerDetector struct{}

// NewDockerDetector creates a new Docker detector.
func NewDockerDetector() *DockerDetector {
	return &DockerDetector{}
}

// Name returns the capability name.
func (d *DockerDetector) Name() string {
	return "docker"
}

// Detect checks if Docker is installed and running.
func (d *DockerDetector) Detect(ctx context.Context) (Capability, error) {
	cap := Capability{
		Name:        "docker",
		DisplayName: "Docker",
		Detected:    false,
	}

	// Check if docker binary exists
	versionCmd := exec.CommandContext(ctx, "docker", "--version")
	output, err := versionCmd.CombinedOutput()
	if err != nil {
		cap.Details = "Docker binary not found"
		return cap, nil
	}

	// Parse version from output
	// Example: "Docker version 24.0.7, build afdd53b"
	versionRegex := regexp.MustCompile(`Docker version ([^,]+)`)
	matches := versionRegex.FindStringSubmatch(string(output))
	if len(matches) > 1 {
		cap.Version = matches[1]
	}

	// Check if Docker socket exists and is accessible
	socketPath := "/var/run/docker.sock"
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		cap.Detected = false
		cap.Details = "Docker is installed but socket not found"
		return cap, nil
	}

	// Check if Docker service is running
	statusCmd := exec.CommandContext(ctx, "systemctl", "is-active", "docker")
	statusOutput, _ := statusCmd.Output()
	isRunning := strings.TrimSpace(string(statusOutput)) == "active"

	cap.Detected = true
	if isRunning {
		cap.Details = "Docker is installed and running"
	} else {
		cap.Details = "Docker is installed but not running"
	}

	return cap, nil
}
