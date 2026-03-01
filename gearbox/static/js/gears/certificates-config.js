function toggleCertbotEnabled(button) {
	const input = document.getElementById('certbot-enabled-input');
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

	// Toggle certbot options visibility
	toggleCertbotOptions(newValue);
}

function toggleCertbotOptions(enabled) {
	const options = document.getElementById('certbot-options');
	if (enabled) {
		options.classList.remove('hidden');
	} else {
		options.classList.add('hidden');
	}
}
