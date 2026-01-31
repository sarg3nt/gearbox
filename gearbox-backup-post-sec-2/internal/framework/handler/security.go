package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sarg3nt/gearbox/internal/framework/auth"
	apperrors "github.com/sarg3nt/gearbox/internal/framework/errors"
	"github.com/sarg3nt/gearbox/internal/framework/models"
	"github.com/sarg3nt/gearbox/internal/framework/templates/pages"
)

// SecurityPage serves the security dashboard page.
func (h *Handler) SecurityPage(w http.ResponseWriter, r *http.Request) {
	// Check if user has permission to view security
	if !h.authManager.HasPermission(r, models.ComponentSecurity, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view security", http.StatusForbidden)
		return
	}

	user, _ := auth.GetUserFromContext(r.Context())

	servers := h.getEnabledServers()
	if len(servers) == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Get user permissions for security actions
	perms, err := h.authManager.GetUserPermissions(r)
	if err != nil {
		h.logger.Error("Failed to get user permissions", "error", err)
		perms = &models.UserPermissions{}
	}

	// Render security template with current enabled servers and permissions
	component := pages.SecurityPage(user, servers, perms)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("Failed to render security template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// APISecuritySummaryHandler returns security summary from the agent.
func (h *Handler) APISecuritySummaryHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentSecurity, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view security", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	collector, exists := h.getCollector(serverID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Get security summary from agent
	summary, err := collector.GetSecuritySummary()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get security summary", err))
		return
	}

	h.writeJSON(w, summary)
}

// APIFail2BanStatsHandler returns fail2ban statistics from the agent.
func (h *Handler) APIFail2BanStatsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentSecurity, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view security", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	collector, exists := h.getCollector(serverID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Get fail2ban stats from agent
	stats, err := collector.GetFail2BanStats()
	if err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("get fail2ban stats", err))
		return
	}

	h.writeJSON(w, stats)
}

// APIBlockedIPsHandler returns the list of blocked IPs.
func (h *Handler) APIBlockedIPsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentSecurity, models.PermissionView) {
		http.Error(w, "Forbidden: insufficient permissions to view security", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	// Get blocked IPs from database (local records)
	blockedIPs, err := h.db.GetBlockedIPs(serverID)
	if err != nil {
		h.logger.Error("Failed to get blocked IPs from database", "error", err)
	}

	// Also get live blocked IPs from agent's nftables
	collector, exists := h.getCollector(serverID)
	if exists {
		liveBlocked, err := collector.GetBlockedIPs()
		if err != nil {
			h.logger.Error("Failed to get live blocked IPs", "error", err)
		} else {
			// Merge live data with database records
			// Live data takes precedence for real-time accuracy
			blockedMap := make(map[string]models.BlockedIP)
			for _, ip := range blockedIPs {
				blockedMap[ip.IPAddress] = ip
			}

			// Add any IPs that are blocked in nftables but not in our DB
			for _, liveIP := range liveBlocked {
				if _, exists := blockedMap[liveIP.IP]; !exists {
					blockedMap[liveIP.IP] = models.BlockedIP{
						ServerID:    serverID,
						IPAddress:   liveIP.IP,
						Reason:      "Unknown (found in nftables)",
						AutoBlocked: true,
					}
				}
			}

			// Convert back to slice
			blockedIPs = make([]models.BlockedIP, 0, len(blockedMap))
			for _, ip := range blockedMap {
				blockedIPs = append(blockedIPs, ip)
			}
		}
	}

	h.writeJSON(w, map[string]interface{}{
		"blocked_ips": blockedIPs,
		"count":       len(blockedIPs),
	})
}

// APIBlockIPHandler handles blocking an IP address.
func (h *Handler) APIBlockIPHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentSecurity, models.PermissionAction) {
		http.Error(w, "Forbidden: insufficient permissions to block IPs", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if serverID == "" {
		http.Error(w, "Server ID required", http.StatusBadRequest)
		return
	}

	var req models.BlockIPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.IPAddress == "" {
		http.Error(w, "IP address required", http.StatusBadRequest)
		return
	}

	// Get the collector to call the agent
	collector, exists := h.getCollector(serverID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Block IP via agent
	if err := collector.BlockIP(req.IPAddress, req.Reason); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("block IP", err))
		return
	}

	// Record in database
	user, _ := auth.GetUserFromContext(r.Context())
	blockedIP := models.BlockedIP{
		ServerID:    serverID,
		IPAddress:   req.IPAddress,
		Reason:      req.Reason,
		Notes:       req.Notes,
		AutoBlocked: false,
		BlockedAt:   time.Now(),
	}
	if user != nil {
		blockedIP.BlockedBy = &user.ID
	}

	if _, err := h.db.BlockIP(&blockedIP); err != nil {
		h.logger.Error("Failed to record blocked IP in database", "error", err)
		// Don't fail the request since the IP is blocked at the firewall level
	}

	h.writeJSON(w, models.BlockIPResponse{
		Success: true,
		Message: "IP blocked successfully",
		IP:      req.IPAddress,
	})
}

// APIUnblockIPHandler handles unblocking an IP address.
func (h *Handler) APIUnblockIPHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authManager.HasPermission(r, models.ComponentSecurity, models.PermissionAction) {
		http.Error(w, "Forbidden: insufficient permissions to unblock IPs", http.StatusForbidden)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	ip := chi.URLParam(r, "ip")

	if serverID == "" || ip == "" {
		http.Error(w, "Server ID and IP address required", http.StatusBadRequest)
		return
	}

	// Get the collector to call the agent
	collector, exists := h.getCollector(serverID)
	if !exists {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	// Unblock IP via agent
	if err := collector.UnblockIP(ip); err != nil {
		apperrors.WriteHTTPError(w, h.logger, apperrors.Internal("unblock IP", err))
		return
	}

	// Record unblock in database
	user, _ := auth.GetUserFromContext(r.Context())
	var userID *string
	if user != nil {
		userID = &user.ID
	}

	if err := h.db.UnblockIP(serverID, ip, userID); err != nil {
		h.logger.Error("Failed to record IP unblock in database", "error", err)
		// Don't fail the request since the IP is unblocked at the firewall level
	}

	h.writeJSON(w, models.BlockIPResponse{
		Success: true,
		Message: "IP unblocked successfully",
		IP:      ip,
	})
}
