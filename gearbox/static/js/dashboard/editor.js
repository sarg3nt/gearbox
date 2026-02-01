// Dashboard Editor JavaScript
// Handles dashboard editing, widget management, drag-and-drop, and configuration

// Dashboard state - initialize with existing widgets
let dashboardState = {
	widgets: []
};

// Toggle widget palette - make global
window.toggleWidgetPalette = function() {
	const palette = document.getElementById('widget-palette-panel');
	if (palette) {
		palette.classList.toggle('hidden');
	}
};

// Initialize on page load
document.addEventListener('DOMContentLoaded', function() {
	// Load existing widgets from JSON data
	const widgetsData = document.getElementById('dashboard-widgets-data');
	if (widgetsData && widgetsData.dataset.widgets) {
		try {
			dashboardState.widgets = JSON.parse(widgetsData.dataset.widgets);
			console.log('Loaded widgets:', dashboardState.widgets);
		} catch (e) {
			console.error('Failed to parse dashboard widgets:', e);
		}
	}

	// Wait for Sortable module to load before injecting controls
	if (window.sortableReady) {
		console.log('Sortable already loaded, initializing controls');
		injectEditControls();
	} else {
		console.log('Waiting for Sortable to load...');
		window.addEventListener('sortable-loaded', function() {
			console.log('Sortable loaded event received, initializing controls');
			injectEditControls();
		});
	}
});

