# Epic 19 Task 3: Scope-Exit Synthesis on Leaf Types — DESIGN

## Architecture: sema computes, HIR carries, MIR emits

Move truth lives in ONE place — sema's move tracking (`movedBindings`,
`observeMove`, the join snapshots). Re-deriving moved-ness in HIR or
MIR would duplicate that logic and drift. The pipeline handoff:

1. **Sema** records drop obligations into `sema.Result` while walking
   (it already holds moved state at every program point):
   - `ScopeEndDrops map[ast.StmtID][]symbols.SymbolID` — keyed by the
     `ast.StmtBlock` whose scope ends; droppable bindings declared in
     that scope (plus, for a function body block, the droppable
     params), live at the normal end, reverse-declaration order.
   - `EarlyExitDrops map[ast.StmtID][]symbols.SymbolID` — keyed by
     return/ret/break/continue; live droppables in every scope being
     exited (all scopes for return; out to the innermost loop body for
     break/continue), innermost-first.
   - `ReassignOldDrops map[ast.ExprID]symbols.SymbolID` — keyed by the
     assign expr; set when the target is a whole droppable binding
     still live AFTER the RHS evaluated. The approved suppression
     falls out for free: `x = f(x)` marks x moved during RHS
     evaluation, so x is not live and no old-drop is recorded.
   - Whole-binding assignment REVIVES a moved binding
     (`clearBindingMoved`) — without this, `eat(s); s = make();`
     would leak the new value at scope end.
2. **HIR lowering** (has `semaRes` + AST ids, the last stage that has
   both) applies the obligations:
   - appends synthesized `StmtDrop` statements at block ends;
   - inserts `StmtDrop` statements BEFORE break/continue (no payload
     change needed — they carry no value);
   - returns carry their list ON the statement
     (`ReturnData.DropsAfterValue`): the drops must run AFTER the
     return value evaluates (`return read_len(&a)` reads `a`), and
     nothing can be inserted after a return;
   - assigns carry `AssignData.DropOverwritten`.
3. **MIR lowering** emits: `StmtDrop` already lowers to `InstrDrop`
   (Task 2); returns emit the carried drops between value evaluation
   and the terminator; assigns emit
   eval RHS → `InstrDrop(target)` → store — the approved order.
4. **mono** clones stmt payloads by struct copy (`clone.go:60`), so
   the new value fields survive monomorphization unchanged.
5. **Backend** is already done (Task 2): strings and dynamic arrays
   free; views defer through the registry; everything else stays a
   no-op until the Task 4 glue — synthesis is type-agnostic, emission
   is leaf-gated.

`@drop` composes with no special casing: since Task 2 it CONSUMES the
binding (sema marks it moved), so an explicitly dropped binding is
simply not live at any later exit point.

## Rejections (both land in sema, kindness-first)

- **Partial-path moves** (approved semantics): at the if/compare
  merges (`type_checker_walk.go:305-331`, `type_expr_flow.go:89-103`)
  the symmetric difference of the branch moved-sets, restricted to
  droppable bindings, is an error — "moved on some paths but not
  others; a droppable value needs one fate — move it on every branch
  or none." Early-exit branches keep their existing non-divergent
  merges (a branch that returns does not flow to the join and stays
  accepted).
- **Loop back-edge moves**: a binding declared OUTSIDE a loop, moved
  INSIDE its body, is use-after-move on the second iteration. The
  while/for walkers snapshot before the body and reject
  outside-declared bindings in (after \ before). Conservative on
  `while c { eat(s); break; }` (always-breaks) — recorded as an
  accepted bound, the diagnostic names the loop.

## Droppable (vertical-1 definition)

