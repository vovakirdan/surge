# N-ANON-RECORD: return origins of an anonymous record literal

<!-- summary -->
1. **Defect.** `fn main() { let p = { x: 1, y: 2 }; }` stopped `surge diag` with `return origins: expression 5 is not typed` (`internal/sema/return_origin_expr.go:31-33` at `1b122bfa`). Three valid golden programs failed on it.
2. **Root cause, in the checker.** An anonymous record literal takes its type only from an expected struct type. With none, `typeExprStruct` returns no type and reports nothing. The checker reads "no type" as "error already reported", so the expressions that read the literal also stay untyped, silently.
3. **Same gap elsewhere.** The empty array literal `let a = [];` is a second root. In every program measured, each untyped expression the analysis reaches is untyped because of one of these two roots.
4. **Not fixed in the checker.** Recording a type would need an anonymous record type, which the type system does not have, so it would widen the language. LANGUAGE.md §2.5 says the literal needs an expected type, so refusing it would rewrite the three committed goldens. Either is an owner decision; section 6 has the ledger text.
5. **Fix, in the analysis.** An untyped expression that one of those literals explains no longer stops the diagnosis. The literal keeps every field value's origins, and a local `let` name keeps its tracked value. A field read, or an operator the checker selected nothing for, is answered only over values that carry no borrow. Anything else is refused by name. Any other missing type still stops the analysis, as before.
6. **Reference fields are real.** The checker accepts `{ r: &owned }` silently, so the literal can carry a borrow. The transfer keeps it, and 4 rows refuse a dying owner with SEM3139.
7. **Goldens.** All three pass the harness's own diag command with exit 0, and their output is byte-identical to the committed, empty `.diag` files. No golden was regenerated.
8. **Tests.** The full `internal/sema` and `internal/driver` suites fail the same 34 top-level tests (56 entries) at the base and after; the only difference is 15 new passing entries, the packet's 2 tests and their 13 rows.
9. **Census.** Of the 1079 golden programs, exactly the three move, from `other` (the abort) to `ok`, in the user form and in the harness form. The other 1076 print byte-identical output in both forms, and no unfinished program changes a row.
10. **Runs.** No measured program that holds an untyped literal builds: MIR validation refuses each of the 4 probe forms on the VM and on LLVM. A task stored in such a literal is refused by the task check (SEM3021), so no task-leak runner opens (RV2-DEBT-365).

Everything below was measured at `1b122bfa` (base) and at `0dee3302` (after) unless it says "(read from code)". The branch is `cloud/anon-record-origin`, started from `validation/step7-d2-on-d1` at `1b122bfa`. It has two code commits: `51ea0a33` (the transfer) and `0dee3302` (its gate, section 2.3). The history of the first is in section 4.

## 1. Root cause

### 1.1 Why the literal has no type

- `typeExprStruct` types each field value, then returns `NoTypeID` when the literal has no type name (`internal/sema/type_expr_values.go:485-487`). A consumer with an expected struct type supplies the type afterwards (`applyExpectedType`, as in `let p: Point = { x: 1, y: 2 }`). A consumer with none supplies nothing, for example `let p = ...` or an operand of `+`, and nothing reports it (read from code; measured below).
- LANGUAGE.md §2.5 (l.324): "Struct literals without an inline type require an unambiguous expected struct type." The checker does not enforce this.
- The type system has no anonymous record type. Structs are nominal (`internal/types`), and `-> { x: int, y: int }` is a syntax error (golden `sema/invalid/returning_anonymous_record.sg`, SYN2001). So the checker has no type it could record without widening the language.

### 1.2 What else inherits the gap

The checker reads `NoTypeID` as an error already reported and stays silent downstream. This was measured with the type dump `tools/type_dump_test.go.txt` (copy it into `internal/driver` to run; it is not part of the suite):

