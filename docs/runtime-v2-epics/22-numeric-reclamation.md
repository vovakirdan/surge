# Epic 22 — Numeric Reclamation (heap bignum ownership)

Status: PARTIAL — RESUMED 2026-09-04, scope chosen. Phases 0a, 0b and 1 shipped:
the ownership axes were split out of `IsCopy`, and `float` is a
reference-counted scalar with a strict-zero valgrind gate
(`TestRuntimeV2FloatReclamationValgrindZero`). NOT done: the crossing deep-copy
barriers listed under "Phase 1 remainder" — the runtime helper
`rt_bigfloat_clone` exists but is unwired, and crossings still REFUSE a
composite carrying a `float` — and Phase 2 (`int`/`uint`), not started.

**The owner answered this epic's open question on 2026-09-04: variant (2) —
build the barriers for all three types first, then add `int`/`uint` to a
finished mechanism.** With it came a second ruling on what the capability
verdicts mean, and the two together decide the order. Both are recorded under
"Phase 2's Scope Question" below; read that section before the phase list.

Parked to take Epic 23, then Epic 24, and unparked when Epic 23b closed. See the
detour chain in `README.md` for why, and note what that chain got wrong: 23b was
assigned these barriers and did NOT deliver them, so this epic inherited a
question rather than a mechanism. What 23b DID deliver is the typed-carrier
plumbing the barriers hang on — the slots are empty, the transport is not.

This document is the onboarding brief — read it end to end before touching
anything; it is written so a reader with no prior context can start work
without reconstructing the discussion.

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

**The paths that need a clone point — MAP RE-DERIVED 2026-09-04 (Global Rule
14).** The list below used to cite `rt_channel_send(void*, uint64_t value_bits)`,
a result crossing as raw `result_bits`, and `send_bits` by pointer. **That ABI is
dead.** Epic 23b replaced it with typed carriers: `rt.h` now declares
`bool rt_channel_send(void* channel, void* src)`, and `value_bits` /
`result_bits` / `send_bits` survive only in the legacy-carrier scanner
(`internal/carriergate`) and in test harnesses — i.e. as strings a gate hunts
for. The consequence is in the epic's favour and must not be lost: **the
plumbing is not being built from nothing.** Every one of these sites already has
an exact typed source pointer and a payload type id, which is precisely the
argument `cross_clone_init` takes. What is empty is the cross SLOTS, not the
transport.

"Six barriers" is a count of BOUNDARIES, not of edits. They are **three shapes
over four MIR paths**, and they reopen **four** gates, not six:

- **capture (`on` and `spawn on`) — one shared path.** Sema classifies both
  through `classifyOnCapture` (`internal/sema/on_crossing_capture.go`), and MIR
  prepares both operands in one loop (`internal/mir/lower_expr_crossing.go`,
  the `captureConsume` line). `spawn on` is still the sharpest case: it returns
  `far Task<T>` and does not suspend the caller, so two shards hold
  independently-droppable refs. In the RUNTIME the same shape lands at three
  separate state-entry points — `rt_immediate_on.c`, `rt_remote_spawn.c` and
  `rt_immediate_on_anchored.c` — which is why a runtime-side barrier is six
  sites where MIR sees one.
- **`blocking { }` — its own path**, `internal/mir/lower_blocking.go`. Its only
  runtime touch point is `rt_value_cell_adopt` inside `rt_blocking_submit`, and
  that call sits **inside `rt_control_lock`**. The storage model forbids running
  a generated cross callback under an owner lock (section 5), so this one either
  splits the lock region or takes its barrier in the caller's frame. It is the
  single reason the barrier cannot be a uniform runtime change.
- **far channel send and the remote `select` SEND arm — one decision, two
  lowerings.** The arm has its own lowering (`lower_expr_select_far.go`) but no
  refusal of its own: `crossing_transport.go` admits a select unconditionally on
  the stated ground that "the arms' send payloads are plain-copy by channel
  construction", and the only thing enforcing that is the check at channel
  CREATION. So these two share a gate and must gain their barriers together.
