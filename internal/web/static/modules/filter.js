// Filter module for advanced packet filtering

const STORAGE_KEY = 'serial-tcp-proxy-filter-presets';

// Filter state
export const filterState = {
    direction: 'all',  // 'all' | 'up' | 'down'
    text: '',
    parsed: null,
    highlightMode: false
};

// Parse filter text into structured filter object
export function parseFilter(text) {
    if (!text.trim()) return null;

    const parsed = {
        direction: null,
        length: null,
        hex: null,
        ascii: null,
        regex: null,
        plainText: null
    };

    // Check for regex pattern: /pattern/ or /pattern/flags
    const regexMatch = text.match(/^\/(.+)\/([gimsuy]*)$/);
    if (regexMatch) {
        try {
            parsed.regex = new RegExp(regexMatch[1], regexMatch[2]);
            return parsed;
        } catch (e) {
            // Invalid regex, treat as plain text
        }
    }

    // Split by spaces and parse each token
    const tokens = text.split(/\s+/);
    const plainTextParts = [];

    for (const token of tokens) {
        if (!token) continue;

        // Direction filter: dir:up or dir:down
        const dirMatch = token.match(/^dir:(up|down)$/i);
        if (dirMatch) {
            parsed.direction = dirMatch[1].toLowerCase();
            continue;
        }

        // Length filter: len:N, len:>N, len:<N, len:>=N, len:<=N, len:N-M
        const lenMatch = token.match(/^len:(>=?|<=?)?(\d+)(?:-(\d+))?$/);
        if (lenMatch) {
            const op = lenMatch[1] || '=';
            const value = parseInt(lenMatch[2], 10);
            const max = lenMatch[3] ? parseInt(lenMatch[3], 10) : null;

            if (max !== null) {
                // Range: N-M
                parsed.length = { op: 'range', min: value, max: max };
            } else {
                parsed.length = { op: op, value: value };
            }
            continue;
        }

        // Hex filter: hex:XX XX XX
        const hexMatch = token.match(/^hex:(.+)$/i);
        if (hexMatch) {
            // Accumulate hex pattern (may span multiple tokens)
            if (parsed.hex) {
                parsed.hex += ' ' + hexMatch[1];
            } else {
                parsed.hex = hexMatch[1];
            }
            continue;
        }

        // ASCII filter: ascii:text
        const asciiMatch = token.match(/^ascii:(.+)$/i);
        if (asciiMatch) {
            if (parsed.ascii) {
                parsed.ascii += ' ' + asciiMatch[1];
            } else {
                parsed.ascii = asciiMatch[1];
            }
            continue;
        }

        // Plain text (search in both hex and ascii)
        plainTextParts.push(token);
    }

    if (plainTextParts.length > 0) {
        parsed.plainText = plainTextParts.join(' ').toLowerCase();
    }

    return parsed;
}

// Check if a packet matches the filter
export function matchesFilter(packet, directionFilter, parsed) {
    // Direction filter from button group
    if (directionFilter === 'up' && !packet.direction.includes('UP ->')) {
        return false;
    }
    if (directionFilter === 'down' && !packet.direction.includes('-> UP')) {
        return false;
    }

    // No text filter
    if (!parsed) return true;

    // Direction from text filter
    if (parsed.direction === 'up' && !packet.direction.includes('UP ->')) {
        return false;
    }
    if (parsed.direction === 'down' && !packet.direction.includes('-> UP')) {
        return false;
    }

    // Length filter
    if (parsed.length) {
        const len = parseInt(packet.length, 10);
        const { op, value, min, max } = parsed.length;

        switch (op) {
            case '=':
                if (len !== value) return false;
                break;
            case '>':
                if (len <= value) return false;
                break;
            case '<':
                if (len >= value) return false;
                break;
            case '>=':
                if (len < value) return false;
                break;
            case '<=':
                if (len > value) return false;
                break;
            case 'range':
                if (len < min || len > max) return false;
                break;
        }
    }

    // Hex filter
    if (parsed.hex) {
        const normalizedHex = packet.hexRaw.toLowerCase().replace(/\s+/g, '');
        const searchHex = parsed.hex.toLowerCase().replace(/\s+/g, '');
        if (!normalizedHex.includes(searchHex)) return false;
    }

    // ASCII filter
    if (parsed.ascii) {
        if (!packet.ascii.toLowerCase().includes(parsed.ascii.toLowerCase())) {
            return false;
        }
    }

    // Regex filter (on hex data without spaces)
    if (parsed.regex) {
        const normalizedHex = packet.hexRaw.toLowerCase().replace(/\s+/g, '');
        if (!parsed.regex.test(normalizedHex)) return false;
    }

    // Plain text (search in both hex and ascii)
    if (parsed.plainText) {
        const hexMatch = packet.hexRaw.toLowerCase().includes(parsed.plainText);
        const asciiMatch = packet.ascii.toLowerCase().includes(parsed.plainText);
        if (!hexMatch && !asciiMatch) return false;
    }

    return true;
}

