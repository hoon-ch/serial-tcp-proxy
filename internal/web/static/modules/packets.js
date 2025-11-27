import { formatTime } from './utils.js';

export const packets = [];
export const selectedPackets = [];
export const hiddenColumns = new Set();

const packetList = document.getElementById('packet-list');
const filterInput = document.getElementById('packet-filter');
const diffBtn = document.getElementById('diff-packets');
const sortHeaders = document.querySelectorAll('.packet-table th.sortable');

let currentSort = { field: null, direction: 'asc' };

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

    const container = packetList.parentElement.parentElement;
    const isAtBottom = container.scrollHeight - container.scrollTop - container.clientHeight < 50;

    packetList.appendChild(row);

    if (packetList.children.length > 500) {
        packetList.removeChild(packetList.firstChild);
    }

    if (isAtBottom) {
        container.scrollTop = container.scrollHeight;
    }
}

export function renderPackets() {
    packetList.innerHTML = '';

    const filterText = filterInput.value.toLowerCase();
    let filtered = packets.filter(p => {
        if (!filterText) return true;
        return p.hexRaw.toLowerCase().includes(filterText) ||
            p.ascii.toLowerCase().includes(filterText);
    });

    if (currentSort.field) {
        filtered.sort((a, b) => {
            let valA = a[currentSort.field];
            let valB = b[currentSort.field];
            if (currentSort.field === 'length') {
                valA = parseInt(valA);
                valB = parseInt(valB);
            }
            if (valA < valB) return currentSort.direction === 'asc' ? -1 : 1;
            if (valA > valB) return currentSort.direction === 'asc' ? 1 : -1;
            return 0;
        });
    }

    filtered.forEach(p => {
        const row = document.createElement('tr');
        row.packet = p;
        if (selectedPackets.includes(p)) {
            row.classList.add('selected');
        }
        row.innerHTML = `
            <td>${p.time}</td>
            <td class="direction ${p.direction.includes('UP ->') ? 'up' : 'down'}">${p.direction}</td>
            <td>${p.length}</td>
            <td class="hex">${p.hexFormatted}</td>
            <td class="ascii">${p.ascii}</td>
        `;
        packetList.appendChild(row);
    });

    updateColumnVisibility();

    if (!filterText && !currentSort.field) {
        const container = packetList.parentElement.parentElement;
        container.scrollTop = container.scrollHeight;
    }
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

export function initPackets() {
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

    // Filter Event Listener
    filterInput.addEventListener('input', () => {
        renderPackets();
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
}
