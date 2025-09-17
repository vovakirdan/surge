#!/usr/bin/env bash
set -euo pipefail

BIN="build/bin/surge"
IN_DIR="tests/lexing/inputs"
OUT_DIR="build/tmp/lexing"
GOLD_DIR="tests/lexing/golden"

cmd="${1:-check}"

mkdir -p "$OUT_DIR" "$GOLD_DIR"

if [ ! -x "$BIN" ]; then
  echo "[lex] surge binary not found: $BIN" >&2
  exit 1
fi

shopt -s nullglob
files=("$IN_DIR"/*.sg)
shopt -u nullglob

if [ ${#files[@]} -eq 0 ]; then
  echo "[lex] no input files in $IN_DIR"
  exit 0
fi

case "$cmd" in
  update)
    echo "[lex] updating golden outputs..."
    for f in "${files[@]}"; do
      base="$(basename "$f" .sg)"
      out="$GOLD_DIR/$base.tokens"
      echo "  -> $base"
      "$BIN" tokenize "$f" > "$out"
    done
    echo "[lex] done."
    ;;
  check)
    echo "[lex] checking against goldens..."
    rc=0
    for f in "${files[@]}"; do
      base="$(basename "$f" .sg)"
      tmp="$OUT_DIR/$base.tokens"
      gold="$GOLD_DIR/$base.tokens"
      echo "  -> $base"
      "$BIN" tokenize "$f" > "$tmp"
      if [ ! -f "$gold" ]; then
        echo "     [warn] missing golden: $gold (run: make lex-golden-update)"
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
