let currentTimeRange = '24h';
let trafficData = null;

// D3 visualization state
let svg, g, simulation, zoom;
let nodes = [], links = [];
let nodeMap = new Map(); // Track existing nodes by ID
let physicsEnabled = true;
let selectedNode = null;
let isFirstRender = true;
let animationFrame = null;
let tooltipPinned = false; // Track if tooltip is pinned by click
let activeFilter = null; // Track active filter for tables

// Client-side source persistence with fade timer
// Sources from stick tables persist for 2 minutes to show connection history
const SOURCE_PERSISTENCE_MS = 120000; // 2 minute fade timer
const persistentSources = new Map(); // IP -> { data, lastSeen, opacity }

// Grid layout and auto-reflow state
let lastReflowTime = 0;
const REFLOW_INTERVAL_MS = 5000; // Reflow every 5 seconds
let userInteracting = false; // Track if user is dragging/interacting
let userInteractionTimeout = null;
let lastTrafficOrder = []; // Track last traffic order to detect changes

// Visualization filter state
let vizFilter = 'active'; // 'active', 'all', 'errors', 'high-traffic'

// Backend byte rate tracking - HAProxy gives cumulative totals, we need rates
// Track previous values and timestamps to calculate actual bytes per second
let previousBackendBytes = new Map(); // backendName -> { bytesIn, bytesOut, timestamp }
let backendByteRates = new Map(); // backendName -> { bytesInPerSec, bytesOutPerSec }

// Calculate byte rates from cumulative totals
function updateBackendByteRates(backends) {
	const now = Date.now();

	// Debug: log first backend to see all available fields
	if (backends.length > 0) {
		console.log('[ByteRate] First backend object:', JSON.stringify(backends[0], null, 2));
	}

	backends.forEach(backend => {
		const name = backend.name;
		const currentBytesIn = backend.bytes_in || 0;
		const currentBytesOut = backend.bytes_out || 0;

		// Debug: log raw byte values for backends with requests
		if (backend.total_requests > 0 || currentBytesIn > 0 || currentBytesOut > 0) {
			console.log(`[ByteRate] ${name} raw data: bytes_in=${currentBytesIn}, bytes_out=${currentBytesOut}, total_requests=${backend.total_requests}`);
		}

		const previous = previousBackendBytes.get(name);

		// Debug: show if we have previous data
		if (previous && (currentBytesIn > 0 || currentBytesOut > 0)) {
			console.log(`[ByteRate] ${name} previous: bytes_in=${previous.bytesIn}, bytes_out=${previous.bytesOut}, age=${((now - previous.timestamp)/1000).toFixed(1)}s`);
		}

		if (previous) {
			const elapsedMs = now - previous.timestamp;
			if (elapsedMs > 0) {
				const elapsedSec = elapsedMs / 1000;

				// Calculate delta (handle counter resets)
				const deltaIn = currentBytesIn >= previous.bytesIn
					? currentBytesIn - previous.bytesIn
					: currentBytesIn; // Counter was reset
				const deltaOut = currentBytesOut >= previous.bytesOut
					? currentBytesOut - previous.bytesOut
					: currentBytesOut;

				// Calculate rate (bytes per second)
				const bytesInPerSec = deltaIn / elapsedSec;
				const bytesOutPerSec = deltaOut / elapsedSec;

				backendByteRates.set(name, {
					bytesInPerSec: bytesInPerSec,
					bytesOutPerSec: bytesOutPerSec,
					totalBytesPerSec: bytesInPerSec + bytesOutPerSec
				});

				// Debug logging for active backends
				if (bytesInPerSec > 0 || bytesOutPerSec > 0) {
					const totalMbps = ((bytesInPerSec + bytesOutPerSec) * 8) / 1000000;
					console.log(`[ByteRate] ${name}: ${bytesInPerSec.toFixed(0)} B/s in, ${bytesOutPerSec.toFixed(0)} B/s out = ${totalMbps.toFixed(2)} Mbps (delta: ${(elapsedMs/1000).toFixed(1)}s)`);
				}
			}
		}

		// Store current values for next calculation
		previousBackendBytes.set(name, {
			bytesIn: currentBytesIn,
			bytesOut: currentBytesOut,
			timestamp: now
		});
	});
}

// Get calculated byte rate for a backend (returns 0 if not yet available)
function getBackendBytesPerSec(backendName) {
	const rate = backendByteRates.get(backendName);
	return rate ? rate.totalBytesPerSec : 0;
}

// Bandwidth configuration from settings (in bits per second)
let bandwidthConfig = {
	outbound_bps: 1000000000, // Default 1 Gbps - external internet upload
	inbound_bps: 1000000000,  // Default 1 Gbps - external internet download
	internal_bps: 0           // Default 0 = no limit for internal traffic
};

// Check if an IP address is a private/internal IP
// RFC 1918 private ranges: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
// Also includes loopback (127.x), link-local (169.254.x), and carrier-grade NAT (100.64-127.x)
function isPrivateIP(ip) {
	if (!ip) return false;
	const parts = ip.split('.').map(Number);
	if (parts.length !== 4) return false;

	// 10.0.0.0/8
	if (parts[0] === 10) return true;
	// 172.16.0.0/12 (172.16.x.x - 172.31.x.x)
	if (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) return true;
	// 192.168.0.0/16
	if (parts[0] === 192 && parts[1] === 168) return true;
	// 127.0.0.0/8 (loopback)
	if (parts[0] === 127) return true;
	// 169.254.0.0/16 (link-local)
	if (parts[0] === 169 && parts[1] === 254) return true;
	// 100.64.0.0/10 (carrier-grade NAT)
	if (parts[0] === 100 && parts[1] >= 64 && parts[1] <= 127) return true;

	return false;
}

// Calculate activity-based visualization from request counts
// This is more reliable than byte rates since active sessions always have request counts
// Returns: { utilization: 0-1, color, particleSpeed, particleCount }
function calculateActivityVisualization(requestCount, activeConnections, maxRequests = 100) {
	// Normalize by the max requests to get activity level (0-1)
	// Use a logarithmic scale so small amounts of activity are visible
	const activity = Math.min(1, Math.log(requestCount + 1) / Math.log(maxRequests + 1));

	// Also factor in active connections - more connections = more activity
	const connectionBoost = Math.min(0.3, activeConnections * 0.1);
	const utilization = Math.min(1, activity + connectionBoost);

	// Color based on activity level: green -> cyan -> blue
	// Use a cooler palette than bandwidth (which uses green->yellow->red)
	let color;
	if (utilization < 0.5) {
		const t = utilization * 2;
		color = interpolateColor('#22c55e', '#06b6d4', t); // green -> cyan
	} else {
		const t = (utilization - 0.5) * 2;
		color = interpolateColor('#06b6d4', '#3b82f6', t); // cyan -> blue
	}

	// Particle speed: faster for higher activity
	// At 0 activity = 4000ms (slow), at 100% = 600ms (fast)
	// Using a slightly slower min speed than bandwidth since this is estimates
	const particleSpeed = 4000 - (utilization * 3400);

	// Particle count: 1-4 based on activity
	const particleCount = Math.max(1, Math.min(4, Math.ceil(utilization * 4)));

	return { utilization, color, particleSpeed, particleCount, isActivityBased: true };
}

// Calculate bandwidth utilization and return visualization parameters
// Returns: { utilization: 0-1, color, particleSpeed, particleCount }
// isInternal: true for private IPs, uses internal_bps (or request-based if 0)
function calculateBandwidthVisualization(bytesPerSecond, isInternal = false) {
	let maxBps;
	if (isInternal) {
		maxBps = bandwidthConfig.internal_bps;
		// If internal bandwidth is 0, use request-based visualization (no bandwidth scaling)
		if (maxBps === 0) {
			// For internal traffic without bandwidth limit, use moderate defaults
			return {
				utilization: 0.3,
				color: '#22c55e', // Green - internal traffic
				particleSpeed: 2500,
				particleCount: 2
			};
		}
	} else {
		// External traffic - use the larger of inbound/outbound as the limit
		// since we're measuring total throughput
		maxBps = Math.max(bandwidthConfig.outbound_bps, bandwidthConfig.inbound_bps);
	}

	const bitsPerSecond = bytesPerSecond * 8;
	const utilization = maxBps > 0 ? Math.min(1, bitsPerSecond / maxBps) : 0;

	// Color based on utilization: green -> yellow -> red
	let color;
	if (utilization < 0.5) {
		// Green to yellow (0-50%)
		const t = utilization * 2;
		color = interpolateColor('#22c55e', '#eab308', t);
	} else {
		// Yellow to red (50-100%)
		const t = (utilization - 0.5) * 2;
		color = interpolateColor('#eab308', '#ef4444', t);
	}

	// Particle speed: faster for higher utilization
	// At 0% = 4000ms (slow), at 100% = 400ms (very fast)
	const particleSpeed = 4000 - (utilization * 3600);

	// Particle count: more for higher utilization (1-6)
	const particleCount = Math.max(1, Math.min(6, Math.ceil(utilization * 6)));

	return { utilization, color, particleSpeed, particleCount };
}

