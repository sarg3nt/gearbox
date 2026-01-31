// Package security provides security-related data collection.
package security

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrServiceNotInstalled indicates the service is not installed on this system.
var ErrServiceNotInstalled = errors.New("service not installed")

// Pre-compiled regex for fail2ban log parsing.
var banRegex = regexp.MustCompile(`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}).*\[(\w+)\] (Ban|Unban) (\S+)`)

// Fail2BanStats represents overall fail2ban statistics.
type Fail2BanStats struct {
	Available   bool          `json:"available"`   // Whether fail2ban is installed
	Running     bool          `json:"running"`     // Whether fail2ban service is active
	Jails       []JailStats   `json:"jails"`
	TotalBanned int           `json:"total_banned"`
	RecentBans  []BanEvent    `json:"recent_bans,omitempty"`
	CollectedAt time.Time     `json:"collected_at"`
}

// JailStats represents statistics for a single fail2ban jail.
type JailStats struct {
	Name          string   `json:"name"`
	CurrentlyBanned int    `json:"currently_banned"`
	TotalBanned   int      `json:"total_banned"`
	CurrentlyFailed int    `json:"currently_failed"`
	TotalFailed   int      `json:"total_failed"`
	BannedIPs     []string `json:"banned_ips,omitempty"`
}

// BanEvent represents a single ban/unban event.
type BanEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Jail      string    `json:"jail"`
	IP        string    `json:"ip"`
	Action    string    `json:"action"` // "ban" or "unban"
}

// Fail2BanCollector collects fail2ban statistics.
type Fail2BanCollector struct{}

// NewFail2BanCollector creates a new fail2ban collector.
func NewFail2BanCollector() *Fail2BanCollector {
	return &Fail2BanCollector{}
}

// IsInstalled checks if fail2ban is installed on the system.
func (c *Fail2BanCollector) IsInstalled() bool {
	_, err := exec.LookPath("fail2ban-client")
	return err == nil
}

// Collect gathers fail2ban statistics.
// Returns ErrServiceNotInstalled if fail2ban is not installed.
func (c *Fail2BanCollector) Collect(includeIPs bool, recentCount int) (*Fail2BanStats, error) {
	stats := &Fail2BanStats{
		CollectedAt: time.Now(),
		Jails:       make([]JailStats, 0),
	}

	// Check if fail2ban is installed
	if !c.IsInstalled() {
		stats.Available = false
		stats.Running = false
		return stats, ErrServiceNotInstalled
	}
	stats.Available = true

	// Check if fail2ban is running
	if err := exec.Command("systemctl", "is-active", "--quiet", "fail2ban").Run(); err != nil {
		stats.Running = false
		return stats, nil
	}
	stats.Running = true

	// Get list of jails
	jails, err := c.getJails()
	if err != nil {
		return stats, fmt.Errorf("failed to get jails: %w", err)
	}

	// Get stats for each jail
	for _, jailName := range jails {
		jailStats, err := c.getJailStats(jailName, includeIPs)
		if err != nil {
			continue // Skip jails we can't read
		}
		stats.Jails = append(stats.Jails, jailStats)
		stats.TotalBanned += jailStats.CurrentlyBanned
	}

	// Get recent ban events if requested
	if recentCount > 0 {
		stats.RecentBans = c.getRecentBans(recentCount)
	}

	return stats, nil
}

// getJails returns the list of configured jails.
func (c *Fail2BanCollector) getJails() ([]string, error) {
	cmd := exec.Command("fail2ban-client", "status")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// Parse output like:
	// Status
	// |- Number of jail:      2
	// `- Jail list:   sshd, haproxy-http
	jails := make([]string, 0)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Jail list:") {
			// Extract jail names after the colon
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				jailList := strings.TrimSpace(parts[1])
				for _, jail := range strings.Split(jailList, ",") {
					jail = strings.TrimSpace(jail)
					if jail != "" {
						jails = append(jails, jail)
					}
				}
			}
		}
	}

	return jails, nil
}

// getJailStats returns statistics for a specific jail.
func (c *Fail2BanCollector) getJailStats(jailName string, includeIPs bool) (JailStats, error) {
	stats := JailStats{Name: jailName}

	cmd := exec.Command("fail2ban-client", "status", jailName)
	output, err := cmd.Output()
	if err != nil {
		return stats, err
	}

	// Parse output like:
	// Status for the jail: sshd
	// |- Filter
	// |  |- Currently failed: 0
	// |  |- Total failed:     15
	// |  `- File list:        /var/log/auth.log
	// `- Actions
	//    |- Currently banned: 2
	//    |- Total banned:     5
	//    `- Banned IP list:   192.168.1.100 10.0.0.50
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Currently failed:") {
			stats.CurrentlyFailed = parseIntFromLine(line)
		} else if strings.Contains(line, "Total failed:") {
			stats.TotalFailed = parseIntFromLine(line)
		} else if strings.Contains(line, "Currently banned:") {
			stats.CurrentlyBanned = parseIntFromLine(line)
		} else if strings.Contains(line, "Total banned:") {
			stats.TotalBanned = parseIntFromLine(line)
		} else if includeIPs && strings.Contains(line, "Banned IP list:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				ipList := strings.TrimSpace(parts[1])
				if ipList != "" {
					stats.BannedIPs = strings.Fields(ipList)
				}
			}
		}
	}

	return stats, nil
}

// parseIntFromLine extracts an integer from a line like "Currently banned: 5"
func parseIntFromLine(line string) int {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return 0
	}
	val, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	return val
}

// getRecentBans parses fail2ban log for recent ban events.
func (c *Fail2BanCollector) getRecentBans(count int) []BanEvent {
	events := make([]BanEvent, 0)

	// Read fail2ban log file directly (no shell)
	file, err := os.Open("/var/log/fail2ban.log")
	if err != nil {
		return events
	}
	defer file.Close()

	// Read all lines and filter for Ban/Unban events
	// We collect matching lines, then take the last 'count' entries
	var allEvents []BanEvent

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Filter for Ban/Unban lines (replaces grep)
		if !strings.Contains(line, "Ban") && !strings.Contains(line, "Unban") {
			continue
		}

		// Parse lines like:
		// 2024-01-17 10:30:45,123 fail2ban.actions [1234]: NOTICE [sshd] Ban 192.168.1.100
		// 2024-01-17 10:35:45,456 fail2ban.actions [1234]: NOTICE [sshd] Unban 192.168.1.100
		matches := banRegex.FindStringSubmatch(line)
		if len(matches) == 5 {
			timestamp, _ := time.Parse("2006-01-02 15:04:05", matches[1])
			allEvents = append(allEvents, BanEvent{
				Timestamp: timestamp,
				Jail:      matches[2],
				Action:    strings.ToLower(matches[3]),
				IP:        matches[4],
			})
		}
	}

	// Take the last 'count' events (replaces tail)
	if len(allEvents) <= count {
		return allEvents
	}
	return allEvents[len(allEvents)-count:]
}
