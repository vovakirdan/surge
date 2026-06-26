# Runtime V2 Working Notes

This is the live handoff log for Runtime V2 work. Keep it current during each
task, then move durable decisions into the owning epic document before closeout.

## Current State

- Runtime V2 target architecture lives in `docs/RUNTIME_V2.md`.
- Epic documents live in `docs/runtime-v2-epics/`.
- Epic 1 is complete. Its main document is
  `01-contract-rules-harness.md`.
- Task breakdown and status live in `01-contract-rules-harness-tasks.md`.
- Global working rules live in `RULES.md`.
- Tasks 1-5 were committed as `b865472a`:
  `docs(runtime): add Runtime V2 epic planning baseline`.
- Tasks 6-7 were committed as `8ae616a1`:
  `docs(runtime): define Runtime V2 liveness gates`.
- Task 8 closeout is complete.
- Final closeout checks already passed: `git diff --check`, `make check`,
  Sentrux repository scan, and Sentrux runtime scan. Both Sentrux `check_rules`
  calls still report missing rules files, which remains recorded baseline debt.
- Epic 2 is drafted in `02-n1-runtime-shard-structure.md`. It is scoped to
  `N=1` `rt_runtime`/`rt_shard` structure only; owner-local waiters, persistent
  fd registry, `N>1`, crossing syntax, and the VM/native/LLVM test-matrix
  rewrite are later epics.
- Epic 2 task files live in `02-tasks/`. Runtime-code tasks are paired with
  test-writing tasks where meaningful tests can be written, and stable Runtime
  V2 liveness tests must be added to CI before Epic 2 closes.
- Epic 2 task evidence is recorded in `02-evidence.md`.
- Subagents now use a plan gate: they must return a plan for approval before
  implementation, test-writing, or review work starts. If no real plan mode is
  available, use a no-edit plan-only prompt and approve the plan explicitly.
- Epic 2 drafting checks passed: `git diff --check`, stale phase/epic wording
  grep, Sentrux repository scan, and Sentrux runtime scan. Sentrux rules are
  still missing at both scan roots and must not be reported as rule compliance.
- Epic 2 Task 1 kickoff evidence is recorded in `02-evidence.md`. It captured
  baseline commit `e7d9563d5c78a90409e4d6a92bd47d49b30ae830`, clean starting
  status on `codex/runtime-net-scheduler-refactor`, accepted VM/backend-test
  debt, root/runtime Sentrux scans, and the missing-rules deferral.
- Epic 2 Task 2 field ownership map is recorded in
  `02-field-ownership-map.md`. It classifies every current `rt_executor` field
  before runtime field movement and names the first code-task field boundary.
- Epic 2 Task 3 CI/test contract is recorded in `02-ci-test-contract.md`. It
  defines the future exact-name Runtime V2 gate and keeps the broad focused
  VM/backend debt out of required green gates.
- Epic 2 Task 4 skeleton-test proof is recorded in
  `internal/vm/runtime_v2_skeleton_static_test.go`. It uses the
  `runtime_v2_pending` build tag and intentionally fails before Task 5 because
  `rt_runtime`, `rt_shard`, the `N=1` count macro, and skeleton accessors do not
  exist yet. The check is local-only until Task 12.
- Epic 2 Task 7 scheduler-shape migration evidence is recorded in
  `02-evidence.md`. Scheduler container fields now live under
  `rt_shard.scheduler`. Current scheduler trace proof uses
  `TestMTWorkStealing` and `TestMTSeededScheduler`. `TestMTSeededScheduler`
  remains in the future CI seed; `TestMTWorkStealing` stays
  local-only/current-runtime evidence because Tier 1 stealing is not a Runtime
  V2 hot-path contract.

## Epic 1 Artifacts

- `RULES.md`: global Runtime V2 development rules.
- `SENTRUX_POLICY.md`: Sentrux scan/rule policy and current missing-rules
  blocker.
