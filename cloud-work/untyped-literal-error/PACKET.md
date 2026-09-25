# Untyped struct or array literal is an error (SEM3219)

<!-- summary -->
1. **Ruling.** The owner's ruling of 2026-09-25 enforces LANGUAGE.md §2.5 (l.324): a struct literal written without a type name needs an unambiguous expected struct type, and there is no anonymous record type. An empty array literal with no expected array type is an error too.
2. **Defect.** `typeExprStruct` (`internal/sema/type_expr_values.go:485-487`) and `typeExprArray` (`:298-300`) left `{ x: 1 }` and `[]` untyped and reported nothing. The checker reads "no type" as "error already reported", so every reader stayed silent, and the program failed later: at the return-origin analysis, or at MIR validation.
3. **New code.** `SemaLiteralNeedsType`, **SEM3219**, is reported once, at the literal. The message says what is missing, and the help says how to fix it: name the struct, or annotate the binding.
4. **No cascade.** A literal that an error already covers gets no second error. A call argument, method argument or method receiver that is untyped because of such a literal reports the literal instead of its own `got unknown`, `no matching overload` or `has no method`.
5. **Types from context.** Two places now pass on an expected type that was lost before: a `return` inside a block expression takes the function's result type (needed by `stdlib/http`), and a tuple literal takes its annotation's element types. A call argument still does not take its parameter's type; it gets SEM3219 naming the parameter's struct.
6. **Goldens.** The anonymous function of `sema/valid/user_record_type.sg` moves to the new `sema/invalid/record_literal_without_type.sg`. Both `overload_autoref_temp_{error,move}.sg` name their struct and move to `sema/invalid/`, where each now fails for the reason it tests: SEM3023 (autoref of a temporary) and SEM3130 (the by-value overload moves `p2`).
7. **Census.** Of 1079 golden programs (1080 after), in both command forms, exactly these change: the two relocated goldens go from `other` (the analysis abort) to `diagnostics` at their new paths, the new `record_literal_without_type.sg` is `diagnostics`, and `sema/valid/user_record_type.sg` goes from `other` to `ok`. The other 1076 print byte-identical output. The 84 files under `showcases/`, `benchmarks/`, `stdlib/` and `core/` also print byte-identical output.
8. **Hard stop not hit.** No program that builds at the base is refused. Of the programs that exist at the base, only the two relocated goldens become refused, and their originals stop at the base with `return origins: expression N is not typed` under `surge build` on both backends.
9. **Tests.** The full `internal/sema`, `internal/driver`, `internal/diag` and `internal/ownershipgate` suites fail the same 36 top-level tests (66 entries) at the base and after; the difference is 30 new passing entries, the 2 new tests and their 28 rows.
10. **Counterfactuals.** Reverting the checker change turns 17 rows and the message test red. Each of 7 narrower counterfactuals turns red exactly the rows that pin its part.

The base is `1b122bfa` (`origin/validation/step7-d2-on-d1`). The branch `cloud/untyped-literal-error` has three commits:

- `893ffb30`: the checker change, tests, goldens, docs and an allowlist edit;
- `10afc901`: puts the allowlist back (section 6);
- `9792fef0`: covers a literal whose `let` annotation fails to resolve, a cascade the census found.

Everything below was measured at `9792fef0` unless it names another commit.

## 1. The diagnostic

**Code.** `SemaLiteralNeedsType Code = 3219` in `internal/diag/codes.go`, the next free SEM number (the highest at the base is 3218, `SemaTaskDropped`). Its `codeDescription` entry reads "a struct literal without a type name, or an empty array literal, needs an expected type". The two guard tests in `internal/diag` (`TestEveryDiagnosticCodeNumberIsClaimedOnce`, `TestEveryDiagnosticCodeHasADescriptionEntry`) pass.

**Text.** Short form (`surge diag --format short --with-notes`), exactly as printed:

