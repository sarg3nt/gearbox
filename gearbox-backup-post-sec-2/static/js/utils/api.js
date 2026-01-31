/**
 * API Client Utilities
 * Common fetch wrappers with error handling
 */

/**
 * Base API client configuration
 */
const API_CONFIG = {
    baseURL: '/api/v1',
    timeout: 30000,
    headers: {
        'Content-Type': 'application/json'
    }
};

/**
 * Handle API response
 * @param {Response} response - Fetch response object
 * @returns {Promise} Response data or error
 */
async function handleResponse(response) {
    const contentType = response.headers.get('content-type');
    const isJSON = contentType && contentType.includes('application/json');

    if (!response.ok) {
        const error = isJSON ? await response.json() : { error: response.statusText };
        throw new Error(error.error || error.message || 'API request failed');
    }

    return isJSON ? response.json() : response.text();
}

/**
 * GET request
 * @param {string} endpoint - API endpoint
 * @param {Object} options - Fetch options
 * @returns {Promise} Response data
 */
async function get(endpoint, options = {}) {
    try {
        const url = endpoint.startsWith('http') ? endpoint : `${API_CONFIG.baseURL}${endpoint}`;
        const response = await fetch(url, {
            method: 'GET',
            headers: { ...API_CONFIG.headers, ...options.headers },
            ...options
        });
        return await handleResponse(response);
    } catch (error) {
        console.error(`GET ${endpoint} failed:`, error);
        throw error;
    }
}

/**
 * POST request
 * @param {string} endpoint - API endpoint
 * @param {Object} data - Request body data
 * @param {Object} options - Fetch options
 * @returns {Promise} Response data
 */
async function post(endpoint, data = {}, options = {}) {
    try {
        const url = endpoint.startsWith('http') ? endpoint : `${API_CONFIG.baseURL}${endpoint}`;
        const response = await fetch(url, {
            method: 'POST',
            headers: { ...API_CONFIG.headers, ...options.headers },
            body: JSON.stringify(data),
            ...options
        });
        return await handleResponse(response);
    } catch (error) {
        console.error(`POST ${endpoint} failed:`, error);
        throw error;
    }
}

/**
 * PUT request
 * @param {string} endpoint - API endpoint
 * @param {Object} data - Request body data
 * @param {Object} options - Fetch options
 * @returns {Promise} Response data
 */
async function put(endpoint, data = {}, options = {}) {
    try {
        const url = endpoint.startsWith('http') ? endpoint : `${API_CONFIG.baseURL}${endpoint}`;
        const response = await fetch(url, {
            method: 'PUT',
            headers: { ...API_CONFIG.headers, ...options.headers },
            body: JSON.stringify(data),
            ...options
        });
        return await handleResponse(response);
    } catch (error) {
        console.error(`PUT ${endpoint} failed:`, error);
        throw error;
    }
}

/**
 * DELETE request
 * @param {string} endpoint - API endpoint
 * @param {Object} options - Fetch options
 * @returns {Promise} Response data
 */
async function del(endpoint, options = {}) {
    try {
        const url = endpoint.startsWith('http') ? endpoint : `${API_CONFIG.baseURL}${endpoint}`;
        const response = await fetch(url, {
            method: 'DELETE',
            headers: { ...API_CONFIG.headers, ...options.headers },
            ...options
        });
        return await handleResponse(response);
    } catch (error) {
        console.error(`DELETE ${endpoint} failed:`, error);
        throw error;
    }
}

/**
 * Create API client with specific server context
 * @param {string} serverID - Server ID for API requests
 * @returns {Object} API client with server-specific methods
 */
function createServerAPI(serverID) {
    const addServerParam = (endpoint) => {
        const separator = endpoint.includes('?') ? '&' : '?';
        return `${endpoint}${separator}server_id=${serverID}`;
    };

    return {
        get: (endpoint, options) => get(addServerParam(endpoint), options),
        post: (endpoint, data, options) => post(addServerParam(endpoint), data, options),
        put: (endpoint, data, options) => put(addServerParam(endpoint), data, options),
        delete: (endpoint, options) => del(addServerParam(endpoint), options)
    };
}

// Export for use in modules
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        get,
        post,
        put,
        del: del,
        createServerAPI
    };
}