- `EVIDENCE_TEMPLATE.md`: required evidence format for future tasks.
- `01-baseline-evidence.md`: current checkout checks, benchmark reports,
  counters, and blockers.
- `LIVENESS_PROBES.md`: liveness probes by changed runtime surface.
- `OPEN_DECISIONS_BEFORE_EPIC_2.md`: accepted debt, blockers, and deferrals
  before structural `N=1` work.
- `01-contract-rules-harness.md`: durable Epic 1 summary and Epic 2 start
  criteria.

## Durable Decisions

- Work proceeds epic-by-epic. Later epics stay as a short roadmap until earlier
  evidence shapes the next slice.
- Subagents must plan first and wait for approval before edits or review work.
- `MUST` rules block completion, except for documented proving spikes with
  hypothesis, allowed files/surfaces, non-final behavior, proof command,
  success/failure criteria, and rollback.
- Runtime code must stay explainable through ownership, wakeup, cancellation,
  lifetime/generation, backpressure, and trace/test evidence.
- Sentrux is mandatory. Root and scoped scans are required when a task mostly
  affects `runtime/`.
- Runtime V2 code limit is 500 lines for new or heavily rewritten code files.
- New V2 C APIs return explicit status codes for recoverable failures.
  `panic_msg` is not the primitive error-handling contract.
- Channel FIFO, task parking at suspension points, cooperative cancellation,
  structured join/failfast outcomes, and `@local spawn` sendability rules are
  source-visible contracts.
- Native global FIFO waiters, global inject, worker-local queues, Tier 1 work
  stealing, direct channel handoff placement, and sync-channel compensation are
  current implementation artifacts unless a later spec promotes one explicitly.
- VM/native parity means semantic output parity under native `threads=1`, not
  identical scheduler interleavings.

## Current Sentrux Baselines

- Repository scan: `/home/zov/projects/surge/surge`, `quality_signal=6210`,
  bottleneck `modularity`.
- Runtime scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5147`, bottleneck `redundancy`.
- `check_rules` reports no `.sentrux/rules.toml` for both scan paths. This is
  not a passing rule check. Runtime-code tasks after Epic 1 must add real rules
  or record an explicit temporary deferral without claiming rule compliance.

## Current Baseline Debt

- `go test ./internal/vm -run 'MT|Async|Net|LLVM'` fails in this checkout when
  timeout-sensitive tests are not skipped.
- Default `make check` passes because `SURGE_SKIP_TIMEOUT_TESTS=1` skips those
  timeout-sensitive VM/LLVM tests through `skipTimeoutTests`.
- The focused VM failure is accepted backend-test debt. A later test/backend
  epic will rewrite the VM/native/LLVM test matrix around stable Runtime V2
  contracts.
- Native net and channel benchmark reports in `build/benchmarks/` were
  regenerated with a temporary current-checkout compiler after a stale `./surge`
  binary was detected.

## Epic 2 Start Blockers

- Sentrux missing-rules status was explicitly deferred by Epic 2 Task 1 without
  claiming rule compliance. Runtime-code tasks still must add real rules or
  record a fresh temporary deferral for the active scan path.
- The first `N=1` task must name the exact behavior equivalence boundary from
  `01-contract-rules-harness.md` and `OPEN_DECISIONS_BEFORE_EPIC_2.md`.
- Epic 2 evidence must keep the focused VM debt named and must not attribute new
  runtime regressions to that debt without proof.

## Epic 2 Task 1 Kickoff Handoff

- Task: `02-tasks/01-kickoff-evidence.md`.
- Scope completed: documentation-only baseline evidence. No runtime,
  compiler, ABI, benchmark, CI, or Sentrux rule-file changes were made.
- Start state: baseline commit
  `e7d9563d5c78a90409e4d6a92bd47d49b30ae830`; branch
  `codex/runtime-net-scheduler-refactor`; `git status --short` was empty before
  the task.
- Sentrux root scan for `/home/zov/projects/surge/surge` returned
  `quality_signal=6210`, `files=4740`, `import_edges=1887`, and
  `lines=370800`; health bottleneck remains `modularity`; `check_rules` still
  reports missing `/home/zov/projects/surge/surge/.sentrux/rules.toml`.
- Sentrux runtime scan for `/home/zov/projects/surge/surge/runtime` returned
  `quality_signal=5147`, `files=32`, `import_edges=30`, and `lines=14883`;
  health bottleneck remains `redundancy`; `check_rules` still reports missing
  `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`.
- Missing Sentrux rules remain a blocker to claiming rule compliance. Task 1
  records a temporary deferral only; the first runtime-code task must either add
  real rules or record a fresh deferral for the active scan path.
- Accepted VM debt remains unchanged:
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` is not an Epic 2 kickoff
  gate and was not run in Task 1. New runtime failures must not be assigned to
  this debt without matching `01-baseline-evidence.md`.