| Where | Message | Note | Help |
|---|---|---|---|
| struct literal, no expected type | `struct literal without a type name gets no type here` | — | ``name the struct, as in `Point { ... }`, or annotate the binding, as in `let p: Point = { ... };` `` |
| struct literal passed where a parameter expects the struct `T` | same | `a parameter's type does not give a struct literal its type` | ``name the struct: `T { ... }` `` |
| empty array literal, no expected type | `empty array literal gets no element type here` | — | ``annotate the binding, as in `let items: int[] = [];` `` |
| `[]` passed where a parameter expects `T[]` | same | `a parameter's type does not give an empty array literal its element type` | ``bind it with its type first, as in `let items: T[] = [];`, and pass `items` `` (the type is spelled as written, so `int[][]`, not the label `[[int]]`) |

For example, the new golden `sema/invalid/record_literal_without_type.diag`:

```
error SEM3219 testdata/golden/sema/invalid/record_literal_without_type.sg:4:13 struct literal without a type name gets no type here
```

The message style follows the nearby diagnostics, for example SEM3015 `struct literal requires explicit type when assigning to int`: lower-case, and code in backticks.

**Documentation.** LANGUAGE.md §2.5 and LANGUAGE.ru.md, at the same sentence (l.324 and l.315), gain one sentence. It names the error and its code, covers the empty array literal, and says a function argument does not supply the expected type.

## 2. The checker change

Files: `internal/sema/untyped_literal.go` (new) and small hooks in `type_expr.go`, `type_checker_core.go`, `check.go`, `type_expr_calls.go`, `type_expr_calls_method_resolution.go`, `type_checker_returns.go`, `type_checker_bindings.go` and `type_checker_walk.go`. Nothing in `internal/hir`, `internal/mir` or the runtime is touched.

1. **Record.** `typeExpr` notes every struct literal without a type name, and every empty array literal, that it types as `NoTypeID` (`noteIfUntypedLiteral`).
2. **Report after the walk.** A new phase `check_untyped_literals`, right after `walk_items`, reports every recorded literal still untyped (`reportUntypedLiterals`). It goes outermost first, so a literal nested inside another is not reported twice. A literal that a consumer typed later through `applyExpectedType` is skipped.
3. **Covered literals.** A literal gets no SEM3219 when an error was already reported at, inside or around its span. That covers `struct literal requires explicit type when assigning to int`, `int is not a struct`, `function returning nothing cannot return a value`, and an error inside one of its fields. It also covers a literal whose `let` annotation failed to resolve with an error. Error spans are recorded by `errorSpanRecorder`, which sits on the checker's own reporter chain (`check.go`). The speculative reporters that other code swaps in are not on that chain, so a buffered, discarded error covers nothing.
4. **Readers stay quiet.** `untypedLiteralRoot` follows an untyped expression back to its literal. It goes through a group, a field, a unary or binary operator, an array, tuple or ternary arm, and the unannotated `let` bound to the literal. Three sites reported on an unknown value even after a real error, and each now reports the literal instead, then stops:
   - the free-call failure path in `type_expr_calls.go`, which covers the single-candidate `expected T, got unknown`, generic inference and `no matching overload`;
   - the method-call failure path, for arguments;
   - the `has no method` check, for the receiver.

   Field reads, operators and `let` were already quiet on an unknown value.
5. **Expected types that were lost.**
   - `validateReturn` recorded a `return` inside a block expression, such as a `compare` arm, without applying the function's result type to an untyped literal. `stdlib/http/context.sg:134` (`return [];` in an arm of `query_all -> string[]`) was such a literal. Without this fix, every program that imports `stdlib/http` would be refused. It now takes the nearest function's result type (`enclosingFunctionReturnType`).
   - `applyExpectedType` had no tuple case. `let t: (Point, int) = ({ x: 1, y: 2 }, 2);` now types each untyped element from the annotation, then types the tuple from the elements' own types. So a wrong element is still reported: `cannot assign (Point, bool) to (Point, int)`.

