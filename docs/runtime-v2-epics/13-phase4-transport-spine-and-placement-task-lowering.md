# Epic 13: Phase 4 Transport Spine And Placement Task Lowering

**Goal:** land the first real Runtime V2 Phase 4 execution vertical: an
OS-neutral cross-shard transport spine plus backend lowering for placement task
crossings. The target executable surface is `spawn on shard(id)` /
`spawn on distributed`, `far Task<T>.await()`, `far Task<T>.cancel()`, and
`on shard(id)` / `on distributed` as the immediate "spawn then await" crossing.

**Status:** proposed as of 2026-07-09; revised the same day after a three-way
design review (severability contract, harness-hardening slice, dedicated
execute/reply for immediate `on`, payload constraint, task-suspend invariant,
debt additions). Task documents are drafted in `13-tasks/` (see its
`README.md` for the order rulings and per-task gates). This document remains
authoritative for boundaries and contracts; each task restates the state it
depends on before implementation starts.

## Why This Epic Exists

Epic 11 made crossing visible in the language. Epic 12 preserved crossing
meaning through sema readiness metadata and made unsupported executable
backends fail deterministically. The next step is not more syntax and not more
compile-only readiness. The next step is a small but real Phase 4 vertical:

- a target shard can receive a runtime message without a lost wake;
- a source shard can park and resume on a transport reply;
- a remote task can be published on a chosen destination shard;
- a `far Task<T>` handle can be awaited or cancelled through the task owner;
- the compiler/backend can lower supported crossing forms to that runtime path;
- unsupported crossing forms remain guarded with deterministic diagnostics.

This epic deliberately does **not** try to complete all Phase 4 features. The
RUNTIME_V2 Phase 4 list also includes remote channels, remote `select`,
distributed scope cancellation, migration, remote-free queues, Tier 2 CPU pool
stealing, and allocator ownership. Those are too many moving parts for the
first executable transport slice.

## Starting State

Runtime state after Epic 12 and the two post-closeout fixes:

- `internal/sema/crossing_lowering.go` records crossing readiness metadata for
  accepted crossing sites.
- Backend guards are default-closed. `BackendVM`, `BackendLLVM`, and unknown
  future executable backend values currently report `FUT7014`-`FUT7017` for
  executable crossing forms unless a backend explicitly records transport
  support.
- Dependency-module crossing constructs are now guarded too
  (`2fce7c22 fix(crossing): guard crossing constructs in dependency modules`).
- Imported `far T` signatures are decoded correctly, and a caller awaiting an
  imported `far Task<T>` records a root lowering record and inferred crossing
  effect
  (`c591788e fix(sema): resolve far-typed signatures of imported functions`).
- HIR still has no real crossing nodes; an `ExprOn` reaching HIR lowering is a
  deterministic internal error backstop.
- Native runtime already has per-shard scheduler queues, waiter stores, sleep
  stores, fd registries, and net wake pipes.
- Native runtime does **not** have a general per-shard inbound message queue,
  transport park state, transport credits, transport wake counters, or remote
  task publication protocol.
- `Placement` is a compile-time/prelude surface with opaque runtime payload;
  the runtime ABI for `shard(id)`, `distributed`, and `pool` still needs a real
  representation before lowering can execute.

## Boundary Decisions

**First vertical, not whole Phase 4.** Epic 13 owns the minimum transport and
lowering needed for placement task crossing. It must not silently expand into
remote channels, remote `select`, distributed scope cancellation, migration, or
remote-free ownership.

**Supported placements are explicit.** The initial executable placements are:

- `shard(id)`: deterministic destination shard after range/clamp/diagnostic
  rules are written in the task contract;
- `distributed`: deterministic runtime-selected destination using a documented
  policy and trace evidence that work can leave the caller shard.

`pool` is not executable in this epic unless a task explicitly adds the Tier 2
CPU destination contract. The default assumption is that `spawn on pool` and
`on pool` remain deterministic backend-unavailable or placement-unavailable
diagnostics until the Tier 2 CPU-pool epic. Do not map `pool` to `distributed`
as a shortcut.

