/**
 * Containers Page - Docker container and Compose stack management.
 * Handles listing, filtering, and performing actions on containers and stacks.
 */

'use strict';

let currentServerID = '';
let currentView = 'stacks'; // 'stacks' or 'all'
let autoRefreshEnabled = true;
let refreshInterval = null;
let allContainers = [];
let allStacks = [];
let containerStats = {}; // id -> stats
let searchFilter = '';

// ─── Initialisation ────────────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
	const serverEl = document.getElementById('current-server-id');
	currentServerID = serverEl ? serverEl.value : '';
	load();
	startAutoRefresh();
});

function load() {
	showLoading();
	Promise.all([loadRuntime(), loadContainers()])
		.catch(err => showError('Failed to load containers: ' + err.message))
		.finally(hideLoading);
}

async function loadRuntime() {
	const resp = await apiFetch('/api/v1/containers/runtime');
	if (!resp.available) {
		showNoRuntime();
		throw new Error('no runtime');
	}
}

async function loadContainers() {
	const resp = await apiFetch('/api/v1/containers/list?server=' + currentServerID);
	allContainers = resp.containers || [];
	buildStacks();
	render();
}

function buildStacks() {
	const stackMap = {};
	for (const c of allContainers) {
		if (!c.stack_name) continue;
		if (!stackMap[c.stack_name]) {
			stackMap[c.stack_name] = { name: c.stack_name, containers: [], running: 0 };
		}
		stackMap[c.stack_name].containers.push(c);
		if (c.state === 'running') stackMap[c.stack_name].running++;
	}
	allStacks = Object.values(stackMap);
}

// ─── Rendering ─────────────────────────────────────────────────────────────

function render() {
	hideLoading();
	hideError();
	hideNoRuntime();

	if (currentView === 'stacks') {
		renderStacksView();
	} else {
		renderAllContainersView();
	}
}

function renderStacksView() {
	document.getElementById('stacks-view').classList.remove('hidden');
	document.getElementById('all-containers-view').classList.add('hidden');

	const stacksList = document.getElementById('stacks-list');
	stacksList.innerHTML = '';

	const filtered = filterContainersList(allStacks.map(s => ({ ...s, _search: s.name })));

	if (filtered.length === 0 && allStacks.length > 0) {
		stacksList.innerHTML = '<p class="text-sm text-gray-500 dark:text-gray-400 text-center py-4">No stacks match your search.</p>';
	}

	for (const stack of filtered) {
		stacksList.appendChild(buildStackCard(stack));
	}

	// Ungrouped containers
	const ungrouped = allContainers.filter(c => !c.stack_name);
	const ungroupedSection = document.getElementById('ungrouped-section');
	if (ungrouped.length > 0) {
		ungroupedSection.classList.remove('hidden');
		renderContainerRows('ungrouped-tbody', ungrouped, false);
	} else {
		ungroupedSection.classList.add('hidden');
	}
}

