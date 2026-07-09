# Epic 13 Task 6: Remote Task Publication Runtime API

**Status:** pending.
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

## API Shape (suggested; final names in-task)

- `rt_remote_spawn_publish(dst_shard, body_fn, payload, payload_meta,
  out_pending) -> status` — enqueue spawn request (data-lane category).
- Destination drain handler: create the task on the destination shard as
  owner, per owner-shard invariants; enqueue spawn ack (control lane) with
  the handle + generation token.
- Caller side: `out_pending` is a task-suspend wait (reply waiter keyed on
  the request id) — NEVER a shard park (reply-wait invariant); ack resolves
  it to the `far Task` handle value.
- Failure statuses: destination shutdown, queue full (bounded!), publication
  refused — each a distinct status code the future lowering maps to runtime
  errors deterministically; no silent local spawn fallback status exists.

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

- All Step 1 rows green; the owner-differs row proves work left the caller
  shard at `SURGE_SHARDS>1`.
- No regression: `make runtime-v2-check` twice, `make
  runtime-v2-crossing-check` (guards still closed — nothing lowers yet).
- `make c-check`, `make cppcheck`, `./check_file_sizes.sh -a`,
  `sentrux check runtime/native`, `make check`.

## Stop Conditions

- Correct publication requires scope completion messages to the caller's
  scope owner — stop: that contradicts the severability contract; return to
  Task 1's record for design review.
- The handle model cannot deliver stale-rejection without generation storage
  the Task 1 decision excluded — stop and amend the decision explicitly, do
  not improvise.
