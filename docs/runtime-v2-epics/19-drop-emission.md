# Epic 19: Drop Emission (local owned-value reclamation) — DRAFT

**Status:** IN EXECUTION — semantics approved 2026-07-14 (scope-exit
drops; partial-path-move rejection; eval-then-suppressible-drop
reassignment order; @raii stays reserved). Task index:
`19-tasks/README.md`. Direction B from
`19-candidates.md`, approved as the next major arc (this epic is
vertical 1 of a 2-3 epic arc: local emission → crossing activation
(RV2-DEBT-034) → owner-routed frees).

## Why This Epic Exists

Compiled code frees NO owned heap value anywhere: `InstrDrop` is a
backend no-op, nothing synthesizes drops at scope exits, no drop glue
exists, and the runtime has only raw `rt_free`. Every string, array,
and heap-owning struct leaks by design. This is the largest gap
between the language's ownership STORY (affine moves, borrow checking,
@shard_movable classification — all real) and its ownership BEHAVIOR.

## Starting State (evidence, verified 2026-07-14)

- Surface EXISTS, needs no grammar: `@drop expr;`
  (`internal/parser/stmt_parser.go:319`, attr catalog
  `internal/ast/attr_catalog.go:93`) → `hir.StmtDrop` →
  `internal/mir/lower_stmt.go:317-366` (reference → `InstrEndBorrow`;
  non-copy → `InstrDrop`; copy → nothing) → backend NO-OP
  (`internal/backend/llvm/emit_instr.go:41`).
- No automatic drop synthesis exists at all (not partial — absent):
  the only implicit end-of-scope concept is the borrow checker's
  `EvDrop` lifetime event (`internal/hir/borrow.go:74`,
  `internal/sema/borrow_runtime_ops.go:295,310`) which ends BORROWS,
  not values.
- Reusable inputs: `internal/sema/move_tracking.go` (moved-out
  bindings must not drop), the borrow machinery's end-of-scope events,
  `IsCopyType` classification.
- Runtime: only `rt_free(ptr, size, align)` exists
  (`runtime/native/rt_alloc.c:57`); no per-type free helpers
  (`rt_byte_array_drop_prefix` is the lone drop-ish helper). Heap
  owners: strings, arrays (`rt_array.c`), user structs composing them.
- **Panic is `_exit(1)`** (`runtime/native/rt_io.c:167-179`): no
  unwinding, therefore NO drop-on-panic paths — a large scope
  reduction locked as a fixed point.
- No user destructors exist: drop == recursive free of heap-owning
  fields. Observability of drop TIMING is therefore only memory
  footprint (heap census) — there is no user code that can run at a
  drop point.
- Adjacent dormant machinery: the crossing drop plumbing (Epic 18,
  RV2-DEBT-034) — `__surge_drop_call` dispatch emitted, pending
  metadata, single drop site — activates in vertical 2 of this arc,
  NOT here.
- Reserved surface noted by the user (2026-07-14): `@raii`, `@arena`,
  `@shared`, `@weak` are parsed-only. This epic must RECORD `@raii`'s
  disposition (marker becomes meaningful / stays reserved / retired)
  as a design-review question; the others stay deferred.

## Fork Resolution (second opinion, 2026-07-14): Model A, strengthened

The review resolved every axis, and STRENGTHENED the A case beyond the
draft's leaning:

- **A (scope exit) is not merely easier — it keeps the arc's own gate
  deterministic.** The census (alloc/free balance) is itself a
  footprint OBSERVER: any mid-scope census read distinguishes A from
  B, so "B == A except footprint" understates it — footprint is
  exactly what the arc measures. B would break the arc's own
  enforcement mechanism.
- Two further equivalence-breakers, both pointing at A: raw
  pointers/address-of in @intrinsic code are not borrow-tracked
  (`OperandAddrOf`, `internal/mir/instr.go:364`) — under B a raw alias
  dangles after the owner's last tracked use; and correct NLL liveness
  must root in tracked borrows, which A gets free from scope lifetime.
- B stays recorded as a future optimization with its preconditions
  written NOW: raw address-of must be liveness-extending, liveness
  rooted in tracked borrows, census asserted at exit/post-scope only.
- **Drop-flag boundary (vertical 1): reject partial-path moves.** Move
  tracking is whole-binding and joins UNION the moved sets
  (`mergeMovedBindings`, `internal/sema/type_checker_walk.go:321,327`;
  `type_expr_flow.go:98`) — `if c { consume(own x); }` has no correct
  static drop point without a per-binding runtime flag. Vertical 1
  computes the symmetric difference of the join snapshots (both
  already held) and diagnoses droppable bindings in it kindly:
  "moved on some paths but not others; a droppable value needs one
  fate — move it on every branch or none." Runtime drop flags are
  deferred profile-gated; both-move / both-don't / copy-typed
  conditional moves stay valid; the early-exit merges
  (`type_checker_walk.go:313-319`) are not divergent and stay
  accepted.
