"""Benchmark clients: N raw TCP clients on the proxy port and M WebSocket clients on /api/ws.

Usage: python3 loadgen.py HOST TCP_PORT WEB_PORT TCP_CLIENTS WS_CLIENTS SECONDS WARMUP PARSE
PARSE=records parses upstream.py 32-byte records for latency and loss; PARSE=bytes only counts bytes.
Prints one JSON object with per-run totals.
"""
import asyncio
import json
import os
import struct
import sys
import time

import websockets

RECORD = 32


def proxy_cpu_s():
    fields = open("/proc/1/stat").read().rsplit(")", 1)[1].split()
    return (int(fields[11]) + int(fields[12])) / os.sysconf("SC_CLK_TCK")


class TcpStats:
    def __init__(self):
        self.bytes = 0
        self.records = 0
        self.bad = 0
        self.gaps = 0
        self.latencies = []
        self.disconnects = 0


async def tcp_client(host, port, stats, parse, stop, window):
    reader, writer = await asyncio.open_connection(host, port)
    buf = b""
    last_seq = None
    try:
        while not stop.is_set():
            try:
                data = await asyncio.wait_for(reader.read(65536), 0.5)
            except asyncio.TimeoutError:
                continue
            if not data:
                stats.disconnects += 1
                return
            now = time.monotonic_ns()
            measuring = window[0] <= time.monotonic() < window[1]
            if measuring:
                stats.bytes += len(data)
            if parse != "records":
                continue
            buf += data
            usable = len(buf) - len(buf) % RECORD
            for off in range(0, usable, RECORD):
                magic, seq, sent = struct.unpack_from(">BIQ", buf, off)
                if magic != 0xA5:
                    stats.bad += 1
                    continue
                if last_seq is not None and seq != last_seq + 1:
                    stats.gaps += 1
                last_seq = seq
                if measuring:
                    stats.records += 1
                    stats.latencies.append(now - sent)
            buf = buf[usable:]
    finally:
        writer.close()


async def ws_client(host, port, counter, stop, window):
    url = f"ws://{host}:{port}/api/ws"
    async with websockets.connect(url, origin=f"http://{host}:{port}", max_size=None) as ws:
        while not stop.is_set():
            try:
                msg = await asyncio.wait_for(ws.recv(), 0.5)
            except asyncio.TimeoutError:
                continue
            if window[0] <= time.monotonic() < window[1]:
                body = json.loads(msg)
                if body.get("type") == "log" and "[PKT]" in body.get("data", ""):
                    counter["messages"] += 1


def pct(values, p):
    if not values:
        return None
    values = sorted(values)
    return values[min(len(values) - 1, int(len(values) * p))] / 1e6


async def main():
    host, tcp_port, web_port = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
    n_tcp, n_ws, seconds, warmup, parse = int(sys.argv[4]), int(sys.argv[5]), float(sys.argv[6]), float(sys.argv[7]), sys.argv[8]
    stop = asyncio.Event()
    measure_from = time.monotonic() + warmup
    window = (measure_from, measure_from + seconds)
    stats = [TcpStats() for _ in range(n_tcp)]
    ws_counter = {"messages": 0}
    tasks = [asyncio.create_task(tcp_client(host, tcp_port, s, parse, stop, window)) for s in stats]
    tasks += [asyncio.create_task(ws_client(host, web_port, ws_counter, stop, window)) for _ in range(n_ws)]
    cpu0 = time.process_time()
    await asyncio.sleep(max(0.0, measure_from - time.monotonic()))
    proxy0, t0 = proxy_cpu_s(), time.monotonic()
    await asyncio.sleep(seconds)
    proxy1, t1 = proxy_cpu_s(), time.monotonic()
    stop.set()
    results = await asyncio.gather(*tasks, return_exceptions=True)
    errors = [repr(r) for r in results if isinstance(r, BaseException)]
    lat = [x for s in stats for x in s.latencies]
    checked = parse == "records"
    print(json.dumps({
        "tcp_clients": n_tcp,
        "ws_clients": n_ws,
        "seconds": seconds,
        "window_s": round(t1 - t0, 3),
        "proxy_cpu_s": round(proxy1 - proxy0, 3),
        "tcp_bytes": sum(s.bytes for s in stats),
        "tcp_records": sum(s.records for s in stats) if checked else None,
        "bad_records": sum(s.bad for s in stats) if checked else None,
        "seq_gaps": sum(s.gaps for s in stats) if checked else None,
        "disconnects": sum(s.disconnects for s in stats),
        "ws_log_messages": ws_counter["messages"],
        "lat_p50_ms": pct(lat, 0.50),
        "lat_p99_ms": pct(lat, 0.99),
        "lat_max_ms": pct(lat, 1.0),
        "loadgen_cpu_s": round(time.process_time() - cpu0, 2),
        "errors": errors,
    }))


asyncio.run(main())
