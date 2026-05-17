package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sarg3nt/gearbox-agent/internal/framework/services/haproxy"
)

// MetadataProvider provides access to HAProxy metadata.
type MetadataProvider interface {
	GetMetadata() *haproxy.Metadata
	GetLastError() error
	GetLastSyncTime() time.Time
}

// Handlers holds HTTP handlers and their dependencies.
type Handlers struct {
	metadataProvider MetadataProvider
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(metadataProvider MetadataProvider) *Handlers {
	return &Handlers{
		metadataProvider: metadataProvider,
	}
}

// HealthResponse represents the health check response.
//
// Only "status" is returned on the unauthenticated /health endpoint to avoid
// leaking version / uptime to remote scanners probing for known-vulnerable
// agent versions. Version and build info are still available on the build
// itself (--version) and through service logs, but are not exposed over the
// network on unauthenticated endpoints. See 2026-05 security audit P2-5.
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

// Health handles GET /health (no auth required).
//
//	@Summary		Health check
//	@Description	Returns the health status of the gearbox-agent service. No authentication required. Only "status" is exposed; version and uptime are intentionally omitted from this unauthenticated endpoint to avoid fingerprinting by remote scanners.
//	@Tags			Health
//	@Produce		json
//	@Success		200	{object}	HealthResponse	"Service is healthy"
//	@Router			/health [get]
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	resp := HealthResponse{Status: "ok"}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// MetadataResponse wraps the metadata with sync status.
type MetadataResponse struct {
	Metadata     *haproxy.Metadata `json:"metadata"`
	LastSyncTime time.Time         `json:"last_sync_time"`
	LastError    string            `json:"last_error,omitempty"`
}

// Metadata handles GET /api/v1/metadata.
//
//	@Summary		HAProxy metadata
//	@Description	Returns HAProxy configuration metadata including frontends, backends, and their settings from the last sync.
//	@Tags			Sync
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	MetadataResponse	"HAProxy metadata"
//	@Failure		401	{string}	string				"Unauthorized"
//	@Failure		503	{string}	string				"Sync service not configured or pending"
//	@Router			/api/v1/metadata [get]
func (h *Handlers) Metadata(w http.ResponseWriter, r *http.Request) {
	if h.metadataProvider == nil {
		http.Error(w, "Metadata not available (sync service not configured)", http.StatusServiceUnavailable)
		return
	}

	metadata := h.metadataProvider.GetMetadata()
	if metadata == nil {
		http.Error(w, "Metadata not yet available (sync pending)", http.StatusServiceUnavailable)
		return
	}

	var lastErr string
	if err := h.metadataProvider.GetLastError(); err != nil {
		lastErr = err.Error()
	}

	resp := MetadataResponse{
		Metadata:     metadata,
		LastSyncTime: h.metadataProvider.GetLastSyncTime(),
		LastError:    lastErr,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// SyncStatusResponse represents the sync service status.
type SyncStatusResponse struct {
	LastSyncTime time.Time `json:"last_sync_time"`
	LastError    string    `json:"last_error,omitempty"`
	BackendCount int       `json:"backend_count"`
}

// SyncStatus handles GET /api/v1/sync/status.
//
//	@Summary		Sync status
//	@Description	Returns the status of the Git sync service including last sync time, error status, and backend count.
//	@Tags			Sync
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	SyncStatusResponse	"Sync status"
//	@Failure		401	{string}	string				"Unauthorized"
//	@Failure		503	{string}	string				"Sync service not configured"
//	@Router			/api/v1/sync/status [get]
func (h *Handlers) SyncStatus(w http.ResponseWriter, r *http.Request) {
	if h.metadataProvider == nil {
		http.Error(w, "Sync service not configured", http.StatusServiceUnavailable)
		return
	}

	var lastErr string
	if err := h.metadataProvider.GetLastError(); err != nil {
		lastErr = err.Error()
	}

	backendCount := 0
	if metadata := h.metadataProvider.GetMetadata(); metadata != nil {
		backendCount = len(metadata.Backends)
	}

	resp := SyncStatusResponse{
		LastSyncTime: h.metadataProvider.GetLastSyncTime(),
		LastError:    lastErr,
		BackendCount: backendCount,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
