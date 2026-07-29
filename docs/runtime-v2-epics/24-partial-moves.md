# Epic 24 — Partial Moves (moving a field out of a live composite)

Status: designed, not started. DIRECTION SETTLED 2026-07-28 by the owner —
implement FULL partial-move tracking, no interim narrowing of the language. The
"reject instead" option this document used to weigh is retained only as
temporary scaffolding (see Phase 1), not as a destination.

This is the onboarding brief — read it end to end before touching anything.

## Why This Epic Exists

Reading a MOVE-ONLY composite field out of a container yields an ALIAS, not a
value, and the container stays usable beside it (RV2-DEBT-077):

```sg
type Inner = { x: int };
type Outer = { inner: Inner, label: int };

let o = Outer { inner = Inner { x = 1 }, label = 7 };
let mut e = o.inner;      // partial move out of a live binding
e.x = 99;
o.inner.x                 // also 99 — `e` is a second name, not a value
```

Measured 2026-07-28 on both backends, identical, and PRE-EXISTING. The `@copy`
case was fixed by Epic 23 (a Copy composite duplicates); this is the move-only
case, and it is not a representation defect at all. Moving a field out of a live
struct is a PARTIAL MOVE, and sema does not track partially-moved bindings, so
the program is neither rejected nor made independent — it silently aliases.

**Nobody writes this today.** Surveyed the whole golden corpus and all 30
showcases by matching the shape in MIR (a named non-Copy local receiving a
`field` read): 252 occurrences, and every single one is compiler-synthesized —
`__cap0` (crossing capture unpacking) and `__payload` (async state). User code:
**zero**. So this epic buys new expressiveness; it does not rescue existing
programs.

> **THAT CLAIM IS FALSE, corrected 2026-07-28 at step 1.** The survey searched
> the golden corpus and the showcases. It never searched `stdlib/`, and the
> stdlib has **20** sites — all in `stdlib/http/` (`body.sg`, `context.sg`,
> `parser.sg`, `response.sg`, `server.sg`); `core/` and the showcases really are
> clean. It also matched only a named local receiving a field read, so it missed
> `return o.field`, `let _ = o.field`, and a struct literal built from another
> value's fields — the same partial move, and the shape the HTTP request parser
> used on every request.
>
> The consequence is worse than a miscount. Two of those shapes CORRUPT MEMORY
> today rather than aliasing (RV2-DEBT-083): a by-value receiver returning a
> heap field, and a value built from a dying value's fields, both measured as
> invalid reads plus invalid frees while printing the right answer. The alias
> symptom this document opens with is the *benign* case — it is protected by
> `markAliasedBinding`, which never drops a projection-read binding. Nothing
> protects a value that ESCAPES by return or into a literal.
>
> So this epic does rescue existing programs, and step 1 was costed as "breaks
> nothing" on the strength of a survey that had not looked in the right place.

## The Sequencing Question, Answered Honestly

**Full partial moves are NOT required before Epic 23 Phase 2.** What Phase 2
requires is that the SEMANTICS be settled — and a rejection settles them just as
firmly as tracking does.

What is true is that Phase 2 makes the current silence WORSE, not better. Today
the symptom is two names for one box. With inline storage the extraction copies
the BITS, so the symptom becomes a silent bitwise duplicate of a move-only
value: two independent records, each believing it owns whatever heap the fields
hold. A double free instead of an alias.

So the ordering constraint is:

- settle the GENERATED protocol before Epic 23 Phase 2 — **required**;
- implement full user-facing tracking before Epic 23 Phase 2 — **not required
  for safety**, but it is what we are doing, by decision.

Rejection would have been the cheap settlement and is wideable later; the
reverse direction breaks code. It is NOT the chosen path — it survives in this
plan only as a temporary gate while the tracking lands (Phase 1), and is lifted
by the last step.

**But "reject user-written partial moves" does NOT settle it on its own, and
this is the hole an adversarial review found in the first draft.** The survey
above is a double-edged fact: user code writes this shape zero times, and the
COMPILER writes it 252 times. Both generated sites are real ownership protocols
that inline storage will change under them:

- crossing capture unpacking (`internal/mir/lower_expr_crossing_spawn_poll.go`)
  reads each capture out of the state struct into a synthetic local with a bare
  `RValueField` — literally the partial-move shape. Epic 23 step 7 had to bolt a
  reclamation rule onto it (drop COPY captures at the body's returns, never
  owned ones, because the body may consume those) and that rule is stated in
  comments, not in a checkable invariant.
- async save/restore (`internal/mir/async_lowering_single.go`) reads saved
  locals out of the state at resume and writes them back at suspend, with the
  same shape in both directions.

Under a heap box, a field read hands over a pointer and the protocol works by
convention. Under inline storage it copies BITS, and the convention stops being
expressible: the local and the state field become two independent records of the
same value. So the required settlement before Phase 2 is **the generated
protocol**, not the user-facing rule — and rejecting user syntax says nothing
about it.

