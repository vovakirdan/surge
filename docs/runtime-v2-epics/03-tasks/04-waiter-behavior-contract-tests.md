# Epic 3 Task 04: Waiter Behavior Contract Tests

**Goal:** add or select tests that prove the current waiter contract before
storage moves.

**Approach:** turn Task 02's dependency map into focused liveness and cleanup
tests. Keep the broad accepted-debt VM regex out of the gate.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `internal/vm/`, `runtime/native/`, `make runtime-v2-check`

---

## Files

- Test: focused files under `internal/vm/`
- Modify: `Makefile` only if a new local target is needed.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Define tests for FIFO-by-key, stale waiter cleanup, wake-before-park,
   cancellation, timeout, close, and shutdown-adjacent behavior.
2. Add the narrowest stable tests first.
3. Run each test locally with timeout wrappers where appropriate.
4. Record tests that are needed but not stable enough for CI yet.

## Done

- The first waiter refactor has behavior tests.
- Unstable or missing probes have owners and follow-up tasks.
