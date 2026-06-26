# Epic 2 Task 05: Runtime/Shard Skeleton

**Goal:** introduce internal `rt_runtime` and `rt_shard` structures with exactly
one shard.

**Approach:** add the smallest skeleton that lets later tasks move field groups
behind owner-named accessors. Keep public ABI and behavior unchanged.

**Skills:** `c-pro`, `static-analysis`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_internal.h`,
`runtime/native/rt_async_state.c`, `make c-check`, `make cppcheck`

---

## Files

- Modify: `runtime/native/rt_async_internal.h`
- Modify: `runtime/native/rt_async_state.c`
- Create or modify: small internal C/header files only if they keep touched
  files within the 500-line rule.
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Read Task 02 ownership map and Task 04 test result.
2. Add internal `rt_runtime` and `rt_shard` types without enabling `N>1`.
3. Add initialization helpers with explicit status codes for recoverable
   failures.
4. Keep existing `panic_msg` paths only where they are legacy behavior.
5. Wire the single shard into `rt_executor` without moving hot behavior yet.
6. Run Task 04 test/check and record result.
7. Run `make c-check`, `make cppcheck`, and `make check`.
8. Run Sentrux root/runtime scan, `health`, and `check_rules`.

## Done

- Public native ABI is unchanged.
- `N=1` is the only runtime shape.
- New helpers use explicit status codes.
- Tests/checks from Task 04 pass or have a recorded blocker unrelated to this
  implementation.
