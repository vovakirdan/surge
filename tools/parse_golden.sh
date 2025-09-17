#!/usr/bin/env bash
set -euo pipefail

BIN="build/bin/surge"
IN_DIR="tests/parsing/inputs"
OUT_DIR="build/tmp/parsing"
GOLD_DIR="tests/parsing/golden"

cmd="${1:-check}"

mkdir -p "$OUT_DIR" "$GOLD_DIR"

if [ ! -x "$BIN" ]; then
  echo "[parse] surge binary not found: $BIN" >&2
  exit 1
fi

shopt -s nullglob
files=("$IN_DIR"/*.sg)
shopt -u nullglob

if [ ${#files[@]} -eq 0 ]; then
  echo "[parse] no input files in $IN_DIR"
  exit 0
fi

case "$cmd" in
  update)
    echo "[parse] updating golden AST snapshots..."
    for f in "${files[@]}"; do
      base="$(basename "$f" .sg)"
      out="$GOLD_DIR/$base.ast"
      echo "  -> $base"
      "$BIN" "$f" > "$out"
    done
    echo "[parse] done."
    ;;
  check)
    echo "[parse] checking AST snapshots..."
    rc=0
    for f in "${files[@]}"; do
      base="$(basename "$f" .sg)"
      tmp="$OUT_DIR/$base.ast"
      gold="$GOLD_DIR/$base.ast"
      echo "  -> $base"
      "$BIN" "$f" > "$tmp"
      if [ ! -f "$gold" ]; then
        echo "     [warn] missing golden: $gold (run: make parse-golden-update)"
        continue
      fi
      if ! diff -u "$gold" "$tmp" >/dev/null; then
        echo "     [FAIL] diff found:"
        diff -u "$gold" "$tmp" || true
        rc=1
      else
        echo "     [OK]"
      fi
    done
    exit $rc
    ;;
  *)
    echo "Usage: $0 {check|update}" >&2
    exit 2
    ;;
esac
