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

    // `join` controls the visual separator between kbd chips for
    // multi-key shortcuts: '+' for a chord (press together), 'then' for
    // a sequence (press in order), '/' for "either of these keys".
    // Single-key rows leave `join` unset and render no separator.
    const SHORTCUTS = [
        { keys: ['?'],                    label: 'Show this help' },
        { keys: ['Cmd/Ctrl', 'K'],        label: 'Open command palette',                              join: '+' },
        { keys: ['g', 'b'],               label: 'Switch box (palette)',                              join: 'then' },
        { keys: ['/'],                    label: 'Focus the page search input' },
        { keys: ['Esc'],                  label: 'Close dialog · exit edit mode · blur input · go back' },
        { keys: ['↑', '↓'],               label: 'Navigate dialog items',                             join: '/' },
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
                if (i > 0 && s.join) {
                    const sep = document.createElement('span');
                    sep.className = 'text-[10px] text-white/50 px-0.5';
                    sep.textContent = s.join;
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

        // `Esc` — universal "back out" key. Priority order, top wins:
        //   1. A per-modal handler already called preventDefault — bail.
        //      (cmdk input, shortcuts-help overlay, etc.)
        //   2. A dialog is open — close the topmost one. Catches the
        //      pre-existing list (cmdk/help/confirm/prompt/alert) plus
        //      every `[role="dialog"]` that lacks its own Esc handler.
        //   3. A gear is in edit mode (a `.gear-cog-btn` is pressed) —
        //      click the cog to exit. Home, Metrics, and future gears.
        //   4. A focusable control has focus — blur it. Lets the user
        //      `/`→type→Esc→Esc to bounce back to the previous gear.
        //   5. Fall through to `history.back()` as a last resort.
        //
        // Browsers dropped native Esc-as-back because of accidental
        // form-data loss; the priority chain above mirrors that intent
        // by handling every "back out of something on the page" case
        // before nav is even considered.
        function isBlurrableTarget(el) {
            if (!el) return false;
            const tag = el.tagName;
            if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
            return !!el.isContentEditable;
        }
        function isVisible(el) {
            if (!el) return false;
            if (el.classList.contains('hidden')) return false;
            if (el.hasAttribute('hidden')) return false;
            const style = window.getComputedStyle(el);
            if (style.display === 'none' || style.visibility === 'hidden') return false;
            return true;
        }
        function visibleDialogs() {
            // Anything tagged as a dialog/modal by ARIA. Backstops:
            // a couple of legacy overlays that don't carry role yet
            // (#cmdk-overlay does carry it; the narrow-viewport
            // Filters sheet and the sidebar context menu don't).
            const out = [];
            document.querySelectorAll('[role="dialog"], [aria-modal="true"]').forEach(function (el) {
                if (isVisible(el)) out.push(el);
            });
            const headerContent = document.getElementById('header-page-content');
            if (headerContent && headerContent.classList.contains('filters-open')) {
                out.push(headerContent);
            }
            const ctxMenu = document.getElementById('sidebar-context-menu');
            if (ctxMenu && isVisible(ctxMenu)) out.push(ctxMenu);
            return out;
        }
        function pickTopDialog(list) {
            // Highest z-index wins; ties broken by DOM order (later =
            // on top in the painting model). Crude but good enough —
            // we just need to avoid closing a dialog underneath an
            // open one.
            let top = null;
            let topZ = -Infinity;
            let topIdx = -1;
            for (let i = 0; i < list.length; i++) {
                const z = parseInt(window.getComputedStyle(list[i]).zIndex, 10);
                const zVal = isNaN(z) ? 0 : z;
                if (zVal > topZ || (zVal === topZ && i > topIdx)) {
                    top = list[i];
                    topZ = zVal;
                    topIdx = i;
                }
            }
            return top;
        }
        function clickCloseButton(dialog) {
            // Try to invoke any existing close button so per-modal
            // cleanup (state resets, focus restoration, persistence)
            // runs. Selector list covers every close-button convention
            // I found in the templates: aria-label, generic data-*,
            // and the per-modal `data-<name>-close|dismiss` flavour.
            const buttons = dialog.querySelectorAll('button, [role="button"], a');
            for (let i = 0; i < buttons.length; i++) {
                const btn = buttons[i];
                if (btn.disabled) continue;
                const label = (btn.getAttribute('aria-label') || '').toLowerCase();
                if (label === 'close' || label === 'dismiss' || label === 'cancel') {
                    btn.click();
                    return true;
                }
                const attrs = btn.attributes;
                for (let j = 0; j < attrs.length; j++) {
                    const name = attrs[j].name;
                    if (!name.startsWith('data-')) continue;
                    if (name === 'data-close' || name === 'data-dismiss' ||
                        name.endsWith('-close') || name.endsWith('-dismiss')) {
                        btn.click();
                        return true;
                    }
                }
            }
            return false;
        }
        function closeTopmostDialog() {
            const list = visibleDialogs();
            if (list.length === 0) return false;
            const top = pickTopDialog(list);
            if (!top) return false;
            // Special-case the narrow-viewport Filters sheet: it has
            // a dedicated toggler that also flips header layout state.
            if (top.id === 'header-page-content' &&
                typeof window.toggleHeaderFilters === 'function') {
                window.toggleHeaderFilters();
                return true;
            }
            if (clickCloseButton(top)) return true;
            // Last resort: just hide it. Better than letting Esc fall
            // through to history.back() with a modal still on screen.
            top.classList.add('hidden');
            return true;
        }
        function exitGearEditMode() {
            const btn = document.querySelector('.gear-cog-btn[aria-pressed="true"]');
            if (!btn) return false;
            btn.click();
            return true;
        }
        document.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape') return;
            if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
            // A per-modal handler already consumed it (cmdk, help,
            // icon-picker, etc. all call preventDefault).
            if (e.defaultPrevented) return;
            if (closeTopmostDialog()) { e.preventDefault(); return; }
            if (exitGearEditMode())   { e.preventDefault(); return; }
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
