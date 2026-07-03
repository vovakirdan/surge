# Epic 6 Evidence: N>1 Accept Ownership And Tier-1 Scheduler Boundary

This file records task evidence for Epic 6. Keep durable conclusions here and
keep `NOTES.md` as the live handoff log.

## Task Status

| Task | Status | Evidence |
| --- | --- | --- |
| 1 | Complete | Starting state, structural facts, accepted debt, line counts, Sentrux scans, current gates, and Task 2-5 gate plan recorded below. |
| 2 | Complete | Accept/readiness/close/cancellation/shutdown dependency map recorded in `06-accept-ownership-dependency-map.md`; no unowned gap found. |
| 3 | Pending | Listener model proving spike. |
| 4 | Pending | Multishard accept contract tests. |
| 5 | Pending | Multishard static shape tests. |
| 6 | Pending | Runtime shard array and config. |
| 7 | Pending | Per-shard scheduler placement. |
| 8 | Pending | Listener and connection owner metadata. |
| 9 | Pending | Accept distribution implementation. |
| 10 | Pending | Per-shard poller and wake ownership. |
| 11 | Pending | Multishard net lifecycle migration. |
| 12 | Pending | Trace counters and benchmark evidence. |
| 13 | Pending | Runtime V2 accept CI gates. |
| 14 | Pending | Large-file refactor tranche. |
| 15 | Pending | Epic closeout and static gates. |

## Task 1: Kickoff Baseline And Sentrux

### Task Identity And Scope

- Task: `06-tasks/01-kickoff-baseline-and-sentrux.md`.
- Epic: `06-n2-accept-ownership-and-tier1-scheduler.md`.
- Date: 2026-07-03.
- Author/session: Codex main session.
- Scope: docs-only kickoff evidence before Epic 6 dependency mapping, proving
  spike, tests, and runtime implementation.
- Out of scope: runtime code, task documents, CI, behavior tests, dependency
  map detail, listener spike work, benchmark changes, and commits.
- Proving spike: no.

### Baseline Commit/Status

- Baseline commit: `9e1de4a0f372d4c30129d5c5ea49ca499ae4f8e9`.
- Baseline commit summary: `9e1de4a0 docs(runtime): expand epic 6 tasks`.
- Branch/worktree: `codex/runtime-net-scheduler-refactor`, ahead of
  `origin/codex/runtime-net-scheduler-refactor` by 2 commits.
- Status before Task 1 edits: clean; `git status --short` printed no output.
- Status after Task 1 docs edit: `git status --short` shows only
  `M docs/runtime-v2-epics/NOTES.md` and
  `?? docs/runtime-v2-epics/06-evidence.md`.
