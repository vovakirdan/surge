#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$root/benchmarks/native/channel_request_reply"
report="${SURGE_CHANNEL_BENCH_REPORT:-$root/build/benchmarks/native-channel-request-reply.md}"
modes="${SURGE_CHANNEL_BENCH_MODES:-1 2 4 8 default}"
trace_probes="${SURGE_CHANNEL_TRACE_PROBES:-channel_ping_pong channel_reused_reply channel_new_reply channel_sync_new_reply}"
surge="${SURGE:-$root/surge}"
channel_wake_policy="handoff-inject"
if [[ -n "${SURGE_CHANNEL_WAKE_INJECT:-}" && "${SURGE_CHANNEL_WAKE_INJECT:-}" != "0" ]]; then
	channel_wake_policy="force-inject"
fi

fail() {
	echo "bench_native_channels: $*" >&2
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

if [[ ! -x "$surge" ]]; then
	surge="$(command -v surge || true)"
fi
[[ -n "$surge" && -x "$surge" ]] || fail "surge binary not found; run 'make build' or set SURGE=/path/to/surge"

export SURGE_STDLIB="${SURGE_STDLIB:-$root}"

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
	echo "# Native channel request/reply benchmark"
	echo
	echo "Generated: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	echo
	echo "## Environment"
	echo
	echo "- surge: $("$surge" version --full | tr '\n' ' ' | sed 's/[[:space:]]*$//')"
	echo "- fixture: ${fixture#$root/}"
	echo "- modes: $modes"
	echo "- trace probes: $trace_probes"
	echo "- channel wake policy: $channel_wake_policy"
	echo "- trace: separate per-probe SURGE_TRACE_EXEC=1 pass"
	echo
	echo "## Results"
	echo
	echo "| mode | probe | iterations | total us | ns/op |"
	echo "| --- | --- | ---: | ---: | ---: |"
} >"$report"

for mode in $modes; do
	if [[ "$mode" == "default" ]]; then
		output="$(env -u SURGE_THREADS "$bench_bin")"
	else
		output="$(SURGE_THREADS="$mode" "$bench_bin")"
	fi
	while IFS= read -r line; do
		[[ "$line" == \|* ]] || continue
		printf '| %s |%s\n' "$mode" "${line#|}" >>"$report"
	done <<<"$output"
	for probe in $trace_probes; do
		trace_log="$(mktemp)"
		if [[ "$mode" == "default" ]]; then
			env -u SURGE_THREADS SURGE_TRACE_EXEC=1 "$bench_bin" "$probe" >/dev/null 2>"$trace_log"
		else
			SURGE_TRACE_EXEC=1 SURGE_THREADS="$mode" "$bench_bin" "$probe" >/dev/null 2>"$trace_log"
		fi
		printf '| %s | %s | %s | %s | %s | %s | %s | %s |\n' \
			"$mode" \
			"$probe" \
			"$(trace_value "$trace_log" TRACE_EXEC channel_blocking_wait)" \
			"$(trace_value "$trace_log" TRACE_EXEC channel_task_blocking_send)" \
			"$(trace_value "$trace_log" TRACE_EXEC channel_task_blocking_recv)" \
			"$(trace_value "$trace_log" TRACE_EXEC channel_handoff_yield)" \
			"$(trace_value "$trace_log" TRACE_EXEC compensation_started)" \
			"$(trace_value "$trace_log" TRACE_EXEC_SNAPSHOT compensation_high_water)" >>"$trace_rows"
		rm -f "$trace_log"
	done
done

cat >>"$report" <<'EOF'

## Runtime Trace

| mode | probe | channel blocking waits | task-context blocking sends | task-context blocking recvs | handoff yields | compensation started | compensation high-water |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
EOF
cat "$trace_rows" >>"$report"

cat >>"$report" <<'EOF'

## Notes

- `channel_reused_reply` approximates the state-manager request/reply hop used by surgekv.
- `channel_new_reply` keeps the older per-request reply-channel shape visible.
- `channel_ping_pong` measures direct async send/recv handoff between two tasks.
- `channel_sync_new_reply` measures the sync-wrapper fallback from async task context.
- Runtime trace rows run one probe at a time so sync-wrapper fallback counters do not pollute async probe counters.
- This is a manual benchmark. Do not wire it into `make check`; use it for before/after runtime PRs.
EOF

echo "benchmark report: $report"
