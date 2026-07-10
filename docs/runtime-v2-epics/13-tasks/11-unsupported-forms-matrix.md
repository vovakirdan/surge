# Epic 13 Task 11: Unsupported Forms And Matrix Hardening

**Status:** complete (2026-07-10). Evidence below (owned matrix table, audit)
and `NOTES.md` "Epic 13 Task 11 Complete".
**Kind:** backend matrix tests + guard audit. No new capability flips.
**Depends on:** Tasks 8, 9, 10 (final capability state).

## Goal

Prove the negative space of the new capability matrix: everything that did
NOT become executable in this epic still fails deterministically in the
intended layer, on every backend, with no hidden local lowering — now that
the guards are per-form instead of blanket.

## The Matrix To Prove

Rows (each × `BackendVM`, `BackendLLVM`, and an unknown/future backend
value):

| Form | Expected outcome after Epic 13 |
| --- | --- |
| `spawn on shard(id)` / `spawn on distributed` | executes on LLVM; FUT7015 on VM/unknown |
| `far Task.await()` / `.cancel()` | executes on LLVM; FUT7016/7017 on VM/unknown |
| `on shard(id)` / `on distributed` | executes on LLVM; FUT7014 on VM/unknown |
| `spawn on pool` / `on pool` | deterministic placement-unavailable (or FUT) diagnostic on ALL backends, incl. LLVM |
| `on far_handle { ... }` | FUT7014 on ALL backends |
| remote `far Channel<T>` ops | Epic 11 sema rejection unchanged (`SemaFarLocalOp` family) |
| `far TcpConn` remote I/O | `SemaOnTcpRemoteIO` unchanged |
| crossing constructs in IMPORTED modules | same per-form outcomes as root-module (dependency scan intact, `2fce7c22`) |
| guard-bypass into HIR on non-capable backend | deterministic ICE (backstop intact) |
| compile-only paths (LSP/check/format/fix) | zero backend-unavailable diagnostics on valid code |

## Owned Matrix (Step 1 deliverable, final state)

Every cell names the test that fails if the behavior regresses. "e2e" rows
run `SURGE_SHARDS=1,2,8`; compile rows run per backend.

| Form / row | LLVM | VM | unknown backend | Owning tests |
| --- | --- | --- | --- | --- |
| `spawn on shard/distributed` (async, copyable) | executes | FUT7015 | FUT7015 | `TestRuntimeV2FarTaskSource{OverrideAcrossShards,ProductionCapability}` (e2e); `TestLLVMTransportCapabilityOpensAsyncSpawnOn`; `TestVMAndUnknownBackendsKeepExecutableAsyncFormsGuarded` |
| `far Task.await()/.cancel()` (async) | executes | FUT7016/7017 | FUT7016/7017 | same e2e; `TestLLVMTransportCapabilityOpensAsyncFarTaskLifecycle`; `TestVMAndUnknownBackendsKeepExecutableAsyncFormsGuarded` |
| `on shard/distributed` (async, copyable) | executes | FUT7014 | FUT7014 | `TestRuntimeV2ImmediateOnSource{OverrideAcrossShards,ProductionCapability}` (e2e); `TestLLVMTransportCapabilityOpensAsyncImmediateOn`; `TestVMAndUnknownBackendsKeepExecutableAsyncFormsGuarded` |
| synchronous forms (all of the above) | FUT7014-7017 | FUT7014-7017 | FUT7014-7017 | `TestCrossingBackendGuardsAreDefaultClosed`; `TestCrossingBackendUnavailableMessages` (suspend-capability stage of the two-stage guard) |
| non-copyable payload / owned captures | FUT7015/7016 | n/a (guarded earlier) | n/a | `TestLLVMTransportPayloadGuard`, `TestLLVMTransportPayloadGuardCoversImportedModules` |
| `spawn on pool` (async) | compiles; deterministic runtime unsupported-placement panic | FUT7015 | FUT7015 | `TestRuntimeV2SpawnOnPoolProductionCapabilityFailsDeterministically`; guards via default-closed rows |
| `on pool` (async) | compiles; deterministic runtime unsupported-placement panic | FUT7014 | FUT7014 | `TestRuntimeV2ImmediateOnPoolProductionCapabilityFailsDeterministically` |
| out-of-range `shard(id)` | `spawn on`: deterministic invalid-placement panic; `on`: Cancelled resume, body never runs | n/a | n/a | far-task e2e error switch; `immediate-on-invalid-shard-cancelled-resume` behavior row + e2e `shard(4096)` row |
| `on far_handle { ... }` | FUT7014 | FUT7014 | FUT7014 | `TestCrossingBackendGuardsAreDefaultClosed/on_far_handle`; `TestCrossingBackendUnavailableMessages` |
| remote `far Channel<T>` ops | sema rejection (`SemaFarLocalOp` family) unchanged | same | same | `internal/sema` SEM3142 rows (`spawn_on_crossing_test.go` far-channel/far-tcp routing, `on_crossing_test.go` far-op-outside-on) + golden `on_negative_far_operation_outside_on` |
| `far TcpConn` remote I/O | `SemaOnTcpRemoteIO` unchanged | same | same | `internal/sema/crossing_lowering_test.go` (SemaOnTcpRemoteIO row) |
| crossings in imported modules (guarded shapes) | FUT7014-7017 | FUT7014-7017 | FUT7014-7017 | `TestCrossingBackendGuardsCoverImportedModules` |
| crossings in imported modules (executable shapes) | executes end to end | n/a | n/a | `TestRuntimeV2ImportedCrossingProductionCapability` (e2e, shards 1/2/8) |
| self-crossing at `SURGE_SHARDS=1` uses transport | counters fire (1 execute request + 1 reply on the caller shard) | n/a | n/a | `immediate-on-self-crossing-uses-transport-at-one-shard` behavior row; e2e `shards_1` sub-rows |
| guard bypass into HIR | deterministic ICE | same | same | `TestLower{On,SpawnOn,FarTask}CrossingBypassReturnsError` |
| compile-only paths (LSP/check/format/fix) | zero backend-unavailable diagnostics | same | same | `TestBackendUnavailableNegativeSpace`; `TestCompileOnlyCrossingDoesNotReportBackendUnavailable` |

