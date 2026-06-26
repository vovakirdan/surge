# Epic 2 Runtime V2 CI Test Contract

Status: Task 03 contract. This file defines the gate shape only. Task 12 owns
the `Makefile` target and GitHub Actions wiring.

## Goal

Epic 2 needs a small Runtime V2 gate that runs timeout-sensitive liveness tests
deliberately, without requiring the accepted broad VM/backend debt to pass.

## Required Rules

- Use exact Go test names or anchored exact-name regexes.
- Run the gate with `SURGE_BACKEND=llvm` and `SURGE_SKIP_TIMEOUT_TESTS=0`.
- Install and preflight `clang` and `ar` before treating the gate as green.
- Keep the default `make check` path unchanged unless a later task explicitly
  changes it.
- Do not make the broad accepted-debt command a required green gate:

```bash
go test ./internal/vm -run 'MT|Async|Net|LLVM'
```

That command may be used as a diagnostic only. A failure there remains accepted
backend-test debt until the later test/backend matrix epic fixes or replaces it.

## Proposed CI Seed

Task 12 should add a `runtime-v2-check` target equivalent to this command:

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
  go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
```

Before running it, the target or CI job should fail fast if either tool is
missing:

```bash
command -v clang
command -v ar
```

The GitHub Actions job should install `clang`, `llvm`, and `lld`, set
`SURGE_MT_TIMEOUT_SCALE=3`, then run `make runtime-v2-check`. The job should be
separate from the existing Go test matrix so the normal skipped-timeout path and
the Runtime V2 liveness gate stay easy to distinguish.

## Candidate Stable Exact Tests

| Test | Source | Surface | Why it belongs in the seed |
| --- | --- | --- | --- |
| `TestMTWakeupsAndCancellation` | `internal/vm/mt_executor_test.go` | Wakeups, join, cancellation | Uses the process timeout wrapper and checks common cancellation wakeups without the broad stress matrix. |
| `TestMTChannelParkUnpark` | `internal/vm/mt_executor_test.go` | Direct async channel wakeups | Asserts direct async channel trace counters and no sync-helper compensation on the hot path. |
| `TestMTBlockingChannelHelpersAllowTimersToAdvance` | `internal/vm/mt_executor_test.go` | Sync channel fallback plus timer progress | Proves a timer can wake work while a sync channel helper is blocking a task. |
| `TestMTSeededScheduler` | `internal/vm/mt_executor_test.go` | Scheduler trace determinism | Checks seeded scheduler trace mode, seed, event count, and hash repeatability. |

These tests still depend on the native LLVM path. Local or CI output must show
the tests ran, not only that they skipped due to missing backend/toolchain.

## Local-Only Until Re-Proven

| Probe or test | Keep local because | Future owner |
| --- | --- | --- |
| `TestMTNetWaiterWakeupLatency` | Has real socket timing and latency assertions. It is strong net evidence but should be re-proven on CI before gating. | Net poll scratch tasks and Task 12 if stable. |
| `TestNativeNetSingleThreadBlockingChannelInAsyncServer` | Useful one-worker compatibility probe, but narrow and server-lifecycle-sensitive. | Net/channel compatibility tasks. |
| `TestMTCorrectnessChannels` | Broader fixture coverage; keep as task evidence until its current-checkout stability is recorded. | Channel/waiter tasks. |
| `TestMTCorrectnessWakeups` | Larger stress workload than the seed gate. | Scheduler and wakeup tasks. |
| `TestMTStructuredConcurrency` | Broader structured-concurrency smoke; add after task evidence proves stable runtime. | Cancellation and scope tasks. |
| `TestMTBlockingPool` | Blocking-pool coverage belongs with compatibility/offload movement. | Blocking compatibility tasks. |
| `TestMTBlockingChannelHelpersDoNotParkWorkers` | Strong trace assertion but heavier sync-helper workload than the seed. | Channel/blocking compatibility tasks. |
| `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` | Compensation-limit stress belongs in local evidence first. | Channel/blocking compatibility tasks. |
| `TestMTWorkStealing` | Asserts current Tier 1 stealing, which Runtime V2 treats as an implementation artifact unless later promoted. | Scheduler-shape tasks or later Tier 2 work. |

## How A Test Joins The Gate

A new test may join `runtime-v2-check` only after the owning task records:

- exact test name and exact command;
- local pass with `SURGE_BACKEND=llvm` and `SURGE_SKIP_TIMEOUT_TESTS=0`;
- process or test timeout that captures stdout/stderr on failure;
- no known nondeterministic hang in the current checkout;
- clear failure message or trace counter that identifies the broken contract;
- evidence entry linking the test to `LIVENESS_PROBES.md`;
- reason the test is a CI gate instead of local-only evidence.

## Task 03 Checks

Task 03 is docs-only. It should run `git diff --check`. The proposed liveness
command above was not run as part of this contract unless the task evidence says
otherwise. Do not report it as a fresh pass without a recorded command result.