Non-copy, non-reference, non-raw-pointer binding from a SIMPLE `let`
or a by-value function param. Excluded in this task (documented, not
silent): tuple-destructuring patterns, for-in loop variables (element
extraction semantics need their own evidence pass), and
crossing-generated bodies (shipped-state ownership is vertical 2 /
DEBT-034 — synthesis must not race the runtime's release path).

## Shadowing (rows 4-5) needs no special code

`let s = make(); let s = make();` declares two SYMBOLS; both register
in the scope; neither is moved → two drops. `let s = transform(s);`
moves the first symbol during the second's initializer → one drop.

## Statement-end temporaries (increment C — after A+B green)

`let s = a + b;` leaves the operand temporaries caller-owned
(`rt_string_concat(a: &string, b: &string)` — evidence: borrows, not
consumption), and loop row 6 (condition temporaries) is temp-shaped.
Bindings-first is deliberate: temporaries need their own design pass
(sema-computed unconsumed-rvalue lists + synthetic bindings, vs
MIR-side consumption tracking) and must not block the binding
vertical. Census rows for A+B keep Task 2's free-count currency
(exact `free_count` deltas over drop-only windows), which is immune
to temp leakage; the alloc/free BALANCE gate arrives once temps drop.

## Increments

- **A — sema**: obligations + the two rejections + assignment revive;
  unit rows in internal/sema.
- **B — HIR/MIR**: carry + emit; e2e rows (free-count currency):
  implicit scope-end drop (string/array/view), early-return drops,
  break/continue drops (row 2), per-iteration loop balance (row 1),
  shadowing (rows 4-5), reassignment drop + suppression, param drop
  at callee exit, moved-binding-never-drops.
- **C — statement-end temporaries**: design + rows 6 and the balance
  windows (may open its own fork note).

Full `make check` after each increment — synthesis touches every
compiled function; the existing suite is the regression net.

## Increments A+B: SHIPPED (2026-07-14)

- **Sema** (`drop_obligations.go` + walker wiring): the three Result
  maps; partial-path rejection at if-joins and compare-arm merges
  (union-vs-intersection, reported once per binding at the move span,
  SEM3136); loop back-edge rejection for while/for/for-in (snapshot
  taken before the CONDITION — it re-evaluates every iteration);
  branch-local and body-local moves are exempt via the "still on the
  scope stack" guard (movedBindings persists past scope ends by
  design). Params (incl. by-value self) register at both fn-walk
  sites. Unit rows in `drop_obligations_test.go`.
- **HIR** (`lower_drops.go`): synthesized `StmtDrop` at block ends and
  (wrapper-block) before break/continue; classic-for init-scope drops
  wrap AFTER the loop statement (both the cond-false edge and every
  break land there); returns carry `ReturnData.DropsAfterValue`;
  assignments carry `BinaryOpData.DropOverwritten`. mono's struct-copy
  cloning preserves both fields unchanged.
- **MIR**: `emitExitDrops` between return-value evaluation and every
  TermReturn; assign lowering emits `InstrDrop(dst)` between RHS
  evaluation and the store (the approved order).
- **E2E** `TestRuntimeV2DropScopeExit` (11 rows, SURGE_THREADS=1,2,
  wired into runtime-v2-heap-check): scope-end (string+array),
  early-return, param-move, shadowing x2, reassign + suppression,
  view-before-base order (pinned by the deferral counter, not just
  counts), per-iteration/break/continue loop rows (twin-differential
  measurement), generic-push double-free regression.

### Discoveries (they bit)

1. **Same-scope shadowing does not exist** — `let s = ...; let s =
   ...;` is "duplicate declaration". Epic rows 4-5 assumed Rust-style
   shadowing; they are re-pinned as NESTED-scope shadowing (inner
   scope drops its own; move-through-initializer drops once).
2. **Moves are NOT observed through generic calls** (pre-existing
   hole): `outer<T>(v: T) { inner(v); }` leaves v "live" in outer.
   With synthesis this became a REAL double free (push -> array_push
   both dropping the same string handle; caught by a glibc abort in
   the probe). Vertical-1 bound: generic-typed bindings
   (`types.ContainsGenericParam`, now exported) are not droppable;
   concrete instantiations drop at concrete call sites. The honest
   fix (arg-ownership through generic signatures) is a prerequisite
   for Task 4's element-recursive glue.
3. **Calibrated census noise**: `a.push(1)` = +2 frees, `a.slice(..)`
   = +1 (bignum/range unboxing — pre-existing); `if` statements and
   int reassignment allocate/free bignum scratch, so loop rows
   measure against droppable-free TWINS and assert the difference.

### Remaining in Task 3

- **Increment C — statement-end temporaries** (loop row 6, the
  alloc/free balance gate): design below.

## Increment C design: statement-end temporaries

The leak: owned rvalues that no binding ever owns. `let s = "a" + "b";`
heap-allocates BOTH literal operands; `__add(self: &string, other:
&string)` only borrows them (evidence: core/intrinsics.sg:700), so
after the statement they are garbage nothing frees. Same for borrowed
call arguments (`use(&make())`), discarded results (`make();`), and
condition temporaries in loops (row 6: they must free per-iteration).

