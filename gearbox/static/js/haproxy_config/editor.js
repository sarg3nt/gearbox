		// Read server ID and config SHA from hidden inputs (Templ doesn't interpolate inside script tags)
		const boxID = document.getElementById('server-id-source')?.value || '';
		let currentSHA = document.getElementById('config-sha-source')?.value || '';
		let fullscreenState = 0;
		let autoGenSections = []; // Store auto-generated sections
		let originalFullConfig = ""; // Store the complete original config
		let selectedSnippet = null; // Currently selected snippet for insertion
		let editorLines = []; // Cache of editor lines for line help

		// Auto-gen marker patterns
		// Matches: "# BEGIN AUTO-GENERATED ROUTING RULES - DO NOT EDIT MANUALLY"
		// Matches: "# BEGIN AUTO-GENERATED BACKENDS - DO NOT EDIT MANUALLY"
		const AUTO_GEN_START = /^\s*#\s*BEGIN\s+AUTO-GENERATED\s+/i;
		// Matches: "# END AUTO-GENERATED ROUTING RULES"
		// Matches: "# END AUTO-GENERATED BACKENDS"
		const AUTO_GEN_END = /^\s*#\s*END\s+AUTO-GENERATED\s+/i;

		// HAProxy directive help dictionary
		const HAPROXY_HELP = {
			// Sections
			'global': { desc: 'Process-wide settings including daemon mode, logging, performance tuning, and SSL configuration.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3' },
			'defaults': { desc: 'Default parameters inherited by all frontend, backend, and listen sections unless overridden.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4' },
			'frontend': { desc: 'Defines a listening endpoint that accepts client connections. Specifies bind addresses, ACLs, and routing rules.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.1' },
			'backend': { desc: 'Defines a pool of servers to forward requests to. Includes load balancing algorithm and health checks.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.1' },
			'listen': { desc: 'Combines frontend and backend in one section. Useful for simple TCP proxying or stats pages.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.1' },

			// Core directives
			'bind': { desc: 'Defines the IP address and port(s) the frontend listens on. Supports SSL/TLS options.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#5.1-bind' },
			'server': { desc: 'Defines a backend server with its address, port, and options like health checks and weight.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#5.2-server' },
			'mode': { desc: 'Sets the proxy mode: "http" for HTTP/HTTPS traffic or "tcp" for raw TCP connections.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-mode' },
			'balance': { desc: 'Sets the load balancing algorithm: roundrobin, leastconn, source, uri, hdr, etc.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-balance' },
			'log': { desc: 'Configures logging destination and format. Can send to syslog, stdout, or a log server.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-log' },
			'maxconn': { desc: 'Sets the maximum number of concurrent connections. Protects backends from overload.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-maxconn' },

			// Timeouts
			'timeout': { desc: 'Sets timeout values. Common: connect, client, server, http-request, http-keep-alive, queue, tunnel.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20connect' },
			'timeout connect': { desc: 'Maximum time to wait for a connection to a backend server. If exceeded, HAProxy tries the next server or returns 503.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20connect' },
			'timeout client': { desc: 'Maximum inactivity time on the client side. Closes idle client connections to free resources.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20client' },
			'timeout server': { desc: 'Maximum inactivity time on the server side. Should accommodate your slowest backend responses.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20server' },
			'timeout http-request': { desc: 'Maximum time to receive a complete HTTP request from the client. Protects against slowloris attacks.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20http-request' },
			'timeout http-keep-alive': { desc: 'Maximum time to wait for a new HTTP request on a keep-alive connection. Shorter than client timeout.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20http-keep-alive' },
			'timeout queue': { desc: 'Maximum time a request waits in queue when all servers are at maxconn. Returns 503 if exceeded.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20queue' },
			'timeout tunnel': { desc: 'Timeout for bidirectional tunnel connections (WebSocket, CONNECT). Applied after connection is established.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20tunnel' },
			'timeout client-fin': { desc: 'Timeout for half-closed client connections (client sent FIN). Allows graceful connection draining.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20client-fin' },
			'timeout server-fin': { desc: 'Timeout for half-closed server connections (server sent FIN). Allows graceful connection draining.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20server-fin' },
			'timeout tarpit': { desc: 'How long to hold tarpitted connections. Used with http-request tarpit to slow down attackers.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-timeout%20tarpit' },

			// ACLs and routing
			'acl': { desc: 'Defines an Access Control List for conditional routing. Can match headers, paths, IPs, etc.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#7.1-acl' },
			'use_backend': { desc: 'Routes traffic to a specific backend when ACL conditions are met.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-use_backend' },
			'default_backend': { desc: 'Specifies the backend to use when no use_backend rules match.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-default_backend' },

			// HTTP rules
			'http-request': { desc: 'Performs actions on HTTP requests: deny, redirect, set-header, add-header, etc.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-http-request' },
			'http-response': { desc: 'Performs actions on HTTP responses: set-header, del-header, replace-value, etc.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-http-response' },

			// Options - general
			'option': { desc: 'Enables various protocol options and behaviors for proxies.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option' },
			'option httplog': { desc: 'Enables detailed HTTP logging with method, URI, status code, timings, and more.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20httplog' },
			'option tcplog': { desc: 'Enables detailed TCP logging with connection info and timings.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20tcplog' },
			'option dontlognull': { desc: 'Prevents logging of connections that transfer no data (health checks, probes).', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20dontlognull' },
			'option forwardfor': { desc: 'Adds X-Forwarded-For header with client IP so backends know the real client address.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20forwardfor' },
			'option http-server-close': { desc: 'Closes connection to server after each response but keeps client connection open. Good for HTTP/1.1.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20http-server-close' },
			'option http-keep-alive': { desc: 'Enables HTTP keep-alive on both client and server sides. Default in modern HAProxy.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20http-keep-alive' },
			'option httpclose': { desc: 'Forces connection close after each request/response. Deprecated, use http-server-close instead.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20httpclose' },
			'option redispatch': { desc: 'Allows re-dispatching a request to another server if the original server fails mid-connection.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20redispatch' },
			'option abortonclose': { desc: 'Aborts requests in queue when the client closes the connection. Saves backend resources.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20abortonclose' },
			'option httpchk': { desc: 'Enables HTTP health checks to backend servers. Specify method and URI to check.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20httpchk' },
			'option tcp-check': { desc: 'Enables advanced TCP health check sequences with send/expect patterns.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20tcp-check' },
			'option ssl-hello-chk': { desc: 'Sends SSLv3 client hello for health checks. Tests if server handles SSL.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20ssl-hello-chk' },
			'option log-health-checks': { desc: 'Logs health check status changes (up/down) for monitoring and debugging.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-option%20log-health-checks' },
			'stats': { desc: 'Configures the built-in statistics page. Options: enable, uri, auth, refresh, admin.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-stats%20enable' },

			// Health checks
			'check': { desc: 'Enables health checking on a server. Used with inter, fall, rise options.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#5.2-check' },

			// SSL/TLS
			'ssl': { desc: 'Enables SSL/TLS on a bind or server directive. Use with crt, ca-file, verify options.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#5.1-ssl' },
			'crt': { desc: 'Specifies the path to the SSL certificate file (PEM format with cert + key).', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#5.1-crt' },
			'ssl-default-bind-ciphers': { desc: 'Sets the default SSL cipher list for all bind lines. Defines which encryption algorithms are allowed for incoming connections.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-ssl-default-bind-ciphers' },
			'ssl-default-bind-ciphersuites': { desc: 'Sets the default TLS 1.3 cipher suites for all bind lines. TLS 1.3 uses separate ciphersuites from earlier versions.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-ssl-default-bind-ciphersuites' },
			'ssl-default-bind-options': { desc: 'Sets default SSL options for all bind lines: minimum TLS version, ticket support, ALPN protocols, etc.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-ssl-default-bind-options' },
			'ssl-default-server-ciphers': { desc: 'Sets the default SSL cipher list for connections to backend servers.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-ssl-default-server-ciphers' },
			'ssl-default-server-ciphersuites': { desc: 'Sets the default TLS 1.3 cipher suites for connections to backend servers.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-ssl-default-server-ciphersuites' },
			'ssl-default-server-options': { desc: 'Sets default SSL options for connections to backend servers.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-ssl-default-server-options' },
			'ca-base': { desc: 'Sets the default directory for CA certificate files, used with ca-file directives.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-ca-base' },
			'crt-base': { desc: 'Sets the default directory for certificate files, used with crt directives.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-crt-base' },

			// Stick tables
			'stick-table': { desc: 'Creates a table to track connection data by IP, session, etc. Used for rate limiting.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-stick-table' },
			'stick': { desc: 'Stores or matches entries in a stick table for session persistence.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-stick' },

			// Global settings
			'daemon': { desc: 'Runs HAProxy as a background daemon process.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-daemon' },
			'user': { desc: 'Sets the user HAProxy runs as after startup (requires root to start).', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-user' },
			'group': { desc: 'Sets the group HAProxy runs as after startup.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-group' },
			'chroot': { desc: 'Changes root directory for the process (security hardening).', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-chroot' },
			'pidfile': { desc: 'Specifies the path to write the process ID file.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-pidfile' },

			// Logging
			'log-format': { desc: 'Defines a custom log format string with variables like %ci, %cp, %ft, %b, %s.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#8.2.4' },

			// Compression
			'compression': { desc: 'Enables HTTP compression. Options: algo (gzip, deflate), type (mime types).', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-compression' },

			// Error handling
			'errorfile': { desc: 'Specifies a custom error page file for specific HTTP status codes.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-errorfile' },
			'errorloc': { desc: 'Redirects to a URL when specific HTTP errors occur.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-errorloc' },

			// Retries
			'retries': { desc: 'Number of times to retry a connection to a server after failure.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-retries' },

			// TCP rules
			'tcp-request': { desc: 'Performs actions on TCP connections: accept, reject, track counters, etc.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-tcp-request' },
			'tcp-response': { desc: 'Performs actions on TCP responses from backends.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-tcp-response' },

			// HTTP check
			'http-check': { desc: 'Configures HTTP health checks. Options: send, expect, disable-on-404.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-http-check' },

			// Capture
			'capture': { desc: 'Captures request or response headers/cookies for logging.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-capture' },

			// Redirect
			'redirect': { desc: 'Redirects requests. Schemes: location, prefix, scheme. Codes: 301, 302, 303.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-redirect' },

			// Rate limiting related
			'rate-limit': { desc: 'Limits the rate of sessions. Used in conjunction with stick-tables.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-rate-limit%20sessions' },

			// Tuning - general
			'tune': { desc: 'Performance tuning options for buffers, SSL, HTTP/2, and other subsystems.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.bufsize' },
			'tune.bufsize': { desc: 'Sets the buffer size for request/response data. Larger values handle bigger headers but use more memory. Default is 16384.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.bufsize' },
			'tune.maxrewrite': { desc: 'Sets reserved buffer space for header rewriting. Increase if you add large headers.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.maxrewrite' },

			// Tuning - SSL
			'tune.ssl.default-dh-param': { desc: 'Sets the size of DH parameters for DHE key exchange. 2048 is recommended minimum for security.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.ssl.default-dh-param' },
			'tune.ssl.cachesize': { desc: 'Sets the number of SSL session cache entries. Larger values improve SSL resumption but use more memory.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.ssl.cachesize' },
			'tune.ssl.lifetime': { desc: 'Sets the SSL session cache lifetime in seconds. Sessions older than this require full handshake.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.ssl.lifetime' },
			'tune.ssl.maxrecord': { desc: 'Sets the maximum SSL record size. Smaller values reduce latency, larger values improve throughput.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.ssl.maxrecord' },
			'tune.ssl.capture-buffer-size': { desc: 'Sets buffer size for capturing SSL session keys (for debugging with Wireshark).', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.ssl.capture-buffer-size' },

			// Tuning - HTTP/2
			'tune.h2.header-table-size': { desc: 'Sets the HTTP/2 HPACK header compression table size. Larger values improve compression but use more memory.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.h2.header-table-size' },
			'tune.h2.initial-window-size': { desc: 'Sets the HTTP/2 initial flow control window size. Larger values allow more data before acknowledgment.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.h2.initial-window-size' },
			'tune.h2.max-concurrent-streams': { desc: 'Sets maximum concurrent HTTP/2 streams per connection. Limits parallel requests over one connection.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.h2.max-concurrent-streams' },
			'tune.h2.max-frame-size': { desc: 'Sets the maximum HTTP/2 frame size. Larger frames reduce overhead but may increase latency.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.2-tune.h2.max-frame-size' },

			// CPU and threading
			'nbthread': { desc: 'Sets the number of threads HAProxy will use. Should match available CPU cores.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-nbthread' },
			'cpu-map': { desc: 'Binds threads or processes to specific CPU cores for better performance.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-cpu-map' },

			// SSL global defaults (hyphen-prefixed, matched via "ssl" prefix)
			// These are covered by the 'ssl' entry via prefix matching

			// Resolvers
			'resolvers': { desc: 'Defines a DNS resolver section for dynamic server resolution.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#5.3' },
			'nameserver': { desc: 'Specifies a DNS nameserver within a resolvers section.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#5.3' },
			'resolve_retries': { desc: 'Number of DNS query retries before giving up.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#5.3' },
			'hold': { desc: 'Sets how long to cache DNS responses (valid, obsolete, nx, etc.).', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#5.3' },

			// Mailers
			'mailers': { desc: 'Defines email alert configuration for health check notifications.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.6' },
			'mailer': { desc: 'Specifies an SMTP server within a mailers section.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.6' },

			// Peers (for stick-table sync)
			'peers': { desc: 'Defines a peers section for synchronizing stick-tables between HAProxy instances.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.5' },
			'peer': { desc: 'Specifies a peer HAProxy instance within a peers section.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.5' },

			// Additional common directives
			'unique-id-format': { desc: 'Defines the format for generating unique request IDs for tracing.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-unique-id-format' },
			'unique-id-header': { desc: 'Sets the header name to store the unique request ID.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-unique-id-header' },
			'hash-type': { desc: 'Configures the hash function for consistent hashing load balancing.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-hash-type' },
			'cookie': { desc: 'Enables cookie-based session persistence. Inserts or rewrites cookies.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-cookie' },
			'fullconn': { desc: 'Sets the connection threshold for applying slowstart and weights.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-fullconn' },
			'grace': { desc: 'Sets the grace period during soft-stop for finishing active connections.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-grace' },
			'hard-stop-after': { desc: 'Maximum time to wait for connections to close during soft-stop.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-hard-stop-after' },
			'spread-checks': { desc: 'Randomizes health check intervals to prevent thundering herd.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#3.1-spread-checks' },
			'external-check': { desc: 'Enables external command-based health checks.', docs: 'https://www.haproxy.com/documentation/haproxy-configuration-manual/latest/#4.2-external-check' },
		};

		// Configuration snippets organized by category
		// Each snippet has: name, desc (brief), code, and details (line-by-line explanation)
		const SNIPPETS = {
			'Sections': [
				{
					name: 'Frontend (HTTP)',
					desc: 'Basic HTTP frontend listening on port 80. A frontend defines how HAProxy receives incoming connections and where to route them.',
					code: `frontend http_front
    bind *:80
    mode http
    default_backend app_servers`,
					details: [
						{ line: 'frontend http_front', explain: 'Declares a new frontend section named "http_front". The name is used for logging and can be referenced elsewhere.' },
						{ line: 'bind *:80', explain: 'Listen on all network interfaces (*) on port 80. You can specify an IP like "192.168.1.1:80" to bind to a specific interface.' },
						{ line: 'mode http', explain: 'Operate in HTTP mode (Layer 7). This enables HTTP-specific features like header inspection, cookies, and URL routing. Use "mode tcp" for raw TCP.' },
						{ line: 'default_backend app_servers', explain: 'Send all traffic to the backend named "app_servers" unless other routing rules (use_backend) match first.' },
					]
				},
				{
					name: 'Frontend (HTTPS)',
					desc: 'HTTPS frontend with SSL/TLS termination. HAProxy decrypts incoming HTTPS traffic and can forward to backends over HTTP or HTTPS.',
					code: `frontend https_front
    bind *:443 ssl crt /etc/ssl/certs/site.pem
    mode http
    default_backend app_servers`,
					details: [
						{ line: 'frontend https_front', explain: 'Declares a new frontend section named "https_front" for handling HTTPS connections.' },
						{ line: 'bind *:443 ssl crt /etc/ssl/certs/site.pem', explain: 'Listen on port 443 with SSL enabled. The "crt" option points to a PEM file containing the certificate AND private key. For multiple domains, use a directory or multiple crt options.' },
						{ line: 'mode http', explain: 'Operate in HTTP mode. Even though traffic arrives encrypted, after SSL termination HAProxy sees plain HTTP and can inspect headers, paths, etc.' },
						{ line: 'default_backend app_servers', explain: 'Route all decrypted traffic to the "app_servers" backend.' },
					]
				},
				{
					name: 'Backend',
					desc: 'A backend defines a pool of servers that can receive traffic. It includes load balancing algorithm, health checks, and server definitions.',
					code: `backend app_servers
    mode http
    balance roundrobin
    option httpchk GET /health
    server srv1 10.0.0.1:8080 check
    server srv2 10.0.0.2:8080 check`,
					details: [
						{ line: 'backend app_servers', explain: 'Declares a backend named "app_servers". Frontends reference this name to route traffic here.' },
						{ line: 'mode http', explain: 'Must match the frontend mode. HTTP mode enables Layer 7 features like health checks with HTTP requests.' },
						{ line: 'balance roundrobin', explain: 'Distribute requests evenly across servers in rotation. Other options: "leastconn" (fewest connections), "source" (sticky by client IP), "uri" (sticky by URL).' },
						{ line: 'option httpchk GET /health', explain: 'Perform HTTP health checks by requesting GET /health from each server. Servers that dont return 2xx/3xx are marked down.' },
						{ line: 'server srv1 10.0.0.1:8080 check', explain: 'Define a server named "srv1" at IP:port. The "check" option enables health checking for this server.' },
						{ line: 'server srv2 10.0.0.2:8080 check', explain: 'Second server in the pool. Traffic is distributed between srv1 and srv2 based on the balance algorithm.' },
					]
				},
				{
					name: 'Listen (Stats Page)',
					desc: 'A "listen" section combines frontend and backend in one block. This example creates HAProxys built-in statistics dashboard.',
					code: `listen stats
    bind *:8404
    mode http
    stats enable
    stats uri /stats
    stats refresh 10s
    stats auth admin:password`,
					details: [
						{ line: 'listen stats', explain: 'Creates a combined frontend+backend named "stats". Listen sections are convenient for simple services that dont need separate frontend/backend.' },
						{ line: 'bind *:8404', explain: 'Listen on port 8404. Using a non-standard port helps prevent accidental exposure of the stats page.' },
						{ line: 'mode http', explain: 'HTTP mode is required for the stats page to render properly in a browser.' },
						{ line: 'stats enable', explain: 'Activates the built-in statistics page feature. Without this, HAProxy wont serve the dashboard.' },
						{ line: 'stats uri /stats', explain: 'The URL path where the stats page is accessible. Visit http://server:8404/stats to view it.' },
						{ line: 'stats refresh 10s', explain: 'Auto-refresh the page every 10 seconds. The browser will reload automatically to show updated metrics.' },
						{ line: 'stats auth admin:password', explain: 'Require HTTP Basic authentication. CHANGE THIS PASSWORD! Consider using stats admin if to restrict admin actions.' },
					]
				},
			],
			'Timeouts': [
				{
					name: 'Standard Timeouts',
					desc: 'Essential timeout values that prevent hung connections. These should be in your defaults or every frontend/backend section.',
					code: `    timeout connect 5s
    timeout client 30s
    timeout server 30s`,
					details: [
						{ line: 'timeout connect 5s', explain: 'Maximum time to wait for a connection to a backend server to establish. If a server doesnt respond within 5 seconds, HAProxy tries the next server (if available).' },
						{ line: 'timeout client 30s', explain: 'Maximum inactivity time on the client side. If the client doesnt send data for 30 seconds, the connection is closed. Prevents idle connections from consuming resources.' },
						{ line: 'timeout server 30s', explain: 'Maximum inactivity time on the server side. If the backend doesnt send data for 30 seconds, the connection is closed. Adjust based on your slowest API responses.' },
					]
				},
				{
					name: 'Long-polling / WebSocket',
					desc: 'Extended timeouts for applications that hold connections open for long periods, like WebSockets, Server-Sent Events, or long-polling APIs.',
					code: `    timeout client 300s
    timeout server 300s
    timeout tunnel 1h`,
					details: [
						{ line: 'timeout client 300s', explain: '5-minute client timeout allows for long periods without data from the client, common in WebSocket applications where the server pushes most data.' },
						{ line: 'timeout server 300s', explain: '5-minute server timeout accommodates servers that may not send data frequently but need to keep the connection alive.' },
						{ line: 'timeout tunnel 1h', explain: 'The tunnel timeout applies to bidirectional data flow after a connection is established (like WebSocket after upgrade). 1 hour allows very long-lived connections.' },
					]
				},
				{
					name: 'Queue Timeout',
					desc: 'Controls how long requests wait in queue when all backend servers are at their connection limit.',
					code: `    timeout queue 30s`,
					details: [
						{ line: 'timeout queue 30s', explain: 'Maximum time a request waits in the queue for a server slot. If all servers are at maxconn and 30 seconds pass, the client gets a 503 error. Set based on acceptable user wait time.' },
					]
				},
			],
			'ACLs & Routing': [
				{
					name: 'Host-based Routing',
					desc: 'Route requests to different backends based on the Host header. Essential for hosting multiple domains/applications behind one HAProxy instance.',
					code: `    acl host_app hdr(host) -i app.example.com
    use_backend app_servers if host_app`,
					details: [
						{ line: 'acl host_app hdr(host) -i app.example.com', explain: 'Creates an ACL named "host_app" that matches when the Host header equals "app.example.com". The "-i" flag makes it case-insensitive. hdr(host) extracts the Host header value.' },
						{ line: 'use_backend app_servers if host_app', explain: 'If the "host_app" ACL matches (Host header is app.example.com), send the request to the "app_servers" backend instead of the default backend.' },
					]
				},
				{
					name: 'Path-based Routing',
					desc: 'Route requests based on URL path. Useful for microservices where /api goes to one backend and /static to another.',
					code: `    acl path_api path_beg /api/
    use_backend api_servers if path_api`,
					details: [
						{ line: 'acl path_api path_beg /api/', explain: 'Creates an ACL named "path_api" that matches when the URL path begins with "/api/". Other options: path_end (suffix), path_sub (contains), path_reg (regex).' },
						{ line: 'use_backend api_servers if path_api', explain: 'Routes requests with paths starting with /api/ to the "api_servers" backend. Requests to /api/users, /api/v2/data, etc. all match.' },
					]
				},
				{
					name: 'Method-based ACL',
					desc: 'Match requests by HTTP method. Useful for allowing only certain methods or routing write operations differently.',
					code: `    acl is_post method POST
    acl is_get method GET`,
					details: [
						{ line: 'acl is_post method POST', explain: 'Creates an ACL that matches HTTP POST requests. Commonly used to route write operations or apply different rate limits to POST vs GET.' },
						{ line: 'acl is_get method GET', explain: 'Creates an ACL that matches HTTP GET requests. You can combine ACLs: "use_backend read_servers if is_get" and "use_backend write_servers if is_post".' },
					]
				},
				{
					name: 'Source IP ACL',
					desc: 'Match requests from specific IP addresses or ranges. Essential for restricting access to admin endpoints or internal services.',
					code: `    acl internal_network src 10.0.0.0/8 192.168.0.0/16
    http-request deny unless internal_network`,
					details: [
						{ line: 'acl internal_network src 10.0.0.0/8 192.168.0.0/16', explain: 'Creates an ACL matching requests from private IP ranges (10.x.x.x and 192.168.x.x). "src" checks the client source IP address.' },
						{ line: 'http-request deny unless internal_network', explain: 'Denies (returns 403) any request NOT from the internal network. Use this to protect admin panels or internal APIs from external access.' },
					]
				},
			],
			'Health Checks': [
				{
					name: 'HTTP Health Check',
					desc: 'Verify backend servers are healthy by making HTTP requests to a health endpoint. Unhealthy servers are automatically removed from the pool.',
					code: `    option httpchk GET /health
    http-check expect status 200`,
					details: [
						{ line: 'option httpchk GET /health', explain: 'Configures HAProxy to send HTTP GET requests to /health on each backend server. The server should return quickly and indicate its status.' },
						{ line: 'http-check expect status 200', explain: 'Only consider the server healthy if it returns HTTP 200. Without this, any 2xx or 3xx is considered healthy. You can also use "expect string OK" to check response body.' },
					]
				},
				{
					name: 'TCP Health Check',
					desc: 'Simple connectivity check that verifies the server accepts TCP connections. Use when HTTP health checks arent appropriate.',
					code: `    option tcp-check
    tcp-check connect`,
					details: [
						{ line: 'option tcp-check', explain: 'Enables TCP-level health checking instead of simple connection checks. Allows for more complex check sequences.' },
						{ line: 'tcp-check connect', explain: 'The check sequence: connect to the server. If the TCP connection succeeds, the server is healthy. Use for non-HTTP services like databases or custom protocols.' },
					]
				},
				{
					name: 'Server with Check Options',
					desc: 'Fine-tune health check behavior per server: how often to check, how many failures before marking down, and how many successes to recover.',
					code: `    server srv1 10.0.0.1:8080 check inter 3s fall 3 rise 2`,
					details: [
						{ line: 'server srv1 10.0.0.1:8080 check inter 3s fall 3 rise 2', explain: '"check" enables health checks. "inter 3s" checks every 3 seconds. "fall 3" marks server down after 3 consecutive failures. "rise 2" marks server up after 2 consecutive successes.' },
					]
				},
			],
			'Logging': [
				{
					name: 'HTTP Logging',
					desc: 'Enable detailed HTTP request logging with timing, status codes, and more. Essential for debugging and monitoring.',
					code: `    option httplog
    log global`,
					details: [
						{ line: 'option httplog', explain: 'Enables detailed HTTP logging format that includes: client IP, timestamps, request method, URL, HTTP version, status code, bytes sent, timings, and more.' },
						{ line: 'log global', explain: 'Use the log destination defined in the global section. This avoids repeating log server configuration in every frontend/backend.' },
					]
				},
				{
					name: 'Custom Log Format',
					desc: 'Define exactly what information to log. This format includes detailed timing breakdown useful for performance analysis.',
					code: `    log-format "%ci:%cp [%tr] %ft %b/%s %TR/%Tw/%Tc/%Tr/%Ta %ST %B %CC %CS %tsc %ac/%fc/%bc/%sc/%rc %sq/%bq %hr %hs %{+Q}r"`,
					details: [
						{ line: 'log-format "..."', explain: '%ci:%cp = client IP:port. %tr = request timestamp. %ft = frontend name. %b/%s = backend/server. %TR/%Tw/%Tc/%Tr/%Ta = timing breakdown (request/wait/connect/response/total). %ST = status code. %B = bytes. %{+Q}r = quoted request line.' },
					]
				},
				{
					name: 'Log to Syslog',
					desc: 'Send HAProxy logs to the local syslog daemon. This is the standard way to handle logs on Linux systems.',
					code: `    log /dev/log local0 info`,
					details: [
						{ line: 'log /dev/log local0 info', explain: 'Send logs to /dev/log (syslog socket) using facility "local0" at "info" level. Configure rsyslog to route local0 to a dedicated HAProxy log file.' },
					]
				},
			],
			'Rate Limiting': [
				{
					name: 'Stick Table for Rate Limiting',
					desc: 'Create an in-memory table to track request rates by client IP. This is the foundation for rate limiting rules.',
					code: `    stick-table type ip size 100k expire 30s store http_req_rate(10s)`,
					details: [
						{ line: 'stick-table type ip size 100k expire 30s store http_req_rate(10s)', explain: '"type ip" tracks by client IP. "size 100k" stores up to 100,000 entries. "expire 30s" removes inactive entries after 30s. "store http_req_rate(10s)" tracks requests per 10-second window.' },
					]
				},
				{
					name: 'Rate Limit Enforcement',
					desc: 'Block clients that exceed a request rate threshold. Returns 429 Too Many Requests to abusive clients.',
					code: `    http-request track-sc0 src
    acl rate_abuse sc_http_req_rate(0) gt 100
    http-request deny deny_status 429 if rate_abuse`,
					details: [
						{ line: 'http-request track-sc0 src', explain: 'Track the client source IP in stick counter 0 (sc0). This increments the request counter for this IP in the stick table.' },
						{ line: 'acl rate_abuse sc_http_req_rate(0) gt 100', explain: 'Create an ACL that matches when the request rate for this IP (from counter 0) exceeds 100 requests per the tracking period (10s from stick-table).' },
						{ line: 'http-request deny deny_status 429 if rate_abuse', explain: 'If rate_abuse matches, deny the request with HTTP 429 (Too Many Requests). The client should back off and retry later.' },
					]
				},
				{
					name: 'Connection Rate Limit',
					desc: 'Limit how many new connections per second an IP can make. Protects against connection flood attacks.',
					code: `    stick-table type ip size 100k expire 30s store conn_rate(10s)
    tcp-request connection track-sc0 src
    tcp-request connection reject if { sc_conn_rate(0) gt 50 }`,
					details: [
						{ line: 'stick-table type ip size 100k expire 30s store conn_rate(10s)', explain: 'Track new connection rate per IP over 10-second windows. Different from http_req_rate which counts HTTP requests.' },
						{ line: 'tcp-request connection track-sc0 src', explain: 'Track at the TCP connection level (before HTTP parsing). Catches attacks that open many connections without completing HTTP requests.' },
						{ line: 'tcp-request connection reject if { sc_conn_rate(0) gt 50 }', explain: 'Reject new connections if this IP has opened more than 50 connections in the last 10 seconds. The inline ACL syntax { } is a shorthand.' },
					]
				},
			],
			'SSL/TLS': [
				{
					name: 'SSL Termination',
					desc: 'Accept HTTPS connections with modern TLS settings and HTTP/2 support. HAProxy handles encryption so backends can use plain HTTP.',
					code: `    bind *:443 ssl crt /etc/ssl/certs/site.pem alpn h2,http/1.1`,
					details: [
						{ line: 'bind *:443 ssl crt /etc/ssl/certs/site.pem alpn h2,http/1.1', explain: '"ssl" enables TLS. "crt" points to PEM file (cert+key). "alpn h2,http/1.1" advertises HTTP/2 and HTTP/1.1 via ALPN, allowing modern browsers to use HTTP/2.' },
					]
				},
				{
					name: 'SSL Backend Connection',
					desc: 'Connect to backend servers over SSL/TLS. Use when backends require encrypted connections or for end-to-end encryption.',
					code: `    server srv1 10.0.0.1:443 ssl verify required ca-file /etc/ssl/ca.pem`,
					details: [
						{ line: 'server srv1 10.0.0.1:443 ssl verify required ca-file /etc/ssl/ca.pem', explain: '"ssl" connects to backend over TLS. "verify required" ensures the backend cert is valid. "ca-file" specifies the CA certificate to verify against. Use "verify none" only for testing!' },
					]
				},
				{
					name: 'Redirect HTTP to HTTPS',
					desc: 'Automatically redirect all HTTP requests to HTTPS. Essential for security - ensures all traffic is encrypted.',
					code: `    http-request redirect scheme https unless { ssl_fc }`,
					details: [
						{ line: 'http-request redirect scheme https unless { ssl_fc }', explain: '"redirect scheme https" changes the URL scheme to https. "ssl_fc" is true if the connection arrived over SSL. So this redirects only non-SSL requests, avoiding redirect loops.' },
					]
				},
				{
					name: 'HSTS Header',
					desc: 'Tell browsers to always use HTTPS for this domain. Prevents downgrade attacks and improves security.',
					code: `    http-response set-header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"`,
					details: [
						{ line: 'http-response set-header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"', explain: 'HSTS header tells browsers to only use HTTPS for 1 year (31536000s). "includeSubDomains" applies to all subdomains. "preload" allows inclusion in browser preload lists.' },
					]
				},
			],
			'Headers': [
				{
					name: 'X-Forwarded Headers',
					desc: 'Pass original client information to backend servers. Without this, backends only see HAProxys IP address.',
					code: `    option forwardfor
    http-request set-header X-Forwarded-Proto https if { ssl_fc }
    http-request set-header X-Forwarded-Port %[dst_port]`,
					details: [
						{ line: 'option forwardfor', explain: 'Automatically adds X-Forwarded-For header with the client IP. Backends can use this to see the real client IP instead of HAProxys IP.' },
						{ line: 'http-request set-header X-Forwarded-Proto https if { ssl_fc }', explain: 'Set X-Forwarded-Proto to "https" if the original connection was SSL. Backends use this to generate correct URLs and set secure cookies.' },
						{ line: 'http-request set-header X-Forwarded-Port %[dst_port]', explain: 'Set X-Forwarded-Port to the original destination port. %[dst_port] is a sample fetch that gets the port the client connected to.' },
					]
				},
				{
					name: 'Security Headers',
					desc: 'Add security headers to all responses. These protect against common web vulnerabilities like clickjacking and XSS.',
					code: `    http-response set-header X-Frame-Options DENY
    http-response set-header X-Content-Type-Options nosniff
    http-response set-header X-XSS-Protection "1; mode=block"`,
					details: [
						{ line: 'http-response set-header X-Frame-Options DENY', explain: 'Prevents the page from being embedded in iframes. Protects against clickjacking attacks. Use "SAMEORIGIN" if you need to embed your own pages.' },
						{ line: 'http-response set-header X-Content-Type-Options nosniff', explain: 'Prevents browsers from MIME-type sniffing. Ensures browsers respect the declared Content-Type, preventing certain attacks.' },
						{ line: 'http-response set-header X-XSS-Protection "1; mode=block"', explain: 'Enables browser XSS filtering. "mode=block" stops rendering if an attack is detected. Note: modern browsers are deprecating this in favor of CSP.' },
					]
				},
				{
					name: 'Remove Server Header',
					desc: 'Hide backend server identity from responses. Prevents attackers from knowing what software powers your backend.',
					code: `    http-response del-header Server`,
					details: [
						{ line: 'http-response del-header Server', explain: 'Removes the Server header that backends typically add (like "Server: nginx/1.18.0"). This is security through obscurity but reduces information disclosure.' },
					]
				},
				{
					name: 'CORS Headers',
					desc: 'Enable Cross-Origin Resource Sharing for APIs accessed from different domains. Required for browser-based JavaScript clients.',
					code: `    http-response set-header Access-Control-Allow-Origin "*"
    http-response set-header Access-Control-Allow-Methods "GET, POST, OPTIONS"
    http-response set-header Access-Control-Allow-Headers "Content-Type, Authorization"`,
					details: [
						{ line: 'http-response set-header Access-Control-Allow-Origin "*"', explain: 'Allow requests from any origin. For production, replace "*" with specific domains like "https://app.example.com" for better security.' },
						{ line: 'http-response set-header Access-Control-Allow-Methods "GET, POST, OPTIONS"', explain: 'Specify which HTTP methods are allowed for cross-origin requests. Add PUT, DELETE, etc. as needed by your API.' },
						{ line: 'http-response set-header Access-Control-Allow-Headers "Content-Type, Authorization"', explain: 'Specify which headers the client can send in cross-origin requests. Add custom headers your API expects here.' },
					]
				},
			],
			'Compression': [
				{
					name: 'Enable Gzip Compression',
					desc: 'Compress responses to reduce bandwidth and improve load times. HAProxy compresses on-the-fly before sending to clients.',
					code: `    compression algo gzip
    compression type text/html text/plain text/css application/javascript application/json`,
					details: [
						{ line: 'compression algo gzip', explain: 'Use gzip compression algorithm. Other options include "deflate" and "identity" (no compression). Gzip has the best browser support.' },
						{ line: 'compression type text/html text/plain text/css application/javascript application/json', explain: 'Only compress these MIME types. Text-based formats compress well (often 70-90% reduction). Dont compress images or already-compressed files.' },
					]
				},
			],
			'Error Handling': [
				{
					name: 'Custom Error Pages',
					desc: 'Serve branded error pages instead of HAProxys defaults. Improves user experience when errors occur.',
					code: `    errorfile 400 /etc/haproxy/errors/400.http
    errorfile 403 /etc/haproxy/errors/403.http
    errorfile 500 /etc/haproxy/errors/500.http
    errorfile 502 /etc/haproxy/errors/502.http
    errorfile 503 /etc/haproxy/errors/503.http`,
					details: [
						{ line: 'errorfile 400 /etc/haproxy/errors/400.http', explain: 'Serve this file for 400 Bad Request errors. The file must be a complete HTTP response including headers (HTTP/1.1 400...\\r\\nContent-Type: text/html\\r\\n...).' },
						{ line: 'errorfile 403 /etc/haproxy/errors/403.http', explain: 'Custom 403 Forbidden page. Shown when access is denied by ACL rules.' },
						{ line: 'errorfile 500 /etc/haproxy/errors/500.http', explain: 'Custom 500 Internal Server Error page. Shown for server-side errors.' },
						{ line: 'errorfile 502 /etc/haproxy/errors/502.http', explain: 'Custom 502 Bad Gateway page. Shown when HAProxy cant connect to any backend server.' },
						{ line: 'errorfile 503 /etc/haproxy/errors/503.http', explain: 'Custom 503 Service Unavailable page. Shown when all backend servers are down or the queue timeout is exceeded.' },
					]
				},
				{
					name: 'Retry on Failure',
					desc: 'Automatically retry failed requests on other servers. Improves reliability when individual servers have transient failures.',
					code: `    retries 3
    option redispatch`,
					details: [
						{ line: 'retries 3', explain: 'Retry up to 3 times if a connection to a backend fails. Each retry can go to a different server depending on redispatch setting.' },
						{ line: 'option redispatch', explain: 'Allow HAProxy to retry on a different server if the original server fails. Without this, retries go to the same server. Essential for high availability.' },
					]
				},
			],
		};

		// ============================================
		// LINE NUMBERS
		// ============================================

		function updateLineNumbers() {
			const editor = document.getElementById('config-editor');
			const lineNumbers = document.getElementById('line-numbers');
			if (!editor || !lineNumbers) return;

			// Get content from editor
			const content = editor.innerText || '';
			editorLines = content.split('\n');
			const count = editorLines.length;

			// Get all line divs from editor to measure their heights
			const lineDivs = editor.querySelectorAll('.editor-line');

			let html = '';
			for (let i = 1; i <= count; i++) {
				// Get the height of the corresponding line div (if it exists)
				let heightStyle = '';
				if (lineDivs[i - 1]) {
					const lineHeight = lineDivs[i - 1].getBoundingClientRect().height;
					heightStyle = ' style="height: ' + lineHeight + 'px; line-height: ' + lineHeight + 'px;"';
				}
				html += '<div class="line-num" data-line="' + i + '" onmouseenter="showLineHelp(event, ' + i + ')" onmouseleave="hideLineHelp()" onclick="openDocsForLine(' + i + ')"' + heightStyle + '>' + i + '</div>';
			}
			lineNumbers.innerHTML = html;

			// Match font size with editor
			lineNumbers.style.fontSize = editor.style.fontSize || '14px';
		}

		function syncLineNumbersScroll() {
			const editor = document.getElementById('config-editor');
			const lineNumbers = document.getElementById('line-numbers');
			if (editor && lineNumbers) {
				lineNumbers.scrollTop = editor.scrollTop;
			}
		}

		// ============================================
		// LINE HELP TOOLTIP
		// ============================================

		function parseDirectiveFromLine(line) {
			const trimmed = line.trim();

			// Skip comments and empty lines
			if (!trimmed || trimmed.startsWith('#')) return null;

			// Check for section headers
			const sectionMatch = trimmed.match(/^(global|defaults|frontend|backend|listen|resolvers|mailers|peers|cache)\b/i);
			if (sectionMatch) return sectionMatch[1].toLowerCase();

			// Get first and second words for compound directive matching
			const words = trimmed.split(/\s+/);
			const firstWord = words[0].toLowerCase();
			const secondWord = words[1] ? words[1].toLowerCase() : '';

			// Try compound match first (e.g., "timeout connect", "option httplog")
			if (secondWord) {
				const compound = firstWord + ' ' + secondWord;
				if (HAPROXY_HELP[compound]) return compound;
			}

			// Check if first word is a known directive (exact match)
			if (HAPROXY_HELP[firstWord]) return firstWord;

			// Check for dot-prefixed directives like "tune.h2.header-table-size"
			if (firstWord.includes('.')) {
				const prefix = firstWord.split('.')[0];
				if (HAPROXY_HELP[prefix]) return prefix;
			}

			// Check for hyphen-prefixed directives like "ssl-default-bind-ciphers"
			if (firstWord.includes('-')) {
				const prefix = firstWord.split('-')[0];
				if (HAPROXY_HELP[prefix]) return prefix;
			}

			// Check for compound directives like "timeout connect"
			const twoWords = trimmed.match(/^(\S+)\s+(\S+)/);
			if (twoWords) {
				const compound = twoWords[1].toLowerCase();
				if (HAPROXY_HELP[compound]) return compound;
			}

			return null;
		}

		function showLineHelp(event, lineNum) {
			if (lineNum < 1 || lineNum > editorLines.length) return;

			const line = editorLines[lineNum - 1];
			const directive = parseDirectiveFromLine(line);

			if (!directive || !HAPROXY_HELP[directive]) return;

			const help = HAPROXY_HELP[directive];
			const tooltip = document.getElementById('line-help-tooltip');
			const directiveEl = document.getElementById('tooltip-directive');
			const descEl = document.getElementById('tooltip-description');

			directiveEl.textContent = directive;
			descEl.textContent = help.desc;

			// Position tooltip near the line number
			const rect = event.target.getBoundingClientRect();
			tooltip.style.left = (rect.right + 10) + 'px';
			tooltip.style.top = (rect.top - 10) + 'px';
			tooltip.classList.remove('hidden');
		}

		function hideLineHelp() {
			const tooltip = document.getElementById('line-help-tooltip');
			tooltip.classList.add('hidden');
		}

		function openDocsForLine(lineNum) {
			if (lineNum < 1 || lineNum > editorLines.length) return;

			const line = editorLines[lineNum - 1];
			const directive = parseDirectiveFromLine(line);

			if (directive && HAPROXY_HELP[directive] && HAPROXY_HELP[directive].docs) {
				window.open(HAPROXY_HELP[directive].docs, '_blank');
			} else {
				// Open general docs
				window.open('https://www.haproxy.com/documentation/haproxy-configuration-tutorials/', '_blank');
			}
		}

		// ============================================
		// SNIPPETS MODAL
		// ============================================

		function showSnippets() {
			const modal = document.getElementById('snippets-modal');
			modal.classList.remove('hidden');
			renderSnippetCategories();
			document.getElementById('snippet-search').value = '';
			document.getElementById('snippet-search').focus();
			selectedSnippet = null;
			updateInsertButton();
		}

		function hideSnippets() {
			document.getElementById('snippets-modal').classList.add('hidden');
			selectedSnippet = null;
		}

		function renderSnippetCategories(filter = '') {
			const container = document.getElementById('snippet-categories');
			let html = '';

			for (const [category, snippets] of Object.entries(SNIPPETS)) {
				const filteredSnippets = filter
					? snippets.filter(s =>
						s.name.toLowerCase().includes(filter.toLowerCase()) ||
						s.desc.toLowerCase().includes(filter.toLowerCase()) ||
						s.code.toLowerCase().includes(filter.toLowerCase())
					)
					: snippets;

				if (filteredSnippets.length === 0) continue;

				html += `<div class="mb-4">
					<h4 class="text-sm font-semibold text-gray-600 dark:text-gray-400 mb-2">${escapeHtml(category)}</h4>
					<div class="space-y-1">`;

				for (const snippet of filteredSnippets) {
					const snippetId = `${category}:${snippet.name}`;
					html += `<button
						onclick="selectSnippet('${escapeHtml(category)}', '${escapeHtml(snippet.name)}')"
						class="snippet-item w-full text-left px-3 py-2 rounded-lg text-sm transition-colors hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300"
						data-snippet-id="${escapeHtml(snippetId)}"
					>
						${escapeHtml(snippet.name)}
					</button>`;
				}

				html += '</div></div>';
			}

			if (!html) {
				html = '<p class="text-gray-500 dark:text-gray-400 text-sm p-4">No snippets match your search.</p>';
			}

			container.innerHTML = html;
		}

		function selectSnippet(category, name) {
			const snippet = SNIPPETS[category]?.find(s => s.name === name);
			if (!snippet) return;

			selectedSnippet = snippet;

			// Update visual selection
			document.querySelectorAll('.snippet-item').forEach(el => {
				el.classList.remove('bg-purple-100', 'dark:bg-purple-900/30');
			});
			const selectedEl = document.querySelector(`[data-snippet-id="${category}:${name}"]`);
			if (selectedEl) {
				selectedEl.classList.add('bg-purple-100', 'dark:bg-purple-900/30');
			}

			// Build line-by-line explanation HTML
			let detailsHtml = '';
			if (snippet.details && snippet.details.length > 0) {
				let detailItems = snippet.details.map(function(d) {
					return '<div class="text-xs">' +
						'<code class="block bg-gray-100 dark:bg-slate-700 text-blue-600 dark:text-blue-400 px-2 py-1 rounded font-mono mb-1">' + escapeHtml(d.line) + '</code>' +
						'<p class="text-gray-600 dark:text-gray-400 pl-2">' + escapeHtml(d.explain) + '</p>' +
						'</div>';
				}).join('');
				detailsHtml = '<div class="mt-4 space-y-3">' +
					'<h5 class="text-sm font-semibold text-gray-700 dark:text-gray-300 border-b border-gray-200 dark:border-slate-600 pb-1">Line-by-Line Explanation</h5>' +
					detailItems +
					'</div>';
			}

			// Show preview
			const preview = document.getElementById('snippet-preview');
			preview.innerHTML = '<h4 class="font-medium text-gray-900 dark:text-gray-100 mb-2">' + escapeHtml(snippet.name) + '</h4>' +
				'<p class="text-gray-600 dark:text-gray-400 text-sm mb-4">' + escapeHtml(snippet.desc) + '</p>' +
				'<div class="bg-[#0d0d0d] rounded-lg p-3 overflow-x-auto mb-4">' +
				'<pre class="text-gray-300 text-xs font-mono whitespace-pre">' + escapeHtml(snippet.code) + '</pre>' +
				'</div>' +
				detailsHtml;

			updateInsertButton();
		}

		function updateInsertButton() {
			const btn = document.getElementById('insert-snippet-btn');
			btn.disabled = !selectedSnippet;
		}

		function filterSnippets(query) {
			renderSnippetCategories(query);
			// Clear selection when filtering
			selectedSnippet = null;
			updateInsertButton();
			document.getElementById('snippet-preview').innerHTML = `
				<h4 class="font-medium text-gray-700 dark:text-gray-300 mb-2">Select a snippet</h4>
				<p class="text-gray-500 dark:text-gray-400 text-sm">Choose from the list on the left to see a preview and description.</p>
			`;
		}

		function insertSelectedSnippet() {
			if (!selectedSnippet) return;

			const editor = document.getElementById('config-editor');

			// Try to insert at cursor position
			const selection = window.getSelection();
			if (selection.rangeCount > 0 && editor.contains(selection.anchorNode)) {
				// Insert at cursor
				const range = selection.getRangeAt(0);
				range.deleteContents();

				// Create text node with the snippet
				const textNode = document.createTextNode(selectedSnippet.code);
				range.insertNode(textNode);

				// Move cursor to end of inserted text
				range.setStartAfter(textNode);
				range.setEndAfter(textNode);
				selection.removeAllRanges();
				selection.addRange(range);
			} else {
				// Append to end of editor
				const currentContent = editor.innerText;
				const newContent = currentContent + '\n\n' + selectedSnippet.code;
				displayConfig(mergeConfig(newContent));
			}

			// Re-highlight and update line numbers
			const content = getEditorContent();
			const parsed = parseConfig(mergeConfig(content));
			autoGenSections = parsed.sections;
			const lines = parsed.editable.split('\n');
			const highlightedLines = lines.map(line => highlightLine(line));
			editor.innerHTML = highlightedLines.join('\n');
			updateLineNumbers();

			hideSnippets();

			if (window.showToast) {
				window.showToast(`Inserted: ${selectedSnippet.name}`, 'success', 2000);
			}
		}

		function setupPageHeader() {
			const source = document.getElementById('page-header-source');
			const target = document.getElementById('header-page-content');
			if (source && target) {
				while (source.firstChild) {
					target.appendChild(source.firstChild);
				}
				source.remove();
			}
		}

		// LocalStorage keys for preferences
		const PREF_FONT_SIZE = 'haproxy-config-font-size';
		const PREF_WORD_WRAP = 'haproxy-config-word-wrap';

		// Update font size and persist
		function updateFontSize(size) {
			const editor = document.getElementById('config-editor');
			const lineNumbers = document.getElementById('line-numbers');
			const sizeDisplay = document.getElementById('font-size-value');
			editor.style.fontSize = size + 'px';
			if (lineNumbers) lineNumbers.style.fontSize = size + 'px';
			sizeDisplay.textContent = size + 'px';
			localStorage.setItem(PREF_FONT_SIZE, size);

			// Re-measure line heights after font size change
			setTimeout(updateLineNumbers, 10);
		}

		// Toggle word wrap and persist
		function toggleWordWrap() {
			const editor = document.getElementById('config-editor');
			const toggle = document.getElementById('word-wrap-toggle');
			const dot = document.getElementById('word-wrap-toggle-dot');
			const isEnabled = toggle.getAttribute('aria-checked') === 'true';

			if (isEnabled) {
				// Turn off
				toggle.setAttribute('aria-checked', 'false');
				toggle.classList.remove('bg-blue-600');
				toggle.classList.add('bg-gray-200', 'dark:bg-gray-600');
				dot.classList.remove('translate-x-5');
				dot.classList.add('translate-x-0');
				editor.classList.remove('word-wrap');
				localStorage.setItem(PREF_WORD_WRAP, 'false');
			} else {
				// Turn on
				toggle.setAttribute('aria-checked', 'true');
				toggle.classList.add('bg-blue-600');
				toggle.classList.remove('bg-gray-200', 'dark:bg-gray-600');
				dot.classList.add('translate-x-5');
				dot.classList.remove('translate-x-0');
				editor.classList.add('word-wrap');
				localStorage.setItem(PREF_WORD_WRAP, 'true');
			}

			// Re-measure line heights after word wrap change
			setTimeout(updateLineNumbers, 10);
		}

		// Set word wrap toggle state without triggering toggle
		function setWordWrapState(enabled) {
			const toggle = document.getElementById('word-wrap-toggle');
			const dot = document.getElementById('word-wrap-toggle-dot');
			const editor = document.getElementById('config-editor');

			if (enabled) {
				toggle.setAttribute('aria-checked', 'true');
				toggle.classList.add('bg-blue-600');
				toggle.classList.remove('bg-gray-200', 'dark:bg-gray-600');
				dot.classList.add('translate-x-5');
				dot.classList.remove('translate-x-0');
				editor.classList.add('word-wrap');
			} else {
				toggle.setAttribute('aria-checked', 'false');
				toggle.classList.remove('bg-blue-600');
				toggle.classList.add('bg-gray-200', 'dark:bg-gray-600');
				dot.classList.remove('translate-x-5');
				dot.classList.add('translate-x-0');
				editor.classList.remove('word-wrap');
			}
		}

		// Load persisted preferences
		function loadPreferences() {
			const savedFontSize = localStorage.getItem(PREF_FONT_SIZE);
			const savedWordWrap = localStorage.getItem(PREF_WORD_WRAP);

			if (savedFontSize) {
				const slider = document.getElementById('font-size-slider');
				const editor = document.getElementById('config-editor');
				const lineNumbers = document.getElementById('line-numbers');
				const sizeDisplay = document.getElementById('font-size-value');
				slider.value = savedFontSize;
				editor.style.fontSize = savedFontSize + 'px';
				if (lineNumbers) lineNumbers.style.fontSize = savedFontSize + 'px';
				sizeDisplay.textContent = savedFontSize + 'px';
			}

			if (savedWordWrap === 'true') {
				setWordWrapState(true);
			}
		}

		// Show marker error dialog
		function showMarkerError(message) {
			const modal = document.getElementById('marker-error-modal');
			const text = document.getElementById('marker-error-text');
			text.textContent = message;
			modal.classList.remove('hidden');
			setTimeout(() => document.getElementById('close-marker-error-btn')?.focus(), 50);
		}

		// Hide marker error dialog
		function hideMarkerError() {
			document.getElementById('marker-error-modal').classList.add('hidden');
		}

		// Show validation result modal (focus close button)
		function showValidationModal(isSuccess, title, message) {
			const modal = document.getElementById('validation-modal');
			const header = document.getElementById('validation-modal-header');
			const titleEl = document.getElementById('validation-modal-title');
			const text = document.getElementById('validation-modal-text');
			const iconSuccess = document.getElementById('validation-icon-success');
			const iconError = document.getElementById('validation-icon-error');

			titleEl.textContent = title;
			text.textContent = message;
			text.className = 'font-mono text-sm whitespace-pre-wrap bg-gray-100 dark:bg-slate-700 rounded-lg p-4 max-h-96 overflow-auto ' +
				(isSuccess ? 'text-green-700 dark:text-green-400' : 'text-red-700 dark:text-red-400');

			if (isSuccess) {
				header.className = 'bg-green-600 dark:bg-green-700 rounded-t-lg px-6 py-4 flex items-center gap-3';
				iconSuccess.classList.remove('hidden');
				iconError.classList.add('hidden');
			} else {
				header.className = 'bg-red-600 dark:bg-red-700 rounded-t-lg px-6 py-4 flex items-center gap-3';
				iconSuccess.classList.add('hidden');
				iconError.classList.remove('hidden');
			}

			modal.classList.remove('hidden');
			setTimeout(() => document.getElementById('close-validation-modal-btn')?.focus(), 50);
		}

		// Hide validation modal
		function hideValidationModal() {
			document.getElementById('validation-modal').classList.add('hidden');
		}

		// Parse config and extract auto-generated sections
		function parseConfig(content) {
			const lines = content.split('\n');
			const editableLines = [];
			const sections = [];
			let inAutoGen = false;
			let currentSection = [];
			let sectionStartIdx = -1;

			for (let i = 0; i < lines.length; i++) {
				const line = lines[i];

				if (AUTO_GEN_START.test(line)) {
					inAutoGen = true;
					sectionStartIdx = editableLines.length;
					currentSection = [line];
					// Add a placeholder marker
					editableLines.push(`# [AUTO-GENERATED SECTION ${sections.length + 1} HIDDEN - DO NOT REMOVE THIS LINE]`);
				} else if (AUTO_GEN_END.test(line)) {
					currentSection.push(line);
					sections.push({
						placeholder: `# [AUTO-GENERATED SECTION ${sections.length + 1} HIDDEN - DO NOT REMOVE THIS LINE]`,
						content: currentSection.join('\n'),
						index: sectionStartIdx
					});
					inAutoGen = false;
					currentSection = [];
				} else if (inAutoGen) {
					currentSection.push(line);
				} else {
					editableLines.push(line);
				}
			}

			return {
				editable: editableLines.join('\n'),
				sections: sections
			};
		}

		// Merge auto-generated sections back into edited content
		function mergeConfig(editedContent) {
			let result = editedContent;

			// Replace placeholders with original auto-gen sections
			for (const section of autoGenSections) {
				result = result.replace(section.placeholder, section.content);
			}

			return result;
		}

		function escapeHtml(text) {
			const div = document.createElement('div');
			div.textContent = text;
			return div.innerHTML;
		}

		function highlightLine(line) {
			// Check for auto-gen placeholder
			if (line.includes('[AUTO-GENERATED SECTION') && line.includes('HIDDEN')) {
				return `<span class="cfg-autogen-marker">${escapeHtml(line)}</span>`;
			}

			// Empty line
			if (!line.trim()) {
				return '';
			}

			// Comment line
			if (line.trim().startsWith('#')) {
				return `<span class="cfg-comment">${escapeHtml(line)}</span>`;
			}

			const trimmed = line.trim();
			const indent = line.match(/^(\s*)/)[1];

			// Section headers without names (global, defaults)
			if (/^(global|defaults)\s*$/.test(trimmed)) {
				return `${indent}<span class="cfg-section">${trimmed}</span>`;
			}

			// Section headers with names (frontend, backend, listen, etc.)
			const sectionMatch = trimmed.match(/^(frontend|backend|listen|resolvers|mailers|peers|cache)\s+(\S+)(.*)$/);
			if (sectionMatch) {
				const [, section, name, rest] = sectionMatch;
				const nameClass = section === 'frontend' ? 'cfg-frontend-name' :
				                  section === 'backend' ? 'cfg-backend-name' : 'cfg-value';
				return `${indent}<span class="cfg-section">${escapeHtml(section)}</span> <span class="${nameClass}">${escapeHtml(name)}</span>${escapeHtml(rest)}`;
			}

			// bind directive
			const bindMatch = trimmed.match(/^(bind)\s+(.+)$/);
			if (bindMatch) {
				return `${indent}<span class="cfg-bind">${escapeHtml(bindMatch[1])}</span> <span class="cfg-ip">${escapeHtml(bindMatch[2])}</span>`;
			}

			// server directive
			const serverMatch = trimmed.match(/^(server)\s+(\S+)\s+(.+)$/);
			if (serverMatch) {
				return `${indent}<span class="cfg-keyword">${escapeHtml(serverMatch[1])}</span> <span class="cfg-server-name">${escapeHtml(serverMatch[2])}</span> ${escapeHtml(serverMatch[3])}`;
			}

			// use_backend directive
			const useBackendMatch = trimmed.match(/^(use_backend)\s+(\S+)(.*)$/);
			if (useBackendMatch) {
				return `${indent}<span class="cfg-use-backend">${escapeHtml(useBackendMatch[1])}</span> <span class="cfg-backend-name">${escapeHtml(useBackendMatch[2])}</span>${escapeHtml(useBackendMatch[3])}`;
			}

			// default_backend directive
			const defaultBackendMatch = trimmed.match(/^(default_backend)\s+(\S+)$/);
			if (defaultBackendMatch) {
				return `${indent}<span class="cfg-keyword">${escapeHtml(defaultBackendMatch[1])}</span> <span class="cfg-backend-name">${escapeHtml(defaultBackendMatch[2])}</span>`;
			}

			// acl directive
			const aclMatch = trimmed.match(/^(acl)\s+(\S+)\s+(.+)$/);
			if (aclMatch) {
				return `${indent}<span class="cfg-keyword">${escapeHtml(aclMatch[1])}</span> <span class="cfg-acl-name">${escapeHtml(aclMatch[2])}</span> ${escapeHtml(aclMatch[3])}`;
			}

			// option directive
			const optionMatch = trimmed.match(/^(option)\s+(.+)$/);
			if (optionMatch) {
				return `${indent}<span class="cfg-option">${escapeHtml(optionMatch[1])}</span> <span class="cfg-directive">${escapeHtml(optionMatch[2])}</span>`;
			}

			// Generic key-value pattern: any word (possibly with dots/hyphens) followed by space and value
			// This handles: log, mode, timeout, maxconn, tune.*, ssl-*, etc.
			const kvMatch = trimmed.match(/^([a-zA-Z][a-zA-Z0-9._-]*)\s+(.+)$/);
			if (kvMatch) {
				const key = kvMatch[1];
				const value = kvMatch[2];

				// Colorize the value based on type
				let coloredValue = escapeHtml(value);

				// Time values (e.g., 30s, 1m, 1h) - only standalone
				coloredValue = coloredValue.replace(/^(\d+)(ms|s|m|h|d)$/g, '<span class="cfg-time">$1$2</span>');

				// Pure numbers only (not part of a larger token)
				coloredValue = coloredValue.replace(/^(\d+)$/g, '<span class="cfg-number">$1</span>');

				// IP:port patterns
				coloredValue = coloredValue.replace(/\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})(:\d+)?\b/g, '<span class="cfg-ip">$1$2</span>');

				return `${indent}<span class="cfg-keyword">${escapeHtml(key)}</span> ${coloredValue}`;
			}

			// Single keyword on a line (e.g., daemon)
			if (/^[a-zA-Z][a-zA-Z0-9._-]*$/.test(trimmed)) {
				return `${indent}<span class="cfg-keyword">${escapeHtml(trimmed)}</span>`;
			}

			// Fallback - just escape
			return escapeHtml(line);
		}

		function displayConfig(content) {
			const editor = document.getElementById('config-editor');

			// Store original full config
			originalFullConfig = content;

			// Parse and separate auto-gen sections
			const parsed = parseConfig(content);
			autoGenSections = parsed.sections;

			// Apply syntax highlighting - wrap each line in a div for height measurement
			const lines = parsed.editable.split('\n');
			const highlightedLines = lines.map((line, idx) => {
				const highlighted = highlightLine(line);
				// Wrap in a div with data-line attribute - use &nbsp; for empty lines to maintain height
				const content = highlighted || '&nbsp;';
				return '<div class="editor-line" data-line="' + (idx + 1) + '">' + content + '</div>';
			});
			editor.innerHTML = highlightedLines.join('');
		}

		function getEditorContent() {
			const editor = document.getElementById('config-editor');
			// Get text content, preserving line structure
			// Replace non-breaking spaces (from &nbsp; used for empty lines) with regular spaces
			// and convert \u00A0 to regular space to avoid HAProxy parsing issues
			let content = editor.innerText;
			// Replace non-breaking space (U+00A0) with regular space
			content = content.replace(/\u00A0/g, ' ');
			// Trim trailing spaces from each line (but preserve leading indentation)
			content = content.split('\n').map(line => line.trimEnd()).join('\n');
			return content;
		}

		function getFullConfig() {
			const editedContent = getEditorContent();
			return mergeConfig(editedContent);
		}

		// Validate that auto-gen section markers haven't been tampered with
		function validateAutoGenMarkers() {
			const content = getEditorContent();
			const expectedMarkers = autoGenSections.map((s, i) =>
				`# [AUTO-GENERATED SECTION ${i + 1} HIDDEN - DO NOT REMOVE THIS LINE]`
			);

			const missingMarkers = [];
			for (const marker of expectedMarkers) {
				if (!content.includes(marker)) {
					missingMarkers.push(marker);
				}
			}

			if (missingMarkers.length > 0) {
				return {
					valid: false,
					message: `Whoa there, rogue sysadmin! You've tampered with the sacred auto-generated section markers.

The HAProxy config gods are not pleased. These markers are the only thing standing between your beautiful manual config and the auto-generated chaos that makes everything work.

Missing or modified markers:
${missingMarkers.map(m => '  • ' + m).join('\n')}

Your penance:
  1. Reload the page (Ctrl+R or Cmd+R)
  2. Contemplate your life choices
  3. Try again without touching those lines

May your configs be valid and your uptime eternal. 🙏`
				};
			}

			return { valid: true };
		}

		// Copy full config to clipboard
		function copyConfig() {
			const copyIcon = document.getElementById('copy-icon');
			const content = getFullConfig();

			navigator.clipboard.writeText(content).then(() => {
				copyIcon.innerHTML = '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>';
				if (window.showToast) {
					window.showToast('Configuration copied to clipboard', 'success', 2000);
				}
				setTimeout(() => {
					copyIcon.innerHTML = '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path>';
				}, 2000);
			}).catch(err => {
				console.error('Failed to copy:', err);
				if (window.showToast) {
					window.showToast('Failed to copy to clipboard', 'error', 4000);
				}
			});
		}

		// Download full config
		function downloadConfig() {
			const content = getFullConfig();
			const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
			const filename = `haproxy-${boxID}-${timestamp}.cfg`;

			const blob = new Blob([content], { type: 'text/plain' });
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = filename;
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);
		}

		async function validateConfig() {
			// First, check that auto-gen markers are intact
			const markerCheck = validateAutoGenMarkers();
			if (!markerCheck.valid) {
				// Exit fullscreen if needed so user can see the dialog
				if (fullscreenState > 0) {
					if (fullscreenState === 2) {
						document.exitFullscreen().catch(() => {});
					} else {
						exitBrowserFullscreen();
					}
				}
				showMarkerError(markerCheck.message);
				return;
			}

			const content = getFullConfig();

			try {
				const response = await fetch(`/api/${boxID}/haproxy/config/validate`, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ content: content, dry_run: true })
				});

				const data = await response.json();

				if (data.success) {
					// Show success toast and modal with details
					if (window.showToast) {
						window.showToast('Configuration is valid', 'success', 3000);
					}
					if (data.validation_output) {
						showValidationModal(true, 'Validation Successful', data.validation_output);
					}
				} else {
					// Show error toast and modal with details
					if (window.showToast) {
						window.showToast('Configuration is invalid', 'error', 4000);
					}
					showValidationModal(false, 'Validation Failed', data.validation_output || data.message);
				}
			} catch (error) {
				console.error('Validation failed:', error);
				if (window.showToast) {
					window.showToast('Validation request failed: ' + error.message, 'error', 5000);
				}
			}
		}

		// Save reason dialog state
		let pendingSaveContent = null;

		function showSaveReasonDialog() {
			const modal = document.getElementById('save-reason-modal');
			const input = document.getElementById('save-reason-input');
			input.value = '';
			modal.classList.remove('hidden');
			input.focus();
		}

		function cancelSaveReason() {
			document.getElementById('save-reason-modal').classList.add('hidden');
			pendingSaveContent = null;
		}

		async function confirmSaveReason() {
			const input = document.getElementById('save-reason-input');
			const reason = input.value.trim() || 'Manual edit via web UI';
			document.getElementById('save-reason-modal').classList.add('hidden');

			if (pendingSaveContent) {
				await doSaveConfig(pendingSaveContent, reason);
				pendingSaveContent = null;
			}
		}

		async function saveConfig() {
			// First, check that auto-gen markers are intact
			const markerCheck = validateAutoGenMarkers();
			if (!markerCheck.valid) {
				// Exit fullscreen if needed so user can see the dialog
				if (fullscreenState > 0) {
					if (fullscreenState === 2) {
						document.exitFullscreen().catch(() => {});
					} else {
						exitBrowserFullscreen();
					}
				}
				showMarkerError(markerCheck.message);
				return;
			}

			pendingSaveContent = getFullConfig();
			showSaveReasonDialog();
		}

		async function doSaveConfig(content, reason) {
			try {
				const apiUrl = `/api/${boxID}/haproxy/config`;
				const response = await fetch(apiUrl, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({
						content: content,
						expected_sha: currentSHA,
						backup_reason: reason,
						dry_run: false
					})
				});

				const data = await response.json();

				if (data.success) {
					currentSHA = data.new_sha256 || currentSHA;
					if (window.showToast) {
						window.showToast('Configuration saved and applied successfully', 'success', 3000);
					}
				} else {
					// Show error as toast
					if (window.showToast) {
						window.showToast('Save failed', 'error', 4000);
					}
					// Show details in modal
					const errorMsg = data.validation_output || data.message || 'Unknown error';
					showValidationModal(false, 'Save Failed', errorMsg);
				}
			} catch (error) {
				console.error('Save failed:', error);
				if (window.showToast) {
					window.showToast('Failed to save configuration: ' + error.message, 'error', 5000);
				}
			}
		}

		async function showBackups() {
			const modal = document.getElementById('backups-modal');
			const list = document.getElementById('backups-list');
			modal.classList.remove('hidden');
			setTimeout(() => document.getElementById('close-backups-btn')?.focus(), 50);

			try {
				const response = await fetch(`/api/${boxID}/haproxy/config/backups`);
				const data = await response.json();

				if (data.backups && data.backups.length > 0) {
					list.innerHTML = data.backups.map(b => `
						<div class="flex items-center justify-between p-3 bg-gray-50 dark:bg-slate-700 rounded-lg">
							<div>
								<div class="font-medium text-gray-900 dark:text-gray-100">${escapeHtml(b.reason || 'Unknown')}</div>
								<div class="text-sm text-gray-500 dark:text-gray-400">${new Date(b.created_at).toLocaleString()}</div>
							</div>
							<button onclick="restoreBackup('${escapeHtml(b.id)}')" class="px-3 py-1 bg-blue-600 text-white rounded hover:bg-blue-700 text-sm">
								Restore
							</button>
						</div>
					`).join('');
				} else {
					list.innerHTML = '<p class="text-gray-500 dark:text-gray-400">No backups available.</p>';
				}
			} catch (error) {
				list.innerHTML = '<p class="text-red-500">Failed to load backups.</p>';
			}
		}

		function hideBackups() {
			document.getElementById('backups-modal').classList.add('hidden');
		}

		async function restoreBackup(backupID) {
			const confirmed = await showConfirmDialog({
				title: 'Restore Backup',
				message: 'Are you sure you want to restore from this backup? The current configuration will be replaced.',
				confirmText: 'Restore',
				type: 'danger'
			});

			if (!confirmed) {
				return;
			}

			try {
				const response = await fetch(`/api/${boxID}/haproxy/config/restore`, {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ backup_id: backupID })
				});

				const data = await response.json();

				if (data.success) {
					if (window.showToast) {
						window.showToast('Configuration restored successfully', 'success', 3000);
					}
					setTimeout(() => window.location.reload(), 1000);
				} else {
					if (window.showToast) {
						window.showToast('Restore failed: ' + data.message, 'error', 5000);
					} else {
						await showAlertDialog({
							title: 'Restore Failed',
							message: 'Restore failed: ' + data.message,
							type: 'error'
						});
					}
				}
			} catch (error) {
				console.error('Restore failed:', error);
				if (window.showToast) {
					window.showToast('Restore failed', 'error', 5000);
				}
			}
		}

		// Fullscreen functionality - matching logs page pattern
		function toggleFullscreen() {
			const editorCard = document.getElementById('editor-card');

			if (fullscreenState === 0) {
				enterBrowserFullscreen();
			} else if (fullscreenState === 1) {
				editorCard.requestFullscreen().catch(err => {
					console.error('Failed to enter fullscreen:', err);
				});
			} else {
				document.exitFullscreen().catch(err => {
					console.error('Failed to exit fullscreen:', err);
				});
			}
		}

		function enterBrowserFullscreen() {
			const editorCard = document.getElementById('editor-card');
			const navElement = document.querySelector('nav');
			const mainElement = document.querySelector('main');
			const headerElement = document.querySelector('header');

			fullscreenState = 1;
			editorCard.classList.add('fullscreen-mode');
			if (navElement) navElement.classList.add('hidden');
			if (headerElement) headerElement.classList.add('hidden');
			if (mainElement) {
				mainElement.style.padding = '0';
				mainElement.style.maxWidth = 'none';
			}
			document.body.style.overflow = 'hidden';
			updateFullscreenIcons();
		}

		function exitBrowserFullscreen() {
			const editorCard = document.getElementById('editor-card');
			const navElement = document.querySelector('nav');
			const mainElement = document.querySelector('main');
			const headerElement = document.querySelector('header');

			fullscreenState = 0;
			editorCard.classList.remove('fullscreen-mode');
			if (navElement) navElement.classList.remove('hidden');
			if (headerElement) headerElement.classList.remove('hidden');
			if (mainElement) {
				mainElement.style.padding = '';
				mainElement.style.maxWidth = '';
			}
			document.body.style.overflow = '';
			updateFullscreenIcons();
		}

		function updateFullscreenIcons() {
			const iconNormal = document.getElementById('fullscreen-icon-normal');
			const iconBrowser = document.getElementById('fullscreen-icon-browser');
			const iconTrue = document.getElementById('fullscreen-icon-true');
			const button = document.getElementById('fullscreen-button');

			iconNormal.classList.add('hidden');
			iconBrowser.classList.add('hidden');
			iconTrue.classList.add('hidden');

			if (fullscreenState === 0) {
				iconNormal.classList.remove('hidden');
				button.title = 'Browser Fullscreen';
			} else if (fullscreenState === 1) {
				iconBrowser.classList.remove('hidden');
				button.title = 'True Fullscreen (hide browser)';
			} else {
				iconTrue.classList.remove('hidden');
				button.title = 'Exit Fullscreen';
			}
		}

		document.addEventListener('fullscreenchange', function() {
			const editorCard = document.getElementById('editor-card');
			const headerElement = document.querySelector('header');

			if (document.fullscreenElement) {
				fullscreenState = 2;
				editorCard.classList.add('fullscreen-mode');
				if (headerElement) headerElement.classList.add('hidden');
			} else {
				fullscreenState = 0;
				editorCard.classList.remove('fullscreen-mode');
				if (headerElement) headerElement.classList.remove('hidden');
				const navElement = document.querySelector('nav');
				const mainElement = document.querySelector('main');
				if (navElement) navElement.classList.remove('hidden');
				if (mainElement) {
					mainElement.style.padding = '';
					mainElement.style.maxWidth = '';
				}
				document.body.style.overflow = '';
			}
			updateFullscreenIcons();
		});

		document.addEventListener('keydown', function(e) {
			if (e.key === 'Escape' && fullscreenState === 1) {
				exitBrowserFullscreen();
			}
		});

		// Calculate and set editor height to fill available viewport
		function updateEditorHeight() {
			if (fullscreenState > 0) return; // Don't adjust in fullscreen mode

			const editorWrapper = document.getElementById('editor-wrapper');
			const editorCard = document.getElementById('editor-card');
			const container = document.getElementById('config-page-container');
			if (!editorWrapper || !editorCard || !container) return;

			// Get the editor card's position from top of viewport
			const cardRect = editorCard.getBoundingClientRect();
			const cardTop = cardRect.top;

			// Check if there's a "Recent Changes" section below the editor
			const recentChanges = container.querySelector('.mt-6');
			let reservedSpace = 0;
			if (recentChanges) {
				// Reserve space for the recent changes section plus its margin
				reservedSpace = recentChanges.offsetHeight + 24; // 24px = mt-6 margin
			}

			// Calculate available height: viewport - card top - reserved space for content below
			const availableHeight = window.innerHeight - cardTop - reservedSpace;

			// Set minimum height of 300px
			const height = Math.max(300, availableHeight);
			editorWrapper.style.height = height + 'px';
			editorCard.style.height = height + 'px';
		}

		// Initialize
		document.addEventListener('DOMContentLoaded', function() {
			setupPageHeader();

			// Load persisted font size and word wrap preferences
			loadPreferences();

			// Load initial config from hidden textarea
			const configSource = document.getElementById('config-content-source');
			if (configSource) {
				displayConfig(configSource.value);
			}

			// Initialize line numbers
			updateLineNumbers();

			// Set up editor event listeners for line numbers
			const editor = document.getElementById('config-editor');
			if (editor) {
				// Update line numbers on content change
				editor.addEventListener('input', updateLineNumbers);
				editor.addEventListener('paste', () => setTimeout(updateLineNumbers, 0));
			}

			// Set initial editor height
			updateEditorHeight();

			// Enter key in save reason dialog should confirm
			const saveReasonInput = document.getElementById('save-reason-input');
			if (saveReasonInput) {
				saveReasonInput.addEventListener('keydown', function(e) {
					if (e.key === 'Enter') {
						e.preventDefault();
						confirmSaveReason();
					} else if (e.key === 'Escape') {
						e.preventDefault();
						cancelSaveReason();
					}
				});
			}

			// Escape key closes any open modal
			document.addEventListener('keydown', function(e) {
				if (e.key !== 'Escape') return;
				const modals = [
					{ id: 'snippets-modal',      hide: hideSnippets },
					{ id: 'backups-modal',        hide: hideBackups },
					{ id: 'validation-modal',     hide: hideValidationModal },
					{ id: 'marker-error-modal',   hide: hideMarkerError },
					{ id: 'save-reason-modal',    hide: cancelSaveReason },
				];
				for (const m of modals) {
					const el = document.getElementById(m.id);
					if (el && !el.classList.contains('hidden')) {
						m.hide();
						break;
					}
				}
			});
		});

		// Update height on window resize
		window.addEventListener('resize', function() {
			updateEditorHeight();
			// Re-measure line heights on resize (affects word wrap)
			setTimeout(updateLineNumbers, 10);
		});

		// Setup event listeners for buttons (replacing inline onclick handlers)
		const snippetsBtn = document.getElementById('snippets-btn');
		if (snippetsBtn) {
			snippetsBtn.addEventListener('click', showSnippets);
		}

		const backupBtn = document.getElementById('backup-btn');
		if (backupBtn) {
			backupBtn.addEventListener('click', showBackups);
		}

		const validateBtn = document.getElementById('validate-btn');
		if (validateBtn) {
			validateBtn.addEventListener('click', validateConfig);
		}

		const saveBtn = document.getElementById('save-btn');
		if (saveBtn) {
			saveBtn.addEventListener('click', saveConfig);
		}

		const fontSizeSlider = document.getElementById('font-size-slider');
		if (fontSizeSlider) {
			fontSizeSlider.addEventListener('input', (e) => updateFontSize(e.target.value));
		}

		const wordWrapToggle = document.getElementById('word-wrap-toggle');
		if (wordWrapToggle) {
			wordWrapToggle.addEventListener('click', toggleWordWrap);
		}

		const copyConfigBtn = document.getElementById('copy-config-btn');
		if (copyConfigBtn) {
			copyConfigBtn.addEventListener('click', copyConfig);
		}

		const downloadConfigBtn = document.getElementById('download-config-btn');
		if (downloadConfigBtn) {
			downloadConfigBtn.addEventListener('click', downloadConfig);
		}

		const fullscreenButton = document.getElementById('fullscreen-button');
		if (fullscreenButton) {
			fullscreenButton.addEventListener('click', toggleFullscreen);
		}

		// Modal event listeners
		const closeBackupsBtn = document.getElementById('close-backups-btn');
		if (closeBackupsBtn) {
			closeBackupsBtn.addEventListener('click', hideBackups);
		}

		const backupsBackdrop = document.getElementById('backups-modal-backdrop');
		if (backupsBackdrop) {
			backupsBackdrop.addEventListener('click', hideBackups);
		}

		const reloadPageBtn = document.getElementById('reload-page-btn');
		if (reloadPageBtn) {
			reloadPageBtn.addEventListener('click', () => window.location.reload());
		}

		const closeMarkerErrorBtn = document.getElementById('close-marker-error-btn');
		if (closeMarkerErrorBtn) {
			closeMarkerErrorBtn.addEventListener('click', hideMarkerError);
		}

		const cancelSaveReasonBtn = document.getElementById('cancel-save-reason-btn');
		if (cancelSaveReasonBtn) {
			cancelSaveReasonBtn.addEventListener('click', cancelSaveReason);
		}

		const confirmSaveReasonBtn = document.getElementById('confirm-save-reason-btn');
		if (confirmSaveReasonBtn) {
			confirmSaveReasonBtn.addEventListener('click', confirmSaveReason);
		}

		const saveReasonBackdrop = document.getElementById('save-reason-backdrop');
		if (saveReasonBackdrop) {
			saveReasonBackdrop.addEventListener('click', cancelSaveReason);
		}

		const closeValidationModalBtn = document.getElementById('close-validation-modal-btn');
		if (closeValidationModalBtn) {
			closeValidationModalBtn.addEventListener('click', hideValidationModal);
		}

		const validationBackdrop = document.getElementById('validation-modal-backdrop');
		if (validationBackdrop) {
			validationBackdrop.addEventListener('click', hideValidationModal);
		}

		const cancelSnippetsBtn = document.getElementById('cancel-snippets-btn');
		if (cancelSnippetsBtn) {
			cancelSnippetsBtn.addEventListener('click', hideSnippets);
		}

		const snippetsBackdrop = document.getElementById('snippets-modal-backdrop');
		if (snippetsBackdrop) {
			snippetsBackdrop.addEventListener('click', hideSnippets);
		}

		const insertSnippetBtn = document.getElementById('insert-snippet-btn');
		if (insertSnippetBtn) {
			insertSnippetBtn.addEventListener('click', insertSelectedSnippet);
		}

		const snippetSearch = document.getElementById('snippet-search');
		if (snippetSearch) {
			snippetSearch.addEventListener('input', (e) => filterSnippets(e.target.value));
		}
	});
