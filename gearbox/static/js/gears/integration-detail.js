	// Move page header content to main header
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

	document.addEventListener('DOMContentLoaded', setupPageHeader);
