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
