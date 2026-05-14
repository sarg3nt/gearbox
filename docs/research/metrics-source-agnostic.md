# Metrics gear — source-agnostic & multi-service plan

## TOC

- [Context](#context)
- [Today's reality](#todays-reality)
- [Target shape](#target-shape)
- [Architectural model: metric sources](#architectural-model-metric-sources)
- [Phased rollout](#phased-rollout)
  - [Phase 0 — Capability manifest endpoint](#phase-0--capability-manifest-endpoint)
  - [Phase 1 — Source attribution in the UI](#phase-1--source-attribution-in-the-ui)
  - [Phase 2 — Graceful no-HAProxy mode](#phase-2--graceful-no-haproxy-mode)
  - [Phase 3 — Service detection layer](#phase-3--service-detection-layer)
  - [Phase 4 — nginx metrics gear (proof of source generality)](#phase-4--nginx-metrics-gear-proof-of-source-generality)
  - [Phase 5 — Generic access-log abstraction](#phase-5--generic-access-log-abstraction)
  - [Phase 6 — Multi-source Error Insights](#phase-6--multi-source-error-insights)
  - [Phase 7 — Apache, Caddy, Docker, Traefik](#phase-7--apache-caddy-docker-traefik)
  - [Phase 8 — Cross-source aggregates (optional)](#phase-8--cross-source-aggregates-optional)
- [Data model changes](#data-model-changes)
- [API additions and stability](#api-additions-and-stability)
- [Scope of agent changes by phase](#scope-of-agent-changes-by-phase)
- [Risks and open questions](#risks-and-open-questions)

## Context

The Metrics gear (`/history`) was designed around HAProxy. Its KPI band, charts grid, and (as of [#87](https://github.com/sarg3nt/gearbox/issues/87)) Error Insights panel implicitly assume HAProxy is present and is the only source of request-level metrics. Pointed at a plain Linux box, the dashboard either renders empty panels or shows misleading "0" values.

This document plans the evolution to a **source-agnostic** metrics dashboard that:

1. Works on any host with the gearbox-agent installed, even with no proxy / web server present.
2. Becomes richer automatically as the agent detects services (HAProxy, nginx, Apache, Caddy, Traefik, Docker, …).
3. Attributes every metric to its source in the UI (e.g. "HAProxy: Response Times", "nginx: 5xx", "Host: CPU Load") so users always know what they're looking at.

## Today's reality

Audited 2026-05-14 against `gearbox-agent` and `gearbox`:

| Concern                              | Status                                                                                                                                                                                                                                                                                            |
|--------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Probe phase / capability manifest    | Exists in-memory only. `gear.ProbeResult` schema is well-defined ([interface.go:208–297](../../gearbox-agent/internal/framework/gear/interface.go)). Result map populated at startup. **No API endpoint exposes it** — a comment in `manager.go:114` references an "upcoming" endpoint that hasn't shipped. |
| Service discovery                    | `gearbox-agent/internal/framework/discovery/` has detectors for **HAProxy, Docker, systemd**. No nginx/Apache/Caddy/Traefik. Detectors are not wired into the probe phase yet.                                                                                                                    |
| Host metrics                         | Solid: `collector.go` collects CPU load, memory, disk, network, plus per-systemd-unit status.                                                                                                                                                                                                     |
| HAProxy metrics                      | Pulled via admin socket or stats URL; parsed from CSV; per-frontend / per-backend / per-server.                                                                                                                                                                                                    |
| Access-log parsing                   | **Only on the dashboard side.** Agent streams raw text from `/api/v1/logs/haproxy`. Dashboard's `parseHAProxyLogLine` in `api_metrics_insights.go` is the only structured parser.                                                                                                                  |
| Metrics collector extensibility      | Hard-coded to one host source. No plugin-style "register an additional source" mechanism.                                                                                                                                                                                                          |
| Source attribution in UI             | None. Chart titles like "Response Times" / "5xx Errors" implicitly mean "HAProxy".                                                                                                                                                                                                                 |

The good news: the bones are in place. `ProbeResult` is the right schema, the gear framework already has the "probe phase" concept, and `discovery/` is the right package. The work is mostly **filling in what's already named** rather than redesigning.

## Target shape

A user pointing the dashboard at a box should see:

1. **Always:** host metrics (CPU, memory, disk, network, load, uptime, systemd service status). Labelled "Host".
2. **When detected:** a section per service. HAProxy → request-level metrics + Error Insights + drill-down (as today). nginx → analogous. Caddy → analogous. Docker → per-container CPU/mem/network.
3. **KPI band:** core host KPIs at the front (always). One additional KPI per detected service (e.g. "HAProxy req/s", "nginx req/s"), shown only when that source is present.
4. **Source attribution:** every card, chart and panel header carries the source name. Tooltips on the KPI cards explain *which collector* produced the number.
5. **Cross-source views (later):** when 2+ proxies are on the same box, a "Combined" toggle aggregates request/error metrics across them.

## Architectural model: metric sources

Introduce a first-class **MetricSource** concept on both sides:

```text
MetricSource
├── ID        e.g. "haproxy", "nginx", "host", "docker"
├── Label     e.g. "HAProxy", "nginx", "Host", "Docker"
├── Icon      sidebar/badge icon
├── Version   e.g. "2.8.5"
├── Status    e.g. "available", "stopped", "not_installed"
├── Reason    human-readable detail
└── Metrics   what this source can produce
    ├── kpis           [ "requests_rate", "5xx_count", ... ]
    ├── timeseries     [ "request_rate", "response_time_ms", ... ]
    └── breakdowns     [ "by_backend", "by_source_ip", "by_country", ... ]
```

On the agent: a `MetricSource` is a gear-level concept. Each agent gear that produces request-level metrics implements a small interface that returns its `MetricSource` descriptor plus standardised series.

On the dashboard: the metrics page renders cards/charts/panels by iterating the box's `sources[]` list. Cards for sources that aren't present simply don't render.

This means **adding a new service (nginx, Caddy) becomes a matter of writing one agent gear** — no dashboard change required to surface it, beyond per-source labels.

## Phased rollout

Each phase is independently shippable and improves the page on its own. **Stop after any phase and the dashboard is still strictly better than today.**

### Phase 0 — Capability manifest endpoint

**Agent**: ship the endpoint the comment promises.

- Add `GET /api/v1/system/capabilities` returning the probe map as JSON.
- Schema:

  ```json
  {
    "agent_version": "...",
    "collected_at": "2026-05-14T...",
    "sources": [
      {
        "id": "host",
        "label": "Host",
        "status": "available",
        "version": "Linux 6.x",
        "capabilities": {"cpu_count": "8", "kernel": "..."}
      },
      {
        "id": "haproxy",
        "label": "HAProxy",
        "status": "available",
        "version": "2.8.5",
        "capabilities": {"stats_socket": "/run/haproxy/admin.sock"}
      }
    ]
  }
  ```

- Reuse the existing `gear.ProbeResult` map; add a thin marshalling layer.

**Dashboard**: query and cache `capabilities` per box.

- Surface in `BoxConfig` or a sibling `BoxCapabilities` struct.
- Refresh on box reconnect + a 5-min TTL.
- No UI change yet — purely plumbing.

**Effort:** small. **Risk:** very low; purely additive.

### Phase 1 — Source attribution in the UI

Every chart and KPI now declares its source.

- Update the existing 7 charts: titles become "HAProxy: Sessions & Requests", "Host: CPU Load", "HAProxy: 5xx Errors", etc.
- KPI cards get a small source badge in the upper-right corner (`HAProxy`, `Host`).
- Error Insights renamed to "HAProxy Error Insights".
- Tooltips on cards explain the source ("Reported by HAProxy stats via /api/{id}/metrics/summary").

This phase **changes wording only** — no behaviour change. But it sets up the user expectation that metrics are tied to sources, and prepares the UI for additional sources.

**Effort:** small. **Risk:** very low.

### Phase 2 — Graceful no-HAProxy mode

Make the page useful on a plain Linux box.

- If `capabilities.sources` does not include `haproxy`, hide:
  - "HAProxy: Sessions & Requests"
  - "HAProxy: Server Health"
  - "HAProxy: Response Times"
  - "HAProxy: 5xx Errors"
  - The Error Insights panel
  - Request-related KPIs (Requests/min, Error Rate, 5xx, Active Sessions, Healthy Backends)
- Keep showing:
  - "Host: CPU Load", "Host: Memory", "Host: Network"
  - Host KPIs (we'll add some): CPU %, Memory %, Disk %, Load (1m), Uptime, Systemd services failed
- Empty-state banner: "No HAProxy detected on this host. Showing host-level metrics only. Install HAProxy or enable the agent's HAProxy gear to see proxy metrics."

This phase makes the page **honest** on non-HAProxy boxes for the first time.

**Effort:** small/medium (mostly conditional rendering + a few new host KPIs).
**Risk:** low — purely additive in the missing-source case.

### Phase 3 — Service detection layer

Flesh out `gearbox-agent/internal/framework/discovery/`:

- Add `nginx.go`, `apache.go`, `caddy.go`, `traefik.go` detectors. Each:
  - Tries the binary on `PATH` and a `--version` invocation.
  - Locates the config file (well-known paths).
  - Probes for a status / metrics endpoint:
    - nginx: `http://127.0.0.1/nginx_status` (stub_status), `/basic_status`, or Plus API.
    - Apache: `http://127.0.0.1/server-status?auto` (mod_status).
    - Caddy: admin API at `http://127.0.0.1:2019/metrics`.
    - Traefik: `:8082/metrics` or dashboard API.
  - Returns a `ProbeResult` with status + capabilities (e.g. `status_endpoint: http://127.0.0.1/nginx_status`).
- Wire detectors into the existing probe phase so they appear in the capabilities manifest.

After this phase, the manifest tells the dashboard *what's installed*. The dashboard doesn't yet know how to read those services' metrics — that's Phase 4+.

**Effort:** medium (one detector per service, ~50–100 lines each).
**Risk:** medium — has to be robust against permission and network failures.

### Phase 4 — nginx metrics gear (proof of source generality)

Implement the first non-HAProxy source end-to-end. Choose nginx because it's the most-deployed alternative.

**Agent side:**

- New gear: `gearbox-agent/internal/gears/nginx/`.
- Strategy 1 (preferred when available): scrape `stub_status`. Returns requests/sec, active_connections, reading/writing/waiting counters.
- Strategy 2 (fallback): tail and parse the access log. Use a configurable format profile (default: combined). Same regex-based parsing approach as the dashboard's `parseHAProxyLogLine`, generalised.
- Reports a `NginxStats` struct with:
  - `requests_total`, `requests_rate`
  - `active_connections`
  - `response_2xx/3xx/4xx/5xx` (from log when stub_status doesn't cover them)
  - `avg_response_time_ms` (when available — nginx logs only have it with `$request_time`)
  - per-vhost breakdown (parsed from log's `$host`)
- New agent endpoints:
  - `GET /api/v1/nginx/stats`
  - `GET /api/v1/nginx/summary`
- Reuses the traffic delta-tracking pattern HAProxy already uses.

**Dashboard side:**

- New "nginx" section in the charts grid when capabilities include nginx.
- Reuses the existing chart components (gradient fill, crosshair tooltip).
- New KPI cards: "nginx: req/s", "nginx: 5xx", "nginx: 5xx %".
- Error Insights gets a second panel: "nginx Error Insights" with top vhosts / source IPs / paths.
- Drill-down drawer reused; backend → "vhost or location block" depending on what nginx reports.

This phase proves the architecture handles a second source cleanly.

**Effort:** medium-large (~2–3 days). **Risk:** medium — log-format variations are the main hazard.

### Phase 5 — Generic access-log abstraction

The agent's current logs gear streams raw text. The dashboard parses HAProxy lines server-side. nginx (Phase 4) will do its own log parsing. Apache/Caddy (Phase 7) will too.

Refactor before adding more:

- Promote `parseHAProxyLogLine` to the agent side as one profile of a `AccessLogParser`.
- Define profiles for: `haproxy-http`, `nginx-combined`, `apache-common`, `apache-combined`, `caddy-json`.
- Agent endpoint: `GET /api/v1/access-log/{source}/recent?status_min=500&limit=500` returning structured records.
- Dashboard's `/metrics/log-errors` becomes a thin proxy to this.

This consolidates parsing in one place and removes the dashboard-side regex maintenance.

**Effort:** medium. **Risk:** medium (must not regress the dashboard's current log-errors feature).

### Phase 6 — Multi-source Error Insights

The Error Insights panel currently shows one block of "Top backends / sources / countries" — implicitly HAProxy.

Generalise:

- One block per detected request-source. HAProxy block has "Top backends". nginx block has "Top vhosts". Apache block likewise.
- "Top source IPs" and "Top countries" sections aggregate across sources (an IP is an IP regardless of which proxy logged it).
- KPI band gains a per-source error count when multiple sources are present.

**Effort:** medium. **Risk:** low — additive UI; existing HAProxy code path doesn't change.

### Phase 7 — Apache, Caddy, Docker, Traefik

Each follows the nginx pattern:

- **Apache:** `mod_status` scraping; access-log parsing for status codes.
- **Caddy:** scrape the Prometheus endpoint at `:2019/metrics`. Caddy is the easiest because it emits well-structured Prometheus metrics already.
- **Traefik:** scrape Prometheus endpoint. Adds router/service-level granularity.
- **Docker:** new agent gear `gears/docker/` running `docker stats --no-stream --format` periodically. Per-container CPU/mem/network/disk-io. Especially useful on TrueNAS / mjolnir.

Each ships as its own gear and capability source. Pick the order based on user demand (Docker is high-value for the current homelab).

**Effort:** ~2 days each, parallelisable.
**Risk:** per-service quirks.

### Phase 8 — Cross-source aggregates (optional)

Once two or more proxies coexist on the same box, offer:

- A "Combined" toggle in the KPI band: sums requests/sec, 5xx, etc. across all sources.
- A "By source" view: stacked area chart showing what fraction of traffic each proxy is handling.

This is sweet-but-not-critical. Skip unless feedback demands it.

## Data model changes

### Agent

- `gear.ProbeResult` already exists; no change to the schema.
- New endpoint **`/api/v1/system/capabilities`** (Phase 0) exposes the probe map.
- New gears under `gearbox-agent/internal/gears/` for nginx, apache, caddy, docker (one per phase).
- `gearbox-agent/internal/framework/discovery/` grows one file per detected service.

### Dashboard

- New table: nothing required for Phases 0–2.
- For Phases 4+, `traffic_flows` (or a new `request_flows`) gains a `source` column:

  ```sql
  ALTER TABLE traffic_flows ADD COLUMN source TEXT NOT NULL DEFAULT 'haproxy';
  CREATE INDEX idx_traffic_flows_source ON traffic_flows(server_id, source, bucket_time);
  ```

  Migration: existing rows default to `'haproxy'`, since that's what they are today. Forward-compatible.
- `BoxCapabilities` cached per box. Roughly:

  ```go
  type BoxCapabilities struct {
      AgentVersion string
      CollectedAt  time.Time
      Sources      []SourceCapability
  }
  type SourceCapability struct {
      ID         string            // "haproxy", "nginx", "host", "docker"
      Label      string
      Status     string            // "available", "stopped", "not_installed"
      Version    string
      Reason     string
      Capabilities map[string]string
  }
  ```

## API additions and stability

### Agent

| Phase | Endpoint                              | Purpose                                                        |
|-------|---------------------------------------|----------------------------------------------------------------|
| 0     | `GET /api/v1/system/capabilities`     | Probe results as JSON                                          |
| 4     | `GET /api/v1/nginx/stats`             | nginx stub_status + log-derived counts                         |
| 4     | `GET /api/v1/nginx/summary`           | nginx summary (per-vhost top-N)                                |
| 5     | `GET /api/v1/access-log/{src}/recent` | Structured recent log lines for any registered source          |
| 7     | `GET /api/v1/apache/stats`            | Apache mod_status                                              |
| 7     | `GET /api/v1/caddy/stats`             | Caddy Prometheus scrape (re-emitted as gearbox shape)          |
| 7     | `GET /api/v1/docker/stats`            | docker stats per container                                     |
| 7     | `GET /api/v1/traefik/stats`           | Traefik Prometheus scrape                                      |

All additions — no breaking changes to existing endpoints. The existing `/api/v1/haproxy/*` endpoints stay exactly as they are.

### Dashboard

The KPI / error-breakdown / backend-details endpoints added in [#87](https://github.com/sarg3nt/gearbox/issues/87) gain an optional `source` query parameter (default: `haproxy`). New endpoints aren't required initially — same shapes, scoped to a source.

| Endpoint                                              | Change                                                                                                  |
|-------------------------------------------------------|---------------------------------------------------------------------------------------------------------|
| `/api/{id}/metrics/summary?range=…&source=…`          | Optional source. When omitted: returns "all sources" combined view.                                     |
| `/api/{id}/metrics/error-breakdown?range=…&source=…`  | Optional source filter.                                                                                 |
| `/api/{id}/metrics/backend/{name}/details?source=…`   | Path-param `{name}` becomes "logical entity name" — backend for HAProxy, vhost for nginx, etc.          |
| `/api/{id}/metrics/log-errors?source=…`               | Optional source. Default still `haproxy` for backwards compatibility.                                   |
| `/api/{id}/capabilities`                              | **New** — proxies the agent's capability manifest to the dashboard's auth-scoped clients.               |

## Scope of agent changes by phase

| Phase | Files touched                                                             | New gears | Risk to running agents      |
|-------|---------------------------------------------------------------------------|-----------|-----------------------------|
| 0     | `internal/api/system.go` (new handler) + 1 route                          | 0         | None                        |
| 3     | `internal/framework/discovery/` (4 new files)                             | 0         | None                        |
| 4     | `internal/gears/nginx/` (new gear) + route registration                   | 1         | Low — gear is opt-in        |
| 5     | New parser package; refactor logs gear                                    | 0         | Medium — touches logs gear  |
| 7     | One gear per service                                                      | 3–4       | Per-gear isolation          |

The agent's existing gear architecture is well-suited: each new source is its own self-contained gear. Disabling a gear disables that source cleanly.

## Risks and open questions

1. **Permissions.** Many web servers' status endpoints aren't world-readable. The agent must run with whatever permissions the host configures. For nginx `stub_status`, that usually means a `location /nginx_status { allow 127.0.0.1; }` block — we may need to detect "stub_status returns 403/404" and tell the user how to enable it.
2. **Log file paths.** Distros vary (`/var/log/nginx/access.log` vs `/usr/local/nginx/logs/access.log`). The discovery phase needs a config-file parse for the truth, with fallbacks.
3. **Disk pressure from log parsing.** Tailing busy access logs to compute response-time histograms is CPU/IO work. Consider: only sample N lines per minute, or only parse on-demand when the user opens the drill-down.
4. **Backwards compatibility.** Existing dashboards talking to existing agents must keep working through every phase. The capability manifest is opt-in; absent it, the dashboard treats the box as "HAProxy-only" exactly like today.
5. **Multiple instances of the same service.** What about two nginx instances on one host? Today's HAProxy code assumes one. Probably out of scope until someone hits it; flag as "future work".
6. **Per-source retention.** Currently retention is one knob applied to `traffic_flows`. With multiple sources, the data volume per source can differ wildly. May need per-source retention later.

This plan is deliberately conservative: every phase ships value on its own and the architecture choices (capability manifest, per-source gears, optional `source` API params) are additive. Worst case, we stop after Phase 2 and the dashboard finally treats non-HAProxy hosts honestly. Best case, we end up with a tool that's genuinely a "platform monitor", not "HAProxy with extras".
