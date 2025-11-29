import { formatTime } from './utils.js';
import {
    filterState,
    parseFilter,
    matchesFilter,
    findMatchPositions,
    loadPresets,
    addPreset
} from './filter.js';

export const packets = [];
export const selectedPackets = [];
export const hiddenColumns = new Set();

const packetList = document.getElementById('packet-list');
const filterInput = document.getElementById('packet-filter');
const diffBtn = document.getElementById('diff-packets');
const sortHeaders = document.querySelectorAll('.packet-table th.sortable');
const container = document.getElementById('packet-table-container');
const goToLatestBtn = document.getElementById('go-to-latest');
const newPacketCount = document.getElementById('new-packet-count');
const autoscrollToggle = document.getElementById('autoscroll-toggle');

// Filter elements
const directionBtns = document.querySelectorAll('.direction-filter .btn-filter');
const highlightToggle = document.getElementById('highlight-toggle');
const filterPresetSelect = document.getElementById('filter-preset-select');
const savePresetBtn = document.getElementById('save-preset');
const filterHelpBtn = document.getElementById('filter-help');
const filterHelpModal = document.getElementById('filter-help-modal');
const closeFilterHelpBtn = document.getElementById('close-filter-help');
const savePresetModal = document.getElementById('save-preset-modal');
const closeSavePresetBtn = document.getElementById('close-save-preset');
const presetNameInput = document.getElementById('preset-name-input');
const presetFilterPreview = document.getElementById('preset-filter-preview');
const confirmSavePresetBtn = document.getElementById('confirm-save-preset');
const customPresetsGroup = document.getElementById('custom-presets');

let currentSort = { field: null, direction: 'asc' };
let autoScrollEnabled = true;
let missedPackets = 0;

export function addPacketEntry(logLine) {
    // Only process packet logs
    if (!logLine.includes('[PKT]')) return;

    // Example: 2024-01-01T... [PKT] [UP->] f7 0e ... (8 bytes)
    const parts = logLine.split(' ');
    const time = formatTime(parts[0]);

    let direction = '';
    let hexData = '';
    let length = '';

    if (logLine.includes('[UP->]')) {
        direction = 'UP -> Client';
        const dirIndex = logLine.indexOf('[UP->]');
        const bytesIndex = logLine.lastIndexOf('(');
        if (dirIndex !== -1 && bytesIndex !== -1) {
            hexData = logLine.substring(dirIndex + 6, bytesIndex).trim();
            length = logLine.substring(bytesIndex + 1, logLine.indexOf(' bytes'));
        }
    } else if (logLine.includes('[->UP]')) {
        direction = 'Client -> UP';
        const dirIndex = logLine.indexOf('[->UP]');
        const bytesIndex = logLine.lastIndexOf('(');
        if (dirIndex !== -1 && bytesIndex !== -1) {
            hexData = logLine.substring(dirIndex + 6, bytesIndex).trim();
            length = logLine.substring(bytesIndex + 1, logLine.indexOf(' bytes'));
        }
    } else {
        return;
    }

    // Format Hex
    let formattedHex = '';
    const hexBytes = hexData.split(' ');
    for (let i = 0; i < hexBytes.length; i += 8) {
        const group = hexBytes.slice(i, i + 8);
        let groupHtml = '<span class="hex-group">';
        groupHtml += group.map(byte => {
            if (byte === '00') return '<span class="hex-null">00</span>';
            const code = parseInt(byte, 16);
            if (code >= 32 && code <= 126) return `<span class="hex-printable">${byte}</span>`;
            return `<span class="hex-control">${byte}</span>`;
        }).join(' ');
        groupHtml += '</span> ';
        formattedHex += groupHtml;
    }

    // ASCII
    let ascii = '';
    for (const byte of hexBytes) {
        const code = parseInt(byte, 16);
        if (code >= 32 && code <= 126) ascii += String.fromCharCode(code);
        else ascii += '.';
    }

    const packet = {
        time,
        direction,
        length,
        hexRaw: hexData,
        hexFormatted: formattedHex,
        ascii
    };

    packets.push(packet);
    if (packets.length > 1000) packets.shift();

    if (!filterInput.value && !currentSort.field) {
        renderRow(packet);
    } else {
        renderPackets();
    }
}

