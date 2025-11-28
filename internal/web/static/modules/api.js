// Get base path for Home Assistant Ingress support
const getBasePath = () => {
    const path = window.location.pathname;
    // Remove trailing slash and any file name (like index.html)
    const base = path.replace(/\/[^/]*$/, '');
    return base || '';
};

export const basePath = getBasePath();

export function apiUrl(endpoint) {
    return `${basePath}${endpoint}`;
}

// Get WebSocket URL with proper protocol (ws/wss) and path
export function wsUrl(endpoint) {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const host = window.location.host;
    return `${protocol}//${host}${basePath}${endpoint}`;
}
