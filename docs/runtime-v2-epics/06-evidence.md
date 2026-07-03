# Epic 6 Evidence: N>1 Accept Ownership And Tier-1 Scheduler Boundary

This file records task evidence for Epic 6. Keep durable conclusions here and
keep `NOTES.md` as the live handoff log.

## Task Status

| Task | Status | Evidence |
| --- | --- | --- |
| 1 | Complete | Starting state, structural facts, accepted debt, line counts, Sentrux scans, current gates, and Task 2-5 gate plan recorded below. |
| 2 | Complete with post-facto audit | Accept/readiness/close/cancellation/shutdown dependency map recorded in `06-accept-ownership-dependency-map.md`; no unowned gap found. The subagent committed before the intended review handoff, so main-agent audit followed the commit. |
| 3 | Complete with process exception | Listener model proving spike chose per-shard `SO_REUSEPORT` listener groups; fallback handoff recorded as compatibility-only; `RV2-DEBT-013` added for stdlib HTTP raw-handle worker handoff. The subagent implemented and committed before explicit approval, so main-agent audit reran the proof before accepting the decision. |
| 4 | Complete | Multishard accept contract tests recorded below. |
| 5 | Complete | Multishard static shape tests recorded below. |
| 6 | Complete | Runtime shard array and config recorded below. |
| 7 | Complete | Per-shard scheduler placement recorded below. |
| 8 | Complete | Listener and connection owner metadata recorded below. |
| 9 | Complete | Accept distribution implementation recorded below. |
| 10 | Complete | Per-shard poller and wake ownership recorded below. |
| 11 | Complete | Multishard net lifecycle migration recorded below. |
| 12 | Complete | Trace counters, liveness fix, and benchmark evidence recorded below. |
| 13 | Complete | Runtime V2 accept CI gates recorded below; independent review, standalone gate, full-chain stability passes, and `make check` passed. |
| 14 | Complete | Large-file refactor tranche recorded below; no new code extraction was needed, legacy ceilings were tightened, and full gates passed. |
| 15 | Complete | Epic closeout, final gates, contract accounting, benchmark confirmation, Sentrux signals, and Epic 7 handoff recorded below. |

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

Post-facto main-agent audit checked Task 2 after the subagent committed the map
before the intended review handoff. The audit checked the task DoD, the new map,
and the evidence/notes updates. It sampled current source citations for
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

## Task 3: Listener Model Proving Spike

### Task Identity And Scope

- Task: `06-tasks/03-listener-model-proving-spike.md`.
- Date: 2026-07-03.
- Scope: proving spike for the Epic 6 listener model and one-user-accept-loop
  conflict.
- Proving spike: yes.
- Created: `docs/runtime-v2-epics/06-listener-model-proving-spike.md`.
- Scratch only: `build/tmp/runtime-v2-epic6/listener_model_probe.c` and
  `build/tmp/runtime-v2-epic6/listener_model_probe`.
- Updated: this evidence file, `NOTES.md`, and `DEBT.md`.
- Out of scope: durable runtime code, VM code, parser/sema/lowering, public
  examples, stdlib signatures, CI, Makefile, fd-registry migration, scheduler
  placement implementation, and Phase 4 messaging.

### Files Touched

| Path | Change | Reason |
| --- | --- | --- |
| `docs/runtime-v2-epics/06-listener-model-proving-spike.md` | created | Rule-1 spike record, proof outputs, listener-model decision. |
| `docs/runtime-v2-epics/06-evidence.md` | updated | Mark Task 3 complete and record proof commands. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 3 handoff. |
| `docs/runtime-v2-epics/DEBT.md` | updated | Add `RV2-DEBT-013` for stdlib HTTP raw-handle worker handoff under N>1. |
| `build/tmp/runtime-v2-epic6/listener_model_probe.c` | scratch | Quarantined C probe; not committed. |

No durable runtime/native, VM, parser, semantic-analysis, lowering, stdlib,
CI, Makefile, or public example file changed.

### Commands/Checks

| Command | Purpose | Result |
| --- | --- | --- |
| `cc -D_GNU_SOURCE -std=c11 -O2 -Wall -Wextra -Werror -pthread build/tmp/runtime-v2-epic6/listener_model_probe.c -o build/tmp/runtime-v2-epic6/listener_model_probe` | Compile the scratch probe with strict warnings. | Passed. |
| `build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1` | Candidate A low-count skew row. | Passed; `counts=0:0,1:0,2:0,3:1 active_shards=1`, accepted `1/1`. |
| `build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 8` | Candidate A low-count row. | Passed; `counts=0:2,1:3,2:2,3:1 active_shards=4`, accepted `8/8`. |
| `build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 32` | Candidate A low-count row. | Passed; `counts=0:9,1:7,2:8,3:8 active_shards=4`, accepted `32/32`. |
| `build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1024` | Candidate A high-load proof row. | Passed; `counts=0:241,1:245,2:265,3:273 active_shards=4`, accepted `1024/1024`. |
| `build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 32` | Candidate B fallback comparison. | Passed; target assignment `0:8,1:8,2:8,3:8`, accepted `32/32`, requires initial owner placement before registry exposure. |
| `build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 1024` | Candidate B fallback comparison. | Passed; target assignment `0:256,1:256,2:256,3:256`, accepted `1024/1024`, not Phase 4 only if one-time placement happens before registry exposure. |
| `wc -l build/tmp/runtime-v2-epic6/listener_model_probe.c docs/runtime-v2-epics/06-listener-model-proving-spike.md` | LOC check for scratch probe and spike doc. | Probe 296 lines, spike doc 190 lines after final update. |
| `git diff --check` | Whitespace gate after final Task 3 docs update. | Passed with no output. |

### Review Pass

Post-facto main-agent audit checked Task 3 after the subagent implemented and
committed the spike before explicit approval. The audit checked the Rule-1 spike
fields, proof output, selected listener model, rejected fallback, ABI/no-syntax
boundary, low-count skew note, and durable debt entry. It also checked that
scratch code is ignored under `build/` and not staged for commit.

The audit reran the scratch proof from the current checkout:

| Command | Post-facto audit result |
| --- | --- |
| `timeout 60s cc -D_GNU_SOURCE -std=c11 -O2 -Wall -Wextra -Werror -pthread build/tmp/runtime-v2-epic6/listener_model_probe.c -o build/tmp/runtime-v2-epic6/listener_model_probe` | Passed. |
| `timeout 30s build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1` | Passed; `counts=0:1,1:0,2:0,3:0 active_shards=1`, accepted `1/1`. |
| `timeout 30s build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 8` | Passed; `counts=0:2,1:0,2:4,3:2 active_shards=3`, accepted `8/8`. |
| `timeout 30s build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 32` | Passed; `counts=0:8,1:8,2:6,3:10 active_shards=4`, accepted `32/32`. |
| `timeout 60s build/tmp/runtime-v2-epic6/listener_model_probe reuseport --shards 4 --clients 1024` | Passed; `counts=0:259,1:256,2:245,3:264 active_shards=4`, accepted `1024/1024`. |
| `timeout 30s build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 32` | Passed; target assignment `0:8,1:8,2:8,3:8`; accepted `32/32`. |
| `timeout 60s build/tmp/runtime-v2-epic6/listener_model_probe handoff --shards 4 --clients 1024` | Passed; target assignment `0:256,1:256,2:256,3:256`; accepted `1024/1024`. |
| `git diff --check` | Passed with no output. |

### Decision

Epic 6 implements per-shard `SO_REUSEPORT` listener groups. One public
`TcpListener` owns an internal group with one member fd per shard. Accept
readiness is owned by the member's shard; the winning member resumes/enqueues
the accept waiter on the member's owner shard; `rt_net_accept()` then creates
an owner-tagged `NetConn`. A local `spawn` from that resumed continuation is
owner-local without new Surge syntax.

Fallback handoff is rejected as the target hot path. It remains documented only
as compatibility: it is acceptable only if the accepted fd receives its initial
owner before fd-registry exposure. Moving a registered/exposed connection would
be the migration control plane and remains outside Epic 6.

### Contracts Touched

| Contract | Status | Evidence |
| --- | --- | --- |
| Runtime behavior | Decision only | No durable runtime code changed. |
| Public Surge syntax/API | Preserved | Existing `net.listen` and `net.accept` signatures stay unchanged. |
| Native ABI | Preserved | `rt.h` function surface unchanged; Task 8 may change internal native structs only. |
| Low-connection skew | Expected | 1-client row activated one shard; Task 12 must judge distribution on a high-load row. |
| HTTP stdlib raw handle handoff | New debt | `RV2-DEBT-013` records that `stdlib/http/server.sg` worker channel handoff is not owner-shard-safe for N>1 yet. |

### Definition Of Done

- [x] Rule-1 proving-spike fields exist; the subagent reported the record was
      written before scratch C implementation, but the main session treats the
      ordering as a process exception because the subagent committed before
      approval. The proof was rerun during post-facto audit before accepting
      the decision.
- [x] Both listener models have recorded findings.
- [x] `SO_REUSEPORT` connection distribution was empirically observed on this
      machine.
- [x] Internal accept representation and handler owner-shard placement are
      named without `TBD`.
- [x] ABI stability and no-new-syntax answers are explicit.
- [x] Low-connection skew expectation is recorded.
- [x] Scratch code is quarantined under `build/tmp/` and not committed.
- [x] Task 4, Task 5, and Task 6 have a decided listener model.

## Task 4: Multishard Accept Contract Tests

### Task Identity And Scope

- Task: `06-tasks/04-multishard-accept-contract-tests.md`.
- Date: 2026-07-03.
- Scope: behavior tests for the Epic 6 accept ownership contract before the
  runtime implementation.
- Created: `internal/vm/runtime_v2_accept_compat_test.go` and
  `internal/vm/runtime_v2_accept_contract_test.go`.
- Updated: this evidence file and `NOTES.md`.
- Out of scope: runtime/native implementation, static shape gates, Makefile,
  CI, trace-counter implementation, Sentrux rules, parser/sema/lowering,
  public syntax, stdlib signatures, and benchmark changes.

### Files Touched

| Path | Change | Reason |
| --- | --- | --- |
| `internal/vm/runtime_v2_accept_compat_test.go` | created | Default-green `SURGE_SHARDS=1` native net compatibility floor. |
| `internal/vm/runtime_v2_accept_contract_test.go` | created | `runtime_v2_pending` contract tests for future multishard accept ownership behavior. |
| `STATS.md` | updated by pre-commit | Repository test-file/code-volume counters changed after adding the new test files. |
| `docs/runtime-v2-epics/06-evidence.md` | updated | Record Task 4 checks, expected-red pending shape, and review result. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 4 handoff for Tasks 6-13. |

No runtime/native code, VM implementation code, task documents, `Makefile`, CI
files, Sentrux rules, parser/sema/lowering, stdlib signatures, or public
examples changed. `STATS.md` changed only as generated repository statistics
from the commit hook.

### Contracts Touched

| Contract bullet | Test coverage |
| --- | --- |
| `SURGE_SHARDS=1` preserves observable native net behavior. | `TestRuntimeV2AcceptShardOneNativeNetCompatibility`, default test. |
| `SURGE_SHARDS=N` initializes exactly `N` shards and exposes proof. | `TestRuntimeV2AcceptShardConfigInitializesRequestedShardCount`, pending on Tasks 6 and 12. |
| Invalid `SURGE_SHARDS` fails explicitly. | `TestRuntimeV2AcceptRejectsInvalidShardConfig`, pending on Task 6. |
| `SURGE_SHARDS>1` rejects conflicting `SURGE_THREADS`. | `TestRuntimeV2AcceptRejectsConflictingThreadCount`, pending on Tasks 6/7. |
| Accepted connections distribute across owner shards. | `TestRuntimeV2AcceptDistributionAcrossOwnerShards`, pending on Tasks 9/12. |
| Readiness, close, cancellation, shutdown, non-owner use, and listener-group close are owner-shard-visible. | `TestRuntimeV2AcceptOwnerShardLifecycleTraceContract`, pending on Tasks 7/8/9/10/11/12. |

### Commands/Checks

