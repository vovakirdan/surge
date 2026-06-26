# Epic 2 Evidence

This file records task-by-task evidence for
`02-n1-runtime-shard-structure.md`.

Do not use this file to hide known debt. The broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` is accepted backend-test debt
until the later test/backend matrix epic fixes or replaces it. Epic 2 evidence
must separate that debt from new runtime regressions.

Current status: Task 12 wiring is implemented. The `runtime-v2-check` target
and separate CI job now run the stable Runtime V2 liveness seed with
timeout-sensitive tests enabled. The broad VM/backend regex remains accepted
debt and is not a green gate.

## Task Evidence Index

| Task | Evidence status | Notes |
| --- | --- | --- |
| 1. Kickoff Evidence | Complete | Baseline, accepted VM debt, and Sentrux missing-rules deferral recorded. |
| 2. Field Ownership Map | Complete | Ownership map linked; movable and deferred field groups recorded. |
| 3. Runtime V2 Test And CI Contract | Complete | CI contract created; exact seed tests and excluded accepted-debt command recorded. |
| 4. Runtime/Shard Skeleton Tests | Complete | Added local-only pending static shape check; pre-Task-05 failure recorded. |
| 5. Runtime/Shard Skeleton | Complete | Internal `N=1` runtime/shard skeleton added; checks and Sentrux deltas recorded. |
| 6. Scheduler Shape Tests | Complete | Scheduler trace evidence selected; parked-with-work remains an explicit missing invariant. |
| 7. Scheduler Shape Migration | Complete | Scheduler container fields moved under `rt_shard.scheduler`; behavior gates and Sentrux status recorded. |
| 8. Net Poll Scratch Tests | Complete | Net wake probe and current-checkout native net benchmark baseline recorded. |
| 9. Net Poll Scratch Migration | Complete | Scratch buffers moved under `rt_shard`; local gates and Sentrux quality evidence recorded. |
| 10. Channel/Blocking Compatibility Tests | Complete with known debt | Stable direct/fallback seed tests and native channel benchmark baseline recorded; heavier local-only stress timeouts documented. |
| 11. Channel/Blocking Compatibility Migration | Complete | Counter ownership moved under `rt_shard.channel_blocking_compat`; direct/fallback gates, benchmark, static audits, and scan-only Sentrux snapshots recorded. |
| 12. CI Runtime V2 Gates | Complete | `runtime-v2-check` target and separate CI job added; local `make runtime-v2-check` and `make check` passed. |
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

## Task 6: Scheduler Shape Tests

### Task Identity And Scope

- Task: Epic 2 Task 6, Scheduler Shape Tests.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: select and run existing scheduler behavior proofs before scheduler
  field movement, record CI ownership, and name the missing scheduler invariant
  without adding a weak nondeterministic test.
- Out of scope: runtime C edits, Go test edits, scheduler migration,
  `Makefile` edits, GitHub Actions edits, STATS updates, benchmarks, Sentrux
  scans, staging, and commit.
- Proving spike: `no`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/02-evidence.md` | Marked Task 6 complete and added this evidence section. | Keep scheduler proof and blocker status durable before Task 7. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 6 handoff. | Preserve Task 7 start context and the parked-with-work condition. | Documentation only. |
| `docs/runtime-v2-epics/02-ci-test-contract.md` | Clarified `TestMTWorkStealing` local-only status after Task 6 evidence. | Keep CI ownership explicit: seeded scheduler stays in the seed; work stealing does not. | Documentation only. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Updated status wording from Tasks 1-5 to Tasks 1-6. | Reflect recorded Task 6 evidence. | Documentation only. |

No runtime C, Go test, `Makefile`, GitHub Actions, STATS, benchmark, task-doc,
or generated-report files were changed.

### Selected Scheduler Proofs

Task 6 uses existing tests instead of adding new tests:

- `TestMTWorkStealing`: current-runtime scheduler source trace proof. The test
  requires `SCHED_TRACE steal>0`, so it proves the existing Tier 1 scheduler can
  still steal ready work. This is legacy/current-runtime evidence only. Runtime
  V2 does not treat Tier 1 stealing as a future hot-path contract.
- `TestMTSeededScheduler`: seeded scheduler trace proof. The test runs the same
  program twice with `SURGE_SCHED=seeded`, `SURGE_SCHED_SEED=424242`, and
  `SURGE_SCHED_TRACE=1`; it requires `mode=seeded`, `seed=424242`, and matching
  trace `hash`/`events`. This remains part of the future Runtime V2 CI seed set.
- Stable Runtime V2 seed command: existing CI-shaped behavior proof for
  wakeups/cancellation, direct async channel wakeups, sync-helper timer
  progress, and seeded scheduler trace determinism.

### Missing Invariant Status

The parked-with-work invariant remains missing. Task 6 did not add a weak
snapshot/stress test because it would not provide deterministic proof.

Task 7 may proceed only while it preserves the current worker sleep and wake
rules: it may move scheduler containers behind `N=1` accessors, but it must not
change wake elision, worker sleep rules, or shard park state. If Task 7 needs
any of those semantic changes, it must stop and first add a real parked-with-work
invariant or re-scope the work.

No-double-poll remains an indirect behavior requirement for Task 7. Existing
tests cover task completion and scheduler trace shape, but they do not expose a
dedicated no-double-poll counter. Task 7 must preserve the current single-poll
discipline while moving fields.

### CI Ownership

`TestMTSeededScheduler` stays in the proposed Runtime V2 CI seed. Task 12 owns
the `runtime-v2-check` target and GitHub Actions wiring.