// Inject edit toolbar into each widget and enable drag-and-drop
function injectEditControls() {
	const widgetContainers = document.querySelectorAll('.widget-container[data-widget-id]');
	console.log('Found widget containers:', widgetContainers.length);
	widgetContainers.forEach((container, index) => {
		// Skip if controls already injected
		if (container.querySelector('.widget-drag-handle-center')) {
			console.log('Controls already exist for widget, skipping');
			return;
		}

		// Get widget ID from data attributes
		const widgetId = container.dataset.widgetId || `widget-${index}`;
		console.log('Injecting controls for widget:', widgetId);

		// Store original height for restoring after drag
		const originalHeight = container.offsetHeight;
		container.dataset.originalHeight = originalHeight;

		// Make container position relative if not already
		if (getComputedStyle(container).position === 'static') {
			container.style.position = 'relative';
		}

		// Create drag handle at top center (two rows of wider dots like TrueNAS)
		const dragHandle = document.createElement('div');
		dragHandle.className = 'widget-drag-handle-center widget-drag-handle';
		dragHandle.title = 'Drag to reposition';

		// SECURITY FIX: Use DOM methods instead of innerHTML to prevent XSS
		const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
		svg.setAttribute('width', '24');
		svg.setAttribute('height', '12');
		svg.setAttribute('viewBox', '0 0 24 12');
		svg.setAttribute('fill', 'currentColor');

		// Create circles for drag handle dots
		const circlePositions = [
			{cx: 4, cy: 3}, {cx: 12, cy: 3}, {cx: 20, cy: 3},
			{cx: 4, cy: 9}, {cx: 12, cy: 9}, {cx: 20, cy: 9}
		];

		circlePositions.forEach(pos => {
			const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
			circle.setAttribute('cx', pos.cx);
			circle.setAttribute('cy', pos.cy);
			circle.setAttribute('r', '2');
			svg.appendChild(circle);
		});

		dragHandle.appendChild(svg);

		// Create edit toolbar at top right
		const toolbar = document.createElement('div');
		toolbar.className = 'widget-edit-toolbar';

		// SECURITY FIX: Build toolbar using DOM methods instead of innerHTML
		// Note: Edit/configure button is hidden until the configuration UI is implemented
		// Widget configuration modal exists but doesn't populate the form yet
		const deleteBtn = document.createElement('button');
		deleteBtn.className = 'widget-edit-btn delete-btn';
		deleteBtn.title = 'Delete';
		deleteBtn.onclick = () => window.deleteWidget(widgetId);

		const deleteSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
		deleteSvg.setAttribute('fill', 'none');
		deleteSvg.setAttribute('stroke', 'currentColor');
		deleteSvg.setAttribute('viewBox', '0 0 24 24');

		const deletePath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
		deletePath.setAttribute('stroke-linecap', 'round');
		deletePath.setAttribute('stroke-linejoin', 'round');
		deletePath.setAttribute('stroke-width', '2');
		deletePath.setAttribute('d', 'M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16');

		deleteSvg.appendChild(deletePath);
		deleteBtn.appendChild(deleteSvg);
		toolbar.appendChild(deleteBtn);

		// Add controls to container
		container.appendChild(dragHandle);
		container.appendChild(toolbar);
	});

	// Initialize Sortable for drag-and-drop on the grid with Swap plugin
	const grid = document.querySelector('[data-dashboard="true"]');
	console.log('Found grid element:', grid);

	if (!grid) {
		console.error('Could not find grid element with data-dashboard="true"');
		return;
	}

	// Check if Sortable is available
	if (typeof window.Sortable === 'undefined') {
		console.error('Sortable not loaded! Swap plugin will not work.');
		return;
	}

	console.log('Creating Sortable instance with manual swap animation and palette group support');

	let lastSwapTarget = null;

	const sortable = window.Sortable.create(grid, {
		animation: 200,
		handle: '.widget-drag-handle',
		ghostClass: 'widget-dragging',
		chosenClass: 'widget-chosen',
		dragClass: 'widget-drag-replica',
		direction: 'vertical',
		easing: 'cubic-bezier(0.4, 0.0, 0.2, 1)',
		group: {
			name: 'dashboard-widgets',
			pull: false,
			put: ['widget-palette']
		},
		onStart: function(evt) {
			const item = evt.item;
			item.dataset.wasDragging = 'true';
			item.dataset.startIndex = evt.oldIndex;
			console.log('Started dragging widget at index', evt.oldIndex);
		},
		onMove: function(evt) {
			// evt.dragged = element being dragged (the faded one)
			// evt.related = element being hovered over
			const draggedElement = evt.dragged;
			const targetElement = evt.related;

			// Check if this is a new widget from the palette
			const isNewWidget = draggedElement.classList.contains('palette-widget-card');

			// If it's a new widget from palette, allow default Sortable behavior
			if (isNewWidget) {
				return true;
			}

			// Only process if we're hovering over a different widget container
			if (!targetElement || !targetElement.classList.contains('widget-container')) {
				return true;
			}

			// Don't swap with ourselves
			if (draggedElement === targetElement) {
				return true;
			}

			// Check if this is a new target (crossed 50% threshold)
			if (targetElement !== lastSwapTarget) {
				console.log('Swapping positions with', targetElement.dataset.widgetId);

				// Manually swap DOM positions for visual feedback
				const parent = draggedElement.parentNode;
				const allItems = Array.from(parent.children);
				const draggedIndex = allItems.indexOf(draggedElement);
				const targetIndex = allItems.indexOf(targetElement);

				// Perform the swap in the DOM
				if (draggedIndex < targetIndex) {
					// Insert dragged element after target
					targetElement.parentNode.insertBefore(draggedElement, targetElement.nextSibling);
				} else {
					// Insert dragged element before target
					targetElement.parentNode.insertBefore(draggedElement, targetElement);
				}

				// Update grid positions for smooth transition
				updateGridPositionsAfterSwap();

				lastSwapTarget = targetElement;
			}

			return false; // Prevent Sortable's default behavior
		},
		onAdd: function(evt) {
			// Handle drop from palette (new widget)
			const item = evt.item;
			console.log('Widget dropped from palette:', item);

			// Get widget data from the palette card
			const widgetType = item.dataset.widgetType;
			const widgetName = item.dataset.widgetName;
			const widgetPlugin = item.dataset.widgetPlugin;

			if (!widgetType) {
				console.error('No widget type found on dropped item');
				item.remove();
				return;
			}

			// Remove the palette card from the grid (it was just a drag placeholder)
			item.remove();

			// Generate widget ID and create widget instance
			const widgetId = `widget-${Date.now()}`;
			const widget = {
				id: widgetId,
				type: widgetType,
				plugin: widgetPlugin || 'core',
				position: {
					row: evt.newIndex + 1,
					column: 1,
					width: 6,
					height: 'auto'
				},
				config: {}
			};

			// Add to dashboard state
			dashboardState.widgets.splice(evt.newIndex, 0, widget);
			console.log('Added new widget to state:', widget);

			// Create and render the actual widget element with loading state
			renderWidgetWithContent(widget, evt.newIndex);

			// Hide empty state if visible
			const emptyState = document.getElementById('empty-state');
			if (emptyState) {
				emptyState.style.display = 'none';
			}

			// Update order of all widgets
			updateWidgetOrder();

			// Auto-save dashboard
			saveDashboardSilently();
		},
		onEnd: function(evt) {
			const item = evt.item;
			delete item.dataset.wasDragging;
			delete item.dataset.startIndex;
			lastSwapTarget = null;

			console.log('Drag ended at index', evt.newIndex);
			updateWidgetOrder();
		}
	});

	// Helper function to update grid row positions after swap
	function updateGridPositionsAfterSwap() {
		const allWidgets = grid.querySelectorAll('.widget-container');
		allWidgets.forEach((widget, index) => {
			widget.style.gridRowStart = index + 1;
		});
	}

	console.log('Sortable instance created:', sortable);
}

