# Task 11: Wake-FD And Shutdown Migration

**Status:** Complete
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
- Do not wire shutdown into normal program lifecycle in this task.

## Files

- `runtime/native/rt_async_waiter.c`
- `runtime/native/rt_fd_registry.c`
- `runtime/native/rt_fd_registry.h`
- `runtime/native/rt_shutdown.c`
- `runtime/native/rt_async_internal.h`
- `internal/vm/runtime_v2_fd_registry_shutdown_static_test.go`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/04-tasks/README.md`

Not touched:

- `runtime/native/rt_async_state.c`
- `runtime/native/rt_net.c`
- Makefile, CI, `STATS.md`, `DEBT.md`

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check`
- `make check`
- Task 10 wake-fd/shutdown tests
- focused net wake probe from `LIVENESS_PROBES.md`
- `git diff --check`
- Sentrux root and scoped scans

Focused implementation checks run:

- `go test -tags runtime_v2_pending ./internal/vm -run
  '^TestRuntimeV2FDRegistryShutdownDrain(StaticContract|Behavior)$'
  -count=1 -v --timeout 60s`
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags
  runtime_v2_pending ./internal/vm -run
  '^TestRuntimeV2FDRegistryCancelledInterestWakesPoller$' -count=1
  -parallel=1 -p=1 -v --timeout 120s`
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags
  runtime_v2_pending ./internal/vm -run
  '^TestRuntimeV2FDRegistry(WakeFDObservedForInterestAddedDuringPoll|CloseWakePollNotificationProof)$'
  -count=1 -parallel=1 -p=1 -v --timeout 120s`
- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMTNetWaiterWakeupLatency$' -count=1 -parallel=1 -p=1 -v
  --timeout 90s`
- `make c-check`
- `make cppcheck`
- `git diff --check`

## Done

- Poller wake and shutdown behavior are registry-aware.
- No cross-shard wake assumptions are introduced.
- Cancellation-side removal wakes the poller only after the last same-key
  open net interest is detached; readiness-completion paths do not write an
  extra wake byte.
- Shutdown drain is registry-owned through
  `rt_fd_registry_drain_shutdown_net_waiters_locked`; public wrappers live in
  `runtime/native/rt_shutdown.c`.
- `rt_executor_request_shutdown` is not wired into normal lifecycle.
- Shutdown evidence is recorded with exact commands.