## Hidden-Fallback Audit (Step 3 deliverable)

Reviewed every executable-path status branch in the Task 8-10 codegen and
runtime; no path lowers a crossing to local `spawn`, local `.await()`, or
local channel operations:

- `internal/backend/llvm/emit_crossing.go:206-232` (`spawn on` status
  switch): PENDING suspends, OK stores the handle, every other status
  branches to `emitPanicBlock` — six named panics plus a default; no local
  spawn emission exists in the error space.
- `internal/backend/llvm/emit_crossing_far_task.go:176-203` (await/cancel
  error blocks): same shape — panic-only error space; the result is
  materialized exclusively from the reply kind/bits
  (`emitFarTaskLifecycleResult`).
- `internal/backend/llvm/emit_crossing_immediate_on.go:129-160` (`on` error
  blocks): panic-only error space; UNSUPPORTED_PLACEMENT (pool) panics
  deterministically.
- `runtime/native/rt_immediate_on.c:104-119`: the only non-error non-execute
  outcome is the documented out-of-range placement path — it resumes
  `Cancelled` WITHOUT creating a task (no body, no local run), and the
  resolver counts it (`invalid_shard_resolutions`).
- `runtime/native/rt_remote_spawn.c` / `rt_remote_task_api.c` /
  `rt_immediate_on.c` enqueue failures: every failure unwinds the pending
  (clear reply wait, consume, lease restore where applicable) and returns a
  status the codegen maps to a panic — nothing retries through a local
  executor path.
- Grep evidence: `grep -n "__task_create\|rt_task_await" internal/backend/llvm/emit_crossing*.go`
  returns no hits — the crossing emitters cannot reach the local task ABI.

## Bypass Backstop (Step 4 deliverable)

`internal/hir/lower_expr_crossing.go:40-44` still stops any crossing that
reaches HIR without the per-form capability with a deterministic internal
error; `TestLower{On,SpawnOn,FarTask}CrossingBypassReturnsError`
(`internal/hir/lower_test.go`) drive that path directly with no enabled
forms, so they remain non-vacuous after the Task 9/10 capability flips (the
flips widen the buildpipeline form map, not the HIR lowerer's own gate).

## Starting State (verify and re-pin)

- Final capability predicate state after Task 10; the crossing-guard tests
  updated by Tasks 9-10.
- Existing matrix machinery: `internal/crossinggate` fixtures
  (`// EXPECT-STAGE: backend`), `internal/buildpipeline/crossing_backend_test.go`
  (incl. `TestCrossingBackendGuardsAreDefaultClosed`,
  `TestCrossingBackendGuardsCoverImportedModules`),
  `internal/crossinggate/negative_space_test.go`.
- `SURGE_SHARDS=1` note: supported forms EXECUTE through the transport path
  at one shard (no local fallback shortcut) — add an explicit row asserting
  transport counters fire even at shards=1.

## Scope

In: test additions/updates across `crossinggate`/`buildpipeline`/`hir`
matrices, a written audit that no code path maps an unsupported form to a
local operation (grep + review evidence over the Task 8-10 codegen), fixture
churn.

Out: changing any capability, fixing anything the matrix finds (a finding is
a stop condition routed to the owning task), remote channel/select work.

## Steps

1. Enumerate the final (backend × form) matrix in a table IN THIS DOCUMENT
   with the owning test for every cell — the Epic 12 "per row a test that
   fails if the behavior regresses" rule, extended to the split-capability
   world. Cells without an owning test get one.
2. Add the missing rows (expected gaps from Tasks 8-10: unknown-backend rows
   for newly executable forms; `pool` on LLVM now that LLVM executes other
   placements; imported-module rows for each newly executable form —
   verify the dependency-scan behavior when the root compiles on LLVM but
   the crossing sits in a dependency).
3. Hidden-fallback audit: review Task 8-10 codegen for any path that could
   fall back to local `spawn` / local `.await()` / local channel ops on
   status failure; each failure status must map to a deterministic runtime
   error. Record the audit with `file:line` references.
4. Bypass backstop: re-verify the HIR/MIR deterministic errors for
   non-capable backends survived the representation split (Task 7's tests
   still meaningful, not vacuous).
5. Run the full matrix twice consecutively on the Task 2-hardened harness.

## Proof

- Matrix table complete, every cell test-owned, full run green twice.
- `make runtime-v2-crossing-check` twice; `make golden-check`;
  `sentrux check internal`; `make check`.

## Stop Conditions

- Any cell shows a hidden local execution or a nondeterministic outcome —
  stop the epic's closeout; route to the owning task (8/9/10) as a blocker.
- A cell's "expected outcome" is genuinely ambiguous — the epic's Lowering
  Contract has a hole; record and resolve at the epic level, not in a test.
