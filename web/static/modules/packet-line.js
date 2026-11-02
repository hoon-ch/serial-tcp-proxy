export function parsePacketLine(line) {
    const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})) \[PKT\] \[(UP->|->UP)\] ((?:[0-9a-f]{2}(?: [0-9a-f]{2})*)?) \((0|[1-9]\d*) bytes\)(?: from (\S+))?\n?(?![\s\S])/.exec(line);
    if (!match || Number.isNaN(Date.parse(match[1]))) return null;

    const bytes = match[3] ? match[3].split(' ').map(byte => parseInt(byte, 16)) : [];
    if (bytes.length !== Number(match[4])) return null;

    return {
        time: match[1],
        direction: match[2] === 'UP->' ? 'up' : 'down',
        bytes,
        source: match[5] || null,
    };
}
