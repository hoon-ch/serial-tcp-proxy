export function updateInspector(hex) {
    const inspectorPanel = document.getElementById('inspector-panel');
    const bytes = new Uint8Array(hex.match(/.{1,2}/g).map(byte => parseInt(byte, 16)));
    const dataView = new DataView(bytes.buffer);

    // Binary
    let binary = '';
    if (bytes.length <= 4) {
        binary = Array.from(bytes).map(b => b.toString(2).padStart(8, '0')).join(' ');
    } else {
        binary = Array.from(bytes.slice(0, 4)).map(b => b.toString(2).padStart(8, '0')).join(' ') + ' ...';
    }
    document.getElementById('insp-binary').innerText = binary;

    // Int8 / Uint8
    if (bytes.length >= 1) {
        document.getElementById('insp-int8').innerText = dataView.getInt8(0);
        document.getElementById('insp-uint8').innerText = dataView.getUint8(0);
    } else {
        document.getElementById('insp-int8').innerText = '-';
        document.getElementById('insp-uint8').innerText = '-';
    }

    // Int16 / Uint16
    if (bytes.length >= 2) {
        const be = dataView.getInt16(0, false);
        const le = dataView.getInt16(0, true);
        const ube = dataView.getUint16(0, false);
        const ule = dataView.getUint16(0, true);
        document.getElementById('insp-int16').innerText = `${le} (LE) / ${be} (BE)`;
        document.getElementById('insp-uint16').innerText = `${ule} (LE) / ${ube} (BE)`;
    } else {
        document.getElementById('insp-int16').innerText = '-';
        document.getElementById('insp-uint16').innerText = '-';
    }

    // Int32 / Uint32
    if (bytes.length >= 4) {
        const be = dataView.getInt32(0, false);
        const le = dataView.getInt32(0, true);
        const ube = dataView.getUint32(0, false);
        const ule = dataView.getUint32(0, true);
        document.getElementById('insp-int32').innerText = `${le} (LE) / ${be} (BE)`;
        document.getElementById('insp-uint32').innerText = `${ule} (LE) / ${ube} (BE)`;
    } else {
        document.getElementById('insp-int32').innerText = '-';
        document.getElementById('insp-uint32').innerText = '-';
    }

    // Float32
    if (bytes.length >= 4) {
        const be = dataView.getFloat32(0, false);
        const le = dataView.getFloat32(0, true);
        document.getElementById('insp-float32').innerText = `${le.toFixed(4)} (LE) / ${be.toFixed(4)} (BE)`;
    } else {
        document.getElementById('insp-float32').innerText = '-';
    }

    // String
    let str = '';
    for (let i = 0; i < bytes.length; i++) {
        const code = bytes[i];
        if (code >= 32 && code <= 126) {
            str += String.fromCharCode(code);
        } else {
            str += '.';
        }
    }
    document.getElementById('insp-string').innerText = str;

    inspectorPanel.style.display = 'block';
}

export function renderDiff(p1, p2) {
    const hex1 = p1.hexRaw.split(' ');
    const hex2 = p2.hexRaw.split(' ');

    const containerA = document.getElementById('diff-packet-a');
    const containerB = document.getElementById('diff-packet-b');

    let htmlA = '';
    let htmlB = '';

    const maxLength = Math.max(hex1.length, hex2.length);

    for (let i = 0; i < maxLength; i++) {
        const b1 = hex1[i] || '';
        const b2 = hex2[i] || '';

        const isDiff = b1 !== b2;
        const diffClass = isDiff ? 'diff' : '';

        if (b1) htmlA += `<span class="diff-byte ${diffClass}">${b1}</span> `;
        if (b2) htmlB += `<span class="diff-byte ${diffClass}">${b2}</span> `;
    }

    containerA.innerHTML = htmlA;
    containerB.innerHTML = htmlB;
}