**Placement task forms only.** Epic 13 may execute:

- `spawn on shard(id) { ret value; }`;
- `spawn on distributed { ret value; }`;
- `far Task<T>.await()`;
- `far Task<T>.cancel()`;
- `on shard(id) { ret value; }`;
- `on distributed { ret value; }`.

Epic 13 does not execute:

- `on far_handle { ... }`;
- remote `far Channel<T>` send/recv;
- remote `far TcpConn` data operations;
- remote `select`;
- `far` arrays or copyable `far` handles;
- cross-shard resource migration.

**No hidden local fallback.** A supported crossing must route through the
transport path even when the destination is the current shard. Same-shard
shortening may be added later only after the transport path has tests proving
the full remote semantics.

**Transport-capable backend is explicit.** The first likely transport-capable
backend is `BackendLLVM` with the native runtime. `BackendVM` may remain
diagnostic-only in this epic. Unknown executable backend values remain
default-closed.

**No new syntax.** Use the Epic 11 surface as-is: `far T`, `on`, `spawn on`,
inferred crossing effects, `@shard_movable`, and `@shard_pinned`. If a valid
construct cannot be lowered under these rules, stop and bring the design back
for review. Do not introduce shorthand, keywords, or attributes in this epic.

**OS-neutral architecture.** Compiler IR and runtime transport APIs must not
encode Linux-only concepts. `eventfd`, `pipe`, `poll`, `epoll`, `kqueue`, and
IOCP are implementation details behind a wake abstraction. A pipe fallback is
acceptable for the first native transport, but the public shape must allow
non-Linux backends later.

**Bounded transport, not unbounded mailboxes.** The per-shard inbound queue is
bounded, and completion, cancellation, credit-return, and shutdown wake
messages must never be blocked behind data-lane backpressure. Full credit
accounting has no real data-lane consumer until the remote-channel epic, so it
stays a documented, non-promoted proving spike here: build the bounded queue
and control lane now, prove the park protocol on them, and do not promote
credit machinery that nothing exercises.

**Park protocol is mandatory.** Any shard sleep/park change must satisfy the
RUNTIME_V2 seq-cst enqueue/PARKED ordering rule and the debug invariant:
no PARKED shard may have a non-empty inbound transport queue at a safepoint.

**Remote handle model is a decision, not an accident.** Before lowering, Task 1
must decide whether `far Task<T>` is represented by the existing stable
`rt_task*` handle plus owner routing, or by a new packed/generation handle. The
decision must cover stale handle prevention, cancellation routing, await
consumption, and cleanup. Whatever the representation, each far task carries a
generation or no-reuse token so completion and cancellation cannot
double-complete a handle; the token is mandatory, not conditional.

**Detached affine far Task is the severability contract.** Epic 13 executes
placed child tasks without the distributed-scope protocol. That is severable
only under the model Epic 11 already fixed statically
(`11-tasks/block-03-spawn-on-remote-spawn.md`, lifecycle matrix `L01`-`L04`):
`far Task<T>` is strictly affine, a dropped handle is a compile error, and drop
is not an implicit detach or cancel. Task 1 must restate this as the runtime
contract:

- a placed child is NOT enrolled in the enclosing local scope's `join_all` or
  failfast accounting; the affine handle is the only lifecycle edge;
- publication wait is not cancellable: caller cancellation takes effect only
  after the publication ack resolves and then routes as a cancel on the
  returned handle, so "was the remote task created" is never ambiguous;
- runtime teardown that sema cannot see (caller cancelled by failfast, unwind,
  or shutdown while holding an unconsumed handle) must route a release/cancel
  for the remote task; this cleanup path is owned by Epic 13.

Scope-edge generation reuse, scope-owner completion accounting, and failfast
broadcasts remain out of scope for this epic.

