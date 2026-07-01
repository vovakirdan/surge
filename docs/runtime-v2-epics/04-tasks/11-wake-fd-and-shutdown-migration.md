# Task 11: Wake-FD And Shutdown Migration

**Status:** Draft
**Kind:** runtime code

## Goal

Migrate wake-fd notification and shutdown drain behavior to the fd registry
contract.

## Scope

- Wake the I/O thread when registry interest is added, removed, closed, or
  shutdown changes.
- Drain or invalidate registry entries during shutdown without leaving parked
  net waiters.
- Preserve current executor lifecycle behavior.
- Keep cross-shard wake protocol out of scope.

## Files

- `runtime/native/rt_net.c`
- fd registry implementation/header files
- `runtime/native/rt_async_state.c`, only if shutdown integration requires it
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- Task 10 wake-fd/shutdown tests
- focused net wake probe from `LIVENESS_PROBES.md`
- `git diff --check`
- Sentrux root and scoped scans

## Done

- Poller wake and shutdown behavior are registry-aware.
- No cross-shard wake assumptions are introduced.
- Shutdown evidence is recorded with exact commands.
