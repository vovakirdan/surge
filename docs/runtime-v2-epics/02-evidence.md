# Epic 2 Evidence

This file records task-by-task evidence for
`02-n1-runtime-shard-structure.md`.

Do not use this file to hide known debt. The broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` is accepted backend-test debt
until the later test/backend matrix epic fixes or replaces it. Epic 2 evidence
must separate that debt from new runtime regressions.

## Task Evidence Index

| Task | Evidence status | Notes |
| --- | --- | --- |
| 1. Kickoff Evidence | Complete | Baseline, accepted VM debt, and Sentrux missing-rules deferral recorded. |
| 2. Field Ownership Map | Complete | Ownership map linked; movable and deferred field groups recorded. |
| 3. Runtime V2 Test And CI Contract | Complete | CI contract created; exact seed tests and excluded accepted-debt command recorded. |
| 4. Runtime/Shard Skeleton Tests | Complete | Added local-only pending static shape check; pre-Task-05 failure recorded. |
| 5. Runtime/Shard Skeleton | Complete | Internal `N=1` runtime/shard skeleton added; checks and Sentrux deltas recorded. |
| 6. Scheduler Shape Tests | Pending | Record selected scheduler liveness checks. |
| 7. Scheduler Shape Migration | Pending | Record scheduler migration checks and traces. |
| 8. Net Poll Scratch Tests | Pending | Record net wake and benchmark baseline. |
| 9. Net Poll Scratch Migration | Pending | Record net migration checks and benchmark rows. |
| 10. Channel/Blocking Compatibility Tests | Pending | Record channel and fallback checks. |
| 11. Channel/Blocking Compatibility Migration | Pending | Record migration checks and trace rows. |
| 12. CI Runtime V2 Gates | Pending | Record new CI target/job and local result. |
| 13. Accessor Cleanup And Static Gates | Pending | Record static checks and quality deltas. |
| 14. Epic Closeout | Pending | Record final gates and handoff to Epic 3. |

## Task 1: Kickoff Evidence

### Task Identity And Scope

- Task: Epic 2 Task 1, Kickoff Evidence.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: record the current checkout, accepted VM/backend-test debt, Sentrux
  root/runtime state, and the first docs-only gate before runtime code changes.
- Out of scope: runtime implementation edits, Sentrux rule-file creation,
  benchmarks, liveness probes, broad focused VM regex runs, staging, and commit.
- Proving spike: `no`.

### Baseline Commit/Status

- Baseline commit: `e7d9563d5c78a90409e4d6a92bd47d49b30ae830`.
- Branch/worktree: `codex/runtime-net-scheduler-refactor`; clean before this
  task.
- Status before: `git status --short` produced empty output.
- Status after closeout: these docs are committed by the main-agent Task 1
  closeout commit, and `git status --short` is checked immediately after that
  commit.
- Dirty or untracked files not touched: none observed before this task.
- Local environment blockers: none observed. Sentrux rules are missing at both
  scan roots; this is recorded as a rule-compliance blocker/deferral, not as a
  passing rule gate.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/02-evidence.md` | Added this Task 1 evidence section and marked the index row complete. | Keep Epic 2 evidence current before runtime code changes. | Documentation only; runtime file-size limits do not apply. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 1 kickoff handoff. | Make the next task startable without chat context. | Documentation only. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Updated status wording to point at kickoff evidence. | Reflect that the kickoff evidence step is recorded. | Documentation only. |

### Contracts Touched

| Contract or behavior | Source | Preserved, changed, or N/A | Evidence |
| --- | --- | --- | --- |
| Runtime behavior | Epic 2 scope and preserved behavior boundary | N/A | Docs-only task; no runtime files changed. |
| Public native ABI | Epic 2 acceptance gates | N/A | Docs-only task; no native ABI files changed. |
| Focused VM debt handling | `01-baseline-evidence.md`, `OPEN_DECISIONS_BEFORE_EPIC_2.md` | Preserved | Broad focused VM regex was not run and remains accepted backend-test debt. |
| Sentrux rule compliance | `SENTRUX_POLICY.md` | Preserved as blocker/deferral | `check_rules` still reports missing rule files at both active scan paths. |

### Accepted VM Debt

`go test ./internal/vm -run 'MT|Async|Net|LLVM'` remains accepted
backend-test debt for Epic 2 kickoff. It fails in the current checkout when
timeout-sensitive tests are not skipped; Epic 2 may start without fixing it.
Do not attribute new runtime failures to this debt unless they match the
recorded classes in `01-baseline-evidence.md`. A later test/backend matrix epic
owns the rewrite.

### Sentrux Root/Scoped Signals

