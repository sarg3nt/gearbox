// Toast notification system
// Provides global showToast(), dismissToast(), and dismissAllToasts() functions.
//
// Usage:
//   showToast('Saved!', 'success')
//   showToast('Oops', 'error', 8000)
//   showToast('Done', 'success', { onClose: () => location.reload(), duration: 3000 })
//   showToast('Choose', 'info', { persistent: true, buttons: [{ label: 'Undo', onClick: fn }] })
//   dismissToast(toastEl)
//   dismissAllToasts()

(function () {
	'use strict';

	// ── Configuration ──────────────────────────────────────────────────
	var DEFAULTS = {
		durations: { success: 5000, error: 8000, warning: 6000, info: 5000, default: 5000 },
		animationDuration: 300,
		progressBar: true,
		dragDismiss: true,
		position: 'top-right',
		maxToasts: 8,
		newestOnTop: true
	};

	var TYPE_CONFIG = {
		success: {
			bg: 'bg-green-600 dark:bg-green-500',
			text: 'text-white',
			icon: 'M5 13l4 4L19 7',
			progressBg: 'bg-green-300/40'
		},
		error: {
			bg: 'bg-red-600 dark:bg-red-500',
			text: 'text-white',
			icon: 'M6 18L18 6M6 6l12 12',
			progressBg: 'bg-red-300/40'
		},
		warning: {
			bg: 'bg-yellow-600 dark:bg-yellow-500',
			text: 'text-white',
			icon: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z',
			progressBg: 'bg-yellow-300/40'
		},
		info: {
			bg: 'bg-blue-600 dark:bg-blue-500',
			text: 'text-white',
			icon: 'M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
			progressBg: 'bg-blue-300/40'
		},
		default: {
			bg: 'bg-gray-800 dark:bg-gray-700',
			text: 'text-white',
			icon: 'M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z',
			progressBg: 'bg-gray-400/40'
		}
	};

	// ── Internal state ─────────────────────────────────────────────────
	var activeToasts = [];
	var toastIdCounter = 0;

	// ── Helpers ────────────────────────────────────────────────────────

	function escapeHtml(text) {
		var div = document.createElement('div');
		div.textContent = text;
		return div.innerHTML;
	}

	function getContainer() {
		return document.getElementById('toast-container');
	}

	// Resolve the flexible 3rd argument into a normalized options object.
	// Supports: number, options object, or undefined.
	function normalizeOptions(durationOrOpts, type) {
		var opts = {};
		if (durationOrOpts != null && typeof durationOrOpts === 'object') {
			opts = durationOrOpts;
		} else if (typeof durationOrOpts === 'number') {
			opts.duration = durationOrOpts;
		}
		// Apply type-specific default duration if not set
		if (opts.duration === undefined) {
			opts.duration = DEFAULTS.durations[type] || DEFAULTS.durations.default;
		}
		return opts;
	}

	// ── Icons (SVG paths) ──────────────────────────────────────────────

	var ICONS = {
		copy: 'M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z',
		check: 'M5 13l4 4L19 7',
		close: 'M6 18L18 6M6 6l12 12'
	};

	function svgIcon(pathD, extraClass) {
		return '<svg class="w-4 h-4' + (extraClass ? ' ' + extraClass : '') + '" fill="none" stroke="currentColor" viewBox="0 0 24 24">' +
			'<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="' + pathD + '"></path></svg>';
	}

	function svgIcon5(pathD) {
		return '<svg class="w-5 h-5 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">' +
			'<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="' + pathD + '"></path></svg>';
	}

	// ── Build toast DOM ────────────────────────────────────────────────

	function buildToast(message, type, opts) {
		var config = TYPE_CONFIG[type] || TYPE_CONFIG.default;
		var toast = document.createElement('div');
		toast.className = 'pointer-events-auto transform transition-all duration-300 ease-in-out translate-x-full opacity-0';
		toast.dataset.message = message;
		toast.dataset.toastId = String(++toastIdCounter);

		// Inner container
		var inner = document.createElement('div');
		inner.className = config.bg + ' ' + config.text +
			' rounded-lg shadow-lg min-w-[300px] max-w-md overflow-hidden select-none';

		// Content row
		var content = document.createElement('div');
		content.className = 'px-4 py-3 flex items-center space-x-3';

		// Type icon
		var iconSpan = document.createElement('span');
		iconSpan.className = 'flex-shrink-0';
		iconSpan.innerHTML = svgIcon5(config.icon);
		content.appendChild(iconSpan);

		// Message
		var msgSpan = document.createElement('span');
		msgSpan.className = 'toast-message text-sm font-medium flex-1 break-words';
		msgSpan.innerHTML = escapeHtml(message);
		content.appendChild(msgSpan);

		// Action buttons area
		var actions = document.createElement('div');
		actions.className = 'flex items-center space-x-1 flex-shrink-0';

		// Custom buttons
		if (opts.buttons && opts.buttons.length) {
			for (var i = 0; i < opts.buttons.length; i++) {
				var btnDef = opts.buttons[i];
				var customBtn = document.createElement('button');
				customBtn.className = 'text-xs font-semibold px-2 py-1 rounded bg-white/20 hover:bg-white/30 transition-colors';
				customBtn.textContent = btnDef.label || 'Action';
				(function (def) {
					customBtn.addEventListener('click', function (e) {
						e.stopPropagation();
						if (typeof def.onClick === 'function') {
							def.onClick(toast);
						}
						if (def.dismiss !== false) {
							dismiss(toast);
						}
					});
				})(btnDef);
				actions.appendChild(customBtn);
			}
		}

		// Copy button
		var copyBtn = document.createElement('button');
		copyBtn.className = 'toast-copy hover:opacity-75 transition-opacity p-1 rounded hover:bg-white/10';
		copyBtn.title = 'Copy to clipboard';
		copyBtn.innerHTML = svgIcon(ICONS.copy);
		actions.appendChild(copyBtn);

		// Close button
		var closeBtn = document.createElement('button');
		closeBtn.className = 'toast-close hover:opacity-75 transition-opacity p-1 rounded hover:bg-white/10';
		closeBtn.title = 'Dismiss';
		closeBtn.innerHTML = svgIcon(ICONS.close);
		actions.appendChild(closeBtn);

		content.appendChild(actions);
		inner.appendChild(content);

		// Progress bar
		var progressBar = null;
		var showProgress = opts.progressBar !== undefined ? opts.progressBar : DEFAULTS.progressBar;
		if (showProgress && !opts.persistent && opts.duration > 0) {
			var progressTrack = document.createElement('div');
			progressTrack.className = 'w-full h-1 ' + config.progressBg;
			progressBar = document.createElement('div');
			progressBar.className = 'h-full bg-white/60 transition-none';
			progressBar.style.width = '100%';
			progressTrack.appendChild(progressBar);
			inner.appendChild(progressTrack);
		}

		toast.appendChild(inner);

		return { toast: toast, copyBtn: copyBtn, closeBtn: closeBtn, progressBar: progressBar };
	}

	// ── Timer management ───────────────────────────────────────────────

	function createTimer(toast, duration, progressBar, onExpire) {
		var remaining = duration;
		var startTime = Date.now();
		var timeoutId = null;
		var animFrameId = null;

		function updateProgress() {
			if (!progressBar) return;
			var elapsed = Date.now() - startTime;
			var pct = Math.max(0, (remaining - elapsed) / duration * 100);
			progressBar.style.width = pct + '%';
			if (pct > 0) {
				animFrameId = requestAnimationFrame(updateProgress);
			}
		}

		function start() {
			startTime = Date.now();
			timeoutId = setTimeout(onExpire, remaining);
			if (progressBar) {
				animFrameId = requestAnimationFrame(updateProgress);
			}
		}

		function pause() {
			if (timeoutId) {
				clearTimeout(timeoutId);
				timeoutId = null;
			}
			if (animFrameId) {
				cancelAnimationFrame(animFrameId);
				animFrameId = null;
			}
			remaining -= (Date.now() - startTime);
			if (remaining < 0) remaining = 0;
		}

		function cleanup() {
			if (timeoutId) clearTimeout(timeoutId);
			if (animFrameId) cancelAnimationFrame(animFrameId);
			timeoutId = null;
			animFrameId = null;
		}

		return { start: start, pause: pause, cleanup: cleanup };
	}

	// ── Drag-to-dismiss ────────────────────────────────────────────────

	function setupDrag(toast, onDismiss) {
		var startX = 0;
		var currentX = 0;
		var dragging = false;
		var threshold = 80;

		function onPointerDown(e) {
			// Ignore clicks on buttons
			if (e.target.closest('button')) return;
			dragging = true;
			startX = e.clientX;
			currentX = 0;
			toast.style.transition = 'none';
			toast.setPointerCapture(e.pointerId);
		}

		function onPointerMove(e) {
			if (!dragging) return;
			currentX = e.clientX - startX;
			// Only allow dragging right (positive direction)
			if (currentX < 0) currentX = 0;
			toast.style.transform = 'translateX(' + currentX + 'px)';
			toast.style.opacity = String(Math.max(0, 1 - currentX / (threshold * 2.5)));
		}

		function onPointerUp() {
			if (!dragging) return;
			dragging = false;
			toast.style.transition = '';
			if (currentX > threshold) {
				onDismiss();
			} else {
				toast.style.transform = 'translateX(0)';
				toast.style.opacity = '1';
			}
		}

		toast.addEventListener('pointerdown', onPointerDown);
		toast.addEventListener('pointermove', onPointerMove);
		toast.addEventListener('pointerup', onPointerUp);
		toast.addEventListener('pointercancel', onPointerUp);
		toast.style.touchAction = 'pan-y';
		toast.style.cursor = 'grab';

		return function cleanupDrag() {
			toast.removeEventListener('pointerdown', onPointerDown);
			toast.removeEventListener('pointermove', onPointerMove);
			toast.removeEventListener('pointerup', onPointerUp);
			toast.removeEventListener('pointercancel', onPointerUp);
		};
	}

	// ── Copy to clipboard ──────────────────────────────────────────────

	function handleCopy(toast, button) {
		var message = toast.dataset.message;
		if (!message) return;

		navigator.clipboard.writeText(message).then(function () {
			var original = button.innerHTML;
			button.innerHTML = svgIcon(ICONS.check, 'text-green-300');
			setTimeout(function () { button.innerHTML = original; }, 1500);
		}).catch(function (err) {
			console.error('Failed to copy:', err);
		});
	}

	// ── Enforce max toasts ─────────────────────────────────────────────

	function enforceMaxToasts() {
		while (activeToasts.length > DEFAULTS.maxToasts) {
			// Splice synchronously so the length decreases each iteration,
			// then dismiss (which animates out and does a no-op splice later).
			var oldest = activeToasts.splice(0, 1)[0];
			dismiss(oldest);
		}
	}

	// ── Dismiss ────────────────────────────────────────────────────────

	function dismiss(toast) {
		if (!toast || toast._dismissed) return;
		toast._dismissed = true;

		// Run cleanup (timer, drag listeners)
		if (typeof toast._cleanup === 'function') toast._cleanup();

		// Animate out
		toast.style.transition = 'transform ' + DEFAULTS.animationDuration + 'ms ease-in, opacity ' + DEFAULTS.animationDuration + 'ms ease-in';
		toast.style.transform = 'translateX(400px)';
		toast.style.opacity = '0';

		setTimeout(function () {
			toast.remove();
			// Remove from active list
			var idx = activeToasts.indexOf(toast);
			if (idx !== -1) activeToasts.splice(idx, 1);
			// Fire onClose callback
			if (typeof toast._onClose === 'function') {
				toast._onClose();
			}
		}, DEFAULTS.animationDuration);
	}

	function dismissAll() {
		// Snapshot since dismiss mutates the array
		var toasts = activeToasts.slice();
		for (var i = 0; i < toasts.length; i++) {
			dismiss(toasts[i]);
		}
	}

	// ── Main entry point ───────────────────────────────────────────────

	function showToast(message, type, durationOrOpts) {
		var container = getContainer();
		if (!container) return null;

		type = type || 'success';
		var opts = normalizeOptions(durationOrOpts, type);
		var duration = opts.persistent ? 0 : opts.duration;

		// Build DOM
		var parts = buildToast(message, type, opts);
		var toast = parts.toast;
		var progressBar = parts.progressBar;

		// Store onClose callback
		toast._onClose = typeof opts.onClose === 'function' ? opts.onClose : null;

		// Insert into container
		if (DEFAULTS.newestOnTop) {
			container.insertBefore(toast, container.firstChild);
		} else {
			container.appendChild(toast);
		}
		activeToasts.push(toast);
		enforceMaxToasts();

		// Animate in
		requestAnimationFrame(function () {
			requestAnimationFrame(function () {
				toast.style.transform = 'translateX(0)';
				toast.style.opacity = '1';
			});
		});

		// Collect cleanup functions
		var cleanups = [];

		// Close button
		parts.closeBtn.addEventListener('click', function () { dismiss(toast); });

		// Copy button
		parts.copyBtn.addEventListener('click', function (e) {
			e.stopPropagation();
			handleCopy(toast, parts.copyBtn);
		});

		// Timer (if not persistent and duration > 0)
		var timer = null;
		if (duration > 0) {
			timer = createTimer(toast, duration, progressBar, function () { dismiss(toast); });

			var onEnter = function () { timer.pause(); };
			var onLeave = function () { timer.start(); };
			toast.addEventListener('mouseenter', onEnter);
			toast.addEventListener('mouseleave', onLeave);
			cleanups.push(function () {
				timer.cleanup();
				toast.removeEventListener('mouseenter', onEnter);
				toast.removeEventListener('mouseleave', onLeave);
			});

			timer.start();
		}

		// Drag-to-dismiss
		var enableDrag = opts.dragDismiss !== undefined ? opts.dragDismiss : DEFAULTS.dragDismiss;
		if (enableDrag) {
			var cleanupDrag = setupDrag(toast, function () { dismiss(toast); });
			cleanups.push(cleanupDrag);
		}

		// Aggregate cleanup
		toast._cleanup = function () {
			for (var i = 0; i < cleanups.length; i++) {
				cleanups[i]();
			}
		};

		return toast;
	}

	// ── Public API ─────────────────────────────────────────────────────

	window.showToast = showToast;

	window.dismissToast = function (toast) {
		dismiss(toast);
	};

	window.dismissAllToasts = function () {
		dismissAll();
	};

	// Allow reading/changing defaults at runtime
	window.toastDefaults = DEFAULTS;

	// ── ToastOnLoad processing ─────────────────────────────────────────

	document.addEventListener('DOMContentLoaded', function () {
		var els = document.querySelectorAll('.toast-on-load');
		for (var i = 0; i < els.length; i++) {
			var el = els[i];
			var msg = el.dataset.toastMessage;
			var type = el.dataset.toastType || 'success';
			el.remove();
			if (msg) {
				showToast(msg, type, 6000);
			}
		}
	});

})();
