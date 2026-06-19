#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: $0 <version>" >&2
	exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${1#v}"
tag="v${version}"
tmp="$(mktemp)"
out="$(mktemp)"
trap 'rm -f "$tmp" "$out"' EXIT

git-cliff --config "$root/cliff.toml" --unreleased --tag "$tag" --strip header --output "$tmp"

awk -v generated="$tmp" '
	!inserted && /^## / {
		while ((getline line < generated) > 0) {
			print line
		}
		print ""
		inserted = 1
	}
	{ print }
	END {
		if (!inserted) {
			while ((getline line < generated) > 0) {
				print line
			}
		}
	}
' "$root/CHANGELOG.md" > "$out"

mv "$out" "$root/CHANGELOG.md"