`TestMTWorkStealing` must stay local-only/current-runtime evidence unless a
later decision explicitly promotes stealing to a Runtime V2 Tier 2 CPU-pool
contract. Do not add it to the Runtime V2 CI seed as a Tier 1 scheduler
requirement.

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `command -v clang` | tool exists | `/usr/bin/clang` | `0` | Required LLVM test preflight. |
| `command -v ar` | tool exists | `/usr/bin/ar` | `0` | Required LLVM test preflight. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(WorkStealing\|SeededScheduler)$' -v --timeout 90s` | pass | passed; both `TestMTWorkStealing` and `TestMTSeededScheduler` ran and passed | `0` | Scheduler source trace proof; terminal output only, no artifact file written. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(WakeupsAndCancellation\|ChannelParkUnpark\|BlockingChannelHelpersAllowTimersToAdvance\|SeededScheduler)$' -v --timeout 120s` | pass | passed; all four exact tests ran and passed | `0` | CI-shaped Runtime V2 behavior proof; terminal output only, no artifact file written. |
| `git diff --check` | no whitespace errors | passed after docs edits | `0` | Final whitespace gate. |

Skipped by scope: broad focused VM regex, `make check`, `make c-check`,
`make cppcheck`, benchmarks, Sentrux scans, `Makefile` edits, GitHub Actions
edits, runtime C edits, Go test edits, staging, and commit.

### Trace/Liveness Interpretation

The focused scheduler command proves current scheduler trace shape through test
assertions, not through manually copied trace rows:

- `TestMTWorkStealing` fails unless the current scheduler reports
  `SCHED_TRACE steal>0`.
- `TestMTSeededScheduler` fails unless seeded mode reports the expected seed and
  repeatable `hash`/`events`.

The command does not prove parked-with-work, wake-fd elision, owner-local
waiters, fd registry lifecycle, or cross-shard behavior.

### Known Regressions

None known. Task 6 changed documentation only.

### Rollback/Recovery Notes

- Files or changes to revert: this Task 6 evidence section, the Task 6 index
  status, the Task 6 notes handoff, the CI contract wording for work-stealing
  local-only status, and the Epic 2 status wording.
- Generated artifacts to remove: none.
- Runtime processes, sockets, or temporary state to clean up: none.

### Follow-Ups And Blockers

| Item | Blocks next task? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Scheduler field movement | No, if Task 7 is a behavior-preserving container/accessor move only. | Epic 2 Task 7. | Current scheduler source trace and CI-shaped behavior proof passed. |
| Parked-with-work invariant | Conditional. Blocks Task 7 if it changes wake elision, worker sleep rules, or shard park state. | Epic 2 Task 7 if it crosses that boundary; otherwise later cross-shard wake/park owner. | The invariant is still missing and was not faked by a nondeterministic test. |
| `TestMTWorkStealing` CI promotion | No. | Later Tier 2 CPU-pool work, if promoted. | Tier 1 stealing is a current implementation artifact, not a Runtime V2 hot-path contract. |
| CI target and workflow wiring | No for Task 7; yes before Epic 2 closeout. | Epic 2 Task 12. | Task 6 records evidence; Task 12 wires automation. |

## Task 7: Scheduler Shape Migration

### Task Identity And Scope

- Task: Epic 2 Task 7, Scheduler Shape Migration.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: move the current scheduler container fields behind the existing
  single-shard shape without changing scheduling behavior.
- Out of scope: `runtime/native/rt.h`, `Makefile`, CI, Go tests, benchmarks,
  Sentrux rules, net scratch migration, waiter ownership, channel/blocking
  migration, parked-with-work invariant, shard park state, `N>1`, staging, and
  commit.
- Proving spike: `no`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `runtime/native/rt_async_internal.h` | Added `rt_scheduler`, placed it under `rt_shard`, moved `inject`, `local_queues`, `worker_ctxs`, `worker_count`, `running_count`, `sched_mode`, and `sched_seed` out of `rt_executor`, and declared scheduler accessors/init. | Make the scheduler owner shape explicit under the single shard. | `446` lines, up from `432`; still below 500. |
| `runtime/native/rt_runtime.c` | Added scheduler accessors and `rt_shard_scheduler_init()`. | Keep runtime/shard helper bodies outside the large state file. | `109` lines, up from `64`. |
| `runtime/native/rt_async_state.c` | Routed scheduler queue, worker-count, running-count, seeded trace, worker-context, and compensation reads/writes through `rt_shard.scheduler`. | Preserve existing ready queue and worker behavior while changing ownership shape. | `2409` lines, up from `2368`; already over limit. No split in this task. |
| `runtime/native/rt_async_task.c` | Routed inline child polling and task await worker-count checks through the scheduler accessor. | Keep task-side scheduler accounting on the moved owner. | `768` lines, up from `763`; already over limit. No split in this task. |
| `docs/runtime-v2-epics/02-evidence.md` | Marked Task 7 complete and added this evidence section. | Record behavior proof, Sentrux status, and handoff. | Documentation. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 7 handoff. | Preserve Task 8/9 start context. | Documentation. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Updated status wording from Tasks 1-6 to Tasks 1-7. | Reflect recorded Task 7 evidence. | Documentation. |

No public ABI, net/channel/waiter/task ownership, benchmark, CI, `Makefile`,
Sentrux rule, staging, or commit changes were made.

### Scheduler Shape Boundary

Task 7 moved only the approved scheduler field group:

- `inject`
- `local_queues`
- `worker_ctxs`
- `worker_count`
- `running_count`
- `sched_mode`
- `sched_seed`

The owner is now `rt_shard.scheduler`. The process-lifetime allocation model did
not change: local queues are still allocated during `exec_init_once()`, now via
`rt_shard_scheduler_init()`, and worker contexts are still allocated when workers
start. The local queue allocation failure path still reaches the legacy init
panic boundary with `async: local queue allocation failed`.

The following state stayed on `rt_executor`: `workers`, `ready_cv`, `io_cv`,
`done_cv`, `lock`, `shutdown`, `net_polling`, `initialized`, `io_started`,
`channel_blocked_workers`, `compensation_count`, `compensation_high_water`, and
all blocking-pool fields.

The change preserved local LIFO, inject FIFO, steal order, seeded RNG formulas,
`SCHED_TRACE` output names and meanings, wake-token flow, wake elision,
`ready_cv` signal/broadcast sites, `running_count` under `ex->lock`, `io_cv`
idle signaling, and `rt_worker_count()` value.

### Missing Invariant Status

The parked-with-work invariant remains missing. Task 7 did not add parked state,
wake-fd elision, or a shard park invariant.

This task did not change wake elision, worker sleep rules, or shard park state.
The Task 6 condition is therefore still satisfied. Any later task that changes
those rules must stop and add a real parked-with-work invariant first.

### Direct Access Audit

The required direct-access audit has no remaining matches:

```bash
rg -n -- 'ex->(inject|local_queues|worker_ctxs|worker_count|running_count|sched_mode|sched_seed)\b|exec_state\.(sched_seed|sched_mode)' runtime/native
```

`rg` returned no output and exit status `1`, which is the expected no-match
status. The moved fields remain visible only through `rt_shard.scheduler` and
the scheduler helpers.

### Sentrux

Main-agent baseline supplied before implementation:

- Repository: `/home/zov/projects/surge/surge`, `quality_signal=6207`, rules
  missing.
- Runtime: `/home/zov/projects/surge/surge/runtime`, `quality_signal=5125`,
  rules missing.
- Runtime `session_start`: saved at `quality_signal=5125`.

Main-agent runtime `session_end` after the Task 7 changes reported `pass=true`,
`signal_before=5125`, `signal_after=5168`, `signal_delta=43`, summary
`Quality stable or improved`, and no violations. A worker-context `session_end`
attempt could not reuse that baseline, so the main-agent result is the recorded
session evidence.

Post-change Sentrux checks:

| Scan | Path | When | quality_signal | Root cause or bottleneck | Rules/session result |
| --- | --- | --- | --- | --- | --- |
| Repository | `/home/zov/projects/surge/surge` | After code changes | `6207` | bottleneck `modularity`; root causes: acyclicity `10000`, depth `6667`, equality `4689`, modularity `3438`, redundancy `8573`; cross-module edges `1820`; files `4744`; import edges `1888`; lines `372216` | `check_rules`: no rules file at `/home/zov/projects/surge/surge/.sentrux/rules.toml`; blocker/temporary deferral, not compliance. |
| Runtime | `/home/zov/projects/surge/surge/runtime` | After code changes | `5168` | bottleneck `redundancy`; root causes: acyclicity `10000`, depth `8889`, equality `4764`, modularity `3333`, redundancy `2612`; cross-module edges `0`; files `33`; import edges `31`; lines `15057` | `check_rules`: no rules file at `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`; blocker/temporary deferral, not compliance. |

Compared with the supplied baseline, the repository signal stayed flat at
`6207`; the runtime session increased from `5125` to `5168` and passed with no
violations. Missing Sentrux rules remain a blocker to claiming rule compliance,
not a blocker to this narrow shape migration.

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `command -v clang` | tool exists | `/usr/bin/clang` | `0` | Required LLVM test preflight. |
| `command -v ar` | tool exists | `/usr/bin/ar` | `0` | Required LLVM test preflight. |
| `rg -n -- 'ex->(inject\|local_queues\|worker_ctxs\|worker_count\|running_count\|sched_mode\|sched_seed)\b\|exec_state\.(sched_seed\|sched_mode)' runtime/native` | no direct moved-field executor hits | no output | `1` | `rg` no-match status; expected. |
| `go clean -testcache` | clear cached Go test results | completed with no output | `0` | Used once so the exact Go probes below ran fresh after final C edits. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s` | pass | `TestRuntimeV2SkeletonStaticShape` ran and passed; package time `0.036s` | `0` | Static Runtime V2 shape check. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(WorkStealing\|SeededScheduler)$' -v --timeout 90s` | pass | `TestMTWorkStealing` and `TestMTSeededScheduler` ran and passed; package time `2.546s` | `0` | Scheduler source trace proof. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(WakeupsAndCancellation\|ChannelParkUnpark\|BlockingChannelHelpersAllowTimersToAdvance\|SeededScheduler)$' -v --timeout 120s` | pass | all four exact tests ran and passed; package time `4.490s` | `0` | CI-shaped Runtime V2 behavior proof. |
| `make c-check` | pass | formatting and strict C warning compile passed; `All C runtime checks passed` | `0` | Standalone final run. |
| `make cppcheck` | pass | scanned 29 native C files; `cppcheck OK` | `0` | A first run flagged const-pointer style issues; fixed before this final pass. |
| `make check` | pass | `go test ./...` with `SURGE_SKIP_TIMEOUT_TESTS=1` passed; `golangci-lint` reported `0 issues`; `make c-check` passed; file-size script found no applicable unstaged files | `0` | Default repository gate. |
| `git diff --check` | no whitespace errors | passed with no output after docs edits | `0` | Final whitespace gate. |

### Trace/Liveness Interpretation

The scheduler trace proof still comes from test assertions:

- `TestMTWorkStealing` fails unless the current scheduler reports
  `SCHED_TRACE steal>0`.
- `TestMTSeededScheduler` fails unless seeded mode reports the expected seed and
  repeatable `hash`/`events`.

Task 7 did not promote work stealing to a future Runtime V2 Tier 1 contract. It
also did not prove parked-with-work, wake-fd elision, owner-local waiters, fd
registry lifecycle, or cross-shard behavior.

### Known Regressions

None known.

### Rollback/Recovery Notes

- Code rollback: revert the `rt_scheduler` field move and helper/accessor use in
  `runtime/native/rt_async_internal.h`, `runtime/native/rt_runtime.c`,
  `runtime/native/rt_async_state.c`, and `runtime/native/rt_async_task.c`.
- Documentation rollback: revert this Task 7 evidence section, the Task 7 index
  status, the Task 7 notes handoff, and the Epic 2 status wording.
- Generated artifacts to remove: none.
- Runtime processes, sockets, or temporary state to clean up: none.

### Follow-Ups And Blockers

| Item | Blocks next task? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Net poll scratch tests | Yes for Task 9; no for Task 8. | Epic 2 Task 8. | Task 8 must record current net wake and benchmark evidence before scratch fields move. |
| Net poll scratch migration | No, after Task 8 evidence exists. | Epic 2 Task 9. | Scheduler shape migration does not touch net waiters or scratch buffers. |
| Parked-with-work invariant | No for Task 8/9 if they preserve worker sleep/wake and shard park state. | Later scheduler/wake-fd owner if those rules change. | Still missing; Task 7 did not cross the Task 6 boundary. |
| Persistent fd registry | No for Task 8/9 if scratch migration preserves rebuild-from-waiters semantics. | Local fd-registry epic. | Task 9 must not introduce persistent readiness registration. |
| `TestMTWorkStealing` CI promotion | No. | Later Tier 2 CPU-pool work, if promoted. | It remains local-only/current-runtime evidence. |
| CI target and workflow wiring | No for Task 8/9; yes before Epic 2 closeout. | Epic 2 Task 12. | Runtime V2 automation remains a later task. |

## Task 8: Net Poll Scratch Tests

### Task Identity And Scope

- Task: Epic 2 Task 8, Net Poll Scratch Tests.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: record net wake and native net benchmark before-evidence before Task 9
  moves net poll scratch storage.
- Out of scope: runtime/native edits, Go test edits, benchmark script edits,
  `Makefile`, CI, Sentrux rules, fd registry, accept ownership, net semantic
  changes, STATS, staging, and commit.
- Proving spike: `no`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/02-evidence.md` | Marked Task 8 complete and added this evidence section. | Preserve net wake and benchmark before-evidence for Task 9. | Documentation. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 8 handoff. | Preserve Task 9 start context and boundaries. | Documentation. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Updated status wording from Tasks 1-7 to Tasks 1-8. | Reflect recorded Task 8 evidence. | Documentation. |

