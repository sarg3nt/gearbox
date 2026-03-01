/**
 * Server-Sent Events (SSE) connection management for real-time chart updates
 */

let eventSource = null;
let eventHandlers = [];

/**
 * Setup SSE connection for real-time updates
 * @param {string} serverID - Box/server ID to subscribe to
 * @param {Function} onConnectionChange - Callback with connection status (true/false)
 * @returns {EventSource} The EventSource instance
 */
export function setupSSE(serverID, onConnectionChange) {
	// Close existing connection
	closeSSE();

	const url = `/api/events?server=${serverID}`;
	eventSource = new EventSource(url);

	eventSource.onopen = function() {
		console.log('SSE connected:', url);
		if (onConnectionChange) onConnectionChange(true);
	};

	eventSource.onerror = function(error) {
		console.error('SSE error:', error);
		if (onConnectionChange) onConnectionChange(false);
	};

	return eventSource;
}

/**
 * Add event listener for SSE events
 * @param {string} eventType - Event type to listen for (e.g., 'stats.updated')
 * @param {Function} handler - Event handler function
 */
export function addSSEEventListener(eventType, handler) {
	if (!eventSource) {
		console.warn('addSSEEventListener: no active SSE connection');
		return;
	}

	eventSource.addEventListener(eventType, handler);
	eventHandlers.push({ eventType, handler });
}

/**
 * Remove all event listeners and close SSE connection
 */
export function closeSSE() {
	if (eventSource) {
		// Remove all registered handlers
		eventHandlers.forEach(({ eventType, handler }) => {
			eventSource.removeEventListener(eventType, handler);
		});
		eventHandlers = [];

		eventSource.close();
		eventSource = null;
	}
}

/**
 * Get active SSE connection
 * @returns {EventSource|null} The EventSource instance or null
 */
export function getSSEConnection() {
	return eventSource;
}

/**
 * Check if SSE is connected
 * @returns {boolean} True if connected
 */
export function isSSEConnected() {
	return eventSource && eventSource.readyState === EventSource.OPEN;
}

/**
 * Update connection status indicator UI
 * @param {boolean} connected - Connection status
 * @param {string} buttonId - ID of the refresh button element (optional)
 */
export function updateConnectionStatus(connected, buttonId = 'refresh-btn') {
	const btn = document.getElementById(buttonId);
	if (!btn) return;

	// Remove all status classes
	btn.classList.remove('text-green-500', 'text-red-500', 'text-yellow-500', 'text-gray-400');

	if (connected) {
		btn.classList.add('text-green-500');
		btn.title = 'Live - streaming data';
	} else {
		btn.classList.add('text-red-500');
		btn.title = 'Disconnected - click to reconnect';
	}
}

/**
 * Trigger manual refresh animation
 * @param {string} iconId - ID of the refresh icon element (optional)
 */
export function triggerRefreshAnimation(iconId = 'refresh-icon') {
	const icon = document.getElementById(iconId);
	if (icon) {
		icon.classList.add('animate-spin');
		setTimeout(() => icon.classList.remove('animate-spin'), 1000);
	}
}
