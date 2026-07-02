# Task 6: Alloc/Free/Realloc Accounting Migration

**Status:** Draft
**Kind:** runtime code

## Goal

Route allocation events through the accounting cells while preserving current
allocation, free, and realloc semantics.

## Scope

- Move `record_alloc`, `record_free`, and `record_realloc` writes away from the
  old file-scope global counter source of truth.
- Preserve no-update semantics for failed allocation and failed realloc.
- Preserve `rt_free(NULL, ...)` semantics.
- Preserve `rt_realloc(NULL, old, new, align)` allocation behavior.
- Preserve `rt_realloc(ptr, old, 0, align)` free behavior.
- Preserve aligned realloc's allocate-copy-free implementation.
- Ensure live counters cannot unsigned-underflow when allocation and free land
  in different cells.
- Keep `rt_array_forget_allocation` on the same free paths that need it.

## Files

- Modify: `runtime/native/rt_alloc.c`
- Modify if needed: `runtime/native/rt_heap_accounting.c`
- Modify if needed: `runtime/native/rt_heap_accounting.h`
- Modify: `internal/vm/runtime_v2_heap_accounting_contract_test.go`
- Modify: `docs/runtime-v2-epics/05-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Checks

- `go test ./internal/vm -run '^TestLLVMNative.*Heap.*|^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s`
- `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s`
- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `git diff --check`
- Root and scoped Sentrux scans plus rule checks

## Done

- Allocation events use the new accounting cells.
- Current heap-stat behavior tests stay green.
- The old global counters are no longer the source of truth.
- Any new allocator/accounting debt is closed or added to `DEBT.md`.