- Approved checks for Task 1: `git diff --check` and `make check`; broad
  focused VM regex, benchmarks, and extra liveness probes are intentionally
  skipped.
- Task 1 checks passed: `git diff --check` produced empty output, and
  `make check` passed in 14.31s. `make check` ran
  `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 90s`, `golangci-lint`,
  `make c-check`, and `check_file_sizes.sh`.
- Next owner: Epic 2 Task 2, Field Ownership Map. It should classify current
  `rt_executor` state before any runtime field movement.

## Epic 2 Task 2 Field Ownership Handoff

- Task: `02-tasks/02-field-ownership-map.md`.
- Scope completed: documentation-only ownership classification. No runtime,
  compiler, ABI, benchmark, CI, Sentrux rule-file, staging, or commit changes
  were made.
- Output: `02-field-ownership-map.md` classifies every `rt_executor` field into
  runtime lifecycle/control plane, `N=1` shard-local hot state,
  compatibility/offload state, trace/debug-facing state, or later-epic state.
- Direct usage searches covered scheduler queues, waiter storage, net poll
  scratch, task/scope registries, lifecycle flags, channel compensation, and
  blocking pool state under `runtime/native`.
- Safe Epic 2 move candidates are runtime lifecycle shell, task/scope registry,
  scheduler queue shape, net poll scratch, and channel/blocking compatibility
  state. Each remains behavior-preserving and must use the matching
  `LIVENESS_PROBES.md` evidence when code moves fields.
- Deferred owners: local-waiter epic for owner-local waiter queues, local
  fd-registry epic for persistent readiness, multi-shard runtime epic for owner
  placement and distributed scope semantics, allocator/pools epic for heap
  counters and hot object pools, and later IO/backend work for backend choice.
- First code-task boundary: introduce the runtime/shard shell around `lock`,
  `ready_cv`, `io_cv`, `done_cv`, `workers`, `worker_ctxs`, `worker_count`,
  `initialized`, `io_started`, `shutdown`, `sched_mode`, and `sched_seed` only.
  Do not move waiters, fd readiness semantics, channel handoff semantics,
  blocking pool queue, or task/scope ownership unless the approved task plan
  expands the field group and evidence.
- File-size risk remains active for `rt_async_state.c`, `rt_net.c`, and
  `rt_async_channel.c`; later runtime-code tasks must avoid growing them or
  record a split/follow-up.
- Approved checks for Task 2: `git diff --check` and the map placeholder sanity
  grep. Runtime tests, benchmarks, liveness probes, and Sentrux scans are
  intentionally skipped for this docs-only task.

## Epic 2 Task 3 CI/Test Contract Handoff

- Task: `02-tasks/03-runtime-v2-ci-test-contract.md`.
- Scope completed: documentation-only CI/test contract. No `Makefile`, GitHub
  Actions, runtime, compiler, benchmark, Sentrux, staging, or commit changes
  were made.
- Output: `02-ci-test-contract.md` defines a future `runtime-v2-check` shape
  that runs exact named tests with `SURGE_BACKEND=llvm` and
  `SURGE_SKIP_TIMEOUT_TESTS=0`.
