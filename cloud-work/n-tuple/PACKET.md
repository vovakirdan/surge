# Packet N-TUPLE: return-origin transfer for tuple index and destructuring

Base `1b122bfa` (`origin/validation/step7-d2-on-d1`), branch `cloud/n-tuple`. Everything below was measured unless it says "(read from code)".

## What changed

- **`internal/sema/return_origin_tuples.go` (new).** It holds the element read `t.N` (expression kind 12) and the destructuring `let (a, b) = v`.
- **`internal/sema/return_origin_expr.go`.** A `case ast.ExprTupleIndex` sends `t.N` to `tupleIndex`. Before this packet it fell into the catch-all "expression kind 12 needs an origin transfer".
- **`internal/sema/return_origin_stmt.go`.** A `let` with a pattern now evaluates its value and then calls `destructure`. Before, it raised "destructuring needs projected origin facts" unconditionally.
- **`internal/driver/return_origin_tuples_test.go` (new).** It adds 30 rows in three tests.

## The transfer rule, and why it is sound

A tuple's value does not keep its elements' origins apart. The constructor joins every child into one value, and a reference-free tuple keeps nothing, because the G6-i guard refuses each loan-carrying child (`return_origin_constructors.go`, `constructorChildren`):

```go
erased := b.shape(id) == returnOriginRefFree
...
if erased && b.shape(child) == returnOriginRefFree {
	b.discardLoans(next.value, node.Span) // G6-i, per child
}
...
contents = contents.join(next.value)
```

`discardLoans` (`return_origin_backing.go:252-259`) raises "storage loan would be discarded by a payload-free value" for any local or parameter root. Since a tuple holds at most the join of its children, `tupleElementValue` gives an element:

1. **Nothing**, when its type can hold no reference and no storage loan (reference-free shape and not a `loanCarrier`). This is the rule the member read already uses for a reference-free field: `if b.shape(id) == returnOriginRefFree { out.value = returnOriginValueOf() }` in `return_origin_expr.go`.
2. **Every root of its tuple** otherwise. That is a superset of its own roots, and it never drops one. This covers a reference-carrying element such as `Some::<&int>(&x)`, and a loan carrier such as a window, including a window inside a reference-carrying tuple, where G6-i does not apply. In a generic body, `returnOriginView(fn).shape` treats a template parameter as reference-carrying, so `first<T>(t: (T, int)) -> T` keeps its parameter source.
3. **A named refusal** in three cases:
   - "tuple element read through a reference needs its referent's contents", when the tuple is reached through `&` or `*` and the element can hold something. The frame has no origin for the referent's contents.
   - "tuple element that holds a callable needs its callable transfer".
   - "tuple element needs its concrete element type", for an unresolved shape, a missing tuple type, or an element type that disagrees with the checker's.

**Storage.** An element's storage is its tuple's storage. Through a reference it is the referent, as the member read does. So `&t.0` borrows `t` as `&t` does, and `&r.0` returns the parameter `r` (row `borrow_of_an_element_through_a_reference`, parameter source 0).

**Destructuring** reads only the patterns the checker accepts (`bindTuplePattern`, `type_checker_bindings.go:248-303`: identifiers and nested tuples). It resolves the subject with the same helper, stripping aliases and `own`; `&` and `*` make it indirect. Each name gets exactly what `t.N` would give. A nested pattern recurses on the element's value. `_` has no symbol and binds nothing. A name whose recorded binding type disagrees with its element type is refused. Every refusal still binds its names to an unknown value, so a later read stays unfinished rather than reading an unbound name.

**Language.** The language is not widened. The checker already accepted every form here: tuple index, `let` destructuring, destructuring through `&`, and `own t.N`. Only the return-origin analysis refused them.

`docs/LANGUAGE.md:563` still says "Tuple destructuring in `let` bindings is not supported yet", but the checker has `checkTupleLet` and `bindTuplePattern`, and three golden `sema/valid` programs use it. The document is stale. That is for the owner; this packet does not change the checker.

