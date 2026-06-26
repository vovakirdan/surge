# Epic 3 Task 05: Waiter Module Extraction Tests

**Goal:** protect extraction of waiter key/list helpers with static and compile
checks.

**Approach:** add checks that make the extracted module boundary visible without
changing behavior.

**Skills:** `static-analysis`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_internal.h`,
`runtime/native/rt_async_state.c`, `make c-check`, `make cppcheck`

---

## Files

- Test or check: `runtime/native/` and focused Go/static checks if needed.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Choose compile/static checks that catch wrong waiter declarations or owners.
2. Run checks before extraction and record baseline.
3. Confirm the checks do not depend on owner-local behavior yet.

## Done

- Task 06 has a pre-move safety check.
- The checks are narrow enough to keep CI stable.