// Helper to interpolate between two hex colors
function interpolateColor(color1, color2, t) {
	const r1 = parseInt(color1.slice(1, 3), 16);
	const g1 = parseInt(color1.slice(3, 5), 16);
	const b1 = parseInt(color1.slice(5, 7), 16);
	const r2 = parseInt(color2.slice(1, 3), 16);
	const g2 = parseInt(color2.slice(3, 5), 16);
	const b2 = parseInt(color2.slice(5, 7), 16);

	const r = Math.round(r1 + (r2 - r1) * t);
	const g = Math.round(g1 + (g2 - g1) * t);
	const b = Math.round(b1 + (b2 - b1) * t);

	return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b.toString(16).padStart(2, '0')}`;
}

// Country data is now provided by the backend via GeoIP lookup

function changeVizFilter(filter) {
	vizFilter = filter;
	isFirstRender = true; // Force full re-render
	nodeMap.clear();
	if (trafficData) {
		updateNetworkVisualization(trafficData);
	}
}

// HAProxy connection state
let haproxyOnline = true; // Assume online initially
let haproxyError = null;

// Normalize backend names like the dashboard does
// Format: group_item_kind (e.g., "hardware_thor_backend" -> "thor", "audiobookshelf_audiobookshelf_backend" -> "audiobookshelf")
function normalizeBackendName(name) {
	const parts = name.split('_');
	if (parts.length >= 3) {
		// Last part is usually "backend", group is first, item is middle
		const group = parts[0];
		const item = parts.slice(1, parts.length - 1).join('_');
		// If group and item are the same, just use item
		return item;
	} else if (parts.length === 2) {
		return parts[1];
	}
	return name;
}

function setupPageHeader() {
	const source = document.getElementById('page-header-source');
	const target = document.getElementById('header-page-content');
	if (source && target) {
		while (source.firstChild) {
			target.appendChild(source.firstChild);
		}
	}
}

function formatBytes(bytes) {
	if (bytes === 0) return '0 B';
	const k = 1024;
	const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
	const i = Math.floor(Math.log(bytes) / Math.log(k));
	return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatNumber(num) {
	if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
	if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
	return num.toString();
}

function switchServer(serverID) {
	currentServerID = serverID;
	if (window.ServerSelector) {
		window.ServerSelector.setSelectedServer(serverID);
	}
	initSSE();
	refreshTrafficData();
}

function changeTimeRange(range) {
	currentTimeRange = range;
	refreshTrafficData();
}

async function refreshTrafficData(reinitSSE = false) {
	const icon = document.getElementById('refresh-icon');
	icon.classList.add('animate-spin');

	if (reinitSSE) {
		initSSE();
	}

	try {
		const response = await fetch(`/api/${currentServerID}/traffic?range=${currentTimeRange}`);
		if (!response.ok) {
			haproxyOnline = false;
			haproxyError = `HTTP ${response.status}: ${response.statusText}`;
			throw new Error('Failed to fetch traffic data');
		}

		haproxyOnline = true;
		haproxyError = null;
		trafficData = await response.json();
		updateUI(trafficData);
	} catch (error) {
		console.error('Failed to refresh traffic data:', error);
		haproxyOnline = false;
		haproxyError = error.message;
		// Still update visualization to show offline state
		if (trafficData) {
			updateNetworkVisualization(trafficData);
		} else {
			// No data at all - create minimal visualization
			updateNetworkVisualization({ live_data: { backend_traffic: [] } });
		}
	} finally {
		icon.classList.remove('animate-spin');
	}
}

function updateUI(data) {
	// Update bandwidth config from server
	if (data.bandwidth_config) {
		bandwidthConfig = {
			outbound_bps: data.bandwidth_config.outbound_bps || 1000000000,
			inbound_bps: data.bandwidth_config.inbound_bps || data.bandwidth_config.outbound_bps || 1000000000,
			internal_bps: data.bandwidth_config.internal_bps || 0
		};
		console.log('Bandwidth config loaded:', bandwidthConfig);
	}

	// Update summary cards
	if (data.summary) {
		document.getElementById('total-requests').textContent = formatNumber(data.summary.total_requests || 0);
		document.getElementById('requests-per-sec').textContent = (data.summary.requests_per_second || 0).toFixed(1);

		const totalBytes = (data.summary.total_bytes_in || 0) + (data.summary.total_bytes_out || 0);
		document.getElementById('total-bandwidth').textContent = formatBytes(totalBytes);
		document.getElementById('bytes-in').textContent = formatBytes(data.summary.total_bytes_in || 0);
		document.getElementById('bytes-out').textContent = formatBytes(data.summary.total_bytes_out || 0);

		document.getElementById('unique-ips').textContent = data.summary.unique_ips || data.live_data?.total_sources || '-';
		document.getElementById('unique-countries').textContent = data.summary.unique_countries || '-';

		document.getElementById('error-rate').textContent = (data.summary.error_rate || 0).toFixed(2) + '%';
		document.getElementById('avg-response-time').textContent = (data.summary.avg_response_time || 0) + 'ms';

		// Update response codes
		document.getElementById('status-2xx-count').textContent = formatNumber(data.summary.total_response_2xx || 0);
		document.getElementById('status-3xx-count').textContent = formatNumber(data.summary.total_response_3xx || 0);
		document.getElementById('status-4xx-count').textContent = formatNumber(data.summary.total_response_4xx || 0);
		document.getElementById('status-5xx-count').textContent = formatNumber(data.summary.total_response_5xx || 0);
	}

	// Update top sources table
	if (data.live_data?.top_by_requests) {
		updateTopSourcesTable(data.live_data.top_by_requests);
	}

	// Update backend traffic table
	if (data.live_data?.backend_traffic) {
		updateBackendTrafficTable(data.live_data.backend_traffic);
	}

	// Calculate byte rates from cumulative totals before visualization
	// HAProxy provides cumulative bytes, we need bytes/sec for bandwidth viz
	if (data.live_data?.backend_traffic) {
		updateBackendByteRates(data.live_data.backend_traffic);
	}

	// Update network visualization
	updateNetworkVisualization(data);

	// Hide loading overlay
	const loading = document.getElementById('network-loading');
	if (loading) loading.classList.add('hidden');
}

function updateTopSourcesTable(sources) {
	const tbody = document.getElementById('top-sources-table');
	if (!sources || sources.length === 0) {
		tbody.innerHTML = '<tr><td colspan="5" class="px-4 py-8 text-center text-gray-500 dark:text-gray-400">No traffic data available</td></tr>';
		return;
	}

	// Apply filter if active
	let filteredSources = sources;
	if (activeFilter) {
		if (activeFilter.type === 'source') {
			filteredSources = sources.filter(s => s.ip_address === activeFilter.value);
		} else if (activeFilter.type === 'backend') {
			filteredSources = sources.filter(s => s.backend === activeFilter.value);
		}
	}

	if (filteredSources.length === 0) {
		tbody.innerHTML = `<tr><td colspan="5" class="px-4 py-8 text-center text-gray-500 dark:text-gray-400">No traffic matching filter: ${activeFilter?.label || ''}</td></tr>`;
		return;
	}

	tbody.innerHTML = filteredSources.slice(0, 15).map(source => {
		const isHighlighted = activeFilter?.type === 'source' && activeFilter?.value === source.ip_address;
		return `
		<tr class="hover:bg-gray-50 dark:hover:bg-slate-700 cursor-pointer ${isHighlighted ? 'bg-violet-50 dark:bg-violet-900/30' : ''}" onclick="highlightNode('source-${source.ip_address}')">
			<td class="px-4 py-3 font-mono text-gray-800 dark:text-gray-200">${source.ip_address}</td>
			<td class="px-4 py-3 text-gray-600 dark:text-gray-300">${formatNumber(source.requests || source.http_request_rate || 0)}</td>
			<td class="px-4 py-3 text-gray-600 dark:text-gray-300">${formatBytes((source.bytes_in || 0) + (source.bytes_out || 0))}</td>
			<td class="px-4 py-3 text-gray-600 dark:text-gray-300">${source.backend ? normalizeBackendName(source.backend) : '-'}</td>
			<td class="px-4 py-3">
				<span class="px-2 py-1 text-xs rounded-full bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200">Active</span>
			</td>
		</tr>
	`;
	}).join('');
}

function updateBackendTrafficTable(backends) {
	const tbody = document.getElementById('backend-traffic-table');
	if (!backends || backends.length === 0) {
		tbody.innerHTML = '<tr><td colspan="6" class="px-4 py-8 text-center text-gray-500 dark:text-gray-400">No backend traffic data available</td></tr>';
		return;
	}

	// Apply filter if active
	let filteredBackends = backends;
	if (activeFilter && activeFilter.type === 'backend') {
		filteredBackends = backends.filter(b => b.name === activeFilter.value);
	}

	if (filteredBackends.length === 0) {
		tbody.innerHTML = `<tr><td colspan="6" class="px-4 py-8 text-center text-gray-500 dark:text-gray-400">No traffic matching filter: ${activeFilter?.label || ''}</td></tr>`;
		return;
	}

	tbody.innerHTML = filteredBackends.map(backend => {
		const errorRate = backend.error_rate || 0;
		let statusClass = 'bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200';
		let statusText = 'Healthy';
		if (errorRate > 20) {
			statusClass = 'bg-red-100 dark:bg-red-900 text-red-800 dark:text-red-200';
			statusText = 'Critical';
		} else if (errorRate > 5) {
			statusClass = 'bg-yellow-100 dark:bg-yellow-900 text-yellow-800 dark:text-yellow-200';
			statusText = 'Warning';
		}

		const isHighlighted = activeFilter?.type === 'backend' && activeFilter?.value === backend.name;
		const displayName = normalizeBackendName(backend.name);

		return `
			<tr class="hover:bg-gray-50 dark:hover:bg-slate-700 cursor-pointer ${isHighlighted ? 'bg-emerald-50 dark:bg-emerald-900/30' : ''}" onclick="highlightNode('backend-${backend.name}')">
				<td class="px-4 py-3 font-medium text-gray-800 dark:text-gray-200">${displayName}</td>
				<td class="px-4 py-3 text-gray-600 dark:text-gray-300">${formatNumber(backend.total_requests || 0)}</td>
				<td class="px-4 py-3 text-gray-600 dark:text-gray-300">${formatBytes((backend.bytes_in || 0) + (backend.bytes_out || 0))}</td>
				<td class="px-4 py-3 text-gray-600 dark:text-gray-300">${backend.avg_response_time || 0}ms</td>
				<td class="px-4 py-3 text-gray-600 dark:text-gray-300">${errorRate.toFixed(2)}%</td>
				<td class="px-4 py-3">
					<span class="px-2 py-1 text-xs rounded-full ${statusClass}">${statusText}</span>
				</td>
			</tr>
		`;
	}).join('');
}

// Initialize D3 visualization
function initVisualization() {
	const container = document.getElementById('network-container');
	const width = container.clientWidth;
	const height = container.clientHeight;

	// Clear existing
	d3.select('#network-svg').selectAll('*').remove();

	svg = d3.select('#network-svg')
		.attr('width', width)
		.attr('height', height);

	// Create gradient definitions
	const defs = svg.append('defs');

	// Glow filter
	const filter = defs.append('filter')
		.attr('id', 'glow')
		.attr('x', '-50%')
		.attr('y', '-50%')
		.attr('width', '200%')
		.attr('height', '200%');
	filter.append('feGaussianBlur')
		.attr('stdDeviation', '3')
		.attr('result', 'coloredBlur');
	const feMerge = filter.append('feMerge');
	feMerge.append('feMergeNode').attr('in', 'coloredBlur');
	feMerge.append('feMergeNode').attr('in', 'SourceGraphic');

	// Animated gradient for links
	const gradient = defs.append('linearGradient')
		.attr('id', 'link-gradient')
		.attr('gradientUnits', 'userSpaceOnUse');
	gradient.append('stop').attr('offset', '0%').attr('stop-color', '#8b5cf6');
	gradient.append('stop').attr('offset', '100%').attr('stop-color', '#3b82f6');

	// Arrow markers - small and subtle
	defs.append('marker')
		.attr('id', 'arrowhead')
		.attr('viewBox', '-0 -3 6 6')
		.attr('refX', 5)
		.attr('refY', 0)
		.attr('orient', 'auto')
		.attr('markerWidth', 4)
		.attr('markerHeight', 4)
		.append('path')
		.attr('d', 'M 0,-3 L 6,0 L 0,3')
		.attr('fill', '#64748b');

	// Pulse animation for idle HAProxy
	const pulseFilter = defs.append('filter')
		.attr('id', 'pulse-glow')
		.attr('x', '-100%')
		.attr('y', '-100%')
		.attr('width', '300%')
		.attr('height', '300%');
	pulseFilter.append('feGaussianBlur')
		.attr('stdDeviation', '4')
		.attr('result', 'coloredBlur');
	const pulseMerge = pulseFilter.append('feMerge');
	pulseMerge.append('feMergeNode').attr('in', 'coloredBlur');
	pulseMerge.append('feMergeNode').attr('in', 'SourceGraphic');

	// Create main group for zoom/pan
	g = svg.append('g');

	// Add background grid pattern
	const gridSize = 50;
	const gridGroup = g.append('g').attr('class', 'grid');
	for (let x = 0; x < width * 2; x += gridSize) {
		gridGroup.append('line')
			.attr('x1', x - width/2)
			.attr('y1', -height/2)
			.attr('x2', x - width/2)
			.attr('y2', height * 1.5)
			.attr('stroke', '#1e293b')
			.attr('stroke-width', 0.5);
	}
	for (let y = 0; y < height * 2; y += gridSize) {
		gridGroup.append('line')
			.attr('x1', -width/2)
			.attr('y1', y - height/2)
			.attr('x2', width * 1.5)
			.attr('y2', y - height/2)
			.attr('stroke', '#1e293b')
			.attr('stroke-width', 0.5);
	}

	// Setup zoom behavior
	zoom = d3.zoom()
		.scaleExtent([0.2, 4])
		.on('zoom', (event) => {
			g.attr('transform', event.transform);
		});

	svg.call(zoom);

	// Click on background to close tooltip
	svg.on('click', function(event) {
		if (event.target === this || event.target.tagName === 'line') {
			closeTooltip();
		}
	});

	// Initialize force simulation - don't start it yet, just set it up
	// Actual forces will be configured per-visualization based on node types
	simulation = d3.forceSimulation()
		.force('link', d3.forceLink().id(d => d.id).distance(150).strength(0.5))
		.force('charge', d3.forceManyBody().strength(-50))
		.force('collision', d3.forceCollide().radius(d => d.radius + 20))
		.stop();  // Don't start simulation until nodes are ready
}

function updateNetworkVisualization(data) {
	const container = document.getElementById('network-container');
	const width = container.clientWidth;
	const height = container.clientHeight;

	// Get data from various sources
	const connectionFlows = data.live_data?.connection_flows || [];
	const activeSessions = data.live_data?.active_sessions || [];
	const backends = data.live_data?.backend_traffic || [];
	const backendHostnames = data.backend_hostnames || {};
	const liveSources = data.live_data?.top_by_requests || [];

	// Merge live sources with historical sources from database
	// Historical sources provide data persistence across page refreshes
	const historicalSources = data.historical?.top_sources || [];

	// Debug: log what we received from API
	console.log('[Sources] API data.historical:', data.historical);
	console.log('[Sources] historicalSources length:', historicalSources.length);
	console.log('[Sources] liveSources length:', liveSources.length);

	// Create a map to deduplicate by IP, preferring live data over historical
	const sourceMap = new Map();

	// Add historical sources first (lower priority)
	historicalSources.forEach(src => {
		const ip = src.ip_address || src.IPAddress;
		if (ip) {
			sourceMap.set(ip, {
				ip_address: ip,
				requests: src.requests || src.Requests || 0,
				bytes_in: src.bytes_in || src.BytesIn || 0,
				bytes_out: src.bytes_out || src.BytesOut || 0,
				country: src.country || src.Country || 'Unknown',
				country_code: src.country_code || src.CountryCode || 'XX',
				backend: src.backend || '',
				isHistorical: true
			});
		}
	});

	// Add live sources (higher priority, overwrites historical)
	liveSources.forEach(src => {
		if (src.ip_address) {
			sourceMap.set(src.ip_address, {
				...src,
				isHistorical: false
			});
		}
	});

	// Convert back to array, sorted by requests
	const topSources = Array.from(sourceMap.values())
		.sort((a, b) => (b.requests || 0) - (a.requests || 0));

	// Debug: log source merge results
	if (historicalSources.length > 0 || liveSources.length > 0) {
		console.log(`[Sources] Merged: ${liveSources.length} live + ${historicalSources.length} historical = ${topSources.length} total`);
	}

	// Build IP -> country lookup from sources (backend provides GeoIP data)
	const ipCountryMap = new Map();
	topSources.forEach(src => {
		if (src.ip_address && src.country) {
			ipCountryMap.set(src.ip_address, { country: src.country, countryCode: src.country_code || '' });
		}
	});

	// If we have no backend data, don't update
	if (backends.length === 0) {
		return;
	}

	const now = Date.now();

	// ============================================
	// BUILD SOURCE -> BACKEND MAPPING FROM SESSIONS
	// ============================================
	// Key: backendName -> Map of sourceIP -> { lastSeen, requests, opacity }
	const backendSources = new Map();

	// Build a lookup map for source byte rates from topSources
	const sourceBytesMap = new Map();
	// Debug: log first source object to see what fields are available
	if (topSources.length > 0) {
		console.log('First topSource object:', JSON.stringify(topSources[0], null, 2));
	}
	topSources.forEach(src => {
		if (src.ip_address) {
			// Debug: log source byte data
			if (src.bytes_in > 0 || src.bytes_out > 0) {
				console.log(`Source data from API - ${src.ip_address}: bytes_in=${src.bytes_in}, bytes_out=${src.bytes_out}`);
			}
			sourceBytesMap.set(src.ip_address, {
				bytes_in: src.bytes_in || 0,
				bytes_out: src.bytes_out || 0
			});
		}
	});

	// Initialize from active sessions - these give us source->backend relationships
	// Debug: log first active session to see available fields
	if (activeSessions.length > 0) {
		console.log('First activeSession object:', JSON.stringify(activeSessions[0], null, 2));
	}
	activeSessions.forEach(sess => {
		if (!sess.source_ip || sess.source_ip === 'unix' || sess.source_ip === 'localhost') return;
		if (sess.source_ip.startsWith('/') || sess.source_ip.startsWith('127.')) return;
		if (!sess.backend || sess.backend === '<NOSRV>' || sess.backend === 'stats' || sess.backend === 'no_backend') return;

		if (!backendSources.has(sess.backend)) {
			backendSources.set(sess.backend, new Map());
		}
		const sources = backendSources.get(sess.backend);

		// Use bytes directly from session object (active sessions have per-session byte data)
		const sessBytesIn = sess.bytes_in || 0;
		const sessBytesOut = sess.bytes_out || 0;

		const existing = sources.get(sess.source_ip);
		if (existing) {
			existing.lastSeen = now;
			existing.requests += sess.request_count || 1;
			existing.opacity = 1.0;
			existing.isActive = true;  // Definitively has current traffic
			// Accumulate bytes from all sessions for this source
			existing.bytes_in = (existing.bytes_in || 0) + sessBytesIn;
			existing.bytes_out = (existing.bytes_out || 0) + sessBytesOut;
		} else {
			sources.set(sess.source_ip, {
				ip: sess.source_ip,
				lastSeen: now,
				requests: sess.request_count || 1,
				opacity: 1.0,
				isActive: true,  // Definitively has current traffic
				bytes_in: sessBytesIn,
				bytes_out: sessBytesOut
			});
		}
	});

	// Also use connection flows for source->backend mapping
	// Connection flows have bytes_in/bytes_out directly on the flow object
	// Debug: log first connection flow to see available fields
	if (connectionFlows.length > 0) {
		console.log('First connectionFlow object:', JSON.stringify(connectionFlows[0], null, 2));
	}
	connectionFlows.forEach(flow => {
		if (!flow.source_ip || flow.source_ip === 'unix' || flow.source_ip === 'localhost') return;
		if (flow.source_ip.startsWith('/') || flow.source_ip.startsWith('127.')) return;
		if (!flow.backend || flow.backend === '<NOSRV>' || flow.backend === 'stats' || flow.backend === 'no_backend') return;

		if (!backendSources.has(flow.backend)) {
			backendSources.set(flow.backend, new Map());
		}
		const sources = backendSources.get(flow.backend);

		// Use bytes directly from flow object (connection flows have per-source byte data)
		const flowBytesIn = flow.bytes_in || 0;
		const flowBytesOut = flow.bytes_out || 0;

		const existing = sources.get(flow.source_ip);
		if (existing) {
			existing.lastSeen = now;
			existing.requests = Math.max(existing.requests, flow.total_requests || flow.active_connections || 1);
			existing.opacity = 1.0;
			existing.isActive = true;  // Definitively has current traffic
			// Use the larger of existing or flow bytes
			existing.bytes_in = Math.max(existing.bytes_in || 0, flowBytesIn);
			existing.bytes_out = Math.max(existing.bytes_out || 0, flowBytesOut);
		} else {
			sources.set(flow.source_ip, {
				ip: flow.source_ip,
				lastSeen: now,
				requests: flow.total_requests || flow.active_connections || 1,
				opacity: 1.0,
				isActive: true,  // Definitively has current traffic
				bytes_in: flowBytesIn,
				bytes_out: flowBytesOut
			});
		}
	});

	// Update persistent sources with fade logic
	// Merge with previous state and apply fade
	// First, process backends that have current activity
	backendSources.forEach((sources, backendName) => {
		const persistKey = `backend-sources-${backendName}`;
		const prevSources = persistentSources.get(persistKey) || new Map();

		// Merge previous sources
		prevSources.forEach((prevSource, ip) => {
			if (!sources.has(ip)) {
				// Source no longer active - check if it should fade or be removed
				const age = now - prevSource.lastSeen;
				if (age <= SOURCE_PERSISTENCE_MS) {
					// Still within persistence window - mark as NOT active (no current traffic)
					prevSource.isActive = false;
					if (age > SOURCE_PERSISTENCE_MS / 2) {
						prevSource.opacity = 1.0 - ((age - SOURCE_PERSISTENCE_MS / 2) / (SOURCE_PERSISTENCE_MS / 2));
					}
					sources.set(ip, prevSource);
				}
			}
		});

		// Store updated sources for next cycle
		persistentSources.set(persistKey, sources);
	});

	// Also check for backends that have NO current activity but DO have persistent sources
	// This is critical for keeping sources visible after connections end
	persistentSources.forEach((prevSources, persistKey) => {
		const backendName = persistKey.replace('backend-sources-', '');
		if (!backendSources.has(backendName)) {
			// No current activity for this backend - check if any sources should persist
			const stillValidSources = new Map();
			prevSources.forEach((prevSource, ip) => {
				const age = now - prevSource.lastSeen;
				if (age <= SOURCE_PERSISTENCE_MS) {
					// Still within persistence window - mark as NOT active (no current traffic)
					prevSource.isActive = false;
					if (age > SOURCE_PERSISTENCE_MS / 2) {
						prevSource.opacity = 1.0 - ((age - SOURCE_PERSISTENCE_MS / 2) / (SOURCE_PERSISTENCE_MS / 2));
					}
					stillValidSources.set(ip, prevSource);
				}
			});
			if (stillValidSources.size > 0) {
				backendSources.set(backendName, stillValidSources);
				persistentSources.set(persistKey, stillValidSources);
			} else {
				// All sources expired - clean up
				persistentSources.delete(persistKey);
			}
		}
	});

	// Build new data maps
	const newNodeData = new Map();
	const newLinkData = new Map();

	// Get valid backends (filter out system backends)
	const validBackends = backends.filter(b =>
		b.name && b.name !== '<NOSRV>' && b.name !== 'stats' && b.name !== 'no_backend'
	);

	// Calculate max for sizing
	let maxBackendReq = 1;
	validBackends.forEach(b => { if ((b.total_requests || 0) > maxBackendReq) maxBackendReq = b.total_requests; });

	// ============================================
	// SORT AND FILTER BACKENDS BY TRAFFIC + ACTIVE SOURCES
	// Most active in center, less active surrounding
	// ============================================
	let filteredBackends = [...validBackends].map(b => {
		const sources = backendSources.get(b.name) || new Map();
		// Score = requests + (active sources * 1000) to prioritize backends with active connections
		const score = (b.total_requests || 0) + (sources.size * 1000);
		const errorRate = b.error_rate || 0;
		return { backend: b, score, sourceCount: sources.size, errorRate };
	});

	// Apply visualization filter
	if (vizFilter === 'active') {
		// Only show backends with active sources
		filteredBackends = filteredBackends.filter(b => b.sourceCount > 0);
	} else if (vizFilter === 'errors') {
		// Only show backends with errors (4xx/5xx > 0)
		filteredBackends = filteredBackends.filter(b => b.errorRate > 0);
	} else if (vizFilter === 'high-traffic') {
		// Only show backends with significant traffic (top 50% by score)
		const scores = filteredBackends.map(b => b.score).sort((a, b) => b - a);
		const threshold = scores[Math.floor(scores.length / 2)] || 0;
		filteredBackends = filteredBackends.filter(b => b.score >= threshold);
	}
	// 'all' shows everything

	const sortedBackends = filteredBackends.sort((a, b) => b.score - a.score);

	// If no backends match filter, show idle HAProxy node
	if (sortedBackends.length === 0) {
		// Show single idle HAProxy sphere in center
		const idleHaproxyId = 'haproxy-idle';
		newNodeData.set(idleHaproxyId, {
			id: idleHaproxyId,
			type: 'haproxy',
			label: 'light-hugger',
			sublabel: haproxyOnline ? 'Awaiting connections...' : 'OFFLINE',
			radius: 50,
			color: haproxyOnline ? '#3b82f6' : '#ef4444',
			strokeColor: haproxyOnline ? '#2563eb' : '#dc2626',
			data: { sources: 0, backend: null, idle: true },
			initialX: width / 2,
			initialY: height / 2,
			groupIndex: 0,
			isIdle: true,
			isOnline: haproxyOnline,
			error: haproxyError
		});

		// No links for idle state
		if (isFirstRender) {
			isFirstRender = false;
			createVisualization(newNodeData, newLinkData, width, height);
			// Start pulse animation for idle node
			startIdlePulse();
		} else {
			updateVisualizationData(newNodeData, newLinkData, width, height);
		}
		return;
	}

	// Check if we should reflow based on traffic order changes
	const currentOrder = sortedBackends.map(s => s.backend.name).join(',');
	const shouldReflow = !userInteracting && (
		isFirstRender ||
		(now - lastReflowTime > REFLOW_INTERVAL_MS && currentOrder !== lastTrafficOrder.join(','))
	);

	if (shouldReflow) {
		lastReflowTime = now;
		lastTrafficOrder = sortedBackends.map(s => s.backend.name);
	}

	// ============================================
	// CALCULATE GRID LAYOUT
	// Arrange in rows/columns to use horizontal space
	// Most active backends in center of grid
	// ============================================
	const FLOW_WIDTH = 380; // Width needed for each flow (source -> haproxy -> backend)
	const FLOW_HEIGHT = 130; // Height for each flow group - needs space for stacked sources
	const PADDING = 60;

	// Calculate how many columns fit
	const availableWidth = width - PADDING * 2;
	const availableHeight = height - PADDING * 2;
	const cols = Math.max(1, Math.floor(availableWidth / FLOW_WIDTH));
	const rows = Math.ceil(sortedBackends.length / cols);

	// Calculate actual spacing - ensure minimum row height for source stacking
	const colWidth = availableWidth / cols;
	const rowHeight = Math.max(FLOW_HEIGHT, availableHeight / Math.max(rows, 1));

	// Create spiral order from center for placement
	// This puts the most active (first in sorted list) at center
	function getSpiralPositions(count, cols, rows) {
		const positions = [];
		const centerCol = Math.floor(cols / 2);
		const centerRow = Math.floor(rows / 2);

		// Start from center and spiral outward
		const grid = [];
		for (let r = 0; r < rows; r++) {
			grid[r] = new Array(cols).fill(false);
		}

		let col = centerCol, row = centerRow;
		let direction = 0; // 0=right, 1=down, 2=left, 3=up
		let stepsInDirection = 1;
		let stepsTaken = 0;
		let directionChanges = 0;

		for (let i = 0; i < count; i++) {
			// Clamp to grid bounds
			const clampedRow = Math.max(0, Math.min(rows - 1, row));
			const clampedCol = Math.max(0, Math.min(cols - 1, col));
			positions.push({ row: clampedRow, col: clampedCol });

			// Move in current direction
			stepsTaken++;
			if (direction === 0) col++;
			else if (direction === 1) row++;
			else if (direction === 2) col--;
			else if (direction === 3) row--;

			// Check if we need to change direction
			if (stepsTaken >= stepsInDirection) {
				stepsTaken = 0;
				direction = (direction + 1) % 4;
				directionChanges++;
				if (directionChanges % 2 === 0) {
					stepsInDirection++;
				}
			}
		}

		return positions;
	}

	const spiralPositions = getSpiralPositions(sortedBackends.length, cols, rows);

	// ============================================
	// CREATE FLOW GROUPS: Sources -> HAProxy -> Backend
	// ============================================
	sortedBackends.forEach(({ backend: backendData, sourceCount }, index) => {
		const backendName = backendData.name;

		// Get position from spiral layout
		const pos = spiralPositions[index] || { row: 0, col: 0 };
		const groupX = PADDING + pos.col * colWidth + colWidth / 2;
		const groupY = PADDING + pos.row * rowHeight + rowHeight / 2;

		// Get hostname for this backend
		const hostname = backendHostnames[backendName] || '';
		const displayName = hostname || normalizeBackendName(backendName);

		// Backend error rate for coloring
		const errorRate = backendData.error_rate || 0;
		let backendColor = '#22c55e';
		let backendStrokeColor = '#16a34a';
		if (errorRate > 20) {
			backendColor = '#ef4444';
			backendStrokeColor = '#dc2626';
		} else if (errorRate > 5) {
			backendColor = '#f59e0b';
			backendStrokeColor = '#d97706';
		}

		// ---- BACKEND NODE (RIGHT of group) ----
		const backendId = `backend-${backendName}`;
		const backendReqs = backendData.total_requests || 0;

		// Position within the flow: source (left) -> haproxy (center) -> backend (right)
		const flowSpread = Math.min(colWidth * 0.4, 140); // How far apart within the flow
		const backendX = groupX + flowSpread;
		const haproxyX = groupX;
		const sourceX = groupX - flowSpread;

		// Fixed size rectangle for backend - wider to fit hostname
		const rectWidth = 130;
		const rectHeight = 32;

		newNodeData.set(backendId, {
			id: backendId,
			type: 'backend',
			shape: 'rect',
			label: displayName.length > 18 ? displayName.substring(0, 18) + '...' : displayName,
			fullName: backendName,
			hostname: hostname,
			sublabel: '',  // No sublabel - keep it clean
			width: rectWidth,
			height: rectHeight,
			radius: rectHeight / 2,  // For collision detection
			color: backendColor,
			strokeColor: backendStrokeColor,
			data: backendData,
			errorRate: errorRate,
			requests: backendReqs,
			initialX: backendX,
			initialY: groupY,
			groupIndex: index
		});

		// ---- HAPROXY NODE (CENTER of group) ----
		const haproxyId = `haproxy-${backendName}`;
		const sources = backendSources.get(backendName) || new Map();
		const srcCount = sources.size;

		newNodeData.set(haproxyId, {
			id: haproxyId,
			type: 'haproxy',
			label: 'light-hugger',  // Server name
			sublabel: '',
			radius: 38,  // Larger to fit server name
			color: '#3b82f6',
			strokeColor: '#2563eb',
			data: { sources: srcCount, backend: backendName },
			initialX: haproxyX,
			initialY: groupY,
			groupIndex: index
		});

		// ---- LINK: HAProxy -> Backend ----
		const haproxyBackendLinkId = `${haproxyId}->${backendId}`;

		// Calculate bandwidth utilization for this backend connection
		// Use calculated byte RATE (bytes/sec) not cumulative totals
		// getBackendBytesPerSec() returns the delta-calculated rate from updateBackendByteRates()
		const backendBytesPerSec = getBackendBytesPerSec(backendName);

		// Determine if traffic is internal or external based on connected sources
		// If ANY source is external (public IP), treat this backend link as external
		const hasExternalSource = Array.from(sources.values()).some(s => !isPrivateIP(s.ip));
		const backendIsInternal = !hasExternalSource;

		// Get the total active connections and request count for this backend
		const activeConnectionCount = sources.size;
		const totalSourceRequests = Array.from(sources.values()).reduce((sum, s) => sum + (s.requests || 0), 0);

		// Use byte rate if available, otherwise fall back to activity-based visualization
		let bwViz;
		if (backendBytesPerSec > 0) {
			// We have actual byte rate data - use bandwidth visualization
			bwViz = calculateBandwidthVisualization(backendBytesPerSec, backendIsInternal);
			// Debug: log bandwidth calculation
			console.log(`Backend ${backendName}: ${backendBytesPerSec.toFixed(0)} B/s = ${(backendBytesPerSec * 8 / 1000000).toFixed(2)} Mbps, utilization: ${(bwViz.utilization * 100).toFixed(1)}%, speed: ${bwViz.particleSpeed}ms`);
		} else if (activeConnectionCount > 0 || totalSourceRequests > 0) {
			// No byte rate data but we have active connections - use activity visualization
			// Find max requests across all backends for normalization
			const maxBackendRequests = Math.max(1, ...sortedBackends.map(b => b.backend.total_requests || 0));
			bwViz = calculateActivityVisualization(totalSourceRequests || backendReqs, activeConnectionCount, maxBackendRequests);
			// Debug: log activity calculation
			console.log(`Backend ${backendName}: ACTIVITY-BASED - ${totalSourceRequests} requests, ${activeConnectionCount} connections, speed: ${bwViz.particleSpeed}ms`);
		} else {
			// No activity - use minimal visualization
			bwViz = {
				utilization: 0,
				color: backendColor,
				particleSpeed: 4000,
				particleCount: 1
			};
		}

		// Use bandwidth-based color if utilization is significant, otherwise use backend health color
		const linkColor = bwViz.utilization > 0.1 ? bwViz.color : backendColor;

		newLinkData.set(haproxyBackendLinkId, {
			id: haproxyBackendLinkId,
			source: haproxyId,
			target: backendId,
			value: 0.5,  // Fixed line width
			requests: backendReqs,
			color: linkColor,
			particleSpeed: bwViz.particleSpeed,
			particleCount: bwViz.particleCount,
			bandwidthUtilization: bwViz.utilization,
			type: 'haproxy-backend'
		});

		// ---- SOURCE NODES (LEFT of group) ----
		const sourceList = Array.from(sources.values())
			.filter(s => s.opacity > 0.1)
			.sort((a, b) => b.requests - a.requests)
			.slice(0, 3); // Max 3 sources per backend for compact grid

		// Increase spacing between stacked sources to prevent overlap (taller rectangles now)
		const sourceSpacing = sourceList.length > 1 ? 55 : 0;
		const sourceStartY = groupY - ((sourceList.length - 1) * sourceSpacing) / 2;

		sourceList.forEach((source, sourceIndex) => {
			const sourceId = `source-${backendName}-${source.ip}`;
			const opacity = source.opacity;
			const isActive = source.isActive === true;  // Explicitly check for true

			// Fixed size rectangle for source - taller for two lines (IP + country)
			const srcRectWidth = 120;
			const srcRectHeight = 36;

			// Get country from backend-provided GeoIP data
			const geo = ipCountryMap.get(source.ip) || { country: '', countryCode: '' };

			newNodeData.set(sourceId, {
				id: sourceId,
				type: 'source',
				shape: 'rect',
				label: source.ip,
				sublabel: geo.country,  // Country on second line (from backend)
				width: srcRectWidth,
				height: srcRectHeight,
				radius: srcRectHeight / 2,  // For collision detection
				color: `rgba(139, 92, 246, ${opacity})`,
				strokeColor: `rgba(124, 58, 237, ${opacity})`,
				opacity: opacity,
				requests: source.requests,
				data: {
					ip_address: source.ip,
					requests: source.requests,
					backend: backendName,
					country: geo.country
				},
				initialX: sourceX,
				initialY: sourceStartY + sourceIndex * sourceSpacing,
				groupIndex: index
			});

			// ---- LINK: Source -> HAProxy ----
			const sourceLinkId = `${sourceId}->${haproxyId}`;

			// Determine if this is internal (private IP) or external traffic
			const isInternal = isPrivateIP(source.ip);

			// Calculate bandwidth utilization for this source connection
			// Use the calculated byte RATE (not cumulative totals) and divide among sources
			const numSources = sourceList.length;
			const backendBytesRate = getBackendBytesPerSec(backendName);
			// Attribute rate to this source proportionally (simple equal division)
			const sourceBytesPerSec = numSources > 0 ? backendBytesRate / numSources : 0;

			// Use byte rate if available, otherwise use activity-based visualization
			let srcBwViz;
			if (sourceBytesPerSec > 0) {
				srcBwViz = calculateBandwidthVisualization(sourceBytesPerSec, isInternal);
			} else if (source.requests > 0) {
				// Use activity-based visualization for sources with requests
				const maxSourceReqs = Math.max(1, ...sourceList.map(s => s.requests || 0));
				srcBwViz = calculateActivityVisualization(source.requests, 1, maxSourceReqs * 2);
			} else {
				// Minimal visualization for inactive sources
				srcBwViz = {
					utilization: 0.1,
					color: '#a78bfa', // Light purple
					particleSpeed: 3500,
					particleCount: 1
				};
			}

			// Use bandwidth-based color for external traffic, or if utilization is significant
			// For internal traffic with no limit, use a subtle purple
			let srcLinkColor;
			if (!isInternal && srcBwViz.utilization > 0.05) {
				// External traffic with measurable bandwidth - use bandwidth color
				srcLinkColor = srcBwViz.color;
			} else if (isInternal && bandwidthConfig.internal_bps > 0 && srcBwViz.utilization > 0.1) {
				// Internal traffic with limit configured and significant utilization
				srcLinkColor = srcBwViz.color;
			} else if (srcBwViz.isActivityBased && srcBwViz.utilization > 0.2) {
				// Activity-based visualization with notable activity
				srcLinkColor = srcBwViz.color;
			} else {
				// Default - use purple with opacity
				srcLinkColor = `rgba(139, 92, 246, ${opacity * 0.7})`;
			}

			newLinkData.set(sourceLinkId, {
				id: sourceLinkId,
				source: sourceId,
				target: haproxyId,
				value: 0.5,  // Fixed line width
				requests: source.requests,
				opacity: opacity,
				isActive: isActive,  // True only when traffic is actively flowing
				color: srcLinkColor,
				particleSpeed: srcBwViz.particleSpeed,
				particleCount: srcBwViz.particleCount,
				bandwidthUtilization: srcBwViz.utilization,
				isInternal: isInternal,
				type: 'source-haproxy'
			});
		});
	});

	// First render - create everything
	if (isFirstRender) {
		isFirstRender = false;
		createVisualization(newNodeData, newLinkData, width, height);
	} else {
		// Subsequent updates - update data and handle node additions/removals
		updateVisualizationData(newNodeData, newLinkData, width, height);
	}
}

function createVisualization(newNodeData, newLinkData, width, height) {
	// Convert maps to arrays
	nodes = Array.from(newNodeData.values()).map(d => {
		const node = { ...d };
		// Set initial positions based on type
		// Each flow group (sources -> haproxy -> backend) is on its own row
		node.x = d.initialX || width / 2;
		node.y = d.initialY || height / 2;
		nodeMap.set(d.id, node);
		return node;
	});

	links = Array.from(newLinkData.values());

	// Clear any existing elements
	g.selectAll('.link').remove();
	g.selectAll('.node').remove();

	// Draw links
	const link = g.selectAll('.link')
		.data(links, d => d.id)
		.enter()
		.append('g')
		.attr('class', 'link');

	link.append('line')
		.attr('class', 'link-line')
		.attr('stroke', d => d.color)
		.attr('stroke-opacity', d => d.opacity || 0.5)
		.attr('stroke-width', d => 1.5 + 3 * d.value)
		.attr('marker-end', 'url(#arrowhead)');

	// Add animated particles on links - count and speed based on bandwidth utilization
	link.each(function(d) {
		// No particles if no active traffic - use explicit isActive flag
		// Particles only flow when there is CURRENT traffic, not cached/persisting connections
		if (!d.isActive) return;

		// Use bandwidth-calculated particle count, or fall back to request-based count
		const requests = d.requests || 0;
		const particleCount = d.particleCount || Math.max(1, Math.min(4, Math.ceil(requests / 10)));
		const particleSize = 3;

		// Use pre-calculated particle speed from bandwidth utilization
		const duration = d.particleSpeed || 2000;

		for (let i = 0; i < particleCount; i++) {
			d3.select(this)
				.append('circle')
				.attr('class', 'particle')
				.attr('r', particleSize)
				.attr('fill', d.color) // Use link color (bandwidth-based)
				.attr('opacity', d.opacity || 0.8)
				.attr('data-offset', i / particleCount)  // Evenly spaced
				.attr('data-speed', duration);
		}
	});

	// Draw nodes - HAProxy first (so it's below other nodes in z-order if needed)
	const node = g.selectAll('.node')
		.data(nodes, d => d.id)
		.enter()
		.append('g')
		.attr('class', 'node')
		.attr('data-id', d => d.id)
		.attr('transform', d => `translate(${d.x},${d.y})`)  // Set initial position immediately
		.style('cursor', 'pointer')
		.call(d3.drag()
			.on('start', dragstarted)
			.on('drag', dragged)
			.on('end', dragended))
		.on('click', (event, d) => showNodeDetails(event, d))
		.on('mouseenter', (event, d) => {
			const shape = d3.select(event.currentTarget).select('.node-shape');
			if (d.shape === 'rect') {
				shape.transition().duration(200)
					.attr('filter', 'url(#glow)')
					.attr('stroke-width', 3);
			} else {
				shape.transition().duration(200)
					.attr('r', d.radius * 1.15)
					.attr('filter', 'url(#glow)');
			}
		})
		.on('mouseleave', (event, d) => {
			const shape = d3.select(event.currentTarget).select('.node-shape');
			if (d.shape === 'rect') {
				shape.transition().duration(200)
					.attr('filter', null)
					.attr('stroke-width', 2);
			} else {
				shape.transition().duration(200)
					.attr('r', d.radius)
					.attr('filter', null);
			}
			hideTooltip();
		});

	// Node shapes - rectangles for source/backend, circles for haproxy
	node.each(function(d) {
		const nodeG = d3.select(this);
		if (d.shape === 'rect') {
			// Rectangle for source and backend nodes
			nodeG.append('rect')
				.attr('class', 'node-shape')
				.attr('x', -d.width / 2)
				.attr('y', -d.height / 2)
				.attr('width', d.width)
				.attr('height', d.height)
				.attr('rx', 4)  // Rounded corners
				.attr('ry', 4)
				.attr('fill', d.color)
				.attr('stroke', d.strokeColor)
				.attr('stroke-width', 2)
				.attr('opacity', d.opacity || 0.9);
		} else {
			// Circle for haproxy nodes
			nodeG.append('circle')
				.attr('class', 'node-shape')
				.attr('r', d.radius)
				.attr('fill', d.color)
				.attr('stroke', d.strokeColor)
				.attr('stroke-width', 3)
				.attr('opacity', 0.9);

			// Inner glow for haproxy
			nodeG.append('circle')
				.attr('class', 'node-inner')
				.attr('r', d.radius * 0.7)
				.attr('fill', 'none')
				.attr('stroke', '#fff')
				.attr('stroke-width', 1)
				.attr('opacity', 0.15);
		}
	});

	// Node labels - inside shapes for rectangles, centered for circles
	node.append('text')
		.attr('class', 'node-label')
		.attr('dy', d => {
			if (d.type === 'source') return -2;  // Upper line for IP
			if (d.type === 'haproxy' && d.isIdle) return 5;  // Centered for idle (larger)
			if (d.type === 'haproxy') return 5;  // Centered in circle
			return 4;  // Centered in rectangle for backend
		})
		.attr('text-anchor', 'middle')
		.attr('fill', '#fff')
		.attr('font-size', d => {
			if (d.type === 'source') return '10px';
			if (d.type === 'haproxy' && d.isIdle) return '12px';  // Larger for idle
			if (d.type === 'haproxy') return '10px';
			return '10px';
		})
		.attr('font-weight', 'bold')
		.attr('opacity', d => d.opacity || 1)
		.text(d => d.label);

	// Add sublabel (country) for source nodes
	node.filter(d => d.type === 'source')
		.append('text')
		.attr('class', 'node-sublabel')
		.attr('dy', 12)  // Below the IP
		.attr('text-anchor', 'middle')
		.attr('fill', '#c4b5fd')  // Lighter purple
		.attr('font-size', '8px')
		.attr('opacity', d => d.opacity || 1)
		.text(d => d.sublabel || '');

	// Add sublabel for idle HAProxy node
	node.filter(d => d.isIdle)
		.append('text')
		.attr('class', 'node-sublabel')
		.attr('dy', 22)  // Below the server name
		.attr('text-anchor', 'middle')
		.attr('fill', d => d.isOnline ? '#93c5fd' : '#fca5a5')  // Light blue or light red
		.attr('font-size', '9px')
		.attr('font-style', 'italic')
		.text(d => d.sublabel || '');

	// Setup simulation with grid-based layout
	// Each node has initialX/initialY set for its grid position
	simulation.nodes(nodes);
	simulation.force('link').links(links);

	// Use initial positions directly - strong force to keep grid layout
	simulation.force('x', d3.forceX(d => d.initialX || width / 2).strength(0.8));
	simulation.force('y', d3.forceY(d => d.initialY || height / 2).strength(0.8));

	// Minimal forces - grid layout handles positioning
	// Increase charge repulsion and collision radius to prevent overlap
	simulation.force('charge', d3.forceManyBody().strength(-50));
	simulation.force('collision', d3.forceCollide().radius(d => {
		// Use larger collision radius based on shape size
		if (d.shape === 'rect') {
			return Math.max(d.width, d.height) / 2 + 15;
		}
		return d.radius + 15;
	}));

	// IMPORTANT: Setup tick handler BEFORE starting simulation
	// This ensures DOM is updated correctly from the first tick
	simulation.on('tick', tickHandler);

	// Now start the simulation - nodes are already at their initial positions
	// due to the transform attribute set when creating node groups
	simulation.alpha(0.5).restart();

	// Start particle animation
	startParticleAnimation();

	// Auto-fit to show all content after simulation settles briefly
	// Use a short delay to let initial positions stabilize
	setTimeout(() => fitToContent(true), 100);
}

function updateVisualizationData(newNodeData, newLinkData, width, height) {
	// Track what nodes/links exist now
	const currentNodeIds = new Set(nodes.map(n => n.id));
	const newNodeIds = new Set(newNodeData.keys());

	// Identify nodes to add, remove, and update
	const nodesToAdd = [];
	const nodesToRemove = [];

	newNodeData.forEach((newData, id) => {
		const existingNode = nodeMap.get(id);
		if (existingNode) {
			// Update data properties
			existingNode.radius = newData.radius;
			existingNode.color = newData.color;
			existingNode.strokeColor = newData.strokeColor;
			existingNode.data = newData.data;
			existingNode.sublabel = newData.sublabel;
			existingNode.errorRate = newData.errorRate;
			existingNode.opacity = newData.opacity;

			// Update target position for reflow (if not being dragged)
			if (existingNode.fx === null || existingNode.fx === undefined) {
				existingNode.initialX = newData.initialX;
				existingNode.initialY = newData.initialY;
			}
		} else {
			// New node - will need to add it
			nodesToAdd.push(newData);
		}
	});

	// Find nodes to remove (exist in current but not in new)
	currentNodeIds.forEach(id => {
		if (!newNodeIds.has(id)) {
			nodesToRemove.push(id);
		}
	});

	// Handle node additions
	nodesToAdd.forEach(newData => {
		const node = { ...newData };
		node.x = newData.initialX || width / 2;
		node.y = newData.initialY || height / 2;
		if (newData.fx !== undefined) node.fx = newData.fx;
		if (newData.fy !== undefined) node.fy = newData.fy;
		nodes.push(node);
		nodeMap.set(newData.id, node);
	});

	// Handle node removals
	nodesToRemove.forEach(id => {
		const idx = nodes.findIndex(n => n.id === id);
		if (idx >= 0) {
			nodes.splice(idx, 1);
		}
		nodeMap.delete(id);
	});

	// Update links
	links = Array.from(newLinkData.values());

	// If nodes changed, we need to rebind and redraw
	if (nodesToAdd.length > 0 || nodesToRemove.length > 0) {
		// Rebind data
		const linkSelection = g.selectAll('.link')
			.data(links, d => d.id);

		// Remove old links
		linkSelection.exit().remove();

		// Add new links
		const linkEnter = linkSelection.enter()
			.append('g')
			.attr('class', 'link');

		linkEnter.append('line')
			.attr('class', 'link-line')
			.attr('stroke', d => d.color)
			.attr('stroke-opacity', d => d.opacity || 0.5)
			.attr('stroke-width', d => 1.5 + 3 * d.value)
			.attr('marker-end', 'url(#arrowhead)');

		// Add particles to new links - count and speed based on bandwidth utilization
		linkEnter.each(function(d) {
			// No particles if no active traffic - use explicit isActive flag
			// Particles only flow when there is CURRENT traffic, not cached/persisting connections
			if (!d.isActive) return;

			// Use bandwidth-calculated particle count, or fall back to request-based count
			const requests = d.requests || 0;
			const particleCount = d.particleCount || Math.max(1, Math.min(4, Math.ceil(requests / 10)));
			const particleSize = 3;
			const duration = d.particleSpeed || 2000;

			for (let i = 0; i < particleCount; i++) {
				d3.select(this)
					.append('circle')
					.attr('class', 'particle')
					.attr('r', particleSize)
					.attr('fill', d.color) // Use link color (bandwidth-based)
					.attr('opacity', d.opacity || 0.8)
					.attr('data-offset', i / particleCount)
					.attr('data-speed', duration);
			}
		});

		// Rebind nodes
		const nodeSelection = g.selectAll('.node')
			.data(nodes, d => d.id);

		// Remove old nodes
		nodeSelection.exit().remove();

		// Add new nodes
		const nodeEnter = nodeSelection.enter()
			.append('g')
			.attr('class', 'node')
			.attr('data-id', d => d.id)
			.attr('transform', d => `translate(${d.x},${d.y})`)  // Set initial position immediately
			.style('cursor', 'pointer')
			.call(d3.drag()
				.on('start', dragstarted)
				.on('drag', dragged)
				.on('end', dragended))
			.on('click', (event, d) => showNodeDetails(event, d))
			.on('mouseenter', (event, d) => {
				const shape = d3.select(event.currentTarget).select('.node-shape');
				if (d.shape === 'rect') {
					shape.transition().duration(200)
						.attr('filter', 'url(#glow)')
						.attr('stroke-width', 3);
				} else {
					shape.transition().duration(200)
						.attr('r', d.radius * 1.15)
						.attr('filter', 'url(#glow)');
				}
			})
			.on('mouseleave', (event, d) => {
				const shape = d3.select(event.currentTarget).select('.node-shape');
				if (d.shape === 'rect') {
					shape.transition().duration(200)
						.attr('filter', null)
						.attr('stroke-width', 2);
				} else {
					shape.transition().duration(200)
						.attr('r', d.radius)
						.attr('filter', null);
				}
				hideTooltip();
			});

		// Add shapes based on type
		nodeEnter.each(function(d) {
			const nodeG = d3.select(this);
			if (d.shape === 'rect') {
				nodeG.append('rect')
					.attr('class', 'node-shape')
					.attr('x', -d.width / 2)
					.attr('y', -d.height / 2)
					.attr('width', d.width)
					.attr('height', d.height)
					.attr('rx', 4)
					.attr('ry', 4)
					.attr('fill', d.color)
					.attr('stroke', d.strokeColor)
					.attr('stroke-width', 2)
					.attr('opacity', d.opacity || 0.9);
			} else {
				nodeG.append('circle')
					.attr('class', 'node-shape')
					.attr('r', d.radius)
					.attr('fill', d.color)
					.attr('stroke', d.strokeColor)
					.attr('stroke-width', 3)
					.attr('opacity', 0.9);

				nodeG.append('circle')
					.attr('class', 'node-inner')
					.attr('r', d.radius * 0.7)
					.attr('fill', 'none')
					.attr('stroke', '#fff')
					.attr('stroke-width', 1)
					.attr('opacity', 0.15);
			}
		});

		nodeEnter.append('text')
			.attr('class', 'node-label')
			.attr('dy', d => {
				if (d.shape === 'rect') return 4;
				if (d.type === 'haproxy') return 4;
				return 4;
			})
			.attr('text-anchor', 'middle')
			.attr('fill', '#fff')
			.attr('font-size', d => {
				if (d.type === 'source') return '9px';
				if (d.type === 'haproxy') return '8px';
				return '9px';
			})
			.attr('font-weight', d => d.type === 'haproxy' ? 'normal' : 'bold')
			.attr('opacity', d => d.opacity || 1)
			.text(d => d.label);

		// Update simulation
		simulation.nodes(nodes);
		simulation.force('link').links(links);
		simulation.alpha(0.3).restart();

		// If nodes were added, auto-fit to show all content
		if (nodesToAdd.length > 0) {
			setTimeout(() => fitToContent(true), 150);
		}
	}

	// Update visual elements
	g.selectAll('.node').each(function(d) {
		const node = d3.select(this);
		const shape = node.select('.node-shape');

		if (d.shape === 'rect') {
			shape.transition().duration(300)
				.attr('fill', d.color)
				.attr('stroke', d.strokeColor)
				.attr('opacity', d.opacity || 0.9);
		} else {
			shape.transition().duration(300)
				.attr('r', d.radius)
				.attr('fill', d.color)
				.attr('stroke', d.strokeColor)
				.attr('opacity', d.opacity || 0.9);

			node.select('.node-inner')
				.transition()
				.duration(300)
				.attr('r', d.radius * 0.7);
		}

		node.select('.node-label')
			.attr('opacity', d.opacity || 1);
	});

	g.selectAll('.link').each(function(d) {
		const link = d3.select(this);
		link.select('.link-line')
			.transition()
			.duration(300)
			.attr('stroke', d.color)
			.attr('stroke-opacity', d.opacity || 0.5)
			.attr('stroke-width', 1.5 + 3 * d.value);

		// Update particles based on current traffic
		// Use explicit isActive flag - particles only flow when there is CURRENT traffic
		if (!d.isActive) {
			// No active traffic - remove all particles
			link.selectAll('.particle').remove();
		} else {
			// Active traffic - update particle properties
			const existingParticles = link.selectAll('.particle');
			const currentCount = existingParticles.size();

			// Use bandwidth-calculated particle count, or fall back to request-based count
			const requests = d.requests || 0;
			const desiredCount = d.particleCount || Math.max(1, Math.min(4, Math.ceil(requests / 10)));

			// Update speed and color on existing particles (bandwidth-based)
			existingParticles
				.attr('fill', d.color) // Use link color (bandwidth-based)
				.attr('opacity', d.opacity || 0.8)
				.attr('data-speed', d.particleSpeed || 2000);

			// Add more particles if needed
			if (currentCount < desiredCount) {
				for (let i = currentCount; i < desiredCount; i++) {
					link.append('circle')
						.attr('class', 'particle')
						.attr('r', 3)
						.attr('fill', d.color) // Use link color (bandwidth-based)
						.attr('opacity', d.opacity || 0.8)
						.attr('data-offset', i / desiredCount)
						.attr('data-speed', d.particleSpeed || 2000);
				}
			} else if (currentCount > desiredCount) {
				// Remove excess particles
				existingParticles.each(function(p, i) {
					if (i >= desiredCount) {
						d3.select(this).remove();
					}
				});
			}
		}
	});
}

// Calculate edge point for a link connecting to a node
// Returns the point on the edge of the shape closest to the other end
function getEdgePoint(node, otherX, otherY) {
	const dx = otherX - node.x;
	const dy = otherY - node.y;
	const dist = Math.sqrt(dx * dx + dy * dy);

	if (dist === 0) return { x: node.x, y: node.y };

	// Normalize direction
	const nx = dx / dist;
	const ny = dy / dist;

	if (node.shape === 'rect') {
		// Rectangle: find intersection with rect boundary
		const hw = (node.width || 120) / 2;
		const hh = (node.height || 36) / 2;

		// Calculate intersection with rectangle edges
		// Check which edge we intersect first
		const tx = nx !== 0 ? hw / Math.abs(nx) : Infinity;
		const ty = ny !== 0 ? hh / Math.abs(ny) : Infinity;
		const t = Math.min(tx, ty);

		return {
			x: node.x + nx * t,
			y: node.y + ny * t
		};
	} else {
		// Circle: point on circumference
		const r = node.radius || 30;
		return {
			x: node.x + nx * r,
			y: node.y + ny * r
		};
	}
}

function tickHandler() {
	// Update link positions - connect to edges, not centers
	g.selectAll('.link-line')
		.attr('x1', d => {
			const edge = getEdgePoint(d.source, d.target.x, d.target.y);
			return edge.x;
		})
		.attr('y1', d => {
			const edge = getEdgePoint(d.source, d.target.x, d.target.y);
			return edge.y;
		})
		.attr('x2', d => {
			const edge = getEdgePoint(d.target, d.source.x, d.source.y);
			return edge.x;
		})
		.attr('y2', d => {
			const edge = getEdgePoint(d.target, d.source.x, d.source.y);
			return edge.y;
		});

	// Update node positions
	g.selectAll('.node')
		.attr('transform', d => `translate(${d.x},${d.y})`);
}

function startParticleAnimation() {
	// Cancel any existing animation
	if (animationFrame) {
		cancelAnimationFrame(animationFrame);
	}

	function animateParticles() {
		const time = Date.now();

		g.selectAll('.link').each(function(d) {
			const linkGroup = d3.select(this);
			const line = linkGroup.select('.link-line');

			const x1 = parseFloat(line.attr('x1')) || 0;
			const y1 = parseFloat(line.attr('y1')) || 0;
			const x2 = parseFloat(line.attr('x2')) || 0;
			const y2 = parseFloat(line.attr('y2')) || 0;

			linkGroup.selectAll('.particle').each(function(p, i) {
				const particle = d3.select(this);
				const offset = parseFloat(particle.attr('data-offset')) || 0;
				const speed = parseFloat(particle.attr('data-speed')) || 3000;

				// Each particle has its own speed and random offset
				const t = ((time / speed) + offset) % 1;

				particle
					.attr('cx', x1 + (x2 - x1) * t)
					.attr('cy', y1 + (y2 - y1) * t);
			});
		});

		animationFrame = requestAnimationFrame(animateParticles);
	}

	animateParticles();
}

let idlePulseAnimation = null;

function startIdlePulse() {
	// Cancel existing pulse
	if (idlePulseAnimation) {
		cancelAnimationFrame(idlePulseAnimation);
	}

	function animatePulse() {
		const time = Date.now();
		// Pulse period: 2 seconds
		const t = (time % 2000) / 2000;
		// Smooth pulse: 0 -> 1 -> 0
		const pulse = Math.sin(t * Math.PI);

		g.selectAll('.node').each(function(d) {
			if (d.isIdle && d.isOnline) {
				const nodeG = d3.select(this);
				const shape = nodeG.select('.node-shape');
				// Animate the glow filter intensity
				const glowIntensity = 2 + pulse * 6;
				shape.attr('filter', `url(#pulse-glow)`);
				// Also slightly scale the node
				const scale = 1 + pulse * 0.05;
				nodeG.attr('transform', `translate(${d.x},${d.y}) scale(${scale})`);
			}
		});

		idlePulseAnimation = requestAnimationFrame(animatePulse);
	}

	animatePulse();
}

