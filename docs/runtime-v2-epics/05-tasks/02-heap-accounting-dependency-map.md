# Task 2: Heap Accounting Dependency Map

**Status:** Draft
**Kind:** design map

## Goal

Map all current heap-accounting producers, consumers, and thread contexts before
choosing the accounting cell model.

## Scope

- Map `rt_alloc`, `rt_free`, `rt_realloc`, `record_alloc`, `record_free`,
  `record_realloc`, and `rt_heap_stats`.
- Identify every direct consumer of `rt_heap_stats()` behavior in tests, VM
  intrinsics, LLVM lowering, and docs.
- Classify allocation callers by context: main thread, worker, I/O thread,
  blocking worker, and cold non-runtime path.
- Identify when runtime state exists and how `rt_alloc.c` can access accounting
  without depending on scheduler internals.
- Propose the minimal accounting cell model for Task 5.

## Files

- Create: `docs/runtime-v2-epics/05-heap-accounting-dependency-map.md`
- Modify: `docs/runtime-v2-epics/05-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`
- Read: `runtime/native/rt_alloc.c`
- Read: `runtime/native/rt_runtime.c`
- Read: `runtime/native/rt_async_state.c`
- Read: `runtime/native/rt_async_internal.h`
- Read: `internal/vm/llvm_native_heap_stats_test.go`
- Read: `internal/vm/intrinsic.go`
- Read: `internal/vm/intrinsic_debug.go`

## Checks

- `rg -n 'heap_alloc_count|heap_free_count|heap_live_blocks|heap_live_bytes|rt_heap_stats|rt_alloc\\(|rt_free\\(|rt_realloc\\(' runtime internal docs`
- `git diff --check`

## Done

- The dependency map names every accounting path and observable contract.
- The map states the selected accounting-cell direction for Task 5.
- Any rejected accounting model is recorded with the reason it was rejected.
