# Epic 5 Evidence: Per-Shard Heap Accounting

This file records task evidence for Epic 5. Keep durable conclusions here and
keep `NOTES.md` as the handoff log.

## Task Status

| Task | Status | Evidence |
| --- | --- | --- |
| 1 | Complete | Starting state, debt scope, Sentrux scans, heap smoke, current Runtime V2 gates, and Task 2-7 gate plan recorded below. |
| 2 | Complete | Dependency map artifact, selected event-delta cell model, caller-context corrections, checks, and review outcome recorded below. |
| 3 | Complete | Heap stats contract tests for alloc/free, realloc, aligned paths, failed realloc, and concurrent workers recorded below. |
| 4 | Complete | Pending static target-shape gate for heap-accounting ownership, ABI, record API, and aggregation recorded below. |
| 5 | Complete | Runtime/shard-owned accounting skeleton, cold cell, lane wiring, static gate split, checks, Sentrux, and review outcome recorded below. |
| 6 | Complete | Alloc/free/realloc writes now route through accounting cells; old global source-of-truth counters are gone; checks, Sentrux, and review outcome recorded below. |
| 7 | Complete | Aggregation audit, focused heap evidence, active static predicate, Sentrux scans, and docs closeout recorded below. |
| 8 | Complete | Repeated concurrent heap tests, SURGE_THREADS stress, manual heap benchmark script/report, review outcome, and benchmark stress debt recorded below. |
| 9 | Draft | Runtime V2 heap CI gate not started. |
| 10 | Draft | Epic closeout not started. |

## Open Evidence Questions

- Which accounting cell model removes the hot global counter cache line without
  hiding a new shared shard-local bottleneck under current multi-worker `N=1`?
- Which cold allocation paths run before runtime initialization, and how do they
  report into `rt_heap_stats()`?
- Which focused tests are stable enough for `runtime-v2-heap-check` and CI?
- Does heap accounting change covered Runtime V2 net or channel benchmark rows?

## Task 1: Kickoff Baseline And Sentrux

### Task Identity And Scope

- Task: `05-tasks/01-kickoff-baseline-and-sentrux.md`.
- Epic: `05-per-shard-heap-accounting.md`.
- Date: 2026-07-03.
- Scope: docs-only kickoff evidence before heap-accounting implementation.
- Out of scope: runtime code, `Makefile`, CI workflow changes, benchmark
  runs, new tests, and heap-accounting design changes.
- Proving spike: no.

### Baseline Commit/Status

- Branch/worktree: `codex/runtime-net-scheduler-refactor`.
- Baseline commit: `5e04a97590874c4f1936581b459562929540ec16`.
- Baseline commit summary: `5e04a975 docs(runtime): plan epic 5 heap accounting`.
- Status before: clean; `git status --short` printed no output.
- Dirty or untracked files not touched at start: none.
- Status after docs edit: `git status --short` showed only
  `M docs/runtime-v2-epics/05-evidence.md` and
  `M docs/runtime-v2-epics/NOTES.md`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/05-evidence.md` | updated | Record Task 1 baseline evidence and Task 2-7 gate plan. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add handoff notes for Task 2. | Documentation only. |

No runtime code or `Makefile` files were changed.

### Baseline Line Counts

Command: `wc -l runtime/native/rt_alloc.c runtime/native/rt_runtime.c runtime/native/rt_async_internal.h runtime/native/rt_async_state.c internal/vm/llvm_native_heap_stats_test.go`

| Path | Lines | Status |
| --- | ---: | --- |
| `runtime/native/rt_alloc.c` | 144 | under 500-line Runtime V2 target. |
| `runtime/native/rt_runtime.c` | 184 | under 500-line Runtime V2 target. |
| `runtime/native/rt_async_internal.h` | 495 | under target but only 5 lines below the hard 500-line line. |
| `runtime/native/rt_async_state.c` | 1727 | over target; accepted baseline debt `RV2-DEBT-003`. |
| `internal/vm/llvm_native_heap_stats_test.go` | 69 | current heap-stat smoke test file. |

Heap-related test-file discovery:

| Command | Result | Exit/status |
| --- | --- | --- |
| `rg --files internal/vm \| rg '(heap_stats\|heap_accounting\|runtime_v2_.*heap)' \|\| true` | `internal/vm/llvm_native_heap_stats_test.go` | `0` |

### Accepted Baseline Debt Scope

| Debt | Epic 5 status |
| --- | --- |
| `RV2-DEBT-001`: broad focused VM/backend command `go test ./internal/vm -run 'MT|Async|Net|LLVM'` fails when timeout-sensitive paths are not skipped. | Accepted baseline debt; not a required green gate for Epic 5. |
| `RV2-DEBT-002`: timeout-sensitive sync-helper tests are excluded from current green gates. | Accepted baseline debt unless a task changes sync-helper/compensation semantics. |
| `RV2-DEBT-003`: `runtime/native/rt_async_state.c` remains over the Runtime V2 line target. | Accepted baseline debt; touching it requires a recorded line-count outcome and heap-accounting reason. |
| `RV2-DEBT-004`: `runtime/native/rt_net.c` remains over the line target. | Not an Epic 5 close condition unless a task touches net lifecycle or net allocation accounting. |
| `RV2-DEBT-005`: other legacy native runtime files remain over the target. | Not an Epic 5 close condition unless touched. |
| `RV2-DEBT-006`: channel benchmark script still relies on outer timeout wrappers. | Not a Task 1 blocker; record outer timeout use if later performance evidence uses that script. |
| `RV2-DEBT-007`: Sentrux thresholds are calibrated to current legacy ceilings. | Accepted quality-hardening debt; current rule checks still must pass. |
| `RV2-DEBT-010`: copied net handles are not generation-aware. | Not an Epic 5 close condition unless heap-accounting work touches copied net handles. |

Any new allocator or heap-accounting debt discovered during Epic 5 must either
close before closeout or land in `DEBT.md` with an owner and close condition.

### Sentrux Root/Scoped Signals

Sentrux MCP was available in this session. Results below are from sequential
`scan`, `health`, and `check_rules` calls, so each row names the active path.

| Scan | Active path | When | quality_signal | Root cause or bottleneck | Rules result |
| --- | --- | --- | ---: | --- | --- |
| Repository | `/home/zov/projects/surge/surge` | Before Task 1 docs edit | 6191 | bottleneck `modularity`; files 4825, import edges 1895, lines 385091, cross-module edges 1820; root-cause scores: acyclicity 10000, depth 6667, equality 4647, modularity 3462, redundancy 8481 | pass; `rules_checked=8`, `total_rules_defined=12`, `violation_count=0`; output truncated by free-tier limit after checking rules. |
| Runtime | `/home/zov/projects/surge/surge/runtime` | Before Task 1 docs edit | 5240 | bottleneck `redundancy`; files 44, import edges 38, lines 16035, cross-module edges 0; root-cause scores: acyclicity 10000, depth 8889, equality 4956, modularity 3333, redundancy 2690 | pass; `rules_checked=7`, `total_rules_defined=8`, `violation_count=0`; output truncated by free-tier limit after checking rules. |
| Runtime/native | `/home/zov/projects/surge/surge/runtime/native` | Before Task 1 docs edit | 5244 | bottleneck `redundancy`; files 41, import edges 38, lines 15992, cross-module edges 37; root-cause scores: acyclicity 10000, depth 8889, equality 4955, modularity 3343, redundancy 2693 | pass; `rules_checked=7`, `total_rules_defined=7`, `violation_count=0`. |

Task 1 did not call `session_start` or `session_end`; this docs-only kickoff has
no runtime-code diff to compare. Runtime-code Tasks 5-7 must start the Sentrux
session on the scoped path they will use for final delta evidence.

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence note |
| --- | --- | --- | --- | --- |
| `git branch --show-current` | record branch | `codex/runtime-net-scheduler-refactor` | `0` | baseline identity. |
| `git rev-parse HEAD` | record commit | `5e04a97590874c4f1936581b459562929540ec16` | `0` | baseline identity. |
| `git log -1 --oneline` | record commit summary | `5e04a975 docs(runtime): plan epic 5 heap accounting` | `0` | baseline identity. |
| `git status --short` | clean start | no output | `0` | no pre-existing dirty files. |
| `rg -n 'RV2-DEBT-00[1-7]\|RV2-DEBT-010\|Accepted Baseline Debt\|broad focused VM\|timeout-sensitive' docs/runtime-v2-epics/05-per-shard-heap-accounting.md docs/runtime-v2-epics/DEBT.md docs/runtime-v2-epics/RULES.md` | find accepted debt references | matched `DEBT.md` rows 22-29 and Epic 5 accepted debt lines 64-80 | `0` | debt scope recorded above. |
| `sed -n '86,109p' Makefile` | record current Runtime V2 gate definition | `runtime-v2-check` runs the MT seed, then `runtime-v2-waiter-check`, then `runtime-v2-fd-registry-check` | `0` | current gate set recorded below. |
| `rg -n 'runtime-v2-check\|runtime-v2-waiter-check\|runtime-v2-fd-registry-check' Makefile .github/workflows/ci.yml` | confirm CI wiring | `.github/workflows/ci.yml:63` runs `make runtime-v2-check`; Makefile targets found at lines 86, 101, and 106 | `0` | current CI gate still uses `make runtime-v2-check`. |
| `go test ./internal/vm -run '^TestLLVMNative(HeapStats\|BufferedChannelAllocatesSingleBlock)$' -count=1 -v --timeout 120s` | pass | `TestLLVMNativeHeapStats` passed in 3.11s; `TestLLVMNativeBufferedChannelAllocatesSingleBlock` passed in 1.70s; package `ok surge/internal/vm 4.817s` | `0` | current heap-stat smoke baseline. |
| `make runtime-v2-check` | pass | MT liveness gate `ok surge/internal/vm 8.007s`; waiter static boundary `ok surge/internal/vm 0.031s`; pending waiter gate `ok surge/internal/vm 19.650s`; fd registry gate `ok surge/internal/vm 15.940s` | `0` | existing Runtime V2 gate baseline. |
| `git diff --check` | no whitespace errors | no output | `0` | pre-edit whitespace gate. |
| `git diff --check` | no whitespace errors | no output | `0` | post-edit whitespace gate. |
| `git status --short` | Task 1 docs only | `M docs/runtime-v2-epics/05-evidence.md`; `M docs/runtime-v2-epics/NOTES.md` | `0` | final working-tree scope. |

### Current Runtime V2 Gate Shape

`make runtime-v2-check` currently runs:

- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 SURGE_MT_TIMEOUT_SCALE=$(SURGE_MT_TIMEOUT_SCALE) go test ./internal/vm -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' -count=1 -parallel=1 -p=1 -v --timeout 120s`.
- `make runtime-v2-waiter-check`.
- `make runtime-v2-fd-registry-check`.

