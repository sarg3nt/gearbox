let currentBoxID = '';
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
		currentBoxID = serverInput.value;
	}

	// Bind snapshot button events
	document.querySelectorAll('.snapshot-restore-btn').forEach(btn => {
		btn.addEventListener('click', () => restoreSnapshot(btn.dataset.snapshotId));
	});
	document.querySelectorAll('.snapshot-delete-btn').forEach(btn => {
		btn.addEventListener('click', () => deleteSnapshot(btn.dataset.snapshotId));
	});

	// Bind pipx button events
	document.querySelectorAll('.pipx-upgrade-btn').forEach(btn => {
		btn.addEventListener('click', () => upgradePipxPackage(btn.dataset.packageName));
	});
	document.querySelectorAll('.pipx-uninstall-btn').forEach(btn => {
		btn.addEventListener('click', () => uninstallPipxPackage(btn.dataset.packageName));
	});

	// Bind package info button events
	document.querySelectorAll('.package-info-btn').forEach(btn => {
		btn.addEventListener('click', () => showPackageInfo(btn));
	});

	// Initialize SSE connection for real-time apt events
	// Delay slightly to ensure currentBoxID is set
	setTimeout(initSSE, 100);
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
	if (!currentBoxID) {
		console.warn('SSE: No server ID available, skipping initialization');
		return;
	}

	if (sseConnection) {
		sseConnection.close();
	}

	const sseUrl = `/api/events?server=${currentBoxID}`;
	sseConnection = new EventSource(sseUrl);

	sseConnection.onopen = function() {
		console.log('SSE: Connection established');
	};

	sseConnection.onmessage = function(e) {
		try {
			const event = JSON.parse(e.data);
			// Dispatch to registered handlers
			if (eventHandlers[event.type]) {
				eventHandlers[event.type].forEach(handler => {
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
	};

	sseConnection.onerror = function(e) {
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

function switchBox(boxID) {
	window.location.href = '/os-updates?server=' + boxID;
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
		// This call runs apt update on the server and waits for it to complete
		const response = await fetch('/api/os-updates/check?server=' + currentBoxID, { method: 'POST' });
		if (!response.ok) throw new Error('Failed to check for updates');

		const data = await response.json();
		showToast('Update check complete. Found ' + data.total_updates + ' updates (' + data.security_updates + ' security).', 'success');

		// Reload page to show updated package list
		window.location.reload();
	} catch (err) {
		showToast('Failed to check for updates: ' + err.message, 'error');
		if (btn) {
			btn.disabled = false;
			btn.innerHTML = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"></path></svg> Check for Updates';
		}
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
				const response = await fetch('/api/os-updates/install?server=' + currentBoxID + '&stream=true', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({})
				});
				if (!response.ok) {
					const errorData = await response.json().catch(() => ({}));
					throw new Error(errorData.error || 'Failed to start update installation');
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
				const response = await fetch('/api/os-updates/install?server=' + currentBoxID + '&stream=true', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ security_only: true })
				});
				if (!response.ok) {
					const errorData = await response.json().catch(() => ({}));
					throw new Error(errorData.error || 'Failed to start security updates installation');
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
				const response = await fetch('/api/os-updates/install?server=' + currentBoxID + '&stream=true', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ packages: packages })
				});
				if (!response.ok) {
					const errorData = await response.json().catch(() => ({}));
					throw new Error(errorData.error || 'Failed to start package installation');
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
		const response = await fetch('/api/os-updates/snapshots?server=' + currentBoxID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ reason: reason })
		});
		if (!response.ok) throw new Error('Failed to create snapshot');
		showToast('Snapshot created', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		showToast('Failed to create snapshot: ' + err.message, 'error');
	}
}

function restoreSnapshot(snapshotID) {
	showConfirmModal({
		title: 'Restore Snapshot',
		message: 'Restore system to snapshot ' + snapshotID + '? This will downgrade packages to their previous versions.',
		type: 'warning',
		confirmText: 'Restore',
		onConfirm: async () => {
			try {
				const response = await fetch('/api/os-updates/snapshots/restore?server=' + currentBoxID, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ snapshot_id: snapshotID })
				});
				if (!response.ok) throw new Error('Failed to restore snapshot');
				showToast('Snapshot restored', 'success');
				setTimeout(() => window.location.reload(), 3000);
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
			try {
				const response = await fetch('/api/os-updates/snapshots/' + snapshotID + '?server=' + currentBoxID, {
					method: 'DELETE'
				});
				if (!response.ok) throw new Error('Failed to delete snapshot');
				showToast('Snapshot deleted', 'success');
				setTimeout(() => window.location.reload(), 1000);
			} catch (err) {
				showToast('Failed to delete snapshot: ' + err.message, 'error');
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
		const response = await fetch('/api/os-updates/reboot?server=' + currentBoxID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ when: when })
		});
		if (!response.ok) throw new Error('Failed to schedule reboot');
		const data = await response.json();
		showToast(data.message || 'Reboot scheduled', 'success');
	} catch (err) {
		showToast('Failed to schedule reboot: ' + err.message, 'error');
	}
}

