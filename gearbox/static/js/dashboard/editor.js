// Dashboard Editor JavaScript
// Handles dashboard editing, widget management, drag-and-drop, and configuration

// Dashboard state - initialize with existing widgets
let dashboardState = {
	widgets: []
};

// Add widget from palette click — creates widget, saves dashboard, reloads page
// to get live server-rendered widget content
window.addWidgetFromPalette = function(widgetType, pluginName) {
	const dashboardGridEditor = document.getElementById('dashboard-grid-editor');
	if (!dashboardGridEditor) {
		console.error('Could not find dashboard-grid-editor element');
		return;
	}
	const dashboardSlug = dashboardGridEditor.dataset.dashboardId;
	const boxId = dashboardGridEditor.dataset.boxId || '';

	// Generate widget ID and create widget instance
	const widgetId = `widget-${Date.now()}`;
	const config = {};
	if (boxId) {
		config.box_id = boxId;
	}

	const widget = {
		id: widgetId,
		type: widgetType,
		position: {
			row: dashboardState.widgets.length + 1,
			column: 1,
			width: 6,
			height: 'auto'
		},
		config: config
	};

	// Add to state
	dashboardState.widgets.push(widget);

	// Save to server then reload so widget renders as live content
	const payload = { widgets: dashboardState.widgets };
	fetch(`/api/dashboards/${dashboardSlug}`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(payload)
	})
	.then(response => {
		if (response.ok) {
			// Reload page to show live rendered widget
			window.location.reload();
		} else {
			return response.text().then(text => {
				throw new Error(text || 'Failed to save');
			});
		}
	})
	.catch(error => {
		console.error('Failed to add widget:', error);
		// Remove from state on failure
		dashboardState.widgets = dashboardState.widgets.filter(w => w.id !== widgetId);
		showError('Add Widget Failed', error.message || 'An error occurred');
	});
};

// Initialize on page load
document.addEventListener('DOMContentLoaded', function() {
	// Load existing widgets from JSON data
	const widgetsData = document.getElementById('dashboard-widgets-data');
	if (widgetsData && widgetsData.dataset.widgets) {
		try {
			dashboardState.widgets = JSON.parse(widgetsData.dataset.widgets);
		} catch (e) {
			console.error('Failed to parse dashboard widgets:', e);
		}
	}

	// Initialize controls — Sortable is loaded globally via base.templ
	if (typeof window.Sortable !== 'undefined') {
		injectEditControls();
	} else {
		console.error('Sortable not loaded! Dashboard editor drag-and-drop will not work.');
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
	// Try the inner dashboard grid first, fall back to the live content container
	let grid = document.querySelector('[data-dashboard="true"]');
	if (!grid) {
		grid = document.getElementById('dashboard-live-content');
	}
	if (!grid) {
		console.error('Could not find grid element');
		return;
	}

	// Check if Sortable is available
	if (typeof window.Sortable === 'undefined') {
		console.error('Sortable not loaded! Swap plugin will not work.');
		return;
	}

	let lastSwapTarget = null;

	const sortable = window.Sortable.create(grid, {
		animation: 200,
		handle: '.widget-drag-handle',
		ghostClass: 'widget-dragging',
		chosenClass: 'widget-chosen',
		dragClass: 'widget-drag-replica',
		direction: 'vertical',
		easing: 'cubic-bezier(0.4, 0.0, 0.2, 1)',
		filter: '#empty-state',
		draggable: '.widget-container',
		onStart: function(evt) {
			const item = evt.item;
			item.dataset.wasDragging = 'true';
			item.dataset.startIndex = evt.oldIndex;
		},
		onMove: function(evt) {
			const draggedElement = evt.dragged;
			const targetElement = evt.related;

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
				// Manually swap DOM positions for visual feedback
				const parent = draggedElement.parentNode;
				const allItems = Array.from(parent.children);
				const draggedIndex = allItems.indexOf(draggedElement);
				const targetIndex = allItems.indexOf(targetElement);

				if (draggedIndex < targetIndex) {
					targetElement.parentNode.insertBefore(draggedElement, targetElement.nextSibling);
				} else {
					targetElement.parentNode.insertBefore(draggedElement, targetElement);
				}

				updateGridPositionsAfterSwap();
				lastSwapTarget = targetElement;
			}

			return false;
		},
		onEnd: function(evt) {
			const item = evt.item;
			delete item.dataset.wasDragging;
			delete item.dataset.startIndex;
			lastSwapTarget = null;
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

}

// Update widget order after drag-and-drop
function updateWidgetOrder() {
	let grid = document.querySelector('[data-dashboard="true"]');
	if (!grid) grid = document.getElementById('dashboard-live-content');
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

	// Auto-save after reorder
	saveDashboardSilently();
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
	const remainingWidgets = document.querySelectorAll('.widget-container').length;
	const emptyState = document.getElementById('empty-state');
	if (remainingWidgets === 0 && emptyState) {
		emptyState.style.display = 'flex';
	}

	// Auto-save after delete
	saveDashboardSilently();
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

	const payload = { widgets: dashboardState.widgets };

	fetch(`/api/dashboards/${dashboardSlug}`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(payload)
	})
	.then(response => {
		if (response.ok) {
			showSuccessToast('Dashboard saved');
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

	// Show loading state
	const saveBtn = document.querySelector('button[onclick="saveDashboard()"]');
	const originalText = saveBtn.textContent;
	saveBtn.disabled = true;
	saveBtn.textContent = 'Saving...';

	const payload = { widgets: dashboardState.widgets };

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

