export function formatTime(timestamp) {
    if (!timestamp) return '';
    // timestamp: 2025-11-27T23:48:33.876583+09:00
    // returns: 23:48:33.876
    try {
        return timestamp.split('T')[1].split(/[Z+-]/)[0].substring(0, 12);
    } catch (e) {
        return timestamp;
    }
}

export function escapeCsv(val) {
    if (val === null || val === undefined) return '';
    const str = String(val);
    if (str.includes(',') || str.includes('\n') || str.includes('"')) {
        return `"${str.replace(/"/g, '""')}"`;
    }
    return str;
}
