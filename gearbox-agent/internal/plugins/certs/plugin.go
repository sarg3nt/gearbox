// Package certs provides certificate monitoring as a plugin.
package certs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	
	"github.com/sarg3nt/gearbox-agent/internal/framework/events"
	"github.com/sarg3nt/gearbox-agent/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// validDomainName matches domain names (letters, numbers, hyphens, dots, and wildcards).
var validDomainName = regexp.MustCompile(`^(\*\.)?[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$`)

// Plugin provides certificate monitoring functionality.
type Plugin struct {
	plugin.BasePlugin
	collector *Collector
	eventBus  *events.Bus
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "certificates",
		DisplayName: "Certificate Monitor",
		Description: "Monitors TLS certificates, tracks expiration, and triggers renewals",
		Version:     "1.0.0",
		Category:    "security",
		Core:        false, // Not always needed
	}
}

// Initialize sets up the plugin.
func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
	if err := p.BasePlugin.Initialize(ctx, deps); err != nil {
		return err
	}

	// Create collector with optional certbot timer from config
	p.collector = NewCollector(deps.Logger, deps.CertbotTimer)
	p.eventBus = deps.EventBus
	return nil
}

// Start is a no-op - collection is handled by the plugin manager.
func (p *Plugin) Start(ctx context.Context) error {
	return nil
}

// Stop cleans up resources.
func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}

// RegisterRoutes registers HTTP API routes.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	r.Get("/api/v1/certificates", p.handleList)
	r.Post("/api/v1/certificates/{domain}/refresh", p.handleRefresh)
	r.Get("/api/v1/certificates/{domain}/download", p.handleDownload)
}

// Collectors returns the periodic collectors for this plugin.
func (p *Plugin) Collectors() []plugin.Collector {
	return []plugin.Collector{
		{
			Name:     "certificates",
			Interval: 5 * time.Minute, // Check certificates every 5 minutes
			Collect: func(ctx context.Context) (any, error) {
				return p.collector.Collect()
			},
			OnData: func(data any) error {
				return p.publishCertificates(data)
			},
		},
	}
}

// EventTypes returns the events this plugin publishes.
func (p *Plugin) EventTypes() []plugin.EventType {
	return []plugin.EventType{
		{
			Name:        string(events.EventCertificatesUpdated),
			Description: "Published when certificate status is collected",
			Payload:     "CertificateMetrics with all certificate details and expiration status",
		},
	}
}

// publishCertificates publishes certificate metrics to the event bus.
func (p *Plugin) publishCertificates(data any) error {
	metrics, ok := data.(*CertificateMetrics)
	if !ok {
		return nil
	}

	eventData := map[string]any{
		"collected_at":       metrics.CollectedAt.UTC().Format(time.RFC3339),
		"cert_count":         len(metrics.Certificates),
		"cert_manager":       metrics.CertManager,
		"auto_renewal":       metrics.AutoRenewalEnabled,
		"certificates":       metrics.Certificates,
	}

	p.eventBus.Publish(events.Event{
		Type:      events.EventCertificatesUpdated,
		Timestamp: time.Now(),
		Data:      eventData,
	})

	p.Logger().Debug("published certificates update", "count", len(metrics.Certificates))
	return nil
}

// HTTP Handlers

// handleList handles GET /api/v1/certificates.
func (p *Plugin) handleList(w http.ResponseWriter, r *http.Request) {
	metrics, err := p.collector.Collect()
	if err != nil {
		http.Error(w, "Failed to collect certificates: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// handleRefresh handles POST /api/v1/certificates/{domain}/refresh.
func (p *Plugin) handleRefresh(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	if domain == "" {
		http.Error(w, "Domain name required", http.StatusBadRequest)
		return
	}

	// Validate domain name to prevent path traversal attacks
	if !validDomainName.MatchString(domain) {
		http.Error(w, "Invalid domain name", http.StatusBadRequest)
		return
	}

	// Trigger certificate refresh
	result, err := p.collector.RefreshCertificate(domain)
	if err != nil {
		http.Error(w, "Refresh failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleDownload handles GET /api/v1/certificates/{domain}/download.
func (p *Plugin) handleDownload(w http.ResponseWriter, r *http.Request) {
	domain := chi.URLParam(r, "domain")
	if domain == "" {
		http.Error(w, "Domain name required", http.StatusBadRequest)
		return
	}

	// Validate domain name to prevent path traversal attacks
	if !validDomainName.MatchString(domain) {
		http.Error(w, "Invalid domain name", http.StatusBadRequest)
		return
	}

	// First, try to find the certificate in our collected data to get its actual file path
	metrics, err := p.collector.Collect()
	if err == nil {
		for _, cert := range metrics.Certificates {
			if cert.Domain == domain && cert.FilePath != "" {
				// Found the certificate - use its actual file path
				certData, err := os.ReadFile(cert.FilePath)
				if err != nil {
					http.Error(w, "Failed to read certificate: "+err.Error(), http.StatusInternalServerError)
					return
				}

				filename := filepath.Base(cert.FilePath)
				w.Header().Set("Content-Type", "application/x-pem-file")
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(certData)))
				w.Write(certData)
				return
			}
		}
	}

	// Fallback: Look for the certificate file in HAProxy certs directory by constructed name
	haproxyCertsDir := "/etc/haproxy/certs"

	// For wildcard domains, also try without the "*." prefix
	domainsToTry := []string{domain}
	if strings.HasPrefix(domain, "*.") {
		domainsToTry = append(domainsToTry, strings.TrimPrefix(domain, "*."))
	}

	var certPath string
	for _, d := range domainsToTry {
		// Try .pem first, then .crt
		for _, ext := range []string{".pem", ".crt"} {
			tryPath := filepath.Join(haproxyCertsDir, d+ext)
			if _, err := os.Stat(tryPath); err == nil {
				certPath = tryPath
				break
			}
		}
		if certPath != "" {
			break
		}
	}

	if certPath == "" {
		http.Error(w, fmt.Sprintf("Certificate for domain %s not found", domain), http.StatusNotFound)
		return
	}

	// Read the certificate file
	certData, err := os.ReadFile(certPath)
	if err != nil {
		http.Error(w, "Failed to read certificate: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Set headers for file download
	filename := filepath.Base(certPath)
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(certData)))
	w.Write(certData)
}

// GetCollector returns the certificate collector for use by other components.
func (p *Plugin) GetCollector() *Collector {
	return p.collector
}

// Ensure plugin implements required interfaces.
var (
	_ plugin.Plugin          = (*Plugin)(nil)
	_ plugin.CollectorPlugin = (*Plugin)(nil)
)