| Command | Purpose | Result |
| --- | --- | --- |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestRuntimeV2AcceptShardOneNativeNetCompatibility$' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Default `SURGE_SHARDS=1` compatibility floor. | Passed in 2.33s. |
| `go test ./internal/vm -run '^TestRuntimeV2Accept' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Default untagged accept subset. | Passed in 2.34s; only the compatibility test runs without `runtime_v2_pending`. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Accept' -count=1 -parallel=1 -p=1 -v --timeout 180s` | Future accept ownership contract. | Expected-red in 13.88s: missing `runtime_shards`; invalid/conflicting env values not rejected; missing accept-owner `TRACE_NET` fields; net-owned shard-0 static gate still failing; missing `RT_RUNTIME_MAX_SHARDS`. No crash or hang. |
| `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Existing net liveness probe selected for this test-writing task. | Passed in 2.26s. |
| `go build ./...` | Default build after adding the untagged compatibility test. | Passed. |
| `git diff --check` | Whitespace gate. | Passed with no output. |
| `wc -l internal/vm/runtime_v2_accept_compat_test.go internal/vm/runtime_v2_accept_contract_test.go` | LOC check for new files. | 189 and 306 lines, both under the 500-line limit. |

### Review Pass

Main-session review checked the subagent-created Task 4 files against the task
spec, Task 2/3 contracts, build tags, no-hang behavior, expected-red failure
shape, and liveness coverage. No P0/P1 blocker was found for the Task 4
behavior tests. The tagged command also ran Task 5's pending accept static tests;
those failures are recorded as expected Task 5/6/7/9/10/11/12 contract gaps,
not Task 4 implementation bugs.

### Definition Of Done

- [x] Every testable Accept Ownership Contract bullet has a behavior test or a
      named trace-contract row.
- [x] The `SURGE_SHARDS=1` regression floor passes today without runtime code
      changes.
- [x] `runtime_v2_pending` tests name the later task(s) expected to make each
      contract pass.
- [x] Pending failures are "not implemented yet" contract failures, not crashes
      or hangs.
- [x] Evidence and notes record the green subset, expected-red subset, and
      liveness probe.

## Task 5: Multishard Static Shape Tests

### Task Identity And Scope

- Task: `06-tasks/05-multishard-static-shape-tests.md`.
- Date: 2026-07-03.
- Scope: static shape gates for the future dynamic shard array and net
  ownership no-shard-0 shortcut rule.
- Updated: `internal/vm/runtime_v2_skeleton_static_test.go`,
  `internal/vm/runtime_v2_fd_registry_static_test.go`,
  `docs/runtime-v2-epics/06-tasks/05-multishard-static-shape-tests.md`, this
  evidence file, and `NOTES.md`.
- Created: `internal/vm/runtime_v2_accept_static_test.go`.
- Out of scope: runtime/native implementation, behavior tests beyond Task 4,
  Makefile, CI, Sentrux rules, trace-counter implementation, parser/sema/
  lowering, public syntax, stdlib signatures, and benchmark changes.

### Files Touched

| Path | Change | Reason |
| --- | --- | --- |
| `internal/vm/runtime_v2_skeleton_static_test.go` | updated | Replace fixed `RT_RUNTIME_SHARD_COUNT == 1` pin with static storage limit compatibility shape. |
| `internal/vm/runtime_v2_fd_registry_static_test.go` | updated | Same static shard storage limit compatibility shape for fd-registry boundary tests. |
| `internal/vm/runtime_v2_accept_static_test.go` | created | Pending net-owned no-shard-0 shortcut gate and pending `RT_RUNTIME_MAX_SHARDS` shape gate. |
| `docs/runtime-v2-epics/06-tasks/05-multishard-static-shape-tests.md` | updated | Correct checks to use `runtime_v2_pending` for tagged static tests. |
| `docs/runtime-v2-epics/06-evidence.md` | updated | Record Task 5 checks and expected-red static gates. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 5 handoff for Tasks 6/7/9/11/13. |

No runtime/native code, VM implementation code, `Makefile`, CI files, Sentrux
rules, parser/sema/lowering, stdlib signatures, public examples, or benchmark
scripts changed.

### Static Gate Contract

| Gate | Current status | Future owner |
| --- | --- | --- |
| Existing skeleton/fd-registry static pins accept either `RT_RUNTIME_MAX_SHARDS` or current `RT_RUNTIME_SHARD_COUNT`, but require a positive storage limit. | Green today under `runtime_v2_pending`. | Task 6 replaces the fallback macro with `RT_RUNTIME_MAX_SHARDS`; these gates should stay green. |
| Net-owned accessors and named net-owned source functions must not route through `rt_runtime_shard0()` or `shards[0]`. | Expected-red today: `rt_executor_net_poll_scratch`, `rt_executor_fd_registry`, and `rt_executor_fd_registry_const` still route through shard 0. Direct scans cover `rt_net.c`, `rt_fd_registry.c`, `rt_shutdown.c`, `rt_async_waiter.c`, and net-poller functions in `rt_async_state.c`. | Tasks 6/8/9/10/11 move net ownership to explicit shard owners. |
| `rt_runtime.shards` must be sized by `RT_RUNTIME_MAX_SHARDS` and runtime `shard_count` must be bounded by it. | Expected-red today: `RT_RUNTIME_MAX_SHARDS` is not defined. | Task 6. |
| Stays-global compatibility accessors are allowed to remain explicit compatibility paths. | Documented in the test: scheduler, channel blocking compat, and generic waiter-store accessors. | Task 7 may shrink scheduler exemptions when owner placement lands; non-net global primitives remain compat in Epic 6. |

### Commands/Checks

| Command | Purpose | Result |
| --- | --- | --- |
| `go build ./...` | Default compile gate after adding pending static tests. | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Skeleton|TestRuntimeV2FDRegistryStatic' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Existing static gates after replacing fixed `N=1` pins. | Passed: `TestRuntimeV2FDRegistryStaticShape`, `TestRuntimeV2FDRegistryStaticBoundary`, and `TestRuntimeV2SkeletonStaticShape`. |
| `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Accept(NetOwnershipNoShard0Shortcut|DynamicShardArrayShape)' -count=1 -parallel=1 -p=1 -v --timeout 90s` | New Task 5 static gates before runtime implementation. | Expected-red in a clean Task 5 tree: net-owned accessors still route through shard 0; `RT_RUNTIME_MAX_SHARDS` is not defined. |
| `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Skeleton|TestRuntimeV2FDRegistryStatic|TestRuntimeV2Accept(NetOwnershipNoShard0Shortcut|DynamicShardArrayShape)' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Mixed green/expected-red confirmation. | Existing static pins passed; new accept static gates failed in the intended shape. |
| `git diff --check` | Whitespace gate. | Passed with no output. |
| `wc -l internal/vm/runtime_v2_accept_static_test.go internal/vm/runtime_v2_skeleton_static_test.go internal/vm/runtime_v2_fd_registry_static_test.go` | LOC check for new/touched static tests. | 208, 66, and 431 lines; all under the 500-line limit. |

### Review Pass

Main-session review checked the Task 5 diff against the task spec, Task 2's
`net-shard-owned` vs `stays-global-compat` line, the existing build tags, and
the current source bodies in `runtime/native/rt_runtime.c`. Independent review
then found two issues before Task 6 continued: the gate was too narrow because
it only scanned three `rt_runtime.c` accessor bodies, and the future
`RT_RUNTIME_MAX_SHARDS` C snippet used an unused `static` function that could
become a false-red under `-Wall -Wextra -Werror`. The follow-up broadened the
same test to scan named net-owned functions in the net, fd-registry, shutdown,
waiter-bridge, and net-poller files, and changed the dynamic-shape snippet to
top-level `_Static_assert`s plus a non-static syntax-check function.

One process issue was fixed before commit: the task's check list originally
omitted `-tags runtime_v2_pending` for files that are already tagged, which
produced a false `no tests to run` result. The task doc and evidence now use
the tagged commands. A second process issue was recorded during review: Task 5
was committed while independent review was still running. The review finding
was resolved as this follow-up commit before any Task 6 closure.

Clean-shape verification temporarily isolated an uncommitted Task 6 draft
header/config change so Task 5's pending `RT_RUNTIME_MAX_SHARDS` test could be
observed against the committed pre-Task-6 runtime shape. After verification the
draft was restored to the worktree.

No P0/P1 blocker remains for this static-test task. The new accept static tests
are deliberately expected-red until later Epic 6 implementation tasks.

### Definition Of Done

- [x] Existing `N=1` static pins now assert a dynamic-compatible positive shard
      storage limit and still pass against current code.
- [x] A pending net-ownership gate fails on net accessors routed through shard 0,
      scans named net-owned source functions for direct shard-0 shortcuts, and
      does not fail documented global-compat accessors.
- [x] The stays-global exemption list is written next to the gate.
- [x] A pending `RT_RUNTIME_MAX_SHARDS` shape test exists for Task 6.
- [x] Task 6 can replace `RT_RUNTIME_SHARD_COUNT` with
      `RT_RUNTIME_MAX_SHARDS` without discovering the old static pins mid-task.

## Task 6: Runtime Shard Array And Config

Status: complete on 2026-07-03.

### Scope Completed

- Replaced fixed `RT_RUNTIME_SHARD_COUNT == 1` storage with
  `RT_RUNTIME_MAX_SHARDS` plus runtime `shard_count`.
- Added `runtime/native/rt_runtime_config.c/h` for startup configuration,
  `SURGE_SHARDS`, `SURGE_THREADS` interaction, and explicit runtime status
  codes outside the bloated async-state file.
- Added shard-indexed runtime accessors:
  `rt_runtime_shard`, `rt_runtime_shard_const`,
  `rt_executor_net_poll_scratch_for_shard`, and
  `rt_executor_fd_registry_for_shard`.
- Initialized every configured shard's structural state:
  `shard_id`, executor/runtime back-pointers, scheduler container,
  waiter store, fd registry, net poll scratch, and heap accounting cell.
- Kept Task 6 structural-only: `rt_start_workers` is still not shard-aware,
  no worker ctx gains owner-shard placement, and net-owned call sites use
  explicit `_for_shard(ex, 0)` compatibility placeholders until Tasks 7-11.
- Added `runtime_shards=<count>` to `TRACE_EXEC` so Task 4's config contract
  can prove startup used the requested shard count.
- Updated fd-registry static harness snippets so they link against the new
  shard-indexed registry accessor names.
- Closed `RV2-DEBT-014`: `check_file_sizes.sh` now counts effective source LOC
  for `.go`, `.c`, and `.h` by ignoring blank/comment-only lines while still
  counting code-bearing lines with trailing comments.

### Bound And Config Decisions

`RT_RUNTIME_MAX_SHARDS` is `64`. This keeps runtime storage bounded and
static, avoids adding allocator/lifetime complexity to the structural task,
and is large enough for the current one-Tier-1-worker-per-shard target on
common development and CI hosts. If a later performance epic proves larger
machines need more than 64 Tier 1 shards, that task can raise the bound with
benchmark evidence and storage-cost accounting.

`SURGE_SHARDS` defaults to `1`. Values must parse strictly as decimal integers
in `1..RT_RUNTIME_MAX_SHARDS`; `0`, non-numeric values, negatives, and
over-bound values exit with an explicit diagnostic instead of falling back.

`SURGE_THREADS` remains the compatibility worker-count control only when
`SURGE_SHARDS=1`. Under `SURGE_SHARDS>1`, it may be unset or equal to
`SURGE_SHARDS`; any other set value is an explicit configuration error. Task 6
does not spawn one worker per shard yet. It initializes per-shard scheduler
containers with `worker_count=1` and leaves real worker-to-shard binding to
Task 7.

`SURGE_BLOCKING_THREADS` remains an explicit override. The default blocking
pool size is derived from `legacy_worker_threads`, not from `shard_count`; this
preserves Task 6's structural-only boundary and avoids making `SURGE_SHARDS>1`
silently create extra blocking OS threads before Task 7 owns thread placement.

### Files Touched

| Path | Physical LOC | Effective LOC | Notes |
| --- | ---: | ---: | --- |
| `check_file_sizes.sh` | 538 | n/a | Tooling script; added source-comment-aware counter and `--self-test`. |
| `runtime/native/rt_async_internal.h` | 492 | 424 | Under 500 physical lines after moving config/status definitions to `rt_runtime_config.h`. |
| `runtime/native/rt_async_state.c` | 1696 | 1573 | Legacy-ok under `.loc-legacy-allowlist` ceiling 1727; config parsing extracted. |
| `runtime/native/rt_async_trace.c` | 500 | 466 | At physical 500-line target; added `runtime_shards` only. |
| `runtime/native/rt_async_waiter.c` | 369 | 319 | Net-owned path uses explicit shard-indexed placeholder. |
| `runtime/native/rt_fd_registry.c` | 409 | 350 | Net-owned path uses explicit shard-indexed placeholder. |
| `runtime/native/rt_fd_registry.h` | 116 | 74 | Declares shard-indexed registry accessor. |
| `runtime/native/rt_net.c` | 904 | 843 | Legacy-ok under ceiling 904; net-owned paths use explicit shard-indexed placeholders. |
| `runtime/native/rt_runtime.c` | 316 | 271 | Shard-count init/destroy/accessor implementation. |
| `runtime/native/rt_runtime_config.c` | 135 | 127 | New config parser/status surface. |
| `runtime/native/rt_runtime_config.h` | 28 | 20 | New max-shard/status/config declarations. |
| `runtime/native/rt_shutdown.c` | 35 | 32 | Shutdown path uses explicit shard-indexed placeholder. |
| `internal/vm/runtime_v2_fd_registry_shutdown_static_test.go` | 320 | n/a | Harness stub for new shard-indexed registry accessor. |
| `internal/vm/runtime_v2_fd_registry_static_test.go` | 437 | n/a | Harness stub for new shard-indexed registry accessor. |

### Contracts Proven

- `SURGE_SHARDS=1` still passes the default native net compatibility contract
  from Task 4.
- `SURGE_SHARDS=4` initializes runtime structures for four shards and exposes
  `runtime_shards=4` in `TRACE_EXEC`.
- Invalid `SURGE_SHARDS` values fail explicitly with diagnostics.
- Conflicting `SURGE_THREADS` under `SURGE_SHARDS>1` fails explicitly.
- Task 5 static gates now pass against the real `RT_RUNTIME_MAX_SHARDS` shape.
- Net-owned shard-0 compatibility is explicit at call sites via
  `_for_shard(ex, 0)`; hidden `rt_runtime_shard0()`/`shards[0]` shortcuts are
  blocked by the pending static gate.

### Review Pass

Independent review found one P2 issue: the first Task 6 draft derived default
blocking pool size from `shard_count` under `SURGE_SHARDS>1`, which could
create extra blocking OS threads and violate the structural-only boundary. The
fix changed the default path to:

```c
blocking_threads = rt_runtime_default_blocking_count(out->legacy_worker_threads);
```

The reviewer then confirmed the default blocking pool is no longer derived
from `shard_count`, found no new findings, and the focused compile/static
gates passed.

### Commands/Checks

| Command | Result |
| --- | --- |
| `make c-check` | Passed. |
| `make cppcheck` | Passed; `rt_runtime_config.c` included in cppcheck. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Accept(ShardConfigInitializesRequestedShardCount\|RejectsInvalidShardConfig\|RejectsConflictingThreadCount)$' -count=1 -parallel=1 -p=1 -v --timeout 180s` | Passed. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestRuntimeV2AcceptShardOneNativeNetCompatibility$' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Passed in independent review. |
| `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Accept(NetOwnershipNoShard0Shortcut\|DynamicShardArrayShape)' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Skeleton\|TestRuntimeV2FDRegistryStatic' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Passed. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Accept' -count=1 -parallel=1 -p=1 -v --timeout 180s` | Expected-red. Config/static/compat cases passed; failures were limited to downstream owner/distribution/lifecycle fields: `accept_owner_active_shards`, `fd_owner_registry_rows`, `close_owner_wakeups`, `cancel_owner_cleanup`, `shutdown_poller_wakeups`, `non_owner_conn_denied`, and `listener_group_members_closed`. |
| `make runtime-v2-check` | Passed. |
| `make check` | Passed, including Go tests, lint, `make c-check`, and effective LOC gate. |
| `./check_file_sizes.sh --self-test` | Passed. |
| `./check_file_sizes.sh -a` | Passed; 697 checked files, 665 OK, 24 acceptable, 8 legacy-ok, 0 bad. |
| `git diff --check` | Passed. |
| `sentrux check .` | Passed, quality 6188. |
| `sentrux check runtime` | Passed, quality 5301. |
| `sentrux check runtime/native` | Passed, quality 5340. |

