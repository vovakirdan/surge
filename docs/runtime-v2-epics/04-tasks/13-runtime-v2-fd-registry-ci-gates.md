# Task 13: Runtime V2 FD Registry CI Gates

**Status:** Complete
**Kind:** CI

## Goal

Add stable fd-registry liveness tests to the Runtime V2 CI gate.

## Scope

- Promote only stable, non-flaky fd-registry tests.
- Keep pending or timeout-sensitive proofs out of required CI until stabilized.
- Update `make runtime-v2-check` if needed.
- Update CI workflow commands and documentation.

## Files

- `Makefile`
- `.github/workflows/*`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`, if the stable probe list changes

`.github/workflows/ci.yml` was not edited: the Runtime V2 CI job already
installs the LLVM toolchain and runs `make runtime-v2-check`, so adding the
fd-registry target under that Makefile gate reaches CI without workflow churn.

## Promoted CI Command

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2FDRegistry(RepeatedReadinessSingleFD|ReadWriteInterestSharesFDRow|DuplicateReadWaitersBothComplete|ClosedFDFailsFast|StaticShape|StaticBoundary|GenerationStaleSnapshotProof|CloseWakePollNotificationProof|ShutdownDrainStaticContract|ShutdownDrainBehavior)$' -count=1 -parallel=1 -p=1 -v --timeout 180s
```

`make runtime-v2-fd-registry-check` runs the same command through `$(GO)`.

Included tests:

- `RepeatedReadinessSingleFD`, `ReadWriteInterestSharesFDRow`,
  `DuplicateReadWaitersBothComplete`, and `ClosedFDFailsFast`: stable behavior
  contract tests that proved the registry migration surface before runtime
  changes.
- `StaticShape`, `StaticBoundary`, `GenerationStaleSnapshotProof`,
  `CloseWakePollNotificationProof`, `ShutdownDrainStaticContract`, and
  `ShutdownDrainBehavior`: deterministic C compile/run checks with no live
  trace timing dependency.

Excluded from CI for now:

- `WakeFDObservedForInterestAddedDuringPoll` and
  `CancelledInterestWakesPoller`: useful local probes, but they depend on live
  `SIGUSR1` trace timing and sleeps.
- `CloseWakesParkedAcceptWaiter` and `CloseWakesParkedReadWaiter`: useful
  local close proofs, but they use short in-language timeout windows.
- `CancelledDuplicateReadWaiterPreservesLiveAndReregister` and
  `CancelledReadInterestPreservesWriteInterest`: useful local lifecycle
  proofs, but they include cancellation timing and a heavier payload path.

## Checks

- `make runtime-v2-check`
- `make check`
- exact promoted Go test command
- `git diff --check`

## Done

- CI covers stable fd-registry liveness.
- Pending tests remain clearly marked and excluded from default gates.
- Documentation names the exact stable command.
