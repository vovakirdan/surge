# Epic 12 Task 5: Test Harness Hardening

**Status:** complete; not promoted to implementation in Epic 12.
**Kind:** test harness; Go test helpers only, no compiler or runtime logic.
**Depends on:** Task 1 (debt reconciliation, current failure modes).

## Goal

Make the crossing-readiness matrix trustworthy: close or reassign the
artifact/matrix debts so that the backend-stage rows Task 2 adds (VM and
LLVM per form) can run repeatedly and concurrently without flakes.

## Starting State (from `DEBT.md`, re-verify in Task 1)

- `RV2-DEBT-011`: VM LLVM build/test artifacts are keyed by **test name**
  under `target/debug/.tests/`, so overlapping runs of the same test race on
  artifact files (missing `build.stdout` class failures).
- `RV2-DEBT-018`: rare in-suite transient `run failed (exit=1, dur=~2.3ms)`
  with empty stdout/stderr; suspected same artifact/binary lifecycle class
  as 011.
- `RV2-DEBT-001`: the broad focused command
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` is not a green gate.
- `RV2-DEBT-002`: MT liveness group shares one host budget under
  `t.Parallel`; sync-compat lane keeps its 10ms slice envelope
  (`RV2-DEBT-017`).

## Scope

In: per-run artifact isolation for VM/LLVM build-run helpers; reproduction
or class-elimination of the 018 transient; narrowing/reassigning 001/002.

Out: rewriting the whole VM/native/LLVM matrix (that is the later
test-matrix epic if 001/002 are reassigned); runtime C changes; test
*content* changes to crossing fixtures (Tasks 2/4 own those).

## Steps

### 1. DEBT-011: per-run unique artifact dirs

Change the VM build/test helpers so artifact directories are unique per run
(test name + PID/random suffix, or `t.TempDir()`), or serialized by lock
where uniqueness is impractical. Requirements:

- two concurrent invocations of the same focused test command pass
  repeatedly (prove with a loop, e.g. two parallel `go test -run <X>`
  processes x 10 iterations, recorded in evidence);
- artifact cleanup does not delete another run's files;
- failure output still names the artifact path for debugging.

### 2. DEBT-018: transient class

After step 1 lands, re-run the recorded reproduction envelope (in-suite runs
that previously showed the transient) enough times to state one of: (a) the
class is eliminated by per-run isolation — close 018 referencing 011's fix;
(b) it reproduces — diagnose to root cause within this task; (c) it neither
reproduces nor is explained — narrow the debt text to the new observation
window and keep it open with the later matrix epic as owner. No silent
disappearance: the disposition must be written.

### 3. DEBT-001/002: narrow or reassign

Per Task 1's reconciliation: implement only what the crossing-readiness
matrix needs (likely nothing beyond steps 1-2; the crossing rows are
`internal/crossinggate` + focused `internal/vm` LLVM build tests). Then
update `DEBT.md`:

- 001: either the broad command is green (unlikely in this epic) or the debt
  is reassigned to a named backend-matrix epic with the crossing rows
  explicitly carved out as already-stable;
- 002: same treatment; this epic must not touch MT liveness budgets unless a
  crossing row actually runs inside that group.

The epic boundary applies: do not close unrelated debt just because it is
nearby.

## Proof Gates

- concurrency loop evidence for step 1 (commands + iteration counts)
- `go test ./internal/vm -run <focused crossing/LLVM rows>` stable across
  the recorded repetition count
- `go test ./internal/crossinggate/` repeated (e.g. `-count=10`) green
- `make check`; `./check_file_sizes.sh -a`; root Sentrux scan
  (`internal/` scope; no `runtime/` scan needed — no C changes)

## Exit Criteria

- Overlapping identical focused VM/LLVM test runs cannot race artifacts.
- 018 has a written disposition (closed, root-caused, or narrowed with
  owner).
- 001/002 rows in `DEBT.md` updated with either closure evidence or a named
  new owner; no stale "Epic 12" placeholders remain anywhere in `DEBT.md`.

## Results (2026-07-08)

Task 5 was not promoted to code changes in Epic 12. The crossing-readiness
matrix added by Tasks 2 and 4 uses `buildpipeline.Compile` through
`internal/buildpipeline` and `internal/crossinggate`; it does not use the
`internal/vm` artifact helpers that create files under `target/debug/.tests`.

Evidence:

- `internal/crossinggate/crossing_gate_test.go` routes backend-stage crossing
  fixtures through `diagnoseBackend`, which calls `buildpipeline.Compile`.
- `internal/crossinggate/integration_test.go` reuses the same helper for Task
  4 integration fixtures.
- `rg -n "newTestArtifacts|target/debug/.tests|buildpipeline.Compile|diagnoseBackend" internal/crossinggate internal/buildpipeline internal/vm/test_helpers_test.go internal/vm/mt_executor_test.go`
  shows `newTestArtifacts` is confined to `internal/vm` tests, while the
  crossinggate backend rows use `buildpipeline.Compile`.
- Task 2 recorded the same boundary after adding backend-unavailable guards:
  backend-stage rows do not create executable VM/LLVM artifacts.

Disposition:

- `RV2-DEBT-001` and `RV2-DEBT-002` remain owned by the future
  **Backend/Test Matrix Cleanup** epic; they do not block compile-time
  crossing readiness.
- `RV2-DEBT-011` remains open and is reassigned to **Backend/Test Matrix
  Cleanup**. Epic 12 did not depend on the artifact helper, so per-run unique
  artifact directories would be unrelated churn here.
- `RV2-DEBT-018` remains open with `RV2-DEBT-011` under **Backend/Test Matrix
  Cleanup**. Epic 12 did not reproduce it in crossing-adjacent focused probes
  and did not exercise the suspected artifact lifecycle path.

No VM, runtime, compiler, or test-harness code changed in this task.
