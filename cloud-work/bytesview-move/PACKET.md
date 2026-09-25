# A live BytesView blocks moving or reassigning the string it views

Base `daeb08e` (`origin/validation/step7-d2-on-d1`, contains `daeb08ed`). Branch `cloud/bytesview-move`.
No new diagnostic code: the rule reuses the window's own codes, SEM3020 (move while borrowed) and
SEM3019 (mutation while borrowed).

Documents read:
- `docs/RUNTIME_V2.md`
- `docs/RUNTIME_MODEL_EXPLAINED.ru.md`
- `docs/runtime-v2-epics/DEBT.md` (rows 365 and 368)
- `docs/RUNTIME.md`
- `docs/LANGUAGE.md`

`docs/CONCURRENCY.md` (v1) was not used.

The ruling, as recorded in `docs/runtime-v2-epics/DEBT.md` (row RV2-DEBT-365, the P1u-TC packet
paragraph):

> "… a `BytesView` (a borrow of a string, by the owner's ruling of 2026-09-15) …"

## 1. The rule, and why it matches windows

### How a window blocks its base today (read from code, measured)

`typeExprIndex` (`internal/sema/type_expr_calling.go`) types `xs[[a..b]]` through the selected
`__index`. It borrows the target for the receiver, and it **never releases that borrow** at the
call:

```go
		if !isAddressOfOperand {
			tc.applyParamOwnership(sig.Params[0], idx.Target, container, tc.exprSpan(idx.Target))
			tc.applyParamOwnership(sig.Params[1], idx.Index, indexType, tc.exprSpan(idx.Index))
		}
		if tc.isArrayRangeIndex(container, indexType) {
			tc.markArrayViewExpr(id)
			...
		return resultType
```

- There is no `dropImplicitBorrowForRefParam` on this path.
- A method call does have one: `dropImplicitBorrowForRefParam` releases the receiver's `&` borrow
  (`"temp_borrow"`) unless the result `carriedReferenceType` (`implicit_borrow.go:122-140`).
- A `BytesView` is a struct, so `carriedReferenceType` says no, and the `&string` receiver borrow of
  `s.bytes()` was dropped at the call. That is the defect.

Measured on the base, the window borrow **lasts until the end of the enclosing block, not the
window's last use**:

| Window or reference | Base |
|---|---|
| `let w = xs[[0..2]]; sink(own xs); w[0]` | SEM3020 at `own xs` |
| `let mut xs…; let w = xs[[0..2]]; xs = [7, 8, 9]; w[0]` | **SEM3019** at the assignment |
| `let w = xs[[0..2]]; let f = w[0]; sink(own xs);` (w dead) | SEM3020 (lexical) |
| `let r = &s; let n = len(r); sink(own s);` (r dead) | SEM3020 (lexical) |
| `{ let w = xs[[0..2]]; let x = w[0]; first = x; } sink(own xs);` | accepted |
| `{ let r = &s; n = len(r); } sink(own s);` | accepted |
| `let first = xs[[0..2]][0]; sink(own xs);` | SEM3020 (and SEM3023) |
| `fn win(xs: &int[]) -> int[] { return xs[[0..2]]; }` then `let w = win(&xs); sink(own xs); w[0]` | **accepted** (see Q-BV2) |

### The change

In `internal/sema/implicit_borrow.go`:

```go
func (tc *typeChecker) viewedStringBorrowOutlivesCall(sym *symbols.Symbol, param symbols.TypeKey, result types.TypeID) bool {
	if sym == nil || !coreDeclaredSymbol(sym) || tc.types == nil || !tc.types.IsBorrowedView(result) {
		return false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(tc.typeFromKey(param)))
	return ok && tt.Kind == types.KindReference && !tt.Mutable && tc.resolveAlias(tt.Elem) == tc.types.Builtins().String
}

func coreDeclaredSymbol(sym *symbols.Symbol) bool {
	if sym.Flags&symbols.SymbolFlagBuiltin != 0 {
		return true
	}
	path := strings.Trim(sym.ModulePath, "/")
	return sym.Flags&symbols.SymbolFlagImported != 0 && (path == "core" || strings.HasPrefix(path, "core/"))
}
```

It is used at the two places that release a call's `&` borrow: the method-receiver path
(`type_expr_calling.go`) and the argument path (`dropImplicitBorrowsForCall`).