- **crossing reply / `far Task.await()` — a TRANSFER, not a sharing.** The
  producing shard keeps no reference, so this edge may need only a handoff
  (`cross_move`) rather than a copy. Measured, not assumed: a bare `float`
  reply is definitely-lost ZERO at 1, 2 and 8 shards, while a COMPOSITE of
  floats still loses 32 blocks — which is why the gate stayed closed instead of
  reopening on a split rule.
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
3. ~~**BLOCKER to schedule, not discover.**~~ **GONE — verified 2026-09-04, and
   it was gone twice over.** This trap said `internal/sema/drop_obligations.go`
   SUPPRESSES drop obligations inside `on` / `spawn on` / `blocking` bodies
   pending RV2-DEBT-034, so that every bignum created inside a crossing body
   would leak the moment scalars became droppable. Neither half is true any
   more. **Crossings:** RV2-DEBT-034 CLOSED 2026-07-20 at the Epic 20 closeout,
   and the crossing suppression was lifted when that closure was found.
   **Blocking:** RV2-DEBT-080 CLOSED 2026-09-03 (Wave F, F5), and its a-3 step
   DELETED `dropObligationsSuppressed` outright, giving blocking bodies the same
   treatment `typeExprAsync` gives async ones. The file now states the result
   directly: *"There is no body whose obligations are suppressed any more."*
   Nothing has to land with or before Phase 1 on this account.
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
  RV2-DEBT-034 must be resolved by here — and it IS: closed 2026-07-20, with
  the blocking half following as RV2-DEBT-080 on 2026-09-03. See trap 3.
  **Landed so far: the LOCAL vertical.** A `float` value now carries a
  reference count end to end within one shard, and the reclamation gate
  (`TestRuntimeV2FloatReclamationValgrindZero`) is strict zero across
  reassignment loops, struct fields, array elements, `for ... in`, a function
  returning a fresh value, and a function returning its own parameter.
  Negative control: disabling the retain emission fails it.

  **It was not wired to any gate until 2026-09-04, and calling it "the gate"
  here is what hid that.** No `Makefile` target selected it; `make check`
  skipped it, because `SURGE_SKIP_TIMEOUT_TESTS` defaults to 1 and CI sets it
  to 1 explicitly; and the only thing that ran it was the nightly tagged sweep,
  which walks `./internal/vm` with no `-run` and therefore names nothing it
  runs. Meanwhile `runtime-v2-heap-check` carried a comment claiming every
  valgrind leak row in the repository ran from there — true of fifteen rows and
  false of this one. Both float rows are on that line now. The lesson is Global
  Rule 13's own: a test is not a gate until a named target selects it, and a
  document that calls it a gate does not make it one.

  Proven on the dedicated host at `8e5b4504`, from a detached worktree, before
  the wiring was believed: the row PASSES in 2.65 s as selected, and with its
  strict-zero assertion inverted in a scratch copy it FAILS (`exit 1`). A gate
  trusted for the first time is broken on purpose first — a row that passes
  both ways would have proven nothing about what it pins.

  The shape that fell out, and the rule that made it simple: **ownership
  transfers on a move, not on a copy.** A `string` parameter is owned because
  passing it moved it; a `float` parameter is BORROWED because passing it
  copied it and the caller kept both its binding and its reference for the
  whole call. So arguments never retain and parameters never drop — which also
  makes the arithmetic correct for free, since `a + b` lowers to a magic call
  whose runtime implementation takes `const void*` and frees nothing. A retain
  at argument sites would have leaked on every operation. Concretely:

  - `placeOperand` yields `OperandRetain` for a consuming read of a
    refcounted scalar (`internal/mir/lower_expr_helpers.go`); every MIR place
    holding one owns exactly one reference over its live range.
  - `newTemp` registers refcounted-scalar temps in the temp-drop frames
    (`internal/mir/lower.go`), because a call result arrives owning and a temp
    has no symbol for sema to hang an obligation on. The RETURN-value temp is
    the one exception (`newTransferTemp`): its reference leaves with the value.
  - `detachFromExitDrops` now accepts `OperandRetain`, so the returned value is
    materialized BEFORE the exit drops. Without that, `return z` emitted
    `drop z; return retain z` — a read of a released block.
  - Float literals materialize through an owned temp (`materializeOwnedConst`).
    Unlike an in-range `int`, a `float` literal is not a compile-time word:
    evaluating one ALLOCATES, so a bare const operand was a block with no owner
    and no release — a leak per evaluation, unbounded in a loop.
  - Field and element reads retain (`retainExtractedValue`): the read copies a
    bare word while the container keeps its own reference, so without this the
    temp's release would be a second one.
  - Drop glue releases refcounted-scalar fields (`emitDropValue`).
  - The LLVM retain is BRANCHLESS inline IR (`emit_refcount.go`): rather than
    branching over the NULL zero, it `select`s the read-modify-write onto a
    thread-local scratch word. That keeps a float copy straight-line and avoids
    splitting the enclosing basic block mid-expression. Release stays an
    out-of-line call — once per place at scope exit, and it has to branch into
    the destructor anyway.
  - MIR's `validate.go` drop-on-copy-local check gained the refcounted-scalar
    exception, one of the three legs Phase 0b flagged.

  **Cross-shard paths are CLOSED, not solved.** A non-atomic count is only
  sound while a block stays on one shard, and the barriers do not exist yet, so
  every path that would hand a second shard the same word is now REFUSED. This
  is a deliberate, temporary narrowing: before this work a float crossing
  compiled and leaked, and after the count landed it would have RACED. It
  reopens when the barriers land.

  Four gates, each with a diagnostic that names the real reason rather than
  saying "not plain-copy data" — which would be false, since `float` IS Copy:

  - reply / `far Task.await()` payload — `TriviallyTransportableBits`
  - `on` / `spawn on` capture — `classifyOnCapture`
  - `blocking` capture — `typeExprBlocking`
  - remote channel element — `crossingRecordExecutable` +
    `classifyCrossingPayload` at `CrossingLoweringChannelCreate`, so the
    diagnostic lands where the element type was chosen rather than at each send

  **The test is RECURSIVE** (`Result.ContainsRefCountedScalar`), and that is
  load-bearing rather than defensive: `@copy type P = { v: float }` is itself
  Copy — `@copy` requires all fields Copy, and `float` IS Copy — so it shipped
  as plain bits and carried the block one level down. Verified reachable before
  the fix. Unions are deliberately not walked: a union is not Copy, so it can
  only cross as an owned `@shard_movable` MOVE, which transfers references
  instead of sharing them.

  Gate: `TestRefCountedScalarCrossingsAreRefused` pins all four refusals, with
  `TestFixedWidthFloatStillCrosses` as the control — `float64` must keep
  crossing, and that row asserts the program compiles with NO errors, not just
  without this one code, so it cannot pass vacuously. (It did pass vacuously
  first: the fixed-width float is spelled `float64`, not `f64`, and every
  diagnostic's "use a fixed-width type" advice named a type that does not
  exist. Corrected.)