// Pipx functions
function showPipxInstallModal() {
	document.getElementById('pipx-install-modal').classList.remove('hidden');
	document.getElementById('pipx-package-name').focus();
}

function hidePipxInstallModal() {
	document.getElementById('pipx-install-modal').classList.add('hidden');
	document.getElementById('pipx-package-name').value = '';
}

async function confirmPipxInstall() {
	const name = document.getElementById('pipx-package-name').value.trim();
	if (!name) {
		showToast('Please enter a package name', 'warning');
		return;
	}

	hidePipxInstallModal();

	try {
		const response = await fetch('/api/os-updates/pipx/install?server=' + currentBoxID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name: name })
		});
		if (!response.ok) throw new Error('Failed to install package');
		showToast('Package ' + name + ' installed', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		showToast('Failed to install package: ' + err.message, 'error');
	}
}

async function upgradePipxPackage(name) {
	try {
		const response = await fetch('/api/os-updates/pipx/upgrade?server=' + currentBoxID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name: name })
		});
		if (!response.ok) throw new Error('Failed to upgrade package');
		showToast('Package ' + name + ' upgraded', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		showToast('Failed to upgrade package: ' + err.message, 'error');
	}
}

async function upgradeAllPipx() {
	try {
		const response = await fetch('/api/os-updates/pipx/upgrade?server=' + currentBoxID, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({})
		});
		if (!response.ok) throw new Error('Failed to upgrade packages');
		showToast('All pipx packages upgraded', 'success');
		setTimeout(() => window.location.reload(), 1000);
	} catch (err) {
		showToast('Failed to upgrade packages: ' + err.message, 'error');
	}
}

function uninstallPipxPackage(name) {
	showConfirmModal({
		title: 'Uninstall Package',
		message: 'Uninstall ' + name + '? This will remove the package and its virtual environment.',
		type: 'danger',
		confirmText: 'Uninstall',
		onConfirm: async () => {
			try {
				const response = await fetch('/api/os-updates/pipx/uninstall?server=' + currentBoxID, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ name: name })
				});
				if (!response.ok) throw new Error('Failed to uninstall package');
				showToast('Package ' + name + ' uninstalled', 'success');
				setTimeout(() => window.location.reload(), 1000);
			} catch (err) {
				showToast('Failed to uninstall package: ' + err.message, 'error');
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

	title.textContent = options.title || 'Confirm';
	message.textContent = options.message || 'Are you sure?';
	actionBtn.textContent = options.confirmText || 'Confirm';

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
		hideConfirmModal();
		if (confirmCallback) confirmCallback();
	};

	modal.classList.remove('hidden');
}

function hideConfirmModal() {
	document.getElementById('confirm-modal').classList.add('hidden');
	confirmCallback = null;
}

// Toast functions
let toastTimeout = null;

function showToast(message, type) {
	const toast = document.getElementById('toast');
	const content = document.getElementById('toast-content');
	const messageEl = document.getElementById('toast-message');

	content.className = 'px-4 py-3 rounded-lg shadow-lg flex items-center gap-3';
	if (type === 'success') {
		content.classList.add('bg-green-100', 'dark:bg-green-900/30', 'text-green-700', 'dark:text-green-400');
	} else if (type === 'error') {
		content.classList.add('bg-red-100', 'dark:bg-red-900/30', 'text-red-700', 'dark:text-red-400');
	} else if (type === 'warning') {
		content.classList.add('bg-yellow-100', 'dark:bg-yellow-900/30', 'text-yellow-700', 'dark:text-yellow-400');
	} else {
		content.classList.add('bg-blue-100', 'dark:bg-blue-900/30', 'text-blue-700', 'dark:text-blue-400');
	}

	messageEl.textContent = message;
	toast.classList.remove('hidden');

	if (toastTimeout) clearTimeout(toastTimeout);
	toastTimeout = setTimeout(hideToast, 5000);
}

function hideToast() {
	document.getElementById('toast').classList.add('hidden');
	if (toastTimeout) {
		clearTimeout(toastTimeout);
		toastTimeout = null;
	}
}

// Terminal modal functions
function openTerminalModal(operationId, title, allPackages = true) {
	currentOperationID = operationId;
	terminalLineCount = 0;

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
}

function closeTerminalModal() {
	const modal = document.getElementById('terminal-modal');
	modal.classList.add('hidden');

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

	const line = event.data.line || '';
	const stream = event.data.stream || 'stdout';
	appendTerminalLine(line, stream);
}

function handleAptCompleted(event) {
	if (!event.data) return;
	const opId = event.data.operation_id;
	if (opId !== currentOperationID) return;

	setTerminalStatus('completed');
	appendTerminalLine('\nOperation completed successfully.', 'info');
}

function handleAptFailed(event) {
	if (!event.data) return;
	const opId = event.data.operation_id;
	if (opId !== currentOperationID) return;

	setTerminalStatus('failed');
	const errorMsg = event.data.error || 'Unknown error';
	appendTerminalLine('\nOperation failed: ' + errorMsg, 'stderr');
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