| Scan | Path | When | quality_signal | Root cause or bottleneck | Rules/session result |
| --- | --- | --- | --- | --- | --- |
| Repository | `/home/zov/projects/surge/surge` | Before docs edit | `6210` | bottleneck `modularity`; root causes: acyclicity `10000`, depth `6667`, equality `4696`, modularity `3435`, redundancy `8588`; cross-module edges `1820`; files `4740`; import edges `1887`; lines `370800` | `check_rules`: no rules file at `/home/zov/projects/surge/surge/.sentrux/rules.toml`; blocker/temporary deferral, not compliance. |
| Scoped | `/home/zov/projects/surge/surge/runtime` | Before docs edit | `5147` | bottleneck `redundancy`; root causes: acyclicity `10000`, depth `8889`, equality `4735`, modularity `3333`, redundancy `2574`; cross-module edges `0`; files `32`; import edges `30`; lines `14883` | `check_rules`: no rules file at `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`; blocker/temporary deferral, not compliance. |
| Scoped | `/home/zov/projects/surge/surge/runtime` | After | N/A | N/A | N/A; this docs-only kickoff records start evidence only and does not call `session_start`/`session_end`. First runtime-code task must use a scoped Sentrux session. |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence path or note |
| --- | --- | --- | --- | --- |
| `git rev-parse HEAD` | current commit | `e7d9563d5c78a90409e4d6a92bd47d49b30ae830` | `0` | baseline commit |
| `git rev-parse --abbrev-ref HEAD` | current branch | `codex/runtime-net-scheduler-refactor` | `0` | baseline branch |
| `git status --short` | clean before edits | empty output | `0` | baseline status |
| `mcp__sentrux.scan` root | scan root path | `quality_signal=6210`; files `4740`; import edges `1887`; lines `370800` | pass | active path `/home/zov/projects/surge/surge` |
| `mcp__sentrux.health` root | health for root active path | bottleneck `modularity`; root-cause scores recorded above | pass | active path `/home/zov/projects/surge/surge` |
| `mcp__sentrux.check_rules` root | report rule status | missing `/home/zov/projects/surge/surge/.sentrux/rules.toml` | blocker/deferral | missing rules are not compliance |
| `mcp__sentrux.scan` runtime | scan runtime path | `quality_signal=5147`; files `32`; import edges `30`; lines `14883` | pass | active path `/home/zov/projects/surge/surge/runtime` |
| `mcp__sentrux.health` runtime | health for runtime active path | bottleneck `redundancy`; root-cause scores recorded above | pass | active path `/home/zov/projects/surge/surge/runtime` |
| `mcp__sentrux.check_rules` runtime | report rule status | missing `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml` | blocker/deferral | missing rules are not compliance |
| `git diff --check` | no whitespace errors | empty output | `0` | docs-only whitespace gate |
| `make check` | pass | passed in 14.31s; ran `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 90s`, `golangci-lint`, `make c-check`, and `check_file_sizes.sh` | `0` | approved Task 1 gate |

Skipped by explicit Task 1 scope: broad focused VM regex,
standalone `make c-check`, `make cppcheck`, benchmarks, and extra long
liveness probes. `make check` still ran its internal `make c-check` step.

### Benchmarks And Generated Reports

N/A. This task changes documentation only and does not touch a
performance-sensitive runtime path.

### Trace Counters/Liveness Proof

N/A. This task changes documentation only and does not touch scheduler, channel,
net, timer, cancellation, or shutdown behavior.

### Known Regressions

None known. Runtime code was not changed.

### Dead Ends / Paths Not To Retry

- Do not treat missing Sentrux rule files as passing rule checks.
- Do not use the broad focused VM regex as an Epic 2 green gate until the later
  test/backend matrix epic replaces or fixes that debt.

### Rollback/Recovery Notes

- Files or changes to revert: this Task 1 section, the Task 1 index update, the
  Task 1 notes entry, and the Epic 2 status wording.
- Generated artifacts to remove: none.
- Runtime processes, sockets, or temporary state to clean up: none.
- Recovery command or owner: revert only the three docs touched by this task.

### Follow-Ups And Blockers

| Item | Blocks completion? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Missing root `.sentrux/rules.toml` | No for this docs-only kickoff; yes for claiming rule compliance. | First runtime-code Epic 2 task or a dedicated Sentrux rules task. | Root `check_rules` cannot pass without a real rules file or an explicit deferral. |
| Missing runtime `.sentrux/rules.toml` | No for this docs-only kickoff; yes for claiming runtime rule compliance. | First runtime-code Epic 2 task or a dedicated Sentrux rules task. | Runtime `check_rules` cannot pass without a real rules file or an explicit deferral. |
| Focused VM regex debt | No for Epic 2 kickoff. | Later test/backend matrix epic. | Existing timeout-sensitive VM/LLVM debt remains accepted baseline debt. |
| Field ownership map | Yes for starting structural runtime movement. | Epic 2 Task 2, `02-tasks/02-field-ownership-map.md`. | The next task must classify current `rt_executor` fields before code movement. |

