# Epic 12: Crossing Surface Integration And Lowering Readiness

**Goal:** bridge Epic 11's accepted crossing language surface into the compiler,
backend, and test matrix layers without yet implementing Phase 4 cross-shard
transport. Epic 12 should prove that `far T`, `on dst { ... }`, `spawn on dst
{ ... }`, inferred crossing effects, `@shard_movable`, and `@shard_pinned`
survive the real compiler pipeline with explicit backend-unavailable behavior
and a written lowering contract.

This is the compile-time/readiness epic between language-surface work and the
runtime transport epic. It may prepare the lowering shape, add guards, harden
tests, and use the new syntax in controlled compiler/stdlib-facing fixtures. It
must not silently turn those fixtures into public runtime capability.

**Status:** proposed document, revised after expert review on 2026-07-08
(representation decision forced into Task 1, default-closed guard contract,
diagnostic negative space, backend taxonomy alignment, sharpened
`RV2-DEBT-024` criterion). Task documents are drafted in `12-tasks/`
(pending review together with this document).

## Why This Epic Exists

Epic 11 proved the language surface at lexer/parser/sema/golden level. That is
necessary but not sufficient. A valid crossing program must now have one of two
clear outcomes:

- it reaches the planned compiler/backend layer with enough preserved metadata
  to lower it once Phase 4 exists; or
- it fails with a deterministic backend-unavailable diagnostic, not a local
  fallback, panic, leak, hidden local spawn, or ambiguous parser/sema error.

Epic 12 is also the place to reconcile older "Epic 12 test/backend matrix"
debt labels with the new post-Epic-11 path. Existing debt is not lost: the first
task of this epic must decide which matrix debts block crossing readiness,
which are closed, and which move to a later dedicated test-matrix epic.

## Starting State After Epic 11

Accepted compile-time surface:

- `far T` is a hard-reserved type modifier and an affine remote-handle type
  former.
- `on dst { ... }` is an immediate placement-crossing block returning
  `TaskResult<T>`.
- `spawn on dst { ... }` is a placed child-task construct returning
  `far Task<T>`.
- `ret` is the block-local result mechanism for `on` / `spawn on`; `return`
  inside those blocks is invalid.
- `crosses` is not a keyword. The compiler infers crossing effects from `on`,
  `spawn on`, `far Task<T>.await()`, `far Task<T>.cancel()`, and direct calls to
  inferred crossing functions.
- `@shard_movable` and `@shard_pinned` are type attributes with recursive
  movement checks and capture diagnostics.

Implemented proof from Epic 11:

- parser, sema, and crossing-gate fixtures cover the accepted and rejected
  source forms;
- direct/intra-module `MayCross` inference exists in sema;
- `TcpConn` and `TcpListener` are `@shard_pinned @nosend`;
- docs are Draft 9 and no longer present the crossing surface as candidate
  syntax;
- backend execution remains unavailable by design.

Known open boundary:

- there is no cross-shard message transport, inbound queue, remote completion
  protocol, remote-free routing, remote `select`, or real `far Task<T>`
  lifecycle yet;
- backend lowering must not pretend those features exist.

## Boundary Decisions

**No Phase 4 transport in this epic.** Do not add per-shard inbound queues,
eventfd/pipe wake protocols for messages, remote-free queues, remote `select`
coordinators, cross-shard channel request/ack, remote cancellation messages, or
distributed scope completion messages. If a task needs those primitives, stop
and write the Phase 4 transport epic instead.

**No new syntax without review.** Epic 12 uses the Epic 11 surface. It must not
add keywords, attributes, shorthand, public examples, or language-level
fallbacks. If real compiler integration reveals that a valid construct cannot
work as designed, the task must stop, record the exact failing construct, and
bring it back for design review.

**No hidden local fallback.** A crossing construct must never lower to local
`spawn`, local `.await()`, local channel send, or owner-agnostic net behavior
just because transport is missing. The only acceptable pre-transport execution
result is a deterministic backend/configuration diagnostic.

**Backend-unavailable is a contract.** The diagnostic must be stable enough for
tests and user understanding. It should name the unsupported crossing surface
and the backend/configuration boundary, not an internal epic number. Known
consequence: the current guard messages in `internal/buildpipeline`
(`FUT7014`–`FUT7017`) already violate this rule by saying "the Phase 4
transport backend is unavailable". Task 2 must rewrite those messages, and any
fixture that pins the old text is expected to churn as part of that task, not
as an accident discovered later.

