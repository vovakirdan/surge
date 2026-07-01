# Task 15: Epic Closeout And Static Gates

**Status:** Draft
**Kind:** closeout

## Goal

Close Epic 4 with durable evidence and a precise Epic 5 handoff.

## Scope

- Run final standing gates for the epic.
- Copy durable notes from `NOTES.md` into this epic and `04-evidence.md`.
- Update `README.md` roadmap/status.
- State exactly which Runtime V2 contracts are now implemented and which remain
  deferred.
- Record final line counts and Sentrux scans.

## Files

- `docs/runtime-v2-epics/04-persistent-fd-registry-and-net-lifecycle.md`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- all stable fd-registry tests
- native net benchmark
- `git diff --check`
- Sentrux root, `runtime/`, and `runtime/native/` scans

## Done

- Epic 4 status is updated to complete or blocked with evidence.
- Accepted debt is not confused with green gates.
- Epic 5 starts from an explicit handoff, not an implicit todo list.
