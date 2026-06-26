# Epic 1: Runtime V2 Contract, Rules, And Harness

**Goal:** make Runtime V2 implementable by turning the target architecture into
checked contracts, strict development rules, explicit C status-code policy, and
repeatable baseline evidence.

**Approach:** this epic does not change scheduler behavior. It creates the
decision records, quality gates, and measurement harness that later runtime and
compiler epics must use. The result should make it hard to merge a Runtime V2
change without proving liveness, preserving intentional language contracts, and
showing whether performance moved for the right reason.

**Status:** draft.

**Task breakdown:** `docs/runtime-v2-epics/01-contract-rules-harness-tasks.md`.

**Primary sources:**

- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/01-baseline-evidence.md`
- `docs/RUNTIME_V2.md`
- `docs/2026-06-25-runtime-net-scheduler-refactor-plan.md`
- `docs/RUNTIME.md`
- `docs/CONCURRENCY.md`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_async_channel.c`
- `runtime/native/rt_alloc.c`
- `scripts/bench_native_net.sh`
- `scripts/bench_native_channels.sh`
- `internal/vm/mt_executor_test.go`
- `internal/vm/mt_correctness_test.go`

## Scope

Included:

- define which scheduler semantics are language contracts and which are current
  implementation artifacts;
- define the first version of strict Runtime V2 development rules;
- define the explicit status-code policy for new V2 C primitives;
- define the working-notes policy for context switching and evidence capture;
- define the evidence template every later epic must fill in;
- capture current baseline commands and expected counters;
- identify missing probes needed before changing `rt_executor` structure;
- document the open `submit_to` / `crosses` decision path, without resolving it
  in this epic unless the answer is needed immediately.

Not included:

- no `rt_runtime` / `rt_shard` implementation yet;
- no waiter or fd-registry rewrite;
- no parser, semantic-analysis, MIR, LLVM, or runtime ABI changes;
- no benchmark tuning by machine-specific constants.

## Contract Questions To Freeze

These are blockers for later epics if they remain ambiguous:

- Which ordering guarantees, if any, are promised by Surge async scheduling?
- Are FIFO waiter semantics observable language behavior or only current
  runtime behavior?
- What must stay identical between VM async tests and native LLVM runtime tests?
- Which operations may run on an I/O polling thread before V2 shards exist?
- What is the minimum source-level rule for visible crossing before `far` and
  `submit_to` exist?
- What is the acceptance rule for a slow path: Present, Proportional,
  Predictable, or rejected?
- Which trace counters are mandatory before and after each scheduler change?

## Scheduler Contract/Artifact Classification

This table is conservative. "Language contract" means source-visible behavior or
documented backend parity policy. "Implementation artifact" means current
VM/native shape or a current test expectation that may change after an explicit
Runtime V2 decision.

