# Metrics Gear

The Metrics gear surfaces time-series metrics for HAProxy and the underlying
host at the dashboard's `/metrics` URL. It's the place users land when they
want to answer *"how busy was the proxy, and what went wrong?"*

> The page used to live at `/history` and was internally referred to as the
> "history" gear; issue #97 renamed it to `/metrics` to keep "history"
> reserved for genuinely distinct concepts (OS-update apt/zypper history,
> HAProxy config change history).

## Layout

The page has four stacked sections:

1. **KPI summary band** — six compact stat cards across the top with an
   inline sparkline and a delta vs. the previous window of equal length:
   Requests / min, Avg Response, Error Rate %, 5xx Errors, Active Sessions,
   Healthy Backends N/M. Cards colour themselves by health (green / amber /
   red) and the delta arrow inverts so that "going up" is red for errors
   and green for traffic. Hover any card for the explanation tooltip.
2. **Charts grid** — seven time-series charts (Sessions, Server Health,
   CPU Load, Memory, Network, Response Times, 5xx Errors). All charts use
   index-mode crosshair tooltips so hovering any chart shows all series at
   the same timestamp. Click any card's fullscreen icon to expand. The 5xx
   Errors chart has a gradient fill — the colour intensity makes spikes
   obvious at a glance.
3. **Error Insights** — replaces the old "Recent Incidents" list. When the
   window contains any 4xx/5xx responses, this panel shows three columns:
   *Top backends by errors*, *Top source IPs*, and *Top countries*. Click
   any backend or source row to open the drill-down drawer. If the window
   has no errors, the panel collapses to a green "everything's quiet" tile.
4. **Drill-down drawer** — slides in from the right when you click a row.
   Shows a per-backend summary (requests, error rate, 5xx, 4xx, average
   latency), two mini-charts (requests + errors over time, status code
   doughnut), the top source IPs hitting that backend, and a list of
   recent 5xx HAProxy log lines (pulled from the agent and filtered by
   backend). A "View logs →" link jumps to the Logs gear with the HAProxy
   source pre-selected. `Esc` or backdrop-click closes the drawer.

## Configuration

| Setting          | Type | Default | Description                    |
|------------------|------|---------|--------------------------------|
| `store_history`  | bool | true    | Enable historical data storage |
| `retention_days` | int  | 7       | Data retention period in days  |

## Permissions

| Permission          | Description                |
|---------------------|----------------------------|
| `metrics:view`      | View the Metrics page      |
| `metrics:configure` | Configure metrics settings |

The drill-down drawer's *Recent 5xx log lines* section additionally
requires `logs:view`. If the user doesn't have that permission, the section
shows a "logs unavailable" hint instead of failing.

## Routes

| Method | Path       | Description       |
|--------|------------|-------------------|
| GET    | `/metrics` | Main Metrics page |

## API endpoints (main handler)

Time-series endpoints (chart data):

| Method | Path                                            | Description                                   |
|--------|-------------------------------------------------|-----------------------------------------------|
| GET    | `/api/{serverID}/metrics/stats`                 | Time-series HAProxy stats (per-snapshot rows) |
| GET    | `/api/{serverID}/metrics/system`                | Time-series host metrics (CPU/mem/disk/net)   |
| GET    | `/api/{serverID}/metrics/backend/{backendName}` | Time-series per-backend stats                 |
| GET    | `/api/{serverID}/metrics/storage-stats`         | Storage statistics                            |
| POST   | `/api/{serverID}/metrics/clear`                 | Clear metrics data                            |

New in v2 ("insights" surface — see `api_metrics_insights.go`):

| Method | Path                                                          | Description                                                                                  |
|--------|---------------------------------------------------------------|----------------------------------------------------------------------------------------------|
| GET    | `/api/{serverID}/metrics/summary?range=...`                   | KPI band: 6 cards, each with value, prior-window delta, sparkline                            |
| GET    | `/api/{serverID}/metrics/error-breakdown?range=...`           | Top backends / source IPs / countries responsible for 4xx+5xx in the window                  |
| GET    | `/api/{serverID}/metrics/backend/{name}/details?range=...`    | Per-backend time-series + top sources for the drill-down drawer                              |
| GET    | `/api/{serverID}/metrics/log-errors?status_min=500&...`       | Recent HAProxy log lines with status >= `status_min`, parsed into source/backend/path/status |

All four accept a `range` query param of `5m`, `30m`, `1h`, `6h`, `24h`,
`3d`, or `7d` (default `24h`). `log-errors` also accepts `lines` (1–10000,
default 2000) and an optional `backend` filter.

## Data sources

The new endpoints sit on top of existing tables — no new collection runs
on the agent. Specifically:

- KPI summary aggregates `stats_history` (per-snapshot HAProxy stats — table
  name unchanged from the pre-rename schema; the records are historical) and
  `traffic_flows` (per-minute response-code buckets from the Traffic gear's
  collector).
- Error Insights and backend details query `traffic_flows` exclusively —
  it stores actual per-bucket response counts rather than cumulative
  counters, so error rates are accurate without delta math.
- Log-errors hits the agent's `/api/v1/logs/haproxy` endpoint live, then
  parses lines in the dashboard. The agent is unchanged.

## Architecture

```text
internal/gears/metrics/
├── plugin.go             # Gear registration (/metrics route)
├── handlers.go           # MetricsPage handler
├── partials.templ        # CPU / memory / disk / etc. widget partials
├── chart_partials.templ  # Reusable chart components for the home gear
├── icons.go              # Sidebar icon
├── settings.go           # Settings page component
└── README.md             # This file

internal/framework/handler/
├── api_stats.go                       # Time-series endpoints
│                                      # (/api/{boxID}/metrics/{stats,system,backend/*})
├── api_metrics_insights.go            # KPI / error breakdown / drill-down / log-errors
└── api_metrics_insights_helpers.go    # KPI math, sparkline downsampling

internal/framework/database/
└── metrics_insights.go    # Top-backends / top-sources / time-series queries
                           # (uses the existing traffic_flows table)
```

## Development

The gear is automatically included in the build via its `init()` function.
After editing `metrics.templ`, run:

```bash
make templ-generate && make build
```
