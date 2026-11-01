# Go vs Rust (Topcoat) resource comparison

This branch adds `rust/`, a Rust reimplementation of the proxy whose web backend runs on [Topcoat](https://github.com/tokio-rs/topcoat) 0.11. It serves the same `internal/web/static` UI and passes the same black-box contract suite (`bench/contract/contract.mjs`, 42 checks, both auth modes) as the Go server. This page reports what each version costs to run.

## Verdict

- **Memory.** Rust uses a quarter to a third of Go's memory. Idle RSS is 1.9 MB against 6.8 MB for tuned Go. On the Raspberry Pi 4 the add-on container uses 1.2 MB against 4.9 MB.
- **CPU at real bus traffic.** There is no measurable difference. Both use about 0.1% of one core.
- **CPU under steady load.** At 5,000 records/s to 10 clients, there is no measurable difference once Go runs with `GOMAXPROCS=1`. Tuned Go uses 6.2% and Rust uses 4.9%, and the ranges overlap.
- **CPU at saturation.** Rust needs about 40% less CPU per byte forwarded: 0.94 CPU-s/GB against 1.56. Here the ranges do not overlap.
- **Latency.** There is no measurable difference.
- **Biggest single cost.** Go's `LogPacket` rebuilds the hex string once per byte, so its cost is quadratic in the chunk size. Fixing that one function cuts Go's CPU by 4× under steady load and by over 100× at saturation. It matters more than the language choice.

For this installation, a switch would save about 3.7 MB of RAM on an 8 GB Pi. CPU stays near zero either way.

## Sides

| side | build |
|---|---|
| `go` | Go 1.25.4 as shipped in v1.3.3 (`CGO_ENABLED=0 -ldflags "-s -w"`) |
| `go-tuned` | `go` plus `go-tuned.patch`: linear hex formatting in `LogPacket` |
| `go-tuned-p1` | `go-tuned` run with `GOMAXPROCS=1` |
| `rs` | Rust 1.99.1, tokio, Topcoat 0.11, musl static, `lto = "fat"`, `codegen-units = 1`. It copies Go's quadratic formatting on purpose. |
| `rs-tuned` | `rs` with `--features linear-hex` |
| `rs-tuned-mi` | `rs-tuned` with `--features mimalloc` |

`go.mod` declares `go 1.22`. At that language version Go 1.25 keeps the old `GOMAXPROCS` default, which is the host's 8 CPUs rather than the container's 1-CPU limit. `go-tuned-p1` is the Go side configured for the benchmark's limit. Tokio already sizes its worker pool from the CPU limit, so Rust runs one worker.

Both languages do the same per-packet work. Each server formats a log line for every chunk, keeps a 1000-line replay buffer, and fans out to TCP clients one by one with a 100 ms write timeout. WebSocket and SSE subscribers have the same channel sizes on both sides.

## Method

- **Environment.** Each run starts a mock EW11 (`upstream.py`), a server container capped at 1 CPU and 256 MiB, and a load generator (`loadgen.py`). Everything runs in an OrbStack linux/arm64 VM with 8 CPUs.
- **Run order.** `run.py` runs every side 5 times per scenario and rotates the side order between runs. Tables show the median and the min to max range.
- **CPU window.** The load generator shares the server's PID namespace. It reads the server's CPU time at the start and the end of the same window in which it counts bytes.
- **Correctness checks.** All scenarios count disconnects, WebSocket log messages, and proxy warnings. Only `stress` parses records, so only `stress` checks sequence gaps and malformed records.
- **Host load.** The host Mac was busy, with a 1-minute load average between 14 and 41 during the final run. Rotating the order spreads that noise across all sides. It still widens the ranges.

The four scenarios:

- **`idle`.** The upstream is connected, sends nothing, and no clients are attached.
- **`real`.** Replays 60 s of live bus traffic (`data/bus-capture.jsonl`: 5 reads/s, 102 B/s) to 1 TCP client and 1 WebSocket client.
- **`stress`.** Sends 32-byte timestamped records at 5,000 records/s to 10 TCP clients and 1 WebSocket client.
- **`flood`.** The upstream writes one prebuilt block as fast as it can, to 10 TCP clients.

## Results

All numbers come from `results/20261102-023910/` (`summary.md`, `raw.jsonl`). Earlier result directories are superseded runs; `decisions.tsv` explains why each was replaced.

### Memory (RSS, MB)

| scenario | go | go-tuned | go-tuned-p1 | rs | rs-tuned | rs-tuned-mi |
|---|---|---|---|---|---|---|
| idle | 7.7 (7.2-8.0) | 7.8 (7.1-7.9) | 6.8 (6.8-6.8) | 1.9 (1.9-1.9) | 1.9 (1.9-1.9) | 8.1 (8.1-8.1) |
| real | 8.3 (8.1-8.5) | 8.3 (7.9-8.5) | 7.0 (6.9-7.0) | 2.1 (2.1-2.1) | 2.1 (2.1-2.1) | 12.2 (10.1-12.2) |
| stress | 16.3 (15.7-19.8) | 13.5 (13.4-14.6) | 12.8 (12.7-13.4) | 4.0 (3.8-4.3) | 4.1 (3.9-4.2) | 12.3 (11.3-13.0) |
| stress peak | 73 (58-111) | 14.4 (13.4-14.9) | 12.9 (12.7-13.8) | 4.1 (3.8-4.6) | 4.1 (3.9-4.3) | 13.8 (12.1-14.3) |
| flood | 74 (56-76) | 49 (47-49) | 54 (53-55) | 13.6 (10.9-14.3) | 15.0 (14.8-15.0) | 23.5 (23.0-24.2) |

Go runs 7 to 14 OS threads by default and 5 to 8 with `GOMAXPROCS=1`. Rust runs 2: the main thread and one tokio worker.

### CPU

| scenario | unit | go | go-tuned | go-tuned-p1 | rs | rs-tuned | rs-tuned-mi |
|---|---|---|---|---|---|---|---|
| real | % of 1 core | 0.40 (0.13-0.50) | 0.13 (0.10-0.35) | 0.12 (0.10-0.32) | 0.08 (0.05-0.13) | 0.12 (0.07-0.18) | 0.07 (0.07-0.20) |
| stress | % of 1 core | 33.7 (25.6-34.2) | 7.7 (6.3-11.3) | 6.2 (4.9-9.4) | 18.9 (17.4-21.2) | 4.9 (4.5-7.1) | 6.0 (4.0-6.9) |
| flood | CPU-s per GB | 233 (200-260) | 1.70 (1.60-2.17) | 1.56 (1.49-1.62) | 270 (262-286) | 0.94 (0.83-1.21) | 0.82 (0.53-0.95) |

### Stress latency (upstream write to client read, ms)

| | go | go-tuned | go-tuned-p1 | rs | rs-tuned | rs-tuned-mi |
|---|---|---|---|---|---|---|
| p50 | 0.83 (0.63-0.83) | 0.48 (0.40-0.57) | 0.45 (0.38-0.53) | 0.72 (0.70-0.76) | 0.46 (0.42-0.54) | 0.47 (0.40-0.57) |
| p99 | 19.9 (6.2-118) | 2.6 (1.0-8.3) | 2.0 (1.2-6.9) | 8.5 (4.3-11.4) | 4.3 (3.5-9.3) | 8.5 (3.0-9.1) |

Every stress run delivered about 1.5 million records per side with 0 sequence gaps, 0 malformed records, and 0 TCP disconnects.

### Flood throughput (MB/s to all clients)

| go | go-tuned | go-tuned-p1 | rs | rs-tuned | rs-tuned-mi |
|---|---|---|---|---|---|
| 4.2 (3.7-4.8) | 564 (432-607) | 620 (578-645) | 3.6 (3.4-3.6) | 1049 (808-1112) | 1124 (1020-1436) |

### Size and startup

| | go | rs |
|---|---|---|
| stripped binary | 6.3 MB | 2.2 MB |
| container image (alpine 3.22) | 23.3 MB | 17.6 MB |
| exec to web port open (`results/startup.json`) | 2 to 8 ms median | 1 to 2.5 ms median |

The startup numbers come from two sets of 15 runs on a busy host. Treat them as a ballpark.

### Raspberry Pi 4 (Home Assistant OS)

Two temporary local add-ons ran the `go` and `rs` binaries. Both were chained as clients of the production proxy, so they saw real bus traffic without opening a second EW11 session. Supervisor `ha apps stats` was sampled every 30 s for about 10 minutes (`results/pi-stats.jsonl`). Add-ons have no CPU limit, so Go used its default `GOMAXPROCS` of 4 there.

| | go | rs |
|---|---|---|
| container memory, median | 4.9 MB | 1.2 MB |
| CPU over the window | 1.75 s | 0.95 s |

This is one window per side, so treat it as a ballpark. The add-ons were removed afterwards.

## What limits each number

- **`flood`.** Every side ran its server at 92 to 98% of the 1-CPU limit, so the server CPU is the limiter. On the Rust sides the Python load generator also used about 98% of one core (`loadgen_cpu_s` 17.6 of 18 s). Rust throughput is therefore a lower bound. CPU-seconds per GB is the comparable figure.
- **`stress`.** The offered load is fixed. No side came close to the CPU limit, so CPU% measures cost, not capacity.
- **Profiling.** `profile.sh` traced `go-tuned` (default `GOMAXPROCS`) and `rs-tuned` under stress (`results/profile-stress.txt`). It was not run on `go-tuned-p1`. Tracing slows the server, and single untraced windows varied enough to reverse the CPU order between runs, so these windows explain where time goes, not which side is faster.
  - **Kernel time.** In all six untraced 10-second windows, kernel time exceeded user time on both sides.
  - **Client writes.** Both sides make one write system call per client per chunk: Go made 43,602 `write` calls and Rust made 37,450 `sendto` calls in 10 s.
  - **Go scheduling.** Go also made about 13,000 `futex` and 6,000 `nanosleep` calls in 10 s for goroutine scheduling. Rust made none.
- **Quadratic formatting.** In `go` and `rs`, the per-byte string rebuild is the limiter. Fixing it changes stress CPU by about 4× and flood throughput by over 100×.

## Caveats

- **WebSocket client in Go stress runs.** Go closed its WebSocket client in 2 of 15 Go stress runs (`go` run 3 and `go-tuned-p1` run 4, `ConnectionClosedError`, 0 messages). In those runs Go skipped WebSocket fan-out, so its stress CPU is slightly understated.
- **Flood warnings.** The `go-tuned` sides log `connection reset by peer` warnings in flood. They appear when the load generator closes its sockets after the measurement window. The Rust side does not log that case.
- **mimalloc.** mimalloc adds 6 to 10 MB of RSS. In flood it lowers CPU-s/GB a little, and in stress there is no measurable change. It also showed a shutdown hang that a signal-handling fix resolved.
- **VM cores.** The VM's cores are faster than the Pi 4's Cortex-A72. Use the ratios, not the absolute values. The Pi numbers are the only on-device data.
- **Topcoat scope.** Topcoat served static files, JSON, SSE, and WebSockets here. Its views, shards, and runtime were not exercised, because the UI is unchanged. Topcoat is early-stage, and its 0.11.0 API already differed from the `main` docs (`app_context`).
