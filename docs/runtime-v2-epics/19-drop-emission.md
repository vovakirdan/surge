# Epic 19: Drop Emission (local owned-value reclamation) — DRAFT

**Status:** draft (2026-07-14); the semantic fork goes to second
opinion before the boundary decisions freeze. Direction B from
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

## The Central Fork (out for second opinion)

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

## Candidate Slices (to be re-cut after the fork resolves)

1. Kickoff: semantics record (the resolved fork + drop-flag boundary +
   @raii disposition), evidence re-pin, runtime free helpers design
   (string/array/struct-glue call shapes), census-gate design,
   Sentrux baselines.
2. Runtime + backend floor: per-type free helpers; `InstrDrop` emission
   for LEAF types (string, array of copyables) triggered by the
   EXISTING explicit `@drop` only — the smallest honest increment that
   makes today's surface real; census harness rows.
3. Scope-exit synthesis: HIR inserts drops at scope exits,
   reassignments, early exits for leaf types; move-out interaction
   rows; loop/shadowing rows.
4. Recursive glue: struct field glue, arrays of droppables, nested
   composition; census e2e at SHARDS=1/2/8 (crossing programs from
   Epics 16-18 rerun WITH the census gate).
5. Bench (alloc/free steady-state cost vs the leak model) + debt
   updates + closeout (vertical 2 scoped as the next epic).
