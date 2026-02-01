// Package security provides security monitoring (fail2ban, firewall) as a plugin.
package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox-agent/internal/framework/plugin"
)

func init() {
	plugin.Register(&Plugin{})
}

// Plugin provides security monitoring functionality.
type Plugin struct {
	plugin.BasePlugin
	fail2ban       *Fail2BanCollector
	firewall       *FirewallCollector
	firewallConfig string
	backupDir      string
}

// Info returns plugin metadata.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		Name:        "security",
		DisplayName: "Security Monitor",
		Description: "Monitors fail2ban and firewall (nftables) statistics",
		Version:     "1.0.0",
		Category:    "security",
		Core:        false,
	}
}

// Initialize sets up the plugin.
func (p *Plugin) Initialize(ctx context.Context, deps plugin.Dependencies) error {
	if err := p.BasePlugin.Initialize(ctx, deps); err != nil {
		return err
	}

	p.fail2ban = NewFail2BanCollector()
	p.firewall = NewFirewallCollector()

	// Set firewall config path (default to /etc/nftables.conf)
	p.firewallConfig = os.Getenv("FIREWALL_CONFIG_PATH")
	if p.firewallConfig == "" {
		p.firewallConfig = "/etc/nftables.conf"
	}

	// Set backup directory
	p.backupDir = filepath.Join(filepath.Dir(p.firewallConfig), "backups")

	return nil
}

// Start is a no-op.
func (p *Plugin) Start(ctx context.Context) error {
	return nil
}

// Stop cleans up resources.
func (p *Plugin) Stop(ctx context.Context) error {
	return nil
}

// RegisterRoutes registers HTTP API routes.
func (p *Plugin) RegisterRoutes(r chi.Router) {
	// Security monitoring
	r.Get("/api/v1/security/fail2ban", p.handleFail2BanStats)
	r.Get("/api/v1/security/firewall", p.handleFirewallStats)
	r.Get("/api/v1/security/summary", p.handleSecuritySummary)

	// Firewall blocking
	r.Post("/api/v1/firewall/block", p.handleBlock)
	r.Delete("/api/v1/firewall/block/{ip}", p.handleUnblock)
	r.Get("/api/v1/firewall/blocked", p.handleListBlocked)
	r.Get("/api/v1/firewall/blocked/{ip}", p.handleCheckBlocked)

	// Firewall configuration
	r.Get("/api/v1/firewall/config", p.handleConfigRead)
	r.Post("/api/v1/firewall/config", p.handleConfigUpdate)
	r.Get("/api/v1/firewall/config/backups", p.handleListBackups)
	r.Post("/api/v1/firewall/config/restore", p.handleConfigRestore)
}

// EventTypes returns the events this plugin publishes.
func (p *Plugin) EventTypes() []plugin.EventType {
	return []plugin.EventType{
		{
			Name:        "updated",
			Description: "Published when security status changes",
			Payload:     "Security summary with fail2ban and firewall status",
		},
	}
}

// HTTP Handlers

