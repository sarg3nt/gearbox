/**
 * CodeMirror 5 syntax mode + hint provider for nftables ruleset files.
 *
 * Recognizes the high-level grammar from nft(8) — families/hooks/policies,
 * match expressions, verdict statements, ct/meta/ip/ip6/tcp/udp/icmp
 * keywords — plus numeric literals (decimal, hex, ports, CIDR/IP), quoted
 * strings, comments (`#...`), and the punctuation that separates rule
 * elements (`{ } ; , :`).
 *
 * Both pieces (mode + hints) live in one file so the templ page only has
 * to pull in one script. The mode is registered as `nftables`; the hint
 * provider hooks into CodeMirror's `show-hint` addon and is invoked from
 * the editor JS on Ctrl-Space and as-you-type.
 */
(function (CM) {
	'use strict';

	// ----- Keyword groupings ---------------------------------------------
	// These drive both the mode (which colorizes them) and the hint
	// provider (which suggests them). Keeping the lists in one place
	// means a typo in one is a typo in both.

	// Top-level container statements + structural words.
	const STATEMENTS = [
		'table', 'chain', 'set', 'map', 'flowtable', 'counter', 'quota',
		'secmark', 'synproxy', 'limit', 'helper', 'rule', 'element',
		'define', 'redefine', 'undefine', 'include',
	];

	// Address families.
	const FAMILIES = ['ip', 'ip6', 'inet', 'arp', 'bridge', 'netdev'];

	// Chain `type`s and the netfilter hooks they attach to.
	const CHAIN_TYPES = ['filter', 'nat', 'route'];
	const HOOKS = [
		'prerouting', 'input', 'forward', 'output', 'postrouting',
		'ingress', 'egress',
	];

	// Verdict statements + tokens that close a rule chain.
	const VERDICTS = [
		'accept', 'drop', 'reject', 'queue', 'continue', 'return',
		'jump', 'goto',
	];

	// Action / mutator statements (everything else a rule can DO).
	const STATEMENTS_RHS = [
		'log', 'counter', 'snat', 'dnat', 'masquerade', 'redirect',
		'tproxy', 'dup', 'fwd', 'notrack', 'meter', 'limit', 'reject',
		'set', 'add', 'update', 'delete', 'flush',
	];

	// Match expression keywords — protocol families, ct primitives, etc.
	const MATCHES = [
		'meta', 'ct', 'tcp', 'udp', 'udplite', 'sctp', 'dccp',
		'icmp', 'icmpv6', 'ip', 'ip6', 'arp', 'ether', 'vlan',
		'iif', 'oif', 'iifname', 'oifname', 'iiftype', 'oiftype',
		'mark', 'priority', 'protocol', 'nfproto', 'l4proto',
		'state', 'status', 'direction', 'expiration',
		'length', 'dscp', 'flowlabel', 'hoplimit', 'ttl', 'id',
		'saddr', 'daddr', 'sport', 'dport', 'flags', 'type', 'code',
		'sequence', 'ack', 'window', 'urgptr', 'checksum', 'nexthdr',
		'frag-off', 'hdrlength', 'version',
	];

	// Operator keywords — the punctuation operators (`==`, `!=`, etc.) are
	// handled in the tokenizer below; this list is the word-shaped ones.
	const OP_KEYWORDS = ['and', 'or', 'xor', 'not', 'in', 'vmap', 'numgen'];

	// Common literal values you'd see on the RHS of an expression. ct
	// states + tcp flags + icmp types most commonly trigger autocomplete.
	const VALUES = [
		// ct state
		'new', 'established', 'related', 'untracked', 'invalid',
		// tcp flags
		'syn', 'ack', 'fin', 'rst', 'psh', 'urg', 'ecn', 'cwr',
		// limit units
		'second', 'minute', 'hour', 'day', 'week', 'over',
		// log levels (used after `log level`)
		'emerg', 'alert', 'crit', 'err', 'warn', 'notice', 'info', 'debug',
		// reject types
		'unreachable', 'port-unreachable', 'host-unreachable',
		'net-unreachable', 'prot-unreachable', 'tcp', 'reset', 'admin-prohibited',
		// icmp types (common subset)
		'echo-request', 'echo-reply', 'destination-unreachable',
		'time-exceeded', 'parameter-problem', 'redirect',
	];

	// Flatten into a `Set` for O(1) membership checks in the tokenizer.
	const KEYWORDS = new Set([
		...STATEMENTS, ...FAMILIES, ...CHAIN_TYPES, ...HOOKS,
		...VERDICTS, ...STATEMENTS_RHS, ...MATCHES, ...OP_KEYWORDS,
	]);

	// All known words for the hint dropdown. Dedup via Set.
	const ALL_HINTS = Array.from(new Set([
		...STATEMENTS, ...FAMILIES, ...CHAIN_TYPES, ...HOOKS,
		...VERDICTS, ...STATEMENTS_RHS, ...MATCHES, ...OP_KEYWORDS,
		...VALUES,
		// A handful of "next-word" suggestions that follow common rule
		// starts but aren't standalone keywords. Including them in the
		// hint list lets the user discover them via tab-cycling without
		// us needing a context-aware parser.
		'hook', 'priority', 'policy', 'devices', 'flags', 'comment',
		'numgen inc', 'numgen random', 'jhash', 'symhash',
	])).sort();

	// ----- Mode -----------------------------------------------------------
	// One-pass stream tokenizer. We don't track grammatical context (e.g.
	// "inside a `table` block vs at top-level") because the highlighting
	// is good enough without it and the parser stays cheap.

	CM.defineMode('nftables', function () {
		function tokenString(quote) {
			return function (stream, state) {
				let escaped = false, ch;
				while ((ch = stream.next()) != null) {
					if (ch === quote && !escaped) {
						state.tokenize = null;
						return 'string';
					}
					escaped = !escaped && ch === '\\';
				}
				return 'string';
			};
		}

		return {
			startState: function () {
				return { tokenize: null };
			},
			token: function (stream, state) {
				if (state.tokenize) {
					return state.tokenize(stream, state);
				}

				if (stream.eatSpace()) return null;

				// Comments — nft uses `#` to EOL.
				if (stream.match('#')) {
					stream.skipToEnd();
					return 'comment';
				}

				// Strings.
				const ch = stream.peek();
				if (ch === '"' || ch === '\'') {
					stream.next();
					state.tokenize = tokenString(ch);
					return state.tokenize(stream, state);
				}

				// Hex / decimal numbers. Captures bare numbers, hex masks,
				// and the trailing component of ranges/CIDR (e.g. `/24`).
				if (stream.match(/^0x[0-9a-fA-F]+/)) return 'number';
				if (stream.match(/^[0-9]+/)) return 'number';

				// IPv4-ish literal — coarse but enough to colorize 10.0.0.1
				// distinctly from a bare integer. Won't match valid IPv6
				// literals (the colons would also be eaten as punctuation),
				// but IPv6 still tokens cleanly as hex + colon punctuation.
				if (stream.match(/^[0-9]+(\.[0-9]+){3}(\/[0-9]+)?/)) return 'atom';

				// Punctuation operators ahead of word matching so `!=` etc.
				// are returned as `operator` and not as `def`.
				if (stream.match(/^(==|!=|<=|>=|<<|>>|&&|\|\||->|=>)/)) return 'operator';
				if (stream.match(/^[{};,]/)) return 'bracket';
				if (stream.match(/^[<>!&|^~=:@*]/)) return 'operator';

				// Identifier / keyword.
				if (stream.match(/^[A-Za-z_][\w\-]*/)) {
					const word = stream.current();

					// Keyword categories — kept distinct so the theme can
					// pick out verdicts / matches in different shades.
					if (VERDICTS.indexOf(word) >= 0) return 'keyword strong';
					if (STATEMENTS.indexOf(word) >= 0) return 'def';
					if (FAMILIES.indexOf(word) >= 0) return 'builtin';
					if (CHAIN_TYPES.indexOf(word) >= 0) return 'builtin';
					if (HOOKS.indexOf(word) >= 0) return 'attribute';
					if (MATCHES.indexOf(word) >= 0) return 'variable-2';
					if (STATEMENTS_RHS.indexOf(word) >= 0) return 'keyword';
					if (OP_KEYWORDS.indexOf(word) >= 0) return 'operator';
					if (VALUES.indexOf(word) >= 0) return 'string-2';
					if (KEYWORDS.has(word)) return 'keyword';
					return null;
				}

				// Fallback — advance one char to avoid an infinite loop.
				stream.next();
				return null;
			},
			lineComment: '#',
			fold: 'brace',
			electricChars: '}',
		};
	});

	// File extension hint for autodetect; not strictly needed when we set
	// the mode explicitly, but cheap to register.
	CM.defineMIME('text/x-nftables', 'nftables');

	// ----- Hint (autocomplete) provider ----------------------------------
	// Hooks into CodeMirror's show-hint addon. Returns matches against the
	// current "word" (sequence of identifier chars) and the broader set of
	// all known nftables tokens.

	CM.registerHelper('hint', 'nftables', function (editor) {
		const cur = editor.getCursor();
		const line = editor.getLine(cur.line);
		// Walk backwards to find the start of the current word.
		let start = cur.ch;
		while (start > 0 && /[\w\-]/.test(line.charAt(start - 1))) start--;
		const end = cur.ch;
		const token = line.slice(start, end);

		// Empty token -> show top-of-grammar suggestions so users at the
		// start of a line get directly useful ones (table/chain/rule etc.)
		// instead of a giant list.
		let candidates;
		if (token.length === 0) {
			candidates = STATEMENTS.concat(MATCHES.slice(0, 12));
		} else {
			const lower = token.toLowerCase();
			candidates = ALL_HINTS.filter(function (w) {
				return w.toLowerCase().indexOf(lower) === 0;
			});
			// If we have nothing on prefix match, fall back to substring
			// match so typos still surface something.
			if (candidates.length === 0) {
				candidates = ALL_HINTS.filter(function (w) {
					return w.toLowerCase().indexOf(lower) >= 0;
				});
			}
		}

		return {
			list: candidates,
			from: CM.Pos(cur.line, start),
			to: CM.Pos(cur.line, end),
		};
	});
})(window.CodeMirror);