**Split (same shape as A+B — one move truth):**

1. **Sema classifies.** A map of flagged exprs (`TempDrops
   map[ast.ExprID]struct{}`): expr produces an OWNED rvalue (non-copy,
   non-reference result of a call / magic op / string literal /
   slice-view expression — never a place read: var refs, field reads,
   element reads are owned by their containers) AND is never CONSUMED.
   Consumption sites, all already walked by sema: move into a binding
   (`let`/assign RHS), return value, by-value call argument
   (`applyParamOwnership`'s non-`&` path — consumption marks at
   `observeMove` entry, before the place-resolution bail), AND
   aggregate literal positions (struct fields, array elements, tuple
   elems) — the aggregate takes ownership. Aggregate positions are
   consumption-only in this increment (no new use-after-move
   rejections from struct literals; that hole is recorded with the
   generic-call one as a Task 4 prerequisite). Borrows never consume.
2. **HIR wraps.** Flagged exprs lower wrapped in a marker node
   (`ExprOwnedTemp{Inner}`) — HIR lowering is the last stage holding
   ast.ExprIDs. mono clone + print gain the case.
3. **MIR scopes and frees.** Lowering an `ExprOwnedTemp` materializes
   the inner operand into a temp local and registers it in the current
   TEMP FRAME; frames flush (emit `InstrDrop` per registered temp,
   reverse order) at region ends: statement end, and — critically for
   loop row 6 — right after a loop condition's value materializes
   (the header block re-runs per iteration, so cond temps free per
   iteration). Return statements flush temps BEFORE the carried
   scope-exit drops (inner lifetime first), after the return operand
   detaches.

**Safety spine:** a misclassified producer (flagging a place read)
would be a double free; a missed consumer (aggregate fields) would be
a use-after-free through the aggregate. The VM backend's dropped-slot
checks scream on both (it caught the double-self in B), and the
balance rows only go green when classification is exact — `let s =
"a" + "b";` must show allocs == frees across the whole call for the
first time.

### Second-opinion verdict (codex, 2026-07-14): NO-GO as drafted,
### GO-WITH-CHANGES — the resolved shape below supersedes the draft

The review surfaced three shipping-grade holes in the draft and the
resolution for each:

1. **Control-flow arms leak ownership OUT of the classification.**
   `let s = if c { make() } else { make() };` — the let consumes the
   IF node; the arm results would stay flagged and drop at statement
   end while s points at one of them (plain UAF). Resolution: arm
   results / block tails / compare arms TRANSFER to the outer
   control-flow expression (unflagged); the outer expression itself
   becomes the producer and lives or dies by ITS consumption. A
   discarded `if c { make() } else { make() };` then drops the JOIN
   result once — single-entry, dominated, correct.
2. **Consumption is static, so deregistration dissolves.** The draft
   flagged at production and would have needed deregister-on-consume
   (double-free risk). Resolved model: sema publishes the FINAL set —
   an expression has exactly one syntactic use position, so
   "produced-owned and never consumed" is decidable at walk time;
   MIR registers only truly-dangling evaluations and never sees a
   consumed one.
3. **Conditional materialization vs the VM's uninit-drop error.**
   LLVM allocas are NOT zero-initialized and the VM errors on
   dropping an uninitialized slot, so flushing at a join after a
   conditionally-evaluated region (short-circuit RHS, arm bodies) is
   wrong on the skipped path. Resolution: temp frames push/flush
   INSIDE each single-entry evaluation region — statement, loop/if
   condition (after the value materializes, before the branch; loop
   headers re-run per iteration, satisfying row 6), short-circuit
   RHS block, if/compare arm bodies, and returns (before the carried
   binding drops). Every flush is dominated by its materializations;
   no active bits needed.

Bounds recorded for this increment (safe direction = leak, never
free): assign-shaped expressions are never producers (their MIR
result is the assigned PLACE); explicit `&temp` operands are
unflagged (an explicit borrow can escape the statement — including
the `&"literal"` shape that handleBorrow deliberately skips);
implicit call-argument borrows cannot escape and stay reclaimed;
statements containing await/select/spawn/crossing/blocking constructs
are tainted and flag nothing (temp lifetimes across suspension spill
state — that machinery belongs to the crossing vertical); HIR-only
generated calls (for-in ranges) stay out. Aggregate literal positions
consume without new use-after-move rejections — recorded with the
generic-call move hole as the Task 4 prerequisite pair.
- Excluded and documented: tuple-destructuring patterns, for-in loop
  variables, block-expression `ret` paths (safe direction: values
  live to the ENCLOSING scope end or leak; never double-free),
  compound-assign old-value drops, module-level lets, crossing and
  blocking bodies (RV2-DEBT-034 vertical).

