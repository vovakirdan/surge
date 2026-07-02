# Task 1: Kickoff Baseline And Sentrux

**Status:** Draft
**Kind:** evidence

## Goal

Record the exact Epic 5 starting state before heap-accounting work begins.

## Scope

- Capture branch, commit, and `git status --short`.
- Capture line counts for `rt_alloc.c`, `rt_runtime.c`,
  `rt_async_internal.h`, `rt_async_state.c`, and touched test files.
- Record accepted debt from `DEBT.md` and confirm which debt is not in Epic 5
  scope.
- Run or record root, `runtime/`, and `runtime/native/` Sentrux scans.
- Run current heap-stat smoke tests and the existing Runtime V2 gates.
- Define the exact gate set for Tasks 2-7.

## Files

- `docs/runtime-v2-epics/05-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/05-per-shard-heap-accounting.md`
- `docs/runtime-v2-epics/DEBT.md`

## Checks

- `git diff --check`
- `go test ./internal/vm -run '^TestLLVMNative(HeapStats|BufferedChannelAllocatesSingleBlock)$' -count=1 -v --timeout 120s`
- `make runtime-v2-check`
- Sentrux repository scan and rule check
- Sentrux `runtime/` scan and rule check
- Sentrux `runtime/native/` scan and rule check

## Done

- Starting evidence is recorded in `05-evidence.md`.
- Accepted debt is named and separated from Epic 5 scope.
- Sentrux rule status is recorded as pass/fail evidence.
- Task 2 has enough context to map heap accounting without rediscovery.
