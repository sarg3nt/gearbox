/**
 * Box Selector Management
 * Handles server switching functionality used across multiple pages
 */

/**
 * Switch to a different server (reloads page with new server parameter)
 * @param {string} boxID - The server ID to switch to
 */
function switchBox(boxID) {
    if (!boxID) return;

    // Get current URL
    const url = new URL(window.location.href);

    // Update or add box_id parameter
    url.searchParams.set('box_id', boxID);

    // Reload page with new server
    window.location.href = url.toString();
}

// Make function available globally for onclick handlers
window.switchBox = switchBox;
