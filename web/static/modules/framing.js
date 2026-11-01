const MAX_LENGTH_ADJUST = 65535 - 255;

export function parseRule(text) {
    const tokens = text.trim().split(/\s+/);
    if (!text.trim()) return null;
    if (tokens.length < 3) throw new Error('Expected header bytes followed by @offset and +adjust');

    const headerTokens = tokens.slice(0, -2);
    if (!headerTokens.every(token => /^[0-9a-f]{2}$/i.test(token))) {
        throw new Error('Header must contain hexadecimal bytes');
    }
    if (!/^@\d+$/.test(tokens.at(-2)) || !/^\+\d+$/.test(tokens.at(-1))) {
        throw new Error('Expected @offset and +adjust after the header');
    }

    const header = headerTokens.map(token => parseInt(token, 16));
    const lengthOffset = Number(tokens.at(-2).slice(1));
    const lengthAdjust = Number(tokens.at(-1).slice(1));
    if (lengthOffset < header.length) throw new Error('Length offset must be after the header');
    if (lengthAdjust < lengthOffset + 1) throw new Error('Length adjustment must include the length byte');
    if (!Number.isSafeInteger(lengthOffset) || !Number.isSafeInteger(lengthAdjust) || lengthAdjust > MAX_LENGTH_ADJUST) {
        throw new Error('Frame length exceeds the supported limit');
    }
    return { header, lengthOffset, lengthAdjust };
}

export function createSplitter(rule) {
    let carry = [];
    const { header, lengthOffset, lengthAdjust } = rule;
    const startsAt = (bytes, index, count) =>
        header.slice(0, count).every((byte, offset) => bytes[index + offset] === byte);

    return bytes => {
        carry.push(...bytes);
        const rows = [];
        while (carry.length) {
            if (!startsAt(carry, 0, Math.min(header.length, carry.length))) {
                let next = 1;
                while (next < carry.length && !startsAt(carry, next, Math.min(header.length, carry.length - next))) next++;
                rows.push(carry.slice(0, next));
                carry = carry.slice(next);
                continue;
            }
            if (carry.length <= lengthOffset) break;
            const total = carry[lengthOffset] + lengthAdjust;
            if (carry.length < total) break;
            rows.push(carry.slice(0, total));
            carry = carry.slice(total);
        }
        return rows;
    };
}

export function createFrameRegistry() {
    let rule = null;
    const streams = new Map();
    return {
        setRule(nextRule) {
            rule = nextRule;
            streams.clear();
        },
        reset() {
            streams.clear();
        },
        split(key, bytes) {
            if (!rule) return [bytes];
            if (!streams.has(key)) streams.set(key, createSplitter(rule));
            return streams.get(key)(bytes);
        }
    };
}