**Guards are default-closed.** The current guard in
`internal/buildpipeline/on_crossing_check.go` returns early unless the backend
is exactly `BackendVM` or `BackendLLVM`, which means an unknown or future
backend value silently skips the guard. Epic 12 must invert this: every
executable backend hits the crossing guard unless it is explicitly recorded as
having transport. If a guard is ever bypassed and a crossing node reaches
HIR/MIR lowering, the compiler must fail with a deterministic internal error,
never a silent drop or a local lowering. Both directions need tests.

**Diagnostics have a negative space.** Compile-only paths — LSP diagnostics,
check/format/fix drivers, and any path that does not select an executable
backend — must not report backend-unavailable on valid crossing code. Today
this holds only as an accidental property of where the guard lives; Epic 12
must state and test it as intentional behavior.

**Compile-time consumers are not public runtime examples.** This epic may add
fixtures, compiler integration tests, and internal stdlib-facing probes that
exercise the surface. It must not advertise or ship runnable examples implying
cross-shard execution works.

**Cross-platform shape first.** Compiler IR and backend contracts must remain
OS-neutral. Linux-specific mechanisms such as `epoll`, `eventfd`, `pipe`, or
`SO_REUSEPORT` belong behind runtime/backend interfaces in the future transport
epic, not in the language/lowering contract itself.

**Debt is explicit.** The old "Epic 12 test/backend matrix rewrite" owner text
in `DEBT.md` must be reconciled in Task 1. If a debt is not closed here, it must
leave the epic with a precise new owner, not a stale placeholder.

## Owned Surfaces

Epic 12 owns the compile-time and guard path for:

- AST/HIR/MIR or equivalent compiler representation needed to preserve `on`,
  `spawn on`, remote task operations, crossing destinations, result types, and
  inferred function effects through the real pipeline;
- backend-unavailable diagnostics for executable crossing forms;
- backend test matrix rows (`BackendVM`, `BackendLLVM`) that prove crossing
  forms fail in the intended layer before transport exists;
- focused fixture and unit coverage for controlled compile-time usage;
- CI wiring for stable crossing-readiness gates.

Epic 12 may inspect or touch:

- `internal/parser`, `internal/ast`, `internal/sema`, `internal/hir`,
  `internal/mir`, `internal/backend/llvm`, `internal/vm`, and related driver
  diagnostics;
- `internal/crossinggate` and golden fixture harnesses;
- `core/intrinsics.sg` and prelude declarations for `Placement`, `Task<T>`,
  `Channel<T>`, `TcpConn`, and `TcpListener`;
- stdlib files only for compile-time annotation/probe work that does not claim
  runtime crossing execution;
- test harness code when artifact races or broad VM/native/LLVM debt make the
  crossing-readiness matrix untrustworthy.

Epic 12 does not own:

- real cross-shard message queues or wake protocols;
- `far Channel<T>` runtime send/recv transport;
- `far TcpConn` read/write transport;
- remote `select`;
- remote-free routing;
- copyable `far` handles;
- `far` arrays;
- public examples that run crossing code end-to-end.

## Debt Ownership

Epic 12 must start by reviewing these debts:

- `RV2-DEBT-001`: broad focused VM/backend command is not a green gate.
- `RV2-DEBT-002`: MT liveness group budget/isolation and sync-compat lane
  concerns remain accepted test debt.
- `RV2-DEBT-011`: VM LLVM build/test artifacts can race when tests overlap.
- `RV2-DEBT-018`: rare empty-output VM harness transient likely related to
  artifact/binary lifecycle.
- `RV2-DEBT-024`: higher-order/function-type and possible cross-module
  crossing-effect propagation are postponed.

Expected treatment:

- `RV2-DEBT-011` and `RV2-DEBT-018` are likely in-scope if crossing backend
  tests need trustworthy artifact isolation.
- `RV2-DEBT-001` and `RV2-DEBT-002` are in-scope only to the extent needed for
  a stable crossing-readiness matrix; otherwise the epic must reassign them to a
  later test-matrix cleanup with a new owner name.
