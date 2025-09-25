# Surge Parser Hardening Plan

The work will be shipped in focused iterations. Each iteration lists the goals, the
relevant modules/files, and concrete subtasks. Use `LANGUAGE.md` (especially §§3–4,
§9.2, §14, §17) and the examples under `examples/` as the source of truth for syntax
and behaviour.

---

## Iteration 1 — Diagnostics & AST groundwork

**Goals**
- Expand `ParseCode` to cover the new error surface.
- Ensure diagnostics are exported by `surge-diagnostics` with stable codes/messages.
- Extend the AST to support upcoming constructs (top-level `let`, parallel expressions,
  richer `TypeNode`).

**Files**
- `crates/parser/src/error.rs`
- `crates/diagnostics/src/collect.rs`
- `crates/parser/src/ast/nodes.rs`

**Steps**
1. Add the new variants listed in the acceptance criteria to `ParseCode` and update
   `ParseDiag::new` usages if needed.
2. Map every new variant in `diagnostics::from_parser_diags` to its `PARSE_*` string.
3. Update the AST:
   - Allow top-level `let` items (e.g. `Item::Let` wrapping a `Stmt::Let`).
   - Introduce `Expr::ParallelMap`, `Expr::ParallelReduce`.
   - Rework `TypeNode` to store a span plus a textual representation resilient to
     missing source text.
4. Adjust any derived traits or helper functions invalidated by the new AST shapes.

**Outcome**: the rest of the iterations can emit/inspect the new diagnostics and
populate the AST correctly.

---

## Iteration 2 — Attribute parsing overhaul

**Goals**
- Parse attributes as `@` + identifier (+ optional string literal for `backend`).
- Validate allowed attribute names; emit `PARSE_UNKNOWN_ATTRIBUTE` otherwise.

**Files**
- `crates/parser/src/parser.rs` (attribute parsing sections)
- `docs/LANGUAGE.md` (§4.2)
- `examples/` (attribute usage in `ok/10_attributes.sg`)

**Steps**
1. Replace the current keyword-based lookups with a parser that consumes `At` tokens
   followed by `Ident`/`StringLit` as required.
2. Build `Attr` values (`Pure`, `Overload`, `Override`, `Backend`). Validate that
   backend strings are `"cpu"` or `"gpu"`; otherwise emit `UnknownAttribute`.
3. Ensure attributes propagate to `FuncSig.attrs` exactly once and that attribute
   parsing recovers gracefully when malformed (skip to the next `@` or `fn`).

---

## Iteration 3 — Function signatures & type nodes

**Goals**
- Make the return type optional (`RetType?`).
- Emit `PARSE_EXPECTED_TYPE_AFTER_ARROW` when `->` lacks a type.
- Guarantee `TypeNode` has usable text even in `parse_tokens()`.

**Files**
- `crates/parser/src/parser.rs` (`parse_fn`, `parse_type_node`)
- `crates/parser/src/lexer_api.rs` (helpers for span → text fallback)

**Steps**
1. Adjust `parse_fn` so absence of `->` is valid; only diagnose when `ThinArrow`
   appears without a type.
2. Rework `parse_type_node` to capture token slices and populate both `span` and
   `repr` (concatenate lexemes when `Stream::src` is `None`).
3. Update all call sites that expect `TypeNode::text` to use the new structure.
4. Expand tests with functions that omit return types.

---

## Iteration 4 — Statement parsing & top-level lets

**Goals**
- Enforce `let` grammar: require `=`; error `PARSE_LET_MISSING_EQUALS` otherwise.
- Allow module-scope `let` declarations.
- Ensure signal statements require `:=` (`PARSE_SIGNAL_MISSING_ASSIGN`).
- Implement `AssignmentWithoutLhs` and `UnexpectedPrimary` checks.

**Files**
- `crates/parser/src/parser.rs` (`parse_item`, `parse_let_stmt`, `parse_signal_stmt`,
  expression parsing and assignment handling)
- `examples/` (global variables, signals)

**Steps**
1. Update `parse_item` to recognise top-level `let` and push `Item::Let` nodes.
2. Modify `parse_let_stmt` to demand `=`; emit the new diagnostic when missing.
3. Modify `parse_signal_stmt` to accept only `ColonEq` and diagnose otherwise.
4. Implement sanity checks before creating `Expr::Assign` to ensure the left side is
   an identifier, index, or other assignable form; emit `PARSE_ASSIGNMENT_WITHOUT_LHS`.
5. Detect invalid primaries up front and produce `PARSE_UNEXPECTED_PRIMARY` with
   recovery to the next synchronisation point.

---

## Iteration 5 — `for` loops (C-style and for-in)

**Goals**
- Cover both loop syntaxes per §3.2.
- Emit specific diagnostics when mandatory pieces are missing.

**Files**
- `crates/parser/src/parser.rs` (`parse_for_stmt`, helper routines)
- `crates/parser/src/ast/nodes.rs` (`Stmt::ForIn` already present)
- `examples/ok/06_control_flow.sg`

**Steps**
1. In `parse_for_stmt`, branch by checking for `(` vs `Ident`/pattern start.
2. Implement `for-in` parsing (`pat : Type in Expr Block`) with precise spans.
3. Emit `PARSE_FORIN_MISSING_COLON`, `PARSE_FORIN_MISSING_TYPE`, `PARSE_FORIN_MISSING_IN`,
   and `PARSE_FORIN_MISSING_EXPR` where appropriate.
