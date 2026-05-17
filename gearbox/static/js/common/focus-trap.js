/**
 * createFocusTrap — minimal modal focus trap.
 *
 * When activated, keyboard focus is constrained inside the container. Tab
 * cycles forward through the focusables; Shift+Tab cycles backward; both
 * wrap at the ends. The previously-focused element is remembered and
 * restored on deactivate, matching the W3C ARIA APG dialog pattern
 * (https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/).
 *
 * Usage:
 *   const trap = createFocusTrap(dialogEl);
 *   trap.activate(initialFocusEl);  // initialFocusEl optional
 *   ...
 *   trap.deactivate();              // restores previous focus
 *
 * Not registered as Window.FocusTrap automatically — caller decides whether
 * to expose it. This module just defines `window.createFocusTrap`.
 */
(function () {
    'use strict';

    const FOCUSABLE_SELECTOR = [
        'a[href]',
        'button:not([disabled])',
        'input:not([disabled]):not([type="hidden"])',
        'select:not([disabled])',
        'textarea:not([disabled])',
        '[tabindex]:not([tabindex="-1"])',
    ].join(', ');

    function getFocusables(container) {
        if (!container) return [];
        const nodes = container.querySelectorAll(FOCUSABLE_SELECTOR);
        // Filter to visible, non-aria-hidden elements. offsetParent === null
        // is a cheap visibility check that excludes display:none subtrees.
        return Array.prototype.filter.call(nodes, function (el) {
            if (el.offsetParent === null && el.tagName !== 'BODY') return false;
            if (el.getAttribute('aria-hidden') === 'true') return false;
            return true;
        });
    }

    function createFocusTrap(container) {
        let active = false;
        let prevFocus = null;

        function onKeydown(e) {
            if (!active || e.key !== 'Tab') return;
            const focusables = getFocusables(container);
            if (focusables.length === 0) {
                e.preventDefault();
                return;
            }
            const first = focusables[0];
            const last = focusables[focusables.length - 1];
            const current = document.activeElement;

            if (e.shiftKey) {
                if (current === first || !container.contains(current)) {
                    e.preventDefault();
                    last.focus();
                }
            } else {
                if (current === last || !container.contains(current)) {
                    e.preventDefault();
                    first.focus();
                }
            }
        }

        function activate(initialFocus) {
            if (active) return;
            active = true;
            prevFocus = document.activeElement;
            document.addEventListener('keydown', onKeydown, true);
            const target = initialFocus
                || getFocusables(container)[0]
                || container;
            // Defer focus to after any display:hidden→block flush.
            setTimeout(function () {
                if (target && typeof target.focus === 'function') {
                    target.focus();
                }
            }, 0);
        }

        function deactivate() {
            if (!active) return;
            active = false;
            document.removeEventListener('keydown', onKeydown, true);
            if (prevFocus && typeof prevFocus.focus === 'function') {
                try { prevFocus.focus(); } catch (_) { /* element may have been removed */ }
            }
            prevFocus = null;
        }

        return { activate: activate, deactivate: deactivate };
    }

    window.createFocusTrap = createFocusTrap;
})();
