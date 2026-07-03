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
| 6 | Draft | Alloc/free/realloc accounting migration not started. |
| 7 | Draft | Heap stats aggregation not started. |
| 8 | Draft | Concurrency and performance evidence not started. |
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
