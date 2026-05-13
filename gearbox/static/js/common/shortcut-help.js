/**
 * Shortcut Help — "?" overlay with the global keyboard shortcut reference.
 *
 * Smoked-glass styling per the issue: full-viewport backdrop, blurred
 * background; content panel uses bg-white/5 + backdrop-blur so the page
 * behind shows through. Open with `?` (Shift+/) from anywhere except
 * inside a text input. Close with `?`, Esc, or click outside.
 *
 * The shortcut list is data-driven so future additions don't need to
 * touch the HTML — extend SHORTCUTS and the table re-renders on open.
 */
(function () {
    'use strict';

    const SHORTCUTS = [
        { keys: ['?'],                    label: 'Show this help' },
        { keys: ['Cmd/Ctrl', 'K'],        label: 'Open command palette' },
        { keys: ['g', 'b'],               label: 'Switch box (palette)' },
        { keys: ['/'],                    label: 'Focus the page search input' },
        { keys: ['Esc'],                  label: 'Close dialog · or blur input · or go back' },
        { keys: ['↑', '↓'],               label: 'Navigate dialog items' },
        { keys: ['↵'],                    label: 'Select highlighted item' },
        { keys: ['Tab'],                  label: 'Cycle focus within a dialog' },
        { keys: ['Ctrl+↵'],               label: 'In palette: switch box but keep palette open' },
    ];

    let trap = null;

    function renderShortcuts() {
        const list = document.getElementById('shortcuts-help-list');
        if (!list) return;
        while (list.firstChild) list.removeChild(list.firstChild);
        const km = window.GearboxKeymap;
        SHORTCUTS.forEach(function (s) {
            const row = document.createElement('div');
            row.className = 'flex items-center justify-between gap-3 px-4 py-2.5 rounded-lg hover:bg-white/5 transition-colors';
            const label = document.createElement('span');
            label.className = 'text-sm text-white/90';
            label.textContent = s.label;
            row.appendChild(label);

            const keys = document.createElement('span');
            keys.className = 'flex items-center gap-1';
            s.keys.forEach(function (k, i) {
                if (i > 0 && (s.keys.length > 1)) {
                    // Inter-key separator: "+" for chord, " then " for sequence.
                    const sep = document.createElement('span');
                    sep.className = 'text-[10px] text-white/40 px-0.5';
                    sep.textContent = (k === '↓' || k === '↑' || /^[A-Z]$/.test(s.keys[0])) ? ' ' : ' ';
                    keys.appendChild(sep);
                }
                const kbd = document.createElement('kbd');
                kbd.className = 'inline-flex items-center justify-center min-w-[1.75rem] px-2 h-6 text-[11px] font-mono ' +
                    'text-white/90 bg-white/10 border border-white/20 rounded';
                // Map common labels to the right symbol on macOS.
                if (km && km.isMacOS() && k === 'Cmd/Ctrl') {
                    kbd.textContent = '⌘';
                } else if (k === 'Cmd/Ctrl') {
                    kbd.textContent = 'Ctrl';
                } else {
                    kbd.textContent = k;
                }
                keys.appendChild(kbd);
            });
            row.appendChild(keys);
            list.appendChild(row);
        });
    }

    function open() {
        const overlay = document.getElementById('shortcuts-help-overlay');
        if (!overlay) return;
        renderShortcuts();
        overlay.classList.remove('hidden');
        const panel = overlay.querySelector('.shortcuts-help-panel');
        if (!trap) trap = window.createFocusTrap(panel || overlay);
        trap.activate(panel || overlay);
    }
    function close() {
        const overlay = document.getElementById('shortcuts-help-overlay');
        if (!overlay) return;
        overlay.classList.add('hidden');
        if (trap) trap.deactivate();
    }

    function init() {
        const overlay = document.getElementById('shortcuts-help-overlay');
        if (!overlay) return;

        overlay.addEventListener('click', function (e) {
            if (e.target === overlay) close();
        });
        overlay.addEventListener('keydown', function (e) {
            if (e.key === 'Escape') {
                e.preventDefault();
                close();
            }
        });

        const km = window.GearboxKeymap;
        document.addEventListener('keydown', function (e) {
            // `?` (Shift+/) — global. Skipped when typing in inputs.
            if (e.key === '?' && !e.metaKey && !e.ctrlKey && !e.altKey) {
                if (km && km.isTypingTarget(e.target)) return;
                e.preventDefault();
                if (overlay.classList.contains('hidden')) open(); else close();
            }
        });

        // `Esc` — universal "back out" key.
        //   1. If a focusable control (input/select/textarea/contenteditable)
        //      has focus, blur it. This is the primary motivator: a user
        //      who pressed `/` and typed a filter can hit Esc to leave the
        //      input, freeing `?` / `Cmd+K` / `/` to work again.
        //   2. If nothing is focused and no overlay/dialog is open, fall
        //      back to `history.back()`. Lets the user `/`→type→Esc→Esc
        //      to bounce back to the previous gear without reaching for
        //      the mouse. Skips when an overlay is up (those have their
        //      own Esc handlers that close them).
        //
        // The browsers stopped doing Esc-as-back natively because of
        // accidental form-data loss; we sidestep that by requiring the
        // page to be in a clean state (no focused input) first.
        function anyOverlayOpen() {
            const ids = [
                'cmdk-overlay', 'shortcuts-help-overlay',
                'confirm-dialog', 'prompt-dialog', 'alert-dialog',
            ];
            for (let i = 0; i < ids.length; i++) {
                const el = document.getElementById(ids[i]);
                if (el && !el.classList.contains('hidden')) return true;
            }
            return false;
        }
        function isBlurrableTarget(el) {
            if (!el) return false;
            const tag = el.tagName;
            if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
            return !!el.isContentEditable;
        }
        document.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape') return;
            if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
            // Let dialogs handle their own Esc.
            if (anyOverlayOpen()) return;
            const el = document.activeElement;
            if (isBlurrableTarget(el)) {
                el.blur();
                e.preventDefault();
                return;
            }
            // Same-origin back nav only — `document.referrer` is empty
            // on direct loads, so checking history.length > 1 is the
            // best proxy we have in-browser.
            if (window.history.length > 1) {
                e.preventDefault();
                window.history.back();
            }
        });

        // `/` — focus the page's primary search/filter input if one
        // exists. Universal "start searching" convention (GitHub, Linear,
        // Slack). The candidate list is in priority order: each page is
        // expected to have *one* primary filter, and the first existing
        // match wins. Pages that have no filter (Metrics, Settings)
        // simply do nothing on `/`. The Home page wires its own `/`
        // handler in static/js/gears/home.js; it fires first and the
        // isTypingTarget guard below makes this listener bail once its
        // input is focused, so there's no double-trigger.
        const FILTER_CANDIDATES = [
            '#bx-search',              // Bx fleet
            '#pkg-search',             // OS Updates packages
            '#filter-input',           // Logs
            '#alert-search',           // Alerts
            '#service-filter',         // Services
            '#global-backend-filter',  // HAProxy overview (backends)
            '#blocked-ip-search',      // Security dashboard
            '#viz-filter',             // Traffic visualization
            '#search-entity',          // Disabled entities (admin)
            '#home-search-input',      // Home bookmark search (also bound by home.js)
        ];
        document.addEventListener('keydown', function (e) {
            if (e.key !== '/') return;
            if (km && km.isTypingTarget(e.target)) return;
            for (let i = 0; i < FILTER_CANDIDATES.length; i++) {
                const el = document.querySelector(FILTER_CANDIDATES[i]);
                if (!el) continue;
                e.preventDefault();
                // If the target sits inside #header-page-content and the
                // viewport is below md, the page-toolbar is hidden behind
                // the Filters pill. Pop the sheet open first so the focus
                // call lands on something visible.
                const headerContent = document.getElementById('header-page-content');
                const narrow = window.matchMedia('(max-width: 767px)').matches;
                if (narrow && headerContent && headerContent.contains(el) &&
                    !headerContent.classList.contains('filters-open') &&
                    typeof window.toggleHeaderFilters === 'function') {
                    window.toggleHeaderFilters();
                }
                // Defer focus so the popover paints before focus moves —
                // focusing a display:none element silently fails.
                setTimeout(function () {
                    el.focus();
                    if (typeof el.select === 'function') el.select();
                }, 0);
                return;
            }
        });
    }

    window.openShortcutHelp = open;
    window.closeShortcutHelp = close;

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
