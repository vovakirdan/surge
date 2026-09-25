# Fix: compare exhaustiveness counts only irrefutable, unguarded arms

Owner ruling 2026-09-25, option B. Base `045c113` (branch `validation/step7-d2-on-d1`), work branch
`cloud/compare-exhaustive`.

## 1. The rule, as implemented

`internal/sema/type_expr_compare.go` `consumeCompareMembers` now drops a union member only when
the arm is certain to catch it:

- A `finally` arm consumes everything left, as before.
- An arm with an `if` guard consumes **nothing**.
- An unguarded arm consumes the members its pattern names (`matchedUnionMembers`, unchanged),
  **and only those whose payload the pattern accepts in full** (`irrefutablyMatchedMembers`,
  new file `internal/sema/type_expr_compare_irrefutable.go`).

The rule, quoted from the header comment of `type_expr_compare_irrefutable.go`: a pattern is
**irrefutable** against a type exactly when it is one of:

- `_`;
- a named binding (an identifier that is not `_`, `nothing` or a tag, the existing
  `isNamedBindingPattern`);
- a tuple pattern of the tuple's arity whose every element is irrefutable against its
  element type;
- a tag pattern (`T`, `T(p1, ..., pn)`, `mod.T(...)`) or `nothing` that names every member
  of the type's union (so the union has that one member), and whose every payload
  sub-pattern is irrefutable against its payload type.

Everything else is **refutable**:
- literals (numbers, strings, bools);
- enum variants (`Color.Red`);
- a tag of a union with more than one member (`Some(Some(x))` over `Option<Option<T>>`);
- any other expression form, including a parenthesised pattern.

`consumeCompareMembers` has three callers, and all three now use the strict count:
1. `checkCompareExhausiveness`, which decides exhaustiveness and the redundant-`finally` warning.
2. `compareAlwaysMatches`, which feeds the missing-return and abrupt-result analysis.
3. The arm loop in `type_expr_flow.go`, which narrows each arm's subject.

The third is what fixes the untyped repeated-tag binding (section 6).

### What changes and what does not

| Case | Before | After |
|---|---|---|
| Variant no arm names | SEM3053 | SEM3053 (unchanged; computed with the old "mentioned" count) |
| Variant named only by guarded arms | accepted, silent `default()` at run time | **SEM3219** |
| Variant named only by refutable-payload arms (`Some(1)`, `Some(Some(x))`, `Some((1, y))`) | accepted, silent `default()` | **SEM3219** |
| The same, followed by `Some(_)` / `Some(k)` / `_` / binding | accepted | accepted |
| The same, followed by `finally` | accepted; SEM3054 "redundant finally" if every variant was mentioned | accepted, **no** SEM3054 (that `finally` is what closes it) |
| `finally` after irrefutable coverage of every variant | SEM3054 | SEM3054 (unchanged) |
| Non-union subject (`int`, `string`, `bool`, tuple) with literal arms and no fallback | not checked | not checked (unchanged; `checkCompareExhausiveness` returns early for non-unions) |
| Union with a bare-type member (`T \| E` caught by `err =>`) | a binding arm covers it | unchanged |

Non-union subjects: the brief asks me to state this, not change it. `compare v { 1 => 10; 2 => 20; }`
over `int` has no exhaustiveness check before or after. On this base such a program does not
diagnose at all ("return origins: expression 8 is not typed", the same on both binaries), so I
could not observe its run-time fallback here. That is recorded under "Unverified".

## 2. Diagnostic

New code in `internal/diag/codes.go`: `SemaNonexhaustiveGuardedMatch Code = 3219`. It has a
`codeDescription` entry, "non-exhaustive pattern match: every arm for a variant can miss". 3218 was
the highest number in use, and `TestEveryDiagnosticCodeNumberIsClaimedOnce` and
`TestEveryDiagnosticCodeHasADescriptionEntry` pass.

```
error SEM3219 lit.sg:2:12 non-exhaustive pattern match: some `Some` values match no arm
help SEM3219 lit.sg:2:12 add `Some(_) => ...` after those arms, or a `finally` arm
note SEM3219 lit.sg:2:12 every arm for `Some` has a guard or a payload pattern that can fail, and only an unguarded arm whose pattern cannot fail covers a variant
```

(`surge diag --format short --with-notes` on `compare v { Some(1) => 10; nothing => 77; }`.)
The help writes one `_` per payload slot (`Pair(_, _) => ...`), gives a bare tag as `Tag => ...`,
and gives `nothing` as `nothing => ...`. When one compare has both kinds of gap, SEM3053 names the
unmentioned variants and SEM3219 names the partly covered ones.

