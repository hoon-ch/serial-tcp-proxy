
import { initTabs, updateStatus, updateUptime } from './modules/ui.js';
import { addLogEntry, clearLogs } from './modules/logs.js';
import {
    addPacketEntry,
    clearPackets,
    packets,
    selectedPackets,
    hiddenColumns,
    initPackets
} from './modules/packets.js';
import { exportPackets } from './modules/export.js';
import { initInjection } from './modules/injection.js';
import { updateInspector, renderDiff } from './modules/inspector.js';
import { initTheme } from './modules/theme.js';
import { apiUrl, wsUrl } from './modules/api.js';

document.addEventListener('DOMContentLoaded', () => {
    // Initialize UI Modules
    initTheme();
    initTabs();
    initInjection();
    initPackets();

    let startTime = null;
    let isPaused = false;
    let isPacketsPaused = false;
    let reconnectAttempts = 0;
    const maxReconnectAttempts = 5;
    let reconnectTimeout = null;

    // Update connection status UI
    function setConnectionStatus(connected) {
        const statusBadge = document.getElementById('connection-status');
        const statusText = statusBadge.querySelector('.text');
        if (connected) {
            statusBadge.classList.remove('disconnected');
            statusBadge.classList.add('connected');
            statusText.textContent = 'Connected';
        } else {
            statusBadge.classList.remove('connected');
            statusBadge.classList.add('disconnected');
            statusText.textContent = 'Disconnected';
        }
    }

    // Handle incoming message (works for both WebSocket and SSE)
    function handleMessage(type, data) {
        if (type === 'status') {
            const start = updateStatus(data);
            if (start && !startTime) {
                startTime = start;
                setInterval(() => updateUptime(startTime), 1000);
            }
        } else if (type === 'log') {
            const logLine = typeof data === 'string' ? data : JSON.stringify(data);
            if (logLine.includes('[PKT]')) {
                if (!isPacketsPaused) {
                    addPacketEntry(logLine);
                }
            } else {
                if (!isPaused) {
                    addLogEntry(logLine);
                }
            }
        }
    }

    // WebSocket connection
    function connectWebSocket() {
        const ws = new WebSocket(wsUrl('/api/ws'));

        ws.onopen = () => {
            console.log('WebSocket connected');
            setConnectionStatus(true);
            reconnectAttempts = 0;
        };

        ws.onmessage = (event) => {
            try {
                const msg = JSON.parse(event.data);
                handleMessage(msg.type, msg.data);
            } catch (err) {
                console.error('Failed to parse WebSocket message:', err);
            }
        };

        ws.onerror = (err) => {
            console.error('WebSocket error:', err);
        };

        ws.onclose = () => {
            console.log('WebSocket disconnected');
            setConnectionStatus(false);

            // Reconnect with exponential backoff
            if (reconnectAttempts < maxReconnectAttempts) {
                const delay = Math.min(1000 * Math.pow(2, reconnectAttempts), 30000);
                reconnectAttempts++;
                console.log(`Reconnecting in ${delay}ms (attempt ${reconnectAttempts})`);
                reconnectTimeout = setTimeout(connectWebSocket, delay);
            } else {
                console.log('Max reconnect attempts reached, falling back to SSE');
                connectSSE();
            }
        };

        return ws;
    }

    // SSE fallback connection
    function connectSSE() {
        console.log('Connecting via SSE fallback');
        const evtSource = new EventSource(apiUrl('/api/events'));

        evtSource.onopen = () => {
            setConnectionStatus(true);
        };

        evtSource.addEventListener('status', (e) => {
            try {
                const data = JSON.parse(e.data);
                handleMessage('status', data);
            } catch (err) {
                console.error('Failed to parse SSE status:', err);
            }
        });

        evtSource.addEventListener('log', (e) => {
            handleMessage('log', e.data);
        });

        evtSource.onerror = () => {
            setConnectionStatus(false);
        };
    }

    // Status & Uptime - initial fetch
    fetch(apiUrl('/api/status'))
        .then(response => response.json())
        .then(data => {
            const start = updateStatus(data);
            if (start && !startTime) {
                startTime = start;
                setInterval(() => updateUptime(startTime), 1000);
            }
        })
        .catch(err => console.error('Failed to fetch initial status:', err));

    // Start WebSocket connection (with SSE fallback)
    connectWebSocket();

    // Global Controls
    document.getElementById('clear-logs').addEventListener('click', clearLogs);

    const pauseBtn = document.getElementById('pause-logs');
    pauseBtn.addEventListener('click', () => {
        isPaused = !isPaused;
        pauseBtn.textContent = isPaused ? 'Resume' : 'Pause';
        pauseBtn.classList.toggle('active', isPaused);
    });

    document.getElementById('clear-packets').addEventListener('click', clearPackets);

    const pausePacketsBtn = document.getElementById('pause-packets');
    pausePacketsBtn.addEventListener('click', () => {
        isPacketsPaused = !isPacketsPaused;
        pausePacketsBtn.textContent = isPacketsPaused ? 'Resume' : 'Pause';
        pausePacketsBtn.classList.toggle('active', isPacketsPaused);
    });

    document.getElementById('export-packets').addEventListener('click', (e) => {
        e.preventDefault();
        exportPackets(packets, hiddenColumns);
    });

    // Inspector & Diff Logic
    const inspectorPanel = document.getElementById('inspector-panel');
    document.getElementById('close-inspector').addEventListener('click', () => {
        inspectorPanel.style.display = 'none';
        window.getSelection().removeAllRanges();
    });

    document.addEventListener('selectionchange', () => {
        const selection = window.getSelection();
        const text = selection.toString().trim();
        if (!text || !selection.anchorNode || !selection.anchorNode.parentElement.closest('.packet-table')) {
            return;
        }
        const cleanHex = text.replace(/[^0-9a-fA-F]/g, '');
        if (cleanHex.length === 0 || cleanHex.length % 2 !== 0) return;
        updateInspector(cleanHex);
    });

    // Diff Modal
    const diffModal = document.getElementById('diff-modal');
    document.getElementById('close-diff').addEventListener('click', () => {
        diffModal.style.display = 'none';
    });

    document.getElementById('diff-packets').addEventListener('click', () => {
        if (selectedPackets.length !== 2) return;
        // Sort by time
        const sorted = [...selectedPackets].sort((a, b) => a.time.localeCompare(b.time));
        renderDiff(sorted[0], sorted[1]);
        diffModal.style.display = 'flex';
    });
});