No runtime C, Go test, script, `Makefile`, CI workflow, Sentrux rule, STATS,
task-doc, staging, or commit changes were made. `02-ci-test-contract.md` already
records `TestMTNetWaiterWakeupLatency` as local-only until re-proven by Task 12,
so this task did not edit the CI contract.

### Current-Checkout Compiler Pin

The benchmark used a temporary compiler binary built outside the repository:

```bash
current_commit="$(git rev-parse --short=12 HEAD)"
tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/surge-task08.XXXXXX")"
go build -ldflags "$(./scripts/ldflags.sh --local)" -o "$tmpdir/surge" ./cmd/surge/
reported_commit="$($tmpdir/surge version --full --format json | python3 -c 'import json,sys; print(json.load(sys.stdin).get("git_commit", ""))')"
printf 'tmpdir=%s\n' "$tmpdir"
printf 'surge=%s\n' "$tmpdir/surge"
printf 'current_commit=%s\n' "$current_commit"
printf 'reported_commit=%s\n' "$reported_commit"
test "$reported_commit" = "$current_commit"
$tmpdir/surge version --full
```

Output summary:

```text
tmpdir=/tmp/surge-task08.zkEoYd
surge=/tmp/surge-task08.zkEoYd/surge
current_commit=49b3aa34ec26
reported_commit=49b3aa34ec26
surge 0.1.13-dev — "forge storms before they land"
commit: 49b3aa34ec26
message: refactor(runtime): move scheduler state under shard
built:  2026-06-26T12:41:59Z
```

The reported commit matched current `HEAD`, so the benchmark evidence came from
the current checkout binary, not an installed or stale `surge`.

### Net Wake Probe

`TestMTNetWaiterWakeupLatency` passed:

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
  go test ./internal/vm \
    -run '^TestMTNetWaiterWakeupLatency$' \
    -v --timeout 90s
```

Output:

```text
=== RUN   TestMTNetWaiterWakeupLatency
--- PASS: TestMTNetWaiterWakeupLatency (2.64s)
PASS
ok  	surge/internal/vm	2.647s
```

The test did not print trace rows on success. It asserted them internally from
the child process stderr: a generic `TRACE_NET` row, a
`TRACE_NET reason=sigusr1` row, and `TRACE_EXEC_SNAPSHOT reason=sigusr1`. The
asserted net fields include `io_poll_calls`, `io_poll_net_ready`,
`io_poll_waiters_last`, `io_poll_waiters_max`, `io_direct_waits`,
`io_waiter_scan_entries`, `io_waiter_net_entries`, `io_poll_rebuilds`,
`io_poll_allocs`, `io_poll_dedup_checks`, `io_waiter_complete_calls`, and
`io_waiter_completed`.

### Native Net Benchmark

Command:

```bash
tmpdir=/tmp/surge-task08.zkEoYd
SURGE_NET_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task08-native-net-before.md" \
  timeout 120s env SURGE="$tmpdir/surge" ./scripts/bench_native_net.sh
```

Result: passed in `8.88s`.

Report:

```text
/home/zov/projects/surge/surge/build/benchmarks/runtime-v2-task08-native-net-before.md
```

Report environment:

- generated: `2026-06-26T12:42:15Z`;
- surge commit: `49b3aa34ec26`;
- fixture: `benchmarks/native/net_request_reply`;
- threads: `1 2 4 8`;
- modes: `echo direct manager`;
- patterns: `seq pipe`;
- requests: `2000`;
- pipeline depth: `64`;
- trace: per run `SURGE_TRACE_EXEC=1 SURGE_SCHED_TRACE=1`.

Selected `## Results` rows for Task 9 before comparison:

| threads | mode | pattern | requests | total us | avg us/op | p50 us | p95 us |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | echo | seq | 2000 | 135956 | 66.14 | 56.33 | 119.26 |
| 1 | echo | pipe | 2000 | 46926 | 22.53 | 22.53 | 25.38 |
| 1 | manager | seq | 2000 | 220693 | 108.54 | 98.95 | 170.56 |
| 2 | echo | seq | 2000 | 183910 | 90.17 | 58.92 | 250.77 |
| 2 | manager | seq | 2000 | 356900 | 176.50 | 144.23 | 354.38 |
| 4 | direct | seq | 2000 | 235650 | 116.08 | 87.06 | 285.76 |
| 4 | manager | pipe | 2000 | 211813 | 104.96 | 105.14 | 112.64 |
| 8 | echo | pipe | 2000 | 79665 | 38.92 | 38.60 | 43.54 |
| 8 | manager | seq | 2000 | 358790 | 177.40 | 147.43 | 338.45 |
| 8 | manager | pipe | 2000 | 212421 | 105.26 | 106.37 | 112.20 |

Selected `## Runtime Trace` rows for Task 9 before comparison:

| threads | mode | pattern | handoff yields | sched inject | sched steal | net direct waits | net poll calls | net ready | net waiters total | waiter scan entries | net waiter entries | poll rebuilds | poll allocs | dedup checks | complete calls | completed waiters |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | echo | seq | 0 | 13843 | 0 | 1837 | 4798 | 1837 | 4798 | 14390 | 4798 | 4798 | 2 | 0 | 3674 | 1837 |
| 1 | echo | pipe | 0 | 8101 | 0 | 31 | 91 | 31 | 91 | 269 | 91 | 91 | 2 | 0 | 62 | 31 |
| 1 | manager | seq | 2000 | 17793 | 0 | 1784 | 4363 | 1784 | 4363 | 17446 | 4363 | 4363 | 2 | 0 | 3568 | 1784 |
| 2 | echo | seq | 0 | 821 | 6 | 430 | 463 | 430 | 463 | 1385 | 463 | 463 | 2 | 0 | 860 | 430 |
| 2 | manager | seq | 1999 | 5056 | 4 | 629 | 730 | 629 | 730 | 2914 | 730 | 730 | 2 | 0 | 1258 | 629 |
| 4 | direct | seq | 0 | 539 | 0 | 350 | 405 | 350 | 405 | 1210 | 405 | 405 | 2 | 0 | 700 | 350 |
| 4 | manager | pipe | 1999 | 4018 | 1 | 30 | 36 | 30 | 36 | 138 | 36 | 36 | 2 | 0 | 60 | 30 |
| 8 | echo | pipe | 0 | 25 | 0 | 30 | 35 | 30 | 35 | 101 | 35 | 35 | 2 | 0 | 60 | 30 |
| 8 | manager | seq | 1999 | 4852 | 4 | 589 | 659 | 589 | 659 | 2630 | 659 | 659 | 2 | 0 | 1178 | 589 |
| 8 | manager | pipe | 1999 | 4017 | 0 | 31 | 35 | 31 | 35 | 134 | 35 | 35 | 2 | 0 | 62 | 31 |

Across the full 24-row report, task-context blocking sends, task-context
blocking recvs, compensation started, and compensation high-water stayed `0`;
`poll allocs` stayed `2`; and `dedup checks` stayed `0`.

### Test Decision

No new semantic test is needed for Task 9 if it only moves
`net_poll_fds`, `net_poll_fds_cap`, `net_poll_pfds`, and
`net_poll_pfds_cap` behind the `N=1` shard/container and preserves
rebuild-from-waiters semantics.

Scratch contents are not persistent readiness state. The current contract is
that each poll rebuilds temporary arrays from the current waiter list, polls
those descriptors, and completes matching waiters. The existing wake probe and
native benchmark cover that boundary. A new test becomes necessary only if Task
9 changes waiter ownership, persistent fd registration, dedup semantics,
readiness lifetime, accept ownership, poll ownership, or net wake placement.

