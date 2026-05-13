/**
 * Command Palette — Cmd/Ctrl+K to open, search to filter, ↵ to select.
 *
 * Subsumes the old box-switcher dialog: the palette is the single entry
 * point for "go to box X", "go to gear Y", and a small set of quick
 * actions (theme toggle, sidebar toggle, reorder sidebar, show shortcuts,
 * log out).
 *
 * Catalog source: server-rendered JSON islands inside CommandPalette()
 * (#cmdk-boxes, #cmdk-gears) so first open is instant and survives offline.
 * Live status (the box dot) is hydrated from /bx/api/status and kept in
 * sync via SSE, exactly like the legacy switcher.
 *
 * Special interactions:
 *   - Enter on a Box   → switch to that box and close.
 *   - Ctrl+Enter on a Box → switch box, KEEP the palette open with the
 *                           input cleared, so the user can immediately
 *                           drill to a gear within that box.
 *   - Enter on a Gear  → navigate.
 *   - Enter on Action  → invoke the action's handler.
 *
 * Recents: each successful selection pushes onto a localStorage list, capped
 * at 5 entries. Empty until the user picks something.
 */
(function () {
    'use strict';

    const RECENTS_KEY = 'gearbox-cmdk-recents';
    const RECENTS_MAX = 5;
    const SVG_NS = 'http://www.w3.org/2000/svg';

    const STATUS_DOT_CLASSES = {
        green:   'bg-green-500 ring-1 ring-inset ring-green-600/40',
        yellow:  'bg-amber-400 ring-1 ring-inset ring-amber-600/40',
        red:     'bg-red-500 ring-1 ring-inset ring-red-700/40',
        gray:    'bg-gray-400 ring-1 ring-inset ring-gray-500/40',
        unknown: 'bg-gray-300 ring-1 ring-inset ring-gray-400/40 animate-pulse',
    };

    /* -------------------------------------------------------------- *
     * Catalog
     * -------------------------------------------------------------- */
    let boxes = [];   // [{id, name}]
    let gears = [];   // [{name, label, path, enabled}]
    let pages = [];   // [{name, label, path}]   (settings + profile pages, perm-filtered)
    let actions = []; // [{id, label, subtitle?, run(), kbd?}]
    const statusByBox = new Map();
    let activeBoxID = '';

    function loadJSONIsland(id) {
        const node = document.getElementById(id);
        if (!node) return null;
        try { return JSON.parse(node.textContent || 'null'); }
        catch (_) { return null; }
    }

    function loadCatalog() {
        boxes = loadJSONIsland('cmdk-boxes') || [];
        gears = loadJSONIsland('cmdk-gears') || [];
        pages = loadJSONIsland('cmdk-pages') || [];
        const overlay = document.getElementById('cmdk-overlay');
        if (overlay) activeBoxID = overlay.dataset.activeBoxId || '';
        actions = buildActions();
    }

    /* -------------------------------------------------------------- *
     * Quick actions
     * -------------------------------------------------------------- */
    function buildActions() {
        const list = [];
        list.push({
            id: 'theme:light', label: 'Theme: Light',
            subtitle: 'Force the light theme for this device',
            run: function () { if (typeof setTheme === 'function') setTheme('light'); },
        });
        list.push({
            id: 'theme:dark', label: 'Theme: Dark',
            subtitle: 'Force the dark theme for this device',
            run: function () { if (typeof setTheme === 'function') setTheme('dark'); },
        });
        list.push({
            id: 'theme:system', label: 'Theme: System',
            subtitle: 'Match the OS theme preference',
            run: function () { if (typeof setTheme === 'function') setTheme('system'); },
        });
        list.push({
            id: 'sidebar:toggle', label: 'Toggle sidebar',
            subtitle: 'Collapse or expand the navigation sidebar',
            run: function () { if (typeof toggleSidebar === 'function') toggleSidebar(); },
        });
        list.push({
            id: 'sidebar:reorder', label: 'Reorder sidebar',
            subtitle: 'Enter drag-to-reorder mode for the gear list',
            run: function () { if (typeof toggleSidebarEditMode === 'function') toggleSidebarEditMode(); },
        });
        list.push({
            id: 'shortcuts:show', label: 'Show keyboard shortcuts',
            subtitle: 'Open the shortcut reference', kbd: '?',
            run: function () { if (typeof window.openShortcutHelp === 'function') window.openShortcutHelp(); },
        });
        list.push({
            id: 'logout', label: 'Log out',
            subtitle: 'End the current session',
            run: function () {
                const f = document.createElement('form');
                f.method = 'POST';
                f.action = '/logout';
                document.body.appendChild(f);
                f.submit();
            },
        });
        return list;
    }

    /* -------------------------------------------------------------- *
     * Recents (localStorage)
     * -------------------------------------------------------------- */
    function loadRecents() {
        try {
            const raw = localStorage.getItem(RECENTS_KEY);
            if (!raw) return [];
            const v = JSON.parse(raw);
            return Array.isArray(v) ? v : [];
        } catch (_) { return []; }
    }
    function pushRecent(entry) {
        const list = loadRecents();
        const filtered = list.filter(function (e) { return e.id !== entry.id; });
        filtered.unshift({ id: entry.id, kind: entry.kind });
        const capped = filtered.slice(0, RECENTS_MAX);
        try { localStorage.setItem(RECENTS_KEY, JSON.stringify(capped)); }
        catch (_) { /* localStorage may be disabled */ }
    }
    function getRecentItems() {
        const recent = loadRecents();
        const out = [];
        recent.forEach(function (r) {
            const item = lookupItem(r.kind, r.id);
            if (item) out.push(item);
        });
        return out;
    }

    /* -------------------------------------------------------------- *
     * Item lookup
     * -------------------------------------------------------------- */
    function gearItems() {
        return gears
            .filter(function (g) { return g.enabled; })
            .map(function (g) { return { kind: 'gear', id: 'gear:' + g.name, label: g.label, subtitle: g.path, gear: g }; });
    }
    function boxItems() {
        return boxes.map(function (b) { return { kind: 'box', id: 'box:' + b.id, label: b.name, box: b }; });
    }
    function pageItems() {
        return pages.map(function (p) { return { kind: 'page', id: 'page:' + p.name, label: p.label, subtitle: p.path, page: p }; });
    }
    function actionItems() {
        return actions.map(function (a) {
            return { kind: 'action', id: a.id, label: a.label, subtitle: a.subtitle || '', kbd: a.kbd || '', action: a };
        });
    }
    function lookupItem(kind, id) {
        switch (kind) {
            case 'gear': {
                const g = gears.find(function (x) { return ('gear:' + x.name) === id; });
                if (!g || !g.enabled) return null;
                return { kind: 'gear', id: id, label: g.label, subtitle: g.path, gear: g };
            }
            case 'box': {
                const b = boxes.find(function (x) { return ('box:' + x.id) === id; });
                if (!b) return null;
                return { kind: 'box', id: id, label: b.name, box: b };
            }
            case 'page': {
                const p = pages.find(function (x) { return ('page:' + x.name) === id; });
                if (!p) return null;
                return { kind: 'page', id: id, label: p.label, subtitle: p.path, page: p };
            }
            case 'action': {
                const a = actions.find(function (x) { return x.id === id; });
                if (!a) return null;
                return { kind: 'action', id: id, label: a.label, subtitle: a.subtitle || '', kbd: a.kbd || '', action: a };
            }
        }
        return null;
    }

    /* -------------------------------------------------------------- *
     * Fuzzy scorer
     * -------------------------------------------------------------- */
    function score(query, label) {
        if (!query) return 0;
        const q = query.toLowerCase();
        const l = label.toLowerCase();
        if (l === q) return 1000;
        const idx = l.indexOf(q);
        if (idx === 0) return 800;
        if (idx > 0 && (l[idx - 1] === ' ' || l[idx - 1] === '/' || l[idx - 1] === '-')) {
            return 700;
        }
        if (idx !== -1) return 500 - idx;
        // Subsequence fallback.
        let li = 0;
        for (let qi = 0; qi < q.length; qi++) {
            const c = q[qi];
            const found = l.indexOf(c, li);
            if (found === -1) return -1;
            li = found + 1;
        }
        return 100;
    }

    /* -------------------------------------------------------------- *
     * Render helpers
     * -------------------------------------------------------------- */
    function makeStatusDot(level) {
        const dot = document.createElement('span');
        dot.className = 'inline-block w-2.5 h-2.5 rounded-full flex-shrink-0 ' +
            (STATUS_DOT_CLASSES[level] || STATUS_DOT_CLASSES.unknown);
        return dot;
    }
    function makeKindBadge(kind) {
        // Simple, accessible kind glyph — colored bullet, no SVG.
        const badge = document.createElement('span');
        badge.className = 'inline-block w-2 h-2 rounded-sm flex-shrink-0 ' +
            (kind === 'gear' ? 'bg-blue-400 dark:bg-blue-500' : 'bg-gray-300 dark:bg-gray-600');
        return badge;
    }

    /* -------------------------------------------------------------- *
     * Render
     * -------------------------------------------------------------- */
    let cursor = 0;
    let lastVisible = [];

    function buildSection(title, items) {
        if (items.length === 0) return null;
        const section = document.createElement('div');
        section.className = 'cmdk-section';
        const heading = document.createElement('div');
        heading.className = 'px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500';
        heading.textContent = title;
        section.appendChild(heading);
        items.forEach(function (item) { section.appendChild(buildRow(item)); });
        return section;
    }

    function buildRow(item) {
        const row = document.createElement('div');
        row.className = 'cmdk-row flex items-center gap-3 px-3 py-2 cursor-pointer text-sm rounded';
        row.dataset.id = item.id;
        row.dataset.kind = item.kind;
        row.setAttribute('role', 'option');

        if (item.kind === 'box') {
            const status = statusByBox.get(item.box.id);
            const level = (status && status.level) || 'unknown';
            row.appendChild(makeStatusDot(level));
        } else {
            row.appendChild(makeKindBadge(item.kind));
        }

        const label = document.createElement('span');
        label.className = 'text-gray-800 dark:text-gray-100 flex-1 truncate';
        label.textContent = item.label;
        row.appendChild(label);

        if (item.subtitle) {
            const sub = document.createElement('span');
            sub.className = 'text-xs text-gray-400 dark:text-gray-500 truncate max-w-[40%]';
            sub.textContent = item.subtitle;
            row.appendChild(sub);
        }
        if (item.kind === 'box' && item.box.id === activeBoxID) {
            const tag = document.createElement('span');
            tag.className = 'text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300';
            tag.textContent = 'Active';
            row.appendChild(tag);
        }
        if (item.kbd) {
            const k = document.createElement('kbd');
            k.className = 'inline-flex items-center px-1.5 h-5 text-[10px] font-mono text-gray-500 dark:text-gray-400 bg-gray-100 dark:bg-slate-700 rounded';
            k.textContent = item.kbd;
            row.appendChild(k);
        }

        row.addEventListener('mouseenter', function () {
            const idx = lastVisible.indexOf(item);
            if (idx >= 0 && idx !== cursor) {
                cursor = idx;
                paintCursor();
            }
        });
        row.addEventListener('click', function (e) {
            const idx = lastVisible.indexOf(item);
            if (idx >= 0) cursor = idx;
            pick({ keepOpen: !!(e.ctrlKey || e.metaKey) });
        });
        return row;
    }

    function paintCursor() {
        const listEl = document.getElementById('cmdk-list');
        if (!listEl) return;
        const rows = listEl.querySelectorAll('.cmdk-row');
        rows.forEach(function (row, i) {
            const isCursor = lastVisible[i] === lastVisible[cursor];
            row.classList.toggle('bg-blue-50', isCursor);
            row.classList.toggle('dark:bg-slate-700', isCursor);
            row.setAttribute('aria-selected', isCursor ? 'true' : 'false');
            if (isCursor) {
                // Auto-scroll to keep cursor in view.
                const top = row.offsetTop;
                const height = row.offsetHeight;
                if (top < listEl.scrollTop) {
                    listEl.scrollTop = top;
                } else if (top + height > listEl.scrollTop + listEl.clientHeight) {
                    listEl.scrollTop = top + height - listEl.clientHeight;
                }
            }
        });
    }

    function rankItems(items, q) {
        if (!q) return items.slice();
        return items
            .map(function (item) { return { item: item, s: score(q, item.label) }; })
            .filter(function (x) { return x.s >= 0; })
            .sort(function (a, b) { return b.s - a.s; })
            .map(function (x) { return x.item; });
    }

    function render() {
        const listEl = document.getElementById('cmdk-list');
        const input = document.getElementById('cmdk-input');
        if (!listEl || !input) return;
        const q = (input.value || '').trim();
        while (listEl.firstChild) listEl.removeChild(listEl.firstChild);
        lastVisible = [];

        const sections = [
            { title: 'Recent', items: q ? [] : getRecentItems() },
            { title: 'Boxes', items: rankItems(boxItems(), q) },
            { title: 'Gears', items: rankItems(gearItems(), q) },
            { title: 'Settings', items: rankItems(pageItems(), q) },
            { title: 'Actions', items: rankItems(actionItems(), q) },
        ];
        sections.forEach(function (s) {
            const block = buildSection(s.title, s.items);
            if (block) {
                listEl.appendChild(block);
                s.items.forEach(function (it) { lastVisible.push(it); });
            }
        });

        if (lastVisible.length === 0) {
            const empty = document.createElement('div');
            empty.className = 'px-3 py-8 text-sm text-center text-gray-500 dark:text-gray-400';
            empty.textContent = 'No matches';
            listEl.appendChild(empty);
        }
        if (cursor >= lastVisible.length) cursor = 0;
        paintCursor();
    }

    /* -------------------------------------------------------------- *
     * Open / close / pick
     * -------------------------------------------------------------- */
    let trap = null;
    function open() {
        const overlay = document.getElementById('cmdk-overlay');
        const input = document.getElementById('cmdk-input');
        if (!overlay || !input) return;
        loadCatalog();
        overlay.classList.remove('hidden');
        cursor = 0;
        input.value = '';
        render();
        if (!trap) trap = window.createFocusTrap(overlay.querySelector('.cmdk-panel') || overlay);
        trap.activate(input);
    }
    function close() {
        const overlay = document.getElementById('cmdk-overlay');
        if (!overlay) return;
        overlay.classList.add('hidden');
        if (trap) trap.deactivate();
    }

    function pick(opts) {
        opts = opts || {};
        const item = lastVisible[cursor];
        if (!item) return;
        switch (item.kind) {
            case 'box':
                pushRecent(item);
                if (opts.keepOpen) {
                    // Switch box without leaving — write the cookie via a
                    // background request, then re-render so the gear list
                    // reflects the new active box. Subsequent gear pick
                    // will navigate to the right place.
                    activeBoxID = item.box.id;
                    const input = document.getElementById('cmdk-input');
                    if (input) input.value = '';
                    cursor = 0;
                    render();
                    fetch('/?box_id=' + encodeURIComponent(item.box.id), {
                        credentials: 'same-origin',
                        headers: { 'Accept': 'application/json' },
                        redirect: 'manual',
                    }).catch(function () { /* best-effort */ });
                    return;
                }
                window.switchBox(item.box.id, '/home');
                close();
                return;
            case 'gear':
                pushRecent(item);
                close();
                window.location.assign(item.gear.path);
                return;
            case 'page':
                pushRecent(item);
                close();
                window.location.assign(item.page.path);
                return;
            case 'action':
                pushRecent(item);
                close();
                if (item.action && typeof item.action.run === 'function') {
                    setTimeout(item.action.run, 0);
                }
                return;
        }
    }

    /* -------------------------------------------------------------- *
     * Status sync (boxes)
     * -------------------------------------------------------------- */
    function applyStatus(s) {
        if (!s || !s.box_id) return;
        statusByBox.set(s.box_id, s);
        if (s.box_id === activeBoxID) {
            const chipDot = document.getElementById('box-switcher-chip-dot');
            if (chipDot) {
                const level = s.level || 'unknown';
                chipDot.className = 'inline-block w-2 h-2 rounded-full ' +
                    (STATUS_DOT_CLASSES[level] || STATUS_DOT_CLASSES.unknown);
            }
        }
        const overlay = document.getElementById('cmdk-overlay');
        if (overlay && !overlay.classList.contains('hidden')) render();
    }
    function fetchStatus() {
        fetch('/bx/api/status', { credentials: 'same-origin' })
            .then(function (r) { return r.ok ? r.json() : null; })
            .then(function (data) {
                if (!data || !Array.isArray(data.rows)) return;
                data.rows.forEach(applyStatus);
            }).catch(function () { /* best-effort */ });
    }
    let evt = null;
    function subscribeSSE() {
        if (typeof EventSource === 'undefined') return;
        if (evt) return;
        try { evt = new EventSource('/bx/api/events'); }
        catch (_) { return; }
        evt.addEventListener('box.status', function (e) {
            try { applyStatus(JSON.parse(e.data)); } catch (_) {}
        });
    }
    if (typeof EventSource !== 'undefined') {
        document.addEventListener('visibilitychange', function () {
            if (document.hidden) {
                if (evt) { evt.close(); evt = null; }
            } else {
                subscribeSSE();
            }
        });
    }

    /* -------------------------------------------------------------- *
     * Wiring
     * -------------------------------------------------------------- */
    function init() {
        const overlay = document.getElementById('cmdk-overlay');
        const input = document.getElementById('cmdk-input');
        if (!overlay || !input) return;

        overlay.addEventListener('click', function (e) {
            if (e.target === overlay) close();
        });

        input.addEventListener('input', function () {
            cursor = 0;
            render();
        });
        input.addEventListener('keydown', function (e) {
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                cursor = Math.min(cursor + 1, lastVisible.length - 1);
                paintCursor();
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                cursor = Math.max(cursor - 1, 0);
                paintCursor();
            } else if (e.key === 'Enter') {
                e.preventDefault();
                pick({ keepOpen: e.ctrlKey || e.metaKey });
            } else if (e.key === 'Escape') {
                e.preventDefault();
                close();
            }
        });

        const km = window.GearboxKeymap;
        let gPressed = false;
        let gTimer = 0;
        document.addEventListener('keydown', function (e) {
            // Cmd/Ctrl+K — open from anywhere (also inside text inputs).
            if (e.key === 'k' && km && km.isMeta(e) && !e.shiftKey && !e.altKey) {
                e.preventDefault();
                if (overlay.classList.contains('hidden')) open(); else close();
                return;
            }
            if (!km || km.isTypingTarget(e.target)) return;
            if (e.key === 'g' && !km.isMeta(e) && !e.altKey) {
                gPressed = true;
                clearTimeout(gTimer);
                gTimer = setTimeout(function () { gPressed = false; }, 800);
                return;
            }
            if (gPressed && e.key === 'b') {
                e.preventDefault();
                gPressed = false;
                open();
            }
        });

        fetchStatus();
        subscribeSSE();
    }

    // Public entry points.
    window.openCommandPalette = open;
    window.closeCommandPalette = close;
    // Back-compat: the chip's onclick handler still calls openBoxSwitcher().
    window.openBoxSwitcher = open;

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
