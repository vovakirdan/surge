#!/usr/bin/env bash
# RV2-DEBT-015 reproducer orchestrator: fixture server +
# sustained stallrepro client + 250ms kernel-side ss watcher that SIGUSR1s
# the server for a live TRACE_EXEC dump on the first detected stall.
# Owns its per-probe timeouts (client socket timeouts + bounded duration +
# watcher lifecycle) instead of relying on outer timeout wrappers.
#
# Usage: run_stallrepro.sh <tag> [stallrepro.py extra args...]
# Env: STALL_CONNS (1024), STALL_SHARDS (8), STALL_MODE (direct),
#      STALL_OUT (build/benchmarks/stallrepro)
set -u
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${STALL_OUT:-$root/build/benchmarks/stallrepro}"
bin="$root/target/release/net_request_reply"
tag="${1:?usage: run_stallrepro.sh <tag> [client args...]}"; shift
conns="${STALL_CONNS:-1024}"
shards="${STALL_SHARDS:-8}"
mode="${STALL_MODE:-direct}"
mkdir -p "$out"
flag="$out/$tag.flag"
rm -f "$flag"

if [[ ! -x "$bin" ]]; then
	echo "run_stallrepro: $bin not found; build it first:" >&2
	echo "  SURGE_STDLIB=$root $root/surge build --release benchmarks/native/net_request_reply" >&2
	exit 1
fi

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"

echo "=== $tag start $(date -u '+%H:%M:%S') port=$port conns=$conns shards=$shards host: $(uptime)" | tee "$out/$tag.meta"

env SURGE_TRACE_EXEC=1 SURGE_SCHED_TRACE=1 SURGE_SHARDS="$shards" SURGE_THREADS="$shards" \
	"$bin" "$port" "$mode" "$conns" >"$out/$tag.server.out" 2>"$out/$tag.server.trace" &
server_pid=$!

# Watcher: 250ms ss snapshots; on stall flag, SIGUSR1 + full snapshot burst.
(
	fired=0
	while kill -0 "$server_pid" 2>/dev/null; do
		ts="$(date +%s.%N)"
		qsum="$(ss -tni "sport = :$port" 2>/dev/null | awk '/^ESTAB/{ if ($2>0) rq++; if ($3>0) sq++; n++ } END { printf "estab=%d recvq_pos=%d sendq_pos=%d", n+0, rq+0, sq+0 }')"
		echo "$ts $qsum" >>"$out/$tag.watch"
		if [[ -s "$flag" && $fired -eq 0 ]]; then
			fired=1
			echo "=== STALL DETECTED $ts — SIGUSR1 + snapshots" >>"$out/$tag.watch"
			kill -USR1 "$server_pid" 2>/dev/null
			{ echo "=== ss full @$ts"; ss -tni "sport = :$port"; } >>"$out/$tag.ss-stall" 2>&1
			sleep 1
			kill -USR1 "$server_pid" 2>/dev/null
			{ echo "=== ss full +1s"; ss -tni "sport = :$port"; } >>"$out/$tag.ss-stall" 2>&1
		fi
		sleep 0.25
	done
) &
watcher_pid=$!

python3 "$root/scripts/stallrepro.py" "$port" --connections "$conns" --stall-flag "$flag" "$@" \
	>"$out/$tag.client.out" 2>"$out/$tag.client.err"
client_rc=$?

# client closed conns; server should exit
for _ in $(seq 1 100); do kill -0 "$server_pid" 2>/dev/null || break; sleep 0.1; done
if kill -0 "$server_pid" 2>/dev/null; then
	echo "server still alive after client exit — SIGUSR1 then kill" >>"$out/$tag.meta"
	kill -USR1 "$server_pid"; sleep 1; kill "$server_pid"
fi
wait "$server_pid" 2>/dev/null; server_rc=$?
kill "$watcher_pid" 2>/dev/null; wait "$watcher_pid" 2>/dev/null

echo "=== $tag end $(date -u '+%H:%M:%S') client_rc=$client_rc server_rc=$server_rc host: $(uptime)" | tee -a "$out/$tag.meta"
tail -6 "$out/$tag.client.out" | tee -a "$out/$tag.meta"
exit "$client_rc"
