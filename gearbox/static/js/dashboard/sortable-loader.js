// Sortable.js Module Loader
// Loads SortableJS with Swap plugin from CDN and makes it available globally

// Import SortableJS with Swap plugin
import Sortable, { Swap } from 'https://cdn.jsdelivr.net/npm/sortablejs@1.15.0/modular/sortable.core.esm.js';

// Mount the Swap plugin
Sortable.mount(new Swap());

// Make Sortable available globally
window.Sortable = Sortable;

// Signal that Sortable is ready
window.sortableReady = true;
window.dispatchEvent(new Event('sortable-loaded'));
console.log('Sortable with Swap plugin loaded and ready');
