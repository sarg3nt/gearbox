	function switchServer(serverID) {
// Update global ServerSelector for tab isolation before navigating
if (window.ServerSelector) {
	window.ServerSelector.setSelectedServer(serverID);
}
window.location.href = '/settings/plugins?server=' + encodeURIComponent(serverID);
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

	function initDragAndDrop() {
const list = document.getElementById('plugins-list');
if (!list) return;

const items = list.querySelectorAll('.plugin-card');
items.forEach(item => {
	const handle = item.querySelector('.drag-handle');
	if (!handle) return;

	handle.addEventListener('mousedown', () => {
		item.setAttribute('draggable', 'true');
	});

	handle.addEventListener('mouseup', () => {
		item.setAttribute('draggable', 'false');
	});

	item.addEventListener('dragstart', (e) => {
		draggedItem = item;
		draggedIndex = getItemIndex(item);
		currentDropIndex = draggedIndex;

		// Use setTimeout to allow the drag image to be captured first
		setTimeout(() => {
			item.classList.add('opacity-50', 'scale-[0.98]');
		}, 0);
		e.dataTransfer.effectAllowed = 'move';
		e.dataTransfer.setData('text/plain', ''); // Required for Firefox
	});

	item.addEventListener('dragend', () => {
		item.classList.remove('opacity-50', 'scale-[0.98]');
		item.setAttribute('draggable', 'false');

		// Perform the actual DOM reorder if position changed
		if (currentDropIndex !== -1 && currentDropIndex !== draggedIndex) {
			// Reset transforms instantly before DOM manipulation
			resetItemTransforms(true);

			const items = Array.from(list.querySelectorAll('.plugin-card'));

			if (currentDropIndex > draggedIndex) {
				// Moving down - insert after the item at currentDropIndex
				const targetItem = items[currentDropIndex];
				list.insertBefore(draggedItem, targetItem.nextSibling);
			} else {
				// Moving up - insert before the item at currentDropIndex
				const targetItem = items[currentDropIndex];
				list.insertBefore(draggedItem, targetItem);
			}
		} else {
			resetItemTransforms(true);
		}

		draggedItem = null;
		draggedIndex = -1;
		currentDropIndex = -1;

		// Save the new order
		savePluginOrder();
	});

	item.addEventListener('dragover', (e) => {
		e.preventDefault();
		e.dataTransfer.dropEffect = 'move';

		if (!draggedItem || draggedItem === item) return;

		const rect = item.getBoundingClientRect();
		const midY = rect.top + rect.height / 2;
		const itemIndex = getItemIndex(item);

		// Determine target index based on cursor position
		let targetIndex;
		if (e.clientY < midY) {
			targetIndex = itemIndex;
		} else {
			targetIndex = itemIndex;
			// If we're in the lower half, we want to drop after this item
			if (itemIndex > draggedIndex) {
				targetIndex = itemIndex;
			} else {
				targetIndex = itemIndex + 1;
			}
		}

		// Clamp to valid range
		const maxIndex = Array.from(list.querySelectorAll('.plugin-card')).length - 1;
		targetIndex = Math.max(0, Math.min(targetIndex, maxIndex));

		animateItems(targetIndex);
	});
});

// Handle dragover on the list itself for edge cases
list.addEventListener('dragover', (e) => {
	e.preventDefault();
});

// Reset animations if drag leaves the list area
list.addEventListener('dragleave', (e) => {
	if (!list.contains(e.relatedTarget)) {
		currentDropIndex = draggedIndex;
		resetItemTransforms(false);
	}
});
	}

	function savePluginOrder() {
const list = document.getElementById('plugins-list');
const items = list.querySelectorAll('.plugin-card');
const serverID = document.getElementById('current-server-id').value;

const orders = {};
const orderedNames = [];
items.forEach((item, index) => {
	const name = item.dataset.pluginName;
	orders[name] = index;
	orderedNames.push(name);
});

fetch('/api/integrations/sort-order?server=' + encodeURIComponent(serverID), {
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
if (serverSelector && window.ServerSelector) {
	window.ServerSelector.initFromSelect(serverSelector);
}
	});

	// Toggle integration via AJAX - allows multiple toasts without page reload
	function togglePlugin(pluginName, displayName, button) {
const serverID = document.getElementById('current-server-id').value;
const currentEnabled = button.dataset.enabled === 'true';
const newEnabled = !currentEnabled;

// Optimistically update the UI
updateToggleUI(button, newEnabled);

fetch('/settings/plugins/' + encodeURIComponent(pluginName) + '/toggle?server=' + encodeURIComponent(serverID), {
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
