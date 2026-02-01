/**
 * Widget Palette Management
 * Handles loading, filtering, and displaying widgets from enabled plugins
 */

// State
let allWidgets = [];
let filteredWidgets = [];
let currentBoxID = '';

/**
 * Initialize the widget palette
 * @param {string} boxID - The server ID to load widgets for
 */
async function initializeWidgetPalette(boxID) {
    currentBoxID = boxID || '';
    await loadWidgets();
    setupPaletteEventListeners();
}

/**
 * Load widgets from the API
 */
async function loadWidgets() {
    const paletteList = document.getElementById('widget-palette-list');

    try {
        // Build API URL with box_id parameter
        const params = new URLSearchParams();
        if (currentBoxID) {
            params.set('box_id', currentBoxID);
        }

        const response = await fetch(`/api/dashboards/widgets?${params.toString()}`);
        if (!response.ok) {
            throw new Error(`Failed to load widgets: ${response.statusText}`);
        }

        allWidgets = await response.json();
        filteredWidgets = [...allWidgets];

        // Populate plugin filter dropdown
        populatePluginFilter();

        // Render widgets
        renderWidgets();

    } catch (error) {
        console.error('Failed to load widgets:', error);
        paletteList.innerHTML = `
            <div class="palette-empty-state">
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path>
                </svg>
                <p>Failed to load widgets</p>
                <p class="text-xs mt-1">${error.message}</p>
            </div>
        `;
    }
}

/**
 * Populate the plugin filter dropdown
 */
function populatePluginFilter() {
    const pluginFilter = document.getElementById('widget-palette-plugin-filter');

    // Get unique plugins from widgets
    const plugins = new Set();
    allWidgets.forEach(widget => {
        if (widget.plugin_name) {
            plugins.add(widget.plugin_name);
        }
    });

    // Sort plugins alphabetically, but keep "core" first
    const sortedPlugins = Array.from(plugins).sort((a, b) => {
        if (a === 'core') return -1;
        if (b === 'core') return 1;
        return a.localeCompare(b);
    });

    // Add options
    pluginFilter.innerHTML = '<option value="">All Plugins</option>';
    sortedPlugins.forEach(plugin => {
        const option = document.createElement('option');
        option.value = plugin;
        option.textContent = formatPluginName(plugin);
        pluginFilter.appendChild(option);
    });
}

/**
 * Format plugin name for display
 * @param {string} pluginName - The plugin name
 * @returns {string} Formatted plugin name
 */
function formatPluginName(pluginName) {
    if (pluginName === 'core') return 'Core';
    if (pluginName === 'haproxy') return 'HAProxy';
    if (pluginName === 'os_updates') return 'OS Updates';

    // Capitalize first letter of each word
    return pluginName
        .split('_')
        .map(word => word.charAt(0).toUpperCase() + word.slice(1))
        .join(' ');
}

/**
 * Setup event listeners for palette controls
 */
function setupPaletteEventListeners() {
    const searchInput = document.getElementById('widget-palette-search');
    const pluginFilter = document.getElementById('widget-palette-plugin-filter');

    if (searchInput) {
        searchInput.addEventListener('input', handleFilter);
    }

    if (pluginFilter) {
        pluginFilter.addEventListener('change', handleFilter);
    }
}

/**
 * Handle filtering of widgets
 */
function handleFilter() {
    const searchInput = document.getElementById('widget-palette-search');
    const pluginFilter = document.getElementById('widget-palette-plugin-filter');

    const searchTerm = searchInput ? searchInput.value.toLowerCase() : '';
    const selectedPlugin = pluginFilter ? pluginFilter.value : '';

    // Filter widgets
    filteredWidgets = allWidgets.filter(widget => {
        // Plugin filter
        if (selectedPlugin && widget.plugin_name !== selectedPlugin) {
            return false;
        }

        // Search filter
        if (searchTerm) {
            const matchesName = widget.name.toLowerCase().includes(searchTerm);
            const matchesDescription = widget.description.toLowerCase().includes(searchTerm);
            const matchesPlugin = widget.plugin_name.toLowerCase().includes(searchTerm);

            if (!matchesName && !matchesDescription && !matchesPlugin) {
                return false;
            }
        }

        return true;
    });

    renderWidgets();
}

