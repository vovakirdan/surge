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
  alloc/free balance gate): needs its own design pass.
- Excluded and documented: tuple-destructuring patterns, for-in loop
  variables, block-expression `ret` paths (safe direction: values
  live to the ENCLOSING scope end or leak; never double-free),
  compound-assign old-value drops, module-level lets, crossing and
  blocking bodies (RV2-DEBT-034 vertical).