### Notes Consolidation Checklist

- [x] `NOTES.md` has the start context and intended proof.
- [x] `NOTES.md` has what changed, what was checked, and what was skipped.
- [x] Durable decisions remain in the owning Epic 1 and Epic 2 documents.
- [x] Skipped checks record the exact reason and blocker status.
- [x] Dead ends are recorded so future tasks do not retry them.
- [x] Follow-ups and blockers have an owner, target document, or next task.

## Task 2: Field Ownership Map

### Task Identity And Scope

- Task: Epic 2 Task 2, Field Ownership Map.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: inspect current `rt_executor` state, classify every field group before
  field movement, name safe Epic 2 move groups, name deferred later-epic owners,
  and link the map from the epic notes and evidence.
- Out of scope: runtime implementation edits, tests, benchmarks, liveness
  probes, Sentrux scans, Sentrux rule-file creation, staging, and commit.
- Proving spike: `no`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/02-field-ownership-map.md` | Created the ownership map. | Classify every current `rt_executor` field before code movement. | Documentation only. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Linked the map and updated Task 2 status wording. | Make the map discoverable from the Epic 2 overview. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 2 handoff. | Preserve context for the first code task. | Documentation only. |
| `docs/runtime-v2-epics/02-evidence.md` | Marked Task 2 complete and added this evidence section. | Keep Epic 2 evidence current. | Documentation only. |

### Runtime Files Inspected

| Path | Reason |
| --- | --- |
| `runtime/native/rt_async_internal.h` | Source of `rt_executor` fields and current executor invariants. |
| `runtime/native/rt_async_state.c` | Scheduler, waiter, timer, trace, lifecycle, and I/O loop field users. |
| `runtime/native/rt_net.c` | Net waiter accounting and net poll scratch field users. |
| `runtime/native/rt_async_channel.c` | Direct async channel and sync helper compatibility users. |
| `runtime/native/rt_async_task.c` | Task id/table, join, select, timer, and worker-count users found by `rg`. |
| `runtime/native/rt_async_poll.c` | Sleep, timeout, and run-until-done users found by `rg`. |
| `runtime/native/rt_async_scope.c` | Scope id/table and structured-concurrency users found by `rg`. |
| `runtime/native/rt_async_blocking.c` | Blocking pool queue, counters, and completion wake users found by `rg`. |

### Field Classification Summary

| Category | Field groups |
| --- | --- |
| Runtime lifecycle/control plane | `lock`, `ready_cv`, `io_cv`, `done_cv`, `workers`, `worker_ctxs`, `worker_count`, `initialized`, `io_started`, `shutdown`, `sched_mode`, `sched_seed` when used for startup and runtime configuration. |
| `N=1` shard-local hot state | Task id/table, scope id/table, `now_ms`, scheduler queues/counters, and net poll scratch buffers. |
| Compatibility/offload state | Sync-channel fallback counters, compensation counters, blocking pool lifecycle, blocking queue, and blocking pool counters. |
| Trace/debug-facing state | Blocking counters read by trace dumps, scheduler source fields, queue lengths, waiter counts, compensation counts, and net poll scratch counters when reported by existing traces. |
| Later-epic state | Owner-local waiters, persistent fd registry, multi-shard owner placement, distributed cancellation/scopes, allocator counters/pools, and IO backend choice. |

Every current `rt_executor` field is covered by the map in
`02-field-ownership-map.md`.

### First Code-Task Boundary

The first runtime-code task should introduce only the runtime/shard shell around
these lifecycle fields:

```text
lock, ready_cv, io_cv, done_cv, workers, worker_ctxs, worker_count,
initialized, io_started, shutdown, sched_mode, sched_seed
```

It should not move waiters, fd readiness semantics, channel handoff semantics,
blocking pool queue, or task/scope ownership unless its approved task plan
expands the field group and names matching evidence.

### Usage Search Commands

| Command | Purpose | Result |
| --- | --- | --- |
| `rg -n -- '->(inject|local_queues|worker_ctxs|worker_count|running_count|ready_cv|sched_mode|sched_seed)\b' runtime/native` | Scheduler and ready queue usage. | Found scheduler uses in `rt_async_state.c` and `rt_async_task.c`. |
| `rg -n -- '->(waiters|waiters_len|waiters_cap|net_waiters_len)\b|\b(add_waiter|remove_waiter|pop_waiter|prepare_park|wake_key_all)\b' runtime/native` | Shared waiter usage. | Found channel, task, poll, scope, blocking, net, and state users. |
| `rg -n -- '->(net_poll_fds|net_poll_fds_cap|net_poll_pfds|net_poll_pfds_cap|net_polling|io_cv)\b|\b(poll_net_waiters|complete_net_waiters|ensure_net_poll_)\b' runtime/native` | Net poll scratch and I/O wait usage. | Found net scratch users in `rt_net.c` and poll ownership/I/O cv users in `rt_async_state.c`. |
| `rg -n -- '->(next_id|next_scope_id|now_ms|tasks|tasks_cap|scopes|scopes_cap)\b|\b(tick_virtual|advance_time_to_next_timer|get_task|get_scope)\b' runtime/native` | Task, scope, and timer usage. | Found task/scope users across task, scope, poll, channel, blocking, net, and state modules. |
| `rg -n -- '->(channel_blocked_workers|compensation_count|compensation_high_water|blocking_.*)\b|\b(rt_blocking_|maybe_start_compensation|rt_wait_current_worker_wakeup)\b' runtime/native` | Channel fallback, compensation, and blocking pool usage. | Found sync-channel compatibility users in `rt_async_state.c`/`rt_async_channel.c` and blocking users in `rt_async_blocking.c`. |
| `rg -n -- '->(lock|done_cv|workers|initialized|io_started|shutdown)\b|\b(ensure_exec|exec_init_once|rt_start_workers|run_until_done|worker_main|io_thread_main)\b' runtime/native` | Lifecycle and lock usage. | Found runtime init, worker startup, condition-variable waits, shutdown checks, and ABI entry users. |
| `rg --no-filename -o -- '->(next_id|next_scope_id|now_ms|tasks|tasks_cap|inject|local_queues|scopes|scopes_cap|waiters|waiters_len|waiters_cap|net_waiters_len|net_poll_fds|net_poll_fds_cap|net_poll_pfds|net_poll_pfds_cap|lock|ready_cv|io_cv|done_cv|workers|worker_ctxs|worker_count|running_count|channel_blocked_workers|net_polling|compensation_count|compensation_high_water|sched_mode|initialized|io_started|shutdown|sched_seed|blocking_lock|blocking_cv|blocking_workers|blocking_count|blocking_started|blocking_shutdown|blocking_running|blocking_submitted|blocking_completed|blocking_cancel_requested|blocking_head|blocking_tail)\b|exec_state\.(blocking_submitted|blocking_running|blocking_completed|blocking_cancel_requested|initialized|sched_seed|sched_mode)' runtime/native | sort | uniq -c` | Compact usage count for every executor field. | Confirmed every executor field appears in direct usage or trace-facing `exec_state` reads. |

### File Size Risk Record

| File | Current lines | Status |
| --- | --- | --- |
| `runtime/native/rt_async_internal.h` | `404` | Under 500 lines. |
| `runtime/native/rt_async_state.c` | `2391` | Over limit; later code tasks must avoid growth or record a split/follow-up. |
| `runtime/native/rt_net.c` | `1039` | Over limit; net scratch work must stay narrow. |
| `runtime/native/rt_async_channel.c` | `549` | Over limit; channel/blocking compatibility work must stay narrow. |

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `git diff --check` | no whitespace errors | pass after docs edits | `0` | Docs-only whitespace gate. |
| `git diff --no-index --check /dev/null docs/runtime-v2-epics/02-field-ownership-map.md` | no whitespace errors in the new untracked map | pass after docs edits | `1` | `git diff --no-index` returns `1` because the files differ; no diagnostics means the check passed. |
| `rg -n 'TBD|TODO|unclassified|unknown' docs/runtime-v2-epics/02-field-ownership-map.md` | no placeholder matches | no matches after docs edits | `1` | `rg` returns `1` when no lines match; this is the expected sanity result. |

Skipped by explicit Task 2 scope: runtime tests, `make check`, `make c-check`,
`make cppcheck`, benchmarks, liveness probes, Sentrux scans, staging, and
commit.

### Map Risks

- Current shared waiter FIFO behavior is an implementation artifact. Future
  work must not promote global FIFO across waiter kinds into a Runtime V2
  contract.
- Current I/O assist can run ready work after net readiness. Future runtime
  code must separate net-woken work from unrelated general inject work before
  treating that path as architecture.
- `worker_count` remains a runtime setting in Epic 2, even though later V2
  shards may map more directly to owner threads.
- Trace-facing fields need equivalent trace names or explicit evidence notes
  when moved.

### Rollback/Recovery Notes

- Files or changes to revert: `02-field-ownership-map.md`, the Task 2 index and
  section in this file, the map link/status in `02-n1-runtime-shard-structure.md`,
  and the Task 2 handoff in `NOTES.md`.
- Generated artifacts to remove: none.
- Runtime processes, sockets, or temporary state to clean up: none.

### Follow-Ups And Blockers

| Item | Blocks next code task? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Runtime/shard skeleton tests | Yes. | Epic 2 Tasks 3-4. | Code movement needs a stable test/CI contract and skeleton checks first. |
| Runtime/shard skeleton implementation | Yes for field movement. | Epic 2 Task 5. | First code task should use the lifecycle-shell field boundary named above. |
| Owner-local waiters | No for skeleton; yes for waiter rewrite. | Local waiter epic. | Requires owner cleanup and stale-wake probes. |
| Persistent fd registry | No for net scratch; yes for fd registry semantics. | Local fd registry epic. | Requires readiness persistence and close/cancel lifecycle tests. |
| Missing Sentrux rules | No for this docs-only task; yes for claiming code-task rule compliance. | First runtime-code Epic 2 task or dedicated Sentrux rules task. | Task 1 deferral remains active but is not a passing rules gate. |

## Task 3: Runtime V2 Test And CI Contract

### Task Identity And Scope

- Task: Epic 2 Task 3, Runtime V2 Test And CI Contract.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: define the docs-only Runtime V2 CI gate contract, choose candidate
  exact seed tests, exclude broad accepted backend-test debt from required
  green gates, and update the epic handoff docs.
- Out of scope: `Makefile` edits, GitHub Actions edits, runtime implementation
  edits, test rewrites, benchmarks, Sentrux scans, staging, and commit.
- Proving spike: `no`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/02-ci-test-contract.md` | Created the CI/test contract. | Record the exact Runtime V2 gate shape before CI wiring. | Documentation only. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Linked the contract and clarified the Task 03/Task 12 boundary. | Keep the epic overview current. | Documentation only. |
| `docs/runtime-v2-epics/02-evidence.md` | Marked Task 3 complete and added this evidence section. | Keep Epic 2 evidence current. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 3 handoff. | Preserve context for Task 4 and Task 12. | Documentation only. |