/**
 * Render widgets in the palette
 */
function renderWidgets() {
    const paletteList = document.getElementById('widget-palette-list');

    if (filteredWidgets.length === 0) {
        paletteList.innerHTML = `
            <div class="palette-empty-state">
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"></path>
                </svg>
                <p>No widgets found</p>
                <p class="text-xs mt-1">Try adjusting your filters</p>
            </div>
        `;
        return;
    }

    // Group widgets by category
    const categories = groupWidgetsByCategory(filteredWidgets);

    // Render categories
    paletteList.innerHTML = '';
    Object.keys(categories).forEach(category => {
        const categoryDiv = createCategorySection(category, categories[category]);
        paletteList.appendChild(categoryDiv);
    });

    // Initialize Sortable for palette after rendering
    initializePaletteSortable();
}

/**
 * Initialize Sortable.js for the widget palette
 */
function initializePaletteSortable() {
    // Wait for Sortable to be available
    if (typeof window.Sortable === 'undefined') {
        console.warn('Sortable not loaded yet, skipping palette sortable initialization');
        return;
    }

    // Get all widget lists in the palette
    const widgetLists = document.querySelectorAll('.palette-widget-list');

    widgetLists.forEach(list => {
        // Skip if already initialized
        if (list.sortableInstance) {
            return;
        }

        const sortable = window.Sortable.create(list, {
            group: {
                name: 'widget-palette',
                pull: 'clone',
                put: false
            },
            sort: false,
            animation: 150,
            dragClass: 'palette-widget-dragging',
            ghostClass: 'palette-widget-ghost',
            onEnd: function() {
                // Reset any styling changes after drag
                const cards = list.querySelectorAll('.palette-widget-card');
                cards.forEach(card => {
                    card.style.opacity = '1';
                });
            }
        });

        // Store instance reference
        list.sortableInstance = sortable;
    });
}

/**
 * Group widgets by category
 * @param {Array} widgets - Array of widgets
 * @returns {Object} Widgets grouped by category
 */
function groupWidgetsByCategory(widgets) {
    const groups = {};

    widgets.forEach(widget => {
        const category = widget.category || 'other';
        if (!groups[category]) {
            groups[category] = [];
        }
        groups[category].push(widget);
    });

    return groups;
}

/**
 * Create a category section
 * @param {string} category - Category name
 * @param {Array} widgets - Widgets in this category
 * @returns {HTMLElement} Category section element
 */
function createCategorySection(category, widgets) {
    const section = document.createElement('div');
    section.className = 'palette-category';

    const title = document.createElement('h4');
    title.className = 'palette-category-title';
    title.textContent = formatCategoryName(category);

    const widgetList = document.createElement('div');
    widgetList.className = 'palette-widget-list';

    widgets.forEach(widget => {
        const card = createWidgetCard(widget);
        widgetList.appendChild(card);
    });

    section.appendChild(title);
    section.appendChild(widgetList);

    return section;
}

/**
 * Format category name for display
 * @param {string} category - Category name
 * @returns {string} Formatted category name
 */
function formatCategoryName(category) {
    const categoryNames = {
        'layout': 'Layout',
        'status-metrics': 'Status & Metrics',
        'data-visualization': 'Charts & Visualization',
        'data-display': 'Tables & Lists',
        'haproxy-monitoring': 'HAProxy Monitoring',
        'system-monitoring': 'System Monitoring',
        'security-monitoring': 'Security Monitoring',
        'control': 'Controls',
        'other': 'Other'
    };

    return categoryNames[category] || category;
}

/**
 * Create a widget card element
 * @param {Object} widget - Widget data
 * @returns {HTMLElement} Widget card element
 */
function createWidgetCard(widget) {
    const card = document.createElement('div');
    card.className = 'palette-widget-card';
    card.draggable = true;
    card.dataset.widgetType = widget.type;
    card.dataset.widgetName = widget.name;
    card.dataset.widgetPlugin = widget.plugin_name;

    // Set up drag events
    card.addEventListener('dragstart', handleWidgetDragStart);
    card.addEventListener('dragend', handleWidgetDragEnd);

    card.innerHTML = `
        <div class="flex items-start gap-3">
            <div class="palette-widget-icon">
                ${getWidgetIcon(widget.icon)}
            </div>
            <div class="flex-1 min-w-0">
                <div class="palette-widget-title">${escapeHtml(widget.name)}</div>
                <div class="palette-widget-description">${escapeHtml(widget.description)}</div>
                <div class="palette-plugin-badge plugin-${widget.plugin_name}">
                    ${formatPluginName(widget.plugin_name)}
                </div>
            </div>
        </div>
    `;

    return card;
}

