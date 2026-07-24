# Epic 22 — Numeric Reclamation (heap bignum ownership)

Status: designed, not started. This document is the onboarding brief — read
it end to end before touching anything; it is written so a reader with no
prior context can start work without reconstructing the discussion.

## Why This Epic Exists

`int`, `uint` and `float` are arbitrary-precision. A value crosses the
runtime boundary as one machine word, tagged (`runtime/native/rt_bignum_tag.h`):

- low bit 1 → inline "fixnum", payload in the upper 63 bits. No heap.
- low bit 0 → an aligned `SurgeBigInt*` / `SurgeBigUint*` / `SurgeBigFloat*`,
  or NULL. NULL is the canonical zero and is never produced inline.

These three types are builtin **Copy** (`internal/sema/type_expr_utils.go`,
`isCopyType` → `tc.types.IsCopy`). Because they are Copy, the drop system
never reclaims the heap half. So:

- **`int`/`uint`** leak only a value beyond the inline range (int ±2^62, uint
  0..2^63−1) that is retained to scope exit. Measured: a plain local with a
  60-digit literal leaks 36 bytes in 1 block, stack `bi_alloc`. Arithmetic
  intermediates already free themselves (`bi_finish` demotes a result that
  fits back into a word), so hot loops are balanced.
- **`float`** is much worse: it has NO inline form at all, so every value is
  heap and every operation leaks. Measured: 100 float additions leak 3,216
  bytes in 201 blocks — roughly two blocks per operation, growing linearly.
  An f64 inline form is ruled out on purpose (`float` keeps full arbitrary
  precision — `1.0/3.0` prints ~250 digits — and an f64 inline would change
  program output and break the VM↔LLVM differential). See RV2-DEBT-038.

This is a pre-existing defect with no Runtime V2 connection; it predates the
refactor and would survive it untouched. Per the project's priority rule,
that is the class not to carry forward.

## The Decision, And Why

**Direction: reference-counted immutable values with value semantics** —
Swift's model. The heap block carries a count, `let b = a` keeps both usable
with no move errors and no surface-language change, a borrowing read never
clones, drop obligations extend to these scalars, the block frees at zero.

Two framing facts decided it.

**The survey.** Every language offering a seamless dynamic int has a garbage
collector. Ruby, OCaml, Lisp and Smalltalk use the *identical* tagged
fixnum/bignum representation Surge uses and simply GC the heap half.
Languages without a GC drop a different leg: Rust makes big numbers an
explicit non-Copy type (`BigInt`), Go/C/Java/Swift fix the width. The
combination Surge wants — seamless dynamic int, value semantics, no GC — is
implemented by nobody. So this is a choice of which leg to release.

**No GC is a hard project constraint**, stated by the owner as a principle.
Treat "add a GC" as out of scope in every form, including narrow ones (a
collector for only the bignum heap, a scoped/generational pass, deferred
sweep). Swift and Rust are the reference models.

**Why refcounting is COMPLETE here, not an approximation.** Refcounting's
classic weakness is uncollectable cycles. Verified structurally by reading
`runtime/native/rt_bignum_internal.h`: there is exactly ONE pointer field in
the whole family — `SurgeBigFloat::mant` → `SurgeBigUint`, which is a leaf
(`limbs[]` is an inline flexible array). `SurgeBigInt` and `SurgeBigUint` are
leaves. Max depth 2, and **no field can hold a back-edge**, so a cycle is
impossible by type, not by convention. Nothing a programmer writes can create
one, and a bignum block can never transitively own a user aggregate. This is
exactly why the objection that pushes Ruby/OCaml/Lisp toward a collector does
not apply: their heaps are general object graphs, Surge's bignum heap is not.