The honest ordering is therefore:

1. specify what a field read out of a compiler-built state MEANS under inline
   storage (transfer, with the source field marked uninitialized — or copy, with
   a stated owner). **Required before Epic 23 Phase 2.**
2. track user-written partial moves. **Independent of Epic 23 Phase 2** in
   safety terms; scheduled first by decision, because the expressiveness is
   wanted and because doing it after inline storage means doing it against a
   representation that is itself new.

That fork is the first decision this document asks for, and it is recorded in
Open Questions rather than assumed.

## What Already Exists — and what only looks like it does

An earlier draft of this document claimed the borrow checker already covered
"the expensive half" and lowered the estimate on that basis. **That was wrong,
and the correction is the main thing a reader should take from this section.**

Genuinely reusable:

- `Place{Base symbols.SymbolID, Path placeKey}` (`internal/sema/borrow.go:52`) —
  a binding plus an interned field path.
- `placesOverlap(a, b)` (`:459`) — the prefix/overlap relation, so `o` overlaps
  `o.inner` while `o.left` and `o.right` do not. This is the predicate a
  path-keyed moved-set needs, and it is written and exercised.
- `resolvePlace` / `expandPlaceDescriptor` / `canonicalPlace`
  (`internal/sema/borrow_runtime.go`) already turn `o.inner.x` into such a place.
- `rejectLoopBackEdgeMoves` (`internal/sema/drop_obligations.go:293`, called from
  `type_checker_walk.go:370,393`) — the loop POLICY is already chosen and
  shipped: reject a move across a back edge rather than compute a fixpoint. So
  loops need widening, not a new dataflow.

NOT what it looks like:

- `placeState` is a **live borrow table**, not a moved-place lattice. Its value
  type is `borrowState{shared []BorrowID, mut BorrowID}` (`:82`) and entries
  expire by lexical scope in `EndScope` (`:350`). `MoveAllowed(place)` (`:332`)
  asks only "does an ACTIVE BORROW conflict with this move" — it never consults
  moved state at all.
- Moves are recorded at place granularity only as an EVENT for HIR to consume
  (`BorrowEvMove`). The DECISION state — what `checkUseAfterMove` reads — is
  `movedBindings map[symbols.SymbolID]source.Span`
  (`internal/sema/type_checker_core.go:120`), whole-binding, and `observeMove`
  only writes to it when the place has no path at all
  (`internal/sema/borrow_runtime_ops.go`, the `direct := len(desc.Segments) == 0`
  branch).

So what exists is the path representation and the overlap predicate. What does
not exist is a moved-set at path granularity, and every flow and drop operation
that reads it is symbol-shaped. That is the expensive half, and it is ahead of
us, not behind.

One small saving found while checking this: `intersectMovedBindings`
(`internal/sema/move_tracking.go:87`) is computed at
`internal/sema/type_expr_flow.go:110` and **never read** — only the union is
assigned to `tc.movedBindings`. Decide whether to delete it rather than port it.

## What Is Missing, By Layer

**1. The moved-set is symbol-keyed.** `movedBindings map[symbols.SymbolID]Span`
(`internal/sema/type_checker_core.go:120`), with ~50 references across 16 sema
files. Its helpers — `markBindingMoved`, `checkUseAfterMove`,
`snapshotMovedBindings`, `intersectMovedBindings`, `mergeMovedBindings`
(`internal/sema/move_tracking.go`) — are all symbol-shaped. They become
place-shaped, and `placesOverlap` supplies the relation they need.

**2. Use-after-move must answer per place.** With `o.inner` moved: reading
`o.label` is fine, reading `o.inner` is an error, reading `o` whole is an error.
Today the question is only "is this symbol moved".

**3. Branch joins must merge places, not symbols.** The merge/intersect helpers
exist; the subtlety they gain is that a field moved on one branch and the WHOLE
value moved on the other must join to "whole moved", which is the overlap
relation applied to the join.

**4. Loop-carried moves must be rejected.** Moving out of a field inside a loop
body moves it twice. This has no analogue in the current symbol-level tracking,
because a whole-binding move in a loop is already rejected by the use-after-move
check on the next iteration; a field move needs the same answer through the
overlap relation.

**5. Drop obligations must carry a path.** `DropLocal{SymbolID, Type, Span}`
(`internal/hir/stmt.go:130`) flows HIR → MIR. A partially-moved binding must
drop only the fields it still holds, so the obligation stops being "drop this
binding" and becomes "drop these places".

**5b. Generated ownership protocols must become checkable.** The crossing and
async paths above already perform field reads with hand-written reclamation
rules. Whatever this epic decides about a field read has to cover them, or the
compiler will keep emitting the one shape the language forbids.

**6. The backends cannot express a projected drop.** This is the part with no
existing machinery at all:

- the VM's `execInstrDrop` (`internal/vm/vm_dispatch.go:222`) IGNORES
  projections — it drops the whole local
  (`vm.execDrop(frame, instr.Drop.Place.Local)`);
- `validateDrop` (`internal/mir/validate.go`) checks the LOCAL's flags for a
  projected place, which is the wrong granularity once fields drop
  independently;
- LLVM is not closer — it is blind in a WORSE way. `placeBaseType`
  (`internal/backend/llvm/emit_helpers_place.go:305`) returns
  `unsupported projected destination` for any projection, and `emitInstrDrop`
  (`internal/backend/llvm/emit_instr.go:60`) swallows that error with a bare
  `return nil`. A projected drop on the native backend is a SILENT no-op today.
  (`emitPlacePtr` does resolve projections for other instructions, so the
  capability exists — the drop entry point just refuses to reach it.)

## Contract (frozen tests — the semantics, not the representation)

1. a field moved out is independent: mutating it is not visible through the
   container;
2. reading the moved FIELD after the move is rejected;
3. reading the WHOLE binding after a field move is rejected;
4. reading a SIBLING field after the move is accepted;
5. a partially-moved binding drops only the fields it still holds — exactly
   once each, no double free;
6. a field move inside a loop is rejected;
7. a field moved on one branch and not the other joins to moved (reading it
   after the join is rejected);
8. a field moved on one branch and the WHOLE value on the other joins to whole;
9. nested paths: moving `o.inner.deep` leaves `o.inner.other` and `o.label`
   readable;
10. a `@copy` field is unaffected — it duplicates, and the container stays whole;
11. `f().inner` — a field read from a TEMPORARY — stays accepted;
12. a field read that feeds a BORROW stays accepted.

POSITIVE controls are mandatory beside rows 2 and 3, and this is not
boilerplate: an implementation that rejects EVERY field read, or that marks the
whole binding moved at any join, passes rows 2, 3, 7 and 8 for entirely the
wrong reason. Each must be paired:

- row 4's sibling must itself be MOVE-ONLY, not an `int` — otherwise it proves
  only that primitives survive;
- `f().inner` must stay accepted (row 11);
- a borrowed field read must stay accepted (row 12);
- for rows 7-8, assert what remains USABLE on each branch, and add the opposite
  case — arm A moves `inner`, arm B moves `label` — which is what separates a
  path union from whole-binding invalidation.

Row 5 cannot be pinned by output assertions at all. The VM hides ownership
mistakes twice over — `execInstrDrop` ignores projections and frame teardown
sweeps whatever explicit drops missed — and the native backend turns a projected
drop into a silent no-op. It needs an allocation census plus valgrind on a
nested heap-owning composite: move one field, drop the residual, assert no leak
and no double free. This is Epic 23's recurring lesson, and here it applies to
the majority of the table.

Additional rows the review named as missing: reinitialization (`o.inner = v`
revives only that field), explicit `@drop o.inner`, loop-local versus
loop-external bindings, dynamic array indices, and the generated crossing/async
shapes.

## Steps

Numbered rather than phased, because every one of them lands on its own and is
gated on its own. The order is not cosmetic: the moved-set has to be
path-shaped before anything reads it that way, and the drop obligations must
not become per-field until a backend can act on one.

