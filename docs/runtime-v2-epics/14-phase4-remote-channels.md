# Epic 14: Remote Channels Over The Transport Spine

## Closeout (2026-07-12)

All tasks complete (1, 1.5, 2-3, 4, 5, 5b, 6 — see `14-tasks/README.md`).
Acceptance against the draft criteria:

- Anchored `on ch` send/recv/close execute on LLVM across
  `SURGE_SHARDS=1,2,8` incl. owner==caller (production capability, no
  override): `TestRuntimeV2OnChAnchoredOpsAcrossShards`.
- The race/failure matrix is test-owned and green repeatedly (behavior
  suite rows 1-10, incl. the self-deadlock expected-panic rows and the
  leak census).
- Single-producer owner-side FIFO observed source-side (e2e 41-then-42);
  the cross-producer negative observation is pinned at the harness level
  (`anchored-cross-producer-order`: values land in the owner's local-lane
  execution order, not block-start order — the first-started block's
  value arrives second when its body is held at a gate). Only the
  SOURCE-LEVEL two-producer program stays with `RV2-DEBT-025` (affine
  handles admit one holder).
- Self-deadlock behavior: runtime detection in every build with the
  actionable panic (decision 5), plus the driver-mode boundary and the
  FFI opt-out documented in `rt_remote_task_deadlock.c`.
- No dispatcher blocks on channel capacity; parked bodies carry cancel
  and teardown wake paths (rows 4-6); QUEUE_FULL is bounded per attempt
  with control-lane progress and `credit_stalls` structurally zero
  (`RV2-DEBT-031`).
- Diagnostics precision shipped for the whole family: FUT7019
  (sync context, with the make-it-async fix), FUT7020 (payload/capture,
  with capture names, exact nested field paths, and the anchored unwrap
  hint); generic backend codes survive only on genuinely backend-blocked
  rows; `unsupported_fallback_attempts` asserted zero across rows.
- Bench row (2000 iters, reference host): on-ch-pair 72491/9166/8970
  pairs/sec at 1/2/8 shards — ~6.9/54.6/55.8 us per anchored block,
  within noise of the plain immediate-on block; the registry pin adds no
  measurable cost. Baselines reproduced within noise on the same run.

Vertical-1 boundaries carried as named debt: general anchored bodies
(`RV2-DEBT-030`, async transform behind an opaque artifact seam), union
reply payloads (in-body unwrap is the pattern until a by-value wire
format), copyable far handles (`RV2-DEBT-025`, blocks concurrent
source-level park-retry and multi-producer rows), credit flow control
(`RV2-DEBT-031`). Sentrux at closeout (committed tree): root `6182`
(`min_equality` `0.4484`, RV2-DEBT-029), `internal` `6506`, `runtime`
`5304`, `runtime/native` `5394` — the small native drift joins the
RV2-DEBT-028 recovery scope.


**Status:** design accepted (2026-07-10); ready for task slicing. Boundary
decisions were validated by an independent external review (Codex via the
second-opinion pass); per-decision outcomes are recorded inline. No task may
start before Task 1 re-pins the starting state with `file:line` evidence.

## Why This Epic Exists

Epic 13 delivered the transport spine and three executable placement task
crossings. The next user-visible Phase 4 surface is operating on a channel
owned by another shard: `far Channel<T>` handles parse and type-check since
Epic 11, but every anchored operation (`on ch { ch.send(...) }`) still stops
at FUT7014. This epic makes the owner-anchored channel crossing executable on
the LLVM/native backend, on the existing spine — no parallel wake or message
path, per the Epic 13 handoff contract.

## Starting State

- Transport spine: bounded two-lane per-shard inbound queues, PARKED-wake
  protocol, `QUEUE_FULL` + drain-retry discipline, trace counters
  (`rt_transport.h`, gate `runtime-v2-transport-check`).
- Executable crossing machinery: the immediate execute/reply category ships a
  compiled body (poll function id + copyable captured state) to a destination
  shard, runs it as a task, and answers exactly once with `TaskResult<T>`
  through the listed-pending + reply-wait + owner-done discipline
  (`rt_immediate_on.c`, `rt_remote_task_pending.c`, `rt_remote_task_wait.c`).
- Cancellation inheritance: a cancelled caller resumes exactly once through
  the cancel path, one token-validated cancel is routed to the destination,
  and the orphaned reply edge is consumed autonomously.
- Language surface (Epic 11, accepted and guarded): `on ch { ... }` anchored
  crossing — the far handle is both destination and the only permitted
  operation target inside the block (`SEM3150` unanchored, `SEM3194` outside
  `on`, `SEM3153` nested); the block evaluates to `TaskResult<T>`. Sema
  records `CrossingLoweringOnFarHandle` with destination anchor + RemoteOps.