- Proposed seed tests:
  `TestMTWakeupsAndCancellation`, `TestMTChannelParkUnpark`,
  `TestMTBlockingChannelHelpersAllowTimersToAdvance`, and
  `TestMTSeededScheduler`.
- Proposed Task 12 command:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
    go test ./internal/vm \
      -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
      -v --timeout 120s
  ```

- Required future CI setup: install `clang`, `llvm`, and `lld`; preflight
  `clang` and `ar`; set `SURGE_MT_TIMEOUT_SCALE=3`; keep the Runtime V2 job
  separate from the default skipped-timeout Go matrix.
- Excluded required gate:
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'`. It remains accepted
  backend-test debt and may be used only as a diagnostic until the later
  test/backend matrix epic fixes or replaces it.
- Local-only until re-proven: net latency, one-worker net/channel
  compatibility, broader channel correctness, structured concurrency, blocking
  pool, heavier sync-helper compensation, compensation-limit stress, and
  current Tier 1 work-stealing probes.
- Candidate Runtime V2 seed and net commands were not run in Task 3. Do not
  report them as fresh passes without Task 12 or task-specific evidence.
- Approved checks for Task 3: `git diff --check` and direct
  `git diff --no-index --check` on the new contract file. Runtime tests,
  `make check`, `make c-check`, `make cppcheck`, benchmarks, and Sentrux scans
  are intentionally skipped for this docs-only task.

## Epic 2 Task 4 Runtime/Shard Skeleton Tests Handoff

- Task: `02-tasks/04-runtime-shard-skeleton-tests.md`.
- Scope completed: added a local-only pending static check for the Task 5
  runtime/shard skeleton. No runtime implementation, `Makefile`, CI workflow,
  benchmark, Sentrux, staging, or commit changes were made.
- New test: `TestRuntimeV2SkeletonStaticShape` in
  `internal/vm/runtime_v2_skeleton_static_test.go`.
- The test is hidden behind `//go:build runtime_v2_pending`; default test runs
  do not see it.
- The test compiles a C snippet with `clang -std=c11 -fsyntax-only` and requires
  `RT_RUNTIME_SHARD_COUNT == 1`, complete `rt_runtime` and `rt_shard` types,
  and the accessors `rt_executor_runtime`, `rt_runtime_shard0`, and
  `rt_runtime_shard_count`.
- Preflight tools exist: `command -v clang` returned `/usr/bin/clang`, and
  `command -v ar` returned `/usr/bin/ar`.
- Expected pre-Task-05 failure was recorded with:

  ```bash
  go test -tags runtime_v2_pending ./internal/vm \
    -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s
  ```

  It failed with missing `RT_RUNTIME_SHARD_COUNT`, undeclared `rt_runtime` and
  `rt_shard`, and undeclared skeleton accessors. This is the desired proof that
  Task 5 has not been implemented yet.
- Default safety check passed:
  `go test ./internal/vm -run '^$' --timeout 30s` returned
  `ok surge/internal/vm (cached) [no tests to run]`.
- `git diff --check` passed after the test and docs edits.
- Task 5 should make this pending check pass as part of skeleton implementation
  or record a blocker unrelated to Task 5 code. Task 12 owns deciding whether
  this exact tagged check or a non-pending successor joins `runtime-v2-check`.

## Epic 2 Task 5 Runtime/Shard Skeleton Handoff

- Task: `02-tasks/05-runtime-shard-skeleton.md`.
- Scope completed: added the internal `N=1` `rt_runtime`/`rt_shard` skeleton
  and accessors required by Task 4. No public ABI, `N>1`, waiter, fd registry,
  scheduler, net poll, channel/blocking, compiler, benchmark, CI, Sentrux rule,
  staging, or commit changes were made.
