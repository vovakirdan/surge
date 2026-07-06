# Epic 9 Task 3: RV2-DEBT-020 — Accept-Transition Join-Waiter Migration Proof

**Status:** complete; fixed by a generic join-route protocol and deterministic
positive/negative proof.
**Kind:** runtime fix + proof. The proof exposed a real late-registration
stranding path in the old order, so this task did not close as proof-only.

## Why This Task Exists

`rt_waiter_migrate_join_waiters` moves `join_key(task)` waiters from a source
owner shard to a destination owner shard during `rt_task_replace_owner`. The
move extracts under the source shard lock and appends under the destination
shard lock, never holding both shard locks at once. Since Epic 8, a joiner's
`rt_task_poll` registration is target-owner-shard-local, not serialized by the
old global control lock. A joiner that registers during an owner-replace gap
could remain on the old shard while future completion wakes route to the new
owner.

Task 3 proved the old protocol could strand a `join_key` waiter registered in
the migration gap, then replaced the protocol with route publication plus
join-route revalidation for add/remove/pop/collect-all wake.

## Known Risk In The Earlier Draft

An earlier local draft attempted to narrow this debt to a net-handle / stdlib
ABI question. That is not accepted. The proof was too broad and did not account
for all `rt_task_replace_owner` callers, especially the F2 adoption shape where
the re-owned task is the current joiner rather than the completed child.

This task therefore starts from the current code, not from the abandoned
narrowing conclusion.

## Caller Enumeration

| Caller | Re-owned task/status | Can a join waiter exist? | Outcome |
| --- | --- | --- | --- |
| `runtime/native/rt_async_task.c:278` (`rt_task_poll_adopt_placement`) | `current`, RUNNING, after consuming a DONE connection-placed child. | Yes. The current task may already have cloned handles. | In scope; generic fix covers it. Completion cannot happen before migration returns because the re-owned task is the executing poll task. |
| `runtime/native/rt_async_waiter.c:381` (accept wake) | waiter popped from the net-accept store, normally WAITING, before `wake_net_task`. | Yes. A cloned task handle can await it. | In scope; generic fix covers it. Completion cannot happen before migration returns because the task is not enqueued until after owner replacement. |
| `runtime/native/rt_net_accept_group.c:101` (accept ready-now) | `rt_current_task()`, RUNNING. | Yes. Handle clones can exist while the task is inside `rt_net_wait_accept`. | In scope; generic fix covers it. Completion cannot happen before migration returns because the re-owned task is the executing task. |
| `runtime/native/rt_net_accept_group.c:111` via `runtime/native/rt_net.c:516` (accept success self-placement) | `rt_current_task()`, RUNNING. | Yes. Handle clones can exist while native accept succeeds. | In scope; generic fix covers it. Completion cannot happen before migration returns because the re-owned task is the executing task. |

Handle escape is possible: async lowering polls task handles through
`rt_task_poll` (`internal/backend/llvm/emit_async.go:313`), and native handles
can be cloned by `rt_task_clone` (`runtime/native/rt_async_task.c:382-393`).
The abandoned net-handle/stdlib narrowing is not used as evidence.

## Fix

The fix is generic and applies to every owner replacement:

- `rt_task` now has `join_owner_shard_id` and acquire/release accessors
  (`runtime/native/rt_async_internal.h:193`, `:230-241`).
- `rt_task_replace_owner` routes join waiters through that field, not directly
  through scheduler placement, and calls
  `rt_waiter_publish_join_owner_and_migrate` before publishing the scheduler
  owner (`runtime/native/rt_scheduler_placement.c:90-105`).
- The production migration publishes the new join route while holding the old
  route shard lock, then drains old entries
  (`runtime/native/rt_waiter_route.c:150-188`). A stale registrar that already
  selected the old route either
  inserts before publication and is drained, or re-reads the changed route under
  that same old lock and retries.
- `WAKER_JOIN` add/remove/pop and completion collect-all wake all use the same
  route protocol:
  `lock_join_waiter_route` resolves the route, locks that shard, re-reads the
  route under the lock, and retries on mismatch
  (`runtime/native/rt_waiter_join_route.c`;
  `runtime/native/rt_async_waiter.c`; `runtime/native/rt_task_park.c`).

This is not publish-before-migrate alone. Existing old-store waiters are safe
for today's four callers because none can complete the re-owned task while
`rt_task_replace_owner` is still executing. Future call sites must re-derive
that shape or add a wake-old-and-new/marker protocol.

## Proof

`SP_MIGRATE_GAP` is test-only and release-zero like the earlier sync points
(`runtime/native/rt_sync_point.h:45-49`, `runtime/native/rt_sync_point.c:76-77`,
`check_sync_points.sh:40`).

`TestRuntimeV2LifecycleDebt020MigrateGapProof` builds the normal runtime with
`RT_TEST_SYNC_POINTS`, blocks after the positive route publication, registers a
real `join_key(adopter)` waiter in that gap through `prepare_park`, opens the
point, and proves the waiter is woken when the adopter completes. It sweeps
`SURGE_SHARDS=2,8`.

`TestRuntimeV2LifecycleDebt020MigrateGapNegativeControl` builds with
`RV2_DEBT_020_NEGATIVE_CONTROL`, which restores old order: drain old waiters,
block at `SP_MIGRATE_GAP`, then publish the route. The same gap waiter inserts
into the old store after the final drain and strands with
`debt020 migrate-gap joiner stranded`.

## Exit Criteria

- `RV2-DEBT-020` is closed by the generic fix and deterministic positive and
  negative coverage.
- The stranding waiter is the late `join_key(reowned_task)` waiter that resolves
  the old store after migration drained it in the negative-control build.
- `make runtime-v2-syncpoint-check`, `make c-check`, targeted Debt020 tests, and
  the touched C/H LOC gate are required final verification.
