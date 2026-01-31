function toggleStoreHistory(button) {
	const input = document.getElementById('store-history-input');
	const isEnabled = input.value === 'true';
	const newValue = !isEnabled;

	// Update hidden input
	input.value = newValue ? 'true' : 'false';

	// Update button appearance
	if (newValue) {
		button.classList.remove('bg-gray-200', 'dark:bg-gray-600');
		button.classList.add('bg-blue-600');
		button.querySelector('span').classList.remove('translate-x-0');
		button.querySelector('span').classList.add('translate-x-5');
	} else {
		button.classList.remove('bg-blue-600');
		button.classList.add('bg-gray-200', 'dark:bg-gray-600');
		button.querySelector('span').classList.remove('translate-x-5');
		button.querySelector('span').classList.add('translate-x-0');
	}
	button.setAttribute('aria-checked', newValue ? 'true' : 'false');

	// Toggle retention options visibility
	toggleRetentionOptions(newValue);
}

function toggleRetentionOptions(enabled) {
	const options = document.getElementById('retention-options');
	if (enabled) {
		options.classList.remove('hidden');
	} else {
		options.classList.add('hidden');
	}
}

function loadStorageStats() {
	const serverID = new URLSearchParams(window.location.search).get('server');
	fetch('/api/' + encodeURIComponent(serverID) + '/metrics/storage-stats')
		.then(response => response.json())
		.then(data => {
			const container = document.getElementById('storage-stats');
			if (data.error) {
				container.innerHTML = '<div class="text-sm text-red-500">' + data.error + '</div>';
				return;
			}

			const sizeKB = Math.round(data.estimated_size_bytes / 1024);
			const sizeMB = (data.estimated_size_bytes / 1024 / 1024).toFixed(2);

			let oldestDate = 'N/A';
			let newestDate = 'N/A';
			if (data.oldest_record && data.oldest_record !== '0001-01-01T00:00:00Z') {
				oldestDate = new Date(data.oldest_record).toLocaleDateString();
			}
			if (data.newest_record && data.newest_record !== '0001-01-01T00:00:00Z') {
				newestDate = new Date(data.newest_record).toLocaleDateString();
			}

			container.innerHTML = `
				<div class="grid grid-cols-2 gap-4 text-sm">
					<div>
						<span class="text-gray-500 dark:text-gray-400">Total Records:</span>
						<span class="ml-2 font-medium text-gray-900 dark:text-gray-100">${data.total_records.toLocaleString()}</span>
					</div>
					<div>
						<span class="text-gray-500 dark:text-gray-400">Estimated Size:</span>
						<span class="ml-2 font-medium text-gray-900 dark:text-gray-100">${sizeMB} MB</span>
					</div>
					<div>
						<span class="text-gray-500 dark:text-gray-400">Oldest Record:</span>
						<span class="ml-2 font-medium text-gray-900 dark:text-gray-100">${oldestDate}</span>
					</div>
					<div>
						<span class="text-gray-500 dark:text-gray-400">Newest Record:</span>
						<span class="ml-2 font-medium text-gray-900 dark:text-gray-100">${newestDate}</span>
					</div>
				</div>
				<div class="mt-3 pt-3 border-t border-gray-200 dark:border-slate-600 text-xs text-gray-500 dark:text-gray-400">
					Stats: ${data.stats_records.toLocaleString()} | Backends: ${data.backend_records.toLocaleString()} | System: ${data.system_metrics_records.toLocaleString()}
				</div>
			`;
		})
		.catch(err => {
			document.getElementById('storage-stats').innerHTML = '<div class="text-sm text-red-500">Failed to load storage statistics</div>';
		});
}

async function confirmClearMetrics() {
	const confirmed = await showConfirmDialog({
		title: 'Clear Metrics Data',
		message: 'Are you sure you want to clear all metrics data?\n\nThis action cannot be undone.',
		confirmText: 'Clear Data',
		type: 'danger'
	});
	if (!confirmed) return;

	const serverID = new URLSearchParams(window.location.search).get('server');
	fetch('/api/' + encodeURIComponent(serverID) + '/metrics/clear', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' }
	})
	.then(response => response.json())
	.then(data => {
		if (data.error) {
			if (window.showToast) {
				window.showToast('Failed to clear metrics: ' + data.error, 'error', 5000);
			}
		} else {
			if (window.showToast) {
				window.showToast('Metrics data cleared successfully', 'success', 3000);
			}
			loadStorageStats();
		}
	})
	.catch(err => {
		if (window.showToast) {
			window.showToast('Failed to clear metrics data', 'error', 5000);
		}
	});
}

document.addEventListener('DOMContentLoaded', function() {
	loadStorageStats();
});
