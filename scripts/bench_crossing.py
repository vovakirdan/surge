#!/usr/bin/env python3
"""Native crossing throughput/latency benchmark.

Measures the placement task crossing verticals on the LLVM/native backend:
  - spawn-await: N `spawn on distributed` + `await()` round trips
  - immediate-on: N immediate `on distributed` round trips
  - on-ch-pair: N anchored send + anchored recv block pairs over one
    `channel_on` channel (two execute/reply round trips per iteration)
  - share-mint: N sibling-lease mints on one channel (share round trips)

Each probe owns its timeout (subprocess-level, reported with probe/mode on
expiry) instead of relying on an outer wrapper. This is a correctness and
liveness-cost baseline, not a line-rate scaling claim: one process, one
round-trip chain, no batching.

Usage: python3 scripts/bench_crossing.py [--shards 1,2,8] [--iterations 2000]
       [--probe-timeout 120]
"""

import argparse
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import time

SPAWN_AWAIT_SOURCE = """
async fn probe(iterations: int) -> int {
    let mut i: int = 0;
    while i < iterations {
        let task: far Task<int> = spawn on distributed {
            ret 1;
        };
        let v: int = compare task.await() {
            Success(x) => x;
            Cancelled() => 0;
        };
        if v != 1 {
            return 1;
        }
        i = i + 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn probe(%ITER%);
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 2;
    };
}
"""

IMMEDIATE_ON_SOURCE = """
async fn probe(iterations: int) -> int {
    let mut i: int = 0;
    while i < iterations {
        let v: int = compare on distributed {
            ret 1;
        } {
            Success(x) => x;
            Cancelled() => 0;
        };
        if v != 1 {
            return 1;
        }
        i = i + 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn probe(%ITER%);
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 2;
    };
}
"""

ON_CH_PAIR_SOURCE = """
async fn probe(iterations: int) -> int {
    let ch: far Channel<int> = channel_on::<int>(distributed, 4);
    let mut i: int = 0;
    while i < iterations {
        let sent: TaskResult<nothing> = on ch { ch.send(1); ret nothing; };
        let ok: int = compare sent { Success(_) => 1; Cancelled() => 0; };
        if ok != 1 {
            return 1;
        }
        let got: TaskResult<int> = on ch {
            let v: Option<int> = ch.recv();
            ret compare v { Some(x) => x; nothing => 0; };
        };
        let value: int = compare got { Success(x) => x; Cancelled() => 0; };
        if value != 1 {
            return 1;
        }
        i = i + 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn probe(%ITER%);
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 2;
    };
}
"""

SHARE_MINT_SOURCE = """
async fn probe(iterations: int) -> int {
    let ch: far Channel<int> = channel_on::<int>(distributed, 4);
    let mut i: int = 0;
    while i < iterations {
        let sib: far Channel<int> = ch.share();
        let _ = sib;
        i = i + 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn probe(%ITER%);
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 2;
    };
}
"""

SELECT_READY_SOURCE = """
async fn probe(iterations: int) -> int {
    let a: far Channel<int> = channel_on::<int>(distributed, 4);
    let b: far Channel<int> = channel_on::<int>(distributed, 4);
    let mut i: int = 0;
    while i < iterations {
        let fed: TaskResult<nothing> = on b { b.send(1); ret nothing; };
        let ok: int = compare fed { Success(_) => 1; Cancelled() => 0; };
        if ok != 1 {
            return 1;
        }
        let w: int = select { a.recv() => 10; b.recv() => 20; };
        if w != 20 {
            return 1;
        }
        i = i + 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn probe(%ITER%);
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 2;
    };
}
"""

MOVE_CAPTURE_SOURCE = """
@shard_movable
type Job = { id: int, weight: int };

fn describe(j: own Job) -> int { return j.id * 100 + j.weight; }

async fn probe(iterations: int) -> int {
    let mut i: int = 0;
    while i < iterations {
        let j: own Job = own Job{ id: 4, weight: 2 };
        let got: TaskResult<int> = on distributed { ret describe(own j); };
        let value: int = compare got { Success(x) => x; Cancelled() => 0; };
        if value != 402 {
            return 1;
        }
        i = i + 1;
    }
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn probe(%ITER%);
    return compare task.await() {
        Success(code) => code;
        Cancelled() => 2;
    };
}
"""

