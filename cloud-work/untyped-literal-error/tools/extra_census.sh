#!/usr/bin/env bash
# Diagnose every .sg under showcases/, benchmarks/, stdlib/ and core/ with a
# base and an after compiler (user form, SURGE_STDLIB=$ROOT) and print one line
# per file: path, base exit, after exit, whether the outputs are identical.
# Usage (from the repository root): BASE=<binary> AFTER=<binary> OUT=<dir> extra_census.sh
set -u
: "${BASE:?}" "${AFTER:?}" "${OUT:?}"
mkdir -p "$OUT/base" "$OUT/after"
export SURGE_STDLIB="$PWD"
find showcases benchmarks stdlib core -type f -name '*.sg' 2>/dev/null | LC_ALL=C sort | while read -r rel; do
	key="$(printf '%s' "$rel" | tr '/' '_')"
	timeout -k 5 120 "$BASE" diag "$rel" >"$OUT/base/$key.txt" 2>&1
	b=$?
	timeout -k 5 120 "$AFTER" diag "$rel" >"$OUT/after/$key.txt" 2>&1
	a=$?
	same=same
	cmp -s "$OUT/base/$key.txt" "$OUT/after/$key.txt" || same=DIFFERENT
	printf '%s\t%s\t%s\t%s\n' "$rel" "$b" "$a" "$same"
done