## Rows (`internal/driver/return_origin_tuples_test.go`)

Every source is a root program against the real core, with frozen sha256 digests and spans derived from unique substrings.

- **`TestAnalyzeTuples`: 19 leaves, each clean with an exact summary.**
  - The six programs' shapes: `swap`, `get_first`, `get_second` (`own t.1`), `nested_access` (`t.0.1`), `destruct_pair`, `destruct_call` (from a call result), `own_nested` (with `own nested.0` and a 1-tuple), and `compare_pair` (t15's shape).
  - Further clean forms:
    - nested destructuring
    - `_`
    - `&r.0` through a reference, with parameter source 0
    - destructuring through a reference with scalar elements
    - generics, both `first<T>` and its use
    - a scalar store into an element followed by reads
  - Rows that must keep the reference origin, each with parameter source 0: `keep_index`, `keep_destructure`, `keep_nested` and `keep_param`.
- **`TestAnalyzeTupleRefusals`: 5 leaves, each with an exact named row.**
  - destructuring through `&(Option<&int>, int)`: referent row
  - `own r.0` through a reference: referent row
  - a callable element: callable row
  - a window in a reference-free tuple: the G6 row stays
  - a store of `Some::<&int>(&x)` into `t.0`: the store row stays
- **`TestTupleEscapeIsReported`: 6 soundness canaries, each SEM3139 for its dying local.**
  - `own t.0`, destructuring, nested index and nested destructuring of `Some::<&int>(&x)`
  - `own t.1` and destructuring of a window `a[[0..2]]` inside a reference-carrying tuple

The 12 canaries I used in the plan (`f01` to `f20`) are not repeated. `f01` and `f02` are still refused by typing with SEM3138 and SEM3015; a reference cannot sit directly in a tuple.

## Counterfactuals

All three were run against the rows above, and the code was restored afterwards.

| Change | Result |
|---|---|
| Transfer reverted: the kind-12 case removed and the destructuring row restored | 27 of 28 leaves red. The survivor, `swap_builds_a_tuple`, has neither a tuple read nor destructuring. |
| Element projection replaced by "fresh" | 11 of 28 red: all 6 escape canaries (`missing SEM3139 ... []`, each leak clean) and the 5 origin-keeping rows (parameter source lost) |
| Only a loan-carrying element made fresh | exactly the 2 window canaries red |

These ran before I added the two store rows, so there are 28 leaves rather than 30.

## Census

I diagnosed every `.sg` under `testdata/golden` (1079 files), in both command forms, with the base and the after binaries, using `cloud-work/d2-unfinished-plan/tools/run_one.sh` and `classify.py` from branch `cloud/d2-unfinished-plan`.

| | user form: ok / unfinished / diagnostics / other | harness form: ok / unfinished / diagnostics / other |
|---|---|---|
| base | 520 / 108 / 448 / 3 | 518 / 107 / 451 / 3 |
| after | 526 / 102 / 448 / 3 | 524 / 101 / 451 / 3 |

- **Exactly the six programs move from unfinished to ok** in both forms: `hir/tuples.sg`, `sema/valid/tuple_access.sg`, `sema/valid/tuple_destructure.sg`, `sema/valid/tuple_destructure_call.sg`, `vm_async_suite/t15_fairness_round_robin.sg` and `vm_tuples/tuple_literals.sg`.
- **No other unfinished program changes its root reasons.**
- **One deviation in byte output, not caused by this packet.** In the user form (pretty format), `sema/invalid/struct_literal_return_empty_for_nonempty.sg` prints its two SEM3015 lines in swapped order. The order varies on the base binary alone: 3 of 20 runs start with `name` and 17 with `value`, against 1 and 19 after. The checker prints these lines before return-origin runs, and the harness-form output is identical.

To reproduce:

