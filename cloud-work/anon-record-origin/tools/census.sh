#!/usr/bin/env bash
# Census of every golden program with a prebuilt compiler, both command forms.
# Reuses run_one.sh and classify.py from branch cloud/d2-unfinished-plan
# (cloud-work/d2-unfinished-plan/tools), unchanged; unlike their run_census.sh
# it does not rebuild, so a base and an after binary can be run side by side.
# Usage: ROOT=<checkout> SURGE=<binary> OUTDIR=<dir> D2TOOLS=<d2 tools dir> [JOBS=n] census.sh
set -euo pipefail
: "${ROOT:?}" "${SURGE:?}" "${OUTDIR:?}" "${D2TOOLS:?}"
export ROOT OUTDIR SURGE
mkdir -p "$OUTDIR/out" "$OUTDIR/hout"
cd "$ROOT"
find testdata/golden -type f -name '*.sg' | LC_ALL=C sort >"$OUTDIR/files.txt"
xargs -a "$OUTDIR/files.txt" -P "${JOBS:-3}" -I{} bash "$D2TOOLS/run_one.sh" {} >"$OUTDIR/status.tsv"
python3 "$D2TOOLS/classify.py" "$ROOT" "$OUTDIR" "$OUTDIR/census.json" "$(git -C "$ROOT" rev-parse HEAD)"
echo "census: $OUTDIR/census.json"
