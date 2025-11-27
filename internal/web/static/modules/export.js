import { escapeCsv } from './utils.js';

export function exportPackets(packets, hiddenColumns) {
    if (packets.length === 0) {
        alert('No packets to export');
        return;
    }

    // Determine visible fields based on hiddenColumns
    const uiCols = ['time', 'dir', 'len', 'hex', 'ascii'];
    const colToField = {
        'time': 'time',
        'dir': 'direction',
        'len': 'length',
        'hex': 'hexRaw',
        'ascii': 'ascii'
    };

    const visibleFields = uiCols
        .filter(col => !hiddenColumns.has(col))
        .map(col => colToField[col]);

    if (visibleFields.length === 0) {
        alert('No columns selected for export');
        return;
    }

    // Convert to TOON format
    let toonStr = `packets[${packets.length}]{${visibleFields.join(',')}}:\n`;

    toonStr += packets.map(p => {
        return visibleFields.map(f => {
            return escapeCsv(p[f]);
        }).join(',');
    }).join('\n');

    const dataStr = "data:text/plain;charset=utf-8," + encodeURIComponent(toonStr);
    const downloadAnchorNode = document.createElement('a');
    downloadAnchorNode.setAttribute("href", dataStr);
    const safeDate = new Date().toISOString().replace(/[:.]/g, '-');
    downloadAnchorNode.setAttribute("download", "serial_packets_" + safeDate + ".toon");
    document.body.appendChild(downloadAnchorNode); // required for firefox
    downloadAnchorNode.click();
    downloadAnchorNode.remove();
}
