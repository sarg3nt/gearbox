/**
 * Command Palette — slide-down panel anchored to the HeaderSearch input
 * (issue #92 redesign).
 *
 * No dedicated input anymore: the unified header-search input drives the
 * palette. When its value starts with `>`, the panel below opens and
 * lists boxes / gears / settings pages / global actions / per-gear
 * commands ranked against the substring after `>`. Backspace from a
 * single-`>` value closes the panel (handled by header-search.js).
 *
 * Visuals:
 *   - position: fixed, just below the header (top: 55px), horizontally
 *     centered on the HeaderSearch input. No backdrop blur, no dim —
 *     the rest of the page stays visible and interactive (VS Code).
 *   - Width matches the search input (clamped to its computed width).
 *
 * Special interactions:
 *   - Enter on Box → switch to that box and close. Ctrl+Enter keeps the
 *     palette open with the active box flipped (rapid drill-down).
 *   - Enter on Gear / Page → navigate.
 *   - Enter on Action / per-gear Command → invoke handler.
 *
 * Per-gear commands come from window.gearbox.commands.list() (registered
 * via window.gearbox.commands.register in each gear's page script).
 *
 * Recents: last 5 picks of any kind, stored in localStorage.
 */
(function () {
    'use strict';

    const RECENTS_KEY = 'gearbox-cmdk-recents';
    const RECENTS_MAX = 5;

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
    let boxes = [];
    let gears = [];
    let pages = [];
    let actions = [];
    const statusByBox = new Map();
    let activeBoxID = '';
    let catalogLoaded = false;

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
        catalogLoaded = true;
    }

    /* -------------------------------------------------------------- *
     * Quick actions (global)
     * -------------------------------------------------------------- */
    function buildActions() {
        return [
            { id: 'theme:light',     label: 'Theme: Light',     subtitle: 'Force the light theme for this device',
              run: function () { if (typeof setTheme === 'function') setTheme('light'); } },
            { id: 'theme:dark',      label: 'Theme: Dark',      subtitle: 'Force the dark theme for this device',
              run: function () { if (typeof setTheme === 'function') setTheme('dark'); } },
            { id: 'theme:system',    label: 'Theme: System',    subtitle: 'Match the OS theme preference',
              run: function () { if (typeof setTheme === 'function') setTheme('system'); } },
            { id: 'sidebar:toggle',  label: 'Toggle sidebar',   subtitle: 'Collapse or expand the navigation sidebar',
              run: function () { if (typeof toggleSidebar === 'function') toggleSidebar(); } },
            { id: 'sidebar:reorder', label: 'Reorder sidebar',  subtitle: 'Enter drag-to-reorder mode for the gear list',
              run: function () { if (typeof toggleSidebarEditMode === 'function') toggleSidebarEditMode(); } },
            { id: 'shortcuts:show',  label: 'Show keyboard shortcuts', subtitle: 'Open the shortcut reference', kbd: '?',
              run: function () { if (typeof window.openShortcutHelp === 'function') window.openShortcutHelp(); } },
            { id: 'logout',          label: 'Log out',          subtitle: 'End the current session',
              run: function () {
                  const f = document.createElement('form');
                  f.method = 'POST'; f.action = '/logout';
                  document.body.appendChild(f); f.submit();
              } },
        ];
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
        try { localStorage.setItem(RECENTS_KEY, JSON.stringify(filtered.slice(0, RECENTS_MAX))); }
        catch (_) {}
    }
    function getRecentItems() {
        return loadRecents()
            .map(function (r) { return lookupItem(r.kind, r.id); })
            .filter(Boolean);
    }

    /* -------------------------------------------------------------- *
     * Item factories
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
    function gearCommandItems() {
        if (!window.gearbox || !window.gearbox.commands) return [];
        return window.gearbox.commands.list().map(function (c) {
            return {
                kind: 'gear-command', id: 'cmd:' + c.id, label: c.label,
                subtitle: c.subtitle || '', kbd: c.kbd || '', group: c.group || 'Page',
                command: c,
            };
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
            case 'gear-command': {
                // gear-command lookup intentionally skipped — these are
                // page-scoped and won't survive recents across navigation
                // anyway. Returning null filters the recent entry.
                return null;
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
        if (idx > 0 && (l[idx - 1] === ' ' || l[idx - 1] === '/' || l[idx - 1] === '-')) return 700;
        if (idx !== -1) return 500 - idx;
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
     * Render
     * -------------------------------------------------------------- */
    let cursor = 0;
    let lastVisible = [];

    function makeStatusDot(level) {
        const dot = document.createElement('span');
        dot.className = 'inline-block w-2.5 h-2.5 rounded-full flex-shrink-0 ' +
            (STATUS_DOT_CLASSES[level] || STATUS_DOT_CLASSES.unknown);
        return dot;
    }
    function makeKindBadge(kind) {
        const cls = (
            kind === 'gear' ? 'bg-blue-400 dark:bg-blue-500' :
            kind === 'gear-command' ? 'bg-emerald-400 dark:bg-emerald-500' :
            kind === 'page' ? 'bg-purple-400 dark:bg-purple-500' :
            'bg-gray-300 dark:bg-gray-600'
        );
        const b = document.createElement('span');
        b.className = 'inline-block w-2 h-2 rounded-sm flex-shrink-0 ' + cls;
        return b;
    }

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
        row.addEventListener('mousedown', function (e) {
            // mousedown not click — click fires after blur on the input,
            // which would race with our re-render. mousedown lets us
            // capture the pick before the focus-leave repaints empty.
            e.preventDefault();
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

    function groupByGearCommands(items) {
        // Group gear-command items by their `group` field. Returns an
        // object keyed by group name → items[].
        const out = {};
        items.forEach(function (it) {
            const g = (it.command && it.command.group) || 'Page';
            if (!out[g]) out[g] = [];
            out[g].push(it);
        });
        return out;
    }

    function render() {
        const listEl = document.getElementById('cmdk-list');
        if (!listEl) return;
        if (!catalogLoaded) loadCatalog();

        const q = (window.HeaderSearch && window.HeaderSearch.getQuery() || '').trim();
        while (listEl.firstChild) listEl.removeChild(listEl.firstChild);
        lastVisible = [];

        const sections = [];

        // Gear-scoped commands are grouped by their `group` so a page
        // with multiple categories of commands (e.g. "Logs: source"
        // vs "Logs: actions") can split itself sensibly.
        const gearCmds = rankItems(gearCommandItems(), q);
        const groups = groupByGearCommands(gearCmds);
        Object.keys(groups).forEach(function (groupName) {
            sections.push({ title: groupName, items: groups[groupName] });
        });

        if (!q) sections.unshift({ title: 'Recent', items: getRecentItems() });
        sections.push({ title: 'Boxes',    items: rankItems(boxItems(), q) });
        sections.push({ title: 'Gears',    items: rankItems(gearItems(), q) });
        sections.push({ title: 'Settings', items: rankItems(pageItems(), q) });
        sections.push({ title: 'Actions',  items: rankItems(actionItems(), q) });

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
     * Open / close / position
     * -------------------------------------------------------------- */
    function panel() { return document.getElementById('header-search-panel'); }

    function position() {
        const p = panel();
        const wrap = document.getElementById('header-search');
        if (!p || !wrap) return;
        const rect = wrap.getBoundingClientRect();
        // Center the panel on the search input. Width tracks the search
        // box but is clamped to a 640px max so it remains readable on
        // wide monitors.
        const width = Math.min(rect.width, 640);
        p.style.left = (rect.left + rect.width / 2 - width / 2) + 'px';
        p.style.width = width + 'px';
        p.style.top = (rect.bottom + 6) + 'px';
    }

    function isOpen() {
        const p = panel();
        return !!(p && !p.classList.contains('hidden'));
    }

    function open() {
        const p = panel();
        if (!p) return;
        if (!catalogLoaded) loadCatalog();
        cursor = 0;
        p.classList.remove('hidden');
        position();
        render();
    }
    function close() {
        const p = panel();
        if (!p) return;
        p.classList.add('hidden');
    }

    /* -------------------------------------------------------------- *
     * Pick
     * -------------------------------------------------------------- */
    function pick(opts) {
        opts = opts || {};
        const item = lastVisible[cursor];
        if (!item) return;
        switch (item.kind) {
            case 'box':
                pushRecent(item);
                if (opts.keepOpen) {
                    activeBoxID = item.box.id;
                    if (window.HeaderSearch) window.HeaderSearch.setValue('>');
                    cursor = 0;
                    render();
                    fetch('/?box_id=' + encodeURIComponent(item.box.id), {
                        credentials: 'same-origin',
                        headers: { 'Accept': 'application/json' },
                        redirect: 'manual',
                    }).catch(function () {});
                    return;
                }
                exitAndDo(function () { window.switchBox(item.box.id, '/home'); });
                return;
            case 'gear':
                pushRecent(item);
                exitAndDo(function () { window.location.assign(item.gear.path); });
                return;
            case 'page':
                pushRecent(item);
                exitAndDo(function () { window.location.assign(item.page.path); });
                return;
            case 'action':
                pushRecent(item);
                exitAndDo(function () {
                    if (item.action && typeof item.action.run === 'function') item.action.run();
                });
                return;
            case 'gear-command':
                // Per-page commands don't go in recents (the page may
                // not be the same on next visit). Run and close.
                exitAndDo(function () {
                    if (item.command && typeof item.command.run === 'function') item.command.run();
                });
                return;
        }
    }

    function exitAndDo(fn) {
        if (window.HeaderSearch) window.HeaderSearch.exitPaletteMode();
        close();
        setTimeout(fn, 0);
    }

    /* -------------------------------------------------------------- *
     * Status sync (boxes — unchanged from the prior implementation)
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
        if (isOpen()) render();
    }
    function fetchStatus() {
        fetch('/bx/api/status', { credentials: 'same-origin' })
            .then(function (r) { return r.ok ? r.json() : null; })
            .then(function (data) {
                if (!data || !Array.isArray(data.rows)) return;
                data.rows.forEach(applyStatus);
            }).catch(function () {});
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
     * Wiring — drive open/close from HeaderSearch mode + Enter from
     * the input itself (palette mode preempts header-search.dispatchSubmit).
     * -------------------------------------------------------------- */
    function init() {
        if (!window.HeaderSearch) {
            // header-search.js failed to load; nothing to attach.
            return;
        }
        window.HeaderSearch.onModeChange(function (mode) {
            if (mode === 'palette') open(); else close();
        });
        window.HeaderSearch.onQueryChange(function () {
            if (!isOpen()) return;
            cursor = 0;
            render();
        });

        const input = document.getElementById('header-search-input');
        if (input) {
            input.addEventListener('keydown', function (e) {
                if (!isOpen()) return;
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
                }
            });
        }

        // Reposition on viewport changes so the panel stays glued to
        // the search box.
        window.addEventListener('resize', function () { if (isOpen()) position(); });
        window.addEventListener('scroll', function () { if (isOpen()) position(); }, true);

        // Click outside → close. Click on the panel itself doesn't bubble
        // through (we handle that via the row's own mousedown handler).
        document.addEventListener('mousedown', function (e) {
            if (!isOpen()) return;
            const p = panel();
            const wrap = document.getElementById('header-search');
            if (p && p.contains(e.target)) return;
            if (wrap && wrap.contains(e.target)) return;
            // Outside click: drop palette mode (also closes the panel).
            window.HeaderSearch.exitPaletteMode();
        });

        // Live updates when a gear registers commands while the panel
        // is open (e.g. lazy page-init wins the race).
        if (window.gearbox && window.gearbox.commands) {
            window.gearbox.commands.onChange(function () { if (isOpen()) render(); });
        }

        // `g b` legacy shortcut: still works, but now just enters
        // palette mode (Cmd+K's job).
        const km = window.GearboxKeymap;
        let gPressed = false;
        let gTimer = 0;
        document.addEventListener('keydown', function (e) {
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
                window.HeaderSearch.enterPaletteMode();
            }
        });

        fetchStatus();
        subscribeSSE();
    }

    /* -------------------------------------------------------------- *
     * Public surface
     * -------------------------------------------------------------- */
    window.openCommandPalette = function () {
        if (window.HeaderSearch) window.HeaderSearch.enterPaletteMode();
    };
    window.closeCommandPalette = function () {
        if (window.HeaderSearch) window.HeaderSearch.exitPaletteMode();
    };
    // Box-switcher chip still calls openBoxSwitcher() — keep the alias.
    window.openBoxSwitcher = window.openCommandPalette;

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
