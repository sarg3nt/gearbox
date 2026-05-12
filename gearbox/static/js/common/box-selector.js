/**
 * Box Selector — page-level helpers for switching the active box.
 *
 * The persistent header chip + command palette in
 * static/js/common/box-switcher.js is the user-facing switcher today.
 * The functions below remain for backwards compatibility with any page
 * that still has a `<select id="server-selector">` dropdown wired to
 * `onchange="switchBox(this.value)"`.
 *
 * Both paths converge on the same URL convention: a `?box_id=<id>` query
 * parameter on the current page. The server-side InjectIntegrationStatus
 * middleware reads that parameter, hydrates the sidebar with that box's
 * enabled gears, and renders the active-box chip.
 */

/**
 * Navigate the current tab to the same path with `?box_id=<id>` set. Drops
 * any existing `box_id` even if the new id is empty (which clears box
 * context — useful for going from a box-specific page to a box-agnostic
 * one). The page reloads; there is no SPA layer in play.
 *
 * @param {string|null} boxID — the box ID to activate, or empty/null to clear.
 * @param {string|null} [path] — optional target path; defaults to the current page.
 */
function switchBox(boxID, path) {
    const url = new URL(window.location.href);
    if (path) {
        url.pathname = path;
    }
    if (boxID) {
        url.searchParams.set('box_id', boxID);
    } else {
        url.searchParams.delete('box_id');
    }
    window.location.assign(url.toString());
}

// Make function available globally for onclick handlers (legacy dropdowns).
window.switchBox = switchBox;
