let currentServerID = '';
let selectedPackages = new Set();
let currentOperationID = null;
let terminalLineCount = 0;
let sseConnection = null;
const eventHandlers = {};
let installingPackages = new Set();
let totalInstallSize = 0;
let installedSize = 0;
let packagesInstalled = 0;
let totalPackagesToInstall = 0;
let operationPollInterval = null;
let operationReceivedSSE = false;
let isRebooting = false;
let rebootPollInterval = null;

// Reboot state persistence via localStorage
const REBOOT_STORAGE_KEY = 'gearbox_reboot_state';
const REBOOT_TIMEOUT_MS = 30 * 60 * 1000; // 30 minutes

function getRebootState() {
	try {
		const raw = localStorage.getItem(REBOOT_STORAGE_KEY);
		if (!raw) return null;
		return JSON.parse(raw);
	} catch {
		return null;
	}
}

function setRebootState(serverID) {
	localStorage.setItem(REBOOT_STORAGE_KEY, JSON.stringify({
		serverID: serverID,
		timestamp: Date.now()
	}));
}

function clearRebootState() {
	localStorage.removeItem(REBOOT_STORAGE_KEY);
}

// Poll the agent to detect when the server comes back online after reboot
function startRebootPolling() {
	stopRebootPolling();
	rebootPollInterval = setInterval(async () => {
		try {
			const response = await fetch('/api/os-updates/status?server=' + currentServerID);
			if (response.ok) {
				// Server is reachable — reboot is complete
				stopRebootPolling();
				isRebooting = false;
				clearRebootState();
				showRebootNotRequiredStatus();
				showToast('Reboot complete. Server is back online.', 'success');
				autoCheckForUpdates();
			}
		} catch {
			// Server still down, keep polling
		}
	}, 5000);
}

function stopRebootPolling() {
	if (rebootPollInterval) {
		clearInterval(rebootPollInterval);
		rebootPollInterval = null;
	}
}

// Extract error message from a failed fetch response.
// Tries to parse JSON { message } or { error }, falls back to status text.
async function extractErrorMessage(response) {
	try {
		const text = await response.text();
		try {
			const json = JSON.parse(text);
			return json.message || json.error || text;
		} catch {
			return text || response.statusText;
		}
	} catch {
		return response.statusText || 'Unknown error';
	}
}

// Move page header content to main header
function setupPageHeader() {
	const source = document.getElementById('page-header-source');
	const target = document.getElementById('header-page-content');
	if (source && target) {
		while (source.firstChild) {
			target.appendChild(source.firstChild);
		}
	}
}

document.addEventListener('DOMContentLoaded', function() {
	setupPageHeader();
	const serverInput = document.getElementById('current-server-id');
	if (serverInput) {
		currentServerID = serverInput.value;
	}

	// Format snapshot dates in local time
	document.querySelectorAll('.snapshot-date').forEach(el => {
		const ts = el.dataset.timestamp;
		if (ts) {
			const d = new Date(ts);
			if (!isNaN(d)) {
				el.textContent = d.toLocaleString(undefined, {
					month: 'short', day: 'numeric', year: 'numeric',
					hour: 'numeric', minute: '2-digit', hour12: true
				});
			}
		}
	});

	// Bind snapshot button events
	document.querySelectorAll('.snapshot-preview-btn').forEach(btn => {
		btn.addEventListener('click', () => previewSnapshot(btn.dataset.snapshotId));
	});
	document.querySelectorAll('.snapshot-restore-btn').forEach(btn => {
		btn.addEventListener('click', () => restoreSnapshot(btn.dataset.snapshotId));
	});
	document.querySelectorAll('.snapshot-delete-btn').forEach(btn => {
		btn.addEventListener('click', () => deleteSnapshot(btn.dataset.snapshotId));
	});

	// Bind pipx button events
	document.querySelectorAll('.pipx-upgrade-btn').forEach(btn => {
		btn.addEventListener('click', () => upgradePipxPackage(btn, btn.dataset.packageName));
	});
	document.querySelectorAll('.pipx-uninstall-btn').forEach(btn => {
		btn.addEventListener('click', () => uninstallPipxPackage(btn, btn.dataset.packageName));
	});

	// Bind pip button events
	document.querySelectorAll('.pip-upgrade-btn').forEach(btn => {
		btn.addEventListener('click', () => upgradePipPackage(btn, btn.dataset.packageName));
	});
	document.querySelectorAll('.pip-uninstall-btn').forEach(btn => {
		btn.addEventListener('click', () => uninstallPipPackage(btn, btn.dataset.packageName));
	});

	// Bind package info button events
	document.querySelectorAll('.package-info-btn').forEach(btn => {
		btn.addEventListener('click', () => showPackageInfo(btn));
	});

	// Restore reboot state from localStorage if a reboot was in progress
	if (currentServerID) {
		const savedState = getRebootState();
		if (savedState && savedState.serverID === currentServerID) {
			if (Date.now() - savedState.timestamp < REBOOT_TIMEOUT_MS) {
				isRebooting = true;
				showRebootingStatus();
				startRebootPolling();
			} else {
				// Stale reboot state (>30 min) — clear it
				clearRebootState();
			}
		}
	}

	// Initialize SSE connection for real-time apt events
	// Delay slightly to ensure currentServerID is set
	setTimeout(initSSE, 100);

	// Lazily load Python package version info (slow PyPI check, done async)
	if (document.querySelector('.pipx-version-cell, .pip-version-cell')) {
		setTimeout(loadPythonVersions, 200);
	}

	// Lazily load installed packages table
	if (document.getElementById('installed-packages-loading')) {
		setTimeout(loadInstalledPackages, 300);
	}

	// Auto-check for updates on page load (if user has action permissions)
	const canAction = document.getElementById('can-action');
	if (canAction && canAction.value === 'true' && currentServerID) {
		const autoCheckKey = 'os-updates-auto-checked-' + currentServerID;
		const lastCheck = parseInt(sessionStorage.getItem(autoCheckKey) || '0');
		const now = Date.now();
		if (now - lastCheck > 30000) {
			sessionStorage.setItem(autoCheckKey, String(now));
			setTimeout(autoCheckForUpdates, 500);
		}
	}
});

// Cleanup SSE connection on page unload
window.addEventListener('beforeunload', function() {
	stopRebootPolling();
	if (sseConnection) {
		sseConnection.close();
	}
});

// Event handler registration (global functions for terminal modal)
window.registerEventHandler = function(eventType, handler) {
	if (!eventHandlers[eventType]) {
		eventHandlers[eventType] = [];
	}
	eventHandlers[eventType].push(handler);
};

window.unregisterEventHandler = function(eventType, handler) {
	if (eventHandlers[eventType]) {
		eventHandlers[eventType] = eventHandlers[eventType].filter(h => h !== handler);
	}
};

function initSSE() {
	// Don't initialize without a server ID
	if (!currentServerID) {
		console.warn('SSE: No server ID available, skipping initialization');
		return;
	}

	if (sseConnection) {
		sseConnection.close();
	}

	updateSSEStatus('connecting');

	const sseUrl = `/api/events?server=${currentServerID}`;
	sseConnection = new EventSource(sseUrl);

	sseConnection.onopen = function() {
		console.log('SSE: Connection established');
		updateSSEStatus('connected');
	};

	// Listen for each named event type individually.
	// The SSE server sends named events (event: apt.output\ndata: ...\n\n)
	// which do NOT trigger onmessage — they require addEventListener.
	const sseEventTypes = [
		'apt.started', 'apt.output', 'apt.completed', 'apt.failed',
		'server.connected', 'server.disconnected',
	];
	sseEventTypes.forEach(eventType => {
		sseConnection.addEventListener(eventType, function(e) {
			try {
				const event = JSON.parse(e.data);
				const type = event.type || eventType;
				if (eventHandlers[type]) {
					eventHandlers[type].forEach(handler => {
						try {
							handler(event);
						} catch (err) {
							console.error('Error in event handler:', err);
						}
					});
				}
			} catch (err) {
				console.error('Failed to parse SSE event:', err);
			}
		});
	});

	// Handle server disconnection (reboot or network issue)
	sseConnection.addEventListener('server.disconnected', function(e) {
		try {
			const event = JSON.parse(e.data);
			console.log('SSE: Server disconnected', event.data);
			// Only show rebooting UI if user explicitly initiated a reboot.
			// Don't assume every disconnect is a reboot — could be a transient issue.
			if (isRebooting) {
				showRebootingStatus();
			}
		} catch (err) {
			console.error('Failed to handle server.disconnected:', err);
		}
	});

	// Handle server reconnection after reboot
	sseConnection.addEventListener('server.connected', function(e) {
		try {
			const event = JSON.parse(e.data);
			console.log('SSE: Server connected', event.data);
			if (isRebooting) {
				isRebooting = false;
				clearRebootState();
				stopRebootPolling();
				showRebootNotRequiredStatus();
				showToast('Reboot complete. Server is back online.', 'success');
				autoCheckForUpdates();
			}
		} catch (err) {
			console.error('Failed to handle server.connected:', err);
		}
	});

	sseConnection.onerror = function(e) {
		updateSSEStatus('disconnected');
		// Only log if not a normal page unload
		if (sseConnection.readyState !== EventSource.CLOSED) {
			console.warn('SSE: Connection error, will retry...');
		}
		// Reconnect after delay
		setTimeout(() => {
			if (sseConnection && sseConnection.readyState === EventSource.CLOSED) {
				initSSE();
			}
		}, 5000);
	};
}

// Manual refresh triggered by the LiveRefreshButton
function manualRefresh() {
	const icon = document.getElementById('refresh-icon');
	if (icon) icon.classList.add('animate-spin');
	window.location.reload();
}

function switchServer(serverID) {
	window.location.href = '/os-updates?server=' + serverID;
}

async function refreshPageData() {
	window.location.reload();
}