// Find matching byte positions for highlighting
export function findMatchPositions(packet, parsed) {
    if (!parsed) return null;

    const positions = [];
    const hexLower = packet.hexRaw.toLowerCase();

    // Hex pattern match
    if (parsed.hex) {
        const searchHex = parsed.hex.toLowerCase().replace(/\s+/g, '');
        const hexNoSpaces = hexLower.replace(/\s+/g, '');
        let pos = 0;
        while ((pos = hexNoSpaces.indexOf(searchHex, pos)) !== -1) {
            // Convert position to byte index
            const byteStart = Math.floor(pos / 2);
            const byteEnd = Math.ceil((pos + searchHex.length) / 2);
            for (let i = byteStart; i < byteEnd; i++) {
                if (!positions.includes(i)) positions.push(i);
            }
            pos++;
        }
    }

    // Regex match
    if (parsed.regex) {
        try {
            const hexNoSpaces = hexLower.replace(/\s+/g, '');
            let match;
            const regex = new RegExp(parsed.regex.source, parsed.regex.flags.includes('g') ? parsed.regex.flags : parsed.regex.flags + 'g');
            while ((match = regex.exec(hexNoSpaces)) !== null) {
                const byteStart = Math.floor(match.index / 2);
                const byteEnd = Math.ceil((match.index + match[0].length) / 2);
                for (let i = byteStart; i < byteEnd; i++) {
                    if (!positions.includes(i)) positions.push(i);
                }
                // Prevent infinite loop on zero-length matches
                if (match[0].length === 0) break;
            }
        } catch (e) {
            // Invalid regex, skip
        }
    }

    // Plain text hex match
    if (parsed.plainText) {
        const hexNoSpaces = hexLower.replace(/\s+/g, '');
        let pos = 0;
        while ((pos = hexNoSpaces.indexOf(parsed.plainText, pos)) !== -1) {
            const byteStart = Math.floor(pos / 2);
            const byteEnd = Math.ceil((pos + parsed.plainText.length) / 2);
            for (let i = byteStart; i < byteEnd; i++) {
                if (!positions.includes(i)) positions.push(i);
            }
            pos++;
        }
    }

    return positions.length > 0 ? positions.sort((a, b) => a - b) : null;
}

// Load custom presets from localStorage
export function loadPresets() {
    try {
        const saved = localStorage.getItem(STORAGE_KEY);
        return saved ? JSON.parse(saved) : [];
    } catch (e) {
        return [];
    }
}

// Save custom presets to localStorage
export function savePresets(presets) {
    try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(presets));
    } catch (e) {
        console.error('Failed to save presets:', e);
    }
}

// Add a new preset
export function addPreset(name, filter) {
    const presets = loadPresets();
    presets.push({ name, filter });
    savePresets(presets);
    return presets;
}

// Remove a preset by name
export function removePreset(name) {
    const presets = loadPresets();
    const filtered = presets.filter(p => p.name !== name);
    savePresets(filtered);
    return filtered;
}
