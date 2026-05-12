/**
 * Box switcher — keyboard-driven command palette for picking the active box.
 *
 * UI: a centered modal opened by clicking the header chip or pressing `g b`
 * (Cockpit-style). Type to filter; arrow keys to navigate; <enter> to pick;
 * <esc> to close. Live status dots are kept in sync with the Bx fleet
 * monitor over SSE — the chip dot, palette dots, and the fleet page all
 * read from the same source.
 *
 * Routing: picking a box reuses the legacy `?box_id=<id>` query convention
 * (see box-selector.js) so every existing page handler keeps working
 * without a route migration. Picking from a box-agnostic page (Bx, Home)
 * routes to Home with the new box selected; picking from a box-specific
 * page rewrites the same path with the new box_id.
 */
(function () {
    'use strict';

    const STATUS_DOT_CLASSES = {
        green:   'bg-green-500 ring-1 ring-inset ring-green-600/40',
        yellow:  'bg-amber-400 ring-1 ring-inset ring-amber-600/40',
        red:     'bg-red-500 ring-1 ring-inset ring-red-700/40',
        gray:    'bg-gray-400 ring-1 ring-inset ring-gray-500/40',
        unknown: 'bg-gray-300 ring-1 ring-inset ring-gray-400/40 animate-pulse',
    };

    /** All known boxes [{id, name}] decoded from the palette dataset. */
    let allBoxes = [];
    /** Current filter substring, lowercased. */
    let filter = '';
    /** Index of the currently-highlighted item in the filtered list. */
    let cursor = 0;
    /** Map of boxID → status row (level, latency_ms, last_checked). */
    const statusByBox = new Map();
    /** Currently active box ID, or '' if none. */
    let activeBoxID = '';

    function $(id) { return document.getElementById(id); }

    function init() {
        const overlay = $('box-switcher-overlay');
        const list = $('box-switcher-list');
        const search = $('box-switcher-search');
        if (!overlay || !list || !search) return; // nothing to wire (no boxes configured)

        try {
            allBoxes = JSON.parse(list.dataset.boxes || '[]');
        } catch (_) {
            allBoxes = [];
        }
        activeBoxID = overlay.dataset.activeBoxId || '';

        // Filter + arrow-key wiring.
        search.addEventListener('input', function () {
            filter = (search.value || '').toLowerCase().trim();
            cursor = 0;
            render();
        });
        search.addEventListener('keydown', function (e) {
            if (e.key === 'ArrowDown') {
                e.preventDefault();
                cursor = Math.min(cursor + 1, visible().length - 1);
                render();
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                cursor = Math.max(cursor - 1, 0);
                render();
            } else if (e.key === 'Enter') {
                e.preventDefault();
                const picked = visible()[cursor];
                if (picked) pick(picked.id);
            } else if (e.key === 'Escape') {
                e.preventDefault();
                close();
            }
        });

        overlay.addEventListener('click', function (e) {
            // Backdrop click closes; clicks inside the panel don't bubble here.
            if (e.target === overlay) close();
        });

        // Global keyboard shortcut: `g b` (mnemonic = "go box"). Avoids
        // conflicts with typing — only fires when no field has focus.
        let gPressed = false;
        let gTimer = 0;
        document.addEventListener('keydown', function (e) {
            const t = e.target;
            if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) {
                return;
            }
            if (e.key === 'g' && !e.metaKey && !e.ctrlKey && !e.altKey) {
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

        // Pre-populate from /bx/api/status so the palette opens with live dots.
        fetchStatus();
        // Live updates via SSE — same stream the Bx page uses.
        subscribeSSE();
    }

    function visible() {
        if (!filter) return allBoxes;
        return allBoxes.filter(function (b) {
            return b.name.toLowerCase().includes(filter);
        });
    }

    function render() {
        const list = $('box-switcher-list');
        if (!list) return;
        // Clear children.
        while (list.firstChild) list.removeChild(list.firstChild);

        const rows = visible();
        if (rows.length === 0) {
            const empty = document.createElement('li');
            empty.className = 'px-3 py-6 text-sm text-center text-gray-500 dark:text-gray-400';
            empty.textContent = 'No boxes match';
            list.appendChild(empty);
            return;
        }
        rows.forEach(function (b, i) {
            list.appendChild(buildRow(b, i === cursor));
        });
    }

    function buildRow(box, isCursor) {
        const li = document.createElement('li');
        const status = statusByBox.get(box.id);
        const level = status && status.level ? status.level : 'unknown';
        const isActive = box.id === activeBoxID;

        li.className = 'flex items-center gap-3 px-3 py-2 cursor-pointer ' +
            (isCursor ? 'bg-blue-50 dark:bg-slate-700' : 'hover:bg-gray-50 dark:hover:bg-slate-700/50');
        li.dataset.boxId = box.id;
        li.addEventListener('mouseenter', function () {
            cursor = visible().indexOf(box);
            render();
        });
        li.addEventListener('click', function () { pick(box.id); });

        const dot = document.createElement('span');
        dot.className = 'inline-block w-2.5 h-2.5 rounded-full flex-shrink-0 ' +
            (STATUS_DOT_CLASSES[level] || STATUS_DOT_CLASSES.unknown);
        li.appendChild(dot);

        const name = document.createElement('span');
        name.className = 'flex-1 text-sm text-gray-800 dark:text-gray-100 truncate';
        name.textContent = box.name;
        li.appendChild(name);

        if (status && typeof status.latency_ms === 'number' && status.latency_ms > 0) {
            const lat = document.createElement('span');
            lat.className = 'text-[11px] font-mono tabular-nums text-gray-400 dark:text-gray-500';
            lat.textContent = status.latency_ms + 'ms';
            li.appendChild(lat);
        }

        if (isActive) {
            const tag = document.createElement('span');
            tag.className = 'text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded ' +
                'bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300';
            tag.textContent = 'Active';
            li.appendChild(tag);
        }
        return li;
    }

    function open() {
        const overlay = $('box-switcher-overlay');
        const search = $('box-switcher-search');
        if (!overlay || !search) return;
        overlay.classList.remove('hidden');
        cursor = 0;
        filter = '';
        search.value = '';
        render();
        // Defer focus to let display:hidden→block flush.
        setTimeout(function () { search.focus(); }, 0);
    }

    function close() {
        const overlay = $('box-switcher-overlay');
        if (overlay) overlay.classList.add('hidden');
    }

    function pick(boxID) {
        // Navigate to the same path with ?box_id= updated. On box-agnostic
        // pages (Bx, Home, Settings) jump to Home in that box's context;
        // the chip stays visible everywhere.
        const path = window.location.pathname;
        const boxAgnostic = ['/bx', '/home', '/home/', '/settings'].some(function (p) {
            return path === p || path.startsWith(p + '/');
        });
        if (typeof window.switchBox === 'function') {
            window.switchBox(boxID, boxAgnostic ? '/home' : null);
        } else {
            const url = new URL(window.location.href);
            url.searchParams.set('box_id', boxID);
            if (boxAgnostic) url.pathname = '/home';
            window.location.assign(url.toString());
        }
    }

    /* -------------------------------------------------------------- *
     * Status sync
     * -------------------------------------------------------------- */
    function applyStatus(s) {
        if (!s || !s.box_id) return;
        statusByBox.set(s.box_id, s);
        // If this is the active box, update the chip's dot too.
        if (s.box_id === activeBoxID) {
            const chipDot = $('box-switcher-chip-dot');
            if (chipDot) {
                const level = s.level || 'unknown';
                chipDot.className = 'inline-block w-2 h-2 rounded-full ' +
                    (STATUS_DOT_CLASSES[level] || STATUS_DOT_CLASSES.unknown);
            }
        }
        // If the palette is open, re-render the affected row in place.
        const overlay = $('box-switcher-overlay');
        if (overlay && !overlay.classList.contains('hidden')) {
            render();
        }
    }

    function fetchStatus() {
        fetch('/bx/api/status', { credentials: 'same-origin' })
            .then(function (r) { return r.ok ? r.json() : null; })
            .then(function (data) {
                if (!data || !Array.isArray(data.rows)) return;
                data.rows.forEach(applyStatus);
            })
            .catch(function () { /* best-effort */ });
    }

    let evt = null;
    function subscribeSSE() {
        if (typeof EventSource === 'undefined') return;
        try {
            evt = new EventSource('/bx/api/events');
        } catch (_) { return; }
        evt.addEventListener('box.status', function (e) {
            try { applyStatus(JSON.parse(e.data)); } catch (_) {}
        });
        document.addEventListener('visibilitychange', function () {
            if (document.hidden) {
                if (evt) { evt.close(); evt = null; }
            } else if (!evt) {
                subscribeSSE();
            }
        });
    }

    // Make open() callable from the chip's onclick attribute in base.templ.
    window.openBoxSwitcher = open;

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
