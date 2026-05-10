# Home Gear (Dashboard)

The **Home** gear is Gearbox's box-agnostic app dashboard — a self-hosted
start page with launcher tiles, service widgets, sparkline graphs,
bookmarks, and a search bar. It's the first gear with
`Info.Scope == ScopeSystem`: there is one row per install, not one per
monitored box, since a dashboard isn't tied to a particular server.

> **Implementation reference:** the canonical, code-adjacent docs live
> alongside the source at
> [`internal/gears/home/README.md`](../internal/gears/home/README.md) —
> capabilities, routes, permissions, schema, and architecture notes.
> This page is the **operator-facing overview** with quickstart and
> common questions.

## Quick start

1. Sign in to Gearbox as an admin and enable the **Home** gear from
   `Settings → Gears`. (Home is system-scoped, so it's listed in the
   global gears section, not under any individual box.)
2. Navigate to `/home/`. You'll see an empty board with an **+ Add tile**
   button in the top-right.
3. Click **+ Add tile**, paste an app's URL (e.g. `https://sonarr.example.com`),
   tab out of the field. The backend probes well-known endpoints in
   parallel; on a fingerprint hit, a green "Detected: <App>" banner
   pre-fills name + icon + slug.
4. For widget data, paste the upstream's API key into the **API Key**
   field. Click **API Instructions** in the detection banner if you
   need step-by-step guidance — works for Sonarr/Radarr/Prowlarr/
   Lidarr/Readarr/Bazarr, qBittorrent, Plex, Jellyfin, Tautulli,
   Pi-hole, AdGuard, Portainer, Immich, and UniFi.
5. **Save**. The tile lands on the board and starts its 30-second
   refresh loop.

## Built-in widget providers

Apps with first-class widgets (live data pills, server-rendered field
maps over SSE):

| App                  | Auth        | Sample fields                                          |
|----------------------|-------------|--------------------------------------------------------|
| Sonarr / Radarr      | API key     | `wanted`, `missing`, `queued`, `series` / `movies`     |
| Lidarr / Readarr     | API key     | `wanted`, `queued`, `artists` / `books`                |
| Prowlarr             | API key     | `numIndexers`, `numGrabs`, `numQueries`, `numFailQueries` |
| qBittorrent          | basic auth  | `download`, `upload`, `leech`, `seed`                  |
| Pi-hole              | API key     | `queries`, `blocked`, `blocked_percent`, `gravity`     |
| Plex                 | `X-Plex-Token` | `streams`, `transcodes`, `bandwidth`, `movies`, `tv`, `episodes`, `libraries` |
| UniFi Network        | `X-API-KEY` (Integration API) | `clients`, `wifi`, `wired`, `vpn`, `devices_online`, `devices_offline`, `wan_status`, `wan_down` *(graph)*, `wan_up` *(graph)*, `gateway_cpu`, `gateway_mem`, `uptime` |

Apps in the catalog without a tier-1 provider still get launcher tiles
(icon + name + reachability status). Adding a new provider is a
small Go file; see the **Adding a Tier-1 widget provider** section in
the gear's [internal README](../internal/gears/home/README.md).

## Sparkline graphs

Fields tagged `graphable: true` in the catalog (currently UniFi
`wan_down` and `wan_up`) render an inline ~38×12 SVG trend line next
to the value. The browser buffers the last 60 samples per
tile×field in memory and scales each line to its buffer's min/max.
Bandwidth values are normalized to bits/s before graphing so the
trend stays smooth across Kbps↔Mbps unit boundaries.

The buffer is **per session** — sparklines start from when you opened
the dashboard and grow as updates arrive. UniFi's Integration API
doesn't ship historical time-series, so anything older than the
current session isn't available. After ~2 minutes of dashboard
uptime you have 4 samples (1 every 30 s); after 30 minutes the buffer
is at its 60-sample cap and starts rolling.

## Security model (where do my API keys live?)

API keys, basic-auth passwords, and bearer tokens are:

- **Encrypted at rest** with the install's master key via the existing
  AES-256-GCM `crypto.Encryptor` (the same primitive used for agent
  API keys). Keys are stored in the `home_tile_secrets` table,
  separate from `home_tiles` so the secret can be `JOIN`ed only by
  code paths that need it.
- **Used only by backend handlers**. All third-party API calls
  (Sonarr, Plex, UniFi, etc.) are made by the Gearbox Go process
  with the decrypted key in headers. The browser never has the key.
- **Never serialized into responses or templ renders**. The widget
  refresh sends the *rendered field map* (`{"streams": "2",
  "bandwidth": "12.4 Mbps"}`) over SSE — the upstream's raw response
  body never reaches the browser.
- **Surfaced to the UI as `has_secret: true` only**. To see a stored
  key after creation, you re-enter it (Stripe-style); there is no
  "show key" button.

If you compromise the Gearbox host, you can read the in-memory cache
and decrypt at-rest secrets with the master key — that's the same
threat model as every other secret in the app.

## Configuration

System-wide settings live on the gear's own `gears.config` JSON row
(`server_id = '__system__'`, `name = 'home'`):

```json
{
  "system_default_landing_path": "/home",
  "health_checks_enabled": true,
  "default_status_interval_seconds": 30,
  "attribution_shown": false
}
```

Per-tile cadence overrides live on the tile's own config JSON
(`AppConfig.status_interval_seconds`, `AppConfig.status_checks_disabled`).

## Backup / restore

The gear ships with schema-versioned import/export at
`/home/api/export` and `/home/api/import`. The JSON file is portable
across installs, but **encrypted secrets are deliberately excluded**
— carrying them across hosts would require sharing the master key,
and we'd rather you re-enter keys on the destination than punch a
hole in the threat model.