function stopIdlePulse() {
	if (idlePulseAnimation) {
		cancelAnimationFrame(idlePulseAnimation);
		idlePulseAnimation = null;
	}
}

function dragstarted(event, d) {
	// Mark user as interacting - prevents reflow
	userInteracting = true;
	if (userInteractionTimeout) clearTimeout(userInteractionTimeout);

	if (!event.active && physicsEnabled) simulation.alphaTarget(0.3).restart();
	d.fx = d.x;
	d.fy = d.y;
}

function dragged(event, d) {
	d.fx = event.x;
	d.fy = event.y;
}

function dragended(event, d) {
	if (!event.active && physicsEnabled) simulation.alphaTarget(0);
	if (physicsEnabled) {
		d.fx = null;
		d.fy = null;
	}

	// Reset user interaction flag after a delay
	userInteractionTimeout = setTimeout(() => {
		userInteracting = false;
	}, 3000); // 3 seconds after interaction before reflow resumes
}

function showNodeDetails(event, d) {
	event.stopPropagation();
	const tooltip = document.getElementById('node-tooltip');
	const content = document.getElementById('tooltip-content');
	const actions = document.getElementById('tooltip-actions');

	let html = '';
	let showFilterButton = false;

	if (d.type === 'haproxy') {
		// HAProxy node for a specific backend flow
		const data = d.data;
		const backendName = data.backend ? normalizeBackendName(data.backend) : 'Unknown';
		html = '<div class="text-blue-400 font-bold text-lg mb-2">HAProxy Router</div>' +
			'<div class="text-gray-200">Routing to: <span class="text-emerald-300">' + backendName + '</span></div>' +
			'<div class="mt-3 pt-3 border-t border-slate-600">' +
			'<div><span class="text-gray-400">Active Sources:</span> <span class="text-violet-300 font-bold">' + (data.sources || 0) + '</span></div>' +
			'</div>';
		showFilterButton = false;
	} else if (d.type === 'source') {
		const data = d.data;
		showFilterButton = true;

		// Show which backend this source is connecting to
		const backendName = data.backend ? normalizeBackendName(data.backend) : 'Unknown';

		html = '<div class="text-violet-400 font-bold text-lg mb-2">Source IP</div>' +
			'<div class="font-mono text-gray-200 text-lg">' + data.ip_address + '</div>' +
			'<div class="mt-3 pt-3 border-t border-slate-600">' +
			'<div class="text-sm mb-2">' +
			'<div><span class="text-gray-400">Requests:</span> <span class="text-violet-300 font-bold">' + formatNumber(data.requests || 0) + '</span></div>' +
			'<div class="mt-1"><span class="text-gray-400">Connecting to:</span> <span class="text-emerald-300">' + backendName + '</span></div>' +
			'</div>' +
			'</div>';
	} else if (d.type === 'backend') {
		const data = d.data;
		const displayName = d.hostname || normalizeBackendName(data.name);
		const errorClass = d.errorRate > 20 ? 'text-red-400' : d.errorRate > 5 ? 'text-amber-400' : 'text-emerald-400';
		showFilterButton = true;
		html = `
			<div class="${errorClass} font-bold text-lg mb-2">Backend</div>
			<div class="text-gray-200">${displayName}</div>
			${d.hostname ? '<div class="text-gray-500 text-xs">' + data.name + '</div>' : ''}
			<div class="mt-3 pt-3 border-t border-slate-600 grid grid-cols-2 gap-2 text-sm">
				<div><span class="text-gray-400">Requests:</span> <span class="text-gray-200">${formatNumber(data.total_requests || 0)}</span></div>
				<div><span class="text-gray-400">Error Rate:</span> <span class="${errorClass}">${(data.error_rate || 0).toFixed(2)}%</span></div>
				<div><span class="text-gray-400">Avg Latency:</span> <span class="text-gray-200">${data.avg_response_time || 0}ms</span></div>
				<div><span class="text-gray-400">Unique IPs:</span> <span class="text-gray-200">${data.unique_ips || 0}</span></div>
				<div><span class="text-gray-400">Bytes In:</span> <span class="text-gray-200">${formatBytes(data.bytes_in || 0)}</span></div>
				<div><span class="text-gray-400">Bytes Out:</span> <span class="text-gray-200">${formatBytes(data.bytes_out || 0)}</span></div>
			</div>
			<div class="mt-3 pt-3 border-t border-slate-600 grid grid-cols-4 gap-1 text-xs text-center">
				<div><span class="block text-emerald-400">${formatNumber(data.response_2xx || 0)}</span>2xx</div>
				<div><span class="block text-blue-400">${formatNumber(data.response_3xx || 0)}</span>3xx</div>
				<div><span class="block text-amber-400">${formatNumber(data.response_4xx || 0)}</span>4xx</div>
				<div><span class="block text-red-400">${formatNumber(data.response_5xx || 0)}</span>5xx</div>
			</div>
		`;
	}

	content.innerHTML = html;

	// Show/hide filter button based on node type
	if (showFilterButton) {
		actions.classList.remove('hidden');
	} else {
		actions.classList.add('hidden');
	}

	// Position tooltip
	const container = document.getElementById('network-container');
	const rect = container.getBoundingClientRect();
	let x = event.clientX - rect.left + 10;
	let y = event.clientY - rect.top + 10;

	// Keep tooltip in bounds
	if (x + 290 > rect.width) x = x - 300;
	if (y + 280 > rect.height) y = y - 290;

	tooltip.style.left = x + 'px';
	tooltip.style.top = y + 'px';
	tooltip.classList.remove('hidden');

	// Pin the tooltip and highlight selected node
	tooltipPinned = true;
	selectedNode = d;
	g.selectAll('.node').attr('opacity', n => n.id === d.id ? 1 : 0.4);
	g.selectAll('.link').attr('opacity', l => l.source.id === d.id || l.target.id === d.id ? 1 : 0.2);
}

