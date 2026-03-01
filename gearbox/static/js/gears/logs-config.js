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

	function toggleLogSource(button, sourceName) {
		const container = button.closest('.log-source-item');
		const logInput = container.querySelector('input[name="log_sources"]');
		const enabledInput = container.querySelector('input[name^="log_enabled_"]');
		const isEnabled = enabledInput.value === 'true';
		const newValue = !isEnabled;

		// Update hidden inputs
		enabledInput.value = newValue ? 'true' : 'false';
		logInput.disabled = !newValue;

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
	}

	function toggleAllLogs(enable) {
		document.querySelectorAll('.log-source-item:not([style*="display: none"])').forEach(item => {
			const logInput = item.querySelector('input[name="log_sources"]');
			const enabledInput = item.querySelector('input[name^="log_enabled_"]');
			const button = item.querySelector('button[role="switch"]');
			if (!logInput || !enabledInput || !button) return;

			const isCurrentlyEnabled = enabledInput.value === 'true';
			if (isCurrentlyEnabled !== enable) {
				enabledInput.value = enable ? 'true' : 'false';
				logInput.disabled = !enable;
				if (enable) {
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
				button.setAttribute('aria-checked', enable ? 'true' : 'false');
			}
		});
	}