| Behavior | Language contract | Current implementation artifact | Evidence | Decision before Epic 2 |
| --- | --- | --- | --- | --- |
| FIFO waiter behavior | `Channel<T>` is FIFO. `select` uses top-to-bottom deterministic tie-break. Parallel native mode does not promise global FIFO or fairness across workers. | VM/asyncrt uses deterministic FIFO ready queues and wakes the oldest waiter for a key. Native currently has one shared FIFO waiter registration list. | `docs/LANGUAGE.md:1600`, `docs/LANGUAGE.md:1612`, `docs/CONCURRENCY.md:249`, `docs/CONCURRENCY.md:367`, `docs/CONCURRENCY.md:370`, `docs/CONCURRENCY.md:442`, `docs/RUNTIME.md:82`, `docs/RUNTIME.md:135`, `internal/asyncrt/asyncrt.go:394` | Preserve channel FIFO and single-worker deterministic test expectations. Owner-local waiter queues may replace the shared FIFO list; do not promote native global FIFO/fairness to a language contract. |
| Worker placement | Worker count is a runtime property. A task must never be polled concurrently by multiple workers; the worker chosen to run it is not source-visible. | Native uses worker-local queues, global inject, local-first wake placement, and stealing. `SCHED_TRACE` exposes `local`, `inject`, and `steal` counters. | `docs/CONCURRENCY.md:12`, `docs/CONCURRENCY.md:21`, `docs/CONCURRENCY.md:307`, `docs/CONCURRENCY.md:322`, `docs/RUNTIME.md:168`, `docs/RUNTIME.md:193`, `docs/RUNTIME.md:326` | Epic 2 `N=1` structure may move queue ownership without promising affinity. Before `N>1`, define owner placement as runtime policy, not language behavior. |
| Work stealing | No source program can rely on stealing. Parallel interleavings are nondeterministic. | Current native MT scheduler steals from worker-local queues, and `TestMTWorkStealing` expects `steal > 0`. Runtime V2 targets Tier 2 CPU offload, not Tier 1 connection stealing. | `docs/CONCURRENCY.md:342`, `docs/CONCURRENCY.md:442`, `docs/RUNTIME.md:170`, `internal/vm/mt_executor_test.go:1350`, `internal/vm/mt_executor_test.go:1422`, `docs/RUNTIME_V2.md:412`, `docs/RUNTIME_V2.md:666` | Before disabling Tier 1 stealing, re-scope the current work-stealing test to legacy/current-runtime evidence or move the stealing assertion to explicit Tier 2 work. |
| Local spawn | `spawn` schedules a `Task<T>` and returns a handle. `@local spawn` allows `@nosend` captures, and its handle cannot cross sendable boundaries. | VM spawn enqueues FIFO and records a parent-child edge when spawned from another task. Current native placement can still run the task on another worker through scheduler queues. Runtime V2 keeps ordinary spawn shard-local; distributed spawn must be explicit. | `docs/CONCURRENCY.md:129`, `docs/CONCURRENCY.md:139`, `docs/CONCURRENCY.md:141`, `docs/CONCURRENCY.md:166`, `docs/RUNTIME_V2.md:366`, `docs/RUNTIME_V2.md:380`, `internal/asyncrt/asyncrt.go:175`, `internal/vm/vm_async_borrow_regression_test.go:63` | Preserve source-level `spawn`/`@local spawn` rules. Epic 2 can model spawn as owner-local with `N=1`; `far Task<T>` and `submit_to(distributed)` remain later language-surface work. |
| Join/cancel | `.await()` consumes a task handle and returns `TaskResult<T>`. `cancel()` is best-effort and observed at suspension points. Scopes join children before completion; `@failfast` cancels siblings. | asyncrt stores children in task order, cancels and join-all scans in that order, and wakes join waiters through current waiter keys. Native uses the shared waiter list for join/cancel paths. | `docs/CONCURRENCY.md:71`, `docs/CONCURRENCY.md:76`, `docs/CONCURRENCY.md:203`, `docs/CONCURRENCY.md:222`, `docs/LANGUAGE.md:1723`, `internal/asyncrt/scope.go:108`, `internal/asyncrt/scope.go:123`, `internal/vm/mt_correctness_test.go:315`, `internal/vm/mt_correctness_test.go:412` | Preserve cooperative cancellation, structured join, and failfast outcomes. Treat child traversal order as an artifact unless a later test/spec makes ordering observable. |
| Channel handoff | Channels are typed FIFO handles. `send`/`recv`/`close` may suspend in async code; closed receive returns `nothing`, closed send is an error; sender/receiver fairness is not specified. | Direct async channel operations park tasks and should not pin workers. Sync helper channel operations use the compatibility path, which may pin workers and start compensation. Current native channel handoff uses the global lock and shared waiters. | `docs/CONCURRENCY.md:276`, `docs/CONCURRENCY.md:296`, `docs/CONCURRENCY.md:328`, `docs/RUNTIME.md:210`, `docs/RUNTIME.md:243`, `docs/RUNTIME.md:246`, `internal/vm/mt_executor_test.go:1007`, `internal/vm/mt_executor_test.go:1131`, `docs/RUNTIME_V2.md:580` | Preserve channel FIFO and async task parking. Channel waiter storage and handoff placement may change; sync helper fallback remains compatibility evidence, not the target hot path. |
| Timers | `sleep`, `timeout`, and `checkpoint` are suspension/cancellation points. `timeout` returns `Cancelled()` on deadline and cancels the target. | VM/asyncrt uses a timer heap with deadline/id ordering and virtual time by default. Current docs disagree on native timer wording: `CONCURRENCY.md` says executor time without a real-time switch; `LANGUAGE.md` says native/LLVM use real time. | `docs/CONCURRENCY.md:214`, `docs/CONCURRENCY.md:222`, `docs/CONCURRENCY.md:224`, `docs/CONCURRENCY.md:226`, `docs/LANGUAGE.md:1738`, `docs/LANGUAGE.md:1741`, `internal/asyncrt/timer.go:21`, `internal/asyncrt/timer.go:51`, `internal/vm/mt_correctness_test.go:374` | Resolve the timer clock wording before timer implementation work. Epic 2 field moves must preserve sleep/timeout/cancellation outcomes and record any skipped timer parity evidence. |
| Shutdown | Scheduler-level shutdown is not fully specified as language syntax. Observable host behavior includes `rt_exit`/panic halting the program and dropping async/channel payloads. | VM shutdown drops frames, globals, async tasks, and buffered channel payloads. Native runtime has shutdown state inside the global executor and signals I/O when shutdown changes. | `internal/vm/vm_async_shutdown_test.go:5`, `internal/vm/vm_async_shutdown_test.go:53`, `internal/vm/vm_async_shutdown_test.go:105`, `internal/vm/shutdown.go:8`, `docs/RUNTIME.md:158`, `docs/RUNTIME.md:273` | Preserve halt/drop behavior. Before moving shutdown state in native code, add or identify a native shutdown liveness/parity probe. |
| VM/native parity | VM and native share language semantics, but not scheduler implementation. Parity tests compare backend semantics with native `threads=1`; MT behavior is validated separately. | VM/asyncrt is single-worker deterministic FIFO. Golden tests may implicitly depend on FIFO ordering; native MT tests cover multi-worker races, handoff, and stealing. | `docs/RUNTIME.md:43`, `docs/RUNTIME.md:49`, `docs/RUNTIME.md:80`, `docs/CONCURRENCY.md:351`, `docs/RUNTIME_V2.md:611`, `internal/vm/mt_executor_test.go:238`, `internal/vm/vm_async_golden_test.go:14` | Keep semantic parity evidence separate from MT scheduler evidence. Before changing scheduler ordering, audit golden FIFO assumptions and update tests so native global FIFO is not treated as a contract. |