Tests for the code: `TestNonexhaustiveGuardedMatchCodeNumber` checks the literal number and that
the code has its own description, and `TestNonexhaustiveGuardedMatchNamesTheVariantAndTheWayOut`
checks the wording. Both are in `internal/sema/compare_irrefutable_exhaustive_test.go`, because
the diag package cannot type a compare.

## 3. Docs

`docs/LANGUAGE.md` §compare Notes, directly after the exhaustiveness bullet (old l.977), has two
new bullets: the rule, and "non-union subjects are not checked; write `finally` when a fallback
is meant". `docs/LANGUAGE.ru.md` carries the same two bullets at the same place; its §compare
Notes are the same English text as `LANGUAGE.md`, so the text is identical.

## 4. Census: base vs after

Both binaries diagnose every `.sg` under `testdata/golden`, `stdlib`, `core`, `showcases` and
`benchmarks`: 1163 files, `--format short`, with `--directives=collect` under `/directives/`, and
`SURGE_STDLIB` set to the repo root. The two outputs are compared per file with order-insensitive
lines.

```
S=<scratch>; git worktree add $S/basewt 045c113 && (cd $S/basewt && go build -o $S/surge-base ./cmd/surge)
go build -o $S/surge-after ./cmd/surge
find testdata/golden stdlib core showcases benchmarks -type f -name '*.sg' | LC_ALL=C sort > $S/files.txt
# per file, both binaries:  timeout 120 env SURGE_STDLIB=$PWD $S/surge-$b diag --format short --directives=$mode $rel
```

**Result: exactly one file changes.**

| File | Base | After |
|---|---|---|
| `testdata/golden/mir/compare_guard_await_release.sg` | `diagnosis failed: return-origin analysis unfinished` (3 reasons, one of them `compare has an unproved unmatched continuation`) | `error SEM3219 …:12:12 non-exhaustive pattern match: some \`Success\` values match no arm` |

Coverage caveats:
- The 10 `core/*.sg` files refuse direct diagnosis on both binaries ("core namespace reserved").
  core is type-checked as part of every golden program (542 diagnose clean after).
- As a direct check, I scanned the source of `core/` and `stdlib/` for any arm with an `if` guard,
  or a tag arm whose payload holds a literal, a nested call or an enum selector. There are 0 hits.
  The same scan over `testdata/golden` finds 8 lines, and the census shows that exactly the
  refused one lacks a later irrefutable arm, so the scan does match. It is line-based, so an arm
  split across lines would be missed.
- stdlib: 28 of 32 files end in return-origin "diagnosis failed" on both binaries. That stage runs
  after sema, and a SEM3219 would have shown instead (as it did for the golden above).
- showcases and benchmarks show no difference.

### The refused program and its fix

`testdata/golden/mir/compare_guard_await_release.sg` is a valid MIR golden (`.diag` empty), so the
source is fixed:

```diff
     return compare a.await() {
         Success(x) if compare b.await() { Success(v) => v; Cancelled() => false; } => x;
+        Success(_) => 0;
         Cancelled() => 0;
     };
```

Why `Success(_) => 0`:
- The stored `.mir` golden shows how the base lowered a failed guard: `bb3: if L13 then bb7 else
  bb2` → `bb2: tag_test L8 is Cancelled` → false → `bb10: L24 = call default()` for `int`, which is
  `0`.
- The author's own fallback arm, `Cancelled() => 0`, returns the same `0`.
- So the new arm spells out the value the program already produced, and the runtime output is
  unchanged. No HARD STOP.
- Evidence: the program has no entry point, so a scratch copy was run with
  `test(n)`/`spawn slow(n)` and a `main` printing `test(1)` and `test(0)`. After the fix, both
  `--backend vm` and `--backend llvm` print `guard true: 6` and `guard false: 0`.
- The original cannot run on the base binary on either backend: `run` stops with the same
  return-origin unfinished error, even with `--unsafe`. So the "before" value comes from the
  lowered MIR, not from a run.

The sidecars (`.tokens`, `.ast`, `.fmt`, `.mir`; `.diag` stays empty) were regenerated with the
same commands `scripts/golden_update.sh` uses (`tokenize`, `parse`, `fmt --stdout`,
`diag --format short --emit-mir`). The script as a whole cannot run on this base: it records
"diagnostics failed for valid case" for 85 golden programs that already fail return-origin. As a
check of the method, running the same `--emit-mir` command in the live tree with the base binary
reproduces the stored `.mir` byte for byte for 5 passing MIR goldens.