// handleFail2BanStats handles GET /api/v1/security/fail2ban.
func (p *Plugin) handleFail2BanStats(w http.ResponseWriter, r *http.Request) {
	includeIPs := r.URL.Query().Get("include_ips") == "true"
	recentCount := 0
	if rc := r.URL.Query().Get("recent"); rc != "" {
		recentCount, _ = strconv.Atoi(rc)
		if recentCount < 0 {
			recentCount = 0
		} else if recentCount > 100 {
			recentCount = 100 // Cap at 100 recent events
		}
	}

	stats, err := p.fail2ban.Collect(includeIPs, recentCount)
	if err != nil {
		if errors.Is(err, ErrServiceNotInstalled) {
			// Service not installed - return 200 with available=false
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(stats)
			return
		}
		p.Logger().Error("fail2ban stats collection error", "error", err)
		http.Error(w, "Failed to collect fail2ban stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleFirewallStats handles GET /api/v1/security/firewall.
func (p *Plugin) handleFirewallStats(w http.ResponseWriter, r *http.Request) {
	includeRules := r.URL.Query().Get("include_rules") == "true"
	recentCount := 0
	if rc := r.URL.Query().Get("recent"); rc != "" {
		recentCount, _ = strconv.Atoi(rc)
		if recentCount < 0 {
			recentCount = 0
		} else if recentCount > 100 {
			recentCount = 100 // Cap at 100 recent events
		}
	}

	stats, err := p.firewall.Collect(includeRules, recentCount)
	if err != nil {
		if errors.Is(err, ErrServiceNotInstalled) {
			// Service not installed - return 200 with available=false
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(stats)
			return
		}
		p.Logger().Error("firewall stats collection error", "error", err)
		http.Error(w, "Failed to collect firewall stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// SecuritySummary provides a quick overview of security status.
type SecuritySummary struct {
	Fail2Ban struct {
		Available   bool   `json:"available"`
		Running     bool   `json:"running"`
		TotalBanned int    `json:"total_banned"`
		JailCount   int    `json:"jail_count"`
		Error       string `json:"error,omitempty"`
	} `json:"fail2ban"`
	Firewall struct {
		Available    bool   `json:"available"`
		Running      bool   `json:"running"`
		RecentBlocks int    `json:"recent_blocks"`
		Error        string `json:"error,omitempty"`
	} `json:"firewall"`
}

// handleSecuritySummary handles GET /api/v1/security/summary.
func (p *Plugin) handleSecuritySummary(w http.ResponseWriter, r *http.Request) {
	summary := SecuritySummary{}

	// Get fail2ban summary
	f2bStats, err := p.fail2ban.Collect(false, 0)
	if err != nil && !errors.Is(err, ErrServiceNotInstalled) {
		p.Logger().Error("fail2ban summary collection error", "error", err)
		summary.Fail2Ban.Error = err.Error()
	}
	if f2bStats != nil {
		summary.Fail2Ban.Available = f2bStats.Available
		summary.Fail2Ban.Running = f2bStats.Running
		summary.Fail2Ban.TotalBanned = f2bStats.TotalBanned
		summary.Fail2Ban.JailCount = len(f2bStats.Jails)
	}

	// Get firewall summary
	fwStats, err := p.firewall.Collect(false, 50)
	if err != nil && !errors.Is(err, ErrServiceNotInstalled) {
		p.Logger().Error("firewall summary collection error", "error", err)
		summary.Firewall.Error = err.Error()
	}
	if fwStats != nil {
		summary.Firewall.Available = fwStats.Available
		summary.Firewall.Running = fwStats.Running
		summary.Firewall.RecentBlocks = len(fwStats.RecentBlocks)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// GetFail2BanCollector returns the fail2ban collector for use by other components.
func (p *Plugin) GetFail2BanCollector() *Fail2BanCollector {
	return p.fail2ban
}

// GetFirewallCollector returns the firewall collector for use by other components.
func (p *Plugin) GetFirewallCollector() *FirewallCollector {
	return p.firewall
}

// Firewall Blocking Handlers

// Pre-compiled regex for validating IP addresses
var ipRegex = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)

// BlockRequest represents a request to block an IP.
type BlockRequest struct {
	IP       string `json:"ip"`
	Reason   string `json:"reason"`
	Duration int    `json:"duration,omitempty"` // Duration in minutes, 0 = permanent
}

// BlockResponse represents the response from a block operation.
type BlockResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	IP      string `json:"ip"`
}

// BlockedIPInfo represents information about a blocked IP.
type BlockedIPInfo struct {
	IP        string    `json:"ip"`
	Packets   int64     `json:"packets"`
	Bytes     int64     `json:"bytes"`
	BlockedAt time.Time `json:"blocked_at,omitempty"`
}

// BlockedIPsResponse represents the list of blocked IPs.
type BlockedIPsResponse struct {
	Available  bool            `json:"available"`
	BlockedIPs []BlockedIPInfo `json:"blocked_ips"`
	Count      int             `json:"count"`
}

// handleBlock handles POST /api/v1/firewall/block
func (p *Plugin) handleBlock(w http.ResponseWriter, r *http.Request) {
	var req BlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate IP address
	if !isValidIP(req.IP) {
		http.Error(w, "Invalid IP address", http.StatusBadRequest)
		return
	}

	// Don't allow blocking localhost or private ranges that might be legitimate
	if isProtectedIP(req.IP) {
		http.Error(w, "Cannot block protected IP addresses (localhost, internal ranges)", http.StatusBadRequest)
		return
	}

	// Ensure the blocklist set exists
	if err := ensureBlocklistSet(); err != nil {
		p.Logger().Error("Failed to ensure blocklist set", "error", err)
		http.Error(w, "Failed to setup blocklist: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Add IP to the set
	cmd := exec.Command("nft", "add", "element", "inet", "filter", "blocklist", "{", req.IP, "}")
	if output, err := cmd.CombinedOutput(); err != nil {
		p.Logger().Error("Failed to block IP", "ip", req.IP, "error", err, "output", string(output))
		http.Error(w, "Failed to block IP: "+err.Error(), http.StatusInternalServerError)
		return
	}

	p.Logger().Info("IP blocked", "ip", req.IP, "reason", req.Reason)

	response := BlockResponse{
		Success: true,
		Message: "IP blocked successfully",
		IP:      req.IP,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleUnblock handles DELETE /api/v1/firewall/block/{ip}
func (p *Plugin) handleUnblock(w http.ResponseWriter, r *http.Request) {
	// Extract IP from path
	ip := chi.URLParam(r, "ip")
	if ip == "" {
		http.Error(w, "IP address required", http.StatusBadRequest)
		return
	}

	// Validate IP address
	if !isValidIP(ip) {
		http.Error(w, "Invalid IP address", http.StatusBadRequest)
		return
	}

	// Remove IP from the blocklist set
	cmd := exec.Command("nft", "delete", "element", "inet", "filter", "blocklist", "{", ip, "}")
	if output, err := cmd.CombinedOutput(); err != nil {
		// Check if the error is because the element doesn't exist
		if strings.Contains(string(output), "No such") || strings.Contains(string(output), "does not exist") {
			http.Error(w, "IP not found in blocklist", http.StatusNotFound)
			return
		}
		p.Logger().Error("Failed to unblock IP", "ip", ip, "error", err, "output", string(output))
		http.Error(w, "Failed to unblock IP: "+err.Error(), http.StatusInternalServerError)
		return
	}

	p.Logger().Info("IP unblocked", "ip", ip)

	response := BlockResponse{
		Success: true,
		Message: "IP unblocked successfully",
		IP:      ip,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleListBlocked handles GET /api/v1/firewall/blocked
func (p *Plugin) handleListBlocked(w http.ResponseWriter, r *http.Request) {
	response := BlockedIPsResponse{
		Available:  true,
		BlockedIPs: []BlockedIPInfo{},
	}

	// Check if nftables is available
	if _, err := exec.LookPath("nft"); err != nil {
		response.Available = false
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Get the blocklist set contents
	cmd := exec.Command("nft", "list", "set", "inet", "filter", "blocklist")
	output, err := cmd.Output()
	if err != nil {
		// Set might not exist yet, which is fine
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Parse the output to extract IPs
	blockedIPs := parseBlocklistOutput(string(output))
	response.BlockedIPs = blockedIPs
	response.Count = len(blockedIPs)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCheckBlocked handles GET /api/v1/firewall/blocked/{ip}
func (p *Plugin) handleCheckBlocked(w http.ResponseWriter, r *http.Request) {
	// Extract IP from path
	ip := chi.URLParam(r, "ip")
	if ip == "" {
		http.Error(w, "IP address required", http.StatusBadRequest)
		return
	}

	// Validate IP address
	if !isValidIP(ip) {
		http.Error(w, "Invalid IP address", http.StatusBadRequest)
		return
	}

	// Check if IP is in the blocklist
	cmd := exec.Command("nft", "list", "set", "inet", "filter", "blocklist")
	output, err := cmd.Output()
	if err != nil {
		// Set doesn't exist, so IP is not blocked
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ip":      ip,
			"blocked": false,
		})
		return
	}

	isBlocked := strings.Contains(string(output), ip)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ip":      ip,
		"blocked": isBlocked,
	})
}

// Firewall Configuration Handlers

// FirewallConfigSection represents a parsed section of the nftables config.
type FirewallConfigSection struct {
	Type      string `json:"type"`       // "table", "chain", "set", "map"
	Name      string `json:"name"`       // Section name
	StartLine int    `json:"start_line"` // 1-indexed
	EndLine   int    `json:"end_line"`   // 1-indexed, inclusive
}

// FirewallConfigReadResponse is returned when reading the firewall configuration.
type FirewallConfigReadResponse struct {
	Content      string                  `json:"content"`
	Sections     []FirewallConfigSection `json:"sections"`
	SHA256       string                  `json:"sha256"`
	LastModified time.Time               `json:"last_modified"`
	FilePath     string                  `json:"file_path"`
}

// FirewallConfigUpdateRequest is the request body for updating firewall configuration.
type FirewallConfigUpdateRequest struct {
	Content      string `json:"content"`
	ExpectedSHA  string `json:"expected_sha"`  // For optimistic locking
	BackupReason string `json:"backup_reason"` // Reason for the change
	DryRun       bool   `json:"dry_run"`       // If true, validate only
}

// FirewallConfigUpdateResponse is returned after updating firewall configuration.
type FirewallConfigUpdateResponse struct {
	Success          bool     `json:"success"`
	ValidationOutput string   `json:"validation_output"`
	BackupPath       string   `json:"backup_path,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	Message          string   `json:"message"`
	NewSHA256        string   `json:"new_sha256,omitempty"`
}

// FirewallConfigBackup represents a configuration backup file.
type FirewallConfigBackup struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"created_at"`
	Reason    string    `json:"reason"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
}

// handleConfigRead handles GET /api/v1/firewall/config
func (p *Plugin) handleConfigRead(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile(p.firewallConfig)
	if err != nil {
		p.Logger().Error("Failed to read firewall config", "error", err)
		http.Error(w, "Failed to read configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get file info for last modified time
	fileInfo, err := os.Stat(p.firewallConfig)
	if err != nil {
		p.Logger().Error("Failed to stat firewall config", "error", err)
		http.Error(w, "Failed to get file info: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate SHA256
	hash := sha256.Sum256(content)
	sha := hex.EncodeToString(hash[:])

	// Parse sections
	sections := p.parseNftablesSections(string(content))

	response := FirewallConfigReadResponse{
		Content:      string(content),
		Sections:     sections,
		SHA256:       sha,
		LastModified: fileInfo.ModTime(),
		FilePath:     p.firewallConfig,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleConfigUpdate handles POST /api/v1/firewall/config
func (p *Plugin) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	var req FirewallConfigUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	response := FirewallConfigUpdateResponse{}

	// Check expected SHA if provided (optimistic locking)
	if req.ExpectedSHA != "" {
		currentContent, err := os.ReadFile(p.firewallConfig)
		if err != nil {
			response.Success = false
			response.Message = "Failed to read current config: " + err.Error()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		hash := sha256.Sum256(currentContent)
		currentSHA := hex.EncodeToString(hash[:])

		if currentSHA != req.ExpectedSHA {
			response.Success = false
			response.Message = "Configuration has been modified since last read. Please reload and try again."
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(response)
			return
		}
	}

	// Validate the configuration using nft -c -f
	validationOutput, err := p.validateConfig(req.Content)
	response.ValidationOutput = validationOutput

	if err != nil {
		response.Success = false
		response.Message = "Configuration validation failed"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// If dry run, stop here
	if req.DryRun {
		response.Success = true
		response.Message = "Configuration is valid (dry run)"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create backup before making changes
	backupPath, err := p.createBackup(req.BackupReason)
	if err != nil {
		p.Logger().Warn("Failed to create backup", "error", err)
		response.Warnings = append(response.Warnings, "Failed to create backup: "+err.Error())
	} else {
		response.BackupPath = backupPath
	}

	// Write the new configuration
	if err := os.WriteFile(p.firewallConfig, []byte(req.Content), 0644); err != nil {
		response.Success = false
		response.Message = "Failed to write configuration: " + err.Error()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Apply the configuration
	if err := p.applyConfig(); err != nil {
		// Rollback - try to restore from backup
		if backupPath != "" {
			if backupContent, readErr := os.ReadFile(backupPath); readErr == nil {
				if restoreErr := os.WriteFile(p.firewallConfig, backupContent, 0644); restoreErr != nil {
					p.Logger().Error("Failed to restore config from backup", "error", restoreErr)
				}
			}
		}
		response.Success = false
		response.Message = "Failed to apply configuration: " + err.Error()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Calculate new SHA
	hash := sha256.Sum256([]byte(req.Content))
	response.NewSHA256 = hex.EncodeToString(hash[:])
	response.Success = true
	response.Message = "Configuration updated and applied successfully"

	p.Logger().Info("Firewall configuration updated", "reason", req.BackupReason)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleListBackups handles GET /api/v1/firewall/config/backups
func (p *Plugin) handleListBackups(w http.ResponseWriter, r *http.Request) {
	backups := []FirewallConfigBackup{}

	// Ensure backup directory exists
	if err := os.MkdirAll(p.backupDir, 0755); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"backups": backups,
		})
		return
	}

	entries, err := os.ReadDir(p.backupDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"backups": backups,
		})
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only include .conf backup files
		if !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		fullPath := filepath.Join(p.backupDir, entry.Name())
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		hash := sha256.Sum256(content)

		// Extract reason from filename (format: nftables_20060102_150405_reason.conf)
		reason := ""
		parts := strings.Split(strings.TrimSuffix(entry.Name(), ".conf"), "_")
		if len(parts) > 3 {
			reason = strings.Join(parts[3:], "_")
		}

		backups = append(backups, FirewallConfigBackup{
			ID:        entry.Name(),
			Filename:  entry.Name(),
			CreatedAt: info.ModTime(),
			Reason:    reason,
			SHA256:    hex.EncodeToString(hash[:]),
			SizeBytes: info.Size(),
		})
	}

	// Sort by creation time, newest first
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"backups": backups,
	})
}

// handleConfigRestore handles POST /api/v1/firewall/config/restore
func (p *Plugin) handleConfigRestore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BackupID string `json:"backup_id"`
		DryRun   bool   `json:"dry_run"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	response := FirewallConfigUpdateResponse{}

	// Read the backup file
	backupPath := filepath.Join(p.backupDir, req.BackupID)
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		response.Success = false
		response.Message = "Failed to read backup: " + err.Error()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Validate the backup content
	validationOutput, err := p.validateConfig(string(backupContent))
	response.ValidationOutput = validationOutput

	if err != nil {
		response.Success = false
		response.Message = "Backup validation failed"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	if req.DryRun {
		response.Success = true
		response.Message = "Backup is valid (dry run)"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Create a backup of current config before restoring
	_, err = p.createBackup("pre-restore")
	if err != nil {
		p.Logger().Warn("Failed to create pre-restore backup", "error", err)
	}

	// Write the backup content to the config file
	if err := os.WriteFile(p.firewallConfig, backupContent, 0644); err != nil {
		response.Success = false
		response.Message = "Failed to write configuration: " + err.Error()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Apply the configuration
	if err := p.applyConfig(); err != nil {
		response.Success = false
		response.Message = "Failed to apply restored configuration: " + err.Error()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Calculate new SHA
	hash := sha256.Sum256(backupContent)
	response.NewSHA256 = hex.EncodeToString(hash[:])
	response.Success = true
	response.Message = "Configuration restored successfully"

	p.Logger().Info("Firewall configuration restored", "backup", req.BackupID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Helper functions

// isValidIP validates an IP address string.
func isValidIP(ip string) bool {
	// Basic format check
	if !ipRegex.MatchString(ip) {
		return false
	}

	// Parse and validate
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Must be IPv4 for now
	return parsedIP.To4() != nil
}

// isProtectedIP checks if an IP should be protected from blocking.
func isProtectedIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Protect localhost
	if parsedIP.IsLoopback() {
		return true
	}

	// Protect the machine's own addresses
	if parsedIP.String() == "127.0.0.1" || parsedIP.String() == "::1" {
		return true
	}

	return false
}

// ensureBlocklistSet creates the blocklist set if it doesn't exist.
func ensureBlocklistSet() error {
	// Check if set exists
	cmd := exec.Command("nft", "list", "set", "inet", "filter", "blocklist")
	if err := cmd.Run(); err == nil {
		return nil // Set already exists
	}

	// Create the set
	cmd = exec.Command("nft", "add", "set", "inet", "filter", "blocklist", "{", "type", "ipv4_addr;", "}")
	if output, err := cmd.CombinedOutput(); err != nil {
		return err
	} else {
		// If set was just created, add a rule to use it
		cmd = exec.Command("nft", "insert", "rule", "inet", "filter", "input", "ip", "saddr", "@blocklist", "drop")
		if _, err := cmd.CombinedOutput(); err != nil {
			// Rule might already exist, which is fine
			_ = output
		}
	}

	return nil
}

// parseBlocklistOutput parses the nft output to extract blocked IPs.
func parseBlocklistOutput(output string) []BlockedIPInfo {
	var ips []BlockedIPInfo

	// Look for the elements line
	elemStart := strings.Index(output, "elements = {")
	if elemStart == -1 {
		return ips
	}

	elemEnd := strings.Index(output[elemStart:], "}")
	if elemEnd == -1 {
		return ips
	}

	elemSection := output[elemStart+len("elements = {") : elemStart+elemEnd]
	elemSection = strings.TrimSpace(elemSection)

	if elemSection == "" {
		return ips
	}

	// Split by comma and parse each IP
	parts := strings.Split(elemSection, ",")
	for _, part := range parts {
		ip := strings.TrimSpace(part)
		if ip != "" && isValidIP(ip) {
			ips = append(ips, BlockedIPInfo{
				IP: ip,
			})
		}
	}

	return ips
}

// validateConfig validates the nftables configuration using nft -c -f
func (p *Plugin) validateConfig(content string) (string, error) {
	// Write content to a temp file
	tmpFile, err := os.CreateTemp("", "nftables-validate-*.conf")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	// Validate using nft -c -f (check mode)
	cmd := exec.Command("nft", "-c", "-f", tmpFile.Name())
	output, err := cmd.CombinedOutput()

	return string(output), err
}

// applyConfig applies the nftables configuration
func (p *Plugin) applyConfig() error {
	// First flush existing rules and apply the new config
	cmd := exec.Command("nft", "-f", p.firewallConfig)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft apply failed: %s: %w", string(output), err)
	}
	return nil
}

// createBackup creates a backup of the current configuration
func (p *Plugin) createBackup(reason string) (string, error) {
	// Ensure backup directory exists
	if err := os.MkdirAll(p.backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Read current config
	content, err := os.ReadFile(p.firewallConfig)
	if err != nil {
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	// Clean up reason for filename
	safeReason := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, reason)
	if len(safeReason) > 30 {
		safeReason = safeReason[:30]
	}

	// Create backup filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("nftables_%s_%s.conf", timestamp, safeReason)
	backupPath := filepath.Join(p.backupDir, filename)

	// Write backup
	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	// Clean up old backups (keep last 20)
	p.cleanupOldBackups(20)

	return backupPath, nil
}

// cleanupOldBackups removes old backups, keeping the most recent count
func (p *Plugin) cleanupOldBackups(keep int) {
	entries, err := os.ReadDir(p.backupDir)
	if err != nil {
		return
	}

	// Filter to only .conf files and sort by modification time
	type backupFile struct {
		name    string
		modTime time.Time
	}

	var backups []backupFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupFile{
			name:    entry.Name(),
			modTime: info.ModTime(),
		})
	}

	// Sort by modification time, newest first
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.After(backups[j].modTime)
	})

	// Remove old backups
	for i := keep; i < len(backups); i++ {
		os.Remove(filepath.Join(p.backupDir, backups[i].name))
	}
}

// parseNftablesSections parses nftables config to identify tables and chains
func (p *Plugin) parseNftablesSections(content string) []FirewallConfigSection {
	var sections []FirewallConfigSection
	lines := strings.Split(content, "\n")

	var currentSection *FirewallConfigSection
	braceDepth := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Check for section starts
		if strings.HasPrefix(trimmed, "table ") {
			if currentSection != nil && braceDepth == 0 {
				currentSection.EndLine = lineNum - 1
				sections = append(sections, *currentSection)
			}
			// Extract table name (format: "table inet filter {")
			parts := strings.Fields(trimmed)
			name := ""
			if len(parts) >= 3 {
				name = parts[1] + " " + parts[2]
			}
			currentSection = &FirewallConfigSection{
				Type:      "table",
				Name:      strings.TrimSuffix(name, " {"),
				StartLine: lineNum,
			}
		} else if strings.HasPrefix(trimmed, "chain ") && currentSection != nil {
			// Chain within a table
			parts := strings.Fields(trimmed)
			chainName := ""
			if len(parts) >= 2 {
				chainName = strings.TrimSuffix(parts[1], " {")
			}
			chainSection := FirewallConfigSection{
				Type:      "chain",
				Name:      chainName,
				StartLine: lineNum,
			}
			// Find the end of this chain (matching brace)
			chainDepth := 0
			if strings.Contains(trimmed, "{") {
				chainDepth++
			}
			for j := i + 1; j < len(lines) && chainDepth > 0; j++ {
				chainLine := strings.TrimSpace(lines[j])
				chainDepth += strings.Count(chainLine, "{") - strings.Count(chainLine, "}")
				if chainDepth == 0 {
					chainSection.EndLine = j + 1
					break
				}
			}
			if chainSection.EndLine == 0 {
				chainSection.EndLine = lineNum
			}
			sections = append(sections, chainSection)
		}

		// Track brace depth for table boundaries
		braceDepth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")

		// Close current table section
		if currentSection != nil && braceDepth == 0 && strings.Contains(trimmed, "}") {
			currentSection.EndLine = lineNum
			sections = append(sections, *currentSection)
			currentSection = nil
		}
	}

	// Handle unclosed section
	if currentSection != nil {
		currentSection.EndLine = len(lines)
		sections = append(sections, *currentSection)
	}

	return sections
}

// Ensure plugin implements required interfaces.
var _ plugin.Plugin = (*Plugin)(nil)