async function checkForUpdates() {
	const btn = document.getElementById('check-updates-btn');
	if (btn) {
		btn.disabled = true;
		btn.innerHTML = '<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg> Checking...';
	}

	try {
		const response = await fetch('/api/os-updates/check?server=' + currentServerID, { method: 'POST' });
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}

		const data = await response.json();
		showToast('Update check complete. Found ' + data.total_updates + ' updates (' + data.security_updates + ' security).', 'success');

		// Update UI in-place
		const totalEl = document.getElementById('total-updates');
		const securityEl = document.getElementById('security-updates');
		if (totalEl) totalEl.textContent = data.total_updates;
		if (securityEl) {
			securityEl.textContent = data.security_updates;
			if (data.security_updates > 0) {
				securityEl.className = 'text-2xl font-bold mt-2 text-red-600 dark:text-red-400';
			} else {
				securityEl.className = 'text-2xl font-bold mt-2 text-gray-800 dark:text-gray-100';
			}
		}
		await refreshPackageTable();

		if (btn) {
			btn.disabled = false;
			btn.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"></path></svg> Check for Updates';
		}
	} catch (err) {
		showToast('Failed to check for updates: ' + err.message, 'error');
		if (btn) {
			btn.disabled = false;
			btn.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"></path></svg> Check for Updates';
		}
	}
}

async function autoCheckForUpdates() {
	try {
		const response = await fetch('/api/os-updates/check?server=' + currentServerID, { method: 'POST' });
		if (!response.ok) {
			console.warn('Auto-check for updates failed:', response.status);
			return;
		}
		const data = await response.json();

		// Update status cards in-place
		const totalEl = document.getElementById('total-updates');
		const securityEl = document.getElementById('security-updates');
		if (totalEl) totalEl.textContent = data.total_updates;
		if (securityEl) {
			securityEl.textContent = data.security_updates;
			if (data.security_updates > 0) {
				securityEl.className = 'text-2xl font-bold mt-2 text-red-600 dark:text-red-400';
			} else {
				securityEl.className = 'text-2xl font-bold mt-2 text-gray-800 dark:text-gray-100';
			}
		}

		// If package count changed, refresh the package table
		const currentCount = document.querySelectorAll('.package-row').length;
		if (data.total_updates !== currentCount) {
			await refreshPackageTable();
		}
	} catch (err) {
		console.warn('Auto-check for updates error:', err.message);
	}
}

function formatBytes(bytes) {
	if (!bytes || bytes === 0) return '-';
	const units = ['B', 'KB', 'MB', 'GB', 'TB'];
	let i = 0;
	let size = bytes;
	while (size >= 1024 && i < units.length - 1) {
		size /= 1024;
		i++;
	}
	return i === 0 ? size + ' B' : size.toFixed(1) + ' ' + units[i];
}

async function refreshPackageTable() {
	try {
		const response = await fetch('/api/os-updates/packages?server=' + currentServerID);
		if (!response.ok) return;
		const data = await response.json();

		const tbody = document.getElementById('packages-table');
		if (!tbody) return;

		const canAction = document.getElementById('can-action');
		const hasAction = canAction && canAction.value === 'true';

		if (!data.packages || data.packages.length === 0) {
			tbody.innerHTML = '<tr><td colspan="8" class="px-4 py-8 text-center text-gray-500 dark:text-gray-400">No updates available - system is up to date!</td></tr>';
			return;
		}

		selectedPackages.clear();
		updateSelectedPackagesUI();

		tbody.innerHTML = '';
		data.packages.forEach(pkg => {
			const row = document.createElement('tr');
			row.className = 'package-row hover:bg-gray-50 dark:hover:bg-slate-700 transition-opacity duration-300';
			row.dataset.packageName = pkg.name;
			row.dataset.packageSize = pkg.size_bytes || 0;

			let html = '';
			if (hasAction) {
				html += '<td class="px-4 py-3"><input type="checkbox" class="package-checkbox w-4 h-4 rounded border-gray-300 dark:border-slate-600 bg-white dark:bg-slate-700" data-package="' + escapeHtml(pkg.name) + '"/></td>';
			}
			html += '<td class="px-4 py-3 package-status-cell"></td>';
			html += '<td class="px-4 py-3 text-gray-800 dark:text-gray-200 font-medium package-name-cell">' + escapeHtml(pkg.name) + '</td>';
			html += '<td class="px-4 py-3 text-gray-600 dark:text-gray-400 font-mono text-xs">' + escapeHtml(pkg.current_version) + '</td>';
			html += '<td class="px-4 py-3 text-gray-600 dark:text-gray-400 font-mono text-xs">' + escapeHtml(pkg.available_version) + '</td>';
			if (pkg.is_security_update) {
				html += '<td class="px-4 py-3"><span class="px-2 py-1 text-xs bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400 rounded">Security</span></td>';
			} else {
				html += '<td class="px-4 py-3"><span class="px-2 py-1 text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 rounded">Standard</span></td>';
			}
			html += '<td class="px-4 py-3 text-gray-600 dark:text-gray-400">' + formatBytes(pkg.size_bytes) + '</td>';
			html += '<td class="px-4 py-3"><button class="package-info-btn p-1.5 rounded hover:bg-gray-100 dark:hover:bg-slate-600 text-gray-500 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400 transition-colors"'
				+ ' data-package-name="' + escapeHtml(pkg.name) + '"'
				+ ' data-package-current="' + escapeHtml(pkg.current_version) + '"'
				+ ' data-package-available="' + escapeHtml(pkg.available_version) + '"'
				+ ' data-package-arch="' + escapeHtml(pkg.architecture || '') + '"'
				+ ' data-package-repo="' + escapeHtml(pkg.repository || '') + '"'
				+ ' data-package-size="' + formatBytes(pkg.size_bytes) + '"'
				+ ' data-package-security="' + (pkg.is_security_update ? 'true' : 'false') + '"'
				+ ' data-changelog-url="' + escapeHtml(pkg.changelog_url || '') + '"'
				+ ' title="View package info">'
				+ '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>'
				+ '</button></td>';

			row.innerHTML = html;
			tbody.appendChild(row);
		});

		// Re-bind package info buttons for new rows
		tbody.querySelectorAll('.package-info-btn').forEach(btn => {
			btn.addEventListener('click', () => showPackageInfo(btn));
		});
	} catch (err) {
		console.warn('Failed to refresh package table:', err.message);
	}
}

function installAllUpdates() {
	showConfirmModal({
		title: 'Install All Updates',
		message: 'Install all available updates? This may take several minutes.',
		type: 'warning',
		confirmText: 'Install Updates',
		onConfirm: async () => {
			try {
				const response = await fetch('/api/os-updates/install?server=' + currentServerID + '&stream=true', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({})
				});
				if (!response.ok) {
					const errMsg = await extractErrorMessage(response);
					throw new Error(errMsg);
				}
				const data = await response.json();
				if (data.operation_id) {
					openTerminalModal(data.operation_id, 'Installing All Updates', true);
				} else {
					showToast(data.message || 'Updates installed successfully', 'success');
					setTimeout(() => window.location.reload(), 3000);
				}
			} catch (err) {
				showToast('Failed to install updates: ' + err.message, 'error');
			}
		}
	});
}

function installSecurityUpdates() {
	showConfirmModal({
		title: 'Install Security Updates',
		message: 'Install security updates only? This will only apply critical security fixes.',
		type: 'danger',
		confirmText: 'Install Security Updates',
		onConfirm: async () => {
			try {
				const response = await fetch('/api/os-updates/install?server=' + currentServerID + '&stream=true', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ security_only: true })
				});
				if (!response.ok) {
					const errMsg = await extractErrorMessage(response);
					throw new Error(errMsg);
				}
				const data = await response.json();
				if (data.operation_id) {
					openTerminalModal(data.operation_id, 'Installing Security Updates', true);
				} else {
					showToast(data.message || 'Security updates installed', 'success');
					setTimeout(() => window.location.reload(), 3000);
				}
			} catch (err) {
				showToast('Failed to install security updates: ' + err.message, 'error');
			}
		}
	});
}

function installSelectedPackages() {
	const packages = Array.from(selectedPackages);
	if (packages.length === 0) {
		showToast('No packages selected', 'warning');
		return;
	}

	showConfirmModal({
		title: 'Install Selected Packages',
		message: 'Install ' + packages.length + ' selected package(s)?',
		type: 'info',
		confirmText: 'Install',
		onConfirm: async () => {
			try {
				const response = await fetch('/api/os-updates/install?server=' + currentServerID + '&stream=true', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ packages: packages })
				});
				if (!response.ok) {
					const errMsg = await extractErrorMessage(response);
					throw new Error(errMsg);
				}
				const data = await response.json();
				if (data.operation_id) {
					openTerminalModal(data.operation_id, 'Installing ' + packages.length + ' Package(s)', false);
				} else {
					showToast(data.message || 'Packages installed', 'success');
					setTimeout(() => window.location.reload(), 3000);
				}
			} catch (err) {
				showToast('Failed to install packages: ' + err.message, 'error');
			}
		}
	});
}

function toggleSelectAllPackages(checked) {
	document.querySelectorAll('.package-checkbox').forEach(cb => {
		cb.checked = checked;
		const pkg = cb.dataset.package;
		if (checked) {
			selectedPackages.add(pkg);
		} else {
			selectedPackages.delete(pkg);
		}
	});
	updateSelectedPackagesUI();
}

document.addEventListener('change', function(e) {
	if (e.target.classList.contains('package-checkbox')) {
		const pkg = e.target.dataset.package;
		if (e.target.checked) {
			selectedPackages.add(pkg);
		} else {
			selectedPackages.delete(pkg);
		}
		updateSelectedPackagesUI();
	}
});

function updateSelectedPackagesUI() {
	const actionsDiv = document.getElementById('selected-packages-actions');
	const countSpan = document.getElementById('selected-count');
	if (actionsDiv && countSpan) {
		if (selectedPackages.size > 0) {
			actionsDiv.classList.remove('hidden');
			countSpan.textContent = selectedPackages.size + ' packages selected';
		} else {
			actionsDiv.classList.add('hidden');
		}
	}
}

function filterPackages(query) {
	const rows = document.querySelectorAll('#packages-table tr');
	query = query.toLowerCase();
	rows.forEach(row => {
		const packageName = row.querySelector('td:nth-child(2)')?.textContent || '';
		if (packageName.toLowerCase().includes(query) || query === '') {
			row.style.display = '';
		} else {
			row.style.display = 'none';
		}
	});
}

async function createSnapshot(reason) {
	try {
		const response = await fetch('/api/os-updates/snapshots?server=' + currentServerID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ reason: reason })
		});
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}
		showToast('Snapshot created', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		showToast('Failed to create snapshot: ' + err.message, 'error');
	}
}