### CI/Test Files Inspected

| Path | Reason |
| --- | --- |
| `.github/workflows/ci.yml` | Current CI uses `SURGE_SKIP_TIMEOUT_TESTS=1` in the Go matrix and installs LLVM only for the existing LLVM backend leg. |
| `Makefile` | Current `test` target defaults `SURGE_SKIP_TIMEOUT_TESTS ?= 1`; no `runtime-v2-check` target exists yet. |
| `internal/vm/test_helpers_test.go` | `skipTimeoutTests`, backend selection, and LLVM toolchain skip behavior define how the future gate must avoid false green skips. |
| `internal/vm/mt_executor_test.go` | Source for the candidate scheduler, channel, timer, and cancellation tests. |
| `internal/vm/mt_correctness_test.go` | Source for net and broader correctness probes kept local-only until re-proven. |
| `internal/vm/llvm_smoke_test.go` and related LLVM tests | Confirmed why broad `LLVM` regex coverage would pull in unrelated backend matrix debt. |
| `docs/runtime-v2-epics/LIVENESS_PROBES.md` | Source of existing usable probes and missing-probe ownership. |

### Contract Summary

- Required future gate: exact test names only, run with `SURGE_BACKEND=llvm`
  and `SURGE_SKIP_TIMEOUT_TESTS=0`.
