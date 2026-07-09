# Epic 13 Task 1: Kickoff Dependency Map And Contracts

**Status:** complete as of 2026-07-09.
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

## Recorded Decisions

1. **`far Task<T>` handle model.** Epic 13 keeps the existing stable task
   identity as the first runtime anchor (`rt_task*` / task table slot), but a
   `far Task<T>` handle is not raw identity alone. The executable transport
   handle carries the task owner shard plus a mandatory generation/no-reuse
   token. Later tasks may decide whether that is a small packed handle or a
   backend-only side record, but the semantic contract is fixed: every await,
   cancel, completion reply, release, and stale reply validates the token
   before consuming or completing the handle.

   Rationale: current tasks have stable ids and owner routes
   (`runtime/native/rt_async_internal.h:181-228`,
   `runtime/native/rt_waiter_join_route.c:1-141`), and fd registry already
   proves the generation-token shape for stale snapshots. Raw pointer-only far
   handles are rejected because `rt_task_lifetime.c` frees the local handle
   after the last reference, and a cross-shard completion/cancel race needs a
   non-ambiguous stale-drop path. `RV2-DEBT-025` is therefore reaffirmed:
   affine single-consumer far handles are a transport invariant, not a missing
   convenience feature for this epic.

2. **Severability contract.** The runtime model follows the Epic 11 lifecycle
   matrix: `L01` dropped far task is invalid, `L02` double await is invalid,
   `L03` await-after-cancel is invalid, and `L04` returning the handle
   transfers ownership
   (`docs/runtime-v2-epics/11-tasks/block-03-spawn-on-remote-spawn.md:200-203`).
   A placed child created by `spawn on` is not enrolled in the enclosing local
   scope's `join_all`/failfast accounting. The affine `far Task<T>` handle is
   the only lifecycle edge until distributed scopes exist.

   Publication wait is not cancellable before ack: caller cancellation is
   observed after the ack resolves, then routes as a cancel/release on the
   returned handle, so "was the remote task created" never becomes ambiguous.
   The cleanup entry points owned by this epic are the new remote-publication
   wait cleanup, far-await/far-cancel waiter cleanup, far-handle local cleanup
   emitted by lowering for failfast/unwind exits, and executor shutdown drain
   for outstanding transport handles. Existing local `scope_enter` /
   `scope_register_child` entry points are intentionally not used for placed
   children.

3. **Payload representation.** Executable crossing captures and `ret`
   payloads are restricted to plain-data/copyable representations in Epic 13.
   Heap-owned moves, owned arrays, and any payload that would require
   cross-shard free/remote-free are rejected for executable lowering until the
   allocator-owner/remote-free epic supplies a proof. The current allocator
   accounts frees to the current thread's heap cell
   (`runtime/native/rt_alloc.c:28-34`, `runtime/native/rt_alloc.c:57-64`), so
   silently moving heap-owned payloads would at minimum misattribute accounting.

   Enforcement surface: sema already records accepted capture verdicts in
   `CrossingLoweringInfo.Captures`
   (`internal/sema/crossing_lowering.go:77-86`,
   `internal/sema/crossing_lowering.go:98-120`). Compiler lowering tasks must
   reject executable rows whose captures or result payload need
   `CrossingCaptureOwnedShardMovable` / heap ownership. This is a backend
   executability restriction, not a syntax rollback.

4. **Executable placement set.** `shard(id)` and `distributed` are the only
   executable placements in Epic 13. `pool` remains deterministic
   placement-unavailable/backend-unavailable until the Tier 2 CPU pool epic.

   `shard(id)` resolves at runtime against `rt_runtime.shard_count`
   (`runtime/native/rt_async_internal.h:176-179`). An out-of-range id is never
   clamped or modulo-mapped. It completes the publication/immediate crossing
   through a deterministic non-executing placement error path: immediate `on`
   resumes as `Cancelled`, and `spawn on` returns a far-task handle already
   completed as cancelled on the caller shard. The body does not run, no shard
   0 fallback is allowed, and a trace counter must make the invalid placement
   visible. Constant out-of-range placements may gain a compile diagnostic
   later, but runtime values must stay deterministic.

   `distributed` uses a runtime round-robin policy over configured shards and,
   when `shard_count > 1`, prefers a shard different from the caller for the
   first attempt. The proof row must show at least one non-caller owner shard
   under `SURGE_SHARDS=2,8`. This is a locality/distribution policy, not a
   load-balancing promise.

5. **Task-suspend-vs-shard-park invariant.** A caller waiting for a transport
   reply suspends its task on a transport waiter. It never parks the shard.
   Shard park remains only the worker-idle path in `rt_worker_main`, after the
   shard has checked ready queues, sleep, net work, and the
   parked-with-work assertion (`runtime/native/rt_worker_turn.c:122-164`).
   The `SURGE_SHARDS=1` self-crossing row is mandatory because a shard-level
   wait would deadlock the only worker able to drain the inbound queue and send
   the reply.

6. **`RV2-DEBT-024` effect boundary.** Direct crossing-site records plus the
   dependency-module guard scan are sufficient for Epic 13. Lowering consumes
   accepted crossing forms from sema readiness metadata
   (`internal/sema/crossing_lowering.go:98-127`) and far-task spans
   (`internal/sema/check.go:63-71`); backend guards already scan dependency
   modules via `DependencyAnalyses`
   (`internal/driver/diagnose_result.go:113-151`,
   `internal/buildpipeline/on_crossing_check.go:23-26`,
   `internal/buildpipeline/spawn_on_check.go:28-53`). Exported hidden-crossing
   effects for ordinary imported functions and higher-order/function-type
   boundaries remain deferred to a later effect-system epic. Task 7/11 must
   keep the imported-far-operation rows and dependency-module guard rows green.