`runtime-v2-waiter-check` currently runs the default-tag
`TestRuntimeV2WaiterHelperStaticBoundary` and the pending Runtime V2 waiter
contract set under `SURGE_BACKEND=llvm` and `SURGE_SKIP_TIMEOUT_TESTS=0`.

`runtime-v2-fd-registry-check` currently runs the pending fd-registry behavior,
static shape, static boundary, stale snapshot, close wake, and shutdown drain
contracts under `SURGE_BACKEND=llvm` and `SURGE_SKIP_TIMEOUT_TESTS=0`.

### Final Gate Set For Tasks 2-7

| Task | Required gates before close |
| --- | --- |
| Task 2, dependency map | Task-specific `rg` dependency search from `05-tasks/02-heap-accounting-dependency-map.md`; `git diff --check`. No runtime code or tests unless the map uncovers a blocker that needs a focused proof. |
| Task 3, heap stats contract tests | `go test ./internal/vm -run '^TestLLVMNative.*Heap.*|^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s`; `gofmt -l internal/vm/llvm_native_heap_stats_test.go internal/vm/runtime_v2_heap_accounting_contract_test.go`; `git diff --check`. |
| Task 4, static shape tests | Expected-red before implementation: `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s`; `gofmt -l internal/vm/runtime_v2_heap_accounting_static_test.go`; `git diff --check`. Record expected-red output as design evidence, not a regression. |
| Task 5, accounting cell skeleton | Task 4 static gate; focused heap smoke if skeleton can affect observable stats; `make c-check`; `make cppcheck`; `make runtime-v2-check`; `make check` unless a narrower gate is explicitly approved in task evidence before close; `git diff --check`; Sentrux root, `runtime/`, and `runtime/native` scans plus rule checks; `session_start`/`session_end` on the scoped runtime-code path used for delta evidence; touched line counts. |
| Task 6, alloc/free/realloc migration | `go test ./internal/vm -run '^TestLLVMNative.*Heap.*|^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s`; Task 4 static gate; `make c-check`; `make cppcheck`; `make runtime-v2-check`; `make check` unless a narrower gate is explicitly approved in task evidence before close; `git diff --check`; Sentrux root, `runtime/`, and `runtime/native` scans plus rule checks; scoped `session_start`/`session_end`; touched line counts; new allocator/accounting debt closed or recorded. |
| Task 7, heap stats aggregation | `go test ./internal/vm -run '^TestLLVMNative(HeapStats|BufferedChannelAllocatesSingleBlock)$' -count=1 -v --timeout 120s`; `go test ./internal/vm -run '^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s`; Task 4 static gate; `make c-check`; `make cppcheck`; `make runtime-v2-check`; `make check` unless a narrower gate is explicitly approved in task evidence before close; `git diff --check`; Sentrux root, `runtime/`, and `runtime/native` scans plus rule checks; scoped `session_start`/`session_end`; touched line counts. |

