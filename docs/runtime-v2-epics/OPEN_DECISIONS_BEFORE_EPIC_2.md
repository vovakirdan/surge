# Open Decisions Before Epic 2

This register records which Runtime V2 decisions must be resolved before Epic 2
starts and which decisions can wait for the epic that first crosses that
boundary. It is documentation-only and does not change runtime behavior.

## Accepted Debt Before Epic 2

### Focused VM baseline failure

- Decision: accept the focused VM failure as backend-test debt. Epic 2 may start
  without fixing it. Runtime-code tasks must still name the debt in evidence and
  must not attribute new failures to this baseline without showing the failure
  matches the recorded debt.
- Evidence/source: `01-baseline-evidence.md` sections "Required Commands",
  "Focused VM failure summary", and "Known Debt And Epic 2 Gate Status";
  `01-contract-rules-harness.md` sections "Closeout Evidence" and "Exit
  Criteria For Starting Epic 2"; `NOTES.md` section "Current Baseline Debt".
- Why this category: the backend and test matrix are in a transitional state.
  A later test/backend epic will rewrite the VM/native/LLVM parity and liveness
  tests around stable Runtime V2 contracts.
- Next owner/document: future test/backend matrix epic. Epic 2 evidence must
  keep this debt named while making its own runtime regressions separable.

## Blocks Epic 2 Runtime-Code Completion

### Sentrux rules gate

- Decision: Sentrux scans remain mandatory. Missing `.sentrux/rules.toml` files
  are not passing rule checks. Epic 2 runtime-code completion must either add
  real rules for the active scan path or record an explicit temporary deferral
  without claiming Sentrux rule compliance.
- Evidence/source: `SENTRUX_POLICY.md` sections "Rule File Decision",
  "Required Policy For Future Tasks", and "Open Decisions And Blockers";
  `01-baseline-evidence.md` sections "Sentrux Baseline" and
  "Known Debt And Epic 2 Gate Status"; `01-contract-rules-harness.md` sections "Sentrux
  Policy", "Closeout Evidence", and "Exit Criteria For Starting Epic 2".
- Why this category: the current checkout has scan baselines, but no rule files.
  That is acceptable for docs-only Epic 1 work and blocks Runtime V2 rule
  compliance for code work unless explicitly deferred.
- Next owner/document: Epic 1 closeout for the start condition; first runtime-code
  Epic 2 task for rule creation or temporary deferral evidence.

### VM/native parity guarantee

- Decision: preserve semantic VM/native parity, not identical scheduler
  interleavings. Parity evidence compares backend semantics with native
  `threads=1`; MT behavior stays in separate multi-worker evidence.
- Evidence/source: `docs/RUNTIME.md` backend/VM parity notes,
  `docs/CONCURRENCY.md` native runtime contract notes,
  `01-contract-rules-harness.md` scheduler contract/artifact table, and
  `NOTES.md` section "Durable Decisions".
- Why this category: Epic 2 structural changes must know whether a changed test
  is a semantic regression or only an implementation-artifact change. Native
  global FIFO, worker placement, and stealing must not become hidden parity
  guarantees.
- Next owner/document: first Epic 2 evidence section and any follow-up update to
  `01-contract-rules-harness.md`.

### Epic 2 `N=1` equivalence boundary

- Decision: the first `N=1` structural work may reorganize runtime state only if
  source-visible behavior remains equivalent. At this boundary, preserve channel
  FIFO, task parking at suspension points, cooperative cancellation and timeout
  outcomes, structured join/failfast outcomes, `@local spawn` sendability rules,
  single-worker deterministic test expectations, shutdown halt/drop behavior,
  debug-facing native heap-stat behavior where touched, and the public native ABI
  unless an epic explicitly says otherwise.
- Evidence/source: `01-contract-rules-harness.md` sections "Scheduler
  Contract/Artifact Classification", "Strict Runtime V2 Development Rules",
  "Open Decisions Before Epic 2", and "Exit Criteria For Starting Epic 2";
  `docs/RUNTIME_V2.md` local-first execution and migration plan; `docs/RUNTIME.md`
  async scheduler/runtime notes; `docs/CONCURRENCY.md` current native runtime
  contract notes.
- Why this category: Epic 2 is allowed to change structure before changing
  concurrency. It therefore needs a precise equivalence statement before any
  field or owner movement.
- Next owner/document: first Epic 2 task file and its evidence template entry.

## Can Wait Until Explicit Crossing

### `crosses` function marker

