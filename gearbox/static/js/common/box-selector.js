/**
 * Box Selector — page-level helpers for switching the active box.
 *
 * The persistent header chip + command palette in
 * static/js/common/box-switcher.js is the user-facing switcher today.
 * The functions below remain for backwards compatibility with any page
 * that still has a `<select id="server-selector">` dropdown wired to
 * `onchange="switchBox(this.value)"`.
 *
 * Active-box state is now persisted server-side in the `gearbox_active_box`
 * cookie. The first request that carries `?box_id=<id>` writes the cookie;
 * subsequent navigations don't need the query string. We still emit
 * `?box_id=` on switch so the middleware updates the cookie, but the URL is
 * cleaned up on landing — see InjectIntegrationStatus in handler.go.
 */

/**
 * Navigate the current tab. Sends `?box_id=<id>` (or `?box_id=` to clear)
 * so the server-side middleware can update the persistent cookie. The
 * destination page itself doesn't need the query string going forward —
 * the cookie carries the selection.
 *
 * @param {string|null} boxID — the box ID to activate, or empty/null to clear.
 * @param {string|null} [path] — optional target path; defaults to the current page.
 */
function switchBox(boxID, path) {
    const url = new URL(window.location.href);
    if (path) {
        url.pathname = path;
    }
    // Clear other query params on box switch — they are page-state and
    // typically don't make sense in a different box's context.
    url.search = '';
    if (boxID) {
        url.searchParams.set('box_id', boxID);
    } else {
        url.searchParams.set('box_id', '');
    }
    window.location.assign(url.toString());
}

// Make function available globally for onclick handlers (legacy dropdowns).
window.switchBox = switchBox;