**Consumers the brief asked about**, measured with `tools/run_probes.sh` (base against after). The table is `results/probes.txt` and the programs are in `probes/`.

| Consumer | Base | After |
|---|---|---|
| `let p = { x: 1, y: 2 };` (s01) | analysis abort, `expression 5 is not typed` | SEM3219 at the literal |
| `let p: Point = { ... }` (s02), `let a: int[] = []` (s19) | clean | clean |
| `let p: int = { ... }` (s03), `let p: Point? = { ... }` (s28) | SEM3015 | SEM3015, no second error |
| `return { ... }` into `-> Point` (s04) | clean | clean |
| `return { ... }` into `-> int` (s05), into `-> nothing` (s06) | SEM3015 | SEM3015, no second error |
| `return []` in a `compare` arm of `-> int[]` (row `typed_return_in_compare_arm`) | untyped, no error | typed, clean |
| argument `take({ x: 1, y: 2 })`, parameter `Point` (s07) | SEM3015 `expected Point, got unknown` | SEM3219, help ``name the struct: `Point { ... }` `` |
| argument, parameter `int` (s08); generic `id({ ... })` (s09) | SEM3015 `got unknown`; SEM3046 `no matching overload for id` | SEM3219 only |
| argument `[]`, parameter `int[]` (s20), `int[][]` (s37) | SEM3015 `expected [int], got unknown` | SEM3219, help ``let items: int[] = [];`` / ``int[][]`` |
| operand of a user `__add`, left (s10) and right (s11) | analysis abort | SEM3219 only |
| array elements `[{ x: 1 }, { x: 2 }]` (s12) | analysis abort | SEM3219 at each literal |
| array elements under `P[]` (s13) | clean | clean |
| tuple element `({ x: 1 }, 2)` (s14) | analysis abort | SEM3219 |
| tuple element under `(P, int)` (s15) | analysis abort | **clean** (typed from the annotation) |
| tuple element of the wrong type under `(P, int)` (s35) | analysis abort | SEM3015 `cannot assign (P, string) to (P, int)` |
| assignment to a typed binding (s16), index set (s30), field of a named literal (s32) | clean | clean |
| assignment `n = { x: 2 }` to an `int` (s17) | SEM3015 | SEM3015 |
| method argument `a.push({ x: 1 })` (s33) | SEM3015 `expected P, got unknown` | SEM3219 |
| method receiver `let mut a = []; a.push(1);` (s34) | SEM3005 `unknown has no method push` | SEM3219 at `[]` only |
| ternary arms under an annotation (s27), a map value under `Map<string, P>` (s31) | analysis abort | SEM3219 (these do not pass the expected type on; see section 8) |
| nested `{ i: { v: 1 } }` (s24) | analysis abort | SEM3219 at the outer literal only |
| field of an untyped binding (s25), expression statement (s26), `{}` (s21), `{ 1, 2 }` (s22) | analysis abort | SEM3219 |

The cascade probes `probes/cascade/c1`-`c5` use an unresolved name instead of a literal, and give the same output at the base and after. The checker's pre-existing cascades after an ordinary error are unchanged.

## 3. Goldens

Regenerated only for the four files below, with the commands `scripts/golden_update.sh` `generate_outputs` runs, from the repository root with `SURGE_STDLIB` set to it: `surge diag --format short --directives=off`, `surge tokenize`, `surge parse` and `surge fmt --stdout`. The script itself was not used. It regenerates the whole corpus and discards everything if any file fails, and at the base 107 unfinished programs already fail it. Regenerating again with the final compiler gave byte-identical files.

