
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

document.addEventListener('DOMContentLoaded', () => {
    // Initialize UI Modules
    initTheme();
    initTabs();
    initInjection();
    initPackets();

    let startTime = null;
    let isPaused = false;
    let isPacketsPaused = false;

    // Status & Uptime
    fetch('/api/status')
        .then(response => response.json())
        .then(data => {
            const start = updateStatus(data);
            if (start && !startTime) {
                startTime = start;
                setInterval(() => updateUptime(startTime), 1000);
            }
        })
        .catch(err => console.error('Failed to fetch initial status:', err));

    // SSE
    const evtSource = new EventSource('/api/events');

    evtSource.addEventListener('status', (e) => {
        const data = JSON.parse(e.data);
        const start = updateStatus(data);
        if (start && !startTime) {
            startTime = start;
            setInterval(() => updateUptime(startTime), 1000);
        }
    });

    evtSource.addEventListener('log', (e) => {
        const logLine = e.data;
        if (logLine.includes('[PKT]')) {
            if (!isPacketsPaused) {
                addPacketEntry(logLine);
            }
        } else {
            if (!isPaused) {
                addLogEntry(logLine);
            }
        }
    });

    evtSource.onerror = () => {
        const statusBadge = document.getElementById('connection-status');
        const statusText = statusBadge.querySelector('.text');
        statusBadge.classList.remove('connected');
        statusBadge.classList.add('disconnected');
        statusText.textContent = 'Disconnected';
    };

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
