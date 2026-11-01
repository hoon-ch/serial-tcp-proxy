# Go vs Rust (Topcoat) resource comparison

This branch adds `rust/`, a Rust reimplementation of the proxy whose web backend runs on [Topcoat](https://github.com/tokio-rs/topcoat) 0.11. It serves the same `internal/web/static` UI and passes the same black-box contract suite (`bench/contract/contract.mjs`, 42 checks, both auth modes) as the Go server. This page reports what each version costs to run.

## Verdict

- **Memory.** Rust uses about a quarter of the memory. Idle RSS is 1.9 MB against 7.7 MB. On the Raspberry Pi 4 the add-on container uses 1.2 MB against 4.9 MB.
- **CPU at real bus traffic.** There is no measurable difference. Both use about 0.1% of one core.
- **CPU under load.** With the same algorithm on both sides, Rust needs about 40% less CPU. At 5,000 records/s to 10 clients, CPU is 5.5% against 8.8%.
- **Latency.** There is no measurable difference.
- **Biggest single cost.** Go's `LogPacket` rebuilds the hex string once per byte, so its cost is quadratic in the chunk size. Fixing that one function cuts Go's CPU by 4× under stress and by 130× in the flood test. It matters more than the language choice.

For this installation, the switch would save about 3.7 MB of RAM on an 8 GB Pi. CPU stays near zero either way.

## Sides

| side | build |
|---|---|
| `go` | Go 1.25.4 as shipped in v1.3.3 (`CGO_ENABLED=0 -ldflags "-s -w"`) |
| `go-tuned` | `go` plus `go-tuned.patch`: linear hex formatting in `LogPacket` |
| `rs` | Rust 1.99.1, tokio, Topcoat 0.11, musl static, `lto = "fat"`, `codegen-units = 1`. It copies Go's quadratic formatting on purpose. |
| `rs-tuned` | `rs` with `--features linear-hex` |
| `rs-tuned-mi` | `rs-tuned` with `--features mimalloc` |

Both languages do the same per-packet work. Each server formats a log line for every chunk, keeps a 1000-line replay buffer, and fans out to TCP clients one by one with a 100 ms write timeout. WebSocket and SSE subscribers have the same channel sizes on both sides.

## Method

- **Environment.** Each run starts a mock EW11 (`upstream.py`), a server container capped at 1 CPU and 256 MiB, and a load generator (`loadgen.py`). Everything runs in an OrbStack linux/arm64 VM with 8 CPUs.
- **Run order.** `run.py` runs every side 5 times per scenario and rotates the side order between runs. Tables show the median and the min to max range.
- **Correctness checks.** Each run counts sequence gaps, malformed records, disconnects, WebSocket log messages, and proxy warnings.
- **Host load.** The host Mac was busy during the runs, with a 1-minute load average between 18 and 115. Rotating the order spreads that noise across all sides. It still widens the ranges.

The four scenarios:

- **`idle`.** The upstream is connected, sends nothing, and no clients are attached.
- **`real`.** Replays 60 s of live bus traffic (`data/bus-capture.jsonl`: 5 reads/s, 102 B/s) to 1 TCP client and 1 WebSocket client.
- **`stress`.** Sends 32-byte timestamped records at 5,000 records/s to 10 TCP clients and 1 WebSocket client.
- **`flood`.** The upstream writes as fast as it can, to 10 TCP clients.

## Results

Raw rows are in `results/20261101-230339/` (`idle`, `flood`) and `results/20261102-003511/` (`real`, `stress`).

### Memory (RSS, MB)

| scenario | go | go-tuned | rs | rs-tuned | rs-tuned-mi |
|---|---|---|---|---|---|
| idle | 7.7 (7.7-8.1) | 7.6 (7.3-7.7) | 1.9 (1.9-2.0) | 1.9 (1.9-2.0) | 8.1 (8.1-8.1) |
| real | 8.2 (8.0-8.4) | 8.0 (7.9-8.2) | 2.1 (2.1-2.1) | 2.1 (2.1-2.1) | 12.2 (10.1-12.2) |
| stress | 17.3 (16.9-17.7) | 13.9 (13.3-14.3) | 4.1 (4.0-4.2) | 4.1 (3.9-4.2) | 12.6 (11.6-13.1) |
| stress peak | 84 (59-100) | 14.3 (13.4-14.7) | 4.4 (4.1-5.9) | 4.2 (4.1-4.5) | 13.6 (13.4-14.2) |
| flood | 60 (49-82) | 48 (47-48) | 13 (12-14) | 15 (15-15) | 24 (22-24) |

Go runs 8 to 13 OS threads. Rust runs 2: the main thread and one tokio worker, because the CPU limit is 1.

### CPU

| scenario | unit | go | go-tuned | rs | rs-tuned | rs-tuned-mi |
|---|---|---|---|---|---|---|
| real | % of 1 core | 0.12 (0.10-0.15) | 0.12 (0.12-0.17) | 0.07 (0.07-0.25) | 0.08 (0.03-0.20) | 0.07 (0.05-0.10) |
| stress | % of 1 core | 34.6 (28.0-35.7) | 8.8 (8.5-11.9) | 20.1 (17.9-29.5) | 5.5 (4.3-6.7) | 5.4 (4.1-6.7) |
| flood | CPU-s per GB | 252 (248-331) | 1.92 (1.65-2.24) | 317 (283-321) | 1.18 (0.89-1.73) | 1.07 (0.98-1.17) |

In `real`, the ranges overlap, so there is no measurable difference. In `stress`, the `go-tuned` and `rs-tuned` ranges do not overlap. In `flood`, `go-tuned` and `rs-tuned` overlap between 1.65 and 1.73, while `rs-tuned-mi` stays below `go-tuned` in every run.

### Stress latency (upstream write to client read, ms)

| | go | go-tuned | rs | rs-tuned | rs-tuned-mi |
|---|---|---|---|---|---|
| p50 | 0.93 (0.68-0.96) | 0.57 (0.46-0.75) | 0.84 (0.75-1.19) | 0.53 (0.44-0.67) | 0.59 (0.44-0.68) |
| p99 | 14.1 (7.6-69.1) | 5.5 (1.9-6.1) | 9.9 (9.4-72.7) | 5.0 (2.8-7.8) | 4.5 (3.5-8.4) |

Every stress run delivered all 1.5 million records per side with 0 sequence gaps, 0 malformed records, and 0 TCP disconnects.

### Flood throughput (MB/s to all clients)

| go | go-tuned | rs | rs-tuned | rs-tuned-mi |
|---|---|---|---|---|
| 3.8 (3.0-4.0) | 493 (415-586) | 3.0 (3.0-3.4) | 798 (548-1089) | 897 (824-966) |

### Size and startup

| | go | rs |
|---|---|---|
| stripped binary | 6.3 MB | 2.2 MB |
| container image (alpine 3.22) | 23.3 MB | 17.6 MB |
| exec to web port open (`results/startup.json`) | 2 to 8 ms median | 1 to 2.5 ms median |

Startup numbers come from two sets of 15 runs on a busy host. Treat them as a ballpark.

### Raspberry Pi 4 (Home Assistant OS)

Two temporary local add-ons ran the `go` and `rs` binaries. Both were chained as clients of the production proxy, so they saw real bus traffic without opening a second EW11 session. Supervisor `ha apps stats` was sampled every 30 s for about 10 minutes (`results/pi-stats.jsonl`).

| | go | rs |
|---|---|---|
| container memory, median | 4.9 MB | 1.2 MB |
| CPU over the window | 1.75 s | 0.95 s |

This is one window per side, so treat it as a ballpark. The add-ons were removed afterwards.

## What limits each number

- **`flood`.** Every side ran its server at 94 to 98% of the 1-CPU limit, so the server CPU is the limiter. On the Rust sides the Python load generator also used about 96% of one core. Rust throughput is therefore a lower bound. CPU-seconds per GB is the comparable figure.
- **`stress`.** The offered load is fixed. No side came close to the CPU limit, so CPU% measures cost, not capacity. `profile.sh` splits that cost (`results/profile-stress.txt`).
  - Both sides spend most of their system calls writing to client sockets. Go made 43,602 `write` calls in 10 s and Rust made 37,450 `sendto` calls.
  - Go also makes about 13,000 `futex` and 6,000 `nanosleep` calls per 10 s for goroutine scheduling. Rust does not make these calls.
  - Rust under musl makes about 900 `mmap`/`munmap` pairs per 10 s from the musl allocator. mimalloc removes them but adds 6 to 10 MB of RSS without a measurable CPU gain.
- **`go` and `rs` (quadratic formatting).** `go` → `go-tuned` and `rs` → `rs-tuned` move the stress CPU by 4× and the flood throughput by 130× or more. That one function is the limiter for both as-shipped variants.

## Caveats

- **WebSocket client in Go stress runs.** Go closed its WebSocket client in 3 of 10 stress runs (`ConnectionClosedError`, 0 messages). In those runs Go skipped WebSocket fan-out, so its stress CPU is slightly understated.
- **VM cores.** The VM's cores are faster than the Pi 4's Cortex-A72. Use the ratios, not the absolute values. The Pi numbers are the only on-device data.
- **Topcoat scope.** Topcoat served static files, JSON, SSE, and WebSockets here. Its views, shards, and runtime were not exercised, because the UI is unchanged. Topcoat is early-stage, and its 0.11.0 API already differed from the `main` docs (`app_context`).
- **Binaries after a fix.** The throughput runs used Rust binaries from before one shutdown fix. That fix registers the SIGTERM handler before the ports open. It does not touch the packet path. `startup.json` used the fixed binaries.
