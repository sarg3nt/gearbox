package security

import (
	"bufio"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Pre-compiled regex patterns for firewall log parsing.
var (
	counterRegex = regexp.MustCompile(`counter packets (\d+) bytes (\d+)`)
	commentRegex = regexp.MustCompile(`comment "([^"]+)"`)
	handleRegex  = regexp.MustCompile(`# handle (\d+)`)
	timeRegex    = regexp.MustCompile(`^(\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})`)
	prefixRegex  = regexp.MustCompile(`\[([^\]]+)\]`)
	inRegex      = regexp.MustCompile(`IN=(\S*)`)
	srcRegex     = regexp.MustCompile(`SRC=(\S+)`)
	dstRegex     = regexp.MustCompile(`DST=(\S+)`)
	protoRegex   = regexp.MustCompile(`PROTO=(\S+)`)
	sptRegex     = regexp.MustCompile(`SPT=(\d+)`)
	dptRegex     = regexp.MustCompile(`DPT=(\d+)`)
)

// FirewallStats represents overall firewall statistics.
type FirewallStats struct {
	Available     bool              `json:"available"`   // Whether nftables is installed
	Running       bool              `json:"running"`     // Whether nftables service is active
	Tables        []TableStats      `json:"tables,omitempty"`
	RecentBlocks  []BlockEvent      `json:"recent_blocks,omitempty"`
	BlockCounts   map[string]int    `json:"block_counts,omitempty"` // By chain/rule
	CollectedAt   time.Time         `json:"collected_at"`
}

// TableStats represents statistics for an nftables table.
type TableStats struct {
	Family string       `json:"family"` // inet, ip, ip6
	Name   string       `json:"name"`
	Chains []ChainStats `json:"chains"`
}

// ChainStats represents statistics for an nftables chain.
type ChainStats struct {
	Name    string      `json:"name"`
	Type    string      `json:"type,omitempty"`    // filter, nat, route
	Hook    string      `json:"hook,omitempty"`    // input, output, forward, etc.
	Policy  string      `json:"policy,omitempty"`  // accept, drop
	Packets int64       `json:"packets"`
	Bytes   int64       `json:"bytes"`
	Rules   []RuleStats `json:"rules,omitempty"`
}

// RuleStats represents statistics for a single rule.
type RuleStats struct {
	Handle  int    `json:"handle,omitempty"`
	Packets int64  `json:"packets"`
	Bytes   int64  `json:"bytes"`
	Comment string `json:"comment,omitempty"`
	Rule    string `json:"rule"` // Rule expression
}

// BlockEvent represents a single firewall block event from logs.
type BlockEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Chain     string    `json:"chain,omitempty"`
	Action    string    `json:"action"` // DROP, REJECT
	Protocol  string    `json:"protocol,omitempty"`
	SrcIP     string    `json:"src_ip,omitempty"`
	DstIP     string    `json:"dst_ip,omitempty"`
	SrcPort   int       `json:"src_port,omitempty"`
	DstPort   int       `json:"dst_port,omitempty"`
	Interface string    `json:"interface,omitempty"`
}

// FirewallCollector collects firewall statistics.
type FirewallCollector struct{}

// NewFirewallCollector creates a new firewall collector.
func NewFirewallCollector() *FirewallCollector {
	return &FirewallCollector{}
}

// IsInstalled checks if nftables is installed on the system.
func (c *FirewallCollector) IsInstalled() bool {
	_, err := exec.LookPath("nft")
	return err == nil
}