| Golden | Change | Why |
|---|---|---|
| `sema/valid/user_record_type.sg` | `fn anonymous_record()` removed. It keeps `Point`, its `__add`, and `add_coords`, which uses both literal forms with a type | The valid parts stay valid. `.diag` stays empty (exit 0), and `.ast`, `.fmt` and `.tokens` were regenerated. |
| `sema/invalid/record_literal_without_type.sg` (new) | the moved function, unchanged, under a `// scenario:` header | Each file tests one thing; this one is the rule. `.diag`: one SEM3219 at `{ x: 1, y: 2 }`. The readers `p.x`, `p.y` and `p_x + p_y` add nothing. |
| `sema/ownership_and_references/overload_autoref_temp_error.sg` → `sema/invalid/overload_autoref_temp_error.sg` | `{ x: 1, y: 2 } + p2` becomes `Point { x: 1, y: 2 } + p2`, plus a scenario line | **Becomes invalid, with the annotation.** The file's only `__add` borrows `self`, and its operand is a temporary. Once the literal has its type, the checker gives SEM3023 `cannot take reference to temporary value; bind it to a variable first`, which is the error the file's name promises. The untyped literal had hidden it: no `__add` was selected and the `.diag` was empty. The annotation keeps the autoref-temp case; being invalid is what that case is. |
| `sema/ownership_and_references/overload_autoref_temp_move.sg` → `sema/invalid/overload_autoref_temp_move.sg` | the same annotation, plus a scenario line | **Becomes invalid, with the annotation.** With both overloads, the temporary selects the by-value `__add(self: Point, other: Point)`. That is the "move" the name refers to, and its sibling `overload_autoref_function.sg` shows the same rule for a function. That overload also takes `p2` by value, so the file's last line, `let _ = p2.x;`, is SEM3130 `use of moved value 'p2'`. Keeping the file valid would have meant deleting that line, which changes what it tests. |

Nothing else under `testdata/golden` changes content. Section 4 names the one other golden whose output changed during the work, and how it was brought back.

## 4. Census

### Golden corpus

Commands, from the repository root, with a prebuilt compiler for each side. `D2TOOLS` is `cloud-work/d2-unfinished-plan/tools` from branch `cloud/d2-unfinished-plan`, and its `run_one.sh` and `classify.py` are used unchanged:

```
ROOT=<worktree at 1b122bfa> SURGE=<surge at 1b122bfa> OUTDIR=<census-base>  JOBS=3 D2TOOLS=... cloud-work/untyped-literal-error/tools/census.sh
ROOT=<worktree at 9792fef0> SURGE=<surge at 9792fef0> OUTDIR=<census-after> JOBS=3 D2TOOLS=... cloud-work/untyped-literal-error/tools/census.sh
python3 cloud-work/untyped-literal-error/tools/compare_census.py <census-base> <census-after>
```

`census.sh` is the wrapper from `cloud/anon-record-origin`. It diagnoses every `.sg` under `testdata/golden` in two forms, each with a 120 s timeout:

- the user form, `SURGE_STDLIB=$ROOT surge diag <file>`;
- the harness form, `surge diag --format short --directives=<off|collect> <file>`, which is what `scripts/golden_update.sh:185` runs.

`compare_census.py` lists the programs present on one side only, compares the class of the others (`ok`, `unfinished`, `diagnostics`, `other`), compares each unfinished program's rows, and compares the raw output bytes. The full output is `results/census.txt`. The base census was made at `1b122bfa` earlier the same day for `cloud/anon-record-origin`, from the same base commit.

| Form | base `1b122bfa`: ok / unfinished / diagnostics / other (1079 files) | after `9792fef0` (1080 files) |
|---|---|---|
| user form | 520 / 108 / 448 / 3 | 521 / 108 / 451 / 0 |
| harness form | 518 / 107 / 451 / 3 | 519 / 107 / 454 / 0 |

Every program whose verdict changes, by name:

