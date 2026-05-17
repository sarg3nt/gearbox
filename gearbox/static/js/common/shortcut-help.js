/**
 * Shortcut Help — `?`-opens reference (issue #92 redesign).
 *
 * Now a full-window smoked-glass overlay (no panel, no card, no border).
 * Shortcuts render in centered two-column grid directly on the blurred
 * backdrop. Close with `?`, `Esc`, or click anywhere on the backdrop.
 *
 * Also owns the universal-Esc handler: priority order top-to-bottom is
 *   1. A per-modal handler already called preventDefault → bail.
 *   2. A visible dialog → close the topmost (covers cmdk, alert,
 *      confirm, prompt, plus the mobile Filters sheet and sidebar
 *      context menu).
 *   3. A gear is in edit mode (a `.gear-cog-btn[aria-pressed="true"]`)
 *      → click the cog to exit.
 *   4. A focusable control has focus → blur.
 *   5. Fall through to history.back() as a last resort.
 *
 * Note: `/`-to-focus-the-page-filter is no longer here. The unified
 * HeaderSearch input owns the `/` shortcut and dispatches keystrokes
 * to whatever filter the current gear registered via
 * window.gearbox.filter.register (see common/gear-commands.js).
 */
(function () {
    'use strict';

    // Multi-key separator: '+' = press together, 'then' = press in
    // sequence, '/' = either. Single-key rows leave join unset.
    const SHORTCUTS = [
        { keys: ['?'],                  label: 'Show this help' },
        { keys: ['/'],                  label: 'Focus the search bar' },
        { keys: ['Cmd/Ctrl', 'K'],      label: 'Open command palette',                                 join: '+' },
        { keys: ['>'],                  label: 'In search bar: switch to palette mode' },
        { keys: ['g', 'b'],             label: 'Open palette (legacy chord)',                          join: 'then' },
        { keys: ['Esc'],                label: 'Close dialog · exit edit mode · clear search · go back' },
        { keys: ['↑', '↓'],             label: 'Navigate palette items',                               join: '/' },
        { keys: ['↵'],                  label: 'Select / run highlighted item' },
        { keys: ['Tab'],                label: 'Cycle focus within a dialog' },
        { keys: ['Ctrl+↵'],             label: 'In palette: switch box but keep palette open' },
    ];

    function renderShortcuts() {
        const list = document.getElementById('shortcuts-help-list');
        if (!list) return;
        while (list.firstChild) list.removeChild(list.firstChild);
        const km = window.GearboxKeymap;
        SHORTCUTS.forEach(function (s) {
            const row = document.createElement('div');
            row.className = 'flex items-center justify-between gap-3 px-2 py-2 border-b border-white/5 last:border-b-0';
            const label = document.createElement('span');
            label.className = 'text-sm text-white/85';
            label.textContent = s.label;
            row.appendChild(label);

            const keys = document.createElement('span');
            keys.className = 'flex items-center gap-1';
            s.keys.forEach(function (k, i) {
                if (i > 0 && s.join) {
                    const sep = document.createElement('span');
                    sep.className = 'text-[10px] text-white/40 px-0.5';
                    sep.textContent = s.join;
                    keys.appendChild(sep);
                }
                const kbd = document.createElement('kbd');
                kbd.className = 'inline-flex items-center justify-center min-w-[1.75rem] px-2 h-6 text-[11px] font-mono ' +
                    'text-white/90 bg-white/10 border border-white/20 rounded';
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

    let returnFocusEl = null;

    function open() {
        const overlay = document.getElementById('shortcuts-help-overlay');
        if (!overlay) return;
        renderShortcuts();
        // Remember whoever had focus so we can hand it back on close.
        returnFocusEl = document.activeElement;
        overlay.classList.remove('hidden');
        // The overlay is role="dialog" aria-modal="true"; move focus
        // into it so the dialog is announced by screen readers and
        // subsequent Tab/Esc keystrokes land here, not on the page
        // behind. The overlay has no interactive controls (clicking
        // anywhere closes), so a single focusable container is enough
        // — no formal trap needed (Copilot a11y review).
        overlay.setAttribute('tabindex', '-1');
        try { overlay.focus({ preventScroll: true }); }
        catch (_) { overlay.focus(); }
    }
    function close() {
        const overlay = document.getElementById('shortcuts-help-overlay');
        if (!overlay) return;
        overlay.classList.add('hidden');
        // Restore focus to whatever opened the overlay (typically the
        // page body or a button), so keyboard users don't get dumped
        // back at the top of the document.
        if (returnFocusEl && typeof returnFocusEl.focus === 'function') {
            try { returnFocusEl.focus({ preventScroll: true }); }
            catch (_) { returnFocusEl.focus(); }
        }
        returnFocusEl = null;
    }

    function init() {
        const overlay = document.getElementById('shortcuts-help-overlay');
        if (!overlay) return;

        overlay.addEventListener('click', function (e) {
            // Click anywhere on the backdrop closes the help — there
            // is no panel and no close button. The shortcut rows
            // themselves are non-interactive text so clicking them
            // is treated as backdrop, too.
            close();
        });

        const km = window.GearboxKeymap;
        document.addEventListener('keydown', function (e) {
            if (e.key === '?' && !e.metaKey && !e.ctrlKey && !e.altKey) {
                if (km && km.isTypingTarget(e.target)) return;
                e.preventDefault();
                if (overlay.classList.contains('hidden')) open(); else close();
            }
        });

        // Universal Esc handler — unchanged behaviour from the prior
        // implementation, just kept here since this file already owns
        // the help overlay's open state.
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
            if (top.id === 'header-page-content' &&
                typeof window.toggleHeaderFilters === 'function') {
                window.toggleHeaderFilters();
                return true;
            }
            // Help overlay has no close button — handle directly.
            if (top.id === 'shortcuts-help-overlay') {
                close();
                return true;
            }
            if (clickCloseButton(top)) return true;
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
            if (e.defaultPrevented) return;
            if (closeTopmostDialog()) { e.preventDefault(); return; }
            if (exitGearEditMode())   { e.preventDefault(); return; }
            const el = document.activeElement;
            if (isBlurrableTarget(el)) {
                el.blur();
                e.preventDefault();
                return;
            }
            if (window.history.length > 1) {
                e.preventDefault();
                window.history.back();
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