function buildStackCard(stack) {
	const total = stack.containers.length;
	const running = stack.running;
	const statusColor = running === total ? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300'
		: running === 0 ? 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300'
		: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300';

	const card = document.createElement('div');
	card.className = 'bg-white dark:bg-slate-800 rounded-lg shadow border border-gray-100 dark:border-slate-700 overflow-hidden';
	card.dataset.stackName = stack.name;
	card.innerHTML = `
		<div class="flex items-center justify-between px-4 py-3 cursor-pointer hover:bg-gray-50 dark:hover:bg-slate-750"
		     onclick="toggleStackExpand('${escHtml(stack.name)}')">
			<div class="flex items-center gap-3">
				<svg class="w-4 h-4 text-gray-400 dark:text-gray-500 stack-expand-icon transition-transform"
				     fill="none" stroke="currentColor" viewBox="0 0 24 24">
					<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"/>
				</svg>
				<span class="font-semibold text-gray-900 dark:text-white">${escHtml(stack.name)}</span>
				<span class="text-xs px-2 py-0.5 rounded-full font-medium ${statusColor}">
					${running}/${total} running
				</span>
			</div>
			<div class="flex items-center gap-2">
				<button onclick="event.stopPropagation(); stackAction('${escHtml(stack.name)}', 'up')"
				        class="px-2 py-1 text-xs bg-green-600 hover:bg-green-700 text-white rounded transition-colors">Up</button>
				<button onclick="event.stopPropagation(); stackAction('${escHtml(stack.name)}', 'down')"
				        class="px-2 py-1 text-xs bg-red-600 hover:bg-red-700 text-white rounded transition-colors">Down</button>
				<button onclick="event.stopPropagation(); stackAction('${escHtml(stack.name)}', 'restart')"
				        class="px-2 py-1 text-xs bg-blue-600 hover:bg-blue-700 text-white rounded transition-colors">Restart</button>
			</div>
		</div>
		<div class="stack-body hidden border-t border-gray-100 dark:border-slate-700 overflow-x-auto">
			<table class="w-full text-sm">
				<thead>
					<tr class="bg-gray-50 dark:bg-slate-700/50 text-left">
						<th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Name</th>
						<th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Image</th>
						<th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Status</th>
						<th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">CPU</th>
						<th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Memory</th>
						<th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Ports</th>
						<th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Actions</th>
					</tr>
				</thead>
				<tbody id="stack-tbody-${escHtml(stack.name)}"></tbody>
			</table>
		</div>`;
	renderContainerRows('stack-tbody-' + stack.name, stack.containers, false);
	return card;
}

function renderAllContainersView() {
	document.getElementById('stacks-view').classList.add('hidden');
	document.getElementById('all-containers-view').classList.remove('hidden');

	const filtered = filterContainersList(allContainers.map(c => ({ ...c, _search: c.name + ' ' + c.stack_name })));
	renderContainerRows('all-containers-tbody', filtered, true);
}

function renderContainerRows(tbodyId, containers, showStack) {
	const tbody = document.getElementById(tbodyId);
	if (!tbody) return;
	tbody.innerHTML = '';

	if (containers.length === 0) {
		const tr = document.createElement('tr');
		tr.innerHTML = `<td colspan="${showStack ? 8 : 7}" class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400 text-center">No containers</td>`;
		tbody.appendChild(tr);
		return;
	}

	for (const c of containers) {
		tbody.appendChild(buildContainerRow(c, showStack));
	}
}

function buildContainerRow(c, showStack) {
	const stateColor = c.state === 'running'
		? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300'
		: c.state === 'paused' ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300'
		: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300';

	const stats = containerStats[c.id] || {};
	const cpu = stats.cpu_percent !== undefined ? stats.cpu_percent.toFixed(1) + '%' : '—';
	const mem = stats.memory_usage !== undefined ? formatBytes(stats.memory_usage) : '—';

	const ports = (c.ports || [])
		.filter(p => p.host_port > 0)
		.map(p => `<span class="text-xs text-blue-600 dark:text-blue-400">${p.host_port}:${p.container_port}/${p.protocol}</span>`)
		.join(' ') || '—';

	const stackCell = showStack
		? `<td class="px-4 py-2 text-gray-600 dark:text-gray-400">${escHtml(c.stack_name || '—')}</td>` : '';

	const tr = document.createElement('tr');
	tr.className = 'border-t border-gray-50 dark:border-slate-700/50 hover:bg-gray-50 dark:hover:bg-slate-750';
	tr.innerHTML = `
		<td class="px-4 py-2 font-medium text-gray-900 dark:text-white text-sm">${escHtml(c.name)}</td>
		<td class="px-4 py-2 text-gray-600 dark:text-gray-400 text-xs font-mono truncate max-w-xs">${escHtml(c.image)}</td>
		${stackCell}
		<td class="px-4 py-2">
			<span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium ${stateColor}">
				${escHtml(c.state || c.status)}
			</span>
		</td>
		<td class="px-4 py-2 text-sm text-gray-600 dark:text-gray-400">${cpu}</td>
		<td class="px-4 py-2 text-sm text-gray-600 dark:text-gray-400">${mem}</td>
		<td class="px-4 py-2">${ports}</td>
		<td class="px-4 py-2">
			<div class="flex items-center gap-1">
				${c.state === 'running'
					? `<button onclick="containerAction('${escHtml(c.id)}', 'stop', '${escHtml(c.name)}')" title="Stop"
					          class="p-1 text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400 transition-colors">
					     <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="1" stroke-width="2"/></svg>
					   </button>
					   <button onclick="containerAction('${escHtml(c.id)}', 'restart', '${escHtml(c.name)}')" title="Restart"
					          class="p-1 text-gray-500 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400 transition-colors">
					     <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581"/></svg>
					   </button>`
					: `<button onclick="containerAction('${escHtml(c.id)}', 'start', '${escHtml(c.name)}')" title="Start"
					          class="p-1 text-gray-500 hover:text-green-600 dark:text-gray-400 dark:hover:text-green-400 transition-colors">
					     <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
					   </button>`}
				<button onclick="openLogsPanel('${escHtml(c.id)}', '${escHtml(c.name)}')" title="Logs"
				        class="p-1 text-gray-500 hover:text-purple-600 dark:text-gray-400 dark:hover:text-purple-400 transition-colors">
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/></svg>
				</button>
			</div>
		</td>`;
	return tr;
}

