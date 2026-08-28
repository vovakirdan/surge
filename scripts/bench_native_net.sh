#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$root/benchmarks/native/net_request_reply"
report="${SURGE_NET_BENCH_REPORT:-$root/build/benchmarks/native-net-request-reply.md}"
shards="${SURGE_NET_BENCH_SHARDS:-1}"
threads="${SURGE_NET_BENCH_THREADS:-}"
connections="${SURGE_NET_BENCH_CONNECTIONS:-1}"
modes="${SURGE_NET_BENCH_MODES:-echo direct manager}"
patterns="${SURGE_NET_BENCH_PATTERNS:-seq pipe}"
requests="${SURGE_NET_BENCH_REQUESTS:-2000}"
pipeline_depth="${SURGE_NET_BENCH_PIPELINE_DEPTH:-64}"
client_parallel="${SURGE_NET_BENCH_CLIENT_PARALLEL:-128}"
run_timeout="${SURGE_NET_BENCH_RUN_TIMEOUT:-30s}"
try_10k="${SURGE_NET_BENCH_TRY_10K:-0}"
allow_stale="${SURGE_NET_BENCH_ALLOW_STALE:-0}"
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

# The scheduler prints no row that sums across owners: each SCHED_TRACE count of
# something a thread did -- where it took its next task from, a steal it was
# refused, a connection task it ran -- belongs to one owner, a carrier or the
# runtime's control lane, and a row summing owners has writers that share
# neither a lock nor an owner, so the runtime refuses to print one. The reader
# adds the owner records up here, over the owner set the runtime record names.
# The `owner=runtime` row is read with trace_value instead, because its counts
# are that one owner's own and not a sum of anybody else's.
trace_owner_sum() {
	local file="$1"
	local key="$2"
	awk -v key="$key" '
		$1 == "SCHED_TRACE" {
			owner = ""
			value = ""
			for (i = 2; i <= NF; i++) {
				split($i, kv, "=")
				if (kv[1] == "owner") owner = kv[2]
				if (kv[1] == key) value = kv[2]
			}
			if ((owner == "carrier" || owner == "control") && value != "") {
				total += value
				seen = 1
			}
		}
		END {
			if (!seen) print "n/a"
			else print total
		}
	' "$file"
}