function renderRow(packet) {
    const row = document.createElement('tr');
    row.packet = packet;

    if (selectedPackets.includes(packet)) {
        row.classList.add('selected');
    }

    row.innerHTML = `
        <td>${packet.time}</td>
        <td class="direction ${packet.direction.includes('UP ->') ? 'up' : 'down'}">${packet.direction}</td>
        <td>${packet.length}</td>
        <td class="hex">${packet.hexFormatted}</td>
        <td class="ascii">${packet.ascii}</td>
    `;

    applyColumnVisibility(row);

    packetList.appendChild(row);

    if (packetList.children.length > 500) {
        packetList.removeChild(packetList.firstChild);
    }

    // Auto-scroll if enabled
    if (autoScrollEnabled) {
        container.scrollTop = container.scrollHeight;
    } else {
        // Track missed packets when not auto-scrolling
        missedPackets++;
        updateGoToLatestButton();
    }
}

// Check if user is at bottom of scroll
function isAtBottom() {
    return container.scrollHeight - container.scrollTop - container.clientHeight < 50;
}

// Update the Go to Latest button visibility and count
function updateGoToLatestButton() {
    if (missedPackets > 0 && !autoScrollEnabled) {
        goToLatestBtn.style.display = 'flex';
        newPacketCount.textContent = `${missedPackets} new`;
    } else {
        goToLatestBtn.style.display = 'none';
    }
}

// Scroll to bottom and reset missed count
function scrollToLatest() {
    container.scrollTop = container.scrollHeight;
    missedPackets = 0;
    autoScrollEnabled = true;
    autoscrollToggle.classList.add('active');
    updateGoToLatestButton();
}

// Toggle auto-scroll
function toggleAutoScroll() {
    autoScrollEnabled = !autoScrollEnabled;
    autoscrollToggle.classList.toggle('active', autoScrollEnabled);

    if (autoScrollEnabled) {
        scrollToLatest();
    }
}

export function renderPackets() {
    packetList.innerHTML = '';

    // Update filter state
    filterState.text = filterInput.value;
    filterState.parsed = parseFilter(filterInput.value);

    const hasActiveFilter = filterState.direction !== 'all' || filterState.text.trim();

    // Filter or process all packets
    let processedPackets = packets.map(p => {
        const matches = matchesFilter(p, filterState.direction, filterState.parsed);
        const matchPositions = matches && filterState.highlightMode ? findMatchPositions(p, filterState.parsed) : null;
        return { packet: p, matches, matchPositions };
    });

    // In normal mode, filter out non-matching packets
    // In highlight mode, show all but mark matches
    if (!filterState.highlightMode && hasActiveFilter) {
        processedPackets = processedPackets.filter(item => item.matches);
    }

    // Apply sorting
    if (currentSort.field) {
        processedPackets.sort((a, b) => {
            let valA = a.packet[currentSort.field];
            let valB = b.packet[currentSort.field];
            if (currentSort.field === 'length') {
                valA = parseInt(valA);
                valB = parseInt(valB);
            }
            if (valA < valB) return currentSort.direction === 'asc' ? -1 : 1;
            if (valA > valB) return currentSort.direction === 'asc' ? 1 : -1;
            return 0;
        });
    }

    // Render rows
    processedPackets.forEach(({ packet: p, matches, matchPositions }) => {
        const row = document.createElement('tr');
        row.packet = p;

        if (selectedPackets.includes(p)) {
            row.classList.add('selected');
        }

        // Apply highlight mode classes
        if (filterState.highlightMode && hasActiveFilter) {
            if (matches) {
                row.classList.add('highlight-match');
            } else {
                row.classList.add('highlight-no-match');
            }
        }

        // Generate hex HTML with optional highlighting
        let hexHtml = p.hexFormatted;
        if (matchPositions && matchPositions.length > 0) {
            hexHtml = generateHighlightedHex(p.hexRaw, matchPositions);
        }

        row.innerHTML = `
            <td>${p.time}</td>
            <td class="direction ${p.direction.includes('UP ->') ? 'up' : 'down'}">${p.direction}</td>
            <td>${p.length}</td>
            <td class="hex">${hexHtml}</td>
            <td class="ascii">${p.ascii}</td>
        `;
        packetList.appendChild(row);
    });

    updateColumnVisibility();

    // Auto-scroll to bottom if enabled and no filter/sort active
    if (autoScrollEnabled && !hasActiveFilter && !currentSort.field) {
        container.scrollTop = container.scrollHeight;
    }
}

