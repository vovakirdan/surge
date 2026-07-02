# Task 10: Epic Closeout And Static Gates

**Status:** Draft
**Kind:** closeout

## Goal

Close Epic 5 with durable evidence, current notes, and a precise Epic 6 handoff.

## Scope

- Run all Epic 5 closeout gates from a clean current checkout.
- Consolidate task evidence into `05-evidence.md`.
- Update `05-per-shard-heap-accounting.md` with the final acceptance matrix.
- Update `README.md` roadmap and artifact list.
- Update `NOTES.md` with the exact Epic 6 starting point.
- Close or record every allocator/accounting debt discovered during Epic 5.
- Keep the language syntax gate visible for the later crossing-syntax epic.

## Files

- Modify: `docs/runtime-v2-epics/05-per-shard-heap-accounting.md`
- Modify: `docs/runtime-v2-epics/05-evidence.md`
- Modify: `docs/runtime-v2-epics/README.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`
- Modify: `docs/runtime-v2-epics/DEBT.md` if Epic 5 owns new or closed debt
- Modify: `docs/RUNTIME_V2.md` only if implemented heap-accounting behavior
  changes the canonical contract wording.

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-heap-check`
- `make runtime-v2-check`
- `make check`
- `git diff --check`
- Root and scoped Sentrux scans plus rule checks
- Any focused heap, net, or channel benchmark required by the final touched
  paths

## Done

- Epic 5 status is updated to complete or blocked with evidence.
- `rt_alloc`, `rt_free`, `rt_realloc`, and `rt_heap_stats` behavior is proven.
- Runtime V2 CI includes the stable heap gate.
- Epic-owned debt is closed or recorded in `DEBT.md`.
- Epic 6 starts from an explicit handoff, not an implicit todo list.
