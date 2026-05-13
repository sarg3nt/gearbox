	// Services gear config — every toggle saves immediately (issue #71 item 3).
	// The legacy "Save Configuration" submit button has been removed; this file
	// owns the persistence flow now.

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

	// In-flight save tracking. We coalesce overlapping saves: only one POST
	// at a time, and if changes pile up while one is flying, queue exactly
	// one re-save with the latest state when it returns. Avoids stomping
	// rapid toggles or sending N parallel requests on Enable All.
	let saveInFlight = false;
	let savePending = false;

	function getActionURL() {
		const root = document.getElementById('services-config-root');
		return root ? root.dataset.action : '';
	}

	function gatherFormData() {
		const data = new FormData();
		document.querySelectorAll('#services-grid .service-item').forEach(item => {
			const enabledInput = item.querySelector('input[name^="service_enabled_"]');
			if (enabledInput && enabledInput.value === 'true') {
				const name = item.dataset.name;
				if (name) data.append('services', name);
			}
		});
		const showAll = document.getElementById('services-show-all');
		if (showAll && showAll.checked) {
			data.append('show_all', 'on');
		}
		return data;
	}

	function autoSave() {
		if (saveInFlight) {
			savePending = true;
			return;
		}
		const url = getActionURL();
		if (!url) return;

		saveInFlight = true;
		fetch(url, {
			method: 'POST',
			headers: { 'Accept': 'application/json' },
			body: gatherFormData(),
			credentials: 'same-origin',
		})
			.then(r => r.json().catch(() => ({ success: r.ok })))
			.then(data => {
				if (data && data.success === false && window.showToast) {
					window.showToast(data.error || 'Failed to save services', 'error', 4000);
				}
			})
			.catch(err => {
				console.error('services auto-save failed', err);
				if (window.showToast) {
					window.showToast('Failed to save services', 'error', 4000);
				}
			})
			.finally(() => {
				saveInFlight = false;
				if (savePending) {
					savePending = false;
					autoSave();
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
		autoSave();
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
		autoSave();
	}

	document.addEventListener('DOMContentLoaded', function () {
		const showAll = document.getElementById('services-show-all');
		if (showAll) {
			showAll.addEventListener('change', autoSave);
		}
	});
