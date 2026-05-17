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
     * The hint chips overlay the input via absolute positioning and
     * use Tailwind's `peer-placeholder-shown:opacity-100 peer-focus:
     * opacity-0` to drive their own visibility — they appear only
     * when the input is empty AND not focused. No JS needed for them.
     *
     * What this function still owns:
     *   - Magnifier icon: hidden (visibility:hidden) in palette mode
     *     so the `>` character is the only affordance, but
     *     space-preserved so the row width doesn't change.
     *   - Clear button: visible iff the input has content; uses
     *     `invisible` not `hidden` so its slot is always reserved
     *     (otherwise the row would grow by ~24px on first keystroke).
     *   - aria-expanded mirror for the palette panel.
     *   - mode-change notifications for the palette controller.
     *
     * The wrapper has no focus ring, so the box looks identical
     * across idle / focused / typing / palette states.
     * -------------------------------------------------------------- */
    function repaint() {
        if (!input) return;
        const mode = currentMode();
        const empty = input.value.length === 0;

        if (iconSearch) {
            iconSearch.classList.toggle('invisible', mode === 'palette');
        }

        if (clearBtn) {
            clearBtn.classList.remove('hidden');
            clearBtn.classList.toggle('invisible', empty);
        }

        input.setAttribute('aria-expanded', mode === 'palette' ? 'true' : 'false');

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
        // Fire exactly one filter callback. Earlier this called both
        // dispatchSearchInput() (→ filter.onInput('')) AND
        // dispatchClear() (→ filter.onClear()), so most gears
        // re-filtered twice and any side-effectful onClear would
        // run alongside the empty-query path (Copilot review).
        // Prefer onClear if the gear registered one; otherwise fall
        // back to the standard empty-query dispatch.
        notify(queryListeners, '');
        const filter = window.gearbox && window.gearbox.filter && window.gearbox.filter.current();
        if (filter && typeof filter.onClear === 'function') {
            try { filter.onClear(); }
            catch (e) { console.error('filter.onClear threw', e); }
        } else if (filter && typeof filter.onInput === 'function') {
            try { filter.onInput(''); }
            catch (e) { console.error('filter.onInput threw', e); }
        }
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
     *
     * Below md (<768px) the row is `hidden md:flex` in markup, so it's
     * display:none by default and only the magnifying-glass compact
     * button shows. expandCompact()/collapseCompact() flip a single
     * marker class — `header-search-expanded` — that pairs with a CSS
     * rule below (in base.templ <style>) to override `hidden` and
     * surface the row as an overlay sheet under the header.
     *
     * Earlier versions toggled `flex`/`hidden` directly, but that
     * overrode the responsive `md:flex` and stuck the row visible
     * even when the viewport widened back above md.
     * -------------------------------------------------------------- */
    function expandCompact() {
        if (!row) return;
        row.classList.add('header-search-expanded');
    }
    function collapseCompact() {
        if (!row) return;
        if (!window.matchMedia('(max-width: 767px)').matches) return;
        row.classList.remove('header-search-expanded');
    }

    /* -------------------------------------------------------------- *
     * Wiring
     * -------------------------------------------------------------- */
    function init() {
        input       = document.getElementById('header-search-input');
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
        // Check the .header-search-expanded marker rather than the
        // `hidden` class — the row's templ markup is `hidden md:flex`,
        // so it ALWAYS carries `hidden` and the old check effectively
        // disabled the outside-click collapse (Copilot review). The
        // overlay state is driven by the marker class, not by toggling
        // `hidden` directly.
        document.addEventListener('click', function (e) {
            if (!window.matchMedia('(max-width: 767px)').matches) return;
            if (!row || !row.classList.contains('header-search-expanded')) return;
            if (wrap && wrap.contains(e.target)) return;
            // Don't collapse while the palette panel is the click target.
            const panel = document.getElementById('header-search-panel');
            if (panel && panel.contains(e.target)) return;
            collapseCompact();
        });
        window.addEventListener('resize', function () {
            if (window.matchMedia('(min-width: 768px)').matches) {
                // md+ always shows the row via the `hidden md:flex`
                // responsive rule — no JS toggling needed. We just
                // clear the compact-overlay marker so a fresh narrow→
                // wide transition leaves the row in its normal slot.
                if (row) row.classList.remove('header-search-expanded');
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