### CI Ownership

`TestMTNetWaiterWakeupLatency` remains local-only Task 8/9 evidence. It should
join `runtime-v2-check` only if Task 12 re-proves its stability in CI with
`SURGE_BACKEND=llvm`, `SURGE_SKIP_TIMEOUT_TESTS=0`, toolchain preflight, and a
clear timeout.

`scripts/bench_native_net.sh` remains manual before/after evidence. Do not wire
the native net benchmark into CI.

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `git status --short` | clean or known dirty state recorded | no output | `0` | Worktree started clean. |
| `command -v clang` | tool exists | `/usr/bin/clang` | `0` | Required LLVM test preflight. |
| `command -v ar` | tool exists | `/usr/bin/ar` | `0` | Required LLVM test preflight. |
| Temp compiler build and commit verification block above | reported commit matches current `HEAD` | `current_commit=49b3aa34ec26`, `reported_commit=49b3aa34ec26` | `0` | `SURGE=/tmp/surge-task08.zkEoYd/surge` used for benchmark. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s` | pass | `TestMTNetWaiterWakeupLatency` ran and passed; package time `2.647s` | `0` | Trace rows asserted internally, not printed on success. |
| `tmpdir=/tmp/surge-task08.zkEoYd; SURGE_NET_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task08-native-net-before.md" timeout 120s env SURGE="$tmpdir/surge" ./scripts/bench_native_net.sh` | pass and write report | passed; report path printed | `0` | Manual benchmark evidence with current-checkout compiler. |
| `git diff --check` | no whitespace errors | passed with no output after docs edits | `0` | Final whitespace gate. |

Skipped by scope: broad focused VM regex, `make check`, `make c-check`,
`make cppcheck`, Sentrux scans, runtime C edits, Go test edits, script edits,
`Makefile` edits, GitHub Actions edits, STATS edits, task-doc edits, staging,
and commit.

### Follow-Ups And Blockers

| Item | Blocks next task? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Net poll scratch migration | No, if Task 9 preserves rebuild-from-waiters semantics and only moves scratch buffers. | Epic 2 Task 9. | Task 8 before-evidence exists. |
| Persistent fd registry | Yes, if attempted in Task 9. | Later local fd-registry epic. | Current evidence does not prove readiness persistence, close lifecycle, or registry dedup. |
| Accept ownership changes | Yes, if attempted in Task 9. | Later accept-ownership/local fd-registry work. | Task 8 only proves current accept/read/write waiter wake behavior. |
| Net semantic changes | Yes, if attempted in Task 9. | Separate approved plan and probes. | Task 8 is evidence-only and does not authorize semantic movement. |
| CI promotion for net latency probe | No for Task 9; yes before adding to automation. | Epic 2 Task 12. | The probe remains local-only until re-proven in CI. |

## Task 9: Net Poll Scratch Migration

### Task Identity And Scope

- Task: Epic 2 Task 9, Net Poll Scratch Migration.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Scope: move only the existing net poll scratch arrays/caps behind the
  `N=1` shard shape.
- Runtime files changed: `runtime/native/rt_async_internal.h`,
  `runtime/native/rt_runtime.c`, `runtime/native/rt_net.c`.
- Docs changed: `docs/runtime-v2-epics/02-evidence.md`,
  `docs/runtime-v2-epics/NOTES.md`,
  `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`.
- Out of scope and unchanged by design: `net_waiters_len`, `net_polling`,
  `io_cv`, waiter ownership, wake fd placement, accept ownership, fd registry,
  readiness lifetime, dedup semantics, public ABI, compiler behavior, benchmark
  scripts, `Makefile`, CI, Sentrux rules, STATS, and test files.

### Changed Files

| File | Change | Why |
| --- | --- | --- |
| `runtime/native/rt_async_internal.h` | Added `rt_net_poll_scratch`, placed it under `rt_shard`, removed the four scratch fields from `rt_executor`, and declared scratch accessors. | Put scratch storage behind the `N=1` shard container without changing public ABI. |
| `runtime/native/rt_runtime.c` | Added `rt_shard_net_poll_scratch()` and `rt_executor_net_poll_scratch()`. | Mirror the scheduler accessor shape and keep callers N=1-compatible. |
| `runtime/native/rt_net.c` | Changed `ensure_net_poll_fds()` and `ensure_net_poll_pfds()` to use shard scratch. | Preserve allocation behavior while moving storage ownership. |
| `docs/runtime-v2-epics/02-evidence.md` | Added this Task 9 evidence section. | Record local gates and the Sentrux handoff. |
| `docs/runtime-v2-epics/NOTES.md` | Added the Task 9 handoff. | Keep the working notes current before closeout. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Updated the status line. | Reflect Task 9 local evidence. |

### Implementation Shape

Task 9 added:

```c
typedef struct {
    void* fds;
    size_t fds_cap;
    void* pfds;
    size_t pfds_cap;
} rt_net_poll_scratch;
```

`struct rt_shard` now owns `rt_net_poll_scratch net_poll_scratch`. The executor
keeps its runtime pointer, and `rt_executor_net_poll_scratch(ex)` resolves the
single shard through `rt_runtime_shard0(rt_executor_runtime(ex))`.

This remains `N=1`-compatible because there is still exactly one
`RT_RUNTIME_SHARD_COUNT` shard. No caller can select another shard, and the
global executor lock still protects the scratch arrays while `poll_net_waiters()`
rebuilds the temporary poll set.

`poll_net_waiters()` still:

- exits on `ex == NULL` or `ex->net_waiters_len == 0`;
- sizes the temporary fd array from `ex->net_waiters_len`;
- scans `ex->waiters` and keeps the existing fd dedup loop;
- initializes and includes the same net poll wake fd;
- releases `ex->lock` only around `poll()`;
- completes the same read, accept, and write waiter keys after readiness.

### Static Audits

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `sed -n '/struct rt_executor {/,/^};/p' runtime/native/rt_async_internal.h \| rg -n 'net_poll_fds\|net_poll_pfds' \|\| true` | no output | no output | `0` | Old scratch fields are gone from `rt_executor`. |
| `sed -n '/struct rt_shard {/,/^};/p' runtime/native/rt_async_internal.h \| rg -n 'rt_net_poll_scratch net_poll_scratch' \|\| true` | one shard owner line | `5:    rt_net_poll_scratch net_poll_scratch;` | `0` | Scratch lives under the single shard. |
| `rg -n -- '->(net_poll_fds\|net_poll_fds_cap\|net_poll_pfds\|net_poll_pfds_cap)\b' runtime/native \|\| true` | no output | no output | `0` | No direct old executor-field usage remains. |
| `rg -n -- 'rt_executor_net_poll_scratch\|rt_shard_net_poll_scratch\|rt_net_poll_scratch' runtime/native` | header, runtime accessor, and `rt_net.c` users only | found `rt_async_internal.h`, `rt_runtime.c`, and `rt_net.c` only | `0` | New owner/accessor surface is narrow. |
| `git diff -U0 -- runtime/native \| rg -n '^[+-].*(net_waiters_len\|net_polling\|io_cv\|waiters\|net_poll_wake\|epoll\|kqueue\|io_uring\|eventfd\|registry\|accept)' \|\| true` | no output | no output | `0` | No changed runtime lines touched forbidden net semantics. |

### Test And Benchmark Evidence

Temporary compiler build and commit verification:

```bash
tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/surge-task09.XXXXXX")"
current_commit="$(git rev-parse --short=12 HEAD)"
go build -ldflags "$(./scripts/ldflags.sh --local)" -o "$tmpdir/surge" ./cmd/surge/
reported_commit="$($tmpdir/surge version --full --format json | python3 -c 'import json,sys; print(json.load(sys.stdin).get("git_commit", ""))')"
test "$reported_commit" = "$current_commit"
```

Result:

```text
tmpdir=/tmp/surge-task09-final.aqFZBL
surge=/tmp/surge-task09-final.aqFZBL/surge
current_commit=b48f58ec84e0
reported_commit=b48f58ec84e0
```

Focused net wake probe:

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s
```

Result: passed. `TestMTNetWaiterWakeupLatency` ran and passed in `2.61s`;
package time was `2.610s`.

Native net after-benchmark:

```bash
SURGE_NET_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task09-native-net-after.md" \
  timeout 120s env SURGE="/tmp/surge-task09-final.aqFZBL/surge" ./scripts/bench_native_net.sh
```

Result: passed. Report path:
`/home/zov/projects/surge/surge/build/benchmarks/runtime-v2-task09-native-net-after.md`.
The report is a local ignored artifact under `build/`; the durable evidence is
the copied selected rows below.

Selected `## Results` rows:

| threads | mode | pattern | requests | total us | avg us/op | p50 us | p95 us |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | echo | seq | 2000 | 150152 | 73.33 | 60.59 | 129.42 |
| 1 | echo | pipe | 2000 | 47976 | 23.09 | 23.23 | 26.70 |
| 1 | manager | seq | 2000 | 253520 | 124.78 | 113.03 | 207.59 |
| 2 | echo | seq | 2000 | 178299 | 87.36 | 58.12 | 241.91 |
| 2 | manager | seq | 2000 | 390944 | 193.33 | 158.72 | 378.61 |
| 4 | direct | seq | 2000 | 265964 | 131.03 | 96.78 | 303.52 |
| 4 | manager | pipe | 2000 | 219484 | 108.77 | 107.46 | 116.60 |
| 8 | echo | pipe | 2000 | 77429 | 37.82 | 37.51 | 44.82 |
| 8 | manager | seq | 2000 | 361986 | 179.00 | 149.08 | 347.00 |
| 8 | manager | pipe | 2000 | 226236 | 112.10 | 110.69 | 129.73 |

Selected `## Runtime Trace` rows:

| threads | mode | pattern | handoff yields | sched inject | sched steal | net direct waits | net poll calls | net ready | net waiters total | waiter scan entries | net waiter entries | poll rebuilds | poll allocs | dedup checks | complete calls | completed waiters |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | echo | seq | 0 | 13829 | 0 | 1823 | 5051 | 1823 | 5051 | 15149 | 5051 | 5051 | 2 | 0 | 3646 | 1823 |
| 1 | echo | pipe | 0 | 8101 | 0 | 31 | 86 | 31 | 86 | 254 | 86 | 86 | 2 | 0 | 62 | 31 |
| 1 | manager | seq | 2000 | 17814 | 0 | 1805 | 4732 | 1805 | 4732 | 18922 | 4732 | 4732 | 2 | 0 | 3610 | 1805 |
| 2 | echo | seq | 0 | 653 | 3 | 417 | 480 | 417 | 480 | 1434 | 480 | 480 | 2 | 0 | 834 | 417 |
| 2 | manager | seq | 2000 | 5389 | 11 | 795 | 905 | 795 | 905 | 3613 | 905 | 905 | 2 | 0 | 1590 | 795 |
| 4 | direct | seq | 0 | 877 | 2 | 532 | 584 | 532 | 584 | 1748 | 584 | 584 | 2 | 0 | 1064 | 532 |
| 4 | manager | pipe | 1999 | 4015 | 0 | 29 | 34 | 29 | 34 | 129 | 34 | 34 | 2 | 0 | 58 | 29 |
| 8 | echo | pipe | 0 | 10 | 0 | 26 | 28 | 26 | 28 | 79 | 28 | 28 | 2 | 0 | 52 | 26 |
| 8 | manager | seq | 1999 | 4841 | 3 | 666 | 754 | 666 | 754 | 3009 | 754 | 754 | 2 | 0 | 1332 | 666 |
| 8 | manager | pipe | 1999 | 4066 | 0 | 30 | 35 | 30 | 35 | 134 | 35 | 35 | 2 | 0 | 60 | 30 |

Across the full 24-row report, task-context blocking sends, task-context
blocking recvs, compensation started, and compensation high-water stayed `0`;
`poll allocs` stayed `2`; and `dedup checks` stayed `0`.

### Sentrux Evidence

Main-session Sentrux checks were run after the worker completed local code and
test gates:

- Root scan before Task 9 runtime edits: `quality_signal=6207`.
- Runtime/native scoped scan before Task 9 runtime edits:
  `/home/zov/projects/surge/surge/runtime/native`, `quality_signal=5132`.
- Runtime/native `session_end` after Task 9 runtime edits: `pass=true`,
  `signal_before=5132`, `signal_after=5146`, `signal_delta=14`, no
  violations, summary `Quality stable or improved`.
- Root scan after Task 9 runtime edits: `quality_signal=6207`.
- Runtime policy scoped scan after Task 9 runtime edits:
  `/home/zov/projects/surge/surge/runtime`, `quality_signal=5182`.
- Runtime/native scoped scan after Task 9 runtime edits: `quality_signal=5146`.
- Root `check_rules`: missing `/home/zov/projects/surge/surge/.sentrux/rules.toml`.
- Runtime policy `check_rules`: missing
  `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`.
- Runtime/native `check_rules`: missing
  `/home/zov/projects/surge/surge/runtime/native/.sentrux/rules.toml`.

The quality gate passed and improved for the runtime/native scope, and the
required runtime policy scan was recorded. Missing Sentrux rule
files remain accepted/deferred rule-compliance debt and must not be reported as
a passing rules check.

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `git status --short --branch` before edits | known branch and no unrelated dirty files | `## codex/runtime-net-scheduler-refactor...origin/codex/runtime-net-scheduler-refactor [ahead 14]` | `0` | Started without listed file changes. |
| Static audits above | pass | passed | `0` | Proved scratch fields moved and forbidden net semantics unchanged. |
| Temporary compiler build and commit verification block above | reported commit matches current `HEAD` | `current_commit=b48f58ec84e0`, `reported_commit=b48f58ec84e0` | `0` | Benchmark used `/tmp/surge-task09-final.aqFZBL/surge`. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s` | pass | passed; package time `2.610s` | `0` | Focused Task 8/9 net wake probe on code commit `b48f58ec84e0`. |
| `SURGE_NET_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task09-native-net-after.md" timeout 120s env SURGE="/tmp/surge-task09-final.aqFZBL/surge" ./scripts/bench_native_net.sh` | pass and write report | passed; report path printed | `0` | Manual benchmark evidence with current-checkout compiler. |
| `make c-check` | pass | passed; C formatting and strict runtime compile OK | `0` | Native C gate. |
| `make cppcheck` | pass | passed; `cppcheck OK` | `0` | Static analysis gate. |
| `make check` | pass | passed; Go tests, lint, nested `make c-check`, and file-size check passed | `0` | Full local gate. |
| `mcp__sentrux.session_end` after main-session runtime/native baseline | pass or no quality regression | `pass=true`; `signal_before=5132`; `signal_after=5146`; `signal_delta=14`; no violations | pass | Scoped runtime/native quality delta. |
| Root, runtime, and runtime/native `mcp__sentrux.scan` | record current quality signals | root `6207`; runtime `5182`; runtime/native `5146` | pass | Post-task quality snapshots. |
| Root, runtime, and runtime/native `mcp__sentrux.check_rules` | rules result recorded honestly | all three rule files missing | debt | Missing rules are not compliance. |
| `git diff --check` | pass | passed with no output | `0` | Final whitespace gate after docs edits. |

Skipped by instruction: staging, commit, `Makefile`, CI, scripts, Sentrux rule
files, STATS, public ABI, test-file edits, compiler edits, and Sentrux rule
creation.

### Follow-Ups And Blockers

| Item | Blocks next task? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Missing Sentrux rules | No for Task 9 implementation; yes for claiming rule compliance. | Dedicated Sentrux rules task or later Epic 2 closeout deferral. | Root and runtime `check_rules` still report missing rule files. |
| Persistent fd registry | Yes, if attempted in Task 9 follow-up. | Later local fd-registry epic. | Task 9 preserved rebuild-from-waiters semantics. |
| Accept ownership changes | Yes, if attempted in Task 9 follow-up. | Later accept-ownership/local fd-registry work. | Task 9 did not change accept ownership. |
| `rt_net.c` file size | No for this task. | Later runtime split/refactor task. | The file was already over the Runtime V2 size limit; this task kept the diff narrow. |

## Task 10: Channel/Blocking Compatibility Tests

### Task Identity And Scope

- Task: Epic 2 Task 10, Channel/Blocking Compatibility Tests.
- Epic: Epic 2, Runtime V2 `N=1` Structure.
- Scope: record channel park/unpark, sync-helper fallback, and native channel
  benchmark evidence before Task 11 moves or wraps channel/blocking
  compatibility counters.
- Runtime files changed: none.
- Test files changed: none.
- Docs changed: `docs/runtime-v2-epics/02-evidence.md`,
  `docs/runtime-v2-epics/NOTES.md`,
  `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`.
- Out of scope and unchanged by design: runtime/native code, Go tests, scripts,
  `Makefile`, CI, Sentrux rules, STATS, public ABI, compiler behavior, staging,
  and commits.

### Completion Boundary

Task 10 is complete with known debt for the narrow Task 11 scope only. Task 11
may move or wrap field ownership for `channel_blocked_workers`,
`compensation_count`, and `compensation_high_water`, and may preserve their
trace-facing accessors.

Task 11 must not change compensation semantics, sync helper behavior, direct
`try_send` or handoff behavior, ready-work draining at the compensation limit,
channel waiter semantics, or channel close/cancellation behavior.

### Go Test Evidence

Stable direct channel evidence passed through the narrowed uncached serial
subset and the CI-contract pair. The CI-contract pair also proved
`TestMTBlockingChannelHelpersAllowTimersToAdvance`.

