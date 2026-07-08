# Epic 12 Task 6: CI Gates And Quality Closeout

**Status:** pending.
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
