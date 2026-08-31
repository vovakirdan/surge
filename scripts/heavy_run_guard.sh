#!/usr/bin/env bash
# Fail-closed guard for repo-owned heavy entry points (make runtime-v2-*,
# behaviour-check*, valgrind/sanitizer rows). It refuses to let a heavy row
# start outside its stand -- a detached git worktree, pinned to a SHA, on the
# dedicated measurement host -- BEFORE anything compiles. See
# docs/runtime-v2-epics/RULES.md, Global Rule 19.
set -euo pipefail

usage() {
	cat >&2 <<'EOF'
usage: heavy_run_guard.sh [--root DIR] [--marker PATH] [--label NAME]

exit 0  the stand is valid (or this is the CI lane)
exit 2  usage error
exit 3  REFUSED: the stand is not valid
EOF
}

root="."
marker="/etc/surge-dedicated-runner"
label="heavy-run-guard"

while [ $# -gt 0 ]; do
	case "$1" in
	--root)
		root="${2:?--root requires a value}"
		shift 2
		;;
	--marker)
		marker="${2:?--marker requires a value}"
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
	reason="$1"
	sha="$(git -C "$root" rev-parse HEAD 2>/dev/null || echo UNKNOWN)"
	cat >&2 <<EOF
heavy-run-guard: ОТКАЗ — ${reason}.
Тяжёлые ряды идут только на выделенной 212.108.83.42, из worktree, закреплённого на SHA.

  ssh root@212.108.83.42
  H=${sha}
  git -C /srv/ci/prewarm-surge worktree add --detach /var/tmp/lane-\$H "\$H"
  cd /var/tmp/lane-\$H && export SURGE_STDLIB=\$PWD
  PATH=/usr/local/go/bin:\$PATH make ${label}

Локально разрешены дешёвые полосы: go test ./internal/gatecheck, make runtime-v2-file-size-check.
Правило: docs/runtime-v2-epics/RULES.md, Global Rule 19.
EOF
	exit 3
}

if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
	echo "heavy-run-guard: CI lane (GITHUB_ACTIONS), allowed"
	exit 0
fi

if [ ! -f "$marker" ]; then
	refuse "не выделенный хост (маркер $marker не найден)"
fi

if git -C "$root" symbolic-ref -q HEAD >/dev/null 2>&1; then
	branch="$(git -C "$root" symbolic-ref -q HEAD)"
	refuse "дерево на именованной ветке ($branch), а не на detached HEAD"
fi

status="$(git -C "$root" status --porcelain)"
if [ -n "$status" ]; then
	refuse "дерево грязное:
$(printf '%s\n' "$status" | head -5)"
fi

if [ -z "${SURGE_STDLIB:-}" ]; then
	refuse "SURGE_STDLIB не задана"
fi

stdlib_real="$(realpath "$SURGE_STDLIB" 2>/dev/null || true)"
root_real="$(realpath "$root" 2>/dev/null || true)"
if [ -z "$stdlib_real" ] || [ "$stdlib_real" != "$root_real" ]; then
	refuse "SURGE_STDLIB ($SURGE_STDLIB) указывает не на этот worktree ($root)"
fi

sha="$(git -C "$root" rev-parse HEAD)"
echo "heavy-run-guard: ${label} ok — ${sha}, marker=${marker}, stdlib=${SURGE_STDLIB}"
exit 0