// ─── Actions ───────────────────────────────────────────────────────────────

async function containerAction(id, action, name) {
	const method = action === 'remove' ? 'DELETE' : 'POST';
	const url = `/api/v1/containers/${id}/${action}?server=${currentServerID}`;

	if (action === 'remove') {
		showConfirm(
			`Remove ${name}?`,
			`This will permanently remove the container "${name}". Any unsaved data will be lost.`,
			async () => {
				await doContainerAction(method, url, name, action);
			}
		);
		return;
	}

	try {
		await doContainerAction(method, url, name, action);
	} catch (err) {
		showToast(`Failed to ${action} ${name}: ${err.message}`, 'error');
	}
}

async function doContainerAction(method, url, name, action) {
	const resp = await fetch(url, { method });
	if (!resp.ok) {
		const msg = await extractErrorMessage(resp);
		throw new Error(msg);
	}
	showToast(`Container "${name}" ${action}ed.`, 'success');
	await loadContainers();
}

async function stackAction(stackName, action) {
	// Stack actions are proxied through the dashboard to the agent
	// Phase 3 will implement this; for now show a coming-soon toast
	showToast(`Stack ${action} for "${stackName}" coming in Phase 3.`, 'info');
}

function openLogsPanel(id, name) {
	// Simple logs viewer: open in a modal or new panel (Phase 2 detail)
	showToast(`Logs for "${name}" coming soon.`, 'info');
}

// ─── Server switching ──────────────────────────────────────────────────────

function switchServer(serverID) {
	currentServerID = serverID;
	load();
}

// ─── View toggle ───────────────────────────────────────────────────────────

function setView(view) {
	currentView = view;
	document.getElementById('view-stacks-btn').className = view === 'stacks'
		? 'px-3 py-1.5 bg-blue-600 text-white text-sm'
		: 'px-3 py-1.5 bg-white dark:bg-slate-800 text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-slate-700 text-sm';
	document.getElementById('view-all-btn').className = view === 'all'
		? 'px-3 py-1.5 bg-blue-600 text-white text-sm'
		: 'px-3 py-1.5 bg-white dark:bg-slate-800 text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-slate-700 text-sm';
	render();
}

// ─── Stack expand/collapse ─────────────────────────────────────────────────

function toggleStackExpand(stackName) {
	const card = document.querySelector(`[data-stack-name="${CSS.escape(stackName)}"]`);
	if (!card) return;
	const body = card.querySelector('.stack-body');
	const icon = card.querySelector('.stack-expand-icon');
	const isOpen = !body.classList.contains('hidden');
	body.classList.toggle('hidden', isOpen);
	icon.style.transform = isOpen ? '' : 'rotate(90deg)';
}

