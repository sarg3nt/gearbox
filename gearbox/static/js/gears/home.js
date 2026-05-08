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

  /* --- Add tile modal --- */

  const addBtn = document.getElementById("home-add-tile");
  const modal = document.getElementById("home-add-tile-modal");
  const form = document.getElementById("home-add-tile-form");

  function showModal() {
    if (!modal) return;
    modal.classList.remove("hidden");
    modal.classList.add("flex");
    const firstInput = modal.querySelector('input[name="name"]');
    if (firstInput) firstInput.focus();
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
    form.addEventListener("submit", async (ev) => {
      ev.preventDefault();
      const data = new FormData(form);
      const payload = {
        type: String(data.get("type") || "bookmark"),
        x: 0,
        y: 0,
        w: 2,
        h: 1,
        config: JSON.stringify({
          url: String(data.get("url") || ""),
          name: String(data.get("name") || ""),
          icon_url: String(data.get("icon_url") || ""),
        }),
      };
      // The API accepts a json.RawMessage on `config`; turn the string into
      // an object so it's nested rather than escaped.
      try {
        payload.config = JSON.parse(payload.config);
      } catch (_e) {
        payload.config = {};
      }
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
