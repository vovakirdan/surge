# Task 7: Poll From Registry

**Status:** Complete
**Kind:** runtime code

## Goal

Build the `poll()` input from persistent fd registry entries instead of
scanning the waiter store.

## Scope

- Replace waiter-store fd discovery in `poll_net_waiters` with registry
  iteration.
- Keep temporary `pollfd` scratch acceptable for the `poll()` backend, but make
  it derive from fd entries.
- Preserve readiness completion semantics for read, write, and accept waits.
- Add evidence that the legacy waiter-derived rebuild path is not used by the
  covered probes.

## Files

- `runtime/native/rt_net.c`
- fd registry implementation/header files
- `runtime/native/rt_async_trace.c`, if trace output needs new counters
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- Task 3 fd lifecycle tests
- focused net wake probe from `LIVENESS_PROBES.md`
- native net benchmark
- `git diff --check`
- Sentrux root and scoped scans

## Done

- Poll construction no longer discovers fds by scanning all waiters.
- Registry-derived polling is covered by behavior and trace evidence.
- Benchmark result is recorded and any regression is explained or blocked.