// Collect gathers firewall statistics.
// Returns ErrServiceNotInstalled if nftables is not installed.
func (c *FirewallCollector) Collect(includeRules bool, recentCount int) (*FirewallStats, error) {
	stats := &FirewallStats{
		CollectedAt:  time.Now(),
		Tables:       make([]TableStats, 0),
		BlockCounts:  make(map[string]int),
	}

	// Check if nftables is installed
	if !c.IsInstalled() {
		stats.Available = false
		stats.Running = false
		return stats, ErrServiceNotInstalled
	}
	stats.Available = true

	// Check if nftables is running
	if err := exec.Command("systemctl", "is-active", "--quiet", "nftables").Run(); err != nil {
		stats.Running = false
		return stats, nil
	}
	stats.Running = true

	// Get table/chain statistics with counters
	tables, err := c.getTableStats(includeRules)
	if err == nil {
		stats.Tables = tables
	}

	// Get recent block events if requested
	if recentCount > 0 {
		stats.RecentBlocks = c.getRecentBlocks(recentCount)

		// Calculate block counts by chain
		for _, block := range stats.RecentBlocks {
			key := block.Chain
			if key == "" {
				key = "unknown"
			}
			stats.BlockCounts[key]++
		}
	}

	return stats, nil
}

// getTableStats retrieves nftables statistics.
func (c *FirewallCollector) getTableStats(includeRules bool) ([]TableStats, error) {
	// Use text output which is more reliable across nftables versions
	return c.getTableStatsText(includeRules)
}

// getTableStatsText parses nftables text output.
func (c *FirewallCollector) getTableStatsText(includeRules bool) ([]TableStats, error) {
	tables := make([]TableStats, 0)

	// Get ruleset with counters
	args := []string{"list", "ruleset"}
	if !includeRules {
		// Just get chain stats without individual rules
		args = []string{"list", "chains"}
	}

	cmd := exec.Command("nft", args...)
	output, err := cmd.Output()
	if err != nil {
		return tables, err
	}

	// Parse output
	var currentTable *TableStats
	var currentChain *ChainStats

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Match table declaration: "table inet filter {"
		if strings.HasPrefix(line, "table ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				currentTable = &TableStats{
					Family: parts[1],
					Name:   strings.TrimSuffix(parts[2], "{"),
					Chains: make([]ChainStats, 0),
				}
				tables = append(tables, *currentTable)
			}
			continue
		}

		// Match chain declaration: "chain input {"
		if strings.HasPrefix(line, "chain ") && currentTable != nil {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				currentChain = &ChainStats{
					Name:  strings.TrimSuffix(parts[1], "{"),
					Rules: make([]RuleStats, 0),
				}
			}
			continue
		}

		// Match chain type/hook/policy: "type filter hook input priority filter; policy drop;"
		if strings.HasPrefix(line, "type ") && currentChain != nil {
			c.parseChainType(line, currentChain)
			continue
		}

		// Match counter in rules if includeRules
		if includeRules && currentChain != nil && strings.Contains(line, "counter") {
			rule := c.parseRuleLine(line)
			if rule.Rule != "" {
				currentChain.Rules = append(currentChain.Rules, rule)
			}
		}

		// End of chain
		if line == "}" && currentChain != nil {
			if currentTable != nil && len(tables) > 0 {
				tables[len(tables)-1].Chains = append(tables[len(tables)-1].Chains, *currentChain)
			}
			currentChain = nil
		}
	}

	return tables, nil
}

// parseChainType parses chain type/hook/policy line.
func (c *FirewallCollector) parseChainType(line string, chain *ChainStats) {
	// "type filter hook input priority filter; policy drop;"
	if strings.Contains(line, "type ") {
		parts := strings.Fields(line)
		for i, p := range parts {
			switch p {
			case "type":
				if i+1 < len(parts) {
					chain.Type = parts[i+1]
				}
			case "hook":
				if i+1 < len(parts) {
					chain.Hook = parts[i+1]
				}
			case "policy":
				if i+1 < len(parts) {
					chain.Policy = strings.TrimSuffix(parts[i+1], ";")
				}
			}
		}
	}
}

// parseRuleLine parses a rule line with counter.
func (c *FirewallCollector) parseRuleLine(line string) RuleStats {
	rule := RuleStats{Rule: line}

	// Parse counter: "counter packets 12345 bytes 6789012"
	matches := counterRegex.FindStringSubmatch(line)
	if len(matches) == 3 {
		rule.Packets, _ = strconv.ParseInt(matches[1], 10, 64)
		rule.Bytes, _ = strconv.ParseInt(matches[2], 10, 64)
	}

	// Parse comment: "comment \"some comment\""
	commentMatches := commentRegex.FindStringSubmatch(line)
	if len(commentMatches) == 2 {
		rule.Comment = commentMatches[1]
	}

	// Parse handle: "# handle 5"
	handleMatches := handleRegex.FindStringSubmatch(line)
	if len(handleMatches) == 2 {
		rule.Handle, _ = strconv.Atoi(handleMatches[1])
	}

	return rule
}

