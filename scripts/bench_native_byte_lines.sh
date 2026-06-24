#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$root/benchmarks/native/byte_lines"
report="${SURGE_BYTES_LINE_BENCH_REPORT:-$root/build/benchmarks/native-byte-lines.md}"
repeats="${SURGE_BYTES_LINE_BENCH_REPEATS:-5}"
surge="${SURGE:-$root/surge}"

fail() {
	echo "bench_native_byte_lines: $*" >&2
	exit 1
}

if [[ ! -x "$surge" ]]; then
	surge="$(command -v surge || true)"
fi
[[ -n "$surge" && -x "$surge" ]] || fail "surge binary not found; run 'make build' or set SURGE=/path/to/surge"
command -v python3 >/dev/null || fail "python3 not found"

export SURGE_STDLIB="${SURGE_BYTES_LINE_BENCH_STDLIB:-$root}"

build_log="$(mktemp)"
rows="$(mktemp)"
trap 'rm -f "$build_log" "$rows"' EXIT

if ! "$surge" build --release "$fixture" >"$build_log" 2>&1; then
	cat "$build_log" >&2
	fail "failed to build $fixture"
fi

built_path="$(awk '/^built / { print $2 }' "$build_log" | tail -n 1)"
[[ -n "$built_path" ]] || fail "cannot find built binary in surge output"
if [[ "$built_path" != /* ]]; then
	if [[ -x "$root/$built_path" ]]; then
		built_path="$root/$built_path"
	else
		built_path="$fixture/$built_path"
	fi
fi
[[ -x "$built_path" ]] || fail "built binary not executable: $built_path"

for i in $(seq 1 "$repeats"); do
	out="$("$built_path")"
	echo "$out"
	string_us="$(sed -n 's/^string_line_us=\([0-9][0-9]*\).*/\1/p' <<<"$out")"
	byte_us="$(sed -n 's/^byte_line_us=\([0-9][0-9]*\).*/\1/p' <<<"$out")"
	token_string_us="$(sed -n 's/^token_string_us=\([0-9][0-9]*\).*/\1/p' <<<"$out")"
	token_byte_us="$(sed -n 's/^token_byte_us=\([0-9][0-9]*\).*/\1/p' <<<"$out")"
	dispatch_string_us="$(sed -n 's/^dispatch_string_us=\([0-9][0-9]*\).*/\1/p' <<<"$out")"
	dispatch_byte_us="$(sed -n 's/^dispatch_byte_us=\([0-9][0-9]*\).*/\1/p' <<<"$out")"
	[[ -n "$string_us" && -n "$byte_us" && -n "$token_string_us" && -n "$token_byte_us" && -n "$dispatch_string_us" && -n "$dispatch_byte_us" ]] || fail "cannot parse benchmark output"
	printf '%s %s %s %s %s %s %s\n' "$i" "$string_us" "$byte_us" "$token_string_us" "$token_byte_us" "$dispatch_string_us" "$dispatch_byte_us" >>"$rows"
done

mkdir -p "$(dirname "$report")"
python3 - "$rows" "$report" "$("$surge" version --full | tr '\n' ' ' | sed 's/[[:space:]]*$//')" "$repeats" <<'PY'
import datetime as dt
import statistics
import sys

rows_path, report_path, surge_version, repeats = sys.argv[1:5]
rows = []
with open(rows_path, "r", encoding="utf-8") as f:
    for line in f:
        run, string_us, byte_us, token_string_us, token_byte_us, dispatch_string_us, dispatch_byte_us = line.split()
        rows.append((int(run), int(string_us), int(byte_us), int(token_string_us), int(token_byte_us), int(dispatch_string_us), int(dispatch_byte_us)))

string_vals = [r[1] for r in rows]
byte_vals = [r[2] for r in rows]
token_string_vals = [r[3] for r in rows]
token_byte_vals = [r[4] for r in rows]
dispatch_string_vals = [r[5] for r in rows]
dispatch_byte_vals = [r[6] for r in rows]
median_string = statistics.median(string_vals)
median_byte = statistics.median(byte_vals)
median_token_string = statistics.median(token_string_vals)
median_token_byte = statistics.median(token_byte_vals)
median_dispatch_string = statistics.median(dispatch_string_vals)
median_dispatch_byte = statistics.median(dispatch_byte_vals)
speedup = median_string / median_byte if median_byte else 0.0
token_speedup = median_token_string / median_token_byte if median_token_byte else 0.0
dispatch_speedup = median_dispatch_string / median_dispatch_byte if median_dispatch_byte else 0.0

with open(report_path, "w", encoding="utf-8") as f:
    generated = dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    f.write("# Native byte line benchmark\n\n")
    f.write(f"Generated: {generated}\n\n")
    f.write("## Environment\n\n")
    f.write(f"- surge: {surge_version}\n")
    f.write("- fixture: benchmarks/native/byte_lines\n")
    f.write(f"- repeats: {repeats}\n\n")
    f.write("## Results\n\n")
    f.write("| run | string line us | byte line us | speedup |\n")
    f.write("| ---: | ---: | ---: | ---: |\n")
    for run, string_us, byte_us, _, _, _, _ in rows:
        run_speedup = string_us / byte_us if byte_us else 0.0
        f.write(f"| {run} | {string_us} | {byte_us} | {run_speedup:.2f}x |\n")
    f.write("\n")
    f.write(f"Median line speedup: {speedup:.2f}x\n\n")
    f.write("| run | string token us | byte token us | speedup |\n")
    f.write("| ---: | ---: | ---: | ---: |\n")
    for run, _, _, token_string_us, token_byte_us, _, _ in rows:
        run_speedup = token_string_us / token_byte_us if token_byte_us else 0.0
        f.write(f"| {run} | {token_string_us} | {token_byte_us} | {run_speedup:.2f}x |\n")
    f.write("\n")
    f.write(f"Median token speedup: {token_speedup:.2f}x\n\n")
    f.write("| run | string dispatch us | byte dispatch us | speedup |\n")
    f.write("| ---: | ---: | ---: | ---: |\n")
    for run, _, _, _, _, dispatch_string_us, dispatch_byte_us in rows:
        run_speedup = dispatch_string_us / dispatch_byte_us if dispatch_byte_us else 0.0
        f.write(f"| {run} | {dispatch_string_us} | {dispatch_byte_us} | {run_speedup:.2f}x |\n")
    f.write("\n")
    f.write(f"Median dispatch speedup: {dispatch_speedup:.2f}x\n")

print(f"median_string_line_us={median_string:.0f} median_byte_line_us={median_byte:.0f} line_speedup={speedup:.2f}x median_string_token_us={median_token_string:.0f} median_byte_token_us={median_token_byte:.0f} token_speedup={token_speedup:.2f}x median_string_dispatch_us={median_dispatch_string:.0f} median_byte_dispatch_us={median_dispatch_byte:.0f} dispatch_speedup={dispatch_speedup:.2f}x report={report_path}")
PY