- `RV2-DEBT-024` is in-scope as a decision point with a testable criterion:
  cross-module or higher-order effect propagation is required now if and only
  if the chosen representation layer needs effect bits on imported function
  symbols to place the guard or preserve lowering metadata. If the guard and
  metadata work entirely from direct sema `FunctionEffects` within a module —
  the likely outcome, since the only real consumer of caller-side effects is
  Phase 4 itself — reaffirm the deferral with that evidence and do not spend a
  spike proving a foregone conclusion.

Epic 12 must not close unrelated debt just because it is nearby.

## Lowering Readiness Contract

### Representation Decision First

The single largest cost driver of this epic is a decision the current draft
must not leave implicit. Today a valid crossing program never reaches HIR: the
buildpipeline guard fires first and compilation stops, and `internal/hir` /
`internal/mir` contain no handling for `ExprOn` at all. Task 1 must therefore
record an explicit choice between:

- **(a) guard-before-HIR**: keep the guard at the pipeline boundary and
  deliver a documented, tested map from sema metadata (destination, captures,
  result types, function effects) to the future lowering point; or
- **(b) lower-into-HIR-then-guard**: introduce real HIR/MIR representation for
  `on` / `spawn on` and move the guard to the backend layer.

Option (b) pulls in affine `far T` handling, capture analysis, moveplan, and
borrow-checker work; if chosen, it requires its own task slice and the slice
plan below must be revised. Option (a) is cheaper but hands Phase 4 less. The
choice must be recorded with a rationale in the Task 1 map, and the rest of
the epic follows that choice instead of drifting between the two.

### Per-Form Contract

By epic closeout, the compiler should have a written answer for each crossing
form:

| Source form | Required preserved meaning before transport | Pre-transport backend outcome |
| --- | --- | --- |
| `on placement { ret value; }` | destination expression, captured payloads, block result type, caller resume point | deterministic backend-unavailable diagnostic |
| `on far_handle { ret op; }` | owner-anchored destination, remote operation kind, block result type | deterministic backend-unavailable diagnostic |
| `spawn on placement { ret value; }` | destination, child task body, remote task result type, `far Task<T>` handle type | deterministic backend-unavailable diagnostic |
| `far Task<T>.await()` | remote task handle consumption, result type `TaskResult<T>` | deterministic backend-unavailable diagnostic |
| `far Task<T>.cancel()` | remote task handle consumption, result type `TaskResult<nothing>` | deterministic backend-unavailable diagnostic |

The concrete shape of the answer follows the recorded representation decision:
IR nodes under option (b), or an explicit lowering record / documented map from
sema metadata under option (a). Under either option it must not be a
comment-only promise: for every row there must be a test that fails if the
listed information is lost.

## Test And Proof Contract

Every implementation task must start with tests or a proving spike plan.

Required positive compile-time proof:

- valid `far`, `on`, `spawn on`, `far Task.await`, and `far Task.cancel`
  programs survive as far as the owned compiler layer for that task;
- the compiler preserves destination and result-type information;
- function-effect metadata is still available where the backend/lowering
  contract needs it;
- direct-call inference remains stable after any representation changes.

Required negative proof:

- executable crossing code does not silently run locally, proven per form: the
  five forms in the lowering contract table plus a direct call to an inferred
  crossing function each have a test;
- unsupported backends/configurations produce the expected diagnostic code and
  message shape;
- the guard is default-closed: a backend value outside the known set still
  hits the guard, and a crossing node that bypasses the guard into HIR/MIR
  produces a deterministic internal error, not a silent drop;
- compile-only paths (LSP, check/format/fix) do not report backend-unavailable
  on valid crossing code;
- invalid syntax/semantics continue to fail at parser/sema with the Epic 11
  diagnostic, not with a later backend error;
- higher-order or cross-module crossing-effect cases are either supported with
  tests or explicitly rejected/deferred with a deterministic diagnostic or
  documented boundary.

Required matrix proof:

- targeted backend rows for crossing backend-unavailable behavior, keyed by
  the compiler's actual backend taxonomy (`BackendVM`, `BackendLLVM`; "native"
  in older debt text means the LLVM native path). Task 2 must not invent
  matrix rows that no driver flag can select;
- artifact isolation or serialization if overlapping tests would make the
  matrix flaky;
- `make golden-check` whenever golden fixtures are added or regenerated;
- `make check` before epic closeout;
- `make c-check` / `make cppcheck` only if native runtime C changes occur.