- Proposed Task 12 target: `make runtime-v2-check`, backed by the anchored
  exact-name regex in `02-ci-test-contract.md`.
- Proposed seed tests:
  `TestMTWakeupsAndCancellation`, `TestMTChannelParkUnpark`,
  `TestMTBlockingChannelHelpersAllowTimersToAdvance`, and
  `TestMTSeededScheduler`.
- Required CI setup: install `clang`, `llvm`, and `lld`; preflight `clang` and
  `ar`; set `SURGE_MT_TIMEOUT_SCALE=3`; keep the Runtime V2 job separate from
  the default Go matrix.
- Excluded required gate:
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'`. It remains accepted
  backend-test debt and may be used only as a diagnostic until the later
  test/backend matrix epic replaces or fixes it.

### Proposed Local Commands

These commands are recorded for Task 12 or targeted task evidence. They were
not run in Task 03 and must not be reported as fresh passes from this task.

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
  go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s

SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
  go test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s

SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
  go test ./internal/vm \
    -run '^TestNativeNetSingleThreadBlockingChannelInAsyncServer$' \
    -v --timeout 90s
```

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `git diff --check` | no whitespace errors | pass after docs edits | `0` | Docs-only whitespace gate. |
| `git diff --no-index --check /dev/null docs/runtime-v2-epics/02-ci-test-contract.md` | no whitespace errors in the new untracked contract | pass after docs edits | `1` | `git diff --no-index` returns `1` because the files differ; no diagnostics means the check passed. |
| Candidate Runtime V2 seed command from `02-ci-test-contract.md` | proposed only | not run | N/A | Avoided claiming fresh liveness pass from a docs-only task. |

Skipped by explicit Task 3 scope: runtime tests, `make check`, `make c-check`,
`make cppcheck`, benchmarks, Sentrux scans, `Makefile` edits, CI workflow edits,
staging, and commit.

### Local-Only Probe Boundary

Task 03 keeps these probes out of the seed gate until their owning task records
current-checkout stability: net latency, one-worker net/channel compatibility,
broader channel correctness, structured concurrency, blocking pool, heavier
sync-helper compensation, compensation-limit stress, and current Tier 1 work
stealing.

### Rollback/Recovery Notes

- Files or changes to revert: `02-ci-test-contract.md`, the Task 3 index and
  section in this file, the contract link/status in
  `02-n1-runtime-shard-structure.md`, and the Task 3 handoff in `NOTES.md`.
- Generated artifacts to remove: none.
- Runtime processes, sockets, or temporary state to clean up: none.

### Follow-Ups And Blockers

| Item | Blocks next code task? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Runtime/shard skeleton tests | Yes. | Epic 2 Task 4. | Structural code needs skeleton checks before Task 5. |
| CI target and workflow wiring | No for Task 4; yes before Epic 2 closeout. | Epic 2 Task 12. | Task 03 defines the contract only. |
| Candidate seed command current pass | No for this docs-only task; yes before Task 12 closes. | Epic 2 Task 12. | The command is proposed, not freshly proven here. |
| Broad focused VM command debt | No for Epic 2. | Later test/backend matrix epic. | It remains excluded from required green gates. |

## Task 4: Runtime/Shard Skeleton Tests

### Task Identity And Scope

- Task: Epic 2 Task 4, Runtime/Shard Skeleton Tests.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: add a local-only pending static check that proves Task 5 must create
  the `N=1` `rt_runtime`/`rt_shard` skeleton before later field movement.
- Out of scope: runtime skeleton implementation, `Makefile` edits, GitHub
  Actions edits, public ABI changes, field movement, Sentrux scans, benchmarks,
  staging, and commit.
- Proving spike: `no`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `internal/vm/runtime_v2_skeleton_static_test.go` | Added a `runtime_v2_pending` build-tagged Go test that compiles a C shape probe with `clang -fsyntax-only`. | Provide a failing pre-Task-05 proof for the missing Runtime V2 skeleton. | New file is 61 lines. |
| `docs/runtime-v2-epics/02-ci-test-contract.md` | Added the pending skeleton check to the local-only list. | Keep CI ownership explicit: Task 12 decides when it joins `runtime-v2-check`. | Documentation only. |
| `docs/runtime-v2-epics/02-evidence.md` | Marked Task 4 complete and added this evidence section. | Keep Epic 2 evidence current before Task 5. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 4 handoff. | Preserve Task 5 start context. | Documentation only. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Updated status wording from Tasks 1-3 to Tasks 1-4. | Reflect recorded Task 4 evidence. | Documentation only. |

### Static Check Contract

`TestRuntimeV2SkeletonStaticShape` is hidden unless the caller passes
`-tags runtime_v2_pending`. The test feeds a small C snippet to `clang` with
`-std=c11 -fsyntax-only -Iruntime/native` and requires:

- `RT_RUNTIME_SHARD_COUNT` exists;
- `RT_RUNTIME_SHARD_COUNT == 1`;
- complete internal `rt_runtime` and `rt_shard` types exist;
- `rt_executor_runtime`, `rt_runtime_shard0`, and
  `rt_runtime_shard_count` are declared.

The check intentionally fails before Task 5 because the skeleton does not exist
yet. It is local-only until Task 12 wires the final Runtime V2 gate.

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `command -v clang` | tool exists | `/usr/bin/clang` | `0` | Static C shape probe preflight. |
| `command -v ar` | tool exists | `/usr/bin/ar` | `0` | Matches the Runtime V2 CI contract preflight. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s` | fail before Task 5 | failed with `RT_RUNTIME_SHARD_COUNT` missing, `rt_runtime` undeclared, `rt_shard` undeclared, and missing `rt_executor_runtime`, `rt_runtime_shard0`, and `rt_runtime_shard_count` declarations | `1` | Desired pre-implementation proof. |
| `go test ./internal/vm -run '^$' --timeout 30s` | default tag-off safety pass | `ok surge/internal/vm (cached) [no tests to run]` | `0` | Proves the pending file does not affect normal Go test discovery. |
| `git diff --check` | no whitespace errors | pass after docs/test edits | `0` | Final whitespace gate. |