/**
 * Get SVG icon for widget
 * @param {string} iconName - Icon name
 * @returns {string} SVG HTML
 */
function getWidgetIcon(iconName) {
    // Default icon if not specified
    if (!iconName) {
        return `
            <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 5a1 1 0 011-1h14a1 1 0 011 1v2a1 1 0 01-1 1H5a1 1 0 01-1-1V5zM4 13a1 1 0 011-1h6a1 1 0 011 1v6a1 1 0 01-1 1H5a1 1 0 01-1-1v-6zM16 13a1 1 0 011-1h2a1 1 0 011 1v6a1 1 0 01-1 1h-2a1 1 0 01-1-1v-6z"></path>
            </svg>
        `;
    }

    // Icon mapping - you can expand this based on your icon needs
    const icons = {
        'activity': '<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"></path></svg>',
        'grid': '<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 5a1 1 0 011-1h4a1 1 0 011 1v4a1 1 0 01-1 1H5a1 1 0 01-1-1V5zM14 5a1 1 0 011-1h4a1 1 0 011 1v4a1 1 0 01-1 1h-4a1 1 0 01-1-1V5zM4 15a1 1 0 011-1h4a1 1 0 011 1v4a1 1 0 01-1 1H5a1 1 0 01-1-1v-4zM14 15a1 1 0 011-1h4a1 1 0 011 1v4a1 1 0 01-1 1h-4a1 1 0 01-1-1v-4z"></path></svg>',
        'cpu': '<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z"></path></svg>',
        'shield': '<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"></path></svg>',
        'alert-triangle': '<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path></svg>',
        'box': '<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"></path></svg>',
        'trending-up': '<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6"></path></svg>',
        'chevrons-up-down': '<svg fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"></path></svg>'
    };

    return icons[iconName] || icons['box'];
}

/**
 * Escape HTML to prevent XSS
 * @param {string} text - Text to escape
 * @returns {string} Escaped text
 */
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

/**
 * Handle drag start event for widget cards
 * @param {DragEvent} event - Drag event
 */
function handleWidgetDragStart(event) {
    const widgetType = event.currentTarget.dataset.widgetType;
    const widgetName = event.currentTarget.dataset.widgetName;
    const widgetPlugin = event.currentTarget.dataset.widgetPlugin;

    // Store widget data for Sortable.js
    event.currentTarget.dataset.isNewWidget = 'true';

    // Set drag data for browser drag-drop API
    event.dataTransfer.effectAllowed = 'copy';
    event.dataTransfer.setData('text/plain', widgetType);
    event.dataTransfer.setData('application/x-widget-type', widgetType);
    event.dataTransfer.setData('application/x-widget-name', widgetName);
    event.dataTransfer.setData('application/x-widget-plugin', widgetPlugin);

    // Add visual feedback
    event.currentTarget.style.opacity = '0.5';
}

/**
 * Handle drag end event for widget cards
 * @param {DragEvent} event - Drag event
 */
function handleWidgetDragEnd(event) {
    // Restore opacity
    event.currentTarget.style.opacity = '1';
}

/**
 * Toggle widget palette visibility
 */
function toggleWidgetPalette() {
    const panel = document.getElementById('widget-palette-panel');
    if (panel) {
        panel.classList.toggle('hidden');

        // Load widgets if opening for the first time
        if (!panel.classList.contains('hidden') && allWidgets.length === 0) {
            // Get server ID from dashboard context or use default
            const dashboardElement = document.getElementById('dashboard-grid-editor');
            const boxID = dashboardElement ? dashboardElement.dataset.boxId : '';
            initializeWidgetPalette(boxID);
        }
    }
}

// Export functions for use in other scripts
window.initializeWidgetPalette = initializeWidgetPalette;
window.toggleWidgetPalette = toggleWidgetPalette;