- Runtime shape: `RT_RUNTIME_SHARD_COUNT == 1`; `rt_runtime` owns
  `shards[RT_RUNTIME_SHARD_COUNT]`; `rt_shard` links to the runtime and current
  executor; `rt_executor` gained only `rt_runtime* runtime`.
- Required accessors now exist: `rt_executor_runtime`, `rt_runtime_shard0`, and
  `rt_runtime_shard_count`.
- New skeleton init uses `rt_runtime_status`. `exec_init_once()` still preserves
  the legacy `pthread_once`/`panic_msg` boundary because it cannot return an
  init status to callers.
- File-size result: `rt_async_internal.h` is `432` lines, new
  `rt_runtime.c` is `64` lines, and over-limit `rt_async_state.c` was reduced
  from `2391` to `2368` lines by moving cold default worker-count helpers.
- Checks passed:

  ```bash
  git diff --check
  command -v clang
  command -v ar
  go test -tags runtime_v2_pending ./internal/vm \
    -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s
  make c-check
  make cppcheck
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
  make check
  ```

- One local failure happened and was fixed inside Task 5: the first
  `make c-check` run showed `rt_async_state.c` still needed `<unistd.h>` for
  existing trace `write()` calls after CPU-count detection moved.
- Main-agent Sentrux runtime `session_end` passed against the pre-task baseline:
  `5147 -> 5144`, delta `-2`, summary `Quality stable or improved`, and no
  violations. A worker-context `session_end` could not reuse that baseline.
- Post-change root Sentrux: `/home/zov/projects/surge/surge`,
  `quality_signal=6209`, bottleneck `modularity`, rules file missing.
- Post-change runtime Sentrux: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5144`, bottleneck `redundancy`, rules file missing.
- Missing Sentrux rules remain a blocker to claiming rule compliance, not a
  blocker to this narrow skeleton implementation.

## Epic 2 Task 6 Scheduler Shape Tests Handoff

- Task: `02-tasks/06-scheduler-shape-tests.md`.
- Scope completed: selected and ran existing scheduler and CI-shaped liveness
  proofs before scheduler field movement. No runtime C, Go test, `Makefile`,
  GitHub Actions, STATS, benchmark, task-doc, Sentrux, staging, or commit
  changes were made.
- Scheduler trace proof command passed:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WorkStealing|SeededScheduler)$' -v --timeout 90s
  ```

  Both `TestMTWorkStealing` and `TestMTSeededScheduler` ran and passed.