function closeTooltip() {
	const tooltip = document.getElementById('node-tooltip');
	tooltip.classList.add('hidden');
	tooltipPinned = false;
	selectedNode = null;
	g.selectAll('.node').attr('opacity', 1);
	g.selectAll('.link').attr('opacity', 1);
}

function hideTooltip() {
	// Don't hide if tooltip is pinned
	if (tooltipPinned) return;

	const tooltip = document.getElementById('node-tooltip');
	tooltip.classList.add('hidden');
	selectedNode = null;
	g.selectAll('.node').attr('opacity', 1);
	g.selectAll('.link').attr('opacity', 1);
}

function filterBySelectedNode() {
	if (!selectedNode) return;

	const d = selectedNode;
	if (d.type === 'source') {
		activeFilter = {
			type: 'source',
			value: d.data.ip_address,
			label: d.data.ip_address
		};
	} else if (d.type === 'backend') {
		activeFilter = {
			type: 'backend',
			value: d.data.name,
			label: normalizeBackendName(d.data.name)
		};
	}

	// Update filter indicator
	const indicator = document.getElementById('filter-indicator');
	const filterText = document.getElementById('filter-text');
	indicator.classList.remove('hidden');
	filterText.textContent = `Filtering by: ${activeFilter.label}`;

	// Update tables with filter
	if (trafficData) {
		if (trafficData.live_data?.top_by_requests) {
			updateTopSourcesTable(trafficData.live_data.top_by_requests);
		}
		if (trafficData.live_data?.backend_traffic) {
			updateBackendTrafficTable(trafficData.live_data.backend_traffic);
		}
	}

	// Scroll to tables
	document.getElementById('top-sources-table').closest('.bg-white, .dark\\:bg-slate-800').scrollIntoView({ behavior: 'smooth', block: 'start' });

	// Close tooltip after filtering
	closeTooltip();
}

