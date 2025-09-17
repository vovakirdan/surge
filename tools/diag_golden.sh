#!/usr/bin/env bash
set -euo pipefail

BIN="build/bin/surge"
IN_DIR="tests/diagnostics/inputs"
OUT_DIR="build/tmp/diagnostics"
GOLD_DIR="tests/diagnostics/golden"

cmd="${1:-check}"

mkdir -p "$OUT_DIR" "$GOLD_DIR"

if [ ! -x "$BIN" ]; then
  echo "[diag] surge binary not found: $BIN" >&2
  exit 1
fi

shopt -s nullglob
files=("$IN_DIR"/*.sg)
shopt -u nullglob

if [ ${#files[@]} -eq 0 ]; then
  echo "[diag] no input files in $IN_DIR"
  exit 0
fi

case "$cmd" in
  update)
    echo "[diag] updating golden diagnostics..."
    for f in "${files[@]}"; do
      base="$(basename "$f" .sg)"
      out="$GOLD_DIR/$base.out"
      echo "  -> $base"
      set +e
      "$BIN" diag "$f" > "$out" 2>&1
      set -e
    done
    echo "[diag] done."
    ;;
  check)
    echo "[diag] checking diagnostics..."
    rc=0
    for f in "${files[@]}"; do
      base="$(basename "$f" .sg)"
      tmp="$OUT_DIR/$base.out"
      gold="$GOLD_DIR/$base.out"
      echo "  -> $base"
      set +e
      "$BIN" diag "$f" > "$tmp" 2>&1
      code=$?
      set -e
      if [ ! -f "$gold" ]; then
        echo "     [warn] missing golden: $gold (run: make diag-golden-update)"
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
