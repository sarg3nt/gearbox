// console.js — multi-session console manager.
//
// Public API:
//   window.openConsole(boxID, boxName)          — backwards-compat shim
//   window.gearbox.console.open(descriptor)     — canonical entry point
//   window.gearbox.console.close()              — close active session
//   window.gearbox.console.setLayout(mode)      — 'drawer'|'dock'|'popout'
//
// Design notes
// ============
// Sessions are the unit of work — one PTY, one xterm, one WebSocket. The
// manager owns the chrome (tabs, toolbar, layout) and routes input/output
// to the active session. This is intentionally pluggable: a future Docker
// "exec into container" gear will just hand the manager a different URL
// descriptor; the rest of the UI is identical.
//
// A "descriptor" is a plain object describing how to open a session:
//   { kind: 'box', boxID, label }
//   { kind: 'container', boxID, containerID, label }  (future)
//
// The URL builder lives in one place (urlForDescriptor / capsURLForDescriptor),
// so adding new session kinds means adding one case to each builder.
//
// Layouts
// -------
// drawer: fullscreen overlay (the original behavior). Single visible chrome.
// dock:   pinned to bottom of viewport, resizable via a drag handle on top,
//         height persisted to localStorage. Main page content stays usable.
// popout: handled by /console/popout/{boxID} — a separate window that hosts
//         its own ConsoleManager in drawer mode. The popout button here just
//         calls window.open() and lets the destination page take over.
//
// Tabs are visible in drawer and dock layouts; one session at a time is
// "active" (focused, fitted, receiving keystrokes). Inactive sessions stay
// connected in the background — their xterm just isn't visible. Closing a
// tab destroys its session (PTY + WS).
//
// Preferences (localStorage, namespaced gearbox.console.*)
// --------------------------------------------------------
//   fontSize    integer 10..24, default 13
//   dockHeight  integer px, default 320
//   layout      'drawer'|'dock', default 'drawer' (popout is transient)