function clearFilter() {
	activeFilter = null;
	const indicator = document.getElementById('filter-indicator');
	indicator.classList.add('hidden');

	// Update tables without filter
	if (trafficData) {
		if (trafficData.live_data?.top_by_requests) {
			updateTopSourcesTable(trafficData.live_data.top_by_requests);
		}
		if (trafficData.live_data?.backend_traffic) {
			updateBackendTrafficTable(trafficData.live_data.backend_traffic);
		}
	}
}

function highlightNode(nodeId) {
	const node = nodes.find(n => n.id === nodeId);
	if (node && svg) {
		// Center on node
		const transform = d3.zoomIdentity
			.translate(svg.attr('width') / 2 - node.x, svg.attr('height') / 2 - node.y)
			.scale(1.5);
		svg.transition().duration(750).call(zoom.transform, transform);

		// Pulse effect
		const nodeEl = g.selectAll('.node').filter(d => d.id === nodeId);
		const shape = nodeEl.select('.node-shape');
		if (node.shape === 'rect') {
			shape.transition().duration(200)
				.attr('stroke-width', 4)
				.transition().duration(200)
				.attr('stroke-width', 2);
		} else {
			shape.transition().duration(200)
				.attr('r', node.radius * 1.5)
				.transition().duration(200)
				.attr('r', node.radius);
		}
	}
}

