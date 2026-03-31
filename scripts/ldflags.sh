#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
base_version=""
pre_release=""

while [[ $# -gt 0 ]]; do
	case "$1" in
		--local)
			pre_release="-dev"
			shift
			;;
		--base-version)
			base_version="${2:-}"
			shift 2
			;;
		*)
			echo "unknown arg: $1" >&2
			exit 2
			;;
	esac
done

if [[ -z "$base_version" ]]; then
	latest_tag="$(
		git -C "$root" tag --sort=-version:refname 2>/dev/null |
			grep -E '^[vV]?[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$' |
			head -n 1 || true
	)"
	base_version="${latest_tag#v}"
	base_version="${base_version#V}"
fi

if [[ -z "$base_version" ]]; then
	base_version="$(sed -n 's/.*BaseVersion = "\([^"]*\)".*/\1/p' "$root/internal/version/version.go" | head -n 1)"
fi

git_commit="$(git -C "$root" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
git_message="$(git -C "$root" log -1 --pretty=%s 2>/dev/null || echo unknown)"
git_message_esc="$(printf '%s' "$git_message" | sed "s/'/'\"'\"'/g")"
build_date="${BUILD_DATE:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}"

printf "%s" "-X surge/internal/version.BaseVersion=${base_version} \
-X surge/internal/version.PreRelease=${pre_release} \
-X surge/internal/version.GitCommit=${git_commit} \
-X 'surge/internal/version.GitMessage=${git_message_esc}' \
-X surge/internal/version.BuildDate=${build_date}"