- CI-shaped Runtime V2 seed command passed:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
  ```

  All four exact tests ran and passed.
- Tool preflight passed: `command -v clang` returned `/usr/bin/clang`, and
  `command -v ar` returned `/usr/bin/ar`.
- CI ownership: `TestMTSeededScheduler` remains in the future Runtime V2 seed.
  `TestMTWorkStealing` remains local-only/current-runtime evidence and must not
  join the seed unless a later Tier 2 CPU-pool decision promotes stealing.
- Parked-with-work remains a missing invariant. Task 6 did not add a weak
  nondeterministic test.
- Task 7 may proceed only if it preserves current wake elision, worker sleep
  rules, and shard park state. If Task 7 needs to change any of those, it must
  stop and add a real parked-with-work invariant first.
- `git diff --check` passed after the documentation updates.
- Verification note: do not run overlapping `go test ./internal/vm` commands
  that include the same MT test names. The test artifact directory is keyed by
  test name under `target/debug/.tests/`, so parallel runs can collide while
  writing artifacts and create a false failure unrelated to runtime behavior.

## Epic 2 Task 7 Scheduler Shape Migration Handoff

- Task: `02-tasks/07-scheduler-shape-migration.md`.
- Scope completed: moved only scheduler container fields behind the existing
  `N=1` `rt_shard.scheduler`: `inject`, `local_queues`, `worker_ctxs`,
  `worker_count`, `running_count`, `sched_mode`, and `sched_seed`.
- Preserved executor/global lifecycle state on `rt_executor`: `workers`,
  `ready_cv`, `io_cv`, `done_cv`, `lock`, `shutdown`, `net_polling`,
  `initialized`, `io_started`, `channel_blocked_workers`,
  `compensation_count`, `compensation_high_water`, and blocking-pool fields.
- No `runtime/native/rt.h`, `Makefile`, CI, Go test, benchmark script,
  Sentrux rule, net/channel/waiter/task ownership semantic, public ABI,
  staging, or commit changes were made.
- Direct moved-field audit passed with no matches:

  ```bash
  rg -n -- 'ex->(inject|local_queues|worker_ctxs|worker_count|running_count|sched_mode|sched_seed)\b|exec_state\.(sched_seed|sched_mode)' runtime/native
  ```

  `rg` returned exit `1`, the expected no-match status.
- Tool preflight passed: `command -v clang` returned `/usr/bin/clang`, and
  `command -v ar` returned `/usr/bin/ar`.
- Final checks passed:

  ```bash
  go test -tags runtime_v2_pending ./internal/vm \
    -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WorkStealing|SeededScheduler)$' -v --timeout 90s
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
  make c-check
  make cppcheck
  make check
  ```

- A first `make cppcheck` run found const-pointer style warnings in
  `rt_async_state.c`; the declarations were narrowed and the final standalone
  `make cppcheck` passed.
- Sentrux post-change root scan: `/home/zov/projects/surge/surge`,
  `quality_signal=6207`, bottleneck `modularity`, rules file missing.
- Sentrux post-change runtime scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5168`, bottleneck `redundancy`, rules file missing.
  Supplied runtime baseline was `5125`, so the scoped signal increased by `43`.
- Main-agent Sentrux runtime `session_end` passed against the pre-task baseline:
  `5125 -> 5168`, delta `+43`, summary `Quality stable or improved`, and no
  violations. Missing rules remain a blocker to claiming rule compliance, not a
  blocker to this narrow shape migration.
- Parked-with-work remains a missing invariant. Task 7 did not change wake
  elision, worker sleep rules, or shard park state, so it did not cross the
  Task 6 boundary.
- Next task: Task 8 must record net poll scratch before-evidence. Run
  `TestMTNetWaiterWakeupLatency` with `SURGE_SKIP_TIMEOUT_TESTS=0`, run the
  native net benchmark with a current-checkout `SURGE` binary and an outer
  timeout, and keep persistent fd registry behavior out of scope. Task 9 should
  not start until Task 8 evidence exists.

## Liveness Requirements

- Runtime-code tasks cannot close with "watch for hangs" as evidence.
- Use `LIVENESS_PROBES.md` to choose probes by changed surface.
- Missing probes that block owning future work include parked-with-work
  invariant, owner-local waiter cleanup tests, fd-registry lifecycle test,
  channel close/cancellation race matrix, native shutdown liveness, cross-shard
  wake-fd elision, cross-shard cancellation generation, and per-probe timeout
  wrappers for channel benchmarks.

## Known Large Files

These files already exceed the 500-line Runtime V2 limit and need care when
touched:

- `runtime/native/rt_async_state.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_async_channel.c`
- `runtime/native/rt_async_task.c`
- `internal/vm/mt_executor_test.go`
- `internal/vm/mt_correctness_test.go`

Touching an over-limit file must record whether the task reduces it, keeps it
flat, or creates a follow-up split task.

## Dead Ends And Cautions

- Do not tune scheduler behavior by machine-specific constants as durable
  design.
- Do not let proving-spike code become architecture without rewriting it into
  rule-compliant form.
- Do not use `TestMTWorkStealing` as a future Tier 1 contract without deciding
  whether the assertion moves to explicit Tier 2 work.
- Do not treat missing Sentrux rules as a passing rules gate.
- Do not treat default `make check` as proof that timeout-sensitive VM/LLVM
  liveness and parity tests pass.
- Do not spend Epic 2 capacity rewriting the semi-broken backend test matrix;
  that belongs to a later dedicated test/backend epic.
