# Task 6: Net Wait Registration

**Status:** Draft
**Kind:** runtime code

## Goal

Route net wait registration through registry-owned fd entries while preserving
current wake behavior.

## Scope

- On accept/read/write wait registration, find or create the owning fd entry.
- Attach readiness interest to that entry.
- Keep current waiter wake semantics until poll-from-registry lands.
- Wake the I/O thread when interest changes can unblock polling.
- Record any temporary compatibility bridge to owner-local waiters.

## Files

- `runtime/native/rt_net.c`
- fd registry implementation/header files from Task 5
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- Task 3 fd lifecycle tests in their current approved mode
- focused net wake probe from `LIVENESS_PROBES.md`
- `git diff --check`
- Sentrux root and scoped scans

## Done

- Net wait registration has a registry-owned fd entry for each live fd.
- Existing net behavior remains green.
- Compatibility bridges are named and scheduled for removal or validation.