| Program | Base | After |
|---|---|---|
| `sema/valid/user_record_type.sg` | other (`return origins: expression 31 is not typed`) | ok |
| `sema/ownership_and_references/overload_autoref_temp_error.sg` → `sema/invalid/overload_autoref_temp_error.sg` | other (`expression 25 is not typed`) | diagnostics (SEM3023) |
| `sema/ownership_and_references/overload_autoref_temp_move.sg` → `sema/invalid/overload_autoref_temp_move.sg` | other (`expression 38 is not typed`) | diagnostics (SEM3130) |
| `sema/invalid/record_literal_without_type.sg` (new) | — | diagnostics (SEM3219) |

- Unfinished programs whose rows changed: 0 of 108.
- Raw output differing byte-for-byte among the 1077 programs present on both sides: only `user_record_type.sg`, in each form.

**Found and fixed during the work.** The census over `893ffb30` showed one more changed output: `sema/invalid/generic_instantiation_wrong_arity.sg` gained SEM3219 beside its own `Pair expects 2 type argument(s), got 1`. For `let bad: Pair<int> = { first: 1, second: 2 };`, the annotation fails, so the literal gets no expected type. That is a cascade, and `9792fef0` covers it: a `let` whose written annotation resolves to no type, with an error reported while resolving it, marks its literal as covered (row `annotation_that_does_not_resolve`, CF8). The same census showed `sema/invalid/struct_literal_return_empty_for_nonempty.sg` in a different order in the user form. That is map-iteration order between two same-span errors (section 8); it did not recur in the final census.

**Hard stop.** No program moves from `ok` to refused. The two goldens that become refused are the relocated `overload_autoref_temp` files. At the base, their originals did not build: `surge build --backend llvm` and `--backend vm` on each stop with `Error: return origins: expression 25 is not typed` (`_error`) and `expression 38 is not typed` (`_move`). `user_record_type.sg` did not build at the base either (`expression 31 is not typed`, both backends), and now diagnoses clean.

### stdlib, core, showcases and benchmarks

`BASE=<surge at 1b122bfa> AFTER=<surge at 9792fef0> OUT=<dir> tools/extra_census.sh` diagnoses each of the 84 `.sg` files under `showcases/`, `benchmarks/`, `stdlib/` and `core/` as its own root, in the user form, with both compilers. The table is `results/extra_census.tsv`. All 84 print byte-identical output: 35 exit 0 on both sides, and 49 exit 1 on both sides. Before the fix to `return` in a block expression, `stdlib/http/context.sg:134` was the one SEM3219 in the stdlib; section 2 has the fix. The golden corpus's `core_stdlib/` copies are in the golden census above and do not change.

## 5. Tests

Command, run in a clean worktree at each commit:

```
go test -json -timeout 150m ./internal/sema/... ./internal/driver/... ./internal/diag/... ./internal/ownershipgate/... > <run>.json
python3 cloud-work/untyped-literal-error/tools/test_names.py base.json after.json
```

`SURGE_STDLIB` was not set. The base run of `internal/sema`, `internal/driver` and `internal/driver/diagnose` was made at `1b122bfa` earlier the same day (for `cloud/anon-record-origin`), and `internal/diag` and `internal/ownershipgate` at `1b122bfa` for this packet. The after run is at `9792fef0` and took 10m01s. The full name lists are in `results/tests.txt`.

| Package | base `1b122bfa` | after `9792fef0` |
|---|---|---|
| `surge/internal/sema` | pass | pass |
| `surge/internal/driver/diagnose` | pass | pass |
| `surge/internal/diag` | pass | pass |
| `surge/internal/driver` | fail: 34 top-level tests (56 entries) | fail: the same 34, the same 56 |
| `surge/internal/ownershipgate` | fail: 2 top-level tests (10 entries) | fail: the same 2, the same 10 |
| passing entries | 2703 | 2733 |
| failing only after | — | none |
| passing only after | — | `TestUntypedLiteralNeedsExpectedType` and its 28 rows, `TestUntypedLiteralArgumentMessage` (30 entries) |
| lost (passing only at the base) | none | — |

