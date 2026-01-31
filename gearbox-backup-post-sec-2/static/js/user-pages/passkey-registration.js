// Check if WebAuthn is supported
function isWebAuthnSupported() {
	return window.PublicKeyCredential !== undefined &&
		typeof window.PublicKeyCredential === 'function';
}

// Convert base64url to ArrayBuffer
function base64urlToArrayBuffer(base64url) {
	const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/');
	const padding = '='.repeat((4 - base64.length % 4) % 4);
	const base64Padded = base64 + padding;
	const binary = atob(base64Padded);
	const bytes = new Uint8Array(binary.length);
	for (let i = 0; i < binary.length; i++) {
		bytes[i] = binary.charCodeAt(i);
	}
	return bytes.buffer;
}

// Convert ArrayBuffer to base64url
function arrayBufferToBase64url(buffer) {
	const bytes = new Uint8Array(buffer);
	let binary = '';
	for (let i = 0; i < bytes.length; i++) {
		binary += String.fromCharCode(bytes[i]);
	}
	const base64 = btoa(binary);
	return base64.replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
}

// Show status message
function showStatus(message, isError) {
	const status = document.getElementById('passkey-status');
	status.textContent = message;
	status.classList.remove('hidden', 'bg-green-100', 'bg-red-100', 'text-green-800', 'text-red-800',
		'dark:bg-green-900/30', 'dark:bg-red-900/30', 'dark:text-green-200', 'dark:text-red-200');
	if (isError) {
		status.classList.add('bg-red-100', 'text-red-800', 'dark:bg-red-900/30', 'dark:text-red-200');
	} else {
		status.classList.add('bg-green-100', 'text-green-800', 'dark:bg-green-900/30', 'dark:text-green-200');
	}
}

// Register a new passkey
async function registerPasskey() {
	const btn = document.getElementById('add-passkey-btn');
	const status = document.getElementById('passkey-status');

	btn.disabled = true;
	btn.textContent = 'Registering...';
	status.classList.add('hidden');

	try {
		// Step 1: Get registration options from server
		const beginResponse = await fetch('/api/passkey/register/begin');
		if (!beginResponse.ok) {
			throw new Error('Failed to start registration');
		}
		const beginData = await beginResponse.json();

		// Convert challenge and user ID from base64url to ArrayBuffer
		const options = beginData.options.publicKey;
		options.challenge = base64urlToArrayBuffer(options.challenge);
		options.user.id = base64urlToArrayBuffer(options.user.id);

		// Convert excludeCredentials if present
		if (options.excludeCredentials) {
			options.excludeCredentials = options.excludeCredentials.map(cred => ({
				...cred,
				id: base64urlToArrayBuffer(cred.id)
			}));
		}

		// Step 2: Create credential with the browser's authenticator
		const credential = await navigator.credentials.create({
			publicKey: options
		});

		// Step 3: Prepare credential for sending to server
		const credentialJSON = {
			id: credential.id,
			rawId: arrayBufferToBase64url(credential.rawId),
			type: credential.type,
			response: {
				attestationObject: arrayBufferToBase64url(credential.response.attestationObject),
				clientDataJSON: arrayBufferToBase64url(credential.response.clientDataJSON)
			}
		};

		// Add transports if available
		if (credential.response.getTransports) {
			credentialJSON.response.transports = credential.response.getTransports();
		}

		// Prompt for passkey name
		const passkeyName = await showPromptDialog({
			title: 'Name Your Passkey',
			message: 'Enter a name for this passkey (e.g., "MacBook Pro", "iPhone"):',
			defaultValue: 'Passkey',
			placeholder: 'My Device'
		});
		if (passkeyName === null) {
			throw new Error('Registration cancelled');
		}

		// Step 4: Send credential to server
		const finishResponse = await fetch('/api/passkey/register/finish', {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify({
				session_id: beginData.session_id,
				name: passkeyName || 'Passkey',
				credential: credentialJSON
			})
		});

		if (!finishResponse.ok) {
			const errorData = await finishResponse.text();
			throw new Error(errorData || 'Failed to complete registration');
		}

		showStatus('Passkey registered successfully! Refreshing...', false);

		// Reload the page to show the new passkey
		setTimeout(() => {
			window.location.reload();
		}, 1500);

	} catch (error) {
		console.error('Passkey registration error:', error);
		showStatus('Failed to register passkey: ' + error.message, true);
		btn.disabled = false;
		btn.textContent = 'Add Passkey';
	}
}

// Initialize on page load
document.addEventListener('DOMContentLoaded', function() {
	// Check for pending user requests
	fetch('/api/user/pending-count')
		.then(response => {
			// If not authenticated (redirect response), silently ignore
			if (!response.ok || response.redirected || response.status === 303) {
				return null;
			}
			return response.json();
		})
		.then(data => {
			if (data && data.count > 0) {
				const link = document.querySelector('a[href="/settings/users"]');
				if (link) {
					// Add notification badge
					const badge = document.createElement('span');
					badge.className = 'absolute -top-1 -right-1 w-6 h-6 bg-red-500 text-white text-xs font-bold rounded-full flex items-center justify-center';
					badge.textContent = data.count;
					// Find the icon container and add the badge
					const iconContainer = link.querySelector('.w-12.h-12');
					if (iconContainer) {
						iconContainer.classList.add('relative');
						iconContainer.appendChild(badge);
					}
					// Also add a text badge next to the title
					const title = link.querySelector('h2');
					if (title) {
						const textBadge = document.createElement('span');
						textBadge.className = 'ml-2 px-2 py-0.5 text-xs bg-red-100 dark:bg-red-900 text-red-800 dark:text-red-200 rounded-full font-medium';
						textBadge.textContent = data.count + ' pending';
						title.appendChild(textBadge);
					}
				}
			}
		})
		.catch(() => {
			// Silently ignore errors on public pages
			console.debug('Pending users count not available (not authenticated)');
		});

	const btn = document.getElementById('add-passkey-btn');
	const supportNote = document.getElementById('passkey-support-note');

	if (!isWebAuthnSupported()) {
		btn.disabled = true;
		btn.title = 'WebAuthn is not supported in this browser';
		supportNote.textContent = 'Passkeys are not supported in this browser.';
		supportNote.classList.add('text-yellow-600', 'dark:text-yellow-400');
		return;
	}

	btn.addEventListener('click', registerPasskey);
});
