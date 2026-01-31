# Dashboard Plugin

The Dashboard plugin provides the main monitoring overview for HAProxy servers.

This is a **core plugin** that is always enabled and cannot be disabled.

## Features

- Real-time HAProxy stats overview
- Multi-server support with server selector
- Status grid view for quick health monitoring
- Global filtering (health status, disabled items, text search)
- SSE-based real-time updates

## Routes

| Method | Path           | Description         |
|--------|----------------|---------------------|
| GET    | `/`            | Main dashboard page |
| GET    | `/status-grid` | Status grid view    |

## Related Routes (Main Handler)

The following routes are related to the dashboard but remain in the main handler
because they require collector access:

| Method | Path                                 | Description                      |
|--------|--------------------------------------|----------------------------------|
| GET    | `/htmx/{serverID}/stats`             | HTMX partial for stats refresh   |
| GET    | `/htmx/{serverID}/metrics`           | HTMX partial for metrics refresh |
| GET    | `/server/{serverID}/frontend/{name}` | Frontend detail page             |
| GET    | `/server/{serverID}/backend/{name}`  | Backend detail page              |

## Permissions

No special permissions required - accessible to all authenticated users.

## Architecture

```txt
internal/plugins/dashboard/
├── plugin.go      # Plugin implementation
├── handlers.go    # HTTP handlers
├── icons.go       # Sidebar icon
└── README.md      # This file
```

## Development

### Building

The plugin is automatically included in the build via its `init()` function.
