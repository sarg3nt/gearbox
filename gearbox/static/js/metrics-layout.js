/**
 * metrics-layout.js — wires the GridStack-driven chart grid on the
 * Metrics page (issue #103). Lives in a separate file from
 * metrics.templ's existing inline script because (a) it interacts
 * with a different concern (layout, not data) and (b) inlining a
 * second large script in the templ makes the file unreadable.
 *
 * Responsibilities:
 *   - Initialise GridStack on #charts-grid in read-only mode.
 *   - Remove tiles whose data-source is capability-hidden so the
 *     grid auto-compacts (no permanent gaps on HAProxy-less hosts).
 *   - Toggle edit mode via #metrics-edit-toggle; persist on exit.
 *   - Fetch saved layout on load; apply via GridStack's `load()`.
 *   - Reset-to-default button drops the saved row and reloads.
 *
 * Vanilla JS, no framework. Mirrors the Home gear's home.js
 * structure (init / edit-mode / persist) so a reader who knows one
 * file can read the other without remapping conventions.
 */
(function () {
  "use strict";

  // Wait for GridStack to load + DOM to be ready. Both scripts use
  // `defer`, so this just confirms; bailing if either is missing
  // means the page degrades to "tiles render at their default
  // positions, no drag" rather than throwing.
  if (typeof GridStack === "undefined") {
    console.warn("GridStack not loaded; metrics layout will be static.");
    return;
  }

  const grid = document.getElementById("charts-grid");
  if (!grid) return;

  /** The chart cards we know about. Order matches the templ's
   *  default-position attributes; capability-gated source cards live
   *  at the tail so the baseline 7 always survive even on minimal
   *  hosts. The strings are the gs-id attribute values the templ
   *  emits — i.e. the same "card-X" the existing chart-render code
   *  targets. */
  const KNOWN_TILES = [
    "card-cpu",
    "card-memory",
    "card-network",
    "card-response-time",
    "card-health",
    "card-errors",
    "card-sessions",
    "card-nginx",
    "card-apache",
    "card-caddy",
    "card-traefik",
  ];

  /** getServerID matches metrics.templ's inline helper of the same
   *  name: prefer #server-select (multi-server mode), fall back to
   *  #default-server-id (single-server mode), then to the URL.
   *  Missing this single-server fallback meant the layout endpoints
   *  silently no-op'd on single-box installs — see review on PR #104.
   */
  function getServerID() {
    const sel = document.getElementById("server-select");
    if (sel && sel.value) return sel.value;
    const fallback = document.getElementById("default-server-id");
    if (fallback && fallback.value) return fallback.value;
    const match = window.location.pathname.match(/^\/servers\/([^/]+)/);
    return match ? match[1] : "";
  }

  // 12-column grid matches the templ's default coordinates (half-
  // width tiles are gs-w=6, the full-width Sessions tile is gs-w=12).
  //
  // Margins set to 6 on every side → 12px visible gap between
  // tiles on both axes. Matches the KPI band's Tailwind gap-3
  // (12px) directly above the grid, so the whole metrics page
  // reads as one consistently-spaced surface rather than two
  // grids with different rhythms.
  //
  // ALSO: keep getCellHeight(false) (not cellHeight()) anywhere we
  // need the cell height — the bare cellHeight() getter side-
  // effects opts.cellHeight to a width-derived value, which then
  // makes GridStack's generated top/height CSS rules drift away
  // from the configured 80 every time we read.
  const gs = GridStack.init(
    {
      column: 12,
      cellHeight: 80,
      marginTop: 6,
      marginBottom: 6,
      marginLeft: 6,
      marginRight: 6,
      float: false,
      staticGrid: true, // read-only until edit mode toggles
      acceptWidgets: false,
      animate: true,
    },
    grid,
  );

  // suppressChange guards against the change-handler firing during
  // setup (load(), removeWidget for capability flips, etc.) and
  // accidentally persisting the wrong layout. Mirrors home.js's
  // pattern — same name on purpose so a future contributor reading
  // both files sees the same idiom.
  let suppressChange = true;

  /** captureLayout returns the current grid state in the same shape
   *  the PATCH endpoint expects — array of {id, x, y, w, h}. We use
   *  gs.save(false) (no node data, just positions); the dashboard
   *  doesn't need the full GridStack node objects. */
  function captureLayout() {
    return gs.save(false);
  }

  /** updateContainerHeight pins the grid's min-height to the bottom
   *  edge of the tallest tile in pixels. GridStack normally auto-
   *  sizes the container via its own engine, but with staticGrid +
   *  capability-driven removeWidget() calls the auto-sizing can lag
   *  the actual layout, causing whatever section comes after the
   *  grid to overlap the bottom tiles (issue reported in PR #104
   *  review). Computing manually after every load/capability flip
   *  closes the gap.
   *
   *  Math: GridStack positions each tile at top = y*cellHeight + margin/2
   *  with height = h*cellHeight - margin. Bottom edge of the last
   *  row = (y+h)*cellHeight. Adding the margin once at the end
   *  matches GridStack's own positioning so the spacing below the
   *  last row equals the spacing between rows. */
  function updateContainerHeight() {
    // CAREFUL: GridStack's cellHeight() — note the parens — is an
    // implicit setter when called as a getter. It re-computes
    // opts.cellHeight from cellWidth + (marginVertical -
    // marginHorizontal) which silently overrides whatever value
    // we passed to init. That breaks the row-spacing math because
    // the styled `top` rules use opts.cellHeight, so every call
    // here was drifting cellHeight away from 80 toward
    // (containerWidth/12 + 12). getCellHeight(false) is the
    // non-side-effecting getter that returns opts.cellHeight as
    // it currently is.
    const ch = gs.getCellHeight(false);
    if (!ch) return;
    let maxBottom = 0;
    (gs.engine.nodes || []).forEach(function (n) {
      const bottom = (n.y || 0) + (n.h || 0);
      if (bottom > maxBottom) maxBottom = bottom;
    });
    if (maxBottom > 0) {
      grid.style.minHeight = maxBottom * ch + "px";
    }
  }

  /** applyCapabilityHiding inspects each tile's inner card for the
   *  `.hidden` class set by the existing applyCapabilities() pass
   *  (which runs from metrics.templ's inline script). Capability-
   *  hidden tiles are removed from the grid without DOM removal so
   *  visible tiles compact upward. When a source flips Available
   *  later, makeCapabilityVisible() puts them back.
   *
   *  Called after every applyCapabilities() run (the inline script
   *  emits a `metrics:capabilities-applied` CustomEvent we listen
   *  for below). */
  function applyCapabilityHiding() {
    KNOWN_TILES.forEach(function (id) {
      const item = grid.querySelector(`.grid-stack-item[gs-id="${id}"]`);
      if (!item) return;
      const card = item.querySelector(`#${id}`);
      if (!card) return;

      // The inner card carries `.hidden` when capability gating
      // says the source isn't Available. Pull the surrounding
      // grid-stack-item out of the engine to free its slot.
      const isCapHidden = card.classList.contains("hidden");
      const isInGrid = !!item.gridstackNode;

      if (isCapHidden && isInGrid) {
        gs.removeWidget(item, false); // keep DOM, drop slot
      } else if (!isCapHidden && !isInGrid) {
        gs.makeWidget(item);
      }
    });
    updateContainerHeight();
  }

  /* ---- Layout persistence ---- */

  /** loadSavedLayout fetches the user's saved layout for the
   *  current box from /api/{boxID}/metrics/layout and applies it
   *  via GridStack.load(). 204 = no saved layout → keep the
   *  template's default positions. Non-2xx (auth, server error)
   *  also falls back to defaults; failing open keeps the page
   *  usable when the layout endpoint is briefly unreachable. */
  async function loadSavedLayout() {
    const serverID = getServerID();
    if (!serverID) return;
    try {
      const res = await fetch(`/api/${serverID}/metrics/layout`);
      if (res.status === 204 || !res.ok) return; // use template defaults
      const layout = await res.json();
      if (!Array.isArray(layout) || layout.length === 0) return;
      gs.load(layout);
    } catch (err) {
      console.debug("metrics layout load failed; using defaults", err);
    }
  }

  /** saveLayout PATCHes the current grid state. Two call sites:
   *  - drag/resize during edit mode → debounced 400 ms so a
   *    multi-tile drag flushes once, not N times.
   *  - edit-mode exit → flush=true to bypass the debounce; without
   *    this a fast close-tab right after "Done editing" could drop
   *    the layout the user just confirmed.
   *
   *  saveTimer is module-scoped so resetLayout() can cancel it
   *  before issuing the DELETE — otherwise a pending PATCH could
   *  fire after the reset and re-create the row the user just
   *  threw away. */
  let saveTimer = null;
  let savesSuppressed = false;

  async function patchLayout() {
    const serverID = getServerID();
    if (!serverID) return;
    try {
      await fetch(`/api/${serverID}/metrics/layout`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(captureLayout()),
      });
    } catch (err) {
      console.warn("metrics layout save failed", err);
    }
  }

  function saveLayout(opts) {
    if (savesSuppressed) return;
    const flush = !!(opts && opts.flush);
    if (saveTimer) {
      clearTimeout(saveTimer);
      saveTimer = null;
    }
    if (flush) {
      // Don't await — caller (edit-mode exit handler) is
      // synchronous; the PATCH fires immediately and any
      // failure surfaces in the console.
      void patchLayout();
      return;
    }
    saveTimer = setTimeout(function () {
      saveTimer = null;
      void patchLayout();
    }, 400);
  }

  /** resetLayout DELETEs the saved row and reloads the page so the
   *  template defaults take effect. Suppresses further saves +
   *  cancels any in-flight debounce before issuing the DELETE so a
   *  pending PATCH can't race the reset and resurrect the row. */
  async function resetLayout() {
    const serverID = getServerID();
    if (!serverID) return;
    savesSuppressed = true;
    if (saveTimer) {
      clearTimeout(saveTimer);
      saveTimer = null;
    }
    try {
      await fetch(`/api/${serverID}/metrics/layout`, { method: "DELETE" });
    } catch (err) {
      console.warn("metrics layout reset failed", err);
    }
    window.location.reload();
  }

  /* ---- Edit-mode toggle ---- */

  const editBtn = document.getElementById("metrics-edit-toggle");
  if (editBtn) {
    editBtn.addEventListener("click", function () {
      // `staticGrid` is the inverse of "editable" — when staticGrid
      // is true, drag/resize are off. Toggle by inspecting the
      // class we add to the grid container; that keeps the visual
      // affordance + the GridStack state in sync.
      const enteringEdit = !grid.classList.contains("metrics-grid-edit");
      grid.classList.toggle("metrics-grid-edit", enteringEdit);
      gs.setStatic(!enteringEdit);

      // aria-pressed drives the cog button's visual state via the
      // .gear-cog-btn[aria-pressed="true"] CSS rule — shared with
      // Home's edit toggle so the affordance reads consistently.
      editBtn.setAttribute("aria-pressed", enteringEdit ? "true" : "false");

      // Reveal/hide edit-only controls (Reset button, future
      // affordances). Mirrors the Home gear's .home-edit-only
      // pattern so the visual idiom reads the same across pages.
      document.querySelectorAll(".metrics-edit-only").forEach(function (el) {
        el.classList.toggle("hidden", !enteringEdit);
      });

      // Persist on exit. Flush bypasses the debounce so a close-
      // tab right after clicking "Done editing" can't drop the
      // layout. Entering edit mode is a no-op for persistence —
      // only the resulting layout matters.
      if (!enteringEdit) saveLayout({ flush: true });
    });
  }

  /* ---- Reset button ---- */

  const resetBtn = document.getElementById("metrics-reset-layout");
  if (resetBtn) {
    resetBtn.addEventListener("click", async function () {
      // Inline confirm rather than a modal — this is destructive
      // but trivially recoverable (just rearrange tiles again).
      if (!window.confirm("Reset the metrics layout for this box to defaults?")) return;
      await resetLayout();
    });
  }

  /* ---- Persistence hook ---- */

  gs.on("change", function () {
    // Recompute container height on every layout change so a drag
    // that bumps a tile down doesn't leave the section below
    // overlapping. Cheap (O(tiles)) and fires regardless of save
    // suppression.
    updateContainerHeight();
    if (suppressChange) return;
    saveLayout();
  });

  /* ---- Re-apply capability-driven visibility on every cap update ---- */

  // The inline script in metrics.templ dispatches a CustomEvent
  // after each applyCapabilities() run. Listening here keeps the
  // capability-hiding logic localised — neither file needs to call
  // the other's internals.
  window.addEventListener("metrics:capabilities-applied", function () {
    suppressChange = true;
    applyCapabilityHiding();
    setTimeout(function () {
      suppressChange = false;
    }, 50);
  });

  /* ---- Init ---- */

  // Initial fetch on first paint. Suppress the change-event so
  // GridStack.load() doesn't PATCH the layout we just received
  // back to the server.
  (async function init() {
    await loadSavedLayout();
    applyCapabilityHiding(); // also calls updateContainerHeight()
    setTimeout(function () {
      suppressChange = false;
    }, 100);
  })();
})();