function createManualSnapshot() {
	showConfirmModal({
		title: 'Create Snapshot',
		message: 'Create a snapshot of the current package state? You can add an optional description.',
		type: 'info',
		confirmText: 'Create Snapshot',
		showInput: true,
		inputPlaceholder: 'e.g. Before nginx upgrade',
		inputMaxLength: 255,
		onConfirm: async (inputVal) => {
			const description = (inputVal || '').trim();
			const reason = description || 'manual';
			await createSnapshot(reason);
		}
	});
}

async function previewSnapshot(snapshotID) {
	const modal = document.getElementById('preview-modal');
	const title = document.getElementById('preview-title');
	const content = document.getElementById('preview-content');

	if (!modal || !content) return;

	title.textContent = 'Snapshot Preview — ' + snapshotID;
	content.innerHTML = '<div class="flex items-center justify-center py-8"><svg class="animate-spin h-6 w-6 text-blue-500" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg><span class="ml-2 text-gray-500 dark:text-gray-400">Loading preview...</span></div>';
	modal.classList.remove('hidden');

	try {
		const response = await fetch('/api/os-updates/snapshots/' + encodeURIComponent(snapshotID) + '/preview?server=' + currentServerID);
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}
		const data = await response.json();
		content.innerHTML = renderPreviewContent(data);
	} catch (err) {
		content.innerHTML = '<div class="text-center py-8"><p class="text-red-500">Failed to load preview: ' + escapeHtml(err.message) + '</p></div>';
	}
}

function renderPreviewContent(data) {
	if (!data.has_versions && (!data.installs || data.installs.length === 0) && (!data.removals || data.removals.length === 0)) {
		return '<div class="text-center py-8 text-gray-500 dark:text-gray-400"><p class="text-lg mb-2">No version data available</p><p class="text-sm">This snapshot was created before version tracking was added. Create a new snapshot to get version-aware previews.</p></div>';
	}

	if (data.total_changes === 0) {
		return '<div class="text-center py-8 text-gray-500 dark:text-gray-400"><p class="text-lg">No changes needed</p><p class="text-sm mt-1">The current system state matches this snapshot.</p></div>';
	}

	let html = '<p class="text-sm text-gray-500 dark:text-gray-400 mb-4">' + data.total_changes + ' change(s) would be applied:</p>';

	if (data.downgrades && data.downgrades.length > 0) {
		html += '<div class="mb-4"><h4 class="text-sm font-semibold text-yellow-600 dark:text-yellow-400 mb-2">Downgrades (' + data.downgrades.length + ')</h4>';
		html += '<div class="bg-gray-50 dark:bg-slate-700/50 rounded-lg overflow-hidden"><table class="w-full text-sm"><thead><tr class="border-b border-gray-200 dark:border-slate-600"><th class="px-3 py-2 text-left text-gray-500 dark:text-gray-400 font-medium">Package</th><th class="px-3 py-2 text-left text-gray-500 dark:text-gray-400 font-medium">Current</th><th class="px-3 py-2 text-center text-gray-400">→</th><th class="px-3 py-2 text-left text-gray-500 dark:text-gray-400 font-medium">Snapshot</th></tr></thead><tbody>';
		data.downgrades.forEach(function(d) {
			html += '<tr class="border-b border-gray-100 dark:border-slate-600/50"><td class="px-3 py-1.5 text-gray-800 dark:text-gray-200 font-mono text-xs">' + escapeHtml(d.package) + '</td><td class="px-3 py-1.5 text-red-600 dark:text-red-400 font-mono text-xs">' + escapeHtml(d.current_version) + '</td><td class="px-3 py-1.5 text-center text-gray-400">→</td><td class="px-3 py-1.5 text-green-600 dark:text-green-400 font-mono text-xs">' + escapeHtml(d.target_version) + '</td></tr>';
		});
		html += '</tbody></table></div></div>';
	}

	if (data.installs && data.installs.length > 0) {
		html += '<div class="mb-4"><h4 class="text-sm font-semibold text-green-600 dark:text-green-400 mb-2">Would be installed (' + data.installs.length + ')</h4>';
		html += '<div class="flex flex-wrap gap-1">';
		data.installs.forEach(function(pkg) {
			html += '<span class="px-2 py-0.5 text-xs bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400 rounded font-mono">' + escapeHtml(pkg) + '</span>';
		});
		html += '</div></div>';
	}

	if (data.removals && data.removals.length > 0) {
		html += '<div class="mb-4"><h4 class="text-sm font-semibold text-red-600 dark:text-red-400 mb-2">Would be removed (' + data.removals.length + ')</h4>';
		html += '<div class="flex flex-wrap gap-1">';
		data.removals.forEach(function(pkg) {
			html += '<span class="px-2 py-0.5 text-xs bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400 rounded font-mono">' + escapeHtml(pkg) + '</span>';
		});
		html += '</div></div>';
	}

	return html;
}

function hidePreviewModal() {
	const modal = document.getElementById('preview-modal');
	if (modal) modal.classList.add('hidden');
}

function escapeHtml(str) {
	const div = document.createElement('div');
	div.appendChild(document.createTextNode(str));
	return div.innerHTML;
}

function restoreSnapshot(snapshotID) {
	showConfirmModal({
		title: 'Restore Snapshot',
		message: 'Restore system to snapshot ' + snapshotID + '? This will downgrade packages to their previous versions.',
		type: 'warning',
		confirmText: 'Restore',
		onConfirm: async () => {
			try {
				const response = await fetch('/api/os-updates/snapshots/restore?server=' + currentServerID, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ snapshot_id: snapshotID })
				});
				if (!response.ok) {
					const errMsg = await extractErrorMessage(response);
					throw new Error(errMsg);
				}
				const data = await response.json();
				if (data.operation_id) {
					openTerminalModal(data.operation_id, 'Restoring Snapshot', false);
				} else {
					showToast('Snapshot restored successfully. Reloading...', 'success', {
						onClose: () => window.location.reload()
					});
				}
			} catch (err) {
				showToast('Failed to restore snapshot: ' + err.message, 'error');
			}
		}
	});
}

function deleteSnapshot(snapshotID) {
	showConfirmModal({
		title: 'Delete Snapshot',
		message: 'Delete snapshot ' + snapshotID + '? This action cannot be undone.',
		type: 'danger',
		confirmText: 'Delete',
		onConfirm: async () => {
			// Show loading indicator on the delete button
			const deleteBtn = document.querySelector('.snapshot-delete-btn[data-snapshot-id="' + snapshotID + '"]');
			if (deleteBtn) {
				deleteBtn.disabled = true;
				deleteBtn.innerHTML = '<svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>';
			}
			try {
				const response = await fetch('/api/os-updates/snapshots/' + snapshotID + '?server=' + currentServerID, {
					method: 'DELETE'
				});
				if (!response.ok) {
					const errMsg = await extractErrorMessage(response);
					throw new Error(errMsg);
				}
				showToast('Snapshot deleted', 'success');
				// Remove the snapshot row from the DOM immediately
				const snapshotRow = document.querySelector('[data-snapshot-row="' + snapshotID + '"]');
				if (snapshotRow) {
					snapshotRow.remove();
					updateBulkDeleteButton();
				} else {
					setTimeout(() => window.location.reload(), 1000);
				}
			} catch (err) {
				showToast('Failed to delete snapshot: ' + err.message, 'error');
				if (deleteBtn) {
					deleteBtn.disabled = false;
					deleteBtn.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>';
				}
			}
		}
	});
}

function toggleSelectAllSnapshots(checked) {
	document.querySelectorAll('.snapshot-checkbox').forEach(cb => {
		cb.checked = checked;
	});
	updateBulkDeleteButton();
}

function updateBulkDeleteButton() {
	const btn = document.getElementById('bulk-delete-snapshots-btn');
	if (!btn) return;
	const all = document.querySelectorAll('.snapshot-checkbox');
	const checked = document.querySelectorAll('.snapshot-checkbox:checked');
	const selectAll = document.getElementById('select-all-snapshots');
	if (selectAll) {
		selectAll.checked = all.length > 0 && checked.length === all.length;
		selectAll.indeterminate = checked.length > 0 && checked.length < all.length;
	}
	if (checked.length > 0) {
		btn.classList.remove('hidden');
		btn.textContent = 'Delete Selected (' + checked.length + ')';
	} else {
		btn.classList.add('hidden');
	}
}

function bulkDeleteSnapshots() {
	const checked = document.querySelectorAll('.snapshot-checkbox:checked');
	if (checked.length === 0) return;

	const ids = Array.from(checked).map(cb => cb.dataset.snapshotId);
	showConfirmModal({
		title: 'Delete Snapshots',
		message: 'Delete ' + ids.length + ' selected snapshot(s)? This action cannot be undone.',
		type: 'danger',
		confirmText: 'Delete All',
		onConfirm: async () => {
			let deleted = 0;
			let failed = 0;
			for (const id of ids) {
				try {
					const response = await fetch('/api/os-updates/snapshots/' + id + '?server=' + currentServerID, {
						method: 'DELETE'
					});
					if (!response.ok) {
						failed++;
						continue;
					}
					deleted++;
					const row = document.querySelector('[data-snapshot-row="' + id + '"]');
					if (row) row.remove();
				} catch {
					failed++;
				}
			}
			if (failed > 0) {
				showToast('Deleted ' + deleted + ', failed ' + failed, 'warning');
			} else {
				showToast(deleted + ' snapshot(s) deleted', 'success');
			}
			updateBulkDeleteButton();
			if (document.querySelectorAll('[data-snapshot-row]').length === 0) {
				setTimeout(() => window.location.reload(), 1000);
			}
		}
	});
}

function scheduleReboot() {
	document.getElementById('reboot-modal').classList.remove('hidden');
}

function hideRebootModal() {
	document.getElementById('reboot-modal').classList.add('hidden');
}

function rebootNow() {
	showConfirmModal({
		title: 'Reboot Now',
		message: 'Reboot the system immediately? All active connections will be terminated.',
		type: 'danger',
		confirmText: 'Reboot Now',
		onConfirm: () => doReboot('now')
	});
}

