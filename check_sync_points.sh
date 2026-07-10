#!/bin/bash
# Runtime V2 Epic 9 proving-spike static gate.
#
# Proves the test-only RT_SYNC_POINT hooks can never sit on the worker steady
# path of a shipping build, and that every hook is on the named allowlist in
# its designated window. Wired into `make runtime-v2-check` via
# `runtime-v2-syncpoint-check`. See docs/runtime-v2-epics/09-tasks/
# 01-proving-spike-sync-points.md.
#
# Checks:
#   1. NEGATIVE-SYMBOL: compile every runtime source WITHOUT RT_TEST_SYNC_POINTS
#      and assert no object references rt_sync_point_reach (the rendezvous
#      entry). Proves the release build links no hook code.
#   2. ALLOWLIST: every RT_SYNC_POINT(<name>) uses an allowlisted enumerator.
#   3. PLACEMENT: each name appears only in its designated window file.
#   4. NO-DEFAULT-ARMING: no default build path defines RT_TEST_SYNC_POINTS or
#      passes -tags surge_syncpoints (only the syncpoint check may).
set -u

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

ROOT="$(cd "$(dirname "$0")" && pwd)"
NATIVE="$ROOT/runtime/native"
HEADER="$NATIVE/rt_sync_point.h"
fail=0

note_fail() { printf "${RED}FAIL${NC} %s\n" "$1"; fail=1; }
note_ok() { printf "${GREEN}OK${NC}   %s\n" "$1"; }

# Allowlist: the RT_SYNC_POINT_SP_* enumerators declared in the header, and the
# file(s) each is permitted to appear in.
declare -A WINDOW_FILE=(
    [SP_CANCEL_BEFORE_WAKE]="rt_task_complete.c"
    [SP_PARK_BEFORE_WAITING]="rt_async_poll.c rt_worker_turn.c"
    [SP_MARKDONE_BEFORE_DONEWAITERS_LOAD]="rt_task_complete.c"
    [SP_AWAIT_AFTER_INCREMENT]="rt_async_task.c"
    [SP_AWAIT_BEFORE_DONECV_WAIT]="rt_async_task.c"
    [SP_WAKEKEY_MID_DRAIN]="rt_task_park.c"
    [SP_MIGRATE_GAP]="rt_scheduler_placement.c rt_waiter_route.c"
    [SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK]="rt_transport.c"
    [SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK]="rt_transport.c"
    [SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD]="rt_transport.c"
    [SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE]="rt_transport.c"
    [SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND]="rt_transport.c"
    [SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE]="rt_transport.c"
    [SP_REMOTE_TASK_AFTER_OWNER_REGISTER]="rt_remote_task_dispatch.c"
    [SP_REMOTE_TASK_BEFORE_OWNER_REGISTER]="rt_remote_task_dispatch.c"
    [SP_REMOTE_SPAWN_BEFORE_ACK]="rt_remote_spawn.c"
    [SP_IMMEDIATE_ON_BEFORE_PUBLISH]="rt_immediate_on.c"
)

# Cross-check the allowlist above against the enumerators actually declared in
# the header, so the two cannot drift.
header_names=$(grep -oE 'RT_SYNC_POINT_SP_[A-Z_]+' "$HEADER" | sed 's/^RT_SYNC_POINT_//' | sort -u)
allow_names=$(printf '%s\n' "${!WINDOW_FILE[@]}" | sort -u)
if [ "$header_names" != "$allow_names" ]; then
    note_fail "allowlist in check_sync_points.sh drifted from rt_sync_point.h enumerators"
    diff <(echo "$header_names") <(echo "$allow_names") || true
else
    note_ok "allowlist matches the 16 header enumerators"
fi

# Files that actually call a sync-point macro.
mapfile -t callers < <(grep -rlE 'RT_SYNC_POINT(_IF)?\(' "$NATIVE" --include='*.c' | sort)

# Check 2 + 3: allowlist membership and placement.
for f in "${callers[@]}"; do
    base="$(basename "$f")"
    while IFS= read -r name; do
        if [ -z "${WINDOW_FILE[$name]+set}" ]; then
            note_fail "$base calls RT_SYNC_POINT($name) which is not allowlisted"
            continue
        fi
        if ! [[ " ${WINDOW_FILE[$name]} " == *" $base "* ]]; then
            note_fail "RT_SYNC_POINT($name) in $base but its window is ${WINDOW_FILE[$name]}"
        fi
    done < <(
        tr '\n' ' ' <"$f" |
            grep -oE 'RT_SYNC_POINT\([[:space:]]*SP_[A-Z_]+[[:space:]]*\)|RT_SYNC_POINT_IF\([^)]*,[[:space:]]*SP_[A-Z_]+[[:space:]]*\)' |
            sed -E 's/.*(SP_[A-Z_]+).*/\1/' |
            sort -u
    )
done
[ "$fail" -eq 0 ] && note_ok "all RT_SYNC_POINT call sites are allowlisted and in their window"

# Check 1: negative-symbol. Compile the same source set the harness uses
# (rt_entry.c excluded) WITHOUT the arming macro; no object may reference the
# rendezvous entry.
if ! command -v clang >/dev/null 2>&1; then
    note_fail "clang not found; cannot run the negative-symbol gate"
else
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    sym_leak=0
    for src in "$NATIVE"/*.c; do
        [ "$(basename "$src")" = "rt_entry.c" ] && continue
        obj="$tmp/$(basename "$src").o"
        if ! clang -std=c11 -pthread -I "$NATIVE" -c "$src" -o "$obj" 2>"$tmp/err"; then
            note_fail "release compile of $(basename "$src") failed"
            cat "$tmp/err"
            continue
        fi
        if nm "$obj" 2>/dev/null | grep -q 'rt_sync_point_reach'; then
            note_fail "$(basename "$src") references rt_sync_point_reach in the tag-off build"
            sym_leak=1
        fi
    done
    [ "$sym_leak" -eq 0 ] && note_ok "no rt_sync_point_reach symbol in the release (tag-off) build"
fi

# Check 4: no default build arms the hooks. Only this gate's own Make target and
# the sync-point test builder may reference the arming macro/tag.
if grep -nE 'RT_TEST_SYNC_POINTS|surge_syncpoints' "$ROOT/Makefile" | grep -vE 'runtime-v2-syncpoint-check|check_sync_points' >/dev/null 2>&1; then
    note_fail "Makefile references RT_TEST_SYNC_POINTS/surge_syncpoints outside the syncpoint gate"
    grep -nE 'RT_TEST_SYNC_POINTS|surge_syncpoints' "$ROOT/Makefile" | grep -vE 'runtime-v2-syncpoint-check|check_sync_points'
else
    note_ok "no default Make target arms RT_TEST_SYNC_POINTS/surge_syncpoints"
fi

if [ "$fail" -ne 0 ]; then
    printf "${RED}>> sync-point static gate FAILED${NC}\n"
    exit 1
fi
printf "${GREEN}>> sync-point static gate passed${NC}\n"