- Decision: do not add or require a `crosses` function marker for Epic 2 `N=1`
  structural work. The marker remains a candidate checked function-level effect,
  similar to `unsafe`, and must be resolved before the cross-shard lowering epic
  if the team wants crossings to propagate into function signatures.
- Evidence/source: `docs/RUNTIME_V2.md` explicit crossing section,
  `01-contract-rules-harness.md` sections "Contract Questions To Freeze" and
  "Open Decisions Before Epic 2", and
  `01-contract-rules-harness-tasks.md` Task 7.
- Why this category: `submit_to` makes the crossing visible at the crossing site.
  Whether callers also see a function-level effect does not block single-shard
  structural work.
- Next owner/document: explicit crossing language-surface epic; future updates to
  `docs/RUNTIME_V2.md` and the language/spec draft.

### `far`/`submit_to` compiler surface

- Decision: keep `far`, `submit_to`, move-only capture checks, and cross-shard
  resume lowering out of Epic 2 unless the parent epic explicitly expands scope.
  The target policy remains that legal crossings are visible in source and that
  borrows or shard-pinned resources do not cross implicitly.
- Evidence/source: `docs/RUNTIME_V2.md:27-34`,
  `docs/RUNTIME_V2.md:618-621`, `docs/RUNTIME_V2.md:626-632`, and
  `docs/RUNTIME_V2.md:668-680`.
- Why this category: these are language, semantic-analysis, async-lowering, and
  ABI decisions, while Epic 2 starts with `N=1` runtime structure.
- Next owner/document: explicit crossing language-surface epic and compiler task
  documents.

### Remote operations cost surface

- Decision: defer remote bounded send, remote `select`, distributed join, and
  shard-pinned resource migration details until explicit crossing work. Keep the
  policy that every remote operation remains Present, Proportional, and
  Predictable; do not make a remote operation look local.
- Evidence/source: `docs/RUNTIME_V2.md:528-558`,
  `docs/RUNTIME_V2.md:562-568`, and `docs/RUNTIME_V2.md:673-689`.
- Why this category: these decisions require the crossing surface and remote
  ownership model. They do not block `N=1` field movement.
- Next owner/document: explicit crossing language-surface epic and later
  cross-shard messaging evidence.

## Can Wait Until Multi-Shard Runtime

### Owner placement and Tier 1 stealing policy

- Decision: Epic 2 `N=1` work does not need to finalize multi-shard owner
  placement. Preserve the current public rule that worker count is a runtime
  property and that parallel mode does not promise global FIFO or fairness. Before
  enabling `N>1`, decide the owner-placement policy and move any legacy Tier 1
  stealing assertion out of the connection hot-path contract.
- Evidence/source: `01-contract-rules-harness.md` scheduler
  contract/artifact table; `docs/CONCURRENCY.md` native runtime sections on
  worker count, scheduling, and parallelism; `docs/RUNTIME_V2.md` migration plan
  notes for owner placement and hot-path stealing.
- Why this category: with one shard, ownership policy can be represented without
  cross-shard execution. The decision becomes observable when multiple shards can
  accept and run connection work.
- Next owner/document: multi-shard runtime epic and MT scheduler evidence.

### Cross-shard messaging, cancellation, and backpressure

- Decision: defer cross-shard transport, bounded inbound backpressure,
  distributed cancellation, stale-generation handling, and remote `select`
  mechanics until the multi-shard runtime/crossing epic. Keep the policy-level
  requirement that the later design must expose owner, wakeup, cancellation,
  lifetime/generation, and backpressure evidence.
- Evidence/source: `RULES.md` explainability rule and
  `docs/RUNTIME_V2.md` sections on remote operations, distributed structured
  concurrency, and migration risk.
- Why this category: these decisions have no effect with `N=1`, but they become
  mandatory once messages can cross shard boundaries.
- Next owner/document: multi-shard runtime epic, crossing epic, and the liveness
  probe plan.

### Distributed structured concurrency

- Decision: keep normal `spawn` shard-local by default in Runtime V2. Distributed
  spawn, distributed join, and cross-shard cancellation remain explicit work for
  the multi-shard runtime and crossing surface.
- Evidence/source: `docs/RUNTIME_V2.md` sections on normal spawn,
  distributed spawn, and distributed structured concurrency; `docs/CONCURRENCY.md`
  structured concurrency and task handle sections.
- Why this category: local structured-concurrency outcomes must be preserved in
  `N=1`; distributed scope ownership only matters once children can run on other
  shards.
