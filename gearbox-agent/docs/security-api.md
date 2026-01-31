# Security API

The gearbox-agent provides endpoints for monitoring security services including fail2ban and the firewall (nftables). These endpoints provide structured data about bans, blocks, and security events.

## Service Availability

These endpoints gracefully handle systems where fail2ban or nftables are not installed:

- Returns HTTP 200 with `available: false` when a service is not installed
- Returns HTTP 200 with `available: true, running: false` when installed but not running
- Returns HTTP 200 with `available: true, running: true` when fully operational
- Never returns errors for missing services - the response indicates the state

This allows the monitoring app to display appropriate status without error handling.

## Rate Limiting

All API endpoints are rate limited to prevent abuse:

- **Rate:** 50 requests per second per IP
- **Burst:** 100 requests (allows short spikes)
- **Response:** HTTP 429 Too Many Requests when exceeded
- **Header:** `Retry-After: 1` included in 429 responses

## Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/security/summary` | GET | API key | Quick overview of security status |
| `/api/v1/security/fail2ban` | GET | API key | Detailed fail2ban statistics |
| `/api/v1/security/firewall` | GET | API key | Detailed firewall statistics |

## Security Summary

Returns a quick overview of security status for dashboards.

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/security/summary
```

### Response

```json
{
  "fail2ban": {
    "available": true,
    "running": true,
    "total_banned": 5,
    "jail_count": 2
  },
  "firewall": {
    "available": true,
    "running": true,
    "recent_blocks": 23
  }
}
```

### Response (Service Not Installed)

```json
{
  "fail2ban": {
    "available": false,
    "running": false,
    "total_banned": 0,
    "jail_count": 0
  },
  "firewall": {
    "available": true,
    "running": true,
    "recent_blocks": 10
  }
}
```

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `fail2ban.available` | boolean | Whether fail2ban is installed on the system |
| `fail2ban.running` | boolean | Whether fail2ban service is active |
| `fail2ban.total_banned` | integer | Total IPs currently banned across all jails |
| `fail2ban.jail_count` | integer | Number of configured jails |
| `firewall.available` | boolean | Whether nftables is installed on the system |
| `firewall.running` | boolean | Whether nftables service is active |
| `firewall.recent_blocks` | integer | Number of recent firewall block events |

## Fail2Ban Statistics

Returns detailed fail2ban statistics including per-jail information.

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/security/fail2ban
```

### Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `include_ips` | boolean | false | Include list of currently banned IPs |
| `recent` | integer | 0 | Number of recent ban/unban events to include (max 100) |

### Example with All Options

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  "https://your-server:8405/api/v1/security/fail2ban?include_ips=true&recent=20"
```

### Response

```json
{
  "running": true,
  "jails": [
    {
      "name": "sshd",
      "currently_banned": 2,
      "total_banned": 15,
      "currently_failed": 3,
      "total_failed": 150,
      "banned_ips": ["192.168.1.100", "10.0.0.50"]
    },
    {
      "name": "haproxy-http",
      "currently_banned": 3,
      "total_banned": 8,
      "currently_failed": 0,
      "total_failed": 25,
      "banned_ips": ["203.0.113.50", "198.51.100.20", "192.0.2.100"]
    }
  ],
  "total_banned": 5,
  "recent_bans": [
    {
      "timestamp": "2024-01-17T10:30:45Z",
      "jail": "sshd",
      "ip": "192.168.1.100",
      "action": "ban"
    },
    {
      "timestamp": "2024-01-17T10:35:45Z",
      "jail": "sshd",
      "ip": "10.0.0.50",
      "action": "ban"
    }
  ],
  "collected_at": "2024-01-17T12:00:00Z"
}
```

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `running` | boolean | Whether fail2ban service is active |
| `jails` | array | Per-jail statistics |
| `jails[].name` | string | Jail name (e.g., "sshd", "haproxy-http") |
| `jails[].currently_banned` | integer | Number of IPs currently banned in this jail |
| `jails[].total_banned` | integer | Total bans since service start |
| `jails[].currently_failed` | integer | Current failed attempts being tracked |
| `jails[].total_failed` | integer | Total failed attempts seen |
| `jails[].banned_ips` | array | List of banned IPs (only if `include_ips=true`) |
| `total_banned` | integer | Sum of currently banned across all jails |
| `recent_bans` | array | Recent ban/unban events (only if `recent` > 0) |
| `recent_bans[].timestamp` | string | ISO 8601 timestamp of event |
| `recent_bans[].jail` | string | Jail that triggered the event |
| `recent_bans[].ip` | string | IP address banned/unbanned |
| `recent_bans[].action` | string | "ban" or "unban" |
| `collected_at` | string | ISO 8601 timestamp when data was collected |

## Firewall Statistics

Returns nftables firewall statistics and recent block events.

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/security/firewall
```

### Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `include_rules` | boolean | false | Include individual rule counters |
| `recent` | integer | 0 | Number of recent block events to include (max 100) |

### Example with Recent Blocks

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  "https://your-server:8405/api/v1/security/firewall?recent=50"
```

### Response

```json
{
  "running": true,
  "tables": [
    {
      "family": "inet",
      "name": "filter",
      "chains": [
        {
          "name": "input",
          "type": "filter",
          "hook": "input",
          "policy": "drop",
          "packets": 12345,
          "bytes": 6789012
        },
        {
          "name": "forward",
          "type": "filter",
          "hook": "forward",
          "policy": "drop",
          "packets": 0,
          "bytes": 0
        }
      ]
    }
  ],
  "recent_blocks": [
    {
      "timestamp": "2024-01-17T10:30:45Z",
      "chain": "nft_drop",
      "action": "DROP",
      "protocol": "TCP",
      "src_ip": "192.168.1.100",
      "dst_ip": "10.0.0.1",
      "src_port": 54321,
      "dst_port": 22,
      "interface": "eth0"
    }
  ],
  "block_counts": {
    "nft_drop": 15,
    "rate_limit": 8
  },
  "collected_at": "2024-01-17T12:00:00Z"
}
```

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `running` | boolean | Whether nftables service is active |
| `tables` | array | nftables table information |
| `tables[].family` | string | Address family (inet, ip, ip6) |
| `tables[].name` | string | Table name |
| `tables[].chains` | array | Chain information within the table |
| `tables[].chains[].name` | string | Chain name |
| `tables[].chains[].type` | string | Chain type (filter, nat, route) |
| `tables[].chains[].hook` | string | Hook point (input, output, forward, etc.) |
| `tables[].chains[].policy` | string | Default policy (accept, drop) |
| `tables[].chains[].packets` | integer | Total packets through chain |
| `tables[].chains[].bytes` | integer | Total bytes through chain |
| `recent_blocks` | array | Recent firewall block events from kernel log |
| `recent_blocks[].timestamp` | string | ISO 8601 timestamp |
| `recent_blocks[].chain` | string | Chain or log prefix that triggered |
| `recent_blocks[].action` | string | DROP or REJECT |
| `recent_blocks[].protocol` | string | Protocol (TCP, UDP, ICMP, etc.) |
| `recent_blocks[].src_ip` | string | Source IP address |
| `recent_blocks[].dst_ip` | string | Destination IP address |
| `recent_blocks[].src_port` | integer | Source port (if applicable) |
| `recent_blocks[].dst_port` | integer | Destination port (if applicable) |
| `recent_blocks[].interface` | string | Incoming interface |
| `block_counts` | object | Count of blocks by chain/prefix |
| `collected_at` | string | ISO 8601 timestamp |

### Response with Rule Counters

When `include_rules=true`, each chain includes rule-level statistics:

```json
{
  "chains": [
    {
      "name": "input",
      "rules": [
        {
          "handle": 5,
          "packets": 1000,
          "bytes": 50000,
          "comment": "allow established",
          "rule": "ct state established,related counter packets 1000 bytes 50000 accept"
        },
        {
          "handle": 8,
          "packets": 50,
          "bytes": 2500,
          "comment": "rate limit",
          "rule": "tcp dport 443 meter ratelimit { ip saddr limit rate 100/second } counter packets 50 bytes 2500 accept"
        }
      ]
    }
  ]
}
```

## JavaScript Example

```javascript
async function getSecuritySummary(baseUrl, apiKey) {
  const response = await fetch(`${baseUrl}/api/v1/security/summary`, {
    headers: { 'Authorization': `Bearer ${apiKey}` }
  });

  if (!response.ok) {
    throw new Error(`Failed: ${response.statusText}`);
  }

  return response.json();
}

