# Epic 12 Task 3: Lowering Readiness Representation

**Status:** pending.
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
