import { describe, expect, test } from 'bun:test';
import { parsePacketLine } from '../../web/static/modules/packet-line.js';

describe('parsePacketLine', () => {
    test('parses upstream packets', () => {
        expect(parsePacketLine('2026-11-02T12:34:56.123456789Z [PKT] [UP->] f7 0e 11 (3 bytes)')).toEqual({
            time: '2026-11-02T12:34:56.123456789Z', direction: 'up', bytes: [0xf7, 0x0e, 0x11], source: null,
        });
    });

    test('parses client packets', () => {
        expect(parsePacketLine('2026-11-02T12:34:56Z [PKT] [->UP] f7 0e (2 bytes) from client#1')).toEqual({
            time: '2026-11-02T12:34:56Z', direction: 'down', bytes: [0xf7, 0x0e], source: 'client#1',
        });
    });

    test('rejects packet-like text inside a warning', () => {
        expect(parsePacketLine("2026-11-02T12:34:56Z [WARN] Login failed for user '[PKT] [->UP] <img/src/onerror=x> (1 bytes)' from client")).toBeNull();
    });

    test('rejects a mismatched byte count', () => {
        expect(parsePacketLine('2026-11-02T12:34:56Z [PKT] [UP->] f7 0e (3 bytes)')).toBeNull();
    });
});
