# Ret Block Values Task Breakdown

**Goal:** Replace implicit value returns from brace-block expressions with explicit `ret`, while preserving short `=> expr` arms and preventing regressions like `sigil#1`.

**Approach:** Introduce `ret` as a dedicated block-exit statement first, then add migration warnings and autofixes for legacy implicit block values, migrate code, and only then remove the old implicit semantics. Keep every iteration independently verifiable with targeted tests plus `make check` and `make golden-check`.

**Skills:** `@brainstorming`, `@task-breakdown`, `@compiler-development`

**Tech Details:** Go compiler frontend (`internal/token`, `internal/parser`, `internal/sema`, `internal/hir`, `internal/mir`), golden snapshots under `testdata/golden`, driver/vm regression tests, `make check`, `make golden-check`

---

## Global Invariants

These must remain true while the work is in progress:

- Short-form `=> expr` arms keep their current semantics.
- `ret` and function `return` are represented separately in parser, sema, HIR, and MIR.
- `ret` never exits the enclosing function.
- `return` never becomes a block-result operator.
- `ret;` and `ret nothing;` are equivalent in typing and lowering.
- Discarded `compare` / `select` / `race` expressions must remain usable for pure control flow.
- Imperative arm blocks like `updated = true;` must not be forced into value-flow errors once they are discarded.
- Invalid block-value code must fail in diagnostics before VM/LLVM execution.

## Seed Example Corpus

Keep these examples available throughout the migration:

```sg
let x = { ret 1; };
let y = { ret; };
let z = { ret nothing; };
```

```sg
compare v {
    Some(x) => x;
    nothing => 0;
}
```

```sg
compare entry {
    VManyString(arr) => {
        arr.push(clone(s));
        updated = true;
    }
    finally => {}
};
```

```sg
let value = {
    work();
    ret 1;
};
```

```sg
let invalid = {
    work();
    1;
};
```

### Task 1: Lock Down Baseline Acceptance Cases

**Files:**
- Modify: `internal/driver/diagnose_test.go`
- Modify: `internal/vm/compare_diag_test.go`
- Create: `testdata/golden/sema/valid/compare_arm_control_flow_block.sg`
- Create: `testdata/golden/sema/valid/compare_arm_control_flow_block.ast`
- Create: `testdata/golden/sema/valid/compare_arm_control_flow_block.diag`
- Create: `testdata/golden/sema/valid/compare_arm_control_flow_block.fmt`
- Create: `testdata/golden/sema/valid/compare_arm_control_flow_block.tokens`
- Recheck: `testdata/golden/sema/invalid/compare_arm_block_missing_return.sg`

**Step 1: Write the control-flow regression test**

Add an inline-source driver test for the `sigil#1` shape:
- outer `compare` used as statement
- inner `compare` used as statement
- imperative arm block ending in assignment `updated = true;`
- `finally => {}`

Expected: no diagnostics.

**Step 2: Keep the existing value-flow failure test**

Verify that the existing `compare_arm_block_missing_return` failure remains covered for:
- `return compare ...`
- `let x = compare ...`

Expected: `SEM3015` still reports `compare arm type mismatch ... got nothing`.

**Step 3: Add a valid golden case**

Add `testdata/golden/sema/valid/compare_arm_control_flow_block.sg` with a minimal `sigil`-style imperative `compare` block.

Expected: empty `.diag`.

**Step 4: Run targeted checks**

Run:
- `go test ./internal/driver -run 'TestDiagnoseRejectsCompareArmBlockThatFallsThroughAsNothing|TestDiagnoseAllowsCompareArmControlFlowBlock' -count=1`
- `SURGE_STDLIB=/home/zov/projects/surge/surge go run ./cmd/surge diag testdata/golden/sema/valid/compare_arm_control_flow_block.sg`
- `SURGE_STDLIB=/home/zov/projects/surge/surge go run ./cmd/surge diag /tmp/ret_sigil_repro.sg`

Expected:
- the invalid value-flow case still fails
- the control-flow case passes cleanly
- the minimal `sigil`-style repro also passes cleanly

**Step 5: Run snapshot refresh**

Run:
- `make golden-check`

Expected: the new valid golden is generated and stable.

### Task 2: Introduce `ret` Token And AST Statement