### Remaining Work

- Task 7 must make worker/task placement shard-aware and define how
  `SURGE_SHARDS>1` actually maps running worker contexts to shards.
- Tasks 8-11 must replace `_for_shard(ex, 0)` compatibility placeholders with
  real owner-shard routing for listeners, accepted connections, readiness,
  close/cancel/shutdown, and poller wake ownership.
- Task 12 must add the remaining owner/distribution/lifecycle `TRACE_NET`
  proof fields that the full pending accept contract still expects.

### Definition Of Done

- [x] `SURGE_SHARDS=1` compatibility path passed Task 4's native net regression
      floor and broad runtime gates.
- [x] `SURGE_SHARDS=N>1` initializes `N` shard structures without spawning new
      shard-bound workers.
- [x] Invalid `SURGE_SHARDS` fails with an explicit diagnostic.
- [x] Conflicting `SURGE_THREADS` under `SURGE_SHARDS>1` fails explicitly.
- [x] Partial initialization has cleanup paths through `rt_runtime_destroy` /
      `rt_runtime_destroy_global`.
- [x] Task 5 static gates pass against `RT_RUNTIME_MAX_SHARDS`.
- [x] Shard-indexed accessors exist for net-owned call sites; stays-global
      compatibility paths remain explicit.
- [x] Line-count impact is recorded; all new/heavily rewritten code files stay
      under the 500-line target, and legacy over-limit files did not grow past
      their allowlist ceilings.

## Task 7: Per-Shard Scheduler Placement

Status: complete on 2026-07-03.

### Scope Completed

- Made `rt_start_workers` shard-aware. Under `SURGE_SHARDS>1`, each configured
  shard gets its own `scheduler.worker_ctxs` and real worker thread(s) using
  that shard's scheduler; under `SURGE_SHARDS=1`, the compatibility worker
  model is preserved.
- Added scheduler placement metadata to `rt_task`:
  `placement_class`, `owner_shard_valid`, and `owner_shard_id`.
- Added `TASK_PLACEMENT_CONNECTION` as the Tier 1 owner-local class. Generic
  tasks remain compatible with existing scheduling and stealing behavior.
- Added placement helpers in `runtime/native/rt_scheduler_placement.c`,
  including `rt_task_scheduler`, `rt_task_set_placement`,
  `rt_task_inherit_placement`, `rt_task_can_steal_from_shard`, and
  `rt_debug_assert_no_parked_with_work`.
- Routed ready placement through the task owner when present; unowned work keeps
  current-worker/shard-0 compatibility behavior.
- Modified steal pops so connection-owned tasks cannot be stolen by a
  non-owner shard. Denied steals restore the deque entry and return before
  `SCHED_TRACE` records a steal.
- Made invalid owner-shard placement fail closed with
  `async: invalid task owner shard` instead of silently falling back to shard 0.
- Aggregated scheduler snapshot trace fields across shards:
  `worker_count`, `running`, `inject_len`, `local_total`, and `local_max`.
- Added `parked_with_work` trace accounting and a scheduler-queue
  parked-with-work assertion on the actual worker sleep path before
  `pthread_cond_wait`.

### Placement And Boundary Decisions

Task owner metadata lives directly on `rt_task`. This is intentionally earlier
than listener/connection metadata because Task 8 has not yet added connection
objects with owner-shard fields. Task 8/9 should attach accepted connection
handlers by calling `rt_task_set_placement(task, owner_shard,
TASK_PLACEMENT_CONNECTION)` or an equivalent internal wrapper.

The no-steal rule is class-aware: only `TASK_PLACEMENT_CONNECTION` with a valid
owner shard is protected. Generic/unowned tasks retain the current compatibility
scheduler, including intra-shard stealing for `SURGE_SHARDS=1` and for any
future per-shard worker count greater than one.

The parked-with-work invariant closed in Task 7 is the scheduler-ready-queue
form: a worker may not sleep while its own shard scheduler has inject/local
ready work. Local fd-ready batches, per-shard wake fds, and poller sleep/wake
ownership remain Task 10 scope, not Task 7 scope.

### Files Touched

| Path | Physical LOC | Effective LOC | Notes |
| --- | ---: | ---: | --- |
| `runtime/native/rt_async_internal.h` | 501 | 440 | Under effective target; adds placement fields/prototypes. |
| `runtime/native/rt_async_state.c` | 1857 | 1726 | Legacy-ok under `.loc-legacy-allowlist` ceiling 1727; worker placement now shard-aware. |
| `runtime/native/rt_async_task.c` | 770 | 731 | Legacy-ok under ceiling 768 effective LOC; child/checkpoint/sleep tasks inherit placement. |
| `runtime/native/rt_async_blocking.c` | 294 | 278 | Blocking tasks inherit parent placement. |
| `runtime/native/rt_async_trace.c` | 534 | 498 | Under effective target; aggregates scheduler snapshot fields across shards. |
| `runtime/native/rt_scheduler_placement.c` | 85 | 78 | New placement/no-steal/invariant helper module. |
| `internal/vm/runtime_v2_scheduler_placement_test.go` | 455 | n/a | New pending harness tests for worker shape, no-steal, invalid owner, and invariant panic. |
| `internal/vm/runtime_v2_scheduler_placement_source_test.go` | 76 | n/a | New pending source gates for steal validation and worker sleep assertion placement. |

### Contracts Proven

- `SURGE_SHARDS=4` with `SURGE_THREADS=4` and with `SURGE_THREADS` unset
  creates four Tier 1 workers, one per shard, with matching heap worker cells.
- `rt_worker_count()` reports total Tier 1 workers under `SURGE_SHARDS>1`.
- `SURGE_SHARDS=1` keeps existing `TestMTWorkStealing` and
  `TestMTSeededScheduler` behavior green.
- Connection-owned tasks are not stolen by a non-owner shard. The adversarial
  harness pins a gate and target task to shard 1, keeps shard 1's worker busy,
  verifies the target does not run while shard 0 is idle, releases shard 1, and
  verifies `SCHED_TRACE steal=0`.
- Generic/unowned tasks remain stealable by the compatibility policy.
- Invalid owner-shard placement fails closed.
- The worker sleep path calls the shard-local parked-with-work assertion before
  sleeping, and a deliberate queued-work violation panics with
  `parked-with-work invariant violated`.
- Task 5 static gates still pass: net-owned shard-0 shortcuts remain blocked,
  and the dynamic shard array shape stays valid.

### Review Pass

Independent review initially found three issues:

- P1: no real worker-path no-steal proof. Fixed with
  `TestRuntimeV2SchedulerPlacementNoStealWorkerPath`.
- P2: invalid owner shard fell back to shard 0. Fixed by failing closed in
  `rt_task_scheduler` plus
  `TestRuntimeV2SchedulerPlacementInvalidOwnerFailsClosed`.
- P2: parked-with-work coverage was too narrow. Fixed with
  `TestRuntimeV2SchedulerPlacementParkedWithWorkSourceGate`, proving the
  assertion is on the actual worker sleep path, while keeping fd-ready/poller
  wake ownership in Task 10 scope.

The reviewer re-ran the focused placement tests and confirmed all previous
findings closed. No remaining Task 7 finding is open.

### Commands/Checks

| Command | Result |
| --- | --- |
| `make c-check` | Passed. |
| `make cppcheck` | Passed. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestMT(WorkStealing\|SeededScheduler)' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2SchedulerPlacement' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2AcceptNetOwnershipNoShard0Shortcut\|TestRuntimeV2AcceptDynamicShardArrayShape\|TestRuntimeV2Skeleton\|TestRuntimeV2FDRegistryStatic' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `make runtime-v2-check` | Passed. |
| `make check` | Passed. |
| `./check_file_sizes.sh --self-test` | Passed. |
| `./check_file_sizes.sh` | Passed; touched runtime files are OK or legacy-ok under effective LOC. |
| `git diff --check` | Passed. |
| `sentrux check .` | Passed, quality 6185. |
| `sentrux check runtime` | Passed, quality 5332. |
| `sentrux check runtime/native` | Passed, quality 5353. |

### Remaining Work

- Task 8 must add listener/connection owner metadata and connect it to the
  task-placement mechanism introduced here.
- Task 9 must place accepted handler tasks on the accepting shard.
- Task 10 owns per-shard poller/wake-fd ownership and the fd-ready/poller form
  of parked-with-work/lost-wakeup proof.

### Definition Of Done

- [x] `rt_start_workers` is shard-aware and preserves `SURGE_SHARDS=1`
      compatibility.
- [x] Owner-shard metadata exists on `rt_task` and can be used for connection
      tasks.
- [x] A non-owner shard cannot steal a marked connection task; proven by an
      adversarial worker-path test and `SCHED_TRACE steal=0`.
- [x] Existing `TestMTWorkStealing`/`TestMTSeededScheduler` stay green under
      the compatibility path.
- [x] CPU-bound/generic tasks are unaffected by the connection-only steal rule.
- [x] Scheduler queued-work parked-with-work invariant is implemented and
      proven on the worker sleep path with a deliberate violation test.
- [x] Relevant Task 5 static gates remain green.

## Task 8: Listener And Connection Owner Metadata

Status: complete on 2026-07-03.

### Scope Completed

- Moved native `NetListener` and `NetConn` definitions out of `rt_net.c` into
  `rt_net_handles.h`, adding owner and lifecycle metadata while preserving the
  public `rt.h` native function signatures.
- `NetListener` now carries:
  `fd`, `closed`, `kind`, `owner_shard_id`, `member_count`, and a
  `NetListenerMember*` array. `NetListenerKind` has explicit discriminators
  for `NET_LISTENER_SINGLE`, `NET_LISTENER_REUSEPORT_GROUP`, and
  `NET_LISTENER_FALLBACK_HANDOFF`.
- `NetListenerMember` records `(fd, owner_shard_id, closed)`.
- `NetConn` now carries `fd`, `closed`, `owner_shard_valid`, and
  `owner_shard_id`.
- Added `rt_net_handles.c` for handle allocation/member selection and
  `rt_net_lifecycle.c/h` for owner-first close helpers with explicit status:
  `RT_NET_LIFECYCLE_OK`, `INVALID`, `REGISTRY_ERROR`, `CLOSE_ERROR`, and
  `PARTIAL_CLOSE`.
- Added `rt_net_listener_socket.c/h` to keep listener socket creation and
  optional `SO_REUSEPORT` setup out of the over-limit `rt_net.c`.
- Updated `rt_net_connect` and `rt_net_accept` to create owner-tagged
  `NetConn` values. Outbound connects use the current worker shard when
  available, otherwise compatibility owner shard `0`.
- Updated `rt_net_close_conn` and `rt_net_close_listener` to route close through
  owner-first lifecycle helpers instead of the deleted `close_net_fd_slot`.
- Added `runtime_v2_net_metadata_test.go` for metadata/static ABI shape and
  `SURGE_SHARDS=4` listen/close compatibility.
- Added `runtime-v2-accept-check` to the Runtime V2 gate, covering the new net
  metadata tests and the accept static shape tests.

### Representation And Activation Boundary

