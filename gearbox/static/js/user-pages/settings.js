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

	document.addEventListener('DOMContentLoaded', function() {
		// Check for pending user requests
		fetch("/api/user/pending-count")
			.then(response => {
				// If not authenticated (redirect response), silently ignore
				if (!response.ok || response.redirected || response.status === 303) {
					return null;
				}
				return response.json();
			})
			.then(data => {
				if (data && data.count > 0) {
					const link = document.querySelector("a[href="/settings/users"]");
					if (link) {
						// Add notification badge
						const badge = document.createElement("span");
						badge.className = "absolute -top-1 -right-1 w-6 h-6 bg-red-500 text-white text-xs font-bold rounded-full flex items-center justify-center";
						badge.textContent = data.count;
						// Find the icon container and add the badge
						const iconContainer = link.querySelector(".w-12.h-12");
						if (iconContainer) {
							iconContainer.classList.add("relative");
							iconContainer.appendChild(badge);
						}
						// Also add a text badge next to the title
						const title = link.querySelector("h2");
						if (title) {
							const textBadge = document.createElement("span");
							textBadge.className = "ml-2 px-2 py-0.5 text-xs bg-red-100 dark:bg-red-900 text-red-800 dark:text-red-200 rounded-full font-medium";
							textBadge.textContent = data.count + " pending";
							title.appendChild(textBadge);
						}
					}
				}
			})
			.catch(() => {
				// Silently ignore errors on public pages
				console.debug("Pending users count not available (not authenticated)");
			});

		setupPageHeader();
	});