// ─── Filter ────────────────────────────────────────────────────────────────

function filterContainers(value) {
	searchFilter = value.toLowerCase();
	render();
}

function filterContainersList(items) {
	if (!searchFilter) return items;
	return items.filter(item => {
		const searchable = (item._search || item.name || '').toLowerCase();
		return searchable.includes(searchFilter);
	});
}

// ─── Auto refresh ──────────────────────────────────────────────────────────

function startAutoRefresh() {
	stopAutoRefresh();
	if (autoRefreshEnabled) {
		refreshInterval = setInterval(() => {
			if (autoRefreshEnabled) loadContainers().catch(() => {});
		}, 10000);
	}
}

function stopAutoRefresh() {
	if (refreshInterval) {
		clearInterval(refreshInterval);
		refreshInterval = null;
	}
}

function toggleAutoRefresh() {
	autoRefreshEnabled = !autoRefreshEnabled;
	document.getElementById('auto-refresh-label').textContent =
		autoRefreshEnabled ? 'Auto-refresh: On' : 'Auto-refresh: Off';
	if (autoRefreshEnabled) {
		startAutoRefresh();
	} else {
		stopAutoRefresh();
	}
}

// ─── Confirm modal ─────────────────────────────────────────────────────────

let pendingConfirmAction = null;

function showConfirm(title, message, action) {
	pendingConfirmAction = action;
	document.getElementById('confirm-title').textContent = title;
	document.getElementById('confirm-message').textContent = message;
	document.getElementById('confirm-modal').classList.remove('hidden');
}

function closeConfirmModal() {
	pendingConfirmAction = null;
	document.getElementById('confirm-modal').classList.add('hidden');
}

document.getElementById('confirm-action-btn')?.addEventListener('click', async () => {
	closeConfirmModal();
	if (pendingConfirmAction) {
		try {
			await pendingConfirmAction();
		} catch (err) {
			showToast('Action failed: ' + err.message, 'error');
		}
	}
});

// ─── UI helpers ────────────────────────────────────────────────────────────

function showLoading() {
	document.getElementById('containers-loading')?.classList.remove('hidden');
	document.getElementById('stacks-view')?.classList.add('hidden');
	document.getElementById('all-containers-view')?.classList.add('hidden');
}

function hideLoading() {
	document.getElementById('containers-loading')?.classList.add('hidden');
}

function showError(msg) {
	hideLoading();
	document.getElementById('containers-error')?.classList.remove('hidden');
	const msgEl = document.getElementById('containers-error-msg');
	if (msgEl) msgEl.textContent = msg;
}

function hideError() {
	document.getElementById('containers-error')?.classList.add('hidden');
}

function showNoRuntime() {
	hideLoading();
	document.getElementById('containers-no-runtime')?.classList.remove('hidden');
}

function hideNoRuntime() {
	document.getElementById('containers-no-runtime')?.classList.add('hidden');
}

function showToast(message, type = 'info') {
	// Re-use existing global toast function if available
	if (typeof window.showToast === 'function') {
		window.showToast(message, type);
		return;
	}
	console.log(`[${type}] ${message}`);
}

// ─── Fetch helpers ─────────────────────────────────────────────────────────

async function apiFetch(url) {
	const resp = await fetch(url);
	if (!resp.ok) {
		const msg = await extractErrorMessage(resp);
		throw new Error(msg);
	}
	return resp.json();
}

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

// ─── Formatting ────────────────────────────────────────────────────────────

function formatBytes(bytes) {
	if (bytes < 1024) return bytes + ' B';
	if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
	if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(1) + ' MB';
	return (bytes / 1024 / 1024 / 1024).toFixed(2) + ' GB';
}

function escHtml(str) {
	if (!str) return '';
	return String(str)
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;')
		.replace(/'/g, '&#x27;');
}