Task 3 remains the accepted model: Epic 6 targets per-shard `SO_REUSEPORT`
listener groups. Task 8 implements the group-capable representation and
listener-member lifecycle loop, but it deliberately does not activate
multi-member `SO_REUSEPORT` listeners in the public `rt_net_listen` path yet.

Reason: today's async task state has one `park_key`, and `rt_net_wait_accept`
can register one net accept fd key. Creating N kernel listener sockets before
Task 9/10/11 can register group waits would let Linux route connections to
member fds that the runtime never waits on, producing client hangs. Task 8
therefore keeps public listen as a single live member while preserving the
array/discriminator shape that Task 9 will populate for real accept
distribution.

The listener-group close helper iterates every member in the representation and
uses each member's `owner_shard_id` when marking fd-registry close snapshots.
With the Task 8 public listen path there is one live member, so the behavior
stays compatibility-safe. Full owner-local waiter-store migration and
per-shard poller wake are still Task 10/11 scope.

Linux `SO_REUSEPORT` listener-group close behavior remains recorded as expected
OS behavior: closing a group member may drop connections already queued on that
member's accept queue. Epic 6 must not promise those queued connections
survive close.

### RV2-DEBT-010 Decision

`RV2-DEBT-010` stays open by deliberate Task 8 decision. Adding owner metadata
does not make copied `TcpConn`/`TcpListener` handles generation-aware.
Current fd-registry generations protect poll snapshots and waiter completions,
not every direct public handle operation. Closing the debt correctly requires
validating public handle operations against a registry generation or stable
handle id before issuing direct fd operations.

No fake generation field was added to `NetConn` or `NetListener`.

### Files Touched

| Path | Physical LOC | Effective LOC | Notes |
| --- | ---: | ---: | --- |
| `runtime/native/rt_net.c` | 904 | 844 | Legacy-ok under `.loc-legacy-allowlist` ceiling 904; net handle structs and close helper moved out. |
| `runtime/native/rt_net_handles.c` | 130 | 122 | New handle/member allocation and selection module. |
| `runtime/native/rt_net_handles.h` | 49 | 42 | New internal metadata shapes for listener/connection handles. |
| `runtime/native/rt_net_lifecycle.c` | 84 | 79 | New owner-first close lifecycle helper module. |
| `runtime/native/rt_net_lifecycle.h` | 26 | 20 | New explicit lifecycle status API. |
| `runtime/native/rt_net_listener_socket.c` | 110 | 103 | New listener socket setup helper with optional `SO_REUSEPORT`. |
| `runtime/native/rt_net_listener_socket.h` | 13 | 10 | New listener socket setup API. |
| `internal/vm/runtime_v2_net_metadata_test.go` | 124 | n/a | New pending metadata/static and listen-close compatibility tests. |
| `internal/vm/runtime_v2_accept_static_test.go` | 215 | n/a | Updated static gate for new lifecycle helper names. |
| `Makefile` | 398 | n/a | Added `runtime-v2-accept-check` to `runtime-v2-check`. |

### Review Pass

Independent review found:

- P1: initial implementation created N `SO_REUSEPORT` members while wait/accept
  only selected the first member, which could hang clients. Fixed by keeping
  public `rt_net_listen` on one live member until Task 9 adds group wait/accept
  routing.
- P1: owner-local waiter/poller routing is not implemented. Reclassified as
  Task 10/11 boundary after removing public N-member activation from Task 8.
- P1: static ownership gate still referenced deleted `close_net_fd_slot`.
  Fixed by checking `rt_net_lifecycle.c` owner-first helpers.
- P2: `RV2-DEBT-010` decision was not recorded. Fixed in this evidence and
  `DEBT.md`.
- P2: new metadata tests were not wired into the Runtime V2 gate. Fixed with
  `runtime-v2-accept-check`.

### Commands/Checks

| Command | Result |
| --- | --- |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2NetMetadata' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2Accept(NetOwnershipNoShard0Shortcut\|DynamicShardArrayShape)$' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `make runtime-v2-accept-check` | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2FDRegistry(ClosedFDFailsFast\|CloseWakePollNotificationProof)$' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Accept(DistributionAcrossOwnerShards\|OwnerShardLifecycleTraceContract)$' -count=1 -parallel=1 -p=1 -v --timeout 180s` | Expected-red without hang; failed only on downstream missing `TRACE_NET` owner/distribution/lifecycle fields. |
| `make c-check` | Passed. |
| `make cppcheck` | Passed. |
| `./check_file_sizes.sh` | Passed; touched runtime files are OK or legacy-ok under effective LOC. |
| `./check_file_sizes.sh --self-test` | Passed. |
| `make runtime-v2-check` | Passed on final rerun. Earlier full attempts hit an intermittent timeout in `TestMTBlockingChannelHelpersAllowTimersToAdvance`; exact rerun passed in 2.6s, and the final full gate then passed. |
| `make check` | Passed. |
| `git diff --check` | Passed. |
| `sentrux check .` | Passed, quality 6185. |
| `sentrux check runtime` | Passed, quality 5320. |
| `sentrux check runtime/native` | Passed, quality 5338. |

### Remaining Work

- Task 9 must activate real multi-member `SO_REUSEPORT` listener groups and
  route accept readiness/accepted connections to the winning owner shard.
- Task 10 must provide per-shard poller/wake ownership before listener-group
  wait can be made fully local.
- Task 11 must migrate net waiters/fd-registry close/cancel/shutdown from
  compatibility shard-0/global semantics to owner-shard semantics.
- `RV2-DEBT-010` remains open for a future stable handle id or
  registry-generation validation design.

### Definition Of Done

- [x] `NetListener`/`NetConn` carry owner-shard metadata and listener
      discriminator/member-array shape.
- [x] Listener close uses an owner-first explicit-status lifecycle helper and
      iterates every represented listener member.
- [x] Public `TcpListener`/`TcpConn` Surge-visible API and native `rt.h`
      function signatures are unchanged.
- [x] `RV2-DEBT-010` is explicitly kept open with reason and owner.
- [x] `rt_net.c` did not grow; effective LOC is `844 <= 904` legacy ceiling.
- [x] Metadata/static tests are part of `runtime-v2-check` through
      `runtime-v2-accept-check`.

## Task 9: Accept Distribution Implementation

Status: complete on 2026-07-03.

### Scope Completed

- Activated the Task 3 listener model for real: `rt_net_listen` now creates a
  per-shard listener group under `SURGE_SHARDS>1`, with one `SO_REUSEPORT`
  member per shard and each member tagged with its owner shard.
- Added `rt_net_accept_group.c/h` for group accept wait/readiness helpers,
  keeping `rt_net.c` below its 904 effective-LOC legacy ceiling.
- `rt_net_wait_accept` now registers one public accept task against every live
  listener member fd. The first ready member stores
  `(fd, owner_shard_id)` on the task, marks the accept continuation as
  `TASK_PLACEMENT_CONNECTION`, and clears sibling listener-member wait keys so
  a later ready member cannot overwrite the winner before `rt_net_accept`.
- `rt_net_accept` consumes the remembered member, accepts from that member,
  creates an owner-tagged `NetConn`, and places the current continuation on
  the accepting shard. If no remembered member exists, it probes live members
  round-robin as a compatibility fallback.
- Added real open-fd rows to `rt_fd_registry` via
  `rt_fd_registry_register_open_fd`. Listener members, outbound connects, and
  accepted connections register open rows in the owner shard registry before
  wait interests are attached. Zero-interest registered rows remain until close;
  non-registered compatibility rows still disappear when their last interest
  detaches.
- The existing single I/O/poll loop now performs Task-9-only aggregate polling:
  it snapshots all shard fd registries into shard 0's scratch, records the
  snapshot owner shard, polls once, and completes readiness against the owning
  registry. Task 10 still owns true per-shard poller/wake-fd ownership.
- Added canonical listener lookup for copied `TcpListener.__opaque` values so
  copied listener handles resolve back to the listener group metadata instead
  of a single copied compatibility fd view.
- Added `TRACE_NET` proof fields used by current pending accept tests:
  `accept_owner_active_shards`, `fd_owner_registry_rows`,
  `close_owner_wakeups`, `cancel_owner_cleanup`,
  `shutdown_poller_wakeups`, `non_owner_conn_denied`, and
  `listener_group_members_closed`.

### Boundaries And Debt

- No public Surge syntax, stdlib signature, or native `rt.h` ABI changed.
- No Phase 4 cross-shard messaging, inbound queues, eventfd protocol, credits,
  or seq-cst parked protocol was implemented.
- Task 10 remains responsible for per-shard poller ownership and per-shard wake
  pipes. Task 9's aggregate poll is a compatibility bridge so nonzero-shard
  accept member fds can make progress before Task 10.
- Task 11 remains responsible for full close/cancellation/shutdown lifecycle
  migration. Task 9 only made shutdown drain iterate shard registries with an
  explicit owner argument where the new aggregate poll path already needed it.
- `RV2-DEBT-013` stays open. Non-owner `TcpConn` operation denial was not added
  in Task 9 because copied/raw handles still need a stable owner/generation
  guard before rejection can be implemented without breaking current owner-local
  accept flow. Newly added Task 9 net-owned paths avoid silent shard-0 fallback:
  missing owner rows under `SURGE_SHARDS>1` attach-miss instead of routing
  through shard 0.
- `RV2-DEBT-010` also stays open for copied-handle generation safety. Task 9
  canonicalizes copied listener handles, but copied connection handles still do
  not carry a generation/stable-id guard for direct public operations.

### Test Corrections

- `TestRuntimeV2AcceptOwnerShardLifecycleTraceContract` now treats Task 10/11
  fields as present-but-zero where Task 9 cannot honestly prove lifecycle
  behavior yet. In particular, `shutdown_poller_wakeups` is not faked from
  listener close.
- `TestRuntimeV2FDRegistryCancelledDuplicateReadWaiterPreservesLiveAndReregister`
  now runs with `SURGE_THREADS=1`, matching the neighboring cancellation
  lifecycle test. Its previous MT timing depended on the parent task cancelling
  a duplicate reader before the client sent data; under Task 9 scheduling that
  became order-sensitive and tested scheduler timing rather than fd-registry
  cancellation semantics.
- Added static guards for the two independent-review P1 fixes:
  `TestRuntimeV2AcceptReadinessClearsSiblingWaitKeys` and
  `TestRuntimeV2AcceptListenerRegistryGrowsUnderLock`.

### Review Pass

Independent review found:

- P1: listener canonical registry capacity was computed before taking
  `net_listener_registry_lock`, so concurrent `rt_net_listen` calls could
  append past capacity. Fixed by computing and ensuring capacity under the
  mutex; pinned by `TestRuntimeV2AcceptListenerRegistryGrowsUnderLock`.
- P1: a ready listener member could be overwritten by a later ready sibling
  before `rt_net_accept` consumed the winner. Fixed by clearing sibling accept
  wait keys in `rt_executor_wake_net_waiters_for_key_on_owner` after the first
  winning accept key is completed; pinned by
  `TestRuntimeV2AcceptReadinessClearsSiblingWaitKeys`.
- P2: accept tests were still trace-counter heavy. This remains partly true
  for downstream Task 10/11 lifecycle fields, but Task 9 now has focused static
  guards for the two concrete review bugs and runtime distribution tests for
  the owner-shard accept path.

### Files Touched

| Path | Effective LOC | Notes |
| --- | ---: | --- |
| `runtime/native/rt_net.c` | 877 | Legacy-ok under ceiling 904; group helper code lives outside this file. |
| `runtime/native/rt_net_accept_group.c` | 246 | New group accept wait/readiness/open-fd helper module. |
| `runtime/native/rt_net_accept_group.h` | 21 | New internal accept-group helper API. |
| `runtime/native/rt_net_handles.c` | 243 | Listener canonical registry plus existing handle allocation/member helpers. |
| `runtime/native/rt_async_waiter.c` | 386 | Owner-aware net waiter completion and accept sibling-key cleanup. |
| `runtime/native/rt_fd_registry.c` | 405 | Open-fd rows and owner-aware completion/drain support. |
| `runtime/native/rt_net_trace.c` | 148 | Added Task 9 proof counters. |

### Commands/Checks

| Command | Result |
| --- | --- |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2FDRegistry' -count=1 -parallel=1 -p=1 -v --timeout 180s` | Passed after making the duplicate-read cancellation fixture single-threaded. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Accept' -count=1 -parallel=1 -p=1 -v --timeout 240s` | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Accept(ReadinessClearsSiblingWaitKeys\|ListenerRegistryGrowsUnderLock\|NetOwnershipNoShard0Shortcut\|DynamicShardArrayShape)$' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `make runtime-v2-accept-check` | Passed. |
| `make c-check` | Passed. |
| `make cppcheck` | Passed. |
| `git diff --check` | Passed. |
| `./check_file_sizes.sh` | Passed; no file exceeded its effective LOC gate. |
| `make runtime-v2-check` | Passed. |
| `make check` | Passed. |
| `sentrux check .` | Passed, quality 6185. |
| `sentrux check runtime` | Passed, quality 5330. |
| `sentrux check runtime/native` | Passed, quality 5450. |

### Definition Of Done

- [x] The selected `SO_REUSEPORT` listener model is implemented for real, not
      promoted spike code.
- [x] Accepted connections register open fd rows in the accepting shard's
      `fd_registry`.
- [x] Accept continuations are placed on the winning owner shard with no
      public Surge syntax changes.