The 34 driver failures are those PLAN.md section 6 item 7 counts at `7131fb2`. The 10 ownership-gate entries are:

- `TestOwnershipRepresentativeCorpusPassesDevGate`, with 8 unfinished fixtures;
- `TestSyntheticSurgeStartInheritsRealEntrypointProvenance`.

**The allowlist step was not clean, and this is how it was found.** The suite run over `893ffb30` failed one more test, `TestRepositoryCompileFailureLedgerIsCompleteAndDebtLinked`: first on the pinned ledger size, and once the pin was moved, on `DEBT.md marker ownership-compile-failure:CF-007 has no compile failure group`. `10afc901` puts the allowlist back, and section 6 says what must land together with DEBT.md.

`go build ./...` and `go vet ./...` exit 0 at `9792fef0`. `gofmt -l` is clean on every changed Go file. It lists `internal/sema/return_origin_test.go`, which is unformatted at the base and is not touched here.

**New code, own test.** SEM3219 is pinned by `TestUntypedLiteralNeedsExpectedType` in `internal/sema/untyped_literal_test.go` (28 rows):

- 16 refused rows, each with exactly its SEM3219 at the literal, the expected help, and no other error;
- 7 typed rows with no diagnostic;
- 5 rows where an existing error covers the literal and no SEM3219 is added.

`TestUntypedLiteralArgumentMessage` pins the argument form's message, note and help word for word. The diag package's two code guard tests cover the number and the description entry.

### Counterfactuals

`tools/run_counterfactuals.py <logdir>` makes one change at a time, runs both new tests, and restores the files; `git status` was clean afterwards. The run on `9792fef0` is in `results/counterfactuals.txt`.

| CF | Change | Rows that go red |
|---|---|---|
| CF1 | the checker change reverted: all eight hooked sema files at `1b122bfa`, `untyped_literal.go` removed, the code constant kept so the tests compile | 17 of the 28 rows and `TestUntypedLiteralArgumentMessage`. They go red with the base's own silence (`got 0 SEM3219, want 1: <none>`) or its cascades (`expected Point, got unknown`, `unknown has no method put`, `no matching overload for id`) |
| CF2 | the sweep off (`reportUntypedLiterals` not called) | the 9 rows no consumer reports: `let`, both `__add` operands, array and tuple elements, nested, expression statement, positional and `{}`, and assignment |
| CF3 | the argument interception off (free and method calls) | `argument`, `empty_array_argument`, `argument_through_binding`, `method_argument`, `generic_argument`, `empty_array_let` and the message test, each back to `got unknown` or `no matching overload` |
| CF4 | the receiver interception off | `empty_array_let` and `method_receiver`, back to `unknown has no method put` |
| CF5 | the covered rule off | 11 rows: 2 or 3 SEM3219 at one literal, and SEM3219 beside SEM3015 or SEM3016 |
| CF6 | `return` in a block expression gets no expected type | `typed_return_in_compare_arm` (`got 1 SEM3219, want 0`) |
| CF7 | no tuple case in `applyExpectedType` | `typed_tuple_elements`, `tuple_element_mismatch` |
| CF8 | a failed `let` annotation does not cover its literal | `annotation_that_does_not_resolve` (`[SEM3015] Pair expects 2 type argument(s), got 1; [SEM3219] ...`) |
| control | none | all pass (`ok surge/internal/sema`) |

CF1 output, abridged:

```
untyped_literal_test.go:132: got 0 SEM3219, want 1: <none>
untyped_literal_test.go:132: got 0 SEM3219, want 1: [SEM3005] unknown has no method put; [SEM3015] expected [int], got unknown
untyped_literal_test.go:132: got 0 SEM3219, want 1: [SEM3015] expected Point, got unknown
untyped_literal_test.go:132: got 0 SEM3219, want 1: [SEM3046] no matching overload for id
...
--- FAIL: TestUntypedLiteralNeedsExpectedType (0.10s)
    --- FAIL: TestUntypedLiteralNeedsExpectedType/let_without_annotation (0.00s)
    --- FAIL: TestUntypedLiteralNeedsExpectedType/operand_of_user_add (0.00s)
    --- FAIL: TestUntypedLiteralNeedsExpectedType/argument (0.00s)
    ... (17 rows)
untyped_literal_test.go:159: diagnostics: [SEM3015] expected Point, got unknown
--- FAIL: TestUntypedLiteralArgumentMessage (0.00s)
FAIL	surge/internal/sema	0.109s
```

