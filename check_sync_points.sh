#!/bin/bash
# Runtime V2 sync-point proving-spike static gate.
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
#   5. NAMED: every declared enumerator has a row in the rt_sp_name table in
#      rt_sync_point.c, and each row returns its own case's spelling.
set -u

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

ROOT="$(cd "$(dirname "$0")" && pwd)"
NATIVE="$ROOT/runtime/native"
HEADER="$NATIVE/rt_sync_point.h"
IMPL="$NATIVE/rt_sync_point.c"
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
    [SP_TASK_POLL_AFTER_JOIN_REGISTER]="rt_async_task.c"
    [SP_BLOCKING_POLL_BEFORE_WAIT_REGISTER]="rt_async_blocking.c"
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
    [SP_REMOTE_SPAWN_BEFORE_DISPATCH]="rt_remote_spawn.c"
    [SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH]="rt_remote_spawn.c"
    [SP_REMOTE_SPAWN_BEFORE_ACK]="rt_remote_spawn.c"
    [SP_IMMEDIATE_ON_BEFORE_DISPATCH]="rt_immediate_on.c"
    [SP_IMMEDIATE_ON_BEFORE_PUBLISH]="rt_immediate_on.c"
    [SP_IMMEDIATE_ON_AFTER_PUBLISH]="rt_immediate_on.c"
    [SP_READY_REQUEUE_BEFORE_LOCK]="rt_ready_queue.c"
    [SP_WAKE_BEFORE_STALE_REMOVAL]="rt_task_park.c"
    [SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY]="rt_far_channel_select.c"
    [SP_FAR_SELECT_BEFORE_DISPATCH]="rt_far_channel_select.c"
    [SP_CARRIER_JUMBO_ADMITTED]="rt_transport.c"
    [SP_CARRIER_CREDIT_PARKED]="rt_transport.c"
    [SP_SLEEP_FIRED_BEFORE_WAKE]="rt_async_sleep.c"
    [SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY]="rt_async_scope.c"
    [SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT]="rt_async_poll.c"
    [SP_MARKDONE_AFTER_SEAL_BEFORE_DONE]="rt_task_complete.c"
    [SP_CHANNEL_LAST_RELEASE_BEFORE_FREE]="rt_channel_refcount.c"
    [SP_BLOCKING_POP_BEFORE_STATUS]="rt_async_blocking.c"
    [SP_BLOCKING_STATE_BEFORE_BODY]="rt_async_blocking.c"
    [SP_BLOCKING_SHUTDOWN_BEFORE_DRAIN]="rt_async_blocking.c"
    [SP_INLINE_CHILD_TAKEN_OFF_QUEUE]="rt_ready_queue.c"
    [SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH]="rt_async_scope.c"
    [SP_SCOPE_CHILD_DONE_AFTER_MEMBERSHIP_TAKE]="rt_async_scope.c"
)

# Cross-check the allowlist above against the enumerators actually declared in
# the header, so the two cannot drift.
header_names=$(grep -oE 'RT_SYNC_POINT_SP_[A-Z_]+' "$HEADER" | sed 's/^RT_SYNC_POINT_//' | sort -u)
allow_names=$(printf '%s\n' "${!WINDOW_FILE[@]}" | sort -u)
if [ "$header_names" != "$allow_names" ]; then
    note_fail "allowlist in check_sync_points.sh drifted from rt_sync_point.h enumerators"
    diff <(echo "$header_names") <(echo "$allow_names") || true
else
    note_ok "allowlist matches the header enumerators"
fi

