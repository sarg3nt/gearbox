# Alerts Plugin

The Alerts plugin provides alert management, rules configuration, and notification functionality.

## Features

- Active alert monitoring
- Alert acknowledgement and resolution
- Alert rules configuration
- Email notifications (when SMTP configured)
- Alert history and retention
- Notes and comments on alerts

## Configuration

| Setting                | Type | Default | Description                  |
|------------------------|------|---------|------------------------------|
| `enable_notifications` | bool | true    | Enable email notifications   |
| `retention_days`       | int  | 30      | Days to keep resolved alerts |

## Permissions

| Permission         | Description                    |
|--------------------|--------------------------------|
| `alerts:view`      | View alerts and history        |
| `alerts:manage`    | Acknowledge and resolve alerts |
| `alerts:configure` | Configure alert rules          |

## Routes

| Method | Path      | Description      |
|--------|-----------|------------------|
| GET    | `/alerts` | Main alerts page |

## API Endpoints (Main Handler)

The following API endpoints are related to alerts but remain in the main handler:

| Method | Path                                | Description                   |
|--------|-------------------------------------|-------------------------------|
| GET    | `/api/alerts/count`                 | Get global active alert count |
| GET    | `/api/{serverID}/alerts`            | Get alerts by status          |
| GET    | `/api/{serverID}/alerts/summary`    | Get alert summary stats       |
| GET    | `/api/{serverID}/alerts/rules`      | Get alert rules               |
| POST   | `/api/{serverID}/alerts/rules`      | Create alert rule             |
| PUT    | `/api/alerts/rules/{ruleID}`        | Update alert rule             |
| DELETE | `/api/alerts/rules/{ruleID}`        | Delete alert rule             |
| POST   | `/api/alerts/{alertID}/acknowledge` | Acknowledge alert             |
| POST   | `/api/alerts/{alertID}/resolve`     | Resolve alert                 |
| POST   | `/api/alerts/{alertID}/silence`     | Silence alert                 |
| GET    | `/api/alerts/{alertID}/notes`       | Get alert notes               |
| POST   | `/api/alerts/{alertID}/notes`       | Add alert note                |

## Architecture

```txt
internal/plugins/alerts/
├── plugin.go      # Plugin implementation
├── handlers.go    # HTTP handlers
├── icons.go       # Sidebar icon
├── settings.go    # Settings page component
└── README.md      # This file
```

## Development

### Building

The plugin is automatically included in the build via its `init()` function.
