#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$root/benchmarks/native/channel_request_reply"
report="${SURGE_CHANNEL_BENCH_REPORT:-$root/build/benchmarks/native-channel-request-reply.md}"
modes="${SURGE_CHANNEL_BENCH_MODES:-1 2 4 8 default}"
surge="${SURGE:-$root/surge}"

fail() {
	echo "bench_native_channels: $*" >&2
	exit 1
}

if [[ ! -x "$surge" ]]; then
	surge="$(command -v surge || true)"
fi
[[ -n "$surge" && -x "$surge" ]] || fail "surge binary not found; run 'make build' or set SURGE=/path/to/surge"

export SURGE_STDLIB="${SURGE_STDLIB:-$root}"

build_log="$(mktemp)"
trap 'rm -f "$build_log"' EXIT

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
done

cat >>"$report" <<'EOF'

## Notes

- `channel_reused_reply` approximates the state-manager request/reply hop used by surgekv.
- `channel_new_reply` keeps the older per-request reply-channel shape visible.
- This is a manual benchmark. Do not wire it into `make check`; use it for before/after runtime PRs.
EOF

echo "benchmark report: $report"
