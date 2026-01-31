/**
 * Server Selector Management
 * Handles server switching functionality used across multiple pages
 */

/**
 * Switch to a different server (reloads page with new server parameter)
 * @param {string} serverID - The server ID to switch to
 */
function switchServer(serverID) {
    if (!serverID) return;

    // Get current URL
    const url = new URL(window.location.href);

    // Update or add server_id parameter
    url.searchParams.set('server_id', serverID);

    // Reload page with new server
    window.location.href = url.toString();
}

// Make function available globally for onclick handlers
window.switchServer = switchServer;