async function getFail2BanDetails(baseUrl, apiKey) {
  const response = await fetch(
    `${baseUrl}/api/v1/security/fail2ban?include_ips=true&recent=10`,
    { headers: { 'Authorization': `Bearer ${apiKey}` } }
  );

  const stats = await response.json();

  console.log(`Fail2ban: ${stats.running ? 'Running' : 'Stopped'}`);
  console.log(`Total banned: ${stats.total_banned}`);

  for (const jail of stats.jails) {
    console.log(`  ${jail.name}: ${jail.currently_banned} banned`);
  }

  return stats;
}

async function getFirewallBlocks(baseUrl, apiKey) {
  const response = await fetch(
    `${baseUrl}/api/v1/security/firewall?recent=20`,
    { headers: { 'Authorization': `Bearer ${apiKey}` } }
  );

  const stats = await response.json();

  console.log(`Firewall: ${stats.running ? 'Running' : 'Stopped'}`);
  console.log(`Recent blocks: ${stats.recent_blocks.length}`);

  // Group blocks by source IP
  const byIP = {};
  for (const block of stats.recent_blocks) {
    byIP[block.src_ip] = (byIP[block.src_ip] || 0) + 1;
  }

  console.log('Blocks by source IP:', byIP);

  return stats;
}
```

## Use Cases

### Dashboard Metrics

Use the summary endpoint for real-time dashboard widgets:

```javascript
// Poll every 30 seconds for dashboard
setInterval(async () => {
  const summary = await getSecuritySummary(baseUrl, apiKey);

  updateWidget('fail2ban-banned', summary.fail2ban.total_banned);
  updateWidget('firewall-blocks', summary.firewall.recent_blocks);
}, 30000);
```

### Security Alerts

Monitor for new bans and blocks:

```javascript
async function checkSecurityEvents() {
  const f2b = await fetch(`${baseUrl}/api/v1/security/fail2ban?recent=5`);
  const stats = await f2b.json();

  for (const event of stats.recent_bans) {
    if (event.action === 'ban') {
      console.log(`ALERT: ${event.ip} banned by ${event.jail}`);
    }
  }
}
```

### Attack Analysis

Get detailed firewall block information for investigation:

```javascript
async function analyzeAttacks() {
  const response = await fetch(
    `${baseUrl}/api/v1/security/firewall?recent=100`,
    { headers: { 'Authorization': `Bearer ${apiKey}` } }
  );

  const stats = await response.json();

  // Find top attackers
  const attackers = {};
  for (const block of stats.recent_blocks) {
    attackers[block.src_ip] = (attackers[block.src_ip] || 0) + 1;
  }

  // Sort by count
  const sorted = Object.entries(attackers)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 10);

  console.log('Top 10 blocked IPs:', sorted);
}
```

## Security Considerations

- All security endpoints require API key authentication
- IP addresses of attackers are exposed - treat this data as sensitive
- Rate limit your monitoring queries to avoid overwhelming the agent
- Consider using these endpoints for alerting, not just display
- The `recent` parameter caps at 100 events to prevent memory issues
