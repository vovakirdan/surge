# Epic 5 Evidence: Per-Shard Heap Accounting

This file records task evidence for Epic 5. Keep durable conclusions here and
keep `NOTES.md` as the handoff log.

## Task Status

| Task | Status | Evidence |
| --- | --- | --- |
| 1 | Complete | Starting state, debt scope, Sentrux scans, heap smoke, current Runtime V2 gates, and Task 2-7 gate plan recorded below. |
| 2 | Draft | Heap accounting dependency map not started. |
| 3 | Draft | Heap stats contract tests not started. |
| 4 | Draft | Static shape tests not started. |
| 5 | Draft | Accounting cell skeleton not started. |
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