| Program | Untyped expressions | Checker diagnostics |
|---|---|---|
| `sema/valid/user_record_type.sg`, `anonymous_record()` | the literal `{ x: 1, y: 2 }`, both reads of `p`, `p.x`, `p.y`. `p_x`, `p_y` and `p_x + p_y` are `int` by annotation | none |
| `sema/ownership_and_references/overload_autoref_temp_error.sg` | the literal and the whole `{ x: 1, y: 2 } + p2`, which selects no `__add` (`MagicBinarySymbols` has no entry) | none |
| probe `p1_type_dump` | `{ r: &owned }`, `{ r: outside }`, `a`, `&a`, `b`, `b.r`, `a.r`, `[a]`, `(a, 1)`, `&{ x: 1 }` | SEM3020 on moves out of `a` and SEM3023 on `&{ x: 1 }` only. The block `{ let owned: int = 1; ret { r: &owned }; }` is typed `nothing` |
| probe `p2_type_dump` | `[]`, `{ }`, `{ 1, 2 }` | none |

The operator is silent because `typeBinary` calls `enforceSameNumericOperands`, which returns `false` without a report when an operand has no type (`internal/sema/type_expr_ops.go:571-573`, called at `:151-153`). So every numeric-family operator — arithmetic, bitwise, shifts, comparisons and compound assignments — stays silently untyped.

These consumers are **not** silent (each measured): a call with the untyped value as an argument (SEM3015 `expected &Point, got unknown`, probes `r14` to `r16`); a method call on it (SEM3005, `r17`); a generic call or a tag constructor over it (SEM3046, `c1`, `c2`, `c5`); and returning the literal itself where the result is not a struct (SEM3015 `int is not a struct`, `r10`).

The second root is `let a = [];`: `typeExprArray` returns `NoTypeID` when no element gives a type (`type_expr_values.go:298-300`). `{ }` and the positional `{ 1, 2 }` are the same `ExprStruct` root as the named-field literal.

In the golden corpus only the brief's three programs reach an untyped expression. They are the only `other` programs of the base census (section 5).

### 1.3 What happens after `diag`

HIR keeps the untyped literal and binding (`let p: ? = { x = owned_temp(1): int, ... }`). It lowers the unselected `+` as a builtin binary operation (`internal/hir/lower_expr.go:309-314`, `:341-345`). MIR validation then refuses the function: `MIR validation failed: function main: local L0 (p): unknown type`. This was measured with `surge run` on the VM and on LLVM for probes `b1`, `b2`, `b4` and `b5` (section 7). `e1` (`let a = [];`) is refused earlier, by the analysis. So `surge diag` accepts these programs and `surge build` refuses them with an internal validation error. That disagreement is the checker's debt (section 6), not this packet's.

## 2. The fix and why it is sound

Files: `internal/sema/return_origin_expr.go` (+4 −1), `internal/sema/return_origin_constructors.go` (+4), and the new `internal/sema/return_origin_unchecked.go` (179 lines, comments included). The test file `internal/driver/return_origin_anonymous_record_test.go` adds 273 lines.

### 2.1 Where it enters

```go
if node == nil || u.Sema.ExprTypes[id] == types.NoTypeID && !b.uncheckedByLiteral(id) {
	return ..., fmt.Errorf("return origins: expression %d is not typed in %s", id, u.SourceKey)
}
if u.Sema.ExprTypes[id] == types.NoTypeID {
	return b.uncheckedExpr(id, node, env, targets) // the checker's silent gap
}
```

Typed expressions never reach the new code, so their transfers are unchanged. An untyped expression that `uncheckedByLiteral` does not admit stops the analysis with the same error as at the base.

### 2.2 The transfers

