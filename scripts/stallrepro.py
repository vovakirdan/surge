#!/usr/bin/env python3
"""Reconstructed stallrepro.py-style sustained load (Epic 8 Task 11, RV2-DEBT-015).

Shape per the DEBT entry: 1024 persistent connections, sustained request loop,
no think time by default. Time-bounded, live tail detection: any request slower
than --stall-threshold writes an immediate STALL line to stderr and touches
--stall-flag so a watcher can snapshot ss/proc and SIGUSR1 the server mid-stall.
"""
import argparse
import socket
import statistics
import sys
import threading
import time

RESPONSE = b'VALUE {"v":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}\n'
PAYLOAD = b"x"


def parse_args():
    p = argparse.ArgumentParser()
    p.add_argument("port", type=int)
    p.add_argument("--connections", type=int, default=1024)
    p.add_argument("--duration", type=float, default=90.0)
    p.add_argument("--stall-threshold", type=float, default=1.0)
    p.add_argument("--sock-timeout", type=float, default=30.0)
    p.add_argument("--ramp", choices=["burst", "staggered"], default="burst")
    p.add_argument("--ramp-interval", type=float, default=0.002)
    p.add_argument("--pace", type=float, default=0.0,
                   help="sleep seconds between requests per conn (0 = no think time)")
    p.add_argument("--stall-flag", default="/tmp/stallrepro.flag")
    return p.parse_args()


def recv_exact(sock, size):
    chunks = []
    remaining = size
    while remaining:
        chunk = sock.recv(remaining)
        if not chunk:
            raise RuntimeError("unexpected EOF")
        chunks.append(chunk)
        remaining -= len(chunk)
    return b"".join(chunks)


class Stats:
    def __init__(self):
        self.lock = threading.Lock()
        self.samples = []
        self.stalls = []  # (ts, conn, latency)
        self.errors = []

    def add(self, lat_us):
        with self.lock:
            self.samples.append(lat_us)

    def stall(self, conn_id, lat_s):
        with self.lock:
            self.stalls.append((time.time(), conn_id, lat_s))

    def error(self, conn_id, exc):
        with self.lock:
            self.errors.append((time.time(), conn_id, repr(exc)))


def worker(conn_id, sock, deadline, args, stats, stop_event):
    local_port = sock.getsockname()[1]
    while time.monotonic() < deadline and not stop_event.is_set():
        t0 = time.perf_counter()
        try:
            sock.sendall(PAYLOAD)
            got = recv_exact(sock, len(RESPONSE))
        except socket.timeout:
            lat = time.perf_counter() - t0
            stats.stall(conn_id, lat)
            print(f"STALL(timeout) ts={time.time():.3f} conn={conn_id} lport={local_port} "
                  f"lat={lat:.3f}s", file=sys.stderr, flush=True)
            _touch(args.stall_flag)
            stats.error(conn_id, "socket.timeout")
            return
        except OSError as exc:
            stats.error(conn_id, exc)
            return
        lat = time.perf_counter() - t0
        if got != RESPONSE:
            stats.error(conn_id, RuntimeError(f"bad response {got!r}"))
            return
        stats.add(lat * 1e6)
        if lat >= args.stall_threshold:
            stats.stall(conn_id, lat)
            print(f"STALL ts={time.time():.3f} conn={conn_id} lport={local_port} "
                  f"lat={lat:.3f}s", file=sys.stderr, flush=True)
            _touch(args.stall_flag)
        if args.pace > 0:
            time.sleep(args.pace)


def _touch(path):
    try:
        with open(path, "a") as f:
            f.write(f"{time.time():.3f}\n")
    except OSError:
        pass


def percentile(values, pct):
    if not values:
        return 0.0
    idx = int((len(values) - 1) * pct / 100.0)
    return sorted(values)[idx]


def main():
    args = parse_args()
    stats = Stats()
    stop_event = threading.Event()
    socks = []
    t_connect0 = time.monotonic()
    for i in range(args.connections):
        deadline = time.monotonic() + 10.0
        last = None
        sock = None
        while time.monotonic() < deadline:
            try:
                sock = socket.create_connection(("127.0.0.1", args.port), timeout=2.0)
                sock.settimeout(args.sock_timeout)
                break
            except OSError as exc:
                last = exc
                time.sleep(0.01)
        if sock is None:
            print(f"connect {i} failed: {last}", file=sys.stderr)
            sys.exit(2)
        socks.append(sock)
        if args.ramp == "staggered":
            time.sleep(args.ramp_interval)
    connect_s = time.monotonic() - t_connect0
    print(f"connected {len(socks)} conns in {connect_s:.2f}s ramp={args.ramp}", flush=True)

    deadline = time.monotonic() + args.duration
    threads = []
    for i, sock in enumerate(socks):
        t = threading.Thread(target=worker, args=(i, sock, deadline, args, stats, stop_event),
                             daemon=True)
        threads.append(t)
        t.start()
    for t in threads:
        t.join(timeout=args.duration + args.sock_timeout + 30)
    stop_event.set()
    for sock in socks:
        try:
            sock.close()
        except OSError:
            pass

    s = stats.samples
    print(f"requests={len(s)} errors={len(stats.errors)} stalls_ge_{args.stall_threshold}s={len(stats.stalls)}")
    if s:
        print(f"avg_us={statistics.fmean(s):.1f} p50_us={percentile(s, 50):.1f} "
              f"p95_us={percentile(s, 95):.1f} p99_us={percentile(s, 99):.1f} "
              f"p999_us={percentile(s, 99.9):.1f} max_us={max(s):.1f}")
    tail1 = sum(1 for v in s if v >= 1e6)
    tail5 = sum(1 for v in s if v >= 5e6)
    tail10 = sum(1 for v in s if v >= 10e6)
    print(f"tails: in_samples >=1s={tail1} >=5s={tail5} >=10s={tail10}")
    for ts, conn, lat in stats.stalls[:40]:
        print(f"  stall conn={conn} lat={lat:.3f}s ts={ts:.3f}")
    if stats.errors:
        for ts, conn, err in stats.errors[:10]:
            print(f"  error conn={conn} {err} ts={ts:.3f}")
    sys.exit(1 if (tail10 or any(x[2] >= 10.0 for x in stats.stalls)) else 0)


if __name__ == "__main__":
    main()