## Increment C: SHIPPED (2026-07-14)

Implemented per the post-review shape: sema publishes the final
unconsumed-owned set (`Result.TempDrops`, classification in
`temp_drops.go` — producers at typeExpr's single exit; consumption at
observeMove entry, aggregate literal positions, ternary/compare-arm
transfers, explicit-& escape; suspension statements taint their
frame); HIR wraps flagged evaluations in `ExprOwnedTemp`; MIR
materializes into region-scoped temp frames (`lower_temp_drops.go`)
flushed at statement ends, loop/if condition value points
(per-iteration for loop headers — row 6), short-circuit RHS blocks,
if-expr arm bodies, and before return terminators. A temp used in
place position (method receiver on a fresh value) materializes
through `lowerPlace`'s OwnedTemp case.

Pipeline traversals that had to learn the node: HIR normalize, all
four mono traversals (clone/collect/subst/var-refs), and
`remapHIRModule` — where the fix ALSO uncovered an A+B latency: the
remap recursed into neither `OwnedTempData.Inner` (stale symbol IDs,
hard MIR error) nor `ReturnData.DropsAfterValue` (the lenient lookup
in `emitExitDrops` silently skipped unmapped drops — core functions'
return-path binding drops were quietly not firing until now).

Evidence pinned: `rt_string_concat` is a FLAT copy (memcpy into one
fresh allocation, rt_string.c:424) — no rope, dropping operand temps
is sound. HeapStats snapshots allocate (~5 boxes); balance windows
calibrate that noise with an empty-call probe and assert
`allocs - noise == frees` exactly.

### Second review round (codex, resumed): GO-WITH-CHANGES — mapping

- **"Register after materialization"**: holds by construction —
  `lowerOwnedTempExpr` registers the temp with the same instruction
  sequence that materializes it; a skipped evaluation never
  registers. This subsumes region-enumeration completeness.
- **CFG exit edges incl. divergence**: returns flush open frames
  after the operand detaches. The diverging-argument case is
  unconstructible: return/break/continue are STATEMENTS, there is no
  try/`?` operator, and panic is `_exit(1)` (no unwinding — the
  epic's fixed point; leak-on-panic by design).
- **Single-owner / transfer**: dissolved statically — consumed
  evaluations are never in any frame; nothing to transfer or
  deregister.
- **Dominance over null-init**: dominance chosen, keeping the VM
  sanitizer's teeth — which immediately paid off (below).
- **Async suspension temps**: deferred by the statement taint (leak,
  never a free) — same boundary as crossing bodies / DEBT-034.

**A finding neither review round predicted, caught by the VM
sanitizer on an existing row — and the concrete reason the final-set
model replaced per-expression producer classification:** control-flow
expressions can forward a PLACE. A compare arm `{ out = out + "x"; }` yields the assignment's
value — the target binding's LIVE handle — so flagging the discarded
compare as a producer dropped `out`'s own value (a UAF through the
binding under LLVM). Resolution: ternary/compare/block expressions
are NEVER producers; fresh values built in arms leak when the outer
value is discarded (safe direction, recorded with the other bounds).
The VM also learned that dropping a MOVED slot is a no-op rather than
a panic (its move flags are coarser than sema's borrow-aware
tracking: non-copy call-argument reads flag as moves), mirroring the
LLVM null-store contract while keeping the double-drop panic.

Recorded for a future MIR verifier pass: "every InstrDrop of a temp
is dominated by that temp's registration" is the single cheap check
that would catch a regression in any of the above — the VM sanitizer
only catches what a test happens to exercise.

## Status

A+B+C SHIPPED. E2e `TestRuntimeV2DropScopeExit` carries 16 rows at
SURGE_THREADS=1,2 — scope ends, early returns, params, shadowing,
reassignment, view ordering (deferral counter), loop rows 1-2 and 6,
generic-push regression, and the first alloc/free BALANCE windows
(concat operands, discarded results, implicitly borrowed temps) —
wired into runtime-v2-heap-check. Full `make check` green.
