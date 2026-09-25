#!/usr/bin/env bash
# Diagnose one golden program in two forms, each under a per-file timeout.
#   user form:    SURGE_STDLIB=$ROOT surge diag <file>
#   harness form: SURGE_STDLIB=$ROOT surge diag --format short --directives=<off|collect> <file>
#                 (collect when the path contains /directives/, as scripts/golden_update.sh:179 does)
# $1 = repo-relative path. ROOT, OUTDIR and SURGE come from the environment.
# Output: $OUTDIR/out/<key>.txt (user form) and $OUTDIR/hout/<key>.txt (harness form),
# each ending with a line "exit_status=N"; key = path with every "/" replaced by "_".
set -u
rel="$1"
key="$(printf '%s' "$rel" | tr '/' '_')"
cd "$ROOT" || exit 99
mode=off
case "/$rel" in */directives/*) mode=collect ;; esac
timeout -k 5 120 env SURGE_STDLIB="$ROOT" "$SURGE" diag "$rel" >"$OUTDIR/out/$key.txt" 2>&1
st=$?
printf 'exit_status=%s\n' "$st" >>"$OUTDIR/out/$key.txt"
timeout -k 5 120 env SURGE_STDLIB="$ROOT" "$SURGE" diag --format short --directives="$mode" "$rel" >"$OUTDIR/hout/$key.txt" 2>&1
hst=$?
printf 'exit_status=%s\n' "$hst" >>"$OUTDIR/hout/$key.txt"
printf '%s\t%s\t%s\n' "$rel" "$st" "$hst"
