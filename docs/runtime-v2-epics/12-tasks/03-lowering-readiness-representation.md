# Epic 12 Task 3: Lowering Readiness Representation

**Status:** complete.
**Kind:** compiler metadata + tests. Under representation option (b) this
task is preceded by a separate HIR/MIR node-introduction task (written only
if Task 1 chose (b)).
**Depends on:** Task 1 (representation decision, sema metadata inventory),
Task 2 (guard contract, so this task builds on stable diagnostics).

## Goal

Deliver the epic's lowering-contract table: for each crossing form, the
listed meaning survives to the chosen layer, proven by tests that fail if the
information is lost. This task also owns the `RV2-DEBT-024` decision point.

## Starting State

- Sema computes but the pipeline does not durably expose: `on` destination
  expression/type (`internal/sema/on_crossing.go`), `spawn on` destination
  and `far Task<T>` result typing (`internal/sema/spawn_on_crossing.go`),
  capture legality judgments (SEM3165-3168 checks), await/cancel spans
  (`FarTaskAwaitSpans` / `FarTaskCancelSpans`), and per-function `MayCross`
  (`internal/sema/crossing_effect.go:41-66`).
- Task 1's deliverable 3 lists, per contract row, where each item currently
  lives and where it is dropped. That table is this task's work list.
- HIR/MIR have no crossing representation; under option (a) they stay that
  way (plus the Task 2 ICE check).

## Scope

In: a lowering-readiness record (option a) or consuming the new IR nodes
(option b); tests per contract row; the DEBT-024 decision; documentation of
the sema-metadata → future-runtime-message map for the Phase 4 handoff.

Out: transport, queues, wake protocols, remote lifecycle; changing what sema
accepts; new syntax; public examples.

## Steps (written for option (a); adapt mechanically if (b) was chosen)

### 1. Define the readiness record

Introduce one explicit structure (e.g. `sema.CrossingLoweringInfo` or a
sibling package type) populated during type checking, one entry per crossing
site, carrying per the contract table:

- form kind (`on`-placement, `on`-far-handle, `spawn on`, `far-await`,
  `far-cancel`);
- destination: expression ID + resolved type, and for far-handle
  destinations the owner-anchoring judgment;
- capture list: symbol, capture mode, movability verdict;
- result type (`TaskResult<T>` / `far Task<T>` / `TaskResult<nothing>`);
- caller resume point (the crossing expression's span/ID is sufficient
  pre-transport, but record it explicitly);
- back-reference to the enclosing function's `FunctionEffects` entry.

The record must be reachable from `driver.DiagnoseResult` (or the pipeline
equivalent) so a future backend consumer has a single access point — the
same place Task 2's guards read from, replacing their raw AST re-scans if
that simplification falls out naturally (optional, not required).

### 2. Loss-detection tests

Per contract-table row, a test that:

1. compiles a fixture containing that form;
2. asserts the record exists for the site and every listed field is
   populated with the expected value (destination type, captures, result
   type);
3. is written so that dropping a field or skipping a site fails the test
   (assert on concrete values, not just non-nil).

Plus a stability test: the direct-call `MayCross` inference results for a
fixture module are unchanged after this task's changes (guarding against
representation work perturbing inference).

### 3. `RV2-DEBT-024` decision

Apply the epic's criterion: cross-module/higher-order propagation is required
now **iff** the readiness record needs effect bits on imported function
symbols to populate itself or to let the guard fire. Concretely: build a
two-module fixture where module B calls a crossing function exported by
module A, and check whether the record/guard behavior in B's compile is
correct with only direct sema effects. Expected outcome: it is (the crossing
sites live in A and are guarded when A compiles), so the deferral is
reaffirmed — record that evidence in `DEBT.md` and this document. If the
fixture proves otherwise, stop and bring the finding back for review before
implementing propagation.

### 4. Handoff map

Write the "sema metadata → future runtime message" map the epic's Handoff
section requires: per form, which record fields become which parts of a
future transport message, what is OS-neutral, and which tests currently fail
only because transport is absent. This lands in this task document and is
summarized in `NOTES.md` at closeout.

## Proof Gates

- `go test ./internal/sema/ ./internal/buildpipeline/ ./internal/crossinggate/`
- new loss-detection tests named in closing evidence
- `make golden-check` if fixtures were added; `make check`
- `./check_file_sizes.sh -a`; root + `internal/` Sentrux scans

## Exit Criteria

- Every lowering-contract row has a populated record and a test that fails
  on information loss.
- Inference-stability test passes.
- DEBT-024 decision recorded in `DEBT.md` with fixture evidence (reaffirm
  expected; escalation documented if not).
- Handoff map written.

## Results (2026-07-08)

Task 3 is complete. The representation decision from Task 1 remains binding:
Epic 12 uses **guard-before-HIR**. No HIR/MIR crossing nodes, transport,
runtime C, stdlib API, syntax, or public runnable examples were introduced.

### Readiness Record

`internal/sema` now exposes `Result.CrossingLowering`, populated during sema
from the same accepted crossing paths that type-check the source forms. The
record is source/sema-level and explicit:

- `CrossingLoweringKind`: `on` placement, `on` far-handle, `spawn on`,
  `far Task.await`, `far Task.cancel`;
- `CrossingDestinationInfo`: destination expression, resolved type,
  placement/far-handle kind, far-handle anchor symbol, owner-anchor verdict;
- `CrossingCaptureInfo`: captured symbol/expression/span/type, capture mode,
  and exact sema verdict (`copy`, `Placement` copy, far-handle move, owned
  `@shard_movable`, owned builtin Copy);
