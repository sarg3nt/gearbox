package models

import "time"

// SystemMetrics represents system-level metrics from the HAProxy server.
type SystemMetrics struct {
	CPUUsagePercent      float64                  `json:"cpu_usage_percent"`
	MemoryUsedBytes      int64                    `json:"memory_used_bytes"`
	MemoryTotalBytes     int64                    `json:"memory_total_bytes"`
	MemoryUsagePercent   float64                  `json:"memory_usage_percent"`
	DiskUsedBytes        int64                    `json:"disk_used_bytes"`
	DiskTotalBytes       int64                    `json:"disk_total_bytes"`
	DiskUsagePercent     float64                  `json:"disk_usage_percent"`
	NetworkRxBytes       int64                    `json:"network_rx_bytes"` // Cumulative bytes received (for delta calculation)
	NetworkTxBytes       int64                    `json:"network_tx_bytes"` // Cumulative bytes transmitted (for delta calculation)
	NetworkRxMbps        float64                  `json:"network_rx_mbps"`
	NetworkTxMbps        float64                  `json:"network_tx_mbps"`
	NetworkCapacityMbps  float64                  `json:"network_capacity_mbps"` // Network interface capacity (e.g., 10000 for 10Gbps)
	NetworkRxPercent     float64                  `json:"network_rx_percent"`
	NetworkTxPercent     float64                  `json:"network_tx_percent"`
	LoadAverage1         float64                  `json:"load_average_1"`
	LoadAverage5         float64                  `json:"load_average_5"`
	LoadAverage15        float64                  `json:"load_average_15"`
	Uptime               time.Duration            `json:"uptime"`
	ServiceStatuses      map[string]ServiceStatus `json:"service_statuses"`
	CollectedAt          time.Time                `json:"collected_at"`
}

// ServiceStatus represents the status of a systemd service.
type ServiceStatus struct {
	Name        string `json:"name"`
	Active      bool   `json:"active"`
	Status      string `json:"status"` // "active", "inactive", "failed", etc.
	Description string `json:"description,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	Uptime      string `json:"uptime,omitempty"`
}

// CalculateCPUUsage calculates CPU usage percentage from load average.
func (s *SystemMetrics) CalculateCPUUsage() float64 {
	// Simple approximation: load average / number of CPUs
	// For now, we'll use load average directly as a rough indicator
	return s.LoadAverage1 * 100
}
