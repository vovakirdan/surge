#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin="$root/surge"
action="${1:-check}"

# ensure binary is built
if [[ ! -x "$bin" ]]; then
  echo ">> building surge binary"
  (cd "$root" && go build -ldflags "-X surge/internal/version.GitCommit=dev -X surge/internal/version.GitMessage=dev -X surge/internal/version.BuildDate=dev" -o surge ./cmd/surge/)
fi

status=0
find "$root/testdata/golden" -type f -name '*.sg' -print0 | sort -z | while IFS= read -r -d '' file; do
  base="$(basename "$file")"
  [[ "$base" == _* ]] && continue

  rel="${file#$root/}"
  expected="${file}.golden"
  tmp="$(mktemp)"

  diag_status=0
  if ! "$bin" diag --format=json --stages=all --with-notes --max-diagnostics=500 "$file" >"$tmp" 2>&1; then
    diag_status=$?
  fi
  echo "exit_status=$diag_status" >>"$tmp"

  if [[ "$action" == "update" ]]; then
    mv "$tmp" "$expected"
    echo "updated $rel"
    continue
  fi

  if [[ ! -f "$expected" ]]; then
    echo "missing golden for $rel (expected $expected)"
    status=1
    rm -f "$tmp"
    continue
  fi

  if ! diff -u "$expected" "$tmp"; then
    echo "golden mismatch for $rel"
    status=1
  fi
  rm -f "$tmp"
done

exit $status
