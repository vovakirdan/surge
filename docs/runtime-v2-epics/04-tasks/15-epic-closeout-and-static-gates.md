# Task 15: Epic Closeout And Static Gates

**Status:** Complete
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
- `docs/runtime-v2-epics/04-tasks/15-epic-closeout-and-static-gates.md`
- `docs/runtime-v2-epics/04-tasks/README.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/DEBT.md` if wording is stale

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

## Result

Epic 4 is complete with accepted debt. The current `poll()` backend now polls
fd-registry snapshots instead of rebuilding fd rows from the full waiter store,
stable fd-registry proofs run through `make runtime-v2-check`, and closeout
evidence is recorded in `04-evidence.md`.

Open debt remains explicit in `DEBT.md`: broad VM/backend tests,
timeout-sensitive tests, over-target `rt_async_state.c` and `rt_net.c`, copied
raw-fd net handles, local-only timing-heavy fd-registry probes, and normal
lifecycle wiring for `rt_executor_request_shutdown`.
