package containers

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

// dockerRuntime wraps the Docker client with data collection methods.
type dockerRuntime struct {
	cli *dockerclient.Client
}

// listContainers returns all containers (running and stopped).
func (d *dockerRuntime) listContainers(ctx context.Context) ([]Container, error) {
	result, err := d.cli.ContainerList(ctx, dockerclient.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}

	containers := make([]Container, 0, len(result.Items))
	for _, c := range result.Items {
		containers = append(containers, summaryToContainer(c))
	}
	return containers, nil
}

// containerStats returns resource usage stats for a single container.
func (d *dockerRuntime) containerStats(ctx context.Context, id string) (*ContainerStats, error) {
	result, err := d.cli.ContainerStats(ctx, id, dockerclient.ContainerStatsOptions{
		Stream:                false,
		IncludePreviousSample: true,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = result.Body.Close() }()

	body, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, err
	}

	var stats container.StatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, err
	}

	return statsResponseToContainerStats(id, &stats), nil
}

// listStacks groups containers into Compose stacks.
func listStacks(containers []Container) []Stack {
	stackMap := make(map[string]*Stack)
	var ungrouped []Container

	for _, c := range containers {
		if c.StackName == "" {
			ungrouped = append(ungrouped, c)
			continue
		}

		stack, ok := stackMap[c.StackName]
		if !ok {
			configPath := findComposeFile(c.Labels)
			workingDir := ""
			if configPath != "" {
				workingDir = filepath.Dir(configPath)
			}
			hasEnvFile := false
			if workingDir != "" {
				if _, err := os.Stat(filepath.Join(workingDir, "stack.env")); err == nil {
					hasEnvFile = true
				}
			}
			stack = &Stack{
				Name:       c.StackName,
				ConfigPath: configPath,
				WorkingDir: workingDir,
				HasEnvFile: hasEnvFile,
			}
			stackMap[c.StackName] = stack
		}
		stack.Containers = append(stack.Containers, c)
		stack.ContainerCount++
		if c.State == "running" {
			stack.RunningCount++
		}
	}

	stacks := make([]Stack, 0, len(stackMap)+1)
	for _, s := range stackMap {
		s.Status = stackStatus(s)
		stacks = append(stacks, *s)
	}

	// Add ungrouped containers as a virtual "Ungrouped" stack if any exist
	_ = ungrouped // ungrouped containers are included in the flat list, not as a stack

	return stacks
}

// findComposeFile extracts the docker-compose file path from container labels.
func findComposeFile(labels map[string]string) string {
	// com.docker.compose.project.config_files contains comma-separated list
	if files, ok := labels["com.docker.compose.project.config_files"]; ok {
		parts := strings.SplitN(files, ",", 2)
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	// Fallback: com.docker.compose.project.working_dir + docker-compose.yml
	if workingDir, ok := labels["com.docker.compose.project.working_dir"]; ok {
		return filepath.Join(workingDir, "docker-compose.yml")
	}
	return ""
}

// stackStatus returns the aggregate status of a stack.
func stackStatus(s *Stack) string {
	if s.RunningCount == 0 {
		return "stopped"
	}
	if s.RunningCount < s.ContainerCount {
		return "partial"
	}
	return "running"
}

// summaryToContainer converts a Docker API container summary to our type.
func summaryToContainer(c container.Summary) Container {
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}

	networks := make([]string, 0)
	if c.NetworkSettings != nil {
		for netName := range c.NetworkSettings.Networks {
			networks = append(networks, netName)
		}
	}

	ports := make([]PortMapping, 0, len(c.Ports))
	for _, p := range c.Ports {
		ports = append(ports, PortMapping{
			ContainerPort: int(p.PrivatePort),
			HostPort:      int(p.PublicPort),
			Protocol:      p.Type,
			HostIP:        p.IP.String(),
		})
	}

	mounts := make([]Mount, 0, len(c.Mounts))
	for _, m := range c.Mounts {
		mounts = append(mounts, Mount{
			Type:        string(m.Type),
			Source:      m.Source,
			Destination: m.Destination,
			Mode:        m.Mode,
			ReadWrite:   m.RW,
		})
	}

	restartPolicy := ""

	return Container{
		ID:            c.ID,
		ShortID:       shortID(c.ID),
		Name:          name,
		Image:         c.Image,
		ImageID:       c.ImageID,
		Status:        c.Status,
		State:         string(c.State),
		Created:       time.Unix(c.Created, 0),
		Ports:         ports,
		Labels:        c.Labels,
		StackName:     c.Labels["com.docker.compose.project"],
		ServiceName:   c.Labels["com.docker.compose.service"],
		Networks:      networks,
		Mounts:        mounts,
		RestartPolicy: restartPolicy,
		Runtime:       "docker",
	}
}

// statsResponseToContainerStats converts Docker stats to our type.
func statsResponseToContainerStats(id string, s *container.StatsResponse) *ContainerStats {
	cpuPercent := calcCPUPercent(s)

	var netRx, netTx uint64
	for _, v := range s.Networks {
		netRx += v.RxBytes
		netTx += v.TxBytes
	}

	var diskRead, diskWrite uint64
	for _, entry := range s.BlkioStats.IoServiceBytesRecursive {
		switch entry.Op {
		case "Read":
			diskRead += entry.Value
		case "Write":
			diskWrite += entry.Value
		}
	}

	memPercent := 0.0
	if s.MemoryStats.Limit > 0 {
		memPercent = float64(s.MemoryStats.Usage) / float64(s.MemoryStats.Limit) * 100.0
	}

	return &ContainerStats{
		ID:            id,
		CPUPercent:    cpuPercent,
		MemoryUsage:   s.MemoryStats.Usage,
		MemoryLimit:   s.MemoryStats.Limit,
		MemoryPercent: memPercent,
		NetworkRx:     netRx,
		NetworkTx:     netTx,
		DiskRead:      diskRead,
		DiskWrite:     diskWrite,
		PIDs:          s.PidsStats.Current,
	}
}

// calcCPUPercent calculates CPU usage percentage from Docker stats.
func calcCPUPercent(stats *container.StatsResponse) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	numCPUs := float64(stats.CPUStats.OnlineCPUs)
	if numCPUs == 0 {
		numCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if systemDelta > 0 && cpuDelta > 0 {
		return (cpuDelta / systemDelta) * numCPUs * 100.0
	}
	return 0.0
}

// shortID returns the first 12 characters of a container ID.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
