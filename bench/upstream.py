"""Mock EW11 for benchmarks. Accepts the proxy's upstream connection and sends bytes.

Modes:
  replay FILE        replay a capture (bench/capture.py) with its original timing, looping
  records RATE       send 32-byte timestamped records at RATE records/s (see RECORD below)
  flood              send 32-byte records as fast as the socket accepts them

Each record: 0xA5, seq (u32 BE), CLOCK_MONOTONIC ns at send (u64 BE), 19 bytes of zero.
Containers on one VM share CLOCK_MONOTONIC, so loadgen.py can compute delivery latency.
"""
import asyncio
import json
import struct
import sys
import time

RECORD = 32
PORT = 8899


def record(seq):
    return struct.pack(">BIQ", 0xA5, seq & 0xFFFFFFFF, time.monotonic_ns()).ljust(RECORD, b"\0")


async def replay(writer, path):
    rows = [json.loads(line) for line in open(path)]
    period = rows[-1]["t"] + 0.2
    loop_start = time.monotonic()
    while True:
        for row in rows:
            delay = loop_start + row["t"] - time.monotonic()
            if delay > 0:
                await asyncio.sleep(delay)
            writer.write(bytes.fromhex(row["hex"]))
            await writer.drain()
        loop_start += period


async def records(writer, rate):
    seq = 0
    start = time.monotonic()
    while True:
        due = int((time.monotonic() - start) * rate)
        if due > seq:
            writer.write(b"".join(record(s) for s in range(seq, due)))
            seq = due
            await writer.drain()
        await asyncio.sleep(0.001)


async def flood(writer):
    block = b"".join(record(s) for s in range(2048))
    while True:
        writer.write(block)
        await writer.drain()


async def handle(reader, writer, mode, arg):
    print(f"proxy connected, mode={mode}", flush=True)
    sink = asyncio.create_task(drain(reader))
    try:
        if mode == "replay":
            await replay(writer, arg)
        elif mode == "records":
            await records(writer, float(arg))
        elif mode == "flood":
            await flood(writer)
        else:
            await asyncio.Event().wait()
    except (ConnectionError, OSError):
        pass
    finally:
        sink.cancel()


async def drain(reader):
    while await reader.read(65536):
        pass


async def main():
    mode = sys.argv[1]
    arg = sys.argv[2] if len(sys.argv) > 2 else None
    server = await asyncio.start_server(lambda r, w: handle(r, w, mode, arg), "0.0.0.0", PORT)
    async with server:
        await server.serve_forever()


asyncio.run(main())