- [x] No accept-handoff fallback is presented as the hot path.
- [x] Non-owner access guard is explicitly deferred in `RV2-DEBT-013`, not left
      silent.
- [x] Relevant Task 4 accept distribution/static tests pass or were corrected
      to keep Task 10/11 lifecycle fields honest.

## Task 10: Per-Shard Poller And Wake Ownership

### Task Identity And Scope

- Task: `06-tasks/10-per-shard-poller-and-wake-ownership.md`.
- Kind: runtime code.
- Commit boundary: pending at evidence-write time.
- Scope: per-shard net wake pipes, per-shard net poll ownership, owner-shard
  wake routing for registry changes/close/cancellation/shutdown, and tests that
  prove cross-shard wake silence.
- Out of scope: Phase 4 cross-shard messaging, eventfd protocol, inbound
  queues, credits, seq-cst `PARKED`, lock sharding, and public Surge syntax.

### Poller Ownership Decision

Epic 6 uses shard-worker-owned net polling for `SURGE_SHARDS>1`.
This matches Task 7's actual worker shape: multishard mode starts one Tier 1
worker per shard, and each worker already owns its scheduler context and
`shard_id`. Adding a dedicated poller thread per shard would double the normal
thread count while still running under the preserved global `ex->lock`.

`rt_io_main` remains a single-shard/timer compatibility path. Its net polling
condition is gated to `shard_count <= 1`, so under `SURGE_SHARDS>1` it no
longer owns net readiness polling.

### Implementation

- Added `rt_net_poll_wake` by value on `rt_shard` and moved poll-in-progress
  state from `rt_executor.net_polling` to `rt_shard.net_polling`.
- Added `runtime/native/rt_net_poller.c` for per-shard wake and poller helper
  ownership:
  - `rt_net_poll_wake_init`
  - `rt_net_poll_wake_close`
  - `rt_net_poll_wake_drain`
  - `rt_net_wake_poll_on_shard`
  - `rt_net_wake_poll_all_shards`
  - `rt_net_has_waiters_on_shard`
  - `rt_net_begin_poll_on_shard`
  - `rt_net_poll_waiters_owned_on_shard`
- `rt_net_wake_poll_on_shard` writes only the target shard's pipe and returns
  an effective wake count. `EAGAIN`/`EWOULDBLOCK` counts as effective because a
  wake byte is already pending in that shard's pipe. Invalid owner/init/write
  failures return zero.
- `rt_net_wake_poll_all_shards` iterates configured shards and returns the sum
  of effective per-shard wakes.
- `rt_executor_request_shutdown` now traces the returned wake count after
  `rt_net_wake_poll_all_shards(ex)`, not raw `shard_count`.
- `poll_net_waiters_on_shard` now snapshots only the target shard's
  `fd_registry`, uses only that shard's `net_poll_scratch`, and drains only
  that shard's wake pipe.
- Owner-routed wake call sites now target the owning shard:
  `park_current`, waiter attach/detach notification, fd close wake, and
  shutdown.

### Tests And Gates

- Added `internal/vm/runtime_v2_net_poller_static_test.go`.
- Added `TestRuntimeV2NetPollerPerShardWakeBehavior`, which compiles a C
  harness including production `rt_net_poller.c`, creates real nonblocking
  pipes, verifies a shard-1 wake does not wake shard 0, verifies all-shards
  wake returns two effective wakes, and checks shard-local waiter/polling
  state.
- Added NetPoller tests to `make runtime-v2-accept-check`, so
  `make runtime-v2-check` now carries the Task 10 static/behavior proof.
- Updated fd-registry shutdown harnesses away from the old
  `rt_net_wake_poll(void)` API and pinned trace accounting to the actual
  all-shards wake return value.

### Review Pass

Independent review initially found:

- P1: `shutdown_poller_wakeups` was traced as raw `shard_count` before the
  wake call and could report wakes that did not happen.
- P1: Task 10 proof was mostly string/static checks and did not prove real
  per-shard pipe behavior or cross-shard wake silence.
- P2: `make cppcheck` failed on const-pointer style and `rt_async_state.c`
  exceeded its legacy LOC ceiling.

Fixes:

- `rt_net_wake_poll_on_shard`/`rt_net_wake_poll_all_shards` now return
  effective counts; shutdown traces the returned count.
- `TestRuntimeV2NetPollerPerShardWakeBehavior` uses production
  `rt_net_poller.c` and real pipes for the cross-silence proof.
- `rt_net_poller.c` owns the new helpers, bringing `rt_async_state.c` back
  under its legacy LOC ceiling.

Re-review found no remaining P0/P1 code blockers. The only remaining review
note was operational: include the new untracked files in the Task 10 commit.

### Files Touched

| Path | Effective LOC | Notes |
| --- | ---: | --- |
| `runtime/native/rt_net_poller.c` | 133 | New per-shard wake/poller helper module. |
| `runtime/native/rt_async_state.c` | 1717 | Legacy-ok under ceiling 1727 after helper extraction. |
| `runtime/native/rt_net.c` | 815 | Legacy-ok under ceiling 904; shard-local polling remains here. |
| `runtime/native/rt_async_internal.h` | 459 | Adds per-shard wake/poll fields and helper prototypes. |
| `runtime/native/rt_async_waiter.c` | 419 | Owner-routed waiter attach/detach wake. |
| `runtime/native/rt_fd_registry.c` | 405 | Owner-routed close/shutdown wake. |

### Commands/Checks

| Command | Result |
| --- | --- |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2NetPoller' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2(AcceptNetOwnershipNoShard0Shortcut\|FDRegistry(StaticBoundary\|CloseWakePollNotificationProof\|ShutdownDrainBehavior\|ShutdownDrainStaticContract)\|NetPoller)' -count=1 -parallel=1 -p=1 -v --timeout 180s` | Passed. |
| `make c-check` | Passed. |
| `make cppcheck` | Passed. |
| `./check_file_sizes.sh` | Passed; `rt_async_state.c` is back under its legacy ceiling. |
| `git diff --check` | Passed. |
| `make runtime-v2-check` | Passed. |
| `make check` | Passed. |
| `sentrux check .` | Passed, quality 6185. |
| `sentrux check runtime` | Passed, quality 5326. |
| `sentrux check runtime/native` | Passed, quality 5465. |

### Definition Of Done

- [x] The poller-ownership model is chosen and justified against Task 7's
      worker shape.
- [x] Every shard that owns net fds has per-shard wake state and per-shard
      poll-in-progress state.
- [x] Process-global wake pipe statics and old `rt_net_wake_poll(void)` /
      `poll_net_waiters(rt_executor*, int)` surfaces are removed.
- [x] `SURGE_SHARDS=1` keeps single-shard compatibility through shard 0's
      struct-owned wake pipe.
- [x] `SURGE_SHARDS>1` net polling is shard-worker-owned; `rt_io_main` does
      not own multishard net polling.
- [x] Cross-shard wake silence is proven with a C behavior harness using real
      per-shard pipes.
- [x] Shutdown wakes all shard pollers and traces the actual effective wake
      count.
- [x] No Phase 4 primitive was implemented or pre-decided.

## Task 11: Multishard Net Lifecycle Migration

### Task Identity And Scope

- Task: `06-tasks/11-multishard-net-lifecycle-migration.md`.
- Kind: runtime code.
- Commit boundary: pending at evidence-write time.
- Scope: owner-shard net waiter storage and lifecycle completion for
  read/write/accept readiness, close, cancellation cleanup, and shutdown.
- Out of scope: public syntax, cross-shard messaging, lock sharding, moving
  non-net waiter kinds off global/shard-0 compatibility, and stable public net
  handle generation guards (`RV2-DEBT-010`).

### Implementation

- Added explicit waiter-store accessors:
  `rt_executor_waiter_store_for_shard` and
  `rt_executor_waiter_store_const_for_shard`.
- Kept `rt_executor_waiter_store` and its const twin as explicit shard-0
  compatibility accessors for non-net waiters.
- Net-key add/remove/pop/wake in `rt_async_waiter.c` now resolves through the
  fd owner shard's waiter store:
  - `add_waiter` stores net waiters in the owner shard's waiter store.
  - `rt_executor_wake_net_waiters_for_key_on_owner` consumes only the owner
    shard's waiter rows and places accepted connection continuations on that
    owner shard.
  - `remove_waiter` scans all shard waiter stores for net-key stale cleanup,
    but detaches fd-registry interest only on the current owner shard when the
    removed row came from that owner store.
  - `pop_waiter` keeps non-net FIFO behavior on the global compatibility store
    while net keys resolve through the owner store.
- Removed the hardcoded owner-0 shutdown wrapper
  `rt_fd_registry_drain_shutdown_net_waiters_locked`; only the owner-explicit
  `_on_owner` surface remains.
- Added `rt_async_trace_waiters.c` and `rt_waiter_trace_counts` so
  `TRACE_EXEC_SNAPSHOT waiters*` aggregates all shard waiter stores. This keeps
  trace snapshots honest after net waiters move off shard 0.
- Updated `runtime-v2-waiter-check` to include owner-local net waiter behavior
  and trace aggregation tests.

### Review Pass

The first independent review found a P1 fd-reuse lifecycle hole:

- If fd `N` was closed on shard 1, then reused/registered on shard 0 before
  delayed cleanup, the initial implementation could resolve cleanup through
  the new shard-0 row and leave the old shard-1 waiter stranded.
- The same fd-only detach path could remove interest from the new fd lifetime
  on another shard.

Fix:

- Net removal now scans all shard waiter stores for stale cleanup, but
  fd-registry detach is owner-explicit through
  `fd_registry_bridge_net_detach_if_last_on_owner`.
- The owner-local C behavior harness now covers this cross-owner fd-reuse case:
  stale shard-1 waiter cleanup must not clear the newly registered shard-0 fd
  interest.

The same review also found two P2 issues:

- `TestRuntimeV2OwnerLocalNetWaiterBehavior` was not in the Runtime V2 gate.
  It is now part of `make runtime-v2-waiter-check`.
- `TRACE_EXEC_SNAPSHOT` counted only shard-0 waiters. It now uses
  `rt_trace_collect_waiter_counts` to aggregate all configured shard stores.

Targeted re-review found no remaining blockers. The only operational note was
to include the new `runtime/native/rt_async_trace_waiters.c` file in the
commit.

### Baseline Debt Checked

The sync-helper fallback probe remains partly red in the current baseline and
is tracked by `RV2-DEBT-002`:

- On Task 11 worktree, `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` timed out
  after their 10s program timeout.
- The same two tests also timed out at clean Task 10 commit `0d206ed2` in a
  detached worktree at `/tmp/surge-task10-baseline-task11`.
- `TestMTBlockingChannelHelpersAllowTimersToAdvance` passed when run alone on
  the Task 11 worktree and is also covered by `make runtime-v2-check`.

This is not classified as a Task 11 regression. It stays open under
`RV2-DEBT-002`.

### Files Touched

| Path | Effective LOC | Notes |
| --- | ---: | --- |
| `runtime/native/rt_async_waiter.c` | 488 | Net waiters resolve through owner shard stores; stale cleanup scans all shard stores; detach is owner-explicit. |
| `runtime/native/rt_async_trace.c` | 473 | Snapshot uses aggregated waiter trace counts. |
| `runtime/native/rt_async_trace_waiters.c` | 30 | New read-only trace aggregation helper. |
| `runtime/native/rt_async_internal.h` | 472 | Adds shard waiter-store accessors and waiter trace count struct. |
| `runtime/native/rt_runtime.c` | 281 | Implements shard-indexed waiter-store accessors. |
| `runtime/native/rt_fd_registry.c` | 401 | Removes owner-0 shutdown drain wrapper; owner-explicit drain remains. |
| `runtime/native/rt_fd_registry.h` | 78 | Removes owner-0 shutdown drain declaration. |
| `runtime/native/rt_net.c` | 815 | Not touched by Task 11; still legacy-ok under ceiling 904. |
| `internal/vm/runtime_v2_owner_local_waiter_static_test.go` | n/a | Adds owner-local behavior harness, fd-reuse regression, and trace aggregation static proof. |
| `internal/vm/runtime_v2_accept_static_test.go` | n/a | Static gate now pins owner-explicit detach helper name. |
| `Makefile` | n/a | `runtime-v2-waiter-check` now includes owner-local net waiter and trace aggregation tests. |

### Commands/Checks

| Command | Result |
| --- | --- |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2OwnerLocalNetWaiterBehavior$' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run 'TestRuntimeV2AcceptNetOwnershipNoShard0Shortcut\|TestRuntimeV2OwnerLocal(WaiterSkeletonStaticShape\|TraceAggregatesShardWaiters\|NetWaiterBehavior)' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `make runtime-v2-waiter-check` | Passed; includes owner-local net waiter behavior, fd-reuse regression, trace aggregation, and non-net waiter contracts. |
| `make runtime-v2-check` | Passed. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestMT(WakeupsAndCancellation\|CorrectnessWakeups\|StructuredConcurrency\|BlockingPool)' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestMTCorrectnessChannels' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Passed. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTBlockingChannelHelpersAllowTimersToAdvance$' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Passed. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTBlockingChannelHelpers(DoNotParkWorkers\|DrainReadyWorkAtCompensationLimit)$' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Expected-red baseline debt; both timed out and reproduced at Task 10 commit `0d206ed2`. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -count=1 -parallel=1 -p=1 -v --timeout 90s` | Passed. |
| `go build -o /tmp/surge-task11-bench ./cmd/surge && timeout 120s env SURGE=/tmp/surge-task11-bench SURGE_NET_BENCH_THREADS=1 SURGE_NET_BENCH_MODES=direct SURGE_NET_BENCH_PATTERNS=seq SURGE_NET_BENCH_REQUESTS=64 SURGE_NET_BENCH_REPORT=/tmp/surge-task11-native-net-smoke.md ./scripts/bench_native_net.sh` | Passed; smoke row: 64 requests, total 14686 us, avg 186.62 us/op, p50 147.26 us, p95 350.56 us; full benchmark evidence remains Task 12. |
| `make c-check` | Passed. |
| `make cppcheck` | Passed. |
| `git diff --check` | Passed. |
| `./check_file_sizes.sh` | Passed; all touched runtime files are under the effective LOC gate. |
| `./check_file_sizes.sh --self-test` | Passed. |
| `make check` | Passed. |
| `sentrux check .` | Passed, quality 6184. |
| `sentrux check runtime` | Passed, quality 5329. |
| `sentrux check runtime/native` | Passed, quality 5466. |

