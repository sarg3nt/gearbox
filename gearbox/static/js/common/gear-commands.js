/**
 * gear-commands — per-page command palette + filter registration (issue #92).
 *
 * Two related APIs hang off `window.gearbox`:
 *
 *   window.gearbox.filter.register({
 *     placeholder: 'Filter logs…',
 *     onInput: (query) => { ... },      // every keystroke in search mode
 *     onSubmit: (query) => { ... },     // optional: Enter pressed
 *     onClear:  () => { ... },          // optional: clear button pressed
 *   })
 *
 *   window.gearbox.commands.register({
 *     id:       'logs.copy',            // stable id; replaces an earlier
 *                                       //   registration with the same id
 *     label:    'Copy logs to clipboard',
 *     subtitle: 'Copy currently-visible log lines',  // optional
 *     group:    'Logs',                 // section label in the palette
 *     kbd:      'c',                    // optional hint
 *     run:      () => { ... },          // invoked when the user picks it
 *   })
 *
 * Both registrations are scoped to the current page: navigating away
 * (turbo-style or full reload) drops them. Each gear's page script
 * should call register() during DOMContentLoaded; clearForPage() is
 * also exposed for explicit cleanup if a gear renders nested SPAs.
 *
 * Listeners can subscribe to changes via:
 *   window.gearbox.commands.onChange(callback)
 *   window.gearbox.filter.onChange(callback)
 * The header-search + command-palette controllers use this to re-render
 * when a gear registers commands after the palette has already opened.
 */
(function () {
    'use strict';

    const ns = window.gearbox = window.gearbox || {};

    /* -------------------------------------------------------------- *
     * Commands registry
     * -------------------------------------------------------------- */
    const commandList = [];       // ordered insertion; later wins on dupe id
    const commandListeners = [];  // [(cmds) => void]

    function notifyCommands() {
        for (let i = 0; i < commandListeners.length; i++) {
            try { commandListeners[i](commandList.slice()); }
            catch (e) { console.error('gear-commands: listener threw', e); }
        }
    }

    function registerCommand(cmd) {
        if (!cmd || !cmd.id || !cmd.label || typeof cmd.run !== 'function') {
            console.warn('gear-commands.register: invalid command', cmd);
            return function () {};
        }
        // Replace any existing entry with the same id so a page can
        // re-register on hot-reload without piling up dupes.
        const idx = commandList.findIndex(function (c) { return c.id === cmd.id; });
        const entry = {
            id:       cmd.id,
            label:    cmd.label,
            subtitle: cmd.subtitle || '',
            group:    cmd.group || 'Page',
            kbd:      cmd.kbd || '',
            run:      cmd.run,
        };
        if (idx >= 0) commandList[idx] = entry;
        else commandList.push(entry);
        notifyCommands();
        return function unregister() {
            const i = commandList.findIndex(function (c) { return c.id === entry.id; });
            if (i >= 0) {
                commandList.splice(i, 1);
                notifyCommands();
            }
        };
    }

    function clearCommands() {
        if (commandList.length === 0) return;
        commandList.length = 0;
        notifyCommands();
    }

    function getCommands() {
        return commandList.slice();
    }

    function runCommandById(id) {
        const cmd = commandList.find(function (c) { return c.id === id; });
        if (!cmd) return false;
        try { cmd.run(); } catch (e) { console.error('command run failed', e); }
        return true;
    }

    function onCommandsChange(fn) {
        if (typeof fn !== 'function') return function () {};
        commandListeners.push(fn);
        return function () {
            const i = commandListeners.indexOf(fn);
            if (i >= 0) commandListeners.splice(i, 1);
        };
    }

    ns.commands = {
        register: registerCommand,
        clear:    clearCommands,
        list:     getCommands,
        run:      runCommandById,
        onChange: onCommandsChange,
    };

    /* -------------------------------------------------------------- *
     * Filter registry — only one filter is active at a time.
     * -------------------------------------------------------------- */
    let filterEntry = null;
    const filterListeners = [];

    function notifyFilter() {
        for (let i = 0; i < filterListeners.length; i++) {
            try { filterListeners[i](filterEntry); }
            catch (e) { console.error('gear-commands.filter: listener threw', e); }
        }
    }

    function registerFilter(opts) {
        if (!opts || typeof opts.onInput !== 'function') {
            console.warn('gear-commands.filter.register: missing onInput', opts);
            return function () {};
        }
        filterEntry = {
            placeholder: opts.placeholder || 'Filter…',
            onInput:     opts.onInput,
            onSubmit:    typeof opts.onSubmit === 'function' ? opts.onSubmit : null,
            onClear:     typeof opts.onClear  === 'function' ? opts.onClear  : null,
        };
        notifyFilter();
        return function clearThisFilter() {
            if (filterEntry && filterEntry.onInput === opts.onInput) {
                filterEntry = null;
                notifyFilter();
            }
        };
    }

    function clearFilter() {
        if (!filterEntry) return;
        filterEntry = null;
        notifyFilter();
    }

    function getFilter() { return filterEntry; }

    function onFilterChange(fn) {
        if (typeof fn !== 'function') return function () {};
        filterListeners.push(fn);
        return function () {
            const i = filterListeners.indexOf(fn);
            if (i >= 0) filterListeners.splice(i, 1);
        };
    }

    ns.filter = {
        register: registerFilter,
        clear:    clearFilter,
        current:  getFilter,
        onChange: onFilterChange,
    };
})();