## 6. The ownership gate's compile-failure allowlist

`internal/ownershipgate/testdata/allowlist.json` lists the three fixtures as expected compile failures:

- `sema/ownership_and_references/overload_autoref_temp_{error,move}.sg` in group CF-007 (`RV2-DEBT-105`), with signature `MIR validation failed ... tmp_struct2: unknown type`;
- `sema/valid/user_record_type.sg` in group CF-010 (`RV2-DEBT-089`), with signature `... function anonymous_record: local L0 (p): unknown type`.

After this change none of them reaches MIR with an unknown type, so all three entries are stale. `893ffb30` removed them together with the two groups. But `TestRepositoryCompileFailureLedgerIsCompleteAndDebtLinked` checks the ledger both ways: DEBT.md rows 089 and 105 carry the markers `ownership-compile-failure:CF-010` and `CF-007`, and a marker without a group fails (`DEBT.md marker ownership-compile-failure:CF-007 has no compile failure group`). The brief forbids editing DEBT.md, so `10afc901` restores the allowlist to its base content, and the package fails exactly its base set again.

To land together with the DEBT.md edit of section 7:

- remove the three `compile_failures` entries and the groups CF-007 and CF-010;
- in `compile_failure_policy_test.go`, move the pin from `14/89` (debt 25) to `12/86` (debt 22), with a dated line saying why.

`893ffb30` contains exactly that allowlist edit. The tagged corpus test (`runtime_v2_ownership_corpus`) would report the three allowances as stale until then; it was not run (section 9).

## 7. Ledger text

These are proposed for `docs/runtime-v2-epics/DEBT.md`, which this packet does not edit. `RV2-DEBT-382` is the next free id at `1b122bfa`; renumber it if it is taken.

New row:

```
| RV2-DEBT-382 | **A STRUCT LITERAL WITHOUT A TYPE NAME, OR AN EMPTY ARRAY LITERAL, THAT NOTHING GIVES A TYPE WAS ACCEPTED BY THE CHECKER AND REFUSED LATER.** `typeExprStruct` (`internal/sema/type_expr_values.go:485-487`) and `typeExprArray` (`:298-300`) returned NoTypeID for `{ x: 1, y: 2 }` and `[]` when no consumer supplied an expected type, and reported nothing. The checker reads NoTypeID as an error already reported, so the binding, a field of it and a numeric operator over it stayed silent too (`enforceSameNumericOperands`), a user `__add` was never selected, and the program either stopped the return-origin analysis (`expression N is not typed`) or failed MIR validation (`unknown type`); three goldens recorded that silence as valid. Owner ruling 2026-09-25: enforce LANGUAGE.md §2.5 -- such a literal is a compile error, and there is no anonymous record type. FIXED on `cloud/untyped-literal-error` (`893ffb30`, `9792fef0`): the checker records each such literal and, after the walk, refuses every one still untyped with SemaLiteralNeedsType (SEM3219) at the literal, once, unless an error already covers it; a call argument, a method argument and a method receiver that are untyped because of such a literal report the literal in place of `got unknown`, `no matching overload` or `has no method`; a `return` inside a block expression now takes the function's result type (`stdlib/http/context.sg:134`), and a tuple literal its annotation's element types. A call argument still does not take its parameter's type (SEM3219 names the parameter's struct); ternary arms and map values under an annotation do not pass the expected type on either. Goldens: `sema/invalid/record_literal_without_type`, and `overload_autoref_temp_{error,move}` moved to `sema/invalid` with their struct named (SEM3023, SEM3130). Rows: `TestUntypedLiteralNeedsExpectedType` (28 rows) and `TestUntypedLiteralArgumentMessage` in `internal/sema/untyped_literal_test.go`; counterfactuals in `cloud-work/untyped-literal-error/PACKET.md`. | Closed when the branch lands | Sema (`internal/sema/untyped_literal.go`) | Done: every such literal is typed from its context or refused with SEM3219 at the literal, and no reader of it adds a second error; the census moves no program that builds at the base. |
```

