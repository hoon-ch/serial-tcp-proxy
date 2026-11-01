"""Record the bytes a live proxy broadcasts, one JSON line per TCP read.

Usage: python3 bench/capture.py HOST PORT SECONDS OUT.jsonl
The client only reads. It never writes, so nothing reaches the serial bus.
"""
import json
import socket
import sys
import time

host, port, seconds, out = sys.argv[1], int(sys.argv[2]), float(sys.argv[3]), sys.argv[4]
sock = socket.create_connection((host, port))
sock.settimeout(1.0)
start = time.monotonic()
reads = 0
with open(out, "w") as f:
    while (elapsed := time.monotonic() - start) < seconds:
        try:
            data = sock.recv(4096)
        except socket.timeout:
            continue
        if not data:
            break
        f.write(json.dumps({"t": round(elapsed, 6), "hex": data.hex()}) + "\n")
        reads += 1
sock.close()
print(f"{reads} reads in {seconds:.0f}s")
