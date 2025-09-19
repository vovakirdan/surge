#!/usr/bin/env bash
set -euo pipefail

BIN_SURGEC="build/bin/surgec"
BIN_SURGE="build/bin/surge"
IN_DIR="tests/codegen/inputs"
OUT_DIR="build/tmp/codegen"
GOLD_DIR="tests/codegen/golden"

cmd="${1:-check}"

mkdir -p "$OUT_DIR" "$GOLD_DIR"

if [ ! -x "$BIN_SURGEC" ]; then
  echo "[disasm] surgec binary not found: $BIN_SURGEC" >&2
  exit 1
fi

if [ ! -x "$BIN_SURGE" ]; then
  echo "[disasm] surge binary not found: $BIN_SURGE" >&2
  exit 1
fi

shopt -s nullglob
files=("$IN_DIR"/*.sg)
shopt -u nullglob

if [ ${#files[@]} -eq 0 ]; then
  echo "[disasm] no input files in $IN_DIR"
  exit 0
fi

run_one () {
  local src="$1"
  local base="$2"
  local tmp_sbc="$OUT_DIR/${base}.sbc"
  local tmp_out="$OUT_DIR/${base}.dasm"
  "$BIN_SURGEC" "$src" -o "$tmp_sbc" >/dev/null
  "$BIN_SURGE" disasm "$tmp_sbc" > "$tmp_out"
}

case "$cmd" in
  update)
    echo "[disasm] updating golden outputs..."
    for src in "${files[@]}"; do
      base="$(basename "$src" .sg)"
      echo "  -> $base"
      run_one "$src" "$base"
      mv "$OUT_DIR/${base}.dasm" "$GOLD_DIR/${base}.dasm"
    done
    echo "[disasm] done."
    ;;
  check)
    echo "[disasm] checking against goldens..."
    rc=0
    for src in "${files[@]}"; do
      base="$(basename "$src" .sg)"
      run_one "$src" "$base"
      gold="$GOLD_DIR/${base}.dasm"
      tmp_out="$OUT_DIR/${base}.dasm"
      if [ ! -f "$gold" ]; then
        echo "  [warn] missing golden: $gold (run: make disasm-golden-update)"
        continue
      fi
      if ! diff -u "$gold" "$tmp_out" >/dev/null; then
        echo "  [FAIL] $base"
        diff -u "$gold" "$tmp_out" || true
        rc=1
      else
        echo "  [OK] $base"
      fi
    done
    exit $rc
    ;;
  *)
    echo "Usage: $0 {check|update}" >&2
    exit 2
    ;;
esac