0. **Specify the GENERATED protocol.** State what a field read out of a
   compiler-built state struct means — crossing capture unpacking
   (`internal/mir/lower_expr_crossing_spawn_poll.go`) and async save/restore
   (`internal/mir/async_lowering_single.go`) — and make it a checkable invariant
   rather than a comment. This is the step Epic 23 Phase 2 actually depends on,
   so it goes first even though the user-facing work does not need it.
   Gate: the invariant is asserted by a test, not by prose; corpus unchanged.

   **PARTIALLY DONE 2026-07-28. The semantics are settled; the invariant is
   BLOCKED on RV2-DEBT-079, and the step turned up two defects on the way.**

   Settled by the owner — the protocol is TRANSFER:

   > Restoring a local or unpacking an owned capture performs a MoveOut from
   > the state field. The destination becomes initialized and the state field
   > becomes uninitialized. A state field may be moved out only while
   > initialized and at most once until it is explicitly reinitialized by a
   > later suspension. State cleanup drops only initialized fields.
   > Transferred fields must never be dropped by the state again. Once all
   > fields have been transferred, the envelope may be released shallowly.

   Also settled: **capture by place, not by binding** — a partially-moved
   binding cannot be captured whole; a closure, crossing or suspension may
   capture individual non-overlapping projections that are DEFINITELY
   initialized at the capture site; maybe-initialized projections are
   rejected; capturing a projection by ownership moves it out of the source.
   That lands as step 9, because "definitely initialized" is not answerable
   until the join lattice of step 4 exists. It needs no third lattice value:
   under union-at-join a maybe-moved place is in the moved set, so the capture
   rule rejects it for free.

   **What the checkable invariant cannot be.** The first formulation — "on any
   acyclic path from entry, a heap-owning state field is read at most once as
   an `RValueField` source" — is a syntactic count, not an ownership
   invariant, and an adversarial review was right to reject it. `RValueField`
   carries no transfer mode and its object operand is always `OperandCopy`, so
   the same MIR shape means "duplicate" or "MoveOut" purely by convention. The
   invariant needs an explicit `FieldReadMoveOut` versus `FieldReadCopy` mode
   first (NOT expressed by moving the object operand, which would move the
   whole state). Path enumeration is also the wrong tool: use a forward
   worklist over the existing successor walk (`reachableBlocksFrom`,
   `internal/mir/async_lowering_locals.go:128`) with per-field
   initialized/moved bits and union at joins.

   **Why it is blocked — and the answer, found the same day.** RV2-DEBT-079
   asked what reclaims an owned capture, since the body plainly does not and
   adding a body-side drop double-frees. Valgrind's free'd-at stack names it:
   the CALLER's own scope-exit drop of the captured binding.

   That drop exists because **a crossing capture is not a move for the
   caller** — no site calls `observeMove` on an `on`/`spawn on` capture, so
   the binding stays live and drops at scope end. Which means the ownership
   rule the crossing surface advertises is not enforced at all, and all three
   of these compile with no diagnostic (RV2-DEBT-081, measured):

   - reading the binding after the crossing;
   - MUTATING it — `j.id = 99` after `spawn on distributed { ret j.id * 100 + 6; }`
     gives caller 99 and body 406, two answers for one value;
   - MOVING IT AGAIN — `consume(own j)` after the capture, so one owned value
     moves twice.

   Replacing a captured heap field in the caller abandons the old block (219
   bytes, measured), because the shipped state still references it. No
   use-after-free was produced: the affine must-await rule keeps the await
   inside the capture's scope, which orders the caller's drop after the body's
   last read, and deferring the await past that scope is rejected by the
   must-await-or-return check. **The current safety rests on a lifecycle rule,
   not on ownership.**

   This is the same defect class this epic exists to fix — an ownership rule
   sema does not track — and it decides the generated protocol rather than
   being decided by it.

0b. **Make the capture a real move.** Both ledger rows closed together, because
   the ownership had to move as one piece: `checkOnCaptures` now calls
   `observeMove` for a `CrossingCaptureMoveOwned` capture, and
   `registerCrossingBodyOwnership` gives the crossing body the drop obligation
   the caller just gave up. Either half alone is broken — the caller's drop
   without the body's is the old hole, the body's without the caller's is the
   double free that stopped the first attempt.

   Rejected now, each with a positive control beside it: reading the binding
   after the capture, moving it a second time, and capturing one owned value
   into TWO crossings. Still accepted: a Copy capture (the caller keeps its
   binding by definition) and an owned capture the caller never touches again.

   Reclamation proven by valgrind across eight shapes — unconsumed capture,
   consumed capture, mixed-branch, body-local live at `ret`, loop and non-loop,
   the immediate `on` form, and a CANCELLED far task whose body never runs.
   Zero definitely-lost and zero invalid accesses in all eight.

   Far-handle captures are deliberately untouched: the anchored form USES the
   destination handle inside the body it was captured for, so consuming it
   would reject every anchored channel operation. Review was right that this is
   a workaround for symbol-granular move tracking rather than an ownership
   model — the handle currently has two owners or a lease, and nothing says
   which. Filed as RV2-DEBT-082, and it gets cheaper once move tracking is
   place-granular.

   **Residual, and it is not crossing-specific:** a field WRITE after a move is
   still unchecked. `j.id = 99` after the capture is accepted, exactly as
   `consume(own j); j.note = "z";` is accepted with no crossing in sight — the
   moved-set is symbol-keyed and the write goes through a projection. Steps 3
   and 5 close it, and `TestCrossingCaptureFieldWriteIsNotYetCaught` is the
   tripwire that fails when they do.

   **What this unblocks:** the transfer invariant now has an owner to name.
   Writing it is still the step-0 tail, and still wants the explicit
   `FieldReadMoveOut` versus `FieldReadCopy` mode first.

   **Two defects found and FIXED here, both pre-existing and unrelated to
   partial moves** (`ret` discharged no ownership obligations at all):

   - `dropObligationsSuppressed` (`internal/sema/drop_obligations.go`) switched
     off every drop recording inside a crossing body, on the authority of
     RV2-DEBT-034 — a row that CLOSED at the Epic 20 closeout. A stale guard.
   - `hir.RetData` carried no drop list and MIR's `StmtRet` never called
     `emitExitDrops`. `StmtReturn` had both; `ret` had neither.

   Together: anything a crossing body allocated and still held at its `ret` was
   abandoned — measured at 16 blocks over 8 crossings (a bound local plus a
   comparison temporary), valgrind, native backend. Fixed by lifting the stale
   guard for crossings, giving the crossing body a `functionRoot` drop scope so
   its exits stop at the boundary rather than at the enclosing function, and
   plumbing `DropsAfterValue` through `RetData` with the same
   `detachFromExitDrops` contract `return` honours. Pinned by
   `TestRuntimeV2CrossingRetDischargesBodyDrops` (red before, green after) with
   `TestRuntimeV2CrossingRetDropsDoNotStealMovedValues` as the must-still-work
   control. Blocking bodies KEEP the suppression, and that is a PARKED
   question rather than a proven boundary — their release path is a separate
   shallow free on the pool side, and they lose a constant 219 bytes
   independent of iteration count, which is a different mechanism and is
   unprobed (RV2-DEBT-080).

   **A measurement trap worth carrying forward.** The first version of the
   test used a 6-character string, passed, and proved nothing — a short string
   is stored INLINE and owns no block, so there is nothing to lose. The second
   version lengthened the string AND moved a comparison from a global constant
   to an in-body call, which introduced the allocation that actually leaked;
   attributing that block to the capture was wrong. A census is only
   trustworthy paired with the leaked block's allocation stack.