Skipped by explicit Task 4 scope: skeleton implementation, runtime behavior
tests, `make check`, `make c-check`, `make cppcheck`, benchmarks, Sentrux
scans, `Makefile` edits, CI workflow edits, staging, and commit.

### CI Ownership

The pending static check is not part of default `make check`, the proposed
Runtime V2 seed command, or GitHub Actions. Task 5 should run it while
implementing the skeleton and record the transition from expected failure to
pass. Task 12 owns deciding whether this exact check, or a non-pending successor
with the same contract, joins `make runtime-v2-check`.

### Rollback/Recovery Notes

- Files or changes to revert: `internal/vm/runtime_v2_skeleton_static_test.go`,
  the Task 4 index and section in this file, the local-only row in
  `02-ci-test-contract.md`, the Task 4 handoff in `NOTES.md`, and the Epic 2
  status wording.
- Generated artifacts to remove: none.
- Runtime processes, sockets, or temporary state to clean up: none.

### Follow-Ups And Blockers

| Item | Blocks next code task? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Runtime/shard skeleton implementation | Yes. | Epic 2 Task 5. | The pending static check now proves the skeleton is absent. |
| Pending static check pass | Yes for Task 5 closeout. | Epic 2 Task 5. | Task 5 should make `TestRuntimeV2SkeletonStaticShape` pass or record a blocker. |
| CI inclusion | No for Task 5 start; yes before Epic 2 closeout. | Epic 2 Task 12. | This check is local-only until the Runtime V2 target/job exists. |
| Broad focused VM command debt | No for Epic 2. | Later test/backend matrix epic. | It remains excluded from required green gates. |

