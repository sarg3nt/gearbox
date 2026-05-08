# Home Gear

The **Home** gear is Gearbox's box-agnostic app dashboard — a self-hosted start
page with launcher tiles, service widgets, and bookmarks. It's the first gear
with `Scope: ScopeSystem` (a single row in the gears table keyed by
`SystemServerID`, not one row per monitored box).

## Status

Phase 1 scaffold only:

- Gear registers with `Scope: ScopeSystem`.
- `/home` serves a placeholder page.
- `EnsureSystemGears()` seeds the disabled-by-default row at startup.
- The post-login redirect and root `/` URL honour the per-user
  `default_landing_path` with a system-wide fallback.

Tile rendering, the predefined-apps catalog, the URL fingerprinter, the
status-check worker, the customapi widget, and import/export are all
forthcoming. See [issue #33](https://github.com/sarg3nt/gearbox/issues/33)
for the full roadmap.

## Routes

| Method | Path     | Purpose                            |
|--------|----------|------------------------------------|
| GET    | `/home/` | Render the active dashboard board. |

## Permissions

| Component | Action  | Description                      |
|-----------|---------|----------------------------------|
| home      | view    | View the dashboard.              |
| home      | edit    | Add/move/edit/delete own tiles.  |
| home      | admin   | Manage shared boards & defaults. |

## Configuration

The gear stores its system-wide config in the `gears.config` JSON column under
`server_id = '__system__'`, `name = 'home'`. Schema:

```json
{
  "system_default_landing_path": "/home",
  "health_checks_enabled": true,
  "default_status_interval_seconds": 30,
  "attribution_shown": false
}
```
