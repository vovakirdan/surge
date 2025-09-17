#!/usr/bin/env bash
set -euo pipefail

BIN="build/bin/surge"
IN_DIR_OK="tests/sema/ok"
IN_DIR_BAD="tests/sema/bad"
OUT_DIR="build/tmp/sema"
GOLD_DIR="tests/sema/golden"

cmd="${1:-check}"

mkdir -p "$OUT_DIR" "$GOLD_DIR"

if [ ! -x "$BIN" ]; then
  echo "[sema] surge binary not found: $BIN" >&2
  exit 1
fi

run_one () {
  local f="$1"; local out="$2"
  set +e
  "$BIN" sema "$f" > "$out" 2>&1
  set -e
}

case "$cmd" in
  update)
    echo "[sema] updating golden outputs..."
    for f in "$IN_DIR_OK"/*.sg; do
      [ -f "$f" ] || continue
      base="$(basename "$f" .sg)"
      run_one "$f" "$GOLD_DIR/${base}.out"
    done
    for f in "$IN_DIR_BAD"/*.sg; do
      [ -f "$f" ] || continue
      base="$(basename "$f" .sg)"
      run_one "$f" "$GOLD_DIR/${base}.out"
    done
    echo "[sema] done."
    ;;
  check)
    echo "[sema] checking..."
    rc=0
    for f in "$IN_DIR_OK"/*.sg "$IN_DIR_BAD"/*.sg; do
      [ -f "$f" ] || continue
      base="$(basename "$f" .sg)"
      tmp="$OUT_DIR/${base}.out"
      gold="$GOLD_DIR/${base}.out"
      run_one "$f" "$tmp"
      if [ ! -f "$gold" ]; then
        echo "  [warn] missing golden: $gold (run: make sema-golden-update)"
        continue
      fi
      if ! diff -u "$gold" "$tmp" >/dev/null; then
        echo "  [FAIL] $base"
        diff -u "$gold" "$tmp" || true
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