// Update widget order after drag-and-drop
function updateWidgetOrder() {
	const grid = document.querySelector('[data-dashboard="true"]');
	if (!grid) {
		console.error('Grid not found in updateWidgetOrder');
		return;
	}

	const widgetElements = grid.querySelectorAll('.widget-container');
	const newOrder = [];

	widgetElements.forEach((el, index) => {
		const widgetId = el.dataset.widgetId;
		const widget = dashboardState.widgets.find(w => w.id === widgetId);
		if (widget) {
			// Update row position based on new order (1-indexed)
			widget.position.row = index + 1;
			newOrder.push(widget);

			// Update the CSS grid-row-start to match new position
			el.style.gridRowStart = widget.position.row;

			// Also update data attributes
			el.dataset.row = widget.position.row;
		}
	});

	dashboardState.widgets = newOrder;
	console.log('Updated widget order:', dashboardState.widgets);
}

// Drag and drop from palette - make global so inline handlers can access
window.handleWidgetDragStart = function(event) {
	event.dataTransfer.setData('widget-type', event.target.dataset.widgetType);
	event.dataTransfer.setData('widget-name', event.target.dataset.widgetName);
	event.dataTransfer.effectAllowed = 'copy';
};

// Handle drop on grid - make global so inline handler can access
window.handleGridDrop = function(event) {
	event.preventDefault();
	const widgetType = event.dataTransfer.getData('widget-type');
	if (widgetType) {
		addWidget(widgetType);
	}
};

// Add widget to dashboard - make global
window.addWidget = function(widgetType) {
	const widgetId = `widget-${Date.now()}`;
	const widget = {
		id: widgetId,
		type: widgetType,
		position: { row: 1, column: 1, width: 6, height: 'auto' },
		config: {}
	};
	dashboardState.widgets.push(widget);
	renderWidget(widget);

	// Hide empty state if visible
	const emptyState = document.getElementById('empty-state');
	if (emptyState) {
		emptyState.style.display = 'none';
	}
};

