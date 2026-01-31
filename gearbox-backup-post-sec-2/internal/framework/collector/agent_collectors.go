package collector

import (
	"fmt"
	"time"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/services/parser"
)

// AgentStatsCollector collects HAProxy statistics via the Agent API.
type AgentStatsCollector struct {
	client *agent.Client
}

// NewAgentStatsCollector creates a new Agent-based stats collector.
func NewAgentStatsCollector(client *agent.Client) *AgentStatsCollector {
	return &AgentStatsCollector{
		client: client,
	}
}

// Collect fetches and parses HAProxy stats via the Agent API.
func (s *AgentStatsCollector) Collect() (*models.HAProxyStats, error) {
	// Get stats CSV from the Agent API and parse it using the existing parser
	csvData, err := s.client.GetStatsCSV()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stats from agent: %w", err)
	}

	// Parse CSV using existing parser
	stats, err := parser.ParseStatsCSV(csvData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse stats: %w", err)
	}

	return stats, nil
}

// AgentMetadataCollector collects HAProxy configuration metadata via the Agent API.
type AgentMetadataCollector struct {
	client *agent.Client
}

// NewAgentMetadataCollector creates a new Agent-based metadata collector.
func NewAgentMetadataCollector(client *agent.Client) *AgentMetadataCollector {
	return &AgentMetadataCollector{
		client: client,
	}
}

// Collect fetches and converts metadata from the Agent API.
func (m *AgentMetadataCollector) Collect() (*models.Metadata, error) {
	resp, err := m.client.GetMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata from agent: %w", err)
	}

	// Convert agent.MetadataResponse to models.Metadata
	metadata := &models.Metadata{
		Version:   resp.Metadata.Version,
		Frontends: make([]models.FrontendMetadata, len(resp.Metadata.Frontends)),
		Backends:  make([]models.BackendMetadata, len(resp.Metadata.Backends)),
	}

	// Parse GeneratedAt
	if resp.Metadata.GeneratedAt != "" {
		if t, err := time.Parse(time.RFC3339, resp.Metadata.GeneratedAt); err == nil {
			metadata.GeneratedAt = t
		}
	}

	// Convert frontends
	for i, f := range resp.Metadata.Frontends {
		metadata.Frontends[i] = models.FrontendMetadata{
			Name:     f.Name,
			Backends: f.Backends,
		}
	}

	// Convert backends
	for i, b := range resp.Metadata.Backends {
		metadata.Backends[i] = models.BackendMetadata{
			Name:      b.Name,
			AppName:   b.AppName,
			Hostname:  b.Hostname,
			Servers:   b.Servers,
			Public:    b.Public,
			RateLimit: b.RateLimit,
			Frontend:  b.Frontend,
		}

		// Convert containers
		if len(b.Containers) > 0 {
			metadata.Backends[i].Containers = make([]models.Container, len(b.Containers))
			for j, c := range b.Containers {
				metadata.Backends[i].Containers[j] = models.Container{
					Name:        c.Name,
					NetworkMode: c.NetworkMode,
					IsBackend:   c.IsBackend,
					DependsOn:   c.DependsOn,
				}
			}
		}
	}

	return metadata, nil
}

// AgentSystemCollector collects system-level metrics via the Agent API.
type AgentSystemCollector struct {
	client *agent.Client
}

// NewAgentSystemCollector creates a new Agent-based system metrics collector.
func NewAgentSystemCollector(client *agent.Client) *AgentSystemCollector {
	return &AgentSystemCollector{
		client: client,
	}
}

// Collect gathers system metrics via the Agent API.
func (s *AgentSystemCollector) Collect() (*models.SystemMetrics, error) {
	resp, err := s.client.GetMetrics()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metrics from agent: %w", err)
	}

	// Convert agent.MetricsResponse to models.SystemMetrics
	metrics := &models.SystemMetrics{
		LoadAverage1:        resp.Metrics.LoadAverage1,
		LoadAverage5:        resp.Metrics.LoadAverage5,
		LoadAverage15:       resp.Metrics.LoadAverage15,
		MemoryTotalBytes:    resp.Metrics.MemoryTotalBytes,
		MemoryUsedBytes:     resp.Metrics.MemoryUsedBytes,
		MemoryUsagePercent:  resp.Metrics.MemoryUsagePercent,
		DiskTotalBytes:      resp.Metrics.DiskTotalBytes,
		DiskUsedBytes:       resp.Metrics.DiskUsedBytes,
		DiskUsagePercent:    resp.Metrics.DiskUsagePercent,
		NetworkRxBytes:      resp.Metrics.NetworkRxBytes,
		NetworkTxBytes:      resp.Metrics.NetworkTxBytes,
		NetworkRxMbps:       resp.Metrics.NetworkRxMbps,
		NetworkTxMbps:       resp.Metrics.NetworkTxMbps,
		NetworkRxPercent:    resp.Metrics.NetworkRxPercent,
		NetworkTxPercent:    resp.Metrics.NetworkTxPercent,
		CollectedAt:         resp.Metrics.CollectedAt,
		ServiceStatuses:     make(map[string]models.ServiceStatus),
	}

	// Calculate CPU usage from load average (assuming number of CPUs)
	if resp.Metrics.CPUCount > 0 {
		metrics.CPUUsagePercent = (resp.Metrics.LoadAverage1 / float64(resp.Metrics.CPUCount)) * 100
		if metrics.CPUUsagePercent > 100 {
			metrics.CPUUsagePercent = 100
		}
	}

	// Convert service statuses
	for _, svc := range resp.Services {
		metrics.ServiceStatuses[svc.Name] = models.ServiceStatus{
			Name:   svc.Name,
			Active: svc.Active,
			Status: svc.Status,
		}
	}

	return metrics, nil
}

// AgentLogCollector collects logs via the Agent API.
type AgentLogCollector struct {
	client *agent.Client
}

// NewAgentLogCollector creates a new Agent-based log collector.
func NewAgentLogCollector(client *agent.Client) *AgentLogCollector {
	return &AgentLogCollector{
		client: client,
	}
}

// CollectLog fetches logs from the specified source via the Agent API.
func (l *AgentLogCollector) CollectLog(logName string, lines int) (string, error) {
	resp, err := l.client.GetLogs(logName, lines)
	if err != nil {
		return "", fmt.Errorf("failed to fetch logs from agent: %w", err)
	}

	return resp.Content, nil
}

// CollectHAProxyLogs fetches HAProxy logs via the Agent API.
func (l *AgentLogCollector) CollectHAProxyLogs(lines int) (string, error) {
	return l.CollectLog("haproxy", lines)
}