Decision notes:

- Epic 2 `N=1` work may move fields and queues only if source-visible async
  outcomes, channel FIFO, cooperative cancellation, task parking, and shutdown
  behavior stay unchanged.
- Current global waiters, global inject, local worker queues, native Tier 1
  stealing, and sync-channel compensation are current implementation artifacts.
- Timer clock wording and the current native work-stealing test need explicit
  follow-up before timer or Tier 1 stealing work starts.

## Sentrux Policy

The current Runtime V2 policy is recorded in
`docs/runtime-v2-epics/SENTRUX_POLICY.md`.

Required scan paths:

- repository root: `/home/zov/projects/surge/surge`;
- runtime scope: `/home/zov/projects/surge/surge/runtime`.

Current baselines:

- repository `quality_signal=6210`, health bottleneck `modularity`;
- runtime `quality_signal=5147`, health bottleneck `redundancy`.

No `.sentrux/rules.toml` exists at either scan path. This is an open rule
enforcement blocker, not a successful rule check. Docs-only Epic 1 tasks may
complete while recording it. Runtime-code tasks after Epic 1 must not claim
Sentrux rule compliance until the relevant rules file exists or an epic records
an explicit temporary deferral.

## Evidence Template And Baseline

Future Runtime V2 tasks must use
`docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md` for closeout evidence. The template
requires task scope, baseline commit/status, files touched, contracts touched,
root and scoped Sentrux signals, commands, benchmarks, liveness probes, known
regressions, dead ends, rollback, follow-ups, and notes consolidation.

Proving spikes must define their hypothesis, allowed files and surfaces,
explicitly non-final behavior, proof test or benchmark or trace or invariant,
success criteria, failure criteria, and rollback before implementation.

Current checkout evidence is recorded in
`docs/runtime-v2-epics/01-baseline-evidence.md`.

Important baseline blockers before Epic 2:

- `go test ./internal/vm -run 'MT|Async|Net|LLVM'` fails in the current checkout
  when timeout-sensitive tests are not skipped;
- default `make check` passes because `SURGE_SKIP_TIMEOUT_TESTS=1` skips those
  timeout-sensitive VM/LLVM tests;
- Sentrux `check_rules` cannot pass as a rule gate until root and/or scoped
  rules files exist.

## Strict Runtime V2 Development Rules

Global rules live in `docs/runtime-v2-epics/RULES.md`. The list below is the
first epic's local summary and must not weaken that document.

1. Start with the current code path and write down the exact behavior being
   preserved before moving fields or queues.
2. Introduce V2-shaped structure with `N=1` before enabling new concurrency.
3. Do not add hot-path work stealing for connection tasks.
4. Do not hide cross-shard or offload cost behind local-looking syntax.
5. Do not use a machine-specific constant as the durable scheduler design.
6. Do not merge a scheduler, wakeup, cancellation, or transport change without a
   liveness proof.