# Check 5: every declared enumerator is REACHABLE BY NAME. The two checks above
# see the declaration and the call site; neither sees rt_sp_name in
# rt_sync_point.c, which is the table SURGE_SYNC_POINT is resolved through. A
# hook that is declared, called, and absent from that table passes checks 1-4
# and then aborts every stand that tries to arm it -- so the gate has to look at
# the table too.
#
# The rows are read as pairs, `case RT_SYNC_POINT_<X>:` with the `return "<Y>";`
# that follows it, because that yields both facts at once: which enumerators the
# table covers at all, and whether a row answers to its own name. A pair of rows
# whose strings were swapped would arm the wrong hook while both sets still
# matched the header, so the set alone is not enough.
table_pairs=$(awk '
    /^static const char\* rt_sp_name\(/ { inside = 1; next }
    inside && /^}/ { inside = 0 }
    inside && /case RT_SYNC_POINT_SP_/ {
        pending = $0
        sub(/^.*case RT_SYNC_POINT_/, "", pending)
        sub(/:.*$/, "", pending)
        next
    }
    inside && pending != "" && /return "SP_/ {
        named = $0
        sub(/^.*return "/, "", named)
        sub(/".*$/, "", named)
        print pending, named
        pending = ""
    }
' "$IMPL")
table_names=$(printf '%s\n' "$table_pairs" | awk 'NF { print $1 }' | sort -u)
if [ "$header_names" != "$table_names" ]; then
    note_fail "rt_sp_name in rt_sync_point.c does not name every rt_sync_point.h enumerator"
    comm -23 <(printf '%s\n' "$header_names") <(printf '%s\n' "$table_names") |
        sed 's/^/       declared but unnamed (no stand can arm it): /'
    comm -13 <(printf '%s\n' "$header_names") <(printf '%s\n' "$table_names") |
        sed 's/^/       named but not declared: /'
else
    note_ok "every declared sync point has a row in the rt_sp_name table"
fi
mismatched=$(printf '%s\n' "$table_pairs" | awk 'NF && $1 != $2 { print $1 " returns \"" $2 "\"" }')
if [ -n "$mismatched" ]; then
    note_fail "an rt_sp_name row answers to a name that is not its own enumerator"
    printf '%s\n' "$mismatched" | sed 's/^/       /'
else
    note_ok "every rt_sp_name row returns its own enumerator spelling"
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
        if nm "$obj" 2>/dev/null | grep -Eq 'rt_sync_point_|rt_carrier_liveness_'; then
            note_fail "$(basename "$src") contains a test rendezvous symbol in the tag-off build"
            sym_leak=1
        fi
    done
    [ "$sym_leak" -eq 0 ] && note_ok "no test rendezvous symbol in the release (tag-off) build"
fi

# Check 4: no build that PRODUCES anything arms the hooks. Only this gate's own
# Make target and the sync-point test builder may reference the arming macro or
# tag.
#
# C_STAND_FLAGS is the one named exception, and it is exempt because it never
# reaches a linker: it exists so the changed-file C scan analyses a TEST STAND
# with the same flags the stand's own harness compiles it with, and every use of
# it is -fsyntax-only, cppcheck, or clang-tidy. Check 4b below is what keeps
# that true -- the exemption is worth exactly as much as the proof beside it.
if grep -nE 'RT_TEST_SYNC_POINTS|surge_syncpoints' "$ROOT/Makefile" |
    grep -vE 'runtime-v2-syncpoint-check|check_sync_points|C_STAND_FLAGS|^[0-9]+:#' >/dev/null 2>&1; then
    note_fail "Makefile references RT_TEST_SYNC_POINTS/surge_syncpoints outside the syncpoint gate"
    grep -nE 'RT_TEST_SYNC_POINTS|surge_syncpoints' "$ROOT/Makefile" |
        grep -vE 'runtime-v2-syncpoint-check|check_sync_points|C_STAND_FLAGS|^[0-9]+:#'
else
    note_ok "no default Make target arms RT_TEST_SYNC_POINTS/surge_syncpoints"
fi

# Check 4b: the analysis-only exemption stays analysis-only. Every COMMAND that
# passes C_STAND_FLAGS must be a syntax-only compile or an analyser -- the moment
# one of them can emit an object, the exemption above stops being safe.
#
# The unit is the command, not the line and not the recipe: a recipe is one
# logical line held together by continuations, so a real compile planted beside
# the three analysers would inherit their words and pass unnoticed. So the
# continuations are joined and the result is split on `;` first.
stand_flag_bad=$(tr '\n' '\001' <"$ROOT/Makefile" |
    sed 's/\\\o001/ /g' |
    tr '\001;' '\n\n' |
    grep -F '$(C_STAND_FLAGS)' |
    grep -vE '\-fsyntax-only|cppcheck|clang-tidy|^[[:space:]]*#|^C_STAND_FLAGS[[:space:]]*:?=' || true)
if [ -n "$stand_flag_bad" ]; then
    note_fail "C_STAND_FLAGS reaches a build that is not analysis-only:"
    printf '%s\n' "$stand_flag_bad"
else
    note_ok "every C_STAND_FLAGS use is analysis-only (no object can carry the hooks)"
fi

if [ "$fail" -ne 0 ]; then
    printf "${RED}>> sync-point static gate FAILED${NC}\n"
    exit 1
fi
printf "${GREEN}>> sync-point static gate passed${NC}\n"
