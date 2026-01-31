// Toast notification system
// Provides global showToast() function for displaying notifications

// Toast notification system
window.showToast = function(message, type = 'success', duration = 5000) {
	const container = document.getElementById('toast-container');
	if (!container) return;

	// Create toast element
	const toast = document.createElement('div');
	toast.className = 'pointer-events-auto transform transition-all duration-300 ease-in-out translate-x-0 opacity-100';

	// Set colors based on type
	let bgColor, textColor, iconPath;
	switch(type) {
		case 'success':
			bgColor = 'bg-green-600 dark:bg-green-500';
			textColor = 'text-white';
			iconPath = 'M5 13l4 4L19 7'; // Checkmark
			break;
		case 'error':
			bgColor = 'bg-red-600 dark:bg-red-500';
			textColor = 'text-white';
			iconPath = 'M6 18L18 6M6 6l12 12'; // X
			break;
		case 'warning':
			bgColor = 'bg-yellow-600 dark:bg-yellow-500';
			textColor = 'text-white';
			iconPath = 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z'; // Warning triangle
			break;
		case 'info':
			bgColor = 'bg-blue-600 dark:bg-blue-500';
			textColor = 'text-white';
			iconPath = 'M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z'; // Info circle
			break;
		default:
			bgColor = 'bg-gray-800 dark:bg-gray-700';
			textColor = 'text-white';
			iconPath = 'M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z';
	}

	// Store message for copy functionality
	toast.dataset.message = message;

	toast.innerHTML = `
		<div class="${bgColor} ${textColor} rounded-lg shadow-lg px-4 py-3 flex items-center space-x-3 min-w-[300px] max-w-md">
			<svg class="w-5 h-5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="${iconPath}"></path>
			</svg>
			<span class="toast-message text-sm font-medium flex-1">${escapeHtml(message)}</span>
			<div class="flex items-center space-x-1 flex-shrink-0">
				<button class="toast-copy hover:opacity-75 transition-opacity p-1 rounded hover:bg-white/10" title="Copy to clipboard">
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path>
					</svg>
				</button>
				<button class="toast-close hover:opacity-75 transition-opacity p-1 rounded hover:bg-white/10" title="Dismiss">
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
					</svg>
				</button>
			</div>
		</div>
	`;

	// Add to container
	container.appendChild(toast);

	// Set up close button
	toast.querySelector('.toast-close').addEventListener('click', function() {
		dismissToast(toast);
	});

	// Set up copy button
	toast.querySelector('.toast-copy').addEventListener('click', function(e) {
		e.stopPropagation();
		copyToastMessage(toast, this);
	});

	// Animate in (slide from right)
	requestAnimationFrame(() => {
		toast.style.transform = 'translateX(0)';
		toast.style.opacity = '1';
	});

	// Auto-remove after duration with hover pause
	if (duration > 0) {
		let timeoutId = null;
		let remainingTime = duration;
		let startTime = Date.now();

		function startTimer() {
			startTime = Date.now();
			timeoutId = setTimeout(() => {
				dismissToast(toast);
			}, remainingTime);
		}

		function pauseTimer() {
			if (timeoutId) {
				clearTimeout(timeoutId);
				timeoutId = null;
				remainingTime -= (Date.now() - startTime);
				if (remainingTime < 0) remainingTime = 0;
			}
		}

		// Pause timer on hover
		toast.addEventListener('mouseenter', pauseTimer);
		toast.addEventListener('mouseleave', startTimer);

		// Start the timer
		startTimer();

		// Store cleanup function
		toast._cleanup = function() {
			if (timeoutId) clearTimeout(timeoutId);
			toast.removeEventListener('mouseenter', pauseTimer);
			toast.removeEventListener('mouseleave', startTimer);
		};
	}

	return toast;
};

// Dismiss a toast with animation
function dismissToast(toast) {
	if (toast._cleanup) toast._cleanup();
	toast.style.transform = 'translateX(400px)';
	toast.style.opacity = '0';
	setTimeout(() => {
		toast.remove();
	}, 300);
}

// Copy toast message to clipboard
function copyToastMessage(toast, button) {
	const message = toast.dataset.message;
	if (!message) return;

	navigator.clipboard.writeText(message).then(function() {
		// Show brief visual feedback
		const originalHTML = button.innerHTML;
		button.innerHTML = `
			<svg class="w-4 h-4 text-green-300" fill="none" stroke="currentColor" viewBox="0 0 24 24">
				<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
			</svg>
		`;
		setTimeout(() => {
			button.innerHTML = originalHTML;
		}, 1500);
	}).catch(function(err) {
		console.error('Failed to copy:', err);
	});
}

// Helper to escape HTML
function escapeHtml(text) {
	const div = document.createElement('div');
	div.textContent = text;
	return div.innerHTML;
}

// Process any toast-on-load elements (from ToastOnLoad component)
document.addEventListener('DOMContentLoaded', function() {
	document.querySelectorAll('.toast-on-load').forEach(function(el) {
		const msg = el.dataset.toastMessage;
		const type = el.dataset.toastType || 'success';
		el.remove();
		if (msg) {
			window.showToast(msg, type, 6000);
		}
	});
});
