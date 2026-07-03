#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
report="${SURGE_HEAP_BENCH_REPORT:-$root/build/benchmarks/runtime-v2-task08-native-heap-current.md}"
modes="${SURGE_HEAP_BENCH_THREADS:-1 2 4 8}"
probes="${SURGE_HEAP_BENCH_PROBES:-empty_loop serial_alloc_free serial_realloc heap_stats_poll concurrent_alloc_free}"
serial_iterations="${SURGE_HEAP_BENCH_SERIAL_ITERATIONS:-100000}"
realloc_iterations="${SURGE_HEAP_BENCH_REALLOC_ITERATIONS:-50000}"
stats_iterations="${SURGE_HEAP_BENCH_STATS_ITERATIONS:-10000}"
concurrent_rounds="${SURGE_HEAP_BENCH_CONCURRENT_ROUNDS:-5000}"
probe_timeout_seconds="${SURGE_HEAP_BENCH_PROBE_TIMEOUT_SECONDS:-30}"
surge="${SURGE:-$root/surge}"

fail() {
	echo "bench_native_heap_accounting: $*" >&2
	exit 1
}

validate_positive_int() {
	local name="$1"
	local value="$2"
	if [[ ! "$value" =~ ^[1-9][0-9]*$ ]]; then
		fail "$name must be a positive integer, got: $value"
	fi
}

validate_probe() {
	case "$1" in
		empty_loop | serial_alloc_free | serial_realloc | heap_stats_poll | concurrent_alloc_free)
			return 0
			;;
		*)
			fail "unknown probe in SURGE_HEAP_BENCH_PROBES: $1"
			;;
	esac
}

if [[ ! -x "$surge" ]]; then
	surge="$(command -v surge || true)"
fi
[[ -n "$surge" && -x "$surge" ]] || fail "surge binary not found; run 'make build' or set SURGE=/path/to/surge"
command -v timeout >/dev/null || fail "timeout command not found"

validate_positive_int "SURGE_HEAP_BENCH_SERIAL_ITERATIONS" "$serial_iterations"
validate_positive_int "SURGE_HEAP_BENCH_REALLOC_ITERATIONS" "$realloc_iterations"
validate_positive_int "SURGE_HEAP_BENCH_STATS_ITERATIONS" "$stats_iterations"
validate_positive_int "SURGE_HEAP_BENCH_CONCURRENT_ROUNDS" "$concurrent_rounds"
validate_positive_int "SURGE_HEAP_BENCH_PROBE_TIMEOUT_SECONDS" "$probe_timeout_seconds"

for mode in $modes; do
	validate_positive_int "SURGE_HEAP_BENCH_THREADS item" "$mode"
done
for probe in $probes; do
	validate_probe "$probe"
done

export SURGE_STDLIB="${SURGE_STDLIB:-$root}"

mkdir -p "$root/build/tmp"
fixture="$(mktemp -d "$root/build/tmp/native_heap_accounting_bench_XXXXXX")"
build_log="$(mktemp)"
trap 'rm -rf "$fixture"; rm -f "$build_log"' EXIT

cat >"$fixture/surge.toml" <<'EOF'
[package]
name = "native_heap_accounting_bench"
root = "."
version = "0.1.0"

[run]
main = "main.sg"
EOF

cat >"$fixture/main.sg" <<EOF
pragma module;

import stdlib/time as time;

fn elapsed_us(start: time.Duration) -> int64 {
    let now = time.monotonic_now();
    let elapsed = now.sub(start);
    return elapsed.as_micros();
}

fn print_row(name: string, iterations: int, total_us: int64, before: HeapStats, after: HeapStats) -> nothing {
    let ns_op: int64 = (total_us * (1000):int64) / (iterations to int64);
    let alloc_delta: uint = after.alloc_count - before.alloc_count;
    let free_delta: uint = after.free_count - before.free_count;
    let live_delta: uint = after.live_blocks - before.live_blocks;
    let byte_delta: uint = after.live_bytes - before.live_bytes;
    print("| " + name
        + " | " + (iterations to string)
        + " | " + (total_us to string)
        + " | " + (ns_op to string)
        + " | " + (alloc_delta to string)
        + " | " + (free_delta to string)
        + " | " + (live_delta to string)
        + " | " + (byte_delta to string)
        + " |");
    return nothing;
}