- Dirty or untracked files not touched at start: none.
- Local environment blockers: none for Sentrux, `make runtime-v2-check`, or
  `make check`; the first `make runtime-v2-check` attempt timed out in a
  known timeout-sensitive MT class, then an immediate rerun passed and is
  recorded below as the current Epic 6 baseline gate status.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/06-evidence.md` | created | Record Epic 6 Task 1 baseline and gates. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 1 handoff for Task 2 start. | Documentation only. |

No runtime code, task documents, test files, `Makefile`, CI files, or Sentrux
rule files were changed.

### Baseline Line Counts

Command:
`wc -l runtime/native/rt_async_internal.h runtime/native/rt_runtime.c runtime/native/rt_net.c runtime/native/rt_fd_registry.h runtime/native/rt_fd_registry.c runtime/native/rt_async_state.c runtime/native/rt_async_task.c internal/vm/runtime_v2_skeleton_static_test.go internal/vm/runtime_v2_fd_registry_static_test.go internal/vm/mt_*_test.go .loc-legacy-allowlist`

| Path | Lines | Status |
| --- | ---: | --- |
| `runtime/native/rt_async_internal.h` | 499 | under 500-line hard target by 1 line. |
| `runtime/native/rt_runtime.c` | 202 | under target. |
| `runtime/native/rt_net.c` | 904 | over target; accepted debt `RV2-DEBT-004`, allowlisted at 904. |
| `runtime/native/rt_fd_registry.h` | 113 | under target. |
| `runtime/native/rt_fd_registry.c` | 409 | under target. |
| `runtime/native/rt_async_state.c` | 1727 | over target; accepted debt `RV2-DEBT-003`, allowlisted at 1727. |
| `runtime/native/rt_async_task.c` | 768 | over target; accepted debt `RV2-DEBT-005`, allowlisted at 768. |
| `internal/vm/runtime_v2_skeleton_static_test.go` | 61 | under target. |
| `internal/vm/runtime_v2_fd_registry_static_test.go` | 426 | under target. |
| `internal/vm/mt_correctness_test.go` | 1351 | existing large VM test file. |
| `internal/vm/mt_executor_test.go` | 1511 | existing large VM test file. |
| `.loc-legacy-allowlist` | 15 | allowlist file. |

Command: `wc -l internal/vm/runtime_v2_*_test.go internal/vm/mt_*_test.go`

| Path | Lines |
| --- | ---: |
| `internal/vm/runtime_v2_fd_registry_contract_test.go` | 499 |
| `internal/vm/runtime_v2_fd_registry_lifecycle_test.go` | 297 |
| `internal/vm/runtime_v2_fd_registry_shutdown_static_test.go` | 303 |
| `internal/vm/runtime_v2_fd_registry_static_test.go` | 426 |
| `internal/vm/runtime_v2_fd_registry_wake_test.go` | 446 |
| `internal/vm/runtime_v2_heap_accounting_contract_test.go` | 275 |
| `internal/vm/runtime_v2_heap_accounting_static_test.go` | 334 |
| `internal/vm/runtime_v2_net_waiter_contract_test.go` | 265 |
| `internal/vm/runtime_v2_owner_local_waiter_static_test.go` | 53 |
| `internal/vm/runtime_v2_skeleton_static_test.go` | 61 |
| `internal/vm/runtime_v2_task_scope_blocking_waiter_contract_test.go` | 242 |
| `internal/vm/runtime_v2_waiter_contract_test.go` | 345 |
| `internal/vm/runtime_v2_waiter_static_test.go` | 86 |
| `internal/vm/mt_correctness_test.go` | 1351 |
| `internal/vm/mt_executor_test.go` | 1511 |

Current `.loc-legacy-allowlist` entries relevant to this epic:

| Path | Ceiling | Debt |
| --- | ---: | --- |
| `runtime/native/rt_async_state.c` | 1727 | `RV2-DEBT-003`. |
| `runtime/native/rt_net.c` | 904 | `RV2-DEBT-004`. |
| `runtime/native/rt_async_task.c` | 768 | `RV2-DEBT-005`. |

### Confirmed Starting Structural Facts

| Fact | Current evidence | Task impact |
| --- | --- | --- |
| Runtime shard count is compile-time `N=1`. | `runtime/native/rt_async_internal.h:127` defines `RT_RUNTIME_SHARD_COUNT 1U`. | Task 6 must replace the fixed count with the Epic 6 dynamic bounded shape. |
| `rt_shard` already owns the per-shard containers needed by this epic. | `rt_async_internal.h:150-160` has `runtime`, `executor`, `scheduler`, `heap_accounting`, `net_poll_scratch`, `fd_registry`, `channel_blocking_compat`, `waiter_store`, and `shard_id`. | Task 6 should initialize existing containers for shards beyond 0 rather than invent parallel storage. |
| `rt_runtime` is still a runtime `shard_count` plus a fixed one-element array. | `rt_async_internal.h:162-165`: `size_t shard_count; rt_shard shards[RT_RUNTIME_SHARD_COUNT];`. | Preferred target remains `RT_RUNTIME_MAX_SHARDS` plus runtime `shard_count`. |
| `rt_task` has no shard or owner field. | `rt_async_internal.h:167-202` lists task state, status, wait keys, timers, and children, but no shard/owner metadata. | Task 7 owns placement metadata. |
| `rt_executor` remains global. | `rt_async_internal.h:216-247` has one `tasks[]`, `scopes[]`, `pthread_mutex_t lock`, `workers`, `net_polling`, blocking pool, and shutdown state. | Epic 6 keeps this lock/global primitive boundary by design. |
| Runtime initialization only populates shard 0. | `runtime/native/rt_runtime.c:19-39` initializes `runtime->shards[0]` and fd registry for shard 0; `rt_runtime_init_global_n1` calls it at `:42-43`. | Task 6 owns structural multi-shard initialization. |
| `rt_runtime_shard0()` is the compatibility resolver. | `rt_runtime.c:50-55`; accessors for scheduler, net poll scratch, channel compat, waiter store, and fd registry route through it at `:79-162`. | Task 5/6 must draw the net-owned vs stays-global line carefully. |
| `rt_shard_scheduler_init` is already parameterized by worker count. | `rt_runtime.c:165-187`. | Task 6 can initialize scheduler containers per shard; Task 7 owns OS worker placement. |
| Net handles have no owner metadata. | `runtime/native/rt_net.c:45-53` defines `NetListener{int fd; bool closed;}` and `NetConn{int fd; bool closed;}`. | Task 8 owns owner-shard metadata and the `RV2-DEBT-010` decision. |
| Wake pipe is process-global. | `rt_net.c:67-68` static read/write fds; init/write/drain at `:93-129`; poll uses it at `:824-879`. | Task 10 owns per-shard poller/wake state, without Phase 4 eventfd/inbound protocol. |
| `rt_net_listen` uses `SO_REUSEADDR`, not `SO_REUSEPORT`. | `rt_net.c:413-469`; `setsockopt(... SO_REUSEADDR ...)` at `:435`; no `SO_REUSEPORT` match in the source sweep. | Task 3 proves the listener model before Task 9 implementation. |
| FD registry storage is shard-scoped, but callers still resolve shard 0. | `rt_async_internal.h:156`, `rt_runtime.c:144-162`, and `rt_fd_registry.c:294` via `rt_executor_fd_registry_const(ex)`. | Task 2 maps every net-owned caller; Task 11 migrates lifecycle paths. |
| `SURGE_THREADS` is the only current worker-count env knob. | `runtime/native/rt_async_state.c:109-110`; `exec_init_once` uses it at `:201`. | Task 6 defines `SURGE_SHARDS` interaction and conflict handling. |
| Existing Runtime V2 static gates pin `N=1`. | `runtime_v2_skeleton_static_test.go:22-27,34`; `runtime_v2_fd_registry_static_test.go:390-395`. | Task 5 updates the contract before Task 6 changes the macro. |

### Accepted Baseline Debt Scope

| Debt | Status for Epic 6 |
| --- | --- |
| `RV2-DEBT-001` broad focused VM/backend command fails when timeout-sensitive paths are not skipped. | Accepted baseline debt; do not promote broad regex to a required Epic 6 gate. |
| `RV2-DEBT-002` timeout-sensitive sync-helper tests are excluded from current green gates. | Relevant to the first-attempt `make runtime-v2-check` timeout recorded below; rerun passed, and stabilization remains owned by Epic 11 unless this epic changes the path. |
| `RV2-DEBT-003` `rt_async_state.c` over line target. | Relevant; any task touching it must record line-count outcome. |
| `RV2-DEBT-004` `rt_net.c` over line target. | Relevant; any task touching it must record line-count outcome. |
| `RV2-DEBT-005` other legacy native runtime files over target, including `rt_async_task.c`. | Relevant if Task 3/7 touches spawn/task creation or Task 14 refactors. |
| `RV2-DEBT-006` channel benchmark script timeout ownership. | Not an Epic 6 close condition unless benchmark tooling crosses into this surface. |
| `RV2-DEBT-007` Sentrux thresholds calibrated to legacy ceilings. | Accepted quality-hardening debt; current Sentrux rules still must pass. |
| `RV2-DEBT-010` copied net handles lack generation-aware validation. | Relevant to Task 8; close or keep explicitly with evidence. |
| `RV2-DEBT-011` VM LLVM test artifacts can race under concurrent same-name runs. | Not a Task 1 blocker; avoid overlapping identical VM build tests. |
| `RV2-DEBT-012` generated heap benchmark crashes under heavier serial pressure. | Not an Epic 6 close condition unless heap benchmark/allocation-heavy benchmark surfaces change. |

### Sentrux Root/Scoped Signals

Sentrux MCP tools were visible and used. CLI fallback was not needed. The exact
CLI `sentrux check` commands requested for Task 1 were also run and matched the
MCP quality signals.

| Scan | Active path | When | quality_signal | Root cause or bottleneck | Rules/session result |
| --- | --- | --- | ---: | --- | --- |
| Repository MCP | `/home/zov/projects/surge/surge` | Before Task 1 docs edit | 6190 | bottleneck `modularity`; files 4848, import edges 1898, lines 391509, cross-module edges 1820; root-cause scores: acyclicity 10000, depth 6667, equality 4638, modularity 3472, redundancy 8464 | `check_rules` pass; `rules_checked=8`, `total_rules_defined=12`, `violation_count=0`; output truncated by free-tier limit after checking rules. |
| Runtime MCP | `/home/zov/projects/surge/surge/runtime` | Before Task 1 docs edit | 5279 | bottleneck `redundancy`; files 46, import edges 41, lines 16357, cross-module edges 0; root-cause scores: acyclicity 10000, depth 8889, equality 5013, modularity 3333, redundancy 2761 | `check_rules` pass; `rules_checked=7`, `total_rules_defined=8`, `violation_count=0`; output truncated by free-tier limit after checking rules. |
| Runtime/native MCP | `/home/zov/projects/surge/surge/runtime/native` | Before Task 1 docs edit | 5318 | bottleneck `redundancy`; files 43, import edges 41, lines 16314, cross-module edges 39; root-cause scores: acyclicity 10000, depth 8889, equality 5011, modularity 3456, redundancy 2764 | `check_rules` pass; `rules_checked=7`, `total_rules_defined=7`, `violation_count=0`. |
| Repository CLI | `.` | Before Task 1 docs edit | 6190 | `sentrux check .` scanned 4848 files and checked 10 rules. | pass; `All rules pass`. |
| Runtime CLI | `runtime` | Before Task 1 docs edit | 5279 | `sentrux check runtime` scanned 46 files and checked 7 rules. | pass; `All rules pass`. |
| Runtime/native CLI | `runtime/native` | Before Task 1 docs edit | 5318 | `sentrux check runtime/native` scanned 43 files and checked 7 rules. | pass; `All rules pass`. |

Task 1 did not call `session_start` or `session_end`; this was docs-only
kickoff evidence, not a runtime-code diff. Runtime-code tasks must start and
end Sentrux sessions on the scoped path used for their delta evidence.

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence note |
| --- | --- | --- | --- | --- |
| `git rev-parse HEAD` | record baseline commit | `9e1de4a0f372d4c30129d5c5ea49ca499ae4f8e9` | `0` | matches requested baseline commit. |
| `git status --short` | clean start | no output | `0` | no pre-existing dirty files. |
| `git status -sb` | record branch/ahead state | `## codex/runtime-net-scheduler-refactor...origin/codex/runtime-net-scheduler-refactor [ahead 2]` | `0` | branch identity. |
| `git log --oneline -10` | record recent context | top commit `9e1de4a0 docs(runtime): expand epic 6 tasks`; Epic 5 closeout commit `bc0a76d7` also present | `0` | baseline history. |
| `git diff --check` | no whitespace errors | no output before docs edit | `0` | pre-edit whitespace gate. |
| `git diff --check` | no whitespace errors | no output after docs edit | `0` | final docs diff whitespace gate. |
| `git diff --no-index --check /dev/null docs/runtime-v2-epics/06-evidence.md` | no whitespace errors in new untracked file | no output | `1` expected because files differ | untracked-file whitespace gate. |
| `sentrux check .` | pass | quality `6190`; 10 rules checked; all rules pass | `0` | required CLI root scan. |
| `sentrux check runtime` | pass | quality `5279`; 7 rules checked; all rules pass | `0` | required CLI runtime scan. |
| `sentrux check runtime/native` | pass | quality `5318`; 7 rules checked; all rules pass | `0` | required CLI runtime/native scan. |
| `make runtime-v2-check` | record first attempt | failed in liveness gate: `TestMTBlockingChannelHelpersAllowTimersToAdvance` timed out after 30s; sibling cases `TestMTWakeupsAndCancellation`, `TestMTSeededScheduler`, and `TestMTChannelParkUnpark` passed | `2` | first-attempt timeout; aligns with accepted timeout-sensitive debt class `RV2-DEBT-002`, not fixed in Task 1. |
| `timeout 300s make runtime-v2-check` | pass on rerun | full Runtime V2 chain passed: MT liveness seed, heap check, waiter check, and fd-registry check | `0` | current baseline gate is green on rerun; keep the first timeout as flake evidence. |
| `make check` | pass | `go test ./...` with `SURGE_SKIP_TIMEOUT_TESTS=1` passed, `golangci-lint` reported `0 issues`, `make c-check` passed, file-size checker printed no uncommitted files found | `0` | full project gate is green in standard skip-timeout mode. |
| `git status --short` | Task 1 docs-only changes | `M docs/runtime-v2-epics/NOTES.md`; `?? docs/runtime-v2-epics/06-evidence.md` | `0` | final working-tree scope. |