**Files:**
- Modify: `internal/token/kind.go`
- Modify: `internal/token/kind_string.go`
- Modify: `internal/token/keywords.go`
- Modify: `internal/token/token.go`
- Modify: `internal/lexer/lexer_test.go`
- Modify: `internal/token/keyword_test.go`
- Modify: `internal/ast/stmt.go`
- Modify: `internal/parser/stmt_parser.go`
- Modify: `internal/parser/stmt_parser_test.go`

**Step 1: Add keyword plumbing**

Add token support for `ret` and mark it as a keyword.

Expected: lexer/token tests can recognize `ret`.

**Step 2: Add AST support**

Add a dedicated AST statement kind and payload for block-return.

Expected: parser can build an AST node for:
- `ret 1;`
- `ret;`
- `ret nothing;`

**Step 3: Parse `ret` statements**

Teach statement parsing to accept `ret` only in statement position, with optional expression.

Expected:
- `ret;` parses
- `ret 1;` parses
- `ret nothing;` parses

**Step 4: Add parser tests**

Add parser tests that verify the new statement shape and payload.

**Step 5: Run targeted checks**

Run:
- `go test ./internal/token ./internal/parser -count=1`

Expected: parser/token layer is green before touching sema/HIR.

**Invariant after Task 2**

- `ret` is syntactically available, but old implicit block-value code still parses.

### Task 3: Thread `ret` Through HIR And MIR

**Files:**
- Modify: `internal/hir/stmt.go`
- Modify: `internal/hir/lower_stmt.go`
- Modify: `internal/hir/print.go`
- Modify: `internal/mir/lower_expr_cf.go`
- Modify: `internal/mir/lower_expr_select.go`
- Modify: `internal/mir/*_test.go`

**Step 1: Add HIR stmt kind for block-return**

Represent `ret` separately from function `return`.

Expected: HIR can distinguish:
- function exit
- block expression exit

**Step 2: Lower AST `ret` to HIR**

Update lowering so `ret` survives into HIR without being rewritten into function `return`.

**Step 3: Lower HIR `ret` to MIR control flow**

Teach MIR lowering for block expressions to jump to the enclosing block exit and place the result value.

Expected:
- `ret expr;` exits the current block expression
- `ret;` and `ret nothing;` exit with `nothing`

**Step 4: Add MIR/CF tests**

Test:
- nested block return
- `compare` arm block with `ret`
- `let x = { ret 1; };`

**Step 5: Run targeted checks**

Run:
- `go test ./internal/hir ./internal/mir -count=1`

Expected: `ret` works through lowering without any sema changes yet.

**Invariant after Task 3**

- lowering can distinguish block exit from function exit even if sema still allows legacy implicit block values.

### Task 4: Add Sema Block-Result Context

**Files:**
- Modify: `internal/sema/type_checker_core.go`
- Modify: `internal/sema/type_checker_returns.go`
- Modify: `internal/sema/type_expr_block.go`
- Modify: `internal/sema/type_expr_flow.go`
- Modify: `internal/sema/type_expr_compare.go`
- Modify: `internal/sema/type_expr_select.go`
- Modify: `internal/sema/check_test.go`
- Modify: `internal/driver/diagnose_test.go`

**Step 1: Add explicit block-result context**

Introduce sema context that tracks expression-block result expectations separately from function returns.

Expected: `return` and `ret` are no longer conflated.

**Step 2: Validate `ret` context**

Rules:
- `ret` allowed only inside block expressions used in value position
- `ret` outside such blocks is a diagnostic
- `ret;` and `ret nothing;` type as `nothing`

**Step 3: Type-check block expressions through `ret`**

Make block expression result type come from explicit `ret` exits, not from generic return collection.

**Step 4: Add sema tests**

Cover:
- `let x = { ret 1; };`
- `let x = { ret; };`
- `let x = { ret nothing; };`
- `return` inside block still returns from function
- `ret` outside block expression is rejected

**Step 5: Run targeted checks**

Run:
- `go test ./internal/sema ./internal/driver -count=1`

Expected: sema understands `ret` before migration warnings are added.

**Invariant after Task 4**

- any accepted block value must be explainable by explicit `ret` or still-temporary legacy block-value rules, never by VM/LLVM accident.