PROBES = {
    "spawn-await": SPAWN_AWAIT_SOURCE,
    "immediate-on": IMMEDIATE_ON_SOURCE,
    # One iteration = one sibling-lease mint round trip (leases accumulate
    # on one entry for the run, so this also exercises the lease-table walk
    # at growing depth).
    "share-mint": SHARE_MINT_SOURCE,
    # One iteration = an anchored send block + an anchored recv block (two
    # execute/reply round trips through the owner's local channel lane).
    "on-ch-pair": ON_CH_PAIR_SOURCE,
    # One iteration = one anchored feed + one remote select deciding on the
    # ready arm (two execute/reply round trips; the select never parks).
    "select-ready": SELECT_READY_SOURCE,
    # One iteration = one immediate on round trip carrying a MOVED owned
    # @shard_movable capture (the migration vertical) — compare against
    # immediate-on (plain-copy captures) for the capture-move overhead.
    "move-capture": MOVE_CAPTURE_SOURCE,
}


def repo_root() -> pathlib.Path:
    return pathlib.Path(__file__).resolve().parent.parent


def build_probe(root: pathlib.Path, name: str, source: str, iterations: int,
                build_timeout: int) -> pathlib.Path:
    # The module scan requires sources inside the repository tree.
    workdir = root / "target" / "debug" / ".bench" / f"crossing-{name}-{os.getpid()}"
    workdir.mkdir(parents=True, exist_ok=True)
    src = workdir / f"bench_{name.replace('-', '_')}.sg"
    src.write_text(source.replace("%ITER%", str(iterations)))
    env = dict(os.environ, SURGE_STDLIB=str(root))
    try:
        subprocess.run(
            ["go", "run", "./cmd/surge", "build", str(src), "--backend", "llvm"],
            cwd=root, env=env, check=True, timeout=build_timeout,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        )
    except subprocess.TimeoutExpired:
        sys.exit(f"BUILD TIMEOUT probe={name} after {build_timeout}s")
    except subprocess.CalledProcessError as err:
        sys.exit(f"BUILD FAILED probe={name}:\n{err.stdout.decode(errors='replace')}")
    binary = root / "target" / "debug" / src.stem
    if not binary.exists():
        sys.exit(f"BUILD produced no binary for probe={name}: {binary}")
    return binary


def run_probe(root: pathlib.Path, name: str, binary: pathlib.Path, shards: int,
              iterations: int, probe_timeout: int) -> float:
    env = dict(
        os.environ,
        SURGE_STDLIB=str(root),
        SURGE_SHARDS=str(shards),
        SURGE_THREADS=str(shards),
    )
    started = time.monotonic()
    try:
        proc = subprocess.run(
            [str(binary)], env=env, timeout=probe_timeout,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        )
    except subprocess.TimeoutExpired:
        sys.exit(f"PROBE TIMEOUT probe={name} shards={shards} after {probe_timeout}s")
    elapsed = time.monotonic() - started
    if proc.returncode != 0:
        sys.exit(
            f"PROBE FAILED probe={name} shards={shards} exit={proc.returncode}:\n"
            f"{proc.stdout.decode(errors='replace')}"
        )
    return elapsed


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--shards", default="1,2,8")
    parser.add_argument("--iterations", type=int, default=2000)
    parser.add_argument("--probe-timeout", type=int, default=120)
    parser.add_argument("--build-timeout", type=int, default=300)
    args = parser.parse_args()

    root = repo_root()
    shard_counts = [int(v) for v in args.shards.split(",") if v]
    binaries = {}
    for name, source in PROBES.items():
        binaries[name] = build_probe(root, name, source, args.iterations,
                                     args.build_timeout)

    print(f"iterations per probe: {args.iterations}")
    print(f"{'probe':<14} {'shards':>6} {'seconds':>9} {'rt/sec':>10} {'us/rt':>9}")
    for name, binary in binaries.items():
        for shards in shard_counts:
            elapsed = run_probe(root, name, binary, shards, args.iterations,
                                args.probe_timeout)
            per_sec = args.iterations / elapsed if elapsed > 0 else 0.0
            micros = (elapsed / args.iterations) * 1e6
            print(f"{name:<14} {shards:>6} {elapsed:>9.3f} {per_sec:>10.0f} {micros:>9.1f}")
    for binary in binaries.values():
        try:
            binary.unlink()
        except OSError:
            pass
    shutil.rmtree(root / "target" / "debug" / ".bench", ignore_errors=True)


if __name__ == "__main__":
    main()
