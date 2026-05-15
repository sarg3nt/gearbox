/**
 * Shared Chart.js configuration and utilities for Gearbox chart widgets.
 * Loaded as a global script (not ES6 module). All functions are namespaced
 * under window.GearboxCharts to avoid polluting the global scope.
 */
(function() {
	'use strict';

	if (!window.GearboxCharts) {
		window.GearboxCharts = {};
	}

	/**
	 * Get theme-aware color palette for charts
	 */
	window.GearboxCharts.getChartColors = function() {
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
	};

	/**
	 * Get zoom/pan configuration for Chart.js zoomPlugin
	 */
	window.GearboxCharts.getZoomOptions = function(onZoomComplete) {
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
	};

	/**
	 * Create Chart.js options with standard Gearbox styling
	 */
	window.GearboxCharts.createChartOptions = function(minY, onZoomComplete) {
		var colors = window.GearboxCharts.getChartColors();
		return {
			responsive: true,
			maintainAspectRatio: false,
			plugins: {
				legend: {
					labels: { color: colors.text }
				},
				zoom: window.GearboxCharts.getZoomOptions(onZoomComplete)
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
	};

	/**
	 * Create Chart.js options with integer Y-axis (for error counts, etc.)
	 */
	window.GearboxCharts.createIntegerYAxisOptions = function(onZoomComplete) {
		var colors = window.GearboxCharts.getChartColors();
		return {
			responsive: true,
			maintainAspectRatio: false,
			plugins: {
				legend: {
					labels: { color: colors.text }
				},
				zoom: window.GearboxCharts.getZoomOptions(onZoomComplete)
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
						// precision: 0 lets Chart.js auto-pick the step size
						// but forces the result to integer values. Avoids the
						// `scales.y.ticks.stepSize: 1 would result generating
						// up to N ticks. Limiting to 1000.` console warning
						// that fires when stepSize: 1 meets a data range in
						// the thousands (HAProxy request counters routinely
						// hit ~8K req/window, and Chart.js can't draw 8K
						// ticks). The callback below stays as a belt-and-
						// braces filter in case a fractional value still
						// slips through.
						precision: 0,
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
	};

	/**
	 * Create Chart.js options with fixed 0-100 Y-axis (for percentages)
	 */
	window.GearboxCharts.createPercentageYAxisOptions = function(onZoomComplete) {
		var colors = window.GearboxCharts.getChartColors();
		var baseOptions = window.GearboxCharts.createChartOptions(0, onZoomComplete);
		return Object.assign({}, baseOptions, {
			scales: {
				x: baseOptions.scales.x,
				y: {
					min: 0,
					max: 100,
					ticks: { color: colors.text },
					grid: { color: colors.grid }
				}
			}
		});
	};

	/**
	 * Format timestamp as HH:MM time string
	 */
	window.GearboxCharts.formatTime = function(timestamp) {
		var date = new Date(timestamp);
		return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	};

	/**
	 * Downsample data array by factor (keep every Nth point)
	 */
	window.GearboxCharts.downsampleData = function(data, factor) {
		if (factor <= 1 || data.length <= 20) return data;
		var result = [];
		for (var i = 0; i < data.length; i += factor) {
			result.push(data[i]);
		}
		if (result[result.length - 1] !== data[data.length - 1]) {
			result.push(data[data.length - 1]);
		}
		return result;
	};

	/**
	 * Create line dataset configuration with Gearbox defaults
	 */
	window.GearboxCharts.createLineDataset = function(label, data, color) {
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
	};

	/**
	 * Toggle fullscreen mode for a chart card
	 */
	window.GearboxCharts.toggleFullscreen = function(cardId, onResize) {
		var card = document.getElementById(cardId);
		if (!card) return;

		var grid = card.closest('.grid') || card.closest('[id$="-grid"]');
		if (!grid) return;

		if (card.classList.contains('fullscreen')) {
			card.classList.remove('fullscreen');
			grid.querySelectorAll('.chart-card').forEach(function(c) {
				if (c.id !== cardId) {
					c.classList.remove('hidden');
				}
			});
			window.GearboxCharts._currentFullscreen = null;
			if (onResize) setTimeout(onResize, 50);
		} else {
			card.classList.add('fullscreen');
			grid.querySelectorAll('.chart-card').forEach(function(c) {
				if (c.id !== cardId) {
					c.classList.add('hidden');
				}
			});
			window.GearboxCharts._currentFullscreen = cardId;
			if (onResize) setTimeout(onResize, 100);
		}
	};

	// Global chart instance storage
	if (!window.chartInstances) {
		window.chartInstances = {};
	}
})();
