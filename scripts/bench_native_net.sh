#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$root/benchmarks/native/net_request_reply"
report="${SURGE_NET_BENCH_REPORT:-$root/build/benchmarks/native-net-request-reply.md}"
threads="${SURGE_NET_BENCH_THREADS:-1 2 4 8}"
modes="${SURGE_NET_BENCH_MODES:-echo direct manager}"
patterns="${SURGE_NET_BENCH_PATTERNS:-seq pipe}"
requests="${SURGE_NET_BENCH_REQUESTS:-2000}"
pipeline_depth="${SURGE_NET_BENCH_PIPELINE_DEPTH:-64}"
surge="${SURGE:-$root/surge}"

fail() {
	echo "bench_native_net: $*" >&2
	exit 1
}

trace_value() {
	local file="$1"
	local record="$2"
	local key="$3"
	awk -v record="$record" -v key="$key" '
		$1 == record {
			for (i = 2; i <= NF; i++) {
				split($i, kv, "=")
				if (kv[1] == key) value = kv[2]
			}
		}
		END {
			if (value == "") value = "n/a"
			print value
		}
	' "$file"
}

pick_port() {
	python3 - <<'PY'
import socket
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
    s.bind(("127.0.0.1", 0))
    print(s.getsockname()[1])
PY
}

run_client() {
	local port="$1"
	local mode="$2"
	local pattern="$3"
	python3 - "$port" "$mode" "$pattern" "$requests" "$pipeline_depth" <<'PY'
import socket
import statistics
import sys
import time

port = int(sys.argv[1])
mode = sys.argv[2]
pattern = sys.argv[3]
requests = int(sys.argv[4])
pipeline_depth = int(sys.argv[5])
payload = b"x"
response = payload if mode == "echo" else b'VALUE {"v":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}\n'

def connect():
    deadline = time.monotonic() + 5.0
    last = None
    while time.monotonic() < deadline:
        try:
            s = socket.create_connection(("127.0.0.1", port), timeout=2.0)
            s.settimeout(5.0)
            return s
        except OSError as exc:
            last = exc
            time.sleep(0.01)
    raise RuntimeError(f"connect failed: {last}")

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

def percentile(values, pct):
    if not values:
        return 0.0
    idx = int((len(values) - 1) * pct / 100.0)
    return sorted(values)[idx]

samples = []
start = time.perf_counter_ns()
with connect() as sock:
    if pattern == "seq":
        for _ in range(requests):
            t0 = time.perf_counter_ns()
            sock.sendall(payload)
            got = recv_exact(sock, len(response))
            if got != response:
                raise RuntimeError(f"bad response: {got!r}")
            samples.append((time.perf_counter_ns() - t0) / 1000.0)
    elif pattern == "pipe":
        done = 0
        while done < requests:
            batch = min(pipeline_depth, requests - done)
            t0 = time.perf_counter_ns()
            sock.sendall(payload * batch)
            for _ in range(batch):
                got = recv_exact(sock, len(response))
                if got != response:
                    raise RuntimeError(f"bad response: {got!r}")
            per_op = (time.perf_counter_ns() - t0) / 1000.0 / batch
            samples.extend([per_op] * batch)
            done += batch
    else:
        raise RuntimeError(f"unknown pattern: {pattern}")
elapsed_us = (time.perf_counter_ns() - start) / 1000.0
avg = statistics.fmean(samples) if samples else 0.0
print(f"{requests} {elapsed_us:.0f} {avg:.2f} {percentile(samples, 50):.2f} {percentile(samples, 95):.2f}")
PY
}

wait_for_pid() {
	local pid="$1"
	local deadline=$((SECONDS + 5))
	while kill -0 "$pid" 2>/dev/null; do
		if (( SECONDS >= deadline )); then
			kill "$pid" 2>/dev/null || true
			wait "$pid" 2>/dev/null || true
			return 1
		fi
		sleep 0.05
	done
	wait "$pid"
}

if [[ ! -x "$surge" ]]; then
	surge="$(command -v surge || true)"
fi
[[ -n "$surge" && -x "$surge" ]] || fail "surge binary not found; run 'make build' or set SURGE=/path/to/surge"
command -v python3 >/dev/null || fail "python3 not found"