// Render widget with actual content from server
function renderWidgetWithContent(widget, insertIndex) {
	const grid = document.querySelector('[data-dashboard="true"]');
	if (!grid) {
		console.error('Grid not found in renderWidgetWithContent');
		return;
	}

	// Create widget container with loading state
	const widgetEl = document.createElement('div');
	widgetEl.className = 'widget-container';
	widgetEl.setAttribute('data-widget-id', widget.id);
	widgetEl.setAttribute('data-widget-type', widget.type);
	widgetEl.setAttribute('data-row', widget.position.row);
	widgetEl.setAttribute('data-column', widget.position.column);
	widgetEl.setAttribute('data-width', widget.position.width);
	widgetEl.style.gridColumn = `span ${widget.position.width}`;
	widgetEl.style.gridRowStart = widget.position.row;

	// Add loading state
	widgetEl.innerHTML = `
		<div class="widget-card bg-white dark:bg-slate-900 border border-gray-200 dark:border-slate-700 rounded-lg p-4">
			<div class="flex items-center justify-center py-8">
				<div class="text-center">
					<svg class="w-8 h-8 mx-auto mb-2 animate-spin text-blue-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"></path>
					</svg>
					<p class="text-sm text-gray-600 dark:text-gray-400">Loading widget...</p>
				</div>
			</div>
		</div>
	`;

	// Insert at the correct position
	const allWidgets = grid.querySelectorAll('.widget-container');
	if (insertIndex >= allWidgets.length) {
		// Insert before empty state if it exists, otherwise append
		const emptyState = document.getElementById('empty-state');
		if (emptyState && grid.contains(emptyState)) {
			grid.insertBefore(widgetEl, emptyState);
		} else {
			grid.appendChild(widgetEl);
		}
	} else {
		grid.insertBefore(widgetEl, allWidgets[insertIndex]);
	}

	// Inject edit controls after adding to DOM
	injectEditControlsForWidget(widgetEl);

	// TODO: Fetch widget content from server via HTMX or API
	// For now, we'll show a placeholder that says the widget was added
	setTimeout(() => {
		widgetEl.innerHTML = `
			<div class="widget-card bg-white dark:bg-slate-900 border border-gray-200 dark:border-slate-700 rounded-lg p-4">
				<div class="widget-header flex items-center justify-between mb-3 pb-3 border-b border-gray-200 dark:border-slate-700">
					<h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">${escapeHtml(widget.type)}</h3>
					<span class="text-xs text-gray-500 dark:text-gray-400">${escapeHtml(widget.plugin)}</span>
				</div>
				<div class="text-sm text-gray-600 dark:text-gray-400">
					<p>Widget added successfully!</p>
					<p class="text-xs mt-2">Save the dashboard to persist this widget.</p>
				</div>
			</div>
		`;
		// Re-inject edit controls after replacing content
		injectEditControlsForWidget(widgetEl);
	}, 500);

	console.log('Widget rendered with content:', widget);
}

// Inject edit controls for a specific widget
function injectEditControlsForWidget(container) {
	// Skip if controls already injected
	if (container.querySelector('.widget-drag-handle-center')) {
		return;
	}

	const widgetId = container.dataset.widgetId;

	// Store original height for restoring after drag
	const originalHeight = container.offsetHeight;
	container.dataset.originalHeight = originalHeight;

	// Make container position relative if not already
	if (getComputedStyle(container).position === 'static') {
		container.style.position = 'relative';
	}

	// Create drag handle at top center
	const dragHandle = document.createElement('div');
	dragHandle.className = 'widget-drag-handle-center widget-drag-handle';
	dragHandle.title = 'Drag to reposition';

	const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
	svg.setAttribute('width', '24');
	svg.setAttribute('height', '12');
	svg.setAttribute('viewBox', '0 0 24 12');
	svg.setAttribute('fill', 'currentColor');

	const circlePositions = [
		{cx: 4, cy: 3}, {cx: 12, cy: 3}, {cx: 20, cy: 3},
		{cx: 4, cy: 9}, {cx: 12, cy: 9}, {cx: 20, cy: 9}
	];

	circlePositions.forEach(pos => {
		const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
		circle.setAttribute('cx', pos.cx);
		circle.setAttribute('cy', pos.cy);
		circle.setAttribute('r', '2');
		svg.appendChild(circle);
	});

	dragHandle.appendChild(svg);

	// Create edit toolbar at top right
	const toolbar = document.createElement('div');
	toolbar.className = 'widget-edit-toolbar';

	const deleteBtn = document.createElement('button');
	deleteBtn.className = 'widget-edit-btn delete-btn';
	deleteBtn.title = 'Delete';
	deleteBtn.onclick = () => window.deleteWidget(widgetId);

	const deleteSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
	deleteSvg.setAttribute('fill', 'none');
	deleteSvg.setAttribute('stroke', 'currentColor');
	deleteSvg.setAttribute('viewBox', '0 0 24 24');

	const deletePath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
	deletePath.setAttribute('stroke-linecap', 'round');
	deletePath.setAttribute('stroke-linejoin', 'round');
	deletePath.setAttribute('stroke-width', '2');
	deletePath.setAttribute('d', 'M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16');

	deleteSvg.appendChild(deletePath);
	deleteBtn.appendChild(deleteSvg);
	toolbar.appendChild(deleteBtn);

	// Add controls to container
	container.appendChild(dragHandle);
	container.appendChild(toolbar);
}

