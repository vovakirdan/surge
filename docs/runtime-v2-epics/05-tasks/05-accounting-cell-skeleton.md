# Task 5: Accounting Cell Skeleton

**Status:** Draft
**Kind:** runtime code

## Goal

Introduce runtime or shard-owned heap-accounting cells without changing public
allocation behavior.

## Scope

- Add the minimal internal data structures selected by Task 2.
- Keep `rt_alloc`, `rt_free`, `rt_realloc`, and `rt_heap_stats` signatures
  unchanged.
- Support current multi-worker `N=1` without replacing global atomics with one
  contended shard counter block.
- Provide an explicit cold accounting path for allocations before runtime state
  or outside a worker context.
- Use owner-first APIs and explicit status codes for any new recoverable
  initialization path.
- Do not move counter writes from `rt_alloc.c` yet unless the task plan proves
  the skeleton and migration cannot be separated safely.

## Files

- Modify: `runtime/native/rt_async_internal.h`
- Modify: `runtime/native/rt_runtime.c`
- Modify: `runtime/native/rt_alloc.c`
- Create if needed: `runtime/native/rt_heap_accounting.c`
- Create if needed: `runtime/native/rt_heap_accounting.h`
- Modify: `internal/vm/runtime_v2_heap_accounting_static_test.go`
- Modify: `docs/runtime-v2-epics/05-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Checks

- `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s`
- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `git diff --check`
- Root and scoped Sentrux scans plus rule checks

## Done

- The accounting cell skeleton exists under runtime or shard ownership.
- No public allocation behavior changes.
- Static shape tests pass for the skeleton parts this task owns.
- Touched over-limit files have recorded line-count outcomes.
