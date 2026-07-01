# Task 10: Wake-FD And Shutdown Tests

**Status:** Draft
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
- optional trace assertion helper
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`

## Checks

- focused new tests in approved tag mode
- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestMTNetWaiterWakeupLatency' -v --timeout 90s`
- `git diff --check`

## Done

- Tests identify the wake-fd and shutdown behavior Task 11 must satisfy.
- Timeout-sensitive failures are not hidden under accepted backend-test debt.
