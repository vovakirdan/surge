# Task 10: Wake-FD And Shutdown Tests

**Status:** Complete
**Kind:** test writing

## Goal

Add focused tests for poller wakeup and shutdown drain behavior around the fd
registry.

## Scope

- Prove new readiness interest wakes a blocked poller.
- Prove close or cancellation wakes the poller when needed.
- Prove shutdown wakes the poller and does not strand registry waiters.
- Prefer exact tests over broad timeout-sensitive VM regexes.

## Files

- `internal/vm/runtime_v2_fd_registry_contract_test.go`
- `internal/vm/runtime_v2_fd_registry_static_test.go`
- `internal/vm/runtime_v2_fd_registry_wake_test.go`
- `internal/vm/runtime_v2_fd_registry_shutdown_static_test.go`
- trace assertion helpers
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- Green wake-fd tests:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2FDRegistry(WakeFDObservedForInterestAddedDuringPoll|CloseWakePollNotificationProof)$' -count=1 -parallel=1 -p=1 -v --timeout 120s`
- Expected-red cancellation proof for Task 11:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2FDRegistryCancelledInterestWakesPoller$' -count=1 -parallel=1 -p=1 -v --timeout 120s`
- Expected-red shutdown static contract for Task 11:
  `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2FDRegistryShutdownDrainStaticContract$' -count=1 -v --timeout 60s`
- Focused net wake probe:
  `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -count=1 -parallel=1 -p=1 -v --timeout 90s`
- `gofmt -l internal/vm/runtime_v2_fd_registry_contract_test.go internal/vm/runtime_v2_fd_registry_static_test.go internal/vm/runtime_v2_fd_registry_wake_test.go internal/vm/runtime_v2_fd_registry_shutdown_static_test.go`
- `git diff --check`

## Tests Added

- Green: `TestRuntimeV2FDRegistryWakeFDObservedForInterestAddedDuringPoll`.
  Proves new fd interest is visible through wake-fd trace and the registry-only
  poll-build zero counters.
- Green: `TestRuntimeV2FDRegistryCloseWakePollNotificationProof`. Direct C
  behavior proof that the Task 9 close helper calls `rt_net_wake_poll()` and
  broadcasts `io_cv` when it wakes close waiters.
- Expected-red for Task 11:
  `TestRuntimeV2FDRegistryCancelledInterestWakesPoller`. Current failure is
  `io_poll_wake_fd` not increasing after cancellation-side interest removal;
  the test waits for the live `reason=sigusr1` baseline before gate release.
- Expected-red for Task 11:
  `TestRuntimeV2FDRegistryShutdownDrainStaticContract`. Current failure is the
  absence of explicit shutdown request/drain APIs visible from
  `rt_async_internal.h`.

## Done

- Tests identify the wake-fd and shutdown behavior Task 11 must satisfy.
- Timeout-sensitive failures are not hidden under accepted backend-test debt.
- Close wake-fd behavior is recorded as green on current Task 9 code; only
  cancellation removal and shutdown drain remain expected-red for Task 11.