1. **Temporary gate: refuse the bare form, and refuse `own` too, for now.** In
   `observeMove`, refuse a move whose resolved place has a non-empty path from a
   named base and whose moved type is non-Copy — with a diagnostic that names
   `own o.inner` as the intended spelling once tracking lands.

   Both spellings are refused at this step, which is the point: the explicit
   form is the DESTINATION, and letting it through before the tracking exists
   would ship exactly the unsound state this gate is here to prevent.

   This is SCAFFOLDING, not the destination — step 8 removes it. Its purpose is
   that steps 2-7 land incrementally, and a half-migrated tracker is worse than
   either end state: a place-aware moved-set with symbol-granular drops will
   drop a field that moved. The rejection makes that window unreachable from
   user code. It costs one sitting and breaks nothing (measured: zero
   user-written partial moves in the corpus).
   Gate: the diagnostic fires on `let e = o.inner`; `f().inner`, a borrowed
   field read, and a `@copy` field all stay accepted.

   **DONE 2026-07-28, and it cost more than one sitting.** `SEM3143`, raised in
   `observeMove` for any move whose resolved place has a non-empty path from a
   named base. The segments are read BEFORE `expandPlaceDescriptor`, because
   expansion rewrites a place through the borrow its base came from and
   manufactures segments the source never wrote.

   "Breaks nothing" was wrong — see the correction above. The gate rejected 20
   stdlib sites and two sema fixtures on the first run, and the shapes it
   rejected were corrupting memory (RV2-DEBT-083), so the rejection is doing
   real work rather than merely deferring expressiveness.

   Those sites were rewritten in two ways, and the split matters because only
   one of them is throwaway:
   - **borrow at the use site** where the container outlives the read
     (`let raw = reader.raw;` → use `reader.raw` directly, since every consumer
     already takes `&byte[]`). Permanent improvement, no cost, no revert.
   - **copy explicitly** where the container dies immediately — the genuine
     partial move. `clone()` covers a `string` but there is no `__clone` for a
     composite, an array or a union, so `stdlib/http/interim_copy.sg` spells out
     the six needed copies. That file exists to be DELETED at step 8, and every
     caller then goes back to reading the field directly.

   Gated shapes, verified: a bound field read, `own o.field`, a nested path
   `o.a.b`, a by-value receiver returning a heap field, a struct literal built
   from a dying value, a discarded read, and — after review found it missing —
   a TUPLE element, which `resolvePlace` had no case for at all. Adding it
   closes a hole in the borrow table too, not only in this gate.

   **Not gated, and the boundary is the same one in every case: `resolvePlace`
   cannot resolve a CALL as a place.** So any projection whose base is a call
   escapes — `f().inner`, `make_array()[i]`, `obj.method().field`. The first of
   those segfaults (RV2-DEBT-084), which makes the epic's "must stay accepted"
   for `f().inner` mean "accepts a use-after-free" today. The others are
   unverified and may be safe when the call returns a borrow; the gate cannot
   tell the two apart, because it cannot see through the call at all.

   Also outside the gate: a projection of a `const`. `resolvePlace` accepts only
   `SymbolLet` and `SymbolParam`. Module-level `let` is banned and a `const` is
   restricted to compile-time numbers and strings, so a move-only composite
   should not be reachable there — but that is an argument, not a measurement.