The broad focused VM/backend command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` stays outside the required
green gate for Tasks 2-7. Timeout-sensitive tests named in `RV2-DEBT-002` stay
outside the required green gate unless a task explicitly changes their path.

### Follow-Ups And Blockers

| Item | Blocks Task 1 completion? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Heap accounting dependency map | No | Task 2 | Task 1 recorded enough baseline context; Task 2 maps allocation producers, consumers, and thread contexts. |
| Heap behavior tests | No | Task 3 | Task 1 only recorded current smoke tests. |
| Heap static shape tests | No | Task 4 | Task 1 only set the expected-red gate. |
| `runtime-v2-heap-check` CI target | No | Task 9 | Task 1 recorded the current Runtime V2 gate; stable heap gate is later scope. |

### Rollback/Recovery Notes

- Revert only this Task 1 section, the Task 1 status row, and the matching
  `NOTES.md` handoff if Task 1 evidence must be removed.
- No generated artifacts, runtime processes, sockets, or temporary benchmark
  state were created by Task 1.

## Task 2: Heap Accounting Dependency Map

### Task Identity And Scope

- Task: `05-tasks/02-heap-accounting-dependency-map.md`.
- Date: 2026-07-03.
- Scope: docs-only dependency map before runtime-code changes.
- Artifact: `05-heap-accounting-dependency-map.md` (242 lines).
- Out of scope: runtime code, VM tests, `Makefile`, CI, and Sentrux rule files.

### Result

Task 2 mapped current `rt_alloc`, `rt_free`, `rt_realloc`, `record_alloc`,
`record_free`, `record_realloc`, and `rt_heap_stats()` producers; mapped direct
consumers in native ABI, LLVM builtins/lowering, VM debug intrinsics, docs, and
current/parallel tests; and classified caller contexts for cold/main startup,
synchronous runner, executor workers, I/O thread, blocking workers, and
runtime-internal helpers under executor lock.

Selected Task 5 direction:

- runtime or shard-owned heap-accounting state;
- lane-local write cells for executor workers, I/O, blocking workers, and the
  main/synchronous runner path, or an explicit recorded decision to route the
  synchronous runner through the cold/external cell;
- explicit cold/external cell for pre-runtime and unregistered contexts;
- event totals per cell: allocation events, free events, allocated bytes, and
  freed bytes;
- aggregate-on-read `rt_heap_stats()` deriving live blocks and bytes from
  totals, avoiding unsigned per-cell live-counter underflow;
- no `ensure_exec()` or scheduler-internal dependency from `rt_alloc.c`.

Important boundary fixed during review: the map now distinguishes heap-accounted
`rt_alloc`/`rt_free`/`rt_realloc` paths from direct libc temporary allocations
in native helper files such as `rt_bignum_format.c`, `rt_io.c`, `rt_fs.c`, and
`rt_net.c`. Those direct libc temporaries are outside the current
`rt_heap_stats()` contract and are not Epic 5 scope.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/05-heap-accounting-dependency-map.md` | created | Source-backed heap-accounting dependency map and Task 5 handoff. | Documentation only; 242 lines. |
| `docs/runtime-v2-epics/05-evidence.md` | updated | Record Task 2 durable evidence. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 2 handoff. | Documentation only. |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence note |
| --- | --- | --- | --- | --- |
| `rg -n 'heap_alloc_count\|heap_free_count\|heap_live_blocks\|heap_live_bytes\|rt_heap_stats\|rt_alloc\(\|rt_free\(\|rt_realloc\(' runtime internal docs` | producer/consumer matches | matched current producers and consumers | `0` | dependency-map input. |
| `rg -n 'record_alloc\|record_free\|record_realloc\|rt_array_forget_allocation\|SurgeHeapStats\|HeapStats\|heap_stats' runtime internal docs` | helper and consumer matches | matched helper paths and HeapStats consumers | `0` | dependency-map input. |
| `git diff --check` | no whitespace errors | no output | `0` | main-session post-fix whitespace gate. |
| `wc -l docs/runtime-v2-epics/05-heap-accounting-dependency-map.md` | under 500-line target | `242` | `0` | documentation artifact is reviewable. |
| Sentrux root check from Task 2 worker | no quality/rules regression | root session stayed `6191 -> 6191`; rules passed | pass | docs-only task; no runtime-code delta. |

### Review Outcome

Review subagent initially found:

- one P1 overclaim that every native allocation/free is heap-accounted;
- one P2 missing main/synchronous-runner runtime lane;
- one P3 missing `docs/RUNTIME.ru.md`;
- one P3 missing `rt_async_task.c` and `rt_async_poll.c` in the source audit.

All were fixed. Final focused re-review returned no findings. Residual risk:
caller-context classification is source-backed, not runtime-trace-proven; Task 8
owns runtime proof.

### Rollback/Recovery Notes

- Revert the dependency-map artifact, this Task 2 section/status row, and the
  matching `NOTES.md` handoff if Task 2 must be removed.
- No runtime artifacts, generated binaries, sockets, or benchmark reports were
  created by Task 2.

## Task 3: Heap Stats Contract Tests

### Task Identity And Scope

- Task: `05-tasks/03-heap-stats-contract-tests.md`.
- Date: 2026-07-03.
- Scope: focused VM/LLVM behavior tests before moving heap-counter storage.
- Out of scope: runtime/native code, `Makefile`, CI, broad VM backend regex,
  and heap-accounting implementation.

### Result

Task 3 added `internal/vm/runtime_v2_heap_accounting_contract_test.go` with:

- sequential contracts for ordinary `rt_alloc`/`rt_free`;
- `rt_realloc` grow, shrink, null-pointer, and zero-size-free cases;
- aligned allocation and aligned reallocation grow/shrink/free cases;
- deterministic failed realloc coverage using invalid aligned realloc
  (`align=24`), proving the current path returns before counter updates and
  leaves the original pointer freeable;
- concurrent worker allocation/free aggregate accounting after join, without
  exact scheduling assumptions.