`testdata/golden.expectations.json` is **not** touched. Its frozen `corpus_sha256`/`entry_count`
(5487 entries) already disagrees with the base tree (5498 entries, digest
`61432dcc…` vs frozen `c7f458f1…`, from `goldencheck.Scan`). Refreezing it needs `make golden-update`
to succeed, and it does not succeed on this base.

## 5. Rows and counterfactual

`internal/sema/compare_irrefutable_exhaustive_test.go`. These are snippet-harness rows over a
user union, `Maybe<T> = Just(T) | Empty` plus the one-member `Boxed<T> = Box(T)`:

| Row | Expect |
|---|---|
| guarded arm does not cover | SEM3219, no SEM3053 |
| guarded arm then binding arm | clean |
| literal payload does not cover (`Just(1)`) | SEM3219 |
| literal payload then `Just(_)` | clean |
| literal payload then `_` | clean |
| nested refutable tag (`Just(Just(x))` over `Maybe<Maybe<int>>`) | SEM3219 |
| nested tag exhaustive for its type (`Just(Box(b))` over `Maybe<Boxed<int>>`) | clean |
| tuple payload with a literal (`Just((1, y))`) | SEM3219 |
| tuple payload of bindings (`Just((x, y))`) | clean |
| `finally` after refutable arms | clean, **no** SEM3054 |
| `finally` after irrefutable coverage | SEM3054 |
| unmentioned variant | SEM3053 only |
| guarded zero-payload variant (`Empty if true`) | SEM3219 |
| non-union `int` subject with literal arms | nothing (unchanged) |

Counterfactual: `type_expr_compare.go` is restored to `045c113`, the new helper file and code are
kept so it compiles, and `go test ./internal/sema -run 'NonexhaustiveGuarded|CompareExhaustivenessCountsOnly' -v`
is run:

```
--- PASS: TestNonexhaustiveGuardedMatchCodeNumber
--- FAIL: TestCompareExhaustivenessCountsOnlyIrrefutableUnguardedArms
    --- FAIL: .../guarded_arm_does_not_cover
    --- PASS: .../guarded_arm_then_binding_arm
    --- FAIL: .../literal_payload_does_not_cover
    --- PASS: .../literal_payload_then_wildcard_payload
    --- PASS: .../literal_payload_then_wildcard_arm
    --- FAIL: .../nested_refutable_tag_does_not_cover
    --- PASS: .../nested_tag_exhaustive_for_its_type_covers
    --- FAIL: .../tuple_payload_with_a_literal_does_not_cover
    --- PASS: .../tuple_payload_of_bindings_covers
    --- FAIL: .../finally_closes_refutable_arms_and_is_not_redundant
    --- PASS: .../finally_after_irrefutable_coverage_stays_redundant
    --- PASS: .../unmentioned_variant_keeps_the_old_code_alone
    --- FAIL: .../guarded_zero-payload_variant_does_not_cover
    --- PASS: .../non-union_subject_is_not_checked,_before_or_after
--- FAIL: TestNonexhaustiveGuardedMatchNamesTheVariantAndTheWayOut
```

Every row the ruling changes goes red. The rows that stay green are the ones the ruling keeps
legal or leaves unchanged, as intended.

## 6. Return-origin: the unmatched continuation

`return_origin_compare.go:105` reports `compare has an unproved unmatched continuation`. For the
forms the ruling covers, that path now cannot be reached: a union compare whose variant is only
guarded or refutably matched is refused in sema (SEM3219), and return-origin never runs. A compare
that sema accepts ends every variant in an irrefutable arm or `finally`, and `split` treats those
as full matches.

Programs that diagnose differently:

| Program | Base | After |
|---|---|---|
| `testdata/golden/mir/compare_guard_await_release.sg` (original source) | unfinished, including `unproved unmatched continuation` at 0:193-333 | SEM3219 on `Success` |
| same, with the source fix | n/a | clean (exit 0) |

