# Web-core extraction plan

Status: **Planned, not started.**
Owner: Dave + Claude (Opus 4.7 → upgrading)
Last updated: 2026-05-28
Working directories involved: `/Users/dave/src/gearbox`, `/Users/dave/src/libation`, future `/Users/dave/src/webcore`

This document is the single source of truth for extracting shared web framework code out of `gearbox` and `libation` into a new module so all current and future sarg3nt web apps consume the same UI primitives, auth flow, session/CSRF handling, SSE transport, and form/validation helpers. It is written to be self-contained — a fresh Claude session must be able to pick up from here without re-running the audit.

---

## 1. Decisions (locked)

| # | Decision | Value |
|---|----------|-------|
| 1 | Module path | `github.com/sarg3nt/webcore` |
| 2 | Repo layout | Single repo, two top-level Go packages: `ui/` and `core/` |
| 3 | Cutover order | Gearbox first (source of truth), then libation |
| 4 | Pre-extraction cleanup | Part of extraction, not separate gearbox PRs |
| 5 | Login UI scope | Include password reset, change-password, forgot-password flows in MVP |
| 6 | SSE transport | Extract `core/transport/sse.go` + `ui/static/js/sse.js` |
| 7 | `dev_bypass` build tags | Lift gearbox's `--tags dev` loopback bypass into shared `core/auth` |

User instruction verbatim: "Write the plan in a detailed .md file in either rep, I don't care which. I need to restart VS code / Claude as there is a new version of Claude out that we need to upgrade to. But write the plan first, make sure you have all the context to get back to work on this once the restart and new Opus model are online."

---

## 2. Conversation context — what just happened

Earlier in the same session before this plan was written:

### Libation UI fixes already shipped (working in browser, not committed)

Goal was to bring libation's layout closer to gearbox. User reported three specific issues:

1. Left-hand nav scrolled with the page — wanted fixed.
2. Toasts appeared in the wrong place (bottom-right vs gearbox's top-right).
3. Sidebar collapse was visually broken.

Changes applied in libation:

- `internal/framework/ui/toast.templ` — moved container `fixed bottom-4 right-4 z-50` → `fixed top-4 right-4 z-[9999]` with `pointer-events-none`.
- `assets/static/css/sidebar.css` — full rewrite. Fixed-position rail (15rem expanded, 3.5rem collapsed), `transition: width 0.25s ease-in-out` on sidebar + `margin-left` on `#main-content`, label fade-out via opacity+width, collapsed nav-link centers icon, mobile (<md) hides sidebar + clears margin, overlay-style scrollbar.
- `internal/framework/templates/layouts/sidebar.templ` — restructured: outer `<aside>` carries only positioning, inner `flex flex-col w-full h-full` wrapper splits non-scrolling header from `<nav id="sidebar-nav-scroll">`. Added `.nav-link`, `.nav-section-heading`, `.nav-sublist` class hooks.
- `internal/framework/templates/layouts/base.templ` — dropped `<div class="flex min-h-screen">` wrapper. `<main id="main-content">` reflows via CSS margin-left. Added inline boot script in `<head>` that applies `.sidebar-collapsed` to `<html>` from localStorage before first paint (prevents flash).
- `assets/static/js/sidebar.js` — toggle now mutates `.sidebar-collapsed` on `<html>` (single source of truth for both rail width and main margin). Dropped `data-collapsed` attribute.
- `.air.toml` — `include_ext` gained `js`, `css` so static-asset edits trigger rebuild + re-embed.

### Libation UI work still pending (deferred until extraction)

User then asked for two more improvements which were deferred to avoid throwaway work:

- **Full-width fixed-top header** like gearbox (currently libation's header is inside the main flow, not fixed). Plan to port gearbox's pattern: `<header class="fixed top-0 inset-x-0 z-50">` with `pt-[57px]` on main-content.
- **Richer toast features** like gearbox's `utils/toast.js` (475 lines vs libation's 133): positions, drag-dismiss, progress bars, action buttons, ARIA, queueing.

These two land **as part of the libation cutover** to `webcore`, not as standalone libation edits.

### Dev server state caveat

During the libation UI fixes, the user's `make dev` (which runs `air`) was disrupted because the old `.air.toml` didn't watch js/css, so the running binary held stale embedded assets. To force a re-embed I `kill -9`'d the main process; this orphaned the air supervisor. I started a fresh `./tmp/main` manually so the browser session kept working. The user needs to `Ctrl-C` and re-run `make dev` in their dev terminal to restore the supervisor. The new `.air.toml` will rebuild on js/css edits going forward.

---

## 3. Architecture: target module shape

```text
github.com/sarg3nt/webcore/
├── go.mod              (module github.com/sarg3nt/webcore)
├── README.md
├── LICENSE             (MIT, matching gearbox/libation)
├── CHANGELOG.md
│
├── ui/                 ── frontend "skin"
│   ├── templ/          ── *.templ files
│   │   ├── alerts.templ
│   │   ├── badge.templ
│   │   ├── collapsible.templ
│   │   ├── doughnut.templ
│   │   ├── icons.templ
│   │   ├── info_tooltip.templ
│   │   ├── live_refresh.templ      (abstract — caller wires manualRefresh/updateSSEStatus)
│   │   ├── login.templ             (email + password + WebAuthn button + flash slot + branding slot)
│   │   ├── change_password.templ
│   │   ├── forgot_password.templ
│   │   ├── reset_password.templ
│   │   ├── metric_card.templ       (split out of gearbox metrics.templ)
│   │   ├── modal.templ
│   │   ├── settings.templ          (SettingsSection + form controls)
│   │   ├── table.templ             (deduped — gearbox currently has two copies)
│   │   ├── toast.templ
│   │   └── toggle.templ
│   ├── static/
│   │   ├── js/
│   │   │   ├── utils/
│   │   │   │   ├── api.js          (baseURL + tenantParam hook, no /api/v1 hardcode)
│   │   │   │   ├── char-limit.js
│   │   │   │   ├── dom.js
│   │   │   │   ├── formatting.js
│   │   │   │   └── toast.js        (gearbox 475-line full-featured)
│   │   │   ├── common/
│   │   │   │   ├── command-palette.js  (gearbox 636-line, fuzzy match + kbd nav)
│   │   │   │   ├── datagrid.js     (Tabulator wrapper, neutral class name)
│   │   │   │   ├── focus-trap.js
│   │   │   │   ├── header-search.js (neutral register() API)
│   │   │   │   ├── info-tooltip.js
│   │   │   │   ├── keymap.js       (renamed namespace from window.GearboxKeymap)
│   │   │   │   ├── page-header.js
│   │   │   │   ├── shortcut-help.js (shortcuts table as input)
│   │   │   │   ├── sidebar.js      (libation's new fixed-rail toggle)
│   │   │   │   └── sse.js          (EventSource wrapper + reconnect + topic filter)
│   │   │   └── charts/
│   │   │       ├── chart-defaults.js
│   │   │       ├── chart-fullscreen.js
│   │   │       └── chart-sse.js    (URL factory parameter)
│   │   └── css/
│   │       ├── utilities.css
│   │       ├── sidebar.css         (libation's new fixed-position rail)
│   │       └── components/
│   │           ├── buttons.css
│   │           ├── cards.css
│   │           ├── datagrid.css    (renamed from datagrid-unifi class)
│   │           └── modals.css
│   └── assets.go       (//go:embed static + var StaticFiles embed.FS)
│
└── core/               ── backend "spine"
    ├── auth/
    │   ├── manager.go           (Login, Logout, GetUser, IsAuthenticated, ExtendSession, GetCSRFToken, ValidateCSRFToken)
    │   ├── userstore.go         (UserStore interface — apps implement against their own DB)
    │   ├── authuser.go          (AuthUser interface — apps' models.User satisfies)
    │   ├── password.go          (HashPassword, CheckPassword, ValidatePassword, GenerateRandomPassword, GeneratePasswordEntropy)
    │   ├── csrf.go              (GenerateCSRFToken, ValidateCSRFToken extracted from manager)
    │   ├── security.go          (GenerateSecureToken, GenerateSessionToken, GenerateUUID, ValidateEmail)
    │   ├── reset.go             (RequestPasswordReset, ResetPassword)
    │   ├── change.go            (ChangePassword, SetPassword, MustChangePassword middleware gate)
    │   ├── middleware.go        (RequireAuth, RequireCSRF, RequireAdmin, RequirePasswordChange)
    │   ├── login_handler.go     (LoginHandlerBuilder takes Manager + Renderer + redirect path)
    │   ├── flash.go             (typed Flash, PutJSON/GetJSON/DeleteKey — libation's clean primitive)
    │   ├── dev_bypass_on.go     (//go:build dev — loopback bypass from gearbox)
    │   └── dev_bypass_off.go    (//go:build !dev)
    ├── webauthn/                (libation's two-file split — RP wrapper + WebAuthnUser)
    │   ├── rp.go
    │   └── user.go
    ├── middleware/
    │   ├── security_headers.go  (CSPConfig — gearbox's CSP builder + libation's local-vs-cdn toggle)
    │   └── ratelimit.go         (golang.org/x/time/rate — libation's vetted impl)
    ├── errors/
    │   └── apperror.go          (AppError + NotFound/Unauthorized/Forbidden/BadRequest/Internal/Conflict/ServiceUnavailable + WrapDatabaseError + WriteHTTPError + SanitizeError)
    ├── events/
    │   └── hub.go               (gearbox's Hub — per-subscriber buffer, drop coalescing, generic Topic filter)
    ├── transport/
    │   └── sse.go               (HTTP handler taking Hub + topic-filter func)
    ├── validation/
    │   └── validator.go         (Required, MinLength, MaxLength, Email, URL, Alphanumeric, IPAddress, Port, Hostname)
    ├── crypto/
    │   └── encryptor.go         (AES-256-GCM)
    ├── migrate/
    │   └── runner.go            (MigrateManager taking caller-supplied embed.FS)
    └── responses/
        └── envelopes.go         (ErrorResponse, SuccessResponse, ValidationResponse — only the generic ones)
```

---

## 4. Inventory: what extracts, what stays, abstractions required

### 4.1 UI — templ components

Sources are gearbox `internal/framework/ui/*.templ` and `internal/framework/templates/components/*.templ`.

| Component | Source | Extract action | Abstraction |
|-----------|--------|----------------|-------------|
| `alerts.templ` | `framework/ui/` | Direct lift | None |
| `badge.templ` | `framework/ui/` | Direct lift | None |
| `toast.templ` | `framework/ui/` | Lift + parameterize | Script src path — caller supplies or split script tag out of templ |
| `toggle.templ` | `framework/ui/` | Direct lift | None |
| `modal.templ` | `framework/ui/` | Direct lift | None |
| `collapsible.templ` | `framework/ui/` | Lift + namespace | localStorage key prefix becomes per-app — pass as template param |
| `icons.templ` | `framework/ui/` | Direct lift (~35 heroicons) | None |
| `info_tooltip.templ` | `templates/components/` | Direct lift | None |
| `doughnut.templ` | `templates/components/` | Direct lift | None (StatusDoughnut signature is already generic) |
| `settings.templ` | `templates/components/` | Direct lift | None |
| `metric_card` | Split from `templates/components/metrics.templ` | Lift `MetricCard*` only | Generic `MetricView{Label, Value, Unit}` |
| `table.templ` | Dedup `framework/ui/` and `templates/components/` first | Lift one | Document `sortTable`/`filterTable`/`exportTableCSV` JS contract (currently missing — must be written) |
| `live_refresh.templ` | Dedup same | Lift one | Caller wires `manualRefresh()` + `updateSSEStatus()` as globals or via injected JS |
| `login.templ` | New | Build using gearbox layout patterns | Branding slot + flash slot + WebAuthn-button slot |
| `change_password.templ` | New | Build | Branding + flash |
| `forgot_password.templ` | New | Build | Branding + flash |
| `reset_password.templ` | New | Build | Branding + flash |

Stays in gearbox:

- `templates/components/console.templ` (multi-session xterm.js drawer/dock — tightly bound to ConsoleManager + box switching)
- `templates/components/container_diagram.templ` (VPN-gateway / multi-container topology — `models.Container`, NetworkMode `service:` semantics)
- All `templates/pages/*.templ` (per-page templates are app-specific)
- `templates/layouts/base.templ` (gearbox's is 2763 lines, heavily coupled to box-chip/breadcrumb/gear concepts — both apps will keep their own `base.templ` and compose shared components inside it)

### 4.2 UI — JavaScript

Source root: gearbox `static/js/`.

| File | Action | Notes |
|------|--------|-------|
| `utils/toast.js` | Direct lift | 475 lines, fully self-contained, no gearbox refs |
| `utils/dom.js` | Direct lift | show/hide/toggle/setText helpers |
| `utils/formatting.js` | Direct lift | formatBytes/Number/Duration |
| `utils/char-limit.js` | Direct lift | `data-charlimit` counter |
| `utils/api.js` | Lift + abstract | Strip `/api/v1` baseURL + `box_id` query param. Take `baseURL` config + `tenantParam` hook |
| `common/focus-trap.js` | Direct lift | W3C APG impl |
| `common/keymap.js` | Lift + rename | `window.GearboxKeymap` → neutral namespace (e.g. `window.WebCore.Keymap`) |
| `common/info-tooltip.js` | Direct lift | JS counterpart of templ |
| `common/page-header.js` | Direct lift | Hoists `#page-header-source` → `#header-page-content` |
| `common/datagrid.js` | Direct lift + rename | Tabulator wrapper, drop `datagrid-unifi` class name to neutral |
| `common/header-search.js` | Lift + abstract | Strip `window.gearbox.filter`; expose neutral `register()` API |
| `common/shortcut-help.js` | Lift + abstract | Shortcuts table as input; pluggable `onEscapeFallback` chain; drop gear "edit mode" detection |
| `common/command-palette.js` | Lift + abstract | 636-line gearbox version. Strip `window.gearbox.commands` registry; expose generic registration. Boxes/gears/pages JSON islands become caller-supplied data sources. |
| `common/sidebar.js` | New, from libation | The new fixed-rail toggle (already written in libation) — promote |
| `common/sse.js` | New | Generic EventSource wrapper + reconnect + topic filter — replaces libation's 30-line `sse.js` and gearbox's `chart-sse.js` |
| `charts/chart-defaults.js` | Direct lift | Chart.js theme palette + zoom — rename namespace |
| `charts/chart-fullscreen.js` | Direct lift | DOM-class fullscreen toggle |
| `charts/chart-sse.js` | Lift + abstract | URL factory parameter instead of `/api/events?server=` |

Stays in gearbox:

- `common/box-selector.js` (`switchBox(boxID)` + `box_id` cookie)
- `common/gear-commands.js` (`window.gearbox.filter`/`commands` per-page registries — names alone are domain)
- Anything per-gear (containers/, gears/, traffic/, etc.)

### 4.3 UI — CSS

Source root: gearbox `static/css/`.

| File | Action | Notes |
|------|--------|-------|
| `utilities.css` | Direct lift | transitions, focus-ring, spinner, gradients, dark scrollbar |
| `components/buttons.css` | Direct lift | `.btn` + variants, tailwind `@apply`-based |
| `components/cards.css` | Direct lift | `.status-card*` |
| `components/modals.css` | Direct lift | `.modal-*` utilities |
| `components/datagrid.css` | Direct lift + rename | UniFi-style Tabulator skin, rename `.datagrid-unifi` → `.datagrid` |
| `sidebar.css` | New, from libation | Promote libation's new file (fixed-position rail + transitions + mobile rules) |

Stays in gearbox:

- `components/console.css` (drawer/dock for `#console-drawer`)
- `components/home.css` (Gridstack tiles)
- `firewall_config_editor.css`, `haproxy_config_editor.css` (per-gear)

### 4.4 Core — Go packages

| Package | Action | Source preference | Coupling notes |
|---------|--------|-------------------|----------------|
| `errors/` | Direct lift | Gearbox | Stdlib only. Libation has nothing here — pure addition for libation. |
| `validation/` | Direct lift | Gearbox | Stdlib only. |
| `crypto/` | Direct lift | Either (equivalent impls) | Compare gearbox vs libation, pick cleaner. |
| `migrate/` | Lift + abstract | Libation (leaner) | Take caller-supplied `embed.FS`. Both repos' impls are nearly identical. |
| `events/` | Lift + strip | Gearbox `Hub` | Strip gearbox `EventType` constants (those stay in gearbox app). Replace `ServerID` filter with generic `Topic`/`Tag`. |
| `transport/sse.go` | Build new | n/a | HTTP handler wrapping `events.Hub`, sets headers, pumps frames, takes topic-filter func. |
| `responses/` | Partial lift | Gearbox | Only `ErrorResponse`, `SuccessResponse`, `ValidationResponse` — domain DTOs stay. |
| `auth/manager.go` | Refactor + lift | Hybrid | Take libation's `SessionManager` shape (typed Flash, gorilla store); layer gearbox's CSRF + audit + password-reset + permissions on top. Define `UserStore` interface; both apps implement against their own DB. Define `AuthUser` interface (`ID`, `Email`, `IsAdmin`, `IsLocked`, `MustChangePassword`, `PasswordHash`). |
| `auth/password.go` | Direct lift | Gearbox | bcrypt + go-password-validator. |
| `auth/csrf.go` | Direct lift | Gearbox | Split out of manager.go. |
| `auth/security.go` | Direct lift | Gearbox | Token + UUID + email helpers. |
| `auth/reset.go` | Lift + abstract | Gearbox | Email send becomes interface (`EmailSender`). |
| `auth/change.go` | Lift + libation gate | Hybrid | Libation has `RequirePasswordChange` middleware gate gearbox lacks. |
| `auth/middleware.go` | Direct lift | Gearbox | `RequireAuth`, `RequireCSRF`, `RequireAdmin` + new `RequirePasswordChange`. |
| `auth/login_handler.go` | Build new | n/a | `LoginHandlerBuilder(authMgr, renderer, redirectAfterLogin)`. Renderer interface lets each app pass its branded templ wrapper. |
| `auth/flash.go` | Direct lift | Libation | Libation's typed Flash + `PutJSON`/`GetJSON`/`DeleteKey` is cleaner. |
| `auth/dev_bypass_on.go` | Direct lift | Gearbox | `//go:build dev` — issue #83 loopback bypass. |
| `auth/dev_bypass_off.go` | Direct lift | Gearbox | `//go:build !dev`. |
| `webauthn/rp.go` | Direct lift | Libation | Libation's RP wrapper split. |
| `webauthn/user.go` | Direct lift | Libation | Libation's user-model split. |
| `middleware/security_headers.go` | Lift + merge | Hybrid | Gearbox's CSP builder + sanitization + env-var extras; libation's local-vs-cdn toggle becomes a `CSPConfig` field. |
| `middleware/ratelimit.go` | Direct lift | Libation | `golang.org/x/time/rate` impl — vetted vs gearbox's hand-rolled bucket. |

Stays in gearbox:

- `framework/agent/*`, `framework/collector/*`, `framework/gear/*`
- `framework/models/{alerts,certificates,firewall,server,stats,system,traffic,settings_pages,metadata,box_config,gear,integration,permission,component}.go` (the user/passkey/audit-log types get pulled behind interfaces in `core/auth` but the concrete impls stay in gearbox)
- `framework/services/{agent_keyring,alerts,geoip,parser/csv}` and gearbox-only adapters
- `framework/handler/api_*.go`, `gears.go`, `haproxy_*.go`, `os_updates.go`, `resolve_box.go`, `console_popout.go`, `alerts.go`
- `framework/database/{alerts,backup,box_agent_keys,firewall,gears,home,log_sources,metrics_*,servers,source_stats,traffic}.go`
- `framework/middleware/permissions.go` (depends on `models.Component`/`Permission` — gearbox-specific concepts)
- `framework/services/email` (extract `SMTPConfigProvider` interface and template renderer concept later, MVP keeps it in gearbox)

---

## 5. Pre-extraction cleanup (in gearbox, done during extraction)

These are inside gearbox right now and must be untangled before the affected file moves to `webcore`. Do them as part of the same PR that extracts the file.

1. **Dedup `table.templ`** — same code in `framework/ui/` and `framework/templates/components/`. Pick canonical, delete other, update all gearbox templ imports.
2. **Dedup `live_refresh.templ`** — same situation.
3. **Split `metrics.templ`** — `MetricCard*` extract to shared `metric_card.templ`; `SystemMetrics`/`ServiceStatus`/`BackendHealthBadge` stay in gearbox as they take `models.*`.
4. **Rename JS namespaces** — `window.GearboxKeymap`, `window.GearboxCharts`, `window.gearbox.*` → neutral (`window.WebCore.*`). Keep `window.gearbox.*` in gearbox app code that registers commands/filters against the neutral primitives.
5. **Parameterize hardcoded URLs**:
    - `utils/api.js` — `/api/v1` baseURL + `box_id` param become config.
    - `charts/chart-sse.js` — `/api/events?server=` becomes URL factory.
    - `ui/toast.templ` — script src path becomes caller-supplied or split out.
6. **Namespace `collapsible-*` localStorage prefix** — per-app prefix passed in so co-hosted apps don't collide.
7. **Document and implement `table.templ` ↔ JS contract** — `sortTable`/`filterTable`/`exportTableCSV` are referenced by the template but no implementation lives in `static/js/`. Find them (may be inline in pages) or write them.

---

## 6. Execution order

### Phase 0 — Repo bootstrap

0.1 User: `gh repo create sarg3nt/webcore --public --description "Shared web framework for sarg3nt apps (UI primitives, auth, SSE, middleware)" --license MIT`.

0.2 Claude: clone to `/Users/dave/src/webcore`, `go mod init github.com/sarg3nt/webcore`, scaffold directory layout per section 3, add `README.md`, `LICENSE`, `CHANGELOG.md`, `.gitignore`, `.markdownlint.json` mirroring gearbox.

0.3 Claude: add to user's VS Code workspace at `/Users/dave/src/homelab/homelab.code-workspace` (or whichever workspace file gearbox + libation live in).

0.4 Claude: initial commit, push.

### Phase 1 — Extract zero-dependency packages

These have no coupling and can land standalone.

1.1 `core/errors/` — copy from gearbox, no changes. Add unit tests. PR1 to webcore.

1.2 `core/validation/` — copy from gearbox. Add unit tests. PR2.

1.3 `core/crypto/` — pick winner (libation or gearbox), add tests. PR3.

1.4 `core/migrate/` — take libation's leaner runner, modify to accept caller `embed.FS`. Add tests using fixture migrations. PR4.

1.5 `core/responses/` — copy `ErrorResponse`, `SuccessResponse`, `ValidationResponse` from gearbox. PR5.

1.6 `core/middleware/ratelimit.go` — libation's `x/time/rate` impl. Add tests. PR6.

1.7 `core/middleware/security_headers.go` — merge gearbox's CSP builder + libation's local/cdn toggle into one `CSPConfig`. Add tests. PR7.

### Phase 2 — Extract events + SSE transport

2.1 `core/events/Hub` — port gearbox `Hub`, strip `ServerID` filter to generic `Topic`. Add tests. PR8.

2.2 `core/transport/sse.go` — new HTTP handler. Wraps `Hub`, takes topic-filter func, sets headers, pumps frames. Add integration test using `httptest`. PR9.

2.3 `ui/static/js/common/sse.js` — new generic EventSource wrapper + reconnect + topic filter. PR9 (same PR — paired).

### Phase 3 — Extract auth + webauthn

3.1 Define `UserStore` + `AuthUser` interfaces in `core/auth/`. Document exactly what gearbox + libation need to implement. PR10.

3.2 `core/auth/flash.go` — libation's typed Flash. PR11.

3.3 `core/auth/password.go`, `csrf.go`, `security.go` — direct lift from gearbox. PR12.

3.4 `core/auth/manager.go` — Login, Logout, GetUser, IsAuthenticated, ExtendSession + CSRF helpers. Built on `UserStore` interface. PR13.

3.5 `core/auth/middleware.go` — RequireAuth, RequireCSRF, RequireAdmin, RequirePasswordChange. PR14.

3.6 `core/auth/reset.go`, `change.go` — with `EmailSender` interface for reset email. PR15.

3.7 `core/auth/login_handler.go` — `LoginHandlerBuilder` with `Renderer` interface. PR16.

3.8 `core/auth/dev_bypass_on.go`, `dev_bypass_off.go` — lift gearbox files behind `//go:build dev` tag. PR17.

3.9 `core/webauthn/rp.go`, `user.go` — lift libation's split. PR18.

### Phase 4 — Extract UI templ components

4.1 `ui/templ/` zero-dependency batch — `alerts`, `badge`, `toggle`, `modal`, `collapsible` (parameterized prefix), `icons`, `info_tooltip`, `doughnut`, `settings`. PR19.

4.2 `ui/templ/toast.templ` + `ui/static/js/utils/toast.js` — paired. PR20.

4.3 Dedup gearbox's two `table.templ` copies, lift canonical to `ui/templ/`. Document JS contract, write/find sortTable/filterTable/exportTableCSV. PR21.

4.4 Dedup `live_refresh.templ`, lift abstract version. PR22.

4.5 Split gearbox `metrics.templ`, lift `metric_card.templ`. PR23.

4.6 Lift sidebar — promote libation's new `sidebar.templ` + `sidebar.css` + `sidebar.js`. PR24.

4.7 Build `login.templ`, `change_password.templ`, `forgot_password.templ`, `reset_password.templ` — using gearbox layout patterns. PR25.

### Phase 5 — Extract UI JS + CSS

5.1 `ui/static/js/utils/` — `dom`, `formatting`, `char-limit`, abstracted `api.js`. PR26.

5.2 `ui/static/js/common/` — `focus-trap`, `keymap` (renamed), `info-tooltip`, `page-header`, `datagrid` (renamed), abstracted `header-search`, abstracted `shortcut-help`, abstracted `command-palette`. PR27.

5.3 `ui/static/js/charts/` — `chart-defaults`, `chart-fullscreen`, abstracted `chart-sse`. PR28.

5.4 `ui/static/css/` — `utilities`, `components/{buttons,cards,modals,datagrid}`. PR29.

### Phase 6 — Gearbox cutover

6.1 Add `webcore` as a `require` in `gearbox/go.mod`, optionally with `replace github.com/sarg3nt/webcore => ../webcore` while iterating locally.

6.2 Per-package cutover, one at a time, smallest-blast-radius first: `errors` → `validation` → `crypto` → `migrate` → `responses` → `middleware/ratelimit` → `middleware/security_headers` → `events` → `transport/sse` → `auth` → `webauthn` → UI templ batch → UI JS batch → UI CSS batch.

6.3 Each cutover step: delete the now-duplicated file from gearbox, update all imports, run `go build ./... && go test ./... && make e2e` (or whatever gearbox's full test target is), commit, PR.

6.4 After cutover, **gearbox base.templ** stays in gearbox (still has gearbox-specific box-chip / breadcrumb / gear concepts), but its internal references to ui/* components now resolve via `webcore/ui/templ/`.

### Phase 7 — Libation cutover

7.1 Same `go.mod` add + optional `replace`.

7.2 Per-package cutover, same order. Critically: after auth cutover, libation's `auth.SessionManager` becomes a thin satisfier of `webcore/core/auth.UserStore`.

7.3 **Apply deferred UI work as part of this phase:**

- Port gearbox's full-width fixed-top header pattern — header becomes `<header class="fixed top-0 inset-x-0 z-50">` plus `pt-[57px]` on `#main-content`.
- Adopt richer `toast.js` from `webcore/ui/static/js/utils/toast.js` (replaces libation's stub).
- Adopt richer `command-palette.js` from `webcore`.

7.4 Run libation's full test + e2e + a11y + visual targets, fix any regressions.

### Phase 8 — Tag v0.1.0

8.1 Drop `replace` directives in both gearbox + libation.

8.2 Tag `webcore` v0.1.0.

8.3 Bump both apps to consume the tag.

8.4 Document the upgrade workflow in `webcore/README.md`: bump tag, update apps, run their full tests, ship.

---

## 7. Open questions / risks

1. **Database table coupling.** `auth` needs `users`, `audit_logs`, `password_resets`, `sessions`, `passkeys`, `smtp_settings`. Both apps have these (gearbox via `database.go`, libation via narrower files). Plan assumes apps own the tables and satisfy `UserStore` against them. Need to confirm gearbox's `users` table schema and libation's match closely enough that the interface contract works for both without each app needing schema migrations. **Action:** during PR10, write out the exact `UserStore` methods + types, validate both apps' tables against it before writing the interface.

2. **`models.User` divergence.** Both apps have a `models.User`. They need to satisfy `AuthUser` interface. The interface design (above) is intentionally narrow (`ID`, `Email`, `IsAdmin`, `IsLocked`, `MustChangePassword`, `PasswordHash`) — anything richer goes in app-side methods. **Action:** make sure libation's `User` actually has all six fields before declaring the interface. Quick check during PR10.

3. **CSP differences.** Gearbox CSP is strict and env-var-driven; libation has a simpler local-vs-cdn flag. Merging them in one `CSPConfig` is straightforward in theory but the field semantics need to match production needs in both apps. **Action:** read both impls' production configurations before designing `CSPConfig`.

4. **Email sender abstraction.** `auth/reset.go` calls into `services/email.SendPasswordResetEmail`. Gearbox's email package reads SMTP settings from `db.GetSMTPSettings()`. Libation doesn't have password reset yet — it doesn't have an email package. **Action:** abstract via `EmailSender` interface during PR15 (`SendPasswordResetEmail(to, token, baseURL) error` etc.). Gearbox implements with its existing email service. Libation either implements with a minimal SMTP client or stubs the feature.

5. **Command palette data model.** Gearbox's `command-palette.js` consumes JSON islands for boxes/gears/pages plus `window.gearbox.commands` for per-page actions. Generalising requires defining a stable shape (e.g. `{label, group, action, href, kbd}`) and a registration API (`WebCore.CommandPalette.register(group, items)`). **Action:** spec the API in PR27 design notes before lifting.

6. **Build-time tailwind.** Currently both apps use Tailwind via CDN (`https://cdn.tailwindcss.com`). With shared components, classnames are duplicated across two repos but Tailwind itself runs in each. **Action:** no change — both apps keep their CDN/local Tailwind setup. The shared CSS files in `webcore/ui/static/css/` use `@apply` directives that resolve at runtime when Tailwind's JIT sees them in the served HTML. Verify this works across an `embed.FS` boundary before committing.

7. **Local development workflow.** During cutover, contributors will want `webcore` edits to be visible in gearbox + libation without publishing. The `replace github.com/sarg3nt/webcore => ../webcore` directive handles this, and `air` already watches Go source. But static asset edits in `webcore` need to be re-embedded into webcore's binary then surfaced to consumers. Since static assets are served from `webcore/ui/static/`, and each app imports them via `webcore/ui.StaticFiles`, the consumer apps need their air to also re-embed when webcore static files change. **Action:** document in `webcore/README.md`. Simplest workflow: when editing `ui/static/*`, run `touch` on the consumer app's go file to force air rebuild + re-embed.

---

## 8. How to resume after restart

If you're a fresh Claude session opening this file: read sections 1–4 (decisions + context + architecture + inventory) in full. Section 5 (cleanup) + 6 (execution) are the operational plan; start at **Phase 0** unless the user says otherwise.

Before writing any code, run these sanity checks:

```bash
# 1. Confirm webcore repo state
ls /Users/dave/src/webcore 2>/dev/null && echo "Repo exists, check phase progress"

# 2. Check libation deferred work hasn't been done yet
grep -q 'fixed top-0 inset-x-0' /Users/dave/src/libation/internal/framework/templates/layouts/base.templ \
  && echo "Header already ported — check git log for current phase"

# 3. Check gearbox cleanup progress
ls /Users/dave/src/gearbox/gearbox/internal/framework/templates/components/table.templ \
   /Users/dave/src/gearbox/gearbox/internal/framework/ui/table.templ 2>/dev/null \
  && echo "table.templ still duplicated — Phase 4.3 not done"

# 4. Confirm libation dev server state
lsof -i :3001 -P 2>/dev/null | grep LISTEN \
  || echo "Libation dev not running — user may need to restart 'make dev'"

# 5. Verify libation UI fixes from prior session are still in tree
grep -q 'top-4 right-4' /Users/dave/src/libation/internal/framework/ui/toast.templ \
  && echo "Libation toast position fix intact"
grep -q 'sidebar-collapsed' /Users/dave/src/libation/assets/static/css/sidebar.css \
  && echo "Libation sidebar.css fix intact"
```

If any of those flag unexpected state, stop and ask the user what changed since this plan was written.

### Pending non-extraction work to remember

- Libation UI deferred work (full-width fixed-top header + richer toast/command-palette) lands in **Phase 7.3**, not standalone.
- Libation UI fixes from the same session as this plan (sidebar fixed, toast top-right, collapse smooth, `.air.toml` includes js/css) are **uncommitted** — user may want them committed separately before extraction starts.

### What to ask the user first when you resume

1. Has the `webcore` GitHub repo been created? If yes, has it been cloned to `/Users/dave/src/webcore`?
2. Should the libation UI fixes from this session be committed to libation before any webcore work starts, or rolled into the libation cutover (Phase 7)?
3. Any change to the decisions in section 1 since this plan was written?
4. Continue at Phase 0 or skip to a later phase if any progress was made offline?

---

## 9. Appendix: source-of-truth file inventory

### Gearbox (full superset)

```text
/Users/dave/src/gearbox/gearbox/internal/framework/
├── auth/                  (8 files, ~2300 LOC)
├── middleware/            (726 LOC)
├── errors/                (no libation equivalent)
├── events/                (gearbox Hub — richer than libation Bus)
├── validation/            (no libation equivalent)
├── responses/             (heavy domain coupling — partial lift)
├── services/crypto/       (parity with libation)
├── services/email/        (gearbox only for now)
├── database/migrate_manager.go
├── ui/{alerts,badge,collapsible,icons,live_refresh,modal,table,toast,toggle}.templ
└── templates/components/{console,container_diagram,doughnut,helpers.go,info_tooltip,live_refresh,metrics,settings,table}.templ
/Users/dave/src/gearbox/gearbox/static/
├── js/utils/{api,char-limit,dom,formatting,toast}.js
├── js/common/{box-selector,command-palette,datagrid,focus-trap,gear-commands,header-search,info-tooltip,keymap,page-header,shortcut-help}.js
├── js/charts/{chart-defaults,chart-fullscreen,chart-sse}.js
└── css/{utilities.css, components/{buttons,cards,console,datagrid,home,modals}.css, firewall_config_editor.css, haproxy_config_editor.css}
```

### Libation (narrower, occasionally cleaner shape)

```text
/Users/dave/src/libation/internal/framework/
├── auth/                  (4 files, 680 LOC — cleaner SessionManager + WebAuthn split)
├── middleware/            (473 LOC, includes RequirePasswordChange)
├── database/              (32-line database.go — cleaner than gearbox's 1137-line file)
├── ui/{badge,icons,modal,toast,toggle}.templ
└── templates/{layouts/{base,sidebar,header}.templ, pages/*.templ}
/Users/dave/src/libation/assets/static/
├── js/{command-palette,dashboard-chart,event-bus,library-grid,profile-menu,sidebar,sse,template-preview,toast,webauthn}.js
└── css/{sidebar.css (new), utilities.css, vendor/}
```
