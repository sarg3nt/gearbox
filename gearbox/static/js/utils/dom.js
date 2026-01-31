/**
 * DOM Manipulation Utilities
 * Common DOM helper functions used across the application
 */

/**
 * Show an element by removing the 'hidden' class
 * @param {string|Element} element - Element ID or element object
 */
function showElement(element) {
    const el = typeof element === 'string' ? document.getElementById(element) : element;
    if (el) {
        el.classList.remove('hidden');
    }
}

/**
 * Hide an element by adding the 'hidden' class
 * @param {string|Element} element - Element ID or element object
 */
function hideElement(element) {
    const el = typeof element === 'string' ? document.getElementById(element) : element;
    if (el) {
        el.classList.add('hidden');
    }
}

/**
 * Toggle element visibility
 * @param {string|Element} element - Element ID or element object
 */
function toggleElement(element) {
    const el = typeof element === 'string' ? document.getElementById(element) : element;
    if (el) {
        el.classList.toggle('hidden');
    }
}

/**
 * Set element text content safely
 * @param {string|Element} element - Element ID or element object
 * @param {string} text - Text content to set
 */
function setTextContent(element, text) {
    const el = typeof element === 'string' ? document.getElementById(element) : element;
    if (el) {
        el.textContent = text || '-';
    }
}

/**
 * Set element HTML content safely (escapes by default)
 * @param {string|Element} element - Element ID or element object
 * @param {string} html - HTML content to set
 * @param {boolean} escape - Whether to escape HTML (default: true)
 */
function setHTMLContent(element, html, escape = true) {
    const el = typeof element === 'string' ? document.getElementById(element) : element;
    if (el) {
        if (escape) {
            el.textContent = html || '';
        } else {
            el.innerHTML = html || '';
        }
    }
}

/**
 * Add CSS class to element
 * @param {string|Element} element - Element ID or element object
 * @param {string|string[]} classes - Class name(s) to add
 */
function addClass(element, classes) {
    const el = typeof element === 'string' ? document.getElementById(element) : element;
    if (el) {
        const classList = Array.isArray(classes) ? classes : [classes];
        el.classList.add(...classList);
    }
}

/**
 * Remove CSS class from element
 * @param {string|Element} element - Element ID or element object
 * @param {string|string[]} classes - Class name(s) to remove
 */
function removeClass(element, classes) {
    const el = typeof element === 'string' ? document.getElementById(element) : element;
    if (el) {
        const classList = Array.isArray(classes) ? classes : [classes];
        el.classList.remove(...classList);
    }
}

/**
 * Create element with classes and attributes
 * @param {string} tag - HTML tag name
 * @param {Object} options - Options object
 * @param {string|string[]} options.classes - CSS classes
 * @param {Object} options.attributes - HTML attributes
 * @param {string} options.text - Text content
 * @param {string} options.html - HTML content
 * @returns {Element} Created element
 */
function createElement(tag, options = {}) {
    const el = document.createElement(tag);

    if (options.classes) {
        const classes = Array.isArray(options.classes) ? options.classes : [options.classes];
        el.classList.add(...classes);
    }

    if (options.attributes) {
        Object.entries(options.attributes).forEach(([key, value]) => {
            el.setAttribute(key, value);
        });
    }

    if (options.text) {
        el.textContent = options.text;
    }

    if (options.html) {
        el.innerHTML = options.html;
    }

    return el;
}

/**
 * Debounce function execution
 * @param {Function} func - Function to debounce
 * @param {number} wait - Delay in milliseconds
 * @returns {Function} Debounced function
 */
function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
        const later = () => {
            clearTimeout(timeout);
            func(...args);
        };
        clearTimeout(timeout);
        timeout = setTimeout(later, wait);
    };
}

/**
 * Throttle function execution
 * @param {Function} func - Function to throttle
 * @param {number} limit - Minimum time between executions in milliseconds
 * @returns {Function} Throttled function
 */
function throttle(func, limit) {
    let inThrottle;
    return function executedFunction(...args) {
        if (!inThrottle) {
            func(...args);
            inThrottle = true;
            setTimeout(() => inThrottle = false, limit);
        }
    };
}

// Export for use in modules
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        showElement,
        hideElement,
        toggleElement,
        setTextContent,
        setHTMLContent,
        addClass,
        removeClass,
        createElement,
        debounce,
        throttle
    };
}
