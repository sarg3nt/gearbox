# Traffic Plugin

The Traffic plugin provides traffic analysis and visualization for HAProxy servers.

## Features

- Traffic source analysis
- GeoIP enrichment for geographic data
- Network visualization diagrams
- Top sources ranking
- Backend traffic distribution

## Configuration

| Setting             | Type | Default | Description                      |
|---------------------|------|---------|----------------------------------|
| `enable_geoip`      | bool | true    | Enable GeoIP location enrichment |
| `top_sources_count` | int  | 10      | Number of top sources to display |

## Permissions

Uses the `metrics:view` permission - no additional permissions required.

## Routes

| Method | Path       | Description                |
|--------|------------|----------------------------|
| GET    | `/traffic` | Main traffic analysis page |

## API Endpoints (Main Handler)

The following API endpoints are related to traffic but remain in the main handler:

| Method | Path                              | Description                        |
|--------|-----------------------------------|------------------------------------|
| GET    | `/api/{serverID}/traffic`         | Get comprehensive traffic analysis |
| GET    | `/api/{serverID}/traffic/sources` | Get top traffic sources            |
| GET    | `/api/{serverID}/traffic/network` | Get network visualization data     |

## Architecture

```txt
internal/plugins/traffic/
├── plugin.go      # Plugin implementation
├── handlers.go    # HTTP handlers
├── icons.go       # Sidebar icon
├── settings.go    # Settings page component
└── README.md      # This file
```

## Development

### Building

The plugin is automatically included in the build via its `init()` function.
