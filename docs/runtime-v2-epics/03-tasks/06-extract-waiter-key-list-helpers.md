# Epic 3 Task 06: Extract Waiter Key/List Helpers

**Goal:** extract cohesive waiter key and list operations from
`rt_async_state.c` without changing behavior.

**Approach:** move only key/list helper code with the API shape needed by later
owner-local storage. Keep the global backing store until Task 08.

**Skills:** `code-refactoring`, `c-pro`, `static-analysis`

**Tech Details:** `runtime/native/rt_async_state.c`,
`runtime/native/rt_async_internal.h`, new small `runtime/native/rt_*` files

---

## Files

- Modify: `runtime/native/rt_async_state.c`
- Modify: `runtime/native/rt_async_internal.h`
- Create or modify: one cohesive waiter module under `runtime/native/`
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Move only helpers proven by Tasks 02 and 05.
2. Keep public ABI and behavior unchanged.
3. Record line-count changes for every touched over-limit file.
4. Run behavior tests, static checks, and Sentrux scans.

## Done

- The helper boundary is explicit.
- `rt_async_state.c` shrinks or stays flat with a recorded reason.
