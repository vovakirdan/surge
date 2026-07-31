# Epic 23 — Value Composites (inline representation, correct copy/move/drop)

Status: PHASE 1 COMPLETE (steps 0-8). Phase 2 — inline representation — is
UNPARKED as of 2026-07-29: Epic 24 (partial moves) landed. ONE preflight item remains: the
places/references and frame-slot storage-model document, which does not exist
yet and which the Phase 2 scope section below requires. The other — Epic 24's
step-0 tail — is DONE as of 2026-07-31: the crossing capture unpack declares its
transfer mode, the async state-envelope protocol is asserted at the MIR level in
`internal/crossinggate`, and the single-suspend lowering that the step's text
attributed three of its four sites to turned out to have no caller and is gone. See the detour chain in `README.md`.
This is the onboarding brief — read it end to end before touching anything.

Revision note (2026-07-27, after two rounds of adversarial design review).
Round 1: the first draft claimed `placeOperand` was already a sufficient
decision site and that Phase 1 was purely additive. Both were wrong, and the
review found a third hole that would have shipped a cross-shard double-free.
Round 2, against the rewrite: four more corrections, all folded in below —
by-value Copy-composite PARAMETERS would have leaked their clone
(`paramTransfersOwnership`), the step ordering was circular (the preferred
crossing policy cannot precede the clone that implements it), the MIR drop
validator has no route to sema's ownership data, and the extraction repair
needs a CLONE where the draft prescribed a retain. The diagnosis in "Why This
Epic Exists" and contract tests 1-7 have survived both rounds unchanged.

## Why This Epic Exists

A struct, tuple, fixed array or tagged union is a VALUE. The language says so
(`docs/ABI_LAYOUT.md`: composites are inline fields with offsets and alignment;
only `string`, dynamic `Array<T>`, `Map`, `Range`, `Task`, `Channel` and opaque
runtime resources are handle-backed). The IMPLEMENTATION does not: every
composite is a heap box referenced by a pointer/handle, on BOTH backends
(`internal/backend/llvm/emit_literals.go:24` `rt_alloc` per struct literal;
`internal/vm/heap.go:228` `AllocStruct` → an `OKStruct` Object with `Fields
[]Value`). A composite value IS a shared pointer.

That representation is wrong in two ways at once, both measured 2026-07-27 and
both PRE-EXISTING (reproduced unchanged at `9b849310`, before Epic 22):

| defect | repro | VM | LLVM |
| --- | --- | --- | --- |
| **aliasing** — `let mut q = p; q.a = 99` mutates `p` too | copy-then-mutate | prints 99 (BUG) | prints 99 (BUG) |
| **leak** — a `@copy` composite is never reclaimed | build/drop in a loop, NO copy | net-live 13/16 (BUG) | 256 B / 16 blocks (BUG) |
| move-only composite (baseline) | build/move in a loop | — | clean |

**These are two distinct defects with one root.** The leak repro performs no
copy, so the leak is a DROP-PATH bug: a `@copy` composite is Copy, so
`ownsHeap` is false (`internal/sema/ownership_axes.go:44`), so nothing releases
its box. The aliasing is a COPY bug: copying a Copy composite duplicates the
pointer, not the value. Both stem from "a Copy composite is treated as a
non-owning Copy scalar." Neither can be fixed alone: make composites droppable
while copy still aliases and you double-free; make copy independent without
making them droppable and they still leak. They land together, behind one
boundary.

`@copy` is the wrong axis for the representation. It governs DUPLICABILITY, not
storage:

- `type File = { fd: int }` — a value, move-only.
- `@copy type Point = { x: int }` — a value, Copy.

Both are physically values. `@copy` only makes a type Copy-capable when its
members are Copy. Representation must follow the value-vs-handle CATEGORY, not
`@copy`.

**A third symptom, not measured but structural.** The aliasing defect does not
stop at a shard boundary. `TriviallyTransportableBits`
(`internal/sema/ownership_axes.go:102`) returns `IsCopyType(id)` for a `@copy`
composite that contains no refcounted scalar, so such a composite crosses today
as raw bits — i.e. the box POINTER is handed to another shard, and two shards
mutate one box. Today that is "only" cross-shard aliasing, because nobody frees
the box. It becomes a cross-shard double-free the moment Phase 1 makes the box
droppable. See "The Phase 1 crossing gate" — this is a blocker, not a test gap.

## The Decision (settled)

**Target: inline value composites.** A struct/tuple/fixed-array/union lives
inline (frame slot on the VM, stack/register with a by-value ABI on LLVM), not
in a heap box. Handle-backed types (`string`, dynamic array, map, range, task,
channel) are unchanged. Copy duplicates the value, move transfers it, borrow
references its place, drop reclaims its owned parts.

**This is reached behind a permanent type-directed copy/move/drop boundary, and
only the STORAGE implementation behind it is ever thrown away.** The boundary,
the MIR integration, the ownership rules and the contract tests are permanent.
Phase 1 puts a temporary BOXED implementation behind the boundary to restore
correctness now; Phase 2 replaces that one implementation with the inline one.

**Do NOT invent a `clone` operation in the LANGUAGE or in HIR.** `clone` is not
a new Surge surface: no `clone` keyword, no `clone.type<T>` intrinsic, no HIR
node, no user-visible method. A copy stays a copy in the source and in HIR.

**This prohibition does NOT extend to MIR operand kinds, and the first draft
was wrong to imply it did.** MIR must distinguish a borrowing read from a
consuming copy, because today it cannot — see below. Adding that distinction is
the same move Epic 22 already made when it introduced `OperandRetain`: a
lowering-level statement of intent, invisible to the language. What must never
appear is a type-parametric clone NODE that a temporary representation defines
and the inline representation would delete.

## The Boundary (what is permanent)

### The problem the first draft missed: `consume` is erased

`placeOperand` (`internal/mir/lower_expr_helpers.go:81`) is the decision site,
but it does not currently PRESERVE its decision for a Copy composite:

```go
kind := OperandCopy
switch {
case consume && l.isRefCountedScalar(ty): kind = OperandRetain
case consume && !l.isCopyType(ty):        kind = OperandMove
}
```

A `@copy` composite read for consumption (`consume=true`) and the same
composite read for borrowing (`consume=false`) both produce `OperandCopy`. The
backend sees one operand kind and cannot tell them apart. So "make the backend
clone on `OperandCopy`" — the first draft's Phase 1 — would clone on every
borrowing read as well: an allocation per field read, and, worse, a semantic
change, because a borrowing read that clones no longer observes later mutation
through the original place.

**Resolution (settled — see Settled Question 1).** MIR gains a new operand
kind, `OperandCopyValue`, emitted by `placeOperand` when `consume &&
isValueComposite(ty)`. `OperandCopy` keeps meaning what it means today: read
the bare word, own nothing. A flag on `Operand` was considered and rejected —
a new KIND is enumerable by grep, a flag is not, and a consumer that ignores a
flag still compiles.

The decision site does not move. What changes is that its answer survives to
the backend.

**One new operand kind is enough for MIR, and NOT enough for LLVM.** `Operand`
already carries `Type` (`internal/mir/instr.go:427`), so the kind states the
intent and the type states what to copy — MIR needs nothing further. The LLVM
backend additionally needs generated, type-directed CLONE GLUE, one function
per concrete composite, exactly parallel to the drop glue that already exists
in `emit_drop_glue.go`. That glue is a backend emission surface, not an IR
node, so it does not cross the prohibition above — but it must be specified
like the drop glue is: emission point, naming, caching per type, and the
ownership convention of the returned value (the caller owns it).

