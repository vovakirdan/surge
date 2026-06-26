# Epic 2 Task 06: Scheduler Shape Tests

**Goal:** add or select tests that prove scheduler field movement preserves
current behavior.

**Approach:** test before moving scheduler containers. Use exact existing MT
probes where they already prove behavior, and add a missing invariant only if
the move changes a sleep, wake, queue, or trace boundary.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `internal/vm/mt_executor_test.go`,
`docs/runtime-v2-epics/LIVENESS_PROBES.md`, `SURGE_SKIP_TIMEOUT_TESTS=0`

---

## Files

- Test: `internal/vm/mt_executor_test.go`
- Test: `internal/vm/mt_correctness_test.go`
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Select exact scheduler probes:
   - `TestMTWorkStealing`
   - `TestMTSeededScheduler`
   - any one-worker progress probe touched by the field group.
2. Decide whether a parked-with-work invariant is required for this move.
3. If required, write the missing invariant test before implementation.
4. Run selected tests with `SURGE_SKIP_TIMEOUT_TESTS=0`.
5. Record pass/fail, output path, and whether the test can join CI.
6. Update the CI contract if the selected subset is stable.

## Done

- Scheduler migration has named tests.
- Any missing invariant is either implemented or explicitly assigned to a later
  owner with a blocker status.
- No scheduler task closes on "watch for hangs".