2. **Moved-set becomes place-keyed.** `movedBindings` goes from
   `map[SymbolID]Span` to a place-keyed set, using `Place{Base, Path}` and
   `placesOverlap`. Every helper in `internal/sema/move_tracking.go` follows.
   NO behavior change: whole-binding moves must answer exactly as today.
   Gate: full corpus identical, and `intersectMovedBindings` either ported or
   deleted (it is currently computed and never read).

   **DONE 2026-07-29.** `movedPlaces map[Place]source.Span`, renamed rather than
   retyped so the compiler forced every call site to be revisited. The reference
   count was 30 across 7 files, not the ~50 across 16 this document estimated.

   Membership stays EXACT, not overlap, and that is the whole discipline of this
   step: answering by overlap here would quietly implement part of step 3 while
   the gate is "the corpus is identical" — precisely the change such a gate
   cannot see. `placeMoved` says so at the point where step 3 will widen it.

   `placesOverlap` became a free function reading only the interned key. It used
   to decode each key back through the `BorrowTable` that interned it, so the
   answer depended on WHICH table was asked — and the moved-set outlives any one
   query. The prefix test is exact because `internPath` terminates every segment
   with `;`, so `f:1;` cannot prefix `f:12;`.

   Both iteration sites fold to one entry per base before applying their
   symbol-shaped filters, and `rejectLoopBackEdgeMoves` picks the earliest span
   of the folded set: map order is random, so an arbitrary pick would make a
   diagnostic's location vary between identical compiles once several places
   share a base.

   `intersectMovedBindings` deleted. The union is the right join — a use must be
   rejected if ANY reachable path moved the value — and "moved on every path" is
   not that condition.

   Evidence: corpus diagnostics byte-identical (22,980 lines over `stdlib`,
   `core`, `showcases`, `testdata/llvm_parity`), and MIR byte-identical for all
   **76** buildable corpus programs. The MIR half is the one that matters:
   diagnostics cannot see a changed drop obligation, and review was right that
   the six-program sample this step started with was smoke coverage rather than
   a migration proof.

   Carried into step 3, deliberately not fixed here: `oneSidedDroppables` still
   asks `moved[wholePlace(base)]` and returns a symbol-shaped obligation list.
   Correct under the gate, not future-ready.

3. **Use-after-move answers per place.** With `o.inner` moved: `o.label` reads,
   `o.inner` and `o` do not. Still no partial moves reachable — step 1's gate
   is still up — so this is verified by unit-level probes on the analysis, not
   by programs.

   **DONE 2026-07-29.** The decision lives in `movedPlaceCovering`: find a moved
   place that OVERLAPS the place being read. Overlap answers both directions at
   once — a container is no longer readable whole once part of it has gone, a
   sibling is untouched — so the four contract rows fall out of one relation
   rather than four rules.

   The structural change is that a projection's BASE CHAIN stopped being a value
   read. `o` in `o.label` is walked to reach a place; only the projection is
   read. Without that, typing `o.label` would type `o` as an identifier and
   reject it for a move of `o.inner`. `placeBaseDepth` marks the base chain,
   `typeExprAsPlaceBase` wraps only the TARGET of member/index/tuple-index/deref
   typing — `o.field[idx]` must still check `idx`, an ordinary value read that
   happens to sit inside a projection — and the outermost projection asks once.

   Method calls are unaffected and stay on the binding-level wording: the call
   path types its receiver through `memberReceiverType`, not through member
   typing, so the receiver is a value read as before. A bare method VALUE
   (`let m = o.method;`) does not exist in the language, which is what would
   otherwise reach place-shaped reporting for a non-field.

   The reported move is a function of the program, not of map order: exact match
   first, then a TOTAL order — span start, span end, path length, path key.
   Spans do tie, so "usually stable" is not enough.

   Evidence: corpus diagnostics and MIR both byte-identical across all 76
   buildable corpus programs — which proves only that reachable whole-binding
   behaviour did not move, since the gate makes the new states unreachable. The
   feature evidence is the unit tests, and the two behaviour tests carry
   negative controls that were RUN: reading a field of a wholly moved binding
   fails without the projection check, and a moved value used as an index
   operand fails if the base-chain suppression is wrongly extended to it.

   Carried forward: `checkPlaceUseAfterMove` expands through borrows exactly as
   `observeMove` does before recording, so a read through a borrowed alias asks
   about the place the move wrote. That symmetry is untested end to end for the
   same reason as everything else in this step.