**Payloads are plain-data or copyable until remote-free exists.** RUNTIME_V2
gives heap-owned crossing payloads a pointer-move representation whose memory
returns to the allocation owner through a remote-free path — and that path is
Phase 5 work. Epic 13 therefore either restricts executable crossing captures
and `ret` payloads to plain-data/copyable representations, or records a written
safety proof that the current allocator makes cross-shard free safe, including
the Epic 5 shard heap-accounting attribution question. Silently moving
heap-owned payloads without one of those two answers is not acceptable.

**Reply waits are task suspends, not shard parks.** A caller waiting for a
transport reply suspends its task cooperatively; it must not park the shard.
On a self-crossing destination — including every run under `SURGE_SHARDS=1` —
the destination shard IS the caller shard, and a shard-level park would sleep
the only worker able to drain the inbound queue, run the body, and produce the
reply: a silent deadlock, not a visible failure. The shard park/wake protocol
governs shard idle sleep only. Task 1 records this as an invariant, and the
self-crossing test row is its forcing function.

## Owned Surfaces

Epic 13 owns:

- native runtime transport state under `runtime/native/`;
- per-shard inbound queue, wake, and park-state integration;
- transport trace counters and liveness probes;
- `Placement` runtime ABI for `shard(id)` and `distributed`;
- remote task publication and returned `far Task<T>` lifecycle;
- remote task await/cancel request/reply routing;
- HIR/MIR/lowering representation for the supported placement task forms;
- LLVM/native backend lowering for the supported placement task forms;
- backend guard split between transport-capable and unsupported backends;
- focused Runtime V2 CI gate for the new crossing transport vertical;
- documentation/debt updates caused by the transport shape.

Epic 13 may inspect or touch:

- `internal/buildpipeline`, `internal/hir`, `internal/mir`,
  `internal/backend/llvm`, `internal/sema`, and `internal/driver`;
- `core/intrinsics.sg` for placement ABI/intrinsic declarations;
- `runtime/native/rt_async_*`, scheduler placement, wake, trace, and task
  lifecycle files;
- test harness code needed for deterministic multi-shard runtime execution.

Epic 13 does not own:

- remote channel operation lowering;
- remote `select` coordinator;
- distributed scope cancellation/completion protocol;
- migration of live connections or resources;
- allocator owner metadata and remote-free queues, except for a recorded
  compatibility note if current allocator behavior makes the first vertical
  safe without them;
- VM execution support unless a task explicitly expands the backend matrix.

## Runtime Transport Contract

The transport spine must define a small set of message categories before code
lands. Names may change in task documents, but the semantic categories are
required:

| Category | Direction | Purpose |
| --- | --- | --- |
| Remote spawn request | caller shard -> destination shard | publish or request creation of a task body on the destination shard |
| Remote spawn ack | destination shard -> caller shard | release `spawn on` publication wait and return a `far Task<T>` handle |
| Remote task completion | task owner shard -> waiter shard | resume a parked `far Task.await` caller with `TaskResult<T>` |
| Remote task cancel request | requester shard -> task owner shard | request cancellation of a remote task |
| Remote task cancel ack | task owner shard -> requester shard | resume `far Task.cancel` with `TaskResult<nothing>` |
| Immediate `on` execute request | caller shard -> destination shard | run an `on` block body on the destination and register the reply route |
| Immediate `on` reply | destination shard -> caller shard | resume the suspended caller with `TaskResult<T>` |
| Credit/control message | target shard -> source shard | release data-lane backpressure without being blocked by the data lane |
| Shutdown wake/control | runtime -> all shards | ensure transport waiters do not sleep through shutdown |

Required invariants:

- enqueue publishes the complete message before any wake decision;
- consumer park stores PARKED with seq-cst ordering and re-checks inbound work
  before sleeping;
- producer wake observes PARKED with seq-cst ordering before writing the wake
  fd;
- a completion/cancel reply cannot be stranded behind a full data lane;
- each message has one owning free path;
- transport wake writes are counted separately from net poll wake writes;
- a caller waiting for a transport reply suspends its task and leaves the
  shard's worker loop free to drain the inbound queue; shard park is reserved
  for shard idle sleep;