- `CrossingRemoteOpInfo`: accepted anchored operation through an `on`
  far-handle destination, including method, receiver expression/symbol/type;
- payload/result/handle type fields and a function-symbol back-reference to
  `FunctionEffects`.

Capture classification is not duplicated. `checkOnCaptures` now returns the
accepted capture summary while using the same `classifyOnCapture` branches that
emit SEM3165-SEM3169 diagnostics. Rejected captures are not recorded as accepted
lowering payloads. Each crossing-site append is also gated by a sema-error
checkpoint: if typing the destination, body, captures, result, remote operation,
or far-task receiver reports a new error, no accepted-looking readiness record
is emitted for that rejected site.

Task 2 backend guards were **not** switched to the readiness record in this
slice. That switch is optional and would require a stable public
`DiagnoseResult` accessor over root + dependency sema records. Current guards
remain raw diagnostic guards; future backend consumers should read
`Result.CrossingLowering`.

### Loss-Detection Tests

`internal/sema/crossing_lowering_test.go` asserts concrete field values for:

- `TestCrossingLoweringOnPlacementRecord`: destination/result/capture/function
  back-reference;
- `TestCrossingLoweringOnFarHandleRecord`: far-handle destination, anchor, and
  accepted remote `close` operation;
- `TestCrossingLoweringSpawnOnRecord`: placement destination, movable owned
  capture, payload/result/handle type;
- `TestCrossingLoweringFarTaskAwaitRecord`: receiver, payload/result, handle
  consumption;
- `TestCrossingLoweringFarTaskCancelRecord`: receiver, result, handle
  consumption;
- `TestCrossingLoweringDirectCallEffectStability`: direct-call propagation still
  marks the wrapper `MayCross`, but no synthetic lowering record is created for
  the call site; only the real `on` site has a record.
- `TestCrossingLoweringRejectedOnBodyDoesNotRecord`,
  `TestCrossingLoweringRejectedSpawnOnBodyDoesNotRecord`,
  `TestCrossingLoweringRejectedFarHandleOpDoesNotRecord`,
  `TestCrossingLoweringRejectedFarTaskReuseDoesNotRecordSecondOperation`, and
  `TestCrossingLoweringRejectedFarTaskAwaitAfterCancelDoesNotRecordSecondOperation`:
  rejected crossing sites do not leave false accepted records.

These tests assert concrete form kind, destination, result/handle type, function
effect back-reference, source coordinates, capture coordinates, receiver
coordinates, and negative-space behavior. They fail on contract-relevant
information loss, not just on missing non-nil values.

### `RV2-DEBT-024` Decision

`internal/driver/crossing_readiness_test.go` adds
`TestCrossingReadinessDebt024ModuleImportDoesNotRequireImportedEffects`, a
two-module fixture:

- module A exports a function containing a real `on pool { ret 7; }` crossing;
- module B imports and calls that function;
- module B does not synthesize a lowering record for the imported call, and its
  caller-side imported `MayCross` effect remains deferred;
- module A's dependency sema result still contains the real on-placement
  readiness record.

Conclusion: imported function effect bits are **not required** for the current
guard-before-HIR readiness representation. `RV2-DEBT-024` remains open for
Phase 4 transport lowering or a later effect-system epic, where higher-order and
cross-module caller effects can be designed with their own matrix.

### Handoff Map

| Source form | Readiness fields | Future runtime-message meaning | OS-neutral boundary |
| --- | --- | --- | --- |
| `on placement { ret value; }` | `Destination.Expr/Type`, `Captures`, `PayloadType`, `ResultType`, `Span`, `Function` | destination placement, payload materialization plan, result envelope, caller resume point | `Placement` is compiler/runtime semantic data; no Linux fd or poll primitive is implied. |
| `on far_handle { ... }` | far-handle destination, `AnchorSymbol`, `OwnerAnchored`, `RemoteOps`, captures, result type | route operation through owner handle; execute only accepted anchored remote operations; return `TaskResult<T>` | handle ownership is semantic; concrete wake/fd/event primitive is Phase 4. |
| `spawn on placement { ret value; }` | placement destination, captures, `PayloadType`, `ResultType`, `HandleType` | create remote child task, move/copy accepted payloads, return `far Task<T>` handle | task placement and handle identity are OS-neutral. |
| `far Task<T>.await()` | receiver expr/symbol/type, `ConsumesHandle`, payload/result | consume remote task handle and resume caller with `TaskResult<T>` | await protocol is transport-level, not tied to epoll/eventfd here. |
| `far Task<T>.cancel()` | receiver expr/symbol/type, `ConsumesHandle`, result | consume/cancel remote task handle and return `TaskResult<nothing>` | cancellation route is semantic; wake mechanism is Phase 4. |

### Proof

Commands run:

```bash
go test ./internal/sema -run 'CrossingLowering|FunctionCrossingEffect' -count=1
go test ./internal/driver -run 'CrossingReadinessDebt024' -count=1
go test ./internal/sema ./internal/buildpipeline ./internal/crossinggate -count=1
go test ./internal/sema ./internal/buildpipeline ./internal/crossinggate ./internal/driver -run 'Crossing|SpawnOn|FunctionCrossingEffect' -count=1
git diff --check
./check_file_sizes.sh -a
```

Sentrux:

- root scan: quality `6187`; `check_rules` pass, `rules_checked=8`,
  `violation_count=0`;
- `sentrux check internal`: existing missing `internal/.sentrux/rules.toml`
  gap; the scoped command fails before a scoped quality score, so no scoped
  rule compliance is claimed.

No golden fixtures were added or regenerated for this task.
