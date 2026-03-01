/**
 * Fullscreen toggle functionality for chart widgets
 */

let currentFullscreenCard = null;

/**
 * Toggle fullscreen mode for a chart card
 * @param {string} cardId - DOM ID of the chart card element
 * @param {Function} onResize - Callback to resize chart after fullscreen toggle
 */
export function toggleFullscreen(cardId, onResize) {
	const card = document.getElementById(cardId);
	if (!card) {
		console.error('toggleFullscreen: card not found:', cardId);
		return;
	}

	// Find the grid container (could be dashboard grid or charts grid)
	const grid = card.closest('.grid') || card.closest('[id$="-grid"]');
	if (!grid) {
		console.error('toggleFullscreen: grid container not found for card:', cardId);
		return;
	}

	if (card.classList.contains('fullscreen')) {
		// Exit fullscreen
		card.classList.remove('fullscreen');
		// Show all other cards in the grid
		grid.querySelectorAll('.chart-card').forEach(c => {
			if (c.id !== cardId) {
				c.classList.remove('hidden');
			}
		});
		currentFullscreenCard = null;

		// Trigger resize callback after a short delay
		if (onResize) {
			setTimeout(onResize, 50);
		}
	} else {
		// Enter fullscreen
		card.classList.add('fullscreen');
		// Hide all other cards in the grid
		grid.querySelectorAll('.chart-card').forEach(c => {
			if (c.id !== cardId) {
				c.classList.add('hidden');
			}
		});
		currentFullscreenCard = cardId;

		// Trigger resize callback after a short delay
		if (onResize) {
			setTimeout(onResize, 100);
		}
	}
}

/**
 * Check if any card is currently fullscreen
 * @returns {string|null} Card ID if fullscreen, null otherwise
 */
export function getCurrentFullscreenCard() {
	return currentFullscreenCard;
}

/**
 * Exit fullscreen mode if active
 * @param {Function} onResize - Callback to resize chart after exit
 */
export function exitFullscreen(onResize) {
	if (currentFullscreenCard) {
		toggleFullscreen(currentFullscreenCard, onResize);
	}
}

/**
 * Setup Escape key handler to exit fullscreen
 * @param {Function} onResize - Callback to resize chart after exit
 */
export function setupFullscreenKeyHandler(onResize) {
	document.addEventListener('keydown', function(e) {
		if (e.key === 'Escape' && currentFullscreenCard) {
			exitFullscreen(onResize);
		}
	});
}

/**
 * Get fullscreen button HTML
 * @param {string} onclickHandler - onclick handler function call
 * @returns {string} HTML string for fullscreen button
 */
export function getFullscreenButtonHTML(onclickHandler) {
	return `
		<button onclick="${onclickHandler}" class="fullscreen-btn p-1 hover:bg-gray-200 dark:hover:bg-slate-600 rounded transition-colors" title="Toggle fullscreen">
			<svg class="w-5 h-5 text-gray-500 dark:text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 8V4m0 0h4M4 4l5 5m11-1V4m0 0h-4m4 0l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4"></path>
			</svg>
		</button>
	`;
}