The tests collect all `HeapStats` snapshots before Go-side assertions. This
avoids contaminating later snapshots with in-program assertion or conversion
allocations.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `internal/vm/runtime_v2_heap_accounting_contract_test.go` | created | Focused heap-accounting behavior contracts. | 275 lines, under 500-line target. |
| `docs/runtime-v2-epics/05-evidence.md` | updated | Record Task 3 durable evidence. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 3 handoff. | Documentation only. |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence note |
| --- | --- | --- | --- | --- |
| `go test ./internal/vm -run '^TestLLVMNative.*Heap.*\|^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s` | pass | `TestLLVMNativeHeapStats`, `TestRuntimeV2HeapAccountingSequentialContracts`, and `TestRuntimeV2HeapAccountingConcurrentWorkersContract` passed; package `ok surge/internal/vm 5.686s` in main-session rerun | `0` | focused behavior gate. |
| `gofmt -l internal/vm/runtime_v2_heap_accounting_contract_test.go internal/vm/runtime_v2_heap_accounting_static_test.go` | no output | no output | `0` | formatting gate shared with Task 4 workspace file. |
| `git diff --check` | no whitespace errors | no output | `0` | main-session post-review whitespace gate. |
| `wc -l internal/vm/runtime_v2_heap_accounting_contract_test.go` | under 500-line target | `275` | `0` | new test file is under limit. |
| Review subagent stability check | pass | focused regex passed with `-count=1` and `-count=3`; Sentrux root scan quality `6191`, rules pass | pass | review-only evidence. |

### Review Outcome

Review subagent returned no findings. Residual risks:

- failed realloc proof covers deterministic invalid-alignment failure, not OOM;
- concurrent coverage proves aggregate accounting after join, not exact worker
  placement or free-on-different-lane scheduling.

### Rollback/Recovery Notes

- Revert the new contract test file, this Task 3 section/status row, and the
  matching `NOTES.md` handoff if Task 3 must be removed.
- No runtime artifacts, generated binaries, sockets, or benchmark reports were
  created by Task 3.

## Task 4: Heap Accounting Static Shape Tests

### Task Identity And Scope

- Task: `05-tasks/04-heap-accounting-static-shape-tests.md`.
- Date: 2026-07-03.
- Scope: pending expected-red static checks for the target heap-accounting
  shape before runtime-code changes.
- Out of scope: runtime/native implementation, `Makefile`, CI, and broad VM
  gates.

### Result

Task 4 added `internal/vm/runtime_v2_heap_accounting_static_test.go` under the
`runtime_v2_pending` build tag. The gate checks:

- public allocation ABI still matches `rt_alloc`, `rt_free`, `rt_realloc`, and
  `rt_heap_stats` declarations in `rt.h`;
- concrete `rt_heap_accounting_cell` and `rt_heap_accounting` owner shape exists
  under `rt_runtime` or `rt_shard`;
- explicit cold heap-accounting cell or accessor exists;
- lane-local current-cell selection uses a TLS heap-accounting cell and cold
  fallback;
- old file-scope static atomic heap counters are rejected across native sources;
- `record_alloc`, `record_free`, and `record_realloc` call the
  `rt_heap_accounting_record_*` API in their function bodies;
- `rt_heap_stats()` calls `rt_heap_accounting_snapshot` instead of direct old
  global loads.

The expected-red gate strips comments before matching and skips C prototypes
when locating function bodies, preventing false-green results from comments or
unused declarations.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `internal/vm/runtime_v2_heap_accounting_static_test.go` | created | Pending static target-shape gate. | 283 lines, under 500-line target. |
| `docs/runtime-v2-epics/05-evidence.md` | updated | Record Task 4 durable evidence. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 4 handoff. | Documentation only. |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence note |
| --- | --- | --- | --- | --- |
| `go test ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s` | no default-tag tests | `testing: warning: no tests to run`; package passed | `0` | pending gate excluded from default suite. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s` | expected red before Tasks 5-7 | ABI subtest passed; target-shape subtest failed on missing owner shape, cold path, lane selection, old file-scope heap counters, missing record API, direct old global loads, and missing snapshot API | `1` expected | design proof, not a regression. |
| `gofmt -l internal/vm/runtime_v2_heap_accounting_static_test.go` | no output | no output | `0` | formatting gate. |
| `git diff --check` | no whitespace errors | no output | `0` | main-session post-review whitespace gate. |
| `wc -l internal/vm/runtime_v2_heap_accounting_static_test.go` | under 500-line target | `283` | `0` | new test file is under limit. |

### Review Outcome

Review subagent initially found one P1 and one P2 false-green risk in the static
predicates. The test was tightened to inspect concrete target types, owner
fields, function bodies, TLS/current-cell selection, cold fallback, record API
calls, and snapshot API calls after stripping comments. Final focused re-review
returned no remaining findings.

Residual risks:

- this remains a regex/source-shape gate, not a semantic C analyzer;
- behavior correctness still depends on Task 3 contract tests and later runtime
  probes.

### Rollback/Recovery Notes

- Revert the pending static test file, this Task 4 section/status row, and the
  matching `NOTES.md` handoff if Task 4 must be removed.
- No runtime artifacts, generated binaries, sockets, or benchmark reports were
  created by Task 4.

## Task 5: Accounting Cell Skeleton

### Task Identity And Scope

- Task: `05-tasks/05-accounting-cell-skeleton.md`.
- Date: 2026-07-03.
- Scope: runtime-code skeleton for owner/cold/lane heap-accounting state.
- Out of scope: migrating `record_alloc`, `record_free`, `record_realloc`, or
  changing public `rt_heap_stats()` aggregation. Those remain Task 6 and Task 7.

### Result

Task 5 introduced the heap-accounting skeleton without changing `rt_alloc.c` or
current public heap-stat behavior:

- new `rt_heap_accounting_cell`, `rt_heap_accounting`, and
  `struct rt_heap_accounting_snapshot` APIs;
- explicit module-owned `cold_cell` outside `rt_runtime`, so future pre-runtime
  events are not lost when `rt_runtime_init_n1()` clears runtime storage;
- `rt_shard.heap_accounting` as the owner for runtime lane cells;
- TLS current-cell selection with cold fallback;
- main/synchronous runner, worker, I/O, blocking, and compensation cell
  selection points;
- bounded worker, blocking, and compensation cell arrays allocated with direct
  libc `calloc` in the accounting module to avoid recursive `rt_alloc`
  accounting;
- blocking worker context storage owned by `rt_executor.blocking_worker_ctxs`
  and allocated with direct `calloc` because detached blocking worker threads
  need stable per-thread context addresses;
- Task 5 static skeleton gate now passes, while Task 6 and Task 7 static
  predicates remain present and explicitly skipped with owning task names.

Task 5 intentionally kept the old `rt_alloc.c` global counters as the public
source of truth. Task 6 owns migrating writes to the new record API. Task 7 owns
switching `rt_heap_stats()` to `rt_heap_accounting_snapshot`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `runtime/native/rt_heap_accounting.h` | created | Internal heap-accounting types and API. | 68 lines. |
| `runtime/native/rt_heap_accounting.c` | created | Cold cell, TLS selection, record helpers, and snapshot skeleton. | 224 lines. |
| `runtime/native/rt_async_internal.h` | updated | Include accounting API, shard owner field, executor blocking context owner, and accessor prototype. | 499 lines, under the 500-line Runtime V2 target. |
| `runtime/native/rt_runtime.c` | updated | Initialize shard-owned heap accounting and expose executor accessor. | 197 lines. |
| `runtime/native/rt_async_state.c` | updated | Prepare cells and select worker/I/O/compensation cells. | Stayed flat at 1727 lines; legacy ceiling not raised. |
| `runtime/native/rt_async_blocking.c` | updated | Add blocking worker contexts and select blocking cells. | 293 lines. |
| `runtime/native/rt_async_poll.c` | updated | Select and restore main/synchronous runner cell in `run_until_done`. | 319 lines. |
| `internal/vm/runtime_v2_heap_accounting_static_test.go` | updated | Split Task 5 skeleton checks from Task 6/7 skipped predicates and add lane install-site checks. | 338 lines. |
| `docs/runtime-v2-epics/05-evidence.md` | updated | Record Task 5 durable evidence. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 5 handoff. | Documentation only. |
| `docs/runtime-v2-epics/DEBT.md` | updated | Record VM test artifact collision debt discovered during Task 5 verification. | Documentation only. |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence note |
| --- | --- | --- | --- | --- |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s` | pass Task 5, skip Task 6/7 predicates | ABI and Task 5 skeleton passed; Task 6 and Task 7 predicates skipped with explicit owning task messages | `0` | static shape gate. |
| `go test ./internal/vm -run '^TestLLVMNative.*Heap.*\|^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s` | pass | heap smoke and Task 3 contract tests passed; package `ok surge/internal/vm 5.937s` in final rerun | `0` | public behavior unchanged. |
| `make c-check` | pass | C formatting and strict warning compilation passed | `0` | C gate. |
| `make cppcheck` | pass | checked 35 C files including `rt_heap_accounting.c`; `cppcheck OK` | `0` | static C gate. |
| `make runtime-v2-check` | pass | MT liveness, waiter gate, and fd-registry gate passed in final sequential rerun | `0` | Runtime V2 liveness gate. |
| `git diff --check` | no whitespace errors | no output | `0` | whitespace gate. |
| `./check_file_sizes.sh` | pass | 7 changed C/H files checked; 6 OK, `rt_async_state.c` `LEGACY OK <=1727`; overall excellent | `0` | LOC gate. |
| Sentrux scoped session on `runtime/native` | stable or improved | `5244 -> 5250`, `signal_delta=6`, pass | pass | runtime-code delta evidence. |
| Sentrux root scan/rules | pass | root quality `6193`, rules pass with 0 violations | pass | required scan. |
| Sentrux `runtime/` scan/rules | pass | runtime quality `5246`, rules pass with 0 violations | pass | required scan. |
| Sentrux `runtime/native` scan/rules | pass | runtime/native quality `5250`, rules pass with 0 violations | pass | required scan. |