- **Reassignment order: eval RHS fully → end RHS borrows → drop old
  (SUPPRESSED if the RHS moved it) → store.** `x = f(own x)` marks x
  moved during RHS evaluation, the old-value drop is suppressed by
  move tracking (the source of truth), and the store un-moves the
  binding. Never drop-then-evaluate.
- **@raii: stays reserved, re-documented, explicitly decoupled from
  drops.** Retiring would discard the natural keyword for the one
  anticipated future surface (user destructors); opt-out-leak is wrong
  (intentional leak belongs to @arena or a future explicit forget —
  an opt-out attribute would mint census-forbidden leak classes). Two
  doc changes land with this epic (`ATTRIBUTES.md`): drops are
  universal and NOT @raii-gated; @raii's future meaning narrows to
  "user-defined scope-exit destructor hook."

## Loop/Shadowing Rows (test-owned, from the review)

1. per-iteration drop: a body-local droppable made and used each
   iteration balances N alloc / N free — no accumulation;
2. drop fires on `break` AND `continue` exits of a body-local
   droppable;
3. move-in-loop of an OUTER binding is use-after-move on the second
   iteration — the while-walker must carry moved state across the
   back-edge (`type_checker_walk.go:331+`);
4. shadowing that leaves the old value live (`let s = make();
   let s = make();`) drops TWICE — the shadowed unnameable binding
   still drops;
5. shadowing that moves the old value (`let s = transform(own s);`)
   drops ONCE;
6. a droppable temporary built in a loop CONDITION drops
   per-iteration.

## The Original Fork (retained for the record)

WHEN does an owned non-copy value drop?

- **Model A — lexical scope exit.** A live (not moved-out) owned value
  drops when its binding's scope ends, on every exit edge (fallthrough,
  return, break/continue); reassignment drops the overwritten value;
  expression temporaries drop at statement end; explicit `@drop`
  becomes an early drop that marks the binding moved-from. Maps
  directly onto the borrow checker's existing end-of-scope events;
  predictable from source; the compiler work is scope-shaped, not
  dataflow-shaped.
- **Model B — eager last-use (NLL-style).** A value drops after its
  last use. Tighter memory footprint, but requires real liveness
  dataflow, interacts with borrows subtly, and — decisive given no
  destructors — is OBSERVATIONALLY EQUIVALENT to A except for
  footprint timing. B is an optimization of A, not a different
  semantic; it can land later as "drop earlier when provably unused"
  without breaking any program.
- **Leaning: A**, with B recorded as a future optimization pass. The
  fork still goes out because scope-exit semantics is language surface
  forever, and the reviewer should probe the edges (loops, shadowing,
  conditional moves — a value moved out on ONE branch only: does the
  join need a drop flag? This is the classic drop-flag question and
  the honest answer may bound vertical 1 to shapes without
  conditional moves, diagnosed kindly).

## Fixed Points

- No new surface syntax; `@drop` is the only explicit trigger and
  becomes real.
- No drop-on-panic (panic is process exit).
- Drop == recursive free; no user code runs at drop points (user
  destructors, if ever wanted, are a separate future surface — record
  with @raii's disposition).
- Moved-out values never drop (move tracking is the source of truth);
  double-drop must be structurally impossible, not runtime-checked.
- The heap census (alloc/free balance) becomes an enforced e2e gate
  for compiled programs — the arc's observable.
- Crossing activation (DEBT-034) is vertical 2, NOT this epic: local
  emission must be green first (the state structs' drop functions are
  exactly the glue this epic builds).

## Planned Slices (confirmed by the review; order deliberately unswapped)

The order separates two independent risk axes: WHERE drops happen
(slice 3: joins, early exits, loops, the partial-move rejection, the
reassignment order) is proven on trivial single frees BEFORE HOW drops
recurse (slice 4: glue depth) — the highest-risk decision is
independent of glue depth.

1. Kickoff: semantics record (resolved fork above), evidence re-pin,
   runtime free helpers design (string/array/struct-glue call shapes),
   census-gate design, the two ATTRIBUTES.md changes, Sentrux
   baselines.
2. Runtime + backend floor: per-type free helpers; `InstrDrop`
   emission for LEAF types (string, array of copyables) triggered by
   the EXISTING explicit `@drop` only; census harness rows.
3. Scope-exit synthesis on leaf types: HIR drops at scope exits,
   reassignments (the eval→suppress-aware-drop→store order), early
   exits; the partial-path-move rejection diagnostic lands HERE
   (observable only once implicit drops exist; detection piggybacks
   on the existing join snapshots); the six loop/shadowing rows.
4. Recursive glue: struct field glue, arrays of droppables, nested
   composition; census e2e at SHARDS=1/2/8 (crossing programs from
   Epics 16-18 rerun WITH the census gate).
5. Bench (alloc/free steady-state cost vs the leak model) + debt
   updates + closeout (vertical 2 — crossing activation, DEBT-034 —
   scoped as the next epic).
