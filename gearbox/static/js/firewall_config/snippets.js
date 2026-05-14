/**
 * Curated nftables snippet catalog.
 *
 * Snippets are grouped by category for the modal's left-column nav.
 * Each entry has a short title, a longer description, and a `code` block
 * that gets inserted at the editor's cursor verbatim.
 *
 * The patterns are drawn from the public references the search turned up:
 *   - https://wiki.nftables.org/wiki-nftables/index.php/Quick_reference-nftables_in_10_minutes
 *   - https://wiki.nftables.org/wiki-nftables/index.php/Simple_rule_management
 *   - https://wiki.archlinux.org/title/Nftables
 *   - https://wiki.gentoo.org/wiki/Nftables/Examples
 *   - https://github.com/k4yt3x/nftables and https://github.com/mqus/nft-rules
 *
 * Keep the snippets small and self-contained — they're meant to be a
 * starting point the user can edit, not a finished policy.
 */
window.FIREWALL_SNIPPETS = [
	{
		category: 'Skeleton',
		title: 'Minimal stateful filter',
		description: 'Boilerplate inet filter table with input/forward/output chains, default drop on input/forward, and the standard ct established/related accept rule.',
		code:
			'table inet filter {\n' +
			'  chain input {\n' +
			'    type filter hook input priority 0; policy drop;\n' +
			'    ct state established,related accept\n' +
			'    ct state invalid drop\n' +
			'    iif lo accept\n' +
			'    ip protocol icmp accept\n' +
			'    ip6 nexthdr ipv6-icmp accept\n' +
			'  }\n' +
			'  chain forward {\n' +
			'    type filter hook forward priority 0; policy drop;\n' +
			'  }\n' +
			'  chain output {\n' +
			'    type filter hook output priority 0; policy accept;\n' +
			'  }\n' +
			'}\n',
	},
	{
		category: 'Skeleton',
		title: 'Logged drop helper chain',
		description: 'A reusable chain that logs and drops; jump to it from input rules when you want visibility on what got blocked.',
		code:
			'chain log_drop {\n' +
			'  limit rate 10/minute log prefix "[nft_drop] " level info\n' +
			'  drop\n' +
			'}\n',
	},
	{
		category: 'Allow rules',
		title: 'SSH from LAN',
		description: 'Allow TCP/22 only from the 10.0.0.0/24 LAN subnet. Adjust the source CIDR to match your network.',
		code: 'iifname "eno1" ip saddr 10.0.0.0/24 tcp dport 22 accept\n',
	},
	{
		category: 'Allow rules',
		title: 'HTTP + HTTPS public',
		description: 'Allow inbound TCP/80 and TCP/443 from anywhere on a public-facing interface.',
		code: 'iifname "eno7" tcp dport { 80, 443 } accept\n',
	},
	{
		category: 'Allow rules',
		title: 'WireGuard inbound',
		description: 'Allow inbound UDP/51820 (default WireGuard port) from the WAN interface.',
		code: 'iifname "eno7" udp dport 51820 accept\n',
	},
	{
		category: 'Allow rules',
		title: 'ICMP / pings',
		description: 'Allow ICMPv4 echo + ICMPv6 ND traffic. ND is required for IPv6 to function at all on a LAN.',
		code:
			'ip protocol icmp icmp type { echo-request, echo-reply, destination-unreachable, time-exceeded } accept\n' +
			'ip6 nexthdr ipv6-icmp icmpv6 type { echo-request, echo-reply, destination-unreachable, packet-too-big, time-exceeded, parameter-problem, mld-listener-query, nd-router-solicit, nd-router-advert, nd-neighbor-solicit, nd-neighbor-advert } accept\n',
	},
	{
		category: 'Drop rules',
		title: 'Block specific source IP',
		description: 'Drop everything from a single IPv4 address. Useful when you only need a quick block — for anything reusable, use a named set.',
		code: 'ip saddr 1.2.3.4 drop\n',
	},
	{
		category: 'Drop rules',
		title: 'Drop block set',
		description: 'Named set + drop rule. Add IPs at runtime with `nft add element inet filter blocklist { 1.2.3.4 }`.',
		code:
			'set blocklist {\n' +
			'  type ipv4_addr\n' +
			'  flags interval\n' +
			'  elements = { 192.0.2.0/24 }\n' +
			'}\n' +
			'ip saddr @blocklist drop\n',
	},
	{
		category: 'Drop rules',
		title: 'Drop bogons (martians)',
		description: 'Drop traffic claiming to come from reserved/non-routable address blocks on a public interface.',
		code:
			'iifname "eno7" ip saddr { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16, 224.0.0.0/4 } drop\n',
	},
	{
		category: 'Rate limiting',
		title: 'SSH brute-force protector',
		description: 'Allow up to 4 new SSH connections per minute per source IP; drop the rest. Pairs well with fail2ban for slower decay.',
		code:
			'tcp dport 22 ct state new meter ssh_meter { ip saddr limit rate 4/minute } accept\n' +
			'tcp dport 22 ct state new drop\n',
	},
	{
		category: 'Rate limiting',
		title: 'ICMP rate limit',
		description: 'Slow down ping floods without dropping useful diagnostic traffic entirely.',
		code: 'ip protocol icmp limit rate 10/second accept\n',
	},
	{
		category: 'NAT',
		title: 'Masquerade outbound',
		description: 'Source-NAT all traffic leaving the WAN interface. Required on a typical home router setup.',
		code:
			'table ip nat {\n' +
			'  chain postrouting {\n' +
			'    type nat hook postrouting priority 100;\n' +
			'    oifname "eno7" masquerade\n' +
			'  }\n' +
			'}\n',
	},
	{
		category: 'NAT',
		title: 'Port forward (DNAT)',
		description: 'Forward inbound TCP/443 on the WAN interface to an internal host. Pair with a `forward` allow rule.',
		code:
			'table ip nat {\n' +
			'  chain prerouting {\n' +
			'    type nat hook prerouting priority -100;\n' +
			'    iifname "eno7" tcp dport 443 dnat to 10.0.0.50:443\n' +
			'  }\n' +
			'}\n',
	},
	{
		category: 'NAT',
		title: 'Redirect (transparent proxy)',
		description: 'Redirect outbound DNS to a local resolver. Useful for ad-blocking / pi-hole style setups.',
		code:
			'table ip nat {\n' +
			'  chain prerouting {\n' +
			'    type nat hook prerouting priority -100;\n' +
			'    iifname "br0" udp dport 53 redirect to :5353\n' +
			'    iifname "br0" tcp dport 53 redirect to :5353\n' +
			'  }\n' +
			'}\n',
	},
	{
		category: 'Logging',
		title: 'Log + accept',
		description: 'Log matching packets to the kernel ring buffer (visible via `journalctl -k`) and accept them.',
		code: 'tcp dport 22 log prefix "[ssh] " level info accept\n',
	},
	{
		category: 'Logging',
		title: 'Log + count drops',
		description: 'Pair a counter with a log on the catch-all drop so you can see how much background noise you\'re blocking.',
		code:
			'chain log_count_drop {\n' +
			'  counter log prefix "[fw_drop] " level info drop\n' +
			'}\n',
	},
	{
		category: 'Tables / chains',
		title: 'New table & chain',
		description: 'Skeleton for adding a brand-new table to an existing config — e.g. when carving out a separate ip6 policy.',
		code:
			'table ip6 myfilter {\n' +
			'  chain input {\n' +
			'    type filter hook input priority 0; policy drop;\n' +
			'  }\n' +
			'}\n',
	},
];