async function confirmReboot() {
	const selected = document.querySelector('input[name="reboot-time"]:checked');
	if (!selected) {
		showToast('Please select a reboot time', 'warning');
		return;
	}

	let when = selected.value;
	if (when === 'custom') {
		when = document.getElementById('custom-reboot-time').value;
		if (!when) {
			showToast('Please enter a custom time', 'warning');
			return;
		}
	}

	hideRebootModal();
	await doReboot(when);
}

async function doReboot(when) {
	try {
		const response = await fetch('/api/os-updates/reboot?server=' + currentServerID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ when: when })
		});
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}
		const data = await response.json();

		if (when === 'now') {
			isRebooting = true;
			setRebootState(currentServerID);
			showRebootingStatus();
			startRebootPolling();
			showToast('System is rebooting...', 'info');
		} else {
			showToast(data.message || 'Reboot scheduled', 'success');
		}
	} catch (err) {
		showToast('Failed to schedule reboot: ' + err.message, 'error');
	}
}

// Update the reboot status card to show rebooting state
function showRebootingStatus() {
	const icon = document.getElementById('reboot-status-icon');
	const status = document.getElementById('reboot-status');
	const subtitle = document.getElementById('reboot-status-subtitle');
	const actions = document.getElementById('reboot-actions');

	if (icon) {
		icon.innerHTML = '<svg class="w-5 h-5 text-blue-500 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>';
	}
	if (status) {
		status.className = 'text-2xl font-bold mt-2 text-blue-600 dark:text-blue-400';
		status.textContent = 'Rebooting...';
	}
	if (subtitle) {
		subtitle.textContent = 'Waiting for server to come back online';
	}
	if (actions) {
		actions.classList.add('hidden');
	}
}

// Update the reboot status card to show not required state
function showRebootNotRequiredStatus() {
	const icon = document.getElementById('reboot-status-icon');
	const status = document.getElementById('reboot-status');
	const subtitle = document.getElementById('reboot-status-subtitle');
	const actions = document.getElementById('reboot-actions');

	if (icon) {
		icon.innerHTML = '<svg class="w-5 h-5 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>';
	}
	if (status) {
		status.className = 'text-2xl font-bold mt-2 text-green-600 dark:text-green-400';
		status.textContent = 'Not Required';
	}
	if (subtitle) {
		subtitle.textContent = 'System reboot status';
	}
	if (actions) {
		actions.classList.add('hidden');
	}
}

// Async version check — fetches latest PyPI versions and updates the Latest cells
async function loadPythonVersions() {
	try {
		const response = await fetch('/api/os-updates/python-tools/versions?server=' + currentServerID);
		if (!response.ok) return;
		const data = await response.json();

		// Update pipx version cells
		document.querySelectorAll('.pipx-version-cell').forEach(cell => {
			const pkg = data.pipx?.packages?.find(p => p.name === cell.dataset.package);
			renderVersionCell(cell, pkg);
		});

		// Update pip version cells
		document.querySelectorAll('.pip-version-cell').forEach(cell => {
			const pkg = data.pip?.packages?.find(p => p.name === cell.dataset.package);
			renderVersionCell(cell, pkg);
		});
	} catch {
		// Non-fatal: leave spinners as-is on network error
	}
}

function renderVersionCell(cell, pkg) {
	if (!pkg) {
		cell.innerHTML = '<span class="text-gray-400 dark:text-gray-500">—</span>';
		disableUpgradeBtn(cell);
		return;
	}
	if (pkg.update_available) {
		cell.innerHTML =
			'<span class="text-amber-600 dark:text-amber-400">' + pkg.latest_version + '</span>' +
			'<span class="ml-1 px-1.5 py-0.5 text-xs bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-400 rounded">Update</span>';
	} else if (pkg.latest_version) {
		cell.innerHTML = '<span class="text-gray-500 dark:text-gray-400">' + pkg.latest_version + '</span>';
		disableUpgradeBtn(cell);
	} else {
		cell.innerHTML = '<span class="text-gray-400 dark:text-gray-500">—</span>';
		disableUpgradeBtn(cell);
	}
}

// Disables the upgrade button in the same table row as the given version cell.
function disableUpgradeBtn(cell) {
	const row = cell.closest('tr');
	if (!row) return;
	const btn = row.querySelector('.pipx-upgrade-btn, .pip-upgrade-btn');
	if (btn) btn.disabled = true;
}

// Button loading state helpers
const SPINNER_SVG = '<svg class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>';

function setButtonLoading(btn, loadingText) {
	btn.disabled = true;
	btn.dataset.originalHtml = btn.innerHTML;
	btn.innerHTML = SPINNER_SVG + '<span>' + loadingText + '</span>';
}

function clearButtonLoading(btn) {
	btn.disabled = false;
	if (btn.dataset.originalHtml) {
		btn.innerHTML = btn.dataset.originalHtml;
		delete btn.dataset.originalHtml;
	}
}

// ── Multi-select: pipx ────────────────────────────────────────────────────────

function onPipxCheckboxChange() {
	const checked = document.querySelectorAll('.pipx-checkbox:checked');
	const all = document.querySelectorAll('.pipx-checkbox');
	const bar = document.getElementById('pipx-bulk-bar');
	const count = document.getElementById('pipx-selected-count');
	const selectAll = document.getElementById('pipx-select-all');
	if (bar) bar.classList.toggle('hidden', checked.length === 0);
	if (count) count.textContent = checked.length + ' selected';
	if (selectAll) selectAll.indeterminate = checked.length > 0 && checked.length < all.length;
	if (selectAll) selectAll.checked = checked.length === all.length && all.length > 0;
}

function togglePipxSelectAll(cb) {
	document.querySelectorAll('.pipx-checkbox').forEach(c => { c.checked = cb.checked; });
	onPipxCheckboxChange();
}

function clearPipxSelection() {
	document.querySelectorAll('.pipx-checkbox').forEach(c => { c.checked = false; });
	const selectAll = document.getElementById('pipx-select-all');
	if (selectAll) { selectAll.checked = false; selectAll.indeterminate = false; }
	onPipxCheckboxChange();
}

async function bulkUpgradePipx() {
	const names = [...document.querySelectorAll('.pipx-checkbox:checked')].map(c => c.value);
	if (names.length === 0) return;
	const btn = document.getElementById('pipx-bulk-upgrade-btn');
	setButtonLoading(btn, 'Upgrading...');
	let failed = [];
	for (const name of names) {
		try {
			const r = await fetch('/api/os-updates/pipx/upgrade?server=' + currentServerID, {
				method: 'POST', headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ name })
			});
			if (!r.ok) failed.push(name);
		} catch { failed.push(name); }
	}
	if (failed.length > 0) {
		showToast('Failed to upgrade: ' + failed.join(', '), 'error');
		clearButtonLoading(btn);
	} else {
		showToast('Upgraded ' + names.length + ' package(s)', 'success');
		setTimeout(() => window.location.reload(), 1000);
	}
}

function bulkUninstallPipx() {
	const names = [...document.querySelectorAll('.pipx-checkbox:checked')].map(c => c.value);
	if (names.length === 0) return;
	showConfirmModal({
		title: 'Uninstall Packages',
		message: 'Uninstall ' + names.length + ' selected package(s)? This cannot be undone.',
		type: 'danger',
		confirmText: 'Uninstall',
		onConfirm: async () => {
			let failed = [];
			for (const name of names) {
				try {
					const r = await fetch('/api/os-updates/pipx/uninstall?server=' + currentServerID, {
						method: 'POST', headers: { 'Content-Type': 'application/json' },
						body: JSON.stringify({ name })
					});
					if (!r.ok) failed.push(name);
				} catch { failed.push(name); }
			}
			if (failed.length > 0) {
				showToast('Failed to uninstall: ' + failed.join(', '), 'error');
			} else {
				showToast('Uninstalled ' + names.length + ' package(s)', 'success');
				setTimeout(() => window.location.reload(), 1000);
			}
		}
	});
}

// ── Multi-select: pip ─────────────────────────────────────────────────────────

function onPipCheckboxChange() {
	const checked = document.querySelectorAll('.pip-checkbox:checked');
	const all = document.querySelectorAll('.pip-checkbox');
	const bar = document.getElementById('pip-bulk-bar');
	const count = document.getElementById('pip-selected-count');
	const selectAll = document.getElementById('pip-select-all');
	if (bar) bar.classList.toggle('hidden', checked.length === 0);
	if (count) count.textContent = checked.length + ' selected';
	if (selectAll) selectAll.indeterminate = checked.length > 0 && checked.length < all.length;
	if (selectAll) selectAll.checked = checked.length === all.length && all.length > 0;
}

function togglePipSelectAll(cb) {
	document.querySelectorAll('.pip-checkbox').forEach(c => { c.checked = cb.checked; });
	onPipCheckboxChange();
}

function clearPipSelection() {
	document.querySelectorAll('.pip-checkbox').forEach(c => { c.checked = false; });
	const selectAll = document.getElementById('pip-select-all');
	if (selectAll) { selectAll.checked = false; selectAll.indeterminate = false; }
	onPipCheckboxChange();
}

async function bulkUpgradePip() {
	const names = [...document.querySelectorAll('.pip-checkbox:checked')].map(c => c.value);
	if (names.length === 0) return;
	const btn = document.getElementById('pip-bulk-upgrade-btn');
	setButtonLoading(btn, 'Upgrading...');
	let failed = [];
	for (const name of names) {
		try {
			const r = await fetch('/api/os-updates/pip/upgrade?server=' + currentServerID, {
				method: 'POST', headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ name })
			});
			if (!r.ok) failed.push(name);
		} catch { failed.push(name); }
	}
	if (failed.length > 0) {
		showToast('Failed to upgrade: ' + failed.join(', '), 'error');
		clearButtonLoading(btn);
	} else {
		showToast('Upgraded ' + names.length + ' package(s)', 'success');
		setTimeout(() => window.location.reload(), 1000);
	}
}