Verification caveat:

- The first `make runtime-v2-check` attempt saw
  `TestMTBlockingChannelHelpersAllowTimersToAdvance` time out once. A focused
  standalone reproduction passed, and final sequential `make runtime-v2-check`
  passed.
- A later parallel duplicate VM test run produced a missing `build.stdout`
  artifact. That was caused by running overlapping VM build tests for the same
  test name concurrently, which race on `target/debug/.tests/`. This is recorded
  as `RV2-DEBT-011`; future VM gates in this epic should run sequentially when
  they can share artifact names.

### Review Outcome

Review subagent initially found two P2 issues:

- blocking worker context array had process-lifetime semantics but no explicit
  owner;
- Task 5 static gate did not read/check lane install-site files.

Both were fixed by storing the context owner on `rt_executor` and adding static
checks for actual main, worker, I/O, blocking, compensation, restore, and cleanup
call sites. Focused re-review returned no remaining findings.

Residual risks:

- Task 6/7 predicates are intentionally skipped and remain open obligations;
- Task 5 lane checks are textual source-shape checks, not full C control-flow
  proof;
- blocking worker context storage remains process-lifetime until shutdown owns
  detached thread lifecycle.

### Debt

- `RV2-DEBT-011` added for VM LLVM test artifact collisions under overlapping
  duplicate VM test runs. This does not block Task 5; it changes how future gates
  should be executed and belongs to the test/backend matrix rewrite unless an
  earlier test-harness task picks it up.

### Rollback/Recovery Notes

- Revert this Task 5 runtime slice, this Task 5 section/status row, the matching
  `NOTES.md` handoff, and `RV2-DEBT-011` if Task 5 must be removed.
- No benchmark reports, sockets, or generated artifacts are required for
  rollback. Test artifacts under `target/debug/.tests/` are disposable.

## Task 6: Alloc/Free/Realloc Accounting Migration

### Task Identity And Scope

- Task: `05-tasks/06-alloc-free-realloc-accounting-migration.md`.
- Date: 2026-07-03.
- Scope: route existing heap-accounted allocation events through the
  runtime/shard-owned accounting cells introduced by Task 5.
- Boundary note: `rt_heap_stats()` had to switch to
  `rt_heap_accounting_snapshot()` in this task so public heap-stat tests keep
  proving the new source of truth after old globals are removed. Task 7 remains
  responsible for the aggregation audit, compatibility evidence, and
  documentation closeout.

### Result

Task 6 removed the old `rt_alloc.c` file-scope global counters:

- `heap_alloc_count`;
- `heap_free_count`;
- `heap_live_blocks`;
- `heap_live_bytes`.

`record_alloc`, `record_free`, and `record_realloc` now call the
`rt_heap_accounting_record_*` API against `rt_heap_accounting_current_cell()`.
Failed allocation and failed realloc paths still return before recording an
event. `rt_free(NULL, ...)`, `rt_realloc(NULL, ...)`,
`rt_realloc(ptr, old, 0, ...)`, aligned realloc's allocate-copy-free path, and
`rt_array_forget_allocation` placement are unchanged.

`rt_heap_stats()` now snapshots `rt_runtime_global_heap_accounting()` before
allocating the public `SurgeHeapStats` result. The snapshot includes the cold
cell even when runtime-owned accounting is not initialized, preserving
pre-runtime accounting events.

Review corrected an important concurrency point: snapshot reads use relaxed
per-lane atomics and are not a single global cut. Therefore raw
`alloc_count`/`free_count` stay raw, while derived `live_blocks` and
`live_bytes` saturate transient underflow to zero instead of treating
`free > alloc` or `freed_bytes > allocated_bytes` as an invariant violation.

