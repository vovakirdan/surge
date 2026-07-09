# Epic 13 Task 2: Transport Harness Hardening

**Status:** pending.
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

## Debt

- `RV2-DEBT-011`: narrow close for the transport-gate path — update the row
  with evidence; the broad matrix scope stays with Backend/Test Matrix
  Cleanup (do not close the whole row unless the fix genuinely covers all VM
  helpers).
- `RV2-DEBT-018`: record whether empty-output capture plus unique dirs
  removes the class or only instruments it.

## Stop Conditions

- The fix requires touching production runtime code — stop; this task is
  harness-only.
- The transient reproduces and points at a real runtime bug (not artifact
  lifecycle) — stop, record, and raise it as its own debt row before
  continuing.
