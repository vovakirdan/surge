# Task 5: Registry Container Skeleton

**Status:** Draft
**Kind:** runtime code

## Goal

Add the shard-local fd registry container without changing net behavior.

## Scope

- Introduce registry and entry types behind owner-named APIs.
- Store the registry under the `N=1` shard owner path.
- Add init/free helpers and no-op or compatibility accessors as needed.
- Preserve current `poll()` rebuild behavior until Task 7.
- Keep new code in cohesive files under the 500-line target.

## Files

- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_runtime.c`
- `runtime/native/rt_net.c`
- new `runtime/native/rt_fd_registry.c` or similarly specific file, if the
  dependency map justifies it
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- Task 4 static check
- `git diff --check`
- Sentrux root and scoped scans

## Done

- Registry lifetime is initialized and freed with the owning shard/runtime.
- No net behavior changes are claimed.
- Line-count outcome is recorded for every touched over-limit file.
