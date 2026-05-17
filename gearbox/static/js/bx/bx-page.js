/**
 * Bx fleet page — client behaviors.
 *
 * The server bakes the current monitor snapshot into a JSON island
 * (#bx-data) so the grid mounts with real rows on first paint. This script
 * upgrades it with:
 *
 *   1. Tabulator-based sortable, filterable grid (UniFi house style).
 *   2. SSE subscription to /bx/api/events for live status / latency
 *      updates without polling — events flow through grid.updateData()
 *      so Tabulator handles in-place row replacement and re-sort.
 *   3. Relative-time labels for the "last checked" column, refreshed on
 *      a 5-second tick.
 *   4. Row click → navigate to that box's default landing.
 *   5. View-state filter dropdown (All / Healthy / Issues / Disabled)
 *      that AND-composes with the search box via grid.setViewFilters().
 */
(function () {
    'use strict';

    const STATUS_CLASSES = {
        green:   'bg-green-500 ring-1 ring-inset ring-green-600/40',
        yellow:  'bg-amber-400 ring-1 ring-inset ring-amber-600/40',
        red:     'bg-red-500 ring-1 ring-inset ring-red-700/40',
        gray:    'bg-gray-400 ring-1 ring-inset ring-gray-500/40',
        unknown: 'bg-gray-300 ring-1 ring-inset ring-gray-400/40 animate-pulse',
    };

    const STATUS_TITLES = {
        green:   'Healthy',
        yellow:  'Warning',
        red:     'Critical',
        gray:    'Disabled',
        unknown: 'Checking…',
    };

    // Status sort order: red > yellow > unknown > green > gray. Lets the
    // user sort the Status column in a way that surfaces problems first.
    const STATUS_RANK = { red: 0, yellow: 1, unknown: 2, green: 3, gray: 4 };

    /* -------------------------------------------------------------- *
     * Time formatters
     * -------------------------------------------------------------- */
    function relTime(ts) {
        if (!ts) return '—';
        const t = new Date(ts).getTime();
        if (!isFinite(t) || t === 0) return '—';
        const diff = (Date.now() - t) / 1000;
        if (diff < 5) return 'just now';
        if (diff < 60) return Math.floor(diff) + 's ago';
        if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
        if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
        return Math.floor(diff / 86400) + 'd ago';
    }

    /* -------------------------------------------------------------- *
     * Cell formatters
     * -------------------------------------------------------------- */
    function statusDotFormatter(cell) {
        const data = cell.getRow().getData();
        const level = data.level || 'unknown';
        const reachable = !!data.reachable;
        let title = STATUS_TITLES[level] || level;
        if (level === 'red' && !reachable) title = 'Agent unreachable';
        const cls = STATUS_CLASSES[level] || STATUS_CLASSES.unknown;
        const span = document.createElement('span');
        span.className = 'inline-block w-2.5 h-2.5 rounded-full ' + cls;
        span.title = title;
        return span;
    }

    function nameFormatter(cell) {
        const v = cell.getValue();
        const span = document.createElement('span');
        span.className = 'font-medium text-gray-900 dark:text-gray-100';
        span.textContent = v == null ? '' : String(v);
        return span;
    }

    // consoleFormatter renders a one-click "open console" icon per row.
    // The SVG is built with explicit DOM nodes (no innerHTML) to satisfy
    // the project's CSP and lint hygiene; the icon content is static
    // anyway. We don't pre-check capabilities here — the click handler
    // hits /api/console/{id}/capabilities and degrades gracefully if
    // console isn't enabled on that box. Lazy keeps the grid render
    // cheap (no per-row fetch on page load).
    function consoleFormatter(cell) {
        // Skip rendering for boxes that haven't opted in — keeps the
        // column visually quiet for fleets where most boxes don't
        // have console enabled. The data still flows through (so a
        // live row update can light it up), we just don't paint the
        // button.
        const row = cell.getRow().getData();
        if (!row || !row.console_enabled) {
            return '';
        }
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'bx-console-btn inline-flex items-center justify-center w-7 h-7 rounded text-slate-500 hover:text-emerald-500 hover:bg-slate-100 dark:hover:bg-slate-700';
        btn.title = 'Open console (>_)';
        const svgNS = 'http://www.w3.org/2000/svg';
        const svg = document.createElementNS(svgNS, 'svg');
        svg.setAttribute('class', 'w-4 h-4');
        svg.setAttribute('fill', 'none');
        svg.setAttribute('stroke', 'currentColor');
        svg.setAttribute('viewBox', '0 0 24 24');
        const path = document.createElementNS(svgNS, 'path');
        path.setAttribute('stroke-linecap', 'round');
        path.setAttribute('stroke-linejoin', 'round');
        path.setAttribute('stroke-width', '2');
        path.setAttribute('d', 'M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z');
        svg.appendChild(path);
        btn.appendChild(svg);
        return btn;
    }

    function emDashIfEmpty(cell) {
        const v = cell.getValue();
        if (v == null || v === '') {
            const span = document.createElement('span');
            span.className = 'text-gray-400 dark:text-gray-500';
            span.textContent = '—';
            return span;
        }
        return String(v);
    }

    function agentFormatter(cell) {
        const v = cell.getValue();
        if (v == null || v === '') {
            return emDashIfEmpty(cell);
        }
        const span = document.createElement('span');
        span.className = 'font-mono text-xs truncate inline-block max-w-full text-gray-600 dark:text-gray-300';
        span.textContent = String(v);
        span.title = String(v);
        return span;
    }

    function latencyFormatter(cell) {
        const v = cell.getValue();
        if (typeof v !== 'number' || v <= 0) {
            const span = document.createElement('span');
            span.className = 'text-gray-400 dark:text-gray-500';
            span.textContent = '—';
            return span;
        }
        const span = document.createElement('span');
        span.className = 'font-mono text-xs tabular-nums text-gray-600 dark:text-gray-300';
        span.textContent = v + 'ms';
        return span;
    }

    function lastCheckedFormatter(cell) {
        const v = cell.getValue();
        if (!v) {
            const span = document.createElement('span');
            span.className = 'text-gray-400 dark:text-gray-500';
            span.textContent = '—';
            return span;
        }
        const span = document.createElement('span');
        span.className = 'text-xs text-gray-500 dark:text-gray-400';
        span.textContent = relTime(v);
        return span;
    }

    /* -------------------------------------------------------------- *
     * Mount the grid
     * -------------------------------------------------------------- */
    let grid = null;
    let viewFilter = 'all';

    function loadInitialRows() {
        const node = document.getElementById('bx-data');
        if (!node) return [];
        try { return JSON.parse(node.textContent || '[]'); }
        catch (_) { return []; }
    }

    function applyViewFilter(value) {
        viewFilter = value || 'all';
        if (!grid) return;
        switch (viewFilter) {
            case 'healthy':
                grid.setViewFilters([{ field: 'level', type: '=', value: 'green' }]);
                break;
            case 'issues':
                grid.setViewFilters([{ field: 'level', type: 'in', value: ['red', 'yellow'] }]);
                break;
            case 'disabled':
                grid.setViewFilters([{ field: 'enabled', type: '=', value: false }]);
                break;
            case 'all':
            default:
                grid.clearViewFilters();
        }
    }

    function init() {
        const mount = document.getElementById('bx-grid');
        if (!mount || typeof Tabulator === 'undefined' || typeof createDataGrid !== 'function') {
            return;
        }
        const initial = loadInitialRows();

        grid = createDataGrid('#bx-grid', {
            data: initial,
            layout: 'fitColumns',
            placeholder: 'No boxes found',
            initialSort: [{ column: 'name', dir: 'asc' }],
            index: 'box_id',
            // `height: auto` lets the table grow naturally with row count
            // instead of forcing a fixed 100%-of-parent box (which made the
            // grid look stretched and produced phantom vertical scrollbars
            // when there's only one row). `maxHeight` caps it on long
            // fleets so the page doesn't become endless-scroll on a 1k-box
            // install.
            height: 'auto',
            maxHeight: '70vh',
            searchInput: '#bx-search',
            searchFields: ['name', 'location', 'agent_url'],
            columns: [
                {
                    title: '',
                    field: 'level',
                    width: 36,
                    minWidth: 36,
                    hozAlign: 'center',
                    headerSort: true,
                    sorter: function (a, b) {
                        const ra = STATUS_RANK[a] != null ? STATUS_RANK[a] : 99;
                        const rb = STATUS_RANK[b] != null ? STATUS_RANK[b] : 99;
                        return ra - rb;
                    },
                    formatter: statusDotFormatter,
                    headerTooltip: 'Status — sort by severity',
                    cssClass: 'bx-col-status',
                },
                { title: 'Name', field: 'name', formatter: nameFormatter, minWidth: 140 },
                {
                    title: '',
                    field: '_console',
                    width: 44,
                    minWidth: 44,
                    hozAlign: 'center',
                    headerSort: false,
                    formatter: consoleFormatter,
                    cellClick: function (e, cell) {
                        e.stopPropagation();
                        const d = cell.getRow().getData();
                        if (window.openConsole) {
                            window.openConsole(d.id, d.name);
                        }
                    },
                    headerTooltip: 'Open remote console (requires box_console:connect)',
                    cssClass: 'bx-col-console',
                },
                { title: 'Location', field: 'location', formatter: emDashIfEmpty, minWidth: 120 },
                { title: 'Agent', field: 'agent_url', formatter: agentFormatter, minWidth: 200 },
                {
                    title: 'Latency',
                    field: 'latency_ms',
                    hozAlign: 'right',
                    width: 110,
                    formatter: latencyFormatter,
                    sorter: 'number',
                },
                {
                    title: 'Last checked',
                    field: 'last_checked',
                    width: 140,
                    formatter: lastCheckedFormatter,
                    sorter: function (a, b) {
                        // Newest first by default. Empty values sort last.
                        const ta = a ? new Date(a).getTime() : -Infinity;
                        const tb = b ? new Date(b).getTime() : -Infinity;
                        return ta - tb;
                    },
                },
            ],
        });

        grid.on('rowClick', function (e, row) {
            const data = row.getData();
            if (!data || !data.box_id) return;
            // /home with the cookie-driven box selection — switchBox writes
            // the cookie via the box_id query and the middleware redirects
            // us back to a clean /home URL.
            window.switchBox(data.box_id, '/home');
        });

        // Cursor + hover styling for clickable rows.
        grid.on('renderComplete', function () {
            mount.querySelectorAll('.tabulator-row').forEach(function (r) {
                r.style.cursor = 'pointer';
            });
        });

        // Re-render only the relative-time column so "12s ago" stays fresh
        // without flushing the whole grid.
        setInterval(function () {
            if (!grid || !grid.getColumns) return;
            const col = grid.getColumn('last_checked');
            if (col && col.redraw) col.redraw(true);
        }, 5000);

        // Apply any preselected view filter from the dropdown (browsers
        // sometimes restore a non-default value via session history).
        const sel = document.getElementById('bx-view-filter');
        if (sel) applyViewFilter(sel.value);
    }

    // Expose for the inline onchange="" handler.
    window.bxOnViewFilterChange = applyViewFilter;

    // HeaderSearch + palette wiring (issue #92). #bx-search is hidden;
    // we proxy the global search keystrokes to it so the existing
    // datagrid filter pipeline keeps working unchanged.
    if (window.gearbox && window.gearbox.filter) {
        const bxSearch = document.getElementById('bx-search');
        window.gearbox.filter.register({
            placeholder: 'Search boxes…',
            onInput: function (q) {
                if (!bxSearch) return;
                bxSearch.value = q || '';
                bxSearch.dispatchEvent(new Event('input', { bubbles: true }));
            },
            onClear: function () {
                if (!bxSearch) return;
                bxSearch.value = '';
                bxSearch.dispatchEvent(new Event('input', { bubbles: true }));
            },
        });
    }
    if (window.gearbox && window.gearbox.commands) {
        const cmds = window.gearbox.commands;
        const viewSel = document.getElementById('bx-view-filter');
        if (viewSel) {
            Array.from(viewSel.options).forEach(function (opt) {
                cmds.register({
                    id: 'bx.view.' + opt.value,
                    label: 'View: ' + opt.text,
                    subtitle: 'Filter boxes by status',
                    group: 'Bx',
                    run: function () { viewSel.value = opt.value; applyViewFilter(opt.value); },
                });
            });
        }
        cmds.register({
            id: 'bx.add',
            label: 'Add box…',
            subtitle: 'Open the new-box dialog',
            group: 'Bx',
            run: function () { window.location.assign('/settings/boxes/new'); },
        });

        // One "Console: <box>" entry per row in the initial snapshot.
        // The palette is the keyboard path into console; the tile icon
        // is the mouse path. Both ultimately call window.openConsole.
        // We don't filter by per-box console_enabled here — the openConsole
        // flow itself shows a clear "disabled on agent" if it isn't ready.
        // Filtering up-front would require a capabilities fetch per box on
        // page load, which is wasteful for a feature most operators rarely use.
        const snapshot = loadInitialRows();
        if (Array.isArray(snapshot)) {
            snapshot.forEach(function (row) {
                // Don't pollute the palette with disabled boxes — if
                // console isn't on for this box, the entry would
                // just produce a "disabled" error when clicked.
                if (!row.console_enabled) return;
                cmds.register({
                    id: 'bx.console.' + row.id,
                    label: 'Console: ' + (row.name || row.id),
                    subtitle: 'Open a remote shell on this box',
                    group: 'Console',
                    run: function () {
                        if (window.openConsole) window.openConsole(row.id, row.name);
                    },
                });
            });
        }
    }

    /* -------------------------------------------------------------- *
     * Live status updates via SSE
     * -------------------------------------------------------------- */
    function applySSE(payload) {
        if (!grid || !payload || !payload.box_id) return;
        // updateData is upsert-by-index when `index` is set (we set it to
        // box_id above). If the box is brand new we addData instead.
        const existing = grid.getRow(payload.box_id);
        if (existing) {
            grid.updateData([payload]);
        } else {
            grid.addData([payload]);
        }
    }

    let evt = null;
    function connect() {
        try {
            evt = new EventSource('/bx/api/events');
        } catch (_) {
            return;
        }
        evt.addEventListener('box.status', function (e) {
            try { applySSE(JSON.parse(e.data)); }
            catch (_) { /* ignore malformed events */ }
        });
        evt.addEventListener('error', function () {
            // Browser auto-reconnects; nothing to do.
        });
    }

    function bootSSE() {
        if (typeof EventSource === 'undefined') return;
        connect();
        document.addEventListener('visibilitychange', function () {
            if (document.hidden) {
                if (evt) { evt.close(); evt = null; }
            } else if (!evt) {
                connect();
            }
        });
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', function () {
            init();
            bootSSE();
        });
    } else {
        init();
        bootSSE();
    }
})();