Updates to existing rows. Both carry an ownership-gate marker, so each must land in the same commit as the allowlist edit of section 6:

- **RV2-DEBT-105** (Open, CF-007). Close it. Its premise, "otherwise-valid" overload fixtures, no longer holds: the literal has no type, so no overload can be selected for it. With the struct named, the two fixtures are invalid goldens that refuse at sema (SEM3023 and SEM3130), and neither reaches MIR. Remove the `ownership-compile-failure:CF-007` marker, as CF-007 is retired.
- **RV2-DEBT-089** (Fixed for the gate, open for the missing type on anonymous-record member access). Close the open half. Anonymous-record member access no longer reaches any later consumer, because the literal it reads is refused (SEM3219) and its readers stay quiet. Remove the `ownership-compile-failure:CF-010` marker, as CF-010 is retired.

## 8. Found on the way

- **Another silent untyped gap, not a literal.** A `compare` statement whose arms all `return` is left untyped with no report (`stdlib/http/cookie.sg:177`). Diagnosing `stdlib/http` as its own root stops the return-origin analysis there (`expression 2172 is not typed in cookie.sg`), at the base and after alike. It is out of this packet's scope and was found with a throwaway build whose abort message printed the expression's kind and span.
- **Arguments do not take their parameter's type.** LANGUAGE.md §2.5 speaks of an "unambiguous expected struct type", and a single non-generic candidate's parameter might count as one. This packet does not type arguments from parameters, because that would make programs compile that the base refuses (s07). They are refused with SEM3219, whose help names the parameter's struct. That is for the owner to widen or not.
- **Ternary arms and map values** under an annotation do not pass the expected type on (s27, s31). At the base they were silent and stopped the analysis; now they are refused with SEM3219.
- **Nondeterministic output at the base, not from this change.** Diagnosing `stdlib/fs/` and `stdlib/path/` as directories prints different bytes on two runs of the same base compiler. The user form of `sema/invalid/struct_literal_return_empty_for_nonempty.sg` orders its two same-span errors by map iteration; the harness form sorts them and is stable.
- **The ownership corpus golden-count pin is already stale.** It is 1074, while the base has 1079 golden `.sg` files. This change adds one, making 1080; the pin was not moved.

## 9. Not verified

- **The tagged ownership corpus** (`go test -tags runtime_v2_ownership_corpus ./internal/ownershipgate`, `make runtime-v2-ownership-check`) was not run. It would read the three stale allowances (section 6) and the golden count (section 8).
- **`make golden-check`** was not run. It regenerates the whole corpus and cannot pass at the base (section 3).
- **Other packages' suites** (`internal/hir`, `internal/mir`, `internal/vm`, `internal/backend/llvm`, `internal/buildpipeline`, `internal/lsp`) were not run. Their Go test literals are not in the census; a test program there with an unannotated literal would now be refused by sema.
- **Module-qualified calls** such as `Mod.f({ ... })` go through `moduleFunctionResult`, which has its own `no matching overload` report (`module_imports.go:206`). That path is not intercepted and was not measured, so such a call may print both that error and SEM3219.
- **Speculative checker paths** that swap the reporter were covered by reasoning only: they are not on the recording chain.
- **No quick fix** is attached to SEM3219. `surge fix` offers nothing for it.
