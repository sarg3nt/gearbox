# Metrics Gear

The Metrics gear provides historical metrics visualization and analysis for HAProxy monitoring.

## Features

- Historical stats graphs
- System metrics over time (CPU, memory, disk, network)
- Backend performance trends
- Configurable data retention
- Time range selection for analysis

## Configuration

| Setting          | Type | Default | Description                    |
|------------------|------|---------|--------------------------------|
| `store_history`  | bool | true    | Enable historical data storage |
| `retention_days` | int  | 7       | Data retention period in days  |

## Permissions

| Permission          | Description                |
|---------------------|----------------------------|
| `metrics:view`      | View metrics and history   |
| `metrics:configure` | Configure metrics settings |

## Routes

| Method | Path       | Description               |
|--------|------------|---------------------------|
| GET    | `/history` | Main history/metrics page |

## API Endpoints (Main Handler)

The following API endpoints are related to metrics but remain in the main handler:

| Method | Path                                            | Description                   |
|--------|-------------------------------------------------|-------------------------------|
| GET    | `/api/{serverID}/history/stats`                 | Get historical HAProxy stats  |
| GET    | `/api/{serverID}/history/metrics`               | Get historical system metrics |
| GET    | `/api/{serverID}/history/backend/{backendName}` | Get backend-specific history  |
| GET    | `/api/{serverID}/metrics/storage-stats`         | Get storage statistics        |
| POST   | `/api/{serverID}/metrics/clear`                 | Clear metrics data            |

## Architecture

```txt
internal/gears/metrics/
├── gear.go      # Gear implementation
├── handlers.go    # HTTP handlers
├── icons.go       # Sidebar icon
├── settings.go    # Settings page component
└── README.md      # This file
```

## Development

### Building

The gear is automatically included in the build via its `init()` function.
