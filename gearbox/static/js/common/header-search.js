/**
 * header-search — controller for the unified search / filter / palette
 * input (issue #92).
 *
 * One text input drives three behaviours, picked by the current value:
 *
 *   - empty + no `>` prefix: idle. Shows the hint chips ("/ to search ·
 *     Cmd K for palette"). On focus, hint is hidden.
 *
 *   - non-empty + no `>` prefix: SEARCH/FILTER mode. If a gear has
 *     registered a filter via window.gearbox.filter.register, each
 *     keystroke fires onInput(query) and Enter (optionally) fires
 *     onSubmit(query). If no filter is registered, Enter falls back to
 *     opening a DuckDuckGo search in a new tab (preserves the old home
 *     gear behaviour).
 *
 *   - leading `>`: PALETTE mode. The slide-down panel
 *     (#header-search-panel, defined in command-palette.js / base.templ)
 *     opens beneath the input and ranks boxes / gears / settings pages
 *     / global actions / per-gear commands using the substring after
 *     the `>`. Backspace from a value of `>` (last char) drops back to
 *     search mode and closes the panel.
 *
 * Keyboard:
 *   `/`            anywhere → focus the input (skip if already typing).
 *   `Cmd/Ctrl+K`   anywhere → focus the input and prepend `>` if absent.
 *   `Esc`          from input → if palette mode, exit palette + clear;
 *                                else blur. Universal-Esc handler in
 *                                shortcut-help.js continues to handle
 *                                cases when focus is elsewhere.
 *
 * The visual chevron icon to the left of the input swaps between a
 * magnifying glass (search mode) and a chevron-right (palette mode).
 *
 * Public surface (used by other modules):
 *   window.HeaderSearch.focus()
 *   window.HeaderSearch.enterPaletteMode()   // prepend `>` and focus
 *   window.HeaderSearch.exitPaletteMode()    // clear value, close panel
 *   window.HeaderSearch.isPaletteMode()
 *   window.HeaderSearch.getQuery()           // input value w/o `>` prefix
 *   window.HeaderSearch.setValue(str)        // programmatic write
 *   window.HeaderSearch.onModeChange(fn)
 *   window.HeaderSearch.onQueryChange(fn)
 *   window.HeaderSearch.onSubmit(fn)         // user pressed Enter
 *   window.HeaderSearch.onEscape(fn)         // user pressed Esc in input
 */