function resetVisualization() {
	hideTooltip();
	// Re-enable physics briefly to rearrange nodes
	if (simulation && physicsEnabled) {
		simulation.alpha(0.3).restart();
	}
	// Fit to show all content
	setTimeout(() => fitToContent(true), 100);
}

function togglePhysics() {
	physicsEnabled = !physicsEnabled;
	const btn = document.getElementById('physics-toggle');
	if (physicsEnabled) {
		btn.textContent = 'Physics: ON';
		btn.classList.remove('bg-slate-600');
		btn.classList.add('bg-blue-600');
		simulation.alpha(0.5).restart();
	} else {
		btn.textContent = 'Physics: OFF';
		btn.classList.remove('bg-blue-600');
		btn.classList.add('bg-slate-600');
		simulation.stop();
	}
}

function zoomIn() {
	if (svg && zoom) {
		svg.transition().duration(300).call(zoom.scaleBy, 1.3);
	}
}

function zoomOut() {
	if (svg && zoom) {
		svg.transition().duration(300).call(zoom.scaleBy, 0.7);
	}
}

// Auto-fit visualization to show all nodes with traffic
// Calculates bounding box of all nodes and applies appropriate zoom/pan
function fitToContent(animate = true) {
	if (!svg || !zoom || nodes.length === 0) return;

	const container = document.getElementById('network-container');
	const width = container.clientWidth;
	const height = container.clientHeight;

	// Calculate bounding box of all nodes
	let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;

	nodes.forEach(node => {
		// Account for node size
		const nodeWidth = node.shape === 'rect' ? (node.width || 120) : (node.radius || 30) * 2;
		const nodeHeight = node.shape === 'rect' ? (node.height || 36) : (node.radius || 30) * 2;

		const left = node.x - nodeWidth / 2;
		const right = node.x + nodeWidth / 2;
		const top = node.y - nodeHeight / 2;
		const bottom = node.y + nodeHeight / 2;

		if (left < minX) minX = left;
		if (right > maxX) maxX = right;
		if (top < minY) minY = top;
		if (bottom > maxY) maxY = bottom;
	});

	// Add padding around the content
	const padding = 60;
	minX -= padding;
	minY -= padding;
	maxX += padding;
	maxY += padding;

	// Calculate the bounding box dimensions
	const contentWidth = maxX - minX;
	const contentHeight = maxY - minY;

	// Calculate scale to fit content in view
	// Use the smaller scale to ensure everything fits
	const scaleX = width / contentWidth;
	const scaleY = height / contentHeight;
	let scale = Math.min(scaleX, scaleY);

	// Clamp scale to reasonable bounds (don't zoom in too much or too little)
	scale = Math.max(0.3, Math.min(2.0, scale));

	// Calculate center of content
	const centerX = (minX + maxX) / 2;
	const centerY = (minY + maxY) / 2;

	// Calculate transform to center the content
	const translateX = width / 2 - centerX * scale;
	const translateY = height / 2 - centerY * scale;

	const transform = d3.zoomIdentity
		.translate(translateX, translateY)
		.scale(scale);

	if (animate) {
		svg.transition().duration(500).call(zoom.transform, transform);
	} else {
		svg.call(zoom.transform, transform);
	}
}