### Definition Of Done

- [x] Every net-owned fd-registry entry point resolves the connection's actual
      owner shard, not shard 0, under `SURGE_SHARDS>1`.
- [x] Close, cancellation, readiness completion, and shutdown for a connection
      use the owner shard's fd registry and waiter store.
- [x] Shutdown drains every shard's net waiters through owner-explicit drain
      calls.
- [x] Non-net waiter compatibility is preserved and re-verified with
      cancellation/join/timeout/channel/blocking probes.
- [x] Remaining relevant Task 4/static accept gates pass.
- [x] Line-count impact is recorded; `rt_net.c` did not grow.

## Task 12: Trace Counters And Benchmark Evidence

### Task Identity And Scope

- Task: `06-tasks/12-trace-counters-and-benchmark-evidence.md`.
- Kind: trace/benchmark.
- Commit boundary: pending at evidence-write time.
- Scope: shard-aware trace counters, multishard native net benchmark rows, and
  the liveness fix required to make the Task 12 accept evidence observable.
- Out of scope: CI wiring (Task 13), lock sharding, Phase 4 messaging, public
  syntax/API changes, and 10k connection stress promotion.

### Implementation

- `TRACE_NET` now includes aggregate shard-aware fields:
  `runtime_shards`, `accept_owner_total`, `accept_owner_min`,
  `accept_owner_max`, `accept_owner_imbalance`,
  `global_path_fallbacks`, `fd_ready_batches`,
  `fd_ready_batch_fds_total`, and `fd_ready_batch_fds_max`.
- A stable `TRACE_NET_SHARDS` line records per-shard `accept_N`,
  `fd_ready_batches_N`, and `fd_ready_fds_N` fields. This avoids adding 64
  fields to the primary `TRACE_NET` row while preserving key/value parsing.
- `SCHED_TRACE` now includes `tier1_steal_denied`,
  `conn_owner_placed`, `conn_owner_local`, and `conn_owner_mismatch`.
- The no-steal counter increments at the same branch that restores a
  connection-owned task to the victim queue before returning without a
  successful steal.
- `scripts/bench_native_net.sh` now supports explicit
  `SURGE_NET_BENCH_SHARDS` and `SURGE_NET_BENCH_CONNECTIONS`, records both
  `SURGE_SHARDS` and `SURGE_THREADS` in rows, verifies the supplied Surge
  binary commit against the checkout, and reports the new counters. Its
  default path preserves the old single-connection benchmark matrix; Task 12
  multishard evidence is requested explicitly by env.
- `benchmarks/native/net_request_reply/main.sg` now accepts N connections and
  handles each accepted connection through `@local spawn`, so benchmark rows
  can exercise owner-local connection tasks without adding language syntax.

### Liveness Bug Found And Fixed

Task 12 validation exposed an existing multishard accept liveness gap:

- A single accept task registers waiters for every listener-group fd. `park`
  previously woke only the first net key's owner poller.
- Worker-owned multishard net polling used a short poll slice and then could
  park on `ready_cv` even while its own shard still had net waiters. Since no
  later ready signal is guaranteed for pure socket readiness, accept waiters
  could remain registered without ever reaching `net-accept-ready`.
- The failure reproduced at clean Task 11 commit `74ad7b46` in detached
  worktree `/tmp/surge-task12-baseline`, so it was not introduced by the
  Task 12 trace counters.

Fix:

- `rt_net_wake_poll_for_task_wait_keys` wakes every owner shard represented
  in the current task's net wait-key list, with a fallback to the parked key.
- Multishard worker polling now continues polling while
  `rt_net_has_waiters_on_shard(ex, ctx->shard_id)` remains true instead of
  sleeping on `ready_cv` after a single timeout slice.
- `TestRuntimeV2NetPollerPerShardWakeBehavior` now covers the multi-key wake
  helper in its C harness.

### Benchmark Evidence

Report path:
`build/benchmarks/runtime-v2-task12-native-net.md`.

Command:

```bash
go build -ldflags "$(./scripts/ldflags.sh --local)" -o /tmp/surge-task12-bench ./cmd/surge
timeout 300s env \
  SURGE=/tmp/surge-task12-bench \
  SURGE_NET_BENCH_SHARDS="1 8" \
  SURGE_NET_BENCH_CONNECTIONS="1 8 32 1024 10000" \
  SURGE_NET_BENCH_REQUESTS=8 \
  SURGE_NET_BENCH_MODES=direct \
  SURGE_NET_BENCH_PATTERNS=seq \
  SURGE_NET_BENCH_CLIENT_PARALLEL=128 \
  SURGE_NET_BENCH_RUN_TIMEOUT=60s \
  SURGE_NET_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task12-native-net.md" \
  ./scripts/bench_native_net.sh
```

Selected rows:

| shards | threads | connections | total requests | total us | avg us/op | p50 us | p95 us |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1 | 1 | 8 | 3536 | 182.34 | 140.35 | 267.30 |
| 1 | 1 | 8 | 64 | 15553 | 1077.84 | 938.22 | 2293.13 |
| 1 | 1 | 32 | 256 | 55870 | 2790.74 | 2681.44 | 5026.52 |
| 1 | 1 | 1024 | 8192 | 1516521 | 22292.61 | 20245.62 | 35968.24 |
| 8 | 8 | 1 | 8 | 3834 | 201.52 | 147.99 | 301.14 |
| 8 | 8 | 8 | 64 | 18489 | 1120.44 | 444.96 | 3542.41 |
| 8 | 8 | 32 | 256 | 55465 | 4586.95 | 3594.86 | 14059.59 |
| 8 | 8 | 1024 | 8192 | 2371469 | 31224.46 | 1161.00 | 76692.39 |

Trace proof highlights:

| shards | connections | runtime shards | sched steal | denied steals | accept total | active accept shards | imbalance | global fallbacks |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1024 | 1 | 0 | 0 | 1024 | 1 | 0 | 0 |
| 8 | 1 | 8 | 0 | 0 | 1 | 1 | 1 | 0 |
| 8 | 8 | 8 | 0 | 0 | 8 | 4 | 3 | 0 |
| 8 | 32 | 8 | 0 | 0 | 32 | 8 | 4 | 0 |
| 8 | 1024 | 8 | 0 | 0 | 1024 | 8 | 36 | 0 |

High-load distribution row:

```text
accept_0=133 accept_1=136 accept_2=136 accept_3=126 accept_4=108 accept_5=133 accept_6=144 accept_7=108
```

Interpretation:

- Low connection counts show expected `SO_REUSEPORT` skew and are not judged
  as distribution failures.
- The 1024-connection row exercises every accept owner shard with moderate
  imbalance (`max-min=36`).
- 8-shard throughput is worse than the 1-shard row in this benchmark. That is
  acceptable under the Epic 6 boundary because the global executor lock is
  still preserved; the Task 12 proof is locality/ownership visibility and no
  shard-0 fallback, not line-rate scaling.
- 10k rows were skipped by the script's safety default:
  `10k row disabled by default; set SURGE_NET_BENCH_TRY_10K=1 after fd-limit
  and timeout checks`.

### Files Touched

| Path | Effective LOC | Notes |
| --- | ---: | --- |
| `runtime/native/rt_net_trace.c` | 276 | New aggregate and per-shard net trace counters. |
| `runtime/native/rt_net_trace.h` | 125 | Inline trace hooks for runtime shards, fallback, and fd-ready batches. |
| `runtime/native/rt_async_trace.c` | 518 | New `SCHED_TRACE` counters. |
| `runtime/native/rt_async_state.c` | 1722 | Legacy-ok under ceiling 1727; no-steal trace hook and net-poller liveness loop. |
| `runtime/native/rt_net_poller.c` | 158 | Multi-key net poller wake helper and per-shard wake behavior proof surface. |
| `runtime/native/rt_net.c` | 818 | FD readiness batch trace hook in shard-local poller. |
| `runtime/native/rt_scheduler_placement.c` | 91 | No-steal denied trace helper and connection placement counter. |
| `scripts/bench_native_net.sh` | n/a | Explicit shard/connection matrix, stale-binary guard, trace columns, 10k skip. |
| `benchmarks/native/net_request_reply/main.sg` | n/a | N-connection benchmark fixture with `@local spawn` handlers. |
| `internal/vm/*runtime_v2*` | n/a | Trace assertions and liveness/static harness updates. |

### Commands/Checks

| Command | Result |
| --- | --- |
| `bash -n scripts/bench_native_net.sh` | Passed. |
| `git diff --check` | Passed. |
| `./check_file_sizes.sh --self-test` | Passed. |
| `./check_file_sizes.sh` | Passed; all touched runtime files under effective LOC gate or legacy ceiling. |
| `make c-check` | Passed. |
| `make cppcheck` | Passed. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^(TestRuntimeV2Accept(ShardConfigInitializesRequestedShardCount\|DistributionAcrossOwnerShards\|OwnerShardLifecycleTraceContract)\|TestRuntimeV2SchedulerPlacement(NoStealPolicy\|NoStealWorkerPath\|StealPathSourceGate))$' -count=1 -parallel=1 -p=1 -v --timeout 240s` | Passed. |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2NetPollerPerShardWakeBehavior$' -count=1 -parallel=1 -p=1 -v --timeout 120s` | Passed after static harness update. |
| `go build -ldflags "$(./scripts/ldflags.sh --local)" -o /tmp/surge-task12-bench ./cmd/surge && timeout 180s env SURGE=/tmp/surge-task12-bench SURGE_NET_BENCH_SHARDS="1 4" SURGE_NET_BENCH_CONNECTIONS="1 8" SURGE_NET_BENCH_REQUESTS=8 SURGE_NET_BENCH_MODES=direct SURGE_NET_BENCH_PATTERNS=seq SURGE_NET_BENCH_REPORT=/tmp/surge-task12-smoke.md ./scripts/bench_native_net.sh` | Passed. |
| `timeout 300s make runtime-v2-check` | Passed on final rerun. First attempt failed in `TestRuntimeV2NetPollerPerShardWakeBehavior` because the isolated C harness lacked stubs for the new helper's dependencies; harness was fixed and the full rerun passed. |
| `timeout 300s make check` | Passed. |
| `timeout 300s env SURGE=/tmp/surge-task12-bench ... ./scripts/bench_native_net.sh` | Passed; report written to `build/benchmarks/runtime-v2-task12-native-net.md`. |
| `sentrux check .` | Passed, quality 6182. |
| `sentrux check runtime` | Passed, quality 5340. |
| `sentrux check runtime/native` | Passed, quality 5467. |

### Review Pass

- Implementer subagent produced a plan, then implementation; main-agent
  validation found the multishard accept liveness bug and fixed it.
- Independent review/test subagent found no blockers after the fix. Its bounded
  command set passed, including C checks, cppcheck, focused Go tests, LOC
  checker, and a short benchmark smoke at 1/4 shards and 1/8 connections.

### Definition Of Done

- [x] Shard-aware trace counters exist for shard count, accepted connections
      by shard, connection-task placement, denied Tier 1 steals, global
      fallback usage, fd readiness batches, and imbalance.
- [x] Benchmark rows exist for 1, 8, 32, and 1024 connections on 1 and 8
      shards; 10k rows are explicitly skipped with a safety reason.
- [x] Rows use a freshly built current-checkout Surge binary.
- [x] Throughput and skew are interpreted against the preserved global-lock
      boundary and `SO_REUSEPORT` low-count skew.
- [x] `global_path_fallbacks=0` for net-owned multishard rows.
- [x] Sentrux root, `runtime`, and `runtime/native` scans pass and are
      recorded.

## Task 13: Runtime V2 Accept CI Gates

### Task Identity And Scope

- Task: `06-tasks/13-runtime-v2-accept-ci-gates.md`.
- Kind: CI.
- Commit boundary: Task 13 closeout commit.
- Scope: Makefile test selection plus docs for the stable accept CI gate.
- Out of scope: runtime C changes, Go test behavior changes, broad VM regex
  stabilization, benchmark promotion, and copied/raw net-handle owner guards.

### Implementation

- `runtime-v2-accept-check` already existed and was already called by
  `runtime-v2-check`; Task 13 refined the target rather than creating it from
  scratch.
- The target now runs sequential focused commands, each with
  `SURGE_BACKEND=llvm`, `SURGE_SKIP_TIMEOUT_TESTS=0`, `-count=1`,
  `-parallel=1`, `-p=1`, and an explicit Go timeout.
- Tagged contracts still run with `-tags runtime_v2_pending`. The untagged
  `SURGE_SHARDS=1` compatibility floor runs without the pending tag.
- `LIVENESS_PROBES.md` now records the accept CI liveness gate and the Task 12
  shard-aware trace fields.

### Selected Test Subset

| Group | Tests | Rationale |
| --- | --- | --- |
| Single-shard compatibility | `TestRuntimeV2AcceptShardOneNativeNetCompatibility` | Proves Epic 6 did not break observable native net behavior under `SURGE_SHARDS=1`. |
| Accept config and metadata | `TestRuntimeV2AcceptShardConfigInitializesRequestedShardCount`, `TestRuntimeV2AcceptRejectsInvalidShardConfig`, `TestRuntimeV2AcceptRejectsConflictingThreadCount`, `TestRuntimeV2NetMetadataStaticShape`, `TestRuntimeV2NetMetadataMultiShardListenClose` | Proves requested shard count, invalid config rejection, thread-count conflict rejection, and stable listener/connection owner metadata. |
| Accept owner/static contracts | `TestRuntimeV2AcceptNetOwnershipNoShard0Shortcut`, `TestRuntimeV2AcceptDynamicShardArrayShape`, `TestRuntimeV2AcceptReadinessClearsSiblingWaitKeys`, `TestRuntimeV2AcceptListenerRegistryGrowsUnderLock` | Keeps the static no-shard-0 shortcut gate plus the deterministic accept-readiness and listener-registry fixes. |
| Task 12 accept trace contracts | `TestRuntimeV2AcceptDistributionAcrossOwnerShards`, `TestRuntimeV2AcceptOwnerShardLifecycleTraceContract` | Proves per-shard accept distribution, `global_path_fallbacks=0`, owner registry/readiness/close/cancel/shutdown trace fields, and listener-group close accounting. |
| Per-shard net poller | `TestRuntimeV2NetPollerPerShardWakeShape`, `TestRuntimeV2NetPollerPerShardWakeBehavior`, `TestRuntimeV2NetPollerShardLocalPollInput`, `TestRuntimeV2NetPollerGlobalIOThreadDoesNotOwnMultiShardNetPolling`, `TestRuntimeV2NetPollerShutdownWakesEveryShard` | Proves owner-shard wake, shard-local poll input, global I/O exclusion for multishard net polling, and shutdown wake across shards. |
| Scheduler placement/no-steal | `TestRuntimeV2SchedulerPlacementWorkerShape`, `TestRuntimeV2SchedulerPlacementNoStealPolicy`, `TestRuntimeV2SchedulerPlacementNoStealWorkerPath`, `TestRuntimeV2SchedulerPlacementStealPathSourceGate` | Proves one worker per shard, helper-level no-steal policy, real worker-path no-steal behavior, and source ordering before `SCHED_TRACE` records a steal. |

### Exclusions

- The broad accepted-debt command
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` remains excluded under
  `RV2-DEBT-001`.