fn bench_empty_loop(iterations: int) -> nothing {
    let before: HeapStats = rt_heap_stats();
    let start = time.monotonic_now();
    let mut sum: int = 0;
    let mut i: int = 0;
    while i < iterations {
        sum = sum + i;
        i = i + 1;
    }
    let total_us = elapsed_us(start);
    let after: HeapStats = rt_heap_stats();
    if sum < 0 {
        print("impossible");
    }
    print_row("empty_loop", iterations, total_us, before, after);
    return nothing;
}

fn bench_serial_alloc_free(iterations: int) -> nothing {
    let before: HeapStats = rt_heap_stats();
    let start = time.monotonic_now();
    let mut i: int = 0;
    while i < iterations {
        let p = rt_alloc(32:uint, 1:uint);
        rt_free(p, 32:uint, 1:uint);
        i = i + 1;
    }
    let total_us = elapsed_us(start);
    let after: HeapStats = rt_heap_stats();
    print_row("serial_alloc_free", iterations, total_us, before, after);
    return nothing;
}

fn bench_serial_realloc(iterations: int) -> nothing {
    let before: HeapStats = rt_heap_stats();
    let start = time.monotonic_now();
    let mut i: int = 0;
    while i < iterations {
        let p0 = rt_alloc(16:uint, 1:uint);
        let p1 = rt_realloc(p0, 16:uint, 64:uint, 1:uint);
        rt_free(p1, 64:uint, 1:uint);
        i = i + 1;
    }
    let total_us = elapsed_us(start);
    let after: HeapStats = rt_heap_stats();
    print_row("serial_realloc", iterations, total_us, before, after);
    return nothing;
}

fn bench_heap_stats_poll(iterations: int) -> nothing {
    let before: HeapStats = rt_heap_stats();
    let start = time.monotonic_now();
    let mut total: uint = 0:uint;
    let mut i: int = 0;
    while i < iterations {
        let stats: HeapStats = rt_heap_stats();
        total = total + stats.alloc_count;
        i = i + 1;
    }
    let total_us = elapsed_us(start);
    let after: HeapStats = rt_heap_stats();
    if total == 0:uint {
        print("impossible");
    }
    print_row("heap_stats_poll", iterations, total_us, before, after);
    return nothing;
}

async fn alloc_worker(rounds: int, size: uint) -> int {
    let mut i: int = 0;
    while i < rounds {
        let p = rt_alloc(size, 1:uint);
        rt_free(p, size, 1:uint);
        checkpoint().await();
        i = i + 1;
    }
    return rounds;
}

async fn bench_concurrent_alloc_free(rounds: int) -> nothing {
    let workers: uint = rt_worker_count();
    let mut task_count_u: uint = workers;
    if task_count_u > 8:uint {
        task_count_u = 8:uint;
    }
    let task_count: int = task_count_u to int;
    let mut tasks: Task<int>[] = Array::<Task<int>>::with_len(task_count_u);
    let before: HeapStats = rt_heap_stats();
    let start = time.monotonic_now();
    let mut i: int = 0;
    while i < task_count {
        tasks[i] = spawn alloc_worker(rounds, 32:uint);
        i = i + 1;
    }
    let mut done: int = 0;
    for task in tasks {
        compare task.await() {
            Success(v) => {
                done = done + v;
            }
            Cancelled() => {
                print("cancelled");
            }
        };
    }
    let total_us = elapsed_us(start);
    let after: HeapStats = rt_heap_stats();
    if done != rounds * task_count {
        print("incomplete");
    }
    print_row("concurrent_alloc_free", done, total_us, before, after);
    return nothing;
}