// SSE connection for real-time updates
let eventSource = null;
let isNavigatingAway = false;

function updateSSEStatus(status) {
	const btn = document.getElementById('refresh-btn');
	if (!btn) return;

	btn.classList.remove('text-green-500', 'text-red-500', 'text-yellow-500', 'text-gray-400');

	switch (status) {
		case 'connected':
			btn.classList.add('text-green-500');
			btn.title = 'Live - streaming data';
			break;
		case 'connecting':
			btn.classList.add('text-yellow-500');
			btn.title = 'Connecting...';
			break;
		case 'disconnected':
			btn.classList.add('text-red-500');
			btn.title = 'Disconnected - click to reconnect';
			break;
		default:
			btn.classList.add('text-gray-400');
			btn.title = 'Refresh';
	}
}

function initSSE() {
	if (eventSource) {
		eventSource.close();
	}

	if (!currentServerID) {
		return;
	}

	updateSSEStatus('connecting');

	const sseUrl = `/api/events?server=${currentServerID}`;
	eventSource = new EventSource(sseUrl);

	eventSource.addEventListener('connected', function(e) {
		console.log('Traffic SSE connected:', JSON.parse(e.data));
		updateSSEStatus('connected');
	});

	eventSource.addEventListener('stats.updated', function(e) {
		refreshTrafficData();
	});

	eventSource.addEventListener('traffic.updated', function(e) {
		refreshTrafficData();
	});

	eventSource.addEventListener('server.connected', function(e) {
		refreshTrafficData();
	});

	eventSource.addEventListener('server.disconnected', function(e) {
		console.log('Server disconnected:', JSON.parse(e.data));
		updateSSEStatus('disconnected');
	});

	eventSource.onerror = function(e) {
		console.error('SSE connection error:', e);
		updateSSEStatus('disconnected');
	};
}

