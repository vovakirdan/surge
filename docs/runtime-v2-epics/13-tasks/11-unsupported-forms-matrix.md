# Epic 13 Task 11: Unsupported Forms And Matrix Hardening

**Status:** pending.
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
