# Runtime V2 Working Notes

This is the live handoff log for Runtime V2 work. Keep it current during each
task, then move durable decisions into the owning epic document before closeout.

## Epic 13 Prep (2026-07-09)

Prepared proposed Epic 13:
`13-phase4-transport-spine-and-placement-task-lowering.md`.

Scope decision for review: Epic 13 should be the first executable Phase 4
vertical, not the whole Phase 4 surface. The proposed executable forms are
`spawn on shard(id)`, `spawn on distributed`, `far Task<T>.await()`,
`far Task<T>.cancel()`, and immediate `on shard(id)` / `on distributed`.
Remote channels, remote `select`, Tier 2 `pool`, migration, distributed-scope
messages, and remote-free queues stay out unless a task explicitly expands the
scope with review.

Post-Epic-12 fixes are part of the starting state:

- `c591788e` resolves imported `far T` signatures and records caller-side
  far-task await readiness/effects for imported `far Task<T>`.
- `2fce7c22` makes backend guards scan dependency-module crossing constructs.

`RV2-DEBT-024` was narrowed accordingly: direct imported `far Task` usage no
longer blocks Phase 4 lowering, but higher-order/function-type and exported
hidden-crossing effects remain an Epic 13 Task 1 decision point or later
effect-system work.

## Epic 13 Task 4 Implementation (2026-07-09)

Task 4 replaces the Task 3 transport stub with a native inbound spine only.
`rt_shard` now embeds `rt_transport_state` by value; `rt_runtime.c`
initializes and destroys it. `rt_transport.c/.h` implement a shard-locked
bounded data queue plus a separate reserved control queue, seq-cst transport
PARKED/RUNNING state, control-before-data drain, transport wake/elision/shutdown
counters, pipe-backed `rt_transport_wake` drain/write accounting, shutdown
wake-all, and a reply-wait task-suspend seam.

Boundary: the transport pipe is not wired into `rt_net_poll_pass`. Current
worker idle sleep is on shard `worker_cv`, so Task 4 delivers correctness wake
through the existing shard `wake_pending`/`worker_cv` token path and keeps the
pipe as an OS-neutral abstraction/counter/drain surface for later pollset work.
The queue is shard-locked in this first spine; producer entry into the
PARKED->recheck window is serialized by the target shard lock, so the
sync-point row proves the recheck shape and the worker row proves the actual
condvar wake path.

Scope deliberately excluded placement ABI, remote publication protocol,
compiler lowering, new syntax, payload ownership semantics beyond shallow
message copies, and credit accounting beyond the declared control category.

Focused evidence: `go test -tags runtime_v2_transport_spine ./internal/vm -run
'^TestRuntimeV2TransportSpineAcceptanceRows$' -count=1 -v --timeout 120s`
passed. Positive rows cover shard-locked recheck shape with sync-point reach,
RUNNING wake elision, real `worker_cv` wake/drain for a transport-PARKED shard,
parked-with-inbound invariant, shutdown wake, and reply-wait task-suspend seam.
Negative controls
`RT_TRANSPORT_NEG_SKIP_RECHECK`, `RT_TRANSPORT_NEG_RELAXED_PARK_ORDER`,
`RT_TRANSPORT_NEG_SKIP_PARKED_WAKE`, `RT_TRANSPORT_NEG_WRITE_RUNNING_WAKE`,
`RT_TRANSPORT_NEG_SHUTDOWN_NO_WAKE`, and
`RT_TRANSPORT_NEG_REPLY_WAIT_PARKS_SHARD` fail deterministically.

## Epic 13 Task 3 Implementation (2026-07-09)

Task 3 adds the transport contract test seam only. New files
`runtime/native/rt_transport.h` and `runtime/native/rt_transport.c` expose the
C-only transport API, message categories, pending debug snapshot, and separate
transport wake counters, but deliberately do not implement an inbound queue,
transport wake fd, or production spine.

The Task 4 sync-point names are now in `rt_sync_point.h/.c` and allowlisted by
`check_sync_points.sh` for `rt_transport.c` only:
`SP_TRANSPORT_AFTER_DRAIN_BEFORE_PARK`,
`SP_TRANSPORT_AFTER_PARK_BEFORE_RECHECK`,
`SP_TRANSPORT_AFTER_PUBLISH_BEFORE_STATE_LOAD`,
`SP_TRANSPORT_AFTER_STATE_LOAD_BEFORE_WAKE`,
`SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND`, and
`SP_TRANSPORT_SHUTDOWN_BEFORE_WAKE`.

Task 3 introduced `make runtime-v2-transport-contract-check` for the passing
`runtime_v2_pending` static/pending-shape rows, intentionally leaving it
outside `runtime-v2-check` until the production spine existed. Task 4 upgrades
the target into the transport gate: it runs the static/behavior rows and the
`runtime_v2_transport_spine` C acceptance rows, and `runtime-v2-check` calls it
directly.

The acceptance rows enumerate lost-wake seq-cst proof plus negative controls,
wake elision positives/negatives, PARKED-with-inbound-work positive/negative,
shutdown wake positive/negative, and reply-wait task-suspend
positive/negative.

## Epic 12 Task 4 Implementation (2026-07-08)

Task 4 is complete for controlled compile-time usage fixtures/probes. Scope
stayed internal: no public examples, runtime transport, HIR/MIR crossing nodes,
new syntax, or stdlib public API changes.

What changed:

- Added `_`-prefixed integration fixtures under
  `testdata/golden/crossing/integration/{valid,invalid}` only. They remain out
  of `make golden-update`/shell golden sidecars.
- Added `internal/crossinggate/integration_test.go` with
  `TestEpic12IntegrationFixtures`, asserting Task 3 `CrossingLowering` records
  via `driver.Diagnose` and backend-unavailable diagnostics through both VM and
  LLVM backend selections.
- Positive coverage now combines valid surfaces only: separate `on` +
  `spawn on`, `spawn on` followed by valid `far Task.await`, far-task
  await/cancel records plus direct-call crossing inference, generic crossing
  source sites with multiple instantiations, and stdlib-facing `far Channel<T>`
  anchoring.
- Invalid probes pin current boundaries: nested crossing remains `SEM3153`;
  remote `TcpConn` I/O remains `SEM3151`; owned `TcpListener` capture remains
  `SEM3167`; rejected sources produce no accepted `CrossingLowering` records.

Design finding: Task 4's planned `spawn on` body containing a nested `on` is
not valid under the Epic 11 nested-crossing invariant. The task records this as
an invalid probe instead of changing syntax or sema.

## Epic 12 Task 5 Disposition (2026-07-08)

Task 5 was closed as not promoted to implementation. Epic 12 crossing backend
rows use `buildpipeline.Compile` through `internal/crossinggate`; they do not
call `internal/vm` artifact helpers or create `target/debug/.tests` files.

Debt disposition:

- `RV2-DEBT-011` remains open under **Backend/Test Matrix Cleanup**.
- `RV2-DEBT-018` remains open with `RV2-DEBT-011`; Epic 12 did not reproduce it
  and did not exercise the suspected artifact lifecycle path.
- `RV2-DEBT-001` / `RV2-DEBT-002` stay under **Backend/Test Matrix Cleanup** as
  established by Task 1.

## Epic 12 Closeout (2026-07-08)

Epic 12 is complete. The accepted shape is **guard-before-HIR**: crossing
source is accepted through parser/sema, represented for future lowering by
`sema.Result.CrossingLowering`, and stopped at executable backends with
deterministic `FUT7014`-`FUT7017` diagnostics until Phase 4 transport exists.

What landed:

- `runtime-v2-crossing-check` in `Makefile`, wired into `runtime-v2-check`.
- Internal Task 4 integration fixtures under
  `testdata/golden/crossing/integration/{valid,invalid}`.
- Compiler-scoped Sentrux rules in `internal/.sentrux/rules.toml`.
- Final docs in the Epic 12 document and Task 6 closeout, including the Phase
  4 handoff.

Final evidence:

- `make runtime-v2-crossing-check` passed twice consecutively.
- `make golden-check` passed.
- `make check` passed.
- `./check_file_sizes.sh -a` passed: 745 files, 0 over hard limit.
- `sentrux check .` passed, quality `6189`.
- `sentrux check internal` passed, quality `6532`.

Debt disposition:

- `RV2-DEBT-001`, `RV2-DEBT-002`, `RV2-DEBT-011`, `RV2-DEBT-017`, and
  `RV2-DEBT-018` remain open under non-Epic-12 owners.
- `RV2-DEBT-024` remains deferred to Phase 4 transport lowering or a later
  effect-system epic. Task 3 proved imported function effect bits are not
  required for the current readiness contract.

Next epic should be real Phase 4 transport/lowering. Start from
`internal/sema/crossing_lowering.go` and the Phase 4 handoff section in
`12-tasks/06-ci-gates-and-closeout.md`.

## Epic 11 Prep Pass (2026-07-07)

Documentation-only prep pass over the Epic 11 crossing-surface documents
(`11-explicit-crossing-language-surface.md` and `11-tasks/block-01..04`), plus a
new `11-tasks/README.md`. No compiler, runtime, or stdlib code was changed; the
stdlib/prelude edits are recorded as Block 4 implementation tasks, not performed.
Decision source: the repo-local Epic 11 contract document, block matrices,
fixture inventory, and this notes file. No external agent memory is required to
reconstruct or execute the Epic 11 language-surface plan.

What changed:

- `far` handles are affine (move-only); `far Task<T>` strictly affine, with
  `.await()`/`.cancel()` consuming the handle. Copyable handles postponed.
- Remote-handle-capable defined once: intrinsic `Channel<T>`, intrinsic
  `Task<T>`, and `@shard_pinned` types. Arrays are not capable: `far T[]` parses
  but sema-rejects as postponed.
- Keyword strategy recorded: `far` hard reserved; `on` and `crosses` contextual
  (back-compat positives `let on = 1;` / `let crosses = 1;`).
- Epic 11 execution scope recorded: surface plus lowering guards only; crossing
  execution emits a deterministic backend-unavailable diagnostic (`on` and
  `spawn on`); all positive golden fixtures are compile-only.
- Diagnostic dedup: Block 4 owns the crosses-missing and capture families;
  Blocks 2/3 reference them. All Block 3 placeholders converted to kebab-case.
  Reuse-first allocation policy annotated (per lead + infra map): e.g.
  `SemaUseAfterMove` 3130, `SemaTaskNotAwaited` 3107, `SemaSpawnNotTask` 3111,
  `SemaTypeMismatch` 3015, `SemaBorrowConflict` 3018, `SemaRawPointerNotAllowed`
  3129, `SynModifierNotAllowed` 2015; postponed surfaces to `FUT` 7xxx.
- Block 3 S06 (`spawn distributed { ... }`) re-specified to the existing local
  `spawn` grammar path (`SemaSpawnNotTask` 3111), not a new missing-`on` code.
  Block 3 B07 re-specified as a local-`Task<T>` capture violation, not an
  illegal-await. `far Task<T>` lifecycle rows added (drop / double-await /
  await-after-cancel).
- `far TcpConn` control-op list closed to `{ close() }` (removed `cancel_read`
  row/fixture). Placement recorded as a `Copy`, shard-movable intrinsic with
  capture rows. `compare on` and statement-position `on` positives added.
  Drop-site contract (own `@shard_movable` dropped on destination if not
  returned) and out-of-range `shard(id)` runtime contract recorded.
- Draft 9 documentation scope recorded in the epic Documentation Contract.
- New debt: RV2-DEBT-024 (`crosses` effect-polymorphism), RV2-DEBT-025
  (`@far_copy` opt-in), RV2-DEBT-026 (`far` arrays), all postponed.
- Diagnostic codes are reserved in `internal/diag/codes_crossing.go`, and the
  staged fixture matrix lives under `testdata/golden/crossing/block0{1..4}`.
- The current `internal/crossinggate` harness is sema-stage only.
  Backend-unavailable rows (`FUT7014`..`FUT7017`) need a separate
  lowering/backend gate or explicit stage/config selector before Block 2/3 full
  gates are enabled.

Historical next-step note at task staging time: start with Block 1 `far`, then
Blocks 2/3, then Block 4 contracts and documentation closeout. The earlier
"Block 4 grammar slice" wording was superseded when the explicit `crosses`
keyword was retired.

## Epic 11 Block 1 Implementation (2026-07-07)

Block 1 (`far T`) implementation started and its crossing gate is enabled.
Current proof:

- `far` is a hard keyword and parses only as a type modifier, plus the narrow
  `is`/`heir` type-operand form needed for `x is far TcpConn`.
- Sema interns `far T` as a distinct `types.KindFar`, preserving aliases,
  generic identity (`Channel<far T>` vs `far Channel<T>`), type labels, and
  debug/LSP/HIR/MIR/mono rendering.
- Invalid forms reject before lowering: nested `far`, remote ownership/borrow,
  raw pointers, `far extern<T>`, remote/local arrays of `far`, non-capability
  bases, and local operations on `far T`.
- `types.KindFar` is intentionally move-only by default (`Interner.IsCopy`
  returns false) and has pointer-sized handle layout/LLVM type.
- Temporary capability bridge for Block 1: intrinsic `Channel<T>`, intrinsic
  `Task<T>`, `@shard_pinned`, and current intrinsic `TcpConn` are accepted as
  remote-handle-capable. The explicit `@shard_pinned` stdlib marker remains a
  Block 4 task; this bridge prevents Block 1 from depending on Block 4.

Fixture adjustments made during implementation:

- `far_positive_is_type_operand.sg` added for `FAR-ID-010`; reserved
  `extern<far> {}` coverage added for `FAR-LEX-NEG-004`.
- `Channel<far Message>` and `Task<far int>` identity fixtures now use capable
  `far TcpConn` payloads so they prove identity mismatches instead of
  non-capability rejection.
- Mutable-handle fixture uses `let mut` because function-parameter `mut` is not
  part of the current parser grammar.
- Local field-access negative uses `far TcpConn.__opaque` so it proves
  `SEM3142` rather than failing first as a plain non-capability struct.
- Block 1 fixtures are no longer `_`-prefixed: they now participate in
  `make golden-check`, with committed `.diag`, `.tokens`, `.ast`, and `.fmt`
  sidecars generated by `make golden-update`.

## Epic 11 Block 2 Implementation (2026-07-07)

Scope of this slice: Block 2 (`on dst { ... }` placement crossing) plus the
Block 4 grammar prerequisites it depends on (`crosses` effect parsing,
`@shard_movable` / `@shard_pinned` type attributes) and the `Placement`
intrinsics. `Block2Enabled` is now `true`; `Block3Enabled` and `Block4Enabled`
stay `false`.

Proof:

- `go test ./internal/crossinggate/`: Block 1 and Block 2 pass, Blocks 3/4 skip.
  Block 2 is 47/47 subtests (20 positives error-free at sema; 26 negatives emit
  their mapped code at sema; 1 backend-unavailable negative emits `FUT7014` at
  the backend stage).
- `make golden-check` green with the committed block02 sidecars.

Implementation surface:

- Parser (parser-dev): `on` crossing expression (`ExprOn`), contextual `on`
  recognition at expression heads (now including literal-headed destinations so
  `on 1 { ... }` reaches sema), struct-literal-suppressed destination parsing
  (so `on Job { ... }` no longer eats the body brace), `crosses` effect,
  `@shard_movable`/`@shard_pinned` type attributes.
- Sema (sema-dev): `on` destination/result/capture/anchor/crosses checks; the
  `Placement`/`ShardId`/`pool`/`distributed`/`shard` intrinsics in
  `core/intrinsics.sg`.
- Backend guard (sema-dev): `internal/buildpipeline/on_crossing_check.go` emits
  `FUT7014` (unconditional across backends, deduped one-per-crossing-span) when
  an `on` crossing reaches lowering, mirroring `addBlockingVMErrors`/`FUT7008`.
- Harness (integration): `internal/crossinggate/crossing_gate_test.go` gained an
  `// EXPECT-STAGE:` selector. Default is the sema stage; `EXPECT-STAGE: backend`
  routes a fixture through `buildpipeline.Compile` with `BackendVM` and asserts
  the code on the resulting bag. This keeps the positive compile-only `on pool`
  fixtures clean at sema while the source-identical backend-unavailable negative
  asserts `FUT7014` at the backend stage.

Fixture activation and adjustments (recorded per the drift rule):

- 46 of 47 block02 fixtures were deprefixed and now carry committed
  `.diag/.tokens/.ast/.fmt` sidecars. `EXPECT-DIAG` headers are retained (they
  are read by the harness; they coexist with the sidecars, exactly as Block 1).
- Documented exception: `_on_negative_backend_unavailable.sg` stays `_`-prefixed
  and carries `// EXPECT-STAGE: backend`. Its diagnostic is emitted only at the
  backend stage, so `surge diag` (sema) produces an empty `.diag`, which
  `scripts/golden_update.sh` rejects for `invalid/` fixtures. Keeping it
  `_`-prefixed excludes it from the shell golden corpus (no sidecars) while the
  `crossinggate` go-test still exercises it (its `invalid/*.sg` glob matches
  `_`-prefixed names; the gate is the `Block2Enabled` constant, not the prefix).
  Block 3 must repeat this pattern for its `FUT7015`–`FUT7017` fixtures.
- `on_negative_mut_borrow_capture.sg` intentionally uses a local `let mut req`
  binding rather than a `mut` parameter, to yield a clean single-`SEM3165`
  golden. This is a fixture-shape choice, not scope drift: the row tests the
  `&mut`-capture-into-`on` invariant, not `mut`-parameter syntax. `mut`
  parameters are out of Epic 11 scope; a briefly-added parser implementation was
  reverted (see the documented-but-unimplemented note below).
- Documented-but-unimplemented (separate ticket): `docs/LANGUAGE.md:2104`
  documents `mut` parameters as valid Surge, but the compiler rejects them with
  `SYN2102` ("expected identifier, got \"mut\""). Epic 11 does not implement
  them; the parse+format support drafted during this block was reverted because
  it lacked sema mutability semantics.
- Contextual-`on` boundary: `atOnCrossingHead` recognizes a crossing when `on` is
  immediately followed by an identifier, a literal
  (int/uint/float/string/fstring/true/false/nothing), or `blocking`. It
  deliberately does NOT treat `(`, `[`, `.`, or operators as crossing heads, so
  `on(x)`, `on[i]`, `on.f`, `on + 1`, `on;`, `on = 1`, and bare `on` stay ordinary
  identifier uses. Consequence: a parenthesized destination written directly
  after `on` (`on (expr) { ... }`) is not recognized as a crossing; bind it to a
  variable first (`let p = expr; on p { ... }`). No block-02 fixture needs one.
- Drift: `@shard_pinned` was added to `TcpConn` in `core/intrinsics.sg` so
  `own TcpConn` capture across a crossing rejects with `SEM3167` (ON-CAP-N004).
  Semantically correct (a socket is shard-pinned) but a stdlib change beyond the
  literal Block 2 recipe; recorded here.
- Golden noise (accepted): several negatives emit incidental cascade codes
  alongside the mapped code (e.g. `SEM3051` missing-return; `tcpconn_read/write`
  also emit `SEM3165` for the borrowed buffer arg; `shard_pinned` also `SEM3005`
  on `conn.close()`). The presence-gate passes; the sidecars record the full
  output.
- Global ID renumbering (expected, no structural change): adding the `Placement`
  and `ShardId` intrinsics/types shifted the global monotonic symbol, type, and
  local ID counters, so 46 unrelated `testdata/golden/{hir,hir_borrow,mir,mono}`
  goldens regenerated with new `sym=`/`type#`/`L`/`S` numbers only. Diff is pure
  renumbering (verified: zero residual difference after normalizing IDs). Any
  future stdlib-symbol addition triggers the same regeneration.

Out-of-scope latent bug surfaced (separate ticket, untouched in Block 2):
`if Foo { ... }` / `while Foo { ... }` share the same struct-literal-eats-block
parse ambiguity that the `on Job { ... }` destination fix resolves for the
crossing position only.

## Epic 8 Closeout (Consolidated 2026-07-06)

Epic 8 (Task Lifecycle Lane And Net Fairness) is COMPLETE at closeout commit on
`af0416fc` (Task 13). Per Global Rule 10, the durable Epic 8 decisions now live
in their permanent homes; the per-task handoff blocks below are kept as history
but are no longer the source of truth:

- Durable architecture result → `docs/RUNTIME_V2.md` Phase 3 (task lifecycle +
  same-owner scope bookkeeping on owner lanes; segmented task table; `done_cv`/
  `compat_cv` external-only; control steady-state ~26.4→9.36/req; F2 placement
  adoption fixing the net starvation funnel).
- Contract-by-contract proof, task ledger, and next-epic handoff →
  `08-tasks/14-epic-closeout.md` (both contracts verified clause-by-clause with
  named test/commit proofs; NO unmet clauses).
- Debt final states → `DEBT.md`: RV2-DEBT-015/016/019 CLOSED; RV2-DEBT-021
  CLOSED (new deterministic cross-owner scope test); RV2-DEBT-020 carried
  (comment corrected, owner → net-handle/accept epic); RV2-DEBT-022 carried;
  RV2-DEBT-023 NEW (cancel RUNNING→WAITING lost-cancellation, found in the
  Task-8-supplemental mid-park re-derivation); RV2-DEBT-003 open with the
  Task 13 recovery clause.
- Per-task gate/evidence records → `08-evidence.md` Tasks 1-14.

MANDATORY next-epic gate (also in the epic doc + RUNTIME_V2 Phase 4): Epic 8 did
NO syntax/parser/semantic/Phase-4 work. `far`/`submit_to`/`crosses`/
shard-movable and all crossing transport remain undesigned; the next epic
touching any crossing surface MUST start with a dedicated user syntax review.

## Current State

- Runtime V2 target architecture lives in `docs/RUNTIME_V2.md`.
- Epic documents live in `docs/runtime-v2-epics/`.
- Epic 1 is complete. Its main document is
  `01-contract-rules-harness.md`.
- Task breakdown and status live in `01-contract-rules-harness-tasks.md`.
- Global working rules live in `RULES.md`.
- Tasks 1-5 were committed as `b865472a`:
  `docs(runtime): add Runtime V2 epic planning baseline`.
- Tasks 6-7 were committed as `8ae616a1`:
  `docs(runtime): define Runtime V2 liveness gates`.
- Task 10 evidence is recorded as complete with known debt for the narrow Task
  11 counter-field migration boundary. Task 11 may move or wrap
  `channel_blocked_workers`, `compensation_count`, and
  `compensation_high_water` only if it does not change direct handoff,
  `try_send`, sync helper, compensation, ready-drain, or waiter semantics.
- Task 11 implementation is recorded. Channel/blocking compatibility counters
  now live under `rt_shard.channel_blocking_compat`; main-session
  runtime/native `session_end` passed: `5146 -> 5172`, no violations.
- Task 12 CI wiring is recorded. `make runtime-v2-check` now runs the stable
  Runtime V2 seed with `SURGE_SKIP_TIMEOUT_TESTS=0`; the separate CI job
  installs `clang`, `llvm`, `lld`, and `binutils`, sets
  `SURGE_MT_TIMEOUT_SCALE=3`, and runs that target.
- Epic 3 Task 19 structural closeout is recorded. Epic 3 is complete for
  owner-local waiters and dependency-aware runtime refactoring under `N=1`.
  Main-session closeout gates and benchmark/smoke evidence are copied into
  `03-evidence.md`. Post-doc Sentrux closeout scans recorded root `6198`,
  runtime `5195`, and runtime/native `5159`; missing rules were debt at that
  time and are closed by the pre-Epic 4 quality hardening.
- Epic 4 is complete with accepted debt for persistent fd registry and net
  lifecycle proof. Closeout lives in
  `04-persistent-fd-registry-and-net-lifecycle.md`; task evidence lives in
  `04-evidence.md`.
- Epic 4 keeps the current `poll()` backend. `epoll`, `kqueue`, `io_uring`,
  accept distribution, `N>1`, crossing syntax, heap counters, and the broad
  VM/native/LLVM test-matrix rewrite remain out of scope.
- Epic 5 should start from heap and hot accounting ownership. Do not start
  from `N>1`, crossing syntax, or cross-shard wake protocol work.
- Epic 6 draft is recorded in
  `06-n2-accept-ownership-and-tier1-scheduler.md`. It starts from `N>1` accept
  ownership and the Tier 1 no-steal scheduler boundary under the preserved
  global executor lock. With `SURGE_SHARDS>1`, Tier 1 uses one worker per shard;
  `SURGE_THREADS` remains a `SURGE_SHARDS=1` compatibility control. Epic 6 owns
  per-shard net poller/wake ownership but not Phase 4 inbound messaging,
  eventfd, credits, or the seq-cst PARKED protocol. It must not change Surge
  syntax, parser rules, semantic checks, async lowering, standard-library
  signatures, or public examples for `far`, `submit_to`, `crosses`, or
  shard-movable markers without a dedicated user syntax review first. Epic 7
  should be the global-lock/global-primitive ownership split, not the syntax
  epic.
- Pre-Epic 4 quality hardening is recorded: Sentrux rules now exist for root,
  `runtime/`, and `runtime/native`; CLI and MCP rule checks pass for all three
  paths. `check_file_sizes.sh` now checks `go,c,h` by default, prunes generated
  dirs, and enforces `.loc-legacy-allowlist` for existing native runtime files
  over the hard gate. Durable debt is tracked in `DEBT.md`.
- Task 14 Epic 2 closeout is recorded and approved local gates passed after the
  docs edits. Epic 2 is complete for the `N=1` runtime/shard structure slice:
  no owner-local waiter, persistent fd registry, `N>1`, or crossing-syntax
  implementation is claimed. The broad VM/backend regex remains later
  test/backend debt. Main-session Task 14 Sentrux closeout scans recorded root
  `6207`, runtime `5209`, and runtime/native `5172`; missing Sentrux rules were
  debt at that time and are closed by the pre-Epic 4 quality hardening.
- Epic 3 Task 17 extracted trace and SIGUSR1 dump responsibility from
  `runtime/native/rt_async_state.c` into
  `runtime/native/rt_async_trace.c`. The refactor did not change scheduler,
  waiter, timer, channel, or net semantics. Post-refactor line counts:
  `rt_async_state.c` 1731, `rt_async_trace.c` 497,
  `rt_async_internal.h` 499, and `rt_net.c` 1024.
- Task 13 accessor cleanup is recorded as audit-only. The migrated scheduler,
  net poll scratch, channel compat, and runtime/shard skeleton surfaces are
  clean in current `runtime/native`; no runtime code change was justified.
  `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`, and
  `git diff --check` passed for the Task 13 docs-only closeout. Main-session
  Sentrux scans recorded root `6207`, runtime `5209`, and runtime/native
  `5172`; missing rules were debt at that time and are closed by the
  pre-Epic 4 quality hardening.
- Task 9 implementation evidence is recorded. Main-session Sentrux runtime/native
  `session_end` passed for this task: `5132 -> 5146`, `signal_delta=14`, no
  violations.
- Latest Task 9 checks passed: `make c-check`, `make cppcheck`, `make check`,
  focused net wake probe, native net benchmark, `git diff --check`, Sentrux
  repository scan, and Sentrux runtime scan. Both Sentrux `check_rules` calls
  reported missing rules files at that time; this is now closed by the
  pre-Epic 4 quality hardening.
- Epic 2 is complete in `02-n1-runtime-shard-structure.md` for the `N=1`
  `rt_runtime`/`rt_shard` structure slice; owner-local waiters, persistent fd
  registry, `N>1`, crossing syntax, and the VM/native/LLVM test-matrix rewrite
  are later epics.
- Epic 2 task files live in `02-tasks/`. Runtime-code tasks are paired with
  test-writing tasks where meaningful tests can be written, and the stable
  Runtime V2 liveness seed is now covered by `make runtime-v2-check` and the
  separate CI job.
- Epic 2 task evidence is recorded in `02-evidence.md`.
- Epic 3 Task 04 added pending waiter behavior contract tests in
  `internal/vm/runtime_v2_waiter_contract_test.go`. The default tag-off gate
  passes with no tests selected. The tagged
  `go test -tags runtime_v2_pending` waiter proof now passes after making the
  `print` default argument explicit in the `.sg` snippets. The earlier
  `rt_string_len_bytes` crash was reclassified as LLVM/default-argument lowering
  debt, not waiter cleanup/stale-wake evidence.
- Epic 3 Task 05 added the default-tag static boundary check
  `internal/vm/runtime_v2_waiter_static_test.go`. It compiles the current
  waiter key/list helper declarations and the legacy executor/task waiter
  storage shape with `clang -fsyntax-only`. It does not execute runtime/native
  code, does not depend on `runtime_v2_pending` behavior tests, and does
  not claim Sentrux rule compliance.
- Subagents now use a plan gate: they must return a plan for approval before
  implementation, test-writing, or review work starts. If no real plan mode is
  available, use a no-edit plan-only prompt and approve the plan explicitly.
- Epic 2 drafting checks passed: `git diff --check`, stale phase/epic wording
  grep, Sentrux repository scan, and Sentrux runtime scan. Sentrux rules are
  still missing at both scan roots and must not be reported as rule compliance.
- Epic 2 Task 1 kickoff evidence is recorded in `02-evidence.md`. It captured
  baseline commit `e7d9563d5c78a90409e4d6a92bd47d49b30ae830`, clean starting
  status on `codex/runtime-net-scheduler-refactor`, accepted VM/backend-test
  debt, root/runtime Sentrux scans, and the missing-rules deferral.
- Epic 2 Task 2 field ownership map is recorded in
  `02-field-ownership-map.md`. It classifies every current `rt_executor` field
  before runtime field movement and names the first code-task field boundary.
- Epic 2 Task 3 CI/test contract is recorded in `02-ci-test-contract.md`. It
  defines the future exact-name Runtime V2 gate and keeps the broad focused
  VM/backend debt out of required green gates.
- Epic 2 Task 4 skeleton-test proof is recorded in
  `internal/vm/runtime_v2_skeleton_static_test.go`. It uses the
  `runtime_v2_pending` build tag and intentionally fails before Task 5 because
  `rt_runtime`, `rt_shard`, the `N=1` count macro, and skeleton accessors do not
  exist yet. The check is local-only until Task 12.
- Epic 2 Task 7 scheduler-shape migration evidence is recorded in
  `02-evidence.md`. Scheduler container fields now live under
  `rt_shard.scheduler`. Current scheduler trace proof uses
  `TestMTWorkStealing` and `TestMTSeededScheduler`. `TestMTSeededScheduler`
  remains in the future CI seed; `TestMTWorkStealing` stays
  local-only/current-runtime evidence because Tier 1 stealing is not a Runtime
  V2 hot-path contract.
- Epic 2 Task 10 channel/blocking compatibility evidence is recorded in
  `02-evidence.md`. Stable direct channel subset and the CI-contract
  channel/blocking pair passed. Native channel before-benchmark passed with
  current-checkout compiler `/tmp/surge-task10.nOjRbh/surge` and wrote
  `build/benchmarks/runtime-v2-task10-native-channel-before.md`. The report is
  ignored under `build/`; selected durable rows were copied into
  `02-evidence.md`.

## Epic 5 Task 01 Handoff

- Scope completed: kickoff baseline before heap-accounting work. Docs-only; no
  runtime code, tests, `Makefile`, CI, or Sentrux rule files changed.
- Start commit: `5e04a975 docs(runtime): plan epic 5 heap accounting`; branch
  `codex/runtime-net-scheduler-refactor`; clean tree at start.
- Line counts at kickoff: `rt_alloc.c` 144, `rt_runtime.c` 184,
  `rt_async_internal.h` 495, `rt_async_state.c` 1727, and
  `internal/vm/llvm_native_heap_stats_test.go` 69.
- Sentrux MCP scans and rule checks passed for all three required paths:
  repository root `6191`, `runtime/` `5240`, and `runtime/native/` `5244`.
  Task 1 did not call `session_start` or `session_end`; runtime-code Tasks 5-7
  must start the scoped session on the path used for final delta evidence.
- Baseline gates passed: heap-stat smoke
  `go test ./internal/vm -run '^TestLLVMNative(HeapStats|BufferedChannelAllocatesSingleBlock)$'`
  passed in package time `4.817s`; `make runtime-v2-check` passed with package
  times `8.007s`, `0.031s`, `19.650s`, and `15.940s`; `git diff --check`
  printed no output.
- Accepted debt remains outside Epic 5 close conditions unless a later task
  touches the related surface: `RV2-DEBT-001` through `RV2-DEBT-007`, plus
  `RV2-DEBT-010`. Any new allocator or heap-accounting debt must be closed or
  recorded in `DEBT.md` before Epic 5 closeout.
- Gate plan for Tasks 2-7 is recorded in `05-evidence.md`. Runtime-code Tasks
  5-7 require focused heap/static tests, `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, `make check` unless a narrower gate is explicitly
  approved in task evidence, `git diff --check`, root/runtime/native Sentrux
  scans and rule checks, scoped Sentrux session delta evidence, and touched
  line counts.
- Next: Task 2 owns `05-heap-accounting-dependency-map.md` and should map
  `rt_alloc`, `rt_free`, `rt_realloc`, `rt_heap_stats`, accounting helper
  paths, consumers, and thread contexts before choosing the accounting cell
  model. The main session continues to own `05-evidence.md` and `NOTES.md`
  updates to avoid write conflicts.

## Epic 5 Task 02 Handoff

- Scope completed: docs-only heap accounting dependency map in
  `05-heap-accounting-dependency-map.md`. No runtime code, tests, `Makefile`,
  CI, or Sentrux rule files changed.
- Selected direction for Tasks 5-7: runtime/shard-owned heap-accounting state
  with lane-local write cells, explicit cold/external cell, event totals
  (`alloc/free events`, `allocated/freed bytes`), and aggregate-on-read
  `rt_heap_stats()` deriving live blocks/bytes.
- Do not implement unsigned per-cell live counters: cross-lane frees are real,
  and live totals should be derived from aggregate alloc/free event and byte
  totals or an equivalent proven model.
- `rt_alloc.c` must stay behind a narrow accounting API. It must not call
  `ensure_exec()` or depend on scheduler queues, waiter stores, fd registry
  internals, or executor initialization.
- Caller lanes to account for: cold/pre-runtime, main/synchronous runner
  (`rt_task_await -> run_until_done -> run_ready_one`), executor workers, I/O
  thread, blocking workers, and runtime-internal helpers under executor locks.
- Boundary: direct libc temporary allocations in `rt_bignum_format.c`,
  `rt_io.c`, `rt_fs.c`, and `rt_net.c` are outside the current
  `rt_heap_stats()` contract. Epic 5 moves existing heap-accounted producer
  state; it does not make every libc temporary visible.
- Review outcome: initial P1/P2/P3 findings were fixed; final focused
  re-review returned no findings. Residual risk is source-backed, not
  runtime-trace-proven, caller classification; Task 8 owns that proof.

## Epic 5 Task 03 Handoff

- Scope completed: focused heap-accounting contract tests in
  `internal/vm/runtime_v2_heap_accounting_contract_test.go`. No runtime/native,
  `Makefile`, CI, or task-doc files changed.
- Coverage added: ordinary alloc/free, realloc grow/shrink/null/zero,
  deterministic failed realloc through invalid aligned realloc (`align=24`),
  aligned alloc/realloc, and concurrent worker aggregate alloc/free accounting.
- Important test-shape detail: sequential contracts print all `HeapStats`
  snapshots first and assert deltas in Go. This avoids Surge-side assertion or
  conversion allocations contaminating later heap snapshots.
- Failed realloc coverage is intentionally not OOM-based; OOM is not stable
  enough for a required contract. If alignment validation semantics change,
  revisit this proof.
- Concurrent coverage proves post-join aggregate accounting and
  `live_blocks == alloc_count - free_count`; it does not prove exact worker
  placement or cross-lane free scheduling.
- Checks passed: focused heap regex with `-count=1`, review-subagent repeated
  focused regex with `-count=3`, `gofmt -l`, `git diff --check`, and root
  Sentrux rules in review. Review outcome: no findings.

## Epic 5 Task 04 Handoff

- Scope completed: pending static target-shape gate in
  `internal/vm/runtime_v2_heap_accounting_static_test.go`. No runtime/native,
  `Makefile`, CI, or task-doc files changed.
- The test is excluded from default gates by `//go:build runtime_v2_pending`.
  Default focused run reported no tests; the pending-tag run failed as intended.
- Static gate now requires concrete future shape: `rt_heap_accounting_cell`,
  `rt_heap_accounting`, owner field in `rt_runtime` or `rt_shard`, explicit
  cold cell/accessor, TLS/current-cell lane selection with cold fallback,
  `rt_heap_accounting_record_*` calls in `record_*` helper bodies, and
  `rt_heap_accounting_snapshot` from `rt_heap_stats()`.
- The test strips comments before matching and skips prototypes before function
  definitions. This was added after review caught false-green risks from
  comments, unused declarations, and API names appearing outside helper bodies.
- Expected-red causes before implementation: old file-scope heap atomics,
  direct `record_*` writes, missing owner/cold/lane shape, direct old global
  loads, and missing snapshot API. Tasks 5-7 own turning this gate green.
- Review outcome: initial P1/P2 findings were fixed; final focused re-review
  returned no remaining findings. Residual risk: regex/source-shape gate only,
  so behavior proof still depends on Task 3 and later probes.

## Epic 5 Task 05 Handoff

- Scope completed: runtime/shard-owned heap-accounting skeleton. `rt_alloc.c`
  and public `rt_heap_stats()` behavior are intentionally unchanged; Task 6 owns
  moving `record_alloc/free/realloc`, and Task 7 owns snapshot aggregation.
- New files: `runtime/native/rt_heap_accounting.h` and
  `runtime/native/rt_heap_accounting.c`. They define `rt_heap_accounting_cell`,
  `rt_heap_accounting`, `struct rt_heap_accounting_snapshot`, module-owned
  `cold_cell`, TLS current-cell selection, skeleton record helpers, and a
  snapshot helper.
- Cold accounting is a deliberate bootstrap exception outside `rt_runtime`, so
  future pre-runtime Task 6 events survive `rt_runtime_init_n1()` clearing
  runtime storage and can be included by Task 7 aggregation.
- `rt_shard.heap_accounting` owns runtime lane cells. Main/synchronous runner,
  worker, I/O, blocking, and compensation paths now select lane cells. Blocking
  worker context storage is owned by `rt_executor.blocking_worker_ctxs` because
  detached blocking workers need stable per-thread context addresses.
- Direct libc allocation is intentional in two places: `rt_heap_accounting.c`
  uses `calloc/free` for accounting cell arrays, and `rt_async_blocking.c` uses
  `calloc` for blocking worker contexts. This avoids recursive `rt_alloc`
  accounting and preserves Task 5 public heap-stat behavior.
- Static gate split: Task 5 skeleton subtest passes; Task 6 record-migration
  predicates and Task 7 snapshot-aggregation predicates remain present and
  explicitly skipped with owning task names. Do not delete or weaken those
  predicates when implementing Tasks 6-7.
- Checks passed after review fixes: focused static heap gate, focused heap
  contracts, `make c-check`, `make cppcheck`, sequential `make
  runtime-v2-check`, `git diff --check`, `./check_file_sizes.sh`, Sentrux
  runtime/native session `5244 -> 5250`, and root/runtime/runtime-native
  Sentrux scans/rules.
- First `make runtime-v2-check` attempt timed out once on
  `TestMTBlockingChannelHelpersAllowTimersToAdvance`, but focused reproduction
  and final sequential `make runtime-v2-check` passed. A separate missing
  `build.stdout` failure came from running overlapping VM test commands in
  parallel; this is recorded as `RV2-DEBT-011`.
- Review outcome: two P2 findings were fixed (blocking context ownership and
  static lane install-site coverage); focused re-review returned no findings.
  Remaining risk: blocking worker contexts are process-lifetime until shutdown
  owns detached thread lifecycle.

## Epic 5 Task 06 Handoff

- Scope completed: `record_alloc`, `record_free`, and `record_realloc` no
  longer write old `rt_alloc.c` global counters. The old global source of truth
  (`heap_alloc_count`, `heap_free_count`, `heap_live_blocks`,
  `heap_live_bytes`) was removed.
- Allocation events now route through `rt_heap_accounting_record_*` using
  `rt_heap_accounting_current_cell()`. Failed allocation/realloc paths still do
  not record events, `rt_free(NULL, ...)` remains a no-op, realloc-null and
  realloc-zero semantics are preserved, and `rt_array_forget_allocation` stayed
  on the same free paths.
- Boundary decision: `rt_heap_stats()` switched to
  `rt_heap_accounting_snapshot()` in Task 6, not Task 7, because removing the
  old globals requires public heap-stat tests to read the new source of truth.
  Task 7 still owns the aggregation audit, focused evidence, and documentation
  closeout.
- `rt_runtime_global_heap_accounting()` is a narrow shard0 accessor. It does
  not allocate, initialize the executor, or call scheduler internals from the
  allocator path.
- Review fix: snapshot reads are relaxed per-lane reads, not a global cut.
  `alloc_count` and `free_count` stay raw; only derived `live_blocks` and
  `live_bytes` saturate transient underflow to zero. The stale
  `RT_HEAP_ACCOUNTING_INVARIANT_VIOLATION` status was removed.
- Static heap gate now runs all Task 5/6/7 predicates and passes. That does not
  close Task 7; it means Task 7 starts from working mechanics and should prove
  and document the aggregation semantics.
- Checks passed: focused static heap gate, focused heap contracts,
  `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`,
  `git diff --check`, `./check_file_sizes.sh`, and final root/runtime/native
  Sentrux scans/rules. Final Sentrux qualities: root `6190`, runtime `5279`,
  runtime/native `5318`.
- Pre-commit refreshed `STATS.md` in the Task 6 commit to reflect the smaller
  native runtime and static-test line counts.
- Review outcome: initial P1/P2 findings were fixed; focused re-review returned
  no findings. No new debt was added. Continue running VM heap gates
  sequentially where artifact names can collide (`RV2-DEBT-011`).

## Epic 5 Task 07 Handoff

- Scope completed: heap-stats aggregation audit, focused evidence, Sentrux
  scans, and docs closeout. No runtime or heap-test files changed in Task 7.
- Current implementation: `rt_heap_stats()` snapshots
  `rt_runtime_global_heap_accounting()` through `rt_heap_accounting_snapshot()`
  before allocating `SurgeHeapStats`; the snapshot aggregates cold, main, I/O,
  worker, blocking, and compensation cells.
- Public behavior remains unchanged: `HeapStats` layout and ABI are stable,
  `rc_increments` and `rc_decrements` stay zero, and stats-result allocations
  are not included in the returned snapshot.
- Static heap gate now runs and passes all Task 5/6/7 predicates. Task 7
  confirmed the old heap globals and direct `rt_heap_stats()` global loads are
  absent.
- Checks passed: native heap-stats smoke tests, Runtime V2 heap accounting
  contracts, pending static heap gate, `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, `make check`, `git diff --check`, Sentrux
  root/runtime/native scans and rules, and a no-code runtime/native Sentrux
  session `5318 -> 5318`. Final Sentrux qualities: root `6190`, runtime
  `5279`, runtime/native `5318`.
- Residual risk is intentional: relaxed per-cell snapshot reads are not a global
  cut, so derived live totals can be transiently conservative. Task 8 owns
  broader concurrency and performance evidence. Continue running overlapping VM
  heap gates sequentially because of `RV2-DEBT-011`.

## Epic 5 Task 08 Handoff

- Scope completed: concurrency and manual performance evidence for heap
  accounting. No runtime or VM test files changed.
- Added `scripts/bench_native_heap_accounting.sh` as a manual benchmark script.
  It generates a temporary Surge fixture, validates numeric/probe env overrides,
  owns per-probe timeouts, writes reports under ignored `build/benchmarks/`, and
  stays under the 500-line target.
- Repeated heap correctness passed: `TestRuntimeV2HeapAccounting` with
  `-count=3`, plus `TestRuntimeV2HeapAccountingConcurrentWorkersContract` with
  `SURGE_THREADS=2`, `4`, and `8`, each with `-count=3`. VM gates were run
  sequentially to avoid `RV2-DEBT-011`.
- Final manual benchmark report:
  `build/benchmarks/runtime-v2-task08-native-heap-current.md`. Selected stable
  rows: `serial_alloc_free` about `1564-1650 ns/op`, `serial_realloc` about
  `2538-2594 ns/op`, `heap_stats_poll` about `699-758 ns/op`, and
  `concurrent_alloc_free` scaling from `18972 ns/op` at 1 thread to
  `183527 ns/op` at 8 threads.
- Important limitation: this is a Surge-level generated fixture benchmark, not
  a pure C allocator/cache-line microbench. The `empty_loop` rows also move heap
  counters, so deltas include language/runtime allocation noise.
- New debt: `RV2-DEBT-012`. The stable default benchmark passes, but
  `serial_alloc_free` with `SURGE_HEAP_BENCH_SERIAL_ITERATIONS=200000`
  reproduced a `status=139` crash. Do not promote this benchmark beyond manual
  evidence until that stress path is minimized, fixed, or bounded deliberately.
- Review outcome: initial P2/P2/P3 benchmark-tooling findings were fixed;
  focused re-review returned no findings. `bash -n` passed; `shellcheck` was
  not installed.
- Final Task 8 Sentrux scans/rules passed: root `6190`, runtime `5279`,
  runtime/native `5318`.

## Epic 5 Task 09 Handoff

- Scope completed: stable heap-accounting tests are now part of local
  `make runtime-v2-check` and therefore the existing Runtime V2 CI job.
- Added `runtime-v2-heap-check` to `Makefile`, added it to `.PHONY`, and wired
  it into `runtime-v2-check` after the initial MT liveness seed and before
  waiter/fd-registry gates.
- The heap gate runs only stable tests: native heap smoke, buffered channel heap
  smoke, sequential/concurrent heap accounting contracts, and Task 5/6/7 static
  predicates. Task 8 stress runs and the manual heap benchmark stay out of CI.
- All heap gate commands force `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0`
  and use `-parallel=1 -p=1`.
- `.github/workflows/ci.yml` did not need an edit: the Runtime V2 job already
  installs LLVM and runs `make runtime-v2-check`.
- Checks passed after review fix: `make runtime-v2-heap-check`,
  `make runtime-v2-check`, `make check`, and `git diff --check`.
- Final Task 9 Sentrux scans/rules passed: root `6190`, runtime `5279`,
  runtime/native `5318`.
- Review outcome: one P2 env-inheritance issue was fixed; focused re-review
  returned no findings. No new debt was added by Task 9.

## Epic 5 Closeout Handoff

- Scope completed: per-shard heap accounting. Allocation/free/realloc accounting
  events now flow through runtime/shard-owned cells, `rt_heap_stats()` aggregates
  those cells on read, and the old global heap counters are not the source of
  truth.
- Public behavior preserved: `rt_alloc`, `rt_free`, `rt_realloc`, `HeapStats`,
  failed allocation/realloc, null/zero realloc, aligned allocation, and
  `rt_array_forget_allocation` semantics are covered by focused behavior and
  static gates.
- Stable CI coverage exists through `runtime-v2-heap-check`, which is called by
  `make runtime-v2-check`; the existing Runtime V2 GitHub Actions job inherits
  the heap gate.
- Runtime contract docs were updated narrowly: `docs/RUNTIME_V2.md` and
  `docs/RUNTIME.ru.md` no longer describe native heap accounting as global
  counters. No syntax, keyword, parser, or language-surface section was changed.
- Remaining debt: `RV2-DEBT-011` for overlapping VM build/test artifacts and
  `RV2-DEBT-012` for the heavier manual heap benchmark crash. The benchmark
  stays current-checkout manual evidence, not a CI threshold.
- Final closeout gates passed: `make c-check`, `make cppcheck`,
  `make runtime-v2-heap-check`, `make runtime-v2-check`, `make check`, and
  `git diff --check`.
- Final Sentrux scans/rules passed: root quality `6190`, `runtime/` quality
  `5279`, and `runtime/native` quality `5318`, all with 0 rule violations.
- Epic 6 should start from `N>1` accept ownership and hot-path connection task
  placement. It should not start from crossing syntax, remote-free queues,
  allocator pools, backend I/O migration, or language keyword selection.
- Preserve the Epic 7 syntax gate: `far`, `submit_to`, and `shard-movable` are
  semantic placeholders until a dedicated syntax review chooses final source
  spelling.

## Epic 4 Task 01 Handoff

- Scope completed: kickoff baseline and Sentrux state before fd-registry work.
  Docs-only; no runtime, test, Makefile, CI, or Sentrux rule changes.
- Start commit: `05ceb7c2 chore(runtime): enforce Runtime V2 quality gates`;
  branch `codex/runtime-net-scheduler-refactor`; clean tree at start.
- Line counts at kickoff: `rt_net.c` 1024, `rt_async_state.c` 1731,
  `rt_async_trace.c` 497, `rt_async_internal.h` 499, `rt_async_waiter.c` 309,
  `rt_runtime.c` 161.
- Sentrux CLI checks passed for all three roots: root `6198` (10 rules),
  runtime `5195` (7 rules), runtime/native `5159` (7 rules).
  `sentrux gate --save` stored the three baselines. The Sentrux MCP server is
  not connected in this session; CLI `check`/`gate` evidence replaces the MCP
  `session_start`/`session_end` flow for Epic 4 and this is recorded honestly
  in `04-evidence.md`.
- Startup gates: `make c-check` pass, `make cppcheck` pass, `make check`
  pass. `make runtime-v2-check` failed once in the MT seed (known Epic 3
  flake class, `AllowTimersToAdvance` program timeout under load); the full
  isolated rerun passed `exit=0`. Pre-existing flake debt, not an Epic 4
  regression.
- Gate plan for Tasks 2-7 is recorded in `04-evidence.md`.
- Next: Tasks 2-4 may run in parallel with separate write sets. Task 2 edits
  only the map doc; Task 3 edits only
  `internal/vm/runtime_v2_fd_registry_contract_test.go`; Task 4 edits only
  `internal/vm/runtime_v2_fd_registry_static_test.go`. The main session owns
  `04-evidence.md` and `NOTES.md` updates to avoid write conflicts.

## Epic 4 Tasks 02-04 Handoff

- Scope completed: dependency map (Task 2), fd lifecycle contract tests
  (Task 3), and registry static shape tests (Task 4). All three ran as
  plan-gated subagents with approved plans and disjoint write sets; the main
  session recorded evidence and owns the commits.
- New artifacts: `04-fd-registry-dependency-map.md` (390 lines),
  `internal/vm/runtime_v2_fd_registry_contract_test.go` (499 lines,
  `runtime_v2_pending`, 4/4 green twice),
  `internal/vm/runtime_v2_fd_registry_static_test.go` (175 lines,
  `runtime_v2_pending`, Boundary green, Shape expected-red until Task 5).
- Load-bearing map facts: close never wakes parked net waiters and never
  kicks the poller; numeric fd reuse can wake old-lifetime waiters;
  `ex->shutdown` has no writer anywhere in `runtime/native` (no graceful
  shutdown contract exists today); the wake pipe is process-global and
  written only from `park_current` for net keys.
- Approved Task 5 shape contract is pinned by the static Shape test:
  `rt_fd_entry {fd, generation, close_state, want_accept, want_read,
  want_write}`, `rt_fd_registry {entries, len, cap}`, by-value
  `rt_shard.fd_registry`, shard/executor accessors, and
  `rt_fd_registry_init/free/ensure_cap/len/find_const` returning
  `rt_runtime_status` for recoverable failures. Declarations go into a new
  `runtime/native/rt_fd_registry.h` included from `rt_async_internal.h`
  (that header is at 499/500 lines and must not grow past the limit).
- Contract-test assertion durability rule (do not violate in later tasks):
  the four fd contract tests assert only migration-durable counters
  (`io_poll_waiters_max`, `io_poll_calls`, `io_poll_net_ready`,
  `io_direct_waits`, `io_waiter_completed`). Tasks 7 and 12 must keep the
  meaning of `io_poll_waiters_max` as max distinct fd rows per poll build.
- Known behavior fact recorded during Task 3: Surge handle copies
  (`{ __opaque: handle }`) clone the `NetConn` view; after `close`, ops
  through a copy hit `EBADF` and map to `NET_ERR_IO` (8), not
  `NotConnected` (5). This is a live fd-reuse hazard input for Tasks 8-9.
- Caution recorded, outside Epic 4 scope: a scratch LLVM program printing a
  pointer-valued int handle (`conn.__opaque to string` concatenation)
  segfaulted reproducibly while uint error codes print fine. Candidate
  compiler/runtime bug for a later backend task; repro kept in the session
  scratchpad, not in the repo.
- CI note: the new fd contract tests are Task 13 promotion candidates by
  extending the `runtime_v2_pending` run filter in `runtime-v2-waiter-check`.

## Epic 4 Task 05 Handoff

- Scope completed: registry container skeleton (Task 5) as a plan-gated
  subagent. Working tree intentionally left uncommitted; the main session
  owns the commit and the Sentrux CLI check/gate evidence.
- What exists now: `runtime/native/rt_fd_registry.h` (54 lines; types,
  accessor and lifecycle declarations, one ownership comment block) included
  from `rt_async_internal.h` directly after the `rt_shard`/`rt_executor`
  forward typedefs; `runtime/native/rt_fd_registry.c` (72 lines;
  `init`/`free`/`ensure_cap`/`len`/`find_const`); by-value
  `rt_shard.fd_registry` beside `net_poll_scratch`; shard-first accessors
  plus shard0 executor adapters in `rt_runtime.c`; init wired into
  `rt_runtime_init_n1` so the registry initializes with the owning shard and
  status flows through the existing `exec_init_once` failure boundary.
- Line budget resolution: `rt_async_internal.h` stayed at 499 lines
  (+include, +field, -2 blank separator lines). All future fd-registry API
  growth (Tasks 6/7/9 mutators) must land in `rt_fd_registry.h`, which costs
  `rt_async_internal.h` nothing.
- Zero-reader guarantee held: nothing in `rt_net.c`, `rt_async_state.c`, or
  `rt_async_waiter.c` references the registry; the poll rebuild path is
  unchanged. No net behavior change is claimed or possible.
- `rt_fd_registry_free` has no caller by design: `ex->shutdown` still has no
  writer, so no teardown path exists to hook. Tasks 10-11 create the
  shutdown path and wire the free. Do not "fix" the unused free earlier.
- Growth contract (mirrors `rt_waiter_store_ensure_cap`): lazy allocation,
  start cap 16, doubling, `SIZE_MAX` overflow guards, `rt_realloc`, explicit
  `RT_RUNTIME_STATUS_*` codes, no `panic_msg` in the new API.
- Tested: Shape static gate flipped red->green with zero test edits;
  Boundary static gate green; Task 3 contract 4-pack green (15.9s); `make
  c-check`/`cppcheck`/`runtime-v2-check`/`check` green;
  `TestMTNetWaiterWakeupLatency` green (2.37s); `git diff --check` clean.
- Not tested: the registry has no behavior yet, so no liveness/behavior
  proof covers it; `ensure_cap`/`find_const` get their first behavior proof
  when Task 6 registration writes land and extend the Shape gate.
- Next decision before Task 6: mutation API shape for registration-side
  interest writes under `ex->lock` alongside `prepare_park`, plus the Shape
  static gate extension for those mutators.

## Epic 4 Task 06 Handoff

- Scope completed: net wait registration through registry-owned fd entries
  (Task 6) as a plan-gated subagent. Working tree intentionally left
  uncommitted; the main session owns the commit and the Sentrux CLI
  check/gate evidence.
- What exists now: `rt_fd_registry_attach_net_interest` /
  `rt_fd_registry_detach_net_interest` in `rt_fd_registry.h/.c` (63/154
  lines), driven by the `fd-registry-waiter-bridge` statics in
  `rt_async_waiter.c` (381 lines) at the four waiter-store mutation sites:
  attach in `add_waiter`, detach-if-last in `remove_waiter` / `pop_waiter`
  (same-pass `kept_same_key` counting) and in
  `rt_executor_wake_net_waiters_for_key` (remaining 0 by construction). The
  hook placement covers every net park/wake/cancel/rollback path with zero
  changes to `rt_net.c` (1024, flat) and `rt_async_state.c` (1731, flat);
  `rt_async_internal.h` stayed 499 (invariant block edited in place: ex->lock
  now lists fd registry rows).
- The registry has writers but ZERO readers: poll input is still 100%
  waiter-derived (`poll_net_waiters` byte-identical), so net behavior is
  preserved by construction. No new wake was added; `park_current` still owns
  the net wake-pipe kick and `io_cv` signal.
- Row lifetime invariant: a row exists iff at least one net-key waiter for
  that fd is parked. Clearing the last interest flag swap-removes the row.
  Consequence Task 9 MUST NOT miss: remove-plus-recreate resets `generation`
  to 0, so today's rows carry no cross-lifetime generation protection —
  Task 9 owns re-deciding row lifetime when it adds generation/close
  semantics (no generation bumps or close marking exist yet).
- Named bridges for later removal/validation:
  `fd-registry-waiter-bridge` (interest mirrors waiter-store membership;
  re-validate or replace at Tasks 7/9) and `fd-registry-attach-miss`
  (allocation failure -> waiter parks without a row, behavior unchanged;
  Task 7 must resolve it when poll input becomes registry-derived).
- Interest flags stay 0/1 flags per the pinned shape, not counts; the
  last-waiter decision comes from waiter-store scans. Duplicate same-key
  waiters keep interest alive (contract test 3 green).
- `wake_key_all_with_policy` net branch intentionally not hooked: grep
  re-verified zero net-key producers (scope/join/blocking only); stays
  Task 7 dead-path cleanup debt per the dependency map.
- Tested: c-check/cppcheck/runtime-v2-check/check green; extended Shape gate
  green with the new mutator pins; Boundary green with zero edits; Task 3
  contract 4-pack 4/4 (15.7s); `TestMTNetWaiterWakeupLatency` PASS (2.46s);
  `TestNativeNetSingleThreadBlockingChannelInAsyncServer` PASS (4.47s);
  manual debug-gated fixture run (`SURGE_ASYNC_DEBUG=1`) exited 0 with zero
  bridge mismatch/attach-miss lines while parking and completing net waiters.
- Not tested: registry contents are not yet observable by any behavior test
  (no reader exists); the debug recount check ran clean but only anomalies
  print, so its first adversarial proof arrives with the Task 8 fixtures.
  Native net benchmark deferred to Task 7 per the gate plan.
- Next decision before Task 7: poll-set construction from registry rows must
  replace `rt_executor_visit_net_waiters`, decide the `fd-registry-attach-miss`
  resolution (fail the wait vs. fallback), replace the `net_len` capacity
  hint, and delete/unify the dead `wake_key_all_with_policy` net bookkeeping.

## Epic 4 Task 07 Handoff

- Scope completed: poll-from-registry migration (Task 7) as a plan-gated
  subagent. Working tree intentionally left uncommitted; the main session
  owns the commit and the Sentrux CLI check/gate evidence.
- What exists now: `poll_net_waiters` builds its poll set ONLY from registry
  rows — capacity from `rt_fd_registry_len`, scratch filled by
  `rt_fd_registry_snapshot_poll_interest` (one linear pass, rows unique per
  fd, `want_accept` folded into readable-class `want_read`), completion and
  poll-error paths unchanged and running against the ex->lock-held snapshot
  copy. The waiter-scan build (`rt_executor_visit_net_waiters`,
  `collect_net_poll_fd`, `NetPollBuildContext`, `NetPollFd`) is deleted, as
  are dead `rt_executor_waiter_len`, `rt_executor_net_waiter_len`,
  `rt_waiter_key_visitor`, and the `wake_key_all_with_policy` net branch.
  Line counts: `rt_net.c` 1002, `rt_async_waiter.c` 348,
  `rt_async_internal.h` 493, `rt_fd_registry.h` 80, `rt_fd_registry.c` 213,
  `rt_async_state.c` 1727.
- fd-registry-attach-miss bridge is RESOLVED: `net_wait_current_task`
  verifies `rt_fd_registry_net_interest_present` after `prepare_park` and on
  a miss undoes the park (remove_waiter + clear park_prepared/park_key/
  pending_key) under the same lock hold, returning spurious readiness (net
  ops are nonblocking and re-wait). Invariant now load-bearing: a parked net
  waiter ALWAYS has a registry row, so `has_net_waiters` (still
  waiter-derived `net_len`) gating `begin_net_poll` cannot admit a
  zero-row poll cycle (which would busy-spin `rt_io_main` and strand
  `rt_worker_main`/`next_ready`).
- Task 8 fixture target (record from main-agent review): stale registry
  interest (flag set, zero same-key waiters) is now the only route to a
  level-triggered io-loop spin, because a stale row keeps its fd in every
  poll set while completions are no-ops. The `SURGE_ASYNC_DEBUG` bridge
  recount polices it; Task 8 fixtures should hammer duplicate-waiter
  cancel/close orderings to prove the invariant adversarially.
- CI-gate contract updates (main-agent approved, recorded in
  `04-evidence.md`): `TestRuntimeV2NetWaiterTraceContract` now asserts
  `io_waiter_scan_entries==0`, `io_waiter_net_entries==0`,
  `io_poll_dedup_checks==0` (machine-checkable "legacy rebuild path unused"
  evidence) and keeps `io_poll_rebuilds==io_poll_calls`;
  `runtime_v2_waiter_static_test.go` dropped the three deleted-symbol pins.
  Counter NAMES are untouched (Task 12 owns naming); only increment sites
  died with the legacy build. `io_poll_waiters_max` keeps its
  distinct-fd-rows-per-build meaning; fd contract 4-pack byte-identical and
  green.
- Tested: c-check/cppcheck/runtime-v2-check/check green; Shape gate extended
  with `rt_fd_poll_interest` + both Task 7 read APIs; Boundary green with
  zero edits; Task 3 4-pack 4/4 (16.0s); `TestMTNetWaiterWakeupLatency` PASS
  (2.31s); `TestNativeNetSingleThreadBlockingChannelInAsyncServer` PASS
  (4.34s); debug-gated contract pair PASS. Before/after
  `bench_native_net.sh` with pinned scratch compilers (`617f8cfa5881`):
  echo rows flat or better (e.g. 1/echo/seq 65.38 -> 62.14 us/op), scan/net
  entries -> 0 across all 24 rows, allocs flat at 2, rebuilds == calls; the
  `1/manager/seq` outlier in the AFTER report (129.62) was re-run same-binary
  to 110.86 — channel-hop run variance, not a poll regression. No leftover
  benchmark processes after either run (`ps` checks recorded).
- Not tested: attach-miss undo path has no fault-injection proof (allocation
  failure is not reachable from a fixture without an alloc shim); it is
  compile-proven, static-pinned, and its invariant is debug-checked. Close/
  cancel/re-register behavior is Tasks 8-9; generation/close_state still have
  no behavior.
- Main-session Task 7 closeout: Sentrux MCP checks passed for root `6198`,
  runtime `5228`, and runtime/native `5172`; `sentrux gate` passed for all
  three roots (`6198 -> 6198`, `5195 -> 5228`, `5159 -> 5172`). Re-run gates
  passed: `git diff --check`, `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, `make check`, fd static gates, fd contract 4-pack,
  net trace contract, focused net wake probe, single-thread net/channel probe,
  debug-path proof, and native net closeout benchmark
  `build/benchmarks/runtime-v2-task07-closeout-native-net.md`. Sentrux
  baseline files are committed so future `sentrux gate` checks are
  reproducible. `.loc-legacy-allowlist` ceilings were lowered to current
  `rt_async_state.c` 1727 and `rt_net.c` 1002.
- Next decision before Task 8/9: fixtures must cover close-with-parked-waiter
  and fd-reuse stale wake (dependency map hazards 1-2) now that rows are poll
  input; Task 9 re-decides row lifetime (remove-plus-recreate resets
  generation to 0) when close/generation semantics land.

## Epic 4 Task 08 Handoff

- Scope completed: close/cancel/re-register behavior tests. Only
  `internal/vm/runtime_v2_fd_registry_lifecycle_test.go`, Task 8 docs, the
  task index, evidence, and this notes file changed. Runtime/native, Makefile,
  CI, Task 7 code, and existing fd contract tests were not edited.
- New file: `runtime_v2_fd_registry_lifecycle_test.go` (297 lines,
  `runtime_v2_pending`, package `vm_test`) reuses the Task 3/7 helpers for
  LLVM fixture build/run, trace parsing, port allocation, and fd-registry trace
  assertions. The existing 499-line fd contract file stayed unchanged.
- Green now: cancelling one duplicate read waiter preserves the other read
  waiter and permits a later same-fd read re-registration; cancelling read
  interest while a write waiter remains active preserves the write interest.
  Final focused command passed both tests in package time `12.464s`.
- Task 9 expected-red now exists and is precise: closing a listener with a
  parked accept waiter exits `3` with `accept_close_timeout`; closing a
  connection with a parked read waiter while the peer stays open exits `3` with
  `read_close_timeout`. Both fixtures build cleanly and fail through runtime
  behavior only. Their TRACE_NET rows kept `io_waiter_scan_entries=0`,
  `io_waiter_net_entries=0`, and `io_poll_dedup_checks=0`.
- Numeric fd reuse was not added as a Go-only fixture. The Task 8 allowed write
  set excludes a native helper, and the Go/socket surface cannot force numeric
  reuse deterministically enough for CI. Task 9 must prove generation or
  closed-state stale-wake handling, or explicitly expand scope for a
  deterministic helper.
- Checks passed: `gofmt -l`, `go vet -tags runtime_v2_pending ./internal/vm`,
  tag-off fd-registry proof, focused green cancel/re-register tests,
  `TestMTNetWaiterWakeupLatency`, `make runtime-v2-check`, new-file
  whitespace check, `check_file_sizes.sh`, root, runtime, and
  runtime/native Sentrux gates, and `git diff --check`. Review subagent found
  no P0/P1 blockers. The close command is intentionally expected-red until
  Task 9 implements close-owned registry lifecycle.

## Epic 1 Artifacts

- `RULES.md`: global Runtime V2 development rules.
- `SENTRUX_POLICY.md`: Sentrux scan/rule policy and current rule-check
  requirements.
- `EVIDENCE_TEMPLATE.md`: required evidence format for future tasks.
- `01-baseline-evidence.md`: current checkout checks, benchmark reports,
  counters, and blockers.
- `LIVENESS_PROBES.md`: liveness probes by changed runtime surface.
- `OPEN_DECISIONS_BEFORE_EPIC_2.md`: accepted debt, blockers, and deferrals
  before structural `N=1` work.
- `01-contract-rules-harness.md`: durable Epic 1 summary and Epic 2 start
  criteria.

## Durable Decisions

- Work proceeds epic-by-epic. Later epics stay as a short roadmap until earlier
  evidence shapes the next slice.
- Subagents must plan first and wait for approval before edits or review work.
- `MUST` rules block completion, except for documented proving spikes with
  hypothesis, allowed files/surfaces, non-final behavior, proof command,
  success/failure criteria, and rollback.
- Runtime code must stay explainable through ownership, wakeup, cancellation,
  lifetime/generation, backpressure, and trace/test evidence.
- Sentrux is mandatory. Root and scoped scans are required when a task mostly
  affects `runtime/`.
- Runtime V2 code limit is 500 lines for new or heavily rewritten code files.
- New V2 C APIs return explicit status codes for recoverable failures.
  `panic_msg` is not the primitive error-handling contract.
- Channel FIFO, task parking at suspension points, cooperative cancellation,
  structured join/failfast outcomes, and `@local spawn` sendability rules are
  source-visible contracts.
- Native global FIFO waiters, global inject, worker-local queues, Tier 1 work
  stealing, direct channel handoff placement, and sync-channel compensation are
  current implementation artifacts unless a later spec promotes one explicitly.
- VM/native parity means semantic output parity under native `threads=1`, not
  identical scheduler interleavings.

## Current Sentrux Baselines

- Repository scan: `/home/zov/projects/surge/surge`, `quality_signal=6198`.
- Runtime scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5195`.
- Runtime/native scan: `/home/zov/projects/surge/surge/runtime/native`,
  `quality_signal=5159`.
- `check_rules` reports no `.sentrux/rules.toml` for the scanned paths. This is
  not a passing rule check. Runtime-code tasks must add real rules or record an
  explicit temporary deferral without claiming rule compliance.

## Current Baseline Debt

- `go test ./internal/vm -run 'MT|Async|Net|LLVM'` fails in this checkout when
  timeout-sensitive tests are not skipped.
- Default `make check` passes because `SURGE_SKIP_TIMEOUT_TESTS=1` skips those
  timeout-sensitive VM/LLVM tests through `skipTimeoutTests`.
- The focused VM failure is accepted backend-test debt. A later test/backend
  epic will rewrite the VM/native/LLVM test matrix around stable Runtime V2
  contracts.
- Native net and channel benchmark reports in `build/benchmarks/` were
  regenerated with a temporary current-checkout compiler after a stale `./surge`
  binary was detected.

## Epic 2 Start Blockers

- Sentrux missing-rules status was explicitly deferred by Epic 2 Task 1 without
  claiming rule compliance. Runtime-code tasks still must add real rules or
  record a fresh temporary deferral for the active scan path.
- The first `N=1` task must name the exact behavior equivalence boundary from
  `01-contract-rules-harness.md` and `OPEN_DECISIONS_BEFORE_EPIC_2.md`.
- Epic 2 evidence must keep the focused VM debt named and must not attribute new
  runtime regressions to that debt without proof.

## Epic 2 Task 1 Kickoff Handoff

- Task: `02-tasks/01-kickoff-evidence.md`.
- Scope completed: documentation-only baseline evidence. No runtime,
  compiler, ABI, benchmark, CI, or Sentrux rule-file changes were made.
- Start state: baseline commit
  `e7d9563d5c78a90409e4d6a92bd47d49b30ae830`; branch
  `codex/runtime-net-scheduler-refactor`; `git status --short` was empty before
  the task.
- Sentrux root scan for `/home/zov/projects/surge/surge` returned
  `quality_signal=6210`, `files=4740`, `import_edges=1887`, and
  `lines=370800`; health bottleneck remains `modularity`; `check_rules` still
  reports missing `/home/zov/projects/surge/surge/.sentrux/rules.toml`.
- Sentrux runtime scan for `/home/zov/projects/surge/surge/runtime` returned
  `quality_signal=5147`, `files=32`, `import_edges=30`, and `lines=14883`;
  health bottleneck remains `redundancy`; `check_rules` still reports missing
  `/home/zov/projects/surge/surge/runtime/.sentrux/rules.toml`.
- Missing Sentrux rules remain a blocker to claiming rule compliance. Task 1
  records a temporary deferral only; the first runtime-code task must either add
  real rules or record a fresh deferral for the active scan path.
- Accepted VM debt remains unchanged:
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` is not an Epic 2 kickoff
  gate and was not run in Task 1. New runtime failures must not be assigned to
  this debt without matching `01-baseline-evidence.md`.
- Approved checks for Task 1: `git diff --check` and `make check`; broad
  focused VM regex, benchmarks, and extra liveness probes are intentionally
  skipped.
- Task 1 checks passed: `git diff --check` produced empty output, and
  `make check` passed in 14.31s. `make check` ran
  `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./... --timeout 90s`, `golangci-lint`,
  `make c-check`, and `check_file_sizes.sh`.
- Next owner: Epic 2 Task 2, Field Ownership Map. It should classify current
  `rt_executor` state before any runtime field movement.

## Epic 2 Task 2 Field Ownership Handoff

- Task: `02-tasks/02-field-ownership-map.md`.
- Scope completed: documentation-only ownership classification. No runtime,
  compiler, ABI, benchmark, CI, Sentrux rule-file, staging, or commit changes
  were made.
- Output: `02-field-ownership-map.md` classifies every `rt_executor` field into
  runtime lifecycle/control plane, `N=1` shard-local hot state,
  compatibility/offload state, trace/debug-facing state, or later-epic state.
- Direct usage searches covered scheduler queues, waiter storage, net poll
  scratch, task/scope registries, lifecycle flags, channel compensation, and
  blocking pool state under `runtime/native`.
- Safe Epic 2 move candidates are runtime lifecycle shell, task/scope registry,
  scheduler queue shape, net poll scratch, and channel/blocking compatibility
  state. Each remains behavior-preserving and must use the matching
  `LIVENESS_PROBES.md` evidence when code moves fields.
- Deferred owners: local-waiter epic for owner-local waiter queues, local
  fd-registry epic for persistent readiness, multi-shard runtime epic for owner
  placement and distributed scope semantics, allocator/pools epic for heap
  counters and hot object pools, and later IO/backend work for backend choice.
- First code-task boundary: introduce the runtime/shard shell around `lock`,
  `ready_cv`, `io_cv`, `done_cv`, `workers`, `worker_ctxs`, `worker_count`,
  `initialized`, `io_started`, `shutdown`, `sched_mode`, and `sched_seed` only.
  Do not move waiters, fd readiness semantics, channel handoff semantics,
  blocking pool queue, or task/scope ownership unless the approved task plan
  expands the field group and evidence.
- File-size risk remains active for `rt_async_state.c`, `rt_net.c`, and
  `rt_async_channel.c`; later runtime-code tasks must avoid growing them or
  record a split/follow-up.
- Approved checks for Task 2: `git diff --check` and the map placeholder sanity
  grep. Runtime tests, benchmarks, liveness probes, and Sentrux scans are
  intentionally skipped for this docs-only task.

## Epic 2 Task 3 CI/Test Contract Handoff

- Task: `02-tasks/03-runtime-v2-ci-test-contract.md`.
- Scope completed: documentation-only CI/test contract. No `Makefile`, GitHub
  Actions, runtime, compiler, benchmark, Sentrux, staging, or commit changes
  were made.
- Output: `02-ci-test-contract.md` defines a future `runtime-v2-check` shape
  that runs exact named tests with `SURGE_BACKEND=llvm` and
  `SURGE_SKIP_TIMEOUT_TESTS=0`.
- Proposed seed tests:
  `TestMTWakeupsAndCancellation`, `TestMTChannelParkUnpark`,
  `TestMTBlockingChannelHelpersAllowTimersToAdvance`, and
  `TestMTSeededScheduler`.
- Proposed Task 12 command:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
    go test ./internal/vm \
      -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
      -v --timeout 120s
  ```

- Required future CI setup: install `clang`, `llvm`, and `lld`; preflight
  `clang` and `ar`; set `SURGE_MT_TIMEOUT_SCALE=3`; keep the Runtime V2 job
  separate from the default skipped-timeout Go matrix.
- Excluded required gate:
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'`. It remains accepted
  backend-test debt and may be used only as a diagnostic until the later
  test/backend matrix epic fixes or replaces it.
- Local-only until re-proven: net latency, one-worker net/channel
  compatibility, broader channel correctness, structured concurrency, blocking
  pool, heavier sync-helper compensation, compensation-limit stress, and
  current Tier 1 work-stealing probes.
- Candidate Runtime V2 seed and net commands were not run in Task 3. Do not
  report them as fresh passes without Task 12 or task-specific evidence.
- Approved checks for Task 3: `git diff --check` and direct
  `git diff --no-index --check` on the new contract file. Runtime tests,
  `make check`, `make c-check`, `make cppcheck`, benchmarks, and Sentrux scans
  are intentionally skipped for this docs-only task.

## Epic 2 Task 4 Runtime/Shard Skeleton Tests Handoff

- Task: `02-tasks/04-runtime-shard-skeleton-tests.md`.
- Scope completed: added a local-only pending static check for the Task 5
  runtime/shard skeleton. No runtime implementation, `Makefile`, CI workflow,
  benchmark, Sentrux, staging, or commit changes were made.
- New test: `TestRuntimeV2SkeletonStaticShape` in
  `internal/vm/runtime_v2_skeleton_static_test.go`.
- The test is hidden behind `//go:build runtime_v2_pending`; default test runs
  do not see it.
- The test compiles a C snippet with `clang -std=c11 -fsyntax-only` and requires
  `RT_RUNTIME_SHARD_COUNT == 1`, complete `rt_runtime` and `rt_shard` types,
  and the accessors `rt_executor_runtime`, `rt_runtime_shard0`, and
  `rt_runtime_shard_count`.
- Preflight tools exist: `command -v clang` returned `/usr/bin/clang`, and
  `command -v ar` returned `/usr/bin/ar`.
- Expected pre-Task-05 failure was recorded with:

  ```bash
  go test -tags runtime_v2_pending ./internal/vm \
    -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s
  ```

  It failed with missing `RT_RUNTIME_SHARD_COUNT`, undeclared `rt_runtime` and
  `rt_shard`, and undeclared skeleton accessors. This is the desired proof that
  Task 5 has not been implemented yet.
- Default safety check passed:
  `go test ./internal/vm -run '^$' --timeout 30s` returned
  `ok surge/internal/vm (cached) [no tests to run]`.
- `git diff --check` passed after the test and docs edits.
- Task 5 should make this pending check pass as part of skeleton implementation
  or record a blocker unrelated to Task 5 code. Task 12 owns deciding whether
  this exact tagged check or a non-pending successor joins `runtime-v2-check`.

## Epic 2 Task 5 Runtime/Shard Skeleton Handoff

- Task: `02-tasks/05-runtime-shard-skeleton.md`.
- Scope completed: added the internal `N=1` `rt_runtime`/`rt_shard` skeleton
  and accessors required by Task 4. No public ABI, `N>1`, waiter, fd registry,
  scheduler, net poll, channel/blocking, compiler, benchmark, CI, Sentrux rule,
  staging, or commit changes were made.
- Runtime shape: `RT_RUNTIME_SHARD_COUNT == 1`; `rt_runtime` owns
  `shards[RT_RUNTIME_SHARD_COUNT]`; `rt_shard` links to the runtime and current
  executor; `rt_executor` gained only `rt_runtime* runtime`.
- Required accessors now exist: `rt_executor_runtime`, `rt_runtime_shard0`, and
  `rt_runtime_shard_count`.
- New skeleton init uses `rt_runtime_status`. `exec_init_once()` still preserves
  the legacy `pthread_once`/`panic_msg` boundary because it cannot return an
  init status to callers.
- File-size result: `rt_async_internal.h` is `432` lines, new
  `rt_runtime.c` is `64` lines, and over-limit `rt_async_state.c` was reduced
  from `2391` to `2368` lines by moving cold default worker-count helpers.
- Checks passed:

  ```bash
  git diff --check
  command -v clang
  command -v ar
  go test -tags runtime_v2_pending ./internal/vm \
    -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s
  make c-check
  make cppcheck
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
  make check
  ```

- One local failure happened and was fixed inside Task 5: the first
  `make c-check` run showed `rt_async_state.c` still needed `<unistd.h>` for
  existing trace `write()` calls after CPU-count detection moved.
- Main-agent Sentrux runtime `session_end` passed against the pre-task baseline:
  `5147 -> 5144`, delta `-2`, summary `Quality stable or improved`, and no
  violations. A worker-context `session_end` could not reuse that baseline.
- Post-change root Sentrux: `/home/zov/projects/surge/surge`,
  `quality_signal=6209`, bottleneck `modularity`, rules file missing.
- Post-change runtime Sentrux: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5144`, bottleneck `redundancy`, rules file missing.
- Missing Sentrux rules remain a blocker to claiming rule compliance, not a
  blocker to this narrow skeleton implementation.

## Epic 2 Task 6 Scheduler Shape Tests Handoff

- Task: `02-tasks/06-scheduler-shape-tests.md`.
- Scope completed: selected and ran existing scheduler and CI-shaped liveness
  proofs before scheduler field movement. No runtime C, Go test, `Makefile`,
  GitHub Actions, STATS, benchmark, task-doc, Sentrux, staging, or commit
  changes were made.
- Scheduler trace proof command passed:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WorkStealing|SeededScheduler)$' -v --timeout 90s
  ```

  Both `TestMTWorkStealing` and `TestMTSeededScheduler` ran and passed.
- CI-shaped Runtime V2 seed command passed:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
  ```

  All four exact tests ran and passed.
- Tool preflight passed: `command -v clang` returned `/usr/bin/clang`, and
  `command -v ar` returned `/usr/bin/ar`.
- CI ownership: `TestMTSeededScheduler` remains in the future Runtime V2 seed.
  `TestMTWorkStealing` remains local-only/current-runtime evidence and must not
  join the seed unless a later Tier 2 CPU-pool decision promotes stealing.
- Parked-with-work remains a missing invariant. Task 6 did not add a weak
  nondeterministic test.
- Task 7 may proceed only if it preserves current wake elision, worker sleep
  rules, and shard park state. If Task 7 needs to change any of those, it must
  stop and add a real parked-with-work invariant first.
- `git diff --check` passed after the documentation updates.
- Verification note: do not run overlapping `go test ./internal/vm` commands
  that include the same MT test names. The test artifact directory is keyed by
  test name under `target/debug/.tests/`, so parallel runs can collide while
  writing artifacts and create a false failure unrelated to runtime behavior.

## Epic 2 Task 7 Scheduler Shape Migration Handoff

- Task: `02-tasks/07-scheduler-shape-migration.md`.
- Scope completed: moved only scheduler container fields behind the existing
  `N=1` `rt_shard.scheduler`: `inject`, `local_queues`, `worker_ctxs`,
  `worker_count`, `running_count`, `sched_mode`, and `sched_seed`.
- Preserved executor/global lifecycle state on `rt_executor`: `workers`,
  `ready_cv`, `io_cv`, `done_cv`, `lock`, `shutdown`, `net_polling`,
  `initialized`, `io_started`, `channel_blocked_workers`,
  `compensation_count`, `compensation_high_water`, and blocking-pool fields.
- No `runtime/native/rt.h`, `Makefile`, CI, Go test, benchmark script,
  Sentrux rule, net/channel/waiter/task ownership semantic, public ABI,
  staging, or commit changes were made.
- Direct moved-field audit passed with no matches:

  ```bash
  rg -n -- 'ex->(inject|local_queues|worker_ctxs|worker_count|running_count|sched_mode|sched_seed)\b|exec_state\.(sched_seed|sched_mode)' runtime/native
  ```

  `rg` returned exit `1`, the expected no-match status.
- Tool preflight passed: `command -v clang` returned `/usr/bin/clang`, and
  `command -v ar` returned `/usr/bin/ar`.
- Final checks passed:

  ```bash
  go test -tags runtime_v2_pending ./internal/vm \
    -run '^TestRuntimeV2SkeletonStaticShape$' -v --timeout 30s
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WorkStealing|SeededScheduler)$' -v --timeout 90s
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -v --timeout 120s
  make c-check
  make cppcheck
  make check
  ```

- A first `make cppcheck` run found const-pointer style warnings in
  `rt_async_state.c`; the declarations were narrowed and the final standalone
  `make cppcheck` passed.
- Sentrux post-change root scan: `/home/zov/projects/surge/surge`,
  `quality_signal=6207`, bottleneck `modularity`, rules file missing.
- Sentrux post-change runtime scan: `/home/zov/projects/surge/surge/runtime`,
  `quality_signal=5168`, bottleneck `redundancy`, rules file missing.
  Supplied runtime baseline was `5125`, so the scoped signal increased by `43`.
- Main-agent Sentrux runtime `session_end` passed against the pre-task baseline:
  `5125 -> 5168`, delta `+43`, summary `Quality stable or improved`, and no
  violations. Missing rules remain a blocker to claiming rule compliance, not a
  blocker to this narrow shape migration.
- Parked-with-work remains a missing invariant. Task 7 did not change wake
  elision, worker sleep rules, or shard park state, so it did not cross the
  Task 6 boundary.
- Next task: Task 8 must record net poll scratch before-evidence. Run
  `TestMTNetWaiterWakeupLatency` with `SURGE_SKIP_TIMEOUT_TESTS=0`, run the
  native net benchmark with a current-checkout `SURGE` binary and an outer
  timeout, and keep persistent fd registry behavior out of scope. Task 9 should
  not start until Task 8 evidence exists.

## Epic 2 Task 8 Net Poll Scratch Tests Handoff

- Scope completed: recorded net wake and native net benchmark before-evidence.
  No runtime C, Go test, script, `Makefile`, CI workflow, Sentrux rule, STATS,
  task-doc, staging, or commit changes were made.
- Temp compiler was built outside the repository at
  `/tmp/surge-task08.zkEoYd/surge`. Its `version --full --format json`
  `git_commit` matched current `HEAD`: `49b3aa34ec26`.
- Version line recorded:

  ```text
  surge 0.1.13-dev — "forge storms before they land"
  commit: 49b3aa34ec26
  message: refactor(runtime): move scheduler state under shard
  built:  2026-06-26T12:41:59Z
  ```

- Net wake probe passed:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
    -run '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s
  ```

  The test ran and passed in package time `2.647s`. It did not print trace rows
  on success; it asserted the `TRACE_NET` and `TRACE_EXEC_SNAPSHOT` rows
  internally from child stderr.
- Native net benchmark passed with an outer timeout:

  ```bash
  tmpdir=/tmp/surge-task08.zkEoYd
  SURGE_NET_BENCH_REPORT="$PWD/build/benchmarks/runtime-v2-task08-native-net-before.md" \
    timeout 120s env SURGE="$tmpdir/surge" ./scripts/bench_native_net.sh
  ```

  Report path:
  `/home/zov/projects/surge/surge/build/benchmarks/runtime-v2-task08-native-net-before.md`.
- Key benchmark invariants from the full 24-row report: task-context blocking
  sends, task-context blocking recvs, compensation started, and compensation
  high-water stayed `0`; `poll allocs` stayed `2`; `dedup checks` stayed `0`.
- Test decision: no new semantic test is needed for Task 9 if it only moves
  `net_poll_fds`, `net_poll_fds_cap`, `net_poll_pfds`, and
  `net_poll_pfds_cap` behind the `N=1` shard/container and preserves
  rebuild-from-waiters semantics.
- Task 9 must stop for a revised plan if it changes waiter ownership,
  persistent fd registration, readiness lifetime, accept ownership, poll
  ownership, or net wake placement.
- CI ownership: `TestMTNetWaiterWakeupLatency` remains local-only Task 8/9
  evidence unless Task 12 re-proves CI stability. The native net benchmark
  remains manual before/after evidence and should not join CI.

## Task 9 Handoff

- Scope completed: moved only `net_poll_fds`, `net_poll_fds_cap`,
  `net_poll_pfds`, and `net_poll_pfds_cap` out of `rt_executor` and into
  `rt_shard.net_poll_scratch`.
- Implementation shape: added `rt_net_poll_scratch`, added
  `rt_shard_net_poll_scratch()` / `rt_executor_net_poll_scratch()`, and changed
  `ensure_net_poll_fds()` / `ensure_net_poll_pfds()` to grow the shard scratch
  buffers.
- Preserved behavior: `poll_net_waiters()` still derives capacity from
  `ex->net_waiters_len`, scans `ex->waiters`, deduplicates fds in the existing
  nested loop, calls `poll()`, and completes read/accept/write waiters through
  the same keys.
- Explicitly not moved or changed: `net_waiters_len`, `net_polling`, `io_cv`,
  waiter ownership, accept ownership, wake fd placement, fd registry/readiness
  lifetime, public ABI, compiler code, benchmark scripts, Makefile, CI, Sentrux
  rules, and STATS.
- Static audit results:
  - No `net_poll_fds` / `net_poll_pfds` fields remain inside `struct
    rt_executor`.
  - `struct rt_shard` now owns `rt_net_poll_scratch net_poll_scratch`.
  - No direct `->net_poll_fds`, `->net_poll_fds_cap`, `->net_poll_pfds`, or
    `->net_poll_pfds_cap` usage remains under `runtime/native`.
  - Zero-context runtime diff has no changed lines mentioning `net_waiters_len`,
    `net_polling`, `io_cv`, waiters, net wake fd placement, fd registry,
    `epoll`, `kqueue`, `io_uring`, `eventfd`, or accept ownership.
- Focused net wake probe passed:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMTNetWaiterWakeupLatency$' -v --timeout 90s`.
- Current-checkout compiler pin passed with temporary binary
  `/tmp/surge-task09-final.aqFZBL/surge`; both current and reported commits
  were `b48f58ec84e0`.
- Native net after-benchmark passed and wrote
  `build/benchmarks/runtime-v2-task09-native-net-after.md`. The report is
  ignored under `build/`; selected durable rows are copied into
  `02-evidence.md`.
- Benchmark invariants from the full 24-row report: task-context blocking sends,
  task-context blocking recvs, compensation started, and compensation high-water
  stayed `0`; `poll allocs` stayed `2`; `dedup checks` stayed `0`.
- Gates passed locally: `make c-check`, `make cppcheck`, and `make check`.
- Main-session Sentrux runtime/native `session_end` passed against the pre-task
  baseline: `signal_before=5132`, `signal_after=5146`, `signal_delta=14`, no
  violations. Root scan stayed `6207`; required runtime policy scan ended at
  `5182`; runtime/native scan ended at `5146`.
- Missing root and runtime Sentrux rules remain baseline debt. This is not a
  passing rules gate.

## Task 10 Handoff

- Scope completed: evidence/docs only. No runtime/native code, Go tests,
  scripts, `Makefile`, CI, Sentrux rules, STATS, public ABI, compiler code,
  staging, or commits were changed.
- Completion state: complete with known debt for the narrow Task 11
  counter-field migration boundary only.
- Task 11 allowed boundary: move or wrap field ownership for
  `channel_blocked_workers`, `compensation_count`, and
  `compensation_high_water`, and preserve their trace-facing accessors.
- Task 11 must stop for a revised plan before changing compensation semantics,
  sync helper behavior, direct `try_send` or handoff behavior, ready-work
  draining at the compensation limit, channel waiter semantics, or channel
  close/cancellation behavior.
- Stable direct channel subset passed:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMT(RecvAckHandoffCompletesSenderAfterNonYieldingReceiver|BufferedRecvRefillCompletesSenderAfterNonYieldingReceiver|BufferedBlockingRecvRefillWakesSender|ChannelParkUnpark)$'
  -v --timeout 120s -count=1 -parallel=1 -p=1`.
- CI-contract channel/blocking pair passed:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
  '^TestMT(ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance)$' -v
  --timeout 120s -count=1 -parallel=1 -p=1`.
- Broader sync fallback local-only probe did not pass:
  `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` timed out at
  their internal 10-second program timeout; `AllowTimersToAdvance` passed.
- Known direct handoff debt: `TestMTNonYieldingTrySendHandoffWakesReceiver`
  times out when run alone at `SURGE_MT_TIMEOUT_SCALE=1` and `3`. This blocks
  Task 11 only if Task 11 changes direct `try_send`, handoff placement, or
  wake-before-park behavior.
- Current-checkout compiler pin passed for temporary binary
  `/tmp/surge-task10.nOjRbh/surge`; both current and reported commits were
  `8ef946f6cc9e`.
- Native channel before-benchmark passed and wrote
  `build/benchmarks/runtime-v2-task10-native-channel-before.md`. The report is
  ignored under `build/`; selected durable rows are copied into
  `02-evidence.md`.
- Benchmark trace baseline: all 20 Runtime Trace rows had required
  channel/fallback fields and no `n/a` values. `channel_reused_reply` and
  `channel_new_reply` kept blocking and compensation counters at `0` with
  `handoff yields=19999`. `channel_sync_new_reply` recorded `5000`
  task-context blocking sends and `5000` task-context blocking recvs in every
  mode; channel blocking waits were `0` in mode `1` and nonzero in multi-worker
  or default modes. Compensation stayed `0` for every benchmark row.
- Future owners: direct channel handoff / `try_send` task for the non-yielding
  handoff timeout, sync-helper compensation/liveness task for
  `DoNotParkWorkers`, compensation-limit and ready-drain task for
  `DrainReadyWorkAtCompensationLimit`, and the later local channel-waiter epic
  for close/cancellation and waiter cleanup matrices.

## Task 11 Handoff

- Scope completed: moved `channel_blocked_workers`, `compensation_count`, and
  `compensation_high_water` out of `rt_executor` and under
  `rt_shard.channel_blocking_compat`. Added shard/executor compatibility
  accessors mirroring the scheduler and net scratch accessor shape.
- Runtime files changed: `runtime/native/rt_async_internal.h`,
  `runtime/native/rt_runtime.c`, and `runtime/native/rt_async_state.c`.
- Docs changed: `docs/runtime-v2-epics/02-evidence.md` and this file.
- Strictly untouched: `runtime/native/rt_async_channel.c`,
  `runtime/native/rt_async_blocking.c`, `runtime/native/rt.h`, Go tests,
  scripts, `Makefile`, CI, Sentrux rules, STATS, public ABI, and compiler code.
- Behavior boundary: no direct `try_send` or handoff changes, no sync-helper
  behavior changes, no compensation semantic changes, no ready-work draining at
  the compensation limit, no channel waiter semantics changes, and no channel
  close/cancellation changes.
- Static audit results:
  - No `channel_blocked_workers`, `compensation_count`, or
    `compensation_high_water` fields remain inside `struct rt_executor`.
  - `struct rt_shard` now owns
    `rt_channel_blocking_compat channel_blocking_compat`.
  - No direct `ex->channel_blocked_workers`, `ex->compensation_count`,
    `ex->compensation_high_water`, or matching `exec_state.*` usage remains
    under `runtime/native`.
  - Forbidden-surface diff for channel protocol, blocking pool, ABI, tests,
    scripts, `Makefile`, CI, STATS, and compiler paths was empty.
- Gates passed locally: stable direct channel subset, CI-contract
  channel/blocking pair, current-checkout native channel benchmark,
  `make c-check`, `make cppcheck`, `make check`, and `git diff --check`.
- Current-checkout compiler pin passed for temporary binary
  `/tmp/surge-task11-final.86ZWJ8/surge`; both current and reported commits
  were `ec640a47b449`.
- Native channel after-benchmark passed and wrote
  `build/benchmarks/runtime-v2-task11-native-channel-after.md`. The report is
  ignored under `build/`; selected durable rows are copied into
  `02-evidence.md`.
- Benchmark trace evidence: all 20 Runtime Trace rows had required
  channel/fallback fields and no `n/a` values. Async request/reply probes kept
  blocking and compensation counters at `0`; `channel_sync_new_reply` recorded
  `5000` task-context blocking sends and `5000` task-context blocking recvs in
  every mode. Compensation started and compensation high-water stayed `0` for
  every benchmark row.
- Known-debt tests intentionally not run in Task 11:
  `TestMTNonYieldingTrySendHandoffWakesReceiver`,
  `TestMTBlockingChannelHelpersDoNotParkWorkers`, and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit`.
- Sentrux evidence: root scan `6207`, runtime scan `5209`, runtime/native scan
  `5172`, and main-session runtime/native `session_end` passed
  `5146 -> 5172` with no violations. All three `check_rules` calls still
  report missing `.sentrux/rules.toml`; this remains debt, not compliance.

## Task 12 Handoff

- Scope completed: added an explicit Runtime V2 liveness gate in `Makefile` and
  a separate GitHub Actions job outside the existing Go matrix.
- Files changed: `Makefile`, `.github/workflows/ci.yml`,
  `docs/runtime-v2-epics/02-ci-test-contract.md`, `02-evidence.md`, and this
  file; `02-n1-runtime-shard-structure.md` received the matching status update.
- `make runtime-v2-check` preflights `clang` and `ar`, then runs:

  ```bash
  SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 SURGE_MT_TIMEOUT_SCALE=3 go test ./internal/vm \
    -run '^TestMT(WakeupsAndCancellation|ChannelParkUnpark|BlockingChannelHelpersAllowTimersToAdvance|SeededScheduler)$' \
    -count=1 -parallel=1 -p=1 -v --timeout 120s
  ```

- CI job details: installs `clang`, `llvm`, `lld`, and `binutils`; sets
  `SURGE_MT_TIMEOUT_SCALE=3`; runs `make runtime-v2-check`.
- Local checks passed:
  - `make runtime-v2-check`: all four exact seed tests ran and passed; package
    time `7.427s`.
  - `make check`: passed; default path still used `SURGE_SKIP_TIMEOUT_TESTS=1`,
    then `golangci-lint`, nested `make c-check`, and the file-size check.
- Main-session verification caught and fixed the first target shape before
  commit: without explicit scale/serialization, `TestMTBlockingChannelHelpersAllowTimersToAdvance`
  hit its internal `program timeout after 10s`.
- Sentrux evidence: root scan `6207`, runtime scan `5209`; both `check_rules`
  calls still report missing `.sentrux/rules.toml`, which remains debt rather
  than compliance.
- Default `make check` was not changed.
- The broad accepted-debt command
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` was not added as a green gate.
- Not promoted to CI in Task 12: `TestMTWorkStealing`,
  `TestMTNetWaiterWakeupLatency`, `TestRuntimeV2SkeletonStaticShape`, and the
  heavier known-debt channel/blocking stress probes.
- Review risk: repository branch protection may need to require the new
  `Runtime V2 liveness (llvm)` job name separately.

## Task 13 Handoff

- Scope completed: audited the migrated Epic 2 accessor surfaces from Tasks 05,
  07, 09, and 11, then recorded static gate evidence.
- Result: audit-only. No `runtime/native` code change was justified.
- Runtime files inspected: `runtime/native/rt_async_internal.h`,
  `runtime/native/rt_runtime.c`, `runtime/native/rt_async_state.c`,
  `runtime/native/rt_async_task.c`, and `runtime/native/rt_net.c`.
- Docs changed: `docs/runtime-v2-epics/02-evidence.md`, this file, and
  `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`.
- Static audit results:
  - No old scheduler fields remain as `ex->inject`, `ex->local_queues`,
    `ex->worker_ctxs`, `ex->worker_count`, `ex->running_count`,
    `ex->sched_mode`, `ex->sched_seed`, or matching `exec_state.*` access.
  - Scheduler users resolve through `rt_executor_scheduler*()` or
    `rt_shard_scheduler*()` before using local `scheduler->...` fields.
  - No old net poll scratch fields remain as `ex->net_poll_*` or
    `exec_state.net_poll_*` access. `poll_net_waiters()` resolves scratch via
    `rt_executor_net_poll_scratch(ex)`.
  - No old channel/blocking compatibility counters remain as
    `ex->channel_blocked_workers`, `ex->compensation_count`,
    `ex->compensation_high_water`, or matching `exec_state.*` access.
  - Direct runtime/shard container access is confined to `rt_runtime.c`, the
    owner/accessor implementation.
  - `rt_runtime_shard_count` was retained. It is used by
    `TestRuntimeV2SkeletonStaticShape` under `runtime_v2_pending`, so it is an
    intentional skeleton surface rather than an unused helper.
- Gates passed locally: `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, `make check`, and `git diff --check`.
- Sentrux evidence: main-session scans recorded root `6207`, runtime `5209`,
  and runtime/native `5172`. All three `check_rules` calls still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.
- Strictly untouched: runtime/native code, Go tests, scripts, `Makefile`, CI,
  Sentrux rules, STATS, public ABI, scheduler/net/channel/blocking semantics,
  owner-local waiters, persistent fd registry, `N>1`, and crossing syntax.

## Task 14 Closeout Handoff

- Scope completed: closed Epic 2 documentation for the `N=1`
  runtime/shard-structure slice and recorded final local gates.
- Docs changed: `docs/runtime-v2-epics/02-n1-runtime-shard-structure.md`,
  `02-evidence.md`, this file, and `README.md`.
- No runtime/native code, Go tests, `Makefile`, CI, scripts, Sentrux rules,
  `STATS.md`, staging, or commit changes were made by this executor.
- Closeout audit found no owner-local waiter, persistent fd registry, `N>1`,
  or crossing-syntax implementation in the Epic 2 runtime/compiler surfaces.
- CI status: `make runtime-v2-check` is the stable local target, and the
  separate GitHub Actions job runs the same seed with timeout-sensitive tests
  enabled. Default `make check` still uses `SURGE_SKIP_TIMEOUT_TESTS=1` and is
  not proof for the broad timeout-sensitive VM/LLVM matrix.
- Sentrux status: main-session closeout scans recorded root `6207`, runtime
  `5209`, and runtime/native `5172`. All three `check_rules` calls still report
  missing `.sentrux/rules.toml`; this remains debt, not compliance.
- Final local gates passed: `make runtime-v2-check`, `make check`,
  `make c-check`, `make cppcheck`, and `git diff --check`.

## Epic 3 Starting Point

- Start with owner-local waiter design, still under `N=1`. Do not combine it
  with persistent fd registry work, `N>1`, accept ownership, or crossing syntax.
- First docs task: map every current waiter user from
  `runtime/native/rt_async_internal.h`, `rt_async_state.c`,
  `rt_async_channel.c`, `rt_async_task.c`, `rt_async_poll.c`,
  `rt_async_scope.c`, `rt_net.c`, and `rt_async_blocking.c`.
- First proof target: owner cleanup, stale wake prevention, cancellation and
  timeout interaction, waiter lifetime/generation, and the current global FIFO
  behavior that must either remain intentional or be demoted from contract to
  implementation detail.
- Gate shape: keep `make runtime-v2-check`, `make check`, `make c-check`,
  `make cppcheck`, and focused `LIVENESS_PROBES.md` commands. Do not promote
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` to a green gate until the
  later test/backend matrix epic replaces that debt.
- Sentrux: record scans honestly. Missing rules are not compliance.

## Liveness Requirements

- Runtime-code tasks cannot close with "watch for hangs" as evidence.
- Use `LIVENESS_PROBES.md` to choose probes by changed surface.
- Missing probes that block owning future work include parked-with-work
  invariant, owner-local waiter cleanup tests, fd-registry lifecycle test,
  channel close/cancellation race matrix, native shutdown liveness, cross-shard
  wake-fd elision, cross-shard cancellation generation, and per-probe timeout
  wrappers for channel benchmarks.

## Known Large Files

These files already exceed the 500-line Runtime V2 limit and need care when
touched:

- `runtime/native/rt_async_state.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_async_channel.c`
- `runtime/native/rt_async_task.c`
- `internal/vm/mt_executor_test.go`
- `internal/vm/mt_correctness_test.go`

Touching an over-limit file must record whether the task reduces it, keeps it
flat, or creates a follow-up split task.

## Dead Ends And Cautions

- Do not tune scheduler behavior by machine-specific constants as durable
  design.
- Do not let proving-spike code become architecture without rewriting it into
  rule-compliant form.
- Do not use `TestMTWorkStealing` as a future Tier 1 contract without deciding
  whether the assertion moves to explicit Tier 2 work.
- Do not treat missing Sentrux rules as a passing rules gate.
- Do not treat default `make check` as proof that timeout-sensitive VM/LLVM
  liveness and parity tests pass.
- Do not spend Epic 2 capacity rewriting the semi-broken backend test matrix;
  that belongs to a later dedicated test/backend epic.

## Epic 3 Draft Handoff

- Drafted Epic 3 as
  `docs/runtime-v2-epics/03-owner-local-waiters-and-runtime-refactor.md`.
- Added `docs/runtime-v2-epics/03-evidence.md` and brief task scopes under
  `docs/runtime-v2-epics/03-tasks/`.
- Epic 3 scope: owner-local waiter storage under `N=1`, with no persistent fd
  registry, no `N>1`, no accept ownership, and no crossing syntax.
- Refactoring is now a first-class Epic 3 track. It must be dependency-aware:
  behavior proof first, dependency cluster recorded before extraction, no
  mixed refactor/behavior commits, and no dead-code deletion without reference,
  build, test, and Sentrux evidence.
- Current line-count pressure recorded in the epic:
  `rt_async_state.c` 2431 lines, `rt_net.c` 1040,
  `rt_async_task.c` 768, `rt_async_channel.c` 549, and
  `rt_async_internal.h` 460.
- First Epic 3 implementation task remains Task 01, the kickoff baseline and
  Sentrux evidence. No runtime code has been changed by the draft.
- Subagent plan gate remains required. A read-only explorer plan for waiter and
  refactor analysis was approved and completed with no file edits.
- Subagent confirmed Runtime V2-relevant pressure in `rt_async_state.c`,
  `rt_net.c`, `rt_async_task.c`, `rt_async_channel.c`, and
  `rt_async_internal.h`. It also noted larger non-waiter files such as
  `rt_term.c` and `rt_fs.c`; keep those out of Epic 3 unless touched by waiter
  work.
- Dead-code seed for Task 03: `rt_select_poll_tasks` is suspect only. It has
  native, ABI, and LLVM builtin references, while current select emission
  appears to use `rt_select_poll`. Do not delete it without generated-IR search,
  ABI review, focused tests, and Sentrux evidence.
- Draft verification: `git diff --check` passed. Sentrux root scan
  `/home/zov/projects/surge/surge` reported `quality_signal=6207`; scoped
  runtime scan `/home/zov/projects/surge/surge/runtime` reported
  `quality_signal=5209`. Both `check_rules` calls still report missing
  `.sentrux/rules.toml`, which remains debt rather than compliance.

## Epic 3 Task 01 Handoff

- Scope completed: kickoff baseline and Sentrux state before implementation.
- Start commit: `f4f83c4d docs(runtime): draft Runtime V2 waiter epic`.
- Working tree was clean at task start.
- Runtime/native line-count pressure at start:
  `rt_async_state.c` 2431, `rt_term.c` 1091, `rt_net.c` 1040,
  `rt_fs.c` 978, `rt_async_task.c` 768, `rt_string.c` 762,
  `rt_async_channel.c` 549, and `rt_async_internal.h` 460.
- Startup gates passed: `make runtime-v2-check`, `make c-check`,
  `make cppcheck`, and `make check`.
- Sentrux baseline:
  - root `/home/zov/projects/surge/surge`: `quality_signal=6207`;
  - runtime `/home/zov/projects/surge/surge/runtime`: `quality_signal=5209`;
  - native `/home/zov/projects/surge/surge/runtime/native`:
    `quality_signal=5172`;
  - `session_start` saved the native scan baseline.
- `check_rules` still reports missing `.sentrux/rules.toml` for root, runtime,
  and runtime/native. This is debt, not compliance.
- Accepted backend-test debt remains unchanged: do not promote
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` to a green gate.

## Epic 3 Tasks 02-03 Handoff

- Scope completed: read-only waiter dependency map and refactor/dead-code audit.
- Created `docs/runtime-v2-epics/03-waiter-dependency-map.md`.
- Created `docs/runtime-v2-epics/03-refactor-audit.md`.
- Updated `docs/runtime-v2-epics/03-tasks/README.md` to mark Tasks 02 and 03
  complete.
- No runtime/native code changed.
- Key waiter map facts:
  - current waiter storage is executor-global under `ex->lock`;
  - `wake_token` guards wake-before-park;
  - `net_waiters_len` is a polling hint, not owner-local storage and not an fd
    registry;
  - shutdown-adjacent waiter cleanup has no scoped contract yet;
  - FIFO-by-key remains an open decision before owner-local storage changes.
- Refactor audit result:
  - first safe tranche is waiter key/list extraction into a cohesive waiter
    module, with storage still executor-global;
  - do not move `wake_task_with_policy`, `wake_key_all_with_policy`,
    `park_current`, `clear_select_timers`, net polling, channel handoff, or
    task/select ABI in the first extraction;
  - no proven-dead code was found;
  - `rt_select_poll_tasks` remains suspect-only and must stay until generated
    IR search, ABI review, focused select tests, static gates, and Sentrux
    evidence prove deletion is safe.

## Epic 3 Task 05 Handoff

- Scope completed: added a default-tag static boundary check for the pre-Task 06
  waiter helper extraction seam.
- Created `internal/vm/runtime_v2_waiter_static_test.go`.
- The test asserts the current `rt_executor` waiter storage fields, `rt_task`
  prepared-waiter cleanup fields, `waker_key`/`waiter` storage shape, and helper
  declarations that Task 06 may move into a cohesive waiter module.
- No `runtime/native` files changed.
- The `runtime_v2_pending` waiter behavior tests belong to Task 04 evidence and
  were not used as a Task 05 gate.
- Task 05 checks passed: focused static Go test, `make c-check`,
  `make cppcheck`, and `git diff --check`.
- Sentrux was not run for Task 05, and no missing-rules status is reported as
  compliance.

## Epic 3 Task 06 Handoff

- Scope completed: extracted the legacy waiter key/list helper tranche into
  `runtime/native/rt_async_waiter.c` while preserving executor-global waiter
  storage and task-local wait-key storage.
- Moved helpers: waker key constructors/classification, private net waiter
  accounting for add/remove/pop paths, waiter capacity, wait-key capacity,
  add/remove/clear waiters, wait-key registration, `prepare_park`, and
  `pop_waiter`.
- Kept in `rt_async_state.c`: `park_current`, `wake_task_with_policy`,
  `wake_key_all_with_policy`, `clear_select_timers`, net polling, channel
  handoff, task/select ABI, and all storage fields.
- Header change was limited to `waker_is_net()` because `park_current()` still
  needs net-key classification after the extraction.
- `wake_key_all_with_policy()` retains the same `net_waiters_len` decrement
  inline so `net_waiters_removed()` can stay private to the waiter module.
- Line counts after closeout: `rt_async_state.c` 2431 -> 2212,
  `rt_async_waiter.c` new at 226, `rt_async_internal.h` 460 -> 461,
  `03-evidence.md` 270 -> 381, `NOTES.md` 912 -> 947, and
  `03-tasks/README.md` 41 -> 41.
- Checks passed: `clang-format -i runtime/native/rt_async_waiter.c`,
  `git diff --check`, `make c-check`, `make cppcheck`, rerun
  `make runtime-v2-check`, `make check`, cancellation/join/timeout smoke,
  `TestMTCorrectnessChannels`, and `TestMTNetWaiterWakeupLatency`.
- `make runtime-v2-check` first timed out once in
  `TestMTBlockingChannelHelpersAllowTimersToAdvance`; an isolated rerun passed,
  then the full target passed.
- Direct channel LLVM probe kept known debt visible:
  `TestMTNonYieldingTrySendHandoffWakesReceiver` timed out after 10s; the other
  four listed direct-channel tests passed. The default-backend command passed
  only because all five MT tests skipped under VM.
- Sentrux post-change scans: root `6215`, runtime `5264`, runtime/native
  `5227`. Root, runtime, and runtime/native `check_rules` still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.

## Epic 3 Task 07 Handoff

- Scope completed: added a pending owner-local waiter skeleton static check.
- Created `internal/vm/runtime_v2_owner_local_waiter_static_test.go` behind
  `runtime_v2_pending`.
- The pending C shape probe expects `rt_waiter_store` with `entries`, `len`,
  `cap`, and `net_len`; `rt_shard.waiter_store`; and the approved
  `rt_shard_waiter_store*` / `rt_executor_waiter_store*` accessor surface.
- The check fails before Task 08 with unknown `rt_waiter_store`, missing waiter
  store accessors, and no `rt_shard.waiter_store` member.
- The default waiter static test was intentionally not changed; it still checks
  current executor-global waiter storage until Task 08 moves the shape.
- No `runtime/native` files changed.
- Task 07 checks run: expected failing pending proof and passing default static
  safety check. `git diff --check` and `make check` are recorded in
  `03-evidence.md`.
- Task 08 must add the owner-local waiter store under the single shard, keep
  compatibility wrappers, and then update or promote the default static shape
  check.

## Epic 3 Task 08 Handoff

- Scope completed: moved waiter storage behind `rt_shard.waiter_store` under
  the existing `N=1` runtime shape.
- Added `rt_waiter_store` with `entries`, `len`, `cap`, and `net_len`; added
  the shard/executor waiter-store accessors approved in Task 07; removed direct
  waiter storage fields from `rt_executor`.
- Kept compatibility helpers: `add_waiter`, `remove_waiter`, `pop_waiter`,
  `prepare_park`, `clear_wait_keys`, `add_wait_key`, and `ensure_waiter_cap`
  remain the caller-facing helper surface.
- Added `rt_waiter_store_ensure_cap()` with explicit `rt_runtime_status`
  results. The compatibility wrapper keeps the old panic-on-allocation-failure
  behavior.
- Routed remaining direct users in `rt_async_state.c` and `rt_net.c` through the
  store. Net polling still rebuilds scratch from the current waiter list, and
  `net_len` remains a hint, not an fd registry.
- No `N>1`, fd registry, crossing syntax, channel semantic, net semantic, or
  public ABI change was added.
- Updated the default waiter static proof to the owner-local shape. The Task 07
  pending owner-local proof now passes.
- Direct-field audit passed: no `->waiters`, `->waiters_len`, `->waiters_cap`,
  or `->net_waiters_len` uses remain in `runtime/native` or `internal/vm`.
- Checks passed: owner-local pending static proof, default waiter static proof,
  pending waiter behavior proof, `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, and `make check`.
- Sentrux post-change scans: root `6206`, runtime `5220`, runtime/native
  `5184`. Root, runtime, and runtime/native `check_rules` still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.
- Line counts after closeout: `rt_async_internal.h` 471, `rt_runtime.c` 161,
  `rt_async_waiter.c` 252, `rt_async_state.c` 2221, `rt_net.c` 1042,
  `runtime_v2_waiter_static_test.go` 82, and
  `runtime_v2_owner_local_waiter_static_test.go` 53.

## Epic 3 Tasks 09-14 Handoff

- Scope completed: proved channel, task, scope, blocking, timer, select, and
  cancellation waiter users after the owner-local waiter-store move.
- Added pending channel/timer contracts in
  `internal/vm/runtime_v2_waiter_contract_test.go`:
  `TestRuntimeV2ChannelCloseWakesSendWaiters` and
  `TestRuntimeV2CancelledSelectCleansWaitKeysAndTimers`.
- Added pending task/scope/blocking contracts in
  `internal/vm/runtime_v2_task_scope_blocking_waiter_contract_test.go`:
  cancelled join waiter cleanup, failfast scope owner wake, blocking completion
  wake, and cancelled blocking waiter cleanup.
- Tasks 10, 12, and 14 closed as no-op runtime migrations. Task 08 had already
  moved waiter storage to `rt_shard.waiter_store`; the affected users call
  compatibility helpers that now route through `rt_executor_waiter_store()`.
- Direct legacy waiter-field audit remains clean: no `->waiters`,
  `->waiters_len`, `->waiters_cap`, or `->net_waiters_len` uses in
  `runtime/native` or `internal/vm`.
- Passing probes recorded in `03-evidence.md`: full pending waiter contract set,
  channel MT probes, `TestMTCorrectnessChannels`, wakeup/structured/blocking MT
  probes, default waiter static proof, and owner-local pending static proof.
- Known debt recorded: `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` timeout after
  30s, including isolated reruns. `TestMTBlockingChannelHelpersAllowTimersToAdvance`
  passed and remains the stable Runtime V2 gate member.
- No runtime/native files changed in Tasks 09-14. New tests are pending local
  proofs until Task 18 decides what to promote into CI.
- Closeout gates passed for Tasks 09-14: default waiter static proof, full
  pending waiter contract set, channel MT probes, task/scope/blocking MT probes,
  `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`, and
  `git diff --check`.
- Sentrux batch scans: root `6206`, runtime `5220`, runtime/native `5184`.
  Root, runtime, and runtime/native `check_rules` still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.

## Epic 3 Tasks 15-16 Handoff

- Scope completed: proved the current net waiter trace contract and migrated
  net waiter traversal/completion behind owner-local waiter helper APIs.
- Added pending proof
  `internal/vm/runtime_v2_net_waiter_contract_test.go`:
  `TestRuntimeV2NetWaiterTraceContract`.
- The pending proof runs a small LLVM net server, drives repeated TCP
  request/reply traffic, sends `SIGUSR1`, and validates live plus exit
  `TRACE_NET` lines.
- Trace contract now checks field presence, nonzero net poll/readiness/direct
  wait/rebuild/complete counters, `io_poll_rebuilds == io_poll_calls`, and
  `io_waiter_net_entries <= io_waiter_scan_entries`.
- Added owner-local helper API:
  `rt_executor_waiter_len`, `rt_executor_net_waiter_len`,
  `rt_executor_visit_net_waiters`, and
  `rt_executor_wake_net_waiters_for_key`.
- The wake helper is explicitly net-only and rejects non-net keys. Generic
  channel/task/scope wake policy remains outside this task.
- `rt_net.c` still owns fd dedupe, `poll()`, wake-fd drain, and trace counters.
  The task did not introduce a persistent fd registry, accept ownership,
  wake-fd relocation, scheduler changes, `N>1`, `eventfd`, `epoll`, `kqueue`,
  or `io_uring`.
- Line counts after Task 16: `rt_net.c` 1024, `rt_async_waiter.c` 309,
  `rt_async_internal.h` 483, `runtime_v2_net_waiter_contract_test.go` 249,
  and `runtime_v2_waiter_static_test.go` 90.
- Checks passed: pending net trace contract, `TestMTNetWaiterWakeupLatency`,
  `TestNativeNetSingleThreadBlockingChannelInAsyncServer`, default waiter
  static boundary, `make c-check`, `make cppcheck`, `make runtime-v2-check`,
  `make check`, and `git diff --check`.
- Read-only review subagent found no P0/P1 blockers. The only P2 was that the
  new pending test file was untracked before staging; close this in the
  Task 15-16 commit scope.
- Native net benchmark before/after ran with freshly built current-checkout
  binaries and wrote ignored reports:
  `build/benchmarks/runtime-v2-epic3-task16-native-net-before.md` and
  `build/benchmarks/runtime-v2-epic3-task16-native-net-after.md`.
- Benchmark trace rows stayed comparable. For the first `1 echo seq` row,
  before had `poll calls=4673`, `poll rebuilds=4673`, `poll allocs=2`; after
  had `poll calls=4421`, `poll rebuilds=4421`, `poll allocs=2`.
- Sentrux native session: 5184 -> 5178, `pass=true`, no violations. Post scans:
  root `6203`, runtime `5214`, runtime/native `5178`.
- Root, runtime, and runtime/native `check_rules` still report missing
  `.sentrux/rules.toml`; this remains debt, not compliance.
- Known debt: `rt_net.c` remains over the 500 LOC target at 1024 lines. Task 17
  owns the next large-file refactor tranche.
- Known future work: net close/cancel/fd-registry lifecycle proof remains out of
  scope until the fd registry epic. Task 18 owns CI promotion for pending net
  proofs.

## Epic 3 Task 17 Handoff

- Scope completed: extracted trace and SIGUSR1 dump responsibility from
  `runtime/native/rt_async_state.c` into
  `runtime/native/rt_async_trace.c`.
- The new module owns `TRACE_EXEC`, `TRACE_EXEC_SNAPSHOT`, `SCHED_TRACE`, trace
  buffers, trace init/dump, signal-dump request handling, and trace counters.
- Scheduler trace source mapping now uses the explicit
  `rt_trace_sched_source` enum instead of raw `0`/`1`/`2` values.
- No scheduler, waiter, timer, channel, or net behavior was changed. No dead
  code was deleted.
- Line counts after Task 17: `rt_async_state.c` 1731,
  `rt_async_trace.c` 497, `rt_async_internal.h` 499, and `rt_net.c` 1024.
- Checks passed after the refactor: stable MT trace subset,
  `TestMTNetWaiterWakeupLatency`, pending
  `TestRuntimeV2NetWaiterTraceContract`, `git diff --check`, `make c-check`,
  `make cppcheck`, `make runtime-v2-check`, and `make check`.
- Read-only review subagent found no blockers. Its only advisory was to include
  the new `rt_async_trace.c` file in the commit.
- Sentrux native session: 5178 -> 5218, `pass=true`, no violations. Post scans:
  root `6208`, runtime `5255`, runtime/native `5218`.
- `check_rules` still reports missing `.sentrux/rules.toml`; this remains debt,
  not rule compliance.

## Epic 3 Task 18 Handoff

- Scope completed: added stable waiter liveness checks to the Runtime V2 local
  and CI gate path.
- Added `runtime-v2-waiter-check` as a companion Makefile target. It runs the
  default-tag `TestRuntimeV2WaiterHelperStaticBoundary` proof and the exact
  `runtime_v2_pending` waiter proof set promoted from Tasks 04, 07, 09, 11, 13,
  and 15.
- `make runtime-v2-check` still runs the existing LLVM MT seed first, then calls
  `make runtime-v2-waiter-check`.
- `.github/workflows/ci.yml` was unchanged. The `Runtime V2 liveness (llvm)` job
  already installs LLVM and invokes `make runtime-v2-check`, so CI now reaches
  the waiter gate through the same entrypoint.
- Excluded from the green gate: broad accepted-debt regex
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'`,
  `TestMTBlockingChannelHelpersDoNotParkWorkers`, and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit`.
- Checks passed: `make runtime-v2-waiter-check`, `make runtime-v2-check`,
  `make check`, and `git diff --check`.
- Sentrux root session: 6198 -> 6198, `pass=true`, no violations. Post scans:
  root `6198`, runtime `5195`, runtime/native `5159`.
- `check_rules` still reports missing `.sentrux/rules.toml`; this remains debt,
  not rule compliance.

## Epic 3 Task 19 Handoff

- Scope completed: structural closeout for Epic 3. The durable epic document,
  Runtime V2 epic README, task index, notes, and evidence ledger now mark Epic
  3 complete and preserve the handoff to Epic 4.
- Current closeout claim is local and bounded. Do not state CI green unless a
  fresh CI run is recorded. The existing CI workflow reaches
  `make runtime-v2-check`, and Task 18 made that target run the stable waiter
  liveness gate.
- Main-session closeout gates passed: `make runtime-v2-check`, `make cppcheck`,
  `git diff --check`, `make c-check`, and `make check`.
- `make runtime-v2-check` ran the existing MT seed and
  `runtime-v2-waiter-check`; the waiter set included
  `TestRuntimeV2WaiterHelperStaticBoundary` and all promoted
  `runtime_v2_pending` waiter proofs, including
  `TestRuntimeV2NetWaiterTraceContract`.
- Fresh net and channel benchmarks passed with
  `/tmp/surge-epic3-closeout.Oo0179/surge`, built from `c9fb2f8e`. The reports
  were written under ignored `build/benchmarks/` paths and must not be added.
- Net first row: `1 echo seq`, `60.08 us/op`, `net direct waits=1787`,
  `net poll calls=4028`, `net ready=1787`, `waiter scan entries=12080`,
  `net waiter entries=4028`, `poll rebuilds=4028`, `poll allocs=2`,
  `complete calls=3574`, `completed waiters=1787`.
- Channel key rows: `1 channel_reused_reply` at `3289 ns/op` with handoff
  yields `19999` and fallback fields `0`; `1 channel_sync_new_reply` at
  `9150 ns/op` with task-context blocking sends `5000` and recvs `5000`.
- Post-doc Sentrux closeout scans: root `6198`, runtime `5195`,
  runtime/native `5159`. `check_rules` still reports missing
  `.sentrux/rules.toml` for all three paths; this remains debt, not rule
  compliance.
- Remaining debt to keep named: broad focused VM regex, missing Sentrux rules,
  timeout-sensitive tests `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit`,
  over-500-line `rt_async_state.c` and `rt_net.c`, and no persistent fd registry
  in Epic 3.
- Epic 4 should start with persistent fd registry and net lifecycle proof:
  registration, readiness lifetime, close/cancel cleanup, wake-fd ownership,
  and shutdown behavior. Do not start Epic 4 with `N>1` or crossing syntax.

## Epic 4 Task 9 Handoff

- Scope completed: close, cancellation/re-register stale completion, and
  stale poll snapshot protection are now fd-registry lifecycle concerns.
- fd rows use registry-owned monotonic generations; close snapshots carry
  fd/generation plus exact accept/read/write interests.
- close marks rows closed under `ex->lock`, raw-closes outside the executor
  lock, then wakes only the snapshot keys and signals net poll/`io_cv`.
- poll snapshots exclude closed rows and carry generation; poll-error and
  readiness completion go through registry guarded completion helpers.
- Task 8 close tests are no longer expected-red:
  `CloseWakesParkedAcceptWaiter` and `CloseWakesParkedReadWaiter` both pass.
- Deterministic stale proof uses fd `42` in a tiny C registry test; no OS fd
  allocation luck is involved.
- Boundary recorded as RV2-DEBT-010: copied public net handles still carry the
  raw fd view and are not generation-aware yet.
- Trace zero contract stays intact: `io_waiter_scan_entries`,
  `io_waiter_net_entries`, and `io_poll_dedup_checks` remain asserted zero by
  `TestRuntimeV2NetWaiterTraceContract`.
- Checks passed: static fd-registry proof, focused Task 8 lifecycle quartet,
  `TestRuntimeV2NetWaiterTraceContract`, `TestMTNetWaiterWakeupLatency`,
  `make c-check`, `make cppcheck`, `make runtime-v2-check` after one isolated
  `TestMTChannelParkUnpark` timeout/rerun, `make check`, and
  `git diff --check`.

## Epic 4 Task 10 Handoff

- Scope completed: wake-fd and shutdown tests only. No runtime C, Makefile,
  CI, `STATS.md`, or `DEBT.md` changes.
- LOC discipline: new Task 10 tests live in separate opt-in files
  `runtime_v2_fd_registry_wake_test.go` (`446` lines) and
  `runtime_v2_fd_registry_shutdown_static_test.go` (`133` lines); existing
  contract/static files remain `499` and `426` lines.
- Green runtime trace proof:
  `TestRuntimeV2FDRegistryWakeFDObservedForInterestAddedDuringPoll` asserts
  `io_poll_wake_fd>=1`, `io_poll_waiters_max>=2`, and zero legacy poll-build
  counters on live `SIGUSR1` and exit traces.
- Green close proof:
  `TestRuntimeV2FDRegistryCloseWakePollNotificationProof` is a deterministic C
  behavior check around `rt_fd_registry_wake_closed_net_waiters`; it proves
  current Task 9 code calls both `rt_net_wake_poll()` and
  `pthread_cond_broadcast(&ex->io_cv)` when a close snapshot wakes waiters.
  Close wake-fd behavior is not expected-red anymore.
- Expected-red for Task 11:
  `TestRuntimeV2FDRegistryCancelledInterestWakesPoller` fails only because
  `io_poll_wake_fd` stays `2 -> 2` after cancellation-side interest removal.
  The test uses a dedicated stderr pipe/scanner and waits for the
  `reason=sigusr1` baseline before releasing the gate; the baseline already
  has two parked fd rows and the legacy scan counters are zero.
- Expected-red for Task 11:
  `TestRuntimeV2FDRegistryShutdownDrainStaticContract` fails only because
  `rt_executor_request_shutdown` and
  `rt_executor_drain_shutdown_net_waiters` are not declared. The names are the
  current explicit-status shutdown contract proposal, following the
  owner-first `rt_executor_*` helper style.
- Important testing note: a runtime close `SIGUSR1` delta test was rejected
  during implementation because `reason=sigusr1` dumps are drained
  asynchronously and can be emitted after the gate release. Keep close wake
  coverage as the direct C behavior proof unless Task 11 adds a synchronous
  trace hook.
- Checks run: green wake/close proof command passed; cancellation expected-red
  command failed with the intended `io_poll_wake_fd` delta assertion; shutdown
  static command failed with the intended two undeclared identifiers;
  `TestMTNetWaiterWakeupLatency` passed; `gofmt -l` and `git diff --check`
  were clean.

## Epic 4 Task 11 Handoff

- Scope completed: wake-fd and shutdown migration focused checks, heavy gates,
  Sentrux, and read-only review subagent passed.
- Cancellation-side net waiter removal now notifies the poller only after the
  last same-key open net interest is detached from the fd registry. This is
  the explicit "in-flight poll snapshot may be stale" signal. Readiness
  completion and `pop_waiter` do not write extra wake bytes.
- Shutdown drain behavior is registry-owned:
  `rt_fd_registry_drain_shutdown_net_waiters_locked` snapshots registry rows,
  wakes exact accept/read/write keys through
  `rt_executor_wake_net_waiters_for_key`, clears matching fd-lifetime rows,
  and wakes poll/`io_cv` only when it drained interests.
- Public owner/control-plane wrappers live in new
  `runtime/native/rt_shutdown.c`: `rt_executor_drain_shutdown_net_waiters`
  and `rt_executor_request_shutdown`. The request API is not wired into normal
  program lifecycle in Task 11.
- LOC discipline: `rt_async_state.c` was not modified and stayed `1727`
  lines; `rt_async_internal.h` stayed below limit at `495` lines; new
  `rt_shutdown.c` is `33` lines.
- Added deterministic proof
  `TestRuntimeV2FDRegistryShutdownDrainBehavior`: public drain wrapper wakes
  registered net keys, clears registry rows/interests, notifies wake-poll/io-cv
  on non-empty drain, and leaves empty drain as OK no-op.
- Former expected-red Task 10 checks are now green:
  `TestRuntimeV2FDRegistryCancelledInterestWakesPoller` and
  `TestRuntimeV2FDRegistryShutdownDrainStaticContract`.
- Focused checks passed: shutdown static+behavior tests, cancellation
  wake-fd test, Task 10 wake/close green tests, `TestMTNetWaiterWakeupLatency`,
  `gofmt -l`, `git diff --check`, `make c-check`, and `make cppcheck`.
- Heavy gates passed: `make runtime-v2-check`, `make check`, and Sentrux
  root/runtime/runtime-native gates (`6198 -> 6194`, `5195 -> 5239`,
  `5159 -> 5184`).
- Review subagent returned APPROVE with no P0/P1/P2 findings. Residual
  boundary: `rt_executor_request_shutdown` is not wired into normal lifecycle
  yet, and blocking-worker shutdown behavior is not behavior-tested in Task 11.

## Epic 4 Task 12 Handoff

- Scope completed: trace/benchmark visibility only. No runtime C, Makefile,
  CI, `STATS.md`, or `DEBT.md` changes.
- No new runtime counters were added. Existing `TRACE_NET` fields already
  prove registry-derived poll input: the legacy poll-build counters stay zero,
  `io_poll_rebuilds == io_poll_calls`, and waiter totals/max/last expose
  registry-row snapshot sizes. Treat these names as migration/debug evidence,
  not public ABI.
- `scripts/bench_native_net.sh` now includes the missing existing net fields
  in the report trace table: poll timeouts, wake-fd count, poll errors,
  timeout last/max, and waiters last/max.
- `TestRuntimeV2NetWaiterTraceContract` now also asserts
  `io_poll_waiters_total >= io_poll_calls` and
  `io_poll_waiters_max >= io_poll_waiters_last`. It still avoids exact counter
  values.
- Focused checks passed: `gofmt -l`, `bash -n scripts/bench_native_net.sh`,
  `TestRuntimeV2NetWaiterTraceContract`, and the wake/cancel fd-registry
  regression command.
- Native net benchmark was run with a fresh `/tmp` compiler binary built from
  `./cmd/surge/`; version commit `fd82d34686e9` matched HEAD. Report:
  `build/benchmarks/runtime-v2-task12-native-net.md`.
- Benchmark report has 24 runtime trace rows. The seven newly reported fields
  are present in every row; zero legacy poll-build counters and
  `poll rebuilds == net poll calls` held for every row.
- Main-session heavy gates passed after the focused slice: `make
  runtime-v2-check`, `make check`, and Sentrux root/runtime/runtime-native
  gates (`6198 -> 6194`, `5195 -> 5230`, `5159 -> 5175`).
- Review subagent returned APPROVE. Residual boundary: benchmark numbers are
  evidence, not a strict performance gate; `TRACE_NET` fields remain
  migration/debug evidence, not public ABI.

## Epic 4 Task 13 Handoff

- Scope completed: stable fd-registry liveness promotion into the Runtime V2
  CI path. No runtime C, Go tests, benchmark scripts, Sentrux rules, `STATS.md`,
  or `DEBT.md` changes.
- `Makefile` now has `runtime-v2-fd-registry-check`, called by
  `runtime-v2-check` after the waiter gate. `.github/workflows/ci.yml` was not
  edited because the Runtime V2 CI job already installs LLVM tools and runs
  `make runtime-v2-check`.
- Promoted stable command:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2FDRegistry(RepeatedReadinessSingleFD|ReadWriteInterestSharesFDRow|DuplicateReadWaitersBothComplete|ClosedFDFailsFast|StaticShape|StaticBoundary|GenerationStaleSnapshotProof|CloseWakePollNotificationProof|ShutdownDrainStaticContract|ShutdownDrainBehavior)$' -count=1 -parallel=1 -p=1 -v --timeout 180s`.
- CI includes the stable behavior contract quartet and deterministic C
  compile/run checks. It intentionally excludes live `SIGUSR1` probes, short
  timeout-window close proofs, and heavier cancellation/payload lifecycle
  proofs until those are separately stabilized for required CI.
- Main-session checks passed: `make runtime-v2-fd-registry-check` (10/10
  selected tests, package time `15.734s`), `make runtime-v2-check` (new
  fd-registry gate reached and passed, package time `16.185s`), `make check`,
  and `git diff --check`.
- Sentrux root session stayed stable at `6194 -> 6194`; `runtime` quality
  `5230` and `runtime/native` quality `5175`; rules passed for all three
  paths.
- Review subagent approved after one P2 doc issue was fixed: the fd-registry
  liveness row now says the global waiter poll scan is pre-Epic-4 baseline and
  the current runtime polls fd-registry snapshots.

## Epic 4 Task 14 Handoff

- Scope completed: `TRACE_NET` counters and dump helpers
  moved from `runtime/native/rt_net.c` into new `runtime/native/rt_net_trace.c`
  plus `runtime/native/rt_net_trace.h`.
- `rt_net.c` still owns wake-fd init/write/drain, poll construction,
  registry snapshots, `poll()`, and close lifecycle. No wake-fd or fd-registry
  behavior was moved in this slice.
- `rt_net_trace_dump(const char*)` remains externally visible and keeps the
  same `TRACE_NET` field names/order. Counters are private `static` atomics in
  the new `.c` file. Header helpers avoid fd-registry types; waiter completion
  passes `calls` and `woken`.
- LOC result: `rt_net.c` `1002 -> 904`; new `rt_net_trace.c` `128`; new
  `rt_net_trace.h` `73`; `rt_async_internal.h` stayed `495`.
  `.loc-legacy-allowlist` lowered the `rt_net.c` ceiling to exact count `904`.
- Focused checks passed: `clang-format -i` on changed C/H files,
  `make c-check`, `make cppcheck`, `make runtime-v2-fd-registry-check`
  (10 selected tests, package time `15.853s` in the final gate rerun),
  `git diff --check`, new-file
  `git diff --no-index --check`, and `./check_file_sizes.sh`.
- Fix note: first `make c-check` failed because `clang-format` reordered local
  includes, so `rt_net_trace.h` could not rely on `rt_async_internal.h` being
  included first. The header now declares `rt_exec_trace_enabled()` itself.
- Main-session gates passed: `make runtime-v2-check`, `make check`, and
  `TestMTNetWaiterWakeupLatency`.
- Native net benchmark passed with current working-tree binary and wrote
  ignored report `build/benchmarks/runtime-v2-task14-native-net.md`. The
  report had 24 runtime trace rows, 30 columns, all required trace columns,
  zero legacy poll-build counter violations, and
  `poll rebuilds == net poll calls` in every row.
- Review subagent approved after one P2 debt-doc issue was fixed.
  RV2-DEBT-004 now records Task 14 as partial progress and leaves remaining
  `rt_net.c` LOC debt open for a future net wake-fd, poll-construction, or
  net-handle lifecycle split.
- Sentrux root/runtime/runtime-native gates are recorded in `04-evidence.md`
  after final documentation updates. Root improved `6194 -> 6196`; scoped
  runtime/native rules passed but quality signals decreased
  `5230 -> 5214` and `5175 -> 5158`, recorded as an accepted split tradeoff
  while RV2-DEBT-004 remains open.

## Epic 4 Closeout Handoff

- Scope completed: persistent fd registry and net lifecycle ownership under
  the existing `N=1` boundary.
- Polling now uses fd-registry snapshots instead of rebuilding fd rows from the
  full waiter store. Closeout trace validation recorded 24 runtime trace rows,
  30 columns, no missing required fields, and zero violations.
- Stable fd-registry proofs run through `make runtime-v2-check` via
  `runtime-v2-fd-registry-check`. Timing-heavy fd-registry probes remain
  local-only and are not CI green gates.
- Fresh closeout gates passed: `make c-check`, `make cppcheck`,
  `make runtime-v2-fd-registry-check`, `make runtime-v2-check`,
  `TestMTNetWaiterWakeupLatency`, `make check`, and `git diff --check`.
- Fresh closeout Sentrux scans passed rules with zero violations: root quality
  `6191`, `runtime/` quality `5240`, and `runtime/native` quality `5244`.
  Root quality ended below kickoff (`6198 -> 6191`), while the scoped runtime
  signals improved (`5195 -> 5240`, `5159 -> 5244`).
- Final line counts: `rt_async_state.c` 1727, `rt_net.c` 904,
  `rt_net_trace.c` 128, `rt_net_trace.h` 73, `rt_fd_registry.c` 409,
  `rt_fd_registry.h` 113, and `rt_async_internal.h` 495.
- Remaining non-green debt: RV2-DEBT-001, RV2-DEBT-002, RV2-DEBT-003,
  RV2-DEBT-004, RV2-DEBT-010, local-only timing-heavy fd-registry probes, and
  normal lifecycle wiring for `rt_executor_request_shutdown`.
- Next epic: heap and hot accounting ownership. Keep `N>1` accept
  distribution, crossing syntax, backend I/O migration, and VM/native/LLVM
  test-matrix rewrite out until their owning epics.

## Epic 5 Draft Handoff

- Created `docs/runtime-v2-epics/05-per-shard-heap-accounting.md` as the next
  standalone epic document.
- Created brief task documents under `docs/runtime-v2-epics/05-tasks/` and the
  initial `05-evidence.md` ledger.
- Scope: remove the four global heap counter cache lines from the hot
  `rt_alloc`/`rt_free`/`rt_realloc` path while keeping `malloc/free`, public
  allocation ABI, `HeapStats` layout, and current `rt_heap_stats()` snapshot
  behavior.
- Important boundary: current `N=1` still has multiple worker threads, so
  "per-shard" accounting must not become one contended shard counter block. The
  epic allows shard-owned per-worker or per-thread accounting cells, aggregated
  by `rt_heap_stats()` on read.
- Excluded: slab/bump pools, owner-shard page or span metadata, remote-free
  queues, cross-shard frees, `N>1` accept ownership, compiler crossing syntax,
  backend I/O migration, broad VM/native/LLVM test-matrix rewrite, and unrelated
  LOC cleanup.
- Debt policy: only debt created by or brought into Epic 5 scope can block Epic
  5. New allocator/accounting debt cannot stay hidden in notes; it must be
  closed or added to `DEBT.md` with an owner and close condition before closeout.

## Future Language Syntax Gate

- Before Epic 7 or any earlier task touches Surge syntax, parser rules, semantic
  checks for new syntax, public examples, or keywords, stop and run a dedicated
  syntax discussion with the user.
- Current names in `docs/RUNTIME_V2.md`, including `far`, `submit_to`, and
  `shard-movable`, are contract placeholders. They are not final language
  keyword choices.
- The syntax review must consider keyword count, readability, and concise source
  spelling before implementation starts.

## Epic 6 Task Cutting

- Epic 6 review feedback (lock model calibration, `SURGE_SHARDS`/`SURGE_THREADS`
  interaction, per-shard poller/wake with an explicit Phase 3/Phase 4 line,
  stays-global-compat invariant for non-net primitives, the accept-loop
  ownership conflict, N=1 static-pin updates, parked-with-work invariant,
  `SO_REUSEPORT` listener-group close semantics, and `SURGE_SHARDS=1` no-steal
  vacuity) was folded into
  `docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md` before
  task cutting, in an "Epic 6 Boundary Decisions" section plus updates to
  Scope, the Accept Ownership Contract, and the Performance Contract.
- All 15 tasks from the epic's Brief Task List are now expanded into full
  documents under `docs/runtime-v2-epics/06-tasks/`, each with Context (with
  exact current-code `file:line` citations so a task stands on its own),
  Goal, Why This Task Exists, Scope, Out Of Scope, Approach/Steps, Files,
  Skills & Working Practice, Checks, Definition Of Done, and Evidence To
  Record. `06-tasks/README.md` indexes them with dependencies and
  parallelization notes.
- Key structural facts recorded during task-cutting, verified directly
  against the current checkout, that later tasks should cite instead of
  re-deriving:
  - `struct rt_shard` (`rt_async_internal.h:150-160`) already carries
    `scheduler`, `heap_accounting`, `net_poll_scratch`, `fd_registry`,
    `channel_blocking_compat`, and `waiter_store` as direct per-shard
    members. Epic 4/5 already made these fields shard-shaped; only
    `shards[0]` is ever populated today. Task 6 mainly needs to make the
    array dynamic and actually initialize `shards[1..N-1]`, not invent new
    per-shard storage.
  - `rt_runtime_shard0()` (`rt_runtime.c:50-55`) is the one compatibility
    accessor every other accessor (`rt_executor_scheduler`,
    `rt_executor_net_poll_scratch`, `rt_executor_channel_blocking_compat`,
    `rt_executor_waiter_store`, `rt_executor_fd_registry`) routes through.
    Task 6/Task 5 must add a shard-indexed sibling for net-owned accessors
    only; non-net accessors keep routing through shard 0 by design.
  - `rt_task` (`rt_async_internal.h:167-202`) has no shard/owner field at
    all today — Task 7 must add it.
  - `NetListener`/`NetConn` (`rt_net.c:45-53`) are `{int fd; bool closed;}` —
    two fields, no shard tag, no generation. Task 8 extends these; this is
    also the natural point to decide `RV2-DEBT-010` (copied-handle
    generation) one way or the other.
  - The wake pipe (`rt_net.c:67-68`, `net_poll_wake_read_fd`/
    `net_poll_wake_write_fd`) is a process-global static, distinct from the
    already-per-shard `net_poll_scratch` buffer. Task 10 replaces it with
    per-shard wake state, explicitly stopping short of the Phase 4
    `eventfd`/`PARKED`/credit protocol.
  - `rt_shard_scheduler_init` (`rt_runtime.c:165-187`) already takes an
    explicit `worker_count` and needs no change to support more shards —
    Task 6/7 just need to call it once per shard.
  - The `Makefile`'s `runtime-v2-check` chain (`runtime-v2-heap-check`,
    `runtime-v2-waiter-check`, `runtime-v2-fd-registry-check`,
    lines ~86-115) is the pattern Task 13's new `runtime-v2-accept-check`
    must follow exactly.
- Execution has not started. Next step is Task 1 (kickoff baseline and
  Sentrux), which must re-verify every `file:line` citation above before
  Task 2 begins, since drift between this note and the real checkout is
  possible.

## Epic 6 Task Cutting — Corrections Before Execution Start

Three corrections applied to the freshly-cut Epic 6 tasks after a review
pass, before any task execution began:

- `06-tasks/12-trace-counters-and-benchmark-evidence.md` touches
  `runtime/native/rt_async_state.c`/`rt_net.c` for new trace counters, which
  makes it a runtime-code task under Global Rule 3 — it was missing Sentrux
  root/`runtime/`/`runtime/native/` scans from its Checks. Added to Checks,
  Definition Of Done, and Evidence To Record.
- `06-tasks/15-epic-closeout-and-static-gates.md` previously lumped "lock
  splitting" into "Phase 4+" deferred work. Per the roadmap in `README.md`,
  splitting `rt_executor.lock` and moving remaining global-compatibility
  primitives to shard-owned state is Epic 7's scope specifically, not
  Phase 4. Reworded to separate: Epic 7 owns lock splitting; Phase 4+
  (owned by later epics 8/9/10 per the roadmap) owns cross-shard messaging,
  `far`/`submit_to`, remote-free, and `io_uring`.
- `06-tasks/06-runtime-shard-array-and-config.md` and
  `06-tasks/07-per-shard-scheduler-placement.md` had an unclear boundary
  around worker OS threads. Verified in code
  (`rt_async_state.c:216-220,278-319`): `rt_shard_scheduler_init` (called by
  Task 6) only sizes `local_queues`/`worker_count`; it never touches
  `worker_ctxs` or calls `pthread_create`. `rt_start_workers` is the
  function that actually allocates `worker_ctxs` and spawns OS threads, and
  it is fully executor-global today (one `ex->workers` array, one I/O
  thread, one worker loop; `rt_worker_ctx` has no shard field). Resolved:
  Task 6 owns shard *structure* initialization only and explicitly does not
  spawn threads or touch `worker_ctxs` beyond shard 0. Task 7 owns making
  `rt_start_workers` shard-aware — real per-shard worker threads, `worker_ctxs`
  allocated per shard, each `rt_worker_ctx` tagged with its shard — because
  Task 7 is also where the owner-shard task metadata that binding needs to
  be meaningful gets added. Both task documents now cross-reference this
  boundary explicitly (Task 7 has a dedicated "Boundary With Task 6"
  section) so it cannot be read either way.

## Epic 6 Task 01 Handoff

- Scope completed: kickoff baseline before Epic 6 implementation. Docs-only;
  no runtime code, tests, task documents, `Makefile`, CI, Sentrux rules, or
  benchmark scripts changed.
- Start commit: `9e1de4a0 docs(runtime): expand epic 6 tasks`; branch
  `codex/runtime-net-scheduler-refactor`; clean tree at start.
- Baseline line counts: `rt_async_internal.h` 499, `rt_runtime.c` 202,
  `rt_net.c` 904, `rt_fd_registry.h` 113, `rt_fd_registry.c` 409,
  `rt_async_state.c` 1727, `rt_async_task.c` 768,
  `runtime_v2_skeleton_static_test.go` 61,
  `runtime_v2_fd_registry_static_test.go` 426, `mt_correctness_test.go` 1351,
  and `mt_executor_test.go` 1511.
- Sentrux MCP was available and used; CLI fallback was not needed. Required CLI
  checks also passed: `sentrux check .` quality `6190`, `sentrux check runtime`
  quality `5279`, and `sentrux check runtime/native` quality `5318`. MCP
  `check_rules` passed for all three paths.
- Structural kickoff facts are recorded in `06-evidence.md`: fixed
  `RT_RUNTIME_SHARD_COUNT 1U`, fixed `rt_runtime.shards[...]`, shard-0
  compatibility accessors, global `rt_executor`, no task owner-shard metadata,
  no net-handle owner metadata, global wake pipe, `SO_REUSEADDR` listener
  setup, shard-shaped fd registry storage still resolved through shard 0, and
  `SURGE_THREADS` as the only worker-count env knob.
- Baseline checks: `git diff --check` passed before and after docs edits;
  `git diff --no-index --check /dev/null docs/runtime-v2-epics/06-evidence.md`
  printed no whitespace errors for the new untracked evidence file; `make
  check` passed with normal `SURGE_SKIP_TIMEOUT_TESTS=1`. The first
  `make runtime-v2-check` attempt failed before any docs edit because
  `TestMTBlockingChannelHelpersAllowTimersToAdvance` timed out after 30s, then
  immediate `timeout 300s make runtime-v2-check` passed the full Runtime V2
  chain. Treat the current baseline gate as green on rerun, with the first
  timeout recorded as `RV2-DEBT-002` flake evidence.
- Accepted debt relevant to Epic 6 remains open and recorded in
  `06-evidence.md`: `RV2-DEBT-001`, `002`, `003`, `004`, `005`, `006`, `007`,
  `010`, `011`, and `012`.
- Next: Task 2 should create `06-accept-ownership-dependency-map.md` and map
  accept/readiness/close/cancellation/shutdown paths from the frozen Task 1
  starting facts. It should not re-litigate the listener model; Task 3 owns the
  proving spike, and both tasks must reconcile before Tasks 4/5 finalize tests.

## Epic 6 Task 02 Handoff

- Scope completed: docs-only accept ownership dependency map in
  `06-accept-ownership-dependency-map.md`. No runtime code, tests, task docs,
  `Makefile`, CI, Sentrux rules, or benchmark scripts changed.
- Classification boundary is now explicit: net accept/readiness/close/shutdown
  ownership moves to the owning shard in Epic 6; non-net waiters and primitives
  remain global compatibility under `ex->lock`; lock sharding remains Epic 7;
  Phase 4 inbound messaging/eventfd/credits/PARKED and syntax are later.
- Current net-owned migration surfaces: native `NetListener`/`NetConn` metadata,
  `rt_executor_fd_registry(_const)` callers in net wait/close/poll/completion/
  shutdown paths, `rt_executor_net_poll_scratch`, process-global wake pipe,
  `begin_net_poll`/`net_polling`, and shutdown drain/wake in
  `runtime/native/rt_shutdown.c`.
- `runtime/native/rt_shutdown.c` exists in this checkout. Its
  `rt_executor_drain_shutdown_net_waiters` and `rt_executor_request_shutdown`
  paths currently drain `rt_executor_fd_registry(ex)` and request wake through
  the process-global `rt_net_wake_poll`; Tasks 10/11 must iterate/wake every
  shard-owned net registry/poller.
- No new unowned Epic 6 gap was found. Open reconciliation points are assigned:
  Task 3 decides listener model and handler placement; Task 6 does structural
  shard array/config; Task 7 handles connection-task placement/no-steal; Task 8
  attaches owner metadata; Tasks 10/11 migrate poller/wake and net lifecycle.
- First safe implementation boundary chosen: Task 6 can add
  `RT_RUNTIME_MAX_SHARDS` plus runtime `shard_count`, `SURGE_SHARDS` parsing
  and conflict validation, and per-shard container initialization under the
  preserved global lock. It should not silently make `rt_start_workers`
  shard-aware unless its approved task scope is expanded; placement belongs to
  Task 7/10.
- Task 3 must not use this map as a listener-model decision. It must answer how
  one public listener handle maps to N internal accept owners, how handler tasks
  reach the owner shard without syntax changes, and how listener-group close
  semantics are represented.
- Task 2 was committed by the subagent before the intended review handoff.
  Post-facto main-agent audit checked the map against the task DoD and sampled
  source citations for shutdown, fd-registry completion/drain, poll input,
  listen/accept, steal branches, and VM handle flow. No content correction was
  required, but the process exception is recorded in `06-evidence.md`.

## Epic 6 Task 03 Start

- Started Listener Model Proving Spike after Task 2 commit
  `8f1547f4`. The Rule-1 spike record is
  `06-listener-model-proving-spike.md`; it was written before scratch code.
- Approved hypothesis: compare per-shard `SO_REUSEPORT` listener group against
  a single-acceptor explicit handoff fallback. The selected model must answer
  internal accept work placement, handler owner-shard placement, ABI stability,
  no-new-syntax, and low-connection skew.
- Allowed scratch surface only:
  `build/tmp/runtime-v2-epic6/listener_model_probe.c` and its compiled binary.
  No durable runtime/native, VM, syntax, stdlib, CI, or Makefile edits in this
  task.

## Epic 6 Task 03 Handoff

- Decision: Epic 6 targets per-shard `SO_REUSEPORT` listener groups. The proof
  row with 4 shards and 1024 clients accepted on all four listener members:
  `0:241,1:245,2:265,3:273`.
- Runtime representation for later tasks: one public `TcpListener` owns an
  internal listener group with one member fd per shard. The member fd is
  registered in the owner shard's fd registry. Accept readiness from member `k`
  resumes/enqueues the waiting accept task on shard `k`; `rt_net_accept()`
  accepts from that winning member and returns an owner-tagged `NetConn`.
- Handler placement answer: no new syntax. Code that awaits
  `net.accept(&listener)` resumes on the accepted connection owner shard, so a
  local `spawn` from that continuation stays owner-local. Task 4/5 should turn
  this into tests/static gates before Task 7/9 implement it.
- Rejected target path: single acceptor plus explicit handoff. It can assign
  target shards only if owner placement happens before fd-registry exposure;
  moving a registered/exposed connection is the migration control plane and is
  outside Epic 6. Do not retry fallback in Task 9 without a new blocker for
  `SO_REUSEPORT`.
- Low-count skew is expected. The 1-client row activated one shard; 8/32
  happened to activate all four on this machine, but Task 12 must judge
  distribution using a high-load row.
- New debt recorded: `RV2-DEBT-013`. Current `stdlib/http/server.sg` sends raw
  `TcpConn.__opaque` through a channel to worker tasks. Under `SURGE_SHARDS>1`,
  those workers may not be owner-shard tasks; Task 7/9/13 must make this
  visible through guards/tests or later owner-local stdlib design.

## Epic 6 Subagent Control Correction

- Process issue found during Task 2/3: subagents created commits
  `8f1547f4 docs(runtime): map epic 6 accept ownership dependencies` and
  `47686287 docs(runtime): prove epic 6 listener model` directly. Task 2 had
  implementation approval but no review/commit approval; Task 3 had plan-only
  approval but no implementation or commit approval.
- Main-session audit result: tracked working tree contained no Task 4/5 files
  and no runtime code changes. Task 2/3 commits are docs-only except the
  intended `DEBT.md` entry `RV2-DEBT-013`; scratch probe code stays ignored
  under `build/tmp/runtime-v2-epic6/`.
- Task 3 proof was rerun from the current checkout after the issue was found:
  compile passed, `SO_REUSEPORT` rows for 1/8/32/1024 clients passed, fallback
  handoff rows for 32/1024 clients passed, and `git diff --check` passed.
- New operating rule for the rest of Epic 6: no subagent may run `git add`,
  `git commit`, or advance task status. Subagents may return plans, reports, or
  leave explicitly approved unstaged edits in a disjoint write set. The main
  session alone stages, commits, updates task status, and starts the next task.
- The main session must check `git status -sb`, `git diff --name-status`, and
  open-agent status after every subagent wait/notification before approving any
  next task. Shared evidence files (`06-evidence.md`, `NOTES.md`, `DEBT.md`)
  are single-writer surfaces; parallel agents must not edit them directly.

## Epic 6 Task 04 Handoff

- Scope completed: behavior contracts for multishard accept ownership. Runtime
  implementation is unchanged. `STATS.md` was updated by the commit hook after
  adding the new test files.
- Added default-green compatibility test
  `TestRuntimeV2AcceptShardOneNativeNetCompatibility` in
  `runtime_v2_accept_compat_test.go`. It builds a native LLVM echo fixture,
  runs with `SURGE_SHARDS=1`, accepts one TCP client, reads four pings, writes
  four `PONG\n` responses, closes the connection, and exits zero.
- Added pending accept contract tests in
  `runtime_v2_accept_contract_test.go` for `SURGE_SHARDS` initialization,
  invalid shard config diagnostics, `SURGE_SHARDS`/`SURGE_THREADS` conflict,
  accept owner distribution, owner-shard registry/close/cancel/shutdown trace
  proof, non-owner connection visibility, and listener-group close proof.
- Independent checks:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestRuntimeV2AcceptShardOneNativeNetCompatibility$' -count=1 -parallel=1 -p=1 -v --timeout 90s`
  passed; `go test ./internal/vm -run '^TestRuntimeV2Accept' -count=1
  -parallel=1 -p=1 -v --timeout 90s` passed; `SURGE_SKIP_TIMEOUT_TESTS=0 go
  test ./internal/vm -run '^TestMTNetWaiterWakeupLatency$' -count=1
  -parallel=1 -p=1 -v --timeout 90s` passed; `go build ./...` passed;
  `git diff --check` passed.
- Expected-red contract check:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags
  runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Accept' -count=1
  -parallel=1 -p=1 -v --timeout 180s` failed in the intended shape: missing
  `runtime_shards`, invalid/conflicting env values not rejected, missing
  accept-owner `TRACE_NET` fields, existing net-owned shard-0 accessors, and
  missing `RT_RUNTIME_MAX_SHARDS`. No crash or hang was observed.
- Handoff: Tasks 6/7 must make config and worker/shard placement meaningful;
  Tasks 8/9/10/11 must make owner metadata, accept distribution, poller wake,
  and lifecycle ownership real; Task 12 must expose the trace fields used by
  the pending contract; Task 13 should promote the tagged gate only after the
  owner implementation is green.

## Epic 6 Task 05 Handoff

- Scope completed: static gates for dynamic shard storage and net ownership
  shortcut prevention. Runtime implementation is unchanged.
- Existing pending static pins in `runtime_v2_skeleton_static_test.go` and
  `runtime_v2_fd_registry_static_test.go` no longer hard-require
  `RT_RUNTIME_SHARD_COUNT == 1`; they accept the future
  `RT_RUNTIME_MAX_SHARDS` name or the current `RT_RUNTIME_SHARD_COUNT` fallback
  and assert the static storage limit is positive.
- Added `runtime_v2_accept_static_test.go` under `runtime_v2_pending`.
  `TestRuntimeV2AcceptNetOwnershipNoShard0Shortcut` mechanically scans the
  current net-owned accessor bodies plus named net-owned functions in
  `rt_net.c`, `rt_fd_registry.c`, `rt_shutdown.c`, `rt_async_waiter.c`, and
  net-poller functions in `rt_async_state.c`. It fails if any of those paths
  route through `rt_runtime_shard0()`/`shards[0]`. The documented global-compat
  exemptions are scheduler, channel blocking compat, and generic waiter-store
  accessors. `TestRuntimeV2AcceptDynamicShardArrayShape` pins the future
  `RT_RUNTIME_MAX_SHARDS` plus bounded runtime `shard_count` contract.
- Independent checks:
  `go build ./...` passed; `go test -tags runtime_v2_pending ./internal/vm
  -run 'TestRuntimeV2Skeleton|TestRuntimeV2FDRegistryStatic' -count=1
  -parallel=1 -p=1 -v --timeout 90s` passed; `git diff --check` passed.
- Expected-red static checks:
  `go test -tags runtime_v2_pending ./internal/vm -run
  'TestRuntimeV2Accept(NetOwnershipNoShard0Shortcut|DynamicShardArrayShape)'
  -count=1 -parallel=1 -p=1 -v --timeout 90s` failed as intended because
  `rt_executor_net_poll_scratch`, `rt_executor_fd_registry`, and
  `rt_executor_fd_registry_const` still route through shard 0, and
  `RT_RUNTIME_MAX_SHARDS` is not defined yet.
- Process correction: the original Task 5 check list used untagged `go test`
  commands for already tagged static files. That produced `no tests to run`;
  the task doc now records the correct tagged commands so Task 6/13 do not
  inherit a false green.
- Independent review follow-up: the first static gate was too narrow because
  it only inspected the three `rt_runtime.c` accessors. The committed follow-up
  broadened the gate before Task 6 continued and also removed a future
  `-Wall -Wextra -Werror` false-red from the dynamic-shape C snippet.

## Epic 6 LOC Checker Handoff

- Tooling correction: `check_file_sizes.sh` now reports effective source LOC
  for `.go`, `.c`, and `.h` files. Blank lines and comment-only `//` or
  `/* ... */` lines do not count; code-bearing lines with trailing comments
  still count. This avoids pressuring runtime work to delete useful invariants
  or design comments just to satisfy the line gate.
- Added `./check_file_sizes.sh --self-test`, covering Go/C/H comment-only
  lines, block comments, trailing comments, and comment tokens inside string
  literals. The self-test passed locally.
- `./check_file_sizes.sh` passed on the current Epic 6 worktree after the
  metric change. Example current effective counts: `rt_async_internal.h` 424,
  `rt_async_state.c` 1573 legacy-ok, and `rt_net.c` 843 legacy-ok.
- `RV2-DEBT-014` was closed immediately in `DEBT.md`; it should not be carried
  as future debt unless a new parser edge case is found.

## Epic 6 Task 06 Handoff

- Scope completed: Runtime V2 now has fixed bounded storage
  `RT_RUNTIME_MAX_SHARDS=64` plus runtime `shard_count`. `SURGE_SHARDS`
  defaults to `1`, rejects invalid values, and drives structural shard
  initialization. `SURGE_THREADS` remains the old compatibility worker-count
  control for `SURGE_SHARDS=1`; under `SURGE_SHARDS>1` it must be unset or
  equal to `SURGE_SHARDS`.
- Task 6 stayed structural. It initializes per-shard containers and scheduler
  structures, but it does not make `rt_start_workers` shard-aware, does not add
  worker/task owner placement, and does not distribute accepted connections.
  Task 7 owns actual worker-to-shard binding.
- New config surface lives in `runtime/native/rt_runtime_config.c/h`. The
  startup path exits with an explicit diagnostic for invalid config because
  `pthread_once` cannot propagate a recoverable status to callers.
- New accessor names to reuse in Tasks 7-11:
  `rt_runtime_shard`, `rt_runtime_shard_const`,
  `rt_executor_net_poll_scratch_for_shard`, and
  `rt_executor_fd_registry_for_shard`. Current net-owned call sites use
  `_for_shard(ex, 0)` as explicit compatibility placeholders; replacing those
  literals with real owner-shard values is future task work.
- `TRACE_EXEC` now includes `runtime_shards=<count>`. This is only a config
  proof field; the remaining full accept contract still expects future
  `TRACE_NET` owner/distribution/lifecycle fields from Tasks 8-12.
- Independent review found and closed one P2: default blocking pool size briefly
  derived from `shard_count` under `SURGE_SHARDS>1`. It now derives from
  `legacy_worker_threads`, so `SURGE_SHARDS` does not silently create extra
  blocking OS threads in the structural task.
- Final gates passed: `make c-check`, `make cppcheck`, focused Task 6 config
  tests, Task 5 static gates, `make runtime-v2-check`, `make check`,
  `git diff --check`, `sentrux check .` quality 6188, `sentrux check runtime`
  quality 5301, and `sentrux check runtime/native` quality 5340.
- Full pending accept remains expected-red only for downstream fields:
  `accept_owner_active_shards`, `fd_owner_registry_rows`,
  `close_owner_wakeups`, `cancel_owner_cleanup`,
  `shutdown_poller_wakeups`, `non_owner_conn_denied`, and
  `listener_group_members_closed`.

## Epic 6 Task 07 Handoff

- Scope completed: per-shard scheduler placement. `rt_start_workers` now
  allocates worker contexts and starts Tier 1 workers per configured shard;
  `SURGE_SHARDS>1` uses one worker per shard, while `SURGE_SHARDS=1` preserves
  existing MT stealing/seeded compatibility.
- Task placement metadata lives on `rt_task`: `placement_class`,
  `owner_shard_valid`, and `owner_shard_id`. `TASK_PLACEMENT_CONNECTION`
  marks Tier 1 connection-owned tasks; generic/unowned tasks keep compatibility
  scheduling.
- Task 8/9 must use `rt_task_set_placement(..., owner_shard,
  TASK_PLACEMENT_CONNECTION)` or a narrow wrapper when attaching accepted
  connection handler tasks to owner shards.
- Invalid owner shard placement now fails closed with
  `async: invalid task owner shard`; do not reintroduce shard-0 fallback for
  owner-marked work.
- No-steal proof: `TestRuntimeV2SchedulerPlacementNoStealWorkerPath` keeps
  shard 1's worker busy, enqueues a connection-owned target task on shard 1,
  verifies shard 0 does not run it while idle, then releases shard 1 and checks
  `SCHED_TRACE steal=0`.
- Parked-with-work proof closed for the scheduler-ready-queue form only: the
  worker sleep path asserts `rt_debug_assert_no_parked_with_work(ex,
  ctx->shard_id)` before `pthread_cond_wait`, and the harness deliberately
  panics on queued shard-local work. Per-shard poller/fd-ready wake proof
  remains Task 10 scope.
- Trace snapshot fields `worker_count`, `running`, `inject_len`,
  `local_total`, and `local_max` now aggregate across shards.
- Independent review initially found missing adversarial no-steal proof,
  invalid-owner shard0 fallback, and narrow parked-with-work coverage. All
  findings were fixed and then closed by re-review.
- Final gates passed: `make c-check`, `make cppcheck`, focused MT
  steal/seeded tests, focused scheduler-placement tests, Task 5 static gates,
  `make runtime-v2-check`, `make check`, `git diff --check`,
  `./check_file_sizes.sh --self-test`, `./check_file_sizes.sh`,
  `sentrux check .` quality 6185, `sentrux check runtime` quality 5332, and
  `sentrux check runtime/native` quality 5353.

## Epic 6 Task 08 Handoff

- Scope completed: listener/connection owner metadata and owner-first net
  close lifecycle helpers. `NetListener` and `NetConn` now live in
  `rt_net_handles.h`; `NetListener` has a single/group/fallback discriminator,
  member array, compatibility fd/owner fields, and per-member `(fd,
  owner_shard_id, closed)`. `NetConn` has `owner_shard_valid` and
  `owner_shard_id`.
- New helper split: `rt_net_handles.c` owns allocation/member selection,
  `rt_net_lifecycle.c` owns explicit-status close paths, and
  `rt_net_listener_socket.c` owns listener socket setup including optional
  `SO_REUSEPORT`. This kept `rt_net.c` at 844 effective LOC under the existing
  904 legacy ceiling.
- Important boundary: public `rt_net_listen` still creates one live listener
  member even under `SURGE_SHARDS>1`. The group-capable representation exists,
  but real N-member `SO_REUSEPORT` activation is Task 9. Do not flip that
  switch before group wait/accept routing exists: current task state has a
  single `park_key`, so waiting on only member 0 while Linux routes clients to
  members 1..N can hang clients.
- Listener close now routes through `rt_net_close_listener_members`, which
  loops over represented members and calls `rt_net_close_fd_on_owner` with each
  member's owner shard. With current public listen this is one member; owner-
  local waiter-store migration and per-shard poller wake remain Tasks 10/11.
- `RV2-DEBT-010` remains open by explicit decision. Owner metadata is not a
  copied-handle generation guard. Current fd-registry generations protect poll
  snapshots/waiter completions only; closing the debt needs stable handle id or
  registry-generation validation before direct fd operations.
- Static gate update: `runtime_v2_accept_static_test.go` no longer expects the
  deleted `close_net_fd_slot`; it checks `rt_net_lifecycle.c` owner-first
  helpers instead.
- CI/gate update: `runtime-v2-accept-check` was added to `make
  runtime-v2-check` and currently covers `TestRuntimeV2NetMetadata*` plus the
  accept static shape tests.
- Review found and closed: N-member listener hang risk, stale static gate, and
  missing debt/gate documentation. The remaining full accept contract is still
  expected-red only for downstream `TRACE_NET` owner/distribution/lifecycle
  fields.
- Final gates passed: `make c-check`, `make cppcheck`,
  `make runtime-v2-accept-check`, `make runtime-v2-check`, `make check`,
  `git diff --check`, `./check_file_sizes.sh --self-test`,
  `./check_file_sizes.sh`, `sentrux check .` quality 6185,
  `sentrux check runtime` quality 5320, and `sentrux check runtime/native`
  quality 5338. A transient full `runtime-v2-check` timeout in
  `TestMTBlockingChannelHelpersAllowTimersToAdvance` did not reproduce in the
  final full rerun; an accidental overlapping VM command reproduced the known
  `RV2-DEBT-011` artifact race, so future checks should stay sequential.

## Epic 6 Task 09 Handoff

- Scope completed: real accept distribution. `rt_net_listen` now creates one
  `SO_REUSEPORT` listener member per configured shard under `SURGE_SHARDS>1`.
  The public `TcpListener` remains one Surge value; internally it resolves to a
  canonical listener group.
- `rt_net_wait_accept` registers the waiting task against every live listener
  member fd. The first ready member records `net_ready_accept_fd` and
  `net_ready_accept_owner_shard` on the task, places the continuation as
  `TASK_PLACEMENT_CONNECTION`, and clears sibling accept wait keys immediately
  so a later ready member cannot overwrite the winner before `rt_net_accept`.
- `rt_fd_registry_register_open_fd` adds durable open-fd rows. Listener members,
  outbound connects, and accepted connections now enter the owner shard's
  fd registry before interests are attached. Registered zero-interest rows stay
  until close; compatibility interest-only rows still disappear on last detach.
- Poller boundary: Task 9 uses a single aggregate poll over all shard
  registries and stores `owner_shard_id` in each poll snapshot. This is a
  deliberate bridge, not Task 10's per-shard poller/wake-fd ownership.
- Lifecycle boundary: Task 9 made shutdown drain iterate owner shard registries
  where aggregate polling needed owner-aware completion, but Task 10/11 still
  own real per-shard poller wake, close/cancel/shutdown lifecycle proof, and
  non-net waiter unaffectedness.
- Non-owner guard decision: no guard was added in Task 9. `RV2-DEBT-013`
  remains open because copied/raw `TcpConn.__opaque` operations need a stable
  owner/generation guard before denial can be made safe. Newly added Task 9
  paths do not route missing owner rows through shard 0.
- Test correction: the duplicate-read cancellation fd-registry fixture now
  forces `SURGE_THREADS=1`; its previous MT timing allowed data to wake both
  duplicate readers before parent cancellation, which tested scheduler timing
  rather than registry cancellation semantics.
- Independent review found two P1 issues and both were fixed: listener registry
  capacity now grows under its mutex, and accept readiness now clears sibling
  wait keys at winner completion. Static guards pin both fixes.
- Final gates passed: focused FD registry suite, full accept suite,
  `make runtime-v2-accept-check`, `make c-check`, `make cppcheck`,
  `git diff --check`, `./check_file_sizes.sh`, `make runtime-v2-check`,
  `make check`, and Sentrux root/runtime/native scans (`6185`, `5330`,
  `5450`).

## Epic 6 Task 10 Handoff

- Scope completed: per-shard net poller/wake ownership. Epic 6 chose
  shard-worker-owned polling for `SURGE_SHARDS>1`, matching Task 7's one
  Tier 1 worker per shard. The global I/O thread remains a single-shard/timer
  compatibility path and is gated out of multishard net polling.
- New module: `runtime/native/rt_net_poller.c` owns per-shard wake pipes,
  effective wake-count returns, wake draining, shard-local waiter detection,
  and shard-local poll ownership helpers.
- `rt_shard` now owns `net_poll_wake` and `net_polling`; `rt_executor` no
  longer owns a process-global net polling flag.
- `poll_net_waiters_on_shard` snapshots only the target shard's registry, uses
  only the target shard's scratch storage, and drains only the target shard's
  wake pipe.
- Wake routing is owner-shard explicit for park commit, waiter
  attach/detach notification, close wake, and shutdown.
- Shutdown trace accounting is now honest: `rt_net_wake_poll_all_shards`
  returns the sum of effective per-shard wakes, and
  `shutdown_poller_wakeups` records that returned value.
- Added `TestRuntimeV2NetPollerPerShardWakeBehavior`, a C behavior harness
  using production `rt_net_poller.c` and real nonblocking pipes. It proves
  shard-1 wake does not wake shard 0, all-shards wake wakes both, and
  shard-local interest detection does not treat registered zero-interest rows
  as pollable work.
- No Phase 4 primitives were added: no eventfd protocol, no inbound queues, no
  credits, no seq-cst `PARKED`, no wake elision, and no cross-shard messaging.
- Independent review found and then re-closed three issues: shutdown wake
  trace was initially fake, tests were initially too string-heavy, and
  `rt_async_state.c` initially exceeded its LOC ceiling. The fixes extracted
  `rt_net_poller.c`, added the behavior harness, and made wake APIs return
  effective counts.
- Final gates passed: focused NetPoller/FDRegistry/Accept tests,
  `make c-check`, `make cppcheck`, `./check_file_sizes.sh`,
  `git diff --check`, `make runtime-v2-check`, `make check`, and Sentrux
  root/runtime/native scans (`6185`, `5326`, `5465`).

## Epic 6 Task 11 Handoff

- Scope completed: multishard net lifecycle now resolves net waiter rows
  through owner shard waiter stores. Non-net waiters still use the global
  shard-0 compatibility store under `ex->lock`.
- Added `rt_executor_waiter_store_for_shard` and const twin. The old
  `rt_executor_waiter_store` remains explicit compatibility for non-net
  channel/join/scope/timer/blocking paths.
- Net `add_waiter`, `pop_waiter`, and
  `rt_executor_wake_net_waiters_for_key_on_owner` use the fd owner shard store.
  Net `remove_waiter` scans all shard stores for stale cleanup, but only
  detaches registry interest from the current owner shard when the removed row
  came from that owner store.
- Removed the hardcoded owner-0 shutdown drain wrapper. Only
  `rt_fd_registry_drain_shutdown_net_waiters_locked_on_owner` remains.
- Added `runtime/native/rt_async_trace_waiters.c`; `TRACE_EXEC_SNAPSHOT`
  waiter counters now aggregate all shard waiter stores, so `waiters_net` does
  not silently become shard-0-only after owner-local migration.
- Added owner-local behavior proof in
  `TestRuntimeV2OwnerLocalNetWaiterBehavior`. The C harness includes
  production `rt_fd_registry.c` and `rt_async_waiter.c`, proves net add/remove,
  close wake, shutdown drain, non-net join staying global, and the fd-reuse
  regression where stale shard-1 cleanup must not clear a newly registered
  shard-0 fd interest.
- `runtime-v2-waiter-check` now carries the owner-local net waiter behavior
  and trace aggregation tests, so `make runtime-v2-check` covers this path.
- Independent review initially found a P1 fd-reuse stale cleanup/detach hole
  plus P2 gate/trace gaps. All were fixed. Targeted re-review found no
  blockers; operational note was to include the new
  `rt_async_trace_waiters.c` file in the commit.
- `RV2-DEBT-002` remains open and unchanged. The known-red
  `TestMTBlockingChannelHelpersDoNotParkWorkers` and
  `TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit` timeouts
  reproduced at clean Task 10 commit `0d206ed2`; `AllowTimersToAdvance` passed
  standalone and in `make runtime-v2-check`.
- Native net benchmark smoke passed with a freshly built `/tmp` Surge binary:
  64 direct/seq requests, avg 186.62 us/op, p50 147.26 us, p95 350.56 us.
  Full performance evidence and scaling explanation remain Task 12.
- Final gates passed: focused owner-local/accept static tests,
  `make runtime-v2-waiter-check`, `make runtime-v2-check`, `make c-check`,
  `make cppcheck`, `git diff --check`, `./check_file_sizes.sh`,
  `./check_file_sizes.sh --self-test`, `make check`, Sentrux
  root/runtime/native scans (`6184`, `5329`, `5466`), and targeted net/non-net
  liveness probes listed in `06-evidence.md`.

## Epic 6 Task 12 Handoff

- Scope completed: shard-aware trace counters and native net benchmark
  evidence. `TRACE_NET` now reports runtime shard count, accept totals,
  active owner shards, min/max/imbalance, global fallback count, and fd-ready
  batch aggregates. `TRACE_NET_SHARDS` reports per-shard accept and fd-ready
  counts. `SCHED_TRACE` reports denied Tier 1 steals and connection-owner
  placement/locality counters.
- Task 12 validation found and fixed an existing multishard accept liveness
  bug. A single accept task can register waiters for all listener-group fds;
  park previously woke only one owner poller, and shard workers could sleep
  after one short poll timeout even while their shard still had net waiters.
  The issue reproduced at clean Task 11 commit `74ad7b46`, so it was not a
  trace-counter regression.
- Fix details: `rt_net_wake_poll_for_task_wait_keys` wakes every net owner
  shard represented by the task's wait keys, and multishard worker polling
  keeps cycling while `rt_net_has_waiters_on_shard` remains true. The
  per-shard wake behavior harness now covers the multi-key helper.
- `scripts/bench_native_net.sh` supports explicit shard and connection
  matrices and reports the new counters. Its default path preserves the old
  single-connection benchmark matrix; Task 12 evidence rows are requested by
  env.
- Benchmark report:
  `build/benchmarks/runtime-v2-task12-native-net.md`. Rows covered 1 and 8
  shards at 1, 8, 32, and 1024 connections; 10k rows were skipped by the
  script's safety default. The 8-shard/1024 row used all owner shards
  (`accept_0=133 accept_1=136 accept_2=136 accept_3=126 accept_4=108
  accept_5=133 accept_6=144 accept_7=108`) with `global_path_fallbacks=0` and
  `sched steal=0`.
- Performance interpretation: 8-shard throughput was worse than 1-shard in the
  Task 12 direct/seq benchmark. This is accepted for Epic 6 because the global
  executor lock is still preserved; Task 12 proves ownership/locality
  visibility and absence of shard-0 fallback, not line-rate scaling. Low-count
  `SO_REUSEPORT` skew at 1/8/32 connections is explicitly non-failure.
- Independent review/test subagent approved the final patch after bounded
  checks and a short benchmark smoke. No new durable debt was opened.
- Final gates passed: `bash -n scripts/bench_native_net.sh`,
  `git diff --check`, `./check_file_sizes.sh --self-test`,
  `./check_file_sizes.sh`, `make c-check`, `make cppcheck`, focused
  accept/scheduler trace tests, `TestRuntimeV2NetPollerPerShardWakeBehavior`,
  `timeout 300s make runtime-v2-check`, `timeout 300s make check`, Task 12
  benchmark evidence, and Sentrux root/runtime/native scans (`6182`, `5340`,
  `5467`).

## Epic 6 Task 13 Handoff

- Scope completed: `runtime-v2-accept-check` stayed wired into
  `runtime-v2-check` and was refined from an earlier metadata/static gate into
  the stable Epic 6 accept CI gate.
- The target now runs four sequential focused commands: untagged
  `SURGE_SHARDS=1` accept compatibility; tagged accept config/metadata/static
  and Task 12 accept trace contracts; tagged per-shard net-poller contracts;
  tagged scheduler placement/no-steal contracts.
- Every command keeps the sibling gate shape:
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0`, `-count=1`, `-parallel=1`,
  `-p=1`, and an explicit Go timeout. Tagged contracts keep
  `-tags runtime_v2_pending`.
- Excluded from this narrow gate: broad `MT|Async|Net|LLVM` VM regex debt,
  live `SIGUSR1` probes, benchmark rows, 10k stress, invalid-owner panic
  checks, and parked-with-work scheduler liveness checks.
- `RV2-DEBT-013` stays open and `DEBT.md` was not edited. Copied/raw net-handle
  owner-generation guards remain a later net-handle or stdlib owner-local
  design task.
- `LIVENESS_PROBES.md` now has the Runtime V2 accept CI liveness gate row and
  the Task 12 `TRACE_NET`, `TRACE_NET_SHARDS`, and `SCHED_TRACE` fields.
- Validation passed: implementation `timeout 300s make runtime-v2-accept-check`;
  independent review/test subagent with no findings plus its own `timeout 300s
  make runtime-v2-accept-check`; three consecutive main-session `timeout 600s
  make runtime-v2-check` passes; and `timeout 600s make check`.

## Epic 6 Task 14 Handoff

- Scope completed as audit/allowlist tightening, not a new runtime-code move.
  Subagent planning could not proceed because the subagent quota was exhausted,
  so the main session performed the Task 14 plan and implementation locally.
- Decision: no new code extraction. Epic 6 already extracted the cohesive
  responsibilities into `rt_scheduler_placement.c`, `rt_net_poller.c`,
  `rt_net_accept_group.c`, `rt_net_handles.c`, `rt_net_lifecycle.c`,
  `rt_net_listener_socket.c`, and `rt_net_trace.c/h`.
- Effective LOC outcomes versus Task 1 baseline: `rt_net.c` `904 -> 818`,
  `rt_async_state.c` `1727 -> 1722`, `rt_async_task.c` `768 -> 731`, and
  `rt_async_internal.h` `499 -> 478`. Physical LOC is recorded in
  `06-evidence.md` for context.
- `.loc-legacy-allowlist` was tightened to the exact final effective counts for
  `rt_net.c`, `rt_async_state.c`, and `rt_async_task.c`.
- Rejected extraction paths: remaining `rt_net.c` poll construction should wait
  for a future poll/read-write split; remaining `rt_async_state.c`
  ready/wake/timer loop should wait for the lock-splitting boundary;
  `rt_async_internal.h` is under the effective target and should not be split
  now.
- Debt status: `RV2-DEBT-003`, `RV2-DEBT-004`, and `RV2-DEBT-005` remain open
  because the files are still over the 500-line target, but their ceilings are
  stricter after Task 14.
- Validation passed: `git diff --check`, `./check_file_sizes.sh --self-test`,
  `./check_file_sizes.sh -a`, `make c-check`, `make cppcheck`, `timeout 600s
  make runtime-v2-check`, `timeout 600s make check`, and Sentrux
  root/runtime/native scans (`6182`, `5340`, `5467`).

## Epic 6 Task 15 Handoff

- Epic 6 is closed. Task 15 was docs/closeout only; no runtime C, Go, stdlib,
  parser, semantic, lowering, or public syntax files changed.
- Implemented Runtime V2 contract: structural `N>1` native TCP accept
  ownership under the preserved global executor lock; per-shard listener
  groups; owner-shard fd registry/waiters/poller/wake for net readiness,
  close, cancellation cleanup, and shutdown; one Tier 1 worker per shard in
  multi-shard mode; no non-owner steal for connection tasks.
- Deferred to Epic 7: splitting `rt_executor.lock` and moving remaining global
  compatibility primitives toward shard-owned state. This includes generic
  task/scope state, global scheduler coordination, non-net waiters, channels,
  join, scope wake, cancellation state, blocking completions, timers,
  `now_ms`, and generic ready work.
- Deferred to later Phase 4+ epics: cross-shard messaging, inbound queues,
  eventfd/credit protocols, remote select, distributed scopes, remote-free,
  public crossing syntax, and alternate I/O backends.
- Syntax gate remains active: before changing keywords, parser, semantic
  checks, async lowering, stdlib public API, or public examples for crossing,
  stop and discuss the language surface with the user. Current names such as
  `far`, `submit_to`, `crosses`, and `shard-movable` are placeholders.
- Debt state: no new Task 15 debt. `RV2-DEBT-010` and `RV2-DEBT-013` remain
  open and now point to future net handle ABI/lifecycle or owner-local stdlib
  server design. `RV2-DEBT-003`, `RV2-DEBT-004`, and `RV2-DEBT-005` remain
  open with stricter LOC ceilings. `RV2-DEBT-001`, `RV2-DEBT-002`, and
  `RV2-DEBT-011` remain test/backend matrix debt; the live ledger now assigns
  that matrix rewrite to Epic 12 after the Epic 8 insertion.
- Final benchmark report:
  `build/benchmarks/runtime-v2-epic6-closeout-native-net.md` (ignored build
  artifact). The 8-shard/1024 row used all 8 accept shards with
  `global fallbacks=0` and `sched steal=0`, but throughput was worse than the
  1-shard row under the preserved global lock.
- Final checks: `git diff --check`, `./check_file_sizes.sh -a`, `make c-check`,
  `make cppcheck`, `timeout 600s make runtime-v2-accept-check`,
  `timeout 900s make check`, final benchmark build/run, and Sentrux
  root/runtime/native scans passed. First `timeout 600s make runtime-v2-check`
  attempt hit the known `RV2-DEBT-002` timeout class in
  `TestMTBlockingChannelHelpersAllowTimersToAdvance`; the immediate rerun
  passed the full Runtime V2 chain including the accept gate.

## Post-Epic 7 Docs/Debt Cleanup

- Epic 7 status was normalized after review: the epic document and roadmap now
  mark it Complete, matching the closeout and `07-tasks/README.md`.
- The next runtime epic is explicitly runtime-only: task lifecycle/control-lane
  peel plus the adopted 8x1024 starvation investigation (`RV2-DEBT-016` and
  `RV2-DEBT-015`). It must not change Surge syntax or implement Phase 4
  transport.
- The explicit crossing surface and Phase 4 transport moved to the following
  roadmap slot and still require a dedicated language-syntax review with the
  user before parser, semantic, lowering, stdlib public API, or examples
  change.
- `RV2-DEBT-004` moved from the Open Debt table to Closed Debt. Test/backend
  matrix owners in the live debt ledger were renumbered to Epic 12 after the
  task-lifecycle epic was inserted before syntax/transport work.

## Epic 7 Kickoff Context

- Epic 7 opened as
  `07-executor-lock-split-and-shard-runtime-state.md` with task index
  `07-tasks/README.md`. Scope: split `rt_executor.lock` into per-shard locks
  plus a reduced global control lane; move scheduler queues, waiter-store key
  ownership, sleep timers, and channel ownership to shard-owned state; re-lane
  blocking completion, main-thread await, and shutdown.
- Current single-lock evidence anchors: invariant comment
  `rt_async_internal.h:259-271`; worker loop takes the global lock every
  scheduler turn and sleeps on the one global `ready_cv`
  (`rt_async_state.c:1656`, `1675`); multi-shard wakes broadcast to all
  workers (`rt_async_state.c:737-743`); non-net waiters all land in shard 0's
  store (`rt_async_waiter.c:438-459`, `rt_runtime.c:245-251`); sleep timers
  scan the whole task table per yield (`rt_async_state.c:1162-1226`); channel
  ops take the global lock (`rt_async_channel.c:130-203`, `213-287`).
- Key boundary decisions already fixed in the epic doc: two-lane model only
  (shard lock + control lane); lock order control -> at most one shard lock,
  never shard -> control, never two shard locks; collect-then-wake for
  cross-shard wakes with `wake_token` absorbing spurious wakes; task ids never
  reused; every task gets an owner shard at creation; one global virtual
  clock preserved, whole-table sleep scans replaced by an explicit sleep
  store; channels get an owner shard (creating task's shard, shard 0
  outside tasks); no Phase 4 messaging, eventfd, credits, or `PARKED`
  protocol in this epic.
- Accept-time owner re-placement (`rt_async_waiter.c:345-358`) is the one
  place a task's owner shard changes after creation; the Task 3 spike must
  define that transition protocol under split locks.
- Intended proof shape: Task 4 behavior tests + Task 5 static gates first,
  then strictly sequenced lane migrations (Tasks 6-11), then counters +
  benchmarks vs the Epic 6 closeout rows, then the `runtime-v2-lock-check`
  gate. Performance contract: the 8-shard/1024-conn row must improve on the
  Epic 6 closeout baseline or the closeout must name the next serialization
  point with evidence.

## Epic 7 Task 1 Handoff

- Baseline commit `77475384` (epic-open docs commit). All gates green on
  first run: `make check` (via pre-commit), `make cppcheck`,
  `timeout 600s make runtime-v2-check`, `./check_file_sizes.sh -a`,
  `git diff --check`. Sentrux CLI baselines equal Epic 6 closeout:
  root `6182`, `runtime` `5340`, `runtime/native` `5467`. Sentrux MCP tools
  are not exposed in this session; the CLI is the recorded mechanism.
- Effective LOC headroom that constrains implementation tasks:
  `rt_async_state.c` 1722/1722 (at ceiling), `rt_net.c` 818/818,
  `rt_async_waiter.c` 488/500, `rt_async_internal.h` 478/500,
  `rt_async_task.c` 707/731. New lane code must go to new files.
- Fresh pre-split benchmark baselines (current-checkout binary, reports under
  `build/benchmarks/runtime-v2-epic7-task1-*`): Epic 6 matrix (8 req/conn)
  8-shard/1024 is 1.64x slower than 1-shard; steady 32-conn x 2000-req
  8-shard is 2.75x slower with 5x worse p95; channel ping-pong 4.4us at 1
  worker vs 17.7us at 2+; sync channel 9.3us -> 93.6us.
- Reproducible baseline liveness deficiency adopted as an Epic 7 target
  probe: 8-shard/1024-conn/100-req steady row fails 3/3 with a client
  `TimeoutError` (>10s single-connection stall; 965/1024 connections finish;
  server exits cleanly, owner counters clean). 1-shard row passes. Post-split
  this row must pass or closeout is blocked.
- Benchmark parameter dead end recorded: 1024-conn rows with default
  `REQUESTS=2000`/`RUN_TIMEOUT=30s` kill the server mid-row; use the Epic 6
  matrix or `REQUESTS=100`/`RUN_TIMEOUT=120s`.

## Epic 7 Task 2 Handoff

- `07-executor-lock-dependency-map.md` written: 187 `rt_lock`/`rt_unlock`
  sites in 12 files inventoried; every
  `rt_executor`/`rt_shard`/`rt_task`/`rt_scope`
  field assigned a target lane (control / shard / atomic / immutable / tls /
  blocking); waiter-key ownership decided per kind (join/timer/blocking ->
  task owner shard, channel -> channel owner shard, net -> fd owner shard,
  scope -> spike); path table covers every locking caller.
- Hazards pinned for the spike and implementation: wake-vs-park token window,
  stale `park_key` reads, duplicate enqueue guard, accept-winner whole-table
  cleanup, channel value loss on cancelled peers, `poll_ready_child_inline`
  relock, compensation workers, init ordering.
- Everything unresolved is explicitly marked *(spike)* in the map: clock
  protocol, task lifetime rule, scope-key store, re-placement transition,
  condvar fates, non-user polls under shard lock, accept-winner cleanup
  shape. Task 3 must answer each mark.

## Epic 7 Task 3 Handoff

- `07-locking-model-proving-spike.md` fixes the model as decisions D1-D16;
  every *(spike)* mark in the dependency map is answered. Core rules:
  per-shard `lock` + `worker_cv` + `poller_cv`; order control -> at most one
  shard lock (TLS debug assertion); waiter entries carry `owner_hint`;
  same-shard hints wake inline under the held store/shard lock, foreign
  hints go through control (collect-then-wake); `owner_shard_id` writes only
  under control with the accept transition as the single post-spawn writer;
  free requires control + owner (control first); `mark_done` = shard phase +
  optional control epilogue; atomic `now_ms` + per-shard sleep stores +
  last-idle-worker advance; select/timeout are control-serialized slow
  lanes; join/channel/net/sleep/blocking steady paths never touch control.
- Proof ran 5/5 green: TSan x4 + O2 x2 on a 4-shard/32-task/20k-cycle model
  with 3 cross-shard wakers; total_wakes exactly equals parks performed
  (nothing lost, nothing double-consumed); spurious parks 0.04-0.08%.
  Prototype source is inlined verbatim in the spike doc.
- Dead ends recorded (do not retry): one cv per shard; free under owner lock
  alone; owner chasing under shard locks; deref of foreign-hint entries
  under store locks; per-shard virtual clocks.
- Next: Task 4 behavior contract tests and Task 5 static shape tests encode
  D1-D16; they may run in parallel with disjoint files.

## Epic 7 Task 4 Handoff

- Nine behavior modes landed (`runtime_v2_lock_split_harness_test.go` +
  `runtime_v2_lock_split_behavior_test.go`), each at `SURGE_SHARDS=1` and
  `SURGE_SHARDS=3`: cross-join, cross-cancel, cross-channel FIFO+close,
  close-wakes, blocking-completion, sleep-idle-advance,
  select-across-owners, timeout-across-owners, shutdown-liveness. All
  bounded-wait; a lost wakeup fails in seconds.
- The suite immediately caught a real pre-existing multi-shard deadlock:
  `wake_channel_task_no_signal` injected into another shard's queue without
  signaling, so a sleeping owner-shard worker never drained the handoff
  (cross-channel mode hung at shards-3, 8/9 modes green). Fixed test-first:
  `711d41f3` (pure deque extraction to make LOC room; state.c 1722 -> 1580)
  then `d78c8d1f` (signal when the woken task's scheduler is not the
  current worker's). 9/9 x 2 configs green after; `runtime-v2-check` green
  twice; cppcheck/c-check green.
- Sentrux after the fix: 6181/5326/5450 (root/runtime/native), all rules
  pass; small drop vs 6182/5340/5467 baseline attributed to the file split
  and new tests; Task 14 owns restoring or explaining the delta.
- Do not retry: relying on `rt_debug_assert_no_parked_with_work` to catch
  cross-scheduler no-signal handoffs — the queue fills after the worker
  commits to sleep, so only the signal fix closes it.

## Epic 7 Task 5 Handoff

- Eight static gates landed in `runtime_v2_lock_split_static_test.go`,
  pinning the D1-D16 shape: shard lock/cvs + waiter owner_hint; lane API
  (`rt_control_lock/unlock`, `rt_shard_lock/unlock`,
  `rt_lane_debug_enabled`); atomic `now_ms` + `sleep_store`; zero
  `rt_lock(`/`rt_unlock(` call sites; `rt_worker_main` on the shard lane;
  no `tasks_cap` sleep scans; channel `owner_shard_id` + owner-lane send;
  `ready_cv`/`io_cv` retirement.
- All eight are red at this commit by design (187 ambiguous lock call sites
  counted). No gate runs them; Task 13 wires the green set. Flip order:
  Task 6 -> shape/lane gates; Task 7 -> worker loop; Task 9 -> clock/sleep;
  Task 10 -> channel; Task 11 -> ambiguous-lock + condvar retirement.
- Helper note: use `lockSplitFunctionDefinitionBody` (definition-aware) for
  body gates; the shared `cFunctionBody` matches forward declarations.

## Epic 7 Task 6 Handoff

- Structure landed behavior-identically: `rt_lane.c` (lane API + TLS order
  panics + `rt_shard_sync_init/destroy`), shard `lock`/`worker_cv`/
  `poller_cv`, waiter `owner_hint`, `rt_lock`/`rt_unlock` delegate to the
  control lane. `ShardSyncShape` + `LaneAPIShape` green; behavior suite and
  `runtime-v2-check` green; sentrux 6181/5334/5458.
- LOC watch: `rt_async_internal.h` 493/500, `rt_async_waiter.c` 490/500 —
  Tasks 8-9 must extract before adding to either.
- Next: Task 7 moves scheduler queues, running counts, and worker sleep to
  the shard lane using the sanctioned nested shape (control -> shard), with
  a per-shard wake-pending counter so the worker can release the control
  lock before waiting on its shard cv.

## Epic 7 Task 7 Handoff

- `ready_cv` is gone: worker sleep = `rt_sched_worker_sleep` (release
  control -> shard `worker_cv` wait consuming `wake_pending` -> reacquire);
  wakes signal only the owner shard; sync-channel compat waiters moved to a
  control-lane `compat_cv` (their tasks are RUNNING during the wait, so the
  ready-push-failed fallback broadcast is their only wake). Universal owner
  assignment landed (`rt_task_assign_spawn_owner`); `ex->shutdown` is
  atomic. Two stub-harness contract tests updated for the new shutdown
  sweep (`rt_sched_wake_broadcast_all` + `compat_cv`).
- Behavior stayed green: Task 4 suite 9/9 x2, `runtime-v2-check` x2,
  cppcheck/c-check. LOC: state.c 1563/1722; header 499/500 — Task 8 must
  extract decls before adding any.
- Worker turn itself still holds the control lock (peel is Task 11);
  `wake_pending` leftover tokens cost one extra rescan by design.
- Next: Task 8 waiter-store key ownership (join/timer/blocking keys to the
  task owner's store, per-key store locks under the nested shape,
  collect-then-wake for foreign hints).

## Epic 7 Task 8 Handoff

- Per-key waiter-store ownership landed (`rt_waiter_route.c` resolver +
  `ex->control_waiters` for scope keys + join-waiter migration inside
  `rt_task_replace_owner` at the three accept-transition sites). Header
  pressure solved by extracting `rt_waiter.h` (internal.h 434). All under
  the control lock still; the peel adds shard locks.
- Key invariant recorded: store resolution is stable because the accept
  transition is the only post-spawn owner change and it migrates
  `join_key(task)` entries; `get_task==NULL` fallback is safe because
  `mark_done` drains join entries before free.
- Gates: Task 4 suite 9/9 x2, `runtime-v2-check` x2, c-check/cppcheck,
  LOC all under targets (waiter.c 491/500). Channel keys stay shard-0 until
  Task 10 (comment + resolver default arm).
- Next: Task 9 sleep/timer store + atomic clock.

## Epic 7 Task 9 Handoff

- Sleep scans are dead: per-shard sorted sleep stores + atomic mirrors +
  atomic clock landed (`rt_async_sleep.c`); tick fires own shard inline and
  tokens foreign shards; worker loop pops own due sleepers; advance is a
  monotonic CAS + global fire sweep. Gates `ClockAndSleepStoreShape` and
  `NoWholeTableSleepScan` green; behavior suite and `runtime-v2-check` x2
  green.
- Trap recorded: empty-store mirrors MUST be UINT64_MAX (zeroed memory
  reads as deadline 0 and spins idle paths) — `rt_sleep_store_init` in
  `rt_shard_init`.
- Next: Task 10 channel owner shard (channel keys to owner store,
  same-shard fast path), then Task 11 the peel.

## Epic 7 Task 10 Handoff

- Channel owner metadata + key routing landed (owner fixed at creation,
  channels never freed, resolution stable). Channel OPS stay control-laned:
  flip-plan corrected — task-state fields must switch guardians atomically
  across all accessors, so worker-loop/channel/ambiguous-lock/condvar gates
  all flip at the Task 11 peel together.
- Gates: channel behavior modes green, `runtime-v2-check` x2 green,
  c-check/cppcheck green. Stub harness gained `rt_channel_owner_shard_id`.
- Next: Task 11 — the peel. Blocking/await/shutdown lanes (the original
  Task 11 scope) merge INTO the peel since done_cv gating and the
  control-epilogue split are inseparable from removing the control lock
  from mark_done and the worker turn.

## Epic 7 Task 11 In Progress — Peel Plan And State

- DONE: alias kill commit `9db7e56e` — `rt_lock`/`rt_unlock` deleted;
  every site names `rt_control_lock/unlock`; harnesses updated; gate
  `NoAmbiguousGlobalLock` green. Remaining red gates: `WorkerLoopShardLane`
  (B1b), `ChannelOwnerShape` (B2), `GlobalCondvarRetirement` (B4, io_cv).
- Step B1a (next): primitives get internal locks; every caller still holds
  control; nothing holds a shard lock when calling them. Exact design:
  - `ready_push_with_policy` splits into a no-lock leaf
    (`ready_push_task_locked(ex, shard, task, force_inject, front,
    signal)`) + a locking wrapper; the wake-token bump and `worker_cv`
    signal fold into the same owner-lock hold (drop the separate
    `rt_sched_wake_signal_shard_n` cycle there).
  - `wake_task_with_policy`: lock owner; token + park-key snapshot + clear
    + status + leaf push; unlock; THEN value-based stale-entry removal from
    the snapshotted key's store (store lock inside `remove_waiter`).
    Absorbed-spurious rule per D5.
  - `park_current`: `add_waiter` (store-locks internally) then owner-lock
    token/commit dance; abort removes the entry after releasing the owner
    lock.
  - `add/remove/pop_waiter`, `wake_key_all`, join migration, and the net
    completion take their store's shard lock internally;
    `wake_key_all`/net-completion switch to collect-then-wake (pop matches
    under store lock into a batch, unlock, wake each via owner-lock wake) —
    never wake under a held store lock, even same-shard (same mutex would
    self-deadlock).
  - `rt_sleep_fire_due_on_shard` manages its own locking (drain due batch
    under shard lock, unlock, wake batch); callers stop wrapping.
    `poll_sleep_task` locks the owner shard around the store add;
    `mark_done` locks around the cancelled-sleeper remove.
  - `pop_waiter`'s stale-skip deref of foreign tasks stays legal in B1a
    (every caller holds control); B2 must restructure channel usage to the
    candidate/validate pattern before channels drop control.
  - Field-guard note: `prepare_park`/`park_prepared`/`park_key` writes on
    the CURRENT task need no owner lock even post-peel — a RUNNING task's
    park prep is single-writer (its poller thread); wake reads `park_key`
    only when status==WAITING, and the parker's WAITING store under the
    owner lock happens-before.
- Step B1b: worker turn drops control; new file `rt_worker_turn.c` with the
  shard-locked turn: pop own queue (deref legal: own-queue entry implies
  owner), inline `wake_task_on_shard_locked` leaf for own-shard wakes
  (sleep fire, net r/w completion), net poll under own lock with the
  syscall unlocked, accept-ready keys DEFERRED (release shard -> control ->
  existing accept completion -> release -> relock shard), `tick_virtual`
  under the held lock returns a foreign-due shard MASK (uint64, shards<=64)
  and the caller signals after unlock, apply_poll_outcome splits: stage 1
  under own lock (task state, own-store ops, collect foreign
  removals/wakes/epilogue flags), stage 2 after unlock (foreign stores,
  control epilogue: scope bookkeeping, gated done_cv, free under
  control+owner re-lock). Worker sleep simplifies to a plain locked
  cond_wait on `worker_cv` (already holding the shard lock).
  Control callers (spawn, cancel, select/timeout, scopes, N=1 runner,
  blocking completion, sync-compat, accept transition) KEEP control and use
  the internally-locking primitives (control -> one nested shard lock is
  legal); `run_ready_one`/`poll_ready_child_inline` keep the control-held
  apply variant.
- Step B2: channel ops drop control: owner-shard lock around buffer +
  candidate pop; validate/deliver resume under the PEER's owner lock via
  control when foreign (collect-then-wake); undeliverable peer -> relock
  store, next candidate (value never lost — the waiter-check contracts are
  the proof).
- Step B4: io thread waits on shard 0's `poller_cv` for N=1 duty; N>1
  idle-advance backstop via coarse control-lane timedwait plus the
  last-idle-worker advance; N=1 runner (`next_ready`) keeps control with
  nested shard-0 locks; `io_cv` retires (gate flips); `done_cv` gating via
  an atomic await-waiter count.
- Deref audit anchors for B1b: own-queue pop implies ownership (universal
  owner + intra-shard-only steal + accept re-place only while parked);
  free only under control + owner (mark_done epilogue re-lock;
  task_release takes control then owner).
- B1a LANDED (this commit): primitives lock internally, all callers still
  hold control. `ready_push_task_locked` + `wake_task_on_shard_locked`
  leaves (const-ex); `wake_task_with_policy` = owner lock + leaf + deferred
  stale-key removal; `park_current` = register-then-commit with owner-lock
  token dance and post-unlock abort removal (`park_requeue_locked`);
  `wake_key_all` and net completion = collect-then-wake batches (16 inline,
  heap growth); `add/remove/pop_waiter` lock their store's shard internally
  (`rt_waiter_key_shard`, NULL = control store); join migration extracts
  under source lock then appends under destination lock in 16-entry loops;
  sleep fire drains 32-entry batches under the shard lock and wakes
  outside; sleep arm/remove sites lock the owner shard. Owner-local waiter
  stub harness gained `rt_shard_lock/unlock` + `rt_runtime_shard0` no-op
  stubs. Gates: behavior 9/9 x2 configs, `runtime-v2-check` x2, c-check,
  cppcheck all green.
- B1b NEXT (not started): worker turn drops control per the recorded plan
  (shard-locked turn in a new `rt_worker_turn.c`, accept deferral to
  control, tick foreign-due mask, apply stage split, mark_done
  shard-phase/control-epilogue, worker sleep as plain locked cond_wait,
  remaining accessors of queues/running_count get shard locks as control
  drops). Then B2 channels, B4 io/runner + io_cv retirement + done_cv
  gating.

## Epic 7 Close (2026-07-04, main session)

- Epic complete: Tasks 1-15, closeout section in the epic doc, ledger
  entries through Task 14, DEBT.md reconciled (002 updated with fixed
  root causes; 004 closed; 015-018 added), README + RUNTIME_V2.md phase
  notes updated.
- Final peel state: B1b landed the shard-locked worker turn
  (`rt_worker_turn.c`) with inject/net fairness ticks; B2 moved channel
  entry APIs onto the channel owner's lane and introduced the
  generation-validated candidate/validate protocol (waiter `seq` +
  `park_seq`, owner-locked consume-or-arm mailbox, store-dedupe on
  registration, wake-only `seq==0` select entries, 10ms compat slice,
  compat broadcast after non-enqueued deliveries; channel code now lives
  in `rt_async_channel.c` + `rt_channel_lane.h` + `rt_channel_sync.c`);
  B4 retired `io_cv` onto shard 0's `poller_cv` (`rt_io_wait_slice` +
  `rt_io_poll_nudge` token) and taught the poll-ownership-miss branch to
  advance due virtual timers.
- For Epic 8: the control lane's steady-path consumer is task lifecycle
  (~26 `control_lock_acquired` per request on the 8x1024 row). The task
  table is already an atomic-snapshot structure and owner stability holds,
  so create/join/done can move shard-local without new deref rules. Watch
  `RV2-DEBT-015` (net starvation, reproducible via
  `stallrepro.py`-style load) when touching scheduler pacing, and do not
  build Phase 4 transport under a lifecycle label (syntax review first).
- Gate hygiene notes: `ldflags.sh` now strips quotes from commit subjects
  (Go's quoted.Split cannot parse shell-escaped apostrophes — a commit
  subject briefly broke `make build`); the net bench script reports the
  five new lock-split counters; `.loc-legacy-allowlist` ceilings:
  state.c 1580, task.c/net.c removed.

## Epic 8 Draft Opened

- Created `08-task-lifecycle-lane-and-net-fairness.md` as the review draft for
  the next runtime-only epic. The document targets `RV2-DEBT-016` (task
  lifecycle control-lane traffic) and `RV2-DEBT-015` (8x1024 starvation), and
  keeps syntax, explicit crossing, inbound queues, remote select, eventfd
  credits, and the seq-cst `PARKED` protocol out of scope.
- `08-tasks/` was not created yet. Next step after user review is to adjust the
  epic scope, then create the brief task index and expand Task 1 only.

## Epic 8 Open (2026-07-04, main session)

- Epic 8 document reviewed against the Epic 7 closeout state and fixed:
  trace counter field names now match the Task 12 `TRACE_EXEC` fields
  (`spurious_wakes_absorbed`, plus `collect_wake_batches` added); the
  accept transition (`rt_task_replace_owner`) is named as the existing
  cross-owner lifecycle edge; the select slow lane is a named non-goal
  (stays control-serialized, keep `seq == 0` wake-only entries working);
  Inputs gained `rt_scheduler_placement.c`, `rt_lane.c`,
  `rt_channel_sync.c`. DEBT.md's test-matrix owner rows already said
  Epic 12 (matches the roadmap; no change needed).
- Task index created at `08-tasks/README.md` (14 tasks, dependencies, lane
  rules carried over from Epic 7 with the additive-then-peel commit shape);
  Task 1 (`01-kickoff-baseline-and-sentrux.md`) expanded and ready to
  execute: fresh baselines are mandatory — same host but a different day
  than the Epic 7 closeout rows, so the ~26 control-acquisitions/request
  target must be re-measured before implementation starts.
- Sequencing note for the session that executes this: Tasks 6-10 are
  strictly sequenced C work on the lifecycle path; Task 11 (starvation
  investigation, `RV2-DEBT-015`) is independent after the spike but must
  not share C write sets with the lifecycle task in flight.

## Epic 8 Task 1 (2026-07-04, main session)

- Baselines recorded post-blocker-fix: 8x1024 matrix row 2.011s (1.30x the
  1-shard row), control_lock_acquired 26.4/request (the epic's numeric
  target), full census of 51 control-lock sites classified in
  `08-evidence.md` (16 steady-path lifecycle sites to migrate).
- GATE BLOCKER found and fixed during baselining (own commit): the
  ParkUnpark "load flake" was a real lost-wake — deferred waiter removals
  (wake-policy stale key; park-abort) executed after the owner lock was
  released could delete a FRESH re-registration of the same channel key.
  Removals are now generation-qualified; dedupe re-arms leftovers onto the
  current generation. 120/120 repro-clean; runtime-v2-check green x2
  consecutive, no rerun. Triage tooling kept under SURGE_TRACE_EXEC:
  TRACE_TASK_WAITING/READY per-task park state + TRACE_STORE len/cap.
  The compat lane moved to rt_async_compat.c (state.c 1452 <= 1580).
- RV2-DEBT-015 surprise: the 8x1024x100 starvation probe completed
  cleanly in one run (19.0s, p95 307us, no >10s tails) — Task 11 must
  first re-establish a reproducer before fixing anything; the Epic 7
  fairness ticks plus this lost-wake fix may have moved the landscape.
- For Task 2 (dependency map): start from the census table; the
  register-then-commit + generation protocol now has THREE moving parts
  (park_seq, entry seq, deferred removals) — the map must treat
  generations as part of the waiter-store ownership contract.

## Epic 8 Task 2 (2026-07-04, mapper architect subagent)

- Plan gate honored (Global Rule 9): plan sent to main and approved before any
  file edit. Two approval notes applied — spike open questions phrased for
  yes/no or concrete-protocol answers; README status wording kept as
  "Complete" to match Task 1.
- Produced `08-lifecycle-dependency-map.md` (the lifecycle analogue of
  `07-executor-lock-dependency-map.md`) and the self-contained task doc
  `08-tasks/02-lifecycle-dependency-map.md`. Docs-only; `git diff --check`
  clean; nothing compiled.
- Line numbers re-verified against baseline `daeac51e`; all 16 steady-path
  census sites still match (task.c 15/62/88/167,173/229/243/289/300;
  state.c release_lane_aware 1429, mark_done 1508 / gate 1486, apply cancelled
  1586; scope.c 10/45/84/100/134).
- Map conclusions (target lane per surface): task-id allocation + table growth
  stay control (fixed); slot publish + ready-push and slot lookup are the
  primary owner-shard candidates (spike S5-Q1, lookup); join poll + result read
  target the target-task owner shard (spike S5-Q3); completion (`mark_done`) is
  already lane-aware and targets owner-shard-local, with control only for the
  residual `mark_done_needs_control` reasons; scope enter/register/cancel/
  join/exit target the scope owner lane with a named control fallback for
  cross-owner (spike S5-Q7..Q11); clone → atomic refcount (spike S5-Q6);
  handle release/final free is already lane-aware (control frees, atomic
  refcount); external await / N=1 runner / sync-compat / select stay compat
  (select is a named non-goal).
- Generation contract documented as part of the waiter-store ownership
  contract (map §4): `park_seq` single-writer on the running poller, entry
  `seq` copied at channel registration (`seq==0` for non-channel), and the two
  deferred generation-qualified removers (`wake_task_with_policy`,
  `park_current` abort). Open question S9-Q7 asks whether join/scope
  registrations need the same qualification once off control (they are
  single-target and not address-reused like channels).
- `mark_done_needs_control` reasons enumerated (map §6): scope membership and
  `WAKER_JOIN`/`WAKER_SCOPE` park-key removal are the removal targets; net-key
  removal and `done_waiters>0` are compatibility (not hot-path debt). S6-Q1
  asks Task 3 to confirm only the two compat reasons survive on the hot path.
- Flagged for closeout: the stale executor-wide invariant comment at
  `rt_async_internal.h:292-304` (still says `ex->lock` owns tasks/scopes/shard
  stores/scheduler queues and `running_count`).
- Next (Task 3, proving spike): answer the 16 open questions (S5-Q1..Q14,
  S6-Q1, S7-Q1, S9-Q7); the spike output rewrites the map's lane table on
  conflict, and Tasks 4/5 must not start until both are reconciled.

## Epic 8 Task 3 (2026-07-04, spiker subagent)

- Plan gate honored (Global Rule 9 + Rule 1): plan sent to main and approved
  before any file edit or long-running command. Main's decisions folded in:
  record BOTH S5-Q1 designs with a numeric escalation criterion (do not commit
  to the segmented table in the spike); run the corroboration Go test; record
  the Epic 7 D8 revision for S5-Q10; and have rule 1 state the completion-pin
  interleaving explicitly (TSan model asserts it).
- Deliverables: `08-lifecycle-lane-proving-spike.md` (spike record + six rules),
  `08-tasks/03-lifecycle-lane-proving-spike.md` (task doc); reconciled
  `08-lifecycle-dependency-map.md` Target-Lane Summary; updated `README.md`
  (Task 3 Complete), `08-evidence.md` (Task 3 section), this file. Docs-only
  commit; tree C state pristine; `git diff --check` clean.
- Proof: throwaway TSan model (scratchpad `lifecycle_publish_refcount_spike.c`,
  NOT committed) — 4 shards / 160000 publications / 20000 completion-pin
  interleavings; safe design PASS on `clang -O1 -g -fsanitize=thread` and
  `-O2 -DNDEBUG`, twice each, `lost_publishes=0 uaf_detected=0`. Two
  deterministic negative controls fire: `-DUNSAFE_PUBLISH` loses a slot
  (shard-lane publish vs control-lane growth), `-DUNSAFE_NOPIN` aborts on a
  poisoned-payload read (joiner frees mid-body without the pin). Corroboration:
  4 waiter-contract tests PASS at baseline. S7-Q1 grep audit: only
  `rt_task_replace_owner` (accept) writes `owner_shard_id` post-spawn.
- 16 verdicts: S5-Q1 YES (ready-push owner shard; publish stays
  control-serialized with growth = realization A; escalate to segmented table B
  iff Task 5 create-site counter shows create >= 2.0 control acq/request on the
  8x1024 row); S5-Q2 YES; S5-Q3 YES; S5-Q4 YES; S5-Q5 control tree walk +
  owner-shard wake; S5-Q6 YES; S5-Q7 adopt task-table atomic-snapshot for
  `ex->scopes`; S5-Q8 YES; S5-Q9 YES; S5-Q10 move `scope_key` waiters to the
  scope owner shard store (revises Epic 7 D8); S5-Q11 YES; S5-Q12 YES; S5-Q14
  YES; S6-Q1 YES (only net-key removal + `done_waiters>0` survive); S7-Q1 YES;
  S9-Q7 `seq == 0` unqualified (join/scope keys are monotonic never-reused ids,
  not reusable addresses — must NOT adopt channel `park_seq`).
- Six written rules recorded (the Tasks 4-10 contract): (1) task lifetime —
  lookup/deref legality, result visibility, refcount release, control-lane free,
  and the completion pin holding the struct across the joiner-frees-mid-body
  interleaving; (2) join result visibility; (3) scope owner-lane model + named
  control fallback; (4) cancellation boundedness; (5) external-await boundary;
  (6) join/scope generation qualification.
- LOAD-BEARING required change for Task 8: `mark_done` currently writes
  `result_kind`/`result_bits` (`rt_async_state.c:1542-1543`) AFTER the
  `TASK_DONE` release store (`:1540`; `task_status_store` is release). Sound
  today only because join readers hold control; Task 8 MUST reorder the result
  writes before the `TASK_DONE` store when Task 7 drops control from the join
  read, or the lock-free join read is unsound.
- Re-scoping for later tasks: Tasks 4 and 5 may now start in parallel (map and
  spike reconciled). Which `mark_done_needs_control` reasons survive: only
  net-key removal (net contract) and `done_waiters>0` (external-await compat) —
  scope membership + `WAKER_JOIN` + `WAKER_SCOPE` all become owner-local after
  S5-Q3/Q10, so Task 8's target is those two compat reasons. Task 6 default is
  realization A; the segmented-table escalation is pre-decided by the numeric
  trigger so Task 6 does not relitigate it.
- Still flagged for closeout: the stale executor-wide invariant comment at
  `rt_async_internal.h:292-304`.
- Task 5 done: per-site `control_lock_acquired` attribution (`rt_ctrl_site`
  enum, `rt_trace_control_lock_site`, 6 `TRACE_EXEC` fields + bench columns),
  static gates (G1-G6 active/wired via new `runtime-v2-lifecycle-check`;
  P6-P10 pending as `t.Skip` with per-task activation criteria), and a
  trace-contract gate. Additive C only (no behavior change), `rt_lane.c`
  untouched. The `runtime-v2-lifecycle-check` stage enumerates each green test by
  name (Epic 7 precedent), so it gates Task 4's behavior contracts too from this
  commit on; pending P6-P10 gates are added to the regex by their peel commits.
- DECISION for Task 6 (measured, do not relitigate): 8x1024 baseline gives
  create = **3.500 control acq/request >= 2.0** → **ESCALATE to realization (B),
  the segmented never-moved-slot task table**. Realization A's per-connection
  amortization is disproven (request trees spawn ~3.5 tasks/request). Total
  26.348/request re-verifies the 26.4 baseline. Secondary targets by size:
  scope 13.000/request (Task 9, biggest payoff), join-poll 3.885 (Task 7),
  completion 0.509 (Task 8); handle/await-compat ~0 on the net bench.

## Task 4 (subagent `tester-behavior`): Lifecycle Behavior Contract Tests

- Added 5 Go test files under `internal/vm/runtime_v2_lifecycle_behavior_*_test.go`
  (build tag `runtime_v2_pending`, native C harness compiled at test time,
  no repository C/H file touched), covering the epic's focused-probe list:
  owner-local create/ready-push, join result observation across
  `SURGE_SHARDS=1,2,8`, three join register-then-verify timing cases,
  clone/release stress, a TSan completion-pin stress, scope enter/register/
  join/exit, cancelled-poll scope teardown, worker-vs-external await, and
  shutdown with join/scope/timer/channel/blocking all parked at once.
- Scope failfast has both a selected existing test
  (`TestRuntimeV2FailfastScopeCancellationWakesOwner`) and a dedicated new
  raw-C-level probe (`TestRuntimeV2LifecycleScopeFailfastCancellation`,
  added after the main session asked for one explicitly, since failfast is
  a required epic-contract item); net-parked-shutdown is satisfied by
  selecting `TestRuntimeV2NetPollerShutdownWakesEveryShard` rather than
  duplicating a synthetic socket. Full reasoning in
  `08-tasks/04-lifecycle-behavior-contract-tests.md`.
- Final count: 10 new tests. 9 PASS cleanly via real `go test`; the 10th
  (`TestRuntimeV2LifecycleCompletionPinInterleavingTSan`) SKIPs by design
  (see below). Full `SURGE_SHARDS=1,2,8` matrix green.
- Discovered (not previously written down): `tick_virtual`/
  `advance_time_to_next_timer` (`rt_async_state.c:1199-1257`) fast-forwards
  the virtual clock to the next timer deadline once workers go idle, so a
  long `rt_sleep` is not a stable "parked forever" primitive — fires in
  ~200ms of real time once nothing else is ready. Used a channel-recv park
  instead wherever a task needed to stay genuinely idle-but-alive.
- **RESOLVED (main session decision):** the TSan test
  (`TestRuntimeV2LifecycleCompletionPinInterleavingTSan`) found two real,
  reproducible races: (1) `mark_done`'s result-write-after-`TASK_DONE`-store
  ordering — Rule 1's already-documented Task 8 "Required change," now
  TSan-confirmed dynamically. (2) A second, not-previously-documented race:
  `wake_task_on_shard_locked` (`rt_async_state.c:965`) writes
  `task->park_key` under the shard lock while `mark_done_needs_control`
  (`:1494`) reads the same field with no lock at all (structurally
  unavoidable — that read decides whether to take a lock). Decision: commit
  the test with its full body, gated by `t.Skip("pending Task 8: baseline
  races RV2-DEBT-019 ...")`, matching Task 5's P6-P10 pending-gate
  convention. The result-visibility mitigation (an external awaiter forcing
  `mark_done_needs_control` true) is now toggleable via
  `LIFECYCLE_PIN_STRESS_NO_KEEPALIVE=1` so Task 8 can reproduce either race
  in isolation or both together. Recorded as `RV2-DEBT-019` in `DEBT.md`,
  owner Task 8 (completion epilogue), interacting with Task 7's helper-wake
  call site, with a close condition naming both the fix and the gate
  activation. Confirmed the full `^TestRuntimeV2Lifecycle` regex (this
  task's 10 tests + Task 5's static/trace tests — exactly what
  `make runtime-v2-lifecycle-check` runs) is green: 9 PASS, 1 SKIP, 0 FAIL.
- Full gates (`make c-check`, `make cppcheck`, `make runtime-v2-check`,
  `make check`, `git diff --check`, `./check_file_sizes.sh -a`) run under
  the granted commit barrier; results recorded below / in `08-evidence.md`.

## Task 6: Task Create And Table Publication

- Escalation verdict from Task 5 is binding: `ctrl_create=3.500/req >= 2.0`
  means Task 6 implements realization (B), the segmented never-moved-slot
  task table — not the safe-minimal (A) the epic doc names as the default.
- Design: `rt_task_table` is now a fixed-size, embedded (not swapped)
  directory of `_Atomic(rt_task_segment*)` (4096 slots/segment, 65536
  segments, 512KB directory). A segment, once allocated, is never freed or
  moved — this is what removes the publish-vs-growth race the spike's
  `-DUNSAFE_PUBLISH` control caught in the old copy-on-grow table.
  `next_id` becomes `_Atomic uint64_t`. `get_task`/`rt_task_slot_store`/
  `rt_task_table_snapshot` stay defined in `rt_async_state.c` (required by
  the active, already-shipped G3 static gate, which greps that exact file
  and function body); `rt_task_table_snapshot`'s signature changed from a
  struct pointer to a `uint64_t next_id` bound (approved by main), used by
  the two full-table scanners instead of touching the segmented internals
  directly. New file `rt_task_table.c` (41 lines) owns segment allocation.
- `__task_create` restructured to the spike-proven order: atomic id-alloc
  (no lock) -> lock-free segment-presence peek -> rare control-lane segment
  growth if missing -> owner-shard lock for slot-store + `task_add_child` +
  ready-push (one critical section, matching the proven interleaving) ->
  unlock -> lane-aware compensation-worker check (reusing `wake_task`'s
  existing idiom, not new machinery).
- **Hazard found during planning, fixed in the same commit (required, not
  optional):** moving `task_add_child` off control opened a real data race
  against `cancel_task`'s control-held children[] walk (a running parent
  cancelled from another thread while it spawns — a supported case). Fixed
  by nesting `task_add_child` under the parent/child's shared owner-shard
  lock (free — a fresh child's owner shard always equals its parent's) and
  having `cancel_task` snapshot children ids under that same lock before
  recursing (collect-then-recurse, mirroring the existing collect-then-wake
  shape). Verified the soundness argument (a parent's owner_shard_id is
  never concurrently rewritten by another thread while task_add_child reads
  it) by grep-auditing all three `rt_task_replace_owner` call sites: only
  one (`rt_executor_wake_net_waiters_for_key_on_owner`) targets a different
  task from another thread, and that target is always popped from a waiter
  store (i.e. parked, not running); the other two always self-replace
  synchronously. Wrote this down as an invariant comment at the lock site
  plus in the task doc, per main's explicit requirement. Proven by a new,
  self-contained test file (does not edit the Task 4 behavior-test files):
  `TestRuntimeV2LifecycleCancelSpawnChildrenRace` (deterministic, enumerated
  in `runtime-v2-lifecycle-check`) and `...RaceTSan` (best-effort, TSan,
  passed clean — zero races).
- Also fixed (required for compilation, not lifecycle-owned): a pre-existing
  stub in `internal/vm/runtime_v2_owner_local_waiter_static_test.go`
  (Epic-7-era, defines its own `rt_task_table_snapshot` to satisfy a
  standalone waiter-behavior harness's linker needs) had its signature
  updated to match the new `uint64_t` return type; its behavior (always a
  no-op scan, since it previously always returned `NULL`) is unchanged.
- Measurement (8x1024 row, `SURGE_TRACE_EXEC=1`, 3 stable runs): `ctrl_create`
  drops from 3.500/req to 0.001/req (residual = ~7-8 rare segment-growth
  events across 8192 requests) — the escalation's numeric target,
  essentially eliminated. `control_lock_acquired` drops from 26.348/req to
  22.780/req. Honest accounting (main required this): `ctrl_create`'s old
  cost does not simply vanish from `sum(sites)` — it reappears almost
  exactly in `ctrl_handle` (28673 total both before-as-create and
  after-as-handle, bit-for-bit reproducible across 3 runs), traced to
  `poll_ready_child_inline`'s pre-existing (unchanged by this task) control
  bracket firing far more reliably now that its "target still at local
  queue tail" precondition benefits from create's faster completion — this
  is exactly the surface the dependency map's S5-Q4 already flags as Task
  7's future migration target. The genuine win (`control_lock_acquired`
  dropping 3.570/req) comes almost entirely from the `OTHER` residual
  bucket (5.454 -> 1.890/req); this task did not fully instrument that
  bucket's internal composition and reports it honestly as an open question
  for Task 7 to pin down when it touches `rt_task_poll` directly.
- Gates: `git diff --check` clean; `make c-check`, `make cppcheck` OK;
  `timeout 1200s make runtime-v2-check` exit 0 (includes the activated P6
  and the new race gate); `make check` exit 0; `./check_file_sizes.sh -a`:
  `rt_async_state.c` 1444 (down from 1455, ceiling 1580 not grown),
  `rt_task_table.c` 41 (new, well under 500); Sentrux root/runtime/
  runtime-native all "All rules pass", quality flat vs baseline (6175/5294/
  5385 vs 6174/5296/5387).
- Full write-up: `08-tasks/06-task-create-and-table-publication.md`.
  Evidence: `08-evidence.md` Task 6 section.
- **Review response (before Task 7 spawned):** independent review of Task 6
  came back APPROVE-WITH-NOTES, two minor findings, both fixed in a
  follow-up commit. (1) The new race test hardcoded `SURGE_SHARDS=4`
  instead of sweeping the epic's required `1,2,8` matrix - fixed, both the
  plain and TSan variants now sweep via subtests, green at all three. (2)
  **Correction:** `STATS.md` committed with Task 6 was bogus -
  `scripts/code_stats_md.sh`'s (and `code_stats.sh`'s, same duplicated
  logic) `get_dir_stats "."` used an unscoped `find "."` that recursed into
  `.claude/worktrees/<agent>/` - a full nested repo checkout from Task 11's
  separate investigator worktree - roughly doubling every "main code"
  count (Files 720->1374, LOC 163977->307176 as committed). This was a
  pre-existing script bug, not something Task 6's changes caused, but it
  would have compounded across every remaining task's commit if left
  unfixed (each pre-commit hook regenerates STATS.md). Fixed by excluding
  `.claude/*` and `target/*` in both scripts; STATS.md regenerated to the
  correct scope (Files 721/LOC 164167 main code, matching the reviewer's
  cited baseline within the few files/lines this task's own new test file
  legitimately adds). Any Task 6 evidence that quoted the bogus STATS.md
  numbers should be disregarded; the `check_file_sizes.sh`/Sentrux/gate
  numbers in the Task 6 evidence section are unaffected (separate tooling,
  already correctly scoped).

## Task 7: Join Poll And Handle Lifetime (Complete)

- Migrated `rt_task_poll` (join register + result read), `poll_ready_child_inline`,
  `rt_task_clone`, and `rt_task_wake` off the control lane (rule 2, S5-Q2/
  Q3/Q4/Q6); `rt_task_cancel` stays control (S5-Q5, unchanged since Task 6).
  Pulled the `mark_done` result-write-before-`TASK_DONE` reorder into this
  task as a named enabling change (2 lines; closes RACE 1 of
  `RV2-DEBT-019`), rather than deferring to Task 8.
- **Folded in F2**, the Epic 8 Task 11 net-fairness fix (`RV2-DEBT-015`): a
  joiner consuming a DONE child carrying `TASK_PLACEMENT_CONNECTION` adopts
  the child's placement via `rt_task_replace_owner` (new static helper
  `rt_task_poll_adopt_placement`, both DONE-consume branches of
  `rt_task_poll`, immediately before `task_release_lane_aware`). Hard-
  constraint arm chosen: (1) an explicit control fallback (gated
  `!rt_lane_holds_control()`, tagged `RT_CTRL_SITE_JOIN_POLL`), reusing the
  accept-transition's own safety argument rather than re-deriving Task 6's
  children[]-append happens-before chain. New `placement_adoptions` trace
  counter proves it fires (~0.25/req on the 8x1024 row, matching the
  O(connections) frequency bound).
- **F2 measured working**: a mid-load `SIGUSR1` dump shows the owner
  histogram genuinely spread across all 8 shards (338-440 tasks/shard) for
  the first time — the pre-F2 baseline was "3073/3073 owner=0" (all
  execution on shard 0, `epic8-task11-placement-funnel`). `TRACE_STORE`
  waiter counts similarly distributed (338-444/shard vs all-in-shard-0);
  steady-state `inject_len=0` (vs ~1023 before). New dedicated test
  `TestRuntimeV2LifecycleJoinConsumePlacementAdoption` proves both the
  positive (adopts) and negative (does not adopt from a
  `TASK_PLACEMENT_GENERIC` child) cases, per main's explicit review
  requirement.
- **Honest accounting of a real, reproducible cost increase** (main's
  "report honestly" precedent from Task 6): `control_lock_acquired`'s total
  went *up* (186593 -> ~195600 on the same 8x1024/8192-request row), driven
  by `ctrl_completion` jumping from 4141 to a bit-exact 28673 every run.
  Root cause: `poll_ready_child_inline` used to hold control across its
  entire body (including the nested `mark_done` call), so `mark_done`'s own
  `need_control` check silently short-circuited false and its control work
  ran "for free" and untagged under the caller's ambient lock. Dropping
  `poll_ready_child_inline`'s control (required by S5-Q4) removes that
  ambient hold, so `mark_done` now correctly (and honestly) evaluates and
  tags its own need for these same completions - not a bug, a previously-
  hidden cost becoming visible. This is squarely Task 8/9's territory to
  claw back (`mark_done_needs_control`'s scope/join-key reasons, S6-Q1
  reduction, and scope ownership moving off control) - flagged explicitly
  in the Task 8 handoff. `ctrl_join_poll` itself dropped from 3.881/req to
  ~0.25/req exactly as designed (S5-Q3); `ctrl_scope` unchanged (106499,
  exact match, Task 9's territory).
- Two already-active Task 5 gates were updated (both approved by main
  before implementation): `TestRuntimeV2LifecycleStaticCensusSitesTagged`
  (G6) - `rt_task_clone`'s case deleted (drops control unconditionally, S5-
  Q6, nothing left to tag), `rt_task_poll`'s entry repointed to the new
  `rt_task_poll_adopt_placement` helper (structurally required by P7's own
  "no `rt_control_lock(` in `rt_task_poll`'s own body" bar, not evasive).
  `TestRuntimeV2LifecycleTraceControlSiteContract` - `ctrl_join_poll`
  removed from the must-be-nonzero list (genuinely 0 in that synthetic
  no-connection-placement program after this task).
- Gates: `git diff --check` clean; `make c-check` OK (one cppcheck fix:
  `rt_task_poll_adopt_placement`'s `target` param made `const`); `make
  cppcheck` 0 findings; `timeout 1200s make runtime-v2-check` exit 0 (all
  lifecycle gates green, including the newly-activated P7 and the new F2
  test); `make check` exit 0; `./check_file_sizes.sh -a`: `rt_async_task.c`
  312 (up from 307, OK), others unchanged/OK; Sentrux root/runtime/
  runtime-native all "All rules pass" (6174/5290/5379 vs baseline
  6175/5294/5385, normal noise). No `RV2-DEBT-018` transient encountered.
- Full write-up: `08-tasks/07-join-poll-and-handle-lifetime.md`. Evidence:
  `08-evidence.md` Task 7 section.

## Task 11: Net Fairness Starvation Investigation (Complete, `investigator` subagent)

- `RV2-DEBT-015` is CLOSED (fixed, not constrained). Mechanism was NOT
  WSL2 poll behavior: a placement funnel (stdlib net wrapper child tasks
  received the accept-transition placement and died with it) put ALL
  user-task execution on shard 0's single worker since at least Epic 6,
  and parked-read completions rotated through that shard's inject FIFO
  (~1s deterministic band at 1024-way sustained load; the historical
  >10s tails were the same rotation stretched by host load). Pre-fix
  build at `072bbde0` reproduced the identical band, ruling out the
  Task 1 lost-wake as the differentiator.
- Fix: F2 placement adoption at join consume, spec'd by this task and
  implemented by Task 7 (`rt_task_poll_adopt_placement`, `d998df20`,
  hard-constraint arm 1). Acceptance at `d998df20`: zero >=1s stalls in
  the 90s sustained run (was 8.4% of requests), 8-shard 1.12x 1-shard
  (was 0.82x), balanced workers, bench probe 5/5 clean, owner histogram
  spread, `inject_len=0`, adoptions O(connections).
- Durable consequences for the rest of the epic: (1) all pre-F2 8x1024
  rows measured a single-worker topology — Task 12 MUST re-baseline
  before judging control-lane targets (recorded in `RV2-DEBT-016`);
  (2) sustained bench p50 is now fair-unimodal (~20ms), not a
  regression; (3) sustained scaling is client-bound (~8.5k req/s Python
  client) — Task 12 needs a stronger load generator; (4) remaining
  control consumer is scope traffic (Task 9).
- Harness promoted: `scripts/stallrepro.py` (sustained 1024-conn client
  with live stall detection), `scripts/run_stallrepro.sh` (server +
  watcher + SIGUSR1 mid-stall dumps), `scripts/cpu_validate.sh`
  (per-thread CPU split) — these own their per-probe timeouts (noted in
  `RV2-DEBT-006`). Host rebooted between pre/post-F2 evidence runs;
  caveat recorded in the task doc.
- Full write-up: `08-tasks/11-net-fairness-starvation-investigation.md`.
  Evidence: `08-evidence.md` Task 11 section.

## Epic 8 Task 08 Handoff

- Scope completed: completion epilogue / done path. Single commit on `585e3c5c`.
- RV2-DEBT-019 CLOSED as a full `park_key` race FAMILY (the debt's two-race
  summary undercounted it). Root fix: the wake primitive
  (`wake_task_on_shard_locked` / `wake_task_with_policy`) now touches a task's
  `park_key`/`park_prepared` ONLY when the task is parked (WAITING and not
  enqueued) under the owner shard lock; the wake token still fires
  unconditionally (D5 abort). This closes the completer-side read AND the
  joiner-side register-then-commit races (`prepare_park`/`park_current` write
  `park_key` unlocked while RUNNING). With the wake gate, `mark_done` reads
  `park_key` as a plain thread-own read (no lock) — completion stays
  shard-local-cheap. Global Rule 7 ownership comment added at
  `wake_task_on_shard_locked`.
- Sibling data race fixed in-commit: `rt_waiter_migrate_join_waiters` unlocked
  `from->len` early-out (removed). Its higher-level control-era assumption is
  NEW debt RV2-DEBT-020 (owner: Epic 8 closeout; suspected benign — F2 fires
  only on DONE targets and register-then-verify re-checks DONE).
- S6-Q1: only the `WAKER_JOIN` reason removed from `mark_done_needs_control`.
  Scope (`parent_scope_id`/`scope_registered`) and `WAKER_SCOPE` reasons stay
  until Task 9's scope-owner-lane migration. `ctrl_completion` (28673) is the
  scope reason -> clawback reassigned to Task 9 (RV2-DEBT-016 note).
- Un-skipped `TestRuntimeV2LifecycleCompletionPinInterleavingTSan` in NO-KEEPALIVE
  mode, swept SHARDS 1/2/8, wired into `runtime-v2-lifecycle-check`. Pre-existence
  of the races confirmed at clean `585e3c5c` (no-keepalive SHARDS=8 FAILED under
  TSan, exit 1).
- P8 static gate activated (result-before-DONE + WAKER_JOIN-gone); scope-reason-
  gone assertion deferred to P9 (Task 9), noted in the P8 comment.
- Reviewer stale comments fixed: `ready_take_current_local_tail` (owner shard
  lock, not control) and `remove_waiter` ("control OR nothing, never a shard
  lock").
- `ctrl_handle` sub-attribution added (`ctrl_handle_wake/cancel/free`): 29696 =
  free 28672 + wake 1024 + cancel 0. The Task 7 rise is the per-connection
  `rt_task_wake` scope-adoption (wake=1024), not the free path.
- Gates green: `runtime-v2-check` (full blast-radius, wake primitive touched),
  `make check`, c-check, cppcheck, file sizes (`rt_async_trace.c` 671->666 via a
  dump-loop refactor that offsets the new counters), git diff --check.
- NOT tested / open: RV2-DEBT-020 re-derivation (Task 14). Task 9 owns the
  scope-reason removal + P9 + the `ctrl_completion` clawback.

## Epic 8 Task 09 Handoff (Scope Owner Lane)

- Scope completed: same-owner scope enter/register/join-all/exit/failfast
  bookkeeping and the `scope_key` waiter store moved off the control lane onto the
  scope's PINNED owner shard lane; `get_scope` is a lock-free acquire load of a new
  segmented `rt_scope_table` (mirrors `rt_task_table`); S6-Q1 complete
  (`mark_done_needs_control` final form = net-key + `done_waiters`); P9 peeled with
  the scope-reason-gone assertion. Full write-up: `08-tasks/09-scope-owner-lane.md`.
- Design: `rt_scope.owner_shard_id` pins the scope's serialization lock for its
  whole life (decoupled from the owner task's F2 mobility). `WAKER_SCOPE` routes to
  that pinned shard store (revises Epic 7 D8). Waiter primitives take the shard lock
  internally, so bookkeeping uses register-then-verify (join_all) and
  mutate-then-wake / snapshot-release-walk (child-done, failfast).
- Cancel-interplay (Q2 rider): the failfast/cancel WALK and cross-owner child-done
  take a counted control fallback (tagged `RT_CTRL_SITE_SCOPE`), NOT control-free —
  re-derived because `cancel_task` reads child `owner_shard_id` that F2 self-replace
  writes under control (Task 6 owner-lock invariant, `rt_async_task.c:71-93`).
  Same-owner non-failfast child-done stays control-free (the win).
- Measurement (8x1024 direct/seq, fresh matching-commit builds): `ctrl_scope`
  106499→19464 (-82%), `control_lock_acquired` 192262→105285 (-45%).
  `ctrl_completion`=28673 UNCHANGED and proven scope-INDEPENDENT — it is a NET
  `wait_keys` removal residual (net/accept contract, S6-Q1 keeps it out of this
  epic), NOT the scope reason. Task 8's DEBT.md clawback attribution was corrected
  (RV2-DEBT-016).
- Gates: `git diff --check`, `make c-check`, `make cppcheck`, `make check`,
  `make runtime-v2-check` (incl. lifecycle-check with P9 + `Scope*AcrossShards`
  shards 1/2/8), no-keepalive `CompletionPinInterleavingTSan` shards 1/2/8
  TSan-clean, `./check_file_sizes.sh -a`. `rt_async_state.c` shrank 1447→1377;
  `rt_async_internal.h` 543→555; new `rt_scope_table.c` 41. Sentrux runtime/native
  5387, all rules pass.
- Fixed along the way: `runtime_v2_owner_local_waiter_static_test.go` link break
  (its harness includes `rt_waiter_route.c`, whose `WAKER_SCOPE` now calls
  `get_scope`/`rt_scope_owner_shard` — added stubs).
- Next (Task 10): `done_cv`/`compat_cv` external-await narrowing. The net
  `wait_keys` `ctrl_completion` residual is future net-handle/accept work, not Task 10.

## Epic 8 Task 10 Handoff (Await / Runner / Blocking Compat, coder-t10)

- Scope: keep `done_cv`/`compat_cv` external-only and COUNTED SEPARATELY (spike
  rule 5); peel P10; honest-split the single-worker runner + sync `compat_cv`
  lane (no migration). Commit on `b9a420c0`. Full write-up:
  `08-tasks/10-await-runner-blocking-compat.md`; evidence in `08-evidence.md`.
- Already true at `b9a420c0` (Tasks 7/8/9): `rt_task_poll` never touches
  `done_cv`; `done_cv`'s only waiter is `rt_task_await` workers>1 (tags
  AWAIT_COMPAT); the `mark_done` broadcast is `done_waiters`-guarded. So Task 10
  had NO lane to migrate — only honest counting + gate + docs.
- Code change (behavior-neutral): `mark_done` splits its control tag —
  COMPLETION iff `wait_keys_len>0 || select_timers_len>0 || net park_key`, else
  AWAIT_COMPAT (the reason is a parked external awaiter, `done_waiters>0`). The
  lock is taken identically; only the trace tag changes. `rt_async_state.c`
  1377→1383.
- P10 peeled + strengthened (`StaticAwaitCompatCountedSeparately`, 5 assertions)
  and a trace guardian (`TraceAwaitCompatCountedSeparately`, asserts external
  await → `ctrl_await_compat>0`); both wired into `runtime-v2-lifecycle-check`.
- Honest split (no migration): `run_until_done`/`run_ready_one` (N=1 whole-
  executor loop) and `rt_wait_current_worker_wakeup` (sync `compat_cv`,
  RV2-DEBT-017) stay control-lane by design, counted-separate compat, not worker
  steady-path; untagged OTHER. Rule 5.
- FINDING (corrects Task 9 + RV2-DEBT-016): the 8x1024 `ctrl_completion`=28673 is
  NOT a net `wait_keys` residual — the tag split moved the whole population to
  `ctrl_await_compat`, proving the only reason was `done_waiters>0`. The net
  bench's `main` externally awaits `serve_many` (main.sg:309), so
  `done_waiters=1` for the whole run and net-wrapper child completions serialize
  on control as external-await compat. Behavior unchanged (total control
  105285→105351 noise; `ctrl_completion` 28673→0, `ctrl_await_compat` 1→28674).
  Task 12 input: ~27% of net-bench control is that harness artifact; not a clean
  steady-state measurement.
- RV2-DEBT-022 raised: narrow pre-existing latent `done_cv` lost-wakeup window
  (lockless `done_waiters` read + StoreLoad gap). Empirically unreachable; NO
  `done_cv` behavior change this task. Owner: focused compat fix or Task 14.
- Gates all green: `git diff --check`, `make c-check`, `make cppcheck`,
  `make runtime-v2-check` (incl P10 + trace gate + no-keepalive
  CompletionPinInterleavingTSan @ shards 1/2/8), `make check`,
  `./check_file_sizes.sh -a` (rt_async_state.c 1383 ≤1580). Sentrux: root 6174,
  runtime 5295, runtime/native 5382, all rules pass (no drop vs Task 9).
- Next (Task 12): net/channel re-baseline. The net bench's external-await
  artifact (done_waiters=1) means completion is shard-local when done_waiters==0;
  factor that into the steady-state control-per-request judgment.
- Task 10 review (Codex, verdict APPROVE-WITH-NOTES): P10 assertion (iii) was
  substring-vacuous (comment matched before the real guard) — fixed to match the
  guard load; completion_reason complement made structural via an out-param from
  mark_done_needs_control (single shared evaluation). Informational: trace
  guardian's ctrl_await_compat>0 includes completions racing the parked awaiter
  (the documented DEBT-016 population). Fix commit follows aa66a0b7.

## Epic 8 Task 12 Handoff (Performance Benchmark And CI Gate, coder-t12)

- Scope: post-F2 net re-baseline (the epic's performance record, replacing the
  non-comparable pre-F2 rows), channels reference refresh, sustained-stall /
  CPU-distribution acceptance re-verify, a per-commit trace-counter CI gate, and
  the RV2-DEBT-016 final-state decision. Baseline HEAD `8c89f358`; fresh
  matching-commit build (stale `8c4b16f9` binary rebuilt). NO runtime C change.
  Full write-up: `08-tasks/12-performance-benchmark-and-ci-gate.md`; the record
  is `08-evidence.md` Task 12.
- RE-BASELINE (8x1024, x5): `control_lock_acquired` ~105316/8192 = 12.86/req
  total; steady-state-control (= total − `ctrl_await_compat` 28674) = 9.36/req
  — both << the Epic 7 ~26.4/req. 8-shard/1024 total ~1.48M us is ~4% FASTER
  than 1-shard ~1.54M (scaling met); p50 15.0ms (8-shard) / 20.4ms (1-shard),
  both unimodal (fairness shape, not regression). 1-shard control (~30.8/req) is
  the N=1 runner loop (not a steady-state point). Sustained rows are client-bound.
- ACCEPTANCE: 90s stallrepro 746372 req, 0 err, 0 tails >=5s/>=10s (one 1.25s
  client-load blip); cpu_validate balanced across all 8 shard workers (max/min
  ~1.7, no funnel). RV2-DEBT-015 holds fixed at HEAD. Channels within Task 1
  noise (rendezvous path untouched this epic).
- CI GATE: new `TestRuntimeV2PerfControlLaneGate`
  (`internal/vm/runtime_v2_perf_gate_test.go`, `runtime_v2_pending`, 422 lines),
  wired via new Makefile `runtime-v2-perf-check` → `runtime-v2-check`.
  Deterministic trace-counter gate on a fixed 8-shard x 128-conn x 8-req
  workload (built via `go test`, no `./surge` dep, wall-clock NOT asserted):
  (1) lifecycle-control/req <= 9.0 [~6.0, bit-stable]; (2) steady-state-control/req
  <= 20.0 [~8.1]; (3) `placement_adoptions` > 0 [~253, F2/anti-funnel];
  (4) `accept_owner_active_shards` >= 2 [8]. Counters preferred over wall-clock
  (host-load/client-bound fragility). The 90s stallrepro + full 8x1024 matrix are
  the MANUAL/nightly acceptance runbook (in the task doc), NOT per-commit.
- RV2-DEBT-016: CLOSED (Task 12) — control target met (9.36/req steady-state),
  scaling met. Residuals reassigned: external-await `ctrl_await_compat`
  (RV2-DEBT-022), cross-owner `ctrl_scope` (RV2-DEBT-021). Three-step attribution
  chain (Task 8→9→10) preserved verbatim in the DEBT.md cell (flipped in place to
  Status=Closed to keep it byte-for-byte). RV2-DEBT-006 got a Task 12 note (channels
  script exercised, per-probe-timeout debt unchanged).
- Gates (at commit): `git diff --check`, `make check`, `make runtime-v2-check`
  (incl. new perf stage), `./check_file_sizes.sh -a`, Sentrux root/runtime/native.
  No C touched → c-check/cppcheck N/A. `make runtime-v2-perf-check` standalone
  PASS (~4s). RV2-DEBT-018 policy applies to any VM-harness transient (focused
  rerun count>=5).

## Epic 8 Task 13 Handoff (Large-File And Quality Tranche, coder-t13)

- Scope: refactor/quality tranche (RULES.md Global Rule 4). Behavior IDENTICAL
  (MOVE/RENAME/COMMENT only); the full `runtime-v2-check` battery is the
  before/after proof. Full write-up: `08-tasks/13-large-file-and-quality-tranche.md`;
  evidence in `08-evidence.md` Task 13.
- EXTRACTION: the task park/unpark + key-wake primitive cluster
  (`wake_task_on_shard_locked`/`wake_task_with_policy`/`wake_task`/`wake_net_task`/
  `park_requeue_locked`/`wake_key_all_with_policy`/`wake_key_all`/`park_current`)
  moved VERBATIM from `rt_async_state.c` into new `runtime/native/rt_task_park.c`
  (203 eff LOC). `diff` proved byte-identical except two intended edits: (1) the
  `channel_wake_force_inject` static read → its `channel_wake_force_inject_enabled()`
  accessor (behavior-identical; static stays in state.c), (2) `wake_key_all_with_policy`
  `static`→extern (mark_done drains join waiters across the new boundary; prototype
  added to `rt_async_internal.h`; every call site byte-identical, mark_done untouched).
  All load-bearing invariant comments (Leaf-wake, RV2-DEBT-019 park_key ownership,
  D5 register-then-commit/wake-token, generation-qualified removal) moved verbatim.
- LOC: `rt_async_state.c` 1386→1184 eff (−202); `rt_task_park.c` 203 new;
  `rt_async_internal.h` 555→556 (one prototype). `.loc-legacy-allowlist` ceiling
  lowered 1580→1184 (exact measured, zero headroom). RV2-DEBT-003 stays OPEN with
  three named remaining split candidates (ready-queue / completion-cancel /
  handle-lifetime); completion cluster deliberately NOT moved at that time
  (then-active RV2-DEBT-022 done_cv hot path + a done_cv filename-pinned static
  gate; Epic 9 Task 4 later moved the `done_cv` helper and closed the debt).
- COMMENT: the stale executor-invariant block in `rt_async_internal.h` (had
  drifted to :346-358, described the pre-Epic-7 executor-wide model) rewritten to
  the three-lane model (control / shard / atomic) naming the cross-owner and
  external-await control residuals.
- STATIC-GATE: `TestRuntimeV2AcceptNetOwnershipNoShard0Shortcut` pinned
  `park_current` to `rt_async_state.c` by filename; pin split so `park_current`→
  `rt_task_park.c`, `next_ready` stays. Build/embed/harness all glob `native/*.c`
  → new file auto-discovered, no Makefile/embed edits.
- SWEEP: no stale attribution comments in touched files (completion comments
  already carry Task 10's corrected attribution); no dead code (strict-warning
  compile clean).
- GATES (green): `git diff --check`, `make c-check`, `make cppcheck`, `make check`,
  `make runtime-v2-check` (CompletionPinInterleavingTSan PASS shards 1/2/8;
  PerfControlLaneGate steady-state-control 8.059/req << 20.0 ceiling — no
  control-lane regression; NoShard0Shortcut pin-split PASS), `./check_file_sizes.sh -a`.
  Two runtime-v2-check runs first hit accepted transients (RV2-DEBT-018 empty-output
  on the select test; a net-timing flake on the accept trace test), each proven
  non-reproducible with focused reruns (5/5 green each; the -count=5 select failure
  was RV2-DEBT-011 same-test artifact-dir reuse, not logic); a third clean run was
  fully green.
- SENTRUX: all rules pass at every scope. `sentrux check` code-scope quality
  dropped −40/−41 (0.76%): the inherent inter-module coupling of splitting the
  runtime's hottest interconnect (park↔wake↔ready↔completion mutual recursion),
  ACCEPTED per RULES.md G3 with RV2-DEBT-003 as recovery owner — the tool's own
  `sentrux gate` shows the QUALITY dimension IMPROVED vs the committed baseline
  (runtime/native 5159→5341), its DEGRADED verdict is on cumulative-since-Jul-2
  coupling/complex-fn drift, not this verbatim move.

## 2026-07-06 Docs/Debt Cleanup Handoff

- Scope: docs-only cleanup after Epic 8 closeout review. No runtime code or
  tests changed.
- `README.md`: marked Epic 8 complete, removed the duplicate draft artifact
  entry, added `08-evidence.md`, and changed Epic 9 from "Phase 4 next" to
  "next scope to decide" with safety-debt candidates named first.
- `08-task-lifecycle-lane-and-net-fairness.md`: changed the top status to
  complete, added the closeout summary, and updated the next-runtime handoff
  to name carried debts before syntax/Phase 4 work.
- `DEBT.md`: moved `RV2-DEBT-016` from Open Debt to Closed Debt with the Task
  12/14 evidence links. Open safety debts that should drive the next planning
  pass remain `RV2-DEBT-020`, `RV2-DEBT-022`, and `RV2-DEBT-023`; longer-lived
  cleanup/compat items remain `RV2-DEBT-003` and `RV2-DEBT-017`.

## 2026-07-06 Epic 9 Draft Handoff

- Created `09-wakeup-and-cancellation-safety.md` as a draft epic document only;
  no task slicing yet.
- Scope: runtime-only safety pass before final crossing work. The epic targets
  `RV2-DEBT-022` (`done_cv` external-await ordering), `RV2-DEBT-023`
  (cancel vs RUNNING->WAITING park ordering), and `RV2-DEBT-020`
  (accept-transition join-waiter migration proof/fix).
- Boundary: no Surge syntax, parser, semantic/lowering, stdlib public examples,
  Phase 4 inbound queues, remote messages, eventfd credits, remote `select`,
  remote-free queues, shard-movable checking, or seq-cst Phase 4 `PARKED`
  protocol.
- Refactor rule: `RV2-DEBT-003` may be touched only if the completion/cancel
  split follows the safety fix's real dependency boundary and reduces coupling;
  no cosmetic file split.

## 2026-07-06 Epic 9 Task 1/2 First Slice

- Added the test-only sync-point scaffold (`RT_SYNC_POINT`,
  `RT_SYNC_POINT_IF`, `runtime-v2-syncpoint-check`). Release builds compile the
  hooks to no `rt_sync_point_reach` symbol, and the static gate now allowlists
  placement even when clang-format wraps a conditional hook across lines.
- Closed `RV2-DEBT-023` for the original cancel-vs-RUNNING-to-WAITING
  never-firing-key window: `cancel_task` now forces the wake token
  unconditionally, and the deterministic positive/negative-control proof is in
  `runtime_v2_lifecycle_behavior_syncpoint_test.go`.
- `RV2-DEBT-020` proof was reset deliberately. The abandoned narrowing to a
  net-handle/stdlib blocker is not evidence; Task 3 must enumerate every
  `rt_task_replace_owner` caller and then prove, narrow, or fix the
  accept-transition join-waiter migration case.
- At the Task 1/2 handoff, still pending in Epic 9: `RV2-DEBT-020`,
  `RV2-DEBT-022`, broader cancellation matrix rows, final full
  `runtime-v2-check`/Sentrux/perf closeout. Task 3 later closed
  `RV2-DEBT-020`.

## 2026-07-06 Epic 9 Task 3 Handoff

- Closed `RV2-DEBT-020`. The proof found a real old-order stranding shape: a
  `join_key(task)` waiter can register on the old owner store after migration
  drained it, while completion wakes route to the new owner.
- Fix shape: `rt_task` now carries atomic `join_owner_shard_id`; owner
  replacement publishes the join route under the old route shard lock before
  draining old entries; `WAKER_JOIN` add/remove/pop and completion collect-all
  wake all resolve, lock, and revalidate the route before touching a store.
  This covers all four production `rt_task_replace_owner` shapes: F2 RUNNING
  `current`, accept wake WAITING task, accept ready-now RUNNING task, and accept
  success self-placement RUNNING task.
- `SP_MIGRATE_GAP` is now active in the sync-point allowlist. Positive proof
  passes at `SURGE_SHARDS=2,8`; negative control
  (`RV2_DEBT_020_NEGATIVE_CONTROL`) restores the old order and strands with
  `debt020 migrate-gap joiner stranded`.
- Verification run for this slice: targeted Debt020 proof pair, focused static
  route/owner-local waiter check, `make runtime-v2-syncpoint-check`,
  `make c-check`, `make cppcheck`, and `./check_file_sizes.sh -a` all passed.
  The join-route helper was split into `rt_waiter_join_route.c` to keep
  `rt_async_waiter.c` below the hard file-size gate.
- At the Task 3 handoff, still pending in Epic 9: `RV2-DEBT-022`, broader
  cancellation matrix rows, final full `runtime-v2-check`/Sentrux/perf
  closeout. Task 4 later closed `RV2-DEBT-022`.

## 2026-07-06 Epic 9 Task 4 Handoff

- Closed `RV2-DEBT-022`. External await and completion now use one seq-cst
  StoreLoad handshake: external await increments `done_waiters` seq-cst and
  loads target status seq-cst; completion stores `TASK_DONE` seq-cst and then
  loads `done_waiters` seq-cst before the guarded broadcast.
- The `done_cv` broadcast moved into `runtime/native/rt_done_cv.c`. It is still
  external-await compatibility only, takes/binds `ex->lock`, tags
  `RT_CTRL_SITE_AWAIT_COMPAT`, and the static gate pins exactly one broadcast
  call site there. Worker joins remain `done_cv`-free.
- Deterministic proof: `SP_AWAIT_AFTER_INCREMENT`,
  `SP_AWAIT_BEFORE_DONECV_WAIT`, and
  `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD` reproduce the window. Positive proof
  passed at `SURGE_SHARDS=1,2,8`; negative-control build strands with
  `debt022 external awaiter stranded before done_cv wait`.
- External-await matrix now covers multi-awaiters, already-DONE target, parked
  target woken by channel send, and cancelled parked target. The cancelled row
  is the explicit `RV2-DEBT-022` x `RV2-DEBT-023` intersection.
- Gates run for the slice: focused Debt022 proof/matrix, focused static gate,
  `make runtime-v2-syncpoint-check`, `make c-check`, `make cppcheck`,
  `./check_file_sizes.sh -a`, `make runtime-v2-lifecycle-check`,
  `make runtime-v2-perf-check`, `make runtime-v2-check`, and `make check`
  passed. Perf counters recorded from the full runtime-v2 check:
  `control_lock_acquired=11819`, `ctrl_await_compat=3458`,
  steady-state-control `8.165/req`, lifecycle-control `5.999/req`.
- Full `runtime-v2-check` initially exposed a test-harness stale assumption from
  Task 3: `TestRuntimeV2OwnerLocalNetWaiterBehavior` used `stub_tasks[8]` while
  registering a join target with id `55`; the new join-route lookup correctly
  required that target to exist. The harness now uses `stub_tasks[128]`, and the
  focused test plus the full gate pass.
- Sentrux `check` passes at root/runtime/runtime-native (`6177`, `5327`,
  `5430`). `sentrux gate` still reports cumulative degradation vs the old
  baseline on complex-function count and runtime/native coupling; this remains
  under `RV2-DEBT-003`, with runtime/native quality improved (`5159 -> 5430`).
- Task 4 handoff before closeout: closeout sweep and the `RV2-DEBT-003`
  completion/cancel split decision still needed final reconciliation. The
  closeout entry below records that reconciliation.

## 2026-07-06 Epic 9 Closeout

- Epic 9 is closed for its three owned local safety debts. Final code commits:
  `dfbf5897` (`RV2-DEBT-023` cancel wake-token proof), `ff57b8a2`
  (`RV2-DEBT-020` join-route migration proof), and `82c633a7`
  (`RV2-DEBT-022` external-await `done_cv` StoreLoad proof).
- Task 5 closeout created `09-tasks/05-epic-closeout.md` and reconciled
  `09-evidence.md`, the Epic 9 document, the task index, the epics README, and
  `docs/RUNTIME_V2.md`.
- Debt state: `RV2-DEBT-020`, `RV2-DEBT-022`, and `RV2-DEBT-023` are CLOSED in
  `DEBT.md`; `RV2-DEBT-003` remains OPEN for dependency-aware cleanup and
  cumulative Sentrux coupling/complexity recovery. No new Epic 9 debt was
  opened.
- Broader cancellation matrix rows named during planning, such as a
  sleep-specific row or cancel during `wake_key_all` mid-drain, did not receive
  dedicated new sync-point tests. They are recorded as future matrix-hardening
  candidates, not as known correctness debt.
- Final code gates are from Task 4: `make runtime-v2-check`, `make check`,
  `make c-check`, `make cppcheck`, `make runtime-v2-syncpoint-check`,
  lifecycle/perf checks, LOC, and required Sentrux `check` scans passed.
  Sentrux `gate` still reports the existing `RV2-DEBT-003` cumulative recovery
  class.
- Next planning should choose between dependency-aware cleanup, net-handle /
  stdlib owner-safety, benchmark/test harness hardening, or Phase 4 crossing.
  Any syntax, keyword, parser, semantic, lowering, or public crossing surface
  work must stop first for the dedicated language-syntax review.

## 2026-07-07 Epic 10 Start (Task 1: Dependency And Debt Map)

- Epic 10 (`10-runtime-debt-burndown-and-owner-safety.md`) implementation
  starts. Owned debts: `RV2-DEBT-003` (dependency-aware `rt_async_state.c`
  split), `RV2-DEBT-010` (copied/stale net-handle safety), `RV2-DEBT-013`
  (stdlib HTTP owner-shard safety). No language syntax, no Phase 4 transport.
- Pre-implementation Sentrux baselines (CLI `sentrux check`, all rules pass):
  root `6178`, `runtime` `5326`, `runtime/native` `5428`. Paths:
  `/home/zov/projects/surge/surge`, `.../runtime`, `.../runtime/native`.
- Baseline `make c-check` passed on the starting checkout.
- `rt_async_state.c` current shape: 1426 raw / 1184 effective LOC (allowlist
  ceiling 1184). Cluster map derived from code: (A) executor bootstrap/init +
  worker start + TLS + debug (~l.1-355), (B) task/scope slot accessors + child
  caps (~l.357-492), (C) ready-queue cluster (`scheduler_runnable_is_empty`,
  `rt_sched_idle_sample_locked`, `sched_next_u64`, `current_worker_scheduler`,
  `current_local_queue`, `pop_task_from_deque`, `ready_push*`,
  `ready_take_current_local_tail`, `ready_pop`, `worker_next_ready`), (D)
  virtual clock/sleep tick + N=1 runner (`tick_virtual`,
  `rt_next_sleep_deadline`, `advance_time_to_next_timer`, `next_ready`), (E)
  handle helpers + lifetime (`task_from_handle`, `task_add_child`,
  `scope_add_child`, `scope_remove_child`, `task_add_ref`, `free_task`,
  `task_release`, `task_release_lane_aware`, `clear_select_timers`), (F)
  completion/cancel (`current_task_cancelled`, `cancel_task`,
  `mark_done_needs_control`, `mark_done`, `apply_poll_outcome`).
- External-caller map recorded (grep over `runtime/native/*.c`): ready-queue
  consumers are `rt_worker_turn.c`, `rt_async_task.c`, `rt_async_poll.c`,
  `rt_async_blocking.c`, `rt_task_park.c`; completion consumers are
  `rt_async_poll.c`, `rt_async_scope.c`, `rt_async_task.c`, `rt_worker_turn.c`,
  `rt_async_select.c`; handle-lifetime consumers are `rt_async_task.c`,
  `rt_async_select.c`. Makefile globs `runtime/native/*.c`, so new files need
  no build wiring.
- Net-handle current shape (`rt_net_handles.h`): `NetConn` is a heap struct
  `{fd, closed, owner_shard_valid, owner_shard_id}`; `TcpConn.__opaque` is an
  `int` carrying the `NetConn*`. `stdlib/http/server.sg` sends that int through
  `Channel<int>` to pre-spawned `serve_worker` tasks (placement decided at
  server start, not per connection) — the RV2-DEBT-013 surface.
- Subagent maps in flight: `net-mapper` (RV2-DEBT-010 surface) and
  `http-mapper` (RV2-DEBT-013 surface + placement mechanism). Task 1 document
  `10-tasks/01-dependency-and-debt-map.md` will consolidate both.

## 2026-07-07 Epic 10 Task 2 (RV2-DEBT-003 split) Implementation

- Task doc: `10-tasks/02-debt-003-state-split.md` (written model + cluster map
  + repointed static gates). Verbatim moves only, no behavior change.
- New files: `runtime/native/rt_ready_queue.c` (378 eff LOC; shard ready-queue
  mutation + worker pop policy, every mutation under the owner shard lock),
  `runtime/native/rt_task_complete.c` (203 eff LOC; cancel_task/mark_done/
  apply_poll_outcome/clear_select_timers/current_task_cancelled),
  `runtime/native/rt_task_lifetime.c` (67 eff LOC; task_add_ref/free_task/
  task_release/task_release_lane_aware).
- `rt_async_state.c`: 1184 -> 537 effective LOC; entry REMOVED from
  `.loc-legacy-allowlist` (now rides the normal gate, OK band). It keeps
  executor bootstrap/config/TLS, task/scope slot accessors (needle-pinned by
  lifecycle static gates — deliberately not moved), virtual clock + N=1
  runner, and child/handle helpers.
- Header change: `ready_push_yielded_task` static -> extern
  (`rt_async_internal.h`), needed by `apply_poll_outcome` across the new
  module boundary. `rt_sync_point.h` include moved with the completion
  cluster.
- Static gates repointed: `check_sync_points.sh` SP_CANCEL_BEFORE_WAKE +
  SP_MARKDONE_BEFORE_DONEWAITERS_LOAD -> `rt_task_complete.c`;
  `readRuntimeV2SchedulerStateSource` -> `rt_ready_queue.c`; lifecycle
  `done_cv` confinement scan EXTENDED to also scan `rt_task_complete.c`.
- Gates so far: `git diff --check` OK, `make c-check` OK, `make cppcheck` OK,
  `./check_sync_points.sh` OK, `./check_file_sizes.sh -a` OK (100%).
- Sentrux after split (CLI `sentrux check`, all rules pass): root `6179`
  (baseline 6178, +1), runtime `5298` (baseline 5326, -28), runtime/native
  `5399` (baseline 5428, -29). `sentrux gate runtime/native` vs the committed
  Jul-2 baseline: quality `5159 -> 5399` (UP +240); DEGRADED verdict repeats
  the KNOWN RV2-DEBT-003 cumulative recovery class with IDENTICAL numbers to
  the Epic 9 Task 4 record (coupling `0.00 -> 0.06`, complex functions
  `21 -> 23`) — this task added no new measured coupling and no new complex
  functions; the scoped `check` drop is the same make-hidden-coupling-visible
  accounting class recorded at Epic 8 Task 13, now with the split landed and
  the allowlist entry gone.
- FINAL Task 2 gates: `make runtime-v2-check` PASSED (full stage list incl.
  lifecycle, perf control-lane gate, accept, lock split, syncpoint) and
  `make check` PASSED (test + lint + c-check + file sizes) on the split tree.
  `RV2-DEBT-003` CLOSED in `DEBT.md` with the Sentrux coupling re-check
  evidence. Task doc: `10-tasks/02-debt-003-state-split.md`.
- Task 1 map is consolidated in `10-tasks/01-dependency-and-debt-map.md`
  (scheduler clusters re-derived in-session; net-handle and stdlib HTTP maps
  from the `net-mapper`/`http-mapper` research subagents, cross-checked).
  Decisions it feeds: Task 3 stamps registry generations into net handles +
  owner-locked data-path validation with status-code reject; Task 4 replaces
  the HTTP `Channel<int>` worker pool with owner-local per-connection serving
  bounded by a token channel.

## 2026-07-07 Epic 10 Task 3 (RV2-DEBT-010 net-handle contract) Implementation

- Task doc: `10-tasks/03-debt-010-net-handle-contract.md` (with the mid-task
  design revision). LOAD-BEARING ABI DISCOVERY, verified by pointer tracing:
  `TcpConn.__opaque` is the first 8 bytes of `NetConn` (packed fd/closed/
  owner_shard_valid), NOT a pointer value at the language level; reconstructed
  `{ __opaque = handle }` boxes are 8 bytes and reading `owner_shard_id` or
  anything past byte 8 on them was pre-existing OOB UB. `__opaque` ints are
  raw words: Surge code may only move them (arithmetic segfaults in
  rt_bigint). Listener copies survive only via the canonical fd-keyed
  listener registry.
- Contract landed: `NetConn.generation_check` (16-bit, in former padding
  bytes 6-7, write-once at registration), `NetListenerMember.generation`
  (full 64-bit, members never reconstructed); `rt_net_conn_probe_open`
  (hint-first per-shard locked probe; first REGISTERED row decides;
  interest-only rows neither validate nor veto); guard calls in
  read/write/read_bytes/write_bytes/wait/close; accept stamps + validates
  members; `rt_net_close_fd_on_owner` gained (generation, generation_check16)
  and revalidates under the owner lock (one closer wins). fd registry gained
  an fd→index dense map (O(1) find; maintained only in
  fd_registry_create_row/fd_registry_remove_at).
- `rt_net.c` LOC gate: guard additions pushed it 666→708 eff; extracted the
  NetResult/NetError constructor cluster verbatim to `rt_net_result.c`
  (160 eff) + `rt_net_result.h` (30) — `rt_net.c` now 537 eff (OK band).
- Proof: `TestRuntimeV2NetHandleStaleCopyReusedFD` (SHARDS=1,2,8) green;
  static shape gate green; new `runtime-v2-net-handle-check` stage wired into
  `runtime-v2-check`. Negative control: fixture built at pre-guard `ec7721c5`
  HANGS (stale read parks on the reused fd's readiness) vs exit 0 now.
- Bench (256-conn direct/pipe; the 1024-conn rows connection-reset on this
  WSL2 host even at the pre-Task-2 commit — verified in a worktree, recorded
  as host limitation, RV2-DEBT-006-adjacent): 1x256 10251.18→10262.97 us/op
  (noise); 8x256 10204.33→10464.74/10123.29 across two runs (within
  variance). Guard hint hits the first lock on owner-local traffic.
- Sentrux after Task 3 (CLI, all rules pass): root `6175`, runtime `5306`,
  native `5402`; `sentrux gate runtime/native`: coupling `0.00 -> 0.05`
  (BETTER than the Epic 9 record's 0.06), complex functions `21 -> 23`
  (unchanged vs record), quality above committed baseline.
- `RV2-DEBT-010` CLOSED in `DEBT.md` with three NAMED narrowed residuals
  (16-bit check aliasing ~2^-16; listener canonical-rebind race; POSIX
  concurrent-close window). Fixture insight for Task 4: the stdlib HTTP
  `Channel<int>` handoff carries the packed handle word, so reconstructed
  worker-side conns are exactly the guarded-handle class.

## 2026-07-07 Epic 10 Task 3 Correction + Task 4/5 Closeout

- Task 3 final implementation was corrected after the 16-bit handle-word plan:
  `TcpConn.__opaque` and `TcpListener.__opaque` now carry stable runtime handle
  ids, not fd-shaped packed words, OS fds, or native pointers. Native net
  entrypoints canonicalize copied handles through the handle table before
  reading fd/owner/closed/generation fields. The fd-registry generation remains
  the owner-locked lifetime proof for live canonical handles.
- `RV2-DEBT-010` ledger and `10-tasks/03-debt-010-net-handle-contract.md` were
  updated to the stable-handle-id contract. Important residual: handle ids are
  monotonic and not reused; this is intentional for stale-copy safety and is
  acceptable until Surge-visible net objects get a destructor/free path.
- Task 4 closed `RV2-DEBT-013`: `stdlib/http` no longer sends raw
  `TcpConn.__opaque` through `Channel<int>`. New `stdlib/http/accept.sg`
  contains the fixed local accept worker; `server.sg` spawns those workers with
  copied listener handles and handles each accepted conn directly via
  `serve_conn`.
- `accept_timeout_ms` note: native Runtime V2 timers are executor timers, not a
  wall-clock readiness mechanism for external Go clients. The HTTP owner
  behavior gate therefore runs the server with `accept_timeout_ms = 0`, proves
  real HTTP responses at `SURGE_SHARDS=1,2,8`, and terminates the process from
  the harness.
- Gates after Task 4: `make runtime-v2-http-owner-check`,
  `go test ./internal/vm -run TestMTCorrectnessHTTPServer -count=1`,
  `make runtime-v2-net-handle-check`, `make runtime-v2-accept-check`,
  `make c-check`, `git diff --check`, commit-hook `make check`, and Sentrux
  `/runtime` (`quality_signal=5345`, rules pass, `0` violations).
- Code commit: `9d1b06c1 fix(runtime): stabilize net handles and http
  ownership`. It includes `STATS.md` updated by the pre-commit hook.

## 2026-07-08 — `crosses` keyword removed (design change D17)

The explicit `crosses` keyword has been REMOVED from the language; the crossing
effect is now inferred at semantic analysis and stored in function metadata (no
surface keyword, no programmer-facing requirement). The `crosses` grammar was
removed (`fn f() crosses -> T` no longer parses: `SYN2012`/`SYN2205`),
`Signature.Crosses` and its setter were deleted, and `SEM3162`/`SEM3163`/`SEM3164`
are retired (numbers reserved). `on`, `spawn on`, and `far Task<T>`
await/cancel are valid in any function. Update after Block 4 completion:
direct sema inference is implemented; `RV2-DEBT-024` now tracks only
higher-order/function-type effect propagation and possible cross-module export
propagation.

This SUPERSEDES earlier keyword-strategy notes in this log — including the
"`on` and `crosses` contextual keyword" strategy and the Block 4 "`crosses`
effect parsing" grammar prerequisites — for the `crosses` keyword specifically
(`on` remains a contextual keyword; `far` remains hard-reserved). The
crosses-requirement negatives are PARKED (not deleted) in
`testdata/golden/crossing/crosses_deferred/` — Block 3's four X03/X04/T07/T08
fixtures and Block 2's `on_negative_missing_crosses` — each carrying a
`FUTURE-ASSERT:` note for the coming semantic crosses-inference; the directory is
not wired into the crossinggate harness and is skipped by `golden_update.sh`.
Block 2 also had `crosses` stripped from every remaining fixture in this cleanup.

## 2026-07-08 — Epic 11 Block 4 crossing contracts implemented

Block 4 is now implemented without a public `crosses` keyword. Sema records
`Result.FunctionEffects[fn].MayCross` for direct `on`, `spawn on`,
`far Task<T>.await()`, `far Task<T>.cancel()`, and transitive direct-call
propagation. The active Block 4 fixtures were rewritten to valid marker-free
source; historical keyword-era fixtures were moved under
`testdata/golden/crossing/crosses_deferred/block04/`.

Shard contracts are active: `@shard_movable`/`@shard_pinned` type-only target
checks (`SYN2016`), conflict checks (`SEM3172`), recursive shard-movable
field/member validation (`SEM3171`), `@send`/`@copy`-only owned crossing
diagnostics (`SEM3169`/`SEM3170`), and `@nosend`/`@shard_pinned` crossing
diagnostics are gated by `internal/crossinggate`. `TcpListener` now matches
`TcpConn` as `@shard_pinned @nosend`. Ordinary `spawn` keeps its established
`SEM3086` nosend diagnostic; `SEM3166` is for `on` / `spawn on` crossing
boundaries.

Debt state updated: `RV2-DEBT-024` is no longer "effect inference deferred";
the remaining debt is higher-order/function-type effect propagation and possible
cross-module export propagation if Phase 4 lowering proves it needs caller
effects across module boundaries.

## 2026-07-08 — Epic 11 Draft 9 documentation closeout

The connected language docs now describe the accepted Epic 11 surface instead
of candidate names: `LANGUAGE.md` / `LANGUAGE.ru.md` are Draft 9, `far T` is a
type modifier, `on dst { ... }` is the immediate placement-crossing block,
`spawn on dst { ... }` returns `far Task<T>`, crossing effects are inferred, and
`crosses` remains an ordinary identifier.

`ATTRIBUTES.md` / `ATTRIBUTES.ru.md` document `@shard_movable` and
`@shard_pinned`; `CONCURRENCY.md` / `CONCURRENCY.ru.md` explain the compile-time
crossing surface without promising Phase 4 execution; `RUNTIME_V2.md` now points
Phase 4 at lowering/transport work instead of syntax selection.

Public runtime examples remain intentionally deferred: Epic 11 proves parser,
semantic checks, golden fixtures, and backend-unavailable guards, but does not
provide cross-shard transport. The next epic should be split into compile-time
usage/wiring first, then real backend/lowering.

The nested `vscode-extension` repository was updated separately for Epic 11
highlighting and versioned as `0.0.15` in commit `e703523` (not committed into
the parent Surge repository).

## 2026-07-08 — Epic 12 proposed document drafted

Proposed next epic document:
`12-crossing-surface-integration-and-lowering-readiness.md`.

Scope: bridge Epic 11's compile-time crossing surface into compiler/backend
readiness without implementing Phase 4 transport. The document explicitly
forbids hidden local fallback for `on` / `spawn on` / `far Task` operations,
requires deterministic backend-unavailable diagnostics before transport exists,
and requires a written lowering-readiness contract that preserves crossing
destination/result/effect metadata through the real compiler path.

The old "Epic 12 test/backend matrix rewrite" placeholder is not dropped.
`DEBT.md` was updated so `RV2-DEBT-001`, `RV2-DEBT-002`, `RV2-DEBT-011`, and
`RV2-DEBT-018` are reconciled by Epic 12 if they block crossing backend rows, or
reassigned to a later named backend/test-harness epic during Task 1. `RV2-DEBT-024`
is a decision point for this epic: implement higher-order/cross-module effect
propagation only if lowering readiness proves it is needed now; otherwise
reaffirm the deferral with evidence.

No `12-tasks/` directory exists yet. The next discussion should review and
accept/rework the epic document before task slicing.

## 2026-07-08 — Epic 12 document revised and task slices drafted

The Epic 12 document was revised after an expert review against the current
compiler state: the representation decision (guard-before-HIR vs
lower-into-HIR-then-guard) is now forced into Task 1 with a recorded
rationale; the backend-unavailable guard contract gained default-closed
gating, an ICE-on-bypass requirement, and a tested negative space (LSP /
check-only paths must stay clean on valid crossing code); matrix taxonomy is
aligned to the real backend set (`BackendVM`, `BackendLLVM`); the current
FUT7014-7017 messages are recorded as violating the new wording contract
("Phase 4" is an internal number) with fixture churn expected in Task 2; and
the `RV2-DEBT-024` criterion is sharpened to "required iff the chosen
representation layer needs effect bits on imported function symbols".

`12-tasks/` was drafted: `README.md` (index, order ruling, per-task gates)
plus six task documents — 01 dependency/debt/representation map (binding
decisions), 02 backend-unavailable diagnostic contract, 03 lowering-readiness
representation (owns the DEBT-024 decision), 04 controlled compile-time usage
fixtures, 05 test harness hardening (DEBT-011/018; may be promoted ahead of
02/04 by Task 1's map), 06 CI gates and closeout. The epic and task documents
are pending review together; no implementation has started.

## 2026-07-08 — Epic 12 Task 1 Closeout

Task 1 is complete as a documentation/evidence task. The binding
representation decision is **guard-before-HIR**: crossing constructs remain
blocked at `buildpipeline.Compile` until a real transport backend exists, and
Task 3 will add a sema-derived lowering-readiness record instead of introducing
`on` / `spawn on` HIR or MIR nodes.

The throwaway HIR-bypass spike disabled the crossing backend guards and built a
valid `on pool { ret 1; }` program with both VM and LLVM backends. Both paths
failed with `MIR validation failed: function crossing_value: bb0: return
without value in non-nothing function`, proving that `ExprOn` reaching HIR is
currently an unsafe bypass (`internal/hir/lower_expr.go` silently returns `nil`
for unhandled expression kinds). Task 2 must add an explicit ICE-on-bypass
guard.

Debt disposition: `RV2-DEBT-001` and `RV2-DEBT-002` remain open but are
reassigned to the named future **Backend/Test Matrix Cleanup** epic because
their broad VM/LLVM failures are not needed for Epic 12 compile-time crossing
readiness. `RV2-DEBT-011` and `RV2-DEBT-018` are not promoted before Tasks 2/4:
a duplicate `TestLLVMBuildPortable` overlap probe passed 20/20 processes, and
focused crossing/sema probes passed; the full `make runtime-v2-check` baseline
also passed. They stay in Task 5 only if later Epic 12 work starts using VM
artifact helpers; otherwise they also move to the backend matrix cleanup track.
`RV2-DEBT-024` remains Task 3's decision point.

## 2026-07-08 — Epic 12 Task 2 Closeout

Task 2 is complete. The backend-unavailable crossing guards are now
default-closed for every non-empty backend until a backend is explicitly marked
transport-capable; an empty backend remains the compile-only/no-backend path.
Frozen FUT7014-FUT7017 messages no longer mention internal epic or phase
numbers.

HIR lowering now reports a deterministic internal compiler error if an
`ast.ExprOn` bypasses the buildpipeline guard. Crossinggate backend-stage rows
run through both VM and LLVM backend selections with `buildpipeline.Compile`;
no VM artifact helpers or executable outputs were introduced, so Task 5
promotion was not needed. Backend-unavailable guards now run only after sema
accepts the module, so sema-invalid crossings do not get extra FUT7014/FUT7015
noise. The direct-call/inferred-crossing backend row is not claimed by Task 2;
Task 3 owns that through the lowering-readiness metadata record and
`RV2-DEBT-024` decision.

Negative-space tests cover diagnose, LSP-facing diagnostics, format-check, and
fix paths for valid crossing code. Proof gates run for this closeout:
focused `go test` for buildpipeline/HIR/crossinggate, `make golden-check`,
`make check`, full LOC scan, root Sentrux check (rules pass), scoped
`sentrux check internal` (existing missing `internal/.sentrux/rules.toml`
recorded, no scoped compliance claimed), guard-text grep, and `git diff
--check`.

## 2026-07-08 — Epic 12 Task 3 Closeout

Task 3 is complete. Sema now exposes `Result.CrossingLowering`, an explicit
guard-before-HIR readiness record for accepted crossing forms: `on` placement,
`on` far-handle, `spawn on`, `far Task.await`, and `far Task.cancel`. The
record carries destination expression/type, far-handle anchor verdict, accepted
capture summaries, accepted remote ops for anchored far handles, payload/result
/ handle types, receiver/consume data for far-task operations, and a function
symbol back-reference to `FunctionEffects`.

Capture summaries are produced by the same classification path that emits the
SEM3165-SEM3169 diagnostics, so the record does not introduce a second copy of
the crossing-capture rules. Record appends are guarded by a per-site sema-error
checkpoint: invalid crossing bodies, rejected remote operations, and rejected
far-task handle reuse do not leave accepted-looking lowering records. Backend
guards were not switched to the record in this slice; they remain Task 2
diagnostic guards, while future backend/lowering consumers should use the sema
readiness record.

`RV2-DEBT-024` was narrowed but not closed. The two-module driver fixture proves
imported function effect bits are not required for the current readiness layer:
module B's imported call does not synthesize a lowering record, while module A's
dependency sema result keeps the real `on` crossing-site record. Higher-order
and cross-module caller-effect propagation remains deferred to Phase 4 transport
lowering or a later effect-system epic.

Proof run for this closeout:
`go test ./internal/sema -run 'CrossingLowering|FunctionCrossingEffect' -count=1`,
`go test ./internal/driver -run 'CrossingReadinessDebt024' -count=1`, and
`go test ./internal/sema ./internal/buildpipeline ./internal/crossinggate -count=1`.
After independent review caught false-record risk, added negative record tests
for invalid on/spawn bodies, rejected far-handle remote I/O, double await, and
await-after-cancel, then re-ran
`go test ./internal/sema ./internal/buildpipeline ./internal/crossinggate ./internal/driver -run 'Crossing|SpawnOn|FunctionCrossingEffect' -count=1`.
Also run: `git diff --check`, `./check_file_sizes.sh -a`, root Sentrux scan
quality `6187` with rules pass, and `sentrux check internal` confirming the
existing missing `internal/.sentrux/rules.toml` rule-file gap. No scoped
`internal` rule compliance or quality score is claimed. No golden fixtures were
added for Task 3.

## 2026-07-09 — Epic 13 Task 1 Closeout

Task 1 is complete as a documentation/evidence task. The documentation commit
that introduced Epic 13 is `094f4a39 docs(runtime): plan Epic 13 transport
vertical`; no production code was changed by Task 1.

Binding decisions for the rest of the epic:

- `far Task<T>` uses the existing stable task identity as the first runtime
  anchor but must carry owner-shard routing plus a mandatory generation/no-reuse
  token. Raw pointer-only remote handles are rejected. Affine single-consumer
  far handles are reaffirmed as a transport invariant.
- Placed children created by `spawn on` are detached from the enclosing local
  scope; the affine handle is the lifecycle edge until distributed scopes exist.
  Publication wait is non-cancellable before ack, and Epic 13 owns cleanup for
  publication waits, far await/cancel waiters, far-handle local cleanup on
  failfast/unwind exits, and shutdown drain.
- Executable crossing payloads are restricted to plain-data/copyable
  representations. Heap-owned moves wait for allocator-owner/remote-free proof;
  the current `rt_free` path accounts frees to the current heap cell.
- Executable placements are `shard(id)` and `distributed`. Out-of-range
  `shard(id)` does not clamp or modulo; it takes a deterministic non-executing
  placement-error/cancel path with trace visibility. `distributed` is
  round-robin and must prove a non-caller owner shard when `SURGE_SHARDS>1`.
- Reply waits suspend tasks, never shards. The `SURGE_SHARDS=1` self-crossing
  row is mandatory to catch deadlocks where the only worker parks instead of
  draining transport.
- `RV2-DEBT-024` is not needed for this vertical: direct crossing-site
  readiness records plus dependency-module guard scans are enough. Hidden
  ordinary-function/higher-order crossing effects move to a later effect-system
  epic.

Debt updates: `RV2-DEBT-011`/`018` are promoted narrowly into Epic 13 Task 2
for the transport-gate VM harness path; the broad matrix cleanup remains
future work. `RV2-DEBT-025` is reassigned to a later explicit far-copy
capability/design-review epic after Phase 4 transport is stable. `RV2-DEBT-026`
is reassigned to a later collections/crossing epic.

Baseline before code: `make runtime-v2-crossing-check` passed twice, `make
runtime-v2-check` passed, `make check` passed in the docs commit hook,
`./check_file_sizes.sh -a` passed with 745 files / 0 over limit / overall
`ОТЛИЧНО`, and sentrux passed at root quality `6189`, runtime `5345`,
runtime/native `5460`, internal `6532`.

## 2026-07-09 — Epic 13 Task 2 Closeout

Task 2 is complete as a harness-hardening task for the Epic 13 transport gate.
The implementation is test-only under `internal/vm`; no production
runtime/compiler code was changed.

What changed:

- `newTestArtifacts` now creates a per-run `MkdirTemp` directory under
  `target/debug/.tests/` and derives the source basename from that directory,
  so identical logical test names no longer share artifact dirs, LLVM output
  binaries, or LLVM tmp dirs.
- Successful tests remove their output binary, tmp dir, registry entry, and
  artifact dir. Failed tests retain artifacts and log artifact dir, binary
  stat, tmp dir, repro command, and `run.diagnostics` when present.
- LLVM run helpers write `run.diagnostics` for non-zero empty-output exits and
  timeout/non-`ExitError` fatal paths. Diagnostics are returned separately and
  do not mutate stdout/stderr, so existing negative tests keep their normal
  assertion surface.
- `runBinaryWithTimeout` can resolve artifact metadata for binaries produced
  by `buildLLVMProgramFromSource`, which gives the MT executor rows the same
  attribution path and preserves `run.stdout`, `run.stderr`, and
  `run.exit_code` for retained artifacts.

Proof added:

- `TestVMTestArtifactsArePerRunUnique` checks two helper invocations in one
  test get distinct artifact/output/tmp paths.
- `TestVMTestArtifactsOverlapStress` runs ten parallel LLVM build/run subtests
  with the same logical name and verifies unique artifact/output/tmp paths.
- `TestRunBinaryWithTimeoutReportsEmptyOutputDiagnostics` proves an
  empty-output non-zero process keeps empty stdout/stderr while returning
  diagnostic metadata and writing retained run artifacts.

Verified during the task:

- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestVMTestArtifactsOverlapStress$' -count=1 -parallel=10 -p=1 -v --timeout 180s`
  passed.
- `go test ./internal/vm -run '^(TestVMTestArtifactsArePerRunUnique|TestRunBinaryWithTimeoutReportsEmptyOutputDiagnostics)$' -count=10 -parallel=2 -p=1 -v --timeout 60s`
  passed.
- `make runtime-v2-check` passed twice consecutively after the final
  run-artifact retention update. Final perf rows included
  `steady-state-control=8.230/req` and then `8.122/req`, with
  `accept_owner_active_shards=8`.
- `make check` passed after the lint cleanup (`intrange` loop style).
- `./check_file_sizes.sh -a` passed: 745 files, 0 over limit, 100% good.
- `sentrux check .` passed with quality `6189`; `sentrux check internal`
  passed with quality `6531`.
- An intermediate second full-gate attempt failed in `TestMTChannelParkUnpark`
  with non-empty stderr `panic: async: double poll`. Focused rerun
  `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 SURGE_MT_TIMEOUT_SCALE=3 go test ./internal/vm -run '^TestMTChannelParkUnpark$' -count=10 -parallel=1 -p=1 -v --timeout 120s`
  passed 10/10. This is recorded as `RV2-DEBT-027` because it is a runtime
  liveness flake, not an artifact/empty-output harness failure.
- Independent rereview found no blockers/P1/P2 after the diagnostics and
  overlap-stress corrections.

Debt disposition:

- `RV2-DEBT-011`: the transport-gate VM helper slice is closed by per-run
  unique artifact/output/tmp paths plus overlap stress. The row remains open
  for broad VM/backend command orchestration under Backend/Test Matrix Cleanup.
- `RV2-DEBT-018`: instrumented but not closed. One implementation-time
  empty-output occurrence was retained with `run.diagnostics`, which proves
  attribution is available, but it does not prove the root cause impossible.
- `RV2-DEBT-027`: opened for the rare non-empty-stderr `async: double poll`
  liveness failure observed while trying to satisfy the second full-gate pass.

## 2026-07-09 — Epic 13 Task 8 Closeout

Task 8 is complete as the guarded `spawn on` publication/codegen vertical. It
does not flip public crossing support: normal production compiles still fail
closed with FUT7015 until Task 9 adds `far Task.await()` / `far Task.cancel()`
and flips the joint capability gate deliberately.

What changed:

- Buildpipeline compile requests gained a test-scoped crossing-form override,
  and dependency-module HIR/MIR combine/lower/validate paths propagate it.
- MIR `InstrCrossing` now carries spawn-on state, pending-slot, and synthetic
  remote-poll function metadata; async liveness/state-machine lowering keeps
  pre-suspend inputs and retry state.
- LLVM lowering emits `rt_remote_spawn_publish_placement`, persistent pending
  retries, async task suspension, deterministic status-to-panic paths, and no
  local spawn fallback.
- Native runtime gained the placement publication wrapper and distinct invalid
  vs unsupported placement statuses. Static VM checks pin
  `rt_far_task_handle` to the codegen allocation shape.

Boundaries and follow-up:

- Full source-level user e2e that consumes the returned `far Task<T>` remains
  Task 9, because Task 8 intentionally provides no await/cancel discharge path.
- Non-async `fn -> far Task<T>` is not opened as a public executable form by
  this task; the production guard remains the safety boundary.
- `pool`, immediate `on`, far-handle `on`, remote channels/select,
  distributed scopes, and remote-free ownership remain outside Task 8.

Verified:

- `go test ./internal/mir -run 'Crossing|SpawnOn' -count=1`
- `go test ./internal/buildpipeline -run 'Crossing|SpawnOn' -count=1`
- `go test ./internal/backend/llvm -run 'Crossing|SpawnOn|Placement' -count=1`
- `go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2RemotePublication(APIShape|Behavior|FailurePathStaticGuards)$' -count=1`
- `go test ./internal/driver -run 'Remap|HIR|Crossing|Module' -count=1`
- `make runtime-v2-crossing-check`
- `make c-check`
- `make cppcheck`
- `make check`
- `sentrux check .` quality `6184`; `sentrux check internal` quality `6528`;
  `sentrux check runtime` quality `5360`; `sentrux check runtime/native`
  quality `5484`.

## 2026-07-10 — Epic 13 Task 9 Start

Task 9 resumes from the uncommitted await/cancel draft after committed Task 8
(`55fb97b5`). The task must not flip production LLVM crossing capability until
remote `far Task` await, cancel, stale-token handling, owner-routed reply waits,
teardown release, and the plain-data/copyable payload boundary are proven
together. `spawn on`, await, and cancel remain one joint public gate.

Initial focused evidence on the inherited draft:

- `go test ./internal/mir ./internal/backend/llvm -run
  'Crossing|FarTask|SpawnOn' -count=1 --timeout 120s` passed.
- `go test -tags runtime_v2_pending ./internal/vm -run
  '^TestRuntimeV2RemotePublication(APIShape|Behavior|FailurePathStaticGuards)$'
  -count=1 --timeout 180s` passed.
- `make c-check` and `make cppcheck` passed.
- `git diff --check` passed.
- `./check_file_sizes.sh -a` passed the repository threshold, but the new
  612-line `rt_remote_task.c` draft must be split before completion.

Review before implementation found mandatory blockers: reply envelopes do not
yet validate route/generation, `WAKER_REMOTE_TASK_REPLY` falls through the
shard-0 waiter route, reply re-registration lacks register-then-verify, handle
allocations have retry/consume leak paths, `rt_far_task_release` has no lowering
callsite, executable heap-owned payloads are not rejected, required 1/2/8 and
race/teardown/shutdown rows are missing, and the production capability remains
default-closed.

Pre-change Sentrux MCP baselines, all with `check_rules` passing:

- repository root quality `6183`;
- `internal/` quality `6527`;
- `runtime/` quality `5362`;
- `runtime/native/` quality `5486`.

The active Sentrux delta session is rooted at `runtime/native/` with baseline
quality `5486`. Task 10 and later forms remain out of scope until Task 9 is
closed and reviewed.

## 2026-07-10 — Epic 13 Task 9 Complete

Task 9 delivers the `far Task<T>.await()` / `far Task<T>.cancel()` executable
vertical and the joint public gate for `spawn on` + await + cancel.
`backendSupportsCrossingForm(LLVM, spawn_on | far_task_await |
far_task_cancel)` is default-open in production
(`internal/buildpipeline/crossing_transport.go`); no override env var exists —
`CrossingFormsForTest` remains a test-scoped struct field whose forms union
with the open production set.

Every mandatory blocker from the start-entry review is closed in the shipped
code:

- reply envelopes are route/generation qualified both directions
  (`request_matches` / `reply_matches` / `owner_matches`,
  `rt_remote_task_dispatch.c`), with stale drops counted per target shard;
- `WAKER_REMOTE_TASK_REPLY` routes by `waker_key.owner_shard_id`
  (`rt_waiter_route.c`), no shard-0 fallthrough;
- reply re-registration parks first and unwinds if already resolved
  (`rt_remote_task_wait.c`);
- `start_remote_task` balances lease consume/restore and pending refs on every
  early return (`rt_remote_task_api.c`);
- teardown release is routed at every entry point: owner-done
  (`rt_task_complete.c`), producer lifetime (`rt_task_lifetime.c`), shutdown
  (`rt_shutdown.c` via `rt_far_task_release_all`), spawn ack/fail paths
  (`rt_remote_spawn_pending.c`, `rt_remote_spawn.c`), and the lowering frees
  handles through `rt_far_task_handle_free` -> lease `RELEASING` ->
  owner-routed `rt_far_task_release` message — never direct cross-shard state;
- executable payloads are compile-guarded to plain-data/copyable
  (`crossingRecordExecutable`, `TestLLVMTransportPayloadGuard*`), and
  `Task<far Task<T>>.clone()` is SEM3116 (`task_clone_affine_test.go`).

Acceptance rows (all green): spawn-then-await and spawn-then-cancel across
`SURGE_SHARDS=1,2,8` under both the override and the production capability
(`runtime_v2_far_task_source_e2e_test.go`, includes shards=1 self-crossing
await); already-DONE immediate reply; fabricated stale request AND stale reply
rejected with per-target stale-drop attribution; cancel-vs-completion
registration races closed under `SP_REMOTE_TASK_BEFORE/AFTER_OWNER_REGISTER`
sync points with exactly-one reply-edge consumption proven by transport
counters (`remote_task_behavior_races.c` — the epic's acceptance row, sync
points, not timing); unconsumed-handle teardown with owner-side observation;
cancel-before-publication-ack; queue-failure lease restore; shutdown wakes
reply waiters on all shards (`runtime_v2_remote_task_behavior_test.go`).

New in this close-out pass:

- the remote-task acceptance suite and the far-task e2e are now wired into
  `make runtime-v2-transport-contract-check` (they were previously orphaned
  from every gate);
- `spawn on pool` post-flip behavior is pinned by
  `TestRuntimeV2SpawnOnPoolProductionCapabilityFailsDeterministically`: the
  form-keyed gate lets it compile, and it must fail with the deterministic
  `spawn on placement is not supported by this backend` panic (placement
  resolver returns UNSUPPORTED for POOL) — no hidden local fallback;
- `rt_transport_debug.c` was added to the two explicit C-harness source lists
  (`runTransportCProgram`, spine acceptance) that broke when
  `rt_transport_debug_snapshot` moved out of `rt_transport.c`;
- runtime dedup: shared `rt_far_task_lease_find_locked`,
  `rt_remote_task_result_kind`, `rt_remote_spawn_enqueue_with_drain` (three
  enqueue-drain-retry copies), unified await/cancel `dispatch_request`,
  shared `rt_remote_task_reply_owner_done`, merged
  `release_owned`/`release_all` loops, factored `result_lease_take_locked`.

Gates (all green): `git diff --check`, `make c-check`, `make cppcheck`,
`make golden-check`, `make runtime-v2-crossing-check` run twice with the
post-flip matrix (compile-only negative space still clean),
`make runtime-v2-transport-contract-check` with the new rows,
`./check_file_sizes.sh -a` (every `rt_remote_task*` module <= 300 lines),
`make check`.

Sentrux CLI (`sentrux check`, all rules pass at every scope): root `6182`,
`internal` `6526`, `runtime` `5356`, `runtime/native` `5464`. The scoped
`runtime/native` signal is below the `5486` start baseline (~-0.36%,
committed-HEAD CLI parity `5484` -> `5464`): the residual is the inherent
import/call coupling of the ~1.1k-line remote-task subsystem after the dedup
pass produced no measurable metric recovery. Accepted per `RULES.md` Global
Rule 3 recovery clause with `RV2-DEBT-028` as the recovery owner (precedent:
Epic 8 Task 13 / `RV2-DEBT-003`). Task 10 (`on` execute/reply) remains next.

## 2026-07-10 — Epic 13 Task 10 Start

Task 10 lowers immediate `on shard(id)` / `on distributed` to the dedicated
execute/reply message category (`RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST`
= 6, `RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY` = 7, declared by Task 4 with the
execute request on the data lane and the reply on the control lane,
`rt_transport.c` `rt_transport_msg_is_control`). The spawn+await desugar is
rejected by the epic's Lowering Contract; trace equivalence (one request, one
reply, no publication-ack pair) must be proven by transport counters.
`on far_handle` stays FUT7014 (separate `CrossingLoweringOnFarHandle` kind, so
the form-keyed flip of `CrossingLoweringOnPlacement` cannot open it); `on pool`
stays a placement diagnostic.

Starting state verified: sema records `CrossingLoweringOnPlacement` with
destination/captures/payload (`internal/sema/on_crossing.go`); MIR
`CrossingInstr` and the spawn-poll body-function lowering
(`lower_expr_crossing_spawn_poll.go`) are reusable; LLVM `emitInstrCrossing`
has no `OnPlacement` case yet (default error);
`crossingRecordExecutable` returns false for `OnPlacement` (default arm), so
the form stays guarded until this task's deliberate flip.

Pre-change Sentrux CLI baselines on the committed tree (`8b82c1f9`), all rules
passing: repository root quality `6184`; `internal/` `6522`; `runtime/`
`5332`; `runtime/native/` `5420`. Observed tool behavior: the CLI
`quality_signal` differs between a dirty and a committed tree for identical
content (Task 9 close-out measured `5464` uncommitted vs `5420` committed for
the same `runtime/native` sources), so Task 10 completion compares
committed-tree numbers against these committed-tree baselines.

## 2026-07-10 — Epic 13 Task 10 Complete

Task 10 delivers immediate `on shard(id)` / `on distributed` on the dedicated
execute/reply category and flips `backendSupportsCrossingForm(LLVM,
on_placement)` in production. No desugar: one request
(`RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST`, data lane), one reply
(`RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY`, control lane), one request-scoped
token, no publicly observable `far Task<T>` handle.

Implementation:

- Runtime: new `rt_immediate_on.c` (275 lines) — caller-side
  `rt_immediate_on_execute` (placement resolve, pending + reply wait reuse of
  the Task 9 machinery, `RT_REMOTE_TASK_OP_EXECUTE`), destination-side
  dispatch (body task creation via the exported
  `rt_remote_spawn_create_body_task`/`publish_body_task` helpers, bind-then-
  owner-register with a status re-check closing the teardown race), routed
  caller-cancel (`rt_immediate_on_cancel_inflight`, exactly-once via
  `cancel_routed`), and caller-teardown release
  (`rt_immediate_on_release_owned`, hooked in `rt_task_complete.c` mark-done):
  bound in-flight requests keep the pending listed so the owner-done hook
  answers the orphaned reply exactly once; unbound queued requests are
  resolved so a late dispatch refuses to create an orphan body. The reply
  dispatcher now consumes (unlink + release) so an orphaned reply cannot leave
  a freed pending linked. Trace counters `immediate_on_execute_requests` /
  `immediate_on_replies` added to the transport debug snapshot.
- Out-of-range `shard(id)` resumes as `Cancelled` without running the body
  (Task 1 decision 4), proven with the resolver counter; `on pool` fails with
  the deterministic `on placement is not supported by this backend` panic.
- Compiler: sema `on` records now carry `SuspendCapable`
  (`internal/sema/on_crossing.go`); MIR `prepareOnPlacementCrossing` reuses
  the spawn-on body-poll-function lowering with a pending slot and no handle;
  LLVM `emitImmediateOnCrossing` (`emit_crossing_immediate_on.go`) suspends on
  the reply and materializes `TaskResult<T>` via the shared lifecycle result
  emitter; `rt_immediate_on_execute` builtin declared; panic strings
  registered.
- Guard matrix: `addOnCrossingBackendErrors` now applies the same two-stage
  rule as spawn-on (`backendBlocked || !crossingRecordExecutable`), so
  synchronous `on`, `on far_handle` (separate `CrossingLoweringOnFarHandle`
  kind), VM, and unknown backends keep FUT7014 deterministically — the entire
  pre-flip crossinggate/buildpipeline matrix passes unchanged post-flip.
- New sync point `SP_IMMEDIATE_ON_BEFORE_PUBLISH` (allowlisted to
  `rt_immediate_on.c`) drives the caller-cancel race row.

Acceptance rows (all green):

- e2e (`runtime_v2_immediate_on_source_e2e_test.go`, override + production,
  `SURGE_SHARDS=1,2,8` — shards=1 is the self-crossing forcing-function row):
  `on shard(0)`, `on distributed`, copyable captures, out-of-range
  `shard(4096)` → `Cancelled`; `on pool` deterministic panic row;
- behavior harness (`remote_task_behavior_immediate_on.c`): trace equivalence
  (exactly 1 execute request + 1 reply, zero publication-ack pairs) + owner
  proof; distributed non-caller proof; invalid-shard Cancelled resume with
  zero transport messages; fabricated stale execute request rejected with a
  destination stale drop; caller-cancel race under
  `SP_IMMEDIATE_ON_BEFORE_PUBLISH:block` — the cancelled caller resumes
  exactly once through the cancel path (a cancelled task cannot re-park:
  `rt_async_yield` completes it), the routed cancel reaches the body
  (`cancelled` observed), and the orphaned reply edge is consumed exactly
  once with zero stale drops; shutdown fails the execute reply waiter with
  `DESTINATION_SHUTDOWN`.
- The immediate-on rows are wired into
  `make runtime-v2-transport-contract-check` (new Task 10 section), and the
  `rt_immediate_on*.[ch]` glob joined the 300-line module pin.

Gates (all green): `git diff --check`, `make c-check`, `make cppcheck`,
`make golden-check`, `make runtime-v2-crossing-check` twice post-flip,
`make runtime-v2-transport-contract-check` (Tasks 4 + 9 + 10 sections),
`./check_sync_points.sh`, `./check_file_sizes.sh -a`, `make check`.
Sentrux CLI on the committed Task 10 tree (`dc1d37fd`), all rules passing at
every scope: root `6183` (baseline `6184`), `internal` `6518` (`6522`),
`runtime` `5324` (`5332`), `runtime/native` `5409` (`5420`, ~-0.2%). The
scoped `runtime/native` residual is the same inherent-subsystem-coupling
shape as Task 9's and is folded into `RV2-DEBT-028` (same recovery owner,
Task 12 closeout).

Task 11 (unsupported-forms matrix) and Task 12 (bench/CI closeout) remain.

## 2026-07-10 — Epic 13 Task 11 Complete

Task 11 proves the negative space of the split-capability matrix. No
capability changed. The owned (backend × form) matrix table, the
hidden-fallback audit with `file:line` evidence, and the bypass-backstop
re-verification live in `13-tasks/11-unsupported-forms-matrix.md`.

New owned cells added by this task (behavior-named per the naming policy in
`naming-cleanup-plan.md`; no epic/task identifiers in code):

- `TestVMAndUnknownBackendsKeepExecutableAsyncFormsGuarded`
  (`internal/buildpipeline/crossing_matrix_test.go`): the exact async +
  copyable shapes the LLVM capability opened stay FUT7014-7017 on `BackendVM`
  and an unknown backend — the flip is per-(backend, form), not blanket.
- `TestLLVMTransportCapabilityOpensAsyncImmediateOn` and
  `TestLLVMTransportCapabilityOpensAsyncFarTaskLifecycle`: compile-to-MIR
  proofs that production LLVM suppresses FUT7014/7015/7016/7017 for the
  opened shapes (parallel to the existing spawn-on variant).
- `TestRuntimeV2ImportedCrossingProductionCapability`
  (`internal/vm/runtime_v2_imported_crossing_e2e_test.go`): an async
  `on distributed` + `spawn on distributed`/await pair living in an imported
  module compiles and EXECUTES end to end on `SURGE_SHARDS=1,2,8` — the
  dependency scan's positive path. Probed first as a spike because the
  Epic 12 record flagged imported-module crossings as an ICE risk; no ICE:
  the executable path is clean. Multi-file support added to the e2e builder
  (`buildRuntimeV2CrossingProject`).
- `immediate-on-self-crossing-uses-transport-at-one-shard` behavior row
  (`rtb_mode_immediate_self_crossing`, `SURGE_SHARDS=1`): the transport
  counters fire (one execute request + one reply on the caller shard) even
  when destination == caller and only one worker exists — no hidden local
  shortcut, the reply wait is a task suspend.

The new compile rows joined `make runtime-v2-crossing-check`; the imported
e2e joined `make runtime-v2-transport-contract-check`.

Sema rows re-pinned as-is (owning tests named in the matrix table): remote
`far Channel<T>` ops keep the `SemaFarLocalOp` family rejection; `far
TcpConn` remote I/O keeps `SemaOnTcpRemoteIO`; the HIR bypass backstop
remains non-vacuous because the capability flips widened only the
buildpipeline form map, never the HIR lowerer's own gate.

Gates: full matrix run green twice (`make runtime-v2-crossing-check` ×2 with
the extended pattern), `make runtime-v2-transport-contract-check`,
`make golden-check`, `sentrux check internal`, `make check`,
`git diff --check`, `./check_file_sizes.sh -a`. Pre-change committed-tree
Sentrux baselines: root `6183`, `internal` `6518`, `runtime` `5324`,
`runtime/native` `5409` (unchanged from the Task 10 close-out; this task
adds only tests and docs). Task 12 (benchmark + CI gate + closeout) remains.

## 2026-07-10 — Epic 13 Task 12 Complete: Epic Closeout And Handoff

The epic's Closeout section (final shape, per-criterion acceptance evidence,
debt disposition, handoff pointers) is written into
`13-phase4-transport-spine-and-placement-task-lowering.md`; this entry is
the handoff log record.

- Gate: `runtime-v2-transport-check` is the stable umbrella over the
  transport contract target (park/wake spine acceptance incl. the lost-wake
  negative control, publication rows, the three e2e verticals, race rows,
  and the negative matrix), wired into `runtime-v2-check`. Every `-run`
  regex verified non-empty via `go test -list`
  (4/1/8/3/4/12/4/14 matching tests per pattern); the gate passed twice
  consecutively before wiring.
- Benchmark: `scripts/bench_crossing.py` (owns per-probe subprocess
  timeouts and reports probe/mode on expiry — the `RV2-DEBT-006` lesson;
  the channel-script debt is not inherited). Baseline at 2000
  iterations/probe: spawn-await 92848/5850/6338 rt/sec at shards 1/2/8
  (10.8/171.0/157.8 us/rt); immediate-on 146388/17643/12018 rt/sec
  (6.8/56.7/83.2 us/rt). Multi-shard round trips cost ~57-171 us — honest
  liveness-cost numbers for the remote-channel epic to baseline against,
  not line-rate claims. Immediate-on's ~2.5-3x advantage over spawn+await
  on multi-shard rows is the dedicated-category rationale made measurable.
- Trace counter review: enqueue (total/control/data), drain
  (total/control/data), wake writes, wake elisions, spawn requests/acks,
  completion replies, cancel replies, stale generation-token drops, release
  requests, immediate execute requests/replies all exist and are exercised
  by rows in the behavior/spine suites. Added this task: `credit_stalls`
  (declared per the epic contract; structurally zero until the credit
  protocol exists — recorded as such) and `unsupported_fallback_attempts`
  (tripwire; asserted zero in the trace-equivalence row — nonzero is a bug
  by definition).
- Debt: `011`/`018` narrow-closed for the transport gate (Task 2 evidence in
  their rows; broad matrix stays with Backend/Test Matrix Cleanup); `024`
  reaffirmed (Task 1 decision; higher-order boundary future); `025`/`026`
  reassigned with affinity as a transport invariant; `028` reviewed and kept
  open, owner moved to the next native structural cleanup pass together
  with `naming-cleanup-plan.md`; no new debt from Tasks 3-11 without an
  owner.
- Docs: `README.md` (epics) marks Epic 13 complete with the new gate;
  `docs/RUNTIME_V2.md` Phase 4 carries the executable-now vs future-work
  status note (no overstatement: channels/select/scopes/migration/credits/
  pool execution remain open).
- Quality closeout: `make check`, `make c-check`, `make cppcheck`,
  `make golden-check`, `./check_file_sizes.sh -a`, `make runtime-v2-check`
  (fully green runs plus one discarded run holding only the documented
  transient), Sentrux CLI four scopes on the committed closing tree
  (`abe301a1`), all rules passing: root `6183`, `internal` `6518`,
  `runtime` `5324`, `runtime/native` `5409` — identical to the Task 10/11
  committed baselines (the closeout added counters, harness fixes, a
  benchmark script, and docs; quality-neutral). The epic-cumulative scoped
  `runtime/native` delta vs the committed pre-Task-9 tree remains the
  `RV2-DEBT-028` record.

Full-gate stabilization findings while running `runtime-v2-check` twice:

- Latent harness breaks from the Task 9 `waker_key.owner_shard_id` field, in
  gates that only run inside the full `runtime-v2-check` family: two-field
  `waker_key` initializers in the fd-registry harness snippets
  (`runtime_v2_fd_registry_static_test.go`,
  `runtime_v2_fd_registry_shutdown_static_test.go`) and the net-poller wake
  matrix (`runtime_v2_net_poller_static_test.go`) failed
  `-Wmissing-field-initializers -Werror`; fixed by adding the third field.
  The fd-registry shutdown harness also needed transport teardown stubs
  (`rt_far_task_release_all`, `rt_remote_spawn_drain_inbound_locked`,
  `rt_remote_spawn_fail_all_pending`) because `rt_shutdown.c` now routes
  far-task release/drain at shutdown. Same class as the explicit-source-list
  pitfall recorded at Task 9 close-out: harnesses that embed or `#include`
  runtime sources must be revisited whenever shared runtime structs/entry
  points change.
- Accepted transient (documented `RV2-DEBT-018`/`027` class):
  `TestRuntimeV2HTTPOwnerLocalBehavior` failed once per discarded run on a
  different shard row each time (shards-8, then shards-2; `status=408`
  empty-body client timeout — net-timing, unrelated to transport work).
  Focused reruns green (3x and 5x); the accepted evidence pair is a fully
  green `runtime-v2-check` plus a second run whose only failure is this
  documented transient with green focused reruns, following the Epic 8
  Task 13 precedent.

Next epics reuse the spine: message-kind + dispatcher-arm + pending/reply-
wait/take-owner discipline (pointers in the epic Closeout). The epic/task
naming cleanup (`naming-cleanup-plan.md`) is the recommended next
maintenance change before the Backend/Test Matrix Cleanup epic.

## 2026-07-10 — Epic 14 Task 1 Complete: Kickoff Decisions

All kickoff decisions are recorded in `14-tasks/01-kickoff.md`; the epic doc
gained boundary decision 9 (handle genesis). Highlights:

- Anchored op set for the first vertical: `{send, recv, close}` on
  `far Channel<T>`, in-body results follow LOCAL channel semantics
  unchanged; `try_*` and `far TcpConn` I/O excluded.
- Owner-side linearization point: the local channel lane transition on the
  owner shard (`rt_channel_lane.h`) — FIFO is inherited from local
  channels, not rebuilt in transport; channels already know their owner
  (`rt_channel_owner_shard_id`).
- Self-deadlock (epic decision 5) resolved: detection with a deterministic
  actionable panic in all build modes at the worker idle-park quiescence
  boundary, extending the `"async deadlock"` precedent
  (`rt_async_poll.c:289`); feasibility bound recorded (narrows to debug +
  design review if quiescence cannot separate deadlock from legitimate
  idle).
- Handle genesis (external review, Codex pass): `channel_on(dst, cap)` is
  the headline producer, sugar over the sanctioned fresh-channel-return
  primitive with a nominal-and-narrow typing rule (no general `T -> far T`
  coercion); one shared handle-token allocator with a `kind` tag, task and
  channel lifetimes kept independent; the local-counterparty rule named
  (the no-consumer mint is the genesis-time face of self-deadlock, gets a
  reproducer). G2 capture-mints and G3 harness-only rejected.
- Diagnostics tiers assigned per epic decision 8, incl. the two
  precision-pass sema codes (sync context; exact non-crossable field).
- Sentrux baselines re-pinned on the committed tree: root `6183`,
  `internal` `6518`, `runtime` `5324`, `runtime/native` `5409`.

Next: Task 1.5 (genesis slice).

## 2026-07-10 — Epic 14 Task 1.5 Complete: Handle Genesis

A runnable program can now obtain a `far Channel<T>`:
`channel_on::<T>(dst, capacity)` works end to end under the test-scoped
capability override on `SURGE_SHARDS=1,2,8` (prelude intrinsic -> sema
crossing record -> MIR pending/handle slots -> LLVM create call with a
deterministic panic error space -> transport create request/reply pair with
counters -> owner-side registry mint -> filled caller token). Three commits:
runtime registry + kind-tagged token, sema record + FUT7018 guard, MIR/LLVM
lowering + mint e2e (wired into the transport gate).

Contract dispositions recorded in `14-tasks/02-handle-genesis.md`: the
fresh-return SYNTAX exercised the slice's stop condition and returns to
design review (`channel_on` implements the sanctioned primitive's semantics
directly); the caller-side token drop follows the language-wide drop story
(`rt_far_channel_handle_drop` hook shipped and harness-proven; the backend
emits no scope-exit drops for any owned type yet); the local-counterparty
reproducer hands to Tasks 2-3 with its registry hooks in place.

Latent gap fixed for every crossing kind: the LLVM string-constant
collector never walked crossing operands, so bignum-typed literals (all
`uint` literals) riding any crossing failed emission; the collector now
covers destination/receiver/state/captures/remote-op operands.

Sentrux committed-tree baselines for the next task are re-measured at its
kickoff (this slice added a runtime module, compiler files, and tests).
Next: Task 2 (behavior rows for the anchored-op race/failure matrix).

## 2026-07-10 — Epic 14 Tasks 2-3 Increment 3: Self-Deadlock Detection

`rt_remote_task_deadlock.c` implements decision 5: at a worker's idle-park
edge (after `rt_transport_prepare_shard_park` publishes PARKED), the runtime
checks global quiescence — every shard transport-PARKED, every sleep store
empty, no net waiters, no blocking work queued or running — and scans the
pending list for a suspended execute/anchored block whose body task is
parked on a channel waiter. A hit is re-verified against quiescence (the
double-check absorbs the blocking-pool dec-before-wake window) and then
panics in every build with the actionable message naming the operation,
the body task, and its shard.

Configuration boundary discovered while proving the rows: SHARDS=1 with one
worker starts no worker threads at all (`rt_async_state.c` returns before
spawning; the awaiting driver thread polls tasks itself), so the park-edge
check cannot run there — that mode's quiescence is already covered by the
driver-side "async deadlock" panic. The reproducer rows therefore run on
one shard with two workers and on two shards.

External-counterparty boundary: four existing anchored rows use the harness
main thread as the channel counterparty (drain/close/cancel/teardown from
outside the runtime), which the quiescence scan correctly cannot see —
detection fired on all four as designed. The resolution is a legitimate
embedder knob, not a test hack: `SURGE_REMOTE_DEADLOCK_DETECT=0` opts out
for processes whose external threads feed or drain channels through FFI;
the default stays on in every build. The rows set the knob and say why.

New rows: `anchored-self-deadlock` (rows 7+8 of the matrix — a full channel
whose only consumer is the suspended caller; asserts nonzero exit plus the
panic text on both configurations) and `anchored-pin-vs-release` (row 9 —
release during an active block returns OK, the token stops resolving
immediately, and the body completes on its already-resolved pointer because
the dispatch-time pin defers reclamation to the reply edge; the body holds
a busy-yield gate so it stays runnable and quiescence never falsely forms).
Row 10 (leak audit) rides Task 6 stress as planned.

### Detection hardening after external review (same day)

Five Codex review rounds against the increment converged to clean. The
material findings, all fixed: per-shard transport `park_state` is a
last-edge flag, so quiescence now reads `running_count`, both ready
queues, inbound length, timers, and net waiters under each shard's own
lock, one at a time, with the caller's lock released around the check
(`wake_pending` keeps the sleep guard); `blocking_head` is read under
`blocking_lock`; the blocking pool claims a job in `blocking_running`
under the queue lock at dequeue and drops it only after the completion
wake is delivered, closing the two windows where a job was invisible to
idleness checks; the suspect scan retains bodies only through
`owner_registered` pendings (the flag is coupled to the registration's
counted body reference under the pending-list lock, so `task_add_ref`
cannot race the last free) and walks the whole list in batches instead of
capping at eight; the verify pass re-confirms the same suspect. The
detector's soundness argument is inductive: once one quiescence pass
holds with a channel-parked registered body, no in-model event source
remains, so the state is stable; every event source is now visible to the
scan at the instant it is checked.

### Init-order regression caught by the TSan gate (2026-07-12)

Wiring the stale completion-visibility static back into
`runtime-v2-lifecycle-check` exposed a real detection-increment bug the
frequently-run gates missed: workers start before `rt_blocking_init`, so a
worker reaching its first idle-park edge locked `blocking_lock` before the
mutex existed. The zeroed static executor masked it in plain builds;
`CompletionPinInterleavingTSan` (also absent from the frequently-run set)
hung inside TSan's race report on it. Fixed by initializing the blocking
pool before `rt_start_workers` — thread creation publishes the pool. The
lifecycle gate now covers both tests; four consecutive gate runs green
(one isolated `JoinPollResultObservation` flake on the first run, 5x green
in isolation, RV2-DEBT-018/027-class transient).

## 2026-07-12 — Epic 14 Tasks 2-3 Complete: Anchored Ops + Detection

Anchored execute with registry pinning and the full behavior-row slice are
closed: rows 1-9 of the race/failure matrix green on the behavior harness
(round trip, stale/wrong-kind without a body, full-channel park with a
live dispatcher, close-vs-recv closed outcome, cancel-no-resurrection,
owner teardown, self-deadlock expected-panic on both worker
configurations, pin-vs-release), row 10 (leak audit) rides Task 6 stress
by design. The self-deadlock detector survived five external review
rounds and the lifecycle-gate hardening (blocking-pool init-order fix).
Closeout Sentrux on the committed tree: root `6186`, `internal` `6509`,
`runtime` `5309`, `runtime/native` `5399`; all scoped rules pass; the
pre-existing root `min_equality` breach is now tracked as RV2-DEBT-029
with the same recovery owner as RV2-DEBT-028. Next: Task 4 (`on ch`
lowering through anchored execute, ChannelCreate capability flip, guard
matrix).

## 2026-07-12 — Epic 14 Task 4 Complete: `on ch` Lowering + Capability Flip

The anchored-op vertical is executable end to end on LLVM: `channel_on`
plus `on ch { send/recv/close }` compile and run across
`SURGE_SHARDS=1,2,8` under the production capability (no override), with
single-producer FIFO and the closed outcome observed source-side.

Shape of the vertical, decided with an external second opinion: bodies
stay one-shot poll functions; the park protocol lives in three runtime
helpers (`rt_anchored_channel_send/recv/close`) that reach the channel
and the shipped poll state through the current task's pending binding and
yield inside — re-entry restarts the body from the top, which sema keeps
sound by requiring the single anchored operation to be the body's first
statement (SEM3175, with the split-into-blocks workaround in the
message). General bodies are RV2-DEBT-030 (async transform per body
behind an opaque artifact seam). Two more boundaries surfaced and were
recorded rather than papered over: union reply payloads stay behind the
payload gate (`ret ch.recv()` would ship an owner-heap pointer; the
pattern is in-body unwrapping), and concurrent source-level park-retry
waits on copyable far handles (RV2-DEBT-025) — the compiled park shape is
proven by the harness helper-protocol row.

Guard matrix after the flip: `channel_on` and `on ch` compile on LLVM
only; VM/unknown backends keep FUT7018/FUT7014; the two-stage payload
gate keeps non-copy replies guarded everywhere. Task 5b sharpens those
messages to name the real cause. Next: Task 5 (negative matrix, payload
negatives, hidden-fallback audit).

## 2026-07-12 — Epic 14 Task 5 Complete: Negative Matrix + Payload Gates

Tests only: the anchored executable shape joined the VM/unknown guarded
matrix; payload negatives pin the heap-element mint (FUT7018), the union
reply, the captured far-task lease, and the shard-movable capture behind
FUT7014 on LLVM; sema rows prove SEM3152/SEM3165 hold behind the
far-handle destination; the far-channel create row watches the
unsupported-fallback tripwire. Finding recorded in the task doc: plain
heap-owning captures die at sema (SEM3168) before any backend gate — the
payload guard's genuine surface is shard-movable captures and non-copy
payloads. Next: Task 5b (diagnostics precision across the crossing
family).

## 2026-07-12 — Epic 14 Task 5b Complete: Cause-Precise Crossing Diagnostics

The guard stage now names the real blocker per crossing record: FUT7019
for a missing async context (same fix on every backend), FUT7020 for
payloads/captures that cannot ship — with the capture binding name, the
exact nested field path of the first heap-owning leaf, and the anchored
union-reply unwrap fix in the message — and the generic per-form
backend codes only where the shape is executable and the backend lacks
transport. Sync golden fixtures re-pinned to FUT7019; default-closed
matrix keeps VM/unknown; message content pinned by a dedicated test.
Next: Task 6 (QUEUE_FULL stress, leak audit, bench row, epic closeout).

## 2026-07-12 — Epic 14 Complete: Remote Channels

Task 6 closed the epic: the queue-full stress row proves bounded
per-attempt failure with control-lane progress past a saturated data
lane (the saturation gate must park the in-flight body on channel
capacity — a busy owner drains the fill before the flooded enqueue can
observe it); the leak census churns 48 lifecycles (release racing a
pinned block included) and finds the pending list and channel registry
empty; the bench row lands at ~6.9/54.6/55.8 us per anchored block at
1/2/8 shards — within noise of plain immediate-on, so the registry
pin+cached-resolve architecture is free at the spine's scale. Credit
flow control is RV2-DEBT-031. Epic acceptance recorded in the epic doc
closeout; the cross-producer FIFO negative moves with RV2-DEBT-025.

## 2026-07-12 — Epic 15 Complete: Structural Cleanup + Gate Integrity

Closed in one pass. Substance: noise-band measurement showed both
redundancy gates were knife-edges by construction (gap < one commit
step); the registry dedup (layered token validation, one reclaim
contract, one reply helper) lifted native redundancy 0.2491 -> 0.2514
and quality to 5405 with a byte-identical exported-symbol census;
thresholds re-placed 3 noise bands below operating points; root scope
demoted to advisory (46% unresolved imports) with a written
re-promotion condition. Gate integrity is now mechanized
(internal/gatecheck in make check): its first runs surfaced 14 orphaned
tagged tests, an invisible fd-registry harness link break, and a
pre-lock-split anchor in the parked-with-work source gate — three
finds, all fixed. RV2-DEBT-027 bounded (50/50 + 3/3 TSan, quarantined
stress target, owner + handoff); DEBT-028 closed re-baselined; DEBT-029
closed advisory; naming plan closed with a zero census. Next-epic
candidates note: `16-candidates.md` (recommendation: @far_copy).

## 2026-07-13 — Epic 16 Complete: Shared Far Handles Via Sibling Leases

One-day epic. The registry entry moved from single-generation identity
to a lease table (generation = lease identity, allocator-unique; the
creating holder is lease zero, so one validation path from the first
token). share() is an anchored owner op through the execute/reply
discipline (kinds 14/15); a released holder cannot propagate access;
reclaim waits for zero active leases AND zero pins. The deadlock
detector needed no quiescence change — only lease-topology wording and
three adversarial rows (true two-holder deadlock, runnable-holder
false-negative guard, deadlock-after-peer-release). The fan-out e2e
lands the two rows Epic 14 recorded as blocked: concurrent compiled
park-retry with cross-holder drain, and cross-producer arrival as a
set. Bench: share-mint in the immediate-on band (8.9/83.9/89.4 us at
1/2/8). DEBT-025 closed superseded; DEBT-032 (force-close) opened
behind design review. Harness gotcha recorded: deadlock-panic rows must
sequence main-thread releases BEFORE the parked producer starts — main
is invisible to quiescence, so the panic can outrun a late release and
change the lease-count wording.

## 2026-08-02 — Epic 25 Resume: Step 1 Review Tail

Resumed from `15c23f9e` on `codex/runtime-net-scheduler-refactor`, with Step 0
and the report-only Step 1 verifier committed. The inherited worktree contains
the post-review fixes for effective operand types inside RValues/spawn and map
element STORE sinks, plus the guarded-drop regression tests; agent-generated
`.claude*`, `.serena`, `.swarm`, AgentDB/RuVector files, and the tracked
`.claude-flow/neural/stats.json` delta are explicitly outside the code commit.

The remaining Step 1 blocker is concrete: the guarded-drop recognizer's
hand-built test assigns the dropped local in each guard arm, while real
`lowerOwnedTempExpr` inserts a join-block transfer into the dropped temp. The
recognizer therefore does not yet accept the canonical real-lowering shape.
The intended proof is a narrow, conservative singleton-TRANSFER chase with a
real-lowering clean fixture and adversarial cycle/decoy coverage; it must keep
the EVERY-reaching-definition rule and report-only/read-only MIR contract.

Current checks: Serena 1.5.3 successfully activated this project and answered
LSP/search calls; AgentMemory MCP successfully returned the `surge` session
history; `go test ./internal/mir -count=1` and `git diff --check` pass before the
guard fix. Sentrux baseline (taken from this inherited dirty state): repository
quality `6194`; scoped `internal/mir` quality `6825`, with the scoped session
started for final comparison. Before Step 2, finish the guard repair, rerun the
Step 1 focused/full gates, review the intended diff, and commit only the MIR
code/tests plus these owned notes.

The real-lowering singleton-transfer guard repair now passes the focused and
full `internal/mir` suites, but independent review found three further Step 1
P1 gaps before closeout: a degenerate `if` whose true and false edges both enter
the drop block can fool the predecessor-block recognizer; bare global writes are
STORE sinks currently skipped with bare locals; and `Await`/normalized `Poll`
plus `Timeout` eventually consume task handles even though retry-safe MIR keeps
their operand as COPY, so the sink must trace the operand's definition under the
instruction contract. These require adversarial tests and a small design-note
correction before Step 1 can close. The first post-repair `make check` ran the
entire Go suite green, then stopped at `golangci-lint`: `opStr`'s `text` test
parameter is always `"x"` (`unparam`). That helper cleanup is part of the same
review tail; C/file-size checks did not run because lint failed first.

The three P1 rows are now implemented with focused tests: degenerate
`Then == Else == drop` falls back to ordinary EVERY-def resolution; bare global
STORE reports `global_assign`; and `Await`/`Poll`/`Timeout` report
`task_consume`, with canonical COPY tracing the task local's definitions while
pending retains the handle. The second `make check` again ran the whole Go suite
green, then lint found three Go-1.22 `copyloopvar` redundancies in the new table
tests; those redundant loop-variable copies were removed. Full check must be
rerun from the top, since C/file-size gates again did not execute after lint
stopped the command.

The third `make check` is green end to end (all Go packages, zero lint issues,
strict C compile, and file-size gate), and `make golden-check` is green.
`make runtime-v2-check` reaches the pre-existing fd-registry suite and fails in
`TestRuntimeV2FDRegistryReadWriteInterestSharesFDRow` (payload-drain timeout)
and `TestRuntimeV2FDRegistryCancelledReadInterestPreservesWriteInterest`
(native double-free). The latter reproduces unchanged in a detached worktree at
the Step 1 base `15c23f9e`, so it is not introduced by the verifier diff; keep
the failure explicit rather than relabelling the full Runtime V2 gate green.

Final independent review then found two additional fail-open cases. First, the
guard shortcut correlated each `G=true` write to one value definition but did
not reject a later alias overwrite reachable while `G` remained true. The
repair must prove a bidirectional frontier correlation, including one
dominating false initializer, and retain the real-lowered singleton-transfer
positive. Second, an untyped non-canonical task COPY could leave its effective
type unknown and escape before the shape check; unknown task COPYs must fail
closed while known non-owning operands remain outside ownership analysis. Both
rows require adversarial Await/Poll/Timeout or nested-choice tests before the
Step 1 commit.

The next Sentrux comparison reported score `6825 -> 6826`, but correctly kept
the session red because complex functions increased `37 -> 38`; the new guard
correlation is being split into small proof helpers before closeout. The fresh
independent re-review also found a distinct canonical-shape false positive:
nested mixed choices raise the outer guard inside an inner branch and transfer
the chosen value through the inner join, where the outer assignment sees both
the false initializer and a true write. Real lowering for
`nested_inner_forwards` and `both_branches_nested_mixed` therefore falls through
to ordinary EVERY-def resolution and is reported even though the guarded drop
is valid. The fix must recursively correlate only exact bare-local MOVE
transfers at ambiguous frontier points; real-lowering nested positives join the
flat positive, while the post-true alias overwrite, noncanonical transfer, and
cycle negatives must stay red.

The nested repair is now proven on the borrowed forms that actually exercise
the recognizer (`a: &string`, branch `*a`): with recursive correlation disabled,
`nested_inner_forwards` and `both_branches_nested_mixed` report at `bb7#0` and
`bb10#0`; with the approved exact-MOVE recursion, both are clean. A nested
post-true alias overwrite remains a finding, and a separate loop fixture hits
the new active recursive-cycle rejection branch (targeted coverage count 1).
The review's final P2 gap is also pinned: an untyped `Spawn.Value` now has both
the minted clean case and the alias-traces-to-definition case.

Final independent Step 1 re-review is CLEAN (no P0-P3). The post-fix
`make check` is green end to end, `make golden-check` is green, and scoped
Sentrux passes with quality `6825 -> 6831`, no violations; root quality is
`6194 -> 6195` and all checked architectural rules pass. The only named broad
gate still red is the already-recorded `runtime-v2-check` fd-registry failure;
the reproduced base failure and the absence of runtime/C changes keep it out of
this verifier commit rather than hiding it. Step 1 is ready for an owned-only
commit and mandatory post-commit Codex review before the corpus gate begins.

## 2026-08-02 — Epic 25 Step 2: Corpus Discovery And Triage

Step 1 landed as `632ed9ed`; its commit-scoped Codex review is running while
Step 2 starts on disjoint infrastructure. Step 2 will add an analysis-only
buildpipeline mode whose zero value preserves executable compilation, plus a
cycle-free `internal/ownershipgate` package and a tagged sequential corpus gate.
The corpus is discovered by attempting every `.sg` under the four specified
roots, not by treating directory names as proof that MIR exists. Exact compile
failures, normalized ownership findings, stale allowlist entries, and DEBT
cross-references all fail closed.

The approved implementation plan is recorded in AgentMemory as
`mem_msbv3x2k_bb0acb0d879b`. Independent review is required after the first
green corpus run and before the Step 2 commit. Sentrux baseline for the new
cross-package surface `/internal` is quality `6504` (1025 files, 234727 lines),
with the comparison session started. First evidence target: focused
buildpipeline/model tests, then the bounded full 1046-fixture census and
classification of every result without blanket suppression.

The final full census is green. Its schema-v2 report is
`target/runtime-v2/ownership-corpus-census.json`; the pinned four-root path
digest is
`47b3f1e5dea9b27669b60c0036cb16651794067ffaa0b54230964a9b867acd10`, and the
accounting closes exactly as `1046 attempted = 575 MIR + 390 invalid + 81
exact-ledger non-invalid failures`. The 81 entries are grouped under
`CF-001`..`CF-015`. The only ownership findings are category-3
`OWN-001`/`OWN-002`, the two intentional cycle edges in the VM3302 positive
fixture, both tied to `RV2-DEBT-114`; there are no normalization errors or
untriaged results.

Two corpus blockers required bounded select fixes. Local select now preserves
exact bare receiver/payload places instead of packing synthesized owning
aliases. Far select gives each unique bare `own` SEND root an explicit
conditional-transfer `ReturnPlace`: pending owns the payload while suspended,
and a non-SEND winner hands it back before the arm body. Duplicate far roots
and computed local receivers stay fail-closed/deferred as `RV2-DEBT-113` and
`RV2-DEBT-116`. Cancellation is pinned by Valgrind A/B controls: far select has
the same 48 B / 2-block baseline for Copy and non-Copy payloads; local select
has the same 96 B / 2-block baseline; both have zero indirect and zero
incremental payload loss. Those visible baselines are the already-open nested
handle drop-glue gap `RV2-DEBT-062`, not a Step 2 leak hidden by tolerance.

The first final independent Step 2 review found one real P1: both new
cancellation e2e tests were skipped by ordinary `make test` and unreachable
from every explicit Runtime V2 target. The bounded fix added their exact names
to `runtime-v2-heap-check`, whose environment sets
`SURGE_SKIP_TIMEOUT_TESTS=0`. Focused execution and the full heap target now run
(not skip) both rows and preserve the measured 48 B / 2-block far and 96 B /
2-block local A/B baselines with zero indirect delta. `internal/gatecheck`
passes, and the independent re-review is CLEAN with no remaining P0-P2.

The first commit-scoped Codex review found one further blocking P2: the
far-select boundary consumed an owned SEND payload but leaked it when the
initial `anchors` array was null. The repair reclaims well-described SEND
payloads before state on that synchronous invalid-argument path, bounded to
`0 < count <= RT_FAR_CHANNEL_SELECT_MAX_ARMS` so malformed oversized counts do
not read caller arrays. A new row reaches the real async caller and proves
`pending == NULL` plus exactly one payload drop. Focused dynamic/static tests,
strict C checks, and the full transport gate pass; independent re-review is
CLEAN with no P0-P2 findings.

Final Step 2 evidence: `make check`, `make golden-check`,
`make runtime-v2-ownership-check`, `make runtime-v2-heap-check`,
`make runtime-v2-transport-check`, `make c-check`, and `git diff --check` pass.
The `make check` file-size phase reports 23/23 changed code files within policy.
The composed `make runtime-v2-check` passes liveness, ownership, crossing,
heap, and waiter before reaching the known fd-registry baseline:
`TestRuntimeV2FDRegistryReadWriteInterestSharesFDRow` times out draining 32 MiB
and `TestRuntimeV2FDRegistryCancelledReadInterestPreservesWriteInterest`
reproduces its existing double-free path. These failures predate Step 2 and are
outside the ownership epic, so no fd-registry code was changed. `make cppcheck`
reports only existing style warnings in unchanged `rt_array_reclaim.c`,
`rt_map.c`, and `rt_array.c`; the changed far-select C file is warning-free.
Sentrux `internal/` quality improves `6504 -> 6506`, all seven rules pass with
zero violations, and health still names modularity as the bottleneck;
`session_end` additionally records the non-blocking complex-function count
change `541 -> 547`. Steps 0-2 are complete and Step 3 is next.

## 2026-08-02 — Epic 25 Step 3: Development Hard Gate

The ownership verifier is now wired only into `surge build --dev`, through a
zero-value-safe `CompileRequest.Dev` field. It runs after async lowering and
MIR validation, before the result publishes MIR, and reports a typed,
deterministic `StageLower` error while preserving diagnose/sema context.
Default builds still compile the RV2-DEBT-102 negative control, `build --dev`
rejects it, and `surge run` is unchanged.

The fast untagged gate compiles 22 pinned representative fixtures sequentially
inside ordinary `make check`; the full corpus separately retains the exact
Step 2 census (`1046 attempted`, `575 MIR`, `390 invalid`, `81` exact-ledger
non-invalid failures, and only `OWN-001`/`OWN-002`). `make check` passes. Two
serialized `make golden-check` runs also pass with zero `testdata/golden` diff,
so neither MIR nor AST goldens drifted. A concurrent-golden experiment exposed
that updater processes race in one shared worktree; all future golden gates
must therefore be serialized. The independent Step 3 review is CLEAN with no
P0-P3 findings, and Sentrux finds zero violations in eight checked rules. Its
existing modularity score is recorded as non-blocking debt, not changed here.
The composed `make runtime-v2-check` passes through liveness, ownership,
crossing, heap/Valgrind, and waiter, then reproduces only the already-recorded
`TestRuntimeV2FDRegistryReadWriteInterestSharesFDRow` timeout. That unrelated
network-runtime baseline remains outside the ownership epic and was not
changed.

## 2026-08-02 — Epic 25 Step 4: Opt-In Annotated MIR

Both `build` and `diag` now accept `--emit-mir-annotated`; it implies ordinary
MIR emission without enabling the development verifier or changing `run`.
The Build API carries the same implication and retains `out.mir`. Annotated
locals receive `owes_release` only when a real `Drop` or `EnvelopeRelease`
targets their base local, while only assignment RHS values receive one of the
existing verifier classes `mint`, `alias`, or `transfer`.

The printer tests pin byte-identical `DumpOptions{}` output before and after an
annotated dump, keep Sema-only/non-opt-in output identical, reject annotated
mode without sema before writing, and prove `OwnsHeap` alone is not a release
obligation. Real build API, build CLI, and diagnose CLI paths are covered in
temporary directories. `make check` passes, and two serialized
`make golden-check` runs leave every regenerated AST/MIR golden byte-identical.
The final independent combined review is CLEAN with no P0-P3 findings.

## 2026-08-02 — Epic 25 Step 5 Start: Typed Four-Axis E2E Gate

Step 4 landed as `bf543542`; its commit-scoped Codex review found no actionable
correctness issue and confirmed default MIR stability. Step 5 starts from that
exact SHA in two temporary worktrees: one owns only the new typed
`ownershipGate` plus its compile-time arity proof, and one owns only the four
named source/test migrations. Both agents must submit a plan-only pass before
editing; their commits will be independently reviewed before integration.

The settled contract has four distinct, required nominal marker types for
move-only heap, `@copy` value-composite, reference-counted scalar, and
non-owning probes. VM and LLVM+Valgrind run as sibling subtests so a native
timeout skip cannot suppress VM evidence. Native success requires exit zero,
all four unique markers, no Memcheck invalid access/free, and strict zero
definitely-lost bytes and blocks. The compile-time negative uses a Go overlay
that calls the real helper with only three axes. No runtime/compiler behavior
changes, broad e2e retrofit, or CI/Make expansion is in Step 5; missing
automated Valgrind selection is non-blocking debt unless the focused gate
cannot be run. Golden generation remains root-only and serialized after the
integrated diff.

Both writer plan-only passes are complete and recorded in AgentMemory as
`mem_msc5ktio_58836561ad36` (typed harness) and
`mem_msc5ko9h_903d9ed669fb` (four-file matrix). Before implementation, Sentrux
records root quality `6212` and scoped `internal/vm` quality `7068`; the final
comparison session is based on that scoped scan. The harness commit must be
reviewed first, then cherry-picked into the matrix worktree so the migration
agent compiles against the exact nominal types and argument order. Only the
reviewed harness and matrix commits may then be cherry-picked into the primary
worktree.

Live Make/CI inspection confirms the known selection gap: ordinary `make test`
and CI set `SURGE_SKIP_TIMEOUT_TESTS=1`, so they can exercise each helper's VM
sibling but skip LLVM+Valgrind, while no explicit Runtime V2 target names the
four Step 5 tests. The focused `SURGE_SKIP_TIMEOUT_TESTS=0 go test` invocation
is therefore the strict native evidence for this step. Wiring that invocation
into Make/CI is non-blocking debt, not implementation scope.

The harness pre-commit hook passed `make check` and, by repository convention,
also regenerated `STATS.md`. Its clean-worktree count corrects a prior polluted
value: the stats script excludes `.claude/` and `target/` but not untracked
`.serena/` Go cache files present in the primary dirty worktree, so the earlier
main-Go count included one 560-line non-product file. Keep the hook-generated
clean-worktree stats; hardening `scripts/code_stats_md.sh` against other agent
metadata is non-blocking tooling debt and is outside Epic 25.

Commit-scoped Codex review of the intentionally harness-only `02d6474d`
reported one P1: the helper cannot enforce the four axes until the four named
tests call it. This is the expected boundary between the two isolated writer
commits, but it is a real Step 5 completion condition. Therefore the harness
must not land alone: the P1 is resolved only after the reviewed matrix commit
uses the helper in all four tests and the combined range is reviewed clean.

Independent harness review is `APPROVE-WITH-DEBT` (P0/P1 none). The accepted
P2 is pre-existing VM test-infrastructure portability: `repoRoot(t)` derives
the root from `runtime.Caller`, so `-trimpath` turns it into module-relative
`surge`. Consequently the overlay cannot open its source, and the native path
builds beneath `internal/vm/surge` before Valgrind exits 127 on a relative
binary path. Exact reproducers are
`go test -trimpath ./internal/vm -run '^TestOwnershipGateMissingFourthAxisDoesNotCompile$' -count=1 -v`
and
`SURGE_SKIP_TIMEOUT_TESTS=0 go test -trimpath ./internal/vm -run '^TestRuntimeV2OwnershipGateFourAxisSelfProof$' -count=1 -v`.
Normal Step 5 commands and current CI do not use `-trimpath` and are green;
fixing the shared root/path infrastructure is therefore durable debt, not a
Step 5 code change. Review evidence is `mem_msc63wvl_a612ca556251`.

The first four-test strict run found a new non-blocking ownership row while
constructing the identity-cast file's missing string axis: identity-casting a
runtime-built string (`runtimeBuilt() to string`) exits successfully but loses
17 bytes in one Valgrind block. Three other migrated files already passed both
VM and LLVM+Valgrind. Step 5 does not repair this newly exposed compiler/runtime
behavior: the move-only axis will use a real runtime-built string move, while
the file retains its complete historical float identity-cast regression. The
exact string-cast reproducer, Valgrind evidence, and dev-verifier result must be
recorded as separate debt before closeout. A same-type `@copy` cast was also
rejected by the language, so that axis uses the supported real duplicate plus
mutation/unchanged-original proof instead of inventing syntax.

The adjusted exact four-test gate is green: four VM siblings and four
LLVM+Valgrind siblings, each native row strict `0/0`; the skip control executes
all four VM siblings and explicitly skips all four native siblings. The string
identity-cast row was minimized separately: Valgrind reports 11 allocations,
10 frees, and definitely-lost `17 B / 1 block`, while
`surge build --backend=vm --dev` exits zero, so the current verifier does not
catch this missing-release class. Reproducer/evidence is in AgentMemory
`mem_msc6ezwt_8a9f44caf25c`; closure needs both the ownership fix and a
regression that prevents the verifier/tooling blind spot from returning.

Commit-scoped Codex review of matrix commit `3d87afb6` found one blocking P2:
the new harness explicitly skipped its native sibling when `valgrind` was not
installed, whereas each replaced test previously attempted Valgrind and failed
closed. That weakens the mandatory memory gate and is not accepted debt. The
bounded review fix keeps timeout-policy skip local to the native sibling, but
with timeout tests enabled it must compile LLVM and then fail explicitly if
Valgrind is unavailable. A no-Valgrind negative proof is required before a new
immutable matrix SHA is reviewed.

The bounded fix landed in the isolated matrix worktree as `401c74e7`: missing
Valgrind now fails explicitly after LLVM build, and the child negative exits 1
with the exact requirement and no SKIP. Commit-scoped Codex review is clean;
independent combined review is CLEAN for new P0-P3, with only the accepted
`-trimpath` P2 debt. Review/evidence is in `mem_msc6yv21_f2a2cb46a130` and
`mem_msc6vjf3_03aac99daa43`. The reviewed series was then cherry-picked into
the primary branch as `a5f4acd3`, `04d03f9e`, and `9697a566`; no user-owned
dirty artifact was staged or modified. Root-owned broad checks, Sentrux, and
serialized golden stability are next.

Post-integration Serena diagnostics are empty for the harness and all four
migrated files. Sentrux's narrow `internal/vm` observation is `7068 -> 7066`,
but that directory has no own rules file; the exact-base comparison was
therefore repeated at the nearest governed scope, `internal/`, using a detached
`bf543542` worktree. It reports quality `6517 -> 6516`, seven rules passing,
and one newly complex function (`562 -> 563`). RULES makes that -1 blocking,
so it is not accepted as debt: a separate worktree owns a behavior-preserving
split of the test-only harness's VM/native assertion paths. Root will rerun the
same exact-base Sentrux comparison before broad/golden closeout.

The first backend split was independently behavior-clean but did not repair the
metric (`6517 -> 6516`, complex functions `562 -> 563`), so it was not
integrated. The successful structural recovery folds the gate into the existing
crossing-source build helper, removes the extra file node, and extracts only the
missing-axis diagnostic tail. Commit-scoped Codex review then found that Go
`%q` is not full JSON escaping for control characters in valid paths; the
follow-up removes absolute paths from the overlay JSON entirely and uses only
validated root-relative ASCII artifact paths. The reviewed commits landed as
`5041d843` and `86adb946`. Exact governed `internal/` Sentrux is stable against
`bf543542`: quality `6517 -> 6517`, files `1056 -> 1056`, import edges
`2158 -> 2158`, complex functions unchanged, all seven rules pass, and
`session_end` passes. Serena diagnostics are empty after the cherry-pick.

The root strict focused matrix then passed on the integrated tree:
`SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run
'^(TestRuntimeV2OwnershipGateFourAxisSelfProof|TestRuntimeV2IdentityCastKeepsItsSourceOwned|TestRuntimeV2BorrowedScalarSurvivesItsReaders|TestRuntimeV2ComparePayloadOutlivesItsArm|TestRuntimeV2BorrowedComparePayloadStaysTheOwners)$'
-count=1 -parallel=1 -p=1 -v --timeout 300s` completed in 28.035s. All five VM
siblings and all five LLVM/Valgrind siblings passed; no native skip occurred.

The integrated-tree `make check` gate passes: all Go packages, lint (zero
issues), strict C formatting/compilation, and the file-size command are green.
The file-size command reports no applicable uncommitted code files because the
reviewed Step 5 code is already committed; both isolated writer pre-commit
runs checked the changed files before integration.

Serialized root `make golden-check` pass 1 completed successfully. The
generator rebuilt its normal AST/MIR sidecars and the subsequent explicit
`git diff --quiet -- testdata/golden` check returned zero; no golden file is
modified or untracked.

Serialized root `make golden-check` pass 2 also completed successfully, and a
second explicit `git diff --quiet -- testdata/golden` returned zero with empty
golden status. The two independent regenerations therefore confirm stable
AST/MIR golden output on the integrated Step 5 tree.

The final `make runtime-v2-ownership-check` passes in 121.292s. Its canonical
census is unchanged: `1046 attempted = 575 MIR + 390 invalid + 81 exact-ledger
non-invalid failures`, with exactly the two pinned projected-assignment
findings in `vm_rc_leak_panics.sg`; the deterministic report was rewritten at
`target/runtime-v2/ownership-corpus-census.json` without contract drift.

The final composed `make runtime-v2-check` follows the established baseline.
Liveness, ownership corpus, crossing, heap/reclamation/Valgrind, fixnum/range,
and waiter gates all pass. It then exits 2 only in
`runtime-v2-fd-registry-check`, reproducing the same two unrelated network
rows recorded before Step 5: `TestRuntimeV2FDRegistryReadWriteInterestSharesFDRow`
times out while draining 33,554,432 bytes, and
`TestRuntimeV2FDRegistryCancelledReadInterestPreservesWriteInterest` reaches
EOF after `double free or corruption (out)`. Every remaining fd-registry row
passes. Epic 25 changes no fd-registry/runtime code, so these failures remain
external debt and are not repaired here.

## 2026-08-02 — Epic 25 Closeout Review: Baseline Gate Contract

Commit-scoped Codex review of docs closeout `3eb4d2d4` found one P2: marking the
epic complete while recording a non-green `make runtime-v2-check` contradicted
the earlier absolute-green wording in "What We May Not Lose." For the Step 5
closeout, the corrected contract is exact-command execution
plus no regression against the verified exact pre-Step-5 baseline. A
baseline-green target must remain green, green
remains the objective for inherited failures, and any newly failing stage or
test, extra failure, formerly-green earlier layer, or changed or worse failure
signature blocks completion.

Fresh A/B evidence at immutable Step 5 base
`bf543542e18f625d8ec94501ee784bee04757bcd` passes liveness, the exact
`1046/575/390/81` ownership census, crossing, heap/reclamation/Valgrind,
fixnum/range, and waiter before failing only the two fd-registry rows now
tracked by RV2-DEBT-122. The read/write row times out draining 33,554,432 bytes
with `read tcp ...: i/o timeout` (variable endpoints omitted; `64.83s`); the
cancelled-read row reaches EOF on the same drain after
`double free or corruption (out)` (`6.23s`); every other
selected fd row passes. Step 1 had already recorded both and reproduced the
cancellation row at detached `15c23f9e`, and Step 2 recorded both again before
Step 5 began. This correction changes no README status, code, STATS, golden, or
Sentrux result.

Commit-scoped Codex review of the first correction, `5e61ceca`, found a second
P2: Step 3 still required the composed Runtime V2 gate to be entirely green,
contradicting the centralized frozen-baseline exception. The Step 3 gate now
requires exact execution, keeps `make check` and `make golden-check` green, and
allows `make runtime-v2-check` to remain non-green only on the exact
RV2-DEBT-122 rows and literal signatures; every other failure blocks. This
follow-up also names `bf543542` accurately as the exact Step 5 pre-change
baseline and records the literal read timeout while omitting variable endpoint
addresses explicitly.

## 2026-08-02 — Epic 25 Closeout Review: Per-Step And Post-FD Correction

Immutable review of `374bfd0f` found that the residual Step 3 gate had been
bound to a later Step 5 baseline and that a fail-fast composed run did not
execute any targets after fd-registry. The normative contract now compares
every implementation step with its own immutable exact pre-change base. Step
3 uses `f247a4c7ffdcf13d6afef3b91d76ce37f2463c6e`; only Step 5 closeout uses
`bf543542e18f625d8ec94501ee784bee04757bcd` as its pre-change A/B base.
RV2-DEBT-122 now defines its two known fd failures as structured normalized
tuples: target, exact test, drain operation and `33554432` bytes, plus stable
terminal class/tokens. Endpoint addresses, checkout paths, and durations are
evidence noise, not signature fields.

Because `make runtime-v2-check` exits at `runtime-v2-fd-registry-check`, root
ran the later targets standalone. `runtime-v2-accept-check`,
`runtime-v2-lock-check`, `runtime-v2-lifecycle-check`,
`runtime-v2-perf-check`, `runtime-v2-syncpoint-check`, and
`runtime-v2-transport-check` pass. `runtime-v2-net-handle-check` passes its
static row but the stale-copy behavior row exits `32` with empty streams at
shards 1/2/8 on both current and `bf543542`; RV2-DEBT-123 records this
recurrence of the proof that closed RV2-DEBT-010. Four current
`runtime-v2-http-owner-check` runs are `FAIL, PASS, PASS, FAIL`, with client-read
connection resets at shards 2 and 8; one `bf543542` run passes, so
RV2-DEBT-124 records the intermittent result without claiming an identical
baseline signature. Neither result expands Epic 25 into network-runtime repair.

The same review found that RV2-DEBT-118 duplicated the mechanism already owned
by RV2-DEBT-094 and that the previous review note mislabeled a Step 3 gate as
Step 2. RV2-DEBT-118 is now explicitly the narrow Step-5 child of 094 and can
close only with or after its parent plus the exact five-test CI matrix; the
step attribution is corrected. This follow-up changes only Epic 25, DEBT, and
NOTES documentation; README status, code, STATS, goldens, and prior Sentrux
evidence remain unchanged.

## 2026-08-02 — Epic 25 Closeout Review: HTTP Baseline Stress Resolution

Commit-scoped Codex and independent immutable reviews of `cc2176d` returned
`REQUEST_CHANGES` with one P1: the first RV2-DEBT-124 record had current HTTP
resets but only one green `bf543542` run, so it could not yet be admitted by an
exact no-regression closeout rule. Comparable baseline stress now supplies the
missing evidence. Exact `bf543542` `make runtime-v2-http-owner-check` runs 1-14
pass; run 15 fails in `TestRuntimeV2HTTPOwnerLocalBehavior/shards-8` when load
client `4` reads its response and receives `connection reset by peer`, with
empty stdout/stderr. Additional current-tree stress passes 9/10; run 7 fails at
shards 2 during warmup client `-1` with the same terminal class and empty
streams.

RV2-DEBT-124 therefore uses the shared normalized tuple
`(target=runtime-v2-http-owner-check,
test=TestRuntimeV2HTTPOwnerLocalBehavior, operation=client read response,
terminal_class=connection reset by peer)`. Shard, client id,
warmup-versus-load wrapper, endpoint addresses, checkout/artifact paths, and
run index are schedule-dependent or ephemeral and are not signature fields.
The comparable base reproduction resolves the review P1 without closing the
debt or expanding Epic 25 into HTTP/network runtime repair; the earlier
single-base-run note remains intact as the chronology before stress evidence.

The completed matched window corrects the preceding interim current count:
current and exact `bf543542` each pass 14/15, with the single current failure
at run 7 and the single base failure at run 15. The initial current
`FAIL, PASS, PASS, FAIL` sequence remains discovery history; the normative
no-regression comparison is the symmetric 15-run window, which shows no
failure-frequency worsening. Because the baseline flake appeared only on run
15, a chance 10/10 green window cannot close RV2-DEBT-124. Closure now requires
a deterministic reset reproducer and root-cause fix with failing-before and
passing-after proof, followed by a green repeated bounded stress target and a
composed gate that reaches and passes it.

The matched windows are reproducible from these exact invocations. Baseline
workdir `/tmp/surge-sentrux-baseline.n2oGgt` is checked out at exact
`bf543542e18f625d8ec94501ee784bee04757bcd`:

```bash
for i in 1 2 3 4 5 6 7 8 9 10; do printf 'base_http_run %s start\n' "$i"; make runtime-v2-http-owner-check; rc=$?; printf 'base_http_run %s exit %s\n' "$i" "$rc"; done
```

```bash
for i in 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do printf 'base_http_run %s start\n' "$i"; make runtime-v2-http-owner-check; rc=$?; printf 'base_http_run %s exit %s\n' "$i" "$rc"; if [ "$rc" -ne 0 ]; then break; fi; done
```

The second baseline loop terminates at run 15's failure. Current workdir
`/tmp/surge-step5-postfd.xM9vmt` has docs HEAD `374bfd0f` and code exactly
`86adb946`:

```bash
for i in 1 2 3 4 5 6 7 8 9 10; do printf 'current_http_run %s start\n' "$i"; make runtime-v2-http-owner-check; rc=$?; printf 'current_http_run %s exit %s\n' "$i" "$rc"; done
```

```bash
for i in 11 12 13 14 15; do printf 'current_http_run %s start\n' "$i"; make runtime-v2-http-owner-check; rc=$?; printf 'current_http_run %s exit %s\n' "$i" "$rc"; done
```

The Make target itself pins `SURGE_BACKEND=llvm`,
`SURGE_SKIP_TIMEOUT_TESTS=0`, the exact HTTP-owner tests, `-count=1`,
`-parallel=1`, `-p=1`, and `--timeout 180s`.

## 2026-08-03 — Runtime V2 Regression And Closeout Triage

History isolation excludes Epic 25 from the deterministic network failures.
RV2-DEBT-122 first turns red at `575f075d` (direct parent `12d67483`):
both fd rows pass 2/2 on the parent and fail 2/2 on the first-bad commit.
They are two symptoms of one product compiler CFG regression in async lowering
of `stdlib/net::write_all_owned$poll`: after `net_wait_writable`, the bad MIR
takes an early `async_return` instead of continuing the post-wait write loop.
The fd target, and therefore the composed `make runtime-v2-check`, remains red.

RV2-DEBT-123 first turns red at `a8351f82` (direct parent `2df134e1`).
The compare-arm drop added there is correct; it exposes an existing native /
language ABI-lifetime mismatch. Native returns a canonical `NetConn*` as a
language-owned eight-byte `TcpConn` box, so the new correct arm drop frees the
registry object. The stable-id test fixture remains valid. The repair belongs
at the ABI boundary and must not roll back compare-arm ownership.

Ledger audit also confirms RV2-DEBT-059 was fixed by `bdcc0695`; its outer
abandoned async state is reclaimed, while nested handle-shaped residuals stay
under RV2-DEBT-062 (and the independently scoped RV2-DEBT-061 remains open).
Epic 21's owner-routed core vertical shipped, but Task 9 acceptance closeout
was not executed and is now tracked by RV2-DEBT-125.

Roadmap audit records RV2-DEBT-126 for status drift: README surfaces disagree
on whether Epic 13 is active or complete, the Epic 13 header still says active,
and `docs/RUNTIME_V2.md` still calls Epic 20 in progress after its closeout.
The stale surfaces are intentionally unchanged in this factual triage pass.

RV2-DEBT-124 is a matched baseline flake, not a deterministic regression:
current and exact baseline each pass 14/15 under comparable serialized stress.
Because the failure is schedule-dependent and appears on both sides, ordinary
commit bisection cannot attribute it. The fix-now/defer classification for
RV2-DEBT-122/123/124/125/126 remains pending the project owner's decision; this
record contains evidence only and does not approve a repair policy.

## 2026-08-03 — Blocking Debt Repair Pass

The project owner approved a narrow pass over only the debts that block the
Runtime V2 epic chain now. That pass closes RV2-DEBT-101, RV2-DEBT-122,
RV2-DEBT-123, and RV2-DEBT-127. The matched-baseline HTTP reset remains the
existing nonblocking RV2-DEBT-124; newly isolated RV2-DEBT-128 through
RV2-DEBT-132 are also deferred. RV2-DEBT-125 and RV2-DEBT-126 retain their
previously recorded owner-decision scope. No unrelated cleanup was folded into
the blocker fixes.

RV2-DEBT-101 is closed by integrated commits `b33d1db4` and `adaa1c37`.
The sema/HIR/MIR matrix pins borrowed tuple elements at `0` drops, an owned
tuple at `2`, direct/one-alias/two-alias `@copy` tuples at `2`, a non-Copy alias
at `0`, and a value handed onward at `1`. The VM row passes and the exact
LLVM/Valgrind row is strict `0/0`. The first independent review found a P1:
Copy was checked before alias resolution, so an aliased `@copy` tuple incorrectly
received zero drops. The follow-up fixed the bounded alias/`own` walk and the
independent re-review returned APPROVE.

RV2-DEBT-122 is closed by integrated commit `116e886b`. The compiler now keeps
the ordinary successor of an unnormalized pre-async suspend until Ready/Pending
continuation ids exist, with `simplify_cfg` reachability and `succBlocks` using
the same rule. The direct MIR regression test is red on the parent and green on
the fix; the two exact fd failures pass, and the full
`SURGE_SKIP_TIMEOUT_TESTS=0 make runtime-v2-fd-registry-check` is green.
Independent review returned APPROVE with no P0-P3 findings.

RV2-DEBT-123 is closed by integrated commits `2dd7442b` and `db6bfad7`.
Language values own a stable-id box separate from the registry's canonical
`NetConn`/`NetListener`, and allocation failure before publication rolls back
the canonical object and fd resources. The first independent review found two
P1s in the initial candidate: immediate normal-close reclamation caused a
use-after-free, and an OOM result-box path leaked unpublished resources. Both
were fixed before landing. `make runtime-v2-net-handle-check` is green, and its
stale-copy behavior row passed 10 consecutive serialized runs at each of
`SURGE_SHARDS=1`, `2`, and `8`; independent re-review returned APPROVE.

RV2-DEBT-127 is closed by `49f98908`. Matched stress distinguishes the old and
current incidence: exact `5d190710` passed 18/20 with two logical failures;
pre-fix `adaa1c37` passed 15/20 with four logical failures plus one already
known empty-output `-1` transient. ASan then exposed the underlying test UAF and
showed a pinned pending reference still `PENDING` after it had been unlisted.
The shutdown path now sets shutdown and fails pending waiters before releasing
reply-waiter leases, while the test pins references it inspects. Restoring the
old compiled order is red with exit `23`; fixed ASan stress is 100/100 green,
and `make check` passes. Independent review returned APPROVE with no P0-P3
findings. Its static ordering proof passed 10/10 while the parent control stayed
red with exit `23`; the tagged behavior row passed 20/20; independent ASan+LSan
stress passed 30/30; related shutdown rows passed 5/5; and the full fd-registry
gate remained green.

The composed gate before RV2-DEBT-127's repair passed liveness, ownership,
crossing, heap/reclamation/Valgrind, fixnum/range, waiter, fd-registry, accept,
lock, lifecycle, and performance, then reached the transport stage and failed
on the reply-waiter shutdown race now assigned to RV2-DEBT-127. This was useful
full-path evidence, but it is not presented as the final post-fix aggregate.

Golden stability is already pinned for the integrated RV2-DEBT-101/122/123
tree: serialized `make golden-check` passed twice. Each run was followed by an
explicit tracked golden diff and untracked-golden inventory; both were empty,
so normal AST/MIR regeneration caused zero golden drift.

The deferred findings are intentionally separate: published canonical object
reclamation needs a real lookup lifetime protocol (RV2-DEBT-128); registry
capacity reads race resize outside the mutex (RV2-DEBT-129); close-error handle
invalidation is unspecified (RV2-DEBT-130); alias -> `own` -> tuple pattern
types are unresolved in sema and never reach runtime (RV2-DEBT-131); and a rare
pre-shutdown setup/publication hang occurs before RV2-DEBT-127's changed path
(RV2-DEBT-132). None blocks resuming the unfinished epic chain under the
owner-approved policy.

Final coordinator evidence is complete. The exact
`SURGE_SKIP_TIMEOUT_TESTS=0 make runtime-v2-check` exits `0` end to end. The
ownership corpus remains `1046/575/390/81` with exactly two pinned findings;
fd-registry, net-handle, HTTP owner, accept, lifecycle, TSan, performance, and
the full transport stage are all green. The shutdown reply-waiter row passes
inside that aggregate rather than only as a focused probe.

The final serialized `make golden-check` exits `0`. Explicit tracked and
untracked golden inventories are empty, the worktree is clean, and `STATS.md`
matches the exact generated statistics. Sentrux reports quality signal `6215`
against baseline `6212` (`+3`), `check_rules` passes all eight rules with zero
violations, and `session_end` classifies the result as stable/improved. The
initial documentation commit also received independent review APPROVE before
this confirmed-evidence follow-up.

## 2026-08-04 — Epic 23b Typed Storage And Carrier Design

The owner accepted the full typed-carrier direction with no transitional final
state and no compatibility obligation. The design must replace boxed value
composites and every erased `uint64`/`i64` runtime carrier across ordinary
calls, VM/native storage, task/channel/select/blocking/map/array paths, and
cross-shard transport. Old generated objects, source behavior, and native ABI
are not supported; no legacy wrapper survives acceptance. The version bump to
`0.2.0` happens when the completed work merges to `main`.

`Task<T>.clone()` is settled: it is legal iff `T` is clonable (`Copy` or a
valid `__clone`). Each consuming handle owns one result entitlement and every
successful await receives an independent `T`; non-clonable `Task<T>` is
affine. The design must keep clone work outside runtime locks on the owner
shard and prove VM/native/local/remote parity.

Surge is a friendly language. Wherever the compiler can prove the cause or a
safe repair, implementation must emit a precise diagnostic, a
machine-applicable fix when the rewrite is unambiguous, and a useful note/help
instead of a bare rejection.

This pass writes one normative storage/ABI design and one executable Epic 23b
document. It deliberately does not create a task-document hierarchy. Three
independent read-only audits run from detached temporary worktrees: diagnostic
surface, canonical-doc consistency, and carrier/acceptance coverage. No product
implementation starts until the reviewed documents are ready.

The live carrier census used Serena against the current tree. It found 25 LLVM
call sites through `emitValueToI64`/`emitI64ToValue`, 133 payload-bit field uses
across 22 native runtime files, VM async payload erasure through `any`, boxed VM
struct/tag values, VM frame/call `[]Value`/`LocalSlot.V`, `Object.Arr []Value`,
`MapEntries []mapEntry` with universal `Value` key/value fields, the `uint64_t`
channel buffer, and the two-word `uint64` native map entry. The final gate
therefore targets carrier symbols/signatures, type-aware VM owner storage, and
emitted behavior; it deliberately does not ban legitimate scalar VM `Value`
uses or 64-bit numerics, ids, generations, sizes, counters, and task-id queues.

The diagnostic audit confirmed that `Diagnostic` already supports multiple
notes and applicability-graded edits, but two user surfaces are incomplete:
`surge build` currently prints only the message, and the LSP DTO drops Notes and
Fixes. `fix once` can also choose a heuristic/manual edit when no AlwaysSafe edit
exists. Epic 23b therefore requires type-directed clone advice, observable
notes on supported CLI/LSP paths, and no speculative fix for non-clonable
`Task<T>.clone()`. The accepted code is `SEM3116`; generic `T` needs a deferred
`Clonable(T)` obligation rather than a raw monomorphization error.

The design and execution documents now exist as
`23-storage-model-and-typed-carrier-abi.md` and
`23b-inline-storage-and-typed-carriers.md`. `RUNTIME_V2.md`, the epic index,
Epic 23, `RULES.md`, and `DEBT.md` were reconciled around them without rewriting
Phase 1 evidence. New debt `RV2-DEBT-133` records the current Task clone
divergence. Epic 23b absorbs active close work for debts 031/056/062/080/082;
061 stays a regression sentinel and 125 keeps its unperformed evidence.

All three pre-draft independent research audits completed from clean detached
worktrees, reported no owner-level open question, and removed their worktrees.
Their findings were folded into the first review draft: full
carrier/terminal-path coverage,
physical byte credits with bounded metadata-only control lane, callbacks outside
runtime locks, friendly diagnostic propagation, semantic absence gates,
isolated worker worktrees, independent non-author review, and two-pass golden
stability. Product code has not started and no code/test/golden command is
claimed by this documentation pass yet.

Cross-checking the Epic 22 boundary exposed one would-be half measure: leaving
its six current `float`/composite deep-copy barriers until after 23b would retain
the very fallback that the full typed-carrier cutover forbids. Epic 23b now owns
those barriers through `plan_cross` plus recursive `cross_move`/`cross_clone`
operations. Epic 22 resumes afterward with WidthAny `int`/`uint` reclamation;
that COW/RC migration remains out of 23b.

Documentation-pass golden evidence: an alternate-index snapshot containing
only the intended docs was materialized as ephemeral commit
`4d6a82a9ff03e7714c1e22044ff21f761cf8d8fd` in a detached temporary worktree.
Two serialized `make golden-check` runs both exited zero; `git status --short --
testdata/golden` was empty after each run, and `git diff --check` passed. The
verification worktree was removed. Later changes in this pass remained
documentation-only.

The first independent review of the integrated draft used snapshot
`7147a7a7190da261dcbc8d12a512a69519c3f815` and correctly returned BLOCK. The
Epic 23b/README status was rolled back from READY while fixes are reviewed. The
review found: a proving-wave dependency cycle; a golden protocol that ignored
`golden-check -> golden-update`; heterogeneous select/resume descriptors; an
underspecified ordinary ABI/manifest sentinel; unchecked layout arithmetic;
Task clone failure/cancel semantics; dynamic cross-plan crediting; missing
fail-closed sanitizer/file-size gates; no numeric benchmark threshold; unclear
0.2.0 release boundary; diagnostic obligation/Help/LSP/fix-policy gaps; and
unowned liveness/Sentrux/status debt.

The integrated document now resolves those choices: verticals start only after
their foundation wave; goldens use regenerate/diff/manifest/repeat semantics;
descriptor tables cover heterogeneous arms/states; non-zero-sized composite
results use uniform sret destinations and by-value composites use canonical
argument slots, while ZSTs invent no payload; one generated manifest and
versioned link sentinel prevent Go/C drift; target-width layout and envelope
arithmetic is checked; returned internal Task-clone failures roll back exactly,
while user panic/exit and allocator `NULL` remain non-unwinding terminal paths
with no exact-drop claim and cancel stays task-global;
compiler-generated CrossPlan/cross-clone never invokes user
`__clone` and allocates under a fixed reservation; sanitizer and file-size
targets fail on skips; benchmarks use 2 warmups + 7 measured runs with 5%/10%
budgets; main version surfaces become 0.2.0 while the owner-triggered release
workflow stays external; and shared post-sema diagnostics add explicit Help,
AlwaysSafe-only fixes/Code Actions, and build/diag/LSP plus skewed-ABI tests.

Liveness ownership, Sentrux serialization, and RV2-DEBT-125/126 ownership are
now explicit. A new independent review snapshot is still required before the
epic can be promoted back to READY.

The independent re-review of snapshot
`08ce9c687869da73912817140bccf62edcce8c74` again returned BLOCK and prevented a
premature READY status. ABI review exposed that the draft promised cleanup
after user panic/OOM even though Surge deliberately has no unwind, and that
Task result entitlements, packed projections, and padding needed physical state
rules. Execution review found a remaining P3 dependency cycle, fail-open
untracked goldens, ambiguous benchmark aggregation/waiver authority, an empty
clean-tree `git diff --check`, and stale live Phase-4 wording. Diagnostic review
found hard-coded clone advice that could suggest an illegal spelling plus LSP
Code Actions with no stale-document guard.

The follow-up text now separates returned internal-status rollback from
process-terminal `panic`/`exit` and allocator `NULL`; defines Task result and
entitlement states, reader pins, last-consumer move, shutdown, and race probes;
freezes deterministic padding and unaligned packed-reference behavior; and
uses a resolver-backed clone capability. Ordinary advice spells the public
`clone(&value)` route, `.clone()` remains the local Task operation, and sealed,
far, structural, conflicting, or non-canonical targets never receive bogus
implementation help. AlwaysSafe LSP actions are bound to a trusted multi-file
snapshot, document versions, old text, and versioned edits.

P3 is now a local Wave-D proof, P4 adds far transport, and independent P5/Wave F
owns concrete/generic diagnostics. Golden acceptance requires zero tracked,
untracked, ignored, or missing generated files and a full path/content manifest;
the current docs worktree has empty golden status and an empty filesystem/index
census. Performance uses one frozen Wave-A harness, paired alternating seven-run
samples, explicit CV/median formulas, per-run absolute gates, and only the
project owner can approve a prospective threshold or intentional regression.
The final evidence compares `EPIC_BASE..HEAD`. A final independent delta review
is still required before promotion to READY.

Final-review candidate `69025d56077218a79c52d93bcbd2d109c16b68bb`
received execution/process APPROVE but remained BLOCKED by ABI and diagnostics;
the status therefore stayed in review. ABI found that two simultaneous awaits
could both enter `CLAIMED_CLONE`, contradicting the documented final move, and
that the absence census omitted current universal VM owner storage
(`LocalSlot.V`/call `[]Value`, `Object.Arr []Value`, `MapEntries []mapEntry`,
async/global/temp slots). Diagnostics found that optional generic advice could
accidentally create a failing `Clonable(T)` obligation and that an LSP
CodeAction debt escape contradicted the mandatory existing AlwaysSafe `own`
fix.

The next candidate adds a reserved `CLAIMED_MOVE_WAIT`: after the entitlement
cohort closes, exactly one not-yet-cloning waiter waits for readers and moves the
canonical result; all-awaited contention proves `N-1` duplications, while mixed
await/drop stays bounded by the closed cohort. The VM absence gate now has
source-shape, type-aware structural, and behavioral layers covering frame/call/
global/temp/async, dynamic-array grow/slice/index/teardown, and full map
lifecycle. Advice-only generic deferral is explicitly non-semantic and has a
no-Help/no-`SEM3116` negative; every reachable AlwaysSafe 23b CodeAction is
mandatory.

Three nonblocking diagnostic findings were deliberately recorded instead of
expanding blocker work: `RV2-DEBT-134` (tri-state/four-state naming),
`RV2-DEBT-135` (disk-only LSP dependency invalidation), and `RV2-DEBT-136`
(compiler-wide legacy fix-metadata audit). Execution review also left two
status annotations under existing `RV2-DEBT-126`.

The owner noticed that reviewers had relied on worktrees/`rg` while Serena and
agentmemory were centralized in the lead. The reviews were not accepted until
each reviewer repeated a focused Serena check, recalled memory, and saved its
new result. Execution saved `mem_msertea6_3f3fdf273f6a`; ABI saved
`mem_mserusgm_dd4e3b650a75`; diagnostics saved
`mem_mserufvw_690625d9e467`. Each temporary worktree and its local Serena
metadata was removed by its owner.

Corrected integrated snapshot
`aa5df892d7d114271c5131ab9cc36ab4c44bca00` received APPROVE from all three
independent lenses. ABI used Serena against the live Task/VM symbols and approved
the move-wait/cohort bound plus the expanded VM absence gate, saving resolution
`mem_mses3z6h_57bfcdd5d1bd`. Diagnostics used Serena against extern resolution,
clone emitters, safe-fix and LSP snapshot machinery; it approved obligation
provenance and mandatory AlwaysSafe actions, saving
`mem_mses434v_2a74bc4506c7`. Execution rechecked the acyclic waves, debt scope,
evidence, worktree isolation, golden/performance rules, and status consistency;
its earlier approved contract remains `mem_msertea6_3f3fdf273f6a`. Both
`git diff --check 90a9b7ab..aa5df892` and the prior-candidate delta passed.
Every reviewer removed its detached worktree and temporary Serena metadata.

With no unresolved design blocker or owner-level product question, Epic 23b and
the roadmap are promoted to READY FOR END-TO-END IMPLEMENTATION. Product code
has not started in this documentation pass. Nonblocking review findings remain
owned by `RV2-DEBT-126` and `RV2-DEBT-134..136` rather than expanding the epic's
blocking scope.

## Epic 23b Wave-A Oracle Hardening (2026-08-04)

The first independent implementation review correctly rejected the Wave-A
benchmark gate as unsound. Timing binaries still linked the resource-counter
bridge, allocation expectations could be vacuous or weakened by a second
row/cross-row contract, `make runtime-v2-carrier-check` omitted the live
carrier census, and the ambient Python launcher was not recorded as debt.

The corrected harness compiles two physically distinct candidate artifacts.
Timing binaries neither link nor contain any `rt_carrier_bench_*` symbol (the
gate uses full `nm -a`, including local symbols); resource binaries are
candidate-only and must contain the bridge. All base/candidate timing warmups
and all seven measured pairs finish before the first resource capture. Resource
elapsed/latency data is retained only as raw evidence and cannot enter
`TimingSample` or the performance score. Every attempt records capture kind,
row, side, phase, warmup/pair index, batch, and global ordinal; missing,
duplicate, reordered, or early-resource sequences fail closed.

The allocation oracle is fixture-owned, not bridge-owned. A real timing build
with no carrier symbols reports `0` for the zero fixture and `1` for the
deliberate `reserve(1)` allocation-control fixture through
`rt_heap_stats().alloc_count`; runtime-exit bridge metrics are `None` in that
binary. The control runs on both base and candidate before the matrix. Each of
the 46 rows has one exact
`candidate_structural_allocations_per_batch` value, checked on every candidate
warmup and measured batch. Row and cross-row `allocation_count` invariants are
schema errors, so paired scalar/composite rows remain classification evidence
only. Stuck-zero and uniform added-box mutations both fail. The caller/IR census
and every raw-to-structural classification are frozen in
`23b-wave-a-allocation-census.md`; agentmemory records are
`mem_msf5kyzx_8fc0fdbf696b` and `mem_msf6bcds_ee91d8b04952`.

Final author-worktree evidence before independent re-review:

- `make runtime-v2-carrier-check` passed: 56 Python harness tests,
  `TestRuntime(TestSyncPoint|CarrierBench)BuildFlag`, the complete
  `internal/carriergate` census, and the VM carrier tests under `-race`.
- `cd scripts && PYTHONDONTWRITEBYTECODE=1 python3 -m unittest
  runtime_v2_carrier_bench_build_test.BuildAndIRTests.test_real_candidate_zero_fixture_contract
  runtime_v2_carrier_bench_protocol_test.ProtocolTests.test_allocation_oracle_rejects_stuck_zero_and_uniform_boxing`
  passed the real split/oracle proof and both allocation mutations.
- Two serialized `make golden-check` runs exited zero. Their temporary
  delete/regenerate phase was allowed to finish before inspection. Both tracked
  and cached golden diffs were empty; the filesystem inventory exactly matched
  all 4,736 tracked files. The sorted path-plus-Git-blob corpus SHA-256 matched
  HEAD and the worktree at
  `93167613eb88c606461c950877a876a066c40c19d25ef4f42744696aed32c4a1`;
  the HEAD golden tree is `faaf955c86b1be089c4e403be12f596241b35804`.
- `clang-format --dry-run --Werror` passed for the changed native bridge files;
  `python3 -m py_compile scripts/runtime_v2_carrier_bench*.py` passed; the final
  staged and unstaged diffs must still pass `git diff --check` at commit time.

No benchmark overlay or base substitution was made. Raw `EPIC_BASE`
`7df10725e001ddf915d536aa58f880bd7e04aafd` still predates the blocking
register-then-verify lost-wake repair in `877e974c` and therefore requires an
explicit owner disposition before a complete Wave-A capture is accepted.
Python launcher/user-site hermeticity remains nonblocking and is recorded only
as `RV2-DEBT-144`; this hardening pass does not implement part of that debt.
The exact review-fix delta still requires the same independent reviewer before
Wave A can be considered accepted.

## Epic 23b Wave-A Final Rereview Repair (2026-08-05)

The same independent reviewer evaluated exact author commit `0d5adfcb` and
returned REQUEST_CHANGES with two P1 findings and no P0/P2/P3. First,
`_build_and_run` built the candidate-only resource artifacts before executing
the paired timing matrix; attempt events covered run order but could not expose
the asymmetric compiler/source/page-cache and thermal warmup from that build.
Second, strict future allocation budgets made the explicit Wave-A RED capture
unreachable on the pre-cutover runtime: the first real candidate warmup
reported `array-grow-composite allocation_count=327` against target `7`, so the
runner aborted with no rows instead of producing the promised complete RED
report. The review records are `mem_msf71oca_8453a11b4281`,
`mem_msf71of2_46f7bf4b9d9c`, and `mem_msf71oi4_349ea5b49a99`.

The repaired orchestration is a hard phase boundary. It builds both timing
artifacts, runs both live allocation controls and the complete counter-free
timing matrix, and validates the entire timing attempt sequence. Only then does
it build and run the candidate resource artifacts; the full combined sequence
is validated again afterward. A canonical 46-row CLI/main/report test observes
the actual `build_fixtures` and `_run_batch` calls and proves
`timing builds -> all timing runs -> resource build -> resource runs`.

`--phase=wave-a --capture-wave-a-baseline` is now an explicit execution mode,
not a late exit-code exception. It may collect only valid numeric non-negative
allocation-budget mismatches as typed endpoint RED records, including
warmup/pair and batch identity. The strict/final path still aborts on the first
such mismatch. Dead controls, missing/null/bool/invalid required allocation
metrics, checksum or identity drift, malformed output, timeouts, missing,
duplicate, or reordered attempts, and timing-protocol failures remain fatal.
Root pre-review caught the `null` availability boundary before commit; a
dedicated negative proves capture mode aborts and records no mismatch for both
missing and null `allocation_count`.

Baseline capture returns success only for a completed report whose actual and
expected row/attempt counts match and are nonzero, both controls and attempt
sequence passed, timing protocol passed, and endpoint status remains RED. A
green, partial, protocol-failed, or aborted report returns nonzero. The same
real `+1` allocation mutation produces all rows/all attempts and capture exit
zero in baseline mode, but an aborted report, nonzero exit, and no resource
build in strict mode. CLI `--help`, the Make echo, the allocation census, and
Epic 23b now state this same boundary. Resolution is saved as
`mem_msf7h3ej_15a492af853d`.

Author-worktree evidence before the next independent rereview:

- `make runtime-v2-carrier-check` passed 58 Python tests, the build-flag and
  complete `internal/carriergate` gates, and the VM carrier tests under race.
- `make check` passed repository Go tests, golangci with zero findings, strict C
  formatting/warnings, and the applicable file-size gate.
- Two serialized `make golden-check` runs exited zero. Staged and unstaged
  golden diffs are empty; the filesystem inventory exactly matches 4,736
  tracked files. HEAD and worktree path-plus-Git-blob corpus SHA-256 are both
  `93167613eb88c606461c950877a876a066c40c19d25ef4f42744696aed32c4a1`;
  the HEAD golden tree remains `faaf955c86b1be089c4e403be12f596241b35804`.

Raw `EPIC_BASE` remains
`7df10725e001ddf915d536aa58f880bd7e04aafd`; this repair neither overlays nor
substitutes it, so the pre-existing blocking lost-wake remains an explicit
owner checkpoint. Python launcher/user-site hermeticity is still only
nonblocking `RV2-DEBT-144` and received no partial implementation. The exact
new delta still requires the same independent reviewer before Wave A is
accepted.
## 2026-08-05 — Epic 23b Wave B3 owner-private SlotControl

Wave B3 workstream C was implemented in an isolated worktree from
`0372c0b721c8e1790ff27772ba3419c39efb67d4`. The native control embeds the
owner's sole authoritative `rt_slot_header`; the frozen B2 header remains
state-only, while descriptor identity, physical storage, logical identity,
generation, epochs, read pins, exclusive claims, and destination reservations
remain owner-private. Every `_locked` entry point requires the caller's
existing owner lock(s), acquires no lock, and invokes no `ValueOps` callback.

Claims are detached immutable tokens. Caller-owned read registrations bind the
exact stable source/destination controls, source epoch, operation kind, and
complete destination identity/generation/epoch, so a byte copy can complete the
same entitlement once but cannot be retagged, redirected, or replayed against a
distinct control pair with identical scalar fields. Destination reservations
retain the same exact control binding and source claim fields. After a read
callback returns, source retirement
and destination publish/release/reject may occur in either order; an unresolved
reservation survives a later source move and generation advance, while a token
rewritten to the live generation is rejected. Rejected initialized
destinations stay in `CLEANUP` until their fully initialized mock value is
dropped outside the owner lock. Move/drop acceptance changes the source to
`CLAIMED` before the callback and has no recoverable post-callback refusal;
only failed cross-move with explicit source-unchanged/destination-empty evidence
restores the source.

Two implementation-review findings were blocking and fixed in scope. First,
opaque operation identity plus caller storage metadata could describe the wrong
physical range. Controls and tokens now retain the concrete process-static
`rt_value_ops` descriptor; init requires exact `layout.size` (including ZST), a
valid supplied alignment satisfying `layout.align`, checked `uintptr_t`
interval arithmetic, and overlap checks use the descriptor's canonical size.
Second, a read token could previously change one read kind into another while
retaining its epoch; the exact registration binding now rejects kind and
destination mutation without decrementing the pin. No nonblocking product
finding remained to add to `DEBT.md` in this workstream.

The checked-in C/Go acceptance harness covers multiple/out-of-order readers,
double retirement and double terminal commit, stale generations, terminal
same-generation reuse rejection, epoch mutation/order, every legal read
destination ordering, readers blocking move/drop/reuse, copy/clone cleanup,
irrevocable move/drop, guarded cross-move rollback, logical self versus physical
overlap, adjacent and wrapping ranges, descriptor size/alignment mismatch, and
same-address zero-sized values aligned to 64/256/4096 without payload access.
The production SlotControl headers and sources are structurally checked to
contain no `ValueOps` call expression. The stateful single-thread mock callbacks
also use `pthread_mutex_trylock` as a harness sequencing check, but that row and
TSan instrumentation are not a behavioural proof against owner concurrency;
the final OpTransaction/owner integration wave owns real lock-held and pthread
negative controls. `runtime-v2-slot-control-check` is explicit, fail-closed on
missing ASan/UBSan/TSan support, reachable from `runtime-v2-check`, and covered
by the gate-integrity test.

Author evidence before independent non-author review:

- `make runtime-v2-slot-control-check` — PASS, including normal,
  ASan+UBSan, TSan, structural, and mandatory-sanitizer mutation rows;
- `go test ./internal/gatecheck -run '^TestGateSelectionsAreLiveAndComplete$'
  -count=1` — PASS;
- `make c-check` and `make cppcheck` — PASS;
- `make runtime-v2-abi-manifest-check` — PASS;
- B2 manifest/generated hashes remain byte-identical:
  `5c95dc2f...89e6`, `3a911480...58c7`, `cc205422...c88`, and
  `75f9ab67...2896`; their focused `git diff --exit-code` is empty;
- `make golden-check` — PASS for both serialized runs; afterward there are no
  `.golden-update.*` directories and tracked, untracked, ignored, and content
  diffs under `testdata/golden` are empty;
- `make check` — PASS, including default tests, lint, strict C compilation, and
  file-size enforcement.

This is author evidence only. The workstream is not accepted until the planned
independent non-author review completes.

### Independent-review correction

The first independent review returned `REQUEST_CHANGES` with two P1 findings.
First, init accepted malformed B2 operation descriptors and claims could start
capabilities absent from their descriptor. Slot init now validates the complete
frozen contract before mutating its output: only known flags; canonical checked
size/alignment/stride including ZST; mandatory `move_init` and `plan_cross`;
and each optional callback present if and only if its exact capability flag is
set. Claim entry points reject unsupported copy/clone/trace/cross-move/
cross-clone before owner state, epoch, reader, or reservation mutation, while
still clearing the output token deterministically on failure. Move remains
mandatory. Drop remains a lifecycle transition for every initialized value, so
a trivial non-DROPPABLE scalar or ZST performs no callback and commits directly.

Second, logical ids, generations, epochs, operation identity, and kind could be
identical in two distinct control pairs. Tokens, read registrations, and
destination reservations now also retain exact source/destination control
pointers; controls are pinned at stable addresses while a token is live. The
acceptance matrix constructs such scalar-identical pairs and proves cross-pair
read publication, read retirement, and exclusive move commit are rejected
without consuming either entitlement or mutating the target pair. Own-token
completion remains exactly once.

The correction adds exhaustive flag/callback polarity negatives, layout and
mandatory-callback negatives, coherent ZST descriptors, unsupported-capability
no-mutation rows, callback-free trivial drop, and scalar-collision replay rows.
The proof boundary remains deliberately narrow: source inspection proves this
component contains no callback invocation; actual owner-lock concurrency proof
belongs to the final OpTransaction/owner integration wave.

Correction author evidence:

- `make runtime-v2-slot-control-check` — PASS for all eight modes under normal,
  ASan+UBSan, and mandatory TSan runs, plus the structural and fail-closed rows;
- `go test ./internal/gatecheck -run '^TestGateSelectionsAreLiveAndComplete$'
  -count=1`, `make c-check`, and `make cppcheck` — PASS;
- `make runtime-v2-abi-manifest-check` — PASS; the frozen manifest, C header,
  C checks, and LLVM Go sidecar remain byte-identical at
  `5c95dc2f...89e6`, `3a911480...58c7`, `cc205422...c88`, and
  `75f9ab67...2896`, with an empty focused diff;
- `make check` — PASS, including default tests, lint, strict C compilation, and
  file-size enforcement;
- `make golden-check` — PASS for both serialized runs; afterward no
  `.golden-update.*` directory and no tracked, untracked, ignored, or content
  diff exists under `testdata/golden`.

This is correction-author evidence. Acceptance still requires a fresh
independent non-author re-review of the corrected range.

## 2026-08-05 — B3A recovery and strict-contract closeout

The damaged primary checkout was recovered without losing accepted work. A
verified full copy and Git bundle were created under
`/home/zov/projects/surge/recovery/`; the primary checkout now points at the
accepted `8512f4c1` staging tree. B3A callable-closure work was recovered from
its persistent writer worktree and transplanted onto a dedicated integration
branch based on `8512f4c1`. Rescue refs remain pinned until the integrated tree
passes final review and gates.

The project owner approved the remaining entrypoint and clone semantics:

- every argv parameter requires the exact public
  `FromArgv.from_str(&string) -> Erring<T, Error>` contract, including a
  parameter with a default; the default is used only when that runtime argument
  is absent;
- stdin accepts exactly one parameter with exact public
  `FromStdin.from_stdin(string) -> Erring<T, Error>`; EOF is the empty string
  and stdin defaults are forbidden;
- there is one canonical clone implementation per `T` program-wide, while
  every direct or deferred/generic source use is checked against the lexical
  access module; process-static `ValueOps<T>` is not source-level visibility.

No legacy ABI/API adapter, fallback, or compatibility wrapper is allowed.
Compiler-proven violations require early source diagnostics with actionable
notes/help.

Current blocking closeout findings:

- the recovered B3A tree introduces 32 `make lint` findings while the accepted
  `8512f4c1` base is lint-clean;
- 23 files violate Runtime V2 file-size/modularity rules: four new files exceed
  500 lines and nineteen pre-existing over-limit files grew;
- the Epic 23b-required
  `make runtime-v2-file-size-check EPIC_BASE=<recorded-base>` target does not
  exist; the old dirty-tree size script cannot prove the committed epic diff.

An independent read-only symbol audit produced non-overlapping split ownership
for sema graph/closure, driver integration, mono, HIR/symbols, driver pipeline,
backend/VM/MIR, plus a fail-closed committed-diff gate. First implementation
wave uses separate persistent worktrees for argv/stdin, clone visibility, and
the file-size gate. File splitting follows only after those functional commits
are integrated, so authors do not edit overlapping symbols.

Focused evidence retained from the recovered tree:

- sema/mono/MIR/driver/buildpipeline/LLVM package tests passed;
- exact B3A VM/LLVM entrypoint/index/composite tests passed;
- `make golden-check` passed from a clean baseline;
- the isolated fixed-array stride correction and its degenerate length 0/1
  follow-up passed independent review and focused/full LLVM tests;
- the exact immediate-on source-override test passes on both `8512f4c1` and the
  B3A tree, including ten serialized runs, so the broad runtime-v2 package
  timeout remains the already recorded package-order/resource debt rather than
  a B3A regression.

Combined-tree checkpoint at `4cea0865`: `go test ./internal/sema
./internal/mono ./internal/mir ./internal/driver ./internal/buildpipeline
./internal/backend/llvm -count=1 -timeout 10m` passed. This verifies the
transplant together with the accepted Wave A/B3 tree before the strict-contract
authors begin; lint and file-size remain the named blockers above.

The first non-overlapping file-size batch owns only backend/VM/MIR extraction:
fixed/dynamic array drop helpers leave `emit_drop_glue.go`; tag construction
leaves `emit_intrinsics_numeric.go`; index operations leave
`vm/intrinsic_ops.go`; and suite-local MIR compile helpers leave the three
legacy oversized test files. This is a symbol move with no intended behavior or
golden change. Proof is gofmt, exact package tests for LLVM/VM/MIR, focused
fixed-array/index rows, diff inspection, and line counts against `8512f4c1`.

Batch-8 evidence after the moves:

- `go test ./internal/backend/llvm -count=1 -timeout 10m` passed;
- `go test ./internal/mir -count=1 -timeout 2m` passed;
- focused fixed-array stride/drop and VM index/reborrow rows passed;
- `SURGE_SKIP_TIMEOUT_TESTS=1 go test ./internal/vm -count=1 -timeout
  90s` passed in 26.5s;
- the exact test that the broad package left running,
  `TestRuntimeV2ImmediateOnPoolProductionCapabilityFailsDeterministically`,
  passed serialized in 4.7s.

The intentionally unfiltered `go test ./internal/backend/llvm ./internal/vm
./internal/mir -count=1 -timeout 10m` is not a green gate and failed as the
existing debt predicts: legacy LLVM parity/HTTP/fs rows, the stale native scope
harness, one far-task witness failure, and finally a package timeout with the
immediate-on test compiling while parallel MT tests waited. This run was not
retried or hidden. Its changed-owner focused rows and the supported skip-timeout
package command are green, so it does not block this symbol-only split.

The next independent file-size slice split the new 762-line driver closure
suite by responsibility without changing test bodies: alias/function-value
authority moved to `instantiation_closure_aliases_test.go`, and deferred-call
reachability/expansion stability moved to
`deferred_callable_reachability_test.go`. The remaining suite is 419 lines;
the new files are 225 and 142 lines. Byte-for-byte function comparisons passed,
all four moved rows passed focused, and `go test ./internal/driver -count=1
-timeout 5m` passed.

The next non-overlapping file-size slice owns only whole-symbol moves in HIR
and symbols: call/access expression payloads leave `hir/expr.go`, range-literal
lowering leaves `hir/lower_expr.go`, and the concrete extern byte-receiver
regression row leaves the legacy 1481-line symbols resolver suite. No body,
behavior, diagnostic, or golden change is intended.

HIR/symbols split evidence: the three moved declaration blocks compare
byte-for-byte after excluding only their former inter-symbol separator newline;
the old/new SHA-256 pairs are identical. `expr.go` is now exactly 500 lines,
the other legacy files shrink, and the three new files are 38--53 lines.
`go test ./internal/hir ./internal/symbols -count=1 -timeout 5m`, new-diff
lint, and `make golden-check` pass. Serena's Go LSP again retained stale old
locations after the move, while repository search and the Go compiler each
find one declaration; compiler results remain authoritative for this known
tool-cache issue.

The next non-overlapping file-size slice owns only whole mono symbol moves:
authoritative closure seeding/use-site indexing leave `monomorphize.go`,
crossing substitution leaves `subst_apply.go`, and the expanded invariant
suite is grouped into authority, rewrite, and compile-helper files. No function
body, behavior, diagnostic, or golden change is intended.

Mono split evidence: all five old/new symbol groups compare byte-for-byte with
matching SHA-256 pairs. `monomorphize.go` is 594 lines versus the accepted
640-line base, `subst_apply.go` is 543 versus 546, and the original invariant
suite is 215 versus 281; all five new files are 36--246 lines. `go test
./internal/mono -count=1 -timeout 5m`, new-diff lint, and `make golden-check`
pass. Serena again retained stale pre-move declaration locations, while the Go
compiler accepted the single current definitions.

## 2026-08-05 — B3A closeout and clone-review follow-ups

B3A is complete on `codex/epic23b-b3a-followups-20260805`, based on the
recovered staging tree `8512f4c1`. Four workstreams integrated:

- entrypoint contract (`1ed48340`, `119e5fad`): exact public argv/stdin
  parsers, and post-merge entrypoint decisions localized back into the
  per-file result that owns them;
- committed file-size gate (`b21ae83b`, `86cd168f`, `010cf014`);
- clone canonicality (`adbcc682`, `d87c6680`, `99b3c536`, `4b1d3cb2`): one
  canonical `__clone` body per concrete `T`, chosen program-wide after
  specificity ranking, with the use site's lexical view applied only to the
  winner. Two new codes: `SEM3185` equal-best conflict, `SEM3186` winner not
  visible. `testdata/golden/vm_arrays/arrays_drop_nested.sg` is the one corpus
  program the rule changed -- its `Foo.__clone` had to become `pub` because a
  generic in `core/array` clones it -- and that is the sanctioned breaking
  change: a module-private `__clone` reached through a cross-module generic
  used to compile;
- lint and file-size closeout (`ed176f57` through `a8aa2fce`).

Two independent reviews approved with no P0/P1. The entrypoint review found
the publication-order defect that `119e5fad` fixes. The clone review
(`010cf014..4b1d3cb2`, seven lenses, all gates rerun green) raised three P2 and
three P3.

This wave answers them:

- P2-1 docs (`178565ed`): `LANGUAGE.md` §6.8 Clone Protocol, mirrored in the
  `.ru.md` twin, with the breaking change called out; the `@copy` sections of
  `ATTRIBUTES.md`/`ATTRIBUTES.ru.md` point at it.
- P2-2 quick fix (`87a1883e`): `SEM3186` now carries the edit instead of only
  naming it. `CallableCandidate` gained a `DeclKeyword` anchor covering the
  declaration's `fn` keyword -- `Source` is untouched, since existing
  diagnostics and goldens consume it -- and the fix replaces that keyword with
  `pub fn` under an exact `fn` guard. A file-private winner still gets no edit,
  because `pub` does not fix it.
- P2-3 fail-closed seam (`87a1883e`): HIR refused to lower a `clone` whose
  request sema recorded and whose answer never arrived, instead of falling
  through to an ordinary call. Keyed by owning file, so the merged authority's
  foreign requests cannot misfire; Copy and generic clones record no request.
- P3-3 (`87a1883e`): mono's "no authoritative implementation" refusal has a
  test.
- P3-1, P3-2, and P2-3's untouched root cause are `RV2-DEBT-146`, `-147`, and
  `-148`.

Gates: `golangci-lint` 0 issues; `runtime-v2-file-size-check
EPIC_BASE=8512f4c1` PASS, 181 files, 0 violations; the eight-package battery
green; `make check` green through the commit hook; `make golden-check` exit 0
with a clean tree.

## 2026-08-06 — B3A accepted and staged

Corrections to the entry above: its gate figure was written mid-series — the
file-size gate measured 181 files at `178565ed`, 183 after the quick-fix
commit, and 185 after the budget repair `ea65c628`, with zero violations at
every point; and the work landed on the integration branch, not the follow-ups
branch it was drafted on.

Acceptance at `ea65c628`: `make check`, `make lint`, two serialized
`make golden-check` runs, `runtime-v2-file-size-check EPIC_BASE=8512f4c1`
(185 files, 0 violations), and whole-range `git diff --check` are all green.
The independent final range review of `8512f4c1..ea65c628` returned APPROVE
with zero P0/P1/P2: all nine cherry-picked workstream commits have
byte-identical `git patch-id --stable` values against their reviewed
originals, the post-verdict lint/follow-up series were reviewed in full
(pointer-wave aliasing hunt clean, splits proven move-only by AST
declaration-body hashing), and the full `./internal/...` suite under the
skip-timeout environment has an empty failure set.

Because that environment skips the native rows, the recorded baseline set was
rerun with `SURGE_SKIP_TIMEOUT_TESTS=0` at `ea65c628` and compared against
the accepted-base audit: every accepted-PASS row still passes, every
accepted-FAIL row fails unchanged, and `TestLLVMSmoke/array_element_ref_field`
— a pre-existing native segfault at the accepted base — now passes, fixed by
this range's shared-reborrow and index-place lowering corrections. No
regression; one pre-existing defect closed as a side effect.

The review's three P3s are closed in this commit: the two census/e2e test
comments now describe behavior instead of naming the epic, the retained mono
clone rediscovery scan is tracked as `RV2-DEBT-149` (owned by the
operation-plan registry work), and this correction note fixes the stale
figures. The staging branch advances to this commit.

## 2026-08-06 — B3B: the operation-plan registry

The B3B plan was accepted only after three independent review rounds (3 P0 +
6 P1 in the first, 3 P1 in the second, PASS with two staged P2 folded into
their commits) and is recorded, with its revisions, in agentmemory. The
implementation landed as ten reviewed commits on the integration branch:

- `5b3efb7b` publication identifies its owning file from the AST-file span
  both fields now derive from; an unknown identity fails closed naming both
  id spaces. Measured on the unfixed tree, the drifted shape published no
  clone symbols and left the entrypoint binding on the non-owning file.
  `RV2-DEBT-148` is closed.
- `4a068371` the four capability attributes travel as detached per-type facts
  merged across records before the closure runs — pure OR, then one
  validation naming every contributing module; a file whose own bag already
  reports the contradiction does not flush the pair.
- `c8188027` the layout root census carries Value and Key roles; the map-key
  probe rides the existing handle-payload walk, so the physical root set and
  the layout registry hash are unchanged.
- `ae22667c` + `0b2e250e` builtin extern blocks reach the merged catalog
  once: the census found TWO duplication shapes (996 records for 498
  operations), so the fold sits at the merge that first joins two modules —
  the selection-time collapse is deleted. `RV2-DEBT-146` is closed.
- `631e40ca` the authoritative capability classifier: clone strictly through
  the canonical selector's use-site-free Select; ShardMovable as the
  owned-move verdict under a greatest fixpoint, the emission-facing axes
  under a least one; CarrierDroppable deliberately disagrees with ownsHeap
  and is wired into nothing yet.
- `f6bf5c7e` required clone operations derive from the reachable closure and
  re-enter it as explicit roots — a third root input beside the seed policy —
  iterating to a budget-bounded fixpoint before the single monomorphization
  pass. The golden corpus did not move.
- `e9ed2e93` `internal/valueops`: an immutable registry whose ABI flags carry
  only what is emissible today (COPY, CLONABLE), whose staged verdicts live
  in a separate non-ABI field, and whose independence from the compiler is
  enforced by an import-absence test and a no-live-references walk.
- `19d84ab3` both production pipelines publish `m.Meta.Operations` after
  layout finalization, resolving each CLONABLE entry's `clone_init` through
  the callable map and failing closed on a nil identity or a missed lookup.
- `49b8c8f8` the clone rediscovery scan is gone: `rewriteCloneCall` refuses
  on every builder, and `ensureFunc` shed its unused result. `RV2-DEBT-149`
  is closed.

Every commit passed the normal pre-commit hook; each lane also ran
golden-check, the crossing gate, and the file-size gate against the epic base
in its own worktree before integration. Wave C/D own the staged slots
(drop_in_place, trace, cross ops, key hash/equal) and the migration of the
staged capability bits into the ABI flags as those slots fill.
