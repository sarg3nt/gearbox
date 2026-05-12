	function filterLogSources(query) {
		const items = document.querySelectorAll('.log-source-item');
		const lowerQuery = query.toLowerCase();
		items.forEach(item => {
			const name = item.dataset.name.toLowerCase();
			const display = item.dataset.display.toLowerCase();
			if (name.includes(lowerQuery) || display.includes(lowerQuery)) {
				item.style.display = '';
			} else {
				item.style.display = 'none';
			}
		});
	}

	// Apply enabled/disabled visual + hidden-input state to a single toggle row.
	function applyLogSourceState(button, enabled) {
		const container = button.closest('.log-source-item');
		const logInput = container.querySelector('input[name="log_sources"]');
		const enabledInput = container.querySelector('input[name^="log_enabled_"]');
		enabledInput.value = enabled ? 'true' : 'false';
		logInput.disabled = !enabled;

		if (enabled) {
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
		button.setAttribute('aria-checked', enabled ? 'true' : 'false');
	}

	function collectEnabledLogSources() {
		return Array.from(document.querySelectorAll('input[name="log_sources"]'))
			.filter(input => !input.disabled)
			.map(input => input.value);
	}

	async function saveLogSourcesNow() {
		const root = document.getElementById('logs-config-root');
		if (!root) return;
		const url = root.dataset.saveUrl;
		const params = new URLSearchParams();
		collectEnabledLogSources().forEach(name => params.append('log_sources', name));

		const response = await fetch(url, {
			method: 'POST',
			headers: {
				'Accept': 'application/json',
				'Content-Type': 'application/x-www-form-urlencoded'
			},
			body: params.toString()
		});
		if (!response.ok) {
			let msg = 'Save failed (' + response.status + ')';
			try { const data = await response.json(); if (data && data.error) msg = data.error; } catch (e) {}
			throw new Error(msg);
		}
		return response.json();
	}

	async function toggleLogSource(button, sourceName) {
		const container = button.closest('.log-source-item');
		const enabledInput = container.querySelector('input[name^="log_enabled_"]');
		const priorEnabled = enabledInput.value === 'true';
		applyLogSourceState(button, !priorEnabled);

		try {
			await saveLogSourcesNow();
			if (window.showToast) window.showToast('Log sources saved', 'success', 2000);
		} catch (err) {
			applyLogSourceState(button, priorEnabled);
			if (window.showToast) window.showToast(err.message || 'Save failed', 'error');
		}
	}

	async function toggleAllLogs(enable) {
		const items = Array.from(document.querySelectorAll('.log-source-item:not([style*="display: none"])'));
		// Snapshot prior state per button so we can revert on failure.
		const snapshot = items.map(item => {
			const button = item.querySelector('button[role="switch"]');
			const enabledInput = item.querySelector('input[name^="log_enabled_"]');
			return { button, wasEnabled: enabledInput.value === 'true' };
		});

		snapshot.forEach(({ button, wasEnabled }) => {
			if (wasEnabled !== enable) applyLogSourceState(button, enable);
		});

		try {
			await saveLogSourcesNow();
			if (window.showToast) {
				window.showToast(enable ? 'All log sources enabled' : 'All log sources disabled', 'success', 2000);
			}
		} catch (err) {
			snapshot.forEach(({ button, wasEnabled }) => applyLogSourceState(button, wasEnabled));
			if (window.showToast) window.showToast(err.message || 'Save failed', 'error');
		}
	}
