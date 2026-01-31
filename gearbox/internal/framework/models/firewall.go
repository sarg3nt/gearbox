package models

import (
	"fmt"
	"time"
)

// BlockedIP represents an IP that has been blocked via the firewall
type BlockedIP struct {
	ID                int64      `json:"id"`
	ServerID          string     `json:"server_id"`
	IPAddress         string     `json:"ip_address"`
	Reason            string     `json:"reason"`
	BlockedAt         time.Time  `json:"blocked_at"`
	BlockedBy         *string    `json:"blocked_by,omitempty"` // UUID
	BlockedByUsername string     `json:"blocked_by_username,omitempty"`
	UnblockedAt       *time.Time `json:"unblocked_at,omitempty"`
	UnblockedBy       *string    `json:"unblocked_by,omitempty"` // UUID
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`   // nil = permanent
	AutoBlocked       bool       `json:"auto_blocked"`           // true if blocked by automation
	Country           string     `json:"country,omitempty"`
	CountryCode       string     `json:"country_code,omitempty"`
	ThreatScore       int        `json:"threat_score,omitempty"`
	Notes             string     `json:"notes,omitempty"`
}

// BlockIPRequest represents a request to block an IP
type BlockIPRequest struct {
	IPAddress   string `json:"ip_address"`
	Reason      string `json:"reason"`
	Duration    int    `json:"duration,omitempty"` // minutes, 0 = permanent
	Notes       string `json:"notes,omitempty"`
}

// UnblockIPRequest represents a request to unblock an IP
type UnblockIPRequest struct {
	IPAddress string `json:"ip_address"`
}

// BlockIPResponse represents the response from blocking an IP
type BlockIPResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	IP      string `json:"ip"`
}

// IsBlocked returns true if the IP is currently blocked
func (b *BlockedIP) IsBlocked() bool {
	if b.UnblockedAt != nil {
		return false
	}
	if b.ExpiresAt != nil && time.Now().After(*b.ExpiresAt) {
		return false
	}
	return true
}

// TimeRemaining returns the duration until the block expires
func (b *BlockedIP) TimeRemaining() string {
	if b.ExpiresAt == nil {
		return "Permanent"
	}
	if time.Now().After(*b.ExpiresAt) {
		return "Expired"
	}
	remaining := time.Until(*b.ExpiresAt)
	if remaining < time.Hour {
		return remaining.Round(time.Minute).String()
	}
	hours := int(remaining.Hours())
	if hours < 24 {
		return remaining.Round(time.Hour).String()
	}
	days := hours / 24
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