The Task 6 and Task 7 static predicates are now active and green.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `runtime/native/rt_alloc.c` | updated | Route record helpers through heap-accounting cells and snapshot public stats from the new source of truth. | 127 lines. |
| `runtime/native/rt_heap_accounting.c` | updated | Keep raw event totals and saturate only derived live values on transient snapshot skew. | 223 lines. |
| `runtime/native/rt_heap_accounting.h` | updated | Remove stale invariant-violation status and expose `rt_runtime_global_heap_accounting()`. | 68 lines. |
| `runtime/native/rt_runtime.c` | updated | Add narrow accessor for shard0 heap accounting without initializing runtime from the allocator path. | 202 lines. |
| `internal/vm/runtime_v2_heap_accounting_static_test.go` | updated | Activate Task 6 and Task 7 static predicates. | 334 lines. |
| `STATS.md` | updated by pre-commit | Refresh generated code-size statistics after runtime/test line-count changes. | Generated stats artifact. |
| `docs/runtime-v2-epics/05-evidence.md` | updated | Record Task 6 durable evidence. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 6 handoff. | Documentation only. |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence note |
| --- | --- | --- | --- | --- |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s` | pass | ABI, Task 5 skeleton, Task 6 record migration, and Task 7 snapshot aggregation predicates all passed; package `ok surge/internal/vm 0.041s` in main-session rerun | `0` | static shape gate. |
| `go test ./internal/vm -run '^TestLLVMNative.*Heap.*\|^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s` | pass | `TestLLVMNativeHeapStats`, sequential contracts, and concurrent workers contract passed; package `ok surge/internal/vm 5.994s` in main-session rerun | `0` | heap behavior gate. |
| `make c-check` | pass | C formatting and strict warning compilation passed | `0` | C gate. |
| `make cppcheck` | pass | checked 35 C files including `rt_alloc.c` and `rt_heap_accounting.c`; `cppcheck OK` | `0` | static C gate. |
| `make runtime-v2-check` | pass | MT liveness, waiter gate, and fd-registry gate passed sequentially; fd-registry package time `15.896s` | `0` | Runtime V2 liveness gate. |
| `make check` | pass | Go suite, golangci-lint, C gate, and file-size gate passed | `0` | broad repo gate requested by Task 1 gate plan. |
| Pre-commit `scripts/code_stats_md.sh` | refresh stats | `STATS.md` updated runtime/native code lines `16304 -> 16291`, test lines `40578 -> 40574`, total lines `200489 -> 200472` | `0` | automatic commit hook artifact. |
| `git diff --check` | no whitespace errors | no output | `0` | whitespace gate. |
| `./check_file_sizes.sh` | pass | 4 changed C/H files checked; all OK; overall excellent | `0` | LOC gate. |
| Sentrux scoped session on `runtime/native` | stable or improved | task-scoped session before final review fixes recorded `5306 -> 5318`, pass | pass | runtime-code delta evidence. |
| Sentrux root scan/rules | pass | root quality `6190`, rules pass with 0 violations | pass | final current-code scan. |
| Sentrux `runtime/` scan/rules | pass | runtime quality `5279`, rules pass with 0 violations | pass | final current-code scan. |
| Sentrux `runtime/native` scan/rules | pass | runtime/native quality `5318`, rules pass with 0 violations | pass | final current-code scan. |

### Review Outcome

Review subagent initially found:

- one P1 issue: returning `NULL` from `rt_heap_stats()` on transient
  cross-counter snapshot skew would make valid concurrent accounting look like
  an invariant violation;
- one P2 issue: Task 7 snapshot aggregation predicates were mechanically
  satisfied by the Task 6 boundary change but still skipped.

Both were fixed. Focused re-review returned no findings after inspecting the
five changed files and rerunning the two focused heap gates.

Residual risks:

- snapshot consistency remains relaxed by design; exact live values can be
  transiently conservative during concurrent updates;
- Task 7 still owns aggregation evidence and docs because the mechanical
  `rt_heap_stats()` switch happened in Task 6 for testability;
- direct libc temporaries outside the current `rt_alloc`/`rt_free` contract
  remain outside Epic 5 scope, as recorded by Task 2.

### Debt

- No new debt was added by Task 6.
- `RV2-DEBT-011` remains active for overlapping VM LLVM test artifact
  collisions; Task 6 gates were run sequentially where VM artifacts can share
  names.

### Rollback/Recovery Notes

- Revert this Task 6 runtime slice, this Task 6 section/status row, and the
  matching `NOTES.md` handoff if Task 6 must be removed.
- Test artifacts under `target/debug/.tests/` are disposable.

## Task 7: Heap Stats Aggregation

### Task Identity And Scope

- Task: `05-tasks/07-heap-stats-aggregation.md`.
- Date: 2026-07-03.
- Scope: close the heap-stats aggregation task with audit, focused evidence, and
  documentation after Task 6 moved `rt_heap_stats()` to the snapshot helper.
- Runtime/test changes: none in Task 7; the audit found no real runtime or test
  gap.
- Sentrux: root, `runtime/`, and `runtime/native` scans/rules were collected;
  a no-code scoped `runtime/native` session was run after review to document
  that Task 7's docs-only closeout did not change native-runtime quality.

### Result

Task 7 confirmed the aggregation behavior now implemented in the runtime:

- `rt_heap_stats()` calls
  `rt_heap_accounting_snapshot(rt_runtime_global_heap_accounting(), ...)`
  before allocating the public `SurgeHeapStats` result, so stats-result
  allocations remain outside the returned snapshot.
- `rt_heap_accounting_snapshot()` aggregates the module-owned cold cell and the
  runtime/shard-owned main, I/O, worker, blocking, and compensation cells.
- The old `rt_alloc.c` heap-counter globals and direct `rt_heap_stats()` global
  loads are absent.
- `HeapStats` layout and public ABI remain unchanged.
- `rc_increments` and `rc_decrements` remain zero until a reference-counting
  epic owns those counters.