- Next owner/document: multi-shard runtime epic and explicit crossing
  language-surface epic.

## Can Wait Until Allocator/Pools

### Per-shard heap counters

- Decision: do not require per-shard heap counters before the first Epic 2 `N=1`
  structure task unless that task touches heap-stat behavior. The planned
  allocator milestone is to move native heap counters to shard-local storage and
  aggregate them on `rt_heap_stats()` reads before or with `N>1` benchmarking.
- Evidence/source: `docs/RUNTIME.md` heap stats notes and
  `docs/RUNTIME_V2.md` sections on per-shard memory ownership and allocator
  migration.
- Why this category: global allocation counters can distort later multi-shard
  measurement, but they are an allocator/pools milestone rather than a global
  Epic 2 start condition.
- Next owner/document: allocator/pools epic and benchmark evidence.

### Hot runtime object pools

- Decision: keep shard-local pools for task state, waiter nodes, connection
  buffers, and parser scratch memory out of the first structural work unless an
  epic explicitly names them. The first allocator step is counter ownership; slab
  or bump pools come after the scheduler result is measurable.
- Evidence/source: `docs/RUNTIME_V2.md` sections on shard-local pools and
  post-ownership allocation optimizations.
- Why this category: object pools are a performance lever after ownership and
  counter behavior are understood, not a prerequisite for `N=1` structure.
- Next owner/document: allocator/pools epic and performance evidence.

### Tier 2 pool split

- Decision: defer the final `submit_to(pool)` and `submit_to(blocking)` surface
  until allocator/pools and explicit crossing work. Preserve the current rule
  that OS-blocking work belongs in `blocking { ... }` on native/LLVM and not on
  executor workers.
- Evidence/source: `docs/CONCURRENCY.md` blocking-work section,
  `docs/RUNTIME.md` blocking pool notes, and `docs/RUNTIME_V2.md` Tier 2
  destination/pool split.
- Why this category: the existing blocking pool behavior is already documented.
  The V2 Tier 2 split needs the crossing construct and pool evidence, not the
  first single-shard structure change.
- Next owner/document: allocator/pools epic, explicit crossing epic, and
  `docs/CONCURRENCY.md` updates.

## Can Wait Until Later IO/Backend Work

### Timer clock wording

- Decision: the timer wording conflict does not block unrelated Epic 2 `N=1`
  structure work, but it must be resolved before timer implementation or timer
  evidence changes. Until then, preserve existing sleep, timeout, cancellation,
  and VM/native semantic outcomes.
- Evidence/source: `01-contract-rules-harness.md` scheduler
  contract/artifact table, `NOTES.md` current baseline debt/handoff notes,
  `docs/RUNTIME.md` backend notes, and `docs/CONCURRENCY.md` timer sections.
- Why this category: the open question concerns timer/backend semantics, not the
  initial runtime-state split.
- Next owner/document: timer/backend task, `docs/CONCURRENCY.md`, and
  `docs/RUNTIME.md`.

### Native shutdown liveness probe

- Decision: preserve observable halt/drop behavior. Add or identify a native
  shutdown liveness/parity probe before any task moves shutdown state or changes
  shutdown signaling. This probe is not required for unrelated docs-only or
  non-shutdown structure work.
- Evidence/source: `01-contract-rules-harness.md` scheduler
  contract/artifact table, `NOTES.md` liveness requirements, `docs/RUNTIME.md`
  shutdown and runtime-state notes, and `LIVENESS_PROBES.md` missing shutdown
  probe.
- Why this category: shutdown is observable backend behavior and needs liveness
  evidence when touched, but it is not a global policy blocker for every
  single-shard structural step.
- Next owner/document: liveness probe plan and the first task that moves shutdown
  state.

### IO capability boundary and backend choice

- Decision: keep the `Io`/`Runtime` capability boundary and optional `io_uring`
  backend out of Epic 2 unless the parent epic explicitly changes scope. A simple
  shard-local `poll`/`epoll` backend is enough to prove ownership first; `io_uring`
  belongs after ownership, lifetime, cancellation, and allocation contracts are
  stable.
- Evidence/source: `docs/RUNTIME_V2.md` `Io`/`Runtime` boundary and backend
  notes, plus `01-contract-rules-harness.md` strict rule that `io_uring` is a
  later lever, not a first fix.
- Why this category: backend selection is a later I/O lever. It does not answer
  whether the current runtime can be reorganized safely with `N=1`.
- Next owner/document: later IO/backend epic and `docs/RUNTIME_V2.md`.
