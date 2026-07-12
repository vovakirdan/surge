#!/usr/bin/env bash
# Per-thread CPU split of the net fixture server under sustained 1024-conn
# load probe: proves whether task execution is
# distributed across shard workers or funneled onto one.
#
# Usage: cpu_validate.sh <tag>
# Env: STALL_SHARDS (8), STALL_CONNS (1024), STALL_OUT (build/benchmarks/stallrepro)
set -u
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${STALL_OUT:-$root/build/benchmarks/stallrepro}"
bin="$root/target/release/net_request_reply"
tag="${1:?usage: cpu_validate.sh <tag>}"
shards="${STALL_SHARDS:-8}"
conns="${STALL_CONNS:-1024}"
mkdir -p "$out"

if [[ ! -x "$bin" ]]; then
	echo "cpu_validate: $bin not found; build it first:" >&2
	echo "  SURGE_STDLIB=$root $root/surge build --release benchmarks/native/net_request_reply" >&2
	exit 1
fi

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
echo "=== $tag port=$port shards=$shards host: $(uptime)"
env SURGE_TRACE_EXEC=1 SURGE_SCHED_TRACE=1 SURGE_SHARDS="$shards" SURGE_THREADS="$shards" \
	"$bin" "$port" direct "$conns" >"$out/$tag.server.out" 2>"$out/$tag.server.trace" &
spid=$!
sleep 1
python3 "$root/scripts/stallrepro.py" "$port" --connections "$conns" --duration 30 \
	--stall-flag "$out/$tag.flag" \
	>"$out/$tag.client.out" 2>"$out/$tag.client.err" &
cpid=$!
sleep 10
declare -A t0
for st in /proc/$spid/task/*/stat; do
	tid=$(basename "$(dirname "$st")")
	read -r -a f < "$st" || continue
	t0[$tid]=$(( ${f[13]} + ${f[14]} ))
done
sleep 15
echo "--- thread cpu jiffies over 15s window (utime+stime delta):"
for st in /proc/$spid/task/*/stat; do
	tid=$(basename "$(dirname "$st")")
	read -r -a f < "$st" || continue
	now=$(( ${f[13]} + ${f[14]} ))
	prev=${t0[$tid]:-0}
	comm=$(cat /proc/$spid/task/$tid/comm 2>/dev/null)
	echo "tid=$tid comm=$comm delta=$((now - prev))"
done
wait "$cpid"
tail -3 "$out/$tag.client.out"
for _ in $(seq 1 100); do kill -0 "$spid" 2>/dev/null || break; sleep 0.1; done
kill "$spid" 2>/dev/null; wait "$spid" 2>/dev/null
echo "=== $tag done host: $(uptime)"