4. **Branch joins and loops go place-aware.** Union at joins, and the case that
   distinguishes a real implementation from a conservative one: a field moved on
   one branch and the WHOLE value on the other joins to whole. Loops reuse the
   shipped policy (`rejectLoopBackEdgeMoves` — reject on the back edge, no
   fixpoint), widened to paths.

   **DONE 2026-07-29.** The union was already place-keyed after step 2, so the
   work was making the joined set CANONICAL. The moved-set is now an ANTICHAIN:
   no entry covers another. `insertMovedPlace` skips a place an existing entry
   already covers and deletes the entries a new place covers; `markPlaceMoved`
   and `mergeMovedPlaces` both go through it. Alongside the symmetric
   `placesOverlap` there is now a directed `placeCovers`, which is the relation
   "makes redundant".

   Row 8 is why. One arm moving `o.inner` and the other moving `o` whole unions
   to `{o.inner, o}`, and the answer the language wants is that `o` went.
   Collapsing at insert makes the join say that once instead of leaving every
   reader to re-derive it, and makes the set independent of the order the moves
   were seen — which a join has no right to depend on.

   Row 7's opposite case is the one a conservative implementation fails, and it
   is tested with a negative control that was RUN: arm A moves `inner`, arm B
   moves `label`, and a third field survives both. Forcing the insert to
   collapse to the whole binding makes exactly those sibling-survival
   assertions fail.

   The loop policy keeps the place rather than only the span, so a field move
   inside a loop can name `o.inner` instead of just `o`, and skips a place
   COVERED by the pre-loop snapshot rather than one merely equal to an entry in
   it — the set collapses, so the entry seen after the body may be wider than
   the one recorded before it.

   Two tests written in steps 2 and 3 had to be rewritten: they asserted
   recording `o.inner` beside an already-moved `o`, which the antichain makes
   unreachable. They were pinning an artifact, not the invariant.

   Evidence: corpus diagnostics and MIR byte-identical across all 76 buildable
   corpus programs — again proving only that reachable whole-binding behaviour
   held still, since the gate keeps the new states out of reach. The join
   behaviour is pinned by unit tests on the relation.

   **What the antichain IS, and what it is not.** It is the read-rejection
   projection: a may-moved set, where every later read covered by any entry is
   invalid. That is exactly the question steps 3 and 4 answer, and collapsing is
   sound for it.

   It is NOT the complete ownership state, and steps 5 and 6 cannot be built
   from it alone. After `{o}` and `o.inner = v`, the state means "`o.inner` is
   initialized, everything else under `o` is still gone" — and an antichain has
   no way to say that without either expanding `o` through its type layout or
   carrying a separate reinitialized-exception set. Step 6 has the same problem
   from the other side: a joined antichain cannot describe which fields a
   partially-moved binding still holds, though each arm's own set can, before
   the join.

   So collapsing does not make step 5 harder — reviving `o.inner` out of `{o}`
   needs that expansion whether or not the field was ever recorded separately,
   since nothing marked `o.inner` in that scenario to begin with — but step 5
   must decide the representation, not inherit this one.

   Cost note: `insertMovedPlace` scans the set twice, so a merge is quadratic in
   the number of moved places. Irrelevant at present sizes and compile-time
   only; if the antichain outlives this epic, index by base and use a prefix
   structure rather than a flat scan.

5. **Reinitialization.** `o.inner = v` revives that field and nothing else;
   `handleAssignment` currently clears moved state for whole bindings only.
   Decide and implement the same question for `@drop o.inner`.

6. **Drop obligations carry paths.** `DropLocal{SymbolID, Type, Span}` gains a
   path, HIR → MIR. A partially-moved binding drops the fields it still holds.
   Must NOT land before step 7 — a per-field obligation with a projection-blind
   backend drops the wrong thing.

