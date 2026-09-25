#!/usr/bin/env bash
# Diagnose every probe with a base and an after compiler (`surge diag --format short`),
# printing: probe, base exit, after exit, then each compiler's error lines.
# Usage (from the repository root): BASE=<binary> AFTER=<binary> run_probes.sh
set -u
: "${BASE:?}" "${AFTER:?}"
export SURGE_STDLIB="$PWD"
for f in cloud-work/untyped-literal-error/probes/*.sg cloud-work/untyped-literal-error/probes/cascade/*.sg; do
	b=$("$BASE" diag --format short "$f" 2>&1); bs=$?
	a=$("$AFTER" diag --format short "$f" 2>&1); as=$?
	printf '== %s\tbase=%s\tafter=%s\n' "${f#cloud-work/untyped-literal-error/probes/}" "$bs" "$as"
	printf '%s\n' "$b" | sed -n '1,4{/^\(error\|Error\)/s/^/  base:  /p}'
	printf '%s\n' "$a" | sed -n '1,4{/^\(error\|Error\)/s/^/  after: /p}'
done