| # | Untyped expression | Transfer | Why it is sound |
|---|---|---|---|
| U1 | anonymous record literal (`ExprStruct` without a type name) | `constructorChildren`, with the result's shape taken as reference-carrying: the fields are evaluated in order, their effects and abrupt exits are kept, and the value is the join of every field's value | A record value is its field values and nothing else, so no origin is lost and none is invented. A typed record that can hold a reference already gets this transfer. |
| U2 | parenthesised group | the inner expression's transfer | Same value. |
| U3 | name of a local `let` binding | the value the environment tracked for the binding, checked for expiry; storage is the binding | The typed name transfer without its type-based erasure, so nothing is dropped. An untracked binding reads as Unknown (`returnOriginEnv.value`) and is refused at the next scope check. A binding outside the function is refused by name. |
| U4 | field read `t.f` | if `t`'s value is proven borrow-free (normal, no root, no callable), the field is borrow-free and lies in `t`'s storage. Otherwise it is refused: "field of an untyped value that may carry a borrow needs a checked type" | `internal/sema/return_origin.go:50-51`: "A normal value with no roots is proven RefFree; an unresolved reference has an explicit Unknown root instead." A value that carries no borrow is not a reference, no part of it carries a borrow, and its field lives inside it. When `t` does carry a borrow, the analysis cannot tell a record from a reference to one, so it refuses. |
| U5 | numeric-family binary operator with no selected operation, not an assignment | operands are evaluated left to right, as in the typed binary. If both values are proven borrow-free, so is the result. Otherwise it is refused: "operator on an untyped value that may carry a borrow needs a checked type" | With no `MagicBinarySymbols` entry, HIR lowers a builtin operation (`hir/lower_expr.go:309-314`). It reads its operands as values and borrows neither operand's storage, so a result computed from borrow-free values carries no borrow. A selected operation takes another path, and so does an operator the checker reports rather than leaves untyped. |
| U6 | any other admitted expression | Unknown, refused: "untyped expression needs a checked type for its origins" | A named refusal of the same shape as the existing catch-all for an expression kind with no transfer. |

### 2.3 The gate: only what an untyped literal explains

`uncheckedByLiteral(id)` admits an untyped expression only when one of these holds:

- it is an untyped anonymous record literal, or an empty array literal;
- it is a name whose own declaration is an unannotated `let` bound to such an expression;
- it is a group, field, unary or binary operator, array, tuple or ternary arm that reads such an expression.

Anything else stops the analysis, including a checked expression whose type record is missing. The pinned tamper tests delete a record, or walk a name the checker never types (such as `Color` in `Color::Red`), and expect that stop.

**Can the literal carry a reference field? Yes, measured.** The checker accepts `{ r: &owned }`, and `{ r: x }` where `x` is a reference parameter, with no diagnostic (probe `p1`, rows below). U1 keeps the field's origin, so the borrow stays visible. It is refused SEM3139 when its owner dies at a block exit, at a function return, or through a callee's summary. It is refused by name where a field read or an operator would have to see through it.

**What the fix leaves to the checker.** The checker accepts returning an untyped value where `int` or `&int` is declared (`r8`, `r13`). The analysis answers from the value. `r13` borrows only its caller's argument and is clean. `r9` and `r12` borrow a local and are refused SEM3139. `r11` shows the callee's summary keeps the parameter origin even for an `int` result, so a caller that lends a dying local is refused. The type confusion itself is the checker's gap.

**Task borrows (RV2-DEBT-365).** The abort was a barrier only for programs that contain an untyped literal, and none of them builds (section 1.3). `b3` stores a task over a local in an anonymous record, `let p = { t: worker(&l) };`. The task check refuses it with SEM3021 `a task still borrows 'l' at this return` at `diag`, at `run --backend vm` and at `run --backend llvm`, at the base and after. The driver `TestH2Tripwire*` tests are in the suite run of section 4 and do not change. The VM tripwire tests were not run (section 8).

**RV2-DEBT-368** is not touched: no string temporary and no call argument takes a new path.

## 3. Rows and counterfactuals

All rows are in `internal/driver/return_origin_anonymous_record_test.go`.

`TestDiagnoseAnonymousRecordReturnOrigins` first checks each row's premise on the checked source alone (`requireUntypedAnonymousRecord`). The literal must have no type, and an operator, where the row has one, must have no type and no selection. If the checker ever types them, the row fails instead of passing vacuously. The row then runs the public `DiagnoseWithOptions`, the route `surge diag` takes, with the real stdlib.