7. Do not remove a global lock or list unless the replacement states its owner,
   cancellation path, and wakeup path.
8. Keep the public native ABI stable unless an epic explicitly says otherwise.
9. Keep VM/native semantic drift visible: if the VM relies on deterministic FIFO
   behavior, document whether native must preserve it.
10. Treat `io_uring`, slab pools, and migration heuristics as later levers, not
    first fixes.
11. New V2 C primitives return explicit status codes for recoverable errors;
    `panic_msg` is not the primitive error-handling contract.
12. Working notes are updated before starting tasks, after evidence runs, and
    before closing an epic.

## Acceptance Gates

This epic is complete when:

- a Runtime V2 rules document exists and is linked from this epic;
- the status-code policy for new V2 C primitives is recorded in the rules;
- `docs/runtime-v2-epics/NOTES.md` exists and captures current state, tested
  paths, untested paths, dead ends, and open decisions;
- the language-contract questions above are answered or explicitly recorded as
  later blockers;
- the baseline command set runs or has recorded local blockers;
- baseline benchmark reports are regenerated or their latest valid reports are
  referenced;
- the liveness probe plan covers net wakeups, channel wakeups, cancellation, and
  shutdown;
- every later epic can copy a standard evidence section instead of inventing its
  own proof format.

Minimum commands to record:

```bash
make check
make c-check
make cppcheck
go test ./internal/vm -run 'MT|Async|Net|LLVM'
./scripts/bench_native_net.sh
./scripts/bench_native_channels.sh
```

For long-running or environment-sensitive checks, record the exact reason if a
command is skipped.

## Brief Task List

### Task 1: Write Runtime V2 Development Rules

Maintain `docs/runtime-v2-epics/RULES.md`. It must include the strict rules
above, the standing gates, the proving-spike rule, the explicit C status-code
policy, the working-notes policy, and the rule that each epic records preserved
behavior before structural edits.

### Task 2: Classify Scheduler Semantics

Audit `docs/RUNTIME.md`, `docs/CONCURRENCY.md`, async VM tests, and native MT
tests. Produce a short table separating language contracts from implementation
artifacts, especially around FIFO behavior, worker placement, cancellation,
join, channel handoff, timers, and shutdown.

### Task 3: Define The Sentrux Rule Policy

Record how repository and scoped Sentrux checks apply to Runtime V2 work. The
current policy lives in `SENTRUX_POLICY.md`; missing rule files are recorded as
an Epic 2 code-completion blocker.

### Task 4: Define The Evidence Template

Create the template that every Runtime V2 epic must fill in: baseline commit,
files touched, contracts touched, test commands, benchmark rows, trace counters,
liveness proof, known regressions, proving-spike proof, and rollback notes.

### Task 5: Refresh Baseline Evidence

Run or intentionally skip the baseline commands. Capture current native net and
channel benchmark report paths, the important counters, and whether they still
match the Runtime V2 hypothesis about global scheduler state.

### Task 6: Define Liveness Probes

List required liveness probes for later work: SIGUSR1 live trace snapshots,
timeout-wrapped MT tests, lost-wakeup invariants, cancellation races, shutdown
drain, and any missing test that should be written before moving waiters.

### Task 7: Record Open Decisions Before Epic 2

Write down which decisions must be resolved before structural `N=1` work and
which can wait until explicit crossing work. The `crosses` function marker can
wait until the language-surface epic unless it blocks the contract table.

### Task 8: Maintain Working Notes And Close The Epic

Keep `docs/runtime-v2-epics/NOTES.md` current during the epic. At close, move
durable decisions and evidence from notes into the epic document and linked
docs, leaving notes as a handoff summary for the next task.

## Exit Criteria For Starting Epic 2

Epic 2 may start only when:

- the development rules are committed or explicitly accepted as the active
  working rules;
- baseline evidence exists for the current checkout;
- the focused VM baseline failure is either fixed or explicitly accepted as
  pre-existing debt for the first structural task;
- Sentrux missing-rules status is either resolved or explicitly deferred for
  the first structural task without claiming rule compliance;
- the contract/artifact split is clear enough to move `rt_executor` fields
  without guessing whether tests rely on the old scheduler shape;
- the first `N=1` structure task can name exactly which behavior must remain
  byte-for-byte or counter-for-counter equivalent.