// Helper function to escape HTML
function escapeHtml(text) {
	const div = document.createElement('div');
	div.textContent = text;
	return div.innerHTML;
}

// Render widget in grid
function renderWidget(widget) {
	const grid = document.getElementById('dashboard-grid');
	if (!grid) return;

	// Create widget placeholder element
	const widgetEl = document.createElement('div');
	widgetEl.className = 'widget-placeholder bg-white dark:bg-slate-900 border-2 border-gray-300 dark:border-slate-600 rounded-lg p-4 relative';
	widgetEl.setAttribute('data-widget-id', widget.id);
	widgetEl.setAttribute('data-widget-type', widget.type);
	widgetEl.style.gridColumn = `span ${widget.position.width}`;

	// SECURITY FIX: Build widget DOM structure safely without innerHTML
	// Widget Header
	const header = document.createElement('div');
	header.className = 'flex items-center justify-between mb-2 pb-2 border-b border-gray-200 dark:border-slate-700';

	const widgetTypeSpan = document.createElement('span');
	widgetTypeSpan.className = 'text-sm font-medium text-gray-700 dark:text-gray-300';
	widgetTypeSpan.textContent = widget.type; // textContent auto-escapes

	const buttonContainer = document.createElement('div');
	buttonContainer.className = 'flex gap-1';

	// Configure button
	const configBtn = document.createElement('button');
	configBtn.className = 'p-1 hover:bg-gray-100 dark:hover:bg-slate-800 rounded';
	configBtn.title = 'Configure';
	configBtn.onclick = () => window.configureWidget(widget.id);

	const configSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
	configSvg.setAttribute('class', 'w-4 h-4');
	configSvg.setAttribute('fill', 'none');
	configSvg.setAttribute('stroke', 'currentColor');
	configSvg.setAttribute('viewBox', '0 0 24 24');

	const configPath1 = document.createElementNS('http://www.w3.org/2000/svg', 'path');
	configPath1.setAttribute('stroke-linecap', 'round');
	configPath1.setAttribute('stroke-linejoin', 'round');
	configPath1.setAttribute('stroke-width', '2');
	configPath1.setAttribute('d', 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z');

	const configPath2 = document.createElementNS('http://www.w3.org/2000/svg', 'path');
	configPath2.setAttribute('stroke-linecap', 'round');
	configPath2.setAttribute('stroke-linejoin', 'round');
	configPath2.setAttribute('stroke-width', '2');
	configPath2.setAttribute('d', 'M15 12a3 3 0 11-6 0 3 3 0 016 0z');

	configSvg.appendChild(configPath1);
	configSvg.appendChild(configPath2);
	configBtn.appendChild(configSvg);

	// Delete button
	const deleteBtn = document.createElement('button');
	deleteBtn.className = 'p-1 hover:bg-red-100 dark:hover:bg-red-900/20 text-red-600 rounded';
	deleteBtn.title = 'Delete';
	deleteBtn.onclick = () => window.deleteWidget(widget.id);

	const deleteSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
	deleteSvg.setAttribute('class', 'w-4 h-4');
	deleteSvg.setAttribute('fill', 'none');
	deleteSvg.setAttribute('stroke', 'currentColor');
	deleteSvg.setAttribute('viewBox', '0 0 24 24');

	const deletePath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
	deletePath.setAttribute('stroke-linecap', 'round');
	deletePath.setAttribute('stroke-linejoin', 'round');
	deletePath.setAttribute('stroke-width', '2');
	deletePath.setAttribute('d', 'M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16');

	deleteSvg.appendChild(deletePath);
	deleteBtn.appendChild(deleteSvg);

	buttonContainer.appendChild(configBtn);
	buttonContainer.appendChild(deleteBtn);
	header.appendChild(widgetTypeSpan);
	header.appendChild(buttonContainer);

	// Widget Preview
	const preview = document.createElement('div');
	preview.className = 'text-xs text-gray-500 dark:text-gray-400';
	preview.textContent = `Widget ID: ${widget.id}`; // textContent auto-escapes

	// Resize Handle
	const resizeHandle = document.createElement('div');
	resizeHandle.className = 'absolute bottom-2 right-2 w-4 h-4 cursor-nwse-resize';

	const resizeSvg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
	resizeSvg.setAttribute('class', 'w-full h-full text-gray-400');
	resizeSvg.setAttribute('fill', 'currentColor');
	resizeSvg.setAttribute('viewBox', '0 0 24 24');

	const resizePath = document.createElementNS('http://www.w3.org/2000/svg', 'path');
	resizePath.setAttribute('d', 'M22 22H20V20H22V22ZM22 18H20V16H22V18ZM18 22H16V20H18V22ZM18 18H16V16H18V18ZM14 22H12V20H14V22Z');

	resizeSvg.appendChild(resizePath);
	resizeHandle.appendChild(resizeSvg);

	// Assemble widget
	widgetEl.appendChild(header);
	widgetEl.appendChild(preview);
	widgetEl.appendChild(resizeHandle);

	// Append to grid before empty state (if it exists)
	const emptyState = document.getElementById('empty-state');
	if (emptyState) {
		grid.insertBefore(widgetEl, emptyState);
	} else {
		grid.appendChild(widgetEl);
	}

	console.log('Widget rendered:', widget);
}

// Configure widget - make global for inline onclick handlers
window.configureWidget = function(widgetId) {
	document.getElementById('config-modal').classList.remove('hidden');
	// Load widget config form
};

// Delete widget - make global for inline onclick handlers
window.deleteWidget = function(widgetId) {
	const widgetEl = document.querySelector(`[data-widget-id="${widgetId}"]`);
	if (!widgetEl) return;

	// Remove from state
	dashboardState.widgets = dashboardState.widgets.filter(w => w.id !== widgetId);

	// Remove from DOM
	widgetEl.remove();

	// Show empty state if no widgets left
	const remainingWidgets = document.querySelectorAll('.widget-placeholder').length;
	const emptyState = document.getElementById('empty-state');
	if (remainingWidgets === 0 && emptyState) {
		emptyState.style.display = 'flex';
	}
};

// Close config modal - make global
window.closeConfigModal = function() {
	document.getElementById('config-modal').classList.add('hidden');
};

// Save widget configuration - make global
window.saveWidgetConfig = function() {
	// Save config
	closeConfigModal();
};

// Save dashboard silently (auto-save after drag-drop)
function saveDashboardSilently() {
	const dashboardGridEditor = document.getElementById('dashboard-grid-editor');
	if (!dashboardGridEditor) {
		console.error('Could not find dashboard-grid-editor element');
		return;
	}
	const dashboardSlug = dashboardGridEditor.dataset.dashboardId;

	console.log('Auto-saving dashboard with widgets:', dashboardState.widgets);

	const payload = { widgets: dashboardState.widgets };

	fetch(`/api/dashboards/${dashboardSlug}`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(payload)
	})
	.then(response => {
		if (response.ok) {
			console.log('Dashboard auto-saved successfully');
			// Show a brief success indicator
			showSuccessToast('Widget added');
		} else {
			return response.text().then(text => {
				throw new Error(text || 'Failed to auto-save dashboard');
			});
		}
	})
	.catch(error => {
		console.error('Auto-save failed:', error);
		showError('Auto-save Failed', error.message || 'An error occurred while auto-saving the dashboard');
	});
}