Direct channel subset:

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(RecvAckHandoffCompletesSenderAfterNonYieldingReceiver|BufferedRecvRefillCompletesSenderAfterNonYieldingReceiver|BufferedBlockingRecvRefillWakesSender|ChannelParkUnpark)$' -v --timeout 120s -count=1 -parallel=1 -p=1
```

Result: passed. Package time was `7.882s`; all four exact tests ran and passed.

CI-contract channel/blocking pair:

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance)$' -v --timeout 120s -count=1 -parallel=1 -p=1
```

Result: passed. Package time was `4.390s`; both exact tests ran and passed.

Broader sync fallback local-only probe:

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTBlockingChannelHelpers(DoNotParkWorkers|AllowTimersToAdvance|DrainReadyWorkAtCompensationLimit)$' -v --timeout 120s -count=1 -parallel=1 -p=1
```

Result: failed. `TestMTBlockingChannelHelpersAllowTimersToAdvance` passed, but
`TestMTBlockingChannelHelpersDoNotParkWorkers` and
`TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` timed out at
their internal 10-second program timeout. These failures are recorded as known
debt below and are not a green gate for the narrow Task 11 counter-field move.

### Native Channel Benchmark Evidence

Temporary compiler build and commit verification:

```bash
tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/surge-task10.XXXXXX")"
current_commit="$(git rev-parse --short=12 HEAD)"
go build -ldflags "$(./scripts/ldflags.sh --local)" -o "$tmpdir/surge" ./cmd/surge/
reported_commit="$("$tmpdir/surge" version --full --format json | python3 -c 'import json,sys; print(json.load(sys.stdin).get("git_commit", ""))')"
test "$reported_commit" = "$current_commit"
```

Result:

```text
tmpdir=/tmp/surge-task10.nOjRbh
surge=/tmp/surge-task10.nOjRbh/surge
current_commit=8ef946f6cc9e
reported_commit=8ef946f6cc9e
```

Native channel before-benchmark:

```bash
SURGE_CHANNEL_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task10-native-channel-before.md" \
  timeout 120s env SURGE="/tmp/surge-task10.nOjRbh/surge" ./scripts/bench_native_channels.sh
```

Result: passed. Report path:
`/home/zov/projects/surge/surge/build/benchmarks/runtime-v2-task10-native-channel-before.md`.
The report is a local ignored artifact under `build/`; the durable evidence is
the selected rows copied below.

Trace-field inspection:

```bash
awk '
  /^## Runtime Trace/ { in_trace=1; next }
  /^## Notes/ { in_trace=0 }
  in_trace && /^\| (1|2|4|8|default) \| channel_/ {
    rows++
    if ($0 ~ /n\/a/) bad++
    if ($0 !~ /\| [0-9]+ \| [0-9]+ \| [0-9]+ \| [0-9]+ \| [0-9]+ \| [0-9]+ \|$/) malformed++
  }
  END {
    printf "runtime_trace_rows=%d\nbad_n_a=%d\nmalformed_rows=%d\n", rows, bad+0, malformed+0
    exit ((rows == 20 && bad == 0 && malformed == 0) ? 0 : 1)
  }
' build/benchmarks/runtime-v2-task10-native-channel-before.md
```

Result:

```text
runtime_trace_rows=20
bad_n_a=0
malformed_rows=0
```

Selected `## Results` rows:

| mode | probe | iterations | total us | ns/op |
| --- | --- | ---: | ---: | ---: |
| 1 | channel_ping_pong | 20000 | 91748 | 4587 |
| 1 | channel_reused_reply | 20000 | 73368 | 3668 |
| 1 | channel_new_reply | 20000 | 81706 | 4085 |
| 1 | channel_sync_new_reply | 5000 | 46354 | 9270 |
| 2 | channel_reused_reply | 20000 | 192601 | 9630 |
| 2 | channel_sync_new_reply | 5000 | 404783 | 80956 |
| 4 | channel_reused_reply | 20000 | 186883 | 9344 |
| 4 | channel_sync_new_reply | 5000 | 660429 | 132085 |
| 8 | channel_reused_reply | 20000 | 189834 | 9491 |
| 8 | channel_sync_new_reply | 5000 | 1136165 | 227233 |
| default | channel_ping_pong | 20000 | 218049 | 10902 |
| default | channel_reused_reply | 20000 | 198665 | 9933 |
| default | channel_new_reply | 20000 | 226221 | 11311 |
| default | channel_sync_new_reply | 5000 | 3724836 | 744967 |

Selected `## Runtime Trace` rows:

| mode | probe | channel blocking waits | task-context blocking sends | task-context blocking recvs | handoff yields | compensation started | compensation high-water |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | channel_ping_pong | 0 | 0 | 0 | 0 | 0 | 0 |
| 1 | channel_reused_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 1 | channel_new_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 1 | channel_sync_new_reply | 0 | 5000 | 5000 | 0 | 0 | 0 |
| 2 | channel_ping_pong | 0 | 0 | 0 | 0 | 0 | 0 |
| 2 | channel_reused_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 2 | channel_new_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 2 | channel_sync_new_reply | 8982 | 5000 | 5000 | 0 | 0 | 0 |
| 4 | channel_ping_pong | 0 | 0 | 0 | 1 | 0 | 0 |
| 4 | channel_reused_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 4 | channel_new_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 4 | channel_sync_new_reply | 9083 | 5000 | 5000 | 0 | 0 | 0 |
| 8 | channel_ping_pong | 0 | 0 | 0 | 1 | 0 | 0 |
| 8 | channel_reused_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 8 | channel_new_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 8 | channel_sync_new_reply | 9667 | 5000 | 5000 | 0 | 0 | 0 |
| default | channel_ping_pong | 0 | 0 | 0 | 1 | 0 | 0 |
| default | channel_reused_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| default | channel_new_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| default | channel_sync_new_reply | 9521 | 5000 | 5000 | 0 | 0 | 0 |

Across the full 20-row Runtime Trace table, all required channel/fallback fields
were present and non-`n/a`. `channel_reused_reply` and `channel_new_reply`
kept task-context blocking sends, task-context blocking recvs, channel blocking
waits, compensation started, and compensation high-water at `0`, while handoff
yields stayed `19999`. `channel_sync_new_reply` recorded `5000`
task-context blocking sends and `5000` task-context blocking recvs in every
mode; channel blocking waits were `0` for mode `1` and nonzero for multi-worker
or default modes. Compensation started and compensation high-water stayed `0`
for every benchmark row.

### Known Evidence Debt

| Item | Blocks Task 11? | Future owner | Reason |
| --- | --- | --- | --- |
| `TestMTNonYieldingTrySendHandoffWakesReceiver` times out when run alone at `SURGE_MT_TIMEOUT_SCALE=1` and `3`. | Only if Task 11 changes direct `try_send`, handoff placement, or wake-before-park behavior. | Future direct channel handoff / `try_send` semantics task. | Task 10 did not debug this failure and did not make it a green gate. |
| `TestMTBlockingChannelHelpersDoNotParkWorkers` times out when run alone at `SURGE_MT_TIMEOUT_SCALE=1` and `3`. | Only if Task 11 changes sync helper wait semantics or compensation start policy. | Future sync-helper compensation/liveness task. | The narrower timer-progress fallback test passed, and the benchmark recorded fallback counters. |
| `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` times out when run alone at `SURGE_MT_TIMEOUT_SCALE=1` and `3`. | Only if Task 11 changes ready queue draining, compensation limits, or worker progress rules. | Future compensation-limit and ready-drain task. | Task 10 records this as stress-test debt, not as authorization for semantic movement. |

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `git status --short --branch` before benchmark/docs | known branch and no unrelated dirty files | `## codex/runtime-net-scheduler-refactor...origin/codex/runtime-net-scheduler-refactor [ahead 16]` | `0` | Started without listed file changes. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(RecvAckHandoffCompletesSenderAfterNonYieldingReceiver\|BufferedRecvRefillCompletesSenderAfterNonYieldingReceiver\|BufferedBlockingRecvRefillWakesSender\|ChannelParkUnpark)$' -v --timeout 120s -count=1 -parallel=1 -p=1` | pass | passed; package time `7.882s` | `0` | Stable direct channel subset. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(ChannelParkUnpark\|BlockingChannelHelpersAllowTimersToAdvance)$' -v --timeout 120s -count=1 -parallel=1 -p=1` | pass | passed; package time `4.390s` | `0` | CI-contract channel/blocking pair. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTBlockingChannelHelpers(DoNotParkWorkers\|AllowTimersToAdvance\|DrainReadyWorkAtCompensationLimit)$' -v --timeout 120s -count=1 -parallel=1 -p=1` | record broader fallback status | failed; `AllowTimersToAdvance` passed, the two stress tests timed out internally | `1` | Known debt for future semantic owners. |
| Temporary compiler build and commit verification block above | reported commit matches current `HEAD` | `current_commit=8ef946f6cc9e`, `reported_commit=8ef946f6cc9e` | `0` | Benchmark used `/tmp/surge-task10.nOjRbh/surge`. |
| `SURGE_CHANNEL_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task10-native-channel-before.md" timeout 120s env SURGE="/tmp/surge-task10.nOjRbh/surge" ./scripts/bench_native_channels.sh` | pass and write report | passed; report path printed | `0` | Manual benchmark evidence with current-checkout compiler. |
| Runtime Trace field inspection | 20 runtime trace rows, no `n/a`, no malformed rows | `runtime_trace_rows=20`, `bad_n_a=0`, `malformed_rows=0` | `0` | Required channel/fallback trace fields were present. |

