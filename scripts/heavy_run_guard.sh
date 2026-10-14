#!/usr/bin/env bash
# Fail-closed guard for repo-owned heavy entry points. Local runs require an
# explicit opt-in and an absolute SURGE_STDLIB matching this worktree. A dirty
# development tree needs a second opt-in and is identified by a content hash;
# final landing evidence still comes from a clean exact SHA.
set -euo pipefail

usage() {
	cat >&2 <<'USAGE'
usage: heavy_run_guard.sh [--root DIR] [--label NAME]

exit 0  CI, or an explicitly opted-in matching local worktree
exit 2  usage error
exit 3  REFUSED: the stand is not valid
USAGE
}

root="."
label="heavy-run-guard"
while [ $# -gt 0 ]; do
	case "$1" in
	--root)
		root="${2:?--root requires a value}"
		shift 2
		;;
	--label)
		label="${2:?--label requires a value}"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "heavy-run-guard: unknown argument: $1" >&2
		usage
		exit 2
		;;
	esac
done

refuse() {
	cat >&2 <<EOF_REFUSE
heavy-run-guard: ОТКАЗ — $1.

Локальный тяжёлый ряд запускается явно:
  export SURGE_LOCAL_HEAVY=1
  export SURGE_STDLIB="$(realpath "$root" 2>/dev/null || printf '%s' "$root")"
  make ${label}

Для промежуточного запуска по грязному worktree дополнительно:
  export SURGE_LOCAL_HEAVY_ALLOW_DIRTY=1

Такой запуск получает content fingerprint. Финальное доказательство посадки всё
равно повторяется на чистом exact SHA. Ryzen и hosted CI не требуются.
Правило: docs/runtime-v2-epics/RULES.md, Global Rule 19.
EOF_REFUSE
	exit 3
}

if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
	echo "heavy-run-guard: CI lane (GITHUB_ACTIONS), allowed"
	exit 0
fi

[ "${SURGE_LOCAL_HEAVY:-}" = "1" ] || refuse "не задан явный local opt-in SURGE_LOCAL_HEAVY=1"
git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1 || refuse "$root не является git worktree"

if [ -z "${SURGE_STDLIB:-}" ]; then
	refuse "SURGE_STDLIB не задана"
fi
case "$SURGE_STDLIB" in
/*) ;;
*) refuse "SURGE_STDLIB должен быть абсолютным путём" ;;
esac
stdlib_real="$(realpath "$SURGE_STDLIB" 2>/dev/null || true)"
root_real="$(realpath "$root" 2>/dev/null || true)"
if [ -z "$stdlib_real" ] || [ "$stdlib_real" != "$root_real" ]; then
	refuse "SURGE_STDLIB ($SURGE_STDLIB) указывает не на этот worktree ($root)"
fi

sha="$(git -C "$root" rev-parse HEAD)"
status="$(git -C "$root" status --porcelain)"
if [ -n "$status" ] && [ "${SURGE_LOCAL_HEAVY_ALLOW_DIRTY:-}" != "1" ]; then
	refuse "дерево грязное; для промежуточного fingerprinted запуска нужен SURGE_LOCAL_HEAVY_ALLOW_DIRTY=1"
fi

if [ -z "$status" ]; then
	evidence="sha:${sha}"
	state="clean"
else
	evidence="$({
		printf 'HEAD\0%s\0' "$sha"
		git -C "$root" diff --binary HEAD
		while IFS= read -r -d '' path; do
			printf 'UNTRACKED\0%s\0' "$path"
			git -C "$root" hash-object -- "$path"
		done < <(git -C "$root" ls-files --others --exclude-standard -z)
	} | sha256sum | awk '{print $1}')"
	evidence="worktree:${evidence}"
	state="dirty-opt-in"
fi

branch="$(git -C "$root" symbolic-ref --short -q HEAD || printf 'detached')"
echo "heavy-run-guard: ${label} ok — head=${sha} branch=${branch} state=${state} evidence=${evidence} stdlib=${stdlib_real}"