4. Retain the existing C-style parsing, but ensure semicolons separate the three
   components even when expressions are absent.
5. Update tests to cover valid/invalid loops.

---

## Iteration 6 — Parallel map/reduce expressions

**Goals**
- Parse `parallel map` and `parallel reduce` constructs according to §9.2.
- Restrict `=>` usage and introduce new diagnostics for malformed headers.

**Files**
- `crates/parser/src/parser.rs` (likely in `parse_prefix` or dedicated helper)
- `crates/parser/src/ast/nodes.rs` (`Expr::ParallelMap`, `Expr::ParallelReduce`)
- `examples/ok/09_generics.sg`, `examples/ok/20_comprehensive_test.sg`

**Steps**
1. When encountering `Keyword(Parallel)`, parse the subsequent `map`/`reduce` flow,
   including optional sequence expression, `with`, init expression (reduce only),
   parameter list, and lambda body.
2. Allow only these constructs to consume `FatArrow`; elsewhere produce
   `PARSE_FATARROW_OUTSIDE_PARALLEL` and recover.
3. Emit `PARSE_PARALLEL_MISSING_WITH`, `PARSE_PARALLEL_MISSING_FATARROW`, and
   `PARSE_PARALLEL_BAD_HEADER` for malformed sequences.
4. Populate the new AST nodes and ensure spans cover the whole construct.

---

## Iteration 7 — Extern blocks & recovery helpers

**Goals**
- Parse `extern<Type> { ... }` minimally per §4.4.
- Improve block recovery with terminator support.
- Flag array literal syntax errors involving stray semicolons.

**Files**
- `crates/parser/src/parser.rs` (`parse_item`, `parse_extern_block`, `parse_block`,
  array literal parsing)
- `examples/ok/14_extern_methods.sg`

**Steps**
1. Add a parser branch for `extern` items that expects `<Type>` and a block, with
   diagnostics (`ExternGenericBrackets`, `ExternMissingType`, `ExternUnclosedBlock`).
2. Enhance `parse_block` to accept additional terminator tokens (e.g. `else`),
   stopping recovery when they are reached.
3. In array literal parsing, treat encountering `;` before `]` as
   `PARSE_INVALID_ARRAY_SYNTAX` and resynchronise to `]`.

---

## Iteration 8 — Type text retrieval & lexer stream helpers

**Goals**
- Ensure `TypeNode` can provide user-friendly text regardless of source availability.
- Introduce helpers on `Stream` for span slicing and token lexeme concatenation.

**Files**
- `crates/parser/src/lexer_api.rs`
- `crates/parser/src/parser.rs` (type parsing)

**Steps**
1. Add a method to `Stream` to rebuild source slices from tokens when raw source is
   absent (e.g., concatenating token lexemes between span bounds).
2. Update `parse_type_node` to call this helper and fill `TypeNode.repr`.
3. Audit any code using `type_node.text` and switch to `type_node.repr`.

---

## Iteration 9 — Test suite expansion

**Goals**
- Cover all new syntax/diagnostics with focused unit tests.
- Ensure lexer-originating failures remain reported without spurious parser success.

**Files**
- `crates/parser/src/tests/ok_smoke.rs`
- `crates/parser/src/tests/bad_smoke.rs`
- Potentially new fixtures under `crates/parser/src/tests/`

**Steps**
1. Add positive tests for:
   - Functions without return types.
   - `for-in` loops.
   - `parallel map/reduce` constructs.
   - Top-level `let` declarations.
2. Add negative tests for every new diagnostic (including the provided corpus 01–20),
   matching `ParseCode` and message.
3. For lexer-owned errors (unterminated string, bad digits, etc.), assert that parser
   emits no false positives while lexer diagnostics still surface.
4. Run `cargo test -p surge-parser` until green.

---

## Iteration 10 — Workspace integration & final QA

**Goals**
- Ensure CLI continues to combine lexer + parser diagnostics.
- Run the full workspace test suite.

**Files**
- `crates/cli/src/main.rs`
- `Cargo.lock`, `Cargo.toml`

**Steps**
1. Re-run targeted CLI smoke tests if available (manual or scripted) to verify
   diagnostics formatting with the new codes.
2. Execute `cargo test --workspace` and fix any regressions.
3. Review the diff for diagnostic message stability and update docs if needed.

---

## Reference checklist (keep handy during implementation)

- Attributes: `@pure`, `@overload`, `@override`, `@backend("cpu"|"gpu")` only.
- Function return type optional; `->` without type → `PARSE_EXPECTED_TYPE_AFTER_ARROW`.
- `let` must include `=`; `signal` must use `:=`.
- C-style `for` and `for-in` supported.
- `parallel map/reduce` are the only owners of `=>`.
- `extern<Type>{}` requires `<`, `Type`, `>`, `{}`, with diagnostics on failures.
- Top-level `let` declarations permitted.
- `TypeNode.repr` populated even when source text unavailable.
- Array semicolon errors → `PARSE_INVALID_ARRAY_SYNTAX`.
- Assignment must have valid LHS; invalid primaries should error early.
- All new diagnostics mapped through `surge-diagnostics`.
- Tests cover both success and failure paths using examples in `examples/`.