- `SURGE_SHARDS=1` still exercises the transport code path for supported
  crossing forms, but multi-shard rows must prove work can leave shard 0.

## Lowering Contract

Epic 13 must transition from Epic 12's guard-before-HIR readiness shape to real
lowering for the supported forms. The transition must be explicit:

- unsupported backends still stop before HIR/MIR with `FUT7014`-`FUT7017` or a
  more precise placement-unavailable diagnostic;
- transport-capable backend paths may let supported crossing forms reach
  HIR/MIR, but only after HIR/MIR have typed nodes or instructions for them;
- the old HIR internal-error backstop remains for impossible guard bypasses;
- the compiler must not lower a crossing form to local `spawn`, local
  `.await()`, or local channel operations.

Required lowering rows:

| Source form | Epic 13 expected executable outcome |
| --- | --- |
| `spawn on shard(id) { ret value; }` | remote publication wait, then returned `far Task<T>` handle |
| `spawn on distributed { ret value; }` | destination selected by documented policy, publication wait, then returned `far Task<T>` handle |
| `far Task<T>.await()` | consume/wait through task owner and return `TaskResult<T>` |
| `far Task<T>.cancel()` | consume/cancel through task owner and return `TaskResult<nothing>` |
| `on shard(id) { ret value; }` | immediate remote execution returning `TaskResult<T>` |
| `on distributed { ret value; }` | immediate remote execution returning `TaskResult<T>` |

`on placement` lowers to the dedicated immediate execute/reply message
category: one request, one reply, one cancellation token, and no publicly
observable `far Task<T>` handle. The "remote spawn + await" desugar is
rejected as the default shape: the window between publication ack and await
creates a transient handle with its own cancellation/cleanup seam, pays a
second cross-shard round-trip on the hottest immediate path, and cannot
produce equivalent trace evidence. A task may revisit the desugar only with a
written proof that it meets the same user-visible semantics, handle cleanup,
cancellation behavior, and trace evidence.

## Test And Proof Contract

Every implementation task starts with tests or a bounded proving spike plan.

Required compile/backend proof:

- `BackendLLVM` transport-capable rows execute the supported forms;
- `BackendVM` and unknown executable backend rows remain deterministic and do
  not attempt local fallback;
- compile-only paths do not report backend-unavailable diagnostics;
- invalid parser/sema cases still fail before backend lowering;
- imported-module crossing constructs stay covered by backend guards or real
  lowering, matching the post-Epic-12 dependency-module fixes.

Required runtime proof:

- `SURGE_SHARDS=1,2,8` rows for remote spawn, await, cancel, and immediate
  `on`;
- a self-crossing row (destination equals the caller shard, including every
  `SURGE_SHARDS=1` run) completes without deadlock, proving reply waits are
  task suspends rather than shard parks;
- at least one row proves the executing task owner differs from the caller
  shard for `SURGE_SHARDS>1`;
- cancellation races do not strand a waiter or double-complete a task;
- shutdown wakes transport waiters on every shard;
- a debug/negative-control proof catches the lost-wake park race;
- a PARKED-with-inbound-work invariant is either runtime-asserted in debug
  builds or proved by a focused deterministic test.

Required performance/trace proof:

- new trace counters for transport enqueue, wake writes, wake elision,
  credit stalls, completion replies, cancellation replies, stale
  generation-token drops, and unsupported fallback attempts;
- one native benchmark or stress row showing crossing throughput/latency under
  `SURGE_SHARDS=1,2,8`;
- performance expectations calibrated honestly: the first vertical proves
  correctness and liveness, not final line-rate scaling for remote work.

Required quality proof:

- `make runtime-v2-crossing-check` remains green, with expected rows updated
  when supported forms stop producing `FUT7014`-`FUT7017`;
- new focused gate, likely `runtime-v2-transport-check`, is stable before it is
  wired into `runtime-v2-check`;