- **Reply-edge probe (done; result: NOT free).** The design note guessed the
  reply edge might need no copy because it is a transfer rather than sharing.
  Measured by temporarily reopening the gate and running 16 `on distributed`
  crossings under valgrind at 1/2/8 shards: **48 blocks leaked, 3 per
  crossing.** The transfer reasoning holds — nothing double-freed — but the
  crossing BODY leaked everything it built: both literals and the arithmetic
  result. Two causes, both now fixed and both wider than float:

  1. `ret` reached the block's exit by a goto WITHOUT flushing the temp-drop
     frames of the regions it skipped. `return` had always flushed; `ret` never
     did, because before refcounted scalars the only frame contents were
     sema-flagged owned temps, and `dropObligationsSuppressed` records none
     inside a crossing body — so the gap was invisible.
  2. The block-expression result slot was read with a retain while `ret` had
     already stored a retained reference into it, so the slot's own reference
     was never given back — one leak per block expression that yields a
     refcounted scalar.

  **The first fix is bounded on purpose, and the obvious version is wrong.**
  Flushing ALL open frames at `ret` segfaults: a frame opened BEFORE the block
  expression can hold a temp this path never materialized (the block's own
  result slot is exactly that), and releasing one reads an uninitialized word.
  Verified — SIGSEGV in `rt_bigfloat_release` on an address that was never
  allocated. `returnCtx` therefore records the frame depth at push, and `ret`
  flushes only the frames above it, which are entirely inside the block and so
  dominated by the `ret`. This is the invariant the head of
  `lower_temp_drops.go` states; the naive flush violated it.

  Still open after those fixes: **1 block per crossing**, from the union
  payload path on the CALLER side — see the next entry, which chased it.