@entrypoint("argv")
fn main(probe: string = "all") -> int {
    let serial_iterations: int = $serial_iterations;
    let realloc_iterations: int = $realloc_iterations;
    let stats_iterations: int = $stats_iterations;
    let concurrent_rounds: int = $concurrent_rounds;
    if probe == "all" {
        bench_empty_loop(serial_iterations);
        bench_serial_alloc_free(serial_iterations);
        bench_serial_realloc(realloc_iterations);
        bench_heap_stats_poll(stats_iterations);
        let _ = bench_concurrent_alloc_free(concurrent_rounds).await();
        return 0;
    }
    if probe == "empty_loop" {
        bench_empty_loop(serial_iterations);
        return 0;
    }
    if probe == "serial_alloc_free" {
        bench_serial_alloc_free(serial_iterations);
        return 0;
    }
    if probe == "serial_realloc" {
        bench_serial_realloc(realloc_iterations);
        return 0;
    }
    if probe == "heap_stats_poll" {
        bench_heap_stats_poll(stats_iterations);
        return 0;
    }
    if probe == "concurrent_alloc_free" {
        let _ = bench_concurrent_alloc_free(concurrent_rounds).await();
        return 0;
    }
    print("unknown probe: " + probe);
    return 2;
}
EOF

if ! "$surge" build --release "$fixture" >"$build_log" 2>&1; then
	cat "$build_log" >&2
	fail "failed to build generated fixture"
fi

built_path="$(awk '/^built / { print $2 }' "$build_log" | tail -n 1)"
[[ -n "$built_path" ]] || fail "cannot find built binary in surge output"

bench_bin="$built_path"
if [[ "$bench_bin" != /* ]]; then
	if [[ -x "$fixture/$bench_bin" ]]; then
		bench_bin="$fixture/$bench_bin"
	elif [[ -x "$root/$bench_bin" ]]; then
		bench_bin="$root/$bench_bin"
	else
		bench_bin="$PWD/$bench_bin"
	fi
fi
[[ -x "$bench_bin" ]] || fail "built binary not executable: $bench_bin"

mkdir -p "$(dirname "$report")"
{
	echo "# Native heap accounting benchmark"
	echo
	echo "Generated: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	echo
	echo "## Environment"
	echo
	echo "- surge: $("$surge" version --full | tr '\n' ' ' | sed 's/[[:space:]]*$//')"
	echo "- fixture: generated temporary native heap accounting probe"
	echo "- threads: $modes"
	echo "- probes: $probes"
	echo "- serial alloc/free iterations: $serial_iterations"
	echo "- serial realloc iterations: $realloc_iterations"
	echo "- heap stats poll iterations: $stats_iterations"
	echo "- concurrent alloc/free rounds per worker: $concurrent_rounds"
	echo "- per-probe timeout seconds: $probe_timeout_seconds"
	echo
	echo "## Results"
	echo
	echo "| threads | probe | iterations | total us | ns/op | alloc delta | free delta | live block delta | live byte delta |"
	echo "| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |"
} >"$report"

for mode in $modes; do
	for probe in $probes; do
		if output="$(timeout "${probe_timeout_seconds}s" env SURGE_THREADS="$mode" "$bench_bin" "$probe" 2>&1)"; then
			:
		else
			status=$?
			printf '%s\n' "$output" >&2
			fail "probe failed: threads=$mode probe=$probe status=$status timeout=${probe_timeout_seconds}s"
		fi
		while IFS= read -r line; do
			[[ "$line" == \|* ]] || continue
			printf '| %s |%s\n' "$mode" "${line#|}" >>"$report"
		done <<<"$output"
	done
done

cat >>"$report" <<'EOF'

## Notes

- `empty_loop` is a timing floor for the generated benchmark program.
- `serial_alloc_free` exercises successful `rt_alloc` plus `rt_free` accounting through a generated Surge fixture.
- `serial_realloc` records the ordinary realloc event shape.
- `heap_stats_poll` measures aggregate-on-read snapshot cost plus public result allocation.
- `concurrent_alloc_free` starts up to one task per runtime worker, capped at eight tasks.
- Heap deltas include generated Surge loop/runtime allocations around each probe.
- This is manual evidence for Runtime V2 Epic 5 Task 8. Do not wire it into `make check`.
EOF

echo "benchmark report: $report"
