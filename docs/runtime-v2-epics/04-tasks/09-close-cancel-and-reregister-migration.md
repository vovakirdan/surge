# Task 9: Close, Cancel, And Re-register Migration

**Status:** Draft
**Kind:** runtime code

## Goal

Move close, cancellation, stale-wake, and re-registration cleanup into the fd
registry lifecycle.

## Scope

- Remove readiness interest on cancellation without disturbing other interests
  for the same fd.
- Mark fd entries closed before or during raw fd close.
- Guard completions by generation, closed state, or an equivalent stale-wake
  mechanism.
- Ensure a reused numeric fd cannot complete an old waiter.
- Wake affected waiters exactly once.

## Files

- `runtime/native/rt_net.c`
- fd registry implementation/header files
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- Task 8 close/cancel/re-register tests
- focused net wake probe from `LIVENESS_PROBES.md`
- `git diff --check`
- Sentrux root and scoped scans

## Done

- Close and cancellation are registry-owned lifecycle transitions.
- Stale wake behavior is proved.
- No compatibility cleanup path remains hidden or undocumented.