- `make check`;
- `make c-check`;
- `make cppcheck` for native C changes;
- `./check_file_sizes.sh -a`;
- Sentrux root plus scoped scans for `runtime/`, `runtime/native/`, and
  `internal/` if compiler files are touched;
- `NOTES.md`, `DEBT.md`, this epic document, and `README.md` updated at
  closeout.

## Debt Ownership

Epic 13 must start by reviewing:

- `RV2-DEBT-024`: higher-order/function-type crossing-effect propagation. The
  post-Epic-12 imported `far Task<T>` fix means direct imported far-task usage
  no longer blocks lowering. The remaining question is whether this transport
  lowering needs exported crossing-effect metadata for function values,
  higher-order calls, or imported ordinary functions that hide crossing in the
  callee. If not, reaffirm the deferral with a test and keep the owner on a
  later effect-system epic.
- `RV2-DEBT-005`: native runtime files over the legacy LOC ceiling. If Epic 13
  touches any allowlisted file, the task must record whether the file shrank,
  stayed flat, or gained a follow-up owner.
- `RV2-DEBT-006` and `RV2-DEBT-017`: channel benchmark and sync-channel compat
  debt are not in scope unless transport benchmarking or remote task lifecycle
  work directly depends on them.
- `RV2-DEBT-011` and `RV2-DEBT-018`: in scope, narrowly. The transport gate
  executes native `SURGE_SHARDS` rows through the same `internal/vm` LLVM
  build/run harness these debts poison, and a lost-wake negative control on a
  flaky harness proves nothing — a transient empty-output failure is
  indistinguishable from a real regression. The harness-hardening slice must
  give the transport gate's execution path per-run unique artifact directories
  (or locking) and empty-output capture; the broad matrix rewrite stays with
  the Backend/Test Matrix Cleanup epic.
- `RV2-DEBT-001`, `RV2-DEBT-002`: not in scope unless Epic 13 chooses to add
  VM execution support or needs the broad VM/native/LLVM matrix as a green
  gate.
- `RV2-DEBT-025` and `RV2-DEBT-026`: Task 1 must review both while deciding
  the `far Task<T>` handle model. `RV2-DEBT-025` (copyable `far` handles) is
  load-bearing: affine single-consumer handles are what make the
  completion/cancel race tractable, so Task 1 either reaffirms affinity as a
  transport invariant and reassigns the stale "Epic 11 follow-up" owner, or
  stops for design review. `RV2-DEBT-026` (`far` arrays) only needs its stale
  owner reassigned.

Do not close unrelated debt just because a file is nearby.

## Proving Spikes Allowed

Allowed bounded spikes:

- remote task handle representation: existing `rt_task*` plus owner route vs
  packed handle with generation;
- inbound queue implementation shape: ring buffer, shard-locked queue, or
  MPSC queue, with bounded capacity and control-lane escape hatch;
- wake fd abstraction: reuse pipe fallback first vs introduce eventfd behind a
  platform-neutral wrapper;
- HIR/MIR representation: direct crossing instructions vs desugaring
  `on` into remote spawn + await;
- `distributed` placement policy: round-robin, hash, or non-current-shard
  selection, with trace proof.

Spike rules:

- record hypothesis, scope, proof command, success/failure criteria, and
  rollback note before code changes;
- do not keep untested experimental fallback execution;
- do not add syntax;
- delete or rewrite spike code before task completion.

## Planned Task Slices

1. **Kickoff dependency map and baseline.** Map current compiler guard points,
   runtime scheduler/wake paths, task handle representation, placement ABI,
   open debt (including `RV2-DEBT-025`/`RV2-DEBT-026`), and exact baseline
   commands. Decide the first executable placement set, the `far Task<T>`
   handle model with its mandatory generation/no-reuse token, the payload
   representation answer, and record the detached-affine-far-Task severability
   contract and the task-suspend-vs-shard-park invariant.
2. **Transport harness hardening.** Give the transport gate's `internal/vm`
   LLVM build/run execution path per-run unique artifact directories (or
   locking) and empty-output capture (`RV2-DEBT-011`/`RV2-DEBT-018`, narrow
   scope), so later park/wake proofs and `SURGE_SHARDS` rows run on a harness
   whose failures are trustworthy.
