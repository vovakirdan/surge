# Epic 5: Per-Shard Heap Accounting

**Goal:** remove global heap counter cache lines from the hot allocation path
before real `N>1` benchmarking, while preserving the current `rt_alloc`,
`rt_free`, `rt_realloc`, and `rt_heap_stats` ABI and behavior.

**Approach:** this epic changes accounting ownership, not allocation strategy.
Keep the underlying `malloc`, `free`, and aligned allocation behavior. Move heap
stats from process-global counters to shard-owned accounting cells, then make
`rt_heap_stats()` aggregate those cells on read. Slab pools, bump pools,
owner-shard span metadata, remote-free queues, and cross-shard memory ownership
stay out until the allocator and hot-pool epic.

**Status:** planned. Task documents are drafted; implementation has not started.

**Task documents:** brief task scopes live under `05-tasks/`. Expand only the
next task before execution.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/04-persistent-fd-registry-and-net-lifecycle.md`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/DEBT.md`
- `docs/runtime-v2-epics/NOTES.md`
- `runtime/native/rt_alloc.c`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_runtime.c`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt.h`
- `internal/vm/llvm_native_heap_stats_test.go`
- `internal/vm/runtime_v2_*_test.go`
- `.github/workflows/*`

## Starting State

`runtime/native/rt_alloc.c` owns four process-global relaxed atomic counters:

- `heap_alloc_count`;
- `heap_free_count`;
- `heap_live_blocks`;
- `heap_live_bytes`.

`rt_alloc`, `rt_free`, and `rt_realloc` update those counters directly on every
allocation path. `rt_heap_stats()` loads the counters, allocates a
`SurgeHeapStats` result, and converts each value to a big integer. The public
behavior is observable through native LLVM tests, especially
`TestLLVMNativeHeapStats` and
`TestLLVMNativeBufferedChannelAllocatesSingleBlock`.

The current runtime already has an internal `N=1` `rt_runtime` and `rt_shard`,
but that does not mean one thread owns all shard work today. The current
executor can run several worker threads inside the single shard. Epic 5 must
not replace four global atomics with one contended shard counter block. The
accounting owner can be the shard, but the write cells may need to be worker
local, thread local, or otherwise lane-local until the future
one-shard-per-thread runtime model lands.

## Accepted Baseline Debt

The broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` remains accepted backend-test
debt. Do not add it as a required green gate in this epic.

Timeout-sensitive tests remain outside this epic's green gate unless a task
explicitly changes their path and adds a focused proof.

Large runtime files remain debt. Epic 5 is not a general LOC cleanup epic.
Touching `rt_async_state.c`, `rt_async_internal.h`, `rt_net.c`, or another
over-limit file is allowed only with a recorded line-count outcome and a reason
tied to heap accounting. New or heavily rewritten files must stay at or below
500 lines.

`RV2-DEBT-001`, `RV2-DEBT-002`, `RV2-DEBT-003`, `RV2-DEBT-004`,
`RV2-DEBT-005`, `RV2-DEBT-006`, `RV2-DEBT-007`, and `RV2-DEBT-010` are not
Epic 5 close conditions unless a task explicitly brings the related surface into
scope. Any new allocator or heap-accounting debt discovered during this epic
must either be closed before closeout or added to `DEBT.md` with an owner and
close condition before the epic can be accepted.

## Scope

Included:

- map every native heap accounting path before changing code;
- classify each allocation path by caller context: worker, I/O thread, blocking
  worker, main thread, and cold non-runtime path;
- define the accounting cell model before implementation;
- move the four heap stat counters out of file-scope global storage and into
  runtime or shard-owned accounting state;
- avoid one shared hot counter block for the current multi-worker `N=1` runtime;
- preserve public `rt_alloc`, `rt_free`, `rt_realloc`, and `rt_heap_stats`
  signatures;
- preserve current `malloc`, `free`, `realloc`, and `posix_memalign` behavior;
- preserve current `rt_heap_stats()` snapshot semantics: the returned values are
  captured before allocations needed to build the stats result;
- preserve null-free and realloc semantics, including no counter update on
  failed realloc;
- add focused behavior tests for heap stats, realloc, aligned allocation, and
  concurrent allocation/free accounting;
- add static checks that prevent global heap counters from returning as the
  source of truth;
- add a stable `runtime-v2-heap-check` gate and wire it into Runtime V2 CI;
- record Sentrux root, `runtime/`, and `runtime/native/` scans before and after
  runtime-code changes;
- keep `NOTES.md` and `05-evidence.md` current after every task.

Not included:

- no slab allocator;
- no bump allocator;
- no owner-shard metadata in allocator pages or spans;
- no remote-free queue;
- no cross-shard free routing;
- no `N>1` accept ownership;
- no work-stealing policy change;
- no public native ABI change for allocation functions;
- no `HeapStats` layout change;
- no reference-counting counter implementation beyond preserving the current
  zero values for `rc_increments` and `rc_decrements`;
- no `io_uring`, `epoll`, `kqueue`, or backend I/O migration;
- no `far`, `submit_to`, `crosses`, move-only capture, or compiler lowering
  work;
- no broad VM/native/LLVM test-matrix rewrite;
- no unrelated bignum, string, filesystem, terminal, or net-handle cleanup.

## Heap Accounting Contract

Epic 5 must make these properties true and testable:

- the four public heap stats are no longer stored as one process-global hot
  counter set;
- each allocation, free, and realloc event records to exactly one accounting
  cell;
- accounting cells are owned by runtime or shard state, even if the current
  compatibility implementation uses per-worker or per-thread cells under that
  owner;
- request-path allocation must not update a shared global heap-counter cache
  line;
- `rt_heap_stats()` aggregates all live accounting cells on read;
- cold paths that run before runtime initialization or outside a worker context
  use an explicit cold accounting cell, not hidden global behavior;
- live block and live byte accounting remains correct when allocation and free
  happen through different execution lanes;
- per-cell live counters must not rely on unsigned underflow. If frees can land
  in a different cell from allocs, live accounting must use aggregate deltas or
  an equivalent proven model;
- failed allocations do not increment allocation or live counters;
- `rt_free(NULL, ...)` does not change counters;
- `rt_realloc(NULL, old, new, align)` behaves like allocation;
- `rt_realloc(ptr, old, 0, align)` behaves like free;
- failed `rt_realloc` keeps the old allocation and does not change counters;
- aligned allocation and aligned reallocation keep the current copy/free
  behavior;
- `rt_array_forget_allocation` still runs exactly on the free paths that need
  it.

The epic may add internal helper APIs, but new V2 C primitives must use
owner-first arguments and explicit status codes for recoverable failures.

## Proof And Quality Contract

Every runtime-code task must run:

- `make c-check`;
- `make cppcheck`;
- `make runtime-v2-check`;
- `make check`, unless the task document records a narrower approved gate;
- `git diff --check`;
- root and scoped Sentrux scans plus rule checks.

Heap-accounting tasks must also add or run focused probes that prove:

- `rt_heap_stats()` still reports monotonic alloc/free counts;
- live block and live byte totals return to the expected value after free;
- realloc updates counters consistently for grow, shrink, failed, null, and
  zero-size cases;
- aligned allocation paths use the same accounting model as ordinary allocation;
- concurrent worker allocation/free does not race into negative aggregate state
  or unsigned underflow;
- the old global counter symbols are absent or no longer used as the source of
  truth.

Performance evidence must answer whether removing global counter contention
changed the covered Runtime V2 probes. A small allocation-heavy microprobe is
allowed, but it is not a substitute for the existing Runtime V2 liveness gates.
Native net or channel benchmark rows are required only if a task changes code
that can affect those paths beyond accounting calls.

## Refactor Safety Contract

Refactoring in this epic is allowed only when it satisfies this contract:

- write or select the behavior proof before moving code;
- record the dependency cluster and owning module before extraction;
- keep behavior changes out of refactor commits;
- move one responsibility at a time;
- do not create catch-all files such as `common`, `misc`, or vague `helpers`;
- keep new or heavily rewritten runtime files at or below 500 lines;
- reduce or keep flat every touched over-limit file unless the task records a
  specific proving-spike exception;
- do not couple `rt_alloc.c` to async scheduler internals unless the task proves
  why a narrower accounting API cannot work;
- delete code only after proving the symbol is unreachable or obsolete through
  references, build, tests, and Sentrux evidence;
- record rejected paths in `NOTES.md` so they are not rediscovered later.

## Parallelization Model

After Task 1, the dependency map, behavior tests, and static shape tests can be
planned in parallel if their write sets stay separate.

Implementation tasks should stay sequenced until the accounting cell model is
chosen. Review subagents may run after each implementation task. Every subagent
must start with a plan-only pass and wait for main-agent approval before edits,
test-writing, or review work starts.

## Brief Task List

| Task | Document | Purpose |
| --- | --- | --- |
| 1 | `05-tasks/01-kickoff-baseline-and-sentrux.md` | Record checkout, line counts, accepted debt, Sentrux state, heap tests, and final Epic 5 gate plan. |
| 2 | `05-tasks/02-heap-accounting-dependency-map.md` | Map `rt_alloc`, `rt_free`, `rt_realloc`, `rt_heap_stats`, thread contexts, and current stats tests. |
| 3 | `05-tasks/03-heap-stats-contract-tests.md` | Add focused behavior tests for heap stats, realloc, aligned allocation, and concurrent accounting. |
| 4 | `05-tasks/04-heap-accounting-static-shape-tests.md` | Add static checks for shard-owned accounting and absence of global counter source-of-truth. |
| 5 | `05-tasks/05-accounting-cell-skeleton.md` | Introduce the runtime/shard-owned accounting cell model without changing public behavior. |
| 6 | `05-tasks/06-alloc-free-realloc-accounting-migration.md` | Route allocation events through the accounting cells and preserve failure semantics. |
| 7 | `05-tasks/07-heap-stats-aggregation.md` | Make `rt_heap_stats()` aggregate all cells, including cold paths, with compatible snapshots. |
| 8 | `05-tasks/08-concurrency-and-performance-evidence.md` | Run focused concurrent allocation probes and record before/after performance evidence. |
| 9 | `05-tasks/09-runtime-v2-heap-ci-gates.md` | Add `runtime-v2-heap-check` and wire it into local Runtime V2 gates and CI. |
| 10 | `05-tasks/10-epic-closeout-and-static-gates.md` | Consolidate evidence, update durable docs, close or record epic-owned debt, and hand off to Epic 6. |

## Epic Acceptance

Epic 5 is complete only when:

- heap stats are owned by runtime or shard accounting state, not by one
  process-global hot counter block;
- current multi-worker `N=1` behavior does not introduce a replacement shared
  heap-counter bottleneck;
- `rt_alloc`, `rt_free`, `rt_realloc`, and `rt_heap_stats` preserve their public
  ABI and observable behavior;
- focused tests cover allocation, free, realloc, aligned allocation, heap stats,
  cold paths, and concurrent worker accounting;
- stable heap-accounting tests run in `make runtime-v2-check` and CI;
- static checks prevent the old global counter source-of-truth from returning;
- `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`, and
  `git diff --check` pass or have recorded blockers unrelated to Epic 5;
- root, `runtime/`, and `runtime/native/` Sentrux scans and rule checks are
  recorded as pass/fail evidence;
- touched over-limit files have recorded line-count outcomes;
- every allocator/accounting debt discovered in this epic is either closed or
  explicitly recorded in `DEBT.md` with an owner and close condition;
- `05-evidence.md`, `NOTES.md`, this document, and `README.md` are updated with
  the final state and the exact Epic 6 handoff.

## Epic 6 Handoff

Epic 6 should start from `N>1` accept ownership only after heap accounting no
longer adds a global hot counter to every allocation and free. It should still
avoid crossing syntax, remote-free queues, allocator pools, and backend I/O
migration unless those surfaces become explicit Epic 6 scope.