| Row | Shape | Verdict pinned |
|---|---|---|
| `let_bound_literal` | `let p = { x: 1, y: 2 }; let p_x: int = p.x; ...; return p_x + p_y;` (the golden's function) | clean |
| `add_operand_borrowed_self` | `let _ = { x: 1, y: 2 } + p2;` with `__add(self: &Point, other: &Point)` (golden `overload_autoref_temp_error`) | clean |
| `add_operand_value_overload` | the same with a by-value `@overload __add` (golden `overload_autoref_temp_move`) | clean |
| `reference_field_block_exit` | `let kept = { let owned: int = 1; ret { r: &owned }; };` | exactly one SEM3139 for `owned` at `ret { r: &owned };` |
| `reference_field_bound_block_exit` | `let p = { r: &owned }; ret p;` inside a value block | SEM3139 at `ret p;` |
| `reference_field_returned` | `let p = { r: &owned }; return p;` | SEM3139 at `return p;` |
| `reference_field_through_callee` | `probe(x: &int)` returns `p = { r: x }`; a caller lends it a dying local | SEM3139 at `ret probe(&owned);` |
| `borrowed_field_read_refused` | `let p = { r: x }; let q = p.r;` | unfinished, exactly the U4 row at `p.r` |
| `borrowed_operand_refused` | `let _ = { r: x } + p2;` | unfinished, exactly the U5 row at the operator |
| `untyped_assignment_refused` | `p = { r: &owned }` into an untyped binding | unfinished, exactly the U6 row at the assignment |
| `empty_array_refused` | `let a = [];` | unfinished, exactly the U6 row at `[]` |

`TestAnalyzeAnonymousRecordLostRecordStaysFatal` runs the analysis on the `let_bound_literal` source. With every record in place the analysis finishes; the test then deletes one checked expression's type and requires the exact base error with no analysis. It has two rows: the operator `p_x + p_y` beside the literal (`checked_operator`), and the field value `1` inside it (`checked_field_value`).

Counterfactuals. `tools/run_counterfactuals.py` applies one change at a time, runs both tests, and restores the files; `git status` was clean afterwards. CF6 also runs the four tamper tests of section 4. The logs stayed in the session's scratch directory.

| CF | Change | What goes red |
|---|---|---|
| CF1 | the fix reverted (`return_origin_expr.go` and `return_origin_constructors.go` at `1b122bfa`, `return_origin_unchecked.go` removed) | all 11 rows of the first test, each with `diagnosis failed: return origins: expression N is not typed` (N = 5, 25, 38, 5, 5, 5, 3, 3, 23, 3, 1), and both rows of the second, whose premise (the intact source is answered) fails |
| CF2 | U1 answers the literal as borrow-free | the 4 `reference_field_*` rows (`errors=[], want exactly one SEM3139 for 'owned'`), `borrowed_field_read_refused` and `borrowed_operand_refused`: the carried borrow disappears and every leak is accepted |
| CF3 | U4 answers any target | `borrowed_field_read_refused` (`errors=[], want unfinished "field of an untyped value ..." at "p.r"`) |
| CF4 | U5 answers any operands | `borrowed_operand_refused` |
| CF5 | U3 drops the binding's value | `reference_field_bound_block_exit`, `reference_field_returned`, `reference_field_through_callee`, `borrowed_field_read_refused` |
| CF6 | the gate removed (every untyped expression answered) | both rows of `TestAnalyzeAnonymousRecordLostRecordStaysFatal`, plus the four tamper tests `TestAnalyzeFarSelectors/record_missing_stays_refused`, `TestReturnOriginDeferredMethodTargetEvidence/runtime_receiver_untyped`, `TestReturnOriginEnumVariantRecord/variant_record_deleted` and `TestReturnOriginTypeTestOperands` (3 leaves). Without the gate, a deleted record on `p_x + p_y` was accepted clean, with no row at all |
| control | no change | both tests and the tamper tests pass |

CF1 output, abridged:

```
return_origin_anonymous_record_test.go:171: diagnosis failed: return origins: expression 5 is not typed in <tmp>/origin.sg
return_origin_anonymous_record_test.go:171: diagnosis failed: return origins: expression 25 is not typed in <tmp>/origin.sg
...
--- FAIL: TestDiagnoseAnonymousRecordReturnOrigins (4.13s)
    --- FAIL: TestDiagnoseAnonymousRecordReturnOrigins/let_bound_literal (0.32s)
    ... (all 11)
--- FAIL: TestAnalyzeAnonymousRecordLostRecordStaysFatal (0.00s)
    --- FAIL: TestAnalyzeAnonymousRecordLostRecordStaysFatal/checked_operator (0.00s)
    --- FAIL: TestAnalyzeAnonymousRecordLostRecordStaysFatal/checked_field_value (0.00s)
FAIL	surge/internal/driver	4.133s
```

CF2 output, abridged:

```
return_origin_anonymous_record_test.go:193: errors=[], want exactly one SEM3139 for 'owned' at "ret { r: &owned };"
return_origin_anonymous_record_test.go:193: errors=[], want exactly one SEM3139 for 'owned' at "ret p;"
return_origin_anonymous_record_test.go:193: errors=[], want exactly one SEM3139 for 'owned' at "return p;"
return_origin_anonymous_record_test.go:193: errors=[], want exactly one SEM3139 for 'owned' at "ret probe(&owned);"
return_origin_anonymous_record_test.go:196: errors=[], want unfinished "field of an untyped value that may carry a borrow needs a checked type" at "p.r"
return_origin_anonymous_record_test.go:196: errors=[], want unfinished "operator on an untyped value that may carry a borrow needs a checked type" at "{ r: x } + p2"
FAIL	surge/internal/driver	8.215s
```

CF6 output, abridged:

```
return_origin_anonymous_record_test.go:269: lost record of "p_x + p_y": analysis=&{Diagnostics:[] Pending:[] Summaries:[...]} error=<nil>, want "return origins: expression ... is not typed in origin.sg"
return_origin_anonymous_record_test.go:269: lost record of "1": analysis=&{Diagnostics:[] Pending:[{... Reason:untyped expression needs a checked type for its origins} ...
--- FAIL: TestAnalyzeFarSelectors/record_missing_stays_refused
--- FAIL: TestReturnOriginEnumVariantRecord/variant_record_deleted
--- FAIL: TestReturnOriginDeferredMethodTargetEvidence/runtime_receiver_untyped
--- FAIL: TestReturnOriginTypeTestOperands/{missing_is_record_stays_refused,missing_heir_record_stays_refused,guard_is_without_record_stays_refused}
```

## 4. Test suites, base and after

Command, run in a clean worktree at each commit (`git worktree add --detach <dir> <commit>`):

```
go test -json -timeout 150m ./internal/sema/... ./internal/driver/... > <run>.json
python3 cloud-work/anon-record-origin/tools/test_names.py base.json after.json
```

`SURGE_STDLIB` was not set, as in PLAN.md's run. Wall time was 9m58s at the base and 10m24s after.

| | base `1b122bfa` | after `0dee3302` |
|---|---|---|
| `surge/internal/sema` | pass | pass |
| `surge/internal/driver/diagnose` | pass | pass |
| `surge/internal/driver` | fail: 34 top-level tests, 56 entries | fail: the same 34, the same 56 |
| passing entries | 2638 | 2653 |
| failing only in one run | — | none |
| passing only after | — | `TestDiagnoseAnonymousRecordReturnOrigins` and its 11 rows; `TestAnalyzeAnonymousRecordLostRecordStaysFatal` and its 2 rows (15 entries) |
| passing only at the base (lost) | none | — |

The counts match PLAN.md section 6 item 7 at `7131fb2` (34 top-level tests, 56 entries); PLAN.md lists no names to compare. None of them contains "is not typed" in its output at the base. `results/tests.txt` lists all 56 failing entries.

**The first landing was not clean, and this is how it was found.** `51ea0a33` answered every untyped expression. The same suite run over it failed 4 more top-level tests (10 entries): `TestAnalyzeFarSelectors/record_missing_stays_refused` (driver), `TestReturnOriginDeferredMethodTargetEvidence/runtime_receiver_untyped`, `TestReturnOriginEnumVariantRecord/variant_record_deleted` and 3 leaves of `TestReturnOriginTypeTestOperands` (sema). Each deletes a type or use record from a checked program, or walks a name the checker never types, and pins the abort. `0dee3302` adds the gate of section 2.3, and the row `TestAnalyzeAnonymousRecordLostRecordStaysFatal`. Both commits are on the branch; the census and probes below are for `0dee3302`.

`go build ./...` and `go vet ./...` exit 0 at `0dee3302`. `gofmt -l` is clean on the four changed files. It lists `internal/sema/return_origin_test.go`, which this packet does not touch and which is unformatted at the base.

## 5. Golden census, base and after

Commands, from the repository root, with a prebuilt compiler for each side (`go build -o <bin> ./cmd/surge` in a clean worktree at the commit). The tools come from branch `cloud/d2-unfinished-plan`:

```
D2TOOLS=<checkout of cloud/d2-unfinished-plan>/cloud-work/d2-unfinished-plan/tools
ROOT=<worktree at 1b122bfa> SURGE=<surge-base> OUTDIR=<census-base> JOBS=3 D2TOOLS=$D2TOOLS cloud-work/anon-record-origin/tools/census.sh
ROOT=<worktree at 0dee3302> SURGE=<surge-after> OUTDIR=<census-after> JOBS=4 D2TOOLS=$D2TOOLS cloud-work/anon-record-origin/tools/census.sh
python3 cloud-work/anon-record-origin/tools/compare_census.py <census-base> <census-after>
```

`census.sh` runs d2's `run_one.sh` on every `.sg` under `testdata/golden`, then d2's `classify.py` on the outputs. Both d2 tools are unchanged; the wrapper only skips the rebuild. Each program is diagnosed in two forms, each with a 120 s timeout. The user form is `SURGE_STDLIB=$ROOT surge diag <file>`. The harness form is `surge diag --format short --directives=<off|collect> <file>`, which is what `scripts/golden_update.sh:185` runs. `compare_census.py` compares each program's class, each unfinished program's rows, and the raw output bytes.

| Form | base `1b122bfa` ok / unfinished / diagnostics / other | after `0dee3302` |
|---|---|---|
| user form, all 1079 files | 520 / 108 / 448 / 3 | 523 / 108 / 448 / 0 |
| harness form, all 1079 files | 518 / 107 / 451 / 3 | 521 / 107 / 451 / 0 |

The base figures equal PLAN.md's census at `7131fb2`, and its 3 `other` programs are the brief's three.

- **Verdict changes: 3 in each form, and nothing else.** `sema/valid/user_record_type.sg`, `sema/ownership_and_references/overload_autoref_temp_error.sg` and `overload_autoref_temp_move.sg` go from `other` to `ok`.
- **Unfinished programs whose rows changed:** 0 of 108.
- **Raw output that differs byte-for-byte:** those 3 files in each form. The other 1076 are identical.
- The full comparison output is `results/census.txt`. The census JSON files stayed in the session scratch directory.

**The goldens, as the harness checks them.** For each of the three, `surge diag --format short --directives=off <file>` with `SURGE_STDLIB` at the checkout gives:

| Golden | base: exit / output vs committed `.diag` | after |
|---|---|---|
| `sema/valid/user_record_type.sg` | 1 / identical (empty; the abort goes to stderr) | 0 / identical (empty) |
| `sema/ownership_and_references/overload_autoref_temp_error.sg` | 1 / identical | 0 / identical |
| `sema/ownership_and_references/overload_autoref_temp_move.sg` | 1 / identical | 0 / identical |

`scripts/golden_update.sh:202-203` requires exit 0 for a valid case, so all three pass its diagnostics check after the fix. The script also writes `.tokens`, `.ast` and `.fmt` files; the packet does not touch the tokenizer, parser or formatter, so it cannot change them. `make golden-check` was not run. It regenerates the whole corpus, and (read from code) it cannot pass at the base: `golden_update.sh:202-203` rejects every valid program that exits 1, and the base census has unfinished programs outside `/invalid/` paths.

## 6. Ledger text for a DEBT row

Proposed for `docs/runtime-v2-epics/DEBT.md`; the file is not edited here, per the brief. `RV2-DEBT-382` is the next free id at `1b122bfa` (the highest row is 381); renumber it if that id is taken at landing.

```
| RV2-DEBT-382 | **AN ANONYMOUS RECORD LITERAL WITH NO EXPECTED TYPE -- AND AN EMPTY ARRAY LITERAL WITH NONE -- IS LEFT UNTYPED WITH NO DIAGNOSTIC, SO `surge diag` ACCEPTS PROGRAMS THAT `surge build` REFUSES AT MIR VALIDATION.** `typeExprStruct` returns no type for `{ x: 1, y: 2 }` written without a type name when its context supplies no expected struct type (`internal/sema/type_expr_values.go:485-487`), and `typeExprArray` does the same for `[]` (`:298-300`). Neither reports, although LANGUAGE.md §2.5 (l.324) says such a literal requires an unambiguous expected struct type. Because the checker reads NoTypeID as an error already reported, what reads the literal stays untyped and silent too: its `let` binding, a field of it, `&` of it, an array or tuple holding it, an assignment of it, a `return` of it into `int` or `&int`, and every numeric-family operator over it (`enforceSameNumericOperands`, `internal/sema/type_expr_ops.go:571-573`), which then selects no user `__add`. A call, method call, generic call or tag constructor over it is refused (SEM3015, SEM3005, SEM3046). The literal's fields are checked against no type, so `{ r: &owned }` carries a borrow. MIR validation refuses every function holding such a value (`MIR validation failed: function main: local L0 (p): unknown type`, on the VM and on LLVM, probes `b1`, `b2`, `b4`, `b5` of `cloud-work/anon-record-origin`), so none runs. Return-origin analysis stopped the whole diagnosis on the first such expression (`return origins: expression N is not typed`), which failed `sema/valid/user_record_type.sg` and `sema/ownership_and_references/overload_autoref_temp_{error,move}.sg` in the golden harness. Since N-ANON-RECORD (`51ea0a33`, gated by `0dee3302`) the analysis answers exactly the untyped expressions such a literal explains, from their values (`internal/sema/return_origin_unchecked.go`): the literal keeps every field's origin, a local binding keeps its tracked value, a field read or an unselected builtin operator is answered only over values that carry no borrow, and anything else is refused by name; a carried borrow of a dying owner is SEM3139 (rows of `TestDiagnoseAnonymousRecordReturnOrigins`), and any other missing type still stops the analysis (`TestAnalyzeAnonymousRecordLostRecordStaysFatal`). The three goldens record the silent acceptance as valid, with empty `.diag` files. Found by the D2 unfinished-plan census (2026-09-25). | Open | The checker (`internal/sema/type_expr_values.go`, `typeExprStruct` and `typeExprArray`), by an owner decision: enforce §2.5 with a named refusal at the literal, which rewrites the three goldens' `.diag`, or give anonymous records a type, which widens the language | `fn main() { let p = { x: 1, y: 2 }; }` and `fn main() { let a = []; }` get the same verdict from `surge diag` and from `surge build` on both backends; the three goldens are regenerated or kept by that ruling; `requireUntypedAnonymousRecord` in `TestDiagnoseAnonymousRecordReturnOrigins` fails once the literal is typed or refused, and the rows are re-pinned in the same commit; `return_origin_unchecked.go` is then removed or reduced to the named refusal, and the golden census shows no program reaching it. |
```

## 7. Probes

The programs are in `probes/` and the full table is `probes/results.tsv`. It was produced by `BASE=<surge at 1b122bfa> AFTER=<surge at 0dee3302> tools/run_probes.sh` from the repository root with `SURGE_STDLIB` set to it. `diag` is `surge diag --format short`, and `run` is `surge run --backend vm|llvm`, with the after compiler only.

| Probe | Shape | base `diag` | after `diag` | after `run` (VM and LLVM) |
|---|---|---|---|---|
| `a0_min_let` | the brief's one-line program | abort, expr 5 | clean | (no entrypoint) |
| `b1_let_field` | `let p = {...}; let p_x: int = p.x; return p_x;` | abort | clean | `MIR validation failed: ... local L0 (p): unknown type` |
| `b2_binary` | `let _ = { x: 1, y: 2 } + p2;` | abort | clean | `MIR validation failed: ... local L2 (tmp_struct2): unknown type` |
| `b3_task_field` | `let p = { t: worker(&l) };` | SEM3021 | SEM3021 | SEM3021 (refused before building) |
| `b4_ref_return` | `probe(x: &int) -> &int { let p = { r: x }; return p; }` | abort | clean | `MIR validation failed: function probe: local L1 (p): unknown type` |
| `b5_discard` | `let _ = { x: 1 };` | abort | clean | `MIR validation failed: ... local L0 (tmp_struct1): unknown type` |
| `e1_empty_array` | `let a = [];` | abort | unfinished (U6) | unfinished (U6), not built |
| `r1` to `r4` | `ret { r: &owned }`, bound then `ret p`, nested `{ inner: { r: &owned } }`, parenthesised | abort | SEM3139 | — |
| `r5`, `r7`, `r6` | field of a borrow-carrying record, operator over it, assignment of one | abort | unfinished (U4, U5, U6) | — |
| `r8`, `r13` | untyped record returned as `int` / `&int`, borrowing only a parameter | abort | clean | — |
| `r9`, `r12`, `r11` | the same borrowing a local, and a caller lending a dying local | abort | SEM3139 | — |
| `r10`, `r14` to `r17`, `c1`, `c2`, `c5` | the literal returned as `int`; a call, method call, generic call or tag constructor over an untyped value | checker error | same checker error | — |
| `c3`, `c4`, `c7` | a tuple, an array, a ternary holding a borrow-carrying record | abort | unfinished (U6) | — |
| `c6` | a `compare` whose arms yield the record | abort (expr 5) | abort (expr 11): `compare` is outside the gate | — |
| `p1`, `p2` | the type-dump fixtures | SEM3020 / abort | SEM3020 / unfinished (U6) | — |

## 8. Not verified

- **The VM tripwire tests** (the `internal/vm` part of `make runtime-v2-h2-tripwire-check`) were not run. The driver part ran inside the full driver suite and did not change. That is the measured fact; I did not read the probe sources to check whether any contains an anonymous record or an empty array literal. One that did would have stopped the diagnosis at the base.
- **Consumers of an untyped value inside typed transfers** were measured only in the forms of section 7: a `let` initializer (typed or not), `return` and `ret`, a field target, an operator operand, an assignment, a call argument, a method receiver, and a generic or tag call. An `if` or `while` condition that compares an untyped field, `for ... in` over an untyped array, `@drop` of an untyped binding and a compound assignment were not probed. Each of those is either admitted by the gate and answered by U3 to U6, or it stops the analysis as at the base.
- **The MIR refusal** was measured for 4 forms (`b1`, `b2`, `b4`, `b5`). Code the MIR never lowers — for example a function nothing reaches — was not probed. Such code does not run either.
- **Other packages' suites** (`internal/hir`, `internal/mir`, `internal/buildpipeline`, `internal/vm`, `internal/backend/llvm`) were not run. The change can only affect a program with an untyped literal, which stopped the diagnosis at the base.
- **Programs outside `testdata/golden`** (stdlib, core, showcases, benchmarks) were not censused as roots. Core and stdlib bodies are analysed inside the census programs that import them, and the base census shows no `other` program beyond the three goldens, so no analysed core or stdlib body reaches an untyped expression.
- **One census run per side.** Section 5's byte-for-byte comparison of the 1076 unchanged outputs is the determinism check; the census was not repeated.
- `uncheckedByLiteral` is not memoised. It runs only for untyped expressions, and its recursion is bounded by the source: a name leads only to an earlier declaration.
