# Task 3: Heap Stats Contract Tests

**Status:** Draft
**Kind:** test writing

## Goal

Add focused tests that preserve current heap-stat behavior before moving the
counter storage.

## Scope

- Cover ordinary allocation and free through `rt_heap_stats()`.
- Cover `rt_realloc` grow, shrink, null-pointer, zero-size, and failure-safe
  behavior where a stable proof is possible.
- Cover aligned allocation and aligned reallocation.
- Cover concurrent worker allocation/free accounting without depending on exact
  scheduling order.
- Keep tests focused; do not add the broad accepted-debt VM regex as a gate.
- Record any desired but unstable allocation probe in `05-evidence.md`, not only
  in working notes.

## Files

- Modify: `internal/vm/llvm_native_heap_stats_test.go`
- Create or modify: `internal/vm/runtime_v2_heap_accounting_contract_test.go`
- Modify: `docs/runtime-v2-epics/05-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Checks

- `go test ./internal/vm -run '^TestLLVMNative.*Heap.*|^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s`
- `gofmt -l internal/vm/llvm_native_heap_stats_test.go internal/vm/runtime_v2_heap_accounting_contract_test.go`
- `git diff --check`

## Done

- Heap-stat behavior tests pass on the current implementation.
- The tests are narrow enough to become part of `runtime-v2-heap-check` later.
- Missing or unstable probes have an owner and close condition.