// Generate hex HTML with highlighted bytes
function generateHighlightedHex(hexRaw, matchPositions) {
    const hexBytes = hexRaw.split(' ');
    let formattedHex = '';

    for (let i = 0; i < hexBytes.length; i += 8) {
        const group = hexBytes.slice(i, i + 8);
        let groupHtml = '<span class="hex-group">';
        groupHtml += group.map((byte, j) => {
            const byteIndex = i + j;
            const isHighlighted = matchPositions.includes(byteIndex);
            const highlightClass = isHighlighted ? ' highlight-byte' : '';

            if (byte === '00') {
                return `<span class="hex-null${highlightClass}">00</span>`;
            }
            const code = parseInt(byte, 16);
            if (code >= 32 && code <= 126) {
                return `<span class="hex-printable${highlightClass}">${byte}</span>`;
            }
            return `<span class="hex-control${highlightClass}">${byte}</span>`;
        }).join(' ');
        groupHtml += '</span> ';
        formattedHex += groupHtml;
    }

    return formattedHex;
}

export function togglePacketSelection(row, packet) {
    if (selectedPackets.includes(packet)) {
        const idx = selectedPackets.indexOf(packet);
        if (idx > -1) selectedPackets.splice(idx, 1);
        row.classList.remove('selected');
    } else {
        if (selectedPackets.length >= 2) {
            const removed = selectedPackets.shift();
            const rows = document.querySelectorAll('.packet-table tbody tr');
            rows.forEach(r => {
                if (r.packet === removed) r.classList.remove('selected');
            });
        }
        selectedPackets.push(packet);
        row.classList.add('selected');
    }

    diffBtn.innerText = `Diff (${selectedPackets.length}/2)`;
    diffBtn.disabled = selectedPackets.length !== 2;
}

export function clearPackets() {
    packets.length = 0;
    selectedPackets.length = 0;
    packetList.innerHTML = '';
    diffBtn.innerText = `Diff (0/2)`;
    diffBtn.disabled = true;
    missedPackets = 0;
    updateGoToLatestButton();
}

export function updateColumnVisibility() {
    document.querySelectorAll('.packet-table th').forEach((th) => {
        const col = th.dataset.sort;
        th.style.display = hiddenColumns.has(col) ? 'none' : '';
    });

    document.querySelectorAll('.packet-table tbody tr').forEach(row => {
        applyColumnVisibility(row);
    });
}

function applyColumnVisibility(row) {
    const cols = ['time', 'dir', 'len', 'hex', 'ascii'];
    row.querySelectorAll('td').forEach((td, index) => {
        const col = cols[index];
        td.style.display = hiddenColumns.has(col) ? 'none' : '';
    });
}

// Load and render custom presets
function loadCustomPresets() {
    const presets = loadPresets();
    customPresetsGroup.innerHTML = '';
    presets.forEach(preset => {
        const option = document.createElement('option');
        option.value = preset.filter;
        option.textContent = preset.name;
        option.dataset.custom = 'true';
        customPresetsGroup.appendChild(option);
    });
}

