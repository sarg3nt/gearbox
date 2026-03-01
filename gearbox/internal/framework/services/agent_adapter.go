package services

import (
	"encoding/json"

	"github.com/sarg3nt/gearbox/internal/framework/agent"
	"github.com/sarg3nt/gearbox/internal/framework/gear"
)

// AgentAdapter wraps the agent.Client to implement gear.AgentClient.
// It provides a simplified interface for plugins to interact with the HAProxy agent.
type AgentAdapter struct {
	client *agent.Client
}

// NewAgentAdapter creates a new AgentAdapter wrapping an existing agent.Client.
func NewAgentAdapter(client *agent.Client) *AgentAdapter {
	return &AgentAdapter{client: client}
}

// IsConnected returns true if the agent is connected.
// This performs a health check to verify connectivity.
func (a *AgentAdapter) IsConnected() bool {
	if a.client == nil {
		return false
	}
	_, err := a.client.Health()
	return err == nil
}

// Get performs a GET request to the agent API.
// The path should not include the base URL or /api/v1 prefix.
// Results are unmarshaled into the provided result interface.
func (a *AgentAdapter) Get(path string, result any) error {
	// Map common paths to agent client methods
	// This is a simplification - plugins should use specific methods when possible
	switch path {
	case "/health":
		health, err := a.client.Health()
		if err != nil {
			return err
		}
		return copyToResult(health, result)
	case "/api/v1/metrics":
		metrics, err := a.client.GetMetrics()
		if err != nil {
			return err
		}
		return copyToResult(metrics, result)
	case "/api/v1/haproxy/stats":
		stats, err := a.client.GetStats()
		if err != nil {
			return err
		}
		return copyToResult(stats, result)
	case "/api/v1/haproxy/info":
		info, err := a.client.GetInfo()
		if err != nil {
			return err
		}
		return copyToResult(info, result)
	case "/api/v1/logs":
		sources, err := a.client.GetLogSources()
		if err != nil {
			return err
		}
		return copyToResult(sources, result)
	case "/api/v1/services":
		services, err := a.client.GetServices(nil)
		if err != nil {
			return err
		}
		return copyToResult(services, result)
	case "/api/v1/certificates":
		certs, err := a.client.GetCertificates()
		if err != nil {
			return err
		}
		return copyToResult(certs, result)
	case "/api/v1/security/summary":
		summary, err := a.client.GetSecuritySummary()
		if err != nil {
			return err
		}
		return copyToResult(summary, result)
	default:
		// For other paths, return an error suggesting to use specific methods
		return &gear.GearError{
			Code:    "unsupported_path",
			Message: "path not supported by adapter, use GetClient() for direct access",
		}
	}
}

// Post performs a POST request to the agent API.
// The path should not include the base URL.
// Results are unmarshaled into the provided result interface.
func (a *AgentAdapter) Post(path string, body, result any) error {
	// For POST requests, recommend using specific methods
	return &gear.GearError{
		Code:    "use_specific_method",
		Message: "POST not supported by adapter, use GetClient() for direct access",
	}
}

// Delete performs a DELETE request to the agent API.
func (a *AgentAdapter) Delete(path string, result any) error {
	// For DELETE requests, recommend using specific methods
	return &gear.GearError{
		Code:    "use_specific_method",
		Message: "DELETE not supported by adapter, use GetClient() for direct access",
	}
}

// GetClient returns the underlying agent.Client for direct access.
// This allows plugins to use the full agent API when needed.
func (a *AgentAdapter) GetClient() *agent.Client {
	return a.client
}

// copyToResult copies a source struct to the result using JSON marshal/unmarshal.
// This is a simple way to copy data without reflection.
func copyToResult(source, result any) error {
	data, err := json.Marshal(source)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, result)
}

// Ensure AgentAdapter implements gear.AgentClient
var _ gear.AgentClient = (*AgentAdapter)(nil)