- Snapshot reads use relaxed per-cell atomics. Raw alloc/free event totals stay
  raw; derived live totals saturate transient underflow to zero because the
  snapshot is not a global cut across lanes.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/05-evidence.md` | updated | Record Task 7 closeout evidence. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 7 handoff. | Documentation only. |
| `docs/runtime-v2-epics/05-tasks/07-heap-stats-aggregation.md` | updated | Mark task complete. | Documentation only. |

Runtime/test files were audited but not changed:

| Path | Lines | Note |
| --- | ---: | --- |
| `runtime/native/rt_async_internal.h` | 499 | unchanged; remains at the hard Task 5/6 limit. |
| `runtime/native/rt_alloc.c` | 127 | contains the public snapshot call before stats-result allocation. |
| `runtime/native/rt_heap_accounting.h` | 68 | exposes snapshot/current accounting API. |
| `runtime/native/rt_heap_accounting.c` | 223 | aggregates cold and runtime-owned cells. |
| `internal/vm/runtime_v2_heap_accounting_static_test.go` | 334 | Task 5/6/7 static predicates are active. |
| `internal/vm/runtime_v2_heap_accounting_contract_test.go` | 275 | sequential and concurrent heap contracts. |
| `internal/vm/llvm_native_heap_stats_test.go` | 69 | native heap-stats smoke coverage. |

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence note |
| --- | --- | --- | --- | --- |
| `go test ./internal/vm -run '^TestLLVMNative(HeapStats\|BufferedChannelAllocatesSingleBlock)$' -count=1 -v --timeout 120s` | pass | `TestLLVMNativeHeapStats` and `TestLLVMNativeBufferedChannelAllocatesSingleBlock` passed; package `ok surge/internal/vm 4.587s` | `0` | public native heap-stats compatibility. |
| `go test ./internal/vm -run '^TestRuntimeV2HeapAccounting' -count=1 -parallel=1 -p=1 -v --timeout 180s` | pass | sequential and concurrent heap accounting contracts passed; package `ok surge/internal/vm 4.253s` | `0` | Runtime V2 heap behavior. |
| `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2HeapAccountingStatic' -count=1 -v --timeout 60s` | pass | ABI, Task 5 skeleton, Task 6 record migration, and Task 7 snapshot aggregation predicates all passed; package `ok surge/internal/vm 0.041s` | `0` | active static aggregation gate. |
| `make c-check` | pass | C formatting and strict warning compilation passed | `0` | C gate. |
| `make cppcheck` | pass | checked 35 C files including `rt_alloc.c` and `rt_heap_accounting.c`; `cppcheck OK` | `0` | static C gate. |
| `make runtime-v2-check` | pass | MT liveness, waiter, pending waiter, and fd-registry gates passed; final fd-registry package `ok surge/internal/vm 15.922s` | `0` | Runtime V2 liveness gate. |
| `make check` | pass | Go suite, golangci-lint, C gate, and file-size gate passed; file-size gate had no applicable dirty code files | `0` | broad repo gate required by the Task 7 gate plan. |
| `git diff --check` | no whitespace errors | no output | `0` | whitespace gate. |
| `wc -l runtime/native/rt_async_internal.h runtime/native/rt_alloc.c runtime/native/rt_heap_accounting.h runtime/native/rt_heap_accounting.c internal/vm/runtime_v2_heap_accounting_static_test.go internal/vm/runtime_v2_heap_accounting_contract_test.go internal/vm/llvm_native_heap_stats_test.go docs/runtime-v2-epics/05-evidence.md docs/runtime-v2-epics/NOTES.md docs/runtime-v2-epics/05-tasks/07-heap-stats-aggregation.md` | record touched/audited sizes | runtime/test line counts: 499, 127, 68, 223, 334, 275, 69; docs: 698, 1933, 53 | `0` | LOC evidence for audited runtime/test surfaces and touched docs. |
| Sentrux scoped no-code session on `runtime/native` | stable | `5318 -> 5318`, `signal_delta=0`, pass | pass | records no native-runtime quality delta for the docs-only closeout. |
| Sentrux root scan/rules | pass | root quality `6190`; rules pass with 0 violations | pass | whole-repo architectural scan. |
| Sentrux `runtime/` scan/rules | pass | runtime quality `5279`; rules pass with 0 violations | pass | runtime scoped scan. |
| Sentrux `runtime/native` scan/rules | pass | runtime/native quality `5318`; rules pass with 0 violations | pass | native runtime scoped scan. |

### Residual Risks

- Snapshot consistency remains relaxed by design. During concurrent updates,
  live totals can be transiently conservative because the snapshot is not a
  global cut across cells.
- Direct libc temporaries outside the public `rt_alloc`/`rt_free` contract remain
  outside Epic 5 scope, as recorded by Task 2.
- `RV2-DEBT-011` remains active for overlapping VM LLVM test artifact
  collisions; Task 7 VM checks were run sequentially where artifact names can
  collide.
- Task 8 still owns broader concurrency and performance evidence.

### Debt

- No new debt was added by Task 7.

### Rollback/Recovery Notes

- Revert this Task 7 docs section, status row, task status wording, and the
  matching `NOTES.md` handoff if the Task 7 closeout must be removed.
- Test artifacts under `target/debug/.tests/` are disposable.

## Task 8: Concurrency And Performance Evidence

### Task Identity And Scope

- Task: `05-tasks/08-concurrency-and-performance-evidence.md`.
- Date: 2026-07-03.
- Scope: collect concurrency and manual performance evidence for the new
  cell-based heap accounting model before CI gate wiring.
- Runtime changes: none.
- Test changes: none.
- Tooling change: added a manual heap-accounting benchmark script.

### Result

Task 8 proved the focused heap accounting contracts under repeated runs and
explicit worker counts, then added a manual Surge-level heap benchmark:

- repeated `TestRuntimeV2HeapAccounting` contracts passed with `-count=3`;
- `TestRuntimeV2HeapAccountingConcurrentWorkersContract` passed with
  `SURGE_THREADS=2`, `4`, and `8`, each with `-count=3`;
- `make runtime-v2-check` still passed after the heap-accounting migration;
- `scripts/bench_native_heap_accounting.sh` now generates a temporary Surge
  benchmark fixture and writes an ignored Markdown report under
  `build/benchmarks/`;
- the benchmark is intentionally manual evidence and is not a CI gate;
- the benchmark validates numeric env overrides, validates probe names, owns a
  per-probe timeout, and reports `threads`, `probe`, status, and timeout on
  failure.

Final current-checkout report:

- `build/benchmarks/runtime-v2-task08-native-heap-current.md`;
- generated with current `surge` binary at `f3bc1df8`;
- modes: `SURGE_THREADS=1 2 4 8`;
- probes: `empty_loop`, `serial_alloc_free`, `serial_realloc`,
  `heap_stats_poll`, `concurrent_alloc_free`.

Selected rows from the final current report:

| Threads | Probe | Iterations | ns/op | Note |
| ---: | --- | ---: | ---: | --- |
| 1 | `serial_alloc_free` | 100000 | 1650 | successful default serial alloc/free run. |
| 2 | `serial_alloc_free` | 100000 | 1564 | stable under 2 worker mode. |
| 4 | `serial_alloc_free` | 100000 | 1607 | stable under 4 worker mode. |
| 8 | `serial_alloc_free` | 100000 | 1571 | stable under 8 worker mode. |
| 1 | `serial_realloc` | 50000 | 2549 | ordinary realloc event shape. |
| 8 | `serial_realloc` | 50000 | 2548 | stable under 8 worker mode. |
| 1 | `heap_stats_poll` | 10000 | 699 | aggregate-on-read plus public result allocation. |
| 8 | `heap_stats_poll` | 10000 | 703 | stable under 8 worker mode. |
| 1 | `concurrent_alloc_free` | 5000 | 18972 | one runtime worker. |
| 2 | `concurrent_alloc_free` | 10000 | 95250 | two runtime workers. |
| 4 | `concurrent_alloc_free` | 20000 | 120946 | four runtime workers. |
| 8 | `concurrent_alloc_free` | 40000 | 183527 | eight runtime workers. |

This benchmark is not a pure C allocator microbenchmark. The generated Surge
fixture and public `HeapStats` operations add runtime/language allocation noise;
the report's `empty_loop` rows make that floor visible. Task 8 therefore uses
the report as current manual pressure evidence, not as a CI threshold.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `scripts/bench_native_heap_accounting.sh` | created | Manual heap-accounting benchmark for current-checkout pressure evidence. | 330 lines, executable, under 500-line target. |
| `docs/runtime-v2-epics/05-evidence.md` | updated | Record Task 8 durable evidence. | Documentation only. |
| `docs/runtime-v2-epics/NOTES.md` | updated | Add Task 8 handoff. | Documentation only. |
| `docs/runtime-v2-epics/05-tasks/08-concurrency-and-performance-evidence.md` | updated | Mark task complete. | Documentation only. |
| `docs/runtime-v2-epics/DEBT.md` | updated | Record high-pressure benchmark crash debt. | Documentation only. |

Ignored generated files:

- `build/benchmarks/runtime-v2-task08-native-heap-current.md`;
- `build/benchmarks/runtime-v2-task08-native-heap-smoke.md`;
- `build/benchmarks/runtime-v2-task08-native-heap-serial200k-probe.md`.

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence note |
| --- | --- | --- | --- | --- |
| `go test ./internal/vm -run '^TestRuntimeV2HeapAccounting' -count=3 -parallel=1 -p=1 -v --timeout 240s` | pass | package `ok surge/internal/vm 11.791s` | `0` | repeated heap contracts. |
| `SURGE_THREADS=2 go test ./internal/vm -run '^TestRuntimeV2HeapAccountingConcurrentWorkersContract$' -count=3 -parallel=1 -p=1 -v --timeout 240s` | pass | package `ok surge/internal/vm 6.004s` | `0` | explicit 2-worker pressure. |
| `SURGE_THREADS=4 go test ./internal/vm -run '^TestRuntimeV2HeapAccountingConcurrentWorkersContract$' -count=3 -parallel=1 -p=1 -v --timeout 240s` | pass | package `ok surge/internal/vm 5.984s` | `0` | explicit 4-worker pressure. |
| `SURGE_THREADS=8 go test ./internal/vm -run '^TestRuntimeV2HeapAccountingConcurrentWorkersContract$' -count=3 -parallel=1 -p=1 -v --timeout 240s` | pass | package `ok surge/internal/vm 5.996s` | `0` | explicit 8-worker pressure. |
| `make build` | pass | current `surge` binary built successfully | `0` | benchmark runner input. |
| `SURGE_HEAP_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task08-native-heap-current.md" timeout 120s env SURGE="$PWD/surge" ./scripts/bench_native_heap_accounting.sh` | pass | wrote ignored report `build/benchmarks/runtime-v2-task08-native-heap-current.md` | `0` | final manual benchmark run after review fixes. |
| `bash -n scripts/bench_native_heap_accounting.sh` | pass | no output | `0` | script syntax gate. |
| smoke benchmark with tiny env overrides | pass | wrote ignored report `build/benchmarks/runtime-v2-task08-native-heap-smoke.md` | `0` | validates env override and final script path without repeating heavy benchmark. |
| `SURGE_HEAP_BENCH_THREADS="1" SURGE_HEAP_BENCH_PROBES="serial_alloc_free" SURGE_HEAP_BENCH_SERIAL_ITERATIONS=200000 ... ./scripts/bench_native_heap_accounting.sh` | diagnostic | reproduced `status=139` for `threads=1 probe=serial_alloc_free`; outer `timeout` reported a dumped core | `1` from script | recorded as `RV2-DEBT-012`, not hidden. |
| `make runtime-v2-check` | pass | Runtime V2 liveness, waiter, pending waiter, and fd-registry gates passed | `0` | broad Runtime V2 gate after stress/bench work. |
| `git diff --check` | no whitespace errors | no output | `0` | whitespace gate. |
| Sentrux root scan/rules | pass | root quality `6190`; rules pass with 0 violations | pass | whole-repo architectural scan after script/docs/debt updates. |
| Sentrux `runtime/` scan/rules | pass | runtime quality `5279`; rules pass with 0 violations | pass | scoped runtime scan; no runtime code changed in Task 8. |
| Sentrux `runtime/native` scan/rules | pass | runtime/native quality `5318`; rules pass with 0 violations | pass | scoped native scan; no native code changed in Task 8. |

### Review Outcome

Review subagent initially found:

- one P2 issue: per-probe benchmark runs had no owned timeout or contextual
  failure message;
- one P2 issue: env override values were spliced into generated Surge source
  before validation;
- one P3 issue: benchmark wording overstated isolation as a pure allocator
  microbench.

All were fixed. Focused re-review returned no findings. `shellcheck` was not
available in the review environment; `bash -n` passed.

### Debt

- `RV2-DEBT-012` added for the heavier generated Surge-level heap benchmark
  crash at `serial_alloc_free` `200000` iterations. This does not block Task 8's
  stable default evidence, but it blocks promoting this benchmark beyond manual
  current-checkout evidence until minimized, fixed, or explicitly bounded.

### Residual Risks

- The benchmark is Surge-level and includes generated fixture/runtime
  allocations; it should not be used as a pure C allocator or cache-line
  microbench.
- No before/after baseline worktree benchmark was run. Current evidence compares
  to the Epic 5 kickoff by behavior gates and records current manual timing
  rows only.
- `RV2-DEBT-011` remains active; Task 8 VM commands were run sequentially with
  `-parallel=1 -p=1`.

### Rollback/Recovery Notes

- Revert the benchmark script, this Task 8 section/status row, task status
  wording, `RV2-DEBT-012`, and the matching `NOTES.md` handoff if Task 8 must
  be removed.
- Generated benchmark reports under `build/benchmarks/` and temporary fixture
  directories under `build/tmp/` are disposable ignored artifacts.
