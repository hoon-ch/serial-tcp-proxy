#!/bin/sh
# Explain stress-scenario CPU for one server binary: user vs kernel CPU in an untraced
# window, then syscall counts with strace -c (tracing slows the server; use the counts only).
# Usage: bench/profile.sh <binary> <label>
set -eu
bin=$1
label=$2
bench=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
cp "$bin" "$work/server"
docker build -q -t "stp-prof-$label" -f "$bench/proxy.Dockerfile" "$work" >/dev/null
docker rm -f stpb-up stpb-proxy stpb-load >/dev/null 2>&1 || true
docker run -d --name stpb-up --network stpbench -v "$bench:/bench:ro" stp-bench-tools python3 -u /bench/upstream.py records 5000 >/dev/null
docker run -d --name stpb-proxy --network stpbench --cpus 1 --memory 256m -e UPSTREAM_HOST=stpb-up -e UPSTREAM_PORT=8899 \
  -e LISTEN_PORT=18899 -e WEB_PORT=18080 -e MAX_CLIENTS=20 -e LOG_PACKETS=false "stp-prof-$label" >/dev/null
sleep 2
docker run -d --name stpb-load --network stpbench -v "$bench:/bench:ro" stp-bench-tools \
  python3 /bench/loadgen.py stpb-proxy 18899 18080 10 1 60 5 records >/dev/null
sleep 8
ticks() { docker exec stpb-proxy cat /proc/1/stat | awk '{print $14, $15}'; }
set -- $(ticks); u0=$1; s0=$2
sleep 10
set -- $(ticks); echo "untraced 10 s: user_ticks=$(($1 - u0)) sys_ticks=$(($2 - s0)) (100 ticks = 1 CPU-second)"
docker run --rm --privileged --pid=container:stpb-proxy alpine:3.22 sh -c '
  apk add -q strace >/dev/null
  timeout -s INT 10 strace -c -f -p 1 2>&1 | grep -v -e attached -e detached -e ptrace_syscall_info | head -14'
docker rm -f stpb-up stpb-proxy stpb-load >/dev/null
rm -rf "$work"