7. **Projected `InstrDrop` in the backends.** VM (`execInstrDrop` currently
   ignores projections), `validateDrop` (currently checks the LOCAL's flags),
   and LLVM (`placeBaseType` refuses projections and `emitInstrDrop` swallows
   the error — a SILENT no-op today). All three together.
   Gate: a projected drop frees the field and only the field, proven by
   allocation census plus valgrind, on both backends.

8. **Lift the step-1 rejection and land the contract.** Partial moves become
   legal, rows 1-12 land as the frozen set with their positive controls.
   Three cleanups are part of this step, not follow-ups, because each is
   interim cost that becomes permanent if forgotten:
   - DELETE `stdlib/http/interim_copy.sg` and restore the direct field reads at
     its callers (`context.sg`, `parser.sg`, `response.sg`, `server.sg`). Those
     copies are per-request allocations on the HTTP path that exist only
     because `own o.field` did not.
   - delete the `SemaPartialMoveUnsupported` gate and its diagnostic code;
   - delete the two debt sentinels (`TestPartialMoveOutOfTemporaryIsNotGated`,
     `TestCrossingCaptureFieldWriteIsNotYetCaught`) rather than adjusting them —
     each asserts behaviour that is wrong.

9. **Monomorphization and generated-path audit.** Any new ownership metadata
   must survive `internal/mono` cloning and substitution, and the step-0
   protocol must still hold once fields drop independently.

## Traps To Know Before Writing Code

1. **Do not widen the moved-set and the drop obligations in separate steps.** A
   place-aware moved-set with symbol-granular drops will drop a moved field; a
   path-carrying obligation with a projection-blind VM will drop the whole
   binding. Either is a double free.
2. **The VM forgives what the native backend does not**, and did so twice in
   Epic 23: frame teardown sweeps what explicit drops missed. A leak that only
   the native backend shows is the normal case, not an anomaly.
3. **`f().inner` must stay legal.** The base is a temporary the lowering itself
   materialized and is spending; rejecting it would be a regression. Epic 23's
   owning-temp marking is the existing answer.
4. **A negative control must run in BOTH directions.** Epic 23 hit this three
   times: reverting a fix proves the fix does something, not that it does the
   right thing. Rows 4 and 10 are the "must still be accepted" side.
5. **Compare-arm bindings are already a partial-move-shaped path** and interact
   with RV2-DEBT-052/075/078. A payload binding extracted from a union is
   morally a field move; whatever this epic decides has to agree with what the
   compare release model decides, or the two will drift.

## Cost Note

The first draft claimed this was SMALLER than Epic 23 Phase 1, on the strength
of the borrow checker's place machinery. Review corrected that, and the
correction is worth keeping visible:

- **Reject-only (Phase 1)**: clearly smaller than Epic 23 Phase 1. A syntactic
  refusal in `observeMove`, plus fixtures.
- **Full tracking (Phase 2 of this epic)**: comparable to Epic 23 Phase 1, or
  larger. Every symbol-granular operation becomes path-granular — the moved-set,
  use-after-move, branch joins, reinitialization, scope/early/arm/reassignment
  drops, HIR `DropLocal`, MIR validation, the VM's projected drop, native drop
  glue, the generated crossing and async protocols, and monomorphization of any
  new ownership metadata. Epic 23 had more clone mechanics; this has far more
  flow and drop STATE.
- **Doing it concurrently with Epic 23 Phase 2**: a multiplier, not a sum.
  Inline storage needs per-field initialized/drop bits, which is the same state
  this epic introduces — attractive to merge, and exactly the kind of merge that
  makes both undebuggable.

What genuinely saves work: the path representation, the overlap predicate, and
the already-chosen loop policy (reject on the back edge, no fixpoint).

## Settled Decisions (owner, 2026-07-28 — do not relitigate)

These four had to be answered before step 2, because step 2 changes the shape of
the moved-set and every one of them changes what that set must hold. All four
are now decided, and the reasoning is kept because it is what a later reader
will want when the answer looks arbitrary.

1. **EXPLICIT.** `let e = own o.inner` is the partial move; the bare
   `let e = o.inner` is not one and stays an error (its diagnostic redirects to
   the `own` form). Rationale: The language already has `own` as a
   move-intent marker at crossings and call sites, so an explicit form is
   consistent rather than novel; it makes the destructive read visible at the
   use site, which matters more here than in Rust because Surge has no
   `#[derive(Clone)]` escape hatch; and it makes step 1's rejection a REDIRECT
   ("write `own o.inner`") instead of a refusal. Cost is the same either way —
   the tracking is identical, only the trigger differs.

2. **REINITIALIZATION REVIVES.** After `o.inner` moves, `o.inner = v` makes
   `o` whole again and readable as a whole. Rationale: Rust does this, it falls out of a place-keyed set
   (remove the place and any prefix that was only invalidated by it), and the
   alternative — a binding that can never be made whole — is surprising.

3. **CONSTANT ARRAY INDEX YES, COMPUTED INDEX REJECTED.**
   `let e = own arr[0]` is a partial move; `let e = own arr[i]` is an error.
   Rationale: This is
   Rust's answer and the reason is the same: a computed index cannot be tracked
   statically, so accepting it would need a runtime drop flag per element —
   real cost, and Surge has no such machinery. NOTE this is the one question
   whose answer changes the DATA: sema's canonical path already collapses an
   index to a generic `i:;` segment (`internal/sema/borrow.go`), so a
   constant-index partial move needs the index VALUE in the path, which the
   borrow checker currently discards.

4. **`@drop o.inner` BECOMES LEGAL.** Explicit drop of a projected place is
   rejected today (`handleDrop` requires a whole symbol); with partial moves it
   falls out of the same machinery — an explicit drop of a place is a move into
   nothing. Cheap once places are tracked.

## Open Questions (non-blocking)

- **Is the survey reproducible?** The "zero user-written partial moves" claim
  rests on matching a MIR shape and reading synthesized names (`__cap0`,
  `__payload`), which does not formally prove source provenance. Check the
  survey script in as a gate rather than repeating it by hand.
- Does a partially-moved binding remain capturable by a crossing or a closure,
  or is that rejected until it is whole?

## Out Of Scope

- Anything about `@copy` composites: they duplicate, Epic 23 settled it.
- The compare-scrutinee leak (RV2-DEBT-078) — adjacent and separately filed.
- Epic 23 Phase 2's inline representation. This epic changes what a partial move
  MEANS; that one changes where a composite lives.

## Related Ledger Rows

`docs/runtime-v2-epics/DEBT.md`: RV2-DEBT-077 (this epic closes it). Adjacent:
RV2-DEBT-052, RV2-DEBT-075, RV2-DEBT-078 — all in the compare-arm binding path,
which is partial-move-shaped. RV2-DEBT-079 — opened by step 0; crossing capture
ownership across the transport edge, and the blocker on stating the generated
protocol as a checkable invariant.
