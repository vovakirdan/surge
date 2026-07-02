# Task 4: Heap Accounting Static Shape Tests

**Status:** Draft
**Kind:** test/static checks

## Goal

Add static checks that describe the target heap-accounting shape before runtime
code moves.

## Scope

- Add a pending Runtime V2 static test for shard or runtime-owned accounting
  cells.
- Assert that the four public heap stats are not stored as one process-global
  source-of-truth counter set.
- Assert that public allocation ABI remains stable.
- Assert that `rt_heap_stats()` has an aggregation path rather than direct loads
  from the old global counters.
- Keep expected-red results explicit until Tasks 5-7 make the checks pass.

## Files

- Create: `internal/vm/runtime_v2_heap_accounting_static_test.go`
- Modify: `docs/runtime-v2-epics/05-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Checks

- Expected red before implementation:
  `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s`
- `gofmt -l internal/vm/runtime_v2_heap_accounting_static_test.go`
- `git diff --check`

## Done

- The static test fails for the current global-counter shape with actionable
  output.
- The expected passing shape is clear enough for Task 5 and Task 7.