- Timing-sensitive live `SIGUSR1` probes, benchmark rows, 10k stress, and
  heavy/manual load evidence stay local-only.
- `TestRuntimeV2SchedulerPlacementInvalidOwnerFailsClosed` remains useful but
  is outside the narrow accept CI minimum.
- `TestRuntimeV2SchedulerPlacementParkedWithWorkInvariant` and
  `TestRuntimeV2SchedulerPlacementParkedWithWorkSourceGate` remain broader
  scheduler liveness checks, not accept-ownership CI checks.
- `RV2-DEBT-013` stays open and was not edited. Task 13 does not close copied
  or raw net-handle owner-generation guard debt.

### Commands/Checks

| Command | Result |
| --- | --- |
| Independent review/test subagent | Passed with no findings. The reviewer verified the Makefile target shape, docs alignment, declared exclusions, `make -n runtime-v2-accept-check`, and `timeout 300s make runtime-v2-accept-check`. |
| `timeout 300s make runtime-v2-accept-check` | Passed in implementation and independent review. Reviewer run groups passed as untagged compatibility (`ok surge/internal/vm 3.318s`), tagged accept/metadata/static contracts (`ok surge/internal/vm 18.311s`), tagged net-poller contracts (`ok surge/internal/vm 0.176s`), and tagged scheduler placement/no-steal contracts (`ok surge/internal/vm 5.932s`). |
| `timeout 600s make runtime-v2-check` x3 | Passed three consecutive main-session runs. Each run included the new accept CI gate through the top-level chain. |
| `timeout 600s make check` | Passed, including Go tests, lint, C checks, and the effective LOC gate. |

### Definition Of Done

- [x] `runtime-v2-accept-check` exists in `Makefile`.
- [x] It remains wired into `runtime-v2-check`.
- [x] The target promotes the current stable Task 4/5/12 accept contracts and
      scheduler placement/no-steal contracts.
- [x] `LIVENESS_PROBES.md` records the new gate.
- [x] Broad regex debt is not silently required by the new gate.
- [x] The selected subset is stable across three consecutive full-chain local
      runs.

## Task 14: Large-File Refactor Tranche

### Task Identity And Scope

- Task: `06-tasks/14-large-file-refactor-tranche.md`.
- Kind: refactor code.
- Commit boundary: Task 14 closeout commit.
- Scope: large-file audit for Epic 6 runtime files, dependency-boundary
  decision, `.loc-legacy-allowlist` tightening, debt/evidence/task docs.
- Out of scope: runtime behavior changes, speculative code movement, lock
  splitting, cross-shard messaging, and non-Epic-6 legacy runtime files.

### Decision

No new runtime-code extraction was performed.

Tasks 6-12 already moved the cohesive Epic 6 responsibilities out of the
large files:

| Responsibility | Owning file after Epic 6 |
| --- | --- |
| Owner-shard placement and no-steal helpers | `runtime/native/rt_scheduler_placement.c` |
| Per-shard poller wake helpers | `runtime/native/rt_net_poller.c` |
| Listener-group member bookkeeping | `runtime/native/rt_net_accept_group.c` |
| Listener/connection handle shapes and owner metadata | `runtime/native/rt_net_handles.c` |
| Owner-shard close and fd-registry lifecycle helpers | `runtime/native/rt_net_lifecycle.c` |
| Listener socket construction and `SO_REUSEPORT` setup | `runtime/native/rt_net_listener_socket.c` |
| Aggregate and per-shard net trace counters | `runtime/native/rt_net_trace.c`, `runtime/native/rt_net_trace.h` |

The remaining large files are all flat or reduced against the Task 1 baseline
by effective LOC, so another split would add churn without an observed
large-file regression.

### Line Counts

| Path | Task 1 baseline | Final physical LOC | Final effective LOC | Outcome |
| --- | ---: | ---: | ---: | --- |
| `runtime/native/rt_net.c` | 904 | 868 | 818 | Reduced; legacy ceiling lowered `904 -> 818`. |
| `runtime/native/rt_async_state.c` | 1727 | 1850 | 1722 | Reduced by effective LOC; legacy ceiling lowered `1727 -> 1722`. |
| `runtime/native/rt_async_task.c` | 768 | 770 | 731 | Reduced by effective LOC; legacy ceiling lowered `768 -> 731`. |
| `runtime/native/rt_async_internal.h` | 499 | 542 | 478 | Under effective 500-line target; no allowlist entry needed. |
| `runtime/native/rt_runtime.c` | 202 | 328 | 281 | Under target. |
| `runtime/native/rt_fd_registry.c` | 409 | 463 | 401 | Under target. |
| `runtime/native/rt_fd_registry.h` | 113 | 119 | 78 | Under target. |
| `runtime/native/rt_async_waiter.c` | n/a | 546 | 488 | Under target. |
| `runtime/native/rt_async_trace.c` | n/a | 558 | 518 | Under 575 OK band. |
| `runtime/native/rt_net_poller.c` | n/a | 170 | 158 | Under target. |
| `runtime/native/rt_net_accept_group.c` | n/a | 260 | 246 | Under target. |
| `runtime/native/rt_net_handles.c` | n/a | 260 | 243 | Under target. |
| `runtime/native/rt_net_lifecycle.c` | n/a | 94 | 89 | Under target. |
| `runtime/native/rt_net_listener_socket.c` | n/a | 110 | 103 | Under target. |
| `runtime/native/rt_scheduler_placement.c` | n/a | 99 | 91 | Under target. |

### Rejected Extraction Paths

- `rt_net.c` poll construction: plausible future work, but Epic 6 already
  extracted wake helpers, handles, listener-group state, listener socket
  construction, lifecycle helpers, and trace counters. Moving the remaining
  `poll_net_waiters_on_shard` loop now would split direct wait-key semantics
  from the socket wait API without a current LOC regression.
- `rt_async_state.c` scheduler loop: placement/no-steal helpers already live in
  `rt_scheduler_placement.c`. The remaining ready/wake/timer/executor loop is
  still intentionally global under `ex->lock`; lock splitting is the future
  boundary, not this tranche.
- `rt_async_internal.h`: effective LOC is below target, and the declarations
  remain the shared internal runtime surface while the executor lock is global.

### Debt And Allowlist Updates

- `.loc-legacy-allowlist` lowered `rt_net.c` from `904` to `818`.
- `.loc-legacy-allowlist` lowered `rt_async_state.c` from `1727` to `1722`.
- `.loc-legacy-allowlist` lowered `rt_async_task.c` from `768` to `731`.
- `RV2-DEBT-003`, `RV2-DEBT-004`, and `RV2-DEBT-005` remain open because those
  files are still over the 500-line target, but their ceilings are now stricter
  and match the current effective counts.

### Commands/Checks

| Command | Result |
| --- | --- |
| `./check_file_sizes.sh -a` | Passed before docs update; relevant effective LOC listed above. |
| `wc -l ...runtime/native...` | Passed; physical LOC listed above for context. |
| `git diff --check` | Passed. |
| `./check_file_sizes.sh --self-test` | Passed. |
| `./check_file_sizes.sh -a` after allowlist tightening | Passed; 708 files checked, 0 bad, and tightened entries reported `rt_net.c <=818`, `rt_async_state.c <=1722`, `rt_async_task.c <=731`. |
| `make c-check` | Passed. |
| `make cppcheck` | Passed. |
| `timeout 600s make runtime-v2-check` | Passed, including the Task 13 accept CI gate. |
| `timeout 600s make check` | Passed, including Go tests, lint, C checks, and the effective LOC gate. |
| `sentrux check .` | Passed, quality 6182. |
| `sentrux check runtime` | Passed, quality 5340. |
| `sentrux check runtime/native` | Passed, quality 5467. |

### Definition Of Done

- [x] Every file Tasks 6-11 touched has a recorded before/final line count.
- [x] No new catch-all file was created.
- [x] No runtime code was changed for this task.
- [x] Each remaining large file has a recorded rejected extraction reason or an
      existing focused owner module.
- [x] `.loc-legacy-allowlist` reflects the final effective LOC ceilings.

## Task 15: Epic Closeout And Static Gates

### Task Identity And Scope

- Task: `06-tasks/15-epic-closeout-and-static-gates.md`.
- Kind: closeout.
- Commit boundary: Epic 6 closeout commit.
- Scope: final standing gates, final benchmark confirmation, Sentrux signals,
  contract accounting, durable docs, debt ownership, and Epic 7 handoff.
- Out of scope: new runtime implementation work.

### Closeout State

- Closeout started from clean commit `01a4f7e5`.
- No runtime C, Go, stdlib, parser, semantic, lowering, or public syntax files
  were changed in Task 15.
- `docs/RUNTIME_V2.md` Phase 3 now records the Epic 6 completion boundary.
- `README.md`, this epic document, `06-tasks/README.md`, `DEBT.md`,
  `NOTES.md`, and this evidence file now reflect the final Epic 6 state.

### Accept Ownership Contract Accounting

