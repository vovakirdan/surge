# Task 9: Close, Cancel, And Re-register Migration

**Status:** Complete
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

## Completion Evidence

Completed on 2026-07-02.

- fd rows now carry monotonic registry-owned generations via
  `next_generation`; new rows do not reset generation after remove/recreate,
  and generation exhaustion returns an explicit `rt_runtime_status`.
- close marks the registry row closed under `ex->lock`, performs raw
  `close(fd)` outside the executor lock, then wakes only the lifecycle snapshot
  keys present at close time and signals the net poll/`io_cv` sleepers.
- poll snapshots skip closed rows and carry fd generation plus exact
  accept/read/write interests. Poll-error and readiness completion fan-out is
  guarded by fd, generation, open state, and current interest kind.
- cancellation remains owned by the existing `remove_waiter` detach path; the
  completion guard makes stale in-flight poll snapshots no-op after interests
  disappear.
- `POLLNVAL` is treated as ready/error for immediate readiness probes and poll
  completion, so a waiter resumed after raw close observes the closed fd instead
  of re-parking.
- Deterministic fd-reuse proof was added to
  `TestRuntimeV2FDRegistryGenerationStaleSnapshotProof`: fd `42` old snapshot
  is stale after close+detach+recreate while the recreated snapshot is current.
- Boundary: Task 9 protects stale poll snapshots and registry-routed waiter
  completions. Copied public net handles are still raw-fd views and remain
  tracked as RV2-DEBT-010.

Checks passed:

- `gofmt -w internal/vm/runtime_v2_fd_registry_static_test.go`
- `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2FDRegistry(StaticShape|GenerationStaleSnapshotProof)$' -count=1 -v --timeout 60s`
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2FDRegistry(CancelledDuplicateReadWaiterPreservesLiveAndReregister|CancelledReadInterestPreservesWriteInterest|CloseWakesParkedAcceptWaiter|CloseWakesParkedReadWaiter)$' -count=1 -v --timeout 90s`
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2NetWaiterTraceContract$' -count=1 -v --timeout 90s`
- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -count=1 -v --timeout 90s`
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestNativeNetSingleThreadBlockingChannelInAsyncServer$' -count=1 -v --timeout 90s`
- `make c-check`
- `make cppcheck`
- `make runtime-v2-check` (first run hit `TestMTChannelParkUnpark` timeout;
  isolated rerun and full rerun passed)
- `make check`
- `git diff --check`