// getRecentBlocks parses kernel log for recent firewall blocks.
func (c *FirewallCollector) getRecentBlocks(count int) []BlockEvent {
	events := make([]BlockEvent, 0)

	// Get kernel messages directly from journalctl (no shell)
	// We fetch more lines than needed and filter in Go to avoid shell pipes
	cmd := exec.Command("journalctl", "-k", "--no-pager", "-n", "1000")
	output, err := cmd.Output()
	if err != nil {
		return events
	}

	// Parse kernel log entries and filter for firewall-related lines
	// Format varies, but common patterns:
	// Jan 17 10:30:45 server kernel: [nft_drop] IN=eth0 OUT= SRC=192.168.1.100 DST=10.0.0.1 ...
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		upperLine := strings.ToUpper(line)

		// Filter for firewall-related entries (replaces grep)
		if !strings.Contains(upperLine, "DROP") &&
			!strings.Contains(upperLine, "REJECT") &&
			!strings.Contains(upperLine, "NFT_") &&
			!(strings.Contains(line, "IN=") && strings.Contains(line, "OUT=")) {
			continue
		}

		event := c.parseBlockLine(line)
		if event.SrcIP != "" || event.Action != "" {
			events = append(events, event)
			// Stop once we have enough events (replaces tail)
			if len(events) >= count {
				break
			}
		}
	}

	return events
}

// parseBlockLine parses a kernel firewall log line.
func (c *FirewallCollector) parseBlockLine(line string) BlockEvent {
	event := BlockEvent{
		Timestamp: time.Now(), // Default to now if we can't parse
	}

	// Try to parse timestamp from journalctl output
	// Format: "Jan 17 10:30:45 server kernel: ..."
	timeMatches := timeRegex.FindStringSubmatch(line)
	if len(timeMatches) == 2 {
		// Parse with current year
		year := time.Now().Year()
		t, err := time.Parse("Jan 2 15:04:05 2006", timeMatches[1]+" "+strconv.Itoa(year))
		if err == nil {
			event.Timestamp = t
		}
	}

	// Determine action
	if strings.Contains(strings.ToUpper(line), "DROP") {
		event.Action = "DROP"
	} else if strings.Contains(strings.ToUpper(line), "REJECT") {
		event.Action = "REJECT"
	}

	// Parse chain/prefix (often in brackets)
	prefixMatches := prefixRegex.FindStringSubmatch(line)
	if len(prefixMatches) == 2 {
		event.Chain = prefixMatches[1]
	}

	// Parse IN= interface
	inMatches := inRegex.FindStringSubmatch(line)
	if len(inMatches) == 2 && inMatches[1] != "" {
		event.Interface = inMatches[1]
	}

	// Parse SRC= IP
	srcMatches := srcRegex.FindStringSubmatch(line)
	if len(srcMatches) == 2 {
		event.SrcIP = srcMatches[1]
	}

	// Parse DST= IP
	dstMatches := dstRegex.FindStringSubmatch(line)
	if len(dstMatches) == 2 {
		event.DstIP = dstMatches[1]
	}

	// Parse protocol
	protoMatches := protoRegex.FindStringSubmatch(line)
	if len(protoMatches) == 2 {
		event.Protocol = protoMatches[1]
	}

	// Parse SPT= (source port)
	sptMatches := sptRegex.FindStringSubmatch(line)
	if len(sptMatches) == 2 {
		event.SrcPort, _ = strconv.Atoi(sptMatches[1])
	}

	// Parse DPT= (destination port)
	dptMatches := dptRegex.FindStringSubmatch(line)
	if len(dptMatches) == 2 {
		event.DstPort, _ = strconv.Atoi(dptMatches[1])
	}

	return event
}