3. **Transport contract tests and park/wake proof.** Write deterministic tests
   and/or negative controls for inbound enqueue, park-state StoreLoad ordering,
   wake elision, shutdown wake, and PARKED-with-inbound-work detection.
4. **Inbound transport spine.** Add per-shard bounded inbound queue, control
   lane, wake abstraction, drain points in the worker loop, trace counters, and
   shutdown cleanup without crossing lowering yet.
5. **Placement ABI and destination resolution.** Implement runtime
   representation for `shard(id)` and `distributed`; keep `pool` explicitly
   unsupported unless the task expands the scope with approval.
6. **Remote task publication runtime API.** Add status-code based native APIs
   for remote task publication and publication ack, preserving owner-shard
   invariants and parent/scope boundaries.
7. **Compiler lowering representation.** Introduce HIR/MIR nodes or
   instructions for the supported crossing forms, split backend guards by
   transport capability, and keep unsupported rows deterministic.
8. **`spawn on` executable vertical.** Lower and execute `spawn on shard(id)` /
   `spawn on distributed`, proving remote owner placement and publication wait.
9. **`far Task.await/cancel` executable vertical.** Route await and cancel
   through task owner messages, with race and shutdown tests.
10. **Immediate `on` executable vertical.** Lower `on shard(id)` /
    `on distributed` to the dedicated execute/reply message category and prove
    `TaskResult<T>` behavior.
11. **Unsupported forms and matrix hardening.** Ensure `pool`, `on far_handle`,
    remote channel/select, VM, and unknown backends remain deterministic and do
    not reach hidden local lowering.
12. **Benchmark, CI gate, and closeout.** Promote the stable transport gate,
    run full quality checks, update debt/notes/docs, and write the handoff for
    remote channels / remote select / Tier 2.

Slices may be split before implementation. The epic must not skip the
transport wake proof or the unsupported-form matrix. Slices 8 and 9 gate
together: `spawn on` must not become publicly executable before the
await/cancel discharge path exists, or the vertical would mint `far Task<T>`
handles with no way to consume them.

## Acceptance Criteria

Epic 13 is complete only when:

- supported placement task crossing forms execute on the transport-capable
  backend and are covered by focused tests;
- unsupported crossing forms and unsupported backends still fail
  deterministically before hidden local lowering;
- `SURGE_SHARDS=1,2,8` runtime rows pass for remote spawn, await, cancel, and
  immediate `on`;
- a multi-shard row proves work actually runs on a non-caller shard;
- a self-crossing row proves reply waits are task suspends, not shard parks;
- the far-task generation/no-reuse token is in place and a completion/cancel
  race row proves a handle cannot double-complete;
- inbound transport wake/park has a deterministic proof, including a
  negative-control or equivalent lost-wake detector;
- transport queues are bounded or the epic records an explicitly approved
  bounded-transport deferral with a replacement safety proof;
- transport trace counters explain enqueue/wake/completion/cancel behavior;
- `runtime-v2-crossing-check` and the new transport gate are green and wired
  into `runtime-v2-check`;
- `make check`, `make c-check`, `make cppcheck`, LOC, and scoped Sentrux gates
  pass;
- `RV2-DEBT-024` is either partially closed for the direct lowering need or
  reaffirmed with the exact remaining higher-order boundary;
- docs state which Phase 4 surfaces are now executable and which remain future
  work.

## Handoff To Later Epics

The likely follow-up epics after Epic 13 are:

- remote channel send/recv over the transport spine;
- remote `select` slow coordinator with generation tokens;
- Tier 2 CPU pool lowering for `spawn on pool`;
- distributed scope cancellation/completion messages;
- allocator owner metadata and remote-free queues;
- optional VM transport model or an explicit permanent VM diagnostic policy.

Epic 13 should close with enough evidence that those epics reuse the transport
spine instead of inventing parallel wake/message paths.