function closeSSE() {
	if (eventSource) {
		eventSource.close();
		eventSource = null;
	}
	if (animationFrame) {
		cancelAnimationFrame(animationFrame);
		animationFrame = null;
	}
	isNavigatingAway = true;
}

// Initialize on page load
document.addEventListener('DOMContentLoaded', function() {
	setupPageHeader();

	const serverSelector = document.getElementById('server-selector');
	const defaultServerInput = document.getElementById('default-server-id');

	if (serverSelector && window.ServerSelector) {
		currentServerID = window.ServerSelector.initFromSelect(serverSelector) || serverSelector.value;
	} else if (serverSelector) {
		currentServerID = serverSelector.value;
	} else if (defaultServerInput) {
		currentServerID = defaultServerInput.value;
	}

	// Initialize D3 visualization
	initVisualization();

	// Handle window resize (debounced)
	let resizeTimeout;
	window.addEventListener('resize', () => {
		clearTimeout(resizeTimeout);
		resizeTimeout = setTimeout(() => {
			// Cancel particle animation
			if (animationFrame) {
				cancelAnimationFrame(animationFrame);
				animationFrame = null;
			}
			// Reset state for full re-render
			isFirstRender = true;
			nodeMap.clear();
			initVisualization();
			if (trafficData) updateNetworkVisualization(trafficData);
		}, 250);
	});

	if (currentServerID) {
		initSSE();
		refreshTrafficData();
	}
});

window.addEventListener('beforeunload', closeSSE);

document.addEventListener('visibilitychange', function() {
	if (document.visibilityState === 'visible' && currentServerID) {
		if (!eventSource || eventSource.readyState === EventSource.CLOSED) {
			initSSE();
		}
		refreshTrafficData();
	}
});

// Event listeners for visualization controls
const serverSelector = document.getElementById('server-selector');
if (serverSelector) {
	serverSelector.addEventListener('change', (e) => switchServer(e.target.value));
}

const timeRangeSelector = document.getElementById('time-range-selector');
if (timeRangeSelector) {
	timeRangeSelector.addEventListener('change', (e) => changeTimeRange(e.target.value));
}

const refreshBtn = document.getElementById('refresh-btn');
if (refreshBtn) {
	refreshBtn.addEventListener('click', () => refreshTrafficData(true));
}

const vizFilterSelect = document.getElementById('viz-filter');
if (vizFilterSelect) {
	vizFilterSelect.addEventListener('change', (e) => changeVizFilter(e.target.value));
}

const resetVizBtn = document.getElementById('reset-viz-btn');
if (resetVizBtn) {
	resetVizBtn.addEventListener('click', resetVisualization);
}

const physicsToggle = document.getElementById('physics-toggle');
if (physicsToggle) {
	physicsToggle.addEventListener('click', togglePhysics);
}

const filterBtn = document.getElementById('filter-btn');
if (filterBtn) {
	filterBtn.addEventListener('click', filterBySelectedNode);
}

const closeTooltipBtn = document.getElementById('close-tooltip-btn');
if (closeTooltipBtn) {
	closeTooltipBtn.addEventListener('click', closeTooltip);
}

const clearFilterBtn = document.getElementById('clear-filter-btn');
if (clearFilterBtn) {
	clearFilterBtn.addEventListener('click', clearFilter);
}

const zoomInBtn = document.getElementById('zoom-in-btn');
if (zoomInBtn) {
	zoomInBtn.addEventListener('click', zoomIn);
}

const zoomOutBtn = document.getElementById('zoom-out-btn');
if (zoomOutBtn) {
	zoomOutBtn.addEventListener('click', zoomOut);
}