// Save dashboard - make global
window.saveDashboard = function() {
	const dashboardGridEditor = document.getElementById('dashboard-grid-editor');
	if (!dashboardGridEditor) {
		console.error('Could not find dashboard-grid-editor element');
		showError('Save Failed', 'Could not find dashboard element');
		return;
	}
	const dashboardSlug = dashboardGridEditor.dataset.dashboardId;

	console.log('Saving dashboard with widgets:', dashboardState.widgets);

	// Show loading state
	const saveBtn = document.querySelector('button[onclick="saveDashboard()"]');
	const originalText = saveBtn.textContent;
	saveBtn.disabled = true;
	saveBtn.textContent = 'Saving...';

	const payload = { widgets: dashboardState.widgets };
	console.log('Sending payload:', JSON.stringify(payload, null, 2));

	fetch(`/api/dashboards/${dashboardSlug}`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(payload)
	})
	.then(response => {
		if (response.ok) {
			window.location.href = `/dashboards/${dashboardSlug}`;
		} else {
			return response.text().then(text => {
				throw new Error(text || 'Failed to save dashboard');
			});
		}
	})
	.catch(error => {
		saveBtn.disabled = false;
		saveBtn.textContent = originalText;
		showError('Save Failed', error.message || 'An error occurred while saving the dashboard');
	});
};

