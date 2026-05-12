# Bx Gear

The **Bx** gear is Gearbox's fleet-overview page and the home of the
box-switching chrome. It lists every configured monitored box in one
place — name, location, agent host, latency, status dot — and is the
default entry point when no specific box is active.

## Why it exists

Before this gear, every page in Gearbox had its own `<select>` server
dropdown stuffed into the page header, the active-box selection was
stored in `sessionStorage` only, and the sidebar always reflected the
"first enabled box" regardless of which box the user was actually
looking at. That worked for one or two boxes; it ran out of road
somewhere between ten and a hundred. The Bx gear replaces that pattern
with two affordances that share the same underlying state:

- A dedicated **fleet view page** at `/bx` — server-rendered table of
  every box with live status dots. The default landing for users who
  have not pinned a specific box.
- A **persistent chip + command-palette switcher** in the top-left of
  the chrome (rendered by [`layouts/base.templ`](../../framework/templates/layouts/base.templ),
  driven by [`static/js/common/box-switcher.js`](../../../static/js/common/box-switcher.js)).
  Click the chip or press `g b` from anywhere → fuzzy-search-and-pick.

These two surfaces are deliberately redundant: the page is for
**comparing** hosts side-by-side; the chip is for **jumping** mid-task
without losing your place. The user can't choose wrong.

## Scope

The Bx gear is the first gear with `Info.Scope == ScopeBoxAgnostic`
([gear/interface.go](../../framework/gear/interface.go)). That means:

- It is install-wide (one row per install, like `ScopeSystem` Home).
- It is *always* visible in the sidebar regardless of active-box
  context. `ScopeBox` gears (HAProxy, Metrics, Logs, …) are hidden
  from the sidebar when no box is selected, on the theory that they
  have nowhere meaningful to point. The Bx page is exactly the place
  the user should go to *pick* a box.
- It is non-disable-able (`Core: true`) because hiding it would orphan
  the switcher chrome.

## Status semantics

Each box's rollup dot is one of five levels, surfaced as a small
colored circle in the table, the palette, and the active-box chip:

| Level     | Meaning                                                          |
|-----------|------------------------------------------------------------------|
| `green`   | Agent reachable, no contributing warnings                        |
| `yellow`  | Agent reachable, warnings present (cert expiring soon, OS updates pending, non-critical alerts) |
| `red`     | Agent unreachable OR critical alert firing OR critical service down |
| `gray`    | Box disabled, or agent not configured                            |
| `unknown` | Never polled (first paint state — shown pulsing)                 |

v1 only checks **agent reachability** (the unauthenticated `/health`
endpoint, with a 5s timeout, on a 30s cadence). Additional contributors
(`yellow` triggers from certs/alerts/OS-updates and `red` from
service-failure signals) plug into [`status.go`](status.go)'s
`probe()` function as their data sources mature. The plan is to extract
this into a framework-level `boxhealth` service once a second gear
needs the same rollup; for now it lives in-gear so the abstraction
isn't premature.

## Routes

| Method | Path             | Permission | Purpose                                       |
|--------|------------------|------------|-----------------------------------------------|
| GET    | `/bx/`           | `bx:view`  | Render the fleet table.                       |
| GET    | `/bx/api/status` | `bx:view`  | JSON snapshot of every box's current status.  |
| GET    | `/bx/api/events` | `bx:view`  | SSE stream of `box.status` events.            |

The status endpoints are read by both the page and the switcher
palette so the dots stay in sync across the chrome.

## Permissions

| Component | Action | Description                                |
|-----------|--------|--------------------------------------------|
| `bx`      | view   | View the Bx fleet overview and box status. |

Admins always pass. For non-admin users, grant `bx:view` via
**Settings → Permissions**.

## Routing convention

Active-box selection rides on the existing `?box_id=<id>` query
parameter — the same convention every box-aware page already understood.
The chip writes that parameter on switch; the
[`InjectIntegrationStatus`](../../framework/handler/handler.go)
middleware reads it on every request and publishes the resolved
`*models.BoxConfig` to the request context, where the sidebar
renderer and chip template consume it.

Selecting a box from a *box-agnostic* page (Bx, Home, Settings) routes
the user to `/home?box_id=<id>` so they end up somewhere the new box
context is meaningful. Selecting from a *box-specific* page rewrites
the same path with the new ID, preserving the user's task.

## Future work

- **Group-by-location** toggle on the table.
- **Cards/grid view** toggle (current table is fine for power users;
  some installs prefer a wallchart).
- **Compact ≤4-box view** — current table is reasonable at small
  counts, but a "status panel" mode would feel less like a fleet
  console for one- or two-box installs.
- **Additional status contributors** — wire certs/alerts/OS-updates
  into `probe()` so the dot reflects real health, not just reachability.
- **Per-box preferred landing** — instead of always opening at
  `/home?box_id=<id>`, let the user pin "open this box at HAProxy."
- **Bulk actions** — multi-select rows and trigger e.g. a coordinated
  OS-update across a tag.
