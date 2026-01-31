// Calculate password entropy (simplified client-side version)
function calculateEntropy(password) {
	if (!password) return 0;

	let charsetSize = 0;
	if (/[a-z]/.test(password)) charsetSize += 26;
	if (/[A-Z]/.test(password)) charsetSize += 26;
	if (/[0-9]/.test(password)) charsetSize += 10;
	if (/[^a-zA-Z0-9]/.test(password)) charsetSize += 32;

	if (charsetSize === 0) return 0;
	return password.length * Math.log2(charsetSize);
}

// Common weak passwords to penalize
const commonPasswords = [
	'password', 'password123', 'password1234', '123456789012',
	'qwertyuiop', 'qwerty123456', 'admin123456', 'letmein12345',
	'welcome12345', 'changeme1234', 'iloveyou1234', 'trustno1234'
];

function getStrengthInfo(password) {
	if (!password || password.length === 0) {
		return { score: 0, label: '', color: 'bg-gray-300', meetsMinimum: false };
	}

	// Check minimum length
	if (password.length < 8) {
		return { score: 10, label: 'Too short', color: 'bg-red-500', meetsMinimum: false };
	}

	// Check for common passwords
	if (commonPasswords.includes(password.toLowerCase())) {
		return { score: 15, label: 'Too common', color: 'bg-red-500', meetsMinimum: false };
	}

	const entropy = calculateEntropy(password);

	// Map entropy to strength
	// Minimum requirement is 50 bits
	if (entropy < 30) {
		return { score: 20, label: 'Weak', color: 'bg-red-500', meetsMinimum: false };
	} else if (entropy < 40) {
		return { score: 35, label: 'Fair', color: 'bg-orange-500', meetsMinimum: false };
	} else if (entropy < 50) {
		return { score: 50, label: 'Moderate', color: 'bg-yellow-500', meetsMinimum: false };
	} else if (entropy < 60) {
		return { score: 70, label: 'Good', color: 'bg-lime-500', meetsMinimum: true };
	} else if (entropy < 70) {
		return { score: 85, label: 'Strong', color: 'bg-green-500', meetsMinimum: true };
	} else {
		return { score: 100, label: 'Excellent', color: 'bg-green-600', meetsMinimum: true };
	}
}

function updateStrengthMeter(fieldId) {
	const input = document.getElementById(fieldId);
	const bar = document.getElementById(fieldId + '_strength_bar');
	const text = document.getElementById(fieldId + '_strength_text');
	const check = document.getElementById(fieldId + '_strength_check');

	if (!input || !bar || !text || !check) return;

	const strength = getStrengthInfo(input.value);

	// Update bar width and color
	bar.style.width = strength.score + '%';
	bar.className = 'h-full transition-all duration-300 rounded-full ' + strength.color;

	// Update text
	text.textContent = strength.label;
	text.className = 'text-xs ' + (strength.meetsMinimum ? 'text-green-600 dark:text-green-400' : 'text-gray-500 dark:text-gray-400');

	// Show/hide checkmark
	if (strength.meetsMinimum) {
		check.classList.remove('hidden');
	} else {
		check.classList.add('hidden');
	}
}

function initPasswordStrengthMeter(fieldId) {
	const input = document.getElementById(fieldId);
	if (input) {
		input.addEventListener('input', function() {
			updateStrengthMeter(fieldId);
		});
		// Initial update
		updateStrengthMeter(fieldId);
	}
}