function bulkUninstallPip() {
	const names = [...document.querySelectorAll('.pip-checkbox:checked')].map(c => c.value);
	if (names.length === 0) return;
	showConfirmModal({
		title: 'Uninstall Packages',
		message: 'Uninstall ' + names.length + ' selected package(s)? This cannot be undone.',
		type: 'danger',
		confirmText: 'Uninstall',
		onConfirm: async () => {
			let failed = [];
			for (const name of names) {
				try {
					const r = await fetch('/api/os-updates/pip/uninstall?server=' + currentServerID, {
						method: 'POST', headers: { 'Content-Type': 'application/json' },
						body: JSON.stringify({ name })
					});
					if (!r.ok) failed.push(name);
				} catch { failed.push(name); }
			}
			if (failed.length > 0) {
				showToast('Failed to uninstall: ' + failed.join(', '), 'error');
			} else {
				showToast('Uninstalled ' + names.length + ' package(s)', 'success');
				setTimeout(() => window.location.reload(), 1000);
			}
		}
	});
}

// ── PyPI live lookup ──────────────────────────────────────────────────────────

let pypiSearchTimer = null;

async function lookupPyPIPackage(name, prefix) {
	const spinner = document.getElementById(prefix + '-search-spinner');
	const infoBox = document.getElementById(prefix + '-package-info');
	const notFound = document.getElementById(prefix + '-package-not-found');

	if (!name || name.length < 2) {
		if (infoBox) infoBox.classList.add('hidden');
		if (notFound) notFound.classList.add('hidden');
		return;
	}

	if (spinner) spinner.classList.remove('hidden');
	if (infoBox) infoBox.classList.add('hidden');
	if (notFound) notFound.classList.add('hidden');

	try {
		const resp = await fetch('/api/os-updates/pypi-lookup?name=' + encodeURIComponent(name));
		if (spinner) spinner.classList.add('hidden');
		if (resp.status === 404) {
			if (notFound) notFound.classList.remove('hidden');
			return;
		}
		if (!resp.ok) return;
		const pkg = await resp.json();
		if (infoBox) {
			document.getElementById(prefix + '-pkg-name').textContent = pkg.name;
			document.getElementById(prefix + '-pkg-version').textContent = pkg.version;
			document.getElementById(prefix + '-pkg-summary').textContent = pkg.summary || '';
			document.getElementById(prefix + '-pkg-link').href = pkg.project_url;
			infoBox.classList.remove('hidden');
		}
	} catch {
		if (spinner) spinner.classList.add('hidden');
	}
}

function onPipxSearchInput(value) {
	clearTimeout(pypiSearchTimer);
	const trimmed = value.trim();
	const infoBox = document.getElementById('pipx-package-info');
	const notFound = document.getElementById('pipx-package-not-found');
	if (!trimmed) {
		if (infoBox) infoBox.classList.add('hidden');
		if (notFound) notFound.classList.add('hidden');
		return;
	}
	pypiSearchTimer = setTimeout(() => lookupPyPIPackage(trimmed, 'pipx'), 350);
}

function onPipSearchInput(value) {
	clearTimeout(pypiSearchTimer);
	const trimmed = value.trim();
	const infoBox = document.getElementById('pip-package-info');
	const notFound = document.getElementById('pip-package-not-found');
	if (!trimmed) {
		if (infoBox) infoBox.classList.add('hidden');
		if (notFound) notFound.classList.add('hidden');
		return;
	}
	pypiSearchTimer = setTimeout(() => lookupPyPIPackage(trimmed, 'pip'), 350);
}

// ── Pipx functions ────────────────────────────────────────────────────────────

function showPipxInstallModal() {
	document.getElementById('pipx-install-modal').classList.remove('hidden');
	document.getElementById('pipx-package-name').focus();
}

function hidePipxInstallModal() {
	document.getElementById('pipx-install-modal').classList.add('hidden');
	document.getElementById('pipx-package-name').value = '';
	document.getElementById('pipx-package-info').classList.add('hidden');
	document.getElementById('pipx-package-not-found').classList.add('hidden');
}

async function confirmPipxInstall() {
	const name = document.getElementById('pipx-package-name').value.trim();
	if (!name) {
		showToast('Please enter a package name', 'warning');
		return;
	}

	hidePipxInstallModal();

	try {
		const response = await fetch('/api/os-updates/pipx/install?server=' + currentServerID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name: name })
		});
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}
		showToast('Package ' + name + ' installed', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		showToast('Failed to install pipx package: ' + err.message, 'error');
	}
}

async function upgradePipxPackage(btn, name) {
	setButtonLoading(btn, 'Upgrading...');
	try {
		const response = await fetch('/api/os-updates/pipx/upgrade?server=' + currentServerID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name: name })
		});
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}
		showToast('Package ' + name + ' upgraded', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		clearButtonLoading(btn);
		showToast('Failed to upgrade pipx package: ' + err.message, 'error');
	}
}

async function upgradeAllPipx(btn) {
	setButtonLoading(btn, 'Upgrading...');
	try {
		const response = await fetch('/api/os-updates/pipx/upgrade?server=' + currentServerID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({})
		});
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}
		showToast('All pipx packages upgraded', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		clearButtonLoading(btn);
		showToast('Failed to upgrade pipx packages: ' + err.message, 'error');
	}
}

function uninstallPipxPackage(btn, name) {
	showConfirmModal({
		title: 'Uninstall Package',
		message: 'Uninstall ' + name + '? This will remove the package and its virtual environment.',
		type: 'danger',
		confirmText: 'Uninstall',
		onConfirm: async () => {
			setButtonLoading(btn, 'Uninstalling...');
			try {
				const response = await fetch('/api/os-updates/pipx/uninstall?server=' + currentServerID, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ name: name })
				});
				if (!response.ok) {
					const errMsg = await extractErrorMessage(response);
					throw new Error(errMsg);
				}
				showToast('Package ' + name + ' uninstalled', 'success');
				setTimeout(() => window.location.reload(), 1000);
			} catch (err) {
				clearButtonLoading(btn);
				showToast('Failed to uninstall pipx package: ' + err.message, 'error');
			}
		}
	});
}

// ── Pip functions ─────────────────────────────────────────────────────────────

function showPipInstallModal() {
	document.getElementById('pip-install-modal').classList.remove('hidden');
	document.getElementById('pip-package-name').focus();
}

function hidePipInstallModal() {
	document.getElementById('pip-install-modal').classList.add('hidden');
	document.getElementById('pip-package-name').value = '';
	document.getElementById('pip-package-info').classList.add('hidden');
	document.getElementById('pip-package-not-found').classList.add('hidden');
}

async function confirmPipInstall() {
	const name = document.getElementById('pip-package-name').value.trim();
	if (!name) {
		showToast('Please enter a package name', 'warning');
		return;
	}

	hidePipInstallModal();

	try {
		const response = await fetch('/api/os-updates/pip/install?server=' + currentServerID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name: name })
		});
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}
		showToast('Package ' + name + ' installed', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		showToast('Failed to install pip package: ' + err.message, 'error');
	}
}

async function upgradePipPackage(btn, name) {
	setButtonLoading(btn, 'Upgrading...');
	try {
		const response = await fetch('/api/os-updates/pip/upgrade?server=' + currentServerID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name: name })
		});
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}
		showToast('Package ' + name + ' upgraded', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		clearButtonLoading(btn);
		showToast('Failed to upgrade pip package: ' + err.message, 'error');
	}
}

async function upgradeAllPip(btn) {
	setButtonLoading(btn, 'Upgrading...');
	try {
		const response = await fetch('/api/os-updates/pip/upgrade?server=' + currentServerID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({})
		});
		if (!response.ok) {
			const errMsg = await extractErrorMessage(response);
			throw new Error(errMsg);
		}
		showToast('All pip packages upgraded', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		clearButtonLoading(btn);
		showToast('Failed to upgrade pip packages: ' + err.message, 'error');
	}
}

function uninstallPipPackage(btn, name) {
	showConfirmModal({
		title: 'Uninstall Package',
		message: 'Uninstall ' + name + '? This will remove the pip package.',
		type: 'danger',
		confirmText: 'Uninstall',
		onConfirm: async () => {
			setButtonLoading(btn, 'Uninstalling...');
			try {
				const response = await fetch('/api/os-updates/pip/uninstall?server=' + currentServerID, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ name: name })
				});
				if (!response.ok) {
					const errMsg = await extractErrorMessage(response);
					throw new Error(errMsg);
				}
				showToast('Package ' + name + ' uninstalled', 'success');
				setTimeout(() => window.location.reload(), 1000);
			} catch (err) {
				clearButtonLoading(btn);
				showToast('Failed to uninstall pip package: ' + err.message, 'error');
			}
		}
	});
}

// Package info modal functions
function showPackageInfo(btn) {
	const modal = document.getElementById('package-info-modal');
	const title = document.getElementById('package-info-title');
	const currentVersion = document.getElementById('pkg-current-version');
	const availableVersion = document.getElementById('pkg-available-version');
	const architecture = document.getElementById('pkg-architecture');
	const size = document.getElementById('pkg-size');
	const repository = document.getElementById('pkg-repository');
	const typeEl = document.getElementById('pkg-type');
	const changelogSection = document.getElementById('pkg-changelog-section');
	const changelogLink = document.getElementById('pkg-changelog-link');

	// Populate modal with package data from button dataset
	title.textContent = btn.dataset.packageName;
	currentVersion.textContent = btn.dataset.packageCurrent || '-';
	availableVersion.textContent = btn.dataset.packageAvailable || '-';
	architecture.textContent = btn.dataset.packageArch || '-';
	size.textContent = btn.dataset.packageSize || '-';
	repository.textContent = btn.dataset.packageRepo || '-';

	// Update type with appropriate styling
	const isSecurity = btn.dataset.packageSecurity === 'true';
	if (isSecurity) {
		typeEl.innerHTML = '<span class="px-2 py-1 text-xs bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400 rounded">Security Update</span>';
	} else {
		typeEl.innerHTML = '<span class="px-2 py-1 text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 rounded">Standard Update</span>';
	}

	// Set changelog link
	const changelogUrl = btn.dataset.changelogUrl;
	if (changelogUrl) {
		changelogSection.classList.remove('hidden');
		changelogLink.href = changelogUrl;
	} else {
		changelogSection.classList.add('hidden');
	}

	modal.classList.remove('hidden');
}

function hidePackageInfoModal() {
	document.getElementById('package-info-modal').classList.add('hidden');
}

// Confirmation modal functions
let confirmCallback = null;