| Contract bullet | Status | Evidence |
| --- | --- | --- |
| `SURGE_SHARDS=1` preserves current observable native net behavior. | Done | `TestRuntimeV2AcceptShardOneNativeNetCompatibility` passed in `runtime-v2-accept-check`. |
| `SURGE_SHARDS=N`, `N>1`, initializes exactly `N` shards or fails explicitly. | Done | `TestRuntimeV2AcceptShardConfigInitializesRequestedShardCount` and invalid config tests passed. |
| Multi-shard mode uses one Tier 1 worker per shard and rejects conflicting `SURGE_THREADS`. | Done | `TestRuntimeV2SchedulerPlacementWorkerShape` and `TestRuntimeV2AcceptRejectsConflictingThreadCount` passed. |
| `rt_executor.lock` remains the single state lock; no lock-level scalability claim. | Done | Epic document and `docs/RUNTIME_V2.md` closeout state this boundary; performance interpretation is calibrated to it. |
| Each shard owns scheduler state, waiter store, fd registry, net poll scratch, heap accounting cells, and trace counters. | Done | Tasks 6-12 implementation evidence plus `runtime-v2-check` gates passed. |
| Listener object records single-fd, per-shard group, or fallback form. | Done | Task 8 metadata/static tests and `TestRuntimeV2NetMetadataMultiShardListenClose` passed. |
| Per-shard listener group closes as one logical handle and records Linux queued-accept behavior. | Done | Task 8/12 evidence and epic closeout document record group-close semantics and OS drop caveat. |
| Each accepted connection has one owner shard at creation. | Done | `TestRuntimeV2AcceptDistributionAcrossOwnerShards` and trace fields passed. |
| Accepted connection fd is registered in the owning shard's fd registry. | Done | Owner-local net waiter behavior and accept lifecycle trace contracts passed. |
| Read, write, close, cancellation, and shutdown use owner shard registry/waiter state. | Done | Task 11 owner-local lifecycle tests and Task 12 trace contracts passed. |
| Local spawn from a request task inherits the current shard. | Done | Scheduler placement worker-path tests passed. |
| Task acting on a `TcpConn`/`TcpListener` must be owner-local unless future migration exists. | Done with debt | Owner-local accept flow is enforced for Epic 6 runtime-generated connection tasks; copied/raw public handle cases stay open under `RV2-DEBT-010` and `RV2-DEBT-013`. |
| Non-owner task must not silently operate through shard 0 or implicit fallback. | Done with debt | Runtime trace gates require `global_path_fallbacks=0`; raw copied handle rejection remains `RV2-DEBT-010`/`RV2-DEBT-013`. |
| Tier 1 connection tasks are not stolen by non-owner shards. | Done | `SCHED_TRACE steal=0`, denied steal counters, and scheduler no-steal tests passed. |
| CPU-bound non-connection work may keep compatibility scheduler. | Done | Static shape tests allow legitimate global compatibility paths and only ban net ownership shard-0 shortcuts. |
| One-user-accept-loop API conflict is resolved before implementation. | Done | Task 3 chose per-shard `SO_REUSEPORT` listener groups and internal owner-local accept/request placement without public syntax changes. |
| Any accept handoff fallback is visible and not called the ideal path. | Done | No fallback path was used in Epic 6 benchmark rows; trace keeps `global_path_fallbacks=0`. |
| Each net-owning shard has a poller owner and wake mechanism. | Done | Task 10 per-shard pipe wake/poller behavior tests passed. |
| Per-shard wake is not Phase 4 cross-shard transport. | Done | No eventfd/inbound queues/credits/seq-cst `PARKED` protocol were added. |
| Close/cancellation does not complete stale waiters on another shard. | Done | Task 11 stale fd-reuse owner-local cleanup proof passed. |
| Shutdown wakes every shard poller and worker without stranded net waiters. | Done | `TestRuntimeV2NetPollerShutdownWakesEveryShard` and lifecycle trace fields passed. |
| New V2 primitives use owner-first arguments and explicit status codes. | Done | Task implementation/review evidence records owner-first helpers and recoverable status paths for config/listener/poller work. |

### Performance Contract Accounting

| Contract bullet | Status | Evidence |
| --- | --- | --- |
| Build and use current checkout `surge` binary for every benchmark row. | Done | Built `/tmp/surge-epic6-closeout` from checkout commit `01a4f7e5`; report records the same commit. |
| Compare single-shard and multi-shard native TCP rows. | Done | Report has 1-shard and 8-shard rows. |
| Include 1, 8, and 32 connection rows. | Done | Report includes 1, 8, and 32 connections for 1 and 8 shards. |
| Include at least one higher-load row near 1k, 10k if safe. | Done | Report includes 1024 connections; 10k remains skipped by the script's safety default. |
| Record accept, fallback, steal, fd-readiness, and imbalance trace counters. | Done | Report includes runtime trace table with per-shard accepts, `global fallbacks`, `sched steal`, fd batches, and imbalance. |
| Explain small-load regression and throughput result. | Done | Closeout notes state low-count skew is expected and 8-shard throughput is worse under the preserved global lock. |
| Judge distribution from high-load row, not small skew. | Done | 8-shard/1024 row used all 8 accept shards; low-count skew is non-failure. |

### Epic Acceptance Accounting

| Acceptance item | Status | Evidence |
| --- | --- | --- |
| `SURGE_SHARDS=1` preserves stable Runtime V2 behavior. | Done | Single-shard compatibility accept test passed. |
| `SURGE_SHARDS>1` initializes bounded shards and rejects invalid config. | Done | Shard config and invalid-config tests passed. |
| Multi-shard mode uses one Tier 1 worker per shard and handles `SURGE_THREADS` conflicts. | Done | Worker-shape and conflicting-thread tests passed. |
| Global-lock boundary is stated and preserved. | Done | Epic and `RUNTIME_V2.md` closeout sections state the boundary. |
| Listener, connection, fd registry, and connection-task placement ownership is visible. | Done | Metadata tests and trace fields passed. |
| Per-shard poller/wake exists without Phase 4 transport. | Done | Per-shard poller tests passed; no Phase 4 transport was introduced. |
| Accepted connection fds register on owner shard. | Done | Owner-local waiter/fd registry tests passed. |
| Tier 1 connection tasks are not stolen by non-owner shards. | Done | Scheduler no-steal tests and benchmark `sched steal=0` passed. |
| Parked-with-local-work or equivalent no-sleep proof exists. | Done | Scheduler placement liveness/static tests passed; broader parked-with-work probes remain outside the narrow accept CI gate. |
| Close, cancellation, readiness, and shutdown tests cover multi-shard path. | Done | Task 10/11/12 tests passed in `runtime-v2-check`. |
| Stable multi-shard accept tests run in `runtime-v2-check` and CI. | Done | `runtime-v2-accept-check` passed standalone and through `runtime-v2-check`. |
| Benchmarks compare single/multi-shard rows and explain global-lock result. | Done | Closeout benchmark report and interpretation recorded. |
| `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`, `git diff --check` pass or have unrelated blockers recorded. | Done with debt | `c-check`, `cppcheck`, `make check`, and final `runtime-v2-check` rerun passed. The first `runtime-v2-check` attempt hit known `RV2-DEBT-002` timeout class. |
| Root, `runtime`, and `runtime/native` Sentrux scans are recorded. | Done | Final scans passed: root `6182`, runtime `5340`, runtime/native `5467`. |
| Touched over-limit files have final line-count outcomes. | Done | Task 14 and closeout record final effective LOC and tightened ceilings. |
| Every Epic 6 debt is closed or recorded with owner and close condition. | Done | `RV2-DEBT-010` and `RV2-DEBT-013` owners were updated; large-file debts remain open with tightened ceilings. |
| Durable docs are updated with final state. | Done | Epic document, README, task index, debt ledger, notes, evidence, and `RUNTIME_V2.md` were updated. |

### Commands/Checks

| Command | Result |
| --- | --- |
| `git status -sb` | Clean before Task 15 edits; branch ahead of origin by 20 commits. |
| `git diff --check` | Passed before closeout docs edit and after closeout docs edit. |
| `./check_file_sizes.sh -a` | Passed; 708 files checked, 0 bad, 8 legacy ceilings. |
| `make c-check` | Passed. |
| `make cppcheck` | Passed. |
| `timeout 600s make runtime-v2-accept-check` | Passed standalone. |
| `timeout 600s make runtime-v2-check` | First attempt failed in `TestMTBlockingChannelHelpersAllowTimersToAdvance` with `program timeout after 30s`; this matches the accepted baseline `RV2-DEBT-002` timeout class. Immediate rerun passed the full Runtime V2 chain, including the accept gate. |
| `timeout 900s make check` | Passed. |
| `go build -ldflags "$(./scripts/ldflags.sh --local)" -o /tmp/surge-epic6-closeout ./cmd/surge` | Passed. |
| `timeout 300s env SURGE=/tmp/surge-epic6-closeout SURGE_NET_BENCH_SHARDS="1 8" SURGE_NET_BENCH_CONNECTIONS="1 8 32 1024" SURGE_NET_BENCH_REQUESTS=8 SURGE_NET_BENCH_MODES=direct SURGE_NET_BENCH_PATTERNS=seq SURGE_NET_BENCH_REPORT=build/benchmarks/runtime-v2-epic6-closeout-native-net.md ./scripts/bench_native_net.sh` | Passed; report generated under ignored `build/benchmarks/`. |
| `sentrux check .` | Passed, quality `6182`. |
| `sentrux check runtime` | Passed, quality `5340`. |
| `sentrux check runtime/native` | Passed, quality `5467`. |

### Final Benchmark Confirmation

Report: `build/benchmarks/runtime-v2-epic6-closeout-native-net.md`.

| Row | avg us/op | Key trace evidence |
| --- | ---: | --- |
| 1 shard, 1024 connections | 22417.86 | `accept_0=1024`, `global fallbacks=0`, `sched steal=0`. |
| 8 shards, 1024 connections | 31268.16 | all 8 accept shards active; `accept_0=152 accept_1=136 accept_2=120 accept_3=115 accept_4=141 accept_5=127 accept_6=126 accept_7=107`; `global fallbacks=0`, `sched steal=0`. |

The eight-shard row is slower than the single-shard row in this direct/seq
benchmark. This is accepted for Epic 6 because the global executor lock is
still preserved. The row proves high-load owner distribution, no shard-0 net
fallback, and no connection-task steal; it does not prove lock-level
throughput scaling.

### Sentrux Final Signals

| Scope | Task 1 baseline | Final | Outcome |
| --- | ---: | ---: | --- |
| Repository root | 6190 | 6182 | Held with recorded closeout exception: all rules pass, affected runtime scopes improved, and the small root dip is not treated as a runtime-quality regression. |
| `runtime` | 5279 | 5340 | Improved. |
| `runtime/native` | 5318 | 5467 | Improved. |

### Final Line Counts And Allowlist

| Path | Final effective LOC | Allowlist state |
| --- | ---: | --- |
| `runtime/native/rt_net.c` | 818 | Legacy ceiling `818`. |
| `runtime/native/rt_async_state.c` | 1722 | Legacy ceiling `1722`. |
| `runtime/native/rt_async_task.c` | 731 | Legacy ceiling `731`. |
| `runtime/native/rt_async_internal.h` | 478 | Under 500; no allowlist entry. |
| `runtime/native/rt_runtime.c` | 281 | Under 500. |
| `runtime/native/rt_fd_registry.c` | 401 | Under 500. |
| `runtime/native/rt_async_waiter.c` | 488 | Under 500. |
| `runtime/native/rt_net_poller.c` | 158 | Under 500. |
| `runtime/native/rt_net_accept_group.c` | 246 | Under 500. |
| `runtime/native/rt_net_handles.c` | 243 | Under 500. |
| `runtime/native/rt_net_lifecycle.c` | 89 | Under 500. |
| `runtime/native/rt_net_listener_socket.c` | 103 | Under 500. |
| `runtime/native/rt_scheduler_placement.c` | 91 | Under 500. |

### Debt State

- No new durable debt was opened by Task 15.
- `RV2-DEBT-010` remains open for copied raw net-handle generation safety and
  now points to a future net handle ABI/lifecycle task.
- `RV2-DEBT-013` remains open for stdlib HTTP raw `TcpConn` worker handoff and
  now points to a future net handle ABI/lifecycle task or stdlib owner-local
  server redesign before public multi-shard HTTP support.
- `RV2-DEBT-003`, `RV2-DEBT-004`, and `RV2-DEBT-005` remain open with stricter
  `.loc-legacy-allowlist` ceilings.
- `RV2-DEBT-001`, `RV2-DEBT-002`, and `RV2-DEBT-011` remain Epic 11
  test/backend matrix debt. Task 15 observed the `RV2-DEBT-002` timeout class
  once and then passed the immediate rerun.

### Epic 7 Handoff

Epic 7 should split the global executor lock and move remaining global
compatibility primitives toward shard-owned state. Start from this Epic 6
shape:

- shards own scheduler state, waiter stores, fd registries, net poll scratch,
  heap accounting cells, trace counters, listener-group member state, and net
  poll wake pipes;
- accepted TCP connections remain owner-shard-local for registry, readiness,
  close, cancellation cleanup, shutdown wake, and Tier 1 task placement;
- `rt_executor.lock` still owns generic task/scope state, global scheduler
  coordination, non-net waiter compatibility paths, channels, join, scope wake,
  cancellation state, blocking completions, timers, `now_ms`, and generic ready
  work.

Do not fold Phase 4 into Epic 7. Cross-shard messaging, inbound queues,
eventfd/credit protocols, remote select, distributed scopes, remote-free, and
public crossing syntax remain later epics.

### Syntax Gate

No Surge syntax, keyword, parser, semantic-analysis, lowering, stdlib API, or
public example surface changed in Epic 6 closeout. Any later epic that changes
the crossing surface must stop first for a dedicated language discussion with
the user. Names such as `far`, `submit_to`, `crosses`, and `shard-movable`
remain semantic placeholders, not accepted syntax.

## Result

Epic 6 is closed. The native Runtime V2 net path now supports structural
multi-shard accept ownership under the preserved global executor lock, with
owner-local fd registry/waiter/poller behavior, per-shard wake, Tier 1
connection-task no-steal placement, stable accept CI coverage, benchmark
evidence, tightened LOC ceilings, and an explicit Epic 7 handoff. The remaining
work is not hidden: copied/raw net-handle generation safety, stdlib HTTP
owner-local handler design, legacy large-file cleanup, and the VM/native/LLVM
test-matrix rewrite stay in `DEBT.md` with owners.
