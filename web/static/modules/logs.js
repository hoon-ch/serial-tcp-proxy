import { formatTime } from './utils.js';

const logsContainer = document.getElementById('logs-container');

export function addLogEntry(logLine) {
    // Example: 2024-01-01T... [PKT] [UP->] ...
    const entry = document.createElement('div');
    entry.className = 'log-entry';

    const parts = logLine.split(' ');
    const time = formatTime(parts[0]);

    let type = 'info';
    if (logLine.includes('[PKT]')) {
        if (logLine.includes('[UP->]')) type = 'pkt-up';
        else if (logLine.includes('[->UP]')) type = 'pkt-down';
    } else if (logLine.includes('[WARN]')) type = 'warn';
    else if (logLine.includes('[ERROR]')) type = 'error';

    entry.classList.add(type);

    entry.innerHTML = `
        <span class="time">${time}</span>
        <span class="content">${logLine.substring(parts[0].length + 1)}</span>
    `;

    logsContainer.appendChild(entry);

    // Auto scroll if near bottom
    if (logsContainer.scrollHeight - logsContainer.scrollTop - logsContainer.clientHeight < 100) {
        logsContainer.scrollTop = logsContainer.scrollHeight;
    }

    // Limit max logs
    if (logsContainer.children.length > 1000) {
        logsContainer.removeChild(logsContainer.firstChild);
    }
}

export function clearLogs() {
    logsContainer.innerHTML = '';
}