function showConfirmModal(options) {
	const modal = document.getElementById('confirm-modal');
	const icon = document.getElementById('confirm-icon');
	const title = document.getElementById('confirm-title');
	const message = document.getElementById('confirm-message');
	const actionBtn = document.getElementById('confirm-action-btn');
	const input = document.getElementById('confirm-input');

	title.textContent = options.title || 'Confirm';
	message.textContent = options.message || 'Are you sure?';
	actionBtn.textContent = options.confirmText || 'Confirm';

	// Handle optional input field
	const counter = document.getElementById('confirm-input-counter');
	if (input) {
		if (options.showInput) {
			input.classList.remove('hidden');
			input.value = '';
			input.placeholder = options.inputPlaceholder || '';
			const maxLen = options.inputMaxLength || 0;
			if (maxLen > 0) {
				input.maxLength = maxLen;
				if (counter) {
					counter.classList.add('hidden');
					input.oninput = function() {
						const len = input.value.length;
						const threshold = Math.floor(maxLen * 0.8);
						if (len >= threshold) {
							counter.textContent = len + ' of ' + maxLen;
							counter.classList.remove('hidden');
							if (len >= maxLen) {
								counter.classList.remove('text-gray-400', 'dark:text-gray-500');
								counter.classList.add('text-red-500', 'dark:text-red-400');
							} else {
								counter.classList.remove('text-red-500', 'dark:text-red-400');
								counter.classList.add('text-gray-400', 'dark:text-gray-500');
							}
						} else {
							counter.classList.add('hidden');
						}
					};
				}
			} else {
				input.removeAttribute('maxLength');
				input.oninput = null;
				if (counter) counter.classList.add('hidden');
			}
		} else {
			input.classList.add('hidden');
			input.value = '';
			input.removeAttribute('maxLength');
			input.oninput = null;
			if (counter) counter.classList.add('hidden');
		}
	}

	// Set icon and button style based on type
	const type = options.type || 'warning';
	icon.className = 'flex-shrink-0 w-10 h-10 rounded-full flex items-center justify-center';
	actionBtn.className = 'px-4 py-2 rounded-lg transition-colors text-white';

	if (type === 'danger') {
		icon.classList.add('bg-red-100', 'dark:bg-red-900/30');
		icon.innerHTML = '<svg class="w-6 h-6 text-red-600 dark:text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path></svg>';
		actionBtn.classList.add('bg-red-600', 'hover:bg-red-700');
	} else if (type === 'warning') {
		icon.classList.add('bg-yellow-100', 'dark:bg-yellow-900/30');
		icon.innerHTML = '<svg class="w-6 h-6 text-yellow-600 dark:text-yellow-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path></svg>';
		actionBtn.classList.add('bg-yellow-600', 'hover:bg-yellow-700');
	} else {
		icon.classList.add('bg-blue-100', 'dark:bg-blue-900/30');
		icon.innerHTML = '<svg class="w-6 h-6 text-blue-600 dark:text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>';
		actionBtn.classList.add('bg-blue-600', 'hover:bg-blue-700');
	}

	confirmCallback = options.onConfirm;
	actionBtn.onclick = () => {
		const cb = confirmCallback;
		// Read input value before hiding (hideConfirmModal clears it)
		const inputVal = input ? input.value : '';
		hideConfirmModal();
		if (cb) cb(inputVal);
	};

	modal.classList.remove('hidden');
}

function hideConfirmModal() {
	const modal = document.getElementById('confirm-modal');
	const input = document.getElementById('confirm-input');
	const counter = document.getElementById('confirm-input-counter');
	if (modal) modal.classList.add('hidden');
	if (input) {
		input.classList.add('hidden');
		input.value = '';
		input.removeAttribute('maxLength');
		input.oninput = null;
	}
	if (counter) counter.classList.add('hidden');
	confirmCallback = null;
}

// Toast helper — delegates to the global toast system (static/js/utils/toast.js).
// All calls in this file use window.showToast which supports:
//   showToast(message, type)
//   showToast(message, type, durationMs)
//   showToast(message, type, { onClose, duration, persistent, buttons })
function showToast(message, type, options) {
	if (window.showToast) {
		return window.showToast(message, type, options);
	}
}

// Terminal modal functions
function openTerminalModal(operationId, title, allPackages = true) {
	currentOperationID = operationId;
	terminalLineCount = 0;
	operationReceivedSSE = false;

	const modal = document.getElementById('terminal-modal');
	const titleEl = document.getElementById('terminal-title');
	const content = document.getElementById('terminal-content');
	const lineCount = document.getElementById('terminal-line-count');
	const statusIndicator = document.getElementById('terminal-status-indicator');
	const statusText = document.getElementById('terminal-status-text');

	titleEl.textContent = title || 'Terminal Output';
	content.textContent = '';
	lineCount.textContent = '0 lines';
	statusIndicator.className = 'w-3 h-3 rounded-full bg-yellow-500 animate-pulse';
	statusText.textContent = 'Running...';

	modal.classList.remove('hidden');

	// Start progress tracking for package grid
	startInstallProgress(allPackages);

	// Register event handlers for apt events
	if (window.registerEventHandler) {
		window.registerEventHandler('apt.output', handleAptOutput);
		window.registerEventHandler('apt.completed', handleAptCompleted);
		window.registerEventHandler('apt.failed', handleAptFailed);
	}

	appendTerminalLine('Starting operation ' + operationId + '...\n', 'info');

	// Start polling fallback — catches cases where SSE events are missed
	// (e.g., race condition, fast operations, SSE reconnection gaps)
	startOperationPolling(operationId);
}

function closeTerminalModal() {
	const modal = document.getElementById('terminal-modal');
	modal.classList.add('hidden');

	// Stop polling
	stopOperationPolling();

	// Unregister event handlers
	if (window.unregisterEventHandler) {
		window.unregisterEventHandler('apt.output', handleAptOutput);
		window.unregisterEventHandler('apt.completed', handleAptCompleted);
		window.unregisterEventHandler('apt.failed', handleAptFailed);
	}

	// If operation completed, refresh the page
	const statusText = document.getElementById('terminal-status-text');
	if (statusText && (statusText.textContent === 'Completed' || statusText.textContent === 'Failed')) {
		setTimeout(() => window.location.reload(), 500);
	}
}

function appendTerminalLine(line, type) {
	const content = document.getElementById('terminal-content');
	const output = document.getElementById('terminal-output');
	const lineCount = document.getElementById('terminal-line-count');
	const autoScroll = document.getElementById('terminal-auto-scroll');

	if (!content) return;

	// Apply syntax coloring based on content
	let colorClass = '';
	if (type === 'stderr' || line.includes('ERROR') || line.includes('E:')) {
		colorClass = 'text-red-400';
	} else if (line.includes('WARNING') || line.includes('W:')) {
		colorClass = 'text-yellow-400';
	} else if (line.includes('Setting up') || line.includes('Unpacking') || line.includes('Processing')) {
		colorClass = 'text-cyan-400';
	} else if (line.includes('upgraded') || line.includes('installed') || line.includes('success')) {
		colorClass = 'text-green-400';
	} else if (type === 'info') {
		colorClass = 'text-blue-400';
	}

	const lineEl = document.createElement('span');
	if (colorClass) lineEl.className = colorClass;
	lineEl.textContent = line + '\n';
	content.appendChild(lineEl);

	terminalLineCount++;
	lineCount.textContent = terminalLineCount + ' lines';

	// Auto-scroll to bottom if enabled
	if (autoScroll && autoScroll.checked) {
		output.scrollTop = output.scrollHeight;
	}
}

function setTerminalStatus(status) {
	const statusIndicator = document.getElementById('terminal-status-indicator');
	const statusText = document.getElementById('terminal-status-text');

	if (status === 'completed') {
		statusIndicator.className = 'w-3 h-3 rounded-full bg-green-500';
		statusText.textContent = 'Completed';
	} else if (status === 'failed') {
		statusIndicator.className = 'w-3 h-3 rounded-full bg-red-500';
		statusText.textContent = 'Failed';
	} else {
		statusIndicator.className = 'w-3 h-3 rounded-full bg-yellow-500 animate-pulse';
		statusText.textContent = 'Running...';
	}
}

function handleAptOutput(event) {
	if (!event.data) return;
	const opId = event.data.operation_id;
	if (opId !== currentOperationID) return;

	operationReceivedSSE = true;
	const line = event.data.line || '';
	const stream = event.data.stream || 'stdout';
	appendTerminalLine(line, stream);
}

function handleAptCompleted(event) {
	if (!event.data) return;
	const opId = event.data.operation_id;
	if (opId !== currentOperationID) return;

	operationReceivedSSE = true;
	stopOperationPolling();
	setTerminalStatus('completed');
	appendTerminalLine('\nOperation completed successfully.', 'info');
}

function handleAptFailed(event) {
	if (!event.data) return;
	const opId = event.data.operation_id;
	if (opId !== currentOperationID) return;

	operationReceivedSSE = true;
	stopOperationPolling();
	setTerminalStatus('failed');
	const errorMsg = event.data.error || 'Unknown error';
	appendTerminalLine('\nOperation failed: ' + errorMsg, 'stderr');
}

function startOperationPolling(operationId) {
	stopOperationPolling();
	operationPollInterval = setInterval(async () => {
		try {
			const response = await fetch('/api/os-updates/operation/' + operationId + '?server=' + currentServerID);
			if (!response.ok) return;
			const data = await response.json();

			if (data.status === 'completed' || data.status === 'failed') {
				stopOperationPolling();
				// Only update terminal if SSE didn't already handle it
				const statusText = document.getElementById('terminal-status-text');
				if (statusText && statusText.textContent === 'Running...') {
					if (data.status === 'completed') {
						setTerminalStatus('completed');
						appendTerminalLine('\nOperation completed successfully.', 'info');
					} else {
						setTerminalStatus('failed');
						appendTerminalLine('\nOperation failed: ' + (data.error || 'Unknown error'), 'stderr');
					}
				}
			}
		} catch (err) {
			// Polling errors are non-fatal — SSE is the primary channel
		}
	}, 3000);
}

function stopOperationPolling() {
	if (operationPollInterval) {
		clearInterval(operationPollInterval);
		operationPollInterval = null;
	}
}

