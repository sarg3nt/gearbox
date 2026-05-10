# Home Gear

The **Home** gear is Gearbox's box-agnostic app dashboard — a self-hosted
start page with launcher tiles, service widgets, bookmarks, and a search bar,
in the spirit of [Homepage](https://gethomepage.dev),
[Homarr](https://homarr.dev), [Dashy](https://dashy.to), and
[Heimdall](https://github.com/linuxserver/Heimdall) — but built into Gearbox
with first-class auth and server-side proxying.

It is the first gear with `Info.Scope == ScopeSystem`: there is one row per
install (keyed by `database.SystemServerID`) instead of one row per monitored
box, since a dashboard isn't tied to any particular server.

## Capabilities

- **Drag-and-drop tile board** powered by [gridstack.js](https://gridstackjs.com/).
  The column count is **derived from viewport width** at ~76 px per cell
  (clamped 6–60), so tiles keep a consistent physical size as the window
  grows — wider viewports get more columns, not bigger tiles. Saved
  positions reflow via gridstack's "list" layout when the column count
  changes; a `ResizeObserver` on the grid container drives recompute.
- **Edit-mode toggle** so casual clicks never accidentally drag tiles.
  Per-tile delete + edit buttons appear in edit mode; auto-save on every drop.
- **Six size presets** in the add/edit modal: Auto, Compact 2×1,
  Square 2×2, Tall 2×3, Wide 4×2, **Big 4×3**. Auto picks 2×2 for
  launchers, 4×2 for widget-provider apps, and 4×3 for apps whose
  catalog entry exposes more than four widget fields (Plex, UniFi
  with its full set, etc.) so pill rows don't clip.
- **Multiple boards** — single-board users never see the switcher; once a
  second board exists, a dropdown appears in the header. URLs are routable
  (`/home/`, `/home/b/<slug>`).
- **Predefined apps catalog** of 50+ self-hosted apps (Sonarr, Radarr,
  qBittorrent, Plex, Jellyfin, Pi-hole, AdGuard, Portainer, Nextcloud,
  UniFi, Vaultwarden, Home Assistant, Grafana, etc.) — see
  [`catalog/apps.json`](catalog/apps.json). Icons via
  [selfh.st/icons](https://selfh.st) over jsDelivr.
- **URL fingerprinter** — paste an app's URL into the add-tile modal and
  the backend probes well-known endpoints (`/api/v3/system/status`,
  `/identity`, `/status.php`, etc.) in parallel; first match auto-fills
  the form with the right name, icon, and slug. See
  [`catalog.go`](catalog.go). The URL field also auto-prepends
  `https://` on blur when the user types a bare host.
- **Icon-name suggester** — fallback for URLs that don't match any
  catalog fingerprint (notably bookmarks). Hostname labels are matched
  against the selfh.st icon library by exact slug/name, so
  `google.com → Google icon` and `navidrome.example.com → Navidrome icon`
  fill empty Name/Icon fields automatically. See
  [`iconlibrary.go`](iconlibrary.go) and `/home/api/icons/suggest`.
- **Tier-1 widgets** — Sonarr/Radarr/Prowlarr/Lidarr/Readarr (queue,
  wanted, library counts), qBittorrent (download/upload speed,
  leechers/seeders), Pi-hole (queries, blocked, gravity), **Plex**
  (active streams, transcodes, total bandwidth, library counts via
  `X-Plex-Token`), and **UniFi Network** (clients by type, devices
  online/offline, gateway uplink throughput, gateway CPU/mem, uptime
  via the Integration API's `X-API-KEY`). Each provider lives in
  [`widget/`](widget/) and runs **server-side only** — secrets never
  leave the Go process.
- **Sparkline graphs** — fields tagged `graphable: true` in the catalog
  (currently UniFi `wan_down` / `wan_up`) render an inline 38×12 SVG
  trend line next to the value. The browser buffers the last 60
  samples per tile×field in memory and scales each line to its
  buffer's min/max — a sleepy WAN at 0–5 Mbps reads the same shape
  as a busy one at 200–800 Mbps. Bandwidth values are normalized to
  bits/s before graphing so the trend stays smooth across Kbps↔Mbps
  unit boundaries.
- **API Instructions dialog** — when a catalog entry has an
  `api_help` block, the green "Detected: <App>" banner exposes an
  *API Instructions* button. The resulting dialog renders the
  catalog's step-by-step instructions (with inline `<code>` for paths
  and button names), an optional security note, and an *Open <App>
  settings* deep link built from the tile's URL plus the entry's
  `settings_path`. Plex (no settings page exposes its token) gets
  the View-XML walkthrough; the Servarr family gets a one-click jump
  to `/settings/general`. Currently 14 entries ship with
  hand-written instructions.
- **Customapi widget** — point at any JSON URL with optional
  basic / bearer / custom-header auth, configure dot-notation field
  mappings (`origin.name`, `locations.0.lat`) with formatters for
  bytes / bitrate / percent / duration / etc. The
  `POST /home/api/customapi/preview` endpoint returns the parsed JSON
  tree so a future UI can offer a clickable picker.
- **Server-side health checks** — every tile URL is probed (HEAD with
  GET fallback) on a 30s default cadence (configurable 10–300s
  per-tile). Status dot updates push live to the browser via SSE.
  Backoff on failure: 30 → 60 → 120 → 300s. Polling pauses entirely
  when no SSE clients are connected.
- **Encrypted secrets** — API keys, basic-auth passwords, and bearer
  tokens are encrypted at rest with the existing AES-256-GCM
  [`crypto.Encryptor`](../../framework/services/crypto/encryption.go)
  used for agent tokens. The browser only ever sees `has_secret: true`
  — secrets are decrypted only inside backend handlers when fetching
  upstream data.
- **Search bar** — DuckDuckGo by default. Press `/` to focus.
  Bookmark-search: if the query matches a tile's display name, opens
  that tile instead of running a web search.
- **Schema-versioned import/export** — JSON backup/restore with a
  forward-migration runner. Encrypted secrets are deliberately
  excluded from backups (carrying them across hosts requires sharing
  the master key). See [`export.go`](export.go).
- **Per-user default landing page** — opt-in toggle in the header sets
  `users.default_landing_path = '/home'` so the post-login redirect
  takes you straight here. Falls back to a system-wide default
  (`HomeConfig.SystemDefaultLandingPath`) and then to `/`.

## Routes

| Method | Path                                  | Permission | Purpose                                         |
|--------|---------------------------------------|------------|-------------------------------------------------|
| GET    | `/home/`                              | view       | Render the active dashboard board.              |
| GET    | `/home/b/{slug}`                      | view       | Render a specific board.                        |
| GET    | `/home/api/boards`                    | view       | List boards.                                    |
| POST   | `/home/api/boards`                    | edit       | Create a board.                                 |
| PATCH  | `/home/api/boards/{id}`               | edit       | Update a board's name and order.                |
| DELETE | `/home/api/boards/{id}`               | edit       | Delete a board (refuses if it's the last one).  |
| GET    | `/home/api/boards/{id}/tiles`         | view       | List tiles on a board (with `has_secret` flag). |
| POST   | `/home/api/boards/{id}/tiles`         | edit       | Create a tile.                                  |
| PATCH  | `/home/api/tiles/{id}`                | edit       | Update tile layout and/or config.               |
| DELETE | `/home/api/tiles/{id}`                | edit       | Delete a tile.                                  |
| PUT    | `/home/api/tiles/{id}/secret`         | edit       | Encrypt and store a tile's secret.              |
| DELETE | `/home/api/tiles/{id}/secret`         | edit       | Clear a tile's secret.                          |
| GET    | `/home/api/tiles/{id}/status`         | view       | One-shot reachability snapshot.                 |
| GET    | `/home/api/tiles/{id}/widget`         | view       | One-shot widget data snapshot.                  |
| GET    | `/home/api/events`                    | view       | SSE stream of `tile.status` and `tile.widget`.  |
| GET    | `/home/api/catalog`                   | view       | Predefined apps catalog.                        |
| GET    | `/home/api/probe?url=…`               | view       | Fingerprint a URL against the catalog.          |
| POST   | `/home/api/customapi/preview`         | edit       | Live JSON preview for the customapi builder.    |
| GET    | `/home/api/export`                    | view       | Download a JSON backup.                         |
| POST   | `/home/api/import`                    | admin      | Replace dashboard state from a backup.          |
| POST   | `/home/api/landing-path`              | view       | Set the calling user's default-landing-path.    |

## Permissions

| Component | Action  | Description                       |
|-----------|---------|-----------------------------------|
| home      | view    | View the dashboard.               |
| home      | edit    | Add / move / edit / delete tiles. |
| home      | admin   | Import backups / system config.   |

## Database

Tables (see [`internal/framework/database/home.go`](../../framework/database/home.go)):

- `home_boards` — `(id, slug, name, sort_order, …)`.
- `home_tiles` — `(id, board_id, type, x, y, w, h, config, sort_order, …)`.
- `home_tile_secrets` — `(tile_id, encrypted_payload, …)`. Joined only by
  backend code paths.

Cascade-on-delete is wired explicitly because SQLite FK enforcement is off
project-wide.

## Configuration

The gear stores its system-wide config in the `gears.config` JSON column
under `server_id = '__system__'`, `name = 'home'`. Schema:

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

## Architecture notes

- Status reachability and widget data are handled by **two separate
  goroutines**, both gated on SSE-client count so an idle dashboard
  costs nothing.
- Secrets never leave the backend. Browsers only see `has_secret: true`
  and pre-rendered widget field maps.
- The grid is server-rendered first (so the dashboard works without JS
  for read-only viewing), then upgraded to drag/resize by gridstack on
  load.
- The catalog and fingerprint logic are decoupled — most catalog entries
  have no fingerprint and are reached via the manual picker in the
  add-tile modal.

## Adding a Tier-1 widget provider

A widget provider lives in [`widget/`](widget/) and implements the
`Provider` interface (slug + `Fetch(ctx, Request) (Result, error)`).
The runner calls `widget.Get(slug)` per tile and broadcasts the result
on the SSE stream every 30 seconds (gated on connected clients). To
add support for a new app:

1. Write a `widget/<slug>.go` that registers itself in `init()`
   ([`plex.go`](widget/plex.go) and [`unifi.go`](widget/unifi.go) are
   short, current examples).
2. Hit upstream via the request's `BaseURL` + `Secret`. Use
   `DefaultClient(req.Timeout)` so self-signed certs work — UniFi /
   Plex / Servarr instances on a homelab almost always have one.
3. Add or extend the catalog entry in [`catalog/apps.json`](catalog/apps.json):
   set `auth`, list the `widget_fields` (key + label + `default`,
   plus `graphable: true` for rate-style fields you want sparklined),
   and ideally add an `api_help` block — the dialog wired to it is a
   huge UX win when the upstream's API key is buried in a settings
   page (or, in Plex's case, isn't visible from any settings page).
4. The frontend renders pill labels from `widget_fields[].label` (with
   a camelCase fallback if the catalog is empty), so as long as the
   key matches what your provider returns, you don't need to touch JS.

## What's not (yet) here

- **Live JSON tree picker UI** for the customapi builder — backend
  `customapi/preview` endpoint is wired; the clickable browser tree
  is a Phase 10.5 polish item.
- **WAN dropout history** for the UniFi widget — the Integration API
  doesn't expose events; we'd derive it by tracking ONLINE→OFFLINE
  transitions in our own state across widget refreshes.
- **Historical traffic graphs** for UniFi — sparklines start from
  page load and accumulate; UniFi doesn't ship time-series data over
  the Integration API, so anything older than the current session
  isn't available.
- **Per-user boards** — boards are shared in v1.
- **Mobile drag/drop** — desktop only; mobile collapses to a single
  column with a list-reorder UI in edit mode.
