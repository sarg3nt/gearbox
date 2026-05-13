/**
 * keymap — shared helpers for global keyboard shortcuts.
 *
 * Centralizes the "ignore when typing in an input" guard and the
 * cross-platform meta/ctrl detection so individual shortcut modules don't
 * have to re-derive these. Each module still wires its own keydown
 * listener — this is just shared utilities, not a registry.
 */
(function () {
    'use strict';

    /**
     * isTypingTarget — returns true if the event's target is something the
     * user is actively typing into (and where global shortcuts should
     * generally not fire). Includes inputs, textareas, contentEditable
     * regions, and any descendant of a contentEditable host.
     */
    function isTypingTarget(target) {
        if (!target) return false;
        const tag = target.tagName;
        if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
            // Allow shortcuts in non-text inputs (checkboxes, buttons via input).
            const t = (target.type || '').toLowerCase();
            const nonText = (
                t === 'checkbox' || t === 'radio' || t === 'button' ||
                t === 'submit' || t === 'reset' || t === 'range' ||
                t === 'color' || t === 'file'
            );
            return !nonText;
        }
        if (target.isContentEditable) return true;
        // Walk up to look for a contentEditable host.
        let n = target;
        while (n && n !== document.body) {
            if (n.isContentEditable) return true;
            n = n.parentNode;
        }
        return false;
    }

    /**
     * isMeta — cross-platform "primary modifier" check. Mac uses Cmd
     * (metaKey); Windows/Linux use Ctrl. Treat them the same.
     */
    function isMeta(e) {
        return !!(e.metaKey || e.ctrlKey);
    }

    /**
     * isMacOS — cosmetic detection used to render ⌘ vs Ctrl in shortcut hints.
     * Defaults to false on environments without navigator.platform.
     */
    function isMacOS() {
        const p = (navigator.platform || '').toLowerCase();
        return p.indexOf('mac') !== -1;
    }

    /**
     * keyForDisplay — render a meta-key chord with the right symbol.
     * `keyForDisplay('K')` → '⌘K' on Mac, 'Ctrl+K' elsewhere.
     */
    function keyForDisplay(key) {
        return (isMacOS() ? '⌘' : 'Ctrl+') + key;
    }

    window.GearboxKeymap = {
        isTypingTarget: isTypingTarget,
        isMeta: isMeta,
        isMacOS: isMacOS,
        keyForDisplay: keyForDisplay,
    };
})();
