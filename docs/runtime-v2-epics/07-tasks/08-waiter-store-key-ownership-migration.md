# Epic 7 Task 8: Waiter-Store Key Ownership Migration

**Kind:** runtime code. **Depends on:** Task 7.

**Goal:** waiter entries live with their key's owner (dependency map section
5): join/timer/blocking keys in the parked-on task's owner shard store,
scope keys in a new control-lane store, net keys unchanged (fd owner),
channel keys explicitly deferred to Task 10 on the shard-0 compatibility
store. All access stays under the control lock (nested phase); the Task 11
peel adds the shard-lock acquisitions.

## Scope

- Pure-move pre-step: waiter types and declarations extracted from
  `rt_async_internal.h` into `rt_waiter.h` (header was at 499/500).
- `rt_waiter_route.c`: `rt_waiter_store_for_key` resolver and
  `rt_waiter_migrate_join_waiters`.
- `add_waiter`/`remove_waiter`/`pop_waiter` non-net arms and
  `wake_key_all_with_policy` resolve stores per key.
- `rt_executor.control_waiters` store for scope keys; the waiter trace
  aggregation scans it as a synthetic extra index.
- Owner re-placement migrates join waiters: `rt_task_replace_owner` wraps
  `rt_task_set_placement` at all three accept-transition sites
  (`rt_async_waiter.c` accept completion, `rt_net_accept_group.c`
  ready-now and place-current paths), moving `join_key(task)` entries from
  the old owner's store so completion wakes never scan a stale shard.

## Ownership Stability Argument

Store resolution must be identical at registration and removal. Join/timer/
blocking keys carry the target task id; the target's owner only changes in
the accept transition, which now migrates the entries under the same control
lock. A `get_task(key.id) == NULL` fallback (target already freed) cannot
strand entries: `mark_done` drains `join_key` entries before the task can be
freed.

## Checks

`make c-check`, `make cppcheck`, Task 4 suite (9/9 x 2 configs),
`timeout 600s make runtime-v2-check` twice, `make check` (pre-commit),
`./check_file_sizes.sh -a`, `git diff --check`, Sentrux scans. The
owner-local waiter stub harness gained stubs for `rt_task_owner_shard` /
`rt_task_replace_owner` and includes `rt_waiter_route.c`.

## Success Criteria

- Per-key ownership routing active with join-waiter migration; behavior
  gates unchanged; evidence/NOTES/index updated; own commits (header move,
  then routing).
