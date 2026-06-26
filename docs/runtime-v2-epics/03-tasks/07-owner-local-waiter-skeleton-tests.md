# Epic 3 Task 07: Owner-Local Waiter Skeleton Tests

**Goal:** prove the owner-local waiter skeleton can exist under `N=1` without
changing observable behavior.

**Approach:** add static or focused runtime checks for the new owner container
before implementing it.

**Skills:** `task-breakdown`, `static-analysis`

**Tech Details:** `runtime/native/rt_runtime.c`,
`runtime/native/rt_async_internal.h`, `internal/vm/`

---

## Files

- Test: focused `internal/vm/` or static checks.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Define the owner-local skeleton invariant for `N=1`.
2. Add a failing or pending check where possible.
3. Confirm no `N>1`, fd registry, or crossing syntax is required.

## Done

- Task 08 has a concrete proof target.
- The skeleton test does not encode future multi-shard behavior.
