#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$root/benchmarks/native/byte_ranges"
report="${SURGE_BYTES_BENCH_REPORT:-$root/build/benchmarks/native-byte-ranges.md}"
repeats="${SURGE_BYTES_BENCH_REPEATS:-5}"
surge="${SURGE:-$root/surge}"

fail() {
	echo "bench_native_bytes: $*" >&2
	exit 1
}

if [[ ! -x "$surge" ]]; then
	surge="$(command -v surge || true)"
fi
[[ -n "$surge" && -x "$surge" ]] || fail "surge binary not found; run 'make build' or set SURGE=/path/to/surge"
command -v python3 >/dev/null || fail "python3 not found"

export SURGE_STDLIB="${SURGE_BYTES_BENCH_STDLIB:-$root}"

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
	copied="$(sed -n 's/^rounds=.* copied_bytes=\([0-9][0-9]*\).*/\1/p' <<<"$out")"
	push_us="$(sed -n 's/^push_loop_us=\([0-9][0-9]*\).*/\1/p' <<<"$out")"
	append_us="$(sed -n 's/^append_range_us=\([0-9][0-9]*\).*/\1/p' <<<"$out")"
	[[ -n "$copied" && -n "$push_us" && -n "$append_us" ]] || fail "cannot parse benchmark output"
	printf '%s %s %s %s\n' "$i" "$copied" "$push_us" "$append_us" >>"$rows"
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
        run, copied, push_us, append_us = line.split()
        rows.append((int(run), int(copied), int(push_us), int(append_us)))

push = [r[2] for r in rows]
append = [r[3] for r in rows]
median_push = statistics.median(push)
median_append = statistics.median(append)
speedup = median_push / median_append if median_append else 0.0
copied = rows[-1][1] if rows else 0

with open(report_path, "w", encoding="utf-8") as f:
    f.write("# Native byte range benchmark\n\n")
    generated = dt.datetime.now(dt.UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
    f.write(f"Generated: {generated}\n\n")
    f.write("## Environment\n\n")
    f.write(f"- surge: {surge_version}\n")
    f.write("- fixture: benchmarks/native/byte_ranges\n")
    f.write(f"- repeats: {repeats}\n")
    f.write(f"- copied bytes per run: {copied}\n\n")
    f.write("## Results\n\n")
    f.write("| run | copied bytes | push loop us | append range us | speedup |\n")
    f.write("| ---: | ---: | ---: | ---: | ---: |\n")
    for run, copied_bytes, push_us, append_us in rows:
        run_speedup = push_us / append_us if append_us else 0.0
        f.write(f"| {run} | {copied_bytes} | {push_us} | {append_us} | {run_speedup:.2f}x |\n")
    f.write("\n")
    f.write(f"Median speedup: {speedup:.2f}x\n")

print(f"median_push_us={median_push:.0f} median_append_range_us={median_append:.0f} speedup={speedup:.2f}x report={report_path}")
PY
