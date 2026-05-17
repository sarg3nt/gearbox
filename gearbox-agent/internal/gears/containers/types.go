// Package containers provides Docker/container runtime monitoring as a gear.
package containers

import "time"

// RuntimeInfo describes the detected container runtime.
type RuntimeInfo struct {
	Available   bool   `json:"available"`
	Runtime     string `json:"runtime"`      // "docker", "none"
	Version     string `json:"version"`      // Docker version string
	APIVersion  string `json:"api_version"`  // Docker API version
	ServerOS    string `json:"server_os"`    // linux, windows
	ServerArch  string `json:"server_arch"`  // amd64, arm64
}

// PortMapping represents a container port binding.
type PortMapping struct {
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port"`
	Protocol      string `json:"protocol"` // tcp, udp
	HostIP        string `json:"host_ip,omitempty"`
}

// Mount represents a container volume mount.
type Mount struct {
	Type        string `json:"type"`   // bind, volume, tmpfs
	Source      string `json:"source"` // Host path or volume name
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	ReadWrite   bool   `json:"read_write"`
}

// Container represents a single container.
type Container struct {
	ID            string            `json:"id"`
	ShortID       string            `json:"short_id"` // First 12 chars
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	ImageID       string            `json:"image_id"`
	Status        string            `json:"status"`        // running, exited, paused, restarting, created, dead
	State         string            `json:"state"`         // running, stopped, paused, etc.
	Created       time.Time         `json:"created"`
	StartedAt     string            `json:"started_at,omitempty"`
	FinishedAt    string            `json:"finished_at,omitempty"`
	Ports         []PortMapping     `json:"ports"`
	Labels        map[string]string `json:"labels"`
	StackName     string            `json:"stack_name,omitempty"`    // from com.docker.compose.project label
	ServiceName   string            `json:"service_name,omitempty"`  // from com.docker.compose.service label
	Networks      []string          `json:"networks"`
	Mounts        []Mount           `json:"mounts"`
	RestartPolicy string            `json:"restart_policy"`
	Runtime       string            `json:"runtime"` // "docker"
}

// ContainerStats represents resource usage stats for a container.
type ContainerStats struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryUsage   uint64  `json:"memory_usage"`   // bytes
	MemoryLimit   uint64  `json:"memory_limit"`   // bytes
	MemoryPercent float64 `json:"memory_percent"`
	NetworkRx     uint64  `json:"network_rx"` // bytes received
	NetworkTx     uint64  `json:"network_tx"` // bytes sent
	DiskRead      uint64  `json:"disk_read"`  // bytes read
	DiskWrite     uint64  `json:"disk_write"` // bytes written
	PIDs          uint64  `json:"pids"`
}

// Stack represents a Docker Compose project (stack).
type Stack struct {
	Name           string      `json:"name"`
	Status         string      `json:"status"`          // running, partial, stopped
	ContainerCount int         `json:"container_count"`
	RunningCount   int         `json:"running_count"`
	ConfigPath     string      `json:"config_path,omitempty"` // Path to docker-compose.yml
	WorkingDir     string      `json:"working_dir,omitempty"`
	HasEnvFile     bool        `json:"has_env_file"`
	Containers     []Container `json:"containers,omitempty"`
}

// Image represents a Docker image.
type Image struct {
	ID         string    `json:"id"`
	ShortID    string    `json:"short_id"`
	Tags       []string  `json:"tags"`
	Size       int64     `json:"size"` // bytes
	Created    time.Time `json:"created"`
	Containers int       `json:"containers"` // Number of running containers
}

// Network represents a Docker network.
type Network struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Scope  string `json:"scope"`
}

// Volume represents a Docker volume.
type Volume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
}

// ContainerList is the response for listing containers.
type ContainerList struct {
	Containers  []Container `json:"containers"`
	Total       int         `json:"total"`
	Running     int         `json:"running"`
	Stopped     int         `json:"stopped"`
	CollectedAt time.Time   `json:"collected_at"`
}

// StackList is the response for listing stacks.
type StackList struct {
	Stacks      []Stack `json:"stacks"`
	Total       int     `json:"total"`
	CollectedAt time.Time `json:"collected_at"`
}
