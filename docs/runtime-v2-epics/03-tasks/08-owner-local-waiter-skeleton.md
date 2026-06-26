# Epic 3 Task 08: Owner-Local Waiter Skeleton

**Goal:** add the owner-local waiter container behind compatibility APIs.

**Approach:** introduce owner-first internal APIs while keeping current
executor-global behavior observable from user programs.

**Skills:** `c-pro`, `static-analysis`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_internal.h`,
`runtime/native/rt_runtime.c`, waiter module from Task 06

---

## Files

- Modify: `runtime/native/rt_async_internal.h`
- Modify: `runtime/native/rt_runtime.c`
- Modify: waiter module from Task 06
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Add owner-local waiter storage for the single shard.
2. Keep compatibility wrappers for existing call sites.
3. Use explicit status codes for recoverable allocation or lifecycle failures.
4. Run Task 07 checks, behavior tests, static checks, and Sentrux scans.

## Done

- Waiter ownership has a real owner-local home under `N=1`.
- Existing waiter users still behave the same.
