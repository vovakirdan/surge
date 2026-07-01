# Task 2: FD Registry Dependency Map

**Status:** Draft
**Kind:** design map

## Goal

Map the current net readiness lifecycle before any registry code is added.

## Scope

- Map listener and connection creation, wait registration, poll readiness,
  close, cancellation, wake-fd, and shutdown paths.
- Identify every current user of `rt_net_poll_scratch`,
  `rt_executor_visit_net_waiters`, and net waiter key wakeups.
- Classify each dependency as registry-owned, waiter-owned, wake-fd owned,
  shutdown-owned, or later-epic work.
- Name the first safe implementation boundary.

## Files

- `docs/runtime-v2-epics/04-fd-registry-dependency-map.md`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- `git diff --check`
- Targeted `rg` evidence for all mapped symbols

## Done

- Every current poll rebuild dependency has an owner classification.
- Deferred work is explicit and not hidden in implementation tasks.
- Task 3 and Task 5 can use the map as their contract source.