### Current Runtime V2 Gate Status

`make runtime-v2-check` passed on immediate rerun at Epic 6 kickoff. The first
attempt still recorded a timeout in the MT liveness seed, so this evidence
keeps both facts: the current gate is green on rerun, and the timeout-sensitive
flake class is real baseline debt.

The first failing command inside the target was:

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 SURGE_MT_TIMEOUT_SCALE=3 go test ./internal/vm -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' -count=1 -parallel=1 -p=1 -v --timeout 120s
```

Observed failure:

```text
mt_executor_test.go:1210: program timeout after 30s
artifacts: /home/zov/projects/surge/surge/target/debug/.tests/TestMTBlockingChannelHelpersAllowTimersToAdvance
--- FAIL: TestMTBlockingChannelHelpersAllowTimersToAdvance (32.78s)
FAIL surge/internal/vm 38.714s
make: *** [Makefile:91: runtime-v2-check] Error 1
```

The immediate rerun command was:

```bash
timeout 300s make runtime-v2-check
```

Observed rerun result:

```text
PASS
ok   surge/internal/vm 8.603s    # MT liveness seed
PASS
ok   surge/internal/vm 3.900s    # native heap smoke
PASS
ok   surge/internal/vm 3.838s    # heap accounting contracts
PASS
ok   surge/internal/vm 0.037s    # heap static gate
PASS
ok   surge/internal/vm 0.030s    # waiter static boundary
PASS
ok   surge/internal/vm 19.689s   # waiter liveness contracts
PASS
ok   surge/internal/vm 15.757s   # fd-registry liveness/static gate
```

This is recorded as a current green baseline with a first-attempt flake note,
not as a stable red gate. Task 1 does not change runtime code; the debt ledger
already owns timeout-sensitive MT test stabilization under `RV2-DEBT-002`/
Epic 11 unless this epic changes that path.

### Contracts Touched

| Contract or behavior | Source | Preserved, changed, or N/A | Evidence |
| --- | --- | --- | --- |
| Runtime behavior | Epic 6 Task 1 scope | N/A | No runtime code changed. |
| Public Surge syntax/API | Epic 6 Not Included | N/A | No parser, semantic, lowering, stdlib, or example files changed. |
| Sentrux baseline | `SENTRUX_POLICY.md` | Preserved as baseline evidence | MCP and CLI scans recorded above. |
| Runtime V2 gate status | Task 1 Scope | Recorded, not changed | first `make runtime-v2-check` attempt timed out; immediate `timeout 300s make runtime-v2-check` rerun and `make check` passed. |

### Benchmarks And Generated Reports

| Benchmark | Expected baseline | Actual key rows | Generated report path | Notes |
| --- | --- | --- | --- | --- |
| N/A | No benchmarks required for Task 1. | N/A | N/A | Task 12 owns Epic 6 benchmark evidence. |

### Trace Counters/Liveness Proof

| Probe or counter | Expected result | Actual result | Evidence path | Pass/blocker |
| --- | --- | --- | --- | --- |
| Runtime V2 liveness gate | Record current baseline | first attempt timed out in `TestMTBlockingChannelHelpersAllowTimersToAdvance`; immediate `timeout 300s make runtime-v2-check` rerun passed the full chain | command output above | Current gate green on rerun; accepted timeout-sensitive flake debt remains open. |

### Gate Plan For Tasks 2-5

| Task | Required gates before close |
| --- | --- |
| Task 2, dependency map | Create `06-accept-ownership-dependency-map.md`; run exact `rg -n` sweeps for every mapped symbol; classify net-shard-owned, stays-global-compat, and later-epic rows with `file:line` citations; run `git diff --check`; update `06-evidence.md` and `NOTES.md`. No runtime code or tests. |
| Task 3, listener proving spike | Write `06-listener-model-proving-spike.md` with Global Rule 1 fields before implementation; empirically test `SO_REUSEPORT` distribution and explicit handoff fallback; name internal accept task representation and handler owner-shard placement with no new syntax; record proof commands; delete or quarantine spike code; run `git diff --check`; update `06-evidence.md` and `NOTES.md`. |
| Task 4, accept behavior contracts | Add `internal/vm/runtime_v2_accept_contract_test.go`; keep `SURGE_SHARDS=1` regression subset default-green; gate future behavior with `runtime_v2_pending`; run default `go test ./internal/vm -run 'TestRuntimeV2Accept'`, pending-tag expected-fail subset, selected net liveness probe from `LIVENESS_PROBES.md`, and `git diff --check`; update evidence/notes. |
| Task 5, static shape tests | Update existing skeleton/fd-registry `N=1` pins to dynamic-shard contract; add net-ownership shard-0-shortcut gate with explicit stays-global exemptions; add pending `RT_RUNTIME_MAX_SHARDS` shape test; run `go build ./...`, focused static tests, pending expected-fail shape test, and `git diff --check`; update evidence/notes. |

### Known Regressions

- No Task 1 docs regression is known.
- Current `make runtime-v2-check` is green on immediate rerun at kickoff. The
  first attempt timed out in `TestMTBlockingChannelHelpersAllowTimersToAdvance`
  after 30s, before any docs edit and with no runtime code changed.

### Dead Ends / Paths Not To Retry

- Do not use broad `go test ./internal/vm -run 'MT|Async|Net|LLVM'` as an
  Epic 6 green gate. It remains accepted backend-test debt in `RV2-DEBT-001`.
- Do not retry Task 1 by changing runtime code because a
  `make runtime-v2-check` attempt flakes; Task 1 only records the current
  status and the successful rerun.

### Rollback/Recovery Notes

- Files or changes to revert: remove `docs/runtime-v2-epics/06-evidence.md`
  and remove the matching Epic 6 Task 01 handoff from `NOTES.md`.
- Generated artifacts to remove: none created intentionally by Task 1.
- Runtime processes, sockets, or temporary state to clean up: none.
- Recovery command or owner: documentation rollback only.

### Follow-Ups And Blockers

| Item | Blocks Task 1 completion? | Owner or next document | Reason |
| --- | --- | --- | --- |
| First-attempt `make runtime-v2-check` timeout | No for Task 1; no for current gate after green rerun, but relevant to future flake claims | `RV2-DEBT-002` / Epic 11 unless Epic 6 changes the path | Task 1 records baseline flake evidence and green rerun; it does not change runtime code. |
| Accept ownership dependency map | No | Task 2 | Task 1 froze the starting facts; Task 2 maps current paths in detail. |
| Listener model decision | No | Task 3 | Must be proven before behavior/static tests finalize. |
| Multi-shard behavior/static tests | No | Tasks 4 and 5 | Task 1 only records their gates. |

### Notes Consolidation Checklist

- [x] `NOTES.md` has the start context and intended proof.
- [x] `NOTES.md` has what changed, what was checked, and what was skipped.
- [x] Durable decisions stayed in the owning epic/task docs; Task 1 only
  recorded baseline evidence.
- [x] Skipped checks record the exact reason and blocker status.
- [x] Dead ends are recorded so future tasks do not retry them.
- [x] Follow-ups and blockers have an owner, target document, or next task.

## Task 2: Accept Ownership Dependency Map

### Task Identity And Scope

- Task: `06-tasks/02-accept-ownership-dependency-map.md`.
- Date: 2026-07-03.
- Scope: docs-only dependency map for accept/readiness/close/cancellation/
  shutdown ownership before any Epic 6 runtime implementation.
- Created: `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md`.
- Updated: this evidence file and `NOTES.md`.
- Out of scope: runtime code, tests, task docs, Makefile, CI, listener-model
  decision, benchmark changes, and commits.

### Files Touched

| Path | Change | Reason |
| --- | --- | --- |
| `docs/runtime-v2-epics/06-accept-ownership-dependency-map.md` | created | Source-backed map of net-shard-owned, stays-global-compat, and later-epic dependencies. |
| `docs/runtime-v2-epics/06-evidence.md` | updated | Mark Task 2 complete and record commands/gaps. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 2 handoff for Task 3/6/7/8/10/11. |

No runtime code, tests, task documents, `Makefile`, CI files, Sentrux rules, or
benchmark scripts changed.

### Commands/Checks

| Command | Purpose | Result |
| --- | --- | --- |
| `rg -n 'rt_runtime_shard0|rt_executor_(scheduler|net_poll_scratch|channel_blocking_compat|waiter_store|fd_registry)' runtime/native internal/vm` | Enumerate shard-0 compatibility accessors and current callers. | Completed; map records net-owned vs global-compat callers. |
| `rg -n 'rt_fd_registry_(attach_net_interest|detach_net_interest|net_interest_present|snapshot_poll_interest|complete_ready_net_waiters|drain_shutdown_net_waiters_locked|wake_closed_net_waiters|mark_closed)' runtime/native internal/vm` | Enumerate fd-registry lifecycle and net completion paths. | Completed; map marks registry paths `net-shard-owned`. |
| `rg -n 'net_poll_wake_(init|drain)|rt_net_wake_poll|net_poll_wake_read_fd|net_poll_wake_write_fd|poll_net_waiters|poll_net_waiters_owned|begin_net_poll|has_net_waiters' runtime/native` | Enumerate current wake-pipe and poller ownership. | Completed; map marks process-global wake pipe as Task 10 migration point. |
| `rg -n 'rt_net_(listen|accept|close_listener|close_conn|read|write|read_bytes|write_bytes|wait_accept|wait_readable|wait_writable)|NetListener|NetConn|net_listener_from|net_conn_from' runtime/native internal/vm` | Trace native and VM-visible net handle flow. | Completed; map records native metadata and public ABI boundary. |
| `rg -n 'net_(accept|read|write)_key|waker_is_net|prepare_park|park_current|wake_task|wake_task_with_policy|mark_done|cancel_task|pop_waiter|add_waiter|net_waiter' runtime/native` | Trace waiter keys, park/wake, cancellation, and cleanup. | Completed; map classifies net-key cleanup separately from global cancellation. |
| `rg -n 'SURGE_THREADS|rt_env_worker_count|exec_init_once|rt_shard_scheduler_init|worker_ctx|worker_next_ready|steal|SCHED_TRACE|trace_sched_steal' runtime/native internal/vm docs/runtime-v2-epics/LIVENESS_PROBES.md` | Trace config, worker model, steal path, and trace evidence. | Completed; map names Task 6/7 boundaries. |
| `rg -n 'shutdown|blocking_shutdown|rt_executor_request_shutdown|rt_executor_drain_shutdown_net_waiters' runtime/native internal/vm docs/runtime-v2-epics` | Trace shutdown state and drain/wake paths. | Completed; `runtime/native/rt_shutdown.c` exists and is mapped as Task 10/11 migration surface. |
| `rg -n 'RT_RUNTIME_SHARD_COUNT|RT_RUNTIME_MAX_SHARDS|shard_count|#error' runtime/native internal/vm/runtime_v2_*_test.go` | Enumerate fixed `N=1` runtime shape and static pins. | Completed; map points Task 5/6 at fixed-shard contract update. |
| `rg -n 'SO_REUSEPORT|SO_REUSEADDR' runtime/native/rt_net.c` | Check listener socket options. | Completed; only `SO_REUSEADDR` matched. |
| `git diff --check` | Whitespace gate for tracked docs diff. | Passed with no output. |
| `git diff --no-index --check /dev/null docs/runtime-v2-epics/06-accept-ownership-dependency-map.md` | Whitespace gate for new untracked map file. | Passed with no output; exit `1` is expected for a `/dev/null` diff. |

### Review Pass

Main-agent review approved Task 2 after checking the task DoD, the new map,
and the evidence/notes updates. The review sampled current source citations for
`rt_shutdown.c`, `rt_fd_registry.c`, `rt_net.c`, `rt_async_state.c`,
`rt_async_waiter.c`, `rt_runtime.c`, and VM net handles; no stale citation,
boundary error, or missing Task 3 reconciliation point was found.

### Contracts Touched

| Contract | Status | Evidence |
| --- | --- | --- |
| Runtime behavior | N/A | Docs-only map; no code changed. |
| Public Surge syntax/API | Preserved | Map explicitly defers syntax/keywords and keeps public net ABI stable. |
| Epic 6 ownership boundary | Clarified | Net accept/readiness/close/shutdown ownership moves; non-net primitives stay global compatibility under `ex->lock`; lock sharding remains Epic 7; Phase 4 messaging remains later. |
| Listener model | Not resolved | Task 3 reconciliation points recorded for `SO_REUSEPORT` group vs fallback handoff. |

### Gaps / Follow-Ups

No new unowned Epic 6 gap was found.

Task 3 must reconcile the listener model before Tasks 4/5 finalize behavior and
static tests: internal accept task placement, one public listener handle vs N
internal fds, handler-task owner placement without syntax changes, listener
group close semantics, fallback-handoff trace counters, and low-connection
`SO_REUSEPORT` skew.

Task 6's first safe implementation boundary is structural:
`RT_RUNTIME_MAX_SHARDS` plus runtime `shard_count`, `SURGE_SHARDS` parsing, and
per-shard container initialization under the preserved global lock. Task 7/10
own worker placement, no-steal connection scheduling, and per-shard poller/wake
ownership.

### Definition Of Done

- [x] Accept, readiness, close, cancellation, and shutdown paths are mapped with
      `file:line` citations.
- [x] Current shard-0 accessor callers are enumerated and classified.
- [x] `SURGE_THREADS`/`SURGE_SHARDS` implementation path is named for Task 6.
- [x] `NetListener`/`NetConn` native and VM-visible handle flow is mapped for
      Task 8.
- [x] No dependency outside Tasks 6-11 or the Not Included boundary was silently
      absorbed.
- [x] Task 3 reconciliation points are explicit.
- [x] `git diff --check` final whitespace gate.