export SURGE_STDLIB="${SURGE_NET_BENCH_STDLIB:-$root}"

build_log="$(mktemp)"
trace_rows="$(mktemp)"
trap 'rm -f "$build_log" "$trace_rows"' EXIT

if ! "$surge" build --release "$fixture" >"$build_log" 2>&1; then
	cat "$build_log" >&2
	fail "failed to build $fixture"
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
	echo "# Native net request/reply benchmark"
	echo
	echo "Generated: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	echo
	echo "## Environment"
	echo
	echo "- surge: $("$surge" version --full | tr '\n' ' ' | sed 's/[[:space:]]*$//')"
	echo "- fixture: ${fixture#$root/}"
	echo "- threads: $threads"
	echo "- modes: $modes"
	echo "- patterns: $patterns"
	echo "- requests: $requests"
	echo "- pipeline depth: $pipeline_depth"
	echo "- trace: per run SURGE_TRACE_EXEC=1"
	echo
	echo "## Results"
	echo
	echo "| threads | mode | pattern | requests | total us | avg us/op | p50 us | p95 us |"
	echo "| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |"
} >"$report"

for worker_count in $threads; do
	for mode in $modes; do
		for pattern in $patterns; do
			port="$(pick_port)"
			server_out="$(mktemp)"
			trace_log="$(mktemp)"
			SURGE_TRACE_EXEC=1 SURGE_THREADS="$worker_count" "$bench_bin" "$port" "$mode" >"$server_out" 2>"$trace_log" &
			server_pid="$!"
			if ! result="$(run_client "$port" "$mode" "$pattern")"; then
				cat "$server_out" >&2 || true
				cat "$trace_log" >&2 || true
				kill "$server_pid" 2>/dev/null || true
				wait "$server_pid" 2>/dev/null || true
				fail "client failed for threads=$worker_count mode=$mode pattern=$pattern"
			fi
			if ! wait_for_pid "$server_pid"; then
				cat "$server_out" >&2 || true
				cat "$trace_log" >&2 || true
				fail "server did not exit for threads=$worker_count mode=$mode pattern=$pattern"
			fi
			read -r got_requests total_us avg_us p50_us p95_us <<<"$result"
			printf '| %s | %s | %s | %s | %s | %s | %s | %s |\n' \
				"$worker_count" "$mode" "$pattern" "$got_requests" "$total_us" "$avg_us" "$p50_us" "$p95_us" >>"$report"
			printf '| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n' \
				"$worker_count" "$mode" "$pattern" \
				"$(trace_value "$trace_log" TRACE_EXEC channel_task_blocking_send)" \
				"$(trace_value "$trace_log" TRACE_EXEC channel_task_blocking_recv)" \
				"$(trace_value "$trace_log" TRACE_EXEC channel_handoff_yield)" \
				"$(trace_value "$trace_log" TRACE_EXEC compensation_started)" \
				"$(trace_value "$trace_log" TRACE_EXEC_SNAPSHOT compensation_high_water)" \
				"$(trace_value "$trace_log" TRACE_NET io_direct_waits)" \
				"$(trace_value "$trace_log" TRACE_NET io_poll_calls)" \
				"$(trace_value "$trace_log" TRACE_NET io_poll_net_ready)" \
				"$(trace_value "$trace_log" TRACE_NET io_poll_waiters_total)" >>"$trace_rows"
			rm -f "$server_out" "$trace_log"
		done
	done
done

cat >>"$report" <<'EOF'

## Runtime Trace

| threads | mode | pattern | task-context blocking sends | task-context blocking recvs | handoff yields | compensation started | compensation high-water | net direct waits | net poll calls | net ready | net waiters total |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
EOF
cat "$trace_rows" >>"$report"

cat >>"$report" <<'EOF'

## Notes

- `echo` measures minimal TCP read/write request/reply with one-byte responses.
- `direct` reads one-byte requests and writes the fixed GET-like response in the socket task.
- `manager` adds a Surge channel request/reply hop before writing the same fixed response.
- `seq` waits for each response before sending the next request.
- `pipe` sends batches before reading responses to reduce client-side round-trip waiting.
- This is a manual benchmark. Do not wire it into `make check`; use it for before/after runtime PRs.
EOF

echo "benchmark report: $report"