- Payload constraint: executable captures and `ret` payloads remain
  plain-data/copyable until the allocator-owner/remote-free epic
  (`crossingRecordExecutable`).
- Local channel machinery on the owner shard (bounded channels,
  suspend/wake waiters) is Phase 1-3 code with its own gates.
- Benchmark baseline: multi-shard round trip 57-171us; immediate-on
  ~2.5-3x cheaper than spawn+await (`scripts/bench_crossing.py`).

## Boundary Decisions

1. **Lowering shape — body-shipping (review: endorsed).** `on ch { ... }`
   lowers to the existing immediate execute/reply category: resolve the
   anchor to its owner shard, ship the body once, run it as an owner-shard
   task where the anchored channel operations are ordinary LOCAL channel
   operations, reply once with `TaskResult<T>`. Backpressure on a full/empty
   channel suspends the BODY TASK on the owner's local waiter machinery —
   never the owner's transport dispatcher. Framing correction from review,
   binding: a block is **ordered, not atomic** — operations inside one block
   run in order on the owner, but if a later operation blocks, observes
   close, or loses a cancel race, EARLIER sends in the same block are
   already visible; there is no rollback, and no two-phase pattern may be
   built on blocks. Per-operation message kinds (send-request/ack,
   recv-request/reply) are NOT in this epic; a later single-op fast path is
   admissible only with an identical-behavior matrix against the block
   lowering (same linearization, cancel, close, and error outcomes) — if it
   diverges it is a new semantics, not an optimization. This supersedes the
   older Phase 4 wording "explicit messages for cross-shard channel
   operations" for the anchored form; `docs/RUNTIME_V2.md` is updated by
   this epic to record the supersession.
2. **Ordering contract (review: strengthened).** Three tiers, all named in
   the contract:
   - per-caller-task: sequential `on ch` blocks of one task are
     happens-before ordered (each block is a suspend point);
   - per-channel: operations linearize at ONE owner-side point — the local
     channel operation order on the owner shard. The epic PROMISES
     owner-side FIFO (a channel behaves at least like a local channel at
     its owner); Task 1 defines the exact linearization point in code terms
     and a test pins it. Silence here would invite users to assume
     weaker-than-local semantics;
   - cross-producer: arrival order of blocks from different tasks/shards and
     interleaving with owner-local sends are explicitly UNORDERED and
     fairness is a non-guarantee; a negative observation documents it.
3. **Handle model (review: endorsed + hardened).** `far Channel<T>` handles
   stay affine as values (move-only) but are multi-use: each `on ch` block
   borrows the handle and does not consume it. The borrow must remain live
   until the block's reply edge is TERMINAL (not merely until the request is
   built), so close/drop/move cannot race an in-flight crossing. A
   generation token validates against owner teardown and is DISTINCT from
   the ordinary channel-closed state: dead-owner (stale token) and
   live-but-closed are different, distinguishable outcomes. Multi-producer
   fan-in (copyable far handles) stays out of scope (`RV2-DEBT-025`).
4. **Close, teardown, cancel (review: endorsed + two additions).** A channel
   closed mid-body surfaces the ordinary local closed-channel outcome inside
   the body and returns through the single reply. Additions from review,
   binding: (a) owner-side close must actively WAKE a body parked in local
   send/recv and hand it the closed-channel result — a parked body may not
   sit; (b) owner/shard teardown must TERMINATE every in-flight request with
   the deterministic stale-owner error — a vanished reply that leaves the
   caller parked forever is a bug class, not an acceptable outcome. Caller
   cancellation reuses the Epic 13 routed-cancel + orphaned-reply
   consumption, plus one channel-specific step: cancel unregisters the
   body's local channel waiter before the body can be resumed, so a late
   channel wake cannot resurrect a cancelled body.
5. **Self-deadlock (review: elevated to a named decision).** The caller task
   is suspended on its one reply; if the body's progress depends on that
   same caller (simplest shape: `on ch { ch.send(v); }` where `ch` is full
   and its only consumer is the initiating caller), the system deadlocks.
   The contract must define the chosen behavior — deterministic error,
   documented hang, or detection — and Task 1 decides it BEFORE
   implementation; a deterministic reproducer row proves whichever behavior
   is chosen. This dependency shape is forbidden/defined explicitly, never
   left implicit. The diagnostics contract (decision 8) biases this choice:
   a silent documented hang is the LAST resort — prefer detection with an
   actionable runtime diagnostic (naming the channel, the parked operation,
   and the wait cycle) at least in debug builds, since the shape is
   statically undecidable (consumer topology is dynamic) but dynamically
   observable.
