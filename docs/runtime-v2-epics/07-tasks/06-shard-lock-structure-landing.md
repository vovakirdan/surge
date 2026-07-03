# Epic 7 Task 6: Shard Lock Structure Landing

**Kind:** runtime code. **Depends on:** Tasks 3, 5.

**Goal:** land the lock-split structure with behavior identical: per-shard
mutex and condvars with lifecycle, the two-lane lock API with the D2 order
assertion, the waiter `owner_hint` field, and delegation of the legacy
`rt_lock`/`rt_unlock` helpers to the control lane so lane tracking is
consistent from this commit on. No path stops taking the global lock yet.

## Scope

- New `runtime/native/rt_lane.c`: `rt_control_lock/unlock`,
  `rt_shard_lock/unlock`, `rt_lane_debug_enabled`, thread-local lane
  tracking that panics on order violations (control while holding a shard
  lock; a second shard lock; reentrant control), and
  `rt_shard_sync_init/destroy` for the shard mutex + `worker_cv` +
  `poller_cv` lifecycle with explicit status codes.
- `rt_async_internal.h`: `rt_shard.lock/worker_cv/poller_cv`,
  `waiter.owner_hint`, new declarations.
- `rt_runtime.c`: shard sync init in `rt_shard_init` (failure unwinds
  partial init), destroy in `rt_shard_destroy`.
- `rt_async_state.c`: `rt_lock`/`rt_unlock` become one-line delegates to the
  control lane (they die entirely in Task 11).
- `rt_async_waiter.c`: `add_waiter` populates `owner_hint` from the task's
  owner shard (0 for tasks without one until Task 7 universal assignment).

## Behavior Contract

Identical observable behavior: the control lane is the same `ex->lock`; no
call path takes a shard lock yet; the Task 4 suite and all standing gates
stay green. The Task 5 gates `ShardSyncShape` and `LaneAPIShape` flip to
green; the other six stay red by design.

## Checks

`make c-check`, `make cppcheck`, focused Task 4 suite (both shard configs),
`timeout 600s make runtime-v2-check`, `make check` (pre-commit),
`./check_file_sizes.sh -a`, `git diff --check`, Sentrux root/runtime/native.

## Success Criteria

- Lane API + shard sync structure in place; order assertion active on every
  lane acquisition.
- `ShardSyncShape` + `LaneAPIShape` green; behavior gates unchanged.
- Evidence, `NOTES.md`, index updated; own commit.
