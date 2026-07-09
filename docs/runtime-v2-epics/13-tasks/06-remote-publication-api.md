# Epic 13 Task 6: Remote Task Publication Runtime API

**Status:** complete in this worktree (2026-07-09).
**Kind:** native runtime API. Status-code based; no compiler lowering yet.
**Depends on:** Task 4 (spine), Task 5 (placement resolution).

## Goal

Native APIs that publish a task body on a destination shard and return the
`far Task<T>` handle per the Task 1 handle model: remote spawn request over
the transport spine, destination-side task creation with correct owner-shard
invariants, publication ack releasing the caller, and the generation/no-reuse
token wired from birth. Runnable and tested at the runtime level (C/test-hook
callers), before any crossing form lowers to it.

## Starting State (verify and re-pin)

- Transport spine: Task 4's queue/park/wake, message envelope with spawn
  request/ack categories declared.
- Task lifecycle: owner-shard task table (`rt_async_task.c`, segmented
  never-moved-slot table from Epic 8), completion `rt_task_complete.c`,
  lifetime/refcount `rt_task_lifetime.c`, join routing
  (`join_owner_shard_id`, Epic 9 `RV2-DEBT-020` protocol) — the publication
  path must respect all of them; list the exact invariants (owner writes
  under owner shard lane, etc.).
- Handle model + token: exactly the Task 1 decision. The token must make
  completion-vs-cancel double-completion impossible and stale handles
  detectable.
- Severability contract (Task 1): the published child is NOT enrolled in the
  caller's local scope accounting; the handle is the only lifecycle edge;
  publication wait is non-cancellable until ack.

## API Shape

- `rt_remote_spawn_publish(dst_shard_id, poll_fn_id, state, pending,
  out_handle) -> rt_remote_spawn_status` enqueues a remote spawn request over
  the data lane. The Task 6 test-hook payload is exactly `poll_fn_id + state`;
  no copied payload bytes/size/alignment contract is introduced here.
- `rt_far_task_handle` is the current public native handle shape:
  `task_id + generation + owner_shard_id`. It deliberately does not expose a
  raw `rt_task*` and does not pack the final public ABI.
- Destination drain creates the task on the destination owner shard, assigns
  the birth generation token, and sends a spawn ack with the far handle.
- Caller side stores a pending record and waits on `WAKER_REMOTE_SPAWN_REPLY`
  keyed by request id. The reply wait uses task suspension, never shard park,
  including same-shard/self-crossing runs.
- Failure statuses are distinct C status codes: invalid argument,
  destination shutdown, queue full, refused, and stale token. There is no
  silent local spawn fallback.

## Scope

In: `runtime/native/` publication/ack code over the spine, handle+token
plumbing, caller pending-wait integration with the task waiter machinery,
runtime-level tests (test hooks), trace counters
(`transport_spawn_requests`, `transport_spawn_acks`).

Out: compiler lowering (Tasks 7-8), await/cancel routing (Task 9 — but the
handle/token shape must already anticipate it), immediate `on` execute/reply
(Task 10), scope enrollment of any kind, payload representations beyond the
Task 1 decision (plain-data/copyable unless the safety proof was recorded).

## Steps

1. **Test-first** runtime rows (through test hooks, `SURGE_SHARDS=1,2,8`):
   - publish to a specific other shard: body runs there (owner id proof),
     ack returns a live handle;
   - publish to self (self-crossing): completes without deadlock — reply
     wait is a task suspend (Task 3 Step 6 seam reused);
   - publish during shutdown: deterministic failure status, no leak, no
     strand;
   - queue-full path: deterministic status (bounded queue), control lane
     unaffected;
   - token birth: handle carries the generation; a fabricated stale token is
     rejected (negative row).
2. Implement request/ack over the spine per the API shape.
3. Wire owner-shard invariants at the destination (create/publish on owner
   lane; join route initialized to the destination owner).
4. Wire the caller pending-wait as a reply waiter.
5. Counters + drain-handler trace evidence.

## Proof

- Focused Task 6 runtime-native proof passed:
  `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2RemotePublication' -count=1 -parallel=1 -p=1 -v --timeout 180s`.
- Native C strict-format/strict-warning proof passed: `make c-check`.
- Covered rows: static API shape/status enum/handle generation field, publish
  to another shard at `SURGE_SHARDS=2` and `SURGE_SHARDS=8`, destination owner
  proof, live ack handle validation, same-shard self-crossing without shard
  park, deterministic shutdown status, data-lane queue-full status with
  control lane unaffected, stale-token rejection, and spawn request/ack
  counters in the transport debug snapshot.
- Deliberately still out of scope: compiler lowering, executable crossing
  vertical, await/cancel routing, packed ABI representation, copied payload
  ownership, scope enrollment, and remote-free.

## Stop Conditions

- Correct publication requires scope completion messages to the caller's
  scope owner — stop: that contradicts the severability contract; return to
  Task 1's record for design review.
- The handle model cannot deliver stale-rejection without generation storage
  the Task 1 decision excluded — stop and amend the decision explicitly, do
  not improvise.
