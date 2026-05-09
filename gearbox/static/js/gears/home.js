/**
 * Home dashboard controller — wires the gridstack-rendered tile board to
 * the /home/api/* CRUD endpoints. Vanilla JS, no framework.
 *
 * Responsibilities:
 *   - Initialise GridStack on #home-grid (read-only by default).
 *   - Toggle edit mode via the #home-edit-toggle button.
 *   - Persist drag/resize via PATCH /home/api/tiles/{id}.
 *   - Open/close the "Add tile" modal and POST new tiles.
 *   - Wire the per-tile delete button (visible only in edit mode).
 *   - Wire the "Make default page" button to /home/api/landing-path.
 *
 * Status-dot polling lives in Phase 4 (server-side worker + SSE) — for now
 * each tile renders an "unknown" grey dot.
 */
(function () {
  "use strict";

  // Hoist the page header content (title / search / controls) into the
  // shared top bar slot. Matches the pattern used by the Logs and HAProxy
  // gear pages — see haproxy_gear_settings.templ.
  function mountPageHeader() {
    const source = document.getElementById("page-header-source");
    const target = document.getElementById("header-page-content");
    if (!source || !target) return;
    while (source.firstChild) target.appendChild(source.firstChild);
    source.remove();
  }
  mountPageHeader();

  const grid = document.getElementById("home-grid");
  if (!grid) return;

  const boardId = grid.dataset.boardId;

  /** Cell sizing. The grid uses a fixed pixel cell height/width (so a
   *  W=2 tile is always ~152px wide regardless of viewport), and the
   *  column count grows with the container so wider screens fit more
   *  tiles per row instead of stretching each one. With a hard
   *  `column: 12` like before, doubling the viewport doubled every
   *  tile's size — see issue: "tiles grow in size and they should not". */
  const TARGET_CELL_PX = 76;     // tile cell width target (close to old default at 1280px viewport)
  const MIN_COLUMNS = 6;         // narrowest sane grid; below this we stop adding columns
  const MAX_COLUMNS = 60;        // safety ceiling on ultrawide displays

  // computeColumns returns the column count for the current grid container
  // width. Aim for ~76px per cell, clamped between MIN/MAX.
  function computeColumns() {
    const w = grid.clientWidth || window.innerWidth;
    const cols = Math.floor(w / TARGET_CELL_PX);
    return Math.max(MIN_COLUMNS, Math.min(MAX_COLUMNS, cols));
  }

  // Gridstack's vendor CSS only ships percentage-width rules for `gs-1`
  // and `gs-12` (matching the historical default column counts). With
  // dynamic column counts we'd otherwise see tiles render at 0px wide
  // for any other count. Generate the missing rules on demand and
  // memoize them so we never re-emit the same column-count's stylesheet.
  const injectedColCSS = new Set();
  function ensureColumnCSS(n) {
    if (n === 1 || n === 12 || injectedColCSS.has(n)) return;
    const css = [];
    for (let i = 1; i <= n; i++) {
      const pct = ((i / n) * 100).toFixed(4);
      // width for tiles spanning i columns
      css.push(`.gs-${n} > .grid-stack-item[gs-w="${i}"]{width:${pct}%}`);
      // left for tiles starting at column i (skip i=n which would be off-grid)
      if (i < n) {
        css.push(`.gs-${n} > .grid-stack-item[gs-x="${i}"]{left:${pct}%}`);
      }
    }
    const style = document.createElement("style");
    style.dataset.gsDynamicCols = String(n);
    style.textContent = css.join("");
    document.head.appendChild(style);
    injectedColCSS.add(n);
  }

  /** Initialize gridstack with the dynamic column count.
   *
   *  - `column: <computed>` so cell width stays ~76px regardless of
   *    viewport (tiles don't grow when the window grows).
   *  - `float: false` so a tile dragged on a wide viewport doesn't
   *    leave gaps when the viewport narrows — gridstack auto-compacts.
   *  - `columnOpts.layout: "list"` so when the column count changes
   *    (resize, narrow viewport), tiles re-flow into the available
   *    columns in their stored order rather than overflowing or
   *    being scaled. The user's drag arrangement is preserved
   *    within a given viewport size; on resize we treat saved
   *    positions as ordering hints, not absolute coordinates. */
  const initialCols = computeColumns();
  ensureColumnCSS(initialCols);
  const gs = GridStack.init(
    {
      column: initialCols,
      cellHeight: 80,
      margin: 16,
      float: false,
      staticGrid: true, // Read-only until the user enters edit mode.
      acceptWidgets: false,
      animate: true,
      columnOpts: { layout: "list" },
    },
    grid,
  );

  /** Track of tiles whose layout the server already knows about, so the
   *  initial gridstack pass doesn't re-PATCH every existing tile. Declared
   *  before the reflow/column-change helpers since they read it. */
  let suppressChange = true;
  setTimeout(() => {
    suppressChange = false;
  }, 100);

  /** reflowTiles re-runs the "list" auto-flow over the current tile
   *  positions. Used at init (saved x,y from a wider viewport could
   *  overflow the current grid) and on resize (column count changed).
   *  The reflow is layout-only and shouldn't be persisted back to the
   *  server — wrapped in `suppressChange` so the change handler that
   *  PATCHes user drags doesn't fire for viewport-driven moves. */
  function reflowTiles() {
    suppressChange = true;
    gs.compact("list", true);
    setTimeout(() => {
      suppressChange = false;
    }, 50);
  }

  // Force one reflow on init. Saved gs-x/gs-y are rendered verbatim
  // from the server, so a position written when the viewport was
  // wider can overflow the current grid (issue: "a tile is cut off
  // and should have wrapped to the next row"). compact() drops them
  // back into valid columns in their stored DOM order.
  reflowTiles();

  /** changeColumns wraps gs.column() to switch column count without
   *  persisting the resulting position changes back to the server. */
  function changeColumns(next) {
    if (next === gs.getColumn()) return;
    ensureColumnCSS(next);
    suppressChange = true;
    gs.column(next, "list");
    setTimeout(() => {
      suppressChange = false;
    }, 50);
  }

  // Recompute column count when the grid container resizes. ResizeObserver
  // fires on layout-driven resizes (sidebar collapse, viewport change,
  // even programmatic window resizes) — more reliable than `window.resize`,
  // which doesn't fire in some embedded contexts. Debounced so the handler
  // doesn't thrash during a continuous drag.
  let resizeTimer = null;
  const resizeObserver = new ResizeObserver(() => {
    if (resizeTimer) clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => {
      changeColumns(computeColumns());
    }, 120);
  });
  resizeObserver.observe(grid);

  // findNextSlot scans gridstack's current nodes for the first empty
  // (x, y) where a w×h tile fits, top-to-bottom and left-to-right.
  // Honors the live column count (which now varies with viewport
  // width). Without this a new tile POSTed at (0, 0) would collide
  // with the existing tile there and gridstack would stack them
  // vertically on reload.
  function findNextSlot(w, h) {
    const cols = gs.getColumn();
    if (w > cols) w = cols;
    const occupied = new Set();
    let maxY = 0;
    (gs.engine.nodes || []).forEach((n) => {
      for (let dy = 0; dy < n.h; dy++) {
        for (let dx = 0; dx < n.w; dx++) {
          occupied.add((n.x + dx) + "," + (n.y + dy));
        }
      }
      if (n.y + n.h > maxY) maxY = n.y + n.h;
    });
    const limit = maxY + h + 1;
    for (let y = 0; y < limit; y++) {
      for (let x = 0; x <= cols - w; x++) {
        let fits = true;
        for (let dy = 0; dy < h && fits; dy++) {
          for (let dx = 0; dx < w && fits; dx++) {
            if (occupied.has((x + dx) + "," + (y + dy))) fits = false;
          }
        }
        if (fits) return { x, y };
      }
    }
    return { x: 0, y: maxY };
  }

  /** Persist layout changes back to the server. */
  gs.on("change", (_event, items) => {
    if (suppressChange || !items) return;
    items.forEach((item) => {
      const id = parseInt(item.id, 10);
      if (!id) return;
      patchTileLayout(id, item.x, item.y, item.w, item.h);
    });
  });

  /* --- Edit mode --- */

  const editBtn = document.getElementById("home-edit-toggle");
  if (editBtn) {
    editBtn.addEventListener("click", () => {
      const isStatic = grid.classList.toggle("home-grid-edit") === false;
      gs.setStatic(isStatic);
      const onLabel = editBtn.querySelector(".home-edit-on-label");
      const offLabel = editBtn.querySelector(".home-edit-off-label");
      if (onLabel && offLabel) {
        onLabel.classList.toggle("hidden", isStatic);
        offLabel.classList.toggle("hidden", !isStatic);
      }
      // Show/hide the per-tile delete and edit buttons.
      grid
        .querySelectorAll("[data-tile-delete], [data-tile-edit]")
        .forEach((btn) => {
          btn.classList.toggle("hidden", isStatic);
        });
      // Reveal management buttons in the top bar (Board, Export, Import,
      // Make-default) only while in edit mode.
      document.querySelectorAll(".home-edit-only").forEach((el) => {
        el.classList.toggle("hidden", isStatic);
      });
      // In edit mode, swallow the launcher click so a misclick on a tile
      // doesn't navigate away while you're trying to drag.
      grid.querySelectorAll("[data-tile-link]").forEach((a) => {
        if (!isStatic) a.classList.add("home-disable-click");
        else a.classList.remove("home-disable-click");
      });
    });
  }

  /* --- Per-tile delete --- */

  grid.addEventListener("click", async (ev) => {
    const target = ev.target;
    if (!(target instanceof Element)) return;
    if (target.closest(".home-disable-click")) {
      ev.preventDefault();
      return;
    }
    // Edit pencil → open the modal pre-populated for this tile.
    const editBtnEl = target.closest("[data-tile-edit]");
    if (editBtnEl) {
      ev.preventDefault();
      ev.stopPropagation();
      const node = editBtnEl.closest(".grid-stack-item");
      if (node) showEditModal(node);
      return;
    }
    const deleteBtn = target.closest("[data-tile-delete]");
    if (!deleteBtn) return;
    ev.preventDefault();
    ev.stopPropagation();
    const tileId = parseInt(deleteBtn.dataset.tileDelete, 10);
    if (!tileId) return;
    const confirmed = await showConfirmDialog({
      title: "Delete tile",
      message: "Remove this tile from the board?",
      confirmText: "Delete",
      type: "danger",
    });
    if (!confirmed) return;
    await deleteTile(tileId);
    const node = deleteBtn.closest(".grid-stack-item");
    if (node) gs.removeWidget(node, true);
    // If we just emptied the board, reload to surface the empty state.
    if (gs.engine.nodes.length === 0) window.location.reload();
  });

  /* --- Icon picker (selfh.st/icons browser) ---
     Fetches /home/api/icons once per session, filters client-side as
     the user types, and on selection writes the icon URL into the
     [name="icon_url"] input on the add-tile form. Designed to stack
     on top of the add-tile modal — z-index higher, doesn't close the
     parent modal when dismissed. */

  const iconPicker = document.getElementById("home-icon-picker");
  const iconSearchInput = iconPicker?.querySelector("[data-icon-picker-search]");
  const iconGrid = iconPicker?.querySelector("[data-icon-picker-grid]");
  const iconStatus = iconPicker?.querySelector("[data-icon-picker-status]");
  const ICON_RENDER_LIMIT = 96; // virtualization-lite — render a window, not all 2,700+

  let iconLibraryCache = null;
  let iconFilterTimer = null;

  async function loadIconLibrary() {
    if (iconLibraryCache) return iconLibraryCache;
    try {
      const res = await fetch("/home/api/icons", { credentials: "same-origin" });
      if (!res.ok) return (iconLibraryCache = []);
      iconLibraryCache = await res.json();
    } catch (_e) {
      iconLibraryCache = [];
    }
    return iconLibraryCache;
  }

  function renderIconGrid(query) {
    if (!iconGrid || !iconStatus) return;
    const lib = iconLibraryCache || [];
    const q = (query || "").trim().toLowerCase();
    const matches = q
      ? lib.filter((e) =>
          (e.name + " " + e.slug + " " + (e.tags || "") + " " + (e.category || ""))
            .toLowerCase()
            .includes(q),
        )
      : lib;
    const shown = matches.slice(0, ICON_RENDER_LIMIT);
    iconGrid.innerHTML = "";
    if (lib.length === 0) {
      iconStatus.textContent = "Icon library couldn't load — check your network.";
      return;
    }
    if (matches.length === 0) {
      iconStatus.textContent = `No icons match "${q}"`;
      return;
    }
    iconStatus.textContent = q
      ? `${matches.length} match${matches.length === 1 ? "" : "es"} for "${q}"` +
        (matches.length > shown.length ? ` (showing first ${shown.length})` : "")
      : `${lib.length} icons — type to filter`;
    const frag = document.createDocumentFragment();
    for (const e of shown) {
      const tile = document.createElement("button");
      tile.type = "button";
      tile.dataset.iconUrl = e.url;
      tile.title = e.name + (e.category ? " — " + e.category : "");
      tile.className =
        "flex flex-col items-center gap-1.5 p-2 rounded-md border border-gray-200 dark:border-slate-700 hover:border-blue-400 dark:hover:border-blue-500 hover:bg-blue-50/40 dark:hover:bg-slate-700 cursor-pointer focus:outline-none focus:ring-2 focus:ring-blue-500";
      const img = document.createElement("img");
      img.src = e.url;
      img.alt = "";
      img.loading = "lazy";
      img.className = "w-10 h-10 object-contain";
      img.onerror = () => {
        tile.style.display = "none";
      };
      const label = document.createElement("span");
      label.className = "text-[0.7rem] truncate w-full text-center text-gray-600 dark:text-gray-300";
      label.textContent = e.name;
      tile.appendChild(img);
      tile.appendChild(label);
      frag.appendChild(tile);
    }
    iconGrid.appendChild(frag);
  }

  async function openIconPicker() {
    if (!iconPicker) return;
    iconPicker.classList.remove("hidden");
    iconPicker.classList.add("flex");
    if (iconSearchInput) {
      iconSearchInput.value = "";
      setTimeout(() => iconSearchInput.focus(), 50);
    }
    if (iconStatus) iconStatus.textContent = "Loading…";
    if (iconGrid) iconGrid.innerHTML = "";
    await loadIconLibrary();
    renderIconGrid("");
  }

  function closeIconPicker() {
    if (!iconPicker) return;
    iconPicker.classList.add("hidden");
    iconPicker.classList.remove("flex");
  }

  // Open trigger lives on the icon URL row inside the add-tile modal.
  document.addEventListener("click", (ev) => {
    const browseBtn = ev.target.closest && ev.target.closest("[data-icon-browse-btn]");
    if (browseBtn) {
      ev.preventDefault();
      openIconPicker();
      return;
    }
    const closeBtn = ev.target.closest && ev.target.closest("[data-icon-picker-close]");
    if (closeBtn) {
      ev.preventDefault();
      closeIconPicker();
    }
  });

  // Click-outside to dismiss.
  if (iconPicker) {
    iconPicker.addEventListener("click", (ev) => {
      if (ev.target === iconPicker) closeIconPicker();
    });
  }
  // Escape closes the picker (without closing the underlying add-tile modal).
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape" && iconPicker && !iconPicker.classList.contains("hidden")) {
      ev.stopPropagation();
      closeIconPicker();
    }
  });

  // Debounced filter on input.
  if (iconSearchInput) {
    iconSearchInput.addEventListener("input", () => {
      if (iconFilterTimer) clearTimeout(iconFilterTimer);
      iconFilterTimer = setTimeout(() => renderIconGrid(iconSearchInput.value), 80);
    });
  }

  // Click an icon tile → write the URL into the icon_url input + close.
  if (iconGrid) {
    iconGrid.addEventListener("click", (ev) => {
      const tile = ev.target.closest("[data-icon-url]");
      if (!tile) return;
      ev.preventDefault();
      const iconInput = document.querySelector('input[name="icon_url"]');
      if (iconInput) {
        iconInput.value = tile.dataset.iconUrl;
        iconInput.dispatchEvent(new Event("input", { bubbles: true }));
      }
      closeIconPicker();
    });
  }

  /* --- Catalog picker (loaded once, used inside the add-tile modal) --- */

  let catalogCache = null;
  async function loadCatalog() {
    if (catalogCache) return catalogCache;
    const res = await fetch("/home/api/catalog", {
      credentials: "same-origin",
    });
    if (!res.ok) {
      catalogCache = [];
    } else {
      catalogCache = await res.json();
    }
    return catalogCache;
  }

  // renderCatalogList renders matching catalog entries into a list element.
  // Each entry is a clickable row that fills the form's name/icon/app_slug.
  function renderCatalogList(listEl, entries, query, onPick) {
    listEl.innerHTML = "";
    const q = (query || "").toLowerCase();
    const filtered = !q
      ? entries.slice(0, 20)
      : entries
          .filter(
            (e) =>
              e.name.toLowerCase().includes(q) ||
              e.slug.toLowerCase().includes(q) ||
              (e.category || "").toLowerCase().includes(q),
          )
          .slice(0, 20);
    if (filtered.length === 0) {
      const li = document.createElement("li");
      li.className = "px-3 py-2 text-sm text-gray-500 dark:text-gray-400";
      li.textContent = "No matches. Use the form fields below to enter a custom app.";
      listEl.appendChild(li);
      return;
    }
    for (const e of filtered) {
      const li = document.createElement("li");
      li.className =
        "flex items-center gap-2 px-3 py-2 text-sm cursor-pointer hover:bg-gray-50 dark:hover:bg-slate-700";
      li.innerHTML =
        `<img src="${e.icon_url}" alt="" class="w-5 h-5"/>` +
        `<span class="font-medium text-gray-900 dark:text-white">${e.name}</span>` +
        (e.category
          ? `<span class="text-xs text-gray-400">${e.category}</span>`
          : "");
      li.addEventListener("click", () => onPick(e));
      listEl.appendChild(li);
    }
  }

  /* --- Add / edit tile modal --- */

  const addBtn = document.getElementById("home-add-tile");
  const modal = document.getElementById("home-add-tile-modal");
  const form = document.getElementById("home-add-tile-form");
  const modalTitleEl = document.getElementById("home-modal-title");
  const modalSubmitBtn = document.getElementById("home-modal-submit");

  // Tracks the tile id currently being edited; null when in create mode.
  let editingTileId = null;

  // Size preset → {w, h}. "auto" omits both so the server picks: launcher
  // tiles get 2×2, widget-provider apps get 4×2 (horizontal layout fits
  // 3 widget pills inline). See CreateTile in api.go.
  //
  // Widths >= 3 trigger the horizontal layout in home.css (icon left,
  // meta right, widget panel below spanning both columns).
  const SIZE_PRESETS = {
    auto: {},
    compact: { w: 2, h: 1 },
    standard: { w: 2, h: 2 },
    tall: { w: 2, h: 3 },
    wide: { w: 4, h: 2 },
  };

  // Sync the radio chips to the hidden size_preset input so a single
  // place owns the value at submit time.
  if (form) {
    form.addEventListener("change", (ev) => {
      if (
        ev.target instanceof HTMLInputElement &&
        ev.target.name === "size_preset_radio"
      ) {
        const hidden = form.querySelector('input[name="size_preset"]');
        if (hidden) hidden.value = ev.target.value;
      }
    });
  }

  function setSizePreset(value) {
    if (!form) return;
    const radios = form.querySelectorAll(
      'input[name="size_preset_radio"]',
    );
    radios.forEach((r) => {
      r.checked = r.value === value;
    });
    const hidden = form.querySelector('input[name="size_preset"]');
    if (hidden) hidden.value = value;
  }

  // Match a tile's current (w, h) to the closest preset name. Returns
  // "auto" when nothing matches — the form will then submit explicit w/h
  // so the layout is preserved.
  function presetFromDims(w, h) {
    for (const [name, dims] of Object.entries(SIZE_PRESETS)) {
      if (dims.w === w && dims.h === h) return name;
    }
    return "auto";
  }

  function setModalMode(mode, tile) {
    editingTileId = mode === "edit" && tile ? tile.id : null;
    if (modalTitleEl) {
      modalTitleEl.textContent = mode === "edit" ? "Edit tile" : "Add tile";
    }
    if (modalSubmitBtn) {
      modalSubmitBtn.textContent = mode === "edit" ? "Save" : "Add tile";
    }
  }

  // Tracks whether the user clicked "Clear saved key" so submit knows to
  // DELETE the secret instead of skipping it. Reset on every modal open.
  let secretCleared = false;

  // Show the API-key section for tile types that fetch upstream data.
  // Bookmarks never need a secret. Hides via `hidden` attribute (NOT the
  // `.hidden` Tailwind class — the modal toggles `flex` for itself).
  // Also hides the URL Detect button for bookmarks since fingerprinting
  // doesn't apply to "any URL" tiles.
  function syncSecretSectionVisibility() {
    if (!form) return;
    const section = form.querySelector("[data-secret-section]");
    const type = form.querySelector('select[name="type"]')?.value || "bookmark";
    const show = type === "app" || type === "customapi";
    if (section) section.hidden = !show;
    const detect = form.querySelector("[data-detect-btn]");
    if (detect) detect.hidden = !show;
  }

  // Reflect "saved key on file" in the modal: dim placeholder, expose Clear.
  function applySecretSavedState(hasSecret) {
    if (!form) return;
    const hint = form.querySelector("[data-secret-hint]");
    const input = form.querySelector('input[name="secret"]');
    const clearBtn = form.querySelector("[data-secret-clear]");
    if (hint) {
      hint.textContent = hasSecret
        ? "(saved — leave blank to keep)"
        : "(optional)";
    }
    if (input) {
      input.placeholder = hasSecret
        ? "•••••• stored — type to replace"
        : "paste API key / token";
      input.value = "";
    }
    if (clearBtn) clearBtn.classList.toggle("hidden", !hasSecret);
    secretCleared = false;
  }

  function showModal() {
    if (!modal) return;
    setModalMode("create");
    if (form) form.reset();
    setSizePreset("auto");
    applySecretSavedState(false);
    syncSecretSectionVisibility();
    modal.classList.remove("hidden");
    modal.classList.add("flex");
    // Reset detection panel + catalog list each time we open.
    const detection = modal.querySelector("[data-detection]");
    if (detection) detection.classList.add("hidden");
    const catalogList = modal.querySelector("[data-catalog-list]");
    if (catalogList) {
      loadCatalog().then((entries) =>
        renderCatalogList(catalogList, entries, "", pickFromCatalog),
      );
    }
    const firstInput = modal.querySelector('input[name="url"]');
    if (firstInput) firstInput.focus();
  }

  // showEditModal opens the same modal pre-populated from a tile node's
  // data attributes (rendered server-side by tileNode in pages.templ —
  // no extra round trip needed). CustomAPI editing is not yet supported
  // here; the user is alerted and the modal stays closed.
  function showEditModal(tileNode) {
    if (!modal || !form) return;
    const type = tileNode.dataset.tileType || "bookmark";
    if (type !== "bookmark" && type !== "app") {
      showAlertDialog({
        title: "Not yet editable",
        message:
          "Editing custom-API tiles isn't supported in this modal yet — delete and re-add for now.",
        type: "info",
      });
      return;
    }
    const id = parseInt(tileNode.dataset.tileId, 10);
    if (!id) return;
    setModalMode("edit", { id });
    form.reset();

    const typeSel = form.querySelector('select[name="type"]');
    const urlIn = form.querySelector('input[name="url"]');
    const nameIn = form.querySelector('input[name="name"]');
    const iconIn = form.querySelector('input[name="icon_url"]');
    const slugIn = form.querySelector('input[name="app_slug"]');
    if (typeSel) typeSel.value = type;
    if (urlIn) urlIn.value = tileNode.dataset.tileUrl || "";
    if (nameIn) nameIn.value = tileNode.dataset.tileName || "";
    if (iconIn) iconIn.value = tileNode.dataset.tileIconUrl || "";
    if (slugIn) slugIn.value = tileNode.dataset.tileAppSlug || "";

    // Pre-select the size chip matching the tile's current dimensions.
    const w = parseInt(tileNode.getAttribute("gs-w") || "2", 10);
    const h = parseInt(tileNode.getAttribute("gs-h") || "2", 10);
    setSizePreset(presetFromDims(w, h));

    // Secret state is exposed via data-tile-has-secret so we can show
    // the "Clear" button + hint without an extra round trip.
    applySecretSavedState(tileNode.dataset.tileHasSecret === "true");
    syncSecretSectionVisibility();

    modal.classList.remove("hidden");
    modal.classList.add("flex");
    const detection = modal.querySelector("[data-detection]");
    if (detection) detection.classList.add("hidden");
    if (nameIn) nameIn.focus();
  }

  // pickFromCatalog fills the form with the chosen catalog entry and closes
  // the picker section.
  function pickFromCatalog(entry) {
    if (!form) return;
    const typeSel = form.querySelector('select[name="type"]');
    const nameIn = form.querySelector('input[name="name"]');
    const iconIn = form.querySelector('input[name="icon_url"]');
    const slugIn = form.querySelector('input[name="app_slug"]');
    if (typeSel) {
      typeSel.value = "app";
      // Detected entries are always app tiles — surface the API Key field
      // even though `change` events from .value= don't fire automatically.
      syncSecretSectionVisibility();
    }
    if (nameIn) nameIn.value = entry.name;
    if (iconIn) iconIn.value = entry.icon_url || "";
    if (slugIn) slugIn.value = entry.slug;
    showDetectionBanner(entry.name, entry.icon_url);
  }

  // showDetectionBanner reveals the green "Detected: <name>" panel above
  // the form fields with the matched icon preview. Used by both the
  // catalog fingerprinter (pickFromCatalog) and the icon-name suggester
  // — same affordance, same affordance, same affordance, regardless of
  // which detection path matched. Hiding the broken-image alt on 404
  // keeps the panel clean when an icon URL goes stale.
  function showDetectionBanner(name, iconURL) {
    if (!modal) return;
    const detection = modal.querySelector("[data-detection]");
    if (!detection) return;
    detection.classList.remove("hidden");
    const detName = detection.querySelector("[data-detection-name]");
    if (detName) detName.textContent = name;
    const detIcon = detection.querySelector("[data-detection-icon]");
    if (!detIcon) return;
    if (iconURL) {
      detIcon.src = iconURL;
      detIcon.alt = name + " icon";
      detIcon.style.display = "";
      detIcon.onerror = () => {
        detIcon.style.display = "none";
      };
    } else {
      detIcon.removeAttribute("src");
      detIcon.style.display = "none";
    }
  }

  // probeURL hits /home/api/probe to fingerprint the entered URL. On a hit,
  // it pre-fills the form via pickFromCatalog. Triggered on URL input blur
  // (debounced) and on the explicit "Detect" button.
  let probeTimer = null;
  function debouncedProbe() {
    if (probeTimer) clearTimeout(probeTimer);
    probeTimer = setTimeout(probeURL, 600);
  }
  async function probeURL() {
    if (!form) return;
    const urlIn = form.querySelector('input[name="url"]');
    if (!urlIn || !urlIn.value || urlIn.value.length < 7) return;
    const typeSel = form.querySelector('select[name="type"]');
    const tileType = (typeSel && typeSel.value) || "app";

    let catalogMatched = false;

    // App tiles run the catalog fingerprint first — that's the
    // high-confidence path (probes the actual upstream and looks for an
    // app-specific signature). Bookmarks skip this entirely; their URL
    // is by definition arbitrary and a fingerprint match against
    // google.com can only be a false positive.
    if (tileType !== "bookmark") {
      try {
        const res = await fetch(
          `/home/api/probe?url=${encodeURIComponent(urlIn.value)}`,
          { credentials: "same-origin" },
        );
        if (res.ok) {
          const body = await res.json();
          if (body.matched && body.app) {
            pickFromCatalog(body.app);
            catalogMatched = true;
          }
        }
      } catch (_e) {
        /* ignore — fall through to icon suggester */
      }
    }

    // Icon-name suggester fallback: scan the URL host's labels against
    // the selfh.st icon library by exact slug/name match. Catches the
    // common case of "google.com → Google icon" for bookmarks, plus
    // self-hosted apps not in our predefined catalog (e.g. "navidrome.
    // sarg3.net" → Navidrome icon). Only fills empty fields so the
    // user's manual choices win — and only reveals the green detection
    // banner when at least one field was actually filled, otherwise the
    // banner would be misleading ("Detected: Google" while the user has
    // already typed their own name).
    if (!catalogMatched) {
      try {
        const res = await fetch(
          `/home/api/icons/suggest?url=${encodeURIComponent(urlIn.value)}`,
          { credentials: "same-origin" },
        );
        if (res.ok) {
          const match = await res.json();
          if (match && match.url) {
            const iconIn = form.querySelector('input[name="icon_url"]');
            const nameIn = form.querySelector('input[name="name"]');
            let filled = false;
            if (iconIn && !iconIn.value) {
              iconIn.value = match.url;
              filled = true;
            }
            if (nameIn && !nameIn.value) {
              nameIn.value = match.name;
              filled = true;
            }
            if (filled) showDetectionBanner(match.name, match.url);
          }
        }
      } catch (_e) {
        /* ignore — empty fields stay empty, user fills manually */
      }
    }
  }
  function hideModal() {
    if (!modal) return;
    modal.classList.add("hidden");
    modal.classList.remove("flex");
    if (form) form.reset();
  }

  if (addBtn) addBtn.addEventListener("click", showModal);
  document.querySelectorAll("[data-home-add-tile]").forEach((b) => {
    b.addEventListener("click", showModal);
  });
  document.querySelectorAll("[data-home-modal-close]").forEach((b) => {
    b.addEventListener("click", hideModal);
  });
  if (modal) {
    modal.addEventListener("click", (ev) => {
      if (ev.target === modal) hideModal();
    });
  }
  if (form) {
    // Trigger fingerprint on URL change (debounced) and on Detect button.
    const urlIn = form.querySelector('input[name="url"]');
    if (urlIn) {
      urlIn.addEventListener("input", debouncedProbe);
      urlIn.addEventListener("blur", probeURL);
    }
    const detectBtn = modal.querySelector("[data-detect-btn]");
    if (detectBtn) detectBtn.addEventListener("click", probeURL);

    // Catalog search box filters the list as you type.
    const searchIn = modal.querySelector("[data-catalog-search]");
    const catalogList = modal.querySelector("[data-catalog-list]");
    if (searchIn && catalogList) {
      searchIn.addEventListener("input", () =>
        loadCatalog().then((entries) =>
          renderCatalogList(catalogList, entries, searchIn.value, pickFromCatalog),
        ),
      );
    }

    // Type select toggles the API-key section.
    const typeSel = form.querySelector('select[name="type"]');
    if (typeSel) {
      typeSel.addEventListener("change", syncSecretSectionVisibility);
    }

    // Clear-saved-key button: marks intent. The actual DELETE happens on
    // submit, alongside the rest of the save flow, so a user who clicks
    // Cancel doesn't accidentally lose their stored key.
    const clearBtn = form.querySelector("[data-secret-clear]");
    if (clearBtn) {
      clearBtn.addEventListener("click", () => {
        secretCleared = true;
        const hint = form.querySelector("[data-secret-hint]");
        const input = form.querySelector('input[name="secret"]');
        if (hint) hint.textContent = "(will be cleared on save)";
        if (input) {
          input.value = "";
          input.placeholder = "paste API key / token";
        }
        clearBtn.classList.add("hidden");
      });
    }

    form.addEventListener("submit", async (ev) => {
      ev.preventDefault();
      const data = new FormData(form);
      const type = String(data.get("type") || "bookmark");
      const config = {
        url: String(data.get("url") || ""),
        name: String(data.get("name") || ""),
        icon_url: String(data.get("icon_url") || ""),
      };
      if (type === "app") {
        const slug = String(data.get("app_slug") || "");
        if (slug) config.app_slug = slug;
      }

      const presetName = String(data.get("size_preset") || "auto");
      const preset = SIZE_PRESETS[presetName] || SIZE_PRESETS.auto;

      let res;
      if (editingTileId) {
        // Edit mode → PATCH only the fields we know about. Layout fields
        // come from the size preset; omit h on "auto" so the server keeps
        // the existing height.
        const payload = { config };
        if (typeof preset.w === "number") payload.w = preset.w;
        if (typeof preset.h === "number") payload.h = preset.h;
        res = await fetch(`/home/api/tiles/${editingTileId}`, {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        });
      } else {
        // For sizing the slot search, use the preset dims if specified,
        // otherwise fall back to the server's "auto" defaults (W=2, H=2)
        // so we still find a meaningful position. The server may end up
        // bumping H to 3 for widget-provider apps; that's fine because
        // findNextSlot just needs a starting size to avoid colliding.
        const slotW = typeof preset.w === "number" ? preset.w : 2;
        const slotH = typeof preset.h === "number" ? preset.h : 2;
        const slot = findNextSlot(slotW, slotH);
        const payload = { type, x: slot.x, y: slot.y, config };
        if (typeof preset.w === "number") payload.w = preset.w;
        // Omit h on "auto" so the server picks H=2 (launcher) or H=3
        // (widget-provider app) — see CreateTile in api.go.
        if (typeof preset.h === "number") payload.h = preset.h;
        res = await fetch(
          `/home/api/boards/${encodeURIComponent(boardId)}/tiles`,
          {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(payload),
          },
        );
      }

      if (!res.ok) {
        await showAlertDialog({
          title: editingTileId ? "Failed to save tile" : "Failed to add tile",
          message: res.statusText || "The server rejected the request.",
          type: "error",
        });
        return;
      }

      // Resolve the tile id we should target for secret persistence.
      // PATCH keeps the same id; POST returns the new id in the response.
      let secretTileId = editingTileId;
      if (!secretTileId) {
        try {
          const created = await res.json();
          secretTileId = created && created.id;
        } catch (_e) {
          /* PATCH returns 204 with no body — fine */
        }
      }

      // Persist the secret intent if the user typed one or clicked Clear.
      // Failures here are surfaced but don't block the layout reload —
      // the tile itself was saved successfully.
      const secretValue = String(data.get("secret") || "").trim();
      if (secretTileId && secretValue) {
        const sres = await fetch(
          `/home/api/tiles/${secretTileId}/secret`,
          {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ secret: secretValue }),
          },
        );
        if (!sres.ok) {
          await showAlertDialog({
            title: "API key not stored",
            message: "The tile was saved, but storing the API key failed: " + (sres.statusText || "unknown error"),
            type: "error",
          });
        }
      } else if (secretTileId && secretCleared) {
        const sres = await fetch(
          `/home/api/tiles/${secretTileId}/secret`,
          { method: "DELETE" },
        );
        if (!sres.ok) {
          await showAlertDialog({
            title: "API key not cleared",
            message: "The tile was saved, but clearing the API key failed: " + (sres.statusText || "unknown error"),
            type: "error",
          });
        }
      }

      // Reload to render the updated/new tile from the server-rendered
      // template. Phase 8 can replace this with an HTMX-style fragment
      // swap if desired.
      window.location.reload();
    });
  }

  /* --- Add board --- */

  const addBoardBtn = document.getElementById("home-add-board");
  if (addBoardBtn) {
    addBoardBtn.addEventListener("click", async () => {
      const name = await showPromptDialog({
        title: "New board",
        message: "What should this board be called?",
        placeholder: "Media",
      });
      if (!name) return;
      // Slug: lowercase, alphanumeric + dashes only.
      const slug = name
        .toLowerCase()
        .trim()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "");
      if (!slug) {
        await showAlertDialog({
          title: "Invalid board name",
          message: "That name doesn't produce a valid slug. Try plain text — letters, numbers, and spaces.",
          type: "error",
        });
        return;
      }
      const res = await fetch("/home/api/boards", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ slug, name, sort_order: 99 }),
      });
      if (!res.ok) {
        await showAlertDialog({
          title: "Failed to create board",
          message: (await res.text()) || res.statusText,
          type: "error",
        });
        return;
      }
      window.location.href = `/home/b/${encodeURIComponent(slug)}`;
    });
  }

  /* --- Delete board --- */

  const deleteBoardBtn = document.getElementById("home-delete-board");
  if (deleteBoardBtn) {
    deleteBoardBtn.addEventListener("click", async () => {
      const boardId = deleteBoardBtn.dataset.boardId;
      const boardName = deleteBoardBtn.dataset.boardName || "this board";
      if (!boardId) return;
      const confirmed = await showConfirmDialog({
        title: "Delete board",
        message: `Delete "${boardName}"? All tiles on this board will be removed. This cannot be undone.`,
        confirmText: "Delete board",
        type: "danger",
      });
      if (!confirmed) return;
      const res = await fetch(`/home/api/boards/${encodeURIComponent(boardId)}`, {
        method: "DELETE",
      });
      if (!res.ok) {
        await showAlertDialog({
          title: "Failed to delete board",
          message: (await res.text()) || res.statusText,
          type: "error",
        });
        return;
      }
      // Redirect to /home/ so the server lands the user on whichever
      // board it picks as the new default.
      window.location.href = "/home/";
    });
  }

  /* --- Import (Export is just a normal anchor link) --- */

  const importBtn = document.getElementById("home-import");
  const importFile = document.getElementById("home-import-file");
  if (importBtn && importFile) {
    importBtn.addEventListener("click", () => importFile.click());
    importFile.addEventListener("change", async () => {
      const file = importFile.files && importFile.files[0];
      if (!file) return;
      const confirmed = await showConfirmDialog({
        title: "Replace dashboard from backup?",
        message:
          "Importing replaces all boards and tiles on this dashboard. " +
          "Encrypted API keys and passwords are NOT included in backups — " +
          "you'll need to re-enter them after import.",
        confirmText: "Replace",
        type: "danger",
      });
      if (!confirmed) {
        importFile.value = "";
        return;
      }
      try {
        const text = await file.text();
        const res = await fetch("/home/api/import", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: text,
        });
        if (!res.ok) {
          await showAlertDialog({
            title: "Import failed",
            message: (await res.text()) || res.statusText,
            type: "error",
          });
          return;
        }
        window.location.href = "/home/";
      } catch (e) {
        await showAlertDialog({
          title: "Import failed",
          message: e.message || String(e),
          type: "error",
        });
      } finally {
        importFile.value = "";
      }
    });
  }

  /* --- Search bar — '/' to focus, bookmark-search on Enter --- */

  const searchForm = document.getElementById("home-search");
  const searchInput = document.getElementById("home-search-input");

  if (searchInput) {
    document.addEventListener("keydown", (ev) => {
      if (ev.key !== "/") return;
      const tag = (document.activeElement && document.activeElement.tagName) || "";
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      ev.preventDefault();
      searchInput.focus();
      searchInput.select();
    });
  }

  if (searchForm) {
    // Always preventDefault on submit and dispatch via window.open. The
    // page's CSP enforces `form-action 'self'`, which blocks the native
    // form submission to https://duckduckgo.com — the new tab opens to
    // about:blank instead of the search results page (issue: "search bar
    // doesn't work; opens a new tab at 'about:blank'"). window.open
    // isn't governed by form-action, so it goes through cleanly. As a
    // bonus, the bookmark-search and web-search paths now share the
    // same dispatch helper.
    const SEARCH_BASE = searchForm.action || "https://duckduckgo.com/";
    const QUERY_PARAM = searchInput?.name || "q";
    searchForm.addEventListener("submit", (ev) => {
      ev.preventDefault();
      const raw = (searchInput && searchInput.value.trim()) || "";
      if (!raw) return;
      const q = raw.toLowerCase();

      // Bookmark-search: if any tile's display name contains the query,
      // open the first match instead of doing a web search.
      let matched = null;
      grid.querySelectorAll(".home-tile-name").forEach((el) => {
        if (matched) return;
        if (el.textContent.toLowerCase().includes(q)) {
          const a = el.closest("a[data-tile-link]");
          if (a) matched = a.href;
        }
      });

      const target = matched
        ? matched
        : SEARCH_BASE + "?" + new URLSearchParams({ [QUERY_PARAM]: raw }).toString();
      window.open(target, "_blank", "noopener");
      searchInput.value = "";
    });
  }

  /* --- "Make default page" toggle --- */

  const landingBtn = document.getElementById("home-landing-toggle");
  if (landingBtn) {
    landingBtn.addEventListener("click", async () => {
      const active = landingBtn.dataset.active === "true";
      const path = active ? "" : "/home";
      const res = await fetch("/home/api/landing-path", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path }),
      });
      if (!res.ok) {
        await showAlertDialog({
          title: "Failed to update default landing page",
          message: res.statusText || "The server rejected the request.",
          type: "error",
        });
        return;
      }
      window.location.reload();
    });
  }

  /* --- Status dots: one-shot fetch + SSE subscription --- */

  // formatRelativeTime returns "5s ago", "3m ago", "2h ago", etc. for an
  // ISO-8601 / RFC3339 timestamp. Used in the status tooltip.
  function formatRelativeTime(iso) {
    if (!iso) return "";
    const t = new Date(iso).getTime();
    if (!t) return "";
    const sec = Math.max(0, Math.floor((Date.now() - t) / 1000));
    if (sec < 60) return `${sec}s ago`;
    if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
    if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`;
    return `${Math.floor(sec / 86400)}d ago`;
  }

  // buildStatusTooltip composes a multi-line tooltip from a StatusEvent
  // (see internal/gears/home/status.go). Falls back gracefully when the
  // event is empty (e.g. unknown status from a never-probed tile).
  function buildStatusTooltip(evt) {
    const status = (evt && evt.status) || "unknown";
    const parts = [`Status: ${status}`];
    if (evt) {
      if (typeof evt.latency_ms === "number" && evt.latency_ms > 0) {
        parts.push(`${evt.latency_ms}ms`);
      }
      if (typeof evt.http_status === "number" && evt.http_status > 0) {
        parts.push(`HTTP ${evt.http_status}`);
      }
      if (evt.checked_at) {
        const rel = formatRelativeTime(evt.checked_at);
        if (rel) parts.push(`checked ${rel}`);
      }
      if (evt.error) {
        parts.push(`Error: ${evt.error}`);
      }
    }
    return parts.join(" · ");
  }

  function applyStatus(tileId, evt) {
    const item = grid.querySelector(`[data-tile-id="${tileId}"]`);
    if (!item) return;
    // Scope the dot query to the .home-tile-status span. We also write
    // data-tile-status on the .home-tile container further down (drives
    // the CSS border accent), so a bare [data-tile-status] selector
    // matches the parent first in document order — and the .status-up
    // class ended up green-flooding the whole tile background. See
    // issue: "the green tile background isn't jiving with me".
    const dot = item.querySelector(".home-tile-status[data-tile-status]");
    const tile = item.querySelector(".home-tile");
    const status = (evt && evt.status) || "unknown";

    if (dot) {
      dot.classList.remove(
        "home-tile-status-up",
        "home-tile-status-down",
        "home-tile-status-degraded",
        "home-tile-status-unknown",
      );
      dot.classList.add(`home-tile-status-${status}`);
      dot.title = buildStatusTooltip(evt);
    }
    // Drives the status-tinted left border accent in CSS.
    if (tile) tile.dataset.tileStatus = status;
  }

  // Pull each tile's last-known status on load so dots aren't grey for the
  // first 30s before the worker probes them. /home/api/tiles/{id}/status
  // returns "unknown" when the worker hasn't probed yet — harmless.
  function loadInitialStatuses() {
    grid.querySelectorAll("[data-tile-id]").forEach(async (el) => {
      const id = el.dataset.tileId;
      try {
        const res = await fetch(`/home/api/tiles/${id}/status`, {
          credentials: "same-origin",
        });
        if (!res.ok) return;
        const evt = await res.json();
        applyStatus(id, evt);
      } catch (_e) {
        /* ignore */
      }
    });
  }

  /* --- Widget data: one-shot fetch + SSE subscription --- */

  function applyWidget(tileId, fields) {
    const panel = grid.querySelector(
      `[data-tile-id="${tileId}"] [data-tile-widget]`,
    );
    if (!panel) return;
    panel.innerHTML = "";
    // Empty fields → leave the panel blank (matches the server-rendered
    // initial state). A persistent "…" placeholder for tiles that never
    // get widget data was confusing and triggered a scrollbar at H=2.
    if (!fields || Object.keys(fields).length === 0) {
      return;
    }
    for (const [key, value] of Object.entries(fields)) {
      const wrap = document.createElement("span");
      wrap.className = "home-tile-widget-field";
      const lbl = document.createElement("span");
      lbl.className = "home-tile-widget-label";
      lbl.textContent = key;
      const val = document.createElement("span");
      val.className = "home-tile-widget-value";
      val.textContent = value;
      wrap.appendChild(lbl);
      wrap.appendChild(val);
      panel.appendChild(wrap);
    }
  }

  function loadInitialWidgets() {
    grid.querySelectorAll("[data-tile-widget]").forEach(async (el) => {
      const id = el.closest("[data-tile-id]").dataset.tileId;
      try {
        const res = await fetch(`/home/api/tiles/${id}/widget`, {
          credentials: "same-origin",
        });
        if (!res.ok) return;
        const evt = await res.json();
        applyWidget(id, evt.fields || {});
      } catch (_e) {
        /* ignore */
      }
    });
  }

  let sseSource = null;
  function openSSE() {
    if (sseSource) return;
    sseSource = new EventSource("/home/api/events");
    sseSource.addEventListener("tile.status", (msg) => {
      try {
        const evt = JSON.parse(msg.data);
        applyStatus(evt.tile_id, evt);
      } catch (_e) {
        /* ignore */
      }
    });
    sseSource.addEventListener("tile.widget", (msg) => {
      try {
        const evt = JSON.parse(msg.data);
        applyWidget(evt.tile_id, evt.fields || {});
      } catch (_e) {
        /* ignore */
      }
    });
    sseSource.addEventListener("error", () => {
      // EventSource auto-reconnects; close+null so a manual retry button
      // could reopen it later if we add one.
      if (sseSource && sseSource.readyState === EventSource.CLOSED) {
        sseSource = null;
        // Wait a beat so we don't tight-loop if the server is down.
        setTimeout(openSSE, 5000);
      }
    });
  }

  loadInitialStatuses();
  loadInitialWidgets();
  openSSE();

  /* --- HTTP helpers --- */

  async function patchTileLayout(id, x, y, w, h) {
    const res = await fetch(`/home/api/tiles/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ x, y, w, h }),
    });
    if (!res.ok) {
      console.error("Tile layout PATCH failed", id, res.status);
    }
  }

  async function deleteTile(id) {
    const res = await fetch(`/home/api/tiles/${id}`, { method: "DELETE" });
    if (!res.ok) {
      await showAlertDialog({
        title: "Failed to delete tile",
        message: res.statusText || "The server rejected the delete request.",
        type: "error",
      });
      throw new Error("delete failed");
    }
  }
})();
