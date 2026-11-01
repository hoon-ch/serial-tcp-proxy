"""Time from exec to an accepting web port, for each server binary, alternating sides.

Usage (inside the tools container): python3 startup.py RUNS bin/go/server bin/rs/server ...
"""
import json
import os
import socket
import statistics
import subprocess
import sys
import time

runs, binaries = int(sys.argv[1]), sys.argv[2:]
env = dict(os.environ, UPSTREAM_HOST="127.0.0.1", UPSTREAM_PORT="9", LISTEN_PORT="28899", WEB_PORT="28080")
times = {b: [] for b in binaries}
hung = {b: 0 for b in binaries}
for i in range(runs):
    for b in binaries[i % len(binaries):] + binaries[:i % len(binaries)]:
        start = time.perf_counter()
        proc = subprocess.Popen([b], env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        while True:
            try:
                socket.create_connection(("127.0.0.1", 28080), timeout=0.05).close()
                break
            except OSError:
                if time.perf_counter() - start > 5:
                    proc.kill()
                    sys.exit(f"{b} never opened the web port:\n{proc.communicate()[0].decode()[-2000:]}")
        times[b].append((time.perf_counter() - start) * 1000)
        proc.terminate()
        try:
            proc.communicate(timeout=3)
        except subprocess.TimeoutExpired:
            hung[b] += 1
            proc.kill()
            proc.communicate()
        time.sleep(0.2)
print(json.dumps({b: {"median_ms": round(statistics.median(t), 2), "min_ms": round(min(t), 2), "max_ms": round(max(t), 2),
                      "runs": len(t), "sigterm_hangs": hung[b]} for b, t in times.items()}, indent=1))
