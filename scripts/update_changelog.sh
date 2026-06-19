#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
	echo "usage: $0 <version> [git-range]" >&2
	exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${1#v}"
tag="v${version}"
range="${2:-}"
tmp="$(mktemp)"
clean="$(mktemp)"
out="$(mktemp)"
trap 'rm -f "$tmp" "$clean" "$out"' EXIT

if [[ -n "$range" ]]; then
	git-cliff --config "$root/cliff.toml" --tag "$tag" --strip header --output "$tmp" "$range"
else
	git-cliff --config "$root/cliff.toml" --unreleased --tag "$tag" --strip header --output "$tmp"
fi

awk -v version="$version" '
	$0 ~ "^## \\[" version "\\]" {
		skip = 1
		next
	}
	skip && /^## / {
		skip = 0
	}
	!skip {
		print
	}
' "$root/CHANGELOG.md" > "$clean"

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
			print ""
			while ((getline line < generated) > 0) {
				print line
			}
		}
	}
' "$clean" > "$out"

mv "$out" "$root/CHANGELOG.md"
