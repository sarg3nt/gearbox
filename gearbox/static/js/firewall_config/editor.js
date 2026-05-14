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
 *   #firewall-validate-status / #firewall-validate-status-dot / -status-text
 *                             tiny live status pill (idle/checking/valid/errors)
 *   #firewall-validate-btn / #firewall-save-btn / #firewall-backups-btn /
 *     #firewall-snippets-btn / #firewall-wrap-btn
 *                             toolbar buttons hoisted to the page header
 *   #firewall-backups-modal   modal scaffold for the backups list
 *   #firewall-snippets-modal  modal scaffold for the snippet catalog
 *   #firewall-error-modal     modal shown when Save & Apply hits validation errors
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

	// Real-time validation state.
	//
	// `validationErrors` holds the most recent parsed errors from nft -c -f
	// (each with line/colStart/colEnd/message). The hover handler scans
	// this list to look up the message for a given cursor position so we
	// don't have to encode the message into the DOM.
	//
	// `validationDebounceTimer` is the pending setTimeout id for the
	// debounced edit→validate trigger. `validationInFlight` lets us drop
	// stale responses if a newer edit kicked off a fresher request.
	let validationErrors = [];
	let validationDebounceTimer = null;
	let validationGeneration = 0; // monotonic; bumped each time a validate fires
	const VALIDATE_DEBOUNCE_MS = 1000;

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

		// Clamp the page body so the firewall editor lives in its own
		// fixed viewport — no body-level scroll, no horizontal overflow
		// from long rule lines. Internal scrolling (the CodeMirror scroller
		// + the section nav) still works as expected. Math-based fixes
		// (`calc(100vh - 57px)` etc.) kept missing by a few pixels because
		// the global header height varies across themes; locking overflow
		// is the bulletproof option, and since gearbox does full page
		// loads between gears the side-effect is naturally undone when the
		// user navigates away.
		document.documentElement.style.overflow = 'hidden';
		document.body.style.overflow = 'hidden';

		setupAutocompleteOnType();
		setupKeywordTooltip();
		setupRealtimeValidation();
		renderSectionNav();
		setupToolbar();
		setupBackupsModal();
		setupSnippetsModal();
		setupErrorModalDismiss();
	}

	// --------------------------------------------------------------------
	// Real-time validation — fire `nft -c -f` (via the dashboard) after the
	// user pauses typing. 1s debounce so a fast typist isn't slamming the
	// agent. The explicit Validate button still works as a hard re-check
	// and toasts on success; real-time updates the inline markers silently.
	// --------------------------------------------------------------------
	function setupRealtimeValidation() {
		if (!cm) return;
		cm.on('change', function () {
			setValidateStatus('pending');
			if (validationDebounceTimer) clearTimeout(validationDebounceTimer);
			validationDebounceTimer = setTimeout(runRealtimeValidate, VALIDATE_DEBOUNCE_MS);
		});
	}

	async function runRealtimeValidate() {
		if (!cm) return;
		const gen = ++validationGeneration;
		const content = cm.getValue();
		setValidateStatus('checking');
		try {
			const response = await fetch(`/api/${serverID}/firewall/config/validate`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ content: content, dry_run: true }),
			});
			// If a newer edit has already kicked off a fresher request,
			// drop this response — the next one will land soon.
			if (gen !== validationGeneration) return;
			const data = await response.json();
			if (data.success) {
				applyValidationResult([]);
				setValidateStatus('valid');
			} else {
				const errs = parseNftErrorLines(data.validation_output || data.message || '');
				applyValidationResult(errs);
				setValidateStatus(errs.length > 0 ? 'invalid' : 'unknown');
			}
		} catch (_err) {
			// Network/transport error — leave existing markers, set status
			// to "unknown" so the user knows we couldn't reach the agent.
			if (gen !== validationGeneration) return;
			setValidateStatus('unknown');
		}
	}

	function setValidateStatus(state) {
		const dot = document.getElementById('firewall-validate-status-dot');
		const text = document.getElementById('firewall-validate-status-text');
		const wrap = document.getElementById('firewall-validate-status');
		if (!dot || !text || !wrap) return;
		// Strip every state class then apply the new one.
		dot.className = 'w-1.5 h-1.5 rounded-full';
		switch (state) {
			case 'pending':
				dot.classList.add('bg-gray-500');
				text.textContent = 'pending';
				wrap.title = 'Waiting for you to pause typing…';
				break;
			case 'checking':
				dot.classList.add('bg-blue-400', 'animate-pulse');
				text.textContent = 'checking';
				wrap.title = 'Running nft -c -f …';
				break;
			case 'valid':
				dot.classList.add('bg-green-500');
				text.textContent = 'valid';
				wrap.title = 'Last validation passed';
				break;
			case 'invalid':
				dot.classList.add('bg-red-500');
				text.textContent = 'errors';
				wrap.title = 'Hover the underlined lines for details';
				break;
			case 'unknown':
				dot.classList.add('bg-yellow-500');
				text.textContent = '—';
				wrap.title = 'Validation unavailable';
				break;
			default:
				dot.classList.add('bg-gray-600');
				text.textContent = 'idle';
				wrap.title = '';
		}
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
	// Explicit Validate button — same wire as the real-time check but
	// gives a toast on success (real-time updates are silent so they
	// don't nag while the user is typing). Errors land as inline markers
	// just like the real-time path.
	// --------------------------------------------------------------------
	async function validateConfig() {
		if (!cm) return;
		const content = cm.getValue();
		// Cancel any in-flight debounce so we don't double-validate.
		if (validationDebounceTimer) {
			clearTimeout(validationDebounceTimer);
			validationDebounceTimer = null;
		}
		const gen = ++validationGeneration;
		setValidateStatus('checking');

		try {
			const response = await fetch(`/api/${serverID}/firewall/config/validate`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ content: content, dry_run: true }),
			});
			if (gen !== validationGeneration) return;
			const data = await response.json();
			if (data.success) {
				applyValidationResult([]);
				setValidateStatus('valid');
				if (window.showToast) {
					window.showToast('Configuration is valid', 'success', 3000);
				}
			} else {
				const errs = parseNftErrorLines(data.validation_output || data.message || '');
				applyValidationResult(errs);
				setValidateStatus(errs.length > 0 ? 'invalid' : 'unknown');
				if (errs.length === 0 && window.showToast) {
					// Agent returned success:false but no parseable lines —
					// surface the raw message as a toast so the user sees it.
					window.showToast(data.message || 'Validation failed', 'error', 6000);
				}
			}
		} catch (err) {
			setValidateStatus('unknown');
			if (window.showToast) {
				window.showToast('Validation request failed: ' + err.message, 'error', 5000);
			}
		}
	}

	// --------------------------------------------------------------------
	// Save & Apply — POSTs the buffer with dry_run=false. Success → toast
	// + SHA refresh. Failure → an error modal with the validator output
	// AND inline markers so the user can navigate the bad lines.
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
				applyValidationResult([]);
				setValidateStatus('valid');
				if (window.showToast) {
					window.showToast('Configuration saved and applied', 'success', 3000);
				}
			} else {
				const raw = data.validation_output || data.message || 'nftables refused the configuration.';
				const errs = parseNftErrorLines(raw);
				applyValidationResult(errs);
				setValidateStatus(errs.length > 0 ? 'invalid' : 'unknown');
				showErrorModal(raw, errs);
			}
		} catch (err) {
			showErrorModal('Save request failed: ' + err.message, []);
		}
	}

	// --------------------------------------------------------------------
	// Validation result plumbing — single entry point for both real-time
	// and explicit validate / save flows. Updates the cached error list
	// (which the hover handler reads) and rewrites the inline markers.
	// --------------------------------------------------------------------
	function applyValidationResult(items) {
		validationErrors = items || [];
		applyLintMarkers(validationErrors);
	}

	// --------------------------------------------------------------------
	// Error modal (Save & Apply failure) — populates and shows the modal
	// scaffold rendered by firewall_config.templ.
	// --------------------------------------------------------------------
	function showErrorModal(rawOutput, errs) {
		const modal = document.getElementById('firewall-error-modal');
		const summary = document.getElementById('firewall-error-modal-summary');
		const output = document.getElementById('firewall-error-modal-output');
		if (!modal || !summary || !output) return;
		if (errs && errs.length > 0) {
			summary.textContent = errs.length === 1
				? errs[0].humanized || errs[0].message
				: errs.length + ' error' + (errs.length === 1 ? '' : 's') + ' — hover the underlined lines for details.';
		} else {
			summary.textContent = '';
		}
		output.textContent = rawOutput;
		modal.classList.remove('hidden');
	}

	function setupErrorModalDismiss() {
		const modal = document.getElementById('firewall-error-modal');
		if (!modal) return;
		modal.querySelectorAll('[data-firewall-error-dismiss]').forEach(function (el) {
			el.addEventListener('click', function () { modal.classList.add('hidden'); });
		});
	}

	// nft's check output looks like:
	//     /tmp/foo.conf:42:18-20: Error: syntax error, unexpected ...
	// or with a trailing snippet/caret-line below. Pull the line number
	// (and optional column span) out so we can put a marker in the gutter.
	// Each parsed error gets a `humanized` field too — a plain-English
	// rewrite of the most opaque nft messages so the hover tooltip can
	// say something useful.
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
			const rawMsg = (m[5] || '').trim();
			out.push({
				line: line - 1, // CM is 0-indexed
				colStart: colStart != null ? colStart - 1 : null,
				colEnd: colEnd != null ? colEnd : null,
				severity: (m[4] || 'Error').toLowerCase(),
				message: rawMsg,
				humanized: humanizeNftError(rawMsg),
			});
		}
		return out;
	}

	// Translate the most opaque `nft -c -f` complaints into plain English.
	// The raw message is always preserved in `message`; this function only
	// produces the friendlier rephrasing for the hover tooltip / save
	// modal. When no pattern matches, falls back to the raw message so
	// we never hide what nft actually said.
	function humanizeNftError(raw) {
		if (!raw) return '';
		const r = String(raw);

		// "syntax error, unexpected X, expecting Y" — by far the most common.
		const synErr = r.match(/^syntax error, unexpected ([^,]+?)(?:, expecting (.+))?$/i);
		if (synErr) {
			const got = synErr[1].trim();
			const want = (synErr[2] || '').trim();
			if (want) {
				return 'Syntax error — expected ' + simplifyExpected(want) + ' here, but saw ' + simplifyGot(got) + '. Check for missing keywords, semicolons, or braces.';
			}
			return 'Syntax error — ' + simplifyGot(got) + ' isn\'t valid here. Check for missing keywords, semicolons, or braces.';
		}

		// Interface lookup failures.
		if (/Could not process rule: No such file or directory/i.test(r)) {
			return 'nftables couldn\'t resolve a referenced object — usually an interface name that doesn\'t exist on this host. Double-check `iif`/`oif`/`iifname`/`oifname` values.';
		}

		// IP family mixing.
		if (/conflicting protocols specified: ip vs ip6/i.test(r) || /conflicting protocols specified: ip6 vs ip/i.test(r)) {
			return 'You can\'t mix `ip` (IPv4) and `ip6` (IPv6) matches in the same rule. Use the `inet` family for dual-stack, or split into two rules.';
		}

		// Unknown set / map / chain references.
		const noSet = r.match(/(?:set|map)\s+'?([^']+?)'?\s+does not exist/i);
		if (noSet) {
			return 'The referenced set/map `' + noSet[1] + '` isn\'t defined. Add a `set ' + noSet[1] + ' { type ...; elements = { ... }; }` block earlier in the file.';
		}
		const noChain = r.match(/(?:chain|jump|goto)\s+'?([^'\s]+)'?\s+does not exist/i);
		if (noChain) {
			return 'The chain `' + noChain[1] + '` referenced by jump/goto doesn\'t exist. Define it as a non-base chain in the same table.';
		}

		// Address / port parsing.
		if (/Could not parse Network Address/i.test(r) || /not a valid Internet address/i.test(r)) {
			return 'That looks like a malformed IP address or CIDR. Check for typos and that the prefix length (after `/`) is valid (0-32 for IPv4, 0-128 for IPv6).';
		}
		if (/Could not parse Service Port/i.test(r) || /invalid port/i.test(r)) {
			return 'That isn\'t a valid port number. Use 1-65535, a named service from /etc/services, or a `{ port1, port2 }` set.';
		}

		// Unknown identifier.
		const unkId = r.match(/unknown identifier:?\s+'?([^'\s]+)'?/i);
		if (unkId) {
			return '`' + unkId[1] + '` isn\'t recognized here. It might be a misspelled keyword, an undefined set/chain, or used in the wrong context.';
		}

		// Operation not supported / kernel rejection.
		if (/Operation not supported/i.test(r)) {
			return 'The kernel rejected this construct — typically because the running kernel/nftables version is missing a feature. Check the host\'s nft + kernel versions against what this rule needs.';
		}

		// `add element ... already exists` etc.
		if (/already exists/i.test(r)) {
			return 'Something with this name already exists in the ruleset. Use `flush` first, or pick a different name.';
		}

		// Default — pass the raw message through.
		return r;
	}

	function simplifyExpected(want) {
		// nft expectations come back as bison terminal lists like
		// "newline or string" or "T_ACCEPT, T_DROP, ...". Try to render
		// them as something a human reads.
		return String(want)
			.replace(/^T_/, '')
			.replace(/\bT_/g, '')
			.toLowerCase()
			.replace(/\bnewline\b/g, 'a new line')
			.replace(/\bstring\b/g, 'an identifier');
	}

	function simplifyGot(got) {
		const g = String(got).trim();
		if (g === 'newline') return 'a new line';
		if (g === '$end') return 'end of input';
		if (g === 'string') return 'an identifier';
		return '`' + g + '`';
	}

	function applyLintMarkers(items) {
		if (!cm) return;
		// Wipe any existing markers/text decorations from a prior pass.
		lintMarkers.forEach(function (m) { m.clear(); });
		lintMarkers = [];
		cm.clearGutter('CodeMirror-lint-markers');

		items.forEach(function (item) {
			// Gutter marker — the ✖ in the line-number gutter. `title` is
			// the humanized message so the gutter ✖ itself is hoverable
			// (native browser tooltip; cheaper than wiring CodeMirror's
			// per-element listener on a gutter element).
			const marker = document.createElement('div');
			marker.className = 'firewall-lint-marker firewall-lint-' + item.severity;
			marker.title = item.humanized || item.message;
			marker.textContent = item.severity === 'warning' ? '!' : '✖';
			cm.setGutterMarker(item.line, 'CodeMirror-lint-markers', marker);

			// Underline the offending span. If we have column info mark
			// just that span; otherwise underline the whole line so the
			// user always has somewhere to hover (an error with no
			// underline would be confusing — "where do I point?").
			let from, to;
			if (item.colStart != null && item.colEnd != null && item.colEnd > item.colStart) {
				from = { line: item.line, ch: item.colStart };
				to = { line: item.line, ch: item.colEnd };
			} else {
				const lineLen = (cm.getLine(item.line) || '').length;
				from = { line: item.line, ch: 0 };
				to = { line: item.line, ch: Math.max(1, lineLen) };
			}
			const mark = cm.markText(from, to, {
				className: 'firewall-lint-underline-' + item.severity,
			});
			lintMarkers.push(mark);
		});
		// Deliberately do NOT scroll the editor on marker updates — that
		// was unbearable during real-time validation while typing. The
		// status pill turning red is the user's signal; they can click a
		// table/chain in the nav to navigate to the bad section.
	}

	// findErrorAt — used by the hover tooltip to map a cursor position to
	// any active validation error covering it. Returns null when none.
	// Errors with column info match only their underlined span; errors
	// without column info match the whole offending line.
	function findErrorAt(pos) {
		if (!pos || !validationErrors.length) return null;
		for (const e of validationErrors) {
			if (e.line !== pos.line) continue;
			if (e.colStart == null || e.colEnd == null) return e;
			if (pos.ch >= e.colStart && pos.ch <= e.colEnd) return e;
		}
		return null;
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

			// Errors take precedence over keyword docs — if the user is
			// pointing at a red-underlined span, the explanation of THAT
			// is what they want, not "here's what `dport` means".
			const err = findErrorAt(pos);
			if (err) {
				showErrorTooltip(ev.clientX, ev.clientY, err);
				return;
			}

			const token = cm.getTokenAt(pos);
			if (!token || !token.string) return hideTooltip();
			const doc = KEYWORD_DOCS[token.string];
			if (!doc) return hideTooltip();
			showKeywordTooltip(ev.clientX, ev.clientY, token.string, doc);
		});
		wrapper.addEventListener('mouseleave', hideTooltip);
		wrapper.addEventListener('mousedown', hideTooltip);
	}

	function ensureTooltipEl() {
		if (!tooltipEl) {
			tooltipEl = document.createElement('div');
			tooltipEl.className = 'firewall-keyword-tooltip';
			document.body.appendChild(tooltipEl);
		}
		clearTimeout(tooltipHideTimer);
		clearChildren(tooltipEl);
		return tooltipEl;
	}

	function showKeywordTooltip(x, y, word, doc) {
		const el = ensureTooltipEl();
		el.classList.remove('firewall-keyword-tooltip-error');

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

		el.appendChild(title);
		el.appendChild(body);
		el.appendChild(link);
		positionTooltip(x, y);
	}

	function showErrorTooltip(x, y, err) {
		const el = ensureTooltipEl();
		el.classList.add('firewall-keyword-tooltip-error');

		const title = document.createElement('div');
		title.className = 'firewall-keyword-tooltip-title';
		title.textContent = (err.severity === 'warning' ? 'Warning' : 'Validation error')
			+ ' · line ' + (err.line + 1);
		const body = document.createElement('div');
		body.className = 'firewall-keyword-tooltip-body';
		body.textContent = err.humanized || err.message;
		el.appendChild(title);
		el.appendChild(body);

		// Show the raw nft message too if it's different from the
		// humanized one — the curated text gives the "what to do" and the
		// raw text gives the "what nft literally said" for power users.
		if (err.humanized && err.humanized !== err.message && err.message) {
			const raw = document.createElement('pre');
			raw.className = 'firewall-keyword-tooltip-raw';
			raw.textContent = err.message;
			el.appendChild(raw);
		}

		positionTooltip(x, y);
	}

	function positionTooltip(x, y) {
		tooltipEl.style.display = 'block';
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
