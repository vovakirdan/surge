# Epic 13 Task 1: Kickoff Dependency Map And Contracts

**Status:** pending.
**Kind:** map/evidence + decisions. No production code changes.
**Depends on:** none.

## Goal

Produce the map and the recorded decisions every later slice follows: the
transport-relevant runtime/compiler state with `file:line` evidence, the
`far Task<T>` handle model with its mandatory generation/no-reuse token, the
payload representation answer, the executable placement set, the
detached-affine-far-Task severability contract as a runtime contract, the
task-suspend-vs-shard-park invariant, and the debt review
(`RV2-DEBT-024`/`025`/`026`, `005`, `011`/`018` promotion confirmation).

## Starting State (verify and re-pin every line)

Compiler:

- Crossing guards: `internal/buildpipeline/on_crossing_check.go` (FUT7014),
  `internal/buildpipeline/spawn_on_check.go` (FUT7015-7017), predicate
  `internal/buildpipeline/crossing_transport.go:7-16`
  (`backendHasCrossingTransport` returns `false` for every backend; guards are
  default-closed). Guards scan the root module AND dependency modules via
  `driver.DiagnoseResult.DependencyAnalyses()`
  (`internal/driver/diagnose_result.go`, commit `2fce7c22`).
- Readiness metadata: `internal/sema/crossing_lowering.go` —
  `CrossingLoweringInfo{Kind, Expr, Span, Body, Function, Destination,
  Captures, PayloadType, ResultType, HandleType, ReceiverExpr, ReceiverSymbol,
  ReceiverType, ConsumesHandle, RemoteOps}`; far-task spans in
  `sema.Result.FarTaskAwaitSpans/FarTaskCancelSpans`
  (`internal/sema/check.go:63-69`).
- HIR backstop: `internal/hir/lower_expr.go:120-126` — `ast.ExprOn` reaching
  HIR is a deterministic ICE. This backstop stays for impossible bypasses even
  after real lowering exists.
- Imported `far T` signatures decode correctly
  (`internal/sema/call_type_instantiation.go`, commit `c591788e`).
- Pipeline order caveat: `driver.DiagnoseWithOptions` lowers HIR before the
  buildpipeline guards run and discards the HIR error
  (`internal/driver/diagnose.go`, `//nolint:errcheck`); the guards then stop
  the compile. Task 7 must not silently rely on that ordering.

Runtime:

- Shard state: `runtime/native/rt_async_internal.h:149` (`struct rt_shard`) —
  per-shard ready queues, `wake_pending`, `net_poll_wake`
  (`rt_net_poll_wake`, `:127`), atomic `wake_token` (`:196`), fd registry
  (`rt_fd_registry.c/.h`), sorted sleep stores, shard lock lanes
  (`rt_lane.c`, control -> at most one shard lock, asserted at runtime).
- Park/wake: `rt_task_park.c`, ready queues `rt_ready_queue.c`, completion
  `rt_task_complete.c`, lifetime `rt_task_lifetime.c`, `done_cv` compat
  `rt_done_cv.c`.
- Deterministic proof mechanism: `runtime/native/rt_sync_point.h/.c`
  (`RT_SYNC_POINT_SP_*`), gated by `check_sync_points.sh`
  (`runtime-v2-syncpoint-check`). This is the pattern for Task 3.
- There is NO per-shard general inbound message queue, transport park state,
  transport wake counter, or remote publication API today. Confirm by
  inspection and record where each will live.

Prelude:

- `core/intrinsics.sg:193-206`: `Placement` opaque Copy type, `pool`,
  `distributed`, `shard(id: ShardId)` intrinsics. No runtime ABI behind them.

Test harness:

- Native execution rows live under `internal/vm/runtime_v2_*_test.go`;
  build/run artifacts are keyed by test name at
  `internal/vm/test_helpers_test.go:218` (`target/debug/.tests/<name>`) — the
  `RV2-DEBT-011` race root.

## Decisions To Record (each with rationale; later slices follow them)

1. **`far Task<T>` handle model.** Existing stable `rt_task*` + owner routing
   vs packed handle with generation. Must cover: stale handle prevention,
   cancellation routing, await consumption, teardown cleanup, and the
   mandatory generation/no-reuse token (completion and cancel must not
   double-complete a handle under any interleaving). Review `RV2-DEBT-025`
   here: affine single-consumer handles are what make the completion/cancel
   race tractable — either reaffirm affinity as a transport invariant and
   reassign the debt owner, or stop for design review.
2. **Severability contract (runtime restatement).** Cite
   `11-tasks/block-03-spawn-on-remote-spawn.md` L01-L04 and record: far child
   NOT enrolled in local scope `join_all`/failfast; publication wait
   non-cancellable until ack, cancel then routes to the returned handle;
   runtime teardown (failfast/unwind/shutdown with an unconsumed handle)
   routes a release/cancel — owned by this epic. List exactly which teardown
   entry points must call the release route.
3. **Payload representation.** Either restrict executable crossing captures
   and `ret` payloads to plain-data/copyable representations for this epic, or
   produce the written safety proof for heap-owned moves under the current
   allocator — including whether Epic 5 shard heap-accounting cells
   (`rt_heap_accounting.c/.h`) misattribute a cross-shard free. Name the sema
   surface that enforces the chosen restriction (capture verdicts already
   recorded in `CrossingLoweringInfo.Captures`).
4. **Executable placement set.** `shard(id)` range/clamp/diagnostic rules
   (what happens for `shard(999)` on 8 shards: clamp, error, or modulo — pick
   one, write it, test it) and the `distributed` selection policy
   (round-robin / hash / non-current-shard; must produce trace evidence that
   work can leave the caller shard). `pool` stays diagnostic-only.
5. **Task-suspend-vs-shard-park invariant.** Record where the caller's reply
   wait parks (task waiter keyed on the transport reply, never the shard park
   protocol), and that the `SURGE_SHARDS=1` self-crossing row is the forcing
   function.
6. **`RV2-DEBT-024` effect boundary.** Does transport lowering need exported
   hidden-crossing effect metadata (imported ordinary function whose body
   crosses), or do direct crossing-site records plus the dependency-module
   guard scan suffice? Record with a test reference either way.
7. **Baseline commands.** Pin the exact green baseline before any code:
   `make runtime-v2-check`, `make runtime-v2-crossing-check` (x2),
   `make check`, `sentrux check .` / `runtime` / `runtime/native` /
   `internal` quality numbers, `./check_file_sizes.sh -a`.

## Scope

In: reading, mapping, decisions, this document, epic-document updates if a
decision contradicts the draft, `DEBT.md` owner updates for 025/026.

Out: any production code, any test code, any harness change (Task 2).

## Deliverables

- This document updated to Complete with the seven decisions and their
  rationale.
- The dependency map: for each later task, the exact files it will touch and
  the invariants it must preserve, each with `file:line` evidence.
- `DEBT.md` rows for `RV2-DEBT-024`/`025`/`026` updated with the recorded
  outcome (owner reassignment or reaffirmation note).

## Stop Conditions

- A decision cannot be made without prototyping — write the bounded proving
  spike plan (hypothesis, files, proof command, success/failure criteria,
  rollback note) into this document first, then run it.
- Any decision requires distributed-scope messages, remote channels, or
  remote-free routing — stop; that is scope for a later epic and the epic
  boundary must be revisited instead of silently expanded.
