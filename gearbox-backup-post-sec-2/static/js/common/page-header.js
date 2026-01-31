/**
 * Page Header Management
 * Handles dynamic page header content injection
 *
 * Many pages use a pattern where header content is defined in a hidden div
 * and then moved to the main page header on load.
 */

/**
 * Setup page header by moving content from source to target
 * This is called automatically on DOMContentLoaded
 */
function setupPageHeader() {
    const source = document.getElementById('page-header-source');
    const target = document.getElementById('header-page-content');

    if (source && target) {
        // Move all children from source to target
        while (source.firstChild) {
            target.appendChild(source.firstChild);
        }
        // Remove the now-empty source container
        source.remove();
    }
}

// Auto-initialize on DOM ready
document.addEventListener('DOMContentLoaded', setupPageHeader);