## Task 5: Runtime/Shard Skeleton

### Task Identity And Scope

- Task: Epic 2 Task 5, Runtime/Shard Skeleton.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: add the smallest internal `rt_runtime` and `rt_shard` skeleton with
  exactly one shard, make the Task 4 static shape check pass, and preserve the
  existing executor behavior boundary.
- Out of scope: public ABI changes, `N>1`, waiter ownership changes, fd
  registry changes, scheduler migration, net poll migration, channel/blocking
  changes, compiler changes, benchmarks, CI wiring, Sentrux rule-file creation,
  staging, and commit.
- Proving spike: `no`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `runtime/native/rt_async_internal.h` | Added `RT_RUNTIME_SHARD_COUNT`, complete internal `rt_runtime` and `rt_shard` types, runtime status codes, `rt_executor.runtime`, and the Task 4 accessors. | Expose the internal static shape required before later field movement. | `432` lines; still below 500. |
| `runtime/native/rt_runtime.c` | Added N=1 skeleton initialization, accessors, and moved cold default worker-count helpers. | Keep skeleton helper bodies out of the over-limit state file. | New file is `64` lines. |
| `runtime/native/rt_async_state.c` | Wired `exec_init_once()` to initialize the global N=1 runtime and renamed default-count calls. | Preserve the old `pthread_once`/panic boundary while creating the internal skeleton. | Reduced from `2391` to `2368` lines. |
| `docs/runtime-v2-epics/02-evidence.md` | Marked Task 5 complete and added this evidence section. | Keep Epic 2 evidence current. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 5 handoff. | Preserve next-task context. | Documentation only. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Updated status wording from Tasks 1-4 to Tasks 1-5. | Reflect recorded Task 5 evidence. | Documentation only. |

`runtime/native/rt.h` was not changed.

### Runtime Shape

The internal skeleton is explicit `N=1` only:

- `RT_RUNTIME_SHARD_COUNT` is `1`.
- `rt_runtime` owns `shards[RT_RUNTIME_SHARD_COUNT]`.
- `rt_shard` links back to its `rt_runtime` and current `rt_executor`.
- `rt_executor` gained only the `rt_runtime* runtime` link pointer.
- `rt_runtime_shard0` is the only shard accessor; no index-based lookup or
  multi-shard policy was added.

No task table, scope table, waiter list, fd scratch state, scheduler queue,
channel state, or blocking state moved in this task.

### Error/Status Boundary

New skeleton initialization uses explicit `rt_runtime_status` values. The only
recoverable validation currently possible is `RT_RUNTIME_STATUS_INVALID_ARGUMENT`
for null init arguments.

`exec_init_once()` still cannot return a status because it is called through
`pthread_once`, so it maps a non-OK skeleton init result to the existing legacy
`panic_msg("async: runtime skeleton initialization failed")` boundary. With the
current call path, this is not expected because `exec_state` is always passed.

### Sentrux Root/Scoped Signals

Main-agent baseline supplied before this task:

- Repository: `/home/zov/projects/surge/surge`, `quality_signal=6210`, rules
  file missing.
- Runtime: `/home/zov/projects/surge/surge/runtime`, `quality_signal=5147`,
  rules file missing.
- Runtime `session_start`: saved at `quality_signal=5147`.