export function initPackets() {
    // Load custom presets on init
    loadCustomPresets();

    // Sorting Event Listeners
    sortHeaders.forEach(header => {
        header.addEventListener('click', () => {
            const field = header.dataset.sort;
            if (currentSort.field === field) {
                currentSort.direction = currentSort.direction === 'asc' ? 'desc' : 'asc';
            } else {
                currentSort.field = field;
                currentSort.direction = 'asc';
            }
            sortHeaders.forEach(h => h.classList.remove('sort-asc', 'sort-desc'));
            header.classList.add(currentSort.direction === 'asc' ? 'sort-asc' : 'sort-desc');
            renderPackets();
        });
    });

    // Filter Text Input Event Listener
    filterInput.addEventListener('input', () => {
        renderPackets();
    });

    // Direction Filter Buttons
    directionBtns.forEach(btn => {
        btn.addEventListener('click', () => {
            directionBtns.forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            filterState.direction = btn.dataset.dir;
            renderPackets();
        });
    });

    // Highlight Mode Toggle
    highlightToggle.addEventListener('click', () => {
        filterState.highlightMode = !filterState.highlightMode;
        highlightToggle.classList.toggle('active', filterState.highlightMode);
        renderPackets();
    });

    // Filter Presets Select
    filterPresetSelect.addEventListener('change', (e) => {
        if (e.target.value) {
            filterInput.value = e.target.value;
            renderPackets();
        }
        // Reset select to placeholder
        e.target.selectedIndex = 0;
    });

    // Save Preset Button - Open Modal
    savePresetBtn.addEventListener('click', () => {
        const currentFilter = filterInput.value.trim();
        if (!currentFilter) {
            alert('Enter a filter first');
            return;
        }
        presetFilterPreview.value = currentFilter;
        presetNameInput.value = '';
        savePresetModal.style.display = 'flex';
        presetNameInput.focus();
    });

    // Close Save Preset Modal
    closeSavePresetBtn.addEventListener('click', () => {
        savePresetModal.style.display = 'none';
    });

    // Confirm Save Preset
    confirmSavePresetBtn.addEventListener('click', () => {
        const name = presetNameInput.value.trim();
        const filter = presetFilterPreview.value;
        if (!name) {
            alert('Enter a preset name');
            return;
        }
        addPreset(name, filter);
        loadCustomPresets();
        savePresetModal.style.display = 'none';
    });

    // Filter Help Button
    filterHelpBtn.addEventListener('click', () => {
        filterHelpModal.style.display = 'flex';
    });

    // Close Filter Help Modal
    closeFilterHelpBtn.addEventListener('click', () => {
        filterHelpModal.style.display = 'none';
    });

    // Close modals on outside click
    [filterHelpModal, savePresetModal].forEach(modal => {
        modal.addEventListener('click', (e) => {
            if (e.target === modal) {
                modal.style.display = 'none';
            }
        });
    });

    // Column Toggle Event Listeners
    document.querySelectorAll('.column-toggles input').forEach(toggle => {
        toggle.addEventListener('change', (e) => {
            const col = e.target.dataset.col;
            if (e.target.checked) {
                hiddenColumns.delete(col);
            } else {
                hiddenColumns.add(col);
            }
            updateColumnVisibility();
        });
    });

    // Packet Selection Delegation
    packetList.addEventListener('click', (e) => {
        const row = e.target.closest('tr');
        if (row && row.packet) {
            togglePacketSelection(row, row.packet);
        }
    });

    // Auto-scroll toggle button
    autoscrollToggle.addEventListener('click', toggleAutoScroll);

    // Go to Latest button
    goToLatestBtn.addEventListener('click', scrollToLatest);

    // Detect manual scroll to disable auto-scroll
    container.addEventListener('scroll', () => {
        if (autoScrollEnabled && !isAtBottom()) {
            // User scrolled up, disable auto-scroll
            autoScrollEnabled = false;
            autoscrollToggle.classList.remove('active');
        } else if (!autoScrollEnabled && isAtBottom()) {
            // User scrolled to bottom, re-enable auto-scroll
            autoScrollEnabled = true;
            autoscrollToggle.classList.add('active');
            missedPackets = 0;
            updateGoToLatestButton();
        }
    });

    // Keyboard shortcut: End key to scroll to latest
    document.addEventListener('keydown', (e) => {
        if (e.key === 'End' && document.getElementById('tab-inspector').classList.contains('active')) {
            e.preventDefault();
            scrollToLatest();
        }
    });
}