```
OUT=$(mktemp -d); mkdir -p $OUT/out $OUT/hout
find testdata/golden -type f -name '*.sg' | LC_ALL=C sort > $OUT/files.txt
ROOT=$PWD OUTDIR=$OUT SURGE=/tmp/surge xargs -a $OUT/files.txt -P 4 -I{} bash tools/run_one.sh {} > $OUT/status.tsv
python3 tools/classify.py $PWD $OUT $OUT/census.json
```

## Tests

| Suite | Base `1b122bfa` | After |
|---|---|---|
| `go build ./...`, `go vet ./...` | | clean |
| `go test ./internal/sema -count=1` | ok | ok |
| `go test ./internal/driver -count=1 -timeout 55m` | FAIL, 56 failing entries | FAIL, the same 56 entries |

The failing-name sets are equal, so the only difference is the new passing tests. The 56 entries are the pre-existing failures recorded in `cloud-work/d2-unfinished-plan/PLAN.md` §6 item 7, re-measured at `1b122bfa`.

`SURGE_BEHAVIOUR_BACKENDS=vm,llvm go test ./internal/vm -run 'TestVMAsyncSuiteGolden|TestVMTuplesGolden'`:
- **Base:** 12 failing entries. t14, t15, t22 and t23 fail on both backends, and so does `TestVMTuplesGolden`, because the programs do not build.
- **After:** 7 failing entries. t15 and `tuple_literals` now pass on the VM and LLVM. t14, t22 and t23 are other packets' programs.

## Runtime runs

Of the six programs, only `t15_fairness_round_robin` and `tuple_literals` have a golden `.out`. Each was run 20 times per backend with `surge run --backend vm|llvm`.

| Program | VM | LLVM |
|---|---|---|
| `t15_fairness_round_robin` (sidecar `.order-backends`: `vm`) | 20/20 byte-equal to `.out`, exit 0 | 20/20 equal as a multiset of lines (3 of them also byte-equal), exit 0 |
| `tuple_literals` | 20/20 byte-equal, exit 0 | 20/20 byte-equal, exit 0 |

The sidecar asks for byte comparison only on the VM. Native is compared as a multiset, per the owner's ruling of 2026-08-31 recorded in the sidecar.

## Ledger text for RV2-DEBT-365, residual paragraph

> **N-TUPLE (cloud/n-tuple, over `1b122bfa`).** Return-origin now answers expression kind 12 (`t.N`) and `let` destructuring (`internal/sema/return_origin_tuples.go`) instead of refusing them. An element that can hold no reference and no storage loan holds nothing: a reference-free tuple carries no loan, because G6-i refuses each loan-carrying child at construction. Any other element holds every root of its tuple. An element reached through a reference keeps the named row "tuple element read through a reference needs its referent's contents", and a callable element keeps "... needs its callable transfer". No residual R-a to R-j is touched; tuples hold no Task that return-origin would have to see into. Six golden programs move from unfinished to ok and no other program changes class. `t15_fairness_round_robin`, which ST-RUNOUT recorded as "still unfinished and not a runtime result", now builds and runs to its golden output, byte-equal on the VM and as a multiset on LLVM, 20 of 20 each, and joins the runner-matrix watch. Rows: `TestAnalyzeTuples`, `TestAnalyzeTupleRefusals` and `TestTupleEscapeIsReported`, whose six SEM3139 canaries go red when the element projection is made fresh.

## Not verified

- **Tuples holding a `Task` or a `Channel`.** The rule would give such an element the tuple's roots, and I did not write a row for it. What a Task holds is the task check's, as ruled 2026-09-15 in DEBT.md:229, and the task check was not exercised with tuples.
- **The coordinator's tripwire probes** were not re-run beyond the full driver suite, whose 10 `TestH2Tripwire*` tests are unchanged, and the two VM corpus tests.
- **A tuple holding a function value** keeps its existing refusals ("constructed result needs concrete borrowed-content facts" at construction) plus the new callable row. Calling `(t.0)(x)` aborts the analysis with "expression N is not typed" on the base and after alike. That abort is not addressed here.
- **Raw-pointer subjects** (`*(A, B)`) are handled like references, as indirect. That comes from reading the code only; I wrote no row for it.