**Copy-on-write is NOT needed — the design is smaller than it sounds.**
Nothing mutates a bignum in place: every `rt_big*` entry takes `const void*`
and returns a fresh pointer, and every field write targets a freshly
allocated `out` (`rt_bignum_int.c`, `rt_bignum_float_core.c`; `bf_neg`
mutates only after `bf_clone`). So these are refcounted **immutable** values.
There is no detach-on-mutation clone, and sema never has to answer the
question RV2-DEBT-038 called hard ("`let b = a` must clone while
`rt_bigfloat_add(a,b)` must not") — with immutability, `let b = a` is just a
retain.

## Owner Decisions (settled — do not relitigate)

1. **The refcount is NON-ATOMIC from the start.** The alternative (ship
   atomic, flip later behind a measurement) was considered and rejected: the
   owner chose to do the barrier work up front rather than carry an
   intermediate state. Consequence: the cross-shard barriers and the globals
   rule below are Phase 1 work, not a later phase.

2. **Module-level `let` is BANNED outright** — both `let` and `let mut`. The
   only globals a program may declare are `const`, i.e. compile-time numbers
   and strings. Rationale from the owner: needing a global array or struct is
   rare, and "declare a global array and work with it concurrently" is a
   pattern worth refusing at the language level rather than supporting. This
   supersedes the immortalization proposal — no sentinel refcount, no
   clone-on-read, no exception for `mut`.

## The Load-Bearing Claim, And Where It Breaks

The non-atomic count is only sound if a bignum block is never shared between
shards. Shards are OS threads in ONE address space (`docs/RUNTIME_V2.md`), so
a copied pointer is always dereferenceable by the other side — **only a clone
at each boundary prevents sharing**.

**Six paths have a clone point** (work is real but bounded). Each ships a raw
pointer word today and needs a deep copy inserted:

- `on` / `spawn on` captures — `internal/mir/lower_expr_crossing.go` lowers
  `CrossingCaptureCopy` with `consume=false`, and
  `runtime/native/rt_immediate_on.c` ships `body_state` by pointer.
  `spawn on` is the sharpest case: it returns `far Task<T>` and does not
  suspend the caller, so two shards hold independently-droppable refs.
- `blocking { }` — `internal/mir/lower_blocking.go` passes `consume=true`, but
  `placeOperand` (`internal/mir/lower_expr_helpers.go`) downgrades to
  `OperandCopy` for Copy types, neutering the flag. State goes to a
  `pthread_create`'d worker.
- far channel send — `rt_channel_send(void*, uint64_t value_bits)`.
- crossing reply / `far Task.await()` — result crosses as raw `result_bits`.
- remote select SEND arms — `rt_far_channel_select.c`, `send_bits` and
  `body_state` both by pointer.
- (`@shard_movable` owned moves need no clone — exclusive transfer.
  `share()` publishes only a sibling handle, no payload.)

**One path had NO clone point, and it is now closed by banning module-level
`let` (see Owner Decisions). Recorded here because it is the reason for the
ban:** `ast.ItemLet` at module scope becomes a global
(`internal/hir/lower.go` ~189-191), initialized in `__surge_start`. Any shard
can then load that global into a local — a retain of a shared block from an
arbitrary shard, with no crossing involved and nowhere to install a copy.

**Measured scope of the globals problem (this is what makes it cheap):**

- ZERO module-level `let` in `stdlib/` and `core/` — globals appear only in
  test fixtures.
- The only `let mut` global in the entire tree is `let mut y: nothing;` — not
  numeric.
- Every numeric global is tiny (`1`, `a + 2`, `10`, `42`), i.e. inline, i.e.
  never heap.

So today **no global is a heap bignum at all**. The hole is real but
currently unexercised.

**Why the ban is the right answer rather than a workaround.** The decisive
fact is that `const` and a global `let` are NOT the same mechanism:

- `const` is INLINED at each use site (`lowerConstValue`,
  `internal/mir/lower_expr_helpers.go` calls `lowerExpr(decl.Value)`), so it
  never creates a shared slot. Each use materializes its own block inside the
  shard that runs the code. Measured: a big `const` used twice leaks 72 bytes
  in 2 blocks — one per use — so it is an ordinary per-use value that Phase
  1/2's normal drop machinery reclaims, and it is structurally safe for a
  non-atomic count because nothing is shared.
- a global `let` creates ONE block in `__surge_start` that every shard reads.
  That is the shared block, and there is no crossing at which to clone it.
  `internal/mir/entrypoint.go` also STORES globals, so `let mut` is a second
  publication point on top of the read path.

Narrowing the ban to numeric globals would NOT close the hole: a global
`int[]` holding out-of-range values is the same shared block one level down.
So the ban has to cover module-level `let` entirely.

**What the ban costs, stated honestly.** `const` only accepts compile-time
numbers and strings — arrays and structs are rejected with SEM3026
("initializer must be a compile-time constant"), verified. A global `let`
accepts all of those plus `let mut`. So the language loses global arrays,
global structs and global mutable state outright. There are ZERO of these in
`stdlib/` and `core/`, and 17 test fixtures use module-level `let` (parser and
sema fixtures such as `testdata/test_let/*` and
`testdata/golden/mir/toplevel_globals.sg`) which will need rewriting or
reclassifying as invalid.

**Two further costs found while landing the ban, not visible at design time:**

- **`@deprecated` on a binding becomes unreachable.** A `let` STATEMENT carries
  no attributes (`Stmts.NewLet` has no attrs parameter), so a module-level
  `let` was the only deprecable variable. `checkDeprecatedSymbol(symID,
  "variable", ...)` (`internal/sema/type_expr_values.go` ~42) can no longer
  fire; `"constant"` and `"function"` are unaffected. The two cases were
  removed from `testdata/golden/sema/invalid/attrs/deprecated_usage.sg`.
- **SEM3108 (`SemaTaskEscapesScope`) is no longer reachable alone.** Its only
  emission site is the module-scope `ItemLet` arm, so every program that
  triggers it now triggers SEM3177 on the same line. Left in place
  deliberately — it still names a second, more specific problem, and retiring a
  diagnostic is its own decision. `task_escape_global.diag` records all three
  codes.

## Do NOT Flip `IsCopy`

RV2-DEBT-038's sketch (M4) says to make these types non-Copy. **That cannot
ship.** `IsCopy` currently means three things at once — surface
duplicability, plain-bits shippability, and non-droppability — and flipping
breaks all three. Verified breakage:

- stdlib stops compiling: `@copy` requires all fields Copy
  (`internal/sema/attr_validation_types.go`), and `@copy` types with numeric
  fields are widespread — `stdlib/bytes/bytes.sg` (`ByteRange` = `{start:
  uint, end: uint}`, `ByteLine`), plus `stdlib/hash`, `stdlib/time`,
  `stdlib/term` and `core/sync.sg`. Note the attribute sits on its own line
  above the type, so a single-line grep for `@copy pub type` finds nothing —
  search for `@copy` alone.
- golden fixtures invert, e.g.
  `testdata/golden/crossing/block04/valid/locality_positive_copy_and_movable_on.sg`
  is `@copy @shard_movable type Point = { x: int, y: int }`.
- every scalar crossing is rejected: `internal/buildpipeline/crossing_guard_classify.go`
  and `crossing_transport.go` gate shippability on `IsCopyType`, so
  `on dst { ret 3 }` would emit a not-shippable diagnostic.
- captures fall through to an error arm in
  `internal/sema/on_crossing_capture.go`.
- move errors everywhere: `internal/sema/borrow_runtime_ops.go` short-circuits
  move tracking for Copy types, so `let b = a` becomes a move and `a` is
  unusable — exactly what "keep value semantics" forbids.

**Instead split off two independent axes, leaving `IsCopy` untouched:**

- `OwnsHeap(T)` — replaces the `!isCopyType` definition of droppability
  (`internal/sema/drop_obligations.go`, `internal/mir/lower_stmt.go`,
  `internal/mir/lower.go`) and joins the recursive walk in
  `internal/backend/llvm/emit_drop_glue.go`. True for the three WidthAny types.
- `TriviallyTransportableBits(T)` — replaces `IsCopyType` at the four
  crossing-gate sites. False for the three, which become "shippable WITH a
  cross-clone".

Blast radius is exactly three TypeIDs: `int`/`uint`/`float` are WidthAny
(`internal/types/interner.go`); `i8..i64`, `u8..u64`, `f32`/`f64` are separate
types and are untouched.

**As landed (Phase 0b).** Both axes live in `internal/sema/ownership_axes.go`:
`typeChecker.ownsHeap` for in-pass use, `Result.OwnsHeap` and
`Result.TriviallyTransportableBits` for MIR and the build pipeline. `IsCopy` is
untouched. Routed: `isDroppableType` (`drop_obligations.go`) is now a one-line
delegate to `ownsHeap`; the two MIR drop sites (`lower_stmt.go` — explicit
`@drop` lowering and `emitExitDrops`) go through a new `funcLowerer.ownsHeap`;
the four crossing-gate sites read `TriviallyTransportableBits`.

**OwnsHeap is NOT simply `!IsCopy`, and the difference is load-bearing.**
`&mut T` is not Copy (`internal/types/interner.go` — `&T` is Copy, `&mut T` is
not), so a plain negation would make a mutable borrow droppable and free
storage the holder never owned. Both legs therefore carry an explicit
reference/pointer clause. This was already true of `isDroppableType`; naming
the axis makes it visible instead of incidental.

**The legs that did NOT move in 0b, and must move together in Phase 1** — all
three are recorded in the `ownership_axes.go` header so they are found from the
predicate rather than by memory:

- `Emitter.typeOwnsHeap` (`internal/backend/llvm/emit_drop_glue.go`) — the
  backend's STRUCTURAL leg: it walks composites for a heap-bearing leaf instead
  of asking about Copy. Sema decides whether a value carries a drop obligation;
  this decides whether that drop has glue to call. Widening one alone either
  drops nothing or calls a function that was never emitted.
- `funcLowerer.localFlags` (`internal/mir/lower.go`) sets `LocalFlagCopy`, and
  `internal/mir/validate.go` (~524) REJECTS `InstrDrop` on a local carrying it
  ("drop on copy local"). A droppable Copy scalar trips that validator on the
  first Phase 1 program.
- `funcLowerer.placeOperand` (`internal/mir/lower_expr_helpers.go`) bit-copies
  Copy operands rather than moving them — trap 4's `OperandRetain` lands here.

**Gate.** `TestOwnershipAxesAgreeWithCopyToday` walks every type in a snippet's
interner and pins both invariants against `IsCopy`, with a companion row
asserting the snippet actually contains a Copy borrow, a non-Copy borrow and a
heap-owning struct (so the walk cannot pass over an empty universe). Phase 1
should see `int`/`uint`/`float` and NOTHING ELSE break the OwnsHeap row.
Negative control: deleting the borrow clause from `Result.OwnsHeap` fails it on
the `&mut int` row. Zero golden diff across the whole tree is the second
witness that 0b changed no behavior — MIR goldens print every drop instruction
and the crossing goldens print the shippability diagnostics.

## Machinery That Already Exists (reuse, do not reinvent)

- **The VM already implements this design.** `internal/vm/heap.go` has a full
  non-atomic refcounted heap: `Retain` (~:295), `Release` (~:322) freeing at
  zero, use-after-free detection (`PanicRCUseAfterFree`), refcount-overflow
  detection, and `AllocBigInt`/`AllocBigUint`/`AllocBigFloat`. Retains already
  fire on value loads (`eval.go`, `eval_data.go`, `eval_ops.go`, `tag.go`).
  **LLVM leads; the VM's existing retain sites are the checklist for where
  LLVM needs them, and the differential converges rather than diverges.**
- **The deep clone is the mirror image of drop glue.**
  `internal/backend/llvm/emit_drop_glue.go` already has recursive per-type
  glue, TypeID-keyed dispatch (`__surge_drop_result_call`,
  `registerCrossingDropState`, `registerCrossingDropResult`) and
  reference/pointer exclusions that were debugged the hard way. A symmetric
  `clone_result.type<ID>` family is a structural analog, not new invention.
  NOT covered by that walk: maps.
- The clone must be RECURSIVE — structs, tuples, fixed arrays and union
  payloads can all carry int/float fields.

## Traps To Know Before Writing Code

1. **`bi_as_uint` layout trap.** `runtime/native/rt_bignum_internal.h`
   reinterprets a `SurgeBigInt`'s tail as a `SurgeBigUint` via
   `(const SurgeBigUint*)&i->len`, relying on `SurgeBigUint` being exactly
   `{len, limbs[]}`. There are 20+ call sites. **Putting a refcount BEFORE
   `len` silently breaks all of them.** Two safe repairs: a suffix-compatible
   layout (`BigUint{len,rc,limbs[]}` / `BigInt{neg,pad,len,rc,limbs[]}`) so the
   alias survives, or a prefix header reached at `ptr[-1]`. Also: alloc/free
   size arithmetic adjusts automatically only if every allocation and release
   routes through `bu_alloc`/`bi_alloc`/`bu_free`/`bi_free`.
2. **Nested ownership is real.** A bigfloat POINTS TO its mantissa, and today
   owns it exclusively (`bf_normalize_mantissa` never returns its input; every
   `out->mant = mant` transfers a fresh block; `bf_clone` deep-copies). Choose
   deliberately: refcount the float only and keep deep-copying the mantissa
   (simplest), or refcount both levels (makes `bf_clone` O(1) and cross-shard
   clones cheaper; still a DAG). Do the single-level version first. The trap:
   the mantissa must be released exactly once, at the float's final release.
   Note the VM EMBEDS the mantissa by value (`internal/vm/bignum/float.go`), so
   VM bignums are pure leaves — the VM is a spec for retain/release PLACEMENT
   but NOT for mantissa ownership.
3. **BLOCKER to schedule, not discover.** `internal/sema/drop_obligations.go`
   (~56-63) SUPPRESSES drop obligations inside `on` / `spawn on` / `blocking`
   bodies, pending RV2-DEBT-034. Today that is free because scalars are not
   droppable. The moment they are, **every bignum created inside a crossing
   body leaks** — and crossing bodies are where server loops live. RV2-DEBT-034
   must land with or before Phase 1.
4. **`OperandCopy` cannot express "retain".** `internal/mir/lower_expr_helpers.go`
   makes a raw bit copy and cannot distinguish storing `a` into `b` (retain)
   from passing `a` to `rt_bigint_add(const void*,...)` (borrow). No type
   predicate can decide this — it is a property of the USE SITE. A new operand
   kind (`OperandRetain`) is needed, emitted at: let-init from a place,
   assignment RHS, struct-literal fields, array element stores, by-value
   argument passing, return materialization, channel send payloads, and
   capture-state fields. Missing one is a leak; a spurious one on a borrowing
   argument is also a leak. **This is the main correctness surface of the
   epic.**
5. **Retain/release must be inlineable IR, not opaque runtime calls.** The
   natural follow-on optimisation is emitting the arithmetic fast path as IR
   at the call site (the algorithm already exists in `rt_bigint_add` — two
   inline operands are added in registers and only promoted on overflow; what
   is missing is that every add is still an out-of-line call). If retain and
   release are builtins in the `rt.h` table, every copy pays a call and eats
   that win. Expose them as emitter-generated IR from day one:
   `if (!(w&1) && w) ...`. The "is it inline" bit test is ALREADY load-bearing
   for representation, so making it gate ownership too reuses the same test
   rather than adding one — they compose cleanly.

## Cost Model

Retain/release on an inline fixnum is one predictable, not-taken branch — no
call, no allocation, and the counter is never touched. So this does not make
the common integer path expensive. Where the cost genuinely lands is **code
size / IR size**: every int copy grows from a register move to a
test-and-branch, at every let-init, argument pass, return, struct-field init
and array store. Treat that as scope, not as a veto.

Note the baseline is now clean: RV2-DEBT-036 (literal churn) was fixed first,
precisely so that reclamation measurements mean what they appear to. Before
that fix a hot loop's allocation traffic was dominated by re-parsing decimal
literals (1,400,015 allocations in a 100,000-iteration loop, now 3 for the
whole run). **Do not benchmark this epic against anything older than commit
`e9fb6713`.**

## Phases

- **Phase 0a — ban module-level `let`. DONE.** A sema diagnostic refusing
  `ast.ItemLet` at module scope, pointing the user at `const`. This is the one
  step that DOES change language semantics, so it lands on its own and is
  separately reviewable. It is a prerequisite for the non-atomic refcount, not
  a cleanup.

  As landed: `SemaModuleLevelLet` = SEM3177, reported from
  `internal/sema/module_level_let.go` off the `ItemLet` arm of `walkItem`
  (`type_checker_walk.go`), which only ever sees module-scope items. The item
  is reported and then checked as before — deliberately, so a use site of the
  banned global does not cascade into unrelated type errors. Verified: a
  program declaring and USING a global emits exactly one diagnostic.

  Fixture count correction: only THREE of the 17 were live. `testdata/test_let/*`
  (13 files) and `testdata/test_fixes/let_fixes/*` (2) have NO consumer in the
  tree — no Go test, script, or Makefile target reads them — and they exercise
  type-expression PARSING, which the ban does not touch (it is a sema rule by
  design, per the kindness-first placement). They were left alone. The live
  three: `testdata/golden/mir/toplevel_globals.*` DELETED (its subject, MIR
  global lowering, is now unreachable from source);
  `testdata/golden/spec_audit/s03_expr_variables.sg` rewritten to drop its
  top-level `let` and its "top-level let: PASS" claim;
  `testdata/golden/sema/invalid/attrs/deprecated_usage.sg` rewritten (see the
  `@deprecated` cost above). A fourth, `.../attrs/deprecated_usage.sg`, was
  invisible to a `^let` grep because the declaration begins with its attribute
  — grep for `\blet\b` at column 0, not `^let`. New golden
  `testdata/golden/sema/invalid/module_level_let.sg` pins both `let` and
  `let mut`.

  **Dead code this leaves, for a later cleanup (deliberately NOT in this
  commit, to keep the ban separately reviewable):** `hir.Module.Globals` is fed
  only by `ItemLet` (`internal/hir/lower.go` ~189), so the whole globals path —
  HIR globals, the MIR `globals=` section, and the `__surge_start` global
  stores in `internal/mir/entrypoint.go` — is now unreachable from valid
  source, and is no longer covered by any golden.
- **Phase 0b — predicate split, no behavior change. DONE.** Introduce
  `OwnsHeap` and `TriviallyTransportableBits` as independent axes, defined so
  every current answer is preserved. Land green with `IsCopy` untouched.
  Separately reviewable; this is what makes everything after it safe. See "As
  landed (Phase 0b)" under "Do NOT Flip `IsCopy`" for the shipped shape, the
  three legs still to move, and the gate.
- **Phase 1 — float vertical, LLVM only, non-atomic from the start.** Float
  first: ~90% shared machinery so this is risk reduction rather than cost
  reduction, but the risk reduction is real — float has no fixnum tag (only
  `NULL == zero`) so retain/release is one branch simpler, its type-system
  radius is smaller (the stdlib `@copy` structs use uint/uint64/int64 and the
  crossing fixtures use int, so float touches neither), and float is the
  unbounded per-operation leak. Order within the phase: refcount field +
  layout repair → `OperandRetain` emission → drop-obligation rewiring →
  cross-clone glue → **the six crossing barriers and the globals rule**
  (these are in Phase 1 because the count is non-atomic from day one).
  RV2-DEBT-034 must be resolved by here.
- **Phase 2 — `int`/`uint`.** Adds only the fixnum-tag branch to a mechanism
  already proven by float.
- **Phase 3 — inline arithmetic in IR.** Independent of this epic; see trap 5
  for the one constraint it places on Phase 1.

## Verification

- **Leak witnesses, the primary gate.** Per phase, a valgrind row asserting
  `definitely lost` zero on: a retained out-of-range literal in a plain local;
  a float loop (the current 201-block leak must go to zero); a value crossing
  each of the six boundaries; a bignum created inside a crossing body.
- **Exactly-once, not just zero.** Reverting the fix must reproduce the leak
  (negative control) — a row that passes both ways proves nothing. This
  session was burned by exactly that twice.
- **Differential.** LLVM output must match the VM byte-for-byte on the numeric
  fixtures, including the fixnum boundaries (2^62−1 inline vs 2^62 heap;
  2^63−1 inline uint vs 2^63 heap).
- **Existing gates.** `make check`, then `make runtime-v2-transport-check`,
  `runtime-v2-crossing-check`, `runtime-v2-heap-check`, `make golden-check`.
- **Heap-census recalibration is expected.** Several tests assert exact
  alloc/free deltas (`TestRuntimeV2DropSelectSendArm`,
  `TestRuntimeV2DropScopeExit`, `TestLLVMNativeBufferedChannelAllocatesSingleBlock`,
  the crossing censuses). When a delta moves, PROVE the change is balanced
  churn — allocations falling with frees, valgrind A/B byte-identical — before
  updating the number. Do not silence a census.
- **Known flake, not yours:**
  `TestRuntimeV2RemoteTaskBehavior/shutdown-wakes-reply-waiters-on-all-shards`
  fails roughly 1 run in 8 on a clean tree, measured both ways.

## Open Questions

1. **Mantissa refcounting** — single-level first (see trap 2); revisit after
   the single-level version is green.

## Related Ledger Rows

`docs/runtime-v2-epics/DEBT.md`: RV2-DEBT-068 (this epic's parent row, scope
corrected to language-wide), RV2-DEBT-038 (the float half, with the M4
`IsCopy` flip that must NOT be taken literally), RV2-DEBT-035 (the fixnum work
that removed the common-case allocation and left this tail), RV2-DEBT-034 (the
crossing-body drop suppression, a Phase 1 blocker), RV2-DEBT-036 (literal
churn — CLOSED, and the reason the baseline is now trustworthy).