(function () {
    'use strict';

    /* ============================================================== *
     * Constants
     * ============================================================== */

    const PREF_PREFIX = 'gearbox.console.';
    const FONT_MIN = 10;
    const FONT_MAX = 24;
    const FONT_DEFAULT = 13;
    const DOCK_MIN = 160;
    const DOCK_DEFAULT = 320;

    /* ============================================================== *
     * Helpers
     * ============================================================== */

    function clearChildren(node) {
        while (node.firstChild) node.removeChild(node.firstChild);
    }

    function clampInt(n, lo, hi) {
        n = parseInt(n, 10);
        if (!isFinite(n)) n = lo;
        return Math.max(lo, Math.min(hi, n));
    }

    /* ============================================================== *
     * Preferences (localStorage with safe fallback)
     * ============================================================== */

    function prefGet(key, fallback) {
        try {
            const v = localStorage.getItem(PREF_PREFIX + key);
            if (v == null) return fallback;
            if (typeof fallback === 'number') {
                const n = parseInt(v, 10);
                return isFinite(n) ? n : fallback;
            }
            return v;
        } catch (_) { return fallback; }
    }
    function prefSet(key, value) {
        try { localStorage.setItem(PREF_PREFIX + key, String(value)); } catch (_) {}
    }

    /* ============================================================== *
     * Descriptor URLs — single place to extend for new session kinds
     * ============================================================== */

    function wsURLForDescriptor(d) {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const host = window.location.host;
        if (d.kind === 'box') {
            return proto + '//' + host + '/api/console/' + encodeURIComponent(d.boxID) + '/ws';
        }
        throw new Error('unknown console descriptor kind: ' + d.kind);
    }

    function capsURLForDescriptor(d) {
        if (d.kind === 'box') {
            return '/api/console/' + encodeURIComponent(d.boxID) + '/capabilities';
        }
        throw new Error('unknown console descriptor kind: ' + d.kind);
    }

    // Every session gets a unique numeric ID. We intentionally do NOT key
    // by descriptor — opening the same box twice should produce two
    // independent tabs (matches terminal-app convention; the user asked
    // for this explicitly).
    let _sessionCounter = 0;
    function nextSessionID() { _sessionCounter += 1; return 's' + _sessionCounter; }

    /* ============================================================== *
     * Base64 helpers (binary-safe data frames)
     * ============================================================== */

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

    /* ============================================================== *
     * ConsoleSession — one PTY, one xterm, one WebSocket
     * ============================================================== */

    function ConsoleSession(descriptor) {
        this.id = nextSessionID();
        this.descriptor = descriptor;
        this.label = descriptor.label || descriptor.boxID || this.id;
        this.status = 'idle';
        this.mode = '';
        this.uid = null;
        this.errorMsg = '';
        this.term = null;
        this.fitAddon = null;
        this.ws = null;
        this.host = null;            // wrapper that gets attached/detached
        this.onStatusChange = null;  // ConsoleManager wires this
        this.fontSize = FONT_DEFAULT;
    }

    ConsoleSession.prototype._setStatus = function (status) {
        this.status = status;
        if (typeof this.onStatusChange === 'function') {
            try { this.onStatusChange(this); } catch (_) {}
        }
    };

    ConsoleSession.prototype._ensureTerm = function () {
        if (this.term) return;
        if (typeof Terminal === 'undefined') {
            console.error('[console] xterm.js not loaded');
            return;
        }
        this.host = document.createElement('div');
        this.host.className = 'console-session-host';
        this.term = new Terminal({
            cursorBlink: true,
            fontFamily: 'ui-monospace, "SF Mono", Menlo, Consolas, monospace',
            fontSize: this.fontSize,
            theme: { background: '#000000', foreground: '#e5e7eb', cursor: '#34d399' },
            scrollback: 5000,
            allowProposedApi: true,
        });
        this.fitAddon = new FitAddon.FitAddon();
        this.term.loadAddon(this.fitAddon);
        this.term.open(this.host);

        const self = this;
        this.term.onData(function (data) {
            const enc = new TextEncoder();
            self._sendData(enc.encode(data));
        });
    };

    ConsoleSession.prototype.attach = function (parent) {
        this._ensureTerm();
        if (!this.host) return;
        if (this.host.parentNode !== parent) {
            parent.appendChild(this.host);
        }
        this.host.style.display = '';
        // Defer fit until the layout has applied size — fit on a zero-sized
        // container silently sets cols=0.
        requestAnimationFrame(this.fit.bind(this));
    };

    ConsoleSession.prototype.detach = function () {
        if (this.host) this.host.style.display = 'none';
    };

    ConsoleSession.prototype.focus = function () {
        if (this.term) this.term.focus();
    };

    ConsoleSession.prototype.fit = function () {
        if (!this.fitAddon) return;
        try {
            this.fitAddon.fit();
            if (this.term && this.ws && this.ws.readyState === WebSocket.OPEN) {
                this._sendResize(this.term.cols, this.term.rows);
            }
        } catch (_) { /* zero-size container during transitions */ }
    };

    ConsoleSession.prototype.setFontSize = function (size) {
        this.fontSize = size;
        if (this.term) {
            this.term.options.fontSize = size;
            this.fit();
        }
    };

    ConsoleSession.prototype.clear = function () {
        if (this.term) this.term.clear();
    };

    ConsoleSession.prototype._sendData = function (bytes) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        this.ws.send(JSON.stringify({ t: 'data', d: b64encode(bytes) }));
    };

    ConsoleSession.prototype._sendResize = function (cols, rows) {
        if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
        this.ws.send(JSON.stringify({ t: 'resize', cols: cols, rows: rows }));
    };

    // Emit a visible error line into the xterm buffer. The status dot
    // also reflects the state via color, but the terminal needs its own
    // surface so a failing session isn't a silent black void. Matches
    // the server-side 'err' frame path which already writes red text.
    ConsoleSession.prototype._writeError = function (msg) {
        if (!this.term) return;
        this.term.write('\r\n\x1b[31m[console] ' + msg + '\x1b[0m\r\n');
    };

    ConsoleSession.prototype.connect = function () {
        this._ensureTerm();
        if (this.ws) { try { this.ws.close(); } catch (_) {} }
        this._setStatus('connecting');
        const self = this;
        fetch(capsURLForDescriptor(this.descriptor), { credentials: 'same-origin' })
            .then(function (r) {
                if (!r.ok) {
                    return r.json().then(function (j) { throw new Error(j.reason || ('HTTP ' + r.status)); },
                                         function ()  { throw new Error('HTTP ' + r.status); });
                }
                return r.json();
            })
            .then(function (caps) {
                if (!caps.enabled) {
                    self.mode = '';
                    self.uid = null;
                    self.errorMsg = 'disabled on agent';
                    self._writeError('console disabled on this agent');
                    self._setStatus('error');
                    return;
                }
                self.mode = caps.mode || '';
                self.uid = (caps.default_uid === undefined ? null : caps.default_uid);
                self._openWS();
            })
            .catch(function (e) {
                self.errorMsg = e.message;
                self._writeError('capabilities fetch failed: ' + e.message);
                self._setStatus('error');
            });
    };

    ConsoleSession.prototype._openWS = function () {
        const self = this;
        const ws = new WebSocket(wsURLForDescriptor(this.descriptor));
        ws.binaryType = 'arraybuffer';
        this.ws = ws;

        ws.onopen = function () {
            self._setStatus('connected');
            requestAnimationFrame(self.fit.bind(self));
        };
        ws.onmessage = function (ev) {
            let frame;
            try { frame = JSON.parse(ev.data); } catch (e) { return; }
            switch (frame.t) {
                case 'data':
                    if (self.term && frame.d) {
                        const bytes = b64decode(frame.d);
                        const dec = new TextDecoder('utf-8', { fatal: false });
                        self.term.write(dec.decode(bytes));
                    }
                    break;
                case 'exit':
                    self.errorMsg = 'exited (' + (frame.code !== undefined ? frame.code : '?') + ')';
                    self._setStatus('closed');
                    break;
                case 'err':
                    self.errorMsg = frame.reason || frame.msg || 'unknown';
                    self._setStatus('error');
                    if (self.term) {
                        self.term.write('\r\n\x1b[31m[console] ' + (frame.msg || frame.reason) + '\x1b[0m\r\n');
                    }
                    break;
            }
        };
        ws.onerror = function () {
            self.errorMsg = 'socket error';
            self._writeError('websocket error');
            self._setStatus('error');
        };
        ws.onclose = function () {
            if (self.status !== 'error' && self.status !== 'closed') {
                self._setStatus('closed');
            }
        };
    };

    ConsoleSession.prototype.destroy = function () {
        if (this.ws) { try { this.ws.close(1000, 'user closed'); } catch (_) {} }
        this.ws = null;
        if (this.term) { try { this.term.dispose(); } catch (_) {} }
        this.term = null;
        this.fitAddon = null;
        if (this.host && this.host.parentNode) this.host.parentNode.removeChild(this.host);
        this.host = null;
        this._setStatus('closed');
    };

    /* ============================================================== *
     * ConsoleManager — owns chrome, sessions, layout, prefs
     * ============================================================== */

    function ConsoleManager() {
        this.sessions = [];          // ordered list
        this.activeID = null;
        this.layout = prefGet('layout', 'drawer');
        if (this.layout !== 'drawer' && this.layout !== 'dock') this.layout = 'drawer';
        this.fontSize = clampInt(prefGet('fontSize', FONT_DEFAULT), FONT_MIN, FONT_MAX);
        this.dockHeight = clampInt(prefGet('dockHeight', DOCK_DEFAULT), DOCK_MIN, 2000);
        this.popoutMode = false;     // set true by the popout page
        this._wired = false;
    }

    ConsoleManager.prototype._$ = function (id) { return document.getElementById(id); };

    ConsoleManager.prototype._wire = function () {
        if (this._wired) return;
        const drawer = this._$('console-drawer');
        if (!drawer) return; // markup not on this page

        const close = this._$('console-drawer-close');
        if (close) close.addEventListener('click', this.close.bind(this));

        const layoutDrawerBtn = this._$('console-btn-layout-drawer');
        const layoutDockBtn = this._$('console-btn-layout-dock');
        const layoutPopoutBtn = this._$('console-btn-layout-popout');
        const clearBtn = this._$('console-btn-clear');
        const newTabBtn = this._$('console-btn-newtab');
        const slider = this._$('console-font-slider');
        const sliderVal = this._$('console-font-value');

        if (layoutDrawerBtn) layoutDrawerBtn.addEventListener('click', this.setLayout.bind(this, 'drawer'));
        if (layoutDockBtn) layoutDockBtn.addEventListener('click', this.setLayout.bind(this, 'dock'));
        if (layoutPopoutBtn) layoutPopoutBtn.addEventListener('click', this._popoutActive.bind(this));
        if (clearBtn) clearBtn.addEventListener('click', this._clearActive.bind(this));
        if (newTabBtn) newTabBtn.addEventListener('click', this._newTabActive.bind(this));

        if (slider) {
            slider.min = String(FONT_MIN);
            slider.max = String(FONT_MAX);
            slider.value = String(this.fontSize);
            if (sliderVal) sliderVal.textContent = this.fontSize + 'px';
            slider.addEventListener('input', this._onFontSlider.bind(this));
        }

        const self = this;
        document.addEventListener('keydown', function (e) {
            if (e.key !== 'Escape') return;
            if (drawer.classList.contains('hidden')) return;
            // In a popout window Esc would just empty the page — let the
            // user close the window with the OS shortcut instead.
            if (self.popoutMode) return;
            if (self.layout === 'dock' && self.sessions.length > 1) {
                self.closeSession(self.activeID);
            } else {
                self.close();
            }
        });

        window.addEventListener('resize', function () {
            const s = self._active();
            if (s) s.fit();
        });

        this._wireDockResize();
        this._wired = true;
    };

    ConsoleManager.prototype._wireDockResize = function () {
        const handle = this._$('console-dock-handle');
        if (!handle) return;
        const self = this;
        let dragging = false;
        let pointerID = null;
        let startY = 0;
        let startH = 0;

        // Pointer Events with setPointerCapture so drag state survives
        // the cursor leaving the window — mouseup-outside used to leave
        // the dock in a stuck "still resizing" state. visibilitychange
        // and blur are additional belt-and-suspenders cleanups.
        const stopDrag = function () {
            if (!dragging) return;
            dragging = false;
            try { if (pointerID !== null) handle.releasePointerCapture(pointerID); } catch (_) {}
            pointerID = null;
            document.body.style.cursor = '';
            document.body.style.userSelect = '';
            prefSet('dockHeight', self.dockHeight);
            const s = self._active();
            if (s) s.fit();
        };

        handle.addEventListener('pointerdown', function (e) {
            dragging = true;
            pointerID = e.pointerId;
            startY = e.clientY;
            startH = self.dockHeight;
            document.body.style.cursor = 'row-resize';
            document.body.style.userSelect = 'none';
            try { handle.setPointerCapture(e.pointerId); } catch (_) {}
            e.preventDefault();
        });
        handle.addEventListener('pointermove', function (e) {
            if (!dragging || e.pointerId !== pointerID) return;
            const dy = startY - e.clientY;
            self.dockHeight = clampInt(startH + dy, DOCK_MIN, window.innerHeight - 100);
            self._applyDockHeight();
        });
        handle.addEventListener('pointerup', stopDrag);
        handle.addEventListener('pointercancel', stopDrag);
        // If the user alt-tabs or the page is hidden mid-drag, treat it
        // as drop — without these the body cursor/userSelect overrides
        // can linger after the user returns.
        window.addEventListener('blur', stopDrag);
        document.addEventListener('visibilitychange', function () {
            if (document.hidden) stopDrag();
        });
    };

    ConsoleManager.prototype._applyDockHeight = function () {
        const drawer = this._$('console-drawer');
        if (!drawer) return;
        if (this.layout === 'dock') {
            drawer.style.height = this.dockHeight + 'px';
            // Keep body padding-bottom in sync so main page content
            // isn't hidden under the dock when its height changes.
            document.documentElement.style.setProperty('--gearbox-console-dock-h', this.dockHeight + 'px');
        } else {
            drawer.style.height = '';
        }
    };

    ConsoleManager.prototype._active = function () {
        if (!this.activeID) return null;
        for (let i = 0; i < this.sessions.length; i++) {
            if (this.sessions[i].id === this.activeID) return this.sessions[i];
        }
        return null;
    };

    ConsoleManager.prototype._onFontSlider = function (e) {
        const v = clampInt(e.target.value, FONT_MIN, FONT_MAX);
        this.fontSize = v;
        prefSet('fontSize', v);
        const valEl = this._$('console-font-value');
        if (valEl) valEl.textContent = v + 'px';
        for (let i = 0; i < this.sessions.length; i++) {
            this.sessions[i].setFontSize(v);
        }
    };

    /* ---------------- session lifecycle ---------------- */

    ConsoleManager.prototype.open = function (descriptor) {
        this._wire();
        const drawer = this._$('console-drawer');
        if (!drawer) {
            console.error('[console] drawer markup missing');
            return null;
        }
        // Always create a new session, even if one already targets the
        // same box — terminal-app convention is "another click = another
        // tab." Tab labels disambiguate duplicates with a #N suffix.
        const sess = new ConsoleSession(descriptor);
        sess.fontSize = this.fontSize;
        sess.onStatusChange = this._renderStatus.bind(this);
        this.sessions.push(sess);
        this._setActive(sess.id);
        this._show();
        sess.connect();
        return sess;
    };

    ConsoleManager.prototype.closeSession = function (id) {
        const idx = this._indexByID(id);
        if (idx < 0) return;
        const sess = this.sessions[idx];
        sess.destroy();
        this.sessions.splice(idx, 1);
        if (this.activeID === id) {
            const next = this.sessions[idx] || this.sessions[idx - 1] || null;
            this.activeID = next ? next.id : null;
            if (!next) {
                this.close();
                return;
            }
        }
        this._renderChrome();
        const active = this._active();
        if (active) active.attach(this._$('console-xterm'));
    };

    ConsoleManager.prototype.close = function () {
        while (this.sessions.length) this.sessions.pop().destroy();
        this.activeID = null;
        const drawer = this._$('console-drawer');
        if (drawer) {
            drawer.classList.add('hidden');
            drawer.classList.remove('flex');
            drawer.classList.remove('console-layout-dock');
            drawer.classList.remove('console-layout-drawer');
            drawer.style.height = '';
        }
        document.body.style.overflow = '';
        document.body.classList.remove('console-dock-open');
    };

    /* ---------------- visibility / layout ---------------- */

    ConsoleManager.prototype._show = function () {
        const drawer = this._$('console-drawer');
        if (!drawer) return;
        drawer.classList.remove('hidden');
        drawer.classList.add('flex');
        this._applyLayout();
        this._renderChrome();
        const active = this._active();
        if (active) {
            active.attach(this._$('console-xterm'));
            active.focus();
        }
    };

    ConsoleManager.prototype.setLayout = function (mode) {
        if (mode !== 'drawer' && mode !== 'dock') return;
        this.layout = mode;
        prefSet('layout', mode);
        this._applyLayout();
        const s = this._active();
        if (s) requestAnimationFrame(s.fit.bind(s));
    };

    ConsoleManager.prototype._applyLayout = function () {
        const drawer = this._$('console-drawer');
        if (!drawer) return;
        drawer.classList.remove('console-layout-drawer', 'console-layout-dock');
        drawer.classList.add('console-layout-' + this.layout);

        if (this.layout === 'drawer') {
            document.body.style.overflow = 'hidden';
            document.body.classList.remove('console-dock-open');
            drawer.style.height = '';
        } else if (this.layout === 'dock') {
            document.body.style.overflow = '';
            document.body.classList.add('console-dock-open');
            this._applyDockHeight();
        }

        // Drawer and dock buttons are mutually exclusive — only the
        // button representing the *other* layout is visible, so clicking
        // it visibly toggles state. The popout button stays visible.
        const drawerBtn = this._$('console-btn-layout-drawer');
        const dockBtn = this._$('console-btn-layout-dock');
        if (drawerBtn) drawerBtn.classList.toggle('hidden', this.layout === 'drawer');
        if (dockBtn) dockBtn.classList.toggle('hidden', this.layout === 'dock');
    };

    ConsoleManager.prototype._popoutActive = function () {
        const s = this._active();
        if (!s) return;
        if (s.descriptor.kind !== 'box') return;
        const url = '/console/popout/' + encodeURIComponent(s.descriptor.boxID);
        window.open(url, '_blank', 'popup=yes,noopener=yes,width=1100,height=700');
    };

    ConsoleManager.prototype._clearActive = function () {
        const s = this._active();
        if (s) { s.clear(); s.focus(); }
    };

    // Click handler for a per-tab status dot. Reconnects when the
    // session is in a terminal non-connected state (closed / error).
    // Other states (connected / connecting / idle) ignore the click —
    // we don't want to rip a live shell or stack a second connect on
    // top of one already in flight. Also makes the clicked tab active
    // so the reconnect happens where the user can see it.
    ConsoleManager.prototype._tabDotClick = function (sessionID) {
        const idx = this._indexByID(sessionID);
        if (idx < 0) return;
        const s = this.sessions[idx];
        if (s.status !== 'closed' && s.status !== 'error') return;
        this._setActive(sessionID);
        s.connect();
        s.focus();
    };

    // Spawn a new session against the same descriptor as the active tab.
    // Bound to the "+" button in the toolbar; pairs with the bx-grid
    // behavior where each shell-icon click opens an additional tab.
    ConsoleManager.prototype._newTabActive = function () {
        const s = this._active();
        if (!s) return;
        // Reuse the descriptor verbatim — the label stays the box name and
        // _renderTabs disambiguates duplicates with a #N suffix.
        this.open(s.descriptor);
    };

    /* ---------------- session lookup ---------------- */

    ConsoleManager.prototype._indexByID = function (id) {
        for (let i = 0; i < this.sessions.length; i++) {
            if (this.sessions[i].id === id) return i;
        }
        return -1;
    };

    ConsoleManager.prototype._setActive = function (id) {
        for (let i = 0; i < this.sessions.length; i++) {
            if (this.sessions[i].id !== id) this.sessions[i].detach();
        }
        this.activeID = id;
        const xtermHost = this._$('console-xterm');
        const active = this._active();
        if (active && xtermHost) {
            active.attach(xtermHost);
            active.focus();
        }
        this._renderChrome();
    };

    /* ---------------- chrome (tabs + status + toolbar) ---------------- */

    ConsoleManager.prototype._renderChrome = function () {
        this._renderTabs();
        this._renderStatus();
    };

    ConsoleManager.prototype._renderTabs = function () {
        const strip = this._$('console-tabs');
        const bar = this._$('console-tab-bar');
        if (!strip || !bar) return;
        if (this.sessions.length === 0) {
            bar.classList.add('hidden');
            clearChildren(strip);
            return;
        }
        bar.classList.remove('hidden');
        clearChildren(strip);

        // Disambiguate duplicate labels with a "#N" suffix. We only
        // suffix when there's actually a conflict — a single "Light
        // Hugger" stays "Light Hugger"; two of them become "Light
        // Hugger #1" and "Light Hugger #2". Counts reset on every
        // render so closing a duplicate cleans up the survivor.
        const labelCounts = {};
        this.sessions.forEach(function (s) {
            labelCounts[s.label] = (labelCounts[s.label] || 0) + 1;
        });
        const labelIdx = {};

        const self = this;
        this.sessions.forEach(function (s) {
            labelIdx[s.label] = (labelIdx[s.label] || 0) + 1;
            const display = labelCounts[s.label] > 1
                ? s.label + ' #' + labelIdx[s.label]
                : s.label;

            // Tab wrapper is layout-only — the focusable element with
            // role="tab" is the label button. This keeps ARIA semantics
            // happy: nested interactive children (dot, close) sit inside
            // the wrapper but are siblings of the role="tab" element,
            // not nested inside it. See Copilot review on PR #141.
            const tab = document.createElement('div');
            tab.className = 'console-tab' + (s.id === self.activeID ? ' active' : '');
            tab.setAttribute('role', 'presentation');

            // Per-tab status dot. Doubles as a reconnect button when the
            // session is closed/errored — clicking switches active to
            // this tab and kicks off a reconnect. Connected / connecting
            // / idle dots are inert.
            const dot = document.createElement('button');
            dot.type = 'button';
            dot.className = 'console-tab-dot is-' + s.status;
            const isReconnectable = s.status === 'closed' || s.status === 'error';
            const dotStateText = ({
                connected: 'Connected',
                connecting: 'Connecting',
                idle: 'Idle',
                closed: 'Closed',
                error: 'Error: ' + (s.errorMsg || 'unknown'),
            })[s.status] || s.status;
            if (isReconnectable) {
                dot.classList.add('is-clickable');
                dot.title = dotStateText + ' — click to reconnect';
                dot.setAttribute('aria-label', display + ' — ' + dotStateText + '. Click to reconnect.');
                dot.removeAttribute('aria-disabled');
            } else {
                dot.title = dotStateText;
                dot.setAttribute('aria-label', display + ' — ' + dotStateText);
                dot.setAttribute('aria-disabled', 'true');
            }
            dot.addEventListener('click', function (e) {
                e.stopPropagation();
                if (isReconnectable) self._tabDotClick(s.id);
            });

            const label = document.createElement('button');
            label.type = 'button';
            label.className = 'console-tab-label';
            label.textContent = display;
            label.title = display + ' (' + s.status + ')';
            // Label button carries the role="tab" semantics so screen
            // readers expose the tab list correctly (one role="tab" per
            // tab, focusable, with aria-selected reflecting active).
            label.setAttribute('role', 'tab');
            label.setAttribute('aria-selected', s.id === self.activeID ? 'true' : 'false');
            label.addEventListener('click', function () { self._setActive(s.id); });

            const close = document.createElement('button');
            close.type = 'button';
            close.className = 'console-tab-close';
            close.setAttribute('aria-label', 'Close ' + display);
            close.title = 'Close tab';
            close.textContent = '×';
            close.addEventListener('click', function (e) {
                e.stopPropagation();
                self.closeSession(s.id);
            });

            tab.appendChild(dot);
            tab.appendChild(label);
            tab.appendChild(close);
            strip.appendChild(tab);
        });
    };

    // Header has no connection state to render anymore — the per-tab
    // dots own that. We still re-render the tab strip on every status
    // change so the dot colors and tooltips stay accurate.
    ConsoleManager.prototype._renderStatus = function () {
        this._renderTabs();
    };

    /* ============================================================== *
     * Wire-up and public API
     * ============================================================== */

    const manager = new ConsoleManager();

    window.openConsole = function (boxID, boxName) {
        manager.open({ kind: 'box', boxID: boxID, label: boxName || boxID });
    };
    window.closeConsole = function () { manager.close(); };

    window.gearbox = window.gearbox || {};
    window.gearbox.console = {
        open: function (d) { return manager.open(d); },
        openBox: function (boxID, label) {
            return manager.open({ kind: 'box', boxID: boxID, label: label || boxID });
        },
        close: function () { manager.close(); },
        closeSession: function (id) { manager.closeSession(id); },
        setLayout: function (mode) { manager.setLayout(mode); },
        manager: manager,
    };

    window.gearbox.console.markPopout = function () {
        manager.popoutMode = true;
        manager.layout = 'drawer';
        // Tag the drawer so CSS can drop the header + tab bar — in a
        // popout window the chrome is wasted vertical space, none of
        // the layout/popout buttons make sense (we're already popped
        // out), and Cmd-W/closing the tab is the natural way to exit.
        const drawer = document.getElementById('console-drawer');
        if (drawer) drawer.classList.add('console-popout');
    };
})();