Required quality proof:

- root Sentrux scan before and after implementation;
- scoped scan for the primary affected area (`internal/` compiler paths, and
  `runtime/` only if runtime C is touched);
- `check_rules` evidence for scanned paths according to `SENTRUX_POLICY.md`;
- LOC checker remains green, and any touched over-limit legacy file records
  whether it shrank, stayed flat, or needs a follow-up owner.

## Proving Spikes Allowed

This epic may use bounded proving spikes for lowering-shape questions only.

Allowed spike examples:

- compare two IR representation shapes for `on` / `spawn on`;
- trace where backend-unavailable diagnostics should fire for `BackendVM` and
  `BackendLLVM`;
- prove whether direct sema `FunctionEffects` is sufficient for current
  lowering readiness or whether export metadata is needed now.

Spike rules:

- record hypothesis, files, proof command, success/failure criteria, and
  rollback note before code changes;
- do not keep experimental local fallback execution;
- do not introduce Phase 4 runtime primitives as a spike shortcut;
- delete or rewrite spike code before task completion.

## Planned Task Slices

Task documents are drafted in `12-tasks/` (see its `README.md` for the order
ruling and per-task gates). The slices are:

1. **Dependency and debt reconciliation map.** Map compiler/backend paths,
   current guard points, old Epic 12 debt labels, and exact test commands.
   This task also records the representation decision (guard-before-HIR vs
   lower-into-HIR-then-guard) with a rationale; every later slice follows it.
2. **Backend-unavailable diagnostic contract.** Define and test stable
   diagnostics for executable crossing forms across the real backend set,
   including the message rewrite away from internal epic numbers, the
   default-closed guard inversion, and the compile-only negative space.
3. **Representation/lowering readiness map.** Preserve crossing destination,
   result type, capture metadata, and function effects into the chosen compiler
   layer without implementing transport. If Task 1 chose option (b), split out
   a dedicated slice for HIR/MIR node introduction and its affine/capture/
   moveplan interactions before this one.
4. **Controlled compile-time usage fixtures.** Add integration fixtures or
   internal probes that use the accepted surface through the real pipeline.
5. **Test harness hardening slice.** Close or reassign artifact/matrix debts
   that block reliable crossing-readiness tests. Note the circular risk: the
   CI acceptance criterion depends on this slice, so Task 1's map should move
   it ahead of slices 2 and 4 if the debts make their matrix rows flaky.
6. **CI gate and quality closeout.** Promote stable focused gates, run
   Sentrux/LOC/checks, update debt, notes, README, and the handoff to the Phase
   4 transport epic.

Tasks may be merged, split, or reordered after Task 1's map, but the epic must
not skip the diagnostic and representation contracts.

## Acceptance Criteria

Epic 12 is complete only when:

- every executable crossing form has a deterministic pre-transport backend
  outcome;
- the representation decision is recorded with a rationale, and the compiler
  has a documented and tested path that preserves crossing meaning until the
  future lowering point;
- no crossing construct can silently execute as a local operation, proven by
  a per-form test over the five lowering-contract rows plus direct calls to
  inferred crossing functions;
- the crossing guard is default-closed for unknown backends, guard bypass into
  HIR/MIR is a deterministic internal error, and compile-only paths stay free
  of backend-unavailable diagnostics on valid code;
- old "Epic 12 test/backend matrix" debt labels have been closed, narrowed, or
  reassigned with explicit owners;
- `RV2-DEBT-024` is either partially implemented because lowering requires it,
  or reaffirmed with an exact reason it can wait;
- CI includes stable focused crossing-readiness gates;
- `NOTES.md`, `DEBT.md`, and `README.md` reflect the final state;
- public runtime examples remain absent or clearly marked unavailable until
  Phase 4 transport exists;
- the next epic can start from a concrete Phase 4 transport/lowering plan
  instead of rediscovering compiler readiness.

## Handoff To The Next Epic

The next epic after Epic 12 should be the real Phase 4 transport/lowering epic.
It should start only after Epic 12 can answer:

- what runtime message each crossing form needs;
- where the caller parks and resumes;
- where completion, cancellation, and cleanup messages route;
- what state is OS-neutral versus backend-specific;
- which compile-time metadata the backend can rely on;
- which tests fail today only because transport is intentionally unavailable.