### Task 5: Add Migration Warning And Autofix For Legacy Implicit Block Values

**Files:**
- Modify: `internal/parser/expression_block.go`
- Modify: `internal/sema/alien_hints.go`
- Modify: `internal/diag/codes.go`
- Modify: `internal/driver/diagnose_test.go`
- Create/Modify: `testdata/golden/sema/*` warning cases

**Step 1: Detect legacy implicit value blocks**

Detect brace-blocks in value-position whose value comes only from legacy tail-expression rules.

Expected: warning only for truly value-producing legacy blocks.

**Step 2: Add fix suggestion**

Attach safe fix:
- insert `ret ` before the legacy tail expr

Example:
- `{ foo(); 1; }` -> `{ foo(); ret 1; }`

**Step 3: Protect control-flow blocks**

Do not warn on:
- `updated = true;`
- trailing call used for side effects
- blocks already using `ret`

**Step 4: Add warning goldens**

Add golden warning cases for:
- `let x = { foo(); 1; };`
- `compare x { A => { foo(); 1; }; ... }`

**Step 5: Run targeted checks**

Run:
- `go test ./internal/driver -count=1`
- `make golden-check`

Expected: warnings and fixes are deterministic.

**Invariant after Task 5**

- every remaining implicit block value in `surge` or `sigil` is observable as a warning with a reviewable fix.

### Task 6: Migrate Surge Code To `ret`

**Files:**
- Modify: all warning sites found by `surge diag .`
- Recheck: `docs/LANGUAGE.md`
- Recheck: `docs/LANGUAGE.ru.md`

**Step 1: Enumerate warning sites**

Run:
- `SURGE_STDLIB=/home/zov/projects/surge/surge go run ./cmd/surge diag .`

Capture every migration warning.

**Step 2: Apply autofix or manual edits**

Convert each true value block to explicit `ret`.

**Step 3: Re-run diagnostics**

Expected: no remaining legacy implicit block value warnings in `surge`.

**Step 4: Run full checks**

Run:
- `make check`
- `make golden-check`

Expected: tree is green after migration.

**Invariant after Task 6**

- `surge` no longer depends on implicit block values.

### Task 7: Migrate Sigil And Lock Cross-Repo Behavior

**Files:**
- External repo: `sigil`
- Recheck: `parse.sg` and any new warning sites

**Step 1: Run diagnostics in `sigil`**

Run:
- `surge diag .`

**Step 2: Convert value blocks to `ret`**

Only migrate genuine value blocks.

Expected:
- the old `sigil#1` control-flow block remains without `ret`
- any genuine block values become explicit

**Step 3: Re-run diagnostics**

Expected: `sigil` is clean before semantics flip.

**Invariant after Task 7**

- both `surge` and `sigil` are migration-clean before removing the old semantics.

### Task 8: Remove Legacy Implicit Block Value Semantics

**Files:**
- Modify: `internal/parser/expression_block.go`
- Modify: `internal/sema/type_expr_block.go`
- Modify: `internal/sema/type_expr_flow.go`
- Modify: `internal/sema/type_expr_compare.go`
- Modify: `internal/sema/type_expr_select.go`
- Modify: `docs/LANGUAGE.md`
- Modify: `docs/LANGUAGE.ru.md`
- Update: `testdata/golden/*`

**Step 1: Remove semantic dependence on implicit tail expr**

Brace-blocks in value context no longer derive value from trailing expression.

**Step 2: Keep `ret` as the only block-value exit**

Expected:
- `{ foo(); ret 1; }` remains valid
- `{ foo(); 1; }` no longer behaves as value-return
- imperative blocks default to `nothing`

**Step 3: Revalidate `#53` and `sigil#1`**

Expected:
- `#53`-style compare-arm value fallthrough is rejected
- `sigil#1`-style control-flow compare compiles cleanly

**Step 4: Run final checks**

Run:
- `make check`
- `make golden-check`
- `SURGE_STDLIB=/home/zov/projects/surge/surge go run ./cmd/surge diag .`
- `surge diag .` inside `sigil`

Expected: final language semantics are stable and fully migrated.

**Invariant after Task 8**

- explicit `ret` is the only path for brace-block values.
