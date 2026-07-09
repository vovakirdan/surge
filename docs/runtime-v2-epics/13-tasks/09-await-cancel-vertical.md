# Epic 13 Task 9: `far Task.await` / `far Task.cancel` Executable Vertical

**Status:** pending.
**Kind:** LLVM lowering + runtime e2e + the joint public gate for
`spawn on` + await/cancel.
**Depends on:** Task 8.

## Goal

Route `far Task<T>.await()` and `far Task<T>.cancel()` through owner-shard
messages with the generation token making double-completion impossible,
deliver the teardown release/cancel route the severability contract assigns
to this epic, and — once both are proven — flip the LLVM capability for
`spawn on` + await + cancel together, updating the crossing-guard matrix
deliberately.

## Starting State (verify and re-pin)

- Task 8's vertical under the override; handle + token from Task 6.
- Affine consumption is compile-time enforced (Epic 11 L02/L03: double
  await/cancel-then-await are `SemaUseAfterMove`); the runtime token is the
  DEFENSE-IN-DEPTH for what sema cannot see (teardown, races), not a
  replacement for it.
- Owner-routed wait precedent: join-route protocol
  (`join_owner_shard_id`, Epic 9 `RV2-DEBT-020`) — await routing must not
  regress it.
- Completion path: `rt_task_complete.c`; result transfer must carry
  `TaskResult<T>` payload back per the payload decision.

## Message Semantics (from the epic's transport table)

- await: consume handle -> owner-routed wait/consume request; owner replies
  with completion (`TaskResult<T>`) when the task finishes (or immediately
  if DONE); caller suspends as a task, resumes on the reply.
- cancel: consume handle -> owner-routed cancel request; owner cancels (or
  observes DONE first), replies ack with `TaskResult<nothing>`; exactly one
  of {completion-to-awaiter, cancel-ack} consumes the task's reply edge —
  the token decides races.
- teardown release: when a caller holding an unconsumed handle is torn down
  (failfast/unwind/shutdown), the runtime routes release/cancel to the owner
  so the remote task is not orphaned. Identify every teardown entry point
  (from the Task 1 list) and wire the route.

## Scope

In: LLVM codegen for await/cancel (handle consumption -> request -> suspend
-> `TaskResult` materialization), owner-side handlers over the spine,
race/shutdown/teardown tests, the joint capability flip + crossing-guard
matrix update, stale-token negative rows.

Out: immediate `on` (Task 10), remote channels/select, distributed scope
accounting, VM.

## Steps

1. **Test-first** rows (`SURGE_SHARDS=1,2,8`):
   - spawn-then-await returns the body's value as `TaskResult<T>` from a
     non-caller shard; spawn-then-cancel returns `TaskResult<nothing>` and
     the body is cancelled or completed-first (both outcomes legal, both
     deterministic in what they return);
   - await an already-DONE remote task (immediate reply);
   - cancel-vs-completion race: sync-point interleavings prove no
     double-complete, no strand, exactly-one reply-edge consumption (token
     proof — this is the epic's acceptance row);
   - stale token negative row: fabricated stale handle is rejected;
   - teardown: caller failfast/unwind/shutdown with unconsumed handle ->
     release routed, remote task not orphaned (observable via owner-side
     counter/trace), no leak (each message one owning free path);
   - shutdown wakes await-parked callers on every shard;
   - self-crossing await (shards=1) completes without deadlock.
2. Implement owner-side handlers + codegen.
3. Wire teardown release at the Task 1-listed entry points.
4. **Joint gate flip:** enable `backendSupportsCrossingForm(LLVM, spawn_on |
   far_task_await | far_task_cancel)` in production; update the
   crossing-guard test matrix: these (backend, form) pairs now execute, all
   other pairs (VM, unknown backends, `pool`, `on far_handle`) still assert
   FUT7014-7017/placement diagnostics. `make runtime-v2-crossing-check`
   expected rows change HERE, deliberately.

## Proof

- All Step 1 rows green; the race row runs under the sync-point mechanism,
  not timing.
- Post-flip: full `make runtime-v2-crossing-check` twice with the updated
  matrix; compile-only negative space still clean.
- `make c-check`, `make cppcheck`, `make golden-check`,
  `./check_file_sizes.sh -a`, Sentrux scoped scans, `make check`.

## Stop Conditions

- The cancel/completion race cannot be closed without scope-edge generation
  semantics — stop: that is the distributed-scope epic's machinery; the
  per-far-Task token must suffice, else design review.
- Teardown release requires walking another shard's state uninvited — stop;
  routes go through the owner via messages, never direct cross-shard state
  access.
