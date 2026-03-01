# Services Gear

The Services gear provides monitoring and control of system services on HAProxy servers.

## Features

- Real-time service status monitoring
- Service control (start, stop, restart) for authorized users
- Support for systemd services (HAProxy, fail2ban, nftables, gearbox-agent)
- Auto-refresh with configurable interval

## Configuration

| Setting            | Type | Default | Description                     |
|--------------------|------|---------|---------------------------------|
| `auto_refresh`     | bool | true    | Enable automatic status refresh |
| `refresh_interval` | int  | 5       | Refresh interval in seconds     |

## Permissions

| Permission         | Description                             |
|--------------------|-----------------------------------------|
| `services:view`    | View service status                     |
| `services:control` | Control services (start, stop, restart) |

## Routes

| Method | Path        | Description        |
|--------|-------------|--------------------|
| GET    | `/services` | Main services page |

## API Endpoints (Main Handler)

The following API endpoints are related to services but remain in the main handler:

| Method | Path                              | Description                          |
|--------|-----------------------------------|--------------------------------------|
| GET    | `/api/{serverID}/services-config` | Get configured services list         |
| GET    | `/api/{serverID}/services`        | Get current service status           |
| POST   | `/api/{serverID}/service-control` | Control service (start/stop/restart) |

## Architecture

```txt
internal/gears/services/
├── gear.go      # Gear implementation
├── handlers.go    # HTTP handlers
├── icons.go       # Sidebar icon
├── settings.go    # Settings page component
└── README.md      # This file
```

## Development

### Building

The gear is automatically included in the build via its `init()` function.
