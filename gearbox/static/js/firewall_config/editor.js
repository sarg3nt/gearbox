/**
 * Firewall (nftables) configuration editor.
 *
 * Wires CodeMirror 5 with our custom `nftables` mode to the page, hosts the
 * Validate / Save / Backups flows, and translates `nft -c -f` validation
 * output into in-editor lint markers + a left-rail error panel.
 *
 * Page contract (set up by firewall_config.templ):
 *   #firewall-server-id       hidden input, value = box slug (templ-rendered)
 *   #firewall-config-sha      hidden input, value = file SHA (optimistic lock)
 *   #firewall-can-edit        hidden input, "1" if user has Configure perm
 *   #firewall-config-source   hidden textarea, value = initial file content
 *   #firewall-sections-data   <script type=application/json>, parsed sections
 *   #firewall-editor          mount point for the CodeMirror instance
 *   #firewall-section-nav     left-rail container for the tables/chains list
 *   #firewall-error-panel     left-rail collapsible for validation failures
 *   #firewall-validate-btn / #firewall-save-btn / #firewall-backups-btn /
 *     #firewall-wrap-btn     toolbar buttons hoisted to the page header
 *   #firewall-backups-modal   modal scaffold for the backups list
 */
(function () {
	'use strict';

	document.addEventListener('DOMContentLoaded', init);

	// Editor + state. Filled in by init() once the DOM is ready.
	let cm = null;
	let serverID = '';
	let currentSHA = '';
	let canEdit = false;
	let sections = [];
	let lintMarkers = [];

	function init() {
		serverID = document.getElementById('firewall-server-id')?.value || '';
		currentSHA = document.getElementById('firewall-config-sha')?.value || '';
		canEdit = (document.getElementById('firewall-can-edit')?.value || '0') === '1';

		const sourceTextarea = document.getElementById('firewall-config-source');
		const mountPoint = document.getElementById('firewall-editor');
		if (!sourceTextarea || !mountPoint || typeof CodeMirror === 'undefined') {
			// Without CodeMirror loaded there's nothing useful to do; the
			// page would have rendered with the textarea visible if we'd
			// chosen that fallback path. Quietly bail.
			return;
		}

		try {
			sections = JSON.parse(document.getElementById('firewall-sections-data')?.value || '[]');
		} catch (_) {
			sections = [];
		}

		cm = CodeMirror(mountPoint, {
			value: sourceTextarea.value,
			mode: 'nftables',
			theme: 'material-darker',
			lineNumbers: true,
			lineWrapping: false,
			matchBrackets: true,
			autoCloseBrackets: true,
			styleActiveLine: true,
			indentUnit: 2,
			tabSize: 2,
			readOnly: canEdit ? false : 'nocursor',
			gutters: ['CodeMirror-lint-markers', 'CodeMirror-linenumbers'],
			extraKeys: {
				'Ctrl-Space': 'autocomplete',
				'Cmd-Space': 'autocomplete',
				'Ctrl-S': function () { saveConfig(); },
				'Cmd-S': function () { saveConfig(); },
				'Ctrl-/': 'toggleComment',
				'Cmd-/': 'toggleComment',
			},
			// Open the hint dropdown as the user types, but only when the
			// last char is identifier-ish — typing punctuation or whitespace
			// shouldn't pop the menu.
			hintOptions: {
				hint: CodeMirror.hint.nftables || CodeMirror.hint.anyword,
				completeSingle: false,
			},
		});

		// Hide the original textarea (it was visible via templ but we don't
		// want the duplicate UI).
		sourceTextarea.style.display = 'none';

		setupAutocompleteOnType();
		renderSectionNav();
		setupToolbar();
		setupBackupsModal();
		setupErrorPanelDismiss();
	}

	// --------------------------------------------------------------------
	// Autocomplete as-you-type — fires the hint addon when the user types
	// a letter, but not when they press Enter, navigate, etc.
	// --------------------------------------------------------------------
	function setupAutocompleteOnType() {
		cm.on('inputRead', function (_cm, change) {
			if (change.text.length === 0) return;
			const ch = change.text[change.text.length - 1];
			if (!/[A-Za-z_]/.test(ch)) return;
			// Defer so the change is applied before we open the menu.
			setTimeout(function () {
				if (!cm.state.completionActive) {
					CodeMirror.commands.autocomplete(cm, null, { completeSingle: false });
				}
			}, 0);
		});
	}

	// --------------------------------------------------------------------
	// Left-rail section navigation — a tree of tables → chains with click
	// to scroll. Reads the JSON the templ page embedded.
	// --------------------------------------------------------------------
	function renderSectionNav() {
		const nav = document.getElementById('firewall-section-nav');
		if (!nav) return;
		clearChildren(nav);

		if (!sections.length) {
			const empty = document.createElement('p');
			empty.className = 'px-2 py-2 text-xs text-gray-500';
			empty.textContent = 'No tables found in this config.';
			nav.appendChild(empty);
			return;
		}

		// Group chains under their containing table by walking the flat
		// list in document order — the agent emits tables before their
		// chains, so we just stick each chain visually under the most
		// recent table.
		sections.forEach(function (s) {
			if (s.type === 'table') {
				nav.appendChild(makeNavButton(s, 'table'));
			} else if (s.type === 'chain') {
				const btn = makeNavButton(s, 'chain');
				btn.classList.add('ml-3');
				nav.appendChild(btn);
			}
		});
	}

	function makeNavButton(section, kind) {
		const btn = document.createElement('button');
		btn.type = 'button';
		btn.dataset.line = String(section.start_line || 1);
		btn.className =
			'w-full text-left px-2 py-1 rounded text-gray-300 hover:bg-gray-800 hover:text-white flex items-center gap-2';
		const dot = document.createElement('span');
		dot.className =
			kind === 'table'
				? 'w-2 h-2 rounded-sm bg-purple-400'
				: 'w-2 h-2 rounded-full bg-blue-400';
		btn.appendChild(dot);
		const label = document.createElement('span');
		label.className = 'truncate';
		label.textContent = section.name || section.type;
		btn.appendChild(label);
		btn.addEventListener('click', function () {
			scrollToLine(section.start_line || 1);
		});
		return btn;
	}

	function scrollToLine(line) {
		if (!cm) return;
		const target = Math.max(0, (line | 0) - 1);
		cm.focus();
		cm.setCursor({ line: target, ch: 0 });
		// Place the target line ~1/4 down the visible viewport so the
		// user sees a bit of preceding context.
		const t = cm.charCoords({ line: target, ch: 0 }, 'local').top;
		const editorHeight = cm.getScrollerElement().clientHeight;
		cm.scrollTo(null, Math.max(0, t - editorHeight / 4));
	}

	// --------------------------------------------------------------------
	// Toolbar buttons — bound here rather than via inline onclick=… so
	// templ's content-security-policy doesn't have to allow inline
	// event handlers.
	// --------------------------------------------------------------------
	function setupToolbar() {
		const validate = document.getElementById('firewall-validate-btn');
		const save = document.getElementById('firewall-save-btn');
		const backups = document.getElementById('firewall-backups-btn');
		const wrap = document.getElementById('firewall-wrap-btn');

		if (validate) validate.addEventListener('click', validateConfig);
		if (save) save.addEventListener('click', saveConfig);
		if (backups) backups.addEventListener('click', showBackups);
		if (wrap) wrap.addEventListener('click', toggleWrap);
	}

	function toggleWrap() {
		const wrap = document.getElementById('firewall-wrap-btn');
		const isWrapping = cm.getOption('lineWrapping');
		cm.setOption('lineWrapping', !isWrapping);
		if (wrap) {
			wrap.setAttribute('aria-pressed', String(!isWrapping));
			wrap.classList.toggle('bg-blue-100', !isWrapping);
			wrap.classList.toggle('dark:bg-blue-900/40', !isWrapping);
		}
	}

	// --------------------------------------------------------------------
	// Validate — sends the current buffer to the agent (via dashboard)
	// with dry_run=true. Toasts on success, surfaces errors in the
	// left-rail error panel + as inline lint markers.
	// --------------------------------------------------------------------
	async function validateConfig() {
		if (!cm) return;
		const content = cm.getValue();

		try {
			const response = await fetch(`/api/${serverID}/firewall/config/validate`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ content: content, dry_run: true }),
			});
			const data = await response.json();
			if (data.success) {
				clearValidationErrors();
				if (window.showToast) {
					window.showToast('Configuration is valid', 'success', 3000);
				}
			} else {
				showValidationErrors(data.validation_output || data.message || 'Unknown validation error');
			}
		} catch (err) {
			showValidationErrors('Validation request failed: ' + err.message);
		}
	}

	// --------------------------------------------------------------------
	// Save & Apply — POSTs the buffer with dry_run=false. Uses the same
	// error-display path as Validate when the agent rejects the config.
	// --------------------------------------------------------------------
	async function saveConfig() {
		if (!cm || !canEdit) return;
		const content = cm.getValue();

		const reason = await window.showPromptDialog({
			title: 'Save Configuration',
			message: 'Enter a reason for this change (optional):',
			defaultValue: 'Manual edit via web UI',
			placeholder: 'Reason for change',
		});
		if (reason === null) return;

		try {
			const response = await fetch(`/api/${serverID}/firewall/config`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					content: content,
					expected_sha: currentSHA,
					backup_reason: reason || 'Manual edit via web UI',
					dry_run: false,
				}),
			});
			const data = await response.json();
			if (data.success) {
				currentSHA = data.new_sha256 || currentSHA;
				const sha = document.getElementById('firewall-config-sha');
				if (sha) sha.value = currentSHA;
				clearValidationErrors();
				if (window.showToast) {
					window.showToast('Configuration saved and applied', 'success', 3000);
				}
			} else {
				const msg = data.validation_output || data.message || 'Save failed';
				showValidationErrors(msg);
				if (window.showToast) {
					window.showToast('Save failed — see validation panel', 'error', 5000);
				}
			}
		} catch (err) {
			showValidationErrors('Save request failed: ' + err.message);
		}
	}

	// --------------------------------------------------------------------
	// Validation error display — populates the left-rail panel AND adds
	// inline gutter markers for each line nft pointed at.
	// --------------------------------------------------------------------
	function showValidationErrors(text) {
		const panel = document.getElementById('firewall-error-panel');
		const out = document.getElementById('firewall-error-text');
		if (out) out.textContent = text;
		if (panel) panel.classList.remove('hidden');
		applyLintMarkers(parseNftErrorLines(text));
	}

	function clearValidationErrors() {
		const panel = document.getElementById('firewall-error-panel');
		if (panel) panel.classList.add('hidden');
		applyLintMarkers([]);
	}

	function setupErrorPanelDismiss() {
		const close = document.getElementById('firewall-error-close');
		if (close) {
			close.addEventListener('click', function () {
				const panel = document.getElementById('firewall-error-panel');
				if (panel) panel.classList.add('hidden');
				// Keep gutter markers so the user can still see where the
				// errors were after dismissing the text panel.
			});
		}
	}

	// nft's check output looks like:
	//     /tmp/foo.conf:42:18-20: Error: syntax error, unexpected ...
	// or with a trailing snippet/caret-line below. Pull the line number
	// (and optional column span) out so we can put a marker in the gutter.
	function parseNftErrorLines(text) {
		const out = [];
		if (!text) return out;
		const re = /(?:^|[^\w])(?:\/[^\s:]+|stdin|[^\s:]+\.\w+|):(\d+)(?::(\d+)(?:-(\d+))?)?:\s*(Error|Warning|Note)?:?\s*(.*)/g;
		const matches = String(text).matchAll(re);
		for (const m of matches) {
			const line = parseInt(m[1], 10);
			if (!Number.isFinite(line) || line < 1) continue;
			const colStart = m[2] ? parseInt(m[2], 10) : null;
			const colEnd = m[3] ? parseInt(m[3], 10) : colStart;
			out.push({
				line: line - 1, // CM is 0-indexed
				colStart: colStart != null ? colStart - 1 : null,
				colEnd: colEnd != null ? colEnd : null,
				severity: (m[4] || 'Error').toLowerCase(),
				message: (m[5] || '').trim(),
			});
		}
		return out;
	}

	function applyLintMarkers(items) {
		if (!cm) return;
		// Wipe any existing markers/text decorations from a prior pass.
		lintMarkers.forEach(function (m) { m.clear(); });
		lintMarkers = [];
		cm.clearGutter('CodeMirror-lint-markers');

		items.forEach(function (item) {
			const marker = document.createElement('div');
			marker.className = 'firewall-lint-marker firewall-lint-' + item.severity;
			marker.title = item.message;
			marker.textContent = item.severity === 'warning' ? '!' : '✖';
			cm.setGutterMarker(item.line, 'CodeMirror-lint-markers', marker);

			// Underline the offending span if we have column info.
			if (item.colStart != null && item.colEnd != null && item.colEnd > item.colStart) {
				const mark = cm.markText(
					{ line: item.line, ch: item.colStart },
					{ line: item.line, ch: item.colEnd },
					{ className: 'firewall-lint-underline-' + item.severity }
				);
				lintMarkers.push(mark);
			}
		});

		// Scroll to the first error so the user sees what nft is unhappy about.
		if (items.length > 0) {
			scrollToLine(items[0].line + 1);
		}
	}

	// --------------------------------------------------------------------
	// Backups modal — list, restore. Lifted from the original textarea
	// version and adapted to read CodeMirror's buffer.
	// --------------------------------------------------------------------
	function setupBackupsModal() {
		const modal = document.getElementById('firewall-backups-modal');
		if (!modal) return;
		modal.querySelectorAll('[data-firewall-modal-dismiss]').forEach(function (el) {
			el.addEventListener('click', function () { modal.classList.add('hidden'); });
		});
	}

	async function showBackups() {
		const modal = document.getElementById('firewall-backups-modal');
		const list = document.getElementById('firewall-backups-list');
		if (!modal || !list) return;
		clearChildren(list);
		const loading = document.createElement('p');
		loading.className = 'text-gray-500';
		loading.textContent = 'Loading...';
		list.appendChild(loading);
		modal.classList.remove('hidden');

		try {
			const response = await fetch(`/api/${serverID}/firewall/config/backups`);
			const data = await response.json();
			renderBackups(list, data.backups || []);
		} catch (_) {
			clearChildren(list);
			const err = document.createElement('p');
			err.className = 'text-red-500';
			err.textContent = 'Failed to load backups.';
			list.appendChild(err);
		}
	}

	function renderBackups(list, backups) {
		clearChildren(list);
		if (backups.length === 0) {
			const empty = document.createElement('p');
			empty.className = 'text-gray-500 dark:text-gray-400';
			empty.textContent = 'No backups available.';
			list.appendChild(empty);
			return;
		}
		backups.forEach(function (b) {
			const row = document.createElement('div');
			row.className =
				'flex items-center justify-between p-3 bg-gray-50 dark:bg-slate-700 rounded-lg';
			const info = document.createElement('div');
			const reasonEl = document.createElement('div');
			reasonEl.className = 'font-medium text-gray-900 dark:text-gray-100';
			reasonEl.textContent = b.reason || 'Unknown';
			const dateEl = document.createElement('div');
			dateEl.className = 'text-sm text-gray-500 dark:text-gray-400';
			dateEl.textContent = b.created_at ? new Date(b.created_at).toLocaleString() : '';
			info.appendChild(reasonEl);
			info.appendChild(dateEl);

			const restoreBtn = document.createElement('button');
			restoreBtn.type = 'button';
			restoreBtn.className = 'px-3 py-1 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm';
			restoreBtn.textContent = 'Restore';
			restoreBtn.addEventListener('click', function () { restoreBackup(b.id); });

			row.appendChild(info);
			row.appendChild(restoreBtn);
			list.appendChild(row);
		});
	}

	async function restoreBackup(backupID) {
		const confirmed = await window.showConfirmDialog({
			title: 'Restore Backup',
			message: 'Restore from this backup? The current configuration will be replaced.',
			confirmText: 'Restore',
			type: 'danger',
		});
		if (!confirmed) return;

		try {
			const response = await fetch(`/api/${serverID}/firewall/config/restore`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ backup_id: backupID }),
			});
			const data = await response.json();
			if (data.success) {
				if (window.showToast) window.showToast('Configuration restored', 'success', 3000);
				setTimeout(function () { window.location.reload(); }, 800);
			} else {
				if (window.showToast) {
					window.showToast('Restore failed: ' + (data.message || ''), 'error', 5000);
				}
			}
		} catch (err) {
			if (window.showToast) window.showToast('Restore failed: ' + err.message, 'error', 5000);
		}
	}

	// Generic helper — replaces the `el.innerHTML = ''` idiom while keeping
	// our XSS-paranoid hooks happy.
	function clearChildren(el) {
		while (el.firstChild) el.removeChild(el.firstChild);
	}
})();
