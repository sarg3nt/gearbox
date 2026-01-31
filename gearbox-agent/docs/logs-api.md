# Log Streaming API

The gearbox-agent provides endpoints for fetching logs from the server. This allows remote log viewing for HAProxy and related services.

## Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/v1/logs` | GET | API key | List available log sources |
| `/api/v1/logs/{name}` | GET | API key | Fetch logs from a specific source |

## List Log Sources

Returns the list of available log sources configured on the agent.

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/logs
```

### Response

```json
{
  "sources": [
    {
      "name": "haproxy",
      "unit": "haproxy"
    },
    {
      "name": "gearbox-agent",
      "unit": "gearbox-agent"
    },
    {
      "name": "nftables",
      "unit": "nftables"
    },
    {
      "name": "fail2ban",
      "unit": "fail2ban"
    },
    {
      "name": "system",
      "command": "journalctl -n 500 --no-pager"
    }
  ]
}
```

### Source Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique identifier for the log source |
| `unit` | string | Systemd unit name (if using journalctl) |
| `command` | string | Custom command to execute (if not a systemd unit) |

## Fetch Logs

Fetches log content from a specific source.

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  https://your-server:8405/api/v1/logs/haproxy
```

### Query Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `lines` | integer | 500 | Number of log lines to fetch (max 10000) |

### Example with Line Limit

```bash
curl -sk -H "Authorization: Bearer YOUR_API_KEY" \
  "https://your-server:8405/api/v1/logs/haproxy?lines=100"
```

### Response

```json
{
  "name": "haproxy",
  "lines": 100,
  "content": "Jan 17 12:34:56 server haproxy[1234]: Connect from...\nJan 17 12:34:57 server haproxy[1234]: ..."
}
```

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Name of the log source |
| `lines` | integer | Number of lines requested |
| `content` | string | Log content as a single string with newlines |

## Default Log Sources

The agent comes with the following default log sources:

### Core Services

| Name | Source | Description |
|------|--------|-------------|
| `haproxy` | systemd unit | HAProxy load balancer logs |
| `gearbox-agent` | systemd unit | This agent's logs |

### Security

| Name | Source | Description |
|------|--------|-------------|
| `fail2ban` | systemd unit | Fail2ban service logs |
| `fail2ban-log` | file | Fail2ban log file (`/var/log/fail2ban.log`) - ban/unban events |
| `auth` | journalctl | SSH authentication logs (successful/failed logins) |
| `ssh` | systemd unit | SSH service logs |

### Firewall

| Name | Source | Description |
|------|--------|-------------|
| `nftables` | systemd unit | nftables firewall service logs |
| `firewall` | kernel | Firewall drop/reject events from kernel log |

### System

| Name | Source | Description |
|------|--------|-------------|
| `system` | journalctl | System warnings and errors (priority warning+) |
| `kernel` | journalctl | Kernel messages (hardware, drivers, etc.) |
| `boot` | journalctl | Current boot messages (notice+ priority) |

## Error Responses

### Unknown Log Source (404)

```json
{
  "error": "Unknown log source: nonexistent"
}
```

### Command Execution Failed (500)

```json
{
  "error": "Failed to fetch logs: command failed: ..."
}
```

## JavaScript Example

```javascript
async function fetchLogs(source, lines = 500) {
  const response = await fetch(
    `https://your-server:8405/api/v1/logs/${source}?lines=${lines}`,
    {
      headers: {
        'Authorization': 'Bearer YOUR_API_KEY'
      }
    }
  );

  if (!response.ok) {
    throw new Error(`Failed to fetch logs: ${response.statusText}`);
  }

  const data = await response.json();
  return data.content;
}

// Usage
const haproxyLogs = await fetchLogs('haproxy', 100);
console.log(haproxyLogs);
```

## Go Example

```go
package main

import (
    "encoding/json"
    "fmt"
    "net/http"
)

type LogResponse struct {
    Name    string `json:"name"`
    Lines   int    `json:"lines"`
    Content string `json:"content"`
}

func fetchLogs(baseURL, apiKey, source string, lines int) (*LogResponse, error) {
    url := fmt.Sprintf("%s/api/v1/logs/%s?lines=%d", baseURL, source, lines)

    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+apiKey)

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var logResp LogResponse
    if err := json.NewDecoder(resp.Body).Decode(&logResp); err != nil {
        return nil, err
    }

    return &logResp, nil
}
```

## Security Considerations

- All log endpoints require API key authentication
- Log content may contain sensitive information (IPs, hostnames, etc.)
- Consider using separate read-only API keys for log viewing
- The agent caps log lines at 10,000 to prevent excessive memory usage
- Commands are predefined and not user-controllable to prevent injection
