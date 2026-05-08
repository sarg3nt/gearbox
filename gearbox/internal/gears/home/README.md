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

- **Drag-and-drop tile board** powered by [gridstack.js](https://gridstackjs.com/)
  on a 12-column responsive grid.
- **Edit-mode toggle** so casual clicks never accidentally drag tiles.
  Per-tile delete buttons appear in edit mode; auto-save on every drop.
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
  [`catalog.go`](catalog.go).
- **Tier-1 widgets** — Sonarr/Radarr/Prowlarr/Lidarr/Readarr (queue,
  wanted, library counts), qBittorrent (download/upload speed,
  leechers/seeders), Pi-hole (queries, blocked, gravity). Live in
  [`widget/`](widget/). Each provider is < 100 lines.
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

## What's not (yet) here

- **Plex Tier-1 widget** — Plex uses XML and `X-Plex-Token`; structurally
  different from the Sonarr/Radarr family. The framework supports it; a
  provider just needs writing.
- **Live JSON tree picker UI** for the customapi builder — backend
  `customapi/preview` endpoint is wired; the clickable browser tree
  is a Phase 10.5 polish item.
- **Per-user boards** — boards are shared in v1.
- **Mobile drag/drop** — desktop only; mobile collapses to a single
  column with a list-reorder UI in edit mode.
