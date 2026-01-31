# Certificates Plugin

The Certificates plugin provides SSL/TLS certificate monitoring and management for HAProxy servers.

## Features

- Certificate expiration monitoring
- Warning and critical thresholds
- Certbot integration for renewal
- Certificate download functionality
- Multi-domain support

## Configuration

| Setting         | Type | Default | Description                                    |
|-----------------|------|---------|------------------------------------------------|
| `warning_days`  | int  | 30      | Show warning for certs expiring within N days  |
| `critical_days` | int  | 7       | Show critical for certs expiring within N days |

## Permissions

| Permission              | Description                    |
|-------------------------|--------------------------------|
| `certificates:view`     | View certificate status        |
| `certificates:action`   | Renew certificates via certbot |
| `certificates:download` | Download certificate files     |

## Routes

| Method | Path            | Description            |
|--------|-----------------|------------------------|
| GET    | `/certificates` | Main certificates page |

## API Endpoints (Main Handler)

The following API endpoints are related to certificates but remain in the main handler:

| Method | Path                                             | Description          |
|--------|--------------------------------------------------|----------------------|
| GET    | `/api/{serverID}/certificates`                   | Get all certificates |
| POST   | `/api/{serverID}/certificates/{domain}/refresh`  | Renew certificate    |
| GET    | `/api/{serverID}/certificates/{domain}/download` | Download certificate |

## Architecture

```txt
internal/plugins/certificates/
├── plugin.go      # Plugin implementation
├── handlers.go    # HTTP handlers
├── icons.go       # Sidebar icon
├── settings.go    # Settings page component
└── README.md      # This file
```

## Development

### Building

The plugin is automatically included in the build via its `init()` function.
