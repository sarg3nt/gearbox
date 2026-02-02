	function switchBox(boxID) {
// Update global BoxSelector for tab isolation before navigating
if (window.BoxSelector) {
	window.BoxSelector.setSelectedServer(boxID);
}
window.location.href = '/settings/plugins?server=' + encodeURIComponent(boxID);
	}

	// Move page header content to main header
	function setupPageHeader() {
const source = document.getElementById('page-header-source');
const target = document.getElementById('header-page-content');
if (source && target) {
	while (source.firstChild) {
		target.appendChild(source.firstChild);
	}
	source.remove();
}
	}

	// Drag and drop reordering with smooth animations
	let draggedItem = null;
	let draggedIndex = -1;
	let currentDropIndex = -1;

	function getItemIndex(item) {
const list = document.getElementById('plugins-list');
const items = Array.from(list.querySelectorAll('.plugin-card'));
return items.indexOf(item);
	}

	function animateItems(targetIndex) {
if (targetIndex === currentDropIndex) return;
currentDropIndex = targetIndex;

const list = document.getElementById('plugins-list');
const items = Array.from(list.querySelectorAll('.plugin-card'));
const draggedHeight = draggedItem.offsetHeight + 16; // Include margin (space-y-4 = 16px)

items.forEach((item, index) => {
	if (item === draggedItem) {
		return;
	}

	let offset = 0;
	if (draggedIndex < targetIndex) {
		// Dragging down: items between old and new position move up
		if (index > draggedIndex && index <= targetIndex) {
			offset = -draggedHeight;
		}
	} else if (draggedIndex > targetIndex) {
		// Dragging up: items between new and old position move down
		if (index >= targetIndex && index < draggedIndex) {
			offset = draggedHeight;
		}
	}

	item.style.transform = offset ? `translateY(${offset}px)` : '';
});
	}

	function resetItemTransforms(instant) {
const list = document.getElementById('plugins-list');
const items = list.querySelectorAll('.plugin-card');
items.forEach(item => {
	if (instant) {
		item.style.transition = 'none';
	}
	item.style.transform = '';
	if (instant) {
		// Force reflow to apply the instant change
		item.offsetHeight;
		item.style.transition = '';
	}
});
	}

	// NOTE: Drag-and-drop functionality has been removed from the plugins page.
	// Plugin reordering is now done in the sidebar navigation using the edit mode button.
	// The sidebar provides a consistent interface for rearranging navigation items.
	function initDragAndDrop() {
		// Drag-and-drop removed - use sidebar edit mode instead
		return;
	}

	// NOTE: Plugin order saving has been moved to sidebar edit mode.
	// This function is no longer used from the plugins page.
	function savePluginOrder() {
		// Moved to sidebar edit mode - see base.templ saveSidebarOrder()
		return;
	}

	// Legacy function kept for compatibility - no longer used
	function _legacySavePluginOrder() {
const list = document.getElementById('plugins-list');
const items = list.querySelectorAll('.plugin-card');
const boxID = document.getElementById('current-server-id').value;

const orders = {};
const orderedNames = [];
items.forEach((item, index) => {
	const name = item.dataset.pluginName;
	orders[name] = index;
	orderedNames.push(name);
});

fetch('/api/plugins/sort-order?server=' + encodeURIComponent(boxID), {
	method: 'POST',
	headers: { 'Content-Type': 'application/json' },
	body: JSON.stringify(orders)
})
.then(response => response.json())
.then(data => {
	if (data.error) {
		console.error('Failed to save order:', data.error);
		if (window.showToast) {
			window.showToast('Failed to save order: ' + data.error, 'error', 4000);
		}
	} else {
		// Update sidebar order to match
		updateSidebarOrder(orderedNames);
	}
})
.catch(err => {
	console.error('Failed to save order:', err);
	if (window.showToast) {
		window.showToast('Failed to save integration order', 'error', 4000);
	}
});
	}

	function updateSidebarOrder(orderedNames) {
// Find the sidebar navigation list
const sidebar = document.querySelector('nav ul.space-y-1');
if (!sidebar) return;

// Map integration names to their sidebar link hrefs
const pluginHrefs = {
	'metrics': '/history',
	'logs': '/logs',
	'services': '/services',
	'certificates': '/certificates',
	'traffic': '/traffic',
	'alerts': '/alerts'
};

// Get all sidebar items
const allItems = Array.from(sidebar.querySelectorAll('li'));

// Find integration items (those with hrefs matching our integrations)
const pluginItems = {};
const nonIntegrationItems = [];

allItems.forEach(item => {
	const link = item.querySelector('a');
	if (!link) return;

	const href = link.getAttribute('href');
	let found = false;
	for (const [name, integrationHref] of Object.entries(pluginHrefs)) {
		if (href === integrationHref) {
			pluginItems[name] = item;
			found = true;
			break;
		}
	}
	if (!found) {
		nonIntegrationItems.push(item);
	}
});

// Clear the sidebar
sidebar.innerHTML = '';

// Re-add non-integration items first (Dashboard, Status Grid)
nonIntegrationItems.forEach(item => sidebar.appendChild(item));

// Re-add integration items in the new order
orderedNames.forEach(name => {
	if (pluginItems[name]) {
		sidebar.appendChild(pluginItems[name]);
	}
});
	}

	document.addEventListener('DOMContentLoaded', function() {
setupPageHeader();
initDragAndDrop();
// Initialize global server selector from current selection
const serverSelector = document.getElementById('server-selector');
if (serverSelector && window.BoxSelector) {
	window.BoxSelector.initFromSelect(serverSelector);
}
// Set Done button to first enabled plugin on load
updateDoneButton();
	});

	// Toggle integration via AJAX - allows multiple toasts without page reload
	function togglePlugin(pluginName, displayName, button) {
const boxID = document.getElementById('current-server-id').value;
const currentEnabled = button.dataset.enabled === 'true';
const newEnabled = !currentEnabled;

// Optimistically update the UI
updateToggleUI(button, newEnabled);

fetch('/settings/plugins/' + encodeURIComponent(pluginName) + '/toggle?server=' + encodeURIComponent(boxID), {
	method: 'POST',
	headers: {
		'X-Requested-With': 'XMLHttpRequest'
	}
})
.then(response => {
	if (response.redirected) {
		// Server returned a redirect - extract success/error from URL
		const url = new URL(response.url);
		const success = url.searchParams.get('success');
		const error = url.searchParams.get('error');
		if (success && window.showToast) {
			window.showToast(success, 'success', 3000);
		} else if (error && window.showToast) {
			window.showToast(error, 'error', 4000);
			// Revert UI on error
			updateToggleUI(button, currentEnabled);
		}
		return null;
	}
	return response.json();
})
.then(data => {
	if (data && data.error) {
		if (window.showToast) {
			window.showToast(data.error, 'error', 4000);
		}
		// Revert UI on error
		updateToggleUI(button, currentEnabled);
	} else if (data && data.success) {
		if (window.showToast) {
			window.showToast(data.message || (displayName + ' has been ' + (newEnabled ? 'enabled' : 'disabled')), 'success', 3000);
		}
		// Refresh sidebar nav to reflect enabled/disabled state
		refreshSidebar();
		// Update Done button to point to first enabled plugin
		updateDoneButton();
	}
})
.catch(error => {
	console.error('Toggle failed:', error);
	if (window.showToast) {
		window.showToast('Failed to toggle ' + displayName, 'error', 4000);
	}
	// Revert UI on error
	updateToggleUI(button, currentEnabled);
});
	}

	function updateToggleUI(button, enabled) {
button.dataset.enabled = enabled ? 'true' : 'false';
button.setAttribute('aria-checked', enabled ? 'true' : 'false');
button.title = enabled ? 'Click to disable' : 'Click to enable';

// Update toggle colors
if (enabled) {
	button.classList.remove('bg-gray-200', 'dark:bg-gray-600');
	button.classList.add('bg-blue-600');
} else {
	button.classList.remove('bg-blue-600');
	button.classList.add('bg-gray-200', 'dark:bg-gray-600');
}

// Update indicator position
const indicator = button.querySelector('.toggle-indicator');
if (indicator) {
	if (enabled) {
		indicator.classList.remove('translate-x-0');
		indicator.classList.add('translate-x-5');
	} else {
		indicator.classList.remove('translate-x-5');
		indicator.classList.add('translate-x-0');
	}
}

// Update icon container colors
const card = button.closest('.plugin-card');
if (card) {
	const iconContainer = card.querySelector('.plugin-icon');
	if (iconContainer) {
		if (enabled) {
			iconContainer.classList.remove('bg-gray-100', 'dark:bg-gray-700');
			iconContainer.classList.add('bg-blue-100', 'dark:bg-blue-900');
		} else {
			iconContainer.classList.remove('bg-blue-100', 'dark:bg-blue-900');
			iconContainer.classList.add('bg-gray-100', 'dark:bg-gray-700');
		}
	}
	// Update icon colors
	const icon = card.querySelector('.plugin-icon svg');
	if (icon) {
		if (enabled) {
			icon.classList.remove('text-gray-400', 'dark:text-gray-500');
			icon.classList.add('text-blue-600', 'dark:text-blue-400');
		} else {
			icon.classList.remove('text-blue-600', 'dark:text-blue-400');
			icon.classList.add('text-gray-400', 'dark:text-gray-500');
		}
	}
}
	}

	// Plugin name to URL path mapping
	const pluginPaths = {
		'haproxy': '/haproxy',
		'metrics': '/history',
		'logs': '/logs',
		'services': '/services',
		'certificates': '/certificates',
		'traffic': '/traffic',
		'alerts': '/alerts',
		'os_updates': '/os-updates'
	};

	// Refresh sidebar nav by fetching updated HTML from server
	function refreshSidebar() {
		const navList = document.getElementById('sidebar-nav-list');
		if (!navList) return;

		fetch('/htmx/sidebar-nav')
			.then(response => {
				if (!response.ok) throw new Error('Failed to fetch sidebar');
				return response.text();
			})
			.then(html => {
				navList.innerHTML = html;
			})
			.catch(err => {
				console.error('Failed to refresh sidebar:', err);
			});
	}

	// Update Done button href to point to first enabled plugin
	function updateDoneButton() {
		const doneBtn = document.getElementById('plugins-done-btn');
		if (!doneBtn) return;

		// Read current toggle states from the plugin cards
		const cards = document.querySelectorAll('.plugin-card');
		for (const card of cards) {
			const toggle = card.querySelector('[data-enabled]');
			if (toggle && toggle.dataset.enabled === 'true') {
				const name = card.dataset.pluginName;
				if (name && pluginPaths[name]) {
					doneBtn.href = pluginPaths[name];
					return;
				}
			}
		}
		// No enabled plugins, fall back to root
		doneBtn.href = '/';
	}
