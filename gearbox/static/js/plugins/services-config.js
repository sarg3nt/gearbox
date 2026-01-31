	function filterServices(query) {
		const items = document.querySelectorAll('.service-item');
		const lowerQuery = query.toLowerCase();
		items.forEach(item => {
			const name = item.dataset.name.toLowerCase();
			if (name.includes(lowerQuery)) {
				item.style.display = '';
			} else {
				item.style.display = 'none';
			}
		});
	}

	function toggleServiceItem(button, serviceName) {
		const container = button.closest('.service-item');
		const serviceInput = container.querySelector('input[name="services"]');
		const enabledInput = container.querySelector('input[name^="service_enabled_"]');
		const isEnabled = enabledInput.value === 'true';
		const newValue = !isEnabled;

		// Update hidden inputs
		enabledInput.value = newValue ? 'true' : 'false';
		serviceInput.disabled = !newValue;

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

	function toggleAllServices(enable) {
		document.querySelectorAll('.service-item:not([style*="display: none"])').forEach(item => {
			const serviceInput = item.querySelector('input[name="services"]');
			const enabledInput = item.querySelector('input[name^="service_enabled_"]');
			const button = item.querySelector('button[role="switch"]');
			if (!serviceInput || !enabledInput || !button) return;

			const isCurrentlyEnabled = enabledInput.value === 'true';
			if (isCurrentlyEnabled !== enable) {
				enabledInput.value = enable ? 'true' : 'false';
				serviceInput.disabled = !enable;
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
