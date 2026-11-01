"""Run the Go and Rust proxies through the same scenarios, alternating sides, and summarize.

Usage: python3 bench/run.py [--runs 5] [--scenarios idle,real,stress,flood] [--sides go,rs]
Needs Docker. Binaries come from bench/bin/<side>/server (linux/arm64, static).
Writes bench/results/<stamp>/raw.jsonl and summary.md.
"""
import argparse
import os
import json
import pathlib
import statistics
import subprocess
import time

BENCH = pathlib.Path(__file__).resolve().parent
NET = "stpbench"
TOOLS = "stp-bench-tools"
CPUS, MEMORY = "1", "256m"

SCENARIOS = {
    "idle": {"upstream": ["idle"], "tcp": 0, "ws": 0, "warmup": 10, "seconds": 20, "parse": "bytes"},
    "real": {"upstream": ["replay", "/bench/data/bus-capture.jsonl"], "tcp": 1, "ws": 1, "warmup": 5, "seconds": 60, "parse": "bytes"},
    "stress": {"upstream": ["records", "5000"], "tcp": 10, "ws": 1, "warmup": 5, "seconds": 30, "parse": "records"},
    "flood": {"upstream": ["flood"], "tcp": 10, "ws": 0, "warmup": 3, "seconds": 15, "parse": "bytes"},
}


def sh(*args, check=True, capture=True):
    result = subprocess.run(args, check=check, capture_output=capture, text=True)
    return result.stdout.strip() if capture else ""


def docker_rm(*names):
    subprocess.run(["docker", "rm", "-f", *names], capture_output=True)


def setup():
    if subprocess.run(["docker", "network", "inspect", NET], capture_output=True).returncode:
        sh("docker", "network", "create", NET)
    sh("docker", "build", "-q", "-t", TOOLS, "-f", str(BENCH / "tools.Dockerfile"), str(BENCH))
    for side_dir in sorted((BENCH / "bin").iterdir()):
        if (side_dir / "server").exists():
            sh("docker", "build", "-q", "-t", f"stp-proxy-{side_dir.name}", "-f", str(BENCH / "proxy.Dockerfile"), str(side_dir))


def proc_sample(container):
    out = sh("docker", "exec", container, "sh", "-c", "cat /proc/1/stat; grep -E 'VmRSS|VmHWM|Threads' /proc/1/status; getconf CLK_TCK")
    lines = out.splitlines()
    fields = lines[0].rsplit(")", 1)[1].split()
    ticks = int(fields[11]) + int(fields[12])
    status = {l.split(":")[0]: int(l.split()[1]) for l in lines[1:-1]}
    return {"cpu_s": ticks / int(lines[-1]), "rss_kb": status["VmRSS"], "hwm_kb": status["VmHWM"], "threads": status["Threads"]}


def wait_healthy(timeout=20):
    probe = "import urllib.request;urllib.request.urlopen('http://stpb-proxy:18080/api/health',timeout=1)"
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if subprocess.run(["docker", "exec", "stpb-up", "python3", "-c", probe], capture_output=True).returncode == 0:
            return True
        time.sleep(0.05)
    return False


