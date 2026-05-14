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
		setupKeywordTooltip();
		renderSectionNav();
		setupToolbar();
		setupBackupsModal();
		setupSnippetsModal();
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
		const snippets = document.getElementById('firewall-snippets-btn');

		if (validate) validate.addEventListener('click', validateConfig);
		if (save) save.addEventListener('click', saveConfig);
		if (backups) backups.addEventListener('click', showBackups);
		if (wrap) wrap.addEventListener('click', toggleWrap);
		if (snippets) snippets.addEventListener('click', showSnippets);
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

	// --------------------------------------------------------------------
	// Snippets modal — a curated catalog of common nftables patterns the
	// user can browse and insert at the cursor. Catalog data is provided
	// by snippets.js as `window.FIREWALL_SNIPPETS`.
	// --------------------------------------------------------------------
	let selectedSnippet = null;

	function setupSnippetsModal() {
		const modal = document.getElementById('firewall-snippets-modal');
		if (!modal) return;
		modal.querySelectorAll('[data-firewall-snippets-dismiss]').forEach(function (el) {
			el.addEventListener('click', function () { modal.classList.add('hidden'); });
		});
		const insertBtn = document.getElementById('firewall-snippets-insert');
		if (insertBtn) insertBtn.addEventListener('click', insertSelectedSnippet);
		const filter = document.getElementById('firewall-snippets-filter');
		if (filter) filter.addEventListener('input', renderSnippetList);
	}

	function showSnippets() {
		const modal = document.getElementById('firewall-snippets-modal');
		if (!modal) return;
		modal.classList.remove('hidden');
		selectedSnippet = null;
		renderSnippetList();
		renderSnippetPreview();
	}

	function renderSnippetList() {
		const wrap = document.getElementById('firewall-snippets-list');
		if (!wrap || !window.FIREWALL_SNIPPETS) return;
		clearChildren(wrap);

		const filterEl = document.getElementById('firewall-snippets-filter');
		const q = (filterEl ? filterEl.value : '').trim().toLowerCase();
		const matches = window.FIREWALL_SNIPPETS.filter(function (s) {
			if (!q) return true;
			return (
				s.title.toLowerCase().indexOf(q) >= 0 ||
				s.description.toLowerCase().indexOf(q) >= 0 ||
				s.category.toLowerCase().indexOf(q) >= 0 ||
				s.code.toLowerCase().indexOf(q) >= 0
			);
		});

		// Group by category, preserving the catalog's order within each.
		const grouped = new Map();
		matches.forEach(function (s) {
			if (!grouped.has(s.category)) grouped.set(s.category, []);
			grouped.get(s.category).push(s);
		});

		grouped.forEach(function (items, category) {
			const heading = document.createElement('h4');
			heading.className = 'px-2 pt-3 pb-1 text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400';
			heading.textContent = category;
			wrap.appendChild(heading);
			items.forEach(function (snippet) {
				const btn = document.createElement('button');
				btn.type = 'button';
				btn.className =
					'w-full text-left px-3 py-2 rounded text-sm text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-slate-700';
				btn.textContent = snippet.title;
				btn.addEventListener('click', function () {
					selectedSnippet = snippet;
					// Visual selected state
					wrap.querySelectorAll('button').forEach(function (b) {
						b.classList.remove('bg-blue-100', 'dark:bg-blue-900/30', 'text-blue-700', 'dark:text-blue-200');
					});
					btn.classList.add('bg-blue-100', 'dark:bg-blue-900/30', 'text-blue-700', 'dark:text-blue-200');
					renderSnippetPreview();
				});
				wrap.appendChild(btn);
			});
		});

		if (matches.length === 0) {
			const empty = document.createElement('p');
			empty.className = 'px-3 py-4 text-sm text-gray-500 dark:text-gray-400';
			empty.textContent = 'No snippets match.';
			wrap.appendChild(empty);
		}
	}

	function renderSnippetPreview() {
		const desc = document.getElementById('firewall-snippets-desc');
		const code = document.getElementById('firewall-snippets-code');
		const insertBtn = document.getElementById('firewall-snippets-insert');
		if (!desc || !code || !insertBtn) return;

		if (!selectedSnippet) {
			desc.textContent = 'Pick a snippet on the left to see what it does.';
			code.textContent = '';
			insertBtn.setAttribute('disabled', 'disabled');
			insertBtn.classList.add('opacity-50', 'cursor-not-allowed');
			return;
		}
		desc.textContent = selectedSnippet.description;
		code.textContent = selectedSnippet.code;
		if (canEdit) {
			insertBtn.removeAttribute('disabled');
			insertBtn.classList.remove('opacity-50', 'cursor-not-allowed');
		}
	}

	function insertSelectedSnippet() {
		if (!selectedSnippet || !cm || !canEdit) return;
		const modal = document.getElementById('firewall-snippets-modal');

		// Insert at the cursor; preserve indentation by replicating the
		// leading whitespace of the current line on each newline of the
		// snippet (other than the first, which goes wherever the cursor
		// is). This keeps a snippet inserted inside a chain `{}` block
		// readable rather than dropping it flush-left.
		const cur = cm.getCursor();
		const lineText = cm.getLine(cur.line) || '';
		const indentMatch = lineText.match(/^[\t ]*/);
		const indent = (indentMatch && indentMatch[0]) || '';
		const indented = selectedSnippet.code
			.split('\n')
			.map(function (l, i) { return i === 0 ? l : indent + l; })
			.join('\n');
		cm.replaceSelection(indented, 'around');
		cm.focus();
		if (modal) modal.classList.add('hidden');
		if (window.showToast) {
			window.showToast('Inserted: ' + selectedSnippet.title, 'success', 2000);
		}
	}

	// --------------------------------------------------------------------
	// Hover tooltips — when the mouse pauses over a keyword in the editor,
	// show a small popover with a one-line description and a doc link.
	// Driven by a table of well-known nftables tokens; everything else is
	// ignored. The popover is built lazily and follows the mouse.
	// --------------------------------------------------------------------
	const KEYWORD_DOCS = {
		table:    { text: 'Container for chains, sets, maps, and other objects. Bound to an address family (ip, ip6, inet, arp, bridge, netdev).', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_tables' },
		chain:    { text: 'A list of rules. Base chains attach to a netfilter hook via `type ... hook ... priority ...`.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_chains' },
		rule:     { text: 'A match + verdict pair inside a chain. Rules are evaluated in order; first match wins.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Simple_rule_management' },
		set:      { text: 'A named collection of values (IPs, ports, names). Reference with `@setname` in a rule.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Sets' },
		map:      { text: 'A key→value lookup table. Use with vmap for verdict maps that branch on a header field.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Maps' },
		flowtable:{ text: 'Software flow offload — bypass the netfilter hooks for established flows.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Flowtables' },
		counter:  { text: 'Increment a packet/byte counter. Standalone or attached to a rule.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Counters' },
		limit:    { text: 'Rate-limit matching packets. Common forms: `limit rate 10/second`, `limit rate over 5/minute`.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Rate_limiting_matchings' },

		// Families
		ip:       { text: 'IPv4 address family. Used in `table ip ...` and as a match prefix (`ip saddr`, `ip protocol`).', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_tables' },
		ip6:      { text: 'IPv6 address family.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_tables' },
		inet:     { text: 'Dual-stack family — one table handles both IPv4 and IPv6 traffic.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_tables' },
		arp:      { text: 'ARP family — filter ARP packets.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_tables' },
		bridge:   { text: 'Bridge family — filter traffic crossing a Linux bridge.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_tables' },
		netdev:   { text: 'Netdev family — ingress hook on a specific interface, before any other netfilter processing.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netdev_family' },

		// Hooks
		prerouting:  { text: 'Hook: packets just arrived, before routing decisions. Used for DNAT and ingress filtering.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netfilter_hooks' },
		input:       { text: 'Hook: packets destined for this host. Where most "host firewall" rules live.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netfilter_hooks' },
		forward:     { text: 'Hook: packets passing through this host (routing/bridging). For router/gateway rules.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netfilter_hooks' },
		output:      { text: 'Hook: packets generated by this host. Filtering here is rarely needed.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netfilter_hooks' },
		postrouting: { text: 'Hook: packets about to leave, after routing. Standard place for SNAT/masquerade.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netfilter_hooks' },
		ingress:     { text: 'Hook: per-device ingress (netdev family) — runs before the rest of netfilter.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netdev_family' },
		egress:      { text: 'Hook: per-device egress (netdev family).', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netdev_family' },

		// Verdicts
		accept:   { text: 'Verdict: accept the packet and stop traversing this chain.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Simple_rule_management' },
		drop:     { text: 'Verdict: drop the packet silently.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Simple_rule_management' },
		reject:   { text: 'Verdict: drop the packet and send an ICMP/TCP-reset response. Customize with `reject with ...`.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes#Reject' },
		jump:     { text: 'Verdict: jump to another chain; control returns here on `return` or fallthrough.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_chains#Jumping_to_chain' },
		goto:     { text: 'Verdict: jump to another chain; control does NOT return (replaces this chain\'s evaluation).', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_chains#Jumping_to_chain' },
		queue:    { text: 'Verdict: pass packet to userspace via NFQUEUE.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Queueing_to_userspace' },
		'return': { text: 'Verdict: return from a sub-chain to the calling chain.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_chains' },

		// Common matches
		meta:     { text: 'Match on packet metadata: iif/oif/iifname/oifname/mark/priority/protocol/length/skuid/...', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Matching_packet_metainformation' },
		ct:       { text: 'Match on conntrack: state/status/direction/mark/saddr/daddr/sport/dport/proto/helper/...', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Matching_connection_tracking_stateful_metainformation' },
		tcp:      { text: 'TCP header match: sport, dport, flags, sequence, ack, win, doff, checksum, urgptr.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes' },
		udp:      { text: 'UDP header match: sport, dport, length, checksum.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes' },
		icmp:     { text: 'ICMPv4 match: type, code, checksum, id, seq, mtu.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes' },
		icmpv6:   { text: 'ICMPv6 match: type, code, checksum, mtu. Be careful not to block ND if you want IPv6 to work.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes' },
		iifname:  { text: 'Match on input interface NAME (e.g. "eno1", "wg0"). Slower than `iif` but stable across reboots.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Matching_packet_metainformation' },
		oifname:  { text: 'Match on output interface NAME. Use for outbound rules on a router.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Matching_packet_metainformation' },
		saddr:    { text: 'Source address. Used as `ip saddr ...`, `ip6 saddr ...`, or with sets/maps.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes' },
		daddr:    { text: 'Destination address.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes' },
		sport:    { text: 'Source port (tcp/udp/sctp/dccp).', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes' },
		dport:    { text: 'Destination port.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes' },
		state:    { text: 'ct state: new, established, related, untracked, invalid. Pair with accept/drop verdicts.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Matching_connection_tracking_stateful_metainformation' },

		// Actions
		log:      { text: 'Log matching packets to the kernel ring buffer. `log prefix "..." level info`.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Logging_traffic' },
		snat:     { text: 'Source NAT — rewrite source address (postrouting). Static counterpart of masquerade.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Performing_Network_Address_Translation_(NAT)' },
		dnat:     { text: 'Destination NAT — rewrite destination address/port (prerouting). Used for port forwards.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Performing_Network_Address_Translation_(NAT)' },
		masquerade:{ text: 'Source NAT to the outbound interface\'s IP. Use when WAN IP is dynamic.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Performing_Network_Address_Translation_(NAT)' },
		redirect: { text: 'Special DNAT — rewrite destination to the local host. Used for transparent proxy.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Performing_Network_Address_Translation_(NAT)' },
		policy:   { text: 'Default verdict for a base chain when no rule matches. Almost always `drop` or `accept`.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Configuring_chains' },
		priority: { text: 'Chain priority (signed int or named: filter=0, nat=-100 prerouting / 100 postrouting, mangle=-150, ...).', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netfilter_hooks' },
		hook:     { text: 'Which netfilter hook this base chain attaches to: prerouting, input, forward, output, postrouting, ingress, egress.', href: 'https://wiki.nftables.org/wiki-nftables/index.php/Netfilter_hooks' },
	};

	let tooltipEl = null;
	let tooltipHideTimer = null;

	function setupKeywordTooltip() {
		if (!cm) return;
		const wrapper = cm.getWrapperElement();

		wrapper.addEventListener('mousemove', function (ev) {
			const pos = cm.coordsChar({ left: ev.clientX, top: ev.clientY }, 'window');
			if (!pos) return hideTooltip();
			const token = cm.getTokenAt(pos);
			if (!token || !token.string) return hideTooltip();
			const doc = KEYWORD_DOCS[token.string];
			if (!doc) return hideTooltip();
			showTooltip(ev.clientX, ev.clientY, token.string, doc);
		});
		wrapper.addEventListener('mouseleave', hideTooltip);
		wrapper.addEventListener('mousedown', hideTooltip);
	}

	function showTooltip(x, y, word, doc) {
		clearTimeout(tooltipHideTimer);
		if (!tooltipEl) {
			tooltipEl = document.createElement('div');
			tooltipEl.className = 'firewall-keyword-tooltip';
			document.body.appendChild(tooltipEl);
		}
		clearChildren(tooltipEl);
		const title = document.createElement('div');
		title.className = 'firewall-keyword-tooltip-title';
		title.textContent = word;
		const body = document.createElement('div');
		body.className = 'firewall-keyword-tooltip-body';
		body.textContent = doc.text;
		const link = document.createElement('a');
		link.href = doc.href;
		link.target = '_blank';
		link.rel = 'noopener noreferrer';
		link.className = 'firewall-keyword-tooltip-link';
		link.textContent = 'open docs ›';
		tooltipEl.appendChild(title);
		tooltipEl.appendChild(body);
		tooltipEl.appendChild(link);
		tooltipEl.style.display = 'block';

		// Position below+right of the cursor, but flip if it would overflow.
		const pad = 14;
		const rect = tooltipEl.getBoundingClientRect();
		let left = x + pad;
		let top = y + pad;
		if (left + rect.width > window.innerWidth) left = x - rect.width - pad;
		if (top + rect.height > window.innerHeight) top = y - rect.height - pad;
		tooltipEl.style.left = Math.max(8, left) + 'px';
		tooltipEl.style.top = Math.max(8, top) + 'px';
	}

	function hideTooltip() {
		if (!tooltipEl) return;
		clearTimeout(tooltipHideTimer);
		tooltipHideTimer = setTimeout(function () {
			if (tooltipEl) tooltipEl.style.display = 'none';
		}, 80);
	}
})();