function copyTerminalOutput() {
	const content = document.getElementById('terminal-content');
	if (!content) return;

	const text = content.textContent;
	navigator.clipboard.writeText(text).then(() => {
		showToast('Output copied to clipboard', 'success');
	}).catch(err => {
		showToast('Failed to copy: ' + err.message, 'error');
	});
}

// ============ Progress Tracking Functions ============

// Initialize progress tracking when install starts
function startInstallProgress(allPackages) {
	installingPackages.clear();
	packagesInstalled = 0;
	installedSize = 0;

	// Calculate total packages and size
	const rows = document.querySelectorAll('.package-row');
	totalPackagesToInstall = 0;
	totalInstallSize = 0;

	rows.forEach(row => {
		const pkgName = row.dataset.packageName;
		const pkgSize = parseInt(row.dataset.packageSize) || 0;

		// If allPackages is true, include all; otherwise check if in selectedPackages
		if (allPackages || selectedPackages.has(pkgName)) {
			installingPackages.add(pkgName);
			totalPackagesToInstall++;
			totalInstallSize += pkgSize;
		}
	});

	// Grey out all packages being installed
	greyOutInstallingPackages();

	// Show progress bar
	const progressContainer = document.getElementById('install-progress-container');
	const progressBar = document.getElementById('install-progress-bar');
	if (progressContainer && progressBar) {
		progressContainer.classList.remove('hidden');
		progressBar.style.width = '0%';
	}
}

// Grey out packages being installed
function greyOutInstallingPackages() {
	const rows = document.querySelectorAll('.package-row');
	rows.forEach(row => {
		const pkgName = row.dataset.packageName;
		if (installingPackages.has(pkgName)) {
			row.classList.add('opacity-50', 'pointer-events-none');
			// Disable checkbox
			const checkbox = row.querySelector('.package-checkbox');
			if (checkbox) checkbox.disabled = true;
		}
	});
}

// Set spinner on current package
function setCurrentPackageSpinner(packageName) {
	const rows = document.querySelectorAll('.package-row');
	rows.forEach(row => {
		const pkgName = row.dataset.packageName;
		const statusCell = row.querySelector('.package-status-cell');
		if (!statusCell) return;

		if (pkgName === packageName) {
			// Show spinner
			statusCell.innerHTML = '<svg class="w-4 h-4 animate-spin text-blue-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>';
		} else if (installingPackages.has(pkgName) && !statusCell.innerHTML.includes('M9 12l2 2 4-4')) {
			// Clear spinner for other installing packages (but not if already completed)
			statusCell.innerHTML = '';
		}
	});
}

// Mark package as completed
function markPackageCompleted(packageName) {
	const rows = document.querySelectorAll('.package-row');
	rows.forEach(row => {
		const pkgName = row.dataset.packageName;
		if (pkgName === packageName) {
			const statusCell = row.querySelector('.package-status-cell');
			if (statusCell) {
				// Show checkmark
				statusCell.innerHTML = '<svg class="w-4 h-4 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>';
			}

			// Update progress
			const pkgSize = parseInt(row.dataset.packageSize) || 0;
			installedSize += pkgSize;
			packagesInstalled++;
			updateProgressBar();
		}
	});
}

// Update progress bar based on installed packages
function updateProgressBar() {
	const progressBar = document.getElementById('install-progress-bar');
	if (!progressBar) return;

	// Calculate progress: weighted 70% by package count, 30% by size
	let progress = 0;
	if (totalPackagesToInstall > 0) {
		const countProgress = packagesInstalled / totalPackagesToInstall;
		const sizeProgress = totalInstallSize > 0 ? installedSize / totalInstallSize : countProgress;
		progress = (countProgress * 0.7 + sizeProgress * 0.3) * 100;
	}

	// Clamp between 0 and 100
	progress = Math.max(0, Math.min(100, progress));
	progressBar.style.width = progress + '%';
}

// Reset install progress (e.g., on completion or page refresh)
function resetInstallProgress() {
	const progressContainer = document.getElementById('install-progress-container');
	const progressBar = document.getElementById('install-progress-bar');
	if (progressContainer) progressContainer.classList.add('hidden');
	if (progressBar) progressBar.style.width = '0%';

	// Clear visual states on rows
	const rows = document.querySelectorAll('.package-row');
	rows.forEach(row => {
		row.classList.remove('opacity-50', 'pointer-events-none');
		const checkbox = row.querySelector('.package-checkbox');
		if (checkbox) checkbox.disabled = false;
		const statusCell = row.querySelector('.package-status-cell');
		if (statusCell) statusCell.innerHTML = '';
	});

	installingPackages.clear();
	packagesInstalled = 0;
	installedSize = 0;
	totalInstallSize = 0;
	totalPackagesToInstall = 0;
}

// Parse apt output to detect current package being installed
function parseAptOutputForProgress(line) {
	// Match "Setting up <package>" or "Unpacking <package>"
	let match = line.match(/(?:Setting up|Unpacking) ([^\s:]+)/);
	if (match) {
		const packageName = match[1];
		setCurrentPackageSpinner(packageName);
		return;
	}

	// Match "Processing triggers for <package>"
	match = line.match(/Processing triggers for ([^\s:]+)/);
	if (match) {
		// Usually means previous package completed
		return;
	}

	// Match completion patterns
	// e.g., "Setting up libfoo:amd64 (1.2.3) ..." followed by next line
	// When we see "Setting up X", mark previous as complete
}

// Extended apt output handler that also tracks progress
const originalHandleAptOutput = handleAptOutput;
handleAptOutput = function(event) {
	// Call original handler for terminal output
	if (originalHandleAptOutput) {
		originalHandleAptOutput.call(this, event);
	}

	// Parse for progress tracking
	if (event.data && event.data.line) {
		const line = event.data.line;

		// Track package installation
		const settingUpMatch = line.match(/Setting up ([^\s:]+)/);
		if (settingUpMatch) {
			const pkgName = settingUpMatch[1];
			// Mark as currently being setup
			setCurrentPackageSpinner(pkgName);
		}

		// Completion is hard to detect line-by-line, so we estimate
		// When we see "Setting up X", mark the previous package complete
		const unpacking = line.match(/Unpacking ([^\s:]+)/);
		if (unpacking) {
			const pkgName = unpacking[1];
			setCurrentPackageSpinner(pkgName);
		}
	}
};

// Extended apt completed handler that updates progress
const originalHandleAptCompleted = handleAptCompleted;
handleAptCompleted = function(event) {
	// Mark all installing packages as complete
	installingPackages.forEach(pkgName => {
		markPackageCompleted(pkgName);
	});

	// Set progress to 100%
	const progressBar = document.getElementById('install-progress-bar');
	if (progressBar) {
		progressBar.style.width = '100%';
	}

	// Call original handler
	if (originalHandleAptCompleted) {
		originalHandleAptCompleted.call(this, event);
	}
};

// ============================================================================
// Update Logs
// ============================================================================

async function loadUpdateLogs() {
	const table = document.getElementById('update-logs-table');
	if (!table) return;

	table.innerHTML = '<tr><td colspan="5" class="px-4 py-8 text-center text-gray-500 dark:text-gray-400">Loading logs...</td></tr>';

	try {
		const response = await fetch(`/api/os-updates/logs?server=${currentServerID}&limit=50`);
		if (!response.ok) {
			throw new Error(await extractErrorMessage(response));
		}

		const data = await response.json();

		if (!data.logs || data.logs.length === 0) {
			table.innerHTML = '<tr><td colspan="5" class="px-4 py-8 text-center text-gray-500 dark:text-gray-400">No update logs available</td></tr>';
			return;
		}

		table.innerHTML = '';
		data.logs.forEach(log => {
			const row = document.createElement('tr');
			row.className = 'hover:bg-gray-50 dark:hover:bg-slate-700';

			const date = new Date(log.started_at).toLocaleString();
			const statusColor = log.status === 'completed'
				? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400'
				: log.status === 'failed'
					? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400'
					: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400';

			const typeLabel = log.type === 'check' ? 'Update Check' : log.type === 'install' ? 'Install' : log.type;
			let details = '';
			if (log.packages && log.packages.length > 0) {
				details = `${log.packages.length} package(s)`;
			} else if (log.security_only) {
				details = 'Security only';
			}
			if (log.error) {
				details = log.error.substring(0, 60);
			}

			row.innerHTML = `
				<td class="px-4 py-3 text-gray-600 dark:text-gray-400">${date}</td>
				<td class="px-4 py-3"><span class="px-2 py-1 text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 rounded">${typeLabel}</span></td>
				<td class="px-4 py-3"><span class="px-2 py-1 text-xs ${statusColor} rounded">${log.status}</span></td>
				<td class="px-4 py-3 text-gray-600 dark:text-gray-400 text-xs">${details}</td>
				<td class="px-4 py-3">
					<button onclick="viewUpdateLog('${log.id}')" class="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 text-xs font-medium">
						View Output
					</button>
				</td>
			`;
			table.appendChild(row);
		});
	} catch (err) {
		table.innerHTML = `<tr><td colspan="5" class="px-4 py-8 text-center text-red-500">${err.message}</td></tr>`;
	}
}

async function viewUpdateLog(logID) {
	const modal = document.getElementById('update-log-modal');
	const output = document.getElementById('log-detail-output');
	const title = document.getElementById('log-detail-title');
	const statusIndicator = document.getElementById('log-status-indicator');

	if (!modal || !output) return;

	// Show modal with loading state
	modal.classList.remove('hidden');
	output.textContent = 'Loading...';
	title.textContent = 'Loading log...';

	try {
		const response = await fetch(`/api/os-updates/logs/${logID}?server=${currentServerID}`);
		if (!response.ok) {
			throw new Error(await extractErrorMessage(response));
		}

		const log = await response.json();

		// Update metadata
		title.textContent = `${log.type === 'check' ? 'Update Check' : 'Install'} - ${log.id.substring(0, 8)}`;
		document.getElementById('log-detail-type').textContent = log.type;
		document.getElementById('log-detail-status').textContent = log.status;
		document.getElementById('log-detail-started').textContent = new Date(log.started_at).toLocaleString();
		document.getElementById('log-detail-exit-code').textContent = log.exit_code;

		// Calculate duration
		if (log.completed_at) {
			const start = new Date(log.started_at);
			const end = new Date(log.completed_at);
			const diffMs = end - start;
			const diffSec = Math.floor(diffMs / 1000);
			if (diffSec < 60) {
				document.getElementById('log-detail-duration').textContent = `${diffSec}s`;
			} else {
				const min = Math.floor(diffSec / 60);
				const sec = diffSec % 60;
				document.getElementById('log-detail-duration').textContent = `${min}m ${sec}s`;
			}
		} else {
			document.getElementById('log-detail-duration').textContent = '-';
		}

		// Status indicator color
		if (log.status === 'completed') {
			statusIndicator.className = 'w-3 h-3 rounded-full bg-green-500';
		} else if (log.status === 'failed') {
			statusIndicator.className = 'w-3 h-3 rounded-full bg-red-500';
		} else {
			statusIndicator.className = 'w-3 h-3 rounded-full bg-gray-500';
		}

		// Show output
		output.textContent = log.output || '(no output captured)';
	} catch (err) {
		output.textContent = `Error loading log: ${err.message}`;
		statusIndicator.className = 'w-3 h-3 rounded-full bg-red-500';
	}
}

