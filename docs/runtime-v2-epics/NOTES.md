# Runtime V2 Working Notes

This is the live handoff log for Runtime V2 work. Keep it current during each
task, then move durable decisions into the owning epic document before closeout.

# Epic 13 Prep (2026-07-09)

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
  `SEM3194` rather than failing first as a plain non-capability struct.
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
implementation landed as ten reviewed feature commits on the integration branch, followed by two budget repairs and this record's corrections:

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
- `6ea90a29` required clone operations derive from the reachable closure and
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
golden-check and the crossing gate in its own worktree before integration.
The committed-diff file-size gate was re-run on the integrated tree, where it
caught two budget regressions the lanes had missed — a one-line legacy growth
in the diagnose pipeline and a rewrite-threshold crossing in the monomorphize
spine — both repaired before acceptance (`512269bb`, `4745f5d1`). Wave C/D own the staged slots
(drop_in_place, trace, cross ops, key hash/equal) and the migration of the
staged capability bits into the ABI flags as those slots fill.

## 2026-08-13 — Wave C and Wave D, written up 60 commits late

This entry exists because the log stopped. The last one before it was
`83802f13` on 2026-08-09, and sixty commits landed after it covering the rest of
Wave C and all of Wave D so far. The rule this file serves — update before
starting a workstream and again at closeout — was not followed, and the cost was
not hypothetical: a session opening on 2026-08-12 read a handover note claiming
"three pre-existing red gates" and inherited three rows marked Open whose fixes
were already committed. Both statements were wrong, and this file is where they
would have been checked.

### What Wave C left, and how it was found

Wave C's LLVM cutover broke every native async form and was repaired inside the
wave. Its integrated tree then carried two crashes into Wave D, one of which —
the far-channel copy-in — was fixed uncommitted and nearly lost.

The wave's own lesson, worth more than its diffs: `make check` cannot see this
class of work. It runs `go test ./...` with `SURGE_SKIP_TIMEOUT_TESTS=1` and a
90s package timeout, so the behavioural corpus, the native lane, and every
valgrind witness are outside it. Several changes shipped green through the
pre-commit hook while the native lane was red.

### Wave D — D0 closed, D1 still short of its own point

D0 closed all nine of its blockers, including one found during the pass: the
mandatory `runtime-v2-carrier-sanitizer-check` did not exist as a Makefile
target at all, having been named mandatory in three documents and never run
once. `make` exits 0 on any unknown target — the Makefile ends in a catch-all
`%:` — so its absence read as a pass for as long as anyone had been looking.

D0 also moved the ABI manifest hash and link sentinel to
`f30fcfb03b62d105dab0cd21d57a3dcc029b0e7ae10a337e966079033608650a`: objects
built before `b9f647c5` will not link against a runtime built after it.

**D1 has not performed its migration.** `internal/vm/object.go` still declares
`Arr []Value`, the field Wave A's census names as the target. Everything spent
on D1 so far — thirteen ledger rows filed, seven closed — is pre-existing native
defects found while SCOPING the flip. The plan named zero defects for D1. That
is the single most useful fact for sizing D2 through D8: the plan's step list
does not predict the cost, because the cost is dominated by what the plan cannot
see.

### The defects D1 uncovered, and the pattern joining three of them

`RV2-DEBT-203` fixed-array signature keys spelled `T[N]` as `[T]` in two
producers; the annotated form was refused and the INFERRED form miscompiled.
`209` array and tuple literals aliased a named binding instead of moving it.
`204` the native element store did not free what it displaced. `208` the same
shortfall at every non-element place, fixed in sema rather than in the emitter
because the VM ran the same sema and freed correctly, so the obligation was
recorded for no place at all. `205` a compare arm whose result is a bare element
read loaded through a pointer AFTER the arm freed the payload — a use-after-free
that exits 0, not the wrong answer the row claimed. `206` a slice minted a view
header nobody owned, and a fixed array's slice could outlive the frame that
backs it; the leak was sema disagreeing with itself and the escape is now
refused as SEM3198.

Three of those are the same shape and it has now bitten this epic three times:
**a leak was the only thing keeping a second owner from freeing what the first
one would.** 204's release detonated 209. 208's first form detonated a
loop-binding double free (`211`) that had already been live for the whole-binding
spelling. Fixing a leak is a reason to go looking for the second owner, every
time.

### What the gates could not see, and now say

Enumerating gate targets out of the Makefile rather than out of a handover found
EIGHT red, not three; four had no ledger row and are now `RV2-DEBT-213`. Worse,
`internal/vm` itself is red — twenty failures under
`SURGE_SKIP_TIMEOUT_TESTS=0`, invisible to `make check`, two of them substantive
and both present at `ab55197c`. `runtime-v2-file-size-check` gained an exemption
list so a diagnostic code can be declared in `codes.go` where the owner ruled it
must live; it prints `EXEMPT` rather than hiding the growth.

Method notes earned, in the order they cost something: the file-size gate reads
COMMITTED blobs, so it must be run AFTER committing — run before, it measures
the base and passes for the wrong reason. `git archive <sha> | tar -x` gives a
pristine base tree for "was this red before me". And an acceptance test can pass
vacuously: sema's snippet harness has no stdlib, so a test asserting "no
diagnostic" is green for programs it never typed, and would stay green with the
rule deleted.

## 2026-08-13 — Wave D/D1 resumed: four owner rulings, and the first full gate baseline

Opened with the four questions the previous handoff demanded, before any work.
Three of them decided whether a language capability stays. All four are recorded
in the rows themselves (`RV2-DEBT-212`, `213`, `214`, `215`); the summary is:
`for-in` is READ-ONLY forever, the reference-typed `compare` arm is refused
forever, `215/216` runs the bignum-bounds experiment before shipping
`rt_range_free`, and `213` is parked WHOLE — including the triage.

`213`'s reason is the part worth carrying: those targets are aimed at Runtime v1
and need REWORK, not repair. Fixing them now is fixing what gets rewritten.

### The branch was not where its name said

`codex/runtime-net-scheduler-refactor` existed LOCALLY at `90a9b7ab` (3 August,
184 commits back) while `origin/codex/runtime-net-scheduler-refactor` was
`1d4d6813`, identical to the working tree. Checking it out naively would have
rewound the tree by 184 commits. Fixed with `git branch -f` before checkout. A
branch name is not a commit; compare against the remote before trusting it.

### First full gate baseline, `1d4d6813`, 66 minutes, all 26 targets

Enumerated from the Makefile and run SERIALLY — several targets rebuild
`./surge`, and `runtime-v2-http-owner-check` is load-sensitive, so a parallel run
would have measured the machine. Logs and exit codes are per target, captured on
their own line rather than through `; echo $?`.

GREEN and load-bearing for what comes next: `check`, `golden-check`,
`behaviour-check-all`, `behaviour-check-mt`, `heap`, `place-overwrite`,
`carrier-sanitizer`, `file-size`. The two behavioural lanes matter because the
next two steps add REFUSALS, which can break programs that compile today.

RED, and every failure text reproduces the ledger assertion-for-assertion rather
than merely "the same gate is red": `fd-registry`, `net-handle`, `accept`,
`ownership` (`213`), `transport` + `transport-contract` (`202`), `carrier`
(`174`, both halves of its correction), and `runtime-v2-check` as their
composite. `http-owner` failed in the suite and passed alone immediately
afterwards — the load sensitivity `213` records, confirmed rather than assumed.

`runtime-v2-check` is a COMPOSITE of 18 other targets, so a full sweep runs most
of them twice. That is not waste — section 12 wants that target twice anyway, and
the second run is a free reproducibility check — but it doubles the wall clock and
should be planned for.

### Two findings the baseline produced that no row had

**The `internal/vm` failure set has a varying member.** Seventeen rows are
deterministic and identical to what `213` records. The eighteenth is an MT slot
whose IDENTITY changes between runs: `213` names `TestMTStructuredConcurrency`,
and at `1d4d6813` that test passed while `TestMTCorrectnessHTTPServer` failed in
its place, on a connection refused at minute 66 of continuous load. Both are in
`mt_correctness_test.go`. At least two load-sensitive members, neither pinned.

**Sentrux cannot currently serve as the Rule 3 gate**, filed as `RV2-DEBT-217`.
`internal/.sentrux/baseline.json` did not exist, so the structural regression
half could not run at all on the directory holding every compiler change — a gate
that reads as present and measures nothing. Created by owner decision, with the
caveat recorded in the row: taken retroactively 184 commits in, green by
construction, never shown able to fail. Separately `sentrux check` is RED on both
ENFORCED runtime scopes (`min_redundancy` 0.2408 and 0.2409 against 0.2450) and
no row had said so.

## 2026-08-14 — the three ruled refusals shipped: 207, 212, 214

All three rulings from 2026-08-13 are implemented at `b5199277`, and they turned
out to be ONE rule about storage read through three representations, exactly as
the owner's "reuse the predicate" instruction implied.

### The reuse was real, and it shrank two files that were over the limit

`frameLocalStorageLabel` answers "does this symbol's storage die when the
function returns" once, in the words the diagnostics print. Two rules had a
byte-identical `switch sym.Kind` and a third, `isFrameLocalStorage`, spelled the
same thing without the label. `loanRootBase` is the second extraction: the
three-table walk from a reference back to the binding its loan roots in.

`refuseDisallowedStore` is the third. The `for-in` refusal wanted to sit exactly
where `storesThroughSharedRef` already sat — before any bookkeeping, before the
place is expanded through its borrow — so instead of a second `if` in a legacy
file, the two became one question: does the language permit this store at all.

That mattered mechanically, not only aesthetically. `borrow_runtime_ops.go`
(587 effective) and `type_expr_compare.go` (662) are both over the 500 limit, so
ANY effective-line growth trips `LEGACY_GROWTH`. Both ended SMALLER — 586 and
658 — because each rule's own code went into its own new file and only the call
remained. Worth internalising: in this repo, a legacy file is a reason to
extract rather than a reason to negotiate the gate.

### The accepting fixture is the one that decides a predicate is right

Each of the three refusals ships with a pair, and in every case the valid half
was the hard one:

- a `Range` cursor over a `&Item[]` REFERENCE PARAMETER, returned, is correct
  and must stay legal — it falls out of `frameLocalStorageLabel` rather than
  being special-cased;
- `&inner[0]` over a BORROWED scrutinee is legal and common, so the SEM3200
  predicate is `armFreesPayloadBinding` and NOT "a reference left an arm";
- `fn total(xs: &mut Item[]) { for it in xs { ... } }` compiles today, so
  SEM3202 tests the `&mut` SYNTAX in the iterable position and not the type.

All three valid fixtures RUN and agree on both lanes (66, 8, 33) rather than
merely reporting nothing — the vacuous-acceptance trap this project has already
paid for once.

### Negative controls, and what they sharpened

All three invalid fixtures were compiled at `92d38194` in a `git archive` tree
and reported NOTHING, so all three go red on revert. Running them there also
re-derived the defects independently: 207's VM returns 66 while the native lane
fails outright, and 214 on the native lane returned **22, 45, 97, 38 across four
runs** against 8 every time on the VM. The row had two samples and read as "a
wrong answer"; four make it what it is, a nondeterministic read of freed storage.

### The commit could not be split, and the reason is worth keeping

The plan called for one commit per rule. The pre-commit hook runs `make check` on
the WORKING TREE, and the linter fails on an unused function, so a tree holding
rule N's helper without rule N's call site cannot be committed at all. Splitting
by file was impossible anyway: `internal/diag/codes.go` is the single registry by
owner ruling, so all four codes live in one file and one hunk-split would be
needed per commit. The refactor's behaviour-neutrality is therefore evidenced by
measurement rather than by isolation — `make behaviour-check-all` green on both
lanes at exactly that tree, plus `golden-check` finding no corpus diff — and the
commit message carries the structure the commits would have.

### Two tests changed meaning rather than being deleted

`TestAssignToLoopBinding*` pinned RV2-DEBT-211 by asserting that assigning to a
loop binding records no drop. The shape is now refused, so the assertion became
unreachable. They assert the refusal instead, and they assert it TWICE per
program: a rule that fires once and then lets the binding become an ordinary
owner would be the same bug wearing a diagnostic.

### Gate results at `b5199277`

Green: `make check` (the pre-commit hook, so this was mandatory), `golden-check`,
`runtime-v2-file-size-check EPIC_BASE=f2641713` at 69 files and 0 violations,
`behaviour-check-all` at 702s on BOTH lanes, and `behaviour-check-mt` at 242s.
The two behavioural lanes and MT were run because these three changes are
REFUSALS, which can only break programs that compile today; the file-size gate
was run after committing, because it reads committed blobs and passes for the
wrong reason before.

The full 26-target baseline was NOT re-run and did not need to be: it was taken
at `1d4d6813` on 2026-08-13, and `git diff --name-only 1d4d6813..92d38194`
returns three files - two documents and `.sentrux/baseline.json` - with ZERO
`.go`/`.c`/`.h` among them, so the baseline carries forward verbatim. Recording
the reasoning rather than the conclusion: a baseline is reusable exactly as far
as the diff that separates it from HEAD.

`TestLLVMParity/random_pcg32` fails at HEAD and fails IDENTICALLY at `92d38194`
in a `git archive` tree - a bignum/division disagreement in
`stdlib/random/random.sg` reached through `wrap_u64_shift_left`. It is part of
the pre-existing `internal/vm` red set that `213` parks, and `make check` cannot
see it because `SURGE_SKIP_TIMEOUT_TESTS=1` skips it.

### The one quick fix, and why only one

RV2-DEBT-212's `&mut` half is the only one of these four refusals where the
author has a single-span edit available: deleting `&mut` leaves a loop that
compiles and behaves identically, which is the very argument for refusing the
spelling. SEM3202 therefore ships a `surge fix`. The other three need a result
type, a signature, or a restructured loop to change with them, so they offer
notes and a hint instead - the same disposition SEM3198 already took.

No golden fixture captures fixes: the corpus records `.diag`, so a fix that
stopped being offered would not move a single byte. It is pinned by a unit test
instead, which asserts the edit DELETES and covers exactly the five bytes of
`&mut ` - and it was shown failing with the suggestion removed.

### `make check` failed once here for a reason no commit could fix

The docs commit's pre-commit hook failed with `internal/vm` timing out at
90.012s against the target's 90s package budget - `TestVMEntrypointStdinInt` was
merely the test holding the clock when the alarm went off, not the cause.

Measured rather than assumed: the package runs in **78.99s at HEAD and 78.58s at
`92d38194`**, so the six fixtures added here cost 0.4s and the budget has about
eleven seconds of headroom on a quiet machine. The failing run happened while the
MT lane's tail and a second build tree were still on the CPU.

This makes the PRE-COMMIT HOOK load-sensitive, which is a sharper statement than
the one `213` already records about `runtime-v2-http-owner-check`: a commit can
be refused for what else is running. Two earlier `make check` runs in this same
session reported 89.833s and 83.228s, so the margin has been thin for a while and
nothing had said so. Worth a row of its own if it recurs; recorded here first
because a single occurrence with a known cause is not yet a defect.

## 2026-08-14 — RV2-DEBT-218 dated, and the measuring instrument was changing the answer

The row asked to be DATED before being answered, on the reasoning that the shape
was allowed by 206 three days earlier but the invalid read might be much older.
It is much older.

**Introduced at `0b677dad`, 2026-07-14** — `feat(compiler): statement-end
temporaries free their owned values` — a month before the row was filed. Bisected
over the 397 commits from `2c8e62e5` with a valgrind probe, then the boundary was
re-tested directly instead of trusted: the parent `9259f22c` answers 55 and is
clean, `0b677dad` reports the invalid read and segfaults. Both endpoints were
validated first, including that the program at the "good" end actually RUNS and
returns 55 rather than merely failing to report an error.

### The correction, which is the part worth carrying

The row says the escaping slice "prints the CORRECT answer 55". It does — **under
valgrind**, which keeps the freed block mapped and intact so the read succeeds.
Run bare at `b5199277`, three consecutive runs gave
`total=-4273895566234763744`, `Segmentation fault` (139), and
`total=86554757626546400`.

So 218 is not a quiet cousin of 207 and 214, it is the same shape: a
nondeterministic garbage answer that sometimes crashes.

**That invalidates the stated reason 207 turned registration down.** 207's row
argued that registering the cursor would only buy "a right answer over a silent
read of freed storage", citing the slice — the form that IS registered — as
evidence. The slice does not produce a right answer. The owner's ruling to refuse
stands and is better supported than the row's reasoning claimed, but the reasoning
itself was built on an artefact.

**The general lesson: valgrind is not a passive observer of a use-after-free.**
It changes whether the freed read succeeds. Any claim of the form "it prints the
right answer AND valgrind reports an invalid read" needs the bare run before it
is believed — the two halves were measured under different conditions and only
one of them is how the program actually behaves.

## 2026-08-14 — RV2-DEBT-215/216 closed, and the mandated experiment answered a different question

### The experiment the owner insisted on, and what it actually found

The ruling was: settle the BIG-bounds question with one valgrind experiment
BEFORE shipping any bound release, and do NOT ship release-without-bounds on the
argument that it is already strict zero for fixnum-bounded programs. Run in all
three parts (let-bound, for-in'd, moved into a slice sink), with bounds at 2^62
so they fall outside the fixnum inline range:

- the bound boxes are reported **INDIRECTLY lost through the range** — 256 bytes
  in 16 blocks over eight iterations, on top of the range's own 192. Indirect is
  valgrind saying the range holds the last pointer to them;
- the double-release the row feared is NOT the blocker. `let c = r` and a
  by-value parameter pass produce exactly ONE heap range (24 direct bytes in 1
  block), so a bound release would fire once;
- **the blocker is that bound release is not expressible.** A heap bignum int has
  no exported lifecycle: `bi_free` is a `static inline` in
  `rt_bignum_internal.h`, undeclared in `rt.h`, and `IsRefCountedScalar` answers
  true only for `float`. There is nothing `rt_range_free` could legally call.

So the answer is the same shipping decision the owner refused to take on the
old argument, reached by a different and much stronger one: it is RV2-DEBT-035's
residual, and 035 says so in its own words. The bound bytes move from
INDIRECTLY to DEFINITELY lost — the same bytes reclassified, not a new leak.

The experiment also produced a lane fact nothing had recorded: **the VM DOES
release bounds** (`internal/vm/heap.go:198-209` releases `Range.Start`/`End` for
a bounds range and `Range.ArrayBase` for a cursor), so this is a lane
disagreement and not only a leak. The native cursor holds a raw element-data
pointer it never retained, so there is nothing there for it to release.

### The rows undercounted the shapes, and enumerating KINDS is what found the rest

215 named two shapes and 216 named one. Enumerating kinds of Range consumption —
literal into a slice, bound range into a slice, bound range for-in'd, cursor
for-in'd, by-value parameter, string slice in both spellings, fixed-array slice,
returned range, unused range, struct member — found two more that leaked:

- **a Range held by a STRUCT MEMBER.** `typeOwnsHeapRec` answered "owns no heap"
  for a struct whose only heap member was a range, so it got no drop glue at all.
  No drop instruction reaches a member; only generated glue does.
- **the by-value `Range<T>` parameter is live in the stdlib**, not theoretical:
  `core/array.sg:78 pub fn from_range(r: Range<T>)`.

The same enumeration turned up a leak that was not a Range at all: a sliced
STRING's result is never freed, 76 bytes in 4 blocks, pre-existing at
`92d38194`. Filed as RV2-DEBT-219, and it is the same shape RV2-DEBT-206 closed
for arrays — a slice MINTS a value and the binding holding it owns it — which
nobody had looked for in strings.

### The fix went where the rows did not expect, twice

**216's budget trap never had to be paid.** The row warned that `rt_string.c` is
over the size limit so any added line trips LEGACY_GROWTH, and offered freeing at
the LLVM call sites *for the string sink only*. Taken for ALL FOUR sites instead
— there are four, not the three the row named; `emit_calls_intrinsics.go` carries
a second string-slice site — and it is better everywhere: not one line added to
`rt_array.c` or `rt_string.c`, the sinks keep reading a `const SurgeRange*`, and
ownership stays where the decision to move the range was made. It is also what
the VM does, so the lanes now agree by construction.

**215's `rt_range_free` was written in C after all, and the reason is the glue.**
The first implementation sized the block in LLVM, reusing `emitRangeObjectSize`'s
kind-byte select, and it worked for the drop arm and the sinks. It could not
serve the drop GLUE: the glue emitter writes straight-line calls with no block of
its own, so it cannot emit a null guard. One C function with the sizing inside it
serves all four sites and leaves simpler IR everywhere.

That put the cursor's 40-byte layout on the C side for the first time, as
`SurgeRangeArrayIter` with four `_Static_assert`s against the emitter's
constants. C already depended on that layout — the slice helpers read `kind`,
`has_start` and `has_end` out of a cursor — but only Go described it.

### Method notes

- **A move is what makes freeing at a call site safe, and the language says so.**
  `xs[h.r]` is refused with "taking `h.r` out of `h` empties it, so write
  `own h.r`", so no caller still owns a range a sink was handed. Verified rather
  than assumed, and it is the whole safety argument for the 216 design.
- **The subagent maps raced the edits and their "refutations" were staleness.**
  Three verifiers reported the tree contradicting the map; two of the three were
  reading a working tree the lead was editing between the map and the verify.
  Their genuine findings were the ones about files nobody was touching. A
  read-only map of a tree under active edit needs a pinned commit, not a branch.

One correction made to this work while writing it down: the first version of the
`rt.h` comment claimed the `_Static_assert`s "fail loudly if the emitter's
constants and this struct ever drift apart". They cannot — a C compiler cannot
see a Go constant, so they catch drift on the C side only. The other side is now
pinned by `internal/backend/llvm/range_layout_test.go`, holding the emitter's
four constants against the same numbers. Both halves are needed because a
mismatch is not a compile error: `rt_alloc`/`rt_free` reconcile the size they are
TOLD rather than measuring the block, so drift is silent heap-accounting
corruption.

### A gate I did not run at `b5199277` had been red ever since

`runtime-v2-place-overwrite-check` went red with the REFUSALS commit and nobody
noticed for two commits, because that commit was validated with `make check`,
`golden-check` and both behavioural lanes but NOT with the runtime-v2 gate suite.
`TestRuntimeV2LoopBindingOverwriteIsNotAnInvalidFree` compiles a program that
assigns to a for-in binding — SEM3201 refuses it now, so the build fails and the
test fails with it.

That is the THIRD test that pinned RV2-DEBT-211 and had to change meaning; the
other two were unit tests found by `make check` and converted in the same commit.
This one was invisible to every gate that commit ran. **The baseline exists to be
re-run, not merely to be taken:** all 26 targets were enumerated on 2026-08-13
and I compared against the subset I happened to run.

The same sweep caught a second expectation that legitimately changed:
`runtime_v2_drop_scope_exit_e2e_test.go`'s "view-order window" asserted five
frees and now sees six, because the slice call in it materialises a Range that
is released at the sink. Adapted with the reason written next to the number.

Converting the e2e was worth more than deleting it. `compileRuntimeV2SourceForDiagnostics`
is new in `runtime_v2_crossing_source_build_test.go` because the existing harness
fatals on a build failure — right for every test that needs a binary, useless for
a test whose subject is a refusal. A rule that makes a shape unexpressible
retires the run-time witness that guarded it, and the witness should assert the
refusal rather than disappear: the guards in `recordReassignOldDrop` and
`reinitializeAssignedPlace` still exist, and what changed is that no program can
reach them. The count is asserted, not just the presence — a refusal firing once
for a program with two of the same mistake would leave the second one live.

## 2026-08-14 — RV2-DEBT-210 closed, and the row had the gate in the wrong file

Compound assignment now frees what it displaces, at every target that owns heap.

### What the row got wrong, and why it did not matter to its conclusion

The row said the whole-binding form was gated in sema. It was not: `handleAssignment`'s
whole-binding branch has no operator test, so `ReassignOldDrops` already held it.
The suppressing gate was in HIR (`internal/hir/lower_expr.go:347`), and the sema
gate (`assign_place_reinit.go:41`) covered only the PLACE form. The row's
conclusion — that removing a gate changes nothing because the lowering never
consulted the flag — is what made both gates harmless, and it survives the
correction unchanged.

### The drop's POSITION is the whole fix

Between the binary operation and the store. Not before: the compound form reads
its target first, and for a string the backend hands the callee the ADDRESS OF
THE SLOT rather than a loaded handle, so an early drop is a use-after-free the
runtime performs on itself. `materializeBeforeOverwrite`, which the plain `=`
needs, buys nothing here — the store's source is a fresh temp and can never be
the destination, which is the only thing that function prevents.

`u += u` is in the fixture for exactly that reason. It is the compound analogue
of `x = x`, the program that forced `materializeBeforeOverwrite` onto `=`, and a
fixture is cheaper than trusting the sentence above.

### The census was short by one shape and long by another

`xs += ys` is a FIFTH path with its own array-concat branch in the backend, and
it leaked one array header plus its data per operation. It was found by an
adversarial verifier reading the map, not by the map. And `xs[0] += "x"` does
not exist: the compiler refuses it with "operator += changes type from &string
to string", so the element target the row worried about was never reachable.

SEM3201 had already taken two shapes off this row's surface between filing and
fixing — a compound store to a for-in binding is refused before any bookkeeping.

### The same wall, twice in one day

A compound assignment whose target is a heap bignum `int`/`uint` still leaks, and
for the same reason a Range cannot release its bounds: `emitInstrDrop` has no arm
for a big int and the runtime exports no lifecycle for one. Two unrelated fixes
reached RV2-DEBT-035 from different directions in a single session, which is a
better argument for taking that row than either would have been alone.

The residual was measured on both sides rather than argued: `b += <2^62>` eight
times reports `358 allocs, 341 frees, 296 bytes in 17 blocks` IDENTICALLY at
`92d38194` and after the fix, with no invalid access at all — the three "errors"
valgrind counts there are the three leak records, not memory faults.

## 2026-08-14 — RV2-DEBT-219 closed on the day it was filed, and 206's asymmetry did not hold

The string-slice result leak is the same defect RV2-DEBT-206 closed for arrays,
in the same predicate, and it had never been looked for in strings:
`projectionReadAliasesItsSource` knew that an ARRAY slice mints a value, so a
string slice fell through to "the container still owns it".

**Copying 206's fix shape would have left half of it live.** For an array, the
two sites that ask this question DISAGREED - `temp_drops.go` said a slice mints,
sema said it aliases - so only the bound form leaked and the temporary form was
already correct. For a STRING both sites asked the same `isArrayViewExpr` and
both were wrong, so `s[[3..5]].__len()` leaked as well. The asymmetry was a fact
about one defect, not a property of the machinery.

`mintsOwnedValue` now answers for both sites, which is what stops that class of
disagreement rather than fixing this instance of it. `isStringSliceExpr` factors
out a shape test `observeMove` already carried inline.

### Attribution beat aggregation

The first measurement put three forms in one program and reported 8 leaked
blocks for 12 slice operations. That number alone says "something leaks"; what
made it actionable was that 8 of 12 is TWO of the three forms, which named the
callee-owned temporary as already-correct (its parameter owns the value) and
sent the fix to the other two. A single-form program would have been green or
red without saying which site to touch.

### The census is about double frees, not leaks

Nine kinds, and the ones that earn their place are the ones that could free
TWICE now that the binding owns something: bound-then-moved, bound-then-borrowed,
slice of a slice, returned from a function that borrowed its source, and
reassigned over. A leak shows up in the numbers; a double free shows up as a
memcheck error, which the gate checks first. An element read is in there for the
opposite reason - it mints nothing, and a drop recorded for it would free a
character the string still owns.

### The pre-commit hook was failing on load, and it was not the commits' fault

`make check` died three times on 2026-08-14 at exactly `90.012s`, on three
unrelated commits, always while something else was running on the machine. The
test named in each panic was whichever one held the clock when the alarm went
off — `TestVMEntrypointStdinInt` once, `TestVMCallDefaultUintCast` another time —
which reads like a flaky test and is not one.

Measured rather than assumed: `internal/vm` runs in **79.97s at HEAD and 79.33s
at `92d38194`**, so the three reclamation e2e tests added today cost 0.6s
together. They run alongside the rest of the package, not after it. The budget
was already ten seconds from the edge before any of this work.

Raised to 300s, filed as RV2-DEBT-220, and the distinction is worth stating
because this repository is otherwise strict about not moving numbers to make red
go green: **that timeout measures nothing about the code.** It is a hang detector.
A hung test still hangs and is still caught, 210 seconds later. The alternative
was gating the reclamation e2e tests out of `make check`, which would have taken
valgrind witnesses out of the pre-commit hook to protect a number that asserts
nothing.

## 2026-08-14 — RV2-DEBT-200 closed: make can say no again

`make` answered 0 to every unknown goal, so a gate that did not exist was
indistinguishable from a gate that passed. The catch-all pattern rule at the end
of the Makefile exists so `make run prog.sg` can pass trailing words to the
program instead of make trying to build each of them; unconditional, it swallowed
every typo and every absent target too. It is how
`runtime-v2-carrier-sanitizer-check` stayed "green" in three documents while
having no target at all.

Defining it only when `run` is the FIRST goal fixes it completely rather than
heuristically, because `run` is the only goal in the file that reads
`MAKECMDGOALS`. Everything else gets its errors back.

The comparison that matters is not "unknown targets now fail" but "`make run`
behaves exactly as before": `make run version` prints the same line with the same
exit code against both Makefiles, checked side by side rather than asserted.

The pinning test sits next to `documented_make_targets_test.go` deliberately.
That one asserts a target a document PROMISES exists; this one asserts make can
still say NO. A promise checker is worth very little if the thing it checks
against answers yes to everything — which is precisely the state this repository
was in while both halves looked healthy.

One caveat, unchanged and now written down: make parses a leading `--flag` after
`run` as one of its own options and prints its help. That predates this row.

### Closing 200 broke the probe that was built to catch 200's class

`HasExplicitMakeTarget` dumps make's database by asking for a goal named `:`
that deliberately does not exist, and treated a non-zero exit as failure. That
worked only because the unconditional catch-all made `:` succeed. Narrowing the
rule — the whole point of the row — made the probe error on the repository's own
Makefile.

It had been reading the exit code of a question it was not asking. make prints
the entire database to stdout BEFORE failing on the goal, so the answer was
always there; the exit code describes the goal, not the query.

Pinning that turned up a second false answer in the same function. A MISSING
makefile also produces a structurally complete database — an empty `# Files`
section, with the error only on stderr — so the probe would have answered "no
such target" for every target of a file it never read. That is a false NEGATIVE
in a gate built to catch false positives, which is the worse direction. The
makefile is stat'd before make is asked now.

Worth generalising: a tool that reports "absent" and a tool that reports "I could
not look" must not share an answer, and an exit code is usually reporting a third
thing entirely.

## 2026-08-14 — the owner's question found a bigger defect than the one being discussed

Asked to rule on RV2-DEBT-218, the owner asked what a refusal would COST: if an
escaping slice is refused, can you clone the window and return the copy? Checking
that answer measured the escaping slice one way it had never been measured — with
its result BOUND before iterating — and it came back clean.

**RV2-DEBT-218's invalid read was never about the slice.** Its reproducer
consumed the result as `for it in build_slice()`, and THAT form is broken for an
ordinary array with no slice anywhere in it:

    fn build() -> int[] { let mut p: int[] = []; p.push(22); p.push(33); return p; }
    for v in build() { ... }        // segfault
    let got = build(); for v in got { ... }   // 55, valgrind-clean

Filed as RV2-DEBT-221 and 218 corrected in place. With five elements the loop
reports the right COUNT and the last three values while the first two come back
as garbage, so the storage is freed before the loop reads it and the allocator
has already reused the head of it.

**RV2-DEBT-207 does NOT dissolve the same way, and that contrast is what makes
the correction safe to act on.** An escaping Range cursor segfaults both bound and
unbound, so the refusal shipped this morning stands on its own evidence. Had it
gone the other way I would have shipped a rule against a shape that was never
broken.

The neighbouring shapes are already decided and only one is wrong:
`build().__len()` is REFUSED ("cannot take reference to temporary value; bind it
to a variable first"), `consume(build())` is correct because the callee owns what
it was handed, and only `for-in` both accepts the temporary and fails to keep it
alive.

Invisible for the usual reason: `for x in <call>` appears exactly three times in
the entire corpus, and all three are fixtures written today.

### The lesson, and it is not a new one here

Every measurement of 218 went through a form that is broken independently, and
the row named the half that was easier to see. **Ask what ANSWERED the question,
not what asked it**: the reproducer had two candidate causes in it and only one
was tested by varying it. Varying the OTHER one — bind versus don't bind — took
two minutes and reversed the conclusion.

It also says something about where these get found. This one surfaced because
somebody asked what a decision would cost, not because a gate went red.

## 2026-08-14 — RV2-DEBT-221 closed: a for-in now keeps its iterable alive

The suspect named when the row was filed was right about the file and wrong
about the mechanism. It is not a per-iteration flush. `normalizeIterFor` turns
`for v in <expr>` into `let __iter = iter_init(<expr>)` followed by a `while`,
and the statement-end machinery frees the iterable at the end of THAT LET — one
statement before the loop that reads through the cursor it just made. MIR said it
in three lines of the entry block:

    L2 = call build()
    L4 = iter_init copy L3
    drop L3

### The fix is the spelling that already worked

Binding the value first is what an author writes by hand when the direct form
misbehaves, and it is what measures clean. So the normalization does it:

    let __src  = build()
    let __iter = iter_init(__src)
    while true { ... }
    release_cursor __iter
    drop __src

That puts the iterable's release exactly where the CURSOR's release already was
— after the loop, and before every return that escapes it — so the change reuses
`injectIterCursorReleaseBeforeReturns` instead of inventing a second placement.
The cursor is released first, because it points into the value; freeing storage
before the thing pointing at it is the very ordering error being fixed.

Only an unconditional whole-value temporary is hoisted. A residual plan (fields
already taken out of it) or a guarded one (a choice expression owning on some
paths and forwarding a place on others) describes a release a plain drop would
get wrong, and a wrong free is worse than the leak that not hoisting leaves —
the direction `iterCursorReleaseIsSafe` next door already errs in.

### What the census was for

Nine kinds of iterable, and the entry that earns its place is a PLAIN BINDING
iterated TWICE. The fix binds the iterable to a synthetic name and drops it, so
the question it must answer is not "does the defect go away" but "does it now
free something the program still owns". If the loop ever took ownership of `xs`,
the second pass would read freed storage.

Before: 11 memcheck errors in 4 contexts, and either a segfault or the answer
`11299214484580899258`. After: `143`, 50 allocs and 50 frees, zero errors. The
allocation counts are identical on both sides, which is what says this was never
a leak and always a use-after-free.

### Regenerating the whole golden corpus moved zero fixtures

The hoist fires only for an owned temporary, and the corpus contains none — the
same census that said `for x in <call>` appears three times in the tree and all
three are fixtures written today. Narrow by construction, and the e2e test is
the only thing covering the new path.

### And closing 221 closed 218 outright

RV2-DEBT-218 had no defect of its own. Every symptom it recorded — the invalid
read, the segfault, the garbage answer — belonged to the for-in temporary, and
fixing that fixed this. Re-measured on the row's own shape and on a wider census
(both consumption forms, int elements, struct elements carrying heap strings, and
a slice of a slice): 194 allocs, 194 frees, zero memcheck errors, the same answer
on three consecutive bare runs.

**No language capability had to be removed.** The decision the row was holding —
refuse an escaping dynamic-array view, as one predicate with RV2-DEBT-207 — is
moot: returning a view over a locally built dynamic array stays legal and is now
actually safe. Had the ruling been taken when it was asked for, a working shape
would have been refused forever on the evidence of a different bug.

That is the whole argument for [[rederiving-means-varying-one-condition]] in one
sentence, and it is worth noticing that the thing which prevented it was not a
gate, a review or a test. It was the owner asking what the decision would cost.

### The array-copy gap, filed and parked

Found while checking what a refusal of RV2-DEBT-218 would have cost the author:
an array whose element type is neither `@copy` nor `__clone`-bearing cannot be
copied at all. `.__clone()` and `clone()` both refuse it, and the manual route
refuses one level down — `push` takes ownership, an element read is a borrow.
`Array::extend` has the same floor, since its body is `self.push(clone(other[i]))`.

Filed as RV2-DEBT-222 and parked by the owner the same day: it is long design
work about `__clone` on generic containers.

It blocks nothing in the epic, and that was checked rather than assumed. Wave D
steps D1–D8 migrate storage OWNERS — element buffers, map entries, channel
mailboxes, task results, select staging, async frames — and none of them copies
an array. The whole corpus contains two `.extend` calls, both in golden fixtures
with copyable elements. It limits what an author can write, not what the epic can
finish, and closing 218 removed the urgency it was found under: returning a view
over a locally built dynamic array is legal and safe, so copying is no longer the
only way out.

## 2026-08-20 — why `plan_cross` is a mandatory slot, and why nobody can say

RV2-DEBT-232 asked whether `rt_slot_operations_preflight` demanding a non-null
`plan_cross` from a purely local owner is deliberate or an over-strict artefact.
The answer is neither of those, and the row's own hypothesis — that `plan_cross`
is read-only and total, hence a universal preflight every type can answer — is
refuted rather than merely unproven. The finding is recorded here because it is
history, not contract; the normative rule it produced lives beside `ValueOps<T>`
in `23-storage-model-and-typed-carrier-abi.md`.

**The declaration is authored and older than the check.** The manifest marks
`plan_cross` `"nullable": false` with "Always-present read-only crossing
preflight", wording it shares only with `move_init`, and the field table is
byte-identical at the freeze commit `0db416cc` (2026-08-04) — a day before
`rt_slot_control.c` existed (`dbf536f3`) and before the check landed
(`8512f4c1`). Nullability is not a default: `internal/abimanifest/validate.go`
refuses a callback type that omits it. Someone typed `false`.

**The rule behind the field table is mechanical.** Six capability bits each NAME
one slot — `_COPY`→copy_init, `_CLONABLE`→clone_init, `_DROPPABLE`→drop_in_place,
`_TRACEABLE`→trace, `_SHARD_MOVABLE`→cross_move_init, `_CROSS_CLONABLE`→
cross_clone_init — exactly one-to-one with the six nullable callbacks.
`move_init` and `plan_cross` are non-null for one reason: no bit names them.
The only recorded stretch of reasoning behaves precisely as that rule predicts:
`NOTES.md:6410-6412` argues mandatory move, argues flag-gated drop, and is
silent on `plan_cross`.

**What refutes the "universal preflight" reading.** `rt_value_plan_cross_fn`
takes `mode: rt_cross_mode`, whose only two values name apply callbacks that
exist solely under the cross flags. A type with neither flag has no legal
`mode`: the function is not total there, it is vacuous. And no
`rt_carrier_status` value means "this type cannot cross".

**"The check mirrors the manifest" overstates the manifest's reach.** `nullable`
on a callback field is machine-inert everywhere else — the C renderer never
reads it, the generated header contains zero `nonnull`, and the checks header
has 154 `_Static_assert`s and no NULL test. Nine fields are `nullable: false`
and exactly two are null-checked anywhere in the tree, both inside this one
preflight; `rt_key_ops.hash`/`equal` are not, in a record that EMBEDS
`rt_value_ops`. Enforcement was its own act, and it converted two of nine.

**The cost premise that came with the row was wrong.** The feared multiplier on
Wave B is 1, not N: every descriptor Wave B can emit today is non-crossing by
construction, because `Entry.checkSlots` refuses any entry setting a staged bit
and ShardMovable/CrossClonable/Droppable/Traceable are all staged. So all of
them want the same stub, and the compiler already ships the mechanism for one
symbol filling a slot across every descriptor (`slotRule.runtimeSymbol` /
`RuntimeFilledSlot`, instantiated as `rt_value_copy_init_unbound_trap`).

**The structural defect this uncovered is worth more than the question.**
`slotRules` is indexed by flag, which encodes "a slot exists iff a capability
bit names it" — and that is why BOTH unconditional slots fell out of the
registry: `valueops.Entry` has no field for `move_init` either. The model owed
is applicability — `unconditional | capability(flag)` — after which both
mandatory slots appear from one structural edit, with `move_init` per-type
generated and `plan_cross` bound to the shared trap.

**Citation drift corrected here:** the unconditional pair is
`rt_slot_control.c:42`; `:44` is the middle of the COPY biconditional and had
been cited wrongly in four documents.

**The manifest text was corrected, and it moved the ABI identity.** Owner ruling
2026-08-20: the price is worth paying, because the sentence that produced the
wrong reading was in the machine contract itself. `plan_cross` now reads
"Mandatory slot: always present, unlike the capability-gated callbacks. Only
STRUCTURALLY unconditional, though - its callable domain is decided by the cross
capability flags…", contrasting `move_init` explicitly. Because `CanonicalBytes`
marshals the whole manifest including every `semantics` string, the hash and the
mandatory link sentinel moved with it:
`f30fcfb03b62d105dab0cd21d57a3dcc029b0e7ae10a337e966079033608650a` ->
`9653d77321b7a2d50226ef27abbb65c9a5061703d81239eb4d1e966909ce4562`. Six
generated views were rewritten. **Objects built before this commit will not link
against a runtime built after it** - the same consequence D0 had when it last
moved this hash, and the reason the earlier D0 note above still names the old
value: that note records what was true then, not now.

## 2026-08-24 — the typed channel's FIFO admission, and four defects it took

The typed-storage flip (branch `wip/d3-typed-channel-storage`) left one gate red
and, when that was chased, three more defects behind it. All four are the same
shape: a decision that was correct while the buffer was a plain word array, and
is not correct now that a transfer through it takes two steps.

**Measured before, at `0a3c1882`:** `make runtime-v2-lock-check` exit 2,
`TestRuntimeV2LockSplitCrossShardChannelFifoAndClose/shards-3` failing 4 of 4
runs with `channel FIFO order violated`. The received sequence, dumped from an
instrumented harness, was `… 101 106 102 103 104 105 107 …`: exactly one value
overtaking a full buffer of four. `make runtime-v2-heap-check` (exit 0, 191 s)
and `make runtime-v2-transport-check` (exit 0, 124 s) were GREEN on that same
tree — the WIP commit message lists them as red, and re-measurement on a clean
tree says otherwise.

**1. A refusal is not an emptiness.** `rt_typed_fifo` allows ONE transfer at a
time, so `reserve_pop` answers BUSY while another task is mid-move. Both the
receiver's park decision and the sender's buffer decision read that refusal as
"nothing here" / "no room". A receiver therefore parked with four values in the
buffer, and the next sender — which looks for a parked receiver BEFORE it looks
at the buffer — handed it a newer value directly. Fix: BUSY is answered by
yielding and looking again, never by parking (a buffered send that fits must not
block); and a rendezvous is admitted only while `rt_typed_fifo_nothing_queued_locked`
holds, which is empty AND quiet, because a value in flight already has its place
in the queue.

**2. A push into the buffer must wake a parked receiver.** Once a value can go
into a cell rather than into a receiver's resume slot, nothing consumes that
receiver's registration, and no other path will call on it.

**3. A re-entering send re-read storage it had already moved out of.** The park
path knew to keep its staged slot; the paths BEFORE it did not, so a sender woken
to retry staged `src` a second time. For an owned element that is one heap value
with two owners. Fix: the staged slot is the value's only home from the moment it
exists, and every path sends from it.

**4. A popped candidate must be delivered to or woken, never dropped.** Three
places popped a peer's registration and then took a branch that did neither: the
receiver's refill when the buffer refused the transfer, and both slot-shortage
exits of the try-send core. The pop CONSUMES the registration, so the peer sleeps
until the process ends. Found from a per-task event ring: `prepare(1) park(1)
committed(1) popped(1)` and then nothing at all.

**And an ack is not a wake.** A sender parked holding its own value — the pool
was full when it parked — was woken with `RESUME_CHAN_SEND_ACK`, which its next
poll reads as "your send completed". The value was never delivered and never
sent again. Now such a sender is woken unacked.

**Measured after:** lock gate exit 0; the FIFO row 20/20 green at both shard
counts. The new row `TestRuntimeV2ChannelOwnedElementArrivesExactlyOnce` — ten
senders against eight park slots, an element whose move empties its source and
whose drop is counted — is 24/24 green with `received=200 bad=0 drops=0
missing=0 duplicated=0 closed=1`, and on the unfixed tree it reports
`received=180 … missing=20` and `stuck sender=2 status=2`: five of six runs red.

## 2026-08-24 (later) — what the gates could not see, and the row that now does

Three more defects, none of them visible to any gate that was green. The tagged
`internal/vm` suite found two and ThreadSanitizer found the third, which is the
whole argument for running both against a representation change rather than
trusting `make check` plus the behaviour lanes.

**A move ran with control held.** `rt_channel_finish_take_owner_locked` refills
the buffer from a parked sender, and the refill MOVES that sender's value. The
core is also reached from the control-lane wrappers, where releasing the channel
shard does not release control, so the fail-closed helper aborted the process:
`rt_value_move_init_detached was dispatched while a runtime lock is held`. Where
control is held the sender is now woken to place its own value. Measured:
`TestMTBufferedBlockingRecvRefillWakesSender` exit=-1 → ok 3/3.

**A composite element's receive slot took its alignment from its spelling.**
`[24 x i8]` says how many bytes and never how they must be placed, and
`emitChannelPayloadSlot` asked `emitAlloca`, which refuses a byte run for
exactly that reason. Three MT rows could not COMPILE. The slot now reads the
alignment from the layout registry.

**The park pool wrote its free list without the lock.** `rt_park_pool_release`
runs the element's drop, so callers released the owner lock around it — and it
also unlinked the slot, decremented `live` and rewrote `first_free`, which every
other entry point touches under that lock. TSan named it on the first run:
`rt_park_pool_return_slot` racing `rt_park_pool_acquire_locked`. The release is
now begin/finish, the split the typed FIFO already used, and
`channel_end_park_locked` spells the cycle once for all seventeen call sites.
Measured: 3 data races → 0, and the TSan rows fell from 91 s each to 1 s.

**The instrument that found the third one is now a gate row.** The owned-element
stand carries an element that OWNS a heap block — its move empties the source,
its drop frees the block and is counted, the receiver frees what it was handed —
and it runs plain, under ASan+UBSan, and under TSan, wired into
`runtime-v2-carrier-sanitizer-check`. Two rows were added beside the buffered
one: an unbuffered channel, and a sender cancelled while its value is staged.

That cancellation row is the instrument RV2-DEBT-245 asked for by name. It reads
a staged value's fate from inside the program (`drops=1 received=0`) rather than
from a leak summary whose noise exceeded the payload, and it reaches the drain
inside `rt_channel_free`, which no compiled program calls. What it does NOT do
is cover the row's seven element classes; the debt stays open for that.

**A first draft of the release split put the claim token IN the pool**, which
grew every channel by 96 bytes and moved the RV2-DEBT-062 leak baseline from
1344 to 1536. Running the whole claim/commit cycle under the lock — the callback
is the only part that may not be — removed the need for it: the pool is 208
bytes again, and the baseline is 1344 again. The lesson is small and general: a
split protocol does not have to carry state across its halves if the half that
must be exclusive can finish while the lock is still held.

**One flake worth naming:** `TestRuntimeV2SelectReleasesAStringPayloadExactlyOnce`
failed once with `-0.25 allocations per take (4 takes left 81 outstanding, 8
takes left 80)` and then passed 4 of 4 on re-run. The census reads one allocation
FEWER with more takes, which is noise rather than a leak, but the row asserts an
exact slope and will keep tripping on it.

## 2026-08-24 — D4 opened: what the task's result is today, and what it becomes

Measured before any edit, at `9095014f`.

**What carries a result now.** `rt_task` holds `uint64_t result_bits`, a
numeric `result_drop_fn_id`, and an `rt_result_copy_fn result_copy_fn` that a
cloned handle installs. A composite result is BOXED: the emitter writes a
pointer into the word (`emitI64ToValue` on the await path,
`internal/backend/llvm/emit_async.go:284`), the numeric id names its drop, and a
second asker is served by copying the box. That is the same word-shaped ABI D3
removed from the channel, in the shape the task wears it.

**Surface.** `mark_done` has 6 call sites; `rt_async_return` is called from 20
files including every C stand; `rt_task_await` / `rt_task_poll` / `rt_task_clone`
are declared in `rt.h`, so the emitter and every stand move with them. The
remote half (`rt_remote_task_pending.c`, 9 mentions) carries its own copy of the
word and belongs to Wave E, but it reads the same fields.

**The design is already written**: `23-storage-model-and-typed-carrier-abi.md`
§10 — one canonical exact-sized result slot with its `ValueOps<T>`, an
entitlement per handle, a reserved final move waiter, clone readers, and a
generation. It is larger than one commit can carry.

**Split, and why it does not need an adapter.** §5 forbids an adapter milestone
INSIDE an owner migration, so the storage flip lands whole:

- **D4a** — the canonical typed result slot. `result_bits`,
  `result_drop_fn_id` and `result_copy_fn` are deleted; the task carries
  `const rt_value_ops*` plus storage it owns, and every entry point takes an
  address. Extra askers are served by the descriptor's `clone_init`/`copy_init`
  instead of by an installed function pointer. Today's OBSERVABLE semantics are
  unchanged: each asker still receives an independent value.
- **D4b** — the entitlement state machine §10 describes: reserved move waiter,
  `claimed_clone_readers`, generations, and the exactly-`E-1` duplication
  contract. That is a policy refinement over a storage shape that already
  exists, not an adapter over one that does not.

**The cost question D4a has to answer, because today's word is free for a
scalar.** `Task<int>` stores its result in the word with no allocation, while a
composite pays a box. An exact-sized slot that always allocates would make the
cheap case worse. The answer is the one the storage model already gives for
inline storage: the slot is sized by the descriptor, and small results live in a
byte run inside `rt_task` rather than in an allocation of their own.

## 2026-08-25 — D4a stopped at a fork the epic's own division does not settle

The local half of the flip is straightforward and was written: the task's result
fields become one typed slot, `rt_async_return(state, src)` moves the value into
it from inside the task's own poll (the one place no runtime lock is held),
`mark_done` stops carrying bits, and `poll_outcome.value_bits` disappears. Seven
runtime files and twenty-two compile errors away from done.

**What it runs into.** A far body and a local body are THE SAME compiled
function. `spawn f()` and `spawn on placement f()` both reach
`emitTermAsyncReturn`, so the emitter cannot box for one and not the other.
Today it boxes for both -- `emitValueToI64` allocates whenever
`hasInlineStorage(type)` -- and the far reply ships that box pointer as its
result word, which the awaiting side adopts with the mirrored `emitI64ToValue`.

So if the emitter stops boxing, the far reply has to box instead, and it has to
box on EXACTLY the emitter's predicate. `size > 8` is not that predicate: a
two-field composite of eight bytes is inline-storage to the emitter and a word
to that rule, and the two sides would disagree in the direction that reads a
pointer out of a value.

**Three ways out, and none is a detail.**

1. Give the descriptor the fact. `rt_value_ops` would say whether a value of
   this type is carried boxed when it must fit a word, and the far reply reads
   it. It is one field in a FROZEN manifest: the hash moves, the link sentinel
   moves with it, and objects built before it will not link against a runtime
   built after -- the consequence D0 already paid once.
2. Type the far await surface now. The transport is in-process, so the value
   never has to become a word at all: the reply names the producer whose slot
   holds it, `rt_far_task_await` takes a destination like `rt_task_await`, and
   the lease that already governs adoption keeps the producer alive. No box
   anywhere, and the local and far paths end up the same shape. It absorbs a
   slice of what section 6 assigns to P4/Wave E.
3. Leave the box where it is and type only what is left. The await destination
   would still be adopting a pointer the runtime allocated, which is the
   representation this step exists to remove, so this buys nothing.

**Nothing was left half-flipped.** The edits were reverted; what stands is the
slot machinery (`a28c5520`), which nothing reads yet. A representation with two
disagreeing readers is the failure this epic has already paid for once, and
starting the flip without settling the fork would build exactly that.

## 2026-08-25 — D4a landed: the result is stored at its own type, near and far

The fork above was settled by the owner with option 2, and with a constraint
that shaped the whole far half: **the reply carries a capability, never an
`rt_task*`**. A pointer cannot say "the task I name completed, was freed, and
its id was reused"; four integers can — task id, task generation, result
generation, owner shard — and every one of them is checked before the value is
touched.

**What the far path does now.** The producer's result slot is PINNED when the
reply is minted, and stays pinned until the terminal outcome. The awaiting side
hands its destination in only on the retry call that will finish: while parked
it holds the capability and the pin alone, because a park or a longjmp would
leave a stored address dangling. On the terminal call the runtime validates the
four fields, claims the producer's slot, moves the value straight into the live
destination and commits. Cancel, a stale reply and an enqueue failure all leave
the source slot its owner, whose ordinary exactly-once cleanup then runs.

Today's far semantics are unchanged: one lease, one move. Several awaiters and
clone entitlements stay in D4b and do not leak into this step.

**Serving is decided by what the value can do**, in one statement that holds for
a near handle and a far one alike:

| the value | what an asker gets | what the slot keeps |
| --- | --- | --- |
| a second asker exists, and a duplication was installed for it | an independent value | its own, for a later asker |
| owns nothing | a copy of the bytes | its own — nothing was given away |
| owns something | the value, moved | nothing; a second asker is told "gone" |

The middle row is why two joiners polling one task both read the same word, and
it is the row that closes RV2-DEBT-053a's shape properly: a value with no
obligation has nothing to hand over twice.

**Where the duplication comes from, and why not from the descriptor.** The
obvious answer -- `rt_value_ops.clone_init` -- is wrong today and instructively
so. The registry sets `RT_VALUE_FLAG_CLONABLE` only for a type whose clone the
compiler MONOMORPHIZED, so a `string`, whose duplication is the runtime's own,
arrives clonable in the language and unclonable in its descriptor. Making the
backend widen that bit was tried and reverted: two tests pin "the descriptor's
capabilities are the registry's", the registry is the authority on purpose, and
a backend that invents a capability is exactly the drift they exist to catch.

So the duplication rides with the CLONE, which is the operation that took the
obligation on: `rt_task_clone(handle, duplicate)` installs the body that will
serve a second asker, and the task's descriptor keeps saying only what the
STORAGE needs -- how this value moves and how it is destroyed. An un-cloned task
is asked once and moves its result out, and installs nothing.

Whether `CLONABLE` should mean "a duplicate can be made" rather than "a body was
monomorphized" is a real ABI question with a real cost (every string-carrying
type would emit glue), and it belongs to D4b's entitlement work, deliberately,
rather than to a storage flip as a side effect.

A `Task<T>` whose T the backend cannot duplicate at all -- only a dynamic array
-- still refuses a second asker rather than handing two of them one buffer, at
the same moment the old representation refused it: when the second asker
arrives.

**Freeing a task destroys its result**, which runs generated code, so it may no
longer happen under a scheduler lock — §8 P2 again, in the shape a task wears
it. A lane holding a lock links the task into a list threaded through the tasks
themselves and frees it the moment it releases the last one. A side table would
have put an allocation on the path whose entire purpose is running a destructor
safely; the intrusive list costs none, which the census below shows.

**Remote select keeps its word.** Its reply still answers "which arm won", and
D5 retypes it. `rt_task_result_take_word` exists for exactly that one caller and
goes away with it.

**Measured** (valgrind `--leak-check=full`, probes returning a heap string and a
two-field composite):

| probe | allocs | frees | definitely lost |
| --- | --- | --- | --- |
| string, before (boxed word) | 84 | 5 | 0 |
| string, after | 84 | 6 | 0 |
| composite, after (16 bytes, inline) | 84 | 6 | 0 |

The composite is the point: it did not fit a word, so it used to be boxed, and
now it lives in the task's own storage.

`TestRuntimeV2TaskResultCensusBalanced` is the falsifier. It windows a scalar
result and a composite one side by side and asserts they cost the SAME, at one
iteration and at eight -- an equality rather than a pinned absolute, because the
absolute is task machinery this step does not claim to have changed while the
DIFFERENCE is exactly the box it removed. Against the pre-flip tree
(`c73c9f41`) it fails with the numbers that say why: narrow 5/27, wide **6/35**
-- one block at the window edge and one more per iteration. On this tree both
read 5/27.

**Three surfaces found by gates rather than by reading.** The `§8 P2` guard caught
`rt_timeout_poll` taking a result under the control lock -- and taking it into a
WORD, which for a composite would have written past an eight-byte stack slot.
It is typed with the rest now. And the freed-channel waiter row caught a
use-after-free: taking the structural free out from under the control lock
removed the mutual exclusion that a cancel arriving on another thread relies on.
The two halves of reclamation have opposite requirements, and `reclaim_task` is
where that is now written down. The third was the clone glue itself: it fixed up
a copied value's MEMBERS and nothing else, so a body whose own type owns
something directly -- a bare `string`, which is what a `Task<string>`'s result
is -- copied the word and left two owners of one buffer. Latent until this step
asked it to duplicate a leaf; a double free the first time it did.

**Evidence, on the whole tree.** `make check` 0. `make golden-check` 0 with no
corpus drift -- the emitter changed for every async program and no fixture
moved, because the goldens pin diagnostics and MIR rather than LLVM. 19 of 21
`runtime-v2-*` gates green; `carrier-check` and `ownership-check` red with text
byte-identical to the base commit (`blocking-scalar: 213 != 278` and 24 issues),
both sanctioned. `make behaviour-check-mt` 0; `make behaviour-check-all` red on
`string_from_bytes_invalid_utf8/llvm` alone, which segfaults identically at the
base.

## 2026-08-25 — D5: a remote select's arm owns its payload

A remote select shipped each SEND arm's payload as a machine word beside a
numeric drop id. Two fields for one value, and they could disagree: the id said
how to destroy it, the word said nothing about how to move it or how wide it
was. Anything wider than a word had to be boxed to travel at all.

Each arm now owns an `rt_value_cell`, which is D4a's task-result slot extracted:
one descriptor, storage sized from it, and a lifecycle of EMPTY -> INITIALIZED
-> MOVED or DROPPED. The two owners needed the same thing for the same reason,
which is what makes it one type rather than two that drift.

That is also what the model already said a select is. RUNTIME_V2.md's ownership
section: *"every payload lives in a typed slot owned by the sender, the channel,
the receiver, or the select operation"* — and *"a `select` operation owns its own
staging slot"*. This step is that sentence, implemented.

**The value moves three times and is copied never.** Out of the caller's storage
when the select arms; out of the arm's cell into the channel when that arm wins;
back into the caller's storage when it loses. The addresses for the first and
the last are the caller's LIVE storage on the poll that asks — the pending keeps
neither, for the same reason D4a's destination is passed only on the terminal
retry: a park between polls would leave a stored address naming a frame that is
gone. The retry pass rebuilds them from the MIR place each time.

**The cell answers what the committed index answered from outside.** A value
already moved out leaves its cell MOVED, so the pending's cleanup disposes every
arm and destroys exactly what is still owned. `select_committed_index` stays as
the record of WHICH arm won; it is no longer also the record of which payload
still exists.

**The reply is typed with it.** The winner index is an integer and it now
travels the way every other result does: the reply names the selector body's
result cell and the caller moves the value out of it.
`rt_task_result_take_word`, which D4a left for this one caller, is gone.

**Where the element type comes from.** The arm's descriptor is named by the
CHANNEL's element type, not by the operand's. Asking the operand would let a
literal's own type size a cell the channel's element does not fit — the
storage-flip defect shape ("something asked a TYPE for an answer only its
container can give") in one line.

**Measured.** `far-select-wide-payload` sends a payload TWO WORDS wide through a
select arm and reads both words back out of the channel intact, with the
caller's storage emptied by the staging move and no drop run on the way. The row
cannot exist against the previous representation: its arm field was a
`uint64_t`. `TestRuntimeV2FarSelectNonCopySendArm/valgrind` still reports strict
zero on a `far Channel<string>` whose losing SEND payload is handed back.

**A gate that only speaks after the commit.** `runtime-v2-file-size-check`
measures COMMITTED blobs, so D4a's four-line growth of
`runtime_v2_lock_split_harness_test.go` past its legacy ceiling was invisible
until D4a was committed. It is shrunk back here. A pre-commit run cannot see
this class of violation at all — worth knowing before trusting a green
pre-commit as the whole answer.

## 2026-08-25 — D6: a blocking body's result keeps its width

`__surge_blocking_call` returned a `uint64_t`. A blocking result wider than a
machine word was therefore boxed by the worker thread and adopted again by the
awaiting poll — and the two sides did not agree about whether that word WAS the
value or pointed at it. A composite result printed garbage for its first field
and then segfaulted. That is not a latent shape: it is what the probe did, on
this tree, before the flip.

The body now writes its result INTO storage the runtime sized from the body's
own type, and the JOB owns that storage in an `rt_value_cell` until the awaiting
poll moves it into the task's result.

**Why the job owns it.** It is the only thing that outlives both frames which
touch the value: the worker thread's, which produced it, and the poll's, which
comes for it later. A result nobody comes for — a cancelled awaiter, a
torn-down executor — is destroyed by that cell where the job is released,
exactly once.

Both cells bind the SAME descriptor, named by `rt_blocking_submit`'s new
`result_type_id`. That is what lets the value MOVE between them rather than be
rebuilt from a word on the other side.

The sret case improves twice over: a body that already writes through a
destination pointer is handed the runtime's own storage, so there is no
frame-local copy on the way either.

**Falsifier.** `TestRuntimeV2BlockingResultKeepsItsWidth` drives a composite
(two fields, one owning heap) and a bare string through `blocking`, and reads
both back. Against the pre-flip tree it fails on the composite's own count
field; on this tree it passes. The word-shaped case's allocation profile is
unchanged: 91 allocs / 9 frees on both sides, including a one-byte leak in
compiled code that predates this and belongs to neither lane.

## 2026-08-25 — D7: a shipped state is destroyed through its own descriptor

A state box the runtime holds — a crossing's shipped state, a suspend frame a
cancellation abandoned, a reply-edge result nobody took — was destroyed by
NUMBER. Three generated dispatch tables (`__surge_drop_call`,
`__surge_drop_abandoned_state_call`, `__surge_drop_result_call`) each routed an
id to a per-type release, and each was a place where a value and its destructor
were connected by a lookup rather than by the value's own type.

The runtime resolves the type's DESCRIPTOR now and does both halves itself: the
members through `drop_in_place`, then the storage at the width and alignment the
descriptor states. That is exactly what the generated release did, which is why
the tables could go rather than be rewritten. The ids the crossings carry are
TYPE ids, and the fields say so: `state_drop_fn_id` → `state_type_id`,
`result_drop_fn_id` → `result_type_id`, `abandoned_state_drop_fn_id` →
`abandoned_state_type_id`.

It fails closed where the type is still legible: `crossingStateTypeID` refuses a
state type the operation census never saw, because a type with no descriptor
would be freed at the opaque word's eight bytes whatever its real width.

**Four things the flip made visible.** Each is a contract the old dispatch let a
caller state without honouring:

- `mark_done` destroyed an abandoned state while holding control — generated
  code under a scheduler lock. The detached guard says so now, and the release
  is deferred to the moment the lane is free.
- the publication stands shipped a STACK address as their state, with a stub
  that only counted. The runtime frees a shipped state — it always did, through
  the generated release — so the stands hand over a real block now.
- a join-waiter stand yielded with a POLL id in the drop-id slot, on a state
  that is a borrowed task handle. Nothing had ever dispatched it; the descriptor
  lookup would have freed a task pointer as an eight-byte block.
- work deferred by the LAST turn a worker thread takes was never run: the thread
  that owed it exits and its queue is per-thread. `rt_lane_run_deferred_now` is
  called on the way out of both the scheduler and blocking pools.

**`emit_channel_reclaim_shape_test.go` is deleted**, as its own note asked: it
pinned the asymmetry between a descriptor's drop and a result drop, and said to
delete it on the day a cell holds what the descriptor describes. D3 made that
true for channels; this removes the second body entirely.

**A flaky row, measured rather than assumed.**
`TestRuntimeV2SelectReleasesA{String,Composite}PayloadExactlyOnce` demand that
two valgrind runs leave the SAME outstanding-allocation count at four takes and
at eight. On this machine that differs by ±1 often enough to fail about one run
in three — and it fails with a NEGATIVE slope as readily as a positive one,
which no leak can produce. Measured against the pre-D7 tree: 2 failures in 6
runs there, 1 in 6 here. It is the row's zero tolerance, not the code.

## 2026-08-26 — Wave D resumes on five lanes: D4b (both halves), D2, RV2-DEBT-167, the carrier scanner

Base `0d00d9fa`. Baseline taken before the first edit, all 26 gate targets
enumerated from the Makefile and run serially (`scratchpad/baseline-0d00d9fa/`):
`make check` 0, `golden-check` 0, 17 of 21 `runtime-v2-*` green; red as
sanctioned: `carrier-check` (`blocking-scalar: 213 != 278`, `array-grow-composite
8 != 7`), `ownership-check` (24 issues); `behaviour-check-all` red on
`string_from_bytes_invalid_utf8/llvm` alone, `behaviour-check-mt` 0; tagged
`internal/vm` 10 failures (list in the baseline directory); `transport-check`
exit 2 under load (a 300 s package timeout) — to be re-run alone before it is
read as anything. Lanes ran in worktrees cut from `0d00d9fa` by the lead, not
by worktree isolation; every agent report leads with `BASE=… LINEAGE_OK`.

### Owner rulings taken today (recorded in full in the plan and the memory)

Local `Task<T>` is droppable; drop retires the handle's entitlement and gives
its reference back, never cancels; `far Task<T>` stays affine; no bitwise Copy
for Task. `rt_task_is_last_asker` (yesterday's uncommitted tail, inferring
ownership from the handle refcount) is reverted: §10 keeps its sentence. The
duplication recipe rides with the clone operation; three notions are kept
apart — `LanguageClonable(T)`, `ops.CLONABLE` (a callable monomorphized
`clone_init`), `result_duplicate` (the recipe a particular clone installed).
VM parity is D4b's. SEM3107 stays strict AND each `clone()` is a source-level
entitlement (the two one-sided programs are refused symmetrically); the runtime
drop path serves cancellation, abandoned frames, container teardown and
shutdown, never a forget API. `Map<K,V>` owns what it is given; `keys()` is an
independent owning snapshot through the language's clone recipe; no second map
type. D7's tail is the full §11 frame. Bench budgets move only by the
≥3-agreeing-runs procedure. The carrier scanner gains categories for the two
channels it could not name (blocking captures, compiled-code-owned frames) and
the base census is re-captured by scanning `7df10725`.

### D4b, native (lead): `6d7a0ee8` · `f749b7d3` · `12e93f33` · `421df648` · `5bda5efd`

- **Sema.** `.clone()` on a task registers its call expression exactly as a
  spawn does, so the binding, the await, the return and the pass-as-argument
  sites find it; before, `let s = t.clone(); t.await()` compiled and dropped
  `s` silently while the mirror image was refused. Eight-row unit test, two of
  them red on the old tree; five golden fixtures; every corpus program that
  clones a task still diagnoses clean.
- **The drop path.** `rt_task_handle_drop` releases the handle's reference and
  decides nothing about the result; `emitDrop` and the composite walk reach a
  task handle through it. The falsifier had to be re-found: the 24-byte
  residual `TestRuntimeV2CancelledSuspendStateReclaimed` documented was already
  gone at `0d00d9fa` (D7), so that row went to strict zero on both sides and
  proves nothing about this change. What does: a body cancelled before its
  first poll abandons its frame at `t.await()` with a clone live, and a task
  the executor still holds at exit is freed by teardown — invisible to
  valgrind — so `TestRuntimeV2AbandonedFrameReleasesItsTaskHandles` reads live
  heap blocks from inside the program: 4 per round and `tasks_done=10` at exit
  without the release, 2 per round and `tasks_done=0` with it. The 2 that stay
  are RV2-DEBT-249.
- **Entitlement counts.** `rt_task_entitlements` (new `rt_task_entitlement.{h,c}`,
  `rt_async_internal.h` stays at 668 against its 676 ceiling by absorbing
  `result_shared`/`result_duplicate`): `live`, `claimed`, a reserved `mover`,
  atomic `clone_readers`, `move_waiting`, `moved`, the recipe. Decided under
  the owner shard lock; the handle refcount stays memory lifetime. The last
  asker moves; a mover that finds a reader out is answered WAIT (kind 0, which
  a DONE task never otherwise answers), parks on the join key (poll) or on
  `done_cv` under control after checking the reader count there (external
  await), and the reader that retires last wakes it. The first version counted
  only unclaimed handles and lost on concurrent askers — two askers each saw
  the other as "someone who can still ask" and both cloned, 6–10 rounds of 96
  — which is why `claimed` and the reservation exist.
- **How duplications were counted.** A heap census could not separate a
  duplication from scheduler queue growth (±1 per window at four askers), and
  a string literal duplicates for free. The row that works: the result type's
  user `__clone` marks the copy it builds, so an asker knows whether it holds
  the original; per round exactly one must. That also exposed that the task
  duplication ignored a user `__clone` entirely (structural glue only) — the
  three-notion ruling settles it: a descriptor carrying `CLONABLE` is used.
- **A pre-existing hang, found by the contention row, fixed as
  `421df648`.** `park_current`'s second abort branch swept a seq-0 join entry
  unqualified; the requeued task had already re-registered on another worker;
  one stranded asker in ~15 runs. Diagnosed by tracing JOINADD/JOINREMOVE/
  JOINCOLLECT with a `backtrace()` at the removal (ptrace is off here). Gated
  on a nonzero generation like the first branch; 60 of 60. A deterministic
  sync-point proof is owed: RV2-DEBT-248.
- C-stands that build an `rt_task` by hand now call `rt_task_entitlements_init`.
  Two invariant panics recorded in the panic ledger (PG-INVARIANT).

### D4b, VM (agent): `210c206b` · `d4ead546` · `f0db0bd7` · `3ca25486`, on top of 167

A poll's return value was evaluated in the poll frame and that frame retired by
the same terminator, so a composite task result named an arena already given up
(`panic VM1999: storage: stale reference … (generation 1, arena is at 2)`).
`transportCopyIn` — the copy a channel send has owed since Wave C — now runs at
the async return. An asker with no handle is a CLAIM (that is what a `timeout`
is; the handle route detonated in the 167 lane and is recorded as a dead end).
The last asker moves, earlier ones duplicate; VM parity row for the native
clone test; cancel through a sibling is task-global. **Lesson worth its own
memory:** on the VM lane `runVM` returns `ExitCode` alongside a fatal error, so
an e2e row that reads only the exit code is vacuous — assert stderr too. The
VM's duplication call is a `composite-box-marker` carrier and is tracked as
RV2-DEBT-246; task records are never removed from `e.tasks`: RV2-DEBT-247.

### RV2-DEBT-167 (agent): `b04ef46d` — closed, see the row.

### D2 (agent): `2ba2e0cf` · `01579589` · `429f8821` · `97351ecf`

The VM half was already typed (`807bf541`); 158/172 closed 2026-08-19 through
SEM3018, so the plan's "D2 carries a known live defect" paragraph is history.
Native: `SurgeMapEntry{uint64, uint64}` is gone; two typed runs in one
allocation with `key_ops`/`value_ops`; growth moves elements, remove is
swap-with-last through `move_init`; every entry point takes an address;
`HasDst:false` means destroy, not "write to an alloca nobody reads".
`rt_map_free` drops live keys and values through their descriptors; `keys()`
takes a clone glue from the compiler. The order was flip THEN ownership, on
purpose: a drop over word storage would have destroyed eight bytes of pointer
as a value. 33 carrier identities retired (`rt_map.c` 18, `rt.h` 10,
`emit_intrinsics_map.go` 5 → 0); the map was the last caller of
`emitValueToI64`, so the copy-in leg of RV2-DEBT-151 is deleted, the adopt leg
survives. `canDuplicateValue(Map)` flipped to no — its justification ("a map
drop frees nothing anyway") stopped being true in the same commit.
`emit_intrinsics_map.go` 570 → 483. Owed and recorded: RV2-DEBT-250 (linear
`map_find`), 251 (`rt_key_ops` unused), 252 (no sanitizer stand, no owning-key
bench row). The two new golden fixtures had their sidecars generated at
integration by the lead.

### Carrier scanner (agent): `fab9f397`

Two categories the gate could not name: `untyped-capture-state` (15 base rows,
retires with D6's tail, RV2-DEBT-080) and `suspension-frame-owner` (7 base
rows, retires with D7's tail, RV2-DEBT-179). Base census re-captured by scan
(604 → 626, digest moved), the ten old categories byte-identical. A token
that would survive the fix it is meant to witness (`rt_blocking_submit` by
name) was refused on purpose.

### Integration

Cherry-picked in dependency order into the main tree by the lead; three
textual conflicts (D2's map arms beside D4b's task arms in `emit_drop_glue.go`
and `emit_instr.go`, and both lanes' heap-gate rows in the Makefile) resolved
by keeping both. Gate rows for the two new invariant panics and the VM
duplication carrier were added at integration, not in the lanes.

**D2 integration exposed a pre-existing LLVM defect in reading a reference payload.**
Recording the behaviour outputs for D2's two new map goldens, `map_composite_value`
printed `10 21 1 30` on the VM and `0 0 1 0` natively: `m[k].a` on a
`Map<int, Pair>` read 0 while `replaced.a` read 30. `__index` answers
`Option<&V>`; `emitTagPayload` spelled that payload as `llvmValueType(Pair)`,
a `[16 x i8]` run, and `emitStorageMemberLoad` on a run returns the ADDRESS --
so the compare arm received the address of the pointer slot and read the
pointer's own bytes as `a`. The cause is one level up: MIR's `canonicalType`
strips `&`, `*` and `own` from `TagCaseMeta.PayloadTypes`, so the emitter cannot
tell `Option<&Pair>` from `Option<Pair>` through the metadata at all. The defect
predates D2 (the same probe on the pre-D2 compiler crashed in `munmap_chunk`)
and stayed invisible because every reference payload before pointed at a word,
which loads the same either way. The fix asks the union's own membership for
the declared kind (`declaredTagPayloadIsRef`) and loads a `ptr`; pinned by
`TestEmitTagPayloadOfReferenceLoadsThePointer` (red on the unfixed emitter:
"payload address %t3 ... is stored as the payload") and by the two goldens on
both lanes. The construction side is a separate, older gap -- sema types
`Some(p)` with `p: &T` as `Some<T>` -- recorded as RV2-DEBT-253.

### 2026-08-26 — Wave F, the diagnostic half (P5), lands: eight commits

Integrated from `wip/wavef-diag` (cut from `0d00d9fa`; `9fe013eb..8a3c7eb2`).
The clonability classifier already carried four canonical states, so
RV2-DEBT-134 was a naming, not a missing authority; C1 adds what turns a
verdict into an edit -- the path to the first non-clonable component, the span
of a rejected `__clone`, and `CanDefineHere` proven by the same receiver
identity rather than `!sealed` (the falsifier, the `!sealed` form, fails 7 of 18
rows). `Task<T>.clone()` owes an independent payload: the concrete case on the
post-merge seam next to `FinalizeDirectCloneBindings`, the generic case as a
fourth kind of deferred edge that resolves to a verdict rather than a callable,
both through ONE function and ONE classifier -- the two `SEM3116` headers in the
new goldens differ only by file and line. Without the instantiation check the
falsifier reproduces the raw mono refusal RV2-DEBT-133 names (`instantiation
closure: deferred callable ... (__clone): no exact implementation`). `SEM3204`
is declared for `Map<K, V>.keys()` with a non-clonable key; its emission site
is the map lane's (`tc.requireClonable(CloneObligationMapKeys, ...)`).

`Diagnostic` gains a `Help` channel of its own (eight fakes with a `"help: "`
prefix migrated); `surge build` printed `d.Message` and nothing else, and now
carries code, span, notes, help and fix headers with applicability. Eleven
advice emitters collapse into one table of site × capability; the advice says
`clone(x)` -- the free function, autoref confirmed against
`stdlib/http/headers.sg` -- and `.clone()` only for a local `Task<T>` whose
payload is duplicable. One golden line changed, legitimately:
`task_await_use_after_move.diag` loses `; call t.clone() to keep a handle`,
because the way out moved into Help, where it is conditional on the payload.
`fix once` no longer falls back to a non-safe edit and prints what it skipped
with `--id`; replace/delete edits without `OldText` are refused. LSP: 
`relatedInformation` (notes, then help), `publishDiagnostics.version`,
`Diagnostic.data` carrying identities only (a test marshals and fails on the
words `newText`/`oldText`/`edits`/`range`), and Code Actions only AlwaysSafe,
registered on the server, guarded before and after materialization, with
versioned `documentChanges` -- 11 race rows, each of which first requires the
action to EXIST and only then breaks a condition.

Two calls the lane made alone, recorded for review: the `nosend_checks.go`
headline carried its advice inline (`...or a copy via .__clone()`) and now
carries it in Help; and `adviceCloneState` is a projection of the authority
onto file-check time (the checker's magic-method registry, imports included),
conservative in the direction of not proposing a clone it cannot prove -- in a
snippet stand without the stdlib, `string` reads as non-clonable and the
advice offers only the borrow (`move_through_calls_test.go`).

Effective lines: `internal/lsp/server_diagnostics.go` 577 → 551,
`internal/sema/type_expr_calls.go` 451 → 448, `internal/diag/codes.go` 593 →
595 (SIZE_EXEMPT), eleven new files all under 200. DEBT: 133 diagnostic half
closed, 134 closed, 135/136 narrowed, 254 opened (corpus spelling).

The two lanes met on one seam. D4b's tracker registers a `Task<T>.clone()` as
an entitlement from its RECEIVER; Wave F's generic obligation leaves the
clone's own type deferred until instantiation; and `trackTaskReturn` asked the
returned expression's type before asking the tracker -- so `return
handle.clone()` inside an uninstantiated generic (`task_clone_uninstantiated_generic`,
a VALID golden) reported SEM3107 on the merged tree while each lane alone was
green. The tracker is asked first now (`clone_returned_in_place_generic` in
`task_clone_entitlement_test.go`, red on the pre-fix tracker).

The hook then found the second seam, and it was not Wave F's. Wave F's
SEM3116 refused `Task<Box>.clone()` in the VM lane's cohort test (a `Box` of
`string` and `int` with no `__clone`, which the owner's ruling makes
non-clonable: the test now declares the contract), but what the test reported
was `HIR merge failed: internal compiler error: clone at 9:5014-5029 has no
published implementation`. The VM-lane helper `compileToMIR` sized its
diagnostic bag at ZERO (`DiagnoseOptions` without `MaxDiagnostics`, and
`diag.NewBag(0)` drops every `Add`), so it had never seen a refusal; the
finalization consumed SEM3116 into a bag that kept nothing, returned before
publishing, and lowering met an unpublished clone. The driver now sizes an
unsized bag at `DefaultMaxDiagnostics` and the merge seam answers
`ErrDiagnosticsReported`; six test helpers gained eyes at once, and two programs
they had compiled blind turn out to be refused by sema -- both red at the
baseline in the tagged run, both skipped now naming RV2-DEBT-255 (a magic
`__index` read's borrow outlives its statement) and RV2-DEBT-256 (`ownsHeap`
still says a `@copy` composite owns a box). RV2-DEBT-257 records the helper.

### 2026-08-26 — the after-run against `ed8144ee`, and what it found

The same 26-gate script as the baseline (`baseline-0d00d9fa`), same order:
19 gates text-to-text as at the baseline (`carrier-check` and
`ownership-check` red with the sanctioned refusals, `behaviour-check-all` red
on `string_from_bytes_invalid_utf8/llvm` only, `behaviour-check-mt` green),
`runtime-v2-transport-check` green both in sequence and alone (the baseline's
2 was the 300 s package timeout under load), `runtime-v2-file-size-check`
PASS on the committed blobs. Two gates turned red and the tagged suite gained
nine `TestMT*` rows; three causes, none of them where the lanes had looked.

1. **`runtime-v2-heap-check` and the nine `TestMT*` rows: one panic.**
   `panic: invalid task handle` from `drop_elem → rt_task_handle_drop(NULL)`:
   the drop glue of a `Task<T>[]` visits a slot the handle was moved out of,
   and D4b's `rt_task_handle_drop` handed NULL to `task_from_handle`. Guarded
   (`TestRuntimeV2TaskHandleDropTreatsAnEmptySlotAsNothing`, a C stand red
   without the guard). Under the guard the SAME programs fail one layer
   deeper -- `panic: async: invalid task owner shard` at teardown -- because
   `for t in tasks { t.await() }` does not empty the slots it consumed, and
   `for s in names { take(s) }` over strings is a double free today under
   valgrind. That is a language question at the seam of the for-in ruling and
   the droppable handle: RV2-DEBT-258, owner decision, reproducer skipped.
   The MT rows and the heap contract stay red until it is made.
2. **`runtime-v2-carrier-sanitizer-check`: D2's own valgrind row leaked
   210 bytes in 10 blocks,** and it did so on the lane's own worktree
   (`ad0efc0a`) -- the row was a claim, not a measurement (the lane could not
   run it). Attributed by building one program per map function: only
   `after_removals` (84 B / 4 blocks) and `map_worker` (42 B / 2), exactly the
   `Owned` values that `remove` and an overwriting `insert` hand back into
   `let _ = ...`. The map was innocent: **`let _ = e` never released `e`**, in
   any program -- `observeMove` consumed the initializer's temporary on behalf
   of a binding that never dropped, while the statement `e;` released it.
   `let _` is the discarded-result path now (`TestDiscardedLetIsFlagged`,
   `TestRuntimeV2DiscardedLetReleasesItsValue` strict zero), and a discarded
   PLACE moves nothing (`let _ = x` leaves `x` with its binding, so
   `discarded_field_read` moved from the refused rows to
   `TestDiscardedFieldReadTakesNothing`). One MIR golden changed with it. The
   map row is strict zero on shards 1 and 4 now.
3. **D2's bench budgets are not measurements.** The lane's last commit says
   so itself: 68→4, 68→4, 64→0 are "the numbers the change is EXPECTED to
   produce". Under ruling 8 they are re-measured by the lead in a clean
   `git archive` tree, ≥3 agreeing runs, before RV2-DEBT-156 can close;
   its five valgrind configurations are met. RV2-DEBT-157's first clause is
   met too (`map_index_set.sg` runs natively, 11 on both lanes); its parity
   row for a heap-owning value and the 158 reproducer build are still owed.

The tagged suite otherwise lost three baseline reds
(`TestVMRefsMapGetMutReadonlyHelperKeepsMutRefLive` green;
`TestVMImportedStdlibMagicMethods` and `TestRuntimeV2CompositeCopyIsIndependent`
skipped naming 255/256) and kept `TestLLVMParity`,
`TestMTNonYieldingTrySendHandoffWakesReceiver`, `TestVMAsyncChildPanicHaltsProgram`,
`TestVMImportedStdlibMagicBinaryOperator`, `TestVMPanicDropsBufferedChannelPayloads`
as they were.

**The bench cannot be re-measured on this branch as it stands.** Three runs of
`make runtime-v2-carrier-bench` in a detached worktree at `992ad672` (a bare
`git archive` is refused: the bench asks git for the manifest) all abort in
the same place, before a single row: the harness builds the `epic_base`
compiler (`7df10725`) and compiles every fixture with it, and
`scored/maps-scalar` and `scored/maps-composite` are written with
`Map::<K, V>.new()` -- the `new` canon of 2026-08-21, which the base compiler
does not know (`LLVM emit failed: unknown external function "new::<Payload64>"`).
The map fixtures did not change on any Wave D lane (last touched by
`cabc951e`) and do not exist at the base at all; the bench has been
unrunnable since the `new` migration, which is what the 2026-08-21 handover
meant by "carrier-bench not caught up". The manifest is digest-frozen and its
`epic_base` is a gate-contract fact, so re-capturing it is the owner's, not a
lane's: it is the open half of RV2-DEBT-174, and RV2-DEBT-156's bench clause
waits on it.

**The owner ruled the same evening: the base is the latest green commit, no
pin.** Measured that way (throwaway worktree at `31af371e` with `epic_base`
and the six `provenance_commit` fields set to it, budgets iterated one row per
run because the harness aborts at the first mismatch): the bench runs, and the
budgets it carries are stale in both directions -- `blocking-composite` 277 →
**341** (+64: one block per job, the `rt_value_cell` a composite result wider
than 16 bytes takes since D6-results), `channel-buffered-composite` 78 →
**14** (−64: the box per element the typed channel storage removed in D3).
The full row-by-row figure lands in the plan's W4 closeout with the manifest
re-capture; this is the first measurement of the bench since the `new`
migration.

### 2026-08-26 — W2 lands: D3b C0 and D6-tail a-1/a-2

**D3b C0 (`8c9851a6`, `1a0b5914`, `743f034e`).** A channel counts what still
names it: `handle_refs` for the copies a program holds and `pins` for the
runtime's own holds, both atomic, both in the new
`runtime/native/rt_channel_refcount.{h,c}` because `rt_async_channel.c` sits
at 500 effective lines exactly (it took +1 for the fail-closed assertion and
gave back one in `rt_channel_new`, whose `memset` became
`rt_channel_handle_refs_init`). The last handle drop reclaims through the
existing `rt_channel_free_when_unlocked` ring; a third field, `reclaiming`,
settles who reclaims when the last handle and the last pin retire on
different lanes. The far registry now releases through `rt_channel_handle_drop`
(`rt_far_channel.c:226`), byte-for-byte the same path as before. Nothing in
the language changed: no pin is taken yet (C3) and no `Channel<T>` is dropped
by generated code yet (C1). Measured by the lead: the C stand's three rows are
green plain, under valgrind (strict zero), ASan/UBSan and TSan; with
`-DRV2_DEBT_155_NEGATIVE_CONTROL` TSan reports the data race at
`rt_channel_handle_retain`/`rt_channel_handle_drop` themselves. RUNTIME_V2 §7
was read against the code while doing it: the two reference kinds are now
there; the teardown order §7 prescribes (mark dying under the owner lock,
detach every initialized slot, invalidate generations, release, then drop and
free) is NOT what `rt_channel_free` does -- it takes no lock (it refuses to
run under one), has no dying mark, and detaches and drops slot by slot inside
`rt_typed_fifo_drain`. RV2-DEBT-259 records the divergence rather than
patching it in a lane.

**D6-tail a-1 (`dcdcb2da`) and a-2 (`0a3fa567`).** Unpacking a blocking
capture declares the transfer (`MoveOut` from the same `byValueArgContract`
the calls use); the job's captured state is one `rt_value_cell` adopted from
the block the compiler already allocates (`rt_value_cell_adopt`, new), claimed
by the worker immediately before the body runs and disposed through its own
descriptor at release -- an initialized cell is walked and freed, a moved one
only freed, so a cancellation landing mid-body cannot come back for captures
the body owns. `rt_blocking_submit` takes the state's type id instead of a
size and an alignment (`rt_async_internal.h` 668 → 666), and the emitter
refuses a state or result type the operation registry does not know, naming
the eight-byte word it would otherwise have become. Measured by the lead on
the integrated tree: `TestRuntimeV2BlockingCapturelessStateIsFreed` (the
one-byte block a capture-less body used to lose) and
`TestRuntimeV2BlockingCaptureValgrindZero` at `rounds=1` and `rounds=8` are
strict zero. The two negative-control toggles (`RV2_DEBT_080_NEGATIVE_CONTROL`,
`RV2_DEBT_080_WALK_ALWAYS_NEGATIVE_CONTROL`) compile but have no stand yet
that observes them -- that stand is a-4's cancel-after-claim row, and until it
exists the "walk always" half of a-2 is asserted, not shown. The
`untyped-capture-state` carrier category reads 0 live rows.

### 2026-08-26 — the owner's rulings land: a `for` reads, a composite owns what a member owns

Seven rulings came back the same evening; two are code now, both built in
worktrees from `992ad672` by one implementer and two independent reviewers
each, and every reviewer accepted with minor findings only.

**Q-A (RV2-DEBT-258): `for` reads, a container is popped.** A move out of a
`for` binding -- whole or by field -- is refused with SEM3205, and the
tracker no longer demands that move: `checkForInTaskConsumed` is gone, a
`for` over a pending task container leaves it pending, and the scope-exit
SEM3107 names the loop that only read it and the drain that empties it
(`while xs.__len() > 0:uint { let t = xs.pop().safe(); ... }`). `for x in own
xs`, which parsed and did nothing, is refused with SEM3206 so the spelling a
consuming loop would have cannot be written into a program meaning nothing --
the trap RV2-DEBT-212 closed for `&mut hs`. Four new goldens, no existing
golden moved. The corpus followed: the nine MT programs, the heap contract,
`mt_correctness_channels.sg`, `showcases/async/02_fanout_fanin`,
`fs_dir_smoke.sg` and `stdlib/fs/fs.sg` (`walkdir_collect` pushed the loop's
copy of an entry `sorted` still owned -- a double free the rule found).

What the lead found running the rows the lane could not: the tracker's drain
loop must DRAIN -- a `return` inside it leaves tasks in the container on that
path, and the tracker refuses that with SEM3107 at the container, which is
the strict rule the owner kept (a spawned task is awaited or returned, never
abandoned). Five drain loops in the MT programs and the heap contract had
`if !ok { return N; }` inside; they set a flag and finish the drain now, as
the other loops in the same files already did. After that, `TestMTBlockingPool`,
`TestMTBlockingChannelHelpersDoNotParkWorkers`,
`TestMTBlockingChannelHelpersDrainReadyWorkAtCompensationLimit`,
`TestRuntimeV2HeapAccountingConcurrentWorkersContract`,
`TestRuntimeV2TaskHandleArrayDrainedByPopTearsDownClean` (unskipped) and
`TestRuntimeV2CompositeCopyIsIndependent` (unskipped) are green on the
integrated tree; `TestLLVMParity` lost `fs_dir_smoke` from its red set and
kept the other seven exactly as at the baseline. One row is intermittent and
was before the rewrite: `TestMTStructuredConcurrency`, 2 of 6 red at
`60287ed8` and 1 of 6 at the integrated tree, its `@failfast async` block
coming back `Success` after a cancelled child -- RV2-DEBT-261, a runtime
race in fail-fast propagation that belongs to D4b's cancel rows.

**`print` reads its string, and converts what it is handed.** The owner's
follow-up to Q-A, in two steps. The lane (`5d80b23d`, `bba4b946`,
`4f5bba29`, `b5e064cf`; one implementer, one reviewer, accepted) made
`print(s: &string, ...)` a borrow, so `for name in names { print(name) }` and
`print(s); print(s)` read, and the corpus `clone(name)` went away; it also
found that a borrowed parameter used to check the argument's PLACE before
its TYPE (`print(42)` reported "cannot take reference to temporary value"
instead of the mismatch) and fixed that first. Then the owner's hello-world
contract -- `print("Hello world!")`, `print(1)` through the implicit
`int.__to(string)`, no `&`, `own` or `clone` at any call -- turned out never
to have held: `print(1)` was refused at every commit before today, and the
corpus writes `print(x to string)` twenty-six times. `8f950b2e` makes it
hold: `print`'s parameter says `@allow_to`, and three things had to change
for that to mean anything -- the conversion targets the referent (there is
no `__to(int, &string)`, and the candidate filter refused wrapper targets),
a converted argument borrows the temporary the conversion produced instead
of failing "not addressable", and `int` reaching `__to` twice (as `int` and
as `&int` through autoref) is one conversion, not an ambiguity. The
temporary is the statement's to release, flagged with the conversion's
target as its type; `TestRuntimeV2PrintReleasesWhatItConverted` reads
strict zero. The reviewer's major finding stays owed: `format(fmt: string,
...)` still takes its template by value. The price the reviewers
named and the owner accepted to pay differently: `print(s: string)` takes its
argument by value, so `for name in names { print(name) }` was refused and the
corpus wrote `print(clone(name))` -- the owner ruled `print` and its
relatives take a borrow, which a follow-up lane does, with the hello-world
shape kept trivially simple (`print("Hello world!")`, `print(1)` through the
implicit `to string`, no `&` at any call). RV2-DEBT-260 holds the residue the
reviewers found: a parenthesised `(own xs)` still slips past SEM3206, a
`compare` over a union-typed loop binding is refused even when the arm only
reads a Copy payload, and SEM3107 points at the container rather than at
the `return` that abandons it.

**Q-B (RV2-DEBT-256): a value composite owns heap iff a member does.** One
structural walk (`ownsHeapIn`, exported as `sema.OwnsHeapIn`) answers for
`tc.ownsHeap`, `Result.OwnsHeap` and HIR's `normCtx.ownsHeap`; the MIR legs
delegate and the backend already walked, so every compiler leg agrees with
the storage the flips left behind. `@copy Pair { a: int, b: int }` handed out
of a borrowed arm is accepted now; a composite with a string is refused as
before; a composite with a `float` -- the reference-counted scalar -- stays
refused, because it does own two counted blocks (the row is in the test with
that reason). Six test files outside the lane's ownership had to follow the
axis (a `Held { v: int }` fixture that assumed the boxed answer, a capability
test that asserted the disagreement). `docs/runtime-v2-epics/23-value-composites.md`
lines 284–300 still describe the boxed-era target for the two sema legs and
are wrong now -- RV2-DEBT-260 carries that too.

### 2026-08-26 — RV2-DEBT-261 lands its join fix; the symptom outlives it

The lane's four commits (`a9ba47c5`, `36131c49`, `872735ee`, `2d7c6ac0`)
close a real window: `rt_scope_join_all` read `failfast_triggered` and
`active_children` under one lock at its first snapshot and re-read the
count ALONE at the register-then-verify re-check, so a cancelled completion
landing between the two answered "drained, not fail-fast".
`scope_join_snapshot_locked` reads both at the verify too;
`SP_SCOPE_FAILFAST_JOIN_BEFORE_VERIFY` holds the gap open and
`RV2_DEBT_261_NEGATIVE_CONTROL` drops the flag from the verify.

Two things the lead's runs added. First, the stand the reviewer accepted
did not run: at every thread count it timed out with "cancelled child never
completed". The owner created its child with `__task_create` from inside
its own poll, which lands on the pushing worker's LOCAL tail, and a single
local entry signals nobody (`ready_push_task_locked`, `rt_ready_queue.c`);
the pusher is the worker then held at the sync point, so the child was
never popped and its cancellation never observed. That is the stand's
defect, not the runtime's (§7: the local queue is the pusher's path).
`37db26fc` spawns the child from the driver, on the inject queue, before
the owner; the owner's first poll registers it. With that: proof green at
`SURGE_THREADS=2/4/8` with "landed: active=0 failfast_triggered=1", the
negative control red AT the verify ("answered Success after a cancelled
child"). A stand whose owner is held inside its poll must get its children
from the driver -- recorded for every future lifecycle stand.

Second, the symptom is not closed. On the fixed tree, pinned off the bench
CPUs, `TestMTStructuredConcurrency` exits 12 in 7 of 20 runs and the new
`TestRuntimeV2FailfastJoinAnswersCancelled` once in 20 (`llvm`,
`SURGE_THREADS=4`) -- the same rate as before the fix (2 of 6). The second
lens (Codex, `scratchpad/second-opinion-261.md`) names the live window: a
task commits `TASK_RESULT_SUCCESS` with `cancelled=1`, because cancellation
is observed only at suspension points whose target is not yet `TASK_DONE`
(`rt_task_poll` after the DONE fast path, `rt_async_yield`) and
`rt_async_return` publishes unconditionally; fail-fast keys on the child's
`result_kind` alone. The fix belongs at the success-commit boundary in
`mark_done`, linearized against `cancel_task`, with VM parity in
`vm_terminator.go`. That is RV2-DEBT-263, on its own lane from this HEAD;
261 stays open until 263 reads 0 of 20 on both rows.

### 2026-08-27 — RV2-DEBT-260, review round two: two SEM3107s that named the wrong statement

The reviewer rejected `9e2e58b2`. Moving SEM3107 from the container to the
exit that abandoned the drain (item 3 of the row) had landed a rule that
fired on statements it did not describe, in two ways, and both were false
sentences about a `break` rather than missing advice.

**A `break` of an inner loop was charged to the drain around it.** The
tracker kept only DRAIN loops on its stack, so `noteTaskContainerLoopBreak`
marked the innermost drain for every `break` it walked — including the
`break` of a plain `while` or `for` nested inside the drain body. That loop
returns to the drain, the drain runs to its own condition and empties the
container, and nothing is abandoned; the author was nevertheless told "this
`break` leaves the drain of `tasks` unfinished" and advised to move it after
a `while` it is not in. The missing fact was the loops IN BETWEEN, and it
was already on the tree: every loop the language has pushes exactly one loop
drop mark around its body, so that stack IS the loop nesting.
`loopNestingDepth` reads it, each drain loop records the depth of its own
body, and a `break` counts as leaving the drain only when the innermost
enclosing loop is that drain. `return` is unchanged — it leaves them all —
and `continue` is recorded nowhere at any depth, because it goes back to the
condition and the drain still finishes.

**An exit outlived the pending life it described.** `taskContainerInfo.Exit`
was written once and never cleared. `markTaskContainerConsumed` dropped
`Pending` when a drain emptied the container but left the exit behind, so a
`push` afterwards started a fresh pending life carrying the old life's
statements: the scope-exit refusal pointed at a `break` two loops back — on
a path a later drain had emptied after all — and helped with "finish the
drain first" about a drain that finishes. `ForIn` had the same problem one
channel down, as the note. `forgetPendingLife` ends all three with the
pending life they describe, on both transitions; repeated pushes into a
STILL-pending container are untouched, which is what keeps a genuinely
abandoned drain pointing at its `break`.

Both fixes are pinned at the level that can see them: unit rows in
`TestForInDoesNotConsumeItsElements` and `TestAbandonedDrainIsRefusedAtTheExit`
(red on the unfixed tree with `expected a clean program, got [SEM3107]` and
`SEM3107 points at offset 1247 ("break;"), want 1133 ("tasks.push")`), and
the goldens `sema/valid/task_container_drain_with_nested_loop_break.sg` and
`sema/invalid/concurrency/task_container_pushed_again_after_a_finished_drain.sg`.
The golden corpus records headlines only, so it sees the first defect (a
"valid" case that reports) and the primary span of the second, but NOT the
stale `for` note — that one is asserted in the unit row, which is the reason
the case table grew a `notNoted` column.

The same review found the blocker under all of it: `ff7b7fb2` and `5f493fb2`
added fixtures without regenerating `testdata/golden.expectations.json` and
`6b535a74` then froze it at 5303 against a corpus of 5304, so
`make golden-check` refused the tree before it compiled anything
(`golden preflight rejected frozen corpus: golden entry count is 5304, want
5303`, exit 2). Frozen at 5304 first, and the two new fixtures took it to
5314 across the two fix commits. The generic `@copy` tuple alias divergence
the row called "residue for a row of its own" is now **RV2-DEBT-264**,
measured rather than restated: `Pair<int>` answers `in.IsCopy=true`,
`Result.IsCopyType=false`, `OwnsHeap=false`, and `(int, int)` answers
`in.IsCopy=false`, because `Result.IsCopyType` resolves the alias before
asking and the `@copy` bit lives on the alias.

### 2026-08-27 — RV2-DEBT-263: a task's answer belongs to the moment it commits

RV2-DEBT-261 closed one window behind the `@failfast` symptom (the join read
its two answers apart). The lead's re-measurement with 261 integrated still
showed `TestMTStructuredConcurrency` exit 12/13 in **7 of 20** pinned runs,
so a second, independent window was still open. This is that one. Acceptance
for the pair is 0 of 20.

**What was wrong.** Cancellation was observed only at suspension points whose
target was not already `TASK_DONE`. `rt_task_poll`'s `TASK_DONE` fast path
(`rt_async_task.c:211-235`) answers from the TARGET and never consults the
awaiter's own `cancelled` flag -- deliberately, and correctly, because asking
"am I cancelled?" first would lose a result that is already sitting in the
target. But it means a body whose every remaining await resolves from an
already-DONE child has NO suspension left to see its own cancel at:

```
let slow = spawn async { let _ = spin(200).await(); return 1; };
```

cancel `slow`, its `spin` child is cancelled through `children[]`, `spin`
completes `Cancelled` and WAKES `slow`; `slow` is then polled with its child
already DONE, reads it from the fast path, `let _ =` discards it, and runs on
to `return 1`. `rt_async_return` published `POLL_DONE_SUCCESS`
unconditionally and `mark_done` committed the kind it was handed. Fail-fast
keys solely on the child's committed `result_kind`, so no child ever
committed `Cancelled`, the flag never fired, and the block resolved
`Success`.

**The model already had the answer.** `23-storage-model-and-typed-carrier-
abi.md:477-480`: `cancel` through a live handle is task-global and "before
committed success it requests cancellation observed by every awaited
entitlement; after success is committed it does not revoke already available
independent results." There is a committed-success MOMENT, and it decides.
So the fix is at that moment and nowhere else: `mark_done` chooses the kind,
and a `SUCCESS` carried in with the cancel observed commits `CANCELLED`.

**A WRONG first answer, kept here on purpose.** The lane's first shape of
this fix was a memory-ordering argument that does not hold, and two
independent reviewers found it. It made `cancel_task` store the flag seq-cst
and then load the status seq-cst, and `mark_done` LOAD the flag seq-cst and
then store `TASK_DONE` seq-cst, and claimed the bad pair "cancel saw a live
task, completion saw no cancel" was a cycle in the single total order. It is
not. Sequential consistency forbids only the store-then-load pair on BOTH
sides (Dekker; §8's park race is written that way on both sides). Here the
completion side is load-then-store, and

```
M.load(cancelled)=OPEN  <  C.store(cancelled)  <  C.load(status)=RUNNING  <  M.store(status=DONE)
```

is a valid total order -- indeed it is just the real-time order. The residual
window ran from `:294` to `:316`, and everything `mark_done` still had to do
was inside it: the result-kind store, the deferred `abandoned_state` release,
three owned releases, and a MUTEX acquisition in `release_matching_leases`
(`rt_remote_task_lease.c`). Hundreds of nanoseconds, not "a few
instructions", and it reproduces the very symptom this row is about.

Worse, the lane's gate went GREEN on it -- and that green was the control
lock, not the protocol. `mark_done_needs_control` returns 1 whenever
`done_waiters > 0`, and the external await in `main` keeps that above zero
for the whole program, so completions were being serialized by the control
lane: option (b) below, arriving implicitly and with no guarantee. The window
is open wherever `done_waiters == 0` with no wait_keys, select timers or net
key -- the steady worker path, `rt_worker_turn.c`, takes no lane lock across
`apply_poll_outcome` at all -- and it would be open everywhere if the
`done_waiters` reason were ever removed from `mark_done_needs_control`, which
its own comment names as the goal.

**Which linearization, and why (a).** Two options:

- **(a) one read-modify-write per side on one word.** `task->cancelled`
  becomes a three-state gate: `OPEN`, `REQUESTED`, `SEALED`. `cancel_task`
  does `CAS(OPEN -> REQUESTED)`; `mark_done` does `CAS(OPEN -> SEALED)`.
- **(b) the task's owner-shard lock around both sides.**

**Chose (a), and the proof is not about fences at all.** RMWs on ONE atomic
object are totally ordered by that object's modification order, and a CAS
reads the value written by the modification immediately before it in that
order (C11 5.1.2.4). Of the two CASes, whichever appears first reads `OPEN`
and moves the word; the other then reads `REQUESTED` or `SEALED` and fails.
So at most one succeeds:

- cancel wins -> `mark_done`'s seal fails -> it commits `Cancelled`;
- completion wins -> `cancel_task`'s request fails -> the cancel is refused
  and stops, which is what the storage model says a cancel arriving after
  committed success does.

"Both believed they won" is not a narrow window; it has no execution. That is
the difference from the first shape, and it is why the fix is a CAS rather
than an ordering.

(b) was rejected on cost: a lock acquire and release on EVERY task completion
-- the steady request-completion path Epic 7 spent its whole scope taking off
the control lane -- to serialize two accesses that need an order, not mutual
exclusion. And it is what the broken shape was accidentally relying on, which
is a poor reason to adopt it deliberately.

No new field: the gate reuses `task->cancelled`, and `rt_async_internal.h`
did not grow (666 effective, base 666, and it is over the 500 ceiling so it
may not grow at all -- paid for by `task_cancelled_store`, which only ever
wrote a zero at creation, becoming the three-line `task_cancel_gate_init`).
`REQUESTED` is the only state that means "a cancel is outstanding", so
`task_cancelled_load` keeps the meaning its fourteen readers already had: a
task whose completion sealed the word is committing its answer, not
cancelled. Both transitions live in `rt_task_complete.c`, which owns
`cancel_task` and `mark_done` alike.

`cancel_task` now STOPS when its CAS fails against `SEALED`. That is the
storage model's "after success is committed it does not revoke", and the same
decision the `TASK_DONE` check at the top of the function already makes for a
cancel that arrives later still. The consequence, stated plainly: the gate
closes a few instructions BEFORE `TASK_DONE` becomes visible, so a cancel in
that sliver no longer wakes the task and no longer walks its `children[]`.
That widens by a few instructions a window the `TASK_DONE` early return
already had, and the children of a task that is completing have been drained
by the awaits that got it there. It is refused visibly rather than swallowed,
which is exactly what `debt263-cancel-after-seal` asserts.

**The value IS destroyed at the boundary — the first version of this note was
wrong about that too.** It argued the slot could keep the value because a
`Cancelled` result is never served, having checked only
`rt_far_task_take_result`. It missed the FAR reply path:
`rt_remote_task_on_owner_done` runs later in `mark_done` and calls
`rt_remote_task_pin_result`, which minted a capability naming the slot on the
strength of "is there a value here"; the holder then moves the value out
UNCONDITIONALLY (`finish_retry`, `rt_remote_task_api.c`) while the generated
`Cancelled` arm never reads the storage it lands in
(`emit_crossing_far_task.go`). An obligation dropped on the floor. "Cancelled
with a ready result cell" was a state that did not exist before this lane
(`POLL_DONE_SUCCESS` implies published, `POLL_DONE_CANCELLED` implies not),
and inventing one and auditing its readers was the wrong trade.

So the state is gone again. `rt_task_result_refuse` (`rt_task_lifetime.c`)
empties the slot at the commit, before anything downstream can name it, and
splits the two moments the brief's `rt_release_owned_block_when_unlocked`
could not: `rt_value_cell_hand_off` marks the cell `MOVED` -- "already taken",
a state every reader answers -- and hands back the storage plus whether the
cell OWNED it. An owning cell's block goes to the existing deferred release,
which drops and frees it. An inline value (anything at or under
`RT_VALUE_CELL_INLINE_BYTES`) lives in the task's own bytes, so it is dropped
in place and never freed, and the deferral PINS the task so it cannot outlive
those bytes; the drain does blocks before task reclaims for that reason. The
destruction waits because the drop is generated code and may not run under a
scheduler lock (rule 8 P2) -- on the steady worker path there is no lock at
all (`rt_worker_turn.c` takes none across `apply_poll_outcome`), so it runs
immediately there. `rt_remote_task_pin_result` also asks the kind first now,
fail-closed behind the invariant rather than in place of it.

**The pre-move fast path in `rt_async_return` was considered and dropped.**
Checking `current_task_cancelled` before the value moves would mean refusing
a value the caller has already given up: generated code emits `unreachable`
straight after the call and never drops it, so the runtime would have to drop
it in place at the caller's storage -- a second, differently shaped ownership
path beside the one the runtime already has, and one that lands right after
`rt_far_task_prepare_return` has handed a far `Task<T>`'s lease back. It buys
nothing the boundary does not already decide. One chokepoint is the point.

**Sync points, two of them, because there are two windows.**
`SP_ASYNC_RETURN_BEFORE_SUCCESS_COMMIT` sits in `rt_async_return`
(`rt_async_poll.c`, inside the `src != NULL` branch, after
`rt_value_cell_commit` and before `poll_result.kind = POLL_DONE_SUCCESS`):
a cancel landing there is BEFORE the commit and must be answered `Cancelled`.
`SP_MARKDONE_AFTER_SEAL_BEFORE_DONE` sits in `mark_done` between the
`result_kind` store and the `TASK_DONE` publication: a cancel landing THERE
has lost, and must be refused rather than swallowed -- that is the residual
window, and holding it open is what the first shape of this fix could not
have survived. `RV2_DEBT_263_NEGATIVE_CONTROL` now restores BOTH pre-fix
sides (the completion commits the kind it was handed however the seal went;
the cancel writes its flag and believes it landed however the CAS went) while
leaving the CASes themselves in place, so it changes only what is BELIEVED
about their results and cannot pass by removing the ordering.

**Rule 13, run on this lane.** The stand `debt263-cancel-commit-boundary`
spawns the child from the DRIVER (`spawn_pinned`; a worker's own spawn lands
on its local tail and a single local entry signals nobody -- the lesson from
`5ca1a131`), has a scope owner adopt it into a `@failfast` scope, releases it
into the window, cancels it there and requires both answers:

- positive, `SURGE_SHARDS=1`, `SURGE_THREADS=2/4/8`: `debt263 cancelled in
  window: cancelled=1 done=0 done_waiters=0`, `debt263 after: child kind=2
  bits=0`, `debt263 after: owner kind=2`, exit 0;
- negative control, same window: `debt263 after: child kind=1 bits=7` and
  `debt263 the task committed Success after a cancel that landed before the
  commit`, exit 1.

The second stand, `debt263-cancel-after-seal`, is the one the review asked
for: it holds a completion at `SP_MARKDONE_AFTER_SEAL_BEFORE_DONE` -- inside
the RESIDUAL window, not before `mark_done` -- and cancels it there.

- positive, 2/4/8: `debt263 seal window: done=0 done_waiters=0`, `debt263
  after seal: cancel believed=0`, `kind=1 bits=9`, exit 0. Both halves of ONE
  answer: the task commits `Success` AND the cancel is visibly REFUSED.
  Asserting only the first would pass on a runtime that swallowed the cancel.
- negative control: `cancel believed=1` beside `kind=1`, failing with
  `debt263 the task committed Success while its canceller believed the cancel
  landed` -- the split brain the old shape allowed.

Both stands assert `done_waiters == 0` at the window, which is the input that
would otherwise force `mark_done` onto the control lane, so neither can be
passing because a lock serialised it -- the exact way the lane's first shape
went green. The seal stand is stronger still: it cancels while a worker is
held INSIDE `mark_done`, and `rt_task_cancel` takes the control lock, so if
`mark_done` held control across that window the row would strand at the sync
point's bounded guard instead of passing. The row existing at all is the
proof the window is control-free.

The program row `TestRuntimeV2TimeoutTargetAnswersCancelledToEveryHandle`
(24 rounds) reported **exit 20 in 9 of 10 runs** with the boundary made inert
-- 5 of 5 at `SURGE_THREADS=1`, 4 of 5 at 4 -- and **10 of 10 green** with
it. It refuses to pass on zero timed-out rounds (exit 21), so a run where the
target beats every deadline says so instead of going green having asked
nothing.

The VM half is pinned by `TestMarkDoneCommitsCancelledForACancelledTask`
(`internal/asyncrt`), red without it with `a cancelled task committed kind 0,
want 1`; its acceptance twin passed in that same red run, so it is not
green-by-absence in the other direction. The VM lane CANNOT witness the
defect in the timeout program -- single-threaded and deterministic, it
re-polls the target while its child is still live, measured green 3 of 3 with
the VM boundary reverted -- so the VM row there is an acceptance row, not the
witness.

**An OWNING value, because an `int` proves nothing about reclamation.** Every
other row here answers with an `int`, which lives inline in the cell and owns
no block (`cell_fits_inline`, threshold 16 bytes), so a refusal that got the
ownership wrong would leak nothing and free nothing twice.
`TestRuntimeV2CancelledOwnedResultValgrindZero` answers with a composite of
two strings -- wider than the inline run, so the slot holds a block it
allocated and two counted payloads inside it -- produced (the body prints
`produced` immediately before its `return`, which is the non-vacuity) and then
refused. Measured: strict zero on the fixed tree; `definitely lost: 200 bytes
in 8 blocks` with `rt_task_result_refuse` handing off and never releasing;
`Invalid free()`, `ERROR SUMMARY: 33 errors from 7 contexts`, with
`rt_value_cell_hand_off` leaving the cell `INITIALIZED` so the refusal and
`reclaim_task` both destroy it. It is in `requiredSanitizerCoverage`.

**Two static rows** pin what a timing test cannot: exactly two CASes on the
gate word with no direct load or store of it in either side
(`StaticCancelGateOneRMWPerSide`), and the far reply asking the kind before
it names a slot (`StaticFarReplyNamesResultOnlyForSuccess`). The first exists
because the broken shape was spelled with the same words as the correct one
and no timing row could be relied on to notice a "simplification" back to it.

**MarkDone hands the refused payload BACK** rather than zeroing it
(`internal/asyncrt/task_complete.go`). The executor is generic over its
payload and cannot destroy one, so a value it silently dropped would be a
value with no owner; the signature makes `refused, ok :=` the only spelling a
caller has. That also aligns the lanes on the MOMENT of destruction -- native
destroys at the commit in `rt_task_result_refuse`, the VM in `runReadyOne` --
so there is no parity gap to record.

**Gate.** Both program rows -- 261's `TestRuntimeV2FailfastJoinAnswersCancelled`
and this one -- are now a recipe line of `runtime-v2-lifecycle-check`. Neither
was run by any target before: they are one defect family ("a cancelled task
answered `Success`"), and rule 13 asks for the row rather than a prefix so
that closing one cannot let the other regress while the gate stays green.

**Not run by this lane** (the lead's): `make runtime-v2-lifecycle-check`,
`go test ./internal/vm`, `make check`, `make golden-check`, the sanitizer
gate, TSan, benchmarks, and the 20 pinned runs that are the acceptance. The
golden corpus
was READ instead of run: every `.sg` under `testdata/golden/vm_async*` and
`vm_async_suite` that calls `.cancel()` or `timeout()` already expects
`Cancelled` where a cancel landed (`async_cancel_child`,
`async_cancel_propagation`, `t07_failfast`, `vm_async_j6_failfast`,
`t10a_timeout_cancel`, `vm_async_j8_timeout_cancel`), and the two that expect
`Success` are the runs where the timer loses and nothing is cancelled.

**Three side defects, filed and NOT fixed:** RV2-DEBT-265 (`rt_scope_join_all`
returns `true` without writing `*failfast` on its two early exits, and the
LLVM caller's `alloca i1` is never initialized -- the `@failfast` tail
branches on undefined memory), RV2-DEBT-266 (`poll_task`'s `cancel_pending`
branch reads `active_children` and frees the scope under the control lane
alone, while the steady child-done mutates both control-free under the pinned
shard lock; the comment on `scope_exit_locked` still describes the old lane),
and RV2-DEBT-267 -- the one this lane owes rather than found: there is no
BEHAVIOURAL row for the far reply path with a cancelled producer. The guards
are in (the slot is emptied at the commit; `pin_result` asks the kind first)
and a static row pins their shape, but a far body is a single call expression,
so "produced, then cancelled across the crossing" needs a program shape the
crossing rows do not have today. Filed with the witness owed rather than
claimed.

**What this lane got wrong, for whoever reads it next.** Two blockers, both
found by review and neither by the lane: a memory-ordering argument that was
simply false (and whose gate went green on the control lock instead), and an
ownership audit that stopped at the first reader it checked. The pattern in
both is the same -- a claim stated more confidently than it was tested. The
protocol is now a CAS whose argument needs no fence reasoning, and the state
whose readers had to be audited no longer exists.

### 2026-08-27 — RV2-DEBT-263 integrates, and the symptom is measured out

The lane was written on `91c2778e` and the trunk had moved twenty-seven
commits past it, so seven of its twelve commits were absent and three of the
absences were interactions rather than merges.

**What the trunk refused.** Two of the lane's e2e programs write
`spawn async { … return … }`, authored before the Q-C lane landed; trunk
answers SEM3207 there, because an async body is its own task and takes `ret`.
The `async fn` bodies keep `return`, which is what the ruling says. The
lane's C was formatted by a different clang-format than this tree's and
`make cfmt-check` refused three hunks. And `channel_owned_element.c`, a C
stand under `internal/vm/testdata`, still called `task_cancelled_store` --
the function the cancel-gate fix replaced with `task_cancel_gate_init`. That
stand is built by `make runtime-v2-carrier-sanitizer-check` and by nothing
else, so `make check` was green over a stand that did not compile; the
sanitizer gate is what found it.

**The measurement.** The acceptance the lane owed was recorded as
`ok surge/internal/vm 250.520s` -- the package, not the row -- and its sibling
log contains no `MTStructuredConcurrency` at all. Both rows skip themselves
when `clang` is absent and when `SURGE_SKIP_TIMEOUT_TESTS` is anything but
`0`/`false`, so that line is indistinguishable from twenty skips. It was
re-taken with the row named, `-v`, `SURGE_SKIP_TIMEOUT_TESTS=0`, and a harness
that counts a round as VACUOUS when either row is missing or skipped from the
output:

```
SURGE_SKIP_TIMEOUT_TESTS=0 SURGE_BACKEND=llvm SURGE_STDLIB=$(pwd) \
  taskset -c 4-31 go test ./internal/vm \
  -run '^(TestMTStructuredConcurrency|TestRuntimeV2FailfastJoinAnswersCancelled)$' \
  -count=1 -v --timeout 600s
```

At `691ae487`, with RV2-DEBT-261 in and 263 out: `TestMTStructuredConcurrency`
red 5 of 20 (exit 12, 13, 22), `TestRuntimeV2FailfastJoinAnswersCancelled` red
7 of 20 (exit 12, 13), 12 of 20 rounds carrying at least one red, 0 vacuous.
At `b07851d1`, with 263 in: 0 of 20 and 0 of 20, 0 vacuous. The before reading
is the negative control, taken on the same machine with the same command an
hour earlier, not remembered from another tree.

**The ledger.** Row 266 -- the cancel-pending scope teardown reading and
freeing a scope under the control lane alone -- is RV2-DEBT-280's site (1) and
RV2-DEBT-283's free, found twice: here while deriving 263, and again a day
later by the multi-carrier planes review, which could not have seen 266
because it lived only on the lane. All three rows stay, each says so, and the
rule is that whichever lane serializes that teardown closes all three; no lane
may claim one alone.

`make runtime-v2-carrier-check` is red and was red on `691ae487` before the
lane (a stale `blocking-composite/main.sg` digest and frozen topology counts),
so it is not this lane's and is not counted against it.

### 2026-08-27 — the dedicated machine reopens what the working machine closed

The RV2-DEBT-263 acceptance above was taken on the working machine and read as
a close. The dedicated runner disagreed within the hour: `make
runtime-v2-lifecycle-check` under `taskset -c 8-15` on `surge-bm` at
`d4d1b3eb` reported

```
runtime_v2_failfast_join_e2e_test.go:121: llvm SURGE_THREADS=4: exit=12 --
  the first @failfast block (fast cancelled, slow cancelled by fail-fast)
  resolved Success
```

which is the symptom RV2-DEBT-261 and RV2-DEBT-263 both exist to remove.

The interesting part is not that a second machine differs. It is that on that
same machine the two rows RUN ALONE are 0 red in 20 pinned rounds -- the same
harness, the same isolated cores, the same commit. What separates the red from
the green is the rest of the lifecycle gate running beside them. So neither
"it reproduces on my machine" nor "the row is green twenty times" is the
question to ask: the row has to be repeated UNDER THE GATE, which is how CI
runs it and how it went red.

Rows 261 and 263 are reopened with both readings recorded, because a ledger
that says Closed over a red gate is worse than one that says nothing. The
first candidate for the third window is RV2-DEBT-265: `rt_scope_join_all` has
two early exits that answer "drained" without writing `*failfast`, and the
LLVM emitter reserves that flag with `alloca i1` and never initialises it, so
the generated tail of a `@failfast` block branches on whatever the frame held.
Its wrong-default reading is `Success`. That is undefined behaviour whose
outcome can differ between two machines for no reason the source can name --
exactly the shape of what was just observed.

### 2026-08-27 — 400 runs on the dedicated machine: 265 is not the window, and a new panic

The instrument was chosen to be the command that actually went red, not the
row in isolation: the second line of `runtime-v2-lifecycle-check`, which runs
the two cancellation-answer program rows with `-parallel=1 -p=1`. It costs
seven seconds, so it can be repeated enough times to say something. The full
gate was tried first and gave 0 red in 10 runs at two minutes each -- true and
useless.

```
SURGE_STDLIB=$(pwd) SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
  taskset -c 8-15 go test ./internal/vm \
  -run '^TestRuntimeV2(FailfastJoinAnswersCancelled|TimeoutTargetAnswersCancelledToEveryHandle)$' \
  -count=1 -parallel=1 -p=1 -v --timeout 300s
```

`d4d1b3eb`, before RV2-DEBT-265: 3 red of 200, 0 vacuous.
`082ffae9`, after it: 1 red of 200, 0 vacuous.

Both remaining reds are `SURGE_THREADS=4: exit=12` on the fail-fast row, on
the `llvm` lane; the VM lane is green in all 400. Two of 200 against one of
200 is noise at this count, so **RV2-DEBT-265 is not the window**. Its fix
stands on its own -- the undefined behaviour is real and the emitter test is
red on the revert -- but it does not get to claim this symptom. The residue is
alive at roughly half a percent, and the next lane has to find a fourth
window rather than assume the third was it.

The third red is a different defect and now has its own row. At `d4d1b3eb`,
`TestRuntimeV2TimeoutTargetAnswersCancelledToEveryHandle/llvm/threads-4`:
`exit=1 -- the program did not run to its verdict (dur=1.03647ms)`, stdout
empty, stderr `panic: async: task slot out of range`. A millisecond in, so
nothing had run yet; `rt_task_slot_store` found the task table's segment still
`NULL` at an id whose creator had just been told the segment was there. That
is RV2-DEBT-291.

What this pass is really worth recording for: a row that is green twenty times
on the working machine is not a closed row, and a row that is green twenty
times on the DEDICATED machine is not one either -- the same two rows run alone
on `surge-bm` are 0 of 20. Only repeating the command the gate runs, enough
times to see a half-percent event, said anything at all.

### 2026-08-27 — half B of the rulings is drafted, reviewed, and refused

Р2, Р5, Р7 and Р8 were written as normative edits by four authors, each
reading the whole document and the code its ruling touches, and each draft was
then attacked by two independent lenses: one asking whether the text says
exactly what the owner ruled, one checking every claim it makes about the tree
by opening the file. Eight verdicts, none accepting. That is the pass working,
not failing: what it caught is the same failure four times over.

**Every draft settled a fork the ruling left open.** Р2's author picked a
single monotonic base for all shards (the ruling says monotonic wall time, not
how many bases), fixed the clock the already-normative 1 ms service budget is
measured on (Р2 does not touch that budget), and defined "sleep semantics" --
the ruling's load-bearing phrase -- and stated the definition as settled. Р5's
author added a scope limit the owner never ruled ("the promise covers one
channel"), and closed with "that is the only freedom this order allows", an
exhaustiveness claim its own open questions contradict twice. Р7's author
invented the fallback's mechanism and settled span granularity as size classes,
which RV2-DEBT-286 already records as an unanswered part of Р7. Р8's author
classified debug invariants as reporting, a classification the ruling does not
make, and enumerated the reporting switches wrongly.

**Two of the four cannot be written as description at all, because the tree
does not do what the ruling says.**

Р2: the runtime does not run on monotonic wall time. `rt_clock_now` reads
`ex->now_ms`, a virtual clock (`rt_async_internal.h:403-411`, commented
"Virtual clock (D7)"), `rt_clock_tick` hands out a millisecond nobody waited
for on every yielded poll, and `rt_clock_advance_to_next_deadline`
(`rt_async_sleep.c:179-202`) jumps straight to the next deadline when the wall
bound is inactive. That is RV2-DEBT-180, already open and already
blocker-class. Р2 is therefore an instruction to close 180, not a paragraph to
add -- and on the VM side it inverts a default, since `surge run --real-time`
opts INTO wall time today.

Р5: the channel orders admission, not commit. `channel_stage_locked` releases
the owner lane across the element move (`rt_async_channel.c:232-234`), so two
senders admitted A then B can commit B then A whenever B's element is cheaper
to move. Writing Р5's promise into Section 7 today would state a guarantee the
runtime does not keep. That is RV2-DEBT-298, opened here.

So half B does not land. The four rulings are recorded with the forks each one
leaves open, and the questions go back to the owner rather than being answered
in prose by whoever happened to be typing.

## Writing a lifecycle stand whose owner is held (the 261 trap, tooled)

**The defect class.** A stand that holds its owner inside its own poll -- at a
sync point, or on any wait that does not return to the scheduler -- must not
create its child with `__task_create` from that poll. That push lands on the
held worker's LOCAL deque and one local entry signals nobody
(`signal_ready_now = signal_ready && local->len > 1`, `rt_ready_queue.c`); the
other workers sit in `pthread_cond_wait` with `wake_pending == 0`
(`rt_worker_turn.c`) and stealing needs an awake thief. The child never runs,
so its cancellation is never delivered and the stand hangs to its timeout
blaming the runtime. The local queue being the pusher's own path is the model.

**The tool.** `lifecycleHarnessStandHelpers`
(`internal/vm/runtime_v2_lifecycle_stand_helper_test.go`): `spawn_child_for_stand`
spawns through the inject queue with the ready signal and refuses on a worker
thread; `stand_require_child_running` proves a worker took the child -- it
watches `(TASK_READY, enqueued=1)`, the pair the push writes and only a pop
undoes -- or prints "stand: child never ran -- spawned from a held poll?" so
the stand fails in seconds with the real culprit named.

**How to write one.** Driver spawns the child, waits for
`stand_require_child_running`, then spawns the owner; the owner's poll only
calls `rt_scope_register_child` on the live task. The falsifier is
`stand-helper-held-poll-trap` (`TestRuntimeV2LifecycleStandHelperHeldPollTrap`,
wired into `runtime-v2-lifecycle-check`): it walks into the trap on purpose and
must fail with that sentence, while a driver-spawned control child runs under
the same hold. `-DRV2_STAND_HELPER_NEGATIVE_CONTROL` removes the helper from
that stand and it spends the full 4s budget to report "cancelled child never
completed" -- the 261 sentence -- instead.

## P3's three missing deterministic rows — 2026-08-28

The record lives in §4.1 of `23b-wave-d-execution-plan.md`, under D4b, written
the way P2's was. This entry exists so the log points at it: a vertical whose
closeout is written in one file and nowhere else is how this one went unnoticed
for three days.

**What was missing.** P3 names four deterministic sync-point rows. Only the
clone-reader-versus-last-await pair existed (`SP_AWAIT_AFTER_INCREMENT`,
`SP_AWAIT_BEFORE_DONECV_WAIT`). The enum declared no hook for shutdown versus a
claimed clone, for cancel versus `READY`, or for stale-generation late
publication — verified against the whole of `rt_sync_point.h`, not against the
previous status line.

**Why no existing stand could have caught it.** Every other lifecycle stand
binds its task result to the opaque machine word, and a word owns nothing: its
take is a COPY that leaves the slot alone. The entitlement machinery — clone,
move, refuse, the reader count, the reserved mover — is only reached by a result
that OWNS something. The new stands are the first with such a result
(`internal/vm/runtime_v2_task_entitlement_stand_test.go`), which is the general
lesson: a machine-word fixture cannot exercise an owning-value contract, however
many rows are pointed at it.

**The three hooks**, each named for the behaviour it holds open, in
`rt_sync_point.{h,c}` and in `check_sync_points.sh`'s window map:
`SP_CLONE_READER_OUT_OF_LOCK` (`rt_task_entitlement.c`) is the only instant a
claim is out — counted into `clone_readers`, no lock held, duplicating the
canonical value where it lies, and two rows race against it;
`SP_CANCEL_AT_COMMITTED_RESULT` (`rt_task_complete.c`) is armed by nothing and
exists for its reached count, which is what keeps the cancel row from being
vacuous; `SP_RESULT_CAPABILITY_BEFORE_MATCH` (`rt_task_result.c`) is the moment
a capability is about to ask a slot whether it still holds the occupant it named.

**The controls, and what each removes.**
`RV2_SHUTDOWN_UNPINNED_CANONICAL_NEGATIVE_CONTROL` reads "shutdown drops any
canonical result" literally and destroys the value on the first release after
shutdown; `RV2_CANCEL_REVOKES_COMMITTED_RESULT_NEGATIVE_CONTROL` lets a cancel
that arrives after the answer is committed empty the slot;
`RV2_STALE_RESULT_GENERATION_NEGATIVE_CONTROL` drops the generation from the
capability match. Each leaves the window intact and changes one guard, so none
can pass by removing the ordering its proof is built on, and each fails in
seconds at its own window with its own sentence.

**Still owed, and recorded rather than left to be rediscovered.** RV2-DEBT-304:
P3's rollback failpoint cannot be written against today's ABI at all, because
`rt_value_clone_init_fn` returns `void` and §3 of the storage model makes a
status return the condition for being rollback-capable. RV2-DEBT-305: the two
bounded child-process controls do not exist and the lifecycle harness has no
child process to build them in.

## The channel's two internal pins, and a teardown in the document's order — 2026-08-28

**What was there.** `rt_channel_pin` / `rt_channel_unpin` existed, were declared,
and had ZERO callers, so `ch->pins` was provably zero and the pin leg of the
fail-closed reclaim assertion refused nothing it could not already refuse
through the handle count. Alongside it, `rt_channel_free` performed none of
section 7's prescribed teardown order: it took no owner lock (and refused to run
under one), carried no dying mark, and interleaved detach with drop slot by slot
through `rt_typed_fifo_drain` / `rt_park_pool_drain`. Only "free the storage"
happened where the document puts it. And the channel's registrations on its two
waiter keys were never removed at reclaim: a debug-only counter looked at them
and PRINTED.

**The three holders, wired.** Section 7 names a registered waiter, a select
subscription, and a claimed detached operation.

- Registered waiter — `channel_park_prepare_locked`'s APPEND arm pins
  (`rt_channel_lane.h`). The dedupe arm does not: it rewrites an entry that
  already holds one.
- Select subscription — `add_waiter`'s generic arm pins (`rt_async_waiter.c`).
- Both retire through the same two removals: `channel_pop_candidate_locked` (one
  entry) and `remove_waiter_from_store_seq` (`removed` entries).
  `wake_key_all_with_policy`'s non-join arm retires too — inert, since no caller
  passes it a channel key, and present so the retire side is complete by
  enumeration rather than by an argument about callers.
- Claimed detached operation — one pin for the whole operation, at
  `rt_channel_send` / `rt_channel_send_yield` / `rt_channel_recv`
  (`rt_async_channel.c`) and `rt_channel_try_send` / `rt_channel_try_recv` /
  `rt_channel_send_blocking` / `rt_channel_recv_blocking` / `rt_channel_close`
  (`rt_channel_sync.c`).

**Why the operation and not the window.** The nine windows where the owner shard
lock is released across generated code are: `channel_stage_locked`'s move into a
park slot and the buffered push beside it (`rt_async_channel.c`); the four takes
in the receive loop — a delivered value, the ring's head, a same-shard parked
sender's slot, a foreign one's; `channel_end_park_locked`'s drop and
`channel_stage_into_ring_locked`'s move (`rt_channel_lane.h`); and
`channel_wake_only` / `channel_deliver_foreign` /
`channel_claim_foreign_sender_locked`, which release the owner lock to take a
peer's. A pin per window would have to be paired down a dozen early returns in
`rt_channel_send_inner` alone, and the first one missed is either a channel that
leaks forever or exactly the free the pin exists to stop. One pin per operation
covers all nine by construction.

**The two holders compose, and that is load-bearing.** A pop retires a waiter's
pin under the owner lock, and the operation that popped it keeps using the
channel afterwards on its own hold. A reclaim set off under a lock is deferred
by the lane's existing queue, so the pop is safe where it stands.

**The negative row, and its numbers.**
`internal/vm/testdata/channel_handle_refcount.c` mode `waiter-pin`
(`registration-refuses-the-reclaim`): three values buffered, a subscription
registered through `add_waiter`, then the last handle dropped.

- fixed: `waiter pin: sent=3 drops_while_registered=0 reclaimed_drops=3 bad=0`
- `-DRV2_CHANNEL_PIN_NEGATIVE_CONTROL`: `drops_while_registered=3` — the reclaim
  runs under the live registration, and the row fails with both figures side by
  side.
- the same negative build under ASan: `heap-use-after-free ... READ of size 4`
  at `rt_channel_unpin` (`rt_channel_refcount.c:100`) via
  `rt_channel_key_retired` <- `remove_waiter_from_store_seq`, 468 bytes into an
  816-byte region freed by `rt_channel_free`.

The pin got its OWN control rather than riding on
`RV2_DEBT_155_NEGATIVE_CONTROL`: that one also makes the handle count
non-atomic, which is a second thing removed and would make a multi-threaded
harness's outcome depend on whether an update was lost. Each control now removes
exactly one defence.

**Teardown, in the document's order.** `rt_channel_free` moved to
`rt_channel_refcount.c` (next to the counts that decide when it runs; also what
bought the room, since `rt_async_channel.c` was at exactly 500 effective LOC and
is now 457) and now does: take the owner lock; assert quiescence; mark dying;
`rt_typed_fifo_detach_all_locked` + `rt_park_pool_detach_all_locked`; release the
lock; `rt_typed_fifo_drop_detached` + `rt_park_pool_drop_detached`; free the
storage. The detach halves are new and run NO element operation — the fifo's
records `{head, len}` because a ring's occupied cells are contiguous, so the
teardown needs no allocation to remember them; the pool's leaves the initialized
headers as its record and empties the free list so nothing can acquire. The
existing `_drain` entry points are untouched, because the slot-control stands
call them on pools they keep using.

**The registration check now REFUSES.** With a registration holding a pin,
reaching `rt_channel_free` with one means the counts disagree with the store they
summarise. `rt_channel_assert_reclaimable` became
`rt_channel_assert_reclaimable_locked(owner, channel)` and panics there; it reads
the store under the lock the caller already holds, which is also the only place
the count and the pins are consistent with each other.

**A negative control that stopped reproducing — caught by the gate, not by
reasoning.** `RV2_DEBT_199_NEGATIVE_CONTROL` restores the dereference of a
channel key during routing and MUST be observed as an ASan use-after-free. A
store entry now holds a pin, so the object is alive when the deferred stale-key
removal routes it, and `TestRuntimeV2FreedChannelWaiterRouting` failed with
`negative control exited cleanly; the proof is vacuous`. Fixed by building that
harness with BOTH controls: the pin control removes the hold, 199 restores the
dereference, and together they are the pre-fix world. The 199 FIX is unchanged
and still needed — the routing path is reached with a key a caller merely
CARRIES, which is a copy nothing counts. Both rows green again.

**A leak the change exposed rather than created.** The deferred-reclaim queue is
`_Thread_local` and its backing array was never freed, so any thread that
reached a last release and then exited left it behind. Before, only lanes that
outlive the process got there; the unpin in `remove_waiter_from_store_seq` put a
short-lived cancel thread on that path, and LeakSanitizer named it:
`Direct leak of 32 byte(s) in 1 object(s)` from `rt_channel_free_when_unlocked`
on `freed_waiter_cancel_thread`. `rt_channel_reclaim_drain` now gives the array
back whenever the queue empties — one allocation per batch of deferred reclaims,
against a leak per thread.

**Gates.** `make runtime-v2-carrier-sanitizer-check` exit 0 before and after.
`make c-check-changed` exit 0 over every touched C/H file.
`make runtime-v2-slot-control-check` exit 0. `make runtime-v2-waiter-check`
exit 0 (after both fixes above).

**PRE-EXISTING RED, not this lane's.** `make runtime-v2-lifecycle-check` is red
on `a6acc31c` itself: `duplicate case value '4042'` when the lifecycle harness is
assembled, from `POLL_SCOPE_MEMBERSHIP_OWNER 4042`
(`internal/vm/runtime_v2_scope_membership_claim_test.go`, commit `0d28f29d`) and
`POLL_ENTITLEMENT_OWNING_RESULT 4042`
(`internal/vm/runtime_v2_task_entitlement_stand_test.go`, commit `53fa7ca4`) —
two lanes that landed the same day and picked the same poll id. 33 rows fail to
BUILD; 35 pass. Measured by stashing this lane's diff and running
`TestRuntimeV2LifecycleDebt080CancelBeforeClaimProof` on the bare commit: same
error. `TestRuntimeV2LifecycleShutdownWithParkedTasks`, the row that would notice
a registration pin leaking a channel at shutdown, builds and PASSES with this
change.

## Channel lifetime — answering the adversarial review (2026-08-28)

Four findings, all conceded; nothing in the review's "Refuted" list survived as
written except one geographic detail.

**The teardown half shipped with no failing test, and now has one.** The review
reverted `rt_channel_free` to the pre-lane body and every census row printed
byte-identically — because every row counts what was DESTROYED, and both orders
destroy the same three values. The order is only visible from INSIDE it, so the
element's own `drop_in_place` now records three things and a fifth row states
them: `teardown order: drops=3 sealed_at_drop=3 still_attached=0
locked_at_drop=0`. Rebuilt against the pre-lane body — `rt_typed_fifo_drain` +
`rt_park_pool_drain`, no owner lock, no mark, no detach — the same build prints
`drops=3 sealed_at_drop=0 still_attached=2 locked_at_drop=0` and the row's
want-substring misses. `still_attached` is the maximum any single drop still
found in the ring or the park pool, which is the clause that matters: a drop is
user code that may re-enter the runtime and must not find a half-emptied object.
`locked_at_drop` CONFIRMS rather than discriminates —
`rt_value_drop_in_place_detached` already refuses to dispatch under a scheduler
lock — and the row's comment says so rather than claiming three proofs where
there are two.

**`dying` was a load-then-store pair, not a gate.** `rt_channel_pin` read
`dying` and then added to `pins`; `rt_channel_free` read `pins` and then stored
`dying`. Two objects, load-then-store against store-then-load: both sides can
read zero, so a pin could land in storage a reclaim was already freeing. This is
the shape `rt_scope_membership.h` was written about. The two words are now ONE —
`rt_channel::pin_state`, seal bit in bit 31, count below it — and each side moves
it with ONE read-modify-write: `channel_pin_admit` compare-exchanges the count
up only while the seal is clear, and `rt_channel_seal_reclaimable_locked`
compare-exchanges `0 -> DYING`, so a CAS that fails IS the "still pinned"
refusal rather than a separate check before it. Modification order on one object
makes exactly one of them first. Cost: one uncontended CAS per pin.

**The header enumerated pin sites that do not exist.** It listed
`rt_channel_release_payload` and "the claim lanes" as taking a pin; neither did.
The list is now the seven entry points that actually call `rt_channel_pin`, plus
an explicit NOT-here paragraph for the five that do not — and those five stopped
being an unmarked hole: `rt_channel_assert_pinned` fails closed on the caller's
pin in all four `*_locked` claim/finish helpers and in
`rt_channel_release_payload`.

**The select slow lane is pinned; the LOC reason for deferring it was wrong.**
The lane reported `rt_far_channel_select.c` at 521 "effective" LOC — that is
`wc -l`; `scripts/effective_loc.awk` says 390. The bracket is not in that file
at all (it has no `rt_channel_claim_*` call); it is in `rt_async_select.c`, 417
effective before and 423 after, against a ceiling of 500. `rt_select_poll` now
pins before the claim on both channel arms and unpins after the finish — for a
winning recv arm, after `rt_channel_release_payload`, which reads the channel's
descriptor with no lock held. Proven by removal, not by argument: with the recv
arm's pin deleted,
`go test ./internal/vm -run TestRuntimeV2SelectReleasesAStringPayloadExactlyOnce`
fails with `panic: async: a claimed channel operation ran without a pin`; it
passes in 20.52s with the pin.

**Corrections to the lane's own report.** The valgrind `possibly lost: 304 bytes
in 1 blocks` figure is not "reproducible 3/3" as a property of the code: the
review saw it in 2 of 19 runs, this worktree saw it in 15 of 15. The block is
the blocking worker's pthread TLS from `ensure_exec`, outside what the row
asserts (definite + indirect at strict zero). `rt_channel_refcount.c` was 200
effective LOC, not 201. Three Go TEST files were in the diff; the accurate claim
is that no file under `internal/backend/llvm/` was opened. And the `STATS.md`
hunk's Go delta (918 files/199138 lines -> 917/198578) was not this lane's — the
file was stale at the base and `scripts/pre-commit` regenerated it; only the C
delta belongs here.

**Global Rule 16 earned its keep, again.** The new caller-holds-a-pin contract is
a protocol change, and a stand encoded the old rule: `channel_probe_take`
(`runtime_v2_remote_publication_harness_common_test.go`) called
`rt_channel_release_payload` after `rt_channel_try_recv` had already given its
own pin back, so the release read the descriptor holding nothing.
`TestRuntimeV2RemoteSelectAbandonEdges` went red at 4.79s in the serialized
tagged sweep and nowhere in the gates this lane had opened. The probe takes the
spanning pin now — the same hold the select slow lane takes around its own
claim/move/release — and the row passes in 4.73s, nine subtests.

**Two transport subtests are red and were red before this.** `make
runtime-v2-transport-check` stops at `TestRuntimeV2RemoteTaskBehavior`:
`result-owner-release` and `caller-abandon-drops-landed-result`, both `code=1`,
`runtime_v2_remote_task_behavior_test.go:478`. Measured on `bf8501e9` with this
work stashed: the same two subtests, the same line, the same code. Every other
row of that gate passes, including the whole remote-task acceptance line
(15 rows) and the three lines after it (88.7s).

**A hang that was the measurement's own fault.**
`TestRuntimeV2ChannelHandleValgrindZero/async_float_param` timed out at 180s
while a second valgrind batch of mine was running on the same machine; alone it
is 9.61s and the five-row set is 47.30s. Two concurrent valgrind batches are not
a measurement of anything.

**Still open, and named rather than fixed.** Nothing sweeps channel-key
registrations at teardown, so a process exiting with a task parked on a channel
leaves that pin unretired and the object unfreed; nothing in the corpus reaches
it. `rt_channel_free` calls `ensure_exec()` unconditionally now, because it needs
the owner shard to lock — a channel reclaimed on a short-lived thread can lazily
spin an executor up. And no carrier benchmark was run for the compare-and-swap
the pin now costs.

## A refused allocation reports the type instead of faulting — 2026-08-28

The owner ruling written into §3 of `23-storage-model-and-typed-carrier-abi.md`
said a local duplication that cannot get memory is FATAL, and named the reason
the ruling was not already implemented: `rt_alloc` answers `NULL` on refusal
(`runtime/native/rt_alloc.c:47,87,93`) and the generated bodies stored through
that answer. So the behaviour was not "fatal"; it was a segmentation fault at an
address the program never chose.

**Seven emitted allocations had no test.** `emit_aggregate_ops.go`
(runtime-owned storage — the suspension frame and a blocking body's captures),
`emit_literals.go` (an array literal's element buffer AND its header),
`emit_intrinsics_default.go` (the header a `default` array answers with),
`emit_intrinsics_error.go` (an error-like value), and `emit_iter.go` (the Range
duplication a `for` makes, and the array cursor). All seven now go through
`emitCheckedAlloc` in `internal/backend/llvm/emit_alloc_guard.go`: the emitted
code tests the pointer and, on `NULL`, calls the runtime's own `rt_panic` with
`out of memory: could not allocate <type>` and `unreachable`.

**Two allocations deliberately do NOT go through it.** The `rt_alloc` intrinsic
(`emit_intrinsics_memory.go`) hands the program the allocator's own answer,
which §5 keeps nullable; panicking there would take the answer away. The async
ref-parameter box (`emit_func.go`) already tests its allocation and traps — it
stops the process, so it is not a hole, but it reports nothing, and its stand
lives in a file owned by another lane.

**The message could not use the module's string table.** That table hands out
globals by index over its sorted contents and must be complete before the first
body names one. The per-type sentence goes through the lazily filled table in
`emit_span.go` instead — and NOT through `spanConstOrder`, which is also the
trace string table the backtrace maps index by position. The first writing put
it there and gave the walker a row nothing names; `messageConstOrder` is the
separation, and `TestTheRefusalMessageIsNotInTheTraceStringTable` pins it.

**Proving it needed a negative-control BUILD.** No program can make a real
allocator refuse. `SURGE_INTERNAL_TEST_ALLOC_REFUSAL=<site>` makes the compiler
ask for 2^64-1 bytes at exactly one named site; nothing else differs, no
allocator is stubbed, and the refusal travels the real `rt_alloc` → `NULL` →
guard path. Measured, `array-literal-elements` and `runtime-owned-storage`:

- before the guard, both armed builds were killed by SIGSEGV, stderr empty
  (`Segmentation fault (core dumped)`; Go's `ExitCode()` reports -1);
- after, `panic: out of memory: could not allocate Array<int>` and
  `panic: out of memory: could not allocate __AsyncState$add`, exit 1.

**The VM lane has no such hole.** Its `rt_alloc` is `handleRtAlloc`
(`internal/vm/intrinsic_mem.go:29`) reaching `rawAlloc`
(`internal/vm/raw_memory.go:29`), whose storage is `make([]byte, size)` — a Go
allocation that cannot answer nil, and whose exhaustion aborts the process
rather than handing back a pointer. Nothing else in the VM allocates a Surge
value through a nullable C allocator.

**Gates.** The rows ride `runtime-v2-panic-surface-check`: a source census that
refuses a new untested `call ptr @rt_alloc`, a roster/call-site agreement check,
the emitted-shape rows, and the native negative control at
`SURGE_SKIP_TIMEOUT_TESTS=0`. The panic ledger gains one row —
`emit_alloc_guard.go::emitAllocRefusalPanic#1`, `PG-ALLOC-FAILURE` — and that
group's reason now records that one of its rows is provoked by a build.

## The predicate was the spelling, and the spelling missed `a.push(7)` — 2026-08-29

The pass above guarded the sites spelled `rt_alloc`. That is the wrong
predicate, and an adversarial review showed the consequence in generated IR:
`a.push(7)` emits `call ptr @rt_realloc`, stores the answer into the array
header untested, records the grown capacity over it, and then writes the element
through the null. The census could not see it either — it matched the text
`call ptr @rt_alloc(`, which `call ptr @rt_realloc(` does not contain — so two
live holes passed a gate that reported coverage.

**The predicate is now "can this call answer NULL", and the roster is
`builtins.go`.** Every ABI declaration with `ret: "ptr"` is classified in
`runtimePointerAnswers` (`emit_alloc_guard_test.go`) as one of: the entry point
reports a refusal itself, its NULL is not a refusal, the generated code tests it,
or it answers NULL untested. 112 declarations, all classified. The roster is the
ABI list rather than the emitters because an entry point reached through the
ORDINARY CALL PATH is written by no emitter at all — which is exactly how the
three open-ended `Range` constructors stayed invisible to a scan of emitter text.

**What is now tested.** `rt_realloc` at both callers (`emitArrayPush`,
`emitArrayReserve`) through `emitCheckedRealloc`, which reports BEFORE the header
records the answer: a refused reallocation releases nothing (`rt_alloc.c`), so
the old block is still the array's and the only pointer to it is the one about to
be overwritten. A guard that read NULL as "the buffer moved" would leak the block
as well. `rt_range_int_new` through `emitCheckedRangeNew` for `a..b` AND through
`emitRuntimeAnswerTest` for `[a..b]`, which is a different lowering to the same
symbol; the three open-ended siblings through that same hook in `emitCallSite`,
keyed on `runtimeAnswersTestedAtTheCallSite`. Both range paths now report the
same sentence: `emitCheckedRangeNew` used to hand its type LABEL to the reporter
as the whole message, so a refused `0..3` said `panic: Range<int>` — 10 bytes, no
reason — beside a 44-byte `out of memory: could not allocate Range<int>` for the
same object down the other path.

**The review's ordering finding is closed at the producer, not at the consumer —
but the producer list was wrong, and a measurement refuted it.**
`emitRangeIterInit` sizes its cursor by reading the source Range's kind byte
before its own guard runs, so the guard has to be at every producer. The list
written here on 2026-08-29 named four and there are five: a BRACKETED bounded
literal `[a..b]` is not lowered by `emitBinary` at all —
`internal/hir/lower_expr_range.go` lowers it to an ordinary call to the SAME
`rt_range_int_new`, which reaches `emitCallSite`, where the hook did not name it.
`let r = [1..3]; for i in r` emitted an untested call and then loaded the kind
byte at offset 19 through the answer. The argument was right in form and wrong in
fact because it enumerated the emitters, which is the same mistake the roster was
moved to `builtins.go` to stop making. Closed by the one line the hook was
missing; the list is no longer written down beside the load, it is derived in
`TestATestedAnswerIsGuardedOnEveryPathThatReachesIt`.

**The string family answers NULL and is a DIFFERENT failure, so it gets a
different answer.** It was not a fault: the readers answer NULL/0 for a handle
that is not there, so the program carried on with a string that does not exist.
Measured on the unfixed tree with `"x" * 9223372036854775807`: exit 0, stderr
empty — it does not report, and `s == ""` is false too, so the program can tell
neither that it got the string nor that it did not. The report is in the RUNTIME
(`string_alloc_or_report`, `rt_string.c`) rather than at each emitted call:
twelve entry points reach those two allocations, several indirectly
(`rt_string_from_int` → `rt_string_from_bytes`), `string` is not a result type so
no caller can be handed a refusal it has no way to represent, and the same
function already treats a length it cannot represent as fatal. `rt_readline`
already did exactly this with the same answer. `rt_string.c` 510 → 509 effective
lines, so the over-limit file did not grow.

**Numbers.** Without the realloc guard, `array_grow_push` and
`array_grow_reserve` exit -1 with stderr empty (the SIGSEGV), and the shape row
reports `the rt_realloc at line 493 is not tested; next line is "store ptr %t27,
ptr %t23"`. Without the Range guards the shape row reports the same for
`rt_range_int_new` (line 713) and `rt_range_int_from_start` (line 849). Without
the string fix, `TestARefusedStringReportsInsteadOfAnsweringTheEmptyString` exits
0 instead of 1. `make check` 0, `make golden-check` 0 with no golden status
change, `runtime-v2-panic-surface-check` 0, `runtime-v2-heap-check` 0 (91 rows),
`runtime-v2-carrier-check` 0 with `allocation_count=8` unmoved.

**Numbers for the bracketed-range hole and the wrong sentence.** Both rows were
run red on the tree that had the four-producer argument in it.
`TestAGuardedAllocationIsTestedAndReportsItsType/bracketed_range_literals`
reported `the rt_range_int_new at line 510 is not tested; next line is "  store
ptr %t12, ptr %l4, align 8"`; `.../operator_range_and_array_growth` reported
`a refused rt_range_int_new reports "Range<int>"; every refusal reads "out of
memory: could not allocate " plus the type`; and
`TestATestedAnswerIsGuardedOnEveryPathThatReachesIt` reported `rt_range_int_new
answers NULL on refusal, no intrinsic emitter claims it, and it is not in
runtimeAnswersTestedAtTheCallSite`. Green afterwards, 10 emitter rows in 2.15s,
and both range paths now carry the same 44-byte constant. `make check` 0 in
2m13s, `make golden-check` 0 in 8m55s with `git status` over `testdata/golden`
empty, `runtime-v2-panic-surface-check` 0 in 54.5s with the four refusal rows at
11.81s / 11.07s / 11.30s / 10.79s.

**The open surface is 31 entry points and it is pinned as a number.** The
filesystem result (17), the socket result (9), and five blocks that carry their
own answer (`rt_entropy_bytes`, `rt_argv`, `rt_heap_stats`,
`rt_string_bytes_view`, `rt_term_read_event`). Each answers NULL when its tagged
result block is refused, and the generated code stores it into the Result slot
whose discriminant the match then reads through it. `untestedRuntimeAnswers`
holds the count so that adding one is a deliberate edit and closing a family
moves it down.

**A SECOND open surface is 25 entry points, and it was hiding inside the
"reported" class.** The tagged bignum arithmetic was recorded as reporting on a
reason that is true of the RESULT block and false of the promotion in front of
it: `bi_promote` calls `bi_from_i64(fixi_value(w), NULL)` and `bu_promote` calls
`bu_from_u64(fixu_value(w), NULL)` (`rt_bignum_api.c`), and `bi_alloc`/`bu_alloc`
only record `BN_ERR_MAX_LIMBS` when they are given somewhere to record it. A
refused promotion therefore hands back a NULL operand indistinguishable from the
tagged zero, and `bi_add(NULL, b, &err)` answers `bi_clone(b)` with `err` still
`BN_OK`: `a + b` returns `b`. This is ordinary `int` arithmetic — the guard's own
probe emits `rt_bigint_add` for `total = total + x`. The class is
`refusalIsSwallowed`, pinned at `swallowedRuntimeAnswers = 25` separately from
the 31 so that closing one family cannot be paid for out of the other: 13 signed
entries through `bi_promote` (counting `rt_bigint_to_bigfloat`), 11 unsigned
through `bu_promote`, and `rt_bigint_to_biguint`'s `bu_clone(..., NULL)`. No test
the emitter could write can close it — the answer is a legal value, not a null —
so the repair is the error out-parameter those helpers do not pass, and it
belongs to the bignum lane. The false row belonged to this one.

**One incidental finding, not fixed here.** `rt_string_from_utf16` is
declared in the ABI roster and emitted by `emit_intrinsics_runtime.go`, and has
no definition in `runtime/native` — a native program that reaches it does not
link.

**What the census still cannot see, said rather than left implicit.**
`emit_call_site.go` builds its call as `fmt.Sprintf("call %s %s(%s)", ...)`, so
no entry point is spelled in its text and a scan of emitter source reads NOTHING
of the whole ordinary call path. That is now a reported condition rather than
silence: an unrecorded file writing that statement is a finding, the one file
that does is recorded in `genericCallPathEmitters` with what covers it, and
`TestATestedAnswerIsGuardedOnEveryPathThatReachesIt` derives the gap the way the
31 is pinned — a tested-class entry point that no `case` in an
`emit_intrinsics_*.go` dispatch claims reaches `emitCallSite`, and must be named
in `runtimeAnswersTestedAtTheCallSite` or it is tested by nothing. That row is
what fails on the missing `rt_range_int_new`, in one line, without building
anything.

**Pre-existing reds, checked not assumed.** `TestLLVMParity` fails on
`random_pcg32`, `hash_xxh64`, `hash_stable64`, `uuid_v4` (RV2-DEBT-159 and -306,
same texts) and on `net_echo`, which is undocumented and was measured at
`5897840a` in a separate worktree: identical failure, `panic VM1999: storage:
string is not an unsized integer (type#10)` at `core/string.sg:341:16`.

## The frame's width was stated three times, and the caller said which — 2026-08-29

Lane base `c075d654`, worktree `w-d7`, landed as `4664a0ff`.

**What existed.** A suspension frame was reserved by `emitRuntimeOwnedStorage`
with the size written into the `rt_alloc` call; given back by a generated
per-type `@release.frame.typeN` with the same size written again into an
`rt_free`; and given back a THIRD way by the runtime, which asked the type's
descriptor and always walked the members. Which of the last two ran was decided
by the CALL SITE and recorded in prose beside each one, so a reclamation holding
only the frame's address could not be told what it was holding.

**What exists now.** `runtime/native/rt_frame.{h,c}`, 38 and 8 effective lines.
`rt_frame_alloc(const rt_value_ops*)` reads size and alignment out of the
descriptor and zero-fills; `rt_frame_release(const rt_value_ops*, void*)` reads
the lifecycle word the frame has carried since `4038a55a` and walks the members
only for a frame that says PACKED, deferring that walk through
`rt_release_owned_block_when_unlocked` because it runs generated code. Both ends
of a frame's life name `@__surge_value_ops_typeN`; the emitter refuses a frame
type the operation registry never saw rather than letting it be carried as the
opaque word.

**The word is fixnum-tagged, which is the thing a reader gets wrong first.**
`__frame_state` is a Surge `int`, so the native lane stores
`store ptr inttoptr (i64 2692908695 to ptr)` — `(0x5041434B << 1) | 1` — and not
`0x5041434B`. `rt_frame.c` decodes with `fixi_as_i64`. A release comparing raw
words would pass a C stand that wrote raw words and answer "not packed" for every
real frame, which leaks silently rather than failing.

**Anything that is not PACKED reads as SPENT, deliberately.** Walking a spent
frame frees members the resumed locals already own — a double free. Skipping a
packed one leaks them. The asymmetry decides the default.

**Census.** `go test ./internal/carriergate`, live rows by category over the
production scope: `suspension-frame-owner` 15 -> 0, whole scope 80 -> 65.

**The brief's arithmetic for that 15 was wrong, and the correction matters.** It
was given as five emitter identifiers. The measured 15 is six Go rows
(`emitRuntimeOwnedStorage` x2, `requireSuspensionFrameRelease` x2,
`emitSuspensionFrameReleaseBody` x2) and NINE C rows — `abandoned_state` x4 and
`abandoned_state_type_id` x5 in `runtime/native`, which the brief never named.
`AsyncStateFreeBuiltin` and `emitAsyncStateFreeIntrinsic` had ZERO live rows
before this lane started: `4038a55a` had already deleted them. Driving the
category to zero therefore required the C half as well, and that is the task
field pair becoming `reclaim_frame` / `reclaim_frame_ops` — a descriptor resolved
where the frame is handed over, instead of a pointer and a number.

**"Remove the retired tokens from the scanner" is not executable, and the tree
says so in one command.** Deleting the five Go tokens and the C pattern makes
`go test ./internal/carriergate` fail:

```
--- FAIL: TestLegacyCarrierManifestMatchesExactBaseCensus
    base_test.go:46: exact-base carrier census changed:
        stale legacy: suspension-frame-owner internal/backend/llvm/emit_async_helpers.go token="AsyncStateFreeBuiltin" ...
        stale legacy: suspension-frame-owner runtime/native/rt_async_internal.h token="abandoned_state" ...
```

The frozen census is a census of a COMMIT and is re-derived by scanning that
commit's tree, so a detector that stops matching re-derives `7df10725` as having
fewer carriers than it had. The detectors stay, matching nothing, which is also
what keeps the category able to see the shape come back. `RV2-DEBT-151`'s own
inventory already treats a surviving-but-inert token in `scan_go.go` this way.

**The allocation guard keeps its site and its sentence.** A frame call carries no
size operand for the negative control to rewrite, so the control rewrites the
DESCRIPTOR the site names: `@__surge_frame_alloc_refusal_ops`, emitted only in an
armed build, whose layout asks for 2^64-1 bytes. The armed build still reports
`panic: out of memory: could not allocate __AsyncState$add` and exits 1. That
sentence needs re-pointing when the `surge: fatal [RT_OOM]` shape lands.
`rt_frame_alloc` is the first pointer-answering entry point the LANGUAGE cannot
name, so `emitterOnlyPointerAnswers` records it and
`TestAnEmitterOnlyAnswerIsNotCallable` checks the claim against
`core/intrinsics.sg` rather than trusting it.

**Gates.** `make check` 0, `make golden-check` 0,
`make runtime-v2-lifecycle-check` 0, `make runtime-v2-heap-check` **2**.

**The heap gate is red at `c075d654` and this lane did not move it.** Measured on
a pristine `git archive c075d654` tree and on the lane, identical rows and
identical numbers: `TestRuntimeV2CrossingHeapCaptureCensusBalanced`,
`TestRuntimeV2CrossingStrictCensusBalanced` (`exit=255`, `census share one= 14`),
`TestRuntimeV2FarSelectCancelNonCopySendArm/valgrind` (`got 104B/3 blocks, want
48B/2 blocks`), `TestRuntimeV2LocalSelectCancelNonCopySendArm/valgrind` (`got
48B/1 blocks, want 0B/0 blocks`).

**The frame is still not reclaimed on the ordinary return path.** `4038a55a` said
so and it is still true: a 200-iteration `await` loop definitely-loses 9,408
bytes in 168 blocks — 56 bytes per frame — the same figure before and after this
lane. Wiring the success leg needs `rt_async_return` to be given the frame's
descriptor, and the frame must not be released before the value moves, so it is a
change to that entry point's signature and not a call inserted in front of it.

**`__surge_drop_abandoned_state_call` is a dead symbol.** Zero references under
`runtime/native` — no declaration, no definition, no call. It survives in 15
files under `internal/` as test-harness stubs plus the carrier scanner's token.
`emit.go` carried a comment saying the runtime routed the abandoned state through
it; that comment is corrected here, the stubs are not touched.

## Phase 4 closeout starts from the model, not from the V1 carrier — 2026-09-01

Integration base is `db349581b5318546aac26e19cc6853a1bb65de0e` on
`codex/runtime-v2-closeout`, in the isolated worktree
`/tmp/surge-rv2-closeout.gaHvNP/worktree`.  The canonical checkout's only local
change is the owner's `.gitignore`; this lane does not read from or write to that
dirty tree.

The accepted order is Epic 23b Wave D closeout, Waves E/F, Epic 22 Phase 2,
Epic 21 Task 9, then a V1-gap census.  V1 implementation and ABI compatibility
are not constraints.  The first tracked design change will make every normative
document agree on the slot/pointer transport before production code adopts it;
transport-owned byte/jumbo storage remains future work.  No language syntax,
public configuration, diagnostic UX, PR, merge, or version bump belongs to this
lane.

The first proof tranche is Wave D only: re-run the recorded ledger evidence,
repair mandatory gate blockers with a same-commit negative control, reconstruct
only the justified parts of `origin/lane-sur224`, and obtain five fresh W8 runs
on the dedicated `ryzen` stand.  A non-obvious runtime failure is first ruled by
`docs/RUNTIME_V2.md`; if that model does not decide it, the lane stops.

Pre-change Sentrux scans, all with configured rules passing:

- repository root: quality `6147`;
- `internal`: quality `6451`;
- `runtime`: quality `5295`;
- `runtime/native`: quality `5440`.

Each scan saved a session baseline.  Final evidence will rescan the same exact
paths and record `health`, `check_rules`, and `session_end` after the code and
documentation are stable.

**Pinned-base preflight.**  A detached `db349581` worktree produced the exact
baseline failures the closeout plan expected: `make golden-check` refused
`5362`, want `5359`; `make cppcheck` reported
`rt_channel_refcount.c:272 knownConditionTrueFalse` and the terminal
`toomanyconfigs` information row.  `go test ./internal/carriergate -count=1`
passed in 1.651 s, and `go test ./internal/gatecheck -count=1` passed in
60.053 s.  The first two failures are blockers; the latter two say the carrier
census and ordinary gate package did not cause them.

**Reviewed lane reconstruction.**  The first six commits from
`origin/lane-sur224` were applied in their original order after a read-only
non-author audit.  The three production functions moved by the size repair kept
their bodies and callers.  `6f52c7a0` records only the accepted N1--N3 task-owner
rules; its N4 accept-roster language question remains open and this lane does
not implement that surface.

The heavy-run guard was reconstructed from `e9c33086` plus `e6fdf5a4` as one
change.  The unrelated, untested `c-roster-preflight` hunk was removed.  The
nested Git fixture now strips inherited `GIT_*` state before creating its inner
repository.  Focused proof results on the reconstructed tree:

- guard, hermetic-Git, and aggregate tests: pass in 0.874 s;
- `go test ./internal/mir ./internal/panicgate -count=1`: pass;
- transport-spine acceptance: all 15 rows pass in 5.72 s;
- poll-outcome-pin proof and deterministic negative control: pass in 35.117 s;
- changed-C checks for `rt_async_panic.c`, `rt_virtual_clock.c`, and
  `rt_async_state.c`: pass.
- full `go test ./internal/gatecheck -count=1`: pass in 53.981 s.

**Golden suffix blocker.**  The behavioural reader accepted only
`.order-backends`, while three fairness fixtures were committed as
`.out-backends`; the updater's keep-list would also have deleted the correct
suffix.  The two new regression rows were run on an unfixed `db349581` scratch:
the corpus row named exactly the three unsupported files, and the updater row
reported that `.order-backends` was not preserved.  After renaming the three
inputs and extending the keep-list, both rows pass in 0.025 s.  A pre-commit
`make golden-check` correctly refused the uncommitted delete/add state; the full
gate must run after this corpus-input change is committed.

The committed input change was followed by `make golden-update`.  It exited 0,
left all generated files unchanged, and proposed only the frozen manifest:
entry count `5359 -> 5362`, digest
`3154831e7f6047bfaccfd0d53f42b6cc65ce2b5c1806a2c142c794b605167aef ->
2f75babf4f297fa70a0d9ca7eeaeb12767a5202ec85ec6e7a930ddb4d5a725fa`.

`make golden-check` then completed both serialized generator passes with exit 0
and no corpus or Git-state change.

**Cppcheck blocker.**  The only error-producing finding is the intentional
`knownConditionTrueFalse` at `rt_channel_refcount.c:272`: the armed channel-pin
negative control maps the registered-waiter count to zero so the sanitizer can
reach the pre-pin defect.  The production configuration still checks the real
count.  The repair is therefore a local, explained inline suppression on that
condition; global cppcheck flags and the negative-control arm stay unchanged.

With that local suppression, `make cppcheck` scanned all 109 configured C
files and exited 0 with `cppcheck OK`.  The terminal `toomanyconfigs` row remains
informational; no warning class or global flag was disabled.

`make c-check-changed C_CHANGED='runtime/native/rt_channel_refcount.c'` then
passed both the ordinary and `RT_TEST_SYNC_POINTS=1` compilations.  The paired
freed-channel waiter proof also passed both its ASan negative-control and fixed
rows in 14.72 s; in this ptrace sandbox only LeakSanitizer postprocessing was
disabled (`ASAN_OPTIONS=detect_leaks=0`), while the expected heap-use-after-free
remained required and observed by the negative row.

## LLVM conversion errors use the canonical bare-union representation — 2026-09-01

The invalid UTF-8 golden row exposed a representation-C regression in both
`string.from_bytes` and numeric `from_str`: each intrinsic built a 16-byte
`Error`, then copied 24 bytes from that pointer as though it already were the
destination `Erring<T, Error>`.  The unfixed detached tree proves the failure
in three independent ways:

- the IR rows for both intrinsics contain no direct-member discriminant and
  perform `memcpy ... i64 24` from `rt_alloc(i64 16, i64 8)`;
- the golden LLVM row exits 255 and prints `err=0` where the VM prints
  `err=1`;
- both final Valgrind rows read eight bytes immediately after the 16-byte box
  and then reach an invalid `rt_string_free`; the focused processes terminate
  with SIGSEGV and `ERROR SUMMARY: 2 errors`.

The ruling comes from the already accepted full-membership model in
RV2-DEBT-233, not from the old backend behaviour.  `Error` is direct physical
case 1 of `Erring`; its payload is 16 bytes at offset 8 in a 24-byte,
8-aligned union.  Both intrinsic error branches now call
`emitUnionMaterialiseBareMember`, fail closed if the proven member cannot be
materialised, and never fall back to a union-sized copy from a member pointer.
The temporary `Error` itself is exact-size inline storage, so the obsolete
`allocSiteErrorValue` heap site and roster entry are removed.

The regression test derives case, offset, payload size, and temporary alignment
from `Module.Meta.UnionCases` plus `layout.PhysicalFacts`, then separately
pins the accepted 1/8/16 and 24/8 ABI.  It follows the exact memcpy source and
requires that source to be an aligned alloca; an `rt_alloc(Error.Size,
Error.Align)` source is red even if a future cleanup were to free it later.
The native `from_str` row proves the expected arm/code and zero invalid
accesses; the existing invalid-UTF-8 fixture additionally proves zero definite
and indirect loss.  Both rows are now selected by `runtime-v2-heap-check`.

Candidate evidence:

- focused LLVM membership/materialisation/allocation-roster group: pass;
- invalid-UTF-8 VM and LLVM golden pair: pass, including LLVM `err=1`;
- `from_str` and `from_bytes` Valgrind rows: pass in 12.617 s;
- `from_bytes`: zero definite and indirect loss;
- `from_str`: no invalid access and the expected error code, with the
  independent 327-byte/6-block message residual recorded as RV2-DEBT-314 rather
  than repaired in this blocking change.

## Hosted Runtime V2 aggregate provisions Valgrind — 2026-09-01

The hosted `runtime-v2-check` job runs the complete aggregate, whose mandatory
heap row fails closed when Valgrind is absent.  The job now installs Valgrind
with its LLVM tools instead of documenting that row as an expected hosted red.
The timeout remains 45 minutes until a complete hosted measurement says it must
change.

The gatecheck contract is command- and order-sensitive: it requires exactly one
`make runtime-v2-check` and exactly one preceding `apt-get install` line with a
real `valgrind` shell word.  The test-only tree was red with `0, want 1`; the
fixed workflow and the full `internal/gatecheck` package pass.  Comment-only,
echo-only, false-prefix, missing-package, late-install, and missing-gate
controls are all rejected.

## Hosted sanitizer job runs the owned-storage stands — 2026-09-01

The old `c-sanitizers` job compiled each native C file into an object that it
never linked or executed, then set CGO sanitizer flags for Go tests even though
the production build pipeline invokes clang directly.  Its broad `go test`
selection also omitted the `runtime_v2_pending` tag that owns the sanitizer
stands.  The job therefore did not prove the Runtime V2 C paths named by its
display name.

The job now invokes `make runtime-v2-owned-storage-check`, the existing
fail-closed gate that builds and runs the six channel-element, channel-handle,
and realloc-view ASan/UBSan/TSan rows and requires every expected test to print
`PASS`.  Its job id and display name remain stable so a required-check context
is not renamed as a side effect.

The test-only tree was red with target count `0, want 1`.  The fixed workflow
passes the focused contract; comment-only and missing-target controls stay red,
and a mutant that keeps two generic `go test` commands is rejected with
`direct go test commands = 2, want 0`.

## LLVM union constructors initialize deterministic storage — 2026-09-01

RV2-DEBT-315's bare-member witness was one instance of a wider constructor
rule: P1 applies to the complete union object, including alignment gaps and
inactive arms.  A source census found five constant-tag writers spread across
the ordinary tag, single-payload, cast-into-storage, and bare-member paths.
The map `Option` path was the sole dynamic-tag writer: it exposed Some's payload
slot to the runtime and selected the final tag afterward, but never initialized
the rest of the object.

All constant-tag constructors now share `emitUnionDiscriminant`, which first
emits a layout-sized byte zero-fill through `emitUnionStorageInit` and only
then commits the tag.  The fill uses `llvm.memset`, not an aggregate zero store:
the complete `make check` representation gate proved the latter would move a
composite through a register.  The map path uses the same initializer before it
hands out the payload address, then writes the runtime-selected Some/nothing
tag.  Active payload writes retain their layout-owned offsets and alignment;
no inactive arm is read or given an ownership obligation.

Rule-13 evidence was non-vacuous:

- the bare-member IR test was red because the 16-byte destination had no zero
  initialization before physical case 1;
- the source census was red with five direct constant-discriminant emitters,
  where the fixed tree has one shared emitter;
- the map IR assertion now derives the Option alloca size and alignment and
  requires the matching full-object initializer before the runtime sees the
  payload pointer.

Candidate evidence is green for the focused constructor, membership, nested
tag, and drop group; map get/insert/remove in-place Option construction;
`storage_padding_union` and `storage_overwrite` on both VM and LLVM; the
invalid-UTF-8 VM/LLVM golden pair; and its strict-zero Valgrind proof.
RV2-DEBT-315 is closed.

## Wave D closeout exposes the exact live carrier census — 2026-09-01

The monotonic carrier ratchet rejects additions but deliberately accepts a
legacy finding once it disappears.  It therefore cannot by itself prove Wave
D's current-state exit condition.  The W8 carrier half now has a separate live
census whose accepted report is exactly:

```
suspension-frame-owner=0
llvm-erased-word-bridge=0
llvm-pointer-word-ir allowed=1 unallowed=0
```

The surviving pointer word is pinned by allowance id and the complete frozen
finding identity to `fixnum-inline-tagged-word` in
`internal/backend/llvm/emit_term.go`; preserving only the count is not enough.
The intended proof restores one retired frame owner, one retired erased bridge,
and one unallowed pointer word independently; removes the allowed fixnum word;
and rebinds the allowance to another frozen pointer finding.  Each mutation
must make the W8 census red.  This proves only the carrier half of W8.  It does
not replace the required count of five complete `runtime-v2-check` runs on the
dedicated stand.

Candidate evidence:

- `GOFLAGS=-buildvcs=false go test ./internal/carriergate -run
  '^TestW8CarrierCensusReportsEachExitCategory$' -count=1 -v` passed in 0.786 s
  and printed the accepted `0 / 0 / allowed 1 / unallowed 0` report;
- the returned-frame, returned-bridge, unallowed-pointer, and removed-fixnum
  controls printed respectively `1 / 0 / 1+1 / allowed 0`, and the allowance
  rebind was rejected before a count could legitimize it;
- `GOFLAGS=-buildvcs=false go test ./internal/carriergate -count=1` passed in
  2.415 s;
- `base_test.go` remains 216 lines and the W8-specific proof is isolated in the
  186-line `w8_census_test.go`.

## The default test gate pins this checkout's stdlib — 2026-09-01

An ambient `SURGE_STDLIB=/usr/local/share/surge` redirected compiler fixtures
away from the candidate tree.  The resulting Task clone, droppable, and layout
diagnostics looked like compiler regressions even though the same package sweep
passed when pointed at this checkout.  `make test` now sets
`SURGE_STDLIB="$(CURDIR)"` on its sole `go test ./...` command, so a caller's
stale environment cannot change which stdlib the gate validates.

The contract test dry-runs `make test` under a deliberately stale ambient
value.  It requires one real package-sweep command, one pre-command
`SURGE_STDLIB` assignment, and the cleaned absolute repository root.  Missing,
stale, late, duplicate, and false-prefix mutants are rejected; echo noise does
not count as a command.  On parent `b7493eae`, with only the test overlaid, the
Rule-13 row was red with `SURGE_STDLIB assignments before go test ./... = 0,
want 1`.

Candidate evidence:

- `GOFLAGS=-buildvcs=false go test ./internal/gatecheck -run
  '^(TestMakeTestPinsStdlibToRepository|TestStdlibMakeDryRunValidatorNegativeControls)$'
  -count=1 -v` passed in 0.125 s;
- `GOFLAGS=-buildvcs=false go test ./internal/gatecheck -count=1` passed in
  47.877 s;
- the reviewer independently reproduced the parent red, verified a quoted
  checkout path containing spaces, and regenerated byte-identical `STATS.md`;
- no heavy Runtime V2, sanitizer, Valgrind, or benchmark gate ran locally.

## Carrier census follows VM/asyncrt owner types, not filenames — 2026-09-01

Scanner-only lane `codex/carriergate-structural-vm-owner` starts from exact
integration commit `d8e100d74f08d77718ad8b45d3e19c7cbe20af34` in the isolated
worktree `/tmp/surge-carriergate-structural.mXPnuH/worktree`.  The rejected D8
candidate proved the current blind spot: `vmOwnerField` recognizes a closed
list of basenames, so moving a `Value` owner to a new file or hiding it behind
an alias/pointer/container can make the live ratchet green without removing
the carrier.

The intended repair is additive.  Existing lexical finding identities and the
exact-base/live-ratchet rules stay intact; a package-wide AST type graph adds
structural findings for VM `Value`, empty-interface payloads, and generic
`P Payload` owner fields.  A separate root invariant rejects a `VM`/`Executor`
collection whose element is itself a lifecycle slot (`state` + generation +
exact backing storage): owner-specific task/channel/select records must wrap
their own region instead of sharing a root general slot pool.  No D8 allowance
or migration entry will be added.  Any owners rediscovered at the immutable
Epic 23b base will be recorded only as a frozen baseline repair, with the exact
base scan as proof.

Pre-edit gates: Sentrux repository scan of the isolated worktree reported
`quality_signal=6150`; the scoped scan of `internal/carriergate` reported
`quality_signal=8984`, and `session_start` saved that scoped baseline.  Planned
Rule-13 controls cover a direct `Value` in an unknown basename, an
alias/container/pointer chain, `P Payload`, token pointer/interface payloads,
and a root general slot pool with no `Value`.  Focused Go tests, unfixed-tree
red proof, `gofmt`, `git diff --check`, file-size checks, final Sentrux gates,
and the repository pre-commit hook remain to run.  Heavy Runtime V2 gates do
not belong on this local branch worktree and will not be run here.

Rule-13 red was captured before scanner implementation with
`GOFLAGS=-buildvcs=false go test ./internal/carriergate -run
'^TestStructuralOwnerCensusRejectsHiddenCarriers$' -count=1 -v`: the unfixed
tree reported `structural owner controls missed = 6, want 0` and named all six
rows.  The failure is non-vacuous: every fixture parsed and scanned; only the
required structural finding was absent.

After the package graph was added, the six-row control and its acceptance twin
both passed, as did the existing lexical determinism test.  The exact-base scan
then rediscovered 33 owners the basename switch had never frozen: 11 erased
asyncrt fields and 22 VM direct/one-cell `Value` fields.  Canonical structural
evidence makes the old `any` and current `P Payload` spellings one surviving
owner identity instead of fabricating a migration for a rename.  One finding
exists live but not at the immutable base: `tagScrutinee.value Value` in
`internal/vm/tag.go`, introduced by `75b56918`.  It is a real temporary owner
under the Epic 23b census wording, not a false positive; it therefore needs an
explicit tracked migration/debt classification rather than an allowance or a
scanner exception.  No D8 candidate owner appears in either scan.

Owner ruling: classify that live-only temporary as `RV2-DEBT-316` and one
manifest migration item, never an allowance.  Its close condition is that a
tag scrutinee no longer owns a composite through `Value`; scalar, nothing and
handle `Value` uses remain legal.  The typed-temp vertical is scheduled before
final Wave D W8.  The D8 owner receives no allowance or migration entry.

The final Rule-13 fixture was copied alone onto a detached clean worktree at
exact `d8e100d7`, then run with
`GOFLAGS=-buildvcs=false go test ./internal/carriergate -run
'^TestStructuralOwnerCensusRejectsHiddenCarriers$' -count=1 -v`.  It failed
before the scanner with the exact diagnostic `structural owner controls missed
= 6, want 0: direct Value in a moved file, alias pointer and container
sidecar, generic Payload owner, token interface payload, token pointer to
interface payload, root general slot pool`.  The scratch worktree was removed
afterward.  The same six-row test is green on the candidate.

Final scanner evidence:

- the immutable-base manifest is `659` findings at digest
  `92969b43d82783481c2052b220892b3696e30190e528cb2b92e5be20bba24a3e`;
  it has exactly four pre-existing allowances, 53 migrations including only
  `tagScrutinee.value->Value` as the new `RV2-DEBT-316` row, and no
  `asyncPayload` allowance or migration;
- the focused exact-base, live-ratchet, six-row structural, spelling-stability,
  manifest-classification, and owner-region controls passed together in
  1.547 s;
- `GOFLAGS=-buildvcs=false go test ./internal/carriergate -count=1` passed in
  2.600 s; `GOFLAGS=-buildvcs=false go test -race ./internal/carriergate
  -count=1` passed in 33.456 s; and `GOFLAGS=-buildvcs=false go vet
  ./internal/carriergate` passed;
- `gofmt`, `git diff --cached --check`, the manifest assertions, and the file
  limits passed.  The new scanner/test files are 282, 244, and 204 lines;
- the scoped Sentrux session began at `quality_signal=8984`.  Its first staged
  scan improved signal to 9048 but correctly rejected one new complex
  orchestration function (`2 -> 3`).  Splitting declaration traversal from
  per-struct inspection removed that regression.  Final scoped signal is 9062
  (`+78`), with `session_end pass=true` and no violation.  The mandatory
  `internal` scan reports 6455 and all seven rules pass; the final repository
  scan reports 6152 versus the pre-edit 6150 and all eight checked rules pass;
- `make runtime-v2-carrier-check` and the other heavy Runtime V2 gates were not
  run locally: Rule 19 reserves them for the dedicated stand.  This scanner
  lane changes no runtime/C code and needs no local sanitizer or Valgrind run.

The shared worktree hook is Fusion's identity guard rather than the repository
`scripts/pre-commit`, so the latter was run explicitly.  Its first invocation
reached the VM package and showed one worktree-only infrastructure failure:
every nested binary build was refused by Go with `error obtaining VCS status:
exit status 128`; a standalone `go build ./cmd/surge` reproduced it while
direct `git status --porcelain` and `git log` remained green.  The required
rerun therefore used the diagnostic Go remedy verbatim,
`GOFLAGS=-buildvcs=false ./scripts/pre-commit`.  The first rerun's lint phase
found two unchecked AST type assertions; both became guarded assertions and
the complete hook was rerun from the start.  That final hook passed the full
`make check` package sweep, lint with zero issues, `c-check`, and the file-size
gate, then regenerated and staged `STATS.md`.

## Structural carrier walk has no depth, generic, or owner-region name escape — 2026-09-01

Independent review of `3606f4f7` found two P1 holes.  First, the carrier walk
spent a one-record `namedBudget`, so a moved cross-file alias or a finite
leaf/middle/token chain could hide the same `Value`.  Second, the root
general-slot check stopped at the first named wrapper and treated
task/channel/select spelling as owner evidence.  The repair removes both name
decisions: finite type graphs end only at a recursion guard, and a root walk
stops only at a structurally proved region with exact layout facts, backing
storage, and lifecycle slots.

Rule-13 was red before the repair in three independent ways.  The carrier
fixture reported all three new cross-file/deep paths missing; the layout-less
named-region fixture returned no `general-slot`; and the generic, selector
decoy, and pointer-reachability group respectively failed to substitute
`Wrapper[ownerSlot]`, accepted `evil.Descriptor` by selector spelling, and
showed that an unmarked pointer cannot be classified as a borrow from the Go
type graph.  The ruling is conservative: both the direct carrier leaf and
every finite unmarked pointer path to it remain visible.  No field or type name
is used to infer ownership.

Generic recursion required one more negative control.  A local recursive
generic whose recursive edge was already visiting fell through to the opaque
external-generic rule and counted a phantom type argument as storage.  The
mutual `Node[P]`/`Branch[P]` fixture was red: it reported the carrier through
the recursive back edge and also reported `Phantom[Value]`.  Local declarations
now stop before that fallback, so the concrete `Branch.value` path survives
and the phantom path does not.  A changing recursive instantiation such as
`Node[*P]` is not another valid graph state: `go tool compile` rejects it as an
`instantiation cycle`.

The stronger exact-base scan adds 24 reviewed paths, all transitive routes to
already known carrier leaves: one asyncrt path and 23 VM paths.  The immutable
manifest is now `683` findings at
`db5a0f475c32c2155aa82f3606800da0668392bd2e7a7aee917b742e76e58ee9`;
the structural category counts are 12 asyncrt and 45 VM.  The live scan found
22 post-base paths.  `VM.Async -> Executor[Value]` is tracked under
RV2-DEBT-151 until D8's exact async-owner cutover.  The other 21 reach
`Frame.Locals -> LocalSlot.V -> Value` through post-base storage/control
graphs; they are one grouped instrumentation finding, RV2-DEBT-318, with 21
exact migration identities.  The manifest still has exactly four allowances;
none of these paths, `tagScrutinee`, or a D8 `asyncPayload` owner is allowed.

Final candidate non-heavy evidence:

- the exact-base, live-ratchet, reviewed-migration, recursive-generic, generic
  slot, descriptor-decoy, owner-region, pointer-reachability, and deep carrier
  controls pass;
- `GOFLAGS=-buildvcs=false go test ./internal/carriergate -count=1` passed in
  2.685 s; `GOFLAGS=-buildvcs=false go test -race ./internal/carriergate
  -count=1` passed in 32.851 s; and `GOFLAGS=-buildvcs=false go vet
  ./internal/carriergate` passed;
- `gofmt`, `git diff --check`, and the project `<500`-line limit pass; the
  scanner implementation is split into 283/221/218-line files and its tests
  into 254/127-line files;
- `GOFLAGS=-buildvcs=false ./scripts/pre-commit` passed its complete
  `go test ./...` sweep, lint with zero issues, strict C checks, and file-size
  gate, and regenerated `STATS.md`;
- Sentrux reports scoped `internal/carriergate` signal 9049.  That is 13 below
  the `3606f4f7` snapshot (9062) because the P1 proof adds generic/region
  branches, but remains 65 above this scanner lane's saved pre-edit signal
  8984.  The `internal` scan remains 6455 with all seven rules green; the root
  scan is 6153 (one above the prior 6152) with all eight checked rules green.
  The scoped directory has no rules file.  Rule 19 still reserves the heavy
  Runtime V2, sanitizer, Valgrind, and benchmark gates for the dedicated stand.

## Structural carrier identity includes effective generic state — 2026-09-01

The second independent review of `3606f4f7` plus `cb7722c3` found five P1
escapes.  Both walkers keyed recursion only by declaration name; the typed
region proof combined unrelated layout, backing, and lifecycle facts; embedded
named methodless interfaces were opaque; the root map walk skipped keys; and
root identity required the literal `VM` or `Executor` struct declaration.

The Rule-13 tests in `scan_go_owner_p1_test.go` were overlaid, without the
production repair, onto a detached worktree at exact
`cb7722c30657748f0fd123006cf712eb681564be`.  The named five-test command exited
1 with eight precise missing paths:

- `TestStructuralOwnerCensusKeysRecursionByEffectiveBindings` missed
  `token.root->Direct.next->Direct.value->Value` and
  `VM.pool->general-slot(generalSlot)`;
- `TestStructuralOwnerCensusRequiresCoherentTypedRegion` let the unrelated
  facts decoy hide `VM.owners->general-slot(foreignSlot)` while its canonical
  positive twin passed;
- `TestStructuralOwnerCensusResolvesEmbeddedMethodlessInterfaces` missed
  `token.payload->universal`;
- `TestStructuralOwnerCensusWalksRootMapKeysBeforeValues` missed
  `VM.pool->general-slot(generalSlot)`; and
- `TestStructuralOwnerCensusResolvesCanonicalRootIdentity` missed the canonical
  `VM.pool` token for both alias and named-underlying roots and the canonical
  `Executor.pool` token for an alias root.

The landed recursion state is `{declaration, canonical effective bindings}`.
Aliases resolve before the binding signature is formed, concrete non-carrier
arguments remain distinct, and recursive generic arguments no longer disappear
behind a declaration-only guard.  The exact `Direct[int] -> Direct[Value]`
fixture also passed `go tool compile`, so the positive control represents a
valid finite Go type graph rather than parser-only syntax.  Map roots inspect
keys before values, and a root declaration resolves through alias and named
underlying chains while its finding retains the canonical `VM` or `Executor`
token.

A typed owner-region boundary now proves one descriptor shape: a nested
`size/align/stride/flags` layout plus the two structurally mandatory operation
shapes (move: two inputs and no result; plan: two inputs and a result).  The
same region must also reach byte backing and a collection of lifecycle slots
with state and generation.  Three unrelated records cannot satisfy the proof;
neither a selector nor task/channel/select spelling grants an exemption.
Embedded methodless interfaces resolve recursively by declaration identity;
methodful, non-basic constraint, and cyclic controls remain non-carriers.

The immutable census remains exactly `683` findings at digest
`db5a0f475c32c2155aa82f3606800da0668392bd2e7a7aee917b742e76e58ee9`.
Allowances remain four, and the reviewed migration rows remain unchanged; this
repair adds no D8 owner entry.  The five new groups pass together in 0.020 s,
the complete `internal/carriergate` package passes in 2.638 s, its race run
passes in 32.745 s, and package vet passes.  The first full pre-commit run
completed every Go package, then lint named two local style issues; named
results and a tagged channel-direction switch repaired them.  The next lint
pass required the two adjacent named result types to share one declaration;
the final rerun started again from the full package sweep and passed tests,
lint with zero issues, strict C checks, and the file-size gate.

Sentrux reports `internal/carriergate` quality 9015, still 31 above this lane's
saved 8984 baseline; that scope has no rules file.  The `internal` scan reports
6454 with all seven rules green, and the root scan reports 6152 with all eight
checked rules green.  Rule 19 keeps heavy Runtime V2, sanitizer, Valgrind, and
benchmark rows on the dedicated stand; this scanner-only repair runs none of
them locally.

## Structural owner proof is nominal and generic substitutions are lexical — 2026-09-01

The third independent review of exact `85d737d1` found four P1 families.  The
typed-region exemption still accepted an arbitrary layout-shaped callback
record, byte slice, and foreign lifecycle slot.  Generic methodless interface
embeddings lost their actual arguments.  Generic aliases and named underlying
types lost the effective bindings of canonical `VM` and `Executor` roots.
Finally, inner type parameters rewrote composite actuals such as `Box[*P]`
instead of resolving them in the caller's immutable environment.

The four new test groups were overlaid without production changes on exact
`85d737d101b2cd76692172cd618c21cdd8ee2404`.  The command
`GOFLAGS=-buildvcs=false go test ./internal/carriergate -run
'^TestStructuralOwnerCensus(RequiresCanonicalVMAsyncOwner|InstantiatesMethodlessInterfaces|PreservesGenericRootBindings|SubstitutesCompositeActualsBeforeShadowing)$'
-count=1` exited 1 with ten exact violations: the callback decoy hid
`VM.owners->general-slot(foreignSlot)`; the real D8 shape exposed
`VM.owners->general-slot(asyncPayloadSlot)`; generic methodless interfaces
missed `token.payload->universal`, `pairToken.payload->universal`, and
`constrained.payload->universal`; generic roots missed
`VM.pool->general-slot(generalSlot)` for alias and two-argument named forms and
the same `Executor.pool` token for its alias; composite substitution missed
`VM.one->general-slot(generalSlot)` and
`VM.two->general-slot(generalSlot)`.  Methodful and non-basic constraints,
generic cycles, and the existing carrier in the composite fixture remained
valid controls.

The repair uses one effective-type representation: an AST expression plus the
immutable lexical environment in which that expression was written.  Carrier,
general-slot, interface, constraint, and root walkers now instantiate from
that representation and key recursion by declaration plus canonical effective
bindings.  A callee's parameter may shadow a spelling, but it cannot rewrite a
caller's captured `*P`.  Root aliases retain their actuals while findings keep
the canonical `VM` or `Executor` token; maps still inspect keys before values.

An adversarial review then found that a package declaration still inherited
the caller's environment and that anonymous composite actuals were rendered
without their captured environment.  The final p3 fixtures were also overlaid
on exact `85d737d1`.  `GOFLAGS=-buildvcs=false go test
./internal/carriergate -run
'^TestStructuralOwnerCensus(KeepsDeclarationEnvironmentsLexical|CanonicalizesAnonymousActualsInEnvironment|TerminatesTransformedGenericAliasCycle|DistinguishesFiniteNestedAliasActuals)$'
-count=1` exited 1: caller capture missed
`VM.pool->general-slot(generalSlot)` and
`token.root->carrierWrap.nested->carrierLeaf.value->Value`; anonymous-actual
identity missed
`token.root->CarrierRoot.value->CarrierDirect.next->CarrierDirect.value->x->Value`.
The same base already terminated the transformed alias-cycle control and found
the root anonymous-slot and finite nested-alias controls.

Two negative controls caught defects in the candidate architecture itself.
`type Link[P any] = *P; type VM = Link[VM]` first crashed the scanner with
`fatal error: stack overflow`, and a declaration-only alias guard then made the
finite `Wrap[Wrap[Q]]` fixture miss
`VM.pool->general-slot(generalSlot)`.  Canonicalization now pre-resolves each
alias actual before activating its declaration cycle guard.  Finite nested
aliases remain distinct, while a recursive alias body stops at the active
declaration.  All four p3 groups now pass together.

The owner exemption now recognizes only the closed nominal
`surge/internal/vm.asyncOwnerRegion` contract used by D8: exact owner identity,
`types.TypeID`, `storageMember`, stride, `Arena`, and
`[]asyncPayloadSlot` lifecycle fields plus their exact dependent nominal and
scalar types.  It has no callback-arity, selector-spelling, or existential
layout heuristic.  Unknown and near-miss regions fail closed.  Negative
controls also reject an extra callback on the exact-shaped owner and a
package-level `uint32` shadow; the latter first failed with
`shadowed primitive spoof hid token
"VM.owners->general-slot(asyncPayloadSlot)"` before builtin resolution began
respecting package declarations.  This internal recognition needs no public
marker, ABI, language, or UX change.

Current non-heavy evidence: the new focused groups and both updated legacy
owner-region twins pass; `go test ./internal/carriergate -count=1` passes in
2.669 s with the immutable census still `683` at digest
`db5a0f475c32c2155aa82f3606800da0668392bd2e7a7aee917b742e76e58ee9`.
`make lint` reports zero issues, and `git diff --check` and
`./check_file_sizes.sh` pass.  The project file-size gate reports all five
modified production files green at 277, 274, 191, 93, and 217 effective LOC.
`go test -race ./internal/carriergate -count=1` passes in 32.730 s, and
`go vet ./...` passes after the alias-guard repair.

The first full pre-commit passed every Go package, including VM in 113.271 s,
then lint rejected one constant `underlying` parameter; `namedUint8` removed
it.  A second complete run was intentionally interrupted during VM when the
independent review reported the nested-alias blocker, so it is not final gate
evidence.  The final `GOFLAGS=-buildvcs=false ./scripts/pre-commit` rerun passed
the complete package sweep (VM 98.993 s), lint with zero issues, strict C
checks, and the file-size gate, then regenerated and staged `STATS.md`.  Final
Sentrux reports scoped `internal/carriergate` quality 9031, 47 above the saved
pre-edit lane baseline 8984; equality remains its only bottleneck.  The MCP
session state no longer retained that old baseline, so the first final
`session_end` honestly returned `No baseline saved`; a same-tree replacement
session closed with `pass=true`, signal 9031 -> 9031, while the recorded
pre-edit/final scans provide the real comparison.  That scope still has no
rules file.  The `internal` scan reports 6455 with all seven rules green, and
the root scan reports 6153 with all eight checked rules green.  Rule 19 still
reserves heavy Runtime V2, sanitizer, Valgrind, and benchmark rows for the
dedicated stand.

## Structural owners resolve through packages inside the scanned scope — 2026-09-01

Final review of `1d32eda5` found one remaining P1 escape.  The fixed production
scope already walks subdirectories, but the structural graph discarded every
source whose directory was not exactly `internal/vm` or `internal/asyncrt`.
The surviving root graph also treated a qualified type as opaque.  A valid
package extraction could therefore put `type Fog = interface{}` or a lifecycle
`Slot { state; generation }` in `internal/vm/sidecar`, refer to it as
`*sidecar.Fog` or `map[uint64]*sidecar.Slot`, and leave the carrier gate green.
That is exactly the renamed indirect sidecar and root general pool the
normative absence contract forbids.

Rule 13 ran before production repair on exact `1d32eda5`:
`GOFLAGS=-buildvcs=false go test ./internal/carriergate -run
'^TestStructuralOwnerCensus(ResolvesScopedPackageSelectors|KeepsScopedSelectorControlsNegative)$'
-count=1 -v` exited 1.  The universal-alias row printed `findings=[]` for
`token.q->universal`, the lifecycle row printed `findings=[]` for
`VM.pool->general-slot(Slot)`, and the methodful/non-lifecycle control passed.

The scanner now indexes each package directory below the two existing scopes.
Unqualified identifiers resolve in their lexical package, and selectors
resolve only when their import names another indexed package in the same fixed
scope; imports outside that scope remain opaque.  Import aliases, dot imports,
and a declared package name different from `path.Base(importPath)` use Go's
lexical binding rather than path spelling.  Nested package structs participate
in the carrier census, while canonical `VM` and `Executor` root discovery stays
restricted to the two root packages.  No marker, public API, ABI, language, or
UX surface was added.

The repaired selector, dot-import, declared-package-name, nested-struct, and
negative-control rows pass.  The exact-base and live ratchets remain green at
the immutable `683` findings and digest
`db5a0f475c32c2155aa82f3606800da0668392bd2e7a7aee917b742e76e58ee9`;
the manifest, four allowances, and 75 migrations are unchanged.  The complete
`internal/carriergate` package passes in 2.562 s, its race run passes in
33.617 s, `go vet ./...` passes, and `make lint` reports zero issues.  The full
pre-commit completed the package sweep (`internal/vm` in 99.922 s), lint,
strict C checks, and Rule 4; after the final package-name-set hardening, the
exact final scanner package and package vet passed again.  Sentrux
closes the scoped session with `pass=true`, signal 9045 -> 9048; `internal`
stays 6455 with all seven rules green, and the repository reports 6154 with all
eight checked rules green.  Rule 19 still reserves heavy Runtime V2,
sanitizer, Valgrind, and benchmark rows for the dedicated stand.

### Wave D tag-scrutinee migration reconciliation

The exact tag-scrutinee owner change removes the live structural token
`tagScrutinee.value->Value`: a composite is now held as exact `StorageRef`, a
heap value as a retained handle, and scalar state only by `ValueKind`.  The
RV2-DEBT-316 migration entry was therefore deleted from the live manifest and
the debt row closed rather than converted into an allowance.  The separate
conservative path through `tagScrutinee.storage -> Arena.refs -> Frame.Locals`
remains an RV2-DEBT-318 Wave F migration; this integration does not confuse
exact tag ownership with the legacy frame-local leaf it can still reach.

## D8 VM interval transport is retired — 2026-09-01

The remaining RV2-DEBT-151 half no longer re-boxes a VM payload in a fresh
transport interval. `internal/vm/transport_storage.go` and its three helpers are
deleted, and all twelve channel send/receive, task publication, select, cancel,
resource-release, and shutdown callers now route through concrete exact typed
owner regions.

The executor-visible `asyncPayload` is control only:
`ownerKind/ownerID/ownerGeneration/region/index/slotGeneration/parkSeq`. It
contains no `Value`, type id, storage reference, arena pointer, or generic
owner pointer. The descriptor remains in one homogeneous channel, task,
select-arm, or task-resume region. A consumer validates every coordinate and
state, initializes caller-owned exact storage by a true storage-to-storage move
without retain/clone, commits the source terminal, and only then makes the slot
reusable. Earlier task-result askers clone directly into their destinations;
the final asker moves. The old `cloneValueComposite` task-result migration row
is therefore removed and RV2-DEBT-246 closes with this cutover.

Reservation and teardown have one lifecycle:
`EMPTY -> RESERVED -> INITIALIZED -> CLAIMED -> MOVED/DROPPED -> EMPTY`.
Reserved, initialized, claimed, stale, cancellation, refill-failure, refused
commit, and shutdown paths retain or discharge exactly one obligation. A slot
whose generation reaches `MaxUint32` becomes quiescent `EXHAUSTED`; it is never
reused, a replacement slot is added, and owner teardown still retires the
arena. This prevents ABA without turning generation exhaustion into a hidden
liveness leak.

Rule 13 evidence was captured before production implementation: the structural
owner scanner missed an alias/pointer/container carrier, the destination-only
API test found the old `Value`-returning take, and the owner-region tests did
not compile. The final scanner parses every production Go file in
`internal/vm`, follows named aliases, pointers and containers transitively, and
keeps `Arena`/`StorageRef` as deliberate exact-storage terminals. Focused
evidence is green for `internal/asyncrt`, the package-wide carrier gate, owner
generation/region/role/park validation, reservation and claim lifecycle,
active-union direct move, task result clone/move, try-send stage failure,
receive-refill failure, shutdown, strict-zero task/channel/transport programs,
the VM async golden corpus, and its LLVM subrows. Heavy aggregate and load
gates were intentionally not run in this development worktree.

One adjacent pre-existing branch is explicitly not a D8 blocker:
`Channel.popSendWaiter` / `hasSendWaiter` can prune a done sender without
returning its generic payload to the VM. It is recorded as RV2-DEBT-317 with an
exact close condition rather than widening this atomic storage cutover.

### D8 independent-review hardening

Two negative controls found lifecycle gaps before integration. First, a
composite stage moved into an owner slot before scratch handoff could report an
internal refusal. The refusal then reset the reserved slot without dropping
the bytes it had already received. Scratch handoff now has a non-mutating
preflight before `storageMoveInit`; a returned failure leaves the destination
`EMPTY` and the source obligation unchanged, as the typed-carrier contract
requires.

Second, the executor's receive queue named only a task. A task woken and parked
again could therefore make an old registration look current, including when it
parked on the same channel. Receive registrations now carry the VM task's
monotonic park sequence. Queue selection, reservation, commit, abort, and close
preserve or validate that exact sequence and the current channel key; the VM
binds the resume claim to the sequence the reservation consumed rather than
re-reading and adopting a newer one. The permanent negative rows cover both a
different-channel repark and two generations on the same channel. The sender
queue issue remains separately scoped to RV2-DEBT-317.

The two touched legacy files already over Rule 4's limit both shrink:
`internal/asyncrt/asyncrt.go` 608 -> 577 physical lines and `channel.go`
541 -> 483. The effective-size gate reports 497 and 422 respectively. Task
accessors and receive-registration validation moved into narrowly named 25-
and 73-line files; no catch-all helper module was introduced.

### D8 rendezvous reservation/close correction

Independent lifecycle review found a third blocking gap: reserving a
rendezvous receiver removed its exact registration from `recvq`, but did not
transfer that registration into channel-owned state visible to close,
cancellation, stale wakeup, or shutdown.  A close between reservation and
publication could therefore leave the receiver parked forever.

The approved owner ruling is now explicit in `docs/RUNTIME_V2.md`: reservation
is not publication; a successful owner-lane commit is publication.
Commit-before-close delivers `T` and close cannot revoke it. Close-before-
commit rejects with the existing send-on-closed result, settles the exact
receiver closed/`nothing`, and leaves the VM payload lane to drop its staged
value exactly once. Close-before-abort retires that same close-won claim
idempotently and never requeues it.

`Channel.recvClaim` is the smallest channel-owned implementation of that
ruling. It carries only `(task, park-generation)` control identity. Moving a
live FIFO registration into it is one owner-lane transition; commit, abort,
close, generic wake/cancel, stale-generation pruning, destroy, and drain can
all retire the same identity. There is at most one active rendezvous claim.
Later direct, buffered, blocking, try, and selected sends cannot overtake it;
a refused blocking sender subscribes to `ChannelSendKey` without evaluating or
staging its source. Claim release wakes both select send subscriptions and
those retry subscribers, and the executor's ready credit closes the late final
`ParkCurrent` sleep race. `parkTask` checks that durable credit before creating
any subscription: a credited task remains `READY` for the next poll regardless
of which key the completed poll reports. `Yield` delegates to `Wake`, so every
existing-task producer retires a parked row and exact receive claim before
entering `readySet`; spawn is the only other producer and starts without park
state. Thus `ready => not parked` is structural, rather than a cleanup deferred
until `NextReady`. Generic wake changes control state only: it never touches
`Value`, `asyncPayload`, or an owner slot.

The VM validates the receiver sequence captured by the reservation in both
`stageReservedChannelSend` and `routeAsyncPayloadToChannel`. The ordinary send
path reserves before evaluating its source, and direct, try, and selected
close-won commits discharge composite payloads in their existing owning lane.
No new `Value` carrier, public payload API, language construction, or syntax
was introduced.

Rule 13 RED evidence used the exact prior production index (binary diff SHA256
`837bda5512aedac2822441002692895a26f8dfd3e8e146a666841ab5827a5c41`)
plus the permanent behavior-only tests. It failed with receiver resume
`(0, 0), want closed/nothing`; close-won abort, cancel, external wake, and drain
each restored one registration; later FIFO and ring sends overtook the claim;
claim-refused send retry rows reserved ahead of it. Both direct composite
teardown orders and both routed/select orders observed `ResumeNone` instead of
closed, and closed `try_send` returned `VM2103` instead of `false` without an
error. Commit-before-close, payload-lane-only terminal cleanup, stale queued
generation filtering, and MaxUint64 were deliberate positive controls and
already passed.

Precise negative mutants cover the controls whose prerequisite fix was already
present in that staged index. Removing the MaxUint64 guard failed with
`implicit receive generation wrapped after MaxUint64`. Treating every waiting
task as live made all four same/different-channel queued/claimed generation
rows fail. Removing the captured-sequence check from the direct and routed VM
paths failed with `stage accepted a changed receiver sequence` and `route
accepted a changed receiver sequence` respectively. Production was restored
after every mutant.

Final focused evidence is green: the asyncrt lifecycle/generation/terminal
matrix (`ok`, 0.004 s), the direct/routed/composite VM matrix (`ok`, 0.034 s),
full `internal/asyncrt` (`ok`, 0.004 s), targeted race detector rows
(`internal/asyncrt` 1.023 s, `internal/vm` 1.227 s), and focused vet. The
cancelled local-select row passed in 10.909 s; its Valgrind subrow passed in
13.061 s (2.12 s inside Valgrind) with copy and non-copy lanes at 0 bytes and
0 blocks, including 0 indirect bytes.

Rule 10 exact command ledger for those recorded runs (all from
`/tmp/surge-d8-lifecycle-fix.Lfnik1`):

```sh
GOFLAGS=-buildvcs=false go test ./internal/asyncrt -count=1 -timeout=2m -run 'TestChannelRendezvous|TestChanCanSendFiltersReceiveGenerations|TestChannelReceiveGenerationOverflowFailsClosed' -v
```

Result: PASS, `ok surge/internal/asyncrt 0.004s`.

```sh
GOFLAGS=-buildvcs=false go test ./internal/vm -count=1 -timeout=3m -run 'TestReservedRendezvousCloseDropsCompositeExactlyOnce|TestReservedTrySendCloseReturnsFalseAndDropsComposite|TestRoutedSelectRendezvousCloseDropsCompositeExactlyOnce|TestReservedRendezvousSequenceChangeLeavesSourceUnchanged|TestRoutedRendezvousSequenceChangeLeavesStagedSourceUnchanged|TestReservedRendezvousTerminalOwnerEventsLeavePayloadToVM|TestReservedTrySendErrorReleasesItsSourceValue|TestRefillErrorDropsAlreadyPoppedRingPayload' -v
```

Result: PASS, `ok surge/internal/vm 0.034s`.

```sh
GOFLAGS=-buildvcs=false go test ./internal/asyncrt -count=1 -timeout=5m
```

Result: PASS, `ok surge/internal/asyncrt 0.004s`.

```sh
GOFLAGS=-buildvcs=false go test -race ./internal/asyncrt -count=1 -timeout=3m -run 'TestChannelRendezvous|TestChanCanSendFiltersReceiveGenerations|TestChannelReceiveGenerationOverflowFailsClosed'
GOFLAGS=-buildvcs=false go test -race ./internal/vm -count=1 -timeout=4m -run 'TestReservedRendezvousCloseDropsCompositeExactlyOnce|TestReservedTrySendCloseReturnsFalseAndDropsComposite|TestRoutedSelectRendezvousCloseDropsCompositeExactlyOnce|TestReservedRendezvousSequenceChangeLeavesSourceUnchanged|TestRoutedRendezvousSequenceChangeLeavesStagedSourceUnchanged|TestReservedRendezvousTerminalOwnerEventsLeavePayloadToVM|TestReservedTrySendErrorReleasesItsSourceValue|TestRefillErrorDropsAlreadyPoppedRingPayload'
```

Results: PASS, `ok surge/internal/asyncrt 1.023s` and
`ok surge/internal/vm 1.227s` respectively.

```sh
GOFLAGS=-buildvcs=false go vet ./internal/asyncrt ./internal/vm
```

Result: PASS, exit 0 with no diagnostics.

```sh
GOFLAGS=-buildvcs=false go test ./internal/vm -count=1 -timeout=2m -run '^TestRuntimeV2LocalSelectCancelNonCopySendArm$/^cancelled_outcome$' -v
GOFLAGS=-buildvcs=false go test ./internal/vm -count=1 -timeout=3m -run '^TestRuntimeV2LocalSelectCancelNonCopySendArm$/^valgrind$' -v
```

Results: PASS, `ok surge/internal/vm 10.909s` and
`ok surge/internal/vm 13.061s`; the latter logged the 2.12 s Valgrind subrow
and zero direct or indirect bytes/blocks for both lanes.

Independent review repeated the complete local-select row on staged snapshot
`54aa252ec14820e3f855753bcd7fe388ef7c1073ef900ba44c57b0cbf45c522f`
with this exact command:

```sh
SURGE_STDLIB=/tmp/surge-d8-lifecycle-fix.Lfnik1 GOFLAGS=-buildvcs=false SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run '^TestRuntimeV2LocalSelectCancelNonCopySendArm$' -count=1 -parallel=1 -p=1 -v --timeout 120s
```

Result: PASS, `ok surge/internal/vm 13.783s`; Valgrind took 2.18 s and both
lanes remained at 0 direct/indirect bytes and blocks.

A final frozen-snapshot audit found that the first late-park test proved only
that ready credit survived: it consumed that credit without checking whether
the final `ParkCurrent` had also recreated a stale waiter row. Against frozen
staged SHA256
`d6ad82e99dbe4ad237ade677144e0b79c13e70a016f9f6277b11c55167bdbf9d`,
the permanent same-key and unrelated-key rows both failed with `late park left
a subscription beside durable ready credit`; a later successful retry could
therefore wake and re-enqueue its currently running sender. The handshake above
makes both rows green, including two queued FIFO receivers, consumed sender
credit, successful retry publication, exact second-receiver wake, and no
self-enqueued sender. The rendezvous matrix passes 100 repetitions and the new
row passes 10 race-detector repetitions. Four old generation tests initially
set `current` directly over an unconsumed Spawn/Wake credit, a state the VM
scheduler cannot produce; their harness now follows the production
`NextReady -> SetCurrent` transition before parking.

The exact post-handshake stress commands were:

```sh
GOFLAGS=-buildvcs=false go test ./internal/asyncrt -count=100 -timeout=3m -run 'TestChannelRendezvous|TestChanCanSendFiltersReceiveGenerations|TestChannelReceiveGenerationOverflowFailsClosed'
GOFLAGS=-buildvcs=false go test ./internal/asyncrt -count=100 -timeout=3m -run 'TestChannelRendezvous'
GOFLAGS=-buildvcs=false go test -race ./internal/asyncrt -count=10 -timeout=4m -run '^TestChannelRendezvousReleaseCreditRetiresLateParkSubscription$'
```

Results: PASS, `ok surge/internal/asyncrt 0.093s`,
`ok surge/internal/asyncrt 0.041s`, and
`ok surge/internal/asyncrt 1.017s` respectively.

Independent review reproduced the adjacent Rule 13 RED with a disposable Go
overlay. `/tmp/surge-d8-final-review/late_park_mutant_overlay.json` replaced
`internal/asyncrt/asyncrt.go` with
`/tmp/surge-d8-final-review/asyncrt_late_park_mutant.go`; that file and the old
index blob `d2a4fb61` both hashed to
`bf9d80d8554f661f95ea47c2e66fc651e84147ff22a8841081c024b4b6f12125`.
The exact test command was:

```sh
SURGE_STDLIB=/tmp/surge-d8-lifecycle-fix.Lfnik1 GOFLAGS=-buildvcs=false go test -overlay=/tmp/surge-d8-final-review/late_park_mutant_overlay.json ./internal/asyncrt -run '^TestChannelRendezvousReleaseCreditRetiresLateParkSubscription$' -count=1 -v
```

Result: expected FAIL, `FAIL surge/internal/asyncrt 0.003s`; both `same-key`
and `unrelated-key` failed with `late park left a subscription beside durable
ready credit`. The overlay was disposable and production bytes were unchanged.

After completing the Rule 10 and Rule 3 evidence text, the exact commands
listed above were rerun unchanged as the final proportionate gate. Results were
focused asyncrt `0.005s`, focused VM `0.033s`, full asyncrt `0.006s`, targeted
asyncrt race `1.020s`, targeted VM race `1.227s`, vet exit 0, the 100-count
matrix `0.098s`, and the 10-count late-park race `1.020s`. The exact combined
local-select command recorded above also passed in `12.938s`; its Valgrind
subrow took `2.07s` and again reported zero direct/indirect bytes and blocks in
both lanes.

The first broad hook run is INVALID environment evidence, not a product red:
it omitted `SURGE_STDLIB` and reproduced the branch's unpinned-stdlib LLVM and
layout failures on the exact prior index. The accepted Rule 16 invocation was
this exact command:

```sh
SURGE_STDLIB=/tmp/surge-d8-lifecycle-fix.Lfnik1 GOFLAGS=-buildvcs=false ./scripts/pre-commit
```

The final post-handshake rerun passed all packages (full VM 101.255 s), lint with
0 issues, C formatting and strict warnings, and the effective-size gate with
25/25 files OK and no legacy or over-limit file. The cumulative lint
differential is main `d8e100d7`: 0, prior D8 index: 48, lifecycle candidate
before cleanup: 52, final: 0.

The final Sentrux tool sequence was
`mcp__sentrux__scan(path=/tmp/surge-d8-lifecycle-fix.Lfnik1/internal)`,
`mcp__sentrux__health`, `mcp__sentrux__check_rules`, and
`mcp__sentrux__session_end`. The scoped scan improved 6454 -> 6455; health
reported 2,590 cross-module edges of 2,877 and `modularity` as the bottleneck.
Root-cause score/raw pairs were acyclicity 10000/0, depth 6154/5, equality
5164/0.4836009522, modularity 3712/0.0568286419, and redundancy
9498/0.0502083333. Rules passed 7/7 with zero violations. `session_end`
measured signal +1 but returned `pass=false` because its secondary count of
complex functions rose 645 -> 647; the Rule 3 lower-signal blocker is absent,
and this secondary warning is retained explicitly rather than hidden.

The repository-root sequence used
`mcp__sentrux__scan(path=/tmp/surge-d8-lifecycle-fix.Lfnik1)`,
`mcp__sentrux__health`, and `mcp__sentrux__check_rules`. The scan improved
6152 -> 6153; health reported 2,854 cross-module edges of 3,248 and again
identified `modularity` as the bottleneck. Root-cause score/raw pairs were
acyclicity 10000/0, depth 5714/6, equality 4704/0.5296070280, modularity
3947/0.0921005386, and redundancy 8315/0.1685102377. Rules passed 8/8 with
zero violations. A direct `session_end` after switching from the scoped scan
was invalid cross-scope evidence and is discarded; the corrected same-root
`mcp__sentrux__session_start` -> `mcp__sentrux__session_end` closure passed at
6153 -> 6153 with delta 0 and no violations.

The heavy Runtime V2 aggregate and load stands remain dedicated-runner work
and were not run from this mutable development worktree.

### Wave D integrated structural-owner reconciliation

Replaying D8 under the stronger package-structural scanner exposed eight paths
that its original local scanner could not classify. Six terminate at the
already-known legacy `Frame.Locals -> LocalSlot.V -> Value` leaf through the
new exact owner region and registry. They are migrations under RV2-DEBT-318,
not allowances; the live count is now 27. The retired direct `VM.Async->Value`,
`tagScrutinee.value->Value`, and `cloneValueComposite` migration entries are
absent and the manifest test rejects their reintroduction.

The other two paths reached existing generic owners through
`ChannelSendReservation.exec` and `.channel`. The approved model answers this
without a new language or UX choice: the reservation is a control claim, not
publication and not a payload owner. The scanner therefore treats only the
exact nominal `internal/asyncrt.ChannelSendReservation[P Payload]` declaration
as a terminal, with its complete eight-field control shape. Adding either `P`
or `Payload`, renaming the declaration, weakening `recvSeq`, or reaching the
same shape through an unrecognized declaration fails closed and restores the
ordinary pointer walk.

Rule 13 was measured before adding the terminal. This exact command:

```sh
GOFLAGS=-buildvcs=false go test ./internal/carriergate -run '^TestStructuralOwnerCensusRecognizesExactChannelSendControlClaim$' -count=1 -v
```

exited 1: the exact row reported both
`ChannelSendReservation.channel->Channel.buf->universal` and
`ChannelSendReservation.exec->Executor.tasks->Task.State->universal`, plus the
transitive envelope path. The payload-field, renamed-declaration, and weakened
shape controls already passed. After the exact structural terminal, all five
rows pass and the complete live carrier ratchet is green. RV2-DEBT-319 records
the distinct value-copyable one-shot-bit defect; it is nonblocking and is not
hidden by this payload-ownership classification.

A final integrated review added the canonical-directory negative control before
changing production. It exited 1 with `nested package spoof became a control
terminal`: a declaration under `internal/asyncrt/sidecar` using the same Go
package name lost both reservation paths. Requiring `decl.file.pkg == graph.root`
makes that row fail closed as well; the exact-control matrix now has six green
rows. The exact final pinned Rule 16 rerun after this guard also passed: full
VM `100.213s`, lint zero issues, strict C checks green, and Rule 4 green.

Integrated verification from
`/tmp/surge-rv2-closeout.gaHvNP/worktree` is green: the 100-count rendezvous
matrix (`0.097s`), focused direct/routed VM close and exact-once rows (`0.034s`),
targeted late-park race count 10 (`1.018s`), full carrier race (`32.817s`), and
focused vet all passed. The pinned Rule 16 command
`SURGE_STDLIB=/tmp/surge-rv2-closeout.gaHvNP/worktree GOFLAGS=-buildvcs=false ./scripts/pre-commit`
passed the full package sweep (`internal/vm` `103.590s`), lint with zero issues,
strict C checks, and Rule 4. Sentrux reports `internal` signal 6458 with all
7 rules green and repository signal 6158 with all 8 checked rules green; the
same-root session closes 6158 -> 6158 with `pass=true`.

### RV2-DEBT-248 deterministic second-token-abort proof

Started from exact integrated base `6bd9ae091aa3d8c54c81cbc0259c3ab6c253d6bb`
in the isolated `codex/rv2-debt248` worktree. The production guard already
refuses a waiter removal when the second `park_current` token-abort branch has
no park generation. What remains open is the row's rate-only evidence.

One after-requeue hook cannot select that branch: a token already present at
`park_current` entry takes the first abort. The deterministic drive therefore
uses two test-only points. The first holds a join park after its initial token
exchange returned zero, with the join registration already prepared but before
`park_current` mutates the prepared fields or commits WAITING; the driver wakes
the still-RUNNING task there. The second holds that same poller after the second
exchange consumed the token, requeued the task as READY, and released the owner
lock, but before any waiter removal. A second carrier then re-polls the task,
publishes a fresh join registration, and pauses inside the poll. The positive
build must preserve both old and fresh entries for the target completion drain;
`RV2_DEBT_248_NEGATIVE_CONTROL` restores only the old unqualified `seq == 0`
removal and must report `target DONE + joiner WAITING + zero registrations`
immediately, never use a timeout as its success oracle.

The lane does not change waiter ownership, token semantics, channel code,
compiler lowering, ABI, or language/UX surface. Focused lifecycle positives,
the Rule 13 control, sync-point placement/release-symbol checks, C checks, lint,
file-size enforcement, and the mandatory pre-commit sequence are owed before
the row can close. Heavy aggregate evidence remains pinned dedicated-runner
work on the final integrated SHA.

The first focused run exposed a stand-only scheduling fact instead of being
waived: the second abort requeues onto the held poller's local deque, and its
single entry deliberately does not signal a sibling. A permanently READY
gated target also kept the inject queue nonempty, so the fresh poll did not
start. The final stand holds its target inside a poll and, after observing the
after-requeue point, uses the existing test driver's shard wake to wake two
sleeping siblings without injecting competing work. A sibling then has exactly
the requeued joiner to steal. No production scheduling policy changed.

Focused evidence is green. The exact positive + Rule 13 command passed once in
`9.763s`, then passed five consecutive runs in `54.593s`:

```sh
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2LifecycleDebt248(SecondTokenAbortKeepsFreshJoinWaiter|SecondTokenAbortNegativeControl)$' -count=5 -parallel=1 -p=1 -v --timeout 300s
```

The positive requires the exact structural report
`debt248 windows: initial=2 abort=1 entries_before=2 entries_parked=2 attempts=2`
and the measured completion `kind=1 bits=42 attempts=2`. The negative build
must instead observe `target=done joiner=waiting entries=0 attempts=2` and fail
with `debt248 negative control swept the fresh join registration`; the Go test
passes only when that failure occurs for exactly this reason.

`./check_sync_points.sh` passed the enumerator/name/window census, tag-off
symbol absence, default-target guard, and analysis-only stand-flag guard.
`EPIC_BASE=6bd9ae091aa3d8c54c81cbc0259c3ab6c253d6bb
./scripts/runtime_v2_file_size_check.sh --worktree` passed six measured code
files with zero violations; the largest changed stand is 435 effective LOC.
The remaining C, lint, focused-roster, pre-commit, Rule 4, and generated stats
evidence is appended after those gates run.

Native checks are green: `make cfmt-check` (`3.924s`), `make c-check`
(`8.467s`), and
`make c-check-changed C_CHANGED='runtime/native/rt_sync_point.c
runtime/native/rt_sync_point.h runtime/native/rt_task_park.c'` (`1.657s`)
all passed. The required whole-tree analyzers also passed: `make cppcheck`
checked 110 files and ended `cppcheck OK`; `make ctidy` ended
`clang-tidy check OK`. Tagged `GOFLAGS=-buildvcs=false go vet -tags
runtime_v2_pending ./internal/vm` passed with no output. The first `make lint`
attempt was refused before analysis because another lane already held
golangci-lint's process lock (`parallel golangci-lint is running`); after that
process exited, the identical `GOFLAGS=-buildvcs=false make lint` command passed
with `0 issues`.

The composed `make runtime-v2-lifecycle-check` was not run locally or claimed
green: its `heavy-run-guard` correctly refused this non-dedicated host before
starting because `/etc/surge-dedicated-runner` is absent. Rule 19 requires a
commit-pinned worktree on the dedicated server; the aggregate therefore remains
post-commit/final-SHA work. The exact two selected lifecycle rows are the local
focused evidence above, and their explicit Makefile alternation is part of this
diff rather than being inferred from a family prefix.

Sentrux on the isolated worktree is green. Repository root scan:
`quality_signal=6158`, bottleneck `modularity`, all 8 checked rules pass.
`runtime/native` scope: `quality_signal=5434`, bottleneck `redundancy`, all 7
rules pass. The scoped session baseline was saved at 5434 and will be closed on
the same absolute path after the final diff.

The staged mandatory pre-commit was run as
`SURGE_STDLIB=/tmp/surge-rv2-debt248.xGYZTb/worktree
GOFLAGS=-buildvcs=false ./scripts/pre-commit` and passed end to end. Its staged
three-file C analysis passed first; `make check` then passed the entire package
sweep (`internal/vm` `121.568s`), lint with `0 issues`, strict C format/compile,
and the repository file-size check with all three changed native files OK.
There were no ignored failures. The hook generated the expected `STATS.md`
update: native code `39002 -> 39023`, test code `134680 -> 134943`, total
`376013 -> 376297`. The checked-in file and two fresh generator streams all
have identical SHA-256
`7cf8176f299cd3770d81dbc2eceb3a953db9809d8e4eabb41c39c64fcf568205`.

Final Sentrux closure used the same absolute `runtime/native` path as its
session baseline: `5434 -> 5434`, delta zero, `pass=true`; health still names
`redundancy` and all 7 rules pass. The final repository-root rescan remains
`quality_signal=6158`, bottleneck `modularity`, with all 8 checked rules green.
After the first evidence append, the staged pre-commit was rerun and passed
again: VM `105.084s`, lint `0 issues`, strict C checks green, and Rule 4 green.
This final sentence is documentation-only; no code or generated artifact
changed after that accepted run.

### Wave D DEBT-307 normative closure — the model was missing a piece

Written 2026-09-02 from exact integration base
`b5e4ae73188c3ade467678a7c18197f8300afa10` in the isolated
`codex/rv2-debt307-normative` worktree. Documentation only: no `.go`, `.c`,
`.h`, `.sg` or generated artifact changed in this commit.

Two independent read-only reviews of the preserved DEBT-307 partial
implementation agreed on something the closeout plan had not accounted for.
The plan's step 4 read "prove affinity, delete `emitAsyncRefParamBox`, pass a
real borrow pointer". That is not sufficient and could not have been: an
ordinary local place is a per-poll `alloca`, suspension packs live values into
task state, and a later poll unpacks them into NEW allocas. A child holding the
original pointer dangles after its parent suspends **even on the same carrier**.
Common carrier is necessary and is not sufficient, so the row was never an
implementation detail — it was a missing piece of the model.

The two documents also disagreed with each other, which is how the gap stayed
invisible. `RUNTIME_V2.md` Section 9 permits a local `spawn` to capture a borrow
of its parent; Section 7 of the storage model forbade storing an ordinary `&T`
or `&mut T` in any owner that can outlive or suspend the borrow, without
exception and without defining an address-stable parent place. One of them had
to move. The owner ruling of 2026-09-02 moves Section 7, narrowly, and this
commit is that closure.

**What Section 7 now says.** The sole exception is a compiler-proven
carrier-affine child-task borrow whose referent is a parent place promoted to
address-stable fixed-offset parent async-state storage; the reference is valid
only while parent storage and the child affinity/lifetime pin are valid, and it
still may not cross carriers, enter blocking or crossing transport, or escape
the structured scope. Promotion is PLACE-oriented, never type-oriented: the
capture set is known before the spawn is lowered, so exactly the borrowed places
are promoted and nothing else. No new category of heap-boxed locals is
introduced, and no async local is heap-promoted for merely living across an
await. The invariant is that a place borrowed by a live carrier-affine child has
ONE stable storage identity from the child's publication until its true
completion; its consequences — no address change between polls, no copy back
into a fresh `alloca`, no reuse of the field as a different logical place while
the borrow lives — are recorded as normative rather than as guidance.

**Why the region is named and not the symbol.** Section 7 says "stable task
activation storage", not `__AsyncState$`. That is deliberate. The root
activation needs promoted places on exactly the same terms, and a rule written
against the async frame symbol would have been copy-pasted, divergently, for the
root within two steps.

**The pin is flow state.** `UnpinForTask` mutating a global table at a syntactic
`await` is refused as a mechanism, because it releases the pin for the paths on
which that await never ran. After `let t = spawn child(&x); if cond { t.await();
} mutate(x);` the borrow is still live at `mutate(x)`. The rule is definite
completion on every reachable incoming path, merged as a may-be-live lattice
(`ACTIVE + RELEASED -> ACTIVE`) through the same snapshot and merge discipline
that already carries ownership flow. Handle move state and referent pin state
stay different facts: the handle may be consumed on one path while the referent
stays borrowed.

**The scheduler half needed no new rule, and that is the finding.** Section 10
already requires that publication and notification are addressed to the
eligibility CLASS, never to the group and never to a thread outside it, and that
a carrier-affine task is normally a singleton class whose only eligible worker
is its carrier. The reviewed implementation published an affine task into a
worker-private deque under a generic group wake credit, which contradicts that
sentence directly — an ineligible waiter consumes the only credit, refuses the
task, and the eligible carrier sleeps. So P0 #1 is an implementation violating
existing normative text, not a gap in it. The same is true of the shutdown half:
Section 10 already says the exiting carrier runs or cancels every task pinned to
it and the group closes only when no carrier-affine task remains. Both are cited
in Section 9 now so the next reader does not re-derive them.

**`main` is given a carrier rather than a refusal.** The reviews exposed a
question the plan did not carry: a synchronous entrypoint runs outside executor
carriers, so "affine to the parent's carrier" had no referent for the one
context every program starts in. Refusing borrow-capturing `spawn` from a
non-carrier context was the cheaper option and was rejected on evidence:
`docs/QUICKSTART.md:555-564` already shows a synchronous `@entrypoint fn main()`
doing `spawn producer(&ch)` and `spawn consumer(&ch)`, and `await` is already
permitted in an entrypoint (`docs/CONCURRENCY.md:197`). The refusal would have
withdrawn working documented UX to avoid naming a carrier the runtime already
has. Instead `@entrypoint` executes as the runtime's synthetic root task on an
initial carrier. This is a lowering and runtime property: `fn main()` stays
`fn main()`, nothing on the source surface becomes `async`, and no attribute is
added. `docs/LANGUAGE.md:171` — borrowed references may not cross worker-thread
boundaries — is unchanged and uncontradicted, because a carrier-affine child
never crosses one.

**The forbidden intermediate state.** Both documents now say it outright: a tree
in which semantic analysis has admitted the borrow and the scheduler has pinned
the task while the lowered pointer is still a per-poll `alloca` is forbidden. It
is worse than the refusal it would replace, because it reports a proof nothing
holds. The existing refusal therefore stands until promoted storage,
path-sensitive pin state and carrier-addressed publication are all live
together, and the closeout plan's step 4 is rewritten as the eight-step ordered
vertical that reaches that state. RV2-DEBT-303 is routed rather than closed: the
box is not taught to retain, it is deleted at step 7 once a real borrow pointer
into promoted storage replaces it, so 303 closes with 307 and not before it.

The preserved partial implementation in the DEBT-307 lane is NOT committed, per
the reviews. Its two P0s are mandatory parts of the same vertical, not
follow-ups.

*Reading order note, added at integration.* The harness-containment section
below is dated 2026-09-01 and belongs to Wave D step 2, so it precedes this one
in both wall-clock and plan order. It appears after it because the two lanes
branched from the same commit and appended at the same anchor, and the merge
kept both rather than reflowing a normative log to fix an ordering that every
section already states in its own first line.

## 2026-09-01 — Wave D remote-harness timeout containment (in progress)

The exact tagged `internal/vm` run at code SHA `d2956347` stalled in
`TestRuntimeV2RemoteTaskBehavior/select-spurious-caller-wake-mints-no-second-request`.
The focused row reproduced the stall and Go's timeout left the native C child
under PID 1. This is red liveness evidence for the product path; this lane does
not resolve or close that defect.

The bounded infrastructure task starts from `163b9cda` in isolated worktree
`/tmp/surge-rv2-harness.rcryLN/worktree`. Its proof is a shared Go subprocess
runner that owns a child from `Start` through `Wait`, gives it a distinct
supported-Unix process group, and on context cancellation sends group
`SIGTERM`, then bounded
`SIGKILL`. A self-reexec regression must create a synchronized child and
grandchild, time out, and prove that neither PID survives. A second row must
force the TERM-to-KILL escalation. Rule 13 will replace the group signal with a
direct-child signal in a scratch tree; the descendant test must then report the
surviving grandchild. No runtime protocol or product semantics are in scope.

Pre-edit Sentrux evidence on the isolated worktree was green. The repository
root reported `quality_signal=6160`, bottleneck `modularity`, with all eight
checked rules passing. The scoped `internal/vm` scan reported
`quality_signal=7192`; that exact absolute path is the saved session baseline.

The first focused infrastructure run passed in `4.062s`, and the identical
post-split run passed in `4.064s`:

```sh
SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
  -run '^(TestRunCommandWithCancellation(ReapsDescendantProcessGroup|EscalatesToKill)|TestRunBinaryWithTimeoutReportsEmptyOutputDiagnostics|TestProcessGroupCancellationHelper)$' \
  -count=1 -parallel=1 -p=1 -v --timeout 30s
```

The descendant row reaches its two-second context deadline only after the
child has received a byte-level readiness acknowledgement from its grandchild.
The child reaps the grandchild before returning, and the Go parent both waits
for the child and proves `ESRCH` for both recorded PIDs. The escalation row
first writes its ready marker after installing `SIGTERM` ignore, then the test
requires a `SIGKILL` wait status after the 50 ms grace. No sleep is used as a
readiness or survivor assertion.

The pre-change red Ryzen row was also rerun once from this local worktree with
the new bounded runner. It completed green rather than reproducing, in
`5.830s` total (`0.02s` in the selected row), and no matching harness process
remained:

```sh
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 \
  go test -tags runtime_v2_pending ./internal/vm \
  -run '^TestRuntimeV2RemoteTaskBehavior$/^select-spurious-caller-wake-mints-no-second-request$' \
  -count=1 -parallel=1 -p=1 -v --timeout 45s
```

That single local pass does not supersede the server failure or close the
product liveness blocker; it only verifies that the focused harness still runs
normally through the shared cancellation path.

The two cancellation regressions then passed five consecutive runs in
`20.284s`. A focused race build passed both rows in `5.326s`, and the tagged VM
package vet plus diff whitespace check passed with no output:

```sh
SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
  -run '^TestRunCommandWithCancellation(ReapsDescendantProcessGroup|EscalatesToKill)$' \
  -count=5 -parallel=1 -p=1 --timeout 60s
SURGE_SKIP_TIMEOUT_TESTS=0 go test -race ./internal/vm \
  -run '^TestRunCommandWithCancellation(ReapsDescendantProcessGroup|EscalatesToKill)$' \
  -count=1 -parallel=1 -p=1 -v --timeout 60s
GOFLAGS=-buildvcs=false go vet -tags runtime_v2_pending ./internal/vm
git diff --check
```

Rule 4 is green against exact base `163b9cda`: seven measured Go files, zero
violations. Both pre-existing over-limit files shrink (`mt_executor_test.go`
1458 to 1450 physical lines; `runtime_v2_remote_publication_test.go` 503 to
496), and every new file is at most 191 effective LOC.

Rule 13 is proven on detached scratch tree `53bd8251`, created from the exact
staged index. The only mutant changed the Linux group target from
`syscall.Kill(-cmd.Process.Pid, signal)` to
`syscall.Kill(cmd.Process.Pid, signal)`. The named descendant test failed with
exit 1 after `2.259s` and the exact report:

```text
grandchild process 3341886 survived process-group timeout
```

The test's cleanup then killed the surviving process group; PID `3341886` was
absent before the scratch worktree was removed. Thus the positive test is not
vacuous: it distinguishes process-group cleanup from the former direct-child
behavior and observes exactly one escaped descendant in the mutant.

Final Sentrux closure on the same absolute `internal/vm` scope improved
`quality_signal` from `7192` to `7194` (`pass=true`, delta reported as `+3` by
the integer API). Health still names `modularity`; the scope has no local
`.sentrux/rules.toml`, so architectural constraints were checked at repository
root instead. The final root scan improved `6160 -> 6161`, still names
`modularity`, and all eight checked repository rules pass.

The staged mandatory hook passed end to end as:

```sh
SURGE_STDLIB=/tmp/surge-rv2-harness.rcryLN/worktree \
  GOFLAGS=-buildvcs=false ./scripts/pre-commit
```

`make check` completed the full package sweep (`internal/vm` `118.097s`),
golangci-lint reported `0 issues`, strict C format/compile checks passed, and
the repository file-size check passed. No C source or header is staged, so the
staged-C-only analyzer correctly had no input. The hook generated and staged
the expected `STATS.md` update: test files `652 -> 657`, test LOC
`135171 -> 135576`, and total LOC `376603 -> 377008`.

## 2026-09-02 — Independent-review repair: leader Wait is not group retirement

Independent review correctly rejected the first implementation. Its
`terminateCancelledCommand` returned as soon as the direct child's `Wait`
finished during the TERM grace. A leader can exit promptly while a descendant
which detached stdio and ignores TERM remains in the same process group, so
that direct `Wait` is not a process-tree retirement point.

The revised cancellation path always holds the complete bounded TERM grace,
sends group `SIGKILL` even when the leader was already reaped, waits for the
leader if necessary, and then requires the process group to disappear within a
bounded one-second post-KILL probe. A failed group signal still falls back to
the direct child but is returned as an error because descendant cleanup can no
longer be guaranteed.

The new deterministic Linux row starts a process-group leader whose default
SIGTERM disposition exits immediately. That leader starts a grandchild with
stdio on `/dev/null`; the grandchild installs SIGTERM ignore before its
readiness byte. `CLONE_PARENT` makes the grandchild directly reapable by the Go
test without changing its inherited process group. After timeout, the row
requires leader status `SIGTERM`, grandchild status `SIGKILL`, and `ESRCH` for
both recorded PIDs after their respective waits.

An intermediate fixture asked a race-instrumented Go signal handler to exit the
leader inside the 250 ms production grace. It passed alone but failed in the
three-row race run because instrumentation sometimes delayed that handler until
the group KILL. The final fixture uses the kernel's default SIGTERM disposition
instead; product grace and runner semantics did not change.

All three cancellation rows passed five consecutive final runs in `32.812s`.
The same three rows passed together under the race detector in `7.589s`:

```sh
SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm \
  -run '^TestRunCommandWithCancellation(ReapsDescendantProcessGroup|KillsTermResistantDescendantAfterLeaderExit|EscalatesToKill)$' \
  -count=5 -parallel=1 -p=1 --timeout 90s
SURGE_SKIP_TIMEOUT_TESTS=0 go test -race ./internal/vm \
  -run '^TestRunCommandWithCancellation(ReapsDescendantProcessGroup|KillsTermResistantDescendantAfterLeaderExit|EscalatesToKill)$' \
  -count=1 -parallel=1 -p=1 -v --timeout 60s
```

Process-group targeting is no longer Linux-only. The implementation build
constraint explicitly covers `aix`, `darwin`, `dragonfly`, `freebsd`, `linux`,
`netbsd`, `openbsd`, and `solaris` (which also covers the Go illumos port); the
direct-child fallback is the exact complement. The tagged VM test package
cross-compiled successfully for experimental macOS support:

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 GOFLAGS=-buildvcs=false \
  go test -c -tags runtime_v2_pending ./internal/vm -o /dev/null
```

The final Rule 13 tree is detached scratch commit `ca8c7795`. Its only mutant
restored the rejected early return from the leader's `Wait`, before the grace
expired or group KILL ran. The new row failed with exit 1 after `3.011s`:

```text
grandchild process 3426525 survived cancellation after leader Wait
```

The regression cleanup then killed and reaped that exact grandchild; PID
`3426525` was absent before the scratch worktree was removed. This is separate
from the earlier group-target mutant: one pins `-PGID` rather than direct PID,
and the other pins continued ownership after the group leader is gone.

Tagged VM vet and Rule 4 are green on the revised tree. The exact-base file
size gate measured seven Go files with zero violations; the largest new test is
329 effective LOC, and the two pre-existing over-limit files still shrink.
Build selection was also inspected explicitly: Darwin and illumos select
`subprocess_cancellation_unix_test.go`, while Windows selects the direct-child
fallback.

```sh
GOFLAGS=-buildvcs=false go vet -tags runtime_v2_pending ./internal/vm
EPIC_BASE=163b9cda ./scripts/runtime_v2_file_size_check.sh --worktree
for task_os in darwin illumos windows; do
  CGO_ENABLED=0 GOOS="$task_os" GOARCH=amd64 \
    go list -tags runtime_v2_pending -f '{{.XTestGoFiles}}' ./internal/vm
done
```

The previously red remote-task row again completed normally through the
revised runner (`5.725s` total, `0.03s` selected row), and the preserved
non-zero/empty-output diagnostics row passed in `0.007s`. The remote pass is
still only harness compatibility evidence, not closure of the intermittent
product liveness blocker.

The revised Sentrux scan improves the original same-worktree `internal/vm`
baseline `7192 -> 7195`; the bottleneck remains `modularity`. Repository root
remains `quality_signal=6161`, and all eight checked architectural rules pass.
The scoped directory still has no local rules file, as recorded in the
first-pass closure above.

The first revised mandatory-hook attempt rejected an unused `stdin` parameter
on the shared helper (`unparam: stdin always receives ""`). The helper and all
call sites were narrowed rather than suppressing the lint. The three
cancellation rows plus the preserved diagnostic then passed again in `6.581s`,
and scoped golangci-lint reported `0 issues`.

The mandatory hook subsequently passed end to end on the final staged tree:

```sh
SURGE_STDLIB=/tmp/surge-rv2-harness.rcryLN/worktree \
  GOFLAGS=-buildvcs=false ./scripts/pre-commit
```

The full package sweep passed (`internal/vm` `101.019s`), repository
golangci-lint reported `0 issues`, strict C formatting and compilation checks
passed, and the repository file-size check passed. The exact-base Runtime V2
file-size gate was also rerun after the lint repair and still reports seven Go
files with zero violations. Final `STATS.md` values relative to base
`163b9cda` are test files `652 -> 657`, test LOC `135171 -> 135759`, and total
LOC `376603 -> 377191`.

## 2026-09-02 — Quality-review repair: hold leader identity until group kill

Quality review rejected commit `af44fb28`. Although it delayed group
`SIGKILL` until the full TERM grace, its concurrently running `cmd.Wait()`
could reap the leader first. A later `kill(-oldPID, SIGKILL)` could therefore
target a reused process-group ID. The subsequent `kill(-oldPID, 0)` probe was
also a TOCTOU check rather than an identity hold.

The accepted design narrows descendant-safe process-group cleanup to Linux.
The pinned `golang.org/x/sys/unix` dependency exposes a supported
`Waitid(..., WNOWAIT)` wrapper on Linux but not on Darwin. Adding raw Darwin
ABI calls or a supervisor solely for the test harness was explicitly rejected
as an unapproved portability/complexity expansion. Non-Linux builds now use a
bounded direct-child lifecycle and do not promise descendant cleanup.

On Linux a blocking `waitid(P_PID, leader, WEXITED|WNOWAIT)` observes exit
without reaping. If cancellation wins, the runner sends group `SIGTERM`, holds
the complete grace, sends final group `SIGKILL`, and only then calls
`cmd.Wait()`. An exited leader therefore remains a zombie holding its numeric
PID/PGID identity until the last group signal has been decided. The former
group-existence probe is gone. If natural exit wins the context race, the
runner immediately calls `cmd.Wait()` and performs no later group signal.

The early-leader regression now installs a hook at the exact pre-SIGKILL
point. A second `Waitid` with `WNOWAIT|WNOHANG` proves the leader is still
waitable, and `kill(leader, 0)` proves the identity is still present; only then
does the real group kill run. The test continues to require leader `SIGTERM`,
TERM-resistant grandchild `SIGKILL`, and `ESRCH` after both are reaped. This
checks the dangerous ordering without attempting an actual PID-reuse attack.

All command paths set a one-second `Cmd.WaitDelay`, bounding inherited output
pipes after the leader exits. The descendant cleanup contract covers only
processes which remain in the inherited process group. A synchronized
`setsid` regression deliberately escapes a grandchild while it retains the
leader's stdout/stderr. The runner returns through `WaitDelay`; the test proves
the escaped process is still alive (and therefore outside the kill contract),
then kills and reaps it deterministically.

Signal calls are injectable only through the internal Linux test lifecycle.
The failure regression makes both group and direct TERM/KILL calls return
sentinel errors while a TERM-resistant leader remains live. The public helper
returns after the bounded post-KILL wait with the combined signal error and an
explicit `reap continues in background` error. The test then performs the real
group kill and proves the one eventual `cmd.Wait()` reaps the leader. Output
capture uses a mutex-protected buffer so taking the bounded-return snapshot is
race-free while the eventual Wait still owns the exec copy goroutines. This
exception is explicit: if every SIGKILL operation fails, bounded return and
guaranteed immediate reap are physically incompatible; there is nevertheless
exactly one eventual Wait owner and no double Wait.

The five cancellation rows passed five consecutive runs in `44.152s`. The
same rows plus preserved empty-output diagnostics passed together under the
race detector in `9.910s`:

```sh
GOFLAGS=-buildvcs=false go test -tags runtime_v2_pending ./internal/vm \
  -run '^TestRunCommandWithCancellation(ReapsDescendantProcessGroup|KillsTermResistantDescendantAfterLeaderExit|EscalatesToKill|SignalFailureReturnsBoundedly|BoundsEscapedInheritedPipe)$' \
  -count=5 -parallel=1 -p=1 --timeout 120s
GOFLAGS=-buildvcs=false go test -race -tags runtime_v2_pending ./internal/vm \
  -run '^(TestRunCommandWithCancellation.*|TestRunBinaryWithTimeoutReportsEmptyOutputDiagnostics)$' \
  -count=1 -parallel=1 -p=1 -v --timeout 90s
```

The first race run correctly exposed that embedding `bytes.Buffer` promoted
its unlocked `ReadFrom`, bypassing the synchronized `Write`, and that the new
setsid test read `cmd.Process` concurrently with `Start`. Composition removed
the promoted method and the result channel now establishes the required
happens-before edge; the quoted final race run is after both repairs.

Tagged VM vet and scoped golangci-lint pass (`0 issues`). Rule 4 passes against
exact base `163b9cda` with eight measured Go files and zero violations; the
largest new file is 359 effective LOC. The tagged VM test package still
cross-compiles for Darwin, but build selection now deliberately chooses
`subprocess_cancellation_linux_test.go` only on Linux and
`subprocess_cancellation_other_test.go` on Darwin, illumos, and Windows.

Rule 13 uses exact-index scratch commit `09bb6d8b`. Its only mutant removed
`WNOWAIT` from the Linux exit observer, allowing that observer to reap the
leader before group `SIGKILL`. The identity regression failed with exit 1 in
`2.259s`:

```text
cancel process group after leader exit: run=waitid: no child processes
signal=observe unreaped leader before group SIGKILL: no child processes
```

The test cleanup killed and reaped the TERM-resistant grandchild; no `vm.test`
process remained after the mutant run. The previously red remote-task row also
completed normally through the final runner (`5.606s` total, `0.02s` selected
row). As before, that is harness compatibility evidence only and does not
close the intermittent product liveness blocker or any Runtime V2 epic.

Final Sentrux evidence on the same isolated paths is green. `internal/vm`
improves the original baseline `7192 -> 7196`, with `modularity` still the
bottleneck. Repository root remains `quality_signal=6161`; all eight checked
architectural rules pass with zero violations.

The mandatory hook passed end to end on the final quality-review tree:

```sh
SURGE_STDLIB=/tmp/surge-rv2-harness.rcryLN/worktree \
  GOFLAGS=-buildvcs=false ./scripts/pre-commit
```

The full package sweep passed (`internal/vm` `100.302s`), repository
golangci-lint reported `0 issues`, strict C formatting and compilation passed,
and the repository file-size check passed. The hook generated and staged the
final `STATS.md`: relative to base `163b9cda`, test files `652 -> 658`, test LOC
`135171 -> 136159`, total files `1755 -> 1761`, and total LOC
`376603 -> 377591`.

## 2026-09-02 — Spec-review repair: disarm cleanup and preserve timeout errors

Spec review rejected `9e36616d` without challenging the Linux
`waitid(WNOWAIT)` production design. It found two test/diagnostic boundary
defects. First, successful tests reaped their children and proved `ESRCH`, but
some deferred cleanups still sent `SIGKILL` to the old numeric PID or PGID.
Second, both timeout consumers selected on `contextErr` and printed only
`signalErr`, hiding a simultaneous low-level `runErr` such as the bounded
`reap continues in background` failure.

The cleanup audit removed deferred numeric group signals from the synchronous
success paths: their returned `cmd.Wait` status or explicit `ESRCH` proof is
already the ownership closure. The injected-signal failure row now arms its
group cleanup only while the leader is unresolved and disarms it immediately
after the eventual background Wait proves the PID absent. The two rows with an
explicit `Wait4` reaper now return immediately from cleanup when `reaped` is
true, bypassing both the numeric kill and the channel receive. They set that
flag immediately after the expected reap, before validating the returned wait
status. The remaining direct `Process.Kill` is the startup-failure handle, not
a stale numeric cleanup.

`runBinaryWithTimeout` now includes `runErr` in both its timeout fatal and its
saved `run.diagnostics`; the tagged remote-publication harness fatal prints the
same shared cancellation block. That block always contains both `run/wait`
and `termination`, so `signalErr=nil` cannot mask a reap/background failure.
`TestCancellationTimeoutDiagnosticsIncludeRunError` fixes the consumption
boundary with exactly that matrix and also verifies the saved `run_error:`
field.

Adding the saved field directly to `test_helpers_test.go` initially crossed
Rule 4 (`498 -> 502` effective LOC). The cohesive `runDiagnostics` record and
formatter were moved unchanged into `run_diagnostics_test.go`; the legacy file
now shrinks to 450 effective LOC. The final exact-base Rule 4 run measures
eleven Go files with zero violations.

The five cancellation rows plus the new diagnostic boundary passed five
consecutive runs in `44.160s`. The full focused matrix, including preserved
empty-output diagnostics, passed under the race detector in `9.898s`. Tagged
VM vet, scoped golangci-lint (`0 issues`), and tagged Darwin cross-compilation
also pass.

Rule 13 was repeated on the exact staged code as scratch commit `b8839faf`.
Removing `WNOWAIT` made
`TestRunCommandWithCancellationKillsTermResistantDescendantAfterLeaderExit`
fail with exit 1 in `2.256s`; the measured output was:

```text
cancel process group after leader exit: run=waitid: no child processes
signal=observe unreaped leader before group SIGKILL: no child processes
```

The test's armed cleanup completed, and no matching
`TestProcessGroupCancellationHelper` process remained. This exact test name,
exit, duration, and failure text are also required in the amended commit
message rather than living only in these notes.

The remote compatibility row remains green after the consumer change
(`5.302s` total, `0.02s` selected). Sentrux remains green at
`internal/vm=7196` and repository root `6161`; all eight checked root rules
pass. The remote pass remains test-infrastructure evidence, not product
liveness or epic closure.

The mandatory hook passed on the final spec-review tree. The full package
sweep completed with `internal/vm` in `100.151s`, repository golangci-lint
reported `0 issues`, strict C checks passed, and the repository file-size gate
passed. The generated final `STATS.md`, relative to base `163b9cda`, records
test files `652 -> 659`, test LOC `135171 -> 136187`, total files
`1755 -> 1762`, and total LOC `376603 -> 377619`.

#### The consumer boundary, and one sentence above that was broader than the code

Continued 2026-09-02 on top of `349f8a1a` in the same lane. The paragraph above
claims that `runBinaryWithTimeout` "includes `runErr` in both its timeout fatal
and its saved `run.diagnostics`". That is true of the TIMEOUT branch and was not
true of the other one: the non-timeout `execution.runErr != nil` branch printed
the error inline in its `t.Fatalf` and built its saved `runDiagnostics` record
WITHOUT `runErr`, so the file left behind for a run that failed to start, or
whose wait failed, did not name the reason. The reader of an artifact directory
after a red gate is the only reader that matters there, because the terminal
output is gone by then, and that reader got an empty stdout, an empty stderr and
no error. The sentence is corrected by the code rather than by editing it: the
record now carries `runErr` on both branches.

Rule 13 on that exact line: removing `runErr: execution.runErr` from the
non-timeout branch makes `TestRunBinaryWithTimeoutSavesNonTimeoutRunError` fail
in `0.01s` with

```text
saved run diagnostics missing "run_error: sentinel non-timeout wait failure"
```

while `TestRunBinaryWithTimeoutReportsCancellationRunError` and
`TestRunBinaryCancellationConsumerHelper` stay green in the same run. The
control is targeted rather than a build break.

The reason the gap survived the first pass is that neither real consumer was
reachable from a test. `runBinaryWithTimeout` and `runRemotePublicationHarness`
each build their own `exec.Cmd` and call `runCommandWithCancellation` directly,
so exercising their failure boundaries meant producing a genuinely unkillable
subprocess inside the test -- which is the thing this lane exists to prevent.
The seam is one package-level variable, `runHarnessCommandWithCancellation`,
initialised to the real function; the two consumers call through it and the new
tests substitute a stub answering with a chosen `runErr` and `contextErr`. No
production path and no cancellation lifecycle changed: the seam is a test
indirection over test infrastructure, and the stub never stands in for the
mechanism the other rows prove.

`TestRunCommandWithCancellationKillsTermResistantDescendantAfterLeaderExit` also
stops racing its own cleanup. It used to block a goroutine in `Wait4` on the
grandchild while both the body and `t.Cleanup` received from that one-shot
channel, so whichever arrived second waited for a value nobody would send. It
now observes the exit with the same WNOWAIT primitive the production path uses
and then reaps explicitly: observe-then-reap in the test mirrors
observe-then-reap in the supervisor, and cleanup can tell "already reaped" from
"still running" without a timeout.

Lint rejected the first version of the observe helper: `awaitObservedProcessExit`
took a `timeout` parameter that every one of its four callers passed
`subprocessKillWait` for (`unparam`). The parameter is removed rather than
silenced, because the constant is not an incidental default — it is the same
bound the supervisor gives its own final group signal, and a test allowed to
wait longer would report a pass the gate would not. The comment now says that,
so the next caller does not reintroduce the knob.

Evidence on this tree: the full cancellation family -- the five process-group
rows, the three new consumer rows, the two remote consumer rows, the diagnostics
boundary and the helper -- passed in `8.864s`; `go vet -tags runtime_v2_pending
./internal/vm` is clean; repository `make lint` reports `0 issues`; the
exact-base file-size gate against `163b9cda` passed 13 files with 0 violations,
the two new ones at 89 and 45 effective LOC. `STATS.md` is regenerated for them:
test files `659 -> 661`, test LOC `136187 -> 136372`, total files
`1762 -> 1764`, total LOC `377619 -> 377804`. The composed heavy aggregate stays
commit-pinned dedicated-runner work and was not run locally.

### Wave D seq-0 remote-reply retry-registration liveness blocker

Started 2026-09-01 from exact integration base
`163b9cdad8f12ef9fa4bcf2f0ad62a9632d05af0` in the isolated
`codex/rv2-seq0-retry-liveness` worktree.  The dedicated-runner tagged suite
stopped in
`select-spurious-caller-wake-mints-no-second-request`; the exact focused row
then reproduced the same terminal hang.  This is a Wave D liveness blocker,
not a timeout-policy defect.

The model answer is already present in `docs/RUNTIME_V2.md`: waiter
registration is published before terminal verification, and an owner terminal
event drains its waiter key.  The live path violates that rule for a
`WAKER_REMOTE_TASK_REPLY` retry.  `wake_task_with_policy` consumes the first
park, releases the task-owner lane, and defers stale-key removal.  The caller
can re-poll and append a second registration on the same key in that window.
Both entries have `seq == 0`, where `remove_waiter_generation` means
unqualified match-all; the existing DEBT-046 exception covers only JOIN, so
the delayed removal can sweep both remote-reply registrations before the
single terminal `wake_key_all` drain.  The caller is then WAITING behind no
store entry.

The intended fix is one lifecycle classification for seq-0 retry
registrations whose terminal event owns a wake-all drain.  It covers JOIN,
SCOPE, BLOCKING, REMOTE_SPAWN_REPLY, and REMOTE_TASK_REPLY; it deliberately
does not cover TIMER (deadline/by-id wake), net readiness, or channel
generation registrations.  A stale terminal-drained entry is retained until
that one terminal drain; a duplicate pop is absorbed by the task wake token.

Closing evidence is a deterministic stand using the existing
`SP_WAKE_BEFORE_STALE_REMOVAL` window: prove one first remote-reply
registration, hold the spurious wake before removal, prove the caller re-parks
with exactly two seq-0 entries, release the removal, then deliver one terminal
reply.  The transport reply counter proves enqueue only, not terminal commit.
The positive therefore requires the caller's committed result and an empty
waiter store.  A Rule-13 build restores the remote-reply sweep, waits (under a
bounded limiter) until `rt_remote_task_pending_snapshot` reports the exact OK
result, then under the one shared owner lane proves the caller is WAITING,
`task_enqueued == 0`, and its exact-key registration count is zero before the
intentional failure.  Timer/by-id policy, channel generation removal, DEBT-277
retry budgeting, transport, and language/UX are out of scope.

Implemented the lifecycle predicate as
`waker_seq0_retry_is_terminal_drained`, a reusable inline primitive beside
`waker_key`; its exact classification proof requires five terminal-drained and
six independently-completed kinds. `wake_task_with_policy` now skips only the
unqualified stale sweep for those terminal-drained seq-0 lifecycles.  The stand
proves `first=1 retry=2 after_removal=2 all_seq0=1`, then
`caller=done entries=0 requests=1 bodies=1 replies=1`.  The Rule-13 build with
`RV2_SEQ0_RETRY_NEGATIVE_CONTROL` restores only the remote-task-reply sweep and
proves `after_removal=0`, `pending=ok`, `caller=waiting`, `enqueued=0`, and zero
exact-key entries before the test accepts that exact intentional failure.  Its
first positive draft incorrectly required the remote body to remain
discoverable after its terminal retirement; that stand-only oracle was
corrected to the single request/body-id/reply counters.  Review then caught
that the reply counter itself names enqueue rather than terminal commit; the
pending snapshot and same-lane caller/store census above close that oracle gap.
No product behavior was changed for either correction.

Focused evidence is green: the new positive/negative pair passed once and then
five consecutive deterministic runs (`internal/vm`, `74.391s` for count 5);
the formerly hanging
`select-spurious-caller-wake-mints-no-second-request` row passed; and the
existing DEBT-046 positive/negative proof remained green.  `make
runtime-v2-syncpoint-check`, the exact-base file-size check, `make
c-check-changed` (8/8 files), `make c-check`, full `make cppcheck`, full `make
ctidy`, and `GOFLAGS=-buildvcs=false make check` all passed.  The first changed-C
analysis exposed const-pointer, duplicate-branch, and pre-existing ABI-padding
diagnostics; the fixture pointers, predicate form, and targeted NOLINT ABI
comment were corrected before the accepted rerun.  The composed heavy lifecycle
aggregate was not run locally: Rule 19 reserves it for the commit-pinned
dedicated runner.

Sentrux was necessarily captured late because implementation preceded this
lane's gate pass; it is not represented as a pre-edit session baseline.  Review
measured the exact parent `runtime/native` signal at 5434 and the first head at
5431, so that head was rejected.  Moving both pure key predicates --
`waker_valid` and the new lifecycle classifier -- beside `waker_key` removes
the extra module coupling without weakening the reusable API or its 5-vs-6
proof.  Final `runtime/native` reports `quality_signal=5435`, bottleneck
`redundancy`, with root-cause scores acyclicity 10000, depth 8000, equality
6074, modularity 3794, and redundancy 2573; all 7 scoped rules pass.  A fresh
scan of the immutable exact parent in a detached worktree also read 5435 (one
point above the review capture), so both the review baseline comparison
`5434 -> 5435` and the immediate same-tool comparison `5435 -> 5435` are
non-regressing.  The final same-path session baseline is 5435 and is closed
after the final diff below.

After the P1 oracle correction, the exact positive/Rule-13 pair passed once
(`11.514s`) and five consecutive deterministic runs (`59.228s`).  The retained
DEBT-046 positive shards 1/2/8 plus its negative control passed in `11.595s`.
The eight-file changed-C analysis, sync-point static gate, cfmt, diff check, and
exact-base file-size gate all passed; the new fixture is 326 effective LOC.
The final same-path Sentrux session then closed `5435 -> 5435`, delta zero,
with the same root causes and all 7 rules green.

The terminal-snapshot probe takes its own pending reference before the send, so
even an unusually late successful caller consumption cannot invalidate the
review oracle.  After that lifetime guard, the exact positive/Rule-13 pair
passed again in `11.595s`, and the changed-C analysis remained 8/8 green.  The
mandatory staged pre-commit then passed: three changed C files passed their
focused static analysis; the full package sweep passed (`internal/vm`
`103.178s`); lint reported `0 issues`; strict C format/compile and file-size
checks passed.  The hook regenerated the committed `STATS.md`; native `.c`
volume is `39101` lines and total code plus tests is `376673` lines.

#### Quality re-review: terminal-owner coverage and ABI restoration

Quality review rejected `8754e649` for two independent reasons. First, the
five-kind lifecycle predicate promised terminal wake-all ownership more broadly
than the implementation actually provided: blocking cancel, remote-spawn
abandonment, remote-task caller teardown, and queued shutdown could each win a
terminal lifecycle without draining the retained registrations. Second, making
`waker_valid` inline broke the established external C helper ABI and made the
isolated fd-registry stands fail at compile time. The final correction keeps the
reusable inline lifecycle classifier and its exact 5-vs-6 proof, but restores
the external `waker_valid` declaration and `rt_waiter_key.c` definition.

The terminal-owner matrix after the correction is:

| seq-0 key | success | cancel | abandon | shutdown |
| --- | --- | --- | --- | --- |
| JOIN | `mark_done` drains JOIN | cancelled `mark_done` drains JOIN | no separate abandon state; the task remains live until a terminal result | shutdown cancellation reaches `mark_done` and the same drain |
| SCOPE | last child (`active_children -> 0`) drains SCOPE | cancelled children converge on the same last-child transition | no detached scope operation can disappear without child exit | shutdown cancellation converges on the same last-child transition |
| BLOCKING | worker terminal CAS drains BLOCKING | poller cancel CAS winner now drains BLOCKING; the worker CAS loses | no separate abandon state; task cancellation is the terminal route | queued/unrun settlement drains BLOCKING; a running job uses its normal worker/cancel terminal CAS |
| REMOTE_SPAWN_REPLY | terminal finish drains unless abandon already won | refusal/cancel-like finish uses the same terminal finish | abandon and finish serialize under `remote_spawn_lock`; abandon-first owns the drain and finish skips, finish-first owns the drain and abandon only unlinks | fail-all reaches terminal finish and the same drain |
| REMOTE_TASK_REPLY | `pending_finish` claims and drains | CANCEL reply uses `pending_finish` and the same claim | AWAIT/CANCEL caller teardown retires only the reply-wait lifecycle, without falsely terminalizing an in-flight request | queued payload drop and fail-all share one claim, so exactly one drains |

The independent `reply_wait_retired` bit is necessary because remote-task
request status and reply-wait lifetime are not the same state: after caller
teardown the request may honestly remain in flight, while its immutable reply
key can never be registered again. Reply, teardown, queued-message release, and
fail-all all claim that bit under `rt_remote_task_state.lock`; only the winner
drains outside the lock. A pending is never reused, and its monotonic request id
plus source shard form the exact retirement key. Remote-spawn abandonment uses
the existing `abandoned`/terminal states instead of another bit. When
abandon-first claims retirement, it also takes an explicit pending ref under
`remote_spawn_lock`, so a concurrent finish may unlink and release its own refs
without invalidating the key/executor snapshot used after unlock.

Blocking cancellation stays under the caller's existing lane for its CAS. Its
terminal drain needs no lock handoff: the documented lane order permits control
then one shard, and `wake_key_all` collect-then-wakes without nesting shard
lanes. Because no lane is released, the live poller's existing lifetime remains
continuous; no unlock window or synthetic handle ref was introduced.

The new deterministic stand adds two seq-0 registrations for each missing
terminal route, executes the winning terminal action, and requires an empty
store. Its Rule-13 build omits only the new terminal drain and must first observe
exactly two stranded entries before cleaning them up and intentionally failing.
It covers blocking cancel, remote-spawn abandon-first plus finish-first ordering,
remote AWAIT and CANCEL caller teardown, and queued shutdown followed by
fail-all. The original remote-select success/Rule-13 stand and DEBT-046 proof
remain unchanged.

Focused evidence after the correction is green:

* the four new positive/mutant terminal-owner rows and the original remote
  success/mutant pair passed with the remote-spawn abandonment suite in
  `27.039s`;
* all seven DEBT-080 rows passed in `34.027s`;
* DEBT-046 positive shards 1/2/8 plus its mutant passed in `11.306s`;
* existing caller-abandon, queue-failure, pre-ack-cancel, and shutdown-waiter
  rows passed in `5.437s`;
* the three fd-registry rows that the inline ABI broke --
  `GenerationStaleSnapshotProof`, `CloseWakePollNotificationProof`, and
  `ShutdownDrainBehavior` -- passed in `0.430s` after restoration.

`make runtime-v2-fd-registry-check` and the composed lifecycle aggregate remain
commit-pinned dedicated-runner work: the local heavy-run guard refused them
before execution as required. Both new Go tests are explicitly wired into the
lifecycle aggregate. Local static evidence is green: sync-point gate, cfmt,
strict whole-runtime C compile, changed-C warning/cppcheck/clang-tidy analysis,
tagged `go vet`, `go test ./internal/gatecheck`, and diff check. The exact-base
worktree file-size gate passed 21 code/test files; the new terminal-owner
fixture is 215 effective LOC under the 500-line limit.

Sentrux history is recorded without treating tool drift as product progress.
Review captured the exact parent at 5434 and rejected the first head at 5431.
The intermediate inline-ABI head measured 5435, but that representation was
rejected by quality review. After restoring the external helper ABI, a fresh
scan of the final `runtime/native` worktree is 5434: equal to the reviewer's
captured parent, bottleneck `redundancy`, root scores acyclicity 10000, depth
8000, equality 6077, modularity 3794, redundancy 2569, and all 7 architectural
rules pass. The prior paragraph's 5435 final claim is superseded by this
post-quality scan.

The mandatory staged pre-commit passed the corrected diff end to end. Its
changed-C analysis accepted all 14 C/header files; the full package sweep passed
with `internal/vm` in `101.628s`; lint reported `0 issues`; strict C
format/compile passed; and the hook's file-size pass accepted all 14 changed C
files. It regenerated `STATS.md`: native C is 39198 lines, tests are 135300
lines, and total code plus tests is 376829 lines. The commit hook is rerun on
the final staged documentation and generated stats during amend. RV2-DEBT-320
remains open until independent re-review and the commit-pinned aggregate accept
the amended SHA.

### The W8 count's first run at the integrated candidate, and what it caught

Run 1 of `make runtime-v2-check` at the integrated candidate `86b3881c` on the
dedicated machine: **19 of 20 sub-gates pass, `runtime-v2-transport-check` fails
in 17s**. The failing row is `TestRuntimeV2RemoteTaskSourcesRespectFileLimit`,
and it is a STATIC assertion rather than a flake:

```text
runtime_v2_remote_task_static_test.go:81: rt_remote_task_pending.c has 362 lines;
    remote-task modules must stay <=360 (split it)
```

The other two lines it printed -- `rt_remote_task_dispatch.c` at 302 and
`rt_immediate_on.c` at 353 against a soft limit of 300 -- are `t.Logf`
advisories and failed nothing. Only the hard limit did.

The cause is the seq-0 lane: it added 29 lines to `rt_remote_task_pending.c`,
carrying it from 333 to 362 and over that module family's own 360-line ceiling.
That ceiling is NOT the repository-wide 500-effective-LOC gate, which passes on
the same tree -- `runtime-v2-file-size-check` reports 0 violations, at 321 and
267 effective LOC either side of this split. A module gate living inside a row
and a repository gate living beside it answer different questions, and passing
one says nothing about the other.

**Why no lane could have caught it, which is the reusable part.** The seq-0
lane's own notes record that "the composed heavy lifecycle aggregate was not run
locally: Rule 19 reserves it for the commit-pinned dedicated runner." Rule 19 is
right, and this is its cost: a lane that may not run the composed aggregate
cannot see a gate that exists only inside one of its rows. The integration count
on the dedicated machine is the first instrument that can -- which is an argument
for counting the aggregate rather than trusting a roster of per-lane greens.

**The remaining four runs were stopped rather than spent.** A deterministic
row-20 failure makes every run red, so the count cannot produce the five
consecutive greens W8 asks for, and the machine time would buy only a re-census
of nineteen rows on a SHA about to be replaced. Killing the driver script did
not stop the run: the `make` it had started survived its parent and had to be
killed by PROCESS GROUP, which is the same lesson this integration's own step 2
fixed for the C harness one level down.

**The split follows the seam, not the line count.** The gate's own comment says
to refactor by readability rather than by lines, so the cut is where the file
changes subject. `rt_remote_task_pending.c` keeps the pending object's own
lifecycle -- create, reference, release, consume, snapshot, publish a reply,
retire, finish, result source, owner flag -- every one of which already has the
pending in hand. The three functions that moved into
`rt_remote_task_pending_lookup.c` start from a TASK and walk the state's list to
find which pending speaks for it: `rt_remote_task_pending_take_owner`,
`rt_remote_task_anchored_binding_current` and
`rt_remote_task_anchored_channel_current`. They share the list and the lock with
the module they left, and not its subject.

The four followers a native split drags along were checked before the cut rather
than after it: nothing `#include`s this `.c` literally; the panic allowlist
carries no row for it; the build globs `native/*.c`, so there is no
translation-unit manifest to edit; and the one static test that reads this file
by path asserts `pending->state_owned != 0`, which lives in
`rt_remote_task_pending_release` and stays. Evidence: the previously failing row
passes, the four neighbouring transport static rows pass,
`TestRuntimeV2RemoteTaskBehavior` and `TestRuntimeV2ImmediateOnAbandonEdges`
pass together in `12.098s` -- which is what proves the new translation unit is
actually linked into the stands rather than merely compiling -- `make c-check`
passes format and strict compile, and the exact-base file-size gate passes 2
files with 0 violations. The composed aggregate is re-counted on the dedicated
machine at the new SHA rather than here.

#### W8 aggregate count at 8963b84a — 5 of 5 green, and it is measured non-vacuous

The re-count above. Recorded in full because a count that omits its own
conditions is not evidence.

| field | value |
| --- | --- |
| SHA | `8963b84a81a9267fa69098bc54721c2fe6e88c61` |
| checkout | `/root/rv2-8963b84a`, detached worktree of `/root/surge-gates` |
| delivery | `git bundle` + `scp` + `git fetch`; the candidate was never pushed |
| host | `161808.example.uk` (212.108.83.42), 16 cores |
| command | `make runtime-v2-check`, five consecutive runs |
| driver | `/root/w8-8963b84a.sh 5`, logs in `/root/w8-8963b84a/` |
| wall time | 06:36:03 -> 07:38:26 local, 1h02m |
| result | **5 of 5 `rc=0`** |

Per-run durations were 12:42, 12:25, 12:26, 12:24 and 12:25 — metronomic, with
none of the heavy tail RV2-DEBT-311 taught this epic to look for.

Non-vacuity is MEASURED rather than assumed. Every run's log carries 20 `pass`
roster rows and 0 `FAIL` rows against the roster the aggregate prints before it
starts, so a row that never ran and a row that passed cannot read alike. The
lane's `git status --porcelain` was empty at the start and is kept as a
zero-byte `tree-status.txt`, and the heavy-run guard accepted each run by SHA,
so no run measured a dirty or a different tree. The driver takes each run's
status on its own statement and never appends `; echo $?` to the lane
invocation, which has reported a false green in this epic before.

**What this count does not say.** CPU affinity was not pinned: there is no
`taskset` here, deliberately. Section 6's quiet rare-symptom campaign reserves
`taskset -c 8-15` and an otherwise-idle host because its evidence is
load-dependent; this is the deterministic roster and a different instrument.
The two are not summed and not conflated. Nor does this close Wave D: the
closeout plan's steps 4 through 8 are open, there has been no code freeze, and
the campaign runs only after one.

The earlier count at `86b3881c` is SUPERSEDED, not pooled with this one. It was
red at row 20 on the static module-size assertion, was stopped after its first
run, and its candidate is no longer the branch tip.

### Carrier-affine borrow, step 2: the pin becomes flow state

Written 2026-09-02 in the isolated `codex/rv2-debt307-pin-flow` worktree. It was
developed and measured against integration base `86b3881c` and then rebased onto
`5b44cb0c`, after the module split and the aggregate count landed; the
measurements below name `86b3881c` because that is the tree they were taken on,
and none of them touch the two files those commits changed. This is step 2 of the
eight-step vertical
the 2026-09-02 ruling put in place of the closeout plan's old step 4. It changes
no lowering and no scheduler.

**The gap is not where the row said it was.** The row attributes the silence to
routing -- an ordinary local `spawn` reaching `registerAsyncBodyOwnership` rather
than `classifyOnCapture`. Measured at `86b3881c` with `surge diag`, varying one
condition, the discriminator is the NAMED BINDING:

| program | result |
| --- | --- |
| `let t = spawn worker(&v); v = 5;` | **zero diagnostics** |
| `let r = &v; let t = spawn worker(r); v = 5;` | `SemaBorrowThreadEscape` + `SEM3019` |
| `docs/QUICKSTART.md:555-564` verbatim | **zero diagnostics** |

`enforceSpawn` -> `scanSpawn` fires only on an `ast.ExprIdent` whose symbol has a
borrow binding. A borrow written inline as `&v` in a call argument is a temporary
no binding holds, and its ordinary region ends at the call, after which `v = 5`
is legal by every rule the compiler had.

**Which decides the shape of step 2: narrow, never widen.** A blanket refusal
would have taken the third row with it, and that row is documented UX the ruling
protects by name. Section 9 of the model already says a local spawn MAY capture
borrows of its parent, so the capture stays legal and what changes is that the
unsound USE is refused. No program that compiled for a sound reason stops
compiling; nothing sema refuses today becomes accepted. The docs said "the
existing refusal stands", which was written believing a refusal covered this
generally; both normative files are corrected in this lane to say what the tree
actually does.

**The pin, and why it is the same lattice as moved places.** A child that
captured a borrow reads the place until it completes, so the borrow outlives its
own lexical region. `taskBorrowPins` maps (task, place) to the capture, and a
spawn opens one pin per borrowed place in its operand. A join REMOVES the pins of
that task on the current path only; the union at every branch join puts back any
the other path still held. That IS `ACTIVE + RELEASED -> ACTIVE`, with no new
merge machinery: `mergeMovedPlaces` was already a union for the mirror-image
reason, stated in its own comment -- a value moved on any path may not be used
after the join.

Because the two lattices are always snapshotted, restored and merged together,
they are bundled as `flowSnapshot` rather than threaded as two parallel
variables. That is what keeps the next join point from remembering one and
forgetting the other, and it is also what let the join points absorb the change
without growing. The first attempt threaded the two lattices separately and the
legacy-file gate refused it: `type_checker_walk.go` went `584 -> 598` and
`borrow_runtime_ops.go` `586 -> 597` effective LOC, both `LEGACY_GROWTH`. After
bundling, one call replaces two at every join and the same files measure
`584 -> 584` and `586 -> 582`. The gate asked for the better shape rather than
for fewer lines, which is the point of it.

Join points covered, all of them existing sites rather than new ones:
statement `if`/`else` including the closed-arm cases and the else-less form,
`while`, `for`, `for-in`, `compare` arms, `select` arms, and the ternary. The
three loops share `closeLoopFlow`, which states the asymmetry: a move across a
back edge is refused outright, while a join inside the body releases nothing
after the loop, because the zero-iteration path is a reachable predecessor.

**`TaskTracker.Awaited` is deliberately not reused.** It is a global per-task
boolean with no flow sensitivity, correct for the scope leak check, which only
asks whether a handle was disposed of somewhere, and exactly wrong for referent
safety, which asks about definite completion on every path. Handle move state and
referent pin state stay different facts.

**One implementation trap worth recording.** The first version read the captures
back from the borrow table after the operand had been typed, and found nothing:
`DropBorrow` deletes the expression's entry when a call-argument borrow ends, so
by then the table no longer remembers that `spawn worker(&v)` borrowed anything.
That deletion is the same fact as the defect -- the binding outlives the call and
the temporary does not. Captures are recorded at CREATION instead, while the
spawn operand is being typed.

**Evidence.** Three new golden fixtures and their controls:
`invalid/task_borrow_mutated_while_child_runs.sg` and
`invalid/task_borrow_join_on_one_branch_only.sg` each record one
`SEM3019 cannot mutate 'v' while it is shared-borrowed`;
`valid/task_borrow_join_on_every_branch.sg` records none, which is what keeps the
rule from meaning "a borrow-capturing spawn poisons the place forever". The two
task-borrow rows the corpus already had are unchanged:
`valid/task_borrow_awaited_in_scope.sg` still compiles, and
`invalid/task_borrows_local_escapes.sg` still reports its two `SEM3139` rows. The
regeneration modified NO existing recorded output -- the only edit outside the
new files is `testdata/golden.expectations.json`, `entry_count` 5362 -> 5377,
which is exactly three fixtures times five artifacts.

Rule 13, and it is targeted rather than a build break: making the else-less `if`
take the then-arm's flow instead of the union makes
`task_borrow_join_on_one_branch_only.sg` report **0 errors** against its recorded
1, while `task_borrow_mutated_while_child_runs.sg` stays at 1 and
`task_borrow_join_on_every_branch.sg` stays at 0. The mutant removes exactly the
may-be-live rule and nothing else.

`go test ./internal/sema` passes, repository `make lint` reports `0 issues`, and
the exact-base file-size gate passes 12 files with 0 violations. `make
golden-check` is run after this commit rather than before it: its preflight
refuses an uncommitted corpus, by design, because an uncommitted corpus has no
reviewed starting point to compare against.

### Carrier-affine borrow, step 3: naming the places that may not move

Step 3 of the vertical, in the same lane, on top of step 2. It adds an ANSWER and
no behaviour: `Result.StableActivationPlaces` names, per enclosing callable, the
bindings whose storage the activation must keep at a fixed address because a
carrier-affine child borrows them.

It is recorded at the SPAWN rather than at the join, because the capture set is
what decides which storage may not move, and it is recorded per callable because
the storage it constrains is that callable's activation -- an `async fn`'s frame,
or the synthetic root activation of an `@entrypoint`, which needs promoted places
on exactly the same terms. Keyed by binding rather than by `Place`, for the
reason `movedPlaces` already gives: only whole-binding places are reachable while
the partial-move gate is up.

**This field has a test and no reader, deliberately.** Its consumer is the
activation-storage lowering, which is step 4, and the ordering rule the ruling
sets is that a real borrow may not be lowered before its storage is stable. A
seam with no consumer is normally a smell; here it is the shape of the sequence,
and the test is what keeps it from rotting before step 4 arrives.

Evidence is five cases in `TestStableActivationPlacesNameOnlyBorrowedCaptures`,
two of which are negative controls rather than confirmations: a spawn that
borrows NOTHING constrains no storage, and a callable that never spawns is absent
from the map even when a sibling in the same file borrows a binding of the same
name. The other three pin the positive shape -- an inline `&v` argument names
`v`, two children borrowing one place name it once, and two borrowed places are
both named. The prelude is `onCrossingPrelude`, reused for the reason that file
states: a sema unit test must not depend on the real stdlib. Without it `Task`
does not resolve, the spawn types as `NoTypeID`, and the analysis correctly
records nothing -- which is how the first draft of this test failed, and is worth
recording because a snippet harness that silently cannot type a `spawn` would
have made any assertion about spawns vacuous.

`go test ./internal/sema` passes, `make lint` reports `0 issues`, and the
file-size gate passes 14 files with 0 violations.

### What two independent adversarial reviews found in the pin

The step-2 pin was reviewed twice, independently, before integration. One review
put the DESIGN to three external models without letting them see the code; the
other RAN a compiler built from the lane against about forty targeted programs
and all of the corpus. Both found real defects and both concluded the lane must
not be integrated as written. Every finding was re-measured here before being
accepted, and two of the reviewers' own witnesses did not survive that.

**The first sentence to go was mine.** Step 2's notes claimed the code base
"already merges moved places by union at exactly these points". That is FALSE FOR
LOOPS: `type_checker_walk.go` does not union at `while`/`for`, it REFUSES,
through `rejectLoopBackEdgeMoves`. Moved places can afford the weaker loop rule
because "moved" is STICKY -- gen with no kill. A pin has a kill, so the
mirror-image argument does not transfer, and copying the merge points without
copying the refusal is exactly what left the back edge open.

#### Seven defects, each closed against a measurement

| what was wrong | before | after |
| --- | --- | --- |
| the pin was asked at writes and moves and NOWHERE else, so acquiring a new borrow never saw it | `spawn reader(&v); spawn writer(&mut v)` -- one child reading and one writing one place -- **accepted** | `SEM3018`, the answer the binding form already gave |
| a borrow in the operand that the child never receives was pinned | `spawn worker(peek(&v))`, child takes an `int` by value, **refused** | accepted |
| the loop back edge was not modelled | a pin opened late in a body was invisible at the top of the next turn | refused at the back edge |
| `break`/`continue` carry no state, so a join after one released a pin on a path that never reached it | **accepted** | refused at the `break`/`continue` |
| a live pin at `return` let the frame die under a running child | **accepted** | refused, unless the return hands the task back |
| a join inside a deferred `async { }` body released the PARENT's pin | **accepted** | refused |
| awaiting a clone released nothing, so the answer depended on which handle was joined first | one order refused, the mirror order accepted | both accepted |

Two deserve their own sentence.

**The pin now guards three operations, not two.** Acquisition was the one that
mattered: a reader child and a writer child admitted on one place is the sharpest
case the feature can have, and it contradicted the ruling, which says the inline
form receives the same diagnostic the binding form receives. `handleBorrow` now
asks before `BeginBorrow`, with the borrow table's own rule applied to a borrow
whose lexical record has ended -- an exclusive pin refuses any new borrow, a
shared pin refuses only an exclusive one.

**The capture set is now the capture set.** The second row falsified the storage
model's own claim that promotion names "exactly those places whose address enters
a task capture and no others"; what the code computed was "borrows taken anywhere
in the operand". `spawnReachingBorrowExprs` names the positions a reference can
actually reach the child from -- the spawned call's arguments and its receiver --
and a nil set still means "everything", which is right for an `async { }` body,
because the body IS the child.

#### One false positive the ruling forbids, and how it closed

Fanning out borrow-capturing children into a container and draining them in a
loop was refused FOREVER: a handle popped from a container cannot be resolved
back to a task by name, so no join the checker can see would ever release the
pin. That is a sound program made uncompilable, which is precisely the widening
the ruling prohibits.

It closed without a new concept, because the compiler already proves the shape:
`taskContainerDrainLoop` recognises "while this container still holds something".
The container now records whose handles were pushed into it, and a PROVEN drain
answers for their pins -- draining is exactly the construct that makes completion
definite for every one of them.

#### The diagnostic was nondeterministic

`taskBorrowPinFor` ranged a Go map and kept the first hit. With two pins on one
place the winner decided the message text and the note's position, so one binary
printed two different outputs for one file -- measured at 37 of 40 runs against 3
of 40. The winner is now chosen: an exact place match beats an overlap, an
exclusive pin beats a shared one, the earliest span breaks the tie.

#### Two reviewer witnesses that did not survive re-measurement

Recorded so they are not retried. The cross-model review's first loop program
(`let mut t = spawn dummy(); while cond { v = 5; t.await(); t = spawn worker(&v); }`)
is refused by `SEM3107` for an unrelated reason -- reassignment defeats the leak
checker -- so it proves nothing about the pin. A witness has to keep the leak
checker satisfied, which is why `break` and `continue` are the way in. Index
aliasing and identifier shadowing were also flagged and are already safe:
`BorrowTable.internPath` writes the literal `"i:;"` for every index segment, so
all indices of one array intern to one place, and tasks are keyed by
`symbols.SymbolID` rather than by spelling.

#### Deliberately NOT fixed here, with reproducers

An `async fn` called WITHOUT `spawn` yields a live `Task<T>` and opens no pin.
The root is measured and is not this lane's: the task tracker does not see that
shape at all. `let t = worker(&v);` never awaited draws NO diagnostic, while
`spawn worker(&v)` never awaited draws `SEM3107`. Teaching the tracker to see
plain calls returning `Task<T>` changes what structured concurrency covers and
belongs in its own row.

An `async { }` block's IMPLICIT captures are never pinned, because the pin is
keyed to a syntactic `&` and a block that merely reads a parent local produces no
`BorrowID`. The model settles the rule -- section 9 says a child that CAPTURES,
not a child that writes an ampersand -- so this needs no owner decision, but it
needs a capture analysis for blocks that does not exist yet. That is the same
question step 3's promotion analysis asks, and it belongs there.

Also open: `StableActivationPlaces` attributes a block-local to the host function
rather than to the block's own activation, which a lowering must not trust; and a
parent READ under an exclusive pin is not refused, which the ordinary borrow rule
does not refuse either, so the pin mirrors a pre-existing gap rather than adding
one.

#### Evidence

Fifteen witnesses -- eight that must be refused, seven that must be accepted --
all behaving correctly. `go test ./internal/sema` passes and `make lint` reports
`0 issues`. The exact-base file-size gate passes 19 files with 0 violations,
which took two splits it asked for and that the code wanted anyway:
`task_container.go` gave up the drain-SHAPE recognition, a syntactic question
about one expression rather than a fact about a container, and
`type_checker_walk.go` gave up the if-statement's flow join, which is a rule
rather than a dispatch.

The corpus is the load-bearing number: `surge diag` over every `.sg` file under
`testdata/` and `examples/`, comparing the step-2 compiler against this one,
reports ZERO differing files. Every one of these changes is visible only to the
programs written to exercise it.

### The promotion analysis was naming the wrong frame

The last of the review's findings on step 3, and it had to close BEFORE step 4
reads the field, because a lowering that trusted a wrong answer here would do
the one thing the storage model calls forbidden.

`StableActivationPlaces` was keyed by the enclosing CALLABLE. An `async { }` or
`blocking { }` block is a separate activation with its own frame, so a local of
the BLOCK, borrowed by a child of the block, was filed under the host function.
Nothing complains at that point: both are real activations and both name a real
binding. The damage lands later — a lowering promotes a field into the HOST's
frame and leaves the block's actual local a per-poll `alloca`, which is exactly
the state section 7 forbids, arrived at silently.

The key is now an ACTIVATION: `ActivationKey{Fn, Block}`, with `Block` the
`async`/`blocking` body being walked and `NoExprID` for the callable's own
activation. Blocks nest, so the innermost wins. The stack is pushed and popped
around the same body walk that already snapshots and restores pin state across
that boundary, which is the right place: a block being a different activation is
one fact, and it decides both what its joins may release and what its locals
belong to.

The storage model already anticipated this. It names "stable task activation
storage" rather than the `__AsyncState$` symbol, and says a rule written against
one frame "would be copied, divergently, for the root activation within two
steps". This is the same divergence, one step earlier than predicted and inside
the analysis rather than the lowering.

Rule 13 is targeted: reducing `currentActivation` to the callable alone makes the
new case fail with `"host/block" constrains nothing; got map[host:[inner]]` --
the defect stated in the assertion's own words -- while the other five cases
stay green.

**The key's shape was got wrong twice, both times by writing what reads well in
sema rather than what the reader can hold.** Worth recording as a pair, because
the second mistake survived the fix for the first.

First draft: the block half was an `ast.ExprID`. `hir.Expr` carries kind, type,
span and data and NO AST id, so the id dies at HIR and the consumer can never
reconstruct it. A block lowers to a synthetic `__async_block$N`
(`internal/mir/lower_expr_misc.go:190`) built by
`lowerSyntheticFunc(..., e.Span, ...)`, and `mir.Func` keeps that span, so the
span is the block identity that survives.

Second draft, still wrong: the key held the HOST callable's symbol beside the
block's span. But `lowerSyntheticFunc` builds the block's function with
`Sym: symbols.NoSymbolID` — it carries no symbol at all — so a reader standing in
that function has the span and nothing else, and could not rebuild a key that
demanded a host symbol too. EXACTLY ONE half is now set: a callable's activation
is named by its symbol, a block's by its span alone. A span carries its FileID,
so it is unique across the program.

Both drafts would have tested green. What caught them was asking where the answer
has to be RESOLVED before fixing the shape of the answer — the storage model's
ordering rule read from the consumer's end. The test now proves the attribution
the honest way: the block case asserts the host is ABSENT from the map.

`go test ./internal/sema` passes, `internal/gatecheck` passes, `make lint`
reports `0 issues`, and the exact-base file-size gate passes 5 files with 0
violations.

### Step 4's foundation: a place walk that cannot forget a kind

Step 4 promotes a borrowed local into a fixed-offset field of the activation's
frame, and the way it does that is by redirecting every use of the local to that
field. So the pass needs to reach ALL of them, and the cost of missing one is
the worst kind: the missed use keeps addressing the old slot while every other
use addresses the field, the parent and the child it lent the place to disagree
about the storage, and nothing crashes and nothing is reported. A silent wrong
answer is exactly what the resident exists to prevent, so a walker that is
"probably complete" is not usable here.

Nothing in the tree could be reused. `layout_roots_walk.go` looks like the right
thing and is not: it collects TYPES, its `walkOperand` never touches
`operand.Place`, and its `InstrAssign` case walks only `Src` and not the
destination. The two walks answer different questions and are kept apart rather
than merged into one that answers neither cleanly.

`forEachPlace` is therefore new, and it is exhaustive by construction and by
test. Every kind switch ends in a `default` that names the kind it did not know,
and four tests walk EVERY kind by value against the enum sentinels the tree
already maintains for this purpose -- `instrKindCount`, `rvalueKindCount`,
`termKindCount`, `operandKindCount`, the same ones `TestKindCountSentinelsStayLast`
pins. A kind added later fails here before it can be forgotten in the walker.

Two more tests ask the questions coverage does not. One builds a function whose
places sit in the positions most easily dropped -- an assignment's destination, a
call's destination, a terminator's operand -- and requires every one back, because
covering a kind is not the same as reaching the places inside it. The other
rewrites through the visitor and reads the result, because a walk that handed out
copies would satisfy everything else and make every caller a silent no-op.

Rule 13: deleting the `InstrChanRecv` case makes
`TestPlaceWalkCoversEveryInstrKind` fail with
`InstrKind 11 (ChanRecv) is not covered by the place walk`, naming the exact kind
rather than reporting a count. That is the property being bought -- a forgotten
kind is loud and self-identifying.

This commit adds no behaviour: the walker is a helper with tests and no
production caller yet. It lands separately so the promotion that will use it is
built on something already proven, rather than the proof arriving with the change
that needs it. `go test ./internal/mir` passes, `make lint` reports `0 issues`,
and the exact-base file-size gate passes 2 files with 0 violations.

### Step 4.4: the borrowed place stops being a per-poll slot

This is the change the whole storage exception rests on. A local of an async body
is a per-poll slot: a suspension packs the live ones into the frame's payload
union and the next poll unpacks them into FRESH slots, so a child holding a
pointer to one is left addressing storage the parent no longer uses -- on the
same carrier, with no crossing and no transport involved. Epic 23 section 7
requires instead that a place a live carrier-affine child borrows have ONE stable
storage identity from publication to completion. A resident is that identity: a
fixed-offset field of the activation's own frame.

Three facts had to be read out of the tree before any of this could be designed,
and each one moved the design.

**The frame does not hold the captures.** `buildAsyncConstructorState` builds a
THREE-field struct -- lifecycle word, resume point, payload -- and the captured
locals go into the payload union's START VARIANT, not into the frame. The comment
there says that variant "keeps the captured locals for the task's whole life",
and across a suspension that is not true: every await site replaces the payload
with a different variant. That is the dangling pointer, in the tree's own words.
It also settles what promoting a PARAMETER means: not "add a field" but "stop
unpacking it into a fresh slot", with the constructor delivering the same value to
the resident field that it used to hand to the start variant.

**A partial struct literal is a supported shape, not a hole.** MIR has no generic
zero of an arbitrary type -- `ConstKind` is Int/Uint/Float/Bool/String/Nothing/Fn
-- so a resident declared in the BODY rather than taken as a parameter looked like
it had nothing to initialize it with in the constructor's literal. The apparent
options were both bad: narrow promotion to types with a representable zero, which
makes sema and lowering silently disagree about which places are promoted, or
invent an undef. Neither was needed. `vm.buildComposite` zeroes an extent
DELIBERATELY, and says why: "a producer that fills only some members would
otherwise leave the rest reading as the corpse of something that has already been
released." A literal naming only some fields is the supported way to say
uninitialized, and `validate.go` agrees -- it checks the operands of the fields
present and requires no completeness. So a body-local resident gets no entry, its
bytes are zero, its first write is the body's own assignment redirected onto the
field, and it is never dropped generically: section 7 already requires residents
to be "dropped through the ordinary obligation of the place they already are, not
through a second lifecycle", so a frame discarded before that assignment drops
nothing.

**Promotion belongs only where storage can move.** A plain `fn` cannot suspend:
its frame is stable for the whole call, and the structured scope joins the child
before that frame dies, so a borrow out of one is already sound. Sema names those
places anyway -- it answers which storage a child constrains, not which storage
moves -- and the lowering, which only ever runs over async functions, never claims
them.

A fourth fact surfaced only because a test demanded it. Sema files a constrained
place under an ACTIVATION, and the obvious implementation asks that map for the
activation being lowered. It returns nothing for every `async fn` in the language,
because **monomorphization rewrites a function's symbol**: `resident_parent`
reaches MIR carrying instance id `0x90000001` where sema recorded `1450`. Blocks
kept working, since they carry no symbol and are keyed by span -- so the failure
was invisible from the block case alone, and the package stayed green. The binding
symbols themselves survive untouched: the local holding `v` still carries the
exact id sema named. So the lookup is by BINDING, which is not a workaround but
the better question -- a binding symbol is one declaration, so it can only appear
as a local of the activation that declared it, and attribution falls out without
being asked for. A generic async function instantiated twice yields two functions
that both carry that binding, and both get the resident, which is what correctness
requires and what a per-instance activation key would have got wrong in the other
direction.

The tests are built so that no one of them alone reads as "promotion works". One
requires a borrowed local of an `async fn` to become a resident and one requires
the same of an `async {}` block's own local; those two fail for different reasons,
and it was the block passing while the function silently did nothing that exposed
the mono rewrite. A third requires that an async function borrowing NOTHING into a
child promotes nothing, because a promotion firing for every local would satisfy
the first two while quietly enlarging every async frame and discarding the
place-oriented rule. A fourth set tests the exclusion from the payload union
directly, since the end-to-end tests structurally cannot see it: a resident that
was promoted AND still packed produces exactly the same field, differing only in
that the frame then carries a stale copy of a place the child is mutating -- the
two-storage problem arrived at from the other side. The rewrite is also required
to keep an existing projection, so a promoted composite's `v.x` does not silently
become the whole of `v`: a read of the wrong size from the right address, which
nothing downstream is positioned to catch.

**The partial literal had one more consequence, on one backend only.** Reading
the runtime settled where the "unnamed means uninitialized" convention actually
lives, and it is not only the VM: `rt_frame_alloc` memsets a frame block, saying
why in the same terms. But `rt_frame_release` runs the type's GENERATED
member-wise drop over any frame whose lifecycle word reads PACKED, and the
constructor writes that word from the frame's first instant. So an abandoned
frame drops every member -- residents included. The VM was safe, because
`buildComposite` zeroes. LLVM was not: `emitStructLit` builds into a bare alloca,
so a frame discarded before its body ran would have dropped a field nobody wrote.
That is a backend-divergent use of uninitialized memory, which is the worst place
for a difference to live, so `emitStructLit` now clears its storage when the
literal names fewer fields than the type has -- and only then, since a complete
literal overwrites every byte that matters and zeroing first would be dead stores
in the overwhelmingly common case.

Rule 13 earned its keep twice here. The first version of that test asserted only
that a memset appeared in the constructor's body, and it PASSED with the clearing
removed: the body already contains one, because a union materialisation clears
its own storage. The assertion is now on the memset's exact byte size -- the frame
struct's own -- and both mutants fail as they should: removing the clearing fails
the partial case, and clearing unconditionally fails the complete one.

One process note worth keeping. The first full-suite run reported eight failures
in `internal/driver` and `internal/lsp`, and a plausible story was ready-made:
this branch added pin refusals, and tests asserting a clean compile are exactly
what pin refusals break. Both halves of that story were wrong. Stashing the
working changes reproduced the identical eight, and the failure TEXT -- `unknown
type Task` -- named a missing prelude rather than a refusal. `SURGE_STDLIB` points
at the repository ROOT, not at `stdlib/`; the Makefile passes `$(CURDIR)`, and the
existence of a `stdlib/` directory is what makes the mistake easy. A wrong stand,
not a regression.

### Scope creation provenance, ported onto the integrated base

Wave D step 5. The work existed in a dirty lane on base `4e4ec572` and is
carried here onto `5b44cb0c`. Nothing about the feature changed in the move; what
changed is what it is now measured against.

**The transfer was by hunks, not by taking the lane whole.** The base had moved
twelve commits, but the real conflict surface was seven files, found by
intersecting the two change sets -- `comm -12` of the base drift against the
lane's own dirty list -- of which three were the journal, the ledger and the
generated counter. `git apply --3way` then landed everything with ONE conflict:
the lifecycle aggregate's test roster. It resolved as a UNION -- the lane's
roster is right to retire the `ScopeMembershipClaim*` rows, because it REPLACED
those tests with `ScopeCreationProvenance*`, and the four `Seq0*` rows that
landed meanwhile are added back.

*A trap worth recording:* piping `git apply --3way` through `head` makes SIGPIPE
kill the apply mid-way and roll it back, after it has already printed a per-file
success for most of the patch. The first attempt looked like it had applied and
had not.

**The lane arrived with two ledger rows already marked `Closed`, and they were
re-derived rather than carried.** Evidence taken on a superseded tree is not
evidence for this one. On this base: the deterministic stand passed again at
threads 2/4/8 and its `RV2_SCOPE_PROVENANCE_NEGATIVE_CONTROL` mutant failed as
recorded; `StaticCreationScopeBeforeRunnablePublication` passed; the three
SEM3209 rows -- the diagnostic contract, the legal inner-creation/outer-binding
control, and the branch-merge row -- passed. `make golden-update` regenerated the
two carried fixture sets BYTE-IDENTICALLY, which is the part that proves the
behaviour is the same here and not merely that files were copied. The invalid
fixture records five `SEM3209` rows, one per spelling the row claims to cover.
Both ledger rows now carry the re-derivation date and base.

**The sync-point gate caught a real defect the lane had.** It maps each sync
point to the file whose window it guards. The lane moved
`RT_SYNC_POINT(SP_SCOPE_MEMBERSHIP_DECIDED_BEFORE_PUBLISH)` from
`rt_async_scope.c` into `rt_async_task.c` -- correctly, since the whole feature
is that membership is decided at CREATION, before slot and ready publication --
and never touched `check_sync_points.sh`. At base the gate passes; on the ported
tree it failed by name. The window moved with the rule it guards. Nothing else
notices a moved sync point: the code compiles and the tests pass, and only this
static gate reads the pairing, so it is worth running on any lane that touches
these calls -- it needs no build.

**Gates.** `check_sync_points.sh`, `make c-check` (format and strict compile),
`go test ./internal/panicgate`, `make lint` (`0 issues`), and the exact-base
file-size gate at 31 files with 0 violations. `internal/mir` and
`internal/asyncrt` pass.

The full `internal/vm` sweep reports three failures --
`TestVMDropOrderDeterministic`, `TestVMHeapOOBPanics`, `TestVMTermReadEventQueue`
-- and they are NOT this port's. The control says so: the same three fail
identically on `5b44cb0c` itself, with and without `SURGE_STDLIB` set
(`cannot resolve 'Key' / 'Resize' / 'Eof'`, `missing argv argument`). They are a
property of this workstation, and a failure without a control is not a
measurement. The first attempt at that sweep is also void for a different reason
and is not quoted anywhere: it was run without `--timeout`, hit `go test`'s own
ten-minute default, and reported `FAIL ... 600.011s`, which is a timeout rather
than a result.

### Closeout, step D0: the trunk is put in order and three things that were held by nothing are held

Written 2026-09-02 on `codex/runtime-v2-closeout`, executing
`docs/EPICS_CLOSEOUT_PLAN.md` Этап 1 as one continuous run.

The integration branch stood at `40f1c6b3`, two commits behind the tip that
carried the resident promotion, and the SEM3209 port sat at `8882ff00` on NO ref
at all -- `git for-each-ref --contains` was empty and one reflog entry was the
only thing between it and garbage collection. `keep/sem3209` now names it, and
the branch fast-forwards through `8b12beb3` to it in one step, because its parent
already was the tip. The DEBT-277 retry-budget work existed only as thirty
staged, uncommitted files in its lane worktree -- the lane's tip `1480151c` is
an ancestor of the trunk, so the branch itself carried nothing, and a single
checkout there would have lost the work. It is snapshotted to a patch and
committed as `wip/debt277-index-snapshot` (`4471d2e7`), explicitly not a landing
candidate: twenty-five commits stale, rewriting `rt_waiter*` that the seq-0
primitive also rewrote, with a DEBT row it marks Closed prematurely. The
DEBT-307 lane (`163b9cda`, fifty-six dirty files, two P0s in review) is recorded
as discarded, its diff kept beside the session's baselines.

A Sentrux structural baseline is saved at this tree for all four policy scopes
(`2500ce89`), because the closeout's final gate compares against a saved
baseline and a final without one is not a comparison. Every scope passes its
rules; no enforced scope moved down against the previously committed baseline
(root 6178 -> 6165 is the advisory scope; internal 6450 -> 6459, runtime
5195 -> 5282, runtime/native 5159 -> 5419).

### Closeout, step D0c: two files are split before the work that would have to grow them

`4bca34c9`. No behaviour changes. The file-size gate measures effective lines
against the epic base and refuses growth in a file already over the limit, so
the question for the scheduler-affinity, retry-budget and claim-registry work
that follows was not whether `rt_async_channel.c` and `rt_async_internal.h`
were large but whether they had ROOM: 42 effective lines in the first against a
retry-budget port needing about 34 of them, and 18 in the second -- already over
500 at the base -- against three changes that each add a field.

The send loop moves verbatim to `rt_async_channel_send.c`; the one helper both
loops perform, staging a value into a park slot the channel owns, keeps its body
and gains external linkage as `rt_channel_stage_locked`. A static-function map
taken before the cut established that nothing else crossed the seam.
`rt_async_channel.c` goes 458 -> 289 effective (base 299) with 170 in the new
file. The task state words and their inline helpers move from
`rt_async_internal.h` to `rt_task_state.h`, a fragment included once, in place,
after `rt_task` and `rt_executor` are complete; 658 -> 542 against a base of 676.

Three gates key on source location, and each was found by the gate going red
rather than by memory, which is the point of running them: the panic-surface
allowlist keys sites as `file::function#ordinal`, so five `rt_channel_send_inner`
rows moved to the new file and the renamed helper moved to its sorted position
(the gate reported first the uncovered sites, then the ordering); the lock-split
static test reads the send loop's body out of a named file and now reads the
file the loop is in; the carrier census was re-run. Proof of no change: the full
C gate (format plus strict warnings), the file-size gate at 0 violations over
1155 files, panicgate, carriergate, the tagged lock-split rows, and the channel
behaviour rows on both lanes -- 429 tests selected untagged and 623 tagged, both
packages `ok`, plus 17 asyncrt rows, all counted rather than assumed.

A fourth thing surfaced from the runner rather than the tree. An aggregate count
on the dedicated machine ran once and then refused four times in under a second
each: the heavy-run guard reported a dirty tree, and the dirt was
`internal/buildpipeline/rv2-crossing-xmod-1577022690/`, a test project that
`TestCrossingBackendGuardsCoverImportedModules` builds in its own package
directory and removes in a Cleanup -- gone whenever the test gets to finish,
left whenever the process is killed first. The project cannot move (resolution
of the imported module is relative to the package directory; `t.TempDir` and the
build cache both fail on the lookup with PRJ5002, both tried), so the name is
git-ignored instead (`7f6c0243`): a leftover is invisible to `git status`, which
is what the guard consults.

### Closeout: the SEM3209 port breaks the HTTP owner gate, and its baseline could not have seen it

Found by the DEBT-312 rate the plan schedules for the runner. The gate went red
on the FIRST iteration at shards-2 and shards-8 alike, which is not the row's
shape (one client of eight, only at eight shards, intermittent); it stayed red
with the box exclusive, so contention was not the cause. Three runs on the
runner closed the attribution: `07df9885` green in 8 s, `8b12beb3` (the resident
promotion) green in 14 s, `8882ff00` (the port) red in 34 s.

The mechanism reads directly off `publish_created_task`. The port re-places
EVERY task created inside a scope onto the scope's owner shard, after
`rt_task_inherit_placement` has already copied the parent's shard AND class.
An HTTP handler is a CONNECTION-class task placed on the shard that owns its
fd; moved to the scope's shard it keeps its class, and every touch of the
connection from the wrong shard is refused -- the red run's trace reads
`non_owner_conn_denied=15` at shards-2 and `=3` at shards-8, and the client's
read times out. At shards-1 the two shards coincide, which is why that leaf
passed. This is the accept-ownership ruling of 2026-08-31 (variant A: the fd
owner decides, no hot path re-places a connection task) contradicted by a hot
path.

Why the port's own baseline was blind: `go test ./...` does not run
`runtime-v2-http-owner-check`, which lives behind `-tags runtime_v2_pending`
and `SURGE_BACKEND=llvm`. "The port carries no red of its own" was true of the
untagged suite only. The rule recorded earlier in this file -- a tagged test
needs a tagged gate -- has a converse: a baseline that omits the tagged gates is
blind to exactly the class those gates exist for.

The fix keeps a connection child where the fd owner put it and re-places only
generic children; scope membership is then published under the SCOPE's lane
when that differs from the task's, one lane at a time and released before the
next, which is the protocol the function already states for the parent relation.
`rt_scope_publish_creation_locked` panics outside the scope's lane, which is
why the port had collapsed the two lanes into one to begin with.

Two things about the stand, kept because they cost a count each. The rate and
an aggregate count were started concurrently on the two halves of the machine
because the plan's schedule allowed functional rows to share it; both contain
this gate, both went red, both counts were discarded as contaminated. Network
gates do not share a box. And `pkill -f <pattern>` issued inside an ssh command
whose own argument string contains that pattern kills the ssh session itself,
which it did twice before the orphaned make was killed by pid.

The witness, Rule 13 read literally: `runtime-v2-http-owner-check` on
`c9c083e2` is `ok` in 14 s on the same runner, exclusive, where `8882ff00` and
`7f6c0243` were red in 34 s. Six tagged rows that were red on the fixed tip
were then run against `8b12beb3`, before the port and the split: four --
the DEBT-261 and DEBT-263 proof and negative-control pairs -- were red there
too and are the port's remaining tail (the stands adopt a driver's child
through `rt_scope_register_child`, which the port made a validator); two --
`LifecycleStaticCompletionResultVisibilityOrder` and
`LifecycleStaticAwaitCompatCountedSeparately` -- were green there, so they were
the split's: both read `rt_async_internal.h` by name for helpers now in
`rt_task_state.h`, hidden behind the `runtime_v2_pending` tag where the untagged
proof could not see them. Repointed in `0db26a48`.

### Closeout, lane B: the pre-existing reds, each with its cause named

Eight rows were red on the tree the closeout started from, verified on
`5b44cb0c` before any of this branch's work. Six are closed; the cause of each
is a sentence, not a story.

Three were test programs the language moved out from under (`64cc028c`), each
refused by a rule the tree states and the corpus follows: `let s: string =
argv[0]` binds an indexed place, and `__index(self: &Array<T>, index: int) ->
&T` makes that SEM3015 -- the corpus reads an element as `clone(a[j])`;
`[s, s + "y"]` moves `s` and then reads it, SEM3130, the rule the
`drop_then_use` fixtures pin; and a compare arm naming an imported module's tag
bare after `import stdlib/term as term` is SEM3005, because tag lookup in a
pattern walks lexical scopes and never an import alias -- `term.Key(_)`
compiles clean, and that arm had been written on 2026-01-15 with no corpus
fixture to move with it.

One was a pin that fell (`e8d1ccff`). The abandoned-frame census pinned the
blocks a scope retains per cancelled round at two and read zero; its own
comment says the last such fall was a defect and that a fall is explained by
naming what removed the cost. A bisect over 245 commits on the runner stopped
at `6376af8a`, "a cancelled task gives back the scope its own poll opened":
the single-worker runner's copied outcome switch skipped the scope teardown,
one 64-byte `rt_scope` per cancelled round, measured there and pinned by its
own Rule-13 row. That block was what the two were made of. The pin is zero and
names the commit.

Two were the VM lane disagreeing with the native lane on the parity row, and
they were two different defects. `random_pcg32` (`60902766`): a value of an
arbitrary-precision type is stored inline while it fits a word and as a heap
bignum once it does not, so `wide_value % U64_MOD`, both declared `uint`, is a
VKInt beside a VKBigUint the moment U64_MOD is 2^64 -- and every arithmetic
dispatch accepted only same-kind pairs. The big readers now widen an inline
operand of the matching signedness, and every dispatch routes a mixed pair to
its big arm; with the widening forced off, the row dies again with "VM1003:
expected numeric, got biguint and int", four times. `net_echo` (`05a57cc3`):
the instruction trace showed the `uint` parameter's argv text being handed to
`core/string.sg:341`, the STRING identity `from_str`. Sema classifies the
numeric parser as a builtin, MIR emits the call with no symbol, and the VM's
name resolution -- already on the typed path, because `from_str` is on the
list of names that must not be shadowed -- tried the wanted result type, found
nothing, and RELAXED to any result type, matching the prelude's user `from_str`
on its argument alone. The relaxed pass is now taken only when the call has no
result type; with it restored, a two-parameter probe dies at the same line.

Two remain and are timing rows rather than logic: `TestMTCorrectnessWakeups`
(20 s deadline, red on the trunk on both machines, green on the port -- one
observation on a concurrency test, not an attribution) and
`goldencheck/HUP` (red on the dedicated runner at both SHAs, green 3 of 3 in
0.138 s on a quiet workstation). Both get a measured rate on the runner, not a
verdict from one run. `TaskContentionCohort` is red only on WSL2 and green on
the runner at the same SHA; it is recorded as machine-dependent.

### D4.6, decided before the code

Carrier-addressed publication is a scheduler structure, not a predicate, and
the shape is fixed here so the implementation does not fix it silently. A
worker already has a stable identity (`rt_worker_ctx{shard_id, worker_id}`)
and its own deque (`local_queues[worker_id]`); a task has a shard and no
worker; publication pushes to the PUSHING worker's deque, never a named one;
wake is one counter and one condvar per shard; steal refusal is per shard and
never fires, because steals never cross shards and more than one carrier exists
only at `SURGE_SHARDS=1`; `spawn` lowers to `rt_task_wake` on a handle the
constructor already made, and `SpawnInstr` carries no flags.

The decisions: the compiler marks only `RequiresParentCarrier` on the spawn,
from the same `StableActivationPlaces` fact the resident promotion keys on; the
runtime resolves the carrier at the spawn through a new `rt_task_pin_carrier`
emitted before `rt_task_wake`, reading the current worker, which IS the
parent's carrier because spawn is a synchronous action of the running parent
-- `__task_create` and the ABI manifest do not change; publication of a pinned
task goes to `local_queues[eligible_worker]` of the eligible shard, never the
inject queue; the wake is addressed with a per-worker token and the shard's
existing condvar -- a broadcast the named worker consumes and the others sleep
through on the re-check that already exists -- rather than a condvar per
worker, which would put a second wait object under the shard lock; steal and
handoff refuse by worker in `pop_task_from_deque`, before the shard check;
shutdown cancels a pinned task that was never polled, the model allowing
either; four sched-trace counters make it observable; and the VM lane is out
of scope, having no carriers to be affine to. The stand and its mutant -- the
shard-wide token in place of the addressed one, the P0 the reviews named --
are written before the implementation, and it lands in three slices with the
aggregate green after each.

### Closeout, the same day continued: D5b, two leftovers a count found, D4.6 landed with one correction, D4.5 by deletion

**D5b (`71bd0674`).** Variant B to its end: `rt_scope_register_child` refuses
a `creation_scope_key` that is not the scope's, as a fatal panic; MIR no
longer emits the call after `InstrSpawn`, membership having been written at
creation. Of the eleven direct stand sites, seven create their children inside
the entered scope and pass the validator unchanged; two adopted a child the
DRIVER had spawned (the DEBT-261 and DEBT-263 stands, spawned outside because
a push from inside a held poll lands on that worker's local deque and signals
nobody) and now create it from the owner's poll through
`spawn_pinned_in_scope`, which seals provenance, publishes membership on the
scope's lane and FORCES the push onto the inject queue; the native scope
stand registers a task the scope never created and must die with the
message, and with HEAD's silent validator restored it reads "was accepted as
its child", code 1 -- the Rule 13 mutant.

**Two leftovers the aggregate found, neither this day's.** The W8 count on
`64cc028c` read 0/5, and both reds pre-dated the count: `lifecycle-check` on
exactly the four DEBT-261/263 rows above, and `ownership-check` on "corpus
count = 1057, want 1055" -- the SEM3209 port (`8882ff00`) added two golden
fixtures and never raised the inventory tripwire, so the gate had been red on
every aggregate since the port (`017686af`). A third, on the widening commit
`60902766`: `eval_ops_arith.go` crossed 500 effective lines (493 -> 513) and
the size gate was not run on it; div and mod moved out (`b583ff91`).

**`goldencheck/HUP` on the dedicated machine is the launcher, not the tree.**
Red on both SHAs there, 45.06 s -- the whole guard -- with INT and TERM green
in the same test, green on the workstation. The counts on the runner were
started with `nohup`, which sets SIGHUP to SIG_IGN; the disposition survives
exec down to `golden_update.sh`, and bash cannot trap a signal ignored on
entry, so its `trap 'exit 129' HUP` is a no-op. Counts now start under
`setsid`; the differential (one run under each, same worktree) is queued
behind the count on `1ba55ecd`. The baseline on `017686af` read exactly one
red, this one, of the seven the day began with.

**D4.6 (`1ba55ecd`), with the correction the tree forced.** The note above
put the pin at the spawn's `rt_task_wake`. The tree publishes earlier:
`__task_create` pushes the task READY at creation (`publish_created_task`),
and the wake at the spawn finds it enqueued and does nothing -- a pin there is
a pin after the first publication. So the mark lives on the CONSTRUCTOR: an
activation whose start payload holds a reference is built through
`__task_create_affine` (`mir.asyncTaskConstructorName`), the same constructor
with the pin taken from the creating worker before the task is published;
`__task_create`, its signature and the ABI manifest are untouched, and the VM
treats the sibling as the plain one. Everything else is as decided:
`carrier_valid`/`carrier_worker_id` on the task; the addressed route and the
per-worker credit in `ready_push_task_locked`, the leaf every publication
funnels through; refusal by worker in `pop_task_from_deque` before the shard
check; four records on the runtime's trace cell. Two things the stand taught
that the note did not know. A yield or a net wake asks for the inject queue
so that a task re-entering every turn cannot starve the ones behind it, and a
pinned task may not go there -- so those go to the HEAD of the carrier's
deque, or three always-ready spinners pinned beside a parked child starve it
forever (LIFO). And a cancel is not a completion: `cancel_task` seals the
gate and wakes the target, and the target unwinds on its next poll, which
only its carrier may give -- so an exiting carrier gives it, popping one
cancelled pinned entry per turn until its deque holds none; a plain cancel
put the task straight back into the deque it was leaving. The
parked-with-work assert now names the parking worker, since a pinned entry
in another carrier's deque is not this sleeper's work (it fired at two
workers otherwise). Proof: the publish stand at 2/4/8 workers, a driver that
is no worker waking a parked child 24 times, every wake reaching the
carrier; the shutdown stand reading DONE+CANCELLED; three mutants red at
eight workers (the pusher's route, caught by the parked-with-work invariant;
the shard-wide credit, stuck on cycle one; the deque left at exit); and a
compiled `spawn plus_one(&v)` from an `@entrypoint` answering 42 at 1 and 8
workers, pinned at 8 -- at one thread the control lane's single runner is no
worker and records no pin, which the test says.

**A flaky row named by its arithmetic.** W8 on `017686af` read 4/5; the one
red was `TestRuntimeV2SelectReleasesACompositePayloadExactlyOnce`, "0.25
allocations per take", 62 outstanding blocks at four takes and 63 at eight.
The row compared two separate valgrind processes for exact equality; a leak
reads a whole block per take, four over four takes. The comparison is a
slope now, half a block per take the line (`83ae586d`).

**D4.5 and D4.7, by deletion.** With the place promoted and the child pinned,
`emitAsyncRefParamBox`, `shouldBoxAsyncRefParams` and the `async-ref-box`
site are gone; a shared `&T` parameter is stored as the pointer it is. Before:
`TestRuntimeV2MutexLockUnlockValgrindBounded` pinned 192 bytes in 24 blocks.
After: `TestRuntimeV2MutexLockUnlockValgrindZero`, strict zero. The IR row
that demanded the box now forbids it and demands the affine constructor
(`TestEmitAsyncSharedRefParamKeepsCallerAlias`); the alloc-guard census row
reaches `rt_frame_alloc` alone. RV2-DEBT-303 closes on the terms its routing
set: nothing is copied, so nothing is held uncounted.

### D6, RV2-DEBT-277: the retry budget, redesigned from the patch (2026-09-03)

**What the lane had, and what it lacked.** The `codex/rv2-channel-retry`
snapshot (`4471d2e7`, thirty staged files, 25 commits behind the trunk) had
a single counter per logical operation, a budget of eight with the mutant
nine, three refusal causes, the counters under `TRACE_EXEC`, and an
exhaustion that registered the ONE arm whose refusal was the eighth. A select
is refused on whichever arm's claim is busy at each poll, so that registration
missed every earlier arm: a release on the first of two busy channels woke
nobody, and the select slept until the second was released too. The port
re-derives every integration hunk against the trunk's `rt_waiter*` (the seq-0
primitive rewrote them after the lane forked) and adds what was missing.

**The design, in one paragraph.** The cores answer a fourth status -- 3,
REFUSED, with a cause: the ring's push, the ring's pop, or a park slot's take
(`rt_channel_sync.c`, `channel_stage_into_ring_locked` gained `out_refusal`).
Every refusal site counts it against the task's current logical operation
(`rt_channel_retry.c`, `rt_channel_retry_state` on the task: operation, key
identity, count, and a prefix ring of up to seven distinct
`{channel, direction, cause}`); seven times the operation republishes with no
key, the eighth parks on the channel's own retry key -- two new waker kinds,
`WAKER_CHAN_SEND_RETRY` / `WAKER_CHAN_RECV_RETRY`, same channel and owner shard
as the ordinary two, so the same pin and the same close settle them. Every
release of a claim wakes that key (`rt_channel_claim_released_locked`: after
commit push and pop, commit take and deliver, abandon push, and the
value-bearing half of `channel_end_park_locked`, whose reserved slot was BUSY
to a taker while the drop ran): select subscriptions (seq 0) are drained
whole, then the oldest still-valid direct waiter is woken, and the ordinary
sender/receiver FIFOs are never touched. `rt_channel_close` drains four keys.
A select counts each refused arm, remembers it in the prefix, and at
exhaustion subscribes to EVERY remembered arm of this poll's arm set,
register-then-verify under each owner lane
(`rt_select_channel_retry.h`); a `default` arm wins only after a clean scan,
and a clean scan with no winner resets the count. Progress resets the state
at every completing return, including the two recovery returns where a
candidate died and the staged value went to the buffer instead.

**Before / after, literally.** Before: `drive_direct_refusals` on a held ring
claim -- eight `rt_channel_send` calls each returned 0 with `pending_key =
none`; nothing counted, nothing parked; `channel_retry_budget_exhaustions`
had no writer. After: `OK_DIRECT: refusals=8 republications=7 exhaustions=1
max_retries=8 woke=1 completed=1`, `channel_claim_refusals_ring_push=8`,
`channel_claim_releases=2`; the recv twin reads `ring_pop=8`, `releases=4`;
the select twin `ring_push=8`, `releases=2`; close reads
`retry_park_terminated=1 resume=closed registrations=1->0 pins=1->0`. Stand:
`internal/vm/testdata/channel_claim_retry*.c` (one process, no workers, a
hand-made current task, the rival claim HELD by the driver), five tests
`TestRuntimeV2ChannelClaimRetry(BudgetAndWake|IdentityAndReset|NegativeControls|RegisterVerify|RegisterVerifyNegativeControl)`
on the waiter gate; local, 77 s, all green.

**Twelve mutants, each red on its named line.** Budget nine ("operation did
not park on the eighth refusal"); no wake on release ("claim release did not
wake the retry park"); close draining two keys ("close did not terminate the
retry park"); select identity by the arm table's stack address ("select
address rotation reset the retry budget"); only the eighth arm registered
(`RV2_DEBT_277_PREFIX_NEGATIVE_CONTROL`, the new one: "release on an earlier
refusing arm did not wake the select" -- two send arms, both held, four polls
of two refusals each, the FIRST arm released); a `default` arm gated on a
clean scan (`RV2_DEBT_277_SELECT_DEFAULT_NEGATIVE_CONTROL`: "select with a
default republished on a refused claim"); no recovery reset, same and
foreign owner ("new send inherited completed retry budget"); no wake from the
value-bearing park release ("finish-release did not preserve retry wake");
one mixed FIFO on the retry key ×2 ("claim release stopped at select
sibling", "claim release left select retry subscription behind"); register
without the owner-lane verify ("select parked after release crossed empty
retry key").

**The independent review, round one: REJECT, five findings, all taken.** The
first port gated a `default` arm on a clean scan -- "arbitration is not
non-readiness" -- and so a select with a `default` crossed a suspension point
on the first refusal and parked on the eighth, against `LANGUAGE.md` ("with
`default` ... executes immediately"). The language norm outranks the
arbitration argument: the default wins, the refusal it counted is reset by
the win, and the gate survives only as the twelfth mutant with its own row
(nine polls against a held claim, nine defaults, no state left). Second: the
wake after `abandon_push` inside `channel_stage_into_ring_locked` was
vacuous (the reservation lived and died under one hold of the owner lane)
and, worse, released that lane between a caller's refusal and its retry
registration -- removed. Third: an exhausted select refused by a park take
re-polls by self-token, and that re-poll went uncounted -- it is a
republication now. Fourth: the first port returned REFUSED from the recv core
on a BUSY sender slot mid-loop, which made `try_recv` answer false while a
later parked sender could still deliver -- reverted to "wake the sender
unacked, look at the next", as the lane had it; park-take refusals are
counted only where the lane counted them, the staged send. Fifth: a NULL
task read as "exhausted" and would have parked nothing -- reads 0.

**One ordering the stand caught.** The first port registered the retry keys
AFTER the readiness keys and held the sync point between them; a close
crossing that gap settled the readiness registration and the verify stand
counted one pin where it expected two. The retry registration now precedes
the readiness keys: what a close crossing the window can settle is exactly
what the verify answers for. Two mutant builds also failed to compile under
`-Werror=unused-function` (the waker with no caller, the class pop with no
caller) -- fixed by keeping the waker referenced and fencing the class
helpers with the same mutant, so each mutant differs from the tree by the one
thing it names.

**Gates on the working tree.** `c-check` green; `check_sync_points.sh` green
with the new window `SP_CHANNEL_SELECT_REFUSED_BEFORE_RETRY_REGISTER` in
`rt_async_select.c`; panicgate green with two `PG-ALLOC-FAILURE` rows for the
select retry allocations; file-size gate `violations=0` (effective:
`rt_async_internal.h` 549 of a 676 base, `rt_channel_lane.h` 400,
`rt_channel_sync.c` 481 -- nineteen to the line, which is why the
release-side wake lives in `rt_channel_retry.c` and D7's claim registry goes
to a file of its own, `rt_async_select.c` 462); gatecheck roster green with
the new waiter-gate line.

**The runner, meanwhile, on `479a75e6` (before D6).** `baseline.sh`: 0 red.
W8 ×5: 5/5 red, every run on exactly the four lifecycle static pins that
still named `__task_create` after the constructor split -- `da5a4d2e` fixes
the pins, nothing else was red in five aggregates. `TestMTCorrectnessWakeups`
×100 under `taskset`: 0 red of 100. And the HUP differential the day before
had asked for: the same worktree, the same binary, `goldencheck/HUP` once
under `nohup` and once under `setsid` -- `FAIL 45.013s` and `PASS 0.016s`.
The row was never the tree's; it was the launcher's signal disposition, and
every count on the runner goes through `setsid` from here on.

### D7, Close-wins: the receiver a rendezvous pops stays the channel's (2026-09-03)

**What was true.** A rendezvous popped the oldest parked receiver out of the
FIFO and released the owner lane to move the value. Popped, the receiver was
in no store: `rt_channel_close` drained every parked peer except the one about
to be handed a value, which then received it on a closed channel. The Go
model has had the answer since the claim work (`internal/asyncrt/
channel_recv_claim.go`, four lifecycle rows); the native channel had only the
FIFO. Two neighbours rode along: RV2-DEBT-276 (the recovery for a receiver
that died inside the staging window destroyed the staged value and then
re-sent the husk `src` had become) and RV2-DEBT-279 (a row whose premise --
"the pin has no call sites" -- the pin work of RV2-DEBT-155 had already
falsified).

**The registry.** `struct rt_channel` gained `recv_claim` (`rt_channel_claim.h`):
the popped receiver's registration, `active`, `close_won`. Every rendezvous
pop opens it under the SAME hold of the owner lane as the pop -- in the send
lane before the value is staged (staging releases the lane for the move),
and in the try-send core before it answers; the select send arm goes
through the core. While it is out, no send is admitted
-- not to the ring either, the Go rows say so -- and a send that meets it is
REFUSED with a fourth cause, `RENDEZVOUS`, counted against the D6 budget;
retiring the claim is the release that wakes an exhausted retrier. Commit is
`channel_recv_claim_take_locked`: the claim is the sender's, deliver; or close
settled it first, and the payload is destroyed exactly once. Abort puts a
still-parked receiver back at the HEAD of its FIFO (`channel_push_candidate_front_locked`,
re-pinned) instead of waking it to re-register behind everyone who came
meanwhile. `rt_channel_close` moved to `rt_channel_close.c` unchanged in its
walk and settles the claim first -- it was the head of its FIFO. Owner-lane
order decides: for a foreign receiver the commit point is the take under the
lane, and a close after it finds the value delivered.

**The drop that could not run where it was.** The control-lane finish
(`rt_channel_finish_put_owner_locked`) runs with control held, and a drop may
not. The first port destroyed the close-won payload inside it and the stand
answered with `rt_value_drop_in_place_detached was dispatched while a runtime
lock is held`. The finish now hands the value back as `RT_CHANNEL_PUT_ORPHAN`
and the caller destroys it with no lock held and the operation pin still in
hand (`rt_channel_release_orphan_put`): `rt_channel_try_send`, the blocking
send loop, and `rt_select_poll`, which carries the orphan past its
`rt_control_unlock` like it carries a won recv arm's value. The same shape
now covers the dead-receiver case in the finish, which used to drop under
control too.

**Before / after, literally.** Stand `internal/vm/testdata/channel_close_wins_modes.c`
(on the claim-retry fixture; an unbuffered channel, the sender on the
control-lane pair so the window is open while the driver acts):
reserve→close→commit reads `receiver=closed drops=1 wakes=1 claim=retired`
(before: the receiver read VALUE after close); reserve→commit→close
`receiver=value drops=0 wakes=1`; reserve→close→abort→abort
`receiver=closed requeued=0 drops=0 wakes=1`; claim-not-overtaken:
`try_send` refused, a task's send republished once with count 1, the commit
reached the first receiver, the released sender met the second; dead-receiver
(276): the sender parks holding its slot, `try_recv` reads 7277 --
before, 0. Trace: `channel_recv_claims_opened/close_won/aborted`,
`channel_claim_refusals_rendezvous`, `channel_rendezvous_recoveries_dead_receiver`,
`channel_values_destroyed_in_recovery` (its only writer is the 276 mutant --
a hard zero by construction, and said so). Four mutants, each red on its
named line: close ignoring the claim ("a receiver was handed a value on a
closed channel"), an open claim admitting later sends ("a later send overtook
the active rendezvous" -- the mutant also commits past the claim, or it would
have died on the invariant panic instead of its line), the old recovery
("recovery destroyed the value"), and the claim opened after the stage ("the
claim was not open while the lane was released", the review's finding, see
below). RV2-DEBT-279 closes by census: eleven pin
sites, the print is a panic, the pin control is armed by the freed-waiter
ASan row, and `unpinned-claim` shows `rt_channel_assert_pinned` refusing --
on a channel with no registration, because a registration holds a pin of its
own and the assert answers for the count, not for who took it.

**The independent review, round one: REJECT, two blocking findings, both
taken.** The first port opened the claim AFTER `rt_channel_stage_locked`,
and staging releases the lane for the move: for the length of a move the
popped receiver was in no store and in no claim, a close crossing there
settled nobody, and the commit that followed delivered a value on a closed
channel -- the very defect the close-wins mutant was written to catch, in
the tree. The same gap let a second sender pop the next receiver and open
the claim first, and the first sender's commit then died on the "no claim on
its receiver" invariant. Fix: the open moved to the hold that pops (the
no-slot path aborts the claim instead of re-inserting by hand), the old
placement survives as `RV2_CLAIM_OPEN_AFTER_STAGE_NEGATIVE_CONTROL`, and
the take also refuses on `ch->closed` as the model does at commit. The row
that proves it, `claim-window-close`: a sender held by
`SP_CHANNEL_RENDEZVOUS_CLAIM_BEFORE_MOVE` with the lane released, the
driver reads `recv_claim.active` under the lane (1 in the tree, 0 under the
mutant), closes, sees the receiver settled closed with one wake, and the
released sender dies on "send on closed channel" -- the process exit the row
expects. Two deviations from the Go model, recorded rather than argued
away: `rt_channel_try_send` still answers 1 when close wins its commit,
because the value was MOVED out of `src` and a 0 would hand the caller a
husk (the model copies at commit and can answer false); and the receive
side is not gated by an open claim (the model gates `ChanTryRecv`), which
matters only for a sender parked without a slot -- noted, not closed. An
orphaned claim after a non-local exit out of the move (longjmp) would block
the channel for ever where before it lost a slot; `rt_channel_abandon_send_locked`
is the control-lane pair's abort for that, called today by the stand alone.

**Gates on the working tree.** `c-check`, panicgate (two rows for
`rt_channel_claim.c`), sync-points, file-size (`rt_channel_sync.c` 434 after
the move, `rt_channel_claim.c` 199, `rt_channel_close.c` 57,
`rt_async_channel_send.c` 226, `rt_async_channel.c` 302,
`rt_async_select.c` 478 -- twenty-two to the line, the next select change
splits it), gatecheck
roster with the new waiter-gate line, the D6 stands and the select/channel
smoke re-run on this tree: all green.

### D4.8, the acceptance at 2, 4 and 8 carriers, and RV2-DEBT-307 closed (2026-09-03)

Five compiled programs, one shape each from the plan's list, at
`SURGE_SHARDS=1` with 2, 4 and 8 threads -- the only topology with several
carriers on one shard, so the only one where a pin is a fact
(`TestRuntimeV2CarrierAffineAcceptanceMatrix`): the borrow spawn; the
borrowed place read by the child after the PARENT suspended and resumed
eight times (resident storage: the address the child dereferences after its
own eight suspensions is the one the parent wrote); sixty-four yields on the
carrier, each reading the place; a borrowing child cancelled by its parent
and joined (Cancelled, on its carrier, nothing left for the exit); and a
by-value child, which is NOT pinned. Each run is read through the scheduler
trace -- `carrier_pinned` at least one where a borrow exists and exactly
zero where none does, `carrier_shutdown_cancelled` zero -- and through the
refusal the runtime makes on every poll: a pinned task polled off its
carrier panics, and a run that answers has never been. Fifteen runs, all
green, on top of the harness rows already at 2/4/8 (addressed publication;
the never-polled child cancelled at exit). RV2-DEBT-307 closes: every clause
of its acceptance column has a row it can be read off, from the two-program
diagnostic (4.1) to the by-value child that carries no pin.

**One trap, twice today.** `go build` of the `surge` binary -- which every
e2e row does -- fails in a worktree under `/tmp` with "error obtaining VCS
status: exit status 128" while `git status` by hand is green: a stray, empty
`/tmp/.git` (September 1, not ours) is what `go` takes for the VCS root.
Golden-check and the acceptance matrix both ran with
`GOFLAGS=-buildvcs=false`; the runner's `/tmp` is clean and its gates run as
written. Recorded, not deleted: it is not ours to remove.

### D8, the Wave D freeze: candidate `c38e4275` (2026-09-03, in progress)

**The candidate.** D7 (`71396627`) was the last scheduler/carrier/channel
change of the wave; D4.8 (`c38e4275`) is a test and its gate line on top.
Nothing in Wave D's list is left to land: D0 (fast-forward, the SEM3209
port, the 277 patch saved), D0b (the pre-existing reds, each with its cause),
D0c (the splits), D4.5–D4.8, D5, D6, D7. `c38e4275` is the freeze candidate,
and the rows the plan's step 8 names run on it, on the runner, one after
another, from one queue script (`/srv/ci/queue_c38.sh` → `baseline.sh`,
`w8count.sh` ×5, `d8.sh`): file-size `--committed`, golden twice plus
corpus determinism, `behaviour-check-all`, the MT corpus at 2/4/8 × 5,
`TestLLVMParity` at 1×1, 1×8 and 8×8, the carrier census pair, the whole
tagged `internal/vm` suite, the sanitizer lane, then the 1000-repeat
campaign on cores 8–15 with the rest of the box idle (each run's llvm PASS
leaves counted, so a vacuous run reads as a number), then W8 ×2 again, then
the DEBT-312 rate (`rate312.sh`, 200 iterations, a live dump on the first
red). The counts land here as they finish.

**Sentrux, on the candidate, locally (the runner has no binary).** All four
scopes pass every rule: `.` quality 6157 (10 rules), `internal` 6446,
`runtime` 5287, `runtime/native` 5415 -- against the Epic 12 record in
`SENTRUX_POLICY.md` of 6189 / 6532 / 5195 / 5159: the two Go scopes a
fraction lower, the two runtime scopes higher. No D0 baseline was taken
separately; this scan is the wave's baseline, and F7's "final" is measured
against it.

**One red to attribute before the freeze.** The D6 baseline (`91c41e43`)
read one red of the whole `go test ./...`: `TestMTCorrectnessHTTPServer`,
"server had already exited (wait: <nil>); the client gave up 1.166s after
start", connection reset -- while `479a75e6` the day before read zero, W8
on `91c41e43` read 4/4 green including the HTTP owner gate, and the same row
is 5/5 green on WSL2. Rule: a rate before a bisect. `http_diff.sh` runs the
row twenty times on `479a75e6` and twenty on `91c41e43`, sequentially and
alone on the box; the two counts decide whether D6 owns it.

**The HTTP differential, read.** `479a75e6`: 1 fail of 20. `91c41e43`: 0 of
20. The red predates D6 and is a pre-existing flake of the HTTP server row;
D6 does not own it, and nothing in this wave does.

**The rows on `c38e4275`, as they landed (all on the runner, one queue).**
`baseline.sh` (`go test ./...`, SURGE_STDLIB at the root): 0 red. W8 ×5:
5/5 green, 860/851/852/854/852 s. `file-size --committed`: PASS,
`violations=0`. `golden-check` twice and `golden-corpus-determinism`: green,
148/148/147 s. `behaviour-check-all`: green, 327 s. The MT corpus at
2/4/8 × 5 repeats (llvm): green, 121 s. `TestLLVMParity` at 1×1: green.
The carrier census pair: green. The sanitizer lane: green, 115 s -- it is
`make runtime-v2-carrier-sanitizer-check`, six `go test` groups (ASan/UBSan,
TSan, the realloc rows); the plan's "13 valgrind rows, 2100 s" describes no
target in this tree, and the number is recorded here as what the target
actually is.

**Two instrument defects, found by the rows themselves.** The topology rows
drove `TestLLVMParity` under `SURGE_SHARDS`/`SURGE_THREADS`, and the parity
harness pins `SURGE_THREADS=1` for every program (`envForParity`): 1×8
measured 1×1 and read green for nothing, 8×8 refused at runtime start on
every program (`async: SURGE_THREADS must equal SURGE_SHARDS when
SURGE_SHARDS>1`, rc=1, fourteen leaves). Neither was a runtime red. They are
re-done by `queue_topo.sh` over the `TestRuntimeV2*` family, which inherits
the environment unless a test sets its own matrix. The tagged-suite row
forced `SURGE_BACKEND=llvm` over the WHOLE `internal/vm` package, and six
VM-only tests (panic text, imported magic operators, the MT executor) read
that as their own failure (rc=1, 1162 s); the Makefile's tagged sub-gates
force the backend only over `TestRuntimeV2*`. Re-done by `queue_tagged.sh`
as two rows: the tagged package with no backend override, and the
`TestRuntimeV2*` family under llvm.

**The campaign is red, and the red is a known open window.** 1000 repeats of
`TestRuntimeV2(FailfastJoinAnswersCancelled|TimeoutTargetAnswersCancelledToEveryHandle)`,
llvm, `SURGE_SKIP_TIMEOUT_TESTS=0`, `taskset -c 8-15`, the rest of the box
idle, every failing log kept: after 454 runs, 16 red (3.5 %), 0 vacuous,
every one of them `FailfastJoinAnswersCancelled/llvm/threads-4: exit=13 --
the second @failfast block (both children cancelled before they ran)
resolved Success`. That is RV2-DEBT-261's program-level row and the
RV2-DEBT-263 window (a cancelled child committing Success, so fail-fast
never fires), both Open; 263 records a third window still open after
RV2-DEBT-265 was refuted by measurement, and RV2-DEBT-266/280/283 name the
one remaining candidate -- scope teardown under the control lane against
child-done under the pinned shard lock -- whose mechanism is owner question
Р6. Before this wave the same instrument read roughly 0.5 % on this box
(1–3 of 200, DEBT-261's own reading at `082ffae9`). Whether Wave D raised it
is a rate question and is measured as one: `queue_ff.sh` runs 300 rounds on
the pre-wave base `8b12beb3` and 300 on `c38e4275`, same command, same
affinity, back to back with the box idle. The freeze of `c38e4275` waits on
the campaign's final count and on that differential; the wave's freeze
condition (§1.8, the campaign) is not met by a 3.5 % red whatever its
provenance, and the fix runs through the open scope-serializer decision.

### E1, the splits before the change (2026-09-03)

Wave E rewrites the far channel and the transport, and the file-size gate
reads any added line in a file over the line as `LEGACY_GROWTH`. So the
files are split first, with no change in behaviour, and each split is proved
the same way: the multiset of code lines (comments and blanks stripped) of
the old file equals the multiset of the new files, minus the lines a split
must add or drop -- `#include`s, guard macros, a `static` that became a
declaration, a forward declaration that became unnecessary.

- `rt_far_channel.c` 630 → 363 + `rt_far_channel_crossing.c` 264: the
  registry, its leases and pins stay; create and share, with their
  dispatchers, move out. Nothing private crosses the seam -- both
  dispatchers mint through the registry's public entry points -- so the
  only delta is four includes on one side and two forward declarations
  (the lease helpers, now defined before their first use) on the other.
  The new name is outside the `rt_remote_task*`/`rt_immediate_on*` glob
  the 360-line remote-task pin reads.
- `rt_transport.c` 452 → 230 + `rt_transport_park.c` 238, with
  `rt_transport_internal.h` (11) as the seam: the envelope queues and the
  wake pipe stay -- what a message IS -- and admission, the park/wake
  protocol, shutdown and the drains move -- what a message DOES to a
  shard. Five statics became the seam's declarations. What followed the
  move: six sync-point windows re-homed in `check_sync_points.sh`, four
  panic rows renamed in the panicgate ledger, the transport contract test
  reads the windows from the new file and both C harnesses (the contract
  and the spine acceptance) compile it.
- `runtime_v2_remote_publication_harness_common_test.go` 497 → 362 +
  `_polls_test.go` 138: one Go constant of C text, cut at
  `__surge_poll_call`; the assembly concatenates the halves in the old
  order, so the C the stand compiles is byte-identical.

Gates on the split tree: `c-check`, sync-points, panicgate, file-size
(`violations=0`), the tagged transport contract rows, the gate roster; the
far-channel, remote, transport and remote-publication rows re-run green.
`remote_task_behavior_anchored.c` (485) is left alone until a change
touches it.

### E2, a far crossing carries its payload at the payload's own width (2026-09-03)

The plan's E2 named five things: a real `plan_cross`, an envelope with a
header in the transport queue, a channel that owns its payload bytes, a
typed select winner, and the removal of `out_bits`/`result_bits` and the
numeric drop dispatch. Read against the tree after Wave D, three of the five
were already there under other names and one is not needed in-process; what
was left is narrower than the plan's text and is recorded exactly.

**What the tree already did.** A far task's result, an immediate-`on` reply
and a select winner reach the caller through the canonical typed slot
(`rt_result_source`, D-side), a channel cell is sized by its element's
descriptor (`rt_channel_new(capacity, ops, element_type_id)`), and a far
select's SEND arms already cross with the channel's element type id. The
transport message still carries `void* payload` into owner-side storage,
and that is correct in-process: the payload never leaves the storage its
owner shard sized for it -- the channel cell, the result slot -- so there
is no second copy for an envelope to own. `rt_envelope_header` and
`rt_cross_plan` stay reserved for a serialized transport; E4's reservation
token goes on the pending and the message, not on a byte envelope. That is
the E2 deviation from the plan, and E5 measures resident bytes on this
shape.

**What was word-only, and what replaced it.** Four residues, all named by
the census: `rt_remote_task_pending.result_bits` threaded through the
reply/finish signatures of four files (`set_reply(pending, status, kind,
source)`, `finish(...)`, `reply_or_finish*(...)` lost the word);
`payload_drop_fn_id` on the pending, which was already a type id in all but
name (`payload_type_id`); the two `out_bits` scratch words of far create and
share that nothing ever read; the select reply's `out_bits`, which is a
winner INDEX and is now called one (`out_winner`); and the scheduler
deque's `uint64_t* buf` of task ids (`task_ids`). On the emitter side the
heap-only gate went: `registerCrossingDropResult`, which registered nothing
and was called only for a payload that owned heap, is deleted, and every
far crossing names its payload type through `registerCrossingPayloadType`
-- channel create, far task await and cancel -- for every type, a scalar
included. `abandonedStateDropID` became `abandonedStateTypeID`: it carries
the frame type's id, which the runtime turns into the frame's descriptor,
and never was a drop id.

**The runtime stops answering "a word".** `rt_channel_element_ops_for`
used to hand `rt_channel_opaque_word_ops` to any id it could not resolve.
That was the width defect: a `bool` or `int8` channel got eight-byte cells
and eight-byte moves out of a one-byte place, and a sixteen-byte `@copy`
composite lost its second field. Now a nonzero id with no compiled
descriptor panics (`async: no descriptor compiled for the crossing's
element type`, panicgate row `PG-INVARIANT`), and every compiled crossing
names its element, so the panic is a compiler defect's, not a payload's.
Id 0 keeps one meaning, "no type named at all": a C stand that links no
compiled lookup, or the select body's winner index, which really is one
word. Both get the opaque-word descriptor with every slot filled -- the
capability question RV2-DEBT-164 asked for is answered by the id, not by a
null slot.

**Witnesses.** `TestEmitCrossingsNameEveryPayloadType`: a `bool` channel
and a `bool` far task cross with a nonzero id that `__surge_value_ops_for`
resolves, share crosses with four pointers and no payload word, and the
retry calls still pass 0 because the runtime reads none of those arguments
on a retry. `TestRuntimeV2FarPayloadWidth`, the corpus's first crossing
payload wider than a word: a sixteen-byte `@copy` composite through a far
channel (`on ch` send and recv, sum 42) and as a far task result (`spawn on
distributed`, sum 42), and an `int8` through a far channel, at
`SURGE_SHARDS=1` and `2`, then under valgrind: no memcheck error,
definitely lost 0 bytes in 0 blocks. Rule 13: the old gate back in
`registerCrossingPayloadType` (`if !typeOwnsHeap { return 0 }`) turns the IR
test red (`channel_on::<bool>: payload type crossed as id "0"`) and the e2e
into `signal: segmentation fault` at 52 ms on both shard counts.

**What E2 did not lift.** `TriviallyTransportableBits` is still "Copy and
no counted scalar": a move-only composite is refused as a reply or an
awaited result with `FUT7020`, and this row's composite is `@copy` for that
reason. The move-only case is a question of who owns the value on each
side, which is E3's owning anchor, not a width question.

**Census.** The live ratchet reads `native-payload-bits` 0,
`native-word-carrier` 0, `numeric-drop-dispatch` 0 and
`llvm-erased-word-bridge` 0; what remains live is `composite-box-marker` 7,
`llvm-pointer-word-ir` 1 (the fixnum allow), `vm-async-any-carrier` 14 and
`vm-universal-owner` 67 (F6b). The allow `scheduler-deque-task-id-buffer`
is removed: its token no longer matches anything and the ratchet refuses a
stale allow. The frozen migration rows stay -- they are the census of the
commit they describe. RV2-DEBT-164 is closed on this.

**Gates.** `c-check`, sync-points, panicgate (one row added), carriergate,
the `llvm`/`abimanifest`/`mir` units, and the far-channel, far-select,
far-task, remote-task, immediate-on, anchored and crossing e2e family
locally (233 s, green) before the runner rows.

### E3, a far handle has one owner across a crossing (2026-09-03)

Two rows, one ownership question each.

**RV2-DEBT-198, the far half: a far handle held as a field is released.**
`typeOwnsHeap` answers YES for a far channel handle and `emitDropHandle`
reaches `rt_far_channel_handle_drop`, the release a far LOCAL's scope-exit
drop has always made; widened together, as the channel was on 2026-08-26.
The agreement gate's `unreclaimedFamilies` is empty and stays declared,
`far Channel<int>` and `FarGate` are ordinary rows in both legs, and the
helper map names the release. Witness
`TestRuntimeV2FarHandleFieldDropReleasesTheLease`: a holder dropped at the
end of the frame that built it and one moved into a callee that drops it;
valgrind strict zero. Rule 13, the walk's far arm removed: exit 0 and
`definitely lost: 48 bytes in 2 blocks`, the two caller-side tokens.

**RV2-DEBT-082: the anchor is a lease, spelled by not being a capture.**
The ownership map, read before any code: the anchored block took the
caller's token THREE ways -- a struct copy into the pending
(`rt_immediate_on_anchored.c`), a consuming read into the crossing state
(MIR's capture loop), and the caller's own binding left live (sema's
deliberate exception) -- and exactly one of the three ever released a lease,
the caller's scope exit. The probe: `let held = ch;` inside
`on ch { ch.send(1); ... }` compiled without a word on `1fdd6383`. Under
valgrind it produced no invalid access, which is itself the finding: the
body's binding consumed the CALLER's handle (the shipped pointer was the
caller's box, the body's drop released it, sema's body-side move marked the
caller's symbol moved), so the two owners were one owner chosen by whichever
side moved first. Incoherent, not corrupting.

Now the anchor of an `on far_handle` block crosses in its own mode,
`CrossingCaptureAnchorLease`, the row's first candidate: the caller keeps the
handle and its drop (`checkOnCaptures` observes no move,
`registerCrossingBodyOwnership` registers nothing for it), MIR lowers it as a
BORROWING read rather than the consuming read every other capture gets, and
the body's copy of the token is a lease view it never dereferences and never
drops; the body reaches the channel only through the pin
`rt_far_channel_pin` takes at dispatch and gives back at the reply edge, the
same path the anchored ops always used (`rt_anchored_channel_*` resolve
through the pending, never through a token). The first cut left the anchor
out of the capture list altogether; the `far TcpConn` control form lowers
its `close()` as an ordinary call on the receiver and needs the body local,
so the mode is what states the rule, not absence. `checkAnchorLeaseUses` walks
every identifier in the block (the capture scanner, refactored into
`scanCapturedIdents` so the first-occurrence dedupe is the caller's, not the
walker's) and refuses any use of the anchor that is not the receiver of the
block's channel operation: `SEM3210`, fixture
`on_negative_anchor_lease_misuse.sg`, `TestAnchorLeaseMisuseIsRejected`
(bound inside the block; passed to a function inside the block).
`anchored_far_channel_still_usable` stays green and
`anchor_still_usable_after_the_block` -- two blocks on one handle -- is new,
because a lease is not a move.

A far handle captured into a NON-anchored crossing is the other case, and it
is a move: `observeMove` at the crossing, so a later use is `SEM3130`, and
the body owns the lease (`registerCrossingBodyOwnership` registers far
captures). The row that pins the crossing-site observation is
`far_handle_read_after_capture` with a WILDCARD binding in the body: a body
that binds the handle to a name moves it itself and marks the caller's symbol
moved either way, which is why the first version of that row passed the
mutant (`observeMove` skipped for far handles) and had to be rewritten; the
wildcard moves nothing sema can see, and the mutant accepts the program.

Runtime, the same map's findings: the pending records `anchor_pinned`, every
unpin path goes through `rt_immediate_on_anchor_unpin` (once per pin,
whichever of the four paths runs), and `rt_far_channel_unpin` refuses a
non-channel token and an entry with no pin left -- it used to subtract from
an unsigned counter unconditionally, and a share request rides the same
pending slot with a channel token. Panicgate row `PG-INVARIANT`. What the
map also found and E3 did not fix: `rt_remote_task_fail_all_pending` severs
the owner registration that the reply-edge unpin depends on, so a pin held
by a body that shutdown prevented from completing is never given back and
`rt_far_channel_release_all` leaves the entry (RV2-DEBT-322, with the reason
the safe place is not obvious).

**Controls, both valgrind strict zero at shards 1 and 2.**
`TestRuntimeV2FarHandleHasOneOwnerAcrossACrossing/anchored`: create, share a
sibling, a block on each handle, two recv blocks, scope exit.
`/move_in`: a handle moved into a `spawn on distributed` body that binds and
drops it. The anchored behaviour rows (`TestRuntimeV2RemoteTaskBehavior`,
the self-deadlock row, the on-ch, non-copy, drop-far-channel and far-select
e2e rows) re-run green on the pin flag.

**Not lifted.** `FUT7020` still refuses a move-only composite as a reply or
awaited result (E2's note); the far TASK handle as a struct field has no
release in the glue (a never-awaited far task's lease), which no row names
yet and this one does not claim.

### E4, the reply's slot is reserved with the request and a producer parks (2026-09-03)

RV2-DEBT-031's two open halves, read against the tree: whether a reply takes
an ordinary data slot or one reserved at admission, and the producer park in
place of the caller-visible `QUEUE_FULL` that `rt_remote_spawn_enqueue_with_drain`
turned into a drain-once-and-retry. The third half, a byte credit, stays
where the 29.08 ruling put it: there is no transport-owned buffer to measure
one over, and the model text no longer pretends otherwise.

**The map first.** One admission site (`rt_transport_push_locked`'s
`len >= cap`), one escape point for a refusal (`rt_transport_enqueue_locked`),
five data-lane reply kinds carrying a live result pin when enqueued, seven
request submission sites of one shape, four `enqueue_with_drain` callers,
seven LLVM switch arms with eight string constants, and not two but nine test
rows asserting the refusal -- the plan named two. `SP_TRANSPORT_DATA_SLOT_TASK_PARKED`
was declared, allowlisted to `rt_transport.c` and armed by nothing;
`docs/RUNTIME_V2.md` §8 named three identifiers that no longer exist.

**Reservation.** `rt_transport_state.reply_reserved` counts data slots
promised to replies. A request that expects a data-lane reply reserves one
slot on ITS OWN shard's lane before it is enqueued anywhere
(`rt_transport_reserve_reply_slot_locked`: refused when
`data_len + reply_reserved >= cap`); ordinary data admission counts the
reservations as occupied (`len + reply_reserved >= cap`); the reply spends
the reservation (`rt_transport_push_reserved_reply_locked`, never refused,
because the invariant makes a held reservation a free physical slot); a
pending resolved without a reply gives it back. Exactly one of spend and
release happens: `rt_remote_admission.reserved` is exchanged to 0 by the one
that gets there (`rt_remote_admission_take_reservation`), and the pending's
final free is the belt, counting an orphan rather than deadlocking if a lock
forbids the release there. Control acks (spawn, cancel) reserve nothing.

**Park.** `rt_remote_admit` (`rt_remote_admit.c`, new; the allowlist row
for the sync point moved there) does both lanes in order. A refusal on
either registers the task on that shard's slot key --
`WAKER_TRANSPORT_SLOT`, id and store both the shard's, in the seq-0
terminal-drained set -- through `prepare_park`, records the park, reaches
the window, and retries once (register-then-verify); the crossing answers
PENDING and the caller's next poll retries through
`rt_remote_task_retry_admission`. A data pop on a lane wakes every producer
parked on it (`rt_transport_wake_slot_waiters`, called in the drain loop
with the shard lock dropped, before the message is dispatched); a released
reservation does the same. Shutdown wakes every slot key and the retry
answers `DESTINATION_SHUTDOWN` (`refused_by_shutdown`), outranking the
caller's own cancellation; a cancelled caller abandons an unsent request as
Cancelled; the teardown sweeps abandon a parked admission with the caller,
and the abandonment is exchanged once between the producer's own retry and
the sweep so the never-enqueued message reference is released exactly once.
The spawn family got the same admission with `wants_reply = 0`.

**What it cost to get the park to hold.** The first version registered the
task and returned PENDING, and the flooded caller parked 1,054 times in a
row with zero wakes (2,108 slot-credit stalls, two per attempt). A poll's
terminator commits the park on the thread-local `pending_key`, which
`rt_remote_task_prepare_reply_wait` sets and `slot_register` did not: the
yield was a plain requeue and the "park" a spin against the lane. The second
finding was the stand's: `poll_anchored_flooded_caller` re-saturated the lane
on every poll, including the one it was woken for. The third was the
topology's: under two carriers a producer woken by shutdown may never be
polled again (its carrier has exited), so rows that resolve a park by
shutdown read the pending and the registry, not the caller, and the one that
needs the caller's own poll (`drop-queue-full`) requests shutdown from inside
that poll under one carrier, where the driver's await is the pump.

**Numbers.** `anchored-saturation-parks-the-producer-and-a-freed-slot-wakes-it`
(2 shards): `parks=1 wakes=1 data_stalls=0/2 reserved=0/0`, the window
reached once. Rule 13 (`TestRuntimeV2TransportSaturationParkNegativeControl`,
`-DRV2_DEBT_031_NEGATIVE_CONTROL` restores drain-once-and-refuse): the stand
goes red on its first assertion, `saturated data lane did not park the
producer`. The nine rows: the anchored one above; `queue-failure` (await parks
on its own reserved lane, shutdown resolves it, the reserved slot returns,
the lease registry empties); `drop-queue-full` (parked request shut down
from inside its poll, `DESTINATION_SHUTDOWN`, one drop); the publication
harness's spawn/immediate/select refusal rows (park, shutdown, one drop, no
body); `TestRuntimeV2TransportSlotCreditReserve` and the transport contract
stand unchanged -- the lane still refuses, the park is above it; the far
select static contract re-anchored on `rt_remote_task_submit`.

**Compiled code.** No QUEUE_FULL arm in any of the seven crossing emitters and
no "queue is full" panic text; the IR tests assert the arm's absence; the
panicgate ledger lost seven positional rows and gained six for the new
refusals (`PG-INVARIANT`: a reply spending a reservation nobody held, a
release without a reservation, the lock-discipline panics of the new entry
points). `rt_transport_park.c` keeps the queues and the shard park and holds
no waiter dependency, so the transport's own stands still link without a
scheduler; the wakes live in `rt_remote_admit.c`.

**Also closed on the way.** RV2-DEBT-062: the far-select cancel copy control
pinned its shape as a 48-byte-in-2-blocks baseline (the two far-channel tokens
a cancelled task's abandoned frame held as fields); E3's far arm reclaimed
them and the pin was re-taken to strict zero. `docs/RUNTIME_V2.md` §8 now
describes the reservation and the park and names the counters that exist.
`rt_immediate_on_release_owned` moved to `rt_immediate_on_teardown.c` for the
360-line remote-task pin.

**Deferred, and why.** The two carrier-bench liveness probes on
`SP_TRANSPORT_DATA_SLOT_TASK_PARKED` (`jumbo-credit-cancel`,
`jumbo-global-shutdown`) stay `deferred` until E5 runs the bench with its
credit counters wired; the window they wait on is armed now. The slot wake
is a wake-all of the parked producers on that lane, each re-parking if it
lost the race; exactly-one holds for the one-parker stand and a wake-one is a
refinement nobody has measured a need for.

**A row the park left behind, found by E5's gate run (2026-09-03).** The
remote task acceptance gate (`Makefile` line 442's roster) was not run
locally for E4, and its `TestRuntimeV2RemoteSpawnAbandonEdges/refusal-queue-
full-drops-once` asserted the refusal E4 removed: it flooded shard 0's data
lane under one carrier and awaited `QUEUE_FULL`; with the park, the publisher
parks, nothing on the one carrier drains the lane, and the deadlock detector
answers for the missing refusal -- `surge: fatal [PANIC]: async deadlock`,
exit 1, red on `ba7f13e4` (2/2 alone, 3/3), green on `341f1978` (2/2). The
premise is gone, not the property: the parked-then-shut-down publication and
its exactly-once drop are `queue-full` in
`TestRuntimeV2RemotePublicationBehavior` (`run_queue_full`, rewritten by
E4). The row is removed; `run_refusal_drop` keeps its one remaining edge,
destination shutdown, and says so. Same commit day, separate commit.

### E5, what a crossing keeps resident, measured at each byte's owner (2026-09-03)

**What the plan asked, and what the tree had.** "Exact resident-byte
telemetry: header, padding, payload, sidecars and crossing clone; slot
budgets stay 64/16; normal and jumbo byte budgets are measured first; a new
numeric limit stops for an owner decision." The carrier bench's bridge
declared six recorders and reported five metrics, and only two had a
production caller (`record_copy` and `record_callback`, in the clone glue);
`record_move`, `record_credit_stall`, `transport_acquire` and
`transport_release` were called from one unit test, so `bytes_moved`,
`credit_stalls` and `peak_transport_bytes` were structural zeros in every
run, and the manifest's numbers for them (`credit_stalls eq 128`,
`peak_transport_bytes ge 8192 le 9472` on `far-jumbo-contention`) were
written against the byte-credit model the 2026-08-29 ruling withdrew. The
ABI's `rt_cross_plan` (envelope, payload offset, sidecar, total) is real and
frozen, and nothing populates it: the one `plan_cross` in the tree refuses.
E2 did not build the envelope-with-header the draft imagined; a message is
still a fixed `rt_transport_msg` over a pointer, and that is what the ruling
says it is charged for. So "header, padding, payload, sidecar" had to be
defined on the tree that exists, one owner per byte, or they would have been
numbers with no writer.

**The definitions (`rt_resident_bytes.h`).** ENVELOPE: the fields of one
`rt_transport_msg` per envelope in a lane, push to pop (36 bytes on x86-64).
PADDING: the bytes of that envelope no field occupies, beside it (4; the
static assert pins fields ≤ sizeof). RECORD: the pending that tracks the
crossing on the source side, `rt_remote_task_pending` (280) or
`rt_remote_spawn_pending`, allocation to last release. PAYLOAD: the shipped
state block at its descriptor's width, from `state_owned = 1` to the
publication-accepted handoff (`state_owned = 0`, the body's frame from
there) or to the pending's drop of a state that never shipped; plus a
remote select's staged SEND payload when it did not fit the arm cell's 16
inline bytes, until the table is freed. SIDECAR: the arm table,
`count × sizeof(rt_far_channel_select_arm)` (96 each), allocation to free.
Each kind: live, peak, acquired-total; a process-wide live and peak; a
release that outruns its acquire clamps to zero and counts an underflow.
CROSSING CLONE: charged by the LLVM emitter in the crossing's initial block
-- the width of every capture whose mode is `CrossingCaptureCopy`, once per
crossing, never on the retry poll (`rt_resident_bytes_record_crossing_clone`
is a total, since the copy lives inside the state block the PAYLOAD counts).
The exec trace (`SURGE_TRACE_EXEC=1`) prints one `TRACE_RESIDENT` line
beside `TRACE_NET`; a resource-capture bench build sees the same
acquires/releases through `transport_acquire/release`, every physical move
through `rt_value_move_init_detached` as `bytes_moved`, and every data-lane
stall (admission and reservation) as a credit stall. The far task's result
is NOT here: the reply pins the producer's slot, so a 4096-byte result reads
as resident in the producer's task and as 16 bytes of transport payload
(the index it captured plus the state word) -- exactly what §8 says.

**The table, for the owner's decision.** `ba7f13e4`+E5, `SURGE_SHARDS=2
SURGE_THREADS=2 SURGE_BLOCKING_THREADS=1`, release llvm, the manifest's own
fixtures and probes, one run each, WSL2 (widths are layout facts, not host
facts). Peaks are bytes; every row read `live_total=0 underflows=0` at exit.

| probe | payload | envelope | padding | record | payload | sidecar | peak_total | clone bytes (clones) |
|---|---|---|---|---|---|---|---|---|
| far-channel-composite | 64 | 36 | 4 | 280 | 80 | 0 | 400 | 4096 (64) |
| far-channel-scalar | 8 | 36 | 4 | 280 | 16 | 0 | 336 | 0 |
| far-immediate-composite | 64 | 36 | 4 | 280 | 72 | 0 | 392 | 4096 (64) |
| far-immediate-scalar | 8 | 36 | 4 | 280 | 0 | 0 | 320 | 0 |
| far-jumbo-contention | 8192 | 36 | 4 | 280 | 8200 | 0 | 8424 | 1048576 (128) |
| far-large-capture | 4096 | 36 | 4 | 280 | 4104 | 0 | 4328 | 524288 (128) |
| far-large-result | 4096 | 36 | 4 | 280 | 16 | 0 | 320 | 512 (64) |
| far-select-composite | 64 | 36 | 4 | 280 | 80 | 192 | 512 | 4096 (64) |
| far-select-scalar | 8 | 36 | 4 | 280 | 16 | 192 | 512 | 0 |
| far-share-control | 0 | 36 | 4 | 280 | 0 | 0 | 320 | 0 |
| far-task-composite | 64 | 36 | 4 | 280 | 72 | 0 | 320 | 4096 (64) |
| far-task-scalar | 8 | 36 | 4 | 280 | 0 | 0 | 320 | 0 |
| select-send-composite (local) | 64 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| select-send-scalar (local) | 8 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

What the table says. (1) The peak of every kind is ONE unit at every far
row: one envelope, one pending, one state block. The handoff empties the
transport before the next request is submitted, so `far-jumbo-contention`'s
two producers never hold two 8200-byte blocks at once -- its whole resident
cost is 8424 bytes, and 8200 of it is the caller's own capture, which was
going to exist anyway. There is no accumulation to bound: the transport
never owns more than one crossing's bytes per producer. (2) A state block is
its captures plus 8 (poll state), plus 8 more for the channel anchor; a
scalar capture that is not captured at all (`far-immediate-scalar`,
`far-task-scalar`, `far-share-control`) ships no block, `payload 0`. (3)
`peak_transport_bytes` under the manifest's `ge 8192 le 9472` for jumbo
would read 8424 today, `ge 4096 le 5376` for large 4328 -- inside the old
windows by accident of the pending's width, not by the model those windows
came from; `far-scalar` `ge 8` reads 0 at two of its four probes, so that
floor is false as written. (4) `credit_stalls eq 128` is unmeasured here:
the fixture reaches no stall at 2×2 with one crossing in flight per
producer, and E4's park makes a stall a park, so the number would have to
be re-derived from the park counter in a saturation probe, which is E6's
two-jumbo-producers row. (5) The local twins are zero: the instrument is far
traffic only, by construction. OWNER STOP №1 is the plan's, and it is this
table: the slot budgets stay 64/16 and no byte limit is derived here; the
frozen six-metric contract, the manifest's numbers and the two deferred
liveness probes are untouched until the owner says which of (a) keep the
old windows, (b) re-pin them to the measured one-unit peaks, (c) drop the
byte assertions and keep slots, they want.

**Stands and controls.** `resident-bytes-of-one-crossing-are-exact-and-
given-back` (behaviour table, 2×2): one immediate execute with a typed 8-byte
state -- payload acquired exactly 8, record exactly `sizeof(pending)`,
envelopes exactly two (request and reply, fields and padding apart), no
sidecar, `peak_total` at least state+record+envelope together, every balance
zero afterwards. Rule 13: `RV2_E5_RESIDENT_NEGATIVE_CONTROL` drops the one
release at the handoff and the row reads "payload bytes still resident
after the crossing completed"
(`TestRuntimeV2RemoteTaskResidentBytesNegativeControl`, on the remote task
acceptance gate). `TestRuntimeV2ResidentBytesTelemetry` (untagged, llvm):
two crossings of a 64-byte Copy value, `crossing_clones=2`,
`crossing_clone_bytes=128`, `payload_acquired ≥ 128`, every `*_live=0`.
`TestRuntimeV2ResidentBytesLedger`: the module alone, peak 15 after 10+5,
clamp on a 40-byte release of 36 with `underflows=1`, `peak_total=51`, one
dump line. `TestEmitCrossingChargesCopyCapturesOnce` / `...DoesNotCharge-
MovedCaptures`: the IR carries `(i64 64)` exactly once for a Copy capture
and no call for an `own` one. Gates local: file-size `--committed` PASS
(1200 files, 0 violations), llvm / mir / sema / panicgate / carriergate /
gatecheck / abimanifest / buildpipeline units, the carrier-bench VM tests
(the counter matrix is untouched, five keys), the transport contract roster
(the two transport-only stands now link `rt_resident_bytes.c`), the remote
task acceptance roster after the row fix above, strict-warning compile and
cppcheck on every touched C file.

**Found on the way, not fixed here.** `make ctidy` (CI's `c-check-clang`)
is red on every file that includes `rt_async_internal.h`, on `ba7f13e4` and
on `341f1978` alike: `clang-analyzer-optin.performance.Padding` reports
`struct rt_task` at 33 padding bytes where 1 is optimal, with a suggested
field order. `git log -S carrier_worker_id` dates the tipping field to
D4.6 (`1ba55ecd`), and `runtime-v2-check` does not run `ctidy`, so the D8
rows could not see it. Fixed in the commit after E5: every word narrower
than a pointer moved into one run at the tail of the struct, words before
bytes, with a comment that says why the run exists; no field changed its
type or name, nothing reads the struct positionally (the only `sizeof`
users allocate and zero it), the probe reads clean, and the static-shape,
lifecycle, carrier-affinity and behaviour stands are green on the new
layout.
