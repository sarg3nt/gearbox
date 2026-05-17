// console.js — drives the remote-console drawer.
//
// Public surface:
//   window.openConsole(boxID, boxName) — opens the drawer and starts a session
//   window.closeConsole()              — closes whatever session is open
//
// The drawer markup (#console-drawer, #console-xterm, etc.) is rendered
// by templ component ConsoleDrawer() in the base layout; we just attach
// xterm.js to #console-xterm and pipe bytes through a WebSocket to
// /api/console/{boxID}/ws (dashboard-side proxy to the agent).
//
// Wire protocol — JSON frames, matched to the agent's protocol.go:
//   {"t":"data","d":"<base64 stdin/stdout>"}
//   {"t":"resize","cols":120,"rows":40}
//   {"t":"signal","s":"INT"}        (rarely used — Ctrl-C is just bytes)
//   {"t":"ping"} / {"t":"pong"}
//   {"t":"exit","code":N,"reason":"…"}
//   {"t":"err","msg":"…","reason":"AUTH_DENIED|…"}
//
// Phase 1c: single drawer, one PTY at a time. Multi-pane is a Phase 2+
// concern. The Esc key closes the drawer.

(function () {
	'use strict';

	let term = null;       // xterm.Terminal instance
	let fitAddon = null;   // xterm-addon-fit
	let ws = null;         // active WebSocket
	let currentBox = null; // {id, name} for status bar
	let resizeObserver = null;

	function $(id) { return document.getElementById(id); }

	function setStatus(text, tone) {
		const el = $('console-drawer-status');
		if (!el) return;
		el.textContent = text;
		el.classList.remove('bg-slate-700', 'bg-emerald-700', 'bg-red-700', 'bg-yellow-700');
		const cls = {
			connecting: 'bg-yellow-700',
			connected: 'bg-emerald-700',
			closed: 'bg-slate-700',
			error: 'bg-red-700',
		}[tone] || 'bg-slate-700';
		el.classList.add(cls);
	}

	function setMode(mode, uid) {
		const m = $('console-drawer-mode');
		const u = $('console-drawer-uid');
		if (m) m.textContent = mode ? 'mode=' + mode : '';
		if (u) u.textContent = (uid === undefined || uid === null) ? '' : 'uid=' + uid;
	}

	// base64 encode/decode for binary-safe data frames. The dashboard
	// pipes them through unchanged; the agent does the actual decode
	// when writing to the PTY.
	function b64encode(bytes) {
		let bin = '';
		for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
		return btoa(bin);
	}
	function b64decode(s) {
		const bin = atob(s);
		const out = new Uint8Array(bin.length);
		for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
		return out;
	}

	function sendData(bytes) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({ t: 'data', d: b64encode(bytes) }));
	}

	function sendResize(cols, rows) {
		if (!ws || ws.readyState !== WebSocket.OPEN) return;
		ws.send(JSON.stringify({ t: 'resize', cols: cols, rows: rows }));
	}

	function fitNow() {
		if (!fitAddon) return;
		try {
			fitAddon.fit();
			if (term) sendResize(term.cols, term.rows);
		} catch (e) {
			// xterm.fit can throw if the container has zero size during
			// transitions — ignore and let the next frame retry.
		}
	}

	function ensureTerm() {
		if (term) return;
		if (typeof Terminal === 'undefined') {
			console.error('[console] xterm.js not loaded');
			return;
		}
		term = new Terminal({
			cursorBlink: true,
			fontFamily: 'ui-monospace, "SF Mono", Menlo, Consolas, monospace',
			fontSize: 13,
			theme: {
				background: '#000000',
				foreground: '#e5e7eb',
				cursor: '#34d399',
			},
			scrollback: 5000,
			allowProposedApi: true,
		});
		fitAddon = new FitAddon.FitAddon();
		term.loadAddon(fitAddon);
		term.open($('console-xterm'));

		// Browser → server: every keystroke is a data frame. xterm
		// emits already-encoded VT sequences (arrow keys, Ctrl-C as
		// 0x03, etc.), so we just byte-encode and ship.
		term.onData(function (data) {
			const enc = new TextEncoder();
			sendData(enc.encode(data));
		});

		// Resize on container changes (drawer open, viewport resize).
		resizeObserver = new ResizeObserver(function () {
			fitNow();
		});
		resizeObserver.observe($('console-xterm'));
	}

	function openWS(boxID) {
		const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = proto + '//' + window.location.host + '/api/console/' + encodeURIComponent(boxID) + '/ws';
		ws = new WebSocket(url);
		ws.binaryType = 'arraybuffer';

		ws.onopen = function () {
			setStatus('connected', 'connected');
			// Fit on next frame so the container has its final size.
			requestAnimationFrame(fitNow);
		};

		ws.onmessage = function (ev) {
			let frame;
			try {
				frame = JSON.parse(ev.data);
			} catch (e) {
				return;
			}
			switch (frame.t) {
				case 'data':
					if (term && frame.d) {
						const bytes = b64decode(frame.d);
						const dec = new TextDecoder('utf-8', { fatal: false });
						term.write(dec.decode(bytes));
					}
					break;
				case 'exit':
					setStatus('exited (' + (frame.code !== undefined ? frame.code : '?') + ')', 'closed');
					break;
				case 'err':
					setStatus('error: ' + (frame.reason || frame.msg || 'unknown'), 'error');
					if (term) {
						term.write('\r\n\x1b[31m[console] ' + (frame.msg || frame.reason) + '\x1b[0m\r\n');
					}
					break;
			}
		};

		ws.onerror = function () {
			setStatus('error', 'error');
		};

		ws.onclose = function () {
			setStatus('closed', 'closed');
		};
	}

	function fetchCapsAndOpen(boxID) {
		fetch('/api/console/' + encodeURIComponent(boxID) + '/capabilities', {
			credentials: 'same-origin',
		}).then(function (r) {
			if (!r.ok) {
				return r.json().then(function (j) {
					throw new Error(j.reason || ('HTTP ' + r.status));
				}, function () {
					throw new Error('HTTP ' + r.status);
				});
			}
			return r.json();
		}).then(function (caps) {
			if (!caps.enabled) {
				setStatus('disabled on agent', 'error');
				return;
			}
			setMode(caps.mode, caps.default_uid);
			openWS(boxID);
		}).catch(function (e) {
			setStatus('error: ' + e.message, 'error');
		});
	}

	window.openConsole = function (boxID, boxName) {
		const drawer = $('console-drawer');
		if (!drawer) {
			console.error('[console] drawer markup missing — is ConsoleDrawer() in the layout?');
			return;
		}
		currentBox = { id: boxID, name: boxName || boxID };
		const boxEl = $('console-drawer-box');
		if (boxEl) boxEl.textContent = currentBox.name;
		setMode('', null);
		setStatus('connecting…', 'connecting');
		drawer.classList.remove('hidden');
		drawer.classList.add('flex');
		document.body.style.overflow = 'hidden';
		ensureTerm();
		fetchCapsAndOpen(boxID);
		requestAnimationFrame(fitNow);
		if (term) term.focus();
	};

	window.closeConsole = function () {
		if (ws) {
			try { ws.close(1000, 'user closed'); } catch (e) {}
			ws = null;
		}
		const drawer = $('console-drawer');
		if (drawer) {
			drawer.classList.add('hidden');
			drawer.classList.remove('flex');
		}
		document.body.style.overflow = '';
		currentBox = null;
		setMode('', null);
		setStatus('closed', 'closed');
		// Note: we keep the xterm instance around for the next
		// session so scrollback isn't lost during a quick reconnect.
		if (term) term.reset();
	};

	// Wire up the close button + Esc once DOM is ready.
	document.addEventListener('DOMContentLoaded', function () {
		const btn = $('console-drawer-close');
		if (btn) btn.addEventListener('click', window.closeConsole);
		document.addEventListener('keydown', function (e) {
			if (e.key === 'Escape') {
				const drawer = $('console-drawer');
				if (drawer && !drawer.classList.contains('hidden')) {
					window.closeConsole();
				}
			}
		});
	});
})();