def run_one(side, name):
    sc = SCENARIOS[name]
    docker_rm("stpb-up", "stpb-proxy", "stpb-load")
    sh("docker", "run", "-d", "--name", "stpb-up", "--network", NET, "-v", f"{BENCH}:/bench:ro", TOOLS,
       "python3", "-u", "/bench/upstream.py", *sc["upstream"])
    t0 = time.monotonic()
    env_file = BENCH / "bin" / side / "env"
    extra = ["--env-file", str(env_file)] if env_file.exists() else []
    sh("docker", "run", "-d", "--name", "stpb-proxy", "--network", NET, "--cpus", CPUS, "--memory", MEMORY, *extra,
       "-e", "UPSTREAM_HOST=stpb-up", "-e", "UPSTREAM_PORT=8899", "-e", "LISTEN_PORT=18899", "-e", "WEB_PORT=18080",
       "-e", "MAX_CLIENTS=20", "-e", "LOG_PACKETS=false", f"stp-proxy-{side}")
    if not wait_healthy():
        raise RuntimeError(f"{side} did not become healthy: {sh('docker', 'logs', 'stpb-proxy', check=False)}")
    startup_s = time.monotonic() - t0
    load = None
    if sc["tcp"] or sc["ws"]:
        load = subprocess.Popen(["docker", "run", "--name", "stpb-load", "--network", NET, "--pid", "container:stpb-proxy",
                                 "-v", f"{BENCH}:/bench:ro", TOOLS,
                                 "python3", "/bench/loadgen.py", "stpb-proxy", "18899", "18080", str(sc["tcp"]), str(sc["ws"]),
                                 str(sc["seconds"]), str(sc["warmup"]), sc["parse"]], stdout=subprocess.PIPE, text=True)
    time.sleep(sc["warmup"] + 0.5)
    before = proc_sample("stpb-proxy")
    w0 = time.monotonic()
    rss = []
    while time.monotonic() - w0 < sc["seconds"] - 1:
        rss.append(proc_sample("stpb-proxy")["rss_kb"])
        time.sleep(1)
    after = proc_sample("stpb-proxy")
    wall = time.monotonic() - w0
    result = {"side": side, "scenario": name, "startup_s": round(startup_s, 3),
              "cpu_pct": round(100 * (after["cpu_s"] - before["cpu_s"]) / wall, 2),
              "rss_median_kb": int(statistics.median(rss)), "rss_hwm_kb": after["hwm_kb"], "threads": after["threads"],
              "host_load1": round(os.getloadavg()[0], 1)}
    if load:
        out, _ = load.communicate(timeout=sc["seconds"] + 60)
        result.update(json.loads(out.strip().splitlines()[-1]))
        # loadgen shares the proxy's PID namespace and reads its CPU over the same window it counts bytes in.
        result["cpu_pct"] = round(100 * result["proxy_cpu_s"] / result["window_s"], 2)
        result["tcp_mb_s"] = round(result["tcp_bytes"] / result["window_s"] / 1e6, 3)
        if result["tcp_mb_s"] > 1:
            result["cpu_s_per_gb"] = round(result["proxy_cpu_s"] / (result["tcp_bytes"] / 1e9), 3)
    proxy_logs = sh("docker", "logs", "stpb-proxy", check=False)
    result["proxy_warnings"] = sum(1 for l in proxy_logs.splitlines() if "WARN" in l or "ERROR" in l)
    docker_rm("stpb-up", "stpb-proxy", "stpb-load")
    return result


METRICS = ["startup_s", "cpu_pct", "rss_median_kb", "rss_hwm_kb", "threads", "tcp_mb_s", "cpu_s_per_gb", "tcp_records",
           "lat_p50_ms", "lat_p99_ms", "lat_max_ms", "ws_log_messages", "seq_gaps", "bad_records", "disconnects",
           "proxy_warnings", "loadgen_cpu_s", "host_load1"]


def summarize(rows, sides, scenarios):
    lines = ["| scenario | metric | " + " | ".join(f"{s} median (min-max)" for s in sides) + " |",
             "|---|---|" + "---|" * len(sides)]
    for name in scenarios:
        for metric in METRICS:
            cells = []
            for side in sides:
                vals = [r[metric] for r in rows if r["side"] == side and r["scenario"] == name and r.get(metric) is not None]
                cells.append(f"{statistics.median(vals):g} ({min(vals):g}-{max(vals):g})" if vals else "-")
            if any(c != "-" for c in cells):
                lines.append(f"| {name} | {metric} | " + " | ".join(cells) + " |")
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--runs", type=int, default=5)
    ap.add_argument("--scenarios", default="idle,real,stress,flood")
    ap.add_argument("--sides", default="go,rs")
    args = ap.parse_args()
    sides, scenarios = args.sides.split(","), args.scenarios.split(",")
    setup()
    out_dir = BENCH / "results" / time.strftime("%Y%m%d-%H%M%S")
    out_dir.mkdir(parents=True)
    rows = []
    with open(out_dir / "raw.jsonl", "w") as raw:
        for name in scenarios:
            for i in range(args.runs):
                order = sides[i % len(sides):] + sides[:i % len(sides)]
                for side in order:
                    row = run_one(side, name)
                    row["run"] = i
                    rows.append(row)
                    raw.write(json.dumps(row) + "\n")
                    raw.flush()
                    print(json.dumps(row), flush=True)
    summary = summarize(rows, sides, scenarios)
    (out_dir / "summary.md").write_text(summary + "\n")
    print(summary)
    print(f"results: {out_dir}")


if __name__ == "__main__":
    main()