- **Union payload path (partly fixed).** Chasing that residual found a
  USE-AFTER-FREE the reclamation vertical had introduced, not just a leak.
  `Option<float>` produced garbage and panicked with "numeric size limit
  exceeded" — the corrupted word being read as a bignum length. Bisected to the
  reclamation commit.

  Cause: **a tag constructor looks like a call but is a STORE.**
  `Some(x)` lowers to `call Some(...)`, so it took the "reference-counted
  scalar arguments BORROW" rule that is correct for real calls — and is what
  keeps `a + b` from leaking on every operation. But the union it builds keeps
  the payload past the call, so the producer's `drop` freed a block the union
  still pointed at:

      L4 = call __add(...)      ; rc=1
      L5 = call Some(copy L4)   ; union takes the word, no reference
      drop L4                   ; rc=0 -> FREED, union now dangles
      return move L6

  Fix: `calleeStoresArguments` marks a `symbols.SymbolTag` callee, and its
  arguments retain like struct-literal fields do. Gate:
  `TestRuntimeV2FloatUnionPayloadSurvivesItsConstructor`, whose sharpest check
  is the computed VALUE (a freed payload reads as garbage), with a negative
  control that reproduces the memcheck error.

  **Compare-arm payload bindings — FIXED, and the mechanism already existed.**
  The binding a compare arm introduces was never released. Three approaches
  were tried and two were wrong, which is worth recording because each failure
  named a real constraint:

  1. Registering it in a MIR temp-drop frame releases it at the end of its own
     LET STATEMENT, not its scope — the value is freed before the arm body
     reads it. Symptom: the program ran clean under valgrind and printed the
     WRONG ANSWER. A leak census alone would have called that a success.
  2. Wrapping the arm result in a synthesized `let` so the drops could run
     after it just moves the problem: that binding is synthesized after sema
     too, so it has no obligation either.
  3. What works: `ReturnData.DropsAfterValue` already carries exactly this
     contract — "free AFTER the return value evaluates (it may borrow them)
     and before the terminator". A compare arm's `ret` is a `StmtReturn` with
     `IsImplicit`, and MIR's implicit-return path **carried the field but
     never emitted it**. Normalization now attaches the payload bindings there
     and MIR emits them, one `emitExitDrops` call.

  Restricted to reference-counted scalars deliberately: those are Copy, so an
  arm can never move one out, and the binding's initialization retains — it is
  a genuine second owner. For every other droppable type a payload binding
  ALIASES storage the union still owns, so releasing it would be a double free.

  **Reply edge measured again after the fix: definitely-lost ZERO at 1, 2 and
  8 shards** for a bare `float` result over 16 crossings. The transfer argument
  holds and the edge is ready to reopen. A COMPOSITE result carrying floats
  (`@copy type P = { a: float, b: float }`) still leaks 32 blocks, so the gate
  stays closed rather than reopening on a split rule ("a float may cross but a
  struct of floats may not") that would then have to change again.

- **Phase 1 remainder — the six crossing barriers.** Install a deep copy
  (`rt_bigfloat_clone`, recursive for composites — the mirror of the drop-glue
  walk) at: `on`/`spawn on` captures, `blocking`, far channel send, crossing
  reply / `far Task.await()`, remote select SEND arms. Then reopen the four
  gates above. Note the reply edge is a TRANSFER rather than sharing (the
  producing shard keeps no reference), so it may need only the handoff barrier,
  while captures and sends are genuine sharing and need the copy.

- **Phase 2 — `int`/`uint`.** Adds only the fixnum-tag branch to a mechanism
  already proven by float — LOCALLY. Across a shard boundary it adds a
  question instead, and that question is open. See "Phase 2's scope question"
  below; do not start Phase 2 with it unanswered, because every answer implies
  a different amount of work and the wrong one is discovered late.
- **Phase 3 — inline arithmetic in IR.** Independent of this epic; see trap 5
  for the one constraint it places on Phase 1.

## Phase 2's Scope Question — ANSWERED 2026-09-04, variant (2)

**The owner chose (2): build the barriers for all three types first, then add
`int`/`uint` to a finished mechanism.** The recommendation below was (1); it is
left as written because the reasoning is what the answer was chosen against, not
because it stands. Two things the answer turns on, neither of them in the
argument below:

- Choosing (2) is not scope EXPANSION, it is this epic returning to its own
  Owner Decision 1 above, which already rejected "ship an atomic count and flip
  it later" in favour of doing the barrier work up front, and already recorded
  the barriers as **Phase 1** work.
- The estimate the recommendation leaned on is withdrawn: the owner called
  `PLAN.md`'s lane-days stale. Cost is no longer the discriminator.

**And a second ruling, on what the capability verdicts MEAN**, because the bits
could not simply be promoted: `evaluateShardMovable` and `evaluateCrossClonable`
(`internal/sema/capability_axes.go`) already answer TRUE for `float` with the
reason "plain bits", and a test pins it. A heap-backed value with a non-atomic
count is not plain bits.

- `ShardMovable(float)` STAYS true — V2 admits the crossing by exclusive `own`
  move, which hands over a single ownership obligation rather than sharing a
  counted block.
- `CrossClonable` means "possible **via deep clone**", not "raw bits are
  copyable". The pinned `"plain bits"` reason is replaced.
- Until `cross_clone` is wired recursively, the ABI flag is NOT set: there is no
  slot, no operation body, and no right to emit cross code.
- **Flag, descriptor, registry hash and `Dump` all derive from ONE backed
  state.** A late mask in `backedFlags` is refused: it would manufacture exactly
  the hidden divergence between a claim and the tree that this revision exists
  to remove.
- Promotion happens in ONE change per capability, after the operation is built
  and proven.

The consequence for ordering is that the move half and the clone half separate.
`ShardMovable` is legitimately true and its claim protocol already EXISTS
(`RT_SLOT_CLAIM_CROSS_MOVE`, `rt_slot_cross_move_failed_locked`, with the
claim-under-lock → unlock → apply → relock sequence demonstrated in
`internal/vm/testdata/slot_control_protocol_cases.c`). The clone half has no
`rt_slot_cross_clone_*` counterpart at all; that is built here.

---

**The question as it was put. Written 2026-09-04, when re-deriving this epic's
premises before starting Phase 2 found one of them gone.**

The count is non-atomic. Its soundness rests on no counted block being
reachable from two shards, and the two things meant to uphold that are a
module-level `let` ban (shipped) and a deep copy at every crossing (NOT
shipped — the six barriers under "Phase 1 remainder" are unbuilt, and
`README.md` said otherwise until this date; the evidence is in
`RV2-DEBT-038`'s 2026-09-04 note). What upholds it today is the fourth thing:
every crossing that would share such a block is REFUSED.

That refusal is affordable for `float` because a program that needs to cross
can spell `float64`. **It is not affordable for `int`.** So Phase 2 cannot
simply widen `IsRefCountedScalar` and inherit Phase 1's narrowing with it.

**The blast radius, counted rather than asserted — corrected 2026-09-04.** This
paragraph named five stdlib modules. The tree says **two**: `stdlib/bytes`
(`ByteRange = {start: uint, end: uint}`, and through its fields `ByteLine`,
`ByteSplit`, `ByteUint64`) and `core/sync.sg` (`BarrierState`, `Barrier`).
`stdlib/hash` is already fixed width (`Hash64 = {value: uint64}`, `Xxh64Lanes`
all `uint64`); so is `stdlib/time` (`Duration = {__opaque: int64}`) and
`stdlib/term` (`TermMods = uint8`, `KeyEvent`, `TermKey`). `term.TermEvent` does
carry a bare `int` in `Resize(int, int)`, but it is a UNION, and
`ContainsRefCountedScalar` deliberately does not walk unions — a union is not
Copy, so it can only cross as an owned `@shard_movable` move. Two modules, six
types.

The crossing fixtures are a weaker argument than they look, too. **No crossing
fixture uses `float` at all**, and the one this section quotes —
`@copy @shard_movable type Point = { x: int, y: int }`, in
`testdata/golden/crossing/block04/valid/locality_positive_copy_and_movable_on.sg`
— captures `own Point`, i.e. a MOVE, and an owned `@shard_movable` move is
exempt at every refusal point. The argument for (2) does not rest on this
paragraph; see the ruling above.

Three answers, with what each costs:

1. **Build the barrier for the heap half of `int`/`uint` only.** A fixnum is
   not a pointer and crosses as bits with no barrier at all, so only values
   beyond ±2^62 need a clone — a rare path, and one whose test is already
   load-bearing for representation. `float` keeps its refusal and the Phase 1
   remainder stays open as its own row. This is the narrowest slice that
   leaves the language usable, and it is closest to the ~5 lane-days
   `PLAN.md` estimates.
2. **Build all six barriers for all three types first, then add `int`/`uint`
   to a finished mechanism.** Architecturally cleaner and it lifts the `float`
   narrowing too. But the cross slots are empty — `cross_move_init` and
   `cross_clone_init` are `filledNowhere`, their bits refuse outright, and the
   runtime has no descriptor support — so this builds a mechanism from
   nothing, and the five-day estimate does not survive it.
3. **Close the local leak only, and do not widen
   `ContainsRefCountedScalar`.** Cheapest, and it reclaims what a single shard
   leaks. But a heap bignum that crosses then puts two shards on one
   non-atomic count, which is a RACE rather than a leak — strictly worse than
   today's leak — so this only holds if that path is refused separately, and
   naming which path that is turns out to be most of option 1's work anyway.

The recommendation is (1), on the grounds that it is the only one whose cost
matches the value: the leak is real and bounded, the barrier is needed only on
the rare heap path, and it does not spend an epic's budget building crossing
machinery that a later representation change might replace. But this is the
owner's call, because it decides how much of Epic 22 Phase 2 is Epic 22 Phase 2.

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
- **Heap-census recalibration is expected, and this list is longer than it
  used to be.** It named four tests; an enumeration on 2026-09-04 found at
  least fifteen pinning exact alloc/free deltas. That matters because the
  bullet exists as a warning, and a short list reads as a small blast radius.
  The four originally named: `TestRuntimeV2DropSelectSendArm`,
  `TestRuntimeV2DropScopeExit`,
  `TestLLVMNativeBufferedChannelAllocatesSingleBlock`, and the crossing
  censuses (`TestRuntimeV2CrossingHeapCaptureCensusBalanced`,
  `TestRuntimeV2CrossingStrictCensusBalanced`). The rest, all with the same
  kind of exact pin: `TestRuntimeV2DropComposite` (twelve windows),
  `TestRuntimeV2DropLeafReclamation` (thirteen),
  `TestRuntimeV2DropUnionCastReclamation`,
  `TestRuntimeV2IterProtocolReclamationCensusBalanced`,
  `TestRuntimeV2CompareScrutineeReleaseCensusBalanced`,
  `TestRuntimeV2CompositeBorrowReadDoesNotDuplicate`,
  `TestRuntimeV2TaskCohortCensus`, `TestRuntimeV2MapEntryCensusBalanced`,
  `TestRuntimeV2RangeForIntegerHead` and `TestRuntimeV2RangeForStoredValue`.
  Three more should be expected to move FIRST, because they pin the integer
  representation itself rather than a neighbour of it:
  `TestRuntimeV2FixnumHotLoopHeapBalanced`, `TestRuntimeV2FixnumBoundaryValues`
  (which pins 2^62-1 inline against 2^62 on the heap, and 2^63-1 against 2^63),
  and `TestRuntimeV2InRangeLiteralsFoldToFixnum`. When a delta moves, PROVE the
  change is balanced churn — allocations falling with frees, valgrind A/B
  byte-identical — before updating the number. Do not silence a census.
- **Known flakes, not yours:**
  `TestRuntimeV2RemoteTaskBehavior/shutdown-wakes-reply-waiters-on-all-shards`
  fails roughly 1 run in 8 on a clean tree, measured both ways.
  `TestRuntimeV2FarTaskCallerCancel/cancel_after_publication_before_first_poll`
  fails roughly 1 run in 10 with "missing far-body witness", at a varying shard
  count; reproduced on an unmodified worktree at the same rate 2026-07-24.
  Both are timing witnesses, and both surface under full-suite load far more
  often than in a dedicated run — check a suspected regression with `-count=10`
  on the test alone AND on a clean worktree before believing it.

## Defects Found While Building This (fixed, not part of the design)

- **Comparing a `float` against zero was inverted, in BOTH backends.** `bf_cmp`
  (`runtime/native/rt_bignum_float_core.c`) and `BigFloat.Cmp`
  (`internal/vm/bignum/float.go`) answered the all-zero case, then fell through
  to exponent ordering. Zero is carried as NULL/zero-value with exponent 0,
  while a normalized non-zero value scales its mantissa to the full 256 bits
  and so carries a large NEGATIVE exponent — so every positive value compared
  BELOW zero. `1.5 > 0.0` was false. The differential could not catch it
  because both backends had the identical bug, which is worth remembering: a
  VM/LLVM match proves agreement, not correctness. Found only because a leak
  witness used `acc > 0.0` as its liveness condition.

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