6. **Credits deferral (review: endorsed with obligations).** The
   credit-return protocol stays deferred, recorded as NAMED DEBT with
   measured justification — the epic does NOT claim the Phase 4 credit
   contract is satisfied. "One outstanding request per suspended caller"
   bounds per-task concurrency, not total inbound pressure: many small
   blocks from many tasks can saturate the inbound queue. Deferral is
   admissible only with flow-control obligations proven in this epic:
   bounded retry behavior under `QUEUE_FULL` stress, control-lane progress
   (cancel/close/reply never starved by data-lane retries), and
   queue-pressure telemetry; `credit_stalls` stays instrumented.
7. **Payloads (review: endorsed + ABI caution).** Copyable-only payloads are
   inherited, not relitigated: channels of `int`/plain structs prove the
   category. The restriction is enforced at the type boundary INCLUDING
   nested struct fields and any indirectly heap-containing value; a capture
   must not smuggle another far handle, a heap-owned value, or a stale
   token. This epic must not accidentally establish a cross-shard
   string/array ABI or ownership convention that the allocator epic would
   have to unwind.
8. **Diagnostics contract — kindness-first.** The compiler diagnoses at the
   earliest stage that can NAME THE REAL CAUSE, with a fix hint; the generic
   backend-unavailable message is reserved for the one case it is true
   (a backend without transport capability). Every failure this epic
   introduces is classified into exactly one tier:
   - sema-diagnosable (emit a precise SEM code at semantic analysis):
     a crossing in a synchronous context ("this crossing suspends; make the
     enclosing function `async`" — sema owns `SuspendCapable`, the backend
     is not the cause); a non-crossable payload or capture ("field `x.y`
     owns heap memory and cannot cross shards yet" — name the exact nested
     field and type, not just the struct); anchor-handle misuse (already
     SEM3194/3150/3153 from Epic 11; any hole found gets a row here, not a
     runtime check);
   - compile-time-but-backend-dependent (keep FUT700x): the same construct
     on VM/unknown backends — the backend genuinely is the cause;
   - runtime-only (deterministic error with an actionable message plus a
     counter): stale tokens, owner teardown, closed channels — and, per
     decision 5, detected wait cycles in debug builds.
   Because the sync-context and payload causes are shared with the Epic 13
   forms (which today all report the generic backend message), the precision
   split lands as a dedicated task slice in this epic and upgrades the whole
   crossing family at once. This classification is the template for every
   future feature: if sema CAN know the cause, sema says it.
9. **Handle genesis (Task 1 resolution; external review recorded).** No
   producer of a `far Channel<T>` value exists before this epic (Epic 11:
   parameters only). Committed: `channel_on(dst, cap) -> far Channel<T>` is
   the headline user-facing producer, shipped in this epic as thin sugar
   over the sanctioned primitive — a crossing body may export a
   FRESHLY-CREATED `Channel<T>` (freshness/escape-checked), which lowering
   mints into `far Channel<T>` via an owner-side channel registry. The
   typing rule is nominal and narrow: a channel capability transfer, not a
   general `T -> far T` return coercion (no other type acquires `far` by
   return). Handle tokens come from ONE shared allocator/validator carrying
   `owner_shard + kind + id + generation` (kind separates task-lease from
   channel-handle, killing cross-registry aliasing), while the task and
   channel LIFETIME models stay independent (one-shot result-transfer
   leases vs live explicitly-closed object records; refcounts and teardown
   uncoupled; teardown order: stop new crossings -> drain in-flight ->
   invalidate generations -> reclaim). The genesis contract must state what
   retains the owner-side endpoint at mint time — the supported idiom is
   body-creates-channel + spawns a local owner-side consumer + returns the
   far producer handle; the no-counterparty shape is the genesis-time face
   of self-deadlock and gets its own reproducer against the decision-5
   detection. Genesis-slice amendment (2026-07-10): the fresh-channel-return SYNTAX
   exercised the slice's stop condition (freshness needs whole-function
   dataflow; the kind/bits reply path cannot carry a token) — `channel_on`
   is the shipped producer implementing the primitive's semantics, and
   `ret <fresh channel>` returns to design review with a dataflow plan
   before any later task picks it up. Full record: `14-tasks/01-kickoff.md`
   and `14-tasks/02-handle-genesis.md`.
10. **Out of scope.** Remote `select`, distributed scopes, migration, `pool`
   execution, VM transport, multi-producer handles, the per-op message fast
   path (admissible later only per decision 1).

## Race And Failure Matrix (contract-level; every row test-owned before the flip)