Skipped by instruction: staging, commit, runtime/native edits, Go test edits,
scripts, `Makefile`, CI, Sentrux rules, STATS, public ABI, compiler edits, and
new tests.

### Follow-Ups And Blockers

| Item | Blocks next task? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Narrow Task 11 counter-field migration | No, if Task 11 only moves or wraps `channel_blocked_workers`, `compensation_count`, and `compensation_high_water`. | Epic 2 Task 11. | Current direct subset, CI-contract pair, and benchmark trace baseline exist. |
| Direct `try_send` or handoff semantic changes | Yes. | Future direct channel handoff / `try_send` task. | `TestMTNonYieldingTrySendHandoffWakesReceiver` is known failing debt. |
| Sync helper semantics or compensation start policy changes | Yes. | Future sync-helper compensation/liveness task. | `TestMTBlockingChannelHelpersDoNotParkWorkers` is known failing debt. |
| Compensation-limit ready-work draining changes | Yes. | Future compensation-limit and ready-drain task. | `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` is known failing debt. |
| Channel waiter ownership, close/cancellation races, or waiter cleanup | Yes. | Later local channel-waiter epic. | Task 10 does not add or prove the missing race matrix. |
| CI promotion for local-only channel probes | No for Task 11; yes before CI wiring. | Epic 2 Task 12 if promoted. | Task 10 keeps the heavier probes local-only/debt-bound. |

## Task 11: Channel/Blocking Compatibility Migration

### Task Identity And Scope

- Task: Epic 2 Task 11, Channel/Blocking Compatibility Migration.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: move ownership of `channel_blocked_workers`,
  `compensation_count`, and `compensation_high_water` under the single shard
  while preserving trace-facing reads and existing counter arithmetic.
- Out of scope and unchanged by design: direct async channel protocol,
  `try_send`, handoff placement, sync-helper wait behavior, compensation
  policy, ready-work draining at the compensation limit, channel waiter
  semantics, channel close/cancellation behavior, blocking-pool
  queue/lifecycle, public ABI, Go tests, scripts, `Makefile`, CI, Sentrux
  rules, STATS, and compiler code.
- Proving spike: `no`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `runtime/native/rt_async_internal.h` | Added `rt_channel_blocking_compat`, placed it under `rt_shard`, removed the three old fields from `rt_executor`, and declared shard/executor accessors. | Make compatibility counter ownership explicit under the `N=1` shard. | `460` lines after change; still below 500. |
| `runtime/native/rt_runtime.c` | Added `rt_shard_channel_blocking_compat*` and `rt_executor_channel_blocking_compat*` accessors. | Mirror the scheduler and net scratch accessor shape. | `140` lines after change. |
| `runtime/native/rt_async_state.c` | Replaced direct counter reads/writes with compatibility accessor reads/writes in trace snapshot, ready wake compensation checks, compensation startup, and sync-helper worker parking. | Preserve the old operations while changing the owner. | `2431` lines after change; already over limit. No split in this task. |
| `docs/runtime-v2-epics/02-evidence.md` | Added this evidence section and marked Task 11 complete in the index. | Keep Task 11 evidence current. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | Added Task 11 handoff. | Make the next task startable without chat context. | Documentation only. |

`runtime/native/rt_async_channel.c` was not edited.

### Implementation Notes

The new owner is:

```c
typedef struct {
    uint32_t channel_blocked_workers;
    uint32_t compensation_count;
    uint32_t compensation_high_water;
} rt_channel_blocking_compat;
```

`struct rt_shard` owns it as `channel_blocking_compat`. Callers use
`rt_executor_channel_blocking_compat()` or
`rt_executor_channel_blocking_compat_const()` in the same style as existing
scheduler and net scratch accessors. The migration does not add semantic
wrapper helpers: increments, decrements, limits, high-water updates, and trace
snapshot fields remain visible at the old call sites.

### Static Audits

| Audit | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `sed -n '/struct rt_executor {/,/^};/p' runtime/native/rt_async_internal.h \| rg -n 'channel_blocked_workers\|compensation_count\|compensation_high_water' \|\| true` | no old executor fields | no output | `0` | Old executor ownership is gone. |
| `sed -n '/struct rt_shard {/,/^};/p' runtime/native/rt_async_internal.h \| rg -n 'rt_channel_blocking_compat channel_blocking_compat' \|\| true` | one shard owner line | `6:    rt_channel_blocking_compat channel_blocking_compat;` | `0` | Compatibility counters live under the single shard. |
| `rg -n -- 'ex->(channel_blocked_workers\|compensation_count\|compensation_high_water)\b\|exec_state\.(channel_blocked_workers\|compensation_count\|compensation_high_water)\b' runtime/native \|\| true` | no direct old executor usage | no output | `0` | Direct old field users are gone. |
| `rg -n -- 'rt_(executor\|shard)_channel_blocking_compat\|rt_channel_blocking_compat' runtime/native` | header, runtime accessors, and `rt_async_state.c` users only | found `rt_async_internal.h`, `rt_runtime.c`, and `rt_async_state.c` only | `0` | New access surface is narrow. |
| `git diff -- runtime/native/rt_async_channel.c runtime/native/rt_async_blocking.c runtime/native/rt.h internal/vm scripts Makefile .github STATS.md` | no forbidden-surface diff | no output | `0` | Channel protocol, blocking pool, ABI, tests, scripts, CI, STATS, and compiler surfaces stayed untouched. |
| `git diff --check` | no whitespace errors | no output | `0` | Diff hygiene passed. |

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `make c-check` | pass | first run failed formatting only in the three edited runtime files | `2` | Fixed with `clang-format -i runtime/native/rt_async_internal.h runtime/native/rt_runtime.c runtime/native/rt_async_state.c`. |
| `make c-check` | pass | C formatting OK; strict C runtime compilation OK | `0` | Rerun after formatting and after cppcheck const cleanup. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(RecvAckHandoffCompletesSenderAfterNonYieldingReceiver\|BufferedRecvRefillCompletesSenderAfterNonYieldingReceiver\|BufferedBlockingRecvRefillWakesSender\|ChannelParkUnpark)$' -v --timeout 120s -count=1 -parallel=1 -p=1` | pass | passed; package time `8.377s` | `0` | Stable direct channel subset. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMT(ChannelParkUnpark\|BlockingChannelHelpersAllowTimersToAdvance)$' -v --timeout 120s -count=1 -parallel=1 -p=1` | pass | passed after code commit; package time `4.766s` | `0` | CI-contract channel/blocking pair. |
| Temporary compiler build and commit verification block | reported commit matches current `HEAD` | `current_commit=ec640a47b449`, `reported_commit=ec640a47b449`; binary `/tmp/surge-task11-final.86ZWJ8/surge` | `0` | Benchmark used the committed Task 11 code. |
| `SURGE_CHANNEL_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task11-native-channel-after.md" timeout 120s env SURGE="/tmp/surge-task11-final.86ZWJ8/surge" ./scripts/bench_native_channels.sh` | pass and write report | passed; report path printed | `0` | Manual benchmark evidence with current-checkout compiler. |
| Runtime Trace field inspection | 20 runtime trace rows and no `n/a` | `runtime_trace_rows=20`, `contains_na=0` | `0` | Required channel/fallback trace fields were present. |
| `make cppcheck` | pass | first run reported two `constVariablePointer` style findings in new read-only compat pointers | `2` | Fixed by switching those reads to the const accessor. |
| `make cppcheck` | pass | `cppcheck OK` | `0` | Rerun after const cleanup. |
| `make check` | pass | passed; ran `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 90s`, `golangci-lint`, `make c-check`, and `check_file_sizes.sh` | `0` | Full local gate. |

Known-debt tests deliberately not run for Task 11:

- `TestMTNonYieldingTrySendHandoffWakesReceiver`.
- `TestMTBlockingChannelHelpersDoNotParkWorkers`.
- `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit`.

These remain future-owner debt because this task did not change direct
`try_send`/handoff behavior, sync-helper semantics, compensation start policy,
or compensation-limit ready-drain behavior.

### Native Channel Benchmark

Report:
`/home/zov/projects/surge/surge/build/benchmarks/runtime-v2-task11-native-channel-after.md`.

Selected `## Runtime Trace` rows:

| mode | probe | channel blocking waits | task-context blocking sends | task-context blocking recvs | handoff yields | compensation started | compensation high-water |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | channel_reused_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 1 | channel_new_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 1 | channel_sync_new_reply | 0 | 5000 | 5000 | 0 | 0 | 0 |
| 2 | channel_reused_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 2 | channel_new_reply | 0 | 0 | 0 | 19999 | 0 | 0 |
| 2 | channel_sync_new_reply | 9160 | 5000 | 5000 | 0 | 0 | 0 |
| 4 | channel_sync_new_reply | 9010 | 5000 | 5000 | 0 | 0 | 0 |
| 8 | channel_sync_new_reply | 9651 | 5000 | 5000 | 0 | 0 | 0 |
| default | channel_sync_new_reply | 9526 | 5000 | 5000 | 0 | 0 | 0 |

Across the full 20-row Runtime Trace table, all required channel/fallback
fields were present and non-`n/a`. `channel_reused_reply` and
`channel_new_reply` kept task-context blocking sends, task-context blocking
recvs, channel blocking waits, compensation started, and compensation
high-water at `0`, while handoff yields stayed `19999`. `channel_sync_new_reply`
recorded `5000` task-context blocking sends and `5000` task-context blocking
recvs in every mode. Compensation started and compensation high-water stayed
`0` for every benchmark row.

### Sentrux Evidence

Main-session runtime/native `session_end` passed against the pre-task baseline:
`signal_before=5146`, `signal_after=5172`, `signal_delta=26`, no violations,
summary `Quality stable or improved`.

| Scan | Path | quality_signal | Root cause or bottleneck | Rules result |
| --- | --- | --- | --- | --- |
| Root | `/home/zov/projects/surge/surge` | `6207` | bottleneck `modularity`; root causes: acyclicity `10000`, depth `6667`, equality `4685`, modularity `3438`, redundancy `8576`; files `4744`; import edges `1888`; lines `373289` | missing `/home/zov/projects/surge/surge/.sentrux/rules.toml`; debt, not compliance. |
| Runtime | `/home/zov/projects/surge/surge/runtime` | `5209` | bottleneck `redundancy`; root causes: acyclicity `10000`, depth `8889`, equality `4783`, modularity `3333`, redundancy `2705`; files `33`; import edges `31`; lines `15125` | missing `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`; debt, not compliance. |
| Runtime/native | `/home/zov/projects/surge/surge/runtime/native` | `5172` | bottleneck `redundancy`; root causes: acyclicity `10000`, depth `8889`, equality `4781`, modularity `3215`, redundancy `2708`; files `32`; import edges `31`; lines `15110` | missing `/home/zov/projects/surge/surge/runtime/native/.sentrux/rules.toml`; debt, not compliance. |

### Follow-Ups And Blockers

| Item | Blocks Task 12? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Known direct non-yielding `try_send` handoff timeout | No. | Future direct channel handoff / `try_send` task. | Task 11 did not touch direct channel handoff or `try_send`. |
| Known sync-helper compensation/liveness timeout | No. | Future sync-helper compensation/liveness task. | Task 11 did not alter sync-helper wait semantics or compensation start policy. |
| Known compensation-limit ready-drain timeout | No. | Future compensation-limit and ready-drain task. | Task 11 did not alter compensation limits or ready-work draining. |
| Missing Sentrux rules | No for this implementation; yes for claiming rule compliance. | Dedicated Sentrux rules task or later closeout. | All scanned paths still lack `.sentrux/rules.toml`. |

## Task 12: CI Runtime V2 Gates

### Task Identity And Scope

- Task: Epic 2 Task 12, CI Runtime V2 Gates.
- Date: 2026-06-26.
- Author/session: Codex.
- Scope: add an explicit Runtime V2 liveness gate to `Makefile` and GitHub
  Actions so timeout-sensitive seed tests run with `SURGE_SKIP_TIMEOUT_TESTS=0`.
- Out of scope and unchanged by design: runtime/native code, Go tests,
  compiler/backend code, scripts, STATS, the default `make check` path, and the
  broad accepted-debt VM/backend regex.
- Proving spike: `no`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `Makefile` | Added `.PHONY` entry, `SURGE_MT_TIMEOUT_SCALE ?= 3`, and `runtime-v2-check` target with `clang`/`ar` preflight and the exact Runtime V2 seed command. | Provide a stable local and CI entrypoint for timeout-sensitive liveness tests. | Existing file; default `check` target unchanged. |
| `.github/workflows/ci.yml` | Added separate `runtime-v2-check` job outside the Go matrix; installs `clang`, `llvm`, `lld`, and `binutils`; sets `SURGE_MT_TIMEOUT_SCALE=3`; runs `make runtime-v2-check`. | Make Runtime V2 liveness visible in CI without changing the skipped-timeout Go matrix. | Workflow only. |
| `docs/runtime-v2-epics/02-ci-test-contract.md` | Updated status and CI package list to match Task 12 implementation. | Keep the contract aligned with the actual gate. | Documentation only. |
| `docs/runtime-v2-epics/02-evidence.md` | Marked Task 12 complete and added this evidence section. | Record the target, job, local checks, and exclusions. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | Added Task 12 handoff. | Make Task 13 startable without chat context. | Documentation only. |
| `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md` | Updated status wording from Tasks 1-11 to Tasks 1-12. | Keep the epic overview aligned with recorded evidence. | Documentation only. |

### Gate Shape

`make runtime-v2-check` fails before `go test` if either required tool is
missing:

```bash
command -v clang
command -v ar
```

The target then runs the stable seed with timeout-sensitive tests enabled:

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 SURGE_MT_TIMEOUT_SCALE=3 \
  go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -count=1 -parallel=1 -p=1 -v --timeout 120s
```

The target defaults `SURGE_MT_TIMEOUT_SCALE` to `3`, and the GitHub Actions job
also keeps `SURGE_MT_TIMEOUT_SCALE=3` scoped to the Runtime V2 gate. The default
`make check` path remains unchanged and still runs the broad repository tests
with `SURGE_SKIP_TIMEOUT_TESTS=1`.

### Commands/Checks

| Command | Expected result | Actual result | Exit/status | Note |
| --- | --- | --- | --- | --- |
| `command -v clang` | tool exists | `/usr/bin/clang` | `0` | Local preflight tool. |
| `command -v ar` | tool exists | `/usr/bin/ar` | `0` | Local preflight tool; CI installs `binutils`. |
| Initial main-session `make runtime-v2-check` verification before scale/serialization tightening | reveal local gate risk | failed in `TestMTBlockingChannelHelpersAllowTimersToAdvance` with its internal `program timeout after 10s` while the other three seed tests passed | `2` | The first target version relied on the caller/CI environment for timeout scale and did not serialize the seed. The target was tightened before commit. |
| `make runtime-v2-check` | pass | all four exact tests ran and passed; package time `7.427s` | `0` | Ran with `SURGE_BACKEND=llvm`, `SURGE_SKIP_TIMEOUT_TESTS=0`, `SURGE_MT_TIMEOUT_SCALE=3`, `-count=1`, `-parallel=1`, and `-p=1`; tests: `TestMTWakeupsAndCancellation`, `TestMTChannelParkUnpark`, `TestMTBlockingChannelHelpersAllowTimersToAdvance`, `TestMTSeededScheduler`. |
| `make check` | pass | passed; ran `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 90s`, `golangci-lint` with `0 issues`, nested `make c-check`, and `check_file_sizes.sh` with no applicable unstaged files | `0` | Default repository gate remains skipped-timeout by design. |

### Explicit Exclusions

The broad focused VM command remains accepted backend-test debt and was not
added as a CI or Makefile green gate:

```bash
go test ./internal/vm -run 'MT|Async|Net|LLVM'
```

Task 12 did not promote `TestMTWorkStealing`,
`TestMTNetWaiterWakeupLatency`, `TestRuntimeV2SkeletonStaticShape`, or the
known heavier channel/blocking stress probes. Their existing owner/debt records
remain unchanged.

### Sentrux Evidence

| Scan | Path | quality_signal | Root cause or bottleneck | Rules result |
| --- | --- | --- | --- | --- |
| Root | `/home/zov/projects/surge/surge` | `6207` | bottleneck `modularity`; root causes: acyclicity `10000`, depth `6667`, equality `4685`, modularity `3438`, redundancy `8576`; files `4744`; import edges `1888`; lines `373627` | missing `/home/zov/projects/surge/surge/.sentrux/rules.toml`; debt, not compliance. |
| Runtime | `/home/zov/projects/surge/surge/runtime` | `5209` | bottleneck `redundancy`; root causes: acyclicity `10000`, depth `8889`, equality `4783`, modularity `3333`, redundancy `2705`; files `33`; import edges `31`; lines `15125` | missing `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`; debt, not compliance. |

### Follow-Ups And Blockers

| Item | Blocks Task 13? | Owner or next document | Reason |
| --- | --- | --- | --- |
| CI required-check configuration | No for this repo change. | Repository settings or branch protection owner. | The workflow job exists, but branch protection may need to require the new job name. |
| Broad VM/backend regex debt | No. | Later test/backend matrix epic. | It remains excluded from required green gates. |
| Missing Sentrux rules | No for this CI/docs task; yes for claiming rule compliance. | Dedicated Sentrux rules task or later closeout. | Root/runtime scans were recorded; both scan roots still lack rules files. |
