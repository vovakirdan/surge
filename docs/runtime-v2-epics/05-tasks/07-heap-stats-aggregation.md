# Task 7: Heap Stats Aggregation

**Status:** Draft
**Kind:** runtime code

## Goal

Make `rt_heap_stats()` aggregate all accounting cells on read while preserving
the public snapshot behavior.

## Scope

- Aggregate worker, thread, shard, and cold accounting cells selected by Task 2.
- Capture the stats snapshot before allocations needed to build the
  `SurgeHeapStats` result.
- Preserve `HeapStats` layout and public ABI.
- Preserve `rc_increments` and `rc_decrements` as zero until a separate
  reference-counting epic owns them.
- Keep aggregation clear and testable; avoid hidden scheduler coupling.
- Update docs only where they describe implemented behavior.

## Files

- Modify: `runtime/native/rt_alloc.c`
- Modify if needed: `runtime/native/rt_heap_accounting.c`
- Modify if needed: `runtime/native/rt_heap_accounting.h`
- Modify: `internal/vm/llvm_native_heap_stats_test.go`
- Modify: `internal/vm/runtime_v2_heap_accounting_contract_test.go`
- Modify: `internal/vm/runtime_v2_heap_accounting_static_test.go`
- Modify: `docs/runtime-v2-epics/05-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Checks

- `go test ./internal/vm -run '^TestLLVMNative(HeapStats|BufferedChannelAllocatesSingleBlock)$' -count=1 -v --timeout 120s`
- `go test ./internal/vm -run '^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s`
- `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s`
- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `git diff --check`
- Root and scoped Sentrux scans plus rule checks

## Done

- `rt_heap_stats()` reports aggregate values from all accounting cells.
- Snapshot semantics remain compatible with existing LLVM heap tests.
- Static checks no longer find the old global source-of-truth shape.