| Row | Expected |
| --- | --- |
| success round trip (send, recv, close) | `TaskResult` value per local semantics |
| full-channel send / empty-channel recv | body suspends locally; dispatcher stays live; reply arrives after capacity/wake |
| close-vs-send / close-vs-recv | one linearization point; either pre-close success or closed outcome; never success-after-close |
| owner teardown mid-flight | deterministic stale-owner error to every suspended caller; no vanished replies |
| caller cancel vs completion | exactly-one reply-edge consumption; late cancel neither suppresses nor duplicates a completed result |
| stale-waiter resurrection | a late channel wake cannot re-park or resume a cancelled/closed body |
| self-deadlock shape | the decision-5 behavior, deterministically reproduced |
| no-counterparty mint (only handle exported, no owner-side consumer) | genesis-contract outcome; reproducer against the decision-5 detection |
| generation wrap/reuse | stale handles fail deterministically across owner restart and channel-slot reuse |
| leak audit under stress | every request ends in exactly one terminal result or one accounted orphaned reply; zero leaked reply edges, waiters, tasks, generation entries |

## Planned Task Slices (expand per-task at execution time)

1. Kickoff: `file:line` re-pin; resolve decision 5 (self-deadlock behavior);
   define the owner-side linearization point; fix the anchored op set for
   the first vertical (send, recv, close) and each op's in-body result
   shape.
1.5. Handle genesis (every e2e depends on it): shared typed handle-token
   mechanism; the fresh-channel-return primitive with its freshness/escape
   check; owner-side channel registry + teardown ordering; the
   local-counterparty contract with its reproducer; one source-level
   create/send/recv e2e; the `channel_on` API shape as sugar.
2. Test-first behavior rows (`SURGE_SHARDS=1,2,8`): the full race/failure
   matrix above plus trace equivalence (one execute request + one reply per
   block; fallback tripwire zero) and the dispatcher-liveness row.
3. Runtime: anchor resolution + generation validation on the execute path;
   channel-handle lease/teardown wiring; close-wakes-parked-bodies; cancel
   unregisters local waiters.
4. Lowering: `CrossingLoweringOnFarHandle` through the immediate execute
   path with the anchor as destination; capability flip + guard-matrix
   update (two-stage guard keeps sync/imported/VM/unknown rows
   deterministic).
5. Negative matrix + hidden-fallback audit extension + compile-time payload
   negatives (nested heap-containing structs, far-handle captures,
   concurrent borrows).
5b. Diagnostics precision pass (decision 8) over the whole crossing family:
   split the sync-context cause and the payload cause out of the generic
   backend-unavailable message into sema-stage diagnostics with fix hints
   and exact field paths; the guard-matrix tests re-pin the new codes; the
   generic message survives only on genuinely backend-blocked rows.
   Golden-fixture churn is expected and reviewed comment/diag-only.
6. `QUEUE_FULL` stress row (bounded retry, control-lane progress,
   telemetry), bench row against the Epic 13 baseline, gate wiring,
   deferred-credit debt row, closeout.

## Acceptance Criteria (draft)

- Anchored `on ch` send/recv/close execute on LLVM across
  `SURGE_SHARDS=1,2,8`, including owner==caller self-crossing rows.
- The race and failure matrix is fully test-owned and green twice.
- Owner-side FIFO pinned by a test; cross-producer non-guarantees
  documented with a negative observation.
- The self-deadlock reproducer exists and shows the decided behavior.
- No dispatcher ever blocks on channel capacity; every parked body has both
  a cancel wake path and a teardown wake path.
- `QUEUE_FULL` stress shows bounded retry and control-lane progress;
  `credit_stalls` instrumented; the credit deferral is a named debt row.
- All unsupported forms keep their diagnostics; compile-only paths stay
  clean; `unsupported_fallback_attempts` stays zero.
- Diagnostics precision: a synchronous-context crossing and a heap-owning
  payload each produce their own sema-stage diagnostic naming the real
  cause (context / exact field), with the generic backend message left only
  on backend-blocked rows; if decision 5 chose detection, the self-deadlock
  reproducer shows the actionable runtime diagnostic.
- Gates: extended transport gate green twice; crossing-check twice; bench
  row recorded.

## Stop Conditions

- The self-deadlock analysis shows the single reply edge + local suspension
  cannot express the decided behavior without a new protocol — stop; that
  is select/credits territory, not a patch.
- Owner-side FIFO cannot be provided at one linearization point without
  restructuring local channels — stop and re-open the ordering contract.
- Anchored ops require walking owner state from the caller side — stop;
  everything routes through messages.
- The `QUEUE_FULL` stress row shows unbounded retries or control-lane
  starvation — stop; credits move INTO this epic instead of the deferral.
- Any capture-boundary hole admits a heap-owned or far-handle payload —
  stop; that is the allocator epic's contract, not a patch site.