```go
		if len(sym.Signature.Params) > 0 && !tc.viewedStringBorrowOutlivesCall(sym, sym.Signature.Params[0], resultType) {
			tc.dropImplicitBorrowForRefParam(member.Target, sym.Signature.Params[0], receiverType, resultType, tc.exprSpan(member.Target))
		}
...
		if !tc.viewedStringBorrowOutlivesCall(sym, sig.Params[paramIndex], result) {
			tc.dropImplicitBorrowForRefParam(arg.expr, sig.Params[paramIndex], arg.ty, result, tc.exprSpan(arg.expr))
		}
```

### The parallel, point by point

| | Array window | BytesView (after) |
|---|---|---|
| Made by | the slice operation (`__index` with a `Range<int>`), a core operation | core's view producers (section 2): a core-declared callee returning the core `BytesView` |
| Borrow taken | `applyParamOwnership` on the target (`&` receiver) | `applyMethodReceiverOwnership` / `applyParamOwnership` on the `&string` (unchanged) |
| Borrow released at the call | never | never (this change) |
| Lifetime | the enclosing block (the borrow's scope), not the view's last use | the same borrow, the same scope |
| Move of the owner | SEM3020 | SEM3020 |
| Reassignment of the owner | SEM3019 | SEM3019 |
| Made through a user function | not tracked (the result is not a carried reference) | not tracked (a user callee is not core-declared) |

- **Identity.**
  - `IsBorrowedView` holds only for the struct `type_decl_core.go` marks by declaration: builtin,
    `@intrinsic`, 3 fields, in a core runtime module. A user struct named `BytesView` is not it.
  - `coreDeclaredSymbol` accepts a builtin symbol or one imported from module path `core/…`:
    - `@intrinsic` functions are refused outside module `core` (`resolve_declarations.go`
      `moduleAllowsIntrinsic`).
    - A module path `core/…` is admitted only for files inside the stdlib root (driver
      `validateCoreModule`).
    - Measured: `bytes` arrives as `module="core" flags=imported|public|method`, and
      `rt_string_bytes_view` inside core as builtin.
- **No name is read.**

**On the brief's code.** The brief asks for SEM3020 at "the move or assignment". For a
reassignment, windows and references give **SEM3019** ("cannot mutate 's' while it is
shared-borrowed"), not SEM3020. To stay exactly as strict as windows, a reassignment under a live
view is SEM3019 too. No new code was added.

## 2. The APIs that produce a view of a string's bytes

Everything in `core/` and `stdlib/` whose result type is `BytesView` (searched):

| API | Where | Borrow kept |
|---|---|---|
| `string.bytes(self: &string) -> BytesView` | `core/string.sg:98` | the receiver `s` |
| `rt_string_bytes_view(s: &string) -> BytesView` (`@intrinsic`, core-internal) | `core/intrinsics.sg:121` | its argument |

- **No API takes a view of a view.** `extern<BytesView>` offers `__len` and `__index` (bytes), and
  none returns a `BytesView`.
- **A copied view** (`let v2 = v;`) is covered by the same rule, because the borrow belongs to the
  block, not to the binding (row `copied_view_still_blocks`).

## 3. Rows (`internal/driver/bytes_view_move_test.go`)

These are full-pipeline runs with the real core. A refusal row requires exactly one error with the
named code at the named span. A control requires the checker to accept: the run finishes, or stops
in return-origin analysis, which runs only after a clean checker. (Every BytesView control still
meets return-origin's own row for `v[0]`, "index requires its selected container transfer", on
this base.)

| Row | Program | Want |
|---|---|---|
| `move_while_view_lives` | `let v = s.bytes(); sink(own s); return v[0];` | SEM3020 at `own s` |
| `view_passed_on_after_move` | `let v = s.bytes(); sink(own s); return read(v);` | SEM3020 at `own s` |
| `reassign_while_view_lives` | `let v = s.bytes(); s = "c" + "d"; return v[0];` | SEM3019 at the assignment |
| `copied_view_still_blocks` | `let v = s.bytes(); let v2 = v; sink(own s); return v2[0];` | SEM3020 |
| `dead_view_in_same_block` | `let v = s.bytes(); let first = v[0]; sink(own s);` | SEM3020 (lexical, as the window) |
| `window_twin_move` / `window_twin_reassign` | the same shapes over `xs[[0..2]]` | SEM3020 / SEM3019 (unchanged; the parallel) |
| control `view_block_ends_before_move` | `{ let v = s.bytes(); first = v[0]; } sink(own s);` | accepted |
| control `view_rederived_after_reassignment` | `{ v = s.bytes() … } s = "c" + "d"; { let v2 = s.bytes(); … }` | accepted |
| control `bytes_copied_into_array_before_move` | `{ let v = s.bytes(); copy.push(v[0]); } sink(own s); return copy[0];` | accepted |
| control `no_view_no_borrow` | `let n = len(&s); sink(own s);` | accepted |

Also measured with `surge diag` on both binaries:
- `let n = s.bytes().__len(); sink(own s);` gets SEM3023 on both. After, it also gets SEM3020, as
  the window twin `xs[[0..2]][0]` does.
- `fn view(s: &string) -> BytesView { return s.bytes(); }` then `let v = view(&s); sink(own s);`
  is accepted by the checker on both, as is the window twin (Q-BV2).

### Counterfactual: the two sema files reverted to `daeb08e`

Command: `go test -count=1 ./internal/driver -run 'TestBytesView' -v`.

```
--- FAIL: TestBytesViewBlocksMovingItsString
    --- FAIL: .../move_while_view_lives
    --- FAIL: .../view_passed_on_after_move
    --- FAIL: .../reassign_while_view_lives
    --- FAIL: .../copied_view_still_blocks
    --- FAIL: .../dead_view_in_same_block
    --- PASS: .../window_twin_move
    --- PASS: .../window_twin_reassign
--- PASS: TestBytesViewControlsStayAccepted (4)
```

All five BytesView refusals go red. The window twins stay green, which shows the change reuses
their rule and does not alter it.

## 4. Census

The tools are `run_one.sh` and `classify.py` from `cloud-work/d2-unfinished-plan/tools/`, run over
**1163 files**: every `.sg` under `testdata/golden`, `stdlib/`, `core/`, `showcases/` and
`benchmarks/`, in both forms, with 4 jobs:

```
find testdata/golden stdlib core showcases benchmarks -type f -name '*.sg' | LC_ALL=C sort > files.txt
export ROOT=$PWD OUTDIR=<dir>/<side> SURGE=<surge-side>
xargs -a files.txt -P 4 -I{} bash tools/run_one.sh {} > <dir>/<side>/status.tsv
python3 tools/classify.py $ROOT <dir>/<side> <dir>/<side>/census.json <side>
```

| | ok | unfinished | diagnostics | other |
|---|---:|---:|---:|---:|
| user form, base and after | 579 | 112 | 449 | 23 |
| harness form, base and after | 577 | 111 | 452 | 23 |

- **No program becomes refused.** No class changes in either form, and no row changes.
- **Other output differences:** the only one is
  `crossing/block04/invalid/movable_negative_nested_unmarked_user_field.sg`, which differs in line
  order only (known output noise).
- **No hard stop.** No building program became refused, and no stdlib, core or golden source needs
  a change. `stdlib/json` (which holds `BytesView` values) and every program importing it are
  unchanged.

## 5. Build and suites

`go build ./...` and `go vet ./...` pass (exit 0).

Command: `go test -count=1 -timeout 30m ./internal/sema ./internal/driver -json`, run in a worktree
at `daeb08e` and in the work tree. Failing names are the `"Action":"fail"` events that carry a `Test`.

| | internal/sema | internal/driver | failing test entries | passing test entries |
|---|---|---|---:|---:|
| base `daeb08e` | ok | FAIL (731 s) | 55 | 2668 |
| after | ok | FAIL (709 s) | 55 | 2681 |

- **Failing-name sets:** after minus base is empty, and base minus after is empty. The 55 are the
  known base failures.
- **Test-entry difference:** exactly this packet's 13 new entries (2 tests and 11 leaves), all
  passing. No entry disappeared.
- **Tripwires, after:** `TestH2Tripwire*` 40/40, `TestAnalyzeTaskAwaits` 10/10 and `TestTaskCheck*`
  247/247 all pass.

## 6. Ledger row text

> **RV2-DEBT-NNN — a live BytesView did not block moving or reassigning its string. Open → Closed by
> packet bytesview-move (2026-09-25).**
> - **Ruling:** by the owner's ruling of 2026-09-15, a `BytesView` borrows the string it views. The
>   checker refused it escaping (SEM3139), but released the `&string` borrow of `s.bytes()` at the
>   call (`dropImplicitBorrowForRefParam`: a struct result carries no reference).
> - **The hole:** `sink(own s)` and `s = …` while the view lived passed the checker. Only return-origin's
>   fail-closed rows on `v[i]` and `len(v)` stopped them, and without those rows they read freed
>   memory (VM3301 on the VM, a wrong byte natively).
> - **The fix:** for core's view producers (`string.bytes()`, `rt_string_bytes_view`; a core-declared
>   callee returning the core `BytesView`), the borrow is now kept as an array window keeps its base
>   borrowed. The same codes follow: SEM3020 for a move, SEM3019 for a reassignment. The same
>   lexical lifetime applies: to the end of the block that made the view.
> - **Witnesses:** `internal/driver/bytes_view_move_test.go`. The census over golden, stdlib, core,
>   showcases and benchmarks is unchanged.
> - **Open, as for windows:** a view returned through a user function (Q-BV2).

## 7. Unverified

- **The runtime effect.** No run of the three witnesses without return-origin's rows was repeated
  here. The brief's VM3301 and native wrong-byte measurements are the coordinator's. After this
  change the witnesses stop at the checker, so they do not reach the runtime.
- **The user-function window twin** (Q-BV2) builds clean on the base and prints the right value on
  both backends (`97`, 1 run each). Whether it is unsafe is **not** established. The runtime's view
  registry for dynamic arrays may pin the storage, so this is a question, not a claimed defect.

## 8. Owner questions

**Q-BV1. Liveness or block scope?**

- **Plain words.** The brief asks that a view which is dead before the move be accepted, "liveness,
  not lexical scope, if that is what windows use". Windows and references use **block scope**:
  - `let w = xs[[0..2]]; let f = w[0]; sink(own xs);` is SEM3020 today, although `w` is dead.
  - So the BytesView version is SEM3020 too (row `dead_view_in_same_block`).
  - A view in a block that ends before the move is accepted (row `view_block_ends_before_move`).
- **Example.** `let v = s.bytes(); let first = v[0]; sink(own s); return first;` gives SEM3020 at
  `own s`.
- **The invariant.** The borrow's end is its scope, and `typeExprIndex` never releases a window's
  base borrow at the call (quoted in section 1). There is no liveness analysis for borrows in this
  checker. `docs/LANGUAGE.md` records lexical lifetimes (RV2-DEBT-041: "Lexical lifetimes + no
  full borrow-checker (LANGUAGE.md)").
- **Options.**
  - **A. Keep block scope** (this packet): exactly as strict as windows and references. Cost: the
    brief's "dead before the move" control is accepted only in block form.
  - **B. Liveness for views only:** end a view's borrow at its last use. Cost: new flow machinery for
    BytesView alone, and views and windows would then disagree.
  - **C. Liveness for all borrows:** a borrow-checker change across the language.
- **Recommendation: A.**
- **What not to do until answered:** do not end the BytesView borrow early for some shapes only.
- **The question.** Should a BytesView's borrow of its string end at the block (as windows and
  references do today), or at the view's last use?

**Q-BV2. Views made through a user function.**

- **Plain words.** A window or BytesView returned by a user function does not keep its source
  borrowed in the caller, because the function's result is not a carried reference.
- **Example.** `fn win(xs: &int[]) -> int[] { return xs[[0..2]]; }` then
  `let w = win(&xs); sink(own xs); return w[0];` builds clean on the base and prints `97` on both
  backends. The BytesView twin passes the checker and stops only in return-origin analysis.
- **The invariant.** `dropImplicitBorrowForRefParam` keeps an argument's borrow only when the
  result carries a reference (`implicit_borrow.go:122-140`). Neither an array nor a `BytesView` is
  one.
- **Options.**
  - **A. Leave both** as today (this packet): no wider than windows.
  - **B. Treat a call's `&` argument as borrowed while a returned view lives**, for windows and
    views alike. Cost: a new checker rule, and a census re-measure.
  - **C. Measure first** whether the array case is unsafe at all, since the view registry may pin it.
- **Recommendation: C, then B** if unsafe.
- **What not to do until answered:** do not widen this packet's rule to user functions for BytesView
  alone.
- **The question.** Should a view returned through a user function keep its source borrowed in the
  caller?
