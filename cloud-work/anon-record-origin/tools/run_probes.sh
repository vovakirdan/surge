#!/usr/bin/env bash
# Diagnose every probe with a base and an after compiler; build and run the
# `@entrypoint` probes (b*, e1) with the after compiler on both backends.
# Usage (from the repository root): BASE=<binary> AFTER=<binary> run_probes.sh
# Prints one line per probe and command: probe, what, exit status, first line.
set -u
: "${BASE:?}" "${AFTER:?}"
export SURGE_STDLIB="$PWD"
dir=cloud-work/anon-record-origin/probes
first() { printf '%s' "$1" | head -n 1 | cut -c1-220; }
for f in "$dir"/*.sg; do
	name=$(basename "$f")
	for which in base after; do
		bin=$BASE
		[ "$which" = after ] && bin=$AFTER
		out=$("$bin" diag --format short "$f" 2>&1)
		printf '%s\tdiag-%s\t%s\t%s\n' "$name" "$which" "$?" "$(first "$out")"
	done
	case "$name" in b*|e1*)
		for backend in vm llvm; do
			out=$(timeout 300 "$AFTER" run --backend "$backend" "$f" 2>&1)
			printf '%s\trun-after-%s\t%s\t%s\n' "$name" "$backend" "$?" "$(first "$out")"
		done
		;;
	esac
done
