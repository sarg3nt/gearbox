/**
 * Bx fleet page — client behaviors.
 *
 * The server renders the table with the current monitor snapshot baked in,
 * so the page is fully usable on first paint with JS disabled. This script
 * upgrades it with:
 *
 *   1. Row click → navigate to that box's default landing.
 *   2. SSE subscription to /bx/api/events for live dot/latency updates
 *      without polling.
 *   3. Relative-time labels for the "last checked" column.
 *
 * SSE is intentionally a polyfill-free EventSource — every browser we care
 * about supports it natively. Reconnection is automatic.
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
        green: 'Healthy',
        yellow: 'Warning',
        red: 'Critical',
        gray: 'Disabled',
        unknown: 'Checking…',
    };

    /* -------------------------------------------------------------- *
     * Row click navigation
     * -------------------------------------------------------------- */
    document.addEventListener('click', function (e) {
        const row = e.target.closest('.bx-row');
        if (!row) return;
        // Defer to nested anchors / buttons — they get their own behavior.
        if (e.target.closest('a, button')) return;
        const href = row.dataset.href;
        if (href) window.location.assign(href);
    });

    /* -------------------------------------------------------------- *
     * Relative-time formatter for "last checked"
     * -------------------------------------------------------------- */
    function relTime(ts) {
        if (!ts) return '—';
        const diff = (Date.now() - new Date(ts).getTime()) / 1000;
        if (!isFinite(diff)) return '—';
        if (diff < 5) return 'just now';
        if (diff < 60) return Math.floor(diff) + 's ago';
        if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
        if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
        return Math.floor(diff / 86400) + 'd ago';
    }
    function refreshTimestamps() {
        document.querySelectorAll('.bx-last-checked').forEach(function (cell) {
            const ts = cell.dataset.ts;
            if (!ts) return;
            cell.textContent = relTime(ts);
        });
    }
    refreshTimestamps();
    setInterval(refreshTimestamps, 5000);

    /* -------------------------------------------------------------- *
     * Live status updates via SSE
     * -------------------------------------------------------------- */
    // setEmDash replaces a cell's contents with the standard em-dash span
    // used by the server-side renderer for "no value." We build it via DOM
    // APIs (not innerHTML) to keep the page XSS-safe in the face of any
    // future SSE payload tampering.
    function setEmDash(cell) {
        while (cell.firstChild) cell.removeChild(cell.firstChild);
        const span = document.createElement('span');
        span.className = 'text-gray-400 dark:text-gray-500';
        span.textContent = '—';
        cell.appendChild(span);
    }

    function applyStatus(s) {
        if (!s || !s.box_id) return;
        const row = document.querySelector(
            '.bx-row[data-box-id="' + CSS.escape(s.box_id) + '"]'
        );
        if (!row) return;

        const dot = row.querySelector('.bx-status-dot');
        if (dot) {
            const level = s.level || 'unknown';
            // Clear color classes, then apply the new set.
            dot.className = 'inline-block w-2.5 h-2.5 rounded-full bx-status-dot ' +
                (STATUS_CLASSES[level] || STATUS_CLASSES.unknown);
            dot.dataset.level = level;
            dot.title = STATUS_TITLES[level] || level;
        }

        const lat = row.querySelector('.bx-latency');
        if (lat) {
            if (s.latency_ms > 0) {
                lat.textContent = s.latency_ms + 'ms';
                lat.classList.remove('text-gray-400', 'dark:text-gray-500');
            } else {
                setEmDash(lat);
            }
        }

        const ts = row.querySelector('.bx-last-checked');
        if (ts && s.last_checked) {
            ts.dataset.ts = s.last_checked;
            ts.textContent = relTime(s.last_checked);
        }
    }

    let evt = null;
    function connect() {
        try {
            evt = new EventSource('/bx/api/events');
        } catch (_) {
            return; // EventSource construction can throw in unusual sandboxes
        }
        evt.addEventListener('box.status', function (e) {
            try {
                applyStatus(JSON.parse(e.data));
            } catch (_) { /* ignore malformed events */ }
        });
        evt.addEventListener('error', function () {
            // Browser will auto-reconnect; nothing to do.
        });
    }
    if (typeof EventSource !== 'undefined') {
        connect();
        // Pause/resume on visibility change to avoid burning the connection
        // budget while the tab is backgrounded.
        document.addEventListener('visibilitychange', function () {
            if (document.hidden) {
                if (evt) { evt.close(); evt = null; }
            } else if (!evt) {
                connect();
            }
        });
    }
})();
