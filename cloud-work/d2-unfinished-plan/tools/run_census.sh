#!/usr/bin/env bash
# Rebuild census.json and clusters.json from scratch.
# Usage (from the repository root): cloud-work/d2-unfinished-plan/tools/run_census.sh [OUTDIR]
# Needs: go, python3 (3.9+), GNU coreutils timeout, xargs -P.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
HERE="$ROOT/cloud-work/d2-unfinished-plan"
OUTDIR="${1:-$(mktemp -d)}"
SURGE="${SURGE:-/tmp/surge}"
JOBS="${JOBS:-8}"
export ROOT OUTDIR SURGE
cd "$ROOT"
go build -o "$SURGE" ./cmd/surge
mkdir -p "$OUTDIR/out" "$OUTDIR/hout"
find testdata/golden -type f -name '*.sg' | LC_ALL=C sort >"$OUTDIR/files.txt"
xargs -a "$OUTDIR/files.txt" -P "$JOBS" -I{} bash "$HERE/tools/run_one.sh" {} >"$OUTDIR/status.tsv"
python3 "$HERE/tools/classify.py" "$ROOT" "$OUTDIR" "$OUTDIR/census.json" "$(git rev-parse HEAD)"
python3 "$HERE/tools/clusters.py" "$OUTDIR/census.json" >"$OUTDIR/clusters.json"
echo "census: $OUTDIR/census.json  clusters: $OUTDIR/clusters.json"