function hideUpdateLogModal() {
	const modal = document.getElementById('update-log-modal');
	if (modal) modal.classList.add('hidden');
}

function copyLogOutput() {
	const output = document.getElementById('log-detail-output');
	if (output) {
		navigator.clipboard.writeText(output.textContent).then(() => {
			showToast('Output copied to clipboard', 'success');
		}).catch(() => {
			showToast('Failed to copy output', 'error');
		});
	}
}

// ── Installed Packages ────────────────────────────────────────────────────────

// ── Installed Packages (Tabulator data grid) ──────────────────────────────────

let installedPkgTable = null;

async function loadInstalledPackages() {
	const el = document.getElementById('installed-packages-table');
	if (!el) return;

	try {
		const resp = await fetch('/api/os-updates/packages/installed?server=' + currentServerID);
		if (!resp.ok) {
			document.getElementById('installed-packages-loading').textContent = 'Failed to load installed packages.';
			return;
		}
		const data = await resp.json();
		const packages = data.packages || [];
		const canAction = document.getElementById('can-action-installed')?.value === 'true';
		initInstalledPackagesGrid(el, packages, canAction);
	} catch {
		const loading = document.getElementById('installed-packages-loading');
		if (loading) loading.textContent = 'Failed to load installed packages.';
	}
}

function initInstalledPackagesGrid(el, packages, canAction) {
	// Remove loading spinner — Tabulator renders into the div directly
	const loading = document.getElementById('installed-packages-loading');
	if (loading) loading.remove();

	// Row height × 20 visible rows + header (~42px) + header filter row (~34px)
	const ROW_HEIGHT = 36;
	const VISIBLE_ROWS = 12;
	const gridHeight = (ROW_HEIGHT * VISIBLE_ROWS) + 42 + 34;

	const columns = [
		{
			title: 'Package',
			field: 'name',
			sorter: 'string',
			headerFilter: 'input',
			headerFilterPlaceholder: 'filter...',
			minWidth: 180,
			formatter: function(cell) {
				return '<span class="font-mono text-xs font-medium">' + escapeHtml(cell.getValue()) + '</span>';
			}
		},
		{
			title: 'Version',
			field: 'version',
			sorter: 'string',
			headerFilter: 'input',
			headerFilterPlaceholder: 'filter...',
			width: 160,
			formatter: function(cell) {
				return '<span class="font-mono text-xs">' + escapeHtml(cell.getValue() || '') + '</span>';
			}
		},
		{
			title: 'Description',
			field: 'description',
			sorter: 'string',
			headerFilter: 'input',
			headerFilterPlaceholder: 'filter...',
			minWidth: 200,
			formatter: function(cell) {
				return '<span class="text-xs">' + escapeHtml(cell.getValue() || '') + '</span>';
			}
		},
	];

	if (canAction) {
		columns.push({
			title: '',
			field: 'name',
			headerSort: false,
			width: 90,
			hozAlign: 'right',
			formatter: function(cell) {
				return '<button class="pkg-remove-btn px-2.5 py-1 text-xs bg-red-100 dark:bg-red-900/30 hover:bg-red-200 dark:hover:bg-red-900/50 text-red-700 dark:text-red-400 rounded transition-colors">Remove</button>';
			},
			cellClick: function(e, cell) {
				if (e.target.classList.contains('pkg-remove-btn')) {
					removeInstalledPackageTabulator(cell, cell.getValue());
				}
			}
		});
	}

	installedPkgTable = new Tabulator(el, {
		data: packages,
		columns: columns,
		layout: 'fitColumns',
		height: gridHeight + 'px',
		virtualDom: true,
		virtualDomBuffer: 180,
		rowHeight: ROW_HEIGHT,
		sortMode: 'local',
		filterMode: 'local',
		initialSort: [{ column: 'name', dir: 'asc' }],
		placeholder: 'No packages found',
	});
}

function removeInstalledPackageTabulator(cell, name) {
	showConfirmModal({
		title: 'Remove Package',
		message: 'Remove ' + name + '? This will uninstall the package from the system.',
		type: 'danger',
		confirmText: 'Remove',
		onConfirm: async () => {
			try {
				const resp = await fetch('/api/os-updates/packages/remove?server=' + currentServerID, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ name: name })
				});
				if (!resp.ok) {
					const errMsg = await extractErrorMessage(resp);
					throw new Error(errMsg);
				}
				showToast('Package ' + name + ' removed', 'success');
				// Delete the row from the live grid without a reload
				cell.getRow().delete();
			} catch (err) {
				showToast('Failed to remove package: ' + err.message, 'error');
			}
		}
	});
}

// ── Apt Install Modal ─────────────────────────────────────────────────────────

let aptSearchTimer = null;
let aptSelectedPackage = null;

function showAptInstallModal() {
	document.getElementById('apt-install-modal').classList.remove('hidden');
	document.getElementById('apt-package-name').focus();
}

function hideAptInstallModal() {
	document.getElementById('apt-install-modal').classList.add('hidden');
	document.getElementById('apt-package-name').value = '';
	document.getElementById('apt-search-results').classList.add('hidden');
	document.getElementById('apt-search-results').innerHTML = '';
	document.getElementById('apt-selected-info').classList.add('hidden');
	document.getElementById('apt-no-results').classList.add('hidden');
	document.getElementById('apt-install-confirm-btn').disabled = true;
	aptSelectedPackage = null;
}

function onAptSearchInput(value) {
	clearTimeout(aptSearchTimer);
	aptSelectedPackage = null;
	document.getElementById('apt-install-confirm-btn').disabled = true;
	document.getElementById('apt-selected-info').classList.add('hidden');
	document.getElementById('apt-search-results').classList.add('hidden');
	document.getElementById('apt-no-results').classList.add('hidden');

	const trimmed = value.trim();
	if (trimmed.length < 2) return;

	aptSearchTimer = setTimeout(() => searchAptPackages(trimmed), 350);
}

async function searchAptPackages(query) {
	const spinner = document.getElementById('apt-search-spinner');
	const results = document.getElementById('apt-search-results');
	const noResults = document.getElementById('apt-no-results');

	if (spinner) spinner.classList.remove('hidden');

	try {
		const resp = await fetch('/api/os-updates/packages/search?server=' + currentServerID + '&q=' + encodeURIComponent(query) + '&limit=20');
		if (spinner) spinner.classList.add('hidden');
		if (!resp.ok) return;
		const data = await resp.json();
		const packages = data.packages || [];

		if (packages.length === 0) {
			noResults.classList.remove('hidden');
			return;
		}

		results.innerHTML = packages.map(pkg => `
			<div class="apt-result-item px-4 py-2.5 hover:bg-blue-50 dark:hover:bg-blue-900/20 cursor-pointer flex items-start gap-3"
				 data-name="${escapeHtml(pkg.name)}" data-desc="${escapeHtml(pkg.description || '')}"
				 onclick="selectAptPackage('${escapeHtml(pkg.name)}', '${escapeHtml(pkg.description || '')}')">
				<div class="flex-1 min-w-0">
					<div class="flex items-center gap-2">
						<span class="font-mono text-sm font-medium text-gray-800 dark:text-gray-100">${escapeHtml(pkg.name)}</span>
						${pkg.installed ? '<span class="px-1.5 py-0.5 text-xs bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400 rounded">installed</span>' : ''}
					</div>
					${pkg.description ? `<span class="text-xs text-gray-500 dark:text-gray-400 truncate">${escapeHtml(pkg.description)}</span>` : ''}
				</div>
			</div>
		`).join('');
		results.classList.remove('hidden');
	} catch {
		if (spinner) spinner.classList.add('hidden');
	}
}

function selectAptPackage(name, description) {
	aptSelectedPackage = name;
	document.getElementById('apt-package-name').value = name;
	document.getElementById('apt-search-results').classList.add('hidden');
	document.getElementById('apt-selected-name').textContent = name;
	document.getElementById('apt-selected-desc').textContent = description;
	document.getElementById('apt-selected-info').classList.remove('hidden');
	document.getElementById('apt-install-confirm-btn').disabled = false;
}

async function confirmAptInstall() {
	const name = aptSelectedPackage || document.getElementById('apt-package-name').value.trim();
	if (!name) {
		showToast('Please select a package to install', 'warning');
		return;
	}

	const btn = document.getElementById('apt-install-confirm-btn');
	hideAptInstallModal();

	try {
		const resp = await fetch('/api/os-updates/packages/install?server=' + currentServerID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name: name })
		});
		if (!resp.ok) {
			const errMsg = await extractErrorMessage(resp);
			throw new Error(errMsg);
		}
		showToast('Package ' + name + ' installed successfully', 'success');
		// Destroy and reload the Tabulator grid
		if (installedPkgTable) {
			installedPkgTable.destroy();
			installedPkgTable = null;
		}
		const el = document.getElementById('installed-packages-table');
		if (el) el.innerHTML = '<div id="installed-packages-loading" class="flex items-center gap-2 p-4 text-sm text-gray-500 dark:text-slate-400"><svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg> Loading installed packages...</div>';
		setTimeout(loadInstalledPackages, 500);
	} catch (err) {
		showToast('Failed to install package: ' + err.message, 'error');
	}
}