trace_prefixed_fields() {
	local file="$1"
	local record="$2"
	local prefix="$3"
	awk -v record="$record" -v prefix="$prefix" '
		$1 == record {
			value = ""
			sep = ""
			for (i = 2; i <= NF; i++) {
				if (index($i, prefix) == 1) {
					value = value sep $i
					sep = " "
				}
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
	local connection_count="$4"
	python3 - "$port" "$mode" "$pattern" "$requests" "$pipeline_depth" "$connection_count" "$client_parallel" <<'PY'
from concurrent.futures import ThreadPoolExecutor, as_completed
import socket
import statistics
import sys
import time

port = int(sys.argv[1])
mode = sys.argv[2]
pattern = sys.argv[3]
requests = int(sys.argv[4])
pipeline_depth = int(sys.argv[5])
connections = int(sys.argv[6])
client_parallel = max(1, int(sys.argv[7]))
payload = b"x"
response = payload if mode == "echo" else b'VALUE {"v":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}\n'

def connect(index):
    deadline = time.monotonic() + 10.0
    last = None
    while time.monotonic() < deadline:
        try:
            sock = socket.create_connection(("127.0.0.1", port), timeout=2.0)
            sock.settimeout(10.0)
            return sock
        except OSError as exc:
            last = exc
            time.sleep(0.01)
    raise RuntimeError(f"connect {index} failed: {last}")

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

def exercise(sock):
    samples = []
    if pattern == "seq":
        for _ in range(requests):
            t0 = time.perf_counter_ns()
            sock.sendall(payload)
            got = recv_exact(sock, len(response))
            if got != response:
                raise RuntimeError(f"bad response: {got!r}")
            samples.append((time.perf_counter_ns() - t0) / 1000.0)
        return samples
    if pattern == "pipe":
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
        return samples
    raise RuntimeError(f"unknown pattern: {pattern}")

sockets = []
samples = []
start = time.perf_counter_ns()
try:
    for i in range(connections):
        sockets.append(connect(i))
    workers = min(connections, client_parallel)
    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = [pool.submit(exercise, sock) for sock in sockets]
        for future in as_completed(futures):
            samples.extend(future.result())
finally:
    for sock in sockets:
        try:
            sock.close()
        except OSError:
            pass

elapsed_us = (time.perf_counter_ns() - start) / 1000.0
avg = statistics.fmean(samples) if samples else 0.0
print(f"{len(samples)} {elapsed_us:.0f} {avg:.2f} {percentile(samples, 50):.2f} {percentile(samples, 95):.2f}")
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

row_skip_reason() {
	local connection_count="$1"
	if (( connection_count >= 10000 )) && [[ "$try_10k" != "1" ]]; then
		echo "10k row disabled by default; set SURGE_NET_BENCH_TRY_10K=1 after fd-limit and timeout checks"
		return 0
	fi
	local fd_limit
	fd_limit="$(ulimit -n || true)"
	if [[ "$fd_limit" =~ ^[0-9]+$ ]]; then
		local needed=$((connection_count + 64))
		if (( fd_limit < needed )); then
			echo "fd limit $fd_limit is below required client/server per-process floor $needed"
			return 0
		fi
	fi
	return 1
}

version_commit() {
	python3 -c 'import json,sys; print(json.load(sys.stdin).get("git_commit", ""))'
}

if [[ ! -x "$surge" ]]; then
	surge="$(command -v surge || true)"
fi
[[ -n "$surge" && -x "$surge" ]] || fail "surge binary not found; run 'make build' or set SURGE=/path/to/surge"
command -v python3 >/dev/null || fail "python3 not found"
command -v timeout >/dev/null || fail "timeout not found"
command -v git >/dev/null || fail "git not found"

current_commit="$(git -C "$root" rev-parse HEAD)"
version_json="$("$surge" version --full --format json)"
surge_commit="$(printf '%s\n' "$version_json" | version_commit)"
if [[ "$allow_stale" != "1" ]]; then
	if [[ -z "$surge_commit" || "$surge_commit" == "unknown" ]]; then
		fail "surge binary has no embedded git commit; rebuild with scripts/ldflags.sh or set SURGE_NET_BENCH_ALLOW_STALE=1"
	fi
	if [[ "$surge_commit" != "$current_commit" &&
	      "${surge_commit:0:12}" != "${current_commit:0:12}" ]]; then
		fail "surge binary commit $surge_commit does not match current checkout $current_commit"
	fi
fi

export SURGE_STDLIB="${SURGE_NET_BENCH_STDLIB:-$root}"

build_log="$(mktemp)"
trace_rows="$(mktemp)"
skip_rows="$(mktemp)"
trap 'rm -f "$build_log" "$trace_rows" "$skip_rows"' EXIT

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
	echo "- surge commit: $surge_commit"
	echo "- checkout commit: $current_commit"
	echo "- fixture: ${fixture#$root/}"
	echo "- shards: $shards"
	echo "- threads: ${threads:-same as SURGE_SHARDS per row}"
	echo "- connections: $connections"
	echo "- modes: $modes"
	echo "- patterns: $patterns"
	echo "- requests per connection: $requests"
	echo "- pipeline depth: $pipeline_depth"
	echo "- client parallelism: $client_parallel"
	echo "- per-row timeout: $run_timeout"
	echo "- trace: per run SURGE_TRACE_EXEC=1 SURGE_SCHED_TRACE=1"
	echo
	echo "## Results"
	echo
	echo "| shards | threads | connections | mode | pattern | requests/conn | total requests | total us | avg us/op | p50 us | p95 us |"
	echo "| ---: | ---: | ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |"
} >"$report"

for shard_count in $shards; do
	thread_values="$threads"
	if [[ -z "$thread_values" ]]; then
		if [[ "$shards" == "1" ]]; then
			thread_values="1 2 4 8"
		else
			thread_values="$shard_count"
		fi
	fi
	for worker_count in $thread_values; do
		if (( shard_count > 1 )) && (( worker_count != shard_count )); then
			fail "SURGE_SHARDS=$shard_count requires SURGE_THREADS=$shard_count, got $worker_count"
		fi
		for connection_count in $connections; do
			skip_reason="$(row_skip_reason "$connection_count" || true)"
			for mode in $modes; do
				for pattern in $patterns; do
					if [[ -n "$skip_reason" ]]; then
						printf '| %s | %s | %s | %s | %s | %s | skipped | skipped | skipped | skipped | skipped |\n' \
							"$shard_count" "$worker_count" "$connection_count" "$mode" "$pattern" "$requests" >>"$report"
						printf -- '- shards=%s threads=%s connections=%s mode=%s pattern=%s: %s\n' \
							"$shard_count" "$worker_count" "$connection_count" "$mode" "$pattern" "$skip_reason" >>"$skip_rows"
						continue
					fi
					port="$(pick_port)"
					server_out="$(mktemp)"
					trace_log="$(mktemp)"
					env SURGE_TRACE_EXEC=1 SURGE_SCHED_TRACE=1 SURGE_SHARDS="$shard_count" SURGE_THREADS="$worker_count" \
						timeout "$run_timeout" "$bench_bin" "$port" "$mode" "$connection_count" >"$server_out" 2>"$trace_log" &
					server_pid="$!"
					if ! result="$(run_client "$port" "$mode" "$pattern" "$connection_count")"; then
						cat "$server_out" >&2 || true
						cat "$trace_log" >&2 || true
						kill "$server_pid" 2>/dev/null || true
						wait "$server_pid" 2>/dev/null || true
						fail "client failed for shards=$shard_count threads=$worker_count connections=$connection_count mode=$mode pattern=$pattern"
					fi
					if ! wait_for_pid "$server_pid"; then
						cat "$server_out" >&2 || true
						cat "$trace_log" >&2 || true
						fail "server did not exit for shards=$shard_count threads=$worker_count connections=$connection_count mode=$mode pattern=$pattern"
					fi
					read -r got_requests total_us avg_us p50_us p95_us <<<"$result"
					printf '| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n' \
						"$shard_count" "$worker_count" "$connection_count" "$mode" "$pattern" \
						"$requests" "$got_requests" "$total_us" "$avg_us" "$p50_us" "$p95_us" >>"$report"
					printf '| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n' \
						"$shard_count" "$worker_count" "$connection_count" "$mode" "$pattern" \
						"$(trace_value "$trace_log" TRACE_NET runtime_shards)" \
						"$(trace_owner_sum "$trace_log" steal)" \
						"$(trace_owner_sum "$trace_log" tier1_steal_denied)" \
						"$(trace_value "$trace_log" SCHED_TRACE conn_owner_placed)" \
						"$(trace_owner_sum "$trace_log" conn_owner_local)" \
						"$(trace_owner_sum "$trace_log" conn_owner_mismatch)" \
						"$(trace_value "$trace_log" TRACE_NET accept_owner_total)" \
						"$(trace_value "$trace_log" TRACE_NET accept_owner_active_shards)" \
						"$(trace_value "$trace_log" TRACE_NET accept_owner_imbalance)" \
						"$(trace_value "$trace_log" TRACE_NET global_path_fallbacks)" \
						"$(trace_value "$trace_log" TRACE_NET fd_ready_batches)" \
						"$(trace_value "$trace_log" TRACE_NET fd_ready_batch_fds_total)" \
						"$(trace_value "$trace_log" TRACE_NET fd_ready_batch_fds_max)" \
						"$(trace_value "$trace_log" TRACE_EXEC control_lock_acquired)" \
						"$(trace_value "$trace_log" TRACE_EXEC ctrl_create)" \
						"$(trace_value "$trace_log" TRACE_EXEC ctrl_join_poll)" \
						"$(trace_value "$trace_log" TRACE_EXEC ctrl_completion)" \
						"$(trace_value "$trace_log" TRACE_EXEC ctrl_scope)" \
						"$(trace_value "$trace_log" TRACE_EXEC ctrl_await_compat)" \
						"$(trace_value "$trace_log" TRACE_EXEC ctrl_handle)" \
						"$(trace_value "$trace_log" TRACE_EXEC cross_shard_wakes)" \
						"$(trace_value "$trace_log" TRACE_EXEC spurious_wakes_absorbed)" \
						"$(trace_value "$trace_log" TRACE_EXEC collect_wake_batches)" \
						"$(trace_value "$trace_log" TRACE_EXEC owner_replacements)" \
						"\`$(trace_prefixed_fields "$trace_log" TRACE_NET_SHARDS accept_)\`" \
						"\`$(trace_prefixed_fields "$trace_log" TRACE_NET_SHARDS fd_ready_batches_)\`" \
						"\`$(trace_prefixed_fields "$trace_log" TRACE_NET_SHARDS fd_ready_fds_)\`" >>"$trace_rows"
					rm -f "$server_out" "$trace_log"
				done
			done
		done
	done
done

cat >>"$report" <<'EOF'

## Runtime Trace

| shards | threads | connections | mode | pattern | runtime shards | sched steal | Tier 1 denied steals | conn owner placed | conn owner local | conn owner mismatch | accept total | active accept shards | accept imbalance | global fallbacks | fd batches | fd batch fds | fd batch max | control lock acq | ctrl create | ctrl join_poll | ctrl completion | ctrl scope | ctrl await_compat | ctrl handle | cross-shard wakes | spurious absorbed | collect batches | owner replacements | per-shard accepts | per-shard fd batches | per-shard fd fds |
| ---: | ---: | ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | --- |
EOF
cat "$trace_rows" >>"$report"

if [[ -s "$skip_rows" ]]; then
	{
		echo
		echo "## Skipped Rows"
		echo
		cat "$skip_rows"
	} >>"$report"
fi

cat >>"$report" <<'EOF'

## Notes

- Defaults preserve the legacy single-connection benchmark matrix. Use `SURGE_NET_BENCH_SHARDS`, `SURGE_NET_BENCH_THREADS`, and `SURGE_NET_BENCH_CONNECTIONS` to request Task 12 evidence rows.
- `SURGE_SHARDS>1` requires `SURGE_THREADS` to match the shard count.
- 10k-connection rows are skipped unless `SURGE_NET_BENCH_TRY_10K=1` is set and local fd/timeout checks are safe.
- Low-count `SO_REUSEPORT` distribution skew is expected; judge distribution from the higher-connection rows.
- Multi-shard throughput is still bounded by the preserved global executor lock.
EOF

echo "benchmark report: $report"