Main-agent runtime `session_end` after the Task 5 changes reported `pass=true`,
`signal_before=5147`, `signal_after=5144`, `signal_delta=-2`, summary
`Quality stable or improved`, and no violations. A worker-context `session_end`
attempt could not reuse that baseline, so the main-agent result is the recorded
session evidence.

| Scan | Path | When | quality_signal | Root cause or bottleneck | Rules/session result |
| --- | --- | --- | --- | --- | --- |
| Repository | `/home/zov/projects/surge/surge` | After code changes | `6209` | bottleneck `modularity`; root causes: acyclicity `10000`, depth `6667`, equality `4695`, modularity `3435`, redundancy `8584`; cross-module edges `1820`; files `4743`; import edges `1887`; lines `371719` | `check_rules`: no rules file at `/home/zov/projects/surge/surge/.sentrux/rules.toml`; blocker/temporary deferral, not compliance. |
| Scoped | `/home/zov/projects/surge/surge/runtime` | After code changes | `5144` | bottleneck `redundancy`; root causes: acyclicity `10000`, depth `8889`, equality `4740`, modularity `3333`, redundancy `2565`; cross-module edges `0`; files `32`; import edges `30`; lines `14888` | `check_rules`: no rules file at `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`; blocker/temporary deferral, not compliance. |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s` | pass after Task 5 | passed; `TestRuntimeV2SkeletonStaticShape` ran and passed | `0` | Task 4 pending check flipped from expected failure to pass. |
| `command -v clang` | tool exists | `/usr/bin/clang` | `0` | Required preflight. |
| `command -v ar` | tool exists | `/usr/bin/ar` | `0` | Required preflight. |
| `git diff --check` | no whitespace errors | passed after final docs update | `0` | Final whitespace gate. |
| `make c-check` | pass | first run failed after moving CPU detection because `rt_async_state.c` still needed `<unistd.h>` for existing trace `write()` calls; after restoring the include, passed with formatting OK and strict C warnings OK | `0` final | The failure was introduced and fixed inside this task. |
| `make cppcheck` | pass | passed; checked `29/29` C files including `rt_runtime.c` | `0` | Static analysis gate. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(WakeupsAndCancellation\|ChannelParkUnpark\|BlockingChannelHelpersAllowTimersToAdvance\|SeededScheduler)$' -v --timeout 120s` | pass | passed; all four exact tests ran and passed | `0` | Runtime V2 seed liveness evidence. |
| `make check` | pass | passed; ran `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 90s`, `golangci-lint`, `make c-check`, and `check_file_sizes.sh` | `0` | Default broad gate still uses skipped timeout-sensitive tests. |
| `mcp__sentrux.session_end` | compare against pre-task runtime session if active | `pass=true`; `signal_before=5147`; `signal_after=5144`; `signal_delta=-2`; no violations | pass | Main-agent runtime session result. |
| `mcp__sentrux.scan` root + `health` + `check_rules` | post-change root signal | `quality_signal=6209`; rules missing | scan/health pass; rules blocker | Missing rules are not compliance. |
| `mcp__sentrux.scan` runtime + `health` + `check_rules` | post-change runtime signal | `quality_signal=5144`; rules missing | scan/health pass; rules blocker | Required runtime post-change check. |

Skipped by scope: benchmarks, `make runtime-v2-check` target wiring, Sentrux
rule-file creation, CI workflow edits, staging, and commit.

### Known Regressions

None known. The implementation changes only internal skeleton shape and cold
initialization links.

### Rollback/Recovery Notes

- Files or changes to revert: `runtime/native/rt_runtime.c`, the skeleton
  additions in `rt_async_internal.h`, the `exec_init_once()` runtime init call
  and default-count renames in `rt_async_state.c`, this Task 5 evidence
  section, the Task 5 notes entry, and the Epic 2 status wording.
- Generated artifacts to remove: none.
- Runtime processes, sockets, or temporary state to clean up: none.

### Follow-Ups And Blockers

| Item | Blocks next task? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Missing Sentrux rules | No for this task's implementation; yes for claiming rule compliance. | Dedicated Sentrux rules task or later Epic 2 closeout. | Both active scan paths still lack `.sentrux/rules.toml`. |
| Scheduler field movement | Yes for scheduler migration. | Epic 2 Tasks 6-7. | This task did not move ready queues, worker placement, or scheduler semantics. |
| Waiter and fd ownership | No for skeleton; yes for later owner work. | Local waiter and fd-registry epics. | This task intentionally kept waiters and fd readiness semantics unchanged. |
| CI inclusion of skeleton check | No for Task 5; yes before Epic 2 closeout if desired. | Epic 2 Task 12. | The shape test remains behind `runtime_v2_pending`. |
