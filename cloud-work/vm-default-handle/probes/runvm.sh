#!/usr/bin/env bash
# Usage: runvm.sh <repo-dir> <label>
# Runs the full internal/vm package on both backends, plain and with runtime_v2_pending,
# writing go test -json output per leg into $RESULTS_DIR/<label>-<backend>-<tags>.json
set -u
DIR="$1"; LABEL="$2"
OUT="${RESULTS_DIR:?set RESULTS_DIR}"; mkdir -p "$OUT"
cd "$DIR"
for backend in vm llvm; do
  for tags in plain pending; do
    tagflag=""
    [ "$tags" = pending ] && tagflag="-tags runtime_v2_pending"
    f="$OUT/$LABEL-$backend-$tags.json"
    start=$(date +%s)
    SURGE_STDLIB="$DIR" SURGE_SKIP_TIMEOUT_TESTS=1 SURGE_BACKEND=$backend \
      go test $tagflag ./internal/vm -count=1 -json --timeout 3000s > "$f" 2> "$f.stderr"
    rc=$?
    echo "$LABEL $backend $tags rc=$rc $(( $(date +%s) - start ))s" >> "$OUT/summary.txt"
  done
done
echo "$LABEL DONE" >> "$OUT/summary.txt"