(function () {
    'use strict';

    const PALETTE_PREFIX = '>';
    const DDG_BASE = 'https://duckduckgo.com/';
    const QUERY_PARAM = 'q';

    /* -------------------------------------------------------------- *
     * DOM references (resolved on init).
     * -------------------------------------------------------------- */
    let input = null;
    let hint = null;
    let clearBtn = null;
    let iconSearch = null;
    let compactBtn = null;
    let row = null;
    let wrap = null;

    /* -------------------------------------------------------------- *
     * Event listeners
     * -------------------------------------------------------------- */
    const modeListeners = [];
    const queryListeners = [];
    const submitListeners = [];
    const escapeListeners = [];
    let lastMode = 'search'; // 'search' | 'palette'

    function notify(list, arg) {
        for (let i = 0; i < list.length; i++) {
            try { list[i](arg); }
            catch (e) { console.error('header-search listener threw', e); }
        }
    }

    function currentMode() {
        if (!input) return 'search';
        return input.value.charAt(0) === PALETTE_PREFIX ? 'palette' : 'search';
    }

    function currentQuery() {
        if (!input) return '';
        const v = input.value;
        return v.charAt(0) === PALETTE_PREFIX ? v.slice(1) : v;
    }

    /* -------------------------------------------------------------- *
     * Filter registration. Gear-specific placeholders were removed per
     * issue #92 follow-up: the input reads identically on every page
     * (magnifier + hint chips), so registrations only matter for the
     * onInput/onSubmit/onClear callbacks. We still call repaint() so
     * the active-filter state can drive future visual cues.
     * -------------------------------------------------------------- */
    function applyFilterRegistration(_filter) {
        if (!input) return;
        repaint();
    }

    /* -------------------------------------------------------------- *
     * Visual mode swap.
     *
     * Search mode (no `>` prefix):
     *   - Magnifier icon visible.
     *   - Hint chips ("/ to search · Cmd K for palette") visible while
     *     the input is empty, regardless of focus.
     *   - Clear button visible while the input has content.
     *
     * Palette mode (leading `>`):
     *   - No icon at all: the `>` character in the input IS the
     *     affordance, an extra glyph would be redundant. Magnifier
     *     comes back the moment the user backspaces over the `>`.
     *   - No hint chips.
     *   - Clear button still visible while there's content.
     *
     * The wrapper deliberately has no focus ring so the box width
     * never changes between modes.
     * -------------------------------------------------------------- */
    function repaint() {
        if (!input) return;
        const mode = currentMode();
        const empty = input.value.length === 0;

        if (iconSearch) {
            if (mode === 'palette') iconSearch.classList.add('hidden');
            else iconSearch.classList.remove('hidden');
        }

        if (hint) {
            // Empty + search-mode → show the affordance. Either typing
            // or entering palette mode hides it.
            const showHint = empty && mode === 'search';
            hint.style.display = showHint ? '' : 'none';
        }

        if (clearBtn) {
            if (empty) clearBtn.classList.add('hidden');
            else clearBtn.classList.remove('hidden');
        }

        input.setAttribute('aria-expanded', mode === 'palette' ? 'true' : 'false');
        // No placeholder swap — the box reads the same on every page
        // and in every mode. Hint chips serve as the visual placeholder
        // when empty; otherwise the user's own text fills the box.

        if (mode !== lastMode) {
            lastMode = mode;
            notify(modeListeners, mode);
        }
    }

    /* -------------------------------------------------------------- *
     * Search-mode dispatch — fan-out to active filter callback.
     * -------------------------------------------------------------- */
    function dispatchSearchInput() {
        const q = currentQuery();
        notify(queryListeners, q);
        if (currentMode() !== 'search') return;
        const filter = window.gearbox && window.gearbox.filter && window.gearbox.filter.current();
        if (filter && typeof filter.onInput === 'function') {
            try { filter.onInput(q); }
            catch (e) { console.error('filter.onInput threw', e); }
        }
    }

    function dispatchSubmit() {
        const q = currentQuery();
        notify(submitListeners, q);
        if (currentMode() !== 'search') return;
        const filter = window.gearbox && window.gearbox.filter && window.gearbox.filter.current();
        if (filter && typeof filter.onSubmit === 'function') {
            try { filter.onSubmit(q); }
            catch (e) { console.error('filter.onSubmit threw', e); }
            return;
        }
        // Fallback: no gear-registered filter, no submit handler. Open
        // DuckDuckGo in a new tab (preserves the old home-page behaviour
        // so the search bar still works on any gear that doesn't take it
        // over). Empty query: just blur.
        const trimmed = q.trim();
        if (!trimmed) {
            input.blur();
            return;
        }
        const target = DDG_BASE + '?' + new URLSearchParams({ [QUERY_PARAM]: trimmed }).toString();
        window.open(target, '_blank', 'noopener');
        clear();
    }

    function dispatchClear() {
        const filter = window.gearbox && window.gearbox.filter && window.gearbox.filter.current();
        if (filter && typeof filter.onClear === 'function') {
            try { filter.onClear(); }
            catch (e) { console.error('filter.onClear threw', e); }
        }
    }

    /* -------------------------------------------------------------- *
     * Public mode / value mutators
     * -------------------------------------------------------------- */
    function enterPaletteMode() {
        if (!input) return;
        if (input.value.charAt(0) !== PALETTE_PREFIX) {
            input.value = PALETTE_PREFIX + input.value;
        }
        focus(/*selectAll*/ false);
        // Position caret at end so further typing extends the query.
        try { input.setSelectionRange(input.value.length, input.value.length); }
        catch (_) {}
        repaint();
        dispatchSearchInput();
    }

    function exitPaletteMode() {
        if (!input) return;
        if (input.value.charAt(0) === PALETTE_PREFIX) {
            input.value = '';
            repaint();
            dispatchSearchInput();
        }
    }

    function clear() {
        if (!input) return;
        input.value = '';
        repaint();
        dispatchSearchInput();
        dispatchClear();
    }

    function focus(selectAll) {
        if (!input) return;
        if (window.matchMedia('(max-width: 767px)').matches) expandCompact();
        input.focus();
        if (selectAll && typeof input.select === 'function') input.select();
    }

    function setValue(v) {
        if (!input) return;
        input.value = v == null ? '' : String(v);
        repaint();
        dispatchSearchInput();
    }

    /* -------------------------------------------------------------- *
     * Mobile compact / expanded toggle.
     * -------------------------------------------------------------- */
    function expandCompact() {
        if (!row) return;
        row.classList.remove('hidden');
        row.classList.add('flex', 'header-search-expanded');
    }
    function collapseCompact() {
        if (!row) return;
        if (!window.matchMedia('(max-width: 767px)').matches) return;
        row.classList.add('hidden');
        row.classList.remove('flex', 'header-search-expanded');
    }

    /* -------------------------------------------------------------- *
     * Wiring
     * -------------------------------------------------------------- */
    function init() {
        input       = document.getElementById('header-search-input');
        hint        = document.getElementById('header-search-hint');
        clearBtn    = document.getElementById('header-search-clear');
        iconSearch  = document.getElementById('header-search-icon-search');
        compactBtn  = document.getElementById('header-search-compact');
        row         = document.getElementById('header-search-row');
        wrap        = document.getElementById('header-search');
        if (!input) return;

        if (window.gearbox && window.gearbox.filter) {
            applyFilterRegistration(window.gearbox.filter.current());
            window.gearbox.filter.onChange(applyFilterRegistration);
        }

        input.addEventListener('input', function () {
            repaint();
            dispatchSearchInput();
        });

        input.addEventListener('focus', repaint);
        input.addEventListener('blur', function () {
            // Defer repaint a tick so click on the panel (which steals
            // focus briefly) doesn't mark the hint visible immediately.
            setTimeout(repaint, 0);
        });

        input.addEventListener('keydown', function (e) {
            if (e.key === 'Enter') {
                if (currentMode() === 'palette') return; // palette handles its own Enter
                e.preventDefault();
                dispatchSubmit();
                return;
            }
            if (e.key === 'Escape') {
                e.preventDefault();
                if (currentMode() === 'palette') {
                    exitPaletteMode();
                    return;
                }
                if (input.value.length > 0) {
                    clear();
                    return;
                }
                notify(escapeListeners);
                input.blur();
                collapseCompact();
                return;
            }
            if (e.key === 'Backspace' && input.value === PALETTE_PREFIX) {
                // Drop out of palette mode cleanly on the last-character
                // backspace (caret would otherwise leave an empty input
                // and we'd repaint to search mode anyway, but the panel
                // close needs to be deterministic).
                e.preventDefault();
                exitPaletteMode();
                return;
            }
        });

        if (clearBtn) {
            clearBtn.addEventListener('click', function (e) {
                e.preventDefault();
                clear();
                focus(false);
            });
        }

        if (compactBtn) {
            compactBtn.addEventListener('click', function (e) {
                e.preventDefault();
                expandCompact();
                input.focus();
            });
        }

        // Compact-mode: clicking outside the search collapses the row.
        document.addEventListener('click', function (e) {
            if (!window.matchMedia('(max-width: 767px)').matches) return;
            if (!row || row.classList.contains('hidden')) return;
            if (wrap && wrap.contains(e.target)) return;
            // Don't collapse while the palette panel is the click target.
            const panel = document.getElementById('header-search-panel');
            if (panel && panel.contains(e.target)) return;
            collapseCompact();
        });
        window.addEventListener('resize', function () {
            if (window.matchMedia('(min-width: 768px)').matches) {
                // md+ always shows the row; clear the compact override.
                if (row) {
                    row.classList.remove('hidden');
                    row.classList.add('flex');
                }
            } else if (document.activeElement !== input) {
                collapseCompact();
            }
        });

        const km = window.GearboxKeymap;
        document.addEventListener('keydown', function (e) {
            // Cmd/Ctrl+K — focus + enter palette mode from anywhere.
            if (km && km.isMeta(e) && !e.shiftKey && !e.altKey && (e.key === 'k' || e.key === 'K')) {
                e.preventDefault();
                enterPaletteMode();
                return;
            }
            // `/` — focus the input. Skip when the user is already
            // typing somewhere (form fields, contentEditable).
            if (e.key === '/' && !e.metaKey && !e.ctrlKey && !e.altKey) {
                if (km && km.isTypingTarget(e.target)) return;
                e.preventDefault();
                focus(true);
                return;
            }
        });

        repaint();
    }

    window.HeaderSearch = {
        focus: focus,
        enterPaletteMode: enterPaletteMode,
        exitPaletteMode: exitPaletteMode,
        isPaletteMode: function () { return currentMode() === 'palette'; },
        getQuery: currentQuery,
        setValue: setValue,
        clear: clear,
        onModeChange: function (fn) { if (typeof fn === 'function') modeListeners.push(fn); },
        onQueryChange: function (fn) { if (typeof fn === 'function') queryListeners.push(fn); },
        onSubmit: function (fn) { if (typeof fn === 'function') submitListeners.push(fn); },
        onEscape: function (fn) { if (typeof fn === 'function') escapeListeners.push(fn); },
    };

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
