function toggleOSUpdatesField(button, inputName) {
	const input = document.getElementById(inputName);
	const currentValue = input.value === 'true';
	const newValue = !currentValue;

	input.value = newValue.toString();
	button.setAttribute('aria-checked', newValue.toString());

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
}