**`isValueComposite(T)` is step 1's first commit**, because every rule
below keys off it. The intended answer: struct, tuple, tagged union, and FIXED
array are value composites; aliases resolve first; a dynamic `Array<T>`, `Map`,
`string`, `Range`, `Task` and `Channel` are handle-backed and are NOT value
composites regardless of what they contain; `&T`/`*T` are never value
composites. Write it as one predicate in one place and have both backends and
sema call it — three structural walks that disagree is how `ownsHeap`,
`typeOwnsHeap` and `IsCopy` drifted apart in the first place.

**Every switch that enumerates operand kinds must learn the new one.** These
are exhaustive today and will silently mis-classify a new kind rather than fail.
Two lists, and the SECOND is the one that bites: the sites that already handle
`OperandRetain` are found by grepping for it, while the sites that classify on
`Copy`-or-`Move` alone predate that kind entirely and no grep for it reveals
them. They matter more, not less, because `OperandCopyValue` appears exactly
where `OperandCopy` used to — a far more common position than `OperandRetain`
ever occupied.

Found by grepping `OperandRetain`:
`internal/mir/async_lowering_locals.go:366`, `internal/mir/async_liveness.go:206`,
`internal/mir/validate.go:256`, `internal/mir/recognize_switch.go:185`,
`internal/mir/lower_stmt.go:487` (return detachment),
`internal/mir/print.go:322`, `internal/vm/eval.go:138`,
`internal/vm/trace.go:385`, `internal/backend/llvm/emit_term.go:115,157`,
`internal/backend/llvm/emit_helpers.go:82`.

Found only by grepping `OperandCopy` and reading each hit — these classify on
`Copy`-or-`Move` and would have dropped a `CopyValue` operand on the floor:
`internal/mir/recognize_switch.go:166` (`isOperandForLocal` — a CopyValue
operand does reference the local), `internal/backend/llvm/emit_helpers.go:100`,
`internal/backend/llvm/emit_async_helpers.go:25`,
`internal/backend/llvm/emit_intrinsics_time.go:100`,
`internal/backend/llvm/emit_calls_intrinsics.go:69`.

Producers that construct `Operand{Kind: OperandCopy}` directly need no change to
introduce the kind, but one of them is a PRODUCER DECISION that belongs on the
step 4/5 list: `operandForLocal` (`internal/mir/async_lowering_locals.go:386`)
picks Copy-vs-Move off `LocalFlagCopy` when saving and restoring async state, so
a Copy composite crossing a suspend point is still a bit-copy of the box
pointer. Left alone on purpose in step 1 — changing a producer is how a
"no behavior change" step stops being one.

### The producer sites that must state their intent

`placeOperand` covers reads of a place. The following produce or consume a
composite through other paths, and each must be classified explicitly as
**borrow**, **copy-to-new-owner**, **move**, or **cross-boundary clone**. This
list is settled, not a research task; it is written out so nobody rediscovers
it, and step 1 is done when every entry is classified in code.

