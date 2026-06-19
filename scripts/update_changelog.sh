#!/usr/bin/env bash
set -euo pipefail

usage() {
	echo "usage: $0 <version> <git-range> [<version> <git-range> ...]" >&2
	exit 2
}

if [[ $# -lt 2 || $(( $# % 2 )) -ne 0 ]]; then
	usage
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$(mktemp "$root/.CHANGELOG.md.out.XXXXXX")"
trimmed="$(mktemp "$root/.CHANGELOG.md.trimmed.XXXXXX")"
trap 'rm -f "$out" "$trimmed"' EXIT

section_order=(
	"Language"
	"Diagnostics"
	"Compiler"
	"Runtime"
	"Standard Library"
	"CLI / Tooling"
	"Documentation"
	"Tests"
	"Other"
)

strip_subject() {
	local subject="$1"
	local lower="${subject,,}"
	case "$lower" in
		"vm async exit poll")
			printf '%s\n' "VM async exit poll"
			return
			;;
		"feat/entropy random uuid")
			printf '%s\n' "Entropy random UUID"
			return
			;;
	esac
	if [[ "$subject" == *": "* && -n "$(commit_type "$subject")" ]]; then
		local message="${subject#*: }"
		printf '%s\n' "${message^}"
	else
		printf '%s\n' "${subject^}"
	fi
}

commit_prefix() {
	local subject="$1"
	[[ "$subject" == *": "* ]] || return 0
	printf '%s\n' "${subject%%: *}"
}

commit_type() {
	local prefix type
	prefix="$(commit_prefix "$1")"
	type="${prefix%%!*}"
	type="${type%%(*}"
	if [[ "$type" =~ ^[[:alpha:]]+$ ]]; then
		printf '%s\n' "$type"
	fi
}

commit_scope() {
	local prefix scope
	prefix="$(commit_prefix "$1")"
	if [[ "$prefix" == *"("*")"* ]]; then
		scope="${prefix#*(}"
		scope="${scope%)*}"
		printf '%s\n' "$scope"
	fi
}

skip_subject() {
	local type="$1"
	local scope="$2"
	local lower="$3"
	[[ "$type" == "docs" && "$scope" == "changelog" ]] && return 0
	[[ "$type" == "chore" && "$lower" =~ ^chore\((release|deps|pr|pull)\) ]] && return 0
	return 1
}

domain_for_subject() {
	local subject="$1"
	local type scope lower
	type="$(commit_type "$subject")"
	scope="$(commit_scope "$subject")"
	lower="${subject,,}"

	if skip_subject "$type" "$scope" "$lower"; then
		printf 'Skip\n'
		return
	fi

	if [[ "$type" == "test" || "$lower" == *"golden"* ]]; then
		printf 'Tests\n'
		return
	fi
	if [[ "$type" == "docs" ]]; then
		printf 'Documentation\n'
		return
	fi
	if [[ "$scope" == runtime || "$scope" == vm || "$type" == "perf" || "$type" == "bench" || "$lower" == vm\ * || "$lower" == *"async exit poll"* ]]; then
		printf 'Runtime\n'
		return
	fi
	if [[ "$scope" == cli || "$scope" == install || "$scope" == tooling || "$scope" == lsp || "$scope" == module || "$lower" == *"issue fixer skill"* ]]; then
		printf 'CLI / Tooling\n'
		return
	fi
	if [[ "$scope" == stdlib* || "$scope" == hash || "$scope" == random || "$scope" == entropy || "$lower" == *"stdlib"* || "$lower" == *"random"* || "$lower" == *"uuid"* || "$lower" == *"entropy"* || "$lower" == *"duration intrinsics"* ]]; then
		printf 'Standard Library\n'
		return
	fi
	if [[ "$type" == "build" || "$type" == "ci" || "$type" == "chore" ]]; then
		printf 'CLI / Tooling\n'
		return
	fi
	if [[ "$scope" == result || "$scope" == attrs || "$lower" == *"compare"* || "$lower" == *"cast"* || "$lower" == *"ret "* || "$lower" == *"block expression"* ]]; then
		printf 'Language\n'
		return
	fi
	if [[ "$lower" == *"diagnos"* || "$lower" == *"warning"* || "$lower" == *"fix-it"* ]]; then
		printf 'Diagnostics\n'
		return
	fi
	if [[ "$scope" == compiler || "$scope" == frontend || "$scope" == sema || "$scope" == mir || "$scope" == llvm || "$scope" == driver || "$scope" == mono || "$scope" == target-selection ]]; then
		printf 'Compiler\n'
		return
	fi

	printf 'Other\n'
}

version_date() {
	local version="$1"
	local tag="v${version#v}"
	if git -C "$root" rev-parse --verify -q "${tag}^{commit}" >/dev/null; then
		git -C "$root" log -1 --format=%cs "$tag"
	else
		date +%F
	fi
}

print_release() {
	local version="${1#v}"
	local range="$2"
	declare -A items=()

	while IFS= read -r subject; do
		[[ -z "$subject" ]] && continue
		local domain
		domain="$(domain_for_subject "$subject")"
		[[ "$domain" == "Skip" ]] && continue
		items["$domain"]+="- $(strip_subject "$subject")"$'\n'
	done < <(git -C "$root" log --no-merges --reverse --format=%s "$range")

	printf '## [%s] - %s\n\n' "$version" "$(version_date "$version")"
	local domain
	for domain in "${section_order[@]}"; do
		[[ -z "${items[$domain]:-}" ]] && continue
		printf '### %s\n\n' "$domain"
		printf '%s\n' "${items[$domain]}"
	done
}

awk '
	/^## / { exit }
	{ print }
' "$root/CHANGELOG.md" > "$out"

while [[ $# -gt 0 ]]; do
	version="$1"
	range="$2"
	shift 2
	print_release "$version" "$range" >> "$out"
done

awk '
	NF { last = NR }
	{ lines[NR] = $0 }
	END {
		for (i = 1; i <= last; i++) {
			print lines[i]
		}
	}
' "$out" > "$trimmed"

mv "$trimmed" "$root/CHANGELOG.md"
