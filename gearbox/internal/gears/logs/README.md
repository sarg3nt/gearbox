# Logs Gear

The Logs gear provides real-time viewing and searching of HAProxy and system logs.

## Features

- View HAProxy access logs with syntax highlighting
- View system/kernel logs
- Real-time log streaming via SSE
- Text and severity filtering
- Copy and download functionality
- Fullscreen mode for log analysis

## Configuration

| Setting         | Type | Default | Description                            |
|-----------------|------|---------|----------------------------------------|
| `default_lines` | int  | 100     | Number of log lines to load by default |
| `auto_refresh`  | bool | true    | Enable real-time log streaming         |

## Permissions

| Permission       | Description                    |
|------------------|--------------------------------|
| `logs:view`      | View logs page and log content |
| `logs:configure` | Configure log source settings  |

## Routes

| Method | Path             | Description                  |
|--------|------------------|------------------------------|
| GET    | `/logs`          | Main logs page               |
| GET    | `/logs/sources`  | Get available log sources    |
| GET    | `/logs/{source}` | Get log content for a source |

## Events

### Published Events

None.

### Subscribed Events

| Event          | Description                        |
|----------------|------------------------------------|
| `logs.updated` | Appends new log lines in real-time |

## Architecture

```txt
internal/gears/logs/
├── gear.go      # Gear implementation
├── handlers.go    # HTTP handlers
├── icons.go       # Sidebar icon
├── settings.go    # Settings page component
└── README.md      # This file
```

## Development

### Testing

```bash
go test ./internal/gears/logs/...
```

### Building

The gear is automatically included in the build via its `init()` function.

To exclude this gear from a build, use build tags (future enhancement).