**Extraction sites — they DO route through `placeOperand`, but too late.**
`lowerFieldAccessExpr` (`internal/mir/lower_expr_access.go:32`),
`lowerIndexExpr` (`:93`) and `lowerTagPayloadExpr`
(`internal/mir/lower_expr_misc.go:30`) each read the container with
`consume=false`, emit `RValueField`/`RValueIndex`/`RValueTagPayload` into a
temp, and only then call `placeOperand` on that temp. The RValue has ALREADY
duplicated the inner box pointer into the temp by the time the decision is
made, so the temp aliases the container's inner composite. The existing repair,
`retainExtractedValue` (`internal/mir/lower_expr_helpers.go:128`), fires only
for refcounted scalars — its doc comment states exactly the hazard ("the temp
would be released at the end of its region while the container still points at
the block") and that hazard now applies verbatim to composites.

**But the repair for a composite is a CLONE, not a retain, and the draft that
said "the composite form of `retainExtractedValue`" was wrong.**
`retainExtractedValue` emits `OperandRetain`, and the VM implements retain by
bumping the count and returning THE SAME HANDLE (`internal/vm/eval.go:138`;
`cloneForShare`, `internal/vm/eval_data.go:336`, is misnamed — it retains,
it does not clone). For an immutable refcounted scalar that is correct and
cheap. For a composite it leaves the temp aliasing the container's inner box,
which is the very defect being fixed. The extraction sites need an independent
allocation, with the container keeping its original box and its normal drop
obligation untouched. **This is a required Phase 1 change**, not a follow-up,
and it is the one place where reusing the existing machinery is the wrong
instinct.

**Literal members.** `lowerStructLiteral` / tuple / fixed-array
(`internal/mir/lower_expr_literals.go:28,79`) evaluate members as consuming
operands. Correct as-is once the operand kind carries intent, but needs a
nested-composite row in the contract tests proving the member is cloned rather
than pointer-stored.

**Call arguments and returns — NOT correct as-is; this is a defect Phase 1
would otherwise introduce.** `lowerCallArgExpr`
(`internal/mir/lower_expr_calls.go:12,84`) must emit the consuming kind for a
composite argument. That alone leaks, because the CALLEE never takes ownership:
`paramTransfersOwnership` (`internal/sema/drop_obligations.go:66`) is
`isDroppableType(id) && !isCopyType(id)`, so a by-value `@copy` composite
parameter is never registered as droppable and the clone the caller just made
is abandoned at the callee's exit. The predicate's own comment justifies the
`!isCopyType` leg ONLY for refcounted scalars ("the parameter merely borrows,
and dropping it in the callee would release a reference the callee never
acquired") — so narrow the exception to refcounted scalars instead of all Copy
types. Returns take an operand at the terminator
(`internal/mir/lower_stmt.go:479,487`) and are materialized before scope drops;
the detachment test there enumerates operand kinds and must learn the new one.
On LLVM every composite is `ptr` in the signature
(`internal/backend/llvm/types.go:39`, `emit_func.go:25`), so a by-value ABI is
Phase 2 work — in Phase 1 the caller passes a pointer to an INDEPENDENT box.

**Crossing captures — the blocker.** `on` / `spawn on` lower a
`CrossingCaptureCopy` with `consume=false`
(`internal/mir/lower_expr_crossing.go:78`), which is precisely the erased case:
a capture-by-copy is indistinguishable from a borrowing read. Blocking captures
build their state literal on a separate path
(`internal/mir/lower_blocking.go:94`).

**Channel sends and far select.** `SelectArmChanSend.Value`
(`internal/mir/instr.go:206`) and the remote payload operand
(`internal/mir/lower_expr_select.go:320`) each need a stated destination-owner
rule, plus what happens on retry and on cancellation.

### Execution (permanent interface, swappable body)

Each backend implements two operations at ITS single site:

- **"produce an independent value of T"** — VM `evalOperand`
  (`internal/vm/eval.go:128`, which today collapses `OperandCopy` and
  `OperandRetain` into one refcount bump), LLVM `emitOperand`
  (`internal/backend/llvm/emit_term.go`).
- **"reclaim the owned parts of T"** — VM `dropValue`
  (`internal/vm/drop.go:134`, today a bare `Heap.Release`), LLVM
  `emitInstrDrop` (`internal/backend/llvm/emit_instr.go:52`) plus the generated
  glue in `emit_drop_glue.go`.

The SIGNATURE is permanent. The BODY is not — and note the body's shape changes
more than "boxed clone → memcpy": Phase 1's LLVM drop is *free the box after
running glue*, Phase 2's is *`drop_in_place(address, T)` that must NOT free the
aggregate's storage*, because the aggregate lives in a frame slot. Write the
Phase 1 helper so that "reclaim the parts" and "release the storage" are two
steps, not one.

### Ownership (permanent)

A value composite OWNS its storage and is droppable. A Copy value composite is
BOTH Copy and owned — that is the combination the type-directed copy exists to
serve.

## The ownership-axis migration checklist

`ownsHeap` cannot be flipped in one place. Every leg below must widen in the
same change, or the build breaks (best case) or double-frees (worst case). This
list replaces the first draft's one-line "`ownsHeap`/`typeOwnsHeap` return
true".

| leg | file | today | after |
| --- | --- | --- | --- |
| sema in-pass | `internal/sema/ownership_axes.go:44` | `isCopyType` → false | value composite → true |
| sema post-check | `internal/sema/ownership_axes.go:71` | same | same, identically |
| drop obligations | `internal/sema/drop_obligations.go:31` | delegates to `ownsHeap` | verify `ReassignOldDrops` still synthesized for the new droppables |
| **by-value param ownership** | `internal/sema/drop_obligations.go:66` | `isDroppableType && !isCopyType` — no Copy type's parameter is ever droppable | narrow the exception to refcounted scalars, or every cloned composite argument leaks in the callee |
| MIR local flags | `internal/mir/lower.go` | DONE — `LocalFlagCopy` from `isCopyType`, `LocalFlagOwnsHeap` from `ownsHeap` | `LocalFlagCopy` now means only "duplicable"; the drop obligation rides beside it |
| **MIR drop validator** | `internal/mir/validate.go` | DONE — rejects on `LocalFlagCopy && !LocalFlagOwnsHeap`; the refcounted-scalar exception is gone | no further change needed; a Copy composite becomes droppable the moment its local carries `LocalFlagOwnsHeap` |
| LLVM structural leg | `internal/backend/llvm/emit_drop_glue.go:30` | walks composites; a composite whose fields own no heap → false | value composite → true |
| VM drop | `internal/vm/drop.go:134` | `Heap.Release` | unchanged shape, but now reached for `@copy` composites |
| crossing drop-fn registration | `internal/backend/llvm/emit_crossing_channel_create.go:86` | keyed off `typeOwnsHeap` | changes meaning for composite payloads — audit, do not assume |
| transport axis | `internal/sema/ownership_axes.go` | DONE — `TriviallyTransportableBits` excludes a Copy value composite via `IsCopyValueComposite` | every crossing route refuses it with a named cause; step 7 upgrades refusal to a boundary clone |

**The validator is a hard blocker, not a detail.** `validateDrop` currently
reads:

```go
if loc.Flags&LocalFlagCopy != 0 && !typesIn.IsRefCountedScalar(...) {
    errs = append(errs, ...drop on copy local...)
}
```

A `@copy` composite with `ownsHeap=true` fails this immediately. Generalize the
predicate to "droppable iff the type owns a drop obligation; a borrow is never
droppable", so the refcounted-scalar exception disappears into the rule instead
of gaining a sibling.

**And the validator cannot ask sema.** `Validate` takes only
`*types.Interner` (`internal/mir/validate.go:22`), while `OwnsHeap` lives on
`*sema.Result` and needs its `CopyTypes` map. "Consult the ownership axis" is
not expressible as written.

**Settled (question 2): the answer travels on the local.** `localFlags`
(`internal/mir/lower.go:503`) records a `LocalFlagOwnsHeap` beside
`LocalFlagCopy` — the lowerer has the sema result right there — and
`validateDrop` checks that flag. MIR validation stays free of a sema
dependency, and the two axes become two flags, which is also why
`LocalFlagCopy` can keep its name.

## The Phase 1 crossing gate (do this BEFORE flipping `ownsHeap`)

Today a `@copy` composite containing no refcounted scalar is
`TriviallyTransportableBits` and crosses as raw bits — the box pointer. Two
shards then hold one box. Making that box droppable turns a latent aliasing bug
into a double-free across shards, and the non-atomic refcount means even the
"just retain it" answer is unsound.

Phase 1 must pick a policy per crossing route BEFORE the ownership flip, and
the routes are: `on` / `spawn on` captures, blocking captures, far task
results, channel send/receive, remote select arms.

Where the clone attaches is settled (question 4): at the capture OPERAND, in
`lower_expr_crossing.go:78` and `lower_blocking.go:112`, not in the three
places that assemble the state literal.

- **Refuse, with a diagnostic — the only option available EARLY.** Sema rejects
  a boxed value composite on a crossing route, naming the real cause at the
  earliest stage that can name it. This makes currently-compiling programs stop
  compiling, and that is honest: those programs are silently sharing one box
  across two shards today. Each refused route gets a debt row with the clone as
  its fix.
- **Clone at the boundary — better, but it cannot come first.** A crossing
  installs a deep, independent copy in the destination shard, and
  refcounted-scalar fields are DEEP-CLONED here (not retained — see the retain
  rule below). This keeps every currently-legal program legal. But it is
  IMPLEMENTED BY the type-directed clone of steps 4-5, so it lands after them,
  not before. The first draft's ordering was circular on exactly this point.

Do NOT claim Phase 2's "inline bits simplify crossings" as a Phase 1 property.
In Phase 1 a composite is still a box.

## The clone algorithm (Phase 1, boxed)

"Copy a value of type T" allocates a fresh box and fills it by walking T:

- **struct / tuple / fixed array** — recurse per field/element.
- **nested value composite** — recurse; a fresh inner box per level. This is
  what closes RV2-DEBT-072 as a side effect: once composites are owned, the
  drop walk reaches the inner box too.
- **refcounted scalar field** (`float`, later `int`/`uint`) — RETAIN. These are
  immutable, so sharing the counted block is sound and a deep clone is waste.
  This rule is SAME-SHARD ONLY; see the crossing gate.
- **handle-backed field** (`string`, dynamic array, map, range, task, channel)
  — governed by that type's existing ownership contract, not by this walk. Do
  not invent a deep copy for them here.
- **reference / pointer field** — copy the bare word, own nothing, never
  follow. `typeOwnsHeapRec` already checks this BEFORE stripping aliases
  (`emit_drop_glue.go:38-50`) and the comment there records why: `&string` once
  looked like an owned string and double-freed.
- **tagged union** — dispatch on the ACTIVE discriminant and clone only that
  payload. Never walk every alternative. The VM's destructor already states
  this invariant for drop (`internal/vm/heap.go:420` releases `obj.Tag.Fields`
  only) and clone must match it. A negative test belongs in the contract.
- **recursive types** — a cycle guard, in the same shape as
  `containsRefCountedScalar`'s `seen` set and `typeOwnsHeapRec`'s. By-value
  recursion is impossible by layout; recursion through a reference is not
  followed; recursion through a handle-backed container is that container's
  problem.
- **failure mid-clone** — no special handling. `rt_alloc` returns NULL on
  exhaustion and no existing caller checks it, so a clone helper that did would
  be the only allocation site in the language with a failure path. See Settled
  Question 3; allocation failure is a whole-runtime concern, not this epic's.

## Contract (frozen tests — survive into Phase 2 unchanged)

These assert language semantics, not representation, so they must pass on both
the boxed (Phase 1) and inline (Phase 2) implementations:

1. copy independence — `let mut q = p; q.a = X` leaves `p` unchanged.
2. nested `@copy` — independence holds through `{ inner: Point, ... }`.
3. copy through a by-value argument, and through a return — asserting BOTH
   independence and leak-freedom, since the callee-ownership leg fails silently.
4. copy of a tagged-union payload binding.
5. overwrite of an existing composite binding frees the old value exactly once.
6. self-assignment `x = x` does not corrupt or free the live value; and the
   self-borrow forms `x = f(&x)` and `x = f(&mut x)` keep working, since both
   are accepted today (measured — see Settled Questions).
7. drop of both copies frees exactly twice, no double-free (valgrind + VM
   census), zero leak after scope exit.

Added by the review, same standing as the seven above:

8. **borrow preservation** — a borrowing read does NOT clone: `f(&p)` observing
   a later `p.a = X`, and a field read used as an operand, still see the
   original storage. This is the guard against the erased-`consume` regression,
   and it is the test that fails loudest if someone implements clone-on-
   `OperandCopy`.
9. **extraction independence** — a composite read OUT of a field, an array
   element, or a union payload is an independent value; mutating it does not
   touch the container, and dropping both frees exactly twice.
10. **union active arm** — cloning a union clones only the active payload;
    a negative control proves the inactive alternatives are not walked.
11. **overlap** — `p.inner = p.inner` and `arr[i] = arr[j]` (including `i == j`)
    are correct and leak-free.
12. **crossing** — per the gate: either a composite captured by `on` is
    independent in the destination shard (clone policy), or the program is
    refused with the named diagnostic (fallback policy). Not silence.

**Do not pin Phase 1 allocation counts as contract.** Phase 1 allocates per
copy and Phase 2 does not; a census assertion written as contract would have to
be rewritten in Phase 2, which is exactly what "frozen" is supposed to prevent.
Census rows live in the recalibration set, not the frozen set.

### Assignment and evaluation order

**Part of the contract.** The current lowering
(`internal/mir/lower_expr_assign.go:14-36`) already does the right thing for a
whole binding and its comment says so: lower the destination place, fully
evaluate the RHS, then `InstrDrop` the overwritten value if
`data.DropOverwritten`, then store. Copy-first-drop-later is therefore not a
change to make but an invariant to PIN — and it only protects case 6 once RHS
evaluation actually produces an independent value, which is what the extraction
fix above delivers.

Three gaps the review found, all Task-level work:

- **The destination place is lowered BEFORE the RHS**
  (`lower_expr_assign.go:14`, `lower_expr_place.go:100`), so `arr[f()] =
  arr[g()]` observably calls `f` before `g`. State this order and test it;
  do not let an implementation quietly reverse it.
- **Projected stores are not `ReassignOldDrops` territory.**
  `ReassignOldDrops` is synthesized for whole bindings only
  (`internal/sema/check.go:63`), while a projected store already drops the
  prior member inside the VM (`internal/vm/place.go:358,390`). Adding a generic
  projected `InstrDrop` without auditing that path double-drops.
- **Compound assignment is already VM/LLVM-divergent for an owning
  destination — a pre-existing defect found by this review, not a new rule to
  add.** `lowerCompoundAssignExpr` (`lower_expr_assign.go:60-91`) reads the
  destination with `consume=false`, builds a temp, and stores back emitting NO
  `InstrDrop`. The VM then releases the overwritten value implicitly inside
  `writeLocal` (`internal/vm/vm_access.go:85-87`), while LLVM emits a bare
  `store` (`internal/backend/llvm/emit_instr.go:179,194`). Today nothing owning
  reaches that path often enough to show, but the moment composites are owned
  the two backends disagree about a free. File it as its own debt row and fix
  it in step 4/5 — do not fold it silently into the composite work, because it
  is not composite-specific.
- **`x = f(&x)`** needs an explicit borrow rule, not an ordering argument.
  Copy-first protects the old `x` only after `f` returns; whether `f` may
  mutate `x`, return an alias-derived value, or suspend is a sema question.

## Phases

### Phase 1 — permanent boundary + temporary boxed implementation

Restores correctness now. No inline work. Ordered, each step gated:

0. **DONE 2026-07-27 — RV2-DEBT-074 and RV2-DEBT-075 both closed.** Neither
   was what the original row said. `compare` on a dereferenced borrow freed the
   referent (the ownership guard tested the scrutinee's TYPE, and a deref
   strips the reference), so every formatted print with a non-string argument
   was a double free; and a refcounted-scalar payload bound out of a borrowed
   union released one reference too many. All 30 showcases now run clean on the
   native backend, against 2 crashing before.

   **This step validated the epic's central claim in miniature, and is worth
   reading before step 1.** RV2-DEBT-075's fix is exactly the mechanism
   described in "The producer sites that must state their intent": an
   extraction cannot decide on its own whether to take a reference, because the
   owned and borrowed cases need OPPOSITE instructions and lower to an
   indistinguishable read. The answer had to be carried down from where
   ownership is actually known. That is the same argument this epic makes for
   `OperandCopyValue`, now with a shipped precedent — `TagPayloadData.
   SubjectBorrowed` — to copy the shape from. It also produced the method rule
   below: a one-directional negative control would have accepted the
   over-broad fix.
1. **DONE 2026-07-27 — the predicate exists and MIR carries intent.**
   `Interner.IsValueComposite` (`internal/types/value_composite.go`) is the one
   predicate, next to `IsRefCountedScalar`: struct, tuple, union and FIXED array
   are inline; `string`, dynamic `Array<T>`, `Map`, `Range`, `Task`, `Channel`,
   references, pointers, far handles and bare enums are not.
   `OperandCopyValue` (`internal/mir/instr.go`) is emitted by `placeOperand`
   when `consume && isValueComposite(ty)` — placed after the move case, so it
   is exactly the Copy composites — and both backends still treat it as
   `OperandCopy`.

   Verified no behavior change: the full `./internal/...` failure set is
   identical to the pre-step run, all 30 showcases stay clean, and the kind
   appears where expected (`L0 = copy_value L1` for a `@copy` struct binding,
   `call f(copy_value L1)` for a Copy composite argument). The predicate has
   its own probes with a non-vacuity assertion, and a negative control confirms
   that disabling its exclusions makes `Range`/`Task`/`Channel`/`Array<T>`
   wrongly report as value composites.

   **What this step taught, for step 4/5:** the enumerating-switch list in the
   Boundary section was incomplete as first written. Five more sites classify
   on `Copy`-or-`Move` without mentioning `OperandRetain`, so no grep for the
   previous kind could surface them; they are now listed there. Budget for the
   same shape of miss whenever a kind is added.
2. **DONE 2026-07-27 — ownership travels on the local, validator generalized.**
   `LocalFlagOwnsHeap` (`internal/mir/types.go`) is set by `localFlags`
   (`internal/mir/lower.go`) from the existing `funcLowerer.ownsHeap` bridge to
   sema, and by the entrypoint builder's own `localFlags` from the same
   no-sema fallback that bridge uses. `validateDrop` now rejects on
   `LocalFlagCopy && !LocalFlagOwnsHeap` — the refcounted-scalar type test
   dissolved into the rule, and `validateDrop` no longer needs the interner at
   all, so the parameter is gone.

   **Why the rule keeps `LocalFlagCopy` in it rather than testing
   `!LocalFlagOwnsHeap` alone.** Plenty of locals are built outside
   `localFlags` with explicit flags — `entrypoint_args.go` alone creates
   several with a bare `LocalFlags(0)` — so a rule that DEMANDS the flag would
   reject drops on synthetic owned locals that are correct today. Keeping the
   rejection keyed on the positive `LocalFlagCopy` marker leaves every such
   local exactly as permissive as before, while the second flag lifts the one
   case that needed lifting. Equivalent today by construction, and it is the
   Copy-and-owned combination that later unblocks value composites.

   Verified: full `./internal/...` failure set identical to step 1, all 30
   showcases still build and run clean (a validation failure surfaces as a
   build failure, so this is a direct corpus check). New unit probe asserts a
   Copy+OwnsHeap local IS droppable; negative control confirms the flag is
   load-bearing — withholding it makes every `float` local fail validation with
   "drop on copy local ... that owns nothing", which is precisely the case the
   old hardcoded exception existed to allow.

**Golden, for steps 1 and 2 together** (run after the fact, per the gap noted in
Verification): 16 fixtures move, decomposing into exactly two shapes with ZERO
unexplained lines — `[owns_heap]` appearing in local-flag dumps (step 2, 143
lines) and three `copy` → `copy_value` operands (step 1). The three are all in
`testdata/golden/mir/imported_operator_overload.mir`, the corpus's only `@copy`
composite (`@copy pub type Digest = { value: uint64 }`), at exactly the intended
sites: two consuming bindings and a by-value argument pair. No id renumbering,
no instruction or drop changes; regeneration is byte-identical across two runs.
Re-blessed.
3. **DONE 2026-07-27 — every route refuses, with a named cause.**
   `Result.IsCopyValueComposite` (`internal/sema/ownership_axes.go`) states the
   one combination whose word is a SHARED box pointer: composite AND
   duplicable. Both halves matter — a move-only composite is equally boxed but
   crosses by transfer, so one shard holds it, and a Copy scalar is equally
   duplicable but its word is the value. `TriviallyTransportableBits` now
   excludes it, and each route reports rather than falling through to the
   generic message:

   | route | stage | says |
   | --- | --- | --- |
   | `on` / `spawn on` capture | sema, SEM3168 | "…capture the fields individually, or move it with `own`" |
   | `blocking` capture | sema, SEM3168 | same, worded for the worker thread |
   | crossing result (`on`/`spawn on`) | transport guard, FUT7020 | "the crossing result…" |
   | far `Task<T>.await()` result | transport guard, FUT7020 | "the awaited result…" |
   | remote channel element | transport guard, FUT7020 | "a remote channel's element…"; remote-select SEND arms inherit it |

   The message is its own helper, not the existing fallback, because "it is not
   plain-copy data" is FALSE for a `@copy` composite and would send the reader
   hunting a non-Copy field that does not exist. The cause is the
   representation, and the diagnostic says so.

   **A regression this step introduced and then fixed, worth not repeating:**
   the first cut refused `Placement`. It is declared as a struct, so the
   predicate called it a composite — but it is `@intrinsic` and its runtime
   value is a tagged word, no box at all, and crossing is the one thing
   placements exist to do. Fixed in `IsValueComposite` itself rather than by
   special-casing the crossing sites, because the predicate was simply wrong
   about it.

   Verified: full `./internal/...` failure set identical to step 2 — the two
   fixtures that broke were the `Placement` bug (fixed) and
   `TestOwnershipAxesAgreeWithCopyToday`, which correctly caught the intended
   axis change and was updated to pin the new shape. Three negative fixtures
   plus a POSITIVE neighbour (a move-only composite channel element, which must
   still ship); negative control confirms all three go red when the predicate is
   disabled. Golden: no change to existing fixtures beyond the step-1/2
   blessing, regeneration deterministic run-vs-run.

   This is the SAFE state the axis flip needs; the clone-at-boundary upgrade
   is step 7, and each refused route is a debt row until then.
4. **DONE 2026-07-27 — the VM produces an independent value.**
   `cloneValueComposite` (`internal/vm/clone_value.go`) allocates a fresh
   object per composite level and is driven by the TYPE, not the object kind,
   because the kinds do not separate the cases that matter — a dynamic
   `Array<T>` and a fixed array are both `OKArray` and only the second is a
   value. Members follow the clone algorithm above: nested composites recurse,
   refcounted scalars and handle-backed members retain, plain bits copy; a tag
   clones `Tag.Fields`, which IS the active arm; a half-built clone releases
   what it made. `evalOperand` clones on `OperandCopyValue` and still retains
   on `OperandCopy`/`OperandRetain` — the borrowing read must keep seeing later
   writes.

   Contract rows 1, 2, 3, 4, 6, 8, 9 and 11 pass on the VM, gated by
   `TestRuntimeV2CompositeCopyIsIndependent`, which is written against
   SEMANTICS only and carries the native backend as a skip that the native
   clone removes without touching the contract text.

   **Two corrections to this step as it was planned.**

   *The census gate cannot live here.* "Heap census net-zero on the leak repro"
   needs drops, and drops need the ownership flip, which must come after BOTH
   backends clone — flipping now would double-free on the native side. Measured
   in the intermediate state: the VM leak repro goes from 10 to 12 net-live
   objects, because a clone allocates where a retain did not and nothing
   reclaims either yet. That is the state the plan predicted ("make copy
   independent without making them droppable and they still leak"), and the
   census belongs to step 6.

   *The parameter-ownership leg is inert here.* `paramTransfersOwnership` is
   `isDroppableType && !isCopyType`, and `isDroppableType` IS `ownsHeap`, so a
   Copy composite parameter is filtered out by the first term regardless of the
   second. Narrowing the Copy exception before the flip changes nothing;
   it moves to step 6, where it becomes load-bearing.

   **A hole in row 8 found by its own negative control, worth copying to every
   later step.** Reverting the clone fails the contract, as expected — but
   making the clone UNCONDITIONAL, so every read duplicates, also PASSES it.
   On the VM that is structural, not luck: places are abstract (`Location`, not
   a raw pointer) and a write goes through the place rather than through
   whatever a read produced, so over-duplication returns identical answers.
   Measured: the same program computes the same result with 510 versus 710
   allocations. So over-duplication has no semantic signature on the VM and
   needs an allocation census, which is what
   `TestRuntimeV2CompositeBorrowReadDoesNotDuplicate` is — deliberately a
   SEPARATE file, since the contract must stay free of representation
   assertions. It took two tries to make that census real: an int is
   arbitrary-precision, so loop arithmetic allocates and swamps the signal
   unless the probe differences against an identical loop with the reads
   removed; and the composite has to be read as a LOCAL, because a read through
   an already-borrowed parameter never produces a composite value operand and
   so can neither duplicate nor detect duplication.
5. **DONE 2026-07-27 — the native backend produces an independent value.**
   `emit_clone_glue.go` generates `@clone.typeN(ptr) -> ptr` per composite,
   deliberately mirroring the drop glue next door: same layouts, same member
   walk, same emission fixpoint. Body: `rt_alloc` a box of the same size,
   `rt_memcpy` the source over it, then fix up ONLY the members whose bits are
   not the value. That order is the design — the memcpy carries every
   plain-bits field, the padding and a union's discriminant, so the fixups
   never need to know the layout of anything they do not own, and a field added
   to a struct is copied correctly the day it is added rather than the day
   someone extends a switch.

   Only three member shapes need fixing, because only a COPY composite reaches
   here and every member of one is itself Copy: a nested composite is replaced
   by a clone of its box, a refcounted scalar takes its own reference, and
   everything else the memcpy already finished. A string, dynamic array or map
   can never appear — none is Copy — which is why no deep-copy policy for them
   had to be invented.

   Contract rows 1, 2, 3, 4, 6, 8, 9, 10, 11 now pass on BOTH backends from one
   source; the skip is gone. That differential is load-bearing: the VM's heap is
   counted and the native heap is not, so an implementation leaning on counting
   passes one and fails the other — confirmed by the negative control, where
   disabling the native clone leaves the VM green and fails the native run at
   row 1.

   **The union active-arm rule needed its own instrument, for the same reason
   step 4's borrow rule did.** Cloning EVERY arm instead of the live one still
   produces the right answers: the arms' payload slots overlap, so the value is
   unaffected — it merely also reads the live arm's bytes as another arm's
   type. With a single payload-carrying arm it is not even wrong, which is why
   the contract's union row cannot catch it. With two arms of DIFFERENT shape it
   dereferences a pointer read out of bytes that were never a pointer, and
   valgrind says so. Gated by
   `TestRuntimeV2CompositeUnionCloneTouchesOnlyActiveArm`, negative control
   confirmed. Third time this pattern has appeared: a rule about what NOT to
   touch is invisible to an assertion about results, and needs a census or a
   memcheck.

   **The valgrind gate in this step's original wording could not hold here
   either.** "Definitely-lost zero on all repro programs" needs drops, which
   need the flip. Measured honestly in the intermediate state: the leak repro
   goes from 389 bytes/22 blocks to 901/54, because a clone allocates where a
   pointer copy did not and neither is reclaimed yet. What IS zero, and is the
   real signal for this step, is invalid reads/writes/frees — none, on every
   probe. Definitely-lost zero moves to step 6 with the rest of reclamation.
6. **DONE 2026-07-27 — the axes are flipped and composites are reclaimed.**
   `ownsHeap`/`OwnsHeap` answer true for a value composite before the Copy
   answer can swallow it; `paramTransfersOwnership` narrowed its exception from
   "Copy" to "reference-counted scalar", which is where it becomes
   load-bearing — a composite argument is CLONED at the call, so the callee owns
   what it received and must drop it; the backend's `typeOwnsHeap` answers true
   for a value composite before its structural field walk, which is what closes
   RV2-DEBT-072 (a nested inner box is reclaimed because it owns its box, not
   because a field of it happens to own heap).

   Contract rows 5 and 7 joined the frozen set, and
   `TestRuntimeV2CompositeIsReclaimed` pins strict-zero definitely-lost under
   valgrind for build-only, build/copy and nested shapes. RV2-DEBT-072 and
   RV2-DEBT-073 are closed.

   **Three defects the flip exposed, each fixed here.**

   *Drop-then-read on reassignment.* `x = x` freed the destination and then read
   it. The existing lowering already claimed the right order — "RHS fully
   evaluated, THEN the overwritten value frees" — but an operand naming a PLACE
   is a LAZY read performed by the store, not by the call that produced it, so
   nothing had been evaluated yet. Latent until composites became droppable.
   Fixed by pinning a place-reading owned operand into a transfer temp before
   the drop.

   *Clone and drop walking different members.* `@copy type Semaphore = {
   permits: Channel<nothing> }` — a Copy composite holding a HANDLE. The clone
   shares the handle, because sharing is what a handle copy means; the drop glue
   then freed it once per copy. That is the invariant the two glues exist to
   keep, violated on the first shape that tested it. Fixed by
   `fieldDropIsExclusive`: a composite's drop frees the members the clone
   duplicated — move-only members, refcounted scalars, nested composites — and
   never a shared Copy handle. This is also where the backend's structural leg
   and sema's answer are reconciled: sema is right about the OBLIGATION, the
   walk is right about the STORAGE, and a drop acts on the obligation.

   *A marker whose default was unsafe.* Binding a literal duplicated the fresh
   box and abandoned the original — visible on the native backend only, because
   the VM's frame teardown sweeps an abandoned temp and the native backend loses
   it. The fix is to TRANSFER from a temp the lowering itself materialized. My
   first cut marked the aliasing temps and treated everything else as owning,
   and that polarity is wrong: a missed shape becomes a transfer of someone
   else's value. It found one immediately — the temp holding the pointee of a
   `&Semaphore` deref — and handed the borrow's box away. Inverted to opt-IN
   marking of materialized temps, where a missed mark costs a wasted duplicate
   that the reclamation census reports as a leak, instead of a use-after-free.

   **Golden moved for the third shape and it is worth reading**:
   `operator_repro.mir`'s `__eq`/`__ne` now take `owns_heap` by-value params,
   drop them at exit, and detach the return value into its own temp first so the
   exit drops do not free what is being returned. That is
   `paramTransfersOwnership` doing the job it could not do before this step.

7. **DONE 2026-07-28 — every route carries a value composite again.**
   The routes did NOT need the same treatment, and this step's main finding is
   that step 3's framing ("clone at the boundary") was right for one shape out
   of three:

   | shape | routes | what it needed |
   | --- | --- | --- |
   | capture — sender keeps its binding | `on`/`spawn on`, `blocking` | a duplicate at the capture operand |
   | result — body builds it, caller receives it | crossing result, far await | nothing but permission: it is a TRANSFER |
   | element — sender keeps its binding | remote channel | a duplicate at the send |

   Adding a copy to the result edge would have allocated a box per reply that
   nobody needed. Captures are now a consuming read for EVERY mode, which also
   settles a live disagreement: blocking captures always read consumingly and
   crossing captures did not.

   **What lifting the refusals was not enough for.** An unpacked COPY capture
   is now reclaimed at the crossing body's returns. The shallow envelope
   release rests on a stated assumption — "the captures were unpacked into
   locals, so only the box is dead" — that was true while a capture was an
   alias and stopped being true the moment the state held a copy of its own.
   The drop is restricted to COPY captures: an OWNED capture may be CONSUMED by
   the body, move tracking is sema's and this synthetic local was never sema's
   to track, so dropping it unconditionally double-freed what the body handed
   on — measured as a tcache double free across the whole migration suite
   before the restriction.

   Verified: a composite captured across `spawn on` is independent of the
   sender and reclaimed at strict zero, gated by
   `TestRuntimeV2CompositeCrossesIndependently`; negative controls both ways
   (borrowing capture fails independence, missing capture drop leaks 32 bytes
   in 2 blocks). The three step-3 negative fixtures became positives beside the
   move-only-element positive, which still ships for the opposite reason.
   RV2-DEBT-076 closed.

   **The session's failure set was re-baselined here against pristine HEAD**,
   because the earlier per-step baselines did not survive a session boundary and
   a number nobody can re-derive is not a baseline. Result: steps 0-7 together
   add exactly ONE failing test to the pre-session suite — the ownership-axis
   invariant, which is supposed to fail when an axis moves and was updated to
   pin the new shape.

8. **DONE 2026-07-28 — the contract is frozen, and freezing it found defects.**
   Rows 1-12 are landed with a manifest in
   `internal/vm/runtime_v2_composite_copy_e2e_test.go` naming what PINS each
   one. The manifest lives in the test rather than a document because the thing
   most likely to rot is the belief that a row is covered.

   **Writing a row down is not pinning it.** The audit found FOUR rows whose
   reclamation nothing measured: the contract test runs without valgrind, and
   the reclamation census covered only build/copy/nested. Measuring rows 3, 4, 5
   and 7 for the first time found two of them leaking, and row 3 leaked TWICE
   per call.

   Root, and it is one root: a materialization temp that is not marked as
   OWNING its value gets duplicated instead of transferred when a composite
   binding consumes it, and its own box is then abandoned. Step 6 introduced
   that marking with a fail-safe polarity precisely so a missed mark would leak
   rather than double-free — and four marks were missing. Now marked: the return
   transfer temp (`detachFromExitDrops`), the call-result temp, binary-op and
   cast temps, and the block-result temp. Each is a site whose own comment
   already said "this is a transfer"; the mark makes the lowering agree.
   Deliberately NOT marked: the deref temp and the tag-payload temp, which
   ALIAS what they read.

   Rows 3, 5 and 7 now carry census coverage and report strict zero. Row 4 —
   a payload bound out of a Copy or borrowed union — still leaks two boxes per
   evaluation and is filed as RV2-DEBT-078 with the mechanism read off the MIR.
   A fix was attempted and REVERTED in the same session: making the compare's
   release predicate answer true for any value composite fixed part of it and
   broke two gates at once, because the release model's shallow-vs-deep choice
   assumes a payload binding MOVES and it now duplicates. That belongs with
   RV2-DEBT-052 and RV2-DEBT-075, not in a tail-end patch.

   **Phase 1 closes here.** The full `./internal/...` failure set is IDENTICAL
   to pristine HEAD — the whole epic adds zero failures — measured against a
   worktree baseline rather than a remembered number. All 30 showcases clean.
   Durable docs updated: `docs/ATTRIBUTES.md` now says what `@copy` means for a
   composite (duplication, not storage) and `docs/KNOWN_LIMITATIONS.md` carries
   the three residual limits, both with their `.ru.md` mirrors.

### Phase 2 — inline representation (the throwaway swap)

Replace the boxed body behind the boundary with inline storage. The contract
tests do not change. Scoped separately below.

## Phase 2 scope

Answered from the code 2026-07-27, corrected by the review:

- **VM: contained at the reference level, NOT free at the storage level.** A
  reference is not a raw pointer — it is an abstract `Location{Kind, FrameRef,
  Local, Index, ByteOffset, Handle}` (`internal/vm/ref.go`), and every
  read/write funnels through `loadLocationRaw` / `storeLocation`
  (`internal/vm/place.go:229,334`), which switch on `Location.Kind` and index
  `obj.Fields[i]`. `ByteOffset` is already carried "for layout-consistent
  addressing even if the VM stores values differently." So place RESOLUTION is
  a handful of choke points. But storage is not: `LocalSlot` holds exactly one
  `Value` (`internal/vm/frame.go:9`) and `Value` represents a composite as
  `VKHandleStruct`/`VKHandleTag` (`internal/vm/value.go:32`) — there is no
  inline aggregate payload in `Value` at all. Phase 2 on the VM needs a new
  aggregate value representation plus frame-slot support, copying, dropping and
  tracing for it. Contained, but not small.
- **LLVM: the heavy half.** Every composite maps to `ptr`
  (`internal/backend/llvm/types.go:39`), args/returns pass the box pointer
  (`emit_func.go:25`), field access derefs. Inline needs a real by-value ABI
  (byval/sret), stack slots, and field GEP — machinery that does not exist yet.
- **Cross-cutting:** heap censuses shift wherever composites were boxed
  (expected recalibration); `AllocID`/use-after-free/tracing reasoning changes
  for composites; crossing transport changes shape (a composite crosses as
  inline bits + refcounted-scalar fields, which SIMPLIFIES the remaining
  composite-crossing barriers — a Phase 2 property only).

Before Phase 2 starts, the places/references and frame-slot storage model must
be designed as its own document — it does not exist yet. The Phase 1 boundary is
deliberately representation-independent so that design is unconstrained.

## Machinery That Already Exists (reuse, do not reinvent)

- **`OperandRetain` and the ownership axes.** Epic 22 already split
  duplicability from heap-ownership from transportability
  (`internal/sema/ownership_axes.go`) and already added an operand kind for
  "duplicate at the surface, count underneath". This epic widens those axes; it
  does not need new ones.
- **`retainExtractedValue`** (`lower_expr_helpers.go:128`) is the right SHAPE
  for the extraction repair — a repair instruction emitted right after the
  RValue, on the temp — but NOT the right operation: composites need a clone
  where scalars need a retain. Reuse the placement, not the body. Same for
  `cloneForShare` (`internal/vm/eval_data.go:336`), whose name promises a clone
  and whose body is a retain.
- **`typeOwnsHeapRec`'s recursion guard and reference check**
  (`emit_drop_glue.go:34-57`) is the template for the clone walk.
- **`containsRefCountedScalar`'s `seen` set** (`ownership_axes.go:136`) is the
  template for cycle handling in a type walk.
- **The `76feda88` box-free for move-only composites** is the drop half already
  working for the non-Copy case; Phase 1 widens its predicate rather than
  writing a second path.
- **The VM's active-arm destructor** (`heap.go:420`) states the union invariant
  the clone must mirror.

## Traps To Know Before Writing Code

1. **Clone-on-`OperandCopy` is the obvious implementation and it is wrong.**
   It allocates on borrowing reads and breaks mutation visibility. Contract
   test 8 exists to catch it.
2. **Flipping `ownsHeap` before the crossing gate ships a double-free**, not a
   leak. Order matters.
3. **The MIR drop validator rejects your first attempt** and the tempting fix
   is a second hardcoded exception. Generalize instead.
4. **Extraction already aliases before `placeOperand` runs.** Auditing only
   `placeOperand` will look complete and be wrong.
5. **Projected stores already drop the old member in the VM.** Adding a generic
   projected drop double-frees.
6. **Union clone must dispatch on the active arm.** Walking all alternatives
   reads uninitialized payload memory.
7. **Retaining a refcounted-scalar field is right locally and wrong across a
   shard** — the count is non-atomic.
8. **Census numbers move in both directions.** Phase 1 adds allocations (per
   copy) and adds frees (per drop). Prove reduction-not-churn per the house
   rule before updating any number, and do not encode Phase 1 counts as
   contract.
9. **A cloned by-value ARGUMENT leaks unless the callee takes ownership.** The
   caller-side change is the visible half; `paramTransfersOwnership` is the
   half that is easy to miss because nothing fails loudly — the program just
   grows. Contract test 3 must assert leak-freedom, not only independence.
10. **A backend that has not learned to clone must not meet a flipped axis.**
   Step 6 last, always. A half-migrated backend turns the leak into a
   double-free, which is strictly worse than what we started with.
11. **`cloneForShare` does not clone.** Neither does `retainExtractedValue`
   despite sitting exactly where a clone belongs. Two names in this codebase
   promise the operation this epic needs and deliver a refcount bump.
12. **A negative control must run in BOTH directions.** Proven in step 0: the
   over-broad fix (retain unconditionally) passed the borrowed probe and
   leaked the owned one. A control that only reverts the fix confirms the
   fix does something; it does not confirm the fix does the RIGHT thing. Every
   conditional this epic introduces — the operand-kind split above all — needs
   a probe for each side of the condition.
13. **Do not bisect heap corruption by exit code.** Also from step 0: the four
   negative controls that "isolated" the original defect were themselves
   corrupt, and merely failed to segfault because the freed block read back as
   a benign value. Run the controls under valgrind. A crash/no-crash split
   attributes to whatever perturbs the allocator, not to the cause.
14. **A rule about what NOT to touch cannot be gated by a result.** Seen three
   times now: over-duplicating on a borrowing read (step 4) and cloning a
   union's inactive arms (step 5) both return correct answers, and both are
   real defects. An assertion on values is blind to them; an allocation census
   or a memcheck is not. When a rule says "only", pick the instrument that can
   see "also".

## Cost Model

Phase 1 is deliberately a REGRESSION in allocation traffic, bought for
correctness:

- a copy of a value composite costs one `rt_alloc` per composite level, plus a
  retain per refcounted-scalar field. Today it costs one word move.
- a drop of a value composite costs one free per level, plus a release per
  refcounted-scalar field. Today it costs nothing (that is the leak).
- a borrowing read costs what it costs today — zero. This is the whole point of
  preserving `consume`.

Phase 2 removes the per-copy allocation entirely (inline copy is a memcpy of
the aggregate plus retains for its refcounted-scalar fields). So the Phase 1
cost is a known temporary, and benchmarking Phase 1 against Phase 0 will look
bad on allocation count and should: the baseline was leaking. Benchmark
correctness-preserving programs, not the leak.

## Sequencing against Epic 22

`float` is the only refcounted scalar today
(`internal/types/refcounted_scalar.go`); `int`/`uint` are planned. **The two
epics are independent and either order works**, provided this one's clone,
drop and crossing logic is type-directed rather than hardcoded to the current
membership of `IsRefCountedScalar`.

They are not interaction-free, though. When `int`/`uint` join, an unchanged
`@copy type Point = { x: int }` silently changes class in three ways: its
same-shard field copy becomes a retain instead of a word copy; its
`TriviallyTransportableBits` flips false via `ContainsRefCountedScalar`
(`internal/sema/ownership_axes.go:106`); and its boundary clone must deep-clone
that field rather than retain it. So Phase 1 is not complete until the contract
tests cover a composite with a refcounted-scalar field AND one with only
plain-bits fields — otherwise Epic 22's next step reclassifies half the test
matrix without a single test noticing.

## Settled Questions (closed 2026-07-27, before implementation)

These were the six open questions. Each is now decided, with the evidence that
decided it. They are settled the way the "Decision" section is settled: do not
relitigate without new evidence.

**1. Operand kind, not a flag.** `placeOperand` emits `OperandCopyValue` when
`consume && isValueComposite(ty)`. Decided by the maintenance argument: `grep
-rn OperandRetain --include=*.go internal/` returns the exact, complete list of
places that must learn a new kind (11 non-test sites, listed in the Boundary
section). A boolean field on `Operand` has no such enumeration — a consumer
that ignores it compiles, passes review, and mis-copies at runtime. The
previous kind's introduction is the proof that the enumerable form works.

**2. Ownership travels on the local, as `LocalFlagOwnsHeap`.** Decided by a
hard fact: `Validate(m *Module, typesIn *types.Interner)`
(`internal/mir/validate.go:22`) has no `*sema.Result` and `OwnsHeap` cannot
answer without one. Of the three ways out, the lowerer already holds the sema
result where flags are computed (`internal/mir/lower.go:503`), so recording a
second flag there costs one line and keeps MIR validation free of a sema
dependency. `ValidateOptions` would thread a predicate through every call site;
a structural reimplementation would create a FOURTH walk to drift from the
other three. This also settles question 6 by construction.

**3. Clone-failure handling is out of scope, and the dichotomy was false.**
`rt_alloc` returns NULL on exhaustion (`runtime/native/rt_alloc.c:40-55`) — it
does not abort — and **no caller checks it**: `emitStructLiteral`
(`internal/backend/llvm/emit_literals.go:24`) stores straight into the returned
pointer. So the language has no allocation-failure path anywhere today, and
"abort-only versus unwind-children" was choosing between two behaviors that
neither exists nor would be consistent with anything else. **The clone helper
follows the existing convention: allocate and use, no check.** Allocation
failure is one cross-cutting concern for the whole runtime, not a rule this
epic invents for one helper. If it is ever taken up, it is taken up everywhere
at once.

**4. The boundary clone attaches at the CAPTURE OPERAND, not at the state
literal.** Decided by counting sites: the capture operand is produced in one
place per mechanism (`internal/mir/lower_expr_crossing.go:78` for `on` /
`spawn on`, `captureOperand` in `internal/mir/lower_blocking.go:112` for
blocking), while the state literal is assembled in at least three
(`prepareSpawnOnCrossing`, `prepareImmediateBodyCrossing`, and
`blockingStateLiteral`), each copying the operand verbatim. Fixing the producer
covers every assembler; fixing the assemblers means finding all of them, now
and forever. There is also a live asymmetry that this repairs: blocking's
`captureOperand` already passes `consume=true`, while the crossing path passes
`consume=false`. Two capture mechanisms disagree today about whether a capture
consumes. Making the crossing path match blocking is the fix, not a new rule.

**5. `x = f(&x)` is ACCEPTED today, in both the shared and mutable forms, and
Epic 23 must keep it accepted.** Measured 2026-07-27, not reasoned: `x =
bump(&x)` compiles and prints the right answer on both backends; `x =
mutate(&mut x)` compiles and the VM prints the right answer. So sema's rule is
already "a self-borrow in the RHS of a self-assignment is legal", the
assignment ordering in `lower_expr_assign.go:14-36` is what makes it safe, and
this epic's job is to not break it. Contract test 6 gains a row for the
mutable form.

**One thing that measurement found and this epic does not own.** The LLVM
backend SEGFAULTS on `x = mutate(&mut x)` while the VM prints correctly — and
the minimal trigger is narrower than self-assignment: writing through a `&mut`
composite parameter, then reading through it, in a function that returns a
value. Four negative controls isolate it. Filed as RV2-DEBT-074, PRE-EXISTING,
attribution not yet made. **It must be closed or attributed before step 6**,
because the ownership flip changes what executes on exactly that return path,
and an unattributed segfault sitting there will be blamed on this epic.

**6. `LocalFlagCopy` keeps its name**, because decision 2 gives it a sibling
that carries the other half. `LocalFlagCopy` means duplicability;
`LocalFlagOwnsHeap` means drop obligation; a value composite carries both. The
name only lied while one flag was answering two questions — which is this
epic's root defect in miniature, and the reason to fix it by splitting rather
than renaming.

## Out Of Scope

- Any inline representation work in Phase 1. If a change is only justified by
  Phase 2, it belongs in the Phase 2 document.
- A by-value LLVM ABI (byval/sret). Phase 2.
- Copy-on-write with a refcount on the box. Evaluated and rejected: composites
  are MUTABLE, so COW needs a write barrier at every mutable field/index store
  and before every mutable borrow — the VM mutates `obj.Fields` directly through
  `storeLocation` (`internal/vm/place.go:334`) and LLVM does a raw GEP+store.
  That is a larger aliasing design than inlining, and it is exactly the
  temporary machinery Phase 2 would delete. It also does not reuse Epic 22's
  work in the way it appears to: scalars are immutable, so retain suffices;
  mutable COW composites need uniqueness checks the scalars never needed.
- Unboxing only SMALL Copy composites while keeping large ones boxed. Not a
  cheaper correctness path — large composites still need the same clone and
  drop, and a size threshold must be concealed from every ABI, capture,
  transport and tracing path. Viable as a Phase 2 OPTIMIZATION once the
  semantic interface exists; not a substitute for it.
- Banning or deprecating `@copy` composites until inline lands. This is the
  cheapest safety stopgap and it is honestly a language REGRESSION — it removes
  legal Copy semantics rather than fixing an implementation. Kept on the record
  as the emergency lever if Phase 1 stalls, not as a plan.
- A garbage collector, in every form. Hard project constraint.

## Verification

- **`make golden-check` per step that can change compiler output.** `go test
  ./internal/...` does NOT cover the golden corpus — it is
  `scripts/golden_update.sh` plus a `!golden` build tag on several suites — and
  missing that was a real gap for the first steps of this epic. Regeneration
  itself is reproducible again since RV2-DEBT-070 closed, so a single run no
  longer conflates a regression with a renumbering flake — but a diff still has
  to be READ, because a wrong answer regenerates just as stably as a right one.
  The protocol: regenerate on the stashed baseline, restore and
  regenerate, then DECOMPOSE the diff — every changed line must be explained by
  an intended shape, with zero unexplained lines — then regenerate again and
  diff run-against-run to show determinism, then read at least one fixture for
  meaning rather than shape. Fixtures moving is expected and legitimate here;
  blanket re-blessing is not.
- The frozen contract tests (1-12), on both backends, both phases.
- valgrind definitely-lost zero on both backends for build/drop, copy-then-drop,
  nested, arg/return, tagged-union-payload, and crossing programs.
- Differential: VM and LLVM byte-identical on composite programs. Note the VM is
  currently an INCORRECT reference for `@copy` (it aliases too), so the
  differential is not sufficient on its own — the value-independence assertions
  are the real check.
- Negative controls: reverting the extraction repair reproduces the extraction
  alias; reverting the operand-kind split reproduces the borrow regression
  (test 8); walking all union arms reproduces the inactive-payload read.
- Heap-census recalibration is expected in BOTH directions; prove
  reduction-not-churn before updating any number.

## Related Ledger Rows

`docs/runtime-v2-epics/DEBT.md`: RV2-DEBT-073 (Copy composite leak + aliasing —
this epic's Phase 1 closes it), RV2-DEBT-072 (nested inner box — Phase 1 closes
it via general composite droppability). Relates to Epic 22 (the refcounted-scalar
fields inside a value composite reuse the retain/release already shipped).

RV2-DEBT-074 (LLVM segfault on write-then-read through a `&mut` composite
parameter with a value return) was filed BY this epic's design review and is
step 0 above: pre-existing, unrelated in cause, but on the path step 6 changes.

New rows to file when Phase 1 starts:

- one per crossing route closed by refusal in step 3, retired by step 7;
- the compound-assignment VM/LLVM divergence (VM releases the overwritten value
  inside `writeLocal`, LLVM emits a bare store) — pre-existing, found by this
  epic's design review, not caused by it.