7. **Harness debt disposition.** `RV2-DEBT-011` and `RV2-DEBT-018` are promoted
   narrowly because Epic 13 will add native `SURGE_SHARDS` execution rows
   through the LLVM VM harness. The artifact root is currently keyed by
   sanitized test name (`internal/vm/test_helpers_test.go:215-231`), source
   and binary basenames reuse the same name
   (`internal/vm/test_helpers_test.go:250-324`,
   `internal/vm/mt_executor_test.go:18-39`), and timeout execution currently
   does not carry artifact path/binary stat in `runResult`
   (`internal/vm/mt_executor_test.go:233-264`). Task 2 fixes this transport
   path before native transport tests land; the broad VM/backend matrix cleanup
   remains a later epic.

## Dependency Map For Later Tasks

- **Task 2, harness hardening.** Touch `internal/vm/test_helpers_test.go` and
  `internal/vm/mt_executor_test.go`. Preserve existing build/run artifact
  capture (`build.stdout`, `build.stderr`, `run.stdout`, `run.stderr`,
  `exit_code`) while making artifact dirs and LLVM output paths per-run
  unique. Empty-output failures must report command, artifact dir, binary path,
  and stat information.
- **Task 3, park/wake proof.** Add sync-point windows only through the
  allowlisted pattern in `runtime/native/rt_sync_point.h:26-56` and
  `runtime/native/rt_sync_point.c:188-224`, then update
  `check_sync_points.sh`. The proof must target the worker idle path in
  `runtime/native/rt_worker_turn.c:122-164` and the future transport inbound
  queue, not net poll wake.
- **Task 4, inbound transport spine.** Add transport state to `struct rt_shard`
  near the shard-owned scheduler/waiter/net fields
  (`runtime/native/rt_async_internal.h:149-174`) and, if global counters are
  needed, to `struct rt_executor`
  (`runtime/native/rt_async_internal.h:307-359`). Do not reuse
  `rt_net_poll_wake` as the public transport abstraction; it is currently a
  per-shard pipe-backed net-poller wake (`runtime/native/rt_net_poller.c:24-92`).
  Worker drains must keep local ready publication semantics from
  `runtime/native/rt_ready_queue.c:138-192`.
- **Task 5, placement ABI.** `core/intrinsics.sg:192-206` is the public
  prelude surface. Add a runtime representation that distinguishes
  `distributed`, `shard(id)`, and unsupported `pool` without exposing
  OS-specific wake details.
- **Task 6, remote publication API.** Extend native task publication around the
  current local create path (`runtime/native/rt_async_task.c:10-100`) without
  copying its local-scope child append into remote placed children. The
  existing comment at `rt_async_task.c:49-93` explains why local children are
  same-owner; remote placed children intentionally do not satisfy that
  invariant.
- **Task 7, lowering representation.** Split backend support at
  `internal/buildpipeline/crossing_transport.go:7-16`, replace the supported
  LLVM/native path's guard-before-HIR flow with real HIR/MIR representation,
  and keep the impossible bypass ICE at `internal/hir/lower_expr.go:120-126`.
  Be explicit about the current pipeline caveat:
  `buildpipeline.Compile` requests HIR at
  `internal/buildpipeline/compile.go:64-75`, while
  `driver.DiagnoseWithOptions` discards HIR lowering errors at
  `internal/driver/diagnose.go:327-334`.
- **Tasks 8-10, executable verticals.** Lower only supported forms from the
  sema readiness records. Do not lower to local spawn/local await. All waits
  must suspend tasks and let the shard worker continue draining transport.
- **Task 11, unsupported matrix.** Preserve deterministic guards for `pool`,
  `on far_handle`, remote channels/select, VM, and unknown backends in
  `internal/buildpipeline/on_crossing_check.go:10-35` and
  `internal/buildpipeline/spawn_on_check.go:10-70` until each form has its own
  approved executable contract.
- **Task 12, closeout.** Wire a focused transport gate into `Makefile`, keep
  `runtime-v2-crossing-check` green, and update this epic, `DEBT.md`,
  `NOTES.md`, and the Runtime V2 index with the exact executable/future split.

## Baseline Before Code

- `make runtime-v2-crossing-check && make runtime-v2-crossing-check` passed on
  2026-07-09.
- `make runtime-v2-check` passed on 2026-07-09. Perf row:
  `control_lock_acquired=11744 (11.469/req)`,
  `ctrl_await_compat=3458 (3.377/req)`,
  `steady-state-control=8286 (8.092/req, ceiling 20.0)`,
  `lifecycle-control=6131 (5.987/req, ceiling 9.0)`,
  `placement_adoptions=241`, `accept_owner_active_shards=8`.
- `make check` passed in the documentation commit hook for
  `094f4a39 docs(runtime): plan Epic 13 transport vertical`.
- `./check_file_sizes.sh -a` passed: 745 files checked, 712 under the good
  threshold, 28 acceptable, 5 legacy ceilings, 0 over limit, overall
  `ОТЛИЧНО`.
- `sentrux check .` passed, quality `6189`.
- `sentrux check runtime` passed, quality `5345`.
- `sentrux check runtime/native` passed, quality `5460`.
- `sentrux check internal` passed, quality `6532`.

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
