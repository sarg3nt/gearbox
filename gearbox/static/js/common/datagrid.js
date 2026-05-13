/**
 * createDataGrid — UniFi-style Tabulator wrapper.
 *
 * Wraps `new Tabulator(...)` with the gearbox house style:
 *   - flat, accent-on-active-sort header (CSS in components/datagrid.css)
 *   - hover-only sort arrows
 *   - columns are draggable to reorder
 *   - per-column header filters are NOT used; instead an external search
 *     input is wired as a universal filter across every searchable field
 *
 * Usage:
 *
 *     const grid = createDataGrid('#my-table', {
 *         columns: [...],                  // standard Tabulator column defs
 *         data:    [...],
 *         searchInput:  '#my-search',      // optional <input> selector/element
 *         searchFields: ['name', 'desc'],  // optional (default: every column with a `field`)
 *         // ...any other Tabulator options pass through
 *     });
 *
 *     grid.setViewFilters([{field: 'status', type: '=', value: 'active'}]);
 *     grid.clearViewFilters();
 *
 * The returned object is the raw Tabulator instance with two extra methods
 * (`setViewFilters` / `clearViewFilters`) that compose with the search filter.
 * Do NOT call `table.setFilter()` directly when using a search input — it
 * will wipe the search. Use `setViewFilters` instead.
 */
(function () {
	'use strict';

	function createDataGrid(target, opts) {
		opts = opts || {};
		const {
			columns,
			data,
			searchInput,
			searchFields,
			cssClass: extraCssClass,
			...rest
		} = opts;

		const cssClass = (extraCssClass ? extraCssClass + ' ' : '') + 'datagrid-unifi';

		const table = new Tabulator(target, Object.assign({
			data: data || [],
			columns: columns,
			layout: 'fitColumns',
			sortMode: 'local',
			filterMode: 'local',
			movableColumns: true,
		}, rest, { cssClass: cssClass }));

		// Tabulator's `cssClass` option records the class but does not always
		// add it to the host element we passed in (depends on whether the
		// element starts empty or already had Tabulator markup). Add it
		// directly so our CSS selectors match unconditionally.
		const hostEl = typeof target === 'string' ? document.querySelector(target) : target;
		if (hostEl) {
			cssClass.split(/\s+/).filter(Boolean).forEach(c => hostEl.classList.add(c));
		}

		// Filter state: view filters (e.g. "Updates / Held / All" dropdown) and
		// search text. We compose both into a single setFilter call so they
		// don't clobber each other.
		const state = {
			viewFilters: [],
			searchQuery: '',
			searchFields: searchFields || (columns || [])
				.map(c => c.field)
				.filter(f => typeof f === 'string' && f.length > 0),
		};

		function matchObjectFilter(f, row) {
			const v = row[f.field];
			const target = f.value;
			switch (f.type) {
				case '=': return v === target;
				case '!=': return v !== target;
				case '<': return v < target;
				case '>': return v > target;
				case '<=': return v <= target;
				case '>=': return v >= target;
				case 'like': return v != null && String(v).toLowerCase().indexOf(String(target).toLowerCase()) !== -1;
				case 'in': return Array.isArray(target) && target.indexOf(v) !== -1;
				default: return v === target;
			}
		}

		function applyFilters() {
			const hasSearch = !!state.searchQuery;
			const viewCount = state.viewFilters.length;

			if (!hasSearch && viewCount === 0) {
				table.clearFilter(true);
				return;
			}

			if (!hasSearch) {
				// View filters only: use native object filter array — Tabulator
				// handles AND composition.
				table.setFilter(state.viewFilters);
				return;
			}

			// Search is active. Tabulator's setFilter silently ignores function
			// elements inside an array (verified empirically in 6.3.0), so we
			// can't mix native object filters with our search function. Build
			// one composite function that AND-composes view filters + search.
			const q = state.searchQuery;
			const fields = state.searchFields;
			const viewFilters = state.viewFilters;
			table.setFilter(function (row) {
				for (let i = 0; i < viewFilters.length; i++) {
					if (!matchObjectFilter(viewFilters[i], row)) return false;
				}
				for (let i = 0; i < fields.length; i++) {
					const v = row[fields[i]];
					if (v != null && String(v).toLowerCase().indexOf(q) !== -1) {
						return true;
					}
				}
				return false;
			});
		}

		table.setViewFilters = function (filters) {
			if (filters == null) {
				state.viewFilters = [];
			} else if (Array.isArray(filters)) {
				state.viewFilters = filters;
			} else {
				state.viewFilters = [filters];
			}
			applyFilters();
		};

		table.clearViewFilters = function () {
			state.viewFilters = [];
			applyFilters();
		};

		// Wire the search input (if provided).
		if (searchInput) {
			const input = typeof searchInput === 'string'
				? document.querySelector(searchInput)
				: searchInput;
			if (input) {
				const onInput = function () {
					state.searchQuery = (input.value || '').trim().toLowerCase();
					applyFilters();
				};
				input.addEventListener('input', onInput);
				// Pre-fill state from any value already in the box (e.g. browser
				// restored the input from history).
				if (input.value) {
					onInput();
				}
			}
		}

		return table;
	}

	window.createDataGrid = createDataGrid;
})();
