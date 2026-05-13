let currentServerID = '';
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

	// Initialize pipx/pip grids from their embedded JSON, then kick off the
	// async PyPI version check that fills in the "Latest" column.
	if (document.getElementById('pipx-grid') || document.getElementById('pip-grid')) {
		initPythonToolsGrids();
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

// Central Escape key handler — closes whichever modal is currently visible.
document.addEventListener('keydown', function(e) {
	if (e.key !== 'Escape') return;
	const modals = [
		{ id: 'confirm-modal',      hide: hideConfirmModal },
		{ id: 'package-info-modal', hide: hidePackageInfoModal },
		{ id: 'reboot-modal',       hide: hideRebootModal },
		{ id: 'update-log-modal',   hide: hideUpdateLogModal },
		{ id: 'preview-modal',      hide: hidePreviewModal },
		{ id: 'pipx-install-modal', hide: hidePipxInstallModal },
		{ id: 'pip-install-modal',  hide: hidePipInstallModal },
		{ id: 'apt-install-modal',  hide: hideAptInstallModal },
	];
	for (const m of modals) {
		const el = document.getElementById(m.id);
		if (el && !el.classList.contains('hidden')) {
			m.hide();
			break;
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

// Manual refresh triggered by the LiveRefreshButton.
// On this page the refresh icon is the only "check for updates" affordance —
// run a real apt update check rather than a full page reload, so the user
// stays on the current scroll position and search/sort state survives.
async function manualRefresh() {
	const icon = document.getElementById('refresh-icon');
	if (icon) icon.classList.add('animate-spin');
	try {
		await checkForUpdates();
	} finally {
		if (icon) icon.classList.remove('animate-spin');
	}
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
		await refreshInstalledPackagesGrid();
		// Also refresh any open collapsible sections so the refresh icon
		// behaves predictably — same data update path as an SSE apt.completed.
		// Closed sections are skipped (their grid hasn't been initialized
		// yet and they'll fetch fresh on next expand anyway).
		if (typeof isSectionOpen === 'function') {
			if (isSectionOpen('snapshots')) refreshSnapshotsGrid();
			if (isSectionOpen('history'))   refreshHistoryGrid();
			if (isSectionOpen('logs'))      refreshLogsGrid();
		}

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

async function autoCheckForUpdates(retryCount = 0) {
	const MAX_RETRIES = 3;
	const RETRY_DELAY_MS = 5000;

	try {
		const response = await fetch('/api/os-updates/check?server=' + currentServerID, { method: 'POST' });
		if (!response.ok) {
			if (retryCount < MAX_RETRIES) {
				setTimeout(() => autoCheckForUpdates(retryCount + 1), RETRY_DELAY_MS);
				return;
			}
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

		// Update reboot status card
		if (data.reboot_required) {
			showRebootRequiredStatus();
		} else if (!isRebooting) {
			showRebootNotRequiredStatus();
		}

		// Update auto-updates card
		const autoEl = document.getElementById('auto-updates-status');
		if (autoEl) {
			autoEl.textContent = data.unattended_active ? 'Enabled' : 'Disabled';
			autoEl.className = 'text-2xl font-bold mt-2 ' + (data.unattended_active
				? 'text-green-600 dark:text-green-400'
				: 'text-gray-500 dark:text-gray-400');
		}

		// Refresh installed packages grid to pick up updated availability info
		await refreshInstalledPackagesGrid();
	} catch (err) {
		if (retryCount < MAX_RETRIES) {
			setTimeout(() => autoCheckForUpdates(retryCount + 1), RETRY_DELAY_MS);
			return;
		}
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
	setTimeout(() => document.getElementById('preview-close-btn')?.focus(), 50);

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
				// Refetch the snapshots grid from the server. Tabulator owns row
				// state — direct DOM manipulation is no longer reliable here.
				if (SECTIONS.snapshots) {
					await SECTIONS.snapshots.refresh();
					updateBulkDeleteButton();
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

// Toggle the bulk-delete button visibility/label based on Tabulator selection.
// Called from the snapshots grid's `rowSelectionChanged` callback in
// initSnapshotsGrid(). Also called manually after delete-one to refresh state.
function updateBulkDeleteButton() {
	const btn = document.getElementById('bulk-delete-snapshots-btn');
	if (!btn) return;
	const grid = SECTIONS.snapshots && SECTIONS.snapshots.grid;
	const count = grid ? grid.getSelectedData().length : 0;
	if (count > 0) {
		btn.classList.remove('hidden');
		btn.textContent = 'Delete Selected (' + count + ')';
	} else {
		btn.classList.add('hidden');
	}
}

function bulkDeleteSnapshots() {
	const grid = SECTIONS.snapshots && SECTIONS.snapshots.grid;
	if (!grid) return;
	const selected = grid.getSelectedData();
	if (selected.length === 0) return;
	const ids = selected.map(s => s.id);

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
					if (!response.ok) { failed++; continue; }
					deleted++;
				} catch {
					failed++;
				}
			}
			if (failed > 0) {
				showToast('Deleted ' + deleted + ', failed ' + failed, 'warning');
			} else {
				showToast(deleted + ' snapshot(s) deleted', 'success');
			}
			// Refresh the grid from the server rather than DOM-manipulating —
			// Tabulator owns the row state now.
			await SECTIONS.snapshots.refresh();
			updateBulkDeleteButton();
		}
	});
}

function scheduleReboot() {
	document.getElementById('reboot-modal').classList.remove('hidden');
	setTimeout(() => document.getElementById('reboot-confirm-btn')?.focus(), 50);
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

// Update the reboot status card to show reboot required state
function showRebootRequiredStatus() {
	const icon = document.getElementById('reboot-status-icon');
	const status = document.getElementById('reboot-status');
	const subtitle = document.getElementById('reboot-status-subtitle');

	if (icon) {
		icon.innerHTML = '<svg class="w-5 h-5 text-yellow-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path></svg>';
	}
	if (status) {
		status.className = 'text-2xl font-bold mt-2 text-yellow-600 dark:text-yellow-400';
		status.textContent = 'Required';
	}
	if (subtitle) {
		subtitle.textContent = 'System reboot status';
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

// pipx + pip live in Tabulator grids — see initPythonToolsGrids().
let pipxGrid = null;
let pipGrid = null;

// Cell formatter for the "Latest" column shared by pipx and pip grids.
// While latest_version is undefined the cell shows a spinner; once
// loadPythonVersions() populates it the cell shows either a muted "in sync"
// value, an amber "Update" badge for upgradable packages, or a dash when
// PyPI didn't return anything for this package.
function pythonLatestFormatter(cell) {
	const row = cell.getRow().getData();
	if (row.latest_version === undefined) {
		return '<svg class="w-3.5 h-3.5 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>';
	}
	if (row.update_available) {
		return '<span class="font-mono text-xs text-amber-600 dark:text-amber-400">' + escapeHtml(row.latest_version || '') + '</span>'
		     + ' <span class="px-1.5 py-0.5 text-xs bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-400 rounded">Update</span>';
	}
	if (row.latest_version) {
		return '<span class="font-mono text-xs text-gray-500 dark:text-gray-400">' + escapeHtml(row.latest_version) + '</span>';
	}
	return '<span class="text-xs text-gray-400 dark:text-gray-500">—</span>';
}

// Build a per-row "Upgrade / Uninstall" action column. The kind argument is
// either 'pipx' or 'pip' so the cellClick can dispatch to the right handlers.
function pythonActionColumn(kind) {
	return {
		title: '', headerSort: false, hozAlign: 'right', width: 200,
		formatter: function(cell) {
			const row = cell.getRow().getData();
			const updateAvail = !!row.update_available;
			const upgradeCls = 'py-upgrade-btn px-3 py-1 text-xs bg-blue-100 dark:bg-blue-900/30 hover:bg-blue-200 dark:hover:bg-blue-900/50 text-blue-700 dark:text-blue-400 rounded transition-colors mr-1.5 disabled:opacity-40 disabled:cursor-not-allowed';
			const upgradeAttrs = updateAvail ? '' : ' disabled';
			return '<button class="' + upgradeCls + '"' + upgradeAttrs + '>Upgrade</button>'
			     + '<button class="py-uninstall-btn px-3 py-1 text-xs bg-red-100 dark:bg-red-900/30 hover:bg-red-200 dark:hover:bg-red-900/50 text-red-700 dark:text-red-400 rounded transition-colors">Uninstall</button>';
		},
		cellClick: function(e, cell) {
			const name = cell.getRow().getData().name;
			const btn = e.target;
			if (btn.classList.contains('py-upgrade-btn') && !btn.disabled) {
				if (kind === 'pipx') upgradePipxPackage(btn, name);
				else                 upgradePipPackage(btn, name);
			} else if (btn.classList.contains('py-uninstall-btn')) {
				if (kind === 'pipx') uninstallPipxPackage(btn, name);
				else                 uninstallPipPackage(btn, name);
			}
		},
	};
}

function readJSONScript(id) {
	const tag = document.getElementById(id);
	if (!tag) return [];
	try {
		const parsed = JSON.parse(tag.textContent || 'null');
		return Array.isArray(parsed) ? parsed : [];
	} catch {
		return [];
	}
}

function initPythonToolsGrids() {
	const canAction = document.getElementById('can-action')?.value === 'true';

	const pipxEl = document.getElementById('pipx-grid');
	if (pipxEl) {
		const cols = [];
		if (canAction) {
			cols.push({
				formatter: 'rowSelection', titleFormatter: 'rowSelection',
				hozAlign: 'center', headerSort: false, width: 40,
				cellClick: function(e, cell) { cell.getRow().toggleSelect(); },
			});
		}
		cols.push({ title: 'Package',  field: 'name', sorter: 'string', minWidth: 160, widthGrow: 2,
			formatter: cell => '<span class="font-mono text-xs font-medium">' + escapeHtml(cell.getValue() || '') + '</span>' });
		cols.push({ title: 'Installed', field: 'version', sorter: 'string', width: 150,
			formatter: cell => '<span class="font-mono text-xs">' + escapeHtml(cell.getValue() || '') + '</span>' });
		cols.push({ title: 'Latest', field: 'latest_version', sorter: 'string', width: 170, formatter: pythonLatestFormatter });
		cols.push({ title: 'Apps', field: 'apps', sorter: 'string', minWidth: 140, widthGrow: 2,
			formatter: function(cell) {
				const apps = cell.getValue();
				if (Array.isArray(apps) && apps.length > 0) {
					return '<span class="text-xs text-gray-500 dark:text-gray-400">' + escapeHtml(apps.join(', ')) + '</span>';
				}
				return '<span class="text-xs text-gray-400 dark:text-gray-500">—</span>';
			},
		});
		if (canAction) cols.push(pythonActionColumn('pipx'));

		pipxGrid = createDataGrid(pipxEl, {
			data: readJSONScript('pipx-data'),
			columns: cols,
			maxHeight: '474px',
			rowHeight: 36,
			placeholder: 'No pipx packages installed',
			initialSort: [{ column: 'name', dir: 'asc' }],
			selectableRows: canAction,
		});
		if (canAction) {
			pipxGrid.on('rowSelectionChanged', updatePipxBulkBar);
		}
	}

	const pipEl = document.getElementById('pip-grid');
	if (pipEl) {
		const cols = [];
		if (canAction) {
			cols.push({
				formatter: 'rowSelection', titleFormatter: 'rowSelection',
				hozAlign: 'center', headerSort: false, width: 40,
				cellClick: function(e, cell) { cell.getRow().toggleSelect(); },
			});
		}
		cols.push({ title: 'Package', field: 'name', sorter: 'string', minWidth: 160, widthGrow: 3,
			formatter: cell => '<span class="font-mono text-xs font-medium">' + escapeHtml(cell.getValue() || '') + '</span>' });
		cols.push({ title: 'Installed', field: 'version', sorter: 'string', width: 150,
			formatter: cell => '<span class="font-mono text-xs">' + escapeHtml(cell.getValue() || '') + '</span>' });
		cols.push({ title: 'Latest', field: 'latest_version', sorter: 'string', width: 170, formatter: pythonLatestFormatter });
		if (canAction) cols.push(pythonActionColumn('pip'));

		pipGrid = createDataGrid(pipEl, {
			data: readJSONScript('pip-data'),
			columns: cols,
			maxHeight: '474px',
			rowHeight: 36,
			placeholder: 'No user-installed pip packages',
			initialSort: [{ column: 'name', dir: 'asc' }],
			selectableRows: canAction,
		});
		if (canAction) {
			pipGrid.on('rowSelectionChanged', updatePipBulkBar);
		}
	}
}

// Async PyPI version check — replaces the per-row spinner cells with real
// latest-version info. Merges into the grid rows in place so sort/filter
// state is preserved.
async function loadPythonVersions() {
	try {
		const response = await fetch('/api/os-updates/python-tools/versions?server=' + currentServerID);
		if (!response.ok) return;
		const data = await response.json();

		const mergeLatest = (grid, pkgs) => {
			if (!grid || !Array.isArray(pkgs)) return;
			const byName = {};
			pkgs.forEach(p => { byName[p.name] = p; });
			grid.getRows().forEach(row => {
				const d = row.getData();
				const upd = byName[d.name];
				if (upd) {
					row.update({
						latest_version: upd.latest_version || '',
						update_available: !!upd.update_available,
					});
				} else {
					// PyPI returned nothing for this package — clear the spinner
					row.update({ latest_version: '', update_available: false });
				}
			});
		};

		mergeLatest(pipxGrid, data.pipx?.packages);
		mergeLatest(pipGrid,  data.pip?.packages);
	} catch {
		// Non-fatal: spinners stay if the network blip; user can refresh.
	}
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
// Tabulator owns row selection state. These helpers project that state onto
// the bulk-action bar in the section header.

function updatePipxBulkBar() {
	const bar = document.getElementById('pipx-bulk-bar');
	const count = document.getElementById('pipx-selected-count');
	const n = pipxGrid ? pipxGrid.getSelectedData().length : 0;
	if (bar) bar.classList.toggle('hidden', n === 0);
	if (count) count.textContent = n + ' selected';
}

function clearPipxSelection() {
	if (pipxGrid) pipxGrid.deselectRow();
}

async function bulkUpgradePipx() {
	if (!pipxGrid) return;
	const names = pipxGrid.getSelectedData().map(r => r.name);
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
	if (!pipxGrid) return;
	const names = pipxGrid.getSelectedData().map(r => r.name);
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

function updatePipBulkBar() {
	const bar = document.getElementById('pip-bulk-bar');
	const count = document.getElementById('pip-selected-count');
	const n = pipGrid ? pipGrid.getSelectedData().length : 0;
	if (bar) bar.classList.toggle('hidden', n === 0);
	if (count) count.textContent = n + ' selected';
}

function clearPipSelection() {
	if (pipGrid) pipGrid.deselectRow();
}

async function bulkUpgradePip() {
	if (!pipGrid) return;
	const names = pipGrid.getSelectedData().map(r => r.name);
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
	if (!pipGrid) return;
	const names = pipGrid.getSelectedData().map(r => r.name);
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
	setTimeout(() => document.getElementById('package-info-close-btn')?.focus(), 50);
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
			input.onkeydown = function(e) {
				if (e.key === 'Enter') { e.preventDefault(); actionBtn.click(); }
			};
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
			input.onkeydown = null;
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
	// Focus the input if shown, otherwise focus the action button so Enter activates it
	setTimeout(() => {
		if (options.showInput && input && !input.classList.contains('hidden')) {
			input.focus();
		} else {
			actionBtn.focus();
		}
	}, 50);
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
	totalPackagesToInstall = 0;
	totalInstallSize = 0;

	// Collect names of packages being installed from Tabulator data
	if (installedPkgTable) {
		const data = installedPkgTable.getData();
		data.forEach(pkg => {
			if (allPackages ? pkg.update_available : false) {
				installingPackages.add(pkg.name);
				totalPackagesToInstall++;
			}
		});
	}

	// Show progress bar
	const progressContainer = document.getElementById('install-progress-container');
	const progressBar = document.getElementById('install-progress-bar');
	if (progressContainer && progressBar) {
		progressContainer.classList.remove('hidden');
		progressBar.style.width = '0%';
	}
}

// No-op stubs kept for compatibility with progress tracking calls
function setCurrentPackageSpinner(packageName) {
	// Progress tracked via line count in apt output handler
	packagesInstalled++;
	updateProgressBar();
}

// Mark package as completed
function markPackageCompleted(packageName) {
	packagesInstalled++;
	updateProgressBar();
}

// Update progress bar based on installed packages
function updateProgressBar() {
	const progressBar = document.getElementById('install-progress-bar');
	if (!progressBar) return;

	let progress = 0;
	if (totalPackagesToInstall > 0) {
		progress = Math.min((packagesInstalled / totalPackagesToInstall) * 100, 95);
	}

	progressBar.style.width = progress + '%';
}

// Reset install progress (e.g., on completion or page refresh)
function resetInstallProgress() {
	const progressContainer = document.getElementById('install-progress-container');
	const progressBar = document.getElementById('install-progress-bar');
	if (progressContainer) progressContainer.classList.add('hidden');
	if (progressBar) progressBar.style.width = '0%';

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

	// Parse for progress tracking — each "Setting up" line means one package configured
	if (event.data && event.data.line) {
		const line = event.data.line;
		if (line.match(/^Setting up /)) {
			packagesInstalled = Math.min(packagesInstalled + 1, totalPackagesToInstall);
			updateProgressBar();
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
// Update Logs (lazy-loaded via SECTIONS.logs, see Collapsible Sections below)
// ============================================================================

async function viewUpdateLog(logID) {
	const modal = document.getElementById('update-log-modal');
	const output = document.getElementById('log-detail-output');
	const title = document.getElementById('log-detail-title');
	const statusIndicator = document.getElementById('log-status-indicator');

	if (!modal || !output) return;

	// Show modal with loading state
	modal.classList.remove('hidden');
	setTimeout(() => document.getElementById('update-log-close-btn')?.focus(), 50);
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
// Track whether the full installed package list has been loaded.
// On page load we only fetch upgradable packages (fast). The full list is
// lazy-loaded the first time the user switches to "All Packages".
let allPackagesLoaded = false;

// Normalize upgradable Package objects (from /api/os-updates/packages) to the
// shape expected by the grid (which was designed around InstalledPackage).
// is_held flows from the agent; held packages can still appear in
// `apt list --upgradable` when a newer version exists but apt-mark/dpkg has
// pinned them. The UI surfaces them via a "held" badge and a dedicated tab.
function normalizeUpgradablePackages(packages) {
	return packages.map(p => ({
		name: p.name,
		version: p.current_version,
		available_version: p.available_version,
		architecture: p.architecture,
		description: '',
		update_available: true,
		is_security_update: p.is_security_update || false,
		is_held: p.is_held || false,
		package_url: p.package_url || '',
	}));
}

async function loadInstalledPackages(retryCount = 0) {
	const el = document.getElementById('installed-packages-table');
	if (!el) return;

	const MAX_RETRIES = 3;
	const RETRY_DELAY_MS = 3000;

	// Default view is "updates" — only fetch upgradable packages on page load.
	try {
		const resp = await fetch('/api/os-updates/packages?server=' + currentServerID);
		if (!resp.ok) {
			if (retryCount < MAX_RETRIES) {
				const loading = document.getElementById('installed-packages-loading');
				if (loading) loading.textContent = 'Connecting to agent...';
				setTimeout(() => loadInstalledPackages(retryCount + 1), RETRY_DELAY_MS);
				return;
			}
			const loading = document.getElementById('installed-packages-loading');
			if (loading) loading.textContent = 'Failed to load packages.';
			return;
		}
		const data = await resp.json();
		const packages = normalizeUpgradablePackages(data.packages || []);
		const canAction = document.getElementById('can-action-installed')?.value === 'true';
		allPackagesLoaded = false;
		initInstalledPackagesGrid(el, packages, canAction);
	} catch {
		if (retryCount < MAX_RETRIES) {
			const loading = document.getElementById('installed-packages-loading');
			if (loading) loading.textContent = 'Connecting to agent...';
			setTimeout(() => loadInstalledPackages(retryCount + 1), RETRY_DELAY_MS);
			return;
		}
		const loading = document.getElementById('installed-packages-loading');
		if (loading) loading.textContent = 'Failed to load packages.';
	}
}

async function loadAllInstalledPackages() {
	if (allPackagesLoaded) return;
	if (!installedPkgTable) return;

	const loading = document.getElementById('installed-packages-loading');
	// Show a subtle loading state in the table
	installedPkgTable.alert('Loading all packages...');

	try {
		const resp = await fetch('/api/os-updates/packages/installed?server=' + currentServerID);
		if (!resp.ok) {
			installedPkgTable.clearAlert();
			return;
		}
		const data = await resp.json();
		const packages = data.packages || [];
		allPackagesLoaded = true;
		installedPkgTable.clearAlert();
		installedPkgTable.setData(packages);
		_applyPkgViewFilter('all', packages);
	} catch (err) {
		if (installedPkgTable) installedPkgTable.clearAlert();
		console.warn('Failed to load all packages:', err.message);
	}
}

function initInstalledPackagesGrid(el, packages, canAction) {
	// Remove loading spinner — Tabulator renders into the div directly
	const loading = document.getElementById('installed-packages-loading');
	if (loading) loading.remove();

	// Row height × 12 visible rows + header (~42px). Per-column filter row
	// has been replaced by a toolbar-mounted global search (see #pkg-search).
	const ROW_HEIGHT = 36;
	const VISIBLE_ROWS = 12;
	const gridHeight = (ROW_HEIGHT * VISIBLE_ROWS) + 42;

	const columns = [
		{
			title: 'Package',
			field: 'name',
			sorter: 'string',
			minWidth: 180,
			formatter: function(cell) {
				const row = cell.getRow().getData();
				const name = escapeHtml(cell.getValue());
				let badge = '';
				if (row.is_held) {
					badge = ' <span class="px-1.5 py-0.5 text-xs bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-400 rounded font-medium">held</span>';
				} else if (row.is_security_update) {
					badge = ' <span class="px-1.5 py-0.5 text-xs bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400 rounded font-medium">security</span>';
				} else if (row.update_available) {
					badge = ' <span class="px-1.5 py-0.5 text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 rounded font-medium">update</span>';
				}
				if (row.package_url) {
					return '<a href="' + escapeHtml(row.package_url) + '" target="_blank" rel="noopener noreferrer" class="font-mono text-xs font-medium text-blue-600 dark:text-blue-400 hover:underline">' + name + '</a>' + badge;
				}
				return '<span class="font-mono text-xs font-medium">' + name + '</span>' + badge;
			}
		},
		{
			title: 'Installed Version',
			field: 'version',
			sorter: 'string',
			width: 170,
			formatter: function(cell) {
				return '<span class="font-mono text-xs">' + escapeHtml(cell.getValue() || '') + '</span>';
			}
		},
		{
			title: 'Available Version',
			field: 'available_version',
			sorter: 'string',
			width: 170,
			formatter: function(cell) {
				const val = cell.getValue();
				if (val) {
					return '<span class="font-mono text-xs text-green-600 dark:text-green-400 font-medium">' + escapeHtml(val) + '</span>';
				}
				// No update available — show current version in muted text
				const current = cell.getRow().getData().version || '';
				return '<span class="font-mono text-xs text-gray-500 dark:text-gray-400">' + escapeHtml(current) + '</span>';
			}
		},
		{
			title: 'Description',
			field: 'description',
			sorter: 'string',
			minWidth: 160,
			widthGrow: 3,
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
			width: 150,
			hozAlign: 'right',
			formatter: function(cell) {
				const row = cell.getRow().getData();
				// Show Unhold for held packages, Hold for any upgradable-but-not-held
				// row. The action mirrors the existing Unhold pattern and posts to
				// the matching /api/os-updates/packages/hold endpoint.
				let holdBtn;
				if (row.is_held) {
					holdBtn = '<button class="pkg-unhold-btn px-2.5 py-1 text-xs bg-yellow-100 dark:bg-yellow-900/30 hover:bg-yellow-200 dark:hover:bg-yellow-900/50 text-yellow-700 dark:text-yellow-400 rounded transition-colors mr-1">Unhold</button>';
				} else if (row.update_available) {
					holdBtn = '<button class="pkg-hold-btn px-2.5 py-1 text-xs bg-yellow-50 dark:bg-yellow-900/20 hover:bg-yellow-100 dark:hover:bg-yellow-900/40 text-yellow-700 dark:text-yellow-400 rounded transition-colors mr-1">Hold</button>';
				} else {
					holdBtn = '';
				}
				return holdBtn + '<button class="pkg-remove-btn px-2.5 py-1 text-xs bg-red-100 dark:bg-red-900/30 hover:bg-red-200 dark:hover:bg-red-900/50 text-red-700 dark:text-red-400 rounded transition-colors">Remove</button>';
			},
			cellClick: function(e, cell) {
				const name = cell.getValue();
				if (e.target.classList.contains('pkg-remove-btn')) {
					removeInstalledPackageTabulator(cell, name);
				} else if (e.target.classList.contains('pkg-unhold-btn')) {
					unholdPackage(name, cell);
				} else if (e.target.classList.contains('pkg-hold-btn')) {
					holdPackage(name, cell);
				}
			}
		});
	}

	const filterSelect = document.getElementById('pkg-view-filter');
	const initialFilter = filterSelect ? filterSelect.value : 'updates';

	installedPkgTable = createDataGrid(el, {
		data: packages,
		columns: columns,
		height: gridHeight + 'px',
		virtualDom: true,
		virtualDomBuffer: 180,
		rowHeight: ROW_HEIGHT,
		initialSort: [{ column: 'name', dir: 'asc' }],
		placeholder: 'No packages found',
		searchInput: '#pkg-search',
		searchFields: ['name', 'version', 'available_version', 'description'],
	});

	// Apply initial filter + visibility AFTER the constructor returns so
	// `installedPkgTable` is guaranteed assigned. Previously this lived in the
	// `tableBuilt` callback, where the timing was unreliable: on some loads it
	// fired before _applyPkgViewFilter's `if (!installedPkgTable) return` had
	// a truthy reference to bail past, leaving the "Update All (N)" button
	// stuck in its hidden template state.
	_applyPkgViewFilter(initialFilter, packages);
}

function onPkgViewFilterChange(value) {
	if (!installedPkgTable) return;
	if (value === 'all' && !allPackagesLoaded) {
		// Lazy-load full package list on first switch to "All Packages"
		loadAllInstalledPackages();
		return;
	}
	const data = installedPkgTable.getData();
	_applyPkgViewFilter(value, data);
}

function _applyPkgViewFilter(value, allData) {
	if (!installedPkgTable) return;

	if (value === 'updates') {
		// Active updates: upgradable AND not held. "Hold" is an explicit operator
		// opt-out from upgrade, so held rows do not belong in the updates view.
		// setViewFilters composes with the toolbar search box; setFilter would
		// clobber it.
		installedPkgTable.setViewFilters([
			{ field: 'update_available', type: '=', value: true },
			{ field: 'is_held', type: '=', value: false },
		]);
	} else if (value === 'held') {
		installedPkgTable.setViewFilters({ field: 'is_held', type: '=', value: true });
	} else {
		installedPkgTable.clearViewFilters();
	}

	// Count packages excluding held ones — held packages are intentionally
	// excluded from the upgrade plan, so "Update All (N)" should reflect only
	// what would actually be upgraded.
	const updateCount = allData.filter(p => p.update_available && !p.is_held).length;
	const securityCount = allData.filter(p => p.is_security_update && !p.is_held).length;

	// Update All / Security buttons drive the active-upgrades plan, so they
	// only make sense on the "updates" view. On "held" or "all" they would be
	// misleading (the visible rows are not what would be upgraded).
	const onUpdatesView = value === 'updates';

	const updateAllBtn = document.getElementById('pkg-update-all-btn');
	const updateAllLabel = document.getElementById('pkg-update-all-label');
	if (updateAllBtn && onUpdatesView && updateCount > 0) {
		updateAllBtn.classList.remove('hidden');
		if (updateAllLabel) {
			updateAllLabel.textContent = 'Update All (' + updateCount + ')';
		}
	} else if (updateAllBtn) {
		updateAllBtn.classList.add('hidden');
	}

	const secBtn = document.getElementById('pkg-update-security-btn');
	if (secBtn) {
		if (onUpdatesView && securityCount > 0) {
			secBtn.classList.remove('hidden');
		} else {
			secBtn.classList.add('hidden');
		}
	}
}

async function refreshInstalledPackagesGrid() {
	if (!installedPkgTable) return;
	const filterSelect = document.getElementById('pkg-view-filter');
	const currentFilter = filterSelect ? filterSelect.value : 'updates';

	try {
		if (currentFilter === 'all' || allPackagesLoaded) {
			// Full refresh of installed packages
			const resp = await fetch('/api/os-updates/packages/installed?server=' + currentServerID);
			if (!resp.ok) return;
			const data = await resp.json();
			const packages = data.packages || [];
			allPackagesLoaded = true;
			installedPkgTable.setData(packages);
			_applyPkgViewFilter(currentFilter, packages);
		} else {
			// Refresh upgradable-only view
			const resp = await fetch('/api/os-updates/packages?server=' + currentServerID);
			if (!resp.ok) return;
			const data = await resp.json();
			const packages = normalizeUpgradablePackages(data.packages || []);
			allPackagesLoaded = false;
			installedPkgTable.setData(packages);
			_applyPkgViewFilter(currentFilter, packages);
		}
	} catch (err) {
		console.warn('Failed to refresh packages grid:', err.message);
	}
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

// ── Package Hold / Unhold ─────────────────────────────────────────────────────

async function holdPackage(name, cell) {
	showConfirmModal({
		title: 'Hold Package',
		message: 'Hold ' + name + '? This prevents apt from upgrading it until the hold is removed.',
		type: 'warning',
		confirmText: 'Hold',
		onConfirm: async () => {
			try {
				const resp = await fetch('/api/os-updates/packages/hold?server=' + currentServerID, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ name: name })
				});
				if (!resp.ok) {
					const errMsg = await extractErrorMessage(resp);
					throw new Error(errMsg);
				}
				showToast(name + ' is now held', 'success');
				// Flip is_held in place so the row re-renders into the held filter.
				if (cell) {
					const row = cell.getRow();
					const data = row.getData();
					data.is_held = true;
					row.update(data);
					// Re-apply the active filter so the row moves between tabs
					// without requiring a manual switch.
					const filterSelect = document.getElementById('pkg-view-filter');
					if (filterSelect) {
						_applyPkgViewFilter(filterSelect.value, installedPkgTable.getData());
					}
				}
			} catch (err) {
				showToast('Failed to hold package: ' + err.message, 'error');
			}
		}
	});
}

async function unholdPackage(name, cell) {
	showConfirmModal({
		title: 'Remove Hold',
		message: 'Allow ' + name + ' to be upgraded again? This removes the hold.',
		type: 'warning',
		confirmText: 'Remove Hold',
		onConfirm: async () => {
			try {
				const resp = await fetch('/api/os-updates/packages/unhold?server=' + currentServerID, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ name: name })
				});
				if (!resp.ok) {
					const errMsg = await extractErrorMessage(resp);
					throw new Error(errMsg);
				}
				showToast(name + ' hold removed', 'success');
				// Update the row data in-place and reapply the active filter so
				// the row moves out of the "Held" tab when viewing it.
				if (cell) {
					const row = cell.getRow();
					const data = row.getData();
					data.is_held = false;
					row.update(data);
					const filterSelect = document.getElementById('pkg-view-filter');
					if (filterSelect) {
						_applyPkgViewFilter(filterSelect.value, installedPkgTable.getData());
					}
				}
			} catch (err) {
				showToast('Failed to remove hold: ' + err.message, 'error');
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
		// Destroy and reload the Tabulator grid. loadInstalledPackages only
		// fetches the upgradable list (fast path) — if the user was on the
		// "All Packages" view, we need to also lazy-load the full installed
		// list so the just-installed package actually appears. Without this,
		// the post-install grid is empty whenever the filter happens to be
		// "All" (or "Held"), and the user has to switch view + back to
		// trigger the full-list fetch manually.
		if (installedPkgTable) {
			installedPkgTable.destroy();
			installedPkgTable = null;
		}
		allPackagesLoaded = false;
		const filterSelect = document.getElementById('pkg-view-filter');
		const postInstallFilter = filterSelect ? filterSelect.value : 'updates';
		const el = document.getElementById('installed-packages-table');
		if (el) el.innerHTML = '<div id="installed-packages-loading" class="flex items-center gap-2 p-4 text-sm text-gray-500 dark:text-slate-400"><svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg> Loading installed packages...</div>';
		setTimeout(async () => {
			await loadInstalledPackages();
			if (postInstallFilter === 'all') {
				await loadAllInstalledPackages();
			}
		}, 500);
	} catch (err) {
		showToast('Failed to install package: ' + err.message, 'error');
	}
}

// ============================================================================
// Collapsible Sections (Snapshots, History, Logs)
//
// These three sections sit in <details data-section="..."> wrappers and are
// lazy-loaded on first expand. While a section is open, apt.completed SSE
// events trigger a refetch so the grid stays live. Closing the section stops
// future refreshes. The grid instance itself is kept around — re-expanding
// shows the cached data instantly (and then a refresh fires if SSE arrives).
//
// Caveat: apt operations performed OUTSIDE gearbox (e.g. `ssh dave@host;
// apt install x`) won't fire the SSE event, so those changes only show up
// after a manual refresh (page reload or the top-right refresh icon). A
// future filesystem-watch on the agent could cover that.
// ============================================================================

const SECTIONS = {
	snapshots: { details: null, grid: null, init: null, refresh: null },
	history:   { details: null, grid: null, init: null, refresh: null },
	logs:      { details: null, grid: null, init: null, refresh: null },
};

function isSectionOpen(name) {
	const s = SECTIONS[name];
	return !!(s && s.details && s.details.open);
}

function fmtLocalDateTime(value) {
	if (!value) return '';
	const d = new Date(value);
	if (isNaN(d.getTime())) return escapeHtml(String(value));
	return d.toLocaleString();
}

function snapshotIsAuto(reason) {
	return typeof reason === 'string' && reason.indexOf('auto:') === 0;
}

function snapshotDescriptionText(reason) {
	if (!reason || reason === 'manual') return '';
	if (typeof reason === 'string' && reason.indexOf('auto: ') === 0) {
		return reason.substring(6);
	}
	return reason;
}

function historyActionColor(action) {
	switch (action) {
		case 'install': return 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400';
		case 'upgrade': return 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400';
		case 'remove':
		case 'purge':   return 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400';
		default:        return 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400';
	}
}

async function fetchSnapshots() {
	const r = await fetch('/api/os-updates/snapshots?server=' + currentServerID);
	if (!r.ok) throw new Error(await extractErrorMessage(r));
	const data = await r.json();
	let snaps = data.snapshots || data || [];
	if (!Array.isArray(snaps)) snaps = [];
	return snaps.map(s => ({
		id: s.id || s.ID || s.name || '',
		createdAt: s.createdAt || s.created_at || s.CreatedAt || '',
		reason: s.reason || s.Reason || '',
	}));
}

function initSnapshotsGrid() {
	const canAction = document.getElementById('can-action')?.value === 'true';
	const el = document.getElementById('snapshots-grid');
	if (!el) return;

	const columns = [];
	if (canAction) {
		columns.push({
			formatter: 'rowSelection', titleFormatter: 'rowSelection',
			hozAlign: 'center', headerSort: false, width: 40,
			cellClick: function(e, cell) { cell.getRow().toggleSelect(); },
		});
	}
	columns.push({
		title: 'Date', field: 'createdAt', sorter: 'string', width: 200,
		formatter: cell => '<span class="text-xs">' + escapeHtml(fmtLocalDateTime(cell.getValue())) + '</span>',
	});
	columns.push({
		title: 'Name', field: 'id', sorter: 'string', width: 170,
		formatter: cell => '<span class="font-mono text-xs font-medium">' + escapeHtml(cell.getValue() || '') + '</span>',
	});
	columns.push({
		title: 'Type', field: 'reason', sorter: 'string', width: 140,
		formatter: function(cell) {
			const reason = cell.getValue() || '';
			const isFirst = cell.getRow().getPosition(true) === 1;
			const typeBadge = snapshotIsAuto(reason)
				? '<span class="px-2 py-0.5 text-xs bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-400 rounded font-medium">auto</span>'
				: '<span class="px-2 py-0.5 text-xs bg-gray-100 dark:bg-slate-600 text-gray-600 dark:text-gray-400 rounded font-medium">manual</span>';
			const latest = isFirst
				? ' <span class="px-2 py-0.5 text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 rounded font-medium">Latest</span>'
				: '';
			return typeBadge + latest;
		},
	});
	columns.push({
		title: 'Description', field: 'reason', sorter: 'string',
		minWidth: 160, widthGrow: 3,
		formatter: cell => '<span class="text-xs">' + escapeHtml(snapshotDescriptionText(cell.getValue())) + '</span>',
	});
	if (canAction) {
		columns.push({
			title: '', field: 'id', headerSort: false, width: 200, hozAlign: 'right',
			formatter: function() {
				return '<button class="snap-preview-btn text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 text-xs font-medium mr-3">Preview</button>'
				     + '<button class="snap-restore-btn text-yellow-600 dark:text-yellow-400 hover:text-yellow-800 dark:hover:text-yellow-300 text-xs font-medium mr-3">Restore</button>'
				     + '<button class="snap-delete-btn text-red-600 dark:text-red-400 hover:text-red-800 dark:hover:text-red-300 text-xs font-medium">Delete</button>';
			},
			cellClick: function(e, cell) {
				const id = cell.getRow().getData().id;
				if (e.target.classList.contains('snap-preview-btn')) previewSnapshot(id);
				else if (e.target.classList.contains('snap-restore-btn')) restoreSnapshot(id);
				else if (e.target.classList.contains('snap-delete-btn')) deleteSnapshot(id);
			},
		});
	}

	SECTIONS.snapshots.grid = createDataGrid(el, {
		data: [],
		columns: columns,
		// Auto-size to content, capped at 474px (12 rows × 36 + header). Sparse
		// lists feel right-sized; long lists scroll inside the card.
		maxHeight: '474px',
		rowHeight: 36,
		placeholder: 'No snapshots available',
		initialSort: [{ column: 'createdAt', dir: 'desc' }],
		selectableRows: canAction,
	});
	// Constructor-level event callbacks aren't reliably picked up in
	// Tabulator 6.3 — subscribe imperatively after the table exists.
	SECTIONS.snapshots.grid.on('rowSelectionChanged', updateBulkDeleteButton);
}

async function refreshSnapshotsGrid() {
	if (!SECTIONS.snapshots.grid) return;
	try {
		const rows = await fetchSnapshots();
		SECTIONS.snapshots.grid.setData(rows);
	} catch (err) {
		console.warn('Snapshots refresh failed:', err.message);
	}
}

async function fetchHistory() {
	const r = await fetch('/api/os-updates/history?server=' + currentServerID + '&limit=50');
	if (!r.ok) throw new Error(await extractErrorMessage(r));
	const data = await r.json();
	let hist = data.history || data || [];
	if (!Array.isArray(hist)) hist = [];
	return hist.map(h => ({
		timestamp:   h.timestamp || h.Timestamp || '',
		action:      h.action || h.Action || '',
		package:     h.package || h.Package || '',
		fromVersion: h.fromVersion || h.from_version || h.FromVersion || '',
		toVersion:   h.toVersion || h.to_version || h.ToVersion || '',
		status:      h.status || h.Status || '',
	}));
}

function initHistoryGrid() {
	const el = document.getElementById('history-grid');
	if (!el) return;
	const columns = [
		{
			title: 'Timestamp', field: 'timestamp', sorter: 'string', width: 200,
			formatter: cell => '<span class="text-xs">' + escapeHtml(fmtLocalDateTime(cell.getValue())) + '</span>',
		},
		{
			title: 'Action', field: 'action', sorter: 'string', width: 110,
			formatter: function(cell) {
				const v = cell.getValue() || '';
				return '<span class="px-2 py-0.5 text-xs rounded ' + historyActionColor(v) + '">' + escapeHtml(v) + '</span>';
			},
		},
		{
			title: 'Package', field: 'package', sorter: 'string', minWidth: 160, widthGrow: 2,
			formatter: cell => '<span class="font-mono text-xs font-medium">' + escapeHtml(cell.getValue() || '') + '</span>',
		},
		{
			title: 'Version Change', field: 'toVersion', sorter: 'string', minWidth: 160, widthGrow: 2,
			formatter: function(cell) {
				const row = cell.getRow().getData();
				if (row.fromVersion && row.toVersion) {
					return '<span class="font-mono text-xs">' + escapeHtml(row.fromVersion) + ' → ' + escapeHtml(row.toVersion) + '</span>';
				}
				if (row.toVersion) {
					return '<span class="font-mono text-xs">' + escapeHtml(row.toVersion) + '</span>';
				}
				return '<span class="text-xs text-gray-500 dark:text-gray-400">—</span>';
			},
		},
		{
			title: 'Status', field: 'status', sorter: 'string', width: 110,
			formatter: function(cell) {
				const v = cell.getValue() || '';
				if (v === 'success' || v === 'install' || v === 'upgrade') {
					return '<span class="px-2 py-0.5 text-xs bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400 rounded">Success</span>';
				}
				if (v === 'remove' || v === 'purge') {
					return '<span class="px-2 py-0.5 text-xs bg-yellow-100 dark:bg-yellow-900/30 text-yellow-700 dark:text-yellow-400 rounded">Removed</span>';
				}
				return '<span class="px-2 py-0.5 text-xs bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 rounded">' + escapeHtml(v) + '</span>';
			},
		},
	];

	SECTIONS.history.grid = createDataGrid(el, {
		data: [],
		columns: columns,
		maxHeight: '474px',
		rowHeight: 36,
		placeholder: 'No update history available',
		initialSort: [{ column: 'timestamp', dir: 'desc' }],
	});
}

async function refreshHistoryGrid() {
	if (!SECTIONS.history.grid) return;
	try {
		const rows = await fetchHistory();
		SECTIONS.history.grid.setData(rows);
	} catch (err) {
		console.warn('History refresh failed:', err.message);
	}
}

async function fetchLogs() {
	const r = await fetch('/api/os-updates/logs?server=' + currentServerID + '&limit=50');
	if (!r.ok) throw new Error(await extractErrorMessage(r));
	const data = await r.json();
	let logs = data.logs || data || [];
	if (!Array.isArray(logs)) logs = [];
	return logs;
}

function initLogsGrid() {
	const el = document.getElementById('logs-grid');
	if (!el) return;
	const columns = [
		{
			title: 'Date', field: 'started_at', sorter: 'string', width: 200,
			formatter: cell => '<span class="text-xs">' + escapeHtml(fmtLocalDateTime(cell.getValue())) + '</span>',
		},
		{
			title: 'Type', field: 'type', sorter: 'string', width: 140,
			formatter: function(cell) {
				const v = cell.getValue() || '';
				const label = v === 'check' ? 'Update Check' : v === 'install' ? 'Install' : v;
				return '<span class="px-2 py-0.5 text-xs bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-400 rounded">' + escapeHtml(label) + '</span>';
			},
		},
		{
			title: 'Status', field: 'status', sorter: 'string', width: 120,
			formatter: function(cell) {
				const v = cell.getValue() || '';
				const cls = v === 'completed'
					? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400'
					: v === 'failed'
						? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-400'
						: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400';
				return '<span class="px-2 py-0.5 text-xs rounded ' + cls + '">' + escapeHtml(v) + '</span>';
			},
		},
		{
			title: 'Details', field: 'packages', sorter: 'string', minWidth: 160, widthGrow: 3,
			formatter: function(cell) {
				const row = cell.getRow().getData();
				let detail = '';
				if (row.error) detail = String(row.error).substring(0, 120);
				else if (row.packages && row.packages.length > 0) detail = row.packages.length + ' package(s)';
				else if (row.security_only) detail = 'Security only';
				return '<span class="text-xs">' + escapeHtml(detail) + '</span>';
			},
		},
		{
			title: '', field: 'id', headerSort: false, width: 120, hozAlign: 'right',
			formatter: () => '<button class="log-view-btn text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 text-xs font-medium">View Output</button>',
			cellClick: function(e, cell) {
				if (e.target.classList.contains('log-view-btn')) viewUpdateLog(cell.getRow().getData().id);
			},
		},
	];

	SECTIONS.logs.grid = createDataGrid(el, {
		data: [],
		columns: columns,
		maxHeight: '474px',
		rowHeight: 36,
		placeholder: 'No update logs available',
		initialSort: [{ column: 'started_at', dir: 'desc' }],
	});
}

async function refreshLogsGrid() {
	if (!SECTIONS.logs.grid) return;
	try {
		const rows = await fetchLogs();
		SECTIONS.logs.grid.setData(rows);
	} catch (err) {
		console.warn('Logs refresh failed:', err.message);
	}
}

function initCollapsibleSections() {
	const map = {
		snapshots: { init: initSnapshotsGrid, refresh: refreshSnapshotsGrid },
		history:   { init: initHistoryGrid,   refresh: refreshHistoryGrid },
		logs:      { init: initLogsGrid,      refresh: refreshLogsGrid },
	};

	document.querySelectorAll('details.datagrid-card[data-section]').forEach(d => {
		const name = d.dataset.section;
		const cfg = map[name];
		if (!cfg) return;
		SECTIONS[name].details = d;
		SECTIONS[name].init = cfg.init;
		SECTIONS[name].refresh = cfg.refresh;

		// Action buttons inside <summary> need to NOT toggle the details on
		// click — without this, clicking "Create Snapshot" would also collapse
		// the section.
		const summary = d.querySelector(':scope > summary');
		if (summary) {
			summary.addEventListener('click', function(e) {
				if (e.target.closest('button')) {
					e.preventDefault();
					e.stopPropagation();
				}
			});
		}

		d.addEventListener('toggle', async function() {
			if (!d.open) return;
			if (!SECTIONS[name].grid) cfg.init();
			await cfg.refresh();
		});
	});

	// Live updates: when apt completes, refresh whichever sections are open.
	if (window.registerEventHandler) {
		window.registerEventHandler('apt.completed', function() {
			if (isSectionOpen('snapshots')) refreshSnapshotsGrid();
			if (isSectionOpen('history'))   refreshHistoryGrid();
			if (isSectionOpen('logs'))      refreshLogsGrid();
		});
	}
}

document.addEventListener('DOMContentLoaded', initCollapsibleSections);
