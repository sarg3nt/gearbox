function toggleBooleanField(button, inputName) {
	const input = document.getElementById(inputName);
	const isEnabled = input.value === 'true';
	const newValue = !isEnabled;

	input.value = newValue ? 'true' : 'false';
	button.setAttribute('aria-checked', newValue ? 'true' : 'false');

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

	// Show/hide suppress minutes config
	if (inputName === 'suppress_after_ack_value') {
		const suppressConfig = document.getElementById('suppress-minutes-config');
		if (suppressConfig) {
			if (newValue) {
				suppressConfig.classList.remove('hidden');
			} else {
				suppressConfig.classList.add('hidden');
			}
		}
	}
}
