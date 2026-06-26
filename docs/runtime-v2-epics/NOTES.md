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
