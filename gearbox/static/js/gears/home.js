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

  const grid = document.getElementById("home-grid");
  if (!grid) return;

  const boardId = grid.dataset.boardId;

  /** Initialize gridstack. */
  const gs = GridStack.init(
    {
      column: 12,
      cellHeight: 80,
      margin: 8,
      float: false,
      staticGrid: true, // Read-only until the user enters edit mode.
      acceptWidgets: false,
      animate: true,
    },
    grid,
  );

  /** Track of tiles whose layout the server already knows about, so the
   *  initial gridstack pass doesn't re-PATCH every existing tile. */
  let suppressChange = true;
  setTimeout(() => {
    suppressChange = false;
  }, 100);

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
      // Show/hide the per-tile delete buttons.
      grid.querySelectorAll("[data-tile-delete]").forEach((btn) => {
        btn.classList.toggle("hidden", isStatic);
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

  grid.addEventListener("click", (ev) => {
    const target = ev.target;
    if (!(target instanceof Element)) return;
    if (target.closest(".home-disable-click")) {
      ev.preventDefault();
      return;
    }
    const deleteBtn = target.closest("[data-tile-delete]");
    if (!deleteBtn) return;
    ev.preventDefault();
    ev.stopPropagation();
    const tileId = parseInt(deleteBtn.dataset.tileDelete, 10);
    if (!tileId) return;
    if (!confirm("Delete this tile?")) return;
    deleteTile(tileId).then(() => {
      const node = deleteBtn.closest(".grid-stack-item");
      if (node) gs.removeWidget(node, true);
      // If we just emptied the board, reload to surface the empty state.
      if (gs.engine.nodes.length === 0) window.location.reload();
    });
  });

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

  /* --- Add tile modal --- */

  const addBtn = document.getElementById("home-add-tile");
  const modal = document.getElementById("home-add-tile-modal");
  const form = document.getElementById("home-add-tile-form");

  function showModal() {
    if (!modal) return;
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

  // pickFromCatalog fills the form with the chosen catalog entry and closes
  // the picker section.
  function pickFromCatalog(entry) {
    if (!form) return;
    const typeSel = form.querySelector('select[name="type"]');
    const nameIn = form.querySelector('input[name="name"]');
    const iconIn = form.querySelector('input[name="icon_url"]');
    const slugIn = form.querySelector('input[name="app_slug"]');
    if (typeSel) typeSel.value = "app";
    if (nameIn) nameIn.value = entry.name;
    if (iconIn) iconIn.value = entry.icon_url || "";
    if (slugIn) slugIn.value = entry.slug;
    const detection = modal.querySelector("[data-detection]");
    if (detection) {
      detection.classList.remove("hidden");
      const detName = detection.querySelector("[data-detection-name]");
      if (detName) detName.textContent = entry.name;
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
    try {
      const res = await fetch(
        `/home/api/probe?url=${encodeURIComponent(urlIn.value)}`,
        { credentials: "same-origin" },
      );
      if (!res.ok) return;
      const body = await res.json();
      if (body.matched && body.app) pickFromCatalog(body.app);
    } catch (_e) {
      /* ignore */
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
      const payload = {
        type,
        x: 0,
        y: 0,
        w: 2,
        h: 1,
        config,
      };
      const res = await fetch(
        `/home/api/boards/${encodeURIComponent(boardId)}/tiles`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        },
      );
      if (!res.ok) {
        alert("Failed to add tile: " + res.statusText);
        return;
      }
      // Reload to render the new tile from the server-rendered template.
      // Phase 8 can replace this with an HTMX-style fragment swap if desired.
      window.location.reload();
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
        alert("Failed to update default landing page");
        return;
      }
      window.location.reload();
    });
  }

  /* --- Status dots: one-shot fetch + SSE subscription --- */

  function applyStatus(tileId, status) {
    const dot = grid.querySelector(
      `[data-tile-id="${tileId}"] [data-tile-status]`,
    );
    if (!dot) return;
    dot.classList.remove(
      "home-tile-status-up",
      "home-tile-status-down",
      "home-tile-status-degraded",
      "home-tile-status-unknown",
    );
    dot.classList.add(`home-tile-status-${status || "unknown"}`);
    dot.title = status ? `Status: ${status}` : "Status: unknown";
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
        applyStatus(id, evt.status);
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
        applyStatus(evt.tile_id, evt.status);
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
      alert("Failed to delete tile: " + res.statusText);
      throw new Error("delete failed");
    }
  }
})();
