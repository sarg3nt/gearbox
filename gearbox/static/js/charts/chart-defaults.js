/**
 * Shared Chart.js configuration and utilities for Gearbox chart widgets
 */

/**
 * Get theme-aware color palette for charts
 * @returns {Object} Color definitions for current theme
 */
export function getChartColors() {
	const isDark = document.documentElement.classList.contains('dark');
	return {
		text: isDark ? '#e2e8f0' : '#374151',
		grid: isDark ? '#475569' : '#e5e7eb',
		primary: '#3b82f6',
		secondary: '#10b981',
		warning: '#f59e0b',
		danger: '#ef4444',
		purple: '#8b5cf6',
		cyan: '#06b6d4'
	};
}

/**
 * Get zoom/pan configuration for Chart.js zoomPlugin
 * @param {Function} onZoomComplete - Callback when zoom completes
 * @returns {Object} Zoom plugin configuration
 */
export function getZoomOptions(onZoomComplete) {
	return {
		zoom: {
			drag: {
				enabled: true,
				backgroundColor: 'rgba(59, 130, 246, 0.1)',
				borderColor: 'rgba(59, 130, 246, 0.5)',
				borderWidth: 1
			},
			mode: 'x',
			onZoomComplete: onZoomComplete || function() {}
		},
		pan: {
			enabled: true,
			mode: 'x',
			modifierKey: 'shift'
		}
	};
}

/**
 * Create Chart.js options with standard Gearbox styling
 * @param {number} minY - Minimum Y-axis value (optional)
 * @param {Function} onZoomComplete - Callback when zoom completes (optional)
 * @returns {Object} Chart.js options object
 */
export function createChartOptions(minY = undefined, onZoomComplete = null) {
	const colors = getChartColors();
	return {
		responsive: true,
		maintainAspectRatio: false,
		plugins: {
			legend: {
				labels: { color: colors.text }
			},
			zoom: getZoomOptions(onZoomComplete)
		},
		scales: {
			x: {
				ticks: {
					color: colors.text,
					maxTicksLimit: 8
				},
				grid: { color: colors.grid }
			},
			y: {
				min: minY,
				ticks: { color: colors.text },
				grid: { color: colors.grid }
			}
		}
	};
}

/**
 * Create Chart.js options with integer Y-axis (for error counts, etc.)
 * @param {Function} onZoomComplete - Callback when zoom completes (optional)
 * @returns {Object} Chart.js options object
 */
export function createIntegerYAxisOptions(onZoomComplete = null) {
	const colors = getChartColors();
	return {
		responsive: true,
		maintainAspectRatio: false,
		plugins: {
			legend: {
				labels: { color: colors.text }
			},
			zoom: getZoomOptions(onZoomComplete)
		},
		scales: {
			x: {
				ticks: {
					color: colors.text,
					maxTicksLimit: 8
				},
				grid: { color: colors.grid }
			},
			y: {
				min: 0,
				ticks: {
					color: colors.text,
					stepSize: 1,
					callback: function(value) {
						if (Math.floor(value) === value) {
							return value;
						}
					}
				},
				grid: { color: colors.grid }
			}
		}
	};
}

/**
 * Create Chart.js options with fixed 0-100 Y-axis (for percentages)
 * @param {Function} onZoomComplete - Callback when zoom completes (optional)
 * @returns {Object} Chart.js options object
 */
export function createPercentageYAxisOptions(onZoomComplete = null) {
	const colors = getChartColors();
	const baseOptions = createChartOptions(0, onZoomComplete);
	return {
		...baseOptions,
		scales: {
			...baseOptions.scales,
			y: {
				min: 0,
				max: 100,
				ticks: { color: colors.text },
				grid: { color: colors.grid }
			}
		}
	};
}

/**
 * Format timestamp as HH:MM time string
 * @param {string|Date} timestamp - ISO timestamp or Date object
 * @returns {string} Formatted time string
 */
export function formatTime(timestamp) {
	const date = new Date(timestamp);
	return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

/**
 * Downsample data array by factor (keep every Nth point)
 * @param {Array} data - Data array to downsample
 * @param {number} factor - Downsampling factor (1 = no downsampling)
 * @returns {Array} Downsampled data array
 */
export function downsampleData(data, factor) {
	if (factor <= 1 || data.length <= 20) return data;
	const result = [];
	for (let i = 0; i < data.length; i += factor) {
		result.push(data[i]);
	}
	// Always include the last point
	if (result[result.length - 1] !== data[data.length - 1]) {
		result.push(data[data.length - 1]);
	}
	return result;
}

/**
 * Update chart data in place without animation
 * @param {Chart} chart - Chart.js instance
 * @param {Array} sampledData - Downsampled data array
 * @param {Array<Array>} datasets - Array of dataset arrays
 */
export function updateChartData(chart, sampledData, datasets) {
	chart.data.labels = sampledData.map(d => formatTime(d.collected_at));
	datasets.forEach((data, index) => {
		if (chart.data.datasets[index]) {
			chart.data.datasets[index].data = data;
		}
	});
	chart.update('none'); // 'none' disables animation
}

/**
 * Create line dataset configuration with Gearbox defaults
 * @param {string} label - Dataset label
 * @param {Array} data - Data array
 * @param {string} color - Line color
 * @returns {Object} Chart.js dataset configuration
 */
export function createLineDataset(label, data, color) {
	return {
		label: label,
		data: data,
		borderColor: color,
		backgroundColor: 'transparent',
		borderWidth: 1.5,
		pointRadius: 0,
		pointHoverRadius: 3,
		tension: 0.4
	};
}