// Show success toast message
function showSuccessToast(message) {
	const toast = document.createElement('div');
	toast.className = 'fixed top-4 right-4 bg-green-600 text-white px-6 py-3 rounded-lg shadow-lg z-50 flex items-center gap-2';

	const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
	svg.setAttribute('class', 'w-5 h-5 flex-shrink-0');
	svg.setAttribute('fill', 'none');
	svg.setAttribute('stroke', 'currentColor');
	svg.setAttribute('viewBox', '0 0 24 24');

	const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
	path.setAttribute('stroke-linecap', 'round');
	path.setAttribute('stroke-linejoin', 'round');
	path.setAttribute('stroke-width', '2');
	path.setAttribute('d', 'M5 13l4 4L19 7');

	svg.appendChild(path);

	const messageEl = document.createElement('span');
	messageEl.textContent = message;

	toast.appendChild(svg);
	toast.appendChild(messageEl);

	document.body.appendChild(toast);
	setTimeout(() => toast.remove(), 2000);
}

// Show error message
function showError(title, message) {
	// SECURITY FIX: Create toast notification using DOM methods instead of innerHTML
	const toast = document.createElement('div');
	toast.className = 'fixed top-4 right-4 bg-red-600 text-white px-6 py-4 rounded-lg shadow-lg z-50 max-w-md';

	const container = document.createElement('div');
	container.className = 'flex items-start gap-3';

	// Icon SVG
	const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
	svg.setAttribute('class', 'w-6 h-6 flex-shrink-0');
	svg.setAttribute('fill', 'none');
	svg.setAttribute('stroke', 'currentColor');
	svg.setAttribute('viewBox', '0 0 24 24');

	const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
	path.setAttribute('stroke-linecap', 'round');
	path.setAttribute('stroke-linejoin', 'round');
	path.setAttribute('stroke-width', '2');
	path.setAttribute('d', 'M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z');

	svg.appendChild(path);

	// Text content
	const textContainer = document.createElement('div');

	const titleEl = document.createElement('h4');
	titleEl.className = 'font-semibold';
	titleEl.textContent = title; // textContent auto-escapes

	const messageEl = document.createElement('p');
	messageEl.className = 'text-sm mt-1';
	messageEl.textContent = message; // textContent auto-escapes

	textContainer.appendChild(titleEl);
	textContainer.appendChild(messageEl);

	container.appendChild(svg);
	container.appendChild(textContainer);
	toast.appendChild(container);

	document.body.appendChild(toast);
	setTimeout(() => toast.remove(), 5000);
}

// Filter widgets in palette - make global for inline oninput handler
window.filterWidgets = function(query) {
	const cards = document.querySelectorAll('.widget-card');
	cards.forEach(card => {
		const name = card.dataset.widgetName.toLowerCase();
		card.style.display = name.includes(query.toLowerCase()) ? '' : 'none';
	});
};

// Update positions after drag - reorder widgets array to match DOM order
function updateWidgetPositions() {
	const widgetElements = document.querySelectorAll('.widget-placeholder');
	const newOrder = [];

	// Build new array in DOM order
	widgetElements.forEach((el) => {
		const widgetId = el.dataset.widgetId;
		const widget = dashboardState.widgets.find(w => w.id === widgetId);
		if (widget) {
			newOrder.push(widget);
		}
	});

	// Replace widgets array with new order
	dashboardState.widgets = newOrder;

	console.log('Reordered widgets to match DOM');
}
