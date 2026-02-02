/**
 * Dashboard-level controls for chart widgets
 * Manages time range, resolution, and other shared settings across all charts
 */

/**
 * Get resolution label from slider value
 * @param {number} value - Slider value (1-10)
 * @returns {string} Human-readable resolution label
 */
export function getResolutionLabel(value) {
	// Slider is inverted: 1 = lowest resolution, 10 = highest resolution
	const labels = {
		1: 'Lowest',
		2: 'Lowest',
		3: 'Low',
		4: 'Low',
		5: 'Medium',
		6: 'Medium',
		7: 'High',
		8: 'High',
		9: 'Highest',
		10: 'Highest'
	};
	return labels[value] || 'Medium';
}

/**
 * Update resolution label text
 * @param {string} sliderId - ID of resolution slider element (default: 'resolution-slider')
 * @param {string} labelId - ID of label element (default: 'resolution-label')
 */
export function updateResolutionLabel(sliderId = 'resolution-slider', labelId = 'resolution-label') {
	const slider = document.getElementById(sliderId);
	const label = document.getElementById(labelId);
	if (slider && label) {
		label.textContent = getResolutionLabel(parseInt(slider.value));
	}
}

/**
 * Calculate downsampling factor from resolution slider value
 * @param {number} sliderValue - Slider value (1-10)
 * @returns {number} Downsampling factor (1-10)
 */
export function getDownsampleFactor(sliderValue = 5) {
	// Invert: slider 10 (highest res) = factor 1 (no downsampling)
	//         slider 1 (lowest res) = factor 10 (max downsampling)
	return 11 - sliderValue;
}

/**
 * Get current dashboard settings from UI controls
 * @param {Object} options - Options for element IDs
 * @param {string} options.serverSelectId - ID of server select element (default: 'server-select')
 * @param {string} options.defaultServerInputId - ID of default server input (default: 'default-server-id')
 * @param {string} options.hoursSelectId - ID of hours select element (default: 'hours-select')
 * @param {string} options.resolutionSliderId - ID of resolution slider (default: 'resolution-slider')
 * @returns {Object} Dashboard settings
 */
export function getDashboardSettings(options = {}) {
	const {
		serverSelectId = 'server-select',
		defaultServerInputId = 'default-server-id',
		hoursSelectId = 'hours-select',
		resolutionSliderId = 'resolution-slider'
	} = options;

	// Get server ID - handles both multi-server (select) and single-server (hidden input) modes
	let serverID = '';
	const selector = document.getElementById(serverSelectId);
	if (selector) {
		serverID = selector.value;
	} else {
		const defaultServer = document.getElementById(defaultServerInputId);
		if (defaultServer) {
			serverID = defaultServer.value;
		}
	}

	// Get time range
	const hoursSelect = document.getElementById(hoursSelectId);
	const hours = hoursSelect ? parseFloat(hoursSelect.value) : 24;

	// Get resolution
	const resolutionSlider = document.getElementById(resolutionSliderId);
	const resolution = resolutionSlider ? parseInt(resolutionSlider.value) : 5;
	const downsampleFactor = getDownsampleFactor(resolution);

	return {
		serverID,
		hours,
		resolution,
		downsampleFactor
	};
}

/**
 * Broadcast dashboard settings changed event
 * This triggers all chart widgets to reload with new settings
 * @param {Object} settings - Dashboard settings (optional, will fetch current if not provided)
 */
export function broadcastSettingsChanged(settings = null) {
	if (!settings) {
		settings = getDashboardSettings();
	}

	const event = new CustomEvent('dashboard:settings-changed', {
		detail: settings,
		bubbles: true
	});
	document.body.dispatchEvent(event);
}

/**
 * Setup dashboard controls event listeners
 * @param {Function} onChange - Callback when settings change
 * @param {Object} options - Options for element IDs
 */
export function setupDashboardControls(onChange, options = {}) {
	const {
		serverSelectId = 'server-select',
		hoursSelectId = 'hours-select',
		resolutionSliderId = 'resolution-slider'
	} = options;

	// Server selector
	const serverSelect = document.getElementById(serverSelectId);
	if (serverSelect) {
		serverSelect.addEventListener('change', () => {
			const settings = getDashboardSettings(options);
			broadcastSettingsChanged(settings);
			if (onChange) onChange(settings);
		});
	}

	// Hours selector
	const hoursSelect = document.getElementById(hoursSelectId);
	if (hoursSelect) {
		hoursSelect.addEventListener('change', () => {
			const settings = getDashboardSettings(options);
			broadcastSettingsChanged(settings);
			if (onChange) onChange(settings);
		});
	}

	// Resolution slider
	const resolutionSlider = document.getElementById(resolutionSliderId);
	if (resolutionSlider) {
		resolutionSlider.addEventListener('input', () => {
			updateResolutionLabel(resolutionSliderId);
		});
		resolutionSlider.addEventListener('change', () => {
			const settings = getDashboardSettings(options);
			broadcastSettingsChanged(settings);
			if (onChange) onChange(settings);
		});
	}
}

/**
 * Show reset zoom button
 * @param {string} buttonId - ID of reset zoom button (default: 'reset-zoom-btn')
 */
export function showResetZoomButton(buttonId = 'reset-zoom-btn') {
	const btn = document.getElementById(buttonId);
	if (btn) btn.classList.remove('hidden');
}

/**
 * Hide reset zoom button
 * @param {string} buttonId - ID of reset zoom button (default: 'reset-zoom-btn')
 */
export function hideResetZoomButton(buttonId = 'reset-zoom-btn') {
	const btn = document.getElementById(buttonId);
	if (btn) btn.classList.add('hidden');
}

/**
 * Broadcast reset zoom event
 * All chart widgets should listen for this and reset their zoom
 */
export function broadcastResetZoom() {
	const event = new CustomEvent('dashboard:reset-zoom', {
		bubbles: true
	});
	document.body.dispatchEvent(event);
	hideResetZoomButton();
}

/**
 * Setup reset zoom button listener
 * @param {string} buttonId - ID of reset zoom button (default: 'reset-zoom-btn')
 */
export function setupResetZoomButton(buttonId = 'reset-zoom-btn') {
	const btn = document.getElementById(buttonId);
	if (btn) {
		btn.addEventListener('click', broadcastResetZoom);
	}
}
