# Epic 13 Task 2: Transport Harness Hardening

**Status:** complete as of 2026-07-09.
**Kind:** test harness. Narrow `RV2-DEBT-011`/`RV2-DEBT-018` promotion; not
the broad matrix rewrite.
**Depends on:** Task 1 (baseline, map).

## Goal

Make the test harness that will carry every Epic 13 native execution row
trustworthy: per-run unique build/run artifact directories (or equivalent
locking) and mandatory empty-output capture, so a transport gate failure is
always attributable — a real regression, never an artifact race or a silent
empty-output transient.

## Why This Is Promoted Now

Epic 12 deferred these debts because its crossing rows were compile-only
(`buildpipeline`/`crossinggate`). Epic 13 executes: Tasks 3, 8, 9, 10 add
`SURGE_SHARDS=1,2,8` rows through the `internal/vm` LLVM build/run harness.
A lost-wake negative control (Task 3) on a flaky harness proves nothing, and
the epic's own rule — "new gate stable before wiring into `runtime-v2-check`"
— is unachievable on one.

## Starting State (verify and re-pin)

- Artifact root keyed by test name: `internal/vm/test_helpers_test.go:218`
  (`target/debug/.tests/<name>`). Two concurrent runs of the same test (or
  overlapping `-run` patterns across packages) can delete or race each
  other's `build.stdout`, binaries, or run logs (`RV2-DEBT-011`).
- Rare transient: `run failed (exit=1, dur=~2.3ms)` with EMPTY stdout/stderr,
  never reproduced focused (`RV2-DEBT-018`); suspected artifact/binary
  lifecycle.
- Existing helper surface: enumerate the helpers in
  `internal/vm/test_helpers_test.go` that create/clean artifact dirs, run the
  built binary, and capture output; list every call site that Epic 13 rows
  will use.

## Scope

In: `internal/vm` test helpers used by the future transport rows; artifact
directory naming; output capture; a stress proof that overlap is safe.

Out: rewriting the broad VM/native/LLVM matrix (`RV2-DEBT-001`), MT liveness
group budgets (`RV2-DEBT-002`), any production runtime/compiler code, any
change to what existing tests assert.

## Steps

### 1. Test-first: reproduce or pin the race window

Write (or adapt) a focused overlap probe: run the same build-test helper
concurrently N times against the same test name and demonstrate the artifact
collision (file deleted or truncated under a sibling's feet), or — if it does
not reproduce on this host — pin the race window by code reading with
`file:line` evidence and write the probe anyway as the regression guard.

### 2. Per-run unique artifact directories

Change the helper to key artifact directories by test name PLUS a per-run
unique component (`os.MkdirTemp` under `target/debug/.tests/`, or
name+PID+counter). Requirements:

- concurrent identical invocations never share a directory;
- directories are cleaned on success, retained on failure with a printed
  path, and bounded (a `t.Cleanup` or an age-based sweep so `.tests/` does
  not grow without limit across CI runs);
- no change to what tests assert; only where artifacts live.

If unique dirs are prohibitively invasive, an flock-based per-name lock is
the fallback — record why.

### 3. Empty-output capture

Harden the run helper: when the built binary exits non-zero with empty
stdout+stderr, capture and print the exit status, signal (if any), `dmesg`-
style hint if available, artifact directory path, and binary stat — enough
that the next `RV2-DEBT-018` occurrence is diagnosable instead of folklore.

### 4. Overlap stress proof

Run the Step 1 probe (>= 10 iterations, >= 2 concurrent processes) green on
the new helper. Then run the full existing runtime gate twice to prove no
behavior change: `make runtime-v2-check` x2.

## Proof

- Step 1 probe demonstrates the collision on the old helper (or pins it by
  evidence) and passes on the new one.
- `make runtime-v2-check` twice consecutively green.
- `make check` green.

## Result

Task 2 hardens the `internal/vm` LLVM build/run harness used by later
transport rows without touching production runtime/compiler code.

Implementation:

- `newTestArtifacts` now delegates to `newTestArtifactsWithName`, which uses
  `os.MkdirTemp` under `target/debug/.tests/` and derives the source basename
  from the unique artifact directory. This makes the artifact dir, LLVM output
  binary, and LLVM tmp dir per-run unique for identical logical test names.
- Artifact cleanup keeps failure artifacts and logs artifact dir, binary stat,
  tmp dir, repro command, and `run.diagnostics`; successful tests remove the
  output binary, tmp dir, registry entry, and artifact dir.
- The LLVM run helpers write `run.diagnostics` for non-zero empty-output exits
  and timeout/non-`ExitError` fatal paths. Diagnostics do not replace
  stdout/stderr, so existing negative tests keep their assertion surface.
- `runBinaryWithTimeout` can resolve artifact metadata for binaries built by
  `buildLLVMProgramFromSource`; it now preserves `run.stdout`, `run.stderr`,
  `run.exit_code`, and empty-output diagnostics without changing the returned
  expected outputs.

New tests:

- `TestVMTestArtifactsArePerRunUnique` proves two helper calls in the same test
  get distinct artifact/output/tmp paths.
- `TestVMTestArtifactsOverlapStress` runs ten parallel LLVM build/run subtests
  with the same logical name and verifies no artifact/output/tmp path repeats.
- `TestRunBinaryWithTimeoutReportsEmptyOutputDiagnostics` proves empty-output
  non-zero exits return unchanged stdout/stderr, preserve run artifacts, and
  write diagnostic metadata.

Evidence:

- Focused overlap proof passed:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestVMTestArtifactsOverlapStress$' -count=1 -parallel=10 -p=1 -v --timeout 180s`.
- Focused helper proof passed:
  `go test ./internal/vm -run '^(TestVMTestArtifactsArePerRunUnique|TestRunBinaryWithTimeoutReportsEmptyOutputDiagnostics)$' -count=10 -parallel=2 -p=1 -v --timeout 60s`.
- `make runtime-v2-check` passed twice consecutively after the final
  run-artifact retention update. Final perf rows included
  `steady-state-control=8.230/req` and then `8.122/req`, with
  `accept_owner_active_shards=8`.
- `make check` passed after the lint cleanup (`intrange` loop style).
- `./check_file_sizes.sh -a` passed: 745 files, 0 over limit, 100% good.
- `sentrux check .` passed with quality `6189`; `sentrux check internal`
  passed with quality `6531`.
- An intermediate second full-gate attempt exposed `RV2-DEBT-027`
  (`TestMTChannelParkUnpark` / `panic: async: double poll`), which is a
  non-empty-stderr runtime liveness flake rather than an artifact/empty-output
  harness failure. Focused rerun of that test passed 10/10.
- Independent rereview reported no blockers/P1/P2 after the diagnostics and
  overlap-stress corrections.

## Debt

- `RV2-DEBT-011`: transport-gate slice is complete. The whole debt row stays
  open for the broad matrix cleanup because duplicate focused VM command
  orchestration is outside this task.
- `RV2-DEBT-018`: instrumented, not closed. During implementation one
  empty-output occurrence was retained with `run.diagnostics`, proving the
  failure is now attributable, but not proving the root cause impossible.

## Stop Conditions

- The fix requires touching production runtime code — stop; this task is
  harness-only.
- The transient reproduces and points at a real runtime bug (not artifact
  lifecycle) — stop, record, and raise it as its own debt row before
  continuing.
