# Epic 12 Task 6: CI Gates And Quality Closeout

**Status:** complete.
**Kind:** CI wiring + documentation closeout. No compiler logic changes.
**Depends on:** all previous tasks.

## Goal

Promote the crossing-readiness proofs into stable CI gates, run the epic's
quality contract end to end, reconcile the documentation set, and write the
handoff so the Phase 4 transport epic starts from facts, not archaeology.

## Steps

### 1. Focused CI gate

Add a Makefile target following the existing `runtime-v2-*-check` pattern
(e.g. `runtime-v2-crossing-check`) that runs, deterministically and without
network:

- `go test ./internal/crossinggate/` (all four block gates + backend-stage
  rows for VM and LLVM);
- the Task 2 default-closed, ICE-on-bypass, and negative-space tests;
- the Task 3 loss-detection tests;
- the Task 4 integration record assertions.

Wire it into the same CI lane the other `runtime-v2-*-check` targets use.
Stability bar: the target passes `-count=` repetition per Task 5's evidence
before it is promoted; a flaky gate is worse than no gate.

### 2. Full quality contract

Per the epic's Test And Proof Contract:

- `make golden-check` (final fixture state), `make check`;
- `./check_file_sizes.sh -a`; for every touched over-limit legacy file,
  record shrank/flat/needs-owner;
- root Sentrux scan + scoped `internal/` scan, `check_rules` evidence per
  `SENTRUX_POLICY.md` (before/after comparison against the Task 1 baseline);
- `make c-check` / `make cppcheck` only if any runtime C changed (expected:
  none — state it explicitly).

### 3. Documentation reconciliation

- `DEBT.md`: final state for 001/002/011/018/024 per Tasks 3 and 5; grep
  proves no debt row still names "Epic 12" as a pending owner.
- `NOTES.md`: closeout entry — representation decision and rationale, frozen
  diagnostic wording, DEBT-024 evidence, gate name, and the handoff summary.