In the census, the reason appears on the base in 2 goldens and after in 1. The remaining one is
`testdata/golden/vm_compare/compare_patterns.sg` l.95/100/105:
`compare t0 { (0, true) => …; (x, false) => …; (x, y) => …; }` over a **tuple**. That subject
is not a union, so the ruling does not reach it, and `(x, y)` is irrefutable. The continuation is
reported because this base has no precise transfer for tuple and literal patterns ("compare pattern
needs a precise matching transfer" on each arm). That is the N-PATTERN and N-TUPLE work, which is
on separate branches.

Scratch probes, base → after (`surge diag --format short`):
- `Some(x) if x > 0 => x; nothing => 0;`: unfinished (includes the unmatched continuation) →
  SEM3219.
- `Some(1) => 10; nothing => 77;`: unfinished (includes the unmatched continuation) → SEM3219.
- `Some(Some(x)) => x; nothing => 0;` over `Option<Option<int>>`: unfinished (includes the
  unmatched continuation) → SEM3219.
- `Some(1) => 10; Some(k) if k > 5 => k; Some(_) => 3; nothing => 77;`: **crash** `return origins:
  pattern binding 2904 has no exact original scope and type` → only `compare pattern needs a
  precise matching transfer` (the N-PATTERN case).
  - The crash came from narrowing. The old count let `Some(1)` consume `Some`, so the next arm
    was typed against `nothing` and `k` got no type.
  - The strict count leaves `Some` in the remaining set, so `k : int`.
- `Some(k) if k > 5 => k; Some(_) => 3; nothing => 77;`: clean on both. `run --backend vm` and
  `--backend llvm` both print `3`, `9`, `77` for `Some(2)`, `Some(9)`, `nothing`.

## 7. Suites: base vs after

`go build ./...` and `go vet ./...` both pass (exit 0).

| Suite | Base (045c113) | After |
|---|---|---|
| `go test ./internal/sema ./internal/diag` | ok, 1248 test passes, 0 failures | ok, 1265 passes, 0 failures (+17 = 2 new tests, 1 table test and its 14 rows) |
| `go test -timeout 55m ./internal/driver` | FAIL, 55 failing test names (33 top-level), 1426 passes, 731 s | FAIL, the **same** 55 names, 1426 passes, 715 s |
| `go test ./internal/ownershipgate ./internal/mir` | mir ok; ownershipgate FAIL, 10 failing names | mir ok; ownershipgate FAIL, 9 failing names |

Failing-name sets:
- **driver:** base minus after is empty, and after minus base is empty. None of the 33 failing
  top-level tests mentions compare, guard or MIR. They are return-origin, clone, publication and
  capability tests that fail identically on the base.
- **ownershipgate:** this package was added because `internal/ownershipgate/representative_test.go`
  lists the edited golden as one of its 22 representative fixtures, and it was the only test file
  outside the corpus runners that names it. The one change is
  `TestOwnershipRepresentativeCorpusPassesDevGate/testdata/golden/mir/compare_guard_await_release.sg`,
  which fails on the base (return-origin unfinished) and **passes** after the source fix. There
  are no new failures.

Commands: `go test -count=1 -timeout 55m ./internal/driver -json` and `go test -count=1 ./internal/ownershipgate ./internal/mir -json`, each run in the base worktree and
in the work tree, with failing names taken from `"Action":"fail"` events that carry a `Test`.

## 8. Ledger text for a new DEBT row (id to be renumbered)

> **RV2-DEBT-NNN: compare exhaustiveness counted refutable and guarded arms (fixed).** The
> checker's `consumeCompareMembers` counted a variant as covered by any arm naming its tag,
> ignoring the arm's guard and payload patterns. As a result:
> - `compare v { Some(1) => …; nothing => …; }` and `Some(x) if c => …` were accepted.
> - The unmatched value reached MIR's `call default()` and returned a silent zero value on both
>   backends.
> - Return-origin failed closed on the same compares with "compare has an unproved unmatched
>   continuation".
> - The same count narrowed later arms onto the wrong member, so `Some(1) => …; Some(k) => …`
>   left `k` untyped and aborted return-origin ("pattern binding has no exact original scope and
>   type").
>
> Owner ruling 2026-09-25 (option B): only an unguarded arm whose pattern is irrefutable (`_`, a
> binding, a tuple of irrefutable patterns, or a one-member-union tag with irrefutable payload)
> covers its variant; otherwise SEM3219 `SemaNonexhaustiveGuardedMatch` asks for `Tag(_) => …` or
> `finally`. One golden (`mir/compare_guard_await_release.sg`) gained `Success(_) => 0`, which is
> the value its lowered fallback already returned. Non-union subjects (int/string/bool/tuple)
> remain unchecked for exhaustiveness. That is a separate question for the owner, not part of this
> fix.

## 9. Unverified or open

- **Non-union fallback at run time.** What an unmatched `int`/`string` compare returns at run
  time: on this base those programs fail return-origin before they can run, so the value is not
  observed here. The docs therefore say only that such compares are not checked.
- **The refused golden's "before" run.** Its runtime value before the fix comes from the stored
  MIR (`call default()`), not from a run, because the base refuses to run it (section 4).
- **Source scan.** The `core/` scan is line-based (section 4).
- **Frozen corpus manifest.** It is stale on the base and is not refrozen (section 4).
- **VM1003 panic.** The VM panic on `Some(nothing)` passed as `Option<Option<int>>` (seen in the
  N-PATTERN work) is unrelated to this ruling and not addressed.
