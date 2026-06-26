# Epic 2 Task 04: Runtime/Shard Skeleton Tests

**Goal:** add tests or static checks that prove the `N=1` skeleton exists
without changing source-visible runtime behavior.

**Approach:** write checks before the skeleton implementation where possible.
Because the skeleton is internal C structure, acceptable tests include native
compile-time checks, initialization smoke tests, and trace-visible invariants.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`, `static-analysis`

**Tech Details:** `runtime/native/`, `internal/vm/`, `Makefile`,
`docs/runtime-v2-epics/02-ci-test-contract.md`

---

## Files

- Test: `internal/vm/mt_executor_test.go` or a new focused test file under
  `internal/vm/`
- Test or helper: `runtime/native/rt_*`
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Choose the narrowest proving surface:
   - compile-time shape check if no behavior is observable yet;
   - native initialization smoke if a public runtime entry point can expose it;
   - trace invariant if the implementation adds a debug counter.
2. Write the failing or pending test/check before moving code.
3. Run the exact local command and record failure mode.
4. Verify the test does not depend on `N>1`, fd registry, waiter locality, or
   crossing syntax.
5. Decide whether this test belongs in `make runtime-v2-check` immediately or
   only after Task 05.
6. Update evidence and notes.

## Done

- The skeleton implementation task has a concrete proving test or a recorded
  reason why only compile/static checks are possible.
- The test has an owner for CI inclusion.
- No broad accepted-debt command is treated as green evidence.