- `README.md` (epics index): Epic 12 marked complete with the gate listed.
- Epic document status line updated from "proposed" to closed state.
- Verify no public runnable crossing example appeared anywhere during the
  epic (re-run Task 4's non-advertisement check).

### 4. Phase 4 handoff

Answer, with pointers into Task 3's handoff map and the readiness record,
the six questions from the epic's Handoff section: per-form runtime message
needs; caller park/resume points; completion/cancel/cleanup routing;
OS-neutral vs backend-specific state; which compile-time metadata the
backend may rely on (the record's fields, by name); and the list of tests
that fail today only because transport is intentionally absent. This lands
as the final section of this document and is the required starting input for
the transport epic's own Task 1.

## Exit Criteria

- `runtime-v2-crossing-check` (or the chosen name) exists, is wired into CI,
  and is repetition-stable.
- Every acceptance criterion in the epic document is checked off with a
  pointer to its evidence (task doc + test name).
- `DEBT.md`, `NOTES.md`, epics `README.md`, and the epic status line are
  consistent with the final state.
- The Phase 4 handoff section is complete.

## Results (2026-07-08)

Task 6 is complete. Epic 12 now has a focused CI gate,
`runtime-v2-crossing-check`, wired into the existing `runtime-v2-check` target.
The GitHub Actions runtime-v2 job already runs `make runtime-v2-check`, so no
workflow file change was needed.

### Gate

`runtime-v2-crossing-check` runs:

```bash
go test ./internal/crossinggate -count=1 --timeout 60s
go test ./internal/buildpipeline ./internal/hir -run '^(TestCrossingBackendUnavailableMessages|TestCrossingBackendGuardsAreDefaultClosed|TestCrossingBackendGuardDoesNotMaskSemaErrors|TestLowerOnCrossingBypassReturnsError|TestLowerSpawnOnCrossingBypassReturnsError)$' -count=1 --timeout 60s
go test ./internal/sema ./internal/driver -run '^(TestCrossingLowering.*|TestFunctionCrossingEffectInference|TestCrossingReadinessDebt024ModuleImportDoesNotRequireImportedEffects)$' -count=1 --timeout 60s
```

This covers:

- all crossing block gates and Task 4 integration fixtures;
- backend-unavailable wording, default-closed guards, and sema-error
  non-masking;
- HIR bypass deterministic internal errors for `on` and `spawn on`;
- sema lowering-readiness records and direct-call crossing inference;
- the `RV2-DEBT-024` cross-module deferral proof.

Evidence:

```bash
make runtime-v2-crossing-check
make runtime-v2-crossing-check
```

Both consecutive runs passed.

### Quality Contract

Commands run:

```bash
make golden-check
make check
./check_file_sizes.sh -a
sentrux check .
sentrux check internal
```

Results:

- `make golden-check`: passed; the Task 4 `_`-prefixed integration fixtures
  remain outside shell golden sidecars.
- `make check`: passed; includes Go tests, lint, `make c-check`, and the
  default changed-file LOC gate.
- `./check_file_sizes.sh -a`: passed; 745 files checked, 0 over hard limit,
  existing legacy ceilings unchanged.
- `sentrux check .`: passed, quality `6189`, all rules pass.
- `sentrux check internal`: passed, quality `6532`, all rules pass.
- `internal/.sentrux/rules.toml` was added during closeout so compiler-scoped
  checks no longer report a missing rules file. It intentionally starts with
  baseline-preserving constraints only.
- No runtime C files changed. `make c-check` ran through `make check`;
  `make cppcheck` was not required for this compile-time epic.

Non-advertisement check: re-scanned docs/testdata for crossing constructs
outside the language/runtime design docs, Runtime V2 epic docs, and golden
crossing fixtures. No public runnable crossing example was added; the only
remaining non-design hit was the internal `core_stdlib` golden fixture for
intrinsics.

### Debt And Documentation

- `RV2-DEBT-001` and `RV2-DEBT-002` remain under **Backend/Test Matrix
  Cleanup**.
- `RV2-DEBT-011` and `RV2-DEBT-018` remain under **Backend/Test Matrix
  Cleanup** because Epic 12 crossing rows use `buildpipeline`/`crossinggate`,
  not VM artifact helpers.
- `RV2-DEBT-017` no longer carries an "Epic 12 if..." owner; it belongs to
  sync-channel compat-lane retirement or Backend/Test Matrix Cleanup.
- `RV2-DEBT-024` remains planned for Phase 4 transport lowering or a later
  effect-system epic. Task 3 proved imported function effect bits are not
  required for the current guard-before-HIR readiness representation.

Updated documents:

- `12-crossing-surface-integration-and-lowering-readiness.md`
- `12-tasks/README.md`
- `DEBT.md`
- `NOTES.md`
- `README.md`
- `SENTRUX_POLICY.md`

## Phase 4 Handoff

Epic 12 chose and completed **guard-before-HIR**. There are still no HIR/MIR
crossing nodes and no runtime transport. Phase 4 should start from the sema
readiness record in `internal/sema/crossing_lowering.go`.

### Runtime Messages By Form

- `on placement { ret value; }`: send a request to the placement's owner shard
  with captured payloads and the block body entry; return a `TaskResult<T>`
  reply to the caller.
- `on far_handle { ... }`: route an owner-anchored operation to the handle's
  owner shard with the remote operation name and captured payloads; reply with
  `TaskResult<T>`.
- `spawn on placement { ret value; }`: create a child task on the destination
  shard and return a `far Task<T>` handle after remote task publication.
- `far Task<T>.await()`: consume the remote task handle, register or route a
  wait at the task owner, and resume the caller with `TaskResult<T>`.
- `far Task<T>.cancel()`: consume the remote task handle, route cancellation to
  the task owner, and resume the caller with `TaskResult<nothing>`.

### Park And Resume Points

Immediate `on`, `far Task.await`, and `far Task.cancel` park the current caller
until a reply arrives. `spawn on` only waits for remote child publication; the
child result is observed later through the returned `far Task<T>` handle.

### Completion, Cancellation, And Cleanup

Completion replies route back to the caller shard recorded by the transport
request. Task cancellation routes to the task owner. Dropping or consuming a
remote handle needs an owner-routed cleanup/free path; Epic 12 did not add this
runtime protocol.

### OS-Neutral Boundary

The compiler metadata is OS-neutral. Eventfd, pipe, epoll, kqueue, IOCP,
wake-credit accounting, and fd readiness details belong behind runtime/backend
transport interfaces in Phase 4.

### Compile-Time Metadata Available

Future lowering may rely on these `CrossingLoweringInfo` fields by name:
`Kind`, `Expr`, `Span`, `Body`, `Function`, `Destination`, `Captures`,
`PayloadType`, `ResultType`, `HandleType`, `ReceiverExpr`, `ReceiverSymbol`,
`ReceiverType`, `ConsumesHandle`, and `RemoteOps`.

### Tests That Are Red Only Because Transport Is Absent

No CI test is red at Epic 12 closeout. The backend-stage rows intentionally
assert `FUT7014`-`FUT7017` for valid crossing source forms because transport is
absent. The same rows are the first tests Phase 4 should invert or split once a
backend can lower real transport.
